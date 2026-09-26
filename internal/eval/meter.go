package eval

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/sagar0163/nebula/internal/llm"
)

// ModelUsage is token usage attributed to one model.
type ModelUsage struct {
	Calls  int `json:"calls"`
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
}

// Usage is a snapshot of metered token consumption. Two snapshots of the same
// Meter are diffed to attribute usage to a single case.
type Usage struct {
	Calls  int
	Input  int
	Output int
	// Models breaks the totals down per model name. A run is not single-model:
	// the diagnose, heal and learn tiers each pick their own, and they are
	// priced differently.
	Models map[string]ModelUsage
}

// Meter accumulates token usage across provider calls. The router fans out
// concurrently, so every mutation is guarded. A nil *Meter is usable and counts
// nothing, which lets callers skip metering entirely.
type Meter struct {
	mu     sync.Mutex
	usage  Usage
	models map[string]ModelUsage
}

// NewMeter returns an empty Meter.
func NewMeter() *Meter {
	return &Meter{models: map[string]ModelUsage{}}
}

// Snapshot returns the cumulative usage so far. The result is a deep copy, so a
// caller can hold it across a window and diff it later.
func (m *Meter) Snapshot() Usage {
	if m == nil {
		return Usage{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	models := make(map[string]ModelUsage, len(m.models))
	for k, v := range m.models {
		models[k] = v
	}
	return Usage{
		Calls:  m.usage.Calls,
		Input:  m.usage.Input,
		Output: m.usage.Output,
		Models: models,
	}
}

func (m *Meter) record(model string, in, out int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage.Calls++
	m.usage.Input += in
	m.usage.Output += out
	if model != "" {
		if m.models == nil {
			m.models = map[string]ModelUsage{}
		}
		mu := m.models[model]
		mu.Calls++
		mu.Input += in
		mu.Output += out
		m.models[model] = mu
	}
}

// DiffUsage returns the usage consumed between two snapshots of the same Meter.
// A clock that moved backwards (only possible if two Meter instances were
// mixed up) yields a zero rather than a negative count.
func DiffUsage(before, after Usage) Usage {
	d := Usage{Models: map[string]ModelUsage{}}
	d.Calls = maxInt(0, after.Calls-before.Calls)
	d.Input = maxInt(0, after.Input-before.Input)
	d.Output = maxInt(0, after.Output-before.Output)

	for name, a := range after.Models {
		b := before.Models[name]
		mu := ModelUsage{
			Calls:  maxInt(0, a.Calls-b.Calls),
			Input:  maxInt(0, a.Input-b.Input),
			Output: maxInt(0, a.Output-b.Output),
		}
		if mu.Calls == 0 && mu.Input == 0 && mu.Output == 0 {
			continue
		}
		d.Models[name] = mu
	}
	return d
}

// ModelNames returns the models that were used, sorted for stable output.
func (u Usage) ModelNames() []string {
	names := make([]string, 0, len(u.Models))
	for name := range u.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DominantModel returns the model that handled the most calls, breaking ties
// alphabetically so the label does not depend on map ordering. It is the model
// to show in a one-line summary of a run.
func (u Usage) DominantModel() string {
	best, bestCalls := "", -1
	for _, name := range u.ModelNames() {
		if c := u.Models[name].Calls; c > bestCalls {
			best, bestCalls = name, c
		}
	}
	return best
}

// Cost returns the estimated USD cost of the usage and whether every model
// involved had a rate. When ok is false the sum is a lower bound and the token
// count is the only trustworthy cost signal. Usage of no models at all — a case
// that failed before the agent reached a provider — costs nothing, so it is
// known rather than unknown.
func (u Usage) Cost() (float64, bool) {
	if len(u.Models) == 0 {
		return 0, true
	}
	var total float64
	ok := true
	for name, mu := range u.Models {
		c, known := CostFor(name, mu.Input, mu.Output)
		if !known {
			ok = false
		}
		total += c
	}
	return total, ok
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MeterProvider wraps inner so every completion and embedding is counted
// against m.
//
// The wrapper forwards Model to the wrapped provider, so metering changes
// neither which model runs nor how it is reported. fallback supplies the
// configured model names for providers that do not implement Model themselves;
// a workload with no entry there is reported as unknown and priced by token
// count alone.
func MeterProvider(inner llm.Provider, m *Meter, fallback map[llm.Workload]string) llm.Provider {
	if inner == nil {
		return nil
	}
	return &meteredProvider{inner: inner, meter: m, models: fallback}
}

type meteredProvider struct {
	inner  llm.Provider
	meter  *Meter
	models map[llm.Workload]string
}

func (p *meteredProvider) Name() string { return p.inner.Name() }

func (p *meteredProvider) Available(ctx context.Context) bool {
	return p.inner.Available(ctx)
}

// Model reports the model the wrapped provider uses for a workload, falling
// back to the configured names and finally to "unknown".
func (p *meteredProvider) Model(w llm.Workload) string {
	if mp, ok := p.inner.(interface{ Model(llm.Workload) string }); ok {
		if m := mp.Model(w); m != "" {
			return m
		}
	}
	if m, ok := p.models[w]; ok && m != "" {
		return m
	}
	if m, ok := p.models[llm.WorkloadHeal]; ok && m != "" {
		return m
	}
	return "unknown"
}

func (p *meteredProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	model := p.Model(req.Workload)
	in := countRequestTokens(req)

	ch, err := p.inner.Complete(ctx, req)
	if err != nil {
		// A failed call still consumed the prompt, so it is billed.
		p.meter.record(model, in, 0)
		return nil, err
	}

	out := make(chan llm.Token, 32)
	go func() {
		defer close(out)
		var produced int
		defer func() { p.meter.record(model, in, produced) }()

		for tok := range ch {
			produced += countTokens(tok.Text)
			select {
			case out <- tok:
			case <-ctx.Done():
				// The consumer went away (a fan-out sibling lost the race);
				// the tokens already produced still count toward the bill.
				return
			}
		}
	}()
	return out, nil
}

func (p *meteredProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vec, err := p.inner.Embed(ctx, text)
	// Embeddings bill input tokens only, with no output component.
	p.meter.record(p.Model(llm.WorkloadEmbed), countTokens(text), 0)
	return vec, err
}

// countRequestTokens estimates the prompt size of a request.
func countRequestTokens(req llm.Request) int {
	n := countTokens(req.SystemPrompt)
	for _, m := range req.Messages {
		n += countTokens(m.Content)
	}
	return n
}

// tokensPerRune is the usual English-text heuristic: roughly four characters
// per token. Nebula's providers do not report usage on the token stream, so
// this is an estimate, and the report labels it as one.
const tokensPerRune = 4

// countTokens estimates the token count of s.
func countTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + tokensPerRune - 1) / tokensPerRune
}

// rate is a list price in USD per million tokens.
type rate struct {
	in  float64
	out float64
}

// rates holds list prices for the models Nebula defaults to. They are a
// starting point for comparing runs, not an invoice — refresh them when a
// provider changes pricing. Any model missing here is reported as a token count
// instead of a dollar cost, which is why self-hosted (ollama) and NIM-hosted
// models are deliberately absent: they have no list price, and claiming $0
// would understate cost.
var rates = map[string]rate{
	"gemini-2.0-flash":        {in: 0.10, out: 0.40},
	"gemini-2.5-flash":        {in: 0.30, out: 2.50},
	"gemini-2.5-pro":          {in: 1.25, out: 10.00},
	"llama-3.1-8b-instant":    {in: 0.05, out: 0.08},
	"llama-3.3-70b-versatile": {in: 0.59, out: 0.79},
	"open-mistral-nemo":       {in: 0.15, out: 0.15},
	"mistral-large-latest":    {in: 2.00, out: 6.00},
	"codestral-latest":        {in: 0.30, out: 0.90},
}

// CostFor returns the estimated USD cost of a call and whether a rate was
// found for the model. When ok is false the caller should report token counts
// instead of a dollar figure.
func CostFor(model string, inputTokens, outputTokens int) (float64, bool) {
	r, ok := rateFor(model)
	if !ok {
		return 0, false
	}
	const perMillion = 1_000_000
	cost := (float64(inputTokens)/perMillion)*r.in + (float64(outputTokens)/perMillion)*r.out
	if cost < 0 {
		cost = 0
	}
	return cost, true
}

// rateFor resolves a model name to its price, preferring an exact match and
// otherwise taking the longest key contained in the name, so a namespaced or
// suffixed variant ("openai/gemini-2.5-pro-preview") still resolves.
func rateFor(model string) (rate, bool) {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return rate{}, false
	}
	if r, ok := rates[name]; ok {
		return r, true
	}
	best := ""
	var found rate
	for key, r := range rates {
		if strings.Contains(name, key) && len(key) > len(best) {
			best, found = key, r
		}
	}
	return found, best != ""
}

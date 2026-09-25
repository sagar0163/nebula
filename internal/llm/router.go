package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// Router selects a provider based on workload and availability,
// with fallback across the registered provider chain.
type Router struct {
	providers map[Workload][]Provider

	mu sync.Mutex
	// cooldown maps a provider name to the end of its rate-limit backoff
	// window, so fan-out does not race a provider that is known to be throttled.
	cooldown map[string]time.Time
}

// NewRouter returns a Router. Providers are tried in slice order per workload.
func NewRouter() *Router {
	return &Router{
		providers: make(map[Workload][]Provider),
		cooldown:  make(map[string]time.Time),
	}
}

// Register adds a provider for the given workload tier.
// Providers registered first have higher priority.
func (r *Router) Register(w Workload, p Provider) {
	r.providers[w] = append(r.providers[w], p)
}

// Response is the outcome of a single provider attempt. On success it carries
// the first token plus the rest of the stream; on failure only Err is set.
type Response struct {
	// Provider is the provider that produced this result.
	Provider Provider
	// First is the first token of the winning stream.
	First Token
	// Stream carries the remaining tokens. Nil when the attempt produced none.
	Stream <-chan Token
	// Err is non-nil when the attempt failed.
	Err error
	// callErr reports whether Err came from Complete itself rather than from
	// the token stream, matching the sequential path's lastErr semantics.
	callErr bool
	// cancel releases the attempt's context.
	cancel context.CancelFunc
	// slot is the fan-out position of the attempt, set before publishing.
	slot int
}

// RoutingConfig holds the provider routing tunables read from the [llm] section
// of config.toml.
type RoutingConfig struct {
	// ParallelFanout is how many providers are raced concurrently.
	// A width of 1 disables fan-out and restores sequential routing.
	ParallelFanout int
	// Timeout bounds a single provider attempt.
	Timeout time.Duration
}

const (
	// defaultParallelFanout races the primary against the next best provider.
	defaultParallelFanout = 2
	// maxParallelFanout caps the race width: every extra provider in the race
	// is another billable request that is thrown away mid-flight.
	maxParallelFanout = 3
	// defaultTimeout bounds a single attempt when llm.timeout_seconds is unset.
	defaultTimeout = 60 * time.Second
	// maxRateLimitRetries is how often a throttled provider is retried in place.
	maxRateLimitRetries = 3
	// rateLimitBackoff is the first backoff step; it doubles per retry and is
	// also the cooldown applied to a provider that stays throttled.
	rateLimitBackoff = 2 * time.Second
)

// routingConfig reads the routing tunables, clamping them to supported ranges.
func routingConfig() RoutingConfig {
	cfg := RoutingConfig{
		ParallelFanout: viper.GetInt("llm.parallel_fanout"),
		Timeout:        time.Duration(viper.GetInt("llm.timeout_seconds")) * time.Second,
	}
	if cfg.ParallelFanout <= 0 {
		cfg.ParallelFanout = defaultParallelFanout
	}
	if cfg.ParallelFanout > maxParallelFanout {
		cfg.ParallelFanout = maxParallelFanout
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return cfg
}

// Complete routes the request to the best available provider for the workload.
// The first llm.parallel_fanout available providers are raced concurrently and
// the first non-error stream wins; with a single candidate the request walks
// the chain sequentially. An available provider that errors falls through to
// the next one, and the last error is surfaced only when all fail.
func (r *Router) Complete(ctx context.Context, w Workload, req Request) (<-chan Token, error) {
	cfg := routingConfig()
	candidates := r.selectCandidates(ctx, w, cfg.ParallelFanout)
	if len(candidates) > 1 {
		return r.fanout(ctx, w, req, candidates, cfg)
	}
	return r.sequential(ctx, w, req, candidates, cfg)
}

// RouteParallel fires the first llm.parallel_fanout available providers for the
// workload concurrently and returns the first non-error stream, cancelling the
// siblings as soon as a winner appears. Providers that error do not cancel the
// race: the remaining attempts keep going and their result is used instead.
// With fewer than two candidates it falls through to sequential routing.
func (r *Router) RouteParallel(ctx context.Context, w Workload, req Request) (<-chan Token, error) {
	cfg := routingConfig()
	candidates := r.selectCandidates(ctx, w, cfg.ParallelFanout)
	return r.route(ctx, w, req, candidates, cfg)
}

// route dispatches to the parallel or sequential path based on candidate count.
func (r *Router) route(ctx context.Context, w Workload, req Request, candidates []Provider, cfg RoutingConfig) (<-chan Token, error) {
	if len(candidates) > 1 {
		return r.fanout(ctx, w, req, candidates, cfg)
	}
	return r.sequential(ctx, w, req, candidates, cfg)
}

// fanout races every candidate at once and keeps the first non-error stream.
func (r *Router) fanout(ctx context.Context, w Workload, req Request, candidates []Provider, cfg RoutingConfig) (<-chan Token, error) {
	req.Workload = w

	// cancelFanout is handed to pump on the winner path; every other exit path
	// has to release it itself, since cancelling it would kill the winner.
	fanCtx, cancelFanout := context.WithCancel(ctx)

	results := make(chan *Response, len(candidates))
	cancels := make([]context.CancelFunc, len(candidates))

	for i, p := range candidates {
		// The attempt context is parented to the fan-out context so the winner's
		// siblings can be cancelled without touching the winning stream.
		attemptCtx, cancelAttempt := context.WithCancel(fanCtx)
		cancels[i] = cancelAttempt
		go func(slot int, p Provider, attemptCtx context.Context, cancelAttempt context.CancelFunc) {
			res := r.attempt(attemptCtx, p, req, cfg.Timeout)
			res.slot = slot
			stream := res.cancel
			res.cancel = func() { stream(); cancelAttempt() }
			results <- res
		}(i, p, attemptCtx, cancelAttempt)
	}

	var lastErr error
	lastSlot := -1
	sawEmpty := false

	for range candidates {
		var res *Response
		select {
		case <-ctx.Done():
			cancelFanout()
			return nil, ctx.Err()
		case res = <-results:
		}

		switch {
		case res.Err != nil:
			// Errors never end the race: the slower providers are still running.
			// Keep the last chain-ordered call error for parity with sequential.
			res.cancel()
			if res.callErr && res.slot >= lastSlot {
				lastErr, lastSlot = res.Err, res.slot
			}
		case res.Stream == nil:
			// The provider answered with an empty stream: not a usable win.
			res.cancel()
			sawEmpty = true
		default:
			for i, cancel := range cancels {
				if i != res.slot {
					cancel()
				}
			}
			return pump(res, cancelFanout), nil
		}
	}

	cancelFanout()
	if lastErr != nil {
		return nil, lastErr
	}
	if sawEmpty {
		empty := make(chan Token)
		close(empty)
		return empty, nil
	}
	return nil, fmt.Errorf("no available LLM provider for workload %d", w)
}

// sequential walks the chain one provider at a time, retrying a throttled
// provider in place before falling through to the next one.
func (r *Router) sequential(ctx context.Context, w Workload, req Request, candidates []Provider, cfg RoutingConfig) (<-chan Token, error) {
	req.Workload = w

	var lastErr error
	for _, p := range candidates {
		res := r.attempt(ctx, p, req, cfg.Timeout)
		switch {
		case res.Err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if res.callErr {
				lastErr = res.Err
			}
		case res.Stream == nil:
			empty := make(chan Token)
			close(empty)
			return empty, nil
		default:
			return pump(res, func() {}), nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no available LLM provider for workload %d", w)
}

// attempt runs one provider until it produces its first token or fails,
// retrying in place while the provider reports a rate limit.
func (r *Router) attempt(ctx context.Context, p Provider, req Request, timeout time.Duration) *Response {
	retries := 0
	backoff := rateLimitBackoff

	for {
		pCtx, cancel := context.WithTimeout(ctx, timeout)
		ch, err := p.Complete(pCtx, req)

		var streamErr error
		var first Token
		var ok bool

		if err != nil {
			streamErr = err
		} else {
			first, ok = <-ch
			if !ok {
				streamErr = nil // stream closed ok immediately
			} else if first.Err != nil {
				streamErr = first.Err
			}
		}

		if streamErr != nil {
			cancel()
			noop := func() {}

			if isRL(streamErr.Error()) && retries < maxRateLimitRetries {
				fmt.Fprintf(os.Stderr, "llm: provider %s rate limited, retrying in %v...\n", p.Name(), backoff)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return &Response{Provider: p, Err: ctx.Err(), callErr: true, cancel: noop}
				}
				retries++
				backoff *= 2
				continue
			}

			// A provider that stays throttled is parked for a cooldown so the
			// next fan-out races somebody else.
			if isRL(streamErr.Error()) {
				r.startCooldown(p.Name(), backoff)
			}
			fmt.Fprintf(os.Stderr, "llm: provider %s failed: %v, falling back\n", p.Name(), streamErr)
			return &Response{Provider: p, Err: streamErr, callErr: err != nil, cancel: noop}
		}

		if !ok {
			cancel()
			return &Response{Provider: p, cancel: func() {}}
		}

		return &Response{Provider: p, First: first, Stream: ch, cancel: cancel}
	}
}

// pump forwards the winning stream to the caller and releases the fan-out once
// the stream ends.
func pump(res *Response, finish context.CancelFunc) <-chan Token {
	out := make(chan Token, 32)
	go func() {
		defer close(out)
		defer finish()
		defer res.cancel()
		out <- res.First
		for x := range res.Stream {
			out <- x
		}
	}()
	return out
}

// chain returns the providers registered for the workload, or every registered
// provider when the workload has none.
func (r *Router) chain(w Workload) []Provider {
	chain := r.providers[w]
	if len(chain) == 0 {
		for _, providers := range r.providers {
			chain = append(chain, providers...)
		}
	}
	return chain
}

// selectCandidates returns the available providers for the workload in
// registration order, leaving out providers inside a rate-limit cooldown
// unless that would leave nothing to try. A positive limit caps the width.
func (r *Router) selectCandidates(ctx context.Context, w Workload, limit int) []Provider {
	var live, cooling []Provider
	for _, p := range r.chain(w) {
		if !p.Available(ctx) {
			continue
		}
		if r.coolingDown(p.Name()) {
			cooling = append(cooling, p)
			continue
		}
		live = append(live, p)
	}

	out := live
	if len(out) == 0 {
		out = cooling
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// startCooldown parks a provider for at least d, never shortening a longer
// cooldown already in effect.
func (r *Router) startCooldown(name string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cooldown == nil {
		r.cooldown = make(map[string]time.Time)
	}
	until := time.Now().Add(d)
	if cur, ok := r.cooldown[name]; ok && cur.After(until) {
		return
	}
	r.cooldown[name] = until
}

// coolingDown reports whether the provider is inside a rate-limit backoff.
func (r *Router) coolingDown(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.cooldown[name]
	return ok && time.Now().Before(until)
}

// Embed routes to the first available embed-capable provider, falling through
// on per-provider errors and surfacing the last error when all fail.
func (r *Router) Embed(ctx context.Context, text string) ([]float32, error) {
	var lastErr error
	for _, p := range r.providers[WorkloadEmbed] {
		if !p.Available(ctx) {
			continue
		}
		vec, err := p.Embed(ctx, text)
		if err != nil {
			lastErr = err
			continue
		}
		return vec, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no available embedding provider")
}

func isRL(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "quota exceeded")
}

// ContextWindow returns the context window token limit for the best available
// provider for the given workload. If unknown or unavailable, returns 4096.
func (r *Router) ContextWindow(ctx context.Context, w Workload) int {
	chain := r.chain(w)

	for _, p := range chain {
		if !p.Available(ctx) {
			continue
		}
		if cp, ok := p.(interface{ ContextWindow(Workload) int }); ok {
			if tokens := cp.ContextWindow(w); tokens > 0 {
				return tokens
			}
		}
		switch p.Name() {
		case "gemini":
			return 1_000_000
		case "mistral", "nvidia":
			return 32_768
		case "groq":
			return 8_192
		case "ollama":
			return 8_192
		case "anthropic", "openai":
			return 128_000
		}
	}

	return 4_096
}

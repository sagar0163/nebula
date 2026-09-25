package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	ollamaapi "github.com/ollama/ollama/api"
	"github.com/sagar0163/nebula/internal/llm"
)

// OllamaConfig holds configuration for the Ollama provider.
type OllamaConfig struct {
	BaseURL       string // default: http://localhost:11434
	ModelDiagnose string
	ModelHeal     string
	ModelLearn    string
	ModelEmbed    string
}

// OllamaProvider implements llm.Provider using a local Ollama server.
type OllamaProvider struct {
	client *ollamaapi.Client
	cfg    OllamaConfig
}

// NewOllama creates an OllamaProvider.
func NewOllama(cfg OllamaConfig) (*OllamaProvider, error) {
	base := cfg.BaseURL
	if base == "" {
		base = "http://localhost:11434"
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("ollama: invalid base URL %q: %w", base, err)
	}
	client := ollamaapi.NewClient(u, nil)
	return &OllamaProvider{client: client, cfg: cfg}, nil
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2_000_000_000) // 2s
	defer cancel()
	_, err := p.client.List(ctx)
	return err == nil
}

func (p *OllamaProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	model := p.selectModel(req)

	// Build prompt from messages (Ollama Generate uses a single prompt string).
	prompt := buildPrompt(req)

	ch := make(chan llm.Token, 32)
	go func() {
		defer close(ch)
		err := p.client.Generate(ctx, &ollamaapi.GenerateRequest{
			Model:  model,
			Prompt: prompt,
			Stream: boolPtr(true),
			Format: jsonFormat(req),
		}, func(resp ollamaapi.GenerateResponse) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if resp.Response != "" {
				ch <- llm.Token{Text: resp.Response}
			}
			if resp.Done {
				ch <- llm.Token{IsLast: true}
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			ch <- llm.Token{Err: err, IsLast: true}
		}
	}()

	return ch, nil
}

func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	model := p.cfg.ModelEmbed
	if model == "" {
		model = "nomic-embed-text"
	}

	resp, err := p.client.Embeddings(ctx, &ollamaapi.EmbeddingRequest{
		Model:  model,
		Prompt: text,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}

	out := make([]float32, len(resp.Embedding))
	for i, v := range resp.Embedding {
		out[i] = float32(v)
	}
	return out, nil
}

func (p *OllamaProvider) selectModel(req llm.Request) string {
	switch req.Workload {
	case llm.WorkloadDiagnose:
		if p.cfg.ModelDiagnose != "" {
			return p.cfg.ModelDiagnose
		}
		return "llama3.2:3b"
	case llm.WorkloadLearn:
		if p.cfg.ModelLearn != "" {
			return p.cfg.ModelLearn
		}
		// fallthrough to heal
	}
	if p.cfg.ModelHeal != "" {
		return p.cfg.ModelHeal
	}
	return "llama3.2"
}

// buildPrompt collapses llm.Request messages into a single Ollama prompt string.
func buildPrompt(req llm.Request) string {
	var out string
	if req.SystemPrompt != "" {
		out += "System: " + req.SystemPrompt + "\n\n"
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			out += "Assistant: " + m.Content + "\n"
		default:
			out += "User: " + m.Content + "\n"
		}
	}
	out += "Assistant:"
	return out
}

func boolPtr(b bool) *bool { return &b }

// jsonFormat returns the Ollama "format" hint for the requested response
// format. Ollama takes the literal string "json" (or a JSON schema); any other
// value means unconstrained output and yields a nil (omitted) field.
func jsonFormat(req llm.Request) json.RawMessage {
	if req.ResponseFormat != "json" {
		return nil
	}
	return json.RawMessage(`"json"`)
}

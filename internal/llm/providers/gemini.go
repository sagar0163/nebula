package providers

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"github.com/sagar0163/nebula/internal/llm"
	"google.golang.org/api/option"
)

// GeminiConfig holds configuration for the Gemini provider.
type GeminiConfig struct {
	APIKey        string
	ModelDiagnose string
	ModelHeal     string
	ModelLearn    string
	ModelEmbed    string
}

// GeminiProvider implements llm.Provider using Google Gemini.
type GeminiProvider struct {
	cfg GeminiConfig
}

// NewGemini creates a GeminiProvider. Returns nil if APIKey is empty.
func NewGemini(cfg GeminiConfig) *GeminiProvider {
	if cfg.APIKey == "" {
		return nil
	}
	return &GeminiProvider{cfg: cfg}
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Available(_ context.Context) bool {
	return p != nil && p.cfg.APIKey != ""
}

func (p *GeminiProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(p.cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("gemini: create client: %w", err)
	}

	modelName := p.selectModel(req)
	model := client.GenerativeModel(modelName)

	if req.SystemPrompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{genai.Text(req.SystemPrompt)},
		}
	}
	if req.MaxTokens > 0 {
		n := int32(req.MaxTokens)
		model.MaxOutputTokens = &n
	}
	if req.Temperature > 0 {
		t := float32(req.Temperature)
		model.Temperature = &t
	}
	if req.ResponseFormat == "json" {
		model.ResponseMIMEType = "application/json"
	}

	parts := toGeminiParts(req.Messages)
	iter := model.GenerateContentStream(ctx, parts...)

	ch := make(chan llm.Token, 32)
	go func() {
		defer close(ch)
		defer client.Close()
		for {
			resp, err := iter.Next()
			if err != nil {
				if err.Error() == "iterator done" {
					ch <- llm.Token{IsLast: true}
					return
				}
				if ctx.Err() == nil {
					ch <- llm.Token{Err: fmt.Errorf("gemini stream: %w", err), IsLast: true}
				}
				return
			}
			for _, cand := range resp.Candidates {
				if cand.Content == nil {
					continue
				}
				for _, part := range cand.Content.Parts {
					if t, ok := part.(genai.Text); ok && string(t) != "" {
						select {
						case ch <- llm.Token{Text: string(t)}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return ch, nil
}

func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(p.cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("gemini embed: create client: %w", err)
	}
	defer client.Close()

	modelName := p.cfg.ModelEmbed
	if modelName == "" {
		modelName = "text-embedding-004"
	}

	model := client.EmbeddingModel(modelName)
	res, err := model.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return nil, fmt.Errorf("gemini embed: %w", err)
	}

	out := make([]float32, len(res.Embedding.Values))
	copy(out, res.Embedding.Values)
	return out, nil
}

func (p *GeminiProvider) selectModel(req llm.Request) string {
	switch req.Workload {
	case llm.WorkloadDiagnose:
		if p.cfg.ModelDiagnose != "" {
			return p.cfg.ModelDiagnose
		}
		return "gemini-2.0-flash"
	case llm.WorkloadLearn:
		if p.cfg.ModelLearn != "" {
			return p.cfg.ModelLearn
		}
		// fallthrough to heal
	}
	if p.cfg.ModelHeal != "" {
		return p.cfg.ModelHeal
	}
	return "gemini-2.5-pro"
}

// Model reports the model this provider uses for a workload tier. It is the
// same resolution Complete performs, exposed so callers that need to label or
// price a run do not have to duplicate the defaults.
func (p *GeminiProvider) Model(w llm.Workload) string {
	return p.selectModel(llm.Request{Workload: w})
}

// toGeminiParts converts llm.Messages to genai.Part slice for the last user turn.
// Gemini's GenerateContentStream takes a flat list of parts for the current turn.
func toGeminiParts(msgs []llm.Message) []genai.Part {
	parts := make([]genai.Part, 0, len(msgs))
	for _, m := range msgs {
		parts = append(parts, genai.Text(m.Content))
	}
	return parts
}

func (p *GeminiProvider) ModelName(w llm.Workload) string {
	switch w {
	case llm.WorkloadHeal:
		return p.cfg.ModelHeal
	case llm.WorkloadLearn:
		return p.cfg.ModelLearn
	default:
		return p.cfg.ModelDiagnose
	}
}

package providers

import (
	"context"
	"fmt"
	"io"

	"google.golang.org/genai"
	"github.com/sagar0163/nebula/internal/llm"
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
	cfg    GeminiConfig
	client *genai.Client
}

// NewGemini creates a GeminiProvider. Returns nil if APIKey is empty.
func NewGemini(ctx context.Context, cfg GeminiConfig) (*GeminiProvider, error) {
	if cfg.APIKey == "" {
		return nil, nil
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: create client: %w", err)
	}
	return &GeminiProvider{cfg: cfg, client: client}, nil
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Available(_ context.Context) bool {
	return p != nil && p.cfg.APIKey != ""
}

func (p *GeminiProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	modelName := p.selectModel(req)

	cfg := &genai.GenerateContentConfig{}
	if req.SystemPrompt != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.SystemPrompt}},
		}
	}
	if req.MaxTokens > 0 {
		n := int32(req.MaxTokens)
		cfg.MaxOutputTokens = n
	}
	if req.Temperature > 0 {
		t := float32(req.Temperature)
		cfg.Temperature = &t
	}
	if req.ResponseFormat == "json" {
		cfg.ResponseMIMEType = "application/json"
	}

	contents := toGeminiContents(req.Messages)
	iter := p.client.Models.GenerateContentStream(ctx, modelName, contents, cfg)

	ch := make(chan llm.Token, 32)
	go func() {
		defer close(ch)
		for resp, err := range iter {
			if err != nil {
				if err == io.EOF {
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
					if part.Text != "" {
						select {
						case ch <- llm.Token{Text: part.Text}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
		ch <- llm.Token{IsLast: true}
	}()

	return ch, nil
}

func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	modelName := p.cfg.ModelEmbed
	if modelName == "" {
		modelName = "text-embedding-004"
	}

	res, err := p.client.Models.EmbedContent(ctx, modelName,
		[]*genai.Content{{Parts: []*genai.Part{{Text: text}}}},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("gemini embed: %w", err)
	}
	if len(res.Embeddings) == 0 {
		return nil, fmt.Errorf("gemini embed: empty response")
	}

	out := make([]float32, len(res.Embeddings[0].Values))
	copy(out, res.Embeddings[0].Values)
	return out, nil
}

func (p *GeminiProvider) selectModel(req llm.Request) string {
	if req.Model != "" {
		return req.Model
	}
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
	}
	if p.cfg.ModelHeal != "" {
		return p.cfg.ModelHeal
	}
	return "gemini-2.5-pro"
}

// ModelName reports the model this provider uses for a workload tier.
func (p *GeminiProvider) ModelName(w llm.Workload) string {
	return p.selectModel(llm.Request{Workload: w})
}

// Model is an alias for ModelName for callers using the older interface.
func (p *GeminiProvider) Model(w llm.Workload) string {
	return p.ModelName(w)
}

// toGeminiContents converts llm.Messages to genai.Content slice.
func toGeminiContents(msgs []llm.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(msgs))
	for _, m := range msgs {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: m.Content}},
		})
	}
	return contents
}

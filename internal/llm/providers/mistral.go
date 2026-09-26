package providers

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/sagar0163/nebula/internal/llm"
)

// MistralConfig holds configuration for the Mistral AI provider.
type MistralConfig struct {
	APIKey        string
	ModelDiagnose string
	ModelHeal     string
	ModelLearn    string
	ModelEmbed    string
}

// MistralProvider implements llm.Provider using the Mistral AI API
// (OpenAI-compatible endpoint).
type MistralProvider struct {
	client openai.Client
	cfg    MistralConfig
}

// NewMistral creates a MistralProvider. Returns nil if APIKey is empty.
func NewMistral(cfg MistralConfig) *MistralProvider {
	if cfg.APIKey == "" {
		return nil
	}
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL("https://api.mistral.ai/v1"),
	)
	return &MistralProvider{client: client, cfg: cfg}
}

func (p *MistralProvider) Name() string { return "mistral" }

func (p *MistralProvider) Available(ctx context.Context) bool {
	if p == nil || p.cfg.APIKey == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3_000_000_000)
	defer cancel()
	_, err := p.client.Models.List(ctx)
	return err == nil
}

func (p *MistralProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	model := p.selectModel(req)
	msgs := buildOpenAIMessages(req)

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(model),
		Messages: msgs,
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = openai.Float(req.Temperature)
	}
	applyResponseFormat(&params, req)

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan llm.Token, 32)
	go func() {
		defer close(ch)
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				select {
				case ch <- llm.Token{Text: delta}:
				case <-ctx.Done():
					return
				}
			}
			if string(chunk.Choices[0].FinishReason) != "" {
				ch <- llm.Token{IsLast: true}
				return
			}
		}
		if err := stream.Err(); err != nil && ctx.Err() == nil {
			ch <- llm.Token{Err: err, IsLast: true}
		}
	}()

	return ch, nil
}

// Embed uses Mistral's embedding endpoint (mistral-embed model).
func (p *MistralProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	model := p.cfg.ModelEmbed
	if model == "" {
		model = "mistral-embed"
	}

	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(model),
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
	})
	if err != nil {
		return nil, fmt.Errorf("mistral embed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("mistral embed: empty response")
	}

	raw := resp.Data[0].Embedding
	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v)
	}
	return out, nil
}

func (p *MistralProvider) selectModel(req llm.Request) string {
	switch req.Workload {
	case llm.WorkloadDiagnose:
		if p.cfg.ModelDiagnose != "" {
			return p.cfg.ModelDiagnose
		}
		return "open-mistral-nemo"
	case llm.WorkloadLearn:
		if p.cfg.ModelLearn != "" {
			return p.cfg.ModelLearn
		}
		// fallthrough to heal
	}
	if p.cfg.ModelHeal != "" {
		return p.cfg.ModelHeal
	}
	return "mistral-large-latest"
}

// Model reports the model this provider uses for a workload tier. It is the
// same resolution Complete performs, exposed so callers that need to label or
// price a run do not have to duplicate the defaults.
func (p *MistralProvider) Model(w llm.Workload) string {
	return p.selectModel(llm.Request{Workload: w})
}

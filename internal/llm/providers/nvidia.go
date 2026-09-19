package providers

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/sagar0163/nebula/internal/llm"
)

// NvidiaConfig holds configuration for the NVIDIA NIM provider.
type NvidiaConfig struct {
	APIKey        string
	BaseURL       string // default: https://integrate.api.nvidia.com/v1
	ModelDiagnose string
	ModelHeal     string
	ModelLearn    string
	ModelEmbed    string
}

// NvidiaProvider implements llm.Provider using the NVIDIA NIM API
// (OpenAI-compatible endpoint).
type NvidiaProvider struct {
	client openai.Client
	cfg    NvidiaConfig
}

// NewNvidia creates a NvidiaProvider. Returns nil if APIKey is empty.
func NewNvidia(cfg NvidiaConfig) *NvidiaProvider {
	if cfg.APIKey == "" {
		return nil
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://integrate.api.nvidia.com/v1"
	}
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(baseURL),
	)
	return &NvidiaProvider{client: client, cfg: cfg}
}

func (p *NvidiaProvider) Name() string { return "nvidia" }

func (p *NvidiaProvider) Available(ctx context.Context) bool {
	if p == nil || p.cfg.APIKey == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5_000_000_000) // NIM can be slow to respond
	defer cancel()
	_, err := p.client.Models.List(ctx)
	return err == nil
}

func (p *NvidiaProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
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

// Embed uses NVIDIA NIM's embedding endpoint.
func (p *NvidiaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	model := p.cfg.ModelEmbed
	if model == "" {
		model = "nvidia/nv-embedqa-e5-v5"
	}

	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(model),
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
	})
	if err != nil {
		return nil, fmt.Errorf("nvidia embed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("nvidia embed: empty response")
	}

	raw := resp.Data[0].Embedding
	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v)
	}
	return out, nil
}

func (p *NvidiaProvider) selectModel(req llm.Request) string {
	if p.cfg.ModelHeal != "" {
		return p.cfg.ModelHeal
	}
	return "nvidia/nemotron-3-super-120b-a12b"
}

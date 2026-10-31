package providers

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"github.com/sagar0163/nebula/internal/llm"
)

// GroqConfig holds configuration for the Groq provider.
type GroqConfig struct {
	APIKey        string
	ModelDiagnose string
	ModelHeal     string
	ModelLearn    string
}

// GroqProvider implements llm.Provider using the Groq API
// (OpenAI-compatible endpoint).
type GroqProvider struct {
	client openai.Client
	cfg    GroqConfig
}

// NewGroq creates a GroqProvider. Returns nil if APIKey is empty.
func NewGroq(cfg GroqConfig) *GroqProvider {
	if cfg.APIKey == "" {
		return nil
	}
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL("https://api.groq.com/openai/v1"),
	)
	return &GroqProvider{client: client, cfg: cfg}
}

func (p *GroqProvider) Name() string { return "groq" }

func (p *GroqProvider) Available(ctx context.Context) bool {
	if p == nil || p.cfg.APIKey == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3_000_000_000) // 3s
	defer cancel()
	_, err := p.client.Models.List(ctx)
	return err == nil
}

func (p *GroqProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
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

// Embed is not supported by Groq.
func (p *GroqProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("groq: embedding not supported")
}

func (p *GroqProvider) selectModel(req llm.Request) string {
	if req.Model != "" {
		return req.Model
	}
	switch req.Workload {
	case llm.WorkloadDiagnose:
		if p.cfg.ModelDiagnose != "" {
			return p.cfg.ModelDiagnose
		}
		return "llama-3.1-8b-instant"
	case llm.WorkloadLearn:
		if p.cfg.ModelLearn != "" {
			return p.cfg.ModelLearn
		}
		// fallthrough to heal
	}
	if p.cfg.ModelHeal != "" {
		return p.cfg.ModelHeal
	}
	return "llama-3.3-70b-versatile"
}

// Model reports the model this provider uses for a workload tier. It is the
// same resolution Complete performs, exposed so callers that need to label or
// price a run do not have to duplicate the defaults.
func (p *GroqProvider) Model(w llm.Workload) string {
	return p.selectModel(llm.Request{Workload: w})
}

// buildOpenAIMessages converts llm.Request messages to openai-go param slice.
// It handles the full OpenAI-compatible message schema:
//   - role "system"    → SystemMessage
//   - role "user"      → UserMessage (plain text)
//   - role "assistant" → AssistantMessage; if ToolCalls is non-empty the
//                        tool_calls array is populated with function call info
//   - role "tool"      → ToolMessage carrying tool_call_id and the result text
func buildOpenAIMessages(req llm.Request) []openai.ChatCompletionMessageParamUnion {
	var msgs []openai.ChatCompletionMessageParamUnion
	if req.SystemPrompt != "" {
		msgs = append(msgs, openai.SystemMessage(req.SystemPrompt))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) > 0 {
				// Build a native assistant message with tool_calls populated.
				var tcs []openai.ChatCompletionMessageToolCallParam
				for _, tc := range m.ToolCalls {
					name := tc.Function.Name
					args := tc.Function.Arguments
					// Fall back to deprecated flat fields for backward compat.
					if name == "" {
						name = tc.Name
					}
					if args == "" {
						args = tc.Arguments
					}
					tcs = append(tcs, openai.ChatCompletionMessageToolCallParam{
						ID:   tc.ID,
						Type: "function",
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      name,
							Arguments: args,
						},
					})
				}
				param := openai.ChatCompletionAssistantMessageParam{
					Role:      "assistant",
					Content:   openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(m.Content)},
					ToolCalls: tcs,
				}
				msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfAssistant: &param})
			} else {
				msgs = append(msgs, openai.AssistantMessage(m.Content))
			}
		case "tool":
			// Tool-result message: role "tool" with tool_call_id.
			name := m.Name
			if name == "" {
				name = "tool"
			}
			param := openai.ChatCompletionToolMessageParam{
				Role:       "tool",
				ToolCallID: m.ToolCallID,
				Content:    openai.ChatCompletionToolMessageParamContentUnion{OfString: openai.String(m.Content)},
			}
			msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfTool: &param})
		default:
			msgs = append(msgs, openai.UserMessage(m.Content))
		}
	}
	return msgs
}

// applyResponseFormat constrains an OpenAI-compatible chat completion to a JSON
// object. Only "json" is mapped; any other value (including empty) leaves the
// completion unconstrained. Callers must still tolerate free-form output since
// a provider may reject or ignore the hint.
func applyResponseFormat(params *openai.ChatCompletionNewParams, req llm.Request) {
	if req.ResponseFormat != "json" {
		return
	}
	params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONObject: &shared.ResponseFormatJSONObjectParam{Type: "json_object"},
	}
}

func (p *GroqProvider) ModelName(w llm.Workload) string {
	switch w {
	case llm.WorkloadHeal:
		return p.cfg.ModelHeal
	case llm.WorkloadLearn:
		return p.cfg.ModelLearn
	default:
		return p.cfg.ModelDiagnose
	}
}

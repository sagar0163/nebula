package llm

import "context"

// Token is a single streaming token from an LLM.
type Token struct {
	Text    string
	IsLast  bool
	Err     error
}

// Usage tracks token consumption for a completion.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Model        string
	Provider     string
}

// Request is a normalized LLM completion request.
type Request struct {
	Workload     Workload
	SystemPrompt string
	Messages     []Message
	Tools        []Tool
	MaxTokens    int
	Temperature  float64
	// ResponseFormat hints to the provider to return structured output.
	// "json" requests JSON-object output where supported; empty means free-form.
	ResponseFormat string
	// Model, when non-empty, overrides the provider's workload-tier default model.
	Model string
}

// ContentPart is one element of a structured message content array.
// Role "user" messages may carry a mix of text and tool-result parts;
// role "assistant" messages with tool calls carry only text (the reasoning)
// alongside ToolCalls, not inside Content.
type ContentPart struct {
	Type       string `json:"type"`                  // "text" | "tool_result"
	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"` // for type "tool_result"
	Content    string `json:"content,omitempty"`      // for type "tool_result"
}

// Message is a single turn in the conversation.
//
// For plain text turns set Role and Content only.
// For assistant turns that invoke tools, set Role "assistant", Content
// (reasoning text, may be empty), and ToolCalls.
// For tool-result turns set Role "tool", ToolCallID, and Content.
// Name is used for role "tool" to label which function returned the result.
type Message struct {
	Role       string     `json:"role"`                  // "user" | "assistant" | "tool"
	Content    string     `json:"content"`               // plain-text content
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`  // set by assistant when calling tools
	ToolCallID string     `json:"tool_call_id,omitempty"` // set on role "tool" replies
	Name       string     `json:"name,omitempty"`         // optional function name for role "tool"
}

// Tool is a function the LLM can call.
type Tool struct {
	Name        string
	Description string
	Parameters  any // JSON schema
}

// FunctionCall holds the name and arguments of a tool invocation.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON
}

// ToolCall is a function call the LLM requested.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`     // always "function"
	Function FunctionCall `json:"function"`

	// Deprecated fields kept for callers that still use the flat form.
	// New code should use Function.Name and Function.Arguments instead.
	Name      string `json:"-"`
	Arguments string `json:"-"` // raw JSON
}

// Provider is the interface every LLM backend must satisfy.
// Implementations live in internal/llm/providers/.
type Provider interface {
	// Name returns the provider identifier (e.g. "gemini", "groq", "ollama").
	Name() string

	// ModelName returns the specific model string used for the given workload.
	ModelName(w Workload) string

	// Available reports whether the provider is reachable (has key, model exists).
	Available(ctx context.Context) bool

	// Complete streams tokens for the given request.
	// The returned channel is closed when the stream ends or ctx is cancelled.
	Complete(ctx context.Context, req Request) (<-chan Token, error)

	// Embed returns a vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Workload hints to the router which model tier to use.
type Workload int

const (
	WorkloadDiagnose  Workload = iota // fast, cheap — error analysis
	WorkloadHeal                      // mid-tier — fix suggestions
	WorkloadLearn                     // strong — workflow pattern learning
	WorkloadEmbed                     // embed model
)

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
}

// Message is a single turn in the conversation.
type Message struct {
	Role    string // "user" | "assistant" | "tool"
	Content string
}

// Tool is a function the LLM can call.
type Tool struct {
	Name        string
	Description string
	Parameters  any // JSON schema
}

// ToolCall is a function call the LLM requested.
type ToolCall struct {
	ID         string
	Name       string
	Arguments  string // raw JSON
}

// Provider is the interface every LLM backend must satisfy.
// Implementations live in internal/llm/providers/.
type Provider interface {
	// Name returns the provider identifier (e.g. "gemini", "groq", "ollama").
	Name() string

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

package shared

import (
	"context"
	"time"
)

// Phase represents a harness execution phase
type Phase string

const (
	PhasePlanning    Phase = "planning"
	PhaseExecution   Phase = "execution"
	PhaseVerification Phase = "verification"
	PhaseCritique    Phase = "critique"
)

// ToolCall represents a structured tool invocation
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	RequestID string                 `json:"request_id"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	CallID   string      `json:"call_id"`
	Success  bool        `json:"success"`
	Output   interface{} `json:"output"`
	Error    string      `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Step represents a single execution step
type Step struct {
	Phase      Phase       `json:"phase"`
	ToolCalls  []ToolCall  `json:"tool_calls"`
	Results    []ToolResult `json:"results"`
	Context    string      `json:"context"`
	Timestamp  time.Time   `json:"timestamp"`
	TokensUsed int         `json:"tokens_used"`
}

// Trajectory records the complete execution history
type Trajectory struct {
	InstanceID string    `json:"instance_id"`
	Steps      []Step    `json:"steps"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	TotalTokens int      `json:"total_tokens"`
	Status     string    `json:"status"` // "running", "resolved", "failed", "error"
	Error      string    `json:"error,omitempty"`
	Patch      string    `json:"patch,omitempty"`
}

// Agent defines the interface for all agents
type Agent interface {
	Name() string
	Phase() Phase
	Execute(ctx context.Context, input AgentInput) (AgentOutput, error)
}

// AgentInput is the input to an agent
type AgentInput struct {
	Context    string                 `json:"context"`
	Tools      []ToolDefinition       `json:"tools"`
	Memory     *MemorySnapshot        `json:"memory,omitempty"`
	Budget     TokenBudget            `json:"budget"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// AgentOutput is the output from an agent
type AgentOutput struct {
	ToolCalls []ToolCall `json:"tool_calls"`
	Context   string     `json:"context"`   // Updated context for next agent
	Done      bool       `json:"done"`      // Whether this phase is complete
	Error     string     `json:"error,omitempty"`
}

// ToolDefinition describes a tool for the LLM
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]ParameterDef `json:"parameters"`
}

// ParameterDef describes a tool parameter
type ParameterDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// TokenBudget allocates tokens per phase
type TokenBudget struct {
	Total     int            `json:"total"`
	PerPhase  map[Phase]int  `json:"per_phase"`
	Used      int            `json:"used"`
	Reserved  int            `json:"reserved"`
}

// MemorySnapshot is a point-in-time view of memory
type MemorySnapshot struct {
	GlobalSummary   string            `json:"global_summary"`
	WorkingContext  string            `json:"working_context"`
	RelevantFiles   []FileReference   `json:"relevant_files"`
	VectorMatches   []VectorMatch     `json:"vector_matches"`
	PastFixes       []PastFix         `json:"past_fixes"`
}

// FileReference points to a file with context
type FileReference struct {
	Path     string `json:"path"`
	Summary  string `json:"summary"`
	RelevantLines [2]int `json:"relevant_lines,omitempty"` // start, end
	Symbols  []string `json:"symbols,omitempty"`
}

// VectorMatch is a semantic search result
type VectorMatch struct {
	Content   string  `json:"content"`
	File      string  `json:"file"`
	Score     float64 `json:"score"`
	Metadata  map[string]string `json:"metadata"`
}

// PastFix records a successful fix pattern
type PastFix struct {
	IssuePattern string `json:"issue_pattern"`
	FixSummary   string `json:"fix_summary"`
	Patch        string `json:"patch"`
	Files        []string `json:"files"`
	SuccessRate  float64 `json:"success_rate"`
	Timestamp    time.Time `json:"timestamp"`
}

// ModelConfig specifies model selection per phase
type ModelConfig struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens  int     `json:"max_tokens"`
}

// ModelRouting maps phases to model configs
type ModelRouting map[Phase]ModelConfig

// DefaultModelRouting provides sensible defaults
var DefaultModelRouting = ModelRouting{
	PhasePlanning:    {Provider: "anthropic", Model: "claude-3-5-sonnet", Temperature: 0.1, MaxTokens: 4000},
	PhaseExecution:   {Provider: "anthropic", Model: "claude-3-5-sonnet", Temperature: 0.1, MaxTokens: 8000},
	PhaseVerification: {Provider: "openai", Model: "gpt-4o-mini", Temperature: 0.0, MaxTokens: 2000},
	PhaseCritique:    {Provider: "anthropic", Model: "claude-3-5-sonnet", Temperature: 0.1, MaxTokens: 4000},
}
package tools

import (
	"context"
	"fmt"
	"strings"
)

// ToolDef represents a tool description for the LLM.
type ToolDef struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  map[string]string `json:"parameters"`
}

// Tool represents a single action the agent can take.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]string
	Execute(ctx context.Context, input map[string]string) (string, error)
}

// Registry manages available tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

// Descriptions returns definitions of all registered tools for LLM prompts.
func (r *Registry) Descriptions() []ToolDef {
	var defs []ToolDef
	for _, t := range r.tools {
		defs = append(defs, ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

// FormatPrompt generates a string description of tools for LLM prompts.
func (r *Registry) FormatPrompt() string {
	var builder strings.Builder
	for _, def := range r.Descriptions() {
		builder.WriteString(fmt.Sprintf("- %s: %s\n", def.Name, def.Description))
		builder.WriteString("  Inputs:\n")
		for k, v := range def.Parameters {
			builder.WriteString(fmt.Sprintf("    %s: %s\n", k, v))
		}
	}
	return builder.String()
}

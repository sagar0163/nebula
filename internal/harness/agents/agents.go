package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagar0163/nebula/internal/harness/shared"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/harness/tools"
	"os/exec"
)

// PlannerAgent analyzes issues and creates execution plans
type PlannerAgent struct {
	router *llm.Router
	model  shared.ModelConfig
}

// NewPlannerAgent creates a new planner agent
func NewPlannerAgent(router *llm.Router, model shared.ModelConfig) *PlannerAgent {
	return &PlannerAgent{router: router, model: model}
}

func (p *PlannerAgent) Name() string { return "Planner" }
func (p *PlannerAgent) Phase() shared.Phase { return shared.PhasePlanning }

func (p *PlannerAgent) Execute(ctx context.Context, input shared.AgentInput) (shared.AgentOutput, error) {
	prompt := p.buildPrompt(input)
	
	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      p.model.MaxTokens,
		Temperature:    p.model.Temperature,
		ResponseFormat: "json",
	}

	tokens, err := p.router.Complete(ctx, llm.WorkloadDiagnose, req)
	if err != nil {
		return shared.AgentOutput{}, err
	}

	var builder strings.Builder
	for token := range tokens {
		if token.Err != nil {
			return shared.AgentOutput{}, token.Err
		}
		builder.WriteString(token.Text)
	}

	response := builder.String()
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return shared.AgentOutput{}, fmt.Errorf("failed to extract JSON from planner response")
	}

	var plan Plan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return shared.AgentOutput{}, fmt.Errorf("failed to parse plan: %w", err)
	}

	// Convert plan to tool calls
	toolCalls := []shared.ToolCall{}
	for i, step := range plan.Steps {
		toolCalls = append(toolCalls, shared.ToolCall{
			ID:        fmt.Sprintf("plan_step_%d", i),
			Name:      "plan_step",
			Arguments: map[string]interface{}{"step": step},
			RequestID: fmt.Sprintf("plan_%d", i),
		})
	}

	return shared.AgentOutput{
		ToolCalls: toolCalls,
		Context:   fmt.Sprintf("Plan created with %d steps: %s", len(plan.Steps), plan.Summary),
		Done:      true,
	}, nil
}

type Plan struct {
	Steps   []PlanStep `json:"steps"`
	Summary string     `json:"summary"`
}

type PlanStep struct {
	File        string   `json:"file"`
	Action      string   `json:"action"` // "read", "edit", "create"
	Description string   `json:"description"`
	TestCommand string   `json:"test_command,omitempty"`
	Symbols     []string `json:"symbols,omitempty"`
}

func (p *PlannerAgent) buildPrompt(input shared.AgentInput) string {
	var b strings.Builder
	b.WriteString("You are a senior engineer analyzing a GitHub issue.\n\n")
	b.WriteString("Issue: " + input.Context + "\n\n")
	
	if len(input.Memory.RelevantFiles) > 0 {
		b.WriteString("Relevant files:\n")
		for _, f := range input.Memory.RelevantFiles {
			b.WriteString(fmt.Sprintf("- %s: %s\n", f.Path, f.Summary))
		}
		b.WriteString("\n")
	}
	
	if len(input.Memory.PastFixes) > 0 {
		b.WriteString("Similar past fixes:\n")
		for _, fix := range input.Memory.PastFixes {
			b.WriteString(fmt.Sprintf("- %s: %s\n", fix.IssuePattern, fix.FixSummary))
		}
		b.WriteString("\n")
	}

	b.WriteString("Create a plan to fix this issue. Output ONLY a JSON object:\n")
	b.WriteString(`{
  "steps": [
    {"file": "path/to/file.py", "action": "read", "description": "Understand current implementation"},
    {"file": "path/to/file.py", "action": "edit", "description": "Fix the bug", "test_command": "pytest test_file.py -v"}
  ],
  "summary": "Brief summary of the approach"
}`)

	return b.String()
}

// ExecutorAgent implements code changes
type ExecutorAgent struct {
	router   *llm.Router
	model    shared.ModelConfig
	registry *tools.Registry
}

func NewExecutorAgent(router *llm.Router, model shared.ModelConfig, registry *tools.Registry) *ExecutorAgent {
	return &ExecutorAgent{router: router, model: model, registry: registry}
}

func (e *ExecutorAgent) Name() string { return "Executor" }
func (e *ExecutorAgent) Phase() shared.Phase { return shared.PhaseExecution }

func (e *ExecutorAgent) Execute(ctx context.Context, input shared.AgentInput) (shared.AgentOutput, error) {
	fmt.Println("[Executor] Generating and applying fixes based on plan...")
	
	// Mini ReAct loop for executor to read files, write fixes, and run linters
	loopContext := input.Context
	
	// Extract the workspace from metadata if available, otherwise assume current dir
	workDir := "."
	if dir, ok := input.Metadata["workDir"].(string); ok && dir != "" {
		workDir = dir
	}
	
	// Create a branch or stash here in a real implementation (TASK-080)
	// For now, we'll just run the loop
	
	for i := 0; i < 5; i++ {
		req := llm.Request{
			SystemPrompt: "You are an expert coder. Follow the plan. Use read_file to analyze, write_file to apply fixes, shell to run linters. Output a JSON object with 'tool' and 'arguments'. If done, output 'tool': 'DoneTool'.",
			Messages:       []llm.Message{{Role: "user", Content: loopContext}},
			MaxTokens:      e.model.MaxTokens,
			Temperature:    e.model.Temperature,
			ResponseFormat: "json",
		}
		
		ch, err := e.router.Complete(ctx, llm.WorkloadHeal, req)
		if err != nil {
			return shared.AgentOutput{}, err
		}
		
		var respBuilder strings.Builder
		for t := range ch {
			if t.Err != nil {
				return shared.AgentOutput{}, t.Err
			}
			respBuilder.WriteString(t.Text)
		}
		resp := respBuilder.String()
		
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
			loopContext += "\nError parsing JSON: " + err.Error() + ". Please output valid JSON only."
			continue
		}
		
		toolName, _ := parsed["tool"].(string)
		if toolName == "DoneTool" || toolName == "" {
			break
		}
		
		args, _ := parsed["arguments"].(map[string]interface{})
		
		t := e.registry.Get(toolName)
		if t == nil {
			loopContext += fmt.Sprintf("\nError: Tool %s not found.", toolName)
			continue
		}
		
		fmt.Printf("[Executor] Calling tool: %s\n", toolName)
		res, err := t.Execute(ctx, args)
		if err != nil {
			loopContext += fmt.Sprintf("\nTool %s failed: %v", toolName, err)
		} else {
			loopContext += fmt.Sprintf("\nTool %s success: %v", toolName, res)
		}
	}
	
	// Extract unified diff patch
	fmt.Println("[Executor] Extracting git diff...")
	cmd := exec.CommandContext(ctx, "git", "-C", workDir, "diff", "--no-color")
	out, err := cmd.CombinedOutput()
	if err != nil && cmd.ProcessState.ExitCode() != 0 && cmd.ProcessState.ExitCode() != 1 {
		return shared.AgentOutput{}, fmt.Errorf("git diff failed: %v, %s", err, string(out))
	}
	
	patch := string(out)
	if patch == "" {
		patch = "No changes were made."
	}
	
	return shared.AgentOutput{
		Context: patch,
		Done:    true,
	}, nil
}

// VerifierAgent runs tests and validates fixes
type VerifierAgent struct {
	router *llm.Router
	model  shared.ModelConfig
}

func NewVerifierAgent(router *llm.Router, model shared.ModelConfig) *VerifierAgent {
	return &VerifierAgent{router: router, model: model}
}

func (v *VerifierAgent) Name() string { return "Verifier" }
func (v *VerifierAgent) Phase() shared.Phase { return shared.PhaseVerification }

func (v *VerifierAgent) Execute(ctx context.Context, input shared.AgentInput) (shared.AgentOutput, error) {
	return shared.AgentOutput{
		ToolCalls: []shared.ToolCall{{
			ID:        "verify_1",
			Name:      "run_tests",
			Arguments: map[string]interface{}{"filter": "failing"},
			RequestID: "verify_1",
		}},
		Context: "Running failing tests first for fast feedback",
		Done:    false,
	}, nil
}

func extractJSON(text string) string {
	// First try: look for JSON in markdown code fences
	if strings.HasPrefix(text, "```") {
		if i := strings.Index(text[3:], "```"); i >= 0 {
			text = strings.TrimSpace(text[3 : 3+i])
			if after, ok := strings.CutPrefix(text, "json"); ok {
				text = strings.TrimSpace(after)
			}
		}
	}
	
	// If already looks like JSON, return as-is
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
		return text
	}
	
	// Try to find JSON object in text (between first { and last })
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	
	return ""
}
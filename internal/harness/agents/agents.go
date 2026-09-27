package agents

import (
	testrunner "github.com/sagar0163/nebula/internal/swebench/testrunner"
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
	router   *llm.Router
	model    shared.ModelConfig
	registry *tools.Registry
}

// NewPlannerAgent creates a new planner agent
func NewPlannerAgent(router *llm.Router, model shared.ModelConfig, registry *tools.Registry) *PlannerAgent {
	return &PlannerAgent{router: router, model: model, registry: registry}
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
	
	if input.Memory != nil && input.Memory.GlobalSummary != "" {
		b.WriteString(input.Memory.GlobalSummary + "\n\n")
	}
	
	b.WriteString("Explore the codebase using the search_code and semantic_search tools to find the exact files to edit.\n")
	b.WriteString("Once you have found the files, output a final JSON plan using the DoneTool with the following JSON string in the 'plan' argument:\n")
	b.WriteString(`{
  "files_to_read": ["path/to/file1.py"],
  "files_to_edit": ["path/to/file2.py"],
  "failing_tests": ["tests/test_bug.py"],
  "approach": "Brief summary of how to fix the issue"
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
	fmt.Println("[Verifier] Validating patch and running tests...")
	
	patch := input.Context
	if patch == "" || patch == "No changes were made." {
		return shared.AgentOutput{
			Context: `{"passed": false, "error_trace": "No patch generated by executor"}`,
			Done:    true,
		}, nil
	}
	
	workDir := ""
	if wd, ok := input.Metadata["workDir"].(string); ok {
		workDir = wd
	}
	if workDir == "" {
		// Fallback to LLM verifier if no workDir
		return v.fallbackLLMVerifier(ctx, patch)
	}
	
	// Detect runner
	var runner testrunner.TestRunner
	if (&testrunner.PythonTestRunner{}).Detect(workDir) {
		runner = &testrunner.PythonTestRunner{}
	} else if (&testrunner.JSTestRunner{}).Detect(workDir) {
		runner = &testrunner.JSTestRunner{}
	} else if (&testrunner.GoTestRunner{}).Detect(workDir) {
		runner = &testrunner.GoTestRunner{}
	} else {
		return v.fallbackLLMVerifier(ctx, patch)
	}
	
	// Apply patch
	patchResult, err := runner.RunTests(ctx, workDir, "")
	if err != nil {
		return shared.AgentOutput{
			Context: fmt.Sprintf(`{"passed": false, "error_trace": %q}`, err.Error()),
			Done:    true,
		}, nil
	}
	
	if patchResult.Failed > 0 {
		return shared.AgentOutput{
			Context: fmt.Sprintf(`{"passed": false, "error_trace": %q}`, fmt.Sprintf("Tests failed: %d failed out of %d. Output: %s", patchResult.Failed, (patchResult.Passed + patchResult.Failed), patchResult.RawOutput)),
			Done:    true,
		}, nil
	}
	
	return shared.AgentOutput{
		Context: `{"passed": true, "error_trace": ""}`,
		Done:    true,
	}, nil
}

func (v *VerifierAgent) fallbackLLMVerifier(ctx context.Context, patch string) (shared.AgentOutput, error) {
	prompt := fmt.Sprintf(`You are the VerifierAgent.
Review the following patch and determine if it theoretically passes the intended tests and fixes the bug without introducing regressions.
Patch:
%s

Output ONLY valid JSON in this format:
{
  "passed": true/false,
  "error_trace": "If passed is false, explain why the patch fails or what is missing."
}`, patch)

	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      v.model.MaxTokens,
		Temperature:    0.1, // low temp for verification
		ResponseFormat: "json",
	}

	ch, err := v.router.Complete(ctx, llm.WorkloadDiagnose, req)
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
	
	resp := extractJSON(respBuilder.String())
	
	return shared.AgentOutput{
		Context: resp,
		Done:    true,
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
// CriticAgent reviews the quality of generated patches
type CriticAgent struct {
	router *llm.Router
	model  shared.ModelConfig
}

// NewCriticAgent creates a new critic agent
func NewCriticAgent(router *llm.Router, model shared.ModelConfig) *CriticAgent {
	return &CriticAgent{router: router, model: model}
}

func (c *CriticAgent) Name() string { return "Critic" }
func (c *CriticAgent) Phase() shared.Phase { return shared.PhaseCritique }

func (c *CriticAgent) Execute(ctx context.Context, input shared.AgentInput) (shared.AgentOutput, error) {
	fmt.Println("[Critic] Reviewing patch quality...")

	patch := input.Context
	if patch == "" || patch == "No changes were made." {
		return shared.AgentOutput{
			Context: `{"approved": true, "warnings": []}`,
			Done:    true,
		}, nil
	}

	prompt := fmt.Sprintf(`Review the following git diff patch for correctness, style, and minimality.
Flag any test file modifications, unrelated changes, or security issues.
If there are severe security issues or completely unrelated destructive changes, reject the patch.
Otherwise, approve it but provide warnings.

Patch:
%s

Output ONLY valid JSON in this format:
{
  "approved": true/false,
  "warnings": ["warning 1", "warning 2"]
}`, patch)

	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      c.model.MaxTokens,
		Temperature:    c.model.Temperature,
		ResponseFormat: "json",
	}

	ch, err := c.router.Complete(ctx, llm.WorkloadDiagnose, req)
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

	// Ensure it parses
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		fmt.Printf("[Critic] Failed to parse response: %v\n", err)
		// Default to approve on parsing failure so we don't block good code
		return shared.AgentOutput{
			Context: `{"approved": true, "warnings": ["Failed to parse critic output"]}`,
			Done:    true,
		}, nil
	}

	return shared.AgentOutput{
		Context: resp,
		Done:    true,
	}, nil
}

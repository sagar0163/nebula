package agents

import (
	testrunner "github.com/sagar0163/nebula/internal/swebench/testrunner"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/harness/shared"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/harness/tools"
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

	tokens, err := p.router.CompleteWithConfig(ctx, llm.ModelConfig{Provider: p.model.Provider, Model: p.model.Model}, llm.WorkloadDiagnose, req)
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
	for i, f := range plan.FilesToRead {
		toolCalls = append(toolCalls, shared.ToolCall{
			ID:        fmt.Sprintf("plan_read_%d", i),
			Name:      "read_file",
			Arguments: map[string]interface{}{"path": f},
			RequestID: fmt.Sprintf("plan_read_%d", i),
		})
	}
	for i, f := range plan.FilesToEdit {
		toolCalls = append(toolCalls, shared.ToolCall{
			ID:        fmt.Sprintf("plan_edit_%d", i),
			Name:      "edit_file",
			Arguments: map[string]interface{}{"path": f, "approach": plan.ApproachString()},
			RequestID: fmt.Sprintf("plan_edit_%d", i),
		})
	}

	// Context is the plan as JSON so the orchestrator can extract failing_tests.
	ctxJSON, err := json.Marshal(plan)
	if err != nil {
		return shared.AgentOutput{}, fmt.Errorf("failed to encode plan: %w", err)
	}

	return shared.AgentOutput{
		ToolCalls: toolCalls,
		Context:   string(ctxJSON),
		Done:      true,
	}, nil
}

// Plan is the planner's output schema; it matches the JSON requested in buildPrompt.
type Plan struct {
	FilesToRead  []string `json:"files_to_read"`
	FilesToEdit  []string `json:"files_to_edit"`
	FailingTests []string `json:"failing_tests"`
	// Approach is RawMessage because LLMs sometimes return an object instead
	// of a plain string; use ApproachString to read it.
	Approach json.RawMessage `json:"approach"`
}

// ApproachString returns the approach as plain text whether the LLM sent a
// JSON string or an object.
func (p Plan) ApproachString() string {
	if len(p.Approach) == 0 {
		return ""
	}
	var approachStr string
	if err := json.Unmarshal(p.Approach, &approachStr); err != nil {
		approachStr = string(p.Approach)
	}
	return approachStr
}

func (p *PlannerAgent) buildPrompt(input shared.AgentInput) string {
	var b strings.Builder
	b.WriteString("You are a senior engineer analyzing a GitHub issue.\n\n")
	b.WriteString("Issue: " + input.Context + "\n\n")
	
	if input.Memory != nil && input.Memory.GlobalSummary != "" {
		b.WriteString(input.Memory.GlobalSummary + "\n\n")
	}
	
	b.WriteString("Based on the issue and repository layout, identify the files that need to be read and edited.\n")
	b.WriteString("Include any files you want examined in files_to_read.\n")
	b.WriteString("Output your final JSON answer and nothing else, using exactly this schema:\n")
	b.WriteString(`{
  "files_to_read": ["path/to/file1.py"],
  "files_to_edit": ["path/to/file2.py"],
  "failing_tests": ["tests/test_bug.py"],
  "approach": "Brief summary of how to fix the issue"
}`)
	b.WriteString("\n\"approach\" must be a plain JSON string, not an object or array.\n")

	return b.String()
}

// ExecutorAgent implements code changes
type ExecutorAgent struct {
	router      *llm.Router
	model       shared.ModelConfig
	registry    *tools.Registry
	agentRunner *agent.Agent
}

func NewExecutorAgent(router *llm.Router, model shared.ModelConfig, registry *tools.Registry) *ExecutorAgent {
	// DoGoal only needs the LLM router; no PTY harness or memory store is required.
	runner := agent.New(nil, router, nil, agent.Config{HistoryDepth: 10})
	return &ExecutorAgent{router: router, model: model, registry: registry, agentRunner: runner}
}

func (e *ExecutorAgent) Name() string { return "Executor" }
func (e *ExecutorAgent) Phase() shared.Phase { return shared.PhaseExecution }

func (e *ExecutorAgent) Execute(ctx context.Context, input shared.AgentInput) (shared.AgentOutput, error) {
	fmt.Println("[Executor] Generating and applying fixes based on plan...")

	// Extract the workspace from metadata if available, otherwise assume current dir
	workDir := "."
	if dir, ok := input.Metadata["workDir"].(string); ok && dir != "" {
		workDir = dir
	}

	// Prefer the original issue/goal over the plan summary.
	goal := ""
	if g, ok := input.Metadata["goal"].(string); ok && g != "" {
		goal = g
	}
	if goal == "" {
		goal = input.Context
	}

	// Run the goal executor in-process (issue #21: shelling out to `nebula do`
	// re-entered the harness and recursed forever).
	fmt.Printf("[Executor] Running DoGoal in %s\n", workDir)
	runCtx, cancel := context.WithTimeout(ctx, 900*time.Second) // 40 steps with a growing conversation
	defer cancel()

	// DoGoal's tools resolve relative paths against the process cwd, so
	// switch into workDir for the duration of the run.
	// TODO: os.Chdir is not concurrency-safe (process-wide). The swebench
	// runner caps Concurrency to 1 until tools set cmd.Dir = workDir instead.
	if workDir != "." {
		if prev, err := os.Getwd(); err == nil {
			if err := os.Chdir(workDir); err != nil {
				return shared.AgentOutput{}, fmt.Errorf("chdir %s: %w", workDir, err)
			}
			defer os.Chdir(prev)
		}
	}

	if runErr := e.agentRunner.DoGoal(runCtx, goal, agent.RunOptions{SkipPermissions: true}, os.Stdout); runErr != nil {
		fmt.Printf("[Executor] DoGoal exited with error: %v\n", runErr)
	} else {
		fmt.Println("[Executor] DoGoal completed.")
	}

	// Extract unified diff patch
	fmt.Println("[Executor] Extracting git diff...")
	// Stage everything (incl. new untracked files) so the diff captures them,
	// then unstage so the index isn't left permanently modified.
	exec.CommandContext(ctx, "git", "-C", workDir, "add", "-A", "--", ".", ":(exclude).nebula").Run()
	diffCmd := exec.CommandContext(ctx, "git", "-C", workDir, "diff", "--cached", "--no-color")
	diffOut, diffErr := diffCmd.CombinedOutput()
	exec.CommandContext(ctx, "git", "-C", workDir, "reset", "-q").Run()
	if diffErr != nil && diffCmd.ProcessState != nil && diffCmd.ProcessState.ExitCode() != 0 && diffCmd.ProcessState.ExitCode() != 1 {
		return shared.AgentOutput{}, fmt.Errorf("git diff failed: %v, %s", diffErr, string(diffOut))
	}

	patch := string(diffOut)
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
	
	// Only run the failing tests; running the full suite with -x lets
	// unrelated failures mask a real fix.
	var failingTests []string
	if ft, ok := input.Metadata["failingTests"]; ok && ft != nil {
		failingTests = toStringSlice(ft)
	}
	filter := strings.Join(failingTests, " ")
	if len(failingTests) == 0 && filter == "" {
		return shared.AgentOutput{
			Context: `{"passed": false, "error_trace": "No failing tests specified — cannot verify"}`,
			Done:    true,
		}, nil
	}

	// Apply patch
	patchResult, err := runner.RunTests(ctx, workDir, filter)
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
	prompt := fmt.Sprintf(`You are reviewing a code patch.
Review the following patch and determine if it theoretically passes the intended tests and fixes the bug without introducing regressions.
Patch:
%s

Do not call any tools. Output your final JSON answer only, in this format:
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

	ch, err := v.router.CompleteWithConfig(ctx, llm.ModelConfig{Provider: v.model.Provider, Model: v.model.Model}, llm.WorkloadDiagnose, req)
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

// toStringSlice normalizes a metadata value into a []string, handling both
// []string and the []interface{} form produced by JSON decoding.
func toStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if val == "" {
			return nil
		}
		return strings.Fields(val)
	default:
		return nil
	}
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

Do not call any tools. Output your final JSON answer only, in this format:
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

	ch, err := c.router.CompleteWithConfig(ctx, llm.ModelConfig{Provider: c.model.Provider, Model: c.model.Model}, llm.WorkloadDiagnose, req)
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

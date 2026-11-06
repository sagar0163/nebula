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

	// Include the shared tool registry so the planner knows which tools are
	// available to downstream agents (issue #29).
	if len(input.Tools) > 0 {
		b.WriteString("Available tools for downstream agents:\n")
		for _, t := range input.Tools {
			b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
		}
		b.WriteString("\n")
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
	// lastSession is the conversation from the most recent Execute call, so a
	// retry can resume it (Metadata["resume"]=true) instead of starting cold.
	lastSession *agent.GoalSession
}

// LastSession returns the executor conversation from the most recent run.
func (e *ExecutorAgent) LastSession() *agent.GoalSession { return e.lastSession }

// ResetSession discards the stored conversation so the next run starts fresh.
func (e *ExecutorAgent) ResetSession() { e.lastSession = nil }

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
	// The orchestrator sizes the per-attempt budget to fit the instance
	// timeout (issue #44); 900s is only the standalone fallback.
	attemptTimeout := 900 * time.Second
	if t, ok := input.Metadata["timeout"].(time.Duration); ok && t > 0 {
		attemptTimeout = t
	}
	runCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	// Route every executor LLM call through the execution-phase model (issue #39).
	// WorkDir is passed through RunOptions so tools resolve paths against
	// workDir using cmd.Dir instead of os.Chdir (issue #65). This removes
	// the process-wide CWD change that blocked parallel execution.
	execModel := e.model
	opts := agent.RunOptions{SkipPermissions: true, ModelConfig: &execModel, WorkDir: workDir}
	if tc, ok := input.Metadata["testCmd"].(string); ok {
		opts.TestCmd = tc
	}
	if ft, ok := input.Metadata["failingTests"]; ok {
		opts.FailingTests = toStringSlice(ft)
	}
	var session *agent.GoalSession
	if resume, _ := input.Metadata["resume"].(bool); resume && e.lastSession != nil {
		session = e.lastSession
		fmt.Printf("[Executor] Resuming previous conversation (%d messages, %d edits so far)\n", len(session.Messages), session.EditsMade)
	}
	newSession, runErr := e.agentRunner.DoGoalSession(runCtx, goal, opts, os.Stdout, session)
	e.lastSession = newSession
	if runErr != nil {
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
		msg := "No patch generated by executor: no source file was modified, so the failing tests were not run."
		if ft := toStringSlice(input.Metadata["failingTests"]); len(ft) > 0 {
			msg += " Tests that must pass: " + strings.Join(ft, ", ")
		}
		b, _ := json.Marshal(map[string]interface{}{"passed": false, "error_trace": msg})
		return shared.AgentOutput{
			Context: string(b),
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
			Context: verifierFailure(patchResult),
			Done:    true,
		}, nil
	}
	
	return shared.AgentOutput{
		Context: `{"passed": true, "error_trace": ""}`,
		Done:    true,
	}, nil
}

// verifierFailure builds the failure JSON: command, failed test ids with their
// messages/locations, then the tail of raw output (which holds pytest's
// assertion errors and tracebacks with file:line). Uses json.Marshal since
// %q produces Go escapes that are not always valid JSON.
func verifierFailure(r *testrunner.TestResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tests failed: %d failed out of %d.\n", r.Failed, r.Passed+r.Failed)
	if r.Command != "" {
		fmt.Fprintf(&b, "Command: %s\n", r.Command)
	}
	for _, ft := range r.FailedTests {
		loc := ft.File
		if ft.Line > 0 {
			loc = fmt.Sprintf("%s:%d", ft.File, ft.Line)
		}
		fmt.Fprintf(&b, "FAILED %s", ft.Name)
		if loc != "" {
			fmt.Fprintf(&b, " (%s)", loc)
		}
		if ft.Error != "" {
			fmt.Fprintf(&b, ": %s", ft.Error)
		}
		b.WriteString("\n")
	}
	out := r.RawOutput
	if len(out) > 6000 {
		out = "...(truncated)...\n" + out[len(out)-6000:]
	}
	b.WriteString("Output:\n" + out)
	j, _ := json.Marshal(map[string]interface{}{"passed": false, "error_trace": b.String()})
	return string(j)
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
	
	rawResp := respBuilder.String()
	resp := extractJSON(rawResp)
	// If extraction failed, build a fallback so the orchestrator receives a
	// non-empty error trace instead of silently feeding an empty string back
	// to the executor (issue #66).
	if resp == "" {
		snippet := rawResp
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		j, _ := json.Marshal(map[string]interface{}{
			"passed":      false,
			"error_trace": "LLM verifier returned unparseable output. Raw response (first 500 chars): " + snippet,
		})
		resp = string(j)
	}

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

	text = strings.TrimSpace(text)

	// Use a bracket counter to find the first complete top-level JSON object.
	// This avoids returning truncated JSON when there is text after the first
	// object (e.g. "{"a":1} extra text {"b":2}").
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inStr {
			escape = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}
// CriticAgent reviews the quality of generated patches using deterministic checks.
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

// testFilePatterns are glob-style substrings that indicate a test file path.
var testFilePatterns = []string{
	"test_", "_test.py", "/tests/", "/testing/", "conftest.py",
}

// debugArtifacts are line prefixes/substrings that indicate debug code left in.
var debugArtifacts = []string{
	"import pdb; pdb.set_trace()",
	"breakpoint()",
	`console.log(`,
	`print(f"DEBUG`,
}

// DoCritique runs deterministic patch quality checks and returns (approved, reason).
// It is exported so tests can call it directly without an Execute round-trip.
func DoCritique(patch string) (approved bool, reason string) {
	// Check 1: patch must be non-empty
	diffLines := 0
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			diffLines++
		}
	}
	if diffLines == 0 {
		return false, "patch is empty: no diff lines found"
	}

	// Check 2: no test file modifications
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "diff --git") {
			lower := strings.ToLower(line)
			for _, pat := range testFilePatterns {
				if strings.Contains(lower, pat) {
					return false, fmt.Sprintf("patch modifies a test file (matched pattern %q in: %s)", pat, line)
				}
			}
		}
	}

	// Check 3: no binary file changes
	for _, line := range strings.Split(patch, "\n") {
		if strings.Contains(line, "Binary files") && strings.Contains(line, "differ") {
			return false, fmt.Sprintf("patch modifies a binary file: %s", line)
		}
	}

	// Check 4: warn (but don't reject) if patch is >500 lines
	warnings := []string{}
	totalLines := len(strings.Split(patch, "\n"))
	if totalLines > 500 {
		warnings = append(warnings, fmt.Sprintf("patch is large (%d lines); review carefully", totalLines))
	}

	// Check 5: no debug artifacts in added lines
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		for _, artifact := range debugArtifacts {
			if strings.Contains(line, artifact) {
				return false, fmt.Sprintf("patch adds debug artifact %q", artifact)
			}
		}
	}

	return true, strings.Join(warnings, "; ")
}

func (c *CriticAgent) Execute(ctx context.Context, input shared.AgentInput) (shared.AgentOutput, error) {
	fmt.Println("[Critic] Reviewing patch quality (deterministic)...")

	patch := input.Context
	if patch == "" || patch == "No changes were made." {
		return shared.AgentOutput{
			Context: `{"approved": true, "warnings": []}`,
			Done:    true,
		}, nil
	}

	approved, reason := DoCritique(patch)

	warnings := []string{}
	if reason != "" {
		warnings = append(warnings, reason)
	}
	warningsJSON, _ := json.Marshal(warnings)
	resp := fmt.Sprintf(`{"approved": %v, "warnings": %s}`, approved, string(warningsJSON))

	if approved {
		fmt.Printf("[Critic] Patch approved. Warnings: %v\n", warnings)
	} else {
		fmt.Printf("[Critic] Patch rejected: %s\n", reason)
	}

	return shared.AgentOutput{
		Context: resp,
		Done:    true,
	}, nil
}

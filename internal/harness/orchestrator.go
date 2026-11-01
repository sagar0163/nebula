package harness

import (
	"os/exec"
	"os"
	"path/filepath"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
	"strings"

	"github.com/sagar0163/nebula/internal/harness/agents"
	"github.com/sagar0163/nebula/internal/harness/contextpkg"
	"github.com/sagar0163/nebula/internal/harness/shared"
	"github.com/sagar0163/nebula/internal/harness/tools"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/swebench/localize"
	"github.com/sagar0163/nebula/internal/swebench/testrunner"
	import_mem "github.com/sagar0163/nebula/internal/memory"
)

// Orchestrator coordinates the multi-agent harness
type Orchestrator struct {
	router       *llm.Router
	modelRouting shared.ModelRouting
	contextMgr   *contextpkg.ContextManager
	toolRegistry *tools.Registry
	
	planner   *agents.PlannerAgent
	executor  *agents.ExecutorAgent
	verifier  *agents.VerifierAgent
	critic   *agents.CriticAgent
	memStore import_mem.Store
	
	maxSteps    int
	stepTimeout time.Duration
	MultiModel bool

	// instanceTimeout bounds the whole Run; executor attempts are sized to fit
	// inside it (issue #44).
	instanceTimeout time.Duration
	runStart        time.Time

	// issue is the original issue/goal text, kept so the executor can run the
	// real goal instead of only the plan summary.
	issue string

	// failingTests are the FAIL_TO_PASS test identifiers the verifier should run.
	failingTests []string
	// testPatch is a patch adding/updating the failing tests (SWE-bench test_patch).
	testPatch string
}

// SetTestContext supplies the FAIL_TO_PASS tests and the test patch for the
// next Run. Either may be empty.
func (o *Orchestrator) SetTestContext(failingTests []string, testPatch string) {
	o.failingTests = failingTests
	o.testPatch = testPatch
}

// NewOrchestrator creates a new harness orchestrator
func NewOrchestrator(router *llm.Router, routing shared.ModelRouting, vectorStore contextpkg.VectorStore) *Orchestrator {
	// Use default routing if none provided
	if routing == nil {
		routing = shared.DefaultModelRouting
	}
	
	o := &Orchestrator{
		router:        router,
		modelRouting:  routing,
		contextMgr:    contextpkg.NewContextManager(8000, nil),
		toolRegistry:  tools.NewDefaultRegistry(""),
		maxSteps:      20,
		stepTimeout:   5 * time.Minute,
		instanceTimeout: 25 * time.Minute,
	}
	
	// Initialize agents with appropriate models
	o.planner = agents.NewPlannerAgent(router, routing[shared.PhasePlanning], o.toolRegistry)
	o.executor = agents.NewExecutorAgent(router, routing[shared.PhaseExecution], o.toolRegistry)
	o.verifier = agents.NewVerifierAgent(router, routing[shared.PhaseVerification])
	o.critic = agents.NewCriticAgent(router, routing[shared.PhaseCritique])
	
	// Keep the memory DB out of the target repo (e.g. /testbed).
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dbPath := filepath.Join(cacheDir, "nebula", "harness.db")
	_ = os.MkdirAll(filepath.Dir(dbPath), 0755)
	if store, err := import_mem.New(dbPath); err == nil {
		o.memStore = store
	}
	
	
	return o
}

// Run executes the full harness pipeline on an issue
func (o *Orchestrator) Run(ctx context.Context, issue string, workDir string) (*shared.Trajectory, error) {
	o.issue = issue
	o.runStart = time.Now()
	traj, err := loadTrajectory(workDir)
	if err != nil {
		fmt.Printf("Warning: failed to load existing trajectory: %v\n", err)
	}
	
	if traj == nil {
		traj = &shared.Trajectory{
			InstanceID: fmt.Sprintf("issue-%d", time.Now().Unix()),
			StartTime:  time.Now(),
			Status:     "running",
		}
	} else if traj.Status != "running" {
		// Only an interrupted (running) trajectory is resumable; anything else
		// is stale and must not block a fresh run.
		fmt.Printf("Discarding stale trajectory with status %q\n", traj.Status)
		if err := deleteTrajectory(workDir); err != nil {
			fmt.Printf("Warning: failed to delete stale trajectory: %v\n", err)
		}
		traj = &shared.Trajectory{
			InstanceID: fmt.Sprintf("issue-%d", time.Now().Unix()),
			StartTime:  time.Now(),
			Status:     "running",
		}
	} else {
		// NOTE (issue #45): this is not a true resume. Planning and execution are
		// re-run from scratch; only the stored step history is carried forward.
		fmt.Printf("Re-running from stored trajectory context (not a true resume): %d prior steps\n", len(traj.Steps))
	}

	// Apply the test patch so the failing tests exist; revert it on exit.
	if o.testPatch != "" {
		testFiles, err := applyTestPatch(ctx, workDir, o.testPatch)
		if err != nil {
			fmt.Printf("Warning: failed to apply test patch: %v\n", err)
		} else if len(testFiles) > 0 {
			defer revertFiles(workDir, testFiles)
		}
	}
	
	// Ensure working dir metadata
	defer saveTrajectory(workDir, traj) // Final save on exit
	
	logger, _ := NewStructuredLogger(workDir)
	if logger != nil {
		defer logger.Close()
	}
	
	// Initialize context
	o.contextMgr.SetWorkingContext("")
	
	// Phase 1: Planning
	fmt.Println("=== PHASE 1: PLANNING ===")
	planStart := time.Now()
	
	// Inject self-improving memory
	if o.memStore != nil {
		past, _ := o.memStore.SearchPastFixes(ctx, issue, 3)
		if len(past) > 0 {
			issue += "\n\n[NEBULA MEMORY - RELEVANT PAST FIXES]:\n"
			for i, p := range past {
				issue += fmt.Sprintf("Fix %d:\nIssue: %s\nPatch Summary: %s\n", i+1, p.IssuePattern, p.PatchSummary)
			}
		}
	}
	
	// Localization hints: traceback frames and symbols from the issue text.
	// Kept in locHints so the executor goal gets them too (issue #38).
	locHints := ""
	if hints := localize.Hints(o.issue); hints != "" {
		fmt.Print(hints)
		locHints = "\n\n[LOCALIZATION HINTS]\n" + hints
		issue += locHints
	}

	// Issue #55: localization v2 — run the failing tests once (pre-run) to
	// capture the actual error traceback, then extract repo-relative file:line
	// hints from /testbed/ paths in the output.
	if len(o.failingTests) == 0 {
		// Try to extract from issue text early so we can pre-run
		o.failingTests = extractFailingTests(o.issue)
	}
	// Pre-run localization disabled: testrunner.BuildTestCommand builds a
	// conda-wrapped command that hangs when conda is not in PATH inside the
	// executor process. Use locHints from the issue text (above) instead.
	// TODO(#55): re-enable once we use o.testCmd (the explicit --test-cmd flag).

	planCtx := o.buildPlanningContext(issue, workDir)
	planOutput, err := o.planner.Execute(ctx, planCtx)
	if logger != nil {
		logger.Log(LogEvent{
			InstanceID: traj.InstanceID,
			Phase:      string(shared.PhasePlanning),
			DurationMs: time.Since(planStart).Milliseconds(),
			Result:     planOutput.Context,
		})
	}
	if err != nil {
		traj.Status = "error"
		traj.Error = fmt.Sprintf("planning failed: %v", err)
		return traj, err
	}
	
	traj.Steps = append(traj.Steps, shared.Step{
		Phase:      shared.PhasePlanning,
		ToolCalls:  planOutput.ToolCalls,
		Context:    planOutput.Context,
		Timestamp:  time.Now(),
	})
	saveTrajectory(workDir, traj)
	
	// Pass metadata down
	planCtx.Metadata = map[string]interface{}{"workDir": workDir}

	// Merge failing tests discovered by the planner with any supplied ones.
	var planOut struct {
		FailingTests []string `json:"failing_tests"`
	}
	if json.Unmarshal([]byte(planOutput.Context), &planOut) == nil && len(o.failingTests) == 0 {
		o.failingTests = planOut.FailingTests
	}
	if len(o.failingTests) == 0 {
		o.failingTests = extractFailingTests(o.issue)
	}
	
	// Executor-Verifier Retry Loop
	execContextStr := planOutput.Context
	resolved := false
	prevPatch := ""
	lastTrace := ""
	testCmd := testrunner.BuildTestCommand(workDir, o.failingTests)
	if testCmd != "" {
		fmt.Printf("Executor test command: %s\n", testCmd)
	}
	o.executor.ResetSession()

	// Size each attempt so all attempts fit in the instance budget (issue #44).
	const maxAttempts = 3
	instanceTimeout := o.instanceTimeout
	if instanceTimeout <= 0 {
		instanceTimeout = 25 * time.Minute
	}
	instanceDeadline := o.runStart.Add(instanceTimeout)
	reserved := 2 * time.Minute // planning + verification + critique
	criticRejected := false

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		remainingAttempts := maxAttempts - attempt + 1
		perAttempt := (time.Until(instanceDeadline) - reserved) / time.Duration(remainingAttempts)
		if perAttempt < 2*time.Minute {
			perAttempt = 2 * time.Minute
		}
		fmt.Printf("Executor per-attempt timeout: %s\n", perAttempt)
		fmt.Printf("=== PHASE 2: EXECUTION (Attempt %d) ===\n", attempt)
		goal := o.issue + locHints + "\n\nPlanning context:\n" + execContextStr
		if attempt > 1 {
			// Issue #48: always resample (fresh conversation) on retry — never
			// resume the same conversation thread. Reset the repo to a clean
			// state so the new attempt starts from the original baseline.
			fmt.Printf("Attempt %d: discarding previous session and resetting repo for fresh resample.\n", attempt)
			exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "--", ".").Run()
			exec.CommandContext(ctx, "git", "-C", workDir, "clean", "-fd", "-e", ".nebula").Run()
			if o.testPatch != "" {
				if _, err := applyTestPatch(ctx, workDir, o.testPatch); err != nil {
					fmt.Printf("Warning: failed to re-apply test patch: %v\n", err)
				}
			}
			o.executor.ResetSession()
			// Build the fresh goal: original problem + prior error trace so the
			// new attempt knows what went wrong, but without any prior reasoning.
			if strings.TrimSpace(prevPatch) == "" || strings.TrimSpace(prevPatch) == "No changes were made." {
				goal = o.issue + locHints + "\n\nPrevious attempt made no changes. Start fresh and find the right file to fix.\n" + execContextStr
			} else {
				goal = o.issue + locHints + "\n\nPrevious attempt produced a patch that did not pass tests. Error:\n" + lastTrace +
					"\n\nStart fresh. Do not assume the previous approach was correct.\n\nPlanning context:\n" + execContextStr
			}
		}
		execCtx := o.buildExecutionContext(execContextStr)
		execCtx.Metadata = map[string]interface{}{
			"workDir":      workDir,
			"goal":         goal,
			"testCmd":      testCmd,
			"failingTests": o.failingTests,
			"resume":       false,
			"timeout":      perAttempt,
		}
		
		execStart := time.Now()
		var execOutput shared.AgentOutput
		var err error
		var skipCritic bool
		
		if o.MultiModel {
			fmt.Println("Running multi-model consensus check...")
			mmCtx := execCtx
			mmCtx.Metadata = make(map[string]interface{})
			for k, v := range execCtx.Metadata {
				mmCtx.Metadata[k] = v
			}
			mmCtx.Metadata["timeout"] = perAttempt / 2
			out1, err1 := o.executor.Execute(ctx, mmCtx)
			// Issue #42: reset the repo to base state before attempt 2 so it
			// does not inherit edits from attempt 1. Re-apply the test patch
			// so the failing tests are still present for the second attempt.
			exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "--", ".").Run()
			exec.CommandContext(ctx, "git", "-C", workDir, "clean", "-fd", "-e", ".nebula").Run()
			if o.testPatch != "" {
				if _, err := applyTestPatch(ctx, workDir, o.testPatch); err != nil {
					fmt.Printf("Warning: failed to re-apply test patch before MultiModel run 2: %v\n", err)
				}
			}
			o.executor.ResetSession()
			out2, err2 := o.executor.Execute(ctx, mmCtx)
			
			if err1 == nil && err2 == nil {
				if out1.Context == out2.Context {
					fmt.Println("Models reached consensus! Will skip Critic.")
					execOutput = out1
					skipCritic = true
				} else {
					fmt.Println("Models diverged. Proceeding to Critic for tie-breaking...")
					execOutput = out1
				}
			} else {
				execOutput, err = out1, err1
			}
		} else {
			execOutput, err = o.executor.Execute(ctx, execCtx)
		}
		if logger != nil {
			logger.Log(LogEvent{
				InstanceID: traj.InstanceID,
				Phase:      string(shared.PhaseExecution),
				DurationMs: time.Since(execStart).Milliseconds(),
				Result:     execOutput.Context,
			})
		}
		if err != nil {
			traj.Status = "error"
			traj.Error = fmt.Sprintf("execution failed: %v", err)
			return traj, err
		}
		
		prevPatch = execOutput.Context
		traj.Steps = append(traj.Steps, shared.Step{
			Phase:     shared.PhaseExecution,
			ToolCalls: execOutput.ToolCalls,
			Context:   execOutput.Context,
			Timestamp: time.Now(),
		})
		saveTrajectory(workDir, traj)
		
		fmt.Println("=== PHASE 3: VERIFICATION ===")
		verifyCtx := o.buildVerificationContext(execOutput.Context, o.failingTests)
		if verifyCtx.Metadata == nil {
			verifyCtx.Metadata = map[string]interface{}{}
		}
		verifyCtx.Metadata["workDir"] = workDir
		
		verifyStart := time.Now()
		verifyOutput, err := o.verifier.Execute(ctx, verifyCtx)
		if logger != nil {
			logger.Log(LogEvent{
				InstanceID: traj.InstanceID,
				Phase:      string(shared.PhaseVerification),
				DurationMs: time.Since(verifyStart).Milliseconds(),
				Result:     verifyOutput.Context,
			})
		}
		if err != nil {
			traj.Status = "error"
			traj.Error = fmt.Sprintf("verification failed: %v", err)
			return traj, err
		}
		
		traj.Steps = append(traj.Steps, shared.Step{
			Phase:     shared.PhaseVerification,
			ToolCalls: verifyOutput.ToolCalls,
			Context:   verifyOutput.Context,
			Timestamp: time.Now(),
		})
		saveTrajectory(workDir, traj)
		
		// Check if tests passed
		var verifyRes map[string]interface{}
		if err := json.Unmarshal([]byte(verifyOutput.Context), &verifyRes); err != nil {
			fmt.Printf("Warning: verifier output is not valid JSON (%v); using raw output as error trace\n", err)
			verifyRes = map[string]interface{}{"passed": false, "error_trace": verifyOutput.Context}
		}
		
		if passed, ok := verifyRes["passed"].(bool); ok && passed {
			fmt.Println(">> Tests passed! Fix is verified.")
			
			// Phase 4: Critique
			if skipCritic {
				fmt.Println("=== PHASE 4: CRITIQUE (SKIPPED DUE TO CONSENSUS) ===")
			} else {
			fmt.Println("=== PHASE 4: CRITIQUE ===")
			criticCtx := shared.AgentInput{
				Context: execOutput.Context, // Send the patch to critic
				Budget:  planCtx.Budget,
			}
			
			criticStart := time.Now()
			criticOutput, err := o.critic.Execute(ctx, criticCtx)
			if logger != nil {
				logger.Log(LogEvent{
					InstanceID: traj.InstanceID,
					Phase:      string(shared.PhaseCritique),
					DurationMs: time.Since(criticStart).Milliseconds(),
					Result:     criticOutput.Context,
				})
			}
			if err == nil {
				traj.Steps = append(traj.Steps, shared.Step{
					Phase:     shared.PhaseCritique,
					ToolCalls: criticOutput.ToolCalls,
					Context:   criticOutput.Context,
					Timestamp: time.Now(),
				})
				saveTrajectory(workDir, traj)
				
				var criticRes map[string]interface{}
				json.Unmarshal([]byte(criticOutput.Context), &criticRes)
				if approved, ok := criticRes["approved"].(bool); ok && !approved {
					fmt.Println(">> Critic rejected the patch due to security or destructive changes.")
					resolved = false
					criticRejected = true
					execContextStr = fmt.Sprintf("Critic rejected your patch. Warnings:\n%v\nPlease fix.", criticRes["warnings"])
					lastTrace = execContextStr
					continue // Loop back to Executor
				}
				fmt.Println(">> Critic approved the patch.")
			}
			}
			
			// Save successful patch to self-improving memory
			if o.memStore != nil {
				o.memStore.SavePastFix(ctx, import_mem.PastFix{
					IssuePattern: issue[:min(len(issue), 500)], // Store prefix as pattern
					PatchSummary: "Patch generated successfully for: " + traj.InstanceID,
					Resolved:     true,
				})
			}
			
			resolved = true
			break
		} else {
			criticRejected = false
			trace, _ := verifyRes["error_trace"].(string)
			if strings.TrimSpace(trace) == "" {
				trace = "(verifier returned no error trace) raw verifier output: " + verifyOutput.Context
			}
			// pytest puts the useful summary (assertion/traceback) at the end; keep the tail.
			if len(trace) > 8000 {
				trace = "...(truncated)...\n" + trace[len(trace)-8000:]
			}
			fmt.Printf(">> Error trace (%d chars):\n%s\n", len(trace), trace)
			fmt.Printf(">> Tests failed on attempt %d. Feeding error trace back to Executor.\n", attempt)
			lastTrace = trace
			execContextStr = fmt.Sprintf("Previous patch failed tests. Error trace:\n%s\nPlease fix the errors.", trace)
		}
	}
	
	// Issue #43: if the last outcome was a Critic rejection and nothing was
	// verified+approved, discard the rejected edits so they are not submitted.
	if !resolved && criticRejected {
		fmt.Println(">> Critic rejected the final patch; reverting working tree so it is not submitted.")
		exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "--", ".").Run()
		exec.CommandContext(ctx, "git", "-C", workDir, "clean", "-fd", "-e", ".nebula").Run()
	}

	traj.EndTime = time.Now()
	if resolved {
		traj.Status = "resolved"
	} else {
		traj.Status = "failed"
	}
	return traj, nil
}

func (o *Orchestrator) buildPlanningContext(issue, workDir string) shared.AgentInput {
	// Build repository summary
	summary := o.buildRepoSummary(workDir)
	o.contextMgr.SetGlobalSummary(summary)
	
	tools := make([]shared.ToolDefinition, len(o.toolRegistry.Definitions()))
	for i, t := range o.toolRegistry.Definitions() {
		tools[i] = shared.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	
	return shared.AgentInput{
		Context: issue,
		Tools:   tools,
		Memory: &shared.MemorySnapshot{
			GlobalSummary: summary,
		},
		Budget: shared.TokenBudget{
			Total: 4000,
			PerPhase: map[shared.Phase]int{
				shared.PhasePlanning: 4000,
			},
		},
	}
}

func (o *Orchestrator) buildExecutionContext(planContext string) shared.AgentInput {
	tools := make([]shared.ToolDefinition, len(o.toolRegistry.Definitions()))
	for i, t := range o.toolRegistry.Definitions() {
		tools[i] = shared.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	
	return shared.AgentInput{
		Context: planContext,
		Tools:   tools,
		Budget: shared.TokenBudget{
			Total: 8000,
			PerPhase: map[shared.Phase]int{
				shared.PhaseExecution: 8000,
			},
		},
	}
}

func (o *Orchestrator) buildVerificationContext(execContext string, failingTests []string) shared.AgentInput {
	tools := make([]shared.ToolDefinition, len(o.toolRegistry.Definitions()))
	for i, t := range o.toolRegistry.Definitions() {
		tools[i] = shared.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}

	metadata := map[string]interface{}{}
	if len(failingTests) > 0 {
		metadata["failingTests"] = failingTests
	}

	return shared.AgentInput{
		Context:  execContext,
		Tools:    tools,
		Metadata: metadata,
		Budget: shared.TokenBudget{
			Total: 2000,
			PerPhase: map[shared.Phase]int{
				shared.PhaseVerification: 2000,
			},
		},
	}
}

// extractFailingTests pulls the list of target tests out of the original issue
// text by looking for the common "so the following tests pass:" marker.
func extractFailingTests(issue string) []string {
	const marker = "so the following tests pass:"
	lower := strings.ToLower(issue)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return nil
	}

	rest := issue[idx+len(marker):]
	var tests []string
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(tests) > 0 {
				break
			}
			continue
		}
		// Stop if we hit a new section/heading.
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
		trimmed = strings.Trim(trimmed, "`")
		for _, field := range strings.FieldsFunc(trimmed, func(r rune) bool {
			return r == ' ' || r == ',' || r == '\t'
		}) {
			field = strings.Trim(field, "`\"'")
			if field == "" {
				continue
			}
			// Keep tokens that look like test identifiers.
			if strings.Contains(field, "::") || strings.Contains(field, ".py") ||
				strings.HasPrefix(field, "test") || strings.Contains(field, "/test") {
				tests = append(tests, field)
			}
		}
	}
	return tests
}

func (o *Orchestrator) buildRepoSummary(workDir string) string {
	const maxDepth = 5
	const maxChars = 4000
	var dirsBuilder strings.Builder
	truncated := false
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if path != workDir && (strings.HasPrefix(name, ".") || name == "__pycache__" || name == "node_modules") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(workDir, path)
		if err != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if d.IsDir() && depth > maxDepth {
			return filepath.SkipDir
		}
		if !d.IsDir() && (depth > maxDepth || !isSourceFile(name)) {
			return nil
		}
		line := "./" + rel
		if rel == "." {
			line = "."
		}
		if d.IsDir() {
			line += "/"
		} else if info, ierr := d.Info(); ierr == nil {
			line += fmt.Sprintf(" (%d B)", info.Size())
		}
		if dirsBuilder.Len()+len(line)+1 > maxChars {
			truncated = true
			return filepath.SkipAll
		}
		if dirsBuilder.Len() > 0 {
			dirsBuilder.WriteString("\n")
		}
		dirsBuilder.WriteString(line)
		return nil
	})

	dirs := dirsBuilder.String()
	if dirs == "" {
		dirs = "<unable to list directories>"
	} else if truncated {
		dirs += "\n... (truncated)"
	}

	// Detect basic languages by checking root files
	langs := ""
	if _, err := exec.Command("ls", filepath.Join(workDir, "package.json")).Output(); err == nil {
		langs += "JavaScript/Node.js detected. "
	}
	if _, err := exec.Command("ls", filepath.Join(workDir, "requirements.txt")).Output(); err == nil {
		langs += "Python detected. "
	}
	if _, err := exec.Command("ls", filepath.Join(workDir, "go.mod")).Output(); err == nil {
		langs += "Go detected. "
	}

	return fmt.Sprintf("Repository at %s\nPrimary languages/frameworks: %s\nDirectory Structure (depth 5, source files with sizes):\n%s", workDir, langs, dirs)
}
// isSourceFile reports whether name looks like a source/config file worth listing.
func isSourceFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".py", ".go", ".js", ".ts", ".java", ".rs", ".c", ".h", ".cpp", ".rb", ".pyx", ".cfg", ".toml":
		return true
	}
	return false
}

// runPreTestLocalization runs the failing tests once before the executor loop
// to capture the actual error traceback (issue #55). It extracts file:line
// targets from /testbed/ paths and returns a localization-hint block, or ""
// if nothing useful was found.
func (o *Orchestrator) runPreTestLocalization(ctx context.Context, workDir string) string {
	if len(o.failingTests) == 0 {
		return ""
	}
	cmd := testrunner.BuildTestCommand(workDir, o.failingTests)
	if cmd == "" {
		return ""
	}
	fmt.Println("[localization v2] Running failing tests to capture traceback...")
	// Cap at 30s — this is best-effort hint gathering, not correctness-critical.
	preCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(preCtx, "bash", "-c", cmd).CombinedOutput()
	rawOutput := string(out)
	if rawOutput == "" {
		return ""
	}
	hints := parseTestbedPaths(rawOutput)
	if hints == "" {
		return ""
	}
	fmt.Print("[localization v2] Traceback-derived file hints:\n" + hints)
	return "\n\n[LOCALIZATION HINTS - TRACEBACK]\n" + hints
}

// parseTestbedPaths extracts repo-relative file:line hints from test output
// containing /testbed/ paths (e.g. /testbed/src/flask/blueprints.py:42).
// It strips the /testbed/ prefix to produce repo-relative paths.
func parseTestbedPaths(output string) string {
	// Match /testbed/<path>:<line> patterns in pytest tracebacks.
	re := regexp.MustCompile(`/testbed/([^\s:]+\.py):(\d+)`)
	seen := map[string]bool{}
	var lines []string
	for _, m := range re.FindAllStringSubmatch(output, -1) {
		key := m[1] + ":" + m[2]
		if !seen[key] {
			seen[key] = true
			lines = append(lines, "  - "+key)
		}
		if len(lines) >= 20 {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Exact file:line targets from pre-run traceback (read these first):\n" + strings.Join(lines, "\n") + "\n"
}

// deleteTrajectory removes the persisted trajectory for workDir.
func deleteTrajectory(workDir string) error {
	err := os.Remove(filepath.Join(workDir, ".nebula", "trajectory.json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// applyTestPatch applies patch with `git apply --whitespace=fix` and returns
// the files it touches.
func applyTestPatch(ctx context.Context, workDir, patch string) ([]string, error) {
	var files []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			files = append(files, strings.TrimPrefix(line, "+++ b/"))
		}
	}
	cmd := exec.CommandContext(ctx, "git", "-C", workDir, "apply", "--whitespace=fix", "-")
	cmd.Stdin = strings.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git apply: %v: %s", err, out)
	}
	return files, nil
}

// revertFiles restores tracked files and removes untracked ones added by the test patch.
func revertFiles(workDir string, files []string) {
	for _, f := range files {
		if exec.Command("git", "-C", workDir, "checkout", "--", f).Run() != nil {
			// Not tracked at HEAD: the patch created it.
			os.Remove(filepath.Join(workDir, f))
		}
	}
}

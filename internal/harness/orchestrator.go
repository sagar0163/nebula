package harness

import (
	"os/exec"
	"os"
	"path/filepath"
	"context"
	"encoding/json"
	"fmt"
	"time"
	"strings"

	"github.com/sagar0163/nebula/internal/harness/agents"
	"github.com/sagar0163/nebula/internal/harness/contextpkg"
	"github.com/sagar0163/nebula/internal/harness/shared"
	"github.com/sagar0163/nebula/internal/harness/tools"
	"github.com/sagar0163/nebula/internal/llm"
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
		toolRegistry:  tools.NewDefaultRegistry(),
		maxSteps:      20,
		stepTimeout:   5 * time.Minute,
	}
	
	// Initialize agents with appropriate models
	o.planner = agents.NewPlannerAgent(router, routing[shared.PhasePlanning], o.toolRegistry)
	o.executor = agents.NewExecutorAgent(router, routing[shared.PhaseExecution], o.toolRegistry)
	o.verifier = agents.NewVerifierAgent(router, routing[shared.PhaseVerification])
	o.critic = agents.NewCriticAgent(router, routing[shared.PhaseCritique])
	
	store, _ := import_mem.New("nebula_harness.db")
	o.memStore = store
	
	
	return o
}

// Run executes the full harness pipeline on an issue
func (o *Orchestrator) Run(ctx context.Context, issue string, workDir string) (*shared.Trajectory, error) {
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
	} else if traj.Status == "resolved" || traj.Status == "failed" {
		fmt.Printf("Resuming existing trajectory which is already %s\n", traj.Status)
		return traj, nil
	} else {
		fmt.Printf("Resuming existing trajectory with %d steps\n", len(traj.Steps))
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
	
	// Executor-Verifier Retry Loop
	execContextStr := planOutput.Context
	resolved := false
	
	for attempt := 1; attempt <= 3; attempt++ {
		fmt.Printf("=== PHASE 2: EXECUTION (Attempt %d) ===\n", attempt)
		execCtx := o.buildExecutionContext(execContextStr)
		execCtx.Metadata = map[string]interface{}{"workDir": workDir}
		
		execStart := time.Now()
		var execOutput shared.AgentOutput
		var err error
		var skipCritic bool
		
		if o.MultiModel {
			fmt.Println("Running multi-model consensus check...")
			out1, err1 := o.executor.Execute(ctx, execCtx)
			out2, err2 := o.executor.Execute(ctx, execCtx)
			
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
		
		traj.Steps = append(traj.Steps, shared.Step{
			Phase:     shared.PhaseExecution,
			ToolCalls: execOutput.ToolCalls,
			Context:   execOutput.Context,
			Timestamp: time.Now(),
		})
		saveTrajectory(workDir, traj)
		
		fmt.Println("=== PHASE 3: VERIFICATION ===")
		verifyCtx := o.buildVerificationContext(execOutput.Context)
		verifyCtx.Metadata = map[string]interface{}{"workDir": workDir}
		
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
		json.Unmarshal([]byte(verifyOutput.Context), &verifyRes)
		
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
					execContextStr = fmt.Sprintf("Critic rejected your patch. Warnings:\n%v\nPlease fix.", criticRes["warnings"])
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
			trace, _ := verifyRes["error_trace"].(string)
			fmt.Printf(">> Tests failed on attempt %d. Feeding error trace back to Executor.\n", attempt)
			execContextStr = fmt.Sprintf("Previous patch failed tests. Error trace:\n%s\nPlease fix the errors.", trace)
		}
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

func (o *Orchestrator) buildVerificationContext(execContext string) shared.AgentInput {
	tools := make([]shared.ToolDefinition, len(o.toolRegistry.Definitions()))
	for i, t := range o.toolRegistry.Definitions() {
		tools[i] = shared.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	
	return shared.AgentInput{
		Context: execContext,
		Tools:   tools,
		Budget: shared.TokenBudget{
			Total: 2000,
			PerPhase: map[shared.Phase]int{
				shared.PhaseVerification: 2000,
			},
		},
	}
}

func (o *Orchestrator) buildRepoSummary(workDir string) string {
	// Use pure Go filepath.WalkDir instead of heavy exec.Command("find") fork
	var dirsBuilder strings.Builder
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		// Skip hidden dirs
		if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return filepath.SkipDir
		}
		
		rel, err := filepath.Rel(workDir, path)
		if err == nil {
			depth := strings.Count(rel, string(os.PathSeparator))
			if depth <= 3 {
				if dirsBuilder.Len() > 0 {
					dirsBuilder.WriteString("\n")
				}
				if rel == "." {
					dirsBuilder.WriteString(".")
				} else {
					dirsBuilder.WriteString("./" + rel)
				}
				if dirsBuilder.Len() > 1000 {
					return filepath.SkipAll // Early exit when truncated
				}
			} else {
				return filepath.SkipDir
			}
		}
		return nil
	})
	
	dirs := dirsBuilder.String()
	if dirs == "" {
		dirs = "<unable to list directories>"
	} else if len(dirs) >= 1000 {
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

	return fmt.Sprintf("Repository at %s\nPrimary languages/frameworks: %s\nDirectory Structure (depth 3):\n%s", workDir, langs, dirs)
}
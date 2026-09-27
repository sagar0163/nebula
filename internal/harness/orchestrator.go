package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sagar0163/nebula/internal/harness/agents"
	"github.com/sagar0163/nebula/internal/harness/contextpkg"
	"github.com/sagar0163/nebula/internal/harness/shared"
	"github.com/sagar0163/nebula/internal/harness/tools"
	"github.com/sagar0163/nebula/internal/llm"
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
	// critic   *agents.CriticAgent // TODO: implement
	
	maxSteps    int
	stepTimeout time.Duration
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
	o.planner = agents.NewPlannerAgent(router, routing[shared.PhasePlanning])
	o.executor = agents.NewExecutorAgent(router, routing[shared.PhaseExecution], o.toolRegistry)
	o.verifier = agents.NewVerifierAgent(router, routing[shared.PhaseVerification])
	// o.critic = agents.NewCriticAgent(router, routing[shared.PhaseCritique])
	
	return o
}

// Run executes the full harness pipeline on an issue
func (o *Orchestrator) Run(ctx context.Context, issue string, workDir string) (*shared.Trajectory, error) {
	traj := &shared.Trajectory{
		InstanceID: fmt.Sprintf("issue-%d", time.Now().Unix()),
		StartTime:  time.Now(),
		Status:     "running",
	}
	
	// Initialize context
	o.contextMgr.SetWorkingContext("")
	
	// Phase 1: Planning
	fmt.Println("=== PHASE 1: PLANNING ===")
	planCtx := o.buildPlanningContext(issue, workDir)
	planOutput, err := o.planner.Execute(ctx, planCtx)
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
	
	// Pass metadata down
	planCtx.Metadata = map[string]interface{}{"workDir": workDir}
	
	// Executor-Verifier Retry Loop
	execContextStr := planOutput.Context
	resolved := false
	
	for attempt := 1; attempt <= 3; attempt++ {
		fmt.Printf("=== PHASE 2: EXECUTION (Attempt %d) ===\n", attempt)
		execCtx := o.buildExecutionContext(execContextStr)
		execCtx.Metadata = map[string]interface{}{"workDir": workDir}
		
		execOutput, err := o.executor.Execute(ctx, execCtx)
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
		
		fmt.Println("=== PHASE 3: VERIFICATION ===")
		verifyCtx := o.buildVerificationContext(execOutput.Context)
		verifyCtx.Metadata = map[string]interface{}{"workDir": workDir}
		
		verifyOutput, err := o.verifier.Execute(ctx, verifyCtx)
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
		
		// Check if tests passed
		var verifyRes map[string]interface{}
		json.Unmarshal([]byte(verifyOutput.Context), &verifyRes)
		
		if passed, ok := verifyRes["passed"].(bool); ok && passed {
			resolved = true
			fmt.Println(">> Tests passed! Fix is verified.")
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
	// TODO: Implement proper repo summarization
	return fmt.Sprintf("Repository at %s", workDir)
}
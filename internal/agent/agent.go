package agent

import (
	"context"
	"fmt"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
)

// Agent is the core orchestration loop: run command → detect failure →
// diagnose → suggest fix → get approval → retry → learn.
type Agent struct {
	harness *pty.Harness
	router  *llm.Router
	store   memory.Store
}

// New creates an Agent wired up with the given dependencies.
func New(harness *pty.Harness, router *llm.Router, store memory.Store) *Agent {
	return &Agent{
		harness: harness,
		router:  router,
		store:   store,
	}
}

// RunResult is the outcome of running a command through the agent.
type RunResult struct {
	Command    string
	ExitCode   int
	Healed     bool
	HealApply  *models.HealSuggestion
}

// Run executes args through the PTY harness, healing on failure.
// It applies the safety policy before and after healing.
func (a *Agent) Run(ctx context.Context, args []string, opts RunOptions) (*RunResult, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	raw := joinArgs(args)
	result := &RunResult{Command: raw}

	// 1. Classify and check permission.
	risk := safety.Classify(raw)
	decision := safety.Decide(risk, opts.SkipPermissions)

	switch decision {
	case safety.DecisionDeny:
		return nil, fmt.Errorf("command denied by safety policy: %s", raw)
	case safety.DecisionAsk:
		if !opts.ApprovalFn(raw, risk) {
			return nil, fmt.Errorf("command rejected by user")
		}
	}

	// 2. Dry-run: show what would run, don't execute.
	if opts.DryRun {
		fmt.Printf("[dry-run] would run: %s\n", raw)
		return result, nil
	}

	// 3. Execute through PTY harness.
	cmdResult, err := a.harness.Run(ctx, args[0], args[1:])
	if err != nil {
		return nil, fmt.Errorf("execution error: %w", err)
	}
	result.ExitCode = cmdResult.ExitCode

	// 4. Save command to memory.
	_ = a.store.SaveCommand(ctx, &models.Command{
		Raw:      raw,
		ExitCode: cmdResult.ExitCode,
		Stdout:   string(cmdResult.Stdout),
	})

	// 5. On failure, attempt healing.
	if cmdResult.ExitCode != 0 {
		suggestion, err := a.diagnose(ctx, raw, string(cmdResult.Stdout))
		if err != nil {
			return result, nil // best-effort: return without healing
		}

		if suggestion != nil && opts.ApprovalFn(suggestion.FixCmd, safety.RiskMedium) {
			result.Healed = true
			result.HealApply = suggestion

			fixResult, err := a.harness.Run(ctx, "sh", []string{"-c", suggestion.FixCmd})
			if err == nil && fixResult.ExitCode == 0 {
				// 6. Learn the successful fix pattern.
				_ = a.learnPattern(ctx, raw, string(cmdResult.Stdout), suggestion.FixCmd)
			}
		}
	}

	return result, nil
}

// diagnose calls the LLM to analyze a failure and suggest a fix.
func (a *Agent) diagnose(ctx context.Context, cmd, output string) (*models.HealSuggestion, error) {
	prompt := buildDiagnosePrompt(cmd, output)
	req := llm.Request{
		SystemPrompt: systemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:    512,
		Temperature:  0.1,
	}

	tokens, err := a.router.Complete(ctx, llm.WorkloadDiagnose, req)
	if err != nil {
		return nil, err
	}

	var response string
	for t := range tokens {
		if t.Err != nil {
			return nil, t.Err
		}
		response += t.Text
	}

	return parseSuggestion(cmd, response), nil
}

func (a *Agent) learnPattern(ctx context.Context, failCmd, failOutput, fixCmd string) error {
	// TODO: embed the failure context and save as a Pattern for future retrieval.
	_ = failCmd
	_ = failOutput
	_ = fixCmd
	return nil
}

// RunOptions configures a single agent run.
type RunOptions struct {
	DryRun          bool
	SkipPermissions bool
	// ApprovalFn is called when user confirmation is needed.
	// Returns true if the user approved.
	ApprovalFn func(cmd string, risk safety.Risk) bool
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}

func buildDiagnosePrompt(cmd, output string) string {
	return fmt.Sprintf(`A shell command failed. Diagnose the error and suggest a fix.

Command: %s

Output:
%s

Respond with:
FIX: <the exact fix command>
EXPLANATION: <one sentence explaining what went wrong and why the fix works>`, cmd, output)
}

func parseSuggestion(originalCmd, response string) *models.HealSuggestion {
	// TODO: implement proper parsing of FIX:/EXPLANATION: response format.
	return &models.HealSuggestion{
		OriginalCmd: originalCmd,
		FixCmd:      response, // placeholder
		Explanation: "",
	}
}

const systemPrompt = `You are Nebula, a self-healing terminal agent.
Your job is to analyze failed shell commands and suggest precise fixes.
Be concise. Only suggest commands that are safe and reversible where possible.`

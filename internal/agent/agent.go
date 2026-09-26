package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
)

type Config struct {
	HistoryDepth int
	FileInjection bool
}

// Agent is the core orchestration loop: run command → detect failure →
// diagnose → suggest fix → get approval → retry → learn.
type Agent struct {
	harness *pty.Harness
	router  *llm.Router
	store   memory.Store

	doomMu         sync.Mutex
	doomLoopCounts map[string]int

	planner  *Planner
	executor *Executor

	sessionHistory []string
	historyDepth   int
}

// New creates an Agent wired up with the given dependencies.
func New(harness *pty.Harness, router *llm.Router, store memory.Store, cfg Config) *Agent {
	if cfg.HistoryDepth <= 0 {
		cfg.HistoryDepth = 10
	}
	return &Agent{
		harness:        harness,
		router:         router,
		store:          store,
		doomLoopCounts: make(map[string]int),
		planner:        NewPlanner(router, store, cfg.FileInjection),
		executor:       NewExecutor(harness, router, store),
		sessionHistory: make([]string, 0, cfg.HistoryDepth),
		historyDepth:   cfg.HistoryDepth,
	}
}

// RunResult is the outcome of running a command through the agent.
type RunResult struct {
	Command       string
	ExitCode      int
	Healed        bool
	HealApply     *models.HealSuggestion
	DoomLoopCount int
}

// Run executes args through the PTY harness, healing on failure.
// It applies the safety policy before and after healing.
func (a *Agent) Run(ctx context.Context, args []string, opts RunOptions) (*RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	raw := strings.Join(args, " ")
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
	start := time.Now()
	cmdResult, err := a.harness.Run(ctx, args[0], args[1:])
	if err != nil {
		return nil, fmt.Errorf("execution error: %w", err)
	}
	result.ExitCode = cmdResult.ExitCode

	// 4. Save command to memory.
	if err := a.store.SaveCommand(ctx, &models.Command{
		Raw:      raw,
		ExitCode: cmdResult.ExitCode,
		Stdout:   string(cmdResult.Stdout),
		Elapsed:  time.Since(start).Milliseconds(),
	}); err != nil {
		log.Printf("warn: save command: %v", err)
	}

	// On success clear all doom-loop counts for this command so healed state
	// doesn't carry stale failure counts. Counts are keyed "%x-%d" (hash +
	// turn index), so we delete every turn slot (0..maxTurns).
	if cmdResult.ExitCode == 0 {
		outHash := sha256.Sum256(append([]byte(raw), cmdResult.Stdout...))
		prefix := fmt.Sprintf("%x", outHash[:8])
		a.doomMu.Lock()
		for i := 0; i <= 6; i++ {
			delete(a.doomLoopCounts, fmt.Sprintf("%s-%d", prefix, i))
		}
		a.doomMu.Unlock()
	}

	a.doomMu.Lock()
	a.sessionHistory = append(a.sessionHistory, fmt.Sprintf("%s (exit %d)", raw, cmdResult.ExitCode))
	if len(a.sessionHistory) > a.historyDepth {
		a.sessionHistory = a.sessionHistory[len(a.sessionHistory)-a.historyDepth:]
	}
	sessionSnap := make([]string, len(a.sessionHistory))
	copy(sessionSnap, a.sessionHistory)
	a.doomMu.Unlock()

	// 5. On failure, attempt multi-turn healing.
		maxTurns := 6
	history := []models.TurnRecord{}
	madeProgress := true // Start true to allow first 3 turns unconditionally

	for cmdResult.ExitCode != 0 && len(history) < maxTurns {
		if len(history) >= 3 && !madeProgress {
			// Stop early if no progress was made after turn 3
			break
		}

		outHash := sha256.Sum256(append([]byte(raw), cmdResult.Stdout...))
		// Update doom-loop fingerprint to use turn count to avoid interference
		fingerprint := fmt.Sprintf("%x-%d", outHash[:8], len(history))

		a.doomMu.Lock()
		count := a.doomLoopCounts[fingerprint]
		if len(history) == 0 {
			a.doomLoopCounts[fingerprint] = count + 1
			result.DoomLoopCount = count + 1
		}
		a.doomMu.Unlock()

		if count >= 3 {
			return result, fmt.Errorf("healing loop detected after 3 attempts — manual intervention required")
		}

		suggestion, err := a.planner.Plan(ctx, raw, string(cmdResult.Stdout), a.harness.Transcript(), cmdResult.ExitCode, history, sessionSnap)
		if err != nil {
			return result, nil // best-effort: return without healing
		}

		if suggestion == nil {
			break
		}

		approved := false
		wrapperFn := func(cmd string, risk safety.Risk) bool {
			approved = opts.ApprovalFn(cmd, risk)
			return approved
		}

		fixResult, err := a.executor.Execute(ctx, suggestion, string(cmdResult.Stdout), wrapperFn)
		if err != nil || !approved {
			break
		}

		if fixResult != nil && fixResult.ExitCode == 0 {
			verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			verifyResult, vErr := a.harness.Run(verifyCtx, args[0], args[1:])
			cancel()
			
			if vErr == nil && verifyResult.ExitCode == 0 {
				result.Healed = true
				result.HealApply = suggestion
				if err := a.executor.LearnPattern(ctx, suggestion.OriginalCmd, string(cmdResult.Stdout), suggestion.FixCmd, history); err != nil {
					log.Printf("warn: learnPattern: %v", err)
				}
				break
			} else if vErr == nil && verifyResult.ExitCode != 0 {
				newOutput := string(verifyResult.Stdout)
				if newOutput == string(cmdResult.Stdout) {
					madeProgress = false
					break
				}
				madeProgress = progressCheck(cmdResult.ExitCode, string(cmdResult.Stdout), verifyResult.ExitCode, newOutput)
				history = append(history, models.TurnRecord{
					FixCmd:    suggestion.FixCmd + " (applied, but original command still failed)",
					Output:    newOutput,
					ExitCode:  verifyResult.ExitCode,
					Reasoning: suggestion.Reasoning,
				})
				cmdResult = verifyResult
			} else {
				break
			}
		} else if fixResult != nil && fixResult.ExitCode != 0 {
			newOutput := string(fixResult.Stdout)
			if newOutput == string(cmdResult.Stdout) {
				// Avoid infinite loop if same exact output
				madeProgress = false
				break
			}
			madeProgress = progressCheck(cmdResult.ExitCode, string(cmdResult.Stdout), fixResult.ExitCode, newOutput)
			history = append(history, models.TurnRecord{
				FixCmd:    suggestion.FixCmd,
				Output:    newOutput,
				ExitCode:  fixResult.ExitCode,
				Reasoning: suggestion.Reasoning,
			})
			cmdResult = fixResult
		}
	}

	return result, nil
}

// Ask handles a general-purpose request in any domain, streaming the LLM
// response and returning the full string. The task is saved to memory.


func progressCheck(prevExit int, prevOut string, newExit int, newOut string) bool {
	if prevExit != newExit {
		return true
	}

	// Identical content — definitely no progress.
	if prevOut == newOut {
		return false
	}

	// Content hash changed — output mutated even if length is similar.
	// This catches "error: undefined foo" → "error: undefined bar" which the
	// length and keyword checks both miss.
	prevHash := sha256.Sum256([]byte(prevOut))
	newHash := sha256.Sum256([]byte(newOut))
	if prevHash != newHash {
		return true
	}

	return false
}

func (a *Agent) Ask(ctx context.Context, input string, stream bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	domain := detectDomain(input)

	workload := llm.WorkloadHeal
	switch domain {
	case "research":
		workload = llm.WorkloadLearn // stronger tier for long/research answers
	}

	req := llm.Request{
		SystemPrompt: systemPromptFor(domain),
		Messages:     []llm.Message{{Role: "user", Content: input}},
		MaxTokens:    2048,
		Temperature:  0.7,
	}

	tokens, err := a.router.Complete(ctx, workload, req)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	for t := range tokens {
		if t.Err != nil {
			return "", t.Err
		}
		builder.WriteString(t.Text)
		if stream {
			fmt.Print(t.Text)
		}
	}
	response := builder.String()
	if stream {
		fmt.Println()
	}

	if err := a.store.SaveTask(ctx, &models.Task{
		Input:    input,
		Response: response,
		Domain:   domain,
	}); err != nil {
		log.Printf("warn: save task: %v", err)
	}
	return response, nil
}

type RunOptions struct {
	DryRun          bool
	SkipPermissions bool
	// ApprovalFn is called when user confirmation is needed.
	// Returns true if the user approved.
	ApprovalFn func(cmd string, risk safety.Risk) bool
}


const systemPrompt = `You are Nebula, a self-healing terminal agent.
Your job is to analyze failed shell commands and suggest precise fixes.
Be concise. Only suggest commands that are safe and reversible where possible.`

const codeSystemPrompt = `You are Nebula, an expert coding assistant. Help with code review, debugging, refactoring, and writing code in any language. Be precise and show working examples.`

const writingSystemPrompt = `You are Nebula, a writing assistant. Help with drafting, editing, improving prose, novels, emails, and any written content. Adapt your tone to the user's style.`

const researchSystemPrompt = `You are Nebula, a research assistant. Synthesize information, explain concepts, compare options, and provide well-structured answers with clear reasoning.`

const generalSystemPrompt = `You are Nebula, a general-purpose AI assistant. Help with any task — coding, writing, analysis, planning, or conversation. Be helpful, concise, and accurate.`

// systemPromptFor returns the system prompt matching the task domain,
// falling back to the general-purpose prompt.
func systemPromptFor(domain string) string {
	switch domain {
	case "terminal":
		return systemPrompt
	case "code":
		return codeSystemPrompt
	case "writing":
		return writingSystemPrompt
	case "research":
		return researchSystemPrompt
	default:
		return generalSystemPrompt
	}
}

// detectDomain classifies an input into a task domain by keyword heuristics.
func detectDomain(input string) string {
	lower := strings.ToLower(input)
	switch {
	case containsAny(lower, terminalKeywords...):
		return "terminal"
	case containsAny(lower, codeKeywords...):
		return "code"
	case containsAny(lower, researchKeywords...):
		return "research"
	case containsAny(lower, writingKeywords...):
		return "writing"
	default:
		return "general"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

var terminalKeywords = []string{
	"shell", "terminal", "command", "bash", "zsh", "install", "apt", "brew",
	"docker", "kubectl", "grep", "sed", "awk", "chmod", "sudo", "curl",
	"wget", "package manager", "alias", "command line", "cron", "ftp", "ssh ",
}

var codeKeywords = []string{
	"code", "function", "class ", "bug", "debug", "refactor", "refactoring",
	"variable", "api", "endpoint", "compiler", "compile", "exception",
	"stack trace", "unit test", "pull request", "python", "javascript",
	"typescript", "golang", "rust", "react", "vue", "node",
	"sql", "json", "yaml", "regex",
}

var writingKeywords = []string{
	"draft", "edit", "rewrite", "essay", "email", "blog", "novel",
	"prose", "story", "article", "poem", "poetry", "resume", "cover letter",
	"headline", "copywriting",
}

var researchKeywords = []string{
	"research", "explain", "compare", "summarize", "summary", "analysis",
	"investigate", "difference between", "literature", "paper on", "study",
	"overview of", "breakdown of",
}

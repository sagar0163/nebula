package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"strings"
	"sync"

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

	doomMu         sync.Mutex
	doomLoopCounts map[string]int
}

// New creates an Agent wired up with the given dependencies.
func New(harness *pty.Harness, router *llm.Router, store memory.Store) *Agent {
	return &Agent{
		harness:        harness,
		router:         router,
		store:          store,
		doomLoopCounts: make(map[string]int),
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
		outHash := sha256.Sum256(cmdResult.Stdout)
		fingerprint := fmt.Sprintf("%s:%x", raw, outHash[:8])

		a.doomMu.Lock()
		count := a.doomLoopCounts[fingerprint]
		a.doomLoopCounts[fingerprint] = count + 1
		a.doomMu.Unlock()

		result.DoomLoopCount = count + 1
		if count >= 3 {
			return result, fmt.Errorf("healing loop detected after 3 attempts — manual intervention required")
		}

		// Try recalling a similar past fix before calling the LLM.
		if recalled, err := a.recallPattern(ctx, raw, string(cmdResult.Stdout)); err == nil && recalled != nil {
			// Use the recalled fix — still ask for approval.
			if opts.ApprovalFn(recalled.FixCmd, safety.RiskMedium) {
				result.Healed = true
				result.HealApply = recalled
				fixResult, err := a.harness.Run(ctx, "sh", []string{"-c", recalled.FixCmd})
				if err == nil && fixResult.ExitCode == 0 {
					_ = a.learnPattern(ctx, raw, string(cmdResult.Stdout), recalled.FixCmd)
				}
				return result, nil
			}
		}

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

// Ask handles a general-purpose request in any domain, streaming the LLM
// response and returning the full string. The task is saved to memory.
func (a *Agent) Ask(ctx context.Context, input string) (string, error) {
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

	var response string
	for t := range tokens {
		if t.Err != nil {
			return "", t.Err
		}
		response += t.Text
	}

	_ = a.store.SaveTask(ctx, &models.Task{
		Input:    input,
		Response: response,
		Domain:   domain,
	})
	return response, nil
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
	embedding, err := encodeEmbeddingText(ctx, a.router, failCmd, failOutput)
	if err != nil {
		// Best-effort: embedding failure shouldn't block the heal flow.
		return nil
	}
	return a.store.SavePattern(ctx, &models.Pattern{
		FailCmd:     failCmd,
		FailOutput:  failOutput,
		FixCmd:      fixCmd,
		SuccessRate: 1.0,
		UseCount:    1,
		Embedding:   embedding,
	})
}

// recallPattern searches stored patterns for a similar past fix for the given
// failure. Returns nil, nil if no match is found or embedding is unavailable.
func (a *Agent) recallPattern(ctx context.Context, failCmd, failOutput string) (*models.HealSuggestion, error) {
	embedding, err := embedText(ctx, a.router, failCmd, failOutput)
	if err != nil {
		return nil, nil
	}

	patterns, err := a.store.FindSimilarPatterns(ctx, embedding, 1)
	if err != nil || len(patterns) == 0 {
		return nil, nil
	}

	return &models.HealSuggestion{
		OriginalCmd: failCmd,
		FixCmd:      patterns[0].FixCmd,
		Explanation: "recalled from similar past fix",
	}, nil
}

// buildEmbeddingText concatenates the failed command and the first 500 chars
// of its output to form the semantic context used for embedding.
func buildEmbeddingText(failCmd, failOutput string) string {
	text := failCmd + "\n"
	if len(failOutput) > 500 {
		return text + failOutput[:500]
	}
	return text + failOutput
}

// embedText embeds the failure context and returns the raw float32 vector.
func embedText(ctx context.Context, router *llm.Router, failCmd, failOutput string) ([]float32, error) {
	return router.Embed(ctx, buildEmbeddingText(failCmd, failOutput))
}

// encodeEmbeddingText embeds the failure context and returns the gob-encoded
// bytes for persistence, matching gobDecodeFloats in the memory store.
func encodeEmbeddingText(ctx context.Context, router *llm.Router, failCmd, failOutput string) ([]byte, error) {
	vec, err := embedText(ctx, router, failCmd, failOutput)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(vec); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
	var fix, explanation string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FIX:") {
			fix = strings.TrimSpace(strings.TrimPrefix(line, "FIX:"))
		} else if strings.HasPrefix(line, "EXPLANATION:") {
			explanation = strings.TrimSpace(strings.TrimPrefix(line, "EXPLANATION:"))
		}
	}
	if fix == "" {
		return nil
	}
	return &models.HealSuggestion{
		OriginalCmd: originalCmd,
		FixCmd:      fix,
		Explanation: explanation,
	}
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
	case containsAny(lower, writingKeywords...):
		return "writing"
	case containsAny(lower, researchKeywords...):
		return "research"
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
	"write", "draft", "edit", "rewrite", "essay", "email", "blog", "novel",
	"prose", "story", "article", "poem", "poetry", "resume", "cover letter",
	"headline", "copywriting",
}

var researchKeywords = []string{
	"research", "explain", "compare", "summarize", "summary", "analysis",
	"investigate", "difference between", "literature", "paper on", "study",
	"overview of", "breakdown of",
}

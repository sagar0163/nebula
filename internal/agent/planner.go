package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
)

type Planner struct {
	fileInjection bool
	router *llm.Router
	store  memory.Store
}

func NewPlanner(router *llm.Router, store memory.Store, fileInjection bool) *Planner {
	return &Planner{
		router: router,
		fileInjection: fileInjection,
		store:  store,
	}
}

func (p *Planner) Plan(ctx context.Context, failCmd, output, transcript string, failExitCode int, history []models.TurnRecord, sessionHistory []string) (*models.HealSuggestion, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Try recalling a similar past fix before calling the LLM.
	if recalled, err := p.recallPattern(ctx, failCmd, output, len(history)); err == nil && recalled != nil {
		return recalled, nil
	}

	return p.diagnose(ctx, failCmd, output, transcript, failExitCode, history, sessionHistory)
}

func (p *Planner) budgetOutput(ctx context.Context, output string) string {
	cw := 4096
	if p.router != nil {
		cw = p.router.ContextWindow(ctx, llm.WorkloadDiagnose)
		if cw <= 0 {
			cw = 4096
		}
	}
	// Budget max 20% of context window for command output (approx 4 chars per token)
	tokenBudget := cw * 20 / 100
	maxBytes := tokenBudget * 4
	if maxBytes < 1024 {
		maxBytes = 1024
	}

	if len(output) <= maxBytes {
		return output
	}

	headCap := maxBytes / 4
	if headCap < 128 {
		headCap = 128
	}
	marker := fmt.Sprintf("\n[... %d bytes omitted ...]\n", len(output)-maxBytes)
	tailBudget := maxBytes - headCap - len(marker)
	if tailBudget <= 0 {
		return output[len(output)-maxBytes:]
	}

	head := output[:headCap]
	tail := output[len(output)-tailBudget:]
	return head + marker + tail
}

func (p *Planner) diagnose(ctx context.Context, cmd, output, transcript string, failExitCode int, history []models.TurnRecord, sessionHistory []string) (*models.HealSuggestion, error) {
	output = p.budgetOutput(ctx, output)
	prompt := buildDiagnosePrompt(safety.ScrubSecrets(cmd), safety.ScrubSecrets(output), safety.ScrubSecrets(transcript), failExitCode, history, sessionHistory, p.fileInjection)
	req := llm.Request{
		SystemPrompt:   systemPrompt,
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      512,
		Temperature:    0.1,
		ResponseFormat: "json",
	}

	tokens, err := p.router.Complete(ctx, llm.WorkloadDiagnose, req)
	if err != nil {
		return nil, err
	}

	var builder strings.Builder
	for t := range tokens {
		if t.Err != nil {
			return nil, t.Err
		}
		builder.WriteString(t.Text)
	}
	response := builder.String()

	return parseSuggestion(cmd, response), nil
}

func (p *Planner) recallPattern(ctx context.Context, failCmd, failOutput string, attemptCount int) (*models.HealSuggestion, error) {
	buildSuggestion := func(pattern *models.Pattern, source string) *models.HealSuggestion {
		fix := pattern.FixCmd
		if pattern.FixChain != "" {
			var chain []string
			if err := json.Unmarshal([]byte(pattern.FixChain), &chain); err == nil && len(chain) > 0 {
				if attemptCount < len(chain) {
					fix = chain[attemptCount]
					source += " (partial step)"
				}
			}
		}
		return &models.HealSuggestion{
			OriginalCmd: failCmd,
			FixCmd:      fix,
			Explanation: source,
		}
	}

	// 1. Exact match first
	pattern, err := p.store.FindPattern(ctx, failCmd, failOutput)
	if err == nil && pattern != nil {
		s := buildSuggestion(pattern, "recalled from exact past fix")
		s.Confidence = 0.95
		return s, nil
	}

	// 2. Keyword overlap fallback
	// Extract basic tokens from command and output
	tokens := strings.Fields(failCmd)
	lines := strings.Split(failOutput, "\n")
	if len(lines) > 0 {
		tokens = append(tokens, strings.Fields(lines[0])...)
	}
	if len(lines) > 1 {
		tokens = append(tokens, strings.Fields(lines[1])...)
	}

	var keywords []string
	ignore := map[string]bool{"the": true, "a": true, "is": true, "at": true, "in": true, "on": true, "to": true, "and": true}
	for _, tok := range tokens {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if len(tok) > 2 && !ignore[tok] {
			keywords = append(keywords, tok)
		}
	}

	if len(keywords) > 0 {
		if len(keywords) > 10 {
			keywords = keywords[:10] // limit to prevent massive queries
		}
		patterns, err := p.store.FindPatternsByKeywords(ctx, keywords, 10)
		if err == nil && len(patterns) > 0 {
			// Rank by overlap
			bestScore := 0
			var best *models.Pattern
			for _, p := range patterns {
				score := 0
				for _, kw := range keywords {
					if strings.Contains(strings.ToLower(p.FailCmd), kw) || strings.Contains(strings.ToLower(p.FailOutput), kw) {
						score++
					}
				}
				if score > bestScore {
					bestScore = score
					best = p
				}
			}
			if bestScore >= 2 && best != nil {
				s := buildSuggestion(best, "recalled from similar past fix (keyword match)")
				s.Confidence = 0.5
				return s, nil
			}
		}
	}

	return nil, nil
}

func buildDiagnosePrompt(cmd, output, transcript string, failExitCode int, history []models.TurnRecord, sessionHistory []string, fileInjection bool) string {
	output = SummarizeOutput(output)
	output = pty.StripANSI(output)
	transcript = pty.StripANSI(transcript)

	output = strings.ReplaceAll(output, "FIX:", "F-I-X:")
	output = strings.ReplaceAll(output, "EXPLANATION:", "E-X-P-L-A-N-A-T-I-O-N:")
	transcript = strings.ReplaceAll(transcript, "FIX:", "F-I-X:")
	transcript = strings.ReplaceAll(transcript, "EXPLANATION:", "E-X-P-L-A-N-A-T-I-O-N:")

	fileContext := ""
	if fileInjection {
		files := ExtractRelevantFiles(cmd, output, 3)
		if len(files) > 0 {
			fileContext = "Relevant files:\n```\n"
			for _, f := range files {
				c := ReadFileExcerpt(f, 2048)
				if c != "" {
					fileContext += "// " + f + "\n" + c + "\n"
				}
			}
			fileContext += "```\n\n"
		}
	}

	prompt := fmt.Sprintf(`A shell command failed. Diagnose the error and suggest a fix.

Command: %s

%sOutput:
%s
`, cmd, fileContext, output)

	pCtx := DetectProjectContext("")
	if pCtxStr := pCtx.String(); pCtxStr != "" {
		prompt = pCtxStr + "\n\n" + prompt
	}

	if len(sessionHistory) > 0 {
		prompt += "\nRecent commands (newest last):\n"
		for _, sh := range sessionHistory {
			prompt += fmt.Sprintf("  %s\n", sh)
		}
		prompt += fmt.Sprintf("  %s (exit %d) ← failing command\n", cmd, failExitCode)
	}

	if transcript != "" {
		prompt += fmt.Sprintf("\nRecent Terminal Context:\n%s\n", transcript)
	}

	if len(history) > 0 {
		prompt += "\nPreviously Tried Fixes:\n"
		for i, h := range history {
			prompt += fmt.Sprintf("Attempt %d: %s\nFailed with (Exit %d):\n%s\n\n", i+1, h.FixCmd, h.ExitCode, SummarizeOutput(pty.StripANSI(h.Output)))
		}
		// Chain-of-thought handoff: replay last turn's reasoning so the LLM
		// can revise its own diagnosis rather than starting from scratch.
		if last := history[len(history)-1]; strings.TrimSpace(last.Reasoning) != "" {
			prompt += fmt.Sprintf("My prior reasoning was: %s. That fix failed with exit %d. Revise.\n\n",
				strings.TrimSpace(last.Reasoning), last.ExitCode)
		}
	}

	prompt += `
Respond with valid JSON only — no markdown, no extra text:
{"fix": "<the exact fix command>", "explanation": "<one sentence explaining what went wrong and why the fix works>", "reasoning": "<your diagnosis of root cause and why this fix should work>", "confidence": <0.0-1.0>}`

	return prompt
}

func parseSuggestion(originalCmd, response string) *models.HealSuggestion {
	// Try JSON first (preferred — structured, unambiguous).
	trimmed := strings.TrimSpace(response)
	// Strip markdown code fences if the model wrapped the JSON.
	if strings.HasPrefix(trimmed, "```") {
		if i := strings.Index(trimmed[3:], "```"); i >= 0 {
			trimmed = strings.TrimSpace(trimmed[3 : 3+i])
			if after, ok := strings.CutPrefix(trimmed, "json"); ok {
				trimmed = strings.TrimSpace(after)
			}
		}
	}
	var parsed struct {
		Fix         string  `json:"fix"`
		Explanation string  `json:"explanation"`
		Reasoning   string  `json:"reasoning"`
		Confidence  float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && parsed.Fix != "" {
		conf := parsed.Confidence
		if conf == 0 {
			if parsed.Reasoning != "" {
				conf = 0.85
			} else {
				conf = 0.7
			}
		}
		return &models.HealSuggestion{
			OriginalCmd: originalCmd,
			FixCmd:      parsed.Fix,
			Explanation: parsed.Explanation,
			Reasoning:   parsed.Reasoning,
			Confidence:  conf,
		}
	}

	// Fallback: legacy FIX:/EXPLANATION: line parsing for providers that ignore JSON mode.
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
		Confidence:  0.7,
	}
}

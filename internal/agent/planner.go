package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
)

type Planner struct {
	router *llm.Router
	store  memory.Store
}

func NewPlanner(router *llm.Router, store memory.Store) *Planner {
	return &Planner{
		router: router,
		store:  store,
	}
}

func (p *Planner) Plan(ctx context.Context, failCmd, output, transcript string, history []models.TurnRecord) (*models.HealSuggestion, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Try recalling a similar past fix before calling the LLM.
	if recalled, err := p.recallPattern(ctx, failCmd, output); err == nil && recalled != nil {
		return recalled, nil
	}

	return p.diagnose(ctx, failCmd, output, transcript, history)
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

func (p *Planner) diagnose(ctx context.Context, cmd, output, transcript string, history []models.TurnRecord) (*models.HealSuggestion, error) {
	output = p.budgetOutput(ctx, output)
	prompt := buildDiagnosePrompt(safety.ScrubSecrets(cmd), safety.ScrubSecrets(output), safety.ScrubSecrets(transcript), history)
	req := llm.Request{
		SystemPrompt: systemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:    512,
		Temperature:  0.1,
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

func (p *Planner) recallPattern(ctx context.Context, failCmd, failOutput string) (*models.HealSuggestion, error) {
	// 1. Exact match first
	pattern, err := p.store.FindPattern(ctx, failCmd, failOutput)
	if err == nil && pattern != nil {
		return &models.HealSuggestion{
			OriginalCmd: failCmd,
			FixCmd:      pattern.FixCmd,
			Explanation: "recalled from exact past fix",
		}, nil
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
				return &models.HealSuggestion{
					OriginalCmd: failCmd,
					FixCmd:      best.FixCmd,
					Explanation: "recalled from similar past fix (keyword match)",
				}, nil
			}
		}
	}

	return nil, nil
}

func buildDiagnosePrompt(cmd, output, transcript string, history []models.TurnRecord) string {
	output = SummarizeOutput(output)
	output = pty.StripANSI(output)
	transcript = pty.StripANSI(transcript)

	output = strings.ReplaceAll(output, "FIX:", "F-I-X:")
	output = strings.ReplaceAll(output, "EXPLANATION:", "E-X-P-L-A-N-A-T-I-O-N:")
	transcript = strings.ReplaceAll(transcript, "FIX:", "F-I-X:")
	transcript = strings.ReplaceAll(transcript, "EXPLANATION:", "E-X-P-L-A-N-A-T-I-O-N:")

	prompt := fmt.Sprintf(`A shell command failed. Diagnose the error and suggest a fix.

Command: %s

Output:
%s
`, cmd, output)

	pCtx := DetectProjectContext("")
	if pCtxStr := pCtx.String(); pCtxStr != "" {
		prompt = pCtxStr + "\n\n" + prompt
	}

	if transcript != "" {
		prompt += fmt.Sprintf("\nRecent Terminal Context:\n%s\n", transcript)
	}

	if len(history) > 0 {
		prompt += "\nPreviously Tried Fixes:\n"
		for i, h := range history {
			prompt += fmt.Sprintf("Attempt %d: %s\nFailed with (Exit %d):\n%s\n\n", i+1, h.FixCmd, h.ExitCode, SummarizeOutput(pty.StripANSI(h.Output)))
		}
	}

	prompt += `
Respond with:
FIX: <the exact fix command>
EXPLANATION: <one sentence explaining what went wrong and why the fix works>`

	return prompt
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

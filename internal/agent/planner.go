package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
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

func (p *Planner) Plan(ctx context.Context, failCmd, output string) (*models.HealSuggestion, error) {
	// Try recalling a similar past fix before calling the LLM.
	if recalled, err := p.recallPattern(ctx, failCmd, output); err == nil && recalled != nil {
		return recalled, nil
	}

	return p.diagnose(ctx, failCmd, output)
}

func (p *Planner) diagnose(ctx context.Context, cmd, output string) (*models.HealSuggestion, error) {
	prompt := buildDiagnosePrompt(cmd, output)
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

	var response string
	for t := range tokens {
		if t.Err != nil {
			return nil, t.Err
		}
		response += t.Text
	}

	return parseSuggestion(cmd, response), nil
}

func (p *Planner) recallPattern(ctx context.Context, failCmd, failOutput string) (*models.HealSuggestion, error) {
	pattern, err := p.store.FindPatternByCmd(ctx, failCmd)
	if err != nil || pattern == nil {
		return nil, nil
	}

	return &models.HealSuggestion{
		OriginalCmd: failCmd,
		FixCmd:      pattern.FixCmd,
		Explanation: "recalled from similar past fix",
	}, nil
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

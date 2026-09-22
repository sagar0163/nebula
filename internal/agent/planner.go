package agent

import (
	"bytes"
	"context"
	"encoding/gob"
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
	embedding, err := embedText(ctx, p.router, failCmd, failOutput)
	if err != nil {
		return nil, nil
	}

	patterns, err := p.store.FindSimilarPatterns(ctx, embedding, 1)
	if err != nil || len(patterns) == 0 {
		return nil, nil
	}

	return &models.HealSuggestion{
		OriginalCmd: failCmd,
		FixCmd:      patterns[0].FixCmd,
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

func buildEmbeddingText(failCmd, failOutput string) string {
	text := failCmd + "\n"
	if len(failOutput) > 500 {
		return text + failOutput[:500]
	}
	return text + failOutput
}

func embedText(ctx context.Context, router *llm.Router, failCmd, failOutput string) ([]float32, error) {
	return router.Embed(ctx, buildEmbeddingText(failCmd, failOutput))
}

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

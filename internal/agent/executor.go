package agent

import (
	"context"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
)

type Executor struct {
	harness *pty.Harness
	router  *llm.Router
	store   memory.Store
}

func NewExecutor(harness *pty.Harness, router *llm.Router, store memory.Store) *Executor {
	return &Executor{
		harness: harness,
		router:  router,
		store:   store,
	}
}

func (e *Executor) Execute(ctx context.Context, suggestion *models.HealSuggestion, failOutput string, approvalFn func(string, safety.Risk) bool) error {
	if !approvalFn(suggestion.FixCmd, safety.RiskMedium) {
		return nil
	}

	fixResult, err := e.harness.Run(ctx, "sh", []string{"-c", suggestion.FixCmd})
	if err == nil && fixResult.ExitCode == 0 {
		_ = e.learnPattern(ctx, suggestion.OriginalCmd, failOutput, suggestion.FixCmd)
	}

	return err
}

func (e *Executor) learnPattern(ctx context.Context, failCmd, failOutput, fixCmd string) error {
	embedding, err := encodeEmbeddingText(ctx, e.router, failCmd, failOutput)
	if err != nil {
		// Best-effort: embedding failure shouldn't block the heal flow.
		return nil
	}
	return e.store.SavePattern(ctx, &models.Pattern{
		FailCmd:     failCmd,
		FailOutput:  failOutput,
		FixCmd:      fixCmd,
		SuccessRate: 1.0,
		UseCount:    1,
		Embedding:   embedding,
	})
}

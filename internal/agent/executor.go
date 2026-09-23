package agent

import (
	"context"
	"errors"
	"strings"

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
	args := strings.Fields(suggestion.FixCmd)
	if len(args) == 0 {
		return errors.New("fix command is empty")
	}
	if strings.ContainsAny(suggestion.FixCmd, "|><;&`$()") {
		return errors.New("fix command contains shell metacharacters — manual review required")
	}

	if !approvalFn(suggestion.FixCmd, safety.Classify(suggestion.FixCmd)) {
		return nil
	}

	fixResult, err := e.harness.Run(ctx, args[0], args[1:])
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

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/shlex"

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

func containsUnquotedMeta(s string) bool {
	inSQuote := false
	inDQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' {
			i++
			continue
		}
		if c == '\'' && !inDQuote {
			inSQuote = !inSQuote
			continue
		}
		if c == '"' && !inSQuote {
			inDQuote = !inDQuote
			continue
		}
		if !inSQuote && !inDQuote {
			if strings.ContainsRune("|><;&`$()", rune(c)) {
				return true
			}
		}
	}
	return false
}

func (e *Executor) Execute(ctx context.Context, suggestion *models.HealSuggestion, failOutput string, approvalFn func(string, safety.Risk) bool) error {
	args, err := shlex.Split(suggestion.FixCmd)
	if err != nil {
		return fmt.Errorf("parse fix command: %w", err)
	}
	if len(args) == 0 {
		return errors.New("fix command is empty")
	}
	if containsUnquotedMeta(suggestion.FixCmd) {
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
	return e.store.SavePattern(ctx, &models.Pattern{
		FailCmd:     failCmd,
		FailOutput:  failOutput,
		FixCmd:      fixCmd,
		SuccessRate: 1.0,
		UseCount:    1,
		Embedding:   nil,
	})
}

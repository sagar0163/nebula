package agent

import (
	"encoding/json"
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

func (e *Executor) Execute(ctx context.Context, suggestion *models.HealSuggestion, failOutput string, approvalFn func(string, safety.Risk) bool) (*pty.CommandResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(suggestion.FixCmd) == "" {
		return nil, errors.New("fix command is empty")
	}
	args, err := shlex.Split(suggestion.FixCmd)
	if err != nil {
		return nil, fmt.Errorf("parse fix command: %w", err)
	}
	if len(args) == 0 {
		return nil, errors.New("fix command is empty")
	}
	if containsUnquotedMeta(suggestion.FixCmd) {
		return nil, errors.New("fix command contains shell metacharacters — manual review required")
	}

	risk := safety.Classify(suggestion.FixCmd)
	promptCmd := suggestion.FixCmd
	if suggestion.Confidence > 0 {
		promptCmd = fmt.Sprintf("Fix suggestion (confidence: %.0f%%): %s", suggestion.Confidence*100, suggestion.FixCmd)
	}

	needsApproval := true
	if suggestion.Confidence >= 0.85 && risk <= safety.RiskLow {
		needsApproval = false
	} else if suggestion.Confidence < 0.6 {
		needsApproval = true
	}

	if needsApproval {
		if !approvalFn(promptCmd, risk) {
			return nil, nil
		}
	}

	fixResult, err := e.harness.Run(ctx, args[0], args[1:])


	return fixResult, err
}

func (e *Executor) LearnPattern(ctx context.Context, failCmd, failOutput, fixCmd string, history []models.TurnRecord) error {
	var fixChainStr string
	if len(history) > 0 {
		chain := make([]string, 0, len(history)+1)
		for _, h := range history {
			chain = append(chain, h.FixCmd)
		}
		chain = append(chain, fixCmd)
		b, _ := json.Marshal(chain)
		fixChainStr = string(b)
	}

	return e.store.SavePattern(ctx, &models.Pattern{
		FailCmd:     failCmd,
		FailOutput:  failOutput,
		FixCmd:      fixCmd,
		SuccessRate: 1.0,
		UseCount:    1,
		Efficiency:  1.0 / float64(len(history)+1),
		Embedding:   nil,
		FixChain:    fixChainStr,
	})
}

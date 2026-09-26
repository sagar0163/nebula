package agent

import (
	"testing"
	"context"
	"strings"

	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
)

func TestParseSuggestion(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantFix  string
		wantExp  string
	}{
		{
			name:     "valid json",
			response: `{"fix": "echo ok", "explanation": "it prints ok"}`,
			wantFix:  "echo ok",
			wantExp:  "it prints ok",
		},
		{
			name:     "json with markdown fences",
			response: "```json\n{\"fix\": \"ls -la\", \"explanation\": \"list all\"}\n```",
			wantFix:  "ls -la",
			wantExp:  "list all",
		},
		{
			name:     "legacy regex format",
			response: "Here is the fix:\nFIX: go mod tidy\nEXPLANATION: missing deps",
			wantFix:  "go mod tidy",
			wantExp:  "missing deps",
		},
		{
			name:     "invalid json falls back to regex",
			response: "{\"fix\": \"broken json\nFIX: cat file\nEXPLANATION: read file",
			wantFix:  "cat file",
			wantExp:  "read file",
		},
		{
			name:     "empty response",
			response: "",
			wantFix:  "",
			wantExp:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSuggestion("bad cmd", tc.response)
			if tc.wantFix == "" {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil result")
			}
			if got.FixCmd != tc.wantFix {
				t.Errorf("got fix %q, want %q", got.FixCmd, tc.wantFix)
			}
			if got.Explanation != tc.wantExp {
				t.Errorf("got explanation %q, want %q", got.Explanation, tc.wantExp)
			}
		})
	}
}

func TestRecallPatternChain(t *testing.T) {
	store, _ := memory.New(t.TempDir() + "/test.db")
	store.SavePattern(context.Background(), &models.Pattern{
		FailCmd: "git push",
		FailOutput: "error: failed to push",
		FixCmd: "git push -u origin main",
		FixChain: `["git fetch", "git rebase", "git push -u origin main"]`,
	})
	
	planner := NewPlanner(nil, store, false)
	
	// First attempt -> git fetch
	s1, _ := planner.recallPattern(context.Background(), "git push", "error: failed to push", 0)
	if s1 == nil || s1.FixCmd != "git fetch" {
		t.Errorf("expected git fetch, got %+v", s1)
	}
	
	// Second attempt -> git rebase
	s2, _ := planner.recallPattern(context.Background(), "git push", "error: failed to push", 1)
	if s2 == nil || s2.FixCmd != "git rebase" {
		t.Errorf("expected git rebase, got %+v", s2)
	}
	
	// Third attempt -> final fix
	s3, _ := planner.recallPattern(context.Background(), "git push", "error: failed to push", 2)
	if s3 == nil || s3.FixCmd != "git push -u origin main" {
		t.Errorf("expected git push -u origin main, got %+v", s3)
	}
}

func TestBuildDiagnosePrompt_NoDuplicateFixes(t *testing.T) {
	history := []models.TurnRecord{
		{
			FixCmd:    `import "strings"`,
			Output:    "cannot use \"twelve\" (untyped string constant) as int value",
			ExitCode:  1,
			Reasoning: "The error mentions a string, so strings is probably needed.",
		},
	}

	prompt := buildDiagnosePrompt("go build ./...", "cannot use \"twelve\" as int", "", 1, history, nil, false)

	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "already been tried") && !strings.Contains(lower, "do not suggest") {
		t.Errorf("prompt missing anti-repetition guard; got:\n%s", prompt)
	}

	if !strings.Contains(prompt, `import "strings"`) {
		t.Errorf("prompt does not list the tried fix command %q; got:\n%s", `import "strings"`, prompt)
	}
}

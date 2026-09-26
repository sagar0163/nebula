package agent

import (
	"testing"
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

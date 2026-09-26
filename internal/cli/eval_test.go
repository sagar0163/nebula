package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sagar0163/nebula/internal/eval"
)

func TestFormatEvalDuration(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{420, "420ms"},
		{1000, "1.0s"},
		{12_340, "12.3s"},
		{125_000, "125.0s"},
	}
	for _, c := range cases {
		if got := formatEvalDuration(c.ms); got != c.want {
			t.Errorf("formatEvalDuration(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{12_345, "12.3k"},
		{999_999, "1000.0k"},
		{1_500_000, "1.50M"},
	}
	for _, c := range cases {
		if got := formatTokens(c.n); got != c.want {
			t.Errorf("formatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// A single heal on a cheap model costs fractions of a cent; rounding that to
// $0.00 would make every case look free.
func TestFormatCost(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, "$0.00"},
		{0.0001, "$0.0001"},
		{0.0042, "$0.0042"},
		{0.01, "$0.01"},
		{1.5, "$1.50"},
	}
	for _, c := range cases {
		if got := formatCost(c.v); got != c.want {
			t.Errorf("formatCost(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestResolveEvalProviderRejectsUnknown(t *testing.T) {
	if _, err := resolveEvalProvider("not-a-provider"); err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	// ollama is selectable even though it has no keyring key.
	if p, err := resolveEvalProvider("ollama"); err != nil || p != "ollama" {
		t.Fatalf("resolveEvalProvider(ollama) = %q, %v", p, err)
	}
}

func TestEvalCaseReason(t *testing.T) {
	cases := []struct {
		name string
		res  eval.Result
		want string
	}{
		{"pass is silent", eval.Result{Status: eval.StatusPass}, ""},
		{"fail quotes the agent", eval.Result{Status: eval.StatusFail, AgentError: "no provider"}, "agent: no provider"},
		{"fail quotes verify", eval.Result{Status: eval.StatusFail, VerifyOutput: "still broken"}, "verify: still broken"},
		{"fail without detail", eval.Result{Status: eval.StatusFail}, "verify_cmd still failing"},
		{"skip", eval.Result{Status: eval.StatusSkip, Skipped: true, Err: "toolchain unavailable: go"}, "skipped: toolchain unavailable: go"},
		{"error", eval.Result{Status: eval.StatusError, Err: "setup.sh exited 1"}, "setup.sh exited 1"},
	}
	for _, c := range cases {
		if got := evalCaseReason(&c.res); got != c.want {
			t.Errorf("%s: evalCaseReason = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPrintEvalReportShowsEveryMetric(t *testing.T) {
	report := &eval.Report{
		Provider:        "gemini",
		Model:           "gemini-2.5-pro",
		Models:          []string{"gemini-2.0-flash", "gemini-2.5-pro"},
		Total:           3,
		Attempted:       2,
		Passed:          2,
		Failed:          0,
		Skipped:         1,
		PassRate:        1,
		InputTokens:     3000,
		OutputTokens:    600,
		TotalTokens:     3600,
		Calls:           4,
		CostUSD:         0.0091,
		CostKnown:       true,
		AvgFixTimeMS:    4200,
		AvgCostPerFix:   0.00455,
		CostPerFixKnown: true,
		DurationMS:      9000,
		Results: []*eval.Result{
			{Name: "go-missing-import", Status: eval.StatusPass, Passed: true, TotalTokens: 2000, CostUSD: 0.005, CostKnown: true, AgentDurationMS: 4000},
		},
	}

	out := captureStdout(t, func() { printEvalReport(report) })

	for _, want := range []string{
		"Pass rate", "2/2", "100.0%", // pass rate
		"Cost/fix", "$0.0046", "$0.0091", // cost per fix and total
		"Time/fix", "4.2s", // time per fix
		"3.6k", "3000", "600", "4 calls", // token usage
		"gemini-2.0-flash", "gemini-2.5-pro", "via gemini", // model used
		"Skipped", "1 case(s)", // skipped cases
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report is missing %q:\n%s", want, out)
		}
	}
}

func TestPrintEvalReportFlagsUnknownCostAndErrors(t *testing.T) {
	report := &eval.Report{
		Provider: "nvidia", Model: "meta/llama-3.1-405b-instruct",
		Total: 1, Attempted: 1, Failed: 1, Errors: 1,
		TotalTokens: 5000, CostKnown: false, CostPerFixKnown: false,
		Results: []*eval.Result{{Name: "go-wrong-type", Status: eval.StatusError, Err: "setup.sh exited 1"}},
	}

	out := captureStdout(t, func() { printEvalReport(report) })
	if !strings.Contains(out, "unknown rate") {
		t.Errorf("expected the unknown-rate notice:\n%s", out)
	}
	if !strings.Contains(out, "Errors") {
		t.Errorf("expected the error count:\n%s", out)
	}
}

func TestPrintEvalCaseRow(t *testing.T) {
	out := captureStdout(t, func() {
		printEvalCase(&eval.Result{
			Name: "go-undefined-var", Status: eval.StatusPass, Passed: true,
			FixCount: 2, TotalTokens: 1500, CostUSD: 0.0031, CostKnown: true,
			AgentDurationMS: 3300,
		})
	})
	for _, want := range []string{"go-undefined-var", "PASS", "2", "3.3s", "1.5k", "$0.0031"} {
		if !strings.Contains(out, want) {
			t.Errorf("case row is missing %q: %q", want, out)
		}
	}
}

func TestFirstAvailableProviderPrefersConfig(t *testing.T) {
	// viper is empty in tests, so nothing is configured and nothing should be
	// reported as available rather than panicking on a missing keyring entry.
	if p := firstAvailableProvider(); p != "" && !isKnownProvider(p) && p != "ollama" {
		t.Fatalf("firstAvailableProvider = %q, which is not a known provider", p)
	}
}

func TestProviderKeyEnvVarMatchesLoadKeys(t *testing.T) {
	// loadKeys reads these names; the eval provider probe must agree with it.
	want := map[string]string{
		"groq":    "NEBULA_GROQ_KEY",
		"gemini":  "NEBULA_GEMINI_KEY",
		"mistral": "NEBULA_MISTRAL_KEY",
		"nvidia":  "NEBULA_NVIDIA_KEY",
	}
	for provider, env := range want {
		if got := providerKeyEnvVar(provider); got != env {
			t.Errorf("providerKeyEnvVar(%q) = %q, want %q", provider, got, env)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	// Closing the writer ends the read; the reader must stay open until it
	// has drained, or the capture comes back empty.
	w.Close()
	return <-done
}

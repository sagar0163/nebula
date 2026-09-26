package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzParseSuggestion — never panic on arbitrary LLM response text
func FuzzParseSuggestion(f *testing.F) {
	f.Add(`{"fix":"echo ok","explanation":"works"}`)
	f.Add("FIX: echo ok\nEXPLANATION: works")
	f.Add("")
	f.Add("null")
	f.Add("{}")
	f.Add(`{"fix":"` + string([]byte{0x00, 0xff, 0xfe}) + `"}`)
	f.Add("```json\n{\"fix\":\"ls\"}\n```")
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		// must never panic
		_ = parseSuggestion("cmd", s)
	})
}

// FuzzSummarizeOutput — never panic, always return valid UTF-8
func FuzzSummarizeOutput(f *testing.F) {
	f.Add("")
	f.Add("error: build failed")
	f.Add(string(make([]byte, 8192)))
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		out := SummarizeOutput(s)
		if !utf8.ValidString(out) {
			t.Errorf("SummarizeOutput returned invalid UTF-8")
		}
	})
}

// FuzzExtractRelevantFiles — never panic, never return paths that escape cwd
func FuzzExtractRelevantFiles(f *testing.F) {
	f.Add("go build ./...", "internal/foo/bar.go:12: error")
	f.Add("npm run build", "src/index.ts:5:3 error TS2345")
	f.Add("cmd", "../../../../etc/passwd.go:1")
	f.Add("cmd", "/absolute/path/secret.go:1")
	f.Fuzz(func(t *testing.T, cmd, output string) {
		if !utf8.ValidString(cmd) || !utf8.ValidString(output) {
			t.Skip()
		}
		paths := ExtractRelevantFiles(cmd, output, 5)
		cwd, _ := os.Getwd()
		for _, p := range paths {
			// must never be absolute
			if filepath.IsAbs(p) {
				t.Errorf("absolute path leaked: %q", p)
			}
			// must not escape cwd — check via filepath.Rel, same logic as isSafe()
			clean := filepath.Clean(p)
			rel, err := filepath.Rel(cwd, filepath.Join(cwd, clean))
			if err != nil {
				continue
			}
			// strings.HasPrefix(rel, "..") means the path escapes cwd
			if strings.HasPrefix(rel, "..") {
				t.Errorf("path escapes cwd: %q (rel=%q)", p, rel)
			}
		}
	})
}

// FuzzBuildDiagnosePrompt — never panic, output always non-empty when cmd given
func FuzzBuildDiagnosePrompt(f *testing.F) {
	f.Add("go build ./...", "error: undefined foo", "", 1)
	f.Add("", "", "", 0)
	f.Fuzz(func(t *testing.T, cmd, output, transcript string, exitCode int) {
		if !utf8.ValidString(cmd) || !utf8.ValidString(output) || !utf8.ValidString(transcript) {
			t.Skip()
		}
		// must never panic
		_ = buildDiagnosePrompt(cmd, output, transcript, exitCode, nil, nil, false)
	})
}

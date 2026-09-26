package agent

import (
	"strings"
	"testing"
)

// Benchmark the current naive string concatenation vs strings.Builder
func BenchmarkStringConcat(b *testing.B) {
	tokens := []string{"hello", " ", "world", " ", "this", " ", "is", " ", "a", " ", "test"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result string
		// Simulate receiving 1000 tokens
		for j := 0; j < 1000; j++ {
			result += tokens[j%len(tokens)]
		}
	}
}

func BenchmarkStringBuilder(b *testing.B) {
	tokens := []string{"hello", " ", "world", " ", "this", " ", "is", " ", "a", " ", "test"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var builder strings.Builder
		// Simulate receiving 1000 tokens
		for j := 0; j < 1000; j++ {
			builder.WriteString(tokens[j%len(tokens)])
		}
		_ = builder.String()
	}
}

// BenchmarkSummarizeOutput_Short — sub-threshold (no-op path)
func BenchmarkSummarizeOutput_Short(b *testing.B) {
	input := strings.Repeat("normal build line\n", 50) // ~900 bytes
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SummarizeOutput(input)
	}
}

// BenchmarkSummarizeOutput_Long — full summarisation path
func BenchmarkSummarizeOutput_Long(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		if i == 250 {
			sb.WriteString("error: undefined variable x\n")
		} else {
			sb.WriteString("this is a normal build output line with no issues\n")
		}
	}
	input := sb.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SummarizeOutput(input)
	}
}

// BenchmarkExtractRelevantFiles — typical Go build error
func BenchmarkExtractRelevantFiles(b *testing.B) {
	cmd := "go build ./..."
	output := "internal/agent/planner.go:42:5: undefined: foo\ninternal/agent/agent.go:12:3: cannot use x"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExtractRelevantFiles(cmd, output, 3)
	}
}

// BenchmarkParseSuggestion_JSON — happy path (JSON response)
func BenchmarkParseSuggestion_JSON(b *testing.B) {
	response := `{"fix":"go mod tidy","explanation":"missing dependency","reasoning":"go.sum is outdated","confidence":0.9}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseSuggestion("go build ./...", response)
	}
}

// BenchmarkParseSuggestion_Fallback — legacy FIX: path
func BenchmarkParseSuggestion_Fallback(b *testing.B) {
	response := "FIX: go mod tidy\nEXPLANATION: missing dependency"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseSuggestion("go build ./...", response)
	}
}

// BenchmarkProgressCheck — sha256 content hash path
func BenchmarkProgressCheck(b *testing.B) {
	prev := strings.Repeat("error: build failed with undefined foo\n", 10)
	next := strings.Repeat("error: build failed with undefined bar\n", 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = progressCheck(1, prev, 1, next)
	}
}

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

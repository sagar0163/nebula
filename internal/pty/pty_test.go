package pty

import (
	"context"
	"strings"
	"testing"
)

func TestCappedBuffer_Run(t *testing.T) {
	// Create a harness with a small cap for testing
	h := NewHarness(0, 1024)
	
	// Create 5000 bytes of output using python
	ctx := context.Background()
	res, err := h.Run(ctx, "python3", []string{"-c", "print('x' * 5000)"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	
	if len(res.Stdout) > 1024 {
		t.Errorf("expected stdout length <= 1024, got %d", len(res.Stdout))
	}
	
	// Verify it captured the end of the output (which is 'x' followed by newline)
	if !strings.Contains(string(res.Stdout), "x") {
		t.Errorf("stdout doesn't contain expected output")
	}
}

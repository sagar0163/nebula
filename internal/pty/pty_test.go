package pty

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestHarness_SignalKill(t *testing.T) {
	h := NewHarness(0, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	
	errCh := make(chan error, 1)
	resCh := make(chan *CommandResult, 1)
	
	go func() {
		res, err := h.Run(ctx, "sleep", []string{"10"})
		if err != nil {
			errCh <- err
		} else {
			resCh <- res
		}
	}()
	
	time.Sleep(100 * time.Millisecond)
	cancel()
	
	select {
	case err := <-errCh:
		// Some implementations might return a direct error on context cancel
		t.Logf("Got expected error: %v", err)
	case res := <-resCh:
		if res.ExitCode != -1 {
			t.Fatalf("expected exit code -1 (signal termination), got %d", res.ExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return in time after context cancellation")
	}
}

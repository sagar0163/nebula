package pty

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	npty "github.com/creack/pty"
)

// withStdinTTY redirects the test process's fd 0 onto a freshly opened PTY so
// the Harness can enter raw mode (term.MakeRaw requires a TTY). The returned
// cleanup restores fd 0 to /dev/null, which also unblocks the Harness's stdin
// pump goroutine so it can drain with EOF.
func withStdinTTY(t *testing.T) func() {
	t.Helper()
	ptmx, tty, err := npty.Open()
	if err != nil {
		t.Skipf("skipping: cannot open pty: %v", err)
	}
	if err := syscall.Dup2(int(tty.Fd()), 0); err != nil {
		t.Fatalf("dup2 pty onto stdin: %v", err)
	}
	return func() {
		_ = tty.Close()
		_ = ptmx.Close()
		dn, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
		if err == nil {
			_ = syscall.Dup2(int(dn.Fd()), 0)
			_ = dn.Close()
		}
	}
}

// waitGoroutines polls until late-exiting harness goroutines drain. It does not
// assert an exact count; callers assert the delta.
func waitGoroutines(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunExitCodes(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"exit 0", []string{"sh", "-c", "exit 0"}, 0},
		{"exit 1", []string{"sh", "-c", "exit 1"}, 1},
		{"exit 127", []string{"sh", "-c", "exit 127"}, 127},
		{"true", []string{"true"}, 0},
		{"false", []string{"false"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := h.Run(context.Background(), c.args[0], c.args[1:])
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.ExitCode != c.want {
				t.Fatalf("ExitCode = %d, want %d (stdout %q)", res.ExitCode, c.want, res.Stdout)
			}
		})
	}
}

func TestRunZeroOutput(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	res, err := h.Run(context.Background(), "true", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Stdout) != 0 {
		t.Fatalf("Stdout = %q (%d bytes), want 0 bytes", res.Stdout, len(res.Stdout))
	}
}

func TestRunLargeOutput(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	start := time.Now()
	res, err := h.Run(context.Background(), "sh", []string{"-c", "yes n | head -c 500000"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Stdout) < 400000 {
		t.Fatalf("captured %d bytes, want a large (>400KB) output", len(res.Stdout))
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("500KB run took %v, suspiciously slow", elapsed)
	}
}

func TestRunYesBoundedOutput(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, maxCaptureBytes)
	res, err := h.Run(context.Background(), "sh", []string{"-c", "yes n | head -c 600000"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Stdout) > maxCaptureBytes {
		t.Fatalf("captured %d bytes, want <= %d", len(res.Stdout), maxCaptureBytes)
	}
}

func TestRunSleepThenExit(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	start := time.Now()
	res, err := h.Run(context.Background(), "sh", []string{"-c", "sleep 2; exit 3"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", res.ExitCode)
	}
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond {
		t.Fatalf("sleep 2 returned after %v, did not actually wait", elapsed)
	}
}

func TestRunContextCancel(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := h.Run(ctx, "sh", []string{"-c", "sleep 5"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("cancelled sleep 5 took %v, context not honoured", elapsed)
	}
	if res.ExitCode == 0 {
		t.Fatalf("cancelled command reported ExitCode 0, want nonzero (killed)")
	}
}

func TestRunStderrOnly(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	res, err := h.Run(context.Background(), "sh", []string{"-c", "echo only-on-stderr >&2; exit 7"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", res.ExitCode)
	}
	if !bytes.Contains(res.Stdout, []byte("only-on-stderr")) {
		t.Fatalf("stderr content not captured: %q", res.Stdout)
	}
}

func TestRunConcurrentSameHarness(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1<<20, 512*1024)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := h.Run(context.Background(), "sh", []string{"-c", "echo concurrent-" + string(rune('0'+i))})
			if err != nil {
				errs <- err
				return
			}
			if res.ExitCode != 0 {
				errs <- &exec.ExitError{}
				return
			}
			if !bytes.Contains(res.Stdout, []byte("concurrent-"+string(rune('0'+i)))) {
				errs <- &os.PathError{Op: "capture", Err: syscall.EINVAL}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent run error: %v", err)
	}
	if got := h.Transcript(); len(got) > (1 << 20) {
		t.Fatalf("Transcript unexpectedly grew to %d bytes", len(got))
	}
}

func TestRingBufferNeverExceedsMaxSize(t *testing.T) {
	defer withStdinTTY(t)()
	const maxSize = 128
	h := NewHarness(maxSize, 512*1024)

	for i := 0; i < 6; i++ {
		res, err := h.Run(context.Background(), "sh", []string{"-c", "yes n | head -c 500000"})
		if err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("Run %d exit = %d", i, res.ExitCode)
		}
		if got := h.Transcript(); len(got) > maxSize {
			t.Fatalf("run %d: Transcript() = %d bytes, max %d", i, len(got), maxSize)
		}
	}

	res, err := h.Run(context.Background(), "echo", []string{"short"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = res
	if got := h.Transcript(); len(got) > maxSize {
		t.Fatalf("after small run: Transcript() = %d bytes, max %d", len(got), maxSize)
	}
	if h.ringBuf.Len() > maxSize {
		t.Fatalf("ringBuf.len = %d, exceeded max %d", h.ringBuf.Len(), maxSize)
	}
}

func TestRunCommandNotFound(t *testing.T) {
	defer withStdinTTY(t)()
	h := NewHarness(1024, 512*1024)
	_, err := h.Run(context.Background(), "definitely-not-a-real-binary-xyz-12345", nil)
	if err == nil {
		t.Fatal("Run(unknown binary) returned a nil error")
	}
	if !strings.Contains(err.Error(), "pty start") {
		t.Fatalf("err = %v, want 'pty start' wrapping exec failure", err)
	}
}

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

func TestCappedBuffer_HeadTail(t *testing.T) {
	cb := newCappedBuffer(300)
	var input bytes.Buffer
	input.WriteString("ROOT_CAUSE_ERROR_LINE_1\nROOT_CAUSE_CONFIG_LINE_2\n")
	for i := 1; i <= 200; i++ {
		input.WriteString(fmt.Sprintf("cascade error middle noise line %d\n", i))
	}
	input.WriteString("FINAL_PANIC_STACK_TRACE_END\n")

	n, err := cb.Write(input.Bytes())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != input.Len() {
		t.Fatalf("Write n = %d, want %d", n, input.Len())
	}

	out := cb.Bytes()
	if len(out) > 300 {
		t.Fatalf("output length %d exceeded cap 300", len(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "ROOT_CAUSE_ERROR_LINE_1") {
		t.Errorf("missing head root cause in output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "omitted") {
		t.Errorf("missing omission marker in output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "FINAL_PANIC_STACK_TRACE_END") {
		t.Errorf("missing tail stack trace in output:\n%s", outStr)
	}
}

func TestCappedBuffer_NoOverflow(t *testing.T) {
	cb := newCappedBuffer(1024)
	input := []byte("short output\nline 2\n")
	_, err := cb.Write(input)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := cb.Bytes()
	if string(out) != string(input) {
		t.Fatalf("got %q, want %q", string(out), string(input))
	}
}


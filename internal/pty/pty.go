package pty

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// CommandResult holds the captured output and exit code of a PTY command.
type CommandResult struct {
	ExitCode int
	Stdout   []byte // captured from PTY (includes stderr in a PTY)
	Elapsed  int64  // milliseconds
}

// Harness wraps a PTY and tees output to the user's terminal
// while also capturing it for AI analysis.
type Harness struct {
	mu          sync.Mutex
	ringBuf     *bytes.Buffer // rolling transcript for AI context
	maxRingSize int
}

// NewHarness creates a PTY harness with the given ring buffer size.
func NewHarness(ringSize int) *Harness {
	return &Harness{
		ringBuf:     &bytes.Buffer{},
		maxRingSize: ringSize,
	}
}

// Run executes the command through a PTY, streaming output to the
// user's terminal while capturing it for analysis.
// Returns once the command exits or ctx is cancelled.
func (h *Harness) Run(ctx context.Context, name string, args []string) (*CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), sentinelEnv()...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	// Put the user's terminal in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Pipe stdin to PTY.
	go func() { io.Copy(ptmx, os.Stdin) }() //nolint:errcheck

	// Tee PTY output: → user's terminal + ring buffer.
	var capture bytes.Buffer
	writer := io.MultiWriter(os.Stdout, &capture, h)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(writer, ptmx) //nolint:errcheck
	}()

	err = cmd.Wait()
	wg.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &CommandResult{
		ExitCode: exitCode,
		Stdout:   capture.Bytes(),
	}, nil
}

// Write implements io.Writer for the ring buffer.
func (h *Harness) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.ringBuf.Write(p)

	// Trim to max size by dropping the oldest bytes.
	if h.ringBuf.Len() > h.maxRingSize {
		overflow := h.ringBuf.Len() - h.maxRingSize
		h.ringBuf.Next(overflow)
	}

	return len(p), nil
}

// Transcript returns the current ring buffer contents.
func (h *Harness) Transcript() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ringBuf.String()
}

// sentinelEnv returns env vars injected into the child shell
// so Nebula can parse command boundaries from the PTY stream.
func sentinelEnv() []string {
	// PROMPT_COMMAND emits a structured sentinel after each command.
	sentinel := `PROMPT_COMMAND='printf "\033]133;NEBULA;exit=%d\033\\" $?'`
	return []string{sentinel}
}

package pty

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// maxCaptureBytes bounds how much of a command's PTY output we retain so a
// high-volume command (e.g. `yes`) cannot blow up memory unboundedly.
const maxCaptureBytes = 512 * 1024

// CommandResult holds the captured output and exit code of a PTY command.
type CommandResult struct {
	ExitCode int
	Stdout   []byte // captured from PTY (includes stderr in a PTY)
	Elapsed  int64  // milliseconds
}

// Harness wraps a PTY and tees output to the user's terminal
// while also capturing it for AI analysis.
type Harness struct {
	mu             sync.Mutex
	ringBuf        *bytes.Buffer // rolling transcript for AI context
	maxRingSize    int
	maxCaptureSize int
}

// NewHarness creates a PTY harness with the given ring buffer size.
func NewHarness(ringSize int, captureSize int) *Harness {
	return &Harness{
		ringBuf:        &bytes.Buffer{},
		maxRingSize:    ringSize,
		maxCaptureSize: captureSize,
	}
}

// Run executes the command through a PTY, streaming output to the
// user's terminal while capturing it for analysis.
// Returns once the command exits or ctx is cancelled.
func (h *Harness) Run(ctx context.Context, name string, args []string) (*CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), sentinelEnv()...)
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
	cmd.WaitDelay = 3 * time.Second

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	// Put the user's terminal in raw mode, if it's actually a terminal.
	isTerm := term.IsTerminal(int(os.Stdin.Fd()))
	if isTerm {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return nil, fmt.Errorf("raw mode: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)

		// Pipe stdin to PTY.
		done := make(chan struct{})
		defer close(done)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-done:
					return
				case b := <-StdinBytes:
					ptmx.Write(b)
				}
			}
		}()
	}

	// Tee PTY output: → user's terminal + ring buffer.
	capSize := h.maxCaptureSize
	if capSize <= 0 {
		capSize = maxCaptureBytes
	}
	capture := newCappedBuffer(capSize)
	writer := io.MultiWriter(os.Stdout, capture, h)

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

func newCappedBuffer(maxCap int) *cappedBuffer {
	headCap := 2048
	if maxCap/4 < headCap {
		headCap = maxCap / 4
	}
	if headCap < 0 {
		headCap = 0
	}
	return &cappedBuffer{
		cap:     maxCap,
		headCap: headCap,
		head:    &bytes.Buffer{},
		tail:    &bytes.Buffer{},
	}
}

type cappedBuffer struct {
	mu         sync.Mutex
	cap        int
	headCap    int
	head       *bytes.Buffer
	tail       *bytes.Buffer
	totalBytes int64
	totalLines int64
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalBytes += int64(len(p))
	c.totalLines += int64(bytes.Count(p, []byte("\n")))

	// Fill head buffer up to headCap
	if c.head.Len() < c.headCap {
		need := c.headCap - c.head.Len()
		if len(p) <= need {
			c.head.Write(p)
		} else {
			c.head.Write(p[:need])
		}
	}

	// Append to rolling tail buffer
	c.tail.Write(p)
	if c.tail.Len() > c.cap {
		overflow := c.tail.Len() - c.cap
		c.tail.Next(overflow)
	}

	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If total output is within capacity, return full output
	if c.totalBytes <= int64(c.cap) {
		out := make([]byte, c.tail.Len())
		copy(out, c.tail.Bytes())
		return out
	}

	// If capacity is too small for head+marker, fall back to plain tail
	const minHeadCap = 16
	if c.headCap < minHeadCap || c.cap < 128 {
		out := make([]byte, c.tail.Len())
		copy(out, c.tail.Bytes())
		if len(out) > c.cap {
			return out[len(out)-c.cap:]
		}
		return out
	}

	headBytes := c.head.Bytes()
	headLen := len(headBytes)

	// Rough estimation for tail budget to format marker
	estMarker := fmt.Sprintf("\n[... %d lines / %d bytes omitted ...]\n", c.totalLines, c.totalBytes)
	tailBudget := c.cap - headLen - len(estMarker)
	if tailBudget < 0 {
		tailBudget = 0
	}

	tailRaw := c.tail.Bytes()
	var tailSlice []byte
	if len(tailRaw) > tailBudget {
		tailSlice = tailRaw[len(tailRaw)-tailBudget:]
	} else {
		tailSlice = tailRaw
	}

	headLines := int64(bytes.Count(headBytes, []byte("\n")))
	tailLines := int64(bytes.Count(tailSlice, []byte("\n")))
	omittedLines := c.totalLines - headLines - tailLines
	if omittedLines < 0 {
		omittedLines = 0
	}
	omittedBytes := c.totalBytes - int64(headLen) - int64(len(tailSlice))
	if omittedBytes < 0 {
		omittedBytes = 0
	}

	marker := fmt.Sprintf("\n[... %d lines / %d bytes omitted ...]\n", omittedLines, omittedBytes)

	// Re-adjust exact fit with final formatted marker
	tailBudget = c.cap - headLen - len(marker)
	if tailBudget < 0 {
		tailBudget = 0
	}
	if len(tailRaw) > tailBudget {
		tailSlice = tailRaw[len(tailRaw)-tailBudget:]
	} else {
		tailSlice = tailRaw
	}

	res := make([]byte, 0, headLen+len(marker)+len(tailSlice))
	res = append(res, headBytes...)
	res = append(res, []byte(marker)...)
	res = append(res, tailSlice...)
	return res
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

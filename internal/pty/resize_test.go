//go:build !windows

package pty

import (
	"syscall"
	"testing"
	"time"

	cpty "github.com/creack/pty"
)

func TestWatchResize(t *testing.T) {
	ptmx, tty, err := cpty.Open()
	if err != nil {
		t.Skipf("skipping: cannot open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	stop := watchResize(ptmx)
	defer stop()

	// Send SIGWINCH to self to exercise signal path
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)

	time.Sleep(50 * time.Millisecond)
}

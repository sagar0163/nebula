//go:build !windows

package pty

import (
	"os"
	"os/signal"
	"syscall"

	cpty "github.com/creack/pty"
)

// watchResize listens for SIGWINCH signals and resizes the PTY to match stdin.
// It returns a cleanup function that stops listening and terminates the goroutine.
func watchResize(ptmx *os.File) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)

	// Inherit initial size immediately if stdin is a terminal.
	_ = cpty.InheritSize(os.Stdin, ptmx)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				_ = cpty.InheritSize(os.Stdin, ptmx)
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}

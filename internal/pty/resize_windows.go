//go:build windows

package pty

import "os"

// watchResize is a no-op on Windows since SIGWINCH is POSIX-specific.
func watchResize(ptmx *os.File) func() {
	return func() {}
}

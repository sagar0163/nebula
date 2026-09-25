//go:build windows

package pty

import "os/exec"

// killProcessGroup is a no-op on Windows: there is no POSIX process group to
// signal. cmd.WaitDelay still bounds the wait by force-killing the process
// after the configured delay, so cancellation cannot hang indefinitely.
func killProcessGroup(cmd *exec.Cmd) error {
	return nil
}

//go:build !windows

package pty

import (
	"os/exec"
	"syscall"
)

// killProcessGroup sends SIGTERM to the whole process group led by cmd.
// The negative pid targets the group, so any grandchildren the shell spawned
// are signalled too.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

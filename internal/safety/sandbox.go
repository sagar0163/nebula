package safety

import (
	"os/exec"
	"runtime"
)

// SandboxLevel controls how tightly the process is isolated.
type SandboxLevel int

const (
	SandboxNone     SandboxLevel = iota // no isolation
	SandboxReadOnly                     // read-only filesystem, no network
	SandboxNetwork                      // read-only filesystem, network allowed
)

// WrapCommand takes a command name + args and returns a potentially wrapped
// name + args that will run the command inside the sandbox.
// Returns the original name+args unchanged when sandboxing is unavailable.
func WrapCommand(name string, args []string, level SandboxLevel) (string, []string) {
	if level == SandboxNone {
		return name, args
	}
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("bwrap"); err != nil {
			return name, args
		}
		return wrapBwrap(name, args, level)
	case "darwin":
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			return name, args
		}
		return wrapSandboxExec(name, args, level)
	default:
		return name, args
	}
}

// wrapBwrap builds a bubblewrap invocation with the process root bind-mounted
// read-only, a tmpfs on /tmp, and networking unshared for SandboxReadOnly.
func wrapBwrap(name string, args []string, level SandboxLevel) (string, []string) {
	wrapped := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}
	if level == SandboxReadOnly {
		wrapped = append(wrapped, "--unshare-net")
	}
	wrapped = append(wrapped, "--", name)
	wrapped = append(wrapped, args...)
	return "bwrap", wrapped
}

// wrapSandboxExec builds a sandbox-exec invocation carrying a minimal Seatbelt
// profile that denies writes and, for SandboxReadOnly, networking as well.
func wrapSandboxExec(name string, args []string, level SandboxLevel) (string, []string) {
	profile := "(version 1)\n(allow default)\n(deny file-write*)"
	if level == SandboxReadOnly {
		profile += "\n(deny network*)"
	}
	wrapped := []string{"-p", profile, "--", name}
	wrapped = append(wrapped, args...)
	return "sandbox-exec", wrapped
}

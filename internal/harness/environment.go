package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Environment encapsulates all filesystem and shell operations an agent needs
// against a specific repository working directory. Passing Environment
// explicitly (rather than relying on os.Chdir) is the foundation for running
// multiple instances concurrently.
type Environment interface {
	// WorkDir returns the absolute path to the repository root.
	WorkDir() string

	// Run executes a command inside WorkDir and returns its combined output.
	Run(ctx context.Context, args ...string) ([]byte, error)

	// ReadFile reads a file at path relative to WorkDir (absolute paths are
	// accepted as-is).
	ReadFile(path string) ([]byte, error)

	// WriteFile writes data to path relative to WorkDir (absolute paths are
	// accepted as-is). Parent directories are created as needed.
	WriteFile(path string, data []byte) error

	// GitDiff returns the output of "git diff" in WorkDir.
	GitDiff(ctx context.Context) (string, error)
}

// LocalEnvironment is the concrete, single-machine implementation of
// Environment. It uses explicit workDir paths for every operation instead of
// os.Chdir so multiple instances can coexist safely.
type LocalEnvironment struct {
	workDir string
}

// NewLocalEnvironment creates a LocalEnvironment rooted at workDir. workDir
// must be an absolute path to an existing directory.
func NewLocalEnvironment(workDir string) (*LocalEnvironment, error) {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("environment: cannot resolve workDir %q: %w", workDir, err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("environment: workDir %q does not exist or is not a directory", abs)
	}
	return &LocalEnvironment{workDir: abs}, nil
}

// WorkDir returns the absolute repository root.
func (e *LocalEnvironment) WorkDir() string { return e.workDir }

// Run executes args as a command inside WorkDir and returns combined stdout+stderr.
func (e *LocalEnvironment) Run(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("environment.Run: no arguments provided")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = e.workDir
	return cmd.CombinedOutput()
}

// resolvePath returns an absolute path: if path is already absolute it is
// returned unchanged; otherwise it is joined with WorkDir.
func (e *LocalEnvironment) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(e.workDir, path)
}

// ReadFile reads the file at path (relative to WorkDir or absolute).
func (e *LocalEnvironment) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(e.resolvePath(path))
}

// WriteFile writes data to path (relative to WorkDir or absolute), creating
// any missing parent directories.
func (e *LocalEnvironment) WriteFile(path string, data []byte) error {
	abs := e.resolvePath(path)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return fmt.Errorf("environment.WriteFile: cannot create parent directory: %w", err)
	}
	return os.WriteFile(abs, data, 0644)
}

// GitDiff returns the output of "git diff" in WorkDir.
func (e *LocalEnvironment) GitDiff(ctx context.Context) (string, error) {
	out, err := e.Run(ctx, "git", "diff")
	if err != nil {
		return "", fmt.Errorf("environment.GitDiff: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

package testrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"os/exec"
)

// GoTestRunner implements TestRunner for Go projects
type GoTestRunner struct{}

// Detect checks if this is a Go project
func (r *GoTestRunner) Detect(workDir string) bool {
	// Check for go.mod
	_, err := os.Stat(filepath.Join(workDir, "go.mod"))
	return err == nil
}

// GetTestCommand returns the go test command
func (r *GoTestRunner) GetTestCommand(filter string) []string {
	cmd := []string{"go", "test", "./...", "-v"}
	if filter != "" {
		cmd = append(cmd, "-run", filter)
	}
	return cmd
}

// RunTests executes Go tests and parses the output
func (r *GoTestRunner) RunTests(ctx context.Context, workDir string, filter string) (*TestResult, error) {
	start := time.Now()
	cmd := r.GetTestCommand(filter)

	// Create command with context
	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	execCmd.Dir = workDir

	output, err := execCmd.CombinedOutput()
	duration := time.Since(start)

	result := &TestResult{
		Command:   strings.Join(cmd, " "),
		Duration:  duration,
		RawOutput: string(output),
	}

	// Parse output using shared parser
	parsed := ParseTestOutput(string(output), "go")
	result.Passed = parsed.Passed
	result.Failed = parsed.Failed
	result.Skipped = parsed.Skipped
	result.FailedTests = parsed.FailedTests

	// If error but tests ran, it's not necessarily a command error
	if err != nil && parsed.Passed == 0 && parsed.Failed == 0 {
		result.Failed = 1
		result.FailedTests = append(result.FailedTests, FailedTest{
			Name:  "test execution",
			Error: err.Error(),
		})
	}

	return result, nil
}
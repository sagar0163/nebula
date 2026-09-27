package testrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// JSTestRunner implements TestRunner for JavaScript/TypeScript projects
type JSTestRunner struct{}

// Detect checks if this is a JS/TS project
func (r *JSTestRunner) Detect(workDir string) bool {
	// Look for package.json
	_, err := os.Stat(filepath.Join(workDir, "package.json"))
	return err == nil
}

// GetTestCommand returns the npm test command
func (r *JSTestRunner) GetTestCommand(filter string) []string {
	// For SWE-bench, if there's a specific test, we might use npx jest or npm test
	// Simple fallback: just npm test
	cmd := []string{"npm", "test"}
	if filter != "" {
		cmd = append(cmd, "--", filter)
	}
	return cmd
}

// RunTests executes the JS/TS tests
func (r *JSTestRunner) RunTests(ctx context.Context, workDir string, filter string) (*TestResult, error) {
	start := time.Now()
	
	args := r.GetTestCommand(filter)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workDir
	
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	
	// We use the generic Jest parser as a catch-all since most modern JS projects use it
	result := ParseTestOutput(string(out), "javascript")
	result.Duration = duration
	result.Command = strings.Join(args, " ")
	result.RawOutput = string(out)
	
	if err != nil && result.Failed == 0 && len(result.FailedTests) == 0 {
		result.Failed = 1
		result.FailedTests = append(result.FailedTests, FailedTest{
			Name: "Command Execution",
			Error: "npm test failed to execute properly",
			StackTrace: string(out),
		})
	}
	
	return result, nil
}

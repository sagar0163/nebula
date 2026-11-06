package testrunner

import (
	"context"
	"strings"
)

// TestRunner interface for language-specific test execution
type TestRunner interface {
	// Detect checks if this runner applies to the given workspace
	Detect(workDir string) bool
	// RunTests executes tests and returns parsed results
	RunTests(ctx context.Context, workDir string, filter string) (*TestResult, error)
	// GetTestCommand returns the command to run tests (for debugging)
	GetTestCommand(filter string) []string
}
// BuildTestCommand returns a shell command string (suitable for `sh -c`) that
// runs the given tests in workDir using the detected runner, mirroring what
// the verifier executes. It returns "" if no runner is detected or no tests
// are given.
func BuildTestCommand(workDir string, tests []string) string {
	if len(tests) == 0 {
		return ""
	}
	// Join with newline so each test ID is kept intact; GetTestCommand splits on "\n".
	filter := strings.Join(tests, "\n")
	var args []string
	switch {
	case (&PythonTestRunner{}).Detect(workDir):
		args = wrapConda(workDir, (&PythonTestRunner{}).GetTestCommand(filter))
	case (&JSTestRunner{}).Detect(workDir):
		args = (&JSTestRunner{}).GetTestCommand(filter)
	case (&GoTestRunner{}).Detect(workDir):
		args = (&GoTestRunner{}).GetTestCommand(filter)
	default:
		return ""
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

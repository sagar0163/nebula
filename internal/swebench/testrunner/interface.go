package testrunner

import (
	"context"
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
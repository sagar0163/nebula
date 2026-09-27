package testrunner

import (
	"regexp"
	"strings"
	"time"
)

// TestResult represents parsed test output
type TestResult struct {
	Passed        int
	Failed        int
	Skipped       int
	FailedTests   []FailedTest
	RawOutput     string
	Duration      time.Duration
	Command       string
}

// FailedTest represents a single failing test
type FailedTest struct {
	Name        string
	File        string
	Line        int
	Error       string
	StackTrace  string
}

// ParseTestOutput parses test output from various test runners
func ParseTestOutput(output string, language string) *TestResult {
	switch language {
	case "go":
		return parseGoTestOutput(output)
	case "python":
		return parsePyTestOutput(output)
	case "java":
		return parseJUnitOutput(output)
	case "javascript", "typescript":
		return parseJestOutput(output)
	case "rust":
		return parseCargoTestOutput(output)
	default:
		return parseGenericOutput(output)
	}
}

func parseGoTestOutput(output string) *TestResult {
	result := &TestResult{}
	lines := strings.Split(output, "\n")

	// Pattern: === RUN   TestName
	runRe := regexp.MustCompile(`^=== RUN\s+(\S+)`)
	// Pattern: --- PASS: TestName (0.00s)
	passRe := regexp.MustCompile(`^--- (PASS|FAIL|SKIP):\s+(\S+)\s*\(`)
	// Pattern: FAIL: TestName (0.00s)
	// Pattern: FAIL	package	0.001s
	failRe := regexp.MustCompile(`^FAIL\s+(\S+)`)
	// Pattern: ok	package	0.001s
	okRe := regexp.MustCompile(`^ok\s+(\S+)\s+(\S+)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if m := runRe.FindStringSubmatch(line); m != nil {
			// currentTest = m[1] - not used currently
			continue
		}

		if m := passRe.FindStringSubmatch(line); m != nil {
			status := m[1]
			testName := m[2]
			switch status {
			case "PASS":
				result.Passed++
			case "FAIL":
				result.Failed++
				result.FailedTests = append(result.FailedTests, FailedTest{
					Name: testName,
					Error: "Test failed",
				})
			case "SKIP":
				result.Skipped++
			}
			continue
		}

		if m := failRe.FindStringSubmatch(line); m != nil {
			// Package-level failure
			result.Failed++
			continue
		}

		if m := okRe.FindStringSubmatch(line); m != nil {
			// Package passed - parse duration string like "0.001s"
			if d, err := time.ParseDuration(m[2]); err == nil {
				result.Duration = d
			}
			continue
		}

		// Capture stack traces for failed tests
		if result.Failed > 0 && (strings.Contains(line, "panic:") ||
			strings.Contains(line, "Error:") ||
			strings.HasPrefix(line, "	")) {
			if len(result.FailedTests) > 0 {
				result.FailedTests[len(result.FailedTests)-1].StackTrace += line + "\n"
			}
		}
	}

	return result
}

func parsePyTestOutput(output string) *TestResult {
	result := &TestResult{}
	lines := strings.Split(output, "\n")

	// Patterns for pytest
	passedRe := regexp.MustCompile(`(\d+)\s+passed`)
	failedRe := regexp.MustCompile(`(\d+)\s+failed`)
	skippedRe := regexp.MustCompile(`(\d+)\s+skipped`)
	errorRe := regexp.MustCompile(`(\d+)\s+error`)

	// Individual test results: test_file.py::TestClass::test_name PASSED/FAILED
	testLineRe := regexp.MustCompile(`^(\S+)\s+(PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)`)
	// Failure details: __________ TestName __________
	failureHeaderRe := regexp.MustCompile(`^_{5,}\s+(.+?)\s+_{5,}$`)

	var currentFailure *FailedTest
	inFailure := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Summary line
		if m := passedRe.FindStringSubmatch(line); m != nil {
			result.Passed = atoi(m[1])
		}
		if m := failedRe.FindStringSubmatch(line); m != nil {
			result.Failed = atoi(m[1])
		}
		if m := skippedRe.FindStringSubmatch(line); m != nil {
			result.Skipped = atoi(m[1])
		}
		if m := errorRe.FindStringSubmatch(line); m != nil {
			result.Failed += atoi(m[1])
		}

		// Individual test lines
		if m := testLineRe.FindStringSubmatch(line); m != nil {
			testName := m[1]
			status := m[2]
			switch status {
			case "PASSED":
				result.Passed++
			case "FAILED", "ERROR":
				result.Failed++
				currentFailure = &FailedTest{Name: testName}
				result.FailedTests = append(result.FailedTests, *currentFailure)
				inFailure = true
			case "SKIPPED", "XFAIL":
				result.Skipped++
			}
			continue
		}

		// Failure header
		if m := failureHeaderRe.FindStringSubmatch(line); m != nil {
			if currentFailure != nil {
				currentFailure.Name = m[1]
			}
			inFailure = true
			continue
		}

		// Collect failure details
		if inFailure && len(result.FailedTests) > 0 {
			lastIdx := len(result.FailedTests) - 1
			if strings.Contains(line, "AssertionError") ||
				strings.Contains(line, "Error:") ||
				strings.Contains(line, "Traceback") ||
				strings.HasPrefix(line, "  ") {
				result.FailedTests[lastIdx].StackTrace += line + "\n"
				// Extract error message
				if result.FailedTests[lastIdx].Error == "" && strings.Contains(line, "AssertionError:") {
					parts := strings.Split(line, "AssertionError:")
					if len(parts) > 1 {
						result.FailedTests[lastIdx].Error = strings.TrimSpace(parts[1])
					}
				}
			}
			// End of failure section
			if line == "" && result.FailedTests[lastIdx].StackTrace != "" {
				inFailure = false
			}
		}
	}

	return result
}

func parseJUnitOutput(output string) *TestResult {
	// Basic JUnit parsing - look for "Tests run: X, Failures: Y, Errors: Z, Skipped: W"
	summaryRe := regexp.MustCompile(`Tests run:\s*(\d+),\s*Failures:\s*(\d+),\s*Errors:\s*(\d+),\s*Skipped:\s*(\d+)`)
	if m := summaryRe.FindStringSubmatch(output); m != nil {
		return &TestResult{
			Passed:  atoi(m[1]) - atoi(m[2]) - atoi(m[3]),
			Failed:  atoi(m[2]) + atoi(m[3]),
			Skipped: atoi(m[4]),
		}
	}
	return &TestResult{}
}

func parseJestOutput(output string) *TestResult {
	result := &TestResult{}
	// Jest summary: Test Suites: X passed, Y failed
	// Tests: X passed, Y failed
	suitesRe := regexp.MustCompile(`Test Suites:\s*(\d+)\s+passed.*?(\d+)\s+failed`)
	testsRe := regexp.MustCompile(`Tests:\s*(\d+)\s+passed.*?(\d+)\s+failed`)

	if m := testsRe.FindStringSubmatch(output); m != nil {
		result.Passed = atoi(m[1])
		result.Failed = atoi(m[2])
	}
	if m := suitesRe.FindStringSubmatch(output); m != nil {
		// Suite level, already captured in tests
	}
	return result
}

func parseCargoTestOutput(output string) *TestResult {
	result := &TestResult{}
	// cargo test output: test result: ok. 5 passed; 0 failed
	passedRe := regexp.MustCompile(`(\d+)\s+passed`)
	failedRe := regexp.MustCompile(`(\d+)\s+failed`)

	if m := passedRe.FindStringSubmatch(output); m != nil {
		result.Passed = atoi(m[1])
	}
	if m := failedRe.FindStringSubmatch(output); m != nil {
		result.Failed = atoi(m[1])
	}
	return result
}

func parseGenericOutput(output string) *TestResult {
	result := &TestResult{}
	// Try to find pass/fail counts in any output
	passedRe := regexp.MustCompile(`(?i)(\d+)\s+(passed|pass|ok)`)
	failedRe := regexp.MustCompile(`(?i)(\d+)\s+(failed|fail|error)`)

	if m := passedRe.FindStringSubmatch(output); m != nil {
		result.Passed = atoi(m[1])
	}
	if m := failedRe.FindStringSubmatch(output); m != nil {
		result.Failed = atoi(m[1])
	}
	return result
}

func atoi(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
package testrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PythonTestRunner implements TestRunner for Python projects
type PythonTestRunner struct{}

// Detect checks if this is a Python project
func (r *PythonTestRunner) Detect(workDir string) bool {
	// Look for requirements.txt, setup.py, pyproject.toml, or pytest.ini
	indicators := []string{"requirements.txt", "setup.py", "pyproject.toml", "pytest.ini"}
	for _, ind := range indicators {
		if _, err := os.Stat(filepath.Join(workDir, ind)); err == nil {
			return true
		}
	}
	
	// Check for any .py files
	foundPy := false
	filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".py") {
			foundPy = true
			return filepath.SkipDir
		}
		return nil
	})
	return foundPy
}

// GetTestCommand returns the pytest command
func (r *PythonTestRunner) GetTestCommand(filter string) []string {
	// Simple invocation, ignoring conda env for now unless injected externally
	cmd := []string{"python", "-m", "pytest", "-x", "--tb=short", "-q"}
	// Each test id is a separate argument (a single space-joined arg would be
	// treated by pytest as one nonexistent path).
	cmd = append(cmd, strings.Fields(filter)...)
	return cmd
}

// condaRoot is the miniconda install used by SWE-bench containers.
const condaRoot = "/opt/miniconda3"

// shellQuote single-quotes s for bash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// wrapConda runs args inside the conda "testbed" env when available.
func wrapConda(workDir string, args []string) []string {
	if _, err := os.Stat(condaRoot); err != nil {
		return args
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	script := "source " + condaRoot + "/etc/profile.d/conda.sh && conda activate testbed && cd " +
		shellQuote(workDir) + " && " + strings.Join(quoted, " ")
	return []string{"bash", "-c", script}
}

// RunTests executes the python tests
func (r *PythonTestRunner) RunTests(ctx context.Context, workDir string, filter string) (*TestResult, error) {
	start := time.Now()
	
	args := wrapConda(workDir, r.GetTestCommand(filter))
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workDir
	
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	
	result := ParseTestOutput(string(out), "python")
	result.Duration = duration
	result.Command = strings.Join(args, " ")
	result.RawOutput = string(out)
	
	// A non-zero exit code usually means tests failed, which is expected.
	// But if there's an actual exec failure, we might want to wrap it.
	if err != nil && result.Failed == 0 && len(result.FailedTests) == 0 {
		// Exec failed but we didn't parse any failing tests, so it might be a syntax error or missing module
		result.Failed = 1
		result.FailedTests = append(result.FailedTests, FailedTest{
			Name: "Command Execution",
			Error: "Command failed to execute properly",
			StackTrace: string(out),
		})
	}
	
	return result, nil
}

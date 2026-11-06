package swebench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"os/exec"
)

// PatchResult represents a validated patch
type PatchResult struct {
	Diff     string
	Files    []string
	Valid    bool
	Error    string
}

// GeneratePatch creates a validated unified diff from the working directory.
// excludePaths lists files (relative to workDir) that should not appear in the
// patch — typically test files added by the SWE-bench test_patch.
func GeneratePatch(ctx context.Context, workDir string, excludePaths ...string) (*PatchResult, error) {
	// 1. Get git diff. Stage everything (incl. new untracked files, issue #40)
	// so the diff captures them, then unstage to leave the index untouched.
	exec.CommandContext(ctx, "git", "-C", workDir, "add", "-A", "--", ".", ":(exclude).nebula").Run()

	// Unstage any test-patch files so they are excluded from the submitted diff
	// (issue #37: test patch files must not appear in the generated patch).
	if len(excludePaths) > 0 {
		args := append([]string{"-C", workDir, "restore", "--staged", "--"}, excludePaths...)
		exec.CommandContext(ctx, "git", args...).Run()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", workDir, "diff", "--cached", "--no-color")
	out, err := cmd.Output()
	exec.CommandContext(ctx, "git", "-C", workDir, "reset", "-q").Run()
	if err != nil {
		return &PatchResult{Valid: false, Error: fmt.Sprintf("git diff failed: %v: %s", err, string(out))}, nil
	}

	diff := string(out)
	if strings.TrimSpace(diff) == "" {
		return &PatchResult{Valid: false, Error: "no changes detected"}, nil
	}

	// 2. Validate patch with git apply --check
	if err := validatePatch(ctx, workDir, diff); err != nil {
		return &PatchResult{
			Diff:  diff,
			Valid: false,
			Error: err.Error(),
		}, nil
	}

	// 3. Extract modified files
	files := extractModifiedFiles(diff)

	return &PatchResult{
		Diff:  diff,
		Files: files,
		Valid: true,
	}, nil
}

// validatePatch checks if a patch can be cleanly applied
func validatePatch(ctx context.Context, workDir, diff string) error {
	// Write patch to a system temp file (not inside workDir) to avoid the temp
	// file showing up as an untracked repo file if the process is interrupted.
	tmpf, err := os.CreateTemp("", "nebula-validate-patch-*.patch")
	if err != nil {
		return err
	}
	patchFile := tmpf.Name()
	if _, err := tmpf.WriteString(diff); err != nil {
		tmpf.Close()
		os.Remove(patchFile)
		return err
	}
	tmpf.Close()
	defer os.Remove(patchFile)

	// Try to apply with --check (dry-run)
	// Use --whitespace=nowarn to be more lenient with line endings
	cmd := exec.CommandContext(ctx, "git", "apply", "--check", "--whitespace=nowarn", patchFile)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// If validation fails, try without --check (actual apply) to see if it works
		// But don't actually apply, just check if it's a whitespace issue
		return fmt.Errorf("patch validation failed: %v: %s", err, string(out))
	}

	return nil
}

// extractModifiedFiles parses git diff to get list of modified files
func extractModifiedFiles(diff string) []string {
	var files []string
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "diff --git") {
			// Format: diff --git a/path/to/file b/path/to/file
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// Remove 'a/' prefix
				file := strings.TrimPrefix(parts[3], "b/")
				file = strings.TrimPrefix(file, "a/")
				files = append(files, file)
			}
		}
	}
	return unique(files)
}

// isTestFile checks if a file is a test file
func isTestFile(path string) bool {
	base := filepath.Base(path)
	// Common test patterns
	testPatterns := []string{
		"_test.go", "_test.py", "test_", "_spec.", ".test.",
		"Test.java", "Spec.js", "spec.ts",
	}
	for _, p := range testPatterns {
		if strings.Contains(base, p) {
			return true
		}
	}
	// Check if in test directory
	dir := filepath.Dir(path)
	testDirs := []string{"test", "tests", "spec", "specs", "__tests__"}
	for _, d := range testDirs {
		if strings.Contains(dir, d) {
			return true
		}
	}
	return false
}

func unique(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
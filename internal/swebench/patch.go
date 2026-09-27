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

// GeneratePatch creates a validated unified diff from the working directory
func GeneratePatch(ctx context.Context, workDir string) (*PatchResult, error) {
	// 1. Get git diff
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-color")
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
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
	// Write patch to temp file
	patchFile := filepath.Join(workDir, ".swebench_patch.tmp")
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		return err
	}
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
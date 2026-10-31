package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type ShellTool struct{}

func (t *ShellTool) Name() string { return "ShellTool" }
func (t *ShellTool) Description() string { return "Executes an arbitrary shell command." }
func (t *ShellTool) Parameters() map[string]string {
	return map[string]string{"command": "The shell command to run"}
}
func (t *ShellTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	cmdStr := input["command"]
	if cmdStr == "" {
		return "", fmt.Errorf("missing command")
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "ReadFileTool" }
func (t *ReadFileTool) Description() string {
	return "Reads a file. For large files pass start_line/end_line (1-based, inclusive) to read only that range; find line numbers first with GrepTool or `grep -n`."
}
func (t *ReadFileTool) Parameters() map[string]string {
	return map[string]string{
		"path":       "The path to the file",
		"start_line": "Optional first line to read (1-based)",
		"end_line":   "Optional last line to read (inclusive)",
	}
}
func (t *ReadFileTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if input["start_line"] == "" && input["end_line"] == "" {
		return string(content), nil
	}
	lines := strings.Split(string(content), "\n")
	start, end := 1, len(lines)
	if v := input["start_line"]; v != "" {
		if n, e := strconv.Atoi(strings.TrimSpace(v)); e == nil && n > 0 {
			start = n
		}
	}
	if v := input["end_line"]; v != "" {
		if n, e := strconv.Atoi(strings.TrimSpace(v)); e == nil && n > 0 {
			end = n
		}
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return "", fmt.Errorf("start_line %d is past end of file (%d lines)", start, len(lines))
	}
	// Raw text (no line-number prefixes) so it can be copied verbatim into StrReplaceTool old_string.
	return fmt.Sprintf("[lines %d-%d of %d]\n%s", start, end, len(lines), strings.Join(lines[start-1:end], "\n")), nil
}

type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "WriteFileTool" }
func (t *WriteFileTool) Description() string { return "Writes content to a file." }
func (t *WriteFileTool) Parameters() map[string]string {
	return map[string]string{
		"path":    "The path to the file",
		"content": "The content to write",
	}
}
func (t *WriteFileTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	content := input["content"]
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	err := os.WriteFile(path, []byte(content), 0644)
	return "File written successfully.", err
}

type GrepTool struct{}

func (t *GrepTool) Name() string { return "GrepTool" }
func (t *GrepTool) Description() string { return "Searches for a pattern in files using grep." }
func (t *GrepTool) Parameters() map[string]string {
	return map[string]string{
		"pattern": "The regular expression pattern to search for",
		"path":    "The path or directory to search in (e.g. '.')",
	}
}
func (t *GrepTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	pattern := input["pattern"]
	path := input["path"]
	if pattern == "" || path == "" {
		return "", fmt.Errorf("missing pattern or path")
	}
	cmd := exec.CommandContext(ctx, "grep", "-rn", pattern, path)
	out, err := cmd.CombinedOutput()
	// grep returns exit code 1 if no lines match, which is not an error for us.
	if err != nil && cmd.ProcessState.ExitCode() == 1 {
		return "", nil // no match
	}
	return string(out), err
}

type GitTool struct{}

func (t *GitTool) Name() string { return "GitTool" }
func (t *GitTool) Description() string { return "Runs common git commands (log, diff, status, commit)." }
func (t *GitTool) Parameters() map[string]string {
	return map[string]string{"args": "Git arguments (e.g. 'status', 'diff HEAD', 'log -n 5')"}
}
func (t *GitTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	args := input["args"]
	if args == "" {
		return "", fmt.Errorf("missing git arguments")
	}
	argsSlice := strings.Split(args, " ")
	cmd := exec.CommandContext(ctx, "git", argsSlice...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type DoneTool struct{}

func (t *DoneTool) Name() string { return "DoneTool" }
func (t *DoneTool) Description() string { return "Signals that the goal has been completed. Input: reason (string)." }
func (t *DoneTool) Parameters() map[string]string {
	return map[string]string{"reason": "Explanation of what was accomplished"}
}
func (t *DoneTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	reason := input["reason"]
	if reason == "" {
		return "", fmt.Errorf("missing reason")
	}
	return fmt.Sprintf("DONE: %s", reason), nil
}

// StrReplaceTool performs a surgical find-and-replace of a unique string in a file.
type StrReplaceTool struct{}

func (t *StrReplaceTool) Name() string { return "StrReplaceTool" }
func (t *StrReplaceTool) Description() string {
	return "Replaces an exact, unique occurrence of old_string with new_string in a file. Prefer this over rewriting whole files."
}
func (t *StrReplaceTool) Parameters() map[string]string {
	return map[string]string{
		"path":       "The path to the file",
		"old_string": "Exact text to find (must appear exactly once in the file)",
		"new_string": "Replacement text",
	}
}
func (t *StrReplaceTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	oldStr, ok := input["old_string"]
	if !ok || oldStr == "" {
		return "", fmt.Errorf("missing old_string")
	}
	newStr := input["new_string"]
	return StrReplaceInFile(path, oldStr, newStr)
}

// StrReplaceInFile replaces the single occurrence of oldStr with newStr in path.
func StrReplaceInFile(path, oldStr, newStr string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	switch n := strings.Count(content, oldStr); n {
	case 0:
		return "", fmt.Errorf("old_string not found in %s", path)
	case 1:
	default:
		return "", fmt.Errorf("old_string appears %d times in %s; it must be unique (add more surrounding context)", n, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(strings.Replace(content, oldStr, newStr, 1)), info.Mode().Perm()); err != nil {
		return "", err
	}
	return fmt.Sprintf("Replaced 1 occurrence in %s", path), nil
}

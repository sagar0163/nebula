package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
func (t *ReadFileTool) Description() string { return "Reads the content of a file." }
func (t *ReadFileTool) Parameters() map[string]string {
	return map[string]string{"path": "The path to the file"}
}
func (t *ReadFileTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	content, err := os.ReadFile(path)
	return string(content), err
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

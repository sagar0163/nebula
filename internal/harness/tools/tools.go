package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sagar0163/nebula/internal/harness/shared"
)

// Tool represents an executable tool
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]shared.ParameterDef
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// ParameterDef describes a tool parameter
type ParameterDef = shared.ParameterDef

// Registry manages available tools
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

func (r *Registry) All() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

func (r *Registry) Definitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

type ToolDefinition struct {
	Name       string                 `json:"name"`
	Description string                `json:"description"`
	Parameters map[string]ParameterDef `json:"parameters"`
}

// ShellTool executes shell commands
type ShellTool struct{}

func (t *ShellTool) Name() string { return "run_command" }
func (t *ShellTool) Description() string { return "Execute a shell command" }
func (t *ShellTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"command": {Type: "string", Description: "The shell command to execute", Required: true},
		"cwd":     {Type: "string", Description: "Working directory (optional)", Required: false},
		"timeout": {Type: "integer", Description: "Timeout in seconds (default: 60)", Required: false},
	}
}

func (t *ShellTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("missing required argument: command")
	}

	cwd := "."
	if cwdArg, ok := args["cwd"].(string); ok && cwdArg != "" {
		cwd = cwdArg
	}

	timeout := 60
	if timeoutArg, ok := args["timeout"].(float64); ok {
		timeout = int(timeoutArg)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	
	result := map[string]interface{}{
		"stdout":   string(output),
		"stderr":   "",
		"exit_code": 0,
	}
	
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			result["exit_code"] = -1
		}
		result["stderr"] = err.Error()
	}
	
	return result, nil
}

// ReadFileTool reads file contents
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the contents of a file" }
func (t *ReadFileTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"path": {Type: "string", Description: "Path to the file", Required: true},
		"start_line": {Type: "integer", Description: "Start line (optional, 0-indexed)", Required: false},
		"end_line": {Type: "integer", Description: "End line (optional, exclusive)", Required: false},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("missing required argument: path")
	}

	startLine := 0
	if v, ok := args["start_line"].(float64); ok {
		startLine = int(v)
	}

	endLine := -1
	if v, ok := args["end_line"].(float64); ok {
		endLine = int(v)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	
	if startLine > 0 || endLine >= 0 {
		if endLine < 0 || endLine > len(lines) {
			endLine = len(lines)
		}
		if startLine < len(lines) {
			lines = lines[startLine:endLine]
		} else {
			lines = []string{}
		}
	}

	return map[string]interface{}{
		"path":     path,
		"content":  strings.Join(lines, "\n"),
		"lines":    len(lines),
	}, nil
}

// WriteFileTool writes file contents
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string { return "Write content to a file" }
func (t *WriteFileTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"path":    {Type: "string", Description: "Path to the file", Required: true},
		"content": {Type: "string", Description: "Content to write", Required: true},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("missing required argument: path")
	}

	content, ok := args["content"].(string)
	if !ok {
		return nil, fmt.Errorf("missing required argument: content")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]interface{}{
		"path":    path,
		"bytes":   len(content),
		"success": true,
	}, nil
}

// GrepTool searches for patterns in files
type GrepTool struct{}

func (t *GrepTool) Name() string { return "search_code" }
func (t *GrepTool) Description() string { return "Search for a pattern in files using grep" }
func (t *GrepTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"pattern": {Type: "string", Description: "Regular expression pattern to search for", Required: true},
		"path":    {Type: "string", Description: "Path or directory to search in", Required: false},
		"file_pattern": {Type: "string", Description: "File pattern (e.g. *.go)", Required: false},
	}
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("missing required argument: pattern")
	}

	searchPath := "."
	if v, ok := args["path"].(string); ok && v != "" {
		searchPath = v
	}

	filePattern := ""
	if v, ok := args["file_pattern"].(string); ok {
		filePattern = v
	}

	args_list := []string{"-rn", pattern}
	if filePattern != "" {
		args_list = append(args_list, "--include="+filePattern)
	}
	args_list = append(args_list, searchPath)

	cmd := exec.CommandContext(ctx, "grep", args_list...)
	output, err := cmd.CombinedOutput()
	
	if err != nil && cmd.ProcessState.ExitCode() == 1 {
		// No matches found
		return map[string]interface{}{
			"matches":  []string{},
			"count":    0,
		}, nil
	}
	
	if err != nil {
		return nil, fmt.Errorf("grep failed: %w: %s", err, string(output))
	}

	matches := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(matches) == 1 && matches[0] == "" {
		matches = []string{}
	}

	return map[string]interface{}{
		"matches": matches,
		"count":   len(matches),
	}, nil
}

// GitTool runs git commands
type GitTool struct{}

func (t *GitTool) Name() string { return "git" }
func (t *GitTool) Description() string { return "Run git commands" }
func (t *GitTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"args": {Type: "string", Description: "Git arguments (e.g. 'status', 'diff HEAD', 'log -n 5')", Required: true},
		"cwd":  {Type: "string", Description: "Working directory", Required: false},
	}
}

func (t *GitTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	argsStr, ok := args["args"].(string)
	if !ok || argsStr == "" {
		return nil, fmt.Errorf("missing required argument: args")
	}

	cwd := "."
	if v, ok := args["cwd"].(string); ok && v != "" {
		cwd = v
	}

	argsList := strings.Fields(argsStr)
	cmd := exec.CommandContext(ctx, "git", argsList...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()

	result := map[string]interface{}{
		"output": string(output),
		"exit_code": 0,
	}
	
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			result["exit_code"] = -1
		}
		result["error"] = err.Error()
	}

	return result, nil
}

// RunTestsTool runs tests with language detection
type RunTestsTool struct{}

func (t *RunTestsTool) Name() string { return "run_tests" }
func (t *RunTestsTool) Description() string { return "Run tests for the project" }
func (t *RunTestsTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"filter": {Type: "string", Description: "Test filter (e.g., failing test name)", Required: false},
		"cwd":    {Type: "string", Description: "Working directory", Required: false},
		"timeout": {Type: "integer", Description: "Timeout in seconds", Required: false},
	}
}

func (t *RunTestsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cwd := "."
	if v, ok := args["cwd"].(string); ok && v != "" {
		cwd = v
	}

	timeout := 120
	if v, ok := args["timeout"].(float64); ok {
		timeout = int(v)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Detect test runner
		var cmd *exec.Cmd

		// Check for Go
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			filter := ""
			if v, ok := args["filter"].(string); ok {
				filter = v
			}
			cmdArgs := []string{"test", "./...", "-v"}
			if filter != "" {
				cmdArgs = append(cmdArgs, "-run", filter)
			}
			cmd = exec.CommandContext(ctx, "go", cmdArgs...)
		} else {
			// Python with pytest
			pytestIni := filepath.Join(cwd, "pytest.ini")
			pyprojectToml := filepath.Join(cwd, "pyproject.toml")
			_, err1 := os.Stat(pytestIni)
			_, err2 := os.Stat(pyprojectToml)
			if err1 == nil || err2 == nil {
				// Python with pytest
				filter := ""
				if v, ok := args["filter"].(string); ok {
					filter = v
				}
				cmdArgs := []string{"-m", "pytest", "-v"}
				if filter != "" {
					cmdArgs = append(cmdArgs, "-k", filter)
				}
				cmd = exec.CommandContext(ctx, "python", cmdArgs...)
			} else if _, err := os.Stat(filepath.Join(cwd, "package.json")); err == nil {
				// JavaScript/TypeScript with npm
				cmd = exec.CommandContext(ctx, "npm", "test")
			} else if _, err := os.Stat(filepath.Join(cwd, "Cargo.toml")); err == nil {
				// Rust
				cmd = exec.CommandContext(ctx, "cargo", "test")
			} else {
				return nil, fmt.Errorf("no recognized test framework found")
			}
		}

	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()

	// Parse results
	result := map[string]interface{}{
		"command":   strings.Join(cmd.Args, " "),
		"output":    string(output),
		"exit_code": 0,
	}
	
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			result["exit_code"] = -1
		}
		result["error"] = err.Error()
	}

	// Try to parse pass/fail counts
	outputStr := string(output)
	passed, failed := parseTestOutput(outputStr)
	result["passed"] = passed
	result["failed"] = failed

	return result, nil
}

func parseTestOutput(output string) (passed, failed int) {
	// Go test
	if strings.Contains(output, "PASS") || strings.Contains(output, "FAIL") {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "--- PASS:") || strings.HasPrefix(line, "--- FAIL:") {
				if strings.Contains(line, "PASS") {
					passed++
				} else {
					failed++
				}
			}
		}
	}
	
	// Pytest
	if strings.Contains(output, "passed") && strings.Contains(output, "failed") {
		// Try to extract from pytest summary
		// e.g. "5 passed, 2 failed in 1.23s"
	}
	
	return passed, failed
}

// NewDefaultRegistry creates a registry with all default tools

// SearchCodeTool searches for code patterns in the workspace
type SearchCodeTool struct{}

func (t *SearchCodeTool) Name() string { return "search_code" }
func (t *SearchCodeTool) Description() string { return "Search for a regex pattern in the codebase using grep" }
func (t *SearchCodeTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"pattern": {Type: "string", Description: "The grep regular expression to search for", Required: true},
		"dir":     {Type: "string", Description: "Directory to search within (default: .)", Required: false},
	}
}

func (t *SearchCodeTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("missing required argument: pattern")
	}

	dir := "."
	if d, ok := args["dir"].(string); ok && d != "" {
		dir = d
	}

	// grep -rnIE "pattern" dir
	cmd := exec.CommandContext(ctx, "grep", "-rnIE", pattern, dir)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		if cmd.ProcessState.ExitCode() == 1 {
			return "No matches found.", nil
		}
		return nil, fmt.Errorf("grep failed: %v, output: %s", err, string(out))
	}
	
	result := string(out)
	if len(result) > 8000 {
		result = result[:8000] + "\n... (truncated)"
	}
	return result, nil
}


// GoToDefinitionTool finds symbol definitions using ctags
type GoToDefinitionTool struct{}

func (t *GoToDefinitionTool) Name() string { return "go_to_definition" }
func (t *GoToDefinitionTool) Description() string { return "Find where a class, function, or symbol is defined in the codebase" }
func (t *GoToDefinitionTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"symbol": {Type: "string", Description: "The symbol name (e.g. 'PlannerAgent')", Required: true},
		"dir":    {Type: "string", Description: "Directory to search within (default: .)", Required: false},
	}
}

func (t *GoToDefinitionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	symbol, ok := args["symbol"].(string)
	if !ok || symbol == "" {
		return nil, fmt.Errorf("missing required argument: symbol")
	}

	dir := "."
	if d, ok := args["dir"].(string); ok && d != "" {
		dir = d
	}

	// Dynamic import avoidance by just running the command directly here, 
	// or we can just run the same sh -c ctags command. We will just execute it directly.
	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("ctags -R -x . | grep -w '%s'", symbol))
	cmd.Dir = dir
	
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return "No definitions found.", nil
		}
		return nil, fmt.Errorf("ctags failed: %v, output: %s", err, string(out))
	}
	
	result := string(out)
	if len(result) > 4000 {
		result = result[:4000] + "\n... (truncated)"
	}
	return result, nil
}


// SemanticSearchTool searches the codebase semantically
type SemanticSearchTool struct{}

func (t *SemanticSearchTool) Name() string { return "semantic_search" }
func (t *SemanticSearchTool) Description() string { return "Search the codebase using semantic meaning instead of exact grep matching" }
func (t *SemanticSearchTool) Parameters() map[string]ParameterDef {
	return map[string]ParameterDef{
		"query": {Type: "string", Description: "Natural language query", Required: true},
	}
}

func (t *SemanticSearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required argument: query")
	}

	// For now, we mock the embedding lookup and return a placeholder 
	// since actual DB indexing happens during orchestrator init
	return fmt.Sprintf("Semantic matches for '%s':\n(Feature in development, use search_code for exact matches)", query), nil
}

func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&ShellTool{})
	r.Register(&ReadFileTool{})
	r.Register(&WriteFileTool{})
	r.Register(&SearchCodeTool{})
	r.Register(&GoToDefinitionTool{})
	r.Register(&SemanticSearchTool{})
	r.Register(&GrepTool{})
	r.Register(&GitTool{})
	r.Register(&RunTestsTool{})
	return r
}
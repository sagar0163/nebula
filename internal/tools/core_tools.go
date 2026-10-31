package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// absPath returns an absolute path for p: if p is already absolute it is
// returned unchanged; otherwise it is joined with workDir. When workDir is
// empty or "." the path is returned as-is so CWD-relative behaviour is
// preserved for callers that have not set a workDir.
func absPath(workDir, p string) string {
	if workDir == "" || workDir == "." || p == "" {
		return p
	}
	if len(p) > 0 && p[0] == '/' {
		return p
	}
	return workDir + "/" + p
}

type ShellTool struct {
	// WorkDir, when set, is used as the command working directory instead of
	// the process CWD, enabling concurrent execution.
	WorkDir string
}

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
	if t.WorkDir != "" && t.WorkDir != "." {
		cmd.Dir = t.WorkDir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type ReadFileTool struct {
	// WorkDir, when set, resolves relative paths against it.
	WorkDir string
}

func (t *ReadFileTool) Name() string { return "ReadFileTool" }
func (t *ReadFileTool) Description() string {
	return "Reads a file with line numbers (\"N: text\"). Files over 200 lines return the first 200 unless start_line/end_line (1-based, inclusive) are given. The \"N: \" prefixes are NOT part of the file; strip them before using text in StrReplaceTool old_string."
}
func (t *ReadFileTool) Parameters() map[string]string {
	return map[string]string{
		"path":       "The path to the file",
		"start_line": "Optional first line to read (1-based)",
		"end_line":   "Optional last line to read (inclusive)",
	}
}

const readFileDefaultLines = 200

func (t *ReadFileTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	content, err := os.ReadFile(absPath(t.WorkDir, path))
	if err != nil {
		return "", err
	}
	text := strings.TrimSuffix(string(content), "\n")
	lines := strings.Split(text, "\n")
	total := len(lines)
	start, end := 1, total
	ranged := input["start_line"] != "" || input["end_line"] != ""
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
	note := ""
	if !ranged && total > readFileDefaultLines {
		end = readFileDefaultLines
		note = fmt.Sprintf("\n[showing first %d of %d lines — use start_line/end_line for more]", readFileDefaultLines, total)
	}
	if end > total {
		end = total
	}
	if start > end {
		return "", fmt.Errorf("start_line %d is past end of file (%d lines)", start, total)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[lines %d-%d of %d — %s]\n", start, end, total, path)
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i, lines[i-1])
	}
	return strings.TrimSuffix(b.String(), "\n") + note, nil
}

type WriteFileTool struct {
	// WorkDir, when set, resolves relative paths against it.
	WorkDir string
}

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
	err := os.WriteFile(absPath(t.WorkDir, path), []byte(content), 0644)
	return "File written successfully.", err
}

type GrepTool struct {
	// WorkDir, when set, is used as cmd.Dir so paths are resolved correctly.
	WorkDir string
}

func (t *GrepTool) Name() string { return "GrepTool" }
func (t *GrepTool) Description() string { return "Searches for a pattern in files using grep." }
func (t *GrepTool) Parameters() map[string]string {
	return map[string]string{
		"pattern": "The regular expression pattern to search for",
		"path":    "The path or directory to search in (e.g. '.')",
	}
}
const grepMaxMatches = 50

func (t *GrepTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	pattern := input["pattern"]
	path := input["path"]
	if pattern == "" || path == "" {
		return "", fmt.Errorf("missing pattern or path")
	}
	// Prefer git grep: tracked files only, skips .git and binaries (-I).
	cmd := exec.CommandContext(ctx, "git", "grep", "-n", "-I", "-e", pattern, "--", path)
	if t.WorkDir != "" && t.WorkDir != "." {
		cmd.Dir = t.WorkDir
	}
	out, err := cmd.Output()
	if err != nil {
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		if code == 1 {
			return "", nil // no match
		}
		// Not a git repo (or git failed): fall back to grep over source files.
		cmd = exec.CommandContext(ctx, "grep", "-rn", "-I",
			"--include=*.go", "--include=*.py", "--include=*.js", "--include=*.ts",
			"--exclude-dir=.git", "-e", pattern, path)
		if t.WorkDir != "" && t.WorkDir != "." {
			cmd.Dir = t.WorkDir
		}
		out, err = cmd.CombinedOutput()
		if err != nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return "", nil
		}
		if err != nil {
			return string(out), err
		}
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > grepMaxMatches {
		extra := len(lines) - grepMaxMatches
		lines = append(lines[:grepMaxMatches], fmt.Sprintf("[%d more matches not shown — narrow your pattern]", extra))
	}
	return strings.Join(lines, "\n"), nil
}

type GitTool struct {
	// WorkDir, when set, is used as cmd.Dir so git operates on the right repo.
	WorkDir string
}

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
	if t.WorkDir != "" && t.WorkDir != "." {
		cmd.Dir = t.WorkDir
	}
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
type StrReplaceTool struct {
	// WorkDir, when set, resolves relative paths against it.
	WorkDir string
}

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
	return StrReplaceInFile(absPath(t.WorkDir, path), oldStr, newStr)
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
		// Retry ignoring trailing whitespace and tab/space differences.
		if fixed, ok := normalizedReplace(content, oldStr, newStr); ok {
			content, oldStr, newStr = fixed, fixed, fixed // write fixed content below
		} else {
			return "", fmt.Errorf("old_string not found in %s%s", path, similarLinesHint(content, oldStr))
		}
	case 1:
	default:
		return "", fmt.Errorf("old_string appears %d times in %s; it must be unique (add more surrounding context)", n, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(strings.Replace(content, oldStr, newStr, 1)), info.Mode().Perm()); err != nil {
		// (for a whitespace-normalized match content==oldStr==newStr, so this writes the fixed content)
		return "", err
	}
	// Lint gate: reject edits that leave a Python file syntactically invalid.
	if strings.HasSuffix(path, ".py") {
		if msg, bad := pythonSyntaxError(path); bad {
			if werr := os.WriteFile(path, data, info.Mode().Perm()); werr != nil {
				return "", fmt.Errorf("edit broke Python syntax and revert failed: %v (syntax error: %s)", werr, msg)
			}
			return "", fmt.Errorf("edit REVERTED: it would make %s syntactically invalid Python:\n%s\nCheck indentation and brackets in new_string and retry", path, msg)
		}
	}
	return fmt.Sprintf("Replaced 1 occurrence in %s", path), nil
}

// normWS normalizes a line for whitespace-insensitive comparison.
func normWS(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\t", "    "), " \r")
}

// normalizedReplace finds oldStr in content line-by-line after whitespace
// normalization. If exactly one match exists it returns content with those
// original lines replaced by newStr.
func normalizedReplace(content, oldStr, newStr string) (string, bool) {
	cLines := strings.Split(content, "\n")
	oLines := strings.Split(strings.TrimRight(oldStr, "\n"), "\n")
	if len(oLines) == 0 || len(oLines) > len(cLines) {
		return "", false
	}
	match := -1
	for i := 0; i+len(oLines) <= len(cLines); i++ {
		ok := true
		for j, ol := range oLines {
			if normWS(cLines[i+j]) != normWS(ol) {
				ok = false
				break
			}
		}
		if ok {
			if match >= 0 {
				return "", false // ambiguous
			}
			match = i
		}
	}
	if match < 0 {
		return "", false
	}
	repl := strings.Split(strings.TrimRight(newStr, "\n"), "\n")
	if newStr == "" {
		repl = nil
	}
	out := append([]string{}, cLines[:match]...)
	out = append(out, repl...)
	out = append(out, cLines[match+len(oLines):]...)
	return strings.Join(out, "\n"), true
}

// similarLinesHint returns up to 3 file lines resembling the first line of
// oldStr, so the model can correct its old_string without re-reading.
func similarLinesHint(content, oldStr string) string {
	first := ""
	for _, l := range strings.Split(oldStr, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			first = t
			break
		}
	}
	if first == "" {
		return ""
	}
	cLines := strings.Split(content, "\n")
	var hits []string
	add := func(i int) {
		if len(hits) < 3 {
			hits = append(hits, fmt.Sprintf("  %d: %s", i+1, cLines[i]))
		}
	}
	for i, l := range cLines {
		if strings.Contains(strings.TrimSpace(l), first) {
			add(i)
		}
	}
	if len(hits) == 0 {
		// Looser: lines containing the longest word-ish token of the first line.
		key := ""
		for _, f := range strings.FieldsFunc(first, func(r rune) bool { return r == ' ' || r == '(' || r == ')' || r == ',' || r == ':' || r == '=' }) {
			if len(f) > len(key) {
				key = f
			}
		}
		if len(key) >= 3 {
			for i, l := range cLines {
				if strings.Contains(l, key) {
					add(i)
				}
			}
		}
	}
	if len(hits) == 0 {
		return ". No similar lines found; re-read the file with ReadFileTool"
	}
	return ". Closest candidate lines (copy exact text, without the \"N: \" prefix):\n" + strings.Join(hits, "\n")
}

// pythonSyntaxError parses path with python3's ast module. It reports
// (message, true) only on a real SyntaxError; if python3 is unavailable or
// fails for another reason the edit is allowed.
func pythonSyntaxError(path string) (string, bool) {
	const script = `import ast, sys
try:
    ast.parse(open(sys.argv[1], encoding="utf-8").read(), sys.argv[1])
except SyntaxError as e:
    print("%s line %s: %s\n%s" % (type(e).__name__, e.lineno, e.msg, (e.text or "").rstrip()))
    sys.exit(3)
`
	out, err := exec.Command("python3", "-c", script, path).CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 {
		return strings.TrimSpace(string(out)), true
	}
	return "", false
}

// NewDefaultRegistry builds the standard tool registry used by the goal
// executor. testCmd is optional; when non-empty, a RunTestsTool is included so
// the model can verify its own fixes. workDir, when non-empty, is set on every
// tool so file and shell operations resolve relative paths against workDir
// instead of the process CWD, enabling safe concurrent execution.
func NewDefaultRegistry(testCmd, workDir string) *Registry {
	r := NewRegistry()
	r.Register(&ShellTool{WorkDir: workDir})
	r.Register(&ReadFileTool{WorkDir: workDir})
	r.Register(&WriteFileTool{WorkDir: workDir})
	r.Register(&StrReplaceTool{WorkDir: workDir})
	r.Register(&GrepTool{WorkDir: workDir})
	r.Register(&GitTool{WorkDir: workDir})
	r.Register(&DoneTool{})
	if testCmd != "" {
		r.Register(&RunTestsTool{TestCmd: testCmd})
	}
	return r
}

// RunTestsTool runs a pre-configured test command so the model can verify its
// fix inside the goal loop.
type RunTestsTool struct {
	TestCmd string // e.g. "python -m pytest -x tests/test_foo.py"
}

const runTestsMaxOutput = 6000

func (t *RunTestsTool) Name() string { return "RunTestsTool" }
func (t *RunTestsTool) Description() string {
	return "Runs the failing tests for this task and returns their output (pass/fail, assertion errors, tracebacks). Takes no input; pass {}."
}
func (t *RunTestsTool) Parameters() map[string]string { return map[string]string{} }
func (t *RunTestsTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	if t.TestCmd == "" {
		return "", fmt.Errorf("no test command configured")
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(runCtx, "sh", "-c", t.TestCmd).CombinedOutput()
	s := string(out)
	// Test summaries and tracebacks are at the end; keep the tail.
	if len(s) > runTestsMaxOutput {
		s = "...(truncated)...\n" + s[len(s)-runTestsMaxOutput:]
	}
	status := "TESTS PASSED"
	if runCtx.Err() == context.DeadlineExceeded {
		status = "TESTS TIMED OUT after 60s"
	} else if err != nil {
		status = fmt.Sprintf("TESTS FAILED (%v)", err)
	}
	// Report failures in the output rather than as a tool error so the model
	// reads the traceback instead of treating the call as broken.
	return fmt.Sprintf("$ %s\n%s\n%s", t.TestCmd, status, s), nil
}

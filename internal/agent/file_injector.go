package agent

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func ExtractRelevantFiles(cmd, output string, maxFiles int) []string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	// isSafe rejects paths that escape cwd or are absolute — prevents
	// attacker-controlled error output from injecting ../../../etc/passwd.go.
	isSafe := func(p string) bool {
		if filepath.IsAbs(p) {
			return false
		}
		clean := filepath.Clean(p)
		// filepath.Clean turns "../foo" into ".." + sep + "foo"; a Rel that
		// starts with ".." means the path escapes the working directory.
		rel, err := filepath.Rel(cwd, filepath.Join(cwd, clean))
		if err != nil {
			return false
		}
		return !strings.HasPrefix(rel, "..")
	}

	var paths []string
	seen := make(map[string]bool)

	addPath := func(p string) {
		if !seen[p] && len(paths) < maxFiles && isSafe(p) {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// Regex for paths: Go (*.go:NN), JS (*.js:NN), Python (*.py:NN), TypeScript (*.ts:NN)
	// Example: "foo/bar.go:12" or "src/index.ts:45:2"
	re := regexp.MustCompile(`([a-zA-Z0-9_./-]+\.(?:go|js|py|ts))(?::\d+)?`)
	matches := re.FindAllStringSubmatch(output, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addPath(m[1])
		}
	}

	// Contextual files
	cmdLower := strings.ToLower(cmd)
	if strings.Contains(cmdLower, "go build") || strings.Contains(cmdLower, "go test") {
		if _, err := os.Stat("go.mod"); err == nil {
			addPath("go.mod")
		}
	}
	if strings.Contains(cmdLower, "npm run") {
		if _, err := os.Stat("package.json"); err == nil {
			addPath("package.json")
		}
	}
	if strings.Contains(cmdLower, "python") || strings.Contains(cmdLower, "pip") {
		if _, err := os.Stat("requirements.txt"); err == nil {
			addPath("requirements.txt")
		} else if _, err := os.Stat("pyproject.toml"); err == nil {
			addPath("pyproject.toml")
		}
	}

	return paths
}

func ReadFileExcerpt(path string, maxBytes int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	if info.Size() <= int64(maxBytes) {
		content, err := io.ReadAll(f)
		if err != nil {
			return ""
		}
		return string(content)
	}

	// Truncate
	head := make([]byte, 512)
	n1, _ := io.ReadFull(f, head)

	f.Seek(-512, io.SeekEnd)
	tail := make([]byte, 512)
	n2, _ := io.ReadFull(f, tail)

	return string(head[:n1]) + "\n[...truncated...]\n" + string(tail[:n2])
}

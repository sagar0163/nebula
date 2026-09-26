package agent

import (
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
)

type CodebaseIndex struct {
	Language   string
	FileTree   []string
	GitCommits []string
}

type FileMatch struct {
	Path  string
	Lines []string
}

func IndexCodebase(dir string) CodebaseIndex {
	var index CodebaseIndex
	index.Language = DetectProjectContext(dir).Language

	// File tree depth 3
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(dir, path)
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth <= 3 {
			index.FileTree = append(index.FileTree, rel)
		} else if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})

	// Git commits
	cmd := exec.Command("git", "-C", dir, "log", "-n", "10", "--oneline")
	out, _ := cmd.CombinedOutput()
	if len(out) > 0 {
		index.GitCommits = strings.Split(strings.TrimSpace(string(out)), "\n")
	}

	return index
}

func (c CodebaseIndex) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Language: %s\n", c.Language))
	sb.WriteString("Recent Git Commits:\n")
	for _, commit := range c.GitCommits {
		sb.WriteString(fmt.Sprintf("- %s\n", commit))
	}
	sb.WriteString("File Tree (depth 3):\n")
	for _, f := range c.FileTree {
		sb.WriteString(fmt.Sprintf("- %s\n", f))
	}
	return sb.String()
}

func SearchCodebase(dir, query string) []FileMatch {
	// naive grep
	cmd := exec.Command("grep", "-rn", query, dir)
	out, _ := cmd.CombinedOutput()
	
	matches := make(map[string][]string)
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		parts := strings.SplitN(l, ":", 3)
		if len(parts) >= 3 {
			matches[parts[0]] = append(matches[parts[0]], parts[1]+":"+parts[2])
		}
	}
	
	var results []FileMatch
	for k, v := range matches {
		results = append(results, FileMatch{Path: k, Lines: v})
	}
	return results
}

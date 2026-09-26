package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRelevantFiles(t *testing.T) {
	// Create a temp dir for files
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create dummy contextual files
	os.WriteFile("go.mod", []byte("module test"), 0644)
	os.WriteFile("package.json", []byte("{}"), 0644)

	tests := []struct {
		name     string
		cmd      string
		output   string
		max      int
		expected []string
	}{
		{
			name:     "go error extracts .go files",
			cmd:      "go build",
			output:   "main.go:10: undefined: foo\nutils/helper.go:5: syntax error",
			max:      3,
			expected: []string{"main.go", "utils/helper.go", "go.mod"},
		},
		{
			name:     "npm error extracts package.json",
			cmd:      "npm run build",
			output:   "src/index.ts:15: error\nsome other text",
			max:      3,
			expected: []string{"src/index.ts", "package.json"},
		},
		{
			name:     "python error extracts py file",
			cmd:      "python script.py",
			output:   "File \"main.py\", line 10\n  print('x')",
			max:      3,
			expected: []string{"main.py"},
		},
		{
			name:     "deduplication and max limits",
			cmd:      "go build",
			output:   "a.go:1\nb.go:2\na.go:3\nc.go:4\nd.go:5",
			max:      3,
			expected: []string{"a.go", "b.go", "c.go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractRelevantFiles(tc.cmd, tc.output, tc.max)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d files, got %d: %v", len(tc.expected), len(got), got)
			}
			for i, v := range got {
				if v != tc.expected[i] {
					t.Errorf("expected %s at index %d, got %s", tc.expected[i], i, v)
				}
			}
		})
	}
}

func TestReadFileExcerpt(t *testing.T) {
	dir := t.TempDir()

	// Short file
	shortPath := filepath.Join(dir, "short.txt")
	os.WriteFile(shortPath, []byte("hello world"), 0644)

	content := ReadFileExcerpt(shortPath, 2048)
	if content != "hello world" {
		t.Errorf("expected 'hello world', got %q", content)
	}

	// Long file
	longPath := filepath.Join(dir, "long.txt")
	head := strings.Repeat("A", 512)
	middle := strings.Repeat("B", 2000)
	tail := strings.Repeat("C", 512)
	os.WriteFile(longPath, []byte(head+middle+tail), 0644)

	content = ReadFileExcerpt(longPath, 2048)
	if !strings.HasPrefix(content, head) {
		t.Errorf("content should start with head")
	}
	if !strings.HasSuffix(content, tail) {
		t.Errorf("content should end with tail")
	}
	if !strings.Contains(content, "[...truncated...]") {
		t.Errorf("content should contain truncated marker")
	}
}

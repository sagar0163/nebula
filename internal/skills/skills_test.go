package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkSkillsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "nebula", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	return dir
}

func TestLoad(t *testing.T) {
	dir := mkSkillsHome(t)
	content := "---\ndescription: useful skill\n---\nAlways be kind."
	if err := os.WriteFile(filepath.Join(dir, "test.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load("test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Name != "test" || s.Description != "useful skill" || strings.TrimSpace(s.Instructions) != "Always be kind." {
		t.Fatalf("Load = %+v, want parsed frontmatter and body", s)
	}
}

func TestLoadMissing(t *testing.T) {
	mkSkillsHome(t)
	if _, err := Load("does-not-exist"); err == nil {
		t.Fatal("Load(missing) returned a nil error")
	}
}

func TestList(t *testing.T) {
	dir := mkSkillsHome(t)
	files := map[string]string{
		"one.md":     "---\ndescription: first\n---\nDo one.",
		"two.md":     "---\ndescription: second\n---\nDo two.",
		"notes.txt":  "not a skill",
		"subdir.md":  "ignored nested file",
	}
	for name, content := range files {
		if name == "subdir.md" {
			if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "subdir", "subdir.md"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	list, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List returned %d skills (%v), want 2", len(list), list)
	}
	names := map[string]bool{}
	for _, s := range list {
		names[s.Name] = true
	}
	if !names["one"] || !names["two"] {
		t.Fatalf("List names = %v, want one and two", names)
	}
}

func TestParseMalformedFrontmatter(t *testing.T) {
	s := parse("raw", "no frontmatter delimiters here at all")
	if s == nil || s.Name != "raw" || s.Instructions == "" {
		t.Fatalf("parse(no delimiter) = %+v", s)
	}

	s = parse("half", "only one ---\ndescription: x")
	if s == nil || s.Name != "half" || s.Instructions == "" {
		t.Fatalf("parse(single delimiter) = %+v", s)
	}

	s = parse("empty", "")
	if s == nil || s.Name != "empty" {
		t.Fatalf("parse(empty) = %+v", s)
	}
}
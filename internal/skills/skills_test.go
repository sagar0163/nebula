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
		"one.md":    "---\ndescription: first\n---\nDo one.",
		"two.md":    "---\ndescription: second\n---\nDo two.",
		"notes.txt": "not a skill",
		"subdir.md": "ignored nested file",
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

func TestSkillLoadChaos(t *testing.T) {
	t.Run("non-existent path", func(t *testing.T) {
		mkSkillsHome(t)
		if _, err := Load("missing-skill"); err == nil {
			t.Fatal("Load(missing) returned a nil error")
		}
	})

	t.Run("valid frontmatter", func(t *testing.T) {
		dir := mkSkillsHome(t)
		if err := os.WriteFile(filepath.Join(dir, "good.md"),
			[]byte("---\ndescription: a good skill\n---\nDo good work."), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load("good")
		if err != nil {
			t.Fatalf("Load(good): %v", err)
		}
		if s.Description != "a good skill" || s.Instructions != "Do good work." {
			t.Fatalf("Load(good) = %+v", s)
		}
	})

	t.Run("no frontmatter", func(t *testing.T) {
		dir := mkSkillsHome(t)
		if err := os.WriteFile(filepath.Join(dir, "raw.md"),
			[]byte("Just plain instructions.\nSecond line."), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load("raw")
		if err != nil {
			t.Fatalf("Load(raw): %v", err)
		}
		if s.Description != "" {
			t.Fatalf("Load(raw) Description = %q, want empty", s.Description)
		}
		if s.Instructions != "Just plain instructions.\nSecond line." {
			t.Fatalf("Load(raw) Instructions = %q, want whole content", s.Instructions)
		}
	})

	t.Run("opening delimiter only", func(t *testing.T) {
		dir := mkSkillsHome(t)
		content := "---\ndescription: unfinished\nbody here"
		if err := os.WriteFile(filepath.Join(dir, "half.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load("half")
		if err != nil {
			t.Fatalf("Load(half): %v", err)
		}
		if s.Name != "half" || !strings.Contains(s.Instructions, "body here") {
			t.Fatalf("Load(half) = %+v, want name and raw body content", s)
		}
	})

	t.Run("empty instructions body", func(t *testing.T) {
		dir := mkSkillsHome(t)
		if err := os.WriteFile(filepath.Join(dir, "empty.md"),
			[]byte("---\ndescription: empty body\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load("empty")
		if err != nil {
			t.Fatalf("Load(empty): %v", err)
		}
		if s.Instructions != "" {
			t.Fatalf("Load(empty) Instructions = %q, want empty", s.Instructions)
		}
	})

	t.Run("utf8 and emoji", func(t *testing.T) {
		dir := mkSkillsHome(t)
		if err := os.WriteFile(filepath.Join(dir, "utf8.md"),
			[]byte("---\ndescription: 世界 🚀 skill\n---\n指令 🎉 done."), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load("utf8")
		if err != nil {
			t.Fatalf("Load(utf8): %v", err)
		}
		if !strings.Contains(s.Description, "🚀") || !strings.Contains(s.Instructions, "🎉") {
			t.Fatalf("Load(utf8) = %+v, want emoji preserved", s)
		}
	})

	t.Run("very long description", func(t *testing.T) {
		dir := mkSkillsHome(t)
		desc := strings.Repeat("d", 10*1024)
		if err := os.WriteFile(filepath.Join(dir, "long.md"),
			[]byte("---\ndescription: "+desc+"\n---\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load("long")
		if err != nil {
			t.Fatalf("Load(long): %v", err)
		}
		if len(s.Description) != len(desc) {
			t.Fatalf("Load(long) Description length = %d, want %d (truncated)", len(s.Description), len(desc))
		}
	})
}

func TestSkillListChaos(t *testing.T) {
	t.Run("mixed extensions", func(t *testing.T) {
		dir := mkSkillsHome(t)
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("one"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "c.markdown"), []byte("three"), 0o644); err != nil {
			t.Fatal(err)
		}
		list, err := List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 1 || list[0].Name != "a" {
			t.Fatalf("List = %+v, want only [a]", list)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		mkSkillsHome(t)
		list, err := List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("List = %+v, want empty", list)
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		list, err := List()
		if err != nil {
			t.Fatalf("List on missing dir = %v, want nil", err)
		}
		if len(list) != 0 {
			t.Fatalf("List on missing dir = %+v, want empty", list)
		}
	})

	t.Run("subdirectory not a skill", func(t *testing.T) {
		dir := mkSkillsHome(t)
		if err := os.WriteFile(filepath.Join(dir, "top.md"), []byte("top"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nested", "inner.md"), []byte("inner"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "odd.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "odd.md", "file.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		list, err := List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 1 || list[0].Name != "top" {
			t.Fatalf("List = %+v, want only [top]", list)
		}
	})
}

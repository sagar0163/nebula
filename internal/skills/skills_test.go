package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestLoadHundredSkills(t *testing.T) {
	dir := mkSkillsHome(t)
	const n = 100
	for i := 0; i < n; i++ {
		name := "skill-" + string(rune('a'+i%26)) + string(rune('0'+i/10)) + string(rune('0'+i%10))
		if err := os.WriteFile(filepath.Join(dir, name+".md"),
			[]byte("---\ndescription: skill "+string(rune('0'+i%10))+"\n---\nbody "+string(rune('0'+i%10))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != n {
		t.Fatalf("List returned %d skills, want %d", len(list), n)
	}
	seen := map[string]bool{}
	for _, s := range list {
		if seen[s.Name] {
			t.Fatalf("duplicate skill name %q in List", s.Name)
		}
		seen[s.Name] = true
		if s.Instructions == "" {
			t.Fatalf("skill %q has empty instructions", s.Name)
		}
	}
}

func TestLoadPathTraversalRejected(t *testing.T) {
	mkSkillsHome(t)
	// Even if the escaped path exists, the loader must refuse traversal names.
	bad := []string{
		"../../../etc/passwd",
		"..\\..\\..\\windows\\win.ini",
		"../sibling",
		"a/b",
		"nested\\skill",
		"..",
		".",
		"",
	}
	for _, name := range bad {
		if _, err := Load(name); err == nil {
			t.Errorf("Load(%q) returned nil error, want rejection", name)
		}
	}
}

func TestLoadDoesNotEscapeSkillsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "nebula", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop a decoy at the exact path a traversal name would resolve to.
	escapeRoot := filepath.Join(home, "etc")
	if err := os.MkdirAll(escapeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(escapeRoot, "passwd.md"), []byte("Sneaky config that a traversal would read."), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("../../../etc/passwd"); err == nil {
		t.Fatal("Load(traversal) followed the escaped path; skills loader must not read outside the skills dir")
	}
}

func TestCircularDependencyReferencesAreInert(t *testing.T) {
	dir := mkSkillsHome(t)
	// Skills have no dependency-resolution mechanism, so frontmatter that
	// references other skills (including circularly) must load without
	// recursion, hanging, or erroring.
	a := "---\ndescription: needs skill-b\n---\nUse skill-b first."
	b := "---\ndescription: needs skill-a\n---\nUse skill-a first."
	if err := os.WriteFile(filepath.Join(dir, "skill-a.md"), []byte(a), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill-b.md"), []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan *Skill, 2)
	errs := make(chan error, 2)
	for _, name := range []string{"skill-a", "skill-b"} {
		go func(name string) {
			s, err := Load(name)
			if err != nil {
				errs <- err
				return
			}
			done <- s
		}(name)
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			t.Fatalf("circular-referencing skill failed to load: %v", err)
		case s := <-done:
			if s.Instructions == "" {
				t.Fatalf("skill %q loaded with empty instructions", s.Name)
			}
		}
	}
}

func TestConcurrentSkillLoads(t *testing.T) {
	dir := mkSkillsHome(t)
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "conc-"+string(rune('0'+i))+".md"),
			[]byte("---\ndescription: concurrent\n---\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				name := "conc-" + string(rune('0'+i%5))
				s, err := Load(name)
				if err != nil {
					errs <- err
					return
				}
				if s.Name != name {
					errs <- fmt.Errorf("loaded %q, want %q", s.Name, name)
					return
				}
			}
			list, err := List()
			if err != nil {
				errs <- err
				return
			}
			if len(list) != 5 {
				errs <- fmt.Errorf("List returned %d, want 5", len(list))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent skill load error: %v", err)
	}
}

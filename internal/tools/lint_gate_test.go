package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStrReplacePythonLintGate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.py")
	orig := "def f():\n    return 1\n"
	os.WriteFile(p, []byte(orig), 0644)
	if _, err := StrReplaceInFile(p, "return 1", "return (1"); err == nil {
		t.Fatal("expected syntax error")
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatalf("file not reverted: %q", b)
	}
	if _, err := StrReplaceInFile(p, "return 1", "return 2"); err != nil {
		t.Fatal(err)
	}
}

package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizePatch(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"a\nb":              "a\nb\n",
		"a\nb\n":            "a\nb\n",
		"a\r\nb\r\n":        "a\nb\n",
		"diff\n+x":          "diff\n+x\n",
	}
	for in, want := range cases {
		if got := normalizePatch(in); got != want {
			t.Errorf("normalizePatch(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTestPatchAppliedIdempotent(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "init")
	// Deliberately no trailing newline: mimics `$(cat file)`.
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1,2 @@\n one\n+two"
	ctx := context.Background()
	if testPatchApplied(ctx, dir, patch) {
		t.Fatal("patch reported applied before apply")
	}
	files, err := applyTestPatch(ctx, dir, patch)
	if err != nil || len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("apply: files=%v err=%v", files, err)
	}
	if !testPatchApplied(ctx, dir, patch) {
		t.Fatal("patch not detected as applied")
	}
}

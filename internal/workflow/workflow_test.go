package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWorkflowFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wf.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFile(t *testing.T) {
	p := writeWorkflowFile(t, `
name: example-workflow
steps:
  - name: research
    prompt: "Research {{index .input \"topic\"}}"
`)
	wf, err := LoadFile(p)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if wf.Name != "example-workflow" {
		t.Fatalf("Name = %q, want %q", wf.Name, "example-workflow")
	}
	if len(wf.Steps) != 1 || wf.Steps[0].Name != "research" {
		t.Fatalf("Steps = %+v, want one research step", wf.Steps)
	}
	if wf.Steps[0].Prompt != `Research {{index .input "topic"}}` {
		t.Fatalf("Step prompt = %q", wf.Steps[0].Prompt)
	}
}

func TestLoadFileMissing(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("LoadFile(missing) returned a nil error")
	}
}

func TestRenderPrompt(t *testing.T) {
	out, err := renderPrompt(`Write about {{index .input "topic"}} then revise {{index .output "prev"}}`,
		map[string]string{"topic": "go"}, map[string]string{"prev": "the intro"})
	if err != nil {
		t.Fatalf("renderPrompt: %v", err)
	}
	want := "Write about go then revise the intro"
	if out != want {
		t.Fatalf("renderPrompt = %q, want %q", out, want)
	}
}

func TestRenderPromptUnknownVar(t *testing.T) {
	out, err := renderPrompt(`{{.missing}}`, map[string]string{}, map[string]string{})
	if err == nil {
		t.Fatalf("renderPrompt(unknown var) returned a nil error, out=%q", out)
	}
}
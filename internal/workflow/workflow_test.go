package workflow

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRenderPromptChaos(t *testing.T) {
	large := strings.Repeat("a", 100*1024)
	cases := []struct {
		name    string
		tmpl    string
		inputs  map[string]string
		outputs map[string]string
		want    string
		wantErr bool
	}{
		{"empty", "", nil, nil, "", false},
		{"no template vars", "hello world", nil, nil, "hello world", false},
		{"known input var", "{{.input.name}}", map[string]string{"name": "alice"}, nil, "alice", false},
		{"known output var", "{{.output.step1}}", nil, map[string]string{"step1": "result"}, "result", false},
		{"unknown input var", "{{.input.missing}}", nil, nil, "", true},
		{"unknown output var", "{{.output.nope}}", nil, nil, "", true},
		{"malformed template", "{{.unclosed", nil, nil, "", true},
		{"literal newline escape", `line1\nline2`, nil, nil, "line1\nline2", false},
		{"html chars not escaped", "<script>alert(1)</script>", nil, nil, "<script>alert(1)</script>", false},
		{"very large prompt", large, nil, nil, large, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := renderPrompt(c.tmpl, c.inputs, c.outputs)
			if c.wantErr {
				if err == nil {
					t.Fatalf("renderPrompt(%q) returned nil error, out=%q", c.tmpl, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("renderPrompt(%q): %v", c.tmpl, err)
			}
			if got != c.want {
				t.Fatalf("renderPrompt(%q) = %q, want %q", c.tmpl, got, c.want)
			}
		})
	}
}

func TestWorkflowLoadFileChaos(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadFile(filepath.Join(t.TempDir(), "nope.yaml"))
		if err == nil || !strings.Contains(err.Error(), "load workflow") {
			t.Fatalf("LoadFile(missing) = %v, want error containing 'load workflow'", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		wf, err := LoadFile(writeWorkflowFile(t, ""))
		if err != nil {
			t.Fatalf("LoadFile(empty): %v", err)
		}
		if wf == nil || len(wf.Steps) != 0 {
			t.Fatalf("LoadFile(empty) = %+v, want zero steps", wf)
		}
	})

	t.Run("no name field", func(t *testing.T) {
		wf, err := LoadFile(writeWorkflowFile(t, "steps:\n  - name: a\n    prompt: p\n"))
		if err != nil {
			t.Fatalf("LoadFile(no name): %v", err)
		}
		if wf.Name != "" || len(wf.Steps) != 1 {
			t.Fatalf("LoadFile(no name) = %+v, want Name %q and 1 step", wf, "")
		}
	})

	t.Run("step missing prompt", func(t *testing.T) {
		wf, err := LoadFile(writeWorkflowFile(t, "name: w\nsteps:\n  - name: a\n"))
		if err != nil {
			t.Fatalf("LoadFile(step w/o prompt): %v", err)
		}
		if len(wf.Steps) != 1 || wf.Steps[0].Prompt != "" {
			t.Fatalf("LoadFile(step w/o prompt) = %+v, want empty prompt", wf)
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		p := writeWorkflowFile(t, "name: x\nsteps:\n  - name: a\n   prompt: bad-indent\n")
		_, err := LoadFile(p)
		if err == nil || !strings.Contains(err.Error(), "parse workflow") {
			t.Fatalf("LoadFile(malformed) = %v, want error containing 'parse workflow'", err)
		}
	})

	t.Run("zero byte file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "empty.yaml")
		if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		wf, err := LoadFile(p)
		if err != nil {
			t.Fatalf("LoadFile(0-byte): %v", err)
		}
		if wf == nil || len(wf.Steps) != 0 {
			t.Fatalf("LoadFile(0-byte) = %+v, want zero steps", wf)
		}
	})
}

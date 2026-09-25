package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"

	_ "github.com/ncruces/go-sqlite3/driver"
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

// wfStubProvider is a mock LLM provider that always returns the same response.
type wfStubProvider struct{ resp string }

func (wfStubProvider) Name() string                       { return "wf-stub" }
func (wfStubProvider) Available(context.Context) bool     { return true }
func (s wfStubProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{Text: s.resp, IsLast: true}
	close(ch)
	return ch, nil
}
func (wfStubProvider) Embed(context.Context, string) ([]float32, error) {
	return []float32{0.1}, nil
}

func testAgent(t *testing.T, resp string) *agent.Agent {
	t.Helper()
	router := llm.NewRouter()
	router.Register(llm.WorkloadHeal, wfStubProvider{resp: resp})
	store, err := memory.New(filepath.Join(t.TempDir(), "wf.db"))
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return agent.New(nil, router, store)
}

func TestRenderPrompt500VarsDefined(t *testing.T) {
	var tmpl strings.Builder
	inputs := map[string]string{}
	var want strings.Builder
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("v%d", i)
		val := fmt.Sprintf("value-%d", i)
		tmpl.WriteString(fmt.Sprintf("{{index .input %q}} ", key))
		inputs[key] = val
		want.WriteString(val)
		want.WriteByte(' ')
	}
	got, err := renderPrompt(tmpl.String(), inputs, nil)
	if err != nil {
		t.Fatalf("renderPrompt(500 vars defined): %v", err)
	}
	if got != want.String() {
		t.Fatalf("renderPrompt(500 vars) = %q, want %q", got, want.String())
	}
}

func TestRenderPrompt500VarsUndefined(t *testing.T) {
	var tmpl strings.Builder
	for i := 0; i < 500; i++ {
		tmpl.WriteString(fmt.Sprintf("{{.input.v%d}} ", i))
	}
	_, err := renderPrompt(tmpl.String(), nil, nil)
	if err == nil {
		t.Fatal("renderPrompt(500 undefined vars) returned nil error, want missingkey error")
	}
	if !strings.Contains(err.Error(), "map has no entry") {
		t.Fatalf("renderPrompt(500 undefined vars) err = %q, want missingkey error", err)
	}
}

func TestRenderPrompt500VarsUndefinedIndexGraceful(t *testing.T) {
	// {{index .input "k"}} does not trigger missingkey=error; missing keys
	// render as empty strings instead. This documents that both failure modes
	// are graceful (error or empty), never a panic or hang.
	var tmpl strings.Builder
	for i := 0; i < 500; i++ {
		tmpl.WriteString(fmt.Sprintf("[{{index .input %q}}]", fmt.Sprintf("gone%d", i)))
	}
	out, err := renderPrompt(tmpl.String(), map[string]string{}, nil)
	if err != nil {
		t.Fatalf("renderPrompt(index missing 500 vars): %v", err)
	}
	if strings.Count(out, "[]") != 500 {
		t.Fatalf("renderPrompt(index missing) = %q, want 500 empty [] pairs", out)
	}
}

func TestLoadFileValidJSONNotWorkflowSchema(t *testing.T) {
	t.Run("arbitrary JSON object", func(t *testing.T) {
		wf, err := LoadFile(writeWorkflowFile(t, `{"hello": "world", "n": 42}`))
		if err != nil {
			t.Fatalf("LoadFile(arbitrary JSON) = %v, want a zero-step workflow", err)
		}
		if wf == nil || len(wf.Steps) != 0 || wf.Name != "" {
			t.Fatalf("LoadFile(arbitrary JSON) = %+v, want empty workflow", wf)
		}
	})

	t.Run("JSON with scalar steps", func(t *testing.T) {
		_, err := LoadFile(writeWorkflowFile(t, `{"name": "bad", "steps": "not-a-list"}`))
		if err == nil {
			t.Fatal("LoadFile(JSON scalar steps) returned nil error")
		}
	})

	t.Run("JSON with steps but no workflow fields", func(t *testing.T) {
		wf, err := LoadFile(writeWorkflowFile(t, `{"steps": [{"name": "x", "prompt": "p"}], "y": [1,2,3]}`))
		if err != nil {
			t.Fatalf("LoadFile(JSON steps) = %v", err)
		}
		if wf == nil || len(wf.Steps) != 1 || wf.Steps[0].Name != "x" {
			t.Fatalf("LoadFile(JSON steps) = %+v, want one x step", wf)
		}
	})
}

func TestCircularStepReferences(t *testing.T) {
	p := writeWorkflowFile(t, `
name: circular
steps:
  - name: a
    prompt: "first {{index .input \"x\"}}"
    depends_on: [b]
  - name: b
    prompt: "second"
    depends_on: [a, c]
  - name: c
    prompt: "third"
    depends_on: [b]
`)
	wf, err := LoadFile(p)
	if err != nil {
		t.Fatalf("LoadFile(circular depends_on): %v", err)
	}
	if len(wf.Steps) != 3 {
		t.Fatalf("LoadFile(circular) steps = %d, want 3", len(wf.Steps))
	}

	// depends_on is not resolved into a graph by Run(); steps execute in
	// declaration order, so circular references must not recurse or hang.
	a := testAgent(t, "stub-output")
	outputs, err := wf.Run(context.Background(), a, map[string]string{"x": "go"})
	if err != nil {
		t.Fatalf("Run(circular depends_on): %v", err)
	}
	if len(outputs) != 3 {
		t.Fatalf("Run(circular) produced %d outputs, want 3", len(outputs))
	}
	if outputs["a"] != "stub-output" || outputs["b"] != "stub-output" || outputs["c"] != "stub-output" {
		t.Fatalf("Run(circular) outputs = %+v", outputs)
	}
}

func TestRenderPromptConcurrent(t *testing.T) {
	tmpl := "write about {{index .input \"topic\"}} then {{index .output \"prev\"}}"
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				got, err := renderPrompt(tmpl, map[string]string{"topic": "go"}, map[string]string{"prev": "intro"})
				if err != nil {
					errs <- err
					return
				}
				if got != "write about go then intro" {
					errs <- fmt.Errorf("goroutine %d: rendered %q", i, got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent renderPrompt error: %v", err)
	}
}

func TestLoadFileSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-workflow.yaml")
	if err := os.WriteFile(real, []byte("name: symlinked\nsteps:\n  - name: s\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "wf-link.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	wf, err := LoadFile(link)
	if err != nil {
		t.Fatalf("LoadFile(symlink): %v", err)
	}
	if wf.Name != "symlinked" || len(wf.Steps) != 1 {
		t.Fatalf("LoadFile(symlink) = %+v, want symlinked workflow with one step", wf)
	}
}

func TestLoadFileFifo(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "wf.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	done := make(chan struct{})
	var (
		wf  *Workflow
		err error
	)
	go func() {
		defer close(done)
		wf, err = LoadFile(fifo)
	}()

	// Opening a FIFO for reading blocks until a writer appears. Wait a beat to
	// confirm the loader is parked (i.e. it does not spin or deadlock on its
	// own), then supply content and require completion.
	select {
	case <-done:
		// Loader returned without a writer (e.g. odd filesystem semantics):
		// as long as it returned, nothing hangs.
		t.Logf("LoadFile(fifo) returned before writer: err=%v", err)
		return
	case <-time.After(200 * time.Millisecond):
	}

	w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open fifo for writing: %v", err)
	}
	if _, err := w.Write([]byte("name: fifo-wf\nsteps:\n  - name: s\n    prompt: p\n")); err != nil {
		t.Fatalf("write fifo: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close fifo writer: %v", err)
	}

	select {
	case <-done:
		if err != nil {
			t.Fatalf("LoadFile(fifo) after write: %v", err)
		}
		if wf == nil || wf.Name != "fifo-wf" || len(wf.Steps) != 1 {
			t.Fatalf("LoadFile(fifo) = %+v, want fifo-wf with one step", wf)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadFile(fifo) hung after the writer closed")
	}
}

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	npty "github.com/creack/pty"
	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
)

type stubProvider struct{ response string }

func (stubProvider) Name() string                       { return "stub" }
func (stubProvider) Available(ctx context.Context) bool { return true }
func (s stubProvider) Complete(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{Text: s.response, IsLast: true}
	close(ch)
	return ch, nil
}
func (stubProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func newTestStore(t *testing.T) memory.Store {
	t.Helper()
	s, err := memory.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDetectDomain(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"how do I run grep recursively", "terminal"},
		{"install docker on ubuntu", "terminal"},
		{"fix this golang bug in my code", "code"},
		{"debug the panic in this function", "code"},
		{"write an essay about the ocean", "writing"},
		{"draft an email to my boss", "writing"},
		{"explain the difference between TCP and UDP", "research"},
		{"give me an overview of climate science", "research"},
		{"what is going on", "general"},
	}
	for _, c := range cases {
		if got := detectDomain(c.input); got != c.want {
			t.Errorf("detectDomain(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func stdinTTY(t *testing.T) func() {
	t.Helper()
	ptmx, tty, err := npty.Open()
	if err != nil {
		t.Skipf("skipping: cannot open pty: %v", err)
	}
	if err := syscall.Dup2(int(tty.Fd()), 0); err != nil {
		t.Fatalf("dup2 pty onto stdin: %v", err)
	}
	return func() {
		_ = tty.Close()
		_ = ptmx.Close()
		dn, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
		if err == nil {
			_ = syscall.Dup2(int(dn.Fd()), 0)
			_ = dn.Close()
		}
	}
}

func TestRunDoomLoop(t *testing.T) {
	defer stdinTTY(t)()
	a := New(pty.NewHarness(0), llm.NewRouter(), newTestStore(t))
	ctx := context.Background()
	args := []string{"sh", "-c", "echo boom; exit 1"}
	opts := RunOptions{SkipPermissions: true}

	for i := 0; i < 3; i++ {
		res, err := a.Run(ctx, args, opts)
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i+1, err)
		}
		if res == nil || res.ExitCode != 1 || res.DoomLoopCount != i+1 {
			t.Fatalf("attempt %d: got %+v, want exit 1 with DoomLoopCount %d", i+1, res, i+1)
		}
	}

	res, err := a.Run(ctx, args, opts)
	if err == nil || !strings.Contains(err.Error(), "healing loop detected") {
		t.Fatalf("4th attempt: want healing loop error, got res=%+v err=%v", res, err)
	}
}

func TestPlannerExecutorWiring(t *testing.T) {
	router := llm.NewRouter()
	router.Register(llm.WorkloadDiagnose, stubProvider{response: "FIX: echo ok\nEXPLANATION: works"})

	planner := NewPlanner(router, newTestStore(t))
	sugg, err := planner.Plan(context.Background(), "cmd --fail", "boom")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if sugg == nil {
		t.Fatal("Plan returned a nil suggestion")
	}
	if sugg.FixCmd != "echo ok" {
		t.Fatalf("Plan FixCmd = %q, want %q", sugg.FixCmd, "echo ok")
	}

	executor := NewExecutor(pty.NewHarness(0), router, newTestStore(t))
	approved := false
	approver := func(cmd string, _ safety.Risk) bool { approved = true; return false }
	if err := executor.Execute(context.Background(), sugg, "boom", approver); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !approved {
		t.Fatal("Execute never consulted the approval function")
	}
}
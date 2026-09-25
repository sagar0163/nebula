package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/sagar0163/nebula/internal/models"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSaveTaskListTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := &models.Task{Input: "help me write a poem", Response: "roses are red", Domain: "writing"}
	if err := s.SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if task.ID == "" {
		t.Fatal("SaveTask did not assign an ID")
	}

	tasks, err := s.ListTasks(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListTasks len = %d, want 1", len(tasks))
	}
	if tasks[0].Input != task.Input || tasks[0].Response != task.Response || tasks[0].Domain != "writing" {
		t.Fatalf("ListTasks = %+v, want stored task", tasks[0])
	}

	other := &models.Task{SessionID: "sess-1", Input: "x", Response: "y", Domain: "code"}
	if err := s.SaveTask(ctx, other); err != nil {
		t.Fatalf("SaveTask second: %v", err)
	}

	sessTasks, err := s.ListTasks(ctx, "sess-1", 0)
	if err != nil {
		t.Fatalf("ListTasks by session: %v", err)
	}
	if len(sessTasks) != 1 || sessTasks[0].ID != other.ID {
		t.Fatalf("ListTasks(sess-1) = %+v, want only the session task", sessTasks)
	}

	if err := s.SaveTask(ctx, nil); err == nil {
		t.Fatal("SaveTask(nil) returned a nil error")
	}
}

func TestWorkflowJobRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	job := &models.WorkflowJob{WorkflowFile: "wf.yaml", Inputs: `{"topic":"go"}`, Status: "running"}
	if err := s.SaveWorkflowJob(ctx, job); err != nil {
		t.Fatalf("SaveWorkflowJob: %v", err)
	}
	if job.ID == "" {
		t.Fatal("SaveWorkflowJob did not assign an ID")
	}

	got, err := s.GetWorkflowJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetWorkflowJob: %v", err)
	}
	if got.Status != "running" || got.WorkflowFile != "wf.yaml" {
		t.Fatalf("GetWorkflowJob = %+v, want running wf.yaml", got)
	}

	job.Status = "done"
	job.CurrentStep = ""
	job.Output = `{"step1":"out"}`
	if err := s.UpdateWorkflowJob(ctx, job); err != nil {
		t.Fatalf("UpdateWorkflowJob: %v", err)
	}

	got, err = s.GetWorkflowJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetWorkflowJob after update: %v", err)
	}
	if got.Status != "done" || got.Output != `{"step1":"out"}` || got.CurrentStep != "" {
		t.Fatalf("updated job = %+v, want done with output", got)
	}

	jobs, err := s.ListWorkflowJobs(ctx, 0)
	if err != nil {
		t.Fatalf("ListWorkflowJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("ListWorkflowJobs = %+v, want 1 job", jobs)
	}

	if _, err := s.GetWorkflowJob(ctx, "does-not-exist"); err != sql.ErrNoRows {
		t.Fatalf("GetWorkflowJob(missing) = %v, want sql.ErrNoRows", err)
	}
}

func recentByRaw(t *testing.T, s *SQLiteStore, raw string) []*models.Command {
	t.Helper()
	cmds, err := s.RecentCommands(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("RecentCommands: %v", err)
	}
	var out []*models.Command
	for _, c := range cmds {
		if c.Raw == raw {
			out = append(out, c)
		}
	}
	return out
}

func TestSaveCommandChaos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("empty raw", func(t *testing.T) {
		cmd := &models.Command{Raw: ""}
		if err := s.SaveCommand(ctx, cmd); err != nil {
			t.Fatalf("SaveCommand(empty raw): %v", err)
		}
		got := recentByRaw(t, s, "")
		if len(got) != 1 {
			t.Fatalf("retrieved %d empty-raw commands, want 1", len(got))
		}
	})

	t.Run("sql injection raw", func(t *testing.T) {
		raw := "'; DROP TABLE commands; --"
		if err := s.SaveCommand(ctx, &models.Command{Raw: raw}); err != nil {
			t.Fatalf("SaveCommand(injection): %v", err)
		}
		if got := recentByRaw(t, s, raw); len(got) != 1 {
			t.Fatalf("retrieved %d injected commands, want 1", len(got))
		}
		if err := s.SaveCommand(ctx, &models.Command{Raw: "still works"}); err != nil {
			t.Fatalf("SaveCommand after injection: %v (commands table dropped?)", err)
		}
	})

	t.Run("null bytes in stdout", func(t *testing.T) {
		stdout := "a\x00b\x00c"
		cmd := &models.Command{Raw: "with-nul", Stdout: stdout}
		if err := s.SaveCommand(ctx, cmd); err != nil {
			t.Fatalf("SaveCommand(null bytes): %v", err)
		}
		got := recentByRaw(t, s, "with-nul")
		if len(got) != 1 {
			t.Fatalf("retrieved %d null-byte commands, want 1", len(got))
		}
		if got[0].Stdout != stdout {
			t.Fatalf("stdout round-trip = %q, want %q", got[0].Stdout, stdout)
		}
	})

	t.Run("1MB stdout", func(t *testing.T) {
		stdout := strings.Repeat("x", 1<<20)
		cmd := &models.Command{Raw: "big-output", Stdout: stdout}
		if err := s.SaveCommand(ctx, cmd); err != nil {
			t.Fatalf("SaveCommand(1MB stdout): %v", err)
		}
		got := recentByRaw(t, s, "big-output")
		if len(got) != 1 || got[0].Stdout != stdout {
			t.Fatalf("1MB stdout did not round-trip (len=%d)", len(got[0].Stdout))
		}
	})

	t.Run("extreme exit codes", func(t *testing.T) {
		for _, code := range []int{255, -1} {
			if err := s.SaveCommand(ctx, &models.Command{Raw: fmt.Sprintf("exit-%d", code), ExitCode: code}); err != nil {
				t.Fatalf("SaveCommand(exit %d): %v", code, err)
			}
			got := recentByRaw(t, s, fmt.Sprintf("exit-%d", code))
			if len(got) != 1 || got[0].ExitCode != code {
				t.Fatalf("exit code %d stored as %d", code, got[0].ExitCode)
			}
		}
	})
}

func TestFindPatternChaos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	embed := []byte{1, 2, 3}

	pattern := func(failCmd, fixCmd string, useCount int) *models.Pattern {
		return &models.Pattern{FailCmd: failCmd, FixCmd: fixCmd, SuccessRate: 1.0, UseCount: useCount, Embedding: embed}
	}

	t.Run("no patterns", func(t *testing.T) {
		p, err := s.FindPattern(ctx, "anything", "")
		if err != nil {
			t.Fatalf("FindPattern(empty store): %v", err)
		}
		if p != nil {
			t.Fatalf("FindPattern(empty store) = %+v, want nil", p)
		}
	})

	t.Run("exact match", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("docker compose up", "docker compose up --scale 2", 3)); err != nil {
			t.Fatalf("SavePattern: %v", err)
		}
		p, err := s.FindPattern(ctx, "docker compose up", "")
		if err != nil {
			t.Fatalf("FindPattern: %v", err)
		}
		if p == nil || p.FixCmd != "docker compose up --scale 2" {
			t.Fatalf("FindPattern = %+v, want exact-match pattern", p)
		}
	})

	t.Run("case sensitive", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("Git Status", "git status --short", 5)); err != nil {
			t.Fatalf("SavePattern: %v", err)
		}
		p, err := s.FindPattern(ctx, "git status", "")
		if err != nil {
			t.Fatalf("FindPattern: %v", err)
		}
		if p != nil {
			t.Fatalf("FindPattern(lowercase) = %+v, want nil (case-sensitive)", p)
		}
	})

	t.Run("substring not a match", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("git status --porcelain", "x", 2)); err != nil {
			t.Fatalf("SavePattern: %v", err)
		}
		p, err := s.FindPattern(ctx, "git status", "")
		if err != nil {
			t.Fatalf("FindPattern: %v", err)
		}
		if p != nil {
			t.Fatalf("FindPattern(substring) = %+v, want nil (exact match only)", p)
		}
	})

	t.Run("highest use count", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("flakey cmd", "fix-low", 1)); err != nil {
			t.Fatalf("SavePattern low: %v", err)
		}
		if err := s.SavePattern(ctx, pattern("flakey cmd", "fix-high", 10)); err != nil {
			t.Fatalf("SavePattern high: %v", err)
		}
		p, err := s.FindPattern(ctx, "flakey cmd", "")
		if err != nil {
			t.Fatalf("FindPattern: %v", err)
		}
		if p == nil || p.FixCmd != "fix-high" {
			t.Fatalf("FindPattern = %+v, want highest use_count pattern", p)
		}
	})

	t.Run("concurrent reads and writes", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				failCmd := fmt.Sprintf("concurrent-%d", i%2)
				for j := 0; j < 15; j++ {
					if err := s.SavePattern(ctx, pattern(failCmd, "fix", 1)); err != nil {
						t.Errorf("concurrent SavePattern: %v", err)
					}
					if _, err := s.FindPattern(ctx, failCmd, ""); err != nil {
						t.Errorf("concurrent FindPattern: %v", err)
					}
				}
			}(i)
		}
		wg.Wait()
	})
}

func TestWorkflowJobChaos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("empty id assigned", func(t *testing.T) {
		job := &models.WorkflowJob{WorkflowFile: "wf.yaml", Status: "running"}
		if err := s.SaveWorkflowJob(ctx, job); err != nil {
			t.Fatalf("SaveWorkflowJob: %v", err)
		}
		if job.ID == "" {
			t.Fatal("SaveWorkflowJob did not assign an ID for empty ID")
		}
	})

	t.Run("update non-existent id", func(t *testing.T) {
		ghost := &models.WorkflowJob{ID: "does-not-exist", Status: "done"}
		err := s.UpdateWorkflowJob(ctx, ghost)
		if err != nil {
			t.Fatalf("UpdateWorkflowJob(missing) = %v, want nil no-op", err)
		}
		if _, err := s.GetWorkflowJob(ctx, "does-not-exist"); err != sql.ErrNoRows {
			t.Fatalf("GetWorkflowJob after ghost update = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("list limit zero returns all", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if err := s.SaveWorkflowJob(ctx, &models.WorkflowJob{WorkflowFile: fmt.Sprintf("wf-%d.yaml", i), Status: "done"}); err != nil {
				t.Fatalf("SaveWorkflowJob: %v", err)
			}
		}
		jobs, err := s.ListWorkflowJobs(ctx, 0)
		if err != nil {
			t.Fatalf("ListWorkflowJobs(0): %v", err)
		}
		if len(jobs) < 3 {
			t.Fatalf("ListWorkflowJobs(0) = %d jobs, want all (%d+)", len(jobs), 3)
		}
	})

	t.Run("status transitions persist", func(t *testing.T) {
		t.Run("running to done", func(t *testing.T) {
			job := &models.WorkflowJob{WorkflowFile: "t.yaml", Status: "running"}
			if err := s.SaveWorkflowJob(ctx, job); err != nil {
				t.Fatalf("SaveWorkflowJob: %v", err)
			}
			job.Status = "done"
			job.Output = `{"s1":"o"}`
			if err := s.UpdateWorkflowJob(ctx, job); err != nil {
				t.Fatalf("UpdateWorkflowJob: %v", err)
			}
			got, err := s.GetWorkflowJob(ctx, job.ID)
			if err != nil {
				t.Fatalf("GetWorkflowJob: %v", err)
			}
			if got.Status != "done" || got.Output != `{"s1":"o"}` {
				t.Fatalf("job = %+v, want done with output", got)
			}
		})

		t.Run("running to failed", func(t *testing.T) {
			job := &models.WorkflowJob{WorkflowFile: "t.yaml", Status: "running"}
			if err := s.SaveWorkflowJob(ctx, job); err != nil {
				t.Fatalf("SaveWorkflowJob: %v", err)
			}
			job.Status = "failed"
			job.Error = "step broke"
			if err := s.UpdateWorkflowJob(ctx, job); err != nil {
				t.Fatalf("UpdateWorkflowJob: %v", err)
			}
			got, err := s.GetWorkflowJob(ctx, job.ID)
			if err != nil {
				t.Fatalf("GetWorkflowJob: %v", err)
			}
			if got.Status != "failed" || got.Error != "step broke" {
				t.Fatalf("job = %+v, want failed with error", got)
			}
		})
	})
}

func TestSaveCommandDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, &models.Session{ID: "sess-dup", WorkDir: "/tmp"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	first := &models.Command{SessionID: "sess-dup", Raw: "ls -la", ExitCode: 0, Stdout: "first"}
	if err := s.SaveCommand(ctx, first); err != nil {
		t.Fatalf("SaveCommand(first): %v", err)
	}
	time.Sleep(10 * time.Millisecond) // ensure a distinct created_at
	second := &models.Command{SessionID: "sess-dup", Raw: "ls -la", ExitCode: 1, Stdout: "second"}
	if err := s.SaveCommand(ctx, second); err != nil {
		t.Fatalf("SaveCommand(duplicate): %v", err)
	}

	// A duplicate save must not error; both executions remain in history, newest first.
	cmds := recentByRaw(t, s, "ls -la")
	if len(cmds) != 2 {
		t.Fatalf("duplicate save kept %d rows, want 2", len(cmds))
	}
	if cmds[0].Stdout != "second" || cmds[1].Stdout != "first" {
		t.Fatalf("history order = [%q %q], want [second first]", cmds[0].Stdout, cmds[1].Stdout)
	}

	// The duplicate save also updates (touches) the session.
	before, _ := s.GetSession(ctx, "sess-dup")
	time.Sleep(10 * time.Millisecond)
	if err := s.SaveCommand(ctx, &models.Command{SessionID: "sess-dup", Raw: "ls -la"}); err != nil {
		t.Fatalf("SaveCommand(third): %v", err)
	}
	after, err := s.GetSession(ctx, "sess-dup")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("session UpdatedAt = %v not after %v; duplicate save did not update", after.UpdatedAt, before.UpdatedAt)
	}
}

func TestFindPatternSqlWildcards(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	wild := "select * from users where name like '100%_x'"
	if err := s.SavePattern(ctx, &models.Pattern{FailCmd: wild, FixCmd: "fix", SuccessRate: 1, UseCount: 1}); err != nil {
		t.Fatalf("SavePattern(wildcard cmd): %v", err)
	}
	// A command full of LIKE metacharacters must round-trip by exact match.
	got, err := s.FindPattern(ctx, wild, "")
	if err != nil {
		t.Fatalf("FindPattern(wildcards): %v", err)
	}
	if got == nil || got.FixCmd != "fix" {
		t.Fatalf("FindPattern(wildcards) = %+v, want exact-match row", got)
	}

	// A LIKE-flavoured query must NOT match every row.
	p, err := s.FindPattern(ctx, "select * from users", "")
	if err != nil {
		t.Fatalf("FindPattern(prefix): %v", err)
	}
	if p != nil {
		t.Fatalf("FindPattern(prefix) = %+v, want nil (no LIKE semantics)", p)
	}

	for _, token := range []string{"%", "_", "*", "100%", "_x"} {
		if _, err := s.FindPattern(ctx, token, ""); err != nil {
			t.Fatalf("FindPattern(%q): %v", token, err)
		}
	}

	if err := s.SavePattern(ctx, &models.Pattern{FailCmd: "%", FixCmd: "percent", SuccessRate: 1, UseCount: 2}); err != nil {
		t.Fatalf("SavePattern(%%): %v", err)
	}
	pct, err := s.FindPattern(ctx, "%", "")
	if err != nil {
		t.Fatalf("FindPattern(%%): %v", err)
	}
	if pct == nil || pct.FixCmd != "percent" {
		t.Fatalf("FindPattern(%% ) = %+v, want the literal '%%' pattern", pct)
	}
	if other, err := s.FindPattern(ctx, "select *", ""); err != nil || other != nil {
		t.Fatalf("FindPattern(select *) = %+v err=%v, want nil (no wildcard expansion)", other, err)
	}
}

func TestFindPatternConcurrentSameKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SavePattern(ctx, &models.Pattern{FailCmd: "hot-key", FixCmd: "fix", SuccessRate: 1, UseCount: 5}); err != nil {
		t.Fatalf("SavePattern: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				p, err := s.FindPattern(ctx, "hot-key", "")
				if err != nil {
					errs <- err
					return
				}
				if p == nil || p.FixCmd != "fix" {
					errs <- errors.New("FindPattern returned nil/wrong row")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent FindPattern error: %v", err)
	}
}

func TestStoreContextTimeout(t *testing.T) {
	s := newTestStore(t)
	if err := s.SavePattern(context.Background(), &models.Pattern{FailCmd: "cmd", FixCmd: "fix", SuccessRate: 1, UseCount: 1}); err != nil {
		t.Fatalf("SavePattern: %v", err)
	}
	if err := s.SaveCommand(context.Background(), &models.Command{Raw: "cmd", ExitCode: 0}); err != nil {
		t.Fatalf("SaveCommand: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // let the deadline expire

	if _, err := s.FindPattern(ctx, "cmd", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("FindPattern(expired ctx) = %v, want context deadline exceeded", err)
	}
	if err := s.SaveCommand(ctx, &models.Command{Raw: "late"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SaveCommand(expired ctx) = %v, want context deadline exceeded", err)
	}
}

func TestStoreThousandPatternsPerformance(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 1000; i++ {
		if err := s.SavePattern(ctx, &models.Pattern{
			FailCmd:     fmt.Sprintf("fail-%d", i),
			FailOutput:  fmt.Sprintf("out-%d", i),
			FixCmd:      fmt.Sprintf("fix-%d", i),
			SuccessRate: 0.5,
			UseCount:    i,
			Embedding:   nil,
		}); err != nil {
			t.Fatalf("SavePattern %d: %v", i, err)
		}
	}
	saveDur := time.Since(start)

	start = time.Now()
	for _, probe := range []string{"fail-0", "fail-500", "fail-999", "missing-key"} {
		outProbe := ""
		if strings.HasPrefix(probe, "fail-") {
			outProbe = strings.Replace(probe, "fail-", "out-", 1)
		}
		if _, err := s.FindPattern(ctx, probe, outProbe); err != nil {
			t.Fatalf("FindPattern(%q): %v", probe, err)
		}
	}
	queryDur := time.Since(start)

	// The heavy requirement: querying against 1000 stored patterns completes within
	// 30 seconds. The bulk-insert burst gets a generous sanity bound.
	if queryDur >= 30*time.Second {
		t.Fatalf("query over 1000 patterns took %v, want < 30s", queryDur)
	}
	if total := saveDur + queryDur; total > 30*time.Second {
		t.Fatalf("save+query over %d patterns took %v, suspiciously slow", 1000, total)
	}
}

func TestPatternUpdateAtomicity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	save := func(useCount int, rate float64) error {
		return s.SavePattern(ctx, &models.Pattern{
			FailCmd: "updatable", FixCmd: "fix", SuccessRate: rate, UseCount: useCount,
		})
	}
	if err := save(1, 1.0); err != nil {
		t.Fatalf("SavePattern(v1): %v", err)
	}
	if err := save(10, 0.9); err != nil {
		t.Fatalf("SavePattern(v10): %v", err)
	}

	got, err := s.FindPattern(ctx, "updatable", "")
	if err != nil {
		t.Fatalf("FindPattern: %v", err)
	}
	if got == nil {
		t.Fatal("FindPattern = nil")
	}
	// Both counters update together — no torn read where use_count is new but
	// success_rate is stale (or vice versa).
	if got.UseCount != 10 || got.SuccessRate != 0.9 {
		t.Fatalf("pattern = use_count %d rate %v, want updated pair {10 0.9} atomically", got.UseCount, got.SuccessRate)
	}

	// Concurrent increments must converge to the highest committed version.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			use := 100 + i
			if err := s.SavePattern(ctx, &models.Pattern{
				FailCmd: "updatable", FixCmd: "fix", SuccessRate: float64(use) / 200, UseCount: use,
			}); err != nil {
				t.Errorf("concurrent SavePattern(use=%d): %v", use, err)
			}
		}(i)
	}
	wg.Wait()

	got, err = s.FindPattern(ctx, "updatable", "")
	if err != nil {
		t.Fatalf("FindPattern after increments: %v", err)
	}
	if got.UseCount != 109 {
		t.Fatalf("final use_count = %d, want 109 (highest committed)", got.UseCount)
	}
	if got.SuccessRate != float64(109)/200 {
		t.Fatalf("final success_rate = %v, want %v (paired with use_count)", got.SuccessRate, float64(109)/200)
	}
}

func TestPatternEmptyFixCmdRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SavePattern(ctx, &models.Pattern{FailCmd: "empty-fix", FixCmd: "", SuccessRate: 0.5, UseCount: 3}); err != nil {
		t.Fatalf("SavePattern(empty fix_cmd): %v", err)
	}
	got, err := s.FindPattern(ctx, "empty-fix", "")
	if err != nil {
		t.Fatalf("FindPattern: %v", err)
	}
	if got == nil {
		t.Fatal("FindPattern returned nil for a stored empty-fix pattern")
	}
	if got.FixCmd != "" || got.UseCount != 3 {
		t.Fatalf("recalled pattern = %+v, want empty FixCmd with UseCount 3", got)
	}
}

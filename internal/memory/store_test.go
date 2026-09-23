package memory

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

func TestFindPatternByCmdChaos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	embed := []byte{1, 2, 3}

	pattern := func(failCmd, fixCmd string, useCount int) *models.Pattern {
		return &models.Pattern{FailCmd: failCmd, FixCmd: fixCmd, SuccessRate: 1.0, UseCount: useCount, Embedding: embed}
	}

	t.Run("no patterns", func(t *testing.T) {
		p, err := s.FindPatternByCmd(ctx, "anything")
		if err != nil {
			t.Fatalf("FindPatternByCmd(empty store): %v", err)
		}
		if p != nil {
			t.Fatalf("FindPatternByCmd(empty store) = %+v, want nil", p)
		}
	})

	t.Run("exact match", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("docker compose up", "docker compose up --scale 2", 3)); err != nil {
			t.Fatalf("SavePattern: %v", err)
		}
		p, err := s.FindPatternByCmd(ctx, "docker compose up")
		if err != nil {
			t.Fatalf("FindPatternByCmd: %v", err)
		}
		if p == nil || p.FixCmd != "docker compose up --scale 2" {
			t.Fatalf("FindPatternByCmd = %+v, want exact-match pattern", p)
		}
	})

	t.Run("case sensitive", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("Git Status", "git status --short", 5)); err != nil {
			t.Fatalf("SavePattern: %v", err)
		}
		p, err := s.FindPatternByCmd(ctx, "git status")
		if err != nil {
			t.Fatalf("FindPatternByCmd: %v", err)
		}
		if p != nil {
			t.Fatalf("FindPatternByCmd(lowercase) = %+v, want nil (case-sensitive)", p)
		}
	})

	t.Run("substring not a match", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("git status --porcelain", "x", 2)); err != nil {
			t.Fatalf("SavePattern: %v", err)
		}
		p, err := s.FindPatternByCmd(ctx, "git status")
		if err != nil {
			t.Fatalf("FindPatternByCmd: %v", err)
		}
		if p != nil {
			t.Fatalf("FindPatternByCmd(substring) = %+v, want nil (exact match only)", p)
		}
	})

	t.Run("highest use count", func(t *testing.T) {
		if err := s.SavePattern(ctx, pattern("flakey cmd", "fix-low", 1)); err != nil {
			t.Fatalf("SavePattern low: %v", err)
		}
		if err := s.SavePattern(ctx, pattern("flakey cmd", "fix-high", 10)); err != nil {
			t.Fatalf("SavePattern high: %v", err)
		}
		p, err := s.FindPatternByCmd(ctx, "flakey cmd")
		if err != nil {
			t.Fatalf("FindPatternByCmd: %v", err)
		}
		if p == nil || p.FixCmd != "fix-high" {
			t.Fatalf("FindPatternByCmd = %+v, want highest use_count pattern", p)
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
					if _, err := s.FindPatternByCmd(ctx, failCmd); err != nil {
						t.Errorf("concurrent FindPatternByCmd: %v", err)
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

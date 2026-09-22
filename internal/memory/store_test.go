package memory

import (
	"context"
	"database/sql"
	"path/filepath"
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
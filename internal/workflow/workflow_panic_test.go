package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
)

type mockStore struct {
	memory.Store
	job *models.WorkflowJob
}

func (m *mockStore) SaveWorkflowJob(ctx context.Context, job *models.WorkflowJob) error { m.job = job; return nil }
func (m *mockStore) UpdateWorkflowJob(ctx context.Context, job *models.WorkflowJob) error {
	m.job = job
	return nil
}

func TestRunBackgroundPanicRecovery(t *testing.T) {
	wf := &Workflow{
		Name: "panic-test",
		Steps: []Step{
			{
				Name:   "step1",
				Prompt: "do something",
			},
		},
	}

	store := &mockStore{}

	// Pass nil for agent to force a panic
	_, err := wf.RunBackground(context.Background(), nil, store, nil)
	if err != nil {
		t.Fatalf("RunBackground returned error: %v", err)
	}
	
	// Wait a bit for the goroutine to finish and panic
	time.Sleep(100 * time.Millisecond)

	if store.job == nil {
		t.Fatal("expected job to be updated in store")
	}
	if store.job.Status != "failed" {
		t.Fatalf("expected job status 'failed', got %q", store.job.Status)
	}
	if !strings.Contains(store.job.Error, "panic") {
		t.Fatalf("expected job error to contain 'panic', got %q", store.job.Error)
	}
}

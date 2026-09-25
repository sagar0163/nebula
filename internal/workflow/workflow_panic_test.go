package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
)

type mockStore struct {
	memory.Store
	mu   sync.Mutex
	done chan struct{}
	job  *models.WorkflowJob
}

func newMockStore() *mockStore {
	return &mockStore{done: make(chan struct{}, 1)}
}

func (m *mockStore) SaveWorkflowJob(ctx context.Context, job *models.WorkflowJob) error {
	m.mu.Lock()
	m.job = job
	m.mu.Unlock()
	return nil
}
func (m *mockStore) UpdateWorkflowJob(ctx context.Context, job *models.WorkflowJob) error {
	m.mu.Lock()
	m.job = job
	m.mu.Unlock()
	if job.Status == "failed" || job.Status == "done" {
		select {
		case m.done <- struct{}{}:
		default:
		}
	}
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

	store := newMockStore()

	// Pass nil for agent to force a panic
	_, err := wf.RunBackground(context.Background(), nil, store, nil)
	if err != nil {
		t.Fatalf("RunBackground returned error: %v", err)
	}

	// Wait for the background goroutine to finish and reach a terminal state.
	select {
	case <-store.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for background job to finish")
	}

	store.mu.Lock()
	defer store.mu.Unlock()

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

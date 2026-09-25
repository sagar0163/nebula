package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/workflow"
)

func Watch(ctx context.Context, a *agent.Agent, store memory.Store, queueDir string) error {
	doneDir := filepath.Join(queueDir, "done")
	if err := os.MkdirAll(doneDir, 0o755); err != nil {
		return fmt.Errorf("create done dir: %w", err)
	}

	fmt.Printf("Watching %s for new workflows...\n", queueDir)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Initial check immediately
	checkQueue(ctx, a, store, queueDir, doneDir)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			checkQueue(ctx, a, store, queueDir, doneDir)
		}
	}
}

func checkQueue(ctx context.Context, a *agent.Agent, store memory.Store, queueDir, doneDir string) {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(queueDir, entry.Name())
		donePath := filepath.Join(doneDir, entry.Name())

		wf, err := workflow.LoadFile(path)
		if err != nil {
			fmt.Printf("Error loading %s: %v\n", entry.Name(), err)
			os.Rename(path, donePath+".error")
			continue
		}

		id, err := wf.RunBackground(context.Background(), a, store, map[string]string{})
		if err != nil {
			fmt.Printf("Error starting job for %s: %v\n", entry.Name(), err)
			os.Rename(path, donePath+".error")
			continue
		}

		fmt.Printf("Job started: %s (file: %s)\n", id, entry.Name())
		os.Rename(path, donePath)

		go func(jobID, filename string) {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					job, err := store.GetWorkflowJob(context.Background(), jobID)
					if err != nil {
						continue
					}
					if job.Status == "done" || job.Status == "failed" {
						fmt.Printf("Job finished: %s (file: %s, status: %s)\n", jobID, filename, job.Status)
						return
					}
				}
			}
		}(id, entry.Name())
	}
}

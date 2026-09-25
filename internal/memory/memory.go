package memory

import (
	"context"
	"math"

	"github.com/sagar0163/nebula/internal/models"
)

// Store is the interface for all persistent memory operations.
// Implementations back it with SQLite (see store.go).
type Store interface {
	// Session operations
	CreateSession(ctx context.Context, s *models.Session) error
	GetSession(ctx context.Context, id string) (*models.Session, error)
	ListSessions(ctx context.Context, limit int) ([]*models.Session, error)

	// Command history
	SaveCommand(ctx context.Context, cmd *models.Command) error
	RecentCommands(ctx context.Context, sessionID string, limit int) ([]*models.Command, error)

	// Pattern learning
	SavePattern(ctx context.Context, p *models.Pattern) error
	FindPattern(ctx context.Context, failCmd, failOutput string) (*models.Pattern, error)
	FindPatternsByKeywords(ctx context.Context, keywords []string, limit int) ([]*models.Pattern, error)


	// General tasks
	SaveTask(ctx context.Context, t *models.Task) error
	ListTasks(ctx context.Context, sessionID string, limit int) ([]*models.Task, error)

	// Workflow Jobs
	SaveWorkflowJob(ctx context.Context, j *models.WorkflowJob) error
	GetWorkflowJob(ctx context.Context, id string) (*models.WorkflowJob, error)
	UpdateWorkflowJob(ctx context.Context, j *models.WorkflowJob) error
	ListWorkflowJobs(ctx context.Context, limit int) ([]*models.WorkflowJob, error)

	Close() error
}

// CosineSimilarity returns the cosine similarity between two float32 vectors.
// At CLI scale (few thousand patterns) brute-force cosine is fast and zero-dependency.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}

	return float32(dot / denom)
}

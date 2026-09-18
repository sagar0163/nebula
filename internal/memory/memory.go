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
	FindSimilarPatterns(ctx context.Context, embedding []float32, topK int) ([]*models.Pattern, error)

	// Permission rules
	SavePermission(ctx context.Context, p *models.Permission) error
	FindPermission(ctx context.Context, cmdPattern string) (*models.Permission, error)

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

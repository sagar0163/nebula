package contextpkg

import (
	"context"
	"math"
	"sort"
)



// SearchResult represents a semantic search match
type SearchResult struct {
	ID    string
	Text  string
	Score float32
}

// MemoryVectorStore is a pure Go, in-memory vector store using cosine similarity
type MemoryVectorStore struct {
	entries []entry
}

type entry struct {
	id        string
	text      string
	embedding []float32
}

func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{entries: make([]entry, 0)}
}

func (m *MemoryVectorStore) Add(ctx context.Context, id string, text string, embedding []float32) error {
	m.entries = append(m.entries, entry{
		id:        id,
		text:      text,
		embedding: embedding,
	})
	return nil
}

func (m *MemoryVectorStore) Search(ctx context.Context, queryEmbedding []float32, topK int) ([]SearchResult, error) {
	if len(m.entries) == 0 {
		return nil, nil
	}

	results := make([]SearchResult, 0, len(m.entries))
	for _, e := range m.entries {
		score := cosineSimilarity(queryEmbedding, e.embedding)
		results = append(results, SearchResult{
			ID:    e.id,
			Text:  e.text,
			Score: score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score // Descending order
	})

	if topK > len(results) {
		topK = len(results)
	}

	return results[:topK], nil
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0.0
	}
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return float32(float64(dotProduct) / (math.Sqrt(float64(normA)) * math.Sqrt(float64(normB))))
}

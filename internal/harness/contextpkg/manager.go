package contextpkg

import (
	"context"
	"fmt"
	"strings"
	"sync"
	
	"github.com/sagar0163/nebula/internal/harness/shared"
)

// ContextManager manages hierarchical context for the harness
type ContextManager struct {
	mu           sync.RWMutex
	globalSummary string
	workingContext string
	relevantFiles map[string]*shared.FileReference
	vectorStore   VectorStore
	maxWorkingTokens int
}

// VectorStore interface for semantic search
type VectorStore interface {
	Add(ctx context.Context, file string, content string, metadata map[string]string) error
	Search(ctx context.Context, query string, k int) ([]shared.VectorMatch, error)
	Delete(ctx context.Context, file string) error
}

// NewContextManager creates a new context manager
func NewContextManager(maxWorkingTokens int, vectorStore VectorStore) *ContextManager {
	return &ContextManager{
		relevantFiles: make(map[string]*shared.FileReference),
		vectorStore:   vectorStore,
		maxWorkingTokens: maxWorkingTokens,
	}
}

// SetGlobalSummary sets the global repository summary
func (cm *ContextManager) SetGlobalSummary(summary string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.globalSummary = summary
}

// GetGlobalSummary returns the global summary
func (cm *ContextManager) GetGlobalSummary() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.globalSummary
}

// AddRelevantFile adds a file to the working context
func (cm *ContextManager) AddRelevantFile(file *shared.FileReference) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.relevantFiles[file.Path] = file
}

// GetRelevantFiles returns all relevant files
func (cm *ContextManager) GetRelevantFiles() []*shared.FileReference {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	files := make([]*shared.FileReference, 0, len(cm.relevantFiles))
	for _, f := range cm.relevantFiles {
		files = append(files, f)
	}
	return files
}

// SetWorkingContext sets the working context
func (cm *ContextManager) SetWorkingContext(ctx string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.workingContext = ctx
}

// GetWorkingContext returns the working context
func (cm *ContextManager) GetWorkingContext() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.workingContext
}

// BuildContext builds the full context for a phase
func (cm *ContextManager) BuildContext(phase shared.Phase, issue string, recentSteps []string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	var b strings.Builder
	
	// Global summary (always included)
	if cm.globalSummary != "" {
		b.WriteString("=== REPOSITORY CONTEXT ===\n")
		b.WriteString(cm.globalSummary)
		b.WriteString("\n\n")
	}
	
	// Relevant files
	if len(cm.relevantFiles) > 0 {
		b.WriteString("=== RELEVANT FILES ===\n")
		for _, f := range cm.relevantFiles {
			b.WriteString(fmt.Sprintf("%s: %s\n", f.Path, f.Summary))
			if f.RelevantLines[0] > 0 || f.RelevantLines[1] > 0 {
				b.WriteString(fmt.Sprintf("  Lines %d-%d\n", f.RelevantLines[0], f.RelevantLines[1]))
			}
			if len(f.Symbols) > 0 {
				b.WriteString(fmt.Sprintf("  Symbols: %s\n", strings.Join(f.Symbols, ", ")))
			}
		}
		b.WriteString("\n")
	}
	
	// Working context (recent history)
	if cm.workingContext != "" {
		b.WriteString("=== WORKING CONTEXT ===\n")
		b.WriteString(cm.workingContext)
		b.WriteString("\n\n")
	}
	
	// Recent steps
	if len(recentSteps) > 0 {
		b.WriteString("=== RECENT STEPS ===\n")
		for _, step := range recentSteps {
			b.WriteString(step + "\n")
		}
		b.WriteString("\n")
	}
	
	// Current issue
	b.WriteString("=== CURRENT ISSUE ===\n")
	b.WriteString(issue)
	b.WriteString("\n\n")
	
	// Phase-specific instructions
	b.WriteString(cm.phaseInstructions(phase))
	
	return b.String()
}

func (cm *ContextManager) phaseInstructions(phase shared.Phase) string {
	switch phase {
	case shared.PhasePlanning:
		return "=== INSTRUCTIONS ===\n" +
			"Analyze the issue and repository context.\n" +
			"Identify 3-5 candidate files that need changes.\n" +
			"Output a plan with file targets and test commands.\n" +
			"Return ONLY a JSON object with the plan.\n"
	case shared.PhaseExecution:
		return "=== INSTRUCTIONS ===\n" +
			"For each file in the plan:\n" +
			"1. read_file to understand current code\n" +
			"2. write_file with the fix\n" +
			"3. run_command (lint/typecheck) to verify\n" +
			"Output a unified diff per file.\n"
	case shared.PhaseVerification:
		return "=== INSTRUCTIONS ===\n" +
			"Apply the patch to the repository.\n" +
			"Run the failing test(s) first for fast feedback.\n" +
			"If tests pass, run the full test suite.\n" +
			"Return test results.\n"
	case shared.PhaseCritique:
		return "=== INSTRUCTIONS ===\n" +
			"Review the final patch for:\n" +
			"- Correctness: Does it actually fix the issue?\n" +
			"- Minimality: No unrelated changes\n" +
			"- Style: Follows project conventions\n" +
			"- Tests: No test modifications unless required\n" +
			"Return approval or requested changes.\n"
	default:
		return ""
	}
}

// UpdateWorkingContext appends to working context with token budget
func (cm *ContextManager) UpdateWorkingContext(newContent string, estimatedTokens int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	cm.workingContext += "\n" + newContent
	
	// Truncate if over budget (rough estimate: 4 chars = 1 token)
	maxChars := cm.maxWorkingTokens * 4
	if len(cm.workingContext) > maxChars {
		// Keep the most recent content
		cm.workingContext = cm.workingContext[len(cm.workingContext)-maxChars:]
	}
}

// AddVectorMatch stores a semantic match in vector store
func (cm *ContextManager) AddVectorMatch(ctx context.Context, match shared.VectorMatch) error {
	if cm.vectorStore == nil {
		return nil
	}
	return cm.vectorStore.Add(ctx, match.File, match.Content, match.Metadata)
}

// SearchVectorStore searches for relevant code
func (cm *ContextManager) SearchVectorStore(ctx context.Context, query string, k int) ([]shared.VectorMatch, error) {
	if cm.vectorStore == nil {
		return nil, nil
	}
	return cm.vectorStore.Search(ctx, query, k)
}
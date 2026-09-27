package index

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Indexer handles codebase symbol indexing using universal-ctags
type Indexer struct {
	workDir string
}

// NewIndexer creates a new ctags-based indexer
func NewIndexer(workDir string) *Indexer {
	return &Indexer{workDir: workDir}
}

// GoToDefinition uses ctags to find the definition of a symbol
func (i *Indexer) GoToDefinition(ctx context.Context, symbol string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("ctags -R -x . | grep -w '%s'", symbol))
	cmd.Dir = i.workDir
	
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	
	if err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return "No definitions found.", nil
		}
		if strings.Contains(err.Error(), "executable file not found") {
			return "", fmt.Errorf("ctags not installed on system")
		}
		return "", err
	}
	
	result := out.String()
	if result == "" {
		return "No definitions found.", nil
	}
	
	if len(result) > 4000 {
		result = result[:4000] + "\n...(truncated)"
	}
	
	return result, nil
}

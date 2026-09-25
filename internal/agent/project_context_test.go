package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProjectContext(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nebula-context-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Test Go
	goDir := filepath.Join(tempDir, "go-proj")
	os.Mkdir(goDir, 0755)
	os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module test"), 0644)
	
	ctx := DetectProjectContext(goDir)
	if ctx.Language != "Go" || ctx.BuildTool != "go build" {
		t.Fatalf("Expected Go project, got %+v", ctx)
	}

	// Test Node
	nodeDir := filepath.Join(tempDir, "node-proj")
	os.Mkdir(nodeDir, 0755)
	os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte("{}"), 0644)

	ctx = DetectProjectContext(nodeDir)
	if ctx.Language != "Node.js" || ctx.BuildTool != "npm" {
		t.Fatalf("Expected Node.js project, got %+v", ctx)
	}
	
	// Test Python
	pyDir := filepath.Join(tempDir, "py-proj")
	os.Mkdir(pyDir, 0755)
	os.WriteFile(filepath.Join(pyDir, "requirements.txt"), []byte(""), 0644)

	ctx = DetectProjectContext(pyDir)
	if ctx.Language != "Python" || ctx.BuildTool != "pip" {
		t.Fatalf("Expected Python project, got %+v", ctx)
	}
}

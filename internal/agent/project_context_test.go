package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestDetectProjectContextCacheTTL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nebula-ttl-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// First call — no marker file, should be empty
	ctx := DetectProjectContext(tmpDir)
	if ctx.Language != "" {
		t.Fatalf("expected empty context for bare dir, got %+v", ctx)
	}

	// Now add go.mod — without TTL expiry, cached empty result would persist
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)

	// Manually expire the cache entry
	contextCache.Store(tmpDir, contextEntry{
		ctx:       ctx,
		expiresAt: time.Now().Add(-1 * time.Second), // already expired
	})

	// Second call — cache expired, should re-detect Go project
	ctx = DetectProjectContext(tmpDir)
	if ctx.Language != "Go" {
		t.Fatalf("expected Go after TTL expiry, got %+v", ctx)
	}
}

func TestDetectProjectContextCacheHit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nebula-cachehit-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)

	// Warm the cache
	ctx1 := DetectProjectContext(tmpDir)
	if ctx1.Language != "Go" {
		t.Fatalf("expected Go, got %+v", ctx1)
	}

	// Remove go.mod — within TTL the cached result should still return Go
	os.Remove(filepath.Join(tmpDir, "go.mod"))
	ctx2 := DetectProjectContext(tmpDir)
	if ctx2.Language != "Go" {
		t.Fatalf("expected cached Go result within TTL, got %+v", ctx2)
	}
}

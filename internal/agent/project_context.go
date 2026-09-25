package agent

import (
	"os"
	"path/filepath"
	"sync"
)

type ProjectContext struct {
	Language  string
	BuildTool string
}

func (p ProjectContext) String() string {
	if p.Language == "" && p.BuildTool == "" {
		return ""
	}
	return "Project: " + p.Language + ", Build tool: " + p.BuildTool
}

var (
	contextCache sync.Map
)

func DetectProjectContext(dir string) ProjectContext {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return ProjectContext{}
		}
	}
	
	if val, ok := contextCache.Load(dir); ok {
		return val.(ProjectContext)
	}

	ctx := ProjectContext{}

	// Basic heuristics
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		ctx = ProjectContext{Language: "Go", BuildTool: "go build"}
	} else if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		ctx = ProjectContext{Language: "Node.js", BuildTool: "npm"}
	} else if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		ctx = ProjectContext{Language: "Python", BuildTool: "pip"}
	} else if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
		ctx = ProjectContext{Language: "Python", BuildTool: "poetry/pip"}
	} else if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		ctx = ProjectContext{Language: "Rust", BuildTool: "cargo"}
	} else if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
		ctx = ProjectContext{Language: "C/C++ or Make-based", BuildTool: "make"}
	} else if _, err := os.Stat(filepath.Join(dir, "pom.xml")); err == nil {
		ctx = ProjectContext{Language: "Java", BuildTool: "maven"}
	} else if _, err := os.Stat(filepath.Join(dir, "build.gradle")); err == nil {
		ctx = ProjectContext{Language: "Java/Kotlin", BuildTool: "gradle"}
	} else {
		// Try to find .go or .py or .js files loosely
		// Just default to unknown for now to keep it cheap
	}

	contextCache.Store(dir, ctx)
	return ctx
}

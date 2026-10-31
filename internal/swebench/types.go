// Package swebench provides SWE-bench dataset loading and evaluation integration.
package swebench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Instance represents a single SWE-bench task instance.
type Instance struct {
	InstanceID           string `json:"instance_id"`
	Repo                 string `json:"repo"`
	IssueID              int    `json:"issue_id"`
	BaseCommit           string `json:"base_commit"`
	ProblemStatement     string `json:"problem_statement"`
	Version              string `json:"version"`
	IssueURL             string `json:"issue_url"`
	PRURL                string `json:"pr_url"`
	Patch                string `json:"patch"`           // Gold patch (solution)
	TestPatch            string `json:"test_patch"`      // Test patch
	CreatedAt            string `json:"created_at"`
	HintsText            string `json:"hints_text"`
	FAILTOPASS           string `json:"FAIL_TO_PASS"`    // Fail-to-pass test cases
	PASSTOPASS           string `json:"PASS_TO_PASS"`    // Pass-to-pass test cases
	EnvironmentSetupCommit string `json:"environment_setup_commit"`
	Difficulty           string `json:"difficulty"`
}

// Prediction represents a model's prediction for an instance.
type Prediction struct {
	InstanceID string `json:"instance_id"`
	Model      string `json:"model"`
	Patch      string `json:"patch"`      // Model-generated patch
	Timestamp  string `json:"timestamp"`
}

// Result represents the evaluation result for a single instance.
type Result struct {
	InstanceID     string        `json:"instance_id"`
	Status         string        `json:"status"`          // "resolved", "failed", "error", "skipped"
	PatchApplied   bool          `json:"patch_applied"`
	TestsPassed    bool          `json:"tests_passed"`
	Error          string        `json:"error,omitempty"`
	Duration       time.Duration `json:"duration"`
	Model          string        `json:"model"`
	GeneratedPatch string        `json:"generated_patch,omitempty"`
	StartedAt      time.Time     `json:"started_at"`
}

// Report aggregates results across all instances.
type Report struct {
	Total         int       `json:"total"`
	Resolved      int       `json:"resolved"`
	Failed        int       `json:"failed"`
	Errors        int       `json:"errors"`
	Skipped       int       `json:"skipped"`
	ResolveRate   float64   `json:"resolve_rate"`
	Model         string    `json:"model"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	Results       []*Result `json:"results"`
}

// DatasetConfig holds configuration for loading a SWE-bench dataset.
type DatasetConfig struct {
	Name        string // "SWE-bench", "SWE-bench_Verified", "SWE-bench_Lite"
	Split       string // "test", "dev", "train"
	LocalPath   string // Local path to dataset (if already downloaded)
	HFToken     string // Hugging Face token for private datasets
	MaxInstances int   // Limit number of instances (0 = all)
}

// LoadDataset loads a SWE-bench dataset from Hugging Face or local path.
// If localPath is provided, loads from there. Otherwise downloads from HF.
func LoadDataset(cfg DatasetConfig) ([]*Instance, error) {
	if cfg.LocalPath != "" {
		return loadFromLocal(cfg.LocalPath, cfg.Split, cfg.MaxInstances)
	}
	return loadFromHuggingFace(cfg)
}

// loadFromLocal loads instances from a local JSONL file.
func loadFromLocal(path, split string, maxInstances int) ([]*Instance, error) {
	file := filepath.Join(path, split+".jsonl")
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var instances []*Instance
	dec := json.NewDecoder(f)
	for dec.More() {
		var inst Instance
		if err := dec.Decode(&inst); err != nil {
			return nil, err
		}
		instances = append(instances, &inst)
		if maxInstances > 0 && len(instances) >= maxInstances {
			break
		}
	}
	return instances, nil
}

// loadFromHuggingFace downloads and loads from Hugging Face datasets.
// Requires HF_TOKEN env var or cfg.HFToken for private datasets.
func loadFromHuggingFace(cfg DatasetConfig) ([]*Instance, error) {
	// This would use the HF datasets library or HTTP API
	// For now, return an error guiding the user to download locally first
	return nil, ErrDatasetNotLocal
}

// ErrDatasetNotLocal is returned when dataset must be downloaded first.
var ErrDatasetNotLocal = &DatasetError{"dataset not found locally; download from Hugging Face first"}

type DatasetError struct{ msg string }
func (e *DatasetError) Error() string { return e.msg }
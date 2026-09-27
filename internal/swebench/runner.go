// Package swebench provides SWE-bench dataset loading and evaluation integration.
package swebench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/llm/providers"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
	"github.com/sagar0163/nebula/internal/swebench/testrunner"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
	"os/exec"
)

// OfficialPrediction matches the SWE-bench harness prediction format.
type OfficialPrediction struct {
	InstanceID string `json:"instance_id"`
	ModelName  string `json:"model_name_or_path"`
	ModelPatch string `json:"model_patch"`
	GeneratedAt string `json:"generated_at"`
}

// RunnerConfig controls a SWE-bench evaluation run.
type RunnerConfig struct {
	Dataset       DatasetConfig
	Provider      string        // LLM provider to use (nvidia, groq, etc.)
	Model         string        // Specific model name
	Timeout       time.Duration // Per-instance timeout
	OutputDir     string        // Where to write predictions.jsonl and results
	Concurrency   int           // Parallel instances (default 1 for safety)
	SkipResolved  bool          // Skip instances already in predictions file
	DryRun        bool          // Don't actually run, just show what would run
}

// Runner executes SWE-bench instances through Nebula agent.
type Runner struct {
	cfg       RunnerConfig
	router    *llm.Router
	predPath  string
	results   []*Result
	startTime time.Time
}

// NewRunner creates a new SWE-bench runner.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(os.TempDir(), "nebula-swebench-"+time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, err
	}

	// Build router pinned to the requested provider
	router := buildSWEBenchRouter(cfg.Provider, cfg.Model)
	if router == nil {
		return nil, fmt.Errorf("no usable %s provider — check its key with: nebula key list", cfg.Provider)
	}

	predPath := filepath.Join(cfg.OutputDir, "predictions.jsonl")

	return &Runner{
		cfg:       cfg,
		router:    router,
		predPath:  predPath,
		startTime: time.Now(),
	}, nil
}

// Run executes the SWE-bench evaluation.


func (r *Runner) Run(ctx context.Context) (*Report, error) {
	instances, err := LoadDataset(r.cfg.Dataset)
	if err != nil {
		return nil, fmt.Errorf("load dataset: %w", err)
	}

	// Load existing predictions to resume
	existing := r.loadPredictions()
	
	r.results = make([]*Result, 0, len(instances))
	
	concurrency := r.cfg.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	// Create a worker pool
	workQueue := make(chan struct{ index int; inst Instance }, len(instances))
	resultsQueue := make(chan *Result, len(instances))
	
	// Enqueue work
	totalCount := 0
	for i, inst := range instances {
		// Check if already predicted
		if r.cfg.SkipResolved && existing[inst.InstanceID] != "" {
			continue
		}
		workQueue <- struct{ index int; inst Instance }{i, inst}
		totalCount++
	}
	close(workQueue)
	
	if totalCount == 0 {
		return r.finalize(), nil
	}
	
	var wg import_sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workQueue {
				select {
				case <-ctx.Done():
					return
				default:
				}
				
				fmt.Printf("\n[%d/%d] Starting %s\n", work.index+1, len(instances), work.inst.InstanceID)
				result := r.runInstance(ctx, work.inst)
				
				status := result.Status
				if result.Error != "" {
					status += " (" + result.Error + ")"
				}
				fmt.Printf("  → %s: %s (%v)\n", work.inst.InstanceID, status, result.Duration.Round(time.Second))
				
				resultsQueue <- result
			}
		}()
	}
	
	// Collect results in a separate goroutine
	go func() {
		wg.Wait()
		close(resultsQueue)
	}()
	
	for res := range resultsQueue {
		r.results = append(r.results, res)
		if err := r.writeOfficialPrediction(res); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write prediction: %v\n", err)
		}
	}
	
	return r.finalize(), nil
}

// runInstance runs a single SWE-bench instance through Nebula.
func (r *Runner) runInstance(ctx context.Context, inst *Instance) *Result {
	start := time.Now()
	result := &Result{
		InstanceID: inst.InstanceID,
		Model:      r.cfg.Model,
		StartedAt:  start,
	}

	// Create temp workspace
	workDir, err := os.MkdirTemp("", "swebench-"+strings.ReplaceAll(inst.InstanceID, "/", "-")+"-")
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("create work dir: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer os.RemoveAll(workDir)

	// Clone repo at base commit
	if err := r.cloneRepo(ctx, workDir, inst.Repo, inst.BaseCommit); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("clone repo: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Build the goal for Nebula
	goal := r.buildGoal(inst)

	// Create agent with isolated memory store
	dbPath := filepath.Join(workDir, "nebula.db")
	store, err := memory.New(dbPath)
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("create store: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer store.Close()

	harness := pty.NewHarness(256*1024, 512*1024)
	a := agent.New(harness, r.router, store, agent.Config{
		HistoryDepth:  10,
		FileInjection: true,
	})

	// Run with a context timeout
			runCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
			defer cancel()

			// Change to workDir for the agent to operate in the correct directory
					prevDir, err := os.Getwd()
					if err != nil {
						result.Status = "error"
						result.Error = fmt.Sprintf("getwd failed: %v", err)
						result.Duration = time.Since(start)
						return result
					}
					fmt.Printf("DEBUG: Changing to workDir: %s\n", workDir)
					if err := os.Chdir(workDir); err != nil {
						result.Status = "error"
						result.Error = fmt.Sprintf("chdir failed: %v", err)
						result.Duration = time.Since(start)
						return result
					}
					fmt.Printf("DEBUG: Goal being sent:\n%s\n", goal)

					// Use DoGoal to have Nebula work on the issue
					var sb strings.Builder
					err = a.DoGoal(runCtx, goal, agent.RunOptions{
						SkipPermissions: true,
						ApprovalFn: func(cmd string, risk safety.Risk) bool { return true },
					}, &sb)

					// Restore original directory
					os.Chdir(prevDir)

					if err != nil {
						fmt.Printf("DEBUG: Agent error: %v\n", err)
		result.Status = "error"
		result.Error = fmt.Sprintf("agent error: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Extract patch from output
	fmt.Printf("DEBUG: Extracting patch from workDir: %s\n", workDir)
	// Also check git status
	cmd := exec.CommandContext(ctx, "git", "status")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("DEBUG: Git status: %s\n", string(out))
	}
	// Also check git diff
	cmd = exec.CommandContext(ctx, "git", "diff", "--no-color")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("DEBUG: Git diff before extractPatch: %s\n", string(out))
	}
	
	patch := r.extractPatch(runCtx, workDir)
	fmt.Printf("DEBUG: Extracted patch: %q\n", patch)
	result.GeneratedPatch = patch

	if patch == "" {
		fmt.Printf("DEBUG: No patch generated\n")
		result.Status = "failed"
		result.Error = "no patch generated"
		result.Duration = time.Since(start)
		return result
	}

	// Apply patch and run tests using the harness
	if err := r.applyPatchAndTest(ctx, workDir, inst, patch); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("test failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	result.Status = "resolved"
	result.PatchApplied = true
	result.TestsPassed = true
	result.Duration = time.Since(start)
	return result
}

// buildGoal constructs the natural language goal for Nebula.
func (r *Runner) buildGoal(inst *Instance) string {
	var b strings.Builder
	b.WriteString("Fix the following GitHub issue in this repository.\n\n")
	b.WriteString("Repository: " + inst.Repo + "\n")
	b.WriteString("Issue: " + inst.ProblemStatement + "\n")
	if inst.HintsText != "" {
		b.WriteString("\nHints: " + inst.HintsText + "\n")
	}
	b.WriteString("\nYou MUST return ONLY a JSON object with this exact structure:\n")
	b.WriteString("{\n")
	b.WriteString("  \"steps\": [\n")
	b.WriteString("    {\"tool\": \"ShellTool\", \"input\": {\"command\": \"ls -la\"}},\n")
	b.WriteString("    {\"tool\": \"ShellTool\", \"input\": {\"command\": \"find . -name '*.py' -o -name '*.go' -o -name '*.js' | head -20\"}},\n")
	b.WriteString("    {\"tool\": \"ShellTool\", \"input\": {\"command\": \"cat path/to/buggy/file.py\"}},\n")
	b.WriteString("    {\"tool\": \"ShellTool\", \"input\": {\"command\": \"sed -i 's/BUGGY_CODE/FIXED_CODE/g' path/to/file.py\"}},\n")
	b.WriteString("    {\"tool\": \"DoneTool\", \"input\": {\"reason\": \"unified diff patch here\"}}\n")
	b.WriteString("  ]\n")
	b.WriteString("}\n\n")
	b.WriteString("Do NOT include any explanation, markdown, or text outside the JSON.\n")
	b.WriteString("Replace the example commands with actual commands for this repository.\n")
	b.WriteString("Use ShellTool to explore, read files, make edits, then use DoneTool with the unified diff as reason.\n")
	return b.String()
}

// extractPatch extracts a unified diff from the agent's output.
// Uses the new robust patch generation.
func (r *Runner) extractPatch(ctx context.Context, workDir string) string {
	fmt.Printf("DEBUG: extractPatch called for workDir: %s\n", workDir)
	patchResult, err := GeneratePatch(ctx, workDir)
	fmt.Printf("DEBUG: GeneratePatch returned err=%v, patchResult=%+v\n", err, patchResult)
	if err != nil {
		fmt.Printf("DEBUG: GeneratePatch error: %v\n", err)
		return ""
	}
	if !patchResult.Valid {
		fmt.Printf("DEBUG: Patch not valid: %s\n", patchResult.Error)
		return ""
	}
	fmt.Printf("DEBUG: Patch valid, length=%d\n", len(patchResult.Diff))
	return patchResult.Diff
}

// applyPatchAndTest applies the generated patch and runs the test suite with retry logic.
func (r *Runner) applyPatchAndTest(ctx context.Context, workDir string, inst *Instance, patch string) error {
	maxRetries := 3
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Write patch to file
		patchFile := filepath.Join(workDir, "fix.patch")
		if err := os.WriteFile(patchFile, []byte(patch), 0o644); err != nil {
			return err
		}

		// Apply patch - first try to reset the file to original state
		// Get list of files in patch
		files := extractModifiedFiles(patch)
		for _, f := range files {
			// Try to checkout the original file
			r.runCommand(ctx, workDir, "git", "checkout", "--", f)
		}

		// Apply patch
		if err := r.runCommand(ctx, workDir, "git", "apply", "fix.patch"); err != nil {
			if attempt == maxRetries-1 {
				return fmt.Errorf("git apply failed after %d attempts: %w", maxRetries, err)
			}
			continue
		}

		// Run tests using language-specific test runner
		testResult, err := r.runTests(ctx, workDir)
		if err != nil {
			if attempt == maxRetries-1 {
				return fmt.Errorf("test execution failed after %d attempts: %w", maxRetries, err)
			}
			// Try to get a new patch from the agent based on failure
			// For now, just retry with same patch
			continue
		}

		if testResult.Failed == 0 {
			return nil // All tests passed
		}

		// Tests failed - if we have retries left, we could ask agent for a new fix
		// For now, just retry with same patch
		if attempt == maxRetries-1 {
			return fmt.Errorf("tests still failing after %d attempts: %d failed", maxRetries, testResult.Failed)
		}
	}
	
	return fmt.Errorf("max retries exceeded")
}

// runTests detects the language and runs appropriate tests
func (r *Runner) runTests(ctx context.Context, workDir string) (*testrunner.TestResult, error) {
	// Try Go test runner first
	goRunner := &testrunner.GoTestRunner{}
	if goRunner.Detect(workDir) {
		return goRunner.RunTests(ctx, workDir, "")
	}
	
	// Fallback to generic test commands
	testCmds := [][]string{
		{"python", "-m", "pytest", "-x", "-v"},
		{"python", "-m", "pytest", "-x"},
		{"python", "setup.py", "test"},
		{"make", "test"},
		{"bash", "run_tests.sh"},
	}

	for _, cmd := range testCmds {
		if _, err := r.runCommandWithOutput(ctx, workDir, cmd[0], cmd[1:]...); err == nil {
			return &testrunner.TestResult{Passed: 1, Failed: 0}, nil
		}
		// Check if test command exists
		if _, err := os.Stat(filepath.Join(workDir, cmd[0])); err == nil {
			continue
		}
	}

	return &testrunner.TestResult{Failed: 1, FailedTests: []testrunner.FailedTest{{Name: "test detection", Error: "no test runner found"}}}, nil
}

func (r *Runner) runCommand(ctx context.Context, dir, name string, args ...string) error {
	_, err := r.runCommandWithOutput(ctx, dir, name, args...)
	return err
}

func (r *Runner) runCommandWithOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// cloneRepo clones the repository at the base commit.
func (r *Runner) cloneRepo(ctx context.Context, workDir, repo, commit string) error {
	// Check if repo is a local path (starts with ./ or ../ or /)
	if strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "../") || strings.HasPrefix(repo, "/") {
		// Copy local repo
		return r.copyLocalRepo(ctx, workDir, repo, commit)
	}

	// Otherwise treat as GitHub repo
	repoURL := "https://github.com/" + repo + ".git"
	if err := r.runCommand(ctx, workDir, "git", "clone", "--depth=1", "--branch="+commit, repoURL, "."); err != nil {
		// Try without branch flag (for commit hashes)
		if err := r.runCommand(ctx, workDir, "git", "clone", repoURL, "."); err != nil {
			return err
		}
		if err := r.runCommand(ctx, workDir, "git", "checkout", commit); err != nil {
			return err
		}
	}
	return nil
}

// copyLocalRepo copies a local repository to the work directory.
func (r *Runner) copyLocalRepo(ctx context.Context, workDir, localPath, commit string) error {
	// Convert to absolute path
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}

	// Copy the repo
	if err := r.runCommand(ctx, workDir, "cp", "-r", absPath+"/.", "."); err != nil {
		return err
	}

	// Checkout the specific commit
	return r.runCommand(ctx, workDir, "git", "checkout", commit)
}

// writeOfficialPrediction writes a prediction in the official SWE-bench format.
func (r *Runner) writeOfficialPrediction(result *Result) error {
	pred := OfficialPrediction{
		InstanceID: result.InstanceID,
		ModelName:  result.Model,
		ModelPatch: result.GeneratedPatch,
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
	f, err := os.OpenFile(r.predPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(pred)
}

// loadPredictions loads existing predictions from the JSONL file.
func (r *Runner) loadPredictions() map[string]string {
	existing := make(map[string]string)
	f, err := os.Open(r.predPath)
	if err != nil {
		return existing
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for dec.More() {
		var pred OfficialPrediction
		if err := dec.Decode(&pred); err != nil {
			continue
		}
		existing[pred.InstanceID] = pred.ModelPatch
	}
	return existing
}

// RunOfficialHarness runs the official SWE-bench harness on generated predictions.
// This requires Docker and the swebench Python package installed.
func RunOfficialHarness(datasetName, split, predictionsPath, runID string, maxWorkers int, timeout int, taskRepo string) error {
	args := []string{
		"-m", "swebench.harness.run_evaluation",
		"--dataset_name", datasetName,
		"--split", split,
		"--predictions_path", predictionsPath,
		"--max_workers", fmt.Sprintf("%d", maxWorkers),
		"--timeout", fmt.Sprintf("%d", timeout),
		"--run_id", runID,
	}

	if taskRepo != "" {
		args = append(args, "--task_repo", taskRepo)
	}

	cmd := exec.Command("python3", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// finalize creates the final report.
func (r *Runner) finalize() *Report {
	report := &Report{
		Model:       r.cfg.Model,
		StartedAt:   r.startTime,
		CompletedAt: time.Now(),
		Total:       len(r.results),
		Results:     r.results,
	}

	for _, res := range r.results {
		switch res.Status {
		case "resolved":
			report.Resolved++
		case "failed":
			report.Failed++
		case "error":
			report.Errors++
		case "skipped":
			report.Skipped++
		}
	}

	if report.Total > 0 {
		report.ResolveRate = float64(report.Resolved) / float64(report.Total)
	}
	return report
}

func buildSWEBenchRouter(provider, model string) *llm.Router {
	only := provider
	if only == "" {
		only = firstAvailableProvider()
	}
	if only == "" {
		return nil
	}
	return buildRouter(only, nil)
}

// firstAvailableProvider returns the first provider in registration order that
// has a key in the config file, the environment, or the keyring.
func firstAvailableProvider() string {
	for _, p := range knownProviders {
		if providerHasKey(p) {
			return p
		}
	}
	if viper.GetString("llm.ollama.base_url") != "" {
		return "ollama"
	}
	return ""
}

// providerHasKey checks if a provider has a key configured.
func providerHasKey(provider string) bool {
	if viper.GetString("llm."+provider+".api_key") != "" {
		return true
	}
	if os.Getenv(providerKeyEnvVar(provider)) != "" {
		return true
	}
	for slot := 1; slot <= 9; slot++ {
		if v, _ := keyring.Get("nebula", slotName(provider, slot)); v != "" {
			return true
		}
	}
	return false
}

// providerKeyEnvVar mirrors the environment variable loadKeys reads for each provider.
func providerKeyEnvVar(provider string) string {
	return "NEBULA_" + strings.ToUpper(provider) + "_KEY"
}

// slotName returns the keyring slot name for a provider.
func slotName(provider string, slot int) string {
	base := provider + "_api_key"
	if slot == 1 {
		return base
	}
	return base + "_" + strconv.Itoa(slot)
}

var knownProviders = []string{"groq", "gemini", "mistral", "nvidia"}

// buildRouter registers every provider that has a key (or an ollama base URL)
// for the workloads it serves. Keys are always read through loadKeys, which
// never writes them to disk.
//
// When only is non-empty, providers whose Name() differs are left
// unregistered, which is how `nebula eval` pins a run to a single backend.
func buildRouter(only string, wrap func(llm.Provider) llm.Provider) *llm.Router {
	router := llm.NewRouter()

	register := func(p llm.Provider, workloads ...llm.Workload) {
		if p == nil {
			return
		}
		if only != "" && p.Name() != only {
			return
		}
		if wrap != nil {
			p = wrap(p)
		}
		for _, w := range workloads {
			router.Register(w, p)
		}
	}

	for _, k := range loadKeys("groq_api_key", viper.GetString("llm.groq.api_key"), "NEBULA_GROQ_KEY") {
		register(providers.NewGroq(providers.GroqConfig{
			APIKey:        k,
			ModelDiagnose: viper.GetString("llm.groq.model_diagnose"),
			ModelHeal:     viper.GetString("llm.groq.model_heal"),
			ModelLearn:    viper.GetString("llm.groq.model_learn"),
		}), llm.WorkloadDiagnose, llm.WorkloadHeal, llm.WorkloadLearn)
	}

	for _, k := range loadKeys("gemini_api_key", viper.GetString("llm.gemini.api_key"), "NEBULA_GEMINI_KEY") {
		register(providers.NewGemini(providers.GeminiConfig{
			APIKey:        k,
			ModelDiagnose: viper.GetString("llm.gemini.model_diagnose"),
			ModelHeal:     viper.GetString("llm.gemini.model_heal"),
			ModelLearn:    viper.GetString("llm.gemini.model_learn"),
			ModelEmbed:    viper.GetString("llm.gemini.model_embed"),
		}), llm.WorkloadDiagnose, llm.WorkloadHeal, llm.WorkloadLearn, llm.WorkloadEmbed)
	}

	if base := viper.GetString("llm.ollama.base_url"); base != "" {
		p, err := providers.NewOllama(providers.OllamaConfig{
			BaseURL:       base,
			ModelDiagnose: viper.GetString("llm.ollama.model_diagnose"),
			ModelHeal:     viper.GetString("llm.ollama.model_heal"),
			ModelLearn:    viper.GetString("llm.ollama.model_learn"),
			ModelEmbed:    viper.GetString("llm.ollama.model_embed"),
		})
		if err == nil {
			register(p, llm.WorkloadDiagnose, llm.WorkloadHeal, llm.WorkloadLearn, llm.WorkloadEmbed)
		}
	}

	for _, k := range loadKeys("mistral_api_key", viper.GetString("llm.mistral.api_key"), "NEBULA_MISTRAL_KEY") {
		register(providers.NewMistral(providers.MistralConfig{
			APIKey:        k,
			ModelDiagnose: viper.GetString("llm.mistral.model_diagnose"),
			ModelHeal:     viper.GetString("llm.mistral.model_heal"),
			ModelLearn:    viper.GetString("llm.mistral.model_learn"),
			ModelEmbed:    viper.GetString("llm.mistral.model_embed"),
		}), llm.WorkloadDiagnose, llm.WorkloadHeal, llm.WorkloadLearn, llm.WorkloadEmbed)
	}

	for _, k := range loadKeys("nvidia_api_key", viper.GetString("llm.nvidia.api_key"), "NEBULA_NVIDIA_KEY") {
		register(providers.NewNvidia(providers.NvidiaConfig{
			APIKey:        k,
			BaseURL:       viper.GetString("llm.nvidia.base_url"),
			ModelDiagnose: viper.GetString("llm.nvidia.model_diagnose"),
			ModelHeal:     viper.GetString("llm.nvidia.model_heal"),
			ModelLearn:    viper.GetString("llm.nvidia.model_learn"),
			ModelEmbed:    viper.GetString("llm.nvidia.model_embed"),
		}), llm.WorkloadDiagnose, llm.WorkloadHeal, llm.WorkloadLearn, llm.WorkloadEmbed)
	}

	return router
}

// loadKeys returns all API keys for a provider: config-file value first,
// then keyring entries named baseKey, baseKey_2 … baseKey_9.
func loadKeys(baseKey, configVal, envVar string) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	add(configVal)
	add(os.Getenv(envVar))
	var keyringErr error
	k, err := keyring.Get("nebula", baseKey)
	if err != nil && err != keyring.ErrNotFound {
		keyringErr = err
	}
	if k != "" {
		add(k)
	}
	for i := 2; i <= 9; i++ {
		k, err := keyring.Get("nebula", baseKey+"_"+strconv.Itoa(i))
		if err != nil && err != keyring.ErrNotFound {
			keyringErr = err
		}
		if k != "" {
			add(k)
		}
	}

	if len(keys) == 0 && keyringErr != nil {
		fmt.Fprintf(os.Stderr, "nebula: keyring unavailable (%v) — set %s or run nebula key add\n", keyringErr, envVar)
	}

	return keys
}

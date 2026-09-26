// Package eval runs Nebula against curated broken-project fixtures and reports
// how often it fixes them, what a fix costs, and how long it takes.
//
// Each fixture is a self-contained directory:
//
//	setup.sh      creates the broken state (run with `bash setup.sh`)
//	run_cmd       the command Nebula has to fix
//	verify_cmd    the command that must exit 0 after the fix
//	description   one-line human description of the failure scenario
//
// A fixture is never mutated: every case is copied into its own temporary
// directory, and the agent is invoked with that directory as the process
// working directory.
package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/shlex"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/safety"
)

// exitToolMissing is the exit code a fixture's setup.sh uses to report that the
// toolchain it needs is not installed. Such a case is skipped rather than
// counted as a fix failure, so a missing toolchain never looks like a
// regression.
const exitToolMissing = 127

// Case is a single eval fixture loaded from disk.
type Case struct {
	// Name is the fixture directory name, e.g. "go-undefined-var".
	Name string
	// Dir is the absolute path of the fixture directory.
	Dir string
	// Description is the one-line failure scenario, from the description file.
	Description string
	// RunCmd is the command handed to the agent.
	RunCmd string
	// VerifyCmd must exit 0 for the case to count as fixed.
	VerifyCmd string
}

// Status values reported per case.
const (
	StatusPass  = "PASS"
	StatusFail  = "FAIL"
	StatusSkip  = "SKIP"
	StatusError = "ERROR"
)

// AgentRunner is the slice of *agent.Agent the eval harness needs. Keeping it
// an interface lets the runner be tested without a live LLM provider.
type AgentRunner interface {
	Run(ctx context.Context, args []string, opts agent.RunOptions) (*agent.RunResult, error)
}

// Resetter is an optional interface an AgentRunner may implement to clear
// per-run accumulated state between eval cases.
type Resetter interface {
	Reset()
}

// RunnerFactory creates a fresh AgentRunner for each eval case. When set on
// Config it takes precedence over Runner, giving each case an isolated agent
// with its own memory store so learned patterns from one case cannot poison
// the next.
type RunnerFactory func() (AgentRunner, error)

// Config controls an eval run.
type Config struct {
	// RunnerFactory creates a fresh AgentRunner per case; takes precedence over
	// Runner when set. Preferred: it prevents learned patterns from one case
	// poisoning the next via the shared memory store.
	RunnerFactory RunnerFactory
	// Runner is used when RunnerFactory is nil. All cases share it, which
	// means learned patterns accumulate across cases.
	Runner AgentRunner
	// Meter records token usage per case. Optional; without it the report
	// carries token counts of zero.
	Meter *Meter
	// Provider and Model label the report and seed per-case token cost.
	Provider string
	Model    string
	// Timeout bounds the agent phase of a single case. Zero means no bound.
	Timeout time.Duration
	// VerifyTimeout bounds the verify_cmd of a single case. Defaults to
	// defaultVerifyTimeout.
	VerifyTimeout time.Duration
	// KeepTemp leaves each case's working directory on disk for inspection.
	KeepTemp bool
	// OnResult, when set, is called as each case finishes so callers can
	// stream progress instead of waiting for the whole run.
	OnResult func(*Result)
}

// defaultVerifyTimeout bounds verify_cmd, which is a plain build/test command
// and should never need anywhere near this long.
const defaultVerifyTimeout = 3 * time.Minute

// Result is the outcome of a single case.
type Result struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Passed      bool   `json:"passed"`
	Skipped     bool   `json:"skipped"`

	// Err is set when the case could not be evaluated at all (copy, setup or
	// verify failure). AgentError is set when the agent itself failed but the
	// case was still verifiable.
	Err        string `json:"error,omitempty"`
	AgentError string `json:"agent_error,omitempty"`

	// FixCount is how many healing turns the agent needed to reach a green
	// verify_cmd.
	FixCount int `json:"fix_count"`
	// ExitCode is the run_cmd exit code the agent saw.
	ExitCode int `json:"exit_code"`

	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	// Calls is how many LLM completions the agent made on this case.
	Calls int `json:"llm_calls"`
	// CostUSD is the metered cost at provider list rates. CostKnown is false
	// when a model in Models had no rate entry, in which case TotalTokens is
	// the only cost signal available.
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`

	Provider string `json:"provider,omitempty"`
	// Model is the model that handled the most calls; Models lists every model
	// the case touched, since diagnose and heal can differ.
	Model  string   `json:"model,omitempty"`
	Models []string `json:"models,omitempty"`

	// AgentDurationMS is the wall time spent inside agent.Run — the time to
	// fix. DurationMS covers the whole case including setup and verify.
	AgentDurationMS int64 `json:"agent_duration_ms"`
	DurationMS      int64 `json:"duration_ms"`

	WorkDir      string `json:"work_dir,omitempty"`
	VerifyOutput string `json:"verify_output,omitempty"`
}

// AgentDuration returns the time the agent spent on this case.
func (r *Result) AgentDuration() time.Duration {
	return time.Duration(r.AgentDurationMS) * time.Millisecond
}

// Report is the aggregate of a full eval run.
type Report struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Models   []string `json:"models"`

	Total     int `json:"total"`
	Attempted int `json:"attempted"`
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	// Errors counts cases that could not be evaluated at all — a broken
	// fixture, an unreadable copy, a setup script that failed. They are
	// excluded from the pass rate, because a suite that cannot run is not
	// evidence that the agent got worse, but they are not silently dropped
	// either: Errors is non-zero and the command exits non-zero.
	Errors int `json:"errors"`

	// PassRate is Passed/Attempted in [0,1]. It is 0 when nothing was attempted.
	PassRate float64 `json:"pass_rate"`

	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	Calls        int `json:"llm_calls"`

	// CostUSD is the summed cost of every case that had a known rate.
	// CostKnown is false when at least one case had no rate entry, so the sum
	// is a lower bound and TotalTokens is reported alongside it.
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`

	AvgFixTimeMS    int64   `json:"avg_fix_time_ms"`
	AvgCostPerFix   float64 `json:"avg_cost_per_fix"`
	CostPerFixKnown bool    `json:"avg_cost_per_fix_known"`

	DurationMS int64     `json:"duration_ms"`
	Results    []*Result `json:"results"`
}

// LoadCases reads every fixture directory under root. A directory is a fixture
// when it holds setup.sh, run_cmd and verify_cmd; a directory missing any of
// them is an error rather than a silent skip, so a typo in a fixture name never
// quietly shrinks the suite.
func LoadCases(root string) ([]Case, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read eval dir %s: %w", root, err)
	}

	var cases []Case
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "setup.sh")); err != nil {
			continue // not a fixture directory
		}

		c := Case{Name: e.Name(), Dir: dir}
		var missing []string
		for _, f := range []struct {
			name string
			dst  *string
		}{
			{"run_cmd", &c.RunCmd},
			{"verify_cmd", &c.VerifyCmd},
		} {
			v, err := readTrimmed(filepath.Join(dir, f.name))
			if err != nil {
				missing = append(missing, f.name)
				continue
			}
			*f.dst = v
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("eval %s: missing or unreadable %s", c.Name, strings.Join(missing, ", "))
		}

		// description is optional: it is documentation, not behaviour.
		c.Description, _ = readTrimmed(filepath.Join(dir, "description"))
		cases = append(cases, c)
	}

	if len(cases) == 0 {
		return nil, fmt.Errorf("no eval fixtures found in %s", root)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

// Filter returns the cases whose name contains substr, matched
// case-insensitively. An empty substr matches everything.
func Filter(cases []Case, substr string) []Case {
	if substr == "" {
		return cases
	}
	needle := strings.ToLower(substr)
	var out []Case
	for _, c := range cases {
		if strings.Contains(strings.ToLower(c.Name), needle) {
			out = append(out, c)
		}
	}
	return out
}

// RunAll runs every case in order and aggregates the results into a Report.
func RunAll(ctx context.Context, cases []Case, cfg Config) *Report {
	if cfg.Runner == nil && cfg.RunnerFactory == nil {
		return &Report{Provider: cfg.Provider, Model: cfg.Model, Total: len(cases), CostKnown: false}
	}

	report := &Report{Provider: cfg.Provider, Model: cfg.Model}
	seen := map[string]bool{}
	start := time.Now()

	for _, c := range cases {
		if err := ctx.Err(); err != nil {
			// Abort the suite but keep what already finished.
			report.DurationMS = time.Since(start).Milliseconds()
			report.finalize()
			return report
		}

		res := RunEval(ctx, c, cfg)
		report.Results = append(report.Results, res)

		for _, m := range res.Models {
			if !seen[m] {
				seen[m] = true
				report.Models = append(report.Models, m)
			}
		}
		sort.Strings(report.Models)

		if cfg.OnResult != nil {
			cfg.OnResult(res)
		}
	}

	report.DurationMS = time.Since(start).Milliseconds()
	report.finalize()
	return report
}

// RunEval runs a single case end to end and never returns an error: a case that
// cannot be evaluated is reported as an ERROR result so one bad fixture does
// not take down the suite.
func RunEval(ctx context.Context, c Case, cfg Config) *Result {
	res := &Result{
		Name:        c.Name,
		Description: c.Description,
		Status:      StatusError,
		Provider:    cfg.Provider,
		Model:       cfg.Model,
	}
	caseStart := time.Now()
	defer func() { res.DurationMS = time.Since(caseStart).Milliseconds() }()

	// Resolve the runner: prefer a fresh instance from the factory so each
	// case starts with a clean memory store and zero doom-loop state.
	runner := cfg.Runner
	if cfg.RunnerFactory != nil {
		var ferr error
		runner, ferr = cfg.RunnerFactory()
		if ferr != nil {
			res.Err = fmt.Sprintf("create runner: %v", ferr)
			return res
		}
	}
	if runner == nil {
		res.Err = "no agent runner configured"
		return res
	}

	// 1. Isolate: copy the fixture into a temp dir so the repo is never
	//    mutated and every case starts from identical bytes.
	work, err := os.MkdirTemp("", "nebula-eval-"+c.Name+"-")
	if err != nil {
		res.Err = fmt.Sprintf("create work dir: %v", err)
		return res
	}
	if err := copyTree(c.Dir, work); err != nil {
		os.RemoveAll(work)
		res.Err = fmt.Sprintf("copy fixture: %v", err)
		return res
	}
	if cfg.KeepTemp {
		res.WorkDir = work
		fmt.Fprintf(os.Stderr, "eval: %s working dir kept at %s\n", c.Name, work)
	} else {
		defer os.RemoveAll(work)
	}

	caseCtx := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		caseCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	// markTimeout records an exhausted budget as a fix failure rather than an
	// unevaluable case: the agent had its chance and did not fix it in time,
	// and scoring it as ERROR would quietly drop it from the pass rate.
	markTimeout := func() bool {
		if caseCtx.Err() == nil {
			return false
		}
		res.Status = StatusFail
		res.AgentError = fmt.Sprintf("timed out after %s: %v", cfg.Timeout, caseCtx.Err())
		return true
	}

	// 2. Materialise the broken state.
	setupOut, setupCode, err := runInDir(caseCtx, work, []string{"bash", "setup.sh"}, 2*time.Minute)
	if err != nil {
		if markTimeout() {
			return res
		}
		res.Err = fmt.Sprintf("setup.sh: %v", err)
		return res
	}
	if setupCode == exitToolMissing {
		res.Skipped = true
		res.Status = StatusSkip
		res.Err = "toolchain unavailable: " + tailOutput(setupOut, 200)
		return res
	}
	if setupCode != 0 {
		res.Err = fmt.Sprintf("setup.sh exited %d: %s", setupCode, tailOutput(setupOut, 200))
		return res
	}

	runArgs, err := splitCommand(c.RunCmd)
	if err != nil {
		res.Err = fmt.Sprintf("parse run_cmd: %v", err)
		return res
	}
	verifyArgs, err := splitCommand(c.VerifyCmd)
	if err != nil {
		res.Err = fmt.Sprintf("parse verify_cmd: %v", err)
		return res
	}

	// 3. Pre-flight: a fixture that already passes proves nothing, so refuse
	//    to spend LLM calls on it.
	_, preCode, err := runInDir(caseCtx, work, runArgs, cfg.Timeout)
	if err != nil {
		if markTimeout() {
			return res
		}
		res.Err = fmt.Sprintf("run_cmd pre-flight: %v", err)
		return res
	}
	if preCode == 0 {
		res.Skipped = true
		res.Status = StatusSkip
		res.Err = "fixture is not broken: run_cmd exited 0 before the agent ran"
		return res
	}

	// 4. Let the agent try to fix it, with the temp dir as cwd so both the
	//    failing command and the agent's fixes land on the fixture copy.
	before := cfg.Meter.Snapshot()

	// The harness shares one agent across cases, so state it accumulated on an
	// earlier fixture — doom-loop fingerprints above all — would carry over and
	// short-circuit a later case before it makes a single LLM call.
	// Reset accumulated doom-loop state when reusing a shared runner (no factory).
	if cfg.RunnerFactory == nil {
		if r, ok := runner.(Resetter); ok {
			r.Reset()
		}
	}

	opts := agent.RunOptions{
		SkipPermissions: true,
		// The harness is non-interactive: every suggestion is approved, since
		// an eval must not block waiting for a human that is not there.
		ApprovalFn: func(string, safety.Risk) bool { return true },
	}

	var runRes *agent.RunResult
	var runErr error
	agentStart := time.Now()
	cdErr := withDir(work, func() error {
		runRes, runErr = runner.Run(caseCtx, runArgs, opts)
		return nil
	})
	res.AgentDurationMS = time.Since(agentStart).Milliseconds()

	after := cfg.Meter.Snapshot()
	used := DiffUsage(before, after)
	res.InputTokens = used.Input
	res.OutputTokens = used.Output
	res.TotalTokens = res.InputTokens + res.OutputTokens
	res.Calls = used.Calls
	res.Models = used.ModelNames()

	if runRes != nil {
		res.ExitCode = runRes.ExitCode
		res.FixCount = runRes.DoomLoopCount
		if runRes.Healed {
			// A healed command that the agent verified itself counts as a fix
			// turn even when it never re-tripped the doom-loop counter.
			if res.FixCount == 0 {
				res.FixCount = 1
			}
		}
	}
	if runErr != nil {
		res.AgentError = runErr.Error()
	}
	if cdErr != nil {
		res.Err = fmt.Sprintf("enter work dir: %v", cdErr)
		return res
	}
	if markTimeout() {
		return res
	}

	if res.Model == "" {
		res.Model = used.DominantModel()
	}
	// Cost is summed per model: a run spans the diagnose and heal tiers, which
	// are billed at different rates.
	res.CostUSD, res.CostKnown = used.Cost()

	// 5. Verify: the fixture's own definition of fixed, not the agent's word
	//    for it.
	verifyTimeout := cfg.VerifyTimeout
	if verifyTimeout <= 0 {
		verifyTimeout = defaultVerifyTimeout
	}
	verifyOut, verifyCode, err := runInDir(caseCtx, work, verifyArgs, verifyTimeout)
	if err != nil {
		res.Err = fmt.Sprintf("verify_cmd: %v", err)
		return res
	}
	if verifyCode == 0 {
		res.Passed = true
		res.Status = StatusPass
		return res
	}

	res.Status = StatusFail
	res.VerifyOutput = tailOutput(verifyOut, 400)
	return res
}

// finalize derives the aggregate numbers from the per-case results.
func (r *Report) finalize() {
	r.Total = len(r.Results)
	r.CostKnown = true

	var fixTimeMS int64
	for _, res := range r.Results {
		switch res.Status {
		case StatusSkip:
			r.Skipped++
			continue
		case StatusError:
			// The case never got as far as an attempt, so it is excluded from
			// the pass rate instead of being scored as a miss.
			r.Errors++
			continue
		}
		r.Attempted++
		if res.Passed {
			r.Passed++
		} else {
			r.Failed++
		}
		r.InputTokens += res.InputTokens
		r.OutputTokens += res.OutputTokens
		r.TotalTokens += res.TotalTokens
		r.Calls += res.Calls
		r.CostUSD += res.CostUSD
		if !res.CostKnown {
			r.CostKnown = false
		}
		fixTimeMS += res.AgentDurationMS
	}

	if r.Attempted > 0 {
		r.PassRate = float64(r.Passed) / float64(r.Attempted)
		r.AvgFixTimeMS = fixTimeMS / int64(r.Attempted)
	}

	// Cost per fix averages over the cases that were actually fixed, and is
	// only meaningful when every one of them had a known rate.
	costed := 0
	var costedSum float64
	allCosted := true
	for _, res := range r.Results {
		if !res.Passed {
			continue
		}
		if !res.CostKnown {
			allCosted = false
			continue
		}
		costed++
		costedSum += res.CostUSD
	}
	if costed > 0 && allCosted {
		r.AvgCostPerFix = costedSum / float64(costed)
		r.CostPerFixKnown = true
	} else {
		r.CostPerFixKnown = false
	}
}

// readTrimmed reads a small text file and strips surrounding whitespace.
func readTrimmed(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s is empty", filepath.Base(path))
	}
	return v, nil
}

// splitCommand splits a fixture command line into argv, honouring quotes.
func splitCommand(cmd string) ([]string, error) {
	args, err := shlex.Split(cmd)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return args, nil
}

// withDir runs fn with dir as the process working directory and always restores
// the previous directory afterwards — the agent executes commands in the
// process cwd, so this is how a fixture gets isolated.
func withDir(dir string, fn func() error) error {
	prev, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working dir: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer func() {
		if rerr := os.Chdir(prev); rerr != nil {
			fmt.Fprintf(os.Stderr, "eval: restore working dir: %v\n", rerr)
		}
	}()
	return fn()
}

// runInDir executes argv in dir with a plain pipe (no PTY) and returns its
// combined output and exit code. A non-zero exit is not an error here; only a
// failure to start the command is.
func runInDir(ctx context.Context, dir string, argv []string, timeout time.Duration) (string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return buf.String(), -1, fmt.Errorf("%s: %w", argv[0], ctxErr)
		}
		// A non-zero exit is the normal case here (that is the whole point of
		// run_cmd and of a failing verify_cmd), so it is reported as a code
		// rather than an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return buf.String(), exitErr.ExitCode(), nil
		}
		return buf.String(), -1, fmt.Errorf("%s: %w", argv[0], err)
	}
	return buf.String(), 0, nil
}

// copyTree copies src into dst recursively, preserving the executable bit so a
// fixture script can be run directly.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // skip symlinks, sockets, devices
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// tailOutput returns at most maxLen trailing characters of s, which is enough
// to see the compiler or test error without dumping a whole build log into the
// report.
func tailOutput(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return "..." + s[len(s)-maxLen:]
}

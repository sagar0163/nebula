package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/llm"
)

// fakeRunner stands in for the agent. fix runs with the work dir as the process
// working directory, exactly like a real fix would, and the metered traffic is
// recorded during Run — before the run it would be excluded from the case.
type fakeRunner struct {
	t       *testing.T
	fix     func(t *testing.T)
	meter   *Meter
	model   string
	in, out int
	runErr  error
	lastArg []string
}

func (f *fakeRunner) Run(_ context.Context, args []string, _ agent.RunOptions) (*agent.RunResult, error) {
	f.lastArg = args
	if f.meter != nil {
		f.meter.record(f.model, f.in, f.out)
	}
	if f.fix != nil {
		f.fix(f.t)
	}
	return &agent.RunResult{Command: strings.Join(args, " "), ExitCode: 1}, f.runErr
}

// writeFixture creates a fixture under root and returns its path.
func writeFixture(t *testing.T, root, name, setup, runCmd, verifyCmd string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"setup.sh":   setup,
		"run_cmd":    runCmd,
		"verify_cmd": verifyCmd,
	}
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadCasesReadsFixtureFiles(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "b-case", "true\n", "false\n", "true\n")
	writeFixture(t, root, "a-case", "true\n", "false\n", "true\n")
	// A directory without setup.sh is not a fixture and must be ignored.
	if err := os.MkdirAll(filepath.Join(root, "not-a-fixture"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases, err := LoadCases(root)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	if cases[0].Name != "a-case" || cases[1].Name != "b-case" {
		t.Fatalf("cases not sorted: %s, %s", cases[0].Name, cases[1].Name)
	}
	if cases[0].RunCmd != "false" || cases[0].VerifyCmd != "true" {
		t.Fatalf("commands not trimmed: %q %q", cases[0].RunCmd, cases[0].VerifyCmd)
	}
}

func TestLoadCasesRejectsIncompleteFixture(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "half-written")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "setup.sh"), []byte("true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// verify_cmd is missing: the suite must fail loudly rather than shrink.
	if _, err := LoadCases(root); err == nil {
		t.Fatal("expected an error for a fixture missing verify_cmd")
	}
}

func TestFilterMatchesCaseInsensitiveSubstring(t *testing.T) {
	cases := []Case{{Name: "go-missing-import"}, {Name: "python-syntax-error"}}
	got := Filter(cases, "GO-")
	if len(got) != 1 || got[0].Name != "go-missing-import" {
		t.Fatalf("Filter matched %v", got)
	}
	if len(Filter(cases, "")) != 2 {
		t.Fatal("empty filter should match everything")
	}
	if len(Filter(cases, "nope")) != 0 {
		t.Fatal("unmatched filter should return nothing")
	}
}

// The core guarantee: the fixture is never mutated, the fix lands on the copy,
// and verify_cmd decides the verdict.
func TestRunEvalFixesIsolatedCopy(t *testing.T) {
	setup := "printf 'broken\\n' > f.txt\n"
	verify := "grep -q fixed f.txt\n"
	dir := writeFixture(t, t.TempDir(), "case", setup, "sh check.sh\n", verify)

	meter := NewMeter()
	runner := &fakeRunner{t: t, meter: meter, model: "gemini-2.5-pro", in: 400, out: 100, fix: func(t *testing.T) {
		if err := os.WriteFile("f.txt", []byte("fixed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}}

	res := RunEval(context.Background(), Case{
		Name: "case", Dir: dir, RunCmd: "sh check.sh", VerifyCmd: verify,
	}, Config{Runner: runner, Meter: meter, Provider: "gemini", Model: "gemini-2.5-pro"})

	if res.Status != StatusPass || !res.Passed {
		t.Fatalf("status=%s passed=%v err=%q verify=%q", res.Status, res.Passed, res.Err, res.VerifyOutput)
	}
	// The fixture on disk must be untouched: no fixed file, no leftover state.
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !os.IsNotExist(err) {
		t.Fatal("run created files in the fixture directory")
	}
	if got := runner.lastArg; len(got) != 2 || got[0] != "sh" || got[1] != "check.sh" {
		t.Fatalf("agent received %v, want [sh check.sh]", got)
	}
	if res.InputTokens != 400 || res.OutputTokens != 100 || res.TotalTokens != 500 {
		t.Fatalf("token accounting wrong: %+v", res)
	}
	if !res.CostKnown || res.CostUSD <= 0 {
		t.Fatalf("expected a known cost, got %v (known=%v)", res.CostUSD, res.CostKnown)
	}
}

func TestRunEvalFailsWhenVerifyStillFails(t *testing.T) {
	noisyVerify := `sh -c "grep -q fixed f.txt || { echo still-broken; exit 1; }"`
	dir := writeFixture(t, t.TempDir(), "case", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", noisyVerify+"\n")

	res := RunEval(context.Background(), Case{
		Name: "case", Dir: dir, RunCmd: "sh check.sh", VerifyCmd: noisyVerify,
	}, Config{Runner: &fakeRunner{t: t}, Meter: NewMeter(), Provider: "gemini", Model: "gemini-2.5-pro"})

	if res.Status != StatusFail || res.Passed {
		t.Fatalf("status=%s passed=%v", res.Status, res.Passed)
	}
	if !strings.Contains(res.VerifyOutput, "still-broken") {
		t.Fatalf("a failing verify should report its output for debugging, got %q", res.VerifyOutput)
	}
}

// A fixture whose toolchain is missing is skipped, so a missing go toolchain
// never registers as a regression.
func TestRunEvalSkipsMissingToolchain(t *testing.T) {
	dir := writeFixture(t, t.TempDir(), "case", "echo 'go toolchain not available' >&2\nexit 127\n", "go build ./...\n", "go build ./...\n")

	res := RunEval(context.Background(), Case{
		Name: "case", Dir: dir, RunCmd: "go build ./...", VerifyCmd: "go build ./...",
	}, Config{Runner: &fakeRunner{t: t}, Meter: NewMeter()})

	if res.Status != StatusSkip || !res.Skipped {
		t.Fatalf("status=%s skipped=%v err=%q", res.Status, res.Skipped, res.Err)
	}
	if !strings.Contains(res.Err, "toolchain unavailable") {
		t.Fatalf("unhelpful skip reason: %q", res.Err)
	}
}

// A fixture that already passes proves nothing, so the agent is never called.
func TestRunEvalSkipsFixtureThatIsNotBroken(t *testing.T) {
	dir := writeFixture(t, t.TempDir(), "case", "true\n", "true\n", "true\n")
	runner := &fakeRunner{t: t}

	res := RunEval(context.Background(), Case{
		Name: "case", Dir: dir, RunCmd: "true", VerifyCmd: "true",
	}, Config{Runner: runner, Meter: NewMeter()})

	if res.Status != StatusSkip || !res.Skipped {
		t.Fatalf("status=%s skipped=%v err=%q", res.Status, res.Skipped, res.Err)
	}
	if runner.lastArg != nil {
		t.Fatal("agent ran on a fixture that was not broken")
	}
}

func TestRunAllAggregatesPassRateAndCost(t *testing.T) {
	root := t.TempDir()
	verify := "grep -q fixed f.txt\n"
	writeFixture(t, root, "a-fails", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", verify)
	// Only this fixture is fixable, which the fix below detects from the work
	// dir — the same way a real fix works off the files in front of it.
	writeFixture(t, root, "b-passes", "printf 'broken\\n' > f.txt; printf 'fixable\\n' > marker\n", "sh check.sh\n", verify)
	writeFixture(t, root, "c-skipped", "exit 127\n", "go build ./...\n", "go build ./...\n")

	meter := NewMeter()
	cases, err := LoadCases(root)
	if err != nil {
		t.Fatal(err)
	}

	// The fix only applies to the fixture that leaves a marker behind, so
	// a-fails stays broken and b-passes goes green.
	report := RunAll(context.Background(), cases, Config{
		Runner: &fakeRunner{t: t, meter: meter, model: "gemini-2.5-pro", in: 200, out: 50, fix: func(t *testing.T) {
			if _, err := os.Stat("marker"); err != nil {
				return // nothing this agent knows how to fix
			}
			if err := os.WriteFile("f.txt", []byte("fixed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		Meter:    meter,
		Provider: "gemini",
		Model:    "gemini-2.5-pro",
	})

	if report.Total != 3 || report.Attempted != 2 || report.Passed != 1 || report.Failed != 1 || report.Skipped != 1 {
		t.Fatalf("counts wrong: %+v", report)
	}
	if report.PassRate != 0.5 {
		t.Fatalf("pass rate = %v, want 0.5", report.PassRate)
	}
	if report.Calls != 2 {
		t.Fatalf("llm calls = %d, want 2 (the skipped case must not be billed)", report.Calls)
	}
	if !report.CostPerFixKnown || report.AvgCostPerFix <= 0 {
		t.Fatalf("cost per fix should be known and positive: %+v", report)
	}
	if len(report.Models) != 1 || report.Models[0] != "gemini-2.5-pro" {
		t.Fatalf("models = %v", report.Models)
	}
}

// meterDiff is a helper for the per-model accounting tests.
// A case that cannot be evaluated at all is reported, but must not be scored
// as a fix failure: a suite that cannot run is not evidence the agent regressed.
func TestRunAllExcludesUnevaluableCasesFromPassRate(t *testing.T) {
	root := t.TempDir()
	verify := "grep -q fixed f.txt\n"
	writeFixture(t, root, "a-passes", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", verify)
	writeFixture(t, root, "b-broken-fixture", "echo 'boom' >&2\nexit 1\n", "sh check.sh\n", verify)

	cases, err := LoadCases(root)
	if err != nil {
		t.Fatal(err)
	}
	report := RunAll(context.Background(), cases, Config{
		Runner: &fakeRunner{t: t, fix: func(t *testing.T) {
			if err := os.WriteFile("f.txt", []byte("fixed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		Meter:    NewMeter(),
		Provider: "gemini",
		Model:    "gemini-2.5-pro",
	})

	if report.Total != 2 || report.Attempted != 1 || report.Passed != 1 || report.Failed != 0 || report.Errors != 1 {
		t.Fatalf("counts wrong: %+v", report)
	}
	if report.PassRate != 1 {
		t.Fatalf("pass rate = %v, want 1 (the error case must not count as a miss)", report.PassRate)
	}
	if report.Results[1].Status != StatusError || report.Results[1].Err == "" {
		t.Fatalf("expected an ERROR result with a reason: %+v", report.Results[1])
	}
}

// Running out of budget is a fix failure, not an unevaluable case: it must
// show up in the pass rate rather than being dropped from it.
func TestRunEvalCountsTimeoutAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the budget is already gone when the case starts

	dir := writeFixture(t, t.TempDir(), "case", "true\n", "true\n", "true\n")
	runner := &fakeRunner{t: t}

	res := RunEval(ctx, Case{Name: "case", Dir: dir, RunCmd: "true", VerifyCmd: "true"},
		Config{Runner: runner, Meter: NewMeter(), Timeout: time.Millisecond})

	if res.Status != StatusFail || res.Passed || res.Skipped {
		t.Fatalf("status=%s passed=%v skipped=%v, want a plain failure", res.Status, res.Passed, res.Skipped)
	}
	if res.AgentError == "" {
		t.Fatal("a timeout should say so")
	}
}

// resetRunner is a fakeRunner that also implements Resetter, counting the
// resets so a test can prove each case starts on a clean agent.
type resetRunner struct {
	fakeRunner
	resets int
}

func (r *resetRunner) Reset() { r.resets++ }

// The harness runs every case through one shared agent, so a runner that
// accumulates per-run state must be reset before each case. Without the reset,
// a case that follows a doom-looped one inherits its fingerprints and bails out
// before spending any LLM calls.
func TestRunnerReset(t *testing.T) {
	root := t.TempDir()
	verify := "grep -q fixed f.txt\n"
	writeFixture(t, root, "a-case", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", verify)
	writeFixture(t, root, "b-case", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", verify)

	cases, err := LoadCases(root)
	if err != nil {
		t.Fatal(err)
	}

	runner := &resetRunner{fakeRunner: fakeRunner{t: t, fix: func(t *testing.T) {
		if err := os.WriteFile("f.txt", []byte("fixed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}}}

	report := RunAll(context.Background(), cases, Config{
		Runner:   runner,
		Meter:    NewMeter(),
		Provider: "gemini",
		Model:    "gemini-2.5-pro",
	})

	if report.Attempted != 2 {
		t.Fatalf("attempted = %d, want 2 (both fixtures should reach the agent): %+v", report.Attempted, report)
	}
	if runner.resets != 2 {
		t.Fatalf("Reset called %d times, want 2 (once before each case)", runner.resets)
	}
}

// A runner that does not implement Resetter is still valid: the reset is
// optional, so a minimal AgentRunner keeps working.
func TestRunEvalWithoutResetter(t *testing.T) {
	dir := writeFixture(t, t.TempDir(), "case", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", "true\n")

	res := RunEval(context.Background(), Case{
		Name: "case", Dir: dir, RunCmd: "sh check.sh", VerifyCmd: "true",
	}, Config{Runner: &fakeRunner{t: t}, Meter: NewMeter()})

	if res.Status != StatusPass {
		t.Fatalf("status=%s err=%q", res.Status, res.Err)
	}
}

func TestDiffUsageAttributesPerModel(t *testing.T) {
	m := NewMeter()
	m.record("gemini-2.0-flash", 100, 20)
	m.record("gemini-2.5-pro", 300, 60)
	before := m.Snapshot()
	m.record("gemini-2.5-pro", 100, 10)
	d := DiffUsage(before, m.Snapshot())

	if d.Input != 100 || d.Output != 10 || d.Calls != 1 {
		t.Fatalf("delta totals wrong: %+v", d)
	}
	if len(d.Models) != 1 {
		t.Fatalf("delta should only contain the model called in the window: %v", d.ModelNames())
	}
	if d.DominantModel() != "gemini-2.5-pro" {
		t.Fatalf("dominant = %q", d.DominantModel())
	}
}

func TestUsageCostSumsPerModelRates(t *testing.T) {
	u := Usage{Models: map[string]ModelUsage{
		"gemini-2.0-flash": {Calls: 1, Input: 1_000_000, Output: 0}, // $0.10
		"gemini-2.5-pro":   {Calls: 1, Input: 100_000, Output: 0},   // $0.125
	}}
	cost, ok := u.Cost()
	if !ok {
		t.Fatal("both models have rates, so cost should be known")
	}
	if diff := cost - 0.225; diff > 0.001 || diff < -0.001 {
		t.Fatalf("cost = %v, want ~0.225", cost)
	}

	// An unpriced model makes the total a lower bound rather than a wrong number.
	u.Models["meta/llama-3.1-405b-instruct"] = ModelUsage{Calls: 1, Input: 1000}
	if _, ok := u.Cost(); ok {
		t.Fatal("expected cost to be unknown once a model has no rate")
	}

	// A case that never reached a provider costs nothing, which is known — not
	// unknown, or one erroring case would mark the whole report as unpriced.
	if cost, ok := (Usage{}).Cost(); !ok || cost != 0 {
		t.Fatalf("empty usage cost = %v (known=%v), want 0 (known)", cost, ok)
	}
}

func TestMeterProviderCountsTokens(t *testing.T) {
	inner := &stubProvider{name: "gemini", text: "0123456789abcdef", model: "gemini-2.5-pro"} // 16 chars -> 4 tokens
	m := NewMeter()
	p := MeterProvider(inner, m, nil)

	ch, err := p.Complete(context.Background(), llm.Request{SystemPrompt: strings.Repeat("x", 40)})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	// The stub emits two tokens, so the goroutine records on close.
	waitFor(t, func() bool { return m.Snapshot().Calls == 1 })

	snap := m.Snapshot()
	if snap.Input != 10 {
		t.Fatalf("input tokens = %d, want 10 (40 chars)", snap.Input)
	}
	if snap.Output != 8 {
		t.Fatalf("output tokens = %d, want 8 (two 16-char tokens)", snap.Output)
	}
	if got := snap.DominantModel(); got != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want the one the wrapped provider reported", got)
	}
}

func TestMeterProviderForwardsModel(t *testing.T) {
	p := MeterProvider(&stubProvider{name: "gemini", model: "gemini-2.5-pro"}, NewMeter(), nil)
	if got := p.(interface{ Model(llm.Workload) string }).Model(llm.WorkloadHeal); got != "gemini-2.5-pro" {
		t.Fatalf("Model = %q", got)
	}
	// A provider that cannot name a model falls back to the configured names.
	p2 := MeterProvider(&stubProvider{name: "x"}, NewMeter(), map[llm.Workload]string{
		llm.WorkloadHeal: "fallback-model",
	})
	if got := p2.(interface{ Model(llm.Workload) string }).Model(llm.WorkloadDiagnose); got != "fallback-model" {
		t.Fatalf("fallback Model = %q", got)
	}
}

func TestCostForUnknownModel(t *testing.T) {
	if _, ok := CostFor("llama3.2", 1000, 1000); ok {
		t.Fatal("a self-hosted model has no list price, so it must not be priced")
	}
	cost, ok := CostFor("gemini-2.5-pro", 1_000_000, 0)
	if !ok || cost < 1.24 || cost > 1.26 {
		t.Fatalf("cost = %v (known=%v), want ~1.25 per million input tokens", cost, ok)
	}
	// A namespaced variant resolves to the same rate.
	if _, ok := CostFor("openai/gemini-2.5-pro-preview", 0, 0); !ok {
		t.Fatal("namespaced model should resolve to its base rate")
	}
}

func TestKeepTempLeavesWorkDirBehind(t *testing.T) {
	dir := writeFixture(t, t.TempDir(), "case", "printf 'broken\\n' > f.txt\n", "sh check.sh\n", "true\n")

	res := RunEval(context.Background(), Case{
		Name: "case", Dir: dir, RunCmd: "sh check.sh", VerifyCmd: "true",
	}, Config{Runner: &fakeRunner{t: t}, Meter: NewMeter(), KeepTemp: true})

	if res.WorkDir == "" {
		t.Fatal("expected a work dir to be reported")
	}
	if _, err := os.Stat(res.WorkDir); err != nil {
		t.Fatalf("work dir was not kept: %v", err)
	}
	os.RemoveAll(res.WorkDir)
}

func TestWithDirRestoresWorkingDirectory(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := withDir(dir, func() error {
		got, err := os.Getwd()
		if err != nil {
			return err
		}
		// macOS reports /private/var for /var, so compare resolved paths.
		if filepath.Base(got) != filepath.Base(dir) {
			t.Fatalf("inside withDir cwd = %q, want %q", got, dir)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("working directory not restored: %q != %q", after, before)
	}
}

// TestRepoFixturesLoad guards the fixtures shipped in testdata/evals.
func TestRepoFixturesLoad(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "testdata", "evals"))
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	want := []string{
		"go-missing-import", "go-undefined-var", "go-wrong-type",
		"node-missing-dep", "python-syntax-error",
	}
	if len(cases) != len(want) {
		t.Fatalf("got %d fixtures, want %d", len(cases), len(want))
	}
	for i, c := range cases {
		if c.Name != want[i] {
			t.Fatalf("fixture %d = %q, want %q", i, c.Name, want[i])
		}
		if c.Description == "" {
			t.Errorf("%s: empty description", c.Name)
		}
		if c.RunCmd == "" || c.VerifyCmd == "" {
			t.Errorf("%s: missing run_cmd/verify_cmd", c.Name)
		}
	}
}

// stubProvider is a minimal llm.Provider for the metering tests.
type stubProvider struct {
	name  string
	text  string
	model string
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Model(llm.Workload) string { return s.model }

func (s *stubProvider) Available(context.Context) bool { return true }

func (s *stubProvider) Complete(_ context.Context, _ llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 4)
	go func() {
		defer close(ch)
		for i := 0; i < 2; i++ {
			ch <- llm.Token{Text: s.text}
		}
	}()
	return ch, nil
}

func (s *stubProvider) Embed(context.Context, string) ([]float32, error) {
	return nil, nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

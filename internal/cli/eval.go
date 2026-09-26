package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/eval"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/memory"
)

const (
	defaultEvalDir     = "testdata/evals"
	defaultEvalTimeout = 5 * time.Minute
)

// evalProviders are the backends an eval run can be pinned to. It is
// knownProviders plus ollama, which authenticates with a base URL rather than a
// keyring key.
var evalProviders = append(append([]string{}, knownProviders...), "ollama")

type evalOptions struct {
	provider string
	dir      string
	filter   string
	json     bool
	keepTemp bool
	timeout  time.Duration
}

func newEvalCmd() *cobra.Command {
	var o evalOptions

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run Nebula against curated broken-project fixtures",
		Long: `Runs each eval fixture in an isolated copy of a broken project and reports
how often Nebula fixes it, what a fix costs and how long it takes.

Every fixture is copied to a temporary directory before setup.sh runs, so the
repo is never modified. A case counts as fixed only when its verify_cmd exits 0.

Exits non-zero when any case fails, so it can gate CI.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := runEval(o, cmd.Context())
			if err != nil {
				return err
			}
			// Exit after runEval's cleanup has run, so a failing suite does not
			// leave temp directories or an open database behind.
			if report.Failed > 0 || report.Errors > 0 {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&o.provider, "provider", "", "provider to evaluate (default: first with a key available)")
	cmd.Flags().StringVar(&o.dir, "dir", defaultEvalDir, "directory containing eval fixtures")
	cmd.Flags().StringVar(&o.filter, "filter", "", "only run cases whose name contains this substring")
	cmd.Flags().BoolVar(&o.json, "json", false, "print the report as JSON instead of a table")
	cmd.Flags().BoolVar(&o.keepTemp, "keep-temp", false, "keep each case's working directory for inspection")
	cmd.Flags().DurationVar(&o.timeout, "timeout", defaultEvalTimeout, "per-case time budget for the agent")

	return cmd
}

func runEval(o evalOptions, parent context.Context) (*eval.Report, error) {
	// Honoring --dry-run would leave every case unfixed and report it as a
	// failure, which would be a lie about the agent.
	if dryRun {
		return nil, fmt.Errorf("--dry-run is not supported by eval: it has no fixture to preview")
	}

	provider, err := resolveEvalProvider(o.provider)
	if err != nil {
		return nil, err
	}

	cases, err := eval.LoadCases(o.dir)
	if err != nil {
		return nil, err
	}
	cases = eval.Filter(cases, o.filter)
	if len(cases) == 0 {
		return nil, fmt.Errorf("no eval fixtures in %s matched %q", o.dir, o.filter)
	}

	// An eval run gets its own throwaway database: the failures it injects and
	// the patterns it learns from them must not reach the user's memory store.
	// Parent temp dir for per-case databases. Each case gets its own DB so
	// patterns learned (or hallucinated) in one case cannot poison the next.
	dbParent, err := os.MkdirTemp("", "nebula-eval-db-")
	if err != nil {
		return nil, fmt.Errorf("create eval data dir: %w", err)
	}
	defer os.RemoveAll(dbParent)

	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Pin the run to one provider so token cost and the reported model are
	// attributable, and meter every completion to report cost per fix.
	meter := eval.NewMeter()
	router := buildRouter(provider, func(p llm.Provider) llm.Provider {
		return eval.MeterProvider(p, meter, nil)
	})

	// Every provider reports the model it would use per workload, so an empty
	// answer means none of them is usable — worth one clear error instead of
	// five cases that each fail for the same reason.
	model := router.Model(ctx, llm.WorkloadHeal)
	if model == "" {
		return nil, fmt.Errorf("no usable %s provider — check its key with: nebula key list", provider)
	}

	agentCfg := agent.Config{
		HistoryDepth:  historyDepth(),
		FileInjection: true, // always on for eval: fixtures are small and models need file context
	}
	caseN := 0
	factory := eval.RunnerFactory(func() (eval.AgentRunner, error) {
		caseN++
		dbPath := filepath.Join(dbParent, fmt.Sprintf("case-%d.db", caseN))
		store, err := memory.New(dbPath)
		if err != nil {
			return nil, fmt.Errorf("open eval store: %w", err)
		}
		return agent.New(newHarness(), router, store, agentCfg), nil
	})

	cfg := eval.Config{
		RunnerFactory: factory,
		Meter:         meter,
		Provider:      provider,
		Model:         model,
		Timeout:       o.timeout,
		KeepTemp:      o.keepTemp,
	}

	// With --json stdout must stay machine-readable: the agent's own PTY
	// output goes to the terminal regardless, but no progress lines are added.
	if !o.json {
		fmt.Printf("Nebula eval — %d case(s) · provider %s · model %s\n\n", len(cases), provider, model)
		cfg.OnResult = printEvalCase
	}

	report := eval.RunAll(ctx, cases, cfg)

	if o.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return nil, fmt.Errorf("encode report: %w", err)
		}
		return report, nil
	}

	printEvalReport(report)
	return report, nil
}

// resolveEvalProvider validates the requested provider, defaulting to the first
// one that has a key. Keys are only ever read — from the config file, the
// environment or the OS keyring — never written to disk.
func resolveEvalProvider(requested string) (string, error) {
	if requested == "" {
		if p := firstAvailableProvider(); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("no provider has a key — run: nebula key add <%s> <key>", strings.Join(knownProviders, "|"))
	}
	for _, p := range evalProviders {
		if p == requested {
			return requested, nil
		}
	}
	return "", fmt.Errorf("unknown provider %q — choose from: %s", requested, strings.Join(evalProviders, ", "))
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

// providerKeyEnvVar mirrors the environment variable loadKeys reads for each
// provider.
func providerKeyEnvVar(provider string) string {
	return "NEBULA_" + strings.ToUpper(provider) + "_KEY"
}

// printEvalCase prints one case as it finishes, with the reason underneath
// whenever it did not pass.
func printEvalCase(res *eval.Result) {
	fix := "-"
	if res.FixCount > 0 {
		fix = strconv.Itoa(res.FixCount)
	}
	cost := "-"
	if res.CostKnown {
		cost = formatCost(res.CostUSD)
	}

	fmt.Printf("  %-24s %-5s  fixes %-3s  %-8s  %-9s  %s\n",
		res.Name, res.Status, fix,
		formatEvalDuration(res.AgentDurationMS),
		formatTokens(res.TotalTokens),
		cost)

	if reason := evalCaseReason(res); reason != "" {
		for _, line := range strings.Split(reason, "\n") {
			fmt.Printf("      %s\n", line)
		}
	}
}

func evalCaseReason(res *eval.Result) string {
	switch res.Status {
	case eval.StatusPass:
		return ""
	case eval.StatusFail:
		if res.AgentError != "" {
			return "agent: " + res.AgentError
		}
		if res.VerifyOutput != "" {
			return "verify: " + res.VerifyOutput
		}
		return "verify_cmd still failing"
	case eval.StatusSkip:
		return "skipped: " + res.Err
	default:
		return res.Err
	}
}

func printEvalReport(r *eval.Report) {
	fmt.Println()
	fmt.Printf("  Pass rate   %d/%d (%s)\n", r.Passed, r.Attempted, formatPercent(r.PassRate))
	if r.CostPerFixKnown {
		fmt.Printf("  Cost/fix    %s avg · %s total\n", formatCost(r.AvgCostPerFix), formatCost(r.CostUSD))
	} else {
		fmt.Printf("  Cost/fix    unknown rate for at least one model\n")
	}
	fmt.Printf("  Time/fix    %s avg\n", formatEvalDuration(r.AvgFixTimeMS))
	fmt.Printf("  Tokens      %s total, est. (%d in / %d out, %d calls)\n",
		formatTokens(r.TotalTokens), r.InputTokens, r.OutputTokens, r.Calls)
	models := strings.Join(r.Models, ", ")
	if models == "" {
		models = r.Model
	}
	fmt.Printf("  Model       %s via %s\n", models, r.Provider)
	fmt.Printf("  Duration    %s\n", formatEvalDuration(r.DurationMS))
	if r.Skipped > 0 {
		fmt.Printf("  Skipped     %d case(s) — see reasons above\n", r.Skipped)
	}
	if r.Errors > 0 {
		fmt.Printf("  Errors      %d case(s) could not be evaluated — see reasons above\n", r.Errors)
	}
	if r.Attempted == 0 {
		fmt.Println("\n  No case was attempted — check the fixtures and the toolchains they need.")
	}
}

// formatEvalDuration renders milliseconds, keeping sub-second fixes legible
// instead of rounding them all to "0s".
func formatEvalDuration(ms int64) string {
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + "s"
}

func formatPercent(f float64) string {
	return strconv.FormatFloat(f*100, 'f', 1, 64) + "%"
}

// formatTokens abbreviates a token count, since the exact number is noise when
// comparing runs.
func formatTokens(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	default:
		return strconv.FormatFloat(float64(n)/1_000_000, 'f', 2, 64) + "M"
	}
}

// formatCost keeps sub-cent fixes readable: a single heal on a cheap model costs
// fractions of a cent, and $0.00 would make every case look free.
func formatCost(v float64) string {
	if v > 0 && v < 0.01 {
		return "$" + strconv.FormatFloat(v, 'f', 4, 64)
	}
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

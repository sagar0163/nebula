package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/llm/providers"
	"github.com/sagar0163/nebula/internal/pty"
	"github.com/sagar0163/nebula/internal/safety"
	"github.com/sagar0163/nebula/internal/tui"
	"github.com/zalando/go-keyring"
)

var (
	cfgFile string
	dryRun  bool
	version = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "nebula [command]",
	Short: "Self-healing terminal agent with AI-powered command recovery",
	Long: `Nebula is a terminal agent that learns from your commands and
automatically fixes failures. When a command fails, it analyzes the
error, suggests a fix, and lets you apply it with one keystroke.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/nebula/config.toml)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "show what would run without executing")
	rootCmd.PersistentFlags().Bool("dangerously-skip-permissions", false, "skip all permission checks (use with extreme caution)")

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runInteractive()
		}
		return runCommand(args)
	}

	rootCmd.AddCommand(
		newSetupCmd(),
		newSessionCmd(),
		newRunCmd(),
		newAskCmd(),
		newKeyCmd(),
		newSkillCmd(),
		newWorkflowCmd(),
		newWatchCmd(),
		newEvalCmd(),
		newDoCmd(),
		newFixCmd(),
		newChatCmd(),
		newVersionCmd(),
	)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		viper.AddConfigPath(home + "/.config/nebula")
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
	}

	viper.SetEnvPrefix("NEBULA")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
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

// buildAgent constructs the agent from viper config.
func buildAgent() (*agent.Agent, error) {
	store, err := openStore()
	if err != nil {
		return nil, err
	}

	return agent.New(newHarness(), buildRouter("", nil), store, agent.Config{
		HistoryDepth:  historyDepth(),
		FileInjection: fileInjectionEnabled(),
	}), nil
}

// newHarness builds the PTY harness from the [pty] config section.
func newHarness() *pty.Harness {
	ringKB := viper.GetInt("pty.ring_kb")
	if ringKB == 0 {
		ringKB = 256
	}
	capKB := viper.GetInt("pty.capture_kb")
	if capKB == 0 {
		capKB = 512
	}
	return pty.NewHarness(ringKB*1024, capKB*1024)
}

func historyDepth() int {
	depth := viper.GetInt("agent.agent_history_depth")
	if depth <= 0 {
		return 10
	}
	return depth
}

func fileInjectionEnabled() bool {
	if viper.IsSet("agent.agent_file_injection") {
		return viper.GetBool("agent.agent_file_injection")
	}
	return true
}

// buildRouter registers every provider that has a key (or an ollama base URL)
// for the workloads it serves. Keys are always read through loadKeys, which
// never writes them to disk.
//
// When only is non-empty, providers whose Name() differs are left
// unregistered, which is how `nebula eval` pins a run to a single backend. wrap,
// when non-nil, decorates each provider before registration; the eval command
// uses it to meter token usage without touching the providers themselves.
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

// agentAdapter wraps *agent.Agent to satisfy tui.AgentRunner,
// translating between the two option/result types.
type agentAdapter struct{ a *agent.Agent }

func (ad agentAdapter) Run(ctx context.Context, args []string, opts tui.AgentRunOptions) (*tui.AgentResult, error) {
	agentOpts := agent.RunOptions{
		DryRun:          opts.DryRun,
		SkipPermissions: opts.SkipPermissions,
		ApprovalFn:      opts.ApprovalFn,
	}
	r, err := ad.a.Run(ctx, args, agentOpts)
	if err != nil {
		return nil, err
	}
	return &tui.AgentResult{
		Command:   r.Command,
		ExitCode:  r.ExitCode,
		Healed:    r.Healed,
		HealApply: r.HealApply,
	}, nil
}

func runInteractive() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	configPath := filepath.Join(home, ".config", "nebula", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := runSetupWizard(); err != nil {
			return fmt.Errorf("run setup wizard: %w", err)
		}
	}

	a, err := buildAgent()
	if err != nil {
		return fmt.Errorf("init agent: %w", err)
	}

	m := tui.New(agentAdapter{a}, dryRun)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func runCommand(args []string) error {
	a, err := buildAgent()
	if err != nil {
		return fmt.Errorf("init agent: %w", err)
	}

	skipPerms, _ := rootCmd.PersistentFlags().GetBool("dangerously-skip-permissions")
	opts := agent.RunOptions{
		DryRun:          dryRun,
		SkipPermissions: skipPerms,
		ApprovalFn: func(cmd string, _ safety.Risk) bool {
			fmt.Fprintf(os.Stderr, "nebula: approve running %q? [y/N] ", cmd)
			var resp string
			fmt.Scanln(&resp)
			return resp == "y" || resp == "Y"
		},
	}
	
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	
	res, err := a.Run(ctx, args, opts)
	if err != nil {
		return err
	}
	if res != nil && res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
	return nil
}

// runAsk sends a general-purpose prompt to the agent and prints the response.
func runAsk(input string) error {
	a, err := buildAgent()
	if err != nil {
		return fmt.Errorf("init agent: %w", err)
	}

	_, err = a.Ask(context.Background(), input, true)
	if err != nil {
		return fmt.Errorf("ask: %w", err)
	}
	return nil
}

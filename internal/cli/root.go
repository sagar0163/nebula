package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/llm/providers"
	"github.com/sagar0163/nebula/internal/memory"
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

// buildAgent constructs the agent from viper config.
func buildAgent() (*agent.Agent, error) {
	// Memory store.
	home, _ := os.UserHomeDir()
	dbPath := viper.GetString("memory.db_path")
	if dbPath == "" {
		dbPath = filepath.Join(home, ".local", "share", "nebula", "nebula.db")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	store, err := memory.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}

	// LLM router.
	router := llm.NewRouter()

	groqKey := viper.GetString("llm.groq.api_key")
	if groqKey == "" {
		groqKey, _ = keyring.Get("nebula", "groq_api_key")
	}
	if groqKey != "" {
		p := providers.NewGroq(providers.GroqConfig{
			APIKey:        groqKey,
			ModelDiagnose: viper.GetString("llm.groq.model_diagnose"),
			ModelHeal:     viper.GetString("llm.groq.model_heal"),
			ModelLearn:    viper.GetString("llm.groq.model_learn"),
		})
		if p != nil {
			router.Register(llm.WorkloadDiagnose, p)
			router.Register(llm.WorkloadHeal, p)
			router.Register(llm.WorkloadLearn, p)
		}
	}

	geminiKey := viper.GetString("llm.gemini.api_key")
	if geminiKey == "" {
		geminiKey, _ = keyring.Get("nebula", "gemini_api_key")
	}
	if geminiKey != "" {
		p := providers.NewGemini(providers.GeminiConfig{
			APIKey:        geminiKey,
			ModelDiagnose: viper.GetString("llm.gemini.model_diagnose"),
			ModelHeal:     viper.GetString("llm.gemini.model_heal"),
			ModelLearn:    viper.GetString("llm.gemini.model_learn"),
			ModelEmbed:    viper.GetString("llm.gemini.model_embed"),
		})
		if p != nil {
			router.Register(llm.WorkloadDiagnose, p)
			router.Register(llm.WorkloadHeal, p)
			router.Register(llm.WorkloadLearn, p)
			router.Register(llm.WorkloadEmbed, p)
		}
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
			router.Register(llm.WorkloadDiagnose, p)
			router.Register(llm.WorkloadHeal, p)
			router.Register(llm.WorkloadLearn, p)
			router.Register(llm.WorkloadEmbed, p)
		}
	}

	mistralKey := viper.GetString("llm.mistral.api_key")
	if mistralKey == "" {
		mistralKey, _ = keyring.Get("nebula", "mistral_api_key")
	}
	if mistralKey != "" {
		p := providers.NewMistral(providers.MistralConfig{
			APIKey:        mistralKey,
			ModelDiagnose: viper.GetString("llm.mistral.model_diagnose"),
			ModelHeal:     viper.GetString("llm.mistral.model_heal"),
			ModelLearn:    viper.GetString("llm.mistral.model_learn"),
			ModelEmbed:    viper.GetString("llm.mistral.model_embed"),
		})
		if p != nil {
			router.Register(llm.WorkloadDiagnose, p)
			router.Register(llm.WorkloadHeal, p)
			router.Register(llm.WorkloadLearn, p)
			router.Register(llm.WorkloadEmbed, p)
		}
	}

	nvidiaKey := viper.GetString("llm.nvidia.api_key")
	if nvidiaKey == "" {
		nvidiaKey, _ = keyring.Get("nebula", "nvidia_api_key")
	}
	if nvidiaKey != "" {
		p := providers.NewNvidia(providers.NvidiaConfig{
			APIKey:        nvidiaKey,
			BaseURL:       viper.GetString("llm.nvidia.base_url"),
			ModelDiagnose: viper.GetString("llm.nvidia.model_diagnose"),
			ModelHeal:     viper.GetString("llm.nvidia.model_heal"),
			ModelLearn:    viper.GetString("llm.nvidia.model_learn"),
			ModelEmbed:    viper.GetString("llm.nvidia.model_embed"),
		})
		if p != nil {
			router.Register(llm.WorkloadDiagnose, p)
			router.Register(llm.WorkloadHeal, p)
			router.Register(llm.WorkloadLearn, p)
			router.Register(llm.WorkloadEmbed, p)
		}
	}

	harness := pty.NewHarness(0)
	return agent.New(harness, router, store), nil
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
	_, err = a.Run(context.Background(), args, opts)
	return err
}

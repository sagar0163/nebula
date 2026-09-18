package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runInteractive()
		}
		return runCommand(args)
	},
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

func runInteractive() error {
	// TODO: launch bubbletea TUI in interactive REPL mode
	fmt.Println("Starting Nebula interactive session... (TUI coming soon)")
	return nil
}

func runCommand(args []string) error {
	// TODO: run a one-shot command through the agent + PTY harness
	fmt.Printf("Running command through Nebula: %v\n", args)
	return nil
}

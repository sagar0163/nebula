package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("nebula %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		},
	}
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Configure Nebula (AI provider, API keys, preferences)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: launch huh form wizard for first-time setup
			fmt.Println("Setup wizard coming soon...")
			return nil
		},
	}
}

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage sessions",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List past sessions",
			RunE: func(cmd *cobra.Command, args []string) error {
				// TODO: query SQLite sessions table
				fmt.Println("Sessions: (coming soon)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "resume [id]",
			Short: "Resume a past session",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				// TODO: load session from SQLite and re-enter TUI
				fmt.Printf("Resuming session %s...\n", args[0])
				return nil
			},
		},
	)

	return cmd
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [command...]",
		Short: "Run a command through the Nebula agent with auto-healing",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: wire through PTY harness + safety + agent loop
			fmt.Printf("Running: %v\n", args)
			return nil
		},
	}
}

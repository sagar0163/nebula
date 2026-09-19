package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sagar0163/nebula/internal/memory"
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
			return runSetupWizard()
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
				store, err := openStore()
				if err != nil {
					return err
				}
				defer store.Close()

				sessions, err := store.ListSessions(context.Background(), 20)
				if err != nil {
					return fmt.Errorf("list sessions: %w", err)
				}
				if len(sessions) == 0 {
					fmt.Println("No sessions yet.")
					return nil
				}
				for _, s := range sessions {
					fmt.Printf("  %-36s  %s  %s\n", s.ID, s.CreatedAt.Format("2006-01-02 15:04:05"), s.WorkDir)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "resume [id]",
			Short: "Resume a past session",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				store, err := openStore()
				if err != nil {
					return err
				}
				defer store.Close()

				s, err := store.GetSession(context.Background(), args[0])
				if err != nil {
					return fmt.Errorf("get session: %w", err)
				}

				fmt.Printf("Session:  %s\n", s.ID)
				fmt.Printf("Started:  %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"))
				fmt.Printf("WorkDir:  %s\n", s.WorkDir)
				if s.Summary != "" {
					fmt.Printf("Summary:  %s\n", s.Summary)
				}

				cmds, err := store.RecentCommands(context.Background(), s.ID, 10)
				if err == nil && len(cmds) > 0 {
					fmt.Println("\nRecent commands:")
					for _, c := range cmds {
						status := "✓"
						if c.ExitCode != 0 {
							status = fmt.Sprintf("✗ (%d)", c.ExitCode)
						}
						fmt.Printf("  %s  %s\n", status, c.Raw)
					}
				}
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
			return runCommand(args)
		},
	}
}

// openStore opens the SQLite store using the configured (or default) db path.
func openStore() (memory.Store, error) {
	dbPath := viper.GetString("memory.db_path")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dbPath = filepath.Join(home, ".local", "share", "nebula", "nebula.db")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return memory.New(dbPath)
}

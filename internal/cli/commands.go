package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/skills"
	"github.com/sagar0163/nebula/internal/workflow"
	"github.com/sagar0163/nebula/internal/daemon"
)

var knownProviders = []string{"groq", "gemini", "mistral", "nvidia"}

func slotName(provider string, slot int) string {
	base := provider + "_api_key"
	if slot == 1 {
		return base
	}
	return base + "_" + strconv.Itoa(slot)
}

func nextFreeSlot(provider string) int {
	for i := 1; i <= 9; i++ {
		v, _ := keyring.Get("nebula", slotName(provider, i))
		if v == "" {
			return i
		}
	}
	return 0
}

func maskKey(k string) string {
	if len(k) >= 12 {
		return k[:4] + "..." + k[len(k)-4:]
	}
	if len(k) > 4 {
		return k[:4] + "..."
	}
	return "****"
}

func isKnownProvider(p string) bool {
	for _, known := range knownProviders {
		if known == p {
			return true
		}
	}
	return false
}

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage API keys stored in the OS keyring",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all stored API keys (masked)",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Printf("  %-10s  %-6s  %s\n", "PROVIDER", "SLOT", "KEY")
				fmt.Printf("  %-10s  %-6s  %s\n", "--------", "----", "---")
				for _, p := range knownProviders {
					printed := false
					for i := 1; i <= 9; i++ {
						v, _ := keyring.Get("nebula", slotName(p, i))
						if v != "" {
							label := ""
							if i == 1 {
								label = " (primary)"
							}
							fmt.Printf("  %-10s  slot %-2d  %s%s\n", p, i, maskKey(v), label)
							printed = true
						}
					}
					if !printed {
						fmt.Printf("  %-10s  slot 1   (not set)\n", p)
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "add <provider> <key>",
			Short: "Add an API key for a provider (auto-selects next free slot)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				provider, key := args[0], args[1]
				if !isKnownProvider(provider) {
					return fmt.Errorf("unknown provider %q — choose from: %s", provider, strings.Join(knownProviders, ", "))
				}
				if key == "" {
					return fmt.Errorf("key cannot be empty")
				}
				slot := nextFreeSlot(provider)
				if slot == 0 {
					return fmt.Errorf("all 9 slots for %s are full — remove one first with: nebula key remove %s <slot>", provider, provider)
				}
				name := slotName(provider, slot)
				if err := keyring.Set("nebula", name, key); err != nil {
					return fmt.Errorf("store key in keyring: %w", err)
				}
				fmt.Printf("Added %s key to slot %d (%s)\n", provider, slot, name)
				return nil
			},
		},
		&cobra.Command{
			Use:   "remove <provider> <slot>",
			Short: "Remove a stored API key by slot number (1-9)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				provider := args[0]
				if !isKnownProvider(provider) {
					return fmt.Errorf("unknown provider %q — choose from: %s", provider, strings.Join(knownProviders, ", "))
				}
				slot, err := strconv.Atoi(args[1])
				if err != nil || slot < 1 || slot > 9 {
					return fmt.Errorf("slot must be a number between 1 and 9")
				}
				name := slotName(provider, slot)
				existing, _ := keyring.Get("nebula", name)
				if existing == "" {
					return fmt.Errorf("no key in slot %d for %s", slot, provider)
				}
				if err := keyring.Delete("nebula", name); err != nil {
					return fmt.Errorf("remove key from keyring: %w", err)
				}
				fmt.Printf("Removed %s key from slot %d (%s)\n", provider, slot, name)
				return nil
			},
		},
	)

	return cmd
}

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage and inspect Nebula skills",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all available skills",
			RunE: func(cmd *cobra.Command, args []string) error {
				list, err := skills.List()
				if err != nil {
					return err
				}
				if len(list) == 0 {
					fmt.Println("No skills found. Add .md files to ~/.config/nebula/skills/")
					return nil
				}
				for _, s := range list {
					fmt.Printf("  %-20s  %s\n", s.Name, s.Description)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "show <name>",
			Short: "Print a skill's instructions",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := skills.Load(args[0])
				if err != nil {
					return err
				}
				fmt.Printf("# %s\n%s\n\n%s\n", s.Name, s.Description, s.Instructions)
				return nil
			},
		},
		&cobra.Command{
			Use:   "create <name>",
			Short: "Create a new skill interactively",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				name := args[0]
				reader := bufio.NewReader(os.Stdin)

				fmt.Print("Description (single line): ")
				desc, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				desc = strings.TrimSpace(desc)

				fmt.Println("Instructions (multi-line, end with empty line or Ctrl+D):")
				var instructions []string
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "" {
						break
					}
					instructions = append(instructions, line)
				}
				if err := scanner.Err(); err != nil {
					return err
				}

				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				skillPath := filepath.Join(home, ".config", "nebula", "skills", name+".md")

				content := fmt.Sprintf("---\ndescription: %s\n---\n%s\n", desc, strings.Join(instructions, "\n"))
				
				if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
					return err
				}

				fmt.Printf("Skill saved to %s\n", skillPath)
				return nil
			},
		},
	)
	return cmd
}

func newWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Run multi-step AI workflows",
	}

	runCmd := &cobra.Command{
		Use:   "run <file> [key=value...]",
		Short: "Run a workflow YAML file",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bg, _ := cmd.Flags().GetBool("background")
			wf, err := workflow.LoadFile(args[0])
			if err != nil {
				return err
			}
			inputs := map[string]string{}
			for _, kv := range args[1:] {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					inputs[parts[0]] = parts[1]
				}
			}
			a, err := buildAgent()
			if err != nil {
				return fmt.Errorf("init agent: %w", err)
			}

			if bg {
				store, err := openStore()
				if err != nil {
					return err
				}
				defer store.Close()

				id, err := wf.RunBackground(context.Background(), a, store, inputs)
				if err != nil {
					return err
				}
				fmt.Printf("Job started: %s\n", id)
				return nil
			}

			outputs, err := wf.Run(context.Background(), a, inputs)
			for stepName, out := range outputs {
				fmt.Printf("\n=== Step: %s ===\n%s\n", stepName, out)
			}
			return err
		},
	}
	runCmd.Flags().Bool("background", false, "Run workflow in the background")

	statusCmd := &cobra.Command{
		Use:   "status <job-id>",
		Short: "Check the status of a background workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			defer store.Close()

			job, err := store.GetWorkflowJob(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("status: %s\ncurrent_step: %s\ncreated_at: %s\n", job.Status, job.CurrentStep, job.CreatedAt.Format("2006-01-02 15:04:05"))
			return nil
		},
	}

	resultCmd := &cobra.Command{
		Use:   "result <job-id>",
		Short: "View the results of a background workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			defer store.Close()

			job, err := store.GetWorkflowJob(context.Background(), args[0])
			if err != nil {
				return err
			}

			var outputs map[string]string
			if job.Output != "" && job.Output != "{}" {
				if err := json.Unmarshal([]byte(job.Output), &outputs); err != nil {
					return fmt.Errorf("parse output: %w", err)
				}
			}

			for stepName, out := range outputs {
				fmt.Printf("\n=== Step: %s ===\n%s\n", stepName, out)
			}
			if job.Error != "" {
				fmt.Printf("\n=== Error ===\n%s\n", job.Error)
			}

			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all past background workflow jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			defer store.Close()

			jobs, err := store.ListWorkflowJobs(context.Background(), 20)
			if err != nil {
				return err
			}

			fmt.Printf("%-36s  %-8s %-12s %s\n", "ID", "STATUS", "STEP", "STARTED")
			fmt.Printf("%-36s  %-8s %-12s %s\n", "----", "------", "----", "-------")
			for _, job := range jobs {
				fmt.Printf("%-36s  %-8s %-12s %s\n", job.ID, job.Status, job.CurrentStep, job.CreatedAt.Format("2006-01-02 15:04"))
			}

			return nil
		},
	}

	
	cancelCmd := &cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a background workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			defer store.Close()
			if err := store.CancelWorkflowJob(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Job %s cancelled.\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(runCmd, statusCmd, resultCmd, listCmd, cancelCmd)
	return cmd
}

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

func newAskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ask [prompt...]",
		Short: "Ask Nebula anything — code, writing, research, general tasks",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsk(strings.Join(args, " "))
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

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Run as a background daemon monitoring the queue for workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAgent()
			if err != nil {
				return fmt.Errorf("init agent: %w", err)
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			defer store.Close()

			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			queueDir := filepath.Join(home, ".config", "nebula", "queue")
			if err := os.MkdirAll(queueDir, 0o755); err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			return daemon.Watch(ctx, a, store, queueDir)
		},
	}
}

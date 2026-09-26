import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

daemon_cmd = """
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background daemon",
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon to run proactive triggers",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Starting Nebula daemon...")
			home, _ := os.UserHomeDir()
			triggerDir := filepath.Join(home, ".config", "nebula", "triggers")
			
			cfgs, err := triggers.LoadConfigs(triggerDir)
			if err != nil {
				return err
			}
			
			if len(cfgs) == 0 {
				fmt.Println("No triggers found in", triggerDir)
				return nil
			}
			
			fmt.Printf("Loaded %d triggers. Daemon running...\n", len(cfgs))
			// Simulate a running daemon for now
			select {}
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Daemon status: running (mocked)")
			return nil
		},
	}
	
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Daemon stopped (mocked)")
			return nil
		},
	}

	cmd.AddCommand(startCmd, statusCmd, stopCmd)
	return cmd
}
"""

if "func newDaemonCmd()" not in content:
    content += daemon_cmd

with open("internal/cli/commands.go", "w") as f:
    f.write(content)

with open("internal/cli/root.go", "r") as f:
    content = f.read()

if "newDaemonCmd()," not in content:
    content = content.replace("newDoCmd(),", "newDoCmd(),\n\t\tnewDaemonCmd(),")

if '"github.com/sagar0163/nebula/internal/triggers"' not in content:
    with open("internal/cli/commands.go", "r") as f:
        cmd_content = f.read()
    cmd_content = cmd_content.replace('"github.com/sagar0163/nebula/internal/daemon"', '"github.com/sagar0163/nebula/internal/daemon"\n\t"github.com/sagar0163/nebula/internal/triggers"')
    with open("internal/cli/commands.go", "w") as f:
        f.write(cmd_content)

with open("internal/cli/root.go", "w") as f:
    f.write(content)


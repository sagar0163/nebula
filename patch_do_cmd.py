import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

do_cmd = """
func newDoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "do [goal...]",
		Short: "Achieve a natural language goal",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAgent()
			if err != nil {
				return err
			}
			
			skipPerms, _ := rootCmd.PersistentFlags().GetBool("dangerously-skip-permissions")
			opts := agent.RunOptions{
				DryRun:          dryRun,
				SkipPermissions: skipPerms,
				ApprovalFn: func(c string, _ safety.Risk) bool {
					fmt.Fprintf(os.Stderr, "nebula: approve running %q? [y/N] ", c)
					var resp string
					fmt.Scanln(&resp)
					return resp == "y" || resp == "Y"
				},
			}
			
			return a.DoGoal(cmd.Context(), strings.Join(args, " "), opts)
		},
	}
}
"""

if "func newDoCmd()" not in content:
    content += do_cmd

with open("internal/cli/commands.go", "w") as f:
    f.write(content)

with open("internal/cli/root.go", "r") as f:
    content = f.read()

if "newDoCmd()," not in content:
    content = content.replace("newVersionCmd(),", "newDoCmd(),\n\t\tnewVersionCmd(),")

with open("internal/cli/root.go", "w") as f:
    f.write(content)


import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

bad_fix = """func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix [description...]",
		Short: "Fix the codebase based on a natural language description",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Running nebula fix...")
			return nil
		},
	}
}"""

good_fix = """func newFixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [description...]",
		Short: "Fix the codebase based on a natural language description",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAgent()
			if err != nil {
				return err
			}
			
			verifyCmd, _ := cmd.Flags().GetString("verify")
			isDryRun, _ := cmd.Flags().GetBool("dry-run")
			
			desc := strings.Join(args, " ")
			goal := fmt.Sprintf("Fix the codebase so that: %s. ", desc)
			
			if verifyCmd != "" {
				goal += fmt.Sprintf("Run the verification command '%s' after making changes to ensure it passes. ", verifyCmd)
			} else {
				goal += "Run the test suite to verify the changes. "
			}
			
			if isDryRun {
				goal += "Show a diff of the proposed changes, but DO NOT apply them or write to the files."
			}
			
			skipPerms, _ := rootCmd.PersistentFlags().GetBool("dangerously-skip-permissions")
			opts := agent.RunOptions{
				DryRun:          isDryRun,
				SkipPermissions: skipPerms,
				ApprovalFn: func(c string, _ safety.Risk) bool {
					fmt.Fprintf(os.Stderr, "nebula: approve running %q? [y/N] ", c)
					var resp string
					fmt.Scanln(&resp)
					return resp == "y" || resp == "Y"
				},
			}
			
			return a.DoGoal(cmd.Context(), goal, opts)
		},
	}
	cmd.Flags().String("verify", "", "Explicit command to verify the fix")
	cmd.Flags().Bool("dry-run", false, "Plan only, show proposed diff, don't apply")
	return cmd
}"""

content = content.replace(bad_fix, good_fix)

with open("internal/cli/commands.go", "w") as f:
    f.write(content)

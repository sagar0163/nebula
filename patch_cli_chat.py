import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

bad_chat = """func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start a multi-turn conversation with Nebula",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Starting nebula chat...")
			return nil
		},
	}
}"""

good_chat = """func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start a multi-turn conversation with Nebula",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAgent()
			if err != nil {
				return err
			}
			
			fmt.Println("Nebula Chat initialized. Type your goals (or 'exit' to quit).")
			scanner := bufio.NewScanner(os.Stdin)
			
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
			
			for {
				fmt.Print("\n> ")
				if !scanner.Scan() {
					break
				}
				input := strings.TrimSpace(scanner.Text())
				if input == "exit" || input == "quit" {
					break
				}
				if input == "" {
					continue
				}
				
				err := a.DoGoal(cmd.Context(), input, opts)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
				}
			}
			return nil
		},
	}
}"""

content = content.replace(bad_chat, good_chat)

with open("internal/cli/commands.go", "w") as f:
    f.write(content)

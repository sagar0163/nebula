import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

fix_chat = """
func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix [description...]",
		Short: "Fix the codebase based on a natural language description",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Running nebula fix...")
			return nil
		},
	}
}

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start a multi-turn conversation with Nebula",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Starting nebula chat...")
			return nil
		},
	}
}
"""

if "func newFixCmd" not in content:
    content += fix_chat

with open("internal/cli/commands.go", "w") as f:
    f.write(content)

with open("internal/cli/root.go", "r") as f:
    content = f.read()

if "newFixCmd()," not in content:
    content = content.replace("newDoCmd(),", "newDoCmd(),\n\t\tnewFixCmd(),\n\t\tnewChatCmd(),")

with open("internal/cli/root.go", "w") as f:
    f.write(content)

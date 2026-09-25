import re

with open("internal/agent/planner.go", "r") as f:
    content = f.read()

replacement = """	if len(history) > 0 {
		prompt += "\\nPreviously Tried Fixes:\\n"
		for i, h := range history {
			prompt += fmt.Sprintf("Attempt %d: %s\\nFailed with (Exit %d):\\n%s\\n\\n", i+1, h.FixCmd, h.ExitCode, SummarizeOutput(pty.StripANSI(h.Output)))
		}
	}

	prompt += `
Respond with:"""

content = content.replace("	prompt += `\nRespond with:", replacement)

with open("internal/agent/planner.go", "w") as f:
    f.write(content)

with open("internal/agent/planner.go", "r") as f:
    content = f.read()

replacement = """	prompt := fmt.Sprintf(`A shell command failed. Diagnose the error and suggest a fix.

Command: %s

Output:
%s
`, cmd, output)

	pCtx := DetectProjectContext("")
	if pCtxStr := pCtx.String(); pCtxStr != "" {
		prompt = pCtxStr + "\n\n" + prompt
	}"""

content = content.replace("""	prompt := fmt.Sprintf(`A shell command failed. Diagnose the error and suggest a fix.

Command: %s

Output:
%s
`, cmd, output)""", replacement)

with open("internal/agent/planner.go", "w") as f:
    f.write(content)

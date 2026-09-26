import re

with open("internal/agent/goal_planner.go", "r") as f:
    content = f.read()

bad = """	provider := p.router.Route(llm.WorkloadDiagnose) // Using diagnose workload for now
	if provider == nil {
		return nil, fmt.Errorf("no provider registered for planning")
	}

	prompt := fmt.Sprintf(`You are an autonomous agent. Your goal is: %s

Available tools:
- ShellTool (input: "command")
- ReadFileTool (input: "path")
- WriteFileTool (input: "path", "content")
- SearchTool (input: "query")
- WebFetchTool (input: "url")

Context:
%s

Break down the goal into a sequence of steps. Respond ONLY with a JSON object in this format:
{
	"steps": [
		{"tool": "ShellTool", "input": {"command": "ls -la"}},
		{"tool": "ReadFileTool", "input": {"path": "main.go"}}
	]
}`, goal, contextData)

	res, err := provider.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}"""

good = """	prompt := fmt.Sprintf(`You are an autonomous agent. Your goal is: %s

Available tools:
- ShellTool (input: "command")
- ReadFileTool (input: "path")
- WriteFileTool (input: "path", "content")
- SearchTool (input: "query")
- WebFetchTool (input: "url")

Context:
%s

Break down the goal into a sequence of steps. Respond ONLY with a JSON object in this format:
{
	"steps": [
		{"tool": "ShellTool", "input": {"command": "ls -la"}},
		{"tool": "ReadFileTool", "input": {"path": "main.go"}}
	]
}`, goal, contextData)

	req := llm.Request{
		Prompt:         prompt,
		MaxTokens:      1024,
		Temperature:    0.1,
		ResponseFormat: "json",
	}

	tokens, err := p.router.Complete(ctx, llm.WorkloadDiagnose, req)
	if err != nil {
		return nil, err
	}
	
	var builder strings.Builder
	for token := range tokens {
		if token.Error != nil {
			return nil, token.Error
		}
		builder.WriteString(token.Text)
	}
	res := builder.String()"""

content = content.replace(bad, good)

with open("internal/agent/goal_planner.go", "w") as f:
    f.write(content)

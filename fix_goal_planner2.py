import re

with open("internal/agent/goal_planner.go", "r") as f:
    content = f.read()

bad = """	req := llm.Request{
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

good = """	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
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
		if token.Err != nil {
			return nil, token.Err
		}
		builder.WriteString(token.Text)
	}
	res := builder.String()"""

content = content.replace(bad, good)

with open("internal/agent/goal_planner.go", "w") as f:
    f.write(content)

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace('\n\t"strings"', '')
content = content.replace('\n\t"github.com/sagar0163/nebula/internal/models"', '')

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)

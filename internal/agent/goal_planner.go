package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/tools"
)

type GoalPlanner struct {
	router *llm.Router
	tools  *tools.Registry
}

func NewGoalPlanner(router *llm.Router, tr *tools.Registry) *GoalPlanner {
	return &GoalPlanner{router: router, tools: tr}
}

func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string, codebase CodebaseIndex) ([]models.GoalStep, error) {
	prompt := fmt.Sprintf(`You are an autonomous agent. Your goal is: %s

%s

Codebase Summary:
%s

Context:
%s

Break down the goal into a sequence of steps. Respond ONLY with a JSON object in this format:
{
	"steps": [
		{"tool": "ShellTool", "input": {"command": "ls -la"}},
		{"tool": "ReadFileTool", "input": {"path": "main.go"}}
	]
}`, goal, p.tools.FormatPrompt(), codebase.Summary(), contextData)

	req := llm.Request{
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
	res := builder.String()

	trimmed := strings.TrimSpace(res)
	if strings.HasPrefix(trimmed, "```") {
		if i := strings.Index(trimmed[3:], "```"); i >= 0 {
			trimmed = strings.TrimSpace(trimmed[3 : 3+i])
			if after, ok := strings.CutPrefix(trimmed, "json"); ok {
				trimmed = strings.TrimSpace(after)
			}
		}
	}

	var plan models.GoalPlan
	if err := json.Unmarshal([]byte(trimmed), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse goal plan: %w", err)
	}

	return plan.Steps, nil
}

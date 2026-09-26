package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/tools"
	"github.com/sagar0163/nebula/internal/profile"
)

type GoalPlanner struct {
	router *llm.Router
	tools  *tools.Registry
}

func NewGoalPlanner(router *llm.Router, tr *tools.Registry) *GoalPlanner {
	return &GoalPlanner{router: router, tools: tr}
}

func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string, codebase CodebaseIndex, user profile.UserProfile, proj profile.ProjectProfile) ([]models.GoalStep, error) {
	prompt := fmt.Sprintf(`You are an autonomous agent. Your goal is: %s

%s

Codebase Summary:
%s

User Profile:
Name: %s
Email: %s
Preferences: %s

Project Profile:
Repo: %s
Readme: %s

Context:
%s

Break down the goal into a sequence of steps. Respond ONLY with a JSON object in this format:
{
	"steps": [
		{"tool": "ShellTool", "input": {"command": "ls -la"}},
		{"tool": "ReadFileTool", "input": {"path": "main.go"}}
	]
}`, goal, p.tools.FormatPrompt(), codebase.Summary(), user.Name, user.Email, user.LanguagePreferences, proj.RepoURL, proj.ReadmeIntro, contextData)

	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      1024,
		Temperature:    0.1,
		ResponseFormat: "json",
	}

	const maxRetries = 3
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		tokens, err := p.router.Complete(ctx, llm.WorkloadDiagnose, req)
		if err != nil {
			lastErr = err
			continue
		}

		var builder strings.Builder
		for token := range tokens {
			if token.Err != nil {
				lastErr = token.Err
				break
			}
			builder.WriteString(token.Text)
		}
		
		if lastErr != nil {
			continue
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
			// JSON parse failed - likely truncated response, retry if we have attempts left
			lastErr = fmt.Errorf("failed to parse goal plan (attempt %d/%d): %w", attempt+1, maxRetries+1, err)
			continue
		}

		return plan.Steps, nil
	}

	return nil, fmt.Errorf("goal planner failed after %d retries: %w", maxRetries+1, lastErr)
}

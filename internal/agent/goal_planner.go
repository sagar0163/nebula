package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string, user profile.UserProfile, proj profile.ProjectProfile) ([]models.GoalStep, error) {
	// Keep the most recent context; small-context providers reject huge prompts (413).
	if len(contextData) > 8000 {
		contextData = "...(earlier context truncated)...\n" + contextData[len(contextData)-8000:]
	}
	prompt := fmt.Sprintf(`Goal: %s

Context:
%s

Available tools:
%s

Decide the SINGLE next step to take, based on the context so far (results of previous steps are included above).
Return ONLY a JSON object with this exact structure, containing exactly one step:
{
  "steps": [
    {"tool": "ReadFileTool", "input": {"path": "path/to/file"}}
  ]
}

Each step's "input" must be an object with string keys and string values matching the tool's parameters.
Prefer StrReplaceTool for edits (old_string must match the file exactly and be unique) instead of rewriting whole files.
Only when the goal is genuinely accomplished and verified (never as the first step), output {"steps": [{"tool": "DoneTool", "input": {"reason": "..."}}]} in your JSON.
The "tool" field is just a string in your JSON reply - do not make native function/tool calls.

Do not include any explanation, markdown, or text outside the JSON.`, goal, contextData, p.tools.FormatPrompt())

	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      4096,
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
			time.Sleep(2 * time.Second)
			continue
		}

		res := builder.String()
		jsonStr := extractJSON(res)
		if jsonStr == "" {
			lastErr = fmt.Errorf("failed to extract JSON from response (attempt %d/%d)", attempt+1, maxRetries+1)
			continue
		}

		var plan models.GoalPlan
		if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
			lastErr = fmt.Errorf("failed to parse goal plan (attempt %d/%d): %w", attempt+1, maxRetries+1, err)
			
			// Feed the error back into the prompt for the next iteration
			req.Messages = append(req.Messages, llm.Message{Role: "assistant", Content: res})
			req.Messages = append(req.Messages, llm.Message{Role: "user", Content: fmt.Sprintf("Your last response was invalid JSON: %v. Please output ONLY valid JSON without markdown or text.", err)})
			
			time.Sleep(2 * time.Second)
			continue
		}

		return plan.Steps, nil
	}

	return nil, fmt.Errorf("goal planner failed after %d retries: %w", maxRetries+1, lastErr)
}

// extractJSON attempts to extract a JSON object from text that may contain extra content.
func extractJSON(text string) string {
	// First try: look for JSON in markdown code fences
	if strings.HasPrefix(text, "```") {
		if i := strings.Index(text[3:], "```"); i >= 0 {
			text = strings.TrimSpace(text[3 : 3+i])
			if after, ok := strings.CutPrefix(text, "json"); ok {
				text = strings.TrimSpace(after)
			}
		}
	}
	
	// If already looks like JSON, return as-is
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
		return text
	}
	
	// Try to find JSON object in text (between first { and last })
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	
	return ""
}

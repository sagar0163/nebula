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
	if len(contextData) > 32000 {
		contextData = "...(earlier context truncated)...\n" + contextData[len(contextData)-32000:]
	}
	prompt := fmt.Sprintf(`Goal: %s

Context:
%s

Available tools:
%s

Decide the SINGLE next step to take, based on the context so far (results of previous steps are included above).
Think through the problem briefly, then output your decision as a JSON object.
Your response must END with a JSON object with this exact structure, containing exactly one step:
{
  "steps": [
    {"tool": "ReadFileTool", "input": {"path": "path/to/file"}}
  ]
}

Each step's "input" must be an object with string keys and string values matching the tool's parameters.
HOW TO FIX A BUG:
1. Locate the relevant file (GrepTool or ShellTool with grep/find).
2. ALWAYS call ReadFileTool on the file first so you see the exact current code (indentation included). Tool results are truncated to ~8000 chars, so for large files first find the line with "grep -n" (ShellTool) and then pass "start_line"/"end_line" to ReadFileTool to read ~60 lines around it.
3. Apply the fix with StrReplaceTool using exactly these input keys:
   - "path": the file path
   - "old_string": the exact current code to replace, copied verbatim from the ReadFileTool output (no line numbers), and unique in the file (include surrounding lines if needed)
   - "new_string": the replacement code with correct indentation
   Example:
   {"steps": [{"tool": "StrReplaceTool", "input": {"path": "src/pkg/module.py", "old_string": "def is_valid(args):\n    return isinstance(args[0], Foo)", "new_string": "def is_valid(args):\n    if not args:\n        return False\n    return isinstance(args[0], Foo)"}}]}
4. If StrReplaceTool returns an error (not found / not unique), ReadFileTool again and retry with a corrected old_string.
5. Use WriteFileTool only for new files. Do not rewrite whole existing files.
Do NOT modify test files; fix the source code so the failing tests pass. If an error trace is given, fix the exact line it points at.
Call DoneTool ONLY after StrReplaceTool or WriteFileTool has successfully changed a file (never as the first step). Output {"steps": [{"tool": "DoneTool", "input": {"reason": "..."}}]} in your JSON.
The "tool" field is just a string in your JSON reply - do not make native function/tool calls.

You may write brief reasoning first, but the last thing in your response must be the JSON object.`, goal, contextData, p.tools.FormatPrompt())

	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:      4096,
		Temperature:    0.1,
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
			req.Messages = append(req.Messages, llm.Message{Role: "assistant", Content: res})
			req.Messages = append(req.Messages, llm.Message{Role: "user", Content: fmt.Sprintf("Your last response was invalid JSON: %v. Please end your response with a single valid JSON object.", err)})
			time.Sleep(2 * time.Second)
			continue
		}

		if len(plan.Steps) == 0 {
			lastErr = fmt.Errorf("planner returned empty steps array (attempt %d/%d)", attempt+1, maxRetries+1)
			req.Messages = append(req.Messages, llm.Message{Role: "assistant", Content: res})
			req.Messages = append(req.Messages, llm.Message{Role: "user", Content: `Your response had an empty "steps" array. You must return exactly one step. If you believe the goal is complete, return DoneTool: {"steps": [{"tool": "DoneTool", "input": {"reason": "done"}}]}`})
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
	
	// Prefer the LAST valid JSON object: reasoning text may contain braces.
	end := strings.LastIndex(text, "}")
	for end >= 0 {
		for start := strings.LastIndex(text[:end], "{"); start >= 0; start = strings.LastIndex(text[:start], "{") {
			if cand := text[start : end+1]; json.Valid([]byte(cand)) {
				return cand
			}
		}
		end = strings.LastIndex(text[:end], "}")
	}

	// Fallback: between first { and last }
	start := strings.Index(text, "{")
	end = strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}

	return ""
}

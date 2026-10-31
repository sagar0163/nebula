package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/tools"
)

const maxGoalIterations = 40

// summarizeArgs renders tool args compactly for logs so edits are visible.
func summarizeArgs(in map[string]interface{}) string {
	var b strings.Builder
	for _, k := range []string{"path", "command", "pattern", "old_string", "new_string", "content", "reason"} {
		v, ok := in[k]
		if !ok {
			continue
		}
		s := fmt.Sprintf("%v", v)
		s = strings.ReplaceAll(s, "\n", "\\n")
		if len(s) > 200 {
			s = s[:200] + "..."
		}
		fmt.Fprintf(&b, " %s=%q", k, s)
	}
	return b.String()
}

// toStringMap converts map[string]interface{} to map[string]string for tool execution.
func toStringMap(m map[string]interface{}) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			out[k] = val
		default:
			b, _ := json.Marshal(val)
			out[k] = string(b)
		}
	}
	return out
}

const (
	maxToolResultChars = 8000
	historyTrimTrigger = 60000 // total chars in history before trimming
	historyKeepChars   = 40000 // chars of recent history kept after trimming
)

// buildGoalSystemPrompt builds the first message of the executor conversation:
// the goal (including any planning context), tool list and working rules.
func buildGoalSystemPrompt(goal string, tr *tools.Registry) string {
	return fmt.Sprintf(`You are an autonomous software engineer fixing a bug in the repository in the current working directory.

Goal:
%s

Available tools:
%s

PROTOCOL:
- Each turn, think briefly about what you learned and what to do next, then END your reply with exactly one JSON object describing ONE tool call:
  {"steps": [{"tool": "ReadFileTool", "input": {"path": "path/to/file"}}]}
- Each step's "input" is an object with string keys and string values matching the tool's parameters.
- After each tool call I will reply with the tool result. You will see the whole conversation, so remember your earlier findings.
- The "tool" field is just a string in your JSON reply - do not make native function/tool calls.

HOW TO FIX A BUG:
1. Locate the relevant file (GrepTool, or ShellTool with grep -n / find).
2. ALWAYS call ReadFileTool on the file before editing so you see the exact current code (indentation included). Tool results are truncated to ~%d chars, so for large files find the line with "grep -n" and pass "start_line"/"end_line" to ReadFileTool.
3. Apply the fix with StrReplaceTool using input keys "path", "old_string" (exact current code, verbatim, unique in the file) and "new_string" (replacement with correct indentation).
   Example: {"steps": [{"tool": "StrReplaceTool", "input": {"path": "src/pkg/module.py", "old_string": "def is_valid(args):\n    return isinstance(args[0], Foo)", "new_string": "def is_valid(args):\n    if not args:\n        return False\n    return isinstance(args[0], Foo)"}}]}
4. If StrReplaceTool returns an error (not found / not unique / syntax error), ReadFileTool again and retry with a corrected edit.
5. Use WriteFileTool only for new files. Do not rewrite whole existing files.
6. Optionally reproduce or run the relevant tests with ShellTool to check your fix.
Do NOT modify test files; fix the source code so the failing tests pass. If an error trace is given, fix the exact line it points at.
Call DoneTool ONLY after StrReplaceTool or WriteFileTool has successfully changed a file: {"steps": [{"tool": "DoneTool", "input": {"reason": "..."}}]}`,
		goal, tr.FormatPrompt(), maxToolResultChars)
}

// historySize returns the total content length of messages.
func historySize(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

// trimMessages keeps the first (system/goal) message and the most recent
// messages totalling at most keep chars once the history exceeds trigger.
// Dropped turns are replaced by a short note listing the tools that were
// called, so the conversation stays well-formed (user/assistant alternation).
func trimMessages(msgs []llm.Message, trigger, keep int) []llm.Message {
	if len(msgs) < 4 || historySize(msgs) <= trigger {
		return msgs
	}
	// Walk back from the end, keeping whole messages until the budget is used.
	size, cut := 0, len(msgs)
	for cut > 1 && size+len(msgs[cut-1].Content) <= keep {
		size += len(msgs[cut-1].Content)
		cut--
	}
	// The kept tail must begin with an assistant message so that, after the
	// summary (a user message), roles still alternate.
	for cut < len(msgs) && msgs[cut].Role != "assistant" {
		cut++
	}
	if cut <= 2 || cut >= len(msgs) {
		return msgs
	}
	var summary strings.Builder
	summary.WriteString("[Earlier conversation summarized to save space. Steps taken so far:]\n")
	for _, m := range msgs[1:cut] {
		if m.Role != "assistant" {
			continue
		}
		var plan models.GoalPlan
		if json.Unmarshal([]byte(extractJSON(m.Content)), &plan) == nil && len(plan.Steps) > 0 {
			fmt.Fprintf(&summary, "- %s%s\n", plan.Steps[0].Tool, summarizeArgs(plan.Steps[0].Input))
		}
	}
	summary.WriteString("Re-read any file you need before editing it again.")
	// Merge the summary into the first message so roles keep alternating:
	// msgs[0] (user) -> tail starting with assistant.
	out := make([]llm.Message, 0, len(msgs)-cut+1)
	out = append(out, llm.Message{Role: "user", Content: msgs[0].Content + "\n\n" + summary.String()})
	out = append(out, msgs[cut:]...)
	return out
}

// nextStep asks the model for the next step given the conversation so far.
// It returns the raw response text and the parsed step. On a parse failure
// it returns the response with a non-nil error describing the problem, so
// the caller can feed the error back into the conversation.
func (a *Agent) nextStep(ctx context.Context, messages []llm.Message) (string, *models.GoalStep, error) {
	req := llm.Request{
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0.1,
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		tokens, err := a.router.Complete(ctx, llm.WorkloadDiagnose, req)
		if err != nil {
			lastErr = err
			continue
		}
		var b strings.Builder
		var streamErr error
		for tok := range tokens {
			if tok.Err != nil {
				streamErr = tok.Err
				break
			}
			b.WriteString(tok.Text)
		}
		if streamErr != nil {
			lastErr = streamErr
			continue
		}
		res := b.String()
		jsonStr := extractJSON(res)
		if jsonStr == "" {
			return res, nil, fmt.Errorf("no JSON object found in your reply")
		}
		var plan models.GoalPlan
		if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
			return res, nil, fmt.Errorf("invalid JSON: %v", err)
		}
		if len(plan.Steps) == 0 {
			return res, nil, fmt.Errorf(`empty "steps" array; you must return exactly one step`)
		}
		return res, &plan.Steps[0], nil
	}
	return "", nil, fmt.Errorf("LLM request failed after retries: %w", lastErr)
}

// DoGoal runs a ReAct loop as a single persistent conversation: the model
// sees its own earlier reasoning and every tool result, one step per turn.
func (a *Agent) DoGoal(ctx context.Context, goal string, opts RunOptions, out io.Writer) error {
	tr := tools.NewRegistry()
	tr.Register(&tools.ShellTool{})
	tr.Register(&tools.ReadFileTool{})
	tr.Register(&tools.WriteFileTool{})
	tr.Register(&tools.StrReplaceTool{})
	tr.Register(&tools.GrepTool{})
	tr.Register(&tools.GitTool{})
	tr.Register(&tools.DoneTool{})

	messages := []llm.Message{{Role: "user", Content: buildGoalSystemPrompt(goal, tr)}}
	editsMade := 0
	parseFailures := 0

	for i := 0; i < maxGoalIterations; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		messages = trimMessages(messages, historyTrimTrigger, historyKeepChars)

		resp, step, err := a.nextStep(ctx, messages)
		if step == nil {
			if resp == "" {
				return err // transport failure
			}
			parseFailures++
			if parseFailures > 5 {
				return fmt.Errorf("model failed to produce a valid step %d times: %w", parseFailures, err)
			}
			fmt.Fprintf(out, "[step %d/%d] unparseable reply: %v\n", i+1, maxGoalIterations, err)
			messages = append(messages,
				llm.Message{Role: "assistant", Content: resp},
				llm.Message{Role: "user", Content: fmt.Sprintf("Your last reply could not be used: %v. End your reply with exactly one JSON object like {\"steps\": [{\"tool\": \"...\", \"input\": {...}}]}.", err)})
			continue
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: resp})

		fmt.Fprintf(out, "[step %d/%d] Executing %s:%s\n", i+1, maxGoalIterations, step.Tool, summarizeArgs(step.Input))
		strInput := toStringMap(step.Input)

		if step.Tool == "DoneTool" {
			if editsMade == 0 && i < 3 {
				// Only block early exits; later DoneTool calls are allowed so the
				// model can't burn every iteration on rejected DoneTool calls.
				fmt.Fprintln(out, "  -> DoneTool rejected: no file modified yet (early exit blocked)")
				messages = append(messages, llm.Message{Role: "user", Content: "DoneTool REJECTED - you have not modified any file yet. Use ReadFileTool to see the exact code, then StrReplaceTool to apply the fix before calling DoneTool."})
				continue
			}
			if editsMade == 0 {
				fmt.Fprintln(out, "  -> Warning: DoneTool accepted with no file modifications")
			}
			fmt.Fprintln(out, "Goal achieved:", strInput["reason"])
			return nil
		}

		var outStr string
		var toolErr error
		if t := tr.Get(step.Tool); t == nil {
			toolErr = fmt.Errorf("unknown tool: %s", step.Tool)
		} else {
			if step.Tool == "ShellTool" && opts.ApprovalFn != nil && !opts.SkipPermissions {
				if !opts.ApprovalFn(strInput["command"], 0) {
					return fmt.Errorf("user rejected command: %s", strInput["command"])
				}
			}
			outStr, toolErr = t.Execute(ctx, strInput)
		}

		if toolErr != nil {
			outStr += fmt.Sprintf("\nError: %v", toolErr)
			fmt.Fprintf(out, "  -> %s FAILED: %v\n", step.Tool, toolErr)
		} else if step.Tool == "StrReplaceTool" || step.Tool == "WriteFileTool" {
			editsMade++
			fmt.Fprintf(out, "  -> %s OK: %s\n", step.Tool, strings.TrimSpace(outStr))
		}

		output := outStr
		if len(output) > maxToolResultChars {
			output = output[:maxToolResultChars] + "\n... (truncated)"
		}
		if strings.TrimSpace(output) == "" {
			output = "(no output)"
		}
		messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf("Tool result for %s (step %d/%d):\n%s", step.Tool, i+1, maxGoalIterations, output)})
	}

	fmt.Fprintf(out, "Reached iteration limit (%d) without DoneTool.\n", maxGoalIterations)
	return nil
}

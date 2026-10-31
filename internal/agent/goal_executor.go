package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
func buildGoalSystemPrompt(goal string, tr *tools.Registry, opts RunOptions) string {
	testRules := ""
	if opts.TestCmd != "" {
		testRules = "\n\nTESTING:\nAfter making a fix with StrReplaceTool, you SHOULD call RunTestsTool to verify your fix works before calling DoneTool. If tests still fail, read the error output and make additional fixes."
		if len(opts.FailingTests) > 0 {
			testRules += "\nTests that must pass: " + strings.Join(opts.FailingTests, ", ")
		}
	}
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
3. Apply the fix with StrReplaceTool using input keys "path", "old_string" (exact current code, verbatim, unique in the file — strip ReadFileTool's "N: " line-number prefixes) and "new_string" (replacement with correct indentation).
   Example: {"steps": [{"tool": "StrReplaceTool", "input": {"path": "src/pkg/module.py", "old_string": "def is_valid(args):\n    return isinstance(args[0], Foo)", "new_string": "def is_valid(args):\n    if not args:\n        return False\n    return isinstance(args[0], Foo)"}}]}
4. If StrReplaceTool returns an error (not found / not unique / syntax error), ReadFileTool again and retry with a corrected edit.
5. Use WriteFileTool only for new files. Do not rewrite whole existing files.
6. Optionally reproduce or run the relevant tests with ShellTool to check your fix.
Do NOT modify test files; fix the source code so the failing tests pass. If an error trace is given, fix the exact line it points at.
Call DoneTool ONLY after StrReplaceTool or WriteFileTool has successfully changed a file: {"steps": [{"tool": "DoneTool", "input": {"reason": "..."}}]}%s`,
		goal, tr.FormatPrompt(), maxToolResultChars, testRules)
}

// historySize returns the total content length of messages.
func historySize(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

// trimMessages applies observation masking (issue #50): once the history
// exceeds trigger chars, every "Tool result for ..." user message older than
// the most recent keep chars is replaced by a one-line stub. The first
// (goal) message and every assistant message (the reasoning chain) are kept
// intact, so roles still alternate and earlier findings are not lost.
func trimMessages(msgs []llm.Message, trigger, keep int) []llm.Message {
	if len(msgs) < 4 || historySize(msgs) <= trigger {
		return msgs
	}
	// Find where the protected recent tail begins.
	size, cut := 0, len(msgs)
	for cut > 1 && size+len(msgs[cut-1].Content) <= keep {
		size += len(msgs[cut-1].Content)
		cut--
	}
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i := 1; i < cut; i++ {
		m := out[i]
		if m.Role != "user" || !strings.HasPrefix(m.Content, "Tool result for ") {
			continue
		}
		stub := "[Tool result elided]"
		if i > 0 && out[i-1].Role == "assistant" {
			var plan models.GoalPlan
			if json.Unmarshal([]byte(extractJSON(out[i-1].Content)), &plan) == nil && len(plan.Steps) > 0 {
				arg := ""
				for _, k := range []string{"path", "pattern", "command"} {
					if v, ok := plan.Steps[0].Input[k]; ok {
						arg = fmt.Sprintf("%v", v)
						if len(arg) > 80 {
							arg = arg[:80] + "..."
						}
						break
					}
				}
				stub = fmt.Sprintf("[Tool result for %s(%s) — elided]", plan.Steps[0].Tool, arg)
			}
		}
		out[i] = llm.Message{Role: "user", Content: stub}
	}
	return out
}

// nextStep asks the model for the next step given the conversation so far.
// It returns the raw response text and the parsed step. On a parse failure
// it returns the response with a non-nil error describing the problem, so
// the caller can feed the error back into the conversation.
func (a *Agent) nextStep(ctx context.Context, messages []llm.Message, opts RunOptions) (string, *models.GoalStep, error) {
	req := llm.Request{
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0.1,
	}
	var lastErr error
	// Errors can arrive mid-stream (after the router already handed over the
	// stream), e.g. NVIDIA "Service temporarily overloaded", so back off
	// exponentially here too rather than giving up after ~4s.
	backoff := 2 * time.Second
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			if lastErr != nil {
				fmt.Fprintf(os.Stderr, "goal: LLM request failed (%v), retrying in %v (attempt %d/5)\n", lastErr, backoff, attempt+1)
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", nil, ctx.Err()
			}
			backoff *= 2
		}
		var tokens <-chan llm.Token
		var err error
		if opts.ModelConfig != nil && opts.ModelConfig.Provider != "" {
			tokens, err = a.router.CompleteWithConfig(ctx, llm.ModelConfig{Provider: opts.ModelConfig.Provider, Model: opts.ModelConfig.Model}, llm.WorkloadDiagnose, req)
		} else {
			tokens, err = a.router.Complete(ctx, llm.WorkloadDiagnose, req)
		}
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

// GoalSession is a resumable executor conversation. Passing it back to
// DoGoalSession continues the same conversation, so a retry keeps full memory
// of what earlier attempts did and why.
type GoalSession struct {
	Messages  []llm.Message
	EditsMade int
}

// DoGoal runs a fresh goal session (backward-compatible wrapper).
func (a *Agent) DoGoal(ctx context.Context, goal string, opts RunOptions, out io.Writer) error {
	_, err := a.DoGoalSession(ctx, goal, opts, out, nil)
	return err
}

// DoGoalSession runs a ReAct loop as a single persistent conversation: the
// model sees its own earlier reasoning and every tool result, one step per
// turn. If session is nil it starts fresh using goal; otherwise it resumes the
// session, appending goal as a new user message (e.g. the latest test failure).
// The returned session is always non-nil and can be passed back to resume.
func (a *Agent) DoGoalSession(ctx context.Context, goal string, opts RunOptions, out io.Writer, session *GoalSession) (*GoalSession, error) {
	tr := tools.NewRegistry()
	tr.Register(&tools.ShellTool{})
	tr.Register(&tools.ReadFileTool{})
	tr.Register(&tools.WriteFileTool{})
	tr.Register(&tools.StrReplaceTool{})
	tr.Register(&tools.GrepTool{})
	tr.Register(&tools.GitTool{})
	tr.Register(&tools.DoneTool{})
	if opts.TestCmd != "" {
		tr.Register(&tools.RunTestsTool{TestCmd: opts.TestCmd})
	}

	var messages []llm.Message
	editsMade := 0
	if session == nil || len(session.Messages) == 0 {
		messages = []llm.Message{{Role: "user", Content: buildGoalSystemPrompt(goal, tr, opts)}}
	} else {
		messages = append([]llm.Message(nil), session.Messages...)
		editsMade = session.EditsMade
		// Keep user/assistant alternation: merge into a trailing user message.
		if last := &messages[len(messages)-1]; last.Role == "user" {
			last.Content += "\n\n" + goal
		} else {
			messages = append(messages, llm.Message{Role: "user", Content: goal})
		}
	}
	sess := &GoalSession{}
	iterations := 0
	save := func() *GoalSession {
		sess.Messages, sess.EditsMade = messages, editsMade
		fmt.Fprintf(out, "[DoGoal summary] %d iterations, %d edits made\n", iterations, editsMade)
		return sess
	}
	parseFailures := 0
	// On resume the model already made edits; still require a new step
	// before DoneTool so it can't immediately quit.
	stepsThisRun := 0

	for i := 0; i < maxGoalIterations; i++ {
		iterations = i + 1
		if err := ctx.Err(); err != nil {
			return save(), err
		}
		messages = trimMessages(messages, historyTrimTrigger, historyKeepChars)

		resp, step, err := a.nextStep(ctx, messages, opts)
		if step == nil {
			if resp == "" {
				return save(), err // transport failure
			}
			parseFailures++
			if parseFailures > 5 {
				return save(), fmt.Errorf("model failed to produce a valid step %d times: %w", parseFailures, err)
			}
			fmt.Fprintf(out, "[step %d/%d] unparseable reply: %v\n", i+1, maxGoalIterations, err)
			messages = append(messages,
				llm.Message{Role: "assistant", Content: resp},
				llm.Message{Role: "user", Content: fmt.Sprintf("Your last reply could not be used: %v. End your reply with exactly one JSON object like {\"steps\": [{\"tool\": \"...\", \"input\": {...}}]}.", err)})
			continue
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: resp})
		stepsThisRun++

		fmt.Fprintf(out, "[step %d/%d] Executing %s:%s\n", i+1, maxGoalIterations, step.Tool, summarizeArgs(step.Input))
		strInput := toStringMap(step.Input)

		if step.Tool == "DoneTool" {
			if session != nil && stepsThisRun == 1 {
				fmt.Fprintln(out, "  -> DoneTool rejected: resumed session must act on the new failure first")
				messages = append(messages, llm.Message{Role: "user", Content: "DoneTool REJECTED - the tests still fail with your current patch. Read the failure above, inspect the code, and fix it (then run the tests if RunTestsTool is available)."})
				continue
			}
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
			return save(), nil
		}

		var outStr string
		var toolErr error
		if t := tr.Get(step.Tool); t == nil {
			toolErr = fmt.Errorf("unknown tool: %s", step.Tool)
		} else {
			if step.Tool == "ShellTool" && opts.ApprovalFn != nil && !opts.SkipPermissions {
				if !opts.ApprovalFn(strInput["command"], 0) {
					return save(), fmt.Errorf("user rejected command: %s", strInput["command"])
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
	return save(), nil
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sagar0163/nebula/internal/tools"
	"github.com/sagar0163/nebula/internal/profile"
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

func (a *Agent) DoGoal(ctx context.Context, goal string, opts RunOptions, out io.Writer) error {
	tr := tools.NewRegistry()
	tr.Register(&tools.ShellTool{})
	tr.Register(&tools.ReadFileTool{})
	tr.Register(&tools.WriteFileTool{})
	tr.Register(&tools.StrReplaceTool{})
	tr.Register(&tools.GrepTool{})
	tr.Register(&tools.GitTool{})
	tr.Register(&tools.DoneTool{})

	planner := NewGoalPlanner(a.router, tr)

	contextData := "Started goal execution."
	editsMade := 0

	for i := 0; i < maxGoalIterations; i++ {
		dir, _ := os.Getwd()
		userProf := profile.GetUserProfile()
		projProf := profile.GetProjectProfile(dir)
		steps, err := planner.Plan(ctx, goal, contextData, userProf, projProf)
		if err != nil {
			return err
		}

		if len(steps) == 0 {
			if editsMade == 0 {
				return fmt.Errorf("planner returned 0 steps with no edits made (iteration %d)", i+1)
			}
			fmt.Fprintln(out, "Goal achieved: planner returned no further steps.")
			return nil
		}

		// Execute only ONE step per iteration so the model can react to its result.
		{
			step := steps[0]
			fmt.Fprintf(out, "[step %d/%d] Executing %s:%s\n", i+1, maxGoalIterations, step.Tool, summarizeArgs(step.Input))
			strInput := toStringMap(step.Input)

			var outStr string
			var toolErr error

			t := tr.Get(step.Tool)
			if step.Tool == "DoneTool" {
				if editsMade == 0 && i < 3 {
					// Only block early exits; later DoneTool calls are allowed so the
					// model can't burn every iteration on rejected DoneTool calls.
					fmt.Fprintln(out, "  -> DoneTool rejected: no file modified yet (early exit blocked)")
					contextData += fmt.Sprintf("\nStep %d: DoneTool REJECTED - you have not modified any file yet. Use ReadFileTool to see the exact code, then StrReplaceTool to apply the fix before calling DoneTool.\n", i+1)
					continue
				}
				if editsMade == 0 {
					fmt.Fprintln(out, "  -> Warning: DoneTool accepted with no file modifications")
				}
				fmt.Fprintln(out, "Goal achieved:", strInput["reason"])
				return nil
			}

			if t == nil {
				toolErr = fmt.Errorf("unknown tool: %s", step.Tool)
			} else {
				// Intercept ShellTool for approval
				if step.Tool == "ShellTool" && opts.ApprovalFn != nil && !opts.SkipPermissions {
					if !opts.ApprovalFn(strInput["command"], 0) {
						return fmt.Errorf("user rejected command: %s", strInput["command"])
					}
				}
				toolOut, tErr := t.Execute(ctx, strInput)
				toolErr = tErr
				outStr = toolOut
			}

			if toolErr != nil {
				outStr += fmt.Sprintf("\nError: %v", toolErr)
				fmt.Fprintf(out, "  -> %s FAILED: %v\n", step.Tool, toolErr)
			} else if step.Tool == "StrReplaceTool" || step.Tool == "WriteFileTool" {
				editsMade++
				fmt.Fprintf(out, "  -> %s OK: %s\n", step.Tool, strings.TrimSpace(outStr))
			}

			// Feed result back for the next planning call
			output := outStr
			if len(output) > 8000 {
				output = output[:8000] + "\n... (truncated)"
			}
			contextData += fmt.Sprintf("\nStep %d: %s %v\nResult (%s):\n%s\n", i+1, step.Tool, step.Input, step.Tool, output)
		}
	}

	fmt.Fprintf(out, "Reached iteration limit (%d) without DoneTool.\n", maxGoalIterations)
	return nil
}


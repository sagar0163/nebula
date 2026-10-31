package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sagar0163/nebula/internal/tools"
	"github.com/sagar0163/nebula/internal/profile"
)

const maxGoalIterations = 25

// summarizeArgs renders tool args compactly for logs so edits are visible.
func summarizeArgs(in map[string]string) string {
	var b strings.Builder
	for _, k := range []string{"path", "command", "pattern", "old_string", "new_string", "content", "reason"} {
		v, ok := in[k]
		if !ok {
			continue
		}
		v = strings.ReplaceAll(v, "\n", "\\n")
		if len(v) > 200 {
			v = v[:200] + "..."
		}
		fmt.Fprintf(&b, " %s=%q", k, v)
	}
	return b.String()
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
			fmt.Fprintln(out, "Goal achieved or no further steps planned.")
			return nil
		}

		// Execute only ONE step per iteration so the model can react to its result.
		{
			step := steps[0]
			fmt.Fprintf(out, "[step %d/%d] Executing %s:%s\n", i+1, maxGoalIterations, step.Tool, summarizeArgs(step.Input))

			var outStr string
			var toolErr error

			t := tr.Get(step.Tool)
			if step.Tool == "DoneTool" {
				if editsMade == 0 {
					fmt.Fprintln(out, "  -> DoneTool rejected: no file has been modified yet")
					contextData += fmt.Sprintf("\nStep %d: DoneTool REJECTED - you have not modified any file yet. Use ReadFileTool to see the exact code, then StrReplaceTool to apply the fix before calling DoneTool.\n", i+1)
					continue
				}
				fmt.Fprintln(out, "Goal achieved:", step.Input["reason"])
				return nil
			}

			if t == nil {
				toolErr = fmt.Errorf("unknown tool: %s", step.Tool)
			} else {
				// Intercept ShellTool for approval
				if step.Tool == "ShellTool" && opts.ApprovalFn != nil && !opts.SkipPermissions {
					if !opts.ApprovalFn(step.Input["command"], 0) {
						return fmt.Errorf("user rejected command: %s", step.Input["command"])
					}
				}
				toolOut, tErr := t.Execute(ctx, step.Input)
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
			if len(output) > 3000 {
				output = output[:3000] + "\n... (truncated)"
			}
			contextData += fmt.Sprintf("\nStep %d: %s %v\nResult (%s):\n%s\n", i+1, step.Tool, step.Input, step.Tool, output)
		}
	}

	fmt.Fprintf(out, "Reached iteration limit (%d) without DoneTool.\n", maxGoalIterations)
	return nil
}
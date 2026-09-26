package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"github.com/sagar0163/nebula/internal/tools"
	"github.com/sagar0163/nebula/internal/profile"
	
)

func (a *Agent) DoGoal(ctx context.Context, goal string, opts RunOptions, out io.Writer) error {
		tr := tools.NewRegistry()
	tr.Register(&tools.ShellTool{})
	tr.Register(&tools.ReadFileTool{})
	tr.Register(&tools.WriteFileTool{})
	tr.Register(&tools.GrepTool{})
	tr.Register(&tools.GitTool{})

	planner := NewGoalPlanner(a.router, tr)
	
	contextData := "Started goal execution."
	
	for i := 0; i < 15; i++ { // limit iterations to 15
		dir, _ := os.Getwd()
		codebase := IndexCodebase(dir)
		userProf := profile.GetUserProfile()
		projProf := profile.GetProjectProfile(dir)
		steps, err := planner.Plan(ctx, goal, contextData, codebase, userProf, projProf)
		if err != nil {
			return err
		}

		if len(steps) == 0 {
			fmt.Fprintln(out, "Goal achieved or no further steps planned.")
			return nil
		}

		for _, step := range steps {
			fmt.Fprintf(out, "Executing %s...\n", step.Tool)
			
			var outStr string
			var toolErr error

			t := tr.Get(step.Tool)
			if step.Tool == "DoneTool" {
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
			}

			// Add to context
			contextData += fmt.Sprintf("\nStep: %s\nResult:\n%s\n", step.Tool, outStr)
		}
		
		// Wait, a ReAct style loop uses the context to plan the next steps.
		// If it reaches here, it has executed the plan. 
		// We could do a verify step here or just ask the planner if it's done.
	}
	
	return nil
}

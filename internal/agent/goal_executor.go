package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"

)

func (a *Agent) DoGoal(ctx context.Context, goal string, opts RunOptions) error {
	planner := NewGoalPlanner(a.router)
	
	contextData := "Started goal execution."
	
	for i := 0; i < 5; i++ { // limit iterations
		steps, err := planner.Plan(ctx, goal, contextData)
		if err != nil {
			return err
		}

		if len(steps) == 0 {
			fmt.Println("Goal achieved or no further steps planned.")
			return nil
		}

		for _, step := range steps {
			fmt.Printf("Executing %s...\n", step.Tool)
			
			var out string
			var toolErr error

			switch step.Tool {
			case "ShellTool":
				cmdStr := step.Input["command"]
				if opts.ApprovalFn != nil && !opts.SkipPermissions {
					// We use a basic risk for now
					if !opts.ApprovalFn(cmdStr, 0) {
						return fmt.Errorf("user rejected command: %s", cmdStr)
					}
				}
				cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
				outBytes, err := cmd.CombinedOutput()
				out = string(outBytes)
				toolErr = err
			case "ReadFileTool":
				path := step.Input["path"]
				content, err := os.ReadFile(path)
				out = string(content)
				toolErr = err
			case "WriteFileTool":
				path := step.Input["path"]
				content := step.Input["content"]
				toolErr = os.WriteFile(path, []byte(content), 0644)
				out = "File written successfully."
			default:
				toolErr = fmt.Errorf("unknown tool: %s", step.Tool)
			}

			if toolErr != nil {
				out += fmt.Sprintf("\nError: %v", toolErr)
			}

			// Add to context
			contextData += fmt.Sprintf("\nStep: %s\nResult:\n%s\n", step.Tool, out)
		}
		
		// Wait, a ReAct style loop uses the context to plan the next steps.
		// If it reaches here, it has executed the plan. 
		// We could do a verify step here or just ask the planner if it's done.
	}
	
	return nil
}

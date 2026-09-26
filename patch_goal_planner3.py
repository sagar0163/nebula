import re

with open("internal/agent/goal_planner.go", "r") as f:
    content = f.read()

bad_prompt = """Available tools:
- ShellTool (input: "command")
- ReadFileTool (input: "path")
- WriteFileTool (input: "path", "content")
- SearchTool (input: "query")
- WebFetchTool (input: "url")"""

good_prompt = "%s"

content = content.replace(bad_prompt, good_prompt)
content = content.replace("goal, contextData)", "goal, p.tools.FormatPrompt(), contextData)")
content = content.replace("type GoalPlanner struct {\n\trouter *llm.Router\n}", "type GoalPlanner struct {\n\trouter *llm.Router\n\ttools  *tools.Registry\n}")
content = content.replace("func NewGoalPlanner(router *llm.Router) *GoalPlanner {\n\treturn &GoalPlanner{router: router}\n}", "func NewGoalPlanner(router *llm.Router, tr *tools.Registry) *GoalPlanner {\n\treturn &GoalPlanner{router: router, tools: tr}\n}")
content = content.replace('"github.com/sagar0163/nebula/internal/llm"\n\t"github.com/sagar0163/nebula/internal/models"', '"github.com/sagar0163/nebula/internal/llm"\n\t"github.com/sagar0163/nebula/internal/models"\n\t"github.com/sagar0163/nebula/internal/tools"')

with open("internal/agent/goal_planner.go", "w") as f:
    f.write(content)

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace('"github.com/sagar0163/nebula/internal/models"', '"github.com/sagar0163/nebula/internal/models"\n\t"github.com/sagar0163/nebula/internal/tools"')

old_planner = "planner := NewGoalPlanner(a.router)"
new_planner = """	tr := tools.NewRegistry()
	tr.Register(&tools.ShellTool{})
	tr.Register(&tools.ReadFileTool{})
	tr.Register(&tools.WriteFileTool{})
	tr.Register(&tools.GrepTool{})
	tr.Register(&tools.GitTool{})

	planner := NewGoalPlanner(a.router, tr)"""
content = content.replace(old_planner, new_planner)

switch_old = """			switch step.Tool {
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
			}"""
switch_new = """			t := tr.Get(step.Tool)
			if t == nil {
				toolErr = fmt.Errorf("unknown tool: %s", step.Tool)
			} else {
				// Intercept ShellTool for approval
				if step.Tool == "ShellTool" && opts.ApprovalFn != nil && !opts.SkipPermissions {
					if !opts.ApprovalFn(step.Input["command"], 0) {
						return fmt.Errorf("user rejected command: %s", step.Input["command"])
					}
				}
				out, toolErr = t.Execute(ctx, step.Input)
			}"""
content = content.replace(switch_old, switch_new)

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)

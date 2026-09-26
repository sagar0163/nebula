import re

with open("internal/agent/goal_planner.go", "r") as f:
    content = f.read()

prompt_old = """Context:
%s"""

prompt_new = """Codebase Summary:
%s

Context:
%s"""

content = content.replace(prompt_old, prompt_new)
content = content.replace("func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string) ([]models.GoalStep, error) {", "func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string, codebase CodebaseIndex) ([]models.GoalStep, error) {")
content = content.replace("goal, p.tools.FormatPrompt(), contextData)", "goal, p.tools.FormatPrompt(), codebase.Summary(), contextData)")

with open("internal/agent/goal_planner.go", "w") as f:
    f.write(content)

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace("steps, err := planner.Plan(ctx, goal, contextData)", 'dir, _ := os.Getwd()\n\t\tcodebase := IndexCodebase(dir)\n\t\tsteps, err := planner.Plan(ctx, goal, contextData, codebase)')

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)

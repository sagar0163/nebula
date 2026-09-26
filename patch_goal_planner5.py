import re

with open("internal/agent/goal_planner.go", "r") as f:
    content = f.read()

prompt_old = """Codebase Summary:
%s"""

prompt_new = """Codebase Summary:
%s

User Profile:
Name: %s
Email: %s
Preferences: %s

Project Profile:
Repo: %s
Readme: %s"""

content = content.replace(prompt_old, prompt_new)
content = content.replace("func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string, codebase CodebaseIndex) ([]models.GoalStep, error) {", 'func (p *GoalPlanner) Plan(ctx context.Context, goal string, contextData string, codebase CodebaseIndex, user profile.UserProfile, proj profile.ProjectProfile) ([]models.GoalStep, error) {')
content = content.replace("goal, p.tools.FormatPrompt(), codebase.Summary(), contextData)", "goal, p.tools.FormatPrompt(), codebase.Summary(), user.Name, user.Email, user.LanguagePreferences, proj.RepoURL, proj.ReadmeIntro, contextData)")

if '"github.com/sagar0163/nebula/internal/profile"' not in content:
    content = content.replace('"github.com/sagar0163/nebula/internal/tools"', '"github.com/sagar0163/nebula/internal/tools"\n\t"github.com/sagar0163/nebula/internal/profile"')

with open("internal/agent/goal_planner.go", "w") as f:
    f.write(content)

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace('steps, err := planner.Plan(ctx, goal, contextData, codebase)', 'userProf := profile.GetUserProfile()\n\t\tprojProf := profile.GetProjectProfile(dir)\n\t\tsteps, err := planner.Plan(ctx, goal, contextData, codebase, userProf, projProf)')

if '"github.com/sagar0163/nebula/internal/profile"' not in content:
    content = content.replace('"github.com/sagar0163/nebula/internal/tools"', '"github.com/sagar0163/nebula/internal/tools"\n\t"github.com/sagar0163/nebula/internal/profile"')

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)

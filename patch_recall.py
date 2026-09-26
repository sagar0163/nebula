import re

with open("internal/agent/planner.go", "r") as f:
    content = f.read()

content = content.replace("if recalled, err := p.recallPattern(ctx, failCmd, output); err == nil && recalled != nil {", "if recalled, err := p.recallPattern(ctx, failCmd, output, len(history)); err == nil && recalled != nil {")

old_recall = "func (p *Planner) recallPattern(ctx context.Context, failCmd, failOutput string) (*models.HealSuggestion, error) {"
new_recall = """func (p *Planner) recallPattern(ctx context.Context, failCmd, failOutput string, attemptCount int) (*models.HealSuggestion, error) {
	buildSuggestion := func(pattern *models.Pattern, source string) *models.HealSuggestion {
		fix := pattern.FixCmd
		if pattern.FixChain != "" {
			var chain []string
			if err := json.Unmarshal([]byte(pattern.FixChain), &chain); err == nil && len(chain) > 0 {
				if attemptCount < len(chain) {
					fix = chain[attemptCount]
					source += " (partial step)"
				}
			}
		}
		return &models.HealSuggestion{
			OriginalCmd: failCmd,
			FixCmd:      fix,
			Explanation: source,
		}
	}
"""

content = content.replace(old_recall, new_recall)
content = content.replace("return &models.HealSuggestion{\n\t\t\tOriginalCmd: failCmd,\n\t\t\tFixCmd:      pattern.FixCmd,\n\t\t\tExplanation: \"recalled from exact past fix\",\n\t\t}, nil", "return buildSuggestion(pattern, \"recalled from exact past fix\"), nil")

content = content.replace("return &models.HealSuggestion{\n\t\t\t\t\tOriginalCmd: failCmd,\n\t\t\t\t\tFixCmd:      best.FixCmd,\n\t\t\t\t\tExplanation: \"recalled from similar past fix (keyword match)\",\n\t\t\t\t}, nil", "return buildSuggestion(best, \"recalled from similar past fix (keyword match)\"), nil")

with open("internal/agent/planner.go", "w") as f:
    f.write(content)


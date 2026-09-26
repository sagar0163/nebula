import re

with open("internal/agent/executor.go", "r") as f:
    content = f.read()

old_learn = """func (e *Executor) LearnPattern(ctx context.Context, failCmd, failOutput, fixCmd string) error {
	return e.store.SavePattern(ctx, &models.Pattern{
		FailCmd:     failCmd,
		FailOutput:  failOutput,
		FixCmd:      fixCmd,
		SuccessRate: 1.0,
		UseCount:    1,
		Embedding:   nil,
	})
}"""

new_learn = """func (e *Executor) LearnPattern(ctx context.Context, failCmd, failOutput, fixCmd string, history []models.TurnRecord) error {
	var fixChainStr string
	if len(history) > 0 {
		chain := make([]string, 0, len(history)+1)
		for _, h := range history {
			chain = append(chain, h.FixCmd)
		}
		chain = append(chain, fixCmd)
		b, _ := json.Marshal(chain)
		fixChainStr = string(b)
	}

	return e.store.SavePattern(ctx, &models.Pattern{
		FailCmd:     failCmd,
		FailOutput:  failOutput,
		FixCmd:      fixCmd,
		SuccessRate: 1.0,
		UseCount:    1,
		Embedding:   nil,
		FixChain:    fixChainStr,
	})
}"""

content = content.replace("encoding/json", "placeholder") # if it was there
content = 'import "encoding/json"\n' + content.replace('import "encoding/json"\n', "") # Ensure json is imported
content = content.replace(old_learn, new_learn)

with open("internal/agent/executor.go", "w") as f:
    f.write(content)

with open("internal/agent/agent.go", "r") as f:
    content = f.read()

content = content.replace("if err := a.executor.LearnPattern(ctx, suggestion.OriginalCmd, string(cmdResult.Stdout), suggestion.FixCmd); err != nil {", "if err := a.executor.LearnPattern(ctx, suggestion.OriginalCmd, string(cmdResult.Stdout), suggestion.FixCmd, history); err != nil {")

with open("internal/agent/agent.go", "w") as f:
    f.write(content)


import re

with open("internal/agent/executor.go", "r") as f:
    content = f.read()

old_logic = """	if !approvalFn(suggestion.FixCmd, safety.Classify(suggestion.FixCmd)) {
		return nil, nil
	}"""

new_logic = """	risk := safety.Classify(suggestion.FixCmd)
	promptCmd := suggestion.FixCmd
	if suggestion.Confidence > 0 {
		promptCmd = fmt.Sprintf("Fix suggestion (confidence: %.0f%%): %s", suggestion.Confidence*100, suggestion.FixCmd)
	}

	needsApproval := true
	if suggestion.Confidence >= 0.85 && risk <= safety.RiskLow {
		needsApproval = false
	} else if suggestion.Confidence < 0.6 {
		needsApproval = true
	}

	if needsApproval {
		if !approvalFn(promptCmd, risk) {
			return nil, nil
		}
	}"""

content = content.replace(old_logic, new_logic)

with open("internal/agent/executor.go", "w") as f:
    f.write(content)


import re

with open("internal/agent/planner.go", "r") as f:
    content = f.read()

# Fix parseSuggestion logic
parse_suggest_old = """	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && parsed.Fix != "" {
		return &models.HealSuggestion{
			OriginalCmd: originalCmd,
			FixCmd:      parsed.Fix,
			Explanation: parsed.Explanation,
			Reasoning:   parsed.Reasoning,
			Confidence:  parsed.Confidence,
		}
	}"""
parse_suggest_new = """	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && parsed.Fix != "" {
		conf := parsed.Confidence
		if conf == 0 {
			if parsed.Reasoning != "" {
				conf = 0.85
			} else {
				conf = 0.7
			}
		}
		return &models.HealSuggestion{
			OriginalCmd: originalCmd,
			FixCmd:      parsed.Fix,
			Explanation: parsed.Explanation,
			Reasoning:   parsed.Reasoning,
			Confidence:  conf,
		}
	}"""
content = content.replace(parse_suggest_old, parse_suggest_new)

parse_fallback_old = """	return &models.HealSuggestion{
		OriginalCmd: originalCmd,
		FixCmd:      fix,
		Explanation: explanation,
	}
}"""
parse_fallback_new = """	return &models.HealSuggestion{
		OriginalCmd: originalCmd,
		FixCmd:      fix,
		Explanation: explanation,
		Confidence:  0.7,
	}
}"""
content = content.replace(parse_fallback_old, parse_fallback_new)

# Fix recallPattern exact match confidence
exact_match_old = 'return buildSuggestion(pattern, "recalled from exact past fix"), nil'
exact_match_new = 's := buildSuggestion(pattern, "recalled from exact past fix")\n\t\ts.Confidence = 0.95\n\t\treturn s, nil'
content = content.replace(exact_match_old, exact_match_new)

# Fix recallPattern keyword match confidence
keyword_match_old = 'return buildSuggestion(best, "recalled from similar past fix (keyword match)"), nil'
keyword_match_new = 's := buildSuggestion(best, "recalled from similar past fix (keyword match)")\n\t\t\t\ts.Confidence = 0.5\n\t\t\t\treturn s, nil'
content = content.replace(keyword_match_old, keyword_match_new)

with open("internal/agent/planner.go", "w") as f:
    f.write(content)


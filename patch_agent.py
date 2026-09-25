import re

with open("internal/agent/agent.go", "r") as f:
    content = f.read()

# Replace step 5
old_step_5 = """	// 5. On failure, attempt healing.
	if cmdResult.ExitCode != 0 {
		outHash := sha256.Sum256(append([]byte(raw), cmdResult.Stdout...))
		fingerprint := fmt.Sprintf("%x", outHash[:8])

		a.doomMu.Lock()
		count := a.doomLoopCounts[fingerprint]
		a.doomLoopCounts[fingerprint] = count + 1
		a.doomMu.Unlock()

		result.DoomLoopCount = count + 1
		if count >= 3 {
			return result, fmt.Errorf("healing loop detected after 3 attempts — manual intervention required")
		}

		suggestion, err := a.planner.Plan(ctx, raw, string(cmdResult.Stdout), a.harness.Transcript())
		if err != nil {
			return result, nil // best-effort: return without healing
		}

		if suggestion != nil {
			approved := false
			wrapperFn := func(cmd string, risk safety.Risk) bool {
				approved = opts.ApprovalFn(cmd, risk)
				return approved
			}
			err := a.executor.Execute(ctx, suggestion, string(cmdResult.Stdout), wrapperFn)
			if err == nil && approved {
				result.Healed = true
				result.HealApply = suggestion
			}
		}
	}"""

new_step_5 = """	// 5. On failure, attempt multi-turn healing.
	maxTurns := 3
	history := []models.TurnRecord{}

	for cmdResult.ExitCode != 0 && len(history) < maxTurns {
		outHash := sha256.Sum256(append([]byte(raw), cmdResult.Stdout...))
		fingerprint := fmt.Sprintf("%x", outHash[:8])

		a.doomMu.Lock()
		count := a.doomLoopCounts[fingerprint]
		if len(history) == 0 {
			a.doomLoopCounts[fingerprint] = count + 1
			result.DoomLoopCount = count + 1
		}
		a.doomMu.Unlock()

		if count >= 3 {
			return result, fmt.Errorf("healing loop detected after 3 attempts — manual intervention required")
		}

		suggestion, err := a.planner.Plan(ctx, raw, string(cmdResult.Stdout), a.harness.Transcript(), history)
		if err != nil {
			return result, nil // best-effort: return without healing
		}

		if suggestion == nil {
			break
		}

		approved := false
		wrapperFn := func(cmd string, risk safety.Risk) bool {
			approved = opts.ApprovalFn(cmd, risk)
			return approved
		}

		fixResult, err := a.executor.Execute(ctx, suggestion, string(cmdResult.Stdout), wrapperFn)
		if err != nil || !approved {
			break
		}

		if fixResult != nil && fixResult.ExitCode == 0 {
			result.Healed = true
			result.HealApply = suggestion
			break
		} else if fixResult != nil && fixResult.ExitCode != 0 {
			newOutput := string(fixResult.Stdout)
			if newOutput == string(cmdResult.Stdout) {
				// Avoid infinite loop if same exact output
				break
			}
			history = append(history, models.TurnRecord{
				FixCmd:   suggestion.FixCmd,
				Output:   newOutput,
				ExitCode: fixResult.ExitCode,
			})
			cmdResult = fixResult
		}
	}"""

content = content.replace(old_step_5, new_step_5)

with open("internal/agent/agent.go", "w") as f:
    f.write(content)

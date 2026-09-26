import re

with open("internal/agent/agent.go", "r") as f:
    content = f.read()

progress_func = """
// levenshtein computes the edit distance between two strings
func levenshtein(s, t string) int {
	if len(s) == 0 {
		return len(t)
	}
	if len(t) == 0 {
		return len(s)
	}
	d := make([][]int, len(s)+1)
	for i := range d {
		d[i] = make([]int, len(t)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for j := 1; j <= len(t); j++ {
		for i := 1; i <= len(s); i++ {
			if s[i-1] == t[j-1] {
				d[i][j] = d[i-1][j-1]
			} else {
				min := d[i-1][j] + 1
				if d[i][j-1]+1 < min {
					min = d[i][j-1] + 1
				}
				if d[i-1][j-1]+1 < min {
					min = d[i-1][j-1] + 1
				}
				d[i][j] = min
			}
		}
	}
	return d[len(s)][len(t)]
}

func progressCheck(prevExit int, prevOut string, newExit int, newOut string) bool {
	if prevExit != newExit {
		return true
	}
	// length difference check (> 10%)
	diff := len(prevOut) - len(newOut)
	if diff < 0 {
		diff = -diff
	}
	maxLen := len(prevOut)
	if len(newOut) > maxLen {
		maxLen = len(newOut)
	}
	if maxLen > 0 && float64(diff)/float64(maxLen) > 0.10 {
		return true
	}

	// Keyword check (error, panic, fatal)
	kwds := []string{"error", "panic", "fatal", "failed"}
	prevOutLower := strings.ToLower(prevOut)
	newOutLower := strings.ToLower(newOut)
	for _, kw := range kwds {
		if strings.Contains(prevOutLower, kw) != strings.Contains(newOutLower, kw) {
			return true
		}
	}
	
	// Edit distance difference > 10%
	// Optimization: if strings are > 1000 chars, only check first 1000 to avoid O(N^2) explosion
	s1 := prevOut
	if len(s1) > 1000 {
		s1 = s1[:1000]
	}
	s2 := newOut
	if len(s2) > 1000 {
		s2 = s2[:1000]
	}
	
	dist := levenshtein(s1, s2)
	maxL := len(s1)
	if len(s2) > maxL {
		maxL = len(s2)
	}
	if maxL > 0 && float64(dist)/float64(maxL) > 0.10 {
		return true
	}

	return false
}
"""

if "progressCheck" not in content:
    content = content.replace("func (a *Agent) Ask(", progress_func + "\nfunc (a *Agent) Ask(")

loop_pattern = re.compile(r'maxTurns := 3\n\thistory := \[\]models\.TurnRecord\{\}\n\n\tfor cmdResult\.ExitCode != 0 && len\(history\) < maxTurns \{')
new_loop = """	maxTurns := 6
	history := []models.TurnRecord{}
	madeProgress := true // Start true to allow first 3 turns unconditionally

	for cmdResult.ExitCode != 0 && len(history) < maxTurns {
		if len(history) >= 3 && !madeProgress {
			// Stop early if no progress was made after turn 3
			break
		}

		outHash := sha256.Sum256(append([]byte(raw), cmdResult.Stdout...))
		// Update doom-loop fingerprint to use turn count to avoid interference
		fingerprint := fmt.Sprintf("%x-%d", outHash[:8], len(history))"""

content = re.sub(r'maxTurns := 3\n\thistory := \[\]models\.TurnRecord\{\}\n\n\tfor cmdResult\.ExitCode != 0 && len\(history\) < maxTurns \{\n\t\toutHash := sha256\.Sum256\(append\(\[\]byte\(raw\), cmdResult\.Stdout\.\.\.\)\)\n\t\tfingerprint := fmt\.Sprintf\("%x", outHash\[:8\]\)', new_loop, content)


progress_tracking = """				newOutput := string(verifyResult.Stdout)
				if newOutput == string(cmdResult.Stdout) {
					madeProgress = false
					break
				}
				madeProgress = progressCheck(cmdResult.ExitCode, string(cmdResult.Stdout), verifyResult.ExitCode, newOutput)
				history = append(history, models.TurnRecord{"""

content = content.replace("""				newOutput := string(verifyResult.Stdout)
				if newOutput == string(cmdResult.Stdout) {
					break
				}
				history = append(history, models.TurnRecord{""", progress_tracking)

progress_tracking_2 = """			newOutput := string(fixResult.Stdout)
			if newOutput == string(cmdResult.Stdout) {
				// Avoid infinite loop if same exact output
				madeProgress = false
				break
			}
			madeProgress = progressCheck(cmdResult.ExitCode, string(cmdResult.Stdout), fixResult.ExitCode, newOutput)
			history = append(history, models.TurnRecord{"""

content = content.replace("""			newOutput := string(fixResult.Stdout)
			if newOutput == string(cmdResult.Stdout) {
				// Avoid infinite loop if same exact output
				break
			}
			history = append(history, models.TurnRecord{""", progress_tracking_2)


with open("internal/agent/agent.go", "w") as f:
    f.write(content)

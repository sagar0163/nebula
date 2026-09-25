package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var errorRegex = regexp.MustCompile(`(?i)error:|fatal:|panic:|FAIL|undefined|not found|cannot|failed`)

// SummarizeOutput compresses long command outputs.
// If output is <= 4096 bytes, it returns it unchanged.
// Otherwise, it keeps the first 512 bytes, the last 512 bytes, and any lines matching error keywords,
// joining them with omitted line markers.
func SummarizeOutput(stdout string) string {
	if len(stdout) <= 4096 {
		return stdout
	}

	lines := strings.Split(stdout, "\n")
	keep := make(map[int]bool)

	// Keep first 512 bytes worth of lines
	headBytes := 0
	for i, line := range lines {
		if headBytes > 512 && i > 0 {
			break
		}
		keep[i] = true
		headBytes += len(line) + 1
	}

	// Keep last 512 bytes worth of lines
	tailBytes := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if tailBytes > 512 && i < len(lines)-1 {
			break
		}
		keep[i] = true
		tailBytes += len(lines[i]) + 1
	}

	// Keep lines matching the error regex
	for i, line := range lines {
		if keep[i] {
			continue
		}
		if errorRegex.MatchString(line) {
			keep[i] = true
		}
	}

	// Collect and sort kept indices
	var indices []int
	for i := range keep {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	var result strings.Builder
	lastIdx := -1

	for _, idx := range indices {
		if lastIdx != -1 && idx > lastIdx+1 {
			omitted := idx - lastIdx - 1
			result.WriteString(fmt.Sprintf("\n[... %d lines omitted ...]\n\n", omitted))
		} else if lastIdx != -1 {
			result.WriteString("\n")
		}
		result.WriteString(lines[idx])
		lastIdx = idx
	}

	return result.String()
}

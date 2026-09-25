package pty

import "regexp"

var ansiRegexp = regexp.MustCompile(`(?i)\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[PX^_][^\x1b]*\x1b\\|\x1b[@-Z\\-_]`)

// StripANSI removes all ANSI escape sequences (colors, bold, cursor movements,
// OSC sequences, clear screen, etc.) from the input string, returning clean plaintext.
func StripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

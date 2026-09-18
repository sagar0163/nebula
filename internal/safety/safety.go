package safety

import (
	"strings"
)

// Risk is the assessed danger level of a command.
type Risk int

const (
	RiskSafe      Risk = iota // read-only, no side effects
	RiskLow                   // minor mutations (e.g. git add)
	RiskMedium                // significant mutations (e.g. git push)
	RiskHigh                  // destructive or hard to reverse
	RiskDangerous             // system-level, bypasses review
)

// Decision is the policy outcome for a command.
type Decision int

const (
	DecisionAllow Decision = iota
	DecisionAsk
	DecisionDeny
)

// dangerousVectors are commands that can execute arbitrary code
// and bypass per-command review — always deny regardless of rules.
var dangerousVectors = []string{
	"bash -c", "sh -c", "zsh -c", "fish -c",
	"python -c", "python3 -c",
	"node -e", "node --eval",
	"ruby -e", "perl -e",
	"env ",
}

// destructivePatterns are commands that are high-risk by default.
var destructivePatterns = []string{
	"rm -rf", "rm -f",
	"dd if=", "mkfs",
	"chmod 777", "chmod -R 777",
	"> /dev/", "truncate",
	"sudo ", "su ",
	":(){:|:&};:", // fork bomb
}

// readOnlyPrefixes are commands considered safe without side effects.
var readOnlyPrefixes = []string{
	"ls", "cat", "head", "tail", "grep", "find", "echo",
	"pwd", "whoami", "id", "uname", "df", "du", "ps",
	"git status", "git log", "git diff", "git show",
	"docker ps", "docker images",
	"kubectl get", "kubectl describe",
}

// Classify assesses the risk level of a raw command string.
// It is intentionally conservative: unknown commands are RiskMedium.
func Classify(cmd string) Risk {
	cmd = strings.TrimSpace(cmd)

	for _, v := range dangerousVectors {
		if strings.Contains(cmd, v) {
			return RiskDangerous
		}
	}

	for _, d := range destructivePatterns {
		if strings.Contains(cmd, d) {
			return RiskHigh
		}
	}

	for _, ro := range readOnlyPrefixes {
		if strings.HasPrefix(cmd, ro) {
			return RiskSafe
		}
	}

	// Unknown: err on the side of caution.
	return RiskMedium
}

// Decide returns the policy decision for a given risk level.
// This is the default policy; user-persisted rules overlay it.
func Decide(r Risk, skipPermissions bool) Decision {
	if skipPermissions {
		return DecisionAllow
	}
	switch r {
	case RiskSafe:
		return DecisionAllow
	case RiskLow:
		return DecisionAllow
	case RiskMedium:
		return DecisionAsk
	case RiskHigh:
		return DecisionAsk
	case RiskDangerous:
		return DecisionDeny
	default:
		return DecisionAsk
	}
}

// ScrubSecrets removes common secret patterns from text before
// sending it to an external LLM API.
func ScrubSecrets(text string) string {
	// TODO: implement regex scrubbing for AWS_, sk-, GITHUB_TOKEN, etc.
	// Placeholder — real implementation uses compiled regexp patterns.
	return text
}

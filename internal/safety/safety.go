package safety

import (
	"regexp"
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

// scrubPatterns are compiled at init time and matched against text before
// it is sent to an external LLM API. Anything they match is redacted.
var scrubPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                             // AWS access key
	regexp.MustCompile(`(?i)aws_secret[_a-z]*\s*=\s*[^\s]{20,}`),       // AWS secret assignment
	regexp.MustCompile(`(?i)(api_key|apikey|api-key)\s*[:=]\s*\S{8,}`), // generic API key assignment
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),                          // OpenAI/Anthropic key
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36}`),                    // GitHub token
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-_\.]{20,}`),               // bearer token
	regexp.MustCompile(`(?i)password\s*[:=]\s*\S{4,}`),                 // generic password assignment
	regexp.MustCompile(`sk_(live|test)_[a-zA-Z0-9]{16,}`),              // Stripe secret key
	regexp.MustCompile(`SG\.[A-Za-z0-9_\-]{16,}\.[A-Za-z0-9_\-]{16,}`), // SendGrid API key
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\b`), // JWT
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]{0,40}PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]{0,40}PRIVATE KEY-----`), // PEM private key block
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
			// find commands can have side effects
			if strings.HasPrefix(cmd, "find") || strings.HasPrefix(cmd, "find ") {
				if strings.Contains(cmd, "-exec") || strings.Contains(cmd, "-execdir") || strings.Contains(cmd, "-delete") {
					return RiskDangerous
				}
			}
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
	if text == "" {
		return text
	}
	for _, re := range scrubPatterns {
		text = re.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

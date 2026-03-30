package gitstatus

import (
	"strings"

	"github.com/marcus/sidecar/internal/config"
)

// DefaultAllowedPrefixes are the standard conventional commit types.
var DefaultAllowedPrefixes = []string{
	"feat", "fix", "docs", "style", "refactor",
	"perf", "test", "build", "ci", "chore", "revert", "merge",
}

// NormalizeResult holds the normalized message and any warnings.
type NormalizeResult struct {
	Message  string
	Warning  string // non-empty if subject exceeds max length
	Error    string // non-empty if validation failed (e.g., missing prefix)
}

// NormalizeCommitMessage cleans up and validates a commit message.
func NormalizeCommitMessage(raw string, cfg config.CommitConfig) NormalizeResult {
	// Normalize line endings to LF.
	msg := strings.ReplaceAll(raw, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")

	// Split into lines and clean each one.
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	// Collapse consecutive blank lines into a single blank line.
	collapsed := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		blank := line == ""
		if blank && prevBlank {
			continue
		}
		collapsed = append(collapsed, line)
		prevBlank = blank
	}

	// Trim leading and trailing blank lines.
	for len(collapsed) > 0 && collapsed[0] == "" {
		collapsed = collapsed[1:]
	}
	for len(collapsed) > 0 && collapsed[len(collapsed)-1] == "" {
		collapsed = collapsed[:len(collapsed)-1]
	}

	result := NormalizeResult{
		Message: strings.Join(collapsed, "\n"),
	}

	if result.Message == "" {
		result.Error = "Commit message cannot be empty"
		return result
	}

	subject := collapsed[0]

	// Check subject line length.
	maxLen := cfg.SubjectMaxLen
	if maxLen <= 0 {
		maxLen = 72
	}
	if len(subject) > maxLen {
		result.Warning = "Subject line exceeds recommended length"
	}

	// Check conventional commit prefix.
	if cfg.ConventionalCommits {
		prefixes := cfg.AllowedPrefixes
		if len(prefixes) == 0 {
			prefixes = DefaultAllowedPrefixes
		}
		if !hasConventionalPrefix(subject, prefixes) {
			result.Error = "Missing conventional commit prefix (e.g., feat:, fix:)"
		}
	}

	return result
}

// hasConventionalPrefix checks if subject starts with "type:" or "type(scope):".
func hasConventionalPrefix(subject string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(subject, prefix+":") || strings.HasPrefix(subject, prefix+"(") {
			return true
		}
	}
	return false
}

// SubjectLength returns the length of the first line of a commit message.
func SubjectLength(msg string) int {
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		return idx
	}
	return len(msg)
}

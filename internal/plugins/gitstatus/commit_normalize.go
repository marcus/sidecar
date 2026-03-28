package gitstatus

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marcus/sidecar/internal/config"
)

// NormalizeCommitMessage applies configured normalization rules to a commit message.
func NormalizeCommitMessage(msg string, cfg config.CommitConfig) string {
	if msg == "" {
		return msg
	}

	lines := strings.SplitAfter(msg, "\n")
	// Rebuild without trailing newline markers for easier manipulation
	parts := strings.Split(msg, "\n")

	subject := parts[0]

	if cfg.AutoCapitalize {
		subject = capitalizeFirst(subject)
	}

	if cfg.StripTrailingPeriod {
		subject = strings.TrimRight(subject, ".")
	}

	if cfg.SubjectMaxLen > 0 && len(subject) > cfg.SubjectMaxLen {
		subject = truncateSubject(subject, cfg.SubjectMaxLen)
	}

	parts[0] = subject

	// Ensure blank second line between subject and body
	if cfg.EnforceBlankSecondLine && len(parts) > 1 {
		if strings.TrimSpace(parts[1]) != "" {
			// Insert blank line between subject and body
			newParts := make([]string, 0, len(parts)+1)
			newParts = append(newParts, parts[0], "")
			newParts = append(newParts, parts[1:]...)
			parts = newParts
		}
	}

	_ = lines // suppress unused warning from earlier split
	return strings.Join(parts, "\n")
}

// capitalizeFirst uppercases the first letter of s.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// truncateSubject truncates subject to maxLen, breaking at word boundary if possible.
func truncateSubject(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Try to break at last space before maxLen
	cut := s[:maxLen]
	if idx := strings.LastIndex(cut, " "); idx > maxLen/2 {
		return cut[:idx]
	}
	return cut
}

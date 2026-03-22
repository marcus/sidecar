package gitstatus

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSubjectLen = 72

// NormalizeCommitMessage standardizes a commit message:
// - trims leading/trailing whitespace
// - capitalizes the first letter of the subject line
// - removes a trailing period from the subject line
// - truncates subject lines exceeding 72 characters (with ellipsis)
// - ensures a blank line separates subject from body
func NormalizeCommitMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}

	lines := strings.SplitN(msg, "\n", 2)
	subject := strings.TrimSpace(lines[0])

	// Capitalize first letter
	if r, size := utf8.DecodeRuneInString(subject); size > 0 && unicode.IsLower(r) {
		subject = string(unicode.ToUpper(r)) + subject[size:]
	}

	// Remove trailing period
	subject = strings.TrimRight(subject, ".")

	// Truncate long subjects
	if len(subject) > maxSubjectLen {
		subject = subject[:maxSubjectLen-1] + "…"
	}

	if len(lines) == 1 {
		return subject
	}

	body := lines[1]
	// Ensure blank line between subject and body
	body = strings.TrimLeft(body, "\n")
	if body == "" {
		return subject
	}

	return subject + "\n\n" + body
}

package security

import (
	"fmt"

	"github.com/marcus/sidecar/internal/adapter"
)

// BatchScanResult represents the result of scanning a session for PII
type BatchScanResult struct {
	SessionID       string
	SessionSlug     string
	PII             []PIIMatch
	HasSensitivePII bool
}

// BatchScanSessions scans multiple sessions for PII
func BatchScanSessions(sessions []adapter.Session, messages map[string][]adapter.Message, sensitivity SensitivityLevel) []BatchScanResult {
	scanner := NewScanner(sensitivity, true)
	var results []BatchScanResult

	for _, session := range sessions {
		sessionMessages := messages[session.ID]
		if len(sessionMessages) == 0 {
			continue
		}

		var allMatches []PIIMatch
		hasSensitive := false

		for _, msg := range sessionMessages {
			matches := scanner.Scan(msg.Content)
			allMatches = append(allMatches, matches...)

			for _, m := range matches {
				if SensitiveTypes[m.Type] {
					hasSensitive = true
				}
			}
		}

		if len(allMatches) > 0 {
			results = append(results, BatchScanResult{
				SessionID:       session.ID,
				SessionSlug:     session.Slug,
				PII:             allMatches,
				HasSensitivePII: hasSensitive,
			})
		}
	}

	return results
}

// FormatBatchResults formats batch scan results for display
func FormatBatchResults(results []BatchScanResult) string {
	if len(results) == 0 {
		return "No PII detected in scanned conversations.\n"
	}

	var output string
	sensitiveCount := 0
	totalCount := 0

	for _, result := range results {
		output += fmt.Sprintf("\n📋 Session: %s (%s)\n", result.SessionSlug, result.SessionID)

		// Group matches by type
		matchesByType := make(map[PIIType][]PIIMatch)
		for _, m := range result.PII {
			matchesByType[m.Type] = append(matchesByType[m.Type], m)
		}

		for piiType, matches := range matchesByType {
			marker := "  "
			if SensitiveTypes[piiType] {
				marker = "⚠ "
				sensitiveCount += len(matches)
			}
			totalCount += len(matches)

			output += fmt.Sprintf("%s%s (%d occurrences)\n", marker, piiType, len(matches))

			// Show first occurrence with line number
			if len(matches) > 0 {
				m := matches[0]
				output += fmt.Sprintf("     Line %d: %s\n", m.Line+1, truncateValue(m.Value, 50))
			}
		}
	}

	output += fmt.Sprintf("\n\nSummary: Found %d PII matches (%d sensitive) in %d sessions\n", totalCount, sensitiveCount, len(results))
	return output
}

// truncateValue returns a truncated string representation of a value
func truncateValue(val string, maxLen int) string {
	if len(val) <= maxLen {
		return val
	}
	return val[:maxLen] + "..."
}

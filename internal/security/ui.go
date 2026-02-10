package security

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// PIIIndicator returns a styled indicator string for PII detection
func PIIIndicator(hasSensitive bool) string {
	if !hasSensitive {
		return ""
	}

	// Warn style for sensitive PII
	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("1")).  // Red
		Background(lipgloss.Color("232")). // Dark background
		Bold(true).
		Padding(0, 1)

	return warnStyle.Render("⚠ PII")
}

// PIIWarningBanner returns a full-width warning banner for sensitive PII
func PIIWarningBanner(matches []PIIMatch, width int) string {
	sensitiveMatches := make([]PIIMatch, 0)
	for _, m := range matches {
		if SensitiveTypes[m.Type] {
			sensitiveMatches = append(sensitiveMatches, m)
		}
	}

	if len(sensitiveMatches) == 0 {
		return ""
	}

	// Count matches by type
	typeCount := make(map[PIIType]int)
	for _, m := range sensitiveMatches {
		typeCount[m.Type]++
	}

	var types string
	for piiType, count := range typeCount {
		if types != "" {
			types += ", "
		}
		types += fmt.Sprintf("%s (%d)", piiType, count)
	}

	warningText := fmt.Sprintf("⚠ Sensitive PII detected: %s", types)

	// Create warning banner style
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("1")).  // Red
		Background(lipgloss.Color("52")). // Dark red
		Bold(true).
		Padding(0, 1).
		Width(width)

	return bannerStyle.Render(warningText)
}

// PIIDetectionSummary returns a summary of detected PII types
func PIIDetectionSummary(matches []PIIMatch) string {
	if len(matches) == 0 {
		return ""
	}

	// Group by type
	typeCount := make(map[PIIType]int)
	for _, m := range matches {
		typeCount[m.Type]++
	}

	summary := "PII detected: "
	first := true
	for piiType, count := range typeCount {
		if !first {
			summary += ", "
		}
		summary += fmt.Sprintf("%s(%d)", piiType, count)
		first = false
	}

	return summary
}

// InlineWarning returns a styled inline warning for a single PII match
func InlineWarning(piiType PIIType) string {
	if !SensitiveTypes[piiType] {
		return ""
	}

	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("1")).
		Background(lipgloss.Color("232")).
		Padding(0, 1)

	return warnStyle.Render(fmt.Sprintf("[%s]", piiType))
}

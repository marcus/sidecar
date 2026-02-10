package docdrift

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ReportFormat specifies the output format for the report.
type ReportFormat string

const (
	FormatJSON   ReportFormat = "json"
	FormatText   ReportFormat = "text"
	FormatMarkdown ReportFormat = "markdown"
)

// Reporter generates formatted drift reports.
type Reporter struct {
	Report *Report
	Format ReportFormat
}

// NewReporter creates a new reporter.
func NewReporter(report *Report, format ReportFormat) *Reporter {
	return &Reporter{
		Report: report,
		Format: format,
	}
}

// Generate produces the formatted report.
func (r *Reporter) Generate() string {
	switch r.Format {
	case FormatJSON:
		return r.generateJSON()
	case FormatMarkdown:
		return r.generateMarkdown()
	default:
		return r.generateText()
	}
}

// generateText generates a human-readable text report.
func (r *Reporter) generateText() string {
	var sb strings.Builder

	sb.WriteString("╔════════════════════════════════════════╗\n")
	sb.WriteString("║      Documentation Drift Report        ║\n")
	sb.WriteString("╚════════════════════════════════════════╝\n\n")

	// Summary
	sb.WriteString(fmt.Sprintf("Coverage: %.1f%% (%d/%d code items documented)\n",
		r.Report.CoveragePercent,
		r.Report.TotalCodeFeatures-r.countGapsByType("undocumented"),
		r.Report.TotalCodeFeatures))
	sb.WriteString(fmt.Sprintf("Total Gaps: %d\n\n", len(r.Report.Gaps)))

	// Group gaps by type
	undocumented := r.filterGapsByType("undocumented")
	orphaned := r.filterGapsByType("orphaned")

	if len(undocumented) > 0 {
		sb.WriteString("⚠ UNDOCUMENTED CODE (High Priority)\n")
		sb.WriteString("────────────────────────────────────\n")
		for _, gap := range undocumented {
			sb.WriteString(fmt.Sprintf("  • %s (%s) - %s\n", gap.Feature, gap.FeatureType, gap.Context))
		}
		sb.WriteString("\n")
	}

	if len(orphaned) > 0 {
		sb.WriteString("ℹ ORPHANED DOCUMENTATION (Low Priority)\n")
		sb.WriteString("─────────────────────────────────────\n")
		for _, gap := range orphaned {
			sb.WriteString(fmt.Sprintf("  • %s (%s) - %s\n", gap.Feature, gap.FeatureType, gap.Context))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// generateJSON generates a JSON report.
func (r *Reporter) generateJSON() string {
	output := map[string]interface{}{
		"coverage": map[string]interface{}{
			"percent":           r.Report.CoveragePercent,
			"documented":        r.Report.TotalCodeFeatures - r.countGapsByType("undocumented"),
			"total":             r.Report.TotalCodeFeatures,
		},
		"gaps": map[string]interface{}{
			"undocumented": r.filterGapsByType("undocumented"),
			"orphaned":     r.filterGapsByType("orphaned"),
			"total":        len(r.Report.Gaps),
		},
	}

	bytes, _ := json.MarshalIndent(output, "", "  ")
	return string(bytes)
}

// generateMarkdown generates a Markdown report.
func (r *Reporter) generateMarkdown() string {
	var sb strings.Builder

	sb.WriteString("# Documentation Drift Report\n\n")

	// Summary section
	sb.WriteString("## Summary\n\n")
	documented := r.Report.TotalCodeFeatures - r.countGapsByType("undocumented")
	sb.WriteString(fmt.Sprintf("- **Coverage**: %.1f%% (%d/%d code items documented)\n",
		r.Report.CoveragePercent, documented, r.Report.TotalCodeFeatures))
	sb.WriteString(fmt.Sprintf("- **Total Gaps**: %d\n", len(r.Report.Gaps)))
	sb.WriteString(fmt.Sprintf("- **Total Code Features**: %d\n", r.Report.TotalCodeFeatures))
	sb.WriteString(fmt.Sprintf("- **Total Doc Claims**: %d\n\n", r.Report.TotalDocClaims))

	// Undocumented section
	undocumented := r.filterGapsByType("undocumented")
	if len(undocumented) > 0 {
		sb.WriteString("## Undocumented Code Features\n\n")
		sb.WriteString("| Feature | Type | Context |\n")
		sb.WriteString("|---------|------|----------|\n")
		for _, gap := range undocumented {
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", gap.Feature, gap.FeatureType, gap.Context))
		}
		sb.WriteString("\n")
	}

	// Orphaned section
	orphaned := r.filterGapsByType("orphaned")
	if len(orphaned) > 0 {
		sb.WriteString("## Orphaned Documentation Claims\n\n")
		sb.WriteString("| Feature | Type | Context |\n")
		sb.WriteString("|---------|------|----------|\n")
		for _, gap := range orphaned {
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", gap.Feature, gap.FeatureType, gap.Context))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// filterGapsByType returns gaps of a specific type.
func (r *Reporter) filterGapsByType(gapType string) []Gap {
	var result []Gap
	for _, gap := range r.Report.Gaps {
		if gap.Type == gapType {
			result = append(result, gap)
		}
	}
	// Sort by severity then feature name
	sort.Slice(result, func(i, j int) bool {
		if result[i].Severity != result[j].Severity {
			return result[i].Severity > result[j].Severity
		}
		return result[i].Feature < result[j].Feature
	})
	return result
}

// countGapsByType returns the count of gaps of a specific type.
func (r *Reporter) countGapsByType(gapType string) int {
	count := 0
	for _, gap := range r.Report.Gaps {
		if gap.Type == gapType {
			count++
		}
	}
	return count
}

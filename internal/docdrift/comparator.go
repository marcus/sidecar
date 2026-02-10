package docdrift

import (
	"strings"
)

// Gap represents a mismatch between code and documentation.
type Gap struct {
	Type        string       // "undocumented", "orphaned"
	Feature     string       // Feature name
	FeatureType string       // "function", "type", "plugin", etc.
	Context     string       // Additional context
	Severity    string       // "high", "medium", "low"
}

// Report summarizes all documentation gaps.
type Report struct {
	Gaps              []Gap
	TotalCodeFeatures int
	TotalDocClaims    int
	CoveragePercent   float64
}

// Comparator finds mismatches between code and documentation.
type Comparator struct {
	CodeFeatures []CodeFeature
	DocClaims    []DocumentationClaim
}

// NewComparator creates a new comparator.
func NewComparator(codeFeatures []CodeFeature, docClaims []DocumentationClaim) *Comparator {
	return &Comparator{
		CodeFeatures: codeFeatures,
		DocClaims:    docClaims,
	}
}

// Compare identifies gaps between code and documentation.
func (c *Comparator) Compare() *Report {
	report := &Report{
		Gaps:              []Gap{},
		TotalCodeFeatures: len(c.CodeFeatures),
		TotalDocClaims:    len(c.DocClaims),
	}

	// Create normalized maps for lookup
	docNamesSet := c.createDocNamesSet()
	codeNamesSet := c.createCodeNamesSet()

	// Find undocumented features (code without docs)
	for _, feat := range c.CodeFeatures {
		if !c.isFeatureDocumented(feat, docNamesSet) {
			severity := c.calculateSeverity(feat.Type)
			report.Gaps = append(report.Gaps, Gap{
				Type:        "undocumented",
				Feature:     feat.Name,
				FeatureType: feat.Type,
				Context:     "Found in " + feat.SourceFile,
				Severity:    severity,
			})
		}
	}

	// Find orphaned claims (docs without code)
	for _, claim := range c.DocClaims {
		if claim.Type == "feature" || claim.Type == "plugin" {
			if !c.isClaimSupported(claim, codeNamesSet) {
				report.Gaps = append(report.Gaps, Gap{
					Type:        "orphaned",
					Feature:     claim.Name,
					FeatureType: claim.Type,
					Context:     "Mentioned in " + claim.Section,
					Severity:    "low",
				})
			}
		}
	}

	// Calculate coverage
	if len(c.CodeFeatures) > 0 {
		documented := len(c.CodeFeatures) - c.countGapsByType(report.Gaps, "undocumented")
		report.CoveragePercent = float64(documented) / float64(len(c.CodeFeatures)) * 100
	}

	return report
}

// createDocNamesSet creates a normalized set of documented names.
func (c *Comparator) createDocNamesSet() map[string]bool {
	set := make(map[string]bool)
	for _, claim := range c.DocClaims {
		normalized := strings.ToLower(claim.Name)
		normalized = strings.ReplaceAll(normalized, " ", "-")
		normalized = strings.ReplaceAll(normalized, "_", "-")
		set[normalized] = true
	}
	return set
}

// createCodeNamesSet creates a normalized set of code names.
func (c *Comparator) createCodeNamesSet() map[string]bool {
	set := make(map[string]bool)
	for _, feat := range c.CodeFeatures {
		normalized := strings.ToLower(feat.Name)
		normalized = strings.ReplaceAll(normalized, " ", "-")
		normalized = strings.ReplaceAll(normalized, "_", "-")
		set[normalized] = true
	}
	return set
}

// isFeatureDocumented checks if a code feature is mentioned in documentation.
func (c *Comparator) isFeatureDocumented(feat CodeFeature, docNames map[string]bool) bool {
	normalized := strings.ToLower(feat.Name)
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = strings.ReplaceAll(normalized, "_", "-")
	return docNames[normalized]
}

// isClaimSupported checks if a documentation claim is supported by code.
func (c *Comparator) isClaimSupported(claim DocumentationClaim, codeNames map[string]bool) bool {
	normalized := strings.ToLower(claim.Name)
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = strings.ReplaceAll(normalized, "_", "-")
	return codeNames[normalized]
}

// calculateSeverity determines gap severity based on feature type.
func (c *Comparator) calculateSeverity(featureType string) string {
	switch featureType {
	case "function", "method", "interface":
		return "high"
	case "type":
		return "medium"
	default:
		return "low"
	}
}

// countGapsByType counts gaps of a specific type.
func (c *Comparator) countGapsByType(gaps []Gap, gapType string) int {
	count := 0
	for _, gap := range gaps {
		if gap.Type == gapType {
			count++
		}
	}
	return count
}

package docdrift

import (
	"testing"
)

func TestComparatorFindUndocumented(t *testing.T) {
	codeFeatures := []CodeFeature{
		{Name: "PublicFunc", Type: "function", IsExported: true},
		{Name: "PublicType", Type: "type", IsExported: true},
		{Name: "DocumentedFunc", Type: "function", IsExported: true},
	}

	docClaims := []DocumentationClaim{
		{Name: "DocumentedFunc", Type: "feature"},
	}

	comparator := NewComparator(codeFeatures, docClaims)
	report := comparator.Compare()

	// Should find 2 undocumented features
	undocumentedCount := 0
	for _, gap := range report.Gaps {
		if gap.Type == "undocumented" {
			undocumentedCount++
		}
	}

	if undocumentedCount != 2 {
		t.Errorf("Expected 2 undocumented features, found %d", undocumentedCount)
	}
}

func TestComparatorFindOrphaned(t *testing.T) {
	codeFeatures := []CodeFeature{
		{Name: "PublicFunc", Type: "function", IsExported: true},
	}

	docClaims := []DocumentationClaim{
		{Name: "PublicFunc", Type: "feature"},
		{Name: "NonExistentFunc", Type: "feature"},
	}

	comparator := NewComparator(codeFeatures, docClaims)
	report := comparator.Compare()

	// Should find 1 orphaned claim
	orphanedCount := 0
	for _, gap := range report.Gaps {
		if gap.Type == "orphaned" {
			orphanedCount++
		}
	}

	if orphanedCount != 1 {
		t.Errorf("Expected 1 orphaned feature, found %d", orphanedCount)
	}
}

func TestComparatorCoverageCalculation(t *testing.T) {
	codeFeatures := []CodeFeature{
		{Name: "Func1", Type: "function", IsExported: true},
		{Name: "Func2", Type: "function", IsExported: true},
		{Name: "Func3", Type: "function", IsExported: true},
		{Name: "Func4", Type: "function", IsExported: true},
	}

	docClaims := []DocumentationClaim{
		{Name: "Func1", Type: "feature"},
		{Name: "Func2", Type: "feature"},
	}

	comparator := NewComparator(codeFeatures, docClaims)
	report := comparator.Compare()

	// 2 out of 4 features documented = 50% coverage
	expectedCoverage := 50.0
	if report.CoveragePercent != expectedCoverage {
		t.Errorf("Expected %.1f%% coverage, got %.1f%%", expectedCoverage, report.CoveragePercent)
	}
}

func TestComparatorNormalization(t *testing.T) {
	// Test that names are normalized (case-insensitive, underscores/spaces)
	codeFeatures := []CodeFeature{
		{Name: "PublicFunc", Type: "function", IsExported: true},
		{Name: "public_type", Type: "type", IsExported: true},
	}

	docClaims := []DocumentationClaim{
		{Name: "publicfunc", Type: "feature"}, // lowercase
		{Name: "public-type", Type: "feature"}, // with dash
	}

	comparator := NewComparator(codeFeatures, docClaims)
	report := comparator.Compare()

	// Both should be found as documented due to normalization
	undocumentedCount := 0
	for _, gap := range report.Gaps {
		if gap.Type == "undocumented" {
			undocumentedCount++
		}
	}

	if undocumentedCount != 0 {
		t.Errorf("Expected all features to be documented after normalization, found %d undocumented", undocumentedCount)
	}
}

func TestComparatorSeverityCalculation(t *testing.T) {
	codeFeatures := []CodeFeature{
		{Name: "Func", Type: "function", IsExported: true},
		{Name: "MyType", Type: "type", IsExported: true},
		{Name: "MyInterface", Type: "interface", IsExported: true},
	}

	docClaims := []DocumentationClaim{}

	comparator := NewComparator(codeFeatures, docClaims)
	report := comparator.Compare()

	// Check severity is assigned correctly
	severities := make(map[string]string)
	for _, gap := range report.Gaps {
		if gap.Type == "undocumented" {
			severities[gap.FeatureType] = gap.Severity
		}
	}

	if severities["function"] != "high" {
		t.Errorf("Expected function severity to be 'high', got '%s'", severities["function"])
	}

	if severities["type"] != "medium" {
		t.Errorf("Expected type severity to be 'medium', got '%s'", severities["type"])
	}

	if severities["interface"] != "high" {
		t.Errorf("Expected interface severity to be 'high', got '%s'", severities["interface"])
	}
}

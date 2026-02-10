package docdrift

import (
	"strings"
	"testing"
)

func TestReporterGenerateText(t *testing.T) {
	report := &Report{
		Gaps: []Gap{
			{Type: "undocumented", Feature: "PublicFunc", FeatureType: "function", Severity: "high"},
			{Type: "orphaned", Feature: "NonExistent", FeatureType: "feature", Severity: "low"},
		},
		TotalCodeFeatures: 5,
		TotalDocClaims:    3,
		CoveragePercent:   80.0,
	}

	reporter := NewReporter(report, FormatText)
	output := reporter.Generate()

	if !strings.Contains(output, "80.0") {
		t.Error("Expected coverage percentage in output")
	}

	if !strings.Contains(output, "UNDOCUMENTED") {
		t.Error("Expected 'UNDOCUMENTED' section in output")
	}

	if !strings.Contains(output, "PublicFunc") {
		t.Error("Expected 'PublicFunc' in output")
	}
}

func TestReporterGenerateJSON(t *testing.T) {
	report := &Report{
		Gaps: []Gap{
			{Type: "undocumented", Feature: "PublicFunc", FeatureType: "function", Severity: "high"},
		},
		TotalCodeFeatures: 5,
		TotalDocClaims:    3,
		CoveragePercent:   80.0,
	}

	reporter := NewReporter(report, FormatJSON)
	output := reporter.Generate()

	if !strings.Contains(output, "coverage") {
		t.Error("Expected 'coverage' key in JSON output")
	}

	if !strings.Contains(output, "80") {
		t.Error("Expected coverage value in JSON output")
	}
}

func TestReporterGenerateMarkdown(t *testing.T) {
	report := &Report{
		Gaps: []Gap{
			{Type: "undocumented", Feature: "PublicFunc", FeatureType: "function", Severity: "high"},
			{Type: "orphaned", Feature: "NonExistent", FeatureType: "feature", Severity: "low"},
		},
		TotalCodeFeatures: 5,
		TotalDocClaims:    3,
		CoveragePercent:   80.0,
	}

	reporter := NewReporter(report, FormatMarkdown)
	output := reporter.Generate()

	if !strings.Contains(output, "# Documentation Drift Report") {
		t.Error("Expected markdown header in output")
	}

	if !strings.Contains(output, "Undocumented Code Features") {
		t.Error("Expected 'Undocumented Code Features' section")
	}

	if !strings.Contains(output, "Orphaned Documentation") {
		t.Error("Expected 'Orphaned Documentation' section")
	}

	if !strings.Contains(output, "| Feature | Type | Context |") {
		t.Error("Expected markdown table header in output")
	}
}

func TestReporterFilterGapsByType(t *testing.T) {
	report := &Report{
		Gaps: []Gap{
			{Type: "undocumented", Feature: "Func1", FeatureType: "function", Severity: "high"},
			{Type: "undocumented", Feature: "Func2", FeatureType: "function", Severity: "high"},
			{Type: "orphaned", Feature: "Claim1", FeatureType: "feature", Severity: "low"},
		},
	}

	reporter := NewReporter(report, FormatText)

	undocumented := reporter.filterGapsByType("undocumented")
	if len(undocumented) != 2 {
		t.Errorf("Expected 2 undocumented gaps, got %d", len(undocumented))
	}

	orphaned := reporter.filterGapsByType("orphaned")
	if len(orphaned) != 1 {
		t.Errorf("Expected 1 orphaned gap, got %d", len(orphaned))
	}
}

func TestReporterCountGapsByType(t *testing.T) {
	report := &Report{
		Gaps: []Gap{
			{Type: "undocumented", Feature: "Func1"},
			{Type: "undocumented", Feature: "Func2"},
			{Type: "orphaned", Feature: "Claim1"},
		},
	}

	reporter := NewReporter(report, FormatText)

	undocumentedCount := reporter.countGapsByType("undocumented")
	if undocumentedCount != 2 {
		t.Errorf("Expected count of 2 undocumented, got %d", undocumentedCount)
	}

	orphanedCount := reporter.countGapsByType("orphaned")
	if orphanedCount != 1 {
		t.Errorf("Expected count of 1 orphaned, got %d", orphanedCount)
	}
}

package tdmonitor

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("expected non-nil plugin")
	}
	if p.activeList != "ready" {
		t.Errorf("expected activeList 'ready', got %q", p.activeList)
	}
}

func TestPluginID(t *testing.T) {
	p := New()
	if id := p.ID(); id != "td-monitor" {
		t.Errorf("expected ID 'td-monitor', got %q", id)
	}
}

func TestPluginName(t *testing.T) {
	p := New()
	if name := p.Name(); name != "TD Monitor" {
		t.Errorf("expected Name 'TD Monitor', got %q", name)
	}
}

func TestPluginIcon(t *testing.T) {
	p := New()
	if icon := p.Icon(); icon != "T" {
		t.Errorf("expected Icon 'T', got %q", icon)
	}
}

func TestFocusContext(t *testing.T) {
	p := New()

	// Default view
	if ctx := p.FocusContext(); ctx != "td-monitor" {
		t.Errorf("expected context 'td-monitor', got %q", ctx)
	}

	// Detail view
	p.showDetail = true
	if ctx := p.FocusContext(); ctx != "td-detail" {
		t.Errorf("expected context 'td-detail', got %q", ctx)
	}
}

func TestDiagnosticsNoDatabase(t *testing.T) {
	p := New()
	diags := p.Diagnostics()

	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}

	if diags[0].Status != "disabled" {
		t.Errorf("expected status 'disabled', got %q", diags[0].Status)
	}
}

func TestActiveListData(t *testing.T) {
	p := New()
	p.inProgress = []Issue{{ID: "1", Title: "In Progress"}}
	p.ready = []Issue{{ID: "2", Title: "Ready"}}
	p.reviewable = []Issue{{ID: "3", Title: "Reviewable"}}

	// Default is ready
	list := p.activeListData()
	if len(list) != 1 || list[0].ID != "2" {
		t.Error("expected ready list")
	}

	// Switch to in_progress
	p.activeList = "in_progress"
	list = p.activeListData()
	if len(list) != 1 || list[0].ID != "1" {
		t.Error("expected in_progress list")
	}

	// Switch to reviewable
	p.activeList = "reviewable"
	list = p.activeListData()
	if len(list) != 1 || list[0].ID != "3" {
		t.Error("expected reviewable list")
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		text     string
		maxWidth int
		expected int
	}{
		{"hello world", 20, 1},
		{"hello world this is a longer text", 10, 5},
		{"", 10, 0},
		{"line one\nline two", 50, 2},
	}

	for _, tt := range tests {
		lines := wrapText(tt.text, tt.maxWidth)
		if len(lines) != tt.expected {
			t.Errorf("wrapText(%q, %d) = %d lines, expected %d",
				tt.text, tt.maxWidth, len(lines), tt.expected)
		}
	}
}

func TestStatusBadge(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"open", "open"},
		{"in_progress", "in_progress"},
		{"in_review", "in_review"},
		{"done", "done"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		result := statusBadge(tt.status)
		// Just check that it contains the status text
		if !contains(result, tt.expected) {
			t.Errorf("statusBadge(%q) should contain %q", tt.status, tt.expected)
		}
	}
}

func TestFormatIssueCount(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{1, "1 issue"},
		{5, "5 issues"},
		{10, "10 issues"},
		{100, "100 issues"},
	}

	for _, tt := range tests {
		result := formatIssueCount(tt.count)
		if result != tt.expected {
			t.Errorf("formatIssueCount(%d) = %q, expected %q",
				tt.count, result, tt.expected)
		}
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

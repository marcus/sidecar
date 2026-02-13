package integration

import (
	"testing"
	"time"
)

func TestMapStatusToGH(t *testing.T) {
	tests := []struct {
		tdStatus string
		want     string
	}{
		{"open", "open"},
		{"in_progress", "open"},
		{"blocked", "open"},
		{"closed", "closed"},
	}
	for _, tt := range tests {
		if got := MapStatusToGH(tt.tdStatus); got != tt.want {
			t.Errorf("MapStatusToGH(%q) = %q, want %q", tt.tdStatus, got, tt.want)
		}
	}
}

func TestMapStatusFromGH(t *testing.T) {
	tests := []struct {
		ghState string
		want    string
	}{
		{"open", "open"},
		{"closed", "closed"},
		{"OPEN", "open"}, // unexpected case, defaults to open
	}
	for _, tt := range tests {
		if got := MapStatusFromGH(tt.ghState); got != tt.want {
			t.Errorf("MapStatusFromGH(%q) = %q, want %q", tt.ghState, got, tt.want)
		}
	}
}

func TestMapTypeToLabels(t *testing.T) {
	tests := []struct {
		tdType string
		want   []string
	}{
		{"bug", []string{"bug"}},
		{"feature", []string{"enhancement"}},
		{"task", []string{"task"}},
		{"chore", []string{"task"}},
		{"epic", nil},
		{"", nil},
	}
	for _, tt := range tests {
		got := MapTypeToLabels(tt.tdType)
		if len(got) != len(tt.want) {
			t.Errorf("MapTypeToLabels(%q) = %v, want %v", tt.tdType, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("MapTypeToLabels(%q)[%d] = %q, want %q", tt.tdType, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMapLabelsToType(t *testing.T) {
	tests := []struct {
		labels []string
		want   string
	}{
		{[]string{"bug"}, "bug"},
		{[]string{"enhancement"}, "feature"},
		{[]string{"task"}, "task"},
		{[]string{"docs", "enhancement"}, "feature"},
		{[]string{"docs"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := MapLabelsToType(tt.labels); got != tt.want {
			t.Errorf("MapLabelsToType(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}

func TestMapPriorityToLabel(t *testing.T) {
	tests := []struct {
		priority string
		want     string
	}{
		{"P0", "priority:P0"},
		{"P4", "priority:P4"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := MapPriorityToLabel(tt.priority); got != tt.want {
			t.Errorf("MapPriorityToLabel(%q) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

func TestMapLabelToPriority(t *testing.T) {
	tests := []struct {
		labels []string
		want   string
	}{
		{[]string{"priority:P1"}, "P1"},
		{[]string{"bug", "priority:P0"}, "P0"},
		{[]string{"bug"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := MapLabelToPriority(tt.labels); got != tt.want {
			t.Errorf("MapLabelToPriority(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}

func TestTDToExternal(t *testing.T) {
	td := TDIssue{
		ID:          "td-abc123",
		Title:       "Fix login bug",
		Description: "Users can't log in",
		Status:      "open",
		Type:        "bug",
		Priority:    "P1",
		Labels:      []string{"frontend"},
		UpdatedAt:   time.Now(),
	}

	ext := TDToExternal(td)

	if ext.Title != "Fix login bug" {
		t.Errorf("Title = %q, want %q", ext.Title, "Fix login bug")
	}
	if ext.Body != "Users can't log in" {
		t.Errorf("Body = %q, want %q", ext.Body, "Users can't log in")
	}
	if ext.State != "open" {
		t.Errorf("State = %q, want %q", ext.State, "open")
	}

	// Should contain: "bug", "priority:P1", "frontend"
	labelSet := make(map[string]bool)
	for _, l := range ext.Labels {
		labelSet[l] = true
	}
	if !labelSet["bug"] {
		t.Error("missing label 'bug'")
	}
	if !labelSet["priority:P1"] {
		t.Error("missing label 'priority:P1'")
	}
	if !labelSet["frontend"] {
		t.Error("missing label 'frontend'")
	}
}

func TestTDToExternalClosed(t *testing.T) {
	td := TDIssue{Status: "closed"}
	ext := TDToExternal(td)
	if ext.State != "closed" {
		t.Errorf("State = %q, want %q", ext.State, "closed")
	}
}

func TestExternalToTD(t *testing.T) {
	ext := ExternalIssue{
		ID:    "42",
		Title: "Add dark mode",
		Body:  "Please add dark mode support",
		State: "open",
		Labels: []string{
			"enhancement",
			"priority:P2",
			"ui",
		},
	}

	td := ExternalToTD(ext)

	if td.Title != "Add dark mode" {
		t.Errorf("Title = %q, want %q", td.Title, "Add dark mode")
	}
	if td.Description != "Please add dark mode support" {
		t.Errorf("Description = %q, want %q", td.Description, "Please add dark mode support")
	}
	if td.Status != "open" {
		t.Errorf("Status = %q, want %q", td.Status, "open")
	}
	if td.Type != "feature" {
		t.Errorf("Type = %q, want %q", td.Type, "feature")
	}
	if td.Priority != "P2" {
		t.Errorf("Priority = %q, want %q", td.Priority, "P2")
	}

	// User labels should only contain "ui" (not "enhancement" or "priority:P2")
	if len(td.Labels) != 1 || td.Labels[0] != "ui" {
		t.Errorf("Labels = %v, want [ui]", td.Labels)
	}
}

func TestExternalToTDClosed(t *testing.T) {
	ext := ExternalIssue{State: "closed"}
	td := ExternalToTD(ext)
	if td.Status != "closed" {
		t.Errorf("Status = %q, want %q", td.Status, "closed")
	}
}

func TestInternalLabelExcluded(t *testing.T) {
	td := TDIssue{
		Title:  "Test",
		Labels: []string{"td-sync", "frontend"},
	}
	ext := TDToExternal(td)

	for _, l := range ext.Labels {
		if l == "td-sync" {
			t.Error("internal label 'td-sync' should be excluded from external labels")
		}
	}
	labelSet := make(map[string]bool)
	for _, l := range ext.Labels {
		labelSet[l] = true
	}
	if !labelSet["frontend"] {
		t.Error("user label 'frontend' should be present")
	}
}

package integration

import "testing"

// Verify JiraMapper satisfies the Mapper interface.
func TestJiraMapperInterface(t *testing.T) {
	var m Mapper = &JiraMapper{}
	_ = m
}

func TestJiraMapperSyncLabel(t *testing.T) {
	m := &JiraMapper{}
	if got := m.SyncLabelPrefix(); got != "jira:" {
		t.Errorf("SyncLabelPrefix() = %q, want %q", got, "jira:")
	}
	if got := m.SyncLabel("PROJ-123"); got != "jira:PROJ-123" {
		t.Errorf("SyncLabel(PROJ-123) = %q, want %q", got, "jira:PROJ-123")
	}
}

func TestJiraMapperIsInternalLabel(t *testing.T) {
	m := &JiraMapper{}
	if !m.IsInternalLabel("jira:PROJ-1") {
		t.Error("expected jira:PROJ-1 to be internal")
	}
	if !m.IsInternalLabel("gh:#42") {
		t.Error("expected gh:#42 to be internal")
	}
	if m.IsInternalLabel("frontend") {
		t.Error("expected frontend to not be internal")
	}
}

func TestMapJiraStatusToTD(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"new", "open"},
		{"indeterminate", "in_progress"},
		{"done", "closed"},
		{"New", "open"},           // case insensitive
		{"Done", "closed"},        // case insensitive
		{"unknown", "open"},       // fallback
	}
	for _, tt := range tests {
		if got := MapJiraStatusToTD(tt.input); got != tt.want {
			t.Errorf("MapJiraStatusToTD(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMapTDStatusToJira(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"open", "new"},
		{"in_progress", "indeterminate"},
		{"blocked", "indeterminate"},
		{"closed", "done"},
		{"unknown", "new"}, // fallback
	}
	for _, tt := range tests {
		if got := MapTDStatusToJira(tt.input); got != tt.want {
			t.Errorf("MapTDStatusToJira(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMapJiraTypeToTD(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Bug", "bug"},
		{"Story", "feature"},
		{"Task", "task"},
		{"Epic", "epic"},
		{"bug", "bug"},     // case insensitive
		{"story", "feature"},
		{"Unknown", "task"}, // fallback
	}
	for _, tt := range tests {
		if got := MapJiraTypeToTD(tt.input); got != tt.want {
			t.Errorf("MapJiraTypeToTD(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMapTDTypeToJira(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bug", "Bug"},
		{"feature", "Story"},
		{"task", "Task"},
		{"epic", "Epic"},
		{"chore", "Task"},
		{"unknown", "Task"}, // fallback
	}
	for _, tt := range tests {
		if got := MapTDTypeToJira(tt.input); got != tt.want {
			t.Errorf("MapTDTypeToJira(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMapJiraPriorityToTD(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Highest", "p0"},
		{"High", "p1"},
		{"Medium", "p2"},
		{"Low", "p3"},
		{"Lowest", "p4"},
		{"highest", "p0"}, // case insensitive
		{"unknown", "p2"}, // fallback
	}
	for _, tt := range tests {
		if got := MapJiraPriorityToTD(tt.input); got != tt.want {
			t.Errorf("MapJiraPriorityToTD(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMapTDPriorityToJira(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"p0", "Highest"},
		{"p1", "High"},
		{"p2", "Medium"},
		{"p3", "Low"},
		{"p4", "Lowest"},
		{"unknown", "Medium"}, // fallback
	}
	for _, tt := range tests {
		if got := MapTDPriorityToJira(tt.input); got != tt.want {
			t.Errorf("MapTDPriorityToJira(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestJiraMapperTDToExternal(t *testing.T) {
	m := &JiraMapper{}
	td := TDIssue{
		Title:       "Fix login bug",
		Description: "Users can't log in",
		Status:      "open",
		Type:        "bug",
		Priority:    "p1",
		Labels:      []string{"frontend", "jira:PROJ-42"},
	}

	ext := m.TDToExternal(td)

	if ext.Title != "Fix login bug" {
		t.Errorf("Title = %q, want %q", ext.Title, "Fix login bug")
	}
	if ext.Body != "Users can't log in" {
		t.Errorf("Body = %q, want %q", ext.Body, "Users can't log in")
	}
	if ext.State != "open" {
		t.Errorf("State = %q, want %q", ext.State, "open")
	}
	if ext.Type != "Bug" {
		t.Errorf("Type = %q, want %q", ext.Type, "Bug")
	}
	if ext.Priority != "High" {
		t.Errorf("Priority = %q, want %q", ext.Priority, "High")
	}

	// Labels should only contain user labels, not jira: sync labels
	if len(ext.Labels) != 1 || ext.Labels[0] != "frontend" {
		t.Errorf("Labels = %v, want [frontend]", ext.Labels)
	}
}

func TestJiraMapperTDToExternalClosed(t *testing.T) {
	m := &JiraMapper{}
	td := TDIssue{Status: "closed"}
	ext := m.TDToExternal(td)
	if ext.State != "closed" {
		t.Errorf("State = %q, want %q", ext.State, "closed")
	}
}

func TestJiraMapperExternalToTD(t *testing.T) {
	m := &JiraMapper{}
	ext := ExternalIssue{
		Title:    "Add dark mode",
		Body:     "Please add dark mode support",
		State:    "indeterminate",
		Type:     "Story",
		Priority: "High",
		Labels:   []string{"ui", "jira:PROJ-99"},
	}

	td := m.ExternalToTD(ext)

	if td.Title != "Add dark mode" {
		t.Errorf("Title = %q, want %q", td.Title, "Add dark mode")
	}
	if td.Description != "Please add dark mode support" {
		t.Errorf("Description = %q, want %q", td.Description, "Please add dark mode support")
	}
	if td.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", td.Status, "in_progress")
	}
	if td.Type != "feature" {
		t.Errorf("Type = %q, want %q", td.Type, "feature")
	}
	if td.Priority != "p1" {
		t.Errorf("Priority = %q, want %q", td.Priority, "p1")
	}

	// User labels should only contain "ui", not "jira:PROJ-99"
	if len(td.Labels) != 1 || td.Labels[0] != "ui" {
		t.Errorf("Labels = %v, want [ui]", td.Labels)
	}
}

func TestJiraMapperExternalToTDDone(t *testing.T) {
	m := &JiraMapper{}
	ext := ExternalIssue{State: "done", Type: "Task", Priority: "Medium"}
	td := m.ExternalToTD(ext)
	if td.Status != "closed" {
		t.Errorf("Status = %q, want %q", td.Status, "closed")
	}
}

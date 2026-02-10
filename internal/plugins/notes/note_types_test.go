package notes

import (
	"testing"
	"time"
)

func TestNoteFilter_String(t *testing.T) {
	tests := []struct {
		filter   NoteFilter
		expected string
	}{
		{
			filter:   FilterActive,
			expected: "Active",
		},
		{
			filter:   FilterArchived,
			expected: "Archived",
		},
		{
			filter:   FilterDeleted,
			expected: "Deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.filter.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestNote_Creation(t *testing.T) {
	now := time.Now()
	note := Note{
		ID:        "test-123",
		Title:     "Test Note",
		Content:   "This is test content",
		CreatedAt: now,
		UpdatedAt: now,
		Pinned:    false,
		Archived:  false,
		DeletedAt: nil,
	}

	if note.ID != "test-123" {
		t.Errorf("ID = %q, want test-123", note.ID)
	}
	if note.Title != "Test Note" {
		t.Errorf("Title = %q, want Test Note", note.Title)
	}
	if note.Content != "This is test content" {
		t.Errorf("Content mismatch")
	}
	if note.Pinned {
		t.Errorf("Pinned should be false")
	}
	if note.Archived {
		t.Errorf("Archived should be false")
	}
	if note.DeletedAt != nil {
		t.Errorf("DeletedAt should be nil")
	}
}

func TestNote_Archived(t *testing.T) {
	note := Note{
		ID:       "test-1",
		Title:    "Archived Note",
		Archived: true,
	}

	if !note.Archived {
		t.Errorf("Archived should be true")
	}
}

func TestNote_Deleted(t *testing.T) {
	now := time.Now()
	note := Note{
		ID:        "test-1",
		Title:     "Deleted Note",
		DeletedAt: &now,
	}

	if note.DeletedAt == nil {
		t.Errorf("DeletedAt should not be nil")
	}
}

func TestNote_TimestampUpdate(t *testing.T) {
	created := time.Now()
	updated := created.Add(1 * time.Hour)

	note := Note{
		ID:        "test-1",
		CreatedAt: created,
		UpdatedAt: updated,
	}

	if note.CreatedAt != created {
		t.Errorf("CreatedAt mismatch")
	}
	if note.UpdatedAt != updated {
		t.Errorf("UpdatedAt mismatch")
	}
}

func TestActionType(t *testing.T) {
	tests := []struct {
		name   string
		action ActionType
	}{
		{"create", ActionCreate},
		{"update", ActionUpdate},
		{"delete", ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.action) != tt.name {
				t.Errorf("ActionType %v should equal %q", tt.action, tt.name)
			}
		})
	}
}

func TestFocusPane(t *testing.T) {
	tests := []struct {
		name  string
		pane  FocusPane
		value int
	}{
		{"list", PaneList, 0},
		{"editor", PaneEditor, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.pane) != tt.value {
				t.Errorf("FocusPane %v should equal %d", tt.pane, tt.value)
			}
		})
	}
}

func TestNoteMatch_Fields(t *testing.T) {
	note := Note{
		ID:     "test-1",
		Title:  "Test",
		Pinned: true,
	}

	match := NoteMatch{
		Note:  note,
		Score: 95,
	}

	if match.Note.ID != "test-1" {
		t.Errorf("NoteMatch.Note.ID mismatch")
	}
	if match.Score != 95 {
		t.Errorf("Score = %d, want 95", match.Score)
	}
}

func TestNote_EmptyFields(t *testing.T) {
	note := Note{
		ID:    "",
		Title: "",
		Content: "",
	}

	if note.ID != "" {
		t.Errorf("Empty ID should be empty string")
	}
	if note.Title != "" {
		t.Errorf("Empty Title should be empty string")
	}
	if note.Content != "" {
		t.Errorf("Empty Content should be empty string")
	}
}

func TestNote_LongContent(t *testing.T) {
	longContent := make([]byte, 100000) // 100KB content
	for i := range longContent {
		longContent[i] = byte('a' + (i % 26))
	}

	note := Note{
		ID:      "test-1",
		Title:   "Long Content Note",
		Content: string(longContent),
	}

	if len(note.Content) != 100000 {
		t.Errorf("Content length = %d, want 100000", len(note.Content))
	}
}

func TestNote_SpecialCharacters(t *testing.T) {
	note := Note{
		ID:      "test-1",
		Title:   "Test with émojis 🚀 and special chars !@#$%",
		Content: "Unicode: ñ, ü, ç\nNewlines\tand\ttabs",
	}

	if note.Title != "Test with émojis 🚀 and special chars !@#$%" {
		t.Errorf("Title with special chars not preserved")
	}
	if note.Content != "Unicode: ñ, ü, ç\nNewlines\tand\ttabs" {
		t.Errorf("Content with special chars not preserved")
	}
}

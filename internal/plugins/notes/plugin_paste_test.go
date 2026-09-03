package notes

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestEditorPasteMarksDirtyAndStartsOneDebounce(t *testing.T) {
	p := newPastePlugin(t)
	p.activePane = PaneEditor
	p.previewMode = false
	p.editorNote = &p.notes[0]
	p.editorTextarea.SetValue("hello")
	p.editorTextarea.Focus()
	p.setTextareaCursorPosition(0, 0)
	beforeID := p.autoSaveID

	_, cmd := p.Update(tea.PasteMsg{Content: "IN"})
	if !p.editorDirty {
		t.Fatal("editor paste did not mark dirty")
	}
	if p.autoSaveID != beforeID+1 {
		t.Fatalf("autoSaveID = %d, want %d (one debounce)", p.autoSaveID, beforeID+1)
	}
	if got := p.editorTextarea.Value(); !strings.Contains(got, "IN") {
		t.Fatalf("textarea = %q, want pasted text inserted", got)
	}
	if cmd == nil {
		t.Fatal("editor paste started no auto-save debounce")
	}
}

func TestSearchPasteInsertsAndRescoresOnce(t *testing.T) {
	p := newPastePlugin(t)
	p.searchMode = true
	p.searchField.SetQuery("pre")
	p.notes = []Note{
		{ID: "n1", Title: "prefix match", Content: "body"},
		{ID: "n2", Title: "other", Content: "nope"},
	}

	_, cmd := p.Update(tea.PasteMsg{Content: "fix\nmatch"})
	if cmd != nil {
		t.Fatal("search paste should rescore synchronously")
	}
	if p.searchQuery() != "prefix match" {
		t.Fatalf("searchQuery = %q, want newlines converted to spaces", p.searchQuery())
	}
	if len(p.filteredNotes) != 1 || p.filteredNotes[0].Note.ID != "n1" {
		t.Fatalf("search paste rescored incorrectly: %+v", p.filteredNotes)
	}
}

func TestViewPasteEntersEditAtReadingPosition(t *testing.T) {
	p := newPastePlugin(t)
	p.activePane = PaneEditor
	p.previewMode = true
	p.editorNote = &p.notes[0]
	p.editorTextarea.SetValue("line0\nline1\nline2")
	p.previewLines = []string{"line0", "line1", "line2"}
	p.previewCursorLine = 0

	_, _ = p.Update(tea.PasteMsg{Content: "IN"})
	if p.previewMode {
		t.Fatal("view paste left preview mode")
	}
	if !p.editorDirty {
		t.Fatal("view paste did not mark dirty")
	}
	got := p.editorTextarea.Value()
	if !strings.HasPrefix(got, "IN") {
		t.Fatalf("textarea = %q, want paste at reading position (start of line 0)", got)
	}
	if strings.HasSuffix(got, "IN") && !strings.HasPrefix(got, "IN") {
		t.Fatal("view paste appended at end")
	}
	if got == "line0\nline1\nline2IN" {
		t.Fatal("view paste appended at end of the note")
	}
	if p.editorTextarea.Line() != 0 {
		t.Fatalf("cursor line = %d, want 0 (insert point, not EOF)", p.editorTextarea.Line())
	}
	if p.editorTextarea.Column() != len([]rune("IN")) {
		t.Fatalf("cursor col = %d, want %d (after inserted text)", p.editorTextarea.Column(), len([]rune("IN")))
	}
}

func TestListPasteCreatesNoteFromNonBlankText(t *testing.T) {
	p := newPastePlugin(t)
	p.activePane = PaneList
	p.viewFilter = FilterActive

	_, cmd := p.Update(tea.PasteMsg{Content: "  \nPasted title\n\nbody line"})
	if cmd == nil {
		t.Fatal("active-list paste scheduled no create")
	}
	result, ok := cmd().(NoteSavedMsg)
	if !ok {
		t.Fatalf("list paste produced %T, want NoteSavedMsg", cmd())
	}
	if result.Err != nil {
		t.Fatalf("create failed: %v", result.Err)
	}
	if result.Note == nil {
		t.Fatal("create returned no note")
	}
	if result.Note.Title != "Pasted title" {
		t.Fatalf("title = %q, want first non-blank line", result.Note.Title)
	}
	if result.Note.Content != "  \nPasted title\n\nbody line" {
		t.Fatalf("content = %q, want full paste", result.Note.Content)
	}
}

func TestArchivedListPasteIsReadOnly(t *testing.T) {
	p := newPastePlugin(t)
	p.activePane = PaneList
	p.viewFilter = FilterArchived

	before, err := p.store.List(true)
	if err != nil {
		t.Fatal(err)
	}
	_, cmd := p.Update(tea.PasteMsg{Content: "should not create"})
	if cmd == nil {
		t.Fatal("read-only paste returned no toast")
	}
	flash, ok := cmd().(msg.FlashMsg)
	if !ok {
		t.Fatalf("read-only paste produced %T, want flash", cmd())
	}
	if !strings.Contains(flash.Text, "read-only") {
		t.Fatalf("flash = %q, want read-only", flash.Text)
	}
	after, err := p.store.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("archived-list paste created a note")
	}
}

func newPastePlugin(t *testing.T) *Plugin {
	t.Helper()
	store := openTestStore(t)
	note, err := store.Create("note", "line0\nline1\nline2")
	if err != nil {
		t.Fatal(err)
	}
	p := New()
	p.ctx = &plugin.Context{Epoch: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p.store = store
	p.notes = []Note{*note}
	p.cursor = 0
	p.viewFilter = FilterActive
	p.width = 100
	p.height = 24
	p.listWidth = 30
	p.editorTextarea = textarea.New()
	p.editorTextarea.SetValue(note.Content)
	p.markdownView = false
	return p
}

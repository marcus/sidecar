package overview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// M4d-d: the global browser's terminal search bar is the shared query field,
// exactly as the project workspace's is. The two surfaces are two projections
// of one model, so a bar that edited differently here would be a bug.
func TestPreviewTerminalSearchWordDeleteRemovesOneWord(t *testing.T) {
	var m Model
	m.terminalSearch.InputActive = true
	m.terminalSearch.SetQuery("error one")

	handled, _ := m.handlePreviewTerminalSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if !handled {
		t.Fatal("alt+backspace was not consumed by the search bar")
	}
	if got := m.terminalSearch.Query(); got != "error " {
		t.Fatalf("query after alt+backspace = %q, want %q", got, "error ")
	}
}

func TestPreviewTerminalSearchPasteInsertsAtTheCaret(t *testing.T) {
	var m Model
	m.terminalSearch.InputActive = true

	if handled, _ := m.WorkspacesTerminalSearchPaste(tea.PasteMsg{Content: "needle"}); !handled {
		t.Fatal("a live terminal search refused a paste")
	}
	if got := m.terminalSearch.Query(); got != "needle" {
		t.Fatalf("query after paste = %q", got)
	}
	m.handlePreviewTerminalSearchKey(tea.KeyPressMsg{Code: tea.KeyHome})
	m.WorkspacesTerminalSearchPaste(tea.PasteMsg{Content: "x"})
	if got := m.terminalSearch.Query(); got != "xneedle" {
		t.Fatalf("query after a paste at the caret = %q, want %q", got, "xneedle")
	}

	// A closed bar is not a text input.
	m.terminalSearch.InputActive = false
	if handled, _ := m.WorkspacesTerminalSearchPaste(tea.PasteMsg{Content: "y"}); handled {
		t.Fatal("a closed terminal search took a paste")
	}
}

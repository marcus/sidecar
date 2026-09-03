package overview

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/resourceview"
)

// The Sessions surface's half of the keyboard-ownership parity assertion. Its
// twin is TestAQueryLineInACollectionPaneOwnsTheKeyboard in
// internal/plugins/workspace: one question — does the pane have the keys —
// answered the same way by both projections, so a fix in one and not the other
// is a failing test rather than a silent gap.
func focusedGlobalCollectionPane(t *testing.T) *Model {
	t.Helper()
	m := resourcePreviewModel(t)
	calls := &globalCollectionCalls{}
	run(t, m, m.SetPluginCalls(calls.callsFor()))
	run(t, m, m.OpenPreviewResource(resourceview.Ref{Instance: "recall", Collection: "results"}))
	res := m.preview.resource
	if res == nil {
		t.Fatal("no Resource pane opened for a collection")
	}
	if !m.resourcePaneFocused() {
		t.Fatal("the Resource pane did not take the keyboard when it opened")
	}
	return m
}

func TestSessionsSurfaceReportsPaneKeyboardOwnership(t *testing.T) {
	m := focusedGlobalCollectionPane(t)

	if m.WorkspacesConsumesTextInput() {
		t.Fatal("the surface claims text input before anything is being typed")
	}
	if handled, _ := m.WorkspacesKey(keyPress("/")); !handled {
		t.Fatal("`/` was not handled by the Sessions surface")
	}
	if !m.WorkspacesConsumesTextInput() {
		t.Fatal("with the query line open the surface does not report text input; " +
			"the host would go on advertising and running the tab digits")
	}

	// The digit the host would otherwise spend switching tabs.
	if handled, _ := m.WorkspacesKey(keyPress("1")); !handled {
		t.Fatal("a digit typed into an open query was not handled")
	}
	view := m.preview.resource.view()
	if got := view.Browser().PaneQuery(); got != "1" {
		t.Fatalf("query = %q after typing into it, want %q", got, "1")
	}

	// Esc clears first and releases the keyboard on the second press, which is
	// the shared query field's contract on every surface that has one.
	if handled, _ := m.WorkspacesKey(keyPress("esc")); !handled {
		t.Fatal("esc in the query line was not handled")
	}
	if !m.WorkspacesConsumesTextInput() || view.Browser().PaneQuery() != "" {
		t.Fatalf("the first esc did not clear the query: consumes=%v query=%q",
			m.WorkspacesConsumesTextInput(), view.Browser().PaneQuery())
	}
	if handled, _ := m.WorkspacesKey(keyPress("esc")); !handled {
		t.Fatal("the second esc was not handled")
	}
	if m.WorkspacesConsumesTextInput() {
		t.Fatal("the surface still claims text input after the query line closed")
	}
}

func TestSessionsSurfaceReportsAPaneOverlay(t *testing.T) {
	m := focusedGlobalCollectionPane(t)

	if m.WorkspacesBlocksGlobalKeys() {
		t.Fatal("the surface blocks host globals with no overlay open")
	}
	if handled, _ := m.WorkspacesKey(keyPress("v")); !handled {
		t.Fatal("`v` was not handled by the Sessions surface")
	}
	if !m.WorkspacesBlocksGlobalKeys() {
		t.Fatal("with the View control open the surface does not block host globals")
	}
	if handled, _ := m.WorkspacesKey(keyPress("esc")); !handled {
		t.Fatal("esc did not close the overlay")
	}
	if m.WorkspacesBlocksGlobalKeys() {
		t.Fatal("the surface still blocks host globals after the overlay closed")
	}
}

func keyPress(key string) tea.KeyPressMsg {
	if key == "esc" {
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
}

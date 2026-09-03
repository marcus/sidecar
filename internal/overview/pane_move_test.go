package overview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/workspaceinventory"
)

func globalMoveKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func enableGlobalPaneMove(t *testing.T) {
	t.Helper()
	features.Init(config.Default())
	features.SetOverride(features.PaneMove.Name, true)
	t.Cleanup(func() { features.Init(config.Default()) })
}

// pane_move ships on, so proving the gate still exists means turning it off
// explicitly rather than relying on the default.
func disableGlobalPaneMove(t *testing.T) {
	t.Helper()
	features.Init(config.Default())
	features.SetOverride(features.PaneMove.Name, false)
	t.Cleanup(func() { features.Init(config.Default()) })
}

func TestGlobalPaneMoveShortcutOpensModalFromPreviewAndList(t *testing.T) {
	m := linkPreviewModel(t, workspaceinventory.KindWorktree)
	run(t, m, openPreviewDocSpan(m, mustPreviewSpan(t, m, previewNeedleAction(t, m, "README.md"))))
	m.preview.focus = focusPreview
	doc := panelayout.FirstOfKind(m.preview.paneRoot, panelayout.Document)
	primary := panelayout.FirstOfKind(m.preview.paneRoot, panelayout.Terminal)
	if doc == nil || primary == nil {
		t.Fatalf("test tree is incomplete: %+v", m.preview.paneRoot)
	}
	m.preview.paneFocus = doc.ID

	disableGlobalPaneMove(t)
	if handled, _ := m.previewKey(globalMoveKey('M')); handled {
		t.Fatal("pane_move turned off still claimed M")
	}
	if hasGlobalPaneMoveCommand(m.Commands(), panereposition.CommandMove) {
		t.Fatal("pane_move turned off still advertises pane reposition")
	}

	enableGlobalPaneMove(t)
	if !hasGlobalPaneMoveCommand(m.Commands(), panereposition.CommandMove) {
		t.Fatal("global preview commands do not advertise pane reposition")
	}
	if handled, _ := m.previewKey(globalMoveKey('M')); !handled {
		t.Fatal("enabled pane_move did not claim M")
	}
	if m.paneLayoutModal == nil || m.paneLayoutModal.LeafID() != doc.ID || m.WorkspaceFocusContext() != panereposition.ModalContext {
		t.Fatalf("preview M opened modal=%v leaf=%d context=%q, want doc %d", m.paneLayoutModal != nil, m.paneLayoutModal.LeafID(), m.WorkspaceFocusContext(), doc.ID)
	}
	m.WorkspacesKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	run(t, m, m.focusList())
	if !hasGlobalPaneMoveCommand(m.Commands(), panereposition.CommandMove) {
		t.Fatal("global list commands do not advertise pane reposition")
	}
	if handled, _ := m.WorkspacesKey(globalMoveKey('M')); !handled {
		t.Fatal("global list M was not handled")
	}
	if m.paneLayoutModal == nil || m.paneLayoutModal.LeafID() != primary.ID {
		t.Fatalf("list M targeted leaf %d, want Primary %d", m.paneLayoutModal.LeafID(), primary.ID)
	}
	m.WorkspacesKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, focusCmd := m.focusPreviewLeaf(primary.ID)
	run(t, m, focusCmd)
	m.terminalSearch.InputActive = true
	m.WorkspacesKey(globalMoveKey('M'))
	if m.paneLayoutModal != nil || m.terminalSearch.Query() != "M" {
		t.Fatalf("Sessions terminal search lost M: modal=%v query=%q", m.paneLayoutModal != nil, m.terminalSearch.Query())
	}
	m.terminalSearch.InputActive = false
	m.terminalSearch.SetQuery("")
	if handled, _ := m.WorkspacesKey(globalMoveKey('M')); !handled || m.paneLayoutModal == nil || m.paneLayoutModal.LeafID() != primary.ID {
		t.Fatal("focused non-interactive Sessions terminal M did not open its modal")
	}
}

func hasGlobalPaneMoveCommand(commands []plugin.Command, id string) bool {
	for _, command := range commands {
		if command.ID == id {
			return true
		}
	}
	return false
}

func TestGlobalPaneMoveBoundaryUsesToast(t *testing.T) {
	m := linkPreviewModel(t, workspaceinventory.KindWorktree)
	m.preview.focus = focusPreview
	enableGlobalPaneMove(t)
	handled, _ := m.WorkspacesKey(globalMoveKey('M'))
	if !handled || m.paneLayoutModal == nil {
		t.Fatal("global M did not open the reposition modal")
	}
	handled, cmd := m.WorkspacesKey(globalMoveKey('k'))
	if !handled || cmd == nil {
		t.Fatal("global modal boundary did not produce a toast command")
	}
	toast, ok := firstToast(cmd)
	if !ok || toast.Message != "already at the top" {
		t.Fatalf("boundary result = %#v, want top-boundary toast", toast)
	}
}

func globalMoveGridIDs(root *panelayout.Node) [][]int {
	grid := panelayout.GridOf(root)
	if grid == nil {
		return nil
	}
	out := make([][]int, grid.ColumnCount())
	for col := 1; col <= grid.ColumnCount(); col++ {
		out[col-1] = make([]int, grid.RowCount(col))
		for row := 1; row <= grid.RowCount(col); row++ {
			out[col-1][row-1] = grid.Cell(col, row).ID
		}
	}
	return out
}

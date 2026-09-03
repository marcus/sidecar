package workspace

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
)

func moveKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func TestProjectPaneMoveShortcutOpensModalFromPreviewAndList(t *testing.T) {
	root := t.TempDir()
	writeDocPaneFixture(t, root, "README.md", "# move\n")
	p := docPaneTestPlugin(t, root, false)
	p.openTerminalPath("README.md", 1)
	doc := panelayout.FirstOfKind(p.paneRoot, panelayout.Document)
	primary := panelayout.FirstOfKind(p.paneRoot, panelayout.Terminal)
	if doc == nil || primary == nil {
		t.Fatalf("test tree is incomplete: %+v", p.paneRoot)
	}

	p.focusLeaf(doc.ID)
	disableWorkspaceFeature(t, features.PaneMove.Name)
	p.handleKeyPress(moveKey('M'))
	if p.paneLayoutModal != nil {
		t.Fatal("pane_move turned off opened the modal")
	}
	if hasPaneMoveCommand(p.Commands(), panereposition.CommandMove) {
		t.Fatal("pane_move turned off still advertises pane reposition")
	}

	enableWorkspaceFeature(t, features.PaneMove.Name)
	if !hasPaneMoveCommand(p.Commands(), panereposition.CommandMove) {
		t.Fatal("project preview commands do not advertise pane reposition")
	}
	p.handleKeyPress(moveKey('M'))
	if p.paneLayoutModal == nil || p.paneLayoutModal.LeafID() != doc.ID || p.FocusContext() != panereposition.ModalContext {
		t.Fatalf("preview M opened modal=%v leaf=%d context=%q, want doc %d", p.paneLayoutModal != nil, p.paneLayoutModal.LeafID(), p.FocusContext(), doc.ID)
	}
	p.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})
	p.activePane = PaneSidebar
	if !hasPaneMoveCommand(p.Commands(), panereposition.CommandMove) {
		t.Fatal("project list commands do not advertise pane reposition")
	}
	p.handleKeyPress(moveKey('M'))
	if p.paneLayoutModal == nil || p.paneLayoutModal.LeafID() != primary.ID {
		t.Fatalf("list M targeted leaf %d, want Primary %d", p.paneLayoutModal.LeafID(), primary.ID)
	}
	p.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})
	p.focusLeaf(primary.ID)
	p.terminalSearch.InputActive = true
	p.handleKeyPress(moveKey('M'))
	if p.paneLayoutModal != nil || p.terminalSearch.Query() != "M" {
		t.Fatalf("project terminal search lost M: modal=%v query=%q", p.paneLayoutModal != nil, p.terminalSearch.Query())
	}
	p.terminalSearch.InputActive = false
	p.terminalSearch.SetQuery("")
	p.handleKeyPress(moveKey('M'))
	if p.paneLayoutModal == nil || p.paneLayoutModal.LeafID() != primary.ID {
		t.Fatal("focused non-interactive project terminal M did not open its modal")
	}
}

func hasPaneMoveCommand(commands []plugin.Command, id string) bool {
	for _, command := range commands {
		if command.ID == id {
			return true
		}
	}
	return false
}

func moveGridIDs(root *panelayout.Node) [][]int {
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

func TestProjectPaneMoveBoundaryUsesToast(t *testing.T) {
	p := docPaneTestPlugin(t, t.TempDir(), false)
	enableWorkspaceFeature(t, features.PaneMove.Name)
	p.handleKeyPress(moveKey('M'))
	p.handleKeyPress(moveKey('k'))
	if p.toastMessage != "already at the top" {
		t.Fatalf("boundary toast = %q", p.toastMessage)
	}
}

func TestProjectMovedShellSurvivesPassiveDeckReprojection(t *testing.T) {
	p := shellLeafTestPlugin(t, SplitRows)
	writeDocPaneFixture(t, p.ctx.WorkDir, "README.md", "# shell graft\n")
	p.openTerminalPath("README.md", 1)
	shell := p.shellLeaf()
	primary := panelayout.FirstOfKind(p.paneRoot, panelayout.Terminal)
	if shell == nil || primary == nil || p.contentDeck == nil {
		t.Fatalf("fixture lacks composed panes: %+v", p.paneRoot)
	}
	state := p.terminalPanes.Leaf(shell.ID)
	if state == nil || !state.Requested {
		t.Fatal("fixture lacks requested shell terminal state")
	}
	liveBefore := panelayout.LiveLeafCount(p.paneRoot)

	enableWorkspaceFeature(t, features.PaneMove.Name)
	p.focusLeaf(shell.ID)
	p.handleKeyPress(moveKey('M'))
	p.handleKeyPress(moveKey('l')) // draft Shell from under Primary to the doc column
	p.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	shapeBefore, ok := shellGraftShape(p.paneRoot, shell.ID)
	if !ok || shapeBefore.anchorID == primary.ID {
		t.Fatalf("Shell did not move away from Primary: %+v tree=%+v", shapeBefore, p.paneRoot)
	}

	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		t.Fatal("fixture has no selected terminal surface")
	}
	p.syncWorkspaceDeckProjection(root, surface)
	shapeAfter, ok := shellGraftShape(p.paneRoot, shell.ID)
	if !ok || shapeAfter != shapeBefore {
		t.Fatalf("deck projection changed Shell graft: before=%+v after=%+v", shapeBefore, shapeAfter)
	}
	if p.shellLeaf() != shell || p.shellLeaf().ID != shell.ID || p.terminalPanes.Leaf(shell.ID) != state {
		t.Fatal("deck projection replaced Shell leaf identity or terminal state")
	}
	if p.paneFocus != shell.ID || !p.shellLeafFocused() {
		t.Fatalf("deck projection lost Shell focus: focus=%d", p.paneFocus)
	}
	if got := panelayout.LiveLeafCount(p.paneRoot); got != liveBefore {
		t.Fatalf("live leaf count = %d, want %d", got, liveBefore)
	}
}

func TestProjectRestoredShellMoveUsesDeckOwnedPassiveIDs(t *testing.T) {
	p := docPaneTestPlugin(t, t.TempDir(), true)
	writeDocPaneFixture(t, p.ctx.WorkDir, "README.md", "# restored shell graft\n")
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		t.Fatal("fixture has no selected terminal surface")
	}
	stagedShellState := p.requireShellTermPane()
	stagedShellState.Session = termPanelSessionPrefix + "restored-move"
	layout := &state.PaneLayoutJSON{
		Root: root, Surface: surface, Open: true,
		Split: &state.PaneSplitJSON{Axis: "cols", Ratio: 62,
			A: &state.PaneLayoutJSON{Split: &state.PaneSplitJSON{Axis: "rows", Ratio: 41,
				A: &state.PaneLayoutJSON{Kind: contentKindTerminal},
				B: &state.PaneLayoutJSON{Kind: contentKindShell, Session: stagedShellState.Session},
			}},
			B: &state.PaneLayoutJSON{Kind: contentKindDoc, Tabs: []state.PaneDocTabJSON{{Path: "README.md"}}},
		},
	}
	p.restorePaneLayout(layout)

	shell := p.shellLeaf()
	doc := panelayout.FirstOfKind(p.paneRoot, panelayout.Document)
	primary := panelayout.FirstOfKind(p.paneRoot, panelayout.Terminal)
	if shell == nil || doc == nil || primary == nil || p.contentDeck == nil {
		t.Fatalf("restored tree is incomplete: %+v", p.paneRoot)
	}
	if p.contentDeck.Leaf(panelayout.Primary) != primary.ID || p.contentDeck.Leaf(panelayout.Document) != doc.ID {
		t.Fatalf("passive IDs diverged: host primary/doc=%d/%d deck=%d/%d", primary.ID, doc.ID,
			p.contentDeck.Leaf(panelayout.Primary), p.contentDeck.Leaf(panelayout.Document))
	}
	deckIDs := make(map[int]bool)
	var collectIDs func(*panelayout.Node)
	collectIDs = func(node *panelayout.Node) {
		if node == nil {
			return
		}
		deckIDs[node.ID] = true
		if node.Split != nil {
			collectIDs(node.Split.A)
			collectIDs(node.Split.B)
		}
	}
	collectIDs(p.contentDeck.Tree())
	if deckIDs[shell.ID] {
		t.Fatalf("restored Shell ID %d aliases Deck ownership", shell.ID)
	}
	var assertHostSplitsDistinct func(*panelayout.Node)
	assertHostSplitsDistinct = func(node *panelayout.Node) {
		if node == nil || node.Split == nil {
			return
		}
		if deckIDs[node.ID] {
			t.Fatalf("host split ID %d aliases Deck ownership", node.ID)
		}
		assertHostSplitsDistinct(node.Split.A)
		assertHostSplitsDistinct(node.Split.B)
	}
	assertHostSplitsDistinct(p.paneRoot)
	if got := p.terminalPanes.Leaf(shell.ID); got != stagedShellState {
		t.Fatal("restore replaced the staged Shell terminal state")
	}
	if stagedShellState.RowAnalyzer == nil {
		t.Fatal("restored staged Shell has no row analyzer")
	}
	stagedAnalyzer := stagedShellState.RowAnalyzer

	enableWorkspaceFeature(t, features.PaneMove.Name)
	p.focusLeaf(shell.ID)
	p.handleKeyPress(moveKey('M'))
	p.handleKeyPress(moveKey('l'))
	p.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	shapeBefore, moved := shellGraftShape(p.paneRoot, shell.ID)
	if !moved || shapeBefore.anchorID == primary.ID {
		t.Fatalf("restored Shell move did not leave Primary: %+v", shapeBefore)
	}
	liveBefore := panelayout.LiveLeafCount(p.paneRoot)
	p.syncWorkspaceDeckProjection(root, surface)
	shapeAfter, restored := shellGraftShape(p.paneRoot, shell.ID)
	if !restored || shapeAfter != shapeBefore {
		t.Fatalf("restored-tree projection changed Shell graft: before=%+v after=%+v", shapeBefore, shapeAfter)
	}
	if p.shellLeaf() != shell || p.terminalPanes.Leaf(shell.ID) != stagedShellState || p.paneFocus != shell.ID || !p.shellLeafFocused() {
		t.Fatal("restored-tree projection lost Shell leaf/state/focus identity")
	}
	if stagedShellState.RowAnalyzer != stagedAnalyzer {
		t.Fatal("restored-tree projection replaced the Shell row analyzer")
	}
	if got := panelayout.LiveLeafCount(p.paneRoot); got != liveBefore {
		t.Fatalf("restored-tree live count = %d, want %d", got, liveBefore)
	}
}

type testShellGraftShape struct {
	anchorID   int
	axis       panelayout.Axis
	ratio      int
	shellFirst bool
}

func shellGraftShape(root *panelayout.Node, shellID int) (testShellGraftShape, bool) {
	parent := panelayout.Find(root, parentSplitID(root, shellID))
	if parent == nil || parent.Split == nil {
		return testShellGraftShape{}, false
	}
	shape := testShellGraftShape{axis: parent.Split.Axis, ratio: parent.Split.Ratio}
	if parent.Split.A.ID == shellID {
		shape.shellFirst, shape.anchorID = true, parent.Split.B.ID
		return shape, true
	}
	if parent.Split.B.ID == shellID {
		shape.anchorID = parent.Split.A.ID
		return shape, true
	}
	return testShellGraftShape{}, false
}

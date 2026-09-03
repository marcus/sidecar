package workspace

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/panelayout"
)

func sidebarTarget() panelayout.Target {
	return panelayout.Target{Kind: panelayout.TargetSidebar}
}

func leafTarget(id int) panelayout.Target {
	return panelayout.Target{Kind: panelayout.TargetLeaf, Leaf: id}
}

func panelTarget(p *Plugin) panelayout.Target {
	return leafTarget(p.shellLeaf().ID)
}

// assertFocus reads the three fields the frame draws focus from rather than the
// setter's own answer, so a target that never reached the state it names fails
// here instead of agreeing with itself.
func assertFocus(t *testing.T, p *Plugin, want panelayout.Target, step string) {
	t.Helper()
	if want.Kind == panelayout.TargetSidebar {
		if p.activePane != PaneSidebar || p.shellLeafFocused() {
			t.Fatalf("%s: pane=%v panelFocused=%v, want the sidebar", step, p.activePane, p.shellLeafFocused())
		}
		return
	}
	if leaf := p.shellLeaf(); leaf != nil && want.Leaf == leaf.ID {
		if p.activePane != PanePreview || !p.shellLeafFocused() || p.paneFocus != leaf.ID {
			t.Fatalf("%s: pane=%v focus=%d panelFocused=%v, want the terminal panel",
				step, p.activePane, p.paneFocus, p.shellLeafFocused())
		}
		return
	}
	if p.activePane != PanePreview || p.shellLeafFocused() || p.paneFocus != want.Leaf {
		t.Fatalf("%s: pane=%v focus=%d panelFocused=%v, want leaf %d",
			step, p.activePane, p.paneFocus, p.shellLeafFocused(), want.Leaf)
	}
}

func tabKey() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyTab} }
func shiftTabKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func walkFocus(t *testing.T, p *Plugin, key tea.KeyPressMsg, want []panelayout.Target, label string) {
	t.Helper()
	for i, target := range want {
		p.handleListKeys(key)
		assertFocus(t, p, target, fmt.Sprintf("%s step %d", label, i+1))
	}
}

// The panel is the window the hand-written cycler could not reach: it reset
// termPanelFocused on every move, so Tab walked past it forever. The full ring
// in both directions is that regression's test.
func TestTabCyclesEveryVisibleWindowIncludingTheTerminalPanel(t *testing.T) {
	stubTd(t)
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	steelThreadPaneTree(t, p, root)
	p.sidebarVisible = true
	showTermPanel(t, p, SplitRows, 50)
	p.setFocusTarget(sidebarTarget())

	forward := []panelayout.Target{leafTarget(1), leafTarget(2), leafTarget(3), panelTarget(p), sidebarTarget()}
	walkFocus(t, p, tabKey(), forward, "tab")

	reverse := []panelayout.Target{panelTarget(p), leafTarget(3), leafTarget(2), leafTarget(1), sidebarTarget()}
	walkFocus(t, p, shiftTabKey(), reverse, "shift+tab")
}

// Focusing the panel is an explicit navigation of it, so its window stops being
// pinned where a document left it. Without the thaw the panel arrives frozen
// and the first key moves nothing.
func TestTabToTheTerminalPanelThawsItsWindow(t *testing.T) {
	stubTd(t)
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	steelThreadPaneTree(t, p, root)
	p.sidebarVisible = false
	showTermPanel(t, p, SplitRows, 50)
	p.setFocusTarget(leafTarget(3))
	p.pinTerminalWindow(true, 4, true)
	if !p.requireShellTermPane().Freeze.Active() {
		t.Fatal("the panel window did not pin")
	}

	p.handleListKeys(tabKey())
	assertFocus(t, p, panelTarget(p), "tab to panel")
	if p.requireShellTermPane().Freeze.Active() || p.requireShellTermPane().FreezeDoc {
		t.Fatalf("panel focus arrived frozen: active=%v doc=%v", p.requireShellTermPane().Freeze.Active(), p.requireShellTermPane().FreezeDoc)
	}
}

func TestTabCyclesABareTerminalWithAndWithoutTheSidebar(t *testing.T) {
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	p.sidebarVisible = true
	p.setFocusTarget(leafTarget(1))

	p.handleListKeys(tabKey())
	assertFocus(t, p, sidebarTarget(), "terminal to sidebar")
	p.handleListKeys(tabKey())
	assertFocus(t, p, leafTarget(1), "sidebar to terminal")

	// A hidden sidebar leaves one window, and a ring of one has nowhere to go:
	// Tab must leave the terminal focused rather than blanking the surface.
	p.sidebarVisible = false
	p.handleListKeys(tabKey())
	assertFocus(t, p, leafTarget(1), "bare terminal tab")
	p.handleListKeys(shiftTabKey())
	assertFocus(t, p, leafTarget(1), "bare terminal shift+tab")
}

// With the workspace_doc_panes flag off there is never a pane tree, so the ring
// has to fall back to the two windows the surface actually draws. Otherwise Tab
// dies on a ring of one and the preview becomes unreachable once left.
func TestTabCyclesTheSidebarAndPreviewWithNoPaneTree(t *testing.T) {
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	p.paneRoot = nil
	p.paneFocus = 0
	p.sidebarVisible = true
	p.setFocusTarget(sidebarTarget())

	p.handleListKeys(tabKey())
	assertFocus(t, p, leafTarget(0), "no tree: sidebar to preview")
	p.handleListKeys(tabKey())
	assertFocus(t, p, sidebarTarget(), "no tree: preview to sidebar")
	p.handleListKeys(shiftTabKey())
	assertFocus(t, p, leafTarget(0), "no tree: sidebar back to preview")
}

// A focused Diff leaf owns intra-window focus (file list ↔ hunks). Tab walks
// the tree and must not change that intra-leaf focus.
func TestTabOnFocusedDiffLeafLeavesIntraFocusUntouched(t *testing.T) {
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, false)
	p.sidebarVisible = true
	if cmd := p.showDiffCmd(); cmd == nil {
		t.Fatal("show-diff opened nothing")
	}
	view := p.activeDiffView()
	if view == nil {
		t.Fatal("no Diff view")
	}
	view.Focus = DiffTabFocusDiff
	p.setFocusTarget(sidebarTarget())

	p.handleListKeys(tabKey())
	if view.Focus != DiffTabFocusDiff {
		t.Fatalf("tab moved diff focus to %v", view.Focus)
	}
}

// A terminal being typed into owns Tab. The exception is structural: the
// interactive mode dispatches before the list keys ever see the key.
func TestInteractiveModeTabDoesNotMoveTheFocusTarget(t *testing.T) {
	stubTd(t)
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	steelThreadPaneTree(t, p, root)
	p.sidebarVisible = true
	p.setFocusTarget(leafTarget(1))
	p.viewMode = ViewModeInteractive
	p.interactiveState = &InteractiveState{Active: true, TargetPane: "%1", TargetSession: "focus-test"}
	t.Cleanup(p.stopTerminalModels)

	p.handleKeyPress(tabKey())
	assertFocus(t, p, leafTarget(1), "interactive tab")
	p.handleKeyPress(shiftTabKey())
	assertFocus(t, p, leafTarget(1), "interactive shift+tab")
}

// Every click writes focus through the one setter, so whatever was clicked ends
// up focused and whatever held it before does not.
func TestClickFocusesTheWindowUnderThePointer(t *testing.T) {
	stubTd(t)
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	steelThreadPaneTree(t, p, root)
	p.sidebarVisible = true
	showTermPanel(t, p, SplitRows, 50)
	p.setFocusTarget(sidebarTarget())

	clicks := []struct {
		name   string
		region mouse.Region
		want   panelayout.Target
	}{
		{"doc leaf", mouse.Region{ID: regionPaneLeaf, Data: 2}, leafTarget(2)},
		{"issue leaf", mouse.Region{ID: regionPaneLeaf, Data: 3}, leafTarget(3)},
		{"terminal panel", mouse.Region{ID: regionTermPanelContent}, panelTarget(p)},
		{"preview terminal", mouse.Region{ID: regionPreviewPane}, leafTarget(1)},
		{"sidebar", mouse.Region{ID: regionSidebar}, sidebarTarget()},
	}
	for _, click := range clicks {
		before := p.currentFocusTarget()
		region := click.region
		p.handleMouseClick(mouse.MouseAction{Type: mouse.ActionClick, Region: &region})
		assertFocus(t, p, click.want, "click on the "+click.name)
		if before == click.want {
			t.Fatalf("click on the %s started from the window it was meant to move focus to", click.name)
		}
		if got := p.currentFocusTarget(); got == before {
			t.Fatalf("click on the %s left focus on %+v", click.name, before)
		}
	}
}

// Tab used to be swallowed by a live terminal search input. Now that it walks
// the ring unconditionally, it has to close the input on the way out, or the
// search box keeps drawing a cursor for keys it will never receive.
func TestTabLeavesALiveTerminalSearchInput(t *testing.T) {
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	p.sidebarVisible = true
	p.setFocusTarget(leafTarget(1))
	p.terminalSearch.InputActive = true
	p.terminalSearch.SetQuery("err")

	p.handleListKeys(tabKey())

	if p.terminalSearch.InputActive {
		t.Fatal("tab left the terminal search input taking keystrokes")
	}
	if p.terminalSearch.Query() != "err" {
		t.Fatalf("tab dropped the search query: %q", p.terminalSearch.Query())
	}
	assertFocus(t, p, sidebarTarget(), "terminal search to sidebar")
}

// Covering the plugin (a tab switch) used to restore the window interactive
// mode was entered from, so typing in the shell and switching to Git put the
// ring back on the list. Covering is not a navigation of this surface's
// windows: the live pane ends, and the shell keeps the ring.
func TestPluginBlurKeepsTheLiveTerminalFocused(t *testing.T) {
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	p.sidebarVisible = true
	p.setFocusTarget(sidebarTarget())
	term := terminalLeafID(p.paneRoot)
	p.viewMode = ViewModeInteractive
	p.interactiveState = &InteractiveState{Active: true, PaneOnEntry: PaneSidebar, TargetSession: "focus-blur"}
	p.activePane = PanePreview
	p.paneFocus = term
	t.Cleanup(p.stopTerminalModels)

	p.SetFocused(false)
	if p.viewMode == ViewModeInteractive {
		t.Fatal("plugin blur left the live pane holding the keyboard")
	}
	p.SetFocused(true)
	assertFocus(t, p, leafTarget(term), "return from another plugin")
}

// Esc is a navigation back, so Enter-from-the-list still returns to the list.
func TestLeavingInteractiveModeRestoresTheWindowItWasEnteredFrom(t *testing.T) {
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	p.sidebarVisible = true
	p.setFocusTarget(sidebarTarget())
	p.viewMode = ViewModeInteractive
	p.interactiveState = &InteractiveState{Active: true, PaneOnEntry: PaneSidebar, TargetSession: "focus-esc"}
	p.activePane = PanePreview
	p.paneFocus = terminalLeafID(p.paneRoot)
	t.Cleanup(p.stopTerminalModels)

	p.leaveInteractiveMode()
	assertFocus(t, p, sidebarTarget(), "esc from a live pane entered on the list")
}

// Opening a document leaves the content deck focused on that leaf. Tab onto
// the shell, then project the deck (the live-refresh path that runs when the
// plugin becomes visible again). The ring must stay on the shell — copying
// deck.FocusedLeaf() was how an extra panel stole the keyboard on return.
func TestDeckProjectionKeepsFocusOnTheTerminal(t *testing.T) {
	root := t.TempDir()
	writeDocPaneFixture(t, root, "README.md", "# readme\n")
	p := docPaneTestPlugin(t, root, true)
	p.sidebarVisible = true
	rootPath, surface, ok := p.selectedTerminalSurface()
	if !ok {
		t.Fatal("no selected terminal surface")
	}
	if cmd := p.openDocPaneForSurface(rootPath, surface, "README.md", 0); cmd == nil {
		t.Fatal("open produced no command")
	}
	if p.contentDeck == nil {
		t.Fatal("document pane did not open a content deck")
	}
	term := terminalLeafID(p.paneRoot)
	p.setFocusTarget(leafTarget(term))
	assertFocus(t, p, leafTarget(term), "tab/click to the shell")
	if p.contentDeck.FocusedLeaf() != term {
		t.Fatalf("deck focus = %d, want the terminal %d", p.contentDeck.FocusedLeaf(), term)
	}

	p.applyWorkspaceDeckBroadcast(struct{}{})
	assertFocus(t, p, leafTarget(term), "after a deck projection")
}

// The panel is a window only while it is drawn. When the preview shrinks past
// the split's minimum the renderer falls back to output-only, and Tab must not
// park focus on a panel that is not on screen.
func TestTabSkipsTheTerminalPanelWhenTheSplitDoesNotFit(t *testing.T) {
	stubTd(t)
	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	steelThreadPaneTree(t, p, root)
	// The sidebar stays off so the preview keeps a real leaf box as the window
	// shrinks; without one the split sizes itself from default dimensions and
	// always fits.
	p.sidebarVisible = false
	showTermPanel(t, p, SplitCols, 50)

	p.View(p.width, p.height)
	if !p.termPanelOnScreen() {
		t.Fatal("fixture should draw the panel before the window shrinks")
	}
	// A column split needs two floored boxes plus a divider; a preview this
	// narrow is short of that, so the frame zooms to the focused leaf and the
	// panel is nowhere on screen.
	for _, size := range [][2]int{{80, 24}, {60, 20}, {50, 16}} {
		p.width, p.height = size[0], size[1]
		p.View(p.width, p.height)
	}
	if _, drawn := p.shellLeafBox(); drawn {
		t.Fatal("fixture failed to produce a split that does not fit")
	}
	if p.termPanelOnScreen() {
		t.Fatal("panel still counted as on screen after the split stopped fitting")
	}

	for _, target := range p.focusRing() {
		if leaf := p.shellLeaf(); leaf != nil && target.Leaf == leaf.ID {
			t.Fatalf("undrawn panel is still in the ring: %+v", p.focusRing())
		}
	}

	// Tab off the last leaf wraps to the first one rather than stopping at the
	// panel on the way round.
	p.setFocusTarget(leafTarget(3))
	p.handleListKeys(tabKey())
	assertFocus(t, p, leafTarget(1), "tab past the undrawn panel")
}

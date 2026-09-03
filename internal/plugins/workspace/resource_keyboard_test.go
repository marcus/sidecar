package workspace

import (
	"testing"

	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/resourceview"
)

// The workspace plugin reports the pane's keyboard ownership to the host, which
// is what stops a host global being taken out of the middle of a collection
// pane's query. Its twin is TestSessionsSurfaceReportsPaneKeyboardOwnership in
// internal/overview: the two projections answer one question, so a fix landing
// in one and not the other is a failing test rather than a silent parity gap.
var _ plugin.KeyRouter = (*Plugin)(nil)

// focusedCollectionPane opens a collection tab and focuses its leaf.
func focusedCollectionPane(t *testing.T) (*Plugin, *collectionCalls) {
	t.Helper()
	p, calls, root, surface := collectionTestPlugin(t)
	runDeckCmd(t, p, p.openRequestedResourcePaneForSurface(root, surface, resourceview.Ref{
		Instance: "recall", Collection: "results",
	}))
	res, leaf := p.activeResourcePane()
	if res == nil || leaf == nil {
		t.Fatal("no Resource leaf opened for a collection")
	}
	p.paneFocus, p.activePane = leaf.ID, PanePreview
	if !p.resourceFocused() {
		t.Fatal("the Resource leaf is not focused")
	}
	return p, calls
}

func TestAQueryLineInACollectionPaneOwnsTheKeyboard(t *testing.T) {
	p, _ := focusedCollectionPane(t)

	if p.ConsumesTextInput() {
		t.Fatal("the pane claims text input before anything is being typed into it")
	}
	if handled, _ := p.handleResourceKey(keyPressMsg("/")); !handled {
		t.Fatal("`/` was not handled by the focused Resource leaf")
	}
	if !p.ConsumesTextInput() {
		t.Fatal("with the query line open the plugin does not report text input; " +
			"the host's tab digits, ` and [ would be taken out of the query")
	}

	// The digit the host would otherwise have spent switching project tabs.
	if handled, _ := p.handleResourceKey(keyPressMsg("1")); !handled {
		t.Fatal("a digit typed into an open query was not handled")
	}
	view := p.focusedResourceTabs().Active()
	if got := view.Browser().PaneQuery(); got != "1" {
		t.Fatalf("query = %q after typing into it, want %q", got, "1")
	}

	// Leaving the query hands the keys back.
	if handled, _ := p.handleResourceKey(keyPressMsg("esc")); !handled {
		t.Fatal("esc in the query line was not handled")
	}
	if p.ConsumesTextInput() {
		t.Fatal("the pane still claims text input after the query line closed")
	}
}

func TestAnOverlayInACollectionPaneBlocksHostGlobals(t *testing.T) {
	p, _ := focusedCollectionPane(t)

	if p.BlocksGlobalKeys() {
		t.Fatal("the pane blocks host globals with no overlay open")
	}
	if handled, _ := p.handleResourceKey(keyPressMsg("v")); !handled {
		t.Fatal("`v` was not handled by the focused Resource leaf")
	}
	if !p.BlocksGlobalKeys() {
		t.Fatal("with the View control open the plugin does not block host globals; " +
			"K, W, # and the tab digits would reach the host past the overlay")
	}
	if handled, _ := p.handleResourceKey(keyPressMsg("esc")); !handled {
		t.Fatal("esc did not close the overlay")
	}
	if p.BlocksGlobalKeys() {
		t.Fatal("the pane still blocks host globals after the overlay closed")
	}
}

// Level 3: the browser's own keys are claimed while its pane has the keyboard,
// so a host global bound to one of them later cannot quietly win.
func TestACollectionPaneClaimsTheBrowsersKeys(t *testing.T) {
	p, _ := focusedCollectionPane(t)

	for _, key := range []string{"j", "k", "enter", "r", "o"} {
		if !p.ClaimsKey(key) {
			t.Fatalf("a focused collection pane does not claim %q", key)
		}
	}
	// The browser's control keys are claimed where they act. This collection
	// declares a search and a view, so `/` and `v` are the pane's; it declares
	// no action, so `a` is inert and stays the host's rather than being
	// swallowed for nothing (td-fcb648).
	for _, key := range []string{"/", "v"} {
		if !p.ClaimsKey(key) {
			t.Fatalf("a collection that declares one does not claim %q", key)
		}
	}
	if p.ClaimsKey("a") {
		t.Fatal("a collection with no applicable action still claims `a`")
	}
	// Keys the pane does not own stay the host's.
	for _, key := range []string{"K", "@", "#", "1", "n", "tab"} {
		if p.ClaimsKey(key) {
			t.Fatalf("a focused collection pane claims %q, which is the host's", key)
		}
	}

	// Nothing is claimed when the keyboard is somewhere else.
	p.activePane = PaneSidebar
	if p.ClaimsKey("j") {
		t.Fatal("the pane claims keys while the sidebar has the keyboard")
	}
}

// QuitKeyExits is the answer the host's own root-context list gave before this
// plugin routed its own keys. Stating it here must not change it.
func TestQuitStillExitsFromTheListAndThePreviewOnly(t *testing.T) {
	p, _ := focusedCollectionPane(t)
	if p.QuitKeyExits() {
		t.Fatalf("q quits sidecar from %q; the pane answers it itself", p.FocusContext())
	}

	p.activePane = PaneSidebar
	p.paneFocus = 0
	if ctx := p.FocusContext(); ctx != "workspace-list" {
		t.Fatalf("focus context = %q, want the list", ctx)
	}
	if !p.QuitKeyExits() {
		t.Fatal("q no longer quits sidecar from the workspace list")
	}
}

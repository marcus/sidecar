package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
)

// One selection at a time is the screen's rule, not the deck's. The plugin
// under the deck can draw a selectable box inside its own frame — the shared
// plugin browser's detail box is the one that does — and it is on screen beside
// the deck's content panes. Two live highlights would leave the copy chord,
// which follows the keyboard rather than the highlight, answering for whichever
// of the two the reader is not looking at.
//
// Both directions are asserted, because the two halves are wired in different
// places: the deck sweeps the plugin when a pane press arms a gesture, and it
// notices the plugin's own gesture only by seeing that the plugin holds a
// selection it did not hold before the pointer event.

func primarySelectionDeckFixture(t *testing.T, p *deckHostTestPlugin) (*Model, *appContentDeck, int, *issueview.Model) {
	t.Helper()
	root := t.TempDir()
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 40)
	if cmd := m.openAppContent(root, p.id, contentlink.Ref{Kind: contentlink.KindIssue, Value: "td-22f35f"}); cmd == nil {
		t.Fatal("issue open returned no load command")
	}
	m.renderContent(200, 40)
	h := m.currentContentDeck()
	if h == nil {
		t.Fatal("content deck was not created")
	}
	view := seedDeckIssue(t, h)
	m.renderContent(200, 40)
	leaf := h.deck.Leaf(panelayout.Issue)
	if leaf == 0 {
		t.Fatal("issue leaf is not open")
	}
	return m, h, leaf, view
}

// dragOverDeckIssueCard drags over two rows of the card's body, over cells that
// are text rather than one of the card's own targets.
func dragOverDeckIssueCard(t *testing.T, m *Model, h *appContentDeck, leaf int) {
	t.Helper()
	inner := appDeckInnerBox(t, h, leaf)
	x := inner.X + 4
	y := inner.Y + paneframe.HeaderRows + 3
	m.appContentMouse(deckClick(x, y))
	m.appContentMouse(deckDragTo(x+8, y+1))
}

func TestAPaneSelectionDropsThePluginsOwnHighlight(t *testing.T) {
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "plain", selection: true}
	m, h, leaf, view := primarySelectionDeckFixture(t, p)

	dragOverDeckIssueCard(t, m, h, leaf)

	if len(view.SelectionText()) == 0 {
		t.Fatal("the drag over the card selected nothing, so the sweep was never asked for")
	}
	if p.selection {
		t.Fatal("a selection started in a hosted pane left the plugin's own highlight on screen")
	}
}

// The other direction, over a document pane rather than an issue card. The card
// would not prove it: an issue card is the one viewer the deck propagates focus
// to, and losing focus already drops its selection through its own render key,
// so the sweep would be invisible behind that. A document pane keeps its
// selection when focus leaves, which is what makes it the honest witness.
func TestThePluginsOwnSelectionDropsAPanesHighlight(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Rendered heading\n\nRendered body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "primary"}
	m := openAppDeckDocument(t, appDeckTestModel(t, root, p), root, p.id, "README.md")
	m.renderContent(180, 40)
	h := m.currentContentDeck()
	view := h.deck.Viewer(h.deck.Leaf(panelayout.Document)).(*docview.Model)

	rect := view.ContentLinkRect()
	m.appContentMouse(deckClick(rect.X, rect.Y))
	m.appContentMouse(deckDragTo(rect.X+12, rect.Y+1))
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: rect.X + 12, Y: rect.Y + 1, Button: tea.MouseLeft}))
	if !view.HasSelection() {
		t.Fatal("the drag over the document selected nothing")
	}

	// The plugin's own box answers the next press with a selection of its own.
	// The press also moves focus onto the Primary leaf, which is what routes it
	// to the plugin.
	p.selectOnPress = true
	m.appContentMouse(deckClick(h.primaryInner.X+1, h.primaryInner.Y+1))

	if !p.selection {
		t.Fatal("the fixture plugin did not start a selection, so nothing was swept")
	}
	if view.HasSelection() {
		t.Fatalf("the plugin's own selection left the document's highlight on screen: %q", view.SelectionText())
	}
}

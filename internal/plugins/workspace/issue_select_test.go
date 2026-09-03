package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/issueview"
)

// issueCardInner is the issue leaf's content box: the placed leaf minus the
// panel chrome around it.
func issueCardInner(t *testing.T, p *Plugin) Box {
	t.Helper()
	return insetPanelChrome(issueLeafBox(t, p, 100, 24))
}

// issueTextCell is the screen position of a column of an issue card row, past
// the leaf's chrome, its header row and the card's own left padding.
func issueTextCell(t *testing.T, p *Plugin, col, row int) (int, int) {
	t.Helper()
	inner := issueCardInner(t, p)
	return inner.X + 1 + col, inner.Y + terminalHeaderRows + row
}

// A drag over the card's text selects the rows it covers, through this host's
// own input path.
func TestIssueLeafDragSelectsTextThroughHost(t *testing.T) {
	p, issue, _, _ := issueScrollbarFixture(t)
	view := issue.view()

	x, y := issueTextCell(t, p, 0, 0)
	p.handleMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	if p.docSelectLeaf == 0 {
		t.Fatal("the press over card text armed no selection gesture")
	}
	p.handleMouse(tea.MouseMotionMsg(tea.Mouse{X: x + 6, Y: y + 1, Button: tea.MouseLeft}))
	p.handleMouse(tea.MouseReleaseMsg(tea.Mouse{X: x + 6, Y: y + 1, Button: tea.MouseLeft}))

	lines := view.SelectionText()
	if len(lines) != 2 {
		t.Fatalf("selected %d rows (%q), want the two the drag covered", len(lines), lines)
	}
	if strings.TrimSpace(strings.Join(lines, "")) == "" {
		t.Fatalf("the drag selected only blank cells: %q", lines)
	}
	if p.docSelectLeaf != 0 {
		t.Fatal("the release left the host holding the gesture")
	}
}

// The card's own rows keep their clicks: a press on a subtask row navigates as
// it always did and arms no selection.
func TestIssueLeafNavRowClickIsStillNavigation(t *testing.T) {
	p, issue, _, _ := issueScrollbarFixture(t)
	view := issue.view()
	view.Scroll(-view.ScrollOffset())
	_ = p.renderListView(p.width, p.height)

	var hit *issueview.Hit
	for _, h := range view.Hits() {
		copied := h
		hit = &copied
		break
	}
	if hit == nil {
		t.Skip("the fixture card drew no navigable rows")
	}
	inner := issueCardInner(t, p)
	x := inner.X + hit.X
	y := inner.Y + terminalHeaderRows + hit.Y

	p.handleMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	if p.docSelectLeaf != 0 {
		t.Fatal("a press on a navigable row armed a selection instead of clicking it")
	}
	if view.HasSelection() {
		t.Fatal("a press on a navigable row selected text")
	}
}

// A modal swallows the release, so the gesture behind it ends at the same
// boundary rather than answering the next unrelated drag.
func TestAModalAbandonsTheIssueSelection(t *testing.T) {
	p, issue, _, _ := issueScrollbarFixture(t)
	view := issue.view()

	x, y := issueTextCell(t, p, 0, 0)
	p.handleMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	p.handleMouse(tea.MouseMotionMsg(tea.Mouse{X: x + 6, Y: y, Button: tea.MouseLeft}))
	if !view.HasSelection() {
		t.Fatal("the drag selected nothing to abandon")
	}

	p.viewMode = ViewModeConfirmDelete
	_ = p.View(p.width, p.height)
	if p.docSelectLeaf != 0 {
		t.Fatal("the modal left the host holding a live gesture")
	}
	if view.HasSelection() {
		t.Fatal("the selection survived the modal that took the keyboard")
	}
	// The gesture is over: a later drag does not start selecting again.
	p.handleMouse(tea.MouseMotionMsg(tea.Mouse{X: x + 20, Y: y + 2, Button: tea.MouseLeft}))
	if view.HasSelection() {
		t.Fatal("a drag after the modal extended a selection the user had finished")
	}
}

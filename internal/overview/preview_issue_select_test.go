package overview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/termpreview"
)

// previewIssueTextPoint reports the screen point of a column of a card row,
// past the leaf's header row and the card's own left padding.
func previewIssueTextPoint(t *testing.T, m *Model, col, row int) (int, int) {
	t.Helper()
	m.WorkspacesView(previewWide, previewTall)
	for _, region := range m.workspacesMouse.HitMap.Regions() {
		if region.Data == previewIssueRegionKind {
			return region.Rect.X + 1 + col, region.Rect.Y + termpreview.HeaderRows + row
		}
	}
	t.Fatal("the preview registered no issue region")
	return 0, 0
}

// A drag over the card's text selects the rows it covers, through this
// surface's own input path — the parity half of the project workspace's test.
func TestPreviewIssueDragSelectsTextThroughHost(t *testing.T) {
	m, issue := openLongPreviewIssue(t)
	view := issue.view()

	x, y := previewIssueTextPoint(t, m, 0, 0)
	run(t, m, m.WorkspacesMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}))
	if got := m.workspacesMouse.DragRegion(); got != previewPaneSelectKind {
		t.Fatalf("the press over card text started drag %q, want %s", got, previewPaneSelectKind)
	}
	run(t, m, m.WorkspacesMouse(tea.MouseMotionMsg{X: x + 6, Y: y + 1, Button: tea.MouseLeft}))
	run(t, m, m.WorkspacesMouse(tea.MouseReleaseMsg{X: x + 6, Y: y + 1, Button: tea.MouseLeft}))

	lines := view.SelectionText()
	if len(lines) != 2 {
		t.Fatalf("selected %d rows (%q), want the two the drag covered", len(lines), lines)
	}
	if strings.TrimSpace(strings.Join(lines, "")) == "" {
		t.Fatalf("the drag selected only blank cells: %q", lines)
	}
}

// A release lost off-window ends the gesture on the next button-less motion,
// the same boundary every other gesture on this surface settles at.
func TestPreviewIssueSelectionLostReleaseRecoversOnHover(t *testing.T) {
	m, issue := openLongPreviewIssue(t)
	view := issue.view()

	x, y := previewIssueTextPoint(t, m, 0, 0)
	run(t, m, m.WorkspacesMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}))
	run(t, m, m.WorkspacesMouse(tea.MouseMotionMsg{X: x + 6, Y: y, Button: tea.MouseLeft}))
	if !view.HasSelection() {
		t.Fatal("the drag selected nothing")
	}

	run(t, m, m.WorkspacesMouse(tea.MouseMotionMsg{X: 1, Y: 1}))
	if m.workspacesMouse.IsDragging() {
		t.Fatal("a button-less motion left the shared handler dragging")
	}
	// The gesture is over: a later drag does not extend the selection.
	before := strings.Join(view.SelectionText(), "\n")
	run(t, m, m.WorkspacesMouse(tea.MouseMotionMsg{X: x + 20, Y: y + 2, Button: tea.MouseLeft}))
	if after := strings.Join(view.SelectionText(), "\n"); after != before {
		t.Fatalf("a drag after the lost release extended the selection: %q -> %q", before, after)
	}
}

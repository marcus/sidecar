package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/noteview"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/ui"
)

// scrollbarDeckFixture opens an overflowing document pane beside the plugin
// surface and reports the deck, the document viewer, and its leaf.
func scrollbarDeckFixture(t *testing.T, name, body string) (*Model, *appContentDeck, *docview.Model) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "plain"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 40)
	if cmd := m.openAppContent(root, p.id, contentlink.Ref{Kind: contentlink.KindFile, Value: name}); cmd == nil {
		t.Fatal("document open returned no load command")
	}
	m.renderContent(200, 40)
	h := m.currentContentDeck()
	if h == nil || h.deck.Leaf(panelayout.Document) == 0 {
		t.Fatalf("document leaf did not open: %+v", h)
	}
	docLeaf := h.deck.Leaf(panelayout.Document)
	doc, ok := h.deck.Viewer(docLeaf).(*docview.Model)
	if !ok {
		t.Fatalf("document viewer = %T", h.deck.Viewer(docLeaf))
	}
	// The open only queued a load; give the viewer its rows directly.
	loaded, ok := doc.Load(999, root, name, 0, h.deck.Context().Epoch)().(docview.LoadedMsg)
	if !ok || loaded.Result.Error != nil || !doc.SetResult(loaded) {
		t.Fatalf("document fixture did not load: ok=%v msg=%+v", ok, loaded)
	}
	if doc.Rendered() {
		// Line zero defaults to rendered; raw text keeps one row per line.
		doc.ToggleRenderMode()
	}
	// Re-render so the hit map carries this frame's bar regions.
	m.renderContent(200, 40)
	return m, h, doc
}

// seedScrollbarDeckViewer loads a hosted card directly, the way
// TestAppContentDeckResolvedAbsoluteDocumentKeepsCanonicalPath does for
// documents: the model's own Load supplies the matching identity, the test
// supplies the data.
func seedDeckIssue(t *testing.T, h *appContentDeck) *issueview.Model {
	t.Helper()
	leaf := h.deck.Leaf(panelayout.Issue)
	if leaf == 0 {
		t.Fatal("issue leaf is not open")
	}
	view, ok := h.deck.Viewer(leaf).(*issueview.Model)
	if !ok {
		t.Fatalf("issue viewer = %T", h.deck.Viewer(leaf))
	}
	loaded, ok := view.Load(777, h.workdir, "td-fixture", h.deck.Context().Epoch)().(issueview.LoadedMsg)
	if !ok {
		t.Fatal("issue load produced no message")
	}
	loaded.Data = &issueview.Data{ID: "td-fixture", Title: "Fixture", Description: strings.Repeat("description paragraph\n\n", 40)}
	loaded.Error = nil // nothing was fetched here; the test supplies the data
	if !view.SetResult(loaded) {
		t.Fatal("issue fixture result was rejected")
	}
	return view
}

func seedDeckNote(t *testing.T, h *appContentDeck) *noteview.Model {
	t.Helper()
	leaf := h.deck.Leaf(panelayout.Note)
	if leaf == 0 {
		t.Fatal("note leaf is not open")
	}
	view, ok := h.deck.Viewer(leaf).(*noteview.Model)
	if !ok {
		t.Fatalf("note viewer = %T", h.deck.Viewer(leaf))
	}
	loaded, ok := view.Load(888, h.workdir, "nt-fixture", h.deck.Context().Epoch)().(noteview.LoadedMsg)
	if !ok {
		t.Fatal("note load produced no message")
	}
	loaded.Data = &noteview.Data{ID: "nt-fixture", Title: "Fixture", Content: strings.Repeat("note paragraph line\n\n", 40)}
	loaded.Error = nil // nothing was fetched here; the test supplies the data
	if !view.SetResult(loaded) {
		t.Fatal("note fixture result was rejected")
	}
	return view
}

func appDeckScrollbarRegions(h *appContentDeck, id string) []mouse.Region {
	var found []mouse.Region
	for _, region := range h.mouse.HitMap.Regions() {
		if region.ID == id {
			found = append(found, region)
		}
	}
	return found
}

func findDeckBarRegion(t *testing.T, h *appContentDeck, leafID int, id string) mouse.Region {
	t.Helper()
	for _, region := range appDeckScrollbarRegions(h, id) {
		if hit, ok := region.Data.(appDeckScrollbarHit); ok && hit.LeafID == leafID {
			return region
		}
	}
	t.Fatalf("leaf %d has no %s region; regions=%d", leafID, id, len(h.mouse.HitMap.Regions()))
	return mouse.Region{}
}

func appDeckInnerBox(t *testing.T, h *appContentDeck, leafID int) paneframe.Box {
	t.Helper()
	for _, placement := range h.layout.Leaves {
		if placement.Node != nil && placement.Node.ID == leafID {
			return paneframe.GeometryForChrome(placement.Box, appDeckHost{h}.Chrome(placement.Node)).Inner
		}
	}
	t.Fatalf("leaf %d has no placement", leafID)
	return paneframe.Box{}
}

func deckClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func deckDragTo(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// The flagship gesture, driven through the full Update ladder like real input:
// pressing the thumb of a deck-hosted document starts the shared drag, held
// motion moves the offset through the shared core, and a release far outside
// the pane settles it with the offset where the pointer left it.
func TestAppContentDeckDocumentThumbDragChangesOffsetEndToEnd(t *testing.T) {
	long := strings.Repeat("paragraph line\n", 80)
	m, h, doc := scrollbarDeckFixture(t, "long.txt", long)
	docLeaf := h.deck.Leaf(panelayout.Document)
	track := findDeckBarRegion(t, h, docLeaf, ui.RegionScrollbarTrack)
	thumb := findDeckBarRegion(t, h, docLeaf, ui.RegionScrollbarThumb)
	topY := track.Rect.Y

	before := doc.ScrollOffset()
	if before != 0 {
		t.Fatalf("fixture starts scrolled to %d", before)
	}

	adopt := func(updated tea.Model) {
		switch next := updated.(type) {
		case Model:
			m = &next
		case *Model:
			m = next
		default:
			t.Fatalf("updated model type %T", updated)
		}
	}

	updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X: thumb.Rect.X, Y: headerHeight + thumb.Rect.Y, Button: tea.MouseLeft,
	}))
	adopt(updated)
	if !doc.ScrollbarDragging() {
		t.Fatal("thumb press did not arm the document's gesture")
	}
	if got := h.mouse.DragRegion(); got != appDeckSelectGestureRegion {
		t.Fatalf("thumb press started drag %q, want %s", got, appDeckSelectGestureRegion)
	}
	if doc.ScrollOffset() != before {
		t.Fatalf("grab at rest moved the offset to %d", doc.ScrollOffset())
	}

	dragRow := 6
	want := ui.OffsetAtRow(doc.ScrollbarParams(), dragRow)
	updated, _ = m.Update(deckDragTo(track.Rect.X, headerHeight+topY+dragRow))
	adopt(updated)
	if doc.ScrollOffset() != want {
		t.Fatalf("drag to row %d left offset %d, want %d", dragRow, doc.ScrollOffset(), want)
	}
	if want <= before {
		t.Fatalf("gesture did not change the offset: before=%d after=%d", before, want)
	}

	updated, _ = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 1, Y: headerHeight + 1, Button: tea.MouseLeft}))
	adopt(updated)
	if doc.ScrollbarDragging() {
		t.Fatal("release outside the pane did not settle the gesture")
	}
	if doc.ScrollOffset() != want {
		t.Fatalf("settle moved the offset to %d, want %d", doc.ScrollOffset(), want)
	}
	if h.selectGestureLeaf != 0 {
		t.Fatal("settle left the deck holding the document leaf")
	}
}

// A track press jumps so the grabbed point becomes the thumb anchor (macOS
// jump-to-spot), and the same gesture keeps dragging from there with the
// pointer mapping straight onto track rows.
func TestAppContentDeckDocumentTrackClickAnchorsAtGrabPoint(t *testing.T) {
	m, h, doc := scrollbarDeckFixture(t, "long.txt", strings.Repeat("paragraph line\n", 80))
	docLeaf := h.deck.Leaf(panelayout.Document)
	track := findDeckBarRegion(t, h, docLeaf, ui.RegionScrollbarTrack)
	_, geom := ui.RenderScrollbarWithGeometry(doc.ScrollbarParams())

	pressRow := geom.ThumbRect.Max.Y + 3 // below the thumb: a track press
	m.appContentMouse(deckClick(track.Rect.X, track.Rect.Y+pressRow))
	if !doc.ScrollbarDragging() {
		t.Fatal("track press did not begin the gesture")
	}
	want := ui.OffsetAtRow(doc.ScrollbarParams(), pressRow)
	if doc.ScrollOffset() != want {
		t.Fatalf("track click left offset %d, want grab-point anchor %d", doc.ScrollOffset(), want)
	}

	nextRow := pressRow + 4
	m.appContentMouse(deckDragTo(track.Rect.X, track.Rect.Y+nextRow))
	if doc.ScrollOffset() != ui.OffsetAtRow(doc.ScrollbarParams(), nextRow) {
		t.Fatalf("continuing drag left offset %d, want %d",
			doc.ScrollOffset(), ui.OffsetAtRow(doc.ScrollbarParams(), nextRow))
	}

	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: track.Rect.X + 5, Y: track.Rect.Y + 2, Button: tea.MouseLeft}))
	if doc.ScrollbarDragging() {
		t.Fatal("release did not settle the gesture")
	}
}

// A release lost off-window recovers on the first button-less motion — the
// same hover boundary every other scrollbar host settles its gestures on.
func TestAppContentDeckLostReleaseRecoversOnButtonlessMotion(t *testing.T) {
	m, h, doc := scrollbarDeckFixture(t, "long.txt", strings.Repeat("paragraph line\n", 80))
	docLeaf := h.deck.Leaf(panelayout.Document)
	thumb := findDeckBarRegion(t, h, docLeaf, ui.RegionScrollbarThumb)

	m.appContentMouse(deckClick(thumb.Rect.X, thumb.Rect.Y))
	if !doc.ScrollbarDragging() {
		t.Fatal("press did not arm the gesture")
	}

	// No release ever arrives; motion without a button ends the shared drag,
	// and the hosted gesture ends with it.
	m.appContentMouse(tea.MouseMotionMsg(tea.Mouse{X: thumb.Rect.X - 10, Y: thumb.Rect.Y + 10}))
	if doc.ScrollbarDragging() {
		t.Fatal("lost release left the document gesture live")
	}
	if h.selectGestureLeaf != 0 {
		t.Fatal("lost release left the deck holding the document leaf")
	}
}

// A note card's bar is wired from the state-free seam: the host registers the
// regions, maps presses and drags through the shared inverse math, and settles
// wherever the pointer is.
func TestAppContentDeckNoteCardThumbAndTrackGestures(t *testing.T) {
	root := t.TempDir()
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "plain"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 40)
	cmd := m.openAppContent(root, p.id, contentlink.Ref{Kind: contentlink.KindInternal, Namespace: "note", Value: "nt-1"})
	if cmd == nil {
		t.Fatal("note open returned no load command")
	}
	m.renderContent(200, 40)
	h := m.currentContentDeck()
	note := seedDeckNote(t, h)
	m.renderContent(200, 40)

	noteLeaf := h.deck.Leaf(panelayout.Note)
	track := findDeckBarRegion(t, h, noteLeaf, ui.RegionScrollbarTrack)
	thumb := findDeckBarRegion(t, h, noteLeaf, ui.RegionScrollbarThumb)
	_, geom := ui.RenderScrollbarWithGeometry(note.ScrollbarParams())
	if !geom.HasThumb {
		t.Fatal("note fixture does not overflow")
	}

	// Thumb grab: nothing moves until the pointer does, then the offset follows
	// the press-time snapshot's mapping.
	m.appContentMouse(deckClick(thumb.Rect.X, thumb.Rect.Y))
	if h.mouse.DragRegion() != ui.RegionScrollbarThumb {
		t.Fatalf("thumb press started drag %q, want %s", h.mouse.DragRegion(), ui.RegionScrollbarThumb)
	}
	if note.ScrollOffset() != 0 {
		t.Fatalf("grab at rest moved the note offset to %d", note.ScrollOffset())
	}
	dragRow := 8
	m.appContentMouse(deckDragTo(thumb.Rect.X, track.Rect.Y+dragRow))
	if want := ui.OffsetAtRow(note.ScrollbarParams(), dragRow); note.ScrollOffset() != want {
		t.Fatalf("note thumb drag left offset %d, want %d", note.ScrollOffset(), want)
	}
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))
	if h.noteBar.active {
		t.Fatal("release outside did not settle the note gesture")
	}

	// Track press: jump-to-spot anchored at the clicked row. Geometry comes
	// from this frame: the earlier gesture moved the thumb.
	m.renderContent(200, 40)
	_, geom = ui.RenderScrollbarWithGeometry(note.ScrollbarParams())
	track = findDeckBarRegion(t, h, noteLeaf, ui.RegionScrollbarTrack)
	pressRow := min(geom.ThumbRect.Max.Y+2, geom.TrackRect.Dy()-1)
	m.appContentMouse(deckClick(track.Rect.X, track.Rect.Y+pressRow))
	if want := note.OffsetAtTrackRow(pressRow); note.ScrollOffset() != want {
		t.Fatalf("note track click left offset %d, want anchor %d", note.ScrollOffset(), want)
	}
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: track.Rect.X + 3, Y: track.Rect.Y, Button: tea.MouseLeft}))
	if h.noteBar.active || h.mouse.IsDragging() {
		t.Fatal("release left the note gesture live")
	}
}

// An issue card hosted by the deck arms its own gesture in HandleClick and
// continues it through ScrollbarDrag; the deck only routes.
func TestAppContentDeckIssueCardThumbDragChangesOffset(t *testing.T) {
	root := t.TempDir()
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "plain"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 40)
	if cmd := m.openAppContent(root, p.id, contentlink.Ref{Kind: contentlink.KindIssue, Value: "td-22f35f"}); cmd == nil {
		t.Fatal("issue open returned no load command")
	}
	m.renderContent(200, 40)
	h := m.currentContentDeck()
	issue := seedDeckIssue(t, h)
	m.renderContent(200, 40)

	issueLeaf := h.deck.Leaf(panelayout.Issue)
	rect := issue.ScrollbarRect()
	if rect.W != 1 || !issue.HasScrollbar() {
		t.Fatalf("issue fixture reports no interactive bar: %+v", rect)
	}
	inner := appDeckInnerBox(t, h, issueLeaf)
	x := inner.X + rect.X
	topY := inner.Y + paneframe.HeaderRows
	params := issue.ScrollbarParams()
	_, geom := ui.RenderScrollbarWithGeometry(params)
	// Press the first track row past the thumb bottom so the grab anchors there.
	pressRow := geom.ThumbRect.Max.Y

	before := issue.ScrollOffset()
	m.appContentMouse(deckClick(x, topY+pressRow))
	if !issue.ScrollbarDragging() {
		t.Fatal("bar press did not arm the issue card's gesture")
	}
	if got := h.mouse.DragRegion(); got != appDeckIssueScrollbarRegion {
		t.Fatalf("bar press started drag %q, want %s", got, appDeckIssueScrollbarRegion)
	}
	want := ui.OffsetAtRow(params, pressRow)
	if issue.ScrollOffset() != want || want <= before {
		t.Fatalf("track click left offset %d (was %d), want anchor %d", issue.ScrollOffset(), before, want)
	}

	m.appContentMouse(deckDragTo(x, topY+pressRow+5))
	if issue.ScrollOffset() != ui.OffsetAtRow(params, pressRow+5) {
		t.Fatalf("issue drag left offset %d, want %d",
			issue.ScrollOffset(), ui.OffsetAtRow(params, pressRow+5))
	}

	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))
	if issue.ScrollbarDragging() {
		t.Fatal("release outside the pane did not settle the issue gesture")
	}
	if h.issueScrollLeaf != 0 {
		t.Fatal("settle left the deck holding the issue leaf")
	}
}

// Bar regions register inside the frame's Body pass, so they come after every
// frame-owned content region and win the reverse scan over the leaf beneath
// them without outranking tabs or the close button.
func TestAppContentDeckScrollbarRegionsRegisterAfterContentRegions(t *testing.T) {
	_, h, _ := scrollbarDeckFixture(t, "long.txt", strings.Repeat("paragraph line\n", 80))
	regions := h.mouse.HitMap.Regions()
	lastFrameOwned := -1
	for i, region := range regions {
		switch region.ID {
		case appDeckTabRegion, appDeckCloseRegion, appDeckDividerRegion:
			lastFrameOwned = i
		}
	}
	firstBar := len(regions)
	for i, region := range regions {
		if isAppDeckNoteBarRegion(region.ID) {
			firstBar = min(firstBar, i)
		}
	}
	if lastFrameOwned < 0 || firstBar >= len(regions) || firstBar <= lastFrameOwned {
		t.Fatalf("bar regions (first at %d) must follow all content regions (last at %d)",
			firstBar, lastFrameOwned)
	}
	// Within one leaf the pair is ordered track then thumb, so the thumb wins
	// their overlap.
	if got := regions[firstBar].ID; got != ui.RegionScrollbarTrack || regions[firstBar+1].ID != ui.RegionScrollbarThumb {
		t.Fatalf("bar pair order = %s then %s", got, regions[firstBar+1].ID)
	}
	resolved := h.mouse.HitMap.Test(regions[firstBar+1].Rect.X, regions[firstBar+1].Rect.Y)
	if resolved == nil || resolved.ID != ui.RegionScrollbarThumb {
		t.Fatalf("thumb column resolves to %#v, want the thumb region", resolved)
	}
}

// A document whose content fits registers no bar regions at all: the reserved
// column is an anti-jitter spacer, and gestures over it stay inert.
func TestAppContentDeckNoBarRegionsWhenContentFits(t *testing.T) {
	m, h, doc := scrollbarDeckFixture(t, "short.txt", "one\ntwo\nthree\n")
	for _, id := range []string{ui.RegionScrollbarThumb, ui.RegionScrollbarTrack} {
		if got := appDeckScrollbarRegions(h, id); len(got) != 0 {
			t.Fatalf("fitting document registered %d %s regions", len(got), id)
		}
	}
	inner := appDeckInnerBox(t, h, h.deck.Leaf(panelayout.Document))
	barX := inner.X + inner.W - 1 // where the spacer column sits
	before := doc.ScrollOffset()
	m.appContentMouse(deckClick(barX, inner.Y+paneframe.HeaderRows+1))
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: barX, Y: inner.Y + paneframe.HeaderRows + 1, Button: tea.MouseLeft}))
	if doc.ScrollbarDragging() || doc.ScrollOffset() != before {
		t.Fatalf("spacer click scrolled (%d) or armed a gesture", doc.ScrollOffset())
	}
	if h.mouse.IsDragging() {
		t.Fatal("spacer click started a drag")
	}
}

// With no gesture active, ordinary mouse traffic leaves the deck's rendering
// byte-for-byte alone — including motion across a bar column, which lights no
// emphasis because this host forwards only presses, drags and releases.
func TestAppContentDeckIdleRenderByteParityUnderMouseTraffic(t *testing.T) {
	m, h, doc := scrollbarDeckFixture(t, "long.txt", strings.Repeat("paragraph line\n", 80))
	baseline := m.renderContent(200, 40)
	docLeaf := h.deck.Leaf(panelayout.Document)
	inner := appDeckInnerBox(t, h, docLeaf)

	traffic := []tea.MouseMsg{
		tea.MouseMotionMsg(tea.Mouse{X: inner.X + 2, Y: inner.Y + paneframe.HeaderRows + 2}),
		deckClick(inner.X+2, inner.Y+paneframe.HeaderRows+2),
		tea.MouseReleaseMsg(tea.Mouse{X: inner.X + 2, Y: inner.Y + paneframe.HeaderRows + 2, Button: tea.MouseLeft}),
	}
	for _, event := range traffic {
		m.appContentMouse(event)
		m.renderContent(200, 40)
	}
	afterTraffic := m.renderContent(200, 40)

	thumb := findDeckBarRegion(t, h, docLeaf, ui.RegionScrollbarThumb)
	m.appContentMouse(tea.MouseMotionMsg(tea.Mouse{X: thumb.Rect.X, Y: thumb.Rect.Y}))
	hovered := m.renderContent(200, 40)

	// A complete click-only bar gesture must leave no pressed-style bytes
	// behind once settled.
	m.appContentMouse(deckClick(thumb.Rect.X, thumb.Rect.Y))
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: thumb.Rect.X, Y: thumb.Rect.Y, Button: tea.MouseLeft}))
	m.appContentMouse(tea.MouseMotionMsg(tea.Mouse{X: 0, Y: 0}))
	settled := m.renderContent(200, 40)

	if hovered != baseline {
		t.Fatal("hovering the bar changed idle bytes")
	}
	if afterTraffic != baseline {
		t.Fatal("ordinary mouse traffic changed idle bytes")
	}
	if settled != baseline {
		t.Fatal("a settled bar gesture left different bytes behind")
	}
	if doc.ScrollbarDragging() {
		t.Fatal("idle parity check left a gesture live")
	}
}

// The second press of a rapid double-press on an issue card's bar re-arms the
// gesture exactly like the first one did — through the seam that can never
// reach a nav row — instead of being absorbed as a navigation-replay guard.
func TestAppContentDeckIssueCardSecondQuickPressStillGrabsTheBar(t *testing.T) {
	root := t.TempDir()
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "plain"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 40)
	if cmd := m.openAppContent(root, p.id, contentlink.Ref{Kind: contentlink.KindIssue, Value: "td-22f35f"}); cmd == nil {
		t.Fatal("issue open returned no load command")
	}
	m.renderContent(200, 40)
	h := m.currentContentDeck()
	issue := seedDeckIssue(t, h)
	m.renderContent(200, 40)

	issueLeaf := h.deck.Leaf(panelayout.Issue)
	rect := issue.ScrollbarRect()
	inner := appDeckInnerBox(t, h, issueLeaf)
	x := inner.X + rect.X
	topY := inner.Y + paneframe.HeaderRows
	params := issue.ScrollbarParams()
	_, geom := ui.RenderScrollbarWithGeometry(params)
	pressRow := geom.ThumbRect.Max.Y

	m.appContentMouse(deckClick(x, topY+pressRow))
	if !issue.ScrollbarDragging() {
		t.Fatal("first press did not arm the gesture")
	}
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: x, Y: topY + pressRow, Button: tea.MouseLeft}))
	if issue.ScrollbarDragging() {
		t.Fatal("release did not settle the first gesture")
	}

	double := tea.MouseClickMsg(tea.Mouse{X: x, Y: topY + pressRow, Button: tea.MouseLeft})
	m.appContentMouse(double)
	if !issue.ScrollbarDragging() {
		t.Fatal("a quick second press on the bar did not re-grab it")
	}
	if got := h.mouse.DragRegion(); got != appDeckIssueScrollbarRegion {
		t.Fatalf("second press started drag %q, want %s", got, appDeckIssueScrollbarRegion)
	}

	m.appContentMouse(deckDragTo(x, topY+pressRow+4))
	if want := ui.OffsetAtRow(params, pressRow+4); issue.ScrollOffset() != want {
		t.Fatalf("post-regrab drag left offset %d, want %d", issue.ScrollOffset(), want)
	}
	m.appContentMouse(tea.MouseReleaseMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))
}

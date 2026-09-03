package filebrowser

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/ui"
)

// newTreePluginWithFiles builds a plugin over a real tree of n root files and
// renders the normal two-pane layout once so every hit region — content and
// scrollbar alike — is registered exactly as the real app does.
func newTreePluginWithFiles(t *testing.T, files int) *Plugin {
	t.Helper()
	tmpDir := t.TempDir()
	for i := range files {
		name := fmt.Sprintf("file%02d.go", i)
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("package x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: tmpDir,
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		},
		width:       100,
		height:      30,
		treeWidth:   30,
		treeVisible: true,
	}
	p.mouseHandler = mouse.NewHandler()
	p.tree = NewFileTree(tmpDir)
	if err := p.tree.Build(); err != nil {
		t.Fatalf("build tree: %v", err)
	}
	p.tree.Flatten()
	p.renderNormalPanes()
	return p
}

// newOverflowTreePlugin is newTreePluginWithFiles with a guard that the tree
// overflows its pane by a wide margin.
func newOverflowTreePlugin(t *testing.T, files int) *Plugin {
	t.Helper()
	p := newTreePluginWithFiles(t, files)
	if p.tree.Len() <= p.treeItemRows() {
		t.Fatalf("test needs an overflowing tree: len=%d rows=%d", p.tree.Len(), p.treeItemRows())
	}
	return p
}

// newOverflowPreviewPlugin builds a plugin whose preview overflows, rendered
// through the normal layout so its scrollbar regions are registered.
func newOverflowPreviewPlugin(t *testing.T, lineCount int) *Plugin {
	t.Helper()
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d of content", i+1)
	}
	p := &Plugin{
		width:        100,
		height:       30,
		treeWidth:    30,
		treeVisible:  true,
		previewFile:  "test.txt",
		previewSize:  100,
		previewLines: lines,
	}
	p.mouseHandler = mouse.NewHandler()
	p.selection.Clear() // zero-value state would read as a {0,0} selection
	p.renderNormalPanes()
	return p
}

func scrollbarRegion(t *testing.T, p *Plugin, id string, view scrollbarView) *mouse.Region {
	t.Helper()
	for _, r := range p.mouseHandler.HitMap.Regions() {
		if r.ID != id {
			continue
		}
		if v, ok := r.Data.(scrollbarView); ok && v == view {
			return &r
		}
	}
	return nil
}

func requireBar(t *testing.T, p *Plugin, view scrollbarView) (thumb, track *mouse.Region, topY int) {
	t.Helper()
	thumb = scrollbarRegion(t, p, ui.RegionScrollbarThumb, view)
	track = scrollbarRegion(t, p, ui.RegionScrollbarTrack, view)
	if thumb == nil || track == nil {
		t.Fatalf("no thumb/track regions registered for view %d", view)
	}
	return thumb, track, p.scrollbarTopY()
}

// pressCmd drives a press and reports the command it produced.
func pressCmd(t *testing.T, p *Plugin, x, y int) tea.Cmd {
	t.Helper()
	_, cmd := p.handleMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	return cmd
}

// A track press is jump-to-spot anchored at the grabbed row, and the gesture
// continues as a drag from that anchor.
func TestTreeScrollbarTrackClickAnchorsAndDrags(t *testing.T) {
	p := newOverflowTreePlugin(t, 60)
	_, track, topY := requireBar(t, p, sbTree)

	params, ok := p.liveScrollbarParams(sbTree)
	if !ok {
		t.Fatal("liveScrollbarParams(tree) not ok")
	}
	// Anchor below the thumb so this is a track press, not a thumb grab.
	anchorRow := p.bars[sbTree].geom.ThumbRect.Dy() + 2
	wantOffset := ui.OffsetAtRow(params, anchorRow)
	if wantOffset == 0 {
		t.Fatalf("test setup: anchor row %d maps to offset 0; pick a lower row", anchorRow)
	}

	press(t, p, track.Rect.X, topY+anchorRow)

	if got := p.treeScrollOff; got != wantOffset {
		t.Errorf("track click scrolled to %d, want %d", got, wantOffset)
	}
	if !p.mouseHandler.IsDragging() {
		t.Error("track click did not continue as a drag")
	}
	if p.dragScrollbar != sbTree {
		t.Errorf("dragScrollbar = %v, want sbTree", p.dragScrollbar)
	}
	if p.treeCursor != 0 {
		t.Errorf("treeCursor moved to %d by a scrollbar click, want 0", p.treeCursor)
	}

	// The anchor holds: dragging back to the clicked row lands on the same
	// offset, and moving above it scrolls up from there.
	motion(t, p, track.Rect.X, topY+anchorRow)
	if got := p.treeScrollOff; got != wantOffset {
		t.Errorf("motion at the anchor row moved the offset to %d, want %d", got, wantOffset)
	}
	motion(t, p, track.Rect.X, topY+anchorRow-5)
	if got := p.treeScrollOff; got >= wantOffset {
		t.Errorf("dragging above the anchor left offset %d, want < %d", got, wantOffset)
	}

	release(t, p, 3, 1) // released well outside the bar
	if p.mouseHandler.IsDragging() {
		t.Error("drag survived release outside the bar")
	}
	if p.dragScrollbar != sbNone {
		t.Errorf("dragScrollbar = %v after release, want sbNone", p.dragScrollbar)
	}
}

// Grabbing the thumb preserves where within it the press landed.
func TestTreeScrollbarThumbDragEndToEnd(t *testing.T) {
	p := newOverflowTreePlugin(t, 60)
	thumb, _, topY := requireBar(t, p, sbTree)

	params, _ := p.liveScrollbarParams(sbTree)
	thumbTop := ui.RowForOffset(params, 0)
	grabWithin := 2

	press(t, p, thumb.Rect.X, topY+thumbTop+grabWithin)
	if !p.mouseHandler.IsDragging() {
		t.Fatal("thumb press did not start a drag")
	}
	if got := p.treeScrollOff; got != 0 {
		t.Fatalf("grabbing the thumb scrolled to %d, want 0", got)
	}

	motion(t, p, thumb.Rect.X, topY+thumbTop+grabWithin+8)
	want := ui.OffsetAtRow(params, thumbTop+8)
	if got := p.treeScrollOff; got != want {
		t.Errorf("after dragging 8 rows: offset = %d, want %d", got, want)
	}
	if p.treeCursor != 0 {
		t.Errorf("treeCursor = %d after a pure scroll, want 0", p.treeCursor)
	}

	// Past the top end: clamped without losing the gesture.
	motion(t, p, thumb.Rect.X, topY-50)
	if got := p.treeScrollOff; got != 0 {
		t.Errorf("dragging far past the top left offset %d, want 0", got)
	}
	if !p.mouseHandler.IsDragging() {
		t.Error("gesture ended by clamping")
	}

	release(t, p, thumb.Rect.X, topY+2)
	if p.mouseHandler.IsDragging() || p.dragScrollbar != sbNone {
		t.Error("drag state survived release")
	}
	if got := p.treeScrollOff; got != 0 {
		t.Errorf("offset = %d after settle, want 0", got)
	}
}

// A release that never arrives (window unfocused, pointer left) is recovered
// by the first button-less motion instead of leaving the bar stuck in drag
// styling.
func TestScrollbarDragRecoveredFromLostRelease(t *testing.T) {
	p := newOverflowTreePlugin(t, 60)
	thumb, _, topY := requireBar(t, p, sbTree)

	press(t, p, thumb.Rect.X, topY+1)
	if !p.mouseHandler.IsDragging() {
		t.Fatal("press did not start a drag")
	}

	// Motion with no button held: the handler ends the gesture and reports
	// hover; our half of the state must follow.
	p.handleMouse(tea.MouseMotionMsg(tea.Mouse{X: thumb.Rect.X, Y: topY + 1}))
	if p.dragScrollbar != sbNone {
		t.Errorf("dragScrollbar = %v after button-less motion, want sbNone", p.dragScrollbar)
	}
}

// A second press inside the double-click window arrives as ActionDoubleClick.
// The bar must treat it as a fresh grab — continuing a thumb grab or
// re-jumping on the track — instead of dropping it for not naming a row.
func TestScrollbarSecondQuickPressStillGrabsTheBar(t *testing.T) {
	p := newOverflowTreePlugin(t, 60)
	thumb, _, topY := requireBar(t, p, sbTree)

	x, y := thumb.Rect.X, topY+2
	press(t, p, x, y)
	release(t, p, x, y)

	_, cmd := p.handleMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	if cmd != nil {
		t.Error("the second press produced a command")
	}
	if !p.mouseHandler.IsDragging() || p.dragScrollbar != sbTree {
		t.Fatalf("a quick second press did not grab the bar (dragScrollbar=%v)", p.dragScrollbar)
	}

	// The re-grab kept the within-thumb anchor: pressing at track row 2 and
	// dragging to row 5 lands three anchor-adjusted rows further down.
	motion(t, p, x, y+3)
	want := ui.OffsetAtRow(mustTreeParams(t, p), (y+3-topY)-2)
	if got := p.treeScrollOff; got != want {
		t.Errorf("post-regrab drag left offset %d, want %d", got, want)
	}
	release(t, p, x, y+3)
	if p.treeCursor != 0 {
		t.Errorf("treeCursor moved to %d by scrollbar presses, want 0", p.treeCursor)
	}
}

func mustTreeParams(t *testing.T, p *Plugin) ui.ScrollbarParams {
	t.Helper()
	params, ok := p.liveScrollbarParams(sbTree)
	if !ok {
		t.Fatal("liveScrollbarParams(tree) not ok")
	}
	return params
}

func TestPreviewScrollbarDragEndToEnd(t *testing.T) {
	p := newOverflowPreviewPlugin(t, 50)
	thumb, _, topY := requireBar(t, p, sbPreview)

	params, _ := p.liveScrollbarParams(sbPreview)
	thumbTop := ui.RowForOffset(params, 0)

	press(t, p, thumb.Rect.X, topY+thumbTop)
	motion(t, p, thumb.Rect.X, topY+thumbTop+10)
	want := ui.OffsetAtRow(params, thumbTop+10)
	if got := p.previewScroll; got != want {
		t.Errorf("previewScroll = %d after dragging, want %d", got, want)
	}
	if p.selection.HasSelection() {
		t.Error("scrollbar drag produced a text selection")
	}

	release(t, p, 2, 2)
	if p.mouseHandler.IsDragging() || p.dragScrollbar != sbNone {
		t.Error("drag state survived release")
	}
	if got := p.previewScroll; got != want {
		t.Errorf("previewScroll = %d after settle, want %d", got, want)
	}
}

func TestSearchResultsScrollbarDragSetsManualOffset(t *testing.T) {
	p := newTreePluginWithFiles(t, 3) // tree small enough to stay out of the way
	const matches = 40
	p.searchMode = true
	p.searchField.SetQuery("file")
	for i := range matches {
		path := fmt.Sprintf("file%02d.go", i)
		p.searchMatches = append(p.searchMatches, filefind.Match{Path: path, Name: path})
	}
	p.renderNormalPanes()

	_, track, topY := requireBar(t, p, sbSearch)
	thumb := scrollbarRegion(t, p, ui.RegionScrollbarThumb, sbSearch)
	if thumb == nil {
		t.Fatal("no search thumb region")
	}

	params, ok := p.liveScrollbarParams(sbSearch)
	if !ok {
		t.Fatal("liveScrollbarParams(search) not ok")
	}

	// Track press below the thumb: jump-to-spot runs to the clamped end and
	// the gesture continues as a drag from there.
	belowThumb := p.bars[sbSearch].geom.ThumbRect.Dy() + topY + 2
	press(t, p, track.Rect.X, belowThumb)
	if got := p.effectiveSearchScrollOff(p.treeItemRows()); got != len(p.searchMatches)-p.treeItemRows() {
		t.Errorf("track press past the thumb left offset %d, want the last page", got)
	}
	release(t, p, track.Rect.X, belowThumb)

	// Thumb drag sets a manual mid-list position that survives re-render.
	p.searchScrollOff = -1
	p.renderNormalPanes()
	grabWithin := 2
	press(t, p, thumb.Rect.X, topY+grabWithin)
	motion(t, p, thumb.Rect.X, topY+grabWithin+4)
	want := ui.OffsetAtRow(params, 4)
	if got := p.effectiveSearchScrollOff(p.treeItemRows()); got != want {
		t.Errorf("manual search offset = %d, want %d", got, want)
	}
	release(t, p, thumb.Rect.X, topY+grabWithin+4)

	// The manual position survives a re-render: the list really starts at the
	// jumped-to match now.
	rendered := ansi.Strip(p.renderNormalPanes())
	first := fmt.Sprintf("file%02d.go", want)
	if !strings.Contains(rendered, first) {
		t.Errorf("rendered output does not show %q after scrolling to offset %d", first, want)
	}
	if strings.Contains(rendered, "file00.go") && want > 0 {
		t.Error("first match still rendered after scrolling past it")
	}

	// Keyboard movement resumes cursor-following.
	p.followSearchCursor()
	if got := p.effectiveSearchScrollOff(p.treeItemRows()); got != 0 {
		t.Errorf("after following the cursor: offset = %d, want 0", got)
	}
}

// The bar's rects sit exactly on the rendered bar column, and they win the
// reverse scan against the row regions reaching into that column.
func TestScrollbarRegionWinsOverRowAndLineClicks(t *testing.T) {
	p := newOverflowTreePlugin(t, 60)
	prev := newOverflowPreviewPlugin(t, 50)

	treeBarX := p.treeScrollbarX()
	rowY := p.scrollbarTopY() + 3
	region := p.mouseHandler.HitMap.Test(treeBarX, rowY)
	if region == nil {
		t.Fatal("nothing hit in the tree scrollbar column")
	}
	switch region.ID {
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
	default:
		t.Fatalf("hit %q in the tree scrollbar column, want a scrollbar region", region.ID)
	}
	if view, _ := region.Data.(scrollbarView); view != sbTree {
		t.Errorf("tree bar region carries view %v, want sbTree", view)
	}

	// Behavioral proof: pressing there starts a scrollbar gesture, never a row
	// selection or a preview load.
	cmd := pressCmd(t, p, treeBarX, rowY)
	if cmd != nil {
		t.Error("scrollbar press returned a command (a preview load?)")
	}
	if p.treeCursor != 0 {
		t.Errorf("treeCursor = %d after scrollbar press, want 0", p.treeCursor)
	}
	if id := p.mouseHandler.DragRegion(); id != ui.RegionScrollbarThumb && id != ui.RegionScrollbarTrack {
		t.Errorf("drag started in region %q, want a scrollbar region", id)
	}

	// Same for the preview: the bar wins over the line-selection rects that
	// reach into its column, and pressing it cannot start a text selection.
	prevBarX := prev.previewScrollbarX()
	lineY := prev.scrollbarTopY() + 4
	region = prev.mouseHandler.HitMap.Test(prevBarX, lineY)
	if region == nil || (region.ID != ui.RegionScrollbarThumb && region.ID != ui.RegionScrollbarTrack) {
		t.Fatalf("preview bar column hit %v, want a scrollbar region", region)
	}
	if view, _ := region.Data.(scrollbarView); view != sbPreview {
		t.Errorf("preview bar region carries view %v, want sbPreview", view)
	}
	press(t, prev, prevBarX, lineY)
	motion(t, prev, prevBarX, lineY+3)
	if prev.selection.HasSelection() {
		t.Error("pressing the preview scrollbar began a text selection")
	}
	release(t, prev, prevBarX, lineY)
}

// Content that fits draws only the anti-jitter spacer and registers nothing.
func TestNoScrollbarRegionsWhenContentFits(t *testing.T) {
	p := newTreePluginWithFiles(t, 3) // under one pane of rows
	p.previewLines = nil
	for _, r := range p.mouseHandler.HitMap.Regions() {
		switch r.ID {
		case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
			t.Errorf("scrollbar region %v registered for content that fits: %+v", r.ID, r.Rect)
		}
	}

	prev := newOverflowPreviewPlugin(t, 5)
	for _, r := range prev.mouseHandler.HitMap.Regions() {
		switch r.ID {
		case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
			t.Errorf("preview scrollbar region registered for content that fits: %+v", r.Rect)
		}
	}
}

// The registered column must be the rendered column: the thumb glyph sits in
// the rect we hand out, or every coordinate above is fiction.
func TestRegisteredColumnMatchesRenderedBar(t *testing.T) {
	p := newOverflowPreviewPlugin(t, 50)
	thumb, _, topY := requireBar(t, p, sbPreview)

	rendered := ansi.Strip(p.renderNormalPanes())
	rows := strings.Split(rendered, "\n")
	x := thumb.Rect.X
	for row := topY; row < topY+thumb.Rect.H; row++ {
		if row >= len(rows) || x >= len([]rune(rows[row])) {
			t.Fatalf("bar cell (%d,%d) outside rendered output", x, row)
		}
		if got := []rune(rows[row])[x]; got != '┃' {
			t.Fatalf("cell (%d,%d) = %q, want the thumb glyph ┃", x, row, string(got))
		}
	}
	// Just past the thumb, the same column shows the track.
	trackRow := topY + p.bars[sbPreview].geom.TrackRect.Dy() - 1
	if trackRow < len(rows) {
		if got := []rune(rows[trackRow])[x]; got != '│' {
			t.Errorf("cell (%d,%d) = %q, want the track glyph │", x, trackRow, string(got))
		}
	}
}

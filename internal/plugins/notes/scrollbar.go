package notes

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/ui"
)

// Which of the plugin's scrollable areas a pointer interaction targets. The
// typed value rides in the hit region Data slot — its distinct type is what
// separates scrollbar regions from every int-carrying content region — so one
// pair of shared region IDs serves both the note list and the note body.
type scrollAreaID int

const (
	scrollAreaNone scrollAreaID = iota
	scrollAreaList
	scrollAreaBody
)

// scrollBarSnapshot is the geometry a bar reported on its last render, in
// plugin coordinates. Press handlers translate screen rows into track rows
// with it; drags keep the press-time snapshot so re-renders cannot shift the
// mapping under the pointer.
type scrollBarSnapshot struct {
	params   ui.ScrollbarParams
	trackX   int // absolute X of the one-column track
	trackY   int // absolute Y of the track top
	thumbTop int // thumb top row within the track
	thumbH   int // thumb height in rows
	has      bool
}

// notesScrollState carries the pointer state of every interactive bar.
type notesScrollState struct {
	hoveringArea scrollAreaID
	dragArea     scrollAreaID
	grabDelta    int               // track rows between pointer and thumb anchor
	dragging     scrollBarSnapshot // snapshot taken at press time
	list         scrollBarSnapshot // latest render's snapshots
	body         scrollBarSnapshot
}

// listScrollbarSnapshot derives the note-list bar's geometry from layout
// state alone, so region registration and rendering share one source of truth
// regardless of which runs first.
func (p *Plugin) listScrollbarSnapshot() (scrollBarSnapshot, bool) {
	noteCount := len(p.getDisplayNotes())
	if noteCount == 0 {
		return scrollBarSnapshot{}, false
	}
	headerLines := 2 // title row + blank breathing row
	if p.searchMode || p.searchQuery() != "" {
		headerLines++ // search input line
	}
	contentHeight := p.height - 2 - headerLines // -2 for borders
	if contentHeight < 1 {
		contentHeight = 1
	}
	params := ui.ScrollbarParams{
		TotalItems:   noteCount,
		ScrollOffset: p.scrollOff,
		VisibleItems: contentHeight,
		TrackHeight:  contentHeight,
	}
	_, geom := ui.RenderScrollbarWithGeometry(params)
	snap := scrollBarSnapshot{params: params, has: geom.HasThumb}
	if geom.HasThumb {
		listInner := p.listWidth - paneChromeX
		bodyWidth := listInner - scrollbarWidth
		if bodyWidth < 10 {
			bodyWidth = 10
		}
		snap.trackX = 2 + bodyWidth // panel border + padding, then the body block
		snap.trackY = 1 + headerLines
		snap.thumbTop, snap.thumbH = geom.ThumbRect.Min.Y, geom.ThumbRect.Dy()
	}
	return snap, true
}

// bodyScrollbarStyle styles the note-body bar (preview and edit share it).
func (p *Plugin) bodyScrollbarStyle() ui.ScrollbarStyle {
	dragging := p.mouseHandler != nil &&
		p.mouseHandler.IsDragging() &&
		p.mouseHandler.DragRegion() == ui.RegionScrollbarThumb &&
		p.scrollPointer.dragArea == scrollAreaBody
	hovering := !dragging && p.scrollPointer.hoveringArea == scrollAreaBody
	return ui.ScrollbarStyle{
		Thumb: ui.HandleStateFrom(hovering, dragging),
		Track: ui.HandleStateFrom(hovering, false),
	}
}

// listScrollbarStyle styles the note-list bar.
func (p *Plugin) listScrollbarStyle() ui.ScrollbarStyle {
	dragging := p.mouseHandler != nil &&
		p.mouseHandler.IsDragging() &&
		p.mouseHandler.DragRegion() == ui.RegionScrollbarThumb &&
		p.scrollPointer.dragArea == scrollAreaList
	hovering := !dragging && p.scrollPointer.hoveringArea == scrollAreaList
	return ui.ScrollbarStyle{
		Thumb: ui.HandleStateFrom(hovering, dragging),
		Track: ui.HandleStateFrom(hovering, false),
	}
}

// stashBodyScrollbar records the note-body bar's rendered geometry. The bar
// does not exist while an inline editor owns the pane.
func (p *Plugin) stashBodyScrollbar(params ui.ScrollbarParams, geom ui.Geometry, trackX, trackY int) {
	snap := scrollBarSnapshot{params: params, has: geom.HasThumb}
	if geom.HasThumb {
		snap.trackX, snap.trackY = trackX, trackY
		snap.thumbTop, snap.thumbH = geom.ThumbRect.Min.Y, geom.ThumbRect.Dy()
	}
	p.scrollPointer.body = snap
}

// registerListScrollbarRegion registers the note-list bar after all list-pane
// content regions so a bar press can never select or create a note.
func (p *Plugin) registerListScrollbarRegion() {
	snap := p.scrollPointer.list
	if !snap.has || len(p.getDisplayNotes()) == 0 {
		return
	}
	p.addScrollbarRegions(scrollAreaList, snap)
}

// registerBodyScrollbarRegion registers the note-body bar after the editor
// line regions. Geometry is derived from layout state here rather than from
// the render stash so regions exist from the very first frame; the render
// stash stays authoritative for press mapping. Skipped while an inline
// terminal editor owns the pane — its presses belong to that editor, and no
// sidecar bar is drawn over it.
func (p *Plugin) registerBodyScrollbarRegion() {
	if p.edit.Active && p.edit.Model != nil {
		p.scrollPointer.body = scrollBarSnapshot{}
		return
	}
	if p.editorNote == nil {
		return
	}
	l := p.editorLayout()
	params := p.editorScrollbarParams(l)
	_, geom := ui.RenderScrollbarWithGeometry(params)
	trackX, trackY := p.bodyScrollbarRect(l)
	snap := scrollBarSnapshot{params: params, has: geom.HasThumb}
	if geom.HasThumb {
		snap.trackX, snap.trackY = trackX, trackY
		snap.thumbTop, snap.thumbH = geom.ThumbRect.Min.Y, geom.ThumbRect.Dy()
	}
	p.scrollPointer.body = snap
	if !snap.has {
		return
	}
	p.addScrollbarRegions(scrollAreaBody, snap)
}

func (p *Plugin) addScrollbarRegions(area scrollAreaID, snap scrollBarSnapshot) {
	p.mouseHandler.HitMap.Add(ui.RegionScrollbarTrack, mouse.Rect{
		X: snap.trackX, Y: snap.trackY, W: 1, H: snap.params.TrackHeight,
	}, area)
	p.mouseHandler.HitMap.Add(ui.RegionScrollbarThumb, mouse.Rect{
		X: snap.trackX, Y: snap.trackY + snap.thumbTop, W: 1, H: snap.thumbH,
	}, area)
}

// handleScrollbarPress starts a thumb drag, or jumps to the clicked spot and
// continues as a drag anchored at the grab row (macOS track-click). The gate
// runs before any editor click fall-through so gestures never reach caret
// logic as synthetic clicks.
func (p *Plugin) handleScrollbarPress(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	area, ok := action.Region.Data.(scrollAreaID)
	if !ok || !p.scrollAreaValid(area) {
		return p, nil
	}
	snap := p.scrollSnapshotFor(area)
	row := action.Y - snap.trackY

	offset := p.scrollOffsetFor(area)
	grabDelta := row - ui.RowForOffset(snap.params, offset)
	if action.Region.ID == ui.RegionScrollbarTrack {
		offset = ui.OffsetAtRow(snap.params, row)
		grabDelta = 0
		p.applyScrollOffset(area, offset)
	}

	p.scrollPointer.dragArea = area
	p.scrollPointer.grabDelta = grabDelta
	p.scrollPointer.dragging = snap
	p.mouseHandler.StartDrag(action.X, action.Y, ui.RegionScrollbarThumb, offset)
	return p, nil
}

// handleScrollbarDrag applies the pressed bar's offset mapping for the pointer
// position, clamped by the shared core at both ends of the track.
func (p *Plugin) handleScrollbarDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	area := p.scrollPointer.dragArea
	if p.mouseHandler.DragRegion() != ui.RegionScrollbarThumb || area == scrollAreaNone {
		return p, nil
	}
	row := action.Y - p.scrollPointer.dragging.trackY - p.scrollPointer.grabDelta
	p.applyScrollOffset(area, ui.OffsetAtRow(p.scrollPointer.dragging.params, row))
	return p, nil
}

// scrollbarDragEnded settles a finished or cancelled gesture. Scroll offsets
// are ephemeral view state; nothing is persisted.
func (p *Plugin) scrollbarDragEnded() {
	p.scrollPointer.dragArea = scrollAreaNone
	p.scrollPointer.grabDelta = 0
	p.scrollPointer.dragging = scrollBarSnapshot{}
}

// updateScrollbarHover records which area's bar (if any) is under the pointer.
func (p *Plugin) updateScrollbarHover(region *mouse.Region) {
	p.scrollPointer.hoveringArea = scrollAreaNone
	if region == nil {
		return
	}
	if region.ID != ui.RegionScrollbarThumb && region.ID != ui.RegionScrollbarTrack {
		return
	}
	if area, ok := region.Data.(scrollAreaID); ok {
		p.scrollPointer.hoveringArea = area
	}
}

func (p *Plugin) scrollAreaValid(area scrollAreaID) bool {
	switch area {
	case scrollAreaList:
		return p.scrollPointer.list.has
	case scrollAreaBody:
		return p.scrollPointer.body.has
	}
	return false
}

func (p *Plugin) scrollSnapshotFor(area scrollAreaID) scrollBarSnapshot {
	switch area {
	case scrollAreaList:
		return p.scrollPointer.list
	case scrollAreaBody:
		return p.scrollPointer.body
	}
	return scrollBarSnapshot{}
}

func (p *Plugin) scrollOffsetFor(area scrollAreaID) int {
	switch area {
	case scrollAreaList:
		return p.scrollOff
	case scrollAreaBody:
		return p.previewScrollOff
	}
	return 0
}

// applyScrollOffset moves one area's viewport. The note body maps differently
// per mode: preview shifts the read offset; edit scrolls the textarea viewport
// while putting the caret back exactly where it was, so text and caret stay
// untouched by the gesture.
func (p *Plugin) applyScrollOffset(area scrollAreaID, offset int) {
	switch area {
	case scrollAreaList:
		p.scrollOff = offset
	case scrollAreaBody:
		if p.editorNote == nil {
			return
		}
		if p.previewMode {
			p.previewScrollOff = offset
			p.clampPreviewScroll()
			p.keepPreviewCursorInView()
			return
		}
		line, col := p.editorTextarea.Line(), p.editorTextarea.Column()
		p.setTextareaCursorAndScroll(line, col, offset)
	}
}

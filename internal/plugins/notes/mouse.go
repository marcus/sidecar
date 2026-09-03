package notes

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// dragForwardThrottle is the minimum interval between forwarding mouse drag
// events to the tmux session for inline editor text selection.
// ~60fps to prevent subprocess spam (each forward spawns tmux send-keys).
const dragForwardThrottle = 16 * time.Millisecond

func (p *Plugin) inlineEditorMouseReporting() bool {
	return p.edit.Model != nil && p.edit.Model.PaneMouseReporting()
}

// forwardWheelToInlineEditor applies the same bounded flick policy as every
// other embedded terminal. A plain editor that has not enabled mouse reporting
// receives no synthetic keys and has no Sidecar-owned scrollback to move.
func (p *Plugin) forwardWheelToInlineEditor(action mouse.MouseAction) tea.Cmd {
	if p.edit.Model == nil {
		return nil
	}
	return tty.WheelHandler{
		Burst:          &p.inlineWheel,
		WritesEnabled:  p.edit.NativeActive(),
		MouseReporting: p.inlineEditorMouseReporting,
		PaneCoords:     p.calculateInlineEditorMouseCoords,
		NoteActivity:   p.edit.Model.NoteMouseActivity,
		SendNotches:    p.edit.Model.SendWheelNotches,
	}.Handle(tty.WheelGesture{
		Delta: action.Delta, X: action.X, Y: action.Y,
		Shift: action.Shift, Alt: action.Alt, Now: time.Now(),
	})
}

// Mouse region identifiers
const (
	regionListPane   = "list-pane"     // Overall list pane for scroll targeting
	regionEditorPane = "editor-pane"   // Overall editor pane for scroll targeting
	regionDivider    = "divider"       // Border between list and editor
	regionListFilter = "list-filter"   // Active/Archived/Deleted header control
	regionNewNote    = "list-new-note" // Header + New Note button
	regionNoteItem   = "note-item"     // Individual note in list (Data: visible index)
	regionEditorLine = "editor-line"   // Individual editor line (Data: line index)
)

// handleMouse processes mouse events and dispatches to appropriate handlers.
func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	// Handle exit confirmation dialog if active
	if p.edit.ShowExitConfirm {
		return p.handleExitConfirmationMouse(msg)
	}

	// Handle inline edit mode - detect click-away and forward mouse events for text selection
	if p.edit.Active && p.edit.Model != nil && p.edit.Model.IsActive() {
		action := p.mouseHandler.HandleMouse(msg)

		// Helper to handle click-away: auto-save and switch to clicked note
		handleClickAway := func(regionID string, regionData interface{}) (*Plugin, tea.Cmd) {
			p.edit.Dragging = false // Cancel any drag in progress

			// Check if the editor session is still alive
			if !p.isInlineEditSessionAlive() {
				// Session is dead - just clean up and process click
				p.exitInlineEditMode()
				p.edit.PendingClickRegion = regionID
				p.edit.PendingClickData = regionData
				return p.processPendingClickAction()
			}

			// Session is alive - auto-save and exit (no confirmation needed)
			// Save current content and exit
			saveCmd := p.saveAndExitInlineEditMode()
			// Exiting resets the inline session, including any pending click.
			// Record the action afterward so the visible click survives teardown.
			p.edit.SetPendingClick(regionID, regionData)

			// Process the click action immediately. Some click-away targets (the
			// filter pill) also schedule work, so preserve that command alongside
			// the retained-export save.
			p2, actionCmd := p.processPendingClickAction()

			return p2, tea.Batch(saveCmd, actionCmd)
		}

		if action.Type == mouse.ActionScrollUp || action.Type == mouse.ActionScrollDown {
			return p, p.forwardWheelToInlineEditor(action)
		}

		// Scrollbar gestures are sidecar's, not the pane app's: consume them
		// here so a bar press can never land in the editor as a caret-moving
		// synthetic click and a bar drag can never become text selection.
		// Drags and releases are keyed to the gesture's origin so a pointer
		// that wanders off the bar stays owned by the scrollbar.
		switch action.Type {
		case mouse.ActionClick, mouse.ActionDoubleClick, mouse.ActionTripleClick:
			if action.Region != nil {
				switch action.Region.ID {
				case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
					// Every press of a rapid multi-press re-grabs the bar;
					// none of them may fall through to the editor beneath.
					return p.handleScrollbarPress(action)
				}
			}
		case mouse.ActionDrag:
			if p.mouseHandler.DragRegion() == ui.RegionScrollbarThumb {
				return p.handleScrollbarDrag(action)
			}
		case mouse.ActionDragEnd:
			if action.DragStartID == ui.RegionScrollbarThumb {
				p.scrollbarDragEnded()
				return p, nil
			}
		}

		// Each physical click is one pane press. The shared handler labels the
		// second and third clicks for local selection users, but a mouse-aware
		// editor still needs the complete click stream to recognize them itself.
		if action.Type == mouse.ActionClick || action.Type == mouse.ActionDoubleClick || action.Type == mouse.ActionTripleClick {
			if action.Region != nil {
				switch action.Region.ID {
				case regionNoteItem, regionListPane, regionListFilter, regionNewNote:
					// Click in list pane - auto-save and switch
					return handleClickAway(action.Region.ID, action.Region.Data)
				case regionEditorPane, regionEditorLine:
					if !p.inlineEditorMouseReporting() {
						return p, nil
					}
					// Forward mouse press to the pane and start tracking drag.
					col, row, ok := p.calculateInlineEditorMouseCoords(action.X, action.Y)
					if ok {
						p.edit.Dragging = true
						p.lastDragForwardTime = time.Time{}
						return p, p.forwardMousePressToInlineEditor(col, row)
					}
					// The click landed on letterbox padding, outside the real
					// pane. Drop it as hover and release do: forwarding to the
					// tty model would send absolute screen coordinates, which
					// the pane never had.
					return p, nil
				}
			}

			// Fallback: use X position to detect list pane clicks
			if action.X < p.listWidth {
				return handleClickAway(regionListPane, nil)
			}
		}

		// Handle mouse motion/hover - forward drag events to vim for text selection.
		// Throttled to ~60fps to prevent subprocess spam (each forward spawns tmux send-keys).
		if action.Type == mouse.ActionHover && p.edit.Dragging {
			now := time.Now()
			if now.Sub(p.lastDragForwardTime) < dragForwardThrottle {
				return p, nil
			}
			col, row, ok := p.calculateInlineEditorMouseCoords(action.X, action.Y)
			if ok {
				p.lastDragForwardTime = now
				return p, p.forwardMouseDragToInlineEditor(col, row)
			}
		}

		// Handle mouse release - end drag
		if rel, ok := msg.(tea.MouseReleaseMsg); ok {
			if p.edit.Dragging {
				p.edit.Dragging = false
				p.lastDragForwardTime = time.Time{}
				rm := rel.Mouse()
				col, row, ok := p.calculateInlineEditorMouseCoords(rm.X, rm.Y)
				if ok {
					return p, p.forwardMouseReleaseToInlineEditor(col, row)
				}
			}
			return p, nil
		}

		// Forward other mouse events to tty model
		cmd := p.edit.Model.Update(msg)
		return p, cmd
	}

	action := p.mouseHandler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick:
		return p.handleMouseClick(action)
	case mouse.ActionDoubleClick:
		return p.handleMouseDoubleClick(action)
	case mouse.ActionTripleClick:
		return p.handleMouseTripleClick(action)
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		return p.handleMouseScroll(action)
	case mouse.ActionDrag:
		return p.handleMouseDrag(action)
	case mouse.ActionDragEnd:
		return p.handleMouseDragEndRegion(action.DragStartID)
	case mouse.ActionHover:
		return p.handleMouseHover(action)
	}
	return p, nil
}

// handleExitConfirmationMouse handles mouse events in the exit confirmation dialog.
func (p *Plugin) handleExitConfirmationMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	// For now, clicks anywhere in the confirmation just select the option under cursor
	// The keyboard handling does the main interaction
	return p, nil
}

// handleMouseClick handles single click actions.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		return p.handleScrollbarPress(action)

	case regionListFilter:
		p.activePane = PaneList
		return p, p.switchViewFilter(nextNoteFilter(p.viewFilter))

	case regionNewNote:
		p.activePane = PaneList
		if p.viewFilter == FilterActive {
			return p, p.createNote()
		}
		return p, nil

	case regionNoteItem:
		idx, ok := action.Region.Data.(int)
		if !ok {
			return p, nil
		}
		p.cursor = idx
		p.activePane = PaneList
		return p, p.loadNoteIntoEditor()

	case regionListPane:
		p.activePane = PaneList
		p.selection.Clear()
		return p, nil

	case regionEditorPane:
		p.activePane = PaneEditor
		p.selection.Clear()
		// A content-link span belongs to the app content deck. Consuming the
		// click as "enter edit" would drop preview surfaces before release
		// can activate the link.
		if p.previewMode && p.previewContentLinkAt(action.X, action.Y) {
			return p, nil
		}
		// Clicking into the note follows the default-editor preference. With no
		// note loaded the pane is a placeholder — focusing an editor over it
		// would show input that the list keys then ignore.
		if p.viewFilter == FilterActive && p.editorNote != nil {
			if p.previewMode && p.ctx != nil && p.ctx.Config != nil && p.ctx.Config.Plugins.Notes.DefaultEditor == config.NotesEditorPane {
				return p, p.editSelectedNote()
			}
			srcLine, srcCol := p.clickToSource(action.X, action.Y)
			cmd := p.enterEditAt(srcLine, srcCol)
			p.pointer.ResetUnit()
			// Prepare drag-to-select (use regionEditorLine for drag dispatch)
			p.selection.PrepareDrag(srcLine, srcCol, action.Region.Rect)
			p.mouseHandler.StartDrag(action.X, action.Y, regionEditorLine, 0)
			return p, cmd
		}
		return p, nil

	case regionEditorLine:
		if lineIdx, ok := action.Region.Data.(int); ok {
			p.activePane = PaneEditor
			if p.previewMode && p.previewContentLinkAt(action.X, action.Y) {
				p.previewCursorLine = lineIdx
				return p, nil
			}
			if p.viewFilter == FilterActive {
				if p.previewMode && p.ctx != nil && p.ctx.Config != nil && p.ctx.Config.Plugins.Notes.DefaultEditor == config.NotesEditorPane {
					return p, p.editSelectedNote()
				}
				srcLine, srcCol := p.clickToSource(action.X, action.Y)
				if p.previewMode {
					p.previewCursorLine = lineIdx
				}
				cmd := p.enterEditAt(srcLine, srcCol)
				p.pointer.ResetUnit()
				p.selection.PrepareDrag(srcLine, srcCol, action.Region.Rect)
				p.mouseHandler.StartDrag(action.X, action.Y, regionEditorLine, srcLine)
				return p, cmd
			}
			// Archived/deleted: read-only. Position the view cursor only.
			p.previewCursorLine = lineIdx
			col := p.editorColAtScreenX(action.X, lineIdx)
			p.selection.PrepareDrag(lineIdx, col, action.Region.Rect)
			p.mouseHandler.StartDrag(action.X, action.Y, regionEditorLine, lineIdx)
		}
		return p, nil

	case regionDivider:
		// Start drag with current list width
		p.mouseHandler.StartDrag(action.X, action.Y, regionDivider, p.listWidth)
		return p, nil
	}

	return p, nil
}

// handleMouseDoubleClick handles double click actions.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		// A scrollbar gesture is not a note open: the second press of a
		// double-press on the bar grabs it again (thumb grab continues,
		// track re-jumps) rather than being swallowed.
		return p.handleScrollbarPress(action)

	case regionNoteItem:
		idx, ok := action.Region.Data.(int)
		if !ok {
			return p, nil
		}
		p.cursor = idx
		p.activePane = PaneEditor
		return p, p.loadNoteIntoEditor()

	case regionEditorPane, regionEditorLine:
		p.activePane = PaneEditor
		if !p.previewMode {
			p.selectSourceUnitAt(action, tty.SelectUnitWord)
		} else if lineIdx, ok := action.Region.Data.(int); ok {
			p.previewCursorLine = lineIdx
		}
		return p, nil
	}

	return p, nil
}

// handleMouseTripleClick selects one logical source line in the built-in
// editor. Wrapped visual rows remain projections of that one source line.
func (p *Plugin) handleMouseTripleClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}
	if action.Region.ID != regionEditorPane && action.Region.ID != regionEditorLine {
		return p, nil
	}
	p.activePane = PaneEditor
	if !p.previewMode {
		p.selectSourceUnitAt(action, tty.SelectUnitLine)
	}
	return p, nil
}

func (p *Plugin) selectSourceUnitAt(action mouse.MouseAction, unit tty.SelectionUnit) {
	line, col := p.clickToSource(action.X, action.Y)
	p.setTextareaCursorPosition(line, col)
	p.trackTextareaScroll()
	start, end, ok := sourceUnitSpan(p.editorTextarea.Value(), srcPos{line: line, col: col}, unit)
	if !ok || !p.pointer.SelectMappedUnit(&p.selection, start.point(), end.point(), unit) {
		return
	}
	p.mouseHandler.StartDrag(action.X, action.Y, regionEditorLine, line)
	p.syncPreviewFromTextarea()
}

// handleMouseScroll handles scroll wheel actions.
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	inListPane := false
	if action.Region != nil {
		inListPane = action.Region.ID == regionListPane || action.Region.ID == regionNoteItem || action.Region.ID == regionListFilter || action.Region.ID == regionNewNote
	} else {
		inListPane = action.X < p.listWidth
	}

	delta := 3
	if action.Type == mouse.ActionScrollUp {
		delta = -3
	}

	if inListPane {
		// Scroll list by moving cursor. A wheel that cannot move the cursor
		// must not reload the editor: the note is already loaded, and the
		// reload is the expensive half of an inertia tail.
		cursor, changed := p.listBounds().Move(delta)
		if !changed {
			return p, nil
		}
		p.cursor = cursor
		return p, p.loadNoteIntoEditor()
	}

	// Scroll editor pane
	if p.editorNote == nil {
		return p, nil
	}

	if p.previewMode {
		p.ensureViewSurface()
		if len(p.viewSurface.Lines) == 0 && len(p.previewLines) == 0 {
			return p, nil
		}
		p.previewScrollOff, _ = p.previewBounds().Move(delta)
		p.keepPreviewCursorInView()
		return p, nil
	}

	// Textarea: move by source line if the cursor can move. Do not focus
	// and do not send keys at a line boundary. WheelAtBoundary stays
	// false here — see wheel.go.
	line := p.editorTextarea.Line()
	last := p.editorTextarea.LineCount() - 1
	if last < 0 {
		last = 0
	}
	if delta < 0 && line <= 0 {
		return p, nil
	}
	if delta > 0 && line >= last {
		return p, nil
	}
	var cmd tea.Cmd
	steps := 3
	if steps > last+1 {
		steps = last + 1
	}
	for i := 0; i < steps; i++ {
		line = p.editorTextarea.Line()
		if delta > 0 {
			if line >= last {
				break
			}
			p.editorTextarea, cmd = p.editorTextarea.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		} else {
			if line <= 0 {
				break
			}
			p.editorTextarea, cmd = p.editorTextarea.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		}
	}
	p.trackTextareaScroll()
	return p, cmd
}

// handleMouseDrag handles drag actions (pane resizing, text selection and
// scrollbar gestures).
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	switch p.mouseHandler.DragRegion() {
	case ui.RegionScrollbarThumb:
		return p.handleScrollbarDrag(action)
	case regionDivider:
		return p.handleDividerDrag(action)
	case regionEditorLine:
		return p.handleEditorSelectionDrag(action)
	}
	return p, nil
}

// handleDividerDrag handles dragging the pane divider to resize.
func (p *Plugin) handleDividerDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	startValue := p.mouseHandler.DragStartValue()
	newWidth := startValue + action.DragDX

	// Clamp to reasonable bounds
	available := p.width - dividerWidth
	minWidth := 20
	maxWidth := available - 40 // Leave at least 40 for editor
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if newWidth < minWidth {
		newWidth = minWidth
	} else if newWidth > maxWidth {
		newWidth = maxWidth
	}

	p.listWidth = newWidth
	return p, nil
}

// handleEditorSelectionDrag handles drag-to-select in the editor pane.
func (p *Plugin) handleEditorSelectionDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if p.editorNote == nil {
		return p, nil
	}

	if !p.previewMode {
		line, col := p.clickToSource(action.X, action.Y)
		if p.pointer.Unit() != tty.SelectUnitChar {
			start, end, ok := sourceUnitSpan(p.editorTextarea.Value(), srcPos{line: line, col: col}, p.pointer.Unit())
			if ok && p.pointer.ExtendMappedUnit(&p.selection, start.point(), end.point()) {
				p.syncPreviewFromTextarea()
			}
			return p, nil
		}
		// Edit mode: convert the click and the pointer (inclusive character
		// carets) into an exclusive source range so backward drags keep both
		// endpoint characters.
		click := srcFromPoint(p.selection.Anchor)
		if !p.selection.Anchor.Valid() {
			click = srcPos{line: line, col: col}
		}
		start, end := mouseExclusiveRange(click, srcPos{line: line, col: col}, p.editorTextarea.Value())
		p.selection.SelectRange(start.point(), end.point(), false)
		p.selection.Anchor = click.point()
		p.syncPreviewFromTextarea()
		return p, nil
	}

	p.ensureViewSurface()
	lineCount := len(p.viewSurface.Lines)
	if lineCount == 0 {
		return p, nil
	}

	currentLine := (action.Y - p.editorContentStartY()) + p.previewScrollOff
	if currentLine < 0 {
		currentLine = 0
	}
	maxLine := lineCount - 1
	if currentLine > maxLine {
		currentLine = maxLine
	}
	col := p.editorColAtScreenX(action.X, currentLine)
	p.selection.HandleDrag(currentLine, col)
	return p, nil
}

// handleMouseDragEnd handles the end of a drag operation.
func (p *Plugin) handleMouseDragEnd() (*Plugin, tea.Cmd) {
	return p.handleMouseDragEndRegion(p.mouseHandler.DragRegion())
}

func (p *Plugin) handleMouseDragEndRegion(region string) (*Plugin, tea.Cmd) {
	switch region {
	case ui.RegionScrollbarThumb:
		// Scroll offsets are ephemeral; nothing to persist or finalize.
		p.scrollbarDragEnded()
	case regionDivider:
		// Save the current list width to state
		_ = state.SetNotesListWidth(p.listWidth)
	case regionEditorLine:
		p.selection.FinishDrag()
		if p.copyOnSelect() && p.selection.HasSelection() {
			return p, p.copySelectionCmd()
		}
	}
	return p, nil
}

// handleMouseHover handles mouse hover for visual feedback.
func (p *Plugin) handleMouseHover(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	p.hoverDivider = action.Region != nil && action.Region.ID == regionDivider
	p.hoverNewNote = action.Region != nil && action.Region.ID == regionNewNote
	p.updateScrollbarHover(action.Region)
	return p, nil
}

// previewContentLinkAt reports whether a preview click landed on a span the
// app content deck can activate without a Ready snapshot (issue ids, http
// URLs, sidecar://note/…). Unresolved file/diff tokens must not match.
func (p *Plugin) previewContentLinkAt(x, y int) bool {
	if !p.previewMode || p.editorNote == nil {
		return false
	}
	p.ensureViewSurface()
	visual := p.screenYToVisualRow(y)
	if visual < 0 || visual >= len(p.viewSurface.Lines) {
		return false
	}
	col := p.screenXToEditorCol(x)
	line := p.viewSurface.Lines[visual]
	// Only kinds ScanFrame can emit without a host Ready snapshot. File and
	// diff hits stay pending until the app deck resolves them; treating those
	// as links here steals the click (no edit, no activation).
	kinds := contentlink.NewKindSet(
		contentlink.KindIssue,
		contentlink.KindURL,
		contentlink.KindInternal,
	)
	frame := contentlink.ScanFrame(line, contentlink.FrameOptions{
		InternalNamespaces: map[string]contentlink.URIOptions{
			"note": {ValidateID: validNotesInternalID},
		},
		AllowedKinds: kinds,
	})
	for _, span := range frame.Spans {
		if span.Kind != "" && kinds.Allows(span.Kind) && col >= span.StartCol && col <= span.EndCol {
			return true
		}
	}
	return false
}

func validNotesInternalID(id string) bool {
	if !strings.HasPrefix(id, "nt-") || len(id) < 4 || len(id) > 67 {
		return false
	}
	for _, r := range id[3:] {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// clickToSource maps a click in the editor body to a source line/column.
// Both rendered/raw view and textarea edit mode map through soft-wrapped visual
// rows. Sets previewCursorLine to that visual row so enterEditAt can keep the
// clicked screen row.
func (p *Plugin) clickToSource(x, y int) (line, col int) {
	visualInView := y - p.editorContentStartY()
	if visualInView < 0 {
		visualInView = 0
	}
	colInRow := p.screenXToEditorCol(x)
	if p.previewMode {
		visual := p.screenYToVisualRow(y)
		p.previewCursorLine = visual
		p.ensureViewSurface()
		return sourceAtVisualRow(p.viewSurface, visual, colInRow, p.editorTextarea.Value())
	}
	visual := p.editorTextarea.ScrollYOffset() + visualInView
	p.previewCursorLine = visual
	raw := markdown.MapWrappedSource(p.editorTextarea.Value(), p.editorLayout().wrapColumn)
	return sourceAtVisualRow(raw, visual, colInRow, p.editorTextarea.Value())
}

// sourceAtVisualRow maps a visual-row click to a source caret. Precise
// paragraphs/headings snap to the clicked word (or the closest occurrence
// near the wrap-math guess) so glamour wrap breakpoints do not land the
// cursor on a different token. Tables and fences stay at the top of the
// block. The fallback is wrap-segment start plus the rune offset in the
// visual row, clamped so a horizontal click cannot spill into the next
// wrap segment.
func sourceAtVisualRow(surface markdown.MappedRender, visual, colInRow int, source string) (line, col int) {
	// A click below the last rendered row is a click in empty space, not on
	// the last line's first column. Land the caret at the end of the text so
	// typing continues where the note left off.
	if visual >= len(surface.Lines) && len(surface.Lines) > 0 {
		return endOfSource(source)
	}
	a := surface.At(visual)
	if !a.Precise {
		return a.SourceLine, a.SourceCol
	}
	visualText := ""
	if visual >= 0 && visual < len(surface.Lines) {
		visualText = ansi.Strip(surface.Lines[visual])
	}
	runeInRow := visualColToRuneOffset(visualText, colInRow)
	if runeInRow < utf8.RuneCountInString(visualText) {
		if line, col, ok := landOnClickedWord(source, a, visualText, runeInRow); ok {
			return line, col
		}
	}
	line, col = a.SourceLine, a.SourceCol+max(0, runeInRow)
	if visual+1 < len(surface.Anchors) {
		next := surface.At(visual + 1)
		if next.SourceLine == line && next.SourceCol > a.SourceCol && col > next.SourceCol {
			col = next.SourceCol
		}
	}
	sourceLines := strings.Split(source, "\n")
	if line >= 0 && line < len(sourceLines) {
		col = min(col, utf8.RuneCountInString(sourceLines[line]))
	}
	if col < 0 {
		col = 0
	}
	return line, col
}

// endOfSource returns the caret position just past the last character of the
// note's final non-empty-trailing line.
func endOfSource(source string) (line, col int) {
	lines := strings.Split(source, "\n")
	last := len(lines) - 1
	// Trailing newlines render no visual row of their own; the natural landing
	// spot is the end of the last line that has text.
	for last > 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	return last, utf8.RuneCountInString(lines[last])
}

func visualColToRuneOffset(s string, col int) int {
	if col <= 0 || s == "" {
		return 0
	}
	graphemes := uniseg.NewGraphemes(s)
	runeOff := 0
	cell := 0
	for graphemes.Next() {
		cluster := graphemes.Str()
		w := rw.StringWidth(cluster)
		if w < 1 {
			w = 1
		}
		if cell+w > col {
			return runeOff
		}
		cell += w
		runeOff += utf8.RuneCountInString(cluster)
		if cell == col {
			return runeOff
		}
	}
	return runeOff
}

// sourceUnitSpan maps the shared word/line semantics onto Notes' exclusive
// logical source carets. The shared selector works in visual cells; converting
// both boundaries keeps wide and combining Unicode intact.
func sourceUnitSpan(content string, at srcPos, unit tty.SelectionUnit) (srcPos, srcPos, bool) {
	lines := sourceLines(content)
	at = clampSrc(at, lines)
	line := lines[at.line]
	runes := []rune(line)
	visualCol := uniseg.StringWidth(string(runes[:at.col]))
	start, end, ok := textselect.UnitSpanAt(unit, line, at.line, visualCol, textselect.DefaultTabWidth)
	if !ok {
		return srcPos{}, srcPos{}, false
	}
	return srcPos{line: at.line, col: visualColToRuneOffset(line, start.Col)},
		srcPos{line: at.line, col: visualColToRuneOffset(line, end.Col+1)}, true
}

func wordAt(s string, runeOff int) (word string, intra int) {
	runes := []rune(s)
	if len(runes) == 0 {
		return "", 0
	}
	if runeOff < 0 {
		runeOff = 0
	}
	if runeOff >= len(runes) {
		runeOff = len(runes) - 1
	}
	if unicode.IsSpace(runes[runeOff]) {
		return "", 0
	}
	start, end := runeOff, runeOff+1
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}
	return string(runes[start:end]), runeOff - start
}

func landOnClickedWord(source string, a markdown.Anchor, visualText string, runeInRow int) (line, col int, ok bool) {
	word, intra := wordAt(visualText, runeInRow)
	if word == "" {
		return 0, 0, false
	}
	lines := strings.Split(source, "\n")
	if len(lines) == 0 {
		return 0, 0, false
	}
	guess := a.SourceCol + max(0, runeInRow)
	bestLine, bestCol, bestDist := -1, 0, int(^uint(0)>>1)
	wr := []rune(word)
	consider := func(lineIdx int) {
		if lineIdx < 0 || lineIdx >= len(lines) {
			return
		}
		src := []rune(lines[lineIdx])
		for i := 0; i+len(wr) <= len(src); i++ {
			if string(src[i:i+len(wr)]) != word {
				continue
			}
			if !tokenBounded(src, i, i+len(wr)) {
				continue
			}
			dist := i - guess
			if dist < 0 {
				dist = -dist
			}
			// Prefer the anchored line, then nearby lines, then column distance.
			linePenalty := lineIdx - a.SourceLine
			if linePenalty < 0 {
				linePenalty = -linePenalty
			}
			dist += linePenalty * 1_000_000
			if dist < bestDist {
				bestDist = dist
				bestLine = lineIdx
				bestCol = i + intra
				if bestCol > i+len(wr) {
					bestCol = i + len(wr)
				}
			}
		}
	}
	consider(a.SourceLine)
	if bestLine >= 0 {
		return bestLine, bestCol, true
	}
	start := a.BlockStart
	if start < 0 {
		start = 0
	}
	for i := start; i < len(lines); i++ {
		if i == a.SourceLine {
			continue
		}
		consider(i)
		// Stay inside the block: a later blank line is a cheap stop once
		// we have walked a bit past the anchor.
		if i > a.SourceLine && strings.TrimSpace(lines[i]) == "" && bestLine >= 0 {
			break
		}
	}
	if bestLine < 0 {
		return 0, 0, false
	}
	return bestLine, bestCol, true
}

func tokenBounded(src []rune, start, end int) bool {
	if start > 0 && isWordRune(src[start-1]) {
		return false
	}
	if end < len(src) && isWordRune(src[end]) {
		return false
	}
	return true
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func (p *Plugin) screenYToVisualRow(y int) int {
	visualRow := y - p.editorContentStartY()
	if visualRow < 0 {
		visualRow = 0
	}
	row := p.previewScrollOff + visualRow
	p.ensureViewSurface()
	n := len(p.viewSurface.Lines)
	if n == 0 {
		return 0
	}
	if row >= n {
		row = n - 1
	}
	if row < 0 {
		row = 0
	}
	return row
}

// editorColAtScreenX maps a screen X coordinate to a visual column within
// the editor content for the given line index.
func (p *Plugin) editorColAtScreenX(x, lineIdx int) int {
	relX := p.screenXToEditorCol(x)

	if p.previewMode {
		p.ensureViewSurface()
		if lineIdx < 0 || lineIdx >= len(p.viewSurface.Lines) {
			return 0
		}
		line := p.viewSurface.Lines[lineIdx]
		if relX > len([]rune(line)) {
			return len([]rune(line))
		}
		return relX
	}

	if lineIdx < 0 || lineIdx >= len(p.previewLines) {
		return 0
	}

	line := p.previewLines[lineIdx]
	if relX > len(line) {
		return len(line)
	}
	return relX
}

// editorContentStartY returns the Y coordinate where editor content begins.
func (p *Plugin) editorContentStartY() int {
	// Pane top border, then the shared layout's body start row.
	return 1 + p.editorLayout().contentRow
}

// screenXToEditorCol converts a screen X coordinate to a column in editor content.
// Preview and edit share editorLayout: pane border+padding, then leftMargin.
func (p *Plugin) screenXToEditorCol(x int) int {
	l := p.editorLayout()
	editorContentX := p.listWidth + dividerWidth + 2 + l.leftMargin
	relX := x - editorContentX
	if relX < 0 {
		relX = 0
	}
	return relX
}

// copyOnSelect reports whether finishing a selection copies it. Off unless
// configured: a drag that silently replaces the clipboard surprises more people
// than it serves.
func (p *Plugin) copyOnSelect() bool {
	return p.ctx != nil && p.ctx.Config != nil && p.ctx.Config.Selection.CopyOnSelect
}

// copySelectionCmd returns a command that copies the selection to every
// clipboard within reach — the system clipboard and, over OSC 52, the
// terminal's.
func (p *Plugin) copySelectionCmd() tea.Cmd {
	lines := p.getSelectedText()
	if len(lines) == 0 {
		return nil
	}
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped = append(stripped, ansi.Strip(line))
	}
	return clip.Copy(strings.Join(stripped, "\n"), func(r clip.Result) tea.Msg {
		return app.FlashMsg{Text: r.Message("Copied to clipboard")}
	})
}

// getSelectedText returns the selected text lines.
func (p *Plugin) getSelectedText() []string {
	if p.editorNote == nil {
		return nil
	}
	if !p.previewMode {
		if !p.hasEditSelection() {
			return nil
		}
		start, end := orderSrc(srcFromPoint(p.selection.Start), srcFromPoint(p.selection.End))
		text := extractExclusive(sourceLines(p.editorTextarea.Value()), start, end)
		if text == "" {
			return nil
		}
		return strings.Split(text, "\n")
	}
	if !p.selection.HasSelection() {
		return nil
	}

	startLine := p.selection.Start.Line
	endLine := p.selection.End.Line

	p.ensureViewSurface()
	allLines := p.viewSurface.Lines
	if startLine < 0 || endLine >= len(allLines) {
		return nil
	}
	lines := allLines[startLine : endLine+1]
	if len(lines) == 0 {
		return nil
	}
	return p.selection.SelectedText(lines, startLine, 8)
}

// registerMouseRegions registers hit regions for mouse interaction.
// Called during View() to update regions based on current layout.
func (p *Plugin) registerMouseRegions() {
	p.mouseHandler.Clear()

	// Skip if dimensions not set
	if p.width == 0 || p.height == 0 {
		return
	}

	// Calculate layout
	p.calculatePaneWidths()

	// IMPORTANT: Add general regions FIRST, specific regions LAST
	// (they get tested in reverse order, so last = highest priority)

	// General pane regions (lower priority)
	p.mouseHandler.HitMap.AddRect(regionListPane, 0, 0, p.listWidth, p.height, nil)
	editorX := p.listWidth + dividerWidth
	editorWidth := p.width - editorX
	p.mouseHandler.HitMap.AddRect(regionEditorPane, editorX, 0, editorWidth, p.height, nil)

	// Divider region
	p.mouseHandler.HitMap.AddRect(regionDivider, p.listWidth, 0, dividerWidth, p.height, nil)

	// Note items in list (higher priority)
	p.registerListItemRegions()

	// Editor line regions (higher priority)
	p.registerEditorLineRegions()

	// Header controls are last so they win over the general list-pane region.
	p.registerListFilterRegion()
	p.registerNewNoteRegion()

	// Scrollbar bars are registered after every content region so the reverse
	// scan prefers them: a press on a bar must never select a note row or
	// place an editor caret. The inline editor gate lives inside
	// registerBodyScrollbarRegion.
	if snap, ok := p.listScrollbarSnapshot(); ok {
		p.scrollPointer.list = snap
	} else {
		p.scrollPointer.list = scrollBarSnapshot{}
	}
	p.registerListScrollbarRegion()
	p.registerBodyScrollbarRegion()
}

func (p *Plugin) registerListFilterRegion() {
	listInner := p.listWidth - paneChromeX
	if listInner < 1 {
		return
	}
	header := p.listHeader(listInner, len(p.getDisplayNotes()))
	if header.filterWidth < 1 {
		return
	}
	// RenderPanel content begins after the left border and its one-cell padding.
	p.mouseHandler.HitMap.AddRect(regionListFilter, 2+header.filterX, 1, header.filterWidth, 1, nil)
}

func (p *Plugin) registerNewNoteRegion() {
	if p.viewFilter != FilterActive {
		return
	}
	listInner := p.listWidth - paneChromeX
	if listInner < 1 {
		return
	}
	header := p.listHeader(listInner, len(p.getDisplayNotes()))
	if header.newWidth < 1 {
		return
	}
	p.mouseHandler.HitMap.AddRect(regionNewNote, 2+header.newX, 1, header.newWidth, 1, nil)
}

// registerListItemRegions registers click regions for visible note items.
func (p *Plugin) registerListItemRegions() {
	displayNotes := p.getDisplayNotes()
	if len(displayNotes) == 0 {
		return
	}

	// Calculate visible range
	headerLines := 2 // title + blank breathing row
	if p.searchMode || p.searchQuery() != "" {
		headerLines++ // + search input
	}
	contentHeight := p.height - 2 - headerLines // -2 for borders
	if contentHeight < 1 {
		contentHeight = 1
	}

	start := p.scrollOff
	end := start + contentHeight
	if end > len(displayNotes) {
		end = len(displayNotes)
	}

	// Y offset: border + header lines
	yOffset := 1 + headerLines

	for i := start; i < end; i++ {
		row := i - start
		// Each note item takes 1 row
		p.mouseHandler.HitMap.AddRect(
			regionNoteItem,
			0,           // x
			yOffset+row, // y
			p.listWidth, // width
			1,           // height (1 row per item)
			i,           // data = note index
		)
	}
}

// registerEditorLineRegions registers click regions for visible editor lines.
func (p *Plugin) registerEditorLineRegions() {
	if p.editorNote == nil {
		return
	}

	// Determine line count based on mode
	var lineCount int
	if p.previewMode {
		p.ensureViewSurface()
		lineCount = len(p.viewSurface.Lines)
	} else {
		lineCount = p.editorTextarea.LineCount()
	}
	if lineCount == 0 {
		return
	}

	// Calculate editor pane position
	editorX := p.listWidth + dividerWidth

	l := p.editorLayout()
	contentHeight := l.contentHeight
	editorWidth := l.wrapColumn

	start := p.previewScrollOff
	end := start + contentHeight
	if end > lineCount {
		end = lineCount
	}

	yOffset := p.editorContentStartY()

	for i := start; i < end; i++ {
		row := i - start
		rect := mouse.Rect{
			X: editorX + 2 + l.leftMargin, // border + panel padding + body inset
			Y: yOffset + row,
			W: editorWidth,
			H: 1,
		}
		p.mouseHandler.HitMap.Add(regionEditorLine, rect, i)
	}
}

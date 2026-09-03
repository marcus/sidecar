package notes

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// calculatePaneWidths sets the list and editor pane widths.
func (p *Plugin) calculatePaneWidths() {
	available := p.width - dividerWidth
	if p.listWidth == 0 {
		p.listWidth = available * 30 / 100
	}

	// Clamp listWidth to valid bounds
	minWidth := 20
	maxWidth := available - 40
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if p.listWidth < minWidth {
		p.listWidth = minWidth
	} else if p.listWidth > maxWidth {
		p.listWidth = maxWidth
	}
}

// renderView renders the full plugin view.
func (p *Plugin) renderView() string {
	if p.store == nil {
		return p.renderInitMessage()
	}
	if p.setupNeeded {
		return p.renderInitMessage()
	}
	if p.loading {
		return p.renderLoading()
	}
	if p.loadErr != nil {
		return p.renderError()
	}

	// Calculate layout dimensions
	contentHeight := p.height
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Register mouse regions for click detection
	p.registerMouseRegions()

	// Render two-pane layout
	return p.renderTwoPaneLayout(contentHeight)
}

// renderTwoPaneLayout renders the list and editor panes side by side.
func (p *Plugin) renderTwoPaneLayout(height int) string {
	p.calculatePaneWidths()

	// Pane height for panels (outer dimensions including borders)
	paneHeight := height
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height (excluding borders)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Determine if panes are active
	innerFocusActive := p.innerPaneFocusActive()
	listActive := innerFocusActive && p.activePane == PaneList && !p.searchMode
	editorActive := innerFocusActive && p.activePane == PaneEditor

	// Calculate editor width
	editorWidth := p.width - p.listWidth - dividerWidth

	// Render pane contents
	listContent := p.renderListPane(innerHeight)
	editorContent := p.renderEditorPane(innerHeight, editorWidth-4) // -4 for borders (2) and padding (2)

	// Apply panel styles
	leftPane := styles.RenderPanel(listContent, p.listWidth, paneHeight, listActive)
	rightPane := styles.RenderPanel(editorContent, editorWidth, paneHeight, editorActive)

	dragging := p.mouseHandler != nil && p.mouseHandler.IsDragging() && p.mouseHandler.DragRegion() == regionDivider
	divider := ui.RenderHandle(paneHeight, true, ui.HandleStateFrom(p.hoverDivider, dragging))

	// Join panes horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// renderListPane renders the list pane content (without borders).
func (p *Plugin) renderListPane(height int) string {
	var sb strings.Builder

	listInner := p.listWidth - paneChromeX
	if listInner < 1 {
		listInner = 1
	}

	// Get display notes (filtered or all).
	displayNotes := p.getDisplayNotes()
	noteCount := len(displayNotes)

	// One aligned header row: title at left, count and state control at right.
	sb.WriteString(p.listHeader(listInner, noteCount).view)
	sb.WriteString("\n")

	headerLines := 2 // title row + requested blank row

	// Search input line (if in search mode or has query)
	if p.searchMode || p.searchQuery() != "" {
		sb.WriteString(p.renderSearchInput(listInner))
		sb.WriteString("\n")
		headerLines++
	}
	// Keep one breathing row between the list chrome and note rows.
	sb.WriteString("\n")

	contentHeight := height - headerLines
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Empty state
	if noteCount == 0 {
		if p.searchQuery() != "" {
			// No matches for search - show create prompt
			sb.WriteString(styles.Muted.Render("No matches"))
			sb.WriteString("\n\n")
			sb.WriteString(styles.Subtle.Render("Press "))
			sb.WriteString(styles.Code.Render("Enter"))
			sb.WriteString(styles.Subtle.Render(" to create"))
		} else {
			sb.WriteString(styles.Muted.Render("No notes"))
			sb.WriteString("\n")
			sb.WriteString(styles.Subtle.Render("n=new"))
		}
		return sb.String()
	}

	// Calculate visible range with scroll offset
	p.ensureCursorVisibleForList(contentHeight, noteCount)
	start := p.scrollOff
	end := start + contentHeight
	if end > noteCount {
		end = noteCount
	}

	bodyWidth := listInner - scrollbarWidth
	if bodyWidth < 10 {
		bodyWidth = 10
	}

	var body strings.Builder
	for i := start; i < end; i++ {
		note := displayNotes[i]
		isSelected := i == p.cursor
		body.WriteString(p.renderNoteRow(note, isSelected, bodyWidth))
		if i < end-1 {
			body.WriteString("\n")
		}
	}

	// The list bar's geometry is derived from layout state so registration
	// and rendering share one source of truth.
	snap, ok := p.listScrollbarSnapshot()
	if !ok {
		return sb.String()
	}
	bar, _ := ui.RenderScrollbarWithState(snap.params, p.listScrollbarStyle())
	p.scrollPointer.list = snap
	sb.WriteString(attachScrollbar(body.String(), bar, bodyWidth, contentHeight))
	return sb.String()
}

type notesListHeader struct {
	view        string
	filterX     int
	filterWidth int
	newX        int
	newWidth    int
}

// listHeader is shared by paint and hit testing so the state pill cannot drift
// away from its mouse target as the count, filter, or Nerd Font setting changes.
func (p *Plugin) listHeader(width, noteCount int) notesListHeader {
	if width < 1 {
		return notesListHeader{}
	}
	count := fmt.Sprintf("%d", noteCount)
	if p.searchQuery() != "" {
		count = fmt.Sprintf("%d/%d", noteCount, len(p.notes))
	}
	count = styles.Muted.Render(count)
	pill := p.renderFilterPill()
	pillWidth := ansi.StringWidth(pill)
	newBtn, newWidth := p.renderNewNoteButton(width)
	right := count + " " + pill
	if newWidth > 0 {
		right += " " + newBtn
	}
	rightWidth := ansi.StringWidth(right)
	if rightWidth > width {
		right = pill
		if newWidth > 0 {
			right += " " + newBtn
		}
		rightWidth = ansi.StringWidth(right)
	}
	if rightWidth > width && newWidth > 0 {
		right = pill
		rightWidth = pillWidth
		newWidth = 0
	}
	if rightWidth > width {
		right = ansi.Truncate(right, width, "")
		rightWidth = ansi.StringWidth(right)
		pillWidth = rightWidth
		newWidth = 0
	}

	leftWidth := width - rightWidth
	left := ""
	if leftWidth > 0 {
		titleWidth := leftWidth
		if rightWidth > 0 {
			titleWidth-- // one separating cell before the right-aligned control
		}
		if titleWidth > 0 {
			left = styles.Title.Render(ansi.Truncate("Notes", titleWidth, ""))
		}
	}
	leftCells := ansi.StringWidth(left)
	view := left + strings.Repeat(" ", width-leftCells-rightWidth) + right
	filterX := width - pillWidth
	newX := 0
	if newWidth > 0 {
		newX = width - newWidth
		filterX = newX - 1 - pillWidth
	}
	return notesListHeader{
		view:        view,
		filterX:     filterX,
		filterWidth: pillWidth,
		newX:        newX,
		newWidth:    newWidth,
	}
}

func (p *Plugin) renderFilterPill() string {
	return styles.RenderPillWithStyle(p.viewFilter.String(), p.noteFilterStyle(), nil)
}

func (p *Plugin) renderNewNoteButton(width int) (string, int) {
	if p.viewFilter != FilterActive {
		return "", 0
	}
	style := p.noteFilterStyle()
	if p.hoverNewNote {
		style = styles.ButtonHover.Padding(0, 1).Bold(true)
	}
	label := "+ New"
	if width >= 42 {
		label = "+ New Note"
	}
	btn := styles.RenderPillWithStyle(label, style, nil)
	return btn, ansi.StringWidth(btn)
}

func (p *Plugin) noteFilterStyle() lipgloss.Style {
	style := lipgloss.NewStyle().Background(styles.BgTertiary).Padding(0, 1).Bold(true)
	switch p.viewFilter {
	case FilterArchived:
		style = style.Foreground(styles.Info)
	case FilterDeleted:
		style = style.Foreground(styles.Error)
	default:
		style = style.Foreground(styles.Success)
	}
	return style
}

// renderEditorPane renders the editor pane content (without borders).
func (p *Plugin) renderEditorPane(height, width int) string {
	// Render inline editor if active
	if p.edit.Active && p.edit.Model != nil {
		return p.renderInlineEditorContent(height)
	}

	// No note selected - show placeholder
	if p.editorNote == nil {
		return p.renderEditorPlaceholder(height)
	}

	l := p.editorLayout()
	bar := p.editorScrollbar(l)
	var body string
	if p.previewMode {
		body = p.renderViewSurface(l.contentHeight)
	} else {
		p.editorTextarea.SetWidth(l.wrapColumn)
		p.editorTextarea.SetHeight(l.contentHeight)
		body = p.editorTextarea.View()
		if p.selection.HasSelection() {
			body = p.overlaySelectionOnEditor(body)
		}
	}

	lines := []string{p.renderEditorStatusHeader(l.innerWidth)}
	for i := 0; i < editorTopRows; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, strings.Split(insetEditorBody(body, bar, l), "\n")...)
	for i := 0; i < editorBottomRows; i++ {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// renderViewSurface draws the current mapped view (glamour or wrapped raw)
// with no gutter and no '~' filler. Height is the content-row count from
// editorLayout. Visual rows are already wrapped to wrapColumn.
func (p *Plugin) renderViewSurface(height int) string {
	p.ensureViewSurface()
	p.clampPreviewScroll()

	lines := p.viewSurface.Lines
	if len(lines) == 0 {
		lines = []string{""}
	}

	start := p.previewScrollOff
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		line := lines[i]
		if !p.markdownView {
			line = styles.Body.Render(line)
		}
		line = p.highlightNoteSearchLine(i, line)
		if p.selection.HasSelection() && p.selection.IsLineSelected(i) {
			startCol, endCol := p.selection.GetLineSelectionCols(i)
			line = ui.InjectCharacterRangeBackground(line, startCol, endCol)
		}
		sb.WriteString(line)
		if i < end-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderPreviewContent draws the current view surface. Tests that want a
// specific wrap/render mode should set markdownView / previewLines first.
func (p *Plugin) renderPreviewContent(height, width int) string {
	_ = width
	return p.renderViewSurface(height)
}

// truncatePreviewLine cuts to wrapWidth cells without splitting a rune.
func truncatePreviewLine(line string, wrapWidth int) string {
	if wrapWidth < 1 {
		return ""
	}
	if ansi.StringWidth(line) <= wrapWidth {
		return line
	}
	return ansi.Truncate(line, wrapWidth, ">")
}

// overlaySelectionOnEditor paints the current exclusive source selection
// onto the textarea surface. Visual rows are remapped through the same
// wrap policy the textarea uses; syntax spans stay deferred.
func (p *Plugin) overlaySelectionOnEditor(view string) string {
	if !p.hasEditSelection() {
		return view
	}
	start, end := orderSrc(srcFromPoint(p.selection.Start), srcFromPoint(p.selection.End))
	raw := markdown.MapWrappedSource(p.editorTextarea.Value(), p.editorLayout().wrapColumn)
	return overlayExclusiveOnView(view, raw, start, end, p.editorTextarea.Value(), p.editorTextarea.ScrollYOffset())
}

// renderEditorStatusHeader renders the persistent status header line.
// Right: the best date detail that fits followed by the actionable save-state
// symbol. Date and view-mode detail degrade before the symbol on narrow panes.
func (p *Plugin) renderEditorStatusHeader(width int) string {
	if p.editorNote == nil {
		return ""
	}
	if width <= 0 {
		return ""
	}

	leftText := ""
	if p.noteSearchMode || p.noteSearchQuery() != "" {
		leftText = p.renderNoteSearchPrompt()
	} else if p.previewMode {
		if p.markdownView {
			leftText = "[md]"
		} else {
			leftText = "[raw]"
		}
	}
	leftPart := styles.Muted.Render(leftText)
	leftWidth := lipgloss.Width(leftPart)

	createdStr := p.editorNote.CreatedAt.Format("Jan 2, 2006")
	updatedStr := p.editorNote.UpdatedAt.Format("Jan 2, 2006")
	dateCandidates := []string{
		fmt.Sprintf("Created: %s | Updated: %s", createdStr, updatedStr),
		fmt.Sprintf("Updated: %s", updatedStr),
		updatedStr,
		"",
	}
	stateSymbol, stateStyle := "•", styles.StatusStaged
	if p.saveErr != nil {
		stateSymbol, stateStyle = "!", styles.StatusDeleted
	} else if p.editorDirty || p.saveInFlight || p.exportSaveInFlight {
		stateSymbol, stateStyle = "*", styles.StatusModified
	}
	statePart := stateStyle.Render(stateSymbol)
	for _, dateText := range dateCandidates {
		rightPart := statePart
		if dateText != "" {
			rightPart = styles.Muted.Render(dateText) + " " + statePart
		}
		rightWidth := lipgloss.Width(rightPart)
		if rightWidth > width {
			continue
		}
		if leftWidth > 0 && leftWidth+1+rightWidth <= width {
			return leftPart + strings.Repeat(" ", width-leftWidth-rightWidth) + rightPart
		}
		return strings.Repeat(" ", width-rightWidth) + rightPart
	}

	return ""
}

// renderEditorPlaceholder shows when no note is selected.
func (p *Plugin) renderEditorPlaceholder(height int) string {
	var sb strings.Builder
	sb.WriteString(styles.Muted.Render("No note selected"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Subtle.Render("Select a note from the list"))
	sb.WriteString("\n")
	sb.WriteString(styles.Subtle.Render("or press "))
	sb.WriteString(styles.Code.Render("n"))
	sb.WriteString(styles.Subtle.Render(" to create new"))
	return sb.String()
}

// previewMaxScroll returns the largest previewScrollOff that still fills the
// viewport. Both modes use soft-wrapped visual-row offsets.
func (p *Plugin) previewMaxScroll(viewHeight, viewWidth int) int {
	if viewHeight < 1 {
		return 0
	}
	if p.previewMode {
		p.ensureViewSurface()
		n := len(p.viewSurface.Lines)
		if n <= viewHeight {
			return 0
		}
		return n - viewHeight
	}
	raw := markdown.MapWrappedSource(p.editorTextarea.Value(), viewWidth)
	if len(raw.Lines) <= viewHeight {
		return 0
	}
	return len(raw.Lines) - viewHeight
}

// clampPreviewScroll keeps the view offset in range without moving it to
// follow the reading cursor. Paint and wheel must use this; snapping to
// the cursor here would undo a wheel that did not also move the cursor.
func (p *Plugin) clampPreviewScroll() {
	height, width := p.previewViewport()
	if p.previewScrollOff < 0 {
		p.previewScrollOff = 0
	}
	maxScroll := p.previewMaxScroll(height, width)
	if p.previewScrollOff > maxScroll {
		p.previewScrollOff = maxScroll
	}
}

// keepPreviewCursorInView moves the reading cursor into the current
// viewport. Wheel uses this so Enter/i/paste stay tied to what is on
// screen, without pulling the viewport back to an old cursor.
func (p *Plugin) keepPreviewCursorInView() {
	height, _ := p.previewViewport()
	if height < 1 {
		height = 1
	}
	n := len(p.previewLines)
	if p.previewMode {
		p.ensureViewSurface()
		n = len(p.viewSurface.Lines)
	}
	if n < 1 {
		p.previewCursorLine = 0
		return
	}
	if p.previewCursorLine < p.previewScrollOff {
		p.previewCursorLine = p.previewScrollOff
	}
	last := p.previewScrollOff + height - 1
	if last >= n {
		last = n - 1
	}
	if p.previewCursorLine > last {
		p.previewCursorLine = last
	}
	if p.previewCursorLine < 0 {
		p.previewCursorLine = 0
	}
}

// ensurePreviewCursorVisibleWithHeight adjusts preview scroll offset for given
// viewport dimensions.
func (p *Plugin) ensurePreviewCursorVisibleWithHeight(viewHeight, viewWidth int) {
	n := len(p.previewLines)
	if p.previewMode {
		p.ensureViewSurface()
		n = len(p.viewSurface.Lines)
	}
	if n == 0 {
		return
	}
	if p.previewCursorLine < 0 {
		p.previewCursorLine = 0
	}
	if p.previewCursorLine >= n {
		p.previewCursorLine = n - 1
	}
	if p.previewCursorLine < p.previewScrollOff {
		p.previewScrollOff = p.previewCursorLine
	}
	if p.previewCursorLine >= p.previewScrollOff+viewHeight {
		p.previewScrollOff = p.previewCursorLine - viewHeight + 1
	}
	if p.previewScrollOff < 0 {
		p.previewScrollOff = 0
	}
	maxScroll := p.previewMaxScroll(viewHeight, viewWidth)
	if p.previewScrollOff > maxScroll {
		p.previewScrollOff = maxScroll
	}
}

// renderInitMessage shows when td is not initialized.
func (p *Plugin) renderInitMessage() string {
	var sb strings.Builder
	sb.WriteString(styles.Title.Render("Notes"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Muted.Render("Notes uses td storage, and this project is not initialized yet."))
	sb.WriteString("\n")
	sb.WriteString(styles.Code.Render("Press Enter to set up td, or r to check again."))
	return sb.String()
}

// renderLoading shows a loading indicator.
func (p *Plugin) renderLoading() string {
	var sb strings.Builder
	sb.WriteString(styles.Title.Render("Notes"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Muted.Render("Loading notes..."))
	return sb.String()
}

// renderError shows an error message.
func (p *Plugin) renderError() string {
	var sb strings.Builder
	sb.WriteString(styles.Title.Render("Notes"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.StatusDeleted.Render("Error: "))
	sb.WriteString(styles.Muted.Render(p.loadErr.Error()))
	return sb.String()
}

// renderSearchInput renders the search input line.
//
// It draws through queryfield.RenderRow, so the note list's `/` row is the row
// the rest of the app shows. The match count is not repeated here — the list
// header already carries `matched/total` — and the × is not drawn: this row
// has no registered hit region, and a control nothing listens to is worse than
// no control.
func (p *Plugin) renderSearchInput(width int) string {
	row, _ := queryfield.RenderRow(width, queryfield.Row{
		Query:       p.searchQuery(),
		Cursor:      p.searchField.Cursor(),
		Focused:     p.searchMode,
		Placeholder: "search notes…",
	})
	return row
}

// Note status icon constants
const (
	iconArchived = "\u25cb" // White circle for archived
	iconDeleted  = "\u00d7" // Multiplication sign (x) for deleted
)

// renderNoteRow renders a single note row.
// Active notes show just the title; archived/deleted notes show icon + title.
func (p *Plugin) renderNoteRow(note Note, selected bool, maxWidth int) string {
	var prefix strings.Builder

	// Status icon only for archived/deleted notes (no placeholder for active)
	if note.DeletedAt != nil {
		prefix.WriteString(styles.StatusDeletedNote.Render(iconDeleted))
		prefix.WriteString(" ")
	} else if note.Archived {
		prefix.WriteString(styles.StatusArchived.Render(iconArchived))
		prefix.WriteString(" ")
	}

	// Cursor indicator
	if selected {
		prefix.WriteString(styles.ListCursor.Render("> "))
	} else {
		prefix.WriteString("  ")
	}

	// Pin badge
	if note.Pinned {
		prefix.WriteString(styles.StatusModified.Render("* "))
	}

	prefixStr := prefix.String()
	prefixLen := lipgloss.Width(prefixStr)

	// Calculate available width for title
	titleWidth := maxWidth - prefixLen
	if titleWidth < 10 {
		titleWidth = 10
	}

	// Get title (first line of content, or "untitled" if empty). The rule lives
	// in workspaceops because the create modal's note picker names its rows the
	// same way; a private copy here is how the picker came to show bare ids for
	// the notes this list showed by name.
	title := workspaceops.NoteTitle(note.Title, note.Content)
	if title == "" {
		title = "untitled"
	}

	// Truncate by terminal cells; byte slicing can split Unicode titles.
	if ansi.StringWidth(title) > maxTitleLength {
		title = ansi.Truncate(title, maxTitleLength, "...")
	}

	if ansi.StringWidth(title) > titleWidth {
		title = ansi.Truncate(title, titleWidth, "...")
	}

	row := prefixStr + styles.Body.Render(title)

	// A selected row is the same row, highlighted. It used to be rebuilt as
	// plain text so that a single lipgloss Background() would cover it, which
	// dropped the status icon's colour, the cursor and the pin badge — the
	// selected row read as a different kind of row. ui.RowBackground holds the
	// background across the styled spans' own resets instead, and does the
	// truncation and padding, so the highlight is uniform and the styling
	// survives.
	if selected {
		return ui.RowBackground(row, maxWidth, styles.BgTertiary)
	}

	// Regular row with styled components
	return row
}

// ensureCursorVisibleForList adjusts scrollOff for a list of given size.
func (p *Plugin) ensureCursorVisibleForList(viewHeight, listSize int) {
	// Clamp cursor to valid range
	if p.cursor < 0 {
		p.cursor = 0
	}
	if listSize > 0 && p.cursor >= listSize {
		p.cursor = listSize - 1
	}

	// Adjust scroll offset to keep cursor in view
	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	}
	if p.cursor >= p.scrollOff+viewHeight {
		p.scrollOff = p.cursor - viewHeight + 1
	}

	// Clamp scroll offset
	if p.scrollOff < 0 {
		p.scrollOff = 0
	}
	maxScroll := listSize - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollOff > maxScroll {
		p.scrollOff = maxScroll
	}
}

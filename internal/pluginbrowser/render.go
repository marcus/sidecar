package pluginbrowser

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacelist"
)

const (
	// listShare is how much of the box the collection takes when the detail box
	// sits beside it. The remainder, less the one-column gap, is the detail.
	listSharePercent = 61
	// paneGap is the one column a split leaves between two boxes.
	paneGap = 1
	// detailFloor is the narrowest detail box worth drawing. Below it the list
	// takes the whole box and Enter has nowhere to put a document, which is the
	// same answer a narrow pane gives.
	detailFloor = 34
	// narrowTable is the inner width below which rows reflow to two lines. It
	// is set where a four-column table stops being a table: below it the
	// secondary cell has so few cells left that every row reads as an ellipsis,
	// and two honest lines say more than one cramped one.
	narrowTable = 64
	// chromeOverhead is what a framed box spends: one border column and one
	// padding column on each side.
	chromeOverhead = 4
	// columnGap is the gap between two table columns.
	columnGap = 2
	// cursorGutter is the two columns every row keeps for the cursor, so text
	// never shifts as the cursor moves.
	cursorGutter = 2
	// statusColumnMax bounds the reserved, unlabelled status column.
	statusColumnMax = 24
	// scrollbarCols is the single column each scrolling box keeps for its bar.
	// It is reserved whether or not a thumb is in it, which is the shared
	// renderer's own convention and what keeps content from reflowing the
	// moment a page grows past its box.
	scrollbarCols = 1
)

// View renders the browser, held to exactly the box it was given. A content
// that hands back more rows than its box would push the app header off screen,
// so the fit is enforced here rather than trusted to the caller.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	// One hit map, cleared here and rebuilt in paint order. Every frame answers
	// for itself: a region left over from the last one is a target for something
	// that is no longer on screen.
	m.pointer().Clear()
	m.geom = frameGeom{}
	// A pane leaf shows one tab shape at a time; see pane.go for why.
	if m.paneMode() {
		return m.paneView()
	}
	listOuter, detailOuter := m.split()

	listActive := m.focused && m.focus == FocusList && m.innerFocusActive()
	list := styles.RenderPanel(
		strings.Join(m.listLines(listOuter-chromeOverhead, m.height-2), "\n"),
		listOuter, m.height, listActive)
	if detailOuter <= 0 {
		m.registerRegions(mouse.Rect{W: listOuter, H: m.height}, mouse.Rect{}, mouse.Rect{})
		return m.overlayView(ui.FitBlock(list, m.width, m.height))
	}
	detailActive := m.focused && m.focus == FocusDetail && m.innerFocusActive()
	detail := styles.RenderPanel(
		strings.Join(m.detailLines(detailOuter-chromeOverhead, m.height-2), "\n"),
		detailOuter, m.height, detailActive)
	// The gap between two boxes is a rail, not a blank column: it is the same
	// handle the pane tree draws, in the same three states.
	rail := ui.RenderHandle(m.height, true, ui.HandleStateFrom(m.hoverRail, m.railDragging()))
	joined := lipgloss.JoinHorizontal(lipgloss.Top, list, rail, detail)
	m.registerRegions(
		mouse.Rect{W: listOuter, H: m.height},
		mouse.Rect{X: listOuter + paneGap, W: detailOuter, H: m.height},
		// Widened one cell into each neighbour's border, as DividerHitBox does
		// for a pane-tree rail. The cell either side is a border, never a
		// header row, so nothing clickable is masked.
		mouse.Rect{X: listOuter - 1, W: paneGap + 2, H: m.height},
	)
	return m.overlayView(ui.FitBlock(joined, m.width, m.height))
}

// railDragging reports that the split is being dragged right now, which is what
// paints the handle in its drag colour.
func (m *Model) railDragging() bool {
	return m.mouse != nil && m.mouse.IsDragging() && m.mouse.DragRegion() == regionRail
}

// ViewIsSelfConstrained reports that View already returns exactly the box it
// was given, so the shell skips its defensive clamp.
func (m *Model) ViewIsSelfConstrained() bool { return true }

// split decides the two outer widths. A box too narrow for both gives the whole
// of itself to the list.
func (m *Model) split() (list, detail int) {
	if m.paneMode() {
		// One shape per tab: whichever this pane is, it gets the whole box.
		if m.paneShape == PaneDocument {
			return 0, m.width
		}
		return m.width, 0
	}
	if m.width < detailFloor*2 {
		return m.width, 0
	}
	list = m.width * m.listShare() / 100
	detail = m.width - list - paneGap
	if detail < detailFloor {
		return m.width, 0
	}
	if list-chromeOverhead < 20 {
		return m.width, 0
	}
	return list, detail
}

// tableRows is how many rows the table body has room for, which is what a page
// step moves by and what the scroll window is measured against.
func (m *Model) tableRows() int {
	listOuter, _ := m.split()
	w := scrolledWidth(listOuter - chromeOverhead)
	h := m.height - 2
	rows := h - m.listChromeRows(w)
	if m.rowLines(w) > 1 {
		rows /= m.rowLines(w)
	}
	if rows < 1 {
		return 1
	}
	return rows
}

// scrolledWidth is the columns content keeps once the scrollbar's own column is
// reserved. The column is reserved whether or not there is a thumb in it, which
// is what stops the table reflowing the moment a page grows past its box.
func scrolledWidth(width int) int {
	if width-scrollbarCols < 1 {
		return max0(width)
	}
	return width - scrollbarCols
}

func (m *Model) rowLines(width int) int {
	if width < narrowTable {
		return 2
	}
	return 1
}

// listChromeRows is every row of the list box that is not a table row.
func (m *Model) listChromeRows(width int) int {
	// Title, then the blank row under it.
	rows := 2
	if c, ok := m.ActiveCollection(); ok && c.Search != pluginhost.SearchNone {
		// The query row owns its own line, with one blank row of padding under
		// it. A blank row and a rule say the same thing; the blank one is
		// quieter.
		rows += 2
	}
	if m.rowLines(width) == 1 {
		// Column headings and the rule under them.
		rows += 2
	}
	s := m.activeState()
	notices := 0
	if s != nil {
		notices = len(s.notices)
	}
	// The rule under the table, its notices, and the summary line.
	rows += 1 + notices + 1
	return rows
}

// listLines is the whole list box, already fitted to width and height.
func (m *Model) listLines(width, height int) []string {
	if width < 1 || height < 1 {
		return nil
	}
	if !m.described {
		return fitLines(m.unreadyLines(width), width, height)
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return fitLines(m.noCollectionsLines(width), width, height)
	}
	s := m.state(c)

	title, pill := m.titleRow(c, width)
	m.geom.pill = pill
	lines := []string{title, ""}
	if c.Search != pluginhost.SearchNone {
		row, outcome := m.queryRow(c, s, width)
		m.geom.query = mouse.Rect{Y: len(lines), W: width, H: 1}
		if outcome.W > 0 {
			outcome.Y = len(lines)
			m.geom.outcome = outcome
		}
		lines = append(lines, row, "")
	}

	bodyTop := len(lines)
	body := m.tableBlock(c, s, width, height-len(lines)-1-len(s.notices)-1)
	shiftGeom(&m.geom, bodyTop)
	lines = append(lines, body...)

	lines = append(lines, styles.Muted.Render(strings.Repeat("─", width)))
	for _, notice := range s.notices {
		m.geom.notices = append(m.geom.notices, mouse.Rect{Y: len(lines), W: width, H: 1})
		lines = append(lines, m.noticeLine(notice, width))
	}
	lines = append(lines, m.summaryRow(c, s, width))
	clampGeom(&m.geom, height)
	return fitLines(lines, width, height)
}

// shiftGeom moves the table block's own coordinates into the list box's, which
// is the one place the two frames of reference meet.
func shiftGeom(g *frameGeom, top int) {
	for i := range g.rows {
		g.rows[i].rect.Y += top
	}
	g.listBar.track.Y += top
	g.listBar.thumb.Y += top
}

// clampGeom drops anything the box did not have room for. fitLines cuts the
// block to height, and a target on a row that was cut is a target for something
// nobody can see.
func clampGeom(g *frameGeom, height int) {
	rows := g.rows[:0]
	for _, row := range g.rows {
		if row.rect.Y < height {
			if row.rect.Y+row.rect.H > height {
				row.rect.H = height - row.rect.Y
			}
			rows = append(rows, row)
		}
	}
	g.rows = rows
	notices := g.notices[:0]
	for _, notice := range g.notices {
		if notice.Y < height {
			notices = append(notices, notice)
		}
	}
	g.notices = notices
	if g.query.Y >= height {
		g.query, g.outcome = mouse.Rect{}, mouse.Rect{}
	}
}

// titleRow is the pane's first inner row: the surface's identity on the left,
// the View control right-aligned, and the control's placement so the frame can
// give it a hit rect. Sidecar never writes a title into a border.
//
// The control is placed through ui.ReserveHeaderControls, which is what keeps
// it whole: a pill clipped to "⇅ Relev…" is a target whose meaning a reader
// cannot recover but whose click still fires. The rung that does not fit is
// dropped entirely, and its hit rect with it.
func (m *Model) titleRow(c pluginhost.Collection, width int) (string, mouse.Rect) {
	titleText := m.Name() + " · " + c.Title
	left := styles.Title.Render(ansi.Truncate(titleText, width, "…"))
	if !m.hasViewControl(c) {
		return left, mouse.Rect{}
	}
	titleW := ansi.StringWidth(ansi.Truncate(titleText, width, "…"))
	// The same three-rung ladder the workspace list's header uses: the control
	// sheds its applied view before its sort word, and its sort word before its
	// glyph.
	for _, label := range m.viewPillLadder(c) {
		pill := styles.RenderPillWithStyle(label, styles.Button, nil)
		pillW := ansi.StringWidth(pill)
		reserve := ui.ReserveHeaderControls(width, ui.HeaderControl{Width: pillW})
		if len(reserve.Controls) == 0 || reserve.Controls[0].Width == 0 {
			continue
		}
		col := reserve.Controls[0].Col
		if col < titleW+1 {
			continue
		}
		row := left + strings.Repeat(" ", col-titleW) + pill
		return row, mouse.Rect{X: col, W: pillW, H: 1}
	}
	return left, mouse.Rect{}
}

// viewPillLadder is the control's forms, widest first.
func (m *Model) viewPillLadder(c pluginhost.Collection) []string {
	full := m.viewPillLabel(c)
	word := workspacelist.SortGlyph + " " + sortLabel(c, m.state(c).sortKey)
	ladder := []string{full}
	if word != full {
		ladder = append(ladder, word)
	}
	return append(ladder, workspacelist.SortGlyph)
}

// hasViewControl reports whether the collection has anything the View modal
// could offer. A control that opens an empty modal is worse than no control.
func (m *Model) hasViewControl(c pluginhost.Collection) bool {
	return len(c.Sort) > 0 || len(c.Views) > 0
}

// viewPillLabel folds the applied view into the sort pill's label rather than
// spending a row on a chip line, because the list-row grammar has no chip line
// to give it.
func (m *Model) viewPillLabel(c pluginhost.Collection) string {
	s := m.state(c)
	label := workspacelist.SortGlyph + " " + sortLabel(c, s.sortKey)
	if title, ok := viewTitle(c, s.view); ok {
		label += " · " + title
	}
	return label
}

func sortLabel(c pluginhost.Collection, id string) string {
	for _, key := range c.Sort {
		if key.ID == id {
			return key.Label
		}
	}
	return "default"
}

func viewTitle(c pluginhost.Collection, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	for _, v := range c.Views {
		if v.ID == id {
			return v.Title, true
		}
	}
	return "", false
}

// queryRow is the search line, drawn as the app's filter row: muted prompt and
// placeholder while idle, the title style, the text and a ▌ block caret while
// it is taking text, and the count and outcome right-aligned. It reports where
// that right-hand cell landed so the frame can make the outcome clickable.
//
// It goes through workspacelist.RenderQueryRow rather than imitating it, so
// this row and the workspace sidebar's cannot drift on what a focused query
// bar looks like.
func (m *Model) queryRow(c pluginhost.Collection, s *collectionState, width int) (string, mouse.Rect) {
	right := m.outcomeSummary(c, s)
	if s.atLimit {
		// Query-bound feedback for a keystroke the bound refused, on the row
		// that refused it.
		right = styles.Body.Foreground(styles.Warning).Render("query is as long as Sidecar keeps")
	}
	row := workspacelist.RenderQueryRow(width, workspacelist.QueryRow{
		Query:       s.query,
		Focused:     s.editing,
		Placeholder: queryPlaceholder(c),
		Right:       right,
	})
	rightW := ansi.StringWidth(right)
	if rightW == 0 || rightW >= width {
		return row, mouse.Rect{}
	}
	return row, mouse.Rect{X: width - rightW, W: rightW, H: 1}
}

func queryPlaceholder(c pluginhost.Collection) string {
	if c.Search == pluginhost.SearchRequired {
		return "search " + c.Title
	}
	return "filter " + c.Title
}

// outcomeSummary is the count and the honest word for what the page claims.
func (m *Model) outcomeSummary(c pluginhost.Collection, s *collectionState) string {
	if !s.loaded && !s.loading {
		return ""
	}
	if s.loading {
		return styles.Muted.Render("refreshing…")
	}
	if c.Search == pluginhost.SearchRequired && strings.TrimSpace(s.query) == "" {
		return styles.Subtle.Render("no query")
	}
	count := fmt.Sprintf("%d results", len(s.items))
	if s.total > 0 && s.total != len(s.items) {
		count = fmt.Sprintf("%d of %d", len(s.items), s.total)
	}
	return styles.Muted.Render(count) + "   " + outcomeStyle(s.outcome).Render(string(s.outcome))
}

func outcomeStyle(outcome pluginhost.PageOutcome) lipgloss.Style {
	switch outcome {
	case pluginhost.OutcomeDegraded:
		return styles.Body.Foreground(styles.Warning)
	case pluginhost.OutcomeAbstained:
		return styles.Subtle
	default:
		return styles.Body.Foreground(styles.Success)
	}
}

// tableBlock is the column headings, the rule, and the rows — or the empty
// state that replaces all three.
func (m *Model) tableBlock(c pluginhost.Collection, s *collectionState, width, height int) []string {
	if height < 1 {
		return nil
	}
	// The bar's column is reserved on every table, so the columns underneath do
	// not reflow when a page grows past its box.
	inner := scrolledWidth(width)
	if len(s.items) == 0 {
		return centreBlock(m.emptyLines(c, s, inner), width, height)
	}
	narrow := m.rowLines(inner) > 1
	cols := layoutColumns(c, s, inner, narrow)

	var lines []string
	if !narrow {
		lines = append(lines, headerRow(cols, inner), headerRule(cols, inner))
	}
	top := len(lines)
	rows := height - top
	perRow := m.rowLines(inner)
	visible := rows / perRow
	if visible < 1 {
		visible = 1
	}
	m.setListWindow(s, visible)
	end := s.scroll + visible
	if end > len(s.items) {
		end = len(s.items)
	}
	for i := s.scroll; i < end; i++ {
		rendered := m.itemRows(c, cols, s, i, inner, narrow)
		m.geom.rows = append(m.geom.rows, rowRect{
			index: i,
			rect:  mouse.Rect{Y: len(lines), W: inner, H: len(rendered)},
		})
		lines = append(lines, rendered...)
	}
	if s.paging {
		lines = append(lines, styles.Subtle.Render(ansi.Truncate("  loading more…", inner, "…")))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	lines = lines[:height]
	m.geom.listBar = m.joinScrollbar(lines, top, inner, width, ui.ScrollbarParams{
		TotalItems:   len(s.items),
		ScrollOffset: s.scroll,
		VisibleItems: visible,
		TrackHeight:  max0(height - top),
	}, m.listBar.style())
	return lines
}

// joinScrollbar draws the shared scrollbar down the reserved column beside a
// block of rows, in place, and reports where it landed. Nothing else in the
// browser decides what a scrollbar looks like.
func (m *Model) joinScrollbar(lines []string, top, inner, width int, params ui.ScrollbarParams, style ui.ScrollbarStyle) barGeom {
	if params.TrackHeight < 1 || inner < 1 || width-inner != scrollbarCols {
		return barGeom{}
	}
	bar, geo := ui.RenderScrollbarWithState(params, style)
	if bar == "" {
		return barGeom{}
	}
	cells := strings.Split(bar, "\n")
	for i := 0; i < params.TrackHeight && top+i < len(lines); i++ {
		lines[top+i] = fitStyled(lines[top+i], inner) + cells[i]
	}
	if !geo.HasThumb {
		return barGeom{}
	}
	return barGeom{
		has:    true,
		track:  mouse.Rect{X: inner, Y: top, W: scrollbarCols, H: params.TrackHeight},
		thumb:  mouse.Rect{X: inner, Y: top + geo.ThumbRect.Min.Y, W: scrollbarCols, H: geo.ThumbRect.Dy()},
		params: params,
	}
}

// setListWindow keeps the cursor inside the visible rows.
func (m *Model) setListWindow(s *collectionState, visible int) {
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	if s.cursor >= s.scroll+visible {
		s.scroll = s.cursor - visible + 1
	}
	if s.scroll > len(s.items)-visible {
		s.scroll = len(s.items) - visible
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
}

func (m *Model) clampListScroll(s *collectionState) {
	m.setListWindow(s, m.tableRows())
}

// emptyLines says what is true and offers the next action. It never says "no
// matches" for a page whose coverage was incomplete, because that would be a
// claim the plugin did not make.
func (m *Model) emptyLines(c pluginhost.Collection, s *collectionState, width int) []string {
	if s.err != nil {
		return m.errorLines(s.err, width)
	}
	if !s.loaded && s.loading {
		return []string{styles.Muted.Render(centre("Loading…", width))}
	}
	if c.Search == pluginhost.SearchRequired && strings.TrimSpace(s.query) == "" {
		return []string{
			styles.Title.Render(centre("This collection needs a query.", width)),
			"",
			styles.Muted.Render(centre("Press  /  to ask.", width)),
		}
	}
	switch s.outcome {
	case pluginhost.OutcomeDegraded:
		return []string{
			styles.Title.Render(centre("No matches, and coverage was incomplete.", width)),
			"",
			styles.Muted.Render(centre("Some source could not answer, so this is not a fact about the query.", width)),
			"",
			styles.Muted.Render(centre("/  edit the query      r  retry", width)),
		}
	case pluginhost.OutcomeAbstained:
		return []string{
			styles.Title.Render(centre("No matches.", width)),
			"",
			styles.Muted.Render(centre("Every source answered, so this is a fact about the query.", width)),
			"",
			styles.Muted.Render(centre("/  edit the query      r  refresh", width)),
		}
	default:
		return []string{styles.Muted.Render(centre("Nothing to show.", width))}
	}
}

func (m *Model) errorLines(err *resource.Error, width int) []string {
	lines := []string{toneStyle(errorTone(err.Code)).Render(centre(errorHeadline(err.Code), width)), ""}
	if err.Message != "" {
		for _, line := range wrapToWidth(err.Message, width) {
			lines = append(lines, styles.Body.Render(centre(line, width)))
		}
	}
	if err.SetupHint != "" {
		lines = append(lines, "", styles.Muted.Render(centre("Setup", width)))
		// The hint is text the user may copy. Sidecar never runs it, and saying
		// so is part of the contract rather than a nicety.
		for _, line := range wrapToWidth(err.SetupHint, width) {
			lines = append(lines, styles.Subtle.Render(centre(line, width)))
		}
	}
	lines = append(lines, "", styles.Muted.Render(centre("r  try again", width)))
	return lines
}

// summaryRow is the one line under the table: what is shown, what is left, and
// whether the host is holding anything back.
func (m *Model) summaryRow(c pluginhost.Collection, s *collectionState, width int) string {
	var parts []string
	if len(s.items) > 0 {
		if s.total > len(s.items) {
			parts = append(parts, fmt.Sprintf("%d of %d shown", len(s.items), s.total))
		} else {
			parts = append(parts, fmt.Sprintf("%d shown", len(s.items)))
		}
		if s.nextCursor != "" {
			parts = append(parts, "the rest load on scroll")
		}
		if s.truncated {
			parts = append(parts, "the plugin sent more than Sidecar keeps")
		}
	}
	left := styles.Muted.Render(strings.Join(parts, " · "))
	if m.flash != "" {
		right := flashStyle(m.flashErr).Render(m.flash)
		if ansi.StringWidth(left)+2+ansi.StringWidth(right) <= width {
			return padBetween(left, right, width)
		}
		return fitStyled(right, width)
	}
	return fitStyled(left, width)
}

func flashStyle(isErr bool) lipgloss.Style {
	if isErr {
		return styles.Body.Foreground(styles.Error)
	}
	return styles.Body.Foreground(styles.Success)
}

func (m *Model) noticeLine(notice pluginhost.Notice, width int) string {
	glyph := noticeGlyph(notice.Tone)
	return fitStyled(toneStyle(notice.Tone).Render(ansi.Truncate(glyph+"  "+notice.Text, width, "…")), width)
}

func noticeGlyph(tone resource.Tone) string {
	switch tone {
	case resource.ToneDanger:
		return "✖"
	case resource.ToneWarning:
		return "⚠"
	case resource.ToneSuccess:
		return "✔"
	default:
		return "•"
	}
}

// unreadyLines is what the tab shows before describe has landed, and what it
// shows when describe failed. A plugin that is installed but unconfigured gets
// a setup card built from its own typed reason; there is no spinner for
// something that is not loading.
func (m *Model) unreadyLines(width int) []string {
	title := styles.Title.Render(ansi.Truncate(m.Name(), width, "…"))
	if m.status.LastError != nil {
		lines := []string{title, ""}
		return append(lines, m.errorLines(m.status.LastError, width)...)
	}
	switch m.status.State {
	case pluginhost.StateDisabled:
		return []string{title, "", styles.Muted.Render(centre("This plugin is turned off.", width))}
	case pluginhost.StateIncompatible:
		return []string{title, "", styles.Body.Foreground(styles.Error).Render(
			centre("This plugin does not speak the Sidecar plugin protocol.", width))}
	default:
		return []string{title, "", styles.Muted.Render(centre("Asking "+m.instance+" what it offers…", width))}
	}
}

func (m *Model) noCollectionsLines(width int) []string {
	return []string{
		styles.Title.Render(ansi.Truncate(m.Name(), width, "…")),
		"",
		styles.Muted.Render(centre("This plugin declares no collections to browse.", width)),
		"",
		styles.Subtle.Render(centre("It can still recognise locators in terminal output.", width)),
	}
}

// column is one laid-out table column.
type column struct {
	pluginhost.Column
	width int
}

// layoutColumns turns the declared columns into widths that fit the box.
//
// A declared width is a hint, not a claim on the box: the flexible columns give
// way first, in the order the protocol says they matter least — the secondary
// text before the primary name — and a column that cannot keep three cells is
// dropped rather than drawn as an ellipsis.
func layoutColumns(c pluginhost.Collection, s *collectionState, width int, narrow bool) []column {
	statusW := statusWidth(s)
	budget := width - cursorGutter - statusW
	if statusW > 0 {
		budget -= columnGap
	}
	if budget < 1 {
		budget = 1
	}

	cols := make([]column, 0, len(c.Columns))
	for _, decl := range c.Columns {
		if narrow && decl.Secondary {
			continue
		}
		w := decl.Width
		if w <= 0 {
			w = naturalWidth(decl, s)
		}
		if w > budget {
			w = budget
		}
		cols = append(cols, column{Column: decl, width: w})
	}
	if len(cols) == 0 {
		return nil
	}

	// Shrink until it fits: secondary first, then primary, then the rest from
	// the right, and finally drop trailing columns.
	for total(cols) > budget {
		if !shrink(cols, budget) {
			break
		}
	}
	for len(cols) > 1 && total(cols) > budget {
		cols = cols[:len(cols)-1]
	}
	if len(cols) == 1 && total(cols) > budget {
		cols[0].width = budget
	}

	// Give whatever is left to the flexible column, so the table fills the box
	// rather than leaving a ragged right edge.
	if spare := budget - total(cols); spare > 0 {
		if i := flexIndex(cols); i >= 0 {
			cols[i].width += spare
		}
	}
	if statusW > 0 {
		cols = append(cols, column{
			Column: pluginhost.Column{ID: statusColumnID, Kind: pluginhost.ColumnStatus, Align: pluginhost.AlignLeft},
			width:  statusW,
		})
	}
	return cols
}

// statusColumnID names the reserved, unlabelled column a row's status pill goes
// in. It is the host's column, not a declared one, which is why it can never
// collide with a plugin's: a declared column with this ID would still be
// declared, and this one is only ever appended.
const statusColumnID = "\x00status"

func statusWidth(s *collectionState) int {
	w := 0
	for _, item := range s.items {
		if item.Status == nil {
			continue
		}
		if n := ansi.StringWidth(item.Status.Label); n > w {
			w = n
		}
	}
	if w > statusColumnMax {
		w = statusColumnMax
	}
	return w
}

func naturalWidth(decl pluginhost.Column, s *collectionState) int {
	w := ansi.StringWidth(decl.Label)
	for _, item := range s.items {
		if n := ansi.StringWidth(item.Cells[decl.ID]); n > w {
			w = n
		}
	}
	if decl.Secondary && w > 48 {
		w = 48
	}
	if w > 64 {
		w = 64
	}
	if w < 1 {
		w = 1
	}
	return w
}

func total(cols []column) int {
	sum := 0
	for _, c := range cols {
		sum += c.width
	}
	return sum + columnGap*(len(cols)-1)
}

// shrink takes one cell from the widest column that can spare it, preferring
// the secondary column and then the primary one.
func shrink(cols []column, budget int) bool {
	for _, want := range []func(column) bool{
		func(c column) bool { return c.Secondary },
		func(c column) bool { return c.Primary },
		func(column) bool { return true },
	} {
		best, bestW := -1, 0
		for i, c := range cols {
			if !want(c) || c.width <= 3 {
				continue
			}
			if c.width > bestW {
				best, bestW = i, c.width
			}
		}
		if best >= 0 {
			cols[best].width--
			return true
		}
	}
	return false
}

func flexIndex(cols []column) int {
	for i, c := range cols {
		if c.Secondary {
			return i
		}
	}
	for i, c := range cols {
		if c.Primary {
			return i
		}
	}
	return -1
}

func headerRow(cols []column, width int) string {
	cells := make([]string, 0, len(cols))
	for _, c := range cols {
		label := ""
		if c.ID != statusColumnID {
			label = strings.ToUpper(c.Label)
		}
		cells = append(cells, alignCell(label, c.width, c.Align))
	}
	return fitStyled(strings.Repeat(" ", cursorGutter)+
		styles.Subtle.Render(strings.Join(cells, strings.Repeat(" ", columnGap))), width)
}

func headerRule(cols []column, width int) string {
	cells := make([]string, 0, len(cols))
	for _, c := range cols {
		cells = append(cells, strings.Repeat("─", c.width))
	}
	return fitStyled(strings.Repeat(" ", cursorGutter)+
		styles.Muted.Render(strings.Join(cells, strings.Repeat(" ", columnGap))), width)
}

// itemRows renders one row: one line at a table width, two when the box is too
// narrow for a table. The reflow rule is the protocol's, stated fully: the
// primary cell takes line one, and the remaining short columns and the
// secondary cell fold into a dimmed line two.
func (m *Model) itemRows(c pluginhost.Collection, cols []column, s *collectionState, index, width int, narrow bool) []string {
	item := s.items[index]
	selected := index == s.cursor
	gutter := strings.Repeat(" ", cursorGutter)
	if selected {
		gutter = styles.ListCursor.Render("❯") + " "
	}

	if !narrow {
		cells := make([]string, 0, len(cols))
		for _, col := range cols {
			cells = append(cells, m.cell(item, col))
		}
		row := gutter + strings.Join(cells, strings.Repeat(" ", columnGap))
		return []string{m.paint(row, width, selected)}
	}

	primary := ""
	if col, ok := c.PrimaryColumn(); ok {
		primary = item.Cells[col.ID]
	}
	lead := ""
	for _, col := range cols {
		if col.Primary || col.ID == statusColumnID || col.Secondary {
			continue
		}
		if v := strings.TrimSpace(item.Cells[col.ID]); v != "" {
			lead += v + " · "
		}
	}
	if item.Status != nil && item.Status.Label != "" {
		lead += item.Status.Label + " · "
	}
	secondary := ""
	for _, col := range c.Columns {
		if col.Secondary {
			secondary = item.Cells[col.ID]
		}
	}
	first := gutter + styles.Body.Render(ansi.Truncate(primary, max0(width-cursorGutter), "…"))
	second := strings.Repeat(" ", cursorGutter+2) +
		styles.Subtle.Render(ansi.Truncate(lead+secondary, max0(width-cursorGutter-2), "…"))
	return []string{m.paint(first, width, selected), m.paint(second, width, selected)}
}

// cell renders one table cell in the column's own style.
func (m *Model) cell(item pluginhost.Item, col column) string {
	if col.ID == statusColumnID {
		if item.Status == nil {
			return strings.Repeat(" ", col.width)
		}
		return alignStyled(toneStyle(item.Status.Tone), item.Status.Label, col.width, col.Align)
	}
	value := item.Cells[col.ID]
	return alignStyled(cellStyle(col.Column), value, col.width, col.Align)
}

func cellStyle(col pluginhost.Column) lipgloss.Style {
	switch {
	case col.Primary:
		return styles.Body
	case col.Secondary:
		return styles.Subtle
	case col.Kind == pluginhost.ColumnTimestamp, col.Kind == pluginhost.ColumnNumber:
		return styles.Muted
	default:
		return styles.Muted
	}
}

// paint applies the selected-row background. A row's spans each close
// themselves with a reset, so the background is re-asserted rather than wrapped.
func (m *Model) paint(row string, width int, selected bool) string {
	if !selected {
		return fitStyled(row, width)
	}
	return ui.RowBackground(row, width, styles.BgTertiary)
}

func alignCell(text string, width int, align pluginhost.Align) string {
	text = ansi.Truncate(text, width, "…")
	pad := width - ansi.StringWidth(text)
	if pad < 0 {
		pad = 0
	}
	switch align {
	case pluginhost.AlignRight:
		return strings.Repeat(" ", pad) + text
	case pluginhost.AlignCenter:
		left := pad / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", pad-left)
	default:
		return text + strings.Repeat(" ", pad)
	}
}

func alignStyled(style lipgloss.Style, text string, width int, align pluginhost.Align) string {
	text = ansi.Truncate(text, width, "…")
	pad := width - ansi.StringWidth(text)
	if pad < 0 {
		pad = 0
	}
	switch align {
	case pluginhost.AlignRight:
		return strings.Repeat(" ", pad) + style.Render(text)
	case pluginhost.AlignCenter:
		left := pad / 2
		return strings.Repeat(" ", left) + style.Render(text) + strings.Repeat(" ", pad-left)
	default:
		return style.Render(text) + strings.Repeat(" ", pad)
	}
}

// padBetween puts left and right on one row of exactly width cells.
func padBetween(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return fitStyled(left+" "+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitStyled holds a string that already carries styling to exactly width
// display cells.
//
// It exists because ui.TruncateString measures runes, not cells behind escape
// sequences, so a styled row hands it a length dominated by colour codes and
// comes back cut in the middle of its data. Everything the browser paints goes
// through here, which is what makes "every line is exactly the box's width" a
// property of one function rather than a promise each renderer keeps.
func fitStyled(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	switch {
	case w > width:
		return ansi.Truncate(s, width, "")
	case w < width:
		return s + strings.Repeat(" ", width-w)
	default:
		return s
	}
}

func centre(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	pad := (width - ansi.StringWidth(text)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + text
}

// centreBlock puts a short block in the vertical middle of the space it was
// given, which is where an empty state belongs.
func centreBlock(lines []string, width, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) >= height {
		return lines[:height]
	}
	top := (height - len(lines)) / 2
	out := make([]string, 0, height)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	out = append(out, lines...)
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// fitLines holds a block to exactly height rows of exactly width cells.
func fitLines(lines []string, width, height int) []string {
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, 0, height)
	for _, line := range lines {
		out = append(out, fitStyled(line, width))
	}
	blank := strings.Repeat(" ", max0(width))
	for len(out) < height {
		out = append(out, blank)
	}
	return out
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

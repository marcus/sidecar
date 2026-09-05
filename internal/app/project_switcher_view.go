package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/projectlist"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// The project switcher's drawing. Everything about which destinations exist,
// what they mean, and what order they are in belongs to internal/projectlist
// and to model.go; this file decides only where things land on the screen and
// which regions the pointer can hit.

const (
	projectSwitcherSortButtonID = "project-switcher-sort"
	projectSwitcherViewListID   = "project-switcher-view-list"
	projectSwitcherViewGridID   = "project-switcher-view-grid"
	projectSwitcherSortOptionID = "project-switcher-sort-option-"
	projectSwitcherSortOrderID  = "project-switcher-sort-order"

	// projectSwitcherWidth is the modal's preferred width. It is what holds
	// three grid columns and a list whose path column does not truncate the
	// paths people actually have; a narrower terminal gets what it has room for.
	projectSwitcherWidth = 104
	// projectSwitcherMinWidth is the point below which the layout stops being
	// a collection with columns and becomes a name list.
	projectSwitcherMinWidth = 44

	// projectSwitcherMetaWidth is the metadata column. It holds its heading
	// ("LAST ACTIVITY") and every value formatted into it.
	projectSwitcherMetaWidth = 13
	// projectSwitcherNameWidth is the name column in the compact list. Names
	// longer than this truncate rather than pushing the path column around,
	// because a column that moves per row is not a column.
	projectSwitcherNameWidth = 28
	// projectSwitcherGutter is the marker column plus the padding that keeps
	// the row off the modal's edge. The selection band spans the full content
	// width, so this padding sits inside the highlight rather than beside it.
	projectSwitcherGutter = 2
)

// projectSwitcherModalWidthFor is the modal's width at a given terminal width.
func projectSwitcherModalWidthFor(screenWidth int) int {
	w := screenWidth - 8
	if w > projectSwitcherWidth {
		w = projectSwitcherWidth
	}
	if w < projectSwitcherMinWidth {
		w = projectSwitcherMinWidth
	}
	if w > screenWidth-4 {
		w = screenWidth - 4
	}
	if w < 20 {
		w = 20
	}
	return w
}

// projectSwitcherContentWidth is the drawable width inside the modal's border
// and padding. The modal library reserves the same amount for every section, so
// the collection is laid out against this rather than against the modal width.
// The authority is the width the modal library actually handed the collection
// section on its last render, recorded there. Key handling asks this question
// too — the grid's arrows need the column count — and an estimate that is two
// columns out is the difference between a three-column grid and a two-column
// one. Before the first render there is nothing to record, so the estimate
// stands in.
func (m *Model) projectSwitcherContentWidth() int {
	if m.projectSwitcherContentW > 0 {
		return m.projectSwitcherContentW
	}
	return max(10, projectSwitcherModalWidthFor(m.width)-6)
}

// projectSwitcherEffectiveView is the layout actually drawn. The grid is only
// offered where a card is readable; below that the view control still says what
// the user chose, but the collection is drawn as a list rather than as a column
// of clipped boxes.
func (m *Model) projectSwitcherEffectiveView() projectlist.View {
	if m.projectSwitcherView == projectlist.ViewGrid &&
		projectlist.GridAvailable(m.projectSwitcherCollectionWidth()) {
		return projectlist.ViewGrid
	}
	return projectlist.ViewList
}

// projectSwitcherCollectionWidth is the width the collection gets. The list
// gives up two columns to its scrollbar; the grid does not, because those two
// columns are the difference between three cards to a row and two, and a grid
// of cards is paged by its cards rather than dragged by a bar.
func (m *Model) projectSwitcherCollectionWidth() int {
	if m.projectSwitcherView == projectlist.ViewGrid {
		return max(10, m.projectSwitcherContentWidth())
	}
	return max(10, m.projectSwitcherContentWidth()-2)
}

// projectSwitcherVisibleRows is how many rows of the collection are on screen.
// It follows the terminal rather than a constant so a tall window shows the
// collection it has room for and a short one still leaves the search field,
// the toolbar and the hints visible.
func (m *Model) projectSwitcherVisibleRows() int {
	// chrome: border 2, title 2, search field 3, toolbar 2, headings 2,
	// hints 2, and the margin the overlay leaves around the modal.
	const chrome = 17
	rows := m.height - chrome
	if m.height <= 0 {
		rows = projectSwitcherMaxVisible
	}
	if m.projectSwitcherEffectiveView() == projectlist.ViewGrid {
		columns := projectlist.GridColumns(m.projectSwitcherCollectionWidth())
		if columns > 0 {
			cardRows := max(1, rows/projectlist.CardHeight)
			return cardRows * columns
		}
	}
	if rows < 3 {
		rows = 3
	}
	if rows > 24 {
		rows = 24
	}
	return rows
}

// ensureProjectSwitcherModal builds/rebuilds the project switcher modal.
func (m *Model) ensureProjectSwitcherModal() {
	modalW := projectSwitcherModalWidthFor(m.width)

	// Only rebuild if modal doesn't exist or width changed
	if m.projectSwitcherModal != nil && m.projectSwitcherModalWidth == modalW {
		return
	}
	m.projectSwitcherModalWidth = modalW

	m.projectSwitcherModal = modal.New("Switch Project",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(m.projectSwitcherInputSection()).
		AddSection(m.projectSwitcherToolbarSection()).
		AddSection(m.projectSwitcherListSection()).
		AddSection(m.projectSwitcherHintsSection())
}

// projectSwitcherInputSection renders the filter field. It is the whole width
// of the modal and holds focus by default: typing always edits the filter, so
// no control below it may claim a bare printable key.
func (m *Model) projectSwitcherInputSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		inputBoxWidth := max(1, contentWidth-2)
		m.projectSwitcherInput.SetWidth(max(1, inputBoxWidth-2))
		if m.projectSwitcherFocus == switcherFocusFilter {
			m.projectSwitcherInput.Focus()
		} else {
			m.projectSwitcherInput.Blur()
		}

		borderColor := styles.BorderNormal
		if m.projectSwitcherFocus == switcherFocusFilter {
			borderColor = styles.Primary
		}
		inputBox := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Width(inputBoxWidth).
			Render(m.projectSwitcherInput.View())

		return modal.RenderedSection{Content: inputBox}
	}, nil)
}

// projectSwitcherToolbarSection renders the count, the sort control, the
// list/grid control and the add button on one row, and hangs the sort popover
// off it as an overlay.
//
// The controls are pills with their own hit regions rather than bare words: the
// mouse target and the focus ring are then the same shape, and the row reads as
// controls rather than as a caption.
func (m *Model) projectSwitcherToolbarSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		controls := []modal.Control{
			{
				ID:      projectSwitcherSortButtonID,
				Label:   "Sort: " + m.projectSwitcherSort.Label() + " ▾",
				Active:  m.projectSwitcherSortOpen,
				Focused: m.projectSwitcherFocus == switcherFocusSort,
			},
			{
				ID:      projectSwitcherViewListID,
				Label:   projectlist.ViewList.Label(),
				Active:  m.projectSwitcherView == projectlist.ViewList,
				Focused: m.projectSwitcherFocus == switcherFocusView && m.projectSwitcherView == projectlist.ViewList,
			},
			{
				ID:      projectSwitcherViewGridID,
				Label:   projectlist.ViewGrid.Label(),
				Active:  m.projectSwitcherView == projectlist.ViewGrid,
				Focused: m.projectSwitcherFocus == switcherFocusView && m.projectSwitcherView == projectlist.ViewGrid,
			},
			{
				ID:      projectSwitcherAddButtonID,
				Label:   "+ Add",
				Focused: m.projectSwitcherFocus == switcherFocusAdd,
			},
		}
		row := modal.ControlRow(m.projectSwitcherCountText(), controls, func(anchors []int) *modal.Overlay {
			if !m.projectSwitcherSortOpen || len(anchors) == 0 {
				return nil
			}
			return m.projectSwitcherSortPopover(anchors[0])
		})
		return row.Render(contentWidth, focusID, hoverID)
	}, nil)
}

// projectSwitcherCountText is the left-hand caption: how much of the collection
// the filter is showing, and nothing when there is nothing to count.
func (m *Model) projectSwitcherCountText() string {
	// Overview is pinned above the collection and is not one of the projects,
	// so counting it would make the caption disagree with the rows under it.
	all := countProjectDestinations(m.projectSwitcherDestinations(""))
	shown := countProjectDestinations(m.projectSwitcherFiltered)
	if m.projectSwitcherInput.Value() != "" {
		return fmt.Sprintf("%d of %d projects", shown, all)
	}
	if all > 0 {
		return fmt.Sprintf("%d projects", all)
	}
	return ""
}

func countProjectDestinations(destinations []projectSwitcherDestination) int {
	count := 0
	for _, destination := range destinations {
		if destination.Kind != destinationOverview {
			count++
		}
	}
	return count
}

// projectSwitcherSortPopover is the sort menu, anchored under its control. It
// offers the three modes and the direction they read in, and states the one
// rule a user cannot see from the rows in front of them: a project with no
// recorded date stays at the end either way.
func (m *Model) projectSwitcherSortPopover(anchorX int) *modal.Overlay {
	const width = 30
	lines := make([]string, 0, len(projectlist.SortModes)+4)
	focusables := make([]modal.FocusableInfo, 0, len(projectlist.SortModes)+1)

	lines = append(lines, styles.Muted.Render(" Sort projects"))
	for i, mode := range projectlist.SortModes {
		mark := "  "
		if mode == m.projectSwitcherSort {
			mark = "✓ "
		}
		label := " " + mark + mode.Label()
		style := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		if i == m.projectSwitcherSortIdx {
			label = ui.SelectedRowBackground(lipgloss.NewStyle().Bold(true).Render(label), width)
		} else {
			label = style.Render(fitCell(label, width))
		}
		lines = append(lines, label)
		focusables = append(focusables, modal.FocusableInfo{
			ID:      fmt.Sprintf("%s%d", projectSwitcherSortOptionID, i),
			OffsetX: 0, OffsetY: len(lines) - 1, Width: width, Height: 1,
			MouseOnly: true,
		})
	}
	lines = append(lines, styles.Subtle.Render(strings.Repeat("─", width)))

	order := " Order: " + projectlist.OrderLabel(m.projectSwitcherSort, m.projectSwitcherOrder)
	orderStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
	if m.projectSwitcherSortIdx == len(projectlist.SortModes) {
		lines = append(lines, ui.SelectedRowBackground(lipgloss.NewStyle().Bold(true).Render(order), width))
	} else {
		lines = append(lines, orderStyle.Render(fitCell(order, width)))
	}
	focusables = append(focusables, modal.FocusableInfo{
		ID:      projectSwitcherSortOrderID,
		OffsetX: 0, OffsetY: len(lines) - 1, Width: width, Height: 1,
		MouseOnly: true,
	})
	lines = append(lines, styles.Muted.Render(fitCell(" "+projectlist.UnknownLabel+" dates appear last", width)))

	body := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(styles.Primary).
		Background(styles.BgSecondary).
		Render(strings.Join(lines, "\n"))

	// Keep the popover on the modal even when its control sits near the right
	// edge: a menu that hangs off the surface is a menu with rows nobody can
	// read or click.
	x := anchorX
	if maxX := m.projectSwitcherContentWidth() - (width + 2); x > maxX {
		x = max(0, maxX)
	}
	return &modal.Overlay{Content: body, OffsetX: x, OffsetY: 1, Focusables: focusables}
}

// fit pads or truncates a plain string to exactly width.
func fitCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return ansi.Truncate(s, width, "…")
}

// projectSwitcherListSection renders the collection in the chosen view. Both
// views draw the same ordered items, register a hit region per destination, and
// answer to the same activation, so a project opens the same way whichever one
// is on screen.
func (m *Model) projectSwitcherListSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		m.projectSwitcherContentW = contentWidth
		if section, done := m.projectSwitcherEmptySection(contentWidth); done {
			return section
		}
		if m.projectSwitcherEffectiveView() == projectlist.ViewGrid {
			return m.renderProjectSwitcherGrid(contentWidth, hoverID)
		}
		return m.renderProjectSwitcherList(contentWidth, hoverID)
	}, m.projectSwitcherListUpdate)
}

// projectSwitcherEmptySection answers the two empty states: nothing configured
// at all, and nothing matching the filter. The second keeps the filter visible
// above it and names a way back, because losing the query is not a recovery.
func (m *Model) projectSwitcherEmptySection(contentWidth int) (modal.RenderedSection, bool) {
	if countProjectDestinations(m.projectSwitcherFiltered) > 0 {
		return modal.RenderedSection{}, false
	}
	if len(m.cfg.Projects.List) == 0 && !m.globalScopeAvailable() {
		var b strings.Builder
		b.WriteString(styles.Muted.Render("No projects configured"))
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render("Sidecar Setup walks through adding one."))
		return modal.RenderedSection{Content: b.String()}, true
	}
	return m.projectSwitcherNoMatchesSection(contentWidth), true
}

// projectSwitcherNoMatchesSection is the no-results state. The pinned rows stay
// drawn above it — Overview is the way out of a search that found nothing — and
// the message names the query and the key that clears it, so recovering does
// not mean guessing.
func (m *Model) projectSwitcherNoMatchesSection(contentWidth int) modal.RenderedSection {
	rowWidth := max(10, contentWidth-2)
	lines := make([]string, 0, 6)
	focusables := make([]modal.FocusableInfo, 0, 1)

	pinned := m.projectSwitcherPinnedCount()
	nameW, pathW, metaW := projectSwitcherColumns(rowWidth)
	for i := 0; i < pinned; i++ {
		itemID := projectSwitcherItemID(i)
		lines = append(lines, m.projectSwitcherRowLine(m.projectSwitcherRows[i], rowWidth, nameW, pathW, metaW,
			i == m.projectSwitcherCursor, false))
		focusables = append(focusables, modal.FocusableInfo{
			ID: itemID, OffsetX: 0, OffsetY: len(lines) - 1, Width: contentWidth, Height: 1,
		})
	}
	if pinned > 0 {
		lines = append(lines, styles.Subtle.Render(strings.Repeat("─", rowWidth)))
	}

	query := strings.TrimSpace(m.projectSwitcherInput.Value())
	lines = append(lines,
		"",
		styles.Body.Render(fmt.Sprintf("  No projects match %q", query)),
		"",
		styles.Muted.Render("  esc clears the filter and keeps the switcher open."),
	)
	return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
}

// renderProjectSwitcherList draws the compact list: one row per destination,
// with a marker gutter, a name column, a path column and a metadata column
// under a heading that names what it holds.
//
// Density is the point of this view — more projects on screen is why it is the
// default — so the breathing room comes from horizontal padding and column
// alignment inside a full-width selection band, not from spending a second row
// on every project.
func (m *Model) renderProjectSwitcherList(contentWidth int, hoverID string) modal.RenderedSection {
	items := m.projectSwitcherRows
	rowWidth := max(10, contentWidth-2) // reserve the scrollbar's column
	pinned := m.projectSwitcherPinnedCount()
	collection := items[pinned:]
	maxVisible := m.projectSwitcherVisibleRows()
	visibleCount := min(len(collection), maxVisible)
	scrollOffset := min(m.projectSwitcherScroll, max(0, len(collection)-visibleCount))

	metaHeading, _ := projectlist.MetaColumn(m.projectSwitcherSort)
	nameW, pathW, metaW := projectSwitcherColumns(rowWidth)

	lines := make([]string, 0, visibleCount+pinned+2)
	focusables := make([]modal.FocusableInfo, 0, visibleCount+pinned)

	addRow := func(entryIdx int) {
		itemID := projectSwitcherItemID(entryIdx)
		lines = append(lines, m.projectSwitcherRowLine(items[entryIdx], rowWidth, nameW, pathW, metaW,
			entryIdx == m.projectSwitcherCursor, itemID == hoverID))
		focusables = append(focusables, modal.FocusableInfo{
			ID: itemID, OffsetX: 0, OffsetY: len(lines) - 1, Width: contentWidth, Height: 1,
		})
	}

	// Overview is a different kind of destination, not the first project. It
	// is drawn above the collection and outside its scroll window, with a rule
	// between them, so scrolling the projects never scrolls it away and the
	// modal's height does not change as the cursor moves.
	for i := 0; i < pinned; i++ {
		addRow(i)
	}
	if pinned > 0 {
		lines = append(lines, styles.Subtle.Render(strings.Repeat("─", rowWidth)))
	}

	// Column headings. They earn their row: without them the third column is a
	// bare relative time whose meaning changes with the sort.
	if pathW > 0 {
		heading := strings.Repeat(" ", projectSwitcherGutter) +
			fitCell("PROJECT", nameW) + "  " + fitCell("PATH", pathW) + "  " + fitCell(metaHeading, metaW)
		lines = append(lines, styles.Subtle.Render(fitCell(heading, rowWidth)))
	} else {
		lines = append(lines, styles.Subtle.Render(fitCell(strings.Repeat(" ", projectSwitcherGutter)+"PROJECT", rowWidth)))
	}

	for i := 0; i < visibleCount; i++ {
		addRow(pinned + scrollOffset + i)
	}

	barParams := ui.ScrollbarParams{
		TotalItems:   len(collection),
		ScrollOffset: scrollOffset,
		VisibleItems: visibleCount,
		TrackHeight:  len(lines),
	}
	scrollbar, _ := ui.RenderScrollbarWithState(barParams, m.projectSwitcherBar.style(m.projectSwitcherMouseHandler))
	body := lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(lines, "\n")+" ", scrollbar)

	return modal.RenderedSection{
		Content:    body,
		Focusables: focusables,
		Scrollbar: &modal.SectionScrollbar{
			TotalItems:   barParams.TotalItems,
			ScrollOffset: barParams.ScrollOffset,
			VisibleItems: barParams.VisibleItems,
			TrackHeight:  barParams.TrackHeight,
			LocalX:       rowWidth + 1,
		},
	}
}

// projectSwitcherColumns divides a row into name, path and metadata columns.
// The metadata column is dropped before the path column, and the path column
// before the name, because a row that cannot say which project it is has
// stopped being a row.
func projectSwitcherColumns(rowWidth int) (nameW, pathW, metaW int) {
	const gaps = 4 // two two-space column gaps
	available := rowWidth - projectSwitcherGutter
	if available <= 0 {
		return max(1, rowWidth), 0, 0
	}
	nameW = min(projectSwitcherNameWidth, available)
	metaW = projectSwitcherMetaWidth
	pathW = available - nameW - metaW - gaps
	if pathW < 12 {
		// Not enough for a useful path: keep the metadata, drop the path.
		pathW = 0
		if available-nameW-metaW-2 < 0 {
			metaW = 0
			nameW = available
		}
	}
	return nameW, pathW, metaW
}

// projectSwitcherRowLine draws one compact row.
func (m *Model) projectSwitcherRowLine(item projectlist.Item, rowWidth, nameW, pathW, metaW int, cursor, hovered bool) string {
	marker := " "
	if cursor {
		marker = "›"
	}
	markerStyle := lipgloss.NewStyle().Foreground(styles.Primary)

	nameStyle := lipgloss.NewStyle().Foreground(styles.Secondary)
	switch {
	case item.Disabled():
		nameStyle = styles.Muted
	case item.Kind == projectlist.KindOverview:
		nameStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case item.Current:
		nameStyle = lipgloss.NewStyle().Foreground(styles.Success).Bold(true)
	case cursor || hovered:
		nameStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	}

	name := item.Name
	badge := ""
	if item.Current {
		// The bound project and the highlighted row are separate facts, so the
		// current project says so in a word rather than borrowing the cursor's
		// look.
		badge = " current"
	}
	nameCell := fitCell(name, max(1, nameW-ansi.StringWidth(badge)))
	cell := nameStyle.Render(nameCell)
	if badge != "" {
		cell += lipgloss.NewStyle().Foreground(styles.Success).Render(badge)
	}

	var b strings.Builder
	b.WriteString(markerStyle.Render(marker))
	b.WriteString(strings.Repeat(" ", max(0, projectSwitcherGutter-1)))
	b.WriteString(cell)

	if pathW > 0 {
		b.WriteString("  ")
		b.WriteString(styles.Subtle.Render(fitCell(m.projectSwitcherDetail(item), pathW)))
	}
	if metaW > 0 {
		b.WriteString("  ")
		meta := projectlist.MetaValue(item, m.projectSwitcherSort, m.projectSwitcherNow())
		if item.Kind == projectlist.KindOverview {
			meta = ""
		}
		b.WriteString(styles.Muted.Render(fitCell(meta, metaW)))
	}

	line := b.String()
	switch {
	case cursor:
		return ui.SelectedRowBackground(line, rowWidth)
	case hovered:
		// Hover is transient and must not read as selection, so it is the
		// raised surface rather than the selection colour.
		return ui.RowBackground(line, rowWidth, styles.SurfaceRaised)
	}
	return fitCell(line, rowWidth)
}

// projectSwitcherDetail is the second column: what the destination is, in the
// terms it is true in. A refused destination spends the column on its reason,
// which is the only place the reason can be read.
func (m *Model) projectSwitcherDetail(item projectlist.Item) string {
	switch {
	case item.Kind == projectlist.KindOverview:
		return "All projects and hosts"
	case item.Disabled():
		return item.DisabledReason
	}
	return shortenHomePath(item.Path)
}

// renderProjectSwitcherGrid draws the collection as cards. The whole card is
// the hit region, which is the reason the view exists.
func (m *Model) renderProjectSwitcherGrid(contentWidth int, hoverID string) modal.RenderedSection {
	items := m.projectSwitcherRows
	columns := projectlist.GridColumns(contentWidth)
	if columns <= 0 {
		return m.renderProjectSwitcherList(contentWidth, hoverID)
	}

	perPage := m.projectSwitcherVisibleRows()
	scrollOffset := min(m.projectSwitcherScroll, max(0, len(items)-perPage))
	end := min(len(items), scrollOffset+perPage)

	lines := make([]string, 0, perPage/columns*projectlist.CardHeight)
	focusables := make([]modal.FocusableInfo, 0, perPage)

	for start := scrollOffset; start < end; start += columns {
		rowEnd := min(end, start+columns)
		cards := make([]string, 0, columns)
		for idx := start; idx < rowEnd; idx++ {
			itemID := projectSwitcherItemID(idx)
			cards = append(cards, m.projectSwitcherCard(items[idx],
				idx == m.projectSwitcherCursor, itemID == hoverID))
			focusables = append(focusables, modal.FocusableInfo{
				ID:      itemID,
				OffsetX: (idx - start) * (projectlist.CardWidth + projectlist.CardGap),
				OffsetY: len(lines),
				Width:   projectlist.CardWidth,
				Height:  projectlist.CardHeight,
			})
		}
		gap := strings.Repeat(" ", projectlist.CardGap)
		joined := make([]string, 0, len(cards)*2)
		for i, card := range cards {
			if i > 0 {
				joined = append(joined, gap)
			}
			joined = append(joined, card)
		}
		block := lipgloss.JoinHorizontal(lipgloss.Top, joined...)
		lines = append(lines, strings.Split(block, "\n")...)
	}

	return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
}

// projectSwitcherCard draws one grid card: name and status, path, and the
// metadata line the current sort names.
//
// Every body line is padded to exactly the card's inner width here rather than
// left to a style's Width, so the card is the size the grid's geometry says it
// is. A card that renders one column wider than the layout believes wraps its
// own content and pushes the next column out of the row.
func (m *Model) projectSwitcherCard(item projectlist.Item, cursor, hovered bool) string {
	inner := projectlist.CardWidth - 4 // two border columns and one of padding each side

	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.TextPrimary)
	switch {
	case item.Disabled():
		nameStyle = styles.Muted
	case item.Kind == projectlist.KindOverview:
		nameStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case item.Current:
		nameStyle = lipgloss.NewStyle().Foreground(styles.Success).Bold(true)
	case cursor:
		nameStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	}

	badge, badgeWidth := "", 0
	if item.Current {
		// The bound project and the highlighted card are separate facts, so
		// the current project says so in a word of its own.
		badgeWidth = len("current") + 1
		badge = " " + lipgloss.NewStyle().Foreground(styles.Success).Render("current")
	}
	nameLine := nameStyle.Render(fitCell(item.Name, max(1, inner-badgeWidth))) + badge

	meta := projectlist.MetaValue(item, m.projectSwitcherSort, m.projectSwitcherNow())
	if item.Kind == projectlist.KindOverview {
		meta = ""
	}
	action, actionWidth := "", 0
	if cursor && !item.Disabled() {
		actionWidth = len("› Open") + 1
		action = " " + lipgloss.NewStyle().Foreground(styles.Primary).Render("› Open")
	}
	metaLine := styles.Muted.Render(fitCell(meta, max(1, inner-actionWidth))) + action

	rows := []string{
		nameLine,
		styles.Subtle.Render(fitCell(m.projectSwitcherDetail(item), inner)),
		strings.Repeat(" ", inner),
		metaLine,
	}
	for i, row := range rows {
		rows[i] = " " + row + " "
	}

	borderColor := styles.BorderNormal
	switch {
	case cursor:
		borderColor = styles.Primary
	case hovered:
		borderColor = styles.Secondary
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(strings.Join(rows, "\n"))
}

// projectSwitcherListUpdate handles key events routed to the collection.
// Scrollbar gestures on the declared bar are answered by projectSwitcherBarEvent
// in the switcher's mouse handler — see modal_scrollbar.go for why they cannot
// route through here.
func (m *Model) projectSwitcherListUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return "", nil
	}
	if len(m.projectSwitcherFiltered) == 0 {
		return "", nil
	}

	switch keyMsg.String() {
	case "up", "k", "ctrl+p":
		return "", m.moveProjectSwitcherCursor(0, -1)
	case "down", "j", "ctrl+n":
		return "", m.moveProjectSwitcherCursor(0, 1)
	case "left":
		return "", m.moveProjectSwitcherCursor(-1, 0)
	case "right":
		return "", m.moveProjectSwitcherCursor(1, 0)
	case "enter":
		if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(m.projectSwitcherFiltered) {
			return "select", nil
		}
	}
	return "", nil
}

// projectSwitcherHintsSection renders the keyboard hints.
func (m *Model) projectSwitcherHintsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var b strings.Builder
		b.WriteString("\n")

		if len(m.cfg.Projects.List) == 0 && !m.globalScopeAvailable() {
			b.WriteString(styles.KeyHint.Render("enter"))
			b.WriteString(styles.Muted.Render(" Sidecar Setup  "))
			b.WriteString(styles.KeyHint.Render("ctrl+a"))
			b.WriteString(styles.Muted.Render(" add  "))
			b.WriteString(styles.KeyHint.Render("y"))
			b.WriteString(styles.Muted.Render(" copy prompt  "))
			b.WriteString(styles.KeyHint.Render("esc"))
			b.WriteString(styles.Muted.Render(" close"))
			return modal.RenderedSection{Content: b.String()}
		}

		if m.projectSwitcherSortOpen {
			b.WriteString(styles.KeyHint.Render("↑/↓"))
			b.WriteString(styles.Muted.Render(" choose  "))
			b.WriteString(styles.KeyHint.Render("enter"))
			b.WriteString(styles.Muted.Render(" apply  "))
			b.WriteString(styles.KeyHint.Render("esc"))
			b.WriteString(styles.Muted.Render(" close sort"))
			return modal.RenderedSection{Content: b.String()}
		}

		if len(m.projectSwitcherFiltered) == 0 {
			b.WriteString(styles.KeyHint.Render("esc"))
			b.WriteString(styles.Muted.Render(" clear filter  "))
			b.WriteString(styles.KeyHint.Render("@"))
			b.WriteString(styles.Muted.Render(" close"))
			return modal.RenderedSection{Content: b.String()}
		}

		arrows := "↑/↓"
		if m.projectSwitcherEffectiveView() == projectlist.ViewGrid {
			arrows = "↑↓←→"
		}
		b.WriteString(styles.KeyHint.Render("enter"))
		b.WriteString(styles.Muted.Render(" switch  "))
		b.WriteString(styles.KeyHint.Render(arrows))
		b.WriteString(styles.Muted.Render(" navigate  "))
		b.WriteString(styles.KeyHint.Render("tab"))
		b.WriteString(styles.Muted.Render(" controls  "))
		b.WriteString(styles.KeyHint.Render("ctrl+a"))
		b.WriteString(styles.Muted.Render(" add  "))
		b.WriteString(styles.KeyHint.Render("esc"))
		b.WriteString(styles.Muted.Render(" close"))
		return modal.RenderedSection{Content: b.String()}
	}, nil)
}

// renderProjectSwitcherOverlay renders the project switcher modal.
func (m *Model) renderProjectSwitcherOverlay(content string) string {
	m.ensureProjectSwitcherModal()
	if m.projectSwitcherModal == nil {
		return content
	}

	if m.projectSwitcherMouseHandler == nil {
		m.projectSwitcherMouseHandler = mouse.NewHandler()
	}
	modalContent := m.projectSwitcherModal.Render(m.width, m.height, m.projectSwitcherMouseHandler)
	base := ui.OverlayModal(content, modalContent, m.width, m.height)

	if m.projectAddMode {
		return m.renderProjectAddModal(base)
	}
	return base
}

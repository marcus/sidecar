package worktree

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// renderKanbanView renders the kanban board view.
func (p *Plugin) renderKanbanView(width, height int) string {
	// Check minimum width - auto-collapse to list view if too narrow
	minKanbanWidth := 80
	if width < minKanbanWidth {
		// Fall back to list view when too narrow
		return p.renderListView(width, height)
	}

	// Use styled separator characters for theme consistency
	borderStyle := lipgloss.NewStyle().Foreground(styles.BorderNormal)
	horizSep := borderStyle.Render("─")
	vertSep := borderStyle.Render("│")

	var lines []string

	// Header with view mode toggle (account for panel border width)
	innerWidth := width - 4 // Account for panel borders
	header := styles.Title.Render("Worktrees")
	listTab := "List"
	kanbanTab := "[Kanban]"
	viewToggle := styles.Muted.Render(listTab + "|" + kanbanTab)
	headerLine := header + strings.Repeat(" ", max(1, innerWidth-len("Worktrees")-len(listTab)-len(kanbanTab)-1)) + viewToggle
	lines = append(lines, headerLine)
	lines = append(lines, strings.Repeat(horizSep, innerWidth))

	// Register view toggle hit regions (inside panel border at Y=1)
	// Position: right-aligned in header line
	toggleTotalWidth := len(listTab) + 1 + len(kanbanTab) // "List|[Kanban]"
	toggleX := width - 2 - toggleTotalWidth               // -2 for panel border
	p.mouseHandler.HitMap.AddRect(regionViewToggle, toggleX, 1, len(listTab), 1, 0)
	p.mouseHandler.HitMap.AddRect(regionViewToggle, toggleX+len(listTab)+1, 1, len(kanbanTab), 1, 1)

	// Group worktrees by status
	columns := p.getKanbanColumns()

	// Column headers and colors
	columnTitles := map[WorktreeStatus]string{
		StatusActive:  "● Active",
		StatusWaiting: "⧗ Waiting",
		StatusDone:    "✓ Ready",
		StatusPaused:  "⏸ Paused",
	}
	columnColors := map[WorktreeStatus]lipgloss.Color{
		StatusActive:  styles.StatusCompleted.GetForeground().(lipgloss.Color), // Green
		StatusWaiting: styles.StatusModified.GetForeground().(lipgloss.Color),  // Yellow
		StatusDone:    lipgloss.Color("81"),                                    // Cyan
		StatusPaused:  lipgloss.Color("245"),                                   // Gray
	}

	// Calculate column widths (account for panel borders)
	numCols := len(kanbanColumnOrder)
	colWidth := (innerWidth - numCols - 1) / numCols // -1 for separators
	if colWidth < 18 {
		colWidth = 18
	}

	// Render column headers with colors and register hit regions
	var colHeaders []string
	colX := 2 // Start after panel border
	for colIdx, status := range kanbanColumnOrder {
		items := columns[status]
		title := fmt.Sprintf("%s (%d)", columnTitles[status], len(items))
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(columnColors[status]).Width(colWidth)
		// Highlight selected column header
		if colIdx == p.kanbanCol {
			headerStyle = headerStyle.Underline(true)
		}
		colHeaders = append(colHeaders, headerStyle.Render(title))

		// Register column header hit region (Y=3, after header line, separator line)
		p.mouseHandler.HitMap.AddRect(regionKanbanColumn, colX, 3, colWidth, 1, colIdx)
		colX += colWidth + 1 // +1 for separator
	}
	lines = append(lines, strings.Join(colHeaders, vertSep))
	lines = append(lines, strings.Repeat(horizSep, innerWidth))

	// Card dimensions: 4 lines per card (name, agent, task, stats)
	cardHeight := 4
	// Calculate content height (account for panel border + header + separators)
	contentHeight := height - 6 // panel borders (2) + header + 2 separators + column headers
	if contentHeight < cardHeight {
		contentHeight = cardHeight
	}
	maxCards := contentHeight / cardHeight

	// Find the maximum number of cards in any column (for row rendering)
	maxInColumn := 0
	for _, status := range kanbanColumnOrder {
		if len(columns[status]) > maxInColumn {
			maxInColumn = len(columns[status])
		}
	}
	if maxInColumn > maxCards {
		maxInColumn = maxCards
	}

	// Render cards row by row and register card hit regions
	// Cards start at Y=5 (panel border(1) + header(1) + sep(1) + col headers(1) + sep(1))
	cardStartY := 5
	for cardIdx := 0; cardIdx < maxInColumn; cardIdx++ {
		// Register hit regions for this row of cards (once per card, not per line)
		cardColX := 2 // Start after panel border
		for colIdx, status := range kanbanColumnOrder {
			items := columns[status]
			if cardIdx < len(items) {
				cardY := cardStartY + (cardIdx * cardHeight)
				p.mouseHandler.HitMap.AddRect(regionKanbanCard, cardColX, cardY, colWidth-1, cardHeight, kanbanCardData{col: colIdx, row: cardIdx})
			}
			cardColX += colWidth + 1 // +1 for separator
		}

		// Each card has 4 lines
		for lineIdx := 0; lineIdx < cardHeight; lineIdx++ {
			var rowCells []string
			for colIdx, status := range kanbanColumnOrder {
				items := columns[status]
				var cellContent string

				if cardIdx < len(items) {
					wt := items[cardIdx]
					isSelected := colIdx == p.kanbanCol && cardIdx == p.kanbanRow
					cellContent = p.renderKanbanCardLine(wt, lineIdx, colWidth-1, isSelected)
				} else {
					cellContent = strings.Repeat(" ", colWidth-1)
				}

				rowCells = append(rowCells, cellContent)
			}
			lines = append(lines, strings.Join(rowCells, vertSep))
		}
	}

	// Fill remaining height with empty space
	renderedRows := maxInColumn * cardHeight
	for i := renderedRows; i < contentHeight; i++ {
		var emptyCells []string
		for range kanbanColumnOrder {
			emptyCells = append(emptyCells, strings.Repeat(" ", colWidth-1))
		}
		lines = append(lines, strings.Join(emptyCells, vertSep))
	}

	// Build content for panel
	content := strings.Join(lines, "\n")

	// Wrap in panel with gradient border (active since kanban is full-screen)
	return styles.RenderPanel(content, width, height, true)
}

// renderKanbanCardLine renders a single line of a kanban card.
// lineIdx: 0=name, 1=agent, 2=task, 3=stats
func (p *Plugin) renderKanbanCardLine(wt *Worktree, lineIdx, width int, isSelected bool) string {
	var content string

	switch lineIdx {
	case 0:
		// Line 0: Status icon + name (rune-safe for Unicode)
		name := wt.Name
		maxNameLen := width - 3 // Account for icon and space
		if runes := []rune(name); len(runes) > maxNameLen {
			name = string(runes[:maxNameLen-3]) + "..."
		}
		content = fmt.Sprintf(" %s %s", wt.Status.Icon(), name)
	case 1:
		// Line 1: Agent type
		agentStr := ""
		if wt.Agent != nil {
			agentStr = "  " + string(wt.Agent.Type)
		} else if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
			agentStr = "  " + string(wt.ChosenAgentType)
		}
		content = agentStr
	case 2:
		// Line 2: Task ID (rune-safe for Unicode)
		if wt.TaskID != "" {
			taskStr := wt.TaskID
			maxLen := width - 2
			if runes := []rune(taskStr); len(runes) > maxLen {
				taskStr = string(runes[:maxLen-3]) + "..."
			}
			content = "  " + taskStr
		}
	case 3:
		// Line 3: Stats (+/- lines)
		if wt.Stats != nil && (wt.Stats.Additions > 0 || wt.Stats.Deletions > 0) {
			content = fmt.Sprintf("  +%d -%d", wt.Stats.Additions, wt.Stats.Deletions)
		}
	}

	// Pad to width
	contentWidth := lipgloss.Width(content)
	if contentWidth < width {
		content += strings.Repeat(" ", width-contentWidth)
	}

	// Apply styling
	if isSelected {
		return styles.ListItemSelected.Width(width).Render(content)
	}

	// Dim non-name lines
	if lineIdx > 0 {
		return styles.Muted.Width(width).Render(content)
	}

	return lipgloss.NewStyle().Width(width).Render(content)
}

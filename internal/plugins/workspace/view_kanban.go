package workspace

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// renderKanbanView renders the kanban board view.
func (p *Plugin) renderKanbanView(width, height int) string {
	numCols := kanbanColumnCount()
	minColWidth := 16
	minKanbanWidth := (minColWidth * numCols) + (numCols - 1) + 4
	// Check minimum width - auto-collapse to list view if too narrow
	if width < minKanbanWidth {
		return p.renderListView(width, height)
	}

	// Use styled separator characters for theme consistency
	borderStyle := lipgloss.NewStyle().Foreground(styles.BorderNormal)
	horizSep := borderStyle.Render("─")
	vertSep := borderStyle.Render("│")

	var lines []string

	// Header with view mode toggle (account for panel border width)
	innerWidth := width - 4 // Account for panel borders
	header := styles.Title.Render("Workspaces")
	listTab := "List"
	kanbanTab := "[Kanban]"
	viewToggle := styles.Muted.Render(listTab + "|" + kanbanTab)
	headerLine := header + strings.Repeat(" ", max(1, innerWidth-len("Workspaces")-len(listTab)-len(kanbanTab)-1)) + viewToggle
	lines = append(lines, headerLine)
	lines = append(lines, strings.Repeat(horizSep, innerWidth))

	// Register view toggle hit regions (inside panel border at Y=1)
	toggleTotalWidth := len(listTab) + 1 + len(kanbanTab) // "List|[Kanban]"
	toggleX := width - 2 - toggleTotalWidth               // -2 for panel border
	p.mouseHandler.HitMap.AddRect(regionViewToggle, toggleX, 1, len(listTab), 1, 0)
	p.mouseHandler.HitMap.AddRect(regionViewToggle, toggleX+len(listTab)+1, 1, len(kanbanTab), 1, 1)

	// Build unified kanban data (worktrees + shells in status columns)
	kd := p.buildKanbanData()

	// Column headers and colors
	columnTitles := map[WorktreeStatus]string{
		StatusActive:   "● Active",
		StatusThinking: "◐ Thinking",
		StatusWaiting:  "⧗ Waiting",
		StatusDone:     "✓ Ready",
		StatusPaused:   "⏸ Paused",
	}
	columnColors := map[WorktreeStatus]lipgloss.Color{
		StatusActive:   styles.StatusCompleted.GetForeground().(lipgloss.Color), // Green
		StatusThinking: styles.Primary,                                          // Purple
		StatusWaiting:  styles.StatusModified.GetForeground().(lipgloss.Color),  // Yellow
		StatusDone:     styles.Secondary,                                        // Cyan/Blue
		StatusPaused:   styles.TextMuted,                                        // Gray
	}

	// Calculate column widths (account for panel borders)
	colWidth := (innerWidth - numCols - 1) / numCols // -1 for separators
	if colWidth < minColWidth {
		colWidth = minColWidth
	}

	// Render column headers with colors and register hit regions
	var colHeaders []string
	colX := 2 // Start after panel border
	for colIdx := 0; colIdx < numCols; colIdx++ {
		var title string
		var headerStyle lipgloss.Style
		if colIdx == kanbanShellColumnIndex {
			title = fmt.Sprintf("Shells (%d)", len(kd.plainShells))
			headerStyle = lipgloss.NewStyle().Bold(true).Foreground(styles.Muted.GetForeground().(lipgloss.Color)).Width(colWidth)
		} else {
			status := kanbanColumnOrder[colIdx-1]
			count := kd.columnItemCount(colIdx)
			title = fmt.Sprintf("%s (%d)", columnTitles[status], count)
			headerStyle = lipgloss.NewStyle().Bold(true).Foreground(columnColors[status]).Width(colWidth)
		}
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

	// Card dimensions: 4 lines per card (name, agent/status, task, stats)
	cardHeight := 4
	contentHeight := height - 6 // panel borders (2) + header + 2 separators + column headers
	if contentHeight < cardHeight {
		contentHeight = cardHeight
	}
	maxCards := contentHeight / cardHeight

	// Find the maximum number of cards in any column
	maxInColumn := 0
	for colIdx := 0; colIdx < numCols; colIdx++ {
		count := kd.columnItemCount(colIdx)
		if count > maxInColumn {
			maxInColumn = count
		}
	}
	if maxInColumn > maxCards {
		maxInColumn = maxCards
	}

	// Render cards row by row
	cardStartY := 5
	for cardIdx := 0; cardIdx < maxInColumn; cardIdx++ {
		// Register hit regions for this row of cards
		cardColX := 2
		for colIdx := 0; colIdx < numCols; colIdx++ {
			cardY := cardStartY + (cardIdx * cardHeight)
			if cardIdx < kd.columnItemCount(colIdx) {
				p.mouseHandler.HitMap.AddRect(regionKanbanCard, cardColX, cardY, colWidth-1, cardHeight, kanbanCardData{col: colIdx, row: cardIdx})
			}
			cardColX += colWidth + 1
		}

		// Each card has 4 lines
		for lineIdx := 0; lineIdx < cardHeight; lineIdx++ {
			var rowCells []string
			for colIdx := 0; colIdx < numCols; colIdx++ {
				var cellContent string
				isSelected := colIdx == p.kanbanCol && cardIdx == p.kanbanRow

				wt, shell := kd.itemAt(colIdx, cardIdx)
				if shell != nil {
					cellContent = p.renderKanbanShellCardLine(shell, lineIdx, colWidth-1, isSelected)
				} else if wt != nil {
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
		for j := 0; j < numCols; j++ {
			emptyCells = append(emptyCells, strings.Repeat(" ", colWidth-1))
		}
		lines = append(lines, strings.Join(emptyCells, vertSep))
	}

	content := strings.Join(lines, "\n")
	return styles.RenderPanel(content, width, height, true)
}

// renderKanbanShellCardLine renders a single line of a shell kanban card.
// lineIdx: 0=name, 1=status, 2-3=empty
func (p *Plugin) renderKanbanShellCardLine(shell *ShellSession, lineIdx, width int, isSelected bool) string {
	var content string

	switch lineIdx {
	case 0:
		// Agent-aware status icon (td-693fc7)
		statusIcon := "○"
		if shell.IsOrphaned {
			statusIcon = "◌"
		} else if shell.ChosenAgent != AgentNone && shell.ChosenAgent != "" {
			if shell.Agent != nil {
				switch shell.Agent.Status {
				case AgentStatusRunning:
					statusIcon = "●"
				case AgentStatusWaiting:
					statusIcon = "○"
				case AgentStatusDone:
					statusIcon = "✓"
				case AgentStatusError:
					statusIcon = "✗"
				default:
					statusIcon = "○"
				}
			}
		} else if shell.Agent != nil {
			statusIcon = "●"
		}
		name := shell.Name
		maxNameLen := width - 3
		if runes := []rune(name); len(runes) > maxNameLen {
			name = string(runes[:maxNameLen-3]) + "..."
		}
		content = fmt.Sprintf(" %s %s", statusIcon, name)
	case 1:
		// Agent-aware status text (td-693fc7, td-6b350b)
		var statusText string
		if shell.IsOrphaned {
			if shell.ChosenAgent != AgentNone && shell.ChosenAgent != "" {
				agentAbbrev := shellAgentAbbreviations[shell.ChosenAgent]
				if agentAbbrev == "" {
					agentAbbrev = string(shell.ChosenAgent)
				}
				statusText = fmt.Sprintf("  %s · offline", agentAbbrev)
			} else {
				statusText = "  shell · offline"
			}
		} else if shell.ChosenAgent != AgentNone && shell.ChosenAgent != "" {
			agentAbbrev := shellAgentAbbreviations[shell.ChosenAgent]
			if agentAbbrev == "" {
				agentAbbrev = string(shell.ChosenAgent)
			}
			if shell.Agent != nil {
				statusLabel := "active"
				switch shell.Agent.Status {
				case AgentStatusWaiting:
					statusLabel = "waiting"
				case AgentStatusDone:
					statusLabel = "done"
				case AgentStatusError:
					statusLabel = "error"
				}
				statusText = fmt.Sprintf("  %s · %s", agentAbbrev, statusLabel)
			} else {
				statusText = fmt.Sprintf("  %s · stopped", agentAbbrev)
			}
		} else if shell.Agent != nil {
			statusText = "  shell · running"
		} else {
			statusText = "  shell · no session"
		}
		content = statusText
	}

	if lipgloss.Width(content) > width {
		content = truncateString(content, width)
	}

	// Pad to width
	contentWidth := lipgloss.Width(content)
	if contentWidth < width {
		content += strings.Repeat(" ", width-contentWidth)
	}

	if isSelected {
		return styles.ListItemSelected.Width(width).Render(content)
	}
	if lineIdx > 0 {
		return styles.Muted.Width(width).Render(content)
	}
	return lipgloss.NewStyle().Width(width).Render(content)
}

// renderKanbanCardLine renders a single line of a kanban card.
// lineIdx: 0=name, 1=agent, 2=task, 3=stats
func (p *Plugin) renderKanbanCardLine(wt *Worktree, lineIdx, width int, isSelected bool) string {
	var content string

	switch lineIdx {
	case 0:
		name := wt.Name
		maxNameLen := width - 3
		if runes := []rune(name); len(runes) > maxNameLen {
			name = string(runes[:maxNameLen-3]) + "..."
		}
		content = fmt.Sprintf(" %s %s", wt.Status.Icon(), name)
	case 1:
		agentStr := ""
		if wt.Agent != nil {
			agentStr = "  " + string(wt.Agent.Type)
		} else if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
			agentStr = "  " + string(wt.ChosenAgentType)
		}
		content = agentStr
	case 2:
		if wt.TaskID != "" {
			taskStr := wt.TaskID
			maxLen := width - 2
			if runes := []rune(taskStr); len(runes) > maxLen {
				taskStr = string(runes[:maxLen-3]) + "..."
			}
			content = "  " + taskStr
		}
	case 3:
		if wt.Stats != nil && (wt.Stats.Additions > 0 || wt.Stats.Deletions > 0) {
			content = fmt.Sprintf("  +%d -%d", wt.Stats.Additions, wt.Stats.Deletions)
		}
	}

	contentWidth := lipgloss.Width(content)
	if contentWidth < width {
		content += strings.Repeat(" ", width-contentWidth)
	}

	if isSelected {
		return styles.ListItemSelected.Width(width).Render(content)
	}
	if lineIdx > 0 {
		return styles.Muted.Width(width).Render(content)
	}
	return lipgloss.NewStyle().Width(width).Render(content)
}

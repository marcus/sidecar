package worktree

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

var (
	// Modal styles
	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	inputFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(0, 1)
)

const tabStopWidth = 8

// View renders the plugin UI.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	// CRITICAL: Clear hit regions at start of each render
	p.mouseHandler.Clear()

	switch p.viewMode {
	case ViewModeCreate:
		return p.renderCreateModal(width, height)
	case ViewModeKanban:
		return p.renderKanbanView(width, height)
	case ViewModeTaskLink:
		return p.renderTaskLinkModal(width, height)
	case ViewModeMerge:
		return p.renderMergeModal(width, height)
	case ViewModeAgentChoice:
		return p.renderAgentChoiceModal(width, height)
	case ViewModeConfirmDelete:
		return p.renderConfirmDeleteModal(width, height)
	case ViewModeCommitForMerge:
		return p.renderCommitForMergeModal(width, height)
	case ViewModePromptPicker:
		return p.renderPromptPickerModal(width, height)
	default:
		return p.renderListView(width, height)
	}
}

// renderListView renders the main split-pane list view.
func (p *Plugin) renderListView(width, height int) string {
	// Pane height for panels (outer dimensions including borders)
	paneHeight := height
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height (excluding borders and header lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// If sidebar is hidden, show only preview pane at full width
	if !p.sidebarVisible {
		// Register hit region for full-width preview
		p.mouseHandler.HitMap.AddRect(regionPreviewPane, 0, 0, width, paneHeight, nil)

		// Register preview tab hit regions (same as sidebar-visible case)
		// X starts at 2 (1 for border + 1 for panel padding)
		tabWidths := []int{10, 8, 8} // " Output " + padding, " Diff " + padding, " Task " + padding
		tabX := 2
		for i, tabWidth := range tabWidths {
			p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX, 1, tabWidth, 1, i)
			tabX += tabWidth + 1
		}

		previewContent := p.renderPreviewContent(width-4, innerHeight)
		return styles.RenderPanel(previewContent, width, paneHeight, true)
	}

	// RenderPanel handles borders internally, so only subtract divider
	available := width - dividerWidth
	sidebarW := (available * p.sidebarWidth) / 100
	if sidebarW < 25 {
		sidebarW = 25
	}
	if sidebarW > available-40 {
		sidebarW = available - 40
	}
	previewW := available - sidebarW
	if previewW < 40 {
		previewW = 40
	}

	// Determine pane focus state
	sidebarActive := p.activePane == PaneSidebar
	previewActive := p.activePane == PanePreview

	// Register hit regions (order matters: last = highest priority)
	// 1. Pane regions (lowest priority - fallback for scroll)
	p.mouseHandler.HitMap.AddRect(regionSidebar, 0, 0, sidebarW, paneHeight, nil)
	p.mouseHandler.HitMap.AddRect(regionPreviewPane, sidebarW+dividerWidth, 0, previewW, paneHeight, nil)

	// 2. Divider region (high priority - for drag)
	p.mouseHandler.HitMap.AddRect(regionPaneDivider, sidebarW, 0, dividerHitWidth, paneHeight, nil)

	// 3. Preview tab hit regions (highest priority for tabs)
	// Tabs are rendered at Y=1 (first line inside panel border)
	// X starts at sidebarW + dividerWidth + 2 (1 for border + 1 for padding)
	previewPaneX := sidebarW + dividerWidth + 2 // +1 for border, +1 for panel padding
	// Tab widths: text is " Output " (8), " Diff " (6), " Task " (6)
	// Plus BarChip Padding(0,1) adds 2 chars = 10, 8, 8 visual width
	tabWidths := []int{10, 8, 8}
	tabX := previewPaneX
	for i, tabWidth := range tabWidths {
		p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX, 1, tabWidth, 1, i)
		tabX += tabWidth + 1 // +1 for spacing between tabs
	}

	// Render content for each pane (subtract 4 for border + padding: 2 border + 2 padding)
	sidebarContent := p.renderSidebarContent(sidebarW-4, innerHeight)
	previewContent := p.renderPreviewContent(previewW-4, innerHeight)

	// Apply gradient border styles
	leftPane := styles.RenderPanel(sidebarContent, sidebarW, paneHeight, sidebarActive)
	rightPane := styles.RenderPanel(previewContent, previewW, paneHeight, previewActive)

	// Render visible divider between panes
	divider := p.renderDivider(paneHeight)

	// Join horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// renderSidebarContent renders the worktree list sidebar content (no borders).
func (p *Plugin) renderSidebarContent(width, height int) string {
	var lines []string

	// Header with [New] button
	titleText := "Worktrees"
	buttonText := "[New]"
	buttonStyle := styles.Button
	if p.hoverNewButton {
		buttonStyle = styles.ButtonHover
	}
	styledButton := buttonStyle.Render(buttonText)
	buttonWidth := lipgloss.Width(styledButton)

	// Calculate spacing between title and button
	titleWidth := lipgloss.Width(titleText)
	spacing := width - titleWidth - buttonWidth
	if spacing < 1 {
		spacing = 1
	}

	header := styles.Title.Render(titleText) + strings.Repeat(" ", spacing) + styledButton
	lines = append(lines, header)

	// Register hit region for the button (X position = 2 for panel padding + spacing + titleWidth)
	// The button is at the right edge of the sidebar content
	buttonX := 2 + titleWidth + spacing // 2 for left border+padding
	p.mouseHandler.HitMap.AddRect(regionCreateWorktreeButton, buttonX, 1, buttonWidth, 1, nil)

	// Show warnings from delete operation if any
	if len(p.deleteWarnings) > 0 {
		warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Orange
		for _, w := range p.deleteWarnings {
			// Truncate warning to fit width
			if len(w) > width-2 {
				w = w[:width-5] + "..."
			}
			lines = append(lines, warningStyle.Render("⚠ "+w))
		}
	}

	lines = append(lines, "") // Empty line after header/warnings

	// Track Y position for hit regions (add 3 for border + header + empty line)
	currentY := 3

	// Calculate visible items (each item is 2 lines)
	contentHeight := height - 2 // header + empty line
	itemHeight := 2             // Each worktree item takes 2 lines
	p.visibleCount = contentHeight / itemHeight

	// Render worktree items
	if len(p.worktrees) == 0 {
		// Calculate vertical centering for empty state
		emptyStateHeight := 2 // "No worktrees" + "Press 'n'..."
		emptyStartY := (contentHeight - emptyStateHeight) / 2
		if emptyStartY < 0 {
			emptyStartY = 0
		}

		// Add padding lines before empty state message
		for i := 0; i < emptyStartY; i++ {
			lines = append(lines, "")
		}

		// Center the text horizontally
		msg1 := "No worktrees"
		msg2 := "Press 'n' to create one"
		pad1 := (width - len(msg1)) / 2
		pad2 := (width - len(msg2)) / 2
		if pad1 < 0 {
			pad1 = 0
		}
		if pad2 < 0 {
			pad2 = 0
		}

		lines = append(lines, styles.Muted.Render(strings.Repeat(" ", pad1)+msg1))
		lines = append(lines, styles.Muted.Render(strings.Repeat(" ", pad2)+msg2))
	} else {
		for i := p.scrollOffset; i < len(p.worktrees) && i < p.scrollOffset+p.visibleCount; i++ {
			wt := p.worktrees[i]
			line := p.renderWorktreeItem(wt, i == p.selectedIdx, width)

			// Register hit region with ABSOLUTE index
			p.mouseHandler.HitMap.AddRect(regionWorktreeItem, 0, currentY, width, itemHeight, i)

			lines = append(lines, line)
			currentY += itemHeight
		}
	}

	return strings.Join(lines, "\n")
}

// renderWorktreeItem renders a single worktree list item.
func (p *Plugin) renderWorktreeItem(wt *Worktree, selected bool, width int) string {
	// Keep selection visible even when preview pane is active (dimmed style)
	isSelected := selected
	isActiveFocus := selected && p.activePane == PaneSidebar

	// Status indicator
	statusIcon := wt.Status.Icon()

	// Check for conflicts
	hasConflict := p.hasConflict(wt.Name, p.conflicts)
	conflictIcon := ""
	if hasConflict {
		conflictIcon = " ⚠"
	}

	// Check for PR
	hasPR := wt.PRURL != ""
	prIcon := ""
	if hasPR {
		prIcon = " PR"
	}

	// Name and time
	name := wt.Name
	timeStr := formatRelativeTime(wt.UpdatedAt)

	// Stats if available
	statsStr := ""
	if wt.Stats != nil && (wt.Stats.Additions > 0 || wt.Stats.Deletions > 0) {
		statsStr = fmt.Sprintf("+%d -%d", wt.Stats.Additions, wt.Stats.Deletions)
	}

	// Build second line parts (plain text)
	var parts []string
	if wt.Agent != nil {
		parts = append(parts, string(wt.Agent.Type))
	} else if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
		parts = append(parts, string(wt.ChosenAgentType))
	} else {
		parts = append(parts, "—")
	}
	if wt.TaskID != "" {
		parts = append(parts, wt.TaskID)
	}
	if statsStr != "" {
		parts = append(parts, statsStr)
	}
	if hasConflict {
		conflictFiles := p.getConflictingFiles(wt.Name, p.conflicts)
		if len(conflictFiles) > 0 {
			parts = append(parts, fmt.Sprintf("⚠ %d conflicts", len(conflictFiles)))
		}
	}

	// When selected, use plain text to ensure consistent background
	if isSelected {
		// Build plain text lines
		line1 := fmt.Sprintf(" %s %s%s%s", statusIcon, name, prIcon, conflictIcon)
		line1Width := lipgloss.Width(line1)
		timeWidth := lipgloss.Width(timeStr)
		if line1Width < width-timeWidth-2 {
			line1 = line1 + strings.Repeat(" ", width-line1Width-timeWidth-1) + timeStr
		}
		line2 := "   " + strings.Join(parts, "  ")
		// Pad line2 to full width for consistent background
		line2Width := lipgloss.Width(line2)
		if line2Width < width {
			line2 = line2 + strings.Repeat(" ", width-line2Width)
		}
		content := line1 + "\n" + line2

		// Use bright style when sidebar is focused, dimmed when preview is focused
		if isActiveFocus {
			return styles.ListItemSelected.Width(width).Render(content)
		}
		// Dimmed selection style (when preview pane is active)
		dimmedSelectedStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("237")). // Darker background
			Foreground(lipgloss.Color("252")). // Slightly dimmed text
			Width(width)
		return dimmedSelectedStyle.Render(content)
	}

	// Not selected - use colored styles for visual interest
	var statusStyle lipgloss.Style
	switch wt.Status {
	case StatusActive:
		statusStyle = styles.StatusCompleted // Green
	case StatusWaiting:
		statusStyle = styles.StatusModified // Yellow/orange (warning)
	case StatusDone:
		statusStyle = styles.StatusCompleted // Green
	case StatusError:
		statusStyle = styles.StatusDeleted // Red
	default:
		statusStyle = styles.Muted // Gray for paused
	}
	icon := statusStyle.Render(statusIcon)

	// Apply conflict style
	styledConflictIcon := ""
	if hasConflict {
		styledConflictIcon = styles.StatusModified.Render(" ⚠")
	}

	// Apply PR style
	styledPRIcon := ""
	if hasPR {
		styledPRIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render(" PR") // blue
	}

	// For non-selected, style parts individually
	var styledParts []string
	if wt.Agent != nil {
		styledParts = append(styledParts, string(wt.Agent.Type))
	} else if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
		styledParts = append(styledParts, dimText(string(wt.ChosenAgentType)))
	} else {
		styledParts = append(styledParts, "—")
	}
	if wt.TaskID != "" {
		styledParts = append(styledParts, wt.TaskID)
	}
	if statsStr != "" {
		styledParts = append(styledParts, statsStr)
	}
	if hasConflict {
		conflictFiles := p.getConflictingFiles(wt.Name, p.conflicts)
		if len(conflictFiles) > 0 {
			styledParts = append(styledParts, styles.StatusModified.Render(fmt.Sprintf("⚠ %d conflicts", len(conflictFiles))))
		}
	}

	// Build lines with styled elements
	line1 := fmt.Sprintf(" %s %s%s%s", icon, name, styledPRIcon, styledConflictIcon)
	line1Width := ansi.StringWidth(line1)
	timeWidth := ansi.StringWidth(timeStr)
	if line1Width < width-timeWidth-2 {
		line1 = line1 + strings.Repeat(" ", width-line1Width-timeWidth-1) + timeStr
	}
	line2 := "   " + strings.Join(styledParts, "  ")

	content := line1 + "\n" + line2
	return styles.ListItemNormal.Width(width).Render(content)
}

// renderDivider renders the vertical divider between panes.
func (p *Plugin) renderDivider(height int) string {
	// Use a subtle vertical bar as the divider with theme color
	// MarginTop(1) shifts it down to align with pane content (below top border)
	dividerStyle := lipgloss.NewStyle().
		Foreground(styles.BorderNormal).
		MarginTop(1)

	var sb strings.Builder
	for i := 0; i < height; i++ {
		sb.WriteString("│")
		if i < height-1 {
			sb.WriteString("\n")
		}
	}
	return dividerStyle.Render(sb.String())
}

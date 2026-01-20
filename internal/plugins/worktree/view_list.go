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
	// Clear truncation cache if dimensions changed
	if p.width != width || p.height != height {
		p.truncateCache.Clear()
	}

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
	case ViewModeTypeSelector:
		return p.renderTypeSelectorModal(width, height)
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

		// Register preview tab hit regions only when a worktree is selected (not shell)
		// Shell has no tabs - it shows primer/output directly
		if !p.shellSelected {
			// X starts at 2 (1 for border + 1 for panel padding)
			tabWidths := []int{10, 8, 8} // " Output " + padding, " Diff " + padding, " Task " + padding
			tabX := 2
			for i, tabWidth := range tabWidths {
				p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX, 1, tabWidth, 1, i)
				tabX += tabWidth + 1
			}
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
	// Only register when a worktree is selected (not shell)
	// Shell has no tabs - it shows primer/output directly
	if !p.shellSelected {
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
	buttonText := "New"
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

	// === Render shells section ===
	if len(p.shells) > 0 {
		// Shells subheader
		lines = append(lines, styles.Muted.Render("Shells"))
		currentY++

		// Render each shell entry
		for i, shell := range p.shells {
			selected := p.shellSelected && i == p.selectedShellIdx
			shellLine := p.renderShellEntryForSession(shell, selected, width)
			lines = append(lines, shellLine)
			// Register hit region with negative index: -1 -> shells[0], -2 -> shells[1], etc.
			p.mouseHandler.HitMap.AddRect(regionWorktreeItem, 0, currentY, width, itemHeight, -(i + 1))
			currentY += itemHeight
		}

		// Add separator line after shells
		lines = append(lines, styles.Muted.Render(strings.Repeat("─", width)))
		currentY++
	}

	// Calculate shell section height (subheader + shells*2 + separator)
	shellSectionHeight := 0
	if len(p.shells) > 0 {
		shellSectionHeight = 1 + len(p.shells)*itemHeight + 1
	}

	// Adjust visible count for shell section
	p.visibleCount = (contentHeight - shellSectionHeight) / itemHeight

	// Render worktree items
	if len(p.worktrees) == 0 {
		// No worktrees exist - show empty state message (unless shell is selected or shells exist)
		if !p.shellSelected && len(p.shells) == 0 {
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
		}
		// When shell is selected and no worktrees, just show the shell entries (already rendered above)
	} else {
		// Worktrees subheader (only if we have shells above)
		if len(p.shells) > 0 {
			lines = append(lines, styles.Muted.Render("Worktrees"))
			currentY++
		}

		// Guard against negative scrollOffset
		if p.scrollOffset < 0 {
			p.scrollOffset = 0
		}
		for i := p.scrollOffset; i < len(p.worktrees) && i < p.scrollOffset+p.visibleCount; i++ {
			wt := p.worktrees[i]
			// Only show as selected if not shellSelected AND index matches
			selected := !p.shellSelected && i == p.selectedIdx
			line := p.renderWorktreeItem(wt, selected, width)

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

	// Calculate max name width to prevent wrapping
	// Line structure: " [icon] [name][prIcon][conflictIcon]  [time]"
	// Reserve: 4 (leading space + icon + space) + icons + time + 2 (min padding)
	iconWidth := 4 // " X " where X is status icon
	prWidth := 0
	if hasPR {
		prWidth = 3 // " PR"
	}
	conflictWidth := 0
	if hasConflict {
		conflictWidth = 2 // " ⚠"
	}
	timeWidth := lipgloss.Width(timeStr)
	minPadding := 2
	maxNameWidth := width - iconWidth - prWidth - conflictWidth - timeWidth - minPadding
	if maxNameWidth < 8 {
		maxNameWidth = 8 // Minimum name width
	}
	// Truncate name if too long (use runes for proper Unicode handling)
	nameRunes := []rune(name)
	if len(nameRunes) > maxNameWidth {
		if maxNameWidth > 1 {
			name = string(nameRunes[:maxNameWidth-1]) + "…"
		} else {
			name = "…"
		}
	}

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
	if line1Width < width-timeWidth-2 {
		line1 = line1 + strings.Repeat(" ", width-line1Width-timeWidth-1) + timeStr
	}
	line2 := "   " + strings.Join(styledParts, "  ")

	content := line1 + "\n" + line2
	return styles.ListItemNormal.Width(width).Render(content)
}

// renderShellEntryForSession renders a shell entry for a specific shell session.
func (p *Plugin) renderShellEntryForSession(shell *ShellSession, selected bool, width int) string {
	isActiveFocus := selected && p.activePane == PaneSidebar

	// Determine icon based on session state
	var statusIcon string
	var statusStyle lipgloss.Style
	if shell.Agent != nil {
		statusIcon = "●" // Session running
		statusStyle = styles.StatusCompleted // Green
	} else {
		statusIcon = "○" // No session
		statusStyle = styles.Muted
	}

	// Use shell display name
	displayName := shell.Name

	// Build second line
	var statusText string
	if shell.Agent != nil {
		statusText = "shell · running"
	} else {
		statusText = "shell · no session"
	}

	// Calculate layout
	maxNameWidth := width - 4 - 2 // icon + padding
	nameRunes := []rune(displayName)
	if len(nameRunes) > maxNameWidth {
		displayName = string(nameRunes[:maxNameWidth-1]) + "…"
	}

	// Build lines
	if selected {
		// Selected style
		line1 := fmt.Sprintf(" %s %s", statusIcon, displayName)
		line1Width := lipgloss.Width(line1)
		if line1Width < width {
			line1 = line1 + strings.Repeat(" ", width-line1Width)
		}
		line2 := "   " + statusText
		line2Width := lipgloss.Width(line2)
		if line2Width < width {
			line2 = line2 + strings.Repeat(" ", width-line2Width)
		}
		content := line1 + "\n" + line2

		if isActiveFocus {
			return styles.ListItemSelected.Width(width).Render(content)
		}
		// Dimmed selection style
		dimmedSelectedStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("252")).
			Width(width)
		return dimmedSelectedStyle.Render(content)
	}

	// Not selected - use styled icon
	icon := statusStyle.Render(statusIcon)
	line1 := fmt.Sprintf(" %s %s", icon, displayName)
	line2 := "   " + dimText(statusText)
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

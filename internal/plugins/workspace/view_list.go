package workspace

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// Modal style functions - return fresh styles using current theme colors.
func modalStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderActive).
		Padding(1, 2)
}

func inputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(styles.BorderNormal).
		Padding(0, 1)
}

func inputFocusedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(styles.Primary).
		Padding(0, 1)
}


// Panel dimension constants for consistent width calculations.
// These must stay in sync with styles.RenderGradientBorder.
const (
	panelBorderWidth  = 2 // Left + right border (1 each)
	panelPaddingWidth = 2 // Left + right padding (1 each)
	panelOverhead     = panelBorderWidth + panelPaddingWidth // Total overhead: 4
)

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
	case ViewModeConfirmDeleteShell:
		return p.renderConfirmDeleteShellModal(width, height)
	case ViewModeCommitForMerge:
		return p.renderCommitForMergeModal(width, height)
	case ViewModePromptPicker:
		return p.renderPromptPickerModal(width, height)
	case ViewModeTypeSelector:
		return p.renderTypeSelectorModal(width, height)
	case ViewModeRenameShell:
		return p.renderRenameShellModal(width, height)
	case ViewModeFilePicker:
		background := p.renderListView(width, height)
		return p.renderFilePickerModal(background)
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
		// Full-width preview: outer width is the full width, content width subtracts panel overhead
		previewW := width
		contentW := previewW - panelOverhead

		// Register hit region for full-width preview (uses outer dimensions)
		p.mouseHandler.HitMap.AddRect(regionPreviewPane, 0, 0, previewW, paneHeight, nil)

		// Register preview tab hit regions only when a worktree is selected (not shell)
		// Shell has no tabs - it shows primer/output directly
		if !p.shellSelected {
			// X starts at panelOverhead/2 (1 for border + 1 for panel padding)
			tabWidths := []int{10, 8, 8} // " Output " + padding, " Diff " + padding, " Task " + padding
			tabX := panelOverhead / 2
			for i, tabWidth := range tabWidths {
				p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX, 1, tabWidth, 1, i)
				tabX += tabWidth + 1
			}
		}

		// Render content using calculated content width (consistent with panel overhead)
		previewContent := p.renderPreviewContent(contentW, innerHeight)

		// Check if preview should flash (guard against zero-value time)
		flashActive := !p.flashPreviewTime.IsZero() && time.Since(p.flashPreviewTime) < flashDuration
		if flashActive {
			return styles.RenderPanelWithGradient(previewContent, previewW, paneHeight, styles.GetFlashGradient())
		}
		return styles.RenderPanel(previewContent, previewW, paneHeight, true)
	}

	// Calculate pane widths for split view
	// RenderPanel handles borders internally, so only subtract divider from available space
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

	// Calculate content widths (subtract panel overhead for borders + padding)
	sidebarContentW := sidebarW - panelOverhead
	previewContentW := previewW - panelOverhead

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
		// X starts at sidebarW + dividerWidth + panelOverhead/2 (border + padding on left side)
		previewPaneX := sidebarW + dividerWidth + panelOverhead/2
		// Tab widths: text is " Output " (8), " Diff " (6), " Task " (6)
		// Plus BarChip Padding(0,1) adds 2 chars = 10, 8, 8 visual width
		tabWidths := []int{10, 8, 8}
		tabX := previewPaneX
		for i, tabWidth := range tabWidths {
			p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX, 1, tabWidth, 1, i)
			tabX += tabWidth + 1 // +1 for spacing between tabs
		}
	}

	// Render content for each pane using pre-calculated content widths
	sidebarContent := p.renderSidebarContent(sidebarContentW, innerHeight)
	previewContent := p.renderPreviewContent(previewContentW, innerHeight)

	// Check if preview should flash (guard against zero-value time)
	flashActive := !p.flashPreviewTime.IsZero() && time.Since(p.flashPreviewTime) < flashDuration

	// Apply gradient border styles
	leftPane := styles.RenderPanel(sidebarContent, sidebarW, paneHeight, sidebarActive)

	var rightPane string
	if p.viewMode == ViewModeInteractive {
		// Use interactive gradient when in interactive mode (td-70aed9)
		rightPane = styles.RenderPanelWithGradient(previewContent, previewW, paneHeight, styles.GetInteractiveGradient())
	} else if flashActive && previewActive {
		rightPane = styles.RenderPanelWithGradient(previewContent, previewW, paneHeight, styles.GetFlashGradient())
	} else {
		rightPane = styles.RenderPanel(previewContent, previewW, paneHeight, previewActive)
	}

	// Render visible divider between panes
	divider := p.renderDivider(paneHeight)

	// Join horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// renderSidebarContent renders the worktree list sidebar content (no borders).
func (p *Plugin) renderSidebarContent(width, height int) string {
	var lines []string

	// Header with [New] button
	titleText := "Workspaces"
	buttonText := "New"
	buttonStyle := styles.Button
	if p.hoverNewButton {
		buttonStyle = styles.ButtonHover
	}
	styledButton := styles.RenderPillWithStyle(buttonText, buttonStyle, "")
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
		warningStyle := lipgloss.NewStyle().Foreground(styles.Warning)
		for _, w := range p.deleteWarnings {
			// Truncate warning to fit width
			if len(w) > width-2 {
				w = w[:width-5] + "..."
			}
			lines = append(lines, warningStyle.Render("⚠ "+w))
		}
	}

	// Show toast message if active (td-a1c8456f: session disconnect notification)
	if p.toastMessage != "" && !p.toastTime.IsZero() && time.Since(p.toastTime) < flashDuration {
		toastStyle := lipgloss.NewStyle().Foreground(styles.Warning).Bold(true)
		msg := p.toastMessage
		if len(msg) > width-4 {
			msg = msg[:width-7] + "..."
		}
		lines = append(lines, toastStyle.Render("⚠ "+msg))
	}

	lines = append(lines, "") // Empty line after header/warnings

	// Track Y position for hit regions (add 3 for border + header + empty line)
	currentY := 3

	// Calculate visible items (each item is 2 lines)
	contentHeight := height - 2 // header + empty line
	itemHeight := 2             // Each worktree item takes 2 lines

	// === Render shells section ===
	if len(p.shells) > 0 {
		// Shells subheader with [+] button (right-aligned)
		shellsTitle := styles.Muted.Render("Shells")
		shellsTitleWidth := lipgloss.Width(shellsTitle)
		shellsPlusStyle := styles.Button
		if p.hoverShellsPlusButton {
			shellsPlusStyle = styles.ButtonHover
		}
		shellsPlusBtn := styles.RenderPillWithStyle("+", shellsPlusStyle, "")
		shellsPlusBtnWidth := lipgloss.Width(shellsPlusBtn)
		// Right-align button with fill spacing
		spacing := width - shellsTitleWidth - shellsPlusBtnWidth
		if spacing < 1 {
			spacing = 1
		}
		shellsHeader := shellsTitle + strings.Repeat(" ", spacing) + shellsPlusBtn
		lines = append(lines, shellsHeader)
		// Register hit region for shells [+] button (right-aligned)
		shellsPlusBtnX := 2 + shellsTitleWidth + spacing // 2 for left border+padding
		p.mouseHandler.HitMap.AddRect(regionShellsPlusButton, shellsPlusBtnX, currentY, shellsPlusBtnWidth, 1, nil)
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
			emptyStateHeight := 2 // "No workspaces" + "Press 'n'..."
			emptyStartY := (contentHeight - emptyStateHeight) / 2
			if emptyStartY < 0 {
				emptyStartY = 0
			}

			// Add padding lines before empty state message
			for i := 0; i < emptyStartY; i++ {
				lines = append(lines, "")
			}

			// Center the text horizontally
			msg1 := "No workspaces"
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
		// Worktrees subheader with [+] button (right-aligned, only if we have shells above)
		if len(p.shells) > 0 {
			workspacesTitle := styles.Muted.Render("Workspaces")
			workspacesTitleWidth := lipgloss.Width(workspacesTitle)
			workspacesPlusStyle := styles.Button
			if p.hoverWorkspacesPlusButton {
				workspacesPlusStyle = styles.ButtonHover
			}
			workspacesPlusBtn := styles.RenderPillWithStyle("+", workspacesPlusStyle, "")
			workspacesPlusBtnWidth := lipgloss.Width(workspacesPlusBtn)
			// Right-align button with fill spacing
			spacing := width - workspacesTitleWidth - workspacesPlusBtnWidth
			if spacing < 1 {
				spacing = 1
			}
			workspacesHeader := workspacesTitle + strings.Repeat(" ", spacing) + workspacesPlusBtn
			lines = append(lines, workspacesHeader)
			// Register hit region for worktrees [+] button (right-aligned)
			workspacesPlusBtnX := 2 + workspacesTitleWidth + spacing // 2 for left border+padding
			p.mouseHandler.HitMap.AddRect(regionWorkspacesPlusButton, workspacesPlusBtnX, currentY, workspacesPlusBtnWidth, 1, nil)
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

	// Status indicator - use special icon for main worktree
	var statusIcon string
	if wt.IsMain {
		statusIcon = "◉" // Bullseye icon for main/primary worktree
	} else {
		statusIcon = wt.Status.Icon()
	}

	// Check for conflicts
	hasConflict := p.hasConflict(wt.Name, p.conflicts)
	conflictIcon := ""
	if hasConflict {
		conflictIcon = " ⚠"
	}

	// Check for orphaned (session crashed)
	orphanedIcon := ""
	if wt.IsOrphaned {
		orphanedIcon = " ⚠"
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
	// Line structure: " [icon] [name][prIcon][conflictIcon][orphanedIcon]  [time]"
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
	orphanedWidth := 0
	if wt.IsOrphaned {
		orphanedWidth = 2 // " ⚠"
	}
	timeWidth := lipgloss.Width(timeStr)
	minPadding := 2
	maxNameWidth := width - iconWidth - prWidth - conflictWidth - orphanedWidth - timeWidth - minPadding
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
	if wt.IsMain {
		// For root workspace, show branch name instead of agent
		parts = append(parts, wt.Branch)
	} else if wt.Agent != nil {
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
	if wt.IsOrphaned {
		parts = append(parts, "⚠ session ended")
	}

	// When selected, use plain text to ensure consistent background
	if isSelected {
		// Build plain text lines
		line1 := fmt.Sprintf(" %s %s%s%s%s", statusIcon, name, prIcon, conflictIcon, orphanedIcon)
		line1Width := lipgloss.Width(line1)
		if line1Width < width-timeWidth-2 {
			line1 = line1 + strings.Repeat(" ", width-line1Width-timeWidth-1) + timeStr
		}
		line2 := "   " + strings.Join(parts, "  ")
		// Truncate line2 to prevent wrapping
		line2Width := lipgloss.Width(line2)
		if line2Width > width {
			line2Runes := []rune(line2)
			if width > 1 {
				line2 = string(line2Runes[:width-1]) + "…"
			}
			line2Width = width
		}
		// Pad line2 to full width for consistent background
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
			Background(styles.BgSecondary).
			Foreground(styles.TextSecondary).
			Width(width)
		return dimmedSelectedStyle.Render(content)
	}

	// Not selected - use colored styles for visual interest
	var statusStyle lipgloss.Style
	if wt.IsMain {
		// Primary/cyan color for main worktree to stand out
		statusStyle = lipgloss.NewStyle().Foreground(styles.Primary)
	} else {
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
	}
	icon := statusStyle.Render(statusIcon)

	// Apply conflict style
	styledConflictIcon := ""
	if hasConflict {
		styledConflictIcon = styles.StatusModified.Render(" ⚠")
	}

	// Apply orphaned style (session ended)
	styledOrphanedIcon := ""
	if wt.IsOrphaned {
		styledOrphanedIcon = styles.StatusModified.Render(" ⚠")
	}

	// Apply PR style
	styledPRIcon := ""
	if hasPR {
		styledPRIcon = lipgloss.NewStyle().Foreground(styles.Secondary).Render(" PR")
	}

	// For non-selected, style parts individually
	var styledParts []string
	if wt.IsMain {
		// For root workspace, show branch name instead of agent
		styledParts = append(styledParts, wt.Branch)
	} else if wt.Agent != nil {
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
	if wt.IsOrphaned {
		styledParts = append(styledParts, styles.StatusModified.Render("⚠ session ended"))
	}

	// Build lines with styled elements
	line1 := fmt.Sprintf(" %s %s%s%s%s", icon, name, styledPRIcon, styledConflictIcon, styledOrphanedIcon)
	line1Width := ansi.StringWidth(line1)
	if line1Width < width-timeWidth-2 {
		line1 = line1 + strings.Repeat(" ", width-line1Width-timeWidth-1) + timeStr
	}
	line2 := "   " + strings.Join(styledParts, "  ")
	// Truncate line2 to prevent wrapping
	if ansi.StringWidth(line2) > width {
		line2 = ansi.Truncate(line2, width-1, "…")
	}

	content := line1 + "\n" + line2
	return styles.ListItemNormal.Width(width).Render(content)
}

// renderShellEntryForSession renders a shell entry for a specific shell session.
func (p *Plugin) renderShellEntryForSession(shell *ShellSession, selected bool, width int) string {
	isActiveFocus := selected && p.activePane == PaneSidebar

	// Determine icon based on session state and agent status
	var statusIcon string
	var statusStyle lipgloss.Style

	// td-f88fdd: Handle orphaned shells (manifest entry exists but tmux session is gone)
	if shell.IsOrphaned {
		statusIcon = "◌" // Empty circle for orphaned
		statusStyle = styles.Muted
	} else if shell.ChosenAgent != AgentNone && shell.ChosenAgent != "" {
		// td-a29b76: Show agent-specific status when an AI agent is running
		// Shell has an AI agent - show agent status
		if shell.Agent != nil {
			switch shell.Agent.Status {
			case AgentStatusRunning:
				statusIcon = "●"
				statusStyle = styles.StatusCompleted // Green - active
			case AgentStatusWaiting:
				statusIcon = "○"
				statusStyle = styles.StatusModified // Yellow - waiting for input
			case AgentStatusDone:
				statusIcon = "✓"
				statusStyle = styles.StatusCompleted // Green/blue - done
			case AgentStatusError:
				statusIcon = "✗"
				statusStyle = styles.StatusDeleted // Red - error
			default:
				statusIcon = "○"
				statusStyle = styles.Muted // Gray - idle/paused
			}
		} else {
			statusIcon = "○"
			statusStyle = styles.Muted
		}
	} else if shell.Agent != nil {
		// Plain shell (no AI agent)
		statusIcon = "●"
		statusStyle = styles.StatusCompleted // Green
	} else {
		statusIcon = "○"
		statusStyle = styles.Muted
	}

	// Use shell display name
	displayName := shell.Name

	// td-a29b76: Build second line with agent type if present
	var statusText string
	if shell.IsOrphaned {
		// td-f88fdd: Orphaned shell - show "offline" status
		if shell.ChosenAgent != AgentNone && shell.ChosenAgent != "" {
			agentAbbrev := shellAgentAbbreviations[shell.ChosenAgent]
			if agentAbbrev == "" {
				agentAbbrev = string(shell.ChosenAgent)
			}
			statusText = fmt.Sprintf("%s · offline", agentAbbrev)
		} else {
			statusText = "shell · offline"
		}
	} else if shell.ChosenAgent != AgentNone && shell.ChosenAgent != "" {
		// Show agent type abbreviation
		agentAbbrev := shellAgentAbbreviations[shell.ChosenAgent]
		if agentAbbrev == "" {
			agentAbbrev = string(shell.ChosenAgent)
		}
		if shell.Agent != nil {
			statusText = fmt.Sprintf("%s · running", agentAbbrev)
		} else {
			statusText = fmt.Sprintf("%s · stopped", agentAbbrev)
		}
	} else if shell.Agent != nil {
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
		if line2Width > width {
			line2Runes := []rune(line2)
			if width > 1 {
				line2 = string(line2Runes[:width-1]) + "…"
			}
			line2Width = width
		}
		if line2Width < width {
			line2 = line2 + strings.Repeat(" ", width-line2Width)
		}
		content := line1 + "\n" + line2

		if isActiveFocus {
			return styles.ListItemSelected.Width(width).Render(content)
		}
		// Dimmed selection style
		dimmedSelectedStyle := lipgloss.NewStyle().
			Background(styles.BgSecondary).
			Foreground(styles.TextSecondary).
			Width(width)
		return dimmedSelectedStyle.Render(content)
	}

	// Not selected - use styled icon
	icon := statusStyle.Render(statusIcon)
	line1 := fmt.Sprintf(" %s %s", icon, displayName)
	line2 := "   " + dimText(statusText)
	// Truncate line2 to prevent wrapping
	if ansi.StringWidth(line2) > width {
		line2 = ansi.Truncate(line2, width-1, "…")
	}
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

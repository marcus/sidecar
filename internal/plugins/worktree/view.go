package worktree

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
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
		previewW := width - 4 // Account for borders
		if previewW < 40 {
			previewW = 40
		}

		// Register hit region for full-width preview
		p.mouseHandler.HitMap.AddRect(regionPreviewPane, 0, 0, width, paneHeight, nil)

		previewContent := p.renderPreviewContent(previewW, innerHeight)
		return styles.RenderPanel(previewContent, width, paneHeight, true)
	}

	// Calculate pane widths (sidebarWidth is percentage)
	// Account for borders (4 total: 2 per pane) and divider
	available := width - 6 - dividerWidth // 6 for borders (2 per pane × 2 + padding)
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

	// Render content for each pane
	sidebarContent := p.renderSidebarContent(sidebarW-2, innerHeight)
	previewContent := p.renderPreviewContent(previewW-2, innerHeight)

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

	// Header with view mode toggle
	header := styles.Title.Render("Worktrees")
	// View mode toggle tabs
	listTab := "[List]"
	kanbanTab := "Kanban"
	if p.viewMode == ViewModeKanban {
		listTab = "List"
		kanbanTab = "[Kanban]"
	}
	viewToggle := styles.Muted.Render(listTab + "|" + kanbanTab)
	headerLine := header + strings.Repeat(" ", max(1, width-len("Worktrees")-len(listTab)-len(kanbanTab)-1)) + viewToggle
	lines = append(lines, headerLine)
	lines = append(lines, "") // Empty line after header

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
	// Status indicator - use theme colors
	statusIcon := wt.Status.Icon()
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

	// Check for conflicts
	hasConflict := p.hasConflict(wt.Name, p.conflicts)
	conflictIcon := ""
	if hasConflict {
		conflictIcon = styles.StatusModified.Render(" ⚠")
	}

	// Name and time
	name := wt.Name
	timeStr := formatRelativeTime(wt.UpdatedAt)

	// Stats if available
	statsStr := ""
	if wt.Stats != nil && (wt.Stats.Additions > 0 || wt.Stats.Deletions > 0) {
		statsStr = fmt.Sprintf("+%d -%d", wt.Stats.Additions, wt.Stats.Deletions)
	}

	// First line: icon, name, conflict indicator, time
	line1 := fmt.Sprintf(" %s %s%s", icon, name, conflictIcon)
	if len(line1) < width-len(timeStr)-2 {
		line1 = line1 + strings.Repeat(" ", width-len(line1)-len(timeStr)-1) + timeStr
	}

	// Second line: agent type, task ID, stats, conflict info
	var parts []string
	if wt.Agent != nil {
		parts = append(parts, string(wt.Agent.Type))
	} else {
		parts = append(parts, "—")
	}
	if wt.TaskID != "" {
		parts = append(parts, wt.TaskID)
	}
	if statsStr != "" {
		parts = append(parts, statsStr)
	}
	// Show conflict info on the second line
	if hasConflict {
		conflictFiles := p.getConflictingFiles(wt.Name, p.conflicts)
		if len(conflictFiles) > 0 {
			conflictStr := fmt.Sprintf("⚠ %d conflicts", len(conflictFiles))
			parts = append(parts, styles.StatusModified.Render(conflictStr))
		}
	}
	line2 := "   " + strings.Join(parts, "  ")

	// Combine lines
	content := line1 + "\n" + line2

	if selected && p.activePane == PaneSidebar {
		return styles.ListItemSelected.Width(width).Render(content)
	}
	return styles.ListItemNormal.Width(width).Render(content)
}

// renderPreviewContent renders the preview pane content (no borders).
func (p *Plugin) renderPreviewContent(width, height int) string {
	var lines []string

	// Tab header
	tabs := p.renderTabs(width)
	lines = append(lines, tabs)
	lines = append(lines, "") // Empty line after header

	contentHeight := height - 2 // header + empty line

	// Render content based on active tab
	var content string
	switch p.previewTab {
	case PreviewTabOutput:
		content = p.renderOutputContent(width, contentHeight)
	case PreviewTabDiff:
		content = p.renderDiffContent(width, contentHeight)
	case PreviewTabTask:
		content = p.renderTaskContent(width, contentHeight)
	}

	lines = append(lines, content)

	return strings.Join(lines, "\n")
}

// renderTabs renders the preview pane tab header.
func (p *Plugin) renderTabs(width int) string {
	tabs := []string{"Output", "Diff", "Task"}
	var rendered []string

	for i, tab := range tabs {
		if PreviewTab(i) == p.previewTab {
			rendered = append(rendered, styles.BarChipActive.Render(" "+tab+" "))
		} else {
			rendered = append(rendered, styles.BarChip.Render(" "+tab+" "))
		}
	}

	return strings.Join(rendered, " ")
}

// renderOutputContent renders agent output.
func (p *Plugin) renderOutputContent(width, height int) string {
	wt := p.selectedWorktree()
	if wt == nil {
		return dimText("No worktree selected")
	}

	if wt.Agent == nil {
		return dimText("No agent running\nPress 'a' to start an agent")
	}

	if wt.Agent.OutputBuf == nil {
		return dimText("No output yet")
	}

	lines := wt.Agent.OutputBuf.Lines()
	if len(lines) == 0 {
		return dimText("No output yet")
	}

	// Apply scroll offset and limit to height
	start := p.previewOffset
	if start >= len(lines) {
		start = len(lines) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

// renderDiffContent renders git diff.
func (p *Plugin) renderDiffContent(width, height int) string {
	if p.diffContent == "" {
		wt := p.selectedWorktree()
		if wt == nil {
			return dimText("No worktree selected")
		}
		return dimText("No changes")
	}

	lines := splitLines(p.diffContent)

	// Apply scroll offset
	start := p.previewOffset
	if start >= len(lines) {
		start = len(lines) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}

	// Basic diff highlighting
	var rendered []string
	for _, line := range lines[start:end] {
		rendered = append(rendered, colorDiffLine(line, width))
	}

	return strings.Join(rendered, "\n")
}

// colorDiffLine applies basic diff coloring using theme styles.
func colorDiffLine(line string, width int) string {
	if len(line) == 0 {
		return line
	}

	// Truncate if needed
	if len(line) > width {
		line = line[:width]
	}

	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		return styles.DiffHeader.Render(line)
	case strings.HasPrefix(line, "@@"):
		return lipgloss.NewStyle().Foreground(styles.Info).Render(line)
	case strings.HasPrefix(line, "+"):
		return styles.DiffAdd.Render(line)
	case strings.HasPrefix(line, "-"):
		return styles.DiffRemove.Render(line)
	default:
		return line
	}
}

// renderTaskContent renders linked task info.
func (p *Plugin) renderTaskContent(width, height int) string {
	wt := p.selectedWorktree()
	if wt == nil {
		return dimText("No worktree selected")
	}

	if wt.TaskID == "" {
		return dimText("No linked task\nPress 't' to link a task")
	}

	// Check if we have cached details for this task
	if p.cachedTask == nil || p.cachedTaskID != wt.TaskID {
		return dimText(fmt.Sprintf("Loading task %s...", wt.TaskID))
	}

	task := p.cachedTask
	var lines []string

	// Header
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Task: %s", task.ID)))

	// Status and priority
	statusLine := fmt.Sprintf("Status: %s", task.Status)
	if task.Priority != "" {
		statusLine += fmt.Sprintf("  Priority: %s", task.Priority)
	}
	if task.Type != "" {
		statusLine += fmt.Sprintf("  Type: %s", task.Type)
	}
	lines = append(lines, statusLine)
	lines = append(lines, strings.Repeat("─", min(width-4, 60)))
	lines = append(lines, "")

	// Title
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(task.Title))
	lines = append(lines, "")

	// Description (word wrap to width)
	if task.Description != "" {
		wrapped := wrapText(task.Description, width-4)
		lines = append(lines, wrapped)
		lines = append(lines, "")
	}

	// Acceptance criteria
	if task.Acceptance != "" {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Acceptance Criteria:"))
		wrapped := wrapText(task.Acceptance, width-4)
		lines = append(lines, wrapped)
		lines = append(lines, "")
	}

	// Timestamps (dimmed)
	if task.CreatedAt != "" {
		lines = append(lines, dimText(fmt.Sprintf("Created: %s", task.CreatedAt)))
	}
	if task.UpdatedAt != "" {
		lines = append(lines, dimText(fmt.Sprintf("Updated: %s", task.UpdatedAt)))
	}

	return strings.Join(lines, "\n")
}

// wrapText wraps text to the specified width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if len(para) <= width {
			lines = append(lines, para)
			continue
		}

		// Simple word wrapping
		words := strings.Fields(para)
		var currentLine string
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
	}
	return strings.Join(lines, "\n")
}

// renderCreateModal renders the new worktree modal with dimmed background.
func (p *Plugin) renderCreateModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	// Modal dimensions
	modalW := 50
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width:
	// - modalStyle has border (2) + padding (4) = 6 chars
	// - inputStyle has border (2) + padding (2) = 4 chars
	// - So input content width = modalW - 6 (modal chrome) - 4 (input chrome) = modalW - 10
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Create New Worktree"))
	sb.WriteString("\n\n")

	// Name field
	nameLabel := "Name:"
	nameStyle := inputStyle
	if p.createFocus == 0 {
		nameStyle = inputFocusedStyle
	}
	nameValue := p.createName
	if nameValue == "" {
		nameValue = " "
	}
	sb.WriteString(nameLabel)
	sb.WriteString("\n")
	sb.WriteString(nameStyle.Width(inputW).Render(nameValue))
	sb.WriteString("\n\n")

	// Base branch field
	baseLabel := "Base Branch (default: current):"
	baseStyle := inputStyle
	if p.createFocus == 1 {
		baseStyle = inputFocusedStyle
	}
	baseValue := p.createBaseBranch
	if baseValue == "" {
		baseValue = " "
	}
	sb.WriteString(baseLabel)
	sb.WriteString("\n")
	sb.WriteString(baseStyle.Width(inputW).Render(baseValue))
	sb.WriteString("\n\n")

	// Task ID field with search dropdown
	taskLabel := "Link Task (optional):"
	taskStyle := inputStyle
	if p.createFocus == 2 {
		taskStyle = inputFocusedStyle
	}
	// Show selected task ID or search query
	taskValue := p.createTaskID
	if taskValue == "" {
		if p.taskSearchQuery != "" {
			taskValue = p.taskSearchQuery
		} else {
			taskValue = " "
		}
	}
	sb.WriteString(taskLabel)
	sb.WriteString("\n")
	sb.WriteString(taskStyle.Width(inputW).Render(taskValue))

	// Show task dropdown when focused and has results
	if p.createFocus == 2 {
		if p.taskSearchLoading {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Loading tasks..."))
		} else if len(p.taskSearchFiltered) > 0 {
			maxDropdown := 5
			dropdownCount := min(maxDropdown, len(p.taskSearchFiltered))
			for i := 0; i < dropdownCount; i++ {
				task := p.taskSearchFiltered[i]
				prefix := "  "
				if i == p.taskSearchIdx {
					prefix = "> "
				}
				// Truncate title to fit
				title := task.Title
				maxTitle := modalW - 18 // Account for prefix, ID, spacing
				if len(title) > maxTitle {
					title = title[:maxTitle-3] + "..."
				}
				line := fmt.Sprintf("%s%s  %s", prefix, task.ID, title)
				sb.WriteString("\n")
				if i == p.taskSearchIdx {
					sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
				} else {
					sb.WriteString(dimText(line))
				}
			}
			if len(p.taskSearchFiltered) > maxDropdown {
				sb.WriteString("\n")
				sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchFiltered)-maxDropdown)))
			}
		} else if p.taskSearchQuery != "" {
			sb.WriteString("\n")
			sb.WriteString(dimText("  No matching tasks"))
		} else if len(p.taskSearchAll) == 0 {
			sb.WriteString("\n")
			sb.WriteString(dimText("  No open tasks found"))
		} else {
			// Show hint when no query
			sb.WriteString("\n")
			sb.WriteString(dimText("  Type to search, ↑/↓ to navigate"))
		}
	}
	sb.WriteString("\n\n")

	// Buttons - Create and Cancel
	createBtnStyle := styles.Button
	cancelBtnStyle := styles.Button
	if p.createFocus == 3 {
		createBtnStyle = styles.ButtonFocused
	}
	if p.createFocus == 4 {
		cancelBtnStyle = styles.ButtonFocused
	}
	sb.WriteString(createBtnStyle.Render(" Create "))
	sb.WriteString("  ")
	sb.WriteString(cancelBtnStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderTaskLinkModal renders the task link modal for existing worktrees with dimmed background.
func (p *Plugin) renderTaskLinkModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	// Modal dimensions
	modalW := 50
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width (same as renderCreateModal)
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	var sb strings.Builder
	title := "Link Task"
	if p.linkingWorktree != nil {
		title = fmt.Sprintf("Link Task to %s", p.linkingWorktree.Name)
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")

	// Search field
	searchLabel := "Search tasks:"
	searchStyle := inputFocusedStyle
	searchValue := p.taskSearchQuery
	if searchValue == "" {
		searchValue = " "
	}
	sb.WriteString(searchLabel)
	sb.WriteString("\n")
	sb.WriteString(searchStyle.Width(inputW).Render(searchValue))

	// Task dropdown
	if p.taskSearchLoading {
		sb.WriteString("\n")
		sb.WriteString(dimText("  Loading tasks..."))
	} else if len(p.taskSearchFiltered) > 0 {
		maxDropdown := 8
		dropdownCount := min(maxDropdown, len(p.taskSearchFiltered))
		for i := 0; i < dropdownCount; i++ {
			task := p.taskSearchFiltered[i]
			prefix := "  "
			if i == p.taskSearchIdx {
				prefix = "> "
			}
			// Truncate title to fit
			taskTitle := task.Title
			maxTitle := modalW - 18
			if len(taskTitle) > maxTitle {
				taskTitle = taskTitle[:maxTitle-3] + "..."
			}
			line := fmt.Sprintf("%s%s  %s", prefix, task.ID, taskTitle)
			sb.WriteString("\n")
			if i == p.taskSearchIdx {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
			} else {
				sb.WriteString(dimText(line))
			}
		}
		if len(p.taskSearchFiltered) > maxDropdown {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchFiltered)-maxDropdown)))
		}
	} else if p.taskSearchQuery != "" {
		sb.WriteString("\n")
		sb.WriteString(dimText("  No matching tasks"))
	} else if len(p.taskSearchAll) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimText("  No open tasks found"))
	} else {
		// Show all tasks when no query
		maxDropdown := 8
		dropdownCount := min(maxDropdown, len(p.taskSearchAll))
		for i := 0; i < dropdownCount; i++ {
			task := p.taskSearchAll[i]
			prefix := "  "
			if i == p.taskSearchIdx {
				prefix = "> "
			}
			taskTitle := task.Title
			maxTitle := modalW - 18
			if len(taskTitle) > maxTitle {
				taskTitle = taskTitle[:maxTitle-3] + "..."
			}
			line := fmt.Sprintf("%s%s  %s", prefix, task.ID, taskTitle)
			sb.WriteString("\n")
			if i == p.taskSearchIdx {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
			} else {
				sb.WriteString(dimText(line))
			}
		}
		if len(p.taskSearchAll) > maxDropdown {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchAll)-maxDropdown)))
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(dimText("↑/↓ navigate  Enter select  Esc cancel"))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// dimText renders dim placeholder text using theme style.
func dimText(s string) string {
	return styles.Muted.Render(s)
}

// renderMergeModal renders the merge workflow modal with dimmed background.
func (p *Plugin) renderMergeModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.mergeState == nil {
		return background
	}

	// Modal dimensions
	modalW := 70
	if modalW > width-4 {
		modalW = width - 4
	}
	modalH := height - 6
	if modalH < 20 {
		modalH = 20
	}

	var sb strings.Builder

	// Title
	title := fmt.Sprintf("Merge Workflow: %s", p.mergeState.Worktree.Name)
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")

	// Progress indicators
	steps := []MergeWorkflowStep{
		MergeStepReviewDiff,
		MergeStepPush,
		MergeStepCreatePR,
		MergeStepWaitingMerge,
		MergeStepCleanup,
	}

	for _, step := range steps {
		status := p.mergeState.StepStatus[step]
		icon := "○" // pending
		color := lipgloss.Color("240")

		switch status {
		case "running":
			icon = "●"
			color = lipgloss.Color("214") // yellow
		case "done":
			icon = "✓"
			color = lipgloss.Color("42") // green
		case "error":
			icon = "✗"
			color = lipgloss.Color("196") // red
		}

		// Highlight current step
		stepName := step.String()
		if step == p.mergeState.Step {
			stepName = lipgloss.NewStyle().Bold(true).Render(stepName)
		}

		stepLine := fmt.Sprintf("  %s %s",
			lipgloss.NewStyle().Foreground(color).Render(icon),
			stepName,
		)
		sb.WriteString(stepLine)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", min(modalW-4, 60)))
	sb.WriteString("\n\n")

	// Step-specific content
	switch p.mergeState.Step {
	case MergeStepReviewDiff:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Diff Summary:"))
		sb.WriteString("\n\n")
		if p.mergeState.DiffSummary != "" {
			// Truncate to fit modal
			summaryLines := strings.Split(p.mergeState.DiffSummary, "\n")
			maxLines := modalH - 15 // Account for header, progress, footer
			if maxLines < 5 {
				maxLines = 5
			}
			if len(summaryLines) > maxLines {
				summaryLines = summaryLines[:maxLines]
				summaryLines = append(summaryLines, fmt.Sprintf("... (%d more lines)", len(strings.Split(p.mergeState.DiffSummary, "\n"))-maxLines))
			}
			for _, line := range summaryLines {
				sb.WriteString(colorDiffLine(line, modalW-4))
				sb.WriteString("\n")
			}
		} else {
			sb.WriteString(dimText("Loading diff..."))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(dimText("Press Enter to push branch, Esc to cancel"))

	case MergeStepPush:
		sb.WriteString("Pushing branch to remote...")

	case MergeStepCreatePR:
		sb.WriteString("Creating pull request...")

	case MergeStepWaitingMerge:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Pull Request Created"))
		sb.WriteString("\n\n")
		if p.mergeState.PRURL != "" {
			sb.WriteString(fmt.Sprintf("URL: %s", p.mergeState.PRURL))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString("Waiting for PR to be merged...")
		sb.WriteString("\n")
		sb.WriteString(dimText("Checking status every 30 seconds"))
		sb.WriteString("\n\n")
		sb.WriteString(dimText("Press Enter to check now, 'c' to skip to cleanup"))

	case MergeStepCleanup:
		sb.WriteString("Cleaning up worktree and branch...")

	case MergeStepDone:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Render("✓ Merge workflow complete!"))
		sb.WriteString("\n\n")
		sb.WriteString("Worktree and branch have been cleaned up.")
		sb.WriteString("\n\n")
		sb.WriteString(dimText("Press Enter to close"))
	}

	// Show error if any
	if p.mergeState.Error != nil {
		sb.WriteString("\n\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(
			fmt.Sprintf("Error: %s", p.mergeState.Error.Error())))
	}

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// formatRelativeTime formats a time as relative (e.g., "3m", "2h").
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
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

// renderKanbanView renders the kanban board view.
func (p *Plugin) renderKanbanView(width, height int) string {
	// Check minimum width - auto-collapse to list view if too narrow
	minKanbanWidth := 80
	if width < minKanbanWidth {
		// Fall back to list view when too narrow
		return p.renderListView(width, height)
	}

	var lines []string

	// Header with view mode toggle
	header := styles.Title.Render("Worktrees")
	listTab := "List"
	kanbanTab := "[Kanban]"
	viewToggle := styles.Muted.Render(listTab + "|" + kanbanTab)
	headerLine := header + strings.Repeat(" ", max(1, width-len("Worktrees")-len(listTab)-len(kanbanTab)-1)) + viewToggle
	lines = append(lines, headerLine)
	lines = append(lines, strings.Repeat("─", width))

	// Group worktrees by status
	columns := map[WorktreeStatus][]*Worktree{
		StatusActive:  {},
		StatusWaiting: {},
		StatusDone:    {},
		StatusPaused:  {},
		StatusError:   {},
	}
	for _, wt := range p.worktrees {
		columns[wt.Status] = append(columns[wt.Status], wt)
	}

	// Column headers and order
	columnOrder := []WorktreeStatus{StatusActive, StatusWaiting, StatusDone, StatusPaused}
	columnTitles := map[WorktreeStatus]string{
		StatusActive:  "● Active",
		StatusWaiting: "💬 Waiting",
		StatusDone:    "✓ Done",
		StatusPaused:  "⏸ Paused",
		StatusError:   "✗ Error",
	}

	// Calculate column widths
	numCols := len(columnOrder)
	colWidth := (width - numCols - 1) / numCols // -1 for separators
	if colWidth < 15 {
		colWidth = 15
	}

	// Render column headers
	var colHeaders []string
	for _, status := range columnOrder {
		items := columns[status]
		title := fmt.Sprintf("%s (%d)", columnTitles[status], len(items))
		colHeaders = append(colHeaders, styles.Title.Width(colWidth).Render(title))
	}
	lines = append(lines, strings.Join(colHeaders, "│"))
	lines = append(lines, strings.Repeat("─", width))

	// Calculate content height
	contentHeight := height - 4 // header + 2 separators + column headers
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Render column content
	for row := 0; row < contentHeight; row++ {
		var rowCells []string
		for _, status := range columnOrder {
			items := columns[status]
			cell := ""
			if row < len(items) {
				wt := items[row]
				// Truncate name to fit column
				name := wt.Name
				if len(name) > colWidth-4 {
					name = name[:colWidth-7] + "..."
				}
				cell = fmt.Sprintf(" %s %s", wt.Status.Icon(), name)
			}
			rowCells = append(rowCells, styles.ListItemNormal.Width(colWidth).Render(cell))
		}
		lines = append(lines, strings.Join(rowCells, "│"))
	}

	// Pad to fill height
	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines[:height], "\n")
}


package worktree

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
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

	// Header
	header := styles.Title.Render("Worktrees")
	lines = append(lines, header)
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
	isSelected := selected && p.activePane == PaneSidebar

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
		return styles.ListItemSelected.Width(width).Render(content)
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

// renderPreviewContent renders the preview pane content (no borders).
func (p *Plugin) renderPreviewContent(width, height int) string {
	var lines []string

	// Hide tabs when no worktree is selected - show tmux guide instead
	wt := p.selectedWorktree()
	if wt == nil {
		return truncateAllLines(p.renderTmuxGuide(width, height), width)
	}

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

	// Final safety: ensure ALL lines are truncated to width
	// This catches any content that wasn't properly truncated
	result := strings.Join(lines, "\n")
	return truncateAllLines(result, width)
}

// renderTmuxGuide renders a helpful tmux guide when no worktree is selected.
func (p *Plugin) renderTmuxGuide(width, height int) string {
	var lines []string

	// Title
	title := lipgloss.NewStyle().Bold(true).Render("tmux Quick Reference")
	lines = append(lines, title)
	lines = append(lines, "")

	// Section: Attaching to agent sessions
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	lines = append(lines, sectionStyle.Render("Agent Sessions"))
	lines = append(lines, dimText("  Enter      Attach to selected worktree session"))
	lines = append(lines, dimText("  Ctrl-b d   Detach from session (return here)"))
	lines = append(lines, "")

	// Section: Navigation inside tmux
	lines = append(lines, sectionStyle.Render("Scrolling (in attached session)"))
	lines = append(lines, dimText("  Ctrl-b [        Enter scroll mode"))
	lines = append(lines, dimText("  PgUp/PgDn       Scroll page (fn+↑/↓ on Mac)"))
	lines = append(lines, dimText("  ↑/↓             Scroll line by line"))
	lines = append(lines, dimText("  q               Exit scroll mode"))
	lines = append(lines, "")

	// Section: Interacting with editors
	lines = append(lines, sectionStyle.Render("Editor Navigation"))
	lines = append(lines, dimText("  When agent opens vim/nano:"))
	lines = append(lines, dimText("    :q!      Quit vim without saving"))
	lines = append(lines, dimText("    :wq      Save and quit vim"))
	lines = append(lines, dimText("    Ctrl-x   Exit nano"))
	lines = append(lines, "")

	// Section: Common tasks
	lines = append(lines, sectionStyle.Render("Tips"))
	lines = append(lines, dimText("  • Create a worktree with 'n' to start"))
	lines = append(lines, dimText("  • Agent output streams in the Output tab"))
	lines = append(lines, dimText("  • Attach to interact with the agent directly"))
	lines = append(lines, "")
	lines = append(lines, dimText("Customize tmux: ~/.tmux.conf (man tmux for options)"))

	return strings.Join(lines, "\n")
}

// truncateAllLines ensures every line in the content is truncated to maxWidth.
func truncateAllLines(content string, maxWidth int) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = expandTabs(line, tabStopWidth)
		if lipgloss.Width(line) > maxWidth {
			line = ansi.Truncate(line, maxWidth, "")
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// expandTabs replaces tabs with spaces, preserving ANSI sequences and column widths.
func expandTabs(line string, tabWidth int) string {
	if tabWidth <= 0 || !strings.Contains(line, "\t") {
		return line
	}

	var sb strings.Builder
	sb.Grow(len(line))

	state := ansi.NormalState
	column := 0
	for len(line) > 0 {
		seq, width, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(line, state, nil)
		if n <= 0 {
			sb.WriteString(line)
			break
		}
		if seq == "\t" && width == 0 {
			spaces := tabWidth - (column % tabWidth)
			if spaces == 0 {
				spaces = tabWidth
			}
			sb.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		} else {
			sb.WriteString(seq)
			column += width
		}
		state = newState
		line = line[n:]
	}

	return sb.String()
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
		return dimText("No agent running\nPress 's' to start an agent")
	}

	// Hint for tmux detach
	hint := dimText("enter to attach • Ctrl-b d to detach")
	height-- // Reserve line for hint

	if wt.Agent.OutputBuf == nil {
		return hint + "\n" + dimText("No output yet")
	}

	lines := wt.Agent.OutputBuf.Lines()
	if len(lines) == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	var start, end int
	if p.autoScrollOutput {
		// Auto-scroll: show newest content (last height lines)
		start = len(lines) - height
		if start < 0 {
			start = 0
		}
		end = len(lines)
	} else {
		// Manual scroll: previewOffset is lines from bottom
		// offset=0 means bottom, offset=N means N lines up from bottom
		start = len(lines) - height - p.previewOffset
		if start < 0 {
			start = 0
		}
		end = start + height
		if end > len(lines) {
			end = len(lines)
		}
	}

	// Apply horizontal offset and truncate each line
	var displayLines []string
	for _, line := range lines[start:end] {
		displayLine := expandTabs(line, tabStopWidth)
		// Apply horizontal offset using ANSI-aware truncation
		if p.previewHorizOffset > 0 {
			displayLine = ansi.TruncateLeft(displayLine, p.previewHorizOffset, "")
		}
		// Truncate to width if needed
		if lipgloss.Width(displayLine) > width {
			displayLine = ansi.Truncate(displayLine, width, "")
		}
		displayLines = append(displayLines, displayLine)
	}

	return hint + "\n" + strings.Join(displayLines, "\n")
}

// renderDiffContent renders git diff using the shared diff renderer.
func (p *Plugin) renderDiffContent(width, height int) string {
	if p.diffRaw == "" {
		wt := p.selectedWorktree()
		if wt == nil {
			return dimText("No worktree selected")
		}
		return dimText("No changes")
	}

	// Parse the raw diff into structured format
	parsed, err := gitstatus.ParseUnifiedDiff(p.diffRaw)
	if err != nil || parsed == nil {
		// Fallback to basic rendering
		return p.renderDiffContentBasic(width, height)
	}

	// Create syntax highlighter if we have file info
	var highlighter *gitstatus.SyntaxHighlighter
	if parsed.NewFile != "" {
		highlighter = gitstatus.NewSyntaxHighlighter(parsed.NewFile)
	}

	// Render based on view mode
	var content string
	if p.diffViewMode == DiffViewSideBySide {
		content = gitstatus.RenderSideBySide(parsed, width, p.previewOffset, height, p.previewHorizOffset, highlighter)
	} else {
		content = gitstatus.RenderLineDiff(parsed, width, p.previewOffset, height, p.previewHorizOffset, highlighter)
	}

	return content
}

// renderDiffContentBasic renders git diff with basic highlighting (fallback).
func (p *Plugin) renderDiffContentBasic(width, height int) string {
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

	// Diff highlighting with horizontal scroll support
	var rendered []string
	for _, line := range lines[start:end] {
		line = expandTabs(line, tabStopWidth)
		var styledLine string
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			styledLine = styles.DiffHeader.Render(line)
		case strings.HasPrefix(line, "@@"):
			styledLine = lipgloss.NewStyle().Foreground(styles.Info).Render(line)
		case strings.HasPrefix(line, "+"):
			styledLine = styles.DiffAdd.Render(line)
		case strings.HasPrefix(line, "-"):
			styledLine = styles.DiffRemove.Render(line)
		default:
			styledLine = line
		}

		if p.previewHorizOffset > 0 {
			styledLine = ansi.TruncateLeft(styledLine, p.previewHorizOffset, "")
		}
		if lipgloss.Width(styledLine) > width {
			styledLine = ansi.Truncate(styledLine, width, "")
		}
		rendered = append(rendered, styledLine)
	}

	return strings.Join(rendered, "\n")
}

// colorDiffLine applies basic diff coloring using theme styles.
func colorDiffLine(line string, width int) string {
	line = expandTabs(line, tabStopWidth)
	if len(line) == 0 {
		return line
	}

	// Truncate if needed
	if lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "")
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

	// Modal dimensions - increased for better task suggestion display
	modalW := 70
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width:
	// - modalStyle has border (2) + padding (4) = 6 chars
	// - inputStyle has border (2) + padding (2) = 4 chars
	// - So textinput content width = modalW - 6 (modal) - 4 (input style)
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	// Set textinput widths and remove default prompt
	p.createNameInput.Width = inputW
	p.createNameInput.Prompt = ""
	p.createBaseBranchInput.Width = inputW
	p.createBaseBranchInput.Prompt = ""
	p.taskSearchInput.Width = inputW
	p.taskSearchInput.Prompt = ""

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Create New Worktree"))
	sb.WriteString("\n\n")

	// Name field - use textinput.View() for proper cursor rendering
	nameLabel := "Name:"
	nameStyle := inputStyle
	if p.createFocus == 0 {
		nameStyle = inputFocusedStyle
	}
	sb.WriteString(nameLabel)
	sb.WriteString("\n")
	sb.WriteString(nameStyle.Render(p.createNameInput.View()))
	sb.WriteString("\n\n")

	// Base branch field with autocomplete
	baseLabel := "Base Branch (default: current):"
	baseStyle := inputStyle
	if p.createFocus == 1 {
		baseStyle = inputFocusedStyle
	}
	sb.WriteString(baseLabel)
	sb.WriteString("\n")
	sb.WriteString(baseStyle.Render(p.createBaseBranchInput.View()))

	// Show branch dropdown when focused and has branches
	if p.createFocus == 1 && len(p.branchFiltered) > 0 {
		maxDropdown := 5
		dropdownCount := min(maxDropdown, len(p.branchFiltered))
		for i := 0; i < dropdownCount; i++ {
			branch := p.branchFiltered[i]
			prefix := "  "
			if i == p.branchIdx {
				prefix = "> "
			}
			// Truncate branch name if needed
			if len(branch) > modalW-10 {
				branch = branch[:modalW-13] + "..."
			}
			line := prefix + branch
			sb.WriteString("\n")
			if i == p.branchIdx {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
			} else {
				sb.WriteString(dimText(line))
			}
		}
		if len(p.branchFiltered) > maxDropdown {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.branchFiltered)-maxDropdown)))
		}
	} else if p.createFocus == 1 && len(p.branchAll) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimText("  Loading branches..."))
	}
	sb.WriteString("\n\n")

	// Prompt field (focus=2)
	promptLabel := "Prompt:"
	promptStyle := inputStyle
	if p.createFocus == 2 {
		promptStyle = inputFocusedStyle
	}
	sb.WriteString(promptLabel)
	sb.WriteString("\n")

	// Get selected prompt
	selectedPrompt := p.getSelectedPrompt()
	if len(p.createPrompts) == 0 {
		// No prompts configured
		sb.WriteString(promptStyle.Render("No prompts configured"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  See: docs/guides/creating-prompts.md"))
	} else if selectedPrompt == nil {
		// None selected
		sb.WriteString(promptStyle.Render("(none)"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  Press Enter to select a prompt template"))
	} else {
		// Show selected prompt with scope indicator
		scopeIndicator := "[G] global"
		if selectedPrompt.Source == "project" {
			scopeIndicator = "[P] project"
		}
		displayText := fmt.Sprintf("%s  %s", selectedPrompt.Name, dimText(scopeIndicator))
		sb.WriteString(promptStyle.Render(displayText))
		sb.WriteString("\n")
		// Show preview of prompt body (first ~60 chars, single line)
		preview := strings.ReplaceAll(selectedPrompt.Body, "\n", " ")
		if runes := []rune(preview); len(runes) > 60 {
			preview = string(runes[:57]) + "..."
		}
		sb.WriteString(dimText(fmt.Sprintf("  Preview: %s", preview)))
	}
	sb.WriteString("\n\n")

	// Task ID field with search dropdown (focus=3)
	// Handle ticketMode: none - show disabled message instead of input
	if selectedPrompt != nil && selectedPrompt.TicketMode == TicketNone {
		sb.WriteString(dimText("Ticket: (not allowed by selected prompt)"))
	} else {
		// Dynamic label based on selected prompt's ticketMode
		var taskLabel string
		if selectedPrompt != nil {
			switch selectedPrompt.TicketMode {
			case TicketRequired:
				taskLabel = "Link Task (required by selected prompt):"
			case TicketOptional:
				taskLabel = "Link Task (optional for selected prompt):"
			default:
				taskLabel = "Link Task (optional):"
			}
		} else {
			taskLabel = "Link Task (optional):"
		}

		taskStyle := inputStyle
		if p.createFocus == 3 {
			taskStyle = inputFocusedStyle
		}
		sb.WriteString(taskLabel)
		sb.WriteString("\n")
		// If task already selected, show ID and title; otherwise show the search input
		if p.createTaskID != "" {
			// Show task ID and title, truncate title if needed
			display := p.createTaskID
			if p.createTaskTitle != "" {
				title := p.createTaskTitle
				maxTitle := inputW - len(p.createTaskID) - 3 // Account for ": " separator
				if maxTitle > 10 && len(title) > maxTitle {
					title = title[:maxTitle-3] + "..."
				}
				if maxTitle > 10 {
					display = fmt.Sprintf("%s: %s", p.createTaskID, title)
				}
			}
			sb.WriteString(taskStyle.Render(display))
		} else {
			sb.WriteString(taskStyle.Render(p.taskSearchInput.View()))
		}

		// Show hint when task is selected and focused
		if p.createFocus == 3 && p.createTaskID != "" {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Backspace to clear"))
		}

		// Show fallback hint for optional tickets
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketOptional && p.createTaskID == "" {
			fallback := ExtractFallback(selectedPrompt.Body)
			if fallback != "" {
				sb.WriteString("\n")
				sb.WriteString(dimText(fmt.Sprintf("  Default if empty: \"%s\"", fallback)))
			}
		}

		// Show tip for required tickets
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketRequired {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Tip: ticket is passed as an ID only. The agent fetches via td."))
		}

		// Show task dropdown when focused and has results
		if p.createFocus == 3 && p.createTaskID == "" {
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
					// Truncate title based on available width
					// Account for: prefix(2) + task.ID(~12) + spacing(2) + modal padding(6)
					title := task.Title
					idWidth := len(task.ID)
					maxTitle := modalW - idWidth - 10
					if maxTitle < 10 {
						maxTitle = 10
					}
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
			} else if p.taskSearchInput.Value() != "" {
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
	}
	sb.WriteString("\n\n")

	// Agent Selection (radio buttons) - focus=4
	sb.WriteString("Agent:")
	sb.WriteString("\n")
	for _, at := range AgentTypeOrder {
		prefix := "  ○ "
		if at == p.createAgentType {
			prefix = "  ● "
		}
		name := AgentDisplayNames[at]
		line := prefix + name

		if p.createFocus == 4 && at == p.createAgentType {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
		} else if at == p.createAgentType {
			sb.WriteString(line)
		} else {
			sb.WriteString(dimText(line))
		}
		sb.WriteString("\n")
	}

	// Skip Permissions Checkbox (only show when agent is selected and supports it) - focus=5
	if p.createAgentType != AgentNone {
		flag := SkipPermissionsFlags[p.createAgentType]
		if flag != "" {
			sb.WriteString("\n")
			checkBox := "[ ]"
			if p.createSkipPermissions {
				checkBox = "[x]"
			}
			skipLine := fmt.Sprintf("  %s Auto-approve all actions", checkBox)

			if p.createFocus == 5 {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(skipLine))
			} else {
				sb.WriteString(skipLine)
			}
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("      (Adds %s)", flag)))
			sb.WriteString("\n")
		} else {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Skip permissions not available for this agent"))
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	// Display error if present
	if p.createError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		sb.WriteString(errStyle.Render("Error: " + p.createError))
		sb.WriteString("\n\n")
	}

	// Buttons - Create and Cancel (focus=6 and focus=7)
	createBtnStyle := styles.Button
	cancelBtnStyle := styles.Button
	if p.createFocus == 6 {
		createBtnStyle = styles.ButtonFocused
	} else if p.createButtonHover == 1 {
		createBtnStyle = styles.ButtonHover
	}
	if p.createFocus == 7 {
		cancelBtnStyle = styles.ButtonFocused
	} else if p.createButtonHover == 2 {
		cancelBtnStyle = styles.ButtonHover
	}
	sb.WriteString(createBtnStyle.Render(" Create "))
	sb.WriteString("  ")
	sb.WriteString(cancelBtnStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Calculate modal position for hit regions
	modalH := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalH) / 2

	// Register hit regions for interactive elements
	// modalStyle has border(1) + padding(1) = 2 rows offset from modalY
	// Track Y position through modal content structure
	hitX := modalX + 3 // border + padding for left edge
	hitW := modalW - 6 // width minus border+padding on both sides
	currentY := modalY + 2

	// Title "Create New Worktree" + blank
	currentY += 2

	// Name field (focus=0): label + input + blank
	currentY++ // "Name:" label
	p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 1, 0)
	currentY++ // input line
	currentY++ // blank

	// Base Branch field (focus=1): label + input
	currentY++ // "Base Branch..." label
	p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 1, 1)
	currentY++ // input line

	// Branch dropdown (if visible)
	if p.createFocus == 1 && len(p.branchFiltered) > 0 {
		maxDropdown := 5
		dropdownCount := min(maxDropdown, len(p.branchFiltered))
		for i := 0; i < dropdownCount; i++ {
			p.mouseHandler.HitMap.AddRect(regionCreateDropdown, hitX, currentY, hitW, 1, dropdownItemData{field: 1, idx: i})
			currentY++
		}
		if len(p.branchFiltered) > maxDropdown {
			currentY++ // "... and N more"
		}
	} else if p.createFocus == 1 && len(p.branchAll) == 0 {
		currentY++ // "Loading branches..."
	}
	currentY++ // blank

	// Prompt field (focus=2): label + display + preview hint + blank
	currentY++ // "Prompt:" label
	p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 1, 2)
	currentY++ // prompt display
	currentY++ // preview/hint line
	currentY++ // blank

	// Task field (focus=3) - only shown when ticketMode allows
	// Note: selectedPrompt already declared at line 852
	if selectedPrompt == nil || selectedPrompt.TicketMode != TicketNone {
		currentY++ // "Link Task..." label
		p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 1, 3)
		currentY++ // input line

		// Task hints (backspace hint, fallback hint, or required hint)
		if p.createFocus == 3 && p.createTaskID != "" {
			currentY++ // "Backspace to clear"
		}
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketOptional && p.createTaskID == "" {
			fallback := ExtractFallback(selectedPrompt.Body)
			if fallback != "" {
				currentY++ // Default hint
			}
		}
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketRequired {
			currentY++ // Tip line
		}

		// Task dropdown (if visible)
		if p.createFocus == 3 && p.createTaskID == "" {
			if p.taskSearchLoading {
				currentY++ // "Loading tasks..."
			} else if len(p.taskSearchFiltered) > 0 {
				maxDropdown := 5
				dropdownCount := min(maxDropdown, len(p.taskSearchFiltered))
				for i := 0; i < dropdownCount; i++ {
					p.mouseHandler.HitMap.AddRect(regionCreateDropdown, hitX, currentY, hitW, 1, dropdownItemData{field: 3, idx: i})
					currentY++
				}
				if len(p.taskSearchFiltered) > maxDropdown {
					currentY++ // "... and N more"
				}
			} else if p.taskSearchInput.Value() != "" {
				currentY++ // "No matching tasks"
			} else if len(p.taskSearchAll) == 0 {
				currentY++ // "No open tasks found"
			} else {
				currentY++ // "Type to search..." hint
			}
		}
	} else {
		currentY++ // "Ticket: (not allowed...)"
	}
	currentY += 2 // blank lines

	// Agent options (focus=4): "Agent:" + agent options
	currentY++ // "Agent:" label
	for i := range AgentTypeOrder {
		p.mouseHandler.HitMap.AddRect(regionCreateAgentOption, hitX, currentY, hitW, 1, i)
		currentY++
	}

	// Skip permissions checkbox (focus=5) - only when agent supports it
	if p.createAgentType != AgentNone {
		flag := SkipPermissionsFlags[p.createAgentType]
		if flag != "" {
			currentY++ // blank before checkbox
			p.mouseHandler.HitMap.AddRect(regionCreateCheckbox, hitX, currentY, hitW, 1, 5)
			currentY++ // checkbox line
			currentY++ // "(Adds --dangerously...)" hint
		} else {
			currentY++ // blank
			currentY++ // "Skip permissions not available..."
		}
	}
	currentY++ // blank

	// Error display (if present)
	if p.createError != "" {
		currentY += 2 // error line + blank
	}

	// Buttons (focus=6 create, focus=7 cancel)
	p.mouseHandler.HitMap.AddRect(regionCreateButton, hitX, currentY, 12, 1, 6)
	p.mouseHandler.HitMap.AddRect(regionCreateButton, hitX+14, currentY, 12, 1, 7)

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderTaskLinkModal renders the task link modal for existing worktrees with dimmed background.
func (p *Plugin) renderTaskLinkModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	// Modal dimensions - increased for better task display
	modalW := 70
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width
	// - modalStyle has border (2) + padding (4) = 6 chars
	// - inputStyle has border (2) + padding (2) = 4 chars
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	// Set textinput width and remove default prompt
	p.taskSearchInput.Width = inputW
	p.taskSearchInput.Prompt = ""

	var sb strings.Builder
	title := "Link Task"
	if p.linkingWorktree != nil {
		title = fmt.Sprintf("Link Task to %s", p.linkingWorktree.Name)
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")

	// Search field - use textinput.View() for proper cursor rendering
	searchLabel := "Search tasks:"
	searchStyle := inputFocusedStyle
	sb.WriteString(searchLabel)
	sb.WriteString("\n")
	sb.WriteString(searchStyle.Render(p.taskSearchInput.View()))

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
			// Truncate title based on available width
			taskTitle := task.Title
			idWidth := len(task.ID)
			maxTitle := modalW - idWidth - 10
			if maxTitle < 10 {
				maxTitle = 10
			}
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
	} else if p.taskSearchInput.Value() != "" {
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
			idWidth := len(task.ID)
			maxTitle := modalW - idWidth - 10
			if maxTitle < 10 {
				maxTitle = 10
			}
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

	// Calculate modal position for hit regions
	modalH := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalH) / 2

	// Register hit regions for task dropdown items
	// Modal layout: border(1) + padding(1) + title(1) + blank(1) + label(1) + input(1) = 6 lines before dropdown
	dropdownStartY := modalY + 2 + 4 + 1 // border+padding + title+blank+label+input

	// Determine which list to use for hit regions
	tasks := p.taskSearchFiltered
	if len(tasks) == 0 && p.taskSearchInput.Value() == "" && len(p.taskSearchAll) > 0 {
		tasks = p.taskSearchAll
	}

	if len(tasks) > 0 {
		maxDropdown := 8
		dropdownCount := min(maxDropdown, len(tasks))
		for i := 0; i < dropdownCount; i++ {
			p.mouseHandler.HitMap.AddRect(regionTaskLinkDropdown, modalX+2, dropdownStartY+i, modalW-6, 1, i)
		}
	}

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderConfirmDeleteModal renders the delete confirmation modal.
func (p *Plugin) renderConfirmDeleteModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.deleteConfirmWorktree == nil {
		return background
	}

	// Modal dimensions
	modalW := 55
	if modalW > width-4 {
		modalW = width - 4
	}

	wt := p.deleteConfirmWorktree

	var sb strings.Builder
	title := "Delete Worktree?"
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(title))
	sb.WriteString("\n\n")

	// Worktree name
	sb.WriteString(fmt.Sprintf("Name:   %s\n", lipgloss.NewStyle().Bold(true).Render(wt.Name)))
	sb.WriteString(fmt.Sprintf("Branch: %s\n", wt.Branch))
	sb.WriteString(fmt.Sprintf("Path:   %s\n", dimText(wt.Path)))
	sb.WriteString("\n")

	// Warning text
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	sb.WriteString(warningStyle.Render("This will:"))
	sb.WriteString("\n")
	sb.WriteString(dimText("  • Remove the working directory"))
	sb.WriteString("\n")
	sb.WriteString(dimText("  • Uncommitted changes will be lost"))
	sb.WriteString("\n")
	sb.WriteString(dimText("  • The branch will remain in the repository"))
	sb.WriteString("\n\n")

	// Render buttons with focus/hover states
	deleteStyle := styles.ButtonDanger
	cancelStyle := styles.Button
	if p.deleteConfirmButtonFocus == 0 {
		deleteStyle = styles.ButtonDangerFocused
	} else if p.deleteConfirmButtonHover == 1 {
		deleteStyle = styles.ButtonDangerHover
	}
	if p.deleteConfirmButtonFocus == 1 {
		cancelStyle = styles.ButtonFocused
	} else if p.deleteConfirmButtonHover == 2 {
		cancelStyle = styles.ButtonHover
	}
	sb.WriteString(deleteStyle.Render(" Delete "))
	sb.WriteString("  ")
	sb.WriteString(cancelStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Register hit regions for the modal buttons
	// Calculate modal position (centered)
	modalHeight := lipgloss.Height(modal)
	modalStartX := (width - modalW) / 2
	modalStartY := (height - modalHeight) / 2

	// Hit regions for buttons
	// border(1) + padding(1) + title(1) + empty(1) + name/branch/path(3) + empty(1) + warning header(1) + bullets(3) + empty(1) = 12 lines
	buttonY := modalStartY + 2 + 12 // border+padding + content lines
	deleteX := modalStartX + 3      // border + padding
	// " Delete " (8) + Padding(0,2) = 12 chars, " Cancel " (8) + Padding(0,2) = 12 chars
	p.mouseHandler.HitMap.AddRect(regionDeleteConfirmDelete, deleteX, buttonY, 12, 1, nil)
	cancelX := deleteX + 12 + 2 // delete width + spacing
	p.mouseHandler.HitMap.AddRect(regionDeleteConfirmCancel, cancelX, buttonY, 12, 1, nil)

	return ui.OverlayModal(background, modal, width, height)
}

// renderPromptPickerModal renders the prompt picker modal.
func (p *Plugin) renderPromptPickerModal(width, height int) string {
	// Render the background (create modal behind it)
	background := p.renderCreateModal(width, height)

	if p.promptPicker == nil {
		return background
	}

	// Modal dimensions
	modalW := 80
	if modalW > width-4 {
		modalW = width - 4
	}
	p.promptPicker.width = modalW
	p.promptPicker.height = height

	content := p.promptPicker.View()
	modal := modalStyle.Width(modalW).Render(content)

	// Calculate modal position for hit regions
	modalH := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalH) / 2

	// Register hit regions for prompt items
	// Layout: border(1) + padding(1) + header(2) + filter(3) + column headers(2) = 9 lines before items
	// "None" option is first, then filtered prompts
	itemStartY := modalY + 2 + 7 // border+padding + header + filter + col headers
	itemHeight := 1              // Each prompt item is 1 line

	// "None" option at index -1
	p.mouseHandler.HitMap.AddRect(regionPromptItem, modalX+2, itemStartY, modalW-6, itemHeight, -1)

	// Prompt items
	maxVisible := 10
	if len(p.promptPicker.filtered) > 0 {
		visibleCount := min(maxVisible, len(p.promptPicker.filtered))
		for i := 0; i < visibleCount; i++ {
			y := itemStartY + 1 + i // +1 for "none" row
			p.mouseHandler.HitMap.AddRect(regionPromptItem, modalX+2, y, modalW-6, itemHeight, i)
		}
	}

	return ui.OverlayModal(background, modal, width, height)
}

// renderAgentChoiceModal renders the agent action choice modal.
func (p *Plugin) renderAgentChoiceModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.agentChoiceWorktree == nil {
		return background
	}

	// Modal dimensions
	modalW := 50
	if modalW > width-4 {
		modalW = width - 4
	}

	var sb strings.Builder
	title := fmt.Sprintf("Agent Running: %s", p.agentChoiceWorktree.Name)
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")
	sb.WriteString("An agent is already running on this worktree.\n")
	sb.WriteString("What would you like to do?\n\n")

	options := []string{"Attach to session", "Restart agent"}
	for i, opt := range options {
		prefix := "  "
		selected := i == p.agentChoiceIdx && p.agentChoiceButtonFocus == 0
		if selected {
			prefix = "> "
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(prefix + opt))
		} else {
			sb.WriteString(dimText(prefix + opt))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// Render buttons with focus/hover states
	confirmStyle := styles.Button
	cancelStyle := styles.Button
	if p.agentChoiceButtonFocus == 1 {
		confirmStyle = styles.ButtonFocused
	} else if p.agentChoiceButtonHover == 1 {
		confirmStyle = styles.ButtonHover
	}
	if p.agentChoiceButtonFocus == 2 {
		cancelStyle = styles.ButtonFocused
	} else if p.agentChoiceButtonHover == 2 {
		cancelStyle = styles.ButtonHover
	}
	sb.WriteString(confirmStyle.Render(" Confirm "))
	sb.WriteString("  ")
	sb.WriteString(cancelStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Register hit regions for the modal
	// Calculate modal position (centered)
	modalHeight := lipgloss.Height(modal)
	modalStartX := (width - modalW) / 2
	modalStartY := (height - modalHeight) / 2

	// Hit regions for options (inside modal content area)
	// Modal border (1) + padding (1) = 2, plus title lines (2) + empty (1) + message (2) + empty (1) = 6
	optionY := modalStartY + 2 + 5 // border+padding + header lines
	optionX := modalStartX + 3     // border + padding + "  " prefix
	for i := range options {
		p.mouseHandler.HitMap.AddRect(regionAgentChoiceOption, optionX, optionY+i, modalW-6, 1, i)
	}

	// Hit regions for buttons
	// Buttons are after options (2) + empty (1) = 3 more lines
	buttonY := optionY + 3
	confirmX := modalStartX + 3 // border + padding
	// " Confirm " (9) + Padding(0,2) = 13 chars, " Cancel " (8) + Padding(0,2) = 12 chars
	p.mouseHandler.HitMap.AddRect(regionAgentChoiceConfirm, confirmX, buttonY, 13, 1, nil)
	cancelX := confirmX + 13 + 2 // confirm width + spacing
	p.mouseHandler.HitMap.AddRect(regionAgentChoiceCancel, cancelX, buttonY, 12, 1, nil)

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
		case "skipped":
			icon = "○"
			color = lipgloss.Color("240") // gray, same as pending
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
		sb.WriteString("Checking merge status every 30 seconds...")
		sb.WriteString("\n\n")
		sb.WriteString(strings.Repeat("─", min(modalW-4, 60)))
		sb.WriteString("\n\n")

		// Radio button selection
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("After merge:"))
		sb.WriteString("\n\n")

		// Option 1: Delete worktree (default)
		if p.mergeState.DeleteAfterMerge {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(" ● Delete worktree after merge"))
		} else {
			sb.WriteString(dimText(" ○ Delete worktree after merge"))
		}
		sb.WriteString("\n")

		// Option 2: Keep worktree
		if !p.mergeState.DeleteAfterMerge {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(" ● Keep worktree"))
		} else {
			sb.WriteString(dimText(" ○ Keep worktree"))
		}
		sb.WriteString("\n\n")

		sb.WriteString(dimText(" (This takes effect only once the PR is merged)"))
		sb.WriteString("\n\n")
		sb.WriteString(dimText("Enter: check now   Esc: exit   ↑/↓: change option"))

	case MergeStepCleanup:
		sb.WriteString("Cleaning up worktree and branch...")

	case MergeStepDone:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Render("✓ Merge workflow complete!"))
		sb.WriteString("\n\n")
		if p.mergeState.DeleteAfterMerge {
			sb.WriteString("Worktree and branch have been cleaned up.")
		} else {
			sb.WriteString("PR merged. Worktree kept for further work.")
		}
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

	// Register hit regions for radio buttons during MergeStepWaitingMerge
	if p.mergeState.Step == MergeStepWaitingMerge {
		modalH := lipgloss.Height(modal)
		modalX := (width - modalW) / 2
		modalY := (height - modalH) / 2

		// Radio buttons are at: border(1) + padding(1) + title(1) + blank(1) +
		// progress steps(5) + blank(1) + separator(1) + blank(2) + content...
		// In MergeStepWaitingMerge: header(2) + blank(1) + PR URL(2) + blank(1) +
		// separator(1) + blank(2) + "After merge:"(1) + blank(2) + radio1 + radio2
		// This is complex; use content line count approach
		contentLines := strings.Count(content, "\n")
		// Radio buttons are approximately at contentLines - 7 (from bottom)
		// "Delete worktree" and "Keep worktree" options
		radio1Y := modalY + 2 + contentLines - 7
		radio2Y := radio1Y + 1
		p.mouseHandler.HitMap.AddRect(regionMergeRadio, modalX+2, radio1Y, modalW-6, 1, 0) // Delete
		p.mouseHandler.HitMap.AddRect(regionMergeRadio, modalX+2, radio2Y, modalW-6, 1, 1) // Keep
	}

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderCommitForMergeModal renders the commit-before-merge modal.
func (p *Plugin) renderCommitForMergeModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.mergeCommitState == nil {
		return background
	}

	// Modal dimensions
	modalW := 60
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	// Set textinput width and remove default prompt
	p.mergeCommitMessageInput.Width = inputW
	p.mergeCommitMessageInput.Prompt = ""

	wt := p.mergeCommitState.Worktree

	var sb strings.Builder
	title := "Uncommitted Changes"
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Render(title))
	sb.WriteString("\n\n")

	// Worktree info
	sb.WriteString(fmt.Sprintf("Worktree: %s\n", lipgloss.NewStyle().Bold(true).Render(wt.Name)))
	sb.WriteString(fmt.Sprintf("Branch:   %s\n", wt.Branch))
	sb.WriteString("\n")

	// Change counts
	sb.WriteString("Changes to commit:\n")
	if p.mergeCommitState.StagedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • %d staged file(s)\n", p.mergeCommitState.StagedCount))
	}
	if p.mergeCommitState.ModifiedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • %d modified file(s)\n", p.mergeCommitState.ModifiedCount))
	}
	if p.mergeCommitState.UntrackedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • %d untracked file(s)\n", p.mergeCommitState.UntrackedCount))
	}
	sb.WriteString("\n")

	// Info message
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(infoStyle.Render("You must commit these changes before creating a PR."))
	sb.WriteString("\n")
	sb.WriteString(infoStyle.Render("All changes will be staged and committed."))
	sb.WriteString("\n\n")

	// Commit message field
	sb.WriteString("Commit message:\n")
	sb.WriteString(inputFocusedStyle.Render(p.mergeCommitMessageInput.View()))
	sb.WriteString("\n")

	// Display error if present
	if p.mergeCommitState.Error != "" {
		sb.WriteString("\n")
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		sb.WriteString(errStyle.Render("Error: " + p.mergeCommitState.Error))
	}

	sb.WriteString("\n\n")
	sb.WriteString(dimText("Enter to commit and continue • Esc to cancel"))

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
		StatusWaiting: "💬 Waiting",
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

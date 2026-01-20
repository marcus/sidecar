package worktree

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// renderPreviewContent renders the preview pane content (no borders).
func (p *Plugin) renderPreviewContent(width, height int) string {
	var lines []string

	// Show welcome guide only when no worktree AND no shell is selected
	wt := p.selectedWorktree()
	if wt == nil && !p.shellSelected {
		return p.truncateAllLines(p.renderWelcomeGuide(width, height), width)
	}

	// When shell is selected, show shell content directly without tabs
	// (Output/Diff/Task tabs are not relevant for the project shell)
	if p.shellSelected {
		content := p.renderShellOutput(width, height)
		return p.truncateAllLines(content, width)
	}

	// Tab header (only for worktrees, not shell)
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
	return p.truncateAllLines(result, width)
}

// renderWelcomeGuide renders a helpful guide when no worktree is selected.
func (p *Plugin) renderWelcomeGuide(width, height int) string {
	var lines []string

	// Section Style
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	// Check if tmux is installed
	if !isTmuxInstalled() {
		lines = append(lines, warningStyle.Render("⚠ tmux Required"))
		lines = append(lines, "")
		lines = append(lines, dimText("Worktrees and shell sessions require tmux to be installed."))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("Install tmux:"))
		lines = append(lines, dimText("  "+getTmuxInstallInstructions()))
		lines = append(lines, "")
		lines = append(lines, dimText("After installing, restart sidecar to use this feature."))
		return strings.Join(lines, "\n")
	}

	// Git Worktree Explanation
	lines = append(lines, sectionStyle.Render("Git Worktrees: A Better Workflow"))
	lines = append(lines, dimText("  • Parallel Development: Work on multiple branches simultaneously"))
	lines = append(lines, dimText("    in separate directories."))
	lines = append(lines, dimText("  • No Context Switching: Keep your editor/server running while"))
	lines = append(lines, dimText("    reviewing a PR or fixing a bug."))
	lines = append(lines, dimText("  • Isolated Environments: Each worktree has its own clean state,"))
	lines = append(lines, dimText("    unaffected by other changes."))
	lines = append(lines, "")
	lines = append(lines, strings.Repeat("─", min(width-4, 60)))
	lines = append(lines, "")

	// Title
	title := lipgloss.NewStyle().Bold(true).Render("tmux Quick Reference")
	lines = append(lines, title)
	lines = append(lines, "")

	// Section: Attaching to agent sessions
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
// Optimized to use strings.Builder for reduced allocations.
func (p *Plugin) truncateAllLines(content string, maxWidth int) string {
	if maxWidth <= 0 {
		return content
	}

	var sb strings.Builder
	sb.Grow(len(content)) // Pre-allocate approximate size

	start := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '\n' {
			line := content[start:i]
			line = expandTabs(line, tabStopWidth)
			if lipgloss.Width(line) > maxWidth {
				line = p.truncateCache.Truncate(line, maxWidth, "")
			}
			if start > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(line)
			start = i + 1
		}
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

	lineCount := wt.Agent.OutputBuf.LineCount()
	if lineCount == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	var start, end int
	if p.autoScrollOutput {
		// Auto-scroll: show newest content (last height lines)
		start = lineCount - height
		if start < 0 {
			start = 0
		}
		end = lineCount
	} else {
		// Manual scroll: previewOffset is lines from bottom
		// offset=0 means bottom, offset=N means N lines up from bottom
		start = lineCount - height - p.previewOffset
		if start < 0 {
			start = 0
		}
		end = start + height
		if end > lineCount {
			end = lineCount
		}
	}

	// Get only the lines we need (avoids copying entire 500-line buffer)
	lines := wt.Agent.OutputBuf.LinesRange(start, end)
	if len(lines) == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	// Apply horizontal offset and truncate each line
	// Use TruncateLeftRight for horizontal scrolling to reduce cache thrashing
	// and avoid cellbuf allocation churn from varying offsets.
	displayLines := make([]string, 0, len(lines))
	for _, line := range lines {
		displayLine := expandTabs(line, tabStopWidth)
		// Apply horizontal offset and truncate to width in a single cached operation
		// This prevents allocation churn from repeated parsing with varying offsets
		displayLine = p.truncateCache.TruncateLeftRight(displayLine, p.previewHorizOffset, width)
		displayLines = append(displayLines, displayLine)
	}

	return hint + "\n" + strings.Join(displayLines, "\n")
}

// renderShellOutput renders the selected shell's output.
func (p *Plugin) renderShellOutput(width, height int) string {
	// Get the selected shell
	shell := p.getSelectedShell()
	if shell == nil || shell.Agent == nil {
		return p.renderShellPrimer(width, height)
	}

	// Hint for tmux detach
	hint := dimText("enter to attach • Ctrl-b d to detach")
	height-- // Reserve line for hint

	if shell.Agent.OutputBuf == nil {
		return hint + "\n" + dimText("No output yet")
	}

	lineCount := shell.Agent.OutputBuf.LineCount()
	if lineCount == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	var start, end int
	if p.autoScrollOutput {
		// Auto-scroll: show newest content (last height lines)
		start = lineCount - height
		if start < 0 {
			start = 0
		}
		end = lineCount
	} else {
		// Manual scroll: previewOffset is lines from bottom
		start = lineCount - height - p.previewOffset
		if start < 0 {
			start = 0
		}
		end = start + height
		if end > lineCount {
			end = lineCount
		}
	}

	// Get only the lines we need
	lines := shell.Agent.OutputBuf.LinesRange(start, end)
	if len(lines) == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	// Apply horizontal offset and truncate each line
	displayLines := make([]string, 0, len(lines))
	for _, line := range lines {
		displayLine := expandTabs(line, tabStopWidth)
		displayLine = p.truncateCache.TruncateLeftRight(displayLine, p.previewHorizOffset, width)
		displayLines = append(displayLines, displayLine)
	}

	return hint + "\n" + strings.Join(displayLines, "\n")
}

// renderShellPrimer renders a helpful guide when no shell session exists.
func (p *Plugin) renderShellPrimer(width, height int) string {
	var lines []string

	// Section style
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	// Check if tmux is installed
	if !isTmuxInstalled() {
		lines = append(lines, warningStyle.Render("⚠ tmux Required"))
		lines = append(lines, "")
		lines = append(lines, dimText("The project shell requires tmux to be installed."))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("Install tmux:"))
		lines = append(lines, dimText("  "+getTmuxInstallInstructions()))
		lines = append(lines, "")
		lines = append(lines, dimText("After installing, restart sidecar to use this feature."))
		return strings.Join(lines, "\n")
	}

	// Title
	lines = append(lines, sectionStyle.Render("Project Shell"))
	lines = append(lines, "")

	// Description
	lines = append(lines, dimText("A tmux session in your project directory for running"))
	lines = append(lines, dimText("builds, dev servers, or quick terminal tasks."))
	lines = append(lines, "")

	// Quick start
	lines = append(lines, sectionStyle.Render("Quick Start"))
	lines = append(lines, dimText("  Enter         Create and attach to shell"))
	lines = append(lines, dimText("  K             Kill shell session"))
	lines = append(lines, dimText("  Ctrl-b d      Detach (return to sidecar)"))
	lines = append(lines, "")
	lines = append(lines, strings.Repeat("─", min(width-4, 50)))
	lines = append(lines, "")

	// Shell vs Worktrees explanation
	lines = append(lines, sectionStyle.Render("Shell vs Worktrees"))
	lines = append(lines, "")
	lines = append(lines, dimText("Shell: A single terminal in your project root."))
	lines = append(lines, dimText("  Use for dev servers, builds, quick commands."))
	lines = append(lines, "")
	lines = append(lines, dimText("Worktrees: Separate git working directories, each"))
	lines = append(lines, dimText("  with its own branch. Use for parallel development"))
	lines = append(lines, dimText("  or running AI agents on isolated tasks."))
	lines = append(lines, "")

	// How to create worktree
	lines = append(lines, sectionStyle.Render("Create a Worktree"))
	lines = append(lines, dimText("  Press 'n' or click New in the sidebar"))

	return strings.Join(lines, "\n")
}

// renderCommitStatusHeader renders the commit status header for diff view.
func (p *Plugin) renderCommitStatusHeader(width int) string {
	if len(p.commitStatusList) == 0 {
		return ""
	}

	// Box style for header
	headerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(width - 2)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	hashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	pushedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	localStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Commits (%d)", len(p.commitStatusList))))
	sb.WriteString("\n")

	// Show up to 5 commits
	maxCommits := 5
	displayCount := len(p.commitStatusList)
	if displayCount > maxCommits {
		displayCount = maxCommits
	}

	for i := 0; i < displayCount; i++ {
		commit := p.commitStatusList[i]

		// Status icon
		var statusIcon string
		if commit.Pushed {
			statusIcon = pushedStyle.Render("↑")
		} else {
			statusIcon = localStyle.Render("○")
		}

		// Truncate subject to fit
		subject := commit.Subject
		maxSubjectLen := width - 15 // hash(7) + icon(2) + spaces(6)
		if maxSubjectLen < 10 {
			maxSubjectLen = 10
		}
		if len(subject) > maxSubjectLen {
			subject = subject[:maxSubjectLen-3] + "..."
		}

		line := fmt.Sprintf("%s %s %s", statusIcon, hashStyle.Render(commit.Hash), subject)
		sb.WriteString(line)
		if i < displayCount-1 {
			sb.WriteString("\n")
		}
	}

	if len(p.commitStatusList) > maxCommits {
		sb.WriteString("\n")
		sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.commitStatusList)-maxCommits)))
	}

	return headerStyle.Render(sb.String())
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

	// Mode indicator
	modeHint := dimText("[m] raw")
	if p.taskMarkdownMode {
		modeHint = dimText("[m] rendered")
	}

	// Header
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Task: %s", task.ID))+"  "+modeHint)

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

	// Markdown rendering for description and acceptance
	if p.taskMarkdownMode && p.markdownRenderer != nil {
		// Build markdown content
		var mdContent strings.Builder
		if task.Description != "" {
			mdContent.WriteString(task.Description)
			mdContent.WriteString("\n\n")
		}
		if task.Acceptance != "" {
			mdContent.WriteString("## Acceptance Criteria\n\n")
			mdContent.WriteString(task.Acceptance)
		}

		// Check if we need to re-render (width changed or cache empty)
		if p.taskMarkdownWidth != width || len(p.taskMarkdownRendered) == 0 {
			p.taskMarkdownRendered = p.markdownRenderer.RenderContent(mdContent.String(), width-4)
			p.taskMarkdownWidth = width
		}

		// Append rendered lines
		lines = append(lines, p.taskMarkdownRendered...)
	} else {
		// Plain text fallback
		if task.Description != "" {
			wrapped := wrapText(task.Description, width-4)
			lines = append(lines, wrapped)
			lines = append(lines, "")
		}

		if task.Acceptance != "" {
			lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Acceptance Criteria:"))
			wrapped := wrapText(task.Acceptance, width-4)
			lines = append(lines, wrapped)
			lines = append(lines, "")
		}
	}

	// Timestamps (dimmed)
	lines = append(lines, "")
	if task.CreatedAt != "" {
		lines = append(lines, dimText(fmt.Sprintf("Created: %s", task.CreatedAt)))
	}
	if task.UpdatedAt != "" {
		lines = append(lines, dimText(fmt.Sprintf("Updated: %s", task.UpdatedAt)))
	}

	return strings.Join(lines, "\n")
}

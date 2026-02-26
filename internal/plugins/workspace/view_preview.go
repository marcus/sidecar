package workspace

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// renderPreviewContent renders the preview pane content (no borders).
func (p *Plugin) renderPreviewContent(width, height int) string {
	var lines []string
	interactive := p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active
	if interactive {
		p.interactiveState.ContentRowOffset = 0
	}

	// Show welcome guide only when no worktree AND no shell is selected
	wt := p.selectedWorktree()
	if wt == nil && !p.shellSelected {
		return p.truncateAllLines(p.renderWelcomeGuide(width, height), width)
	}

	// When shell is selected, show shell content directly without tabs
	// (Output/Diff/Task tabs are not relevant for the project shell)
	if p.shellSelected {
		content := p.renderShellOutput(width, height)
		if interactive && !p.flashPreviewTime.IsZero() && time.Since(p.flashPreviewTime) < flashDuration {
			p.interactiveState.ContentRowOffset++
		}
		content = p.prependFlashHint(content)
		return p.truncateAllLines(content, width)
	}

	// Main worktree: show informational view instead of normal tabs
	if wt.IsMain {
		return p.truncateAllLines(p.renderMainWorktreeView(width, height), width)
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
		if interactive {
			p.interactiveState.ContentRowOffset += 2
			if !p.flashPreviewTime.IsZero() && time.Since(p.flashPreviewTime) < flashDuration {
				p.interactiveState.ContentRowOffset++
			}
		}
	case PreviewTabDiff:
		content = p.renderDiffContent(width, contentHeight)
	case PreviewTabTask:
		content = p.renderTaskContent(width, contentHeight)
	}

	lines = append(lines, content)

	// Final safety: ensure ALL lines are truncated to width
	// This catches any content that wasn't properly truncated
	result := strings.Join(lines, "\n")
	result = p.prependFlashHint(result)
	return p.truncateAllLines(result, width)
}

// prependFlashHint adds an attach hint at the top of content when flash is active.
func (p *Plugin) prependFlashHint(content string) string {
	if !p.flashPreviewTime.IsZero() && time.Since(p.flashPreviewTime) < flashDuration {
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(styles.GetCurrentTheme().Colors.Warning)).
			Bold(true)
		hint := hintStyle.Render("Enter or double-click to attach")
		return hint + "\n" + content
	}
	return content
}

// renderWelcomeGuide renders a helpful guide when no worktree is selected.
func (p *Plugin) renderWelcomeGuide(width, height int) string {
	var lines []string

	// Section Style
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Primary)
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Warning)

	// Check if tmux is installed
	if !isTmuxInstalled() {
		lines = append(lines, warningStyle.Render("⚠ tmux Required"))
		lines = append(lines, "")
		lines = append(lines, dimText("Workspaces and shell sessions require tmux to be installed."))
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
	prefix := getTmuxPrefix()
	lines = append(lines, sectionStyle.Render("Agent Sessions"))
	lines = append(lines, dimText("  Enter      Attach to selected worktree session"))
	lines = append(lines, dimText(fmt.Sprintf("  %s d   Detach from session (return here)", prefix)))
	lines = append(lines, "")

	// Section: Navigation inside tmux
	lines = append(lines, sectionStyle.Render("Scrolling (in attached session)"))
	lines = append(lines, dimText(fmt.Sprintf("  %s [        Enter scroll mode", prefix)))
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
			line = ui.ExpandTabs(line, tabStopWidth)
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
			rendered = append(rendered, styles.RenderPillWithStyle(tab, styles.BarChipActive, ""))
		} else {
			rendered = append(rendered, styles.RenderPillWithStyle(tab, styles.BarChip, ""))
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

	// Check for orphaned worktree (agent file exists but tmux session gone)
	if wt.IsOrphaned && wt.Agent == nil {
		return p.renderOrphanedMessage(wt.ChosenAgentType)
	}

	if wt.Agent == nil {
		return dimText("No agent running\nPress 's' to start an agent")
	}

	// Hint depends on mode - interactive mode shows exit hints
	var hint string
	if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
		// Interactive mode - show exit hint with highlight
		interactiveStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(styles.GetCurrentTheme().Colors.Warning)).
			Bold(true)
		hint = interactiveStyle.Render("INTERACTIVE") + " " + dimText(p.getInteractiveExitKey()+" exit • "+p.getInteractiveAttachKey()+" attach")
	} else {
		// Only show "E for interactive" hint if feature flag is enabled
		detach := getTmuxDetachHint()
		if features.IsEnabled(features.TmuxInteractiveInput.Name) {
			hint = dimText(fmt.Sprintf("t to attach • E for interactive • %s to detach", detach))
		} else {
			hint = dimText(fmt.Sprintf("t to attach • %s to detach", detach))
		}
	}
	height-- // Reserve line for hint

	if wt.Agent.OutputBuf == nil {
		return hint + "\n" + dimText("No output yet")
	}

	lineCount := wt.Agent.OutputBuf.LineCount()
	if lineCount == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	interactive := p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active
	var cursorRow, cursorCol, paneHeight, paneWidth int
	var cursorVisible bool
	if interactive {
		p.interactiveState.VisibleStart = 0
		p.interactiveState.VisibleEnd = 0
		p.interactiveState.ContentRowOffset = 1
		cursorRow, cursorCol, paneHeight, paneWidth, cursorVisible, _ = p.getCursorPosition()
	}

	visibleHeight := height
	if interactive && paneHeight > 0 && paneHeight < visibleHeight {
		visibleHeight = paneHeight
	}

	displayWidth := width
	if interactive && paneWidth > 0 && paneWidth < displayWidth {
		displayWidth = paneWidth
	}

	effectiveLineCount := lineCount
	if p.autoScrollOutput && !interactive {
		lines := wt.Agent.OutputBuf.Lines()
		if idx := lastNonEmptyLine(lines); idx >= 0 {
			nonEmptyCount := idx + 1
			if nonEmptyCount < visibleHeight {
				effectiveLineCount = nonEmptyCount
			}
		} else {
			effectiveLineCount = 0
		}
		if effectiveLineCount == 0 {
			return hint + "\n" + dimText("No output yet")
		}
	}

	var start, end int
	if p.autoScrollOutput {
		// Auto-scroll: show newest content (last visibleHeight lines)
		start = effectiveLineCount - visibleHeight
		if start < 0 {
			start = 0
		}
		end = effectiveLineCount
	} else {
		// Manual scroll: previewOffset is lines from bottom
		// offset=0 means bottom, offset=N means N lines up from bottom
		// td-f7c8be: Use scrollBaseLineCount to prevent bounce when polling adds content.
		// Without this, added lines shift the view because offset is relative to bottom.
		baseCount := effectiveLineCount
		if p.scrollBaseLineCount > 0 && p.scrollBaseLineCount <= effectiveLineCount {
			baseCount = p.scrollBaseLineCount
		}
		start = baseCount - visibleHeight - p.previewOffset
		if start < 0 {
			start = 0
		}
		end = start + visibleHeight
		if end > effectiveLineCount {
			end = effectiveLineCount
		}
	}

	// Get only the lines we need (avoids copying entire 500-line buffer)
	lines := wt.Agent.OutputBuf.LinesRange(start, end)
	if len(lines) == 0 {
		return hint + "\n" + dimText("No output yet")
	}
	if interactive {
		p.interactiveState.VisibleStart = start
		p.interactiveState.VisibleEnd = end
	}

	// Truncate each line to display width
	// and avoid cellbuf allocation churn from varying offsets.
	displayLines := make([]string, 0, len(lines))
	for i, line := range lines {
		displayLine := ui.ExpandTabs(line, tabStopWidth)
		// Apply character-level selection background BEFORE truncation
		if interactive && p.selection.HasSelection() {
			startCol, endCol := p.selection.GetLineSelectionCols(start + i)
			if startCol >= 0 {
				displayLine = ui.InjectCharacterRangeBackground(displayLine, startCol, endCol)
			}
		}
		// Truncate to width
		displayLine = p.truncateCache.Truncate(displayLine, displayWidth, "")
		displayLines = append(displayLines, displayLine)
	}

	if interactive && paneHeight > 0 {
		targetHeight := visibleHeight
		if targetHeight > height {
			targetHeight = height
		}
		if targetHeight > 0 && len(displayLines) < targetHeight {
			displayLines = padLinesToHeight(displayLines, targetHeight)
		}
	}

	content := strings.Join(displayLines, "\n")

	// Apply cursor overlay in interactive mode
	if interactive && cursorVisible {
		// cursor_y is relative to tmux pane (0 to paneHeight-1).
		// Our display shows len(displayLines) lines.
		displayHeight := len(displayLines)
		relativeRow := cursorRow
		if paneHeight > displayHeight {
			relativeRow = cursorRow - (paneHeight - displayHeight)
		} else if paneHeight > 0 && paneHeight < displayHeight {
			relativeRow = cursorRow + (displayHeight - paneHeight)
		}
		relativeCol := cursorCol

		// Clamp cursor position to visible area instead of hiding it (td-16bfa6).
		// This ensures cursor remains visible even during pane size mismatches,
		// which can occur with lots of scrollback or during resize transitions.
		if relativeRow < 0 {
			relativeRow = 0
		}
		if relativeRow >= displayHeight {
			relativeRow = displayHeight - 1
		}
		if relativeCol < 0 {
			relativeCol = 0
		}
		if relativeCol >= displayWidth {
			relativeCol = displayWidth - 1
		}

		content = renderWithCursor(content, relativeRow, relativeCol, cursorVisible)
	}

	return hint + "\n" + content
}

// renderOrphanedMessage renders the recovery prompt for orphaned worktrees.
func (p *Plugin) renderOrphanedMessage(agentType AgentType) string {
	var lines []string

	// Warning header
	warningStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(styles.GetCurrentTheme().Colors.Warning))

	lines = append(lines, warningStyle.Render("Session Ended"))
	lines = append(lines, "")
	lines = append(lines, dimText("The tmux session has ended, but your worktree and work are still intact."))
	lines = append(lines, "")

	// Show previously running agent
	agentName := AgentDisplayNames[agentType]
	if agentName == "" {
		agentName = string(agentType)
	}
	lines = append(lines, dimText(fmt.Sprintf("Previously running: %s", agentName)))
	lines = append(lines, "")

	// Action prompt
	actionStyle := lipgloss.NewStyle().
		Foreground(styles.Primary)
	lines = append(lines, actionStyle.Render("Press Enter to start a new session"))

	return strings.Join(lines, "\n")
}

// renderShellOutput renders the selected shell's output.
func (p *Plugin) renderShellOutput(width, height int) string {
	// Get the selected shell
	shell := p.getSelectedShell()
	if shell == nil || shell.Agent == nil {
		return p.renderShellPrimer(width, height)
	}

	// Hint depends on mode - interactive mode shows exit hints
	var hint string
	if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
		// Interactive mode - show exit hint with highlight
		interactiveStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(styles.GetCurrentTheme().Colors.Warning)).
			Bold(true)
		hint = interactiveStyle.Render("INTERACTIVE") + " " + dimText(p.getInteractiveExitKey()+" exit")
	} else {
		// Only show "E for interactive" hint if feature flag is enabled
		detach := getTmuxDetachHint()
		if features.IsEnabled(features.TmuxInteractiveInput.Name) {
			hint = dimText(fmt.Sprintf("t to attach • E for interactive • %s to detach", detach))
		} else {
			hint = dimText(fmt.Sprintf("t to attach • %s to detach", detach))
		}
	}
	height-- // Reserve line for hint

	if shell.Agent.OutputBuf == nil {
		return hint + "\n" + dimText("No output yet")
	}

	lineCount := shell.Agent.OutputBuf.LineCount()
	if lineCount == 0 {
		return hint + "\n" + dimText("No output yet")
	}

	interactive := p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active
	var cursorRow, cursorCol, paneHeight, paneWidth int
	var cursorVisible bool
	if interactive {
		p.interactiveState.VisibleStart = 0
		p.interactiveState.VisibleEnd = 0
		p.interactiveState.ContentRowOffset = 1
		cursorRow, cursorCol, paneHeight, paneWidth, cursorVisible, _ = p.getCursorPosition()
	}

	visibleHeight := height
	if interactive && paneHeight > 0 && paneHeight < visibleHeight {
		visibleHeight = paneHeight
	}

	displayWidth := width
	if interactive && paneWidth > 0 && paneWidth < displayWidth {
		displayWidth = paneWidth
	}

	effectiveLineCount := lineCount
	if p.autoScrollOutput && !interactive {
		lines := shell.Agent.OutputBuf.Lines()
		if idx := lastNonEmptyLine(lines); idx >= 0 {
			nonEmptyCount := idx + 1
			if nonEmptyCount < visibleHeight {
				effectiveLineCount = nonEmptyCount
			}
		} else {
			effectiveLineCount = 0
		}
		if effectiveLineCount == 0 {
			return hint + "\n" + dimText("No output yet")
		}
	}

	var start, end int
	if p.autoScrollOutput {
		// Auto-scroll: show newest content (last visibleHeight lines)
		start = effectiveLineCount - visibleHeight
		if start < 0 {
			start = 0
		}
		end = effectiveLineCount
	} else {
		// Manual scroll: previewOffset is lines from bottom
		// td-f7c8be: Use scrollBaseLineCount to prevent bounce when polling adds content.
		baseCount := effectiveLineCount
		if p.scrollBaseLineCount > 0 && p.scrollBaseLineCount <= effectiveLineCount {
			baseCount = p.scrollBaseLineCount
		}
		start = baseCount - visibleHeight - p.previewOffset
		if start < 0 {
			start = 0
		}
		end = start + visibleHeight
		if end > effectiveLineCount {
			end = effectiveLineCount
		}
	}

	// Get only the lines we need
	lines := shell.Agent.OutputBuf.LinesRange(start, end)
	if len(lines) == 0 {
		return hint + "\n" + dimText("No output yet")
	}
	if interactive {
		p.interactiveState.VisibleStart = start
		p.interactiveState.VisibleEnd = end
	}

	// Apply horizontal offset and truncate each line
	displayLines := make([]string, 0, len(lines))
	for i, line := range lines {
		displayLine := ui.ExpandTabs(line, tabStopWidth)
		// Apply character-level selection background BEFORE truncation
		if interactive && p.selection.HasSelection() {
			startCol, endCol := p.selection.GetLineSelectionCols(start + i)
			if startCol >= 0 {
				displayLine = ui.InjectCharacterRangeBackground(displayLine, startCol, endCol)
			}
		}
		displayLine = p.truncateCache.Truncate(displayLine, displayWidth, "")
		displayLines = append(displayLines, displayLine)
	}

	if interactive && paneHeight > 0 {
		targetHeight := visibleHeight
		if targetHeight > height {
			targetHeight = height
		}
		if targetHeight > 0 && len(displayLines) < targetHeight {
			displayLines = padLinesToHeight(displayLines, targetHeight)
		}
	}

	content := strings.Join(displayLines, "\n")

	// Apply cursor overlay in interactive mode
	if interactive && cursorVisible {
		// cursor_y is relative to tmux pane (0 to paneHeight-1).
		// Our display shows len(displayLines) lines.
		displayHeight := len(displayLines)
		relativeRow := cursorRow
		if paneHeight > displayHeight {
			relativeRow = cursorRow - (paneHeight - displayHeight)
		} else if paneHeight > 0 && paneHeight < displayHeight {
			relativeRow = cursorRow + (displayHeight - paneHeight)
		}
		relativeCol := cursorCol

		// Clamp cursor position to visible area instead of hiding it (td-16bfa6).
		// This ensures cursor remains visible even during pane size mismatches,
		// which can occur with lots of scrollback or during resize transitions.
		if relativeRow < 0 {
			relativeRow = 0
		}
		if relativeRow >= displayHeight {
			relativeRow = displayHeight - 1
		}
		if relativeCol < 0 {
			relativeCol = 0
		}
		if relativeCol >= displayWidth {
			relativeCol = displayWidth - 1
		}

		content = renderWithCursor(content, relativeRow, relativeCol, cursorVisible)
	}

	return hint + "\n" + content
}

func padLinesToHeight(lines []string, target int) []string {
	if target <= 0 || len(lines) >= target {
		return lines
	}
	for len(lines) < target {
		lines = append(lines, "")
	}
	return lines
}

func lastNonEmptyLine(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(ansi.Strip(lines[i])) != "" {
			return i
		}
	}
	return -1
}

// renderShellPrimer renders a helpful guide when no shell session exists.
func (p *Plugin) renderShellPrimer(width, height int) string {
	var lines []string

	// Section style
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Primary)
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Warning)

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
	prefix := getTmuxPrefix()
	lines = append(lines, sectionStyle.Render("Quick Start"))
	lines = append(lines, dimText("  Enter         Create and attach to shell"))
	lines = append(lines, dimText("  K             Kill shell session"))
	lines = append(lines, dimText(fmt.Sprintf("  %s d      Detach (return to sidecar)", prefix)))
	lines = append(lines, "")
	lines = append(lines, strings.Repeat("─", min(width-4, 50)))
	lines = append(lines, "")

	// Shell vs Worktrees explanation
	lines = append(lines, sectionStyle.Render("Shell vs Worktrees"))
	lines = append(lines, "")
	lines = append(lines, dimText("Shell: A single terminal in your project root."))
	lines = append(lines, dimText("  Use for dev servers, builds, quick commands."))
	lines = append(lines, "")
	lines = append(lines, dimText("Workspaces: Separate git working directories, each"))
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
		BorderForeground(styles.Primary).
		Padding(0, 1).
		Width(width - 2)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Primary)
	hashStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	pushedStyle := lipgloss.NewStyle().Foreground(styles.Success)
	localStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)

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

// renderMainWorktreeView renders a helpful view when the main worktree is selected.
func (p *Plugin) renderMainWorktreeView(width, height int) string {
	var lines []string

	// ASCII art tree - thematic for "worktrees"
	tree := []string{
		"",
		"              *",
		"             /|\\",
		"            / | \\",
		"           /  |  \\",
		"          /   |   \\",
		"         /___ | ___\\",
		"             |||",
		"             |||",
		"            /|||\\ ",
		"",
	}

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Primary)
	treeStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
	hintStyle := lipgloss.NewStyle().Foreground(styles.Success)

	// Center the tree
	for _, line := range tree {
		centered := treeStyle.Render(line)
		lines = append(lines, centered)
	}

	lines = append(lines, "")
	lines = append(lines, titleStyle.Render("Main Worktree"))
	lines = append(lines, "")
	lines = append(lines, dimText("This is your primary working directory—the trunk of the tree."))
	lines = append(lines, dimText("Workspaces branch off from here as isolated environments."))
	lines = append(lines, "")
	lines = append(lines, strings.Repeat("─", min(width-4, 50)))
	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("Press 'n' to create a new workspace"))
	lines = append(lines, "")
	lines = append(lines, dimText("Each workspace gets its own directory, branch, and"))
	lines = append(lines, dimText("optional AI agent. Work on multiple features in"))
	lines = append(lines, dimText("parallel without switching branches."))

	return strings.Join(lines, "\n")
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

	// Check if we're loading or don't have cached details for this task
	if p.taskLoading || p.cachedTask == nil || p.cachedTaskID != wt.TaskID {
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

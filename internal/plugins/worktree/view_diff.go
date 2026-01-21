package worktree

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// renderDiffContent renders git diff using the shared diff renderer.
func (p *Plugin) renderDiffContent(width, height int) string {
	wt := p.selectedWorktree()
	if wt == nil {
		return dimText("No worktree selected")
	}

	// Render commit status header if it belongs to current worktree
	header := ""
	if p.commitStatusWorktree == wt.Name {
		header = p.renderCommitStatusHeader(width)
	}

	headerHeight := 0
	if header != "" {
		headerHeight = lipgloss.Height(header) + 1 // +1 for blank line
	}

	if p.diffRaw == "" {
		if header != "" {
			return header + "\n" + dimText("No uncommitted changes")
		}
		return dimText("No changes")
	}

	// Adjust available height for diff content
	contentHeight := height - headerHeight
	if contentHeight < 5 {
		contentHeight = 5
	}

	// Use multi-file diff rendering if available
	if p.multiFileDiff != nil && len(p.multiFileDiff.Files) > 0 {
		var mode gitstatus.DiffViewMode
		if p.diffViewMode == DiffViewSideBySide {
			mode = gitstatus.DiffViewSideBySide
		} else {
			mode = gitstatus.DiffViewUnified
		}
		diffContent := gitstatus.RenderMultiFileDiff(p.multiFileDiff, mode, width, p.previewOffset, contentHeight, p.previewHorizOffset)
		if header != "" {
			return header + "\n" + diffContent
		}
		return diffContent
	}

	// Fallback: Parse the raw diff into structured format (single file)
	parsed, err := gitstatus.ParseUnifiedDiff(p.diffRaw)
	if err != nil || parsed == nil {
		// Fallback to basic rendering
		diffContent := p.renderDiffContentBasicWithHeight(width, contentHeight)
		if header != "" {
			return header + "\n" + diffContent
		}
		return diffContent
	}

	// Create syntax highlighter if we have file info
	var highlighter *gitstatus.SyntaxHighlighter
	if parsed.NewFile != "" {
		highlighter = gitstatus.NewSyntaxHighlighter(parsed.NewFile)
	}

	// Render based on view mode
	var diffContent string
	if p.diffViewMode == DiffViewSideBySide {
		diffContent = gitstatus.RenderSideBySide(parsed, width, p.previewOffset, contentHeight, p.previewHorizOffset, highlighter)
	} else {
		diffContent = gitstatus.RenderLineDiff(parsed, width, p.previewOffset, contentHeight, p.previewHorizOffset, highlighter)
	}

	if header != "" {
		return header + "\n" + diffContent
	}
	return diffContent
}

// renderDiffContentBasic renders git diff with basic highlighting (fallback).
func (p *Plugin) renderDiffContentBasic(width, height int) string {
	return p.renderDiffContentBasicWithHeight(width, height)
}

// renderDiffContentBasicWithHeight renders git diff with basic highlighting with explicit height.
func (p *Plugin) renderDiffContentBasicWithHeight(width, height int) string {
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
			styledLine = p.truncateCache.TruncateLeft(styledLine, p.previewHorizOffset, "")
		}
		if lipgloss.Width(styledLine) > width {
			styledLine = p.truncateCache.Truncate(styledLine, width, "")
		}
		rendered = append(rendered, styledLine)
	}

	return strings.Join(rendered, "\n")
}

// jumpToNextFile jumps to the next file in the multi-file diff.
func (p *Plugin) jumpToNextFile() tea.Cmd {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) <= 1 {
		return nil
	}

	// Find current file index based on scroll position
	currentIdx := p.multiFileDiff.FileAtLine(p.previewOffset)
	if currentIdx < 0 {
		currentIdx = 0
	}

	// Jump to next file
	nextIdx := currentIdx + 1
	if nextIdx >= len(p.multiFileDiff.Files) {
		// Already at last file, stay there
		return nil
	}

	// Set scroll position to start of next file
	p.previewOffset = p.multiFileDiff.Files[nextIdx].StartLine
	return nil
}

// jumpToPrevFile jumps to the previous file in the multi-file diff.
func (p *Plugin) jumpToPrevFile() tea.Cmd {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) <= 1 {
		return nil
	}

	// Find current file index based on scroll position
	currentIdx := p.multiFileDiff.FileAtLine(p.previewOffset)
	if currentIdx < 0 {
		currentIdx = 0
	}

	// If we're past the start of current file, jump to its start
	if p.previewOffset > p.multiFileDiff.Files[currentIdx].StartLine {
		p.previewOffset = p.multiFileDiff.Files[currentIdx].StartLine
		return nil
	}

	// Jump to previous file
	prevIdx := currentIdx - 1
	if prevIdx < 0 {
		// Already at first file, jump to start
		p.previewOffset = 0
		return nil
	}

	// Set scroll position to start of previous file
	p.previewOffset = p.multiFileDiff.Files[prevIdx].StartLine
	return nil
}

// renderFilePickerModal renders the file picker modal overlay.
func (p *Plugin) renderFilePickerModal(background string) string {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) == 0 {
		return background
	}

	files := p.multiFileDiff.Files

	// Build modal content
	var sb strings.Builder
	sb.WriteString(styles.ModalTitle.Render("Jump to File"))
	sb.WriteString("\n\n")

	// List files with selection highlight
	for i, file := range files {
		line := file.FileName() + " " + styles.Muted.Render("("+file.ChangeStats()+")")
		if i == p.filePickerIdx {
			sb.WriteString(styles.ListItemSelected.Render("▸ " + line))
		} else {
			sb.WriteString("  " + line)
		}
		if i < len(files)-1 {
			sb.WriteString("\n")
		}
	}

	// Calculate modal dimensions
	modalWidth := 50
	for _, file := range files {
		nameWidth := lipgloss.Width(file.FileName()) + lipgloss.Width(file.ChangeStats()) + 6
		if nameWidth > modalWidth {
			modalWidth = nameWidth
		}
	}
	if modalWidth > p.width-10 {
		modalWidth = p.width - 10
	}

	// Style the modal
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 2).
		Width(modalWidth)

	modal := modalStyle.Render(sb.String())

	return ui.OverlayModal(background, modal, p.width, p.height)
}

// colorDiffLine applies basic diff coloring using theme styles.
func (p *Plugin) colorDiffLine(line string, width int) string {
	line = expandTabs(line, tabStopWidth)
	if len(line) == 0 {
		return line
	}

	// Truncate if needed
	if lipgloss.Width(line) > width {
		line = p.truncateCache.Truncate(line, width, "")
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

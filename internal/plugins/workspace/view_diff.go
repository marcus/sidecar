package workspace

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// diffTabFileListWidth returns the width allocated to the file list in the diff tab.
// Uses ~25% of available width, clamped to reasonable bounds.
// The diff viewer should take the majority of the space.
func diffTabFileListWidth(totalWidth int) int {
	w := totalWidth * 25 / 100
	if w < 20 {
		w = 20
	}
	maxW := totalWidth - 30
	if maxW < 20 {
		maxW = 20
	}
	if w > maxW {
		w = maxW
	}
	return w
}

// clampDiffTabCursor ensures the diff tab cursor is within valid bounds.
// Call this before rendering to avoid state mutation in View().
func (p *Plugin) clampDiffTabCursor() {
	totalItems := p.diffTabTotalItems()
	if totalItems == 0 {
		p.diffTabCursor = 0
		p.diffTabScroll = 0
		return
	}
	if p.diffTabCursor >= totalItems {
		p.diffTabCursor = totalItems - 1
	}
	if p.diffTabCursor < 0 {
		p.diffTabCursor = 0
	}
}

// diffTabCollapseThreshold is the minimum width (in columns) for the two-pane diff layout.
// Below this, the diff tab collapses to a single-pane hierarchical view where l/enter
// drills down one level and h/esc goes back up.
const diffTabCollapseThreshold = 120

// renderDiffContent renders git diff using a two-pane layout (or collapsed single-pane
// when width is below diffTabCollapseThreshold).
func (p *Plugin) renderDiffContent(width, height int) string {
	wt := p.selectedWorktree()
	if wt == nil {
		return dimText("No worktree selected")
	}

	hasFiles := p.multiFileDiff != nil && len(p.multiFileDiff.Files) > 0
	hasCommits := len(p.commitStatusList) > 0

	// Only show "No changes" if there are truly no files AND no commits to display
	if !hasFiles && !hasCommits {
		if p.diffRaw == "" {
			return dimText("No changes")
		}
		// Have raw diff but no parsed multi-file diff — fall back to basic rendering
		return p.renderDiffContentBasicWithHeight(width, height)
	}

	// Clamp cursor before rendering (avoid state mutation during View)
	p.clampDiffTabCursor()

	// Collapsed single-pane mode for narrow terminals
	if width < diffTabCollapseThreshold {
		return p.renderDiffContentCollapsed(width, height)
	}

	// Two-pane layout dimensions
	fileListWidth := p.diffTabListWidth
	if fileListWidth <= 0 {
		fileListWidth = diffTabFileListWidth(width)
	}
	// Clamp to available space
	if fileListWidth < 20 {
		fileListWidth = 20
	}
	maxW := width - 30
	if maxW < 20 {
		maxW = 20
	}
	if fileListWidth > maxW {
		fileListWidth = maxW
	}
	diffPaneWidth := width - fileListWidth - 1 // -1 for divider
	if diffPaneWidth < 10 {
		diffPaneWidth = 10
	}

	// Register hit region for the diff tab divider (for drag-to-resize).
	// Calculate absolute X position: preview content starts after sidebar + main divider + panel border/padding.
	if !p.sidebarVisible {
		// Full-width preview: content starts at panelOverhead/2
		absX := panelOverhead/2 + fileListWidth
		p.mouseHandler.HitMap.AddRect(regionDiffTabDivider, absX, 0, dividerHitWidth, p.height, nil)
	} else {
		available := p.width - dividerWidth
		sidebarW := (available * p.sidebarWidth) / 100
		if sidebarW < 25 {
			sidebarW = 25
		}
		if sidebarW > available-40 {
			sidebarW = available - 40
		}
		absX := sidebarW + dividerWidth + panelOverhead/2 + fileListWidth
		p.mouseHandler.HitMap.AddRect(regionDiffTabDivider, absX, 0, dividerHitWidth, p.height, nil)
	}

	var leftPane, rightPane string

	if p.diffTabFocus == DiffTabFocusCommitFiles || p.diffTabFocus == DiffTabFocusCommitDiff {
		// Commit drill-down: left=commit files, right=commit file diff
		leftPane = p.renderCommitFileList(fileListWidth, height)
		rightPane = p.renderCommitFileDiffPane(diffPaneWidth, height)
	} else {
		// Default: left=files+commits, right=per-file diff
		leftPane = p.renderDiffTabFileList(fileListWidth, height)
		rightPane = p.renderDiffTabDiffPane(diffPaneWidth, height)
	}

	divider := p.renderDiffTabDivider(height)

	// Pad panes to equal height so lipgloss.JoinHorizontal aligns correctly
	leftPane = padToHeight(leftPane, height, fileListWidth)
	rightPane = padToHeight(rightPane, height, diffPaneWidth)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// renderDiffContentCollapsed renders the diff tab as a single full-width pane.
// The current diffTabFocus determines which level is shown:
//   - FileList: file list + commits (l/enter drills into diff or commit)
//   - Diff: per-file diff (h/esc goes back to file list)
//   - CommitFiles: commit file list (h/esc goes back to file list)
//   - CommitDiff: commit file diff (h/esc goes back to commit files)
func (p *Plugin) renderDiffContentCollapsed(width, height int) string {
	switch p.diffTabFocus {
	case DiffTabFocusDiff:
		return p.renderDiffTabDiffPane(width, height)
	case DiffTabFocusCommitFiles:
		return p.renderCommitFileList(width, height)
	case DiffTabFocusCommitDiff:
		return p.renderCommitFileDiffPane(width, height)
	default:
		return p.renderDiffTabFileList(width, height)
	}
}

// padToHeight ensures content has exactly `height` lines, padding with empty lines
// or truncating as needed. Width is used to ensure blank lines fill the space.
func padToHeight(content string, height, width int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

// renderDiffTabFileList renders the file list sidebar within the diff tab.
func (p *Plugin) renderDiffTabFileList(width, height int) string {
	var sb strings.Builder

	var files []gitstatus.FileDiffInfo
	if p.multiFileDiff != nil {
		files = p.multiFileDiff.Files
	}
	commits := p.commitStatusList

	fileListActive := p.diffTabFocus == DiffTabFocusFileList
	maxWidth := width - 2 // Padding

	// Header
	headerText := fmt.Sprintf("Files (%d)", len(files))
	if fileListActive {
		sb.WriteString(styles.Title.Render(headerText))
	} else {
		sb.WriteString(styles.Muted.Render(headerText))
	}
	sb.WriteString("\n")

	linesUsed := 1

	// Calculate space for files vs commits
	commitLines := 0
	if len(commits) > 0 {
		commitLines = 2 + len(commits) // header + divider + commit lines
		if commitLines > height/3 {
			commitLines = height / 3
			if commitLines < 3 {
				commitLines = 3
			}
		}
	}
	filesHeight := height - linesUsed - commitLines
	if filesHeight < 3 {
		filesHeight = 3
	}

	// Adjust scroll to keep cursor visible (when cursor is on a file)
	if p.diffTabCursor < len(files) {
		if p.diffTabCursor < p.diffTabScroll {
			p.diffTabScroll = p.diffTabCursor
		}
		if p.diffTabCursor >= p.diffTabScroll+filesHeight {
			p.diffTabScroll = p.diffTabCursor - filesHeight + 1
		}
	}

	// Render file entries
	startIdx := p.diffTabScroll
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + filesHeight
	if endIdx > len(files) {
		endIdx = len(files)
	}

	for i := startIdx; i < endIdx; i++ {
		file := files[i]
		selected := i == p.diffTabCursor

		// File status icon based on diff type
		statusIcon := "M"
		if file.Additions > 0 && file.Deletions == 0 {
			statusIcon = "A"
		} else if file.Additions == 0 && file.Deletions > 0 {
			statusIcon = "D"
		}

		// File path - truncate if needed (rune-safe)
		fileName := file.FileName()
		statsStr := file.ChangeStats()
		availableWidth := maxWidth - 4 // status + spaces
		fileRunes := []rune(fileName)
		if len(fileRunes)+len(statsStr)+1 > availableWidth {
			keepWidth := availableWidth - len(statsStr) - 2 // -2 for "…" and space
			if keepWidth > 3 {
				fileName = "…" + string(fileRunes[len(fileRunes)-keepWidth:])
			} else if availableWidth > 5 {
				keepWidth = availableWidth - 3
				if keepWidth > 0 && keepWidth <= len(fileRunes) {
					fileName = "…" + string(fileRunes[len(fileRunes)-keepWidth:])
				}
			}
		}
		if selected && fileListActive {
			plainLine := fmt.Sprintf("%s %s %s", statusIcon, fileName, statsStr)
			lineWidth := lipgloss.Width(plainLine)
			if lineWidth < maxWidth {
				plainLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(styles.ListItemSelected.Render(plainLine))
		} else if selected {
			// Selected but file list not focused - subtle highlight
			styledLine := styles.Muted.Render(statusIcon+" ") + styles.Body.Render(fileName) + " " + styles.Muted.Render(statsStr)
			lineWidth := lipgloss.Width(styledLine)
			if lineWidth < maxWidth {
				styledLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(styledLine)
		} else {
			var statusStyle lipgloss.Style
			switch statusIcon {
			case "A":
				statusStyle = styles.StatusStaged
			case "D":
				statusStyle = styles.StatusDeleted
			default:
				statusStyle = styles.StatusModified
			}
			styledLine := statusStyle.Render(statusIcon) + " " + fileName + " " + styles.Muted.Render(statsStr)
			lineWidth := lipgloss.Width(styledLine)
			if lineWidth < maxWidth {
				styledLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(styledLine)
		}
		sb.WriteString("\n")
		linesUsed++
	}

	// Commits section
	if len(commits) > 0 {
		sb.WriteString(styles.Muted.Render(strings.Repeat("─", maxWidth)))
		sb.WriteString("\n")
		linesUsed++

		commitHeaderText := fmt.Sprintf("Commits (%d)", len(commits))
		sb.WriteString(styles.Title.Render(commitHeaderText))
		sb.WriteString("\n")
		linesUsed++

		maxCommitLines := height - linesUsed
		if maxCommitLines < 0 {
			maxCommitLines = 0
		}

		for i, commit := range commits {
			if i >= maxCommitLines {
				break
			}

			selected := (len(files) + i) == p.diffTabCursor

			// Hash + subject
			hash := commit.Hash
			if len(hash) > 7 {
				hash = hash[:7]
			}
			subject := commit.Subject
			subjectWidth := maxWidth - 12
			if subjectWidth < 10 {
				subjectWidth = 10
			}
			subjectRunes := []rune(subject)
			if len(subjectRunes) > subjectWidth {
				subject = string(subjectRunes[:subjectWidth-1]) + "…"
			}

			if selected && fileListActive {
				plainIndicator := "○ "
				if commit.Pushed {
					plainIndicator = "↑ "
				}
				plainLine := fmt.Sprintf("%s%s %s", plainIndicator, hash, subject)
				lineWidth := lipgloss.Width(plainLine)
				if lineWidth < maxWidth {
					plainLine += strings.Repeat(" ", maxWidth-lineWidth)
				}
				sb.WriteString(styles.ListItemSelected.Render(plainLine))
			} else {
				var indicator string
				if commit.Pushed {
					indicator = styles.DiffAdd.Render("↑") + " "
				} else {
					indicator = styles.Muted.Render("○") + " "
				}
				styledLine := fmt.Sprintf("%s%s %s", indicator, styles.Code.Render(hash), subject)
				lineWidth := lipgloss.Width(styledLine)
				if lineWidth < maxWidth {
					styledLine += strings.Repeat(" ", maxWidth-lineWidth)
				}
				sb.WriteString(styledLine)
			}
			sb.WriteString("\n")
			linesUsed++
		}
	}

	return sb.String()
}

// renderDiffTabDivider renders the vertical divider between file list and diff pane.
func (p *Plugin) renderDiffTabDivider(height int) string {
	dividerStyle := lipgloss.NewStyle().Foreground(styles.BorderNormal)
	var sb strings.Builder
	for i := 0; i < height; i++ {
		sb.WriteString(dividerStyle.Render("│"))
		if i < height-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderDiffTabDiffPane renders the per-file diff pane within the diff tab.
func (p *Plugin) renderDiffTabDiffPane(width, height int) string {
	var fileCount int
	if p.multiFileDiff != nil {
		fileCount = len(p.multiFileDiff.Files)
	}

	// If cursor is on a commit, show commit preview with file list
	if p.diffTabCursor >= fileCount {
		commitIdx := p.diffTabCursor - fileCount
		if commitIdx >= 0 && commitIdx < len(p.commitStatusList) {
			commit := p.commitStatusList[commitIdx]
			return p.renderDiffTabCommitPreview(commit, width, height)
		}
		return dimText("Select a file to view diff")
	}

	file := p.multiFileDiff.Files[p.diffTabCursor]
	parsed := file.Diff
	if parsed == nil {
		return dimText("No diff data")
	}

	var sb strings.Builder

	// Header: filename + view mode
	var viewModeStr string
	switch p.diffViewMode {
	case DiffViewSideBySide:
		viewModeStr = "split"
	case DiffViewFullFile:
		viewModeStr = "full-file"
	default:
		viewModeStr = "unified"
	}

	fileName := file.FileName()
	headerStr := fmt.Sprintf("%s [%s]", fileName, viewModeStr)
	if p.diffTabFocus == DiffTabFocusDiff {
		sb.WriteString(styles.Title.Render(headerStr))
	} else {
		sb.WriteString(styles.Muted.Render(headerStr))
	}
	sb.WriteString("\n\n")

	// Content height for diff
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Render diff based on view mode
	highlighter := gitstatus.NewSyntaxHighlighter(file.FileName())
	var diffContent string

	switch p.diffViewMode {
	case DiffViewFullFile:
		if p.fullFileDiff != nil {
			diffW := width - gitstatus.MinimapWidth
			mmStr := gitstatus.RenderMinimap(p.fullFileDiff, p.diffTabDiffScroll, contentHeight, contentHeight)
			if mmStr != "" && diffW >= 30 {
				diffContent = gitstatus.RenderFullFileSideBySide(p.fullFileDiff, diffW, p.diffTabDiffScroll, contentHeight, p.diffTabHorizScroll, highlighter, false)
				diffContent = lipgloss.JoinHorizontal(lipgloss.Top, diffContent, mmStr)
			} else {
				diffContent = gitstatus.RenderFullFileSideBySide(p.fullFileDiff, width, p.diffTabDiffScroll, contentHeight, p.diffTabHorizScroll, highlighter, false)
			}
		} else {
			diffContent = dimText("Loading full file...")
		}
	case DiffViewSideBySide:
		diffContent = gitstatus.RenderSideBySide(parsed, width, p.diffTabDiffScroll, contentHeight, p.diffTabHorizScroll, highlighter, false)
	default:
		diffContent = gitstatus.RenderLineDiff(parsed, width, p.diffTabDiffScroll, contentHeight, p.diffTabHorizScroll, highlighter, false)
	}

	sb.WriteString(diffContent)
	return sb.String()
}

// renderDiffTabCommitPreview renders commit info + file list when cursor is on a commit.
func (p *Plugin) renderDiffTabCommitPreview(commit CommitStatusInfo, width, height int) string {
	var sb strings.Builder

	hashStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	pushedLabel := dimText("local")
	if commit.Pushed {
		pushedLabel = styles.DiffAdd.Render("pushed")
	}

	sb.WriteString(styles.Title.Render("Commit"))
	sb.WriteString(" ")
	sb.WriteString(hashStyle.Render(commit.Hash))
	sb.WriteString(" ")
	sb.WriteString(pushedLabel)
	sb.WriteString("\n")
	sb.WriteString(commit.Subject)
	sb.WriteString("\n\n")

	linesUsed := 3

	// Show commit's file list if loaded
	if p.commitDetail != nil {
		files := p.commitDetail.Files
		sb.WriteString(styles.Muted.Render(fmt.Sprintf("Files (%d)", len(files))))
		sb.WriteString("\n")
		linesUsed++

		maxLines := height - linesUsed
		if maxLines < 0 {
			maxLines = 0
		}
		maxWidth := width - 2
		for i, file := range files {
			if i >= maxLines {
				break
			}
			statusIcon := "M"
			switch file.Status {
			case gitstatus.StatusAdded:
				statusIcon = "A"
			case gitstatus.StatusDeleted:
				statusIcon = "D"
			case gitstatus.StatusRenamed:
				statusIcon = "R"
			}
			var statusStyle lipgloss.Style
			switch statusIcon {
			case "A":
				statusStyle = styles.StatusStaged
			case "D":
				statusStyle = styles.StatusDeleted
			default:
				statusStyle = styles.StatusModified
			}
			fileName := file.Path
			fileRunes := []rune(fileName)
			if len(fileRunes) > maxWidth-4 {
				keep := maxWidth - 5
				if keep > 0 {
					fileName = "…" + string(fileRunes[len(fileRunes)-keep:])
				}
			}
			plainLine := fmt.Sprintf("%s %s", statusIcon, fileName)
			lineWidth := lipgloss.Width(plainLine)
			if lineWidth < maxWidth {
				plainLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(statusStyle.Render(plainLine))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(styles.Muted.Render("l/enter → view diffs"))
	} else {
		sb.WriteString(dimText("Loading files..."))
	}

	return sb.String()
}

// renderCommitFileList renders the file list for a drilled-into commit.
func (p *Plugin) renderCommitFileList(width, height int) string {
	var sb strings.Builder
	maxWidth := width - 2

	if p.commitDetail == nil {
		sb.WriteString(styles.Muted.Render("Loading commit files..."))
		return sb.String()
	}

	files := p.commitDetail.Files
	isActive := p.diffTabFocus == DiffTabFocusCommitFiles

	// Commit info at the top
	hash := p.commitDetail.ShortHash
	if hash == "" && len(p.commitDetail.Hash) >= 7 {
		hash = p.commitDetail.Hash[:7]
	}
	hashStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	sb.WriteString(styles.Muted.Render("←") + " " + hashStyle.Render(hash))
	sb.WriteString("\n")
	// Truncate subject to fit
	subject := p.commitDetail.Subject
	subjectRunes := []rune(subject)
	if len(subjectRunes) > maxWidth {
		subject = string(subjectRunes[:maxWidth-1]) + "…"
	}
	sb.WriteString(styles.Muted.Render(subject))
	sb.WriteString("\n")

	// Separator + file header
	sb.WriteString(styles.Muted.Render(strings.Repeat("─", maxWidth)))
	sb.WriteString("\n")
	filesHeader := fmt.Sprintf("Files (%d)", len(files))
	if isActive {
		sb.WriteString(styles.Title.Render(filesHeader))
	} else {
		sb.WriteString(styles.Muted.Render(filesHeader))
	}
	sb.WriteString("\n")

	if len(files) == 0 {
		sb.WriteString(dimText("No files in commit"))
		return sb.String()
	}

	// Scroll management (header uses 4 lines: hash, subject, separator, "Files (N)")
	contentHeight := height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}
	if p.commitFileCursor < p.commitFileScroll {
		p.commitFileScroll = p.commitFileCursor
	}
	if p.commitFileCursor >= p.commitFileScroll+contentHeight {
		p.commitFileScroll = p.commitFileCursor - contentHeight + 1
	}

	startIdx := p.commitFileScroll
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + contentHeight
	if endIdx > len(files) {
		endIdx = len(files)
	}

	for i := startIdx; i < endIdx; i++ {
		file := files[i]
		selected := i == p.commitFileCursor

		// Status icon
		statusIcon := "M"
		switch file.Status {
		case gitstatus.StatusAdded:
			statusIcon = "A"
		case gitstatus.StatusDeleted:
			statusIcon = "D"
		case gitstatus.StatusRenamed:
			statusIcon = "R"
		}

		// File path - rune-safe truncation
		fileName := file.Path
		statsStr := fmt.Sprintf("+%d/-%d", file.Additions, file.Deletions)
		fileRunes := []rune(fileName)
		availableWidth := maxWidth - 4
		if len(fileRunes)+len(statsStr)+1 > availableWidth {
			keepWidth := availableWidth - len(statsStr) - 2
			if keepWidth > 3 {
				fileName = "…" + string(fileRunes[len(fileRunes)-keepWidth:])
			}
		}

		if selected && isActive {
			plainLine := fmt.Sprintf("%s %s %s", statusIcon, fileName, statsStr)
			lineWidth := lipgloss.Width(plainLine)
			if lineWidth < maxWidth {
				plainLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(styles.ListItemSelected.Render(plainLine))
		} else {
			var statusStyle lipgloss.Style
			switch statusIcon {
			case "A":
				statusStyle = styles.StatusStaged
			case "D":
				statusStyle = styles.StatusDeleted
			case "R":
				statusStyle = lipgloss.NewStyle().Foreground(styles.Info)
			default:
				statusStyle = styles.StatusModified
			}
			plainLine := fmt.Sprintf("%s %s %s", statusIcon, fileName, statsStr)
			lineWidth := lipgloss.Width(plainLine)
			if lineWidth < maxWidth {
				plainLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(statusStyle.Render(plainLine))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderCommitFileDiffPane renders the diff for the selected file in a commit.
func (p *Plugin) renderCommitFileDiffPane(width, height int) string {
	if p.commitDetail == nil {
		return dimText("Loading...")
	}
	if len(p.commitDetail.Files) == 0 {
		return dimText("No files in commit")
	}
	if p.commitFileCursor < 0 || p.commitFileCursor >= len(p.commitDetail.Files) {
		return dimText("Select a file")
	}

	file := p.commitDetail.Files[p.commitFileCursor]
	var sb strings.Builder

	// Header with view mode indicator
	isActive := p.diffTabFocus == DiffTabFocusCommitDiff
	viewModeStr := "unified"
	if p.diffViewMode == DiffViewSideBySide {
		viewModeStr = "split"
	}
	headerStr := fmt.Sprintf("%s [%s]", file.Path, viewModeStr)
	if isActive {
		sb.WriteString(styles.Title.Render(headerStr))
	} else {
		sb.WriteString(styles.Muted.Render(headerStr))
	}
	sb.WriteString("\n\n")

	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	if p.commitFileParsed == nil {
		if p.commitFileDiffRaw == "" {
			sb.WriteString(dimText("Loading diff..."))
		} else {
			sb.WriteString(dimText("No diff content"))
		}
		return sb.String()
	}

	// Render diff based on view mode
	highlighter := gitstatus.NewSyntaxHighlighter(file.Path)
	var diffContent string
	if p.diffViewMode == DiffViewSideBySide {
		diffContent = gitstatus.RenderSideBySide(p.commitFileParsed, width, p.diffTabDiffScroll, contentHeight, p.diffTabHorizScroll, highlighter, false)
	} else {
		diffContent = gitstatus.RenderLineDiff(p.commitFileParsed, width, p.diffTabDiffScroll, contentHeight, p.diffTabHorizScroll, highlighter, false)
	}
	sb.WriteString(diffContent)

	return sb.String()
}

// selectedDiffTabFile returns the filename currently selected in the diff tab file list.
func (p *Plugin) selectedDiffTabFile() string {
	if p.multiFileDiff == nil || p.diffTabCursor < 0 || p.diffTabCursor >= len(p.multiFileDiff.Files) {
		return ""
	}
	return p.multiFileDiff.Files[p.diffTabCursor].FileName()
}

// diffTabFileCount returns the number of files in the diff tab's file list.
func (p *Plugin) diffTabFileCount() int {
	if p.multiFileDiff == nil {
		return 0
	}
	return len(p.multiFileDiff.Files)
}

// diffTabTotalItems returns the total navigable items (files + commits).
func (p *Plugin) diffTabTotalItems() int {
	count := p.diffTabFileCount()
	count += len(p.commitStatusList)
	return count
}

// parsedDiffForCurrentFile returns the ParsedDiff for the currently selected file, or nil.
func (p *Plugin) parsedDiffForCurrentFile() *gitstatus.ParsedDiff {
	if p.multiFileDiff == nil || p.diffTabCursor < 0 || p.diffTabCursor >= len(p.multiFileDiff.Files) {
		return nil
	}
	return p.multiFileDiff.Files[p.diffTabCursor].Diff
}

// countDiffTabDiffLines returns the total line count for the current per-file diff.
func (p *Plugin) countDiffTabDiffLines() int {
	if p.diffViewMode == DiffViewFullFile {
		if p.fullFileDiff != nil {
			return p.fullFileDiff.TotalLines()
		}
		// Fall back to parsed diff line count while full-file data is loading
		if p.diffTabParsedDiff != nil {
			return gitstatus.CountParsedDiffLines(p.diffTabParsedDiff)
		}
		return 0
	}
	if p.diffTabParsedDiff != nil {
		return gitstatus.CountParsedDiffLines(p.diffTabParsedDiff)
	}
	return 0
}

// renderDiffContentBasicWithHeight renders git diff with basic highlighting with explicit height.
func (p *Plugin) renderDiffContentBasicWithHeight(width, height int) string {
	lines := splitLines(p.diffContent)

	// Apply scroll offset
	start := p.diffTabDiffScroll
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
		line = ui.ExpandTabs(line, tabStopWidth)
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

		if lipgloss.Width(styledLine) > width {
			styledLine = p.truncateCache.Truncate(styledLine, width, "")
		}
		rendered = append(rendered, styledLine)
	}

	return strings.Join(rendered, "\n")
}

// jumpToNextFile jumps to the next file in the diff tab file list.
func (p *Plugin) jumpToNextFile() tea.Cmd {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) <= 1 {
		return nil
	}
	if p.diffTabCursor < len(p.multiFileDiff.Files)-1 {
		p.diffTabCursor++
		p.diffTabDiffScroll = 0
		p.diffTabHorizScroll = 0
		p.fullFileDiff = nil
		p.diffTabParsedDiff = p.parsedDiffForCurrentFile()
		if p.diffViewMode == DiffViewFullFile {
			return p.loadFullFileDiffForWorkspace()
		}
	}
	return nil
}

// jumpToPrevFile jumps to the previous file in the diff tab file list.
func (p *Plugin) jumpToPrevFile() tea.Cmd {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) <= 1 {
		return nil
	}
	if p.diffTabCursor > 0 {
		p.diffTabCursor--
		p.diffTabDiffScroll = 0
		p.diffTabHorizScroll = 0
		p.fullFileDiff = nil
		p.diffTabParsedDiff = p.parsedDiffForCurrentFile()
		if p.diffViewMode == DiffViewFullFile {
			return p.loadFullFileDiffForWorkspace()
		}
	}
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

// colorStatLine applies coloring to git --stat output lines.
// Colors the +/- bar graph characters green/red.
func (p *Plugin) colorStatLine(line string, width int) string {
	if len(line) == 0 {
		return line
	}

	// Truncate if needed
	if lipgloss.Width(line) > width {
		line = p.truncateCache.Truncate(line, width, "")
	}

	// Find the | separator that precedes the bar graph
	pipeIdx := strings.LastIndex(line, "|")
	if pipeIdx == -1 {
		// Summary line or no bar graph - render as-is
		return line
	}

	prefix := line[:pipeIdx+1]
	bar := line[pipeIdx+1:]

	// Color individual + and - characters in the bar portion
	var colored strings.Builder
	colored.WriteString(prefix)
	for _, ch := range bar {
		switch ch {
		case '+':
			colored.WriteString(styles.DiffAdd.Render("+"))
		case '-':
			colored.WriteString(styles.DiffRemove.Render("-"))
		default:
			colored.WriteRune(ch)
		}
	}
	return colored.String()
}

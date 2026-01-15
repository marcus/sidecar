package gitstatus

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// dividerWidth is the width of the draggable divider between panes.
const dividerWidth = 1

// calculatePaneWidths sets the sidebar and diff pane widths.
// If sidebarWidth is already set (from drag), only updates diffPaneWidth.
func (p *Plugin) calculatePaneWidths() {
	if !p.sidebarVisible {
		p.sidebarWidth = 0
		p.diffPaneWidth = p.width
		return
	}

	// RenderPanel handles borders internally, so only subtract divider
	available := p.width - dividerWidth

	// Only set default sidebarWidth if not yet initialized
	if p.sidebarWidth == 0 {
		p.sidebarWidth = available * 30 / 100
	}

	// Clamp sidebarWidth to valid bounds
	minWidth := 25
	maxWidth := available - 40 // Leave at least 40 for diff
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if p.sidebarWidth < minWidth {
		p.sidebarWidth = minWidth
	} else if p.sidebarWidth > maxWidth {
		p.sidebarWidth = maxWidth
	}

	// Calculate diffPaneWidth from remaining space
	p.diffPaneWidth = available - p.sidebarWidth
	if p.diffPaneWidth < 40 {
		p.diffPaneWidth = 40
	}
}

// renderThreePaneView creates the three-pane layout for git status.
func (p *Plugin) renderThreePaneView() string {
	p.calculatePaneWidths()

	// Pane height for panels (outer dimensions including borders)
	// Note: App footer is rendered by the app, not the plugin
	paneHeight := p.height
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height (excluding borders and header lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Clear and rebuild hit regions for mouse support
	p.mouseHandler.Clear()

	if p.sidebarVisible {
		// Register hit regions - tested in reverse order (last added = highest priority)
		// Sidebar region - lowest priority fallback
		p.mouseHandler.HitMap.AddRect(regionSidebar, 0, 0, p.sidebarWidth, p.height, nil)

		// Diff pane region (after divider) - medium priority
		diffX := p.sidebarWidth + dividerWidth
		p.mouseHandler.HitMap.AddRect(regionDiffPane, diffX, 0, p.diffPaneWidth, p.height, nil)

		// Pane divider region - HIGH PRIORITY (registered after panes so it wins in overlap)
		// Sidebar is Width(sidebarWidth), so occupies columns 0 to sidebarWidth-1
		// Divider is at column sidebarWidth
		dividerX := p.sidebarWidth
		dividerHitWidth := 3
		p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, 0, dividerHitWidth, p.height, nil)

		// Determine if panes are active based on focus
		sidebarActive := p.activePane == PaneSidebar
		diffActive := p.activePane != PaneSidebar

		sidebarContent := p.renderSidebar(innerHeight)
		diffContent := p.renderDiffPane(innerHeight)

		// Apply gradient border styles
		leftPane := styles.RenderPanel(sidebarContent, p.sidebarWidth, paneHeight, sidebarActive)

		// Render visible divider between panes
		// MarginTop(1) in renderDivider shifts it down, so use paneHeight directly
		divider := p.renderDivider(paneHeight)

		rightPane := styles.RenderPanel(diffContent, p.diffPaneWidth, paneHeight, diffActive)

		return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
	}

	// Full-width diff pane when sidebar is hidden
	p.mouseHandler.HitMap.AddRect(regionDiffPane, 0, 0, p.width, p.height, nil)

	diffContent := p.renderDiffPane(innerHeight)

	// Apply gradient border style (always active when full-width)
	return styles.RenderPanel(diffContent, p.diffPaneWidth, paneHeight, true)
}

// renderSidebar renders the left sidebar with files and commits.
func (p *Plugin) renderSidebar(visibleHeight int) string {
	var sb strings.Builder

	// Track Y position for mouse hit regions
	// Start at 3: 1 for pane border + 2 for header lines
	currentY := 3

	// Header with branch name (truncated to fit sidebar)
	header := styles.Title.Render("Files")
	if p.pushStatus != nil {
		if p.pushStatus.CurrentBranch != "" {
			branch := p.pushStatus.CurrentBranch
			// "Files " = 6 chars, leave 4 for padding = max branch length is sidebarWidth - 10
			maxLen := p.sidebarWidth - 10
			if maxLen > 0 && len(branch) > maxLen {
				branch = branch[:maxLen-1] + "…"
			}
			header += " " + styles.Muted.Render(branch)
		} else if p.pushStatus.DetachedHead {
			header += " " + styles.Muted.Render("(detached)")
		}
	}
	sb.WriteString(header)
	sb.WriteString("\n\n")

	entries := p.tree.AllEntries()
	if len(entries) == 0 {
		sb.WriteString(styles.Muted.Render("Working tree clean"))
		sb.WriteString("\n")
		currentY++
	} else {
		// Calculate space for files vs commits
		// Reserve ~30% for commits section (min 4 lines for header + 2-3 commits)
		commitsReserve := 5
		if len(p.recentCommits) > 3 {
			commitsReserve = 6
		}
		filesHeight := visibleHeight - commitsReserve - 2 // -2 for section headers
		if filesHeight < 3 {
			filesHeight = 3
		}

		// Render file sections
		lineNum := 0
		globalIdx := 0

		// Staged section
		if len(p.tree.Staged) > 0 && lineNum < filesHeight {
			sb.WriteString(p.renderSidebarSection("Staged", p.tree.Staged, &lineNum, &globalIdx, filesHeight, &currentY))
		}

		// Modified section
		if len(p.tree.Modified) > 0 && lineNum < filesHeight {
			if len(p.tree.Staged) > 0 {
				sb.WriteString("\n")
				lineNum++
				currentY++
			}
			sb.WriteString(p.renderSidebarSection("Modified", p.tree.Modified, &lineNum, &globalIdx, filesHeight, &currentY))
		}

		// Untracked section
		if len(p.tree.Untracked) > 0 && lineNum < filesHeight {
			if len(p.tree.Staged) > 0 || len(p.tree.Modified) > 0 {
				sb.WriteString("\n")
				lineNum++
				currentY++
			}
			sb.WriteString(p.renderSidebarSection("Untracked", p.tree.Untracked, &lineNum, &globalIdx, filesHeight, &currentY))
		}
	}

	// Separator
	sb.WriteString("\n")
	currentY++
	sb.WriteString(styles.Muted.Render(strings.Repeat("─", p.sidebarWidth-4)))
	sb.WriteString("\n")
	currentY++

	// Remote operation status (push/fetch/pull)
	if p.pushInProgress {
		sb.WriteString(styles.StatusInProgress.Render("Pushing..."))
		sb.WriteString("\n")
		currentY++
	} else if p.fetchInProgress {
		sb.WriteString(styles.StatusInProgress.Render("Fetching..."))
		sb.WriteString("\n")
		currentY++
	} else if p.pullInProgress {
		sb.WriteString(styles.StatusInProgress.Render("Pulling..."))
		sb.WriteString("\n")
		currentY++
	} else if p.pushSuccess {
		sb.WriteString(styles.StatusStaged.Render("✓ Pushed"))
		sb.WriteString("\n")
		currentY++
	} else if p.fetchSuccess {
		sb.WriteString(styles.StatusStaged.Render("✓ Fetched"))
		sb.WriteString("\n")
		currentY++
	} else if p.pullSuccess {
		sb.WriteString(styles.StatusStaged.Render("✓ Pulled"))
		sb.WriteString("\n")
		currentY++
	} else if p.pushError != "" {
		// Truncate error if too long (account for "✗ " prefix)
		errMsg := p.pushError
		maxLen := p.sidebarWidth - 8 // 2 for "✗ " prefix + 6 for padding
		if len(errMsg) > maxLen && maxLen > 3 {
			errMsg = errMsg[:maxLen-3] + "..."
		}
		sb.WriteString(styles.StatusDeleted.Render("✗ " + errMsg))
		sb.WriteString("\n")
		currentY++
	} else if p.fetchError != "" {
		errMsg := p.fetchError
		maxLen := p.sidebarWidth - 8
		if len(errMsg) > maxLen && maxLen > 3 {
			errMsg = errMsg[:maxLen-3] + "..."
		}
		sb.WriteString(styles.StatusDeleted.Render("✗ " + errMsg))
		sb.WriteString("\n")
		currentY++
	} else if p.pullError != "" {
		errMsg := p.pullError
		maxLen := p.sidebarWidth - 8
		if len(errMsg) > maxLen && maxLen > 3 {
			errMsg = errMsg[:maxLen-3] + "..."
		}
		sb.WriteString(styles.StatusDeleted.Render("✗ " + errMsg))
		sb.WriteString("\n")
		currentY++
	}

	// Recent commits section
	commitsAvailable := p.commitSectionCapacity(visibleHeight)
	sb.WriteString(p.renderRecentCommits(&currentY, commitsAvailable))

	return sb.String()
}

// renderSidebarSection renders a file section in the sidebar.
func (p *Plugin) renderSidebarSection(title string, entries []*FileEntry, lineNum, globalIdx *int, maxLines int, currentY *int) string {
	var sb strings.Builder

	// Section header with color based on type
	headerStyle := styles.Subtitle
	if title == "Staged" {
		headerStyle = styles.StatusStaged
	} else if title == "Modified" {
		headerStyle = styles.StatusModified
	}

	sb.WriteString(headerStyle.Render(fmt.Sprintf("%s (%d)", title, len(entries))))
	sb.WriteString("\n")
	*lineNum++
	*currentY++

	// Available width for file names
	maxWidth := p.sidebarWidth - 6 // Account for padding and cursor

	for _, entry := range entries {
		if *lineNum >= maxLines {
			break
		}

		selected := *globalIdx == p.cursor
		line := p.renderSidebarEntry(entry, selected, maxWidth)

		// Register hit region for this file entry
		p.mouseHandler.HitMap.AddRect(regionFile, 1, *currentY, p.sidebarWidth-2, 1, *globalIdx)

		sb.WriteString(line)
		sb.WriteString("\n")
		*lineNum++
		*globalIdx++
		*currentY++
	}

	return sb.String()
}

// renderSidebarEntry renders a single file entry in the sidebar.
func (p *Plugin) renderSidebarEntry(entry *FileEntry, selected bool, maxWidth int) string {

	// Status indicator
	var statusStyle lipgloss.Style
	switch entry.Status {
	case StatusModified:
		statusStyle = styles.StatusModified
	case StatusAdded:
		statusStyle = styles.StatusStaged
	case StatusDeleted:
		statusStyle = styles.StatusDeleted
	case StatusRenamed:
		statusStyle = styles.StatusStaged
	case StatusUntracked:
		statusStyle = styles.StatusUntracked
	default:
		statusStyle = styles.Muted
	}

	status := statusStyle.Render(string(entry.Status))

	// Handle folder entries specially
	if entry.IsFolder {
		folderName := entry.Path
		fileCount := len(entry.Children)
		countStr := fmt.Sprintf("(%d)", fileCount)

		// Only show expand/collapse indicator if folder has children
		indicator := ""
		if fileCount > 0 {
			indicator = "▶ "
			if entry.IsExpanded {
				indicator = "▼ "
			}
		}

		// Calculate available width
		availableWidth := maxWidth - 2 - len(indicator) // status + indicator + spacing
		displayName := folderName
		if len(folderName)+len(countStr)+1 > availableWidth && availableWidth > 10 {
			displayName = folderName[:availableWidth-len(countStr)-4] + "…/"
		}

		if selected {
			plainLine := fmt.Sprintf("%s %s%s %s", string(entry.Status), indicator, displayName, countStr)
			if len(plainLine) < maxWidth {
				plainLine += strings.Repeat(" ", maxWidth-len(plainLine))
			}
			return styles.ListItemSelected.Render(plainLine)
		}

		return styles.ListItemNormal.Render(fmt.Sprintf("%s %s%s %s", status, indicator, displayName, styles.Muted.Render(countStr)))
	}

	// Path - truncate if needed
	path := entry.Path
	availableWidth := maxWidth - 2 // status + space
	if len(path) > availableWidth && availableWidth > 3 {
		path = "…" + path[len(path)-availableWidth+1:]
	}

	if selected {
		plainLine := fmt.Sprintf("%s %s", string(entry.Status), path)
		if len(plainLine) < maxWidth {
			plainLine += strings.Repeat(" ", maxWidth-len(plainLine))
		}
		return styles.ListItemSelected.Render(plainLine)
	}

	return styles.ListItemNormal.Render(fmt.Sprintf("%s %s", status, path))
}

// renderRecentCommits renders the recent commits section in the sidebar.
// maxVisible is the maximum number of commits that can be displayed.
func (p *Plugin) renderRecentCommits(currentY *int, maxVisible int) string {
	var sb strings.Builder

	// Section header with push status and filter indicator
	header := "Recent Commits"
	if p.historyFilterActive {
		// Show filter indicator
		var filterParts []string
		if p.historyFilterAuthor != "" {
			filterParts = append(filterParts, "author:"+truncateStr(p.historyFilterAuthor, 10))
		}
		if p.historyFilterPath != "" {
			filterParts = append(filterParts, "path:"+truncateStr(p.historyFilterPath, 10))
		}
		if len(filterParts) > 0 {
			header = fmt.Sprintf("Commits %s", styles.StatusModified.Render("["+strings.Join(filterParts, ", ")+"]"))
		}
	} else if p.pushStatus != nil {
		status := p.pushStatus.FormatAheadBehind()
		if status != "" {
			header = fmt.Sprintf("Recent Commits %s", styles.StatusModified.Render(status))
		}
	}
	// Add graph indicator if enabled
	if p.showCommitGraph {
		header += " " + styles.Muted.Render("[graph]")
	}
	headerLine := styles.Title.Render(header)
	headerWidth := p.sidebarWidth - 4
	if headerWidth > 0 {
		headerLine = truncateStyledLine(headerLine, headerWidth)
	}
	sb.WriteString(headerLine)
	sb.WriteString("\n")
	*currentY++

	// Use filtered commits if filter is active, otherwise recent commits
	commits := p.recentCommits
	if p.historyFilterActive && p.filteredCommits != nil {
		commits = p.filteredCommits
	}

	if len(commits) == 0 {
		if p.historyFilterActive {
			sb.WriteString(styles.Muted.Render("No matching commits"))
		} else {
			sb.WriteString(styles.Muted.Render("No commits"))
		}
		return sb.String()
	}

	// Calculate graph column width if graph is shown
	graphWidth := 0
	if p.showCommitGraph && len(p.commitGraphLines) > 0 {
		for _, gl := range p.commitGraphLines {
			if gl.Width > graphWidth {
				graphWidth = gl.Width
			}
		}
		if graphWidth > 12 {
			graphWidth = 12 // Cap graph width to prevent overflow
		}
	}

	// Cursor selection: cursor indexes files first, then commits
	fileCount := len(p.tree.AllEntries())
	maxWidth := p.sidebarWidth - 4

	// Calculate visible range based on scroll offset
	startIdx := p.commitScrollOff
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(commits) {
		startIdx = len(commits) - 1
		if startIdx < 0 {
			startIdx = 0
		}
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(commits) {
		endIdx = len(commits)
	}

	for i := startIdx; i < endIdx; i++ {
		commit := commits[i]
		// Use absolute commit index for cursor comparison
		selected := p.cursor == fileCount+i

		// Graph column (if enabled)
		var graphStr string
		var graphVisualWidth int
		if p.showCommitGraph && i < len(p.commitGraphLines) {
			graphStr = p.renderGraphLine(p.commitGraphLines[i], graphWidth)
			graphVisualWidth = graphWidth
		}

		// Push indicator: ↑ for unpushed, nothing for pushed
		var indicator string
		if !commit.Pushed {
			indicator = styles.StatusModified.Render("↑") + " "
		} else {
			indicator = "  " // Two spaces to align with indicator
		}

		// Format: "[graph] ↑ abc1234 commit message..."
		hash := styles.Code.Render(commit.Hash[:7])
		msgWidth := maxWidth - 12 - graphVisualWidth // indicator + hash + space + graph
		if msgWidth < 10 {
			msgWidth = 10
		}
		msg := commit.Subject
		if len(msg) > msgWidth && msgWidth > 3 {
			msg = msg[:msgWidth-1] + "…"
		}

		// Register hit region for this commit with ABSOLUTE index
		p.mouseHandler.HitMap.AddRect(regionCommit, 1, *currentY, p.sidebarWidth-2, 1, i)

		if selected {
			plainIndicator := "  "
			if !commit.Pushed {
				plainIndicator = "↑ "
			}
			// For selected lines, include graph prefix without styling (will be styled by selection)
			graphPlain := ""
			if graphStr != "" {
				graphPlain = p.renderGraphLinePlain(p.commitGraphLines[i], graphWidth)
			}
			plainLine := fmt.Sprintf("%s%s%s %s", graphPlain, plainIndicator, commit.Hash[:7], msg)
			// Pad to full width
			lineWidth := lipgloss.Width(plainLine)
			if lineWidth < maxWidth {
				plainLine += strings.Repeat(" ", maxWidth-lineWidth)
			}
			sb.WriteString(styles.ListItemSelected.Render(plainLine))
		} else {
			line := fmt.Sprintf("%s%s%s %s", graphStr, indicator, hash, msg)
			sb.WriteString(styles.ListItemNormal.Render(line))
		}
		*currentY++
		if i < endIdx-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderGraphLine formats a GraphLine to a styled fixed-width string.
func (p *Plugin) renderGraphLine(gl GraphLine, width int) string {
	var sb strings.Builder
	commitStyle := lipgloss.NewStyle().Foreground(styles.Accent)
	for _, ch := range gl.Chars {
		switch ch {
		case '*':
			sb.WriteString(commitStyle.Render("●"))
		case '|':
			sb.WriteString(styles.Muted.Render("│"))
		case '\\':
			sb.WriteString(styles.Muted.Render("╲"))
		case '/':
			sb.WriteString(styles.Muted.Render("╱"))
		case '_':
			sb.WriteString(styles.Muted.Render("─"))
		default:
			sb.WriteRune(ch)
		}
	}
	// Pad to fixed width
	result := sb.String()
	visualWidth := lipgloss.Width(result)
	if visualWidth < width {
		result += strings.Repeat(" ", width-visualWidth)
	}
	return result
}

// renderGraphLinePlain formats a GraphLine to a plain fixed-width string (for selected items).
func (p *Plugin) renderGraphLinePlain(gl GraphLine, width int) string {
	var sb strings.Builder
	for _, ch := range gl.Chars {
		switch ch {
		case '*':
			sb.WriteRune('●')
		case '|':
			sb.WriteRune('│')
		case '\\':
			sb.WriteRune('╲')
		case '/':
			sb.WriteRune('╱')
		case '_':
			sb.WriteRune('─')
		default:
			sb.WriteRune(ch)
		}
	}
	// Pad to fixed width
	result := sb.String()
	if len(result) < width {
		result += strings.Repeat(" ", width-len(result))
	}
	return result
}

// renderDiffPane renders the right diff pane.
func (p *Plugin) renderDiffPane(visibleHeight int) string {
	// If previewing a commit, render commit preview instead of diff
	if p.previewCommit != nil && p.cursorOnCommit() {
		return p.renderCommitPreview(visibleHeight)
	}

	var sb strings.Builder

	// Width: pane content width - padding (2) - extra buffer (2)
	// The pane style applies Padding(0,1) which takes 2 chars from content area
	diffWidth := p.diffPaneWidth - 4
	if diffWidth < 40 {
		diffWidth = 40
	}

	// Header with view mode and scroll indicators
	viewModeStr := "unified"
	if p.diffPaneViewMode == DiffViewSideBySide {
		viewModeStr = "split"
	}
	header := "Diff"
	if p.selectedDiffFile != "" {
		header = truncateDiffPath(p.selectedDiffFile, p.diffPaneWidth-20) // Leave room for mode + indicators
	}

	// Calculate scroll indicators for side-by-side mode
	scrollIndicator := ""
	if p.diffPaneViewMode == DiffViewSideBySide && p.diffPaneParsedDiff != nil {
		// Calculate content width for side-by-side (each panel)
		panelWidth := (diffWidth - 3) / 2
		lineNoWidth := 5
		contentWidth := panelWidth - lineNoWidth - 2

		clipInfo := GetSideBySideClipInfo(p.diffPaneParsedDiff, contentWidth, p.diffPaneHorizScroll)
		if clipInfo.HasMoreLeft || clipInfo.HasMoreRight {
			leftArrow := " "
			rightArrow := " "
			if clipInfo.HasMoreLeft {
				leftArrow = "◀"
			}
			if clipInfo.HasMoreRight {
				rightArrow = "▶"
			}
			scrollIndicator = " " + styles.Muted.Render(leftArrow+rightArrow)
		}
	}

	header = fmt.Sprintf("%s [%s]%s", header, viewModeStr, scrollIndicator)
	sb.WriteString(styles.Title.Render(header))
	sb.WriteString("\n\n")

	if p.selectedDiffFile == "" {
		sb.WriteString(styles.Muted.Render("Select a file to view diff"))
		return sb.String()
	}

	if p.diffPaneParsedDiff == nil {
		sb.WriteString(styles.Muted.Render("Loading diff..."))
		return sb.String()
	}

	// Render the diff content
	contentHeight := visibleHeight - 2 // Account for header
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Render diff based on view mode
	highlighter := p.getHighlighter(p.selectedDiffFile)
	var diffContent string
	if p.diffPaneViewMode == DiffViewSideBySide {
		diffContent = RenderSideBySide(p.diffPaneParsedDiff, diffWidth, p.diffPaneScroll, contentHeight, p.diffPaneHorizScroll, highlighter)
	} else {
		diffContent = RenderLineDiff(p.diffPaneParsedDiff, diffWidth, p.diffPaneScroll, contentHeight, p.diffPaneHorizScroll, highlighter)
	}
	// Force truncate each line to prevent wrapping
	lines := strings.Split(diffContent, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > diffWidth {
			// Truncate the line to fit
			lines[i] = truncateStyledLine(line, diffWidth-3) + "..."
		}
	}
	sb.WriteString(strings.Join(lines, "\n"))

	return sb.String()
}

// renderCommitPreview renders commit detail in the right pane.
func (p *Plugin) renderCommitPreview(visibleHeight int) string {
	var sb strings.Builder

	c := p.previewCommit
	if c == nil {
		sb.WriteString(styles.Muted.Render("Loading commit..."))
		return sb.String()
	}

	maxWidth := p.diffPaneWidth - 4

	// Track Y position for hit regions
	// Y=0 is pane border, Y=1 is first content line
	currentY := 1

	// Calculate X offset for diff pane content
	diffPaneX := 1 // Default when sidebar hidden (just pane border)
	if p.sidebarVisible {
		diffPaneX = p.sidebarWidth + dividerWidth + 1 // sidebar + divider + pane border
	}

	// Commit hash badge style
	hashBadge := lipgloss.NewStyle().
		Foreground(styles.Accent).
		Background(styles.BgSecondary).
		Padding(0, 1).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(styles.TextMuted)

	// Header with styled commit hash
	sb.WriteString(styles.Title.Render("Commit "))
	sb.WriteString(hashBadge.Render(c.ShortHash))
	sb.WriteString("\n\n")
	currentY += 2 // header line + blank line from \n\n

	// Author with icon-like prefix
	authorStr := c.Author
	if len(authorStr) > maxWidth-12 {
		authorStr = authorStr[:maxWidth-15] + "..."
	}
	sb.WriteString(labelStyle.Render("󰀄 ")) // Author icon
	sb.WriteString(styles.Body.Render(authorStr))
	sb.WriteString("\n")
	currentY++

	// Date with icon-like prefix
	sb.WriteString(labelStyle.Render("󰃰 ")) // Calendar icon
	sb.WriteString(styles.Muted.Render(RelativeTime(c.Date)))
	sb.WriteString("\n\n")
	currentY += 2 // date + blank line

	// Subject in bold
	subject := c.Subject
	if len(subject) > maxWidth-2 {
		subject = subject[:maxWidth-5] + "..."
	}
	subjectStyle := lipgloss.NewStyle().
		Foreground(styles.TextPrimary).
		Bold(true)
	sb.WriteString(subjectStyle.Render(subject))
	sb.WriteString("\n")
	currentY++

	// Body (if present, truncated)
	if c.Body != "" {
		sb.WriteString("\n")
		currentY++
		bodyLines := strings.Split(strings.TrimSpace(c.Body), "\n")
		maxBodyLines := 3
		for i, line := range bodyLines {
			if i >= maxBodyLines {
				sb.WriteString(styles.Muted.Render("..."))
				sb.WriteString("\n")
				currentY++
				break
			}
			if len(line) > maxWidth-2 {
				line = line[:maxWidth-5] + "..."
			}
			sb.WriteString(styles.Muted.Render(line))
			sb.WriteString("\n")
			currentY++
		}
	}

	// Separator with subtle styling
	sb.WriteString("\n")
	currentY++
	separator := lipgloss.NewStyle().Foreground(styles.BorderNormal)
	sb.WriteString(separator.Render(strings.Repeat("─", maxWidth)))
	sb.WriteString("\n")
	currentY++

	// Files header with stats
	statsLine := fmt.Sprintf("Files (%d)", len(c.Files))
	if c.Stats.Additions > 0 || c.Stats.Deletions > 0 {
		addStr := styles.DiffAdd.Render(fmt.Sprintf("+%d", c.Stats.Additions))
		delStr := styles.DiffRemove.Render(fmt.Sprintf("-%d", c.Stats.Deletions))
		statsLine = fmt.Sprintf("Files (%d)  %s %s", len(c.Files), addStr, delStr)
	}
	sb.WriteString(styles.Subtitle.Render(statsLine))
	sb.WriteString("\n")
	currentY++

	// Calculate remaining height for file list
	linesUsed := 10 // header, metadata, subject, separator, files header
	if c.Body != "" {
		bodyLineCount := len(strings.Split(strings.TrimSpace(c.Body), "\n"))
		if bodyLineCount > 3 {
			bodyLineCount = 4 // includes "..."
		}
		linesUsed += bodyLineCount + 1
	}
	fileListHeight := visibleHeight - linesUsed
	if fileListHeight < 3 {
		fileListHeight = 3
	}

	// Files list with cursor
	if len(c.Files) == 0 {
		sb.WriteString(styles.Muted.Render("No files changed"))
	} else {
		start := p.previewCommitScroll
		if start >= len(c.Files) {
			start = 0
		}
		end := start + fileListHeight
		if end > len(c.Files) {
			end = len(c.Files)
		}

		for i := start; i < end; i++ {
			file := c.Files[i]
			selected := i == p.previewCommitCursor && p.activePane == PaneDiff

			// Register hit region for this file (using absolute index into Files)
			p.mouseHandler.HitMap.AddRect(regionCommitFile, diffPaneX, currentY, p.diffPaneWidth-2, 1, i)

			line := p.renderCommitPreviewFile(file, selected, maxWidth-4)
			sb.WriteString(line)
			sb.WriteString("\n")
			currentY++
		}
	}

	return sb.String()
}

// renderCommitPreviewFile renders a single file in the commit preview.
func (p *Plugin) renderCommitPreviewFile(file CommitFile, selected bool, maxWidth int) string {
	// Status indicator with color
	var statusStyle lipgloss.Style
	switch file.Status {
	case StatusModified:
		statusStyle = styles.StatusModified
	case StatusAdded:
		statusStyle = styles.StatusStaged
	case StatusDeleted:
		statusStyle = styles.StatusDeleted
	case StatusRenamed:
		statusStyle = styles.StatusStaged
	default:
		statusStyle = styles.Muted
	}
	status := statusStyle.Render(string(file.Status))

	// Path - truncate if needed
	path := file.Path
	pathWidth := maxWidth - 4 // status + spacing
	if len(path) > pathWidth && pathWidth > 3 {
		path = "…" + path[len(path)-pathWidth+1:]
	}

	if selected {
		plainLine := fmt.Sprintf("%s %s", string(file.Status), path)
		if len(plainLine) < maxWidth {
			plainLine += strings.Repeat(" ", maxWidth-len(plainLine))
		}
		return styles.ListItemSelected.Render(plainLine)
	}

	return styles.ListItemNormal.Render(fmt.Sprintf("%s %s", status, path))
}

// truncateStr truncates a string to maxLen characters with ellipsis.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

// truncateStyledLine truncates a line that may contain ANSI codes to a visual width.
func truncateStyledLine(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	// Use ANSI-aware truncation (not MaxWidth which wraps)
	return ansi.Truncate(s, maxWidth, "")
}

// truncateDiffPath shortens a path to fit width (rune-based for Unicode safety).
func truncateDiffPath(path string, maxWidth int) string {
	runes := []rune(path)
	if len(runes) <= maxWidth {
		return path
	}
	if maxWidth < 10 {
		return string(runes[:maxWidth])
	}
	return "…" + string(runes[len(runes)-maxWidth+1:])
}

// renderDivider renders the vertical divider between panes.
func (p *Plugin) renderDivider(height int) string {
	// Use a subtle vertical bar as the divider
	// MarginTop(1) shifts it down to align with pane content (below top border)
	dividerStyle := lipgloss.NewStyle().
		Foreground(styles.BorderNormal).
		MarginTop(1)

	// Build vertical bar with exact height
	var sb strings.Builder
	for i := 0; i < height; i++ {
		sb.WriteString("│")
		if i < height-1 {
			sb.WriteString("\n")
		}
	}

	return dividerStyle.Render(sb.String())
}

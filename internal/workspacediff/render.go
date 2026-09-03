package workspacediff

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// RenderOpts controls optional chrome the project plugin needs and the
// global preview does not: hit regions, list-pane width, host paint.
type RenderOpts struct {
	Hit          func(id string, x, y, w, h int, data any)
	ContentBaseX int
	BaseY        int
	PanelHeight  int
	Truncate     func(string, int, string) string
	// PaintFile draws one file body. workspacediff cannot import gitstatus;
	// the project host supplies RenderLineDiff / RenderSideBySide /
	// RenderFullFileSideBySide here. Empty return falls back to renderRaw.
	PaintFile func(name, raw string, mode ViewMode, width, height, scroll, horiz int) string
	// Handle is the list/hunk divider's hover/drag state.
	Handle ui.HandleState
}

const (
	RegionDivider      = "diff-tab-divider"
	RegionFile         = "diff-tab-file"
	RegionCommit       = "diff-tab-commit"
	RegionDiffPane     = "diff-tab-diff-pane"
	RegionPreviewFile  = "diff-tab-preview-file"
	RegionFileListPane = "diff-tab-filelist-pane"
	RegionCommitBack   = "commit-file-back"
	RegionCommitFile   = "commit-file-item"
	RegionCommitDiff   = "commit-file-diff-pane"
	RegionMinimap      = "diff-tab-minimap"
	dividerHitWidth    = 3
)

func dimText(s string) string { return styles.Muted.Render(s) }

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

func (v *View) baseRef() string {
	if v.Snapshot != nil && v.Snapshot.BaseRef != "" {
		return v.Snapshot.BaseRef
	}
	return "resolved base"
}

// Render draws the working-tree + commits Diff view.
//
// The body is inset by ContentInset columns on both sides, the same single
// column the issue pane keeps, so the Diff leaf does not sit flush against its
// neighbour's border. The inset is applied here and in ContentBox alone: every
// piece of column arithmetic below — the list/diff divider, the minimap, the
// side-by-side split, and the hit regions the host registers — works in the
// inner box, so what is drawn and what is clickable cannot drift apart.
func (v *View) Render(width, height int, opts RenderOpts) string {
	inner := contentWidth(width)
	if inner < width {
		opts.ContentBaseX += ContentInset
		// The inset is part of the frame a pointer lands on, so the selection
		// is recorded and painted after it: the columns the engine names are
		// the columns the reader sees.
		return v.paintSelection(indentLines(v.render(inner, height, opts), ContentInset), width, height)
	}
	return v.paintSelection(v.render(width, height, opts), width, height)
}

// indentLines shifts every line right by pad columns.
func indentLines(content string, pad int) string {
	if pad <= 0 || content == "" {
		return content
	}
	prefix := strings.Repeat(" ", pad)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func (v *View) render(width, height int, opts RenderOpts) string {
	switch v.State {
	case LoadStateLoading:
		return dimText("Loading diff…")
	case LoadStateError:
		err := v.Error
		if opts.Truncate != nil {
			err = opts.Truncate(err, width, "…")
		}
		return styles.StatusDeleted.Render("Error loading diff") + "\n" + err
	}
	if v.Scope == ScopeAggregate && v.Target.Kind == TargetWorkingTree {
		return v.renderAggregate(width, height)
	}

	if v.Target.Kind == TargetCommit {
		return v.renderCommitRoot(width, height, opts)
	}

	hasFiles := len(v.Files) > 0
	hasCommits := len(v.Commits) > 0
	if !hasFiles && !hasCommits {
		if v.Raw == "" {
			if v.Target.Kind == TargetRange {
				return dimText(v.rangeLabel() + ": no changes")
			}
			if v.Scope == ScopeCommits {
				return dimText(fmt.Sprintf("Commits unique to %s: none", v.baseRef()))
			}
			return dimText("Working Tree vs HEAD: clean")
		}
		return v.renderRaw(v.Content, width, height, opts)
	}

	if width < CollapseThreshold {
		return v.renderCollapsed(width, height, opts)
	}

	listWidth := v.resolvedListWidth(width)
	diffPaneWidth := width - listWidth - 1
	if diffPaneWidth < 10 {
		diffPaneWidth = 10
	}
	rightX := opts.ContentBaseX + listWidth + 1

	var leftPane, rightPane string
	if v.Focus == FocusCommitFiles || v.Focus == FocusCommitDiff {
		leftPane = v.renderCommitFileList(listWidth, height, opts.ContentBaseX, opts.BaseY, opts)
		rightPane = v.renderCommitFileDiffPane(diffPaneWidth, height, rightX, opts.BaseY, opts)
	} else {
		leftPane = v.renderFileList(listWidth, height, opts.ContentBaseX, opts.BaseY, opts)
		rightPane = v.renderDiffPane(diffPaneWidth, height, rightX, opts.BaseY, opts)
	}
	leftPane = padToHeight(leftPane, height, listWidth)
	rightPane = padToHeight(rightPane, height, diffPaneWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, ui.RenderHandle(height, true, opts.Handle), rightPane)
}

func (v *View) renderCommitRoot(width, height int, opts RenderOpts) string {
	if v.CommitDetail == nil {
		return v.commitDetailPlaceholder(width, opts)
	}
	if width < CollapseThreshold {
		if v.Focus == FocusCommitDiff {
			return v.renderCommitFileDiffPane(width, height, 0, 0, opts)
		}
		return v.renderCommitFileList(width, height, 0, 0, opts)
	}
	listWidth := v.resolvedListWidth(width)
	diffPaneWidth := width - listWidth - 1
	if diffPaneWidth < 10 {
		diffPaneWidth = 10
	}
	rightX := opts.ContentBaseX + listWidth + 1
	leftPane := padToHeight(v.renderCommitFileList(listWidth, height, opts.ContentBaseX, opts.BaseY, opts), height, listWidth)
	rightPane := padToHeight(v.renderCommitFileDiffPane(diffPaneWidth, height, rightX, opts.BaseY, opts), height, diffPaneWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, ui.RenderHandle(height, true, opts.Handle), rightPane)
}

func (v *View) rangeLabel() string {
	label := v.Target.TabLabel()
	if label == "" || label == "Range" {
		dots := v.Target.Dots
		if dots != "..." {
			dots = ".."
		}
		if v.Target.A != "" && v.Target.B != "" {
			return v.Target.A + dots + v.Target.B
		}
		return "Range"
	}
	return label
}

func (v *View) renderCollapsed(width, height int, opts RenderOpts) string {
	switch v.Focus {
	case FocusDiff:
		return v.renderDiffPane(width, height, 0, 0, opts)
	case FocusCommitFiles:
		return v.renderCommitFileList(width, height, 0, 0, opts)
	case FocusCommitDiff:
		return v.renderCommitFileDiffPane(width, height, 0, 0, opts)
	default:
		return v.renderFileList(width, height, 0, 0, opts)
	}
}

func (v *View) renderFileList(width, height, baseX, baseY int, opts RenderOpts) string {
	var sb strings.Builder
	files := v.fileRows()
	commits := v.Commits
	fileListActive := v.Focus == FocusFileList
	maxWidth := width - 2

	if opts.Hit != nil && baseX > 0 {
		opts.Hit(RegionFileListPane, baseX, baseY, width, height, nil)
	}

	headerText := fmt.Sprintf("Working Tree vs HEAD (%d)", len(files))
	if v.Target.Kind == TargetRange {
		headerText = fmt.Sprintf("%s (%d)", v.rangeLabel(), len(files))
		commits = nil
	} else if v.Scope == ScopeCommits {
		headerText = fmt.Sprintf("Commits vs %s (%d)", v.baseRef(), len(commits))
	} else if v.Snapshot != nil && v.Snapshot.Truncated {
		headerText = fmt.Sprintf("Working Tree vs HEAD (%d) [untracked caps: %d files, %d B/file, %d B total; %d omitted]",
			len(files), MaxUntrackedFiles, MaxUntrackedFileSize, MaxUntrackedTotalBytes, v.Snapshot.UntrackedOmitted)
	}
	if fileListActive {
		sb.WriteString(styles.Title.Render(headerText))
	} else {
		sb.WriteString(styles.Muted.Render(headerText))
	}
	sb.WriteString("\n")
	linesUsed := 1

	commitLines := 0
	if len(commits) > 0 {
		commitLines = 2 + len(commits)
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

	startIdx := v.Scroll
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + filesHeight
	if endIdx > len(files) {
		endIdx = len(files)
	}

	for i := startIdx; i < endIdx; i++ {
		file := files[i]
		selected := i == v.Cursor
		if opts.Hit != nil && baseX > 0 {
			opts.Hit(RegionFile, baseX, baseY+linesUsed, width, 1, i)
		}
		statusIcon := "M"
		if file.Additions > 0 && file.Deletions == 0 {
			statusIcon = "A"
		} else if file.Additions == 0 && file.Deletions > 0 {
			statusIcon = "D"
		}
		fileName := file.Path
		statsStr := fmt.Sprintf("+%d/-%d", file.Additions, file.Deletions)
		availableWidth := maxWidth - 4
		if lipgloss.Width(fileName)+lipgloss.Width(statsStr)+1 > availableWidth {
			keepWidth := availableWidth - len(statsStr) - 2
			if keepWidth > 3 {
				fileName = "…" + ansi.TruncateLeft(fileName, lipgloss.Width(fileName)-keepWidth, "")
			}
		}
		if selected && fileListActive {
			plainLine := fmt.Sprintf("%s %s %s", statusIcon, fileName, statsStr)
			if gap := maxWidth - lipgloss.Width(plainLine); gap > 0 {
				plainLine += strings.Repeat(" ", gap)
			}
			sb.WriteString(styles.ListItemSelected.Render(plainLine))
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
			if gap := maxWidth - lipgloss.Width(styledLine); gap > 0 {
				styledLine += strings.Repeat(" ", gap)
			}
			sb.WriteString(styledLine)
		}
		sb.WriteString("\n")
		linesUsed++
	}

	if len(commits) > 0 {
		sb.WriteString(styles.Muted.Render(strings.Repeat("─", maxWidth)))
		sb.WriteString("\n")
		linesUsed++
		sb.WriteString(styles.Title.Render(fmt.Sprintf("Commits (%d)", len(commits))))
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
			selected := (len(files) + i) == v.Cursor
			if opts.Hit != nil && baseX > 0 {
				opts.Hit(RegionCommit, baseX, baseY+linesUsed, width, 1, len(files)+i)
			}
			hash := commit.Hash
			if len(hash) > 7 {
				hash = hash[:7]
			}
			subject := commit.Subject
			subjectWidth := maxWidth - 12
			if subjectWidth < 10 {
				subjectWidth = 10
			}
			if lipgloss.Width(subject) > subjectWidth {
				subject = ansi.Truncate(subject, subjectWidth, "…")
			}
			if selected && fileListActive {
				plainIndicator := "○ "
				if commit.Pushed {
					plainIndicator = "↑ "
				}
				plainLine := fmt.Sprintf("%s%s %s", plainIndicator, hash, subject)
				if gap := maxWidth - lipgloss.Width(plainLine); gap > 0 {
					plainLine += strings.Repeat(" ", gap)
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
				if gap := maxWidth - lipgloss.Width(styledLine); gap > 0 {
					styledLine += strings.Repeat(" ", gap)
				}
				sb.WriteString(styledLine)
			}
			sb.WriteString("\n")
			linesUsed++
		}
	}
	return sb.String()
}

func (v *View) renderDiffPane(width, height, baseX, baseY int, opts RenderOpts) string {
	if v.Cursor >= v.FileCount() {
		commit, ok := v.SelectedCommit()
		if !ok {
			return dimText("Select a file to view diff")
		}
		return v.renderCommitPreview(commit, width, height, baseX, baseY, opts)
	}
	name := v.selectedFileName()
	var sb strings.Builder
	headerStr := fmt.Sprintf("%s [%s]", name, v.viewModeLabel())
	if v.Focus == FocusDiff {
		sb.WriteString(styles.Title.Render(headerStr))
	} else {
		sb.WriteString(styles.Muted.Render(headerStr))
	}
	sb.WriteString("\n\n")
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	sb.WriteString(v.renderFileDiff(name, v.selectedFileRaw(), width, contentHeight, opts))
	if opts.Hit != nil && baseX > 0 {
		opts.Hit(RegionDiffPane, baseX, baseY, width, height, nil)
	}
	return sb.String()
}

func (v *View) viewModeLabel() string {
	switch v.ViewMode {
	case ViewSideBySide:
		return "split"
	case ViewFullFile:
		return "full-file"
	default:
		return "unified"
	}
}

func (v *View) renderFileDiff(name, raw string, width, height int, opts RenderOpts) string {
	if opts.PaintFile != nil {
		if painted := opts.PaintFile(name, raw, v.ViewMode, width, height, v.DiffScroll, v.HorizScroll); painted != "" {
			return painted
		}
	}
	if raw != "" {
		return v.renderRaw(raw, width, height, opts)
	}
	return dimText("No diff data")
}

func (v *View) renderCommitFileList(width, height, baseX, baseY int, opts RenderOpts) string {
	var sb strings.Builder
	maxWidth := width - 2
	if v.CommitDetail == nil {
		sb.WriteString(v.commitDetailPlaceholder(width, opts))
		return sb.String()
	}
	files := v.CommitDetail.Files
	isActive := v.Focus == FocusCommitFiles
	if opts.Hit != nil && baseX > 0 {
		opts.Hit(RegionFileListPane, baseX, baseY, width, height, nil)
	}
	hash := v.CommitDetail.ShortHash
	if hash == "" && len(v.CommitDetail.Hash) >= 7 {
		hash = v.CommitDetail.Hash[:7]
	}
	hashStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	if v.Target.Kind == TargetCommit {
		sb.WriteString(styles.Title.Render("Commit ") + hashStyle.Render(hash))
	} else {
		sb.WriteString(styles.Muted.Render("←") + " " + hashStyle.Render(hash))
		if opts.Hit != nil && baseX > 0 {
			opts.Hit(RegionCommitBack, baseX, baseY, width, 1, nil)
		}
	}
	sb.WriteString("\n")
	subject := v.CommitDetail.Subject
	subjectRunes := []rune(subject)
	if len(subjectRunes) > maxWidth && maxWidth > 1 {
		subject = string(subjectRunes[:maxWidth-1]) + "…"
	}
	sb.WriteString(styles.Muted.Render(subject))
	sb.WriteString("\n")
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
	contentHeight := height - commitFileListHeaderLines
	if contentHeight < 1 {
		contentHeight = 1
	}
	startIdx := v.CommitFileScroll
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + contentHeight
	if endIdx > len(files) {
		endIdx = len(files)
	}
	for i := startIdx; i < endIdx; i++ {
		file := files[i]
		selected := i == v.CommitFileCursor
		if opts.Hit != nil && baseX > 0 {
			opts.Hit(RegionCommitFile, baseX, baseY+commitFileListHeaderLines+(i-startIdx), width, 1, i)
		}
		statusIcon := file.Status
		if statusIcon == "" {
			statusIcon = "M"
		}
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
			if gap := maxWidth - lipgloss.Width(plainLine); gap > 0 {
				plainLine += strings.Repeat(" ", gap)
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
			if gap := maxWidth - lipgloss.Width(plainLine); gap > 0 {
				plainLine += strings.Repeat(" ", gap)
			}
			sb.WriteString(statusStyle.Render(plainLine))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (v *View) renderCommitFileDiffPane(width, height, baseX, baseY int, opts RenderOpts) string {
	if v.CommitDetail == nil {
		return v.commitDetailPlaceholder(width, opts)
	}
	if len(v.CommitDetail.Files) == 0 {
		return dimText("No files in commit")
	}
	if v.CommitFileCursor < 0 || v.CommitFileCursor >= len(v.CommitDetail.Files) {
		return dimText("Select a file")
	}
	file := v.CommitDetail.Files[v.CommitFileCursor]
	var sb strings.Builder
	headerStr := fmt.Sprintf("%s [%s]", file.Path, v.viewModeLabel())
	if v.Focus == FocusCommitDiff {
		sb.WriteString(styles.Title.Render(headerStr))
	} else {
		sb.WriteString(styles.Muted.Render(headerStr))
	}
	sb.WriteString("\n\n")
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	if v.CommitFileDiffRaw == "" {
		sb.WriteString(v.commitFileDiffPlaceholder(width, opts))
		return sb.String()
	}
	sb.WriteString(v.renderFileDiff(file.Path, v.CommitFileDiffRaw, width, contentHeight, opts))
	if opts.Hit != nil && baseX > 0 {
		opts.Hit(RegionCommitDiff, baseX, baseY, width, height, nil)
	}
	return sb.String()
}

// commitFileDiffPlaceholder is what the right pane says when it has no patch
// text. There are three distinct reasons and they must not look alike: a load
// still in flight, a load that failed, and a load that succeeded with nothing
// to draw. Only the first is "Loading".
func (v *View) commitFileDiffPlaceholder(width int, opts RenderOpts) string {
	if v.CommitFileDiffErr != "" {
		err := v.CommitFileDiffErr
		if opts.Truncate != nil {
			err = opts.Truncate(err, width, "…")
		}
		return styles.StatusDeleted.Render("Could not load this file's diff") + "\n" + dimText(err)
	}
	if !v.CommitFileDiffLoaded {
		return dimText("Loading diff…")
	}
	if v.CommitDetail != nil && v.CommitDetail.IsMerge {
		return dimText("Merge commit: git shows no combined diff for this file.") + "\n" +
			dimText("Open a parent commit to see its changes.")
	}
	return dimText("No textual diff for this file in this commit.")
}

func (v *View) renderCommitPreview(commit CommitInfo, width, height, baseX, baseY int, opts RenderOpts) string {
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

	if v.CommitDetail != nil {
		files := v.CommitDetail.Files
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
			if opts.Hit != nil && baseX > 0 {
				opts.Hit(RegionPreviewFile, baseX, baseY+linesUsed+i, width, 1, i)
			}
			statusIcon := file.Status
			if statusIcon == "" {
				statusIcon = "M"
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
			if gap := maxWidth - lipgloss.Width(plainLine); gap > 0 {
				plainLine += strings.Repeat(" ", gap)
			}
			sb.WriteString(statusStyle.Render(plainLine))
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString(v.commitDetailPlaceholder(width, opts))
	}
	return sb.String()
}

// commitDetailPlaceholder is what a commit surface says when it has no file
// list: the reason it failed if it failed, and "Loading" only while a load is
// genuinely outstanding.
func (v *View) commitDetailPlaceholder(width int, opts RenderOpts) string {
	if v.CommitDetailErr == "" {
		return dimText("Loading commit files…")
	}
	err := v.CommitDetailErr
	if opts.Truncate != nil {
		err = opts.Truncate(err, width, "…")
	}
	return styles.StatusDeleted.Render("Could not load this commit") + "\n" + dimText(err)
}

func (v *View) renderAggregate(width, height int) string {
	if v.Snapshot == nil {
		return dimText("Loading aggregate diff…")
	}
	return v.renderRaw(v.aggregateText(), width, height, RenderOpts{})
}

func (v *View) aggregateText() string {
	if v.Snapshot == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Aggregate: %s..HEAD", v.Snapshot.MergeBase)
	sb.WriteString("\nCommitted branch changes\n")
	if strings.TrimSpace(v.Snapshot.AggregateCommitted) == "" {
		sb.WriteString("(none)\n")
	} else {
		sb.WriteString(v.Snapshot.AggregateCommitted)
		sb.WriteByte('\n')
	}
	sb.WriteString("\nUncommitted working tree changes vs HEAD\n")
	if strings.TrimSpace(v.Snapshot.AggregateUncommitted) == "" {
		sb.WriteString("(none)\n")
	} else {
		sb.WriteString(v.Snapshot.AggregateUncommitted)
	}
	if v.Snapshot.Truncated {
		fmt.Fprintf(&sb, "\n\nUntracked limits: %d files, %d bytes/file, %d bytes total; omitted %d files (%d bytes).",
			MaxUntrackedFiles, MaxUntrackedFileSize, MaxUntrackedTotalBytes,
			v.Snapshot.UntrackedOmitted, v.Snapshot.UntrackedBytesOmitted)
	}
	return sb.String()
}

func (v *View) renderRaw(content string, width, height int, opts RenderOpts) string {
	lines := splitLines(content)
	start := v.DiffScroll
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
	var rendered []string
	for _, line := range lines[start:end] {
		line = ui.ExpandTabs(line, 4)
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
		if opts.Truncate != nil && lipgloss.Width(styledLine) > width {
			styledLine = opts.Truncate(styledLine, width, "")
		}
		rendered = append(rendered, styledLine)
	}
	return strings.Join(rendered, "\n")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

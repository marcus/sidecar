package gitstatus

import (
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
)

func (p *Plugin) toggleSidebar() {
	if p.sidebarVisible {
		p.sidebarRestore = p.activePane
		p.sidebarVisible = false
		if p.activePane == PaneSidebar {
			p.activePane = PaneDiff
		}
		return
	}

	p.sidebarVisible = true
	if p.sidebarRestore == PaneSidebar {
		p.activePane = PaneSidebar
	} else {
		p.activePane = PaneDiff
	}
}

// Sidebar movement over one list of files followed by commits.
//
// These four are the whole of what a bound pane's movement is too, so they live
// apart from the key switch that owns stage, discard, and push. Two copies of
// "which row is the cursor on, and does reaching it need another page" is how
// one surface quietly stops paging.

func (p *Plugin) cursorDown() tea.Cmd {
	if p.cursor >= p.totalSelectableItems()-1 {
		return nil
	}
	p.cursor++
	p.ensureCursorVisible()
	if p.cursorOnCommit() {
		commitIdx := p.selectedCommitIndex()
		p.ensureCommitVisible(commitIdx)
		// Trigger load-more when within 3 commits of end (only for unfiltered view)
		var loadMoreCmd tea.Cmd
		commits := p.activeCommits()
		if !p.historyFilterActive && p.moreCommitsAvailable && commitIdx >= len(commits)-3 && !p.loadingMoreCommits {
			loadMoreCmd = p.loadMoreCommits()
		}
		return tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
	}
	return p.autoLoadDiff()
}

func (p *Plugin) cursorUp() tea.Cmd {
	if p.cursor <= 0 {
		return nil
	}
	p.cursor--
	p.ensureCursorVisible()
	if p.cursorOnCommit() {
		p.ensureCommitVisible(p.selectedCommitIndex())
		return p.autoLoadCommitPreview()
	}
	return p.autoLoadDiff()
}

func (p *Plugin) cursorToTop() tea.Cmd {
	p.cursor = 0
	p.scrollOff = 0
	p.commitScrollOff = 0 // Reset commit scroll when jumping to top
	if p.cursorOnCommit() {
		return p.autoLoadCommitPreview()
	}
	return p.autoLoadDiff()
}

func (p *Plugin) cursorToBottom() tea.Cmd {
	totalItems := p.totalSelectableItems()
	if totalItems <= 0 {
		return nil
	}
	p.cursor = totalItems - 1
	p.ensureCursorVisible()
	if p.cursorOnCommit() {
		commitIdx := p.selectedCommitIndex()
		p.ensureCommitVisible(commitIdx)
		// Trigger load-more when jumping to end
		var loadMoreCmd tea.Cmd
		if p.moreCommitsAvailable && commitIdx >= len(p.recentCommits)-3 && !p.loadingMoreCommits {
			loadMoreCmd = p.loadMoreCommits()
		}
		return tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
	}
	return p.autoLoadDiff()
}

// updateStatus handles key events in the status view.
func (p *Plugin) updateStatus(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	// Handle diff pane keys when focused on diff
	if p.activePane == PaneDiff {
		return p.updateStatusDiffPane(msg)
	}

	entries := p.tree.AllEntries()
	if p.writeInProgress() && isStatusMutationKey(msg.String()) {
		return p, p.writeBusyToast()
	}

	switch msg.String() {
	case "j", "down":
		return p, p.cursorDown()

	case "k", "up":
		return p, p.cursorUp()

	case "g":
		return p, p.cursorToTop()

	case "G":
		return p, p.cursorToBottom()

	case "l", "right":
		// Focus diff pane (when on a file) or commit preview pane (when on a commit)
		if p.sidebarVisible {
			if p.cursorOnCommit() && p.previewCommit != nil {
				p.activePane = PaneDiff
			} else if p.selectedDiffFile != "" {
				p.activePane = PaneDiff
			}
		}

	case "L":
		// Open pull menu
		if p.canPull() && !p.pullInProgress {
			p.pullMenuReturnMode = p.viewMode
			p.viewMode = ViewModePullMenu
			p.pullSelectedIdx = 0
		}

	case "tab", "shift+tab":
		// Switch focus to diff pane (if sidebar visible)
		if p.sidebarVisible && (p.selectedDiffFile != "" || p.previewCommit != nil) {
			p.activePane = PaneDiff
		}

	case "+":
		// Grow sidebar width
		if p.sidebarVisible {
			available := p.width - dividerWidth
			maxWidth := available - 40
			p.sidebarWidth += 3
			if p.sidebarWidth > maxWidth {
				p.sidebarWidth = maxWidth
			}
			p.diffPaneWidth = available - p.sidebarWidth
			_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
		}

	case "-":
		// Shrink sidebar width
		if p.sidebarVisible {
			p.sidebarWidth -= 3
			if p.sidebarWidth < 25 {
				p.sidebarWidth = 25
			}
			available := p.width - dividerWidth
			p.diffPaneWidth = available - p.sidebarWidth
			_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
		}

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()
		if !p.sidebarVisible {
			return p, appmsg.ShowFlash("Sidebar hidden (\\ to restore)")
		}

	case "s":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if !entry.Staged {
				if p.activeOperation != nil {
					return p, p.writeBusyToast()
				}
				selectionPath := entry.Path
				if entry.IsFolder && len(entry.Children) > 0 {
					selectionPath = entry.Children[0].Path
				}
				return p, p.beginWrite(operationStage, []string{"add", "--", entry.Path}, selectionIdentity{path: selectionPath, wantStaged: true})
			}
		}

	case "u":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if entry.Staged {
				if p.activeOperation != nil {
					return p, p.writeBusyToast()
				}
				return p, p.beginWrite(operationUnstage, []string{"restore", "--staged", "--", entry.Path}, selectionIdentity{path: entry.Path})
			}
		}

	case "d":
		// Open full-screen diff view for files
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffStaged = entry.Staged
			p.diffCommit = ""
			p.diffCommitSubject = ""
			p.diffCommitShortHash = ""
			p.diffScroll = 0
			p.diffLoaded = false
			if entry.IsFolder {
				return p, p.loadFullFolderDiff(entry)
			}
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}
		// For commits, focus the preview pane (same as l/right)
		if p.cursorOnCommit() && p.previewCommit != nil {
			p.activePane = PaneDiff
		}

	case "enter":
		// For folders: toggle expand/collapse
		// For files: open in editor
		// For commits: focus the preview pane
		if p.cursorOnCommit() {
			if p.previewCommit != nil {
				p.activePane = PaneDiff
			}
		} else if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if entry.IsFolder {
				// Toggle folder expansion
				entry.IsExpanded = !entry.IsExpanded
				// Reload diff for this folder
				return p, p.autoLoadDiff()
			}
			return p, p.openFileEntry(entry.Path)
		}

	case "r":
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case "S":
		// Stage all files
		if p.activeOperation != nil {
			return p, p.writeBusyToast()
		}
		selection := selectionIdentity{}
		if p.cursor < len(entries) {
			selection = selectionIdentity{path: entries[p.cursor].Path, wantStaged: true}
		}
		return p, p.beginWrite(operationStageAll, []string{"add", "-A"}, selection)

	case "U":
		// Unstage all files
		if p.activeOperation != nil {
			return p, p.writeBusyToast()
		}
		selection := selectionIdentity{}
		if p.cursor < len(entries) {
			selection = selectionIdentity{path: entries[p.cursor].Path}
		}
		return p, p.beginWrite(operationUnstageAll, []string{"reset", "HEAD"}, selection)

	case "h":
		// Jump cursor to commits section (show history)
		fileCount := len(entries)
		commits := p.activeCommits()
		if len(commits) > 0 && p.cursor < fileCount {
			p.cursor = fileCount
			p.ensureCommitVisible(0)
			return p, p.autoLoadCommitPreview()
		}

	case "O":
		// Open file in file browser (for files only, not commits)
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			return p, p.openInFileBrowser(entry.Path)
		}

	case "c":
		// Enter commit mode only if staged files exist
		if p.tree.HasStagedFiles() {
			p.viewMode = ViewModeCommit
			p.initCommitTextarea()
			return p, nil
		}

	case "A":
		// Amend last commit (no staged files required)
		if len(p.recentCommits) > 0 {
			p.commitAmend = true
			p.viewMode = ViewModeCommit
			p.initCommitTextarea()
			return p, p.loadAmendMessage()
		}

	case "P":
		// Open push menu (following lazygit convention)
		if p.canPush() && !p.pushInProgress {
			p.pushMenuReturnMode = p.viewMode
			p.viewMode = ViewModePushMenu
			p.pushMenuFocus = 0
			p.clearPushMenuModal()
		}

	case "y":
		// Yank commit as markdown (when on commit in sidebar)
		if p.cursorOnCommit() {
			return p, p.copyCommitToClipboard()
		}

	case "Y":
		// Yank commit ID (when on commit in sidebar)
		if p.cursorOnCommit() {
			return p, p.copyCommitIDToClipboard()
		}

	case "o":
		// Open commit in GitHub (when on commit in sidebar)
		if p.cursorOnCommit() {
			return p, p.openCommitInGitHub()
		}

	case "D":
		// Discard changes (confirm modal) - only for modified/staged files, not commits
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			// Don't allow discard on untracked folders (would delete)
			if entry.IsFolder && entry.Status == StatusUntracked {
				return p, nil
			}
			p.discardFile = entry
			p.discardReturnMode = p.viewMode
			p.viewMode = ViewModeConfirmDiscard
			p.buildDiscardModal()
		}

	case "z":
		// Stash current changes (if there are any)
		if p.tree.TotalCount() > 0 {
			return p, p.doStashPush()
		}

	case "Z":
		// Pop latest stash - show confirm modal first
		return p, p.confirmStashPop()

	case "ctrl+z":
		// Apply latest stash (non-destructive, stash entry preserved)
		return p, p.doStashApply()

	case "b":
		// Open branch picker
		p.branchReturnMode = p.viewMode
		p.branchCursor = 0
		p.viewMode = ViewModeBranchPicker
		p.clearBranchPickerModal()
		return p, p.loadBranches()

	case "f":
		// On commit: filter by author; on file: fetch
		if p.cursorOnCommit() {
			commits := p.activeCommits()
			commitIdx := p.selectedCommitIndex()
			if commitIdx >= 0 && commitIdx < len(commits) {
				commit := commits[commitIdx]
				p.historyFilterAuthor = commit.Author
				p.historyFilterActive = true
				return p, p.loadFilteredCommits()
			}
		} else {
			// Fetch from remote
			if !p.fetchInProgress {
				p.fetchInProgress = true
				return p, p.doFetch()
			}
		}

	case "F":
		// Clear all history filters
		if p.historyFilterActive {
			p.historyFilterAuthor = ""
			p.historyFilterPath = ""
			p.historyFilterActive = false
			p.filteredCommits = nil
			// Recompute graph for unfiltered commits
			if p.showCommitGraph && len(p.recentCommits) > 0 {
				p.commitGraphLines = ComputeGraphForCommits(p.recentCommits)
			}
		}

	case "p":
		// On commit: filter by path (open modal)
		if p.cursorOnCommit() {
			p.pathFilterMode = true
			p.pathFilterField.Reset()
			p.pathFilterField.Focus()
			return p, nil
		}

	case "/":
		// Open history search modal (only when on commits)
		if p.cursorOnCommit() {
			if p.historySearchState == nil {
				p.historySearchState = NewHistorySearchState()
			}
			p.historySearchState.Reset()
			p.historySearchMode = true
			return p, nil
		}

	case "n":
		// Next search match (after search committed)
		if p.historySearchState != nil && p.historySearchState.Committed && len(p.historySearchState.Matches) > 0 {
			p.historySearchState.Cursor++
			if p.historySearchState.Cursor >= len(p.historySearchState.Matches) {
				p.historySearchState.Cursor = 0 // Wrap around
			}
			return p, p.jumpToSearchMatch()
		}

	case "N":
		// Previous search match
		if p.historySearchState != nil && p.historySearchState.Committed && len(p.historySearchState.Matches) > 0 {
			p.historySearchState.Cursor--
			if p.historySearchState.Cursor < 0 {
				p.historySearchState.Cursor = len(p.historySearchState.Matches) - 1 // Wrap around
			}
			return p, p.jumpToSearchMatch()
		}

	case "esc":
		// ESC clears search state (if any active search)
		if p.historySearchState != nil && p.historySearchState.Committed {
			p.clearSearchState()
			return p, nil
		}

	case "v":
		// Toggle commit graph display (only when on commits)
		if p.cursorOnCommit() {
			p.showCommitGraph = !p.showCommitGraph
			_ = state.SetGitGraphEnabled(p.showCommitGraph)
			if p.showCommitGraph {
				commits := p.activeCommits()
				p.commitGraphLines = ComputeGraphForCommits(commits)
			}
		}
		return p, nil
	}

	return p, nil
}

// updateStatusDiffPane handles key events when the diff pane is focused.
func (p *Plugin) updateStatusDiffPane(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	// If showing commit preview, handle file list navigation
	if p.previewCommit != nil && p.cursorOnCommit() {
		return p.updateCommitPreviewPane(msg)
	}

	switch msg.String() {
	case "esc":
		// Restore sidebar if hidden, then return to it
		if !p.sidebarVisible {
			p.sidebarVisible = true
		}
		p.activePane = PaneSidebar

	case "h", "left":
		// Horizontal scroll left, or return to sidebar if already at leftmost
		if p.diffPaneHorizScroll > 0 {
			p.diffPaneHorizScroll -= 10
			if p.diffPaneHorizScroll < 0 {
				p.diffPaneHorizScroll = 0
			}
		} else {
			if !p.sidebarVisible {
				p.sidebarVisible = true
			}
			p.activePane = PaneSidebar
		}

	case "l", "right":
		// Horizontal scroll right
		p.diffPaneHorizScroll += 10
		p.clampDiffPaneHorizScroll()

	case "j", "down":
		p.diffPaneScroll++
		p.clampDiffPaneScroll()

	case "k", "up":
		if p.diffPaneScroll > 0 {
			p.diffPaneScroll--
		}

	case "g":
		p.diffPaneScroll = 0
		p.diffPaneHorizScroll = 0

	case "G":
		// Jump to end — set scroll high and let clamp fix it.
		if p.diffPaneViewMode == DiffViewFullFile && p.diffPaneFullFileDiff != nil {
			p.diffPaneScroll = p.diffPaneFullFileDiff.TotalLines()
		} else if p.diffPaneParsedDiff != nil {
			p.diffPaneScroll = countParsedDiffLines(p.diffPaneParsedDiff)
		}
		p.clampDiffPaneScroll()

	case "ctrl+d":
		p.diffPaneScroll += 10
		p.clampDiffPaneScroll()

	case "ctrl+u":
		p.diffPaneScroll -= 10
		if p.diffPaneScroll < 0 {
			p.diffPaneScroll = 0
		}

	case "|":
		// Snap horizontal scroll back to column 0.
		//
		// This was `0` — vim's column-zero key — until the header grew its
		// named global entries (8/9/0 select Sessions/Activity/Tasks), which
		// the host handles before any plugin sees the key. `0` here was only
		// ever working by falling through a gap in the host's ladder, and the
		// number row is deliberately host-owned: keymap.GlobalKeys lists all
		// ten, on the stated rule that a number meaning one thing in one tab
		// and something else in another is a key whose meaning depends on
		// where you happen to be. So the binding moves rather than being
		// claimed back.
		//
		// `|` is vim's goto-column key and sits next to `\` (toggle sidebar)
		// and near `w` (toggle wrap), the two other horizontal-space keys in
		// this pane. Unlike the old `0` it is in Commands() and the keymap
		// registry, so it shows up in the footer and in `?`.
		p.diffPaneHorizScroll = 0

	case "v":
		// Cycle view mode (unified → side-by-side → full-file) for inline diff pane
		switch p.diffPaneViewMode {
		case DiffViewUnified:
			p.diffPaneViewMode = DiffViewSideBySide
		case DiffViewSideBySide:
			p.diffPaneViewMode = DiffViewFullFile
			// Load full-file content if not already loaded
			if p.diffPaneFullFileDiff == nil && p.selectedDiffFile != "" {
				entries := p.tree.AllEntries()
				for _, entry := range entries {
					if entry.Path == p.selectedDiffFile && entry.Staged == p.selectedDiffStaged {
						return p, p.loadFullFileDiff(entry.Path, entry.Staged, entry.Status, "", true)
					}
				}
			}
		default:
			// Switching from full-file back to unified: map scroll position
			if p.diffPaneFullFileDiff != nil && p.diffPaneParsedDiff != nil && p.diffPaneScroll > 0 {
				p.diffPaneScroll = p.diffPaneFullFileDiff.FullFileLineToHunkLine(p.diffPaneScroll, p.diffPaneParsedDiff)
			}
			p.diffPaneViewMode = DiffViewUnified
			p.diffPaneFullFileDiff = nil // Free memory
		}

	case "n":
		// Jump to next change in full-file view
		if p.diffPaneViewMode == DiffViewFullFile && p.diffPaneFullFileDiff != nil {
			next := p.diffPaneFullFileDiff.NextChange(p.diffPaneScroll)
			if next >= 0 {
				p.diffPaneScroll = next
			}
		}

	case "N":
		// Jump to previous change in full-file view
		if p.diffPaneViewMode == DiffViewFullFile && p.diffPaneFullFileDiff != nil {
			prev := p.diffPaneFullFileDiff.PrevChange(p.diffPaneScroll)
			if prev >= 0 {
				p.diffPaneScroll = prev
			}
		}

	case "w":
		// Toggle line wrapping for inline diff pane
		p.diffWrapEnabled = !p.diffWrapEnabled
		_ = state.SetLineWrapEnabled(p.diffWrapEnabled)
		p.diffPaneHorizScroll = 0
		p.diffPaneScroll = 0

	case "tab", "shift+tab":
		// Switch focus to sidebar (if visible)
		if p.sidebarVisible {
			p.activePane = PaneSidebar
		}

	case "+":
		// Grow sidebar width
		if p.sidebarVisible {
			available := p.width - dividerWidth
			maxWidth := available - 40
			p.sidebarWidth += 3
			if p.sidebarWidth > maxWidth {
				p.sidebarWidth = maxWidth
			}
			p.diffPaneWidth = available - p.sidebarWidth
			_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
		}

	case "-":
		// Shrink sidebar width
		if p.sidebarVisible {
			p.sidebarWidth -= 3
			if p.sidebarWidth < 25 {
				p.sidebarWidth = 25
			}
			available := p.width - dividerWidth
			p.diffPaneWidth = available - p.sidebarWidth
			_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
		}

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()
		if !p.sidebarVisible {
			return p, appmsg.ShowFlash("Sidebar hidden (\\ to restore)")
		}

	case "d":
		// Open full-screen diff view for current file
		entries := p.tree.AllEntries()
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffStaged = entry.Staged
			p.diffCommit = ""
			p.diffCommitSubject = ""
			p.diffCommitShortHash = ""
			p.diffScroll = 0
			p.diffLoaded = false
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}
	}

	return p, nil
}

// updateCommitPreviewPane handles key events when viewing commit preview in the diff pane.
func (p *Plugin) updateCommitPreviewPane(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	c := p.previewCommit
	if c == nil {
		return p, nil
	}

	// When commit body is expanded, handle scrolling within it
	if p.commitBodyExpanded {
		return p.updateCommitBodyExpanded(msg)
	}

	switch msg.String() {
	case "esc", "h", "left":
		// Return to sidebar
		p.activePane = PaneSidebar

	case "j", "down":
		// Navigate file list
		if p.previewCommitCursor < len(c.Files)-1 {
			p.previewCommitCursor++
			p.ensurePreviewCursorVisible()
		}

	case "k", "up":
		if p.previewCommitCursor > 0 {
			p.previewCommitCursor--
			p.ensurePreviewCursorVisible()
		} else if c.Body != "" {
			// At top of file list — expand commit body
			p.commitBodyExpanded = true
			p.commitBodyScroll = 0
		}

	case "g":
		p.previewCommitCursor = 0
		p.previewCommitScroll = 0

	case "G":
		if len(c.Files) > 0 {
			p.previewCommitCursor = len(c.Files) - 1
			p.ensurePreviewCursorVisible()
		}

	case "enter", "d", "l", "right":
		// Open full-screen diff for selected file in commit
		if p.previewCommitCursor < len(c.Files) {
			file := c.Files[p.previewCommitCursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = file.Path
			p.diffStaged = false
			p.diffCommit = c.Hash
			p.diffCommitSubject = c.Subject
			p.diffCommitShortHash = c.ShortHash
			p.diffScroll = 0
			p.diffLoaded = false
			parentHash := ""
			if c.IsMerge && len(c.ParentHashes) > 0 {
				parentHash = c.ParentHashes[0]
			}
			return p, p.loadCommitFileDiff(c.Hash, file.Path, parentHash)
		}

	case "tab", "shift+tab":
		// Switch focus to sidebar (if visible)
		if p.sidebarVisible {
			p.activePane = PaneSidebar
		}

	case "+":
		// Grow sidebar width
		if p.sidebarVisible {
			available := p.width - dividerWidth
			maxWidth := available - 40
			p.sidebarWidth += 3
			if p.sidebarWidth > maxWidth {
				p.sidebarWidth = maxWidth
			}
			p.diffPaneWidth = available - p.sidebarWidth
			_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
		}

	case "-":
		// Shrink sidebar width
		if p.sidebarVisible {
			p.sidebarWidth -= 3
			if p.sidebarWidth < 25 {
				p.sidebarWidth = 25
			}
			available := p.width - dividerWidth
			p.diffPaneWidth = available - p.sidebarWidth
			_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
		}

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()

	case "y":
		// Yank commit as markdown
		return p, p.copyCommitToClipboard()

	case "Y":
		// Yank commit ID
		return p, p.copyCommitIDToClipboard()

	case "o":
		// Open commit in GitHub
		return p, p.openCommitInGitHub()

	case "b":
		// Open selected file in file browser
		if p.previewCommitCursor < len(c.Files) {
			file := c.Files[p.previewCommitCursor]
			return p, p.openInFileBrowser(file.Path)
		}
	}

	return p, nil
}

// updateCommitBodyExpanded handles keys when the full commit message is expanded.
func (p *Plugin) updateCommitBodyExpanded(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	c := p.previewCommit
	bodyLines := strings.Split(strings.TrimSpace(c.Body), "\n")
	totalLines := len(bodyLines)
	bodyHeight := p.commitBodyHeight()

	maxScroll := totalLines - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch msg.String() {
	case "esc", "j", "down":
		// Collapse and return to file list
		p.commitBodyExpanded = false
		p.commitBodyScroll = 0

	case "k", "up":
		if p.commitBodyScroll > 0 {
			p.commitBodyScroll--
		}

	case "g":
		p.commitBodyScroll = 0

	case "G":
		p.commitBodyScroll = maxScroll

	case "ctrl+u":
		p.commitBodyScroll -= bodyHeight / 2
		if p.commitBodyScroll < 0 {
			p.commitBodyScroll = 0
		}

	case "ctrl+d":
		p.commitBodyScroll += bodyHeight / 2
		if p.commitBodyScroll > maxScroll {
			p.commitBodyScroll = maxScroll
		}
	}

	return p, nil
}

// closeDiffView clears diff state and returns to the previous view mode.
func (p *Plugin) closeDiffView() {
	p.diffContent = ""
	p.diffRaw = ""
	p.parsedDiff = nil
	p.fullFileDiff = nil
	p.diffLoaded = false
	p.diffTruncated = false
	p.diffHorizOff = 0
	p.diffCommit = ""
	p.diffCommitSubject = ""
	p.diffCommitShortHash = ""
	p.diffFile = ""
	p.diffStaged = false
	p.diffBackWidth = 0
	p.viewMode = p.diffReturnMode
	if p.diffReturnMode == ViewModeStatus && p.previewCommit != nil {
		p.activePane = PaneDiff
	}
}

// updateDiff handles key events in the diff view.
func (p *Plugin) updateDiff(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		p.closeDiffView()

	case "j", "down":
		p.diffScroll++
		p.clampDiffScroll()

	case "k", "up":
		if p.diffScroll > 0 {
			p.diffScroll--
		}

	case "g":
		p.diffScroll = 0
		p.diffHorizOff = 0

	case "G":
		// Jump to end — set scroll high and let clamp fix it.
		if p.diffViewMode == DiffViewFullFile && p.fullFileDiff != nil {
			p.diffScroll = p.fullFileDiff.TotalLines()
		} else {
			p.diffScroll = countLines(p.diffContent)
		}
		p.clampDiffScroll()

	case "v":
		// Cycle view mode (unified → side-by-side → full-file)
		switch p.diffViewMode {
		case DiffViewUnified:
			p.diffViewMode = DiffViewSideBySide
			_ = state.SetGitDiffMode("side-by-side")
		case DiffViewSideBySide:
			p.diffViewMode = DiffViewFullFile
			_ = state.SetGitDiffMode("full-file")
			// Load full-file content if not already loaded
			if p.fullFileDiff == nil && p.diffFile != "" {
				if entry := p.currentWorkingTreeDiffEntry(); entry != nil {
					return p, p.loadFullFileDiff(entry.Path, entry.Staged, entry.Status, p.diffCommit, false)
				}
				// For commit diffs where file isn't in tree
				if p.diffCommit != "" {
					return p, p.loadFullFileDiff(p.diffFile, false, "", p.diffCommit, false)
				}
			}
		default:
			// Switching from full-file back to unified: map scroll position
			if p.fullFileDiff != nil && p.parsedDiff != nil && p.diffScroll > 0 {
				p.diffScroll = p.fullFileDiff.FullFileLineToHunkLine(p.diffScroll, p.parsedDiff)
			}
			p.diffViewMode = DiffViewUnified
			_ = state.SetGitDiffMode("unified")
			p.fullFileDiff = nil // Free memory
		}
		p.diffHorizOff = 0

	case "n":
		// Jump to next change in full-file view
		if p.diffViewMode == DiffViewFullFile && p.fullFileDiff != nil {
			next := p.fullFileDiff.NextChange(p.diffScroll)
			if next >= 0 {
				p.diffScroll = next
			}
		}

	case "N":
		// Jump to previous change in full-file view
		if p.diffViewMode == DiffViewFullFile && p.fullFileDiff != nil {
			prev := p.fullFileDiff.PrevChange(p.diffScroll)
			if prev >= 0 {
				p.diffScroll = prev
			}
		}

	// Stepping through files is , / . everywhere a diff is on screen. { and }
	// mean "cycle tabs" throughout Sidecar; this view has no tabs, so they are
	// deliberately left unbound rather than made to mean something else here.
	case ",":
		return p, p.cycleDiffFile(-1)

	case ".":
		return p, p.cycleDiffFile(1)

	case "w":
		// Toggle line wrapping
		p.diffWrapEnabled = !p.diffWrapEnabled
		_ = state.SetLineWrapEnabled(p.diffWrapEnabled)
		p.diffHorizOff = 0
		p.diffScroll = 0

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()

	case "h", "left", "<", "H":
		// Horizontal scroll left, or exit diff view if already at leftmost
		if p.diffHorizOff > 0 {
			p.diffHorizOff -= 10
			if p.diffHorizOff < 0 {
				p.diffHorizOff = 0
			}
		} else {
			p.closeDiffView()
		}

	case "l", "right", ">", "L":
		// Horizontal scroll right
		p.diffHorizOff += 10
		p.clampDiffHorizScroll()

	case "ctrl+d":
		// Page down (~10 lines)
		p.diffScroll += 10
		p.clampDiffScroll()

	case "ctrl+u":
		// Page up (~10 lines)
		p.diffScroll -= 10
		if p.diffScroll < 0 {
			p.diffScroll = 0
		}

	case "O":
		// Open file in file browser
		if p.diffFile != "" {
			return p, p.openInFileBrowser(p.diffFile)
		}
	}

	if p.diffViewMode == DiffViewFullFile && p.fullFileDiff != nil {
		slog.Debug("minimap scroll state",
			"key", msg.String(),
			"diffScroll", p.diffScroll,
			"totalLines", len(p.fullFileDiff.Lines),
			"height", p.height,
		)
	}

	return p, nil
}

// cycleDiffFile moves to the adjacent file represented by the current diff.
// It wraps so repeated presses can traverse the whole working tree or commit.
func (p *Plugin) cycleDiffFile(delta int) tea.Cmd {
	if p.diffCommit != "" && p.previewCommit != nil && p.previewCommit.Hash == p.diffCommit {
		files := p.previewCommit.Files
		if len(files) < 2 {
			return nil
		}
		current := 0
		for i, file := range files {
			if file.Path == p.diffFile {
				current = i
				break
			}
		}
		next := (current + delta + len(files)) % len(files)
		file := files[next]
		p.diffFile = file.Path
		p.diffScroll = 0
		p.diffHorizOff = 0
		p.diffLoaded = false
		p.fullFileDiff = nil
		parentHash := ""
		if p.previewCommit.IsMerge && len(p.previewCommit.ParentHashes) > 0 {
			parentHash = p.previewCommit.ParentHashes[0]
		}
		return p.loadCommitFileDiff(p.diffCommit, file.Path, parentHash)
	}

	entries := p.tree.AllEntries()
	files := make([]*FileEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsFolder {
			files = append(files, entry)
		}
	}
	if len(files) < 2 {
		return nil
	}
	current := 0
	for i, entry := range files {
		if entry.Path == p.diffFile && entry.Staged == p.diffStaged {
			current = i
			break
		}
	}
	next := (current + delta + len(files)) % len(files)
	entry := files[next]
	p.diffFile = entry.Path
	p.diffStaged = entry.Staged
	p.diffScroll = 0
	p.diffHorizOff = 0
	p.diffLoaded = false
	p.fullFileDiff = nil
	return p.loadDiff(entry.Path, entry.Staged, entry.Status)
}

func (p *Plugin) currentWorkingTreeDiffEntry() *FileEntry {
	if p.diffCommit != "" || p.tree == nil {
		return nil
	}
	for _, entry := range p.tree.AllEntries() {
		if entry.Path == p.diffFile && entry.Staged == p.diffStaged {
			return entry
		}
	}
	return nil
}

// updateCommit handles key events in the commit view.
func (p *Plugin) updateCommit(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	p.ensureCommitModal()
	if p.commitModal == nil {
		return p, nil
	}

	switch msg.String() {
	case "ctrl+s", "ctrl+enter":
		return p, p.tryCommit()

	case "ctrl+a":
		// Toggle amend mode (only if there are commits to amend and staged files)
		if len(p.recentCommits) > 0 && p.tree.HasStagedFiles() {
			p.commitAmend = !p.commitAmend
			// Invalidate modal cache to rebuild with new state
			p.commitModal = nil
			p.commitModalWidthCache = 0
			// If enabling amend and message is empty, prefill with last commit message
			if p.commitAmend && strings.TrimSpace(p.commitMessage.Value()) == "" {
				return p, p.loadAmendMessage()
			}
		}
		return p, nil
	}

	wasAmend := p.commitAmend
	focusID := p.commitModal.FocusedID()
	action, cmd := p.commitModal.HandleKey(msg)

	if p.commitAmend != wasAmend {
		p.commitModal = nil
		p.commitModalWidthCache = 0
	}

	if action == commitActionID && focusID == commitMessageID {
		return p, cmd
	}

	switch action {
	case commitActionID:
		return p, p.tryCommit()
	case "cancel":
		p.viewMode = ViewModeStatus
		p.commitAmend = false
		p.commitError = ""
		p.commitModal = nil
		p.commitModalWidthCache = 0
		return p, nil
	}

	return p, cmd
}

// tryCommit attempts to execute the commit (or amend) if message is valid.
func (p *Plugin) tryCommit() tea.Cmd {
	if p.writeInProgress() {
		return p.writeBusyToast()
	}
	message := strings.TrimSpace(p.commitMessage.Value())
	if message == "" {
		p.commitError = "Commit message cannot be empty"
		return nil
	}
	p.commitInProgress = true
	if p.commitAmend {
		return p.doAmend(message)
	}
	return p.doCommit(message)
}

// updatePushMenu handles key events in the push menu.
func (p *Plugin) updatePushMenu(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	p.ensurePushMenuModal()
	if p.pushMenuModal == nil {
		return p, nil
	}

	// Direct-execution shortcuts
	switch msg.String() {
	case "p":
		return p.executePushMenuAction(0)
	case "f":
		return p.executePushMenuAction(1)
	case "u":
		return p.executePushMenuAction(2)
	}

	switch msg.String() {
	case "esc", "q":
		p.viewMode = p.pushMenuReturnMode
		p.clearPushMenuModal()
		p.pushMenuFocus = 0
		return p, nil
	}

	action, cmd := p.pushMenuModal.HandleKey(msg)
	switch action {
	case "cancel":
		p.viewMode = p.pushMenuReturnMode
		p.clearPushMenuModal()
		p.pushMenuFocus = 0
		return p, nil
	case pushMenuOptionPush:
		return p.executePushMenuAction(0)
	case pushMenuOptionForce:
		return p.executePushMenuAction(1)
	case pushMenuOptionUpstream:
		return p.executePushMenuAction(2)
	case pushMenuActionID:
		return p.executePushMenuAction(p.pushMenuFocus)
	}

	return p, cmd
}

// updatePullMenu handles key events in the pull menu.
func (p *Plugin) updatePullMenu(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	p.ensurePullModal()
	if p.pullModal == nil {
		return p, nil
	}

	// Direct-execution keyboard shortcuts - execute regardless of current selection.
	// Power users can press p/r/f/a to immediately execute the corresponding action.
	switch msg.String() {
	case "p":
		return p.executePullMenuAction(pullMenuOptionMerge)
	case "r":
		return p.executePullMenuAction(pullMenuOptionRebase)
	case "f":
		return p.executePullMenuAction(pullMenuOptionFFOnly)
	case "a":
		return p.executePullMenuAction(pullMenuOptionAutostash)
	}

	action, cmd := p.pullModal.HandleKey(msg)

	switch action {
	case "cancel":
		p.viewMode = p.pullMenuReturnMode
		p.clearPullModal()
		return p, nil
	case pullMenuOptionMerge, pullMenuOptionRebase, pullMenuOptionFFOnly, pullMenuOptionAutostash:
		return p.executePullMenuAction(action)
	case pullMenuActionID:
		// Primary action (Enter) - execute the currently selected option
		return p.executePullMenuActionByIndex(p.pullSelectedIdx)
	}

	return p, cmd
}

// executePullMenuAction executes the pull menu action by ID.
func (p *Plugin) executePullMenuAction(actionID string) (plugin.Plugin, tea.Cmd) {
	if p.writeInProgress() {
		return p, p.writeBusyToast()
	}
	if actionID != pullMenuOptionMerge && actionID != pullMenuOptionRebase && actionID != pullMenuOptionFFOnly && actionID != pullMenuOptionAutostash {
		return p, nil
	}
	p.viewMode = p.pullMenuReturnMode
	p.pullInProgress = true
	p.clearPullModal()

	switch actionID {
	case pullMenuOptionMerge:
		return p, p.doPull()
	case pullMenuOptionRebase:
		return p, p.doPullRebase()
	case pullMenuOptionFFOnly:
		return p, p.doPullFFOnly()
	case pullMenuOptionAutostash:
		return p, p.doPullAutostash()
	}
	return p, nil
}

// executePullMenuActionByIndex executes the pull menu action by selected index.
func (p *Plugin) executePullMenuActionByIndex(idx int) (plugin.Plugin, tea.Cmd) {
	actions := []string{pullMenuOptionMerge, pullMenuOptionRebase, pullMenuOptionFFOnly, pullMenuOptionAutostash}
	if idx >= 0 && idx < len(actions) {
		return p.executePullMenuAction(actions[idx])
	}
	return p, nil
}

// clearPullModal clears pull menu modal state.
func (p *Plugin) clearPullModal() {
	p.pullModal = nil
	p.pullModalWidth = 0
	p.pullSelectedIdx = 0
}

// updatePullConflict handles key events in the pull conflict modal.
func (p *Plugin) updatePullConflict(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	p.ensurePullConflictModal()
	if p.pullConflictModal == nil {
		return p, nil
	}

	switch msg.String() {
	case "a":
		// Abort merge/rebase
		return p.abortPullConflict()
	case "esc", "q":
		// Dismiss modal (conflicts remain, user resolves manually)
		return p.dismissPullConflict()
	}

	action, cmd := p.pullConflictModal.HandleKey(msg)
	switch action {
	case pullConflictAbortID:
		return p.abortPullConflict()
	case "cancel", pullConflictDismissID:
		return p.dismissPullConflict()
	}
	return p, cmd
}

func (p *Plugin) abortPullConflict() (plugin.Plugin, tea.Cmd) {
	if p.writeInProgress() {
		return p, p.writeBusyToast()
	}
	p.pullInProgress = true
	p.viewMode = ViewModeStatus
	p.clearPullConflictModal()
	return p, p.doAbortPull()
}

func (p *Plugin) dismissPullConflict() (plugin.Plugin, tea.Cmd) {
	p.viewMode = ViewModeStatus
	p.pullConflictFiles = nil
	p.clearPullConflictModal()
	return p, p.refresh()
}

// executePushMenuAction executes the push menu action at the given index.
func (p *Plugin) executePushMenuAction(idx int) (plugin.Plugin, tea.Cmd) {
	if p.writeInProgress() {
		return p, p.writeBusyToast()
	}
	if idx < 0 || idx > 2 {
		return p, nil
	}
	p.viewMode = p.pushMenuReturnMode
	p.pushInProgress = true
	p.pushMenuFocus = 0
	p.clearPushMenuModal()

	// Preserve selected commit hash before push to restore cursor after refresh
	p.pushPreservedCommitHash = ""
	if p.cursorOnCommit() {
		commits := p.activeCommits()
		commitIdx := p.selectedCommitIndex()
		if commitIdx >= 0 && commitIdx < len(commits) {
			p.pushPreservedCommitHash = commits[commitIdx].Hash
		}
	}

	switch idx {
	case 0:
		return p, p.doPush(false)
	case 1:
		return p, p.doPushForce()
	case 2:
		return p, p.doPushSetUpstream()
	}
	return p, nil
}

// updateConfirmDiscard handles key events in the confirm discard modal.
func (p *Plugin) updateConfirmDiscard(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	if p.discardModal == nil {
		return p, nil
	}

	// Handle quick confirm shortcuts
	switch msg.String() {
	case "y", "Y":
		return p.confirmDiscard()
	}

	// Route to modal
	action, cmd := p.discardModal.HandleKey(msg)

	switch action {
	case "discard":
		return p.confirmDiscard()
	case "cancel":
		return p.cancelDiscard()
	}

	return p, cmd
}

// confirmDiscard executes the discard and closes the modal.
func (p *Plugin) confirmDiscard() (plugin.Plugin, tea.Cmd) {
	if p.writeInProgress() {
		return p, p.writeBusyToast()
	}
	var cmd tea.Cmd
	if p.discardFile != nil {
		cmd = p.doDiscard(p.discardFile)
	}
	p.viewMode = p.discardReturnMode
	p.discardFile = nil
	p.discardModal = nil
	return p, cmd
}

// cancelDiscard closes the modal without discarding.
func (p *Plugin) cancelDiscard() (plugin.Plugin, tea.Cmd) {
	p.viewMode = p.discardReturnMode
	p.discardFile = nil
	p.discardModal = nil
	return p, nil
}

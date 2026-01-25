package gitstatus

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// updateStatus handles key events in the status view.
func (p *Plugin) updateStatus(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Handle diff pane keys when focused on diff
	if p.activePane == PaneDiff {
		return p.updateStatusDiffPane(msg)
	}

	entries := p.tree.AllEntries()
	totalItems := p.totalSelectableItems()

	switch msg.String() {
	case "j", "down":
		if p.cursor < totalItems-1 {
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
				return p, tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
			}
			return p, p.autoLoadDiff()
		}
		return p, nil

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			if p.cursorOnCommit() {
				p.ensureCommitVisible(p.selectedCommitIndex())
				return p, p.autoLoadCommitPreview()
			}
			return p, p.autoLoadDiff()
		}
		return p, nil

	case "g":
		p.cursor = 0
		p.scrollOff = 0
		p.commitScrollOff = 0 // Reset commit scroll when jumping to top
		if p.cursorOnCommit() {
			return p, p.autoLoadCommitPreview()
		}
		return p, p.autoLoadDiff()

	case "G":
		if totalItems > 0 {
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
				return p, tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
			}
			return p, p.autoLoadDiff()
		}
		return p, nil

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
			p.pullMenuFocus = 0
		}

	case "tab", "shift+tab":
		// Switch focus to diff pane (if sidebar visible)
		if p.sidebarVisible && (p.selectedDiffFile != "" || p.previewCommit != nil) {
			p.activePane = PaneDiff
		}

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()

	case "s":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if !entry.Staged {
				stagedCount := len(p.tree.Staged)
				totalEntries := len(entries)

				// Handle folder entries - stage all children
				if entry.IsFolder {
					for _, child := range entry.Children {
						_ = p.tree.StageFile(child.Path)
					}
				} else {
					if err := p.tree.StageFile(entry.Path); err != nil {
						return p, nil
					}
				}
				// After staging, move cursor to first unstaged file position
				newFirstUnstaged := stagedCount + 1
				if newFirstUnstaged < totalEntries {
					p.cursor = newFirstUnstaged
				} else {
					p.cursor = totalEntries - 1
				}
				return p, tea.Batch(p.refresh(), p.loadRecentCommits())
			}
		}

	case "u":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if entry.Staged {
				if err := p.tree.UnstageFile(entry.Path); err == nil {
					return p, tea.Batch(p.refresh(), p.loadRecentCommits())
				}
			}
		}

	case "d":
		// Open full-screen diff view for files
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffCommit = ""
			p.diffScroll = 0
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
			return p, p.openFile(entry.Path)
		}

	case "r":
		p.pushError = "" // Clear any stale push error
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case "S":
		// Stage all files
		if err := p.tree.StageAll(); err == nil {
			return p, tea.Batch(p.refresh(), p.loadRecentCommits())
		}

	case "U":
		// Unstage all files
		if err := p.tree.UnstageAll(); err == nil {
			return p, tea.Batch(p.refresh(), p.loadRecentCommits())
		}

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
			msg := getLastCommitMessage(p.repoRoot)
			p.commitMessage.SetValue(msg)
			return p, nil
		}

	case "P":
		// Open push menu (following lazygit convention)
		if p.canPush() && !p.pushInProgress {
			p.pushMenuReturnMode = p.viewMode
			p.viewMode = ViewModePushMenu
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

	case "b":
		// Open branch picker
		p.branchReturnMode = p.viewMode
		p.branchCursor = 0
		p.viewMode = ViewModeBranchPicker
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
				p.fetchError = ""
				p.fetchSuccess = false
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
			p.pathFilterInput = ""
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
func (p *Plugin) updateStatusDiffPane(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// If showing commit preview, handle file list navigation
	if p.previewCommit != nil && p.cursorOnCommit() {
		return p.updateCommitPreviewPane(msg)
	}

	switch msg.String() {
	case "esc":
		// ESC always returns to sidebar
		p.activePane = PaneSidebar

	case "h", "left":
		// Horizontal scroll left (use ESC or Tab to switch panes)
		if p.diffPaneHorizScroll > 0 {
			p.diffPaneHorizScroll -= 10
			if p.diffPaneHorizScroll < 0 {
				p.diffPaneHorizScroll = 0
			}
		}

	case "l", "right":
		// Horizontal scroll right
		p.diffPaneHorizScroll += 10
		p.clampDiffPaneHorizScroll()

	case "j", "down":
		p.diffPaneScroll++

	case "k", "up":
		if p.diffPaneScroll > 0 {
			p.diffPaneScroll--
		}

	case "g":
		p.diffPaneScroll = 0
		p.diffPaneHorizScroll = 0

	case "G":
		if p.diffPaneParsedDiff != nil {
			lines := countParsedDiffLines(p.diffPaneParsedDiff)
			maxScroll := lines - (p.height - 6)
			if maxScroll > 0 {
				p.diffPaneScroll = maxScroll
			}
		}

	case "ctrl+d":
		p.diffPaneScroll += 10
		// Clamp to max
		if p.diffPaneParsedDiff != nil {
			lines := countParsedDiffLines(p.diffPaneParsedDiff)
			maxScroll := lines - (p.height - 6)
			if maxScroll < 0 {
				maxScroll = 0
			}
			if p.diffPaneScroll > maxScroll {
				p.diffPaneScroll = maxScroll
			}
		}

	case "ctrl+u":
		p.diffPaneScroll -= 10
		if p.diffPaneScroll < 0 {
			p.diffPaneScroll = 0
		}

	case "0":
		// Reset horizontal scroll
		p.diffPaneHorizScroll = 0

	case "v":
		// Toggle view mode (unified/side-by-side) for inline diff pane
		if p.diffPaneViewMode == DiffViewUnified {
			p.diffPaneViewMode = DiffViewSideBySide
		} else {
			p.diffPaneViewMode = DiffViewUnified
		}

	case "tab", "shift+tab":
		// Switch focus to sidebar (if visible)
		if p.sidebarVisible {
			p.activePane = PaneSidebar
		}

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()

	case "d":
		// Open full-screen diff view for current file
		entries := p.tree.AllEntries()
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffCommit = ""
			p.diffScroll = 0
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}
	}

	return p, nil
}

// updateCommitPreviewPane handles key events when viewing commit preview in the diff pane.
func (p *Plugin) updateCommitPreviewPane(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	c := p.previewCommit
	if c == nil {
		return p, nil
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
		}

	case "g":
		p.previewCommitCursor = 0
		p.previewCommitScroll = 0

	case "G":
		if len(c.Files) > 0 {
			p.previewCommitCursor = len(c.Files) - 1
			p.ensurePreviewCursorVisible()
		}

	case "enter", "d":
		// Open full-screen diff for selected file in commit
		if p.previewCommitCursor < len(c.Files) {
			file := c.Files[p.previewCommitCursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = file.Path
			p.diffCommit = c.Hash
			p.diffScroll = 0
			return p, p.loadCommitFileDiff(c.Hash, file.Path)
		}

	case "tab", "shift+tab":
		// Switch focus to sidebar (if visible)
		if p.sidebarVisible {
			p.activePane = PaneSidebar
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

// updateDiff handles key events in the diff view.
func (p *Plugin) updateDiff(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Return to previous view
		p.diffContent = ""
		p.diffRaw = ""
		p.parsedDiff = nil
		p.diffHorizOff = 0
		p.diffCommit = ""
		p.diffFile = ""
		p.viewMode = p.diffReturnMode
		// If returning to status view with commit preview, focus the preview pane
		if p.diffReturnMode == ViewModeStatus && p.previewCommit != nil {
			p.activePane = PaneDiff
		}

	case "j", "down":
		p.diffScroll++

	case "k", "up":
		if p.diffScroll > 0 {
			p.diffScroll--
		}

	case "g":
		p.diffScroll = 0
		p.diffHorizOff = 0

	case "G":
		lines := countLines(p.diffContent)
		maxScroll := lines - (p.height - 2)
		if maxScroll > 0 {
			p.diffScroll = maxScroll
		}

	case "v":
		// Toggle between unified and side-by-side view
		if p.diffViewMode == DiffViewUnified {
			p.diffViewMode = DiffViewSideBySide
			_ = state.SetGitDiffMode("side-by-side")
		} else {
			p.diffViewMode = DiffViewUnified
			_ = state.SetGitDiffMode("unified")
		}
		p.diffHorizOff = 0

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()

	case "h", "left", "<", "H":
		// Horizontal scroll left
		if p.diffHorizOff > 0 {
			p.diffHorizOff -= 10
			if p.diffHorizOff < 0 {
				p.diffHorizOff = 0
			}
		}

	case "l", "right", ">", "L":
		// Horizontal scroll right
		p.diffHorizOff += 10
		p.clampDiffHorizScroll()

	case "ctrl+d":
		// Page down (~10 lines)
		p.diffScroll += 10
		// Clamp to max
		lines := countLines(p.diffContent)
		maxScroll := lines - (p.height - 2)
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.diffScroll > maxScroll {
			p.diffScroll = maxScroll
		}

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

	return p, nil
}

// updateCommit handles key events in the commit view.
func (p *Plugin) updateCommit(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel commit, return to status
		p.viewMode = ViewModeStatus
		p.commitAmend = false
		p.commitError = ""
		return p, nil

	case "ctrl+s":
		// Execute commit if message not empty
		return p, p.tryCommit()

	case "tab", "shift+tab":
		// Toggle focus between textarea and button
		p.commitButtonFocus = !p.commitButtonFocus
		if p.commitButtonFocus {
			p.commitMessage.Blur()
		} else {
			p.commitMessage.Focus()
		}
		return p, nil

	case "enter":
		// If button is focused, execute commit
		if p.commitButtonFocus {
			return p, p.tryCommit()
		}
		// Otherwise let textarea handle it (newline)
	}

	// Pass other keys to textarea (only if textarea is focused)
	if !p.commitButtonFocus {
		var cmd tea.Cmd
		p.commitMessage, cmd = p.commitMessage.Update(msg)
		return p, cmd
	}

	return p, nil
}

// tryCommit attempts to execute the commit (or amend) if message is valid.
func (p *Plugin) tryCommit() tea.Cmd {
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
func (p *Plugin) updatePushMenu(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	const itemCount = 3 // push, force, upstream

	switch msg.String() {
	case "tab", "j", "down":
		p.pushMenuFocus = (p.pushMenuFocus + 1) % itemCount
		return p, nil
	case "shift+tab", "k", "up":
		p.pushMenuFocus = (p.pushMenuFocus - 1 + itemCount) % itemCount
		return p, nil
	case "enter":
		return p.executePushMenuAction(p.pushMenuFocus)
	case "esc", "q":
		p.viewMode = p.pushMenuReturnMode
		p.pushMenuFocus = 0
		return p, nil
	case "p":
		// Regular push (shortcut)
		return p.executePushMenuAction(0)
	case "f":
		// Force push (shortcut)
		return p.executePushMenuAction(1)
	case "u":
		// Push and set upstream (shortcut)
		return p.executePushMenuAction(2)
	}
	return p, nil
}

// updatePullMenu handles key events in the pull menu.
func (p *Plugin) updatePullMenu(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	const itemCount = 4 // merge, rebase, ff-only, autostash

	switch msg.String() {
	case "tab", "j", "down":
		p.pullMenuFocus = (p.pullMenuFocus + 1) % itemCount
		return p, nil
	case "shift+tab", "k", "up":
		p.pullMenuFocus = (p.pullMenuFocus - 1 + itemCount) % itemCount
		return p, nil
	case "enter":
		return p.executePullMenuAction(p.pullMenuFocus)
	case "esc", "q":
		p.viewMode = p.pullMenuReturnMode
		p.pullMenuFocus = 0
		return p, nil
	case "p":
		return p.executePullMenuAction(0)
	case "r":
		return p.executePullMenuAction(1)
	case "f":
		return p.executePullMenuAction(2)
	case "a":
		return p.executePullMenuAction(3)
	}
	return p, nil
}

// executePullMenuAction executes the pull menu action at the given index.
func (p *Plugin) executePullMenuAction(idx int) (plugin.Plugin, tea.Cmd) {
	p.viewMode = p.pullMenuReturnMode
	p.pullInProgress = true
	p.pullError = ""
	p.pullSuccess = false
	p.pullMenuFocus = 0

	switch idx {
	case 0:
		return p, p.doPull()
	case 1:
		return p, p.doPullRebase()
	case 2:
		return p, p.doPullFFOnly()
	case 3:
		return p, p.doPullAutostash()
	}
	return p, nil
}

// updatePullConflict handles key events in the pull conflict modal.
func (p *Plugin) updatePullConflict(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "a":
		// Abort merge/rebase
		p.viewMode = ViewModeStatus
		return p, p.doAbortPull()
	case "esc", "q":
		// Dismiss modal (conflicts remain, user resolves manually)
		p.viewMode = ViewModeStatus
		p.pullConflictFiles = nil
		return p, p.refresh()
	}
	return p, nil
}

// executePushMenuAction executes the push menu action at the given index.
func (p *Plugin) executePushMenuAction(idx int) (plugin.Plugin, tea.Cmd) {
	p.viewMode = p.pushMenuReturnMode
	p.pushInProgress = true
	p.pushError = ""
	p.pushSuccess = false
	p.pushMenuFocus = 0

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

// handleConfirmKey handles common key input for confirm dialogs with button pairs.
// focusPtr points to the button focus state (1=confirm, 2=cancel).
// onConfirm is called when the user confirms (enter on confirm, y/Y).
// onCancel is called when the user cancels (enter on cancel, esc/n/N/q).
func (p *Plugin) handleConfirmKey(msg tea.KeyMsg, focusPtr *int, onConfirm, onCancel func()) (bool, tea.Cmd) {
	switch msg.String() {
	case "tab":
		if *focusPtr == 1 {
			*focusPtr = 2
		} else {
			*focusPtr = 1
		}
		return true, nil
	case "shift+tab":
		if *focusPtr == 2 {
			*focusPtr = 1
		} else {
			*focusPtr = 2
		}
		return true, nil
	case "enter":
		if *focusPtr == 2 {
			onCancel()
			return true, nil
		}
		onConfirm()
		return true, nil
	case "y", "Y":
		onConfirm()
		return true, nil
	case "esc", "n", "N", "q":
		onCancel()
		return true, nil
	}
	return false, nil
}

func (p *Plugin) updateConfirmStashPop(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	var cmd tea.Cmd
	handled, _ := p.handleConfirmKey(msg, &p.stashPopButtonFocus,
		func() {
			// Confirm
			if p.stashPopItem != nil {
				cmd = p.doStashPop()
			}
			p.viewMode = ViewModeStatus
			p.stashPopItem = nil
			p.stashPopButtonFocus = 1
		},
		func() {
			// Cancel
			p.viewMode = ViewModeStatus
			p.stashPopItem = nil
			p.stashPopButtonFocus = 1
		},
	)
	if handled {
		return p, cmd
	}
	return p, nil
}

// updateConfirmDiscard handles key events in the confirm discard modal.
func (p *Plugin) updateConfirmDiscard(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
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

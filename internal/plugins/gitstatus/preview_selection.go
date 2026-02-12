package gitstatus

import (
	tea "github.com/charmbracelet/bubbletea"
)

// getHighlighter returns a syntax highlighter for the given filename.
// Caches the highlighter to avoid re-creating on every render.
func (p *Plugin) getHighlighter(filename string) *SyntaxHighlighter {
	if filename == "" {
		return nil
	}
	if p.syntaxHighlighterFile == filename && p.syntaxHighlighter != nil {
		return p.syntaxHighlighter
	}
	p.syntaxHighlighter = NewSyntaxHighlighter(filename)
	p.syntaxHighlighterFile = filename
	return p.syntaxHighlighter
}

// totalSelectableItems returns the count of all selectable items (files + commits).
func (p *Plugin) totalSelectableItems() int {
	entries := p.tree.AllEntries()
	commits := p.recentCommits
	if p.historyFilterActive && p.filteredCommits != nil {
		commits = p.filteredCommits
	}
	return len(entries) + len(commits)
}

// cursorOnCommit returns true if cursor is on a commit (past all files).
func (p *Plugin) cursorOnCommit() bool {
	return p.cursor >= len(p.tree.AllEntries())
}

// activeCommits returns filtered commits if filter active, otherwise recent commits.
func (p *Plugin) activeCommits() []*Commit {
	if p.historyFilterActive && p.filteredCommits != nil {
		return p.filteredCommits
	}
	return p.recentCommits
}

// selectedCommitIndex returns the index into active commits when cursor is on commit.
func (p *Plugin) selectedCommitIndex() int {
	entries := p.tree.AllEntries()
	return p.cursor - len(entries)
}

// autoLoadDiff triggers loading the diff for the currently selected file or folder.
func (p *Plugin) autoLoadDiff() tea.Cmd {
	entries := p.tree.AllEntries()
	if len(entries) == 0 || p.cursor >= len(entries) {
		p.selectedDiffFile = ""
		p.diffPaneParsedDiff = nil
		return nil
	}

	entry := entries[p.cursor]
	isNewFile := entry.Path != p.selectedDiffFile
	if !isNewFile && !p.forceNextDiffReload {
		return nil // Already loaded
	}

	p.selectedDiffFile = entry.Path
	p.forceNextDiffReload = false
	if isNewFile {
		// Only reset scroll when switching to a different file
		p.diffPaneScroll = 0
	}
	// Clear commit preview when switching to file
	p.previewCommit = nil

	// Handle folder entries
	if entry.IsFolder {
		return p.loadFolderDiff(entry)
	}

	return p.loadInlineDiff(entry.Path, entry.Staged, entry.Status)
}

// autoLoadCommitPreview triggers loading commit detail for the currently selected commit.
func (p *Plugin) autoLoadCommitPreview() tea.Cmd {
	if !p.cursorOnCommit() {
		p.previewCommit = nil
		return nil
	}

	commits := p.activeCommits()
	commitIdx := p.selectedCommitIndex()
	if commitIdx < 0 || commitIdx >= len(commits) {
		p.previewCommit = nil
		return nil
	}

	commit := commits[commitIdx]
	// Already loaded this commit?
	if p.previewCommit != nil && p.previewCommit.Hash == commit.Hash {
		return nil
	}

	// Clear file diff when switching to commit
	p.selectedDiffFile = ""
	p.diffPaneParsedDiff = nil
	p.previewCommitCursor = 0
	p.previewCommitScroll = 0

	return p.loadCommitDetailForPreview(commit.Hash)
}

// autoLoadPreview loads the appropriate preview for the current cursor position.
// When forceReload is true, bypasses diff dedup so the load always fires
// (use after operations that change the file list like stage/unstage/discard/commit).
// Commit previews are not force-reloaded since commits are immutable.
func (p *Plugin) autoLoadPreview(forceReload bool) tea.Cmd {
	if forceReload {
		p.forceNextDiffReload = true
	}
	if p.cursorOnCommit() {
		return p.autoLoadCommitPreview()
	}
	return p.autoLoadDiff()
}

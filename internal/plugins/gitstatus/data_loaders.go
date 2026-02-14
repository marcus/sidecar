package gitstatus

import (
	tea "github.com/charmbracelet/bubbletea"
)

// loadDiff loads the diff for a file.
func (p *Plugin) loadDiff(path string, staged bool, status FileStatus) tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	return func() tea.Msg {
		var rawDiff string
		var err error

		// Untracked files need special handling - create new file diff
		if status == StatusUntracked {
			rawDiff, err = GetNewFileDiff(workDir, path)
		} else {
			rawDiff, err = GetDiff(workDir, path, staged)
		}
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return DiffLoadedMsg{Epoch: epoch, Content: rawDiff, Raw: rawDiff}
	}
}

// loadInlineDiff loads a diff for inline preview in the three-pane view.
func (p *Plugin) loadInlineDiff(path string, staged bool, status FileStatus) tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	return func() tea.Msg {
		var rawDiff string
		var err error

		// Untracked files need special handling - create new file diff
		if status == StatusUntracked {
			rawDiff, err = GetNewFileDiff(workDir, path)
		} else {
			rawDiff, err = GetDiff(workDir, path, staged)
		}
		if err != nil {
			return InlineDiffLoadedMsg{Epoch: epoch, File: path, Raw: "", Parsed: nil}
		}
		parsed, _ := ParseUnifiedDiff(rawDiff)
		return InlineDiffLoadedMsg{Epoch: epoch, File: path, Raw: rawDiff, Parsed: parsed}
	}
}

// loadRecentCommits loads recent commits for the sidebar with push status.
func (p *Plugin) loadRecentCommits() tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	return func() tea.Msg {
		commits, pushStatus, err := GetCommitHistoryWithPushStatus(workDir, commitHistoryPageSize)
		if err != nil {
			return RecentCommitsLoadedMsg{Epoch: epoch, Commits: nil, PushStatus: nil}
		}
		return RecentCommitsLoadedMsg{Epoch: epoch, Commits: commits, PushStatus: pushStatus}
	}
}

// loadMoreCommits fetches the next batch of commits for infinite scroll.
func (p *Plugin) loadMoreCommits() tea.Cmd {
	if p.loadingMoreCommits {
		return nil
	}
	p.loadingMoreCommits = true

	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	skip := len(p.recentCommits)
	return func() tea.Msg {
		commits, pushStatus, err := GetCommitHistoryWithPushStatusOffset(workDir, commitHistoryPageSize, skip)
		if err != nil {
			return MoreCommitsLoadedMsg{Epoch: epoch, Commits: nil, PushStatus: nil}
		}
		return MoreCommitsLoadedMsg{Epoch: epoch, Commits: commits, PushStatus: pushStatus}
	}
}

// loadFilteredCommits fetches commits with current filter options.
func (p *Plugin) loadFilteredCommits() tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	opts := HistoryFilterOpts{
		Author: p.historyFilterAuthor,
		Path:   p.historyFilterPath,
		Limit:  50,
	}
	return func() tea.Msg {
		commits, pushStatus, err := GetCommitHistoryFilteredWithPushStatus(workDir, opts)
		if err != nil {
			return FilteredCommitsLoadedMsg{Epoch: epoch, Commits: nil, PushStatus: nil}
		}
		return FilteredCommitsLoadedMsg{Epoch: epoch, Commits: commits, PushStatus: pushStatus}
	}
}

// loadFolderDiff loads a concatenated diff for all files in a folder.
func (p *Plugin) loadFolderDiff(entry *FileEntry) tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	folderPath := entry.Path
	children := entry.Children
	return func() tea.Msg {
		rawDiff, err := GetFolderDiff(workDir, children)
		if err != nil {
			return InlineDiffLoadedMsg{Epoch: epoch, File: folderPath, Raw: "", Parsed: nil}
		}
		parsed, _ := ParseUnifiedDiff(rawDiff)
		return InlineDiffLoadedMsg{Epoch: epoch, File: folderPath, Raw: rawDiff, Parsed: parsed}
	}
}

// loadFullFolderDiff loads a concatenated diff for full-screen view.
func (p *Plugin) loadFullFolderDiff(entry *FileEntry) tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	children := entry.Children
	return func() tea.Msg {
		rawDiff, err := GetFolderDiff(workDir, children)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return DiffLoadedMsg{Epoch: epoch, Content: rawDiff, Raw: rawDiff}
	}
}

// loadCommitFileDiff loads diff for a file in a commit.
// parentHash should be the first parent hash for merge commits, or "" for regular commits.
func (p *Plugin) loadCommitFileDiff(hash, path, parentHash string) tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	return func() tea.Msg {
		rawDiff, err := GetCommitDiff(workDir, hash, path, parentHash)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return DiffLoadedMsg{Epoch: epoch, Content: rawDiff, Raw: rawDiff}
	}
}

// loadCommitDetailForPreview loads commit detail for inline preview.
func (p *Plugin) loadCommitDetailForPreview(hash string) tea.Cmd {
	epoch := p.ctx.Epoch
	workDir := p.repoRoot
	return func() tea.Msg {
		commit, err := GetCommitDetail(workDir, hash)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return CommitPreviewLoadedMsg{Epoch: epoch, Commit: commit}
	}
}

package gitstatus

import (
	tea "github.com/charmbracelet/bubbletea"
)

// ensurePreviewCursorVisible adjusts scroll to keep commit preview cursor visible.
func (p *Plugin) ensurePreviewCursorVisible() {
	// Estimate visible file rows (rough - matches renderCommitPreview calculation)
	visibleRows := p.height - 15
	if visibleRows < 3 {
		visibleRows = 3
	}

	if p.previewCommitCursor < p.previewCommitScroll {
		p.previewCommitScroll = p.previewCommitCursor
	} else if p.previewCommitCursor >= p.previewCommitScroll+visibleRows {
		p.previewCommitScroll = p.previewCommitCursor - visibleRows + 1
	}
}

// ensureCursorVisible adjusts scroll to keep cursor visible.
func (p *Plugin) ensureCursorVisible() {
	visibleRows := p.height - 4 // Account for header and section spacing
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	} else if p.cursor >= p.scrollOff+visibleRows {
		p.scrollOff = p.cursor - visibleRows + 1
	}
}

// visibleCommitCount returns how many commits can display in the sidebar.
func (p *Plugin) visibleCommitCount() int {
	// paneHeight = p.height - 2 (borders)
	// innerHeight = paneHeight - 2 (header)
	paneHeight := p.height - 2
	if paneHeight < 4 {
		paneHeight = 4
	}
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	return p.commitSectionCapacity(innerHeight)
}

func (p *Plugin) clampCommitScroll() {
	visibleCommits := p.visibleCommitCount()
	if visibleCommits < 1 {
		visibleCommits = 1
	}
	maxOff := len(p.recentCommits) - visibleCommits
	if maxOff < 0 {
		maxOff = 0
	}
	if p.commitScrollOff < 0 {
		p.commitScrollOff = 0
	}
	if p.commitScrollOff > maxOff {
		p.commitScrollOff = maxOff
	}
}

func (p *Plugin) ensureCommitListFilled() tea.Cmd {
	if p.historyFilterActive || p.loadingMoreCommits || !p.moreCommitsAvailable {
		return nil
	}
	visibleCommits := p.visibleCommitCount()
	if visibleCommits < 1 || len(p.recentCommits) >= visibleCommits {
		return nil
	}
	return p.loadMoreCommits()
}

// commitSectionCapacity returns how many commits can display in the sidebar
// given the current layout and inner height.
func (p *Plugin) commitSectionCapacity(visibleHeight int) int {
	// visibleHeight already excludes the "Files" header lines.
	linesUsed := 0

	entries := p.tree.AllEntries()
	if len(entries) == 0 {
		// "Working tree clean"
		linesUsed++
	} else {
		// Reserve space for commits when files are present (match renderSidebar)
		commitsReserve := 5
		if len(p.recentCommits) > 3 {
			commitsReserve = 6
		}
		filesHeight := visibleHeight - commitsReserve - 2 // -2 for section headers
		if filesHeight < 3 {
			filesHeight = 3
		}

		lineNum := 0
		if len(p.tree.Staged) > 0 && lineNum < filesHeight {
			lineNum++ // section header
			for range p.tree.Staged {
				if lineNum >= filesHeight {
					break
				}
				lineNum++
			}
		}
		if len(p.tree.Modified) > 0 && lineNum < filesHeight {
			if len(p.tree.Staged) > 0 {
				if lineNum < filesHeight {
					lineNum++ // blank line between sections
				}
			}
			if lineNum < filesHeight {
				lineNum++ // section header
			}
			for range p.tree.Modified {
				if lineNum >= filesHeight {
					break
				}
				lineNum++
			}
		}
		if len(p.tree.Untracked) > 0 && lineNum < filesHeight {
			if len(p.tree.Staged) > 0 || len(p.tree.Modified) > 0 {
				if lineNum < filesHeight {
					lineNum++ // blank line between sections
				}
			}
			if lineNum < filesHeight {
				lineNum++ // section header
			}
			for range p.tree.Untracked {
				if lineNum >= filesHeight {
					break
				}
				lineNum++
			}
		}
		linesUsed += lineNum
	}

	// Separator (blank line + separator line)
	linesUsed += 2

	// Remote operation status line if present
	if p.pushInProgress || p.fetchInProgress || p.pullInProgress ||
		p.pushSuccess || p.fetchSuccess || p.pullSuccess ||
		p.pushError != "" || p.fetchError != "" || p.pullError != "" {
		linesUsed++
	}

	// Commit header
	linesUsed += 1

	available := visibleHeight - linesUsed
	if available < 2 {
		available = 2
	}
	return available
}

// ensureCommitVisible adjusts commitScrollOff to keep selected commit visible.
// commitIdx is the absolute index into activeCommits.
func (p *Plugin) ensureCommitVisible(commitIdx int) {
	commits := p.activeCommits()
	if len(commits) == 0 {
		p.commitScrollOff = 0
		return
	}

	// Use a conservative estimate for visible commits
	visibleCommits := p.visibleCommitCount()
	if visibleCommits < 1 {
		visibleCommits = 1
	}

	// Only adjust scroll if absolutely necessary
	if commitIdx < p.commitScrollOff {
		// Commit is above visible area - scroll up to show it at top
		p.commitScrollOff = commitIdx
	} else if commitIdx >= p.commitScrollOff+visibleCommits {
		// Commit is below visible area - scroll down minimally
		p.commitScrollOff = commitIdx - visibleCommits + 1
	}
	// If commit is within visible range, don't adjust scroll at all

	// Clamp scroll offset to valid range
	if p.commitScrollOff < 0 {
		p.commitScrollOff = 0
	}
	// Ensure we don't scroll past where we'd have empty rows
	// This is the key: maxOff ensures the visible area is always filled
	maxOff := len(commits) - visibleCommits
	if maxOff < 0 {
		maxOff = 0
	}
	if p.commitScrollOff > maxOff {
		p.commitScrollOff = maxOff
	}
}

// clampDiffHorizScroll clamps diffHorizOff to valid range based on content width.
func (p *Plugin) clampDiffHorizScroll() {
	if p.parsedDiff == nil {
		return
	}
	// Calculate content width like the view does
	panelWidth := (p.width - 3) / 2 // -3 for center separator
	lineNoWidth := 5
	contentWidth := panelWidth - lineNoWidth - 2

	clipInfo := GetSideBySideClipInfo(p.parsedDiff, contentWidth, p.diffHorizOff)
	maxScroll := clipInfo.MaxContentWidth - contentWidth
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.diffHorizOff > maxScroll {
		p.diffHorizOff = maxScroll
	}
	if p.diffHorizOff < 0 {
		p.diffHorizOff = 0
	}
}

// clampDiffPaneHorizScroll clamps diffPaneHorizScroll to valid range.
func (p *Plugin) clampDiffPaneHorizScroll() {
	if p.diffPaneParsedDiff == nil {
		return
	}
	// Calculate content width for inline diff pane
	paneWidth := p.width - p.sidebarWidth - 2
	panelWidth := (paneWidth - 3) / 2
	lineNoWidth := 5
	contentWidth := panelWidth - lineNoWidth - 2

	clipInfo := GetSideBySideClipInfo(p.diffPaneParsedDiff, contentWidth, p.diffPaneHorizScroll)
	maxScroll := clipInfo.MaxContentWidth - contentWidth
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.diffPaneHorizScroll > maxScroll {
		p.diffPaneHorizScroll = maxScroll
	}
	if p.diffPaneHorizScroll < 0 {
		p.diffPaneHorizScroll = 0
	}
}

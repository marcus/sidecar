package workspace

// kanbanColumnOrder defines the order of columns in kanban view.
var kanbanColumnOrder = []WorktreeStatus{StatusActive, StatusThinking, StatusWaiting, StatusDone, StatusPaused}

const kanbanShellColumnIndex = 0

func kanbanColumnCount() int {
	return len(kanbanColumnOrder) + 1 // Shells column + worktree columns
}

func kanbanStatusForColumn(col int) (WorktreeStatus, bool) {
	if col <= kanbanShellColumnIndex {
		return 0, false
	}
	idx := col - 1
	if idx < 0 || idx >= len(kanbanColumnOrder) {
		return 0, false
	}
	return kanbanColumnOrder[idx], true
}

func (p *Plugin) kanbanColumnItemCount(col int, columns map[WorktreeStatus][]*Worktree) int {
	if col == kanbanShellColumnIndex {
		return len(p.shells)
	}
	status, ok := kanbanStatusForColumn(col)
	if !ok {
		return 0
	}
	return len(columns[status])
}

func (p *Plugin) kanbanShellAt(row int) *ShellSession {
	if row < 0 || row >= len(p.shells) {
		return nil
	}
	return p.shells[row]
}

// getKanbanColumns returns worktrees grouped by status for kanban view.
// StatusError worktrees are grouped with StatusPaused since they require user intervention.
func (p *Plugin) getKanbanColumns() map[WorktreeStatus][]*Worktree {
	columns := map[WorktreeStatus][]*Worktree{
		StatusActive:   {},
		StatusThinking: {},
		StatusWaiting:  {},
		StatusDone:     {},
		StatusPaused:   {},
	}
	for _, wt := range p.worktrees {
		status := wt.Status
		// Group error worktrees with paused since they require user intervention
		if status == StatusError {
			status = StatusPaused
		}
		columns[status] = append(columns[status], wt)
	}
	return columns
}

// selectedKanbanWorktree returns the worktree at the current kanban position.
func (p *Plugin) selectedKanbanWorktree() *Worktree {
	columns := p.getKanbanColumns()
	if p.kanbanCol == kanbanShellColumnIndex {
		return nil
	}
	status, ok := kanbanStatusForColumn(p.kanbanCol)
	if !ok {
		return nil
	}
	items := columns[status]
	if p.kanbanRow < 0 || p.kanbanRow >= len(items) {
		return nil
	}
	return items[p.kanbanRow]
}

// syncKanbanToList syncs the kanban selection to the list selectedIdx.
func (p *Plugin) syncKanbanToList() {
	if p.kanbanCol == kanbanShellColumnIndex {
		shell := p.kanbanShellAt(p.kanbanRow)
		if shell == nil {
			return
		}
		p.shellSelected = true
		p.selectedShellIdx = p.kanbanRow
		return
	}
	wt := p.selectedKanbanWorktree()
	if wt == nil {
		return
	}
	for i, w := range p.worktrees {
		if w.Name == wt.Name {
			p.shellSelected = false
			p.selectedIdx = i
			return
		}
	}
}

func (p *Plugin) applyKanbanSelectionChange(oldShellSelected bool, oldShellIdx, oldWorktreeIdx int) bool {
	selectionChanged := p.shellSelected != oldShellSelected ||
		(p.shellSelected && p.selectedShellIdx != oldShellIdx) ||
		(!p.shellSelected && p.selectedIdx != oldWorktreeIdx)
	if selectionChanged {
		p.previewOffset = 0
		p.autoScrollOutput = true
		p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot for new selection
		p.taskLoading = false
		p.exitInteractiveMode()
		p.saveSelectionState()
	}
	return selectionChanged
}

// moveKanbanColumn moves the kanban cursor to an adjacent column (navigation only, no selection sync).
func (p *Plugin) moveKanbanColumn(delta int) {
	columns := p.getKanbanColumns()
	newCol := p.kanbanCol + delta

	// Wrap around or clamp
	if newCol < 0 {
		newCol = 0
	}
	if newCol >= kanbanColumnCount() {
		newCol = kanbanColumnCount() - 1
	}

	if newCol != p.kanbanCol {
		p.kanbanCol = newCol
		// Try to preserve row position, but clamp to new column's item count
		count := p.kanbanColumnItemCount(p.kanbanCol, columns)
		if count == 0 {
			p.kanbanRow = 0
		} else if p.kanbanRow >= count {
			p.kanbanRow = count - 1
		}
	}
}

// moveKanbanRow moves the kanban cursor within the current column (navigation only, no selection sync).
func (p *Plugin) moveKanbanRow(delta int) {
	columns := p.getKanbanColumns()
	count := p.kanbanColumnItemCount(p.kanbanCol, columns)

	if count == 0 {
		return
	}

	newRow := p.kanbanRow + delta
	if newRow < 0 {
		newRow = 0
	}
	if newRow >= count {
		newRow = count - 1
	}

	p.kanbanRow = newRow
}

// getKanbanWorktree returns the worktree at the given Kanban coordinates.
func (p *Plugin) getKanbanWorktree(col, row int) *Worktree {
	columns := p.getKanbanColumns()
	if col == kanbanShellColumnIndex {
		return nil
	}
	status, ok := kanbanStatusForColumn(col)
	if !ok {
		return nil
	}
	items := columns[status]
	if row >= 0 && row < len(items) {
		return items[row]
	}
	return nil
}

// syncListToKanban syncs the list selectedIdx to kanban position.
// Called when switching from list to kanban view.
func (p *Plugin) syncListToKanban() {
	if p.shellSelected {
		p.kanbanCol = kanbanShellColumnIndex
		if p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			p.kanbanRow = p.selectedShellIdx
		} else {
			p.kanbanRow = 0
		}
		return
	}
	wt := p.selectedWorktree()
	if wt == nil {
		p.kanbanCol = 0
		p.kanbanRow = 0
		return
	}

	columns := p.getKanbanColumns()
	for colIdx, status := range kanbanColumnOrder {
		items := columns[status]
		for rowIdx, item := range items {
			if item.Name == wt.Name {
				p.kanbanCol = colIdx + 1
				p.kanbanRow = rowIdx
				return
			}
		}
	}

	// Worktree not found in any column, default to first column
	p.kanbanCol = 0
	p.kanbanRow = 0
}

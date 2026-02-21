package workspace

// kanbanColumnOrder defines the order of columns in kanban view.
var kanbanColumnOrder = []WorktreeStatus{StatusActive, StatusThinking, StatusWaiting, StatusDone, StatusPaused}

const kanbanShellColumnIndex = 0

func kanbanColumnCount() int {
	return len(kanbanColumnOrder) + 1 // Shells column + status columns
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

// kanbanData holds pre-computed kanban column data for a render/update cycle.
// Status columns contain worktrees first, then agent-shells.
// The Shells column (index 0) contains only plain shells (no agent or orphaned).
type kanbanData struct {
	worktrees   map[WorktreeStatus][]*Worktree
	shells      map[WorktreeStatus][]*ShellSession
	plainShells []*ShellSession
}

// kanbanShellStatus maps a shell's agent status to a WorktreeStatus for kanban routing.
// Returns false if the shell should stay in the plain Shells column (no agent, orphaned, etc).
func kanbanShellStatus(shell *ShellSession) (WorktreeStatus, bool) {
	if shell.ChosenAgent == AgentNone || shell.ChosenAgent == "" || shell.Agent == nil {
		return 0, false
	}
	if shell.IsOrphaned {
		return 0, false
	}
	switch shell.Agent.Status {
	case AgentStatusWaiting:
		return StatusWaiting, true
	case AgentStatusDone:
		return StatusDone, true
	case AgentStatusError:
		return StatusPaused, true
	default:
		// AgentStatusRunning and AgentStatusIdle both map to Active
		return StatusActive, true
	}
}

// buildKanbanData computes column assignments for all worktrees and shells.
func (p *Plugin) buildKanbanData() *kanbanData {
	kd := &kanbanData{
		worktrees: map[WorktreeStatus][]*Worktree{
			StatusActive:   {},
			StatusThinking: {},
			StatusWaiting:  {},
			StatusDone:     {},
			StatusPaused:   {},
		},
		shells: map[WorktreeStatus][]*ShellSession{
			StatusActive:   {},
			StatusThinking: {},
			StatusWaiting:  {},
			StatusDone:     {},
			StatusPaused:   {},
		},
	}

	for _, wt := range p.worktrees {
		status := wt.Status
		if status == StatusError {
			status = StatusPaused
		}
		kd.worktrees[status] = append(kd.worktrees[status], wt)
	}

	for _, shell := range p.shells {
		if status, ok := kanbanShellStatus(shell); ok {
			kd.shells[status] = append(kd.shells[status], shell)
		} else {
			kd.plainShells = append(kd.plainShells, shell)
		}
	}

	return kd
}

// columnItemCount returns total items (worktrees + shells) in a column.
func (kd *kanbanData) columnItemCount(col int) int {
	if col == kanbanShellColumnIndex {
		return len(kd.plainShells)
	}
	status, ok := kanbanStatusForColumn(col)
	if !ok {
		return 0
	}
	return len(kd.worktrees[status]) + len(kd.shells[status])
}

// itemAt returns the worktree or shell at (col, row). Worktrees come first in each column.
func (kd *kanbanData) itemAt(col, row int) (*Worktree, *ShellSession) {
	if col == kanbanShellColumnIndex {
		if row >= 0 && row < len(kd.plainShells) {
			return nil, kd.plainShells[row]
		}
		return nil, nil
	}
	status, ok := kanbanStatusForColumn(col)
	if !ok {
		return nil, nil
	}
	wts := kd.worktrees[status]
	if row < len(wts) {
		return wts[row], nil
	}
	shellRow := row - len(wts)
	shells := kd.shells[status]
	if shellRow >= 0 && shellRow < len(shells) {
		return nil, shells[shellRow]
	}
	return nil, nil
}

// --- Plugin methods that use kanbanData ---

// getKanbanColumns is a backward-compatible helper that returns just worktree columns.
// Used by code paths that don't need shell data.
func (p *Plugin) getKanbanColumns() map[WorktreeStatus][]*Worktree {
	kd := p.buildKanbanData()
	return kd.worktrees
}

func (p *Plugin) kanbanShellAt(row int) *ShellSession {
	kd := p.buildKanbanData()
	if row < 0 || row >= len(kd.plainShells) {
		return nil
	}
	return kd.plainShells[row]
}

// selectedKanbanItem returns the worktree or shell at the current kanban cursor.
func (p *Plugin) selectedKanbanItem() (*Worktree, *ShellSession) {
	kd := p.buildKanbanData()
	return kd.itemAt(p.kanbanCol, p.kanbanRow)
}

// selectedKanbanWorktree returns the worktree at the current kanban position (nil if shell).
func (p *Plugin) selectedKanbanWorktree() *Worktree {
	wt, _ := p.selectedKanbanItem()
	return wt
}

// syncKanbanToList syncs the kanban selection to the list selectedIdx.
func (p *Plugin) syncKanbanToList() {
	wt, shell := p.selectedKanbanItem()
	if shell != nil {
		// Shell selected — find its index in p.shells
		for i, s := range p.shells {
			if s.TmuxName == shell.TmuxName {
				p.shellSelected = true
				p.selectedShellIdx = i
				return
			}
		}
		return
	}
	if wt != nil {
		for i, w := range p.worktrees {
			if w.Name == wt.Name {
				p.shellSelected = false
				p.selectedIdx = i
				return
			}
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
	kd := p.buildKanbanData()
	newCol := p.kanbanCol + delta

	if newCol < 0 {
		newCol = 0
	}
	if newCol >= kanbanColumnCount() {
		newCol = kanbanColumnCount() - 1
	}

	if newCol != p.kanbanCol {
		p.kanbanCol = newCol
		count := kd.columnItemCount(p.kanbanCol)
		if count == 0 {
			p.kanbanRow = 0
		} else if p.kanbanRow >= count {
			p.kanbanRow = count - 1
		}
	}
}

// moveKanbanRow moves the kanban cursor within the current column (navigation only, no selection sync).
func (p *Plugin) moveKanbanRow(delta int) {
	kd := p.buildKanbanData()
	count := kd.columnItemCount(p.kanbanCol)

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
	kd := p.buildKanbanData()
	wt, _ := kd.itemAt(col, row)
	return wt
}

// syncListToKanban syncs the list selectedIdx to kanban position.
// Called when switching from list to kanban view.
func (p *Plugin) syncListToKanban() {
	kd := p.buildKanbanData()

	if p.shellSelected {
		// Find which column/row the selected shell is in
		shell := p.getSelectedShell()
		if shell == nil {
			p.kanbanCol = kanbanShellColumnIndex
			p.kanbanRow = 0
			return
		}
		// Check plain shells first (column 0)
		for i, s := range kd.plainShells {
			if s.TmuxName == shell.TmuxName {
				p.kanbanCol = kanbanShellColumnIndex
				p.kanbanRow = i
				return
			}
		}
		// Check status columns
		for colIdx, status := range kanbanColumnOrder {
			wtCount := len(kd.worktrees[status])
			for i, s := range kd.shells[status] {
				if s.TmuxName == shell.TmuxName {
					p.kanbanCol = colIdx + 1
					p.kanbanRow = wtCount + i // shells come after worktrees
					return
				}
			}
		}
		// Not found, default
		p.kanbanCol = kanbanShellColumnIndex
		p.kanbanRow = 0
		return
	}

	wt := p.selectedWorktree()
	if wt == nil {
		p.kanbanCol = 0
		p.kanbanRow = 0
		return
	}

	for colIdx, status := range kanbanColumnOrder {
		items := kd.worktrees[status]
		for rowIdx, item := range items {
			if item.Name == wt.Name {
				p.kanbanCol = colIdx + 1
				p.kanbanRow = rowIdx
				return
			}
		}
	}

	p.kanbanCol = 0
	p.kanbanRow = 0
}

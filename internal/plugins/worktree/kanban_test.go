package worktree

import (
	"testing"
)

func TestGetKanbanColumns(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusWaiting},
			{Name: "wt3", Status: StatusDone},
			{Name: "wt4", Status: StatusPaused},
			{Name: "wt5", Status: StatusActive},
			{Name: "wt6", Status: StatusError}, // Should be grouped with Paused
		},
	}

	columns := p.getKanbanColumns()

	if len(columns[StatusActive]) != 2 {
		t.Errorf("expected 2 active worktrees, got %d", len(columns[StatusActive]))
	}
	if len(columns[StatusWaiting]) != 1 {
		t.Errorf("expected 1 waiting worktree, got %d", len(columns[StatusWaiting]))
	}
	if len(columns[StatusDone]) != 1 {
		t.Errorf("expected 1 done worktree, got %d", len(columns[StatusDone]))
	}
	// Paused should include both StatusPaused and StatusError worktrees
	if len(columns[StatusPaused]) != 2 {
		t.Errorf("expected 2 paused worktrees (1 paused + 1 error), got %d", len(columns[StatusPaused]))
	}
}

func TestGetKanbanColumnsEmpty(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{},
	}

	columns := p.getKanbanColumns()

	for _, status := range kanbanColumnOrder {
		if len(columns[status]) != 0 {
			t.Errorf("expected empty column for %v, got %d items", status, len(columns[status]))
		}
	}
}

func TestSyncListToKanban(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusWaiting},
			{Name: "wt3", Status: StatusDone},
			{Name: "wt4", Status: StatusPaused},
		},
		selectedIdx: 2, // wt3 (Done)
	}

	p.syncListToKanban()

	if p.kanbanCol != 2 { // Done column
		t.Errorf("expected kanbanCol=2 (Done), got %d", p.kanbanCol)
	}
	if p.kanbanRow != 0 { // First item in Done column
		t.Errorf("expected kanbanRow=0, got %d", p.kanbanRow)
	}
}

func TestSyncListToKanbanWithErrorStatus(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusError}, // Should be in Paused column
		},
		selectedIdx: 1, // wt2 (Error -> Paused)
	}

	p.syncListToKanban()

	if p.kanbanCol != 3 { // Paused column (index 3)
		t.Errorf("expected kanbanCol=3 (Paused), got %d", p.kanbanCol)
	}
	if p.kanbanRow != 0 {
		t.Errorf("expected kanbanRow=0, got %d", p.kanbanRow)
	}
}

func TestSyncListToKanbanNoWorktrees(t *testing.T) {
	p := &Plugin{
		worktrees:   []*Worktree{},
		selectedIdx: 0,
		kanbanCol:   2,
		kanbanRow:   5,
	}

	p.syncListToKanban()

	if p.kanbanCol != 0 {
		t.Errorf("expected kanbanCol=0, got %d", p.kanbanCol)
	}
	if p.kanbanRow != 0 {
		t.Errorf("expected kanbanRow=0, got %d", p.kanbanRow)
	}
}

func TestSyncKanbanToList(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusWaiting},
			{Name: "wt3", Status: StatusDone},
			{Name: "wt4", Status: StatusPaused},
		},
		kanbanCol:   1, // Waiting column
		kanbanRow:   0, // First item (wt2)
		selectedIdx: 0,
	}

	p.syncKanbanToList()

	if p.selectedIdx != 1 { // wt2 is at index 1
		t.Errorf("expected selectedIdx=1, got %d", p.selectedIdx)
	}
}

func TestSyncKanbanToListEmptyColumn(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
		},
		kanbanCol:   1, // Waiting column (empty)
		kanbanRow:   0,
		selectedIdx: 0,
	}

	p.syncKanbanToList()

	// selectedIdx should remain unchanged since column is empty
	if p.selectedIdx != 0 {
		t.Errorf("expected selectedIdx=0 (unchanged), got %d", p.selectedIdx)
	}
}

func TestMoveKanbanColumn(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusWaiting},
		},
		kanbanCol: 0, // Start at Active
		kanbanRow: 0,
	}

	// Move right
	p.moveKanbanColumn(1)
	if p.kanbanCol != 1 {
		t.Errorf("expected kanbanCol=1 after move right, got %d", p.kanbanCol)
	}

	// Move left
	p.moveKanbanColumn(-1)
	if p.kanbanCol != 0 {
		t.Errorf("expected kanbanCol=0 after move left, got %d", p.kanbanCol)
	}

	// Move left at boundary (should stay at 0)
	p.moveKanbanColumn(-1)
	if p.kanbanCol != 0 {
		t.Errorf("expected kanbanCol=0 at left boundary, got %d", p.kanbanCol)
	}

	// Move to far right
	p.kanbanCol = len(kanbanColumnOrder) - 1
	p.moveKanbanColumn(1)
	if p.kanbanCol != len(kanbanColumnOrder)-1 {
		t.Errorf("expected kanbanCol=%d at right boundary, got %d", len(kanbanColumnOrder)-1, p.kanbanCol)
	}
}

func TestMoveKanbanRow(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusActive},
			{Name: "wt3", Status: StatusActive},
		},
		kanbanCol: 0, // Active column (has 3 items)
		kanbanRow: 0,
	}

	// Move down
	p.moveKanbanRow(1)
	if p.kanbanRow != 1 {
		t.Errorf("expected kanbanRow=1 after move down, got %d", p.kanbanRow)
	}

	// Move down again
	p.moveKanbanRow(1)
	if p.kanbanRow != 2 {
		t.Errorf("expected kanbanRow=2 after move down, got %d", p.kanbanRow)
	}

	// Move down at boundary (should stay at 2)
	p.moveKanbanRow(1)
	if p.kanbanRow != 2 {
		t.Errorf("expected kanbanRow=2 at bottom boundary, got %d", p.kanbanRow)
	}

	// Move up
	p.moveKanbanRow(-1)
	if p.kanbanRow != 1 {
		t.Errorf("expected kanbanRow=1 after move up, got %d", p.kanbanRow)
	}

	// Move up to top
	p.kanbanRow = 0
	p.moveKanbanRow(-1)
	if p.kanbanRow != 0 {
		t.Errorf("expected kanbanRow=0 at top boundary, got %d", p.kanbanRow)
	}
}

func TestMoveKanbanRowEmptyColumn(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
		},
		kanbanCol: 1, // Waiting column (empty)
		kanbanRow: 0,
	}

	// Move should have no effect on empty column
	p.moveKanbanRow(1)
	if p.kanbanRow != 0 {
		t.Errorf("expected kanbanRow=0 after move in empty column, got %d", p.kanbanRow)
	}
}

func TestSelectedKanbanWorktree(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusWaiting},
		},
		kanbanCol: 1, // Waiting column
		kanbanRow: 0,
	}

	wt := p.selectedKanbanWorktree()
	if wt == nil {
		t.Fatal("expected non-nil worktree")
	}
	if wt.Name != "wt2" {
		t.Errorf("expected wt2, got %s", wt.Name)
	}
}

func TestSelectedKanbanWorktreeEmptyColumn(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
		},
		kanbanCol: 1, // Waiting column (empty)
		kanbanRow: 0,
	}

	wt := p.selectedKanbanWorktree()
	if wt != nil {
		t.Errorf("expected nil worktree for empty column, got %s", wt.Name)
	}
}

func TestSelectedKanbanWorktreeInvalidColumn(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
		},
		kanbanCol: -1, // Invalid
		kanbanRow: 0,
	}

	wt := p.selectedKanbanWorktree()
	if wt != nil {
		t.Error("expected nil worktree for invalid column")
	}

	p.kanbanCol = 100 // Too large
	wt = p.selectedKanbanWorktree()
	if wt != nil {
		t.Error("expected nil worktree for out-of-range column")
	}
}

func TestMoveKanbanColumnClampsRow(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "wt1", Status: StatusActive},
			{Name: "wt2", Status: StatusActive},
			{Name: "wt3", Status: StatusActive},
			{Name: "wt4", Status: StatusWaiting}, // Only 1 item in Waiting
		},
		kanbanCol: 0, // Active column (3 items)
		kanbanRow: 2, // Last item
	}

	// Move to Waiting column (only 1 item)
	p.moveKanbanColumn(1)

	if p.kanbanCol != 1 {
		t.Errorf("expected kanbanCol=1, got %d", p.kanbanCol)
	}
	// Row should be clamped to 0 since Waiting only has 1 item
	if p.kanbanRow != 0 {
		t.Errorf("expected kanbanRow=0 (clamped), got %d", p.kanbanRow)
	}
}

package trifecta

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/trifectaindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKey_FilterTransitions tests T10: keybindings for filter state transitions
func TestHandleKey_FilterTransitions(t *testing.T) {
	tests := []struct {
		key        string
		wantStatus trifectaindex.WOStatus
	}{
		{"p", trifectaindex.WOStatusPending},
		{"r", trifectaindex.WOStatusRunning},
		{"d", trifectaindex.WOStatusDone},
		{"f", trifectaindex.WOStatusFailed},
		{"a", trifectaindex.WOStatus("")}, // Clear filter
	}

	for _, tt := range tests {
		t.Run("key "+tt.key, func(t *testing.T) {
			p := New()
			p.indexLoaded = true

			p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			assert.Equal(t, tt.wantStatus, p.filterStatus, "filterStatus should match expected for key "+tt.key)
		})
	}
}

// TestHandleKey_CursorMovement tests T10: cursor movement keys
func TestHandleKey_CursorMovement(t *testing.T) {
	p := New()
	p.indexLoaded = true
	p.index = &trifectaindex.WOIndex{
		WorkOrders: []trifectaindex.WorkOrder{
			{ID: "WO-0001", Title: "First", Status: trifectaindex.WOStatusPending},
			{ID: "WO-0002", Title: "Second", Status: trifectaindex.WOStatusPending},
			{ID: "WO-0003", Title: "Third", Status: trifectaindex.WOStatusPending},
		},
	}

	t.Run("down/j moves cursor down", func(t *testing.T) {
		initialCursor := p.cursor
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		assert.Equal(t, initialCursor+1, p.cursor, "j should move cursor down")

		p.Update(tea.KeyMsg{Type: tea.KeyDown})
		assert.Equal(t, initialCursor+2, p.cursor, "down arrow should move cursor down")
	})

	t.Run("up/k moves cursor up", func(t *testing.T) {
		p.cursor = 2
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		assert.Equal(t, 1, p.cursor, "k should move cursor up")

		p.Update(tea.KeyMsg{Type: tea.KeyUp})
		assert.Equal(t, 0, p.cursor, "up arrow should move cursor up")
	})

	t.Run("cursor does not go below zero", func(t *testing.T) {
		p.cursor = 0
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		assert.Equal(t, 0, p.cursor, "cursor should stay at 0")
	})
}

// TestHandleKey_Quit tests T10: quit keybindings
func TestHandleKey_Quit(t *testing.T) {
	p := New()

	t.Run("q quits", func(t *testing.T) {
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
		require.NotNil(t, cmd, "q should return a command")
		assert.Equal(t, tea.Quit(), cmd(), "q should return tea.Quit")
	})

	t.Run("esc quits", func(t *testing.T) {
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEscape})
		require.NotNil(t, cmd, "esc should return a command")
		assert.Equal(t, tea.Quit(), cmd(), "esc should return tea.Quit")
	})
}

// TestHandleKey_Refresh tests T10: refresh keybinding
func TestHandleKey_Refresh(t *testing.T) {
	p := New()
	p.ctx = &plugin.Context{WorkDir: "/tmp"}
	p.indexLoaded = true

	// R (uppercase) triggers refresh - it calls loadIndex()
	// Note: This will fail since /tmp doesn't have a valid index, but we're testing the keybinding
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	// Refresh doesn't return a command, just reloads index
	assert.Nil(t, cmd, "R should not return a command")
}

// TestHandleKey_OpenYAML tests T10: open YAML keybinding
func TestHandleKey_OpenYAML(t *testing.T) {
	p := New()
	p.indexLoaded = true

	// 'o' key triggers handleOpenYAML - returns nil if no work order selected
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	assert.Nil(t, cmd, "o should return nil when no WO selected")
}

// TestLoadIndex_Integration tests T11: integration test for WO index loading
func TestLoadIndex_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	realRepoPath := "/Users/felipe_gonzalez/Developer/agent_h/trifecta_dope"

	t.Run("loads real index successfully", func(t *testing.T) {
		p := New()
		p.ctx = &plugin.Context{WorkDir: realRepoPath}

		err := p.loadIndex()
		require.NoError(t, err, "loadIndex should succeed for real repo")
		assert.True(t, p.indexLoaded, "indexLoaded should be true after load")
		assert.NotNil(t, p.index, "index should not be nil after load")
	})

	t.Run("work orders have required fields", func(t *testing.T) {
		p := New()
		p.ctx = &plugin.Context{WorkDir: realRepoPath}
		err := p.loadIndex()
		require.NoError(t, err)

		for _, wo := range p.index.WorkOrders {
			assert.NotEmpty(t, wo.ID, "WO ID should not be empty")
			assert.NotEmpty(t, wo.Title, "WO Title should not be empty")
			assert.NotEmpty(t, wo.Status, "WO Status should not be empty")
		}
	})

	t.Run("returns error for nonexistent path", func(t *testing.T) {
		p := New()
		p.ctx = &plugin.Context{WorkDir: "/nonexistent/path/12345"}

		err := p.loadIndex()
		assert.Error(t, err, "loadIndex should return error for nonexistent path")
		assert.False(t, p.indexLoaded, "indexLoaded should be false on error")
	})
}

// TestLoadIndex_IndexFilePath tests the index file path construction
func TestLoadIndex_IndexFilePath(t *testing.T) {
	p := New()
	p.ctx = &plugin.Context{WorkDir: "/tmp/test"}

	expected := "/tmp/test/_ctx/index/wo_worktrees.json"
	assert.Equal(t, expected, p.IndexFilePath(), "IndexFilePath should return correct path")
}

// TestFilteredWorkOrders tests the filter functionality
func TestFilteredWorkOrders(t *testing.T) {
	p := New()
	p.index = &trifectaindex.WOIndex{
		WorkOrders: []trifectaindex.WorkOrder{
			{ID: "WO-0001", Title: "First", Status: trifectaindex.WOStatusPending},
			{ID: "WO-0002", Title: "Second", Status: trifectaindex.WOStatusRunning},
			{ID: "WO-0003", Title: "Third", Status: trifectaindex.WOStatusDone},
			{ID: "WO-0004", Title: "Fourth", Status: trifectaindex.WOStatusFailed},
		},
	}
	p.indexLoaded = true

	tests := []struct {
		filter    trifectaindex.WOStatus
		wantCount int
	}{
		{trifectaindex.WOStatusPending, 1},
		{trifectaindex.WOStatusRunning, 1},
		{trifectaindex.WOStatusDone, 1},
		{trifectaindex.WOStatusFailed, 1},
		{trifectaindex.WOStatus(""), 4}, // No filter
	}

	for _, tt := range tests {
		t.Run(string(tt.filter), func(t *testing.T) {
			p.filterStatus = tt.filter
			filtered := p.filteredWorkOrders()
			assert.Len(t, filtered, tt.wantCount, "filtered count should match expected")
		})
	}
}

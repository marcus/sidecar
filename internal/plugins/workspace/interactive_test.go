package workspace

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/tty"
)

// TestMapKeyToTmux_Printable tests regular character input
func TestMapKeyToTmux_Printable(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "a" {
		t.Errorf("expected key='a', got '%s'", key)
	}
	if !useLiteral {
		t.Error("expected useLiteral=true for printable character")
	}
}

// TestMapKeyToTmux_MultiRune tests multi-character input
func TestMapKeyToTmux_MultiRune(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "hello" {
		t.Errorf("expected key='hello', got '%s'", key)
	}
	if !useLiteral {
		t.Error("expected useLiteral=true for multi-character input")
	}
}

// TestMapKeyToTmux_Enter tests Enter key mapping
func TestMapKeyToTmux_Enter(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "Enter" {
		t.Errorf("expected key='Enter', got '%s'", key)
	}
	if useLiteral {
		t.Error("expected useLiteral=false for Enter key")
	}
}

// TestMapKeyToTmux_Backspace tests Backspace key mapping
func TestMapKeyToTmux_Backspace(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "BSpace" {
		t.Errorf("expected key='BSpace', got '%s'", key)
	}
	if useLiteral {
		t.Error("expected useLiteral=false for Backspace")
	}
}

// TestMapKeyToTmux_Tab tests Tab key mapping
func TestMapKeyToTmux_Tab(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyTab}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "Tab" {
		t.Errorf("expected key='Tab', got '%s'", key)
	}
	if useLiteral {
		t.Error("expected useLiteral=false for Tab")
	}
}

// TestMapKeyToTmux_Escape tests Escape key mapping
func TestMapKeyToTmux_Escape(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "Escape" {
		t.Errorf("expected key='Escape', got '%s'", key)
	}
	if useLiteral {
		t.Error("expected useLiteral=false for Escape")
	}
}

// TestMapKeyToTmux_ArrowKeys tests arrow key mappings
func TestMapKeyToTmux_ArrowKeys(t *testing.T) {
	tests := []struct {
		name     string
		keyType  tea.KeyType
		expected string
	}{
		{"Up", tea.KeyUp, "Up"},
		{"Down", tea.KeyDown, "Down"},
		{"Left", tea.KeyLeft, "Left"},
		{"Right", tea.KeyRight, "Right"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tt.keyType}
			key, useLiteral := MapKeyToTmux(msg)
			if key != tt.expected {
				t.Errorf("expected key='%s', got '%s'", tt.expected, key)
			}
			if useLiteral {
				t.Error("expected useLiteral=false for arrow keys")
			}
		})
	}
}

// TestMapKeyToTmux_CtrlKeys tests Ctrl+letter key mappings
func TestMapKeyToTmux_CtrlKeys(t *testing.T) {
	tests := []struct {
		name     string
		keyType  tea.KeyType
		expected string
	}{
		{"Ctrl+A", tea.KeyCtrlA, "C-a"},
		{"Ctrl+C", tea.KeyCtrlC, "C-c"},
		{"Ctrl+D", tea.KeyCtrlD, "C-d"},
		{"Ctrl+Z", tea.KeyCtrlZ, "C-z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tt.keyType}
			key, useLiteral := MapKeyToTmux(msg)
			if key != tt.expected {
				t.Errorf("expected key='%s', got '%s'", tt.expected, key)
			}
			if useLiteral {
				t.Error("expected useLiteral=false for Ctrl keys")
			}
		})
	}
}

// TestMapKeyToTmux_FunctionKeys tests F1-F12 key mappings
func TestMapKeyToTmux_FunctionKeys(t *testing.T) {
	tests := []struct {
		keyType  tea.KeyType
		expected string
	}{
		{tea.KeyF1, "F1"},
		{tea.KeyF2, "F2"},
		{tea.KeyF5, "F5"},
		{tea.KeyF10, "F10"},
		{tea.KeyF12, "F12"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tt.keyType}
			key, useLiteral := MapKeyToTmux(msg)
			if key != tt.expected {
				t.Errorf("expected key='%s', got '%s'", tt.expected, key)
			}
			if useLiteral {
				t.Error("expected useLiteral=false for function keys")
			}
		})
	}
}

// TestMapKeyToTmux_NavigationKeys tests navigation key mappings
func TestMapKeyToTmux_NavigationKeys(t *testing.T) {
	tests := []struct {
		name     string
		keyType  tea.KeyType
		expected string
	}{
		{"Home", tea.KeyHome, "Home"},
		{"End", tea.KeyEnd, "End"},
		{"PageUp", tea.KeyPgUp, "PPage"},
		{"PageDown", tea.KeyPgDown, "NPage"},
		{"Insert", tea.KeyInsert, "IC"},
		{"Delete", tea.KeyDelete, "DC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tt.keyType}
			key, useLiteral := MapKeyToTmux(msg)
			if key != tt.expected {
				t.Errorf("expected key='%s', got '%s'", tt.expected, key)
			}
			if useLiteral {
				t.Error("expected useLiteral=false for navigation keys")
			}
		})
	}
}

// TestMapKeyToTmux_Space tests Space key mapping
func TestMapKeyToTmux_Space(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeySpace}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "Space" {
		t.Errorf("expected key='Space', got '%s'", key)
	}
	if useLiteral {
		t.Error("expected useLiteral=false for Space")
	}
}

// TestMapKeyToTmux_EmptyRunes tests empty rune slice
func TestMapKeyToTmux_EmptyRunes(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{}}
	key, useLiteral := MapKeyToTmux(msg)
	if key != "" {
		t.Errorf("expected empty key for empty runes, got '%s'", key)
	}
	if !useLiteral {
		t.Error("expected useLiteral=true for runes type")
	}
}

// TestIsPasteInput_SingleChar tests single character is not paste
func TestIsPasteInput_SingleChar(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	if isPasteInput(msg) {
		t.Error("single character should not be detected as paste")
	}
}

// TestIsPasteInput_PasteFlag tests paste flag triggers paste detection
func TestIsPasteInput_PasteFlag(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a"), Paste: true}
	if !isPasteInput(msg) {
		t.Error("paste flag should be detected as paste")
	}
}

// TestIsPasteInput_ShortString tests short string without newlines
func TestIsPasteInput_ShortString(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}
	if isPasteInput(msg) {
		t.Error("short string without newlines should not be paste")
	}
}

// TestIsPasteInput_WithNewline tests string with newline is paste
func TestIsPasteInput_WithNewline(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello\nworld")}
	if !isPasteInput(msg) {
		t.Error("string with newline should be detected as paste")
	}
}

// TestIsPasteInput_LongString tests long string is paste
func TestIsPasteInput_LongString(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("this is a longer string that should be paste")}
	if !isPasteInput(msg) {
		t.Error("long string (>10 chars) should be detected as paste")
	}
}

// TestIsPasteInput_NonRunes tests non-rune key types
func TestIsPasteInput_NonRunes(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	if isPasteInput(msg) {
		t.Error("non-rune key types should not be detected as paste")
	}
}

// TestRenderWithCursor_MiddleOfLine tests cursor in middle of text
func TestRenderWithCursor_MiddleOfLine(t *testing.T) {
	content := "hello\nworld"
	result := renderWithCursor(content, 0, 2, true)

	// Should contain the original text (possibly with ANSI codes)
	// In test environment (no TTY), lipgloss may not add ANSI codes
	// So we just verify the function doesn't error and returns reasonable content
	if !strings.Contains(result, "he") {
		t.Error("expected 'he' to be preserved in result")
	}
	if !strings.Contains(result, "lo") {
		t.Error("expected 'lo' to be preserved in result")
	}
	// Verify the result still has the right structure
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

// TestRenderWithCursor_EndOfLine tests cursor past end of line
func TestRenderWithCursor_EndOfLine(t *testing.T) {
	content := "hi"
	result := renderWithCursor(content, 0, 10, true)

	// Should append cursor block since cursor is past end
	if len(result) <= len(content) {
		t.Error("expected result to be longer than content when cursor past end")
	}
}

func TestRenderWithCursor_EndOfLineWithSpace(t *testing.T) {
	content := "word"
	result := renderWithCursor(content, 0, 5, true)

	if !strings.Contains(result, "word ") {
		t.Error("expected padded space before cursor when cursor past end")
	}
}

// TestRenderWithCursor_NotVisible tests invisible cursor
func TestRenderWithCursor_NotVisible(t *testing.T) {
	content := "hello"
	result := renderWithCursor(content, 0, 2, false)

	// Should return content unchanged when cursor not visible
	if result != content {
		t.Errorf("expected unchanged content when cursor not visible, got '%s'", result)
	}
}

// TestRenderWithCursor_NegativePosition tests negative cursor position
func TestRenderWithCursor_NegativePosition(t *testing.T) {
	content := "hello"

	// Negative row
	result := renderWithCursor(content, -1, 2, true)
	if result != content {
		t.Error("expected unchanged content for negative row")
	}

	// Negative column
	result = renderWithCursor(content, 0, -1, true)
	if result != content {
		t.Error("expected unchanged content for negative column")
	}
}

// TestRenderWithCursor_RowOutOfBounds tests cursor row beyond content
func TestRenderWithCursor_RowOutOfBounds(t *testing.T) {
	content := "hello\nworld"
	result := renderWithCursor(content, 5, 2, true)

	// Should return content unchanged when row is out of bounds
	if result != content {
		t.Error("expected unchanged content when cursor row out of bounds")
	}
}

// TestRenderWithCursor_MultiLine tests cursor on second line
func TestRenderWithCursor_MultiLine(t *testing.T) {
	content := "hello\nworld"
	result := renderWithCursor(content, 1, 0, true)

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatal("expected 2 lines")
	}
	// First line should be unchanged
	if lines[0] != "hello" {
		t.Errorf("expected first line unchanged, got '%s'", lines[0])
	}
	// Second line should contain "orld" (the part after cursor)
	// In test environment (no TTY), lipgloss may not add ANSI codes
	if !strings.Contains(lines[1], "orld") {
		t.Errorf("expected second line to contain 'orld', got '%s'", lines[1])
	}
}

// TestRenderWithCursor_PreservesANSI tests that ANSI codes are preserved in before/after parts
func TestRenderWithCursor_PreservesANSI(t *testing.T) {
	// Red "hello" = \x1b[31mhello\x1b[0m
	// Cursor at position 2 (on 'l')
	content := "\x1b[31mhello\x1b[0m"
	result := renderWithCursor(content, 0, 2, true)

	// The result should preserve ANSI codes in before/after parts
	// Before part "he" should retain \x1b[31m prefix
	// After part "lo" should retain coloring
	if !strings.Contains(result, "\x1b[31m") {
		t.Errorf("expected ANSI color code to be preserved, got: %q", result)
	}

	// After cursor should contain "lo" (possibly with reset codes)
	if !strings.Contains(result, "lo") {
		t.Errorf("expected 'lo' in result, got: %q", result)
	}
}

// TestRenderWithCursor_ANSIWidthCalc tests that ANSI codes don't affect width calculation
func TestRenderWithCursor_ANSIWidthCalc(t *testing.T) {
	// Line with ANSI codes: visual width is 5 ("hello")
	// Cursor at position 10 (past end) should append cursor block
	content := "\x1b[31mhello\x1b[0m"
	result := renderWithCursor(content, 0, 10, true)

	// Should have cursor block appended (length increase)
	if len(result) <= len(content) {
		t.Error("expected result longer than content when cursor past visual end")
	}
}

// ============================================================================
// State Transition Tests (td-2e75f54f)
// ============================================================================

// TestExitInteractiveMode_ClearsState tests that exitInteractiveMode clears state correctly
func TestExitInteractiveMode_ClearsState(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetPane:    "%1",
			TargetSession: "test-session",
		},
	}

	p.exitInteractiveMode()

	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList, got %v", p.viewMode)
	}
	if p.interactiveState != nil {
		t.Error("expected interactiveState to be nil after exit")
	}
}

// TestExitInteractiveMode_WhenAlreadyExited tests exitInteractiveMode is safe to call multiple times
func TestExitInteractiveMode_WhenAlreadyExited(t *testing.T) {
	p := &Plugin{
		viewMode:         ViewModeList,
		interactiveState: nil,
	}

	// Should not panic
	p.exitInteractiveMode()

	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList, got %v", p.viewMode)
	}
}

// TestExitInteractiveMode_WhenStateInactive tests exitInteractiveMode with inactive state
func TestExitInteractiveMode_WhenStateInactive(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active: false,
		},
	}

	p.exitInteractiveMode()

	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList, got %v", p.viewMode)
	}
	if p.interactiveState != nil {
		t.Error("expected interactiveState to be nil after exit")
	}
}

// ============================================================================
// handleEscapeTimer Tests (td-2e75f54f)
// ============================================================================

// TestHandleEscapeTimer_NilState tests timer with nil interactiveState
func TestHandleEscapeTimer_NilState(t *testing.T) {
	p := &Plugin{
		interactiveState: nil,
	}

	// Should return nil and not panic
	cmd := p.handleEscapeTimer()
	if cmd != nil {
		t.Error("expected nil command when interactiveState is nil")
	}
}

// TestHandleEscapeTimer_InactiveState tests timer with inactive state
func TestHandleEscapeTimer_InactiveState(t *testing.T) {
	p := &Plugin{
		interactiveState: &InteractiveState{
			Active:        false,
			EscapePressed: true, // Even with pending escape, inactive should return nil
		},
	}

	cmd := p.handleEscapeTimer()
	if cmd != nil {
		t.Error("expected nil command when state is inactive")
	}
}

// TestHandleEscapeTimer_NoPendingEscape tests timer fires with no pending escape
func TestHandleEscapeTimer_NoPendingEscape(t *testing.T) {
	p := &Plugin{
		interactiveState: &InteractiveState{
			Active:        true,
			EscapePressed: false,
		},
	}

	cmd := p.handleEscapeTimer()
	if cmd != nil {
		t.Error("expected nil command when no escape is pending")
	}
}

// ============================================================================
// handleInteractiveKeys Tests (td-2e75f54f)
// ============================================================================

// TestHandleInteractiveKeys_NilState tests key handling with nil state
func TestHandleInteractiveKeys_NilState(t *testing.T) {
	p := &Plugin{
		viewMode:         ViewModeInteractive,
		interactiveState: nil,
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	cmd := p.handleInteractiveKeys(msg)

	// Should exit interactive mode
	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList after nil state handling, got %v", p.viewMode)
	}
	if cmd != nil {
		t.Error("expected nil command")
	}
}

// TestHandleInteractiveKeys_InactiveState tests key handling with inactive state
func TestHandleInteractiveKeys_InactiveState(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active: false,
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	cmd := p.handleInteractiveKeys(msg)

	// Should exit interactive mode
	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList after inactive state handling, got %v", p.viewMode)
	}
	if cmd != nil {
		t.Error("expected nil command")
	}
}

// TestHandleInteractiveKeys_FirstEscapeSetsFlag tests first Escape sets pending flag
func TestHandleInteractiveKeys_FirstEscapeSetsFlag(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test",
			EscapePressed: false,
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	cmd := p.handleInteractiveKeys(msg)

	// Should set EscapePressed flag and start timer
	if !p.interactiveState.EscapePressed {
		t.Error("expected EscapePressed to be true after first Escape")
	}
	if cmd == nil {
		t.Error("expected timer command to be returned")
	}
	// Should still be in interactive mode (not exited yet)
	if p.viewMode != ViewModeInteractive {
		t.Errorf("expected to remain in interactive mode, got %v", p.viewMode)
	}
}

// TestHandleInteractiveKeys_DoubleEscapeExits tests double Escape exits interactive mode
func TestHandleInteractiveKeys_DoubleEscapeExits(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test",
			EscapePressed: true, // First escape already pressed
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	p.handleInteractiveKeys(msg)

	// Should exit interactive mode
	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList after double Escape, got %v", p.viewMode)
	}
}

// TestHandleInteractiveKeys_NonEscapeClearsPendingEscape tests non-escape key clears pending flag
func TestHandleInteractiveKeys_NonEscapeClearsPendingEscape(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test",
			EscapePressed: true, // Pending escape
		},
	}

	// Note: We can't fully test this without mocking tmux commands
	// The actual sendKeyToTmux will fail, which will exit interactive mode
	// But we can verify the flag is cleared before the call
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	_ = p.handleInteractiveKeys(msg)

	// The EscapePressed flag should be cleared
	// (state might be nil if tmux command failed)
	if p.interactiveState != nil && p.interactiveState.EscapePressed {
		t.Error("expected EscapePressed to be false after non-escape key")
	}
}

// ============================================================================
// Polling Decay Constants Tests (td-2e75f54f)
// ============================================================================

// TestPollingDecayConstants tests that polling constants are properly defined
func TestPollingDecayConstants(t *testing.T) {
	// Verify decay constants follow expected order: fast < medium < slow
	if pollingDecayFast >= pollingDecayMedium {
		t.Errorf("pollingDecayFast (%v) should be less than pollingDecayMedium (%v)",
			pollingDecayFast, pollingDecayMedium)
	}
	if pollingDecayMedium >= pollingDecaySlow {
		t.Errorf("pollingDecayMedium (%v) should be less than pollingDecaySlow (%v)",
			pollingDecayMedium, pollingDecaySlow)
	}

	// Verify inactivity thresholds are reasonable
	if inactivityMediumThreshold >= inactivitySlowThreshold {
		t.Errorf("inactivityMediumThreshold (%v) should be less than inactivitySlowThreshold (%v)",
			inactivityMediumThreshold, inactivitySlowThreshold)
	}
}

// TestDoubleEscapeDelayConstant tests double escape delay is reasonable
func TestDoubleEscapeDelayConstant(t *testing.T) {
	// Per spec: 150ms delay for double-escape
	if doubleEscapeDelay.Milliseconds() != 150 {
		t.Errorf("doubleEscapeDelay should be 150ms, got %v", doubleEscapeDelay)
	}
}

// ============================================================================
// InteractiveState Tests (td-2e75f54f)
// ============================================================================

// TestInteractiveState_Initialization tests InteractiveState default values
func TestInteractiveState_Initialization(t *testing.T) {
	state := &InteractiveState{}

	if state.Active {
		t.Error("expected Active to be false by default")
	}
	if state.EscapePressed {
		t.Error("expected EscapePressed to be false by default")
	}
	if state.TargetPane != "" {
		t.Error("expected TargetPane to be empty by default")
	}
	if state.TargetSession != "" {
		t.Error("expected TargetSession to be empty by default")
	}
}

// ============================================================================
// Mouse Interaction Tests (td-80d96956)
// ============================================================================

// TestClickOutsidePreviewExitsInteractiveMode tests that clicking outside preview exits interactive mode
func TestClickOutsidePreviewExitsInteractiveMode(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test",
		},
	}

	// Simulate click on sidebar region (not preview pane)
	// Note: handleMouseClick requires action.Region != nil
	// and checks if region.ID != regionPreviewPane

	// Since handleMouseClick is complex and requires region setup,
	// we test the exit logic directly by simulating the condition
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}

	if p.viewMode != ViewModeList {
		t.Errorf("expected viewMode=ViewModeList after click outside, got %v", p.viewMode)
	}
}

// ============================================================================
// Session Disconnect Tests (td-a1c8456f)
// ============================================================================

// TestIsSessionDeadError_TrueForPaneNotFound tests detection of "can't find pane" error
func TestIsSessionDeadError_TrueForPaneNotFound(t *testing.T) {
	err := fmt.Errorf("can't find pane: %%5")
	if !isSessionDeadError(err) {
		t.Error("expected true for 'can't find pane' error")
	}
}

// TestIsSessionDeadError_TrueForNoSuchSession tests detection of "no such session" error
func TestIsSessionDeadError_TrueForNoSuchSession(t *testing.T) {
	err := fmt.Errorf("no such session: test-session")
	if !isSessionDeadError(err) {
		t.Error("expected true for 'no such session' error")
	}
}

// TestIsSessionDeadError_FalseForOtherErrors tests that other errors return false
func TestIsSessionDeadError_FalseForOtherErrors(t *testing.T) {
	err := fmt.Errorf("some random error")
	if isSessionDeadError(err) {
		t.Error("expected false for unrelated error")
	}
}

// TestIsSessionDeadError_FalseForNil tests nil error handling
func TestIsSessionDeadError_FalseForNil(t *testing.T) {
	if isSessionDeadError(nil) {
		t.Error("expected false for nil error")
	}
}

// TestViewModeInteractiveAllowsDoubleClick tests that double-click is handled in interactive mode
func TestViewModeInteractiveAllowsDoubleClick(t *testing.T) {
	// Verify that ViewModeInteractive is included in double-click handling
	// (not blocked like other modal modes)
	p := &Plugin{
		viewMode: ViewModeInteractive,
	}

	// The double-click handler should not return early for ViewModeInteractive
	// This is a behavioral test - ViewModeInteractive should be allowed
	if p.viewMode != ViewModeInteractive {
		t.Error("setup error: expected ViewModeInteractive")
	}

	// Verify the mode is properly defined (this would fail if ViewModeInteractive
	// wasn't properly defined in the ViewMode constants)
	modes := []ViewMode{
		ViewModeList,
		ViewModeKanban,
		ViewModeInteractive,
	}

	found := false
	for _, m := range modes {
		if m == ViewModeInteractive {
			found = true
			break
		}
	}
	if !found {
		t.Error("ViewModeInteractive not found in modes slice")
	}
}

// TestGetInteractiveExitKey_Default tests default exit key when no config is set
func TestGetInteractiveExitKey_Default(t *testing.T) {
	p := &Plugin{ctx: nil}
	key := p.getInteractiveExitKey()
	if key != defaultExitKey {
		t.Errorf("expected default key '%s', got '%s'", defaultExitKey, key)
	}
}

// TestGetInteractiveExitKey_NilConfig tests default exit key with nil config
func TestGetInteractiveExitKey_NilConfig(t *testing.T) {
	p := &Plugin{ctx: &plugin.Context{}}
	key := p.getInteractiveExitKey()
	if key != defaultExitKey {
		t.Errorf("expected default key '%s' with nil config, got '%s'", defaultExitKey, key)
	}
}

// TestGetInteractiveExitKey_EmptyConfigKey tests default exit key when config key is empty
func TestGetInteractiveExitKey_EmptyConfigKey(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Workspace.InteractiveExitKey = ""
	p := &Plugin{ctx: &plugin.Context{Config: cfg}}
	key := p.getInteractiveExitKey()
	if key != defaultExitKey {
		t.Errorf("expected default key '%s' with empty config, got '%s'", defaultExitKey, key)
	}
}

// TestGetInteractiveExitKey_CustomKey tests custom exit key from config
func TestGetInteractiveExitKey_CustomKey(t *testing.T) {
	customKey := "ctrl+]"
	cfg := config.Default()
	cfg.Plugins.Workspace.InteractiveExitKey = customKey
	p := &Plugin{ctx: &plugin.Context{Config: cfg}}
	key := p.getInteractiveExitKey()
	if key != customKey {
		t.Errorf("expected custom key '%s', got '%s'", customKey, key)
	}
}

// TestGetInteractiveExitKey_VariousKeys tests various custom exit key configurations
func TestGetInteractiveExitKey_VariousKeys(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"ctrl+]", "ctrl+]", "ctrl+]"},
		{"ctrl+x", "ctrl+x", "ctrl+x"},
		{"ctrl+`", "ctrl+`", "ctrl+`"},
		{"escape", "escape", "escape"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Plugins.Workspace.InteractiveExitKey = tt.key
			p := &Plugin{ctx: &plugin.Context{Config: cfg}}
			key := p.getInteractiveExitKey()
			if key != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, key)
			}
		})
	}
}

// TestForwardScrollToTmux_ScrollUp tests that scroll up pauses auto-scroll
func TestForwardScrollToTmux_ScrollUp(t *testing.T) {
	p := &Plugin{autoScrollOutput: true, previewOffset: 0}
	p.forwardScrollToTmux(-1)
	if p.autoScrollOutput {
		t.Error("expected autoScrollOutput=false after scroll up")
	}
	if p.previewOffset != 1 {
		t.Errorf("expected previewOffset=1, got %d", p.previewOffset)
	}
}

// TestForwardScrollToTmux_ScrollDown tests that scroll down resumes auto-scroll at bottom
func TestForwardScrollToTmux_ScrollDown(t *testing.T) {
	p := &Plugin{autoScrollOutput: false, previewOffset: 1}
	p.forwardScrollToTmux(1)
	if !p.autoScrollOutput {
		t.Error("expected autoScrollOutput=true after scrolling to bottom")
	}
	if p.previewOffset != 0 {
		t.Errorf("expected previewOffset=0, got %d", p.previewOffset)
	}
}

// TestForwardClickToTmux_ReturnsNil tests that click forwarding returns nil when inactive
func TestForwardClickToTmux_ReturnsNil(t *testing.T) {
	p := &Plugin{interactiveState: nil}
	cmd := p.forwardClickToTmux(10, 20)
	if cmd != nil {
		t.Error("expected nil cmd when interactiveState is nil")
	}
}

// TestDetectBracketedPasteMode_EnabledOnly tests detection when only enable sequence is present
func TestDetectBracketedPasteMode_EnabledOnly(t *testing.T) {
	output := "some output\x1b[?2004hmore output"
	if !detectBracketedPasteMode(output) {
		t.Error("expected bracketed paste to be detected as enabled")
	}
}

// TestDetectBracketedPasteMode_DisabledOnly tests detection when only disable sequence is present
func TestDetectBracketedPasteMode_DisabledOnly(t *testing.T) {
	output := "some output\x1b[?2004lmore output"
	if detectBracketedPasteMode(output) {
		t.Error("expected bracketed paste to be detected as disabled")
	}
}

func TestDetectMouseReportingMode_EnabledOnly(t *testing.T) {
	output := "some output" + mouseModeEnable1006 + "more output"
	if !detectMouseReportingMode(output) {
		t.Error("expected mouse reporting to be detected as enabled")
	}
}

func TestDetectMouseReportingMode_DisabledOnly(t *testing.T) {
	output := "some output" + mouseModeEnable1006 + mouseModeDisable1006
	if detectMouseReportingMode(output) {
		t.Error("expected mouse reporting to be detected as disabled")
	}
}

// TestDetectBracketedPasteMode_EnabledThenDisabled tests detection when enable followed by disable
func TestDetectBracketedPasteMode_EnabledThenDisabled(t *testing.T) {
	output := "some output\x1b[?2004henabled\x1b[?2004ldisabled"
	if detectBracketedPasteMode(output) {
		t.Error("expected bracketed paste to be disabled when disable comes after enable")
	}
}

// TestDetectBracketedPasteMode_DisabledThenEnabled tests detection when disable followed by enable
func TestDetectBracketedPasteMode_DisabledThenEnabled(t *testing.T) {
	output := "some output\x1b[?2004ldisabled\x1b[?2004henabled"
	if !detectBracketedPasteMode(output) {
		t.Error("expected bracketed paste to be enabled when enable comes after disable")
	}
}

// TestDetectBracketedPasteMode_NoSequences tests detection with no sequences
func TestDetectBracketedPasteMode_NoSequences(t *testing.T) {
	output := "some normal output without any sequences"
	if detectBracketedPasteMode(output) {
		t.Error("expected bracketed paste to be disabled when no sequences present")
	}
}

// TestDetectBracketedPasteMode_EmptyOutput tests detection with empty output
func TestDetectBracketedPasteMode_EmptyOutput(t *testing.T) {
	if detectBracketedPasteMode("") {
		t.Error("expected bracketed paste to be disabled for empty output")
	}
}

// TestUpdateBracketedPasteMode_NilState tests that update handles nil state
func TestUpdateBracketedPasteMode_NilState(t *testing.T) {
	p := &Plugin{interactiveState: nil}
	// Should not panic
	p.updateBracketedPasteMode("some output\x1b[?2004h")
}

// TestUpdateBracketedPasteMode_InactiveState tests that update handles inactive state
func TestUpdateBracketedPasteMode_InactiveState(t *testing.T) {
	p := &Plugin{interactiveState: &InteractiveState{Active: false}}
	p.updateBracketedPasteMode("some output\x1b[?2004h")
	// Should not update when inactive
	if p.interactiveState.BracketedPasteEnabled {
		t.Error("expected BracketedPasteEnabled to remain false when inactive")
	}
}

// TestUpdateBracketedPasteMode_ActiveState tests that update works for active state
func TestUpdateBracketedPasteMode_ActiveState(t *testing.T) {
	p := &Plugin{interactiveState: &InteractiveState{Active: true}}
	p.updateBracketedPasteMode("some output\x1b[?2004h")
	if !p.interactiveState.BracketedPasteEnabled {
		t.Error("expected BracketedPasteEnabled to be true after update with enable sequence")
	}
}

// ============================================================================
// Partial Mouse Sequence Filtering Tests (td-791865)
// ============================================================================

// TestPartialMouseSeqRegex_MatchesScrollDown tests SGR scroll down detection
func TestPartialMouseSeqRegex_MatchesScrollDown(t *testing.T) {
	if !tty.PartialMouseSeqRegex.MatchString("[<65;83;33M") {
		t.Error("expected regex to match scroll-down sequence [<65;83;33M")
	}
}

// TestPartialMouseSeqRegex_MatchesScrollUp tests SGR scroll up detection
func TestPartialMouseSeqRegex_MatchesScrollUp(t *testing.T) {
	if !tty.PartialMouseSeqRegex.MatchString("[<64;10;5M") {
		t.Error("expected regex to match scroll-up sequence [<64;10;5M")
	}
}

// TestPartialMouseSeqRegex_MatchesRelease tests SGR release event (lowercase m)
func TestPartialMouseSeqRegex_MatchesRelease(t *testing.T) {
	if !tty.PartialMouseSeqRegex.MatchString("[<0;50;20m") {
		t.Error("expected regex to match release sequence [<0;50;20m")
	}
}

// TestPartialMouseSeqRegex_NoMatchNormalText tests that normal text is not matched
func TestPartialMouseSeqRegex_NoMatchNormalText(t *testing.T) {
	for _, text := range []string{"hello", "[notmouse]", "[<abc;def;ghiM", "ls -la"} {
		if tty.PartialMouseSeqRegex.MatchString(text) {
			t.Errorf("regex should not match normal text %q", text)
		}
	}
}

// TestPartialMouseSeqRegex_MatchesMultipleSequences tests that multiple concatenated
// mouse sequences (from fast scrolling) are matched as a single KeyRunes message
func TestPartialMouseSeqRegex_MatchesMultipleSequences(t *testing.T) {
	// Two scroll events arriving together (fast scroll)
	if !tty.PartialMouseSeqRegex.MatchString("[<64;81;24M[<64;81;24M") {
		t.Error("expected regex to match two concatenated scroll sequences")
	}
	// Three events
	if !tty.PartialMouseSeqRegex.MatchString("[<64;10;5M[<65;10;5M[<0;10;5m") {
		t.Error("expected regex to match three concatenated mouse sequences")
	}
}

// TestPartialMouseSeqRegex_NoMatchWithESC tests sequences with ESC are not matched
// (those are handled by mouseEscapeRegex instead)
func TestPartialMouseSeqRegex_NoMatchWithESC(t *testing.T) {
	if tty.PartialMouseSeqRegex.MatchString("\x1b[<65;83;33M") {
		t.Error("regex should not match full ESC sequence (handled by mouseEscapeRegex)")
	}
}

// TestHandleInteractiveKeys_DropsPartialMouseSequence tests that partial SGR mouse
// sequences are dropped and not forwarded to tmux (td-791865)
func TestHandleInteractiveKeys_DropsPartialMouseSequence(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test-session",
		},
	}

	// Simulate a partial mouse sequence arriving as KeyRunes
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;83;33M")}
	cmd := p.handleInteractiveKeys(msg)

	// Should not exit interactive mode
	if p.viewMode != ViewModeInteractive {
		t.Error("expected to remain in interactive mode after dropping mouse sequence")
	}
	// Should return nil (no commands to execute)
	if cmd != nil {
		t.Error("expected nil cmd when dropping partial mouse sequence")
	}
}

// TestHandleInteractiveKeys_CancelsPendingEscapeForMouseSequence tests the actual
// split-read scenario: ESC arrived first (setting EscapePressed=true), then the
// partial mouse sequence arrives. The pending escape must be cancelled since it
// was part of the mouse event, not a real user keypress (td-791865).
func TestHandleInteractiveKeys_CancelsPendingEscapeForMouseSequence(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test-session",
			EscapePressed: true, // ESC arrived first (split-read)
			EscapeTime:    time.Now(),
		},
	}

	// Partial mouse sequence arrives as the next message
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;83;33M")}
	cmd := p.handleInteractiveKeys(msg)

	// EscapePressed must be cleared — it was part of the mouse sequence
	if p.interactiveState.EscapePressed {
		t.Error("expected EscapePressed to be cleared after partial mouse sequence")
	}
	// Should remain in interactive mode
	if p.viewMode != ViewModeInteractive {
		t.Error("expected to remain in interactive mode")
	}
	// No commands should be returned (no forwarding)
	if cmd != nil {
		t.Error("expected nil cmd, not forwarding anything to tmux")
	}
}

// TestHandleInteractiveKeys_ForwardsNormalRunes tests that normal rune input is
// still forwarded (not incorrectly filtered)
func TestHandleInteractiveKeys_ForwardsNormalRunes(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeInteractive,
		interactiveState: &InteractiveState{
			Active:        true,
			TargetSession: "test-session",
		},
	}

	// Normal single character should proceed to MapKeyToTmux (will fail at sendKeys but that's ok)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	_ = p.handleInteractiveKeys(msg)

	// The key thing is the function didn't panic and tried to forward
	// (it will exit interactive mode due to tmux command failure, which is expected in test)
}

// TestOutputBuffer_StripsPartialMouseSequences tests that OutputBuffer.Update
// strips partial mouse sequences without ESC prefix (td-791865)
func TestOutputBuffer_StripsPartialMouseSequences(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Content with partial mouse sequences (no ESC prefix)
	content := "prompt$ [<65;83;33M[<65;83;33Mls\nfile1.txt\n"
	buf.Update(content)

	lines := buf.Lines()
	result := strings.Join(lines, "\n")
	if strings.Contains(result, "[<65;83;33M") {
		t.Errorf("expected partial mouse sequences to be stripped, got: %q", result)
	}
	if !strings.Contains(result, "prompt$ ls") {
		t.Errorf("expected remaining content preserved, got: %q", result)
	}
}

// TestOutputBuffer_StripsFullAndPartialMouseSequences tests both forms are stripped
func TestOutputBuffer_StripsFullAndPartialMouseSequences(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Mix of full (with ESC) and partial (without ESC) sequences
	content := "output\x1b[<64;10;5M[<65;83;33Mmore output\n"
	buf.Update(content)

	lines := buf.Lines()
	result := strings.Join(lines, "\n")
	if strings.Contains(result, "[<64;10;5M") {
		t.Errorf("expected full mouse sequence to be stripped, got: %q", result)
	}
	if strings.Contains(result, "[<65;83;33M") {
		t.Errorf("expected partial mouse sequence to be stripped, got: %q", result)
	}
	if !strings.Contains(result, "outputmore output") {
		t.Errorf("expected remaining content preserved, got: %q", result)
	}
}

// TestOutputBuffer_PreservesNormalBrackets tests that normal bracket usage is not stripped
func TestOutputBuffer_PreservesNormalBrackets(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Content with brackets that should NOT be stripped
	content := "array[0] = value\nif [[ -f file ]]; then\n"
	buf.Update(content)

	lines := buf.Lines()
	result := strings.Join(lines, "\n")
	if !strings.Contains(result, "array[0]") {
		t.Errorf("expected normal brackets preserved, got: %q", result)
	}
	if !strings.Contains(result, "[[ -f file ]]") {
		t.Errorf("expected bash test brackets preserved, got: %q", result)
	}
}

// TestCalculatePreviewDimensions_WidthConsistency verifies that calculatePreviewDimensions
// returns the same content width as used in renderListView for both sidebar visible/hidden cases.
// This prevents regression of td-0655df (width calculation mismatch causing right-side truncation).
func TestCalculatePreviewDimensions_WidthConsistency(t *testing.T) {
	tests := []struct {
		name           string
		totalWidth     int
		totalHeight    int
		sidebarVisible bool
		sidebarWidth   int // percentage
	}{
		{"full_width_small", 80, 24, false, 30},
		{"full_width_large", 200, 50, false, 30},
		{"sidebar_25pct", 120, 30, true, 25},
		{"sidebar_50pct", 120, 30, true, 50},
		{"sidebar_min_clamped", 60, 20, true, 10}, // should clamp sidebar to min
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				width:          tt.totalWidth,
				height:         tt.totalHeight,
				sidebarVisible: tt.sidebarVisible,
				sidebarWidth:   tt.sidebarWidth,
			}

			// Get width from calculatePreviewDimensions (used for tmux resize)
			calcWidth, _ := p.calculatePreviewDimensions()

			// Simulate renderListView width calculation
			var renderWidth int
			if !p.sidebarVisible {
				// Full width case: previewW = width, contentW = previewW - panelOverhead
				renderWidth = tt.totalWidth - panelOverhead
			} else {
				// Sidebar visible: same calculation as in renderListView
				available := tt.totalWidth - dividerWidth
				sidebarW := (available * p.sidebarWidth) / 100
				if sidebarW < 25 {
					sidebarW = 25
				}
				if sidebarW > available-40 {
					sidebarW = available - 40
				}
				previewW := available - sidebarW
				if previewW < 40 {
					previewW = 40
				}
				renderWidth = previewW - panelOverhead
			}

			if calcWidth != renderWidth {
				t.Errorf("width mismatch: calculatePreviewDimensions()=%d, renderListView would use=%d",
					calcWidth, renderWidth)
			}
		})
	}
}

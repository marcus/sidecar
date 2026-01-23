package worktree

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/features"
)

// Interactive mode constants
const (
	// doubleEscapeDelay is the max time between Escape presses for double-escape exit.
	// Single Escape is delayed by this amount to detect double-press.
	doubleEscapeDelay = 150 * time.Millisecond

	// pollingDecayFast is the polling interval during active typing.
	pollingDecayFast = 50 * time.Millisecond

	// pollingDecayMedium is the polling interval after brief inactivity.
	pollingDecayMedium = 200 * time.Millisecond

	// pollingDecaySlow is the polling interval after extended inactivity.
	pollingDecaySlow = 500 * time.Millisecond

	// keystrokeDebounce delays polling after keystrokes to batch rapid typing (td-8a0978).
	// Allows typing bursts to coalesce into fewer polls, reducing CPU usage.
	keystrokeDebounce = 20 * time.Millisecond

	// inactivityMediumThreshold triggers medium polling.
	inactivityMediumThreshold = 2 * time.Second

	// inactivitySlowThreshold triggers slow polling.
	inactivitySlowThreshold = 10 * time.Second

	// defaultExitKey is the default keybinding to exit interactive mode.
	defaultExitKey = "ctrl+\\"

	// defaultAttachKey is the default keybinding to attach from interactive mode (td-fd68d1).
	defaultAttachKey = "ctrl+]"
)

// escapeTimerMsg is sent when the escape delay timer fires.
// If pendingEscape is still true, we forward the single Escape to tmux.
type escapeTimerMsg struct{}

// InteractiveSessionDeadMsg indicates the tmux session has ended.
// Sent when send-keys or capture fails with a session/pane not found error.
type InteractiveSessionDeadMsg struct{}

// getInteractiveExitKey returns the configured exit keybinding for interactive mode.
// Falls back to defaultExitKey ("ctrl+\") if not configured.
func (p *Plugin) getInteractiveExitKey() string {
	if p.ctx != nil && p.ctx.Config != nil {
		if key := p.ctx.Config.Plugins.Worktree.InteractiveExitKey; key != "" {
			return key
		}
	}
	return defaultExitKey
}

// getInteractiveAttachKey returns the configured attach keybinding for interactive mode (td-fd68d1).
// Falls back to defaultAttachKey ("ctrl+]") if not configured.
func (p *Plugin) getInteractiveAttachKey() string {
	if p.ctx != nil && p.ctx.Config != nil {
		if key := p.ctx.Config.Plugins.Worktree.InteractiveAttachKey; key != "" {
			return key
		}
	}
	return defaultAttachKey
}

// isSessionDeadError checks if an error indicates the tmux session/pane is gone.
func isSessionDeadError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "can't find pane") ||
		strings.Contains(errStr, "no such session") ||
		strings.Contains(errStr, "session not found") ||
		strings.Contains(errStr, "pane not found")
}

// MapKeyToTmux translates a Bubble Tea key message to a tmux send-keys argument.
// Returns the tmux key name and whether to use literal mode (-l).
// For modified keys and special keys, returns the tmux key name.
// For literal characters, returns the character with useLiteral=true.
func MapKeyToTmux(msg tea.KeyMsg) (key string, useLiteral bool) {
	switch msg.String() {
	case "shift+up":
		return "\x1b[1;2A", true
	case "shift+down":
		return "\x1b[1;2B", true
	case "shift+right":
		return "\x1b[1;2C", true
	case "shift+left":
		return "\x1b[1;2D", true
	case "ctrl+up":
		return "\x1b[1;5A", true
	case "ctrl+down":
		return "\x1b[1;5B", true
	case "ctrl+right":
		return "\x1b[1;5C", true
	case "ctrl+left":
		return "\x1b[1;5D", true
	case "alt+up":
		return "\x1b[1;3A", true
	case "alt+down":
		return "\x1b[1;3B", true
	case "alt+right":
		return "\x1b[1;3C", true
	case "alt+left":
		return "\x1b[1;3D", true
	case "shift+tab":
		return "\x1b[Z", true
	}

	// Handle special keys
	// Note: KeyCtrlI == KeyTab and KeyCtrlM == KeyEnter in BubbleTea,
	// so we handle Tab and Enter first, then other Ctrl keys.
	switch msg.Type {
	case tea.KeyEnter: // Also KeyCtrlM
		return "Enter", false
	case tea.KeyBackspace:
		return "BSpace", false
	case tea.KeyDelete:
		return "DC", false
	case tea.KeyTab: // Also KeyCtrlI
		return "Tab", false
	case tea.KeySpace:
		return "Space", false
	case tea.KeyUp:
		return "Up", false
	case tea.KeyDown:
		return "Down", false
	case tea.KeyLeft:
		return "Left", false
	case tea.KeyRight:
		return "Right", false
	case tea.KeyHome:
		return "Home", false
	case tea.KeyEnd:
		return "End", false
	case tea.KeyPgUp:
		return "PPage", false
	case tea.KeyPgDown:
		return "NPage", false
	case tea.KeyInsert:
		return "IC", false
	case tea.KeyEscape:
		return "Escape", false

	// Ctrl combinations (excluding KeyCtrlI/Tab and KeyCtrlM/Enter handled above)
	case tea.KeyCtrlA:
		return "C-a", false
	case tea.KeyCtrlB:
		return "C-b", false
	case tea.KeyCtrlC:
		return "C-c", false
	case tea.KeyCtrlD:
		return "C-d", false
	case tea.KeyCtrlE:
		return "C-e", false
	case tea.KeyCtrlF:
		return "C-f", false
	case tea.KeyCtrlG:
		return "C-g", false
	case tea.KeyCtrlH:
		return "C-h", false
	case tea.KeyCtrlJ:
		return "C-j", false
	case tea.KeyCtrlK:
		return "C-k", false
	case tea.KeyCtrlL:
		return "C-l", false
	case tea.KeyCtrlN:
		return "C-n", false
	case tea.KeyCtrlO:
		return "C-o", false
	case tea.KeyCtrlP:
		return "C-p", false
	case tea.KeyCtrlQ:
		return "C-q", false
	case tea.KeyCtrlR:
		return "C-r", false
	case tea.KeyCtrlS:
		return "C-s", false
	case tea.KeyCtrlT:
		return "C-t", false
	case tea.KeyCtrlU:
		return "C-u", false
	case tea.KeyCtrlV:
		return "C-v", false
	case tea.KeyCtrlW:
		return "C-w", false
	case tea.KeyCtrlX:
		return "C-x", false
	case tea.KeyCtrlY:
		return "C-y", false
	case tea.KeyCtrlZ:
		return "C-z", false

	// Function keys (F1-F12)
	case tea.KeyF1:
		return "F1", false
	case tea.KeyF2:
		return "F2", false
	case tea.KeyF3:
		return "F3", false
	case tea.KeyF4:
		return "F4", false
	case tea.KeyF5:
		return "F5", false
	case tea.KeyF6:
		return "F6", false
	case tea.KeyF7:
		return "F7", false
	case tea.KeyF8:
		return "F8", false
	case tea.KeyF9:
		return "F9", false
	case tea.KeyF10:
		return "F10", false
	case tea.KeyF11:
		return "F11", false
	case tea.KeyF12:
		return "F12", false

	case tea.KeyRunes:
		// Regular character input
		if len(msg.Runes) > 0 {
			return string(msg.Runes), true
		}
		return "", true
	}

	// Fallback for any unhandled key types
	if msg.String() != "" {
		return msg.String(), true
	}
	return "", true
}

// sendKeyToTmux sends a key to a tmux pane using send-keys.
// Uses the tmux key name syntax (e.g., "Enter", "C-c", "Up").
func sendKeyToTmux(sessionName, key string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, key)
	return cmd.Run()
}

// sendLiteralToTmux sends literal text to a tmux pane using send-keys -l.
// This prevents tmux from interpreting special key names.
func sendLiteralToTmux(sessionName, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-l", "-t", sessionName, text)
	return cmd.Run()
}

// sendPasteToTmux pastes multi-line text via tmux buffer.
// Uses load-buffer + paste-buffer which works regardless of app paste mode state.
func sendPasteToTmux(sessionName, text string) error {
	// Load text into tmux default buffer via stdin
	loadCmd := exec.Command("tmux", "load-buffer", "-")
	loadCmd.Stdin = strings.NewReader(text)
	if err := loadCmd.Run(); err != nil {
		return err
	}

	// Paste buffer into target pane
	pasteCmd := exec.Command("tmux", "paste-buffer", "-t", sessionName)
	return pasteCmd.Run()
}

// Bracketed paste escape sequences
const (
	bracketedPasteEnable  = "\x1b[?2004h" // ESC[?2004h - app enables bracketed paste
	bracketedPasteDisable = "\x1b[?2004l" // ESC[?2004l - app disables bracketed paste
	bracketedPasteStart   = "\x1b[200~"   // ESC[200~ - start of pasted content
	bracketedPasteEnd     = "\x1b[201~"   // ESC[201~ - end of pasted content
	mouseModeEnable1000   = "\x1b[?1000h"
	mouseModeEnable1002   = "\x1b[?1002h"
	mouseModeEnable1003   = "\x1b[?1003h"
	mouseModeEnable1006   = "\x1b[?1006h"
	mouseModeEnable1015   = "\x1b[?1015h"
	mouseModeDisable1000  = "\x1b[?1000l"
	mouseModeDisable1002  = "\x1b[?1002l"
	mouseModeDisable1003  = "\x1b[?1003l"
	mouseModeDisable1006  = "\x1b[?1006l"
	mouseModeDisable1015  = "\x1b[?1015l"
)

// detectBracketedPasteMode checks captured output to determine if the app has
// enabled bracketed paste mode. Looks for the most recent occurrence of either
// the enable (ESC[?2004h) or disable (ESC[?2004l) sequence.
func detectBracketedPasteMode(output string) bool {
	enableIdx := strings.LastIndex(output, bracketedPasteEnable)
	disableIdx := strings.LastIndex(output, bracketedPasteDisable)
	// If enable was found more recently than disable, bracketed paste is enabled
	return enableIdx > disableIdx
}

// sendBracketedPasteToTmux sends text wrapped in bracketed paste sequences.
// Used when the target app has enabled bracketed paste mode.
func sendBracketedPasteToTmux(sessionName, text string) error {
	// Send bracketed paste start sequence
	if err := sendLiteralToTmux(sessionName, bracketedPasteStart); err != nil {
		return err
	}

	// Send the actual text
	if err := sendLiteralToTmux(sessionName, text); err != nil {
		return err
	}

	// Send bracketed paste end sequence
	return sendLiteralToTmux(sessionName, bracketedPasteEnd)
}

func detectMouseReportingMode(output string) bool {
	enableSeqs := []string{
		mouseModeEnable1000,
		mouseModeEnable1002,
		mouseModeEnable1003,
		mouseModeEnable1006,
		mouseModeEnable1015,
	}
	disableSeqs := []string{
		mouseModeDisable1000,
		mouseModeDisable1002,
		mouseModeDisable1003,
		mouseModeDisable1006,
		mouseModeDisable1015,
	}

	latestEnable := -1
	for _, seq := range enableSeqs {
		if idx := strings.LastIndex(output, seq); idx > latestEnable {
			latestEnable = idx
		}
	}

	latestDisable := -1
	for _, seq := range disableSeqs {
		if idx := strings.LastIndex(output, seq); idx > latestDisable {
			latestDisable = idx
		}
	}

	return latestEnable > latestDisable
}

func (p *Plugin) updateMouseReportingMode(output string) {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return
	}
	p.interactiveState.MouseReportingEnabled = detectMouseReportingMode(output)
}

// updateBracketedPasteMode updates the BracketedPasteEnabled state from captured output.
// Should be called whenever new output is received for the interactive pane.
func (p *Plugin) updateBracketedPasteMode(output string) {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return
	}
	p.interactiveState.BracketedPasteEnabled = detectBracketedPasteMode(output)
}

// isPasteInput detects if the input is a paste operation.
// Returns true if the input contains newlines or is longer than a typical typed sequence.
func isPasteInput(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes || len(msg.Runes) <= 1 {
		return false
	}
	text := string(msg.Runes)
	// Treat as paste if contains newline or is suspiciously long for typing
	return strings.Contains(text, "\n") || len(msg.Runes) > 10
}

// enterInteractiveMode enters interactive mode for the current selection.
// Returns a tea.Cmd if mode entry succeeded, nil otherwise.
// Requires tmux_interactive_input feature flag to be enabled.
func (p *Plugin) enterInteractiveMode() tea.Cmd {
	// Check feature flag
	if !features.IsEnabled(features.TmuxInteractiveInput.Name) {
		return nil
	}

	// Determine target based on current selection
	var sessionName, paneID string

	if p.shellSelected {
		// Shell session
		if p.selectedShellIdx < 0 || p.selectedShellIdx >= len(p.shells) {
			return nil
		}
		shell := p.shells[p.selectedShellIdx]
		if shell.Agent == nil {
			return nil
		}
		sessionName = shell.TmuxName
		paneID = shell.Agent.TmuxPane
	} else {
		// Worktree
		wt := p.selectedWorktree()
		if wt == nil || wt.Agent == nil {
			return nil
		}
		sessionName = wt.Agent.TmuxSession
		paneID = wt.Agent.TmuxPane
	}

	// Resize tmux pane to match preview width (td-c7dd1e)
	// This ensures terminal content fits the visible area without being cut off
	target := paneID
	if target == "" {
		target = sessionName // Fall back to session name if pane ID not available
	}
	if target != "" {
		// Reset scroll offsets so cursor alignment matches the visible pane (td-43d37b)
		p.previewOffset = 0
		p.previewHorizOffset = 0
		p.autoScrollOutput = true
		previewWidth, previewHeight := p.calculatePreviewDimensions()
		p.resizeTmuxPane(target, previewWidth, previewHeight)
	}

	// Initialize interactive state
	p.interactiveState = &InteractiveState{
		Active:        true,
		TargetPane:    paneID,
		TargetSession: sessionName,
		LastKeyTime:   time.Now(),
		CursorVisible: true, // Assume visible until we get first cursor query result
	}

	p.viewMode = ViewModeInteractive

	// Invalidate existing poll timers to prevent duplicate poll chains (td-97327e).
	// Without this, entering interactive mode creates a second poll chain that runs
	// in parallel with the existing one, causing 200% CPU usage.
	if p.shellSelected {
		p.shellPollGeneration[sessionName]++
	} else {
		if wt := p.selectedWorktree(); wt != nil {
			p.pollGeneration[wt.Name]++
		}
	}

	// Trigger immediate poll for fresh content (cursor position is captured atomically with output)
	return p.pollInteractivePane()
}

// calculatePreviewDimensions returns the content width and height for the preview pane.
// Used to resize tmux panes to match the visible area.
// IMPORTANT: This must stay in sync with renderListView() width calculations.
func (p *Plugin) calculatePreviewDimensions() (width, height int) {
	if p.width <= 0 || p.height <= 0 {
		return 80, 24 // Safe defaults
	}

	// Calculate preview pane width based on sidebar visibility
	// Uses panelOverhead constant to ensure consistency with render path
	if !p.sidebarVisible {
		// Full width minus panel overhead (borders + padding)
		width = p.width - panelOverhead
	} else {
		// Account for sidebar and divider (same calculation as renderListView)
		available := p.width - dividerWidth
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
		// Subtract panel overhead for content width
		width = previewW - panelOverhead
	}

	// Calculate height: total height minus borders (2) and UI elements
	// - panelBorderWidth for top/bottom panel borders
	// - 1 for hint line
	// - 2 for tabs header (worktrees only)
	paneHeight := p.height - panelBorderWidth
	if p.shellSelected {
		// Shell: no tabs, just hint
		height = paneHeight - 1
	} else {
		// Worktree: tabs header + hint
		height = paneHeight - 3
	}

	if width < 20 {
		width = 20
	}
	if height < 5 {
		height = 5
	}

	return width, height
}

// resizeInteractivePaneCmd resizes the active interactive tmux pane to match the UI.
// This is used after window/sidebar resizing to keep cursor position aligned.
func (p *Plugin) resizeInteractivePaneCmd() tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	target := p.interactiveState.TargetPane
	if target == "" {
		target = p.interactiveState.TargetSession
	}
	if target == "" {
		return nil
	}

	previewWidth, previewHeight := p.calculatePreviewDimensions()
	return func() tea.Msg {
		p.resizeTmuxPane(target, previewWidth, previewHeight)
		if actualWidth, actualHeight, ok := queryPaneSize(target); ok {
			if actualWidth != previewWidth || actualHeight != previewHeight {
				p.resizeTmuxPane(target, previewWidth, previewHeight)
			}
		}
		return nil
	}
}

func (p *Plugin) maybeResizeInteractivePane(paneWidth, paneHeight int) tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}
	if paneWidth <= 0 || paneHeight <= 0 {
		return nil
	}

	previewWidth, previewHeight := p.calculatePreviewDimensions()
	if paneWidth == previewWidth && paneHeight == previewHeight {
		return nil
	}

	if !p.interactiveState.LastResizeAt.IsZero() && time.Since(p.interactiveState.LastResizeAt) < 500*time.Millisecond {
		return nil
	}
	p.interactiveState.LastResizeAt = time.Now()
	return p.resizeInteractivePaneCmd()
}

// resizeTmuxPane resizes a tmux pane to the specified dimensions.
func (p *Plugin) resizeTmuxPane(paneID string, width, height int) {
	if width <= 0 && height <= 0 {
		return
	}

	args := []string{"resize-pane", "-t", paneID}
	if width > 0 {
		args = append(args, "-x", strconv.Itoa(width))
	}
	if height > 0 {
		args = append(args, "-y", strconv.Itoa(height))
	}
	cmd := exec.Command("tmux", args...)
	_ = cmd.Run() // Ignore errors - pane may not support resize
}

func queryPaneSize(target string) (width, height int, ok bool) {
	if target == "" {
		return 0, 0, false
	}

	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_width},#{pane_height}")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, false
	}

	parts := strings.Split(strings.TrimSpace(string(output)), ",")
	if len(parts) < 2 {
		return 0, 0, false
	}

	width, _ = strconv.Atoi(parts[0])
	height, _ = strconv.Atoi(parts[1])
	return width, height, true
}

// resizeSelectedPaneCmd resizes the currently selected tmux pane to match the
// preview dimensions. Called in non-interactive mode so that capture-pane output
// is already wrapped at the correct width (avoiding clipping when entering
// interactive mode or width mismatches in preview).
func (p *Plugin) resizeSelectedPaneCmd() tea.Cmd {
	if p.width <= 0 || p.height <= 0 {
		return nil
	}

	// Throttle to max once every 500ms
	if !p.lastPreviewResizeAt.IsZero() && time.Since(p.lastPreviewResizeAt) < 500*time.Millisecond {
		return nil
	}

	var target string
	if p.shellSelected {
		if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil {
			target = shell.Agent.TmuxPane
			if target == "" {
				target = shell.TmuxName
			}
		}
	} else {
		if wt := p.selectedWorktree(); wt != nil && wt.Agent != nil {
			target = wt.Agent.TmuxPane
			if target == "" {
				target = wt.Agent.TmuxSession
			}
		}
	}
	if target == "" {
		return nil
	}

	previewWidth, previewHeight := p.calculatePreviewDimensions()
	p.lastPreviewResizeAt = time.Now()
	return func() tea.Msg {
		p.resizeTmuxPane(target, previewWidth, previewHeight)
		return nil
	}
}

// exitInteractiveMode exits interactive mode and returns to list view.
func (p *Plugin) exitInteractiveMode() {
	if p.interactiveState != nil {
		p.interactiveState.Active = false
	}
	p.interactiveState = nil
	p.viewMode = ViewModeList
}

// handleInteractiveKeys processes key input in interactive mode.
// Returns a tea.Cmd for any async operations needed.
func (p *Plugin) handleInteractiveKeys(msg tea.KeyMsg) tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		p.exitInteractiveMode()
		return nil
	}

	// Check for exit keys

	// Primary exit: Configurable key (default: Ctrl+\)
	if msg.String() == p.getInteractiveExitKey() {
		p.exitInteractiveMode()
		return nil
	}

	// Attach shortcut: exit interactive and attach to full session (td-fd68d1)
	if msg.String() == p.getInteractiveAttachKey() {
		p.exitInteractiveMode()
		// Attach to the appropriate session
		if p.shellSelected {
			if idx := p.selectedShellIdx; idx >= 0 && idx < len(p.shells) {
				return p.ensureShellAndAttachByIndex(idx)
			}
		} else {
			if wt := p.selectedWorktree(); wt != nil && wt.Agent != nil {
				p.attachedSession = wt.Name
				return p.AttachToSession(wt)
			}
		}
		return nil
	}

	// Secondary exit: Double-Escape with 150ms delay
	// Per spec: first Escape is delayed to detect double-press
	if msg.Type == tea.KeyEscape {
		if p.interactiveState.EscapePressed {
			// Second Escape within window: exit interactive mode
			p.interactiveState.EscapePressed = false
			p.interactiveState.EscapeTimerPending = false // Cancel pending timer
			p.exitInteractiveMode()
			return nil
		}
		// First Escape: mark pending and start delay timer
		// Do NOT forward to tmux yet - wait for timer or next key
		p.interactiveState.EscapePressed = true
		p.interactiveState.EscapeTime = time.Now()
		// Timer leak prevention (td-83dc22): only schedule timer if one isn't already pending
		if !p.interactiveState.EscapeTimerPending {
			p.interactiveState.EscapeTimerPending = true
			return tea.Tick(doubleEscapeDelay, func(t time.Time) tea.Msg {
				return escapeTimerMsg{}
			})
		}
		return nil
	}

	// Non-escape key: check if we have a pending Escape to forward first
	var cmds []tea.Cmd
	if p.interactiveState.EscapePressed {
		p.interactiveState.EscapePressed = false
		// Timer leak prevention (td-83dc22): pending timer will be ignored when it fires
		// since EscapePressed is now false (no need to cancel, it's harmless)
		// Forward the pending Escape before this key
		if err := sendKeyToTmux(p.interactiveState.TargetSession, "Escape"); err != nil {
			p.exitInteractiveMode()
			if isSessionDeadError(err) {
				return func() tea.Msg { return InteractiveSessionDeadMsg{} }
			}
			return nil
		}
	}

	// Update last key time for polling decay
	p.interactiveState.LastKeyTime = time.Now()

	sessionName := p.interactiveState.TargetSession

	// Check for paste (multi-character input with newlines or long text)
	if isPasteInput(msg) {
		text := string(msg.Runes)
		var err error
		// Use bracketed paste if app has it enabled (td-79ab6163)
		if p.interactiveState.BracketedPasteEnabled {
			err = sendBracketedPasteToTmux(sessionName, text)
		} else {
			err = sendPasteToTmux(sessionName, text)
		}
		if err != nil {
			p.exitInteractiveMode()
			if isSessionDeadError(err) {
				return func() tea.Msg { return InteractiveSessionDeadMsg{} }
			}
			return nil
		}
		cmds = append(cmds, p.pollInteractivePane())
		return tea.Batch(cmds...)
	}

	// Map key to tmux format and send
	key, useLiteral := MapKeyToTmux(msg)
	if key == "" {
		return tea.Batch(cmds...)
	}

	var err error
	if useLiteral {
		err = sendLiteralToTmux(sessionName, key)
	} else {
		err = sendKeyToTmux(sessionName, key)
	}

	if err != nil {
		// Session may have died - exit interactive mode
		p.exitInteractiveMode()
		if isSessionDeadError(err) {
			return func() tea.Msg { return InteractiveSessionDeadMsg{} }
		}
		return nil
	}

	// Schedule debounced poll to batch rapid keystrokes (td-8a0978)
	cmds = append(cmds, p.scheduleDebouncedPoll(keystrokeDebounce))
	return tea.Batch(cmds...)
}

// handleEscapeTimer processes the escape delay timer firing.
// If a single Escape is still pending (no second Escape arrived), forward it to tmux.
func (p *Plugin) handleEscapeTimer() tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	// Timer leak prevention (td-83dc22): clear the pending flag since timer has fired
	p.interactiveState.EscapeTimerPending = false

	if !p.interactiveState.EscapePressed {
		// Escape was already handled (double-press or another key arrived)
		return nil
	}

	// Timer fired with pending Escape: forward the single Escape to tmux
	p.interactiveState.EscapePressed = false
	if err := sendKeyToTmux(p.interactiveState.TargetSession, "Escape"); err != nil {
		p.exitInteractiveMode()
		if isSessionDeadError(err) {
			return func() tea.Msg { return InteractiveSessionDeadMsg{} }
		}
		return nil
	}

	// Update last key time and poll immediately for better responsiveness (td-babfd9)
	p.interactiveState.LastKeyTime = time.Now()
	return p.pollInteractivePaneImmediate()
}

// forwardScrollToTmux sends scroll wheel events to the tmux pane.
// Returns a tea.Cmd for any async operations needed.
// Scroll up (delta < 0) sends PPage (Page Up), scroll down (delta > 0) sends NPage (Page Down).
// For smoother scrolling, smaller scroll increments use arrow keys.
func (p *Plugin) forwardScrollToTmux(delta int, x, y int) tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	sessionName := p.interactiveState.TargetSession

	if p.interactiveState.MouseReportingEnabled {
		col, row, ok := p.interactiveMouseCoords(x, y)
		if !ok {
			return nil
		}
		button := 64 // wheel up
		if delta > 0 {
			button = 65 // wheel down
		}
		if err := sendSGRMouse(sessionName, button, col, row, false); err != nil {
			p.exitInteractiveMode()
			if isSessionDeadError(err) {
				return func() tea.Msg { return InteractiveSessionDeadMsg{} }
			}
			return nil
		}
	} else {
		// Fallback to key-based scrolling when mouse reporting is disabled.
		var key string
		var count int
		if delta < 0 {
			count = -delta
			if count >= 3 {
				key = "PPage"
				count = 1
			} else {
				key = "Up"
			}
		} else {
			count = delta
			if count >= 3 {
				key = "NPage"
				count = 1
			} else {
				key = "Down"
			}
		}

		for i := 0; i < count; i++ {
			if err := sendKeyToTmux(sessionName, key); err != nil {
				p.exitInteractiveMode()
				if isSessionDeadError(err) {
					return func() tea.Msg { return InteractiveSessionDeadMsg{} }
				}
				return nil
			}
		}
	}

	// Update last key time and poll immediately for better responsiveness (td-babfd9)
	p.interactiveState.LastKeyTime = time.Now()

	return p.pollInteractivePaneImmediate()
}

// forwardClickToTmux sends a mouse click to the tmux pane.
// Currently a no-op as full mouse support requires knowing the terminal's mouse mode.
// This is provided for future extension.
func (p *Plugin) forwardClickToTmux(x, y int) tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}
	if !p.interactiveState.MouseReportingEnabled {
		return nil
	}
	sessionName := p.interactiveState.TargetSession
	col, row, ok := p.interactiveMouseCoords(x, y)
	if !ok {
		return nil
	}

	return func() tea.Msg {
		if err := sendSGRMouse(sessionName, 0, col, row, false); err != nil {
			p.exitInteractiveMode()
			if isSessionDeadError(err) {
				return InteractiveSessionDeadMsg{}
			}
			return nil
		}
		if err := sendSGRMouse(sessionName, 0, col, row, true); err != nil {
			p.exitInteractiveMode()
			if isSessionDeadError(err) {
				return InteractiveSessionDeadMsg{}
			}
			return nil
		}
		p.interactiveState.LastKeyTime = time.Now()
		return nil
	}
}

func sendSGRMouse(sessionName string, button, col, row int, release bool) error {
	if col <= 0 || row <= 0 {
		return nil
	}
	suffix := "M"
	if release {
		suffix = "m"
	}
	seq := fmt.Sprintf("\x1b[<%d;%d;%d%s", button, col, row, suffix)
	return sendLiteralToTmux(sessionName, seq)
}

func (p *Plugin) interactiveMouseCoords(x, y int) (col, row int, ok bool) {
	if p.width <= 0 || p.height <= 0 {
		return 0, 0, false
	}
	if !p.shellSelected && p.previewTab != PreviewTabOutput {
		return 0, 0, false
	}

	previewX := 0
	if p.sidebarVisible {
		available := p.width - dividerWidth
		sidebarW := (available * p.sidebarWidth) / 100
		if sidebarW < 25 {
			sidebarW = 25
		}
		if sidebarW > available-40 {
			sidebarW = available - 40
		}
		previewX = sidebarW + dividerWidth
	}

	contentX := previewX + panelOverhead/2
	contentY := 1
	if !p.shellSelected {
		contentY += 2
	}
	if !p.flashPreviewTime.IsZero() && time.Since(p.flashPreviewTime) < flashDuration {
		contentY++
	}
	contentY++ // hint line

	relX := x - contentX
	relY := y - contentY
	if relX < 0 || relY < 0 {
		return 0, 0, false
	}

	paneWidth, paneHeight := p.calculatePreviewDimensions()
	if p.interactiveState != nil {
		if p.interactiveState.PaneWidth > 0 && p.interactiveState.PaneWidth < paneWidth {
			paneWidth = p.interactiveState.PaneWidth
		}
		if p.interactiveState.PaneHeight > 0 && p.interactiveState.PaneHeight < paneHeight {
			paneHeight = p.interactiveState.PaneHeight
		}
	}

	if paneWidth <= 0 || paneHeight <= 0 {
		return 0, 0, false
	}
	if relX >= paneWidth || relY >= paneHeight {
		return 0, 0, false
	}

	col = relX + 1 + p.previewHorizOffset
	row = relY + 1
	if col > paneWidth {
		col = paneWidth
	}
	if row > paneHeight {
		row = paneHeight
	}

	return col, row, true
}

// pollInteractivePane schedules a poll for interactive mode with adaptive timing.
func (p *Plugin) pollInteractivePane() tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	// Determine polling interval based on activity
	interval := pollingDecayFast
	inactivity := time.Since(p.interactiveState.LastKeyTime)

	if inactivity > inactivitySlowThreshold {
		interval = pollingDecaySlow
	} else if inactivity > inactivityMediumThreshold {
		interval = pollingDecayMedium
	}

	// Use existing shell or worktree polling mechanism
	if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
		return p.scheduleShellPollByName(p.shells[p.selectedShellIdx].TmuxName, interval)
	}
	if wt := p.selectedWorktree(); wt != nil {
		return p.scheduleAgentPoll(wt.Name, interval)
	}
	return nil
}

// scheduleDebouncedPoll schedules a poll with debounce delay to batch rapid keystrokes (td-8a0978).
// Uses generation tracking to cancel stale timers, reducing subprocess spam during typing.
func (p *Plugin) scheduleDebouncedPoll(delay time.Duration) tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	// Use shell or worktree polling mechanism based on current selection.
	// IMPORTANT: Use the correct generation map for each type (td-97327e):
	// - Shells use shellPollGeneration (checked by scheduleShellPollByName)
	// - Worktrees use pollGeneration (checked by scheduleAgentPoll)
	if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
		shellName := p.shells[p.selectedShellIdx].TmuxName
		if shellName != "" {
			p.shellPollGeneration[shellName]++
			return p.scheduleShellPollByName(shellName, delay)
		}
	} else if wt := p.selectedWorktree(); wt != nil {
		p.pollGeneration[wt.Name]++
		return p.scheduleAgentPoll(wt.Name, delay)
	}

	return nil
}

// pollInteractivePaneImmediate schedules an immediate poll for interactive mode (td-babfd9).
// Used after keystrokes to minimize latency - captures output immediately rather than
// waiting for the next poll cycle.
func (p *Plugin) pollInteractivePaneImmediate() tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	// Schedule with 0ms delay for immediate capture
	// This reduces perceived latency when typing
	if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
		return p.scheduleShellPollByName(p.shells[p.selectedShellIdx].TmuxName, 0)
	}
	if wt := p.selectedWorktree(); wt != nil {
		return p.scheduleAgentPoll(wt.Name, 0)
	}
	return nil
}

// cursorStyle defines the appearance of the cursor overlay.
// Uses bold reverse video with a bright background for maximum visibility (td-43d37b).
// The bright cyan/white combination stands out against most terminal backgrounds
// including Claude Code's diff highlighting and colored output.
var cursorStyle = lipgloss.NewStyle().
	Reverse(true).
	Bold(true).
	Background(lipgloss.Color("14")). // Bright cyan when reversed becomes the text color
	Foreground(lipgloss.Color("0"))   // Black text on bright background

// getCursorPosition returns the cached cursor position for rendering (td-648af4).
// This NEVER spawns subprocesses - it only returns cached state updated by
// queryCursorPositionCmd() which runs asynchronously during polling.
// Returns the cursor row, column (0-indexed), pane height, and whether the cursor is visible.
func (p *Plugin) getCursorPosition() (row, col, paneHeight, paneWidth int, visible bool, err error) {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return 0, 0, 0, 0, false, nil
	}

	// Return cached values - never spawn subprocess from View()
	return p.interactiveState.CursorRow, p.interactiveState.CursorCol, p.interactiveState.PaneHeight, p.interactiveState.PaneWidth, p.interactiveState.CursorVisible, nil
}

// queryCursorPositionCmd returns a tea.Cmd that queries tmux for cursor position (td-648af4).
// This is called from the poll handler when output changes, NOT from View().
// The result is delivered via cursorPositionMsg and cached in interactiveState.
func (p *Plugin) queryCursorPositionCmd() tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	paneID := p.interactiveState.TargetPane
	if paneID == "" {
		paneID = p.interactiveState.TargetSession
	}

	return func() tea.Msg {
		// Query cursor position using tmux display-message
		// #{cursor_x},#{cursor_y} gives 0-indexed position
		// #{cursor_flag} is 0 if cursor hidden (e.g., alternate screen), 1 if visible
		cmd := exec.Command("tmux", "display-message", "-t", paneID,
			"-p", "#{cursor_x},#{cursor_y},#{cursor_flag}")
		output, err := cmd.Output()
		if err != nil {
			return cursorPositionMsg{Row: 0, Col: 0, Visible: false}
		}

		parts := strings.Split(strings.TrimSpace(string(output)), ",")
		if len(parts) < 2 {
			return cursorPositionMsg{Row: 0, Col: 0, Visible: false}
		}

		col, _ := strconv.Atoi(parts[0])
		row, _ := strconv.Atoi(parts[1])
		visible := len(parts) < 3 || parts[2] != "0"

		return cursorPositionMsg{Row: row, Col: col, Visible: visible}
	}
}

// queryCursorPositionSync synchronously queries cursor position for the given target.
// Used to capture cursor position atomically with output in poll goroutines.
// Returns row, col (0-indexed), paneHeight, visible, and ok (false if query failed).
// paneHeight is needed to calculate cursor offset when display height differs from pane height.
func queryCursorPositionSync(target string) (row, col, paneHeight, paneWidth int, visible, ok bool) {
	if target == "" {
		return 0, 0, 0, 0, false, false
	}

	cmd := exec.Command("tmux", "display-message", "-t", target,
		"-p", "#{cursor_x},#{cursor_y},#{cursor_flag},#{pane_height},#{pane_width}")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, 0, 0, false, false
	}

	parts := strings.Split(strings.TrimSpace(string(output)), ",")
	if len(parts) < 2 {
		return 0, 0, 0, 0, false, false
	}

	col, _ = strconv.Atoi(parts[0])
	row, _ = strconv.Atoi(parts[1])
	visible = len(parts) < 3 || parts[2] != "0"
	if len(parts) >= 4 {
		paneHeight, _ = strconv.Atoi(parts[3])
	}
	if len(parts) >= 5 {
		paneWidth, _ = strconv.Atoi(parts[4])
	}
	return row, col, paneHeight, paneWidth, visible, true
}

// renderWithCursor overlays the cursor on content at the specified position.
// cursorRow is relative to the visible content (0 = first visible line).
// cursorCol is the column within the line (0-indexed).
// Preserves ANSI escape codes in surrounding content while rendering cursor.
func renderWithCursor(content string, cursorRow, cursorCol int, visible bool) string {
	if !visible || cursorRow < 0 || cursorCol < 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	if cursorRow >= len(lines) {
		return content
	}

	line := lines[cursorRow]

	// Use ANSI-aware width calculation for visual position
	lineWidth := ansi.StringWidth(line)

	if cursorCol >= lineWidth {
		// Cursor past end of line: append visible cursor block (td-43d37b)
		padding := cursorCol - lineWidth
		if padding < 0 {
			padding = 0
		}
		lines[cursorRow] = line + strings.Repeat(" ", padding) + cursorStyle.Render("█")
	} else {
		// Use ANSI-aware slicing to preserve escape codes in before/after
		before := ansi.Cut(line, 0, cursorCol)
		char := ansi.Cut(line, cursorCol, cursorCol+1)
		after := ansi.Cut(line, cursorCol+1, lineWidth)

		// Strip the cursor char to get clean styling
		charStripped := ansi.Strip(char)
		// Use a block character for empty/whitespace to make cursor more visible (td-43d37b)
		if charStripped == "" || charStripped == " " {
			charStripped = "█"
		}
		lines[cursorRow] = before + cursorStyle.Render(charStripped) + after
	}

	return strings.Join(lines, "\n")
}

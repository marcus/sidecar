package workspace

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	app "github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/tty"
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

	// defaultCopyKey is the default keybinding to copy selection in interactive mode.
	defaultCopyKey = "alt+c"

	// defaultPasteKey is the default keybinding to paste clipboard in interactive mode.
	defaultPasteKey = "alt+v"
)

// =============================================================================
// Scroll tuning constants (td-3b15ee)
// Adjust these to balance scroll responsiveness vs escape sequence filtering.
// =============================================================================
const (
	// scrollDebounceInterval is the base debounce for scroll events (~60fps).
	// Lower = more responsive but more CPU. Higher = smoother but laggy.
	scrollDebounceInterval = 16 * time.Millisecond

	// scrollBurstDebounce is used during fast scrolling (burst mode).
	// Lower = more responsive. Higher = better filtering but feels sluggish.
	// 32ms ≈ 30fps, good balance of smooth scrolling and reduced event spam.
	scrollBurstDebounce = 32 * time.Millisecond

	// scrollBurstThreshold is scroll events needed to enter burst mode.
	// Lower = enter burst mode faster. Higher = more normal scrolling before burst kicks in.
	scrollBurstThreshold = 3

	// scrollBurstTimeout is how long after last scroll before burst mode ends.
	// Should be long enough for garbage events to clear. Too long = delayed typing response.
	scrollBurstTimeout = 500 * time.Millisecond

	// snapBackCooldown prevents snap-back to live output during active scrolling.
	// If user scrolled within this window, suspicious input won't trigger snap-back.
	snapBackCooldown = 100 * time.Millisecond

	// postScrollFilterWindow is how long after scrolling to keep filtering garbage input.
	// Mouse event garbage can arrive after scroll ends due to terminal/OS buffering.
	// Longer = better filtering but may eat legitimate keystrokes. Shorter = risk of leakage.
	postScrollFilterWindow = 500 * time.Millisecond
)

// partialMouseSeqRegex is now provided by the tty package as tty.PartialMouseSeqRegex

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
		if key := p.ctx.Config.Plugins.Workspace.InteractiveExitKey; key != "" {
			return key
		}
	}
	return defaultExitKey
}

// getInteractiveAttachKey returns the configured attach keybinding for interactive mode (td-fd68d1).
// Falls back to defaultAttachKey ("ctrl+]") if not configured.
func (p *Plugin) getInteractiveAttachKey() string {
	if p.ctx != nil && p.ctx.Config != nil {
		if key := p.ctx.Config.Plugins.Workspace.InteractiveAttachKey; key != "" {
			return key
		}
	}
	return defaultAttachKey
}

// getInteractiveCopyKey returns the configured copy keybinding for interactive mode.
// Falls back to defaultCopyKey ("alt+c") if not configured.
func (p *Plugin) getInteractiveCopyKey() string {
	if p.ctx != nil && p.ctx.Config != nil {
		if key := p.ctx.Config.Plugins.Workspace.InteractiveCopyKey; key != "" {
			return key
		}
	}
	return defaultCopyKey
}

// getInteractivePasteKey returns the configured paste keybinding for interactive mode.
// Falls back to defaultPasteKey ("alt+v") if not configured.
func (p *Plugin) getInteractivePasteKey() string {
	if p.ctx != nil && p.ctx.Config != nil {
		if key := p.ctx.Config.Plugins.Workspace.InteractivePasteKey; key != "" {
			return key
		}
	}
	return defaultPasteKey
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

// MapKeyToTmux is a wrapper around tty.MapKeyToTmux for backward compatibility.
// See tty.MapKeyToTmux for documentation.
func MapKeyToTmux(msg tea.KeyMsg) (key string, useLiteral bool) {
	return tty.MapKeyToTmux(msg)
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

// keySpec describes a key to send to tmux with ordering preserved.
type keySpec struct {
	value   string
	literal bool
}

// sendInteractiveKeysCmd sends keys to tmux asynchronously (td-c2961e).
// Keys are sent in order within a single goroutine to prevent reordering.
// Returns InteractiveSessionDeadMsg if the session has ended.
func sendInteractiveKeysCmd(sessionName string, keys ...keySpec) tea.Cmd {
	return func() tea.Msg {
		for _, k := range keys {
			var err error
			if k.literal {
				err = sendLiteralToTmux(sessionName, k.value)
			} else {
				err = sendKeyToTmux(sessionName, k.value)
			}
			if err != nil && isSessionDeadError(err) {
				return InteractiveSessionDeadMsg{}
			}
		}
		return nil
	}
}

// sendInteractivePasteInputCmd sends paste text to tmux asynchronously (td-c2961e).
// Used for multi-character terminal input (not clipboard paste which is already async).
func sendInteractivePasteInputCmd(sessionName, text string, bracketed bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if bracketed {
			err = sendBracketedPasteToTmux(sessionName, text)
		} else {
			err = sendPasteToTmux(sessionName, text)
		}
		if err != nil && isSessionDeadError(err) {
			return InteractiveSessionDeadMsg{}
		}
		return nil
	}
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

func (p *Plugin) pasteClipboardToTmuxCmd() tea.Cmd {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}

	sessionName := p.interactiveState.TargetSession
	if sessionName == "" {
		return nil
	}
	bracketed := p.interactiveState.BracketedPasteEnabled

	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil {
			return InteractivePasteResultMsg{Err: err}
		}
		if text == "" {
			return InteractivePasteResultMsg{Empty: true}
		}

		if bracketed {
			err = sendBracketedPasteToTmux(sessionName, text)
		} else {
			err = sendPasteToTmux(sessionName, text)
		}
		if err != nil {
			return InteractivePasteResultMsg{Err: err, SessionDead: isSessionDeadError(err)}
		}

		return InteractivePasteResultMsg{}
	}
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
	if msg.Type != tea.KeyRunes {
		return false
	}
	if msg.Paste {
		return true
	}
	if len(msg.Runes) <= 1 {
		return false
	}
	text := string(msg.Runes)
	// Treat as paste if contains newline or is suspiciously long for typing
	return strings.Contains(text, "\n") || len(msg.Runes) > 10
}

// isNormalTyping returns true if the input looks like normal keyboard typing.
// Used during scroll bursts to distinguish real typing from garbage input.
func isNormalTyping(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Single printable character is normal typing
	if len(s) == 1 {
		r := rune(s[0])
		return r >= 32 && r < 127 // Printable ASCII
	}
	// Multi-char: only allow if all are printable alphanumeric or common punctuation
	// Reject anything that looks like control sequences
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
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

		// td-f88fdd: Handle orphaned shells - recreate before entering interactive mode
		if shell.IsOrphaned {
			return p.recreateOrphanedShell(p.selectedShellIdx)
		}

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
	p.clearInteractiveSelection()

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
	cmds := []tea.Cmd{p.pollInteractivePane()}
	if !p.interactiveCopyPasteHintShown {
		p.interactiveCopyPasteHintShown = true
		cmds = append(cmds, func() tea.Msg {
			return app.ToastMsg{
				Message:  fmt.Sprintf("Interactive copy/paste: %s / %s (configurable)", p.getInteractiveCopyKey(), p.getInteractivePasteKey()),
				Duration: 3 * time.Second,
			}
		})
	}
	return tea.Batch(cmds...)
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

	return p.resizeTmuxTargetCmd(target)
}

// resizeTmuxTargetCmd returns a tea.Cmd that resizes a tmux target to preview dimensions.
// Skips resize if current size already matches. Retries once if verify fails.
// Returns paneResizedMsg when the size actually changed, triggering a fresh poll
// so captured content reflects the new width/wrapping.
func (p *Plugin) resizeTmuxTargetCmd(target string) tea.Cmd {
	if target == "" {
		return nil
	}

	previewWidth, previewHeight := p.calculatePreviewDimensions()
	return func() tea.Msg {
		if actualWidth, actualHeight, ok := queryPaneSize(target); ok {
			if actualWidth == previewWidth && actualHeight == previewHeight {
				return nil
			}
		}
		p.resizeTmuxPane(target, previewWidth, previewHeight)
		if actualWidth, actualHeight, ok := queryPaneSize(target); ok {
			if actualWidth != previewWidth || actualHeight != previewHeight {
				p.resizeTmuxPane(target, previewWidth, previewHeight)
			}
		}
		return paneResizedMsg{}
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

// resizeTmuxPane resizes a tmux window/pane to the specified dimensions.
// resize-window works for detached sessions; resize-pane is a fallback.
func (p *Plugin) resizeTmuxPane(paneID string, width, height int) {
	if width <= 0 && height <= 0 {
		return
	}

	args := []string{"resize-window", "-t", paneID}
	if width > 0 {
		args = append(args, "-x", strconv.Itoa(width))
	}
	if height > 0 {
		args = append(args, "-y", strconv.Itoa(height))
	}
	cmd := exec.Command("tmux", args...)
	if err := cmd.Run(); err == nil {
		return
	}

	// Fallback for older tmux or attached clients that reject resize-window.
	args = []string{"resize-pane", "-t", paneID}
	if width > 0 {
		args = append(args, "-x", strconv.Itoa(width))
	}
	if height > 0 {
		args = append(args, "-y", strconv.Itoa(height))
	}
	_ = exec.Command("tmux", args...).Run()
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
// is already wrapped at the correct width.
func (p *Plugin) resizeSelectedPaneCmd() tea.Cmd {
	if !features.IsEnabled(features.TmuxInteractiveInput.Name) {
		return nil
	}
	return p.resizeTmuxTargetCmd(p.previewResizeTarget())
}

// resizeForAttachCmd resizes the tmux pane to the full terminal size before
// attaching, so the user gets the full available space without dot borders.
func (p *Plugin) resizeForAttachCmd(target string) tea.Cmd {
	if target == "" {
		return nil
	}
	width, height := p.width, p.height
	if width <= 0 || height <= 0 {
		return nil
	}
	return func() tea.Msg {
		p.resizeTmuxPane(target, width, height)
		return nil
	}
}

// attachWithResize resizes the tmux pane to full terminal, waits briefly for
// tmux to process, then attaches. Centralizes resize-before-attach logic.
func (p *Plugin) attachWithResize(target, sessionName, displayName string, onComplete func(error) tea.Msg) tea.Cmd {
	c := exec.Command("tmux", "attach-session", "-t", sessionName)
	return tea.Sequence(
		p.resizeForAttachCmd(target),
		tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return nil }),
		tea.Printf("\nAttaching to %s. Press %s d to return to sidecar.\n", displayName, getTmuxPrefix()),
		tea.ExecProcess(c, onComplete),
	)
}

// previewResizeTarget returns the tmux target for the currently selected pane.
func (p *Plugin) previewResizeTarget() string {
	if p.shellSelected {
		shell := p.getSelectedShell()
		if shell == nil || shell.Agent == nil {
			return ""
		}
		if shell.Agent.TmuxPane != "" {
			return shell.Agent.TmuxPane
		}
		return shell.Agent.TmuxSession
	}

	wt := p.selectedWorktree()
	if wt == nil || wt.Agent == nil {
		return ""
	}
	if wt.Agent.TmuxPane != "" {
		return wt.Agent.TmuxPane
	}
	return wt.Agent.TmuxSession
}

// exitInteractiveMode exits interactive mode and returns to list view.
func (p *Plugin) exitInteractiveMode() {
	if p.interactiveState != nil {
		p.interactiveState.Active = false
	}
	p.interactiveState = nil
	p.clearInteractiveSelection()
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

	// td-3b15ee: Fast-path rejection during and after scroll bursts.
	// Mouse event garbage can continue arriving after scroll ends due to
	// terminal/OS buffering. Use time-based check for wider protection window.
	timeSinceScroll := time.Since(p.lastScrollTime)
	if timeSinceScroll < postScrollFilterWindow && msg.Type == tea.KeyRunes {
		s := string(msg.Runes)
		// Drop anything that looks like mouse sequence garbage:
		// - Contains [, <, ;, M, m (mouse sequence chars)
		// - Is not normal alphanumeric typing
		if strings.ContainsAny(s, "[<;Mm") || !isNormalTyping(s) {
			p.interactiveState.EscapePressed = false
			return nil
		}
	}

	// Filter partial SGR mouse sequences that leaked through Bubble Tea's
	// input parser due to split-read timing (ESC arrived separately) (td-791865).
	// Must be checked BEFORE forwarding pending escape, since the ESC was part
	// of the mouse sequence, not a real user keypress.
	// td-e2ce50: Use lenient check to catch truncated/split sequences during fast scrolling.
	// Catches even very short fragments like "[<" that were previously slipping through.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		if tty.LooksLikeMouseFragment(string(msg.Runes)) {
			// Cancel the pending escape — it was the leading byte of this mouse event
			p.interactiveState.EscapePressed = false
			return nil // Drop mouse sequence fragments
		}
	}

	// Non-escape key: check if we have a pending Escape to forward first
	var cmds []tea.Cmd
	pendingEscape := false
	if p.interactiveState.EscapePressed {
		p.interactiveState.EscapePressed = false
		// Timer leak prevention (td-83dc22): pending timer will be ignored when it fires
		// since EscapePressed is now false (no need to cancel, it's harmless)
		pendingEscape = true
	}

	if msg.String() == p.getInteractiveCopyKey() {
		return p.copyInteractiveSelectionCmd()
	}

	if msg.String() == p.getInteractivePasteKey() {
		p.interactiveState.LastKeyTime = time.Now()
		if p.previewOffset > 0 {
			p.previewOffset = 0
			p.autoScrollOutput = true
			p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
		}
		cmds = append(cmds, p.pasteClipboardToTmuxCmd())
		return tea.Batch(cmds...)
	}

	// Update last key time for polling decay
	p.interactiveState.LastKeyTime = time.Now()

	// Snap back to live view if scrolled up, so user can see what they're typing
	// td-e2ce50: Multiple guards against bounce during fast scrolling:
	// 1. Don't snap back if we recently scrolled (time-based protection)
	// 2. Don't snap back for mouse sequence fragments
	// 3. Only snap back for actual user typing (single printable chars or specific keys)
	if p.previewOffset > 0 && p.shouldSnapBack(msg) {
		p.previewOffset = 0
		p.autoScrollOutput = true
		p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
	}

	sessionName := p.interactiveState.TargetSession

	// Check for paste (multi-character input with newlines or long text)
	if isPasteInput(msg) {
		text := string(msg.Runes)
		bracketed := p.interactiveState.BracketedPasteEnabled
		// Send paste async (td-c2961e): escape + paste in order if pending
		if pendingEscape {
			cmds = append(cmds, func() tea.Msg {
				if err := sendKeyToTmux(sessionName, "Escape"); err != nil && isSessionDeadError(err) {
					return InteractiveSessionDeadMsg{}
				}
				var err error
				if bracketed {
					err = sendBracketedPasteToTmux(sessionName, text)
				} else {
					err = sendPasteToTmux(sessionName, text)
				}
				if err != nil && isSessionDeadError(err) {
					return InteractiveSessionDeadMsg{}
				}
				return nil
			})
		} else {
			cmds = append(cmds, sendInteractivePasteInputCmd(sessionName, text, bracketed))
		}
		cmds = append(cmds, p.pollInteractivePane())
		return tea.Batch(cmds...)
	}

	// Map key to tmux format and send
	key, useLiteral := MapKeyToTmux(msg)
	if key == "" {
		// Still send pending escape if nothing else to send
		if pendingEscape {
			cmds = append(cmds, sendInteractiveKeysCmd(sessionName, keySpec{"Escape", false}))
			cmds = append(cmds, p.scheduleDebouncedPoll(keystrokeDebounce))
		}
		return tea.Batch(cmds...)
	}

	// Send keys async (td-c2961e): pending escape + key in order within single goroutine
	if pendingEscape {
		cmds = append(cmds, sendInteractiveKeysCmd(sessionName,
			keySpec{"Escape", false},
			keySpec{key, useLiteral},
		))
	} else {
		cmds = append(cmds, sendInteractiveKeysCmd(sessionName, keySpec{key, useLiteral}))
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

	// Timer fired with pending Escape: forward the single Escape to tmux async (td-c2961e)
	p.interactiveState.EscapePressed = false

	// Update last key time and poll immediately for better responsiveness (td-babfd9)
	p.interactiveState.LastKeyTime = time.Now()
	return tea.Batch(
		sendInteractiveKeysCmd(p.interactiveState.TargetSession, keySpec{"Escape", false}),
		p.pollInteractivePaneImmediate(),
	)
}

// forwardScrollToTmux scrolls through the captured pane output using previewOffset.
// No tmux subprocesses needed — we scroll through the already-captured 600 lines of scrollback.
// Scroll up (delta < 0) pauses auto-scroll, scroll down (delta > 0) moves toward live output.
func (p *Plugin) forwardScrollToTmux(delta int) tea.Cmd {
	now := time.Now()

	// Detect and handle scroll bursts (fast trackpad scrolling)
	timeSinceLastScroll := now.Sub(p.lastScrollTime)
	if timeSinceLastScroll < scrollBurstTimeout {
		p.scrollBurstCount++
	} else {
		// Burst ended, reset
		p.scrollBurstCount = 1
		p.scrollBurstStarted = now
	}

	// During burst mode, use more aggressive debouncing
	debounceInterval := scrollDebounceInterval
	if p.scrollBurstCount > scrollBurstThreshold {
		debounceInterval = scrollBurstDebounce
	}

	if timeSinceLastScroll < debounceInterval {
		return nil
	}
	p.lastScrollTime = now

	if delta < 0 {
		// Scroll up: pause auto-scroll, show older content
		p.autoScrollOutput = false
		p.captureScrollBaseLineCount() // td-f7c8be: prevent bounce on poll
		p.previewOffset++
	} else {
		// Scroll down: show newer content, resume auto-scroll at bottom
		if p.previewOffset > 0 {
			p.previewOffset--
			if p.previewOffset == 0 {
				p.autoScrollOutput = true
				p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
			}
		}
	}
	return nil
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

	col = relX + 1
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

	// td-3b15ee: Skip polling during active scroll bursts.
	// User is scrolling through already-captured content; no need for new captures.
	// This reduces CPU load and prevents capturing garbage during fast scrolling.
	if time.Since(p.lastScrollTime) < scrollBurstTimeout && p.scrollBurstCount > 0 {
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
	// Worktrees use scheduleInteractivePoll to skip stagger (td-8856c9)
	if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
		return p.scheduleShellPollByName(p.shells[p.selectedShellIdx].TmuxName, interval)
	}
	if wt := p.selectedWorktree(); wt != nil {
		return p.scheduleInteractivePoll(wt.Name, interval)
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
	// - Worktrees use pollGeneration (checked by scheduleInteractivePoll)
	if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
		shellName := p.shells[p.selectedShellIdx].TmuxName
		if shellName != "" {
			p.shellPollGeneration[shellName]++
			return p.scheduleShellPollByName(shellName, delay)
		}
	} else if wt := p.selectedWorktree(); wt != nil {
		p.pollGeneration[wt.Name]++
		return p.scheduleInteractivePoll(wt.Name, delay)
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

	// td-3b15ee: Skip polling during active scroll bursts.
	if time.Since(p.lastScrollTime) < scrollBurstTimeout && p.scrollBurstCount > 0 {
		return nil
	}

	// Schedule with 0ms delay for immediate capture (td-8856c9: no stagger for worktrees)
	if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
		return p.scheduleShellPollByName(p.shells[p.selectedShellIdx].TmuxName, 0)
	}
	if wt := p.selectedWorktree(); wt != nil {
		return p.scheduleInteractivePoll(wt.Name, 0)
	}
	return nil
}

// cursorStyle defines the appearance of the cursor overlay.
// Uses bold reverse video with a bright background for maximum visibility (td-43d37b).
// The bright cyan/white combination stands out against most terminal backgrounds
// including Claude Code's diff highlighting and colored output.
// cursorStyle returns the cursor style using current theme colors.
func cursorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Reverse(true).
		Bold(true).
		Background(styles.Primary).
		Foreground(styles.BgPrimary)
}

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
		lines[cursorRow] = line + strings.Repeat(" ", padding) + cursorStyle().Render("█")
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
		lines[cursorRow] = before + cursorStyle().Render(charStripped) + after
	}

	return strings.Join(lines, "\n")
}

// shouldSnapBack determines if we should snap back to live view for a given key (td-e2ce50).
// Returns false during active scrolling or for input that looks like mouse sequence fragments.
// This prevents bounce-scroll caused by split mouse events triggering snap-back.
func (p *Plugin) shouldSnapBack(msg tea.KeyMsg) bool {
	// Guard 1: Don't snap back during active scrolling (time-based protection)
	// If user scrolled recently, suspicious input is likely mouse garbage
	if time.Since(p.lastScrollTime) < snapBackCooldown {
		return false
	}

	// Guard 2: Don't snap back for anything that looks like mouse sequence data
	if msg.Type == tea.KeyRunes {
		s := string(msg.Runes)
		// Check for any mouse-like fragments
		if tty.LooksLikeMouseFragment(s) {
			return false
		}
		// Multi-character input (not single keypress) is suspicious during scrolling
		// Could be paste (which we handle separately) or split mouse sequence
		if len(msg.Runes) > 1 {
			return false
		}
	}

	// Guard 3: Don't snap back for Escape - it might be start of a mouse sequence
	// Real escape is handled by the double-escape exit logic
	if msg.Type == tea.KeyEscape {
		return false
	}

	// Snap back for actual user typing:
	// - Single printable characters
	// - Navigation/editing keys
	switch msg.Type {
	case tea.KeyRunes:
		// Single character that's not suspicious
		return len(msg.Runes) == 1
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace, tea.KeyDelete,
		tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight,
		tea.KeyHome, tea.KeyEnd, tea.KeyPgUp, tea.KeyPgDown:
		return true
	default:
		// Other special keys (ctrl+x, etc.) - snap back
		return true
	}
}

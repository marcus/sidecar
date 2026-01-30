package tty

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Config holds configuration options for a tty Model.
type Config struct {
	// ExitKey is the keybinding to exit interactive mode (default: "ctrl+\\").
	ExitKey string

	// AttachKey is the keybinding to attach to the full tmux session (default: "ctrl+]").
	AttachKey string

	// CopyKey is the keybinding to copy selection (default: "alt+c").
	CopyKey string

	// PasteKey is the keybinding to paste clipboard (default: "alt+v").
	PasteKey string

	// ScrollbackLines is the number of scrollback lines to capture (default: 600).
	ScrollbackLines int
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		ExitKey:         "ctrl+\\",
		AttachKey:       "ctrl+]",
		CopyKey:         "alt+c",
		PasteKey:        "alt+v",
		ScrollbackLines: 600,
	}
}

// State tracks the interactive mode state for a tmux session.
type State struct {
	// Active indicates whether interactive mode is currently active.
	Active bool

	// TargetPane is the tmux pane ID (e.g., "%12") receiving input.
	TargetPane string

	// TargetSession is the tmux session name for the active pane.
	TargetSession string

	// LastKeyTime tracks when the last key was sent for polling decay.
	LastKeyTime time.Time

	// Escape handling state
	EscapePressed      bool
	EscapeTime         time.Time
	EscapeTimerPending bool

	// Cursor state (updated asynchronously via CaptureResultMsg)
	CursorRow     int
	CursorCol     int
	CursorVisible bool
	PaneHeight    int
	PaneWidth     int

	// Terminal mode state (updated from captured output)
	BracketedPasteEnabled bool
	MouseReportingEnabled bool

	// Visible buffer range for selection mapping
	VisibleStart     int
	VisibleEnd       int
	ContentRowOffset int

	// Resize debouncing
	LastResizeAt time.Time

	// Output buffer
	OutputBuf *OutputBuffer

	// Poll generation for invalidating stale polls
	PollGeneration int
}

// Model is an embeddable component that provides interactive tmux functionality.
// Plugins embed this Model and delegate Update/View when interactive mode is active.
type Model struct {
	Config Config
	State  *State

	// Width and Height are set by the containing plugin
	Width  int
	Height int

	// Callbacks for plugin integration
	OnExit   func() tea.Cmd // Called when user exits interactive mode
	OnAttach func() tea.Cmd // Called when user requests full tmux attach
}

// New creates a new tty Model with the given configuration.
// If config is nil, DefaultConfig() is used.
func New(config *Config) *Model {
	cfg := DefaultConfig()
	if config != nil {
		if config.ExitKey != "" {
			cfg.ExitKey = config.ExitKey
		}
		if config.AttachKey != "" {
			cfg.AttachKey = config.AttachKey
		}
		if config.CopyKey != "" {
			cfg.CopyKey = config.CopyKey
		}
		if config.PasteKey != "" {
			cfg.PasteKey = config.PasteKey
		}
		if config.ScrollbackLines > 0 {
			cfg.ScrollbackLines = config.ScrollbackLines
		}
	}
	return &Model{
		Config: cfg,
	}
}

// IsActive returns whether interactive mode is currently active.
func (m *Model) IsActive() bool {
	return m.State != nil && m.State.Active
}

// Enter enters interactive mode for the specified tmux session/pane.
// Returns a tea.Cmd to start polling for output.
func (m *Model) Enter(sessionName, paneID string) tea.Cmd {
	m.State = &State{
		Active:        true,
		TargetPane:    paneID,
		TargetSession: sessionName,
		LastKeyTime:   time.Now(),
		CursorVisible: true,
		OutputBuf:     NewOutputBuffer(m.Config.ScrollbackLines),
	}

	// Resize pane to match view dimensions
	target := paneID
	if target == "" {
		target = sessionName
	}
	if target != "" && m.Width > 0 && m.Height > 0 {
		ResizeTmuxPane(target, m.Width, m.Height)
	}

	// Return command to trigger initial poll
	return m.schedulePoll(0)
}

// Exit exits interactive mode.
func (m *Model) Exit() {
	if m.State != nil {
		m.State.Active = false
	}
	m.State = nil
}

// Update handles messages in interactive mode.
// Returns the updated model and any commands to execute.
// Plugins should call this when they receive messages and interactive mode is active.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if !m.IsActive() {
		return nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case EscapeTimerMsg:
		return m.handleEscapeTimer()

	case CaptureResultMsg:
		return m.handleCaptureResult(msg)

	case PollTickMsg:
		return m.handlePollTick(msg)

	case PaneResizedMsg:
		return m.schedulePoll(0)

	case SessionDeadMsg:
		m.Exit()
		if m.OnExit != nil {
			return m.OnExit()
		}
		return nil

	case PasteResultMsg:
		if msg.SessionDead {
			m.Exit()
			if m.OnExit != nil {
				return m.OnExit()
			}
		}
		return nil
	}

	return nil
}

// View renders the interactive terminal content with cursor overlay.
// Plugins should call this to render the terminal when interactive mode is active.
func (m *Model) View() string {
	if !m.IsActive() || m.State.OutputBuf == nil {
		return ""
	}

	lines := m.State.OutputBuf.Lines()
	content := strings.Join(lines, "\n")

	// Overlay cursor if visible
	if m.State.CursorVisible && m.State.CursorRow >= 0 && m.State.CursorCol >= 0 {
		// Adjust cursor row to visible content
		cursorRow := m.State.CursorRow
		if m.State.PaneHeight > 0 && m.Height > 0 && m.State.PaneHeight != m.Height {
			// Adjust for pane height difference
			cursorRow = m.State.CursorRow - (m.State.PaneHeight - m.Height)
		}
		content = RenderWithCursor(content, cursorRow, m.State.CursorCol, true)
	}

	return content
}

// GetTarget returns the current tmux target (pane ID or session name).
func (m *Model) GetTarget() string {
	if !m.IsActive() {
		return ""
	}
	if m.State.TargetPane != "" {
		return m.State.TargetPane
	}
	return m.State.TargetSession
}

// handleKey processes key input in interactive mode.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if !m.IsActive() {
		return nil
	}

	// Check for exit key
	if msg.String() == m.Config.ExitKey {
		m.Exit()
		if m.OnExit != nil {
			return m.OnExit()
		}
		return nil
	}

	// Check for attach key
	if msg.String() == m.Config.AttachKey {
		m.Exit()
		if m.OnAttach != nil {
			return m.OnAttach()
		}
		return nil
	}

	// Double-escape exit handling
	if msg.Type == tea.KeyEscape {
		if m.State.EscapePressed {
			// Second Escape within window: exit
			m.State.EscapePressed = false
			m.State.EscapeTimerPending = false
			m.Exit()
			if m.OnExit != nil {
				return m.OnExit()
			}
			return nil
		}
		// First Escape: mark pending and start delay timer
		m.State.EscapePressed = true
		m.State.EscapeTime = time.Now()
		if !m.State.EscapeTimerPending {
			m.State.EscapeTimerPending = true
			return tea.Tick(DoubleEscapeDelay, func(t time.Time) tea.Msg {
				return EscapeTimerMsg{}
			})
		}
		return nil
	}

	// Filter partial SGR mouse sequences (td-e2ce50: use lenient check for truncated sequences)
	// Catches even very short fragments like "[<" that occur when terminal splits mouse events.
	// Note: bare "[" is NEVER filtered. With tea.WithMouseAllMotion(), leaked mouse ESC bytes
	// frequently set EscapePressed, making EscapePressed unreliable for gating "[".
	// Multi-char fragments like "[<35;10;20M" are caught by LooksLikeMouseFragment.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		if LooksLikeMouseFragment(string(msg.Runes)) {
			m.State.EscapePressed = false
			return nil // Drop mouse sequence fragments
		}
	}

	// Handle pending escape before processing new key
	var cmds []tea.Cmd
	pendingEscape := false
	if m.State.EscapePressed {
		m.State.EscapePressed = false
		pendingEscape = true
	}

	// Paste key
	if msg.String() == m.Config.PasteKey {
		m.State.LastKeyTime = time.Now()
		return PasteClipboardToTmuxCmd(m.State.TargetSession, m.State.BracketedPasteEnabled)
	}

	// Update last key time
	m.State.LastKeyTime = time.Now()

	sessionName := m.State.TargetSession

	// Check for paste input
	if IsPasteInput(msg) {
		text := string(msg.Runes)
		bracketed := m.State.BracketedPasteEnabled
		if pendingEscape {
			cmds = append(cmds, func() tea.Msg {
				if err := SendKeyToTmux(sessionName, "Escape"); err != nil && IsSessionDeadError(err) {
					return SessionDeadMsg{}
				}
				var err error
				if bracketed {
					err = SendBracketedPasteToTmux(sessionName, text)
				} else {
					err = SendPasteToTmux(sessionName, text)
				}
				if err != nil && IsSessionDeadError(err) {
					return SessionDeadMsg{}
				}
				return nil
			})
		} else {
			cmds = append(cmds, SendPasteInputCmd(sessionName, text, bracketed))
		}
		cmds = append(cmds, m.schedulePoll(KeystrokeDebounce))
		return tea.Batch(cmds...)
	}

	// Map key to tmux format and send
	key, useLiteral := MapKeyToTmux(msg)
	if key == "" {
		if pendingEscape {
			cmds = append(cmds, SendKeysCmd(sessionName, KeySpec{"Escape", false}))
			cmds = append(cmds, m.schedulePoll(KeystrokeDebounce))
		}
		return tea.Batch(cmds...)
	}

	// Send keys
	if pendingEscape {
		cmds = append(cmds, SendKeysCmd(sessionName,
			KeySpec{"Escape", false},
			KeySpec{key, useLiteral},
		))
	} else {
		cmds = append(cmds, SendKeysCmd(sessionName, KeySpec{key, useLiteral}))
	}

	cmds = append(cmds, m.schedulePoll(KeystrokeDebounce))
	return tea.Batch(cmds...)
}

// handleMouse processes mouse input in interactive mode.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !m.IsActive() || !m.State.MouseReportingEnabled {
		return nil
	}

	// Only handle click events
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}

	// Convert to pane-relative coordinates
	col := msg.X + 1
	row := msg.Y + 1

	sessionName := m.State.TargetSession
	return func() tea.Msg {
		if err := SendSGRMouse(sessionName, 0, col, row, false); err != nil {
			if IsSessionDeadError(err) {
				return SessionDeadMsg{}
			}
			return nil
		}
		if err := SendSGRMouse(sessionName, 0, col, row, true); err != nil {
			if IsSessionDeadError(err) {
				return SessionDeadMsg{}
			}
		}
		return nil
	}
}

// handleEscapeTimer processes the escape delay timer firing.
func (m *Model) handleEscapeTimer() tea.Cmd {
	if !m.IsActive() {
		return nil
	}

	m.State.EscapeTimerPending = false

	if !m.State.EscapePressed {
		return nil
	}

	// Timer fired with pending Escape: forward it to tmux
	m.State.EscapePressed = false
	m.State.LastKeyTime = time.Now()

	return tea.Batch(
		SendKeysCmd(m.State.TargetSession, KeySpec{"Escape", false}),
		m.schedulePoll(0),
	)
}

// handleCaptureResult processes captured output from tmux.
func (m *Model) handleCaptureResult(msg CaptureResultMsg) tea.Cmd {
	if !m.IsActive() || m.State.OutputBuf == nil {
		return nil
	}

	if msg.Err != nil {
		if IsSessionDeadError(msg.Err) {
			m.Exit()
			if m.OnExit != nil {
				return m.OnExit()
			}
		}
		return nil
	}

	// Update output buffer
	changed := m.State.OutputBuf.Update(msg.Output)

	// Update cursor state
	m.State.CursorRow = msg.CursorRow
	m.State.CursorCol = msg.CursorCol
	m.State.CursorVisible = msg.CursorVisible
	m.State.PaneHeight = msg.PaneHeight
	m.State.PaneWidth = msg.PaneWidth

	// Update terminal mode state
	if changed {
		m.State.BracketedPasteEnabled = DetectBracketedPasteMode(msg.Output)
		m.State.MouseReportingEnabled = DetectMouseReportingMode(msg.Output)
	}

	// Schedule next poll with adaptive interval
	return m.schedulePoll(CalculatePollingInterval(m.State.LastKeyTime))
}

// handlePollTick handles a poll tick message.
func (m *Model) handlePollTick(msg PollTickMsg) tea.Cmd {
	if !m.IsActive() {
		return nil
	}

	// Check generation to skip stale polls
	if msg.Generation != m.State.PollGeneration {
		return nil
	}

	target := m.GetTarget()
	if target == "" {
		return nil
	}

	// Capture output and cursor position atomically
	return func() tea.Msg {
		output, err := CapturePaneOutput(target, m.Config.ScrollbackLines)
		if err != nil {
			return CaptureResultMsg{Target: target, Err: err}
		}

		row, col, paneHeight, paneWidth, visible, _ := QueryCursorPositionSync(target)

		return CaptureResultMsg{
			Target:        target,
			Output:        output,
			CursorRow:     row,
			CursorCol:     col,
			CursorVisible: visible,
			PaneHeight:    paneHeight,
			PaneWidth:     paneWidth,
		}
	}
}

// schedulePoll schedules a poll with the given delay.
func (m *Model) schedulePoll(delay time.Duration) tea.Cmd {
	if !m.IsActive() {
		return nil
	}

	m.State.PollGeneration++
	gen := m.State.PollGeneration
	target := m.GetTarget()

	if delay <= 0 {
		return func() tea.Msg {
			return PollTickMsg{Target: target, Generation: gen}
		}
	}

	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return PollTickMsg{Target: target, Generation: gen}
	})
}

// SetDimensions updates the view dimensions for resize handling.
func (m *Model) SetDimensions(width, height int) tea.Cmd {
	if width == m.Width && height == m.Height {
		return nil
	}

	m.Width = width
	m.Height = height

	if !m.IsActive() {
		return nil
	}

	// Debounce resize
	if !m.State.LastResizeAt.IsZero() && time.Since(m.State.LastResizeAt) < 500*time.Millisecond {
		return nil
	}
	m.State.LastResizeAt = time.Now()

	target := m.GetTarget()
	if target == "" {
		return nil
	}

	return func() tea.Msg {
		// Check if resize is needed
		actualWidth, actualHeight, ok := QueryPaneSize(target)
		if ok && actualWidth == width && actualHeight == height {
			return nil
		}
		ResizeTmuxPane(target, width, height)
		return PaneResizedMsg{}
	}
}

package workspace

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/state"
)

// Shell session constants
const (
	shellSessionPrefix = "sidecar-sh-" // Distinct from worktree prefix "sidecar-ws-"
)

// tmuxInstalled caches whether tmux is available in PATH.
// Checked once and cached to avoid repeated exec calls.
var (
	tmuxInstalledOnce   sync.Once
	tmuxInstalledCached bool

	tmuxPrefixOnce   sync.Once
	tmuxPrefixCached string
)

// isTmuxInstalled returns true if tmux is available in PATH.
// Result is cached after first check.
func isTmuxInstalled() bool {
	tmuxInstalledOnce.Do(func() {
		_, err := exec.LookPath("tmux")
		tmuxInstalledCached = err == nil
	})
	return tmuxInstalledCached
}

// getTmuxPrefix returns the user's tmux prefix key in human-readable format.
// Queries `tmux show-options -g prefix` and converts notation (C-b → Ctrl-b).
// Falls back to "Ctrl-b" if detection fails. Result is cached.
func getTmuxPrefix() string {
	tmuxPrefixOnce.Do(func() {
		tmuxPrefixCached = "Ctrl-b" // default fallback

		if !isTmuxInstalled() {
			return
		}

		out, err := exec.Command("tmux", "show-options", "-g", "prefix").Output()
		if err != nil {
			return
		}

		// Output format: "prefix C-b" or "prefix C-a"
		line := strings.TrimSpace(string(out))
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return
		}

		tmuxPrefixCached = tmuxNotationToHuman(parts[1])
	})
	return tmuxPrefixCached
}

// tmuxNotationToHuman converts tmux key notation to human-readable format.
// Examples: C-b → Ctrl-b, C-a → Ctrl-a, M-x → Alt-x
func tmuxNotationToHuman(notation string) string {
	if len(notation) < 2 {
		return notation
	}

	// Handle C- prefix (Ctrl)
	if strings.HasPrefix(notation, "C-") {
		return "Ctrl-" + notation[2:]
	}

	// Handle M- prefix (Meta/Alt)
	if strings.HasPrefix(notation, "M-") {
		return "Alt-" + notation[2:]
	}

	return notation
}

// getTmuxInstallInstructions returns platform-specific tmux install instructions.
func getTmuxInstallInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install tmux"
	case "linux":
		return "sudo apt install tmux  # or: sudo dnf install tmux"
	default:
		return "Install tmux from your package manager"
	}
}

// Shell session messages
type (
	// ShellCreatedMsg signals shell session was created
	ShellCreatedMsg struct {
		SessionName string // tmux session name
		DisplayName string // Display name (e.g., "Shell 1")
		PaneID      string // tmux pane ID (e.g., "%12") for interactive mode
		Err         error  // Non-nil if creation failed
	}

	// ShellDetachedMsg signals user detached from shell session
	ShellDetachedMsg struct {
		Err error
	}

	// ShellKilledMsg signals shell session was terminated
	ShellKilledMsg struct {
		SessionName string // tmux session name that was killed
	}

	// ShellSessionDeadMsg signals shell session was externally terminated
	// (e.g., user typed 'exit' in the shell)
	ShellSessionDeadMsg struct {
		TmuxName string // Session name for cleanup (stable identifier)
	}

	// ShellOutputMsg signals shell output was captured (for polling)
	ShellOutputMsg struct {
		TmuxName string // Session name (stable identifier)
		Output   string
		Changed  bool
		// Cursor position captured atomically with output (only set in interactive mode)
		CursorRow     int
		CursorCol     int
		CursorVisible bool
		HasCursor     bool // True if cursor position was captured
		PaneHeight    int  // Tmux pane height for cursor offset calculation
		PaneWidth     int  // Tmux pane width for display alignment
	}

	// RenameShellDoneMsg signals shell rename operation completed
	RenameShellDoneMsg struct {
		TmuxName string // Session name (stable identifier)
		NewName  string // New display name
		Err      error  // Non-nil if rename failed
	}

	// pollShellByNameMsg triggers a poll for a specific shell's output by name.
	// Includes generation for timer leak prevention (td-83dc22).
	pollShellByNameMsg struct {
		TmuxName   string
		Generation int // Generation at time of scheduling; ignore if stale
	}

	// shellAttachAfterCreateMsg triggers attachment after shell creation
	shellAttachAfterCreateMsg struct {
		Index int // Index of the shell to attach to
	}
)

// pollShellMsg triggers a shell output poll (legacy, polls selected shell).
type pollShellMsg struct{}

// initShellSessions discovers existing shell sessions for the current project.
// Called from Init() to reconnect to sessions from previous runs.
func (p *Plugin) initShellSessions() {
	p.shells = p.discoverExistingShells()
	p.restoreShellDisplayNames()
}

func (p *Plugin) restoreShellDisplayNames() {
	if p.ctx == nil || len(p.shells) == 0 {
		return
	}

	wtState := state.GetWorkspaceState(p.ctx.WorkDir)
	if len(wtState.ShellDisplayNames) == 0 {
		return
	}

	for _, shell := range p.shells {
		if shell == nil {
			continue
		}
		if name, ok := wtState.ShellDisplayNames[shell.TmuxName]; ok && name != "" {
			shell.Name = name
		}
	}
}

// discoverExistingShells finds all existing sidecar shell sessions for this project.
func (p *Plugin) discoverExistingShells() []*ShellSession {
	projectName := filepath.Base(p.ctx.WorkDir)
	basePrefix := shellSessionPrefix + sanitizeName(projectName)

	// List all tmux sessions
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var shells []*ShellSession
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Pattern to match: sidecar-sh-{project}-{index} or legacy sidecar-sh-{project}
	indexPattern := regexp.MustCompile(`^` + regexp.QuoteMeta(basePrefix) + `(?:-(\d+))?$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := indexPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		// Determine display name based on index
		var displayName string
		if matches[1] == "" {
			// Legacy format: sidecar-sh-{project} -> "Shell 1"
			displayName = "Shell 1"
		} else {
			idx, _ := strconv.Atoi(matches[1])
			displayName = fmt.Sprintf("Shell %d", idx)
		}

		// Capture pane ID for interactive mode support
		paneID := getPaneID(line)

		shell := &ShellSession{
			Name:     displayName,
			TmuxName: line,
			Agent: &Agent{
				Type:        AgentShell,
				TmuxSession: line,
				TmuxPane:    paneID,
				OutputBuf:   NewOutputBuffer(outputBufferCap),
				StartedAt:   time.Now(), // Approximate
				Status:      AgentStatusRunning,
			},
			CreatedAt: time.Now(), // Approximate
		}
		shells = append(shells, shell)
		p.managedSessions[line] = true
	}

	return shells
}

// nextShellIndex returns the next available shell index based on existing sessions.
func (p *Plugin) nextShellIndex() int {
	projectName := filepath.Base(p.ctx.WorkDir)
	basePrefix := shellSessionPrefix + sanitizeName(projectName)

	maxIdx := 0
	indexPattern := regexp.MustCompile(`-(\d+)$`)

	for _, shell := range p.shells {
		matches := indexPattern.FindStringSubmatch(shell.TmuxName)
		if matches != nil {
			idx, _ := strconv.Atoi(matches[1])
			if idx > maxIdx {
				maxIdx = idx
			}
		} else if shell.TmuxName == basePrefix {
			if maxIdx < 1 {
				maxIdx = 1
			}
		}
	}

	return maxIdx + 1
}

// nextShellDisplayName returns the default display name for the next shell.
func (p *Plugin) nextShellDisplayName() string {
	return fmt.Sprintf("Shell %d", p.nextShellIndex())
}

// generateShellSessionName creates a unique tmux session name for a new shell.
func (p *Plugin) generateShellSessionName() string {
	projectName := filepath.Base(p.ctx.WorkDir)
	basePrefix := shellSessionPrefix + sanitizeName(projectName)
	return fmt.Sprintf("%s-%d", basePrefix, p.nextShellIndex())
}

// createNewShell creates a new shell session. If customName is non-empty, it is
// used as the display name instead of the auto-generated "Shell N".
func (p *Plugin) createNewShell(customName string) tea.Cmd {
	if !isTmuxInstalled() {
		return func() tea.Msg {
			return ShellCreatedMsg{Err: fmt.Errorf("tmux not installed: %s", getTmuxInstallInstructions())}
		}
	}

	sessionName := p.generateShellSessionName()
	displayName := strings.TrimSpace(customName)
	if displayName == "" {
		displayName = p.nextShellDisplayName()
	}
	workDir := p.ctx.WorkDir

	return func() tea.Msg {
		// Check if session already exists (shouldn't happen with unique names)
		if sessionExists(sessionName) {
			paneID := getPaneID(sessionName)
			return ShellCreatedMsg{SessionName: sessionName, DisplayName: displayName, PaneID: paneID}
		}

		// Create new detached session in project directory
		args := []string{
			"new-session",
			"-d",              // Detached
			"-s", sessionName, // Session name
			"-c", workDir, // Working directory
		}
		cmd := exec.Command("tmux", args...)
		if err := cmd.Run(); err != nil {
			return ShellCreatedMsg{
				SessionName: sessionName,
				DisplayName: displayName,
				Err:         fmt.Errorf("create shell session: %w", err),
			}
		}

		// Capture pane ID for interactive mode support
		paneID := getPaneID(sessionName)

		return ShellCreatedMsg{SessionName: sessionName, DisplayName: displayName, PaneID: paneID}
	}
}

// attachToShellByIndex attaches to a specific shell session by index.
func (p *Plugin) attachToShellByIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(p.shells) {
		return nil
	}

	shell := p.shells[idx]
	sessionName := shell.TmuxName
	displayName := shell.Name

	target := ""
	if shell.Agent != nil && shell.Agent.TmuxPane != "" {
		target = shell.Agent.TmuxPane
	} else {
		target = sessionName
	}

	c := exec.Command("tmux", "attach-session", "-t", sessionName)
	// Resize to full terminal before attaching so no dot borders appear
	return tea.Sequence(
		p.resizeForAttachCmd(target),
		tea.Printf("\nAttaching to %s. Press %s d to return to sidecar.\n", displayName, getTmuxPrefix()),
		tea.ExecProcess(c, func(err error) tea.Msg {
			return ShellDetachedMsg{Err: err}
		}),
	)
}

// ensureShellAndAttachByIndex creates shell session if needed, then attaches.
func (p *Plugin) ensureShellAndAttachByIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(p.shells) {
		return nil
	}

	shell := p.shells[idx]
	sessionName := shell.TmuxName

	// If session already exists, attach directly
	if sessionExists(sessionName) {
		return p.attachToShellByIndex(idx)
	}

	// Session doesn't exist but we have a record - recreate it
	workDir := p.ctx.WorkDir
	return tea.Sequence(
		func() tea.Msg {
			args := []string{"new-session", "-d", "-s", sessionName, "-c", workDir}
			cmd := exec.Command("tmux", args...)
			if err := cmd.Run(); err != nil {
				return ShellCreatedMsg{
					SessionName: sessionName,
					DisplayName: shell.Name,
					Err:         fmt.Errorf("recreate shell session: %w", err),
				}
			}
			// Capture pane ID for interactive mode support
			paneID := getPaneID(sessionName)
			return ShellCreatedMsg{SessionName: sessionName, DisplayName: shell.Name, PaneID: paneID}
		},
		func() tea.Msg {
			if !waitForSession(sessionName) {
				return ShellCreatedMsg{
					SessionName: sessionName,
					DisplayName: shell.Name,
					Err:         fmt.Errorf("shell session failed to become ready"),
				}
			}
			return shellAttachAfterCreateMsg{Index: idx}
		},
	)
}

// waitForSession waits for a tmux session to become available using exponential backoff.
// Returns true if session exists, false if max attempts exceeded.
func waitForSession(sessionName string) bool {
	const maxAttempts = 10
	delay := 10 * time.Millisecond

	for range maxAttempts {
		if sessionExists(sessionName) {
			return true
		}
		time.Sleep(delay)
		delay *= 2 // Exponential backoff: 10, 20, 40, 80, 160, 320, 640ms...
		if delay > 200*time.Millisecond {
			delay = 200 * time.Millisecond // Cap at 200ms per attempt
		}
	}
	return false
}

// killShellSessionByName terminates a specific shell tmux session.
func (p *Plugin) killShellSessionByName(sessionName string) tea.Cmd {
	if sessionName == "" {
		return nil
	}

	return func() tea.Msg {
		// Kill the session
		cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
		cmd.Run() // Ignore errors (session may already be dead)

		// Clean up pane cache
		globalPaneCache.remove(sessionName)

		return ShellKilledMsg{SessionName: sessionName}
	}
}

// pollShellSessionByName captures output from a specific shell session by name.
// Uses cached capture to avoid blocking subprocess calls (td-c2961e).
func (p *Plugin) pollShellSessionByName(tmuxName string) tea.Cmd {
	// Find the shell by TmuxName
	var shell *ShellSession
	for _, s := range p.shells {
		if s.TmuxName == tmuxName {
			shell = s
			break
		}
	}
	if shell == nil || shell.Agent == nil {
		return nil
	}

	// Capture references before spawning closure to avoid data races
	outputBuf := shell.Agent.OutputBuf
	maxBytes := p.tmuxCaptureMaxBytes
	selectedShell := p.getSelectedShell()
	interactiveCapture := p.viewMode == ViewModeInteractive &&
		p.interactiveState != nil &&
		p.interactiveState.Active &&
		p.shellSelected &&
		selectedShell != nil &&
		selectedShell.TmuxName == tmuxName

	// When feature is enabled, skip -J for the selected shell so content wraps
	// at the pane width (matching interactive mode). Resize inline to avoid races.
	directCapture := false
	var resizeTarget string
	var previewWidth, previewHeight int
	if !interactiveCapture && features.IsEnabled(features.TmuxInteractiveInput.Name) {
		if selectedShell != nil && selectedShell.TmuxName == tmuxName {
			directCapture = true
			previewWidth, previewHeight = p.calculatePreviewDimensions()
			resizeTarget = p.previewResizeTarget()
		}
	}

	// Capture cursor target for atomic cursor position query
	var cursorTarget string
	if interactiveCapture && p.interactiveState != nil {
		cursorTarget = p.interactiveState.TargetPane
		if cursorTarget == "" {
			cursorTarget = p.interactiveState.TargetSession
		}
	}

	return func() tea.Msg {
		// Ensure pane is at preview width before capturing (avoids race with async resize)
		if directCapture && resizeTarget != "" {
			if w, h, ok := queryPaneSize(resizeTarget); !ok || w != previewWidth || h != previewHeight {
				p.resizeTmuxPane(resizeTarget, previewWidth, previewHeight)
			}
		}

		// Use direct capture for shells (no batch), preserving wraps in interactive mode.
		// Shell sessions have prefix "sidecar-sh-" not "sidecar-ws-" so batch capture skips them.
		joinWrapped := !interactiveCapture && !directCapture
		output, err := capturePaneDirectWithJoin(tmuxName, joinWrapped)
		if err != nil {
			// Capture error - check error message to determine if session is dead
			// Avoid synchronous sessionExists() call which would block (td-c2961e)
			errStr := err.Error()
			if strings.Contains(errStr, "can't find") ||
				strings.Contains(errStr, "no server") ||
				strings.Contains(errStr, "no such session") ||
				strings.Contains(errStr, "session not found") {
				return ShellSessionDeadMsg{TmuxName: tmuxName}
			}
			// Other errors (timeout, etc.) - return empty output and schedule retry
			return ShellOutputMsg{TmuxName: tmuxName, Output: "", Changed: false}
		}

		// Capture cursor position atomically with output when in interactive mode.
		var cursorRow, cursorCol, paneHeight, paneWidth int
		var cursorVisible, hasCursor bool
		if interactiveCapture && cursorTarget != "" {
			cursorRow, cursorCol, paneHeight, paneWidth, cursorVisible, hasCursor = queryCursorPositionSync(cursorTarget)
		}

		// Trim to max bytes
		output = trimCapturedOutput(output, maxBytes)

		// Update buffer and check if content changed
		changed := outputBuf.Update(output)

		return ShellOutputMsg{
			TmuxName:      tmuxName,
			Output:        output,
			Changed:       changed,
			CursorRow:     cursorRow,
			CursorCol:     cursorCol,
			CursorVisible: cursorVisible,
			HasCursor:     hasCursor,
			PaneHeight:    paneHeight,
			PaneWidth:     paneWidth,
		}
	}
}

// scheduleShellPollByName schedules a poll for a specific shell's output by name.
// Uses generation tracking (td-83dc22) to invalidate stale timers when shells are removed.
func (p *Plugin) scheduleShellPollByName(tmuxName string, delay time.Duration) tea.Cmd {
	// Capture current generation for this shell
	gen := p.shellPollGeneration[tmuxName]
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return pollShellByNameMsg{TmuxName: tmuxName, Generation: gen}
	})
}

// findShellByName returns the shell with the given TmuxName, or nil if not found.
func (p *Plugin) findShellByName(tmuxName string) *ShellSession {
	for _, s := range p.shells {
		if s.TmuxName == tmuxName {
			return s
		}
	}
	return nil
}

// getSelectedShell returns the currently selected shell, or nil if none.
func (p *Plugin) getSelectedShell() *ShellSession {
	if !p.shellSelected || p.selectedShellIdx < 0 || p.selectedShellIdx >= len(p.shells) {
		return nil
	}
	return p.shells[p.selectedShellIdx]
}

// handleResumeConversation processes ResumeConversationMsg from conversations plugin (td-aa4136).
// Creates a new shell or worktree based on msg.Type and starts the resume flow.
func (p *Plugin) handleResumeConversation(msg ResumeConversationMsg) (*Plugin, tea.Cmd) {
	switch msg.Type {
	case "shell":
		return p, p.createShellWithResume(msg.ResumeCmd)
	case "worktree":
		return p, p.createWorktreeWithResume(msg)
	default:
		return p, nil
	}
}

// createShellWithResume creates a new shell and injects the resume command.
// The command is typed into the shell but not executed - user presses Enter.
func (p *Plugin) createShellWithResume(resumeCmd string) tea.Cmd {
	// Store pending resume command to inject after shell creation
	p.pendingResumeCmd = resumeCmd

	// Create new shell (ShellCreatedMsg will trigger command injection)
	return p.createNewShell("")
}

// sendResumeCommandToShell injects a command into the shell without executing it.
func (p *Plugin) sendResumeCommandToShell(tmuxSession string, resumeCmd string) tea.Cmd {
	if !isTmuxInstalled() {
		return nil
	}

	return func() tea.Msg {
		// Use tmux send-keys to type the command without pressing Enter
		// This lets the user review before executing
		cmd := exec.Command("tmux", "send-keys", "-t", tmuxSession, resumeCmd)
		if err := cmd.Run(); err != nil {
			return shellResumeErrorMsg{Err: err}
		}
		return shellResumeInjectedMsg{TmuxSession: tmuxSession}
	}
}

// shellResumeInjectedMsg signals that resume command was injected into shell.
type shellResumeInjectedMsg struct {
	TmuxSession string
}

// shellResumeErrorMsg signals an error injecting resume command.
type shellResumeErrorMsg struct {
	Err error
}

// worktreeResumeCreatedMsg signals that a worktree for resume was created (td-aa4136).
type worktreeResumeCreatedMsg struct {
	Worktree  *Worktree
	ResumeCmd string
	AgentType AgentType
	SkipPerms bool
	Err       error
}

// createWorktreeWithResume creates a new worktree and starts the agent with the resume command.
func (p *Plugin) createWorktreeWithResume(msg ResumeConversationMsg) tea.Cmd {
	name := msg.WorktreeName
	baseBranch := msg.BaseBranch
	agentType := msg.AgentType
	skipPerms := msg.SkipPerms
	resumeCmd := msg.ResumeCmd

	if name == "" {
		return func() tea.Msg {
			return worktreeResumeCreatedMsg{Err: fmt.Errorf("workspace name is required")}
		}
	}

	return func() tea.Msg {
		// Create the worktree (reuse doCreateWorktree)
		wt, err := p.doCreateWorktree(name, baseBranch, "", "", agentType)
		if err != nil {
			return worktreeResumeCreatedMsg{Err: err}
		}

		return worktreeResumeCreatedMsg{
			Worktree:  wt,
			ResumeCmd: resumeCmd,
			AgentType: agentType,
			SkipPerms: skipPerms,
		}
	}
}

// startAgentWithResumeCmd starts an agent in a worktree with a resume command instead of normal startup.
func (p *Plugin) startAgentWithResumeCmd(wt *Worktree, agentType AgentType, skipPerms bool, resumeCmd string) tea.Cmd {
	return func() tea.Msg {
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - should not happen for new resume worktree
			paneID := getPaneID(sessionName)
			return AgentStartedMsg{
				WorkspaceName: wt.Name,
				SessionName:   sessionName,
				PaneID:        paneID,
				AgentType:     agentType,
				Reconnected:   true,
			}
		}

		// Create new detached session with working directory
		args := []string{
			"new-session",
			"-d",              // Detached
			"-s", sessionName, // Session name
			"-c", wt.Path, // Working directory
		}

		cmd := exec.Command("tmux", args...)
		if err := cmd.Run(); err != nil {
			return AgentStartedMsg{Err: fmt.Errorf("create session: %w", err)}
		}

		// Set history limit for scrollback capture
		exec.Command("tmux", "set-option", "-t", sessionName, "history-limit",
			strconv.Itoa(tmuxHistoryLimit)).Run()

		// Set TD_SESSION_ID environment variable for td session tracking
		tdEnvCmd := fmt.Sprintf("export TD_SESSION_ID=%s", shellQuote(sessionName))
		exec.Command("tmux", "send-keys", "-t", sessionName, tdEnvCmd, "Enter").Run()

		// Apply environment isolation
		envOverrides := BuildEnvOverrides(p.ctx.WorkDir)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Send the resume command instead of the normal agent command
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, resumeCmd, "Enter")
		if err := sendCmd.Run(); err != nil {
			// Try to kill the session if we failed to start the agent
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			return AgentStartedMsg{Err: fmt.Errorf("start agent with resume: %w", err)}
		}

		// Capture pane ID for interactive mode support
		paneID := getPaneID(sessionName)

		return AgentStartedMsg{
			WorkspaceName: wt.Name,
			SessionName:   sessionName,
			PaneID:        paneID,
			AgentType:     agentType,
		}
	}
}

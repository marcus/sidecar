package workspace

import (
	"fmt"
	"os"
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
	"github.com/marcus/sidecar/internal/tty"
)

// Shell session constants
const (
	shellSessionPrefix = "sidecar-sh-" // Distinct from worktree prefix "sidecar-ws-"
)

// launchedInsideTmux is true if sidecar was started from within an existing
// tmux session (i.e. TMUX env var was set at process start). This is captured
// at package init time, before main() unsets TMUX for nested session support.
var launchedInsideTmux = os.Getenv("TMUX") != ""

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

// getTmuxDetachHint returns the key sequence hint for detaching from a nested
// tmux session. When sidecar was launched inside an existing tmux session, the
// user needs to press the prefix twice (once to reach the inner session) before
// pressing d. Otherwise a single prefix + d suffices.
func getTmuxDetachHint() string {
	prefix := getTmuxPrefix()
	if launchedInsideTmux {
		return prefix + " " + prefix + " d"
	}
	return prefix + " d"
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
		SessionName string    // tmux session name
		DisplayName string    // Display name (e.g., "Shell 1")
		PaneID      string    // tmux pane ID (e.g., "%12") for interactive mode
		Err         error     // Non-nil if creation failed
		AgentType   AgentType // td-16b2b5: Agent to start (AgentNone if plain shell)
		SkipPerms   bool      // td-16b2b5: Whether to skip permissions for agent
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

	// ShellAgentStartedMsg signals agent was started in a shell session.
	// td-21a2d8: Sent after agent command is sent to tmux.
	ShellAgentStartedMsg struct {
		TmuxName  string    // Shell's tmux session name
		AgentType AgentType // Agent type that was started
		SkipPerms bool      // Whether skip permissions was enabled
	}

	// ShellAgentErrorMsg signals agent failed to start in a shell session.
	// td-21a2d8: Sent when agent command fails to execute.
	ShellAgentErrorMsg struct {
		TmuxName string // Shell's tmux session name
		Err      error  // Error that occurred
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
// td-f88fdd: Uses manifest as source of truth, merged with tmux discovery.
func (p *Plugin) initShellSessions() {
	// Discover running tmux sessions
	tmuxSessions := p.discoverTmuxSessionNames()
	tmuxMap := make(map[string]bool)
	for _, name := range tmuxSessions {
		tmuxMap[name] = true
	}

	p.shells = nil

	// Process manifest entries (if manifest exists)
	if p.shellManifest != nil {
		for _, def := range p.shellManifest.Shells {
			isRunning := tmuxMap[def.TmuxName]
			if !isRunning {
				// Tmux session is dead - remove stale entry from manifest
				_ = p.shellManifest.RemoveShell(def.TmuxName)
				continue
			}
			shell := p.shellFromDefinition(def, isRunning)
			p.shells = append(p.shells, shell)
			p.managedSessions[def.TmuxName] = true
			delete(tmuxMap, def.TmuxName)
		}
	}

	// Add tmux sessions not in manifest (upgrade path / external creation)
	for tmuxName := range tmuxMap {
		shell := p.shellFromTmux(tmuxName)
		p.shells = append(p.shells, shell)
		p.managedSessions[tmuxName] = true
		// Save to manifest for future sessions
		if p.shellManifest != nil {
			_ = p.shellManifest.AddShell(shellToDefinition(shell))
		}
	}

	// Also restore display names from legacy state.json (migration path)
	p.restoreShellDisplayNames()
}

// shellFromDefinition creates a ShellSession from a manifest definition.
func (p *Plugin) shellFromDefinition(def ShellDefinition, isRunning bool) *ShellSession {
	shell := &ShellSession{
		Name:        def.DisplayName,
		TmuxName:    def.TmuxName,
		CreatedAt:   def.CreatedAt,
		ChosenAgent: definitionToAgentType(def.AgentType),
		SkipPerms:   def.SkipPerms,
		IsOrphaned:  !isRunning,
	}

	if isRunning {
		paneID := getPaneID(def.TmuxName)
		displayType := AgentShell
		if shell.ChosenAgent != AgentNone {
			displayType = shell.ChosenAgent
		}
		shell.Agent = &Agent{
			Type:        displayType,
			TmuxSession: def.TmuxName,
			TmuxPane:    paneID,
			OutputBuf:   NewOutputBuffer(outputBufferCap),
			StartedAt:   def.CreatedAt,
			Status:      AgentStatusRunning,
		}
	}

	return shell
}

// shellFromTmux creates a ShellSession from a discovered tmux session.
func (p *Plugin) shellFromTmux(tmuxName string) *ShellSession {
	paneID := getPaneID(tmuxName)
	displayName := p.deriveDisplayName(tmuxName)

	return &ShellSession{
		Name:     displayName,
		TmuxName: tmuxName,
		Agent: &Agent{
			Type:        AgentShell,
			TmuxSession: tmuxName,
			TmuxPane:    paneID,
			OutputBuf:   NewOutputBuffer(outputBufferCap),
			StartedAt:   time.Now(),
			Status:      AgentStatusRunning,
		},
		CreatedAt: time.Now(),
	}
}

// deriveDisplayName extracts a display name from a tmux session name.
func (p *Plugin) deriveDisplayName(tmuxName string) string {
	projectName := filepath.Base(p.ctx.WorkDir)
	basePrefix := shellSessionPrefix + sanitizeName(projectName)
	indexPattern := regexp.MustCompile(`-(\d+)$`)

	if matches := indexPattern.FindStringSubmatch(tmuxName); matches != nil {
		idx, _ := strconv.Atoi(matches[1])
		return fmt.Sprintf("Shell %d", idx)
	} else if tmuxName == basePrefix {
		return "Shell 1"
	}
	return "Shell"
}

// discoverTmuxSessionNames returns names of all sidecar shell sessions for this project.
func (p *Plugin) discoverTmuxSessionNames() []string {
	projectName := filepath.Base(p.ctx.WorkDir)
	basePrefix := shellSessionPrefix + sanitizeName(projectName)

	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result []string
	indexPattern := regexp.MustCompile(`^` + regexp.QuoteMeta(basePrefix) + `(?:-(\d+))?$`)

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if indexPattern.MatchString(line) {
			result = append(result, line)
		}
	}

	return result
}

// syncShellsFromManifest reloads the manifest and syncs the shell list.
// Called when the manifest file changes (from another sidecar instance).
func (p *Plugin) syncShellsFromManifest() tea.Cmd {
	return func() tea.Msg {
		if p.shellManifest == nil {
			return nil
		}

		// Reload manifest from disk
		newManifest, err := LoadShellManifest(p.shellManifest.Path())
		if err != nil {
			return nil
		}

		return shellManifestSyncMsg{Manifest: newManifest}
	}
}

// shellManifestSyncMsg carries the reloaded manifest for syncing.
type shellManifestSyncMsg struct {
	Manifest *ShellManifest
}

// applyManifestSync syncs the in-memory shell list with the manifest.
// Called after receiving shellManifestSyncMsg.
func (p *Plugin) applyManifestSync() {
	if p.shellManifest == nil {
		return
	}

	// Get current tmux sessions
	tmuxSessions := p.discoverTmuxSessionNames()
	tmuxMap := make(map[string]bool)
	for _, name := range tmuxSessions {
		tmuxMap[name] = true
	}

	// Build map of current shells by TmuxName
	currentShells := make(map[string]*ShellSession)
	for _, shell := range p.shells {
		currentShells[shell.TmuxName] = shell
	}

	// Track which shells exist in manifest
	manifestShells := make(map[string]bool)
	for _, def := range p.shellManifest.Shells {
		manifestShells[def.TmuxName] = true
	}

	// Process manifest entries - add new or update existing
	var newShells []*ShellSession
	for _, def := range p.shellManifest.Shells {
		isRunning := tmuxMap[def.TmuxName]

		if existing, ok := currentShells[def.TmuxName]; ok {
			// Update existing shell
			existing.Name = def.DisplayName
			existing.ChosenAgent = definitionToAgentType(def.AgentType)
			existing.SkipPerms = def.SkipPerms
			existing.IsOrphaned = !isRunning
			newShells = append(newShells, existing)
		} else {
			// New shell from manifest
			shell := p.shellFromDefinition(def, isRunning)
			newShells = append(newShells, shell)
			if isRunning {
				p.managedSessions[def.TmuxName] = true
			}
		}
	}

	// Remove shells not in manifest
	for _, shell := range p.shells {
		if !manifestShells[shell.TmuxName] {
			// Shell was deleted in another instance
			delete(p.managedSessions, shell.TmuxName)
			globalPaneCache.remove(shell.TmuxName)
			globalActiveRegistry.remove(shell.TmuxName)
		}
	}

	p.shells = newShells

	// Adjust selection if needed
	if p.shellSelected && p.selectedShellIdx >= len(p.shells) {
		if len(p.shells) > 0 {
			p.selectedShellIdx = len(p.shells) - 1
		} else if len(p.worktrees) > 0 {
			p.shellSelected = false
			p.selectedIdx = 0
		}
	}
}

func (p *Plugin) restoreShellDisplayNames() {
	if p.ctx == nil || len(p.shells) == 0 {
		return
	}

	wtState := state.GetWorkspaceState(p.ctx.ProjectRoot)
	if len(wtState.ShellDisplayNames) == 0 {
		return
	}

	// td-f88fdd: Migrate display names from state.json to manifest
	migrated := false
	for _, shell := range p.shells {
		if shell == nil {
			continue
		}
		if name, ok := wtState.ShellDisplayNames[shell.TmuxName]; ok && name != "" {
			shell.Name = name
			// If we have a manifest, update it with the migrated name
			if p.shellManifest != nil && p.shellManifest.FindShell(shell.TmuxName) != nil {
				_ = p.shellManifest.UpdateShell(shellToDefinition(shell))
				migrated = true
			}
		}
	}

	// Clear from state.json after migration to avoid re-migration
	if migrated {
		wtState.ShellDisplayNames = nil
		_ = state.SetWorkspaceState(p.ctx.ProjectRoot, wtState)
	}
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

// createShellWithAgent creates a new shell session with optional agent startup.
// td-16b2b5: Captures agent info from type selector state, creates shell, and includes
// agent info in the message so the handler can start the agent after shell creation.
func (p *Plugin) createShellWithAgent() tea.Cmd {
	// Capture state before clearing modal
	customName := p.typeSelectorNameInput.Value()
	agentType := p.typeSelectorAgentType
	skipPerms := p.typeSelectorSkipPerms

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
			return ShellCreatedMsg{
				SessionName: sessionName,
				DisplayName: displayName,
				PaneID:      paneID,
				AgentType:   agentType,
				SkipPerms:   skipPerms,
			}
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

		return ShellCreatedMsg{
			SessionName: sessionName,
			DisplayName: displayName,
			PaneID:      paneID,
			AgentType:   agentType,
			SkipPerms:   skipPerms,
		}
	}
}

// recreateOrphanedShell recreates a tmux session for an orphaned shell.
// td-f88fdd: Called when user tries to attach/interact with an orphaned shell.
func (p *Plugin) recreateOrphanedShell(idx int) tea.Cmd {
	if idx < 0 || idx >= len(p.shells) {
		return nil
	}

	shell := p.shells[idx]
	if !shell.IsOrphaned {
		return nil // Not orphaned, nothing to do
	}

	sessionName := shell.TmuxName
	workDir := p.ctx.WorkDir
	previewWidth, previewHeight := p.calculatePreviewDimensions()

	return func() tea.Msg {
		// Create new detached session
		args := []string{"new-session", "-d", "-s", sessionName, "-c", workDir}
		if previewWidth > 0 && previewHeight > 0 {
			args = append(args, "-x", strconv.Itoa(previewWidth), "-y", strconv.Itoa(previewHeight))
		}
		cmd := exec.Command("tmux", args...)
		if err := cmd.Run(); err != nil {
			return ShellCreatedMsg{
				SessionName: sessionName,
				DisplayName: shell.Name,
				Err:         fmt.Errorf("recreate shell session: %w", err),
			}
		}

		tty.SetWindowSizeManual(sessionName)

		// Capture pane ID
		paneID := getPaneID(sessionName)

		return ShellCreatedMsg{
			SessionName: sessionName,
			DisplayName: shell.Name,
			PaneID:      paneID,
			AgentType:   shell.ChosenAgent,
			SkipPerms:   shell.SkipPerms,
		}
	}
}

// startAgentInShell sends an agent command to an existing shell's tmux session.
// td-21a2d8: Called after shell is created when an agent was selected.
func (p *Plugin) startAgentInShell(tmuxName string, agentType AgentType, skipPerms bool) tea.Cmd {
	return func() tea.Msg {
		// Get the base command for this agent type
		baseCmd := AgentCommands[agentType]
		if baseCmd == "" {
			return ShellAgentErrorMsg{
				TmuxName: tmuxName,
				Err:      fmt.Errorf("unknown agent type: %s", agentType),
			}
		}

		// Add skip permissions flag if enabled
		if skipPerms {
			if flag := SkipPermissionsFlags[agentType]; flag != "" {
				baseCmd = baseCmd + " " + flag
			}
		}

		// Send the command to the shell's tmux session
		cmd := exec.Command("tmux", "send-keys", "-t", tmuxName, baseCmd, "Enter")
		if err := cmd.Run(); err != nil {
			return ShellAgentErrorMsg{
				TmuxName: tmuxName,
				Err:      fmt.Errorf("failed to start agent: %w", err),
			}
		}

		return ShellAgentStartedMsg{
			TmuxName:  tmuxName,
			AgentType: agentType,
			SkipPerms: skipPerms,
		}
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

	// Resize to full terminal before attaching so no dot borders appear
	return p.attachWithResize(target, sessionName, displayName, func(err error) tea.Msg {
		return ShellDetachedMsg{Err: err}
	})
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
	previewWidth, previewHeight := p.calculatePreviewDimensions()
	return tea.Sequence(
		func() tea.Msg {
			args := []string{"new-session", "-d", "-s", sessionName, "-c", workDir}
			if previewWidth > 0 && previewHeight > 0 {
				args = append(args, "-x", strconv.Itoa(previewWidth), "-y", strconv.Itoa(previewHeight))
			}
			cmd := exec.Command("tmux", args...)
			if err := cmd.Run(); err != nil {
				return ShellCreatedMsg{
					SessionName: sessionName,
					DisplayName: shell.Name,
					Err:         fmt.Errorf("recreate shell session: %w", err),
				}
			}
			tty.SetWindowSizeManual(sessionName)
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
		_ = cmd.Run() // Ignore errors (session may already be dead)

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
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	return func() tea.Msg {
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - should not happen for new resume worktree
			paneID := getPaneID(sessionName)
			return AgentStartedMsg{
				Epoch:         epoch,
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
			return AgentStartedMsg{Epoch: epoch, Err: fmt.Errorf("create session: %w", err)}
		}

		// Set history limit for scrollback capture
		_ = exec.Command("tmux", "set-option", "-t", sessionName, "history-limit",
			strconv.Itoa(tmuxHistoryLimit)).Run()

		// Set TD_SESSION_ID environment variable for td session tracking
		tdEnvCmd := fmt.Sprintf("export TD_SESSION_ID=%s", shellQuote(sessionName))
		_ = exec.Command("tmux", "send-keys", "-t", sessionName, tdEnvCmd, "Enter").Run()

		// Apply environment isolation
		envOverrides := BuildEnvOverrides(p.ctx.WorkDir)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Send the resume command instead of the normal agent command
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, resumeCmd, "Enter")
		if err := sendCmd.Run(); err != nil {
			// Try to kill the session if we failed to start the agent
			_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			return AgentStartedMsg{Epoch: epoch, Err: fmt.Errorf("start agent with resume: %w", err)}
		}

		// Capture pane ID for interactive mode support
		paneID := getPaneID(sessionName)

		return AgentStartedMsg{
			Epoch:         epoch,
			WorkspaceName: wt.Name,
			SessionName:   sessionName,
			PaneID:        paneID,
			AgentType:     agentType,
		}
	}
}

package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// paneCacheEntry holds cached capture output with timestamp
type paneCacheEntry struct {
	output    string
	timestamp time.Time
}

// paneCache provides thread-safe caching for tmux pane captures.
// When one poll triggers a capture, it captures ALL active sessions
// at once, so subsequent polls within the cache window get cached results.
type paneCache struct {
	mu      sync.Mutex
	entries map[string]paneCacheEntry
	ttl     time.Duration
}

type captureCoordinator struct {
	mu       sync.Mutex
	inFlight bool
	cond     *sync.Cond
}

type capturedCursor struct {
	Row     int
	Col     int
	Visible bool
	Valid   bool
	// MouseReporting mirrors tmux's #{mouse_any_flag} for the captured pane.
	// See tty.ControlSnapshot.MouseReporting for why it comes from tmux instead
	// of the capture text.
	MouseReporting bool
	capturedPaneMetadata
}

// capturedPaneMetadata is the tmux state captured atomically with the pane's
// text. PaneWidth/PaneHeight are the pane's real geometry, which need not match
// the size sidecar last requested (td-73fa86); they are populated whenever tmux
// answered, independently of Valid, which reports only that the history
// metadata parsed.
type capturedPaneMetadata struct {
	HistorySize    int
	CaptureBase    int
	PaneWidth      int
	PaneHeight     int
	PaneTitle      string
	CurrentCommand string
	Valid          bool
	// RowsJoined records that the capture was taken with -J, so its rows do not
	// correspond one for one with the grid's. It travels with the text it
	// describes all the way to tty.CaptureSnapshot, which then publishes no
	// history/pane split rather than a wrong one.
	RowsJoined bool
}

func newCaptureCoordinator() *captureCoordinator {
	cc := &captureCoordinator{}
	cc.cond = sync.NewCond(&cc.mu)
	return cc
}

// runBatch executes fn if no batch is currently running. If a batch is in-flight,
// it waits for completion and returns ran=false so callers can re-check cache.
func (c *captureCoordinator) runBatch(fn func() (map[string]string, error)) (outputs map[string]string, err error, ran bool) {
	c.mu.Lock()
	if c.inFlight {
		for c.inFlight {
			c.cond.Wait()
		}
		c.mu.Unlock()
		return nil, nil, false
	}
	c.inFlight = true
	c.mu.Unlock()

	outputs, err = fn()

	c.mu.Lock()
	c.inFlight = false
	c.cond.Broadcast()
	c.mu.Unlock()

	return outputs, err, true
}

// Global cache instance for pane captures
var globalPaneCache = &paneCache{
	entries: make(map[string]paneCacheEntry),
	ttl:     300 * time.Millisecond, // Cache valid for 300ms
}

var globalCaptureCoordinator = newCaptureCoordinator()

// activeSessionRegistry tracks sessions that have been recently polled (td-018f25).
// Used by batch capture to only capture sessions that are actively being monitored,
// avoiding unnecessary captures of idle sessions.
type activeSessionRegistry struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

// globalActiveRegistry tracks sessions with recent poll activity.
var globalActiveRegistry = &activeSessionRegistry{
	entries: make(map[string]time.Time),
	ttl:     30 * time.Second, // Consider session active if polled within 30s
}

// markActive records that a session was just polled.
func (r *activeSessionRegistry) markActive(session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[session] = time.Now()
}

// getActiveSessions returns sessions that have been polled recently.
func (r *activeSessionRegistry) getActiveSessions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var active []string
	for session, lastPoll := range r.entries {
		if now.Sub(lastPoll) < r.ttl {
			active = append(active, session)
		} else {
			// Clean up stale entry
			delete(r.entries, session)
		}
	}
	return active
}

// remove deletes a session from the registry (called when session ends).
func (r *activeSessionRegistry) remove(session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, session)
}

// get returns cached output if valid, or empty string if expired/missing
func (c *paneCache) get(session string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[session]; ok {
		if time.Since(entry.timestamp) < c.ttl {
			return entry.output, true
		}
		// Entry expired - delete it to prevent unbounded growth
		delete(c.entries, session)
	}
	return "", false
}

// setAll stores multiple session outputs at once, replacing old entries
func (c *paneCache) setAll(outputs map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	// Remove stale entries not in the new batch (prevents memory growth)
	for k := range c.entries {
		if _, exists := outputs[k]; !exists {
			delete(c.entries, k)
		}
	}
	for session, output := range outputs {
		c.entries[session] = paneCacheEntry{output: output, timestamp: now}
	}
}

// remove deletes a session from the cache (called when session ends)
func (c *paneCache) remove(session string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, session)
}

// cleanup removes all expired entries from the cache.
// Called periodically to prevent memory leaks from dead sessions.
func (c *paneCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for session, entry := range c.entries {
		if now.Sub(entry.timestamp) >= c.ttl {
			delete(c.entries, session)
		}
	}
}

// startCleanupLoop starts a background goroutine that periodically
// cleans up expired cache entries. Runs every 10 seconds.
func (c *paneCache) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c.cleanup()
		}
	}()
}

func init() {
	// Start periodic cleanup to prevent memory leaks from dead sessions
	globalPaneCache.startCleanupLoop()
}

const (
	// Tmux session prefix for sidecar-managed worktree sessions
	tmuxSessionPrefix = "sidecar-ws-"

	// Lines to capture from tmux. We only need recent output for status
	// detection and display; the same window is requested from the tty control
	// manager, so it tracks tty.DefaultScrollbackLines rather than restating it.
	// Older ranges beyond this window are fetched lazily on scroll, which is why
	// this is far below outputBufferCap.
	captureLineCount = tty.DefaultScrollbackLines

	// Hard cap on captured output size to avoid runaway memory for TUI-heavy panes.
	defaultTmuxCaptureMaxBytes = 2 * 1024 * 1024

	// Timeout for tmux capture commands to avoid blocking on hung sessions
	tmuxCaptureTimeout      = 2 * time.Second
	tmuxBatchCaptureTimeout = 3 * time.Second

	// Polling intervals - adaptive based on agent status and visibility
	// Fast (visible+focused): 200ms active, 2s idle
	// Medium (visible+unfocused): 500ms
	// Slow (not visible): 10-20s
	pollIntervalInitial          = 200 * time.Millisecond // First poll after agent starts
	pollIntervalActive           = 200 * time.Millisecond // Agent actively processing (keep fast for UX)
	pollIntervalIdle             = 2 * time.Second        // No change detected
	pollIntervalWaiting          = 2 * time.Second        // Agent waiting for user input
	pollIntervalDone             = 20 * time.Second       // Agent completed/exited
	pollIntervalBackground       = 10 * time.Second       // Output not visible, plugin focused
	pollIntervalVisibleUnfocused = 500 * time.Millisecond // Output visible but plugin not focused
	pollIntervalUnfocused        = 20 * time.Second       // Plugin not focused, output not visible
	pollIntervalThrottled        = 20 * time.Second       // Runaway session throttled (td-018f25)

	// Poll staggering to prevent simultaneous subprocess spawns
	pollStaggerMax = 400 * time.Millisecond // Max stagger offset based on worktree name hash

	// Status detection window - chars from end to check for status patterns
	// ~10 lines of 150 chars each = 1500, but we use 2048 for UTF-8 safety margin
	statusCheckBytes = 2048

	// Prompt extraction window - chars from end to search for prompts
	// ~15 lines of 150 chars each = 2250, but we use 2560 for UTF-8 safety margin
	promptCheckBytes = 2560

	// Runaway detection thresholds (td-018f25)
	// Detect sessions producing continuous output and throttle them to reduce CPU usage.
	runawayPollCount  = 20              // Number of polls to track
	runawayTimeWindow = 3 * time.Second // If 20 polls happen within this window = runaway
	runawayResetCount = 3               // Consecutive unchanged polls to reset throttle
)

// AgentStartedMsg signals an agent has been started in a worktree.
type AgentStartedMsg struct {
	Epoch         uint64 // Epoch when request was issued (for stale detection)
	WorktreeKey   string
	WorkspaceName string
	SessionName   string
	PaneID        string // tmux pane ID (e.g., "%12") for interactive mode
	AgentType     AgentType
	Reconnected   bool // True if we reconnected to an existing session
	Err           error
}

// GetEpoch implements plugin.EpochMessage.
func (m AgentStartedMsg) GetEpoch() uint64 { return m.Epoch }

// ApproveResultMsg signals the result of an approve action.
type ApproveResultMsg struct {
	WorkspaceName string
	Err           error
}

// RejectResultMsg signals the result of a reject action.
type RejectResultMsg struct {
	WorkspaceName string
	Err           error
}

// SendTextResultMsg signals the result of sending text to an agent.
type SendTextResultMsg struct {
	WorkspaceName string
	Text          string
	Err           error
}

// pollAgentMsg triggers output polling for a worktree's agent.
// Includes generation for timer leak prevention (td-83dc22).
type pollAgentMsg struct {
	WorkspaceName string
	Generation    int // Generation at time of scheduling; ignore if stale
}

// reconnectedAgentsMsg delivers reconnected agents from startup.
type reconnectedAgentsMsg struct {
	Agents []reconnectedAgent
}

type reconnectedAgent struct {
	WorktreeKey string
	Agent       *Agent
}

// RecordPollTime records a poll time for runaway detection (td-018f25).
// Should be called when an AgentOutputMsg (content changed) is received.
func (a *Agent) RecordPollTime() {
	now := time.Now()
	a.RecentPollTimes = append(a.RecentPollTimes, now)
	// Keep only the last N poll times (use copy to avoid memory leak from reslicing, td-e04a5c)
	if len(a.RecentPollTimes) > runawayPollCount {
		newSlice := make([]time.Time, runawayPollCount)
		copy(newSlice, a.RecentPollTimes[len(a.RecentPollTimes)-runawayPollCount:])
		a.RecentPollTimes = newSlice
	}
	// Reset unchanged count since content changed
	a.UnchangedPollCount = 0
}

// RecordUnchangedPoll records an unchanged poll for throttle reset (td-018f25).
// Should be called when an AgentPollUnchangedMsg is received.
func (a *Agent) RecordUnchangedPoll() {
	a.UnchangedPollCount++
	// If enough unchanged polls, reset throttle
	if a.PollsThrottled && a.UnchangedPollCount >= runawayResetCount {
		a.PollsThrottled = false
		a.RecentPollTimes = nil // Clear history
		a.UnchangedPollCount = 0
	}
}

// CheckRunaway checks if this agent should be throttled (td-018f25).
// Returns true and sets PollsThrottled if runaway condition is detected.
func (a *Agent) CheckRunaway() bool {
	if a.PollsThrottled {
		return true // Already throttled
	}
	if len(a.RecentPollTimes) < runawayPollCount {
		return false // Not enough data
	}
	// Check if runawayPollCount polls happened within runawayTimeWindow
	oldest := a.RecentPollTimes[0]
	newest := a.RecentPollTimes[len(a.RecentPollTimes)-1]
	elapsed := newest.Sub(oldest)
	if elapsed < runawayTimeWindow {
		a.PollsThrottled = true
		return true
	}
	return false
}

// StartAgent creates a tmux session and starts an agent for a worktree.
// If a session already exists, it reconnects to it instead of failing.
func (p *Plugin) StartAgent(wt *Worktree, agentType AgentType) tea.Cmd {
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	key, name, path, taskID := wt.IdentityKey(), wt.Name, wt.Path, wt.TaskID
	sessionName := worktreeTmuxSession(wt)
	mainRoot := p.ctx.ProjectRoot
	if mainRoot == "" {
		mainRoot = p.ctx.WorkDir
	}
	envOverrides := BuildEnvOverrides(mainRoot)
	agentCmd := p.getAgentCommandWithContext(agentType, wt)
	return func() tea.Msg {

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - reconnect to it instead of failing
			paneID := getPaneID(sessionName)
			return AgentStartedMsg{
				Epoch:         epoch,
				WorktreeKey:   key,
				WorkspaceName: name,
				SessionName:   sessionName,
				PaneID:        paneID,
				AgentType:     agentType,
				Reconnected:   true, // Flag that we reconnected to existing session
			}
		}

		// Create new detached session with working directory
		args := []string{
			"new-session",
			"-d",              // Detached
			"-s", sessionName, // Session name
			"-c", path, // Working directory
		}

		if err := tty.NewSession(args...); err != nil {
			return AgentStartedMsg{Epoch: epoch, Err: fmt.Errorf("create session: %w", err)}
		}

		// Set TD_SESSION_ID environment variable for td session tracking
		envCmd := fmt.Sprintf("export TD_SESSION_ID=%s", shellQuote(sessionName))
		_ = exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()

		// Apply environment isolation to prevent conflicts (GOWORK, etc.)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// If worktree has a linked task, start it in td
		if taskID != "" {
			tdStartCmd := fmt.Sprintf("td start %s", taskID)
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, tdStartCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Get the agent command with optional task context
		// Send the agent command to start it
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, agentCmd, "Enter")
		if err := sendCmd.Run(); err != nil {
			// Try to kill the session if we failed to start the agent
			_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			return AgentStartedMsg{Epoch: epoch, Err: fmt.Errorf("start agent: %w", err)}
		}

		// Capture pane ID for interactive mode support
		paneID := getPaneID(sessionName)

		return AgentStartedMsg{
			Epoch:         epoch,
			WorktreeKey:   key,
			WorkspaceName: name,
			SessionName:   sessionName,
			PaneID:        paneID,
			AgentType:     agentType,
		}
	}
}

// getAgentCommand returns the command to start an agent.
func getAgentCommand(agentType AgentType) string {
	if cmd, ok := AgentCommands[agentType]; ok {
		return cmd
	}
	return "claude" // Default to claude
}

func sanitizeAgentStartCommand(raw string) string {
	cmd := strings.TrimSpace(raw)
	if cmd == "" || strings.ContainsAny(cmd, "\r\n") {
		return ""
	}

	var cleaned strings.Builder
	cleaned.Grow(len(cmd))
	for _, r := range cmd {
		if r == '\uFFFD' || r == '\uFEFF' {
			continue
		}
		if unicode.Is(unicode.Cf, r) || unicode.IsControl(r) {
			continue
		}
		cleaned.WriteRune(r)
	}

	result := strings.TrimSpace(cleaned.String())
	if result == "" || strings.ContainsAny(result, "\r\n") {
		return ""
	}
	return result
}

func resolveConfigAgentStart(agentStart map[string]string, agentType AgentType) string {
	if len(agentStart) == 0 {
		return ""
	}

	lookupOrder := []string{string(agentType), "*", "default"}
	for _, key := range lookupOrder {
		cmd := sanitizeAgentStartCommand(agentStart[key])
		if cmd != "" {
			return cmd
		}
	}
	return ""
}

// resolveAgentBaseCommand returns the command used to launch the selected agent family.
// Precedence: worktree .sidecar-agent-start > config.plugins.workspace.agentStart > AgentCommands map.
func (p *Plugin) resolveAgentBaseCommand(worktreePath string, agentType AgentType) string {
	var configured map[string]string
	if p != nil && p.ctx != nil && p.ctx.Config != nil {
		configured = p.ctx.Config.Plugins.Workspace.AgentStart
	}
	return workspaceops.ResolveAgentCommand(worktreePath, string(agentType), configured, false)
}

// buildAgentCommand builds the agent command with optional skip permissions and task context.
// If there's task context, it writes a launcher script to avoid shell escaping issues.
func (p *Plugin) buildAgentCommand(agentType AgentType, wt *Worktree, skipPerms bool) string {
	worktreePath := ""
	if wt != nil {
		worktreePath = wt.Path
	}
	baseCmd := p.resolveAgentBaseCommand(worktreePath, agentType)

	// Apply skip permissions flag if requested
	if skipPerms {
		if flag := SkipPermissionsFlags[agentType]; flag != "" {
			baseCmd = baseCmd + " " + flag
		}
	}

	// Task-linked launch injects task context when no other prompt is supplied.
	var ctx string
	if wt != nil && wt.TaskID != "" {
		ctx = p.getTaskContext(wt.TaskID)
		if ctx == "" && wt.TaskTitle != "" {
			ctx = fmt.Sprintf("Task: %s", wt.TaskTitle)
		}
	}

	// No context - return simple command
	if ctx == "" {
		return baseCmd
	}

	// Write launcher script to avoid shell escaping issues with complex markdown
	launcherCmd, err := p.writeAgentLauncher(wt.Path, agentType, baseCmd, ctx)
	if err != nil {
		// Fall back to simple command without context on error
		return baseCmd
	}
	return launcherCmd
}

// writeAgentLauncher writes a launcher script that safely passes the prompt to the agent.
// Returns the command to execute the launcher. This avoids shell escaping issues
// with complex markdown content (backticks, newlines, quotes, etc).
func (p *Plugin) writeAgentLauncher(worktreePath string, agentType AgentType, baseCmd, prompt string) (string, error) {
	wtDir, err := projectdir.WorktreeDir(p.ctx.ProjectRoot, worktreePath)
	if err != nil {
		return "", fmt.Errorf("resolve worktree dir: %w", err)
	}
	launcherFile := filepath.Join(wtDir, "start.sh")

	// Build shell profile sourcing command.
	// This ensures tools like claude (installed via nvm) are in PATH.
	// We handle nvm explicitly since it's often lazy-loaded in shell profiles.
	shellSetup := `# Setup PATH for tools installed via nvm, homebrew, etc.
export NVM_DIR="${NVM_DIR:-$HOME/.nvm}"
[ -s "$NVM_DIR/nvm.sh" ] && source "$NVM_DIR/nvm.sh" 2>/dev/null
# Fallback: source shell profile if nvm not found
if ! command -v node &>/dev/null; then
  [ -f "$HOME/.zshrc" ] && source "$HOME/.zshrc" 2>/dev/null
  [ -f "$HOME/.bashrc" ] && source "$HOME/.bashrc" 2>/dev/null
fi
`

	// Use a heredoc with quoted delimiter to prevent ALL shell expansion.
	// This safely handles backticks, $variables, quotes, newlines, etc.
	// The prompt is embedded directly in the script, not read from a file.
	var script string
	switch agentType {
	case AgentAider:
		// aider uses --message flag
		script = fmt.Sprintf(`#!/bin/bash
%s
read -r -d '' sidecar_prompt <<'SIDECAR_PROMPT_EOF'
%s
SIDECAR_PROMPT_EOF
%s --message "$sidecar_prompt"
rm -f %q
`, shellSetup, prompt, baseCmd, launcherFile)
	case AgentOpenCode:
		// opencode uses 'run' subcommand
		script = fmt.Sprintf(`#!/bin/bash
%s
read -r -d '' sidecar_prompt <<'SIDECAR_PROMPT_EOF'
%s
SIDECAR_PROMPT_EOF
%s run "$sidecar_prompt"
rm -f %q
`, shellSetup, prompt, baseCmd, launcherFile)
	case AgentAmp:
		// amp requires piping via stdin, does not accept positional args
		script = fmt.Sprintf(`#!/bin/bash
%s
cat <<'SIDECAR_PROMPT_EOF' | %s
%s
SIDECAR_PROMPT_EOF
rm -f %q
`, shellSetup, baseCmd, prompt, launcherFile)
	default:
		// Most agents (claude, codex, antigravity, cursor) take prompt as positional argument
		script = fmt.Sprintf(`#!/bin/bash
%s
read -r -d '' sidecar_prompt <<'SIDECAR_PROMPT_EOF'
%s
SIDECAR_PROMPT_EOF
%s "$sidecar_prompt"
rm -f %q
`, shellSetup, prompt, baseCmd, launcherFile)
	}

	if err := os.WriteFile(launcherFile, []byte(script), 0700); err != nil {
		return "", err
	}

	return "bash " + shellQuote(launcherFile), nil
}

// getAgentCommandWithContext returns the agent command with optional task context (legacy, no skip perms).
func (p *Plugin) getAgentCommandWithContext(agentType AgentType, wt *Worktree) string {
	return p.buildAgentCommand(agentType, wt, false)
}

// StartAgentWithOptions creates a tmux session and starts an agent with options.
// If a session already exists, it reconnects to it instead of failing.
func (p *Plugin) StartAgentWithOptions(wt *Worktree, agentType AgentType, skipPerms bool) tea.Cmd {
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	key, name, path, taskID := wt.IdentityKey(), wt.Name, wt.Path, wt.TaskID
	sessionName := worktreeTmuxSession(wt)
	mainRoot := p.ctx.ProjectRoot
	if mainRoot == "" {
		mainRoot = p.ctx.WorkDir
	}
	envOverrides := workspaceops.BuildEnvOverrides(mainRoot)
	agentCmd := p.buildAgentCommand(agentType, wt, skipPerms)
	return func() tea.Msg {
		result, err := workspaceops.LaunchWorktreeSession(p.operationCtx, workspaceops.AgentLaunchSpec{
			SessionName: sessionName, WorkDir: path, AgentCommand: agentCmd, TaskID: taskID,
			Env: envOverrides, StartAgent: true,
		})
		if err != nil {
			return AgentStartedMsg{Epoch: epoch, Err: err}
		}
		return AgentStartedMsg{
			Epoch:         epoch,
			WorktreeKey:   key,
			WorkspaceName: name,
			SessionName:   sessionName,
			PaneID:        result.PaneID,
			AgentType:     agentType,
			Reconnected:   result.Reconnected,
		}
	}
}

// AttachToWorktreeDir creates a tmux session in the worktree directory and attaches to it.
func (p *Plugin) AttachToWorktreeDir(wt *Worktree) tea.Cmd {
	if !fullTmuxAttachEnabled() {
		return nil
	}
	sessionName := worktreeTmuxSession(wt)

	// Check if session already exists
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if checkCmd.Run() != nil {
		// Session doesn't exist, create it
		args := []string{
			"new-session",
			"-d",              // Detached
			"-s", sessionName, // Session name
			"-c", wt.Path, // Working directory
		}
		if err := tty.NewSession(args...); err != nil {
			return func() tea.Msg {
				return TmuxAttachFinishedMsg{WorkspaceName: wt.Name, Err: fmt.Errorf("create session: %w", err)}
			}
		}

		// Track as managed session
		p.managedSessions[sessionName] = true
	}

	// Attach to the session - resize to full terminal first so no dot borders appear
	return p.attachWithResize(sessionName, sessionName, wt.Name, func(err error) tea.Msg {
		return TmuxAttachFinishedMsg{WorkspaceName: wt.Name, Err: err}
	})
}

// getTaskContext fetches task title and description for agent context.
func (p *Plugin) getTaskContext(taskID string) string {
	// Guard against nil context in tests
	var workDir string
	if p.ctx != nil {
		workDir = p.ctx.WorkDir
	}

	cmd := exec.Command("td", "show", taskID, "--json")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	var task struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(output, &task); err != nil {
		return ""
	}

	if task.Description != "" {
		return fmt.Sprintf("Task: %s\n\n%s", task.Title, task.Description)
	}
	return fmt.Sprintf("Task: %s", task.Title)
}

// sanitizeName cleans a name for use in tmux session names.
// tmux session names can't contain periods or colons.
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}

// worktreeSessionSuffix is the stable tmux suffix for a worktree. Display
// names are user-editable; the git directory slug is not.
func worktreeSessionSuffix(wt *Worktree) string {
	if wt == nil {
		return ""
	}
	if wt.Path != "" {
		return sanitizeName(filepath.Base(wt.Path))
	}
	return sanitizeName(wt.Name)
}

func worktreeTmuxSession(wt *Worktree) string {
	return tmuxSessionPrefix + worktreeSessionSuffix(wt)
}

// getPaneID retrieves the tmux pane ID for a session.
// Returns pane IDs like "%12" which are globally unique and stable.
// Uses caching to avoid subprocess calls (pane IDs rarely change) (td-c2961e).
func getPaneID(sessionName string) string {
	// Check cache first
	if paneID, ok := globalPaneIDCache.get(sessionName); ok {
		return paneID
	}
	// One tmux call, in workspaceops, so a global caller asking the same
	// question does not grow a second copy of it. The cache stays here: it is
	// this plugin's optimisation, not part of what the question means.
	paneID := workspaceops.PaneID(sessionName)
	if paneID != "" {
		globalPaneIDCache.set(sessionName, paneID)
	}
	return paneID
}

// staggerOffset returns a consistent stagger offset for a worktree name.
// This spreads poll timings across worktrees to prevent CPU spikes.
func staggerOffset(name string) time.Duration {
	// Simple hash: sum of bytes mod max stagger
	var hash uint32
	for i := 0; i < len(name); i++ {
		hash = hash*31 + uint32(name[i])
	}
	return time.Duration(hash%uint32(pollStaggerMax/time.Millisecond)) * time.Millisecond
}

// scheduleAgentPoll returns a command that schedules a poll after delay.
// Adds stagger offset based on worktree name to prevent simultaneous polls.
// Uses generation tracking (td-83dc22) to invalidate stale timers when worktrees are removed.
func (p *Plugin) scheduleAgentPoll(worktreeName string, delay time.Duration) tea.Cmd {
	// This is the semantic activity cadence even while tty.Model owns display.
	// It intentionally continues to observe provider files and tmux evidence;
	// Update keeps its capture from overwriting the model-owned buffer.
	stagger := staggerOffset(worktreeName)
	return p.pollScheduler.Schedule(agentPollKey(worktreeName), delay+stagger, func(gen int) tea.Msg {
		return pollAgentMsg{WorkspaceName: worktreeName, Generation: gen}
	})
}

// scheduleInteractivePoll schedules a poll without stagger for the active interactive session (td-8856c9).
// Stagger exists to spread polls across multiple worktrees, but the selected interactive worktree
// needs minimal latency. Uses the same generation tracking as scheduleAgentPoll.
func (p *Plugin) scheduleInteractivePoll(worktreeName string, delay time.Duration) tea.Cmd {
	return p.pollScheduler.Schedule(agentPollKey(worktreeName), delay, func(gen int) tea.Msg {
		return pollAgentMsg{WorkspaceName: worktreeName, Generation: gen}
	})
}

// AgentPollUnchangedMsg signals content unchanged, schedule next poll.
type AgentPollUnchangedMsg struct {
	WorkspaceName string
	Generation    int
	AgentType     AgentType // Live provider inferred from the captured pane
	Output        string
	CurrentStatus WorktreeStatus // Status including session file re-check
	WaitingFor    string         // Prompt text if waiting
	// Cursor position captured atomically (even when content unchanged)
	CursorRow     int
	CursorCol     int
	CursorVisible bool
	HasCursor     bool
	PaneHeight    int // Tmux pane height for cursor offset calculation
	PaneWidth     int // Tmux pane width for display alignment
	HistorySize   int
	CaptureBase   int
	HasHistory    bool
	// RowsJoined says the capture was taken with -J, so it carries no usable
	// history/pane split.
	RowsJoined bool
	// MouseReporting is tmux's #{mouse_any_flag} for the pane. Only meaningful
	// when HasCursor is set.
	MouseReporting bool
	Activity       agentactivity.Result
	CapturedAt     time.Time
	PaneTitle      string
	CurrentCommand string
}

// handlePollAgent captures output from a tmux session asynchronously.
// Uses a goroutine to avoid blocking the UI thread on tmux subprocess calls (td-c2961e).
func (p *Plugin) handlePollAgent(worktreeName string, generation int) tea.Cmd {
	wt := p.findWorktree(worktreeName)
	if wt == nil || wt.Agent == nil {
		return func() tea.Msg {
			return AgentStoppedMsg{WorkspaceName: worktreeName, Generation: generation}
		}
	}

	// Capture session name and worktree path before spawning goroutine
	sessionName := wt.Agent.TmuxSession
	wtPath := wt.Path
	agentType := wt.Agent.Type
	maxBytes := p.tmuxCaptureMaxBytes
	outputBuf := wt.Agent.OutputBuf
	currentStatus := wt.Status
	ctx := p.ctx

	// Use non-joined capture when interactive mode is active for this worktree
	// to preserve tmux line wrapping for cursor positioning (td-c7dd1e).
	interactiveCapture := p.viewMode == ViewModeInteractive &&
		p.interactiveState != nil &&
		p.interactiveState.Active &&
		!p.interactiveState.TermPanel &&
		!p.selectingShell()
	if interactiveCapture {
		if selected := p.selectedWorktree(); selected == nil || selected.IdentityKey() != worktreeName {
			interactiveCapture = false
		}
	}
	if interactiveCapture {
		if remaining, scrolling := p.interactiveScrollDelay(); scrolling {
			return p.scheduleInteractivePoll(worktreeName, remaining)
		}
	}

	// The selected worktree gets a direct metadata capture so its history can
	// be addressed lazily. Interactive input additionally preserves native
	// wrapping and resizes the pane to the preview dimensions.
	directCapture := false
	joinWrapped := !features.IsEnabled(features.TmuxInteractiveInput.Name)
	var resizeTarget string
	var previewWidth, previewHeight int
	if !interactiveCapture {
		if selected := p.selectedWorktree(); selected != nil && selected.IdentityKey() == worktreeName {
			directCapture = true
			if features.IsEnabled(features.TmuxInteractiveInput.Name) {
				if p.termPanelVisible {
					previewWidth, previewHeight = p.calculateAgentPaneDimensions()
				} else {
					previewWidth, previewHeight = p.calculatePreviewDimensions()
				}
				previewWidth = p.terminalContentWidth(previewWidth)
				resizeTarget = p.previewResizeTarget()
			}
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

	// Return a tea.Cmd that spawns a goroutine for async capture
	return func() tea.Msg {
		if ctx != nil {
			traceTerminalCapture(ctx.Logger, "workspace", "agent", "semantic_activity", generation)
		}
		// Ensure pane is at preview width before capturing (avoids race with async resize)
		if directCapture && resizeTarget != "" {
			if w, h, ok := tty.QueryPaneSize(resizeTarget); !ok || w != previewWidth || h != previewHeight {
				tty.ResizeTmuxPane(resizeTarget, previewWidth, previewHeight)
			} else {
				// Already the right size; still tick the geometry lease so a
				// settled owner does not go stale (td-ee222a).
				tty.TouchGeometryLease(resizeTarget)
			}
		}

		var output string
		var err error
		var cursor capturedCursor
		var capture capturedPaneMetadata
		if interactiveCapture && cursorTarget != "" {
			output, cursor, err = capturePaneDirectWithJoinAndCursor(sessionName, cursorTarget, false)
			capture = cursor.capturedPaneMetadata
		} else if interactiveCapture || directCapture {
			output, capture, err = capturePaneDirectWithJoinMetadata(sessionName, joinWrapped)
		} else {
			output, capture, err = capturePaneWithMetadata(sessionName)
		}
		if err != nil {
			// Session may have been killed
			if strings.Contains(err.Error(), "can't find") ||
				strings.Contains(err.Error(), "no server") {
				return AgentStoppedMsg{WorkspaceName: worktreeName, Generation: generation}
			}
			// Schedule retry on other errors (with delay to prevent busy-loop)
			time.Sleep(pollIntervalActive)
			return pollAgentMsg{WorkspaceName: worktreeName, Generation: generation}
		}

		var removedRows int
		output, removedRows = trimCapturedOutputRows(output, maxBytes)
		if capture.Valid {
			capture.CaptureBase += removedRows
		}

		// Use hash-based change detection to skip processing if content unchanged
		outputChanged := outputBuf == nil || (capture.Valid &&
			outputBuf.WouldChangeSnapshot(output, capture.CaptureBase)) ||
			(!capture.Valid && outputBuf.WouldChange(output))

		capturedAt := time.Now()
		observation := agentactivity.Observation{
			Screen: output, PaneTitle: capture.PaneTitle,
			CurrentCommand: capture.CurrentCommand, CapturedAt: capturedAt,
		}
		observedAgentType := AgentType(agentactivity.Identify(observation))
		if observedAgentType == "" {
			observedAgentType = agentType
		}
		activity := agentactivity.Result{}
		if supportsAgentActivity(observedAgentType) {
			observation.Agent = string(observedAgentType)
			activity = agentactivity.Detect(observation)
		}

		// Detect status. Both detectors run; each is authoritative for what it's good at (td-2fca7d):
		//   - tmux patterns: thinking, done, error (high-signal, session files can't detect these)
		//   - session files: active vs waiting (reliable, tmux patterns are noisy for this)
		// Session file detection ALWAYS runs (even when output unchanged) because the agent
		// may finish while tmux output stays the same (td-2fca7d v8).
		status := currentStatus
		waitingFor := ""
		if !supportsAgentActivity(observedAgentType) && outputChanged {
			// Tmux pattern detection only when output changes (same output = same patterns).
			status = detectStatus(output)
			if status == StatusWaiting {
				waitingFor = extractPrompt(output)
			}
		}
		// Session file check runs every poll — mtime changes independently of tmux output.
		// Only override active/waiting; preserve tmux-detected thinking/done/error.
		if !supportsAgentActivity(observedAgentType) && (status == StatusActive || status == StatusWaiting) {
			if sessionStatus, ok := detectAgentSessionStatus(observedAgentType, wtPath); ok {
				prevStatus := status
				status = sessionStatus
				if status == StatusWaiting {
					waitingFor = extractPrompt(output)
					if waitingFor == "" {
						waitingFor = "Waiting for input"
					}
				} else {
					waitingFor = ""
				}
				slog.Debug("status: session file override", "worktree", worktreeName, "prev", prevStatus, "session", sessionStatus)
			} else {
				slog.Debug("status: no session file, using tmux", "worktree", worktreeName, "status", status, "agent", observedAgentType)
			}
		}

		if !outputChanged {
			return AgentPollUnchangedMsg{
				WorkspaceName:  worktreeName,
				Generation:     generation,
				AgentType:      observedAgentType,
				Output:         output,
				CurrentStatus:  status,
				WaitingFor:     waitingFor,
				CursorRow:      cursor.Row,
				CursorCol:      cursor.Col,
				CursorVisible:  cursor.Visible,
				HasCursor:      cursor.Valid,
				PaneHeight:     capture.PaneHeight,
				PaneWidth:      capture.PaneWidth,
				HistorySize:    capture.HistorySize,
				CaptureBase:    capture.CaptureBase,
				HasHistory:     capture.Valid,
				RowsJoined:     capture.RowsJoined,
				MouseReporting: cursor.MouseReporting,
				Activity:       activity,
				CapturedAt:     capturedAt,
				PaneTitle:      capture.PaneTitle,
				CurrentCommand: capture.CurrentCommand,
			}
		}

		return AgentOutputMsg{
			WorkspaceName:  worktreeName,
			Generation:     generation,
			AgentType:      observedAgentType,
			Output:         output,
			Status:         status,
			WaitingFor:     waitingFor,
			CursorRow:      cursor.Row,
			CursorCol:      cursor.Col,
			CursorVisible:  cursor.Visible,
			HasCursor:      cursor.Valid,
			PaneHeight:     capture.PaneHeight,
			PaneWidth:      capture.PaneWidth,
			HistorySize:    capture.HistorySize,
			CaptureBase:    capture.CaptureBase,
			HasHistory:     capture.Valid,
			RowsJoined:     capture.RowsJoined,
			MouseReporting: cursor.MouseReporting,
			Activity:       activity,
			CapturedAt:     capturedAt,
			PaneTitle:      capture.PaneTitle,
			CurrentCommand: capture.CurrentCommand,
		}
	}
}

// capturePaneWithMetadata captures the last N lines of a tmux pane.
// Uses caching to avoid redundant subprocess calls when multiple worktrees poll simultaneously.
// On cache miss, captures active sessions at once to populate cache for concurrent polls.
// Only captures sessions that have been recently polled (td-018f25).
func capturePaneWithMetadata(sessionName string) (string, capturedPaneMetadata, error) {
	// Mark this session as active (td-018f25)
	globalActiveRegistry.markActive(sessionName)

	// Check cache first
	if output, ok := globalPaneCache.get(sessionName); ok {
		paneOutput, metadata := splitCaptureEnvelope(output)
		return paneOutput, metadata, nil
	}

	// Cache miss - batch capture active sidecar sessions (singleflight)
	outputs, err, ran := globalCaptureCoordinator.runBatch(batchCaptureActiveSessions)
	if !ran {
		// Another goroutine captured; re-check cache
		if output, ok := globalPaneCache.get(sessionName); ok {
			paneOutput, metadata := splitCaptureEnvelope(output)
			return paneOutput, metadata, nil
		}
		return capturePaneDirectWithJoinMetadata(sessionName, !features.IsEnabled(features.TmuxInteractiveInput.Name))
	}
	if err != nil {
		// Fall back to single capture on batch error
		return capturePaneDirectWithJoinMetadata(sessionName, !features.IsEnabled(features.TmuxInteractiveInput.Name))
	}

	// Cache all results from batch
	globalPaneCache.setAll(outputs)

	// Return requested session's output
	if output, ok := outputs[sessionName]; ok {
		paneOutput, metadata := splitCaptureEnvelope(output)
		return paneOutput, metadata, nil
	}

	// Session not in batch results - try direct capture
	return capturePaneDirectWithJoinMetadata(sessionName, !features.IsEnabled(features.TmuxInteractiveInput.Name))
}

// capturePaneDirectWithJoinMetadata captures the live tail and the tmux
// history size in one argv-only command chain.
func capturePaneDirectWithJoinMetadata(sessionName string, joinWrapped bool) (string, capturedPaneMetadata, error) {
	args := []string{"display-message", "-t", sessionName, "-p", "#{history_size},#{pane_width},#{pane_height},#{pane_current_command},#{pane_title}", ";"}
	args = append(args, capturePaneArgs(sessionName, joinWrapped)...)
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCaptureTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", capturedPaneMetadata{}, fmt.Errorf("capture-pane: timeout after %s", tmuxCaptureTimeout)
	}
	if err != nil {
		return "", capturedPaneMetadata{}, fmt.Errorf("capture-pane: %w", err)
	}
	header, paneOutput, found := strings.Cut(string(output), "\n")
	if !found {
		return "", capturedPaneMetadata{}, fmt.Errorf("capture-pane: missing history metadata")
	}
	fields := strings.Split(strings.TrimSpace(header), ",")
	historySize, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil || historySize < 0 {
		return "", capturedPaneMetadata{}, fmt.Errorf("capture-pane: invalid history size %q", header)
	}
	metadata := capturedPaneMetadata{
		HistorySize: historySize,
		CaptureBase: max(historySize-captureLineCount, 0),
		Valid:       true,
		RowsJoined:  joinWrapped,
	}
	if len(fields) >= 3 {
		width, errWidth := strconv.Atoi(strings.TrimSpace(fields[1]))
		height, errHeight := strconv.Atoi(strings.TrimSpace(fields[2]))
		if errWidth == nil && errHeight == nil {
			metadata.PaneWidth = width
			metadata.PaneHeight = height
		}
	}
	if len(fields) >= 5 {
		metadata.CurrentCommand = strings.TrimSpace(fields[3])
		metadata.PaneTitle = strings.Join(fields[4:], ",")
	}
	return paneOutput, metadata, nil
}

// capturePaneEvidence observes only semantic tmux metadata. Model-owned plain
// shells use this cadence to notice a newly launched agent without reintroducing
// capture-pane as their steady presentation renderer.
func capturePaneEvidence(target string) (capturedPaneMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCaptureTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p",
		"#{pane_width},#{pane_height},#{pane_current_command},#{pane_title}").Output()
	if ctx.Err() == context.DeadlineExceeded {
		return capturedPaneMetadata{}, fmt.Errorf("pane evidence: timeout after %s", tmuxCaptureTimeout)
	}
	if err != nil {
		return capturedPaneMetadata{}, fmt.Errorf("pane evidence: %w", err)
	}
	fields := strings.Split(strings.TrimSpace(string(output)), ",")
	if len(fields) < 4 {
		return capturedPaneMetadata{}, fmt.Errorf("pane evidence: invalid metadata %q", output)
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(fields[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(fields[1]))
	if errWidth != nil || errHeight != nil {
		return capturedPaneMetadata{}, fmt.Errorf("pane evidence: invalid geometry %q", output)
	}
	return capturedPaneMetadata{
		PaneWidth: width, PaneHeight: height,
		CurrentCommand: strings.TrimSpace(fields[2]), PaneTitle: strings.Join(fields[3:], ","),
	}, nil
}

func capturePaneArgs(sessionName string, joinWrapped bool) []string {
	args := []string{"capture-pane", "-p", "-e"}
	if joinWrapped {
		args = append(args, "-J")
	}
	return append(args, "-S", fmt.Sprintf("-%d", captureLineCount), "-t", sessionName)
}

// capturePaneDirectWithJoinAndCursor captures cursor metadata and pane output
// in one tmux process. The command separator is an argv element, not shell
// syntax, so target names are never interpreted by a shell.
func capturePaneDirectWithJoinAndCursor(sessionName, cursorTarget string, joinWrapped bool) (string, capturedCursor, error) {
	args := capturePaneWithCursorArgs(sessionName, cursorTarget, joinWrapped)

	ctx, cancel := context.WithTimeout(context.Background(), tmuxCaptureTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", capturedCursor{}, fmt.Errorf("capture-pane: timeout after %s", tmuxCaptureTimeout)
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", capturedCursor{}, fmt.Errorf("capture-pane: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", capturedCursor{}, fmt.Errorf("capture-pane: %w", err)
	}
	header, paneOutput, found := strings.Cut(string(output), "\n")
	if !found {
		return "", capturedCursor{}, fmt.Errorf("capture-pane: missing cursor metadata")
	}
	cursor := parseCapturedCursor(header)
	cursor.RowsJoined = joinWrapped
	return paneOutput, cursor, nil
}

func capturePaneWithCursorArgs(sessionName, cursorTarget string, joinWrapped bool) []string {
	args := []string{
		"display-message", "-t", cursorTarget, "-p",
		"#{cursor_x},#{cursor_y},#{cursor_flag},#{pane_height},#{pane_width},#{history_size},#{mouse_any_flag},#{pane_current_command},#{pane_title}",
		";",
	}
	args = append(args, capturePaneArgs(sessionName, joinWrapped)...)
	return args
}

func parseCapturedCursor(header string) capturedCursor {
	parts := strings.Split(strings.TrimSpace(header), ",")
	if len(parts) < 5 {
		return capturedCursor{}
	}
	col, errCol := strconv.Atoi(parts[0])
	row, errRow := strconv.Atoi(parts[1])
	paneHeight, errHeight := strconv.Atoi(parts[3])
	paneWidth, errWidth := strconv.Atoi(parts[4])
	if errCol != nil || errRow != nil || errHeight != nil || errWidth != nil {
		return capturedCursor{}
	}
	cursor := capturedCursor{
		Row:     row,
		Col:     col,
		Visible: parts[2] != "0",
		Valid:   true,
	}
	cursor.PaneHeight = paneHeight
	cursor.PaneWidth = paneWidth
	if len(parts) >= 6 {
		if historySize, err := strconv.Atoi(parts[5]); err == nil && historySize >= 0 {
			cursor.HistorySize = historySize
			cursor.CaptureBase = max(historySize-captureLineCount, 0)
			cursor.capturedPaneMetadata.Valid = true
		}
	}
	if len(parts) >= 7 {
		cursor.MouseReporting = parts[6] != "0" && parts[6] != ""
	}
	if len(parts) >= 9 {
		cursor.CurrentCommand = strings.TrimSpace(parts[7])
		cursor.PaneTitle = strings.Join(parts[8:], ",")
	}
	return cursor
}

// batchCaptureActiveSessions captures only recently-polled sidecar sessions (td-018f25).
// Returns map of session name to output.
// If there are 0-1 active sessions, returns empty map to signal caller should use direct capture.
func batchCaptureActiveSessions() (map[string]string, error) {
	// Get list of recently-polled sessions
	activeSessions := globalActiveRegistry.getActiveSessions()

	// If only 0-1 active sessions, skip batch capture overhead
	// Let caller use direct capture instead
	if len(activeSessions) <= 1 {
		return nil, nil
	}

	// When tmux_interactive_input is enabled, panes are resized to match preview width,
	// so skip -J to preserve tmux's native wrapping (matches interactive mode rendering).
	joinWrapped := !features.IsEnabled(features.TmuxInteractiveInput.Name)
	sort.Strings(activeSessions)
	nonce, err := newCaptureNonce()
	if err != nil {
		return nil, fmt.Errorf("batch capture nonce: %w", err)
	}
	args := buildBatchCaptureArgs(activeSessions, nonce, joinWrapped)

	ctx, cancel := context.WithTimeout(context.Background(), tmuxBatchCaptureTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("batch capture: timeout after %s", tmuxBatchCaptureTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("batch capture: %w", err)
	}

	return parseBatchCaptureOutput(string(output), activeSessions, nonce), nil
}

func newCaptureNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func batchCaptureMarker(nonce string, index int) string {
	return fmt.Sprintf("===SIDECAR_CAPTURE:%s:%d===", nonce, index)
}

const captureMetadataSeparator = "\x1f"

func batchCaptureMetadataMarker(nonce string, index int) string {
	return batchCaptureMarker(nonce, index) + captureMetadataSeparator +
		"#{pane_current_command}" + captureMetadataSeparator + "#{pane_title}"
}

func buildBatchCaptureArgs(sessions []string, nonce string, joinWrapped bool) []string {
	var args []string
	for i, session := range sessions {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args,
			"display-message", "-p", "-t", session, batchCaptureMetadataMarker(nonce, i),
			";",
		)
		args = append(args, capturePaneArgs(session, joinWrapped)...)
	}
	return args
}

func parseBatchCaptureOutput(output string, sessions []string, nonce string) map[string]string {
	results := make(map[string]string, len(sessions))
	for i, session := range sessions {
		marker := batchCaptureMarker(nonce, i)
		start := strings.Index(output, marker)
		if start < 0 {
			continue
		}
		lineEnd := strings.IndexByte(output[start:], '\n')
		if lineEnd < 0 {
			continue
		}
		metadataLine := output[start : start+lineEnd]
		start += lineEnd + 1
		end := len(output)
		if i+1 < len(sessions) {
			if next := strings.Index(output[start:], batchCaptureMarker(nonce, i+1)); next >= 0 {
				end = start + next
			}
		}
		if strings.Contains(metadataLine, captureMetadataSeparator) {
			results[session] = strings.Clone(metadataLine + "\n" + output[start:end])
		} else {
			results[session] = strings.Clone(output[start:end])
		}
	}
	return results
}

func splitCaptureEnvelope(output string) (string, capturedPaneMetadata) {
	header, paneOutput, found := strings.Cut(output, "\n")
	if !found {
		return output, capturedPaneMetadata{}
	}
	parts := strings.SplitN(header, captureMetadataSeparator, 3)
	if len(parts) != 3 {
		return output, capturedPaneMetadata{}
	}
	return paneOutput, capturedPaneMetadata{CurrentCommand: parts[1], PaneTitle: parts[2]}
}

// trimCapturedOutputRows applies the byte cap only at a complete line
// boundary and reports how many absolute rows were removed from the front.
// A single oversized row is preserved intact rather than returning a partial
// row whose pane coordinate cannot be represented.
func trimCapturedOutputRows(output string, maxBytes int) (string, int) {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output, 0
	}
	start := len(output) - maxBytes
	for start < len(output) && !utf8.RuneStart(output[start]) {
		start++
	}
	// The cap can land exactly after a newline. In that case the suffix already
	// starts at a valid row and must not lose one more line.
	if start > 0 && output[start-1] == '\n' {
		return output[start:], strings.Count(output[:start], "\n")
	}

	rowStart := strings.LastIndexByte(output[:start], '\n') + 1
	rowEnd := len(output)
	if nl := strings.IndexByte(output[start:], '\n'); nl >= 0 {
		rowEnd = start + nl + 1
	}
	rowBytes := rowEnd - rowStart
	if rowBytes > maxBytes || rowEnd == len(output) {
		// The containing row itself exceeds the cap (or is the only remaining
		// final row). Preserve it intact, but discard complete prefix rows.
		return output[rowStart:], strings.Count(output[:rowStart], "\n")
	}
	// The cutoff is inside an ordinary row; drop that partial row and retain
	// the newest complete rows after it.
	return output[rowEnd:], strings.Count(output[:rowEnd], "\n")
}

// tailUTF8Safe returns the last n bytes of s, adjusted to not split UTF-8 chars.
// If the slice would split a multi-byte character, it advances to the next valid
// UTF-8 boundary (returning slightly fewer than n bytes).
func tailUTF8Safe(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	// Advance to next valid UTF-8 start byte (max 3 bytes forward for 4-byte chars)
	for i := 0; i < 3 && start < len(s); i++ {
		if utf8.RuneStart(s[start]) {
			break
		}
		start++
	}
	return s[start:]
}

// detectStatus determines agent status from captured output.
// This is the tmux-based fallback for agents without session file support (td-2fca7d).
// For supported agents (Claude, Codex, Antigravity, OpenCode), session file analysis runs
// first in handlePollAgent and is more reliable than tmux pattern matching.
func detectStatus(output string) WorktreeStatus {
	// Check tail of output for status patterns (avoids splitting entire string)
	checkText := tailUTF8Safe(output, statusCheckBytes)
	textLower := strings.ToLower(checkText)

	// Waiting patterns — only check the last few lines of output (td-2fca7d).
	// A prompt is only relevant if it's at the bottom of the screen (the agent is
	// actually waiting right now). Checking 2048 bytes of scrollback history caused
	// false positives from old prompts and shell prompt characters like "❯".
	waitingPatterns := []string{
		"[y/n]",       // Claude Code permission prompt
		"(y/n)",       // Aider style
		"allow edit",  // Claude Code file edit
		"allow bash",  // Claude Code bash command
		"press enter", // Continue prompt
		"continue?",
		"approve",
		"confirm",
		"do you want", // Common prompt
	}

	lastLines := extractLastNLines(checkText, 5)
	lastLinesLower := strings.ToLower(lastLines)
	for _, pattern := range waitingPatterns {
		if strings.Contains(lastLinesLower, pattern) {
			return StatusWaiting
		}
	}

	// Thinking patterns (agent is processing) - check after waiting
	// Only report thinking if we have an unclosed thinking tag
	thinkingTags := []struct {
		open  string
		close string
	}{
		{"<thinking>", "</thinking>"},
		{"<internal_monologue>", "</internal_monologue>"},
	}
	for _, tag := range thinkingTags {
		openIdx := strings.LastIndex(textLower, tag.open)
		if openIdx >= 0 {
			closeIdx := strings.LastIndex(textLower, tag.close)
			// Only thinking if open tag is after close tag (or no close tag)
			if closeIdx < openIdx {
				return StatusThinking
			}
		}
	}
	// Generic thinking indicators (no close tag to check)
	if strings.Contains(textLower, "thinking...") || strings.Contains(textLower, "reasoning about") {
		return StatusThinking
	}

	// Done patterns (agent completed)
	donePatterns := []string{
		"task completed",
		"all done",
		"finished",
		"exited with code 0",
		"goodbye",
	}

	for _, pattern := range donePatterns {
		if strings.Contains(textLower, pattern) {
			return StatusDone
		}
	}

	// Error patterns
	errorPatterns := []string{
		"error:",
		"failed",
		"exited with code 1",
		"panic:",
		"exception:",
		"traceback",
	}

	for _, pattern := range errorPatterns {
		if strings.Contains(textLower, pattern) {
			return StatusError
		}
	}

	// Default: active if we have output
	return StatusActive
}

// extractLastNLines returns the last n non-empty lines of text.
// Used by detectStatus to restrict waiting pattern matching to the bottom of the terminal.
func extractLastNLines(text string, n int) string {
	// Work backwards from the end to find the last n lines
	end := len(text)
	// Skip trailing whitespace/newlines
	for end > 0 && (text[end-1] == '\n' || text[end-1] == '\r' || text[end-1] == ' ') {
		end--
	}
	if end == 0 {
		return ""
	}

	linesFound := 0
	pos := end
	for pos > 0 && linesFound < n {
		pos--
		if text[pos] == '\n' {
			linesFound++
		}
	}
	// If we stopped at a newline, skip past it
	if pos > 0 || (pos == 0 && text[0] == '\n') {
		pos++
	}
	return text[pos:end]
}

// extractPrompt finds the prompt text from output.
// Optimized to search backwards without splitting the entire string.
func extractPrompt(output string) string {
	// Search tail of output for prompts (avoids splitting entire string)
	checkText := tailUTF8Safe(output, promptCheckBytes)

	// Find last newline and work backwards line by line
	for linesChecked := 0; linesChecked < 10 && len(checkText) > 0; linesChecked++ {
		lastNL := strings.LastIndex(checkText, "\n")
		var line string
		if lastNL == -1 {
			line = checkText
			checkText = ""
		} else {
			line = checkText[lastNL+1:]
			checkText = checkText[:lastNL]
		}

		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "[y/n]") ||
			strings.Contains(lineLower, "(y/n)") ||
			strings.Contains(lineLower, "allow edit") ||
			strings.Contains(lineLower, "allow bash") ||
			strings.Contains(lineLower, "approve") ||
			strings.Contains(lineLower, "confirm") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// Approve sends "y" to approve a pending prompt.
func (p *Plugin) Approve(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		if wt.Agent == nil {
			return ApproveResultMsg{WorkspaceName: wt.Name, Err: fmt.Errorf("no agent running")}
		}

		// Send "y" followed by Enter
		cmd := exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "y", "Enter")
		err := cmd.Run()

		return ApproveResultMsg{
			WorkspaceName: wt.Name,
			Err:           err,
		}
	}
}

// Reject sends "n" to reject a pending prompt.
func (p *Plugin) Reject(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		if wt.Agent == nil {
			return RejectResultMsg{WorkspaceName: wt.Name, Err: fmt.Errorf("no agent running")}
		}

		cmd := exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "n", "Enter")
		err := cmd.Run()

		return RejectResultMsg{
			WorkspaceName: wt.Name,
			Err:           err,
		}
	}
}

// ApproveAll approves all worktrees with pending prompts.
func (p *Plugin) ApproveAll() tea.Cmd {
	var cmds []tea.Cmd
	for _, wt := range p.worktrees {
		if wt.Status == StatusWaiting && wt.Agent != nil {
			cmds = append(cmds, p.Approve(wt))
		}
	}

	if len(cmds) == 0 {
		return nil
	}

	return tea.Batch(cmds...)
}

// SendText sends arbitrary text to an agent.
func (p *Plugin) SendText(wt *Worktree, text string) tea.Cmd {
	return func() tea.Msg {
		if wt.Agent == nil {
			return SendTextResultMsg{Err: fmt.Errorf("no agent running")}
		}

		// Use -l to send literal text (no key name lookup)
		cmd := exec.Command("tmux", "send-keys", "-l", "-t", wt.Agent.TmuxSession, text)
		if err := cmd.Run(); err != nil {
			return SendTextResultMsg{Err: err}
		}

		// Send Enter separately
		cmd = exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "Enter")
		err := cmd.Run()

		return SendTextResultMsg{
			WorkspaceName: wt.Name,
			Text:          text,
			Err:           err,
		}
	}
}

// AttachToSession attaches to a tmux session using tea.ExecProcess.
func (p *Plugin) AttachToSession(wt *Worktree) tea.Cmd {
	if !fullTmuxAttachEnabled() || wt == nil || wt.Agent == nil {
		return nil
	}

	sessionName := wt.Agent.TmuxSession
	target := wt.Agent.TmuxPane
	if target == "" {
		target = sessionName
	}

	// Resize to full terminal before attaching so no dot borders appear
	return p.attachWithResize(target, sessionName, wt.Name, func(err error) tea.Msg {
		return TmuxAttachFinishedMsg{WorkspaceName: wt.Name, Err: err}
	})
}

// StopAgent stops an agent running in a worktree.
func (p *Plugin) StopAgent(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		if wt.Agent == nil {
			return AgentStoppedMsg{WorkspaceName: wt.Name}
		}

		sessionName := wt.Agent.TmuxSession

		// Try graceful interrupt first (Ctrl+C)
		_ = exec.Command("tmux", "send-keys", "-t", sessionName, "C-c").Run()

		// Wait briefly for graceful shutdown
		time.Sleep(2 * time.Second)

		// Check if still running
		if sessionExists(sessionName) {
			// Force kill
			_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
		}

		return AgentStoppedMsg{WorkspaceName: wt.Name}
	}
}

// sessionExists checks if a tmux session exists.
func sessionExists(name string) bool { return workspaceops.SessionExists(name) }

// detectOrphanedWorktrees marks worktrees as orphaned if they have a saved
// agent type but no running tmux session.
func (p *Plugin) detectOrphanedWorktrees() {
	for _, wt := range p.worktrees {
		// Skip main worktree - can't attach agents to it anyway
		if wt.IsMain {
			wt.IsOrphaned = false
			// Clean up any stale agent file from main worktree
			if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
				_ = saveAgentType(p.ctx.ProjectRoot, wt.Path, AgentNone)
				wt.ChosenAgentType = ""
			}
			continue
		}
		// Skip if agent is connected
		if wt.Agent != nil {
			wt.IsOrphaned = false
			continue
		}
		// Skip if no agent type was ever chosen
		if wt.ChosenAgentType == AgentNone || wt.ChosenAgentType == "" {
			wt.IsOrphaned = false
			continue
		}
		// Check if tmux session exists
		sessionName := worktreeTmuxSession(wt)
		wt.IsOrphaned = !sessionExists(sessionName)
	}
}

// reconnectAgents finds and reconnects to existing tmux sessions on startup.
func (p *Plugin) reconnectAgents() tea.Cmd {
	type candidate struct {
		key       string
		agentType AgentType
	}
	candidates := make(map[string]candidate, len(p.worktrees))
	ambiguous := make(map[string]bool)
	for _, wt := range p.worktrees {
		name := worktreeSessionSuffix(wt)
		if ambiguous[name] {
			continue
		}
		if _, duplicate := candidates[name]; duplicate {
			delete(candidates, name) // ambiguous presentation cannot route a session
			ambiguous[name] = true
			continue
		}
		candidates[name] = candidate{key: wt.IdentityKey(), agentType: p.resolveWorktreeAgentType(wt)}
	}
	return func() tea.Msg {
		// Find existing sidecar-ws-* tmux sessions
		cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
		output, err := cmd.Output()
		if err != nil {
			// No tmux server running, that's fine
			return reconnectedAgentsMsg{}
		}

		var agents []reconnectedAgent
		sessions := strings.Split(string(output), "\n")

		for _, session := range sessions {
			session = strings.TrimSpace(session)
			if session == "" {
				continue
			}

			// Only reconnect to sessions with our prefix
			if !strings.HasPrefix(session, tmuxSessionPrefix) {
				continue
			}

			sanitizedName := strings.TrimPrefix(session, tmuxSessionPrefix)

			// Check if we have a matching worktree
			// Session suffix is the path slug, not the display name.
			candidate, ok := candidates[sanitizedName]
			if !ok {
				// Session exists but no worktree - orphaned, skip
				continue
			}

			// Create agent record
			paneID := getPaneID(session)
			agent := &Agent{
				Type:        candidate.agentType,
				TmuxSession: session,
				TmuxPane:    paneID,     // Capture pane ID for interactive mode
				StartedAt:   time.Now(), // Unknown actual start
				OutputBuf:   tty.NewOutputBuffer(outputBufferCap),
			}

			agents = append(agents, reconnectedAgent{WorktreeKey: candidate.key, Agent: agent})
		}

		return reconnectedAgentsMsg{Agents: agents}
	}
}

// Cleanup cleans up tmux sessions, optionally removing them.
func (p *Plugin) Cleanup(removeSessions bool) error {
	for name, agent := range p.agents {
		if removeSessions {
			// Only kill sessions we created
			if p.managedSessions[agent.TmuxSession] {
				_ = exec.Command("tmux", "kill-session", "-t", agent.TmuxSession).Run()
				delete(p.managedSessions, agent.TmuxSession)
				globalPaneCache.remove(agent.TmuxSession)
				globalActiveRegistry.remove(agent.TmuxSession) // td-018f25
			}
		}
		delete(p.agents, name)
	}
	return nil
}

// CleanupOrphanedSessions removes sessions that no longer have worktrees.
func (p *Plugin) CleanupOrphanedSessions() error {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return nil // No tmux server
	}

	for _, session := range strings.Split(string(output), "\n") {
		session = strings.TrimSpace(session)
		if session == "" {
			continue
		}

		// Only cleanup sessions we explicitly created and tracked
		if !p.managedSessions[session] {
			continue
		}

		// Check if corresponding worktree still exists
		sanitizedName := strings.TrimPrefix(session, tmuxSessionPrefix)
		if p.findWorktreeBySanitizedName(sanitizedName) == nil {
			_ = exec.Command("tmux", "kill-session", "-t", session).Run()
			delete(p.managedSessions, session)
			globalPaneCache.remove(session)
			globalActiveRegistry.remove(session) // td-018f25
		}
	}
	return nil
}

// validateManagedSessions checks managedSessions against actual tmux sessions
// and returns a command that will deliver the result.
func (p *Plugin) validateManagedSessions() tea.Cmd {
	return func() tea.Msg {
		existing := make(map[string]bool)

		// List all tmux sessions
		cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
		output, err := cmd.Output()
		if err != nil {
			// No tmux server, all sessions are gone
			return validateManagedSessionsResultMsg{ExistingSessions: existing}
		}

		// Build set of existing sessions
		for _, session := range strings.Split(string(output), "\n") {
			session = strings.TrimSpace(session)
			if session != "" {
				existing[session] = true
			}
		}

		return validateManagedSessionsResultMsg{ExistingSessions: existing}
	}
}

// scheduleSessionValidation schedules the next session validation.
func (p *Plugin) scheduleSessionValidation(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return validateManagedSessionsMsg{}
	})
}

// findWorktree finds a worktree by stable key. Name fallback is retained for
// synthetic tests and tmux session compatibility, never for inventoried peers.
func (p *Plugin) findWorktree(key string) *Worktree {
	for _, wt := range p.worktrees {
		if wt.IdentityKey() == key {
			return wt
		}
	}
	var match *Worktree
	for _, wt := range p.worktrees {
		if wt.Name == key {
			if match != nil {
				return nil // presentation name is ambiguous; refuse to route
			}
			match = wt
		}
	}
	return match
}

// findWorktreeBySanitizedName finds a worktree by its tmux session suffix.
// Session identity is the path slug (or Name when Path is empty), not the
// user-editable display name.
func (p *Plugin) findWorktreeBySanitizedName(sanitizedName string) *Worktree {
	for _, wt := range p.worktrees {
		if worktreeSessionSuffix(wt) == sanitizedName {
			return wt
		}
	}
	return nil
}

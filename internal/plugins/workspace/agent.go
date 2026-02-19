package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/projectdir"
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

	// Default history limit for tmux scrollback capture
	tmuxHistoryLimit = 10000

	// Lines to capture from tmux (slightly > outputBufferCap for margin)
	// We only need recent output for status detection and display
	captureLineCount = 600

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
	runawayPollCount    = 20               // Number of polls to track
	runawayTimeWindow   = 3 * time.Second  // If 20 polls happen within this window = runaway
	runawayResetCount   = 3                // Consecutive unchanged polls to reset throttle
)

// AgentStartedMsg signals an agent has been started in a worktree.
type AgentStartedMsg struct {
	Epoch         uint64 // Epoch when request was issued (for stale detection)
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
	Err          error
}

// RejectResultMsg signals the result of a reject action.
type RejectResultMsg struct {
	WorkspaceName string
	Err          error
}

// SendTextResultMsg signals the result of sending text to an agent.
type SendTextResultMsg struct {
	WorkspaceName string
	Text         string
	Err          error
}

// pollAgentMsg triggers output polling for a worktree's agent.
// Includes generation for timer leak prevention (td-83dc22).
type pollAgentMsg struct {
	WorkspaceName string
	Generation   int // Generation at time of scheduling; ignore if stale
}

// reconnectedAgentsMsg delivers reconnected agents from startup.
type reconnectedAgentsMsg struct {
	Cmds []tea.Cmd
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
	return func() tea.Msg {
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - reconnect to it instead of failing
			paneID := getPaneID(sessionName)
			return AgentStartedMsg{
				Epoch:         epoch,
				WorkspaceName: wt.Name,
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
		envCmd := fmt.Sprintf("export TD_SESSION_ID=%s", shellQuote(sessionName))
		_ = exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()

		// Apply environment isolation to prevent conflicts (GOWORK, etc.)
		envOverrides := BuildEnvOverrides(p.ctx.WorkDir)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// If worktree has a linked task, start it in td
		if wt.TaskID != "" {
			tdStartCmd := fmt.Sprintf("td start %s", wt.TaskID)
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, tdStartCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Get the agent command with optional task context
		agentCmd := p.getAgentCommandWithContext(agentType, wt)

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
			WorkspaceName: wt.Name,
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

// buildAgentCommand builds the agent command with optional skip permissions and task context.
// If there's task context, it writes a launcher script to avoid shell escaping issues.
func (p *Plugin) buildAgentCommand(agentType AgentType, wt *Worktree, skipPerms bool, prompt *Prompt) string {
	baseCmd := getAgentCommand(agentType)

	// Apply skip permissions flag if requested
	if skipPerms {
		if flag := SkipPermissionsFlags[agentType]; flag != "" {
			baseCmd = baseCmd + " " + flag
		}
	}

	// Determine context to pass to agent
	var ctx string
	if prompt != nil {
		// Use prompt template with ticket expansion
		ctx = ExpandPromptTemplate(prompt.Body, wt.TaskID)
	} else if wt.TaskID != "" {
		// No prompt selected but task selected: try to fetch full context
		ctx = p.getTaskContext(wt.TaskID)
		if ctx == "" && wt.TaskTitle != "" {
			// Fallback: use task title from modal if td show failed
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
%s --message "$(cat <<'SIDECAR_PROMPT_EOF'
%s
SIDECAR_PROMPT_EOF
)"
rm -f %q
`, shellSetup, baseCmd, prompt, launcherFile)
	case AgentOpenCode:
		// opencode uses 'run' subcommand
		script = fmt.Sprintf(`#!/bin/bash
%s
%s run "$(cat <<'SIDECAR_PROMPT_EOF'
%s
SIDECAR_PROMPT_EOF
)"
rm -f %q
`, shellSetup, baseCmd, prompt, launcherFile)
	default:
		// Most agents (claude, codex, gemini, cursor) take prompt as positional argument
		script = fmt.Sprintf(`#!/bin/bash
%s
%s "$(cat <<'SIDECAR_PROMPT_EOF'
%s
SIDECAR_PROMPT_EOF
)"
rm -f %q
`, shellSetup, baseCmd, prompt, launcherFile)
	}

	if err := os.WriteFile(launcherFile, []byte(script), 0700); err != nil {
		return "", err
	}

	return "bash " + shellQuote(launcherFile), nil
}

// getAgentCommandWithContext returns the agent command with optional task context (legacy, no skip perms).
func (p *Plugin) getAgentCommandWithContext(agentType AgentType, wt *Worktree) string {
	return p.buildAgentCommand(agentType, wt, false, nil)
}

// StartAgentWithOptions creates a tmux session and starts an agent with options.
// If a session already exists, it reconnects to it instead of failing.
func (p *Plugin) StartAgentWithOptions(wt *Worktree, agentType AgentType, skipPerms bool, prompt *Prompt) tea.Cmd {
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	return func() tea.Msg {
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - reconnect to it instead of failing
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

		// Apply environment isolation to prevent conflicts (GOWORK, etc.)
		envOverrides := BuildEnvOverrides(p.ctx.WorkDir)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// If worktree has a linked task, start it in td
		if wt.TaskID != "" {
			tdStartCmd := fmt.Sprintf("td start %s", wt.TaskID)
			_ = exec.Command("tmux", "send-keys", "-t", sessionName, tdStartCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Build the agent command with skip permissions and prompt if enabled
		agentCmd := p.buildAgentCommand(agentType, wt, skipPerms, prompt)

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
			WorkspaceName: wt.Name,
			SessionName:   sessionName,
			PaneID:        paneID,
			AgentType:     agentType,
		}
	}
}

// AttachToWorktreeDir creates a tmux session in the worktree directory and attaches to it.
func (p *Plugin) AttachToWorktreeDir(wt *Worktree) tea.Cmd {
	sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

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
		cmd := exec.Command("tmux", args...)
		if err := cmd.Run(); err != nil {
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

// getPaneID retrieves the tmux pane ID for a session.
// Returns pane IDs like "%12" which are globally unique and stable.
// Uses caching to avoid subprocess calls (pane IDs rarely change) (td-c2961e).
func getPaneID(sessionName string) string {
	// Check cache first
	if paneID, ok := globalPaneIDCache.get(sessionName); ok {
		return paneID
	}

	cmd := exec.Command("tmux", "list-panes", "-t", sessionName, "-F", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Return first pane ID (sessions typically have one pane)
	paneID := strings.TrimSpace(string(output))
	if idx := strings.Index(paneID, "\n"); idx > 0 {
		paneID = paneID[:idx]
	}

	// Cache for future lookups
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
	// Capture current generation for this worktree
	gen := p.pollGeneration[worktreeName]
	stagger := staggerOffset(worktreeName)
	return tea.Tick(delay+stagger, func(t time.Time) tea.Msg {
		return pollAgentMsg{WorkspaceName: worktreeName, Generation: gen}
	})
}

// scheduleInteractivePoll schedules a poll without stagger for the active interactive session (td-8856c9).
// Stagger exists to spread polls across multiple worktrees, but the selected interactive worktree
// needs minimal latency. Uses the same generation tracking as scheduleAgentPoll.
func (p *Plugin) scheduleInteractivePoll(worktreeName string, delay time.Duration) tea.Cmd {
	gen := p.pollGeneration[worktreeName]
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return pollAgentMsg{WorkspaceName: worktreeName, Generation: gen}
	})
}

// AgentPollUnchangedMsg signals content unchanged, schedule next poll.
type AgentPollUnchangedMsg struct {
	WorkspaceName  string
	CurrentStatus WorktreeStatus // Status including session file re-check
	WaitingFor    string         // Prompt text if waiting
	// Cursor position captured atomically (even when content unchanged)
	CursorRow     int
	CursorCol     int
	CursorVisible bool
	HasCursor     bool
	PaneHeight    int // Tmux pane height for cursor offset calculation
	PaneWidth     int // Tmux pane width for display alignment
}

// handlePollAgent captures output from a tmux session asynchronously.
// Uses a goroutine to avoid blocking the UI thread on tmux subprocess calls (td-c2961e).
func (p *Plugin) handlePollAgent(worktreeName string) tea.Cmd {
	wt := p.findWorktree(worktreeName)
	if wt == nil || wt.Agent == nil {
		return func() tea.Msg {
			return AgentStoppedMsg{WorkspaceName: worktreeName}
		}
	}

	// Capture session name and worktree path before spawning goroutine
	sessionName := wt.Agent.TmuxSession
	wtPath := wt.Path
	agentType := wt.Agent.Type
	maxBytes := p.tmuxCaptureMaxBytes
	outputBuf := wt.Agent.OutputBuf
	currentStatus := wt.Status

	// Use non-joined capture when interactive mode is active for this worktree
	// to preserve tmux line wrapping for cursor positioning (td-c7dd1e).
	interactiveCapture := p.viewMode == ViewModeInteractive &&
		p.interactiveState != nil &&
		p.interactiveState.Active &&
		!p.shellSelected
	if interactiveCapture {
		if selected := p.selectedWorktree(); selected == nil || selected.Name != worktreeName {
			interactiveCapture = false
		}
	}

	// When feature is enabled, use direct capture without -J for the selected worktree.
	// This ensures the preview shows content wrapped at the pane width (which is resized
	// to match the preview). We also resize inline to avoid races with async resize cmds.
	directCapture := false
	var resizeTarget string
	var previewWidth, previewHeight int
	if !interactiveCapture && features.IsEnabled(features.TmuxInteractiveInput.Name) {
		if selected := p.selectedWorktree(); selected != nil && selected.Name == worktreeName {
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

	// Return a tea.Cmd that spawns a goroutine for async capture
	return func() tea.Msg {
		// Ensure pane is at preview width before capturing (avoids race with async resize)
		if directCapture && resizeTarget != "" {
			if w, h, ok := queryPaneSize(resizeTarget); !ok || w != previewWidth || h != previewHeight {
				p.resizeTmuxPane(resizeTarget, previewWidth, previewHeight)
			}
		}

		var output string
		var err error
		if interactiveCapture || directCapture {
			output, err = capturePaneDirectWithJoin(sessionName, false)
		} else {
			output, err = capturePane(sessionName)
		}
		if err != nil {
			// Session may have been killed
			if strings.Contains(err.Error(), "can't find") ||
				strings.Contains(err.Error(), "no server") {
				return AgentStoppedMsg{WorkspaceName: worktreeName}
			}
			// Schedule retry on other errors (with delay to prevent busy-loop)
			time.Sleep(pollIntervalActive)
			return pollAgentMsg{WorkspaceName: worktreeName}
		}

		// Capture cursor position atomically with output when in interactive mode.
		// This prevents race conditions where cursor position changes between
		// output capture and cursor query.
		var cursorRow, cursorCol, paneHeight, paneWidth int
		var cursorVisible, hasCursor bool
		if interactiveCapture && cursorTarget != "" {
			cursorRow, cursorCol, paneHeight, paneWidth, cursorVisible, hasCursor = queryCursorPositionSync(cursorTarget)
		}

		output = trimCapturedOutput(output, maxBytes)

		// Use hash-based change detection to skip processing if content unchanged
		outputChanged := outputBuf == nil || outputBuf.Update(output)

		// Detect status. Both detectors run; each is authoritative for what it's good at (td-2fca7d):
		//   - tmux patterns: thinking, done, error (high-signal, session files can't detect these)
		//   - session files: active vs waiting (reliable, tmux patterns are noisy for this)
		// Session file detection ALWAYS runs (even when output unchanged) because the agent
		// may finish while tmux output stays the same (td-2fca7d v8).
		status := currentStatus
		waitingFor := ""
		if !interactiveCapture {
			if outputChanged {
				// Tmux pattern detection only when output changes (same output = same patterns).
				status = detectStatus(output)
				if status == StatusWaiting {
					waitingFor = extractPrompt(output)
				}
			}
			// Session file check runs every poll — mtime changes independently of tmux output.
			// Only override active/waiting; preserve tmux-detected thinking/done/error.
			if status == StatusActive || status == StatusWaiting {
				if sessionStatus, ok := detectAgentSessionStatus(agentType, wtPath); ok {
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
					slog.Debug("status: no session file, using tmux", "worktree", worktreeName, "status", status, "agent", agentType)
				}
			}
		}

		if !outputChanged {
			return AgentPollUnchangedMsg{
				WorkspaceName:  worktreeName,
				CurrentStatus: status,
				WaitingFor:    waitingFor,
				CursorRow:     cursorRow,
				CursorCol:     cursorCol,
				CursorVisible: cursorVisible,
				HasCursor:     hasCursor,
				PaneHeight:    paneHeight,
				PaneWidth:     paneWidth,
			}
		}

		return AgentOutputMsg{
			WorkspaceName:  worktreeName,
			Output:        output,
			Status:        status,
			WaitingFor:    waitingFor,
			CursorRow:     cursorRow,
			CursorCol:     cursorCol,
			CursorVisible: cursorVisible,
			HasCursor:     hasCursor,
			PaneHeight:    paneHeight,
			PaneWidth:     paneWidth,
		}
	}
}

// capturePane captures the last N lines of a tmux pane.
// Uses caching to avoid redundant subprocess calls when multiple worktrees poll simultaneously.
// On cache miss, captures active sessions at once to populate cache for concurrent polls.
// Only captures sessions that have been recently polled (td-018f25).
func capturePane(sessionName string) (string, error) {
	// Mark this session as active (td-018f25)
	globalActiveRegistry.markActive(sessionName)

	// Check cache first
	if output, ok := globalPaneCache.get(sessionName); ok {
		return output, nil
	}

	// Cache miss - batch capture active sidecar sessions (singleflight)
	outputs, err, ran := globalCaptureCoordinator.runBatch(batchCaptureActiveSessions)
	if !ran {
		// Another goroutine captured; re-check cache
		if output, ok := globalPaneCache.get(sessionName); ok {
			return output, nil
		}
		return capturePaneDirect(sessionName)
	}
	if err != nil {
		// Fall back to single capture on batch error
		return capturePaneDirect(sessionName)
	}

	// Cache all results from batch
	globalPaneCache.setAll(outputs)

	// Return requested session's output
	if output, ok := outputs[sessionName]; ok {
		return output, nil
	}

	// Session not in batch results - try direct capture
	return capturePaneDirect(sessionName)
}

// capturePaneDirect captures a single pane without caching.
// When tmux_interactive_input is enabled, panes are resized to match preview width,
// so we skip -J to preserve tmux's native wrapping (matches interactive mode rendering).
func capturePaneDirect(sessionName string) (string, error) {
	joinWrapped := !features.IsEnabled(features.TmuxInteractiveInput.Name)
	return capturePaneDirectWithJoin(sessionName, joinWrapped)
}

// capturePaneDirectWithJoin captures a single pane without caching.
// When joinWrapped is false, tmux preserves wrapped lines for correct cursor alignment.
func capturePaneDirectWithJoin(sessionName string, joinWrapped bool) (string, error) {
	startLine := fmt.Sprintf("-%d", captureLineCount)
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCaptureTimeout)
	defer cancel()
	args := []string{"capture-pane", "-p", "-e"}
	if joinWrapped {
		args = append(args, "-J")
	}
	args = append(args, "-S", startLine, "-t", sessionName)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("capture-pane: timeout after %s", tmuxCaptureTimeout)
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("capture-pane: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(output), nil
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

	// Build bash script that only captures active sessions
	// Quote session names to handle special characters safely
	var quotedSessions []string
	for _, s := range activeSessions {
		quotedSessions = append(quotedSessions, fmt.Sprintf("%q", s))
	}

	// When tmux_interactive_input is enabled, panes are resized to match preview width,
	// so skip -J to preserve tmux's native wrapping (matches interactive mode rendering).
	captureArgs := "-p -e -J"
	if features.IsEnabled(features.TmuxInteractiveInput.Name) {
		captureArgs = "-p -e"
	}

	script := fmt.Sprintf(`
for session in %s; do
    echo "===SIDECAR_SESSION:$session==="
    tmux capture-pane %s -S -%d -t "$session" 2>/dev/null
done
`, strings.Join(quotedSessions, " "), captureArgs, captureLineCount)

	ctx, cancel := context.WithTimeout(context.Background(), tmuxBatchCaptureTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("batch capture: timeout after %s", tmuxBatchCaptureTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("batch capture: %w", err)
	}

	// Parse output by splitting on delimiter
	results := make(map[string]string)
	parts := strings.Split(string(output), "===SIDECAR_SESSION:")

	for _, part := range parts {
		if part == "" {
			continue
		}
		// Find session name (ends with ===)
		idx := strings.Index(part, "===")
		if idx == -1 {
			continue
		}
		sessionName := strings.Clone(part[:idx])
		content := ""
		if idx+3 < len(part) {
			content = part[idx+3:]
			// Trim leading newline from content
			content = strings.TrimPrefix(content, "\n")
		}
		results[sessionName] = strings.Clone(content)
	}

	return results, nil
}

func trimCapturedOutput(output string, maxBytes int) string {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output
	}
	trimmed := tailUTF8Safe(output, maxBytes)
	if nl := strings.Index(trimmed, "\n"); nl >= 0 && nl+1 < len(trimmed) {
		return trimmed[nl+1:]
	}
	return trimmed
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
// For supported agents (Claude, Codex, Gemini, OpenCode), session file analysis runs
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
			Err:          err,
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
			Err:          err,
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
			Text:         text,
			Err:          err,
		}
	}
}

// AttachToSession attaches to a tmux session using tea.ExecProcess.
func (p *Plugin) AttachToSession(wt *Worktree) tea.Cmd {
	if wt.Agent == nil {
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
func sessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

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
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)
		wt.IsOrphaned = !sessionExists(sessionName)
	}
}

// reconnectAgents finds and reconnects to existing tmux sessions on startup.
func (p *Plugin) reconnectAgents() tea.Cmd {
	return func() tea.Msg {
		// Find existing sidecar-ws-* tmux sessions
		cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
		output, err := cmd.Output()
		if err != nil {
			// No tmux server running, that's fine
			return reconnectedAgentsMsg{Cmds: nil}
		}

		var pollingCmds []tea.Cmd
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
			// Use sanitized name lookup since session names are created with sanitizeName()
			wt := p.findWorktreeBySanitizedName(sanitizedName)
			if wt == nil {
				// Session exists but no worktree - orphaned, skip
				continue
			}

			// Create agent record
			paneID := getPaneID(session)
			agentType := wt.ChosenAgentType
			if agentType == "" || agentType == AgentNone {
				agentType = AgentClaude // Fallback if no .sidecar-agent file
			}
			agent := &Agent{
				Type:        agentType,
				TmuxSession: session,
				TmuxPane:    paneID,     // Capture pane ID for interactive mode
				StartedAt:   time.Now(), // Unknown actual start
				OutputBuf:   NewOutputBuffer(outputBufferCap),
			}

			wt.Agent = agent
			p.agents[wt.Name] = agent

			// Track as managed (for safe cleanup)
			p.managedSessions[session] = true

			// Schedule polling via tea.Cmd
			pollingCmds = append(pollingCmds, p.scheduleAgentPoll(wt.Name, 0))
		}

		return reconnectedAgentsMsg{Cmds: pollingCmds}
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
		// Use sanitized name lookup since session names are created with sanitizeName()
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

// findWorktree finds a worktree by name.
func (p *Plugin) findWorktree(name string) *Worktree {
	for _, wt := range p.worktrees {
		if wt.Name == name {
			return wt
		}
	}
	return nil
}

// findWorktreeBySanitizedName finds a worktree by its sanitized name.
// This is used when matching tmux session names back to worktrees, since
// session names are created with sanitizeName(wt.Name) which replaces
// '.', ':', and '/' with '-'.
func (p *Plugin) findWorktreeBySanitizedName(sanitizedName string) *Worktree {
	for _, wt := range p.worktrees {
		if sanitizeName(wt.Name) == sanitizedName {
			return wt
		}
	}
	return nil
}

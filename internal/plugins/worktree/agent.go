package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
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

// Global cache instance for pane captures
var globalPaneCache = &paneCache{
	entries: make(map[string]paneCacheEntry),
	ttl:     300 * time.Millisecond, // Cache valid for 300ms
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

const (
	// Tmux session prefix for sidecar-managed worktree sessions
	tmuxSessionPrefix = "sidecar-wt-"

	// Default history limit for tmux scrollback capture
	tmuxHistoryLimit = 10000

	// Lines to capture from tmux (slightly > outputBufferCap for margin)
	// We only need recent output for status detection and display
	captureLineCount = 600

	// Polling intervals - adaptive based on agent status
	// Conservative values to reduce CPU with multiple worktrees while maintaining responsiveness
	pollIntervalInitial    = 500 * time.Millisecond // First poll after agent starts
	pollIntervalActive     = 500 * time.Millisecond // Agent actively processing (keep fast for UX)
	pollIntervalIdle       = 5 * time.Second        // No change detected (was 3s)
	pollIntervalWaiting    = 5 * time.Second        // Agent waiting for user input
	pollIntervalDone       = 20 * time.Second       // Agent completed/exited (was 10s)
	pollIntervalBackground = 10 * time.Second       // Output not visible, plugin focused (was 5s)
	pollIntervalUnfocused  = 20 * time.Second       // Plugin not focused (was 15s)

	// Poll staggering to prevent simultaneous subprocess spawns
	pollStaggerMax = 400 * time.Millisecond // Max stagger offset based on worktree name hash

	// Status detection window - chars from end to check for status patterns
	// ~10 lines of 150 chars each = 1500, but we use 2048 for UTF-8 safety margin
	statusCheckBytes = 2048

	// Prompt extraction window - chars from end to search for prompts
	// ~15 lines of 150 chars each = 2250, but we use 2560 for UTF-8 safety margin
	promptCheckBytes = 2560
)

// AgentStartedMsg signals an agent has been started in a worktree.
type AgentStartedMsg struct {
	WorktreeName string
	SessionName  string
	AgentType    AgentType
	Reconnected  bool // True if we reconnected to an existing session
	Err          error
}

// ApproveResultMsg signals the result of an approve action.
type ApproveResultMsg struct {
	WorktreeName string
	Err          error
}

// RejectResultMsg signals the result of a reject action.
type RejectResultMsg struct {
	WorktreeName string
	Err          error
}

// SendTextResultMsg signals the result of sending text to an agent.
type SendTextResultMsg struct {
	WorktreeName string
	Text         string
	Err          error
}

// pollAgentMsg triggers output polling for a worktree's agent.
type pollAgentMsg struct {
	WorktreeName string
}

// reconnectedAgentsMsg delivers reconnected agents from startup.
type reconnectedAgentsMsg struct {
	Cmds []tea.Cmd
}

// StartAgent creates a tmux session and starts an agent for a worktree.
// If a session already exists, it reconnects to it instead of failing.
func (p *Plugin) StartAgent(wt *Worktree, agentType AgentType) tea.Cmd {
	return func() tea.Msg {
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - reconnect to it instead of failing
			return AgentStartedMsg{
				WorktreeName: wt.Name,
				SessionName:  sessionName,
				AgentType:    agentType,
				Reconnected:  true, // Flag that we reconnected to existing session
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
		envCmd := fmt.Sprintf("export TD_SESSION_ID=%s", sessionName)
		exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()

		// Apply environment isolation to prevent conflicts (GOWORK, etc.)
		envOverrides := BuildEnvOverrides(p.ctx.WorkDir)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// If worktree has a linked task, start it in td
		if wt.TaskID != "" {
			tdStartCmd := fmt.Sprintf("td start %s", wt.TaskID)
			exec.Command("tmux", "send-keys", "-t", sessionName, tdStartCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Get the agent command with optional task context
		agentCmd := p.getAgentCommandWithContext(agentType, wt)

		// Send the agent command to start it
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, agentCmd, "Enter")
		if err := sendCmd.Run(); err != nil {
			// Try to kill the session if we failed to start the agent
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			return AgentStartedMsg{Err: fmt.Errorf("start agent: %w", err)}
		}

		return AgentStartedMsg{
			WorktreeName: wt.Name,
			SessionName:  sessionName,
			AgentType:    agentType,
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
	launcherFile := filepath.Join(worktreePath, ".sidecar-start.sh")

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

	return "bash " + launcherFile, nil
}

// getAgentCommandWithContext returns the agent command with optional task context (legacy, no skip perms).
func (p *Plugin) getAgentCommandWithContext(agentType AgentType, wt *Worktree) string {
	return p.buildAgentCommand(agentType, wt, false, nil)
}

// StartAgentWithOptions creates a tmux session and starts an agent with options.
// If a session already exists, it reconnects to it instead of failing.
func (p *Plugin) StartAgentWithOptions(wt *Worktree, agentType AgentType, skipPerms bool, prompt *Prompt) tea.Cmd {
	return func() tea.Msg {
		sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)

		// Check if session already exists
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			// Session exists - reconnect to it instead of failing
			return AgentStartedMsg{
				WorktreeName: wt.Name,
				SessionName:  sessionName,
				AgentType:    agentType,
				Reconnected:  true,
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
		tdEnvCmd := fmt.Sprintf("export TD_SESSION_ID=%s", sessionName)
		exec.Command("tmux", "send-keys", "-t", sessionName, tdEnvCmd, "Enter").Run()

		// Apply environment isolation to prevent conflicts (GOWORK, etc.)
		envOverrides := BuildEnvOverrides(p.ctx.WorkDir)
		if envCmd := GenerateSingleEnvCommand(envOverrides); envCmd != "" {
			exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()
		}

		// If worktree has a linked task, start it in td
		if wt.TaskID != "" {
			tdStartCmd := fmt.Sprintf("td start %s", wt.TaskID)
			exec.Command("tmux", "send-keys", "-t", sessionName, tdStartCmd, "Enter").Run()
		}

		// Small delay to ensure env is set
		time.Sleep(100 * time.Millisecond)

		// Build the agent command with skip permissions and prompt if enabled
		agentCmd := p.buildAgentCommand(agentType, wt, skipPerms, prompt)

		// Send the agent command to start it
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, agentCmd, "Enter")
		if err := sendCmd.Run(); err != nil {
			// Try to kill the session if we failed to start the agent
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			return AgentStartedMsg{Err: fmt.Errorf("start agent: %w", err)}
		}

		return AgentStartedMsg{
			WorktreeName: wt.Name,
			SessionName:  sessionName,
			AgentType:    agentType,
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
				return TmuxAttachFinishedMsg{WorktreeName: wt.Name, Err: fmt.Errorf("create session: %w", err)}
			}
		}

		// Track as managed session
		p.managedSessions[sessionName] = true
	}

	// Attach to the session
	c := exec.Command("tmux", "attach-session", "-t", sessionName)
	return tea.Sequence(
		tea.Printf("\nAttaching to %s. Press Ctrl-b d to return to sidecar.\n", wt.Name),
		tea.ExecProcess(c, func(err error) tea.Msg {
			return TmuxAttachFinishedMsg{WorktreeName: wt.Name, Err: err}
		}),
	)
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
func (p *Plugin) scheduleAgentPoll(worktreeName string, delay time.Duration) tea.Cmd {
	stagger := staggerOffset(worktreeName)
	return tea.Tick(delay+stagger, func(t time.Time) tea.Msg {
		return pollAgentMsg{WorktreeName: worktreeName}
	})
}

// AgentPollUnchangedMsg signals content unchanged, schedule next poll.
type AgentPollUnchangedMsg struct {
	WorktreeName  string
	CurrentStatus WorktreeStatus // For adaptive polling interval selection
}

// handlePollAgent captures output from a tmux session.
func (p *Plugin) handlePollAgent(worktreeName string) tea.Cmd {
	return func() tea.Msg {
		wt := p.findWorktree(worktreeName)
		if wt == nil || wt.Agent == nil {
			return AgentStoppedMsg{WorktreeName: worktreeName}
		}

		output, err := capturePane(wt.Agent.TmuxSession)
		if err != nil {
			// Session may have been killed
			if strings.Contains(err.Error(), "can't find") ||
				strings.Contains(err.Error(), "no server") {
				return AgentStoppedMsg{WorktreeName: worktreeName}
			}
			// Schedule retry on other errors
			return pollAgentMsg{WorktreeName: worktreeName}
		}

		// Use hash-based change detection to skip processing if content unchanged
		if wt.Agent.OutputBuf != nil && !wt.Agent.OutputBuf.Update(output) {
			// Content unchanged - signal to schedule next poll with delay
			// Include current status for adaptive polling interval selection
			return AgentPollUnchangedMsg{
				WorktreeName:  worktreeName,
				CurrentStatus: wt.Status,
			}
		}

		// Content changed - detect status and emit
		status := detectStatus(output)
		waitingFor := ""
		if status == StatusWaiting {
			waitingFor = extractPrompt(output)
		}

		// For supported agents: supplement tmux detection with session file analysis
		// Session files are more reliable for detecting "waiting at prompt" state
		if status == StatusActive {
			if sessionStatus, ok := detectAgentSessionStatus(wt.Agent.Type, wt.Path); ok {
				if sessionStatus == StatusWaiting {
					status = StatusWaiting
					waitingFor = "Waiting for input"
				}
			}
		}

		return AgentOutputMsg{
			WorktreeName: worktreeName,
			Output:       output,
			Status:       status,
			WaitingFor:   waitingFor,
		}
	}
}

// capturePane captures the last N lines of a tmux pane.
// Uses caching to avoid redundant subprocess calls when multiple worktrees poll simultaneously.
// On cache miss, captures ALL sidecar sessions at once to populate cache for other polls.
func capturePane(sessionName string) (string, error) {
	// Check cache first
	if output, ok := globalPaneCache.get(sessionName); ok {
		return output, nil
	}

	// Cache miss - batch capture all sidecar sessions
	outputs, err := batchCaptureAllSessions()
	if err != nil {
		// Fall back to single capture on batch error
		return capturePaneDirect(sessionName)
	}

	// Cache all results
	globalPaneCache.setAll(outputs)

	// Return requested session's output
	if output, ok := outputs[sessionName]; ok {
		return output, nil
	}

	// Session not in batch results - try direct capture
	return capturePaneDirect(sessionName)
}

// capturePaneDirect captures a single pane without caching.
func capturePaneDirect(sessionName string) (string, error) {
	startLine := fmt.Sprintf("-%d", captureLineCount)
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", startLine, "-t", sessionName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(output), nil
}

// batchCaptureAllSessions captures all sidecar-wt-* sessions in a single subprocess.
// Returns map of session name to output.
func batchCaptureAllSessions() (map[string]string, error) {
	// Single shell command that lists sessions and captures each
	// Uses a unique delimiter to separate outputs
	script := fmt.Sprintf(`
for session in $(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep '^%s'); do
    echo "===SIDECAR_SESSION:$session==="
    tmux capture-pane -p -e -J -S -%d -t "$session" 2>/dev/null
done
`, tmuxSessionPrefix, captureLineCount)

	cmd := exec.Command("bash", "-c", script)
	output, err := cmd.Output()
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
		sessionName := part[:idx]
		content := ""
		if idx+3 < len(part) {
			content = part[idx+3:]
			// Trim leading newline from content
			content = strings.TrimPrefix(content, "\n")
		}
		results[sessionName] = content
	}

	return results, nil
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
// Optimized to avoid unnecessary string allocations.
func detectStatus(output string) WorktreeStatus {
	// Check tail of output for status patterns (avoids splitting entire string)
	checkText := tailUTF8Safe(output, statusCheckBytes)
	textLower := strings.ToLower(checkText)

	// Waiting patterns (agent needs user input) - highest priority
	waitingPatterns := []string{
		"[y/n]",       // Claude Code permission prompt
		"(y/n)",       // Aider style
		"allow edit",  // Claude Code file edit
		"allow bash",  // Claude Code bash command
		"waiting for", // Generic waiting
		"press enter", // Continue prompt
		"continue?",
		"approve",
		"confirm",
		"do you want", // Common prompt
		"❯",           // Claude Code input prompt (waiting for user)
		"╰─❯",         // Claude Code prompt with tree line decoration
	}

	for _, pattern := range waitingPatterns {
		if strings.Contains(textLower, pattern) {
			return StatusWaiting
		}
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
			return ApproveResultMsg{WorktreeName: wt.Name, Err: fmt.Errorf("no agent running")}
		}

		// Send "y" followed by Enter
		cmd := exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "y", "Enter")
		err := cmd.Run()

		return ApproveResultMsg{
			WorktreeName: wt.Name,
			Err:          err,
		}
	}
}

// Reject sends "n" to reject a pending prompt.
func (p *Plugin) Reject(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		if wt.Agent == nil {
			return RejectResultMsg{WorktreeName: wt.Name, Err: fmt.Errorf("no agent running")}
		}

		cmd := exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "n", "Enter")
		err := cmd.Run()

		return RejectResultMsg{
			WorktreeName: wt.Name,
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
			WorktreeName: wt.Name,
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

	// Use tea.ExecProcess to suspend Bubble Tea and run tmux attach
	c := exec.Command("tmux", "attach-session", "-t", wt.Agent.TmuxSession)

	// Print hint before attaching, then attach
	return tea.Sequence(
		tea.Printf("\nAttaching to %s. Press Ctrl-b d to return to sidecar.\n", wt.Name),
		tea.ExecProcess(c, func(err error) tea.Msg {
			return TmuxAttachFinishedMsg{WorktreeName: wt.Name, Err: err}
		}),
	)
}

// StopAgent stops an agent running in a worktree.
func (p *Plugin) StopAgent(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		if wt.Agent == nil {
			return AgentStoppedMsg{WorktreeName: wt.Name}
		}

		sessionName := wt.Agent.TmuxSession

		// Try graceful interrupt first (Ctrl+C)
		exec.Command("tmux", "send-keys", "-t", sessionName, "C-c").Run()

		// Wait briefly for graceful shutdown
		time.Sleep(2 * time.Second)

		// Check if still running
		if sessionExists(sessionName) {
			// Force kill
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
		}

		return AgentStoppedMsg{WorktreeName: wt.Name}
	}
}

// sessionExists checks if a tmux session exists.
func sessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// reconnectAgents finds and reconnects to existing tmux sessions on startup.
func (p *Plugin) reconnectAgents() tea.Cmd {
	return func() tea.Msg {
		// Find existing sidecar-wt-* tmux sessions
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

			worktreeName := strings.TrimPrefix(session, tmuxSessionPrefix)

			// Check if we have a matching worktree
			wt := p.findWorktree(worktreeName)
			if wt == nil {
				// Session exists but no worktree - orphaned, skip
				continue
			}

			// Create agent record
			agent := &Agent{
				Type:        AgentClaude, // Default, will be detected from output
				TmuxSession: session,
				StartedAt:   time.Now(), // Unknown actual start
				OutputBuf:   NewOutputBuffer(outputBufferCap),
			}

			wt.Agent = agent
			p.agents[wt.Name] = agent

			// Track as managed (for safe cleanup)
			p.managedSessions[session] = true

			// Schedule polling via tea.Cmd
			pollingCmds = append(pollingCmds, p.scheduleAgentPoll(worktreeName, 0))
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
				exec.Command("tmux", "kill-session", "-t", agent.TmuxSession).Run()
				delete(p.managedSessions, agent.TmuxSession)
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
		worktreeName := strings.TrimPrefix(session, tmuxSessionPrefix)
		if p.findWorktree(worktreeName) == nil {
			exec.Command("tmux", "kill-session", "-t", session).Run()
			delete(p.managedSessions, session)
		}
	}
	return nil
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

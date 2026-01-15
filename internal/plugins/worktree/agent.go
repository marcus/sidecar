package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// Tmux session prefix for sidecar-managed worktree sessions
	tmuxSessionPrefix = "sidecar-wt-"

	// Default history limit for tmux scrollback capture
	tmuxHistoryLimit = 10000

	// Polling intervals
	pollIntervalInitial = 500 * time.Millisecond
	pollIntervalNormal  = 1 * time.Second
)

// AgentStartedMsg signals an agent has been started in a worktree.
type AgentStartedMsg struct {
	WorktreeName string
	SessionName  string
	AgentType    AgentType
	Reconnected  bool  // True if we reconnected to an existing session
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
				WorktreeName:  wt.Name,
				SessionName:   sessionName,
				AgentType:     agentType,
				Reconnected:   true, // Flag that we reconnected to existing session
			}
		}

		// Create new detached session with working directory
		args := []string{
			"new-session",
			"-d",             // Detached
			"-s", sessionName, // Session name
			"-c", wt.Path,    // Working directory
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
func (p *Plugin) buildAgentCommand(agentType AgentType, wt *Worktree, skipPerms bool) string {
	baseCmd := getAgentCommand(agentType)

	// Apply skip permissions flag if requested
	if skipPerms {
		if flag := SkipPermissionsFlags[agentType]; flag != "" {
			baseCmd = baseCmd + " " + flag
		}
	}

	// Get task context if available
	var ctx string
	if wt.TaskID != "" {
		ctx = p.getTaskContext(wt.TaskID)
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
	promptFile := filepath.Join(worktreePath, ".sidecar-prompt")
	launcherFile := filepath.Join(worktreePath, ".sidecar-start.sh")

	// Write prompt to file (raw bytes, no escaping needed)
	if err := os.WriteFile(promptFile, []byte(prompt), 0600); err != nil {
		return "", err
	}

	// Build the agent invocation with prompt read from file
	// Different agents have different syntax for passing prompts
	var agentInvocation string
	switch agentType {
	case AgentAider:
		// aider uses --message flag
		agentInvocation = fmt.Sprintf(`%s --message "$PROMPT"`, baseCmd)
	case AgentOpenCode:
		// opencode uses 'run' subcommand
		agentInvocation = fmt.Sprintf(`%s run "$PROMPT"`, baseCmd)
	default:
		// Most agents take prompt as positional argument
		agentInvocation = fmt.Sprintf(`%s "$PROMPT"`, baseCmd)
	}

	// Write launcher script
	script := fmt.Sprintf(`#!/bin/bash
PROMPT=$(cat %q)
rm -f %q
%s
rm -f %q
`, promptFile, promptFile, agentInvocation, launcherFile)

	if err := os.WriteFile(launcherFile, []byte(script), 0700); err != nil {
		os.Remove(promptFile) // cleanup on error
		return "", err
	}

	return "bash " + launcherFile, nil
}

// getAgentCommandWithContext returns the agent command with optional task context (legacy, no skip perms).
func (p *Plugin) getAgentCommandWithContext(agentType AgentType, wt *Worktree) string {
	return p.buildAgentCommand(agentType, wt, false)
}

// StartAgentWithOptions creates a tmux session and starts an agent with options.
// If a session already exists, it reconnects to it instead of failing.
func (p *Plugin) StartAgentWithOptions(wt *Worktree, agentType AgentType, skipPerms bool) tea.Cmd {
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
			"-c", wt.Path,     // Working directory
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

		// Build the agent command with skip permissions if enabled
		agentCmd := p.buildAgentCommand(agentType, wt, skipPerms)

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
			"-c", wt.Path,     // Working directory
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
	cmd := exec.Command("td", "show", taskID, "--json")
	cmd.Dir = p.ctx.WorkDir
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

// scheduleAgentPoll returns a command that schedules a poll after delay.
func (p *Plugin) scheduleAgentPoll(worktreeName string, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return pollAgentMsg{WorktreeName: worktreeName}
	})
}

// AgentPollUnchangedMsg signals content unchanged, schedule next poll.
type AgentPollUnchangedMsg struct {
	WorktreeName string
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
			return AgentPollUnchangedMsg{WorktreeName: worktreeName}
		}

		// Content changed - detect status and emit
		status := detectStatus(output)
		waitingFor := ""
		if status == StatusWaiting {
			waitingFor = extractPrompt(output)
		}

		return AgentOutputMsg{
			WorktreeName: worktreeName,
			Output:       output,
			Status:       status,
			WaitingFor:   waitingFor,
		}
	}
}

// capturePane captures the current content of a tmux pane.
func capturePane(sessionName string) (string, error) {
	// -p: Print to stdout (instead of buffer)
	// -e: Preserve ANSI escape sequences (colors)
	// -J: Join wrapped lines
	// -S -: Capture entire scrollback history
	// -t: Target session
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-t", sessionName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(output), nil
}

// detectStatus determines agent status from captured output.
func detectStatus(output string) WorktreeStatus {
	lines := strings.Split(output, "\n")

	// Check last ~10 lines for patterns
	checkLines := lines
	if len(lines) > 10 {
		checkLines = lines[len(lines)-10:]
	}
	text := strings.Join(checkLines, "\n")
	textLower := strings.ToLower(text)

	// Waiting patterns (agent needs user input) - highest priority
	waitingPatterns := []string{
		"[y/n]",        // Claude Code permission prompt
		"(y/n)",        // Aider style
		"allow edit",   // Claude Code file edit
		"allow bash",   // Claude Code bash command
		"waiting for",  // Generic waiting
		"press enter",  // Continue prompt
		"continue?",
		"approve",
		"confirm",
		"do you want", // Common prompt
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
func extractPrompt(output string) string {
	lines := strings.Split(output, "\n")

	// Find line containing prompt (search from end)
	for i := len(lines) - 1; i >= 0 && i > len(lines)-10; i-- {
		line := lines[i]
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

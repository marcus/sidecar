package worktree

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
)

// Shell session constants
const (
	shellSessionPrefix = "sidecar-sh-" // Distinct from worktree prefix "sidecar-wt-"
)

// tmuxInstalled caches whether tmux is available in PATH.
// Checked once and cached to avoid repeated exec calls.
var (
	tmuxInstalledOnce   sync.Once
	tmuxInstalledCached bool
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

	// ShellOutputMsg signals shell output was captured (for polling)
	ShellOutputMsg struct {
		Index   int    // Index of the shell in p.shells
		Output  string
		Changed bool
	}

	// pollShellByIdxMsg triggers a poll for a specific shell's output
	pollShellByIdxMsg struct {
		Index int
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

		shell := &ShellSession{
			Name:     displayName,
			TmuxName: line,
			Agent: &Agent{
				Type:        AgentShell,
				TmuxSession: line,
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

// generateShellSessionName creates a unique tmux session name for a new shell.
func (p *Plugin) generateShellSessionName() string {
	projectName := filepath.Base(p.ctx.WorkDir)
	basePrefix := shellSessionPrefix + sanitizeName(projectName)

	// Find the next available index
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
			// Legacy format counts as index 1
			if maxIdx < 1 {
				maxIdx = 1
			}
		}
	}

	return fmt.Sprintf("%s-%d", basePrefix, maxIdx+1)
}

// createNewShell creates a new shell session and returns a command.
func (p *Plugin) createNewShell() tea.Cmd {
	sessionName := p.generateShellSessionName()
	displayName := fmt.Sprintf("Shell %d", len(p.shells)+1)
	workDir := p.ctx.WorkDir

	return func() tea.Msg {
		// Check if session already exists (shouldn't happen with unique names)
		if sessionExists(sessionName) {
			return ShellCreatedMsg{SessionName: sessionName, DisplayName: displayName}
		}

		// Create new detached session in project directory
		args := []string{
			"new-session",
			"-d",              // Detached
			"-s", sessionName, // Session name
			"-c", workDir,     // Working directory
		}
		cmd := exec.Command("tmux", args...)
		if err := cmd.Run(); err != nil {
			return ShellCreatedMsg{
				SessionName: sessionName,
				DisplayName: displayName,
				Err:         fmt.Errorf("create shell session: %w", err),
			}
		}

		return ShellCreatedMsg{SessionName: sessionName, DisplayName: displayName}
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

	c := exec.Command("tmux", "attach-session", "-t", sessionName)
	return tea.Sequence(
		tea.Printf("\nAttaching to %s. Press Ctrl-b d to return to sidecar.\n", displayName),
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
			return ShellCreatedMsg{SessionName: sessionName, DisplayName: shell.Name}
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

// pollShellSessionByIndex captures output from a specific shell session.
func (p *Plugin) pollShellSessionByIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(p.shells) {
		return nil
	}

	shell := p.shells[idx]
	if shell.Agent == nil {
		return nil
	}

	sessionName := shell.TmuxName
	outputBuf := shell.Agent.OutputBuf
	maxBytes := p.tmuxCaptureMaxBytes

	return func() tea.Msg {
		output, err := capturePaneDirect(sessionName)
		if err != nil {
			return ShellOutputMsg{Index: idx, Output: "", Changed: false}
		}

		// Trim to max bytes
		output = trimCapturedOutput(output, maxBytes)

		// Update buffer and check if content changed
		changed := outputBuf.Update(output)

		return ShellOutputMsg{Index: idx, Output: output, Changed: changed}
	}
}

// scheduleShellPollByIndex schedules a poll for a specific shell's output.
func (p *Plugin) scheduleShellPollByIndex(idx int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return pollShellByIdxMsg{Index: idx}
	})
}

// getSelectedShell returns the currently selected shell, or nil if none.
func (p *Plugin) getSelectedShell() *ShellSession {
	if !p.shellSelected || p.selectedShellIdx < 0 || p.selectedShellIdx >= len(p.shells) {
		return nil
	}
	return p.shells[p.selectedShellIdx]
}

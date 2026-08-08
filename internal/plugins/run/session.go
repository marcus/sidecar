package run

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	sessionPrefix          = "sidecar-run-"
	defaultCaptureMaxBytes = 2 * 1024 * 1024 // 2MB
)

// RunStatus represents the state of a running command.
type RunStatus int

const (
	StatusIdle    RunStatus = iota // Not yet run
	StatusRunning                  // Currently executing
	StatusDone                     // Finished successfully
	StatusError                    // Finished with error
)

// String returns a display label for the status.
func (s RunStatus) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusDone:
		return "done"
	case StatusError:
		return "error"
	default:
		return "idle"
	}
}

// RunSession tracks a tmux session for a running command.
type RunSession struct {
	SessionName string
	Command     RunCommand
	Status      RunStatus
	Output      string
	StartedAt   time.Time
}

// Messages for run session lifecycle.
type (
	// RunSessionStartedMsg signals a command session was created and started.
	RunSessionStartedMsg struct {
		Index       int
		SessionName string
		Err         error
	}

	// RunOutputMsg carries captured output from a running session.
	RunOutputMsg struct {
		Index   int
		Output  string
		Changed bool
	}

	// RunSessionStoppedMsg signals a session was terminated.
	RunSessionStoppedMsg struct {
		Index int
	}

	// PollRunOutputMsg triggers a poll for command output.
	PollRunOutputMsg struct {
		Index int
	}

	// DetectCommandsMsg carries auto-detected commands.
	DetectCommandsMsg struct {
		Commands []RunCommand
	}
)

// startRunSession creates a tmux session and runs the command.
func startRunSession(idx int, cmd RunCommand, workDir string) tea.Cmd {
	sessionName := fmt.Sprintf("%s%d", sessionPrefix, idx)

	return func() tea.Msg {
		// Kill any existing session with this name
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()

		// Create new detached session
		args := []string{
			"new-session",
			"-d",
			"-s", sessionName,
			"-c", workDir,
		}
		createCmd := exec.Command("tmux", args...)
		if err := createCmd.Run(); err != nil {
			return RunSessionStartedMsg{
				Index:       idx,
				SessionName: sessionName,
				Err:         fmt.Errorf("create session: %w", err),
			}
		}

		// Send the command
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, cmd.Command, "Enter")
		if err := sendCmd.Run(); err != nil {
			_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			return RunSessionStartedMsg{
				Index:       idx,
				SessionName: sessionName,
				Err:         fmt.Errorf("send command: %w", err),
			}
		}

		return RunSessionStartedMsg{
			Index:       idx,
			SessionName: sessionName,
		}
	}
}

// captureRunOutput captures the pane output from a tmux session.
func captureRunOutput(idx int, sessionName string) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("tmux", "capture-pane", "-t", sessionName, "-p", "-S", "-500").Output()
		if err != nil {
			// Session may have died
			return RunOutputMsg{Index: idx, Output: "", Changed: false}
		}

		output := string(out)
		// Trim trailing whitespace
		output = strings.TrimRight(output, "\n ")

		return RunOutputMsg{Index: idx, Output: output, Changed: true}
	}
}

// stopRunSession kills a tmux session.
func stopRunSession(idx int, sessionName string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
		return RunSessionStoppedMsg{Index: idx}
	}
}

// schedulePoll schedules a poll for output after a delay.
func schedulePoll(idx int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return PollRunOutputMsg{Index: idx}
	})
}

// isSessionAlive checks if a tmux session exists.
func isSessionAlive(sessionName string) bool {
	err := exec.Command("tmux", "has-session", "-t", sessionName).Run()
	return err == nil
}

package tty

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// IsSessionDeadError checks if an error indicates the tmux session/pane is gone.
func IsSessionDeadError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "can't find pane") ||
		strings.Contains(errStr, "no such session") ||
		strings.Contains(errStr, "session not found") ||
		strings.Contains(errStr, "pane not found")
}

// SendKeyToTmux sends a key to a tmux pane using send-keys.
// Uses the tmux key name syntax (e.g., "Enter", "C-c", "Up").
func SendKeyToTmux(sessionName, key string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, key)
	return cmd.Run()
}

// SendLiteralToTmux sends literal text to a tmux pane using send-keys -l.
// This prevents tmux from interpreting special key names.
func SendLiteralToTmux(sessionName, text string) error {
	// tmux treats bare ; in argv as a command separator, so a literal
	// semicolon never reaches send-keys. Fall back to hex encoding (-H)
	// which bypasses tmux's command parser entirely.
	if strings.Contains(text, ";") {
		args := []string{"send-keys", "-t", sessionName, "-H"}
		for _, b := range []byte(text) {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		return exec.Command("tmux", args...).Run()
	}
	cmd := exec.Command("tmux", "send-keys", "-l", "-t", sessionName, text)
	return cmd.Run()
}

// SendKeysCmd sends keys to tmux asynchronously.
// Keys are sent in order within a single goroutine to prevent reordering.
// Returns SessionDeadMsg if the session has ended.
func SendKeysCmd(sessionName string, keys ...KeySpec) tea.Cmd {
	return func() tea.Msg {
		for _, k := range keys {
			var err error
			if k.Literal {
				err = SendLiteralToTmux(sessionName, k.Value)
			} else {
				err = SendKeyToTmux(sessionName, k.Value)
			}
			if err != nil && IsSessionDeadError(err) {
				return SessionDeadMsg{}
			}
		}
		return nil
	}
}

// ResizeTmuxPane resizes a tmux window/pane to the specified dimensions.
// resize-window works for detached sessions; resize-pane is a fallback.
func ResizeTmuxPane(paneID string, width, height int) {
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

// SetWindowSizeManual sets the tmux window-size option to "manual" for a session.
// This prevents tmux from auto-constraining window size based on attached clients,
// allowing resize-window commands to stick reliably.
func SetWindowSizeManual(sessionName string) {
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "window-size", "manual").Run()
}

// QueryPaneSize queries the current size of a tmux pane.
func QueryPaneSize(target string) (width, height int, ok bool) {
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

// SendSGRMouse sends an SGR mouse event to a tmux pane.
// button is the mouse button (0=left, 1=middle, 2=right).
// col and row are 1-indexed coordinates.
// release indicates if this is a button release event.
func SendSGRMouse(sessionName string, button, col, row int, release bool) error {
	if col <= 0 || row <= 0 {
		return nil
	}
	suffix := "M"
	if release {
		suffix = "m"
	}
	seq := fmt.Sprintf("\x1b[<%d;%d;%d%s", button, col, row, suffix)
	return SendLiteralToTmux(sessionName, seq)
}

// CapturePaneOutput captures the current output of a tmux pane.
// Uses capture-pane with -p flag to print to stdout and -e to preserve
// ANSI escape sequences (colors, styles).
// The scrollback parameter controls how many lines of history to capture.
func CapturePaneOutput(target string, scrollback int) (string, error) {
	args := []string{"capture-pane", "-p", "-e", "-t", target}
	if scrollback > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", scrollback))
	}
	cmd := exec.Command("tmux", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

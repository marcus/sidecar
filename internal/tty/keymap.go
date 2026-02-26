// Package tty provides an embeddable tmux terminal model for TUI plugins.
// It handles tmux session management, key mapping, cursor rendering, and adaptive polling.
package tty

import (
	tea "github.com/charmbracelet/bubbletea"
)

// MapKeyToTmux translates a Bubble Tea key message to a tmux send-keys argument.
// Returns the tmux key name and whether to use literal mode (-l).
// For modified keys and special keys, returns the tmux key name.
// For literal characters, returns the character with useLiteral=true.
func MapKeyToTmux(msg tea.KeyMsg) (key string, useLiteral bool) {
	switch msg.String() {
	case "shift+up":
		return "\x1b[1;2A", true
	case "shift+down":
		return "\x1b[1;2B", true
	case "shift+right":
		return "\x1b[1;2C", true
	case "shift+left":
		return "\x1b[1;2D", true
	case "ctrl+up":
		return "\x1b[1;5A", true
	case "ctrl+down":
		return "\x1b[1;5B", true
	case "ctrl+right":
		return "\x1b[1;5C", true
	case "ctrl+left":
		return "\x1b[1;5D", true
	case "alt+up":
		return "\x1b[1;3A", true
	case "alt+down":
		return "\x1b[1;3B", true
	case "alt+right":
		return "\x1b[1;3C", true
	case "alt+left":
		return "\x1b[1;3D", true
	case "shift+tab":
		return "\x1b[Z", true
	case "shift+enter":
		return "\x1b[13;2u", true // CSI u: shift+return
	}

	// Handle special keys
	// Note: KeyCtrlI == KeyTab and KeyCtrlM == KeyEnter in BubbleTea,
	// so we handle Tab and Enter first, then other Ctrl keys.
	switch msg.Type {
	case tea.KeyEnter: // Also KeyCtrlM
		return "Enter", false
	case tea.KeyBackspace:
		return "BSpace", false
	case tea.KeyDelete:
		return "DC", false
	case tea.KeyTab: // Also KeyCtrlI
		return "Tab", false
	case tea.KeySpace:
		return "Space", false
	case tea.KeyUp:
		return "Up", false
	case tea.KeyDown:
		return "Down", false
	case tea.KeyLeft:
		return "Left", false
	case tea.KeyRight:
		return "Right", false
	case tea.KeyHome:
		return "Home", false
	case tea.KeyEnd:
		return "End", false
	case tea.KeyPgUp:
		return "PPage", false
	case tea.KeyPgDown:
		return "NPage", false
	case tea.KeyInsert:
		return "IC", false
	case tea.KeyEscape:
		return "Escape", false

	// Ctrl combinations (excluding KeyCtrlI/Tab and KeyCtrlM/Enter handled above)
	case tea.KeyCtrlA:
		return "C-a", false
	case tea.KeyCtrlB:
		return "C-b", false
	case tea.KeyCtrlC:
		return "C-c", false
	case tea.KeyCtrlD:
		return "C-d", false
	case tea.KeyCtrlE:
		return "C-e", false
	case tea.KeyCtrlF:
		return "C-f", false
	case tea.KeyCtrlG:
		return "C-g", false
	case tea.KeyCtrlH:
		return "C-h", false
	case tea.KeyCtrlJ:
		return "C-j", false
	case tea.KeyCtrlK:
		return "C-k", false
	case tea.KeyCtrlL:
		return "C-l", false
	case tea.KeyCtrlN:
		return "C-n", false
	case tea.KeyCtrlO:
		return "C-o", false
	case tea.KeyCtrlP:
		return "C-p", false
	case tea.KeyCtrlQ:
		return "C-q", false
	case tea.KeyCtrlR:
		return "C-r", false
	case tea.KeyCtrlS:
		return "C-s", false
	case tea.KeyCtrlT:
		return "C-t", false
	case tea.KeyCtrlU:
		return "C-u", false
	case tea.KeyCtrlV:
		return "C-v", false
	case tea.KeyCtrlW:
		return "C-w", false
	case tea.KeyCtrlX:
		return "C-x", false
	case tea.KeyCtrlY:
		return "C-y", false
	case tea.KeyCtrlZ:
		return "C-z", false

	// Function keys (F1-F12)
	case tea.KeyF1:
		return "F1", false
	case tea.KeyF2:
		return "F2", false
	case tea.KeyF3:
		return "F3", false
	case tea.KeyF4:
		return "F4", false
	case tea.KeyF5:
		return "F5", false
	case tea.KeyF6:
		return "F6", false
	case tea.KeyF7:
		return "F7", false
	case tea.KeyF8:
		return "F8", false
	case tea.KeyF9:
		return "F9", false
	case tea.KeyF10:
		return "F10", false
	case tea.KeyF11:
		return "F11", false
	case tea.KeyF12:
		return "F12", false

	case tea.KeyRunes:
		// Regular character input
		if len(msg.Runes) > 0 {
			return string(msg.Runes), true
		}
		return "", true
	}

	// Fallback for any unhandled key types
	if msg.String() != "" {
		return msg.String(), true
	}
	return "", true
}

// KeySpec describes a key to send to tmux with ordering preserved.
type KeySpec struct {
	Value   string
	Literal bool
}

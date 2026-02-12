package keymap

import (
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const sequenceTimeout = 500 * time.Millisecond

// Command represents a registered command handler.
type Command struct {
	ID      string
	Name    string
	Handler func() tea.Cmd
	Context string
}

// Binding maps a key or key sequence to a command.
type Binding struct {
	Key     string // e.g., "tab", "ctrl+s", "g g"
	Command string // Command ID
	Context string // "global", plugin ID, etc.
}

// Registry manages key bindings and command dispatch.
type Registry struct {
	commands      map[string]Command   // ID -> Command
	bindings      map[string][]Binding // context -> bindings
	userOverrides map[string]string    // key -> command ID
	pendingKey    string
	pendingTime   time.Time
	mu            sync.RWMutex
}

// NewRegistry creates a new keymap registry.
func NewRegistry() *Registry {
	return &Registry{
		commands:      make(map[string]Command),
		bindings:      make(map[string][]Binding),
		userOverrides: make(map[string]string),
	}
}

// RegisterCommand adds a command to the registry.
func (r *Registry) RegisterCommand(cmd Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[cmd.ID] = cmd
}

// RegisterBinding adds a key binding.
func (r *Registry) RegisterBinding(b Binding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[b.Context] = append(r.bindings[b.Context], b)
}

// RegisterPluginBinding satisfies plugin.BindingRegistrar interface.
// It converts plugin.Binding to keymap.Binding and registers it.
func (r *Registry) RegisterPluginBinding(key, command, context string) {
	r.RegisterBinding(Binding{Key: key, Command: command, Context: context})
}

// SetUserOverride sets a user-configured key override.
func (r *Registry) SetUserOverride(key, commandID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userOverrides[key] = commandID
}

// Handle dispatches a key event to the appropriate command handler.
// Returns nil if no matching binding is found.
func (r *Registry) Handle(key tea.KeyMsg, activeContext string) tea.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()

	keyStr := keyToString(key)

	// Check for pending key sequence
	if r.pendingKey != "" {
		if time.Since(r.pendingTime) < sequenceTimeout {
			seq := r.pendingKey + " " + keyStr
			r.pendingKey = ""
			if cmd := r.findCommand(seq, activeContext); cmd != nil {
				return cmd
			}
			// Sequence didn't match, try just the new key
		} else {
			r.pendingKey = ""
		}
	}

	// Check if this key starts a sequence
	if r.isSequenceStart(keyStr, activeContext) {
		r.pendingKey = keyStr
		r.pendingTime = time.Now()
		return nil
	}

	return r.findCommand(keyStr, activeContext)
}

// findCommand looks up a command for the given key in order of precedence.
func (r *Registry) findCommand(key, activeContext string) tea.Cmd {
	// 1. Check user overrides first
	if cmdID, ok := r.userOverrides[key]; ok {
		if cmd, ok := r.commands[cmdID]; ok && cmd.Handler != nil {
			return cmd.Handler()
		}
	}

	// 2. Check active context bindings
	if activeContext != "" && activeContext != "global" {
		if cmd, found := r.findInContext(key, activeContext); found {
			return cmd
		}
	}

	// 3. Fall back to global bindings
	cmd, _ := r.findInContext(key, "global")
	return cmd
}

// findInContext finds a command for a key in a specific context.
// Returns the command result and whether a binding was found.
func (r *Registry) findInContext(key, context string) (tea.Cmd, bool) {
	for _, b := range r.bindings[context] {
		if b.Key == key {
			if cmd, ok := r.commands[b.Command]; ok && cmd.Handler != nil {
				return cmd.Handler(), true
			}
		}
	}
	return nil, false
}

// isSequenceStart checks if this key could start a multi-key sequence.
func (r *Registry) isSequenceStart(key, activeContext string) bool {
	prefix := key + " "

	// Check all contexts that could be active
	contexts := []string{"global"}
	if activeContext != "" && activeContext != "global" {
		contexts = append(contexts, activeContext)
	}

	for _, ctx := range contexts {
		for _, b := range r.bindings[ctx] {
			if strings.HasPrefix(b.Key, prefix) {
				return true
			}
		}
	}

	// Also check user overrides
	for k := range r.userOverrides {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}

	return false
}

// ResetPending clears any pending key sequence.
func (r *Registry) ResetPending() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingKey = ""
}

// GetCommand retrieves a command by ID.
// Returns the command and true if found, or zero value and false otherwise.
func (r *Registry) GetCommand(id string) (Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cmd, ok := r.commands[id]
	return cmd, ok
}

// BindingsForContext returns all bindings for a given context.
func (r *Registry) BindingsForContext(context string) []Binding {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bindings[context]
}

// AllContexts returns all contexts that have bindings.
func (r *Registry) AllContexts() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	contexts := make([]string, 0, len(r.bindings))
	for ctx := range r.bindings {
		contexts = append(contexts, ctx)
	}
	return contexts
}

// HasPending returns true if there's a pending key sequence.
func (r *Registry) HasPending() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pendingKey != "" && time.Since(r.pendingTime) < sequenceTimeout
}

// keyToString converts a tea.KeyMsg to a string representation.
func keyToString(key tea.KeyMsg) string {
	switch key.Type {
	case tea.KeyCtrlC:
		return "ctrl+c"
	case tea.KeyCtrlA:
		return "ctrl+a"
	case tea.KeyCtrlB:
		return "ctrl+b"
	case tea.KeyCtrlD:
		return "ctrl+d"
	case tea.KeyCtrlE:
		return "ctrl+e"
	case tea.KeyCtrlF:
		return "ctrl+f"
	case tea.KeyCtrlG:
		return "ctrl+g"
	case tea.KeyCtrlH:
		return "ctrl+h"
	case tea.KeyTab:
		return "tab"
	case tea.KeyCtrlJ:
		return "ctrl+j"
	case tea.KeyCtrlK:
		return "ctrl+k"
	case tea.KeyCtrlL:
		return "ctrl+l"
	case tea.KeyEnter:
		return "enter"
	case tea.KeyCtrlN:
		return "ctrl+n"
	case tea.KeyCtrlO:
		return "ctrl+o"
	case tea.KeyCtrlP:
		return "ctrl+p"
	case tea.KeyCtrlQ:
		return "ctrl+q"
	case tea.KeyCtrlR:
		return "ctrl+r"
	case tea.KeyCtrlS:
		return "ctrl+s"
	case tea.KeyCtrlT:
		return "ctrl+t"
	case tea.KeyCtrlU:
		return "ctrl+u"
	case tea.KeyCtrlV:
		return "ctrl+v"
	case tea.KeyCtrlW:
		return "ctrl+w"
	case tea.KeyCtrlX:
		return "ctrl+x"
	case tea.KeyCtrlY:
		return "ctrl+y"
	case tea.KeyCtrlZ:
		return "ctrl+z"
	case tea.KeyEsc:
		return "esc"
	case tea.KeySpace:
		return "space"
	case tea.KeyBackspace:
		return "backspace"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	case tea.KeyLeft:
		return "left"
	case tea.KeyRight:
		return "right"
	case tea.KeyHome:
		return "home"
	case tea.KeyEnd:
		return "end"
	case tea.KeyPgUp:
		return "pgup"
	case tea.KeyPgDown:
		return "pgdown"
	case tea.KeyDelete:
		return "delete"
	case tea.KeyShiftTab:
		return "shift+tab"
	case tea.KeyRunes:
		return string(key.Runes)
	default:
		return key.String()
	}
}

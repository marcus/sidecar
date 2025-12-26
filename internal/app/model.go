package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sst/sidecar/internal/keymap"
	"github.com/sst/sidecar/internal/plugin"
)

// Model is the root Bubble Tea model for the sidecar application.
type Model struct {
	// Plugin management
	registry     *plugin.Registry
	activePlugin int

	// Keymap
	keymap        *keymap.Registry
	activeContext string

	// UI state
	width, height   int
	showHelp        bool
	showDiagnostics bool
	showFooter      bool

	// Header/footer
	ui *UIState

	// Status/toast messages
	statusMsg    string
	statusExpiry time.Time

	// Error handling
	lastError error

	// Ready state
	ready bool
}

// New creates a new application model.
func New(reg *plugin.Registry, km *keymap.Registry) Model {
	return Model{
		registry:      reg,
		keymap:        km,
		activePlugin:  0,
		activeContext: "global",
		showFooter:    true,
		ui:            NewUIState(),
		ready:         false,
	}
}

// Init initializes the model and returns initial commands.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
	}

	// Start all registered plugins
	for _, cmd := range m.registry.Start() {
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// ActivePlugin returns the currently active plugin.
func (m Model) ActivePlugin() plugin.Plugin {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return nil
	}
	if m.activePlugin >= len(plugins) {
		m.activePlugin = 0
	}
	return plugins[m.activePlugin]
}

// SetActivePlugin sets the active plugin by index.
func (m *Model) SetActivePlugin(idx int) {
	plugins := m.registry.Plugins()
	if idx >= 0 && idx < len(plugins) {
		// Unfocus current
		if current := m.ActivePlugin(); current != nil {
			current.SetFocused(false)
		}
		m.activePlugin = idx
		// Focus new
		if next := m.ActivePlugin(); next != nil {
			next.SetFocused(true)
			m.activeContext = next.FocusContext()
		}
	}
}

// NextPlugin switches to the next plugin.
func (m *Model) NextPlugin() {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return
	}
	m.SetActivePlugin((m.activePlugin + 1) % len(plugins))
}

// PrevPlugin switches to the previous plugin.
func (m *Model) PrevPlugin() {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return
	}
	idx := m.activePlugin - 1
	if idx < 0 {
		idx = len(plugins) - 1
	}
	m.SetActivePlugin(idx)
}

// ShowToast displays a temporary status message.
func (m *Model) ShowToast(msg string, duration time.Duration) {
	m.statusMsg = msg
	m.statusExpiry = time.Now().Add(duration)
}

// ClearToast clears any expired toast message.
func (m *Model) ClearToast() {
	if m.statusMsg != "" && time.Now().After(m.statusExpiry) {
		m.statusMsg = ""
	}
}

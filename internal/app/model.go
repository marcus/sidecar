package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sst/sidecar/internal/keymap"
	"github.com/sst/sidecar/internal/palette"
	"github.com/sst/sidecar/internal/plugin"
	"github.com/sst/sidecar/internal/version"
)

// TabBounds represents the X position range of a tab for mouse hit testing.
type TabBounds struct {
	Start, End int
}

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
	showPalette     bool
	showQuitConfirm bool
	palette         palette.Model

	// Header/footer
	ui *UIState

	// Status/toast messages
	statusMsg    string
	statusExpiry time.Time

	// Error handling
	lastError error

	// Ready state
	ready bool

	// Version info
	currentVersion  string
	updateAvailable *version.UpdateAvailableMsg

	// Intro animation
	intro IntroModel
}

// New creates a new application model.
func New(reg *plugin.Registry, km *keymap.Registry, currentVersion string) Model {
	return Model{
		registry:       reg,
		keymap:         km,
		activePlugin:   0,
		activeContext:  "global",
		showFooter:     true,
		palette:        palette.New(),
		ui:             NewUIState(),
		ready:          false,
		intro:          NewIntroModel(),
		currentVersion: currentVersion,
	}
}

// Init initializes the model and returns initial commands.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		IntroTick(),
		version.CheckAsync(m.currentVersion),
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

// SetActivePlugin sets the active plugin by index and returns a command
// to notify the plugin it has been focused.
func (m *Model) SetActivePlugin(idx int) tea.Cmd {
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
			return PluginFocused()
		}
	}
	return nil
}

// NextPlugin switches to the next plugin.
func (m *Model) NextPlugin() tea.Cmd {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return nil
	}
	return m.SetActivePlugin((m.activePlugin + 1) % len(plugins))
}

// PrevPlugin switches to the previous plugin.
func (m *Model) PrevPlugin() tea.Cmd {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return nil
	}
	idx := m.activePlugin - 1
	if idx < 0 {
		idx = len(plugins) - 1
	}
	return m.SetActivePlugin(idx)
}

// FocusPluginByID switches to a plugin by its ID.
func (m *Model) FocusPluginByID(id string) tea.Cmd {
	plugins := m.registry.Plugins()
	for i, p := range plugins {
		if p.ID() == id {
			return m.SetActivePlugin(i)
		}
	}
	return nil
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

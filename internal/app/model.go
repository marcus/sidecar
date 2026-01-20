package app

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/version"
)

// TabBounds represents the X position range of a tab for mouse hit testing.
type TabBounds struct {
	Start, End int
}

// Model is the root Bubble Tea model for the sidecar application.
type Model struct {
	// Configuration
	cfg *config.Config

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

	// Project switcher modal
	showProjectSwitcher    bool
	projectSwitcherCursor  int
	projectSwitcherScroll  int
	projectSwitcherHover   int // -1 = no hover, 0+ = hovered project index

	// Header/footer
	ui *UIState

	// Status/toast messages
	statusMsg     string
	statusExpiry  time.Time
	statusIsError bool

	// Error handling
	lastError error

	// Ready state
	ready bool

	// Version info
	currentVersion  string
	updateAvailable *version.UpdateAvailableMsg
	tdVersionInfo   *version.TdVersionMsg

	// Update feature state
	updateButtonFocus  bool
	updateInProgress   bool
	updateError        string
	needsRestart       bool
	updateButtonBounds mouse.Rect
	updateSpinnerFrame int

	// Intro animation
	intro IntroModel
}

// New creates a new application model.
// initialPluginID optionally specifies which plugin to focus on startup (empty = first plugin).
func New(reg *plugin.Registry, km *keymap.Registry, cfg *config.Config, currentVersion, workDir, initialPluginID string) Model {
	repoName := GetRepoName(workDir)
	ui := NewUIState()
	ui.WorkDir = workDir

	// Determine initial active plugin index
	activeIdx := 0
	if initialPluginID != "" {
		for i, p := range reg.Plugins() {
			if p.ID() == initialPluginID {
				activeIdx = i
				break
			}
		}
	}

	return Model{
		cfg:                   cfg,
		registry:              reg,
		keymap:                km,
		activePlugin:          activeIdx,
		activeContext:         "global",
		showFooter:            true,
		palette:               palette.New(),
		ui:                    ui,
		ready:                 false,
		intro:                 NewIntroModel(repoName),
		currentVersion:        currentVersion,
		projectSwitcherHover:  -1, // No hover initially
	}
}

// Init initializes the model and returns initial commands.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		IntroTick(),
		version.CheckAsync(m.currentVersion),
		version.CheckTdAsync(),
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
		m.statusIsError = false
	}
}

// hasUpdatesAvailable returns true if either sidecar or td has an update available.
func (m *Model) hasUpdatesAvailable() bool {
	if m.updateAvailable != nil {
		return true
	}
	if m.tdVersionInfo != nil && m.tdVersionInfo.HasUpdate && m.tdVersionInfo.Installed {
		return true
	}
	return false
}

// doUpdate executes go install commands for available updates.
func (m *Model) doUpdate() tea.Cmd {
	sidecarUpdate := m.updateAvailable
	tdUpdate := m.tdVersionInfo

	return func() tea.Msg {
		// Check Go is available
		if _, err := exec.LookPath("go"); err != nil {
			return UpdateErrorMsg{Step: "check", Err: fmt.Errorf("go not found in PATH")}
		}

		var sidecarUpdated, tdUpdated bool
		var newSidecarVersion, newTdVersion string

		// Update sidecar
		if sidecarUpdate != nil {
			args := []string{
				"install",
				"-ldflags", fmt.Sprintf("-X main.Version=%s", sidecarUpdate.LatestVersion),
				fmt.Sprintf("github.com/marcus/sidecar/cmd/sidecar@%s", sidecarUpdate.LatestVersion),
			}
			cmd := exec.Command("go", args...)
			if output, err := cmd.CombinedOutput(); err != nil {
				return UpdateErrorMsg{Step: "sidecar", Err: fmt.Errorf("%v: %s", err, output)}
			}
			sidecarUpdated = true
			newSidecarVersion = sidecarUpdate.LatestVersion
		}

		// Update td
		if tdUpdate != nil && tdUpdate.HasUpdate && tdUpdate.Installed {
			cmd := exec.Command("go", "install",
				fmt.Sprintf("github.com/marcus/td@%s", tdUpdate.LatestVersion))
			if output, err := cmd.CombinedOutput(); err != nil {
				return UpdateErrorMsg{Step: "td", Err: fmt.Errorf("%v: %s", err, output)}
			}
			tdUpdated = true
			newTdVersion = tdUpdate.LatestVersion
		}

		return UpdateSuccessMsg{
			SidecarUpdated:    sidecarUpdated,
			TdUpdated:         tdUpdated,
			NewSidecarVersion: newSidecarVersion,
			NewTdVersion:      newTdVersion,
		}
	}
}

// updateDiagnosticsButtonBounds calculates the button bounds for mouse clicks.
// Call this when diagnostics modal is shown or window is resized.
func (m *Model) updateDiagnosticsButtonBounds() {
	if !m.hasUpdatesAvailable() || m.updateInProgress || m.needsRestart {
		m.updateButtonBounds = mouse.Rect{} // No clickable button
		return
	}

	// The modal content has a known structure:
	// - Logo: 7 lines
	// - Blank: 1
	// - Plugins section: 1 (title) + N (one per plugin with potential diagnostics)
	// - Blank: 1
	// - System section: 1 (title) + 2 (workdir, refresh)
	// - Blank: 1
	// - Version section: 1 (title) + 2-3 (sidecar, td)
	// - Blank: 1
	// - Button line (this is what we need)

	// Count lines dynamically
	lineCount := 7 + 1 // logo + blank
	lineCount++        // plugins title
	for _, p := range m.registry.Plugins() {
		lineCount++
		if dp, ok := p.(plugin.DiagnosticProvider); ok {
			lineCount += len(dp.Diagnostics())
		}
	}
	lineCount++ // blank after plugins
	lineCount += 3 // system section (title + 2 lines)
	lineCount++ // blank
	lineCount++ // version title
	lineCount++ // sidecar version line
	if m.tdVersionInfo != nil {
		lineCount++ // td version line
	}
	lineCount++ // blank before button
	// Now we're at the button line

	buttonLineInModal := lineCount

	// ModalBox has 1 cell padding all around, plus 1 cell border
	modalPadding := 1
	modalBorder := 1
	buttonIndent := 2 // "  " before button

	// Estimate modal dimensions (will be close enough for click detection)
	// Logo width is approximately 45 chars
	modalWidth := 50 + (modalPadding * 2) + (modalBorder * 2)
	modalHeight := lineCount + 4 + (modalPadding * 2) + (modalBorder * 2) // +4 for lines after button

	// Calculate modal position (centered)
	modalX := (m.width - modalWidth) / 2
	modalY := (m.height - modalHeight) / 2
	if modalX < 0 {
		modalX = 0
	}
	if modalY < 0 {
		modalY = 0
	}

	// Calculate button position
	buttonX := modalX + modalBorder + modalPadding + buttonIndent
	buttonY := modalY + modalBorder + modalPadding + buttonLineInModal
	buttonWidth := 8 // " Update "

	m.updateButtonBounds = mouse.Rect{X: buttonX, Y: buttonY, W: buttonWidth, H: 1}
}

// resetProjectSwitcher resets the project switcher modal state.
func (m *Model) resetProjectSwitcher() {
	m.showProjectSwitcher = false
	m.projectSwitcherCursor = 0
	m.projectSwitcherScroll = 0
	m.projectSwitcherHover = -1
}

// switchProject switches all plugins to a new project directory.
func (m *Model) switchProject(projectPath string) tea.Cmd {
	// Skip if already on this project
	if projectPath == m.ui.WorkDir {
		return func() tea.Msg {
			return ToastMsg{Message: "Already on this project", Duration: 2 * time.Second}
		}
	}

	// Save the active plugin state for the old workdir
	oldWorkDir := m.ui.WorkDir
	if activePlugin := m.ActivePlugin(); activePlugin != nil {
		state.SetActivePlugin(oldWorkDir, activePlugin.ID())
	}

	// Update the UI state
	m.ui.WorkDir = projectPath
	m.intro.RepoName = GetRepoName(projectPath)

	// Reinitialize all plugins with the new working directory
	// This stops all plugins, updates the context, and starts them again
	startCmds := m.registry.Reinit(projectPath)

	// Restore active plugin for the new workdir if saved, otherwise keep current
	newActivePluginID := state.GetActivePlugin(projectPath)
	if newActivePluginID != "" {
		m.FocusPluginByID(newActivePluginID)
	}

	// Return batch of start commands plus a toast notification
	return tea.Batch(
		tea.Batch(startCmds...),
		func() tea.Msg {
			return ToastMsg{
				Message:  fmt.Sprintf("Switched to %s", GetRepoName(projectPath)),
				Duration: 3 * time.Second,
			}
		},
	)
}

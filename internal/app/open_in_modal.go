package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	openInItemPrefix  = "open-in-item-"
	openInMaxVisible  = 10
)

// openInApp represents a known IDE/app that can open the project directory.
type openInApp struct {
	ID              string   // short identifier (e.g. "vscode")
	Name            string   // display name (e.g. "VS Code")
	AppBundles      []string // macOS .app bundle names to look for in /Applications
	AlwaysAvailable bool     // true if always shown (e.g. Finder)
	FoundBundle     string   // the actual bundle name found during detection (e.g. "Visual Studio Code.app")
}

// openInRegistry is the static list of known macOS apps.
var openInRegistry = []openInApp{
	{ID: "vscode", Name: "VS Code", AppBundles: []string{"Visual Studio Code.app"}},
	{ID: "cursor", Name: "Cursor", AppBundles: []string{"Cursor.app"}},
	{ID: "android-studio", Name: "Android Studio", AppBundles: []string{"Android Studio.app"}},
	{ID: "intellij", Name: "IntelliJ IDEA", AppBundles: []string{"IntelliJ IDEA.app", "IntelliJ IDEA CE.app"}},
	{ID: "webstorm", Name: "WebStorm", AppBundles: []string{"WebStorm.app"}},
	{ID: "goland", Name: "GoLand", AppBundles: []string{"GoLand.app"}},
	{ID: "pycharm", Name: "PyCharm", AppBundles: []string{"PyCharm.app", "PyCharm CE.app"}},
	{ID: "xcode", Name: "Xcode", AppBundles: []string{"Xcode.app"}},
	{ID: "sublime", Name: "Sublime Text", AppBundles: []string{"Sublime Text.app"}},
	{ID: "zed", Name: "Zed", AppBundles: []string{"Zed.app"}},
	{ID: "fleet", Name: "Fleet", AppBundles: []string{"Fleet.app"}},
	{ID: "terminal", Name: "Terminal", AppBundles: []string{"Terminal.app"}},
	{ID: "finder", Name: "Finder", AlwaysAvailable: true},
}

// openInItemID returns the focusable ID for an open-in item at the given index.
func openInItemID(idx int) string {
	return fmt.Sprintf("%s%d", openInItemPrefix, idx)
}

// detectInstalledApps checks which apps from the registry are installed.
// It returns only apps whose .app bundles exist in appsDir, plus any AlwaysAvailable apps.
func detectInstalledApps(registry []openInApp, appsDir string) []openInApp {
	var installed []openInApp
	for _, app := range registry {
		if app.AlwaysAvailable {
			installed = append(installed, app)
			continue
		}
		for _, bundle := range app.AppBundles {
			if _, err := os.Stat(filepath.Join(appsDir, bundle)); err == nil {
				found := app
				found.FoundBundle = bundle
				installed = append(installed, found)
				break
			}
		}
	}
	return installed
}

// findLastUsedIndex returns the index of the app with the given ID, or 0 if not found.
func findLastUsedIndex(apps []openInApp, lastID string) int {
	if lastID == "" {
		return 0
	}
	for i, app := range apps {
		if app.ID == lastID {
			return i
		}
	}
	return 0
}

// openInEnsureCursorVisible adjusts scroll to keep cursor in view.
func openInEnsureCursorVisible(cursor, scroll, maxVisible int) int {
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+maxVisible {
		return cursor - maxVisible + 1
	}
	return scroll
}

// initOpenIn initializes the Open In modal state.
func (m *Model) initOpenIn() {
	m.clearOpenInModal()

	// Detect installed apps
	m.openInApps = detectInstalledApps(openInRegistry, "/Applications")

	// Resolve last-used app ID: per-project first, then global fallback
	m.openInLastID = ""
	if pc := m.currentProjectConfig(); pc != nil && pc.LastOpenInApp != "" {
		m.openInLastID = pc.LastOpenInApp
	} else if m.cfg != nil && m.cfg.UI.LastOpenInApp != "" {
		m.openInLastID = m.cfg.UI.LastOpenInApp
	}

	// Set cursor to last-used app
	m.openInCursor = findLastUsedIndex(m.openInApps, m.openInLastID)
	m.openInScroll = openInEnsureCursorVisible(m.openInCursor, 0, openInMaxVisible)
}

// resetOpenIn resets all Open In modal state.
func (m *Model) resetOpenIn() {
	m.showOpenIn = false
	m.openInCursor = 0
	m.openInScroll = 0
	m.openInApps = nil
	m.openInLastID = ""
	m.clearOpenInModal()
}

// clearOpenInModal clears the modal/width/mouseHandler cache.
func (m *Model) clearOpenInModal() {
	m.openInModal = nil
	m.openInModalWidth = 0
	m.openInMouseHandler = nil
}

// ensureOpenInModal builds/rebuilds the Open In modal if needed.
func (m *Model) ensureOpenInModal() {
	modalW := 40
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 30 {
		modalW = 30
	}

	// Only rebuild if modal doesn't exist or width changed
	if m.openInModal != nil && m.openInModalWidth == modalW {
		return
	}
	m.openInModalWidth = modalW

	m.openInModal = modal.New("Open In...",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(m.openInListSection()).
		AddSection(m.openInHintsSection())
}

// openInListSection renders the IDE list with cursor and scroll support.
func (m *Model) openInListSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		apps := m.openInApps

		if len(apps) == 0 {
			return modal.RenderedSection{Content: styles.Muted.Render("No apps found")}
		}

		cursorStyle := lipgloss.NewStyle().Foreground(styles.Primary)
		nameNormalStyle := lipgloss.NewStyle().Foreground(styles.Secondary)
		nameSelectedStyle := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)

		maxVisible := openInMaxVisible
		visibleCount := len(apps)
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}
		scrollOffset := m.openInScroll

		var sb strings.Builder
		focusables := make([]modal.FocusableInfo, 0, visibleCount)
		lineOffset := 0

		// Scroll indicator (top)
		if scrollOffset > 0 {
			sb.WriteString(styles.Muted.Render(fmt.Sprintf("  ↑ %d more above", scrollOffset)))
			sb.WriteString("\n")
			lineOffset++
		}

		for i := scrollOffset; i < scrollOffset+visibleCount && i < len(apps); i++ {
			app := apps[i]
			isCursor := i == m.openInCursor
			itemID := openInItemID(i)
			isHovered := itemID == hoverID

			// Cursor indicator
			if isCursor {
				sb.WriteString(cursorStyle.Render("> "))
			} else {
				sb.WriteString("  ")
			}

			// Name styling
			var nameStyle lipgloss.Style
			if isCursor || isHovered {
				nameStyle = nameSelectedStyle
			} else {
				nameStyle = nameNormalStyle
			}

			sb.WriteString(nameStyle.Render(app.Name))

			// "(last)" badge
			if app.ID == m.openInLastID {
				sb.WriteString(styles.Muted.Render(" (last)"))
			}

			if i < scrollOffset+visibleCount-1 && i < len(apps)-1 {
				sb.WriteString("\n")
			}

			// Each item takes 1 line
			focusables = append(focusables, modal.FocusableInfo{
				ID:      itemID,
				OffsetX: 0,
				OffsetY: lineOffset + (i - scrollOffset),
				Width:   contentWidth,
				Height:  1,
			})
		}

		// Scroll indicator (bottom)
		remaining := len(apps) - (scrollOffset + visibleCount)
		if remaining > 0 {
			sb.WriteString("\n")
			sb.WriteString(styles.Muted.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		}

		return modal.RenderedSection{Content: sb.String(), Focusables: focusables}
	}, m.openInListUpdate)
}

// openInListUpdate handles key events for the Open In list.
func (m *Model) openInListUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	apps := m.openInApps
	if len(apps) == 0 {
		return "", nil
	}

	switch keyMsg.String() {
	case "up", "k", "ctrl+p":
		if m.openInCursor > 0 {
			m.openInCursor--
			m.openInScroll = openInEnsureCursorVisible(m.openInCursor, m.openInScroll, openInMaxVisible)
			m.openInModalWidth = 0 // Force modal rebuild for scroll
		}
		return "", nil

	case "down", "j", "ctrl+n":
		if m.openInCursor < len(apps)-1 {
			m.openInCursor++
			m.openInScroll = openInEnsureCursorVisible(m.openInCursor, m.openInScroll, openInMaxVisible)
			m.openInModalWidth = 0 // Force modal rebuild for scroll
		}
		return "", nil

	case "enter":
		if m.openInCursor >= 0 && m.openInCursor < len(apps) {
			return "select", nil
		}
		return "", nil
	}

	return "", nil
}

// openInHintsSection renders the help text for the Open In modal.
func (m *Model) openInHintsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var sb strings.Builder
		sb.WriteString("\n")
		sb.WriteString(styles.KeyHint.Render("enter"))
		sb.WriteString(styles.Muted.Render(" open  "))
		sb.WriteString(styles.KeyHint.Render("↑/↓"))
		sb.WriteString(styles.Muted.Render(" navigate  "))
		sb.WriteString(styles.KeyHint.Render("esc"))
		sb.WriteString(styles.Muted.Render(" cancel"))
		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// renderOpenInModal renders the Open In modal overlay on top of the given content.
func (m *Model) renderOpenInModal(content string) string {
	m.ensureOpenInModal()
	if m.openInModal == nil {
		return content
	}

	if m.openInMouseHandler == nil {
		m.openInMouseHandler = mouse.NewHandler()
	}
	modalContent := m.openInModal.Render(m.width, m.height, m.openInMouseHandler)
	return ui.OverlayModal(content, modalContent, m.width, m.height)
}

// handleOpenInMouse handles mouse events for the Open In modal.
func (m *Model) handleOpenInMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureOpenInModal()
	if m.openInModal == nil {
		return m, nil
	}
	if m.openInMouseHandler == nil {
		m.openInMouseHandler = mouse.NewHandler()
	}

	action := m.openInModal.HandleMouse(msg, m.openInMouseHandler)

	// Check if action is an item click
	if strings.HasPrefix(action, openInItemPrefix) {
		var idx int
		if _, err := fmt.Sscanf(action, openInItemPrefix+"%d", &idx); err == nil {
			if idx >= 0 && idx < len(m.openInApps) {
				m.openInCursor = idx
				cmd := m.confirmOpenIn()
				m.resetOpenIn()
				m.updateContext()
				return m, cmd
			}
		}
		return m, nil
	}

	switch action {
	case "cancel":
		m.resetOpenIn()
		m.updateContext()
		return m, nil
	case "select":
		cmd := m.confirmOpenIn()
		m.resetOpenIn()
		m.updateContext()
		return m, cmd
	}

	return m, nil
}

// confirmOpenIn launches the selected app and saves the preference.
func (m *Model) confirmOpenIn() tea.Cmd {
	if m.openInCursor < 0 || m.openInCursor >= len(m.openInApps) {
		return nil
	}

	selected := m.openInApps[m.openInCursor]
	workDir := m.ui.WorkDir

	// Launch the app
	var launchErr error
	if selected.ID == "finder" {
		launchErr = exec.Command("open", workDir).Start()
	} else {
		// Use the bundle name (sans .app) for open -a, since display names
		// may differ from bundle names (e.g., "VS Code" vs "Visual Studio Code").
		appArg := selected.Name
		if selected.FoundBundle != "" {
			appArg = strings.TrimSuffix(selected.FoundBundle, ".app")
		}
		launchErr = exec.Command("open", "-a", appArg, workDir).Start()
	}

	if launchErr != nil {
		appName := selected.Name
		return func() tea.Msg {
			return ToastMsg{Message: "Failed to open " + appName, Duration: 3 * time.Second, IsError: true}
		}
	}

	// Save preference: use project path if available, otherwise workDir
	projectPath := workDir
	if pc := m.currentProjectConfig(); pc != nil {
		projectPath = pc.Path
	}
	_ = config.SaveLastOpenInApp(projectPath, selected.ID)

	// Update in-memory config
	if m.cfg != nil {
		m.cfg.UI.LastOpenInApp = selected.ID
		if pc := m.currentProjectConfig(); pc != nil {
			pc.LastOpenInApp = selected.ID
		}
	}

	appName := selected.Name
	return func() tea.Msg {
		return ToastMsg{Message: "Opened in " + appName, Duration: 2 * time.Second}
	}
}

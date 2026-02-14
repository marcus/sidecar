package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	projectAddNameID   = "project-add-name"
	projectAddPathID   = "project-add-path"
	projectAddThemeID  = "project-add-theme"
	projectAddAddID    = "project-add-add"
	projectAddCancelID = "project-add-cancel"
)

// ensureProjectAddModal builds/rebuilds the project add modal.
func (m *Model) ensureProjectAddModal() {
	modalW := 50
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 30 {
		modalW = 30
	}

	// Only rebuild if modal doesn't exist or width changed
	if m.projectAddModal != nil && m.projectAddModalWidth == modalW {
		return
	}
	m.projectAddModalWidth = modalW

	m.projectAddModal = modal.New("Add Project",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(m.projectAddNameSection()).
		AddSection(modal.Spacer()).
		AddSection(m.projectAddPathSection()).
		AddSection(modal.Spacer()).
		AddSection(m.projectAddThemeSection()).
		AddSection(modal.When(func() bool { return m.projectAdd != nil && m.projectAdd.errorMessage != "" }, m.projectAddErrorSection())).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Add ", projectAddAddID, modal.BtnPrimary()),
			modal.Btn(" Cancel ", projectAddCancelID),
		)).
		AddSection(m.projectAddHintsSection())
}

// clearProjectAddModal clears the modal state.
func (m *Model) clearProjectAddModal() {
	m.projectAddModal = nil
	m.projectAddModalWidth = 0
}

// projectAddNameSection renders the name input field.
func (m *Model) projectAddNameSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if m.projectAdd == nil {
			return modal.RenderedSection{}
		}
		var sb strings.Builder

		sb.WriteString("Name:")
		sb.WriteString("\n")

		// Sync textinput focus state with modal focus
		isFocused := focusID == projectAddNameID
		if isFocused {
			m.projectAdd.nameInput.Focus()
		} else {
			m.projectAdd.nameInput.Blur()
		}

		// Input field style based on focus
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.TextMuted).
			Padding(0, 1)
		if isFocused {
			inputStyle = inputStyle.BorderForeground(styles.Primary)
		}

		sb.WriteString(inputStyle.Render(m.projectAdd.nameInput.View()))

		return modal.RenderedSection{
			Content: sb.String(),
			Focusables: []modal.FocusableInfo{{
				ID:      projectAddNameID,
				OffsetX: 0,
				OffsetY: 1, // After the label line
				Width:   contentWidth,
				Height:  3, // Border + content + border
			}},
		}
	}, m.projectAddNameUpdate)
}

// projectAddNameUpdate handles key events for the name input.
func (m *Model) projectAddNameUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if focusID != projectAddNameID {
		return "", nil
	}
	if m.projectAdd == nil {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	// Clear error on typing
	m.projectAdd.errorMessage = ""
	m.projectAddModalWidth = 0 // Force rebuild to hide error

	var cmd tea.Cmd
	m.projectAdd.nameInput, cmd = m.projectAdd.nameInput.Update(keyMsg)
	return "", cmd
}

// projectAddPathSection renders the path input field.
func (m *Model) projectAddPathSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if m.projectAdd == nil {
			return modal.RenderedSection{}
		}
		var sb strings.Builder

		sb.WriteString("Path:")
		sb.WriteString("\n")

		// Sync textinput focus state with modal focus
		isFocused := focusID == projectAddPathID
		if isFocused {
			m.projectAdd.pathInput.Focus()
		} else {
			m.projectAdd.pathInput.Blur()
		}

		// Input field style based on focus
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.TextMuted).
			Padding(0, 1)
		if isFocused {
			inputStyle = inputStyle.BorderForeground(styles.Primary)
		}

		sb.WriteString(inputStyle.Render(m.projectAdd.pathInput.View()))

		return modal.RenderedSection{
			Content: sb.String(),
			Focusables: []modal.FocusableInfo{{
				ID:      projectAddPathID,
				OffsetX: 0,
				OffsetY: 1, // After the label line
				Width:   contentWidth,
				Height:  3, // Border + content + border
			}},
		}
	}, m.projectAddPathUpdate)
}

// projectAddPathUpdate handles key events for the path input.
func (m *Model) projectAddPathUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if focusID != projectAddPathID {
		return "", nil
	}
	if m.projectAdd == nil {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	// Clear error on typing
	m.projectAdd.errorMessage = ""
	m.projectAddModalWidth = 0 // Force rebuild to hide error

	var cmd tea.Cmd
	m.projectAdd.pathInput, cmd = m.projectAdd.pathInput.Update(keyMsg)
	return "", cmd
}

// projectAddThemeSection renders the theme selector field.
func (m *Model) projectAddThemeSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var sb strings.Builder

		sb.WriteString("Theme:")
		sb.WriteString("\n")

		themeValue := "(use global)"
		if m.projectAdd != nil && m.projectAdd.themeSelected != "" {
			themeValue = m.projectAdd.themeSelected
		}

		// Field style based on focus/hover
		fieldStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.TextMuted).
			Padding(0, 1)
		if focusID == projectAddThemeID || hoverID == projectAddThemeID {
			fieldStyle = fieldStyle.BorderForeground(styles.Primary)
		}

		sb.WriteString(fieldStyle.Render(themeValue))

		return modal.RenderedSection{
			Content: sb.String(),
			Focusables: []modal.FocusableInfo{{
				ID:      projectAddThemeID,
				OffsetX: 0,
				OffsetY: 1, // After the label line
				Width:   contentWidth,
				Height:  3, // Border + content + border
			}},
		}
	}, m.projectAddThemeUpdate)
}

// projectAddThemeUpdate handles key events for the theme field.
func (m *Model) projectAddThemeUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if focusID != projectAddThemeID {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	if keyMsg.String() == "enter" {
		return "open-theme-picker", nil
	}

	return "", nil
}

// projectAddErrorSection renders the error message.
func (m *Model) projectAddErrorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		errStyle := lipgloss.NewStyle().Foreground(styles.Error)
		if m.projectAdd == nil {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: errStyle.Render(m.projectAdd.errorMessage)}
	}, nil)
}

// projectAddHintsSection renders the help text.
func (m *Model) projectAddHintsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var sb strings.Builder

		sb.WriteString(styles.KeyHint.Render("tab"))
		sb.WriteString(styles.Muted.Render(" next  "))
		sb.WriteString(styles.KeyHint.Render("enter"))
		sb.WriteString(styles.Muted.Render(" confirm  "))
		sb.WriteString(styles.KeyHint.Render("esc"))
		sb.WriteString(styles.Muted.Render(" back"))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// renderProjectAddModal renders the project add modal using the modal library.
func (m *Model) renderProjectAddModal(content string) string {
	// If theme picker is open, render it on top
	if m.projectAddThemeMode {
		return m.renderProjectAddThemePickerOverlay(content)
	}

	m.ensureProjectAddModal()
	if m.projectAddModal == nil {
		return content
	}

	if m.projectAddMouseHandler == nil {
		m.projectAddMouseHandler = mouse.NewHandler()
	}
	modalContent := m.projectAddModal.Render(m.width, m.height, m.projectAddMouseHandler)
	return ui.OverlayModal(content, modalContent, m.width, m.height)
}

// handleProjectAddModalKeys handles keyboard input for the project add modal.
func (m *Model) handleProjectAddModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If theme picker is open, handle it separately
	if m.projectAddThemeMode {
		return m.handleProjectAddThemePickerKeys(msg)
	}

	m.ensureProjectAddModal()
	if m.projectAddModal == nil {
		return m, nil
	}

	action, cmd := m.projectAddModal.HandleKey(msg)

	switch action {
	case "cancel", projectAddCancelID:
		m.resetProjectAdd()
		return m, nil
	case "open-theme-picker":
		m.initProjectAddThemePicker()
		return m, nil
	case projectAddAddID:
		if errMsg := m.validateProjectAdd(); errMsg != "" {
			if m.projectAdd != nil {
				m.projectAdd.errorMessage = errMsg
			}
			m.projectAddModalWidth = 0 // Force rebuild to show error
			return m, nil
		}
		saveCmd := m.saveProjectAdd()
		m.resetProjectAdd()
		return m, saveCmd
	}

	return m, cmd
}

// handleProjectAddModalMouse handles mouse events for the project add modal.
func (m *Model) handleProjectAddModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// If theme picker is open, let it handle mouse
	if m.projectAddThemeMode {
		return m.handleProjectAddThemePickerMouse(msg)
	}

	m.ensureProjectAddModal()
	if m.projectAddModal == nil {
		return m, nil
	}

	if m.projectAddMouseHandler == nil {
		m.projectAddMouseHandler = mouse.NewHandler()
	}

	action := m.projectAddModal.HandleMouse(msg, m.projectAddMouseHandler)

	switch action {
	case projectAddCancelID:
		m.resetProjectAdd()
		return m, nil
	case projectAddThemeID:
		m.initProjectAddThemePicker()
		return m, nil
	case projectAddAddID:
		if errMsg := m.validateProjectAdd(); errMsg != "" {
			if m.projectAdd != nil {
				m.projectAdd.errorMessage = errMsg
			}
			m.projectAddModalWidth = 0 // Force rebuild to show error
			return m, nil
		}
		saveCmd := m.saveProjectAdd()
		m.resetProjectAdd()
		return m, saveCmd
	}

	return m, nil
}

// handleProjectAddThemePickerMouse handles mouse events for theme picker.
func (m *Model) handleProjectAddThemePickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Theme picker doesn't have dedicated mouse handling yet
	return m, nil
}

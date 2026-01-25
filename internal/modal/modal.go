package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
)

// Modal represents a declarative modal dialog with automatic hit region management.
type Modal struct {
	title           string
	variant         Variant
	width           int
	sections        []Section
	showHints       bool
	primaryAction   string
	closeOnBackdrop bool

	// State (managed internally)
	focusIdx     int      // Current focused element index in focusIDs
	hoverID      string   // Currently hovered element ID
	focusIDs     []string // Ordered list of focusable IDs (built during Render)
	scrollOffset int      // Content scroll position in lines
}

// New creates a new Modal with the given title and options.
func New(title string, opts ...Option) *Modal {
	m := &Modal{
		title:           title,
		variant:         VariantDefault,
		width:           DefaultWidth,
		showHints:       true,
		closeOnBackdrop: true,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// AddSection adds a section to the modal. Returns the modal for chaining.
func (m *Modal) AddSection(s Section) *Modal {
	m.sections = append(m.sections, s)
	return m
}

// Render renders the modal and registers hit regions.
// Returns the styled modal content string.
func (m *Modal) Render(screenW, screenH int, handler *mouse.Handler) string {
	return m.buildLayout(screenW, screenH, handler)
}

// HandleKey processes keyboard input.
// Returns:
//   - action: the action ID if triggered ("cancel" for Esc, button/input ID for Enter, etc.)
//   - cmd: any tea.Cmd from bubbles models (cursor blink, etc.)
func (m *Modal) HandleKey(msg tea.KeyMsg) (action string, cmd tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		return "cancel", nil

	case "tab":
		m.cycleFocus(1)
		return "", nil

	case "shift+tab":
		m.cycleFocus(-1)
		return "", nil

	case "enter":
		// Enter on a focused element triggers that element's action
		focusID := m.currentFocusID()
		if focusID != "" {
			// Route to focused section first
			action, cmd = m.routeToFocusedSection(msg)
			if action != "" {
				return action, cmd
			}
			// If section didn't return an action, use the focus ID or primary action
			if m.primaryAction != "" {
				return m.primaryAction, cmd
			}
			return focusID, cmd
		}
		return "", nil

	default:
		// Route other keys to the focused section
		return m.routeToFocusedSection(msg)
	}
}

// HandleMouse processes mouse input.
// Returns the action ID if a clickable element was clicked, empty string otherwise.
func (m *Modal) HandleMouse(msg tea.MouseMsg, handler *mouse.Handler) string {
	action := handler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick:
		if action.Region == nil {
			return ""
		}
		id := action.Region.ID

		// Backdrop click optionally dismisses the modal.
		if id == "modal-backdrop" {
			if m.closeOnBackdrop {
				return "cancel"
			}
			return ""
		}

		// Body clicks absorb but don't trigger actions.
		if id == "modal-body" {
			return ""
		}

		// Click on a focusable element - focus it and return its ID as action
		for i, fid := range m.focusIDs {
			if fid == id {
				m.focusIdx = i
				return id
			}
		}
		return ""

	case mouse.ActionHover:
		if action.Region != nil && action.Region.ID != "modal-backdrop" && action.Region.ID != "modal-body" {
			m.hoverID = action.Region.ID
		} else {
			m.hoverID = ""
		}
		return ""

	case mouse.ActionScrollUp:
		if action.Region != nil && action.Region.ID == "modal-body" {
			m.scrollOffset = max(0, m.scrollOffset-3)
		}
		return ""

	case mouse.ActionScrollDown:
		if action.Region != nil && action.Region.ID == "modal-body" {
			m.scrollOffset += 3
			// Clamping happens in buildLayout
		}
		return ""
	}

	return ""
}

// SetFocus sets focus to a specific element by ID.
func (m *Modal) SetFocus(id string) {
	for i, fid := range m.focusIDs {
		if fid == id {
			m.focusIdx = i
			return
		}
	}
}

// FocusedID returns the currently focused element ID.
func (m *Modal) FocusedID() string {
	return m.currentFocusID()
}

// HoveredID returns the currently hovered element ID.
func (m *Modal) HoveredID() string {
	return m.hoverID
}

// Reset resets the modal state (focus, hover, scroll).
func (m *Modal) Reset() {
	m.focusIdx = 0
	m.hoverID = ""
	m.scrollOffset = 0
}

// currentFocusID returns the ID of the currently focused element.
func (m *Modal) currentFocusID() string {
	if len(m.focusIDs) == 0 {
		return ""
	}
	if m.focusIdx < 0 || m.focusIdx >= len(m.focusIDs) {
		return m.focusIDs[0]
	}
	return m.focusIDs[m.focusIdx]
}

// cycleFocus moves focus by delta (1 for next, -1 for previous).
func (m *Modal) cycleFocus(delta int) {
	if len(m.focusIDs) == 0 {
		return
	}
	m.focusIdx = (m.focusIdx + delta + len(m.focusIDs)) % len(m.focusIDs)
}

// routeToFocusedSection routes a key message to the focused section.
func (m *Modal) routeToFocusedSection(msg tea.KeyMsg) (string, tea.Cmd) {
	focusID := m.currentFocusID()
	if focusID == "" {
		return "", nil
	}

	// Find which section contains this focus ID and route to it
	for _, section := range m.sections {
		action, cmd := section.Update(msg, focusID)
		if action != "" || cmd != nil {
			return action, cmd
		}
	}
	return "", nil
}

package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// Section is the interface for modal content sections.
type Section interface {
	// Render returns the rendered section content and focusable elements.
	// contentWidth is the available width for content (modal width minus border/padding).
	// focusID is the ID of the currently focused element (for styling).
	// hoverID is the ID of the currently hovered element (for styling).
	Render(contentWidth int, focusID, hoverID string) RenderedSection

	// Update handles input when this section contains the focused element.
	// Returns action string if the input triggers an action, plus any tea.Cmd.
	Update(msg tea.Msg, focusID string) (action string, cmd tea.Cmd)
}

// RenderedSection is the result of rendering a section.
type RenderedSection struct {
	Content    string          // Rendered string content
	Focusables []FocusableInfo // Focusable elements with hit region info
}

// FocusableInfo describes a focusable element within a section.
type FocusableInfo struct {
	ID      string // Unique identifier for this element
	OffsetX int    // X offset relative to section top-left (within content area)
	OffsetY int    // Y offset relative to section top-left (within content area)
	Width   int    // Width in characters
	Height  int    // Height in lines
}

// --- Text Section ---

// textSection is a static text section.
type textSection struct {
	text string
}

// Text creates a static text section.
func Text(s string) Section {
	return &textSection{text: s}
}

func (t *textSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	// Wrap text to content width
	wrapped := wrapText(t.text, contentWidth)
	return RenderedSection{Content: wrapped}
}

func (t *textSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	return "", nil
}

// --- Spacer Section ---

// spacerSection renders a blank line.
type spacerSection struct{}

// Spacer creates a blank line section.
func Spacer() Section {
	return &spacerSection{}
}

func (s *spacerSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	// Use a single space so measureHeight reports a 1-line spacer.
	return RenderedSection{Content: " "}
}

func (s *spacerSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	return "", nil
}

// --- When Section ---

// whenSection conditionally renders another section.
type whenSection struct {
	condition func() bool
	inner     Section
}

// When creates a conditional section that only renders when condition() returns true.
func When(condition func() bool, section Section) Section {
	return &whenSection{condition: condition, inner: section}
}

func (w *whenSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if !w.condition() {
		return RenderedSection{Content: ""}
	}
	return w.inner.Render(contentWidth, focusID, hoverID)
}

func (w *whenSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if !w.condition() {
		return "", nil
	}
	return w.inner.Update(msg, focusID)
}

// --- Custom Section ---

// customSection allows escape-hatch for complex custom content.
type customSection struct {
	renderFn   func(contentWidth int, focusID, hoverID string) RenderedSection
	updateFn   func(msg tea.Msg, focusID string) (string, tea.Cmd)
	focusables []FocusableInfo
}

// CustomRenderFunc is the signature for custom section render functions.
type CustomRenderFunc func(contentWidth int, focusID, hoverID string) RenderedSection

// CustomUpdateFunc is the signature for custom section update functions.
type CustomUpdateFunc func(msg tea.Msg, focusID string) (action string, cmd tea.Cmd)

// Custom creates a custom section with user-provided render and update functions.
// If updateFn is nil, updates are no-ops.
func Custom(renderFn CustomRenderFunc, updateFn CustomUpdateFunc) Section {
	return &customSection{
		renderFn: renderFn,
		updateFn: updateFn,
	}
}

func (c *customSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if c.renderFn == nil {
		return RenderedSection{}
	}
	return c.renderFn(contentWidth, focusID, hoverID)
}

func (c *customSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if c.updateFn == nil {
		return "", nil
	}
	return c.updateFn(msg, focusID)
}

// --- Buttons Section ---

// ButtonDef defines a button in a button row.
type ButtonDef struct {
	Label    string
	ID       string
	IsDanger bool
}

// BtnOption is a functional option for buttons.
type BtnOption func(*ButtonDef)

// Btn creates a button definition.
func Btn(label, id string, opts ...BtnOption) ButtonDef {
	b := ButtonDef{Label: label, ID: id}
	for _, opt := range opts {
		opt(&b)
	}
	return b
}

// BtnDanger marks the button as a danger/destructive action.
func BtnDanger() BtnOption {
	return func(b *ButtonDef) {
		b.IsDanger = true
	}
}

// BtnPrimary is a no-op for compatibility (primary styling is default for focused).
func BtnPrimary() BtnOption {
	return func(b *ButtonDef) {}
}

// buttonsSection renders a row of buttons.
type buttonsSection struct {
	buttons []ButtonDef
}

// Buttons creates a button row section.
func Buttons(btns ...ButtonDef) Section {
	return &buttonsSection{buttons: btns}
}

func (b *buttonsSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if len(b.buttons) == 0 {
		return RenderedSection{}
	}

	var sb strings.Builder
	focusables := make([]FocusableInfo, 0, len(b.buttons))
	currentX := 0

	for i, btn := range b.buttons {
		if i > 0 {
			sb.WriteString("  ") // Button spacing
			currentX += 2
		}

		// Determine button style
		style := b.resolveStyle(btn, focusID, hoverID)
		rendered := style.Render(btn.Label)
		sb.WriteString(rendered)

		// Calculate visual width (ANSI-stripped)
		visualWidth := ansi.StringWidth(rendered)

		focusables = append(focusables, FocusableInfo{
			ID:      btn.ID,
			OffsetX: currentX,
			OffsetY: 0,
			Width:   visualWidth,
			Height:  1,
		})

		currentX += visualWidth
	}

	return RenderedSection{
		Content:    sb.String(),
		Focusables: focusables,
	}
}

func (b *buttonsSection) resolveStyle(btn ButtonDef, focusID, hoverID string) lipgloss.Style {
	isFocused := btn.ID == focusID
	isHovered := btn.ID == hoverID

	if btn.IsDanger {
		if isFocused {
			return styles.ButtonDangerFocused
		}
		if isHovered {
			return styles.ButtonDangerHover
		}
		return styles.ButtonDanger
	}

	if isFocused {
		return styles.ButtonFocused
	}
	if isHovered {
		return styles.ButtonHover
	}
	return styles.Button
}

func (b *buttonsSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	// Buttons don't handle key input directly - that's handled by the modal
	return "", nil
}

// --- Checkbox Section ---

// checkboxSection renders a toggleable checkbox.
type checkboxSection struct {
	id      string
	label   string
	checked *bool
}

// Checkbox creates a checkbox section.
func Checkbox(id, label string, checked *bool) Section {
	return &checkboxSection{id: id, label: label, checked: checked}
}

func (c *checkboxSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	box := "[ ]"
	if c.checked != nil && *c.checked {
		box = "[x]"
	}

	isFocused := c.id == focusID
	isHovered := c.id == hoverID

	var style lipgloss.Style
	if isFocused {
		style = styles.ButtonFocused
	} else if isHovered {
		style = styles.ButtonHover
	} else {
		style = styles.Button
	}

	content := style.Render(box + " " + c.label)
	visualWidth := ansi.StringWidth(content)

	return RenderedSection{
		Content: content,
		Focusables: []FocusableInfo{{
			ID:      c.id,
			OffsetX: 0,
			OffsetY: 0,
			Width:   visualWidth,
			Height:  1,
		}},
	}
}

func (c *checkboxSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if c.id != focusID {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	switch keyMsg.String() {
	case "enter", " ":
		if c.checked != nil {
			*c.checked = !*c.checked
		}
		// Checkboxes don't return an action on toggle
		return "", nil
	}

	return "", nil
}

// --- Helper functions ---

// wrapText wraps text to fit within the given width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	var result []string

	for _, line := range lines {
		if ansi.StringWidth(line) <= width {
			result = append(result, line)
			continue
		}

		// Simple word wrapping
		words := strings.Fields(line)
		var currentLine string
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if ansi.StringWidth(currentLine+" "+word) <= width {
				currentLine += " " + word
			} else {
				result = append(result, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			result = append(result, currentLine)
		}
	}

	return strings.Join(result, "\n")
}

// measureHeight returns the number of lines in rendered content.
// Trims trailing newlines and returns 0 for empty content.
func measureHeight(content string) int {
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return 0
	}
	return lipgloss.Height(trimmed)
}

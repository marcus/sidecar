package modal

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// ScrollOwnerSection is implemented by sections that own their own scroll
// state (a document viewport, a nested list) rather than moving the modal
// body's offset. When the pointer is over a region such a section claims,
// Modal.WheelAtBoundary delegates the answer to it.
//
// Both methods must be read-only: they may not rebuild or mutate content.
// ScrollAtBoundary follows the same semantics as Modal.WheelAtBoundary - true
// only when applying delta is certainly a no-op, false for movable or unknown.
type ScrollOwnerSection interface {
	Section

	// OwnsScrollRegion reports whether the given mouse hit region ID belongs to
	// this section's scrollable area.
	OwnsScrollRegion(regionID string) bool

	// ScrollAtBoundary reports whether delta lines (negative up, positive down)
	// cannot move this section's own scroll state.
	ScrollAtBoundary(delta int) bool
}

// NaturalWidthSection is implemented by a section that has a width it would
// rather be given than be truncated to. A segmented Select is the case that
// forced it: its shape is a single row of labels, so a modal that is narrower
// than that row either truncates the labels into stubs or drops the control to
// its list shape, and both are decisions the host should be making with the
// number in front of it.
//
// The reported width is a CONTENT width — what the section wants inside the
// modal's border and padding. It is a wish, not a floor: a host still caps it
// to the frame, and a section handed less must still draw something sensible.
type NaturalWidthSection interface {
	Section

	// NaturalWidth is the content width this section would rather have, or 0
	// for a section that fills whatever column it is handed.
	NaturalWidth() int
}

// NaturalWidth is the widest natural width among sections, or 0 when none of
// them asks for one. A When wrapper answers for its inner section only while
// its condition holds, so a hidden control does not widen a modal.
func NaturalWidth(sections ...Section) int {
	widest := 0
	for _, s := range sections {
		n, ok := asNaturalWidth(s)
		if !ok {
			continue
		}
		if w := n.NaturalWidth(); w > widest {
			widest = w
		}
	}
	return widest
}

// WidthForSections is the modal width that holds every section at its natural
// width: the widest control, the box's own border and padding, and the column a
// scrollbar takes when the body is taller than the surface — because a modal
// that fits its widest control only until the body scrolls is a modal that
// changes shape as it fills up. It returns 0 when no section asks for a width;
// a host takes the larger of this and its own default, and caps the result to
// the frame it floats over.
func WidthForSections(sections ...Section) int {
	widest := NaturalWidth(sections...)
	if widest <= 0 {
		return 0
	}
	return widest + ModalPadding + ScrollbarColumns
}

// asNaturalWidth resolves a section to its natural width, seeing through When
// wrappers whose condition currently holds.
func asNaturalWidth(s Section) (NaturalWidthSection, bool) {
	for {
		if w, ok := s.(*whenSection); ok {
			if !w.condition() {
				return nil, false
			}
			s = w.inner
			continue
		}
		n, ok := s.(NaturalWidthSection)
		return n, ok
	}
}

// asScrollOwner resolves a section to its scroll owner, seeing through When
// wrappers whose condition currently holds.
func asScrollOwner(s Section) (ScrollOwnerSection, bool) {
	for {
		if w, ok := s.(*whenSection); ok {
			if !w.condition() {
				return nil, false
			}
			s = w.inner
			continue
		}
		owner, ok := s.(ScrollOwnerSection)
		return owner, ok
	}
}

// RenderedSection is the result of rendering a section.
type RenderedSection struct {
	Content    string            // Rendered string content
	Focusables []FocusableInfo   // Focusable elements with hit region info
	Overlay    *Overlay          // Optional floating layer; does not affect section height
	Scrollbar  *SectionScrollbar // Interactive scrollbar drawn into this section's content (see scrollbar.go)
}

// Overlay is drawn over later sections and does not count toward modal height.
// Focusables are relative to the overlay's top-left, not the section.
type Overlay struct {
	Content    string
	OffsetX    int
	OffsetY    int // preferred top relative to the section (typically just below it)
	Focusables []FocusableInfo
}

// FocusableInfo describes a focusable element within a section.
type FocusableInfo struct {
	ID        string // Unique identifier for this element
	OffsetX   int    // X offset relative to section top-left (within content area)
	OffsetY   int    // Y offset relative to section top-left (within content area)
	Width     int    // Width in characters
	Height    int    // Height in lines
	MouseOnly bool   // Hit-tested but omitted from the Tab / focus order
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
	renderFn func(contentWidth int, focusID, hoverID string) RenderedSection
	updateFn func(msg tea.Msg, focusID string) (string, tea.Cmd)
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

// scrollingCustomSection is a Custom section that owns its own scroll state.
type scrollingCustomSection struct {
	customSection
	ownsRegion func(regionID string) bool
	atBoundary func(delta int) bool
}

// ScrollingCustom creates a Custom section that owns its own scroll state and
// can answer wheel-boundary questions for the regions it claims. ownsRegion
// receives a mouse hit region ID; atBoundary must be read-only and may return
// true only when delta is certainly a no-op. Either callback may be nil, in
// which case the section never claims the wheel.
func ScrollingCustom(renderFn CustomRenderFunc, updateFn CustomUpdateFunc, ownsRegion func(regionID string) bool, atBoundary func(delta int) bool) Section {
	return &scrollingCustomSection{
		customSection: customSection{renderFn: renderFn, updateFn: updateFn},
		ownsRegion:    ownsRegion,
		atBoundary:    atBoundary,
	}
}

func (s *scrollingCustomSection) OwnsScrollRegion(regionID string) bool {
	if s.ownsRegion == nil || s.atBoundary == nil {
		return false
	}
	return s.ownsRegion(regionID)
}

func (s *scrollingCustomSection) ScrollAtBoundary(delta int) bool {
	if s.atBoundary == nil {
		return false
	}
	return s.atBoundary(delta)
}

// --- Buttons Section ---

// ButtonDef defines a button in a button row.
type ButtonDef struct {
	Label    string
	ID       string
	IsDanger bool
	// Disabled buttons are drawn muted, are skipped by focus, and never
	// return their action. A refusal the user can see beats a click that
	// quietly does nothing.
	Disabled bool
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

// BtnDisabled marks the button unavailable: muted, unfocusable, and inert to
// both Enter and a click.
func BtnDisabled() BtnOption {
	return func(b *ButtonDef) {
		b.Disabled = true
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

		// A disabled button registers nothing: Tab passes over it and a click
		// lands on no target, so refusal is structural rather than a check
		// every caller has to remember.
		if !btn.Disabled {
			focusables = append(focusables, FocusableInfo{
				ID:      btn.ID,
				OffsetX: currentX,
				OffsetY: 0,
				Width:   visualWidth,
				Height:  1,
			})
		}

		currentX += visualWidth
	}

	return RenderedSection{
		Content:    sb.String(),
		Focusables: focusables,
	}
}

func (b *buttonsSection) resolveStyle(btn ButtonDef, focusID, hoverID string) lipgloss.Style {
	if btn.Disabled {
		return styles.Muted
	}
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
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return "", nil
	}

	// Enter on a focused button returns that button's ID as the action
	if keyMsg.String() == "enter" {
		for _, btn := range b.buttons {
			if btn.ID == focusID && !btn.Disabled {
				return btn.ID, nil
			}
		}
	}
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

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return "", nil
	}

	switch keyMsg.String() {
	case " ", "space":
		if c.checked != nil {
			*c.checked = !*c.checked
		}
		return actionOverlayIdle, nil
	case "enter":
		// Submit the modal without flipping. Space is the toggle.
		return actionSubmitPrimary, nil
	}

	return "", nil
}

// --- Checkbox Display Section (non-focusable) ---

// checkboxDisplaySection renders a checkbox state indicator (not focusable).
// Use a keyboard shortcut to toggle the value externally.
type checkboxDisplaySection struct {
	label   string
	checked *bool
	hint    string
}

// CheckboxDisplay creates a non-focusable checkbox display.
// The hint shows the keyboard shortcut to toggle (e.g., "ctrl+a").
func CheckboxDisplay(label string, checked *bool, hint string) Section {
	return &checkboxDisplaySection{label: label, checked: checked, hint: hint}
}

func (c *checkboxDisplaySection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	box := "[ ]"
	if c.checked != nil && *c.checked {
		box = "[x]"
	}

	// Always use muted style since it's not focusable
	content := styles.Muted.Render(box + " " + c.label)

	// Add hint if provided
	if c.hint != "" {
		hintText := styles.Muted.Render(" (" + c.hint + ")")
		content += hintText
	}

	// Return empty Focusables slice - this element is not in tab order
	return RenderedSection{
		Content:    content,
		Focusables: nil,
	}
}

func (c *checkboxDisplaySection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	// Non-focusable, so no updates handled here
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

// clampLines holds every line of a section's content to contentWidth, so the
// height the layout measures is the height the box actually renders at.
//
// The box is drawn with a lipgloss Width, which wraps anything wider than the
// content column onto another row — but that happens after the layout has
// counted the section's newline-separated lines and reserved rows for them, so
// every wrapped line costs a row nobody budgeted and pushes the bottom border
// out of the surface entirely. A section that overflows its column is a bug in
// the section; clamping here is what keeps that bug from eating the border, and
// what makes "measured height == rendered height" true rather than usually true.
func clampLines(content string, contentWidth int) string {
	if content == "" || contentWidth < 1 {
		return content
	}
	lines := strings.Split(content, "\n")
	changed := false
	for i, line := range lines {
		if ansi.StringWidth(line) <= contentWidth {
			continue
		}
		lines[i] = ansi.Truncate(line, contentWidth, "…")
		changed = true
	}
	if !changed {
		return content
	}
	return strings.Join(lines, "\n")
}

// measureHeight returns the number of lines the content occupies when the
// layout joins sections with newlines: trailing blank lines included, so
// measured height == rendered height — the same invariant clampLines keeps
// for width. A section that ends its content with "\n" (a padding row, a
// deliberate gap) paints that line when joined, so it must pay for it here;
// undercounting by one made the viewport slice the last real section — the
// action row — off the bottom of the modal.
func measureHeight(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

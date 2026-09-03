package modal

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// SelectItem is one choice in a Select.
type SelectItem struct {
	ID    string // Unique hit-region identifier for this choice
	Label string // The word the choice is known by
	// Description is the aligned second column of the list shape. The
	// segmented shape has no room for it and leaves it out.
	Description string
	Data        any // Optional associated data
}

// SelectShape is how a Select draws itself.
type SelectShape int

const (
	// ShapeAuto picks by item count: the segmented toggle under five choices,
	// the list at five or more, and the list whenever the segments would not
	// fit the width they are given.
	ShapeAuto SelectShape = iota
	// ShapeSegmented forces [ A | B | C ].
	ShapeSegmented
	// ShapeList forces the ❯-cursor column. A surface whose labels are
	// sentences, or whose twin surface's are, forces it so the two match.
	ShapeList
)

const (
	selectSeparator  = " | "
	selectFrameOpen  = "["
	selectFrameClose = "]"

	// selectListMinItems is the count past which the segmented toggle becomes
	// the vertical list with aligned descriptions.
	selectListMinItems = 5
)

// SelectOption is an option for Select sections.
type SelectOption interface {
	applySelect(*selectSection)
}

type selectOptionFunc func(*selectSection)

func (f selectOptionFunc) applySelect(s *selectSection) { f(s) }

// WithShape forces one of the two shapes instead of choosing by item count.
func WithShape(shape SelectShape) SelectOption {
	return selectOptionFunc(func(s *selectSection) { s.shape = shape })
}

// WithDisabled marks choices that cannot be picked right now, answering per
// index WHY not — the empty string for a choice that can. A disabled choice
// stays visible and muted, with its reason in place of its description, so the
// rule is read before the row is entered; the keyboard steps over it and a
// click on it changes nothing.
func WithDisabled(reason func(i int) string) SelectOption {
	return selectOptionFunc(func(s *selectSection) { s.disabled = reason })
}

// WithOnSelect reports every selection change — by key or by click — to the
// caller, for a control whose choice has consequences beyond the index (the
// Create Workspace kind list rebuilds its form around it).
func WithOnSelect(fn func(i int)) SelectOption {
	return selectOptionFunc(func(s *selectSection) { s.onSelect = fn })
}

// WithSelectAction fixes the action a row activation returns, instead of the
// activated row's own ID. A selector embedded in a form uses it so a click
// still reports the control rather than a row the host has no branch for.
func WithSelectAction(action string) SelectOption {
	return selectOptionFunc(func(s *selectSection) { s.action = action })
}

// selectSection is the app's one single-choice control: a segmented toggle
// while the choices are few, the ❯-cursor list once they are many, with
// choiceList's mechanics underneath either shape.
type selectSection struct {
	core     choiceList
	items    []SelectItem
	shape    SelectShape
	disabled func(int) string
	onSelect func(int)
	action   string
}

// Select creates the single-choice control: pick one of items, with selected
// holding the chosen index.
//
// It draws as a segmented [ A | B | C ] while the choices are few and as a
// full-width ❯-cursor list once they are many, because a row of segments stops
// being readable at about five. Movement stops at the ends rather than
// wrapping, a click resolves to a row inside the section with no help from the
// host, and WithMaxVisible scrolls anything past it with the more-above and
// more-below markers.
func Select(id string, items []SelectItem, selected *int, opts ...SelectOption) Section {
	s := &selectSection{items: items}
	s.core = choiceList{
		id:          id,
		selectedIdx: selected,
		count:       func() int { return len(s.items) },
		itemID:      func(i int) string { return s.items[i].ID },
	}
	for _, opt := range opts {
		opt.applySelect(s)
	}
	s.core.selectable = func(i int) bool { return s.reason(i) == "" }
	return s
}

// reason is why choice i cannot be picked, empty when it can.
func (s *selectSection) reason(i int) string {
	if s.disabled == nil || i < 0 || i >= len(s.items) {
		return ""
	}
	return s.disabled(i)
}

// shapeFor answers which shape to draw in. The count rule is the design
// language's; the width check is the safety net under it, because a segmented
// row that does not fit is truncated into an unreadable stub and the list
// always fits.
func (s *selectSection) shapeFor(contentWidth int) SelectShape {
	switch s.shape {
	case ShapeSegmented, ShapeList:
		return s.shape
	}
	if len(s.items) >= selectListMinItems {
		return ShapeList
	}
	if contentWidth > 0 && s.segmentedWidth() > contentWidth {
		return ShapeList
	}
	return ShapeSegmented
}

// segmentedWidth is how wide the toggle draws before any truncation: the frame,
// every label, the separators between them, and the chrome each segment's own
// style puts around its label.
//
// That last term is the one that was missing. styles.Button pads a segment by
// two columns either side, so a three-segment control drew twelve columns wider
// than this function claimed — wide enough for the width floor below to say the
// toggle fitted while the renderer truncated it into "Up…". A width the control
// reports has to be the width it draws.
func (s *selectSection) segmentedWidth() int {
	w := ansi.StringWidth(selectFrameOpen) + ansi.StringWidth(selectFrameClose)
	for i, item := range s.items {
		w += segmentWidth(item.Label)
		if i < len(s.items)-1 {
			w += ansi.StringWidth(selectSeparator)
		}
	}
	return w
}

// segmentWidth is one drawn segment: its label, the space either side, and the
// row style's own padding. Every row style pads identically, so a segment does
// not change width when the selection or the pointer moves over it.
func segmentWidth(label string) int {
	return ansi.StringWidth(" "+label+" ") + selectRowStyle(false, false, false).GetHorizontalFrameSize()
}

// NaturalWidth is the content width this control would rather be given, for a
// host sizing a modal around it (see WidthForSections). The segmented shape
// asks for its whole label row, because the alternative to fitting it is
// truncating it; the list shape fills whatever column it is handed and so asks
// for nothing.
func (s *selectSection) NaturalWidth() int {
	if len(s.items) == 0 {
		return 0
	}
	switch s.shape {
	case ShapeList:
		return 0
	case ShapeSegmented:
		return s.segmentedWidth()
	}
	if len(s.items) >= selectListMinItems {
		return 0
	}
	return s.segmentedWidth()
}

func (s *selectSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if len(s.items) == 0 {
		return RenderedSection{Content: styles.Muted.Render("(no choices)")}
	}
	focused := focusID == s.core.id
	hovered := hoverID == s.core.id || s.core.indexOf(hoverID) >= 0
	rowHovered := func(i int) bool {
		// Hovering the control as a whole (its frame, or its Tab-stop region)
		// lights every row it could move to; hovering one row lights that row.
		return hovered && (hoverID == s.core.id || s.items[i].ID == hoverID)
	}
	if s.shapeFor(contentWidth) == ShapeSegmented {
		return s.renderSegmented(contentWidth, focused, hovered, rowHovered)
	}
	return s.renderList(contentWidth, focused, hovered, rowHovered)
}

// renderSegmented draws [ A | B | C ] with one hit region per segment. The
// first segment owns the opening frame and the last owns everything to the
// right edge, so a click on the frame or past the end keeps the nearest
// choice rather than falling through to the section.
func (s *selectSection) renderSegmented(contentWidth int, focused, hovered bool, rowHovered func(int) bool) RenderedSection {
	frame := selectFrameStyle(focused, hovered)
	parts := make([]string, 0, len(s.items)*2+2)
	parts = append(parts, frame.Render(selectFrameOpen))
	regions := make([]FocusableInfo, 0, len(s.items)+1)
	x := ansi.StringWidth(selectFrameOpen)
	sep := ansi.StringWidth(selectSeparator)
	for i, item := range s.items {
		selected := i == s.core.selected()
		style := selectRowStyle(s.reason(i) != "", selected, rowHovered(i) && !selected)
		if i > 0 {
			parts = append(parts, styles.Muted.Render(selectSeparator))
		}
		parts = append(parts, style.Render(" "+item.Label+" "))

		start, end := x, x+segmentWidth(item.Label)
		if i < len(s.items)-1 {
			// The separator belongs to the choice on its left, so no click
			// between two choices misses both.
			end += sep
		} else if contentWidth > end {
			end = contentWidth
		}
		if i == 0 {
			start = 0
		}
		regions = append(regions, FocusableInfo{
			ID: item.ID, OffsetX: start, OffsetY: 0,
			Width: max(1, end-start), Height: 1, MouseOnly: true,
		})
		x = end
	}
	parts = append(parts, frame.Render(selectFrameClose))
	content := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	if contentWidth > 0 && ansi.StringWidth(content) > contentWidth {
		content = ansi.Truncate(content, contentWidth, "…")
	}
	// The control's own Tab stop first, so the per-segment regions above win
	// HitMap.Test where they overlap it.
	focusables := append([]FocusableInfo{s.core.sectionFocusable(contentWidth, 1)}, regions...)
	return RenderedSection{Content: content, Focusables: focusables}
}

// renderList draws one row per choice: the ❯ cursor, the label column aligned
// across every choice, then the description column. A disabled row keeps its
// place with its reason where the description was. The rows and their
// more-above / more-below markers sit inside a border whose colour is the
// control's focus state, so a modal holding three of these says which one has
// the keyboard rather than leaving three gold rows to be told apart.
func (s *selectSection) renderList(contentWidth int, focused, hovered bool, rowHovered func(int) bool) RenderedSection {
	border := selectListBorderStyle(focused, hovered)
	frameX := border.GetHorizontalFrameSize()
	frameLeft := border.GetBorderLeftSize()
	frameTop := border.GetBorderTopSize()
	inner := contentWidth
	if contentWidth > 0 {
		inner = max(1, contentWidth-frameX)
	}

	// The label column is measured over every choice, not just the visible
	// window, so scrolling does not shift the descriptions sideways.
	labelW := 0
	for _, item := range s.items {
		if w := ansi.StringWidth(item.Label); w > labelW {
			labelW = w
		}
	}
	start, visible := s.core.window()
	lines := make([]string, 0, visible)
	for i := 0; i < visible; i++ {
		idx := start + i
		if idx >= len(s.items) {
			break
		}
		item := s.items[idx]
		reason := s.reason(idx)
		selected := idx == s.core.selected()
		cursor := "  "
		if selected {
			cursor = "❯ "
		}
		desc := item.Description
		if reason != "" {
			desc = reason
		}
		text := cursor + item.Label + strings.Repeat(" ", labelW-ansi.StringWidth(item.Label)) + "   " + desc
		style := selectRowStyle(reason != "", selected, rowHovered(idx) && !selected)
		lines = append(lines, renderSelectRow(style, text, inner))
	}
	content := strings.Join(lines, "\n")
	if inner > 0 {
		content = truncateSelectLines(content, inner)
	}
	// The rows are laid out in the box's own coordinates first, markers
	// included, and then moved inside the border in one step — so the border is
	// one offset applied to every region rather than a term each caller of
	// rowFocusables has to remember.
	rows := s.core.rowFocusables(inner, start, visible, true)
	content, rows = s.core.withMarkers(content, rows, start, visible)
	if contentWidth > 0 {
		// lipgloss counts the border inside Width, so the box is asked for the
		// whole content column and the rows were sized to what is left of it.
		content = border.Width(contentWidth).Render(content)
	} else {
		content = border.Render(content)
	}
	for i := range rows {
		rows[i].OffsetX += frameLeft
		rows[i].OffsetY += frameTop
	}
	// The control's Tab stop covers the whole box, border included: hovering
	// the frame lights the control, and a click on a border cell focuses it
	// without landing on a row — a border is not a choice.
	section := s.core.sectionFocusable(contentWidth, measureHeight(content))
	return RenderedSection{Content: content, Focusables: append([]FocusableInfo{section}, rows...)}
}

// selectListBorderStyle is the box around the list shape. It follows the same
// ladder a modal input's border does — BorderNormal idle, Primary focused,
// TextMuted hovered — because a selector is a control the keyboard lands on and
// nothing else in a modal says so. The selected row keeps its Primary fill
// whether or not the control has focus, so the control still says which choice
// is active when the keyboard is elsewhere; the border is what says where the
// keyboard is.
func selectListBorderStyle(focused, hovered bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(selectFrameStyle(focused, hovered).GetForeground())
}

// renderSelectRow draws one list row across the whole content column. A row's
// fill is what separates it from its neighbours, so every row has to reach the
// same right edge: chrome sized to the text leaves the list with a ragged edge
// whose shape is an accident of the longest description.
func renderSelectRow(style lipgloss.Style, text string, contentWidth int) string {
	if contentWidth < 1 {
		return style.Render(text)
	}
	inner := contentWidth - style.GetHorizontalFrameSize()
	if inner < 1 {
		return style.Render(text)
	}
	if ansi.StringWidth(text) > inner {
		text = ansi.Truncate(text, inner, "…")
	}
	return style.Width(contentWidth).Render(text)
}

func truncateSelectLines(content string, contentWidth int) string {
	if content == "" || contentWidth < 1 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > contentWidth {
			lines[i] = ansi.Truncate(line, contentWidth, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// selectDisabledSelected is the selected-but-unavailable row: the selected
// row's chrome so the control still says which choice is active, in muted text
// so it still says that choice cannot be acted on. It is a function rather
// than a value so it reads the colour at render time and follows a theme
// change.
func selectDisabledSelected() lipgloss.Style {
	return styles.ButtonHover.Foreground(styles.TextMuted)
}

func selectRowStyle(disabled, selected, hovered bool) lipgloss.Style {
	if disabled && selected {
		// A disabled row that is still the active choice — the fields it
		// governs are drawn below it — must read as selected, or the control
		// shows nothing selected at all. Selected chrome, muted text.
		return selectDisabledSelected()
	}
	if disabled {
		// The unselected row's chrome with muted text: the control is one
		// filled block, and a row that dropped the fill would read as a hole
		// in it rather than as an unavailable choice.
		return styles.Button.Foreground(styles.TextMuted)
	}
	if selected {
		return styles.ButtonFocused
	}
	if hovered {
		return styles.ButtonHover
	}
	return styles.Button
}

// selectFrameStyle is the [ ] around the segmented shape. It uses the same
// colours as a modal input border so "this control is active" is not the same
// signal as "this choice is selected".
func selectFrameStyle(focused, hovered bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch {
	case focused:
		return s.Foreground(styles.Primary)
	case hovered:
		return s.Foreground(styles.TextMuted)
	default:
		return s.Foreground(styles.BorderNormal)
	}
}

func (s *selectSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if click, ok := msg.(overlayClickMsg); ok {
		return s.activate(s.core.indexOf(click.id))
	}
	if focusID != s.core.id || len(s.items) == 0 || s.core.selectedIdx == nil {
		return "", nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return "", nil
	}
	moved := false
	switch key.String() {
	case "left", "h", "up", "k":
		moved = s.core.move(-1)
	case "right", "l", "down", "j":
		moved = s.core.move(1)
	case "home":
		moved = s.core.jumpToEnd(-1)
	case "end":
		moved = s.core.jumpToEnd(1)
	case "enter":
		return s.activate(s.core.selected())
	default:
		return "", nil
	}
	if moved && s.onSelect != nil {
		s.onSelect(*s.core.selectedIdx)
	}
	return "", nil
}

// activate makes choice i the selection and reports it. A disabled choice is
// refused: the selection does not move, and the caller is told that nothing
// happened rather than being handed a choice it must then reject.
func (s *selectSection) activate(i int) (string, tea.Cmd) {
	if i < 0 || i >= len(s.items) {
		return "", nil
	}
	if s.reason(i) != "" {
		if s.action != "" {
			return s.action, nil
		}
		return actionOverlayIdle, nil
	}
	if s.core.selectedIdx != nil && *s.core.selectedIdx != i {
		*s.core.selectedIdx = i
		if s.onSelect != nil {
			s.onSelect(i)
		}
	}
	if s.action != "" {
		return s.action, nil
	}
	return s.items[i].ID, nil
}

// FocusIDForClick claims a click on one of this control's rows for the
// control's own Tab stop: the rows are mouse-only regions, and without this
// the arrows after a click would still be steering whatever had focus before
// the pointer moved.
func (s *selectSection) FocusIDForClick(id string) string {
	if s.core.indexOf(id) < 0 {
		return ""
	}
	return s.core.id
}

package modal

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/styles"
)

// ListItem represents an item in a list section.
type ListItem struct {
	ID    string // Unique identifier for this item
	Label string // Display text
	Data  any    // Optional associated data
}

// ListOption is an option for List sections. It is an interface rather than a
// function type so one option value can configure more than one kind of
// section: WithMaxVisible caps a List and a Select alike, and neither has to
// invent its own spelling of the same idea.
type ListOption interface {
	applyList(*listSection)
}

// listOptionFunc adapts a plain function to ListOption.
type listOptionFunc func(*listSection)

func (f listOptionFunc) applyList(s *listSection) { f(s) }

// maxVisibleOption caps how many rows a single-choice section draws at once.
// It configures both List and Select, which is why it is a named type.
type maxVisibleOption int

func (o maxVisibleOption) applyList(s *listSection)     { s.core.setMaxVisible(int(o)) }
func (o maxVisibleOption) applySelect(s *selectSection) { s.core.setMaxVisible(int(o)) }

// WithMaxVisible sets the maximum number of visible items. Anything past it
// scrolls, with the more-above and more-below markers saying so.
func WithMaxVisible(n int) maxVisibleOption {
	return maxVisibleOption(n)
}

// WithSingleFocus makes the list register as a single focusable unit for Tab navigation.
// When focused, j/k or up/down change selection within the list without Tab-cycling through each item.
// This is useful for lists that are part of a larger form where Tab should skip between sections.
// Note: This is now the default behavior. This option is kept for backward compatibility.
func WithSingleFocus() ListOption {
	return listOptionFunc(func(s *listSection) {
		s.singleFocus = true
	})
}

// WithPerItemFocus makes the list register each item as a separate focusable for Tab navigation.
// This overrides the default single-focus behavior when you want Tab to cycle through individual items.
func WithPerItemFocus() ListOption {
	return listOptionFunc(func(s *listSection) {
		s.singleFocus = false
	})
}

// choiceList is the mechanics every single-choice list shares: the scroll
// window, the per-row hit regions that let a click resolve to a row without
// help from the host, the more-above and more-below markers, and the movement
// keys that stop at the ends rather than wrapping. listSection and
// selectSection each drive one, so there is exactly one copy of these rules.
type choiceList struct {
	id           string
	selectedIdx  *int // may be nil: no selection
	maxVisible   int  // 0 draws every row
	scrollOffset int
	count        func() int
	itemID       func(int) string
	// selectable answers whether a row can be chosen at all. nil means every
	// row can; a Select with disabled rows says no for those, and they are
	// stepped over rather than landed on.
	selectable func(int) bool
}

func (c *choiceList) setMaxVisible(n int) {
	if n > 0 {
		c.maxVisible = n
	}
}

func (c *choiceList) selected() int {
	if c.selectedIdx == nil {
		return 0
	}
	return *c.selectedIdx
}

func (c *choiceList) canSelect(i int) bool {
	if i < 0 || i >= c.count() {
		return false
	}
	return c.selectable == nil || c.selectable(i)
}

// window scrolls the selection into view and returns the first visible row and
// how many rows are drawn.
func (c *choiceList) window() (int, int) {
	n := c.count()
	visible := n
	if c.maxVisible > 0 {
		visible = min(c.maxVisible, n)
	}
	sel := c.selected()
	if sel < c.scrollOffset {
		c.scrollOffset = sel
	} else if sel >= c.scrollOffset+visible {
		c.scrollOffset = sel - visible + 1
	}
	c.scrollOffset = clamp(c.scrollOffset, 0, max(0, n-visible))
	return c.scrollOffset, visible
}

// rowFocusables is one hit region per drawn row, so a click resolves inside the
// section instead of being mapped by whoever is hosting the modal.
func (c *choiceList) rowFocusables(contentWidth, start, visible int, mouseOnly bool) []FocusableInfo {
	regions := make([]FocusableInfo, 0, visible)
	for i := 0; i < visible; i++ {
		idx := start + i
		if idx >= c.count() {
			break
		}
		regions = append(regions, FocusableInfo{
			ID:        c.itemID(idx),
			OffsetX:   0,
			OffsetY:   i,
			Width:     contentWidth,
			Height:    1,
			MouseOnly: mouseOnly,
		})
	}
	return regions
}

// sectionFocusable is the list's own Tab stop, covering every drawn row. It is
// registered FIRST so the per-row regions win HitMap.Test.
func (c *choiceList) sectionFocusable(contentWidth, visible int) FocusableInfo {
	return FocusableInfo{
		ID:      c.id,
		OffsetX: 0,
		OffsetY: 0,
		Width:   contentWidth,
		Height:  visible,
	}
}

// withMarkers adds the "more above" and "more below" lines when the window does
// not hold everything, shifting the hit regions past the line it prepends.
func (c *choiceList) withMarkers(content string, focusables []FocusableInfo, start, visible int) (string, []FocusableInfo) {
	if start > 0 {
		content = styles.Muted.Render("↑ more above") + "\n" + content
		for i := range focusables {
			focusables[i].OffsetY++
		}
	}
	if start+visible < c.count() {
		content = content + "\n" + styles.Muted.Render("↓ more below")
	}
	return content, focusables
}

// move steps delta rows, skipping rows that cannot be selected and stopping at
// the ends rather than wrapping: the ends of a list are easier to feel than to
// count, and a wrap reads as a lost keypress. It reports whether it moved.
func (c *choiceList) move(delta int) bool {
	if c.selectedIdx == nil || delta == 0 {
		return false
	}
	idx := *c.selectedIdx
	for {
		idx += delta
		if idx < 0 || idx >= c.count() {
			return false
		}
		if c.canSelect(idx) {
			*c.selectedIdx = idx
			return true
		}
	}
}

// jumpToEnd moves to the first (dir < 0) or last (dir > 0) selectable row.
func (c *choiceList) jumpToEnd(dir int) bool {
	if c.selectedIdx == nil {
		return false
	}
	n := c.count()
	if n == 0 || dir == 0 {
		return false
	}
	idx := 0
	if dir > 0 {
		idx = n - 1
	}
	for idx >= 0 && idx < n {
		if c.canSelect(idx) {
			if *c.selectedIdx == idx {
				return false
			}
			*c.selectedIdx = idx
			return true
		}
		idx -= dir
	}
	return false
}

// indexOf resolves a row's hit-region ID back to its index.
func (c *choiceList) indexOf(id string) int {
	if id == "" {
		return -1
	}
	for i, n := 0, c.count(); i < n; i++ {
		if c.itemID(i) == id {
			return i
		}
	}
	return -1
}

// listSection renders a scrollable list of items.
type listSection struct {
	core        choiceList
	items       []ListItem
	singleFocus bool // If true, register as single focusable (Tab skips between sections, j/k changes selection)
}

// List creates a list section with selectable items.
// selectedIdx is a pointer to the currently selected index (can be nil for no selection).
//
// List is the low-level form: a column of rows a caller reads what it likes
// out of. A single choice among a known set — a sort, a filter, a kind — is a
// Select, which owns the two shapes and the disabled rule on top of these same
// mechanics.
func List(id string, items []ListItem, selectedIdx *int, opts ...ListOption) Section {
	s := &listSection{
		items:       items,
		singleFocus: true, // Default: Tab skips between sections, j/k changes selection
	}
	s.core = choiceList{
		id:          id,
		selectedIdx: selectedIdx,
		maxVisible:  5, // Default
		count:       func() int { return len(s.items) },
		itemID:      func(i int) string { return s.items[i].ID },
	}
	for _, opt := range opts {
		opt.applyList(s)
	}
	return s
}

func (s *listSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if len(s.items) == 0 {
		return RenderedSection{Content: styles.Muted.Render("(no items)")}
	}

	start, visibleCount := s.core.window()

	// In singleFocus mode, check if the list itself has focus
	listHasFocus := s.singleFocus && focusID == s.core.id

	var sb strings.Builder

	for i := 0; i < visibleCount; i++ {
		itemIdx := start + i
		if itemIdx >= len(s.items) {
			break
		}

		item := s.items[itemIdx]
		isSelected := s.core.selectedIdx != nil && *s.core.selectedIdx == itemIdx
		isHovered := item.ID == hoverID

		// Determine style
		var style lipgloss.Style
		if isSelected {
			style = styles.ListItemFocused
		} else if isHovered {
			style = styles.ListItemSelected
		} else {
			style = styles.ListItemNormal
		}

		// Render cursor - show when selected, or when list has focus and this is selected item
		cursor := "  "
		if isSelected {
			if listHasFocus {
				cursor = styles.ListCursor.Render("▸ ") // Filled cursor when list has focus
			} else {
				cursor = styles.ListCursor.Render("> ")
			}
		}

		// Render item
		line := cursor + style.Render(item.Label)
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
	}

	// Per-item hit targets: Tab still stops on the list ID in singleFocus,
	// but a click / hover must resolve to this row's ID.
	focusables := s.core.rowFocusables(contentWidth, start, visibleCount, s.singleFocus)

	// singleFocus: one Tab stop for the list, registered first so later
	// per-item regions win HitMap.Test.
	if s.singleFocus {
		focusables = append([]FocusableInfo{s.core.sectionFocusable(contentWidth, visibleCount)}, focusables...)
	}

	content, focusables := s.core.withMarkers(sb.String(), focusables, start, visibleCount)

	return RenderedSection{
		Content:    content,
		Focusables: focusables,
	}
}

func (s *listSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if click, ok := msg.(overlayClickMsg); ok {
		return s.activateItem(click.id)
	}

	// Check if the list or any of its items are focused
	isFocused := false
	if s.singleFocus {
		// In singleFocus mode, ONLY check if list ID matches (don't respond to individual item IDs)
		isFocused = focusID == s.core.id
	} else {
		// Otherwise, check if any item is focused
		for _, item := range s.items {
			if item.ID == focusID {
				isFocused = true
				break
			}
		}
	}
	if !isFocused {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return "", nil
	}

	if s.core.selectedIdx == nil {
		return "", nil
	}

	switch keyMsg.String() {
	case "up", "k":
		s.core.move(-1)
		return "", nil

	case "down", "j":
		s.core.move(1)
		return "", nil

	case "enter":
		if *s.core.selectedIdx >= 0 && *s.core.selectedIdx < len(s.items) {
			return s.activateItem(s.items[*s.core.selectedIdx].ID)
		}
		return "", nil

	case "home":
		s.core.jumpToEnd(-1)
		return "", nil

	case "end":
		s.core.jumpToEnd(1)
		return "", nil
	}

	return "", nil
}

func (s *listSection) activateItem(id string) (string, tea.Cmd) {
	i := s.core.indexOf(id)
	if i < 0 {
		return "", nil
	}
	if s.core.selectedIdx != nil {
		*s.core.selectedIdx = i
	}
	return s.items[i].ID, nil
}

package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// ListItem represents an item in a list section.
type ListItem struct {
	ID    string // Unique identifier for this item
	Label string // Display text
	Data  any    // Optional associated data
}

// ListOption is a functional option for List sections.
type ListOption func(*listSection)

// listSection renders a scrollable list of items.
type listSection struct {
	id           string
	items        []ListItem
	selectedIdx  *int // Pointer to allow external control
	maxVisible   int  // Maximum number of visible items
	scrollOffset int  // Current scroll position
}

// List creates a list section with selectable items.
// selectedIdx is a pointer to the currently selected index (can be nil for no selection).
func List(id string, items []ListItem, selectedIdx *int, opts ...ListOption) Section {
	s := &listSection{
		id:          id,
		items:       items,
		selectedIdx: selectedIdx,
		maxVisible:  5, // Default
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithMaxVisible sets the maximum number of visible items.
func WithMaxVisible(n int) ListOption {
	return func(s *listSection) {
		if n > 0 {
			s.maxVisible = n
		}
	}
}

func (s *listSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if len(s.items) == 0 {
		return RenderedSection{Content: styles.Muted.Render("(no items)")}
	}

	// Determine visible range
	visibleCount := min(s.maxVisible, len(s.items))
	selectedIdx := 0
	if s.selectedIdx != nil {
		selectedIdx = *s.selectedIdx
	}

	// Adjust scroll to keep selection visible
	if selectedIdx < s.scrollOffset {
		s.scrollOffset = selectedIdx
	} else if selectedIdx >= s.scrollOffset+visibleCount {
		s.scrollOffset = selectedIdx - visibleCount + 1
	}

	// Clamp scroll offset
	maxScroll := max(0, len(s.items)-visibleCount)
	s.scrollOffset = clamp(s.scrollOffset, 0, maxScroll)

	var sb strings.Builder
	focusables := make([]FocusableInfo, 0, visibleCount)

	for i := 0; i < visibleCount; i++ {
		itemIdx := s.scrollOffset + i
		if itemIdx >= len(s.items) {
			break
		}

		item := s.items[itemIdx]
		isSelected := s.selectedIdx != nil && *s.selectedIdx == itemIdx
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

		// Render cursor
		cursor := "  "
		if isSelected {
			cursor = styles.ListCursor.Render("> ")
		}

		// Render item
		line := cursor + style.Render(item.Label)
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)

		// Register focusable
		focusables = append(focusables, FocusableInfo{
			ID:      item.ID,
			OffsetX: 0,
			OffsetY: i,
			Width:   ansi.StringWidth(line),
			Height:  1,
		})
	}

	// Show scroll indicators if needed
	content := sb.String()
	hasTopIndicator := s.scrollOffset > 0
	if hasTopIndicator {
		content = styles.Muted.Render("\u2191 more above") + "\n" + content
		// Adjust focusable offsets since we prepended a line
		for i := range focusables {
			focusables[i].OffsetY++
		}
	}
	if s.scrollOffset+visibleCount < len(s.items) {
		content = content + "\n" + styles.Muted.Render("\u2193 more below")
	}

	return RenderedSection{
		Content:    content,
		Focusables: focusables,
	}
}

func (s *listSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	// Check if any of our items are focused
	isFocused := false
	for _, item := range s.items {
		if item.ID == focusID {
			isFocused = true
			break
		}
	}
	if !isFocused {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	if s.selectedIdx == nil {
		return "", nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if *s.selectedIdx > 0 {
			*s.selectedIdx--
		}
		return "", nil

	case "down", "j":
		if *s.selectedIdx < len(s.items)-1 {
			*s.selectedIdx++
		}
		return "", nil

	case "enter":
		// Return the selected item's ID as the action
		if *s.selectedIdx >= 0 && *s.selectedIdx < len(s.items) {
			return s.items[*s.selectedIdx].ID, nil
		}
		return "", nil

	case "home":
		*s.selectedIdx = 0
		return "", nil

	case "end":
		*s.selectedIdx = len(s.items) - 1
		return "", nil
	}

	return "", nil
}


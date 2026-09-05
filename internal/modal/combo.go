package modal

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// Internal actions consumed by Modal.HandleKey / HandleMouse.
const (
	actionDismissOverlay = "modal.dismiss-overlay"
	actionOverlayIdle    = "modal.overlay-idle"
	actionSubmitPrimary  = "modal.submit-primary"
)

const (
	comboItemIDSep    = "/item/"
	comboOverlayIDSfx = "/overlay"
)

// DropdownItem is one row in a Combo overlay.
type DropdownItem struct {
	ID    string
	Label string
	Value string // written into the input on commit; empty means Label
	Desc  string
	Data  any
}

// ComboFilterFunc reports whether item matches the current query.
type ComboFilterFunc func(query string, item DropdownItem) bool

// ComboOption configures a Combo section.
type ComboOption func(*comboSection)

type comboSection struct {
	id            string
	field         *inputSection
	items         []DropdownItem
	selected      *int // items index, same convention as List
	highlight     int  // index into filtered
	maxVisible    int
	openOnFocus   bool
	submitOnEnter bool
	filter        ComboFilterFunc

	open      bool
	dismissed bool
	focused   bool
	scroll    int
	filtered  []int
}

// Combo creates a text field whose filtered list floats over later sections.
// selected is an index into items (the same convention as List). Typing
// moves it to the top match's items index, not 0 unless that item is first.
func Combo(id string, input *textinput.Model, items []DropdownItem, selected *int, opts ...ComboOption) Section {
	s := &comboSection{
		id: id,
		field: &inputSection{
			id:            id,
			model:         input,
			submitOnEnter: false,
		},
		items:         items,
		selected:      selected,
		maxVisible:    8,
		openOnFocus:   true,
		submitOnEnter: true,
		filter:        defaultComboFilter,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithComboMaxVisible sets how many overlay rows are shown (default 8).
func WithComboMaxVisible(n int) ComboOption {
	return func(s *comboSection) {
		if n > 0 {
			s.maxVisible = n
		}
	}
}

// WithOpenOnFocus opens the overlay when the field gains focus (default true).
func WithOpenOnFocus(open bool) ComboOption {
	return func(s *comboSection) {
		s.openOnFocus = open
	}
}

// WithComboFilter replaces the default case-insensitive substring filter.
func WithComboFilter(fn ComboFilterFunc) ComboOption {
	return func(s *comboSection) {
		if fn != nil {
			s.filter = fn
		}
	}
}

// WithComboSubmitOnEnter submits the modal primary action after Enter commits
// the highlight (default true).
func WithComboSubmitOnEnter(submit bool) ComboOption {
	return func(s *comboSection) {
		s.submitOnEnter = submit
	}
}

func defaultComboFilter(query string, item DropdownItem) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(item.Label), q) {
		return true
	}
	if item.Value != "" && strings.Contains(strings.ToLower(item.Value), q) {
		return true
	}
	if item.Desc != "" && strings.Contains(strings.ToLower(item.Desc), q) {
		return true
	}
	return false
}

func comboItemID(comboID string, filteredIdx int) string {
	return comboID + comboItemIDSep + strconv.Itoa(filteredIdx)
}

func comboOverlayID(comboID string) string {
	return comboID + comboOverlayIDSfx
}

func parseComboItemID(comboID, id string) (int, bool) {
	prefix := comboID + comboItemIDSep
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(id[len(prefix):])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (s *comboSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	s.syncFocus(s.id == focusID)
	s.rebuildFilter()

	closed := s.field.Render(contentWidth, focusID, hoverID)
	if !s.open {
		return closed
	}

	ov := s.buildOverlay(contentWidth, hoverID)
	ov.OffsetY = measureHeight(closed.Content)
	return RenderedSection{
		Content:    closed.Content,
		Focusables: closed.Focusables,
		Overlay:    ov,
	}
}

func (s *comboSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	switch msg := msg.(type) {
	case overlayClickMsg:
		return s.handleItemClick(msg.id)
	case overlayCommitMsg:
		if s.id != focusID {
			return "", nil
		}
		s.commitIfOpen()
		return actionOverlayIdle, nil
	case overlayBlurMsg:
		s.commitIfOpen()
		return "", nil
	}

	if s.id != focusID {
		return "", nil
	}
	s.syncFocus(true)
	if s.field.model != nil {
		s.field.model.Focus()
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if s.field.model == nil {
			return "", nil
		}
		var cmd tea.Cmd
		*s.field.model, cmd = s.field.model.Update(msg)
		return "", cmd
	}

	switch keyMsg.String() {
	case "esc":
		if s.open {
			s.closeOverlay(true)
			return actionDismissOverlay, nil
		}
		return "", nil

	case "enter":
		s.commitIfOpen()
		if s.submitOnEnter {
			return actionSubmitPrimary, nil
		}
		return actionOverlayIdle, nil

	case "tab", "shift+tab":
		s.commitIfOpen()
		return actionOverlayIdle, nil

	case "up", "ctrl+p":
		s.ensureOpen()
		s.moveHighlight(-1)
		return actionOverlayIdle, nil

	case "down", "ctrl+n":
		s.ensureOpen()
		s.moveHighlight(1)
		return actionOverlayIdle, nil
	}

	old := s.query()
	var cmd tea.Cmd
	if s.field.model != nil {
		*s.field.model, cmd = s.field.model.Update(msg)
	}
	if s.query() != old {
		s.rebuildFilter()
		s.setFilteredHighlight(0)
		s.dismissed = false
		s.open = true
	}
	return "", cmd
}

func (s *comboSection) syncFocus(focused bool) {
	if focused && !s.focused {
		if s.openOnFocus && !s.dismissed {
			s.open = true
		}
	}
	if !focused && s.focused {
		s.open = false
		s.dismissed = false
	}
	s.focused = focused
}

func (s *comboSection) ensureOpen() {
	s.dismissed = false
	s.open = true
	s.rebuildFilter()
}

func (s *comboSection) closeOverlay(dismiss bool) {
	s.open = false
	s.dismissed = dismiss
}

func (s *comboSection) commitIfOpen() {
	if !s.open {
		return
	}
	s.commitHighlight()
	s.closeOverlay(true)
}

func (s *comboSection) query() string {
	if s.field.model == nil {
		return ""
	}
	return s.field.model.Value()
}

func (s *comboSection) rebuildFilter() {
	query := s.query()
	next := s.filtered[:0]
	if cap(next) < len(s.items) {
		next = make([]int, 0, len(s.items))
	}
	for i, item := range s.items {
		if s.filter == nil || s.filter(query, item) {
			next = append(next, i)
		}
	}
	s.filtered = next
	if fi := s.filteredPosOfSelected(); fi >= 0 {
		s.highlight = fi
		return
	}
	if n := len(s.filtered); n > 0 && s.highlight >= n {
		s.highlight = n - 1
	}
}

func (s *comboSection) filteredPos(itemIdx int) int {
	for i, idx := range s.filtered {
		if idx == itemIdx {
			return i
		}
	}
	return -1
}

func (s *comboSection) filteredPosOfSelected() int {
	if s.selected == nil {
		return -1
	}
	return s.filteredPos(*s.selected)
}

// filteredHighlight is the caret inside the current filtered list.
func (s *comboSection) filteredHighlight() int {
	if fi := s.filteredPosOfSelected(); fi >= 0 {
		return fi
	}
	if s.highlight < 0 {
		return 0
	}
	if n := len(s.filtered); n > 0 && s.highlight >= n {
		return n - 1
	}
	return s.highlight
}

// setFilteredHighlight moves the caret in the filtered list and writes the
// corresponding items index into selected.
func (s *comboSection) setFilteredHighlight(fi int) {
	n := len(s.filtered)
	if n == 0 {
		s.highlight = 0
		return
	}
	if fi < 0 {
		fi = 0
	}
	if fi >= n {
		fi = n - 1
	}
	s.highlight = fi
	if s.selected != nil {
		*s.selected = s.filtered[fi]
	}
}

func (s *comboSection) moveHighlight(delta int) {
	if len(s.filtered) == 0 {
		return
	}
	s.setFilteredHighlight(s.filteredHighlight() + delta)
}

func (s *comboSection) commitHighlight() {
	s.rebuildFilter()
	hi := s.filteredHighlight()
	if hi < 0 || hi >= len(s.filtered) {
		return
	}
	item := s.items[s.filtered[hi]]
	if s.field.model != nil {
		s.field.model.SetValue(itemCommitValue(item))
		s.field.model.CursorEnd()
	}
}

func (s *comboSection) handleItemClick(id string) (string, tea.Cmd) {
	if id == comboOverlayID(s.id) {
		return actionOverlayIdle, nil
	}
	idx, ok := parseComboItemID(s.id, id)
	if !ok {
		return "", nil
	}
	s.rebuildFilter()
	if idx < 0 || idx >= len(s.filtered) {
		return "", nil
	}
	s.setFilteredHighlight(idx)
	s.commitHighlight()
	s.closeOverlay(true)
	return actionOverlayIdle, nil
}

func (s *comboSection) visibleWindow() (start, count int) {
	n := len(s.filtered)
	count = min(s.maxVisible, n)
	if count < 1 {
		return 0, 0
	}
	hi := s.filteredHighlight()
	if hi < s.scroll {
		s.scroll = hi
	} else if hi >= s.scroll+count {
		s.scroll = hi - count + 1
	}
	s.scroll = clamp(s.scroll, 0, max(0, n-count))
	return s.scroll, count
}

func (s *comboSection) buildOverlay(contentWidth int, hoverID string) *Overlay {
	if contentWidth < 1 {
		contentWidth = 1
	}

	start, count := s.visibleWindow()
	if count == 0 {
		line := styles.Muted.Background(styles.BgTertiary).Width(contentWidth).Render("(no matches)")
		return &Overlay{
			Content: styles.FillBackground(line, contentWidth, styles.BgTertiary),
			Focusables: []FocusableInfo{{
				ID:      comboOverlayID(s.id),
				OffsetX: 0,
				OffsetY: 0,
				Width:   contentWidth,
				Height:  1,
			}},
		}
	}

	hi := s.filteredHighlight()
	lines := make([]string, 0, count)
	focusables := make([]FocusableInfo, 0, count)

	for i := 0; i < count; i++ {
		fi := start + i
		item := s.items[s.filtered[fi]]
		id := comboItemID(s.id, fi)
		cursor := "  "
		style := styles.ListItemNormal.Background(styles.BgTertiary)
		switch {
		case fi == hi:
			cursor = styles.ListCursor.Render("▸ ")
			style = styles.ListItemFocused
		case id == hoverID:
			style = styles.ListItemSelected
		}

		text := item.Label
		if item.Desc != "" {
			text += "  " + item.Desc
		}
		plain := cursor + text
		if ansi.StringWidth(plain) > contentWidth {
			plain = ansi.Truncate(plain, contentWidth, "…")
		}
		line := style.Width(contentWidth).Render(plain)
		lines = append(lines, line)
		focusables = append(focusables, FocusableInfo{
			ID:      id,
			OffsetX: 0,
			OffsetY: i,
			Width:   contentWidth,
			Height:  1,
		})
	}

	content := styles.FillBackground(strings.Join(lines, "\n"), contentWidth, styles.BgTertiary)
	return &Overlay{
		Content:    content,
		Focusables: focusables,
	}
}

func itemCommitValue(item DropdownItem) string {
	if item.Value != "" {
		return item.Value
	}
	return item.Label
}

// Messages used by Modal to talk to Combo without forwarding raw keys
// (Tab must not reach textinput / textarea).
type overlayClickMsg struct{ id string }
type overlayCommitMsg struct{}
type overlayBlurMsg struct{}

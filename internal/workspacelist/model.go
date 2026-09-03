package workspacelist

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedscroll "github.com/marcus/sidecar/internal/scroll"
	"github.com/marcus/sidecar/internal/styles"
)

// RegionKind names a mouse target the list drew. Regions are reported from the
// same layout that rendered the rows, so a hit test can never disagree with
// what is on screen.
type RegionKind string

const (
	RegionRow    RegionKind = "workspacelist-row"
	RegionSort   RegionKind = "workspacelist-sort"
	RegionFilter RegionKind = "workspacelist-filter"
	// RegionFilterClear is the × drawn at the right of a non-empty query row.
	// It is registered only where it is drawn, so the columns it would occupy
	// belong to the filter row itself whenever there is nothing to clear.
	RegionFilterClear RegionKind = "workspacelist-filter-clear"
)

type Region struct {
	Kind          RegionKind
	ID            string
	X, Y, W, H    int
	VisibleIndex  int
	SectionHeader bool
	Data          any
}

// Model is the list state a consumer owns: the catalog projection, the chosen
// sort, the filter, and the selection/viewport. Selection is by stable ID, so a
// refresh, a filter keystroke, or a sort change moves the cursor with the item
// rather than with the row number.
type Model struct {
	items    []Item
	sortMode Sort
	filter   Filter

	selectedID string
	scroll     int
	// freeScroll latches that the viewport is where a scrollbar gesture put
	// it, so renders stop parking the selected row back into view. Selection
	// movement clears it; see Model.SetScrollViewport.
	freeScroll      bool
	visible         []Item
	rows            int
	loading         bool
	failures        []string
	emptyText       string
	emptyLines      []string
	emptyActionID   string
	emptyActionLine int
	pinnedIDs       []string
	pulseFrame      int
	headerAction    *SidebarAction
	sectionActions  map[string]*SidebarAction
	sortNote        string
}

// SetSortNote marks the sort control with state the sort itself does not
// describe. The global browser uses it for HostHiddenGlyph: a list missing a
// machine's rows has to say so on the control that can put them back, or the
// absence reads as the machine having nothing to show.
func (m *Model) SetSortNote(note string) { m.sortNote = note }

// SetCreateActions supplies presentation-only create affordances. Section
// actions are keyed by Section.Key, which is a stable project identity under
// Project sort. Nil removes the corresponding affordance and hit region.
func (m *Model) SetCreateActions(header *SidebarAction, sections map[string]*SidebarAction) {
	m.headerAction = header
	m.sectionActions = sections
}

// SetPulseFrame advances the working/blocked marker animation. Consumers that
// never tick leave it at zero and get the steady first frame.
func (m *Model) SetPulseFrame(frame int) { m.pulseFrame = frame }

// NeedsPulse reports whether any currently visible row carries a lane that
// breathes, so a consumer can keep its animation clock off when nothing on
// screen would move.
func (m *Model) NeedsPulse() bool {
	for _, item := range m.visible {
		if PulseLane(item.Marker.Lane) {
			return true
		}
	}
	return false
}

func (m *Model) Filter() *Filter { return &m.filter }

func (m *Model) Sort() Sort { return m.sortMode }

func (m *Model) SetSort(mode Sort) {
	m.sortMode = mode
	m.reproject()
}

// CycleSort advances `s` through Activity → Project → Recent → Name.
func (m *Model) CycleSort() { m.SetSort(m.sortMode.Next()) }

// SetLoading marks that inventory is still arriving. Rows already collected
// stay on screen: incremental results are the point.
func (m *Model) SetLoading(loading bool) { m.loading = loading }

// SetFailures records per-project unavailability rows. They are presentation
// only; the list never retries or repairs anything.
func (m *Model) SetFailures(failures []string) {
	m.failures = append([]string(nil), failures...)
}

func (m *Model) SetEmptyText(text string) {
	m.emptyText = text
	m.emptyLines = nil
	m.emptyActionID = ""
	m.emptyActionLine = 0
}

// SetEmptyState replaces the catalog-empty copy with already-styled lines and
// an optional pressable row. SetEmptyText is the one-line form.
func (m *Model) SetEmptyState(lines []string, actionID string, actionLine int) {
	m.emptyLines = append([]string(nil), lines...)
	m.emptyActionID = actionID
	m.emptyActionLine = actionLine
	m.emptyText = ""
}

// SetItems replaces the catalog projection, preserving the selected identity.
func (m *Model) SetItems(items []Item) {
	m.items = append([]Item(nil), items...)
	m.reproject()
}

// SetPinned replaces the pin order. IDs that are not in the current catalog
// are kept until the next reproject so a later SetItems can restore them;
// display only includes pins that still have a matching item.
func (m *Model) SetPinned(ids []string) {
	m.pinnedIDs = uniquePinned(ids)
	m.reproject()
}

// PinnedIDs returns the configured pin order, including IDs that are not
// currently visible.
func (m *Model) PinnedIDs() []string { return append([]string(nil), m.pinnedIDs...) }

// IsPinned reports whether id is in the pin list.
func (m *Model) IsPinned(id string) bool {
	for _, existing := range m.pinnedIDs {
		if existing == id {
			return true
		}
	}
	return false
}

// TogglePin pins or unpins id and returns the new pin order. First-pinned
// first: a new pin is appended.
func (m *Model) TogglePin(id string) []string {
	if id == "" {
		return m.PinnedIDs()
	}
	for i, existing := range m.pinnedIDs {
		if existing == id {
			m.pinnedIDs = append(m.pinnedIDs[:i:i], m.pinnedIDs[i+1:]...)
			m.reproject()
			return m.PinnedIDs()
		}
	}
	m.pinnedIDs = append(m.pinnedIDs, id)
	m.reproject()
	return m.PinnedIDs()
}

func uniquePinned(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (m *Model) reproject() {
	previous := m.selectedID
	matched := Filtered(m.items, m.filter.Query())
	pinned, rest := splitPinned(matched, m.pinnedIDs)
	m.visible = append(pinned, Sorted(rest, m.sortMode)...)
	if previous != "" && m.indexOf(previous) >= 0 {
		m.selectedID = previous
		m.ensureVisible()
		return
	}
	if len(m.visible) == 0 {
		m.selectedID, m.scroll = "", 0
		return
	}
	m.selectedID = m.visible[0].ID
	m.scroll = 0
}

func (m *Model) indexOf(id string) int {
	for i, item := range m.visible {
		if item.ID == id {
			return i
		}
	}
	return -1
}

func (m *Model) Items() []Item   { return append([]Item(nil), m.items...) }
func (m *Model) Visible() []Item { return append([]Item(nil), m.visible...) }
func (m *Model) Counts() (matched, total int) {
	return len(m.visible), len(m.items)
}

func (m *Model) Selected() (Item, bool) {
	index := m.indexOf(m.selectedID)
	if index < 0 {
		return Item{}, false
	}
	return m.visible[index], true
}

func (m *Model) SelectedID() string { return m.selectedID }

// SelectID moves the cursor to a stable identity when it is visible.
func (m *Model) SelectID(id string) bool {
	if m.indexOf(id) < 0 {
		return false
	}
	m.selectedID = id
	m.ensureVisible()
	return true
}

// Move steps the selection, clamping at both ends rather than wrapping.
func (m *Model) Move(delta int) bool {
	if len(m.visible) == 0 {
		return false
	}
	index := m.indexOf(m.selectedID)
	if index < 0 {
		index = 0
	}
	next := MoveIndex(index, delta, len(m.visible))
	if next == index {
		return false
	}
	m.selectedID = m.visible[next].ID
	m.ensureVisible()
	return true
}

func (m *Model) Top() bool {
	if len(m.visible) == 0 {
		return false
	}
	changed := m.selectedID != m.visible[0].ID
	m.selectedID = m.visible[0].ID
	m.scroll = 0
	m.freeScroll = false
	return changed
}

func (m *Model) Bottom() bool {
	if len(m.visible) == 0 {
		return false
	}
	last := m.visible[len(m.visible)-1].ID
	changed := m.selectedID != last
	m.selectedID = last
	m.freeScroll = false
	m.ensureVisible()
	return changed
}

// FilterKey applies a key while the filter owns focus and reprojects when the
// query changed.
func (m *Model) FilterKey(msg tea.KeyPressMsg) KeyResult {
	result := m.filter.HandleKey(msg)
	if result == KeyHandled || result == KeyExit {
		m.reproject()
	}
	return result
}

// Reproject re-runs filter and sort after a caller changed the query directly
// (a paste, a programmatic clear). Selection is preserved by identity.
func (m *Model) Reproject() { m.reproject() }

// FilterPaste puts a bracketed paste into a focused query and reprojects.
func (m *Model) FilterPaste(msg tea.PasteMsg) KeyResult {
	result := m.filter.HandlePaste(msg)
	if result == KeyHandled {
		m.reproject()
	}
	return result
}

// FocusFilter is the `/` entry point.
func (m *Model) FocusFilter() { m.filter.Focus() }

// ClearFilter empties the query and reprojects, keeping focus where it is. It
// is what the row's × and the filter-clear command run.
func (m *Model) ClearFilter() {
	m.filter.Clear()
	m.reproject()
}

func (m *Model) ensureVisible() {
	// Whatever moved the selection — a key, the wheel, a click, a refresh that
	// had to reselect — owns the viewport again.
	m.freeScroll = false
	if m.rows <= 0 {
		return
	}
	index := m.indexOf(m.selectedID)
	if index < 0 {
		return
	}
	if index < m.scroll {
		m.scroll = index
	} else if index >= m.scroll+m.rows {
		m.scroll = index - m.rows + 1
	}
	m.scroll = min(max(m.scroll, 0), max(0, len(m.visible)-m.rows))
}

// ScrollOffset is the first visible row index. Consumers read it to prove the
// viewport survived a refresh; nothing else depends on it.
func (m *Model) ScrollOffset() int { return m.scroll }

// Scroll follows the selected row. It remains as the compatibility entry point
// for callers that previously moved only the viewport; workspace wheel input
// must have the same selection-following semantics everywhere.
func (m *Model) Scroll(delta int) { m.Move(delta) }

// ScrollAtBoundary reports whether moving the selection by delta would leave
// this list unchanged. Wheel navigation follows selection, so its boundary is
// the first or last visible item rather than the viewport offset alone.
func (m *Model) ScrollAtBoundary(delta int) bool {
	if m == nil {
		return true
	}
	return (sharedscroll.Bounds{
		Position: m.indexOf(m.selectedID),
		Maximum:  len(m.visible) - 1,
	}).AtBoundary(delta)
}

// RenderOptions describes the box the list is drawn into.
type RenderOptions struct {
	Width, Height int
	Title         string
	Focused       bool
	Now           time.Time
	// ScrollbarHover / ScrollbarDrag carry the list bar's pointer emphasis
	// from the surface's mouse state into the draw.
	ScrollbarHover bool
	ScrollbarDrag  bool
}

// Rendered is the drawn list plus the regions it registered.
type Rendered struct {
	View    string
	Regions []Region
	// Scrollbar reports the interactive bar this pass drew, for surfaces
	// answering presses on its thumb/track regions.
	Scrollbar SidebarScrollbar
}

// twoLineWidth is the sidebar width below which a row degrades to one
// ANSI-safe truncated line instead of a name/subtitle pair.
const twoLineWidth = 34

// Render draws the list: header with the active sort, filter row, section
// headings, rows, and a scrollbar. Headings scroll with their rows so the
// heading a row sits under is always the heading above it.
func (m *Model) Render(opts RenderOptions) Rendered {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	matched, total := m.Counts()
	sections := GroupedAt(m.visible, m.sortMode, now, m.pinnedIDs)
	// A project whose inventory could not be read is a row, not a leftover. Its
	// lines are reserved out of the body before the item viewport is sized, so a
	// catalog longer than the pane — the normal multi-project case — cannot make
	// an unavailable project silently vanish. The reservation is bounded: a long
	// outage list collapses into a count rather than pushing the catalog off the
	// screen.
	failureRows := len(m.failures)
	if failureRows > 0 {
		limit := max(0, opts.Height-3)
		if len(m.visible) > 0 {
			limit = min(limit, max(1, opts.Height/3))
		}
		failureRows = min(failureRows, limit)
	}
	sidebarSections := make([]SidebarSection, 0, len(sections))
	for _, section := range sections {
		s := SidebarSection{Title: section.Title, ProjectKey: section.Key, Count: len(section.Items)}
		if action := m.sectionActions[section.Key]; action != nil {
			copy := *action
			s.Action = &copy
		}
		showProject := m.showsProjectInRow(section)
		for _, item := range section.Items {
			item := item
			s.Rows = append(s.Rows, SidebarRow{ID: item.ID, Data: item.Data, Render: func(width int, selected, focused bool) []string {
				return m.renderRow(item, selected, focused, width, now, showProject)
			}})
		}
		sidebarSections = append(sidebarSections, s)
	}
	empty := []string(nil)
	if len(m.visible) == 0 {
		// An empty list must say which kind of empty it is: a query that matches
		// nothing reads very differently from a catalog that is still loading.
		switch {
		case m.filter.Active():
			empty = []string{NoMatchRow(max(1, opts.Width-1), m.filter.Query())}
		case m.loading:
			empty = []string{styles.Muted.Render("Loading workspaces…")}
		case len(m.emptyLines) > 0:
			empty = append([]string(nil), m.emptyLines...)
		case m.emptyText != "":
			empty = []string{styles.Muted.Render(m.emptyText)}
		}
	}
	filterLine, filterClear := m.filter.RenderRow(opts.Width, matched, total)
	rendered := RenderSidebar(SidebarOptions{Width: opts.Width, Height: opts.Height, Title: opts.Title, Focused: opts.Focused,
		SelectedID: m.selectedID, ScrollOffset: m.scroll,
		// The global list's bar is live: its thumb/track regions ride along
		// with every content region, and a gesture-chosen offset is honored
		// rather than re-derived from the selection.
		InteractiveScrollbar: true,
		FreeScroll:           m.freeScroll,
		ScrollbarHover:       opts.ScrollbarHover,
		ScrollbarDrag:        opts.ScrollbarDrag,
		HeaderAction:         m.headerAction,
		HeaderMeta:           &SidebarAction{ID: "sort", Label: SortPillLabel(m.sortMode), Suffix: m.sortNote},
		// The filter row costs a row of chrome, so it appears when the filter is
		// live and not before — the rule the project sidebar already follows, so
		// the first heading sits on the same row on both surfaces.
		FilterActive: m.filter.Active(), FilterLine: filterLine, FilterClear: filterClear,
		Sections: sidebarSections, EmptyLines: empty,
		EmptyActionID: m.emptyActionID, EmptyActionLine: m.emptyActionLine,
		FooterLines: m.failureLines(failureRows, opts.Width)})
	m.scroll, m.rows = rendered.ScrollOffset, rendered.VisibleRows
	return Rendered{View: rendered.View, Regions: rendered.Regions, Scrollbar: rendered.Scrollbar}
}

// failureLines renders the per-project unavailable rows that fit in the space
// reserved for them. When more projects failed than there is room for, the last
// reserved line becomes a count: the user still learns that inventory is
// incomplete, which is the whole point of the row.
func (m *Model) failureLines(rows, width int) []string {
	if rows <= 0 || len(m.failures) == 0 {
		return nil
	}
	if len(m.failures) <= rows {
		out := make([]string, 0, len(m.failures))
		for _, failure := range m.failures {
			out = append(out, fit(styles.Muted.Render("⚠ "+failure), width))
		}
		return out
	}
	out := make([]string, 0, rows)
	for _, failure := range m.failures[:rows-1] {
		out = append(out, fit(styles.Muted.Render("⚠ "+failure), width))
	}
	remaining := len(m.failures) - (rows - 1)
	return append(out, fit(styles.Muted.Render(fmt.Sprintf("⚠ +%d more projects unavailable", remaining)), width))
}

// renderRow draws one global list item. Two lines where the width supports it:
// project + name with relative age, then kind glyph + agent. Status lives in
// the gutter marker and is not repeated as text. Project colour reinforces
// the project word but is never the only differentiator.
// showsProjectInRow decides whether rows in a section still need to name their
// project. Under Project sort the heading above them already does, and the
// repeat costs the row width it needs for the name and its detail. The Pinned
// section is the exception: it is not a project section, so its rows keep the
// prefix even under Project sort or they would be the only rows on screen with
// no project at all.
func (m *Model) showsProjectInRow(section Section) bool {
	return m.sortMode != SortProject || section.Key == ""
}

func (m *Model) renderRow(item Item, selected, focused bool, width int, now time.Time, showProject bool) []string {
	namePrefix := rowNamePrefix(item, showProject)
	after := make([]RowField, 0, 1)
	if item.Detail != "" {
		after = append(after, RowField{Text: item.Detail, Rendered: styles.Muted.Render(item.Detail)})
	}
	marker := item.Marker
	if icon, style, ok := PulseMarker(marker.Lane, m.pulseFrame); ok {
		marker.Icon, marker.Style, marker.HasStyle = icon, style, true
	}
	return RenderRow(RowPresentation{
		Marker:        marker,
		Kind:          item.Kind,
		Name:          item.Name,
		NamePrefix:    namePrefix,
		NameMeta:      item.NameMeta,
		Age:           RelativeAge(item.ChangedAt, now),
		Provider:      item.Provider,
		AfterProvider: after,
		Pinned:        m.IsPinned(item.ID),
	}, width, selected, focused)
}

// rowNamePrefix is everything that precedes a row's own name: the host glyph
// when the row came from another machine, then the project label when the
// section heading is not already carrying it.
//
// The host is deliberately NOT repeated as text. The global browser writes it
// into the project label already ("mini · api"), which is what the Project
// sort groups by and what the heading shows, so a second copy beside the name
// would say the same machine twice on the same line. What the row was missing
// is that the label reads as an ordinary project name: the glyph and the
// per-host colour are what make it read as a machine. Under the Project sort
// the label is hidden and the heading says the host instead, so the glyph
// stands alone — which is still the one thing a reader needs at a glance.
func rowNamePrefix(item Item, showProject bool) RowField {
	prefix := RowField{}
	hue := styles.ProjectHue(item.ProjectKey)
	if item.Host != "" {
		hue = HostHue(item.Host)
		prefix = RowField{
			Text:     HostGlyph + " ",
			Rendered: lipgloss.NewStyle().Foreground(hue).Render(HostGlyph) + " ",
		}
	}
	if !showProject || item.Project == "" {
		return prefix
	}
	return RowField{
		Text:     prefix.Text + item.Project + " ",
		Rendered: prefix.Rendered + lipgloss.NewStyle().Foreground(hue).Render(item.Project) + " ",
	}
}

func selectionStyle(focused bool) lipgloss.Style {
	if focused {
		return styles.ListItemSelected
	}
	// Focus changes the text hierarchy, not the selection geometry: selected
	// rows keep the same full-width BgTertiary fill on both shared surfaces.
	return lipgloss.NewStyle().Background(styles.BgTertiary).Foreground(styles.TextSecondary)
}

func fit(line string, width int) string {
	if width < 1 {
		return ""
	}
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	if gap := width - ansi.StringWidth(line); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

// RelativeAge formats freshness in the same small units the Agents board uses,
// so one item does not read "3m" on one tab and "3 minutes" on the other.
//
// Everything under a minute reads "now". A second-level countdown is a claim to
// precision this data cannot back: last-interaction time is inferred from tmux
// and session files and is routinely off by more than the digits it is showing.
// It also churned — every row that had just moved redrew its number once a
// second, so the weakest number on the surface was also the one drawing the eye.
// "now" is the honest form of the same fact and it holds still.
func RelativeAge(changedAt, now time.Time) string {
	if changedAt.IsZero() {
		return ""
	}
	d := now.Sub(changedAt)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// RegionAt resolves a click to the region drawn under it, last registered
// winning — the same rule the workspace plugin's hit map uses.
func RegionAt(regions []Region, x, y int) (Region, bool) {
	for i := len(regions) - 1; i >= 0; i-- {
		r := regions[i]
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return r, true
		}
	}
	return Region{}, false
}

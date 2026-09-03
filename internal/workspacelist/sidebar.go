package workspacelist

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// SidebarAction is an optional action painted in a header. Omitting it paints
// and registers nothing, which lets read-only consumers use the same sidebar
// without inheriting project workspace mutations.
type SidebarAction struct {
	ID      string
	Label   string
	Hovered bool
	// Suffix is a mark the control keeps at every degradation step, appended
	// after whatever is left of Label. It is for state the label does not
	// otherwise say — "some rows are being withheld" — which is precisely the
	// thing that must not vanish when the header runs out of room, since a
	// narrow panel is where a missing row is hardest to explain.
	Suffix string
}

// SidebarRow is one caller-owned record projected into the shared list. Data
// is returned unchanged in the row's Region so consumers never decode display
// text or a volatile visible index to recover their record.
type SidebarRow struct {
	ID     string
	Data   any
	Render func(width int, selected, focused bool) []string
}

// SidebarSection is a headed run of rows. The optional action is commonly the
// project's create-shell/create-worktree affordance; global sections omit it.
//
// Title is the bare name ("Shells", "Needs Attention") and Count is rendered
// beside it. They are kept apart rather than pre-joined so a narrow heading can
// drop the count and still name its rows. An empty Title means an unheaded run.
type SidebarSection struct {
	Title string
	// ProjectKey opts a project-group heading into the stable, theme-normalized
	// project hue. Category and time sections leave it empty and stay neutral.
	ProjectKey string
	Count      int
	Action     *SidebarAction
	Rows       []SidebarRow
}

// SidebarOptions contains only resolved presentation state. Collection,
// selection side effects, preview loading and mutations stay with the caller.
type SidebarOptions struct {
	Width, Height int
	Title         string
	Focused       bool
	SelectedID    string
	ScrollOffset  int
	HeaderAction  *SidebarAction
	HeaderMeta    *SidebarAction
	PrefixLines   []string
	FilterLine    string
	FilterActive  bool
	// FilterClear is where the filter row drew its × clear control, in the
	// row's own coordinates. The zero rect means the row drew none, and then
	// no region is registered for it.
	FilterClear mouse.Rect
	Sections    []SidebarSection
	EmptyLines  []string
	// EmptyActionID names one of EmptyLines as a pressable control and gives
	// it a hit region. An empty state that offers an action has to be
	// clickable for the same reason its key has to be advertised: the two
	// interaction models say the same thing or neither is trustworthy.
	EmptyActionID   string
	EmptyActionLine int
	FooterLines     []string
	// InteractiveScrollbar opts the bar into the pointer contract: thumb and
	// track Regions are appended after every content region — reverse-scan
	// priority, so a press on the bar is answered by the bar and never by a
	// row beneath its column — and the drawn geometry is reported for hit
	// registration. Nothing is registered when everything fits. Surfaces that
	// have not adopted pointer scrolling leave it false and get exactly the
	// draw-only bar of before.
	InteractiveScrollbar bool
	// FreeScroll reports that ScrollOffset was chosen by a pointer gesture
	// rather than derived from the selection, so the render clamps it without
	// dragging the selected row back into view — a drag past the selection
	// would otherwise be pulled straight back. Any selection move clears it.
	FreeScroll bool
	// ScrollbarHover / ScrollbarDrag feed the bar's pointer emphasis back into
	// the draw, following the divider's HandleState convention. Idle output
	// stays byte-identical to plain RenderScrollbar.
	ScrollbarHover bool
	ScrollbarDrag  bool
}

// SidebarRendered is the exact view, geometry and viewport produced by one
// layout pass. Regions use content-local coordinates; callers add their panel
// border/padding origin once when registering them.
type SidebarRendered struct {
	View         string
	Regions      []Region
	ScrollOffset int
	VisibleRows  int
	// Scrollbar reports the bar this pass drew at the list's right edge, for
	// surfaces answering presses on its regions. Has is false whenever no
	// thumb exists — everything fits, or no body was drawn.
	Scrollbar SidebarScrollbar
}

// SidebarScrollbar is what one render pass learned about the list's bar: the
// params it was drawn with and where its track and thumb landed, in content
// coordinates (the same space as Regions). A bar that was not drawn can never
// register regions or answer a press.
type SidebarScrollbar struct {
	Params   ui.ScrollbarParams
	TrackTop int // first row of the track, content-local
	ThumbTop int // thumb's offset within the track, in rows
	ThumbH   int // thumb height in rows
	Has      bool
}

const (
	RegionHeaderAction  RegionKind = "workspacelist-header-action"
	RegionSectionAction RegionKind = "workspacelist-section-action"
	RegionEmptyAction   RegionKind = "workspacelist-empty-action"
)

// The scrollbar's region kinds are the shared core's IDs, so every surface's
// hit map names a scrollbar identically and the shared gesture contract reads
// the same everywhere.
const (
	RegionScrollbarTrack RegionKind = RegionKind(ui.RegionScrollbarTrack)
	RegionScrollbarThumb RegionKind = RegionKind(ui.RegionScrollbarThumb)
)

// IsScrollbarRegion reports whether kind belongs to the list's bar rather than
// to its content.
func IsScrollbarRegion(kind RegionKind) bool {
	return kind == RegionScrollbarTrack || kind == RegionScrollbarThumb
}

// emptySpacerMinBody is how many body rows must survive the blank line under
// the panel header for an empty-state message to afford that breathing room.
const emptySpacerMinBody = 3

// emptySpacerFits reports whether a short pane can afford the blank line
// between the panel's chrome and an empty-state message. It sits below the
// filter row rather than directly under the title because the filter is part
// of the header's chrome.
func emptySpacerFits(opts SidebarOptions) bool {
	chrome := 1 + len(opts.PrefixLines)
	if opts.FilterActive {
		chrome++
	}
	chrome += min(len(opts.FooterLines), max(0, opts.Height-chrome))
	return opts.Height-chrome > emptySpacerMinBody
}

// listSpacerFits keeps one quiet row between the panel controls and list
// content, but never spends the row that a short pane needs to show its first
// complete heading and content row. The spacer is chrome: it stays fixed while the
// list scrolls and the scrollbar starts beside content, not beside empty air.
func listSpacerFits(opts SidebarOptions, flat []sidebarFlatRow, chromeRows int) bool {
	if len(flat) == 0 {
		return false
	}
	footerRows := min(len(opts.FooterLines), max(0, opts.Height-chromeRows))
	available := max(0, opts.Height-chromeRows-footerRows)
	if available < 2 {
		return false
	}

	// Use the first row the existing viewport would show without the spacer.
	// This respects a restored scroll offset or a selection that has moved out
	// of view, while keeping the spacer decision state-free and deterministic.
	scroll := adjustSidebarScroll(flat, opts.Sections, opts.ScrollOffset, available, opts.Width, opts.SelectedID, opts.Focused, opts.FreeScroll)
	entry := flat[min(max(scroll, 0), len(flat)-1)]
	needed := 1 + max(1, len(sidebarRowLines(entry.row.Render(max(1, opts.Width-1), entry.row.ID == opts.SelectedID, opts.Focused))))
	if opts.Sections[entry.section].Title != "" {
		needed++
	}
	return available >= needed
}

type sidebarFlatRow struct {
	section int
	row     SidebarRow
}

// RenderSidebar is the single row/section/viewport/scrollbar/hit-region
// renderer used by project and global Workspaces.
func RenderSidebar(opts SidebarOptions) SidebarRendered {
	width, height := opts.Width, opts.Height
	if width < 1 || height < 1 {
		return SidebarRendered{}
	}
	title := opts.Title
	if title == "" {
		title = "Workspaces"
	}
	lines := make([]string, 0, height)
	regions := make([]Region, 0)
	header, placed := sidebarHeader(title, opts.HeaderMeta, opts.HeaderAction, width)
	lines = append(lines, fit(header, width))
	for _, control := range placed {
		regions = append(regions, Region{Kind: control.kind, ID: control.id, X: control.x, Y: 0, W: control.w, H: 1})
	}
	for _, line := range opts.PrefixLines {
		lines = append(lines, fit(line, width))
	}
	if opts.FilterActive {
		y := len(lines)
		lines = append(lines, fit(opts.FilterLine, width))
		regions = append(regions, Region{Kind: RegionFilter, X: 0, Y: y, W: width, H: 1})
		// The × is registered after the row it sits on, so it wins the hit
		// test: RegionAt scans in reverse, and the smaller, more specific
		// target has to be found first.
		if clear := opts.FilterClear; clear.W > 0 && clear.H > 0 {
			regions = append(regions, Region{Kind: RegionFilterClear, X: clear.X, Y: y, W: clear.W, H: clear.H})
		}
	}
	flat := make([]sidebarFlatRow, 0)
	for sectionIndex, section := range opts.Sections {
		for _, row := range section.Rows {
			flat = append(flat, sidebarFlatRow{section: sectionIndex, row: row})
		}
	}
	if len(flat) > 0 && listSpacerFits(opts, flat, len(lines)) {
		lines = append(lines, strings.Repeat(" ", width))
	} else if len(flat) == 0 && emptySpacerFits(opts) {
		// Padded rather than empty: this line is chrome, so unlike the body's
		// section separators it is never widened by the scrollbar join and would
		// otherwise leave a zero-width line inside a fixed-width box.
		lines = append(lines, strings.Repeat(" ", width))
	}

	footerRows := min(len(opts.FooterLines), max(0, height-len(lines)))
	bodyHeight := max(0, height-len(lines)-footerRows)

	scroll := adjustSidebarScroll(flat, opts.Sections, opts.ScrollOffset, bodyHeight, width, opts.SelectedID, opts.Focused, opts.FreeScroll)
	visibleEnd := sidebarVisibleEnd(flat, opts.Sections, scroll, bodyHeight, width, opts.SelectedID, opts.Focused)
	rowWidth := max(1, width-1)
	y := len(lines)
	bodyStart := y
	lastSection := -1
	visibleRows := 0
	for i := scroll; i < visibleEnd; i++ {
		entry := flat[i]
		section := opts.Sections[entry.section]
		if entry.section == lastSection && y > bodyStart {
			// Adjacent cards get exactly one empty physical line. It is not part
			// of either row's hit region, and a row scrolled to the top never pays
			// for a gap whose preceding card is outside the viewport.
			lines = append(lines, "")
			y++
		} else if entry.section != lastSection && section.Title != "" {
			// Sections are separated by one blank line. The fixed top-content
			// spacer already separates the first visible heading from the chrome,
			// so only later sections own an additional pre-header line.
			if y > bodyStart {
				lines = append(lines, "")
				y++
			}
			heading, x, w := sidebarSectionHeader(section, rowWidth)
			lines = append(lines, fit(heading, rowWidth))
			if section.Action != nil && w > 0 {
				regions = append(regions, Region{Kind: RegionSectionAction, ID: section.Action.ID, X: x, Y: y, W: w, H: 1, SectionHeader: true})
			}
			y++
		}
		rowLines := sidebarRowLines(entry.row.Render(rowWidth, entry.row.ID == opts.SelectedID, opts.Focused))
		if len(rowLines) == 0 {
			rowLines = []string{""}
		}
		for j := range rowLines {
			rowLines[j] = fit(rowLines[j], rowWidth)
		}
		lines = append(lines, rowLines...)
		regions = append(regions, Region{Kind: RegionRow, ID: entry.row.ID, X: 0, Y: y, W: rowWidth, H: len(rowLines), VisibleIndex: i, Data: entry.row.Data})
		y += len(rowLines)
		lastSection = entry.section
		visibleRows++
	}
	if len(flat) == 0 {
		for i, line := range opts.EmptyLines {
			if len(lines) >= height-footerRows {
				break
			}
			if opts.EmptyActionID != "" && i == opts.EmptyActionLine {
				regions = append(regions, Region{Kind: RegionEmptyAction, ID: opts.EmptyActionID, X: 0, Y: len(lines), W: min(ansi.StringWidth(line), rowWidth), H: 1})
			}
			lines = append(lines, fit(line, rowWidth))
		}
	}

	// The scrollbar occupies the final content column and spans the actual body
	// rows, including section headings. This keeps row targets off its column.
	chromeRows := height - footerRows - bodyHeight
	renderedBodyRows := max(0, len(lines)-chromeRows)
	var bar SidebarScrollbar
	if renderedBodyRows > 0 {
		body := strings.Join(lines[chromeRows:], "\n")
		params := ui.ScrollbarParams{TotalItems: len(flat), ScrollOffset: scroll, VisibleItems: max(1, visibleRows), TrackHeight: renderedBodyRows}
		state := ui.HandleStateFrom(opts.ScrollbarHover, opts.ScrollbarDrag)
		scrollbar, geom := ui.RenderScrollbarWithState(params, ui.ScrollbarStyle{Thumb: state, Track: state})
		joined := lipgloss.JoinHorizontal(lipgloss.Top, body, scrollbar)
		lines = append(lines[:chromeRows], strings.Split(joined, "\n")...)
		bar = SidebarScrollbar{Params: params, TrackTop: chromeRows, ThumbTop: geom.ThumbRect.Min.Y, ThumbH: geom.ThumbRect.Dy(), Has: geom.HasThumb}
		if opts.InteractiveScrollbar && geom.HasThumb {
			// Registered after every content region so the reverse scan prefers
			// the bar in the column it owns.
			regions = append(regions,
				Region{Kind: RegionScrollbarTrack, X: renderedBodyX(width), Y: chromeRows, W: 1, H: geom.TrackRect.Dy()},
				Region{Kind: RegionScrollbarThumb, X: renderedBodyX(width), Y: chromeRows + geom.ThumbRect.Min.Y, W: 1, H: geom.ThumbRect.Dy()},
			)
		}
	}
	for len(lines) < height-footerRows {
		lines = append(lines, strings.Repeat(" ", width))
	}
	for _, line := range opts.FooterLines[:footerRows] {
		lines = append(lines, fit(line, width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return SidebarRendered{View: strings.Join(lines, "\n"), Regions: regions, ScrollOffset: scroll, VisibleRows: visibleRows, Scrollbar: bar}
}

// renderedBodyX is the content-local column JoinHorizontal places the bar in:
// the body's widest line, which fit() holds at width-1.
func renderedBodyX(width int) int { return max(1, width-1) }

// adjustSidebarScroll keeps SelectedID on screen using body lines, not row
// counts. Headings and separators consume height, so paging by visible row
// count over-scrolls: items sit above the fold and the body pads empty space.
//
// If the selection already fits at the incoming offset, the offset is kept
// (then clamped so the last page still fills the pane). Otherwise the
// smallest offset that reveals it is used — 0 when the row fits from the top.
//
// freeScroll marks an offset a pointer gesture chose: only the clamp applies,
// because dragging past the selected row must not be dragged straight back.
func adjustSidebarScroll(flat []sidebarFlatRow, sections []SidebarSection, scroll, height, width int, selectedID string, focused bool, freeScroll bool) int {
	n := len(flat)
	if n == 0 {
		return 0
	}
	maxScroll := sidebarMaxScroll(flat, sections, height, width, selectedID, focused)
	scroll = min(max(scroll, 0), maxScroll)
	if freeScroll {
		return scroll
	}
	selected := -1
	for i := range flat {
		if flat[i].row.ID == selectedID {
			selected = i
			break
		}
	}
	if selected >= 0 && selected < scroll {
		// Moving up out of the viewport: park the row at the top. Using the
		// global minimum here would jump a tall list back to offset 0 on k.
		scroll = min(selected, maxScroll)
	}
	for selected >= 0 {
		end := sidebarVisibleEnd(flat, sections, scroll, height, width, selectedID, focused)
		if selected < end || scroll >= selected {
			break
		}
		scroll++
	}
	return min(scroll, maxScroll)
}

// sidebarMaxScroll is the smallest offset at which the last row is still
// visible. Larger offsets leave blank body lines while items exist above.
func sidebarMaxScroll(flat []sidebarFlatRow, sections []SidebarSection, height, width int, selectedID string, focused bool) int {
	n := len(flat)
	if n == 0 || height <= 0 {
		return 0
	}
	if sidebarVisibleEnd(flat, sections, 0, height, width, selectedID, focused) >= n {
		return 0
	}
	for scroll := 1; scroll < n; scroll++ {
		if sidebarVisibleEnd(flat, sections, scroll, height, width, selectedID, focused) >= n {
			return scroll
		}
	}
	return n - 1
}

func sidebarVisibleEnd(flat []sidebarFlatRow, sections []SidebarSection, scroll, height, width int, selectedID string, focused bool) int {
	if height <= 0 || scroll >= len(flat) {
		return scroll
	}
	remaining, end, lastSection := height, scroll, -1
	for end < len(flat) {
		entry := flat[end]
		section := sections[entry.section]
		need := 0
		if entry.section == lastSection && remaining < height {
			need++ // the inter-card line RenderSidebar draws within a section
		} else if entry.section != lastSection && section.Title != "" {
			need++
			if remaining < height {
				need++ // the blank separator RenderSidebar draws above the heading
			}
		}
		rowLines := sidebarRowLines(entry.row.Render(max(1, width-1), entry.row.ID == selectedID, focused))
		need += max(1, len(rowLines))
		if need > remaining {
			break
		}
		remaining -= need
		lastSection = entry.section
		end++
	}
	return end
}

func sidebarRowLines(rendered []string) []string {
	var lines []string
	for _, block := range rendered {
		lines = append(lines, strings.Split(block, "\n")...)
	}
	return lines
}

// renderControl gives every sidebar control one style: a flat pill at rest and
// an accent pill on hover. Project's "New" and global's view control sit in the
// same place and do the same kind of job, so they may not read as two different
// species of thing — one a button, the other a muted caption that gives no clue
// it can be pressed.
func renderControl(action *SidebarAction) string {
	return renderControlLabel(action, action.Label)
}

// renderControlLabel paints a control with a caller-chosen label, so a
// degradation step can shorten the text without inventing a second style.
func renderControlLabel(action *SidebarAction, label string) string {
	style := styles.Button
	if action.Hovered {
		style = styles.ButtonHover
	}
	return styles.RenderPillWithStyle(label, style, nil)
}

// placedControl is one header control that survived layout, with the geometry
// its hit region needs.
type placedControl struct {
	kind RegionKind
	id   string
	x, w int
}

// sidebarHeader lays out the panel title and its right-hand controls: the sort
// pill, then the create button, in that reading order.
//
// Chrome degrades in a defined order rather than clipping. A control that
// cannot be drawn whole is dropped entirely, and its hit region with it,
// because a control clipped to "Activi…" — or to a bare "…" — is a target whose
// meaning a reader cannot recover but whose click still fires. Losing a control
// at 18 columns costs the user a mouse affordance they still have a key for;
// keeping a mystery button costs them a wrong action.
//
// The order is create last to go. When both cannot fit, the sort pill sheds its
// word and keeps its glyph — the list still says it is ordered by something and
// the control is still there to press — and only then disappears. Create is an
// action with no substitute in the header; the sort's label has one, because
// the section headings underneath already name the grouping.
func sidebarHeader(title string, sort, create *SidebarAction, width int) (string, []placedControl) {
	plain := styles.Title.Render(title)
	titleWidth := ansi.StringWidth(title)

	type candidate struct {
		kind  RegionKind
		id    string
		label string
	}
	// Widest form first, then each fallback, most complete to least.
	for _, attempt := range headerAttempts(sort, create) {
		controls := make([]candidate, 0, 2)
		for _, c := range attempt {
			if c.action != nil && c.label != "" {
				controls = append(controls, candidate{kind: c.kind, id: c.action.ID, label: renderControlLabel(c.action, c.label)})
			}
		}
		if len(controls) == 0 {
			continue
		}
		total := 0
		for i, c := range controls {
			total += ansi.StringWidth(c.label)
			if i > 0 {
				total++ // one column between adjacent controls
			}
		}
		if titleWidth+1+total > width {
			continue
		}
		x := width - total
		line := plain + strings.Repeat(" ", x-titleWidth)
		placed := make([]placedControl, 0, len(controls))
		for i, c := range controls {
			if i > 0 {
				line += " "
				x++
			}
			w := ansi.StringWidth(c.label)
			line += c.label
			placed = append(placed, placedControl{kind: c.kind, id: c.id, x: x, w: w})
			x += w
		}
		return line, placed
	}
	return plain, nil
}

type headerCandidate struct {
	kind   RegionKind
	action *SidebarAction
	label  string
}

// headerAttempts is the fixed degradation ladder, widest first.
func headerAttempts(sort, create *SidebarAction) [][]headerCandidate {
	sortFull, sortGlyph, sortMark := "", "", ""
	if sort != nil {
		sortFull = sort.Label
		// The glyph-only form is the label's first cell when the caller built it
		// with SortPillLabel; a caller that passed a bare word keeps its word.
		if _, rest, ok := strings.Cut(sort.Label, " "); ok && rest != "" {
			sortGlyph = SortGlyph
		} else {
			sortGlyph = sort.Label
		}
		if sort.Suffix != "" {
			sortMark = sort.Suffix
			sortFull += " " + sort.Suffix
			sortGlyph += " " + sort.Suffix
		}
	}
	createLabel := ""
	if create != nil {
		createLabel = create.Label
	}
	// The mark is the last thing the control sheds, because it is the only
	// thing on the header that explains rows the user cannot see. A sort word
	// has a substitute — the section headings underneath name the grouping —
	// and a bare sort glyph says only that the list is ordered by something,
	// which nothing is waiting to be told. So the mark-only pill outranks both
	// of them, and a control with no mark degrades exactly as it always did.
	return [][]headerCandidate{
		{{RegionSort, sort, sortFull}, {RegionHeaderAction, create, createLabel}},
		{{RegionSort, sort, sortGlyph}, {RegionHeaderAction, create, createLabel}},
		{{RegionSort, sort, sortMark}, {RegionHeaderAction, create, createLabel}},
		{{RegionHeaderAction, create, createLabel}},
		{{RegionSort, sort, sortFull}},
		{{RegionSort, sort, sortGlyph}},
		{{RegionSort, sort, sortMark}},
	}
}

// sidebarSectionHeader lays out one category heading and its optional action.
//
// The degradation order is deliberate: the action goes first, then the count,
// then the name truncates. A heading's job is naming what the rows beneath it
// are, and the panel header already offers the same create action the section
// "+" does — so when the two compete for a narrow row, the words win.
func sidebarSectionHeader(section SidebarSection, width int) (string, int, int) {
	if width <= 0 || section.Title == "" {
		return "", 0, 0
	}
	full := sectionHeaderLabel(section, true, 0)
	if section.Action != nil && section.Action.Label != "" {
		button := renderControl(section.Action)
		w := ansi.StringWidth(button)
		// Keep the action only when the full label and at least one rule glyph
		// fit beside it. The action is the first degradation step; preserving a
		// button by silently losing the header's rule would invert that order.
		if ansi.StringWidth(full)+2+w <= width {
			x := width - w
			return sectionHeaderWithRule(full, x) + button, x, w
		}
	}
	if ansi.StringWidth(full) > width {
		full = sectionHeaderLabel(section, false, 0)
	}
	if ansi.StringWidth(full) > width {
		full = sectionHeaderLabel(section, false, width)
	}
	return sectionHeaderWithRule(full, width), 0, 0
}

// sectionHeaderLabel gives every section the same category grammar. Known
// activity buckets carry their semantic glyph and colour; project/time/custom
// groups use the quiet open circle so arbitrary caller-owned titles still fit
// the shared renderer without inventing a second heading species.
func sectionHeaderLabel(section SidebarSection, count bool, width int) string {
	glyph, glyphStyle := sectionHeaderGlyph(section.Title)
	titleStyle := styles.Title
	if section.ProjectKey != "" {
		projectHue := styles.ProjectHue(section.ProjectKey)
		glyphStyle = lipgloss.NewStyle().Foreground(projectHue)
		titleStyle = titleStyle.Foreground(projectHue)
	}
	title := strings.ToUpper(section.Title)
	countText := fmt.Sprintf("(%d)", section.Count)
	if width > 0 {
		available := max(0, width-ansi.StringWidth(glyph)-1)
		if count {
			available -= ansi.StringWidth(countText) + 1
		}
		if available < 1 {
			return glyphStyle.Render(glyph)
		}
		title = ansi.Truncate(title, available, "…")
	}
	label := glyphStyle.Render(glyph) + " " + titleStyle.Render(title)
	if count {
		label += " " + styles.Muted.Render(countText)
	}
	return label
}

func sectionHeaderGlyph(title string) (string, lipgloss.Style) {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "pinned":
		return "📌", lipgloss.NewStyle().Foreground(styles.Warning)
	case "needs attention":
		return "◆", lipgloss.NewStyle().Foreground(styles.Error)
	case "working", "done", "live":
		return "●", lipgloss.NewStyle().Foreground(styles.Success)
	default:
		return "○", styles.Muted
	}
}

// sectionHeaderWithRule starts the border-coloured solid rule immediately
// after the label and extends it through every remaining column. rightEdge is
// either the full content width or the first cell of a trailing action.
func sectionHeaderWithRule(label string, rightEdge int) string {
	labelWidth := ansi.StringWidth(label)
	if labelWidth >= rightEdge {
		return ansi.Truncate(label, rightEdge, "…")
	}
	ruleWidth := rightEdge - labelWidth - 1
	rule := " "
	if ruleWidth > 0 {
		rule += lipgloss.NewStyle().Foreground(styles.BorderNormal).Render(strings.Repeat("─", ruleWidth))
	}
	return label + rule
}

// MoveIndex applies the shared clamped selection semantics used by keyboard
// and wheel navigation in both workspace sidebars.
func MoveIndex(index, delta, count int) int {
	if count <= 0 {
		return -1
	}
	return min(max(index+delta, 0), count-1)
}

// ResizePercent applies the shared percentage delta and bounds used by both
// workspace dividers.
func ResizePercent(start, deltaColumns, viewportWidth int) int {
	if viewportWidth <= 0 {
		return min(max(start, 10), 60)
	}
	return min(max(start+deltaColumns*100/viewportWidth, 10), 60)
}

// ApplySelection gives both consumers the same focused and unfocused cursor
// treatment while leaving their row text and icons caller-owned. Do not wrap
// a pre-rendered marker: lipgloss.Render resets all attributes, and the
// name after the icon will lose the fill. Use RenderRow, which paints the
// marker and the name as sibling spans.
func ApplySelection(content string, width int, selected, focused bool) string {
	if !selected {
		return content
	}
	return selectionStyle(focused).Width(width).Render(content)
}

// ScrollGesture is one surface's in-flight pointer gesture on the shared
// sidebar bar, following docs/plans/active/mouse-draggable-scrollbars.md:
// a thumb press grabs where it landed, a track press jumps so the grabbed
// point becomes the thumb anchor and the same gesture continues from there,
// and dragging maps the pointer back through the shared inverse mapping —
// clamped at both ends without ever ending the gesture.
//
// It is state-free about the list itself: the surface owns its offset and
// applies what Press and DragTo return through its own viewport setter, which
// is what keeps the math adoptable by a headless caller unchanged.
type ScrollGesture struct {
	params    ui.ScrollbarParams
	trackY    int // absolute row of the track top, captured at press time
	grabDelta int // rows between the pointer and the thumb's anchor row
	active    bool
}

// Active reports that a gesture is in flight.
func (g *ScrollGesture) Active() bool { return g.active }

// End settles the gesture — on a release anywhere, or on the first button-less
// motion after a release was lost outside the window. Nothing is persisted; a
// scroll offset is ephemeral view state.
func (g *ScrollGesture) End() {
	g.active = false
	g.grabDelta = 0
}

// Press begins a gesture from a press on one of the bar's regions and returns
// the offset to scroll to now. bar is the last render's snapshot, trackY the
// absolute screen row of the track top, pointerRow the pressed absolute row,
// thumb whether the thumb rather than the track was pressed, and offset the
// view's current scroll offset — the anchor a thumb press grabs at.
func (g *ScrollGesture) Press(bar SidebarScrollbar, trackY, pointerRow int, thumb bool, offset int) int {
	local := pointerRow - trackY
	g.params, g.trackY, g.active = bar.Params, trackY, true
	if !thumb {
		// Jump-to-spot: grab at the clicked row with no offset within the
		// thumb, so the drag from here maps the pointer straight onto rows.
		g.grabDelta = 0
		return ui.OffsetAtRow(bar.Params, local)
	}
	g.grabDelta = local - ui.RowForOffset(bar.Params, offset)
	return offset
}

// DragTo maps the pointer's absolute row back onto the dragged offset,
// preserving where within the thumb the gesture grabbed. OffsetAtRow clamps
// past both ends of the track without ending anything.
func (g *ScrollGesture) DragTo(pointerRow int) int {
	return ui.OffsetAtRow(g.params, pointerRow-g.trackY-g.grabDelta)
}

// SetScrollViewport pins the list's viewport to an offset a scrollbar gesture
// chose, latching free-scroll mode: renders keep the chosen position even when
// the selected row sits outside it. Any selection move — keyboard, wheel, or a
// click on a row — clears the latch, and following resumes.
func (m *Model) SetScrollViewport(offset int) {
	m.freeScroll = true
	m.scroll = max(offset, 0)
}

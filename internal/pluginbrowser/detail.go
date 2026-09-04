package pluginbrowser

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// detailLines is the whole detail box, already fitted to its width and height,
// with the shared scrollbar down its reserved column.
func (m *Model) detailLines(width, height int) []string {
	if width < 1 || height < 1 {
		return nil
	}
	// Settle the selection before drawing it: a box that has never held one
	// still has to say so, because an untouched selection state reads as a
	// one-cell selection at the top-left corner.
	m.expireSelection()
	inner := scrolledWidth(width)
	lines := m.detailBlock(inner)
	start := m.detail.scroll
	if start > max0(len(lines)-1) {
		start = max0(len(lines) - 1)
	}
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	view := fitLines(append([]string(nil), lines[start:end]...), inner, height)
	// The highlight is painted at slice time, onto the rows about to be drawn
	// and never into anything the card caches: a selection belongs to this
	// frame alone, and the row it covers is named by its place in the
	// unscrolled block, which is the coordinate space the engine holds it in.
	for i := range view {
		view[i] = m.selection.DecorateRow(view[i], start+i)
	}
	m.geom.docBar = m.joinScrollbar(view, 0, inner, width, ui.ScrollbarParams{
		TotalItems:   len(lines),
		ScrollOffset: start,
		VisibleItems: height,
		TrackHeight:  height,
	}, m.docBar.style())
	return fitLines(view, width, height)
}

// detailBlock is the unscrolled card, which is what both the viewport and the
// scroll bound are measured against, so what can be scrolled to and what is
// drawn cannot disagree.
func (m *Model) detailBlock(width int) []string {
	switch {
	case !m.described:
		return []string{styles.Muted.Render(ansi.Truncate("Waiting for "+m.instance+"…", width, "…"))}
	case m.detail.loading && !m.detail.loaded:
		return []string{
			styles.Title.Render(ansi.Truncate(m.detail.id, width, "…")),
			"",
			styles.Muted.Render(ansi.Truncate("Loading from "+m.instance+"…", width, "…")),
		}
	case !m.detail.loaded && m.detail.err != nil:
		return append([]string{styles.Title.Render(ansi.Truncate(m.detail.id, width, "…")), ""},
			m.errorLines(m.detail.err, width)...)
	case !m.detail.loaded:
		return m.detailEmptyLines(width)
	}
	return m.documentLines(width)
}

// detailEmptyLines is the box with nothing open. It says what the next gesture
// does rather than showing a blank card, which is the difference between an
// empty surface and a broken one.
//
// In a Tab placement, where the box stands beside the list rather than instead
// of it, a plugin with a second collection gets that collection here instead of
// the help text. A plugin's next collection is very often the ledger explaining
// the list — recall's `sources` beside recall's `results` — so "no matches"
// and "why" are on screen together, which is what makes an `abstained` page
// verifiable in place rather than one keystroke away.
func (m *Model) detailEmptyLines(width int) []string {
	if next, ok := m.nextCollection(); ok {
		return m.nextCollectionLines(next, width)
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return []string{styles.Muted.Render(ansi.Truncate("Nothing to open.", width, "…"))}
	}
	lines := []string{styles.Title.Render(ansi.Truncate(m.Name()+" · "+c.Title, width, "…")), ""}
	if !c.Detail {
		lines = append(lines, styles.Muted.Render(ansi.Truncate(
			"This collection's rows have no document behind them.", width, "…")))
		return lines
	}
	lines = append(lines,
		styles.Muted.Render(ansi.Truncate("Press Enter on a row to open it here.", width, "…")),
		"",
		styles.Subtle.Render(ansi.Truncate("A second Enter moves the keyboard into this box.", width, "…")),
	)
	if len(m.desc.Actions) > 0 {
		lines = append(lines, "", styles.Subtle.Render(ansi.Truncate(
			fmt.Sprintf("a  offers %d plugin action(s), each confirmed by the host.", len(m.desc.Actions)), width, "…")))
	}
	return lines
}

// nextCollection is the collection the empty detail box shows: the one after
// the collection on screen, wrapping to the first.
//
// It exists only in a Tab placement. A pane shows one shape at a time and has
// no second box to put anything in, and a browser with one collection has no
// "next" — both keep the help text.
func (m *Model) nextCollection() (pluginhost.Collection, bool) {
	if m == nil || m.paneMode() || !m.described {
		return pluginhost.Collection{}, false
	}
	cols := m.Collections()
	if len(cols) < 2 {
		return pluginhost.Collection{}, false
	}
	active := m.active
	if active < 0 || active >= len(cols) {
		active = 0
	}
	return cols[(active+1)%len(cols)], true
}

// nextCollectionRows is how many of the next collection's rows the box shows.
// It is a preview, not a second list: the collection's own tab is one keystroke
// away and is where a reader who wants all of them goes.
const nextCollectionRows = 8

// nextCollectionLines draws that collection in the empty box: its title, then
// what it currently says, then the rows it has.
func (m *Model) nextCollectionLines(c pluginhost.Collection, width int) []string {
	title := c.Title
	if title == "" {
		title = c.ID
	}
	lines := []string{styles.Title.Render(ansi.Truncate(m.Name()+" · "+title, width, "…")), ""}

	s := m.states[c.ID]
	queried := s != nil && strings.TrimSpace(s.queryText()) != ""
	switch {
	case (c.Search == pluginhost.SearchRequired && !queried) || (s != nil && s.unqueried):
		// A required-search collection was never asked, so it claims nothing.
		// This box never asks one either, so "not read yet" would blame the
		// host for a silence the collection's own contract explains, and "no
		// rows" would report an answer nobody gave.
		lines = append(lines, styles.Subtle.Render(ansi.Truncate(
			"This collection answers a query, and there is none.", width, "…")))
	case s == nil || (!s.loaded && !s.loading):
		lines = append(lines, styles.Muted.Render(ansi.Truncate("Not read yet.", width, "…")))
	case s.loading && !s.loaded:
		lines = append(lines, styles.Muted.Render(ansi.Truncate("Reading "+title+"…", width, "…")))
	case s.err != nil:
		lines = append(lines, styles.Body.Foreground(styles.Error).Render(ansi.Truncate(
			errorHeadline(s.err.Code), width, "…")))
	case len(s.items) == 0:
		lines = append(lines, styles.Muted.Render(ansi.Truncate(
			emptyHeadline(s.outcome), width, "…")))
	default:
		lines = append(lines, m.nextCollectionRowLines(c, s, width)...)
	}

	// The closing line says what this box is, and what the box would hold if
	// the reader opened something. A collection whose rows have no document
	// behind them says that instead of promising an Enter that does nothing.
	closing := "Enter on a row opens it here."
	if active, ok := m.ActiveCollection(); ok && !active.Detail {
		closing = "The list beside this has no documents behind its rows."
	}
	return append(lines, "",
		styles.Subtle.Render(ansi.Truncate("This plugin's next collection.", width, "…")),
		styles.Subtle.Render(ansi.Truncate(closing, width, "…")))
}

// nextCollectionRowLines is the preview's rows: the primary cell, with the
// status label right-aligned where there is room for it. No table, no cursor —
// this box is not a second list to drive.
func (m *Model) nextCollectionRowLines(c pluginhost.Collection, s *collectionState, width int) []string {
	primary, hasPrimary := c.PrimaryColumn()
	rows := make([]string, 0, nextCollectionRows+1)
	for i, item := range s.items {
		if i >= nextCollectionRows {
			rows = append(rows, styles.Subtle.Render(ansi.Truncate(
				fmt.Sprintf("+%d more", len(s.items)-nextCollectionRows), width, "…")))
			break
		}
		name := item.ID
		if hasPrimary {
			if cell := strings.TrimSpace(item.Cells[primary.ID]); cell != "" {
				name = cell
			}
		}
		label := ""
		if item.Status != nil {
			label = ansi.Truncate(item.Status.Label, statusColumnMax, "…")
		}
		left := styles.Body.Render(ansi.Truncate(name, max0(width-ansi.StringWidth(label)-2), "…"))
		if label == "" {
			rows = append(rows, left)
			continue
		}
		rows = append(rows, padBetween(left, toneStyle(item.Status.Tone).Render(label), width))
	}
	return rows
}

// emptyHeadline is the one-line version of the list's own empty state, for a
// preview that has no room for the full card. The three claims stay distinct
// here for the reason they are distinct there: each says a different thing
// about the same empty list.
func emptyHeadline(outcome pluginhost.PageOutcome) string {
	switch outcome {
	case pluginhost.OutcomeFailed:
		return "Nothing could be asked; every source failed."
	case pluginhost.OutcomeDegraded:
		return "No rows, and coverage was incomplete."
	case pluginhost.OutcomeAbstained:
		return "No rows, and every source answered."
	default:
		return "No rows."
	}
}

// documentLines renders one resource: identity, fields, body, then sections in
// the order the plugin declared them, each under a titled rule.
func (m *Model) documentLines(width int) []string {
	doc := m.detail.doc
	title := doc.Title
	if title == "" {
		title = doc.Identity
	}
	head := styles.Title.Render(ansi.Truncate(title, width, "…"))
	if doc.Status != nil && doc.Status.Label != "" {
		pill := toneStyle(doc.Status.Tone).Render(doc.Status.Label)
		if ansi.StringWidth(head)+2+ansi.StringWidth(pill) <= width {
			head = padBetween(head, pill, width)
		}
	}
	lines := []string{head, ""}

	// The provider-stable identity, in the resource card's own meta style, and
	// only where the title has not already said it. It is what a reader quotes
	// back to the plugin's CLI, so a card that shows a title and hides it is a
	// card the reader cannot act on outside Sidecar.
	if meta := metaRow(doc, title, width); meta != "" {
		lines = append(lines, meta, "")
	}
	if m.detail.err != nil {
		// A refresh that failed keeps the document and says so, rather than
		// replacing what the user is reading with an error card.
		lines = append(lines, toneStyle(errorTone(m.detail.err.Code)).Render(
			ansi.Truncate("Refresh failed: "+errorHeadline(m.detail.err.Code), width, "…")), "")
	} else if m.detail.loading {
		// The card on screen is the row the cursor has left, and saying
		// "refreshing" about it would be a claim about the wrong document.
		word := "Refreshing…"
		if m.detail.id != m.detail.shownID {
			word = "Loading " + m.detail.id + "…"
		}
		lines = append(lines, styles.Muted.Render(ansi.Truncate(word, width, "…")), "")
	}

	if len(doc.Fields) > 0 {
		lines = append(lines, fieldGrid(doc.Fields, width)...)
	}
	if body := m.renderedBody(width); len(body) > 0 {
		if len(doc.Fields) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, body...)
		if doc.Body != nil && doc.Body.Truncated {
			lines = append(lines, "", styles.Muted.Render(ansi.Truncate(
				"… body truncated by Sidecar's size limit", width, "…")))
		}
	}

	for _, section := range doc.Sections {
		lines = append(lines, sectionRule(section.Title, width))
		lines = append(lines, m.sectionLines(section, width)...)
	}

	lines = append(lines, "")
	if doc.SourceURL != "" {
		lines = append(lines, styles.Muted.Render(ansi.Truncate("o  open "+doc.SourceURL, width, "…")))
	} else {
		lines = append(lines, styles.Subtle.Render(ansi.Truncate(
			"no sourceUrl — o is unavailable on this record", width, "…")))
	}
	return lines
}

// metaRow is the identity-and-subtitle line under the title, drawn exactly as
// resourceview's is: the identity `Muted`, the subtitle `Subtle`, two columns
// between them. The status is not in it, because this card carries the status
// as a pill on the title row.
func metaRow(doc resource.Document, title string, width int) string {
	var parts []string
	if doc.Identity != "" && doc.Identity != title {
		parts = append(parts, styles.Muted.Render(doc.Identity))
	}
	if doc.Subtitle != "" {
		parts = append(parts, styles.Subtle.Render(doc.Subtitle))
	}
	if len(parts) == 0 {
		return ""
	}
	return fitStyled(ansi.Truncate(strings.Join(parts, "  "), width, "…"), width)
}

// sectionRule is the section header the design language names: a bold label,
// then a rule fill.
func sectionRule(title string, width int) string {
	label := "── " + title + " "
	fill := width - ansi.StringWidth(label)
	if fill < 0 {
		return styles.Muted.Render(ansi.Truncate(label, width, "…"))
	}
	return styles.Muted.Render(label + strings.Repeat("─", fill))
}

// sectionLines renders exactly one of a section's three shapes. Sanitization
// already picked which one, so nothing here has to choose.
func (m *Model) sectionLines(section resource.Section, width int) []string {
	switch {
	case section.Body != nil && section.Body.Text != "":
		return m.renderBody(section.Body, width)
	case len(section.Fields) > 0:
		return fieldGrid(section.Fields, width)
	case len(section.Items) > 0:
		return m.timelineLines(section.Items, width)
	default:
		return []string{styles.Subtle.Render(ansi.Truncate("(empty)", width, "…"))}
	}
}

// timelineLines renders a timeline with its times relative, which is what the
// protocol says `when` means and what keeps a column of absolute stamps from
// being the widest thing in a narrow box.
func (m *Model) timelineLines(items []resource.TimelineItem, width int) []string {
	now := m.now()
	stamp := 0
	rendered := make([]string, len(items))
	for i, item := range items {
		rendered[i] = relativeAge(item.When, now)
		if w := ansi.StringWidth(rendered[i]); w > stamp {
			stamp = w
		}
	}
	if stamp > 10 {
		stamp = 10
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		when := ansi.Truncate(rendered[i], stamp, "…")
		pad := stamp - ansi.StringWidth(when)
		text := item.Title
		if item.Text != "" {
			if text != "" {
				text += " · "
			}
			text += item.Text
		}
		rest := max0(width - stamp - 3)
		out = append(out, styles.Muted.Render(when+strings.Repeat(" ", max0(pad)))+"   "+
			styles.Body.Render(ansi.Truncate(text, rest, "…")))
	}
	return out
}

func (m *Model) now() time.Time {
	if m.calls.Now != nil {
		return m.calls.Now()
	}
	return time.Now()
}

// relativeAge is the host's relative form. It stops at weeks because a plugin
// that wants a calendar date can send one in a field, and a number of months
// inferred from a timestamp is precision the data rarely backs.
func relativeAge(when, now time.Time) string {
	if when.IsZero() {
		return ""
	}
	d := now.Sub(when)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}

// fieldGrid lays bounded label/value pairs out in an aligned column. A value
// that overruns its column takes a whole line of its own rather than wrapping
// into the label gutter.
func fieldGrid(fields []resource.Field, width int) []string {
	labelW := 0
	for _, f := range fields {
		if w := ansi.StringWidth(f.Label); w > labelW {
			labelW = w
		}
	}
	if maxLabel := width / 3; labelW > maxLabel {
		labelW = maxLabel
	}
	if labelW < 1 {
		labelW = 1
	}
	valueW := width - labelW - 2
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		label := ansi.Truncate(f.Label, labelW, "…")
		pad := labelW - ansi.StringWidth(label)
		if valueW < 8 || ansi.StringWidth(f.Value) > valueW {
			lines = append(lines, styles.Muted.Render(label))
			lines = append(lines, styles.Body.Render(ansi.Truncate(f.Value, width, "…")))
			continue
		}
		lines = append(lines, styles.Muted.Render(label+strings.Repeat(" ", max0(pad)))+"  "+
			styles.Body.Render(f.Value))
	}
	return lines
}

// renderedBody renders the document's own body once per width, generation and
// renderer style key. The style key is part of the key because the cached lines
// carry their palette in their own escape sequences: without it a theme change
// repaints everything around the body and leaves the body in the old colours
// until a resize happens to invalidate it (td-83a3fa).
func (m *Model) renderedBody(width int) []string {
	doc := m.detail.doc
	if doc.Body == nil || doc.Body.Text == "" {
		return nil
	}
	style := m.styleKey()
	if m.detail.body != nil && m.detail.bodyForW == width &&
		m.detail.bodyForGen == m.detail.generation && m.detail.bodyForStyle == style {
		return m.detail.body
	}
	out := m.renderBody(doc.Body, width)
	m.detail.body = out
	m.detail.bodyForW = width
	m.detail.bodyForGen = m.detail.generation
	m.detail.bodyForStyle = style
	return out
}

// styleKey is the renderer's palette identity, or the empty string when there
// is no renderer to ask.
func (m *Model) styleKey() string {
	if m.renderer == nil {
		return ""
	}
	return m.renderer.StyleKey()
}

// renderBody is the sanitized markdown path: raw HTML dropped, images inert alt
// text, links plain text with no destination, and all OSC stripped after
// rendering.
func (m *Model) renderBody(body *resource.Body, width int) []string {
	if body == nil || body.Text == "" {
		return nil
	}
	if body.Format == resource.FormatMarkdown {
		return resource.RenderSafeMarkdown(m.renderer, body.Text, width)
	}
	var plain []string
	for _, line := range strings.Split(body.Text, "\n") {
		plain = append(plain, wrapToWidth(line, width)...)
	}
	return resource.StripRenderedOSC(plain)
}

// maxDetailScroll is the furthest the detail viewport can travel, and zero when
// there is no detail box on screen at all.
func (m *Model) maxDetailScroll() int {
	_, detailOuter := m.split()
	if detailOuter <= 0 {
		return 0
	}
	lines := len(m.detailBlock(scrolledWidth(detailOuter - chromeOverhead)))
	maxScroll := lines - (m.height - 2)
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

// clampDetailScroll keeps the detail viewport inside the card.
func (m *Model) clampDetailScroll() {
	_, detailOuter := m.split()
	if detailOuter <= 0 {
		m.detail.scroll = 0
		return
	}
	maxScroll := m.maxDetailScroll()
	if m.detail.scroll > maxScroll {
		m.detail.scroll = maxScroll
	}
	if m.detail.scroll < 0 {
		m.detail.scroll = 0
	}
}

// ScrollBy moves whichever window has focus, and reports the follow-load
// schedule alongside whether anything moved.
//
// Over the list this is a cursor move like any other, so it schedules the
// detail that goes with the row it lands on. The cmd has to reach the host's
// Update or the follow never runs — and worse, a follow already scheduled from
// the keyboard is invalidated by the newer generation this move stamps, so a
// dropped cmd leaves the detail showing a row the cursor has left.
func (m *Model) ScrollBy(delta int) (tea.Cmd, bool) {
	if m.focus == FocusDetail {
		before := m.detail.scroll
		m.detail.scroll += delta
		m.clampDetailScroll()
		return nil, m.detail.scroll != before
	}
	s := m.activeState()
	if s == nil {
		return nil, false
	}
	before := s.cursor
	cmd := m.moveTo(s.cursor + delta)
	return cmd, s.cursor != before
}

// ScrollAtBoundary reports whether a scroll of delta would move nothing, so a
// host can hand the wheel event to whatever is underneath instead of swallowing
// it.
func (m *Model) ScrollAtBoundary(delta int) bool {
	if m.focus == FocusDetail {
		if delta < 0 {
			return m.detail.scroll <= 0
		}
		before := m.detail.scroll
		m.detail.scroll += delta
		m.clampDetailScroll()
		at := m.detail.scroll == before
		m.detail.scroll = before
		return at
	}
	s := m.activeState()
	if s == nil {
		return true
	}
	if delta < 0 {
		return s.cursor <= 0
	}
	return s.cursor >= len(s.items)-1
}

// errorHeadline is the host's sentence for a stable code. The plugin's own
// message is displayed beneath it; this line is Sidecar's, so a plugin cannot
// restyle or reframe the failure.
func errorHeadline(code resource.Code) string {
	switch code {
	case resource.CodeNotFound:
		return "Not found"
	case resource.CodeUnauthorized:
		return "Not authorized"
	case resource.CodeForbidden:
		return "Access denied"
	case resource.CodeRateLimited:
		return "Rate limited"
	case resource.CodeInvalidConfig:
		return "This plugin is not configured"
	case resource.CodeInvalidRequest:
		return "Sidecar sent a request this plugin rejected"
	case resource.CodeUnavailable:
		return "This plugin is unavailable"
	default:
		return "This plugin failed"
	}
}

func errorTone(code resource.Code) resource.Tone {
	switch code {
	case resource.CodeNotFound:
		return resource.ToneNeutral
	case resource.CodeRateLimited, resource.CodeUnavailable:
		return resource.ToneWarning
	default:
		return resource.ToneDanger
	}
}

func toneStyle(tone resource.Tone) lipgloss.Style {
	switch tone {
	case resource.ToneInfo:
		return styles.Body.Foreground(styles.Info)
	case resource.ToneSuccess:
		return styles.Body.Foreground(styles.Success)
	case resource.ToneWarning:
		return styles.Body.Foreground(styles.Warning)
	case resource.ToneDanger:
		return styles.Body.Foreground(styles.Error)
	default:
		return styles.Muted
	}
}

// wrapToWidth is a plain greedy word wrap in display cells. It always returns
// at least one line so an empty string still costs a row where one is meant.
func wrapToWidth(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if ansi.StringWidth(text) <= width {
		return []string{text}
	}
	var lines []string
	var current string
	for _, word := range strings.Fields(text) {
		switch {
		case current == "":
			current = word
		case ansi.StringWidth(current)+1+ansi.StringWidth(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
		for ansi.StringWidth(current) > width {
			lines = append(lines, ansi.Truncate(current, width, "…"))
			current = trimCells(current, width)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func trimCells(s string, width int) string {
	taken := 0
	for i, r := range s {
		if taken >= width {
			return s[i:]
		}
		taken += ansi.StringWidth(string(r))
	}
	return ""
}

// DetailDocument returns the open document and whether there is one, for a host
// that needs its source URL or its identity.
func (m *Model) DetailDocument() (resource.Document, bool) {
	return m.detail.doc, m.detail.loaded
}

// Collection returns the collection on screen for a host that needs its ID.
func (m *Model) Collection() (pluginhost.Collection, bool) { return m.ActiveCollection() }

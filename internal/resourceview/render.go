package resourceview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// SetSize records the content box. The body is re-rendered lazily on the next
// View, because a resize during a drag would otherwise re-run the markdown
// renderer on every frame of the drag.
func (m *Model) SetSize(width, height int) {
	if width == m.width && height == m.height {
		return
	}
	m.width, m.height = width, height
	if m.browser != nil {
		m.browser.SetSize(width, height)
		return
	}
	if width != m.bodyForW {
		m.invalidateBody()
	}
	m.clampScroll()
}

// Width and Height report the last box the view was given.
func (m *Model) Width() int  { return m.width }
func (m *Model) Height() int { return m.height }

// Scroll is the current vertical offset, which the host persists.
func (m *Model) Scroll() int { return m.scroll }

// ScrollBy moves the viewport and reports whether anything moved, so a host
// can decide whether the wheel event was consumed.
func (m *Model) ScrollBy(delta int) bool {
	if m.browser != nil {
		return m.browser.PaneScroll(delta)
	}
	before := m.scroll
	m.scroll += delta
	m.clampScroll()
	return m.scroll != before
}

// ScrollAtBoundary reports whether a scroll of delta would move nothing,
// which is how a host decides to hand a wheel event to whatever is underneath
// instead of swallowing it. The doc, issue and diff viewers all answer this;
// without it a host has to probe by scrolling and undoing.
func (m *Model) ScrollAtBoundary(delta int) bool {
	if m.browser != nil {
		return m.browser.PaneScrollAtBoundary(delta)
	}
	if delta < 0 {
		return m.scroll <= 0
	}
	if delta > 0 {
		return m.scroll >= m.maxScroll()
	}
	return true
}

// ScrollTo jumps to an absolute offset.
func (m *Model) ScrollTo(offset int) bool {
	before := m.scroll
	m.scroll = offset
	m.clampScroll()
	return m.scroll != before
}

func (m *Model) clampScroll() {
	max := m.maxScroll()
	if m.scroll > max {
		m.scroll = max
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *Model) maxScroll() int {
	lines := len(m.lines())
	if lines <= m.height {
		return 0
	}
	return lines - m.height
}

func (m *Model) invalidateBody() {
	m.body = nil
	m.bodyForW = -1
	m.bodyForStyle = ""
}

// View renders the card, held to exactly the box it was given. A content that
// hands back more rows than its box would push the app header off screen, so
// the fit is enforced here rather than trusted to the caller.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.browser != nil {
		return m.browser.View()
	}
	// Settle the selection before drawing it: a card that has never held one
	// still has to say so, because an untouched selection state reads as a
	// one-cell selection at the top-left corner.
	m.expireSelection()
	lines := m.lines()
	start := m.scroll
	if start > len(lines) {
		start = len(lines)
	}
	end := start + m.height
	if end > len(lines) {
		end = len(lines)
	}
	view := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		// The highlight is painted at slice time, onto the row about to be
		// drawn and never into the body the card caches: a selection belongs to
		// this frame only, and the row it covers is named by its place in the
		// unscrolled card.
		view = append(view, m.selection.DecorateRow(ansi.Truncate(lines[i], m.width, ""), i))
	}
	return ui.FitBlock(strings.Join(view, "\n"), m.width, m.height)
}

// lines is the whole card, unscrolled. It is what both View and maxScroll
// measure, so what can be scrolled to and what is drawn cannot disagree.
func (m *Model) lines() []string {
	switch m.state {
	case StateArmed:
		return m.armedLines()
	case StateLoading:
		return m.loadingLines()
	case StateError:
		return m.errorLines()
	default:
		return m.documentLines()
	}
}

func (m *Model) armedLines() []string {
	return []string{
		styles.Title.Render(ui.TruncateString(m.ref.Locator, m.width)),
		"",
		styles.Muted.Render(m.fit("Waiting for " + m.ref.Instance + " to be ready.")),
		"",
		styles.Subtle.Render(m.fit("This tab is remembered. It resolves when the provider reports ready,")),
		styles.Subtle.Render(m.fit("or press r to try now.")),
	}
}

func (m *Model) loadingLines() []string {
	return []string{
		styles.Title.Render(ui.TruncateString(m.ref.Locator, m.width)),
		"",
		styles.Muted.Render(m.fit("Loading from " + m.ref.Instance + "…")),
	}
}

func (m *Model) errorLines() []string {
	err := m.err
	if err == nil {
		err = resource.Errorf(resource.CodeInternal, "unknown failure")
	}
	lines := []string{
		styles.Title.Render(ui.TruncateString(m.ref.Locator, m.width)),
		"",
		toneStyle(errorTone(err.Code)).Render(m.fit(errorHeadline(err.Code))),
	}
	if err.Message != "" {
		lines = append(lines, "")
		lines = append(lines, m.wrap(err.Message, styles.Body)...)
	}
	if err.SetupHint != "" {
		lines = append(lines, "")
		lines = append(lines, styles.Muted.Render(m.fit("Setup")))
		// The hint is text the user may copy. Sidecar never runs it, and
		// saying so is part of the contract rather than a nicety.
		lines = append(lines, m.wrap(err.SetupHint, styles.Subtle)...)
	}
	lines = append(lines, "")
	if err.Retryable {
		lines = append(lines, styles.Muted.Render(m.fit("r  retry")))
	} else {
		lines = append(lines, styles.Muted.Render(m.fit("r  try again after fixing the above")))
	}
	return lines
}

func (m *Model) documentLines() []string {
	doc := m.doc
	var lines []string

	title := doc.Title
	if title == "" {
		title = doc.Identity
	}
	lines = append(lines, m.wrap(title, styles.Title)...)

	meta := m.metaRow(doc)
	if meta != "" {
		lines = append(lines, meta)
	}

	// A refresh that failed keeps the document and says so, rather than
	// replacing what the user is reading with an error card.
	if m.err != nil {
		lines = append(lines, "", toneStyle(errorTone(m.err.Code)).Render(
			m.fit("Refresh failed: "+errorHeadline(m.err.Code))))
	} else if m.refreshing {
		lines = append(lines, "", styles.Muted.Render(m.fit("Refreshing…")))
	}

	if len(doc.Fields) > 0 {
		lines = append(lines, "")
		lines = append(lines, m.fieldGrid(doc.Fields)...)
	}

	if body := m.renderedBody(); len(body) > 0 {
		lines = append(lines, "")
		lines = append(lines, body...)
		if doc.Body != nil && doc.Body.Truncated {
			lines = append(lines, "", styles.Muted.Render(m.fit("… body truncated by Sidecar's size limit")))
		}
	}

	if doc.SourceURL != "" {
		lines = append(lines, "", styles.Muted.Render(m.fit("o  open "+doc.SourceURL)))
	}
	return lines
}

// metaRow is the one-line identity/status/subtitle summary under the title.
func (m *Model) metaRow(doc resource.Document) string {
	var parts []string
	if doc.Identity != "" {
		parts = append(parts, styles.Muted.Render(doc.Identity))
	}
	if doc.Status != nil && doc.Status.Label != "" {
		parts = append(parts, toneStyle(doc.Status.Tone).Render(doc.Status.Label))
	}
	if doc.Subtitle != "" {
		parts = append(parts, styles.Subtle.Render(doc.Subtitle))
	}
	if len(parts) == 0 {
		return ""
	}
	return m.fitRendered(strings.Join(parts, "  "))
}

// fieldGrid lays the bounded label/value pairs out in an aligned column. The
// label column is capped so one long label cannot squeeze every value out of
// the box.
func (m *Model) fieldGrid(fields []resource.Field) []string {
	labelW := 0
	for _, f := range fields {
		if w := ansi.StringWidth(f.Label); w > labelW {
			labelW = w
		}
	}
	if maxLabel := m.width / 3; labelW > maxLabel {
		labelW = maxLabel
	}
	if labelW < 1 {
		labelW = 1
	}
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		label := ui.TruncateString(f.Label, labelW)
		pad := labelW - ansi.StringWidth(label)
		valueW := m.width - labelW - 2
		if valueW < 1 {
			valueW = 1
		}
		lines = append(lines, styles.Muted.Render(label+strings.Repeat(" ", max0(pad)))+"  "+
			styles.Body.Render(ui.TruncateString(f.Value, valueW)))
	}
	return lines
}

// renderedBody renders the sanitized body once per width and generation. The
// markdown path is the resource-specific one: raw HTML is dropped, images
// become inert alt text, links become plain text with no destination, and all
// OSC is stripped after rendering.
func (m *Model) renderedBody() []string {
	if m.doc.Body == nil || m.doc.Body.Text == "" {
		return nil
	}
	style := m.renderer.StyleKey()
	if m.body != nil && m.bodyForW == m.width && m.bodyForGen == m.generation && m.bodyForStyle == style {
		return m.body
	}
	var out []string
	if m.doc.Body.Format == resource.FormatMarkdown {
		out = resource.RenderSafeMarkdown(m.renderer, m.doc.Body.Text, m.width)
	} else {
		// Plain text is still stripped: the sanitizer guarantees no control
		// bytes, and this guarantees no renderer-synthesized ones either.
		out = resource.StripRenderedOSC(m.wrapPlain(m.doc.Body.Text))
	}
	m.body = out
	m.bodyForW = m.width
	m.bodyForGen = m.generation
	m.bodyForStyle = style
	return out
}

func (m *Model) wrapPlain(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapToWidth(line, m.width)...)
	}
	return out
}

// wrap renders text into styled lines that fit the box.
func (m *Model) wrap(text string, style interface{ Render(...string) string }) []string {
	wrapped := wrapToWidth(text, m.width)
	out := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		out = append(out, style.Render(line))
	}
	return out
}

func (m *Model) fit(s string) string { return ui.TruncateString(s, m.width) }

// fitRendered truncates a string that already carries styling.
func (m *Model) fitRendered(s string) string {
	if ansi.StringWidth(s) <= m.width {
		return s
	}
	return ui.TruncateString(s, m.width)
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
		// A single word longer than the box is hard-split rather than
		// allowed to overflow the leaf.
		for ansi.StringWidth(current) > width {
			lines = append(lines, ui.TruncateString(current, width))
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

// trimCells drops the first width display cells from s.
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

// errorHeadline is the human sentence for a stable code. The provider's own
// message is displayed beneath it; this line is Sidecar's, so a provider
// cannot restyle or reframe the failure.
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
		return "Provider is not configured"
	case resource.CodeInvalidRequest:
		return "Sidecar sent a request this provider rejected"
	case resource.CodeUnavailable:
		return "Provider is unavailable"
	default:
		return "Provider failed"
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

func toneStyle(tone resource.Tone) interface{ Render(...string) string } {
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

// TabLabel is what the tab strip shows: the locator, which is stable from the
// moment of the click, rather than a title that appears a moment later and
// makes the strip jump.
func (m *Model) TabLabel() string {
	if m.browser != nil {
		return m.browser.PaneTabLabel()
	}
	if m.ref.Locator != "" {
		return m.ref.Locator
	}
	return "Resource"
}

// Title is the pane's headline for host chrome.
func (m *Model) Title() string {
	if m.browser != nil {
		return m.browser.PaneTitle()
	}
	if m.hasDoc && m.doc.Title != "" {
		return fmt.Sprintf("%s: %s", m.ref.Locator, m.doc.Title)
	}
	return m.TabLabel()
}

// SourceURL is the one action a document can offer, already validated as
// http(s) by the document sanitizer.
func (m *Model) SourceURL() string {
	if m.browser != nil {
		return m.browser.PaneSourceURL()
	}
	if !m.hasDoc {
		return ""
	}
	return m.doc.SourceURL
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

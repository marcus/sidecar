package modal

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/overlay"
	"github.com/marcus/sidecar/internal/styles"
)

// renderedSection holds a section's rendered content and metadata.
type renderedSection struct {
	content    string
	height     int
	focusables []FocusableInfo
	overlay    *Overlay
	scrollbar  *SectionScrollbar
	section    int // index into m.sections this render came from
}

// renderSections renders all sections at the given content width and returns
// the rendered sections along with collected focusable IDs.
func (m *Modal) renderSections(contentWidth int) ([]renderedSection, []string) {
	// First paint has no tab order yet; seed it so the focused field (and a
	// Combo overlay) can render correctly on this frame.
	if len(m.focusIDs) == 0 {
		m.focusIDs = m.collectFocusIDs(contentWidth)
	}
	if m.pendingFocusID != "" {
		for i, fid := range m.focusIDs {
			if fid == m.pendingFocusID {
				m.focusIdx = i
				m.pendingFocusID = ""
				break
			}
		}
	}

	focusID := m.currentFocusID()
	rendered := make([]renderedSection, 0, len(m.sections))
	var focusIDs []string

	for i, s := range m.sections {
		res := s.Render(contentWidth, focusID, m.hoverID)
		res.Content = clampLines(res.Content, contentWidth)
		height := measureHeight(res.Content)

		rendered = append(rendered, renderedSection{
			content:    res.Content,
			height:     height,
			focusables: res.Focusables,
			overlay:    res.Overlay,
			scrollbar:  res.Scrollbar,
			section:    i,
		})

		focusIDs = appendTabIDs(focusIDs, res.Focusables)
	}

	return rendered, focusIDs
}

func (m *Modal) collectFocusIDs(contentWidth int) []string {
	var ids []string
	for _, s := range m.sections {
		res := s.Render(contentWidth, "", m.hoverID)
		ids = appendTabIDs(ids, res.Focusables)
	}
	return ids
}

// appendTabIDs adds section hit targets that participate in Tab order.
// MouseOnly rows stay in the hit map but are not focus stops.
func appendTabIDs(dst []string, focusables []FocusableInfo) []string {
	for _, f := range focusables {
		if f.MouseOnly {
			continue
		}
		dst = append(dst, f.ID)
	}
	return dst
}

// buildLayout renders all sections, measures heights, and registers hit regions.
func (m *Modal) buildLayout(screenW, screenH int, handler *mouse.Handler) string {
	// Clamp modal width
	maxWidth := screenW - 2*m.marginX
	if maxWidth < 1 {
		maxWidth = 1
	}
	minWidth := MinModalWidth
	if maxWidth < minWidth {
		minWidth = maxWidth
	}
	modalWidth := clamp(m.width, minWidth, maxWidth)
	contentWidth := modalWidth - ModalPadding // border(2) + padding(4)
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Compute viewport height budget
	modalInnerHeight := desiredModalInnerHeight(screenH, m.marginY)
	headerLines := 0
	if m.title != "" {
		headerLines = 2 // title + blank line
	}
	footerLines := hintLines(m.showHints)
	if m.customFooter != "" {
		footerLines += strings.Count(m.customFooter, "\n") + 1
	}
	maxViewportHeight := max(1, modalInnerHeight-headerLines-footerLines)

	// 1. First pass: render sections at full width to measure total height
	rendered, focusIDs := m.renderSections(contentWidth)
	m.focusIDs = focusIDs
	if m.pendingFocusID != "" {
		for i, fid := range m.focusIDs {
			if fid == m.pendingFocusID {
				m.focusIdx = i
				m.pendingFocusID = ""
				break
			}
		}
	}

	// Ensure focusIdx is valid
	if len(m.focusIDs) > 0 && m.focusIdx >= len(m.focusIDs) {
		m.focusIdx = 0
	}

	// Filter out zero-height sections
	visible := filterVisible(rendered)
	actualContentHeight := totalHeight(visible)

	// 2. Determine if scrollbar is needed
	needsScrollbar := actualContentHeight > maxViewportHeight

	// If scrollbar needed, re-render sections with reduced width
	if needsScrollbar && contentWidth > 1 {
		rendered, focusIDs = m.renderSections(contentWidth - 1)
		m.focusIDs = focusIDs
		if m.pendingFocusID != "" {
			for i, fid := range m.focusIDs {
				if fid == m.pendingFocusID {
					m.focusIdx = i
					m.pendingFocusID = ""
					break
				}
			}
		}
		if len(m.focusIDs) > 0 && m.focusIdx >= len(m.focusIDs) {
			m.focusIdx = 0
		}
		visible = filterVisible(rendered)
		actualContentHeight = totalHeight(visible)
		// Recheck: content may now fit (unlikely but possible with wrapping changes)
		needsScrollbar = actualContentHeight > maxViewportHeight
	}

	// Cache absolute Y positions of focusable elements for scroll-to-focus
	m.focusPositions = make(map[string]focusablePos, len(focusIDs))
	{
		sectionY := 0
		for _, r := range visible {
			for _, f := range r.focusables {
				m.focusPositions[f.ID] = focusablePos{
					y:      sectionY + f.OffsetY,
					height: f.Height,
				}
			}
			sectionY += r.height
		}
	}

	// 3. Join full content with newlines between non-empty sections
	var parts []string
	for _, r := range visible {
		parts = append(parts, r.content)
	}
	fullContent := strings.Join(parts, "\n")

	// 4. Compute scroll viewport
	viewportHeight := maxViewportHeight
	padToHeight := true
	if actualContentHeight <= maxViewportHeight {
		viewportHeight = max(1, actualContentHeight)
		padToHeight = false
		// If any section has an overlay, ensure the viewport is tall enough to
		// show the overlay without clipping its rows off the bottom.
		overlayNeeded := 0
		secY := 0
		for _, r := range visible {
			if r.overlay != nil && r.overlay.Content != "" {
				oh := measureHeight(r.overlay.Content)
				bottom := secY + r.overlay.OffsetY + oh
				if bottom > overlayNeeded {
					overlayNeeded = bottom
				}
			}
			secY += r.height
		}
		if overlayNeeded > viewportHeight {
			viewportHeight = min(maxViewportHeight, overlayNeeded)
			padToHeight = true
		}
	}
	m.lastViewportH = viewportHeight

	// Clamp scroll offset and cache the render-derived bounds so a pre-update
	// wheel-boundary query can answer exactly without re-rendering.
	maxScroll := max(0, actualContentHeight-viewportHeight)
	m.lastMaxScroll = maxScroll
	m.layoutValid = handler != nil
	m.scrollOffset = clamp(m.scrollOffset, 0, maxScroll)

	// Slice content to viewport
	viewport := sliceLines(fullContent, m.scrollOffset, viewportHeight, padToHeight)

	// 5. If scrollbar needed, render and join horizontally
	var viewportBar placedBar
	if needsScrollbar && contentWidth > 1 {
		barRendered, bar := m.renderViewportBar(handler, actualContentHeight, m.scrollOffset, viewportHeight)
		// Reserve the bar's column deterministically: hold every viewport
		// line to exactly the reduced content width BEFORE joining, so the
		// glyph renders in the last content column no matter where the
		// widest visible line ends. Hit registration reads this same column
		// (registerBars), so drawn bar and hit regions cannot drift apart.
		viewport = padViewportLines(viewport, contentWidth-1)
		viewport = lipgloss.JoinHorizontal(lipgloss.Top, viewport, barRendered)
		viewportBar = bar
	}

	// 5b. Fill each viewport line's background to prevent splotchy colors.
	// Inner elements with ANSI resets clear the parent's background, leaving
	// terminal-default black for the remaining width. Explicitly padding each
	// line with BgSecondary ensures a uniform background.
	viewport = styles.FillBackground(viewport, contentWidth, styles.BgSecondary)

	// 5c. Composite floating overlays onto the already-sized viewport. They
	// never change measured height; later hit-region registration uses the
	// same placements so clicks land on overlay rows.
	overlays := placeOverlays(visible, m.scrollOffset, viewportHeight)
	for _, ov := range overlays {
		viewport = compositeOverlay(viewport, ov.content, ov.x, ov.y)
		for _, f := range ov.focusables {
			if f.ID != "" {
				m.focusPositions[f.ID] = focusablePos{
					y:      ov.y + f.OffsetY,
					height: f.Height,
				}
			}
		}
	}

	// 6. Build modal content
	var inner strings.Builder
	if m.title != "" {
		inner.WriteString(renderTitleLine(m.title, m.variant))
		inner.WriteString("\n")
	}
	inner.WriteString(viewport)
	if m.showHints {
		inner.WriteString("\n")
		inner.WriteString(renderHintLine(m.hintText))
	}
	if m.customFooter != "" {
		inner.WriteString("\n")
		inner.WriteString(m.customFooter)
	}

	// 7. Apply modal style
	styled := m.modalStyle(modalWidth).Render(inner.String())
	modalH := lipgloss.Height(styled)
	modalX := (screenW - modalWidth) / 2
	modalY := (screenH - modalH) / 2

	// Calculate content area position
	contentX := modalX + 3 // border(1) + padding(2)
	contentY := modalY + 2 // border(1) + padding(1)
	if m.title != "" {
		contentY += headerLines
	}

	// 8. Register hit regions
	if handler != nil {
		handler.HitMap.Clear()

		// Background absorber (added first = lowest priority)
		handler.HitMap.AddRect(BackdropRegionID, 0, 0, screenW, screenH, nil)

		// Modal body absorber (for scroll events)
		handler.HitMap.AddRect("modal-body", modalX, modalY, modalWidth, modalH, nil)

		// Register focusable elements with measured positions
		sectionStartY := 0
		for _, r := range visible {

			for _, f := range r.focusables {
				// Calculate absolute position
				absY := contentY + sectionStartY + f.OffsetY - m.scrollOffset

				// Only register if visible in viewport
				if intersectsViewport(absY, f.Height, contentY, viewportHeight) {
					absX := contentX + f.OffsetX
					handler.HitMap.AddRect(f.ID, absX, absY, f.Width, f.Height, f.ID)
				}
			}
			sectionStartY += r.height
		}

		// Overlay regions last so they win HitMap.Test over covered fields.
		for _, ov := range overlays {
			ow := measureWidth(ov.content)
			oh := measureHeight(ov.content)
			absX := contentX + ov.x
			absY := contentY + ov.y
			topY := max(contentY, absY)
			bottomY := min(contentY+viewportHeight, absY+oh)
			if bottomY > topY && ow > 0 {
				handler.HitMap.AddRect(RegionOverlayBackdrop, absX, topY, ow, bottomY-topY, nil)
			}
			for _, f := range ov.focusables {
				if f.ID == "" {
					continue
				}
				absFY := absY + f.OffsetY
				topFY := max(contentY, absFY)
				bottomFY := min(contentY+viewportHeight, absFY+f.Height)
				if bottomFY > topFY {
					absFX := absX + f.OffsetX
					handler.HitMap.AddRect(f.ID, absFX, topFY, f.Width, bottomFY-topFY, f.ID)
				}
			}
		}

		// Interactive scrollbar regions register after everything above, so
		// the bar's column beats any content rect that reaches into it. A bar
		// with no thumb (or scrolled fully out of the viewport) registers
		// nothing at all. See scrollbar.go for the gesture contract.
		m.registerBars(handler, viewportBar, visible, contentX, contentY, contentWidth, contentY, viewportHeight)
	} else {
		// No handler: no regions can exist, so stale bar geometry must not
		// survive either.
		m.bars = m.bars[:0]
	}

	return styled
}

// filterVisible returns only sections with non-empty content.
func filterVisible(sections []renderedSection) []renderedSection {
	visible := make([]renderedSection, 0, len(sections))
	for _, r := range sections {
		if r.content != "" || r.height > 0 {
			visible = append(visible, r)
		}
	}
	return visible
}

// totalHeight sums the heights of all sections.
func totalHeight(sections []renderedSection) int {
	h := 0
	for _, r := range sections {
		h += r.height
	}
	return h
}

// modalStyle returns the lipgloss style for the modal box based on variant.
func (m *Modal) modalStyle(width int) lipgloss.Style {
	borderColor := styles.Primary
	switch m.variant {
	case VariantDanger:
		borderColor = styles.Error
	case VariantWarning:
		borderColor = styles.Warning
	case VariantInfo:
		borderColor = styles.Info
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(styles.BgSecondary).
		Padding(1, 2).
		Width(width)
}

// renderTitleLine renders the modal title.
func renderTitleLine(title string, variant Variant) string {
	titleStyle := styles.ModalTitle
	switch variant {
	case VariantDanger:
		titleStyle = titleStyle.Foreground(styles.Error)
	case VariantWarning:
		titleStyle = titleStyle.Foreground(styles.Warning)
	case VariantInfo:
		titleStyle = titleStyle.Foreground(styles.Info)
	}
	return titleStyle.Render(title)
}

// renderHintLine renders the keyboard hint line.
func renderHintLine(text string) string {
	if strings.TrimSpace(text) == "" {
		text = "Tab to switch \u00b7 Enter to confirm \u00b7 Esc to cancel"
	}
	return styles.Muted.Render(text)
}

// hintLines returns the number of lines the hint takes (0 if hidden, 1 if shown).
func hintLines(show bool) int {
	if show {
		return 1
	}
	return 0
}

// desiredModalInnerHeight calculates the max inner height based on screen size:
// the surface less the box's own border and padding, less the margin the modal
// keeps clear above and below itself.
func desiredModalInnerHeight(screenH, marginY int) int {
	maxH := screenH - ChromeHeight - 2*marginY
	if maxH < 1 {
		maxH = 1
	}
	return maxH
}

// sliceLines extracts a viewport from content starting at offset for height lines.
// Pads with empty lines if padToHeight is true.
func sliceLines(content string, offset, height int, padToHeight bool) string {
	lines := strings.Split(content, "\n")

	// Handle offset
	if offset >= len(lines) {
		offset = max(0, len(lines)-1)
	}
	lines = lines[offset:]

	// Truncate to height
	if len(lines) > height {
		lines = lines[:height]
	}

	// Pad if needed
	if padToHeight {
		for len(lines) < height {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

// padViewportLines holds every viewport line to exactly width columns so the
// scrollbar joins at a fixed column: the drawn bar's position becomes a fact
// of the layout rather than a side effect of wherever the widest visible line
// happens to end (which moves with scroll position on ragged content).
func padViewportLines(content string, width int) string {
	if width < 1 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		switch w := ansi.StringWidth(line); {
		case w < width:
			lines[i] = line + strings.Repeat(" ", width-w)
		case w > width:
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// intersectsViewport checks if an element at y with height h intersects the viewport.
func intersectsViewport(y, h, viewportY, viewportH int) bool {
	elementTop := y
	elementBottom := y + h
	viewportTop := viewportY
	viewportBottom := viewportY + viewportH

	return elementTop < viewportBottom && elementBottom > viewportTop
}

// overlayPlacement is an overlay positioned in viewport coordinates.
type overlayPlacement struct {
	content    string
	x, y       int
	focusables []FocusableInfo
}

// placeOverlays positions each section overlay in the viewport, flipping
// above the field when it would clip off the bottom.
func placeOverlays(sections []renderedSection, scrollOffset, viewportHeight int) []overlayPlacement {
	var out []overlayPlacement
	sectionY := 0
	for _, r := range sections {
		if r.overlay != nil && r.overlay.Content != "" {
			if p, ok := placeOverlay(*r.overlay, sectionY, scrollOffset, viewportHeight); ok {
				out = append(out, p)
			}
		}
		sectionY += r.height
	}
	return out
}

func placeOverlay(ov Overlay, sectionY, scroll, viewportH int) (overlayPlacement, bool) {
	oh := measureHeight(ov.Content)
	if oh == 0 {
		return overlayPlacement{}, false
	}
	preferY := sectionY + ov.OffsetY - scroll
	y := preferY
	if preferY+oh > viewportH {
		visBelow := max(0, viewportH-preferY)
		visAbove := max(0, min(oh, sectionY-scroll))
		if visAbove > visBelow {
			y = sectionY - oh - scroll
			// Overlap the field rather than clip the highlight off the top.
			if y < 0 {
				y = 0
			}
		}
	}
	return overlayPlacement{
		content:    ov.Content,
		x:          ov.OffsetX,
		y:          y,
		focusables: ov.Focusables,
	}, true
}

// compositeOverlay draws overlay onto base at (x, y) without adding lines. The
// cell arithmetic lives in internal/overlay, which the Configuration surface
// composites its own dropdowns with: there is one way to paint a floating list
// over what is behind it, not one per surface.
func compositeOverlay(base, over string, x, y int) string {
	return overlay.Composite(base, over, x, y)
}

// clamp constrains a value between min and max.
func clamp(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

// measureWidth returns the maximum cell width among all lines in content.
func measureWidth(content string) int {
	if content == "" {
		return 0
	}
	w := 0
	for _, line := range strings.Split(content, "\n") {
		if lw := ansi.StringWidth(line); lw > w {
			w = lw
		}
	}
	return w
}

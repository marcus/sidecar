package modal

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
)

// renderedSection holds a section's rendered content and metadata.
type renderedSection struct {
	content    string
	height     int
	focusables []FocusableInfo
}

// buildLayout renders all sections, measures heights, and registers hit regions.
func (m *Modal) buildLayout(screenW, screenH int, handler *mouse.Handler) string {
	// Clamp modal width
	maxWidth := screenW - 4
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

	// 1. Render sections individually, measure heights, collect focusables
	focusID := m.currentFocusID()
	rendered := make([]renderedSection, 0, len(m.sections))
	m.focusIDs = m.focusIDs[:0] // Reset focusable IDs

	for _, s := range m.sections {
		res := s.Render(contentWidth, focusID, m.hoverID)
		height := measureHeight(res.Content)

		rendered = append(rendered, renderedSection{
			content:    res.Content,
			height:     height,
			focusables: res.Focusables,
		})

		// Collect focusable IDs in order
		for _, f := range res.Focusables {
			m.focusIDs = append(m.focusIDs, f.ID)
		}
	}

	// Ensure focusIdx is valid
	if len(m.focusIDs) > 0 && m.focusIdx >= len(m.focusIDs) {
		m.focusIdx = 0
	}

	// Filter out zero-height sections (e.g., inactive When)
	visible := make([]renderedSection, 0, len(rendered))
	for _, r := range rendered {
		if r.content != "" || r.height > 0 {
			visible = append(visible, r)
		}
	}

	// 2. Join full content with newlines between non-empty sections
	var parts []string
	for _, r := range visible {
		parts = append(parts, r.content)
	}
	fullContent := strings.Join(parts, "\n")

	// 3. Compute scroll viewport
	totalContentHeight := measureHeight(fullContent)

	modalInnerHeight := desiredModalInnerHeight(screenH)
	headerLines := 0
	if m.title != "" {
		headerLines = 2 // title + blank line
	}
	footerLines := hintLines(m.showHints)
	maxViewportHeight := max(1, modalInnerHeight-headerLines-footerLines)

	viewportHeight := maxViewportHeight
	padToHeight := true
	if totalContentHeight <= maxViewportHeight {
		viewportHeight = max(1, totalContentHeight)
		padToHeight = false
	}

	// Clamp scroll offset
	maxScroll := max(0, totalContentHeight-viewportHeight)
	m.scrollOffset = clamp(m.scrollOffset, 0, maxScroll)

	// Slice content to viewport
	viewport := sliceLines(fullContent, m.scrollOffset, viewportHeight, padToHeight)

	// 4. Build modal content
	var inner strings.Builder
	if m.title != "" {
		inner.WriteString(renderTitleLine(m.title, m.variant))
		inner.WriteString("\n")
	}
	inner.WriteString(viewport)
	if m.showHints {
		inner.WriteString("\n")
		inner.WriteString(renderHintLine())
	}

	// 5. Apply modal style
	styled := m.modalStyle(modalWidth).Render(inner.String())
	modalH := lipgloss.Height(styled)
	modalX := (screenW - modalWidth) / 2
	modalY := (screenH - modalH) / 2

	// 6. Register hit regions
	if handler != nil {
		handler.HitMap.Clear()

		// Background absorber (added first = lowest priority)
		handler.HitMap.AddRect("modal-backdrop", 0, 0, screenW, screenH, nil)

		// Modal body absorber (for scroll events)
		handler.HitMap.AddRect("modal-body", modalX, modalY, modalWidth, modalH, nil)

		// Calculate content area position
		contentX := modalX + 3 // border(1) + padding(2)
		contentY := modalY + 2 // border(1) + padding(1)
		if m.title != "" {
			contentY += headerLines
		}

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
	}

	return styled
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
func renderHintLine() string {
	return styles.Muted.Render("Tab to switch \u00b7 Enter to confirm \u00b7 Esc to cancel")
}

// hintLines returns the number of lines the hint takes (0 if hidden, 1 if shown).
func hintLines(show bool) int {
	if show {
		return 1
	}
	return 0
}

// desiredModalInnerHeight calculates the max inner height based on screen size.
func desiredModalInnerHeight(screenH int) int {
	// Leave room for modal border and some margin
	maxH := screenH - 6
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

// intersectsViewport checks if an element at y with height h intersects the viewport.
func intersectsViewport(y, h, viewportY, viewportH int) bool {
	elementTop := y
	elementBottom := y + h
	viewportTop := viewportY
	viewportBottom := viewportY + viewportH

	return elementTop < viewportBottom && elementBottom > viewportTop
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

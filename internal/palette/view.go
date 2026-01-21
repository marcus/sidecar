package palette

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// keyColumnWidth is the fixed width for the key column to ensure alignment.
// Fits "shift+tab" (9 chars) + KeyHint padding (2) + 1 buffer.
const keyColumnWidth = 12

// Palette-specific styles
var (
	paletteBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.Primary).
			Background(styles.BgSecondary).
			Padding(1, 2)

	paletteInput = lipgloss.NewStyle().
			Foreground(styles.TextPrimary).
			Background(styles.BgTertiary).
			Padding(0, 1).
			MarginBottom(1)

	layerHeaderCurrent = lipgloss.NewStyle().
				Foreground(styles.Primary).
				Bold(true).
				PaddingLeft(1).
				MarginTop(1)

	layerHeaderPlugin = lipgloss.NewStyle().
				Foreground(styles.Secondary).
				Bold(true).
				PaddingLeft(1).
				MarginTop(1)

	layerHeaderGlobal = lipgloss.NewStyle().
				Foreground(styles.TextSubtle).
				PaddingLeft(1).
				MarginTop(1)

	entryNormal = lipgloss.NewStyle().
			Foreground(styles.TextPrimary)

	entrySelected = lipgloss.NewStyle().
			Foreground(styles.TextPrimary).
			Background(styles.BgTertiary)

	entryName = lipgloss.NewStyle().
			Foreground(styles.TextPrimary).
			Width(20)

	entryDesc = lipgloss.NewStyle().
			Foreground(styles.TextSecondary)

	matchHighlight = lipgloss.NewStyle().
			Foreground(styles.Primary).
			Bold(true)
)

// renderItem represents a single line in the palette (header or entry).
type renderItem struct {
	isHeader   bool
	layer      Layer
	entry      *PaletteEntry
	entryIndex int // index in filtered entries (for cursor matching)
}

// View renders the command palette.
func (m Model) View() string {
	// Clear hit regions from previous render
	m.mouseHandler.Clear()

	var b strings.Builder

	// Calculate width
	width := min(80, m.width-4)
	if width < 40 {
		width = 40
	}

	// Header with search input
	// Calculate content width (inside padding)
	contentWidth := width - 4

	promptPrefix := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true).Render(">")
	escChip := styles.KeyHint.Render("esc")
	inputWidth := contentWidth - lipgloss.Width(promptPrefix) - lipgloss.Width(escChip) - 3
	paddedInput := lipgloss.NewStyle().Width(inputWidth).Render(m.textInput.View())
	header := fmt.Sprintf("%s %s %s", promptPrefix, paddedInput, escChip)
	b.WriteString(header)
	b.WriteString("\n")

	// Mode indicator with context badge
	var modeText string
	if m.showAllContexts {
		modeText = styles.BarChip.Render("All Contexts")
	} else {
		modeText = styles.BarChip.Render(m.activeContext)
	}
	toggleHint := styles.Muted.Render("tab to toggle")
	b.WriteString(fmt.Sprintf("%s  %s", modeText, toggleHint))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", contentWidth))
	b.WriteString("\n")

	// Build flat list of render items
	items := m.buildRenderItems()
	totalEntries := len(m.filtered)

	// Calculate visible range based on entry indices
	visibleStart := m.offset
	visibleEnd := m.offset + m.maxVisible
	if visibleEnd > totalEntries {
		visibleEnd = totalEntries
	}

	// Track Y position for hit regions (relative to modal content)
	// Header = 3 lines (input, mode indicator, divider)
	currentY := 3

	// Show scroll-up indicator if content above
	if m.offset > 0 {
		b.WriteString(styles.Muted.Render(fmt.Sprintf("  ↑ %d more above", m.offset)))
		b.WriteString("\n")
		currentY++
	}

	// Render only visible items
	for _, item := range items {
		if item.isHeader {
			// Show header only if it has visible entries
			if m.layerHasVisibleEntries(item.layer, visibleStart, visibleEnd) {
				b.WriteString(m.renderLayerHeader(item.layer, m.countEntriesInLayer(item.layer)))
				b.WriteString("\n")
				currentY++
			}
		} else {
			// Only render entries within visible range
			if item.entryIndex >= visibleStart && item.entryIndex < visibleEnd {
				isSelected := item.entryIndex == m.cursor
				line := m.renderEntry(*item.entry, isSelected, width-4)
				b.WriteString(line)
				b.WriteString("\n")

				// Register hit region for this entry
				// Region is full width, 1 line tall, data = entry index
				m.mouseHandler.HitMap.AddRect(regionPaletteEntry, 0, currentY, width, 1, item.entryIndex)
				currentY++
			}
		}
	}

	// Show scroll-down indicator if content below
	if visibleEnd < totalEntries {
		remaining := totalEntries - visibleEnd
		b.WriteString(styles.Muted.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n")
	}

	// Empty state
	if len(m.filtered) == 0 {
		emptyMsg := styles.Muted.Render("No matching commands")
		b.WriteString("\n")
		b.WriteString(emptyMsg)
		b.WriteString("\n")
	}

	// Wrap in box
	content := strings.TrimRight(b.String(), "\n")
	box := paletteBox.Width(width).Render(content)

	return box
}

// buildRenderItems creates a flat list of headers and entries for rendering.
func (m Model) buildRenderItems() []renderItem {
	groups := GroupEntriesByLayer(m.filtered)
	layers := []Layer{LayerCurrentMode, LayerPlugin, LayerGlobal}

	var items []renderItem
	entryIndex := 0

	for _, layer := range layers {
		entries, ok := groups[layer]
		if !ok || len(entries) == 0 {
			continue
		}

		// Add layer header
		items = append(items, renderItem{isHeader: true, layer: layer})

		// Add entries
		for i := range entries {
			items = append(items, renderItem{
				entry:      &entries[i],
				entryIndex: entryIndex,
			})
			entryIndex++
		}
	}

	return items
}

// layerHasVisibleEntries checks if a layer has any entries in the visible range.
func (m Model) layerHasVisibleEntries(layer Layer, visibleStart, visibleEnd int) bool {
	groups := GroupEntriesByLayer(m.filtered)
	layers := []Layer{LayerCurrentMode, LayerPlugin, LayerGlobal}

	entryIndex := 0
	for _, l := range layers {
		entries := groups[l]
		layerStart := entryIndex
		layerEnd := entryIndex + len(entries)

		if l == layer {
			// Check if any entries in this layer fall within visible range
			return layerStart < visibleEnd && layerEnd > visibleStart
		}
		entryIndex = layerEnd
	}
	return false
}

// countEntriesInLayer returns the count of entries in a specific layer.
func (m Model) countEntriesInLayer(layer Layer) int {
	groups := GroupEntriesByLayer(m.filtered)
	return len(groups[layer])
}

// renderLayerHeader renders a layer section header.
func (m Model) renderLayerHeader(layer Layer, count int) string {
	var style lipgloss.Style
	var name string

	switch layer {
	case LayerCurrentMode:
		style = layerHeaderCurrent
		name = strings.ToUpper(m.activeContext)
	case LayerPlugin:
		style = layerHeaderPlugin
		name = strings.ToUpper(m.pluginContext)
	case LayerGlobal:
		style = layerHeaderGlobal
		name = "GLOBAL"
	}

	return style.Render(name)
}

// renderEntry renders a single palette entry.
func (m Model) renderEntry(entry PaletteEntry, selected bool, maxWidth int) string {
	// Key column - render as pill/chip using KeyHint style
	keyStr := styles.KeyHint.Render(entry.Key)
	keyWidth := lipgloss.Width(keyStr)

	// Pad key to fixed column width for alignment
	if keyWidth < keyColumnWidth {
		keyStr = keyStr + strings.Repeat(" ", keyColumnWidth-keyWidth)
	}

	// Name with match highlighting
	nameStr := m.highlightMatches(entry.Name, entry.MatchRanges)
	nameStr = entryName.Render(nameStr)

	// Description (truncate if needed)
	// Account for: 2 leading spaces + keyColumnWidth + 1 space + 20 name + 1 space
	descWidth := maxWidth - keyColumnWidth - 20 - 4
	desc := entry.Description

	// Show context count if command appears in multiple contexts
	if entry.ContextCount > 1 {
		desc = fmt.Sprintf("%s (%d contexts)", desc, entry.ContextCount)
	}

	if descWidth > 3 && len(desc) > descWidth {
		desc = desc[:descWidth-3] + "..."
	}
	descStr := entryDesc.Render(desc)

	line := fmt.Sprintf("  %s %s %s", keyStr, nameStr, descStr)

	// Pad to full width for consistent selection highlighting
	paddedLine := lipgloss.NewStyle().Width(maxWidth).Render(line)

	if selected {
		return entrySelected.Width(maxWidth).Render(paddedLine)
	}
	return entryNormal.Render(paddedLine)
}

// highlightMatches applies highlighting to matched characters.
func (m Model) highlightMatches(text string, ranges []MatchRange) string {
	if len(ranges) == 0 {
		return text
	}

	var result strings.Builder
	lastEnd := 0

	for _, r := range ranges {
		// Add non-matched part
		if r.Start > lastEnd {
			result.WriteString(text[lastEnd:r.Start])
		}
		// Add matched part with highlighting
		if r.End <= len(text) {
			result.WriteString(matchHighlight.Render(text[r.Start:r.End]))
		}
		lastEnd = r.End
	}

	// Add remaining text
	if lastEnd < len(text) {
		result.WriteString(text[lastEnd:])
	}

	return result.String()
}

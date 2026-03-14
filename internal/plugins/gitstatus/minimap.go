package gitstatus

import (
	"log/slog"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// MinimapWidth is the total character width of the minimap column (rail + map).
const MinimapWidth = 3 // "█▀▀" or "▀▀▀" etc.

// Minimap palette — dim for outside viewport, bright for inside.
var (
	mmContextDim    = lipgloss.Color("#1a1e28")
	mmContextBright = lipgloss.Color("#2d3548")
	mmAddDim        = lipgloss.Color("#0e2e1a")
	mmAddBright     = lipgloss.Color("#166534")
	mmRemoveDim     = lipgloss.Color("#2e0e0e")
	mmRemoveBright  = lipgloss.Color("#7f1d1d")
)

// RenderMinimap produces a minimap for the full-file diff view.
//
// Each terminal row encodes two file-line slots via the half-block character ▀
// (foreground = top slot, background = bottom slot), giving 2× vertical resolution.
//
// scrollPos is the first visible line index, visibleLines is the viewport height
// in file lines, and height is the minimap height in terminal rows.
//
// The output ends with a trailing newline on each row (matching the diff renderer
// format) so that lipgloss.JoinHorizontal aligns correctly.
func RenderMinimap(fullDiff *FullFileDiff, scrollPos, visibleLines, height int) string {
	if fullDiff == nil || len(fullDiff.Lines) == 0 || height < 2 {
		return ""
	}

	totalLines := len(fullDiff.Lines)

	// Clamp scrollPos.
	if scrollPos < 0 {
		scrollPos = 0
	}

	// Cap minimap height so it never exceeds the diff renderer's actual row count.
	// RenderFullFileSideBySide produces at most min(totalLines, maxLines) rows,
	// so the minimap must not be taller than the diff content.
	if height > totalLines {
		height = totalLines
	}
	if height < 2 {
		return ""
	}

	// Clamp viewport bounds.
	viewEnd := scrollPos + visibleLines
	if viewEnd > totalLines {
		viewEnd = totalLines
	}

	// Each minimap row has 2 slots; map slots → file line ranges.
	totalSlots := height * 2
	linesPerSlot := float64(totalLines) / float64(totalSlots)

	slog.Debug("minimap render",
		"totalLines", totalLines,
		"scrollPos", scrollPos,
		"visibleLines", visibleLines,
		"viewEnd", viewEnd,
		"height", height,
		"totalSlots", totalSlots,
		"linesPerSlot", linesPerSlot,
	)

	railColor := styles.Primary

	// Track rail rows for debug logging.
	railCount := 0
	firstRailRow := -1
	lastRailRow := -1

	var sb strings.Builder
	for row := 0; row < height; row++ {
		// --- Diff density map slots ---
		topSlot := row * 2
		bottomSlot := topSlot + 1

		topStart, topEnd := slotLineRange(topSlot, linesPerSlot, totalLines)
		bottomStart, bottomEnd := slotLineRange(bottomSlot, linesPerSlot, totalLines)

		topType := slotDominantType(fullDiff.Lines, topStart, topEnd)
		bottomType := slotDominantType(fullDiff.Lines, bottomStart, bottomEnd)

		topInView := rangesOverlap(topStart, topEnd, scrollPos, viewEnd)
		bottomInView := rangesOverlap(bottomStart, bottomEnd, scrollPos, viewEnd)

		// --- Viewport rail: half-block precision matching the map ---
		// Uses ▀/▄/█ so the rail boundary aligns exactly with the
		// bright/dim boundary of the map cells.
		switch {
		case topInView && bottomInView:
			// Full row in viewport — solid block.
			sb.WriteString(lipgloss.NewStyle().Foreground(railColor).Render("█"))
			if firstRailRow < 0 {
				firstRailRow = row
			}
			lastRailRow = row
			railCount++
		case topInView:
			// Only top half in viewport.
			sb.WriteString(lipgloss.NewStyle().Foreground(railColor).Render("▀"))
			if firstRailRow < 0 {
				firstRailRow = row
			}
			lastRailRow = row
			railCount++
		case bottomInView:
			// Only bottom half in viewport.
			sb.WriteString(lipgloss.NewStyle().Foreground(railColor).Render("▄"))
			if firstRailRow < 0 {
				firstRailRow = row
			}
			lastRailRow = row
			railCount++
		default:
			sb.WriteString(" ")
		}

		fg := mmColor(topType, topInView)
		bg := mmColor(bottomType, bottomInView)

		// ▀ renders top half in foreground, bottom half in background.
		mapCell := lipgloss.NewStyle().Foreground(fg).Background(bg).Render("▀▀")
		sb.WriteString(mapCell)

		// Trailing newline on every row (matches diff renderer format).
		sb.WriteString("\n")
	}

	slog.Debug("minimap rail",
		"railRows", railCount,
		"firstRow", firstRailRow,
		"lastRow", lastRailRow,
		"totalRows", height,
	)

	return sb.String()
}

// MinimapScrollTarget converts a minimap click Y position to a scroll target.
// clickRow is the 0-based row within the minimap area.
func MinimapScrollTarget(clickRow, minimapHeight, totalLines, visibleLines int) int {
	if minimapHeight <= 0 || totalLines <= 0 {
		return 0
	}
	if clickRow < 0 {
		clickRow = 0
	}
	if clickRow >= minimapHeight {
		clickRow = minimapHeight - 1
	}
	// Map click to the center of that row's file range.
	targetLine := (clickRow*2 + 1) * totalLines / (minimapHeight * 2)
	// Center viewport on that line.
	target := targetLine - visibleLines/2
	if target < 0 {
		target = 0
	}
	maxScroll := totalLines - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if target > maxScroll {
		target = maxScroll
	}
	return target
}

// slotLineRange returns the [start, end) file line range for a given slot index.
// When the minimap is taller than the file, multiple consecutive slots map to the
// same trailing line — this is expected and produces a visually uniform bottom region.
func slotLineRange(slot int, linesPerSlot float64, totalLines int) (int, int) {
	start := int(float64(slot) * linesPerSlot)
	end := int(float64(slot+1) * linesPerSlot)
	if end <= start {
		end = start + 1
	}
	if start >= totalLines {
		start = totalLines - 1
	}
	if start < 0 {
		start = 0
	}
	if end > totalLines {
		end = totalLines
	}
	return start, end
}

// slotDominantType returns the dominant diff line type in [start, end).
// Changes (add/remove) take priority over context lines when present.
func slotDominantType(lines []FullFileLine, start, end int) LineType {
	if start >= len(lines) || start >= end {
		return LineContext
	}
	if end > len(lines) {
		end = len(lines)
	}

	var adds, removes int
	for i := start; i < end; i++ {
		switch lines[i].Type {
		case LineAdd:
			adds++
		case LineRemove:
			removes++
		}
	}

	if adds > 0 || removes > 0 {
		if adds >= removes {
			return LineAdd
		}
		return LineRemove
	}
	return LineContext
}

// rangesOverlap returns true if [a0, a1) and [b0, b1) overlap.
func rangesOverlap(a0, a1, b0, b1 int) bool {
	return a0 < b1 && a1 > b0
}

// mmColor returns the minimap color for a line type and viewport membership.
func mmColor(lt LineType, inViewport bool) lipgloss.Color {
	if inViewport {
		switch lt {
		case LineAdd:
			return mmAddBright
		case LineRemove:
			return mmRemoveBright
		default:
			return mmContextBright
		}
	}
	switch lt {
	case LineAdd:
		return mmAddDim
	case LineRemove:
		return mmRemoveDim
	default:
		return mmContextDim
	}
}

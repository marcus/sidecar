package gitstatus

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/ui"
)

// Package-level cache for gitstatus rendering. Safe because Bubble Tea updates run on one goroutine.
var truncCache = ui.NewTruncateCache(1000)

func truncateStyledLineCached(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return truncCache.Truncate(s, maxWidth, "")
}

func truncateLeftCached(s string, offset int) string {
	if offset <= 0 {
		return s
	}
	return truncCache.TruncateLeft(s, offset, "")
}

func clearTruncCache() { truncCache.Clear() }

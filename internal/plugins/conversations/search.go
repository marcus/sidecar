package conversations

import (
	"strings"
	"time"

	"github.com/sst/sidecar/internal/adapter"
)

// SearchFilters holds multi-dimensional filter criteria.
type SearchFilters struct {
	Query      string    // Text search
	Models     []string  // ["opus", "sonnet", "haiku"]
	DateRange  DateRange // today, week, custom
	MinTokens  int       // Sessions with > N tokens
	MaxTokens  int       // Sessions with < N tokens
	ActiveOnly bool      // Only currently active
	HasFiles   []string  // Sessions that touched these files
}

// DateRange represents a date range filter.
type DateRange struct {
	Preset string    // "today", "yesterday", "week", "month", "all"
	Start  time.Time // For custom range
	End    time.Time
}

// IsActive returns true if any filter is active.
func (f *SearchFilters) IsActive() bool {
	return f.Query != "" ||
		len(f.Models) > 0 ||
		f.DateRange.Preset != "" ||
		f.MinTokens > 0 ||
		f.MaxTokens > 0 ||
		f.ActiveOnly ||
		len(f.HasFiles) > 0
}

// ToggleModel toggles a model in the filter list.
func (f *SearchFilters) ToggleModel(model string) {
	for i, m := range f.Models {
		if m == model {
			f.Models = append(f.Models[:i], f.Models[i+1:]...)
			return
		}
	}
	f.Models = append(f.Models, model)
}

// HasModel returns true if the model is in the filter list.
func (f *SearchFilters) HasModel(model string) bool {
	for _, m := range f.Models {
		if m == model {
			return true
		}
	}
	return false
}

// SetDateRange sets the date range preset.
func (f *SearchFilters) SetDateRange(preset string) {
	if f.DateRange.Preset == preset {
		f.DateRange.Preset = "" // Toggle off
		return
	}
	f.DateRange.Preset = preset

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch preset {
	case "today":
		f.DateRange.Start = today
		f.DateRange.End = now
	case "yesterday":
		f.DateRange.Start = today.AddDate(0, 0, -1)
		f.DateRange.End = today
	case "week":
		f.DateRange.Start = today.AddDate(0, 0, -7)
		f.DateRange.End = now
	case "month":
		f.DateRange.Start = today.AddDate(0, -1, 0)
		f.DateRange.End = now
	default:
		f.DateRange.Start = time.Time{}
		f.DateRange.End = time.Time{}
	}
}

// Matches checks if a session matches all filter criteria.
func (f *SearchFilters) Matches(session adapter.Session) bool {
	// Text search
	if f.Query != "" {
		query := strings.ToLower(f.Query)
		if !strings.Contains(strings.ToLower(session.Name), query) &&
			!strings.Contains(strings.ToLower(session.Slug), query) &&
			!strings.Contains(session.ID, query) {
			return false
		}
	}

	// Model filter
	if len(f.Models) > 0 {
		// Would need session.PrimaryModel field to check this
		// For now, skip model filtering at session level
	}

	// Date range filter
	if f.DateRange.Preset != "" {
		if session.UpdatedAt.Before(f.DateRange.Start) || session.UpdatedAt.After(f.DateRange.End) {
			return false
		}
	}

	// Token filters
	if f.MinTokens > 0 && session.TotalTokens < f.MinTokens {
		return false
	}
	if f.MaxTokens > 0 && session.TotalTokens > f.MaxTokens {
		return false
	}

	// Active only filter
	if f.ActiveOnly && !session.IsActive {
		return false
	}

	return true
}

// String formats active filters for display.
func (f *SearchFilters) String() string {
	var parts []string

	if len(f.Models) > 0 {
		parts = append(parts, "[model:"+strings.Join(f.Models, ",")+"]")
	}
	if f.DateRange.Preset != "" {
		parts = append(parts, "["+f.DateRange.Preset+"]")
	}
	if f.MinTokens > 0 {
		parts = append(parts, "[tokens:>"+formatTokenCount(f.MinTokens)+"]")
	}
	if f.MaxTokens > 0 {
		parts = append(parts, "[tokens:<"+formatTokenCount(f.MaxTokens)+"]")
	}
	if f.ActiveOnly {
		parts = append(parts, "[active]")
	}

	return strings.Join(parts, " ")
}

// formatTokenCount formats a token count for display.
func formatTokenCount(n int) string {
	if n >= 1000000 {
		return strings.TrimSuffix(strings.TrimSuffix(
			strings.Replace(string(rune('0'+n/1000000)), "0", "", 1), "0"), "0") + "M"
	}
	if n >= 1000 {
		return strings.TrimSuffix(string(rune('0'+n/1000)), "0") + "k"
	}
	return string(rune('0' + n))
}

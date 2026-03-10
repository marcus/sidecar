package conversations

import (
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestSearchFilters_IsActive(t *testing.T) {
	f := &SearchFilters{}
	if f.IsActive() {
		t.Error("empty filters should not be active")
	}

	f.Query = "test"
	if !f.IsActive() {
		t.Error("filters with query should be active")
	}
}

func TestSearchFilters_ToggleModel(t *testing.T) {
	f := &SearchFilters{}

	f.ToggleModel("opus")
	if !f.HasModel("opus") {
		t.Error("after toggle on, HasModel should be true")
	}
	if len(f.Models) != 1 {
		t.Errorf("expected 1 model, got %d", len(f.Models))
	}

	f.ToggleModel("sonnet")
	if len(f.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(f.Models))
	}

	// Toggle off
	f.ToggleModel("opus")
	if f.HasModel("opus") {
		t.Error("after toggle off, HasModel should be false")
	}
	if len(f.Models) != 1 {
		t.Errorf("expected 1 model after removal, got %d", len(f.Models))
	}
}

func TestSearchFilters_ToggleAdapter(t *testing.T) {
	f := &SearchFilters{}

	f.ToggleAdapter("claude-code")
	if !f.HasAdapter("claude-code") {
		t.Error("should have adapter after toggle on")
	}

	f.ToggleAdapter("claude-code")
	if f.HasAdapter("claude-code") {
		t.Error("should not have adapter after toggle off")
	}
}

func TestSearchFilters_SetDateRange(t *testing.T) {
	f := &SearchFilters{}

	f.SetDateRange("today")
	if f.DateRange.Preset != "today" {
		t.Errorf("preset = %q, want %q", f.DateRange.Preset, "today")
	}
	if f.DateRange.Start.IsZero() {
		t.Error("start should be set for 'today'")
	}

	// Toggle off
	f.SetDateRange("today")
	if f.DateRange.Preset != "" {
		t.Errorf("preset should be empty after toggle off, got %q", f.DateRange.Preset)
	}

	// Test different presets
	for _, preset := range []string{"yesterday", "week", "month"} {
		f.SetDateRange(preset)
		if f.DateRange.Preset != preset {
			t.Errorf("preset = %q, want %q", f.DateRange.Preset, preset)
		}
		if f.DateRange.Start.IsZero() {
			t.Errorf("start should be set for %q", preset)
		}
		f.SetDateRange(preset) // toggle off
	}
}

func TestSearchFilters_Matches(t *testing.T) {
	now := time.Now()
	session := adapter.Session{
		ID:          "ses_123",
		Name:        "Fix authentication bug",
		AdapterID:   "claude-code",
		AdapterName: "Claude Code",
		UpdatedAt:   now,
		TotalTokens: 5000,
		IsActive:    false,
	}

	tests := []struct {
		name    string
		filters SearchFilters
		want    bool
	}{
		{"empty filters match all", SearchFilters{}, true},
		{"matching query", SearchFilters{Query: "auth"}, true},
		{"non-matching query", SearchFilters{Query: "database"}, false},
		{"case insensitive query", SearchFilters{Query: "FIX"}, true},
		{"matching adapter", SearchFilters{Adapters: []string{"claude-code"}}, true},
		{"non-matching adapter", SearchFilters{Adapters: []string{"codex"}}, false},
		{"min tokens pass", SearchFilters{MinTokens: 1000}, true},
		{"min tokens fail", SearchFilters{MinTokens: 10000}, false},
		{"max tokens pass", SearchFilters{MaxTokens: 10000}, true},
		{"max tokens fail", SearchFilters{MaxTokens: 100}, false},
		{"active only fail", SearchFilters{ActiveOnly: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filters.Matches(session); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchFilters_Matches_CategoryPassthrough(t *testing.T) {
	// Sessions with empty SessionCategory (non-Pi adapters) should always
	// pass through the category filter (td-d3b1f6)
	emptyCategory := adapter.Session{
		ID:              "ses_cc_1",
		Name:            "Claude Code session",
		AdapterID:       "claude-code",
		SessionCategory: "", // non-Pi adapter: no category set
	}
	piInteractive := adapter.Session{
		ID:              "ses_pi_1",
		Name:            "Pi interactive",
		AdapterID:       "pi",
		SessionCategory: "interactive",
	}
	piSystem := adapter.Session{
		ID:              "ses_pi_2",
		Name:            "Pi system",
		AdapterID:       "pi",
		SessionCategory: "system",
	}

	f := SearchFilters{Categories: []string{"interactive"}}

	if !f.Matches(emptyCategory) {
		t.Error("session with empty category should pass through category filter")
	}
	if !f.Matches(piInteractive) {
		t.Error("interactive Pi session should match interactive filter")
	}
	if f.Matches(piSystem) {
		t.Error("system Pi session should NOT match interactive filter")
	}
}

func TestSearchFilters_Matches_DateRange(t *testing.T) {
	f := &SearchFilters{}
	f.SetDateRange("today")

	// Use a time anchored to the filter's own Start boundary so the test is
	// timezone-agnostic. "2 hours ago" can cross UTC midnight in CI.
	withinToday := adapter.Session{
		UpdatedAt: f.DateRange.Start.Add(time.Minute), // 1 minute after today's start
	}
	if !f.Matches(withinToday) {
		t.Error("session 1 minute after today's start should match 'today' range")
	}

	outsideToday := adapter.Session{
		UpdatedAt: f.DateRange.Start.Add(-24 * time.Hour), // clearly yesterday
	}
	if f.Matches(outsideToday) {
		t.Error("session from yesterday should not match 'today' range")
	}
}

func TestSearchFilters_String(t *testing.T) {
	f := &SearchFilters{
		Models:    []string{"opus", "sonnet"},
		Adapters:  []string{"claude-code"},
		MinTokens: 1000,
	}
	f.SetDateRange("week")

	s := f.String()
	if s == "" {
		t.Error("String() should not be empty with active filters")
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{500, "500"},
		{1000, "1k"},
		{5000, "5k"},
		{1000000, "1M"},
		{2500000, "2M"},
	}

	for _, tt := range tests {
		got := formatTokenCount(tt.input)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

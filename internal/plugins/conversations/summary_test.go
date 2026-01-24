package conversations

import (
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestComputeSessionSummary_Empty(t *testing.T) {
	summary := ComputeSessionSummary(nil, 10*time.Minute)

	if summary.MessageCount != 0 {
		t.Errorf("expected MessageCount 0, got %d", summary.MessageCount)
	}
	if summary.FileCount != 0 {
		t.Errorf("expected FileCount 0, got %d", summary.FileCount)
	}
	if summary.Duration != 10*time.Minute {
		t.Errorf("expected Duration 10m, got %s", summary.Duration)
	}
}

func TestComputeSessionSummary_SingleMessage(t *testing.T) {
	messages := []adapter.Message{
		{
			Model:      "claude-opus-4-5-20251101",
			TokenUsage: adapter.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			ToolUses:   []adapter.ToolUse{{Name: "Read", Input: `{"file_path": "/foo/bar.go"}`}},
		},
	}
	summary := ComputeSessionSummary(messages, 5*time.Minute)

	if summary.MessageCount != 1 {
		t.Errorf("expected MessageCount 1, got %d", summary.MessageCount)
	}
	if summary.TotalTokensIn != 1000 {
		t.Errorf("expected TotalTokensIn 1000, got %d", summary.TotalTokensIn)
	}
	if summary.TotalTokensOut != 500 {
		t.Errorf("expected TotalTokensOut 500, got %d", summary.TotalTokensOut)
	}
	if summary.PrimaryModel != "claude-opus-4-5-20251101" {
		t.Errorf("expected PrimaryModel opus, got %s", summary.PrimaryModel)
	}
	if summary.FileCount != 1 {
		t.Errorf("expected FileCount 1, got %d", summary.FileCount)
	}
	if summary.ToolCounts["Read"] != 1 {
		t.Errorf("expected Read count 1, got %d", summary.ToolCounts["Read"])
	}
}

func TestComputeSessionSummary_MultipleMessages(t *testing.T) {
	messages := []adapter.Message{
		{
			Model:      "claude-sonnet-4-5-20250929",
			TokenUsage: adapter.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			ToolUses:   []adapter.ToolUse{{Name: "Read", Input: `{"file_path": "/a.go"}`}},
		},
		{
			Model:      "claude-sonnet-4-5-20250929",
			TokenUsage: adapter.TokenUsage{InputTokens: 2000, OutputTokens: 1000},
			ToolUses:   []adapter.ToolUse{{Name: "Edit", Input: `{"file_path": "/a.go"}`}},
		},
		{
			Model:      "claude-opus-4-5-20251101",
			TokenUsage: adapter.TokenUsage{InputTokens: 500, OutputTokens: 250},
			ToolUses:   []adapter.ToolUse{{Name: "Read", Input: `{"file_path": "/b.go"}`}},
		},
	}
	summary := ComputeSessionSummary(messages, 15*time.Minute)

	if summary.MessageCount != 3 {
		t.Errorf("expected MessageCount 3, got %d", summary.MessageCount)
	}
	if summary.TotalTokensIn != 3500 {
		t.Errorf("expected TotalTokensIn 3500, got %d", summary.TotalTokensIn)
	}
	if summary.TotalTokensOut != 1750 {
		t.Errorf("expected TotalTokensOut 1750, got %d", summary.TotalTokensOut)
	}
	// Sonnet appears twice, opus once -> primary is sonnet
	if summary.PrimaryModel != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected PrimaryModel sonnet, got %s", summary.PrimaryModel)
	}
	// Two unique files: /a.go and /b.go
	if summary.FileCount != 2 {
		t.Errorf("expected FileCount 2, got %d", summary.FileCount)
	}
	if summary.ToolCounts["Read"] != 2 {
		t.Errorf("expected Read count 2, got %d", summary.ToolCounts["Read"])
	}
	if summary.ToolCounts["Edit"] != 1 {
		t.Errorf("expected Edit count 1, got %d", summary.ToolCounts["Edit"])
	}
}

func TestComputeSessionSummary_CacheRead(t *testing.T) {
	messages := []adapter.Message{
		{
			Model:      "claude-opus-4-5-20251101",
			TokenUsage: adapter.TokenUsage{InputTokens: 10000, OutputTokens: 1000, CacheRead: 8000},
		},
	}
	summary := ComputeSessionSummary(messages, 5*time.Minute)

	if summary.TotalCacheRead != 8000 {
		t.Errorf("expected TotalCacheRead 8000, got %d", summary.TotalCacheRead)
	}
	// Cost should be reduced due to cache
	if summary.TotalCost <= 0 {
		t.Error("expected positive cost")
	}
}

func TestEstimateTotalCost_Opus(t *testing.T) {
	// Opus: $15/M in, $75/M out
	cost := estimateTotalCost("claude-opus-4-5-20251101", 1_000_000, 1_000_000, 0)
	// Expected: 15 + 75 = 90
	if cost < 89 || cost > 91 {
		t.Errorf("expected cost ~90, got %f", cost)
	}
}

func TestEstimateTotalCost_Sonnet(t *testing.T) {
	// Sonnet: $3/M in, $15/M out
	cost := estimateTotalCost("claude-sonnet-4-5-20250929", 1_000_000, 1_000_000, 0)
	// Expected: 3 + 15 = 18
	if cost < 17 || cost > 19 {
		t.Errorf("expected cost ~18, got %f", cost)
	}
}

func TestEstimateTotalCost_Haiku(t *testing.T) {
	// Haiku: $0.25/M in, $1.25/M out
	cost := estimateTotalCost("claude-3-5-haiku-latest", 1_000_000, 1_000_000, 0)
	// Expected: 0.25 + 1.25 = 1.5
	if cost < 1.4 || cost > 1.6 {
		t.Errorf("expected cost ~1.5, got %f", cost)
	}
}

func TestEstimateTotalCost_WithCache(t *testing.T) {
	// Opus with 80% cache hit
	// 1M input, 800k from cache (10% rate), 200k regular
	cost := estimateTotalCost("claude-opus-4-5-20251101", 1_000_000, 0, 800_000)
	// Cache: 800k * 15 * 0.1 / 1M = 1.2
	// Regular: 200k * 15 / 1M = 3
	// Total: 4.2
	if cost < 4 || cost > 4.5 {
		t.Errorf("expected cost ~4.2, got %f", cost)
	}
}

func TestEstimateTotalCost_ZeroTokens(t *testing.T) {
	cost := estimateTotalCost("claude-opus-4-5-20251101", 0, 0, 0)
	if cost != 0 {
		t.Errorf("expected cost 0, got %f", cost)
	}
}

func TestGroupSessionsByTime_Empty(t *testing.T) {
	groups := groupSessionsByTimeAt(nil, testNow())
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupSessionsByTime_Today(t *testing.T) {
	now := testNow()
	sessions := []adapter.Session{
		{ID: "1", UpdatedAt: now},
		{ID: "2", UpdatedAt: now.Add(-1 * time.Hour)},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Label != "Today" {
		t.Errorf("expected Today label, got %s", groups[0].Label)
	}
	if len(groups[0].Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(groups[0].Sessions))
	}
}

func TestGroupSessionsByTime_Yesterday(t *testing.T) {
	now := testNow()
	yesterday := now.AddDate(0, 0, -1)
	sessions := []adapter.Session{
		{ID: "1", UpdatedAt: yesterday},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Label != "Yesterday" {
		t.Errorf("expected Yesterday label, got %s", groups[0].Label)
	}
}

func TestGroupSessionsByTime_ThisWeek(t *testing.T) {
	now := testNow()
	threeDaysAgo := now.AddDate(0, 0, -3)
	sessions := []adapter.Session{
		{ID: "1", UpdatedAt: threeDaysAgo},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Label != "This Week" {
		t.Errorf("expected 'This Week' label, got %s", groups[0].Label)
	}
}

func TestGroupSessionsByTime_Older(t *testing.T) {
	now := testNow()
	twoWeeksAgo := now.AddDate(0, 0, -14)
	sessions := []adapter.Session{
		{ID: "1", UpdatedAt: twoWeeksAgo},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Label != "Older" {
		t.Errorf("expected Older label, got %s", groups[0].Label)
	}
}

func TestGroupSessionsByTime_AllBuckets(t *testing.T) {
	now := testNow()
	sessions := []adapter.Session{
		{ID: "today", UpdatedAt: now},
		{ID: "yesterday", UpdatedAt: now.AddDate(0, 0, -1)},
		{ID: "thisweek", UpdatedAt: now.AddDate(0, 0, -3)},
		{ID: "older", UpdatedAt: now.AddDate(0, 0, -14)},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}

	// Verify order: Today, Yesterday, This Week, Older
	expectedLabels := []string{"Today", "Yesterday", "This Week", "Older"}
	for i, label := range expectedLabels {
		if groups[i].Label != label {
			t.Errorf("expected group %d to be %s, got %s", i, label, groups[i].Label)
		}
	}
}

func TestGroupSessionsByTime_EmptyGroups(t *testing.T) {
	now := testNow()
	// Only today and older, skip yesterday and this week
	sessions := []adapter.Session{
		{ID: "today", UpdatedAt: now},
		{ID: "older", UpdatedAt: now.AddDate(0, 0, -30)},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Label != "Today" {
		t.Errorf("expected first group Today, got %s", groups[0].Label)
	}
	if groups[1].Label != "Older" {
		t.Errorf("expected second group Older, got %s", groups[1].Label)
	}
}

func TestGroupSessionsByTime_GroupSummaryPopulated(t *testing.T) {
	now := testNow()
	sessions := []adapter.Session{
		{ID: "1", UpdatedAt: now, TotalTokens: 1000, EstCost: 0.50},
		{ID: "2", UpdatedAt: now, TotalTokens: 2000, EstCost: 1.00},
		{ID: "3", UpdatedAt: now.AddDate(0, 0, -1), TotalTokens: 500, EstCost: 0.25},
	}

	groups := groupSessionsByTimeAt(sessions, now)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// Today group should have 2 sessions with aggregated tokens/cost
	today := groups[0]
	if today.Label != "Today" {
		t.Errorf("expected Today, got %s", today.Label)
	}
	if today.Summary.SessionCount != 2 {
		t.Errorf("expected 2 sessions, got %d", today.Summary.SessionCount)
	}
	if today.Summary.TotalTokens != 3000 {
		t.Errorf("expected 3000 tokens, got %d", today.Summary.TotalTokens)
	}
	if today.Summary.TotalCost < 1.49 || today.Summary.TotalCost > 1.51 {
		t.Errorf("expected cost ~1.50, got %f", today.Summary.TotalCost)
	}

	// Yesterday group should have 1 session
	yesterday := groups[1]
	if yesterday.Label != "Yesterday" {
		t.Errorf("expected Yesterday, got %s", yesterday.Label)
	}
	if yesterday.Summary.SessionCount != 1 {
		t.Errorf("expected 1 session, got %d", yesterday.Summary.SessionCount)
	}
	if yesterday.Summary.TotalTokens != 500 {
		t.Errorf("expected 500 tokens, got %d", yesterday.Summary.TotalTokens)
	}
	if yesterday.Summary.TotalCost < 0.24 || yesterday.Summary.TotalCost > 0.26 {
		t.Errorf("expected cost ~0.25, got %f", yesterday.Summary.TotalCost)
	}
}

func testNow() time.Time {
	return time.Date(2024, 2, 15, 12, 0, 0, 0, time.Local)
}

func TestUpdateSessionSummary_Basic(t *testing.T) {
	// Start with existing summary
	summary := &SessionSummary{
		MessageCount:   2,
		TotalTokensIn:  1000,
		TotalTokensOut: 500,
		TotalCacheRead: 100,
		PrimaryModel:   "claude-sonnet-4-5-20250929",
		ToolCounts:     map[string]int{"Read": 1},
		FilesTouched:   []string{"/a.go"},
		FileCount:      1,
	}
	modelCounts := map[string]int{"claude-sonnet-4-5-20250929": 2}
	fileSet := map[string]bool{"/a.go": true}

	newMessages := []adapter.Message{
		{
			Model:      "claude-sonnet-4-5-20250929",
			TokenUsage: adapter.TokenUsage{InputTokens: 500, OutputTokens: 250, CacheRead: 50},
			ToolUses:   []adapter.ToolUse{{Name: "Edit", Input: `{"file_path": "/b.go"}`}},
		},
	}

	UpdateSessionSummary(summary, newMessages, modelCounts, fileSet)

	if summary.MessageCount != 3 {
		t.Errorf("expected MessageCount 3, got %d", summary.MessageCount)
	}
	if summary.TotalTokensIn != 1500 {
		t.Errorf("expected TotalTokensIn 1500, got %d", summary.TotalTokensIn)
	}
	if summary.TotalTokensOut != 750 {
		t.Errorf("expected TotalTokensOut 750, got %d", summary.TotalTokensOut)
	}
	if summary.TotalCacheRead != 150 {
		t.Errorf("expected TotalCacheRead 150, got %d", summary.TotalCacheRead)
	}
	if summary.FileCount != 2 {
		t.Errorf("expected FileCount 2, got %d", summary.FileCount)
	}
	if summary.ToolCounts["Edit"] != 1 {
		t.Errorf("expected Edit count 1, got %d", summary.ToolCounts["Edit"])
	}
}

func TestUpdateSessionSummary_DuplicateFile(t *testing.T) {
	summary := &SessionSummary{
		ToolCounts:   map[string]int{"Read": 1},
		FilesTouched: []string{"/a.go"},
		FileCount:    1,
	}
	fileSet := map[string]bool{"/a.go": true}

	newMessages := []adapter.Message{
		{ToolUses: []adapter.ToolUse{{Name: "Edit", Input: `{"file_path": "/a.go"}`}}},
	}

	UpdateSessionSummary(summary, newMessages, nil, fileSet)

	// File should not be duplicated
	if summary.FileCount != 1 {
		t.Errorf("expected FileCount 1 (no duplicate), got %d", summary.FileCount)
	}
	if len(summary.FilesTouched) != 1 {
		t.Errorf("expected 1 file, got %d", len(summary.FilesTouched))
	}
}

func TestUpdateSessionSummary_ModelChange(t *testing.T) {
	summary := &SessionSummary{
		MessageCount: 2,
		PrimaryModel: "claude-sonnet-4-5-20250929",
		ToolCounts:   make(map[string]int),
	}
	modelCounts := map[string]int{"claude-sonnet-4-5-20250929": 2}

	// Add 3 opus messages to change primary model
	newMessages := []adapter.Message{
		{Model: "claude-opus-4-5-20251101"},
		{Model: "claude-opus-4-5-20251101"},
		{Model: "claude-opus-4-5-20251101"},
	}

	UpdateSessionSummary(summary, newMessages, modelCounts, nil)

	// Opus should now be primary (3 > 2)
	if summary.PrimaryModel != "claude-opus-4-5-20251101" {
		t.Errorf("expected PrimaryModel opus, got %s", summary.PrimaryModel)
	}
	if summary.MessageCount != 5 {
		t.Errorf("expected MessageCount 5, got %d", summary.MessageCount)
	}
}

func TestUpdateSessionSummary_NilSummary(t *testing.T) {
	// Should not panic on nil summary
	UpdateSessionSummary(nil, []adapter.Message{{Model: "test"}}, nil, nil)
}

func TestUpdateSessionSummary_EmptyMessages(t *testing.T) {
	summary := &SessionSummary{
		MessageCount: 5,
		ToolCounts:   make(map[string]int),
	}

	UpdateSessionSummary(summary, nil, nil, nil)

	// Should remain unchanged
	if summary.MessageCount != 5 {
		t.Errorf("expected MessageCount 5, got %d", summary.MessageCount)
	}
}

func TestUpdateSessionSummary_Equivalent(t *testing.T) {
	// Verify incremental update produces same result as full computation
	messages := []adapter.Message{
		{
			Model:      "claude-sonnet-4-5-20250929",
			TokenUsage: adapter.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			ToolUses:   []adapter.ToolUse{{Name: "Read", Input: `{"file_path": "/a.go"}`}},
		},
		{
			Model:      "claude-sonnet-4-5-20250929",
			TokenUsage: adapter.TokenUsage{InputTokens: 2000, OutputTokens: 1000, CacheRead: 500},
			ToolUses:   []adapter.ToolUse{{Name: "Edit", Input: `{"file_path": "/a.go"}`}},
		},
		{
			Model:      "claude-opus-4-5-20251101",
			TokenUsage: adapter.TokenUsage{InputTokens: 500, OutputTokens: 250},
			ToolUses:   []adapter.ToolUse{{Name: "Read", Input: `{"file_path": "/b.go"}`}},
		},
	}

	// Full computation
	fullSummary := ComputeSessionSummary(messages, 10*time.Minute)

	// Incremental: compute first 2, then update with 3rd
	incSummary := ComputeSessionSummary(messages[:2], 10*time.Minute)
	modelCounts := map[string]int{"claude-sonnet-4-5-20250929": 2}
	fileSet := map[string]bool{"/a.go": true}
	UpdateSessionSummary(&incSummary, messages[2:], modelCounts, fileSet)

	// Compare
	if fullSummary.MessageCount != incSummary.MessageCount {
		t.Errorf("MessageCount: full=%d, inc=%d", fullSummary.MessageCount, incSummary.MessageCount)
	}
	if fullSummary.TotalTokensIn != incSummary.TotalTokensIn {
		t.Errorf("TotalTokensIn: full=%d, inc=%d", fullSummary.TotalTokensIn, incSummary.TotalTokensIn)
	}
	if fullSummary.TotalTokensOut != incSummary.TotalTokensOut {
		t.Errorf("TotalTokensOut: full=%d, inc=%d", fullSummary.TotalTokensOut, incSummary.TotalTokensOut)
	}
	if fullSummary.TotalCacheRead != incSummary.TotalCacheRead {
		t.Errorf("TotalCacheRead: full=%d, inc=%d", fullSummary.TotalCacheRead, incSummary.TotalCacheRead)
	}
	if fullSummary.FileCount != incSummary.FileCount {
		t.Errorf("FileCount: full=%d, inc=%d", fullSummary.FileCount, incSummary.FileCount)
	}
	if fullSummary.PrimaryModel != incSummary.PrimaryModel {
		t.Errorf("PrimaryModel: full=%s, inc=%s", fullSummary.PrimaryModel, incSummary.PrimaryModel)
	}
}

package conversations

import (
	"sort"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

// SessionSummary holds aggregated statistics for a session.
type SessionSummary struct {
	FilesTouched   []string       // Unique files from tool uses
	FileCount      int            // Number of unique files
	TotalTokensIn  int            // Sum of input tokens
	TotalTokensOut int            // Sum of output tokens
	TotalCacheRead int            // Sum of cache read tokens
	TotalCost      float64        // Estimated cost in dollars
	Duration       time.Duration  // Session duration
	PrimaryModel   string         // Most used model
	MessageCount   int            // Total messages
	ToolCounts     map[string]int // Tool name -> count
}

// ComputeSessionSummary aggregates statistics from messages.
func ComputeSessionSummary(messages []adapter.Message, duration time.Duration) SessionSummary {
	summary := SessionSummary{
		Duration:   duration,
		ToolCounts: make(map[string]int),
	}

	fileSet := make(map[string]bool)
	modelCounts := make(map[string]int)

	for _, msg := range messages {
		summary.MessageCount++
		summary.TotalTokensIn += msg.InputTokens
		summary.TotalTokensOut += msg.OutputTokens
		summary.TotalCacheRead += msg.CacheRead

		if msg.Model != "" {
			modelCounts[msg.Model]++
		}

		for _, tu := range msg.ToolUses {
			summary.ToolCounts[tu.Name]++
			if fp := extractFilePath(tu.Input); fp != "" {
				fileSet[fp] = true
			}
		}
	}

	// Collect unique files
	for fp := range fileSet {
		summary.FilesTouched = append(summary.FilesTouched, fp)
	}
	sort.Strings(summary.FilesTouched)
	summary.FileCount = len(summary.FilesTouched)

	// Determine primary model
	var maxCount int
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			summary.PrimaryModel = model
		}
	}

	// Calculate cost
	summary.TotalCost = estimateTotalCost(
		summary.PrimaryModel,
		summary.TotalTokensIn,
		summary.TotalTokensOut,
		summary.TotalCacheRead,
	)

	return summary
}

// UpdateSessionSummary incrementally updates an existing summary with new messages.
// It avoids re-scanning all messages by only processing the new ones.
// Note: primary model may change if new messages use a different model more frequently.
func UpdateSessionSummary(summary *SessionSummary, newMessages []adapter.Message, modelCounts map[string]int, fileSet map[string]bool) {
	if summary == nil || len(newMessages) == 0 {
		return
	}

	// Initialize maps if not provided (for first-time callers)
	if modelCounts == nil {
		modelCounts = make(map[string]int)
	}
	if fileSet == nil {
		fileSet = make(map[string]bool)
		for _, fp := range summary.FilesTouched {
			fileSet[fp] = true
		}
	}

	for _, msg := range newMessages {
		summary.MessageCount++
		summary.TotalTokensIn += msg.InputTokens
		summary.TotalTokensOut += msg.OutputTokens
		summary.TotalCacheRead += msg.CacheRead

		if msg.Model != "" {
			modelCounts[msg.Model]++
		}

		for _, tu := range msg.ToolUses {
			summary.ToolCounts[tu.Name]++
			if fp := extractFilePath(tu.Input); fp != "" {
				if !fileSet[fp] {
					fileSet[fp] = true
					summary.FilesTouched = append(summary.FilesTouched, fp)
				}
			}
		}
	}

	// Update primary model if needed
	var maxCount int
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			summary.PrimaryModel = model
		}
	}

	summary.FileCount = len(summary.FilesTouched)

	// Recalculate cost
	summary.TotalCost = estimateTotalCost(
		summary.PrimaryModel,
		summary.TotalTokensIn,
		summary.TotalTokensOut,
		summary.TotalCacheRead,
	)
}

// estimateTotalCost calculates cost based on model and tokens.
func estimateTotalCost(model string, inputTokens, outputTokens, cacheRead int) float64 {
	var inRate, outRate float64
	switch {
	case strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o") || strings.Contains(model, "codex"):
		return 0
	case strings.Contains(model, "opus"):
		inRate, outRate = 15.0, 75.0
	case strings.Contains(model, "sonnet"):
		inRate, outRate = 3.0, 15.0
	case strings.Contains(model, "haiku"):
		inRate, outRate = 0.25, 1.25
	default:
		inRate, outRate = 3.0, 15.0
	}

	regularIn := inputTokens - cacheRead
	if regularIn < 0 {
		regularIn = 0
	}
	cacheInCost := float64(cacheRead) * inRate * 0.1 / 1_000_000
	regularInCost := float64(regularIn) * inRate / 1_000_000
	outCost := float64(outputTokens) * outRate / 1_000_000

	return cacheInCost + regularInCost + outCost
}

// SessionGroup represents a group of sessions by time period.
type SessionGroup struct {
	Label    string            // "Today", "Yesterday", "This Week", "Older"
	Sessions []adapter.Session // Sessions in this group
	Summary  GroupSummary      // Aggregate stats
}

// GroupSummary holds aggregate stats for a session group.
type GroupSummary struct {
	SessionCount int
	TotalTokens  int
	TotalCost    float64
}

// GroupSessionsByTime organizes sessions into time-based groups.
func GroupSessionsByTime(sessions []adapter.Session) []SessionGroup {
	return groupSessionsByTimeAt(sessions, time.Now())
}

func groupSessionsByTimeAt(sessions []adapter.Session, now time.Time) []SessionGroup {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	weekAgo := today.AddDate(0, 0, -7)

	groups := map[string]*SessionGroup{
		"Today":     {Label: "Today"},
		"Yesterday": {Label: "Yesterday"},
		"This Week": {Label: "This Week"},
		"Older":     {Label: "Older"},
	}

	for _, s := range sessions {
		var group *SessionGroup
		switch {
		case s.UpdatedAt.After(today) || s.UpdatedAt.Equal(today):
			group = groups["Today"]
		case s.UpdatedAt.After(yesterday) || s.UpdatedAt.Equal(yesterday):
			group = groups["Yesterday"]
		case s.UpdatedAt.After(weekAgo):
			group = groups["This Week"]
		default:
			group = groups["Older"]
		}
		group.Sessions = append(group.Sessions, s)
		group.Summary.SessionCount++
		group.Summary.TotalTokens += s.TotalTokens
		group.Summary.TotalCost += s.EstCost
	}

	// Build result in order, skip empty groups
	var result []SessionGroup
	for _, label := range []string{"Today", "Yesterday", "This Week", "Older"} {
		g := groups[label]
		if len(g.Sessions) > 0 {
			result = append(result, *g)
		}
	}

	return result
}

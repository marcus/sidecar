package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/marcus/sidecar/internal/adapter/pricing"
)

// StatsCache represents the aggregated usage stats from stats-cache.json.
type StatsCache struct {
	Version          int                   `json:"version"`
	LastComputedDate string                `json:"lastComputedDate"`
	TotalSessions    int                   `json:"totalSessions"`
	TotalMessages    int                   `json:"totalMessages"`
	FirstSessionDate time.Time             `json:"firstSessionDate"`
	DailyActivity    []DailyActivity       `json:"dailyActivity"`
	DailyModelTokens []DailyModelTokens    `json:"dailyModelTokens"`
	ModelUsage       map[string]ModelUsage `json:"modelUsage"`
	HourCounts       map[string]int        `json:"hourCounts"`
	LongestSession   LongestSession        `json:"longestSession"`
}

// DailyActivity tracks activity for a single day.
type DailyActivity struct {
	Date          string `json:"date"`
	MessageCount  int    `json:"messageCount"`
	SessionCount  int    `json:"sessionCount"`
	ToolCallCount int    `json:"toolCallCount"`
}

// DailyModelTokens tracks token usage by model for a day.
type DailyModelTokens struct {
	Date          string         `json:"date"`
	TokensByModel map[string]int `json:"tokensByModel"`
}

// ModelUsage tracks cumulative token usage for a model.
type ModelUsage struct {
	InputTokens              int `json:"inputTokens"`
	OutputTokens             int `json:"outputTokens"`
	CacheReadInputTokens     int `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
}

// LongestSession tracks info about the longest session.
type LongestSession struct {
	SessionID    string    `json:"sessionId"`
	Duration     int64     `json:"duration"` // milliseconds
	MessageCount int       `json:"messageCount"`
	Timestamp    time.Time `json:"timestamp"`
}

// LoadStatsCache loads and parses ~/.claude/stats-cache.json.
func LoadStatsCache() (*StatsCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(home, ".claude", "stats-cache.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var stats StatsCache
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetRecentActivity returns daily activity for the last n days.
func (s *StatsCache) GetRecentActivity(days int) []DailyActivity {
	if len(s.DailyActivity) <= days {
		return s.DailyActivity
	}
	return s.DailyActivity[len(s.DailyActivity)-days:]
}

// GetPeakHours returns the top n peak hours.
func (s *StatsCache) GetPeakHours(top int) []struct {
	Hour  string
	Count int
} {
	type hourCount struct {
		Hour  string
		Count int
	}

	var hours []hourCount
	for h, c := range s.HourCounts {
		hours = append(hours, hourCount{h, c})
	}

	// Sort by count descending, then by hour ascending for stable ordering
	sort.Slice(hours, func(i, j int) bool {
		if hours[i].Count != hours[j].Count {
			return hours[i].Count > hours[j].Count // descending by count
		}
		return hours[i].Hour < hours[j].Hour // ascending by hour for ties
	})

	if len(hours) > top {
		hours = hours[:top]
	}

	result := make([]struct {
		Hour  string
		Count int
	}, len(hours))
	for i, h := range hours {
		result[i].Hour = h.Hour
		result[i].Count = h.Count
	}
	return result
}

// CacheEfficiency calculates the percentage of tokens served from cache.
func (s *StatsCache) CacheEfficiency() float64 {
	var totalIn, cacheRead int64
	for _, usage := range s.ModelUsage {
		totalIn += int64(usage.InputTokens) + int64(usage.CacheReadInputTokens)
		cacheRead += int64(usage.CacheReadInputTokens)
	}
	if totalIn == 0 {
		return 0
	}
	return float64(cacheRead) / float64(totalIn) * 100
}

// TotalCost estimates the total cost across all models.
func (s *StatsCache) TotalCost() float64 {
	var total float64
	for model, usage := range s.ModelUsage {
		total += CalculateModelCost(model, usage)
	}
	return total
}

// CalculateModelCost calculates cost for a specific model's usage.
func CalculateModelCost(model string, usage ModelUsage) float64 {
	return pricing.ModelCost(model, pricing.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheRead:    usage.CacheReadInputTokens,
		CacheWrite:   usage.CacheCreationInputTokens,
	})
}

package projects

import (
	"github.com/marcus/td/pkg/monitor"
)

// refreshDataMsg carries refreshed project summaries.
type refreshDataMsg struct {
	entries []ProjectEntry
}

// scanResultMsg carries results from a directory scan.
type scanResultMsg struct {
	results []ScanResult
}

// tickMsg triggers a periodic data refresh.
type tickMsg struct{}

// ScanResult represents a discovered td-initialized project.
type ScanResult struct {
	Name string
	Path string
}

// ProjectEntry represents a project with its td stats.
type ProjectEntry struct {
	Name    string
	Path    string
	Summary monitor.ProjectSummary
	HasTD   bool
	Index   int // 1-based display number

	// AI session stats (from conversation adapters)
	SessionCount int
	TotalTokens  int
	EstCost      float64
}

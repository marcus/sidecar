package integration

import (
	"fmt"
	"strings"
)

// Mapper defines the interface for mapping between td issues and external issues.
type Mapper interface {
	// SyncLabelPrefix returns the prefix for sync indicator labels (e.g., "gh:#", "jira:").
	SyncLabelPrefix() string

	// SyncLabel returns a sync indicator label for a given external ID.
	SyncLabel(externalID string) string

	// IsInternalLabel returns true for labels managed by the sync system.
	IsInternalLabel(label string) bool

	// TDToExternal converts a td issue to an ExternalIssue.
	TDToExternal(td TDIssue) ExternalIssue

	// ExternalToTD converts an ExternalIssue to td fields.
	ExternalToTD(ext ExternalIssue) TDIssue
}

// GitHubMapper implements Mapper for GitHub Issues.
type GitHubMapper struct{}

// Status mapping: td has open/in_progress/blocked/closed, GH has open/closed.
const (
	ghStateOpen   = "open"
	ghStateClosed = "closed"
)

// Label prefixes used for mapping td fields to GH labels.
const (
	priorityLabelPrefix = "priority:"
	ghLabelPrefix       = "gh:#"
	jiraLabelPrefix     = "jira:"
)

func (m *GitHubMapper) SyncLabelPrefix() string { return ghLabelPrefix }

func (m *GitHubMapper) SyncLabel(externalID string) string {
	return ghLabelPrefix + externalID
}

func (m *GitHubMapper) IsInternalLabel(label string) bool {
	return isInternalLabel(label)
}

func (m *GitHubMapper) TDToExternal(td TDIssue) ExternalIssue {
	return TDToExternal(td)
}

func (m *GitHubMapper) ExternalToTD(ext ExternalIssue) TDIssue {
	return ExternalToTD(ext)
}

// GHSyncLabel returns a sync indicator label like "gh:#42" for a given GH issue number.
func GHSyncLabel(ghNumber int) string {
	return fmt.Sprintf("%s%d", ghLabelPrefix, ghNumber)
}

// containsLabel checks if a label slice contains a specific label.
func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

// Type-to-label mapping.
var typeToLabel = map[string]string{
	"bug":     "bug",
	"feature": "enhancement",
	"task":    "task",
	"chore":   "task",
}

// Label-to-type mapping (reverse of typeToLabel).
var labelToType = map[string]string{
	"bug":         "bug",
	"enhancement": "feature",
	"task":        "task",
}

// syncLabel is an internal label used to track synced issues.
// This label is excluded from bidirectional label sync.
const syncLabel = "td-sync"

// MapStatusToGH converts a td status to a GitHub state.
func MapStatusToGH(tdStatus string) string {
	switch tdStatus {
	case "closed":
		return ghStateClosed
	default:
		// open, in_progress, blocked → all map to GH open
		return ghStateOpen
	}
}

// MapStatusFromGH converts a GitHub state to a td status.
func MapStatusFromGH(ghState string) string {
	switch ghState {
	case ghStateClosed:
		return "closed"
	default:
		return "open"
	}
}

// MapTypeToLabels converts a td type to GitHub labels.
// Returns nil if the type has no label mapping.
func MapTypeToLabels(tdType string) []string {
	if label, ok := typeToLabel[tdType]; ok {
		return []string{label}
	}
	return nil
}

// MapLabelsToType extracts a td type from GitHub labels.
// Returns empty string if no type label is found.
func MapLabelsToType(labels []string) string {
	for _, l := range labels {
		if t, ok := labelToType[l]; ok {
			return t
		}
	}
	return ""
}

// MapPriorityToLabel converts a td priority to a GitHub label.
// Returns empty string for empty priority.
func MapPriorityToLabel(priority string) string {
	if priority == "" {
		return ""
	}
	return priorityLabelPrefix + priority
}

// MapLabelToPriority extracts a td priority from GitHub labels.
// Returns empty string if no priority label is found.
func MapLabelToPriority(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, priorityLabelPrefix) {
			return strings.TrimPrefix(l, priorityLabelPrefix)
		}
	}
	return ""
}

// TDToExternal converts a td issue to an ExternalIssue.
func TDToExternal(td TDIssue) ExternalIssue {
	var labels []string

	// Add type labels
	labels = append(labels, MapTypeToLabels(td.Type)...)

	// Add priority label
	if pl := MapPriorityToLabel(td.Priority); pl != "" {
		labels = append(labels, pl)
	}

	// Add user labels, excluding internal ones
	for _, l := range td.Labels {
		if !isInternalLabel(l) {
			labels = append(labels, l)
		}
	}

	return ExternalIssue{
		Title:     td.Title,
		Body:      td.Description,
		State:     MapStatusToGH(td.Status),
		Labels:    labels,
		UpdatedAt: td.UpdatedAt,
	}
}

// ExternalToTD converts an ExternalIssue to td fields.
func ExternalToTD(ext ExternalIssue) TDIssue {
	// Extract user labels (exclude type, priority, and internal labels)
	var userLabels []string
	for _, l := range ext.Labels {
		if isInternalLabel(l) {
			continue
		}
		if _, isType := labelToType[l]; isType {
			continue
		}
		if strings.HasPrefix(l, priorityLabelPrefix) {
			continue
		}
		userLabels = append(userLabels, l)
	}

	return TDIssue{
		Title:       ext.Title,
		Description: ext.Body,
		Status:      MapStatusFromGH(ext.State),
		Type:        MapLabelsToType(ext.Labels),
		Priority:    MapLabelToPriority(ext.Labels),
		Labels:      userLabels,
		UpdatedAt:   ext.UpdatedAt,
	}
}

// isInternalLabel returns true for labels used internally by the sync system.
func isInternalLabel(label string) bool {
	return label == syncLabel || strings.HasPrefix(label, ghLabelPrefix) || strings.HasPrefix(label, jiraLabelPrefix)
}

package integration

import "strings"

// Status mapping: td has open/in_progress/blocked/closed, GH has open/closed.
const (
	ghStateOpen   = "open"
	ghStateClosed = "closed"
)

// Label prefixes used for mapping td fields to GH labels.
const (
	priorityLabelPrefix = "priority:"
)

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
	return label == syncLabel
}

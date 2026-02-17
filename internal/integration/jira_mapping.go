package integration

import "strings"

// JiraMapper implements Mapper for Jira Cloud.
// Jira uses native type/priority fields rather than label encoding.
type JiraMapper struct{}

func (m *JiraMapper) SyncLabelPrefix() string { return jiraLabelPrefix }

func (m *JiraMapper) SyncLabel(externalID string) string {
	return jiraLabelPrefix + externalID
}

func (m *JiraMapper) IsInternalLabel(label string) bool {
	return isInternalLabel(label)
}

// Jira status category key → td status.
var jiraStatusToTD = map[string]string{
	"new":           "open",
	"indeterminate": "in_progress",
	"done":          "closed",
}

// td status → Jira status category key.
var tdStatusToJira = map[string]string{
	"open":        "new",
	"in_progress": "indeterminate",
	"blocked":     "indeterminate",
	"closed":      "done",
}

// Jira issue type name → td type.
var jiraTypeToTD = map[string]string{
	"bug":   "bug",
	"story": "feature",
	"task":  "task",
	"epic":  "epic",
}

// td type → Jira issue type name.
var tdTypeToJira = map[string]string{
	"bug":     "Bug",
	"feature": "Story",
	"task":    "Task",
	"epic":    "Epic",
	"chore":   "Task",
}

// Jira priority name → td priority.
var jiraPriorityToTD = map[string]string{
	"highest": "p0",
	"high":    "p1",
	"medium":  "p2",
	"low":     "p3",
	"lowest":  "p4",
}

// td priority → Jira priority name.
var tdPriorityToJira = map[string]string{
	"p0": "Highest",
	"p1": "High",
	"p2": "Medium",
	"p3": "Low",
	"p4": "Lowest",
}

// MapJiraStatusToTD converts a Jira status category key to a td status.
func MapJiraStatusToTD(statusCategoryKey string) string {
	if s, ok := jiraStatusToTD[strings.ToLower(statusCategoryKey)]; ok {
		return s
	}
	return "open"
}

// MapTDStatusToJira converts a td status to a Jira status category key.
func MapTDStatusToJira(tdStatus string) string {
	if s, ok := tdStatusToJira[tdStatus]; ok {
		return s
	}
	return "new"
}

// MapJiraTypeToTD converts a Jira issue type name to a td type.
func MapJiraTypeToTD(jiraType string) string {
	if t, ok := jiraTypeToTD[strings.ToLower(jiraType)]; ok {
		return t
	}
	return "task"
}

// MapTDTypeToJira converts a td type to a Jira issue type name.
func MapTDTypeToJira(tdType string) string {
	if t, ok := tdTypeToJira[tdType]; ok {
		return t
	}
	return "Task"
}

// MapJiraPriorityToTD converts a Jira priority name to a td priority.
func MapJiraPriorityToTD(jiraPriority string) string {
	if p, ok := jiraPriorityToTD[strings.ToLower(jiraPriority)]; ok {
		return p
	}
	return "p2"
}

// MapTDPriorityToJira converts a td priority to a Jira priority name.
func MapTDPriorityToJira(tdPriority string) string {
	if p, ok := tdPriorityToJira[tdPriority]; ok {
		return p
	}
	return "Medium"
}

func (m *JiraMapper) TDToExternal(td TDIssue) ExternalIssue {
	// Determine state from td status
	state := "open"
	if td.Status == "closed" {
		state = "closed"
	}

	// Filter out internal labels
	var labels []string
	for _, l := range td.Labels {
		if !isInternalLabel(l) {
			labels = append(labels, l)
		}
	}

	return ExternalIssue{
		Title:    td.Title,
		Body:     td.Description,
		State:    state,
		Labels:   labels,
		Type:     MapTDTypeToJira(td.Type),
		Priority: MapTDPriorityToJira(td.Priority),
	}
}

func (m *JiraMapper) ExternalToTD(ext ExternalIssue) TDIssue {
	// Filter out internal labels
	var userLabels []string
	for _, l := range ext.Labels {
		if !isInternalLabel(l) {
			userLabels = append(userLabels, l)
		}
	}

	return TDIssue{
		Title:       ext.Title,
		Description: ext.Body,
		Status:      MapJiraStatusToTD(ext.State),
		Type:        MapJiraTypeToTD(ext.Type),
		Priority:    MapJiraPriorityToTD(ext.Priority),
		Labels:      userLabels,
	}
}

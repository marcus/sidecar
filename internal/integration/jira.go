package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// JiraProvider implements Provider for Jira Cloud using the REST API v3.
type JiraProvider struct {
	baseURL    string
	projectKey string
	email      string
	apiToken   string
	client     *http.Client
}

// JiraProviderOptions configures a JiraProvider.
type JiraProviderOptions struct {
	URL        string
	ProjectKey string
	Email      string
	APIToken   string
}

// NewJiraProvider creates a new Jira Cloud provider.
// Environment variables JIRA_URL, JIRA_EMAIL, JIRA_API_TOKEN, and JIRA_PROJECT
// override the options when set.
func NewJiraProvider(opts JiraProviderOptions) *JiraProvider {
	p := &JiraProvider{
		baseURL:    opts.URL,
		projectKey: opts.ProjectKey,
		email:      opts.Email,
		apiToken:   opts.APIToken,
		client:     &http.Client{Timeout: 30 * time.Second},
	}

	// Environment variables override config values
	if v := os.Getenv("JIRA_URL"); v != "" {
		p.baseURL = v
	}
	if v := os.Getenv("JIRA_EMAIL"); v != "" {
		p.email = v
	}
	if v := os.Getenv("JIRA_API_TOKEN"); v != "" {
		p.apiToken = v
	}
	if v := os.Getenv("JIRA_PROJECT"); v != "" {
		p.projectKey = v
	}

	// Strip trailing slash from URL
	p.baseURL = strings.TrimRight(p.baseURL, "/")

	return p
}

func (j *JiraProvider) ID() string     { return "jira" }
func (j *JiraProvider) Name() string   { return "Jira" }
func (j *JiraProvider) Mapper() Mapper { return &JiraMapper{} }

// Available checks if Jira credentials are configured and connectivity works.
func (j *JiraProvider) Available(workDir string) (bool, error) {
	if j.baseURL == "" {
		return false, fmt.Errorf("Jira URL not configured")
	}
	if j.apiToken == "" {
		return false, fmt.Errorf("Jira API token not configured")
	}
	if j.email == "" {
		return false, fmt.Errorf("Jira email not configured")
	}

	// Verify connectivity
	req, err := http.NewRequest("GET", j.baseURL+"/rest/api/3/myself", nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	j.setAuth(req)

	resp, err := j.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("connect to Jira: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Jira auth failed (HTTP %d)", resp.StatusCode)
	}

	return true, nil
}

// jiraSearchResponse is the response from Jira's search/jql API.
type jiraSearchResponse struct {
	Issues        []jiraIssue `json:"issues"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

// jiraIssue represents a Jira issue from the REST API.
type jiraIssue struct {
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary   string          `json:"summary"`
	Desc      json.RawMessage `json:"description"` // ADF format
	Status    jiraStatus      `json:"status"`
	IssueType jiraIssueType   `json:"issuetype"`
	Priority  jiraPriority    `json:"priority"`
	Labels    []string        `json:"labels"`
	Created   string          `json:"created"`
	Updated   string          `json:"updated"`
}

type jiraStatus struct {
	Name           string             `json:"name"`
	StatusCategory jiraStatusCategory `json:"statusCategory"`
}

type jiraStatusCategory struct {
	Key string `json:"key"` // "new", "indeterminate", "done"
}

type jiraIssueType struct {
	Name string `json:"name"`
}

type jiraPriority struct {
	Name string `json:"name"`
}

// List returns all issues from the configured Jira project.
// Uses the /rest/api/3/search/jql endpoint (the old /rest/api/3/search was removed in 2025).
func (j *JiraProvider) List(ctx context.Context, workDir string) ([]ExternalIssue, error) {
	jql := fmt.Sprintf("project=%s ORDER BY updated DESC", j.projectKey)
	fields := "summary,description,status,issuetype,priority,labels,created,updated"

	var result []ExternalIssue
	var nextPageToken string

	for {
		u := fmt.Sprintf("%s/rest/api/3/search/jql?jql=%s&maxResults=200&fields=%s",
			j.baseURL, url.QueryEscape(jql), url.QueryEscape(fields))
		if nextPageToken != "" {
			u += "&nextPageToken=" + url.QueryEscape(nextPageToken)
		}

		body, err := j.doRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("search issues: %w", err)
		}

		var resp jiraSearchResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parse search response: %w", err)
		}

		for _, issue := range resp.Issues {
			created, _ := parseJiraTime(issue.Fields.Created)
			updated, _ := parseJiraTime(issue.Fields.Updated)

			ext := ExternalIssue{
				ID:        issue.Key,
				Title:     issue.Fields.Summary,
				Body:      adfToText(issue.Fields.Desc),
				State:     issue.Fields.Status.StatusCategory.Key,
				Labels:    issue.Fields.Labels,
				Type:      issue.Fields.IssueType.Name,
				Priority:  issue.Fields.Priority.Name,
				CreatedAt: created,
				UpdatedAt: updated,
			}
			result = append(result, ext)
		}

		if resp.NextPageToken == "" {
			break
		}
		nextPageToken = resp.NextPageToken
	}

	return result, nil
}

// Create creates a new Jira issue and returns its key.
func (j *JiraProvider) Create(ctx context.Context, workDir string, issue ExternalIssue) (string, error) {
	payload := map[string]interface{}{
		"fields": map[string]interface{}{
			"project":     map[string]string{"key": j.projectKey},
			"summary":     issue.Title,
			"description": textToADF(issue.Body),
			"issuetype":   map[string]string{"name": issueTypeOrDefault(issue.Type)},
			"priority":    map[string]string{"name": priorityOrDefault(issue.Priority)},
		},
	}

	// Add labels if any
	if len(issue.Labels) > 0 {
		fields := payload["fields"].(map[string]interface{})
		fields["labels"] = issue.Labels
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal issue: %w", err)
	}

	body, err := j.doRequest(ctx, "POST", j.baseURL+"/rest/api/3/issue", data)
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}

	var created struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("parse create response: %w", err)
	}

	return created.Key, nil
}

// Update updates an existing Jira issue by key.
func (j *JiraProvider) Update(ctx context.Context, workDir string, id string, issue ExternalIssue) error {
	payload := map[string]interface{}{
		"fields": map[string]interface{}{
			"summary":     issue.Title,
			"description": textToADF(issue.Body),
			"priority":    map[string]string{"name": priorityOrDefault(issue.Priority)},
		},
	}

	// Add labels if any
	if len(issue.Labels) > 0 {
		fields := payload["fields"].(map[string]interface{})
		fields["labels"] = issue.Labels
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal issue: %w", err)
	}

	_, err = j.doRequest(ctx, "PUT", j.baseURL+"/rest/api/3/issue/"+id, data)
	if err != nil {
		return fmt.Errorf("update issue %s: %w", id, err)
	}

	return nil
}

// Close transitions a Jira issue to a "done" status.
func (j *JiraProvider) Close(ctx context.Context, workDir string, id string) error {
	return j.transition(ctx, id, "done")
}

// Reopen transitions a Jira issue to a "new" (to-do) status.
func (j *JiraProvider) Reopen(ctx context.Context, workDir string, id string) error {
	return j.transition(ctx, id, "new")
}

// jiraTransitionsResponse is the response from the transitions endpoint.
type jiraTransitionsResponse struct {
	Transitions []jiraTransition `json:"transitions"`
}

type jiraTransition struct {
	ID string              `json:"id"`
	To jiraTransitionTarget `json:"to"`
}

type jiraTransitionTarget struct {
	StatusCategory jiraStatusCategory `json:"statusCategory"`
}

// transition moves an issue to a status category (e.g., "done", "new").
func (j *JiraProvider) transition(ctx context.Context, id string, targetCategory string) error {
	url := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", j.baseURL, id)
	body, err := j.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("get transitions for %s: %w", id, err)
	}

	var resp jiraTransitionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse transitions: %w", err)
	}

	// Find transition matching target category
	for _, t := range resp.Transitions {
		if t.To.StatusCategory.Key == targetCategory {
			payload := map[string]interface{}{
				"transition": map[string]string{"id": t.ID},
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal transition: %w", err)
			}
			_, err = j.doRequest(ctx, "POST", url, data)
			if err != nil {
				return fmt.Errorf("execute transition for %s: %w", id, err)
			}
			return nil
		}
	}

	return fmt.Errorf("no transition to %q found for %s", targetCategory, id)
}

// doRequest executes an authenticated HTTP request against the Jira API.
func (j *JiraProvider) doRequest(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	j.setAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract Jira error message
		var jiraErr struct {
			ErrorMessages []string `json:"errorMessages"`
		}
		if json.Unmarshal(respBody, &jiraErr) == nil && len(jiraErr.ErrorMessages) > 0 {
			return nil, fmt.Errorf("Jira API %s %d: %s", method, resp.StatusCode, strings.Join(jiraErr.ErrorMessages, "; "))
		}
		return nil, fmt.Errorf("Jira API %s %d: %s", method, resp.StatusCode, string(respBody))
	}

	// PUT/POST for transitions returns 204 No Content
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	return respBody, nil
}

// setAuth sets Basic Auth header using email:apiToken.
func (j *JiraProvider) setAuth(req *http.Request) {
	creds := j.email + ":" + j.apiToken
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(creds)))
}

// textToADF converts plain text to Atlassian Document Format (ADF).
func textToADF(text string) interface{} {
	if text == "" {
		return map[string]interface{}{
			"version": 1,
			"type":    "doc",
			"content": []interface{}{},
		}
	}

	// Split into paragraphs
	paragraphs := strings.Split(text, "\n\n")
	content := make([]interface{}, 0, len(paragraphs))
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		content = append(content, map[string]interface{}{
			"type": "paragraph",
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": p,
				},
			},
		})
	}

	return map[string]interface{}{
		"version": 1,
		"type":    "doc",
		"content": content,
	}
}

// adfToText extracts plain text from an ADF (Atlassian Document Format) tree.
func adfToText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Not JSON — might be a plain string
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}

	var sb strings.Builder
	extractText(doc, &sb)
	return strings.TrimSpace(sb.String())
}

// extractText recursively walks an ADF node tree and extracts text content.
func extractText(node map[string]interface{}, sb *strings.Builder) {
	if t, ok := node["type"].(string); ok && t == "text" {
		if text, ok := node["text"].(string); ok {
			sb.WriteString(text)
		}
		return
	}

	content, ok := node["content"].([]interface{})
	if !ok {
		return
	}

	for i, child := range content {
		childMap, ok := child.(map[string]interface{})
		if !ok {
			continue
		}
		extractText(childMap, sb)

		// Add paragraph separation
		if t, ok := node["type"].(string); ok && t == "doc" && i < len(content)-1 {
			sb.WriteString("\n\n")
		}
	}
}

// parseJiraTime parses Jira's ISO 8601 timestamp format.
func parseJiraTime(s string) (time.Time, error) {
	// Jira uses format like "2026-01-15T10:30:00.000+0000"
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse Jira time: %q", s)
}

// issueTypeOrDefault returns the issue type name, or "Task" if empty.
func issueTypeOrDefault(t string) string {
	if t == "" {
		return "Task"
	}
	return t
}

// priorityOrDefault returns the priority name, or "Medium" if empty.
func priorityOrDefault(p string) string {
	if p == "" {
		return "Medium"
	}
	return p
}

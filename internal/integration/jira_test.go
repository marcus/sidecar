package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupJiraTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// GET /rest/api/3/myself — auth check
	mux.HandleFunc("/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"accountId":   "abc123",
			"displayName": "Test User",
		})
	})

	// GET /rest/api/3/search/jql — list issues (new endpoint, old /search returns 410)
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := jiraSearchResponse{
			Issues: []jiraIssue{
				{
					Key: "PROJ-1",
					Fields: jiraIssueFields{
						Summary: "Fix login bug",
						Desc:    json.RawMessage(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Users cannot log in"}]}]}`),
						Status: jiraStatus{
							Name: "To Do",
							StatusCategory: jiraStatusCategory{
								Key: "new",
							},
						},
						IssueType: jiraIssueType{Name: "Bug"},
						Priority:  jiraPriority{Name: "High"},
						Labels:    []string{"frontend"},
						Created:   "2026-01-15T10:30:00.000+0000",
						Updated:   "2026-01-16T10:30:00.000+0000",
					},
				},
				{
					Key: "PROJ-2",
					Fields: jiraIssueFields{
						Summary: "Add dark mode",
						Desc:    json.RawMessage(`null`),
						Status: jiraStatus{
							Name: "Done",
							StatusCategory: jiraStatusCategory{
								Key: "done",
							},
						},
						IssueType: jiraIssueType{Name: "Story"},
						Priority:  jiraPriority{Name: "Medium"},
						Labels:    nil,
						Created:   "2026-01-10T08:00:00.000+0000",
						Updated:   "2026-01-20T12:00:00.000+0000",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	// POST /rest/api/3/issue — create issue
	mux.HandleFunc("/rest/api/3/issue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"key":  "PROJ-3",
				"self": "https://test.atlassian.net/rest/api/3/issue/10003",
			})
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	// PUT /rest/api/3/issue/PROJ-1 — update issue
	mux.HandleFunc("/rest/api/3/issue/PROJ-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	// GET/POST /rest/api/3/issue/PROJ-1/transitions — transitions
	mux.HandleFunc("/rest/api/3/issue/PROJ-1/transitions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jiraTransitionsResponse{
				Transitions: []jiraTransition{
					{
						ID: "31",
						To: jiraTransitionTarget{
							StatusCategory: jiraStatusCategory{Key: "done"},
						},
					},
					{
						ID: "11",
						To: jiraTransitionTarget{
							StatusCategory: jiraStatusCategory{Key: "new"},
						},
					},
				},
			})
			return
		}
		if r.Method == "POST" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	return httptest.NewServer(mux)
}

func newTestJiraProvider(serverURL string) *JiraProvider {
	return NewJiraProvider(JiraProviderOptions{
		URL:        serverURL,
		ProjectKey: "PROJ",
		Email:      "test@example.com",
		APIToken:   "test-token",
	})
}

func TestJiraProviderInterface(t *testing.T) {
	var p Provider = &JiraProvider{}
	_ = p
}

func TestJiraProviderIDAndName(t *testing.T) {
	p := &JiraProvider{}
	if p.ID() != "jira" {
		t.Errorf("ID() = %q, want %q", p.ID(), "jira")
	}
	if p.Name() != "Jira" {
		t.Errorf("Name() = %q, want %q", p.Name(), "Jira")
	}
}

func TestJiraProviderMapper(t *testing.T) {
	p := &JiraProvider{}
	m := p.Mapper()
	if _, ok := m.(*JiraMapper); !ok {
		t.Error("Mapper() should return *JiraMapper")
	}
}

func TestJiraProviderAvailable(t *testing.T) {
	server := setupJiraTestServer(t)
	defer server.Close()

	p := newTestJiraProvider(server.URL)
	ok, err := p.Available("")
	if !ok {
		t.Fatalf("Available() = false, %v", err)
	}
}

func TestJiraProviderAvailableMissingConfig(t *testing.T) {
	tests := []struct {
		name string
		opts JiraProviderOptions
	}{
		{"no URL", JiraProviderOptions{Email: "a", APIToken: "b"}},
		{"no email", JiraProviderOptions{URL: "http://x", APIToken: "b"}},
		{"no token", JiraProviderOptions{URL: "http://x", Email: "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewJiraProvider(tt.opts)
			ok, _ := p.Available("")
			if ok {
				t.Error("Available() should be false when config is missing")
			}
		})
	}
}

func TestJiraProviderList(t *testing.T) {
	server := setupJiraTestServer(t)
	defer server.Close()

	p := newTestJiraProvider(server.URL)
	issues, err := p.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}

	// First issue
	if issues[0].ID != "PROJ-1" {
		t.Errorf("issues[0].ID = %q, want %q", issues[0].ID, "PROJ-1")
	}
	if issues[0].Title != "Fix login bug" {
		t.Errorf("issues[0].Title = %q, want %q", issues[0].Title, "Fix login bug")
	}
	if issues[0].Body != "Users cannot log in" {
		t.Errorf("issues[0].Body = %q, want %q", issues[0].Body, "Users cannot log in")
	}
	if issues[0].State != "new" {
		t.Errorf("issues[0].State = %q, want %q", issues[0].State, "new")
	}
	if issues[0].Type != "Bug" {
		t.Errorf("issues[0].Type = %q, want %q", issues[0].Type, "Bug")
	}
	if issues[0].Priority != "High" {
		t.Errorf("issues[0].Priority = %q, want %q", issues[0].Priority, "High")
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "frontend" {
		t.Errorf("issues[0].Labels = %v, want [frontend]", issues[0].Labels)
	}

	// Second issue (closed)
	if issues[1].ID != "PROJ-2" {
		t.Errorf("issues[1].ID = %q, want %q", issues[1].ID, "PROJ-2")
	}
	if issues[1].State != "done" {
		t.Errorf("issues[1].State = %q, want %q", issues[1].State, "done")
	}
	if issues[1].Body != "" {
		t.Errorf("issues[1].Body = %q, want empty (null description)", issues[1].Body)
	}
}

func TestJiraProviderCreate(t *testing.T) {
	server := setupJiraTestServer(t)
	defer server.Close()

	p := newTestJiraProvider(server.URL)
	key, err := p.Create(context.Background(), "", ExternalIssue{
		Title:    "New issue",
		Body:     "Description here",
		Type:     "Task",
		Priority: "Medium",
		Labels:   []string{"backend"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key != "PROJ-3" {
		t.Errorf("created key = %q, want %q", key, "PROJ-3")
	}
}

func TestJiraProviderUpdate(t *testing.T) {
	server := setupJiraTestServer(t)
	defer server.Close()

	p := newTestJiraProvider(server.URL)
	err := p.Update(context.Background(), "", "PROJ-1", ExternalIssue{
		Title:    "Updated title",
		Body:     "Updated body",
		Priority: "High",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestJiraProviderClose(t *testing.T) {
	server := setupJiraTestServer(t)
	defer server.Close()

	p := newTestJiraProvider(server.URL)
	err := p.Close(context.Background(), "", "PROJ-1")
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestJiraProviderReopen(t *testing.T) {
	server := setupJiraTestServer(t)
	defer server.Close()

	p := newTestJiraProvider(server.URL)
	err := p.Reopen(context.Background(), "", "PROJ-1")
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
}

func TestTextToADF(t *testing.T) {
	adf := textToADF("Hello world")
	doc, ok := adf.(map[string]interface{})
	if !ok {
		t.Fatal("textToADF should return a map")
	}
	if doc["type"] != "doc" {
		t.Errorf("type = %v, want 'doc'", doc["type"])
	}
	content := doc["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("content has %d elements, want 1", len(content))
	}
}

func TestTextToADFEmpty(t *testing.T) {
	adf := textToADF("")
	doc, ok := adf.(map[string]interface{})
	if !ok {
		t.Fatal("textToADF should return a map")
	}
	content := doc["content"].([]interface{})
	if len(content) != 0 {
		t.Errorf("empty text should produce empty content, got %d elements", len(content))
	}
}

func TestTextToADFMultipleParagraphs(t *testing.T) {
	adf := textToADF("First paragraph\n\nSecond paragraph")
	doc := adf.(map[string]interface{})
	content := doc["content"].([]interface{})
	if len(content) != 2 {
		t.Errorf("two paragraphs should produce 2 content nodes, got %d", len(content))
	}
}

func TestADFToText(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [
			{
				"type": "paragraph",
				"content": [
					{"type": "text", "text": "Hello "},
					{"type": "text", "text": "world"}
				]
			},
			{
				"type": "paragraph",
				"content": [
					{"type": "text", "text": "Second paragraph"}
				]
			}
		]
	}`)

	got := adfToText(raw)
	if got != "Hello world\n\nSecond paragraph" {
		t.Errorf("adfToText = %q, want %q", got, "Hello world\n\nSecond paragraph")
	}
}

func TestADFToTextNull(t *testing.T) {
	if got := adfToText(json.RawMessage("null")); got != "" {
		t.Errorf("adfToText(null) = %q, want empty", got)
	}
}

func TestADFToTextEmpty(t *testing.T) {
	if got := adfToText(nil); got != "" {
		t.Errorf("adfToText(nil) = %q, want empty", got)
	}
}

func TestParseJiraTime(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"2026-01-15T10:30:00.000+0000", true},
		{"2026-01-15T10:30:00.000Z", true},
		{"2026-01-15T10:30:00Z", true},
		{"invalid", false},
	}
	for _, tt := range tests {
		_, err := parseJiraTime(tt.input)
		if (err == nil) != tt.valid {
			t.Errorf("parseJiraTime(%q) valid=%v, want %v", tt.input, err == nil, tt.valid)
		}
	}
}

func TestNewJiraProviderTrimsTrailingSlash(t *testing.T) {
	p := NewJiraProvider(JiraProviderOptions{
		URL: "https://myteam.atlassian.net/",
	})
	if p.baseURL != "https://myteam.atlassian.net" {
		t.Errorf("baseURL = %q, should have trailing slash trimmed", p.baseURL)
	}
}

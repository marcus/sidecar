package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLoadStateMissing(t *testing.T) {
	dir := t.TempDir()
	state, err := LoadState(dir, "github")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state == nil {
		t.Fatal("state is nil")
	}
	if len(state.Issues) != 0 {
		t.Errorf("expected empty issues map, got %d entries", len(state.Issues))
	}
}

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()

	state := &SyncState{
		ProviderID: "github",
		ProviderMeta: map[string]string{
			"owner": "marcus",
			"repo":  "sidecar",
		},
		Issues: map[string]SyncStateEntry{
			"td-abc123": {
				ExternalID:   "42",
				TDUpdatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				ExtUpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := SaveState(dir, "github", state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify file exists with new naming convention
	path := filepath.Join(dir, "github-sync.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("sync state file not created")
	}

	// Verify JSON is valid and readable
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Load it back
	loaded, err := LoadState(dir, "github")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.ProviderID != "github" {
		t.Errorf("ProviderID = %q, want %q", loaded.ProviderID, "github")
	}
	if loaded.ProviderMeta["owner"] != "marcus" {
		t.Errorf("ProviderMeta[owner] = %q, want %q", loaded.ProviderMeta["owner"], "marcus")
	}
	if loaded.ProviderMeta["repo"] != "sidecar" {
		t.Errorf("ProviderMeta[repo] = %q, want %q", loaded.ProviderMeta["repo"], "sidecar")
	}

	entry, ok := loaded.Issues["td-abc123"]
	if !ok {
		t.Fatal("missing entry for td-abc123")
	}
	if entry.ExternalID != "42" {
		t.Errorf("ExternalID = %q, want %q", entry.ExternalID, "42")
	}
}

func TestFindByTDID(t *testing.T) {
	state := &SyncState{
		Issues: map[string]SyncStateEntry{
			"td-abc": {ExternalID: "1"},
			"td-def": {ExternalID: "2"},
		},
	}

	entry := state.FindByTDID("td-abc")
	if entry == nil {
		t.Fatal("FindByTDID returned nil for existing entry")
	}
	if entry.ExternalID != "1" {
		t.Errorf("ExternalID = %q, want %q", entry.ExternalID, "1")
	}

	if state.FindByTDID("td-missing") != nil {
		t.Error("FindByTDID should return nil for missing entry")
	}
}

func TestFindByExternalID(t *testing.T) {
	state := &SyncState{
		Issues: map[string]SyncStateEntry{
			"td-abc": {ExternalID: "1"},
			"td-def": {ExternalID: "2"},
		},
	}

	tdID, entry := state.FindByExternalID("2")
	if entry == nil {
		t.Fatal("FindByExternalID returned nil for existing entry")
	}
	if tdID != "td-def" {
		t.Errorf("tdID = %q, want %q", tdID, "td-def")
	}

	tdID, entry = state.FindByExternalID("99")
	if entry != nil || tdID != "" {
		t.Error("FindByExternalID should return nil for missing entry")
	}
}

func TestLoadStateCorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "github-sync.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadState(dir, "github")
	if err == nil {
		t.Error("expected error for corrupted JSON")
	}
}

func TestLoadStateNilIssuesMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "github-sync.json")
	// Write valid JSON but with null issues
	data := `{"providerID":"github","issues":null}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	state, err := LoadState(dir, "github")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Issues == nil {
		t.Error("Issues map should be initialized, not nil")
	}
}

func TestLegacyGHStateMigration(t *testing.T) {
	dir := t.TempDir()

	// Write legacy gh-sync.json format
	legacy := `{
		"owner": "marcus",
		"repo": "sidecar",
		"issues": {
			"td-abc123": {
				"ghNumber": 42,
				"tdUpdatedAt": "2026-01-01T00:00:00Z",
				"ghUpdatedAt": "2026-01-02T00:00:00Z"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "gh-sync.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	// Loading github state should trigger migration
	state, err := LoadState(dir, "github")
	if err != nil {
		t.Fatalf("LoadState with migration: %v", err)
	}

	if state.ProviderID != "github" {
		t.Errorf("ProviderID = %q, want %q", state.ProviderID, "github")
	}
	if state.ProviderMeta["owner"] != "marcus" {
		t.Errorf("ProviderMeta[owner] = %q, want %q", state.ProviderMeta["owner"], "marcus")
	}

	entry, ok := state.Issues["td-abc123"]
	if !ok {
		t.Fatal("missing entry for td-abc123")
	}
	if entry.ExternalID != "42" {
		t.Errorf("ExternalID = %q, want %q", entry.ExternalID, "42")
	}

	// Verify new file was written
	if _, err := os.Stat(filepath.Join(dir, "github-sync.json")); os.IsNotExist(err) {
		t.Error("migrated github-sync.json file not created")
	}
}

func TestSyncStateFilenameByProvider(t *testing.T) {
	tests := []struct {
		providerID string
		want       string
	}{
		{"github", "github-sync.json"},
		{"jira", "jira-sync.json"},
	}
	for _, tt := range tests {
		if got := syncStateFilename(tt.providerID); got != tt.want {
			t.Errorf("syncStateFilename(%q) = %q, want %q", tt.providerID, got, tt.want)
		}
	}
}

func TestJiraProviderStateIsolation(t *testing.T) {
	dir := t.TempDir()

	// Save state for jira provider
	jiraState := &SyncState{
		ProviderID: "jira",
		Issues: map[string]SyncStateEntry{
			"td-xyz": {ExternalID: "PROJ-123"},
		},
	}
	if err := SaveState(dir, "jira", jiraState); err != nil {
		t.Fatalf("SaveState jira: %v", err)
	}

	// Save state for github provider
	ghState := &SyncState{
		ProviderID: "github",
		Issues: map[string]SyncStateEntry{
			"td-abc": {ExternalID: "42"},
		},
	}
	if err := SaveState(dir, "github", ghState); err != nil {
		t.Fatalf("SaveState github: %v", err)
	}

	// Load jira state and verify isolation
	loaded, err := LoadState(dir, "jira")
	if err != nil {
		t.Fatalf("LoadState jira: %v", err)
	}
	if len(loaded.Issues) != 1 {
		t.Errorf("jira state has %d issues, want 1", len(loaded.Issues))
	}
	if loaded.Issues["td-xyz"].ExternalID != "PROJ-123" {
		t.Errorf("jira ExternalID = %q, want %q", loaded.Issues["td-xyz"].ExternalID, "PROJ-123")
	}
}

// TestGitHubMapperInterface verifies GitHubMapper satisfies the Mapper interface.
func TestGitHubMapperInterface(t *testing.T) {
	var m Mapper = &GitHubMapper{}
	_ = m
}

func TestGitHubMapperSyncLabel(t *testing.T) {
	m := &GitHubMapper{}
	if got := m.SyncLabelPrefix(); got != "gh:#" {
		t.Errorf("SyncLabelPrefix() = %q, want %q", got, "gh:#")
	}
	if got := m.SyncLabel("42"); got != "gh:#42" {
		t.Errorf("SyncLabel(42) = %q, want %q", got, "gh:#42")
	}
}

func TestGitHubMapperTDToExternal(t *testing.T) {
	m := &GitHubMapper{}
	td := TDIssue{
		Title:    "Test",
		Status:   "open",
		Type:     "bug",
		Priority: "P1",
	}
	ext := m.TDToExternal(td)
	if ext.State != "open" {
		t.Errorf("State = %q, want %q", ext.State, "open")
	}
	// Should have bug and priority:P1 labels
	labelSet := make(map[string]bool)
	for _, l := range ext.Labels {
		labelSet[l] = true
	}
	if !labelSet["bug"] {
		t.Error("missing label 'bug'")
	}
	if !labelSet["priority:P1"] {
		t.Error("missing label 'priority:P1'")
	}
}

func TestGitHubMapperExternalToTD(t *testing.T) {
	m := &GitHubMapper{}
	ext := ExternalIssue{
		Title:  "Test",
		State:  "closed",
		Labels: []string{"bug", "priority:P2"},
	}
	td := m.ExternalToTD(ext)
	if td.Status != "closed" {
		t.Errorf("Status = %q, want %q", td.Status, "closed")
	}
	if td.Type != "bug" {
		t.Errorf("Type = %q, want %q", td.Type, "bug")
	}
	if td.Priority != "P2" {
		t.Errorf("Priority = %q, want %q", td.Priority, "P2")
	}
}

// Keep backward-compat GHSyncLabel tests
func TestGHSyncLabelCompat(t *testing.T) {
	tests := []struct {
		number int
		want   string
	}{
		{1, "gh:#1"},
		{42, "gh:#42"},
	}
	for _, tt := range tests {
		if got := GHSyncLabel(tt.number); got != tt.want {
			t.Errorf("GHSyncLabel(%d) = %q, want %q", tt.number, got, tt.want)
		}
		// Should match mapper.SyncLabel
		m := &GitHubMapper{}
		if got := m.SyncLabel(strconv.Itoa(tt.number)); got != tt.want {
			t.Errorf("GitHubMapper.SyncLabel(%d) = %q, want %q", tt.number, got, tt.want)
		}
	}
}

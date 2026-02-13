package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStateMissing(t *testing.T) {
	dir := t.TempDir()
	state, err := LoadState(dir)
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
		Owner: "marcus",
		Repo:  "sidecar",
		Issues: map[string]SyncStateEntry{
			"td-abc123": {
				GHNumber:    42,
				TDUpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				GHUpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, syncStateFile)
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
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.Owner != "marcus" {
		t.Errorf("Owner = %q, want %q", loaded.Owner, "marcus")
	}
	if loaded.Repo != "sidecar" {
		t.Errorf("Repo = %q, want %q", loaded.Repo, "sidecar")
	}

	entry, ok := loaded.Issues["td-abc123"]
	if !ok {
		t.Fatal("missing entry for td-abc123")
	}
	if entry.GHNumber != 42 {
		t.Errorf("GHNumber = %d, want %d", entry.GHNumber, 42)
	}
}

func TestFindByTDID(t *testing.T) {
	state := &SyncState{
		Issues: map[string]SyncStateEntry{
			"td-abc": {GHNumber: 1},
			"td-def": {GHNumber: 2},
		},
	}

	entry := state.FindByTDID("td-abc")
	if entry == nil {
		t.Fatal("FindByTDID returned nil for existing entry")
	}
	if entry.GHNumber != 1 {
		t.Errorf("GHNumber = %d, want %d", entry.GHNumber, 1)
	}

	if state.FindByTDID("td-missing") != nil {
		t.Error("FindByTDID should return nil for missing entry")
	}
}

func TestFindByGHNumber(t *testing.T) {
	state := &SyncState{
		Issues: map[string]SyncStateEntry{
			"td-abc": {GHNumber: 1},
			"td-def": {GHNumber: 2},
		},
	}

	tdID, entry := state.FindByGHNumber(2)
	if entry == nil {
		t.Fatal("FindByGHNumber returned nil for existing entry")
	}
	if tdID != "td-def" {
		t.Errorf("tdID = %q, want %q", tdID, "td-def")
	}

	tdID, entry = state.FindByGHNumber(99)
	if entry != nil || tdID != "" {
		t.Error("FindByGHNumber should return nil for missing entry")
	}
}

func TestLoadStateCorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, syncStateFile)
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadState(dir)
	if err == nil {
		t.Error("expected error for corrupted JSON")
	}
}

func TestLoadStateNilIssuesMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, syncStateFile)
	// Write valid JSON but with null issues
	data := `{"owner":"test","repo":"test","issues":null}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	state, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Issues == nil {
		t.Error("Issues map should be initialized, not nil")
	}
}

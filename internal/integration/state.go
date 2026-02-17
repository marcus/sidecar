package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// legacySyncStateFile is the old filename used before provider-agnostic refactoring.
const legacySyncStateFile = "gh-sync.json"

// syncStateFilename returns the sync state filename for a given provider ID.
func syncStateFilename(providerID string) string {
	return providerID + "-sync.json"
}

// SyncState tracks the mapping between td issues and external issues.
type SyncState struct {
	ProviderID   string                    `json:"providerID"`
	ProviderMeta map[string]string         `json:"providerMeta,omitempty"`
	Issues       map[string]SyncStateEntry `json:"issues"` // keyed by td issue ID
}

// SyncStateEntry tracks a single synced issue pair.
type SyncStateEntry struct {
	ExternalID  string    `json:"externalID"`
	TDUpdatedAt time.Time `json:"tdUpdatedAt"`
	ExtUpdatedAt time.Time `json:"extUpdatedAt"`
}

// LoadState loads the sync state for a provider from the todos directory.
// Returns an empty state if the file doesn't exist.
// For the "github" provider, migrates from legacy gh-sync.json if needed.
func LoadState(todosDir string, providerID string) (*SyncState, error) {
	filename := syncStateFilename(providerID)
	path := filepath.Join(todosDir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Try legacy migration for github provider
			if providerID == "github" {
				return migrateGHState(todosDir)
			}
			return &SyncState{
				ProviderID: providerID,
				Issues:     make(map[string]SyncStateEntry),
			}, nil
		}
		return nil, err
	}

	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	if state.Issues == nil {
		state.Issues = make(map[string]SyncStateEntry)
	}

	return &state, nil
}

// legacySyncState is the old state format for migration.
type legacySyncState struct {
	Owner  string                         `json:"owner"`
	Repo   string                         `json:"repo"`
	Issues map[string]legacySyncStateEntry `json:"issues"`
}

type legacySyncStateEntry struct {
	GHNumber    int       `json:"ghNumber"`
	TDUpdatedAt time.Time `json:"tdUpdatedAt"`
	GHUpdatedAt time.Time `json:"ghUpdatedAt"`
}

// migrateGHState migrates from legacy gh-sync.json to github-sync.json.
func migrateGHState(todosDir string) (*SyncState, error) {
	legacyPath := filepath.Join(todosDir, legacySyncStateFile)
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncState{
				ProviderID: "github",
				Issues:     make(map[string]SyncStateEntry),
			}, nil
		}
		return nil, err
	}

	var legacy legacySyncState
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy gh-sync.json: %w", err)
	}

	// Convert to new format
	state := &SyncState{
		ProviderID: "github",
		ProviderMeta: map[string]string{
			"owner": legacy.Owner,
			"repo":  legacy.Repo,
		},
		Issues: make(map[string]SyncStateEntry, len(legacy.Issues)),
	}

	for tdID, entry := range legacy.Issues {
		state.Issues[tdID] = SyncStateEntry{
			ExternalID:   strconv.Itoa(entry.GHNumber),
			TDUpdatedAt:  entry.TDUpdatedAt,
			ExtUpdatedAt: entry.GHUpdatedAt,
		}
	}

	// Write new format
	if err := SaveState(todosDir, "github", state); err != nil {
		return nil, fmt.Errorf("save migrated state: %w", err)
	}

	return state, nil
}

// SaveState persists the sync state for a provider to the todos directory.
func SaveState(todosDir string, providerID string, state *SyncState) error {
	filename := syncStateFilename(providerID)
	path := filepath.Join(todosDir, filename)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// FindByTDID returns the sync entry for a td issue, or nil if not found.
func (s *SyncState) FindByTDID(tdID string) *SyncStateEntry {
	entry, ok := s.Issues[tdID]
	if !ok {
		return nil
	}
	return &entry
}

// FindByExternalID returns the td issue ID and sync entry for an external issue ID.
// Returns empty string and nil if not found.
func (s *SyncState) FindByExternalID(externalID string) (string, *SyncStateEntry) {
	for tdID, entry := range s.Issues {
		if entry.ExternalID == externalID {
			return tdID, &entry
		}
	}
	return "", nil
}

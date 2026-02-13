package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const syncStateFile = "gh-sync.json"

// SyncState tracks the mapping between td issues and GitHub issues.
type SyncState struct {
	Owner  string                    `json:"owner"`
	Repo   string                    `json:"repo"`
	Issues map[string]SyncStateEntry `json:"issues"` // keyed by td issue ID
}

// SyncStateEntry tracks a single synced issue pair.
type SyncStateEntry struct {
	GHNumber    int       `json:"ghNumber"`
	TDUpdatedAt time.Time `json:"tdUpdatedAt"`
	GHUpdatedAt time.Time `json:"ghUpdatedAt"`
}

// LoadState loads the sync state from the todos directory.
// Returns an empty state if the file doesn't exist.
func LoadState(todosDir string) (*SyncState, error) {
	path := filepath.Join(todosDir, syncStateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncState{
				Issues: make(map[string]SyncStateEntry),
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

// SaveState persists the sync state to the todos directory.
func SaveState(todosDir string, state *SyncState) error {
	path := filepath.Join(todosDir, syncStateFile)
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

// FindByGHNumber returns the td issue ID and sync entry for a GH issue number.
// Returns empty string and nil if not found.
func (s *SyncState) FindByGHNumber(number int) (string, *SyncStateEntry) {
	for tdID, entry := range s.Issues {
		if entry.GHNumber == number {
			return tdID, &entry
		}
	}
	return "", nil
}

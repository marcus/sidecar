package trifectaindex

import (
	"encoding/json"
	"fmt"
	"time"
)

// WOStatus represents the status of a Work Order
type WOStatus string

const (
	// WOStatusPending indicates the WO is not yet started
	WOStatusPending WOStatus = "pending"
	// WOStatusRunning indicates the WO is currently being worked on
	WOStatusRunning WOStatus = "running"
	// WOStatusDone indicates the WO is completed
	WOStatusDone WOStatus = "done"
	// WOStatusFailed indicates the WO failed
	WOStatusFailed WOStatus = "failed"
)

// WOIndex represents the complete Trifecta work order index
type WOIndex struct {
	Version            int         `json:"version"`
	Schema             string      `json:"schema"`
	GeneratedAt        time.Time   `json:"generated_at"`
	RepoRoot           string      `json:"repo_root"`
	GitHeadSHARepoRoot string      `json:"git_head_sha_repo_root"`
	WorkOrders         []WorkOrder `json:"work_orders"`
	Errors             []string    `json:"errors"`
}

// WorkOrder represents a single Trifecta Work Order
type WorkOrder struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Status          WOStatus  `json:"status"`
	Priority        string    `json:"priority"`
	Owner           string    `json:"owner"`
	EpicID          string    `json:"epic_id"`
	WorktreePath    string    `json:"worktree_path"`
	WorktreeExists  bool      `json:"worktree_exists"`
	Branch          string    `json:"branch"`
	WorktreeHeadSHA string    `json:"worktree_head_sha,omitempty"`
	WOYAMLPath      string    `json:"wo_yaml_path"`
	CreatedAt       time.Time `json:"created_at"`
	ClosedAt        time.Time `json:"closed_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

const (
	// ExpectedVersion is the supported version of the index format
	ExpectedVersion = 1
	// ExpectedSchema is the supported schema identifier
	ExpectedSchema = "trifecta.sidecar.wo_index.v1"
	// IndexFilename is the name of the index file
	IndexFilename = "wo_worktrees.json"
)

// Validate checks that the index conforms to the expected schema
func (idx *WOIndex) Validate() error {
	if idx.Version != ExpectedVersion {
		return fmt.Errorf("unsupported version: %d", idx.Version)
	}
	if idx.Schema != ExpectedSchema {
		return fmt.Errorf("unsupported schema: %s", idx.Schema)
	}
	return nil
}

func parseTimestamp(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// UnmarshalJSON allows legacy space-separated generated_at values.
func (idx *WOIndex) UnmarshalJSON(data []byte) error {
	type alias struct {
		Version            int         `json:"version"`
		Schema             string      `json:"schema"`
		GeneratedAt        string      `json:"generated_at"`
		RepoRoot           string      `json:"repo_root"`
		GitHeadSHARepoRoot string      `json:"git_head_sha_repo_root"`
		WorkOrders         []WorkOrder `json:"work_orders"`
		Errors             []string    `json:"errors"`
	}

	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	idx.Version = aux.Version
	idx.Schema = aux.Schema
	idx.RepoRoot = aux.RepoRoot
	idx.GitHeadSHARepoRoot = aux.GitHeadSHARepoRoot
	idx.WorkOrders = aux.WorkOrders
	idx.Errors = aux.Errors
	if aux.GeneratedAt != "" {
		t, err := parseTimestamp(aux.GeneratedAt)
		if err != nil {
			return err
		}
		idx.GeneratedAt = t
	}
	return nil
}

// UnmarshalJSON allows empty created_at/closed_at values without failing load.
func (wo *WorkOrder) UnmarshalJSON(data []byte) error {
	type alias struct {
		ID              string   `json:"id"`
		Title           string   `json:"title"`
		Status          WOStatus `json:"status"`
		Priority        string   `json:"priority"`
		Owner           string   `json:"owner"`
		EpicID          string   `json:"epic_id"`
		WorktreePath    string   `json:"worktree_path"`
		WorktreeExists  bool     `json:"worktree_exists"`
		Branch          string   `json:"branch"`
		WorktreeHeadSHA string   `json:"worktree_head_sha,omitempty"`
		WOYAMLPath      string   `json:"wo_yaml_path"`
		CreatedAt       string   `json:"created_at"`
		ClosedAt        *string  `json:"closed_at,omitempty"`
		LastError       string   `json:"last_error,omitempty"`
	}

	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	wo.ID = aux.ID
	wo.Title = aux.Title
	wo.Status = aux.Status
	wo.Priority = aux.Priority
	wo.Owner = aux.Owner
	wo.EpicID = aux.EpicID
	wo.WorktreePath = aux.WorktreePath
	wo.WorktreeExists = aux.WorktreeExists
	wo.Branch = aux.Branch
	wo.WorktreeHeadSHA = aux.WorktreeHeadSHA
	wo.WOYAMLPath = aux.WOYAMLPath
	wo.LastError = aux.LastError

	if aux.CreatedAt != "" {
		t, err := parseTimestamp(aux.CreatedAt)
		if err != nil {
			return err
		}
		wo.CreatedAt = t
	}

	if aux.ClosedAt != nil && *aux.ClosedAt != "" {
		t, err := parseTimestamp(*aux.ClosedAt)
		if err != nil {
			return err
		}
		wo.ClosedAt = t
	}

	return nil
}

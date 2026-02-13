package integration

import (
	"context"
	"time"
)

// ExternalIssue is the normalized representation of an external issue.
type ExternalIssue struct {
	ID        string    // External identifier (e.g., "42" for GH issue #42)
	Title     string
	Body      string
	State     string   // "open" or "closed"
	Labels    []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TDIssue is a local td issue as returned by td list --json.
type TDIssue struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Type        string    `json:"type"`
	Priority    string    `json:"priority"`
	Labels      []string  `json:"labels"`
	Points      int       `json:"points"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Provider defines the interface for external issue trackers.
type Provider interface {
	// ID returns a unique provider identifier (e.g., "github").
	ID() string

	// Name returns a human-readable provider name.
	Name() string

	// Available checks whether the provider can be used in the given workDir.
	Available(workDir string) (bool, error)

	// List returns all issues from the external tracker.
	List(ctx context.Context, workDir string) ([]ExternalIssue, error)

	// Create creates a new issue and returns its external ID.
	Create(ctx context.Context, workDir string, issue ExternalIssue) (string, error)

	// Update updates an existing issue by external ID.
	Update(ctx context.Context, workDir string, id string, issue ExternalIssue) error

	// Close closes an issue by external ID.
	Close(ctx context.Context, workDir string, id string) error

	// Reopen reopens an issue by external ID.
	Reopen(ctx context.Context, workDir string, id string) error
}

package jj

// ChangeKind represents a working copy change kind reported by jj status.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
	ChangeRenamed  ChangeKind = "renamed"
	ChangeCopied   ChangeKind = "copied"
	ChangeUnknown  ChangeKind = "unknown"
)

// Change represents a single changed path in the working copy.
type Change struct {
	Kind ChangeKind
	Path string
	Raw  string
}

// StatusSnapshot is the parsed representation of `jj status`.
type StatusSnapshot struct {
	Changes []Change
	Raw     string
}

// Commit is a lightweight commit summary for log views.
type Commit struct {
	ChangeID string
	CommitID string
	Subject  string
}

// LogOptions controls jj log queries.
type LogOptions struct {
	Revisions string
	Limit     int
	Paths     []string
}

// DiffOptions controls jj diff queries.
type DiffOptions struct {
	Revision string
	Paths    []string
	Git      bool
	Summary  bool
	NameOnly bool
	Context  int
}

// GraphCommit maps a parsed commit to its line index in log graph output.
type GraphCommit struct {
	CommitID  string
	LineIndex int
}

// LogGraph captures rendered log graph lines and selectable commits.
type LogGraph struct {
	Lines   []string
	Commits []GraphCommit
}

// StructuredCommit holds fully parsed commit metadata for custom graph rendering.
type StructuredCommit struct {
	ChangeID       string
	ChangeIDShort  string // shortest unique prefix of the change ID
	CommitID       string
	Description    string
	Author         string
	Timestamp      string
	Bookmarks      string
	IsEmpty        bool
	IsConflict     bool
	IsImmutable    bool
	IsWorkingCopy  bool
	Parents        []string
}

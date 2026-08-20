package uirequest

import (
	"encoding/json"
	"time"
)

// Action identifies the requested UI presentation mutation.
type Action string

const (
	ActionOpen           Action = "open"
	ActionRenameWorktree Action = "rename-worktree"
	ActionRenameShell    Action = "rename-shell"
	// ActionNotify posts (or dismisses) a notification in a running instance.
	// Its payload is a notification record rather than a target, because the
	// object it names does not exist until the request lands.
	ActionNotify Action = "notify"
)

// TargetKind identifies the type of object affected by a UI request.
type TargetKind string

const (
	TargetKindFile TargetKind = "file"
	// TargetKindURL is an http(s) link. Activation validates it with
	// terminallink.SafeHTTPURL and refuses anything else; it is never a
	// scheme the app hands to the OS unchecked.
	TargetKindURL      TargetKind = "url"
	TargetKindIssue    TargetKind = "issue"
	TargetKindDiff     TargetKind = "diff"
	TargetKindWorktree TargetKind = "worktree"
	TargetKindShell    TargetKind = "shell"
	// TargetKindResource is an external terminal resource provider's locator.
	// Provider names the configured instance; the running app decides which of
	// that instance's matchers claims the locator, because only it has a live
	// matcher snapshot. The short-lived CLI process starts no provider.
	TargetKindResource TargetKind = "resource"
	// TargetKindNotification is a notification id. Its Value is empty on a
	// post (the id travels in the payload) and set on a dismiss.
	TargetKindNotification TargetKind = "notification"
	// TargetKindSession is a tmux session name to attach. Sidecar-owned names
	// are detected in text (terminallink.KindSession); any name may be
	// attached by a poster that names one.
	TargetKindSession TargetKind = "session"
	// TargetKindTask is a task in the embedded Tasks UI. Nothing detects one
	// in text — a task id is bare 8-hex, indistinguishable from a short sha —
	// so it only ever arrives from a poster that named it.
	TargetKindTask TargetKind = "task"
)

// Status describes the host's response to a UI request.
type Status string

const (
	StatusOpened     Status = "opened"
	StatusQueued     Status = "queued"
	StatusRetargeted Status = "retargeted"
	StatusDeclined   Status = "declined"
	StatusError      Status = "error"
)

// Origin identifies the calling process and its owning Sidecar project shell.
type Origin struct {
	TmuxSession string `json:"tmuxSession"`
	Namespace   string `json:"namespace"`
	ProjectKey  string `json:"projectKey"`
	WorkDir     string `json:"workDir"`
	PID         int    `json:"pid"`
}

// Target identifies the object affected by the request. Value carries the
// action-specific path, issue ID, or presentation value.
type Target struct {
	Kind  TargetKind `json:"kind"`
	Value string     `json:"value"`
	Line  int        `json:"line"`
	// Provider is the configured provider instance for TargetKindResource and
	// is empty for every other kind. It is required rather than guessed: a
	// bare locator must not make the CLI start provider discovery or choose
	// among instances.
	Provider string `json:"provider,omitempty"`
	// Matcher is the provider-stable matcher ID for TargetKindResource when the
	// sender already knows it — a scanned resource span does, because a live
	// matcher claimed the locator to produce the span. It is empty for every
	// other kind and for senders (the CLI) with no matcher snapshot, and a host
	// with one fills it in.
	Matcher string `json:"matcher,omitempty"`
}

// Options specifies optional placement flags. There is deliberately no focus
// option: an open request never moves the user's selection or focus.
type Options struct {
	Split string `json:"split,omitempty"` // "auto", "right", "below"
}

// Request is the payload written by the CLI into the request bus.
type Request struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	TTLMs     int       `json:"ttlMs"`
	Origin    Origin    `json:"origin"`
	Action    Action    `json:"action"`
	Target    Target    `json:"target"`
	Options   Options   `json:"options,omitempty"`
	// Payload carries an action-specific record for actions whose object is
	// not addressable as a Target — a posted notification, so far. It is raw
	// JSON so uirequest stays a transport and does not import the packages
	// that own these records.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Ack is the acknowledgement written by each Sidecar instance handling a request.
type Ack struct {
	Instance string    `json:"instance"`
	Host     string    `json:"host"`
	PID      int       `json:"pid"`
	Status   Status    `json:"status"`
	Reason   string    `json:"reason,omitempty"`
	Surface  string    `json:"surface,omitempty"`
	Pane     int       `json:"pane,omitempty"`
	At       time.Time `json:"at"`
}

// How a request chose its destination. Values are stable for --json callers.
const (
	ResolvedCurrentShell = "current-shell"
	ResolvedShell        = "shell"
	ResolvedProject      = "project"
	ResolvedInstance     = "instance"
)

// Result is the consolidated outcome presented to the agent or caller.
type Result struct {
	Action    Action `json:"action"`
	Target    Target `json:"target"`
	Shell     string `json:"shell"`
	Name      string `json:"name"`
	Project   string `json:"project"`
	Resolved  string `json:"resolved"`
	Delivered int    `json:"delivered"`
	Results   []Ack  `json:"results"`
}

package uirequest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/panelayout"
)

// Action identifies the requested UI presentation mutation.
type Action string

const (
	ActionOpen           Action = "open"
	ActionRenameWorktree Action = "rename-worktree"
	ActionRenameShell    Action = "rename-shell"
	// ActionCreate selects a shell or worktree the CLI just recorded. The
	// durable write has already happened; the payload is the selection cue.
	ActionCreate Action = "create"
	// ActionNotify posts (or dismisses) a notification in a running instance.
	// Its payload is a notification record rather than a target, because the
	// object it names does not exist until the request lands.
	ActionNotify Action = "notify"
	// ActionConfigReload tells live instances that a targeted CLI save has
	// completed. The config file remains authoritative; the request carries no
	// settings payload for a host to trust or merge.
	ActionConfigReload Action = "config-reload"
	// ActionLayout reads or composes a surface's pane layout in one request.
	// Like notify, its object is not addressable as a Target: the payload is a
	// LayoutPayload naming the mode and, for apply, every requested pane. The
	// ack carries per-pane verdicts (Items) and, for get, the layout report
	// itself (Layout).
	ActionLayout Action = "layout"
	// ActionSwitchProject switches the running Sidecar TUI to a configured project.
	ActionSwitchProject Action = "switch-project"
	// ActionPluginChanged tells live instances that a protocol plugin's data
	// moved, so every visible tab of that plugin re-lists. Like notify, its
	// object is not addressable as a Target — the plugin has no locator — so
	// the payload is a PluginChangedPayload.
	//
	// It is the poke a plugin gives Sidecar when the change is not a file it
	// declared under refresh.watch: a shell hook after `dex log`, a daemon that
	// finished indexing. Nothing about it starts a process on its own; it only
	// says that asking again is now worth a process.
	ActionPluginChanged Action = "plugin-changed"
)

// PluginChangedPayload is the ActionPluginChanged record. Collection is
// optional: empty means every collection of that plugin, which is what a tool
// that does not know what it touched should say.
type PluginChangedPayload struct {
	Instance   string `json:"instance"`
	Collection string `json:"collection,omitempty"`
}

// DecodePluginChangedPayload reads the record and refuses one naming no plugin.
func DecodePluginChangedPayload(raw json.RawMessage) (PluginChangedPayload, error) {
	var p PluginChangedPayload
	if len(raw) == 0 {
		return p, fmt.Errorf("plugin-changed payload is required")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("invalid plugin-changed payload: %w", err)
	}
	p.Instance = strings.TrimSpace(p.Instance)
	p.Collection = strings.TrimSpace(p.Collection)
	if p.Instance == "" {
		return p, fmt.Errorf("plugin-changed payload names no plugin")
	}
	return p, nil
}

// Layout modes. Get answers with the current layout; apply opens panes; move
// repositions one pane that is already open.
const (
	LayoutModeGet   = "get"
	LayoutModeApply = "apply"
	LayoutModeMove  = "move"
)

// CreatePayload is the ActionCreate record. Kind distinguishes a workspace
// shell from a worktree; Focus defaults to true when omitted. Run/Type are
// the split-mode seeds executed in the terminal-panel session after it exists.
type CreatePayload struct {
	Kind        string `json:"kind"`
	Session     string `json:"session,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Focus       *bool  `json:"focus,omitempty"`
	Path        string `json:"path,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Run         string `json:"run,omitempty"`
	Type        string `json:"type,omitempty"`
}

const (
	CreateKindShell    = "shell"
	CreateKindWorktree = "worktree"
)

func (p CreatePayload) ShouldFocus() bool {
	if p.Focus == nil {
		return true
	}
	return *p.Focus
}

// LayoutPane is one requested pane of an apply batch or one cell of a full
// --spec layout. Kind uses the layout vocabulary's wire names: primary, file,
// issue, diff, resource, shell, note. The first target opens the pane and the
// rest join it as tabs of the same kind; shells carry run/type/name instead of
// targets. At is an optional grid cell "col.row" (1-based) — a requirement,
// refused rather than re-placed — meaningful only in the batch form: in a spec
// a pane's position IS its column and row.
//
// A live leaf is CARRIED into a spec with exactly what `layout get` prints:
// {"kind":"primary"} for the host's own terminal, {"kind":"shell","session":
// "<tmux-session>"} for a split terminal. A spec that omits a live leaf is
// declined naming the session (apply never destroys a live terminal).
type LayoutPane struct {
	Kind    string   `json:"kind"`
	Targets []string `json:"targets,omitempty"`
	At      string   `json:"at,omitempty"`
	// Provider names the configured plugin instance for kind resource. Required
	// there and ignored elsewhere: a bare locator is never guessed at.
	Provider string `json:"provider,omitempty"`
	// Collection and Query are the plugin collection form of a resource pane.
	// With a collection and no targets the pane opens that collection's tab;
	// with a collection and one target it opens that row's document tab.
	Collection string `json:"collection,omitempty"`
	Query      string `json:"query,omitempty"`
	// Filters is the collection tab's applied filter set, {id: value}. Like
	// Query it needs a Collection to apply to.
	Filters map[string]string `json:"filters,omitempty"`
	Run     string            `json:"run,omitempty"`
	Type    string            `json:"type,omitempty"`
	Name    string            `json:"name,omitempty"`
	// Session carries a shell pane's tmux session. Set means CARRY that live
	// leaf; empty with run/type/name means open a new split beside the origin.
	Session string `json:"session,omitempty"`
}

// LayoutSpec is the full-layout grammar: 1..MaxGridColumns columns, each
// stacking 1..MaxGridRows panes. It is the JSON shape decision 5 settled and
// what a `layout get` grid projects back onto, so get → edit → apply is a
// round trip without translation.
type LayoutSpec struct {
	Columns []LayoutSpecColumn `json:"columns"`
}

// LayoutSpecColumn is one column of a LayoutSpec: its panes, top to bottom.
type LayoutSpecColumn struct {
	Panes []LayoutPane `json:"panes"`
}

func DecodeLayoutSpec(raw json.RawMessage) (LayoutSpec, error) {
	var spec LayoutSpec
	if len(raw) == 0 {
		return spec, fmt.Errorf("layout spec is required")
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return spec, err
	}
	return spec, nil
}

// DecodeLayoutColumns decodes the ActionLayout payload's columns field: the
// spec's column array itself, since the payload already names the mode.
func DecodeLayoutColumns(raw json.RawMessage) ([]LayoutSpecColumn, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("layout spec is required")
	}
	var columns []LayoutSpecColumn
	if err := json.Unmarshal(raw, &columns); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("a layout spec needs at least one column")
	}
	return columns, nil
}

// ValidateLayoutSpec checks a spec's grammar: shape within the caps, known
// kinds, exactly one primary, and the fields each kind takes. It knows
// nothing about the CURRENT tree — live-leaf accounting, target resolution,
// and floors are the host's to answer. Both surfaces share it so a CLI usage
// error and a host decline can never disagree about what a valid spec is.
func ValidateLayoutSpec(spec LayoutSpec) error {
	if len(spec.Columns) == 0 {
		return fmt.Errorf("a layout spec needs at least one column")
	}
	if len(spec.Columns) > panelayout.MaxGridColumns {
		return fmt.Errorf("a layout spec spans %d columns; the cap is %d", len(spec.Columns), panelayout.MaxGridColumns)
	}
	primaries := 0
	for c, column := range spec.Columns {
		if len(column.Panes) == 0 {
			return fmt.Errorf("column %d carries no panes", c+1)
		}
		if len(column.Panes) > panelayout.MaxGridRows {
			return fmt.Errorf("column %d stacks %d panes; the cap is %d", c+1, len(column.Panes), panelayout.MaxGridRows)
		}
		for r, pane := range column.Panes {
			if err := validateSpecPane(pane); err != nil {
				return fmt.Errorf("column %d row %d: %w", c+1, r+1, err)
			}
			if pane.Kind == panelayout.KindNamePrimary {
				primaries++
			}
		}
	}
	switch {
	case primaries == 0:
		return fmt.Errorf("a layout spec needs exactly one \"primary\" pane; none found")
	case primaries > 1:
		return fmt.Errorf("a layout spec needs exactly one \"primary\" pane; found %d", primaries)
	}
	return nil
}

func validateSpecPane(pane LayoutPane) error {
	kind, ok := panelayout.KindByName(strings.TrimSpace(pane.Kind))
	if !ok {
		return fmt.Errorf("unknown pane kind %q", pane.Kind)
	}
	if pane.At != "" {
		return fmt.Errorf("%q carries \"at\"; a spec positions panes by their column and row", pane.Kind)
	}
	switch kind {
	case panelayout.Primary:
		if len(pane.Targets) > 0 || pane.Provider != "" || pane.Collection != "" || pane.Query != "" || len(pane.Filters) > 0 || pane.Run != "" || pane.Type != "" || pane.Name != "" || pane.Session != "" {
			return fmt.Errorf("the primary pane takes no other fields; it carries the host's own terminal")
		}
		return nil
	case panelayout.Shell:
		if len(pane.Targets) > 0 {
			return fmt.Errorf("a shell pane takes session or run/type/name, not targets")
		}
		if pane.Session != "" && (pane.Run != "" || pane.Type != "") {
			return fmt.Errorf("a carried shell takes only \"session\"; run/type would re-seed a live terminal")
		}
		return nil
	case panelayout.Resource:
		if strings.TrimSpace(pane.Provider) == "" {
			return fmt.Errorf("a resource pane needs its configured \"provider\" instance")
		}
		if strings.TrimSpace(pane.Collection) != "" {
			if len(pane.Targets) > 1 {
				return fmt.Errorf("a resource pane with a \"collection\" takes at most one target, the row to open")
			}
			return nil
		}
		if pane.Query != "" {
			return fmt.Errorf("\"query\" needs a \"collection\" to search; a matched locator is not searched")
		}
		if len(pane.Filters) > 0 {
			return fmt.Errorf("\"filters\" needs a \"collection\" to narrow; a matched locator has no filters")
		}
		if len(pane.Targets) == 0 {
			return fmt.Errorf("a resource pane needs at least one target, or a \"collection\" to list")
		}
		return nil
	default:
		if len(pane.Targets) == 0 && kind != panelayout.Diff {
			return fmt.Errorf("a %s pane needs at least one target", kind.Name())
		}
		return nil
	}
}

// LayoutMove is the ActionLayout move record: which pane moves and where.
// Exactly one source form is set — Focused for the surface's focused pane, or
// From as a pre-move grid cell "col.row" — and To names the destination.
//
// To is carried VERBATIM rather than pre-resolved because the direction words
// mean what the keyboard and the modal mean by them, and only the host has the
// tree to ask. The CLI validates the grammar with ParseLayoutMoveTo and the
// host resolves it against panelayout, so neither can drift.
type LayoutMove struct {
	From    string `json:"from,omitempty"`
	Focused bool   `json:"focused,omitempty"`
	To      string `json:"to"`
}

// LayoutMoveForm names how a To value addresses its destination.
type LayoutMoveForm int

const (
	// LayoutMoveCell is "col.row" in the pre-move grid.
	LayoutMoveCell LayoutMoveForm = iota
	// LayoutMoveColumn is a bare column number: append at the bottom of that
	// column, opening one past the last if it does not exist yet.
	LayoutMoveColumn
	// LayoutMoveDirection is left/right/up/down, resolved by the same
	// panelayout.MoveDirection rule the modal's h/j/k/l use.
	LayoutMoveDirection
)

// LayoutMoveTarget is a validated To value. Exactly one field is meaningful,
// named by Form.
type LayoutMoveTarget struct {
	Form      LayoutMoveForm
	Cell      panelayout.Cell
	Column    int
	Direction panelayout.Direction
}

// LayoutMoveDirections is the accepted direction vocabulary, in help order.
var LayoutMoveDirections = []string{"left", "right", "up", "down"}

// ParseLayoutMoveTo validates one --to value. A bare number is a column, so it
// is checked before the cell form, which would otherwise read "3" as 3.1.
func ParseLayoutMoveTo(value string) (LayoutMoveTarget, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return LayoutMoveTarget{}, fmt.Errorf("a move needs a destination: a cell like 1.2, a column like 3, or one of %s", strings.Join(LayoutMoveDirections, "/"))
	}
	switch strings.ToLower(trimmed) {
	case "left":
		return LayoutMoveTarget{Form: LayoutMoveDirection, Direction: panelayout.DirectionLeft}, nil
	case "right":
		return LayoutMoveTarget{Form: LayoutMoveDirection, Direction: panelayout.DirectionRight}, nil
	case "up":
		return LayoutMoveTarget{Form: LayoutMoveDirection, Direction: panelayout.DirectionUp}, nil
	case "down":
		return LayoutMoveTarget{Form: LayoutMoveDirection, Direction: panelayout.DirectionDown}, nil
	}
	if column, err := strconv.Atoi(trimmed); err == nil {
		if column < 1 || column > panelayout.MaxGridColumns {
			return LayoutMoveTarget{}, fmt.Errorf("column %d is outside the %d-column grid", column, panelayout.MaxGridColumns)
		}
		return LayoutMoveTarget{Form: LayoutMoveColumn, Column: column}, nil
	}
	cell, ok := panelayout.ParseCell(trimmed)
	if !ok {
		return LayoutMoveTarget{}, fmt.Errorf("%q is not a cell like 1.2, a column like 3, or one of %s", value, strings.Join(LayoutMoveDirections, "/"))
	}
	if cell.Col > panelayout.MaxGridColumns || cell.Row > panelayout.MaxGridRows {
		return LayoutMoveTarget{}, fmt.Errorf("cell %s is outside the %dx%d layout grid", cell.String(), panelayout.MaxGridColumns, panelayout.MaxGridRows)
	}
	return LayoutMoveTarget{Form: LayoutMoveCell, Cell: cell}, nil
}

// ValidateLayoutMove checks a move record's grammar. It knows nothing about the
// current tree: which pane sits at a cell, whether the destination fits, and
// every cap and floor are the host's to answer.
func ValidateLayoutMove(move LayoutMove) error {
	from := strings.TrimSpace(move.From)
	switch {
	case move.Focused && from != "":
		return fmt.Errorf("name the pane to move by cell or with --focused, not both")
	case !move.Focused && from == "":
		return fmt.Errorf("name the pane to move: a cell like 2.1, or --focused")
	}
	if from != "" {
		cell, ok := panelayout.ParseCell(from)
		if !ok {
			return fmt.Errorf("%q is not a grid cell like 2.1", move.From)
		}
		if cell.Col > panelayout.MaxGridColumns || cell.Row > panelayout.MaxGridRows {
			return fmt.Errorf("cell %s is outside the %dx%d layout grid", cell.String(), panelayout.MaxGridColumns, panelayout.MaxGridRows)
		}
	}
	_, err := ParseLayoutMoveTo(move.To)
	return err
}

// LayoutPayload is the ActionLayout record. Apply carries either the batch's
// Panes or a full-layout Columns spec, never both; get carries neither; move
// carries only its Move record.
type LayoutPayload struct {
	Mode    string          `json:"mode"`
	Panes   []LayoutPane    `json:"panes,omitempty"`
	Columns json.RawMessage `json:"columns,omitempty"`
	Move    *LayoutMove     `json:"move,omitempty"`
}

func DecodeLayoutPayload(raw json.RawMessage) (LayoutPayload, error) {
	var p LayoutPayload
	if len(raw) == 0 {
		return p, fmt.Errorf("layout payload is required")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	switch p.Mode {
	case LayoutModeGet:
		if len(p.Columns) > 0 {
			return p, fmt.Errorf("get carries no layout spec")
		}
	case LayoutModeApply:
		if len(p.Panes) > 0 && len(p.Columns) > 0 {
			return p, fmt.Errorf("apply carries panes or a spec, never both")
		}
		if len(p.Panes) == 0 && len(p.Columns) == 0 {
			return p, fmt.Errorf("apply payload carries no panes")
		}
	case LayoutModeMove:
		if len(p.Panes) > 0 || len(p.Columns) > 0 {
			return p, fmt.Errorf("move repositions one open pane; it carries no panes or spec")
		}
		if p.Move == nil {
			return p, fmt.Errorf("move payload carries no move record")
		}
		if err := ValidateLayoutMove(*p.Move); err != nil {
			return p, err
		}
	default:
		return p, fmt.Errorf("unknown layout mode %q", p.Mode)
	}
	return p, nil
}

func DecodeCreatePayload(raw json.RawMessage) (CreatePayload, error) {
	var p CreatePayload
	if len(raw) == 0 {
		return p, fmt.Errorf("create payload is required")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	if p.Kind == "" {
		return p, fmt.Errorf("create payload kind is required")
	}
	return p, nil
}

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
	// TargetKindNote is a td note identity (nt-…). sidecar open sidecar://note/<id>
	// is the agent-facing form; the pane is a read-only card, not the Notes editor.
	TargetKindNote TargetKind = "note"
)

// Status describes the host's response to a UI request.
type Status string

const (
	StatusOpened     Status = "opened"
	StatusQueued     Status = "queued"
	StatusRetargeted Status = "retargeted"
	StatusDeclined   Status = "declined"
	StatusError      Status = "error"
	// StatusMoved is an accepted layout move: a pane that was already open
	// changed position. Nothing opened and nothing closed, which is why it is
	// its own word rather than StatusOpened.
	StatusMoved Status = "moved"
	// StatusUnchanged is an accepted request that had nothing to do — a move to
	// the cell the pane already occupies, or a direction with no room beyond it.
	// It is a success, not a refusal, and deliberately not StatusRetargeted:
	// nothing was re-pointed at anything.
	StatusUnchanged Status = "unchanged"
)

// Origin identifies the calling process and its owning Sidecar project shell.
type Origin struct {
	TmuxSession string `json:"tmuxSession"`
	Namespace   string `json:"namespace"`
	ProjectKey  string `json:"projectKey"`
	WorkDir     string `json:"workDir"`
	PID         int    `json:"pid"`
	// Sessions is true when the request addresses the running instance's
	// global Sessions surface rather than a project workspace. The project
	// plugin ignores these; the overview answers them.
	Sessions bool `json:"sessions,omitempty"`
	// SessionsRow is the durable inventory ID of the Sessions row. Empty
	// means the currently selected row.
	SessionsRow string `json:"sessionsRow,omitempty"`
	// HostID names the registered remote host a workspace lives on. Empty
	// means this machine. It is set on an attention record's visible origin so
	// "the user is looking at that workspace" cannot be answered by a local
	// workspace that merely shares a tmux session name with a remote one.
	HostID string `json:"hostId,omitempty"`
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
	// Collection names a protocol plugin's collection. A TargetKindResource
	// carrying one is a plugin target rather than a matched locator: with an
	// empty Value it opens the collection tab, and with a Value it opens that
	// row's document tab. It is empty for every other kind.
	Collection string `json:"collection,omitempty"`
	// Query is the collection tab's opening query. It is meaningful only
	// alongside Collection with no Value: a row's document is not searched.
	Query string `json:"query,omitempty"`
	// Filters is the collection tab's applied filter set, {id: value}. It is
	// meaningful only alongside Collection, for the same reason Query is, and
	// the host drops a key the collection never declared at call time.
	Filters map[string]string `json:"filters,omitempty"`
}

// Options specifies optional placement flags. There is deliberately no focus
// option: an open request never moves the user's selection or focus.
type Options struct {
	Split string `json:"split,omitempty"` // "auto", "right", "below"
	// At is an explicit grid cell "col.row" (1-based) for a single open. It is
	// a requirement, not a preference: a kind whose open would retarget an
	// existing pane, and any cell that cannot be honored exactly, decline
	// rather than land elsewhere — the deliberate divergence from Split, which
	// only ever overrides an axis. At and Split are mutually exclusive.
	At string `json:"at,omitempty"`
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

// AckResult is the machine contract for `sidecar request ack --json`.
type AckResult struct {
	ID           string          `json:"id"`
	Action       Action          `json:"action"`
	Status       Status          `json:"status"`
	Reason       string          `json:"reason,omitempty"`
	Surface      string          `json:"surface,omitempty"`
	Pane         int             `json:"pane,omitempty"`
	ItemsVersion int             `json:"itemsVersion,omitempty"`
	Items        []AckItem       `json:"items,omitempty"`
	Layout       json.RawMessage `json:"layout,omitempty"`
}

// ValidRemoteResult reports whether a decoded object is this verb's answer.
func (r AckResult) ValidRemoteResult() bool {
	return r.ID != "" && r.Action != "" && r.Status != ""
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
	// ItemsVersion names the shape of Items whenever Items is present; 1 today.
	// Callers gate on it instead of guessing, so the array can grow without
	// breaking an agent that parsed yesterday's acks.
	ItemsVersion int       `json:"itemsVersion,omitempty"`
	Items        []AckItem `json:"items,omitempty"`
	// Layout carries the ActionLayout get-mode answer verbatim: the layout
	// report the host built from its focused surface's tree. Empty for every
	// other action and for apply acks.
	Layout json.RawMessage `json:"layout,omitempty"`
}

// Per-pane verdicts for an ActionLayout apply. They ride beside Status, which
// stays the overall outcome: on a decline, Items still lists EVERY requested
// pane with the verdict each earned during validation, and Reason names the
// first violation.
const (
	ItemVerdictOpened     = "opened"
	ItemVerdictRetargeted = "retargeted"
	ItemVerdictDeclined   = "declined"
	// ItemVerdictCarried is a --spec pane that was KEPT, not opened: the
	// {"kind":"primary"} entry every spec must carry, and a shell carried by
	// session. Nothing was created for it and nothing was destroyed — the spec
	// accounted for a live leaf, which is the whole reason those entries are
	// mandatory. Reporting it as "opened" told an agent a pane appeared when
	// the same pane had been there all along.
	ItemVerdictCarried = "carried"
	// ItemVerdictMoved is layout move's accepted outcome: this pane changed
	// position and Cell names where it landed.
	ItemVerdictMoved = "moved"
	// ItemVerdictUnchanged is an accepted no-op. Cell names where the pane still
	// is, and Reason says why nothing moved.
	ItemVerdictUnchanged = "unchanged"
)

// AckItem is one requested pane's verdict: what became of it, where it landed
// (cell "col.row" and surface), and — when declined — why.
type AckItem struct {
	Index   int    `json:"index"`
	Verdict string `json:"verdict"`
	Cell    string `json:"cell,omitempty"`
	Surface string `json:"surface,omitempty"`
	Pane    int    `json:"pane,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// How a request chose its destination. Values are stable for --json callers.
const (
	ResolvedCurrentShell = "current-shell"
	ResolvedShell        = "shell"
	ResolvedProject      = "project"
	ResolvedInstance     = "instance"
	ResolvedSessions     = "sessions"
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

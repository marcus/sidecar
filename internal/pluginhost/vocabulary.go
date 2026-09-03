package pluginhost

import (
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/resource"
)

// The plugin protocol's Limits table. Everything in resource v1's table still
// applies; these are the bounds the new vocabulary adds. They are host choices
// the protocol document points back at, so changing one changes the contract.
const (
	// MaxCollections bounds how many collections one plugin may declare.
	MaxCollections = 16
	// MaxColumns bounds one collection's column list.
	MaxColumns = 12
	// MaxViews and MaxSortKeys bound one collection's view pills and sort keys.
	MaxViews    = 32
	MaxSortKeys = 16
	// MaxFilters bounds one collection's declared choosers. Eight is what the
	// View modal can draw without becoming a page of its own, and the first of
	// them is the collection's scope.
	MaxFilters = 8
	// MaxFilterChoices bounds one choice filter's option list.
	MaxFilterChoices = 64
	// MaxFilterIDChars and MaxFilterTitleChars bound a filter's identity and
	// the strings the host paints for it. They are short because a filter id
	// rides in persisted tab state and a filter title rides in the View pill.
	MaxFilterIDChars    = 32
	MaxFilterTitleChars = 32
	// MaxFilterValueChars bounds a text filter's value — the initial text a
	// plugin declares and the value the host sends back. It is bounded on both
	// sides because the value is persisted with the tab.
	MaxFilterValueChars = 64
	// MaxCoverageRows and MaxCoverageReasonChars bound the per-source ledger a
	// page may carry. It is read only by the coverage modal; the notices stay
	// the one-line summary.
	MaxCoverageRows        = 64
	MaxCoverageReasonChars = 200
	// MaxCoverageSourceChars bounds one coverage row's source name.
	MaxCoverageSourceChars = 64
	// MaxActions bounds a plugin's action list and MaxActionInputs one
	// action's form.
	MaxActions      = 32
	MaxActionInputs = 8
	// MaxActionChoices bounds a choice input's option list. It is not in the
	// protocol table: a choice list is a menu the host has to draw, and an
	// unbounded one is a frame that never finishes.
	MaxActionChoices = 64
	// MaxPageItems bounds one page and clamps the requested limit.
	MaxPageItems = 500
	// MaxCellChars bounds one table cell.
	MaxCellChars = 512
	// MaxNotices and MaxNoticeChars bound the notice rows above a list.
	MaxNotices     = 4
	MaxNoticeChars = 200
	// MaxWatchPaths bounds a plugin's declared refresh paths.
	MaxWatchPaths = 8
	// MinRefreshSeconds and MaxRefreshSeconds clamp a declared poll interval.
	MinRefreshSeconds = 15
	MaxRefreshSeconds = 900
	// MaxCollectionIDChars, MaxTitleChars and MaxColumnLabelChars bound the
	// identifiers and labels the host paints. IDs are persisted in pane state,
	// which is why they are bounded at all.
	MaxCollectionIDChars = 64
	MaxCollectionTitle   = 64
	MaxColumnIDChars     = 64
	MaxColumnLabelChars  = 32
	MaxColumnWidth       = 200
	// MaxActionIDChars and MaxActionTitleChars bound an action's identity and
	// its menu row.
	MaxActionIDChars    = 64
	MaxActionTitleChars = 64
	// MaxInputChars bounds an input's label and its default value.
	MaxInputLabelChars   = 64
	MaxInputDefaultChars = 512
	// MaxOutcomeMessageChars bounds the single line an act flashes.
	MaxOutcomeMessageChars = 200
	// MaxRefreshCollections bounds how many collections one outcome may ask to
	// re-list.
	MaxRefreshCollections = MaxCollections
)

// ContextKind is a kind of host context a plugin declares that it reads.
// Declaring it in describe is what lets the settings page and `sidecar plugin
// list` show it before anything runs; configuring the plugin is the trust act,
// and there is no second per-field grant.
type ContextKind string

const (
	// ContextProject is the surface's project: root, working directory, name,
	// branch, and the host ID when the surface is bound to another machine.
	ContextProject ContextKind = "project"
	// ContextSelection is the text the user had selected when they invoked an
	// action.
	ContextSelection ContextKind = "selection"
)

// CoerceContextKind returns the declared kind, or "" for anything this version
// does not know. Nothing else is a context kind in v1: terminal lines,
// scrollback, tmux targets, file contents, and the environment are not, and
// adding one is a protocol revision rather than a field.
func CoerceContextKind(v string) ContextKind {
	switch ContextKind(strings.TrimSpace(v)) {
	case ContextProject:
		return ContextProject
	case ContextSelection:
		return ContextSelection
	default:
		return ""
	}
}

// Context is what the host sends, and only for kinds the plugin declared.
type Context struct {
	Project   *ProjectContext   `json:"project,omitempty"`
	Selection *SelectionContext `json:"selection,omitempty"`
}

// ProjectContext describes the surface's project. On a remote-bound surface
// HostID is non-empty and the paths are that machine's, so a plugin that only
// knows this machine can say so with a typed unavailable naming the host rather
// than answering about the wrong checkout.
type ProjectContext struct {
	Root    string `json:"root,omitempty"`
	WorkDir string `json:"workDir,omitempty"`
	Name    string `json:"name,omitempty"`
	Branch  string `json:"branch,omitempty"`
	HostID  string `json:"hostId,omitempty"`
}

// SelectionContext is the selected text an action was invoked over.
type SelectionContext struct {
	Text string `json:"text"`
}

// Empty reports whether there is nothing to send. A context object with no
// kinds in it is omitted rather than sent as `{}`.
func (c *Context) Empty() bool {
	return c == nil || (c.Project == nil && c.Selection == nil)
}

// filter returns the part of c the plugin declared it reads. An undeclared kind
// is never sent, which is the whole of the context permission model.
func (c *Context) filter(declared []ContextKind) *Context {
	if c.Empty() || len(declared) == 0 {
		return nil
	}
	var out Context
	for _, kind := range declared {
		switch kind {
		case ContextProject:
			out.Project = c.Project
		case ContextSelection:
			out.Selection = c.Selection
		}
	}
	if out.Empty() {
		return nil
	}
	return &out
}

// SearchMode is whether a collection takes a query.
type SearchMode string

const (
	// SearchNone means the collection has no query line.
	SearchNone SearchMode = "none"
	// SearchOptional means a query filters the collection.
	SearchOptional SearchMode = "optional"
	// SearchRequired means the collection is empty until there is a query. The
	// host answers an empty query itself, without starting a process.
	SearchRequired SearchMode = "required"
)

// CoerceSearchMode maps a declared value onto a known one. An unknown value
// becomes SearchNone rather than SearchRequired: a collection the host cannot
// classify must still be listable.
func CoerceSearchMode(v string) SearchMode {
	switch SearchMode(strings.TrimSpace(v)) {
	case SearchOptional:
		return SearchOptional
	case SearchRequired:
		return SearchRequired
	default:
		return SearchNone
	}
}

// ColumnKind decides how the host renders a cell.
type ColumnKind string

const (
	ColumnText      ColumnKind = "text"
	ColumnStatus    ColumnKind = "status"
	ColumnTimestamp ColumnKind = "timestamp"
	ColumnUser      ColumnKind = "user"
	ColumnNumber    ColumnKind = "number"
	ColumnBadge     ColumnKind = "badge"
)

// CoerceColumnKind maps a declared kind onto a known one; anything else is
// plain text, which every kind degrades to safely.
func CoerceColumnKind(v string) ColumnKind {
	switch ColumnKind(strings.TrimSpace(v)) {
	case ColumnStatus:
		return ColumnStatus
	case ColumnTimestamp:
		return ColumnTimestamp
	case ColumnUser:
		return ColumnUser
	case ColumnNumber:
		return ColumnNumber
	case ColumnBadge:
		return ColumnBadge
	default:
		return ColumnText
	}
}

// Align is a column's horizontal alignment.
type Align string

const (
	AlignLeft   Align = "left"
	AlignRight  Align = "right"
	AlignCenter Align = "center"
)

// CoerceAlign maps a declared alignment onto a known one.
func CoerceAlign(v string) Align {
	switch Align(strings.TrimSpace(v)) {
	case AlignRight:
		return AlignRight
	case AlignCenter:
		return AlignCenter
	default:
		return AlignLeft
	}
}

// SortDir is a sort direction.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

// CoerceSortDir maps a declared direction onto a known one, with "" preserved
// as "the plugin stated no default".
func CoerceSortDir(v string) SortDir {
	switch SortDir(strings.TrimSpace(v)) {
	case SortAsc:
		return SortAsc
	case SortDesc:
		return SortDesc
	default:
		return ""
	}
}

// Column is one table column.
type Column struct {
	ID    string
	Label string
	// Width is a hint in cells; the host reflows. Zero means no hint.
	Width int
	Align Align
	Kind  ColumnKind
	// Primary names the row. Exactly one column is primary; validation
	// promotes the first column when the plugin named none.
	Primary bool
	// Secondary is folded under the primary line when the pane is too narrow
	// for a table. At most one column is secondary.
	Secondary bool
}

// View is a named preset filter, shown as a pill.
type View struct {
	ID    string
	Title string
}

// SortKey is one sortable key offered in the sort picker.
type SortKey struct {
	ID    string
	Label string
	// Default is "asc"/"desc" when this key is the collection's default sort,
	// and "" otherwise.
	Default SortDir
}

// Refresh is how a collection stays current without a resident process.
type Refresh struct {
	// EverySeconds is a poll interval, clamped to [15, 900] and polled only
	// while a tab from this plugin is visible. Zero means no polling.
	EverySeconds int
	// Watch are absolute paths under the user's home directory, already
	// expanded and validated. A change to one re-lists visible collections.
	Watch []string
}

// Collection is a named, listable set of rows the host shows as a table with a
// cursor.
type Collection struct {
	ID      string
	Title   string
	Search  SearchMode
	Columns []Column
	Views   []View
	Sort    []SortKey
	// Filters are the collection's declared choosers, in declared order. The
	// first is the collection's scope, whose current value the host always
	// shows.
	Filters []Filter
	// Detail reports whether get is meaningful for rows.
	Detail  bool
	Refresh Refresh
	// Context narrows the collection: ["project"] means it is meaningful only
	// where project context exists, so a global surface hides it.
	Context []ContextKind
}

// PrimaryColumn returns the column that names a row.
func (c Collection) PrimaryColumn() (Column, bool) {
	for _, col := range c.Columns {
		if col.Primary {
			return col, true
		}
	}
	return Column{}, false
}

// ActionTarget is what an action applies to.
type ActionTarget string

const (
	// ActionOnItem applies to one collection row.
	ActionOnItem ActionTarget = "item"
	// ActionOnCollection applies to the whole list.
	ActionOnCollection ActionTarget = "collection"
	// ActionOnResource applies to a matcher-resolved document.
	ActionOnResource ActionTarget = "resource"
	// ActionOnGlobal has no subject.
	ActionOnGlobal ActionTarget = "global"
)

// CoerceActionTarget maps a declared target onto a known one, or "" for one the
// host does not know — which validation refuses rather than guessing at.
func CoerceActionTarget(v string) ActionTarget {
	switch ActionTarget(strings.TrimSpace(v)) {
	case ActionOnItem:
		return ActionOnItem
	case ActionOnCollection:
		return ActionOnCollection
	case ActionOnResource:
		return ActionOnResource
	case ActionOnGlobal:
		return ActionOnGlobal
	default:
		return ""
	}
}

// InputKind is the shape of one action input.
type InputKind string

const (
	InputText      InputKind = "text"
	InputMultiline InputKind = "multiline"
	InputChoice    InputKind = "choice"
	InputConfirm   InputKind = "confirm"
)

// CoerceInputKind maps a declared kind onto a known one, or "" for an unknown
// one — refused rather than degraded, because a form field the host draws as
// the wrong type collects the wrong value.
func CoerceInputKind(v string) InputKind {
	switch InputKind(strings.TrimSpace(v)) {
	case InputText:
		return InputText
	case InputMultiline:
		return InputMultiline
	case InputChoice:
		return InputChoice
	case InputConfirm:
		return InputConfirm
	default:
		return ""
	}
}

// ActionInput is one field of an action's form.
type ActionInput struct {
	ID       string
	Label    string
	Kind     InputKind
	Required bool
	Choices  []string
	Default  string
}

// Action is a typed operation the user can invoke. The plugin declares it; the
// host decides how it is reached. Actions never carry code, keys the host did
// not grant, or colours.
type Action struct {
	ID    string
	Title string
	On    ActionTarget
	// Collection is the collection this applies to, required for item and
	// collection actions and empty otherwise.
	Collection string
	Inputs     []ActionInput
	Mutates    bool
	// Confirm is the resolved answer, not the declared one: a mutating action
	// with no inputs confirms unless the plugin said confirm:false, and an
	// action with inputs does not, because its form is the confirm step.
	Confirm bool
	// Key is a single lowercase letter the plugin asked for. It is a request,
	// never a grant: the host clears it when its own reserved set or the
	// surface's bindings already use that key, and it is never persisted.
	Key string
}

// PageOutcome is what a list call is claiming about coverage.
type PageOutcome string

const (
	// OutcomeAnswered means the plugin asked everything it should have.
	OutcomeAnswered PageOutcome = "answered"
	// OutcomeAbstained means nothing matched and every source was fine, so an
	// empty list honestly reads as "no matches".
	OutcomeAbstained PageOutcome = "abstained"
	// OutcomeDegraded means some eligible source could not answer, so an empty
	// list reads as "no matches, and coverage was incomplete".
	OutcomeDegraded PageOutcome = "degraded"
	// OutcomeFailed means every source that was asked failed, so the page says
	// nothing at all about the query. The host renders an error card over an
	// empty list and never the words "no matches", which would be a claim
	// nothing made.
	OutcomeFailed PageOutcome = "failed"
)

// Outcome describes the ROW SET of this page and nothing else.
//
// A collection whose rows are all present answers `answered` even when what
// those rows describe is unhealthy — a list of sources, half of them stale, is
// a complete list. Health of the subject belongs on the rows, in their own
// status pills. Conflating the two is what made an honest plugin report
// `degraded` for a page that was in fact complete, and left the reader unable
// to tell "I could not look" from "what I found is in a bad way".

// CoercePageOutcome maps a declared outcome onto a known one.
//
// An absent outcome is "answered": that is what every plugin that never thinks
// about coverage means. An outcome this version does not recognise becomes
// degraded rather than answered, because the honest reading of a claim the host
// cannot understand is that coverage is unknown — and of the two ways to be
// wrong, saying "coverage may be incomplete" is the one that does not invent a
// guarantee on the plugin's behalf.
func CoercePageOutcome(v string) PageOutcome {
	switch PageOutcome(strings.TrimSpace(v)) {
	case "":
		return OutcomeAnswered
	case OutcomeAnswered:
		return OutcomeAnswered
	case OutcomeAbstained:
		return OutcomeAbstained
	case OutcomeFailed:
		return OutcomeFailed
	default:
		return OutcomeDegraded
	}
}

// CoverageState is what one source did when this page was gathered.
type CoverageState string

const (
	// CoverageAnswered means the source answered in full.
	CoverageAnswered CoverageState = "answered"
	// CoverageTimeout means the source ran out of the budget it was given.
	CoverageTimeout CoverageState = "timeout"
	// CoverageUnhealthy means the source was reachable but could not be
	// trusted to answer — a stale checkpoint, a broken index.
	CoverageUnhealthy CoverageState = "unhealthy"
	// CoverageSkipped means the source was not asked at all.
	CoverageSkipped CoverageState = "skipped"
	// CoverageFailed means the source was asked and errored.
	CoverageFailed CoverageState = "failed"
)

// CoerceCoverageState maps a declared state onto a known one. An unknown state
// reads as failed for the same reason an unknown outcome reads as degraded: of
// the two ways to be wrong about a word the host cannot read, the one that does
// not invent a guarantee on the plugin's behalf is the honest one.
func CoerceCoverageState(v string) CoverageState {
	switch CoverageState(strings.TrimSpace(v)) {
	case CoverageAnswered:
		return CoverageAnswered
	case CoverageTimeout:
		return CoverageTimeout
	case CoverageUnhealthy:
		return CoverageUnhealthy
	case CoverageSkipped:
		return CoverageSkipped
	default:
		return CoverageFailed
	}
}

// Tone is the status tone the host paints a coverage state in. It is the host's
// mapping, not the plugin's: a plugin must not be able to colour its own
// failure green.
func (s CoverageState) Tone() resource.Tone {
	switch s {
	case CoverageAnswered:
		return resource.ToneSuccess
	case CoverageSkipped:
		return resource.ToneNeutral
	case CoverageFailed:
		return resource.ToneDanger
	default:
		return resource.ToneWarning
	}
}

// Coverage is one source's row in a page's per-source ledger. It is read only
// by the host's coverage modal; the notices stay the one-line summary, because
// thirteen sources' states do not fit in four notice rows and a reader who
// wants them is asking a second question.
type Coverage struct {
	Source string
	State  CoverageState
	// Reason is the plugin's own one-line explanation, shown verbatim.
	Reason string
	// ElapsedMs is how long the source took. Zero means the plugin did not say.
	ElapsedMs int
}

// Omitted is what the plugin held back from a page it could otherwise have
// filled: rows below its own relevance floor, and rows past its budget. The
// host renders both as data in the summary row rather than leaving a plugin to
// write them into free-text notices.
type Omitted struct {
	Suppressed int
	Dropped    int
}

// Any reports whether anything was held back.
func (o Omitted) Any() bool { return o.Suppressed > 0 || o.Dropped > 0 }

// Notice is one single-line row the host shows above or below a list.
type Notice struct {
	Tone resource.Tone
	Text string
}

// Item is one row of a page.
type Item struct {
	// ID is what get and item actions receive.
	ID string
	// Cells is keyed by column ID; a missing cell renders blank.
	Cells map[string]string
	// Status is an optional pill.
	Status *resource.Status
	// SourceURL is an optional validated http(s) URL.
	SourceURL string
}

// Page is one list answer.
type Page struct {
	Outcome PageOutcome
	Items   []Item
	// NextCursor is opaque; empty means no more. The host pages on demand,
	// never eagerly.
	NextCursor string
	// Total is an optional count for the footer. Zero means the plugin did not
	// say.
	Total   int
	Notices []Notice
	// Omitted is what the plugin held back, as counts the summary row renders
	// as data.
	Omitted Omitted
	// Coverage is the per-source ledger, read only by the coverage modal.
	Coverage []Coverage
	// Truncated reports that the plugin sent more items than the host keeps.
	Truncated bool
	// CoverageTruncated reports the same for the coverage ledger, so the modal
	// can say the table is not the whole of it rather than implying it is.
	CoverageTruncated bool
}

// ActStatus is whether an action succeeded.
type ActStatus string

const (
	// ActDone is a successful action.
	ActDone ActStatus = "done"
	// ActFailed is a typed failure with a message — not a transport failure.
	ActFailed ActStatus = "failed"
)

// CoerceActStatus maps a declared status onto a known one. An unrecognised
// status is a failure: an action whose result the host cannot read must not be
// reported to the user as having worked.
func CoerceActStatus(v string) ActStatus {
	if ActStatus(strings.TrimSpace(v)) == ActDone {
		return ActDone
	}
	return ActFailed
}

// OpenTarget is a row an action asks the host to open afterwards.
type OpenTarget struct {
	Collection string
	ID         string
}

// Outcome is one act answer.
type Outcome struct {
	Status  ActStatus
	Message string
	// Refresh names collections the host should re-list if they are visible.
	Refresh []string
	// Open is the capture-then-show row, or nil.
	Open *OpenTarget
}

// ClampRefreshSeconds brings a declared poll interval into range. Zero and
// negative mean "no polling" rather than "poll as fast as possible".
func ClampRefreshSeconds(seconds int) int {
	switch {
	case seconds <= 0:
		return 0
	case seconds < MinRefreshSeconds:
		return MinRefreshSeconds
	case seconds > MaxRefreshSeconds:
		return MaxRefreshSeconds
	default:
		return seconds
	}
}

// ClampListLimit brings a requested page size into range.
func ClampListLimit(limit int) int {
	switch {
	case limit <= 0:
		return MaxPageItems
	case limit > MaxPageItems:
		return MaxPageItems
	default:
		return limit
	}
}

// CallTimeout is the per-call budget for list, get, act and resolve. describe
// has its own fixed, shorter budget because it is local and must be fast.
func CallTimeout(configured time.Duration) time.Duration {
	return resource.ClampResolveTimeout(configured)
}

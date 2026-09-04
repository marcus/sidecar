package agentintegration

import (
	_ "embed"
	"strings"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Kilo Code integration.
//
// Same split as OpenCode and Pi, for the same reason: the handler below is a Go
// mirror of the state machine inside assets/kilo/sidecar-lifecycle.js, kept
// deliberately separate from the shipped JavaScript so the mapping can be
// replayed in an ordinary test. A bug in "which event becomes which lane" is
// then a failing assertion rather than something discovered when a pane freezes
// on someone's machine. The two are held together by
// TestBundledKiloAssetBehavesLikeTheHandler, which drives both over the same
// fixtures and requires identical ordered argv.
//
// The provider half is ported from Herdr's kilo integration at version 4 and is
// kept verbatim in behaviour apart from the session.status field-shape fix; the
// deliberate differences are named in the asset and recorded in portedfrom.go.

// Kilo integration identity.
const (
	KiloProvider = "kilo"
	KiloSource   = "sidecar.kilo.plugin"

	// KiloAssetVersion is the bundled asset's version. Authority is granted to a
	// source at a version, so this changing is what makes an installed copy
	// "outdated" rather than merely different.
	//
	// Bump it whenever assets/kilo/sidecar-lifecycle.js changes, once a release
	// has shipped `agent integration install kilo`. Until then the bytes may be
	// revised in place, because there is no earlier copy of version 1 anywhere to
	// be misread. See asset_golden_test.go, which states the whole bump order.
	KiloAssetVersion = "1"

	// KiloAssetName is the filename the asset is installed as.
	//
	// Kilo globs `{plugin,plugins}/*.{ts,js}` inside every config directory it
	// discovers, so the extension may be either and .js is chosen: node cannot
	// import a .ts module, and the equivalence harness has to run the shipped
	// file itself.
	KiloAssetName = "sidecar-lifecycle.js"
)

//go:embed assets/kilo/sidecar-lifecycle.js
var kiloAsset string

// KiloAsset returns the bundled plugin source.
func KiloAsset() string { return kiloAsset }

// KiloEvent is one flattened Kilo plugin event.
//
// It carries only what the mapping reads: the event type, the session id, and
// the raw session.status value. No message content ever reaches this type, so no
// handler bug can leak any.
type KiloEvent struct {
	// Type is the bus event name, or the literal "chat.message" for the hook of
	// that name. Kilo has no bus event called chat.message, so the two cannot
	// collide.
	Type string
	// SessionID is properties.sessionID, when the event carried one.
	SessionID string
	// StatusType is properties.status.type, the shape Kilo actually emits.
	StatusType string
	// StatusString is properties.status when the provider sends a bare string.
	//
	// Both shapes are modelled because upstream's asset reads only the second and
	// Kilo only sends the first: keeping them apart is what lets a fixture drive
	// the bug and the fix separately rather than asserting them by prose.
	StatusString string
}

// KiloAction is one report the handler wants made.
type KiloAction struct {
	Kind   agentlifecycle.Kind
	State  agentactivity.State
	Reason agentlifecycle.ReasonCode
	// SessionID is the reference a KindSession action binds. Kilo reports an id
	// and never a transcript path, so there is no path variant here.
	SessionID string
}

// KiloHandler maps Kilo's plugin events onto lifecycle reports.
//
// The zero value is ready.
type KiloHandler struct {
	// lane is the last lane reported and session the last session bound. Both
	// exist only to suppress an exact repeat; see the asset for why suppression
	// belongs to the transport half.
	lane    agentactivity.State
	session string
}

// Handle returns the actions one event should produce, in order. Most events
// produce none.
func (h *KiloHandler) Handle(ev KiloEvent) []KiloAction {
	var actions []KiloAction

	// adopt takes the session id the event carried. Upstream attaches the
	// event's own sessionID to every state report rather than remembering one,
	// so adopting on every event is the same behaviour expressed once.
	adopt := func() {
		if ev.SessionID != "" {
			h.session = ev.SessionID
		}
	}
	lane := func(state agentactivity.State, reason agentlifecycle.ReasonCode) {
		if h.lane == state {
			return
		}
		h.lane = state
		actions = append(actions, KiloAction{Kind: agentlifecycle.KindState, State: state, Reason: reason})
	}
	bind := func() {
		if ev.SessionID == "" || ev.SessionID == h.session {
			return
		}
		h.session = ev.SessionID
		actions = append(actions, KiloAction{Kind: agentlifecycle.KindSession, SessionID: ev.SessionID})
	}

	switch ev.Type {
	case "chat.message":
		adopt()
		lane(agentactivity.StateWorking, agentlifecycle.ReasonTurnStart)

	case "session.created", "session.updated":
		bind()

	case "session.status":
		// adopt belongs to the two lane branches only. bind adopts as part of
		// binding, and adopting before it would make bind's own repeat check
		// compare the id against itself and suppress every rotation.
		switch kiloStateFromSessionStatus(ev) {
		case agentactivity.StateWorking:
			adopt()
			lane(agentactivity.StateWorking, agentlifecycle.ReasonTurnStart)
		case agentactivity.StateIdle:
			adopt()
			lane(agentactivity.StateIdle, agentlifecycle.ReasonTurnComplete)
		default:
			bind()
		}

	case "tool.execute.before", "tool.execute.after":
		// Unreachable on kilo 7.5.9: these are plugin hooks rather than bus
		// events, so an `event` handler never sees them. Kept because upstream
		// lists them and the branch costs one comparison; `tool_use` is not
		// claimed in the capability entry on the strength of it.
		adopt()
		lane(agentactivity.StateWorking, agentlifecycle.ReasonToolUse)

	case "permission.replied", "question.replied", "question.rejected":
		adopt()
		lane(agentactivity.StateWorking, agentlifecycle.ReasonPermissionResolved)

	case "session.compacted":
		adopt()
		lane(agentactivity.StateWorking, agentlifecycle.ReasonCompaction)

	case "permission.asked":
		adopt()
		lane(agentactivity.StateBlocked, agentlifecycle.ReasonPermissionRequest)

	case "question.asked":
		adopt()
		lane(agentactivity.StateBlocked, agentlifecycle.ReasonQuestion)

	case "session.error":
		// Upstream reports blocked for a session error and the port keeps it.
		// The traces show the next session.status idle closing that lane within
		// a millisecond, which is why it is safe and why the status fix below is
		// load-bearing.
		adopt()
		lane(agentactivity.StateBlocked, agentlifecycle.ReasonProviderError)

	case "session.idle":
		adopt()
		lane(agentactivity.StateIdle, agentlifecycle.ReasonTurnComplete)

	case "session.deleted":
		// Upstream ignores it, and so does this.
	}
	return actions
}

// kiloSessionStates is upstream's status vocabulary, kept verbatim.
//
// Kilo 7.5.9's own SessionStatus schema admits idle, busy, retry and offline, so
// three of these cannot occur and `retry` -- which can -- is deliberately absent.
// An unmapped status re-asserts the binding rather than a lane, which is
// upstream's behaviour and is harmless: a retry happens inside a turn a busy
// assertion already opened.
var kiloSessionStates = map[string]agentactivity.State{
	"idle":      agentactivity.StateIdle,
	"active":    agentactivity.StateWorking,
	"busy":      agentactivity.StateWorking,
	"pending":   agentactivity.StateWorking,
	"running":   agentactivity.StateWorking,
	"streaming": agentactivity.StateWorking,
	"working":   agentactivity.StateWorking,
}

// kiloStateFromSessionStatus reads the session.status discriminator from either
// shape the field can take.
//
// This is the one genuine upstream bug the port fixes. Herdr's kilo asset
// accepts a status only when it is a string; Kilo's session.status carries an
// object whose `type` is the discriminator, so upstream's asset never maps a
// status to a lane on any release shipping that schema. Herdr's own opencode
// asset at version 10 already reads `status?.type`; the kilo variant never
// received it.
func kiloStateFromSessionStatus(ev KiloEvent) agentactivity.State {
	kind := ev.StatusString
	if ev.StatusType != "" {
		kind = ev.StatusType
	}
	if kind == "" {
		return ""
	}
	return kiloSessionStates[strings.ToLower(kind)]
}

// KiloReportArgs builds the exact CLI argv one action becomes.
//
// It mirrors buildArgs in the bundled asset. Both exist because the asset must
// construct argv in JavaScript at runtime, and the equivalence test compares the
// two lists element for element -- so this is the Go statement of the same
// contract, not a convenience wrapper.
//
// NEITHER VERB CARRIES --seq, and there is no sequence parameter to pass. The
// store assigns under the exclusive lock it already takes for the append, which
// is the only place the read and the write are atomic. The asset's buildArgs
// carries the full account of why holding a counter here was tried twice on the
// Pi asset and dropped reports both times.
func KiloReportArgs(action KiloAction, sessionID string) []string {
	if action.Kind == agentlifecycle.KindSession {
		return []string{
			"agent", "report-session",
			"--kind", KiloProvider,
			"--source", KiloSource,
			"--id", action.SessionID,
		}
	}
	args := []string{
		"agent", "report",
		"--source", KiloSource,
		"--source-version", KiloAssetVersion,
		"--provider", KiloProvider,
	}
	if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}
	args = append(args, "--state", string(action.State))
	return append(args, "--reason", string(action.Reason))
}

// Session returns the provider session id the handler has adopted, which the
// asset also carries on every state report.
func (h *KiloHandler) Session() string { return h.session }

package agentintegration

import (
	_ "embed"
	"regexp"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The OMP (oh-my-pi) integration.
//
// Same split as Pi, Kilo and OpenCode, for the same reason: the handler below is
// a Go mirror of the state machine inside assets/omp/sidecar-omp-lifecycle.js,
// kept deliberately separate from the shipped JavaScript so the mapping can be
// replayed in an ordinary test. A bug in "which event becomes which lane" is
// then a failing assertion rather than something discovered when a pane freezes
// on someone's machine. The two are held together by
// TestBundledOmpAssetBehavesLikeTheHandler, which drives both over the same
// fixtures and requires identical ordered output.
//
// The provider half is ported from Herdr's omp integration at version 9 and is
// kept verbatim in behavior; the deliberate differences are named in the asset
// and recorded in portedfrom.go.
//
// OMP is a rebranded fork of Pi's codebase, so the two adapters look alike. They
// do not map alike, and the differences were measured against OMP 18.1.8's own
// shipped TypeScript rather than inherited from the resemblance. The four that
// change the port are named at the top of the asset; the shortest statement of
// them is that OMP has no agent_settled, has a real permission system, retries
// provider errors by itself, and gates on ctx.hasUI.

// OMP integration identity.
const (
	OmpProvider = "omp"
	OmpSource   = "sidecar.omp.extension"

	// OmpAssetVersion is the bundled asset's version. Authority is granted to a
	// source at a version, so this changing is what makes an installed copy
	// "outdated" rather than merely different.
	//
	// Bump it whenever assets/omp/sidecar-omp-lifecycle.js changes, once a
	// release has shipped `agent integration install omp`. See
	// asset_golden_test.go, which states the whole bump order.
	OmpAssetVersion = "1"

	// OmpAssetName is the filename the asset is installed as.
	//
	// It is deliberately NOT sidecar-lifecycle.js, which is what every other
	// file-shaped Sidecar asset is called. OMP and Pi read the same
	// PI_CODING_AGENT_DIR, so with that variable set the two providers resolve
	// to one extensions directory (see ompExtensionsCollision), and a shared
	// name would put Sidecar's OMP asset exactly where Sidecar's Pi asset lives.
	// The marker rule would still refuse to overwrite it -- ownership compares
	// the source id, not the filename -- but a refusal is a worse outcome than
	// two files that were never going to collide in the first place. Herdr names
	// its own OMP asset apart for the same reason.
	OmpAssetName = "sidecar-omp-lifecycle.js"

	// OmpIdleDebounceMS and OmpRetryGraceMS are upstream's two constants,
	// unchanged, and the asset carries the same values.
	//
	// The debounce is why an ended run does not publish idle immediately: OMP can
	// end a run and start another at once, and a pane that flickered through idle
	// between two halves of one piece of work is a notification a user did not
	// want. The grace is how long a retryable provider error is held at working
	// before it is admitted to be a block, because OMP retries by itself and a
	// failure it is about to clear is still work in progress.
	OmpIdleDebounceMS = 250
	OmpRetryGraceMS   = 2500
)

//go:embed assets/omp/sidecar-omp-lifecycle.js
var ompAsset string

// OmpAsset returns the bundled extension source.
func OmpAsset() string { return ompAsset }

// OmpEvent is one flattened OMP extension event.
//
// It carries only what the mapping reads: a few of the events' own fields, and
// the three parts of `ctx` the asset touches. No message content, tool argument,
// tool result or file content ever reaches this type, so no handler bug can leak
// any. The two error fields are the exception that proves the rule: they are the
// last assistant message's stop reason and error string, they exist only so the
// retry classifier can run, and neither is ever put into an argv.
type OmpEvent struct {
	// Type is the OMP event name -- session_start, session_switch, agent_start,
	// agent_end, tool_approval_requested, tool_approval_resolved,
	// tool_execution_start, tool_execution_end, session_shutdown -- or one of the
	// two synthetic names the mapping's own timers re-enter through, idle_timer
	// and retry_timer, or "blocked" for the untyped event-bus channel.
	Type string
	// Reason is session_switch's reason (new, resume, fork) and
	// tool_approval_requested's optional human label.
	Reason string
	// HasUI is ctx.hasUI, and it is the TUI gate. It is a tri-state because an
	// absent value is not a claim that the session has a UI.
	HasUI *bool
	// Idle is ctx.isIdle(), a tri-state on purpose: an absent isIdle is unknown,
	// and unknown must not open a turn.
	Idle *bool
	// SessionPath and SessionID are ctx.sessionManager's two answers.
	SessionPath string
	SessionID   string
	// Tool is the toolName on the four tool events.
	Tool string
	// Question is the first question text on an `ask` tool execution. It is used
	// only as a suppression key and is never transmitted.
	Question string
	// BlockedActive and BlockedLabel are the payload of the blocked bus channel.
	BlockedActive bool
	BlockedLabel  string
	// WillContinue is agent_end's flag, a tri-state because an older OMP build
	// omits the field entirely and that is not the same as "no continuation".
	WillContinue *bool
	// StopReason and ErrorMessage are the last assistant message's, read for the
	// retry classifier and for nothing else.
	StopReason   string
	ErrorMessage string
}

// OmpActionKind distinguishes the two things the mapping can ask the runtime to
// do that are not a report.
type OmpActionKind string

const (
	// OmpActionSchedule arms one of the two timers.
	OmpActionSchedule OmpActionKind = "schedule"
	// OmpActionCancel disarms both.
	OmpActionCancel OmpActionKind = "cancel"
)

// OmpAction is one thing the handler wants done.
//
// It is a report, a session binding, or a timer instruction. The timer
// instructions exist because this provider's mapping has a clock in it and a
// pure function may not: the mapping decides when a timer should be armed and
// the runtime owns the setTimeout, so a fixture can drive the debounced idle and
// the retry admission deterministically in both implementations.
type OmpAction struct {
	// Kind is the lifecycle kind for a report or a binding, and empty for a timer
	// instruction, which Timer names instead.
	Kind   agentlifecycle.Kind
	State  agentactivity.State
	Reason agentlifecycle.ReasonCode
	// SessionPath and SessionID are the reference a KindSession action binds.
	// Exactly one is set: a path identifies the transcript a restore would
	// resume, which an id alone does not, so a path wins when both are known.
	SessionPath string
	SessionID   string
	// Timer is OmpActionSchedule or OmpActionCancel for a timer instruction, and
	// empty otherwise.
	Timer OmpActionKind
	// TimerName is "idle" or "retry" on a schedule.
	TimerName string
	// DelayMS is the schedule's delay.
	DelayMS int
}

// OmpHandler maps OMP's extension events onto lifecycle reports.
//
// The zero value is ready and uses upstream's default durations. Every field is
// upstream's, because the ladder they feed is upstream's.
type OmpHandler struct {
	// IdleDebounceMS and RetryGraceMS override the defaults. Zero means the
	// default, which is why they are not settable to zero here -- the asset's own
	// environment lever can set zero, and this mirror does not need to, since a
	// replay never waits for a timer.
	IdleDebounceMS int
	RetryGraceMS   int

	// rootSession latches once a session with a UI has been seen. Until then
	// nothing is reported, which is what keeps a print, json or plain rpc
	// invocation from claiming a pane it is not on screen in.
	rootSession  bool
	agentActive  bool
	blockedCount int
	// retryHoldActive is a failed run OMP may still retry: the pane stays at
	// working. failureBlocked is that same failure after the grace ran out.
	retryHoldActive bool
	failureBlocked  bool
	// failureMessage and blockedMessage are carried but never transmitted. They
	// participate in the repeat-suppression comparison exactly as upstream's do,
	// which is why they are here; putting an unbounded provider error string or a
	// model-authored question on the wire is a separate decision, and the answer
	// to it is no.
	failureMessage string
	blockedMessage string
	lastState      agentactivity.State
	lastMessage    string
	sessionPath    string
	sessionID      string
}

func (h *OmpHandler) idleDebounce() int {
	if h.IdleDebounceMS > 0 {
		return h.IdleDebounceMS
	}
	return OmpIdleDebounceMS
}

func (h *OmpHandler) retryGrace() int {
	if h.RetryGraceMS > 0 {
		return h.RetryGraceMS
	}
	return OmpRetryGraceMS
}

// Handle returns the actions one event should produce, in order. Most events
// produce none.
func (h *OmpHandler) Handle(ev OmpEvent) []OmpAction {
	var actions []OmpAction
	// Upstream's activateRootSession reports the session, and three of the
	// handlers below then report it again on the same event. On Herdr's socket
	// that is one extra frame; here it would be one extra subprocess with
	// byte-identical argv. So a binding is emitted at most once per event, and
	// the per-turn re-binding upstream does is kept in full.
	bound := false

	bind := func() {
		if bound {
			return
		}
		bound = true
		actions = append(actions, h.sessionActions()...)
	}
	cancel := func() {
		actions = append(actions, OmpAction{Timer: OmpActionCancel})
	}
	// activateRootSession is upstream's, hasUI gate included. It is called both
	// to open the session and, from four later handlers, to adopt a session whose
	// session_start this extension was not loaded for.
	activateRootSession := func() bool {
		if ev.HasUI == nil || !*ev.HasUI {
			return false
		}
		h.rootSession = true
		h.updateSessionRef(ev)
		bind()
		return true
	}
	activateBlocked := func(message string, reason agentlifecycle.ReasonCode) {
		cancel()
		h.blockedCount++
		h.blockedMessage = message
		actions = append(actions, h.publish(reason, false)...)
	}
	deactivateBlocked := func() {
		if h.blockedCount > 0 {
			h.blockedCount--
		}
		if h.blockedCount == 0 {
			h.blockedMessage = ""
		}
		actions = append(actions, h.publish(agentlifecycle.ReasonPermissionResolved, false)...)
	}

	switch ev.Type {
	case "session_start":
		if !activateRootSession() {
			return actions
		}
		// A reload can replace the extension mid-run without emitting another
		// agent_start, so the run's true state is read back rather than assumed
		// idle. Explicitly false, not "not true": unknown is not working.
		h.agentActive = ev.Idle != nil && !*ev.Idle
		return append(actions, h.publish(agentlifecycle.ReasonSessionStart, true)...)

	case "session_switch":
		if !activateRootSession() {
			return actions
		}
		// A switch is a different conversation in the same process: every counter
		// from the previous one is meaningless and is dropped rather than carried.
		cancel()
		h.clearFailureState()
		h.agentActive = false
		h.blockedCount = 0
		h.blockedMessage = ""
		return append(actions, h.publish(agentlifecycle.ReasonSessionChange, true)...)

	case "agent_start":
		if !h.rootSession && !activateRootSession() {
			return actions
		}
		h.updateSessionRef(ev)
		bind()
		cancel()
		h.clearFailureState()
		h.agentActive = true
		return append(actions, h.publish(agentlifecycle.ReasonTurnStart, false)...)

	case "agent_end":
		if !h.rootSession {
			return actions
		}
		// OMP can emit duplicate or late end events while auto-retry is already
		// holding the pane at working. An unqualified duplicate end must not
		// cancel the retry hold and publish a false idle.
		if !h.agentActive {
			return actions
		}
		// A continuation is already scheduled, so this end is not a settle. Older
		// builds omit the field and fall through, which is why this compares
		// against an explicit true rather than testing for non-nil.
		if ev.WillContinue != nil && *ev.WillContinue {
			return actions
		}
		h.agentActive = false
		if message, retryable := ompRetryableError(ev); retryable {
			cancel()
			h.retryHoldActive = true
			h.failureBlocked = false
			h.failureMessage = message
			actions = append(actions, h.publish(agentlifecycle.ReasonProviderError, false)...)
			return append(actions, OmpAction{Timer: OmpActionSchedule, TimerName: "retry", DelayMS: h.retryGrace()})
		}
		cancel()
		h.clearFailureState()
		return append(actions, OmpAction{Timer: OmpActionSchedule, TimerName: "idle", DelayMS: h.idleDebounce()})

	case "idle_timer":
		return h.publish(agentlifecycle.ReasonTurnComplete, false)

	case "retry_timer":
		h.retryHoldActive = false
		h.failureBlocked = true
		return h.publish(agentlifecycle.ReasonProviderError, false)

	case "tool_approval_requested":
		if !h.rootSession && !activateRootSession() {
			return actions
		}
		activateBlocked(ompApprovalLabel(ev), agentlifecycle.ReasonPermissionRequest)
		return actions

	case "tool_approval_resolved":
		if !h.rootSession && !activateRootSession() {
			return actions
		}
		deactivateBlocked()
		return actions

	case "tool_execution_start":
		// Only the `ask` tool blocks. Every other tool is work already covered by
		// the lane agent_start opened, which is why this asset does not claim
		// tool_use.
		if ev.Tool != "ask" {
			return actions
		}
		if !h.rootSession && !activateRootSession() {
			return actions
		}
		activateBlocked(ompAskLabel(ev), agentlifecycle.ReasonQuestion)
		return actions

	case "tool_execution_end":
		if ev.Tool != "ask" {
			return actions
		}
		if !h.rootSession && !activateRootSession() {
			return actions
		}
		deactivateBlocked()
		return actions

	case "blocked":
		if !h.rootSession {
			return actions
		}
		if !ev.BlockedActive {
			deactivateBlocked()
			return actions
		}
		activateBlocked(ev.BlockedLabel, agentlifecycle.ReasonPermissionRequest)
		return actions

	case "session_shutdown":
		// Upstream cancels its pending timers here and reports nothing, and this
		// keeps both halves. session_shutdown is not an exit: OMP emits it for a
		// session swap as well, so releasing the lane on it would hand a live pane
		// back to screen detection in the middle of a run.
		if !h.rootSession {
			return actions
		}
		cancel()
		return actions
	}
	return actions
}

// desiredState is upstream's ladder, unchanged, and its order is load-bearing at
// every rung. An explicit block outranks a provider failure, because a human is
// being asked something; a provider failure outranks working; a retry hold reads
// as working, because OMP is still going to do the work.
func (h *OmpHandler) desiredState() (agentactivity.State, string) {
	switch {
	case h.blockedCount > 0:
		return agentactivity.StateBlocked, h.blockedMessage
	case h.failureBlocked:
		return agentactivity.StateBlocked, h.failureMessage
	case h.agentActive || h.retryHoldActive:
		return agentactivity.StateWorking, ""
	default:
		return agentactivity.StateIdle, ""
	}
}

// publish reports the desired lane unless it is an exact repeat.
//
// force exists for two callers, session_start and session_switch, which
// re-assert the lane even when it has not changed, because a reload replaces the
// extension mid-run and Sidecar has no record of what the previous instance
// reported.
func (h *OmpHandler) publish(reason agentlifecycle.ReasonCode, force bool) []OmpAction {
	state, message := h.desiredState()
	if !force && state == h.lastState && message == h.lastMessage {
		return nil
	}
	h.lastState, h.lastMessage = state, message
	return []OmpAction{{Kind: agentlifecycle.KindState, State: state, Reason: reason}}
}

func (h *OmpHandler) clearFailureState() {
	h.retryHoldActive = false
	h.failureBlocked = false
	h.failureMessage = ""
}

// updateSessionRef adopts the conversation reference the event carried.
func (h *OmpHandler) updateSessionRef(ev OmpEvent) {
	h.sessionPath = ""
	if ompAbsoluteSessionPath(ev.SessionPath) {
		h.sessionPath = ev.SessionPath
	}
	h.sessionID = ev.SessionID
}

// sessionActions emits the binding, or nothing when OMP has told us nothing to
// bind.
func (h *OmpHandler) sessionActions() []OmpAction {
	switch {
	case h.sessionPath != "":
		return []OmpAction{{Kind: agentlifecycle.KindSession, SessionPath: h.sessionPath}}
	case h.sessionID != "":
		return []OmpAction{{Kind: agentlifecycle.KindSession, SessionID: h.sessionID}}
	}
	return nil
}

// ompWindowsAbsolutePath matches the two shapes a Windows absolute path arrives
// in, C:\... and C:/....
var ompWindowsAbsolutePath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// ompAbsoluteSessionPath reports whether a session file path is worth reporting.
//
// This is upstream's OMP form, and it is the half of Herdr's two Pi-family
// assets that the Pi variant never received: that one accepts a session path
// only when it starts with "/", which silently discards every Windows path.
// Sidecar's Pi port adopted the fixed form; this is where it came from.
func ompAbsoluteSessionPath(path string) bool {
	if path == "" {
		return false
	}
	if path[0] == '/' {
		return true
	}
	return ompWindowsAbsolutePath.MatchString(path)
}

// ompRetryableErrorPattern is upstream's classifier, character for character.
//
// It decides whether a failed run is one OMP will retry (hold the pane at
// working) or one a human has to see (blocked). Keeping it verbatim is the
// point: the list is a record of which provider error strings OMP's own retry
// path recovers from, and narrowing it here would make Sidecar report a block
// OMP is about to clear by itself.
//
// It has no lookahead, no backreference and no named group, so RE2 accepts it
// unchanged and this is the same pattern the asset runs rather than an
// approximation of it. Only the case-insensitivity is spelled differently.
var ompRetryableErrorPattern = regexp.MustCompile(`(?i)overloaded|provider.?returned.?error|rate.?limit|too many requests|429|500|502|503|504|service.?unavailable|server.?error|internal.?error|network.?error|connection.?error|connection.?refused|connection.?lost|websocket.?closed|websocket.?error|other side closed|fetch failed|upstream.?connect|reset before headers|socket hang up|ended without|http2 request did not get a response|timed? out|timeout|terminated|retry delay`)

// ompRetryableError decides whether a finished run failed in a way OMP retries.
func ompRetryableError(ev OmpEvent) (string, bool) {
	if ev.StopReason != "error" {
		return "", false
	}
	if !ompRetryableErrorPattern.MatchString(ev.ErrorMessage) {
		return "", false
	}
	if ev.ErrorMessage == "" {
		return "retryable provider error", true
	}
	return ev.ErrorMessage, true
}

// ompApprovalLabel is upstream's label for a tool waiting on approval.
func ompApprovalLabel(ev OmpEvent) string {
	if ev.Reason != "" {
		return ev.Reason
	}
	tool := ev.Tool
	if tool == "" {
		tool = "Tool"
	}
	return tool + " approval"
}

// ompAskLabel is upstream's label for a turn blocked on the `ask` tool.
func ompAskLabel(ev OmpEvent) string {
	if ev.Question != "" {
		return ev.Question
	}
	return "waiting for user input"
}

// OmpReportArgs builds the exact CLI argv one action becomes.
//
// It mirrors buildArgs in the bundled asset. Both exist because the asset must
// construct argv in JavaScript at runtime, and the equivalence test compares the
// two lists element for element -- so this is the Go statement of the same
// contract, not a convenience wrapper. A timer instruction is not an argv and
// returns none; the runtime switches on the kind before it ever gets here.
//
// NEITHER VERB CARRIES --seq, and there is no sequence parameter to pass.
// `agent report-session` never had the flag. `agent report` has it and it is
// omitted, which is what its own help names as the right thing for a per-event
// hook process to do: the store assigns under the exclusive lock it already
// takes for the append (lifecyclestore.AppendNext), which is the only place the
// read and the write are atomic. Upstream numbers every message it sends, seeded
// from the clock; Sidecar's store bounds the field at MaxSequence and that seed
// is about 1600x over it, which is how every Pi report was silently rejected
// once already.
//
// The blocked label, the question text and the provider error string are all
// deliberately absent from every argv: they are unbounded text authored by a
// model or a provider, and nothing but lanes, bounded codes and conversation
// identifiers goes over this wire.
func OmpReportArgs(action OmpAction, sessionID string) []string {
	if action.Timer != "" {
		return nil
	}
	if action.Kind == agentlifecycle.KindSession {
		args := []string{"agent", "report-session", "--kind", OmpProvider, "--source", OmpSource}
		switch {
		case action.SessionPath != "":
			args = append(args, "--path", action.SessionPath)
		case action.SessionID != "":
			args = append(args, "--id", action.SessionID)
		}
		return args
	}
	args := []string{
		"agent", "report",
		"--source", OmpSource,
		"--source-version", OmpAssetVersion,
		"--provider", OmpProvider,
	}
	if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}
	args = append(args, "--state", string(action.State))
	return append(args, "--reason", string(action.Reason))
}

// Session returns the provider session id the handler has adopted, which the
// asset also carries on every state report.
func (h *OmpHandler) Session() string { return h.sessionID }

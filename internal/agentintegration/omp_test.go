package agentintegration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/agentsession"
)

// The OMP suite.
//
// It is in two halves, like Pi's. The first drives the mapping over the fixtures
// in testdata/omp, which are translations of Herdr's own herdr-agent-state.test.ts
// plus the branches that test does not reach -- see that directory's README for
// why the distinction between a fixture and a trace matters, and why this
// provider's capability entry says what it says. The second is the installer
// contract, which is the same contract every other adapter's suite pins,
// asserted again here because an adapter that satisfies it by accident is an
// adapter that stops satisfying it on the next edit.

// ompFixture builds a service against a temporary tree with omp on PATH and
// omp's agent directory already present, which is the state a machine is in
// after omp has been run once.
func ompFixture(t *testing.T, opts ...func(*Env)) (Service, Env, ompPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == OmpProvider {
				return filepath.Join(home, "bin", "omp"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "18.1.8" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	paths := ompPathsFor(env)
	if paths.AgentDir != "" {
		if err := os.MkdirAll(paths.AgentDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, paths
}

func withoutOmp(e *Env) {
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
}

func ompStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(OmpProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func ompApply(t *testing.T, s Service, act Action) Plan {
	t.Helper()
	p, err := s.Apply(OmpProvider, act)
	if err != nil {
		t.Fatalf("%s: %v", act, err)
	}
	return p
}

// ompFixtureColumns is the fixture layout, and it is the same list the node
// replay harness reads. Keeping the two literal about the three tri-state
// columns is the point: OMP's ctx.isIdle can be absent, ctx.hasUI can be absent,
// and agent_end's willContinue is absent on older builds, and "absent" is not
// false to any of the guards that read them.
const ompFixtureColumns = 14

// ompEvents parses a fixture into handler events.
func ompEvents(t *testing.T, name string) []OmpEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "omp", name))
	if err != nil {
		t.Fatal(err)
	}
	var events []OmpEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != ompFixtureColumns {
			t.Fatalf("malformed fixture row in %s (%d columns, want %d): %q", name, len(cols), ompFixtureColumns, line)
		}
		text := func(s string) string {
			if s == "-" {
				return ""
			}
			return s
		}
		tri := func(s string) *bool {
			if s == "-" {
				return nil
			}
			v := s == "true"
			return &v
		}
		events = append(events, OmpEvent{
			Type:          cols[1],
			Reason:        text(cols[2]),
			HasUI:         tri(cols[3]),
			Idle:          tri(cols[4]),
			SessionPath:   text(cols[5]),
			SessionID:     text(cols[6]),
			Tool:          text(cols[7]),
			Question:      text(cols[8]),
			BlockedActive: cols[9] == "true",
			BlockedLabel:  text(cols[10]),
			WillContinue:  tri(cols[11]),
			StopReason:    text(cols[12]),
			ErrorMessage:  text(cols[13]),
		})
	}
	if len(events) == 0 {
		t.Fatalf("%s has no rows", name)
	}
	return events
}

// ompReplay drives a fixture through the handler and returns what it produced.
func ompReplay(t *testing.T, name string) []OmpAction {
	t.Helper()
	var h OmpHandler
	var actions []OmpAction
	for _, ev := range ompEvents(t, name) {
		actions = append(actions, h.Handle(ev)...)
	}
	return actions
}

// ompLanes renders actions the way the assertions below read them: a lane name
// for a state report, "bind:" plus the reference for a binding, and a "#"-marked
// word for a timer instruction, which is the one thing this provider's mapping
// does that Pi's and Kilo's do not.
func ompLanes(actions []OmpAction) []string {
	var out []string
	for _, a := range actions {
		switch {
		case a.Timer == OmpActionCancel:
			out = append(out, "#cancel")
		case a.Timer == OmpActionSchedule:
			out = append(out, "#schedule:"+a.TimerName)
		case a.Kind == agentlifecycle.KindState:
			out = append(out, string(a.State)+"/"+string(a.Reason))
		case a.Kind == agentlifecycle.KindSession && a.SessionPath != "":
			out = append(out, "bind:path="+a.SessionPath)
		case a.Kind == agentlifecycle.KindSession:
			out = append(out, "bind:id="+a.SessionID)
		default:
			out = append(out, "?"+string(a.Kind))
		}
	}
	return out
}

func assertOmpLanes(t *testing.T, got []OmpAction, want ...string) {
	t.Helper()
	have := ompLanes(got)
	if !reflect.DeepEqual(have, want) {
		t.Fatalf("actions = %v, want %v", have, want)
	}
}

// TestOmpReloadPreservesWorkingState is upstream's shared Pi-family fixture
// (herdr-agent-state.test.ts:186), driven for the OMP module.
//
// A reload replaces this extension mid-run without emitting another agent_start,
// so the run's true state is read back from ctx rather than assumed idle. The
// report is forced, because Sidecar has no record of what the previous instance
// said.
func TestOmpReloadPreservesWorkingState(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "reload-preserves-working.tsv"),
		"working/session_start",
	)
}

// TestOmpTurnCompletionIsDebouncedAndNotPublishedByAgentEnd is the trap this
// provider has and Pi does not.
//
// Pi ends a turn on a second event, agent_settled. OMP has no such event at all:
// its registry ends a run on agent_end. So agent_end does not publish anything
// itself -- it arms a timer, and the timer's firing is what publishes idle. An
// asset that reported idle inline would announce a finished turn 250ms before
// OMP agrees it is finished, and would flicker the pane between two halves of one
// piece of work.
func TestOmpTurnCompletionIsDebouncedAndNotPublishedByAgentEnd(t *testing.T) {
	var h OmpHandler
	yes, no := true, false
	h.Handle(OmpEvent{Type: "session_start", HasUI: &yes, Idle: &yes})
	h.Handle(OmpEvent{Type: "agent_start", HasUI: &yes, Idle: &no})

	end := h.Handle(OmpEvent{Type: "agent_end", HasUI: &yes, Idle: &yes, StopReason: "stop"})
	assertOmpLanes(t, end, "#cancel", "#schedule:idle")
	for _, action := range end {
		if action.Kind == agentlifecycle.KindState {
			t.Fatalf("agent_end published a lane directly: %v", ompLanes(end))
		}
		if action.Timer == OmpActionSchedule && action.DelayMS != OmpIdleDebounceMS {
			t.Fatalf("the idle timer was armed for %dms, want the %dms debounce", action.DelayMS, OmpIdleDebounceMS)
		}
	}
	assertOmpLanes(t, h.Handle(OmpEvent{Type: "idle_timer"}), "idle/turn_complete")
}

// TestOmpDoesNotSettleAScheduledContinuation is upstream's own OMP fixture
// (herdr-agent-state.test.ts:501, "keeps working when a turn ends with a
// scheduled continuation", which cites OMP issue #2851).
//
// An agent_end carrying willContinue true is not a settle: OMP has already
// scheduled the next loop. The comparison is against an explicit true rather
// than truthiness, because an older build omits the field and must still settle.
func TestOmpDoesNotSettleAScheduledContinuation(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "will-continue-does-not-settle.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		// The willContinue end produces nothing at all: no cancel, no schedule.
		"#cancel", "#schedule:idle",
		"idle/turn_complete",
	)
}

// TestOmpIgnoresADuplicateEndWhileInactive is the other agent_end guard, and it
// is upstream's reason for having it: OMP emits duplicate and late end events
// while auto-retry is already holding the pane at working, and an unqualified
// duplicate would cancel that hold and publish a false idle.
func TestOmpIgnoresADuplicateEndWhileInactive(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "duplicate-agent-end-is-ignored.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		"#cancel", "#schedule:idle",
		// The second agent_end arrives with agentActive already false and emits
		// nothing, which is why there is only one schedule here.
		"idle/turn_complete",
	)
}

// TestOmpHoldsARetryableProviderErrorAtWorking is the retry lane.
//
// OMP retries provider errors by itself, so a failure it is about to clear is
// still work in progress. The pane stays at working for the grace period -- which
// is why the first publish is suppressed, the lane not having changed -- and only
// a failure still outstanding when the timer fires becomes a block.
func TestOmpHoldsARetryableProviderErrorAtWorking(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "retry-hold-then-block.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		// agent_end classifies the error as retryable, holds the lane at working
		// (an exact repeat, so nothing is published) and arms the grace timer.
		"#cancel", "#schedule:retry",
		"blocked/provider_error",
	)

	var h OmpHandler
	yes, no := true, false
	h.Handle(OmpEvent{Type: "session_start", HasUI: &yes, Idle: &yes})
	h.Handle(OmpEvent{Type: "agent_start", HasUI: &yes, Idle: &no})
	for _, action := range h.Handle(OmpEvent{
		Type: "agent_end", HasUI: &yes, Idle: &yes,
		StopReason: "error", ErrorMessage: "upstream connect error: 503 service unavailable",
	}) {
		if action.Timer == OmpActionSchedule && action.DelayMS != OmpRetryGraceMS {
			t.Fatalf("the retry timer was armed for %dms, want the %dms grace", action.DelayMS, OmpRetryGraceMS)
		}
	}
}

// TestOmpTreatsAnUnrecognisedFailureAsACompletedTurn is the other half of the
// classifier. A failure OMP will not retry is a turn that is over, so it takes
// the ordinary debounced idle path rather than the grace period.
func TestOmpTreatsAnUnrecognisedFailureAsACompletedTurn(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "a-plain-failure-is-not-a-retry.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		"#cancel", "#schedule:idle",
		"idle/turn_complete",
	)
}

// TestOmpRetryClassifierIsUpstreamsAndRunsOnRE2 is the assertion that the Go
// mirror runs upstream's pattern rather than an approximation of it.
//
// The list is a record of which provider error strings OMP's own retry path
// recovers from, so narrowing it would make Sidecar report a block OMP is about
// to clear by itself. Every string below is taken from a branch of the pattern.
func TestOmpRetryClassifierIsUpstreamsAndRunsOnRE2(t *testing.T) {
	for _, message := range []string{
		"Overloaded", "provider returned error", "rate-limit exceeded", "Too Many Requests",
		"429", "502 bad gateway", "service unavailable", "internal error", "connection refused",
		"websocket closed", "other side closed", "fetch failed", "upstream connect error",
		"reset before headers", "socket hang up", "ended without a response",
		"http2 request did not get a response", "timed out", "TIMEOUT", "terminated",
		"retry delay of 30s",
	} {
		if _, retryable := ompRetryableError(OmpEvent{StopReason: "error", ErrorMessage: message}); !retryable {
			t.Errorf("%q is not classified as retryable; upstream's pattern says it is", message)
		}
	}
	for _, message := range []string{
		"the model refused the request", "invalid api key", "context length exceeded", "",
	} {
		if _, retryable := ompRetryableError(OmpEvent{StopReason: "error", ErrorMessage: message}); retryable && message != "" {
			t.Errorf("%q is classified as retryable, which would hold a pane at working forever", message)
		}
	}
	// A stop reason that is not "error" is never retryable, whatever the message
	// field happens to hold.
	if _, retryable := ompRetryableError(OmpEvent{StopReason: "stop", ErrorMessage: "overloaded"}); retryable {
		t.Fatal("a completed turn was classified as a retryable failure")
	}
}

// TestOmpBlockedOutranksAProviderFailure drives every rung of upstream's ladder
// against each other. The ordering is load-bearing rather than stylistic: a
// human being asked something outranks a provider that failed, which outranks
// work in progress, which outranks idle.
func TestOmpBlockedOutranksAProviderFailure(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "blocked-outranks-provider-error.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		"#cancel", "#schedule:retry",
		"blocked/provider_error",
		// The approval outranks the failure, so the lane stays blocked but its
		// message changes, which is what stops the repeat suppression firing.
		"#cancel", "blocked/permission_request",
		// Resolving the approval does NOT return the pane to working: the provider
		// failure is still outstanding underneath it.
		"blocked/permission_resolved",
	)
}

// TestOmpApprovalEventsDriveTheBlockedLane is the sharpest difference from the
// Pi port. Pi ships no permission system, so its blocked lane is structurally
// unreachable; OMP's tool_approval_requested and tool_approval_resolved are
// typed, first-class events.
func TestOmpApprovalEventsDriveTheBlockedLane(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "approval-blocks-and-unblocks.tsv"),
		"bind:path=/tmp/omp-a.jsonl", "idle/session_start",
		"bind:path=/tmp/omp-a.jsonl", "#cancel", "working/turn_start",
		"#cancel", "blocked/permission_request",
		"working/permission_resolved",
	)
}

// TestOmpOnlyTheAskToolBlocksATurn pins the tool_execution_* pair's one
// condition. Every other tool is work already covered by the lane agent_start
// opened, which is why this asset does not claim tool_use.
func TestOmpOnlyTheAskToolBlocksATurn(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "ask-tool-blocks-on-a-question.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		// The `read` tool's start and end produce nothing at all.
		"#cancel", "blocked/question",
		"working/permission_resolved",
	)
	// A question with no readable text still blocks, on upstream's fallback label.
	assertOmpLanes(t, ompReplay(t, "ask-without-a-question-still-blocks.tsv"),
		"idle/session_start",
		"#cancel", "blocked/question",
	)
}

// TestOmpSessionSwitchRebindsAndResets pins the event Pi does not have.
//
// A switch is a different conversation in the same process, so every counter
// from the previous one is dropped rather than carried: an outstanding approval
// block from the abandoned session must not hold the new one at blocked.
func TestOmpSessionSwitchRebindsAndResets(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "session-switch-resets-the-session.tsv"),
		"bind:path=/tmp/omp-a.jsonl", "idle/session_start",
		"bind:path=/tmp/omp-a.jsonl", "#cancel", "working/turn_start",
		"#cancel", "blocked/permission_request",
		// The switch binds the new transcript and drops the block with it.
		"bind:path=/tmp/omp-b.jsonl", "#cancel", "idle/session_change",
	)
}

// TestOmpReportsNothingInAHeadlessSession is the hasUI gate, and it is the one
// place this port deliberately does not copy the Pi asset's choice.
//
// Pi gates on ctx.mode because an RPC Pi session reports hasUI true while being
// headless. OMP computes hasUI as `isInteractive || mode === "rpc-ui"`
// (src/main.ts:1830), so print, json and plain rpc are already excluded, and
// upstream's OMP asset uses hasUI. The gate is re-checked on every handler that
// can adopt a session, so a headless invocation can never latch.
func TestOmpReportsNothingInAHeadlessSession(t *testing.T) {
	if got := ompReplay(t, "headless-session-is-ignored.tsv"); len(got) != 0 {
		t.Fatalf("a headless session produced %v; it must claim no pane", ompLanes(got))
	}
}

// TestOmpUnknownUIOrIdlenessIsNotAClaim keeps the two tri-states honest. An
// absent hasUI is not a UI, and an absent isIdle is not a running turn.
func TestOmpUnknownUIOrIdlenessIsNotAClaim(t *testing.T) {
	var h OmpHandler
	if got := h.Handle(OmpEvent{Type: "session_start", Idle: boolPtr(false)}); len(got) != 0 {
		t.Fatalf("a session with no hasUI produced %v", ompLanes(got))
	}
	assertOmpLanes(t, ompReplay(t, "unknown-idleness-is-not-working.tsv"), "idle/session_start")
}

func boolPtr(v bool) *bool { return &v }

func ompContains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestOmpBindsAWindowsSessionPath is upstream's own OMP test
// (herdr-agent-state.test.ts:232), and it is the half of Herdr's two Pi-family
// assets that the Pi variant never received: that one accepts a path only when
// it starts with "/", silently discarding every Windows session.
func TestOmpBindsAWindowsSessionPath(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "windows-session-path-is-bound.tsv"),
		`bind:path=C:\Users\User\.omp\agent\sessions\s.jsonl`,
		"idle/session_start",
	)
	for _, value := range []string{
		"/tmp/omp-session.jsonl",
		`C:\Users\User\.omp\agent\sessions\omp-session.jsonl`,
		"C:/Users/User/.omp/agent/sessions/omp-session.jsonl",
	} {
		if !ompAbsoluteSessionPath(value) {
			t.Errorf("%q is not accepted as an absolute session path", value)
		}
	}
	for _, value := range []string{"relative/omp-session.jsonl", "", "C:relative"} {
		if ompAbsoluteSessionPath(value) {
			t.Errorf("%q is accepted as an absolute session path", value)
		}
	}
}

// TestOmpFallsBackToTheSessionIdAndDiscardsARelativePath is upstream's :240 plus
// the fallback the two accessors exist for.
func TestOmpFallsBackToTheSessionIdAndDiscardsARelativePath(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "session-id-only-binds-by-id.tsv"),
		"bind:id=omp-id-only", "idle/session_start")
	assertOmpLanes(t, ompReplay(t, "relative-session-path-is-discarded.tsv"),
		"idle/session_start")
}

// TestOmpAdoptsASessionItsSessionStartMissed is upstream's activateRootSession
// being re-entered from four later handlers, and it is what makes the extension
// work when it is loaded into a session that has already started.
//
// It also pins the one deliberate difference in this branch: upstream's
// activateRootSession reports the session and agent_start then reports it again
// on the same event. On Herdr's socket that is one extra frame; here it would be
// one extra subprocess with byte-identical argv, so a binding is emitted at most
// once per event. The per-turn re-binding is kept in full, which the fixture
// above with two agent_starts shows.
func TestOmpAdoptsASessionItsSessionStartMissed(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "agent-start-adopts-an-unseen-session.tsv"),
		"bind:path=/tmp/omp-late.jsonl", "#cancel", "working/turn_start")
}

// TestOmpSessionShutdownCancelsWithoutReporting pins a silence.
//
// session_shutdown is not an exit: OMP emits it for a session swap as well, and
// releasing the lane on one would hand a live pane back to screen detection in
// the middle of a run. The asset subscribes only to cancel its pending timers,
// and `process_exit` is not claimed in the capability entry.
func TestOmpSessionShutdownCancelsWithoutReporting(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "session-shutdown-cancels-without-reporting.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		"#cancel", "#schedule:idle",
		"#cancel",
	)
}

// TestOmpBusChannelStillDrivesTheLadder keeps upstream's cooperative channel
// working. It is not what OMP's blocked lane rests on -- the typed approval
// events are -- but removing a branch upstream has is not a port.
func TestOmpBusChannelStillDrivesTheLadder(t *testing.T) {
	assertOmpLanes(t, ompReplay(t, "bus-channel-blocks-and-unblocks.tsv"),
		"idle/session_start",
		"#cancel", "working/turn_start",
		"#cancel", "blocked/permission_request",
		"working/permission_resolved",
	)
}

// TestOmpSuppressesAnExactRepeat is the property the queue depth depends on: a
// state machine that republished an unchanged lane on every event would make the
// serialized queue grow with the event rate rather than the state-change rate.
func TestOmpSuppressesAnExactRepeat(t *testing.T) {
	var h OmpHandler
	yes, no := true, false
	h.Handle(OmpEvent{Type: "session_start", HasUI: &yes, Idle: &yes})
	first := h.Handle(OmpEvent{Type: "agent_start", HasUI: &yes, Idle: &no})
	if len(first) == 0 {
		t.Fatal("the first turn start reported nothing")
	}
	second := h.Handle(OmpEvent{Type: "agent_start", HasUI: &yes, Idle: &no})
	for _, action := range second {
		if action.Kind == agentlifecycle.KindState {
			t.Fatalf("a second agent_start republished the same lane: %v", ompLanes(second))
		}
	}
}

// TestOmpReportArgsCarryTheAssetVersionAndNoUnboundedText is the argv contract.
//
// Authority stays scoped to a source at a version, so every state report carries
// one; without it every stored record claims an unknown version and an outdated
// asset can never be detected. And none of the three unbounded strings this
// mapping holds -- a model-authored question, an approval label, a provider's
// error text -- ever leaves the process.
func TestOmpReportArgsCarryTheAssetVersionAndNoUnboundedText(t *testing.T) {
	args := OmpReportArgs(OmpAction{
		Kind:   agentlifecycle.KindState,
		State:  agentactivity.StateBlocked,
		Reason: agentlifecycle.ReasonQuestion,
	}, "omp-session")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--source-version "+OmpAssetVersion) {
		t.Fatalf("argv does not carry the asset version: %v", args)
	}
	if !strings.Contains(joined, "--session-id omp-session") {
		t.Fatalf("argv lost the session id: %v", args)
	}
	if strings.Contains(joined, "--detail") {
		t.Fatalf("argv carries a detail field; no unbounded text may be transmitted: %v", args)
	}
	// NO SEQUENCE, on either verb. The store assigns one under the lock it
	// already holds. Upstream seeds a counter from the clock, which is about
	// 1600x over Sidecar's MaxSequence and had every Pi report silently rejected
	// once already.
	if strings.Contains(joined, "--seq") {
		t.Fatalf("a state report carries --seq; the store must assign it: %v", args)
	}

	bind := OmpAction{Kind: agentlifecycle.KindSession, SessionPath: "/tmp/s.jsonl"}
	bindArgs := strings.Join(OmpReportArgs(bind, "omp-session"), " ")
	if strings.Contains(bindArgs, "--seq") {
		t.Fatalf("the binding argv carries --seq, which report-session would reject as usage: %s", bindArgs)
	}
	if !strings.Contains(bindArgs, "--kind "+OmpProvider) || !strings.Contains(bindArgs, "--path /tmp/s.jsonl") {
		t.Fatalf("the binding argv is not a report-session call: %s", bindArgs)
	}
	// A timer instruction is not an argv and must never become one.
	if got := OmpReportArgs(OmpAction{Timer: OmpActionSchedule, TimerName: "idle", DelayMS: 250}, ""); got != nil {
		t.Fatalf("a timer instruction produced argv %v", got)
	}

	// The label fields exist for suppression and for nothing else. This drives a
	// block whose label is a whole sentence and asserts none of it reaches an
	// argv.
	var h OmpHandler
	yes := true
	h.Handle(OmpEvent{Type: "session_start", HasUI: &yes, Idle: &yes})
	for _, action := range h.Handle(OmpEvent{
		Type: "tool_execution_start", HasUI: &yes, Tool: "ask",
		Question: "should I force-push over the release branch",
	}) {
		for _, arg := range OmpReportArgs(action, h.Session()) {
			if strings.Contains(arg, "force-push") {
				t.Fatalf("the question text reached an argv: %v", OmpReportArgs(action, h.Session()))
			}
		}
	}
}

// TestOmpCapabilityIsRegisteredForTheBundledSource ties the asset to the
// registry the resolver actually consults. An asset with no capability entry
// would install, report, and be ignored.
//
// The tier is asserted at exactly what the port has earned and no higher.
func TestOmpCapabilityIsRegisteredForTheBundledSource(t *testing.T) {
	cap, ok := agentlifecycle.CapabilityForSource(OmpSource)
	if !ok {
		t.Fatalf("no capability registered for %q", OmpSource)
	}
	if cap.Provider != OmpProvider {
		t.Fatalf("capability provider = %q", cap.Provider)
	}
	if cap.AssetVersion != OmpAssetVersion {
		t.Fatalf("registry asset version %q != bundled %q; every report would look outdated",
			cap.AssetVersion, OmpAssetVersion)
	}
	// Session identity is the only transition the entry may claim without
	// traces, and it is the one the binding itself proves. Everything else the
	// shipped asset reports -- work start, turn completion, both blocked lanes,
	// the unblock -- is deliberately unclaimed until a capture of a released OMP
	// earns it, which is the same order pi, kilo and kimi went in.
	if !cap.Covers(agentlifecycle.TransitionSessionIdentity) {
		t.Fatal("omp does not claim session_identity, which its binding is")
	}
	if cap.CoversFullLifecycle() {
		t.Fatal("omp claims full lifecycle coverage, which no trace here supports")
	}
	tier, reason := cap.TierFor(agentlifecycle.StatusCurrent, true)
	switch cap.Evidence {
	case agentlifecycle.EvidenceDocsOnly:
		if tier != agentlifecycle.TierSessionIdentity {
			t.Fatalf("omp exercises %q (%s) on docs-only evidence, want session-identity", tier, reason)
		}
		if cap.TestedProviderRange != "" {
			t.Fatalf("omp records a tested provider range %q with no traces behind it", cap.TestedProviderRange)
		}
	case agentlifecycle.EvidenceRealTrace:
		if tier != agentlifecycle.TierAdvisory {
			t.Fatalf("omp exercises %q (%s) with traces, want advisory: it is the ceiling", tier, reason)
		}
		if cap.TestedProviderRange == "" {
			t.Fatal("omp claims traces but records no tested provider range, so they are attached to no version")
		}
	default:
		t.Fatalf("omp records evidence %q; an entry with none falls out to screen fallback", cap.Evidence)
	}
	// These stay unclaimed whatever the tier does, and the list does not shrink
	// when a tier goes up. process_exit is unclaimed because session_shutdown is
	// not an exit; cancelled because upstream's mapping never reads the stop
	// reason that would distinguish it; tool_use because tool use is a refinement
	// of work_start; subagent because nothing in the event stream describes one.
	for _, absent := range []agentlifecycle.Transition{
		agentlifecycle.TransitionToolUse,
		agentlifecycle.TransitionProcessExit,
		agentlifecycle.TransitionCancelled,
		agentlifecycle.TransitionSubagent,
	} {
		if cap.Covers(absent) {
			t.Fatalf("omp claims %q, which the shipped asset does not report", absent)
		}
	}
}

// TestBundledOmpAssetBehavesLikeTheHandler is the real asset-to-handler
// equivalence check.
//
// It drives the asset's actual mapping under node over the same fixtures and
// requires the identical ordered action list. Timer instructions are compared
// too, rendered as "#"-prefixed markers on both sides, because this provider's
// mapping has a clock in it and the scheduling decision would otherwise be the
// one thing nothing compares between the two implementations.
func TestBundledOmpAssetBehavesLikeTheHandler(t *testing.T) {
	node := requireNode(t, "the shipped OMP asset's mapping against the checked-in fixtures")

	entries, err := os.ReadDir(filepath.Join("testdata", "omp"))
	if err != nil {
		t.Fatal(err)
	}
	// Every fixture in the directory is driven, rather than a list kept in step
	// by hand: a fixture nobody added to a list is a branch nobody compares.
	silent := map[string]bool{"headless-session-is-ignored.tsv": true}
	var drove int
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".tsv") {
			continue
		}
		drove++
		t.Run(entry.Name(), func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("testdata", "omp", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, "replay-harness.mjs", path)
			cmd.Dir = filepath.Join("assets", "omp")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("running the asset harness: %v\n%s", err, stderr.String())
			}

			var fromAsset [][]string
			if err := json.Unmarshal(out, &fromAsset); err != nil {
				t.Fatalf("harness output is not JSON: %q (%v)", out, err)
			}
			fromHandler := ompHandlerArgs(t, entry.Name())

			if len(fromAsset) != len(fromHandler) {
				t.Fatalf("the asset emitted %d actions, the handler %d:\nasset:   %v\nhandler: %v",
					len(fromAsset), len(fromHandler), fromAsset, fromHandler)
			}
			for i := range fromHandler {
				if strings.Join(fromAsset[i], " ") != strings.Join(fromHandler[i], " ") {
					t.Fatalf("action %d differs:\nasset:   %v\nhandler: %v", i, fromAsset[i], fromHandler[i])
				}
			}
			// A fixture that produces nothing on both sides proves nothing --
			// unless producing nothing is the assertion, which is what the
			// headless case is.
			if len(fromHandler) == 0 && !silent[entry.Name()] {
				t.Fatal("neither produced any action; this fixture proves nothing")
			}
			if len(fromHandler) != 0 && silent[entry.Name()] {
				t.Fatalf("the headless fixture produced %v; a session with no UI must claim no pane", fromHandler)
			}
		})
	}
	if drove < len(silent)+1 {
		t.Fatalf("only %d fixtures were driven", drove)
	}
}

// ompHandlerArgs replays a fixture through the Go handler and renders each
// action the way the node harness renders it.
func ompHandlerArgs(t *testing.T, fixture string) [][]string {
	t.Helper()
	var h OmpHandler
	var out [][]string
	for _, ev := range ompEvents(t, fixture) {
		for _, action := range h.Handle(ev) {
			switch action.Timer {
			case OmpActionCancel:
				out = append(out, []string{"#cancel"})
			case OmpActionSchedule:
				out = append(out, []string{"#schedule", action.TimerName, itoa(action.DelayMS)})
			default:
				out = append(out, OmpReportArgs(action, h.Session()))
			}
		}
	}
	return out
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

// TestTheOmpAssetExportsOnlyPluginFactories guards a failure mode no Go test can
// see. OMP's loader takes a module's default export and drops the module when it
// is not a function -- silently, with no error anywhere. The extension then
// installs cleanly, loads, and reports nothing at all.
func TestTheOmpAssetExportsOnlyPluginFactories(t *testing.T) {
	node := requireNode(t, "the OMP asset's export surface, which OMP silently drops the whole module for")
	cmd := exec.Command(node, "exports-harness.mjs")
	cmd.Dir = filepath.Join("assets", "omp")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("inspecting the asset's exports: %v\n%s", err, stderr.String())
	}
	var surface struct {
		Names         []string `json:"names"`
		NonFunctions  []string `json:"nonFunctions"`
		DefaultIsFunc bool     `json:"defaultIsFunction"`
		DefaultName   string   `json:"defaultName"`
	}
	if err := json.Unmarshal(out, &surface); err != nil {
		t.Fatalf("harness output is not JSON: %q", out)
	}
	if !surface.DefaultIsFunc {
		t.Fatal("the default export is not a function; OMP would import the module and never call it")
	}
	if len(surface.NonFunctions) != 0 {
		t.Fatalf("non-function exports %v; OpenCode rejects a whole module for this and the assets "+
			"hold to one export convention", surface.NonFunctions)
	}
	if len(surface.Names) != 1 || surface.Names[0] != "default" {
		t.Fatalf("exports = %v, want exactly [default]", surface.Names)
	}
	if surface.DefaultName != "SidecarLifecycle" {
		t.Fatalf("the default export is named %q; the pure mapping hangs off it by name", surface.DefaultName)
	}
}

// ompRuntime is the ordering harness's report of one real run of the asset.
type ompRuntime struct {
	Order     []string            `json:"order"`
	Argv      map[string][]string `json:"argv"`
	Events    []string            `json:"events"`
	BusEvents []string            `json:"busEvents"`
	ElapsedMS int                 `json:"elapsedMs"`
}

func ompRunOrderingHarness(t *testing.T, node string) ompRuntime {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(node, "ordering-harness.mjs",
		filepath.Join(dir, "sidecar-stub"),
		filepath.Join(dir, "order.log"),
		filepath.Join(dir, "argv"))
	cmd.Dir = filepath.Join("assets", "omp")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the ordering harness: %v\n%s", err, stderr.String())
	}
	var result ompRuntime
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("harness output is not JSON: %q (%v)", out, err)
	}
	return result
}

// TestTheOmpAssetSerializesReportsAndBindsFirst pins the runtime properties the
// pure mapping cannot show.
//
// Each report is a subprocess taking an exclusive lock on an append-only store
// that enforces a strictly increasing sequence per run, so spawning them
// concurrently assigns sequences in order and delivers them out of order -- and
// the store correctly rejects the loser. The stub inverts the exit order, so
// serialized and concurrent produce different recorded orders and the assertion
// cannot pass by luck. It also pins that the binding lands before the first
// state report, and that the debounced idle really travels through a timer the
// mapping armed rather than being published inline.
func TestTheOmpAssetSerializesReportsAndBindsFirst(t *testing.T) {
	node := requireNode(t, "that the shipped OMP asset serializes its reports and binds the session first")
	result := ompRunOrderingHarness(t, node)

	want := []string{
		"session",
		"working-session_start",
		"blocked-permission_request",
		"working-permission_resolved",
		"idle-turn_complete",
	}
	if !reflect.DeepEqual(result.Order, want) {
		t.Fatalf("reports were delivered in order %v, want %v.\n"+
			"That list reversed is the signature of concurrent spawns: the stub inverts the exit order, so "+
			"that is what unserialized delivery produces and it is exactly what the store rejects. A first "+
			"element that is not \"session\" means a state report raced the binding it depends on, and a "+
			"missing final idle means the debounce timer never fired.",
			result.Order, want)
	}
}

// TestTheOmpAssetSubscribesToTheEventsItClaimsTo closes the gap every other test
// in this file leaves open.
//
// The replay harness drives mapEvent and buildArgs directly, so the asset's
// runtime wiring is untested by it: a typo in pi.on("agent_start"), a swap of
// getSessionFile for getSessionId inside readCtx, a lastAssistantError that
// stopped scanning the message list, or a schedule action that armed nothing
// would each pass the whole suite while the shipped extension silently never
// reported a turn correctly. This runs the real factory against a stub host and
// asserts what it registered and what it actually spawned.
func TestTheOmpAssetSubscribesToTheEventsItClaimsTo(t *testing.T) {
	node := requireNode(t, "the OMP asset's own subscriptions and the argv it spawns")
	result := ompRunOrderingHarness(t, node)

	// The typed registry, exactly. An extra name here is a claim the asset does
	// not have evidence for; a missing one is a lane that never gets reported.
	// agent_settled is absent because OMP has no such event; turn_start and
	// turn_end are absent because a "turn" here is one provider round trip;
	// auto_retry_start and auto_retry_end are absent deliberately, because the
	// provider half is upstream's and upstream classifies the error instead.
	sort.Strings(result.Events)
	wantEvents := []string{
		"agent_end", "agent_start", "session_shutdown", "session_start", "session_switch",
		"tool_approval_requested", "tool_approval_resolved",
		"tool_execution_end", "tool_execution_start",
	}
	if !reflect.DeepEqual(result.Events, wantEvents) {
		t.Fatalf("the asset subscribed to %v, want exactly %v", result.Events, wantEvents)
	}
	if !reflect.DeepEqual(result.BusEvents, []string{"sidecar:blocked"}) {
		t.Fatalf("the asset subscribed to bus channels %v, want exactly [sidecar:blocked]", result.BusEvents)
	}

	// The binding carries the session FILE, because a path names the exact
	// transcript a restore would resume where an id alone does not. The harness
	// hands ctx a path and a different id, so a readCtx that read the wrong
	// accessor would bind "omp-order" by --id and this would say so.
	wantSession := []string{
		"agent", "report-session", "--kind", "omp", "--source", "sidecar.omp.extension",
		"--path", "/tmp/omp-order.jsonl",
	}
	if got := result.Argv["session"]; !reflect.DeepEqual(got, wantSession) {
		t.Fatalf("the session process was spawned with %v, want %v", got, wantSession)
	}

	// Every state report, in full. There is nothing to strip: no verb carries
	// --seq. `working` on the first one is the isIdle read: the harness's ctx
	// reports isIdle() === false, so a readCtx that dropped idle would publish
	// `idle` here and never say a turn was running.
	for _, tc := range []struct {
		label string
		state string
		rest  []string
	}{
		{"working-session_start", "working", []string{"--reason", "session_start"}},
		{"blocked-permission_request", "blocked", []string{"--reason", "permission_request"}},
		{"working-permission_resolved", "working", []string{"--reason", "permission_resolved"}},
		{"idle-turn_complete", "idle", []string{"--reason", "turn_complete"}},
	} {
		argv := result.Argv[tc.label]
		want := append([]string{
			"agent", "report",
			"--source", "sidecar.omp.extension",
			"--source-version", OmpAssetVersion,
			"--provider", "omp",
			"--session-id", "omp-order",
			"--state", tc.state,
		}, tc.rest...)
		if !reflect.DeepEqual(argv, want) {
			t.Fatalf("the %s report was spawned with %v, want %v", tc.label, argv, want)
		}
	}
}

// TestTheOmpAssetFailsOpen checks the properties that keep a reporting failure
// from ever becoming the agent's problem.
func TestTheOmpAssetFailsOpen(t *testing.T) {
	asset := OmpAsset()
	if asset == "" {
		t.Fatal("the bundled asset is empty")
	}
	if !strings.HasPrefix(asset, Marker((OmpAdapter{}).asset())) {
		t.Fatal("the asset does not open with the marker the installer identifies it by")
	}
	if !strings.Contains(asset, "SIDECAR_MANAGED_SHELL") {
		t.Fatal("the asset does not check the managed-shell cue, so it would spawn outside Sidecar")
	}
	if !strings.Contains(asset, "SIDECAR_BIN") {
		t.Fatal("the asset does not use the published Sidecar binary path")
	}
	if !strings.Contains(asset, `stdio: "ignore"`) {
		t.Fatal("the asset does not silence report output; it would appear in the agent's own terminal")
	}
	if !strings.Contains(asset, "REPORT_TIMEOUT_MS") {
		t.Fatal("the asset has no per-report timeout; one hung subprocess would stall the queue forever")
	}
	if !strings.Contains(asset, "queue = queue.then(") {
		t.Fatal("the asset does not chain reports onto a queue; concurrent spawns lose reports to the store's sequence check")
	}
	if !strings.Contains(asset, `child.on("exit"`) {
		t.Fatal("the asset does not wait for a report process to exit, so the chain does not order deliveries")
	}
	if strings.Contains(asset, "detached: true") {
		t.Fatal("reports must not be detached; the queue depends on observing exit")
	}
	// A pending report timer must never be the reason a finished OMP process
	// stays alive.
	if !strings.Contains(asset, "timer.unref?.()") {
		t.Fatal("the asset does not unref its timers; a pending debounce would hold OMP's event loop open")
	}
	// No shell composition anywhere: every value reaches the CLI as its own argv
	// element.
	for _, forbidden := range []string{"exec(", "shell: true", "/bin/sh", "child_process.exec"} {
		if strings.Contains(asset, forbidden) {
			t.Fatalf("the asset uses %q; provider data must never be shell-composed", forbidden)
		}
	}
	// Herdr's transport must not have come along with the mapping. Claiming to be
	// Herdr is what the parity plan's first decision refuses.
	for _, forbidden := range []string{"HERDR_ENV", "HERDR_SOCKET_PATH", "HERDR_PANE_ID", "node:net"} {
		if strings.Contains(asset, forbidden) {
			t.Fatalf("the asset still references %q; the transport half is Sidecar's", forbidden)
		}
	}
	if !strings.Contains(asset, `const BLOCKED_CHANNEL = "sidecar:blocked"`) {
		t.Fatal("the blocked channel is not Sidecar's own namespace")
	}
	if strings.Contains(asset, `on("herdr:blocked"`) {
		t.Fatal("the asset subscribes to Herdr's blocked channel")
	}
}

// TestOmpAgentDirIsResolvedTheWayOmpResolvesIt pins the three directory facts
// that separate OMP from Pi, all read from OMP 18.1.8's own
// @oh-my-pi/pi-utils/src/dirs.ts.
//
// PI_CONFIG_DIR overrides the NAME of the config directory under $HOME and
// defaults to ".omp"; a named profile inserts /profiles/<name> AND makes OMP
// ignore PI_CODING_AGENT_DIR entirely; and PI_CODING_AGENT_DIR is path.resolve'd
// rather than tilde-expanded, so a non-absolute value binds to a cwd Sidecar
// cannot know and is refused rather than guessed.
func TestOmpAgentDirIsResolvedTheWayOmpResolvesIt(t *testing.T) {
	home := "/home/u"
	for _, tc := range []struct {
		name      string
		env       Env
		want      string
		wantRoots []string
		// rootsAreWider marks the one case where the trust boundary lists more
		// directories than the installer writes to, because it reads the
		// environment through a func(string) string and cannot tell an empty
		// OMP_PROFILE from an absent one.
		rootsAreWider bool
	}{
		{
			name:      "default",
			env:       Env{Home: home},
			want:      "/home/u/.omp/agent",
			wantRoots: []string{"/home/u/.omp/agent/sessions"},
		},
		{
			name:      "config dir name override",
			env:       Env{Home: home, OmpConfigDir: ".omp-work"},
			want:      "/home/u/.omp-work/agent",
			wantRoots: []string{"/home/u/.omp-work/agent/sessions"},
		},
		{
			name:      "absolute agent dir override",
			env:       Env{Home: home, PiAgentDir: "/opt/omp/agent"},
			want:      "/opt/omp/agent",
			wantRoots: []string{"/opt/omp/agent/sessions"},
		},
		{
			// path.resolve does not treat a leading tilde specially, so this is a
			// relative path to OMP and Sidecar cannot know what it resolves against.
			name: "tilde agent dir override is unknowable",
			env:  Env{Home: home, PiAgentDir: "~/elsewhere/agent"},
			want: "",
		},
		{
			name: "relative agent dir override is unknowable",
			env:  Env{Home: home, PiAgentDir: "relative/agent"},
			want: "",
		},
		{
			name:      "a named profile ignores the agent dir override",
			env:       Env{Home: home, PiAgentDir: "/opt/omp/agent", OmpProfile: "work", OmpProfileSet: true},
			want:      "/home/u/.omp/profiles/work/agent",
			wantRoots: []string{"/home/u/.omp/profiles/work/agent/sessions"},
		},
		{
			name:          "PI_PROFILE is the legacy fallback",
			env:           Env{Home: home, PiProfile: "legacy"},
			want:          "/home/u/.omp/profiles/legacy/agent",
			wantRoots:     []string{"/home/u/.omp/profiles/legacy/agent/sessions", "/home/u/.omp/agent/sessions"},
			rootsAreWider: true,
		},
		{
			// An OMP_PROFILE that is set but empty selects the DEFAULT profile and
			// suppresses PI_PROFILE. This is why Env carries a separate "set" flag:
			// os.Getenv alone cannot tell this case from an absent variable, and the
			// two install into different directories.
			//
			// It is also the one case where the store roots list two directories.
			// agentsession.Roots reads the environment through a func(string) string
			// and cannot see the flag, so it lists the profile's sessions directory
			// AND the default one, which is the widening that keeps this side from
			// refusing a binding the installer's own extension sent.
			name:          "an empty OMP_PROFILE suppresses PI_PROFILE",
			env:           Env{Home: home, OmpProfile: "", OmpProfileSet: true, PiProfile: "legacy"},
			want:          "/home/u/.omp/agent",
			wantRoots:     []string{"/home/u/.omp/profiles/legacy/agent/sessions", "/home/u/.omp/agent/sessions"},
			rootsAreWider: true,
		},
		{
			name:      "an invalid profile falls back to the default directory",
			env:       Env{Home: home, OmpProfile: "Not A Profile", OmpProfileSet: true},
			want:      "/home/u/.omp/agent",
			wantRoots: []string{"/home/u/.omp/agent/sessions"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ompAgentDir(tc.env); got != tc.want {
				t.Fatalf("agent dir = %q, want %q", got, tc.want)
			}
			if tc.wantRoots == nil {
				return
			}
			// The installer and the trust boundary must land in the same tree, and
			// this is the assertion that says so. Pi's two derivations drifted once
			// and every session binding was silently refused as a result; these
			// share a derivation, and this keeps them sharing it.
			env := tc.env
			roots := agentsession.Roots{Home: home, Env: func(name string) string {
				switch name {
				case "PI_CONFIG_DIR":
					return env.OmpConfigDir
				case "PI_CODING_AGENT_DIR":
					return env.PiAgentDir
				case "OMP_PROFILE":
					return env.OmpProfile
				case "PI_PROFILE":
					return env.PiProfile
				}
				return ""
			}}.For(OmpProvider)
			if !reflect.DeepEqual(roots, tc.wantRoots) {
				t.Fatalf("approved store roots are %v, want %v", roots, tc.wantRoots)
			}
			// The invariant behind the list, and the one that actually matters: the
			// directory the installer writes into is always one the trust boundary
			// approves. Pi's two derivations drifted on exactly this and every
			// binding its extension sent was silently refused.
			installed := filepath.Join(tc.want, "sessions")
			if !ompContains(roots, installed) {
				t.Fatalf("the installer writes under %s but %s is not an approved store root: %v",
					tc.want, installed, roots)
			}
			if !tc.rootsAreWider && len(roots) != 1 {
				t.Fatalf("the trust boundary approves %v, which is wider than the one directory the installer uses", roots)
			}
		})
	}

	// The asset lands in <agent dir>/extensions, which is where OMP's loader
	// scans, and nowhere else. The filename is deliberately not the one every
	// other file-shaped Sidecar asset uses; see OmpAssetName.
	paths := ompPathsFor(Env{Home: home})
	if paths.Owned != "/home/u/.omp/agent/extensions/sidecar-omp-lifecycle.js" {
		t.Fatalf("the asset path is %q", paths.Owned)
	}
	if OmpAssetName == PiAssetName {
		t.Fatal("OMP and Pi ship their assets under the same filename, which collides whenever PI_CODING_AGENT_DIR is set")
	}
}

// TestOmpRefusesToShareAnExtensionDirectoryWithPi is the collision refusal, and
// it is the one rule this adapter has that no other adapter needs.
//
// PI_CODING_AGENT_DIR is Pi's variable and OMP reads it too, because OMP is a
// rebranded fork of Pi's codebase. With it set the two agents resolve to one
// extensions directory and every extension in it is loaded by both binaries, so
// Sidecar would be reporting one provider's lane from the other's pane -- and
// `agent report` verifies --provider against the pane's occupant, so one of the
// two would be refused on every single event. Herdr's install_omp refuses the
// same state.
func TestOmpRefusesToShareAnExtensionDirectoryWithPi(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared-agent")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	env := Env{
		Home:            home,
		PiAgentDir:      shared,
		LookPath:        func(string) (string, error) { return filepath.Join(home, "bin", "omp"), nil },
		ProviderVersion: func(string) string { return "18.1.8" },
		UID:             os.Getuid(),
	}
	svc := Service{Env: env, Adapters: DefaultAdapters()}

	_, err := svc.Plan(OmpProvider, ActionInstall)
	if err == nil {
		t.Fatal("installing into a directory pi also reads was allowed")
	}
	if !strings.Contains(err.Error(), "pi and omp both read") {
		t.Fatalf("the refusal does not name the collision: %v", err)
	}

	// The status says the same thing, because a status that stayed silent while
	// the refusal explained itself is how a missing action looks like a bug.
	st := ompStatus(t, svc)
	if !strings.Contains(st.Message, "pi and omp both read") {
		t.Fatalf("the status does not mention the collision: %q", st.Message)
	}
	for _, offered := range st.Offered {
		if offered == ActionInstall {
			t.Fatal("install is offered in a state where it refuses")
		}
	}

	// And nothing was written, in particular not over Pi's own asset. The
	// distinct filename means the two were never going to occupy the same path,
	// and the ownership marker would refuse the overwrite even if they did; this
	// asserts the outcome rather than either mechanism.
	if entries, err := os.ReadDir(filepath.Join(shared, "extensions")); err == nil && len(entries) != 0 {
		t.Fatalf("the refused install left %d files behind", len(entries))
	}
}

// TestOmpNeverOverwritesPisAssetEvenInASharedDirectory is the "never overwrite"
// half of the same rule, asserted directly rather than inferred from the
// refusal. Ownership compares the marker's source id, so Sidecar's Pi asset in
// Sidecar's OMP directory is still somebody else's file.
func TestOmpNeverOverwritesPisAssetEvenInASharedDirectory(t *testing.T) {
	svc, env, paths := ompFixture(t)
	if err := os.MkdirAll(paths.OwnedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pi's real asset, at OMP's own asset path. Nothing puts it there in
	// practice; the point is what happens if something does.
	if err := os.WriteFile(paths.Owned, []byte(PiAsset()), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = env

	st := ompStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a foreign asset at the OMP path reads as %s", st.Status)
	}
	if _, err := svc.Plan(OmpProvider, ActionInstall); err == nil {
		t.Fatal("install overwrote a file carrying another integration's marker")
	}
	after, err := os.ReadFile(paths.Owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != PiAsset() {
		t.Fatal("Pi's asset was modified")
	}
}

// TestOmpRefusesAnAgentDirectoryItCannotResolve is the PI_CODING_AGENT_DIR
// reading OMP has and Pi does not: path.resolve rather than tilde expansion.
func TestOmpRefusesAnAgentDirectoryItCannotResolve(t *testing.T) {
	home := t.TempDir()
	env := Env{
		Home:            home,
		PiAgentDir:      "~/relative-to-nothing",
		LookPath:        func(string) (string, error) { return filepath.Join(home, "bin", "omp"), nil },
		ProviderVersion: func(string) string { return "18.1.8" },
		UID:             os.Getuid(),
	}
	svc := Service{Env: env, Adapters: DefaultAdapters()}
	for _, act := range Actions() {
		if _, err := svc.Plan(OmpProvider, act); err == nil {
			t.Fatalf("%s was planned against a directory Sidecar cannot name", act)
		}
	}
	st := ompStatus(t, svc)
	if !strings.Contains(st.Message, "not absolute") {
		t.Fatalf("the status does not explain the unresolvable directory: %q", st.Message)
	}
	if len(st.TargetPaths) != 0 {
		t.Fatalf("a status with no resolvable directory still names target paths %v", st.TargetPaths)
	}
	if got := OmpPaths(env); len(got) != 0 {
		t.Fatalf("OmpPaths = %v with no resolvable directory; a surface would print a path Sidecar cannot touch", got)
	}
}

// TestOmpInstallIntoACleanTreeIsExplicitAndIdempotent is the installer's basic
// contract: the operations are named before they happen, and running the same
// action twice is a no-op rather than a second write.
func TestOmpInstallIntoACleanTreeIsExplicitAndIdempotent(t *testing.T) {
	svc, _, paths := ompFixture(t)

	if st := ompStatus(t, svc); st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("a clean tree reports %s", st.Status)
	}

	plan := ompApply(t, svc, ActionInstall)
	if len(plan.Ops) != 2 || plan.Ops[0].Kind != OpMkdir || plan.Ops[1].Kind != OpWrite {
		t.Fatalf("install ops = %+v, want mkdir then write", plan.Ops)
	}
	if plan.StatusAfter != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install = %s", plan.StatusAfter)
	}

	installed, err := os.ReadFile(paths.Owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != OmpAsset() {
		t.Fatal("the installed bytes are not the bundled asset")
	}
	if !strings.HasPrefix(string(installed), "// sidecar-integration: id="+OmpSource) {
		t.Fatal("the installed file does not carry the marker ownership is decided by")
	}

	again, err := svc.Apply(OmpProvider, ActionInstall)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !again.Unchanged || len(again.Ops) != 0 {
		t.Fatalf("a second install was not a no-op: %+v", again)
	}
}

// TestAnOmpDryRunAndTheRealRunDescribeTheSameOperations is what makes --dry-run
// honest: the preview is the mutation with the execution step skipped, produced
// by the same function.
func TestAnOmpDryRunAndTheRealRunDescribeTheSameOperations(t *testing.T) {
	svc, _, _ := ompFixture(t)
	preview, err := svc.Plan(OmpProvider, ActionInstall)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun {
		t.Fatal("a planned action did not mark itself a dry run")
	}
	real := ompApply(t, svc, ActionInstall)
	if len(preview.Ops) != len(real.Ops) {
		t.Fatalf("dry run planned %d ops, the real run %d", len(preview.Ops), len(real.Ops))
	}
	for i := range preview.Ops {
		a, b := preview.Ops[i], real.Ops[i]
		if a.Kind != b.Kind || a.Path != b.Path || a.Checksum != b.Checksum || a.Mode != b.Mode {
			t.Fatalf("op %d differs:\ndry: %+v\nreal: %+v", i, a, b)
		}
	}
}

// TestOmpRefusesWhenOmpHasNeverBeenSetUp keeps Herdr's ensure_extension_dir
// semantics without copying its error shape.
//
// OMP's agent directory is created by OMP, so its absence means OMP has never
// run here, and creating a whole ~/.omp/agent tree for an agent that may be
// about to be configured somewhere else is Sidecar inventing a provider's
// private state.
func TestOmpRefusesWhenOmpHasNeverBeenSetUp(t *testing.T) {
	home := t.TempDir()
	env := Env{
		Home:            home,
		LookPath:        func(string) (string, error) { return filepath.Join(home, "bin", "omp"), nil },
		ProviderVersion: func(string) string { return "18.1.8" },
		UID:             os.Getuid(),
	}
	svc := Service{Env: env, Adapters: DefaultAdapters()}

	_, err := svc.Plan(OmpProvider, ActionInstall)
	if err == nil {
		t.Fatal("install into a home with no omp agent directory was allowed")
	}
	if !strings.Contains(err.Error(), "has not been set up") {
		t.Fatalf("the refusal does not say omp has never run here: %v", err)
	}
	// The status carries the same sentence, so the missing install action has a
	// visible reason rather than looking like a bug.
	if st := ompStatus(t, svc); !strings.Contains(st.Message, "has not been set up") {
		t.Fatalf("the status does not explain why install is not offered: %q", st.Message)
	}
	if _, err := os.Stat(filepath.Join(home, ".omp")); !os.IsNotExist(err) {
		t.Fatal("the refused install created omp's private state anyway")
	}
}

// TestAMissingOmpProviderRefusesInstallButStillAllowsCleanup is the asymmetry
// every adapter has: Sidecar will not create a provider's directory for a
// provider that is not installed, but it will always clean up after itself.
func TestAMissingOmpProviderRefusesInstallButStillAllowsCleanup(t *testing.T) {
	svc, _, paths := ompFixture(t)
	ompApply(t, svc, ActionInstall)

	// The same tree, read by a service whose PATH no longer finds omp.
	svc2 := Service{Env: envWithout(svc.Env, withoutOmp), Adapters: DefaultAdapters()}
	if _, err := svc2.Plan(OmpProvider, ActionInstall); err == nil {
		t.Fatal("install was allowed with no omp on PATH")
	}
	st := ompStatus(t, svc2)
	if st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status with no provider = %s", st.Status)
	}
	plan, err := svc2.Apply(OmpProvider, ActionUninstall)
	if err != nil {
		t.Fatalf("uninstall with no provider: %v", err)
	}
	if len(plan.Ops) == 0 {
		t.Fatal("uninstall removed nothing although the asset was installed")
	}
	if _, err := os.Stat(paths.Owned); !os.IsNotExist(err) {
		t.Fatal("the asset survived uninstall")
	}
}

func envWithout(env Env, opts ...func(*Env)) Env {
	for _, o := range opts {
		o(&env)
	}
	return env
}

// TestOmpNeverAdoptsOverwritesOrDeletesAFileItDoesNotOwn is the ownership rule,
// which is stricter than Herdr's in both directions: its uninstall_omp deletes
// its file without checking that the file is still its own.
func TestOmpNeverAdoptsOverwritesOrDeletesAFileItDoesNotOwn(t *testing.T) {
	svc, _, paths := ompFixture(t)
	if err := os.MkdirAll(paths.OwnedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// somebody else's extension\nexport default function () {}\n"
	if err := os.WriteFile(paths.Owned, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	if st := ompStatus(t, svc); st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a foreign file at the asset path reads as %s", st.Status)
	}
	for _, act := range []Action{ActionInstall, ActionUpdate, ActionRepair, ActionUninstall} {
		if _, err := svc.Plan(OmpProvider, act); err == nil {
			t.Fatalf("%s was planned against a file Sidecar does not own", act)
		}
	}
	after, err := os.ReadFile(paths.Owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != foreign {
		t.Fatal("the foreign file was modified")
	}
}

// TestASymlinkAtTheOmpAssetPathIsRefusedRatherThanFollowed keeps a write inside
// the directory Sidecar owns.
func TestASymlinkAtTheOmpAssetPathIsRefusedRatherThanFollowed(t *testing.T) {
	svc, _, paths := ompFixture(t)
	if err := os.MkdirAll(paths.OwnedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.js")
	if err := os.WriteFile(target, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.Owned); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, act := range []Action{ActionInstall, ActionRepair, ActionUninstall} {
		if _, err := svc.Plan(OmpProvider, act); err == nil {
			t.Fatalf("%s followed a symlink at the asset path", act)
		}
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "untouched\n" {
		t.Fatal("the symlink target was written through")
	}
}

// TestOmpStatusComesFromTheInstalledBytesNotFromAClaimedVersion is why the
// status hashes rather than reading the marker's version field: a truncated or
// hand-edited asset must read as needs-repair rather than as current.
func TestOmpStatusComesFromTheInstalledBytesNotFromAClaimedVersion(t *testing.T) {
	svc, _, paths := ompFixture(t)
	ompApply(t, svc, ActionInstall)

	truncated := OmpAsset()[:len(OmpAsset())/2]
	if err := os.WriteFile(paths.Owned, []byte(truncated), 0o644); err != nil {
		t.Fatal(err)
	}
	st := ompStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a truncated asset claiming version %s reads as %s", OmpAssetVersion, st.Status)
	}
	repaired := ompApply(t, svc, ActionRepair)
	if len(repaired.Ops) == 0 {
		t.Fatal("repair did nothing")
	}
	body, err := os.ReadFile(paths.Owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != OmpAsset() {
		t.Fatal("repair did not restore the bundled asset")
	}
}

// TestOmpUninstallLeavesUnrelatedExtensionsExactlyAsItFoundThem is the rule that
// matters most on a machine that also has Herdr installed: its own OMP extension
// lives in this same directory.
func TestOmpUninstallLeavesUnrelatedExtensionsExactlyAsItFoundThem(t *testing.T) {
	svc, _, paths := ompFixture(t)
	ompApply(t, svc, ActionInstall)

	neighbour := filepath.Join(paths.OwnedDir, "herdr-omp-agent-state.ts")
	body := "// installed by herdr\nexport default function () {}\n"
	if err := os.WriteFile(neighbour, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ompApply(t, svc, ActionUninstall)

	if _, err := os.Stat(paths.Owned); !os.IsNotExist(err) {
		t.Fatal("Sidecar's own asset survived uninstall")
	}
	after, err := os.ReadFile(neighbour)
	if err != nil {
		t.Fatalf("the neighbouring extension was removed: %v", err)
	}
	if string(after) != body {
		t.Fatal("the neighbouring extension was modified")
	}
	if _, err := os.Stat(paths.OwnedDir); err != nil {
		t.Fatal("the extension directory was removed although it still holds another extension")
	}
}

// TestOmpUninstallRemovesTheExtensionDirectoryOnlyWhenSidecarEmptiedIt is the
// other half of the same rule.
func TestOmpUninstallRemovesTheExtensionDirectoryOnlyWhenSidecarEmptiedIt(t *testing.T) {
	svc, _, paths := ompFixture(t)
	ompApply(t, svc, ActionInstall)
	ompApply(t, svc, ActionUninstall)
	if _, err := os.Stat(paths.OwnedDir); !os.IsNotExist(err) {
		t.Fatal("the extension directory survived although Sidecar emptied it")
	}
	if _, err := os.Stat(paths.AgentDir); err != nil {
		t.Fatal("uninstall removed omp's own agent directory, which Sidecar did not create")
	}
}

// TestAnOutdatedOmpAssetIsUpdatedAndTheReplacedCopyIsRecoverable pins the backup.
func TestAnOutdatedOmpAssetIsUpdatedAndTheReplacedCopyIsRecoverable(t *testing.T) {
	svc, _, paths := ompFixture(t)
	if err := os.MkdirAll(paths.OwnedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := "// sidecar-integration: id=" + OmpSource + " schema=1 version=0\nexport default function () {}\n"
	if err := os.WriteFile(paths.Owned, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := ompStatus(t, svc); st.Status != agentlifecycle.StatusOutdated {
		t.Fatalf("an older owned asset reads as %s", st.Status)
	}
	if _, err := svc.Plan(OmpProvider, ActionInstall); err == nil {
		t.Fatal("install was allowed over an existing older version; update is the verb for that")
	}
	plan := ompApply(t, svc, ActionUpdate)
	if len(plan.Ops) != 2 || plan.Ops[0].Kind != OpBackup || plan.Ops[1].Kind != OpWrite {
		t.Fatalf("update ops = %+v, want backup then write", plan.Ops)
	}
	backup, err := os.ReadFile(paths.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != old {
		t.Fatal("the backup is not the replaced asset")
	}
	body, err := os.ReadFile(paths.Owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != OmpAsset() {
		t.Fatal("update did not write the bundled asset")
	}
	// And the backup Sidecar made is Sidecar's to remove.
	ompApply(t, svc, ActionUninstall)
	if _, err := os.Stat(paths.Backup); !os.IsNotExist(err) {
		t.Fatal("uninstall left the backup behind")
	}
}

// TestOmpOfferedActionsAreExactlyTheOnesThatWouldNotRefuse keeps the surface
// honest: a verb a surface offers is a verb that will not refuse when pressed.
func TestOmpOfferedActionsAreExactlyTheOnesThatWouldNotRefuse(t *testing.T) {
	svc, _, _ := ompFixture(t)
	for _, phase := range []string{"clean", "installed"} {
		st := ompStatus(t, svc)
		offered := map[Action]bool{}
		for _, a := range st.Offered {
			offered[a] = true
		}
		for _, act := range Actions() {
			_, err := svc.Plan(OmpProvider, act)
			if offered[act] != (err == nil) {
				t.Fatalf("%s: %s is offered=%v but planning it gave err=%v", phase, act, offered[act], err)
			}
		}
		if phase == "clean" {
			ompApply(t, svc, ActionInstall)
		}
	}
}

// TestTheOmpAdapterReportsEveryPathItTouches is the "show the exact paths before
// mutating" rule, asserted against the plan rather than against a second list.
func TestTheOmpAdapterReportsEveryPathItTouches(t *testing.T) {
	svc, env, paths := ompFixture(t)
	declared := map[string]bool{}
	for _, p := range OmpPaths(env) {
		declared[p] = true
	}
	if !declared[paths.Owned] {
		t.Fatalf("OmpPaths = %v, which does not name the asset at %s", OmpPaths(env), paths.Owned)
	}
	plan := ompApply(t, svc, ActionInstall)
	for _, op := range plan.Ops {
		if op.Path == "" {
			t.Fatalf("op %+v names no path", op)
		}
		if !strings.HasPrefix(op.Path, paths.AgentDir) {
			t.Fatalf("op %+v writes outside omp's agent directory %s", op, paths.AgentDir)
		}
	}
	st := ompStatus(t, svc)
	if len(st.TargetPaths) == 0 {
		t.Fatal("an installed integration names no target paths")
	}
}

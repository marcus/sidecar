package agentlifecycle

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Phase D traced Codex and Claude Code, which are hook-shaped rather than
// bus-shaped: a provider runs one short-lived process per event instead of
// emitting onto a stream a plugin subscribes to. Their traces therefore have a
// different column layout from the Phase A/B OpenCode files, and their own
// reader.
//
// These tests exist for the same reason the OpenCode trace tests do: every
// claim in capabilities.json that says "traced" should be a claim something
// reads back and asserts, so that deleting or altering a trace breaks a test
// rather than quietly turning a measurement into a sentence.

// hookRow is one sanitized hook-trace line: the relative millisecond offset,
// the provider's event name, placeholder session and turn identifiers, the tool
// name where there was one, and the payload's field NAMES.
//
// Field names are carried because they are evidence in their own right. A
// payload with a field called "prompt" is a payload Sidecar must never persist,
// and recording the name costs nothing while recording the value would be the
// exact privacy failure the plan forbids.
type hookRow struct {
	event   string
	session string
	turn    string
	tool    string
	fields  []string
}

func readHookTrace(t *testing.T, provider, name string) []hookRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "traces", provider, name))
	if err != nil {
		t.Fatal(err)
	}
	var rows []hookRow
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 6 {
			t.Fatalf("malformed hook trace row in %s/%s: %q", provider, name, line)
		}
		rows = append(rows, hookRow{
			event: cols[1], session: cols[2], turn: cols[3], tool: cols[4],
			fields: strings.Split(cols[5], ","),
		})
	}
	return rows
}

func eventsOf(rows []hookRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.event)
	}
	return out
}

func assertEvents(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event sequence %v, want %v", got, want)
	}
}

// valueBearingTraceKeys is the closed set of field keys a trace may record a
// VALUE for. Everything else may appear only as a bare name.
//
// The rule this enforces is the README's: a value is permitted only where the
// vocabulary is closed and chosen by the provider's own source, which is what
// made it safe for the OpenCode Phase B traces to record a bounded error class
// name where a message would have been a privacy failure. `type` and `reason`
// are Pi's own discriminators. The four `ctx.` entries are derived observations
// rather than payload fields, and they are here because the shipped asset's
// guards are built on exactly them and a bare field name would not show whether
// a guard was correct — `ctx.sessionFile` and `ctx.sessionId` record only
// `present` or `absent`, never a path or an id.
//
// Widening this set is a deliberate act. Adding a key here means asserting that
// its values cannot carry user content, and the review that finds otherwise has
// to happen before the trace is checked in, not after.
//
// The four kilo entries are appended under the same rule and are worth their own
// sentence each, because widening this set is the deliberate act the paragraph
// above describes. `status` is kilo's session.status discriminator, whose
// vocabulary its own schema closes at idle, busy, retry and offline; recording it
// is what lets a fixture show that upstream reads the field's shape wrongly.
// `error` is a bounded error class name, truncated in the tracer, which is
// exactly the concession the OpenCode phase B traces already made for
// MessageAbortedError and for the same reason: a class name is chosen by the
// provider's source where a message is written by a model. `info.id` and
// `info.parentID` record only `present` or `absent`, never an identifier, in the
// same way `ctx.sessionFile` does.
var valueBearingTraceKeys = map[string]bool{
	"type":            true,
	"reason":          true,
	"ctx.mode":        true,
	"ctx.isIdle":      true,
	"ctx.sessionFile": true,
	"ctx.sessionId":   true,
	"status":          true,
	"error":           true,
	"info.id":         true,
	"info.parentID":   true,
	// Kimi's two. `source` is SessionStart's own discriminator, startup or
	// resume, and `client_type` names which client produced the payload; both
	// are enumerations in Kimi's published hooks reference. Kimi's `reason`,
	// which distinguishes a cancelled Interrupt from an exiting SessionEnd, is
	// covered by the `reason` entry above. Kimi's tool name is deliberately NOT
	// here: it already has a column of its own, and recording it twice would
	// widen this set for nothing.
	"client_type": true,
	"source":      true,
	// OMP's four, appended under the same rule and each worth its own sentence.
	// `ctx.hasUI` is a boolean, and it is the gate this provider's asset checks
	// where Pi's checks `ctx.mode`; recording it is what lets a fixture show the
	// gate was satisfied. `willContinue` is AgentEndEvent's own boolean flag,
	// recorded as true, false or absent, and the difference between "absent" and
	// "false" is exactly why the asset compares against an explicit true.
	// `stopReason` is OMP's own closed StopReason vocabulary -- stop, aborted,
	// error and the rest -- chosen by its source rather than written by a model,
	// which is the same concession `error` already makes for kilo. `approved` is
	// ToolApprovalResolvedEvent's boolean, and it is the whole of what makes a
	// denial distinguishable from an approval on this provider.
	"ctx.hasUI":    true,
	"willContinue": true,
	"stopReason":   true,
	"approved":     true,
	// Mastra Code's four, each an enumeration its own types.d.ts closes.
	// `stop_reason` is complete, aborted, error or suspended; `decision` is
	// approved, declined, dismissed, auto_approved or auto_declined;
	// `permission_kind` is tool_approval, sandbox_access or plan_approval; and its
	// `reason`, which distinguishes a user interrupt from the other two interrupt
	// causes and names why a Notification fired, is covered by the entry above.
	//
	// `session_id` is the one that needs its own sentence, because unlike every
	// other key here it names a field whose real value is an identifier. It
	// records ONLY the word `placeholder` or the word `thread` -- never an id --
	// in exactly the way `ctx.sessionFile` records only present or absent. The
	// distinction is the whole evidence for this port's one correction to
	// upstream's mapping, and a bare field name could not carry it: SessionStart
	// and UserPromptSubmit have the field and it holds Mastra Code's constructor
	// placeholder rather than a conversation.
	"stop_reason":     true,
	"decision":        true,
	"permission_kind": true,
	"session_id":      true,
}

// TestNoHookTraceCarriesAValue is the privacy gate over the fixtures
// themselves. The traces record which fields a payload had, and a field named
// "prompt" or "tool_input" is exactly the kind of thing that must never appear
// with a value beside it.
//
// It used to check the session and turn columns and nothing else, which was
// enough while every trace here recorded bare field names. The Pi traces are the
// first to put `key=value` pairs in the `fields` column, and under the old check
// a future capture that recorded `prompt=<the user's prompt>` would have passed
// every test in the tree. So the allowlist above is enforced here: a `=` on any
// key outside it fails, whatever the value looks like.
func TestNoHookTraceCarriesAValue(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "pi", "kilo", "kimi", "omp", "grok", "qwen", "mastracode"} {
		entries, err := os.ReadDir(filepath.Join("testdata", "traces", provider))
		if err != nil {
			t.Fatalf("%s has no traces but its capability entry claims real evidence: %v", provider, err)
		}
		for _, e := range entries {
			rows := readHookTrace(t, provider, e.Name())
			if len(rows) == 0 {
				t.Fatalf("%s/%s is empty", provider, e.Name())
			}
			for _, r := range rows {
				if !strings.HasPrefix(r.session, "session-") && r.session != "-" {
					t.Fatalf("%s/%s carries a real session identifier %q", provider, e.Name(), r.session)
				}
				if !strings.HasPrefix(r.turn, "turn-") && r.turn != "-" {
					t.Fatalf("%s/%s carries a real turn identifier %q", provider, e.Name(), r.turn)
				}
				for _, field := range r.fields {
					key, _, hasValue := strings.Cut(field, "=")
					if !hasValue || valueBearingTraceKeys[key] {
						continue
					}
					t.Fatalf("%s/%s records a value for %q, which is not in the closed set of keys "+
						"whose vocabulary the provider's own source fixes (%v). A payload field's NAME is "+
						"evidence; its value is the thing these traces exist not to keep.",
						provider, e.Name(), key, sortedTraceValueKeys())
				}
			}
		}
	}
}

// TestCodexTraceProvesEveryFullLifecycleTransition is the evidence behind the
// recorded claim that Codex's own contract would support the full tier. It is
// asserted rather than described, so removing a trace or changing its shape
// fails here instead of leaving the registry asserting something unbacked.
func TestCodexTraceProvesEveryFullLifecycleTransition(t *testing.T) {
	// Work start, tool use, turn completion, session identity.
	assertEvents(t, eventsOf(readHookTrace(t, "codex", "exec-tool-turn.tsv")),
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop")

	// Blocking and unblocking. PermissionRequest is the last event before the
	// pane blocks, and PostToolUse is what follows an approval.
	assertEvents(t, eventsOf(readHookTrace(t, "codex", "permission-approved.tsv")),
		"UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "Stop")

	// Process exit.
	assertEvents(t, eventsOf(readHookTrace(t, "codex", "session-end.tsv")), "SessionEnd")
}

// TestCodexResolvesABlockedPaneByTwoDifferentEvents is the finding that decides
// whether a Codex adapter can be trusted with the blocked lane at all.
//
// Approval and denial do NOT converge on the same event. An adapter that
// unblocked only on PostToolUse would sit on `blocked` forever every time a
// user said no, which is precisely the latch that keeps Claude Code below full
// authority. Codex escapes it only because Interrupt covers the refusal path.
func TestCodexResolvesABlockedPaneByTwoDifferentEvents(t *testing.T) {
	approved := eventsOf(readHookTrace(t, "codex", "permission-approved.tsv"))
	denied := eventsOf(readHookTrace(t, "codex", "permission-denied.tsv"))

	assertEvents(t, denied, "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Interrupt")

	if approved[len(approved)-1] == denied[len(denied)-1] {
		t.Fatal("the fixture no longer shows approval and denial ending differently, which is the whole finding")
	}
	for _, e := range denied {
		if e == "Stop" || e == "PostToolUse" {
			t.Fatalf("the denial trace contains %s; the recorded finding is that it contains neither", e)
		}
	}
}

// TestCodexCancellationIsFirstClass pins the transition OpenCode had to infer
// from an error class name. Codex has a dedicated event for it.
func TestCodexCancellationIsFirstClass(t *testing.T) {
	rows := readHookTrace(t, "codex", "cancelled-turn.tsv")
	assertEvents(t, eventsOf(rows), "UserPromptSubmit", "Interrupt")
	if rows[1].turn != rows[0].turn {
		t.Fatal("the interrupt does not carry the turn it cancelled, so it could not be attributed")
	}
}

// TestClaudeCancellationEmitsNothingAtAll is the contract gap, asserted.
//
// This is the single fact that caps Claude Code below full lifecycle authority,
// and it is a fact about what is ABSENT.
//
// Be precise about what this test can and cannot do, because an earlier
// description of it claimed more. It reads a static checked-in fixture, so it
// CANNOT fail on the day Claude starts emitting a cancellation event -- there is
// no live provider in this test to change its behavior. It fails only after a
// human has re-traced Claude and edited the fixture, and that human already
// knows what they found.
//
// What it does do is protect the record: the absence cannot be edited away
// silently, and a re-trace that contradicts it has to be a deliberate, reviewed
// change to a file under version control. That is a fixture-integrity guard, not
// a tripwire on the provider. Noticing that the gap has closed is the
// requalification procedure's job, on the cadence the capability matrix states,
// and nothing in CI will do it for you.
func TestClaudeCancellationEmitsNothingAtAll(t *testing.T) {
	rows := readHookTrace(t, "claude", "interrupted-turn.tsv")
	assertEvents(t, eventsOf(rows), "UserPromptSubmit")

	// Escape-cancelling a permission prompt is the other cancellation route and
	// is equally silent: the trace ends on Notification, with the user's answer
	// producing nothing.
	cancelled := eventsOf(readHookTrace(t, "claude", "permission-cancelled.tsv"))
	assertEvents(t, cancelled, "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification")

	// Both claims rest on a capture window, and a window that lives only in a td
	// log is one a future requalifier cannot weigh: they would have no way to
	// tell whether nothing fired over one second or over eighteen. Requiring it
	// in the fixture keeps the evidence attached to the claim.
	for _, name := range []string{"interrupted-turn.tsv", "permission-cancelled.tsv"} {
		if window := captureWindow(t, "claude", name); window == "" {
			t.Fatalf("%s makes an absence claim without recording how long it watched", name)
		}
	}
}

// captureWindow reads the `# capture-window:` row a trace carries when its
// value is an absence. Traces that only record what did happen do not need one.
func captureWindow(t *testing.T, provider, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "traces", provider, name))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# capture-window:"
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, marker) {
			continue
		}
		window := strings.TrimSpace(strings.TrimPrefix(line, marker))
		if _, err := time.ParseDuration(window); err != nil {
			t.Fatalf("%s/%s records an unparseable capture window %q: %v", provider, name, window, err)
		}
		return window
	}
	return ""
}

// TestClaudeSkipsPostToolUseAfterAPermissionPrompt pins the second Claude
// finding, which is more insidious than the first because the event that goes
// missing is one that fires perfectly well on every turn that did not block.
func TestClaudeSkipsPostToolUseAfterAPermissionPrompt(t *testing.T) {
	plain := eventsOf(readHookTrace(t, "claude", "print-mode-tool-turn.tsv"))
	prompted := eventsOf(readHookTrace(t, "claude", "permission-approved-skips-posttooluse.tsv"))

	if !contains(plain, "PostToolUse") {
		t.Fatal("the unprompted trace lost PostToolUse, so the comparison proves nothing")
	}
	if contains(prompted, "PostToolUse") {
		t.Fatal("the prompted trace now contains PostToolUse; the recorded finding is that it does not")
	}
	if !contains(prompted, "Stop") {
		t.Fatal("the approved turn did not complete, so this is not the case the finding describes")
	}
}

// TestClaudeBlockingIsFirstClassOnTheCurrentRelease is the correction. An
// earlier docs-only reading recorded Claude as offering session identity and
// nothing more; PermissionRequest fires, and so does a Notification.
func TestClaudeBlockingIsFirstClassOnTheCurrentRelease(t *testing.T) {
	blocked := eventsOf(readHookTrace(t, "claude", "permission-denied.tsv"))
	if !contains(blocked, "PermissionRequest") {
		t.Fatal("PermissionRequest is absent, which would restore the stale reading of this provider")
	}
	if !contains(blocked, "Notification") {
		t.Fatal("Notification is absent from the blocked trace")
	}
}

// sortedTraceValueKeys renders the allowlist for a failure message.
func sortedTraceValueKeys() []string {
	out := make([]string, 0, len(valueBearingTraceKeys))
	for k := range valueBearingTraceKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// Pi.
//
// The Pi entry is the first in this registry promoted from docs-only to
// real-trace by a live capture, and these tests are what stop that promotion
// from being a sentence. Each one re-derives a specific line of the capability
// entry from the fixtures, so deleting a trace or editing its shape fails here
// rather than quietly turning a measurement back into a claim.

// TestPiTraceEarnsExactlyWorkStartAndTurnCompletion is the promotion, asserted.
//
// It reads the registry entry and requires the traces to contain the events
// that entry says it reports — and requires that they contain no event the
// entry does not claim. A future edit that adds `tool_use` to `covered` without
// the asset subscribing to anything fails here.
func TestPiTraceEarnsExactlyWorkStartAndTurnCompletion(t *testing.T) {
	cap, ok := CapabilityForSource("sidecar.pi.extension")
	if !ok {
		t.Fatal("no capability registered for sidecar.pi.extension")
	}
	if cap.Evidence != EvidenceRealTrace {
		t.Fatalf("pi evidence = %q; these traces exist to make it real-trace", cap.Evidence)
	}
	if tier, reason := cap.TierFor(StatusCurrent, true); tier != TierAdvisory {
		t.Fatalf("pi exercises %q (%s), want advisory: it is the traced ceiling", tier, reason)
	}

	events := eventsOf(readHookTrace(t, "pi", "simple-turn.tsv"))
	assertEvents(t, events,
		"session_start",
		"before_agent_start", "agent_start", "turn_start", "message_start", "message_end",
		"message_start", "message_end", "turn_end", "agent_end", "agent_settled")

	// Each claimed transition maps to an event that is actually in the trace.
	for _, claim := range []struct {
		transition Transition
		event      string
	}{
		{TransitionSessionIdentity, "session_start"},
		{TransitionWorkStart, "agent_start"},
		{TransitionTurnComplete, "agent_settled"},
	} {
		if !cap.Covers(claim.transition) {
			t.Fatalf("pi no longer claims %s, which %s in the trace supports", claim.transition, claim.event)
		}
		if !contains(events, claim.event) {
			t.Fatalf("pi claims %s but no %s appears in simple-turn.tsv", claim.transition, claim.event)
		}
	}
	// And nothing else is claimed. blocked_on_request and unblocked are
	// structurally unreachable; tool_use and process_exit are unclaimed by
	// choice; cancelled is unknowable. See the tests below for each.
	for _, absent := range []Transition{
		TransitionToolUse, TransitionProcessExit, TransitionCancelled,
		TransitionBlockedOnRequest, TransitionUnblocked, TransitionSubagent,
	} {
		if cap.Covers(absent) {
			t.Fatalf("pi claims %q, which no trace here supports", absent)
		}
	}
	if cap.CoversFullLifecycle() {
		t.Fatal("pi claims full lifecycle coverage; no released Pi can produce a blocked signal")
	}
}

// TestPiSessionStartIsTuiOnlyAndCarriesATranscript pins the two facts the
// session-identity claim rests on: the mode gate the asset checks instead of
// hasUI, and the presence of a session file to bind the pane to.
func TestPiSessionStartIsTuiOnlyAndCarriesATranscript(t *testing.T) {
	rows := readHookTrace(t, "pi", "simple-turn.tsv")
	if rows[0].event != "session_start" {
		t.Fatalf("simple-turn.tsv no longer opens on session_start (%s)", rows[0].event)
	}
	for _, want := range []string{"reason=startup", "ctx.mode=tui", "ctx.sessionFile=present", "ctx.sessionId=present"} {
		if !contains(rows[0].fields, want) {
			t.Fatalf("session_start no longer records %s; fields are %v", want, rows[0].fields)
		}
	}
}

// TestPiTurnCompletionCannotComeFromAgentEnd is the trap, measured.
//
// The asset deliberately does not subscribe to agent_end, and the reason is not
// visible from the event names: agent_end can be followed by an automatic retry
// or a compaction. The trace shows the mechanical half of that — Pi still
// reports itself busy at agent_end and idle at agent_settled, milliseconds
// later — so an asset that closed a turn on agent_end would announce a finished
// turn in the middle of one.
func TestPiTurnCompletionCannotComeFromAgentEnd(t *testing.T) {
	rows := readHookTrace(t, "pi", "simple-turn.tsv")
	var end, settled *hookRow
	for i := range rows {
		switch rows[i].event {
		case "agent_end":
			end = &rows[i]
		case "agent_settled":
			settled = &rows[i]
		}
	}
	if end == nil || settled == nil {
		t.Fatal("the trace no longer contains both agent_end and agent_settled, which is the whole comparison")
	}
	if !contains(end.fields, "ctx.isIdle=false") {
		t.Fatalf("agent_end no longer reports isIdle false; fields are %v", end.fields)
	}
	if !contains(settled.fields, "ctx.isIdle=true") {
		t.Fatalf("agent_settled no longer reports isIdle true; fields are %v", settled.fields)
	}
}

// TestPiTurnEndIsAProviderRoundTripNotATurn is the second half of the same
// finding, and the one that rules out the other plausible completion event.
//
// A single agent run that calls a tool emits turn_start and turn_end twice:
// once around the tool call, once around the reply. agent_settled emits once.
func TestPiTurnEndIsAProviderRoundTripNotATurn(t *testing.T) {
	events := eventsOf(readHookTrace(t, "pi", "tool-turn.tsv"))
	if n := count(events, "turn_end"); n < 2 {
		t.Fatalf("tool-turn.tsv has %d turn_end rows; the finding is that one agent run emits more than one", n)
	}
	if n := count(events, "agent_settled"); n != 1 {
		t.Fatalf("tool-turn.tsv has %d agent_settled rows, want exactly 1", n)
	}
	if events[len(events)-1] != "agent_settled" {
		t.Fatalf("the run no longer ends on agent_settled (%s)", events[len(events)-1])
	}
}

// TestPiRunsAToolWithNoPermissionEvent is the emitter-side proof that the
// blocked lane is structurally unreachable rather than merely untraced.
//
// The capability entry's reading of Pi's shipped type definitions said no
// permission event exists. This is the same claim measured from the other side:
// a bash tool ran, and nothing that could be a block appeared around it.
func TestPiRunsAToolWithNoPermissionEvent(t *testing.T) {
	cap, ok := CapabilityForSource("sidecar.pi.extension")
	if !ok {
		t.Fatal("no capability registered for sidecar.pi.extension")
	}
	if cap.Covers(TransitionBlockedOnRequest) || cap.Covers(TransitionUnblocked) {
		t.Fatal("pi claims a blocked transition; no released Pi can produce one")
	}

	rows := readHookTrace(t, "pi", "tool-turn.tsv")
	var sawTool bool
	for _, r := range rows {
		if r.event == "tool_execution_start" {
			sawTool = true
			if r.tool == "-" {
				t.Fatal("tool_execution_start no longer names the tool, so tool_use being unclaimed-by-choice is unproved")
			}
		}
		for _, banned := range []string{"permission", "approval", "prompt_request", "blocked"} {
			if strings.Contains(strings.ToLower(r.event), banned) {
				t.Fatalf("tool-turn.tsv contains %q; the recorded finding is that Pi emits no such event", r.event)
			}
		}
	}
	if !sawTool {
		t.Fatal("tool-turn.tsv contains no tool execution, so it proves nothing about permissions")
	}
}

// TestPiCancellationIsIndistinguishableFromCompletion is an absence claim, and
// it is why `cancelled` is not in `covered`.
//
// Read the note on TestClaudeCancellationEmitsNothingAtAll about what a static
// fixture can and cannot do: this is a fixture-integrity guard, not a tripwire
// on the provider.
func TestPiCancellationIsIndistinguishableFromCompletion(t *testing.T) {
	cancelled := readHookTrace(t, "pi", "cancelled-turn.tsv")
	completed := readHookTrace(t, "pi", "simple-turn.tsv")

	tail := func(rows []hookRow) []hookRow { return rows[len(rows)-3:] }
	for i, got := range tail(cancelled) {
		want := tail(completed)[i]
		if got.event != want.event {
			t.Fatalf("cancelled tail %v differs from completed tail %v; the finding is that they are the same",
				eventsOf(tail(cancelled)), eventsOf(tail(completed)))
		}
		if strings.Join(got.fields, ",") != strings.Join(want.fields, ",") {
			t.Fatalf("%s now carries different fields on a cancelled turn (%v) than a completed one (%v); "+
				"a distinguishing field would make the cancelled transition claimable",
				got.event, got.fields, want.fields)
		}
	}
	if window := captureWindow(t, "pi", "cancelled-turn.tsv"); window == "" {
		t.Fatal("cancelled-turn.tsv makes an absence claim without recording how long it watched")
	}
}

// TestPiErrorTurnResolvesAndQuitIsReadable covers the last two fixtures.
//
// A failed turn takes the same path as a successful one, so the pane does not
// latch on working — the failure mode the resolver has to survive. And
// session_shutdown's reason is readable and is "quit", which is what keeps
// process_exit an unclaimed-by-choice gap rather than an impossible one.
func TestPiErrorTurnResolvesAndQuitIsReadable(t *testing.T) {
	rows := readHookTrace(t, "pi", "error-turn-and-quit.tsv")
	events := eventsOf(rows)
	if !contains(events, "agent_start") || !contains(events, "agent_settled") {
		t.Fatalf("the error turn no longer runs the full ladder: %v", events)
	}

	last := rows[len(rows)-1]
	if last.event != "session_shutdown" {
		t.Fatalf("the fixture no longer ends on session_shutdown (%s)", last.event)
	}
	if !contains(last.fields, "reason=quit") {
		t.Fatalf("session_shutdown no longer records reason=quit; fields are %v", last.fields)
	}

	cap, ok := CapabilityForSource("sidecar.pi.extension")
	if !ok {
		t.Fatal("no capability registered for sidecar.pi.extension")
	}
	if cap.Covers(TransitionProcessExit) {
		t.Fatal("pi claims process_exit; the shipped asset does not subscribe to session_shutdown at all")
	}
}

func count(xs []string, want string) int {
	n := 0
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}

// The kilo traces, re-derived.
//
// Kilo is bus-shaped rather than hook-shaped, and its traces use the six-column
// hook layout for the same reason Pi's do: the columns carry the same evidence
// and a second reader would have bought nothing. The `event` column carries the
// tracer's `bus:` or `hook:` prefix, because whether a name arrives on the bus or
// as a plugin hook is precisely the distinction one of these tests exists to
// pin.

// kiloEvents returns a trace's events with the tracer's kind prefix stripped.
func kiloEvents(t *testing.T, name string) []string {
	t.Helper()
	rows := readHookTrace(t, "kilo", name)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		_, event, found := strings.Cut(r.event, ":")
		if !found {
			t.Fatalf("kilo/%s row %q has no bus:/hook: prefix, which the traces record deliberately", name, r.event)
		}
		out = append(out, event)
	}
	return out
}

// TestKiloTraceEarnsExactlyWhatTheEntryClaims is the promotion, asserted.
//
// It reads the registry entry and requires the traces to contain the events that
// entry says it reports, and requires that they contain no event the entry does
// not claim. A future edit that adds `tool_use` to `covered` without kilo
// publishing the name fails here.
func TestKiloTraceEarnsExactlyWhatTheEntryClaims(t *testing.T) {
	cap, ok := CapabilityForSource("sidecar.kilo.plugin")
	if !ok {
		t.Fatal("no capability registered for sidecar.kilo.plugin")
	}
	if cap.Evidence != EvidenceRealTrace {
		t.Fatalf("kilo evidence = %q; these traces exist to make it real-trace", cap.Evidence)
	}
	if tier, reason := cap.TierFor(StatusCurrent, true); tier != TierAdvisory {
		t.Fatalf("kilo exercises %q (%s), want advisory: it is the ceiling this asset can reach", tier, reason)
	}

	simple := kiloEvents(t, "simple-turn.tsv")
	blocked := kiloEvents(t, "blocked-turn.tsv")

	for _, claim := range []struct {
		transition Transition
		event      string
		events     []string
	}{
		{TransitionSessionIdentity, "session.created", simple},
		{TransitionWorkStart, "chat.message", simple},
		{TransitionTurnComplete, "session.idle", simple},
		{TransitionBlockedOnRequest, "permission.asked", blocked},
		{TransitionUnblocked, "permission.replied", blocked},
	} {
		if !cap.Covers(claim.transition) {
			t.Fatalf("kilo no longer claims %s, which %s in the traces supports", claim.transition, claim.event)
		}
		if !contains(claim.events, claim.event) {
			t.Fatalf("kilo claims %s but no %s appears in the trace that earned it", claim.transition, claim.event)
		}
	}
	// And nothing else. tool_use is unreachable, cancelled is indistinguishable,
	// process_exit is unclaimed by choice, and there is no subagent evidence.
	for _, absent := range []Transition{
		TransitionToolUse, TransitionCancelled, TransitionProcessExit, TransitionSubagent,
	} {
		if cap.Covers(absent) {
			t.Fatalf("kilo claims %q, which no trace here supports", absent)
		}
	}
	if cap.CoversFullLifecycle() {
		t.Fatal("kilo claims full lifecycle coverage; the shipped asset can produce neither cancelled nor process_exit")
	}
}

// TestKiloSessionStatusIsAnObjectNotAString is the upstream bug, measured.
//
// Herdr's kilo asset at integration version 4 accepts a session.status only when
// it is a string. Kilo's event carries an object whose `type` is the
// discriminator, so upstream's asset maps none of them on this release and falls
// through to re-reporting the session instead. The trace records the value under
// the key `status`, which the tracer read from `properties.status.type`, and
// this is the assertion that keeps the port's one deliberate fix attached to the
// measurement that justified it.
func TestKiloSessionStatusIsAnObjectNotAString(t *testing.T) {
	rows := readHookTrace(t, "kilo", "simple-turn.tsv")
	var busy, idle int
	for _, r := range rows {
		if !strings.HasSuffix(r.event, ":session.status") {
			continue
		}
		// The bare field name is present because properties.status exists; the
		// key=value pair is present because the tracer read status.type. Both
		// together are what say the field is an object.
		if !contains(r.fields, "status") {
			t.Fatalf("a session.status row no longer records the raw field name: %v", r.fields)
		}
		switch {
		case contains(r.fields, "status=busy"):
			busy++
		case contains(r.fields, "status=idle"):
			idle++
		default:
			t.Fatalf("a session.status row carries a discriminator this test does not know: %v", r.fields)
		}
	}
	if busy < 2 {
		t.Fatalf("simple-turn.tsv records %d busy assertions; the repeat is what the asset's suppression exists for", busy)
	}
	if idle != 1 {
		t.Fatalf("simple-turn.tsv records %d idle assertions, want exactly one", idle)
	}
}

// TestKiloToolEventsNeverReachThePluginEventStream is the absence that keeps
// tool_use unclaimed.
//
// tool.execute.before and tool.execute.after are plugin hooks in kilo, not bus
// events, so an `event` handler never sees them. The fixture is a turn in which a
// bash tool really ran, and the absence is delimited by two rows a reader can see
// rather than by a watch that ended, so no capture window is owed.
func TestKiloToolEventsNeverReachThePluginEventStream(t *testing.T) {
	events := kiloEvents(t, "tool-turn.tsv")
	first, last := indexOf(events, "chat.message"), indexOf(events, "session.idle")
	if first < 0 || last < 0 || last <= first {
		t.Fatalf("tool-turn.tsv no longer brackets the tool call between chat.message and session.idle: %v", events)
	}
	for _, absent := range []string{"tool.execute.before", "tool.execute.after"} {
		if contains(events[first:last], absent) {
			t.Fatalf("%s now appears on the bus; the recorded gap is stale and the matrix must be updated", absent)
		}
	}
	cap, ok := CapabilityForSource("sidecar.kilo.plugin")
	if !ok {
		t.Fatal("no capability registered for sidecar.kilo.plugin")
	}
	if cap.Covers(TransitionToolUse) {
		t.Fatal("kilo claims tool_use; no tool event reaches the plugin event stream at all")
	}
}

// TestKiloBlockingAndUnblockingBothCarryTheSession is what the blocked and
// unblock claims rest on. Both events exist, in this order, and both name the
// session, which is what lets the pane's lane move and stay attributed.
func TestKiloBlockingAndUnblockingBothCarryTheSession(t *testing.T) {
	rows := readHookTrace(t, "kilo", "blocked-turn.tsv")
	var asked, replied *hookRow
	for i := range rows {
		switch {
		case strings.HasSuffix(rows[i].event, ":permission.asked"):
			asked = &rows[i]
		case strings.HasSuffix(rows[i].event, ":permission.replied"):
			replied = &rows[i]
		}
	}
	if asked == nil || replied == nil {
		t.Fatalf("blocked-turn.tsv no longer contains both halves of the permission pair: %v", kiloEvents(t, "blocked-turn.tsv"))
	}
	for _, r := range []*hookRow{asked, replied} {
		if !strings.HasPrefix(r.session, "session-") {
			t.Fatalf("%s does not carry a session, so a blocked pane could not be attributed", r.event)
		}
		if !contains(r.fields, "sessionID") {
			t.Fatalf("%s no longer carries sessionID; fields are %v", r.event, r.fields)
		}
	}
}

// TestKiloSessionErrorIsClosedByTheNextStatus is why upstream's blocked mapping
// for a session error is safe rather than merely tolerated, and it is the
// concrete argument for reading session.status at all: with upstream's
// string-only read nothing would assert a lane here, and the spurious blocked
// would stand until session.idle.
func TestKiloSessionErrorIsClosedByTheNextStatus(t *testing.T) {
	rows := readHookTrace(t, "kilo", "error-turn.tsv")
	errIdx := -1
	for i, r := range rows {
		if strings.HasSuffix(r.event, ":session.error") {
			errIdx = i
			break
		}
	}
	if errIdx < 0 {
		t.Fatal("error-turn.tsv records no session.error, so it proves nothing about a failed turn")
	}
	var errName string
	for _, f := range rows[errIdx].fields {
		if name, ok := strings.CutPrefix(f, "error="); ok {
			errName = name
		}
	}
	if errName == "" {
		t.Fatal("session.error records no bounded error class name")
	}

	var sawIdleStatus bool
	for _, r := range rows[errIdx+1:] {
		if strings.HasSuffix(r.event, ":session.status") && contains(r.fields, "status=idle") {
			sawIdleStatus = true
			break
		}
	}
	if !sawIdleStatus {
		t.Fatal("no session.status idle follows the error, so the blocked lane upstream opens would latch")
	}

	cap, ok := CapabilityForSource("sidecar.kilo.plugin")
	if !ok {
		t.Fatal("no capability registered for sidecar.kilo.plugin")
	}
	if cap.Covers(TransitionCancelled) {
		t.Fatalf("kilo claims cancelled; a session.error carrying %q is the same shape a user interrupt takes, "+
			"and the shipped asset does not read the name", errName)
	}
}

// TestNoKiloTraceRecordsASubagent keeps the recorded subagent gap attached to the
// fixtures. Kilo publishes info.parentID on its session events and upstream's
// kilo asset does not read it, so a child session's events would drive the pane's
// lane. Nothing here measures that case, and saying so is the point.
func TestNoKiloTraceRecordsASubagent(t *testing.T) {
	for _, name := range []string{"simple-turn.tsv", "tool-turn.tsv", "blocked-turn.tsv", "error-turn.tsv"} {
		for _, r := range readHookTrace(t, "kilo", name) {
			if contains(r.fields, "info.parentID=present") {
				t.Fatalf("kilo/%s records a child session; the subagent gap is no longer unmeasured", name)
			}
		}
	}
}

// --- Kimi Code CLI ---
//
// Kimi's traces are the first here whose provider half is a table of config
// entries rather than a script, so what they have to prove is slightly
// different: not that an asset's state machine is right, but that every event
// upstream's twelve rows depend on really fires, in the order the ladder
// assumes, on a released Kimi.

// TestKimiTraceProvesTheLadderItsHookTableAssumes reads the one capture that
// walks the whole ladder and asserts the ordered event sequence each of
// upstream's rows rests on.
func TestKimiTraceProvesTheLadderItsHookTableAssumes(t *testing.T) {
	rows := readHookTrace(t, "kimi", "tool-turn-with-permission.tsv")
	assertEvents(t, eventsOf(rows),
		"SessionStart", "UserPromptSubmit", "TurnStarted",
		"PreToolUse", "PermissionRequest", "PermissionResult", "PostToolUse", "Stop")

	// SessionStart identifies the conversation, which is what the session hook
	// exists to bind. The id itself is never in the fixture; its presence is.
	if !contains(rows[0].fields, "session_id") {
		t.Fatalf("SessionStart no longer carries a session_id: %v", rows[0].fields)
	}
	if !contains(rows[0].fields, "source=startup") {
		t.Fatalf("SessionStart no longer carries its own source discriminator: %v", rows[0].fields)
	}

	// The matcher target the two complementary PreToolUse rows are evaluated
	// against. Without a tool name on the payload, upstream's whole
	// AskUserQuestion split would be unimplementable.
	var sawTool bool
	for _, r := range rows {
		if r.event == "PreToolUse" {
			sawTool = r.tool != "-" && r.tool != ""
		}
	}
	if !sawTool {
		t.Fatal("PreToolUse no longer carries a tool name, so the matcher rows cannot fire")
	}
}

// TestKimiResolvesABlockedPaneWithOneEvent is the finding that decides whether
// the blocked lane can be claimed at all, and it is the direct contrast with
// Codex and Claude Code.
//
// Claude Code caps below full authority because a denied permission emits
// nothing, so a hook-driven pane latches on blocked. Codex escapes that only
// because denial takes a *different* event from approval, which an adapter has
// to know about. Kimi has a single PermissionResult that fires either way and
// carries the outcome in a `decision` field, so upstream's one unblocking row
// is sufficient and no denial-specific handling is needed.
func TestKimiResolvesABlockedPaneWithOneEvent(t *testing.T) {
	rows := readHookTrace(t, "kimi", "tool-turn-with-permission.tsv")
	request, result := -1, -1
	for i, r := range rows {
		switch r.event {
		case "PermissionRequest":
			request = i
		case "PermissionResult":
			result = i
		}
	}
	if request < 0 || result < 0 {
		t.Fatalf("the fixture no longer contains the permission pair: %v", eventsOf(rows))
	}
	if result <= request {
		t.Fatal("PermissionResult no longer follows PermissionRequest")
	}
	if !contains(rows[result].fields, "decision") {
		t.Fatalf("PermissionResult no longer carries the field that says how it was resolved: %v", rows[result].fields)
	}

	capability, ok := CapabilityForSource("sidecar.kimi.hooks")
	if !ok {
		t.Fatal("no capability registered for sidecar.kimi.hooks")
	}
	for _, want := range []Transition{TransitionBlockedOnRequest, TransitionUnblocked} {
		if !capability.Covers(want) {
			t.Fatalf("the trace shows %s and the registry does not claim it", want)
		}
	}
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

// TestKimiCancellationIsFirstClassAndCarriesItsReason pins the transition that
// is inferred on OpenCode and absent on Claude Code.
func TestKimiCancellationIsFirstClassAndCarriesItsReason(t *testing.T) {
	rows := readHookTrace(t, "kimi", "cancelled-turn.tsv")
	assertEvents(t, eventsOf(rows), "UserPromptSubmit", "TurnStarted", "Interrupt")

	last := rows[len(rows)-1]
	if !contains(last.fields, "reason=cancelled") {
		t.Fatalf("Interrupt no longer records why it fired: %v", last.fields)
	}
	// No Stop follows it, which is what makes upstream's Interrupt-to-idle row
	// load-bearing rather than redundant.
	for _, e := range eventsOf(rows) {
		if e == "Stop" {
			t.Fatal("the cancelled turn now contains a Stop; the recorded finding is that it does not")
		}
	}
}

// TestKimiProcessExitIsUnclaimedByChoice is the same shape as Pi's
// session_shutdown gap: the event exists, it is readable, and the port does not
// subscribe because upstream's table does not.
func TestKimiProcessExitIsUnclaimedByChoice(t *testing.T) {
	rows := readHookTrace(t, "kimi", "session-end.tsv")
	assertEvents(t, eventsOf(rows), "SessionEnd")
	if !contains(rows[0].fields, "reason=exit") {
		t.Fatalf("SessionEnd no longer records reason=exit: %v", rows[0].fields)
	}

	capability, ok := CapabilityForSource("sidecar.kimi.hooks")
	if !ok {
		t.Fatal("no capability registered for sidecar.kimi.hooks")
	}
	if capability.Covers(TransitionProcessExit) {
		t.Fatal("kimi claims process_exit; the shipped hook table does not carry a SessionEnd row")
	}
}

// TestKimiNonInteractiveRunsSkipThePermissionPair records why the blocked
// evidence had to come from a real TUI.
//
// The absence is bounded by two rows a reader can see rather than by a watch:
// PreToolUse is followed directly by PostToolUse. A `kimi -p` run has nobody to
// ask, so it approves and proceeds, and no capture taken that way could ever
// show the blocked lane.
func TestKimiNonInteractiveRunsSkipThePermissionPair(t *testing.T) {
	events := eventsOf(readHookTrace(t, "kimi", "exec-turn-auto-approves.tsv"))
	assertEvents(t, events,
		"SessionStart", "UserPromptSubmit", "TurnStarted", "PreToolUse", "PostToolUse", "Stop")
	for _, e := range events {
		if e == "PermissionRequest" || e == "PermissionResult" {
			t.Fatalf("the non-interactive trace now contains %s; the recorded finding is that it contains neither", e)
		}
	}
}

// The OMP traces.
//
// OMP is a rebranded fork of Pi's codebase, so its traces use the same six-column
// hook layout and the two providers look alike at a glance. The tests below are
// the places where they are not alike, and each one exists because the Pi port's
// answer does not transfer.

// TestOmpTraceEarnsExactlyWhatTheAssetReports is the promotion, asserted.
//
// It reads the registry entry and requires the traces to contain the events that
// entry says it reports -- and requires that they contain no event the entry does
// not claim. A future edit that adds a transition to `covered` without a capture
// behind it fails here.
func TestOmpTraceEarnsExactlyWhatTheAssetReports(t *testing.T) {
	cap, ok := CapabilityForSource("sidecar.omp.extension")
	if !ok {
		t.Fatal("no capability registered for sidecar.omp.extension")
	}
	if cap.Evidence != EvidenceRealTrace {
		t.Fatalf("omp evidence = %q; these traces exist to make it real-trace", cap.Evidence)
	}
	if tier, reason := cap.TierFor(StatusCurrent, true); tier != TierAdvisory {
		t.Fatalf("omp exercises %q (%s), want advisory: it is the traced ceiling", tier, reason)
	}

	// Each claimed transition maps to an event that is actually in a trace.
	for _, claim := range []struct {
		transition Transition
		trace      string
		event      string
	}{
		{TransitionSessionIdentity, "simple-turn.tsv", "session_start"},
		{TransitionWorkStart, "simple-turn.tsv", "agent_start"},
		{TransitionTurnComplete, "simple-turn.tsv", "agent_end"},
		{TransitionBlockedOnRequest, "tool-turn-with-approval.tsv", "tool_approval_requested"},
		{TransitionUnblocked, "tool-turn-with-approval.tsv", "tool_approval_resolved"},
	} {
		if !cap.Covers(claim.transition) {
			t.Fatalf("omp no longer claims %s, which %s in %s supports", claim.transition, claim.event, claim.trace)
		}
		if !contains(eventsOf(readHookTrace(t, "omp", claim.trace)), claim.event) {
			t.Fatalf("omp claims %s but no %s appears in %s", claim.transition, claim.event, claim.trace)
		}
	}
	// And nothing else is claimed. cancelled is observable and unclaimed by
	// choice; process_exit is unclaimable; tool_use is unclaimed by choice; there
	// are no subagent events. See the tests below for each.
	for _, absent := range []Transition{
		TransitionToolUse, TransitionProcessExit, TransitionCancelled, TransitionSubagent,
	} {
		if cap.Covers(absent) {
			t.Fatalf("omp claims %q, which no trace here supports", absent)
		}
	}
	if cap.CoversFullLifecycle() {
		t.Fatal("omp claims full lifecycle coverage; cancelled and process_exit are not reported")
	}
}

// TestOmpHasNoSettledEventAndIsIdleIsNoDiscriminator is the trap, measured, and
// it is the one place Pi's answer is actively wrong for this provider.
//
// Pi closes a turn on agent_settled and discards a settlement seen while
// isIdle() is not true. OMP has no agent_settled at all, and the trace shows that
// the guard would not work if it did: ctx.isIdle is FALSE on the agent_end of a
// turn that completed normally and TRUE on the agent_end of a cancelled one. So
// an asset that ported Pi's rule across would never settle a completed turn and
// would settle a cancelled one.
func TestOmpHasNoSettledEventAndIsIdleIsNoDiscriminator(t *testing.T) {
	completed := readHookTrace(t, "omp", "simple-turn.tsv")
	assertEvents(t, eventsOf(completed),
		"session_start", "agent_start", "turn_start", "turn_end", "agent_end")
	for _, r := range completed {
		if r.event == "agent_settled" {
			t.Fatal("an agent_settled row appeared in an OMP trace; OMP's event registry has no such event")
		}
	}
	end := findHookRow(t, completed, "agent_end")
	if !contains(end.fields, "ctx.isIdle=false") {
		t.Fatalf("agent_end on a completed turn no longer reports isIdle false; fields are %v", end.fields)
	}
	if !contains(end.fields, "stopReason=stop") {
		t.Fatalf("agent_end on a completed turn no longer reports stopReason=stop; fields are %v", end.fields)
	}

	cancelled := findHookRow(t, readHookTrace(t, "omp", "denied-tool-and-quit.tsv"), "agent_end")
	if !contains(cancelled.fields, "ctx.isIdle=true") {
		t.Fatalf("agent_end on a cancelled turn no longer reports isIdle true; fields are %v", cancelled.fields)
	}
	// Both directions in one sentence: the field's value is inverted from what a
	// Pi-shaped guard would need on both kinds of end.
	if contains(end.fields, "ctx.isIdle=true") || contains(cancelled.fields, "ctx.isIdle=false") {
		t.Fatal("isIdle is no longer inverted across the two ends, which is the whole comparison")
	}
}

// TestOmpWillContinueIsAbsentRatherThanFalse is why the asset compares against an
// explicit true. The key is on every agent_end payload and its value is absent on
// an ordinary end, so a truthiness test would behave the same but a
// key-presence test would settle nothing.
func TestOmpWillContinueIsAbsentRatherThanFalse(t *testing.T) {
	for _, trace := range []string{"simple-turn.tsv", "tool-turn-with-approval.tsv", "denied-tool-and-quit.tsv"} {
		end := findHookRow(t, readHookTrace(t, "omp", trace), "agent_end")
		if !contains(end.fields, "willContinue") {
			t.Fatalf("%s: agent_end no longer carries a willContinue key at all: %v", trace, end.fields)
		}
		if !contains(end.fields, "willContinue=absent") {
			t.Fatalf("%s: agent_end's willContinue is no longer absent: %v", trace, end.fields)
		}
	}
}

// TestOmpDenialIsTheSameEventAsAnApproval is the contrast this matrix exists for.
//
// Claude Code caps below full because a denial emits nothing at all and the pane
// latches; Codex escapes that only because denial takes a different event from
// approval. OMP needs neither workaround: one event carries the outcome in a
// boolean, so upstream's single unblocking row clears the lane either way and
// `unblocked` is claimed on evidence.
func TestOmpDenialIsTheSameEventAsAnApproval(t *testing.T) {
	approved := findHookRow(t, readHookTrace(t, "omp", "tool-turn-with-approval.tsv"), "tool_approval_resolved")
	denied := findHookRow(t, readHookTrace(t, "omp", "denied-tool-and-quit.tsv"), "tool_approval_resolved")
	if !contains(approved.fields, "approved=true") {
		t.Fatalf("the approved trace no longer records approved=true: %v", approved.fields)
	}
	if !contains(denied.fields, "approved=false") {
		t.Fatalf("the denied trace no longer records approved=false: %v", denied.fields)
	}
	if approved.event != denied.event {
		t.Fatalf("approval and denial now take different events (%s and %s), which would change the port",
			approved.event, denied.event)
	}
	cap, _ := CapabilityForSource("sidecar.omp.extension")
	if !cap.Covers(TransitionUnblocked) {
		t.Fatal("omp does not claim unblocked, which both halves of this comparison support")
	}
}

// TestOmpApprovalIsRequestedAfterToolExecutionStarts pins the ordering that
// decides why the asset ignores tool_execution_start for every tool but `ask`.
//
// The tool's execution start fires first, in the same millisecond as the approval
// request. An asset that treated it as work would publish `working` one event
// before the pane actually blocked.
func TestOmpApprovalIsRequestedAfterToolExecutionStarts(t *testing.T) {
	events := eventsOf(readHookTrace(t, "omp", "tool-turn-with-approval.tsv"))
	start, request := indexOfHookEvent(events, "tool_execution_start"), indexOfHookEvent(events, "tool_approval_requested")
	if start < 0 || request < 0 {
		t.Fatalf("the trace no longer contains both events: %v", events)
	}
	if start > request {
		t.Fatalf("tool_execution_start now follows the approval request, which would change which event opens the block: %v", events)
	}
	// And the turn pair fires twice inside this one agent run, which is why
	// neither is subscribed: a turn here is a provider round trip.
	var turns int
	for _, e := range events {
		if e == "turn_start" {
			turns++
		}
	}
	if turns != 2 {
		t.Fatalf("turn_start fired %d times in one tool turn, want 2; a turn is a provider round trip here", turns)
	}
}

// TestOmpCancellationIsObservableAndStillUnclaimed is the difference from Pi
// stated as a test.
//
// Pi's cancelled and completed turns are byte-identical, so `cancelled` is
// unknowable there. Here the discriminator exists -- stopReason=aborted against
// stopReason=stop -- and the transition is still unclaimed, because upstream's
// mapping does not read it and this port keeps the provider half verbatim. If a
// future asset version reads it, this test is what says the evidence was already
// on file.
func TestOmpCancellationIsObservableAndStillUnclaimed(t *testing.T) {
	completed := findHookRow(t, readHookTrace(t, "omp", "simple-turn.tsv"), "agent_end")
	cancelled := findHookRow(t, readHookTrace(t, "omp", "denied-tool-and-quit.tsv"), "agent_end")
	if !contains(completed.fields, "stopReason=stop") || !contains(cancelled.fields, "stopReason=aborted") {
		t.Fatalf("the two ends no longer differ by stop reason:\ncompleted %v\ncancelled %v",
			completed.fields, cancelled.fields)
	}
	cap, _ := CapabilityForSource("sidecar.omp.extension")
	if cap.Covers(TransitionCancelled) {
		t.Fatal("omp claims cancelled; the shipped asset never reads the stop reason that would distinguish it")
	}
}

// TestOmpSessionShutdownCarriesNothing is why process_exit is unclaimABLE here
// rather than unclaimed by choice.
//
// Pi's session_shutdown carries a reason, and three of its five values are a
// session swap rather than an exit, so a future Pi asset could subscribe and
// release only on `quit`. OMP's event is `{type}` and nothing else, so nothing in
// the payload distinguishes a quit from a swap and no future version of this
// asset can claim the transition from this event alone.
func TestOmpSessionShutdownCarriesNothing(t *testing.T) {
	shutdown := findHookRow(t, readHookTrace(t, "omp", "denied-tool-and-quit.tsv"), "session_shutdown")
	for _, field := range shutdown.fields {
		key, _, hasValue := strings.Cut(field, "=")
		if !hasValue && key != "" {
			t.Fatalf("session_shutdown now carries a payload key %q; process_exit may be claimable after all", key)
		}
		if key == "reason" {
			t.Fatal("session_shutdown now carries a reason, which is Pi's shape and not OMP's")
		}
	}
	cap, _ := CapabilityForSource("sidecar.omp.extension")
	if cap.Covers(TransitionProcessExit) {
		t.Fatal("omp claims process_exit; its session_shutdown says nothing that would support it")
	}
}

// TestOmpSessionStartCarriesATranscriptAndNoReason pins the two facts the
// session-identity claim rests on, and the one field Pi has that OMP does not.
func TestOmpSessionStartCarriesATranscriptAndNoReason(t *testing.T) {
	rows := readHookTrace(t, "omp", "simple-turn.tsv")
	if rows[0].event != "session_start" {
		t.Fatalf("simple-turn.tsv no longer opens on session_start (%s)", rows[0].event)
	}
	for _, want := range []string{"ctx.hasUI=true", "ctx.sessionFile=present", "ctx.sessionId=present"} {
		if !contains(rows[0].fields, want) {
			t.Fatalf("session_start no longer records %s; fields are %v", want, rows[0].fields)
		}
	}
	for _, field := range rows[0].fields {
		if key, _, _ := strings.Cut(field, "="); key == "reason" {
			t.Fatal("session_start now carries a reason; OMP's SessionStartEvent has none, which is why the " +
				"asset's forced report always uses session_start and session_switch's uses session_change")
		}
	}
}

// findHookRow returns the last row with the given event, which is the one the
// assertions above mean whenever an event repeats inside a trace.
func findHookRow(t *testing.T, rows []hookRow, event string) hookRow {
	t.Helper()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].event == event {
			return rows[i]
		}
	}
	t.Fatalf("no %s row in the trace", event)
	return hookRow{}
}

func indexOfHookEvent(events []string, want string) int {
	for i, e := range events {
		if e == want {
			return i
		}
	}
	return -1
}

// TestGrokBindsOnlyOnceASessionExists is the finding that decides what grok's
// session-identity port is actually worth, and it is a measurement rather than
// a reading of the documentation.
//
// grok's documentation says SessionStart fires when "a session starts", which
// reads as process start. It does not. A grok TUI was launched into a
// Sidecar-managed shell and left untouched at its prompt for the window this
// trace records, and no hook event fired at all; the event arrived on the first
// prompt, which is when grok creates the session. So a grok pane is bound one
// turn into its life, and an idle pane a user opened and walked away from
// carries no binding at all.
//
// That is the same shape as Antigravity, which has no session event and binds
// on its first PreInvocation, and it is why neither entry can be described as
// binding "at startup".
func TestGrokBindsOnlyOnceASessionExists(t *testing.T) {
	rows := readHookTrace(t, "grok", "session-start-and-end.tsv")
	assertEvents(t, eventsOf(rows), "session_start", "session_end")

	window := captureWindow(t, "grok", "session-start-and-end.tsv")
	if window == "" {
		t.Fatal("the trace records an absence -- nothing fired before the first prompt -- and carries no capture window, " +
			"so a reader cannot tell whether that was measured over a second or a minute")
	}
	d, err := time.ParseDuration(window)
	if err != nil || d < 30*time.Second {
		t.Fatalf("the capture window is %q; an idle TUI has to be watched long enough for a slow startup to be excluded", window)
	}
}

// TestGrokSendsBothSpellingsOfEverySessionField pins the correction the trace
// made to this port's own documentation.
//
// grok's published payload example is entirely camelCase, and the capability
// entry said so before the capture. Every real payload carries both spellings
// of every common field. Sidecar's reader prefers the snake_case one, so it
// reads session_id here; recording that both are present is what stops a future
// reader concluding that the camelCase preference in Herdr's asset was
// load-bearing, or that dropping one spelling would be safe.
func TestGrokSendsBothSpellingsOfEverySessionField(t *testing.T) {
	rows := readHookTrace(t, "grok", "session-start-and-end.tsv")
	for _, r := range rows {
		have := map[string]bool{}
		for _, f := range r.fields {
			have[f] = true
		}
		for _, pair := range [][2]string{
			{"sessionId", "session_id"},
			{"hookEventName", "hook_event_name"},
			{"permissionMode", "permission_mode"},
		} {
			if !have[pair[0]] || !have[pair[1]] {
				t.Fatalf("%s carries %q=%v and %q=%v; the recorded finding is that grok sends both",
					r.event, pair[0], have[pair[0]], pair[1], have[pair[1]])
			}
		}
	}
}

// --- Mastra Code ---
//
// Mastra Code's traces have the same job Kimi's do -- its provider half is a
// table of config entries rather than a script, so what has to be proved is that
// every event the eleven rows depend on really fires, in the order the ladder
// assumes, on a released Mastra Code. One of them has a second job: it is the
// whole evidence for the one row whose event this port moved.

// TestMastracodeSessionStartCarriesNoConversation is the measurement behind this
// port's single correction to upstream's mapping, and it is the reason the
// session binding is on AgentStart.
//
// Herdr binds the pane's conversation on SessionStart. On Mastra Code 0.38.0 the
// hook manager is constructed with the literal session id "session-init" and only
// a thread_created/thread_changed subscription ever replaces it, while the TUI
// dispatches SessionStart immediately after init. In a project with no thread to
// restore, both SessionStart and -- the surprising half -- UserPromptSubmit carry
// the placeholder, because the thread is not created until the prompt is on its
// way. AgentStart is the earliest event that can name the conversation at all.
//
// Binding the placeholder would have marked a constant as an official, resumable
// conversation reference on every machine, which is exactly what agentsession's
// "resuming the wrong conversation is worse than resuming none" forbids.
func TestMastracodeSessionStartCarriesNoConversation(t *testing.T) {
	rows := readHookTrace(t, "mastracode", "session-start-carries-no-thread.tsv")
	assertEvents(t, eventsOf(rows), "SessionStart", "UserPromptSubmit", "AgentStart")

	for _, r := range rows[:2] {
		if !contains(r.fields, "session_id=placeholder") {
			t.Fatalf("%s no longer carries the constructor placeholder: %v; if Mastra Code has "+
				"started naming the thread here, the session binding can move back to SessionStart", r.event, r.fields)
		}
	}
	if !contains(rows[2].fields, "session_id=thread") {
		t.Fatalf("AgentStart no longer carries a real thread id: %v; the shipped session row binds on "+
			"this event and would be binding nothing", rows[2].fields)
	}
}

// TestMastracodeAResumedSessionStartDoesCarryTheThread is the other half, and it
// is why the placeholder is invisible in casual testing: a TUI restarted into an
// existing thread fires thread_changed during init, so SessionStart is correct
// there. A capture that only ever resumed would have made upstream's mapping look
// right.
func TestMastracodeAResumedSessionStartDoesCarryTheThread(t *testing.T) {
	rows := readHookTrace(t, "mastracode", "tool-turn-auto-approved.tsv")
	if rows[0].event != "SessionStart" {
		t.Fatalf("the capture no longer starts at SessionStart: %v", eventsOf(rows))
	}
	if !contains(rows[0].fields, "session_id=thread") {
		t.Fatalf("the resumed SessionStart no longer carries a real thread id: %v", rows[0].fields)
	}
}

// TestMastracodeResolvesABlockedPaneWithOneEvent is the finding that decides
// whether the blocked lane can be claimed, and it is the direct contrast with
// Codex and Claude Code that Kimi's equivalent test also draws.
//
// This capture is stronger than Kimi's in one respect: the decision it records is
// `declined`. Claude Code caps below full authority precisely because a DENIED
// permission emits nothing, so the pane latches. Mastra Code's PermissionResult
// fires on the denial, carries the outcome, and is followed by no PreToolUse at
// all, because the tool never ran.
func TestMastracodeResolvesABlockedPaneWithOneEvent(t *testing.T) {
	rows := readHookTrace(t, "mastracode", "declined-permission-turn.tsv")
	assertEvents(t, eventsOf(rows),
		"UserPromptSubmit", "AgentStart", "Notification", "PermissionRequest",
		"PermissionResult", "Notification", "AgentEnd", "Stop")

	request, result := indexOf(eventsOf(rows), "PermissionRequest"), indexOf(eventsOf(rows), "PermissionResult")
	if result <= request {
		t.Fatal("PermissionResult no longer follows PermissionRequest")
	}
	if !contains(rows[result].fields, "decision=declined") {
		t.Fatalf("the capture no longer records a DENIED permission: %v. An approval alone would not show "+
			"that the pane leaves the blocked lane on a denial, which is the whole claim.", rows[result].fields)
	}
	for _, e := range eventsOf(rows) {
		if e == "PreToolUse" {
			t.Fatal("a declined permission is followed by PreToolUse; the recorded finding is that the tool never runs")
		}
	}

	capability, ok := CapabilityForSource("sidecar.mastracode.hooks")
	if !ok {
		t.Fatal("no capability registered for sidecar.mastracode.hooks")
	}
	for _, want := range []Transition{TransitionBlockedOnRequest, TransitionUnblocked} {
		if !capability.Covers(want) {
			t.Fatalf("the trace shows %s and the registry does not claim it", want)
		}
	}
}

// TestMastracodePermissionPrecedesTheToolHook pins an ordering that is the
// reverse of Kimi's and that a reader of the store would otherwise have to
// discover.
//
// On Kimi, PreToolUse fires and then the permission pair resolves around it. On
// Mastra Code the permission is asked first and PreToolUse only fires if it was
// granted, so a pane reads blocked, then working(permission_resolved), then
// working(tool_use) -- never working(tool_use) then blocked.
func TestMastracodePermissionPrecedesTheToolHook(t *testing.T) {
	declined := eventsOf(readHookTrace(t, "mastracode", "declined-permission-turn.tsv"))
	if indexOf(declined, "PermissionRequest") < 0 {
		t.Fatal("the declined capture no longer holds a permission request")
	}
	if indexOf(declined, "PreToolUse") >= 0 {
		t.Fatal("PreToolUse fired for a tool that was never allowed to run")
	}
	// The auto-approved capture is the same turn shape with the permission pair
	// absent entirely, which is what a session grant or YOLO mode produces.
	auto := eventsOf(readHookTrace(t, "mastracode", "tool-turn-auto-approved.tsv"))
	if indexOf(auto, "PermissionRequest") >= 0 {
		t.Fatal("the auto-approved capture holds a permission request; it exists to show the case with none")
	}
	if indexOf(auto, "PreToolUse") < 0 {
		t.Fatal("the auto-approved capture no longer runs a tool")
	}
}

// TestMastracodeCancellationIsFirstClassButIsFollowedByATurnCompletion pins the
// transition and the limitation together, because the second is the part that
// would otherwise be discovered from a confusing `agent explain`.
//
// Interrupt fires carrying reason=user_interrupt, which the port maps to
// idle/cancelled. But AgentEnd and Stop bracket it, both carrying
// stop_reason=aborted, and upstream's mapping reads neither -- so the LAST report
// of a cancelled turn carries reason=turn_complete. The lane is idle throughout
// and correct; only the reason a later reader sees is the weaker one.
func TestMastracodeCancellationIsFirstClassButIsFollowedByATurnCompletion(t *testing.T) {
	rows := readHookTrace(t, "mastracode", "interrupted-turn.tsv")
	events := eventsOf(rows)

	interrupt := indexOf(events, "Interrupt")
	if interrupt < 0 {
		t.Fatalf("the cancelled capture no longer holds an Interrupt: %v", events)
	}
	if !contains(rows[interrupt].fields, "reason=user_interrupt") {
		t.Fatalf("Interrupt no longer records why it fired: %v", rows[interrupt].fields)
	}
	if events[len(events)-1] != "Stop" {
		t.Fatalf("a cancelled turn no longer ends at Stop: %v", events)
	}
	if !contains(rows[len(rows)-1].fields, "stop_reason=aborted") {
		t.Fatalf("the closing Stop no longer records that the turn was aborted: %v", rows[len(rows)-1].fields)
	}

	capability, ok := CapabilityForSource("sidecar.mastracode.hooks")
	if !ok {
		t.Fatal("no capability registered for sidecar.mastracode.hooks")
	}
	if !capability.Covers(TransitionCancelled) {
		t.Fatal("the trace shows a cancellation and the registry does not claim it")
	}
}

// TestMastracodeProcessExitIsUnclaimedAndUnclaimable is the same shape as Kimi's
// and Pi's gap tests with one difference that matters: for those two the event
// carries a reason and the gap is a choice. Here the event carries none, so even
// an asset that subscribed could not tell an exit from any other session ending.
func TestMastracodeProcessExitIsUnclaimedAndUnclaimable(t *testing.T) {
	rows := readHookTrace(t, "mastracode", "session-end.tsv")
	assertEvents(t, eventsOf(rows), "SessionEnd")
	for _, f := range rows[0].fields {
		if strings.HasPrefix(f, "reason") || strings.HasPrefix(f, "stop_reason") {
			t.Fatalf("SessionEnd now carries %q; the recorded finding is that it carries no discriminator "+
				"at all, which is why process_exit is unclaimable rather than merely unclaimed", f)
		}
	}

	capability, ok := CapabilityForSource("sidecar.mastracode.hooks")
	if !ok {
		t.Fatal("no capability registered for sidecar.mastracode.hooks")
	}
	if capability.Covers(TransitionProcessExit) {
		t.Fatal("mastracode claims process_exit; the shipped hook table does not carry a SessionEnd row")
	}
}

// TestMastracodeTierIsTheCeilingItsTracesReach guards the boundary the rest of
// these tests sit on. `advisory` is what six of the seven transitions
// FullLifecycleTransitions names earn; `full` needs process_exit, which the test
// above shows is unclaimable on this release.
func TestMastracodeTierIsTheCeilingItsTracesReach(t *testing.T) {
	capability, ok := CapabilityForSource("sidecar.mastracode.hooks")
	if !ok {
		t.Fatal("no capability registered for sidecar.mastracode.hooks")
	}
	if capability.Tier != TierAdvisory {
		t.Fatalf("the mastracode source claims %q; its traces earn advisory", capability.Tier)
	}
	if capability.Evidence != EvidenceRealTrace {
		t.Fatalf("the mastracode source claims %q evidence; five captures of 0.38.0 are checked in", capability.Evidence)
	}
	if capability.CoversFullLifecycle() {
		t.Fatal("the mastracode source covers every full-lifecycle transition and still claims advisory; " +
			"either the coverage is overstated or the tier is understated")
	}
	var missing []Transition
	for _, want := range FullLifecycleTransitions() {
		if !capability.Covers(want) {
			missing = append(missing, want)
		}
	}
	if len(missing) != 1 || missing[0] != TransitionProcessExit {
		t.Fatalf("the uncovered full-lifecycle transitions are %v; the recorded finding is that process_exit "+
			"is the only one", missing)
	}
	// Sub-agent activity is untraced rather than unreachable, and the two are
	// different claims. The rows ship; no captured turn spawned one.
	if capability.Covers(TransitionSubagent) {
		t.Fatal("mastracode claims the subagent transition; no checked-in capture holds a SubagentStart")
	}
	for _, name := range []string{"session-start-carries-no-thread.tsv", "declined-permission-turn.tsv",
		"tool-turn-auto-approved.tsv", "interrupted-turn.tsv", "session-end.tsv"} {
		for _, e := range eventsOf(readHookTrace(t, "mastracode", name)) {
			if strings.HasPrefix(e, "Subagent") {
				t.Fatalf("%s records a sub-agent event; the subagent gap is no longer unmeasured", name)
			}
		}
	}
}

// TestGrokNamesASessionOnEveryEventSidecarSubscribesTo is the tier's own
// evidence: the entry claims session_identity and nothing else, so the one
// thing that has to be true is that the event it is registered on names a
// session.
func TestGrokNamesASessionOnEveryEventSidecarSubscribesTo(t *testing.T) {
	for _, r := range readHookTrace(t, "grok", "session-start-and-end.tsv") {
		named := false
		for _, f := range r.fields {
			if f == "session_id" || f == "sessionId" {
				named = true
			}
		}
		if !named {
			t.Fatalf("%s carries no session identifier, so nothing could be bound from it: %v", r.event, r.fields)
		}
	}
}

// TestGrokSessionEndCarriesAReasonSidecarDoesNotSubscribeTo records a gap the
// capability entry states rather than one it hides. SessionEnd fires, carries a
// reason and a transcript path, and would support the process_exit transition;
// the shipped asset does not subscribe to it, so the gap and the covered list
// move together if a future asset version does.
func TestGrokSessionEndCarriesAReasonSidecarDoesNotSubscribeTo(t *testing.T) {
	rows := readHookTrace(t, "grok", "session-start-and-end.tsv")
	var end *hookRow
	for i := range rows {
		if rows[i].event == "session_end" {
			end = &rows[i]
		}
	}
	if end == nil {
		t.Fatal("the trace has no session_end row")
	}
	have := map[string]bool{}
	for _, f := range end.fields {
		have[f] = true
	}
	for _, want := range []string{"reason", "transcript_path", "transcriptPath"} {
		if !have[want] {
			t.Fatalf("session_end does not carry %q: %v", want, end.fields)
		}
	}

	cap, ok := CapabilityForSource("sidecar.grok.hooks")
	if !ok {
		t.Fatal("no capability entry for the grok source")
	}
	if cap.Covers(TransitionProcessExit) {
		t.Fatal("the entry claims process_exit, which the shipped asset does not subscribe to")
	}
	if !cap.Covers(TransitionSessionIdentity) {
		t.Fatal("the entry does not claim session_identity, which is the whole of what it ships")
	}
}

// TestQwenSessionStartFiresBeforeAuthentication is the whole of Qwen's tier,
// re-derived from the capture that earned it.
//
// A session-identity port has one thing to prove and this is it: a released
// build fires the event and the payload names the conversation. What makes the
// Qwen capture worth its own test is the second half -- it was taken while the
// pane sat on the provider picker with no auth type selected, so the event
// belongs to session creation rather than to a configured session. That is the
// fact that makes requalifying this provider cost one process start instead of
// a model turn, and it would be lost the moment it lived only in prose.
func TestQwenSessionStartFiresBeforeAuthentication(t *testing.T) {
	rows := readHookTrace(t, "qwen", "session-start.tsv")
	assertEvents(t, eventsOf(rows), "SessionStart")

	fields := map[string]bool{}
	for _, f := range rows[0].fields {
		fields[strings.SplitN(f, "=", 2)[0]] = true
	}
	// The identifier the binding is made from. Without it the entry installs
	// cleanly and binds nothing, which is the failure this trace rules out.
	if !fields["session_id"] {
		t.Fatal("the captured SessionStart payload carries no session_id, so nothing could bind the pane")
	}
	// A transcript path is present too, so the binding has a fallback shape if
	// the id ever stops arriving.
	if !fields["transcript_path"] {
		t.Error("the payload carries no transcript_path; report-session's path fallback has nothing to use")
	}
	// source is the discriminator, and only startup is traced. A trace edited
	// to claim resume, clear or compact without a capture fails here.
	var source string
	for _, f := range rows[0].fields {
		if strings.HasPrefix(f, "source=") {
			source = strings.TrimPrefix(f, "source=")
		}
	}
	if source != "startup" {
		t.Fatalf("the capture records source=%q; only startup was traced", source)
	}
	// The tier this earns, and no more. Session identity is the ceiling by
	// construction: the asset installs one entry and reports which conversation
	// occupies the pane, never what state it is in.
	capability, ok := CapabilityForSource("sidecar.qwen.hooks")
	if !ok {
		t.Fatal("no capability entry for sidecar.qwen.hooks")
	}
	if capability.Tier != TierSessionIdentity || capability.Evidence != EvidenceRealTrace {
		t.Fatalf("qwen is %s on %s evidence; the capture earns session-identity on real-trace",
			capability.Tier, capability.Evidence)
	}
	if len(capability.Covered) != 1 || !capability.Covers(TransitionSessionIdentity) {
		t.Fatalf("covered = %v; one SessionStart proves session identity and nothing else", capability.Covered)
	}
	if capability.TestedProviderRange != "0.23.0" {
		t.Fatalf("testedProviderRange = %q, want the version the capture was taken from", capability.TestedProviderRange)
	}
}

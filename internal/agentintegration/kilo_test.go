package agentintegration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Kilo suite.
//
// It is in two halves. The first drives the mapping over the fixtures in
// testdata/kilo, which are written against Herdr's kilo integration at version 4
// and against kilo 7.5.9's own event shapes rather than being captured traces --
// see that directory's README for why the distinction matters. The captured
// traces live next door in internal/agentlifecycle/testdata/traces/kilo and are
// what the capability tier rests on. The second half is the installer contract,
// which is the same contract OpenCode's and Pi's suites pin, asserted again here
// because an adapter that satisfies it by accident is an adapter that stops
// satisfying it on the next edit.

// kiloFixture builds a service against a temporary tree with kilo on PATH and
// kilo's config directory already present, which is the state a machine is in
// after kilo has been run once.
func kiloFixture(t *testing.T, opts ...func(*Env)) (Service, Env, kiloPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == KiloProvider {
				return filepath.Join(home, "bin", "kilo"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "7.5.9" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	paths := kiloPathsFor(env)
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, paths
}

func withoutKilo(e *Env) {
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
}

func kiloStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(KiloProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func kiloApply(t *testing.T, s Service, act Action) Plan {
	t.Helper()
	p, err := s.Apply(KiloProvider, act)
	if err != nil {
		t.Fatalf("%s: %v", act, err)
	}
	return p
}

// kiloEvents parses a fixture into handler events.
//
// The column layout is the one the node harness reads, and both readers refuse a
// row that sets both status columns: no real event can, and a fixture that did
// would be asserting the two shapes at once instead of one of them.
func kiloEvents(t *testing.T, name string) []KiloEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "kilo", name))
	if err != nil {
		t.Fatal(err)
	}
	field := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	var events []KiloEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 {
			t.Fatalf("malformed fixture row in %s (%d columns, want 5): %q", name, len(cols), line)
		}
		ev := KiloEvent{
			Type:         cols[1],
			SessionID:    field(cols[2]),
			StatusType:   field(cols[3]),
			StatusString: field(cols[4]),
		}
		if ev.StatusType != "" && ev.StatusString != "" {
			t.Fatalf("fixture row in %s sets both status columns, which no real event can: %q", name, line)
		}
		events = append(events, ev)
	}
	return events
}

// kiloReplay drives a fixture through the handler and returns what it produced.
func kiloReplay(t *testing.T, name string) []KiloAction {
	t.Helper()
	var h KiloHandler
	var actions []KiloAction
	for _, ev := range kiloEvents(t, name) {
		actions = append(actions, h.Handle(ev)...)
	}
	return actions
}

// kiloLanes renders actions the way the assertions below read them: a lane name
// for a state report, "bind:" plus the id for a session binding.
func kiloLanes(actions []KiloAction) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		if a.Kind == agentlifecycle.KindSession {
			out = append(out, "bind:"+a.SessionID)
			continue
		}
		out = append(out, string(a.State)+"/"+string(a.Reason))
	}
	return out
}

func assertKiloLanes(t *testing.T, got []KiloAction, want ...string) {
	t.Helper()
	if lanes := kiloLanes(got); !reflect.DeepEqual(lanes, want) {
		t.Fatalf("actions %v, want %v", lanes, want)
	}
}

// TestKiloReportsAnOrdinaryTurn is the central fixture: a binding, work, and a
// completion, with the repeated busy assertion producing nothing.
func TestKiloReportsAnOrdinaryTurn(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "simple-turn.tsv"),
		"bind:ses-1",
		"working/turn_start",
		"idle/turn_complete",
	)
}

// TestKiloReadsTheSessionStatusObject is the one genuine upstream bug this port
// fixes, asserted rather than described.
//
// Herdr's kilo asset accepts a session.status only when it is a string. Kilo's
// event carries an object whose `type` is the discriminator, so upstream's asset
// maps none of them and falls through to re-reporting the session instead. The
// live traces in internal/agentlifecycle/testdata/traces/kilo record the object
// shape; this is the mapping half of the same claim.
func TestKiloReadsTheSessionStatusObject(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "status-object-is-read.tsv"),
		"bind:ses-1",
		"working/turn_start",
		"idle/turn_complete",
	)
}

// TestKiloStillReadsABareStringStatus is the other direction, and it is what
// makes the fix a widening rather than a replacement. The lookup is also
// case-insensitive, which is upstream's own toLowerCase kept verbatim.
func TestKiloStillReadsABareStringStatus(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "status-string-is-still-read.tsv"),
		"bind:ses-1",
		"working/turn_start",
		"idle/turn_complete",
	)
}

// TestKiloAnUnmappedStatusRebindsRatherThanAssertingALane pins upstream's
// fallback. `retry` is a status kilo really emits and upstream's vocabulary
// really lacks, and the port keeps the vocabulary verbatim: the lane is already
// working from the busy that opened the turn, so re-binding is the safe answer
// and inventing a mapping would be an improvement rather than a port.
func TestKiloAnUnmappedStatusRebindsRatherThanAssertingALane(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "unmapped-status-rebinds.tsv"),
		"bind:ses-1",
		"bind:ses-2",
	)
}

// TestKiloBlocksOnAPermissionAndResolvesOnTheReply is the blocked lane, which is
// the one the notification lane cares about most.
func TestKiloBlocksOnAPermissionAndResolvesOnTheReply(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "permission-blocks-and-resolves.tsv"),
		"bind:ses-1",
		"working/turn_start",
		"blocked/permission_request",
		"working/permission_resolved",
		"idle/turn_complete",
	)
}

// TestKiloTreatsAQuestionLikeAPermission covers the second blocking pair, and the
// suppression that follows it: question.rejected after a question.replied does
// not change the lane, so it emits nothing.
func TestKiloTreatsAQuestionLikeAPermission(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "question-blocks-and-resolves.tsv"),
		"bind:ses-1",
		"blocked/question",
		"working/permission_resolved",
		"blocked/question",
		"working/permission_resolved",
	)
}

// TestKiloASessionErrorBlocksAndIsClosedByTheNextStatus keeps upstream's
// surprising mapping and the reason it is safe in one place.
//
// Reporting blocked for an error reads oddly, and the traces show why it does not
// latch: session.status idle arrives a millisecond later. That is also the
// concrete argument for the status fix, because with upstream's string-only read
// nothing would close the lane until session.idle.
func TestKiloASessionErrorBlocksAndIsClosedByTheNextStatus(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "session-error-is-blocked-then-idle.tsv"),
		"bind:ses-1",
		"working/turn_start",
		"blocked/provider_error",
		"idle/turn_complete",
	)
}

// TestKiloToolBranchesExistEvenThoughNothingReachesThem drives the two branches
// that no released kilo can deliver.
//
// tool.execute.before and tool.execute.after are plugin hooks in kilo, invoked
// through Plugin.trigger, and not bus events. traces/kilo/tool-turn.tsv is a turn
// in which a bash tool really ran and neither name appears. The branches stay
// because they cost one comparison and would be correct if kilo ever published
// the names; what does not happen is a `tool_use` claim in the capability entry.
func TestKiloToolBranchesExistEvenThoughNothingReachesThem(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "tool-events-map-to-working.tsv"),
		"bind:ses-1",
		"idle/turn_complete",
		"working/tool_use",
	)

	capability, ok := agentlifecycle.CapabilityForSource(KiloSource)
	if !ok {
		t.Fatal("no capability is registered for the bundled kilo source")
	}
	if capability.Covers(agentlifecycle.TransitionToolUse) {
		t.Fatal("the capability entry claims tool_use, which no kilo event stream can deliver")
	}
}

// TestKiloCompactionReopensTheWorkingLane keeps a pane from reading idle while
// kilo is compacting a session.
func TestKiloCompactionReopensTheWorkingLane(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "compaction-maps-to-working.tsv"),
		"bind:ses-1",
		"idle/turn_complete",
		"working/compaction",
	)
}

// TestKiloBindsOncePerSessionAndAgainOnRotation pins the binding suppression.
//
// Upstream re-reports the binding on every session.updated, and kilo emits one
// per message. Here each report is a subprocess rather than a socket write, so a
// repeat is suppressed and a genuine rotation is not. Herdr's own opencode asset
// at version 10 already compares against a remembered session id for exactly this
// reason, so this is upstream's newer behaviour rather than an invention.
func TestKiloBindsOncePerSessionAndAgainOnRotation(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "session-rotation-rebinds.tsv"),
		"bind:ses-1",
		"bind:ses-2",
	)
}

// TestKiloIgnoresWhatUpstreamIgnores pins the silence. session.deleted is
// explicitly ignored upstream, and everything outside the switch falls through.
func TestKiloIgnoresWhatUpstreamIgnores(t *testing.T) {
	assertKiloLanes(t, kiloReplay(t, "ignored-events-report-nothing.tsv"), "bind:ses-1")
}

// TestKiloChatMessageCarriesTheSessionWithoutBinding pins the difference between
// adopting a session and binding one. Upstream attaches the event's own session
// id to the state report it sends and sends no binding, and the argv is where
// that shows.
func TestKiloChatMessageCarriesTheSessionWithoutBinding(t *testing.T) {
	actions := kiloReplay(t, "chat-message-binds-nothing-but-carries-the-session.tsv")
	assertKiloLanes(t, actions, "working/turn_start")

	var h KiloHandler
	for _, ev := range kiloEvents(t, "chat-message-binds-nothing-but-carries-the-session.tsv") {
		h.Handle(ev)
	}
	if h.Session() != "ses-1" {
		t.Fatalf("the handler adopted session %q, want ses-1", h.Session())
	}
	argv := KiloReportArgs(actions[0], h.Session())
	if !containsArg(argv, "--session-id", "ses-1") {
		t.Fatalf("the state report argv %v does not carry the session id", argv)
	}
}

// TestKiloReportArgsCarryTheAssetVersionAndNoSequence pins the wire shape.
//
// Authority stays scoped to a source at a version, so every state report carries
// one; without it every stored record claims an unknown version and an outdated
// asset can never be detected. And no verb sends a sequence: the store assigns
// under the lock it already holds, which is what the Pi port learned twice.
func TestKiloReportArgsCarryTheAssetVersionAndNoSequence(t *testing.T) {
	state := KiloReportArgs(KiloAction{
		Kind:   agentlifecycle.KindState,
		State:  agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonTurnStart,
	}, "ses-1")
	want := []string{
		"agent", "report",
		"--source", KiloSource,
		"--source-version", KiloAssetVersion,
		"--provider", KiloProvider,
		"--session-id", "ses-1",
		"--state", "working",
		"--reason", "turn_start",
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("state argv %v, want %v", state, want)
	}

	session := KiloReportArgs(KiloAction{Kind: agentlifecycle.KindSession, SessionID: "ses-1"}, "ses-1")
	wantSession := []string{
		"agent", "report-session",
		"--kind", KiloProvider,
		"--source", KiloSource,
		"--id", "ses-1",
	}
	if !reflect.DeepEqual(session, wantSession) {
		t.Fatalf("session argv %v, want %v", session, wantSession)
	}

	for _, argv := range [][]string{state, session} {
		for _, arg := range argv {
			if arg == "--seq" {
				t.Fatalf("argv %v sends a sequence; the store assigns it under the lock it already holds", argv)
			}
		}
	}
}

func containsArg(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

// TestKiloCapabilityIsRegisteredForTheBundledSource ties the asset to the
// registry the resolver actually consults. An asset with no capability entry
// would install, report, and be ignored.
//
// The tier is asserted at exactly what the port earned, and no higher.
// `advisory` is the ceiling this asset can reach rather than a waypoint: `full`
// additionally needs `cancelled`, which upstream's mapping cannot distinguish,
// and `process_exit`, which upstream's asset does not subscribe to at all. The
// traces themselves are asserted next door in agentlifecycle/hooktrace_test.go,
// where each claimed transition is re-derived from the fixture that earned it.
func TestKiloCapabilityIsRegisteredForTheBundledSource(t *testing.T) {
	capability, ok := agentlifecycle.CapabilityForSource(KiloSource)
	if !ok {
		t.Fatalf("no capability is registered for %s, so every report it sends resolves at screen fallback", KiloSource)
	}
	if capability.Provider != KiloProvider {
		t.Fatalf("the capability names provider %q, the adapter is %q", capability.Provider, KiloProvider)
	}
	if capability.AssetVersion != KiloAssetVersion {
		t.Fatalf("the capability records asset version %q, the bundled asset is %q; a report from this asset "+
			"would be treated as outdated", capability.AssetVersion, KiloAssetVersion)
	}
	if capability.Tier != agentlifecycle.TierAdvisory {
		t.Fatalf("the capability claims tier %q; the traces earn advisory and no more", capability.Tier)
	}
	if capability.Evidence != agentlifecycle.EvidenceRealTrace {
		t.Fatalf("the capability claims evidence %q; four captures of kilo 7.5.9 are checked in", capability.Evidence)
	}
	for _, claimed := range []agentlifecycle.Transition{
		agentlifecycle.TransitionWorkStart,
		agentlifecycle.TransitionBlockedOnRequest,
		agentlifecycle.TransitionUnblocked,
		agentlifecycle.TransitionTurnComplete,
		agentlifecycle.TransitionSessionIdentity,
	} {
		if !capability.Covers(claimed) {
			t.Fatalf("the capability does not claim %s, which the shipped asset reports", claimed)
		}
	}
	for _, unclaimed := range []agentlifecycle.Transition{
		agentlifecycle.TransitionToolUse,
		agentlifecycle.TransitionCancelled,
		agentlifecycle.TransitionProcessExit,
		agentlifecycle.TransitionSubagent,
	} {
		if capability.Covers(unclaimed) {
			t.Fatalf("the capability claims %s, which the shipped asset never reports", unclaimed)
		}
	}
	if capability.CoversFullLifecycle() {
		t.Fatal("the capability claims full lifecycle coverage; this asset can produce neither cancelled nor process_exit")
	}
	if _, ok := PortedFromProvider(KiloProvider); !ok {
		t.Fatal("no PortedFrom record for kilo, so the next Herdr sync would silently skip its provider")
	}
}

// TestBundledKiloAssetBehavesLikeTheHandler is the real asset-to-handler
// equivalence check.
//
// It is the same mechanism that caught two live defects in the OpenCode pair
// after a substring test had passed over both. Every fixture is replayed through
// the shipped JavaScript under node and through the Go handler, and the two argv
// lists must be identical element for element.
func TestBundledKiloAssetBehavesLikeTheHandler(t *testing.T) {
	node := requireNode(t, "that the shipped Kilo asset and its Go mirror produce the same reports")

	entries, err := os.ReadDir(filepath.Join("testdata", "kilo"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tsv") {
			fixtures = append(fixtures, e.Name())
		}
	}
	if len(fixtures) == 0 {
		t.Fatal("no kilo fixtures, so this asserts nothing")
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fixture, err := filepath.Abs(filepath.Join("testdata", "kilo", name))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, "replay-harness.mjs", fixture)
			cmd.Dir = filepath.Join("assets", "kilo")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("replaying %s through the asset: %v\n%s", name, err, stderr.String())
			}
			var fromAsset [][]string
			if err := json.Unmarshal(out, &fromAsset); err != nil {
				t.Fatalf("harness output is not JSON: %q (%v)", out, err)
			}

			var h KiloHandler
			var fromHandler [][]string
			for _, ev := range kiloEvents(t, name) {
				for _, action := range h.Handle(ev) {
					fromHandler = append(fromHandler, KiloReportArgs(action, h.Session()))
				}
			}
			if len(fromAsset) == 0 {
				fromAsset = nil
			}
			if !reflect.DeepEqual(fromAsset, fromHandler) {
				t.Fatalf("the shipped asset and the Go handler disagree on %s.\nasset:   %v\nhandler: %v",
					name, fromAsset, fromHandler)
			}
		})
	}
}

// TestTheKiloAssetExportsOnlyPluginFactories guards a failure mode no Go test
// can see, and one where kilo is measurably laxer than the OpenCode it forked
// from.
//
// Kilo's loader walks the module namespace and skips an export it cannot call, so
// a factory beside a string export still runs; measured on 7.5.9 with a probe
// plugin. OpenCode 1.18.25 drops the whole module in that case, silently, with no
// error anywhere. The asset holds to the stricter rule anyway: the cost is zero,
// the three JavaScript assets Sidecar ships then share one export convention, and
// relying on a fork staying laxer than its upstream is a bet nobody promised to
// honour.
func TestTheKiloAssetExportsOnlyPluginFactories(t *testing.T) {
	node := requireNode(t, "the Kilo asset's export surface")
	cmd := exec.Command(node, "exports-harness.mjs")
	cmd.Dir = filepath.Join("assets", "kilo")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("inspecting the asset's exports: %v\n%s", err, stderr.String())
	}
	var surface struct {
		Names        []string `json:"names"`
		NonFunctions []string `json:"nonFunctions"`
		Factory      bool     `json:"factory"`
	}
	if err := json.Unmarshal(out, &surface); err != nil {
		t.Fatalf("harness output is not JSON: %q", out)
	}
	if !surface.Factory {
		t.Fatal("SidecarLifecycle is not an exported function; kilo would have nothing to call")
	}
	if len(surface.NonFunctions) != 0 {
		t.Fatalf("non-function exports %v; OpenCode rejects a whole module for this and Sidecar's three "+
			"JavaScript assets hold to one export convention", surface.NonFunctions)
	}
	if len(surface.Names) != 1 || surface.Names[0] != "SidecarLifecycle" {
		t.Fatalf("exports = %v, want exactly [SidecarLifecycle]", surface.Names)
	}
}

// kiloRuntime is the ordering harness's report of one real run of the asset:
// which hooks it returned, what argv every report process was spawned with, and
// the order those processes completed in.
type kiloRuntime struct {
	Order     []string            `json:"order"`
	Argv      map[string][]string `json:"argv"`
	Hooks     []string            `json:"hooks"`
	ElapsedMS int                 `json:"elapsedMs"`
}

func kiloRunOrderingHarness(t *testing.T, node string) kiloRuntime {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(node, "ordering-harness.mjs",
		filepath.Join(dir, "sidecar-stub"),
		filepath.Join(dir, "order.log"),
		filepath.Join(dir, "argv"))
	cmd.Dir = filepath.Join("assets", "kilo")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the ordering harness: %v\n%s", err, stderr.String())
	}
	var result kiloRuntime
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("harness output is not JSON: %q (%v)", out, err)
	}
	return result
}

// TestTheKiloAssetSerializesItsReports pins the runtime property the pure
// mapping cannot show, and the one upstream's kilo asset does not have at all.
//
// Each report is a subprocess taking an exclusive lock on an append-only store
// that enforces a strictly increasing sequence per run, so spawning them
// concurrently assigns sequences in order and delivers them out of order, and the
// store correctly rejects the loser. The stub inverts the exit order, so
// serialized and concurrent produce different recorded orders and the assertion
// cannot pass by luck: that list reversed is the signature of concurrent spawns.
func TestTheKiloAssetSerializesItsReports(t *testing.T) {
	node := requireNode(t, "that the shipped Kilo asset serializes its reports")
	result := kiloRunOrderingHarness(t, node)

	want := []string{
		"session",
		"working-turn_start",
		"blocked-permission_request",
		"idle-turn_complete",
	}
	if !reflect.DeepEqual(result.Order, want) {
		t.Fatalf("reports were delivered in order %v, want %v.\n"+
			"That list reversed is the signature of concurrent spawns: the stub inverts the exit order, so "+
			"that is what unserialized delivery produces and it is exactly what the store rejects.",
			result.Order, want)
	}
}

// TestTheKiloAssetReturnsTheHooksItClaimsTo closes the gap the replay harness
// leaves open.
//
// The replay harness drives mapEvent and buildArgs directly, so the asset's
// runtime wiring is untested by it: a hook named "chat.messages", a flattener
// that read properties.session_id, or one that stopped reading properties.status
// would each pass the whole suite while the shipped plugin silently never
// reported a turn correctly. This runs the real factory against a stub kilo and
// asserts what it returned and what it actually spawned.
func TestTheKiloAssetReturnsTheHooksItClaimsTo(t *testing.T) {
	node := requireNode(t, "the Kilo asset's own hooks and the argv it spawns")
	result := kiloRunOrderingHarness(t, node)

	// Exactly two hooks. `dispose` is absent on purpose: upstream's kilo asset
	// has never had one, the provider half is kept verbatim, and process_exit is
	// therefore unclaimed. An extra name here is a claim the capability entry does
	// not carry; a missing one is a lane that never gets reported.
	want := []string{"chat.message", "event"}
	got := append([]string(nil), result.Hooks...)
	sortStrings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the asset returned hooks %v, want exactly %v", got, want)
	}

	// The binding is built from properties.sessionID and the idle lane from
	// properties.status.type, which is the object shape kilo really emits. A
	// flattener that read either field wrongly produces different argv here.
	if argv := result.Argv["session"]; !containsArg(argv, "--id", "ses_orderharness") {
		t.Fatalf("the session binding argv %v does not carry the id from properties.sessionID", argv)
	}
	if argv := result.Argv["idle-turn_complete"]; len(argv) == 0 {
		t.Fatal("no idle report was spawned; the asset did not read properties.status.type")
	}
	for label, argv := range result.Argv {
		if len(argv) < 2 || argv[0] != "agent" {
			t.Fatalf("the %s report spawned %v, which is not a `sidecar agent ...` invocation", label, argv)
		}
		for _, arg := range argv {
			if arg == "--seq" {
				t.Fatalf("the %s report sends a sequence: %v", label, argv)
			}
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// TestTheKiloAssetFailsOpen checks the properties that keep a reporting failure
// from ever becoming the agent's problem.
func TestTheKiloAssetFailsOpen(t *testing.T) {
	asset := KiloAsset()
	if asset == "" {
		t.Fatal("the bundled asset is empty")
	}
	if !strings.HasPrefix(asset, Marker((KiloAdapter{}).asset())) {
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
}

// TestKiloConfigDirIsResolvedTheWayKiloResolvesIt pins the directory facts, all
// of which were measured against kilo 7.5.9 rather than read from Herdr, which
// hardcodes ~/.config/kilo and honours no override at all.
func TestKiloConfigDirIsResolvedTheWayKiloResolvesIt(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        Env
		want       string
		wantPlugin string
	}{
		{
			name: "the XDG default",
			env:  Env{Home: "/home/u"},
			want: filepath.Join("/home/u", ".config", "kilo"),
		},
		{
			name: "XDG_CONFIG_HOME moves it",
			env:  Env{Home: "/home/u", ConfigHome: "/elsewhere/config"},
			want: filepath.Join("/elsewhere/config", "kilo"),
		},
		{
			name: "KILO_CONFIG_DIR wins outright",
			env:  Env{Home: "/home/u", ConfigHome: "/elsewhere/config", KiloConfigDir: "/scratch/kilo"},
			want: "/scratch/kilo",
		},
		{
			name: "a whitespace-only override is not a directory named space",
			env:  Env{Home: "/home/u", KiloConfigDir: "   "},
			want: filepath.Join("/home/u", ".config", "kilo"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := kiloConfigDir(tc.env); got != tc.want {
				t.Fatalf("kiloConfigDir = %q, want %q", got, tc.want)
			}
			paths := kiloPathsFor(tc.env)
			if want := filepath.Join(tc.want, KiloOwnedDir, KiloAssetName); paths.Owned != want {
				t.Fatalf("owned path %q, want %q", paths.Owned, want)
			}
			if want := filepath.Join(tc.want, KiloConflictDir, KiloAssetName); paths.Conflict != want {
				t.Fatalf("conflict path %q, want %q", paths.Conflict, want)
			}
		})
	}
}

// -- the installer contract ------------------------------------------------

func TestKiloInstallIntoACleanTreeIsExplicitAndIdempotent(t *testing.T) {
	svc, env, paths := kiloFixture(t)

	before := kiloStatus(t, svc)
	if before.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("a clean tree reports %s", before.Status)
	}
	if before.EffectiveTier != agentlifecycle.TierScreenFallback {
		t.Fatalf("nothing is installed but the tier is %s", before.EffectiveTier)
	}

	plan := kiloApply(t, svc, ActionInstall)
	if plan.Unchanged {
		t.Fatal("installing into a clean tree changed nothing")
	}
	kinds := make([]OpKind, 0, len(plan.Ops))
	for _, op := range plan.Ops {
		kinds = append(kinds, op.Kind)
	}
	if !reflect.DeepEqual(kinds, []OpKind{OpMkdir, OpWrite}) {
		t.Fatalf("install performed %v, want a directory and a write", kinds)
	}

	data, err := os.ReadFile(paths.Owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != (KiloAdapter{}).asset().Content {
		t.Fatal("the installed bytes are not the bundled asset")
	}
	if _, err := os.Stat(paths.Conflict); !os.IsNotExist(err) {
		t.Fatalf("install wrote into %s, which Sidecar does not own", paths.ConflictDir)
	}

	after := kiloStatus(t, svc)
	if after.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("after install the status is %s", after.Status)
	}
	if after.EffectiveTier != agentlifecycle.TierAdvisory {
		t.Fatalf("a current install exercises %s (%s), want advisory", after.EffectiveTier, after.TierReason)
	}

	// Idempotent: a second install and an update over a current install both
	// converge to no operations rather than rewriting bytes that already match.
	for _, act := range []Action{ActionInstall, ActionUpdate} {
		again := kiloApply(t, svc, act)
		if !again.Unchanged || len(again.Ops) != 0 {
			t.Fatalf("a second %s was not a no-op: %+v", act, again)
		}
	}
	_ = env
}

// TestKiloRefusesWhenKiloHasNeverBeenSetUp keeps Herdr's own refusal semantics.
// Kilo creates its config directory on first run, so its absence means kilo has
// never run here, and creating a whole config tree for an agent that may be about
// to be configured elsewhere is Sidecar inventing a provider's private state.
func TestKiloRefusesWhenKiloHasNeverBeenSetUp(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	if err := os.RemoveAll(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Plan(KiloProvider, ActionInstall)
	r := refusalFrom(t, err)
	if r.Code != RefuseProviderMissing {
		t.Fatalf("refusal code %s, want %s", r.Code, RefuseProviderMissing)
	}
	if !strings.Contains(r.Message, "KILO_CONFIG_DIR") {
		t.Fatalf("the refusal does not name the override that would fix it: %s", r.Message)
	}

	// The status has to carry the same sentence, or a machine where kilo is on
	// PATH but has never been run reads as a plain not-installed with no
	// explanation of why no action is offered.
	st := kiloStatus(t, svc)
	if !strings.Contains(st.Message, "has not been set up on this machine") {
		t.Fatalf("the status is silent about why no install is offered: %q", st.Message)
	}
	for _, act := range st.Offered {
		if act == ActionInstall {
			t.Fatal("install is offered but would refuse")
		}
	}
	if _, err := os.Stat(paths.ConfigDir); !os.IsNotExist(err) {
		t.Fatal("the refused install created kilo's config directory anyway")
	}
}

func TestAMissingKiloProviderRefusesInstallButStillAllowsCleanup(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	kiloApply(t, svc, ActionInstall)

	gone, _, _ := kiloFixture(t, func(e *Env) {
		e.Home = filepath.Dir(filepath.Dir(filepath.Dir(paths.ConfigDir)))
	})
	_ = gone

	svcNoProvider := Service{Env: svc.Env, Adapters: DefaultAdapters()}
	withoutKilo(&svcNoProvider.Env)

	st, err := svcNoProvider.Status(KiloProvider)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status %s, want provider-missing", st.Status)
	}
	if _, err := svcNoProvider.Plan(KiloProvider, ActionInstall); err == nil {
		t.Fatal("install was accepted with no kilo on PATH")
	}
	// Uninstall must still work: removing the provider must not strand the asset.
	plan, err := svcNoProvider.Apply(KiloProvider, ActionUninstall)
	if err != nil {
		t.Fatalf("uninstall with no provider: %v", err)
	}
	if plan.Unchanged {
		t.Fatal("uninstall removed nothing")
	}
	if _, err := os.Stat(paths.Owned); !os.IsNotExist(err) {
		t.Fatal("the asset is still installed after uninstall")
	}
}

// TestKiloNeverAdoptsOverwritesOrDeletesAFileItDoesNotOwn is the ownership rule,
// which is where Sidecar is deliberately stricter than Herdr: uninstall_kilo
// deletes its file without checking its own marker.
func TestKiloNeverAdoptsOverwritesOrDeletesAFileItDoesNotOwn(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	if err := os.MkdirAll(paths.OwnedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// somebody else's plugin\nexport const Other = async () => ({})\n"
	if err := os.WriteFile(paths.Owned, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	st := kiloStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a foreign file at the asset path reports %s", st.Status)
	}
	for _, act := range []Action{ActionInstall, ActionUpdate, ActionRepair, ActionUninstall} {
		if _, err := svc.Plan(KiloProvider, act); err == nil {
			t.Fatalf("%s was accepted over a file Sidecar does not own", act)
		}
	}
	data, err := os.ReadFile(paths.Owned)
	if err != nil || string(data) != foreign {
		t.Fatal("the foreign file was modified")
	}
}

// TestKiloReportsTheDoubleLoadDirectoryAsDamage is the trap kilo inherits from
// OpenCode: it globs {plugin,plugins}/*.{ts,js}, so a copy in each fires every
// event twice. Measured on 7.5.9 with two probe plugins, one per directory, both
// of which loaded and ran.
func TestKiloReportsTheDoubleLoadDirectoryAsDamage(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	kiloApply(t, svc, ActionInstall)

	if err := os.MkdirAll(paths.ConflictDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Conflict, []byte((KiloAdapter{}).asset().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	st := kiloStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a copy in both directories reports %s", st.Status)
	}
	if !strings.Contains(st.Message, "twice") {
		t.Fatalf("the status does not say what is wrong: %q", st.Message)
	}
	plan := kiloApply(t, svc, ActionRepair)
	if plan.Unchanged {
		t.Fatal("repair did nothing about the duplicate")
	}
	if _, err := os.Stat(paths.Conflict); !os.IsNotExist(err) {
		t.Fatal("repair left the duplicate in place")
	}
	if kiloStatus(t, svc).Status != agentlifecycle.StatusCurrent {
		t.Fatal("repair did not converge")
	}
}

// TestKiloRefusesAForeignDuplicateRatherThanDeletingIt is the other half of the
// same trap. A file Sidecar did not write in the directory it does not own is
// still damage, and it is still not Sidecar's to remove.
func TestKiloRefusesAForeignDuplicateRatherThanDeletingIt(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	kiloApply(t, svc, ActionInstall)

	if err := os.MkdirAll(paths.ConflictDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// somebody else's plugin\n"
	if err := os.WriteFile(paths.Conflict, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Plan(KiloProvider, ActionRepair); err == nil {
		t.Fatal("repair was accepted over a duplicate Sidecar does not own")
	} else if r := refusalFrom(t, err); r.Code != RefuseForeignFile {
		t.Fatalf("refusal code %s, want %s", r.Code, RefuseForeignFile)
	}
	data, err := os.ReadFile(paths.Conflict)
	if err != nil || string(data) != foreign {
		t.Fatal("the foreign duplicate was modified")
	}
}

// TestKiloStatusComesFromTheInstalledBytesNotFromAClaimedVersion pins that a
// hand-edited asset reads as needs-repair rather than as current.
func TestKiloStatusComesFromTheInstalledBytesNotFromAClaimedVersion(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	kiloApply(t, svc, ActionInstall)

	tampered := (KiloAdapter{}).asset().Content + "\n// a line someone added\n"
	if err := os.WriteFile(paths.Owned, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	st := kiloStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("an edited asset reports %s", st.Status)
	}
	if st.EffectiveTier != agentlifecycle.TierScreenFallback {
		t.Fatalf("a damaged asset still exercises %s", st.EffectiveTier)
	}
	kiloApply(t, svc, ActionRepair)
	if kiloStatus(t, svc).Status != agentlifecycle.StatusCurrent {
		t.Fatal("repair did not restore the bundled bytes")
	}
}

// TestKiloUninstallLeavesUnrelatedPluginsExactlyAsItFoundThem is the case a
// machine with Herdr installed is in every day.
func TestKiloUninstallLeavesUnrelatedPluginsExactlyAsItFoundThem(t *testing.T) {
	svc, _, paths := kiloFixture(t)
	kiloApply(t, svc, ActionInstall)

	other := filepath.Join(paths.OwnedDir, "herdr-agent-state.js")
	body := "// installed by herdr\n"
	if err := os.WriteFile(other, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	kiloApply(t, svc, ActionUninstall)
	if _, err := os.Stat(paths.Owned); !os.IsNotExist(err) {
		t.Fatal("Sidecar's own asset survived uninstall")
	}
	data, err := os.ReadFile(other)
	if err != nil || string(data) != body {
		t.Fatal("uninstall touched a plugin that is not Sidecar's")
	}
	if _, err := os.Stat(paths.OwnedDir); err != nil {
		t.Fatal("uninstall removed a directory that still holds another plugin")
	}
}

// TestKiloOfferedActionsAreExactlyTheOnesThatWouldNotRefuse keeps the status
// surface honest: a verb offered is a verb that works.
func TestKiloOfferedActionsAreExactlyTheOnesThatWouldNotRefuse(t *testing.T) {
	svc, _, _ := kiloFixture(t)
	for _, stage := range []string{"clean", "installed"} {
		if stage == "installed" {
			kiloApply(t, svc, ActionInstall)
		}
		st := kiloStatus(t, svc)
		offered := map[Action]bool{}
		for _, act := range st.Offered {
			offered[act] = true
		}
		for _, act := range Actions() {
			_, err := svc.Plan(KiloProvider, act)
			if offered[act] != (err == nil) {
				t.Fatalf("%s: %s is offered=%v but plans with err=%v", stage, act, offered[act], err)
			}
		}
	}
}

// TestTheKiloAdapterReportsEveryPathItTouches is the rule that lets a surface
// name the exact files before asking for confirmation.
func TestTheKiloAdapterReportsEveryPathItTouches(t *testing.T) {
	svc, env, paths := kiloFixture(t)
	st := kiloStatus(t, svc)

	want := []string{paths.Owned, paths.Conflict}
	if !reflect.DeepEqual(st.TargetPaths, want) {
		t.Fatalf("target paths %v, want %v", st.TargetPaths, want)
	}
	if !reflect.DeepEqual(KiloPaths(env), want) {
		t.Fatalf("KiloPaths %v, want %v", KiloPaths(env), want)
	}
	seen := map[string]bool{}
	for _, f := range st.Files {
		seen[f.Path] = true
	}
	for _, p := range []string{paths.OwnedDir, paths.Owned, paths.Conflict, paths.Backup} {
		if !seen[p] {
			t.Fatalf("the adapter inspects %s but does not report on it", p)
		}
	}
}

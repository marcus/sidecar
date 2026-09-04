package agentintegration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/agentresolve"
)

// --- fixtures ---

// kimiFixtureRows reads one of the checked-in TSV fixtures, dropping comments
// and turning "-" into the empty string.
func kimiFixtureRows(t *testing.T, name string) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "kimi", name))
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		for i, c := range cols {
			if c == "-" {
				cols[i] = ""
			}
		}
		rows = append(rows, cols)
	}
	if len(rows) == 0 {
		t.Fatalf("testdata/kimi/%s holds no rows, so it asserts nothing", name)
	}
	return rows
}

// kimiMatches evaluates one of Kimi's hook matchers against an event target.
//
// It exists because Go cannot evaluate the matcher the port ships. Upstream's
// "every tool except the question tool" matcher is `^(?!AskUserQuestion$).*$`,
// and Go's regexp is RE2, which has no lookahead at all — so a test that tried
// to compile the shipped table would not fail on a wrong matcher, it would fail
// to run. That is worth knowing rather than working around silently: the
// matcher is provider-side, Kimi's own engine evaluates it, and whether a
// lookahead compiles there is a recorded gap in the capability entry.
//
// So this understands exactly the two shapes upstream's table uses and refuses
// anything else. A future row with a third shape has to teach this function
// what it means, which is the deliberate step.
func kimiMatches(t *testing.T, matcher, target string) bool {
	t.Helper()
	switch matcher {
	case "":
		return true
	case kimiAskUserQuestionMatcher:
		return target == "AskUserQuestion"
	case kimiOtherToolMatcher:
		return target != "AskUserQuestion"
	}
	t.Fatalf("the port ships matcher %q, which this test does not know how to evaluate; "+
		"teach kimiMatches what it means before adding it", matcher)
	return false
}

// TestKimiHookTableMatchesItsRecordedFixture is the provider half's golden.
//
// The fixture is what a reviewer diffs against Herdr's KIMI_HOOK_EVENTS on the
// next sync, so it has to be the shipped table rather than a description of it.
// Both directions are checked: a row added to the Go table without the fixture
// is as much a stale record as the reverse.
func TestKimiHookTableMatchesItsRecordedFixture(t *testing.T) {
	rows := kimiFixtureRows(t, "hook-table.tsv")
	hooks := KimiHooks()
	if len(rows) != len(hooks) {
		t.Fatalf("the fixture records %d hooks and the port ships %d", len(rows), len(hooks))
	}
	for i, row := range rows {
		if len(row) != 5 {
			t.Fatalf("row %d has %d columns, want 5", i, len(row))
		}
		h := hooks[i]
		verb := "report"
		if h.Session() {
			verb = "report-session"
		}
		got := []string{h.Event, h.Matcher, verb, string(h.State), string(h.Reason)}
		for j := range got {
			if got[j] != row[j] {
				t.Errorf("hook %d column %d: the port has %q, the fixture records %q", i, j, got[j], row[j])
			}
		}
	}
}

// TestKimiFiresExactlyOneHookPerEvent is the property upstream's two
// complementary PreToolUse matchers exist to guarantee, made falsifiable.
//
// Kimi runs every hook matching an event in parallel. Two rows matching one
// (event, target) pair would be two `sidecar agent report` processes racing for
// a store sequence, and which lane the pane ended in would depend on the
// scheduler. Nothing about the port would look wrong; the pane would just be
// wrong sometimes.
func TestKimiFiresExactlyOneHookPerEvent(t *testing.T) {
	// Targets worth asking about: the question tool, an ordinary tool, and the
	// empty target every event with no matcher carries.
	targets := []string{"", "AskUserQuestion", "Shell", "Read", "Task"}
	for _, h := range KimiHooks() {
		for _, target := range targets {
			var matched []string
			for _, other := range KimiHooks() {
				if other.Event != h.Event {
					continue
				}
				if kimiMatches(t, other.Matcher, target) {
					matched = append(matched, fmt.Sprintf("%s/%q", other.Event, other.Matcher))
				}
			}
			if len(matched) > 1 {
				t.Errorf("event %s with target %q fires %d hooks in parallel: %v",
					h.Event, target, len(matched), matched)
			}
		}
	}
}

// TestKimiCommandsRoundTripThroughFields is what the ownership rule rests on.
//
// invokesKimiReport reads an installed command by splitting it on whitespace, so
// an argv element carrying a space would make the command Sidecar writes and the
// command Sidecar recognises two different things — and an uninstall would then
// decline to remove an entry it had installed itself.
func TestKimiCommandsRoundTripThroughFields(t *testing.T) {
	for _, h := range KimiHooks() {
		command := KimiHookCommand(h)
		fields := strings.Fields(command)
		if len(fields) != len(KimiHookArgv(h))+1 {
			t.Fatalf("%s: %q does not split into the binary plus its argv", h.Event, command)
		}
		if fields[0] != "sidecar" {
			t.Fatalf("%s: command starts with %q", h.Event, fields[0])
		}
		for i, want := range KimiHookArgv(h) {
			if fields[i+1] != want {
				t.Fatalf("%s: argv element %d round-tripped as %q, want %q", h.Event, i, fields[i+1], want)
			}
		}
		if !invokesKimiReport(command) {
			t.Fatalf("%s: Sidecar does not recognise its own command %q as its own", h.Event, command)
		}
	}
}

// TestKimiNeverSendsASequence is the seam the Pi port lost twice, stated for
// this port before it can be lost a third time.
//
// A per-event hook process has no counter that survives, and the store assigns
// under the lock it already holds for the append. A --seq here would be a value
// the asset chose, and both times Pi chose one every report was silently
// rejected.
func TestKimiNeverSendsASequence(t *testing.T) {
	for _, argv := range KimiHookArgvCorpus() {
		for _, arg := range argv {
			if arg == "--seq" {
				t.Fatalf("a kimi hook passes --seq: %v", argv)
			}
		}
	}
}

// TestKimiQuestionHooksReportBlockedUntilTheQuestionFinishes is the direct
// translation of upstream's test of the same name
// (src/integration/tests.rs). It is the one branch of the ladder that is
// asymmetric: a question blocks on PreToolUse and can end two different ways,
// so a port that kept only PostToolUse would latch the pane on blocked whenever
// a question was cancelled rather than answered.
func TestKimiQuestionHooksReportBlockedUntilTheQuestionFinishes(t *testing.T) {
	want := []struct {
		event, matcher string
		state          agentactivity.State
	}{
		{"PreToolUse", kimiAskUserQuestionMatcher, agentactivity.StateBlocked},
		{"PostToolUse", kimiAskUserQuestionMatcher, agentactivity.StateWorking},
		{"PostToolUseFailure", kimiAskUserQuestionMatcher, agentactivity.StateWorking},
		{"PreToolUse", kimiOtherToolMatcher, agentactivity.StateWorking},
	}
	for _, w := range want {
		found := false
		for _, h := range KimiHooks() {
			if h.Event == w.event && h.Matcher == w.matcher && h.State == w.state {
				found = true
			}
		}
		if !found {
			t.Errorf("no hook maps %s (%s) to %s", w.event, w.matcher, w.state)
		}
	}
}

// --- the ladder, driven through the real store and resolver ---

// kimiHookFor selects the single hook one (event, target) pair fires, the way
// Kimi's own matcher evaluation would.
func kimiHookFor(t *testing.T, event, target string) KimiHook {
	t.Helper()
	for _, h := range KimiHooks() {
		if h.Event == event && kimiMatches(t, h.Matcher, target) {
			return h
		}
	}
	t.Fatalf("no kimi hook fires for event %s with target %q", event, target)
	return KimiHook{}
}

// kimiEmit stores the report one hook would send, exactly as the installed
// config entry would spawn it: no sequence of its own, so the store assigns.
func kimiEmit(t *testing.T, rig *steelRig, h KimiHook) {
	t.Helper()
	if h.Session() {
		return
	}
	rec := agentlifecycle.Report{
		SchemaVersion: agentlifecycle.SchemaVersion,
		ID:            fmt.Sprintf("kimi-%d", time.Now().UnixNano()),
		Kind:          agentlifecycle.KindState,
		Identity: agentlifecycle.Identity{
			Host:              testHost,
			ServerIncarnation: testServer,
			PaneID:            testPane,
			Provider:          KimiProvider,
			RunID:             "kimi-run-1",
			ProcessGeneration: testGen,
		},
		Source:        KimiSource,
		SourceVersion: KimiAssetVersion,
		ObservedAt:    rig.now,
		State:         h.State,
		Reason:        h.Reason,
	}
	if _, _, err := rig.store.AppendNext(rec); err != nil {
		t.Fatalf("storing %s: %v", h.Event, err)
	}
}

// kimiResolve runs one surface refresh with a capability the test supplies,
// mirroring steelRig.poll's body. Everything except the capability and the
// screen comes from the real StoreSource.
func kimiResolve(rig *steelRig, capability agentlifecycle.Capability) agentlifecycle.Explanation {
	in := agentlifecycle.Input{Now: rig.now, Screen: blankScreen}
	if ev, ok := rig.source.Evidence(agentresolve.PaneRef{Session: testSession}); ok {
		in.Live = ev.Live
		in.ProcessAlive = ev.ProcessAlive
		in.Status = ev.Status
		in.ProviderInTestedRange = ev.ProviderInTestedRange
		in.Latest = ev.Latest
		in.StoreUnavailable = ev.StoreUnavailable
		in.InvalidReports = ev.InvalidReports
	}
	in.Capability = capability
	in.Status = agentlifecycle.StatusCurrent
	return agentlifecycle.Resolve(in).Explanation
}

// TestKimiLaneWalkDrivesEveryBranchOfTheLadder feeds the checked-in turn through
// the real lifecycle store, the real StoreSource and the real resolver, and
// requires the pane to be in the lane the fixture records after every event.
//
// Every branch of the ladder is exercised, including the two a successful turn
// never reaches: a question and a permission request are the only two things
// that block a Kimi pane, and each is driven to its resolving event so a latched
// blocked lane would fail here rather than on a user's machine.
func TestKimiLaneWalkDrivesEveryBranchOfTheLadder(t *testing.T) {
	rig := newSteelRig(t)
	// The registry's own entry, not a synthetic one. The traces under
	// internal/agentlifecycle/testdata/traces/kimi earned it `advisory`, which
	// is a tier that authors state, so this walk is what the shipped build
	// actually does rather than a rehearsal of what it might do later.
	capability, ok := agentlifecycle.CapabilityForSource(KimiSource)
	if !ok {
		t.Fatal("no capability is registered for the kimi source, so its reports would be refused outright")
	}

	seenReasons := map[agentlifecycle.ReasonCode]bool{}
	for i, row := range kimiFixtureRows(t, "lane-walk.tsv") {
		if len(row) != 4 {
			t.Fatalf("row %d has %d columns, want 4", i, len(row))
		}
		event, target, wantLane, wantReason := row[0], row[1], agentactivity.State(row[2]), agentlifecycle.ReasonCode(row[3])

		h := kimiHookFor(t, event, target)
		if h.State != wantLane {
			t.Fatalf("row %d: %s(%q) reports %q, the fixture says %q", i, event, target, h.State, wantLane)
		}
		if h.Reason != wantReason {
			t.Fatalf("row %d: %s(%q) carries reason %q, the fixture says %q", i, event, target, h.Reason, wantReason)
		}
		seenReasons[h.Reason] = true

		rig.advance(2 * time.Second)
		kimiEmit(t, rig, h)
		exp := kimiResolve(rig, capability)
		if exp.State != wantLane {
			t.Fatalf("row %d: after %s(%q) the pane is %q, the fixture says %q (authority %q, fallback %q)",
				i, event, target, exp.State, wantLane, exp.Authority, exp.FallbackReason)
		}
		if exp.Authority != agentlifecycle.AuthorityLifecycle {
			t.Fatalf("row %d: the lane was authored by %q rather than by the hook (fallback %q)",
				i, exp.Authority, exp.FallbackReason)
		}
	}

	// Every reason the port can send has to appear somewhere in the walk, or the
	// fixture is exercising a subset and calling it the ladder.
	for _, h := range KimiHooks() {
		if h.Session() {
			continue
		}
		if !seenReasons[h.Reason] {
			t.Errorf("reason %q is never reached by lane-walk.tsv", h.Reason)
		}
	}
}

// TestTheShippedKimiTierIsTheCeilingItsTracesReach guards the boundary the lane
// walk above sits on.
//
// `advisory` is what six traced transitions earn, and it is the ceiling rather
// than a waypoint: `full` also needs session_identity and process_exit, and
// neither is claimable today. Session identity is refused by agentcatalog.Lookup
// because kimi is a detection-only family, measured live in the proof run as
// `unsupported_kind`. Process exit is unclaimed by choice, because upstream's
// twelve rows carry no SessionEnd and this port keeps the provider half
// verbatim.
//
// So a build that claims more than advisory has either fixed one of those or
// edited the registry, and only the first is honest. The test fails on both, and
// its message says which change would make it legitimate.
func TestTheShippedKimiTierIsTheCeilingItsTracesReach(t *testing.T) {
	capability, ok := agentlifecycle.CapabilityForSource(KimiSource)
	if !ok {
		t.Fatal("no capability is registered for the kimi source, so its reports would be refused outright")
	}
	tier, _ := capability.TierFor(agentlifecycle.StatusCurrent, true)
	if tier != agentlifecycle.TierAdvisory {
		t.Fatalf("the kimi capability exercises %q, not advisory. Reaching full needs session_identity, which "+
			"needs kimi to be a family agentcatalog.Lookup resolves, and process_exit, which needs a SessionEnd "+
			"row this port deliberately does not have. Ship either and update this test in the same change.", tier)
	}
	for _, unclaimed := range []agentlifecycle.Transition{
		agentlifecycle.TransitionSessionIdentity,
		agentlifecycle.TransitionProcessExit,
	} {
		if capability.Covers(unclaimed) {
			t.Fatalf("the registry claims %s; nothing in testdata/traces/kimi shows Sidecar observing it", unclaimed)
		}
	}
}

// --- the installer ---

func kimiFixture(t *testing.T, opts ...func(*Env)) (Service, Env, kimiPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == KimiProvider {
				return filepath.Join(home, "bin", "kimi"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "0.14.0" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, kimiPathsFor(env)
}

// kimiSetUp creates the data directory Kimi would have created, which is the
// precondition every converge verb requires.
func kimiSetUp(t *testing.T, paths kimiPaths) {
	t.Helper()
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func kimiStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(KimiProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// TestInstallingKimiWritesTheBlockAndPreservesEveryOtherLine is the translation
// of upstream's install_kimi_writes_hook_and_updates_config, down to its
// fixture: a config.toml carrying a model setting and a Notification hook of the
// user's, both of which have to survive.
func TestInstallingKimiWritesTheBlockAndPreservesEveryOtherLine(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	writeFileForTest(t, paths.Config,
		"default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\nmatcher = \"task.completed\"\ncommand = \"echo keep\"\ntimeout = 3\n")

	applyTo(t, svc, KimiProvider, ActionInstall)

	got := readFileForTest(t, paths.Config)
	for _, want := range []string{
		"default_model = \"moonshot\"",
		"command = \"echo keep\"",
		kimiMarkerLine(kimiBlockBegin, KimiAssetVersion),
		kimiMarkerLine(kimiBlockEnd, KimiAssetVersion),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the rewritten config.toml does not contain %q", want)
		}
	}
	total, ours, err := kimiHookCounts([]byte(got))
	if err != nil {
		t.Fatalf("the rewritten config.toml does not parse: %v", err)
	}
	if ours != len(KimiHooks()) {
		t.Fatalf("%d Sidecar hooks were written, want %d", ours, len(KimiHooks()))
	}
	if total != len(KimiHooks())+1 {
		t.Fatalf("the file holds %d hooks, want Sidecar's %d plus the user's one", total, len(KimiHooks())+1)
	}
	if st := kimiStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install = %q (%s)", st.Status, st.Message)
	}

	// The backup is the user's original file, byte for byte.
	if backup := readFileForTest(t, paths.Backup); !strings.Contains(backup, "echo keep") ||
		strings.Contains(backup, markerToken) {
		t.Fatal("the backup is not the file as it was found")
	}
}

// TestInstallingKimiIsIdempotent is upstream's
// install_kimi_is_idempotent_for_config_block. A second converge must be a
// visible no-op rather than a second block.
func TestInstallingKimiIsIdempotent(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)

	applyTo(t, svc, KimiProvider, ActionInstall)
	first := readFileForTest(t, paths.Config)

	second, err := svc.Apply(KimiProvider, ActionUpdate)
	if err != nil {
		t.Fatalf("second converge: %v", err)
	}
	if !second.Unchanged || len(second.Ops) != 0 {
		t.Fatalf("a second converge was not a no-op: unchanged=%v ops=%d", second.Unchanged, len(second.Ops))
	}
	if got := readFileForTest(t, paths.Config); got != first {
		t.Fatal("a second converge rewrote the file")
	}
	if n := strings.Count(first, kimiMarkerLine(kimiBlockBegin, KimiAssetVersion)); n != 1 {
		t.Fatalf("the file carries %d begin markers, want 1", n)
	}
}

// TestKimiHonoursKimiCodeHome is upstream's install_kimi_uses_kimi_code_home_env.
// It is what lets a relocated Kimi be found, and it is what lets a proof run
// redirect the provider away from the user's real ~/.kimi-code.
func TestKimiHonoursKimiCodeHome(t *testing.T) {
	relocated := t.TempDir()
	svc, _, paths := kimiFixture(t, func(e *Env) { e.KimiCodeHome = relocated })
	if paths.Config != filepath.Join(relocated, KimiConfigName) {
		t.Fatalf("the adapter targets %s, not the relocated %s", paths.Config, relocated)
	}
	applyTo(t, svc, KimiProvider, ActionInstall)
	if _, err := os.Stat(paths.Config); err != nil {
		t.Fatalf("nothing was written to the relocated config: %v", err)
	}

	// A tilde is expanded the way Kimi expands it, so Sidecar and the provider
	// read the same directory rather than one of them using a directory named
	// literally "~".
	home := t.TempDir()
	if got := kimiHomeDir(home, "~/elsewhere"); got != filepath.Join(home, "elsewhere") {
		t.Fatalf("kimiHomeDir did not expand a tilde: %q", got)
	}
	if got := kimiHomeDir(home, "   "); got != filepath.Join(home, ".kimi-code") {
		t.Fatalf("a whitespace-only override was treated as a directory: %q", got)
	}
}

// TestUninstallingKimiRemovesExactlyItsOwnBlock is the ownership rule in its
// most consequential direction. Herdr's uninstall_kimi deletes its block
// without checking what is in it; Sidecar's removes its own lines and leaves the
// file otherwise byte-identical to what it was before the install.
func TestUninstallingKimiRemovesExactlyItsOwnBlock(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	original := "default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n"
	writeFileForTest(t, paths.Config, original)

	applyTo(t, svc, KimiProvider, ActionInstall)
	applyTo(t, svc, KimiProvider, ActionUninstall)

	if got := readFileForTest(t, paths.Config); got != original {
		t.Fatalf("uninstall did not restore the file it started from.\ngot:\n%s\nwant:\n%s", got, original)
	}
	if st := kimiStatus(t, svc); st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status after uninstall = %q (%s)", st.Status, st.Message)
	}

	// And a second uninstall is a no-op rather than an error.
	again, err := svc.Apply(KimiProvider, ActionUninstall)
	if err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
	if !again.Unchanged {
		t.Fatal("a second uninstall was not a no-op")
	}
}

// TestUninstallingKimiRemovesAFileItCreated covers the other end: a config.toml
// that held nothing but Sidecar's block is a file Sidecar created, so uninstall
// takes it with it rather than leaving an empty one behind.
func TestUninstallingKimiRemovesAFileItCreated(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)

	applyTo(t, svc, KimiProvider, ActionInstall)
	applyTo(t, svc, KimiProvider, ActionUninstall)

	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("config.toml survived an uninstall that removed the only thing in it: %v", err)
	}
	if _, err := os.Stat(paths.Backup); err != nil {
		t.Fatalf("no recoverable copy was kept: %v", err)
	}
}

// TestKimiRefusesAForeignHookInsideItsBlock is the refusal that keeps the block
// from becoming a place a user's work can be lost.
func TestKimiRefusesAForeignHookInsideItsBlock(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	applyTo(t, svc, KimiProvider, ActionInstall)

	// Somebody adds a hook of their own inside the markers.
	content := readFileForTest(t, paths.Config)
	end := kimiMarkerLine(kimiBlockEnd, KimiAssetVersion)
	content = strings.Replace(content, end,
		"[[hooks]]\nevent = \"Notification\"\ncommand = \"echo mine\"\ntimeout = 3\n\n"+end, 1)
	writeFileForTest(t, paths.Config, content)

	st := kimiStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("status = %q, want needs-repair (%s)", st.Status, st.Message)
	}
	_, err := svc.Plan(KimiProvider, ActionUninstall)
	r, ok := AsRefusal(err)
	if !ok || r.Code != RefuseForeignFile {
		t.Fatalf("uninstall was not refused as a foreign file: %v", err)
	}
	if got := readFileForTest(t, paths.Config); !strings.Contains(got, "echo mine") {
		t.Fatal("the user's hook did not survive a refused uninstall")
	}
}

// TestKimiRefusesAStrayCopyOutsideItsBlock is the duplicate-report case. A hook
// invoking Sidecar's source outside Sidecar's block reports independently, so
// leaving it there doubles every event — and Sidecar may not edit outside its
// own region to remove it.
func TestKimiRefusesAStrayCopyOutsideItsBlock(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	stray := "[[hooks]]\nevent = \"Stop\"\ncommand = " +
		tomlBasicString(KimiHookCommand(kimiHookFor(t, "Stop", ""))) + "\ntimeout = 10\n"
	writeFileForTest(t, paths.Config, stray)

	st := kimiStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("status = %q, want needs-repair (%s)", st.Status, st.Message)
	}
	for _, act := range []Action{ActionRepair, ActionUninstall} {
		_, err := svc.Plan(KimiProvider, act)
		r, ok := AsRefusal(err)
		if !ok || r.Code != RefuseForeignFile {
			t.Fatalf("%s was not refused as a foreign file: %v", act, err)
		}
	}
	if got := readFileForTest(t, paths.Config); got != stray {
		t.Fatal("a refused mutation changed the file anyway")
	}
}

// TestKimiRefusesAConfigItCannotInterpret covers the two shapes the line editor
// declines rather than guesses at. Both fail in the safe direction: nothing is
// written, and the refusal names the file and the reason.
func TestKimiRefusesAConfigItCannotInterpret(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"not valid toml", "this is not = = toml\n"},
		{"a multi-line string", "prompt = \"\"\"\nhello\n\"\"\"\n"},
		{"a begin marker with no end", kimiMarkerLine(kimiBlockBegin, KimiAssetVersion) + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, paths := kimiFixture(t)
			kimiSetUp(t, paths)
			writeFileForTest(t, paths.Config, tc.content)

			if st := kimiStatus(t, svc); st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("status = %q, want needs-repair (%s)", st.Status, st.Message)
			}
			for _, act := range Actions() {
				if _, err := svc.Plan(KimiProvider, act); err == nil {
					t.Fatalf("%s was allowed against a config Sidecar cannot read", act)
				}
			}
			if got := readFileForTest(t, paths.Config); got != tc.content {
				t.Fatal("the file changed despite every action refusing")
			}
		})
	}
}

// TestKimiSaysWhyItCannotInstallIntoAFileItCannotAppendTo is the sibling of the
// data-directory message, for the other way install can be absent from Offered
// on a file that otherwise reads as a plain not-installed.
//
// Kimi's schema wants `[[hooks]]`, an array of tables. A user who wrote
// `[hooks]` instead has a file Sidecar cannot append its block to at all: the
// composed image is not valid TOML, so the oracle refuses. The refusal itself
// was already exact. What was missing was the status, which showed
// "not installed", no message, and no install action, which is how a correct
// refusal reads as a broken surface.
func TestKimiSaysWhyItCannotInstallIntoAFileItCannotAppendTo(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	writeFileForTest(t, paths.Config, "[hooks]\nenabled = true\n")

	st := kimiStatus(t, svc)
	if st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status = %q, want not-installed (%s)", st.Status, st.Message)
	}
	if st.Message == "" {
		t.Fatal("the status offers no install and says nothing about why")
	}
	for _, act := range st.Offered {
		if act == ActionInstall {
			t.Fatal("install was offered against a file the oracle refuses")
		}
	}
	if _, err := svc.Plan(KimiProvider, ActionInstall); err == nil {
		t.Fatal("install was planned against a file the composed image cannot parse as")
	}
}

// TestKimiRefusesWhenTheDataDirectoryIsMissing keeps Herdr's own precondition:
// ~/.kimi-code is created by Kimi, so its absence means Kimi has never run here,
// and Sidecar does not invent a provider's private state tree.
func TestKimiRefusesWhenTheDataDirectoryIsMissing(t *testing.T) {
	svc, _, paths := kimiFixture(t)

	st := kimiStatus(t, svc)
	if st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status = %q, want not-installed", st.Status)
	}
	if !strings.Contains(st.Message, paths.Dir) {
		t.Fatalf("the status does not say why no install is offered: %q", st.Message)
	}
	for _, act := range st.Offered {
		if act == ActionInstall {
			t.Fatal("install was offered on a machine kimi has never run on")
		}
	}
	_, err := svc.Plan(KimiProvider, ActionInstall)
	r, ok := AsRefusal(err)
	if !ok || r.Code != RefuseProviderMissing {
		t.Fatalf("install was not refused for a missing data directory: %v", err)
	}
}

// TestKimiRefusesWhenTheProviderIsNotOnPath is the other precondition, and the
// asymmetry with uninstall is deliberate: a user who removed kimi must still be
// able to clean up after it.
func TestKimiRefusesWhenTheProviderIsNotOnPath(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	applyTo(t, svc, KimiProvider, ActionInstall)

	gone := Service{
		Env:      Env{Home: svc.Env.Home, UID: os.Getuid(), LookPath: func(string) (string, error) { return "", errors.New("not found") }},
		Adapters: DefaultAdapters(),
	}
	if st := kimiStatus(t, gone); st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status = %q, want provider-missing", st.Status)
	}
	if _, err := gone.Plan(KimiProvider, ActionUpdate); err == nil {
		t.Fatal("update was allowed with no kimi on PATH")
	}
	if _, err := gone.Plan(KimiProvider, ActionUninstall); err != nil {
		t.Fatalf("uninstall was refused with no kimi on PATH, so a removed provider cannot be cleaned up: %v", err)
	}
}

// TestUpdatingKimiReplacesAnOutdatedBlock proves the version half of the
// ownership model: a block at an earlier version reads as outdated rather than
// as damaged or as current, install refuses in favour of update, and update
// converges.
func TestUpdatingKimiReplacesAnOutdatedBlock(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	applyTo(t, svc, KimiProvider, ActionInstall)

	// Age the installed block by rewriting both markers to an earlier version.
	content := readFileForTest(t, paths.Config)
	content = strings.ReplaceAll(content, kimiMarkerLine(kimiBlockBegin, KimiAssetVersion), kimiMarkerLine(kimiBlockBegin, "0"))
	content = strings.ReplaceAll(content, kimiMarkerLine(kimiBlockEnd, KimiAssetVersion), kimiMarkerLine(kimiBlockEnd, "0"))
	writeFileForTest(t, paths.Config, content)

	st := kimiStatus(t, svc)
	if st.Status != agentlifecycle.StatusOutdated {
		t.Fatalf("status = %q, want outdated (%s)", st.Status, st.Message)
	}
	if st.InstalledVersion != "0" {
		t.Fatalf("installed version reads as %q", st.InstalledVersion)
	}
	if _, err := svc.Plan(KimiProvider, ActionInstall); err == nil {
		t.Fatal("install was allowed over an existing older block")
	}
	applyTo(t, svc, KimiProvider, ActionUpdate)
	if st := kimiStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after update = %q (%s)", st.Status, st.Message)
	}
	if n := strings.Count(readFileForTest(t, paths.Config), kimiMarkerPrefix); n != 2 {
		t.Fatalf("the file carries %d marker lines after an update, want 2", n)
	}
}

// TestKimiDryRunAndApplyProduceTheSameOps is the honesty property of --dry-run,
// asserted for this adapter rather than assumed from the engine.
func TestKimiDryRunAndApplyProduceTheSameOps(t *testing.T) {
	svc, _, paths := kimiFixture(t)
	kimiSetUp(t, paths)
	writeFileForTest(t, paths.Config, "default_model = \"moonshot\"\n")

	preview, err := svc.Plan(KimiProvider, ActionInstall)
	if err != nil {
		t.Fatal(err)
	}
	applied := applyTo(t, svc, KimiProvider, ActionInstall)
	if len(preview.Ops) != len(applied.Ops) {
		t.Fatalf("the preview described %d operations and the run performed %d", len(preview.Ops), len(applied.Ops))
	}
	for i := range preview.Ops {
		if preview.Ops[i].Kind != applied.Ops[i].Kind || preview.Ops[i].Path != applied.Ops[i].Path ||
			preview.Ops[i].Checksum != applied.Ops[i].Checksum {
			t.Fatalf("operation %d differs between preview and run:\n%+v\n%+v", i, preview.Ops[i], applied.Ops[i])
		}
	}
}

// TestKimiOracleRefusesARewriteThatWouldLoseUserSettings is the second line of
// defence, driven directly rather than through a config the line editor would
// never produce. The line editor is held to its own contract above; this proves
// the parser-backed check would catch it anyway.
func TestKimiOracleRefusesARewriteThatWouldLoseUserSettings(t *testing.T) {
	pre := []byte("default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n")

	if err := kimiOracleConverged(pre, []byte(string(pre)+"\n"+kimiBlock())); err != nil {
		t.Fatalf("a correct rewrite was refused: %v", err)
	}
	for _, tc := range []struct {
		name string
		post string
	}{
		{"drops a user setting", "[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n\n" + kimiBlock()},
		{"drops a user hook", "default_model = \"moonshot\"\n\n" + kimiBlock()},
		{"changes a user setting", "default_model = \"other\"\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n\n" + kimiBlock()},
		{"adds a setting of its own", "default_model = \"moonshot\"\nsidecar_was_here = true\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n\n" + kimiBlock()},
		{"installs half the hooks", "default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := kimiOracleConverged(pre, []byte(tc.post)); err == nil {
				t.Fatal("the oracle accepted a rewrite that changes what Sidecar does not own")
			}
		})
	}
	if err := kimiOracleRemoved(pre, pre); err != nil {
		t.Fatalf("removing nothing from a file with nothing of Sidecar's was refused: %v", err)
	}
	if err := kimiOracleRemoved(pre, []byte(string(pre)+"\n"+kimiBlock())); err == nil {
		t.Fatal("the oracle called an uninstall done while every Sidecar hook was still in the file")
	}
}

// TestKimiOwnershipIsNarrow pins the direction invokesKimiReport is allowed to
// be wrong in. An exotic spelling of a genuine invocation is left alone, at
// worst a duplicate; a user's similar-looking entry being claimed would mean
// their configuration is destroyed.
func TestKimiOwnershipIsNarrow(t *testing.T) {
	ours := KimiHookCommand(kimiHookFor(t, "Stop", ""))
	if !invokesKimiReport(ours) {
		t.Fatal("Sidecar does not recognise its own command")
	}
	if !invokesKimiReport("/usr/local/bin/sidecar agent report --source " + KimiSource + " --state idle") {
		t.Fatal("an absolute path to the sidecar binary was not recognised")
	}
	if !invokesKimiReport(`C:\tools\sidecar.exe agent report --source ` + KimiSource + " --state idle") {
		t.Fatal("a Windows-shaped invocation was not recognised, so an uninstall there would leave it behind")
	}
	for _, command := range []string{
		"",
		"echo sidecar agent report --source " + KimiSource,
		"sidecar-helper agent report --source " + KimiSource,
		"sidecar agent explain --source " + KimiSource,
		"sidecar agent report --source sidecar.pi.extension --state idle",
		"sidecar agent report --state idle",
		"my-wrapper " + ours,
	} {
		if invokesKimiReport(command) {
			t.Errorf("Sidecar claimed a command that is not its own: %q", command)
		}
	}
}

package agentintegration

import (
	"encoding/json"
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

// mastracodeFixtureRows reads one of the checked-in TSV fixtures, dropping
// comments and turning "-" into the empty string.
func mastracodeFixtureRows(t *testing.T, name string) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "mastracode", name))
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
		t.Fatalf("testdata/mastracode/%s holds no rows, so it asserts nothing", name)
	}
	return rows
}

// TestMastracodeHookTableMatchesItsRecordedFixture is the provider half's
// golden.
//
// The fixture is what a reviewer diffs against Herdr's MASTRACODE_HOOK_EVENTS on
// the next sync, so it has to be the shipped table rather than a description of
// it. Both directions are checked: a row added to the Go table without the
// fixture is as much a stale record as the reverse.
func TestMastracodeHookTableMatchesItsRecordedFixture(t *testing.T) {
	rows := mastracodeFixtureRows(t, "hook-table.tsv")
	hooks := MastracodeHooks()
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
			verb = reportSessionVerb
		}
		blocking := "no"
		if h.Blocking() {
			blocking = "yes"
		}
		got := []string{h.Event, blocking, verb, string(h.State), string(h.Reason)}
		for j := range got {
			if got[j] != row[j] {
				t.Errorf("hook %d column %d: the port has %q, the fixture records %q", i, j, got[j], row[j])
			}
		}
	}
}

// TestMastracodeFiresExactlyOneHookPerEvent is the property upstream's table has
// by construction, made falsifiable.
//
// Mastra Code dispatches the hooks for one event sequentially rather than in
// parallel, so two rows on one event would not race the way Kimi's would. They
// would still be two reports for one provider event, which is a lane written
// twice and a store sequence spent twice, and there is no event here where that
// would be right. Sidecar's own converge oracle also assumes one entry per event
// when it decides current-versus-outdated.
func TestMastracodeSendsEachReportAtMostOncePerEvent(t *testing.T) {
	state := map[string]int{}
	session := map[string]int{}
	for _, h := range MastracodeHooks() {
		if h.Session() {
			session[h.Event]++
			continue
		}
		state[h.Event]++
	}
	for event, n := range state {
		if n > 1 {
			t.Errorf("%d state rows fire on %s; one provider event may author one lane", n, event)
		}
	}
	for event, n := range session {
		if n > 1 {
			t.Errorf("%d session rows fire on %s; one provider event may bind the pane once", n, event)
		}
	}
	// AgentStart is the one event with two rows, and they are two different
	// reports rather than two of the same. Named rather than derived, so moving
	// the session binding somewhere else is a deliberate edit here too.
	if session["AgentStart"] != 1 || state["AgentStart"] != 1 {
		t.Errorf("AgentStart carries %d session rows and %d state rows, want one of each: it binds the "+
			"conversation and reports the lane, because SessionStart's payload carries no thread id",
			session["AgentStart"], state["AgentStart"])
	}
	if session["SessionStart"] != 0 {
		t.Error("the session binding is back on SessionStart, whose payload on Mastra Code 0.38.0 is the " +
			"literal \"session-init\" on every session; see mastracode.go")
	}
}

// TestEveryMastracodeEventIsOneMastraCodeActuallyEmits is the guard against a
// row that reports nothing because the provider has no such event.
//
// Mastra Code's loader keeps only the fourteen keys in its own VALID_EVENTS and
// silently drops every other top-level key, so a misspelled event is not an
// error anywhere: the entry is written, never fires, and the lane it was meant
// to report simply never arrives. mastracodeEventNames is that list, transcribed
// from @mastra/code-sdk/dist/hooks/config.js at 0.38.0.
func TestEveryMastracodeEventIsOneMastraCodeActuallyEmits(t *testing.T) {
	for _, h := range MastracodeHooks() {
		if !mastracodeEventNames[h.Event] {
			t.Errorf("the port installs a hook on %q, which is not one of Mastra Code's own VALID_EVENTS; "+
				"the entry would be written, never fire, and never be noticed", h.Event)
		}
	}
	if len(mastracodeEventNames) != 14 {
		t.Errorf("mastracodeEventNames holds %d events; Mastra Code 0.38.0 declares 14", len(mastracodeEventNames))
	}
}

// TestMastracodeCannotBlockTheAgentItReportsOn is the guard on the one failure
// mode of this port that would change what a user's agent does.
//
// Mastra Code reads exit code 2 from a hook on PreToolUse, Stop or
// UserPromptSubmit as a refusal of the tool call or the turn. `sidecar agent
// report` exits 2 on a usage error, and an installed hooks.json outlives the
// binary that wrote it, so a Sidecar downgrade below the release that added a
// flag this asset passes would otherwise refuse every prompt and every tool call
// with Sidecar's own usage text as the reason.
//
// Both directions are asserted. A row on a blocking event without the guard is
// the dangerous one. A row on a non-blocking event WITH the guard is also wrong,
// because it throws away the warning line Mastra Code shows the user for a
// non-zero exit, which is the only place a broken integration is visible from
// inside the agent.
func TestMastracodeCannotBlockTheAgentItReportsOn(t *testing.T) {
	for _, h := range MastracodeHooks() {
		command := MastracodeHookCommand(h)
		guarded := strings.HasSuffix(command, mastracodeFailOpenSuffix)
		switch {
		case h.Blocking() && !guarded:
			t.Errorf("%s is a blocking event and its command is %q; without the fail-open guard an exit "+
				"code of 2 from sidecar refuses the agent's own work", h.Event, command)
		case !h.Blocking() && guarded:
			t.Errorf("%s is not a blocking event and its command carries the fail-open guard, which only "+
				"hides the warning a failing report would otherwise show the user", h.Event)
		}
	}
	// The set itself is the provider's, not the port's, so it is pinned rather
	// than derived from the rows that happen to use it.
	want := map[string]bool{"PreToolUse": true, "Stop": true, "UserPromptSubmit": true}
	for event := range want {
		if !mastracodeBlockingEvents[event] {
			t.Errorf("%s is one of Mastra Code's blocking events and the port does not know it", event)
		}
	}
	for event := range mastracodeBlockingEvents {
		if !want[event] {
			t.Errorf("the port treats %s as blocking; Mastra Code 0.38.0's isBlockingEvent names only "+
				"PreToolUse, Stop and UserPromptSubmit", event)
		}
	}
}

// TestMastracodeTimeoutIsInMilliseconds is one line of test for one line of
// trap.
//
// Every other provider Sidecar installs into counts a hook timeout in seconds
// and shares hookTimeoutSec. Mastra Code's executor reads the field straight
// into setTimeout, so writing hookTimeoutSec here would give every report ten
// milliseconds and SIGKILL it before it could append -- and nothing else in the
// suite would notice, because the entry would still be valid JSON, still load,
// and still be byte-equal to what the oracle expected.
func TestMastracodeTimeoutIsInMilliseconds(t *testing.T) {
	if mastracodeHookTimeoutMS != hookTimeoutSec*1000 {
		t.Fatalf("the mastracode timeout is %d and the shared one is %d seconds; they must name the same duration",
			mastracodeHookTimeoutMS, hookTimeoutSec)
	}
	entry := mastracodeCanonicalEntry(MastracodeHooks()[0])
	var got struct {
		Timeout int `json:"timeout"`
	}
	if err := json.Unmarshal(entry, &got); err != nil {
		t.Fatal(err)
	}
	if got.Timeout != 10000 {
		t.Fatalf("the installed entry carries timeout %d; Mastra Code reads it as milliseconds and 10000 is ten seconds", got.Timeout)
	}
}

// TestMastracodeCommandsRoundTripThroughFields is what the ownership rule rests
// on: it reads a command by splitting it on whitespace, so every argv element
// has to be a single shell word.
func TestMastracodeCommandsRoundTripThroughFields(t *testing.T) {
	for _, h := range MastracodeHooks() {
		argv := MastracodeHookArgv(h)
		command := MastracodeHookCommand(h)
		fields := strings.Fields(command)
		if len(fields) < 1 || fields[0] != "sidecar" {
			t.Fatalf("%s: command %q does not start with the binary name", h.Event, command)
		}
		want := append([]string{"sidecar"}, argv...)
		if h.Blocking() {
			want = append(want, "||", "true")
		}
		if strings.Join(fields, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("%s: command splits to %v, the argv is %v", h.Event, fields, want)
		}
		if !invokesMastracodeReport(command) {
			t.Errorf("%s: the ownership rule does not recognise the command this build installs", h.Event)
		}
	}
}

// TestMastracodeNeverSendsASequence is the seam the Pi port lost twice, stated
// for this provider so a later edit cannot reintroduce it quietly.
func TestMastracodeNeverSendsASequence(t *testing.T) {
	for _, argv := range MastracodeHookArgvCorpus() {
		for _, arg := range argv {
			if arg == "--seq" || strings.HasPrefix(arg, "--seq=") {
				t.Fatalf("a hook argv carries %q; the store assigns, and a per-event hook process has no counter to be right about", arg)
			}
		}
	}
}

// TestMastracodeOwnershipIsNarrow pins the direction invokesMastracodeReport is
// allowed to be wrong in.
//
// It must never adopt a command that merely mentions Sidecar or the verb, and it
// must recognise its own commands whether or not they carry the shell guard,
// because dropping the guard in a later asset version must not orphan the
// entries the current one wrote.
func TestMastracodeOwnershipIsNarrow(t *testing.T) {
	bare := "sidecar agent report --source " + MastracodeSource + " --provider mastracode --state idle"
	for _, ours := range []string{
		bare,
		bare + mastracodeFailOpenSuffix,
		"/opt/homebrew/bin/sidecar agent report --source " + MastracodeSource,
		`C:\tools\sidecar.exe agent report --source ` + MastracodeSource,
		"sidecar agent " + reportSessionVerb + " --kind mastracode --source " + MastracodeSource + " --hook-stdin",
	} {
		if !invokesMastracodeReport(ours) {
			t.Errorf("%q is a command Sidecar installs and the ownership rule does not claim it", ours)
		}
	}
	for _, theirs := range []string{
		"",
		"echo sidecar agent report --source " + MastracodeSource,
		"sidecar-helper agent report --source " + MastracodeSource,
		"sidecar agent explain --source " + MastracodeSource,
		"sidecar agent report --source sidecar.kimi.hooks --provider kimi --state idle",
		"my-wrapper.sh sidecar agent report --source " + MastracodeSource,
	} {
		if invokesMastracodeReport(theirs) {
			t.Errorf("%q is not Sidecar's and the ownership rule claims it; uninstall deletes what it claims", theirs)
		}
	}
}

// --- the lane walk ---

// mastracodeHookFor returns the row that reports a LANE for an event. AgentStart
// carries a session row as well, and a caller asking "what does this event report"
// is asking about the lane.
func mastracodeHookFor(t *testing.T, event string) MastracodeHook {
	t.Helper()
	for _, h := range MastracodeHooks() {
		if h.Event == event && !h.Session() {
			return h
		}
	}
	t.Fatalf("no mastracode state hook fires for event %s", event)
	return MastracodeHook{}
}

// mastracodeEmit stores the report one hook would send, exactly as the installed
// config entry would spawn it: no sequence of its own, so the store assigns.
func mastracodeEmit(t *testing.T, rig *steelRig, h MastracodeHook) {
	t.Helper()
	if h.Session() {
		return
	}
	rec := agentlifecycle.Report{
		SchemaVersion: agentlifecycle.SchemaVersion,
		ID:            fmt.Sprintf("mastracode-%d", time.Now().UnixNano()),
		Kind:          agentlifecycle.KindState,
		Identity: agentlifecycle.Identity{
			Host:              testHost,
			ServerIncarnation: testServer,
			PaneID:            testPane,
			Provider:          MastracodeProvider,
			RunID:             "mastracode-run-1",
			ProcessGeneration: testGen,
		},
		Source:        MastracodeSource,
		SourceVersion: MastracodeAssetVersion,
		ObservedAt:    rig.now,
		State:         h.State,
		Reason:        h.Reason,
	}
	if _, _, err := rig.store.AppendNext(rec); err != nil {
		t.Fatalf("storing %s: %v", h.Event, err)
	}
}

// TestMastracodeLaneWalkDrivesEveryBranchOfTheLadder feeds the checked-in turns
// through the real lifecycle store, the real StoreSource and the real resolver,
// and requires the pane to be in the lane the fixture records after every event.
//
// The capability is the registry's own, which the five checked-in captures of
// mastracode 0.38.0 earned `advisory` for. So this walk is what the shipped build
// actually does rather than a rehearsal of what it might do later: at
// screen-fallback the reports would author nothing and the first row would fail.
func TestMastracodeLaneWalkDrivesEveryBranchOfTheLadder(t *testing.T) {
	rig := newSteelRig(t)
	capability, ok := agentlifecycle.CapabilityForSource(MastracodeSource)
	if !ok {
		t.Fatal("no capability is registered for the mastracode source, so its reports would be refused outright")
	}

	seenReasons := map[agentlifecycle.ReasonCode]bool{}
	for i, row := range mastracodeFixtureRows(t, "lane-walk.tsv") {
		if len(row) != 3 {
			t.Fatalf("row %d has %d columns, want 3", i, len(row))
		}
		event, wantLane, wantReason := row[0], agentactivity.State(row[1]), agentlifecycle.ReasonCode(row[2])

		h := mastracodeHookFor(t, event)
		if h.State != wantLane {
			t.Fatalf("row %d: %s reports %q, the fixture says %q", i, event, h.State, wantLane)
		}
		if h.Reason != wantReason {
			t.Fatalf("row %d: %s carries reason %q, the fixture says %q", i, event, h.Reason, wantReason)
		}
		seenReasons[h.Reason] = true

		rig.advance(2 * time.Second)
		mastracodeEmit(t, rig, h)
		exp := mastracodeResolve(rig, capability)
		if exp.State != wantLane {
			t.Fatalf("row %d: after %s the pane is %q, the fixture says %q (authority %q, fallback %q)",
				i, event, exp.State, wantLane, exp.Authority, exp.FallbackReason)
		}
		if exp.Authority != agentlifecycle.AuthorityLifecycle {
			t.Fatalf("row %d: the lane was authored by %q rather than by the hook (fallback %q)",
				i, event, exp.Authority)
		}
	}

	// Every reason the port can send has to appear somewhere in the walk, or the
	// fixture is exercising a subset and calling it the ladder.
	for _, h := range MastracodeHooks() {
		if h.Session() {
			continue
		}
		if !seenReasons[h.Reason] {
			t.Errorf("reason %q is never reached by lane-walk.tsv", h.Reason)
		}
	}
}

// mastracodeResolve runs one surface refresh with a capability the test supplies,
// mirroring steelRig.poll's body. Everything except the capability and the screen
// comes from the real StoreSource.
func mastracodeResolve(rig *steelRig, capability agentlifecycle.Capability) agentlifecycle.Explanation {
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

// --- the installer ---

func mastracodeFixture(t *testing.T, opts ...func(*Env)) (Service, Env, mastracodePaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == MastracodeProvider {
				return filepath.Join(home, "bin", "mastracode"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string {
			t.Fatal("the mastracode adapter must not probe for a provider version; " +
				"mastracode has no version flag and the probe returns its usage banner")
			return ""
		},
		UID: os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, mastracodePathsFor(env)
}

func mastracodeStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(MastracodeProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// mastracodeHooksJSON reads hooks.json back as a decoded document.
func mastracodeHooksJSON(t *testing.T, path string) map[string][]map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string][]map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("hooks.json is not the shape Mastra Code reads: %v", err)
	}
	return doc
}

// TestInstallingMastracodeCreatesTheDirectoryAndWritesEveryEntry is the
// clean-tree case, and it pins the directory decision this port made against a
// measurement.
//
// ~/.mastracode is not a directory Mastra Code creates on startup the way Pi and
// Kimi create theirs: a real headless run in an empty home created
// ~/Library/Application Support/mastracode and nothing else, and the only thing
// in the package that creates it unasked is the TUI's analytics writer. So an
// installer that refused on its absence could not install before the agent's
// first launch, which is exactly when somebody is likely to be installing.
func TestInstallingMastracodeCreatesTheDirectoryAndWritesEveryEntry(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)

	if st := mastracodeStatus(t, svc); st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status before install is %q, want not-installed", st.Status)
	}
	applyTo(t, svc, MastracodeProvider, ActionInstall)

	doc := mastracodeHooksJSON(t, paths.Config)
	// Read by row rather than by event, because AgentStart carries two: the
	// session binding first, then the lane. The order inside an event array is
	// the order Mastra Code runs them in.
	at := map[string]int{}
	for _, h := range MastracodeHooks() {
		entries := doc[h.Event]
		i := at[h.Event]
		at[h.Event]++
		if len(entries) <= i {
			t.Fatalf("hooks.json holds %d entries under %s, want at least %d", len(entries), h.Event, i+1)
		}
		if got, _ := entries[i]["command"].(string); got != MastracodeHookCommand(h) {
			t.Errorf("%s[%d] carries command %q, not the one this build renders", h.Event, i, got)
		}
		if got, _ := entries[i]["type"].(string); got != "command" {
			t.Errorf("%s[%d] carries type %q; Mastra Code's loader drops an entry whose type is not \"command\"", h.Event, i, got)
		}
	}
	for event, n := range at {
		if len(doc[event]) != n {
			t.Errorf("hooks.json holds %d entries under %s, and the port ships %d", len(doc[event]), event, n)
		}
	}
	if st := mastracodeStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install is %q, want current", st.Status)
	}
}

// TestInstallingMastracodePreservesAHookOfTheUsers is the translation of
// upstream's install_mastracode_preserves_existing_hooks, down to its fixture:
// a hooks.json carrying a Notification hook of the user's and a PreToolUse hook
// of theirs, both of which have to survive on the events Sidecar also writes to.
func TestInstallingMastracodePreservesAHookOfTheUsers(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	before := `{
  "Notification": [
    {
      "type": "command",
      "command": "say 'mastracode wants you'",
      "timeout": 5000
    }
  ],
  "PreToolUse": [
    {
      "type": "command",
      "command": "/usr/local/bin/audit-tool",
      "matcher": { "tool_name": "execute_command" }
    }
  ]
}
`
	writeFileForTest(t, paths.Config, before)
	applyTo(t, svc, MastracodeProvider, ActionInstall)

	doc := mastracodeHooksJSON(t, paths.Config)
	notification := doc["Notification"]
	if len(notification) != 1 {
		t.Fatalf("Notification holds %d entries; Sidecar writes none there and the user wrote one", len(notification))
	}
	if got, _ := notification[0]["command"].(string); got != "say 'mastracode wants you'" {
		t.Errorf("the user's Notification hook became %q", got)
	}
	pre := doc["PreToolUse"]
	if len(pre) != 2 {
		t.Fatalf("PreToolUse holds %d entries, want the user's plus Sidecar's", len(pre))
	}
	if got, _ := pre[0]["command"].(string); got != "/usr/local/bin/audit-tool" {
		t.Errorf("the user's PreToolUse hook is no longer first, or no longer theirs: %q", got)
	}
	if got, _ := pre[0]["matcher"]; got == nil {
		t.Error("the user's matcher was dropped; Sidecar rewrites its own entries and nothing else")
	}
	if got, _ := pre[1]["command"].(string); got != MastracodeHookCommand(mastracodeHookFor(t, "PreToolUse")) {
		t.Errorf("Sidecar's PreToolUse entry is %q", got)
	}
}

// TestInstallingMastracodeIsIdempotent is upstream's
// install_mastracode_is_idempotent: a second install adds nothing and the file
// does not change.
func TestInstallingMastracodeIsIdempotent(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	applyTo(t, svc, MastracodeProvider, ActionInstall)
	first := readFileForTest(t, paths.Config)

	plan, err := svc.Plan(MastracodeProvider, ActionInstall)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !plan.Unchanged || len(plan.Ops) != 0 {
		t.Fatalf("a second install plans %d operations; it should report unchanged", len(plan.Ops))
	}
	if got := readFileForTest(t, paths.Config); got != first {
		t.Error("hooks.json changed between two identical installs")
	}
}

// TestUninstallingMastracodeRemovesExactlyItsOwnEntries is the ownership rule in
// its load-bearing direction: what a user wrote survives byte for byte, and the
// event keys Sidecar's entry was the only occupant of go with it.
func TestUninstallingMastracodeRemovesExactlyItsOwnEntries(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	before := `{
  "Notification": [
    {
      "type": "command",
      "command": "say 'mastracode wants you'",
      "timeout": 5000
    }
  ],
  "Stop": [
    {
      "type": "command",
      "command": "/usr/local/bin/on-stop"
    }
  ]
}
`
	writeFileForTest(t, paths.Config, before)
	applyTo(t, svc, MastracodeProvider, ActionInstall)
	applyTo(t, svc, MastracodeProvider, ActionUninstall)

	doc := mastracodeHooksJSON(t, paths.Config)
	if len(doc) != 2 {
		t.Fatalf("hooks.json holds %d event keys after uninstall, want the two the user wrote: %v", len(doc), doc)
	}
	if got, _ := doc["Notification"][0]["command"].(string); got != "say 'mastracode wants you'" {
		t.Errorf("the user's Notification hook became %q", got)
	}
	if len(doc["Stop"]) != 1 {
		t.Fatalf("Stop holds %d entries after uninstall, want the user's one", len(doc["Stop"]))
	}
	if got, _ := doc["Stop"][0]["command"].(string); got != "/usr/local/bin/on-stop" {
		t.Errorf("the user's Stop hook became %q", got)
	}
	if st := mastracodeStatus(t, svc); st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status after uninstall is %q, want not-installed", st.Status)
	}
}

// TestUninstallingMastracodeRemovesAFileItCreated covers the other end: a
// hooks.json holding nothing but Sidecar's entries is a file Sidecar created, so
// uninstall takes it away rather than leaving an empty object behind.
func TestUninstallingMastracodeRemovesAFileItCreated(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	applyTo(t, svc, MastracodeProvider, ActionInstall)
	applyTo(t, svc, MastracodeProvider, ActionUninstall)

	if _, err := os.Lstat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("hooks.json survives an uninstall of a file Sidecar created: %v", err)
	}
	if _, err := os.Lstat(paths.Backup); err != nil {
		t.Fatalf("the removed file left no recoverable copy: %v", err)
	}
}

// TestUninstallingMastracodeIsByteIdentical is the strongest form of the
// ownership claim, and it is the one the live proof for Kimi made: install and
// uninstall against a file the user already owns must leave that file exactly as
// it was found.
func TestUninstallingMastracodeIsByteIdentical(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	before := `{
  "SessionStart": [
    {
      "type": "command",
      "command": "/usr/local/bin/on-session",
      "description": "mine"
    }
  ],
  "PostToolUse": [
    {
      "type": "command",
      "command": "/usr/local/bin/after-tool"
    }
  ]
}
`
	writeFileForTest(t, paths.Config, before)
	applyTo(t, svc, MastracodeProvider, ActionInstall)
	applyTo(t, svc, MastracodeProvider, ActionUninstall)

	if got := readFileForTest(t, paths.Config); got != before {
		t.Errorf("hooks.json is not what it was before the install:\n--- before ---\n%s\n--- after ---\n%s", before, got)
	}
}

// TestMastracodeStripsAStaleEntryUnderAnyEvent is the property that replaces
// upstream's MASTRACODE_REMOVED_HOOK_EVENTS.
//
// Herdr keeps a hand-written list of superseded (event, command) pairs and
// removes those exact strings. Sidecar identifies its own entries by the source
// their command names, so an entry left by any earlier asset version -- under an
// event this build no longer writes to at all -- is found and removed by the same
// pass that installs the current eleven. A list can go stale; this cannot.
func TestMastracodeStripsAStaleEntryUnderAnyEvent(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	stale := "sidecar agent report --source " + MastracodeSource + " --source-version 0 --provider mastracode --state idle --reason process_exit"
	writeFileForTest(t, paths.Config, `{
  "SessionEnd": [
    {
      "type": "command",
      "command": "`+stale+`"
    }
  ]
}
`)
	// The stale entry is Sidecar's, so the file does not read as not-installed and
	// install is not the verb; repair is, which is exactly the conversation the
	// verb gate exists to have.
	st := mastracodeStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a file holding only a superseded Sidecar entry reads as %q, want needs-repair", st.Status)
	}
	applyTo(t, svc, MastracodeProvider, ActionRepair)

	doc := mastracodeHooksJSON(t, paths.Config)
	if _, ok := doc["SessionEnd"]; ok {
		t.Error("the superseded entry survived, so every SessionEnd would report a lane this build does not claim")
	}
	if st := mastracodeStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after repair is %q, want current", st.Status)
	}
}

// TestMastracodeRefusesAConfigItCannotInterpret covers the shapes the editor
// declines rather than guesses at. A rewrite of a file the scan could not read is
// a clobber by construction.
func TestMastracodeRefusesAConfigItCannotInterpret(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"not JSON", "{ this is not json\n"},
		{"an event key that is not an array", `{"Stop": {"type": "command"}}`},
		{"an entry that is not an object", `{"Stop": ["echo hi"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, paths := mastracodeFixture(t)
			writeFileForTest(t, paths.Config, tc.content)

			st := mastracodeStatus(t, svc)
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("status is %q, want needs-repair", st.Status)
			}
			for _, act := range Actions() {
				if _, err := svc.Plan(MastracodeProvider, act); err == nil {
					t.Errorf("%s was planned against a file Sidecar cannot read", act)
				}
			}
			if got := readFileForTest(t, paths.Config); got != tc.content {
				t.Error("the file changed despite every verb refusing")
			}
		})
	}
}

// TestMastracodeIgnoresATopLevelKeyThatIsNotAnEvent is the other half of the
// strictness rule, and it matters because hooks.json is a file a user may keep
// other things in.
//
// Mastra Code's own loader iterates VALID_EVENTS and ignores every other
// top-level key, so a key that is not an event is not a malformed hook array --
// it is data the provider never looks at. Sidecar must not read it for meaning
// and must not refuse the file for it.
func TestMastracodeIgnoresATopLevelKeyThatIsNotAnEvent(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	writeFileForTest(t, paths.Config, `{
  "$schema": "https://code.mastra.ai/hooks.schema.json",
  "notes": {"why": "these are mine"}
}
`)
	if st := mastracodeStatus(t, svc); st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status is %q, want not-installed", st.Status)
	}
	applyTo(t, svc, MastracodeProvider, ActionInstall)

	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFileForTest(t, paths.Config)), &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["$schema"]) != `"https://code.mastra.ai/hooks.schema.json"` {
		t.Errorf("a top-level key that is not an event became %s", doc["$schema"])
	}
	if got := canonicalJSON(doc["notes"]); got != `{"why":"these are mine"}` {
		t.Errorf("a top-level object that is not an event became %s", got)
	}
}

// TestMastracodeRefusesWhenTheProviderIsNotOnPath is the precondition, and the
// one place the adapter's answer differs from the Pi and Kimi adapters': the
// configuration directory's absence is not a refusal here, because Mastra Code
// does not create it.
func TestMastracodeRefusesWhenTheProviderIsNotOnPath(t *testing.T) {
	svc, _, paths := mastracodeFixture(t, func(env *Env) {
		env.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	})
	st := mastracodeStatus(t, svc)
	if st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status is %q, want provider-missing", st.Status)
	}
	if _, err := svc.Plan(MastracodeProvider, ActionInstall); err == nil {
		t.Fatal("install was planned with no mastracode on PATH")
	}
	if _, err := os.Lstat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("something was written despite the refusal: %v", err)
	}
}

// TestMastracodeDryRunAndApplyProduceTheSameOps is the honesty property of
// --dry-run: what a cautious user reads is what a run does.
func TestMastracodeDryRunAndApplyProduceTheSameOps(t *testing.T) {
	svc, _, _ := mastracodeFixture(t)
	planned, err := svc.Plan(MastracodeProvider, ActionInstall)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	applied := applyTo(t, svc, MastracodeProvider, ActionInstall)
	if len(planned.Ops) != len(applied.Ops) {
		t.Fatalf("the plan describes %d operations and the run performed %d", len(planned.Ops), len(applied.Ops))
	}
	for i := range planned.Ops {
		if planned.Ops[i].Kind != applied.Ops[i].Kind || planned.Ops[i].Path != applied.Ops[i].Path {
			t.Errorf("operation %d: planned %s %s, ran %s %s",
				i, planned.Ops[i].Kind, planned.Ops[i].Path, applied.Ops[i].Kind, applied.Ops[i].Path)
		}
	}
}

// TestMastracodeStatusComesFromTheInstalledBytes is what makes the golden
// checksum mean anything at runtime. A hand-edited entry still invokes Sidecar's
// source, so it is Sidecar's to manage, and it is not what this build ships, so
// it reads as needs-repair rather than as current.
func TestMastracodeStatusComesFromTheInstalledBytes(t *testing.T) {
	svc, _, paths := mastracodeFixture(t)
	applyTo(t, svc, MastracodeProvider, ActionInstall)

	tampered := strings.Replace(readFileForTest(t, paths.Config),
		"--state working --reason turn_start", "--state idle --reason turn_complete", 1)
	writeFileForTest(t, paths.Config, tampered)

	st := mastracodeStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a hand-edited entry reads as %q, want needs-repair", st.Status)
	}
	applyTo(t, svc, MastracodeProvider, ActionRepair)
	if st := mastracodeStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after repair is %q, want current", st.Status)
	}
}

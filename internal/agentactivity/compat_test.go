package agentactivity

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity/manifests"
	"github.com/marcus/sidecar/internal/agentcatalog"
)

// These are characterization tests. They pin what the agent-state pipeline does
// today so a later extraction of a shared authority resolver has to change them
// deliberately rather than silently. Nothing here asserts that current behavior
// is correct; where it looks odd the oddity is recorded in a comment.

func assertJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("schema drift in %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// TestActivityStateVocabularyIsFrozen pins both the string values and their
// order. The values are written into persisted snapshots and compared by
// agentstatus, so renaming one silently reclassifies every stored tracker.
func TestActivityStateVocabularyIsFrozen(t *testing.T) {
	states := []State{StateUnknown, StateIdle, StateWorking, StateBlocked}
	wantStates := []State{"unknown", "idle", "working", "blocked"}
	if !reflect.DeepEqual(states, wantStates) {
		t.Fatalf("states = %#v", states)
	}
}

// TestResultFieldsAreFrozen pins the shape a provider probe returns. A resolver
// extracted later has to carry every one of these signals: dropping
// FallbackIdle or SkipStateUpdate would change which idles announce completion
// and which overlays retain the prior state. Result carries no JSON tags today
// because it is never persisted.
func TestResultFieldsAreFrozen(t *testing.T) {
	typ := reflect.TypeOf(Result{})
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if tag, ok := field.Tag.Lookup("json"); ok {
			t.Fatalf("Result.%s carries a json tag %q; Result is not a wire type", field.Name, tag)
		}
		fields = append(fields, field.Name)
	}
	want := []string{"State", "Evidence", "VisibleIdle", "VisibleWorking", "VisibleBlocker", "SkipStateUpdate", "FallbackIdle"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("Result fields = %#v", fields)
	}
}

// TestSnapshotJSONContract freezes the only JSON-tagged type in this package.
// Snapshot is persisted across restarts, so a renamed or newly omitted key
// makes an existing state file read as a different agent state.
func TestSnapshotJSONContract(t *testing.T) {
	snapshot := Snapshot{
		State:        "idle",
		Evidence:     "claude.known-live-fallback",
		ChangedAt:    time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC),
		Seen:         true,
		IdleInferred: true,
	}
	assertJSONFixture(t, "testdata/snapshot.json", snapshot)

	// The omitempty set is part of the contract: state, changedAt and seen are
	// always written, evidence and idleInferred vanish when empty.
	minimal, err := json.Marshal(Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(minimal), `{"state":"","changedAt":"0001-01-01T00:00:00Z","seen":false}`; got != want {
		t.Fatalf("minimal snapshot = %s, want %s", got, want)
	}
}

// TestSupportedProviderSetIsFrozen pins exactly which agents have
// provider-owned evidence. Supports gates ProviderSupported throughout the
// pipeline, so adding or removing a name flips whole workspaces between the
// semantic and the legacy projection.
//
// The set grew from ten to twenty in Phase 4 of
// docs/plans/active/herdr-detection-parity.md, and the reason is recorded here
// rather than only in the list, because the list changing is the whole point of
// freezing it. The ten added are Herdr's remaining screen-manifest agents, whose
// manifests Sidecar already vendored and whose rules the engine already
// executed; what they lacked was an identity, so `Detect` answered
// "unsupported-agent" for every pane running one. They are detection-only: no
// launch, no resume, no conversation adapter, no curated colour. Supports is a
// statement about the screen lane alone and has always been read that way by its
// four callers — workspaceinventory, agentcontrol, the workspace plugin, and
// `agent explain --file` — each of which now treats these ten the way it already
// treated Muse.
//
// Two upstream agents stay out and both are decisions, not omissions. `gemini`
// is Decision 4: Antigravity replaced it and `agy` is already a full family.
// `omp` has no screen manifest upstream at all, so there is nothing to execute.
//
// The list being frozen only means something if the freeze can be broken, and
// until the review that produced this version it could not: the test iterated
// its own lists and a twenty-first name would have passed it unchanged. Two
// closures fix that. The set is asserted equal to the catalog's two lists, which
// is how a family added there arrives here; and Supports is asserted false for
// every vendored manifest id outside the twenty, which is how an agent synced in
// and wired straight into the switch arrives here. Neither can catch a case
// added to Supports for a name that is in neither the catalog nor the vendored
// tree, and that is accepted: such a name has no manifest to execute, so
// Supports saying yes to it would make Detect answer manifest-unavailable rather
// than change a verdict.
func TestSupportedProviderSetIsFrozen(t *testing.T) {
	frozen := map[string]bool{}
	for _, agent := range sidecarFamilies() {
		if !Supports(agent) {
			t.Fatalf("Supports(%q) = false", agent)
		}
		frozen[agent] = true
	}

	// Closure one: the frozen twenty are exactly the catalog's two lists. A
	// family added to either one is supported the moment it lands -- Supports
	// reads the hand-gated ten by name and the alias-gated ten through
	// aliasGatedFamily, which TestAliasGatedSetMatchesTheCatalog ties to the
	// catalog -- so this is where a twenty-first name has to be argued for.
	catalog := map[string]bool{}
	for _, family := range append(agentcatalog.Families(), agentcatalog.DetectionFamilies()...) { //nolint:gocritic // one combined pass over both halves
		if _, hooksOnly := familiesWithNoScreenManifest[family.ID]; hooksOnly {
			// Registered in the catalog, deliberately unsupported by this lane:
			// upstream ships no screen manifest for it, so there is nothing to
			// execute. Declared once, in detection_only_test.go.
			continue
		}
		catalog[family.ID] = true
		if !frozen[family.ID] {
			t.Errorf("agentcatalog registers %q, which Supports answers %v for, and the frozen list above does not name it.\n"+
				"Supports gates ProviderSupported through the whole pipeline, so growing it moves whole workspaces\n"+
				"between the semantic and the legacy projection. Add it here deliberately, with the reason.",
				family.ID, Supports(family.ID))
		}
	}
	for agent := range frozen {
		if !catalog[agent] {
			t.Errorf("the frozen list names %q, which agentcatalog no longer registers", agent)
		}
	}

	// Closure two: no vendored manifest outside the twenty is supported. A sync
	// brings the manifest first and the identity second, so this is the other
	// door an agent comes through.
	vendored, err := manifests.Agents()
	if err != nil {
		t.Fatalf("list vendored manifests: %v", err)
	}
	for _, id := range vendored {
		if frozen[id] || frozen[sidecarFamilyForManifestID(id)] {
			continue
		}
		if Supports(id) {
			t.Errorf("Supports(%q) = true for a vendored manifest the frozen list does not name; register the family in "+
				"agentcatalog and add it above, or say here why it is supported without one", id)
		}
	}

	// Everything else, including the shell identity Identify can return and the
	// shared runtimes that need a process probe, is unsupported. The four Herdr
	// spellings are here on purpose. "gemini" and "omp" are the two Herdr agents
	// Sidecar declines to register, and a sync that quietly registered one would
	// show up here. "mastracode" is in Herdr's alias and authority tables but
	// ships no screen manifest either, so like omp there is nothing to execute
	// and nothing to support. "qoder" is a process *spelling* of the Qoder
	// family, not its id: Supports is asked about family ids, so the alias
	// answering true would mean Observation.Agent could carry either spelling and
	// two ids would name one manifest.
	for _, agent := range []string{"", "shell", "node", "bun", "agent", "Claude", "CODEX", "gemini", "omp", "mastracode", "qoder", "aider", "unknown"} {
		if Supports(agent) {
			t.Fatalf("Supports(%q) = true", agent)
		}
	}
}

// sidecarFamilyForManifestID inverts ManifestAgentID over the catalog, so a
// vendored file named for Herdr's spelling is checked against the Sidecar family
// that reads it (github-copilot.toml is the `copilot` family's manifest).
func sidecarFamilyForManifestID(manifestID string) string {
	for _, family := range append(agentcatalog.Families(), agentcatalog.DetectionFamilies()...) {
		if ManifestAgentID(family.ID) == manifestID {
			return family.ID
		}
	}
	return manifestID
}

// TestDetectFallsBackForUnsupportedAgentsAndProcessMismatches pins the two
// evidence strings a caller sees when no provider probe runs at all. Both are
// StateUnknown, which is what keeps an unrecognised pane out of the semantic
// lanes.
func TestDetectFallsBackForUnsupportedAgentsAndProcessMismatches(t *testing.T) {
	t.Run("unsupported-agent", func(t *testing.T) {
		for _, agent := range []string{"", "shell", "gemini"} {
			got := Detect(Observation{Agent: agent, CurrentCommand: "claude", Screen: "Working…"})
			if got.State != StateUnknown || got.Evidence != "unsupported-agent" {
				t.Fatalf("Detect(%q) = %+v", agent, got)
			}
		}
	})
	// Every provider gates on the live process before it reads any UI, so a
	// screen full of another agent's chrome cannot claim it. The evidence is
	// uniformly "<agent>.process-mismatch".
	for _, agent := range []string{"codex", "claude", "grok", "antigravity", "pi", "copilot", "cursor", "opencode", "amp", "muse"} {
		t.Run(agent+"/process-mismatch", func(t *testing.T) {
			got := Detect(Observation{
				Agent:          agent,
				CurrentCommand: "zsh",
				PaneTitle:      "Plugin confirmation needed",
				Screen:         "Working…\nesc to interrupt\n△ Permission required\nDo you want to proceed?",
			})
			if got.State != StateUnknown || got.Evidence != agent+".process-mismatch" {
				t.Fatalf("Detect(%q) = %+v", agent, got)
			}
			if got.VisibleIdle || got.VisibleWorking || got.VisibleBlocker || got.SkipStateUpdate || got.FallbackIdle {
				t.Fatalf("mismatch result carried visibility flags: %+v", got)
			}
		})
	}
}

// TestTrackerTimingConstantsAreFrozen pins the two windows that decide when a
// transition is real. Both are read by transition policy elsewhere, so a change
// here changes when notifications fire.
func TestTrackerTimingConstantsAreFrozen(t *testing.T) {
	if IdleDebounce != 400*time.Millisecond {
		t.Fatalf("IdleDebounce = %v", IdleDebounce)
	}
	if SkipRetentionCap != 2*time.Minute {
		t.Fatalf("SkipRetentionCap = %v", SkipRetentionCap)
	}
}

// TestTrackerIdleCommitRequiresDebounceUnlessVisible pins the asymmetry between
// an explicit on-screen idle and an inferred one. An explicit idle lands on the
// first observation; anything else has to hold for IdleDebounce, which is what
// stops a single quiet frame mid-turn from reading as a finished turn.
func TestTrackerIdleCommitRequiresDebounceUnlessVisible(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		result      Result
		wantFirst   bool
		wantAtDelta bool
		delta       time.Duration
	}{
		{"visible idle commits immediately", Result{State: StateIdle, Evidence: "claude.screen.idle", VisibleIdle: true}, true, false, IdleDebounce},
		{"inferred idle waits one debounce", Result{State: StateIdle, Evidence: "claude.known-live-fallback", FallbackIdle: true}, false, true, IdleDebounce},
		{"inferred idle still suppressed just under the debounce", Result{State: StateIdle, Evidence: "claude.known-live-fallback", FallbackIdle: true}, false, false, IdleDebounce - time.Nanosecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tracker Tracker
			if got := tracker.Apply(tt.result, now); got != tt.wantFirst {
				t.Fatalf("first Apply = %v, want %v", got, tt.wantFirst)
			}
			if tt.wantFirst {
				return
			}
			if got := tracker.Apply(tt.result, now.Add(tt.delta)); got != tt.wantAtDelta {
				t.Fatalf("Apply at +%v = %v, want %v", tt.delta, got, tt.wantAtDelta)
			}
		})
	}

	// Working and blocked have no debounce at all: they commit on sight.
	for _, result := range []Result{
		{State: StateWorking, Evidence: "claude.screen.working", VisibleWorking: true},
		{State: StateBlocked, Evidence: "claude.screen.blocked", VisibleBlocker: true},
	} {
		var tracker Tracker
		if !tracker.Apply(result, now) {
			t.Fatalf("%s did not commit on first sight", result.State)
		}
	}
}

// TestTrackerIgnoresIdenticalStateAndEvidence pins the no-churn rule: a repeat
// observation must not move ChangedAt, because ChangedAt is what the done TTL
// and the "how long has it been blocked" reading are measured from.
func TestTrackerIgnoresIdenticalStateAndEvidence(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	var tracker Tracker
	tracker.Apply(Result{State: StateWorking, Evidence: "codex.screen.working"}, now)
	if !tracker.ChangedAt.Equal(now) {
		t.Fatalf("ChangedAt = %v", tracker.ChangedAt)
	}
	if tracker.Apply(Result{State: StateWorking, Evidence: "codex.screen.working"}, now.Add(time.Minute)) {
		t.Fatal("identical observation published a transition")
	}
	if !tracker.ChangedAt.Equal(now) {
		t.Fatalf("identical observation moved ChangedAt to %v", tracker.ChangedAt)
	}
	// Same state with different evidence is a transition today: it re-stamps
	// ChangedAt even though the lane the user sees does not move.
	if !tracker.Apply(Result{State: StateWorking, Evidence: "codex.title.working"}, now.Add(2*time.Minute)) {
		t.Fatal("evidence change did not publish")
	}
	if !tracker.ChangedAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("evidence change ChangedAt = %v", tracker.ChangedAt)
	}
}

// TestTrackerSeenMarksOnlyARealCompletion pins which idles create the unseen
// "done" state. Only a transition out of live work does; a first observation, a
// restart, and any inferred idle are all pre-acknowledged so an agent that was
// already sitting idle never announces a completion nobody caused.
func TestTrackerSeenMarksOnlyARealCompletion(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		prior    *Result
		idle     Result
		wantSeen bool
	}{
		{"working to explicit idle is a completion", &Result{State: StateWorking, Evidence: "w"}, Result{State: StateIdle, Evidence: "i", VisibleIdle: true}, false},
		{"blocked to explicit idle is a completion", &Result{State: StateBlocked, Evidence: "b"}, Result{State: StateIdle, Evidence: "i", VisibleIdle: true}, false},
		{"first ever idle is quiet", nil, Result{State: StateIdle, Evidence: "i", VisibleIdle: true}, true},
		{"idle out of unknown is quiet", &Result{State: StateUnknown, Evidence: "u"}, Result{State: StateIdle, Evidence: "i", VisibleIdle: true}, true},
		{"inferred idle after work is quiet", &Result{State: StateWorking, Evidence: "w"}, Result{State: StateIdle, Evidence: "i", VisibleIdle: true, FallbackIdle: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tracker Tracker
			if tt.prior != nil {
				tracker.Apply(*tt.prior, now)
			}
			if !tracker.Apply(tt.idle, now.Add(time.Second)) {
				t.Fatal("idle did not commit")
			}
			if tracker.Seen != tt.wantSeen {
				t.Fatalf("Seen = %v, want %v", tracker.Seen, tt.wantSeen)
			}
			want := "idle"
			if !tt.wantSeen {
				want = "done"
			}
			if got := tracker.DisplayState(); got != want {
				t.Fatalf("DisplayState = %q, want %q", got, want)
			}
		})
	}

	// Committing to working or blocked always resets Seen, which is what makes
	// the next completion announceable.
	var tracker Tracker
	tracker.Apply(Result{State: StateIdle, Evidence: "i", VisibleIdle: true}, now)
	tracker.Acknowledge()
	tracker.Apply(Result{State: StateWorking, Evidence: "w"}, now.Add(time.Second))
	if tracker.Seen {
		t.Fatal("committing to working left Seen set")
	}
	tracker.Apply(Result{State: StateBlocked, Evidence: "b"}, now.Add(2*time.Second))
	if tracker.Seen {
		t.Fatal("committing to blocked left Seen set")
	}
}

// TestTrackerIdleInferredMirrorsFallbackIdle pins the flag views use to say
// "this provider cannot report completion" rather than letting the absence of a
// done state read as a bug. It is set only by a committed idle and cleared by
// any committed non-idle.
func TestTrackerIdleInferredMirrorsFallbackIdle(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	var tracker Tracker
	tracker.Apply(Result{State: StateWorking, Evidence: "w"}, now)
	tracker.Apply(Result{State: StateIdle, Evidence: "fallback", VisibleIdle: true, FallbackIdle: true}, now.Add(time.Second))
	if !tracker.IdleInferred {
		t.Fatal("fallback idle did not mark IdleInferred")
	}
	tracker.Apply(Result{State: StateWorking, Evidence: "w2"}, now.Add(2*time.Second))
	if tracker.IdleInferred {
		t.Fatal("committing to working did not clear IdleInferred")
	}
	tracker.Apply(Result{State: StateIdle, Evidence: "explicit", VisibleIdle: true}, now.Add(3*time.Second))
	if tracker.IdleInferred {
		t.Fatal("explicit idle set IdleInferred")
	}
}

// TestTrackerSkipRetentionSuppressesUntilTheCap pins the overlay rule. A skip
// result holds the prior state so a transcript viewer does not erase a live
// turn, but only for SkipRetentionCap; past that the overlay's own StateUnknown
// lands so the badge admits it no longer knows.
func TestTrackerSkipRetentionSuppressesUntilTheCap(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	var tracker Tracker
	tracker.Apply(Result{State: StateWorking, Evidence: "codex.screen.working", VisibleWorking: true}, now)
	skip := Result{State: StateUnknown, Evidence: "codex.viewer.retain", SkipStateUpdate: true}

	if tracker.Apply(skip, now.Add(time.Second)) {
		t.Fatal("first skip published")
	}
	if tracker.State != StateWorking {
		t.Fatalf("skip did not retain working: %v", tracker.State)
	}
	if tracker.Apply(skip, now.Add(time.Second).Add(SkipRetentionCap-time.Nanosecond)) {
		t.Fatal("skip published just under the cap")
	}
	if !tracker.Apply(skip, now.Add(time.Second).Add(SkipRetentionCap)) {
		t.Fatal("skip did not fall through at the cap")
	}
	if tracker.State != StateUnknown || tracker.Evidence != "codex.viewer.retain" {
		t.Fatalf("expired skip = %v/%q", tracker.State, tracker.Evidence)
	}

	// A skip result never leaves the blocker visible, even while it retains a
	// blocked state.
	var blocked Tracker
	blocked.Apply(Result{State: StateBlocked, Evidence: "b", VisibleBlocker: true}, now)
	blocked.Apply(Result{State: StateBlocked, Evidence: "overlay", VisibleBlocker: true, SkipStateUpdate: true}, now.Add(time.Second))
	if blocked.VisibleBlocker {
		t.Fatal("skip result kept VisibleBlocker set")
	}
}

// TestResetForProcessChangeClearsSemanticsAndAdmitsTheFirstIdle pins what
// happens when a pane changes owner. The prior agent's state is dropped, the
// tracker is pre-acknowledged so the new process's first idle is not reported
// as a completion, and that first idle lands without waiting out the debounce.
func TestResetForProcessChangeClearsSemanticsAndAdmitsTheFirstIdle(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	var tracker Tracker
	tracker.Apply(Result{State: StateWorking, Evidence: "codex.screen.working"}, now)
	tracker.ResetForProcessChange(now.Add(time.Second))

	if tracker.State != StateUnknown || tracker.Evidence != "live-process-changed" || !tracker.Seen {
		t.Fatalf("reset tracker = %#v", tracker)
	}
	// The reset also drops ChangedAt, so a restored transition time from the
	// previous owner cannot leak into the new one.
	if !tracker.ChangedAt.IsZero() {
		t.Fatalf("reset kept ChangedAt = %v", tracker.ChangedAt)
	}
	if !tracker.Apply(Result{State: StateIdle, Evidence: "claude.known-live-fallback", FallbackIdle: true}, now.Add(time.Second)) {
		t.Fatal("first idle after a process change was debounced")
	}
	if !tracker.Seen {
		t.Fatal("first idle after a process change announced a completion")
	}
}

// TestDisplayStateVocabularyIsFrozen pins the five strings every view and the
// lane resolver switch on. "done" is not a State: it is idle plus an unseen
// completion, and that projection lives here alone.
func TestDisplayStateVocabularyIsFrozen(t *testing.T) {
	trackers := []Tracker{
		{State: StateUnknown, Seen: true},
		{State: StateIdle, Seen: true},
		{State: StateWorking},
		{State: StateBlocked},
		{State: StateIdle, Seen: false},
	}
	got := make([]string, 0, len(trackers))
	for _, tracker := range trackers {
		got = append(got, tracker.DisplayState())
	}
	want := []string{"unknown", "idle", "working", "blocked", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("display states = %#v", got)
	}
	// A zero tracker reports the empty string rather than "unknown". The lane
	// resolver's default branch is what turns that into a paused row.
	if got := (Tracker{}).DisplayState(); got != "" {
		t.Fatalf("zero tracker DisplayState = %q", got)
	}
}

// TestRestorePreservesChangedAt pins that a persisted transition time survives
// a restart. Without it a turn that finished before the restart would read as
// having just finished, and the done TTL would restart with it.
func TestRestorePreservesChangedAt(t *testing.T) {
	changed := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)
	original := Tracker{State: StateIdle, Evidence: "codex.screen.idle", ChangedAt: changed, Seen: false, IdleInferred: true}
	restored := Restore(original.Snapshot())
	if !restored.ChangedAt.Equal(changed) {
		t.Fatalf("ChangedAt = %v, want %v", restored.ChangedAt, changed)
	}
	if restored.State != original.State || restored.Evidence != original.Evidence || restored.Seen != original.Seen || restored.IdleInferred != original.IdleInferred {
		t.Fatalf("restored = %#v, want %#v", restored, original)
	}
	if restored.DisplayState() != "done" {
		t.Fatalf("restored DisplayState = %q", restored.DisplayState())
	}
	// Transition timers and blocker visibility are deliberately not persisted:
	// a restored tracker starts with no idle candidate, no skip window, and no
	// claim that a blocker is still on screen.
	if restored.VisibleBlocker {
		t.Fatal("Restore claimed a visible blocker")
	}
}

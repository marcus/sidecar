package pluginbrowser

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
)

// A query line is bounded by the same figure the CLI and the persisted record
// are. Without the bound a long query is typed, saved, decoded, and then
// refused as invalid when the tabs are rebuilt — the tab disappears on
// relaunch, with nothing on screen having said no.
func TestAQueryStopsAtTheBoundItIsPersistedAgainst(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	press(t, m, "/")

	// The keystrokes are handed to the model directly: each one schedules a
	// debounce tick, and running those here would spend a real quarter-second
	// per character for no assertion.
	for i := 0; i < resource.MaxQueryChars+8; i++ {
		m.HandleKey(keyPress("a"))
	}
	got := m.activeState().queryText()
	if len(got) != resource.MaxQueryChars {
		t.Fatalf("query is %d characters, want it clamped at %d", len(got), resource.MaxQueryChars)
	}
	if strings.Trim(got, "a") != "" {
		t.Fatalf("query holds something other than what was typed: %q", got)
	}
	// The bound is the one a persisted reference is checked against.
	ref := resource.Reference{Instance: "fixture", Collection: "results", Query: got}
	if !ref.Valid() {
		t.Fatal("a query typed to the bound does not survive Reference.Valid; it would vanish on relaunch")
	}
}

// The describe generation counts describe snapshots, not reads of one. Refresh
// runs on every tab focus and every Resolve, and the live-refresh binding keys
// its validated watch set — one stat per declared path — by this number.
func TestTheDescribeGenerationCountsSnapshotsNotReads(t *testing.T) {
	host := &fakeHost{described: true, status: pluginhost.Status{State: pluginhost.StateReady}, desc: testDescription(), page: testPage(3)}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(120, 30)
	run(t, m, m.Refresh())

	first := m.DescribeGeneration()
	if first == 0 {
		t.Fatal("the first describe did not open a generation")
	}
	for i := 0; i < 5; i++ {
		run(t, m, m.Refresh())
	}
	if got := m.DescribeGeneration(); got != first {
		t.Fatalf("generation moved to %d over five re-reads of one describe (was %d)", got, first)
	}

	// A describe that actually says something new opens the next generation.
	changed := testDescription()
	changed.Collections[0].Refresh.Watch = []string{"/tmp/does-not-exist"}
	host.desc = changed
	run(t, m, m.Refresh())
	if got := m.DescribeGeneration(); got != first+1 {
		t.Fatalf("generation = %d after a changed describe, want %d", got, first+1)
	}
}

package pluginbrowser

import (
	"testing"

	"github.com/marcus/sidecar/internal/pluginhost"
)

// A text filter has to be typeable. The modal context binds j, k and r for the
// overlays that need them, so proving the value arrives through real key
// presses — rather than through SetValue — is what says a focused input wins
// those keys back.
func TestTextFilterTakesTypedKeysAndCommitsOnEnter(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	press(t, m, "v")
	// A section becomes focusable only once the box has rendered, which is what
	// the runtime does between key presses.
	_ = m.View()
	for i := 0; i < 40 && m.overlay.box.FocusedID() != filterTextPfx+"since"; i++ {
		press(t, m, "tab")
		_ = m.View()
	}
	if got := m.overlay.box.FocusedID(); got != filterTextPfx+"since" {
		t.Fatalf("the text filter is not reachable by keyboard; focus = %q", got)
	}
	for _, key := range []string{"j", "k", "r", "2", "0", "2", "6"} {
		press(t, m, key)
		_ = m.View()
	}
	if got := m.overlay.filters[2].text.Value(); got != "jkr2026" {
		t.Fatalf("typed value = %q; the modal's own j/k/r ate the keystrokes", got)
	}

	before := len(host.lists)
	press(t, m, "enter")
	if len(host.lists) != before+1 {
		t.Fatalf("enter inside the input did not commit: lists = %d", len(host.lists))
	}
	if got := lastList(t, host).Params.Filters["since"]; got != "jkr2026" {
		t.Fatalf("params.filters = %v", lastList(t, host).Params.Filters)
	}
}

// A filter id is storable text, so it may contain a colon — and a radio's
// action id is the filter id and the choice id joined with one. The pick has to
// resolve against the live declaration rather than at the first colon, or a
// legally declared filter closes the modal having silently done nothing.
func TestFilterIDsMayContainAColon(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	desc := testDescription()
	desc.Collections[0].Filters = []pluginhost.Filter{{
		ID: "date:since", Label: "Since", Kind: pluginhost.FilterChoice, Default: "any",
		Choices: []pluginhost.FilterOption{
			{ID: "any", Title: "Any time"},
			{ID: "this:week", Title: "This week"},
		},
	}}
	host.desc = desc
	run(t, m, m.Refresh())
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	before := len(host.lists)
	press(t, m, "v")
	run(t, m, m.applyViewAction(filterChoicePfx+"date:since:this:week"))
	if len(host.lists) != before+1 {
		t.Fatalf("picking a radio on a colon-bearing filter id did not re-list: lists = %d", len(host.lists))
	}
	if got := lastList(t, host).Params.Filters; got["date:since"] != "this:week" {
		t.Fatalf("params.filters = %v, want date:since=this:week", got)
	}
	// And the pill names it, because it is the first declared filter.
	if got := m.viewControlLabel(); got != "⇅ Relevance · This week" {
		t.Fatalf("pill = %q", got)
	}
}

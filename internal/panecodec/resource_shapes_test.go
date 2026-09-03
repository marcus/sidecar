package panecodec

import (
	"testing"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/state"
)

func resourceLayout(tabs ...state.PaneResourceTabJSON) *state.PaneLayoutJSON {
	return &state.PaneLayoutJSON{Split: &state.PaneSplitJSON{
		Axis:  axisCols,
		Ratio: 50,
		A:     &state.PaneLayoutJSON{Kind: KindTerminal},
		B:     &state.PaneLayoutJSON{Kind: KindResource, ResourceTabs: tabs},
	}}
}

func resourceTabsOf(st contentpanes.State) []contentpanes.TabState {
	var walk func(*contentpanes.NodeState) []contentpanes.TabState
	walk = func(n *contentpanes.NodeState) []contentpanes.TabState {
		if n == nil {
			return nil
		}
		if n.Kind == "resource" && n.Pane != nil {
			return n.Pane.Tabs
		}
		if tabs := walk(n.A); tabs != nil {
			return tabs
		}
		return walk(n.B)
	}
	return walk(st.Root)
}

// Relaunch restores a collection tab with the list the user was reading, not
// the collection's default page. The query, view, sort and cursor all survive
// the round trip; nothing a plugin returned does.
func TestCollectionTabRoundTripsItsViewPosition(t *testing.T) {
	saved := resourceLayout(state.PaneResourceTabJSON{
		Provider: "recall", Collection: "results", Query: "dex",
		View: "recent", Sort: "relevance", CursorID: "rc:notes:1", Scroll: 3,
	})
	st, _ := Decode(saved, Options{})
	tabs := resourceTabsOf(st)
	if len(tabs) != 1 {
		t.Fatalf("decoded %d resource tabs, want 1", len(tabs))
	}
	got := tabs[0]
	if got.Ref.Provider != "recall" || got.Ref.Collection != "results" || got.Ref.Query != "dex" {
		t.Fatalf("collection identity lost: %+v", got.Ref)
	}
	if got.Ref.Matcher != "" || got.Ref.Value != "" {
		t.Fatalf("a collection tab decoded with document fields set: %+v", got.Ref)
	}
	if got.View != "recent" || got.Sort != "relevance" || got.CursorID != "rc:notes:1" || got.Scroll != 3 {
		t.Fatalf("view position lost: %+v", got)
	}

	back := Encode(st, Options{Live: []Live{{Kind: KindTerminal}}})
	leaf := firstResourceLeaf(back)
	if leaf == nil || len(leaf.ResourceTabs) != 1 {
		t.Fatalf("re-encoded layout has no resource tab: %+v", back)
	}
	if !leaf.ResourceTabs[0].Equal(saved.Split.B.ResourceTabs[0]) {
		t.Fatalf("round trip changed the record:\n got %+v\nwant %+v",
			leaf.ResourceTabs[0], saved.Split.B.ResourceTabs[0])
	}
}

// A collection tab's applied filters ride with the rest of its view position,
// and survive both directions unchanged.
func TestCollectionTabRoundTripsItsFilters(t *testing.T) {
	saved := resourceLayout(state.PaneResourceTabJSON{
		Provider: "recall", Collection: "results", Query: "dex",
		Filters: map[string]string{"profile": "docs", "since": "2026-08-01"},
	})
	st, _ := Decode(saved, Options{})
	tabs := resourceTabsOf(st)
	if len(tabs) != 1 {
		t.Fatalf("decoded %d resource tabs, want 1", len(tabs))
	}
	if got := tabs[0].Filters; len(got) != 2 || got["profile"] != "docs" || got["since"] != "2026-08-01" {
		t.Fatalf("filters lost on the way in: %v", got)
	}

	back := firstResourceLeaf(Encode(st, Options{Live: []Live{{Kind: KindTerminal}}}))
	if back == nil || len(back.ResourceTabs) != 1 {
		t.Fatalf("re-encoded layout has no resource tab: %+v", back)
	}
	if !back.ResourceTabs[0].Equal(saved.Split.B.ResourceTabs[0]) {
		t.Fatalf("round trip changed the record:\n got %+v\nwant %+v",
			back.ResourceTabs[0], saved.Split.B.ResourceTabs[0])
	}

	// A row tab is not a collection tab: it carries no filters, because a
	// document is not narrowed by them.
	row := resourceLayout(state.PaneResourceTabJSON{
		Provider: "recall", Collection: "results", Locator: "rc:notes:1",
		Filters: map[string]string{"profile": "docs"},
	})
	rowState, _ := Decode(row, Options{})
	if got := resourceTabsOf(rowState)[0].Filters; len(got) != 0 {
		t.Fatalf("a row tab decoded with filters: %v", got)
	}
}

// One row of a collection is its own shape: a collection plus a locator, and no
// matcher anywhere, because a plugin row is addressed by collection and ID.
func TestItemTabRoundTripsWithoutAMatcher(t *testing.T) {
	saved := resourceLayout(state.PaneResourceTabJSON{
		Provider: "recall", Collection: "results", Locator: "rc:notes:1",
	})
	st, _ := Decode(saved, Options{})
	tabs := resourceTabsOf(st)
	if len(tabs) != 1 {
		t.Fatalf("decoded %d resource tabs, want 1", len(tabs))
	}
	if tabs[0].Ref.Matcher != "" {
		t.Fatalf("a row tab decoded with a matcher: %+v", tabs[0].Ref)
	}
	back := firstResourceLeaf(Encode(st, Options{Live: []Live{{Kind: KindTerminal}}}))
	if back == nil || back.ResourceTabs[0].Matcher != "" {
		t.Fatalf("a row tab re-encoded with a matcher: %+v", back)
	}
}

// The frozen protocol's records are read back byte-identically. Every release
// before the plugin protocol wrote exactly this shape.
func TestMatchedTabIsUnchanged(t *testing.T) {
	saved := resourceLayout(state.PaneResourceTabJSON{
		Provider: "jira-work", Matcher: "issue-key", Locator: "CASH-1245", Scroll: 5,
	})
	st, _ := Decode(saved, Options{})
	tabs := resourceTabsOf(st)
	if len(tabs) != 1 || tabs[0].Ref.Collection != "" {
		t.Fatalf("a matched document decoded as something else: %+v", tabs)
	}
	back := firstResourceLeaf(Encode(st, Options{Live: []Live{{Kind: KindTerminal}}}))
	if !back.ResourceTabs[0].Equal(saved.Split.B.ResourceTabs[0]) {
		t.Fatalf("round trip changed the record: %+v", back.ResourceTabs[0])
	}
}

// An ambiguous record is dropped rather than guessed at. A record naming both a
// matcher and a collection could be either tab, and restoring the wrong one
// points the pane at something the user never opened.
func TestDecodeRefusesAnAmbiguousResourceTab(t *testing.T) {
	for _, tab := range []state.PaneResourceTabJSON{
		{Provider: "recall", Matcher: "key", Locator: "X-1", Collection: "results"},
		{Provider: "recall"},
		{Provider: "recall", Matcher: "key"},
		{Provider: "recall", Locator: "X-1"},
		{Collection: "results"},
	} {
		st, _ := Decode(resourceLayout(tab), Options{})
		if tabs := resourceTabsOf(st); len(tabs) != 0 {
			t.Errorf("record %+v decoded to %+v; it names no single shape", tab, tabs)
		}
	}
}

// A collection tab and a row of that collection are two tabs, not one. Encode
// must keep both, and the active index must still point at the same one.
func TestBothShapesLiveInOneLeaf(t *testing.T) {
	saved := resourceLayout(
		state.PaneResourceTabJSON{Provider: "recall", Collection: "results", Query: "dex"},
		state.PaneResourceTabJSON{Provider: "recall", Collection: "results", Locator: "rc:notes:1"},
	)
	saved.Split.B.Active = 1
	st, _ := Decode(saved, Options{})
	tabs := resourceTabsOf(st)
	if len(tabs) != 2 {
		t.Fatalf("decoded %d tabs, want 2", len(tabs))
	}
	if tabs[0].Ref.Query != "dex" || tabs[1].Ref.Value != "rc:notes:1" {
		t.Fatalf("tabs decoded in the wrong shapes: %+v", tabs)
	}
	back := firstResourceLeaf(Encode(st, Options{Live: []Live{{Kind: KindTerminal}}}))
	if back.Active != 1 {
		t.Fatalf("active tab moved: %d", back.Active)
	}
}

// The one place a collection reference reaches contentlink: a decoded tab must
// carry the collection on the ref, because that is what the deck normalizes and
// keys the tab by.
func TestDecodedCollectionRefIsAResourceKind(t *testing.T) {
	st, _ := Decode(resourceLayout(state.PaneResourceTabJSON{
		Provider: "recall", Collection: "results",
	}), Options{})
	tabs := resourceTabsOf(st)
	if len(tabs) != 1 || tabs[0].Ref.Kind != contentlink.KindResource {
		t.Fatalf("decoded ref is not a resource kind: %+v", tabs)
	}
}

func firstResourceLeaf(j *state.PaneLayoutJSON) *state.PaneLayoutJSON {
	if j == nil {
		return nil
	}
	if j.Kind == KindResource {
		return j
	}
	if j.Split == nil {
		return nil
	}
	if leaf := firstResourceLeaf(j.Split.A); leaf != nil {
		return leaf
	}
	return firstResourceLeaf(j.Split.B)
}

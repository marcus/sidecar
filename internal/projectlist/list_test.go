package projectlist

import (
	"testing"
	"time"
)

func at(day int) time.Time { return time.Date(2026, 9, day, 12, 0, 0, 0, time.UTC) }

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}

func equal(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func collection() []Item {
	return []Item{
		{ID: "overview", Kind: KindOverview, Name: "Overview"},
		{ID: "b", Kind: KindProject, Name: "braid", Path: "~/code/braid", LastActiveAt: at(3), AddedAt: at(1)},
		{ID: "a", Kind: KindProject, Name: "archive", Path: "~/code/archive"},
		{ID: "s", Kind: KindProject, Name: "sidecar", Path: "~/code/sidecar", LastActiveAt: at(4), AddedAt: at(2)},
		{ID: "t", Kind: KindProject, Name: "tui", Path: "~/code/tui", LastActiveAt: at(2)},
	}
}

func TestOverviewIsPinnedAheadOfEveryOrder(t *testing.T) {
	for _, mode := range SortModes {
		for _, order := range []Order{OrderAscending, OrderDescending} {
			got := Sorted(collection(), mode, order)
			if got[0].Kind != KindOverview {
				t.Fatalf("%s/%s put %q first; Overview is outside the sorting domain",
					mode.Label(), OrderLabel(mode, order), got[0].Name)
			}
		}
	}
}

func TestUnknownTimestampsSortLastInBothDirections(t *testing.T) {
	for _, order := range []Order{OrderAscending, OrderDescending} {
		got := ids(Sorted(collection(), SortActivity, order))
		if got[len(got)-1] != "a" {
			t.Fatalf("%s: unknown activity landed at %v, want last", OrderLabel(SortActivity, order), got)
		}
	}
	// Two projects have no added date; both stay behind the known ones.
	for _, order := range []Order{OrderAscending, OrderDescending} {
		got := ids(Sorted(collection(), SortAdded, order))
		tail := got[len(got)-2:]
		if !contains(tail, "a") || !contains(tail, "t") {
			t.Fatalf("%s: unknown added dates landed at %v, want last two", OrderLabel(SortAdded, order), got)
		}
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestSortedOrders(t *testing.T) {
	tests := []struct {
		name  string
		mode  Sort
		order Order
		want  []string
	}{
		{"activity newest first", SortActivity, OrderDescending, []string{"overview", "s", "b", "t", "a"}},
		{"activity oldest first", SortActivity, OrderAscending, []string{"overview", "t", "b", "s", "a"}},
		{"name a-z", SortName, OrderAscending, []string{"overview", "a", "b", "s", "t"}},
		{"name z-a", SortName, OrderDescending, []string{"overview", "t", "s", "b", "a"}},
		{"added newest first", SortAdded, OrderDescending, []string{"overview", "s", "b", "a", "t"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			equal(t, ids(Sorted(collection(), tc.mode, tc.order)), tc.want)
		})
	}
}

func TestUnknownTiesBreakOnNameSoOrderIsStable(t *testing.T) {
	// archive and tui both lack an added date; the tie is broken by name in
	// both directions, so reversing the order does not shuffle them.
	asc := ids(Sorted(collection(), SortAdded, OrderAscending))
	desc := ids(Sorted(collection(), SortAdded, OrderDescending))
	if asc[len(asc)-2] != "a" || asc[len(asc)-1] != "t" {
		t.Fatalf("ascending unknown tail %v, want archive then tui", asc)
	}
	if desc[len(desc)-2] != "a" || desc[len(desc)-1] != "t" {
		t.Fatalf("descending unknown tail %v, want archive then tui", desc)
	}
}

func TestSortedDoesNotMutateTheInput(t *testing.T) {
	in := collection()
	before := ids(in)
	_ = Sorted(in, SortName, OrderDescending)
	equal(t, ids(in), before)
}

func TestFilterMatchesNameAndPathAndHost(t *testing.T) {
	items := append(collection(), Item{ID: "r", Kind: KindProject, Name: "[beta] api", Path: "/srv/api", Host: "beta"})
	if got := ids(Filter(items, "CODE/BR")); len(got) != 1 || got[0] != "b" {
		t.Fatalf("path match got %v", got)
	}
	if got := ids(Filter(items, "beta")); len(got) != 1 || got[0] != "r" {
		t.Fatalf("host match got %v", got)
	}
	if got := Filter(items, ""); len(got) != len(items) {
		t.Fatalf("empty query dropped rows: %v", ids(got))
	}
	if got := Filter(items, "nothing-here"); len(got) != 0 {
		t.Fatalf("no-match query kept %v", ids(got))
	}
}

func TestSelectionSurvivesSortAndFilterByIdentity(t *testing.T) {
	items := collection()
	id := items[4].ID // tui
	reordered := Sorted(Filter(items, "t"), SortName, OrderDescending)
	if IndexOf(reordered, id) < 0 {
		t.Fatalf("identity %q lost through filter+sort: %v", id, ids(reordered))
	}
	if IndexOf(reordered, "gone") != -1 {
		t.Fatal("a missing identity must report -1 so the caller can fall back")
	}
}

func TestLabelsRoundTripThroughPersistedForm(t *testing.T) {
	for _, mode := range SortModes {
		got, ok := SortFromLabel(mode.Label(), SortModes)
		if !ok || got != mode {
			t.Fatalf("sort label %q did not round-trip", mode.Label())
		}
		if got, ok := SortFromAction(SortActionID(mode), SortModes); !ok || got != mode {
			t.Fatalf("sort action %q did not round-trip", SortActionID(mode))
		}
		for _, order := range []Order{OrderAscending, OrderDescending} {
			back, ok := OrderFromLabel(OrderLabel(mode, order))
			if !ok || back != order {
				t.Fatalf("order label %q did not round-trip", OrderLabel(mode, order))
			}
		}
	}
	for _, v := range ViewModes {
		if got, ok := ViewFromLabel(v.Label()); !ok || got != v {
			t.Fatalf("view label %q did not round-trip", v.Label())
		}
		if got, ok := ViewFromAction(ViewActionID(v)); !ok || got != v {
			t.Fatalf("view action %q did not round-trip", ViewActionID(v))
		}
	}
	if _, ok := SortFromLabel("Date created", SortModes); ok {
		t.Fatal(`"Date created" must not resolve: Sidecar records registration, not creation`)
	}
}

func TestDefaultOrderReadsTheWayEachModeIsWanted(t *testing.T) {
	if SortName.DefaultOrder() != OrderAscending {
		t.Fatal("names should start A–Z")
	}
	for _, mode := range []Sort{SortActivity, SortAdded} {
		if mode.DefaultOrder() != OrderDescending {
			t.Fatalf("%s should start newest first", mode.Label())
		}
	}
}

func TestFormatRelative(t *testing.T) {
	now := at(10)
	tests := []struct {
		in   time.Time
		want string
	}{
		{time.Time{}, "Unknown"},
		{now, "now"},
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-4 * time.Minute), "4m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-30 * time.Hour), "yesterday"},
		{now.Add(-5 * 24 * time.Hour), "5d ago"},
		{at(1).AddDate(0, -6, 0), at(1).AddDate(0, -6, 0).Format("2006-01-02")},
	}
	for _, tc := range tests {
		if got := FormatRelative(tc.in, now); got != tc.want {
			t.Fatalf("FormatRelative(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMetaColumnFollowsADateSortAndOtherwiseShowsActivity(t *testing.T) {
	item := Item{LastActiveAt: at(9), AddedAt: at(1)}
	now := at(10)

	if heading, byDate := MetaColumn(SortAdded); heading != "DATE ADDED" || !byDate {
		t.Fatalf("added sort heading = %q/%v", heading, byDate)
	}
	if got := MetaValue(item, SortAdded, now); got != "2026-09-01" {
		t.Fatalf("added value = %q", got)
	}
	for _, mode := range []Sort{SortName, SortActivity} {
		heading, byDate := MetaColumn(mode)
		if heading != "LAST ACTIVITY" || byDate {
			t.Fatalf("%s heading = %q/%v", mode.Label(), heading, byDate)
		}
		if got := MetaValue(item, mode, now); got != "yesterday" {
			t.Fatalf("%s value = %q", mode.Label(), got)
		}
	}
	if got := MetaValue(Item{}, SortActivity, now); got != UnknownLabel {
		t.Fatalf("absent activity = %q, want %q", got, UnknownLabel)
	}
}

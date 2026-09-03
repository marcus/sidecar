package workspacelist

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// key builds the key message the app forwards. A single-rune key carries its
// own text, which is what the query row types.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "alt+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

// Slice 2 of docs/plans/active/global-overview-workspaces.md: the list, filter,
// and sort component both Workspaces surfaces share. These tests pin the
// promises the plan makes about matching, ordering, selection, and the filter's
// keyboard contract — the behaviours a consumer is allowed to rely on.

func items() []Item {
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	return []Item{
		{ID: "a", Name: "modal look and feel", Project: "sidecar", ProjectOrder: 0, Branch: "modal-look-and-feel", Task: "td-71de3d", Provider: "codex", Status: "working", Kind: KindWorktree, Group: GroupWorking, ChangedAt: base},
		{ID: "b", Name: "Kanban scrolling", Project: "sidecar", ProjectOrder: 0, Provider: "shell", Status: "live", Kind: KindShell, Group: GroupLive, ChangedAt: base.Add(-time.Hour)},
		{ID: "c", Name: "réponse", Project: "braid", ProjectOrder: 1, Branch: "feature/RÉPONSE", Provider: "claude", Status: "needs attention", Kind: KindWorktree, Group: GroupNeedsAttention, ChangedAt: base.Add(-2 * time.Hour)},
		{ID: "d", Name: "old worktree", Project: "td", ProjectOrder: 2, Status: "no session", Kind: KindWorktree, Group: GroupNoSession},
	}
}

func TestFilterMatchesEveryPromisedFieldCaseAndUnicodeSafely(t *testing.T) {
	all := items()
	cases := []struct {
		query string
		want  []string
	}{
		{"MODAL", []string{"a"}},                    // name, case-insensitive
		{"td-71", []string{"a"}},                    // task id
		{"modal-look", []string{"a"}},               // branch
		{"braid", []string{"c"}},                    // project
		{"claude", []string{"c"}},                   // provider
		{"needs", []string{"c"}},                    // semantic status label
		{"réponse", []string{"c"}},                  // unicode, and the branch's uppercase spelling
		{"sidecar working", []string{"a"}},          // every token must match
		{"   ", []string{"a", "b", "c", "d"}},       // whitespace-only is not a filter
		{"nothing at all", nil},                     // honest empty result
		{"no session", []string{"d"}},               // plain rows are matchable too
		{"scrolling sidecar", []string{"b"}},        // token order does not matter
		{"live", []string{"b"}},                     // presentation bucket
		{"sidecar", []string{"a", "b"}},             // project narrows, does not widen
		{"work", []string{"a", "d"}},                // substring across name and status
		{"réponse braid", []string{"c"}},            // combined
		{"RÉPONSE", []string{"c"}},                  // uppercase unicode query
		{"kanban", []string{"b"}},                   // case-insensitive name
		{"old", []string{"d"}},                      // last row
		{"sidecar braid td", nil},                   // no row belongs to three projects
		{"feature/", []string{"c"}},                 // punctuation inside a token
		{"codex", []string{"a"}},                    // provider only
		{"modal look", []string{"a"}},               // spaces split tokens, not phrases
		{"worktree", []string{"d"}},                 // name token
		{"td", []string{"a", "d"}},                  // matches the task id and the project
		{"", []string{"a", "b", "c", "d"}},          // empty query matches everything
		{"sidecar modal working", []string{"a"}},    // three-token AND
		{"kanban scrolling", []string{"b"}},         // two-token AND on one field
		{"braid claude attention", []string{"c"}},   // across three fields
		{"sidecar live", []string{"b"}},             // project + bucket
		{"nosuch", nil},                             // no match
		{"td-71d3", nil},                            // near miss on a task id
		{"session", []string{"d"}},                  // status word
		{"look and feel", []string{"a"}},            // multi-token phrase-ish
		{"BRAID", []string{"c"}},                    // uppercase project
		{"shell", []string{"b"}},                    // provider label
		{"working", []string{"a"}},                  // status
		{"modal feel sidecar", []string{"a"}},       // reordered tokens
		{"c", []string{"a", "b", "c"}},              // a single letter is a legitimate substring search
		{"scroll", []string{"b"}},                   // partial word
		{"réponse feature", []string{"c"}},          // unicode plus ascii
		{"no", []string{"d"}},                       // substring search, not fuzzy matching
		{"needs attention braid", []string{"c"}},    // full label
		{"old td", []string{"d"}},                   // name + project
		{"modal-look-and-feel", []string{"a"}},      // whole branch
		{"KANBAN SCROLLING", []string{"b"}},         // uppercase multi-token
		{"sidecar codex td-71d63", nil},             // one bad token fails the whole query
		{"live shell", []string{"b"}},               // bucket + provider
		{"attention", []string{"c"}},                // single status word
		{"worktree td", []string{"d"}},              // name + project
		{"réponse claude braid", []string{"c"}},     // everything about one row
		{"modal", []string{"a"}},                    // simple
		{"feel", []string{"a"}},                     // simple
		{"and", []string{"a"}},                      // stop-words are not special
		{"sidecar kanban scrolling", []string{"b"}}, // project + name
	}
	for _, tc := range cases {
		var got []string
		for _, item := range Filtered(all, tc.query) {
			got = append(got, item.ID)
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("Filtered(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestMatchFieldsIsTheSameMatcherForCallersWithoutItems(t *testing.T) {
	if !MatchFields("codex working", "topic", "sidecar", "branch", "", "codex", "working") {
		t.Fatal("project-side field matching disagrees with the item matcher")
	}
	if MatchFields("codex missing", "topic", "sidecar", "branch", "", "codex", "working") {
		t.Fatal("an unmatched token must fail the whole query")
	}
}

func TestFourSortsAreStableAndPresentationOnly(t *testing.T) {
	all := items()
	order := func(mode Sort) []string {
		var got []string
		for _, item := range Sorted(all, mode) {
			got = append(got, item.ID)
		}
		return got
	}
	want := map[Sort][]string{
		SortActivity: {"c", "a", "b", "d"}, // needs attention, working, live, no session
		SortProject:  {"a", "b", "c", "d"}, // configured project order, then input order
		SortRecent:   {"a", "b", "c", "d"}, // newest change first; zero times last
		SortName:     {"b", "a", "d", "c"}, // case-insensitive by name
	}
	for mode, expected := range want {
		if got := order(mode); strings.Join(got, ",") != strings.Join(expected, ",") {
			t.Fatalf("%s sort = %v, want %v", mode.Label(), got, expected)
		}
	}
	// Sorting is presentation only: the input slice and its identities survive.
	for i, item := range items() {
		if all[i].ID != item.ID {
			t.Fatal("Sorted mutated its input")
		}
	}
	// An unchanged poll must not churn: sorting twice is identical.
	first := strings.Join(order(SortActivity), ",")
	second := strings.Join(order(SortActivity), ",")
	if first != second {
		t.Fatal("repeated sorts disagree")
	}
	if SortActivity.Next() != SortProject || SortProject.Next() != SortRecent || SortRecent.Next() != SortName || SortName.Next() != SortActivity {
		t.Fatal("`s` does not cycle Activity → Project → Recent → Name")
	}
}

func TestActivityGroupingIsTheKanbanProjection(t *testing.T) {
	sections := Grouped(Sorted(items(), SortActivity), SortActivity)
	var got []Group
	for _, section := range sections {
		got = append(got, section.Group)
	}
	want := []Group{GroupNeedsAttention, GroupWorking, GroupLive, GroupNoSession}
	if len(got) != len(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sections = %v, want %v", got, want)
		}
	}
	if sections[0].Title != string(GroupNeedsAttention) {
		t.Fatalf("activity heading = %q, want %q", sections[0].Title, GroupNeedsAttention)
	}
}

func TestNameSortStaysUnheaded(t *testing.T) {
	sections := Grouped(Sorted(items(), SortName), SortName)
	if len(sections) != 1 || sections[0].Title != "" || sections[0].Group != "" {
		t.Fatalf("name sort produced headings: %#v", sections)
	}
	if got := idsOf(sections[0].Items); strings.Join(got, ",") != "b,a,d,c" {
		t.Fatalf("name sort items = %v", got)
	}
}

func TestProjectSortHeadsEachProjectAndOmitsEmpty(t *testing.T) {
	sections := Grouped(Sorted(items(), SortProject), SortProject)
	if len(sections) != 3 {
		t.Fatalf("project sections = %d, want 3 (empty projects omitted): %#v", len(sections), sections)
	}
	want := []struct {
		title string
		ids   string
	}{
		{"sidecar", "a,b"},
		{"braid", "c"},
		{"td", "d"},
	}
	for i, tc := range want {
		if sections[i].Title != tc.title {
			t.Fatalf("section %d title = %q, want %q", i, sections[i].Title, tc.title)
		}
		if got := strings.Join(idsOf(sections[i].Items), ","); got != tc.ids {
			t.Fatalf("section %q items = %s, want %s", tc.title, got, tc.ids)
		}
	}
}

func TestRecentSortHeadsNonEmptyBucketsNewestFirst(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	rows := []Item{
		{ID: "new-b", Name: "newer", ChangedAt: now.Add(-10 * time.Minute)},
		{ID: "new-a", Name: "new", ChangedAt: now.Add(-30 * time.Minute)},
		{ID: "today", Name: "today", ChangedAt: now.Add(-3 * time.Hour)},
		{ID: "week", Name: "week", ChangedAt: now.Add(-3 * 24 * time.Hour)},
		{ID: "old", Name: "old", ChangedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: "zero", Name: "zero"},
	}
	sections := GroupedAt(Sorted(rows, SortRecent), SortRecent, now, nil)
	want := []struct {
		title string
		ids   string
	}{
		{RecentNew, "new-b,new-a"},
		{RecentToday, "today"},
		{RecentThisWeek, "week"},
		{RecentOlder, "old,zero"},
	}
	if len(sections) != len(want) {
		t.Fatalf("recent sections = %#v", sections)
	}
	for i, tc := range want {
		if sections[i].Title != tc.title {
			t.Fatalf("section %d title = %q, want %q", i, sections[i].Title, tc.title)
		}
		if got := strings.Join(idsOf(sections[i].Items), ","); got != tc.ids {
			t.Fatalf("section %q items = %s, want %s", tc.title, got, tc.ids)
		}
	}

	// Empty buckets are omitted.
	sparse := []Item{
		{ID: "hot", ChangedAt: now.Add(-5 * time.Minute)},
		{ID: "ancient", ChangedAt: now.Add(-40 * 24 * time.Hour)},
	}
	got := GroupedAt(Sorted(sparse, SortRecent), SortRecent, now, nil)
	if len(got) != 2 || got[0].Title != RecentNew || got[1].Title != RecentOlder {
		t.Fatalf("sparse recent sections = %#v", got)
	}
}

func TestPinnedSectionSitsAboveSortGroupsAndIsNotDuplicated(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	all := items()
	sections := GroupedAt(Sorted(all, SortActivity), SortActivity, now, []string{"d", "missing", "a", "d"})
	if len(sections) < 2 || sections[0].Title != "Pinned" {
		t.Fatalf("pinned block missing: %#v", sections)
	}
	if got := strings.Join(idsOf(sections[0].Items), ","); got != "d,a" {
		t.Fatalf("pin order = %s, want first-pinned first and gone IDs dropped", got)
	}
	var rest []string
	for _, section := range sections[1:] {
		rest = append(rest, idsOf(section.Items)...)
		if section.Title == "Pinned" {
			t.Fatal("pinned block repeated")
		}
	}
	if strings.Contains(strings.Join(rest, ","), "a") || strings.Contains(strings.Join(rest, ","), "d") {
		t.Fatalf("pinned rows were re-bucketed: %v", rest)
	}
	if strings.Join(rest, ",") != "c,b" {
		t.Fatalf("unpinned activity remainder = %v, want c,b", rest)
	}
}

func TestModelTogglePinHeadsTheListAndDoesNotDuplicate(t *testing.T) {
	var m Model
	m.SetItems(items())
	m.SetSort(SortActivity)
	if got := strings.Join(idsOf(m.Visible()), ","); got != "c,a,b,d" {
		t.Fatalf("activity visible = %s", got)
	}
	if got := m.TogglePin("d"); strings.Join(got, ",") != "d" {
		t.Fatalf("first pin = %v", got)
	}
	if got := m.TogglePin("a"); strings.Join(got, ",") != "d,a" {
		t.Fatalf("pin order = %v, want first-pinned first", got)
	}
	if got := strings.Join(idsOf(m.Visible()), ","); got != "d,a,c,b" {
		t.Fatalf("visible after pin = %s, want pinned first then activity", got)
	}
	view := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 20}).View)
	if !strings.Contains(view, "📌 PINNED (2)") {
		t.Fatalf("missing Pinned heading:\n%s", view)
	}
	if strings.Count(view, "old worktree") != 1 || strings.Count(view, "modal look and feel") != 1 {
		t.Fatalf("pinned rows were duplicated:\n%s", view)
	}
	if !strings.Contains(view, "*") {
		t.Fatalf("pinned row has no pin mark:\n%s", view)
	}
	if got := m.TogglePin("d"); strings.Join(got, ",") != "a" {
		t.Fatalf("unpin = %v", got)
	}
	if m.IsPinned("d") || !m.IsPinned("a") {
		t.Fatal("unpin did not restore pin membership")
	}

	m.SetPinned([]string{"gone", "b", "gone"})
	if got := strings.Join(m.PinnedIDs(), ","); got != "gone,b" {
		t.Fatalf("SetPinned kept %s", got)
	}
	if got := strings.Join(idsOf(m.Visible()), ","); got != "b,c,a,d" {
		t.Fatalf("gone pin leaked into visible: %s", got)
	}
}

func idsOf(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func TestSelectionFollowsIdentityThroughRefreshFilterAndSort(t *testing.T) {
	var m Model
	m.SetItems(items())
	m.Render(RenderOptions{Width: 40, Height: 20})
	if !m.SelectID("c") {
		t.Fatal("could not select a visible identity")
	}

	// A refresh that reorders and renames rows keeps the same item selected.
	refreshed := items()
	refreshed[2].Name = "renamed"
	refreshed = append(refreshed[2:], refreshed[:2]...)
	m.SetItems(refreshed)
	if m.SelectedID() != "c" {
		t.Fatalf("refresh moved the cursor to %q", m.SelectedID())
	}

	// Sorting is presentation: the selected identity does not move.
	m.CycleSort()
	if m.SelectedID() != "c" {
		t.Fatalf("sorting moved the cursor to %q", m.SelectedID())
	}

	// A query that still matches the selection keeps it; one that removes it
	// falls back to the first visible row rather than to a neighbour by index.
	m.FocusFilter()
	for _, r := range "réponse" {
		m.FilterKey(key(string(r)))
	}
	if m.SelectedID() != "c" {
		t.Fatalf("filtering to a matching row moved the cursor to %q", m.SelectedID())
	}
	m.FilterKey(key("ctrl+u"))
	for _, r := range "modal" {
		m.FilterKey(key(string(r)))
	}
	if m.SelectedID() != "a" {
		t.Fatalf("filtering away the selection selected %q, want the first match", m.SelectedID())
	}
}

func TestFilterKeyboardContract(t *testing.T) {
	var m Model
	m.SetItems(items())
	m.Render(RenderOptions{Width: 40, Height: 20})

	// Navigation stays live while filtering.
	m.FocusFilter()
	if result := m.FilterKey(key("down")); result != KeyIgnored {
		t.Fatalf("down was consumed by the filter: %v", result)
	}
	for _, r := range "sidecar" {
		if result := m.FilterKey(key(string(r))); result != KeyHandled {
			t.Fatalf("typing %q was not handled: %v", r, result)
		}
	}
	if matched, total := m.Counts(); matched != 2 || total != 4 {
		t.Fatalf("counts = %d of %d, want 2 of 4", matched, total)
	}

	// Enter accepts: the query stays, focus returns to the list.
	if result := m.FilterKey(key("enter")); result != KeyAccept || m.Filter().Focused() {
		t.Fatalf("enter did not accept: %v focused=%v", result, m.Filter().Focused())
	}
	if !m.Filter().Active() || m.Filter().Query() != "sidecar" {
		t.Fatal("enter discarded the query")
	}

	// First escape clears the query, second releases focus.
	m.FocusFilter()
	if result := m.FilterKey(key("esc")); result != KeyHandled || m.Filter().Query() != "" || !m.Filter().Focused() {
		t.Fatalf("first escape = %v query=%q focused=%v", result, m.Filter().Query(), m.Filter().Focused())
	}
	if result := m.FilterKey(key("esc")); result != KeyExit || m.Filter().Focused() {
		t.Fatalf("second escape = %v focused=%v", result, m.Filter().Focused())
	}
	if matched, _ := m.Counts(); matched != 4 {
		t.Fatalf("clearing the query did not restore the list: %d rows", matched)
	}

	// Pastes go through the same insertion path as keystrokes.
	m.FocusFilter()
	m.Filter().Insert("braid")
	m.Reproject()
	if matched, _ := m.Counts(); matched != 1 {
		t.Fatalf("paste filtered to %d rows, want 1", matched)
	}
}

func TestRenderShowsCountsGroupsNoMatchAndNarrowRows(t *testing.T) {
	var m Model
	m.SetItems(items())

	wide := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 20, Title: "Workspaces", Focused: true}).View)
	for _, want := range []string{"Workspaces", "Activity", "◆ NEEDS ATTENTION (1)", "● WORKING (1)", "○ NO SESSION (1)", "sidecar", "braid"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("wide render is missing %q:\n%s", want, wide)
		}
	}
	for _, line := range strings.Split(wide, "\n") {
		if ansi.StringWidth(line) != 46 {
			t.Fatalf("render produced a %d-wide line in a 46-wide box: %q", ansi.StringWidth(line), line)
		}
	}

	// The filter row is chrome the list only spends a row on while a query is
	// live. A non-empty list keeps one quiet row between chrome and content.
	if strings.Contains(wide, "/ filter") {
		t.Fatalf("an unfiltered list drew the filter row:\n%s", wide)
	}
	// The first visible section has one row of breathing room below the panel.
	rows := strings.Split(wide, "\n")
	if strings.TrimSpace(rows[1]) != "" || !strings.Contains(rows[2], "◆ NEEDS ATTENTION (1)") {
		t.Fatalf("rows 1-2 = %q / %q, want one blank then the first heading", rows[1], rows[2])
	}
	m.FocusFilter()
	filtering := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 20, Title: "Workspaces", Focused: true}).View)
	filterRows := strings.Split(filtering, "\n")
	if row := filterRows[1]; !strings.HasPrefix(row, "/ ") {
		t.Fatalf("a live filter drew %q under the title, want its query row:\n%s", row, filtering)
	}
	if strings.TrimSpace(filterRows[2]) != "" || !strings.Contains(filterRows[3], "◆ NEEDS ATTENTION (1)") {
		t.Fatalf("live filter lost the chrome/content spacer:\n%s", filtering)
	}
	m.Filter().Reset()

	// No-match is an explicit state, with counts to explain it.
	m.FocusFilter()
	m.Filter().Insert("zzz")
	m.Reproject()
	empty := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 12}).View)
	if !strings.Contains(empty, "0 of 4") || !strings.Contains(empty, "No workspaces match") {
		t.Fatalf("no-match state is not honest:\n%s", empty)
	}

	m.Filter().Reset()
	m.Reproject()

	// Project and Recent print headings; Name stays a flat run.
	m.SetSort(SortProject)
	projectStyled := m.Render(RenderOptions{Width: 46, Height: 20, Title: "Workspaces"}).View
	projectView := ansi.Strip(projectStyled)
	for _, want := range []string{"○ SIDECAR (2)", "○ BRAID (1)", "○ TD (1)"} {
		if !strings.Contains(projectView, want) {
			t.Fatalf("project sort missing heading %q:\n%s", want, projectView)
		}
	}
	wantProjectHue := segmentForeground(styles.Title.Foreground(styles.ProjectHue("sidecar")).Render("SIDECAR"), "SIDECAR")
	if got := segmentForeground(projectStyled, "SIDECAR"); got != wantProjectHue {
		t.Fatalf("project sort did not carry the stable project key into its heading: got %q want %q", got, wantProjectHue)
	}
	m.SetSort(SortRecent)
	recentView := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 20, Now: time.Date(2026, 8, 11, 12, 30, 0, 0, time.UTC)}).View)
	if !strings.Contains(recentView, "○ NEW (1)") || !strings.Contains(recentView, "○ TODAY (2)") {
		t.Fatalf("recent sort missing buckets:\n%s", recentView)
	}
	if strings.Contains(recentView, "This week") {
		t.Fatalf("recent sort kept an empty bucket:\n%s", recentView)
	}
	m.SetSort(SortName)
	nameView := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 20}).View)
	if strings.Contains(nameView, " (") && strings.Contains(nameView, "SIDECAR (") {
		t.Fatalf("name sort grew project headings:\n%s", nameView)
	}
	m.SetSort(SortActivity)

	// Narrow: one truncated line per row, still exactly the box width.
	m.Filter().Reset()
	m.Reproject()
	narrow := ansi.Strip(m.Render(RenderOptions{Width: 24, Height: 14}).View)
	if !strings.Contains(narrow, "modal") {
		t.Fatalf("narrow render dropped rows:\n%s", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if ansi.StringWidth(line) != 24 {
			t.Fatalf("narrow render produced a %d-wide line: %q", ansi.StringWidth(line), line)
		}
	}
}

// A project whose inventory could not be read has to be visible in the list
// that is missing it — including in the normal case, where the catalog is
// longer than the pane and there are no leftover rows for it to occupy.
func TestFailureRowsSurviveACatalogLongerThanThePane(t *testing.T) {
	var m Model
	var many []Item
	for i := range 40 {
		item := items()[0]
		item.ID = string(rune('a'+i%26)) + strings.Repeat("x", i)
		many = append(many, item)
	}
	m.SetItems(many)
	m.SetFailures([]string{"braid unavailable: not a Git repository"})

	view := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 12}).View)
	if !strings.Contains(view, "braid unavailable") {
		t.Fatalf("failure row was squeezed out by a full viewport:\n%s", view)
	}
	if !strings.Contains(view, "modal look and feel") {
		t.Fatalf("failure row cost the list its items:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) != 46 {
			t.Fatalf("failure row broke the box width: %q", line)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines != 12 {
		t.Fatalf("render produced %d lines in a 12-row box", lines)
	}

	// A long outage list collapses into a count rather than taking the pane.
	failures := make([]string, 0, 20)
	for i := range 20 {
		failures = append(failures, "project"+string(rune('a'+i))+" unavailable: gone")
	}
	m.SetFailures(failures)
	collapsed := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 12}).View)
	if !strings.Contains(collapsed, "more projects unavailable") {
		t.Fatalf("long failure list did not collapse:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "modal look and feel") {
		t.Fatalf("failures pushed the catalog off the screen:\n%s", collapsed)
	}
}

func TestRegionsFollowRenderedGeometry(t *testing.T) {
	var m Model
	m.SetItems(items())
	rendered := m.Render(RenderOptions{Width: 46, Height: 20})
	sort, ok := RegionAt(rendered.Regions, 44, 0)
	if !ok || sort.Kind != RegionSort {
		t.Fatalf("sort region = %#v ok=%v", sort, ok)
	}
	// No filter row is drawn until a query is live, so there is no region for
	// one to click either.
	if filter, ok := RegionAt(rendered.Regions, 3, 1); ok && filter.Kind == RegionFilter {
		t.Fatal("an unfiltered list registered a filter region")
	}
	m.FocusFilter()
	filtering := m.Render(RenderOptions{Width: 46, Height: 20})
	filter, ok := RegionAt(filtering.Regions, 3, 1)
	if !ok || filter.Kind != RegionFilter {
		t.Fatalf("filter region = %#v ok=%v", filter, ok)
	}
	m.Filter().Reset()
	var rows int
	for _, region := range rendered.Regions {
		if region.Kind == RegionRow {
			rows++
			if region.ID == "" {
				t.Fatal("a row region carries no stable identity")
			}
		}
	}
	if rows != 4 {
		t.Fatalf("row regions = %d, want one per drawn row", rows)
	}
}

func TestSelectionClampsAtBothEndsAndScrollFollows(t *testing.T) {
	var m Model
	m.SetItems(items())
	m.Render(RenderOptions{Width: 46, Height: 8})
	for i := 0; i < 10; i++ {
		m.Move(1)
	}
	if selected, _ := m.Selected(); selected.ID != "d" {
		t.Fatalf("moving past the end selected %q", selected.ID)
	}
	for i := 0; i < 10; i++ {
		m.Move(-1)
	}
	if selected, _ := m.Selected(); selected.ID != "c" {
		t.Fatalf("moving past the start selected %q", selected.ID)
	}
	if !m.Bottom() || m.SelectedID() != "d" {
		t.Fatalf("G did not reach the last row: %q", m.SelectedID())
	}
	if !m.Top() || m.SelectedID() != "c" {
		t.Fatalf("g did not reach the first row: %q", m.SelectedID())
	}
}

func TestRelativeAgeUsesTheBoardsUnits(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	// The sub-minute boundary is pinned deliberately: everything below a minute
	// reads "now", and the first minute is the first row that shows a number.
	cases := map[time.Duration]string{
		0:                "now",
		20 * time.Second: "now",
		59 * time.Second: "now",
		time.Minute:      "1m",
		61 * time.Second: "1m",
		5 * time.Minute:  "5m",
		3 * time.Hour:    "3h",
		50 * time.Hour:   "2d",
	}
	for ago, want := range cases {
		if got := RelativeAge(now.Add(-ago), now); got != want {
			t.Fatalf("RelativeAge(-%s) = %q, want %q", ago, got, want)
		}
	}
	if RelativeAge(time.Time{}, now) != "" {
		t.Fatal("a zero change time must render nothing")
	}
}

// Two shells in one project can share a display name; only the tmux session
// name tells them apart, and it is a filter field rather than a rendered one.
func TestFilterSeparatesIdenticallyNamedShellsByTmuxName(t *testing.T) {
	rows := []Item{
		{ID: "a", Name: "shell", Project: "sidecar", TmuxName: "sc-alpha", Status: "live", Group: GroupLive},
		{ID: "b", Name: "shell", Project: "sidecar", TmuxName: "sc-bravo", Status: "live", Group: GroupLive},
	}
	var matched []string
	for _, row := range rows {
		if Match(row, "sc-bravo") {
			matched = append(matched, row.ID)
		}
	}
	if len(matched) != 1 || matched[0] != "b" {
		t.Fatalf("query matched %v, want only the shell with that session", matched)
	}
	var m Model
	for _, row := range rows {
		rendered := ansi.Strip(strings.Join(m.renderRow(row, false, true, 60, time.Now(), true), "\n"))
		if strings.Contains(rendered, row.TmuxName) {
			t.Fatalf("the tmux session name became visible in the row: %q", rendered)
		}
	}
}

func TestGlobalRenderRowIsKindProjectNameAgeThenAgent(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	var m Model
	item := Item{
		ID: "a", Name: "review td-196c42", Project: "sidecar", ProjectKey: "sidecar",
		Provider: "grok", Status: "working", Detail: "td-196c42", Kind: KindWorktree,
		Marker: RowMarker{Icon: "●", Lane: "working"}, ChangedAt: now.Add(-time.Minute),
	}
	lines := m.renderRow(item, false, true, 56, now, true)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	line1, line2 := ansi.Strip(lines[0]), ansi.Strip(lines[1])
	if !strings.Contains(line1, "⑂ sidecar review td-196c42") || !strings.Contains(line1, "1m") {
		t.Fatalf("line 1 = %q, want kind glyph + project + name + age", line1)
	}
	if strings.Contains(line2, "⑂") || !strings.Contains(line2, "grok") {
		t.Fatalf("line 2 = %q, want the agent alone", line2)
	}
	for _, status := range []string{"working", "live", "idle", "ambiguous panes", "no session"} {
		if strings.Contains(line1, status) || strings.Contains(line2, status) {
			t.Fatalf("status text %q leaked onto the row:\n%s\n%s", status, line1, line2)
		}
	}

	shell := item
	shell.Kind = KindShell
	shell.Name = "Shell 2"
	shell.Project = "braid"
	shell.Status = "live"
	shell.Detail = ""
	shell.Marker = RowMarker{Icon: "◎", Tone: MarkerLive}
	shellLines := m.renderRow(shell, false, true, 56, now, true)
	got := ansi.Strip(strings.Join(shellLines, "\n"))
	if !strings.Contains(ansi.Strip(shellLines[0]), "❯ braid Shell 2") {
		t.Fatalf("shell row = %q", got)
	}
	if strings.Contains(got, "live") {
		t.Fatalf("shell row repeated status text: %q", got)
	}
}

// TestProjectSortDropsTheRedundantProjectFromEachRow pins td-ccd6cd: under
// Project sort the heading above a run of rows already names their project, so
// repeating it on every row spends width on a word the reader has just read.
// Every other sort has no such heading and must keep it.
func TestProjectSortDropsTheRedundantProjectFromEachRow(t *testing.T) {
	var m Model
	m.SetItems(items())

	m.SetSort(SortProject)
	byProject := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 24, Title: "Workspaces", Focused: true}).View)
	if !strings.Contains(byProject, "○ SIDECAR (2)") {
		t.Fatalf("project heading missing:\n%s", byProject)
	}
	for _, line := range strings.Split(byProject, "\n") {
		if strings.Contains(line, "sidecar") && !strings.Contains(line, "(") {
			t.Fatalf("a row still repeats its project heading: %q\n%s", line, byProject)
		}
	}
	if !strings.Contains(byProject, "modal look and feel") {
		t.Fatalf("the row itself is gone:\n%s", byProject)
	}

	m.SetSort(SortActivity)
	byActivity := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 24, Title: "Workspaces", Focused: true}).View)
	if !strings.Contains(byActivity, "sidecar modal look and feel") {
		t.Fatalf("a sort with no project heading dropped the project prefix:\n%s", byActivity)
	}
}

// TestPinnedRowsKeepTheirProjectUnderProjectSort guards the exception: the
// Pinned section is not a project section, so its rows are the only ones on
// screen with no heading to inherit a project from.
func TestPinnedRowsKeepTheirProjectUnderProjectSort(t *testing.T) {
	var m Model
	m.SetItems(items())
	m.SetSort(SortProject)
	m.SetPinned([]string{"a"})
	view := ansi.Strip(m.Render(RenderOptions{Width: 46, Height: 24, Title: "Workspaces", Focused: true}).View)
	if !strings.Contains(view, "sidecar modal look and feel") {
		t.Fatalf("a pinned row lost the project it has no heading for:\n%s", view)
	}
}

// The query row's × is a control only where there is something to clear, and
// it is a hit target wherever it is drawn — the same rule the shared field
// states and both Workspaces surfaces inherit.
func TestFilterClearRegionExistsOnlyWithAQuery(t *testing.T) {
	var m Model
	m.SetItems(items())
	m.FocusFilter()
	m.Render(RenderOptions{Width: 46, Height: 20})

	rendered := m.Render(RenderOptions{Width: 46, Height: 20})
	if _, ok := clearRegion(rendered.Regions); ok {
		t.Fatal("an empty query registered a clear control")
	}

	for _, r := range "modal" {
		m.FilterKey(key(string(r)))
	}
	rendered = m.Render(RenderOptions{Width: 46, Height: 20})
	region, ok := clearRegion(rendered.Regions)
	if !ok {
		t.Fatalf("a non-empty query registered no clear control: %+v", rendered.Regions)
	}
	// The region has to win the hit test against the filter row it sits on.
	hit, found := RegionAt(rendered.Regions, region.X, region.Y)
	if !found || hit.Kind != RegionFilterClear {
		t.Fatalf("a click on the × resolved to %v", hit.Kind)
	}
	if !strings.Contains(ansi.Strip(rendered.View), "×") {
		t.Fatalf("the × was registered but not drawn:\n%s", ansi.Strip(rendered.View))
	}

	// The command and the control run the same clear.
	m.ClearFilter()
	rendered = m.Render(RenderOptions{Width: 46, Height: 20})
	if _, ok := clearRegion(rendered.Regions); ok {
		t.Fatal("clearing the query left the × registered")
	}
	if matched, _ := m.Counts(); matched != 4 {
		t.Fatalf("clearing the query did not restore the list: %d rows", matched)
	}
}

func clearRegion(regions []Region) (Region, bool) {
	for _, region := range regions {
		if region.Kind == RegionFilterClear {
			return region, true
		}
	}
	return Region{}, false
}

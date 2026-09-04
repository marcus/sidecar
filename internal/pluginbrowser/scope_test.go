package pluginbrowser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/state"
)

// filteredDescription is testDescription's results collection plus the three
// filter shapes: a choice with a default (the collection's SCOPE), a choice
// without one, and a text filter.
func filteredDescription() pluginhost.Description {
	desc := testDescription()
	desc.Collections[0].Filters = []pluginhost.Filter{
		{
			ID: "scope", Label: "Scope", Kind: pluginhost.FilterChoice, Default: "everything",
			Choices: []pluginhost.FilterOption{
				{ID: "everything", Title: "Everything"},
				{ID: "project", Title: "This project"},
				{ID: "notes", Title: "Notes only"},
			},
		},
		{
			ID: "source", Label: "Source", Kind: pluginhost.FilterChoice, Default: "any",
			Choices: []pluginhost.FilterOption{
				{ID: "any", Title: "Any"},
				{ID: "notes", Title: "notes"},
			},
		},
		{ID: "since", Label: "Since", Kind: pluginhost.FilterText},
	}
	return desc
}

// newFilteredModel is newTestModel over a collection that declares filters,
// with a page already loaded under a query.
func newFilteredModel(t *testing.T, host *fakeHost) *Model {
	t.Helper()
	m := newTestModel(t, host)
	host.desc = filteredDescription()
	run(t, m, m.Refresh())
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	return m
}

func lastList(t *testing.T, host *fakeHost) ListCall {
	t.Helper()
	if len(host.lists) == 0 {
		t.Fatal("no list was ever run")
	}
	return host.lists[len(host.lists)-1]
}

// Choosing a filter re-lists, and the request carries only what is applied.
func TestFilterChangeRelistsWithNewParams(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	before := len(host.lists)

	press(t, m, "v")
	if !m.OverlayOpen() {
		t.Fatal("v did not open the View modal")
	}
	view := strip(m.View())
	for _, want := range []string{"Scope  (scope)", "This project", "Source", "Since"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the View modal is missing %q:\n%s", want, view)
		}
	}

	run(t, m, m.applyViewAction(filterChoicePfx+"scope:project"))
	if m.OverlayOpen() {
		t.Fatal("choosing a filter left the modal open")
	}
	if len(host.lists) != before+1 {
		t.Fatalf("choosing a filter did not re-list: lists = %d", len(host.lists))
	}
	got := lastList(t, host).Params.Filters
	if len(got) != 1 || got["scope"] != "project" {
		t.Fatalf("params.filters = %v, want only scope=project", got)
	}

	// Choosing the default again is choosing nothing: a missing key means the
	// default, so the field goes back to being absent rather than carrying it.
	before = len(host.lists)
	press(t, m, "v")
	run(t, m, m.applyViewAction(filterChoicePfx+"scope:everything"))
	if len(host.lists) != before+1 {
		t.Fatalf("returning to the default did not re-list: lists = %d", len(host.lists))
	}
	if got := lastList(t, host).Params.Filters; got != nil {
		t.Fatalf("params.filters = %v, want the field omitted", got)
	}

	// Re-choosing what is already applied changes nothing and spends no
	// process.
	before = len(host.lists)
	press(t, m, "v")
	run(t, m, m.applyViewAction(filterChoicePfx+"scope:everything"))
	if len(host.lists) != before {
		t.Fatalf("re-choosing the live value spent a list: lists = %d", len(host.lists))
	}
}

// A text filter is committed by Done, and Esc discards what was typed: an
// uncommitted edit is not a choice.
func TestTextFilterCommitsOnDoneAndDiscardsOnEscape(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)

	press(t, m, "v")
	m.overlay.filters[2].text.SetValue("2026-08-01")
	before := len(host.lists)
	run(t, m, m.applyViewAction(viewDoneID))
	if len(host.lists) != before+1 {
		t.Fatalf("Done did not commit the typed filter: lists = %d", len(host.lists))
	}
	if got := lastList(t, host).Params.Filters; got["since"] != "2026-08-01" {
		t.Fatalf("params.filters = %v", got)
	}

	press(t, m, "v")
	m.overlay.filters[2].text.SetValue("2026-09-09")
	before = len(host.lists)
	run(t, m, m.applyViewAction("cancel"))
	if len(host.lists) != before {
		t.Fatalf("Esc applied an uncommitted edit: lists = %d", len(host.lists))
	}
	if m.activeState().filters["since"] != "2026-08-01" {
		t.Fatalf("filters = %v, want the committed value kept", m.activeState().filters)
	}
}

// An undeclared key never reaches the plugin, even when something put one in
// the browser's own state — a restored tab from a plugin that has since renamed
// its filter, say.
func TestUndeclaredFilterKeysNeverReachThePlugin(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	s := m.activeState()
	s.filters = map[string]string{"scope": "project", "smuggled": "x", "source": "any"}
	run(t, m, m.list(m.desc.Collections[0], s, false))
	got := lastList(t, host).Params.Filters
	if len(got) != 1 || got["scope"] != "project" {
		t.Fatalf("params.filters = %v; undeclared keys and defaults must be dropped", got)
	}
}

// The pill always names the scope when the collection declares filters, adds a
// count for the others, and sheds one rung at a time as the box narrows.
func TestViewPillLadderShedsFiltersThenScopeThenWord(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	s := m.activeState()
	s.filters = map[string]string{"scope": "project", "source": "notes", "since": "2026-08-01"}
	c := m.desc.Collections[0]

	ladder := m.viewPillLadder(c)
	want := []string{
		"⇅ Relevance · This project · 2 filters",
		"⇅ Relevance · This project",
		"⇅ Relevance",
		"⇅",
	}
	if len(ladder) != len(want) {
		t.Fatalf("ladder = %q, want %q", ladder, want)
	}
	for i := range want {
		if ladder[i] != want[i] {
			t.Fatalf("rung %d = %q, want %q", i, ladder[i], want[i])
		}
	}

	// Four widths, one per rung: the frame keeps the widest form that fits and
	// never draws a clipped one.
	for _, tc := range []struct {
		width int
		want  string
	}{
		{160, want[0]},
		{60, want[1]},
		{46, want[2]},
		{34, want[3]},
	} {
		m.SetSize(tc.width, 45)
		row, _ := m.titleRow(c, tc.width-chromeOverhead)
		if !strings.Contains(strip(row), tc.want) {
			t.Fatalf("at width %d the pill row is %q, want it to carry %q", tc.width, strip(row), tc.want)
		}
	}
}

// One applied filter beyond the scope reads "1 filter", not "1 filters".
func TestPillCountsOneFilterInTheSingular(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	m.activeState().filters = map[string]string{"source": "notes"}
	if got := m.viewControlLabel(); got != "⇅ Relevance · Everything · 1 filter" {
		t.Fatalf("pill = %q", got)
	}
}

// A collection with no filters keeps the label it had.
func TestPillIsUnchangedWithoutFilters(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	if got := m.viewControlLabel(); got != "⇅ Relevance" {
		t.Fatalf("pill = %q", got)
	}
}

// A collection tab's applied filters survive a save and a restore, and a value
// the newest describe can no longer express is dropped on the way back in.
func TestPaneFiltersRoundTripThroughTheTabRecord(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	host.described = true
	host.status = pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}
	host.desc = filteredDescription()
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(120, 40)
	m.SetPaneCollection("results")
	run(t, m, m.Refresh())
	s := m.activeState()
	s.setQuery("dex")
	s.filters = map[string]string{"scope": "project", "since": "2026-08-01"}

	saved := m.PaneFilters()
	ref := resource.Reference{
		Instance: "fixture", Collection: "results", Query: m.PaneQuery(),
		Filters: resource.FilterValues(saved),
	}
	if !ref.Valid() {
		t.Fatal("a reference carrying applied filters does not survive Reference.Valid")
	}

	// The round trip a persisted record makes: map -> sorted slice -> canonical
	// string -> back.
	back := resource.DecodeFilters(resource.EncodeFilters(ref.Filters))
	if len(back) != 2 || back[0].ID != "scope" || back[1].Value != "2026-08-01" {
		t.Fatalf("round trip = %+v", back)
	}

	restored := New("fixture", "fixture", host.calls(), nil)
	restored.SetSize(120, 40)
	restored.SetPaneCollection("results")
	restored.RestorePaneView("dex", "", "", "", resource.FilterMap(back))
	run(t, restored, restored.Refresh())
	if got := restored.PaneFilters(); len(got) != 2 || got["scope"] != "project" {
		t.Fatalf("restored filters = %v", got)
	}

	// A plugin that dropped a choice must not have the host asking for it
	// forever: the value is not adopted, and nothing is sent for it.
	gone := New("fixture", "fixture", host.calls(), nil)
	gone.SetSize(120, 40)
	gone.SetPaneCollection("results")
	gone.RestorePaneView("dex", "", "", "", map[string]string{"scope": "atlantis", "nowhere": "x"})
	run(t, gone, gone.Refresh())
	if got := gone.PaneFilters(); got != nil {
		t.Fatalf("restored filters = %v, want none adopted", got)
	}
}

// A global tab has no pane record, so it remembers its own position — query,
// view, sort and filters — in the per-plugin state file.
func TestGlobalTabRemembersItsQueryAndFilters(t *testing.T) {
	clearTabView(t, "fixture", "results")
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	s := m.activeState()
	s.sortKey = "recency"
	s.filters = map[string]string{"scope": "project"}
	run(t, m, m.list(m.desc.Collections[0], s, false))

	saved := state.GetPluginBrowserView("fixture", "results")
	if saved.Query != "dex" || saved.Sort != "recency" || saved.Filters["scope"] != "project" {
		t.Fatalf("saved view = %+v", saved)
	}

	// A fresh browser for the same plugin opens where the last one left off.
	next := New("fixture", "fixture", host.calls(), nil)
	next.SetSize(160, 45)
	run(t, next, next.Refresh())
	restored := next.activeState()
	if restored.queryText() != "dex" || restored.sortKey != "recency" || restored.filters["scope"] != "project" {
		t.Fatalf("restored state = query %q sort %q filters %v", restored.queryText(), restored.sortKey, restored.filters)
	}

	// A pane-mode browser writes nothing here: its position rides on the tab
	// record the surface saves, and two authorities on one position disagree.
	clearTabView(t, "fixture", "results")
	pane := New("fixture", "fixture", host.calls(), nil)
	pane.SetSize(120, 40)
	pane.SetPaneCollection("results")
	run(t, pane, pane.Refresh())
	ps := pane.activeState()
	ps.setQuery("other")
	run(t, pane, pane.list(pane.desc.Collections[0], ps, false))
	if got := state.GetPluginBrowserView("fixture", "results"); !got.Empty() {
		t.Fatalf("a pane wrote the global tab's remembered view: %+v", got)
	}
}

// The coverage modal explains the page: the outcome and what it means, every
// notice in full, omitted as two lines, and one table row per source — held to
// a box far too small for all of it.
func TestCoverageModalRendersTheTableAndIsHeldToItsBox(t *testing.T) {
	host := &fakeHost{page: coverageTestPage()}
	m := newFilteredModel(t, host)
	m.SetSize(80, 24)

	if !m.hasCoverage() {
		t.Fatal("a degraded page with a coverage ledger has something to explain")
	}
	press(t, m, "c")
	if !m.OverlayOpen() {
		t.Fatal("c did not open the coverage modal")
	}
	view := strip(m.View())
	// The modal opens on the claim it is explaining, with the table under it.
	for _, want := range []string{
		"Coverage · Results",
		"degraded",
		"Some source that should have answered could not",
		"⚠  5 of 13 sources did not answer",
		"1 below the relevance floor",
		"6 over the budget",
		"SOURCE", "STATE", "ELAPSED", "REASON",
		"mail      unhealthy",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("the coverage modal is missing %q:\n%s", want, view)
		}
	}
	// Held to its box: every line fits the frame and there are no more lines
	// than rows. Thirteen sources do not fit in 24, which is exactly why the
	// body scrolls rather than the modal growing past the frame.
	lines := strings.Split(view, "\n")
	if len(lines) > 24 {
		t.Fatalf("the modal drew %d lines into a 24-row box", len(lines))
	}
	for i, line := range lines {
		if w := len([]rune(line)); w > 80 {
			t.Fatalf("line %d is %d cells wide in an 80-column box: %q", i, w, line)
		}
	}
	if strings.Contains(view, "Retry") {
		t.Fatalf("the buttons are visible without scrolling; this box cannot hold the whole modal:\n%s", view)
	}

	// Scrolling reaches the rest, buttons included.
	m.overlay.box.ScrollToBottom()
	bottom := strip(m.View())
	for _, want := range []string{"archive", "Retry", "Done"} {
		if !strings.Contains(bottom, want) {
			t.Fatalf("scrolled to the bottom, the modal is missing %q:\n%s", want, bottom)
		}
	}
	if len(strings.Split(bottom, "\n")) > 24 {
		t.Fatal("the scrolled modal grew past its box")
	}

	// Retry is a key as well as a button, because nothing here may be reachable
	// only by pointer.
	before := len(host.lists)
	press(t, m, "r")
	if m.OverlayOpen() {
		t.Fatal("r left the coverage modal open")
	}
	if len(host.lists) != before+1 {
		t.Fatalf("r did not retry: lists = %d", len(host.lists))
	}
}

// A page's coverage table names the state of every source, in the host's own
// tones rather than the plugin's.
func TestCoverageTableRendersEveryRow(t *testing.T) {
	rows := coverageTestPage().Coverage
	if len(rows) != 13 {
		t.Fatalf("the fixture page carries %d coverage rows, want 13", len(rows))
	}
	table := coverageTable(rows, 72)
	if len(table) != len(rows)+1 {
		t.Fatalf("table = %d lines, want a heading plus %d rows", len(table), len(rows))
	}
	joined := strip(strings.Join(table, "\n"))
	for _, want := range []string{"mail", "unhealthy", "checkpoint stale", "timeout", "skipped", "failed", "2.0s"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the table is missing %q:\n%s", want, joined)
		}
	}
	// A source that did not report an elapsed time gets a dash, not "0ms":
	// zero milliseconds is a measurement and silence is not.
	if !strings.Contains(joined, "—") {
		t.Fatalf("an unreported elapsed time was rendered as a number:\n%s", joined)
	}
}

// A failed page is an error card. It never says "no matches", which would be a
// claim nothing made.
func TestFailedPageRendersAnErrorCardAndNeverNoMatches(t *testing.T) {
	host := &fakeHost{page: pluginhost.Page{
		Outcome: pluginhost.OutcomeFailed,
		Notices: []pluginhost.Notice{{Tone: resource.ToneDanger, Text: "every source failed"}},
		Coverage: []pluginhost.Coverage{
			{Source: "notes", State: pluginhost.CoverageFailed, Reason: "index missing"},
		},
	}}
	m := newFilteredModel(t, host)
	view := strip(m.View())
	if strings.Contains(view, "No matches") || strings.Contains(view, "no matches") {
		t.Fatalf("a failed page claimed there were no matches:\n%s", view)
	}
	for _, want := range []string{"Nothing could be asked.", "Every source this page needed failed", "c  what failed", "failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the failed card is missing %q:\n%s", want, view)
		}
	}
	status, isErr := m.plugin(t).FooterStatus()
	if !isErr || !strings.Contains(status, "failed") {
		t.Fatalf("footer = %q (err=%v)", status, isErr)
	}
}

// A required-search collection nobody has typed into is UNQUERIED, not
// abstained: nothing was asked, so nothing was claimed. The word never appears,
// the coverage card is unavailable, and the footer says what is true.
func TestUnqueriedCollectionNeverSaysAbstained(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	s := m.activeState()
	if !s.unqueried {
		t.Fatal("a required-search collection with no query is not marked unqueried")
	}
	if s.outcome == pluginhost.OutcomeAbstained {
		t.Fatalf("outcome = %q; nothing was asked, so nothing abstained", s.outcome)
	}
	view := strip(m.View())
	if strings.Contains(view, "abstained") {
		t.Fatalf("the unqueried page says abstained:\n%s", view)
	}
	for _, want := range []string{"This collection needs a query.", "no query"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the unqueried page is missing %q:\n%s", want, view)
		}
	}
	if m.hasCoverage() {
		t.Fatal("a collection that was never asked has a coverage card")
	}
	if m.ClaimsKey("c") {
		t.Fatal("c is claimed where there is nothing to explain")
	}
	status, isErr := m.plugin(t).FooterStatus()
	if isErr || !strings.Contains(status, "waiting for a query") {
		t.Fatalf("footer = %q (err=%v)", status, isErr)
	}
	if strings.Contains(status, "abstained") {
		t.Fatalf("the footer says abstained for a query that was never run: %q", status)
	}
}

// coverageTestPage is the fixture's `coverage` query in host types: a degraded
// page with omitted counts and thirteen sources.
func coverageTestPage() pluginhost.Page {
	page := testPage(2)
	page.Outcome = pluginhost.OutcomeDegraded
	page.Notices = []pluginhost.Notice{
		{Tone: resource.ToneWarning, Text: "5 of 13 sources did not answer (mail: checkpoint stale)"},
	}
	page.Omitted = pluginhost.Omitted{Suppressed: 1, Dropped: 6}
	rows := []struct {
		source  string
		state   pluginhost.CoverageState
		reason  string
		elapsed int
	}{
		{"notes", pluginhost.CoverageAnswered, "", 12},
		{"shell", pluginhost.CoverageAnswered, "", 31},
		{"td", pluginhost.CoverageAnswered, "", 44},
		{"mail", pluginhost.CoverageUnhealthy, "checkpoint stale since 2026-08-30", 2},
		{"calendar", pluginhost.CoverageTimeout, "no answer within the 2s budget", 2000},
		{"web", pluginhost.CoverageSkipped, "not in the selected profile", 0},
		{"slack", pluginhost.CoverageFailed, "auth token expired", 8},
		{"drive", pluginhost.CoverageAnswered, "", 77},
		{"photos", pluginhost.CoverageSkipped, "not in the selected profile", 0},
		{"music", pluginhost.CoverageAnswered, "", 5},
		{"books", pluginhost.CoverageUnhealthy, "index rebuilt 4 minutes ago", 19},
		{"contacts", pluginhost.CoverageAnswered, "", 6},
		{"archive", pluginhost.CoverageTimeout, "no answer within the 2s budget", 2000},
	}
	for _, row := range rows {
		page.Coverage = append(page.Coverage, pluginhost.Coverage{
			Source: row.source, State: row.state, Reason: row.reason, ElapsedMs: row.elapsed,
		})
	}
	return page
}

// plugin wraps the model in its TabPlugin, which is what owns the footer.
func (m *Model) plugin(t *testing.T) *TabPlugin {
	t.Helper()
	return &TabPlugin{id: m.instance, model: m}
}

func lastGet(t *testing.T, host *fakeHost) GetCall {
	t.Helper()
	if len(host.gets) == 0 {
		t.Fatal("no get was ever run")
	}
	return host.gets[len(host.gets)-1]
}

// Expanding a row carries the scope the row was found under. A row is only
// visible because of the filters the list ran with, so a get that dropped them
// would expand it under the plugin's declared defaults — a different document
// at best, and a refusal at worst.
func TestGetCarriesTheListsAppliedFilters(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	s := m.activeState()
	s.filters = map[string]string{"scope": "project", "since": "2026-08-01"}

	run(t, m, m.openDocument("results", "rc:notes:1", openReplace))
	got := lastGet(t, host).Params.Filters
	if len(got) != 2 || got["scope"] != "project" || got["since"] != "2026-08-01" {
		t.Fatalf("get params.filters = %v, want the applied set the list was run with", got)
	}

	// The same narrowing list sends: an undeclared key and a value equal to its
	// filter's own default never reach the plugin.
	s.filters = map[string]string{"scope": "project", "source": "any", "smuggled": "x"}
	run(t, m, m.openDocument("results", "rc:notes:1", openReplace))
	if got := lastGet(t, host).Params.Filters; len(got) != 1 || got["scope"] != "project" {
		t.Fatalf("get params.filters = %v; undeclared keys and defaults must be dropped", got)
	}

	// Nothing applied is the declared defaults, which is an absent field rather
	// than an object full of them.
	s.filters = nil
	run(t, m, m.openDocument("results", "rc:notes:1", openReplace))
	if got := lastGet(t, host).Params.Filters; got != nil {
		t.Fatalf("get params.filters = %v, want the field omitted", got)
	}
}

// A document tab has no list of its own: it was opened from one, or restored
// from a tab record, so the scope it was armed with is the scope it expands
// under.
func TestPaneDocumentGetCarriesItsArmedFilters(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	m.ArmPaneDocument("results", "rc:notes:1", map[string]string{"scope": "project"})
	run(t, m, m.SetPaneDocument("results", "rc:notes:1", map[string]string{"scope": "project"}))

	if got := lastGet(t, host).Params.Filters; len(got) != 1 || got["scope"] != "project" {
		t.Fatalf("get params.filters = %v, want the armed scope", got)
	}
}

// Enter on a row in a pane opens the row as a second tab, and the scope travels
// with it: the host builds that tab's reference from what this list was run
// with.
func TestPaneOpenRowHandsOnTheAppliedFilters(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newFilteredModel(t, host)
	m.SetPaneCollection("results")
	s := m.activeState()
	s.filters = map[string]string{"scope": "project"}

	var handed map[string]string
	m.SetOnOpenRow(func(_, _ string, filters map[string]string) tea.Cmd {
		handed = filters
		return nil
	})
	press(t, m, "enter")
	if len(handed) != 1 || handed["scope"] != "project" {
		t.Fatalf("the row was handed %v, want the list's applied scope", handed)
	}
}

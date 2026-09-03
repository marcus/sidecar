package pluginbrowser

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/state"
)

// fakeHost is an in-memory stand-in for the plugin host. It records every call
// so a test can assert what the browser asked for, and answers synchronously so
// a test can run the returned command itself.
type fakeHost struct {
	desc      pluginhost.Description
	status    pluginhost.Status
	described bool

	page    pluginhost.Page
	pageErr error
	doc     resource.Document
	docErr  error
	outcome pluginhost.Outcome
	actErr  error

	lists   []ListCall
	gets    []GetCall
	acts    []ActCall
	opened  []string
	project *pluginhost.ProjectContext
}

func (f *fakeHost) calls() Calls {
	return Calls{
		Describe: func(string) (pluginhost.Description, pluginhost.Status, bool) {
			return f.desc, f.status, f.described
		},
		List: func(call ListCall) tea.Cmd {
			f.lists = append(f.lists, call)
			return func() tea.Msg {
				return ListedMsg{
					Instance:   call.Instance,
					Browser:    call.Browser,
					Collection: call.Params.Collection,
					Generation: call.Generation,
					Append:     call.Append,
					Page:       f.page,
					Err:        f.pageErr,
				}
			}
		},
		Get: func(call GetCall) tea.Cmd {
			f.gets = append(f.gets, call)
			return func() tea.Msg {
				return GotMsg{
					Instance:   call.Instance,
					Browser:    call.Browser,
					Collection: call.Params.Collection,
					ID:         call.Params.ID,
					Generation: call.Generation,
					Document:   f.doc,
					Err:        f.docErr,
				}
			}
		},
		Act: func(call ActCall) tea.Cmd {
			f.acts = append(f.acts, call)
			return func() tea.Msg {
				return ActedMsg{
					Instance:   call.Instance,
					Browser:    call.Browser,
					Action:     call.Params.Action,
					Generation: call.Generation,
					Outcome:    f.outcome,
					Err:        f.actErr,
				}
			}
		},
		OpenURL: func(url string) tea.Cmd {
			f.opened = append(f.opened, url)
			return nil
		},
		Context: func() *pluginhost.Context {
			if f.project == nil {
				return nil
			}
			return &pluginhost.Context{Project: f.project}
		},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	}
}

func testDescription() pluginhost.Description {
	return pluginhost.Description{
		Info:    pluginhost.Info{Kind: "fixture", Name: "Fixture"},
		Context: []pluginhost.ContextKind{pluginhost.ContextProject},
		Collections: []pluginhost.Collection{
			{
				ID: "results", Title: "Results", Search: pluginhost.SearchRequired, Detail: true,
				Columns: []pluginhost.Column{
					{ID: "rank", Label: "#", Width: 3, Align: pluginhost.AlignRight, Kind: pluginhost.ColumnNumber},
					{ID: "title", Label: "Title", Primary: true},
					{ID: "source", Label: "Source", Width: 14},
					{ID: "excerpt", Label: "Excerpt", Secondary: true},
				},
				Sort: []pluginhost.SortKey{
					{ID: "relevance", Label: "Relevance", Default: pluginhost.SortDesc},
					{ID: "recency", Label: "Recency"},
				},
			},
			{
				ID: "sources", Title: "Sources", Search: pluginhost.SearchNone, Detail: true,
				Columns: []pluginhost.Column{
					{ID: "name", Label: "Source", Primary: true},
					{ID: "health", Label: "Health", Kind: pluginhost.ColumnStatus},
				},
				Views: []pluginhost.View{{ID: "all", Title: "All"}, {ID: "stale", Title: "Stale"}},
			},
		},
		Actions: []pluginhost.Action{
			{
				ID: "refresh-source", Title: "Refresh source", On: pluginhost.ActionOnItem,
				Collection: "sources", Mutates: true, Confirm: true, Key: "R",
			},
			{
				ID: "log-note", Title: "Log note", On: pluginhost.ActionOnItem, Collection: "results",
				Mutates: true,
				Inputs: []pluginhost.ActionInput{
					{ID: "text", Label: "Note", Kind: pluginhost.InputMultiline, Required: true},
				},
			},
			{ID: "capture", Title: "Capture", On: pluginhost.ActionOnCollection, Collection: "results"},
		},
	}
}

func testPage(n int) pluginhost.Page {
	items := make([]pluginhost.Item, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, pluginhost.Item{
			ID: "rc:notes:" + itoa(i),
			Cells: map[string]string{
				"rank": itoa(i), "title": "Row " + itoa(i),
				"source": "notes", "excerpt": "…excerpt " + itoa(i) + "…",
			},
			Status:    &resource.Status{Label: "exact", Tone: resource.ToneSuccess},
			SourceURL: "https://fixture.example.test/" + itoa(i),
		})
	}
	return pluginhost.Page{Outcome: pluginhost.OutcomeAnswered, Items: items, Total: n}
}

// newTestModel builds a described browser with a loaded results page.
func newTestModel(t *testing.T, host *fakeHost) *Model {
	t.Helper()
	// A global tab remembers its query, view, sort and filters between
	// launches, and every test in this package shares one isolated state file.
	// Clearing the entry is what makes each test start where a first launch
	// does rather than where the previous test left off.
	clearTabView(t, "fixture", "results")
	clearTabView(t, "fixture", "sources")
	host.described = true
	host.status = pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}
	host.desc = testDescription()
	host.project = &pluginhost.ProjectContext{Root: "/tmp/p", WorkDir: "/tmp/p", Name: "p"}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(160, 45)
	m.SetReservedKeys(map[string]bool{"1": true, "q": true})
	run(t, m, m.Refresh())
	return m
}

// clearTabView forgets one global tab's remembered view position.
func clearTabView(t *testing.T, instance, collection string) {
	t.Helper()
	if err := state.SetPluginBrowserView(instance, collection, state.PluginBrowserViewJSON{}); err != nil {
		t.Fatalf("clearing the remembered view: %v", err)
	}
}

// run executes a command and feeds whatever it produced back into the model,
// following batches, which is what the Bubble Tea runtime does.
func run(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 8; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next []tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				sub := c()
				if sub != nil {
					next = append(next, m.Update(sub))
				}
			}
			cmd = tea.Batch(next...)
			continue
		}
		cmd = m.Update(msg)
	}
}

func press(t *testing.T, m *Model, key string) {
	t.Helper()
	cmd, _ := m.HandleKey(keyPress(key))
	run(t, m, cmd)
}

// keyPress builds the key message the app forwards. A single-rune key carries
// its own text, which is what the query line types.
func keyPress(key string) tea.KeyPressMsg {
	if len([]rune(key)) == 1 {
		return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}
	return tea.KeyPressMsg{Code: keyCode(key)}
}

func keyCode(key string) rune {
	switch key {
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	case "backspace":
		return tea.KeyBackspace
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "tab":
		return tea.KeyTab
	default:
		return 0
	}
}

func strip(s string) string { return ansi.Strip(s) }

// Nothing renders a populated pane before describe has landed: a protocol
// plugin's first process runs after the first ready frame, and until it answers
// the tab has to say so.
func TestLoadingUntilDescribeLands(t *testing.T) {
	host := &fakeHost{}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(120, 30)
	if cmd := m.Refresh(); cmd != nil {
		t.Fatal("Refresh started work before describe landed")
	}
	view := strip(m.View())
	if !strings.Contains(view, "Asking fixture what it offers") {
		t.Fatalf("loading card missing:\n%s", view)
	}
	if len(host.lists) != 0 {
		t.Fatalf("listed %d times before describe", len(host.lists))
	}
}

// A describe that failed is a setup card built from the plugin's own typed
// reason, not a spinner for something that is not loading.
func TestDescribeFailureRendersTheSetupCard(t *testing.T) {
	host := &fakeHost{described: true, status: pluginhost.Status{
		Instance: "fixture",
		State:    pluginhost.StateTemporarilyFailed,
		LastError: &resource.Error{
			Code:      resource.CodeInvalidConfig,
			Message:   "fixture credentials are missing",
			SetupHint: "run fixtureprovider configure",
		},
	}}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(120, 30)
	run(t, m, m.Refresh())
	view := strip(m.View())
	for _, want := range []string{"not configured", "credentials are missing", "Setup", "run fixtureprovider configure"} {
		if !strings.Contains(view, want) {
			t.Fatalf("setup card is missing %q:\n%s", want, view)
		}
	}
}

// A required-search collection with an empty query is answered by the host. No
// process is started, which is what keeps a search box free until there is
// something to search for.
func TestRequiredSearchWithNoQueryStartsNoProcess(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	if len(host.lists) != 0 {
		t.Fatalf("an empty required query started %d list calls", len(host.lists))
	}
	view := strip(m.View())
	if !strings.Contains(view, "This collection needs a query.") {
		t.Fatalf("empty-query state missing:\n%s", view)
	}
	if !strings.Contains(view, "no query") {
		t.Fatalf("the query row does not say there is no query:\n%s", view)
	}
}

// Typing schedules one debounce per keystroke and only the newest one spends a
// process. An earlier tick is a keystroke the user has already typed past.
func TestQueryDebounceRunsOnlyTheNewestKeystroke(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	press(t, m, "/")
	if !m.ConsumesTextInput() {
		t.Fatal("the query line does not report that it is taking text")
	}
	// The returned tick is deliberately dropped: a test that ran it would be
	// waiting out the debounce it is here to prove exists.
	for _, key := range []string{"d", "e", "x"} {
		if cmd, _ := m.HandleKey(keyPress(key)); cmd == nil {
			t.Fatalf("typing %q scheduled no debounce", key)
		}
	}
	if len(host.lists) != 0 {
		t.Fatalf("typing listed %d times before the debounce elapsed", len(host.lists))
	}
	s := m.activeState()
	if s.queryText() != "dex" {
		t.Fatalf("query = %q, want dex", s.queryText())
	}
	// The two superseded ticks do nothing at all.
	run(t, m, m.Update(QueryDebouncedMsg{Instance: "fixture", Collection: "results", Sequence: s.debounce - 1}))
	if len(host.lists) != 0 {
		t.Fatalf("a superseded debounce listed %d times", len(host.lists))
	}
	run(t, m, m.Update(QueryDebouncedMsg{Instance: "fixture", Collection: "results", Sequence: s.debounce}))
	if len(host.lists) != 1 {
		t.Fatalf("the newest debounce listed %d times, want 1", len(host.lists))
	}
	if host.lists[0].Params.Query != "dex" {
		t.Fatalf("listed query = %q, want dex", host.lists[0].Params.Query)
	}
	if host.lists[0].PaneKey == "" {
		t.Fatal("a list with no pane key can never be superseded")
	}
	if host.lists[0].Context == nil || host.lists[0].Context.Project == nil {
		t.Fatal("the list carried no project context")
	}
	view := strip(m.View())
	if !strings.Contains(view, "Row 1") || !strings.Contains(view, "answered") {
		t.Fatalf("the page did not render:\n%s", view)
	}
}

// A page for a query the user has moved past is discarded rather than painted
// over the newer one.
func TestStalePageIsDiscarded(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	if len(s.items) != 3 {
		t.Fatalf("items = %d, want 3", len(s.items))
	}
	m.Update(ListedMsg{
		Instance: "fixture", Browser: m.id, Collection: "results", Generation: s.generation - 1,
		Page: pluginhost.Page{Outcome: pluginhost.OutcomeAbstained},
	})
	if len(s.items) != 3 {
		t.Fatalf("a stale page replaced the live one: items = %d", len(s.items))
	}
}

// Enter opens the row; a second Enter on the same row moves the keyboard into
// the detail box and costs no process.
func TestEnterOpensThenFocuses(t *testing.T) {
	host := &fakeHost{page: testPage(3), doc: resource.Document{
		Identity: "rc:notes:1", Title: "Row 1", Subtitle: "notes · 2026-08-14",
		Status: &resource.Status{Label: "exact", Tone: resource.ToneSuccess},
		Fields: []resource.Field{{Label: "Source", Value: "notes"}},
		Body:   &resource.Body{Format: resource.FormatMarkdown, Text: "body text"},
		Sections: []resource.Section{
			{Title: "Evidence", Body: &resource.Body{Format: resource.FormatMarkdown, Text: "evidence"}},
			{Title: "Timeline", Items: []resource.TimelineItem{
				{When: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), Title: "Note added"},
			}},
		},
	}}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	press(t, m, "enter")
	if len(host.gets) != 1 || host.gets[0].Params.ID != "rc:notes:1" {
		t.Fatalf("get calls = %+v", host.gets)
	}
	view := strip(m.View())
	for _, want := range []string{"Row 1", "Source", "── Evidence", "── Timeline", "2w ago", "Note added"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail is missing %q:\n%s", want, view)
		}
	}
	press(t, m, "enter")
	if len(host.gets) != 1 {
		t.Fatalf("the second Enter spent a process: gets = %d", len(host.gets))
	}
	if m.PaneFocus() != string(FocusDetail) {
		t.Fatalf("focus = %q, want detail", m.PaneFocus())
	}
	stops := m.PaneFocusStops()
	if len(stops) != 2 {
		t.Fatalf("focus stops = %v, want both windows once a document is open", stops)
	}
}

// Moving off the end of a page that has more behind it fetches the next one.
// Nothing is fetched eagerly.
func TestPagingHappensOnDemand(t *testing.T) {
	host := &fakeHost{}
	page := testPage(3)
	page.NextCursor = "page-2"
	host.page = page
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	if len(host.lists) != 1 {
		t.Fatalf("lists = %d, want the first page only", len(host.lists))
	}
	press(t, m, "j")
	press(t, m, "j")
	if len(host.lists) != 1 {
		t.Fatalf("paging happened before the cursor reached the end: lists = %d", len(host.lists))
	}
	press(t, m, "j")
	if len(host.lists) != 2 {
		t.Fatalf("reaching past the last row did not page: lists = %d", len(host.lists))
	}
	if !host.lists[1].Append || host.lists[1].Params.Cursor != "page-2" {
		t.Fatalf("the paging call was %+v", host.lists[1].Params)
	}
}

// The View modal offers the declared sort keys and views, and choosing one
// re-lists rather than re-sorting in the host: the plugin owns the order.
func TestViewModalChangesSortAndRelists(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	before := len(host.lists)

	press(t, m, "v")
	if !m.OverlayOpen() || !m.BlocksGlobalKeys() {
		t.Fatal("v did not open the View modal")
	}
	view := strip(m.View())
	for _, want := range []string{"View · Results", "Current sort: Relevance", "Recency", "Done"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View modal is missing %q:\n%s", want, view)
		}
	}
	run(t, m, m.applyViewAction("sort:recency"))
	if m.OverlayOpen() {
		t.Fatal("choosing a sort left the modal open")
	}
	if len(host.lists) != before+1 {
		t.Fatalf("choosing a sort did not re-list: lists = %d", len(host.lists))
	}
	if host.lists[len(host.lists)-1].Params.Sort.Key != "recency" {
		t.Fatalf("sort = %+v, want recency", host.lists[len(host.lists)-1].Params.Sort)
	}
	if label := m.viewControlLabel(); !strings.Contains(label, "Recency") {
		t.Fatalf("the View pill still reads %q", label)
	}
}

// An action with inputs shows a form; the form is the confirm step, and a
// required input left empty stops before the plugin is called.
func TestActionFormCollectsInputs(t *testing.T) {
	host := &fakeHost{page: testPage(2), outcome: pluginhost.Outcome{
		Status: pluginhost.ActDone, Message: "Logged a note for rc:notes:1", Refresh: []string{"results"},
	}}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	press(t, m, "a")
	if !m.OverlayOpen() {
		t.Fatal("a did not open the action menu")
	}
	menu := strip(m.View())
	if !strings.Contains(menu, "Log note") || !strings.Contains(menu, "Capture") {
		t.Fatalf("the action menu is missing an applicable action:\n%s", menu)
	}
	if strings.Contains(menu, "Refresh source") {
		t.Fatalf("an action for another collection is offered:\n%s", menu)
	}
	run(t, m, m.applyMenuAction("act:log-note"))
	if m.overlay.kind != overlayForm {
		t.Fatalf("overlay = %v, want the form", m.overlay.kind)
	}
	form := strip(m.View())
	if !strings.Contains(form, "Note *") {
		t.Fatalf("the form does not mark the required input:\n%s", form)
	}

	run(t, m, m.submitAction())
	if len(host.acts) != 0 {
		t.Fatal("an empty required input still called the plugin")
	}
	if !strings.Contains(strip(m.View()), "Note is required.") {
		t.Fatalf("the form said nothing about the empty input:\n%s", strip(m.View()))
	}

	m.overlay.inputs[0].area.SetValue("met at the workshop")
	run(t, m, m.submitAction())
	if len(host.acts) != 1 {
		t.Fatalf("acts = %d, want 1", len(host.acts))
	}
	got := host.acts[0].Params
	if got.Action != "log-note" || got.Collection != "results" || got.ID != "rc:notes:1" {
		t.Fatalf("act params = %+v", got)
	}
	if got.Inputs["text"] != "met at the workshop" {
		t.Fatalf("inputs = %+v", got.Inputs)
	}
	if flash, isErr := m.Flash(); flash == "" || isErr {
		t.Fatalf("outcome flash = %q err=%v", flash, isErr)
	}
	// The outcome named a collection to re-list, and the browser did.
	if host.lists[len(host.lists)-1].Params.Collection != "results" {
		t.Fatalf("the outcome's refresh did not re-list: %+v", host.lists)
	}
}

// A mutating action with no inputs gets a confirm step, because there is no
// form to be the confirmation.
func TestMutatingActionWithNoInputsConfirms(t *testing.T) {
	host := &fakeHost{page: testPage(2), outcome: pluginhost.Outcome{Status: pluginhost.ActDone, Message: "Refreshed"}}
	m := newTestModel(t, host)
	m.active = 1 // sources
	run(t, m, m.ensureListed())

	run(t, m, m.startAction("refresh-source"))
	if m.overlay.kind != overlayConfirm {
		t.Fatalf("overlay = %v, want a confirm step", m.overlay.kind)
	}
	if len(host.acts) != 0 {
		t.Fatal("the action ran before it was confirmed")
	}
	if !strings.Contains(strip(m.View()), "This changes data in Fixture.") {
		t.Fatalf("the confirm does not say what it changes:\n%s", strip(m.View()))
	}
	run(t, m, m.applyFormAction(actionRunID))
	if len(host.acts) != 1 {
		t.Fatalf("acts = %d, want 1 after confirming", len(host.acts))
	}
}

// A plugin-suggested key is a request, never a grant.
func TestPluginKeyIsGrantedOnlyWhenFree(t *testing.T) {
	host := &fakeHost{page: testPage(1)}
	m := newTestModel(t, host)
	if _, ok := m.GrantedKey("R"); !ok {
		t.Fatal("a free letter was not granted")
	}
	// The browser's own keys and the surface's bindings both refuse.
	desc := testDescription()
	desc.Actions[0].Key = "r"
	host.desc = desc
	m2 := New("fixture", "fixture", host.calls(), nil)
	m2.SetReservedKeys(map[string]bool{"1": true})
	run(t, m2, m2.Refresh())
	if _, ok := m2.GrantedKey("r"); ok {
		t.Fatal("the browser's own refresh key was handed to a plugin")
	}
	desc.Actions[0].Key = "1"
	host.desc = desc
	m3 := New("fixture", "fixture", host.calls(), nil)
	m3.SetReservedKeys(map[string]bool{"1": true})
	run(t, m3, m3.Refresh())
	if _, ok := m3.GrantedKey("1"); ok {
		t.Fatal("a key the surface already binds was handed to a plugin")
	}
}

// A degraded page renders its notices and never claims coverage it does not
// have; an abstained one says the opposite, honestly.
func TestOutcomesRenderHonestly(t *testing.T) {
	host := &fakeHost{}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")

	host.page = pluginhost.Page{
		Outcome: pluginhost.OutcomeDegraded,
		Items:   testPage(1).Items,
		Total:   1,
		Notices: []pluginhost.Notice{
			{Tone: resource.ToneWarning, Text: "1 of 4 sources did not answer (mail: checkpoint stale)"},
		},
	}
	run(t, m, m.list(m.desc.Collections[0], s, false))
	view := strip(m.View())
	if !strings.Contains(view, "degraded") || !strings.Contains(view, "1 of 4 sources did not answer") {
		t.Fatalf("degraded page did not render its notice:\n%s", view)
	}

	host.page = pluginhost.Page{Outcome: pluginhost.OutcomeAbstained}
	run(t, m, m.list(m.desc.Collections[0], s, false))
	view = strip(m.View())
	if !strings.Contains(view, "No matches.") || !strings.Contains(view, "fact about the query") {
		t.Fatalf("abstained page did not read as an answer:\n%s", view)
	}

	host.page = pluginhost.Page{Outcome: pluginhost.OutcomeDegraded}
	run(t, m, m.list(m.desc.Collections[0], s, false))
	view = strip(m.View())
	if !strings.Contains(view, "coverage was incomplete") {
		t.Fatalf("an empty degraded page claimed to be an answer:\n%s", view)
	}
}

// o opens only a destination that passed the host's own validation.
func TestSourceURLGoesThroughTheConfirmedPath(t *testing.T) {
	host := &fakeHost{page: testPage(1)}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	press(t, m, "o")
	if len(host.opened) != 1 || host.opened[0] != "https://fixture.example.test/1" {
		t.Fatalf("opened = %v", host.opened)
	}

	bad := testPage(1)
	bad.Items[0].SourceURL = "javascript:alert(1)"
	host.page = bad
	run(t, m, m.list(m.desc.Collections[0], s, false))
	press(t, m, "o")
	if len(host.opened) != 1 {
		t.Fatalf("an unvalidated destination was opened: %v", host.opened)
	}
}

// The browser is held to exactly the box it is given, at every width the tab
// can be. A content that hands back more rows than its box would push the app
// header off screen.
func TestViewIsHeldToItsBox(t *testing.T) {
	host := &fakeHost{page: testPage(40)}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	for _, size := range [][2]int{{160, 45}, {120, 30}, {100, 24}, {80, 20}, {52, 18}, {40, 12}} {
		m.SetSize(size[0], size[1])
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d: rendered %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w != size[0] {
				t.Fatalf("%dx%d: line %d is %d cells wide", size[0], size[1], i, w)
			}
		}
	}
}

// Below the table floor a row takes two lines: the primary cell on the first,
// the short columns and the secondary text folded into a dimmed second.
func TestNarrowRowsReflow(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	m.SetSize(52, 24)
	view := strip(m.View())
	if strings.Contains(view, "TITLE") {
		t.Fatalf("a narrow pane still drew column headings:\n%s", view)
	}
	if !strings.Contains(view, "Row 1") {
		t.Fatalf("the primary cell is missing:\n%s", view)
	}
	if !strings.Contains(view, "notes · exact ·") {
		t.Fatalf("the second line did not fold the short columns in:\n%s", view)
	}
}

// A collection that narrows itself to project context is hidden where there is
// none: a tab that can only ever be empty claims a capability the surface does
// not have.
func TestProjectNarrowedCollectionHiddenWithoutProject(t *testing.T) {
	host := &fakeHost{}
	host.described = true
	host.status = pluginhost.Status{State: pluginhost.StateReady}
	desc := testDescription()
	desc.Collections[0].Context = []pluginhost.ContextKind{pluginhost.ContextProject}
	host.desc = desc
	host.project = nil

	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(120, 30)
	run(t, m, m.Refresh())
	cols := m.Collections()
	if len(cols) != 1 || cols[0].ID != "sources" {
		t.Fatalf("collections = %+v, want only sources", cols)
	}
}

// The footer's own line is the host's; the browser renders none of its own and
// reports its standing condition instead.
func TestFooterStatusReportsTheStandingCondition(t *testing.T) {
	host := &fakeHost{page: testPage(1)}
	m := newTestModel(t, host)
	p := &TabPlugin{id: "fixture", name: "fixture", model: m, focused: true}
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	status, isErr := p.FooterStatus()
	if status != "fixture · results · answered" || isErr {
		t.Fatalf("footer status = %q err=%v", status, isErr)
	}
	for _, command := range p.Commands() {
		if command.Context != p.FocusContext() {
			t.Fatalf("command %q is in context %q", command.ID, command.Context)
		}
	}
	if strings.Contains(strip(m.View()), "Move   Enter") {
		t.Fatal("the browser rendered a footer of its own")
	}
}

// Every row keeps every column, selected or not.
//
// This is a regression: ui.TruncateString measures runes rather than display
// cells behind escape sequences, so a styled row handed to it came back cut in
// the middle of its data — and only for the rows that were not selected, since
// the selected one is padded by ui.RowBackground instead. The frame stayed the
// right width the whole time, which is exactly why a width assertion did not
// catch it.
func TestUnselectedRowsKeepEveryColumn(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	view := strip(m.View())
	for _, row := range []string{"Row 1", "Row 2", "Row 3"} {
		var line string
		for _, candidate := range strings.Split(view, "\n") {
			if strings.Contains(candidate, row) {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Fatalf("%s is not on screen:\n%s", row, view)
		}
		if !strings.Contains(line, "notes") || !strings.Contains(line, "excerpt") {
			t.Fatalf("%s lost a column: %q", row, line)
		}
		if !strings.Contains(line, "exact") {
			t.Fatalf("%s lost its status column: %q", row, line)
		}
		if strings.Contains(line, "...") {
			t.Fatalf("%s was truncated by a rune-counting measure: %q", row, line)
		}
	}
	// The column headings are the same story one row up.
	for _, candidate := range strings.Split(view, "\n") {
		if strings.Contains(candidate, "TITLE") {
			if !strings.Contains(candidate, "EXCERPT") || strings.Contains(candidate, "...") {
				t.Fatalf("the heading row was truncated: %q", candidate)
			}
		}
	}
}

// Two browsers of one plugin are two independent readers. A global tab and a
// pane showing the same collection each keep their own query and their own
// generation, and a page fetched for one must not land in the other — which is
// exactly what happened while the answer named only the instance, the
// collection and a generation both browsers happened to be on.
func TestAPageDoesNotLandInAnotherBrowser(t *testing.T) {
	tabHost := &fakeHost{desc: testDescription(), described: true,
		status: pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}}
	tabHost.page = pluginhost.Page{Outcome: pluginhost.OutcomeAnswered, Items: []pluginhost.Item{
		{ID: "tab-row", Cells: map[string]string{"title": "the tab's page"}},
	}}
	tab := New("fixture", "fixture", tabHost.calls(), nil)
	tab.SetPaneCollection("sources")
	tab.SetSize(160, 45)
	run(t, tab, tab.Refresh())

	paneHost := &fakeHost{desc: testDescription(), described: true,
		status: pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}}
	paneHost.page = pluginhost.Page{Outcome: pluginhost.OutcomeAnswered, Items: []pluginhost.Item{
		{ID: "pane-row", Cells: map[string]string{"title": "the pane's page"}},
	}}
	pane := New("fixture", "fixture", paneHost.calls(), nil)
	pane.SetPaneCollection("sources")
	pane.SetSize(80, 30)
	run(t, pane, pane.Refresh())

	if tab.id == pane.id {
		t.Fatal("two browsers were handed the same identity")
	}
	tabState := tab.states["sources"]
	paneState := pane.states["sources"]
	if tabState == nil || paneState == nil {
		t.Fatalf("a browser never listed: tab=%v pane=%v", tabState != nil, paneState != nil)
	}
	if len(tabState.items) != 1 || tabState.items[0].ID != "tab-row" {
		t.Fatalf("the tab shows %+v, want its own page", tabState.items)
	}
	if len(paneState.items) != 1 || paneState.items[0].ID != "pane-row" {
		t.Fatalf("the pane shows %+v, want its own page", paneState.items)
	}

	// And a page addressed to one is refused by the other outright.
	pane.Update(ListedMsg{
		Instance: "fixture", Browser: tab.id, Collection: "sources",
		Generation: paneState.generation,
		Page:       pluginhost.Page{Outcome: pluginhost.OutcomeAnswered},
	})
	if len(paneState.items) != 1 || paneState.items[0].ID != "pane-row" {
		t.Fatalf("another browser's page landed in this one: %+v", paneState.items)
	}

	// Their list calls are superseded independently, so a pane re-querying does
	// not kill the tab's in-flight process group.
	if tabHost.lists[0].PaneKey == paneHost.lists[0].PaneKey {
		t.Fatalf("two browsers share the cancellation key %q", tabHost.lists[0].PaneKey)
	}
}

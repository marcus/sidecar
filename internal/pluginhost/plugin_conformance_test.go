package pluginhost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/resource"
)

// The plugin protocol's conformance suite. Every case here drives the real
// fixture executable through the real host, because the properties being
// checked — process groups, timeouts, stdout discipline, cancellation — are
// exactly the ones an in-memory fake cannot have.

// newFixturePlugin builds a plugin-protocol CommandProvider over the fixture
// executable. home is a temp directory so watch-path validation is bounded to
// somewhere the test owns rather than the developer's real home.
func newFixturePlugin(t *testing.T, instance string, args ...string) (*CommandProvider, string) {
	t.Helper()
	home := t.TempDir()
	p, err := NewCommandProvider(CommandConfig{
		Instance: instance,
		Argv:     append([]string{fixtureBin}, args...),
		Dir:      t.TempDir(),
		HostEnv:  os.Environ(),
		Host:     HostInfo{Name: "sidecar", Version: "test"},
		Protocol: Protocol,
		Home:     home,
	})
	if err != nil {
		t.Fatalf("NewCommandProvider: %v", err)
	}
	return p, home
}

// newFixtureManager wires one fixture plugin into a real Manager and describes
// it, which is what every list, get, and act needs before it can run.
func newFixtureManager(t *testing.T, args ...string) (*Manager, *CommandProvider) {
	t.Helper()
	provider, _ := newFixturePlugin(t, "fixture", args...)
	m := NewManager(ManagerOptions{})
	m.SetProviders([]Provider{provider}, nil)
	m.DescribeAll(context.Background())
	return m, provider
}

func TestFixturePluginDescribesTheWholeVocabulary(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture")
	desc, err := provider.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}

	if desc.Info.Kind != "fixture" || desc.Info.Name != "Fixture" {
		t.Fatalf("identity = %+v; the plugin key must be read under its own spelling", desc.Info)
	}
	if len(desc.Matchers) != 2 {
		t.Fatalf("matchers = %d, want 2; a plugin is still allowed to declare matchers", len(desc.Matchers))
	}
	// "a-kind-from-the-future" is dropped rather than refused.
	if len(desc.Context) != 1 || desc.Context[0] != ContextProject {
		t.Fatalf("context = %v, want [project] with the unknown kind dropped", desc.Context)
	}
	if len(desc.Collections) != 2 {
		t.Fatalf("collections = %d, want 2", len(desc.Collections))
	}

	results, ok := desc.Collection("results")
	if !ok {
		t.Fatal("the results collection is missing")
	}
	if results.Search != SearchRequired {
		t.Fatalf("results search = %q, want required", results.Search)
	}
	primary, ok := results.PrimaryColumn()
	if !ok || primary.ID != "title" {
		t.Fatalf("primary column = %+v, want title", primary)
	}
	if results.Columns[0].Align != AlignRight || results.Columns[0].Kind != ColumnNumber {
		t.Fatalf("rank column = %+v", results.Columns[0])
	}
	if !results.Columns[3].Secondary {
		t.Fatalf("excerpt column = %+v, want secondary", results.Columns[3])
	}
	if len(results.Sort) != 2 || results.Sort[0].Default != SortDesc || results.Sort[1].Default != "" {
		t.Fatalf("sort keys = %+v, want one default", results.Sort)
	}
	if len(results.Filters) != 3 {
		t.Fatalf("filters = %+v, want three", results.Filters)
	}
	scope, ok := results.ScopeFilter()
	if !ok || scope.ID != "scope" || scope.Default != "everything" {
		t.Fatalf("scope filter = %+v; the first declared filter is the scope", scope)
	}
	if title, _ := scope.OptionTitle("project"); title != "This project" {
		t.Fatalf("scope option title = %q", title)
	}
	source := results.Filters[1]
	if source.Kind != FilterChoice || source.Default != "any" {
		t.Fatalf("source filter = %+v; a choice with no stated default opens on its first option", source)
	}
	since := results.Filters[2]
	if since.Kind != FilterText || len(since.Choices) != 0 || since.Default != "" {
		t.Fatalf("since filter = %+v, want an empty text filter", since)
	}
	if !results.Detail {
		t.Fatal("results declared detail:true and lost it")
	}

	sources, _ := desc.Collection("sources")
	// The fixture asks for five seconds; the floor is fifteen.
	if sources.Refresh.EverySeconds != MinRefreshSeconds {
		t.Fatalf("sources poll = %ds, want it clamped to %d", sources.Refresh.EverySeconds, MinRefreshSeconds)
	}
	if len(sources.Views) != 2 {
		t.Fatalf("views = %+v, want 2", sources.Views)
	}

	if len(desc.Actions) != 4 {
		t.Fatalf("actions = %d, want 4", len(desc.Actions))
	}
	refreshSource, _ := desc.Action("refresh-source")
	if !refreshSource.Confirm {
		t.Fatal("a mutating action with no inputs must confirm")
	}
	if refreshSource.Key != "" {
		t.Fatalf("key = %q; an uppercase request is not a grantable key", refreshSource.Key)
	}
	logNote, _ := desc.Action("log-note")
	if logNote.Confirm {
		t.Fatal("an action with a form must not also confirm; the form is the confirm step")
	}
	if len(logNote.Inputs) != 1 || logNote.Inputs[0].Kind != InputMultiline || !logNote.Inputs[0].Required {
		t.Fatalf("log-note inputs = %+v", logNote.Inputs)
	}
	capture, _ := desc.Action("capture")
	if len(capture.Inputs) != 1 || len(capture.Inputs[0].Choices) != 2 {
		t.Fatalf("capture inputs = %+v", capture.Inputs)
	}
	transition, _ := desc.Action("transition")
	if transition.On != ActionOnResource || transition.Collection != "" {
		t.Fatalf("transition = %+v; a resource action names no collection", transition)
	}
	if transition.Key != "t" {
		t.Fatalf("transition key = %q, want the requested letter to survive", transition.Key)
	}
}

func TestFixturePluginListsGetsAndActs(t *testing.T) {
	m, _ := newFixtureManager(t)
	ctx := context.Background()

	page, err := m.List(ctx, ListRequest{Instance: "fixture", Params: ListParams{Collection: "results", Query: "dex"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Outcome != OutcomeAnswered || len(page.Items) != 3 {
		t.Fatalf("page = %+v", page)
	}
	if page.Items[0].Cells["title"] != "dex schema notes" {
		t.Fatalf("first row = %+v", page.Items[0])
	}
	if page.NextCursor != "page-2" {
		t.Fatalf("nextCursor = %q", page.NextCursor)
	}

	doc, err := m.Get(ctx, GetRequest{Instance: "fixture", Params: GetParams{Collection: "results", ID: page.Items[0].ID}})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc.Identity != page.Items[0].ID {
		t.Fatalf("identity = %q, want %q", doc.Identity, page.Items[0].ID)
	}
	if len(doc.Sections) != 4 {
		t.Fatalf("sections = %d, want 4 (the ambiguous one keeps its body)", len(doc.Sections))
	}
	if doc.Sections[0].Body == nil || doc.Sections[1].Fields == nil || len(doc.Sections[2].Items) != 2 {
		t.Fatalf("sections = %+v", doc.Sections)
	}
	ambiguous := doc.Sections[3]
	if ambiguous.Body == nil || len(ambiguous.Items) != 0 {
		t.Fatalf("a section that claims two shapes must keep exactly one: %+v", ambiguous)
	}

	outcome, err := m.Act(ctx, ActRequest{Instance: "fixture", Params: ActParams{
		Action: "log-note", Collection: "results", ID: page.Items[0].ID,
		Inputs: map[string]string{"text": "a note"},
	}})
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	if outcome.Status != ActDone {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(outcome.Refresh) != 1 || outcome.Refresh[0] != "results" {
		t.Fatalf("refresh = %v", outcome.Refresh)
	}
	if outcome.Open == nil || outcome.Open.ID != page.Items[0].ID {
		t.Fatalf("open = %+v", outcome.Open)
	}
}

func TestFixturePluginPageOutcomes(t *testing.T) {
	m, _ := newFixtureManager(t)
	tests := []struct {
		query string
		want  PageOutcome
		items int
	}{
		{"nothing", OutcomeAbstained, 0},
		{"degraded", OutcomeDegraded, 1},
		// Every asked source failed: the page says so rather than reporting an
		// empty list as "no matches".
		{"failed", OutcomeFailed, 0},
		// An outcome from a later protocol version must not be read as a
		// completeness guarantee.
		{"future", OutcomeDegraded, 0},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "results", Query: tc.query}})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if page.Outcome != tc.want || len(page.Items) != tc.items {
				t.Fatalf("page = %+v, want outcome %q with %d items", page, tc.want, tc.items)
			}
		})
	}
}

// A required search with an empty query is answered by the host. The fixture
// says so in a notice if it is ever reached, which is how this test can tell
// the difference between "not spawned" and "spawned and abstained".
func TestEmptyRequiredQueryStartsNoProcess(t *testing.T) {
	m, _ := newFixtureManager(t)
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "results"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Outcome != OutcomeAbstained {
		t.Fatalf("outcome = %q, want abstained", page.Outcome)
	}
	if len(page.Notices) != 0 {
		t.Fatalf("the plugin was started for an empty required query: %+v", page.Notices)
	}
}

// A second list for the same pane supersedes the first: the earlier process
// group is killed rather than left running to the end of its timeout.
func TestSupersededListForAPaneIsKilled(t *testing.T) {
	m, _ := newFixtureManager(t)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := m.List(context.Background(), ListRequest{
			Instance: "fixture",
			PaneKey:  "workspace/pane-1",
			// The fixture sleeps ten minutes; only a kill ends it.
			Params: ListParams{Collection: "results", Query: "mode:hang:"},
		})
		done <- err
	}()
	<-started

	// Give the first call time to actually spawn before superseding it.
	deadline := time.After(10 * time.Second)
	for {
		if _, err := m.List(context.Background(), ListRequest{
			Instance: "fixture",
			PaneKey:  "workspace/pane-1",
			Params:   ListParams{Collection: "results", Query: "dex"},
		}); err != nil {
			t.Fatalf("the superseding list failed: %v", err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("the superseded list returned a page instead of being cancelled")
			}
			if OutcomeCode(err) != string(ReasonCanceled) && OutcomeCode(err) != string(ReasonTimeout) {
				t.Fatalf("superseded list ended with %q, want the process group to have been killed", OutcomeCode(err))
			}
			return
		case <-deadline:
			t.Fatal("the superseded list was still running 10s after being superseded")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Cancelling a pane outright must also kill whatever it had running.
func TestCancelPaneKillsAnInFlightList(t *testing.T) {
	m, _ := newFixtureManager(t)
	done := make(chan error, 1)
	go func() {
		_, err := m.List(context.Background(), ListRequest{
			Instance: "fixture",
			PaneKey:  "overview/pane-2",
			Params:   ListParams{Collection: "results", Query: "mode:hang:"},
		})
		done <- err
	}()

	deadline := time.After(10 * time.Second)
	for {
		m.CancelPane("overview/pane-2")
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("CancelPane left the list running to completion")
			}
			return
		case <-deadline:
			t.Fatal("CancelPane did not end the in-flight list")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Every hostile case must produce a bounded, typed answer rather than a hang, a
// panic, or an unbounded allocation.
func TestPluginHostileCasesAreBounded(t *testing.T) {
	tests := []struct {
		name string
		mode string
		// wantDescribeErr is true when the case is refused at describe time.
		wantDescribeErr bool
		detail          string
	}{
		{name: "13 columns", mode: "wide-collection", wantDescribeErr: true, detail: "13 columns"},
		{name: "too many collections", mode: "too-many-collections", wantDescribeErr: true, detail: "20 collections"},
		{name: "watch path outside home", mode: "watch-escape", wantDescribeErr: true, detail: "not inside the home directory"},
		{name: "watch path is the home directory", mode: "watch-home", wantDescribeErr: true, detail: "not inside the home directory"},
		{name: "relative watch path", mode: "watch-relative", wantDescribeErr: true, detail: "not absolute"},
		{name: "too many watch paths", mode: "too-many-watch", wantDescribeErr: true, detail: "watch paths"},
		{name: "unknown action target", mode: "unknown-action-target", wantDescribeErr: true, detail: "not one of item"},
		{name: "action names an undeclared collection", mode: "action-unknown-collection", wantDescribeErr: true, detail: "not declared"},
		{name: "choice input with no choices", mode: "choice-without-choices", wantDescribeErr: true, detail: "no choices"},
		{name: "answers the resource protocol only", mode: "resource-only", wantDescribeErr: true, detail: ""},
		{name: "answers the pre-freeze draft identifier", mode: "draft-answer", wantDescribeErr: true, detail: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider, _ := newFixturePlugin(t, "fixture", "-mode="+tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := provider.Describe(ctx)
			if tc.wantDescribeErr && err == nil {
				t.Fatalf("Describe accepted %s", tc.mode)
			}
			if err != nil && tc.detail != "" && !strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("Describe error = %q, want it to name %q", err, tc.detail)
			}
		})
	}
}

// A plugin that only answers the frozen resource identifier is a protocol
// failure when it was asked on the plugin identifier — never a silent
// downgrade to a provider.
func TestResourceOnlyPluginIsAProtocolFailure(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture", "-mode=resource-only")
	_, err := provider.Describe(context.Background())
	if err == nil {
		t.Fatal("Describe accepted a response on the wrong protocol")
	}
	if OutcomeCode(err) != string(ReasonProtocol) {
		t.Fatalf("outcome = %q, want %q", OutcomeCode(err), ReasonProtocol)
	}
	// The same executable, configured as a resource provider, still works.
	legacy := newFixtureProvider(t, "fixture")
	desc, err := legacy.Describe(context.Background())
	if err != nil {
		t.Fatalf("the same binary failed as a resource provider: %v", err)
	}
	if len(desc.Matchers) != 2 || len(desc.Collections) != 0 {
		t.Fatalf("resource-provider describe = %+v; it must carry matchers and no collections", desc)
	}
}

// The identifier is frozen and the host validates it strictly: a plugin that
// still answers the pre-freeze draft identifier is a protocol failure, not a
// tolerated older dialect.
//
// This is the other half of the freeze rule. Tolerance lives on the plugin
// side — a plugin that accepts either identifier on a request and answers with
// whichever it was asked keeps working with an older Sidecar and with this
// one — and the host stays strict so the reference's rule is literally true.
func TestDraftIdentifierAnswerIsAProtocolFailure(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture", "-mode=draft-answer")
	_, err := provider.Describe(context.Background())
	if err == nil {
		t.Fatal("Describe accepted an answer on the pre-freeze draft identifier")
	}
	if OutcomeCode(err) != string(ReasonProtocol) {
		t.Fatalf("outcome = %q, want %q", OutcomeCode(err), ReasonProtocol)
	}
	if !strings.Contains(err.Error(), Protocol) {
		t.Fatalf("error = %q, want it to name the identifier the host asked on (%s)", err, Protocol)
	}
}

// An act that never returns is bounded by the per-call timeout and its process
// group is killed, exactly as a hanging resolve is.
func TestActThatNeverReturnsIsBounded(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture", "-mode=act-hang")
	m := NewManager(ManagerOptions{})
	m.SetProviders([]Provider{provider}, nil)
	m.DescribeAll(context.Background())

	short, err := NewCommandProvider(CommandConfig{
		Instance:       "fixture",
		Argv:           append([]string{fixtureBin}, "-mode=act-hang"),
		Dir:            t.TempDir(),
		HostEnv:        os.Environ(),
		Protocol:       Protocol,
		Home:           t.TempDir(),
		ResolveTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewCommandProvider: %v", err)
	}
	m2 := NewManager(ManagerOptions{})
	m2.SetProviders([]Provider{short}, nil)
	m2.DescribeAll(context.Background())

	started := time.Now()
	_, actErr := m2.Act(context.Background(), ActRequest{Instance: "fixture", Params: ActParams{
		Action: "refresh-source", Collection: "sources", ID: "notes",
	}})
	if actErr == nil {
		t.Fatal("a hanging act returned an outcome")
	}
	if OutcomeCode(actErr) != string(ReasonTimeout) {
		t.Fatalf("outcome = %q, want timeout", OutcomeCode(actErr))
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("the hanging act took %s to be bounded", elapsed)
	}
}

// A cursor that points at itself is the plugin's business. The host's contract
// is that it never pages on its own, so one list is one process and the loop
// cannot start.
func TestLoopingCursorDoesNotPageOnItsOwn(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=cursor-loop")
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "results", Query: "dex"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.NextCursor != "loop" || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	again, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{
		Collection: "results", Query: "dex", Cursor: page.NextCursor,
	}})
	if err != nil {
		t.Fatalf("second List: %v", err)
	}
	if again.NextCursor != "loop" {
		t.Fatalf("the fixture stopped looping, so this case proves nothing: %+v", again)
	}
}

func TestHostilePageIsSanitized(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=hostile-page")
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "results", Query: "dex"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want the row with no id dropped", len(page.Items))
	}
	row := page.Items[0]
	if strings.ContainsRune(row.ID, '\x1b') || strings.Contains(row.ID, "]8;;") {
		t.Fatalf("row id kept an escape sequence: %q", row.ID)
	}
	for column, value := range row.Cells {
		if strings.ContainsRune(value, '\x1b') {
			t.Fatalf("cell %q kept an escape sequence: %q", column, value)
		}
		if len([]rune(value)) > MaxCellChars {
			t.Fatalf("cell %q is %d runes, the limit is %d", column, len([]rune(value)), MaxCellChars)
		}
	}
	if row.SourceURL != "" {
		t.Fatalf("a javascript: URL survived as %q", row.SourceURL)
	}
	if row.Status == nil || row.Status.Tone != resource.ToneNeutral {
		t.Fatalf("status = %+v, want an unknown tone coerced to neutral", row.Status)
	}
	if len(page.Notices) != MaxNotices {
		t.Fatalf("notices = %d, want %d", len(page.Notices), MaxNotices)
	}
	for _, notice := range page.Notices {
		if len([]rune(notice.Text)) > MaxNoticeChars {
			t.Fatalf("notice is %d runes, the limit is %d", len([]rune(notice.Text)), MaxNoticeChars)
		}
	}
}

func TestOverLimitPageIsTruncatedNotRefused(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=over-limit-page")
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "results", Query: "dex"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != MaxPageItems {
		t.Fatalf("items = %d, want %d", len(page.Items), MaxPageItems)
	}
	if !page.Truncated {
		t.Fatal("a truncated page must say so")
	}
	if len([]rune(page.Items[0].Cells["title"])) > MaxCellChars {
		t.Fatal("an over-long cell was not cut")
	}
}

// A cell keyed by a column the plugin never declared has nowhere to be painted,
// so it does not survive into the page.
func TestUndeclaredCellsAreDropped(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=undeclared-cells")
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "results", Query: "dex"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %+v", page.Items)
	}
	if _, ok := page.Items[0].Cells["smuggled"]; ok {
		t.Fatalf("an undeclared cell survived: %+v", page.Items[0].Cells)
	}
	if page.Items[0].Cells["title"] != "declared" {
		t.Fatalf("the declared cell was lost: %+v", page.Items[0].Cells)
	}
}

func TestHostileSectionsAreSanitized(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=hostile-document")
	doc, err := m.Get(context.Background(), GetRequest{Instance: "fixture", Params: GetParams{Collection: "results", ID: "row"}})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(doc.Sections) != resource.MaxSections {
		t.Fatalf("sections = %d, want %d", len(doc.Sections), resource.MaxSections)
	}
	for _, section := range doc.Sections {
		if strings.ContainsRune(section.Title, '\x1b') {
			t.Fatalf("section title kept an escape sequence: %q", section.Title)
		}
		if len([]rune(section.Title)) > resource.MaxSectionTitleChars {
			t.Fatalf("section title is %d runes, the limit is %d", len([]rune(section.Title)), resource.MaxSectionTitleChars)
		}
		if len(section.Items) > resource.MaxTimelineItems {
			t.Fatalf("timeline is %d items, the limit is %d", len(section.Items), resource.MaxTimelineItems)
		}
		for _, item := range section.Items {
			if strings.ContainsRune(item.Title, '\x1b') {
				t.Fatalf("timeline title kept an escape sequence: %q", item.Title)
			}
			if !item.When.IsZero() {
				t.Fatalf("an unparseable timestamp became %s", item.When)
			}
		}
	}
}

// A get whose response is a page is a shape failure, not a blank card.
func TestGetThatReturnsAPageIsAShapeFailure(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=page-shaped-get")
	_, err := m.Get(context.Background(), GetRequest{Instance: "fixture", Params: GetParams{Collection: "results", ID: "row"}})
	if err == nil {
		t.Fatal("Get accepted a page")
	}
	if OutcomeCode(err) != string(ReasonInvalidResource) {
		t.Fatalf("outcome = %q", OutcomeCode(err))
	}
}

// A typed act failure is the plugin's answer, not a transport failure: it
// carries a message the user can read and the host reports it as an outcome.
func TestTypedActFailureIsAnOutcome(t *testing.T) {
	m, _ := newFixtureManager(t, "-mode=act-failed")
	outcome, err := m.Act(context.Background(), ActRequest{Instance: "fixture", Params: ActParams{
		Action: "refresh-source", Collection: "sources", ID: "notes",
	}})
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	if outcome.Status != ActFailed || outcome.Message == "" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// An instance configured as a terminal resource provider has no list, get, or
// act, and is refused before anything is spawned.
func TestResourceProviderRefusesPluginMethods(t *testing.T) {
	provider := newFixtureProvider(t, "legacy")
	m := NewManager(ManagerOptions{})
	m.SetProviders([]Provider{provider}, nil)
	m.DescribeAll(context.Background())

	if _, err := m.List(context.Background(), ListRequest{Instance: "legacy", Params: ListParams{Collection: "results"}}); err == nil {
		t.Fatal("List was accepted on a resource provider")
	} else if code := resource.CoerceCode(string(AsResourceError(err).Code)); code != resource.CodeInvalidRequest {
		t.Fatalf("List error code = %q, want invalid_request", code)
	}
	if _, err := m.Get(context.Background(), GetRequest{Instance: "legacy", Params: GetParams{Collection: "c", ID: "x"}}); err == nil {
		t.Fatal("Get was accepted on a resource provider")
	}
	if _, err := m.Act(context.Background(), ActRequest{Instance: "legacy", Params: ActParams{Action: "x"}}); err == nil {
		t.Fatal("Act was accepted on a resource provider")
	}
}

// A collection the plugin never declared is refused without a process.
func TestUndeclaredCollectionIsRefused(t *testing.T) {
	m, _ := newFixtureManager(t)
	if _, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{Collection: "nowhere", Query: "x"}}); err == nil {
		t.Fatal("List accepted an undeclared collection")
	}
	if _, err := m.Act(context.Background(), ActRequest{Instance: "fixture", Params: ActParams{Action: "nowhere"}}); err == nil {
		t.Fatal("Act accepted an undeclared action")
	}
}

// The context permission model is enforced at the process boundary: the plugin
// declared only "project", so a selection offered by the caller never reaches
// it.
func TestOnlyDeclaredContextReachesThePlugin(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture")
	if _, err := provider.Describe(context.Background()); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	doc, err := provider.Get(context.Background(), GetParams{Collection: "results", ID: "mode:request-echo:row"}, &Context{
		Project:   &ProjectContext{Root: "/checkout", Name: "sidecar", Branch: "main", HostID: "workstation"},
		Selection: &SelectionContext{Text: "secret selection"},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	sent := doc.Body.Text
	if !strings.Contains(sent, `"hostId":"workstation"`) {
		t.Fatalf("declared project context did not reach the plugin:\n%s", sent)
	}
	if strings.Contains(sent, "secret selection") {
		t.Fatalf("undeclared selection context was sent:\n%s", sent)
	}
}

// A plugin that has never described successfully has declared nothing, so it
// receives nothing.
func TestUndescribedPluginReceivesNoContext(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture")
	doc, err := provider.Get(context.Background(), GetParams{Collection: "results", ID: "mode:request-echo:row"}, &Context{
		Project: &ProjectContext{Root: "/checkout"},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(doc.Body.Text, `"context"`) {
		t.Fatalf("context reached a plugin that has declared nothing:\n%s", doc.Body.Text)
	}
}

// The envelope the host writes is the published one, asserted from the child's
// side rather than from the host's own encoder.
func TestPluginRequestEnvelopeOnTheWire(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture")
	doc, err := provider.List(context.Background(),
		ListParams{Collection: "results", Query: "mode:request-echo:dex"},
		nil,
		Collection{ID: "results", Columns: []Column{{ID: "title", Primary: true}}},
	)
	// request-echo answers with a resource, not a page, so List reports a shape
	// failure. The echo is still what the child saw, so read it through Get,
	// which does accept a resource.
	_ = doc
	if err == nil {
		t.Fatal("a resource answer to list should be a shape failure")
	}

	echoed, err := provider.Get(context.Background(), GetParams{Collection: "results", ID: "mode:request-echo:row"}, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(echoed.Body.Text), &sent); err != nil {
		t.Fatalf("the echoed request is not JSON: %v\n%s", err, echoed.Body.Text)
	}
	if sent["protocol"] != Protocol {
		t.Fatalf("protocol = %v, want %q", sent["protocol"], Protocol)
	}
	if sent["method"] != MethodGet || sent["instance"] != "fixture" {
		t.Fatalf("envelope = %+v", sent)
	}
	if sent["deadlineMs"] != float64(resource.DefaultResolveTimeout.Milliseconds()) {
		t.Fatalf("deadlineMs = %v", sent["deadlineMs"])
	}
	params, _ := sent["params"].(map[string]any)
	if params["collection"] != "results" {
		t.Fatalf("params = %+v", params)
	}
}

// The published request and response fixtures are the contract. A plugin author
// vendors these files, so the host must encode exactly them and accept exactly
// them.
func TestPluginProtocolGoldens(t *testing.T) {
	t.Run("describe request", func(t *testing.T) {
		req := Request{
			Protocol:   Protocol,
			Method:     MethodDescribe,
			Instance:   "recall",
			DeadlineMs: resource.DescribeTimeout.Milliseconds(),
			Host:       &HostInfo{Name: "sidecar", Version: "0.0.0"},
		}
		assertMatchesGolden(t, "plugin-describe-request.json", req)
	})

	t.Run("list request", func(t *testing.T) {
		req := Request{
			Protocol:   Protocol,
			Method:     MethodList,
			Instance:   "recall",
			DeadlineMs: resource.DefaultResolveTimeout.Milliseconds(),
			Context: &Context{Project: &ProjectContext{
				Root: "/path/to/checkout", WorkDir: "/path/to/checkout", Name: "sidecar", Branch: "main",
			}},
			Params: &ListParams{
				Collection: "results", Query: "dex", Limit: 100,
				Filters: map[string]string{"profile": "docs", "since": "2026-08-01"},
			},
		}
		assertMatchesGolden(t, "plugin-list-request.json", req)
	})

	t.Run("get request", func(t *testing.T) {
		req := Request{
			Protocol:   Protocol,
			Method:     MethodGet,
			Instance:   "recall",
			DeadlineMs: resource.DefaultResolveTimeout.Milliseconds(),
			Params:     &GetParams{Collection: "results", ID: "rc:notes:2026-08-14-dex-design"},
		}
		assertMatchesGolden(t, "plugin-get-request.json", req)
	})

	t.Run("act request", func(t *testing.T) {
		req := Request{
			Protocol:   Protocol,
			Method:     MethodAct,
			Instance:   "dex",
			DeadlineMs: resource.DefaultResolveTimeout.Milliseconds(),
			Context: &Context{Project: &ProjectContext{
				Root: "/path/to/checkout", WorkDir: "/path/to/checkout", Name: "sidecar", Branch: "main",
			}},
			Params: &ActParams{
				Action: "log-note", Collection: "people", ID: "p:ada",
				Inputs: map[string]string{"text": "Met at the conference; follow up about the retrieval eval pack."},
			},
		}
		assertMatchesGolden(t, "plugin-act-request.json", req)
	})

	home := t.TempDir()

	t.Run("describe response", func(t *testing.T) {
		resp := decodePluginGolden(t, "plugin-describe-response.json")
		desc, err := ValidateDescribe("recall", resp, home)
		if err != nil {
			t.Fatalf("ValidateDescribe: %v", err)
		}
		if desc.Info.Kind != "recall" || len(desc.Matchers) != 1 {
			t.Fatalf("description = %+v", desc)
		}
		if !desc.ReadsContext(ContextProject) || desc.ReadsContext(ContextSelection) {
			t.Fatalf("context = %v", desc.Context)
		}
		results, ok := desc.Collection("results")
		if !ok || results.Search != SearchRequired || len(results.Columns) != 4 {
			t.Fatalf("results = %+v", results)
		}
		if len(results.Filters) != 3 {
			t.Fatalf("filters = %+v, want three", results.Filters)
		}
		scope, ok := results.ScopeFilter()
		if !ok || scope.ID != "profile" || scope.Default != "home" {
			t.Fatalf("scope = %+v; the first declared filter is the scope", scope)
		}
		if results.Filters[1].Default != "any" || results.Filters[2].Kind != FilterText {
			t.Fatalf("filters = %+v", results.Filters)
		}
		sources, _ := desc.Collection("sources")
		if sources.Refresh.EverySeconds != 120 {
			t.Fatalf("sources poll = %d", sources.Refresh.EverySeconds)
		}
		if len(desc.Actions) != 2 {
			t.Fatalf("actions = %+v", desc.Actions)
		}
	})

	t.Run("list response", func(t *testing.T) {
		resp := decodePluginGolden(t, "plugin-list-response.json")
		collection := Collection{ID: "results", Columns: []Column{
			{ID: "rank"}, {ID: "title", Primary: true}, {ID: "source"}, {ID: "excerpt", Secondary: true},
		}}
		page := SanitizePage(resp.Page, collection)
		if page.Outcome != OutcomeDegraded || len(page.Items) != 1 || page.Total != 7 {
			t.Fatalf("page = %+v", page)
		}
		if len(page.Notices) != 1 || page.Notices[0].Tone != resource.ToneWarning {
			t.Fatalf("notices = %+v", page.Notices)
		}
		if page.Items[0].Status.Tone != resource.ToneSuccess {
			t.Fatalf("status = %+v", page.Items[0].Status)
		}
		if page.Omitted != (Omitted{Suppressed: 2, Dropped: 6}) {
			t.Fatalf("omitted = %+v", page.Omitted)
		}
		if len(page.Coverage) != 4 || page.Coverage[3].State != CoverageUnhealthy {
			t.Fatalf("coverage = %+v", page.Coverage)
		}
	})

	t.Run("get response", func(t *testing.T) {
		resp := decodePluginGolden(t, "plugin-get-response.json")
		doc, structural := resource.SanitizeDocument(resp.Resource)
		if structural != nil {
			t.Fatalf("SanitizeDocument: %v", structural)
		}
		if len(doc.Sections) != 2 {
			t.Fatalf("sections = %+v", doc.Sections)
		}
		if doc.Sections[0].Body == nil || len(doc.Sections[1].Items) != 2 {
			t.Fatalf("sections = %+v", doc.Sections)
		}
		if doc.Sections[1].Items[0].When.IsZero() {
			t.Fatal("an RFC 3339 timeline timestamp was dropped")
		}
	})

	t.Run("act response", func(t *testing.T) {
		resp := decodePluginGolden(t, "plugin-act-response.json")
		outcome := SanitizeOutcome(resp.Outcome)
		if outcome.Status != ActDone || outcome.Open == nil || outcome.Open.ID != "p:ada" {
			t.Fatalf("outcome = %+v", outcome)
		}
	})

	t.Run("error response", func(t *testing.T) {
		resp := decodePluginGolden(t, "plugin-error-response.json")
		e := resource.SanitizeError(resp.Error)
		if e.Code != resource.CodeInvalidConfig || e.Retryable || e.SetupHint == "" {
			t.Fatalf("error = %+v", e)
		}
	})
}

func decodePluginGolden(t *testing.T, name string) *Response {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "protocol", name))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	resp, reason, detail := decodeResponse(raw, Protocol)
	if reason != "" {
		t.Fatalf("golden %s rejected: %s (%s)", name, reason, detail)
	}
	return resp
}

// The two config sections load into one list, and the section an entry came
// from decides its dialect. This is the seam that lets a Jira provider keep
// working while a recall plugin runs beside it.
func TestFromInstancesDispatchesEachSectionOnItsOwnProtocol(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.External = []config.PluginInstanceConfig{
		{ID: "recall", Command: []string{fixtureBin}, Enabled: true},
		{ID: "off", Command: []string{fixtureBin}, Enabled: false},
	}
	cfg.TerminalResources.Providers = []config.TerminalResourceProviderConfig{
		{ID: "jira", Command: []string{fixtureBin}, Enabled: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	providers, disabled, err := FromInstances(cfg.PluginInstances(), Options{Dir: t.TempDir(), HostEnv: os.Environ()})
	if err != nil {
		t.Fatalf("FromInstances: %v", err)
	}
	if len(providers) != 2 || len(disabled) != 1 || disabled[0] != "off" {
		t.Fatalf("providers = %d, disabled = %v", len(providers), disabled)
	}
	recall := providers[0].(*CommandProvider)
	jira := providers[1].(*CommandProvider)
	if recall.Protocol() != Protocol || !recall.SpeaksPluginProtocol() {
		t.Fatalf("plugins.external entry dispatched with %q", recall.Protocol())
	}
	if jira.Protocol() != resource.Protocol || jira.SpeaksPluginProtocol() {
		t.Fatalf("terminalResources entry dispatched with %q", jira.Protocol())
	}
}

// The one variable the host sets rather than inherits, so a tool whose ordinary
// CLI and whose plugin subcommand share a binary can tell them apart. It exists
// only on the plugin protocol: the frozen resource protocol publishes its child
// environment exactly.
func TestPluginMarkerIsSetOnlyForThePluginProtocol(t *testing.T) {
	provider, _ := newFixturePlugin(t, "fixture")
	doc, err := provider.Get(context.Background(), GetParams{Collection: "results", ID: "mode:env-report:row"}, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(doc.Body.Text, PluginMarker) {
		t.Fatalf("the plugin marker was not set:\n%s", doc.Body.Text)
	}

	legacy := newFixtureProvider(t, "legacy")
	legacyDoc, err := legacy.Resolve(context.Background(), resource.Reference{
		Instance: "legacy", Matcher: "issue-key", Locator: "mode:env-report:CASH-1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if strings.Contains(legacyDoc.Body.Text, "SIDECAR_PLUGIN") {
		t.Fatalf("the marker reached a frozen-protocol provider:\n%s", legacyDoc.Body.Text)
	}
}

// Only declared, non-default filters reach the plugin, proven from the outside:
// the fixture echoes the filters map it actually received.
func TestOnlyDeclaredNonDefaultFiltersReachThePlugin(t *testing.T) {
	m, _ := newFixtureManager(t)
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{
		Collection: "results",
		Query:      "filters",
		Filters: map[string]string{
			"scope":    "project",      // declared, not the default
			"source":   "any",          // declared, but IS the default
			"since":    "2026-08-01",   // declared text
			"smuggled": "not-a-filter", // never declared
			"source2":  "notes",        // never declared
		},
	}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	got := page.Items[0].Cells["title"]
	if got != "scope=project;since=2026-08-01" {
		t.Fatalf("the plugin received %q; the host must drop undeclared keys and default values", got)
	}
}

// Everything at its default is no filters at all: the field is omitted from the
// request rather than sent as an object full of defaults.
func TestFiltersAtTheirDefaultsAreNotSent(t *testing.T) {
	m, _ := newFixtureManager(t)
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{
		Collection: "results", Query: "filters",
		Filters: map[string]string{"scope": "everything", "source": "any"},
	}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := page.Items[0].Cells["title"]; got != "(none)" {
		t.Fatalf("the plugin received %q, want no filters at all", got)
	}
}

// A degraded page carries what a one-line notice cannot: the counts it held
// back, and one row per source.
func TestPageCarriesOmittedAndCoverage(t *testing.T) {
	m, _ := newFixtureManager(t)
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{
		Collection: "results", Query: "coverage",
	}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Outcome != OutcomeDegraded {
		t.Fatalf("outcome = %q", page.Outcome)
	}
	if page.Omitted != (Omitted{Suppressed: 1, Dropped: 6}) {
		t.Fatalf("omitted = %+v", page.Omitted)
	}
	if len(page.Coverage) != 13 || page.CoverageTruncated {
		t.Fatalf("coverage = %d rows (truncated=%v), want 13 kept whole", len(page.Coverage), page.CoverageTruncated)
	}
	if page.Coverage[0].Source != "notes" || page.Coverage[0].State != CoverageAnswered {
		t.Fatalf("first row = %+v", page.Coverage[0])
	}
	mail := page.Coverage[3]
	if mail.State != CoverageUnhealthy || mail.Reason == "" {
		t.Fatalf("mail row = %+v", mail)
	}
	if page.Coverage[4].State != CoverageTimeout || page.Coverage[4].ElapsedMs != 2000 {
		t.Fatalf("calendar row = %+v", page.Coverage[4])
	}
	if page.Coverage[5].State != CoverageSkipped || page.Coverage[6].State != CoverageFailed {
		t.Fatalf("states = %+v", page.Coverage[5:7])
	}
	// The host owns the tone, so a plugin cannot paint its own failure green.
	if page.Coverage[6].State.Tone() != resource.ToneDanger {
		t.Fatalf("failed tone = %q", page.Coverage[6].State.Tone())
	}
}

// A failed page is a claim about the row set, and an empty list under it never
// reads as "no matches".
func TestFailedOutcomeKeepsItsNoticesAndCoverage(t *testing.T) {
	m, _ := newFixtureManager(t)
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{
		Collection: "results", Query: "failed",
	}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Outcome != OutcomeFailed || len(page.Items) != 0 {
		t.Fatalf("page = %+v", page)
	}
	if len(page.Notices) != 1 || page.Notices[0].Tone != resource.ToneDanger {
		t.Fatalf("notices = %+v", page.Notices)
	}
	if len(page.Coverage) != 1 || page.Coverage[0].State != CoverageFailed {
		t.Fatalf("coverage = %+v", page.Coverage)
	}
}

// An over-limit coverage ledger is truncated and marked, exactly as over-limit
// rows are: a page that is merely too big must still explain itself.
func TestOverLimitCoverageIsTruncatedAndMarked(t *testing.T) {
	m, _ := newFixtureManager(t)
	page, err := m.List(context.Background(), ListRequest{Instance: "fixture", Params: ListParams{
		Collection: "results", Query: "mode:over-limit-page:x",
	}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Coverage) != MaxCoverageRows || !page.CoverageTruncated {
		t.Fatalf("coverage = %d rows (truncated=%v), want %d and marked", len(page.Coverage), page.CoverageTruncated, MaxCoverageRows)
	}
	if runeLenTest(page.Coverage[0].Reason) != MaxCoverageReasonChars {
		t.Fatalf("reason kept %d chars, want it cut to %d", runeLenTest(page.Coverage[0].Reason), MaxCoverageReasonChars)
	}
	// A negative count is noise, not a claim that a negative number of rows
	// was held back.
	if page.Omitted != (Omitted{Dropped: 9}) {
		t.Fatalf("omitted = %+v", page.Omitted)
	}
}

func runeLenTest(s string) int { return len([]rune(s)) }

// A second get for the same pane supersedes the first, exactly as a list does.
// It is what makes a detail box that follows the cursor affordable: ten rows
// cost at most one live process, and the ones the cursor left are killed rather
// than left running to the end of their timeout.
func TestSupersededGetForAPaneIsKilled(t *testing.T) {
	m, _ := newFixtureManager(t)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := m.Get(context.Background(), GetRequest{
			Instance: "fixture",
			PaneKey:  "pluginbrowser-detail/1",
			// The fixture sleeps ten minutes; only a kill ends it.
			Params: GetParams{Collection: "results", ID: "mode:hang:rc:notes:1"},
		})
		done <- err
	}()
	<-started

	deadline := time.After(10 * time.Second)
	for {
		if _, err := m.Get(context.Background(), GetRequest{
			Instance: "fixture",
			PaneKey:  "pluginbrowser-detail/1",
			Params:   GetParams{Collection: "results", ID: "rc:notes:2"},
		}); err != nil {
			t.Fatalf("the superseding get failed: %v", err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("the superseded get returned a document instead of being cancelled")
			}
			if OutcomeCode(err) != string(ReasonCanceled) && OutcomeCode(err) != string(ReasonTimeout) {
				t.Fatalf("superseded get ended with %q, want the process group to have been killed", OutcomeCode(err))
			}
			return
		case <-deadline:
			t.Fatal("the superseded get was still running 10s after being superseded")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// A get with no pane key supersedes nothing: a CLI call and a pane's call are
// two readers, and one must not cancel the other. The unkeyed reader here is
// slow on purpose, so the later gets overlap it rather than following it.
func TestAGetWithNoPaneKeySupersedesNothing(t *testing.T) {
	m, _ := newFixtureManager(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		// The fixture sleeps ten minutes; only a kill ends it.
		_, err := m.Get(ctx, GetRequest{
			Instance: "fixture",
			Params:   GetParams{Collection: "results", ID: "mode:hang:rc:notes:1"},
		})
		done <- err
	}()

	if _, err := m.Get(context.Background(), GetRequest{
		Instance: "fixture",
		Params:   GetParams{Collection: "results", ID: "rc:notes:2"},
	}); err != nil {
		t.Fatalf("the second unkeyed get failed: %v", err)
	}
	if _, err := m.Get(context.Background(), GetRequest{
		Instance: "fixture",
		PaneKey:  "pluginbrowser-detail/1",
		Params:   GetParams{Collection: "results", ID: "rc:notes:3"},
	}); err != nil {
		t.Fatalf("the keyed get failed: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("the unkeyed get was ended by another reader: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	// Its own caller can still end it, and the process must go with it.
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the cancelled get returned a document")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the unkeyed get outlived its own context")
	}
}

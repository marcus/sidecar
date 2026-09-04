package pluginbrowser

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
)

// sourcesPage is the ledger collection's answer: three sources, each with the
// pill that says whether it answered.
func sourcesPage() pluginhost.Page {
	return pluginhost.Page{
		Outcome: pluginhost.OutcomeAnswered,
		Items: []pluginhost.Item{
			{ID: "notes", Cells: map[string]string{"name": "notes"},
				Status: &resource.Status{Label: "answered", Tone: resource.ToneSuccess}},
			{ID: "mail", Cells: map[string]string{"name": "mail"},
				Status: &resource.Status{Label: "checkpoint stale", Tone: resource.ToneWarning}},
		},
		Total: 2,
	}
}

// An empty detail box in a Tab placement shows the plugin's next collection
// rather than a card of help text. recall's `sources` beside recall's
// `results` is the case this exists for: "no matches" and "why" have to be on
// screen together, or an `abstained` page cannot be checked where it is read.
func TestEmptyTabDetailShowsTheNextCollection(t *testing.T) {
	host := &fakeHost{page: sourcesPage()}
	m := newTestModel(t, host)

	// The next collection was read once, without a query, because its search is
	// not required.
	if n := listsFor(host, "sources"); n != 1 {
		t.Fatalf("the next collection was listed %d times, want once", n)
	}
	if got := lastListFor(t, host, "sources").Params.Query; got != "" {
		t.Fatalf("the next collection was listed with query %q; it is not searched here", got)
	}

	view := strip(m.View())
	if !strings.Contains(view, "Fixture · Sources") {
		t.Fatalf("the empty detail box does not name the next collection:\n%s", view)
	}
	for _, want := range []string{"notes", "answered", "mail", "checkpoint stale"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the next collection's rows are missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Press Enter on a row to open it here.") {
		t.Fatalf("the help card is still there beside a collection to show:\n%s", view)
	}

	// Opening a row replaces it: the box is the document's again.
	host.doc = resource.Document{Identity: "rc:notes:1", Title: "A document"}
	run(t, m, m.openDocument("results", "rc:notes:1", openReplace))
	view = strip(m.View())
	if strings.Contains(view, "Fixture · Sources") {
		t.Fatalf("the next collection survived a document being opened:\n%s", view)
	}
}

// With one collection there is no next one, so the help line stays: the box
// still has to say what the next gesture does.
func TestEmptyTabDetailKeepsTheHelpLineWithOneCollection(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	host.desc = testDescription()
	host.desc.Collections = host.desc.Collections[:1]
	run(t, m, m.Refresh())

	view := strip(m.View())
	if !strings.Contains(view, "Press Enter on a row to open it here.") {
		t.Fatalf("a single-collection plugin lost its help line:\n%s", view)
	}
}

// A pane shows one shape at a time and has no second box, so nothing changes
// there and no extra process is spent.
func TestPaneDetailDoesNotShowTheNextCollection(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	before := listsFor(host, "sources")
	m.SetPaneCollection("results")
	run(t, m, m.Refresh())
	if n := listsFor(host, "sources"); n != before {
		t.Fatalf("a pane read the next collection: %d lists, want %d", n, before)
	}
	if _, ok := m.nextCollection(); ok {
		t.Fatal("a pane-mode browser claimed a next collection")
	}
}

// A `search: required` next collection is never listed to fill the box: it has
// nothing to say without a query, and asking would spend a process being told
// so on every describe.
func TestRequiredSearchNextCollectionIsNotListed(t *testing.T) {
	host := &fakeHost{page: testPage(2)}
	m := newTestModel(t, host)
	host.desc = testDescription()
	host.desc.Collections[1].Search = pluginhost.SearchRequired
	host.lists = nil
	m.states = map[string]*collectionState{}
	run(t, m, m.Refresh())
	if n := listsFor(host, "sources"); n != 0 {
		t.Fatalf("a required-search next collection was listed %d times", n)
	}
	view := strip(m.View())
	if !strings.Contains(view, "Fixture · Sources") {
		t.Fatal("the box does not name the next collection at all")
	}
	// And it says why it is empty. "Not read yet" would blame the host for a
	// silence the collection's own contract explains.
	if !strings.Contains(view, "This collection answers a query, and there is none.") {
		t.Fatalf("the box does not say the next collection needs a query:\n%s", view)
	}
	if strings.Contains(view, "Not read yet.") {
		t.Fatalf("the box claims the next collection is merely unread:\n%s", view)
	}
}

// A collection whose rows have no document behind them keeps that fact: the
// next-collection box says so rather than promising an Enter that does nothing.
func TestNextCollectionBoxSaysWhenRowsHaveNoDocument(t *testing.T) {
	host := &fakeHost{page: sourcesPage()}
	m := newTestModel(t, host)
	host.desc = testDescription()
	host.desc.Collections[0].Detail = false
	run(t, m, m.Refresh())

	view := strip(m.View())
	if !strings.Contains(view, "Fixture · Sources") {
		t.Fatalf("the box does not name the next collection:\n%s", view)
	}
	if strings.Contains(view, "Enter on a row opens it here.") {
		t.Fatalf("the box promised an Enter this collection cannot answer:\n%s", view)
	}
	if !strings.Contains(view, "The list beside this has no documents behind its rows.") {
		t.Fatalf("the box does not say the rows have no document:\n%s", view)
	}
}

package resourceview

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/resource"
)

func TestOpenSameLocatorFocusesRatherThanDuplicating(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	tabs.Open(ref("CASH-1"))
	tabs.Open(ref("CASH-2"))
	if tabs.Len() != 2 {
		t.Fatalf("want 2 tabs, got %d", tabs.Len())
	}

	tabs.Open(ref("CASH-1"))
	if tabs.Len() != 2 {
		t.Fatalf("re-opening a locator must focus, not append: got %d tabs", tabs.Len())
	}
	if got := tabs.Active().Reference().Locator; got != "CASH-1" {
		t.Errorf("active = %q, want CASH-1", got)
	}
}

func TestOpenDistinguishesProviderAndMatcher(t *testing.T) {
	tabs := NewTabs(nil, (&recorder{}).resolver())
	tabs.Open(resource.Reference{Instance: "a", Matcher: "m", Locator: "X-1"})
	tabs.Open(resource.Reference{Instance: "b", Matcher: "m", Locator: "X-1"})
	if tabs.Len() != 2 {
		t.Fatalf("the same locator from two providers is two resources, got %d tabs", tabs.Len())
	}
}

func TestSetResolverRebindsExistingArmedTabs(t *testing.T) {
	var called bool
	tabs := NewTabs(nil, nil)
	tabs.Arm(ref("CASH-1"), 0)
	tabs.SetResolver(func(modelID int, generation, epoch uint64, got Ref, refresh bool) tea.Cmd {
		called = true
		return nil
	})
	tabs.ResolveActive()
	if !called {
		t.Fatal("existing tab kept the resolver bound at construction")
	}
}

func TestCycleWrapsBothWays(t *testing.T) {
	tabs := NewTabs(nil, (&recorder{}).resolver())
	tabs.Open(ref("CASH-1"))
	tabs.Open(ref("CASH-2"))
	tabs.Open(ref("CASH-3"))

	tabs.Next()
	if got := tabs.Active().Reference().Locator; got != "CASH-1" {
		t.Errorf("next from the last tab should wrap to the first, got %q", got)
	}
	tabs.Prev()
	if got := tabs.Active().Reference().Locator; got != "CASH-3" {
		t.Errorf("prev from the first tab should wrap to the last, got %q", got)
	}
}

func TestCloseKeepsAReasonableActiveTab(t *testing.T) {
	tabs := NewTabs(nil, (&recorder{}).resolver())
	tabs.Open(ref("CASH-1"))
	tabs.Open(ref("CASH-2"))
	tabs.Open(ref("CASH-3"))

	if empty := tabs.CloseActive(); empty {
		t.Fatal("closing one of three must not empty the leaf")
	}
	if tabs.Len() != 2 {
		t.Fatalf("want 2 tabs, got %d", tabs.Len())
	}
	if tabs.Active() == nil {
		t.Fatal("there must still be an active tab")
	}
	tabs.CloseActive()
	if empty := tabs.CloseActive(); !empty {
		t.Error("closing the last tab must report the leaf empty")
	}
}

func TestApplyRoutesToTheRequestingTabOnly(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	tabs.SetSize(40, 10)
	tabs.Open(ref("CASH-1"))
	firstID, firstGen, _ := rec.last()
	tabs.Open(ref("CASH-2"))

	if !tabs.Apply(ResolvedMsg{ModelID: firstID, Generation: firstGen,
		Document: doc("CASH-1", "First")}) {
		t.Fatal("the first tab's answer should have been routed to it")
	}
	if _, ok := tabs.At(0).Document(); !ok {
		t.Error("tab 0 should hold the document")
	}
	if _, ok := tabs.At(1).Document(); ok {
		t.Error("tab 1 must not have received another tab's answer")
	}
}

func TestAnswerForAClosedTabIsDropped(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	tabs.Open(ref("CASH-1"))
	id, gen, _ := rec.last()
	tabs.CloseActive()

	if tabs.Apply(ResolvedMsg{ModelID: id, Generation: gen, Document: doc("CASH-1", "gone")}) {
		t.Error("a result for a closed tab must not be applied anywhere")
	}
}

func TestCanonicalIdentityRekeysTheTab(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	tabs.Open(ref("cash-1"))
	id, gen, _ := rec.last()

	tabs.Apply(ResolvedMsg{ModelID: id, Generation: gen, Document: doc("CASH-1", "Canonical")})
	if got := tabs.At(0).Reference().Locator; got != "CASH-1" {
		t.Errorf("locator = %q, want the canonical CASH-1", got)
	}
}

func TestCanonicalIdentityMergesWithAnAlreadyOpenTab(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	// The user opens the canonical key first, then a variant that resolves to
	// the same thing. They must not end up with two tabs for one ticket.
	tabs.Open(ref("CASH-1"))
	tabs.Open(ref("cash-1"))
	if tabs.Len() != 2 {
		t.Fatalf("precondition: want 2 tabs before the merge, got %d", tabs.Len())
	}
	id, gen, _ := rec.last()

	tabs.Apply(ResolvedMsg{ModelID: id, Generation: gen, Document: doc("CASH-1", "Canonical")})

	if tabs.Len() != 1 {
		t.Fatalf("want the duplicate merged away, got %d tabs", tabs.Len())
	}
	surviving := tabs.Active()
	if surviving == nil {
		t.Fatal("the merged tab must still be active")
	}
	if got, ok := surviving.Document(); !ok || got.Title != "Canonical" {
		t.Errorf("the resolved tab should survive the merge, got %+v ok=%v", got, ok)
	}
}

func TestReferencesAreTheOnlyThingPersisted(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	tabs.SetSize(40, 10)
	tabs.Open(ref("CASH-1"))
	id, gen, _ := rec.last()
	tabs.Apply(ResolvedMsg{ModelID: id, Generation: gen, Document: resource.Document{
		Identity: "CASH-1", Title: "secret title",
		Fields:    []resource.Field{{Label: "Assignee", Value: "someone"}},
		Body:      &resource.Body{Format: resource.FormatText, Text: "ticket body"},
		SourceURL: "https://jira.example.test/browse/CASH-1",
	}})

	got := tabs.References()
	if len(got) != 1 {
		t.Fatalf("want 1 persisted reference, got %d", len(got))
	}
	// The struct has no field that could carry a title, body, or URL. This
	// asserts what it does carry, which is the reference and nothing else.
	want := PersistedTab{Provider: "jira-work", Matcher: "issue-key", Locator: "CASH-1"}
	if !got[0].Equal(want) {
		t.Errorf("persisted = %+v, want %+v", got[0], want)
	}
}

func TestArmedTabsRestoreWithoutResolving(t *testing.T) {
	rec := &recorder{}
	tabs := NewTabs(nil, rec.resolver())
	tabs.SetSize(40, 10)
	tabs.Arm(ref("CASH-1"), 0)
	tabs.Arm(ref("CASH-2"), 5)
	tabs.Arm(ref("CASH-3"), 0)

	if len(rec.calls) != 0 {
		t.Fatalf("restore must not fan out one process per tab, got %d resolves", len(rec.calls))
	}
	if tabs.Len() != 3 {
		t.Fatalf("want 3 restored tabs, got %d", tabs.Len())
	}

	// Only selecting a tab resolves it.
	tabs.SetActive(1)
	if len(rec.calls) != 1 {
		t.Fatalf("selecting one restored tab should resolve exactly it, got %d", len(rec.calls))
	}
	if tabs.At(0).State() != StateArmed || tabs.At(2).State() != StateArmed {
		t.Error("unselected restored tabs must stay armed")
	}
}

func TestTabCountIsBounded(t *testing.T) {
	tabs := NewTabs(nil, (&recorder{}).resolver())
	for i := 0; i < MaxTabs+8; i++ {
		tabs.Open(ref(fmt.Sprintf("CASH-%d", i)))
	}
	if tabs.Len() > MaxTabs {
		t.Fatalf("tabs = %d, want at most %d", tabs.Len(), MaxTabs)
	}
	if tabs.Active() == nil {
		t.Fatal("the most recently opened tab must still be active")
	}
	if got := tabs.Active().Reference().Locator; got != fmt.Sprintf("CASH-%d", MaxTabs+7) {
		t.Errorf("active = %q, want the newest tab", got)
	}
}

func TestLabelsAreLocatorsFromTheMomentOfTheClick(t *testing.T) {
	tabs := NewTabs(nil, (&recorder{}).resolver())
	tabs.Open(ref("CASH-1"))
	tabs.Open(ref("GRES-9"))
	labels := tabs.Labels()
	want := []string{"CASH-1", "GRES-9"}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("label %d = %q, want %q", i, labels[i], want[i])
		}
	}
}

func TestEmptyTabsRenderNothingAndReportEmpty(t *testing.T) {
	tabs := NewTabs(nil, (&recorder{}).resolver())
	if !tabs.Empty() {
		t.Error("a fresh tab set is empty")
	}
	if got := tabs.View(); got != "" {
		t.Errorf("view = %q, want empty", got)
	}
	if tabs.Active() != nil {
		t.Error("there is no active tab")
	}
}

// A filter deliberately cleared to the empty string is a value, not an absence:
// a text filter with a declared default is cleared exactly that way. Comparing
// two records by lookup rather than by comma-ok would call these equal and skip
// the save that carries the change.
func TestPersistedTabEqualDistinguishesAClearedFilter(t *testing.T) {
	base := PersistedTab{Provider: "recall", Collection: "results"}
	cleared := base
	cleared.Filters = map[string]string{"since": ""}
	other := base
	other.Filters = map[string]string{"profile": ""}
	if cleared.Equal(other) {
		t.Fatal("two different filter sets compared equal because an absent key reads as an empty value")
	}
	same := base
	same.Filters = map[string]string{"since": ""}
	if !cleared.Equal(same) {
		t.Fatal("the same filter set compared unequal")
	}
}

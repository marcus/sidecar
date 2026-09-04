package pluginhost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/resource"
)

// The manager's three plugin-protocol promises that a conformance case against
// the real fixture cannot see, because they are about how many child processes
// run rather than about what one answers: get shares the resolve cache, act
// shares nothing, and a collection the plugin never declared is refused before
// anything is started.
//
// A counting fake stands in for the process adapter here for exactly that
// reason — the property under test is the count.

// countingPlugin is a PluginProvider that records how often each method ran.
type countingPlugin struct {
	instance string

	mu       sync.Mutex
	describe int
	get      int
	act      int
	list     int
}

func (p *countingPlugin) Instance() string { return p.instance }

func (p *countingPlugin) Describe(context.Context) (Description, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.describe++
	return Description{
		Info: Info{Kind: "counting", Name: "Counting"},
		Collections: []Collection{{
			ID:      "rows",
			Title:   "Rows",
			Search:  SearchOptional,
			Detail:  true,
			Columns: []Column{{ID: "title", Label: "Title", Primary: true}},
		}},
		Actions: []Action{{ID: "poke", Title: "Poke", On: ActionOnItem, Collection: "rows", Mutates: true}},
	}, nil
}

func (p *countingPlugin) Resolve(context.Context, resource.Reference) (resource.Document, error) {
	return resource.Document{}, &resource.Error{Code: resource.CodeNotFound, Message: "no matchers"}
}

func (p *countingPlugin) List(_ context.Context, params ListParams, _ *Context, _ Collection) (Page, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list++
	return Page{Outcome: OutcomeAnswered, Items: []Item{{ID: "r1", Cells: map[string]string{"title": params.Query}}}}, nil
}

func (p *countingPlugin) Get(_ context.Context, params GetParams, _ *Context, _ Collection) (resource.Document, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.get++
	// FreshFor is what makes the entry cacheable at all; a document that is
	// stale on arrival would make this test pass for the wrong reason.
	return resource.Document{Identity: params.ID, Title: "Row " + params.ID, FreshFor: time.Minute}, nil
}

func (p *countingPlugin) Act(context.Context, ActParams, *Context) (Outcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.act++
	return Outcome{Status: ActDone, Message: "done"}, nil
}

func (p *countingPlugin) counts() (describe, list, get, act int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.describe, p.list, p.get, p.act
}

func newCountingManager(t *testing.T) (*Manager, *countingPlugin) {
	t.Helper()
	plugin := &countingPlugin{instance: "counting"}
	m := NewManager(ManagerOptions{})
	m.SetProviders([]Provider{plugin}, nil)
	m.DescribeAll(context.Background())
	return m, plugin
}

// A second Enter on the same row must cost no process: get shares the resolve
// cache under a get-prefixed key.
func TestGetSharesTheResolveCache(t *testing.T) {
	m, plugin := newCountingManager(t)
	req := GetRequest{Instance: "counting", Params: GetParams{Collection: "rows", ID: "r1"}}

	first, err := m.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	second, err := m.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if first.Identity != second.Identity || second.Identity != "r1" {
		t.Fatalf("identities = %q, %q", first.Identity, second.Identity)
	}
	if _, _, got, _ := plugin.counts(); got != 1 {
		t.Fatalf("get ran %d times, want 1: the second Enter started a process", got)
	}

	// Refresh is the explicit opt out, and it re-caches.
	refresh := req
	refresh.Refresh = true
	if _, err := m.Get(context.Background(), refresh); err != nil {
		t.Fatalf("refreshing Get: %v", err)
	}
	if _, _, got, _ := plugin.counts(); got != 2 {
		t.Fatalf("get ran %d times after a refresh, want 2", got)
	}
}

// Two identical acts are two intentions. Collapsing them would silently drop a
// change the user asked for twice on purpose.
func TestActIsNeverCachedOrDeduplicated(t *testing.T) {
	m, plugin := newCountingManager(t)
	req := ActRequest{Instance: "counting", Params: ActParams{Action: "poke", Collection: "rows", ID: "r1"}}
	for i := 0; i < 3; i++ {
		if _, err := m.Act(context.Background(), req); err != nil {
			t.Fatalf("Act %d: %v", i, err)
		}
	}
	if _, _, _, act := plugin.counts(); act != 3 {
		t.Fatalf("act ran %d times, want 3", act)
	}
}

// The declaration is what says which columns exist, so a get for a collection
// the newest describe never declared is refused before anything is started —
// the same rule list already holds.
func TestGetRefusesAnUndeclaredCollection(t *testing.T) {
	m, plugin := newCountingManager(t)
	_, err := m.Get(context.Background(), GetRequest{
		Instance: "counting",
		Params:   GetParams{Collection: "nowhere", ID: "r1"},
	})
	if err == nil {
		t.Fatal("Get accepted a collection the plugin never declared")
	}
	var rerr *resource.Error
	if !errors.As(err, &rerr) || rerr.Code != resource.CodeInvalidRequest {
		t.Fatalf("error = %v, want a typed invalid_request", err)
	}
	if _, _, got, _ := plugin.counts(); got != 0 {
		t.Fatalf("get ran %d times, want 0: the refusal started a process", got)
	}
}

// An instance that has been removed keeps no describe result behind it, so a
// later call cannot be answered from a declaration that is no longer true.
func TestRemovingAnInstanceDropsItsDescription(t *testing.T) {
	m, _ := newCountingManager(t)
	if _, ok := m.Description("counting"); !ok {
		t.Fatal("the description did not land")
	}
	m.SetProviders(nil, nil)
	if _, ok := m.Description("counting"); ok {
		t.Fatal("a removed instance kept its description")
	}
	if _, err := m.List(context.Background(), ListRequest{
		Instance: "counting",
		Params:   ListParams{Collection: "rows"},
	}); err == nil {
		t.Fatal("List answered for a removed instance")
	}
}

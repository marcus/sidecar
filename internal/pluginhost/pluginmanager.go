package pluginhost

import (
	"context"

	"github.com/marcus/sidecar/internal/resource"
)

// The manager's plugin-protocol half. It is the same manager, the same cache,
// the same per-instance and global concurrency budget, and the same process
// group policy the resource protocol has had: a plugin's list is a child
// process like a provider's resolve, and one budget has to cover both or the
// budget covers nothing.

// ListRequest addresses one list call.
type ListRequest struct {
	Instance string
	Params   ListParams
	// Context is what the caller can offer. The provider narrows it to the
	// kinds the plugin declared, so offering more than a plugin reads is safe.
	Context *Context
	// PaneKey identifies the surface asking. A second list for the same pane
	// supersedes the first: the earlier call's context is cancelled, which
	// kills its process group. Empty means "no pane", and nothing is
	// superseded — which is what a CLI call wants.
	PaneKey string
}

// GetRequest addresses one get call.
type GetRequest struct {
	Instance string
	Params   GetParams
	Context  *Context
	// Refresh bypasses cached freshness for this call and re-caches the
	// result. A failed refresh leaves the last good document available.
	Refresh bool
	// PaneKey identifies the surface asking, exactly as it does for a list. A
	// second get for the same key supersedes the first: the earlier call's
	// context is cancelled, which kills its process group. That is what makes
	// a detail box that follows the cursor affordable — ten rows cost at most
	// one live process, not ten. Empty means "no pane", and nothing is
	// superseded, which is what a CLI call wants.
	PaneKey string
}

// ActRequest addresses one act call.
type ActRequest struct {
	Instance string
	Params   ActParams
	Context  *Context
}

// Description returns an instance's newest validated describe result.
func (m *Manager) Description(instance string) (Description, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	desc, ok := m.descriptions[instance]
	return desc, ok
}

// Collection returns one declared collection of one instance.
func (m *Manager) Collection(instance, collection string) (Collection, bool) {
	desc, ok := m.Description(instance)
	if !ok {
		return Collection{}, false
	}
	return desc.Collection(collection)
}

// List returns one page of one collection.
//
// The collection must be one the instance declared in its newest successful
// describe. That is not ceremony: the declaration is what says which columns
// exist, and a page sanitized against no declaration would carry cells the host
// has nowhere to paint. It also means a list for a collection that has gone
// away fails with a typed error rather than starting a process to find out.
func (m *Manager) List(ctx context.Context, req ListRequest) (Page, error) {
	provider, err := m.pluginFor(req.Instance, MethodList)
	if err != nil {
		return Page{}, err
	}
	collection, ok := m.Collection(req.Instance, req.Params.Collection)
	if !ok {
		return Page{}, &resource.Error{
			Code:    resource.CodeInvalidRequest,
			Message: "That plugin does not declare a collection with that id.",
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if req.PaneKey != "" {
		call := &pendingCall{cancel: cancel}
		m.supersede(req.PaneKey, call)
		defer m.clearPending(req.PaneKey, call)
	}

	if err := m.acquire(ctx, req.Instance); err != nil {
		return Page{}, err
	}
	defer m.release(req.Instance)
	return provider.List(ctx, req.Params, req.Context, collection)
}

// Get expands one collection row into one document. It shares the resolve
// cache: a second Enter on the same row costs no process.
//
// With a PaneKey it also supersedes: the surface's previous get is cancelled
// and its process group killed, on the same rule and through the same pending
// map List uses. A cursor moving down a list is one surface asking a new
// question, not ten surfaces asking at once.
func (m *Manager) Get(ctx context.Context, req GetRequest) (resource.Document, error) {
	provider, err := m.pluginFor(req.Instance, MethodGet)
	if err != nil {
		return resource.Document{}, err
	}
	if _, ok := m.Collection(req.Instance, req.Params.Collection); !ok {
		return resource.Document{}, &resource.Error{
			Code:    resource.CodeInvalidRequest,
			Message: "That plugin does not declare a collection with that id.",
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if req.PaneKey != "" {
		call := &pendingCall{cancel: cancel}
		m.supersede(req.PaneKey, call)
		defer m.clearPending(req.PaneKey, call)
	}
	// The cache key carries a NUL separator, which SanitizeLine strips from
	// every locator, so a get can never collide with a resolve of the same
	// instance however a plugin spells its ids.
	lookup := "get\x00" + req.Params.Collection + "\x00" + req.Params.ID
	return m.fetch(ctx, req.Instance, MethodGet, lookup, req.Refresh, func(ctx context.Context) (resource.Document, error) {
		return provider.Get(ctx, req.Params, req.Context)
	})
}

// Act performs one typed operation.
//
// It is never cached and never deduplicated. Two identical acts are two
// intentions, and collapsing them into one would silently drop a change the
// user asked for twice on purpose.
func (m *Manager) Act(ctx context.Context, req ActRequest) (Outcome, error) {
	provider, err := m.pluginFor(req.Instance, MethodAct)
	if err != nil {
		return Outcome{}, err
	}
	desc, ok := m.Description(req.Instance)
	if !ok {
		return Outcome{}, &resource.Error{
			Code:    resource.CodeInvalidRequest,
			Message: "That plugin has not described itself yet.",
		}
	}
	if _, ok := desc.Action(req.Params.Action); !ok {
		return Outcome{}, &resource.Error{
			Code:    resource.CodeInvalidRequest,
			Message: "That plugin does not declare an action with that id.",
		}
	}
	if err := m.acquire(ctx, req.Instance); err != nil {
		return Outcome{}, err
	}
	defer m.release(req.Instance)
	return provider.Act(ctx, req.Params, req.Context)
}

// pendingCall is one pane's in-flight list. It exists so the map can be keyed
// on identity: Go cannot compare two func values, and a call that has already
// been superseded must not delete its successor's entry on the way out.
type pendingCall struct {
	cancel context.CancelFunc
}

// CancelPane cancels whatever call is in flight for a pane — a list or a get —
// killing its process group. A pane that closes while a slow call is running
// must not leave a child behind.
func (m *Manager) CancelPane(paneKey string) {
	if paneKey == "" {
		return
	}
	m.mu.Lock()
	call := m.pending[paneKey]
	delete(m.pending, paneKey)
	m.mu.Unlock()
	if call != nil {
		call.cancel()
	}
}

// supersede records this call as the pane's in-flight one and cancels whatever
// was there. The cancel runs outside the lock: it kills a process group, and
// holding the manager lock across that would stall every other instance.
func (m *Manager) supersede(paneKey string, call *pendingCall) {
	m.mu.Lock()
	previous := m.pending[paneKey]
	m.pending[paneKey] = call
	m.mu.Unlock()
	if previous != nil {
		previous.cancel()
	}
}

// clearPending removes this call only if it is still the pane's.
func (m *Manager) clearPending(paneKey string, call *pendingCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.pending[paneKey]; ok && current == call {
		delete(m.pending, paneKey)
	}
}

// pluginFor resolves an instance to a provider that speaks the plugin protocol.
// A resource provider is refused here, before anything is spawned, with the
// same typed error a missing instance gets — because from the caller's side
// both mean "this instance cannot answer that".
func (m *Manager) pluginFor(instance, method string) (PluginProvider, error) {
	provider, err := m.providerFor(instance)
	if err != nil {
		return nil, err
	}
	plugin, ok := provider.(PluginProvider)
	if !ok {
		return nil, &resource.Error{
			Code:    resource.CodeInvalidRequest,
			Message: "That instance does not speak the plugin protocol, so it has no " + method + " method.",
		}
	}
	if speaker, ok := provider.(interface{ SpeaksPluginProtocol() bool }); ok && !speaker.SpeaksPluginProtocol() {
		return nil, &resource.Error{
			Code:    resource.CodeInvalidRequest,
			Message: "That instance is configured as a terminal resource provider, which has no " + method + " method.",
		}
	}
	return plugin, nil
}

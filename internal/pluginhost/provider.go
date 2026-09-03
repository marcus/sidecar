// Package pluginhost runs external Sidecar plugins: it owns the process
// boundary, the protocol envelope, the compiled matcher snapshot, and the
// host-side cache and concurrency policy.
//
// It speaks two dialects through one manager, one cache, and one process
// policy. An instance configured under terminalResources is dispatched with the
// frozen sidecar.terminal-resource/v1 identifier and answers describe and
// resolve; an instance configured under plugins.external is dispatched with
// sidecar.plugin/v1-draft and additionally answers list, get, and act. Both end
// up as the same host types, because the second protocol is the first one grown
// rather than a replacement.
//
// Two seams, each narrow on purpose:
//
//   - Provider is the in-process adapter the Manager consumes. CommandProvider
//     is the default implementation; tests use an in-memory fake; a future
//     resident transport implements the same methods. PluginProvider is the
//     optional extension a plugin-protocol instance also satisfies.
//   - The executable protocol is the language-agnostic boundary. It returns
//     declarations and data, never a Sidecar interface.
//
// A process boundary is crash isolation, not a sandbox. Enabling a plugin
// trusts that executable with the user's full OS privileges.
//
// See docs/reference/plugin-protocol.md and
// docs/reference/terminal-resource-provider-protocol.md.
package pluginhost

import (
	"context"

	"github.com/marcus/sidecar/internal/resource"
)

// Provider is the whole of what the Manager may ask an implementation to do.
// Both methods must be safe for concurrent use and must honor ctx.
type Provider interface {
	// Instance is the configured provider instance ID. It is host-owned: a
	// provider cannot rename itself, and a response never changes it.
	Instance() string

	// Describe reports what the provider is and what it can recognize. It must
	// be local, fast, and non-interactive: no credential prompt, no network.
	Describe(ctx context.Context) (Description, error)

	// Resolve turns one locator into one document. It may perform network I/O.
	Resolve(ctx context.Context, ref resource.Reference) (resource.Document, error)
}

// PluginProvider is a Provider that also speaks the plugin protocol. It is a
// separate interface rather than three more methods on Provider so that a
// resource provider — and every in-memory fake written against the frozen
// protocol — stays a complete implementation of what it actually offers.
//
// All three methods must be safe for concurrent use and must honor ctx.
type PluginProvider interface {
	Provider

	// List returns one page of one collection. collection is the validated
	// declaration from the describe snapshot, which is how the host knows
	// which cells a row is allowed to carry.
	List(ctx context.Context, params ListParams, callCtx *Context, collection Collection) (Page, error)

	// Get expands one row into one document, with sections.
	Get(ctx context.Context, params GetParams, callCtx *Context) (resource.Document, error)

	// Act performs one typed operation. It is the only method that mutates.
	Act(ctx context.Context, params ActParams, callCtx *Context) (Outcome, error)
}

// Info is the informational identity a provider declares. None of it can
// rename or collide with a configured instance ID.
type Info struct {
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	// DocsURL is the only executable-declared Setup action Sidecar will
	// follow, and only after user confirmation. It passes the same http/https
	// validation as a resource's sourceUrl.
	DocsURL string `json:"docsUrl,omitempty"`
}

// Matcher is one declared resource-key shape. The pattern is Go/RE2 and the
// whole match is the locator: there are no capture-group templates and no
// provider code runs in the scanner.
type Matcher struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`
	Priority int    `json:"priority,omitempty"`
}

// Description is a validated describe result. Reaching this type means every
// pattern compiled, every ID was unique, and every bound held.
//
// The last three fields are the plugin protocol's; a resource provider leaves
// them empty, which is exactly what "a plugin that declares no collections and
// no actions is exactly a resource provider" means in the host.
type Description struct {
	Info     Info
	Matchers []Matcher
	// Context is the kinds of host context the plugin declared it reads.
	// Nothing outside this list is ever sent to it.
	Context []ContextKind
	// Collections are the listable sets of rows it offers.
	Collections []Collection
	// Actions are the typed operations it exposes.
	Actions []Action
}

// Collection returns one declared collection by ID.
func (d Description) Collection(id string) (Collection, bool) {
	for _, c := range d.Collections {
		if c.ID == id {
			return c, true
		}
	}
	return Collection{}, false
}

// Action returns one declared action by ID.
func (d Description) Action(id string) (Action, bool) {
	for _, a := range d.Actions {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

// ReadsContext reports whether the plugin declared it reads kind.
func (d Description) ReadsContext(kind ContextKind) bool {
	for _, declared := range d.Context {
		if declared == kind {
			return true
		}
	}
	return false
}

// WatchPaths returns every distinct path this plugin asked the host to watch,
// across all its collections. It is what the livepanes binding reads.
func (d Description) WatchPaths() []string {
	var out []string
	seen := make(map[string]bool)
	for _, c := range d.Collections {
		for _, path := range c.Refresh.Watch {
			if seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

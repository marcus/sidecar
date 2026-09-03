package pluginhost

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/marcus/sidecar/internal/resource"
)

// Protocol is the plugin contract this host speaks. It is a draft: it freezes
// the way sidecar.terminal-resource/v1 did, after one real external plugin has
// implemented it against a live tool and both have revised what they found.
//
// The frozen resource identifier lives in internal/resource and is unchanged.
// One host speaks both: an instance configured under terminalResources is
// dispatched with resource.Protocol, an instance configured under
// plugins.external with this one, and the answer is translated into the same
// host types either way.
const Protocol = "sidecar.plugin/v1-draft"

// Method names. An unknown method must return an internal error rather than
// crash the plugin; the host never sends one.
const (
	MethodDescribe = "describe"
	MethodResolve  = "resolve"
	MethodList     = "list"
	MethodGet      = "get"
	MethodAct      = "act"
)

// HostInfo identifies Sidecar to a plugin. It carries no user, no project,
// no repository, and no environment.
type HostInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Request is the single JSON object written to a plugin's stdin.
//
// DeadlineMs is advisory but accurate: it is exactly the timeout the host is
// about to enforce. A plugin that budgets its own I/O inside it can return a
// typed `unavailable` — which gives the user a real error card and a working
// Retry — instead of being SIGKILLed, which gives them an opaque transport
// failure.
//
// Params is typed per method (*ResolveParams, *ListParams, *GetParams,
// *ActParams) rather than one union struct, because a union would encode every
// method's fields on every call and a plugin author reading the wire would have
// to guess which of them applied.
type Request struct {
	Protocol   string    `json:"protocol"`
	Method     string    `json:"method"`
	Instance   string    `json:"instance"`
	DeadlineMs int64     `json:"deadlineMs"`
	Host       *HostInfo `json:"host,omitempty"`
	// Context is present only for kinds the plugin declared in describe and
	// the host actually has. An undeclared kind is never sent.
	Context *Context `json:"context,omitempty"`
	Params  any      `json:"params,omitempty"`
}

// ResolveParams is the whole of what a resolve request carries. Widening it
// requires a named capability and an explicit per-instance permission, not a
// silent field addition.
type ResolveParams struct {
	Matcher string `json:"matcher"`
	Locator string `json:"locator"`
}

// ListParams addresses one page of one collection.
//
// Filters carries every declared filter whose value is not its default; a
// missing key means the default. The host drops undeclared keys before the
// call, so a plugin only ever reads names it published itself.
type ListParams struct {
	Collection string            `json:"collection"`
	Query      string            `json:"query"`
	View       string            `json:"view"`
	Sort       SortOrder         `json:"sort"`
	Filters    map[string]string `json:"filters,omitempty"`
	Cursor     string            `json:"cursor"`
	Limit      int               `json:"limit"`
}

// SortOrder is the chosen sort key and direction, both empty when the plugin
// declared no sort keys.
type SortOrder struct {
	Key string `json:"key"`
	Dir string `json:"dir"`
}

// GetParams addresses one row of one collection.
type GetParams struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
}

// ActParams addresses one action. A collection or item action carries
// collection+id; a resource action carries matcher+locator instead, which is
// how "transition this ticket" fits without a sixth method.
type ActParams struct {
	Action     string            `json:"action"`
	Collection string            `json:"collection,omitempty"`
	ID         string            `json:"id,omitempty"`
	Matcher    string            `json:"matcher,omitempty"`
	Locator    string            `json:"locator,omitempty"`
	Inputs     map[string]string `json:"inputs,omitempty"`
}

// Response is the single JSON object read from a plugin's stdout. Exactly one
// of the describe shape, Resource, Page, Outcome, or Error is meaningful.
type Response struct {
	Protocol string `json:"protocol"`
	// Plugin is the plugin protocol's spelling of the identity block; Provider
	// is the resource protocol's. Both are accepted on both dialects — an
	// author who copied a resource provider's response is describing the same
	// thing under the older name — and Plugin wins when both are present.
	Plugin      *Info                  `json:"plugin,omitempty"`
	Provider    *Info                  `json:"provider,omitempty"`
	Context     []string               `json:"context,omitempty"`
	Matchers    []Matcher              `json:"matchers,omitempty"`
	Collections []WireCollection       `json:"collections,omitempty"`
	Actions     []WireAction           `json:"actions,omitempty"`
	Resource    *resource.WireDocument `json:"resource,omitempty"`
	Page        *WirePage              `json:"page,omitempty"`
	Outcome     *WireOutcome           `json:"outcome,omitempty"`
	Error       *resource.WireError    `json:"error,omitempty"`
}

// identity returns the declared identity block under either spelling.
func (r *Response) identity() *Info {
	if r.Plugin != nil {
		return r.Plugin
	}
	return r.Provider
}

// decodeResponse enforces "exactly one JSON object on stdout". Anything else —
// no JSON, unparseable JSON, a second value, or trailing garbage — is a
// transport failure, because a plugin that cannot keep its stdout clean is not
// one whose typed answers can be trusted either.
//
// wantProtocol is the identifier this instance was dispatched with. A plugin
// must answer on the dialect it was asked on, so a resource provider replying
// with the plugin identifier — or the reverse — is a protocol failure rather
// than a silent upgrade.
//
// Unknown fields are deliberately allowed through: forward compatibility is a
// protocol rule, so no decoder here may set DisallowUnknownFields.
func decodeResponse(stdout []byte, wantProtocol string) (*Response, TransportReason, string) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, ReasonMalformed, "plugin wrote nothing to stdout"
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		return nil, ReasonMalformed, "stdout was not one JSON object"
	}
	// Anything after the first value — a second object, a log line, a banner —
	// fails the invocation.
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, ReasonExtraOutput, "stdout carried more than one value"
	}
	if resp.Protocol != wantProtocol {
		return nil, ReasonProtocol, "response protocol is not " + wantProtocol
	}
	return &resp, "", ""
}

// hasDescribeShape reports whether the response carries a describe result. A
// plugin block with no matchers and no collections is legitimate — a plugin can
// be ready and currently offer nothing.
func (r *Response) hasDescribeShape() bool {
	return r.identity() != nil || len(r.Matchers) > 0 || len(r.Collections) > 0 || len(r.Actions) > 0
}

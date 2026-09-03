package pluginbrowser

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
)

// Calls is the seam between the browser and whatever owns the plugin host.
//
// Every field is a function the host supplies. Describe reads already-cached
// state and must not start anything; List, Get and Act return commands, so the
// process spawn happens inside a tea.Cmd and never on the render path.
//
// A zero Calls is usable: the browser then renders a loading card and does
// nothing, which is exactly what it should do before the host has described
// anything.
type Calls struct {
	// Describe reports the newest describe outcome for this instance. The bool
	// is false until the host has run a describe pass at all.
	Describe func(instance string) (pluginhost.Description, pluginhost.Status, bool)

	// List asks for one page. The returned command must eventually produce a
	// ListedMsg carrying the same identity fields it was given.
	List func(ListCall) tea.Cmd

	// Get expands one row into one document, producing a GotMsg.
	Get func(GetCall) tea.Cmd

	// Act performs one typed operation, producing an ActedMsg.
	Act func(ActCall) tea.Cmd

	// OpenURL opens a validated http(s) URL through the host's own confirmed
	// path. The browser validates before calling; a nil OpenURL means `o` does
	// nothing, which is what a surface with no way to open a browser wants.
	OpenURL func(url string) tea.Cmd

	// Context is the host context on offer for this surface. The host boundary
	// narrows it to the kinds the plugin declared, so offering more than a
	// plugin reads is safe. Nil means no context at all.
	Context func() *pluginhost.Context

	// Cancel drops whatever the host has in flight for a pane key, killing its
	// process group. A browser calls it for its own keys when it closes; a nil
	// Cancel means the host keeps no in-flight calls to drop.
	Cancel func(paneKey string)

	// Now is the clock relative timestamps are measured against. Nil means
	// time.Now; a test supplies its own so a timeline renders the same string
	// on every run.
	Now func() time.Time
}

// ListCall is one request for a page of one collection.
type ListCall struct {
	Instance string
	// Browser identifies the asking browser. Two browsers of one plugin — a
	// global tab and a pane showing the same collection — are two independent
	// readers with independent generations, so an answer must name which one
	// asked or the tab's page lands in the pane.
	Browser uint64
	// PaneKey identifies the asking surface. A second list for the same key
	// supersedes the first, which kills the earlier call's process group; that
	// is what makes search-as-you-type affordable with one-shot processes.
	PaneKey string
	Params  pluginhost.ListParams
	Context *pluginhost.Context

	// Generation stamps the answer so a late page cannot land after the query,
	// view, sort or collection it belonged to has moved on.
	Generation uint64
	// Append marks a page requested with a cursor, whose items extend the
	// current list rather than replacing it.
	Append bool
}

// GetCall is one request to expand a row.
type GetCall struct {
	Instance string
	Browser  uint64
	// PaneKey identifies the asking detail box. A second get for the same key
	// supersedes the first, which kills the earlier call's process group; that
	// is what makes a detail box that follows the cursor cost one process
	// rather than one per row.
	PaneKey string
	Params  pluginhost.GetParams
	Context *pluginhost.Context
	// Refresh bypasses cached freshness for this call.
	Refresh bool

	Generation uint64
}

// ActCall is one typed operation.
type ActCall struct {
	Instance string
	Browser  uint64
	Params   pluginhost.ActParams
	Context  *pluginhost.Context

	Generation uint64
}

// ListedMsg is one page answer.
type ListedMsg struct {
	Instance   string
	Browser    uint64
	Collection string
	Generation uint64
	Append     bool
	Page       pluginhost.Page
	Err        error
}

// GotMsg is one document answer.
type GotMsg struct {
	Instance   string
	Browser    uint64
	Collection string
	ID         string
	Generation uint64
	Document   resource.Document
	Err        error
}

// ActedMsg is one action answer.
type ActedMsg struct {
	Instance   string
	Browser    uint64
	Action     string
	Generation uint64
	Outcome    pluginhost.Outcome
	Err        error
}

// DescribedMsg tells every browser that a describe pass has settled, so it can
// re-read its host's cached description rather than polling for one.
//
// It is broadcast rather than addressed: a describe pass covers every
// configured instance, and a browser that is not the subject simply finds its
// own state unchanged.
type DescribedMsg struct{}

// paneKey is the identity a list call is superseded by. One collection of one
// instance in one browser is one key: two collections of the same plugin can
// legitimately be in flight at once, and cancelling one because the other moved
// would be the host inventing a conflict — and so can two BROWSERS of the same
// collection, which is why the asking browser is part of the key rather than
// having a pane cancel the tab's page out from under it.
func paneKey(browser uint64, instance, collection string) string {
	return "pluginbrowser\x00" + strconv.FormatUint(browser, 10) + "\x00" + instance + "\x00" + collection
}

// detailPaneKey is the identity a get is superseded by. The detail box shows
// one document at a time, so one browser's box is one key whatever collection
// the row came from: moving the cursor is one reader changing its mind, and the
// process answering the row it left has nothing to come back to.
func detailPaneKey(browser uint64, instance string) string {
	return "pluginbrowser-detail\x00" + strconv.FormatUint(browser, 10) + "\x00" + instance
}

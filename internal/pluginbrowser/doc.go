// Package pluginbrowser renders one protocol plugin as a list-and-detail
// browser: a collection's rows in a table with a cursor, and the document
// behind the cursor beside it.
//
// It is deliberately host-independent, in the same way internal/resourceview
// is. It knows pluginhost's vocabulary — descriptions, collections, pages,
// items, actions — and resource documents, and it knows nothing about panes,
// tmux, workspaces, projects, or which surface is showing it. A global tab and
// (from M3) a pane leaf bind the same model, so anything that behaves
// differently between them is a bug rather than a preference.
//
// It is also passive about processes. Every call to the plugin is a command the
// host supplies through Calls; the browser decides when to ask and what to do
// with the answer, and never starts a process, reads a file, or blocks inside
// Update or View.
//
// Keys are host-owned and identical for every plugin, which is the whole point
// of one browser rather than one per plugin: j/k move, Enter opens, / edits the
// query, v opens the View modal, r refreshes, a opens the action menu, o opens
// a validated sourceUrl through the host's confirmed path, c explains the
// page's outcome, and +/- move the split. `n` and Tab are deliberately not the
// browser's: the pane switcher and the app's focus ring already answer both,
// and the browser joins the ring through PaneFocusProvider and hands the key
// over through PaneFocusRingHost rather than by binding one. A plugin may suggest one
// letter for an action; it is granted only when nothing above, nothing in
// keymap.HostReservedKeys, and nothing in the surface's own bindings already
// uses it.
package pluginbrowser

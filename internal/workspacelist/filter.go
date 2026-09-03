package workspacelist

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/styles"
)

// KeyResult tells a consumer what a key did to the filter, so the list keeps
// its own navigation while the filter owns the text. It is the shared query
// field's vocabulary, aliased here because every existing consumer names it
// through this package.
type KeyResult = queryfield.KeyResult

const (
	// KeyIgnored: the filter did not want this key. Navigation keys land here
	// on purpose — arrows and ctrl+n/ctrl+p must stay live while filtering.
	KeyIgnored = queryfield.KeyIgnored
	// KeyHandled: the query or the caret moved (or a first escape cleared it).
	KeyHandled = queryfield.KeyHandled
	// KeyAccept: enter — leave the focused item selected, return to the list.
	KeyAccept = queryfield.KeyAccept
	// KeyExit: escape on an empty query — filter focus is released.
	KeyExit = queryfield.KeyExit
)

// Filter is the workspace sidebar's inline `/` query and its focus state. It is
// the shared queryfield.Field wearing this surface's placeholder and count:
// cursor movement, word forward and back, word delete, home and end, and paste
// come from the field rather than from a second implementation here.
//
// It is deliberately still a plain value: filter state is in-memory per
// consumer and is never written to config or project state.
type Filter struct {
	field queryfield.Field
}

func (f *Filter) Focused() bool { return f != nil && f.field.Focused() }

// Active reports that the filter still affects the list. A query survives
// losing focus so a user can filter, press enter, and keep working inside the
// narrowed list.
func (f *Filter) Active() bool { return f != nil && f.field.Active() }

func (f *Filter) Query() string {
	if f == nil {
		return ""
	}
	return f.field.Query()
}

func (f *Filter) SetQuery(query string) {
	if f != nil {
		f.field.SetQuery(query)
	}
}

// Focus enters filtering. `/` is the explicit entry precisely so printable
// project commands (n, D, p) keep working when it is not focused.
func (f *Filter) Focus() { f.field.Focus() }

// Blur releases focus without discarding the query.
func (f *Filter) Blur() { f.field.Blur() }

// Clear empties the query and keeps focus. It is what the row's × and the
// filter-clear command do.
func (f *Filter) Clear() { f.field.Clear() }

// Reset clears both query and focus — used when the underlying list is
// replaced wholesale (a project switch, a plugin reinit).
func (f *Filter) Reset() { f.field.Reset() }

// Insert puts typed or pasted text in at the caret. Pastes go through the same
// path as keystrokes so a pasted branch name filters exactly like a typed one.
func (f *Filter) Insert(text string) { f.field.Insert(text) }

// HandleKey applies one key while the filter has focus. It returns KeyIgnored
// for anything the list should handle itself.
func (f *Filter) HandleKey(msg tea.KeyPressMsg) KeyResult {
	if f == nil {
		return KeyIgnored
	}
	return f.field.HandleKey(msg)
}

// HandlePaste puts a bracketed paste into the query.
func (f *Filter) HandlePaste(msg tea.PasteMsg) KeyResult {
	if f == nil {
		return KeyIgnored
	}
	return f.field.HandlePaste(msg)
}

// RenderRow draws the one-line filter affordance both consumers show. It is
// shared so the project sidebar and the global browser cannot drift on what
// filtering looks like or on how counts are phrased.
//
// The rect is where the row's × clear control landed, in the row's own
// coordinates, for the host to register. It is the zero rect when there is
// nothing to clear, and a host that registers the zero rect registers nothing.
func (f *Filter) RenderRow(width, matched, total int) (string, mouse.Rect) {
	counts := ""
	if f.Active() {
		counts = styles.Muted.Render(fmt.Sprintf("%d of %d", matched, total))
	}
	return f.field.Render(width, "filter…", counts)
}

// NoMatchRow is the honest empty state for a query that matches nothing. A
// blank list would read as "nothing exists" rather than "nothing matches".
func NoMatchRow(width int, query string) string {
	text := "No workspaces match " + shortQuery(query)
	if width > 0 && ansi.StringWidth(text) > width {
		text = ansi.Truncate(text, width, "…")
	}
	return styles.Muted.Render(text)
}

func shortQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "the filter"
	}
	if len([]rune(query)) > 24 {
		query = string([]rune(query)[:24]) + "…"
	}
	return "“" + query + "”"
}

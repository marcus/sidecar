package workspacelist

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// KeyResult tells a consumer what a key did to the filter, so the list keeps
// its own navigation while the filter owns the text.
type KeyResult uint8

const (
	// KeyIgnored: the filter did not want this key. Navigation keys land here
	// on purpose — arrows and ctrl+n/ctrl+p must stay live while filtering.
	KeyIgnored KeyResult = iota
	// KeyHandled: the query changed (or a first escape cleared it).
	KeyHandled
	// KeyAccept: enter — leave the focused item selected, return to the list.
	KeyAccept
	// KeyExit: escape on an empty query — filter focus is released.
	KeyExit
)

// Filter is the inline `/` query and its focus state. It is deliberately a
// plain value: filter state is in-memory per consumer and is never written to
// config or project state.
type Filter struct {
	focused bool
	query   []rune
}

func (f *Filter) Focused() bool { return f != nil && f.focused }

// Active reports that the filter still affects the list. A query survives
// losing focus so a user can filter, press enter, and keep working inside the
// narrowed list.
func (f *Filter) Active() bool {
	return f != nil && (f.focused || len(f.query) > 0)
}

func (f *Filter) Query() string {
	if f == nil {
		return ""
	}
	return string(f.query)
}

func (f *Filter) SetQuery(query string) { f.query = []rune(query) }

// Focus enters filtering. `/` is the explicit entry precisely so printable
// project commands (n, D, p) keep working when it is not focused.
func (f *Filter) Focus() { f.focused = true }

// Blur releases focus without discarding the query.
func (f *Filter) Blur() { f.focused = false }

// Reset clears both query and focus — used when the underlying list is
// replaced wholesale (a project switch, a plugin reinit).
func (f *Filter) Reset() {
	f.focused = false
	f.query = nil
}

// Insert appends typed or pasted text. Pastes go through the same path as
// keystrokes so a pasted branch name filters exactly like a typed one.
func (f *Filter) Insert(text string) {
	if text == "" {
		return
	}
	f.query = append(f.query, []rune(text)...)
}

// HandleKey applies one key while the filter has focus. It returns KeyIgnored
// for anything the list should handle itself.
func (f *Filter) HandleKey(key, text string) KeyResult {
	if f == nil || !f.focused {
		return KeyIgnored
	}
	switch key {
	case "esc":
		// First escape clears the query, second exits filter focus.
		if len(f.query) > 0 {
			f.query = nil
			return KeyHandled
		}
		f.focused = false
		return KeyExit
	case "enter":
		f.focused = false
		return KeyAccept
	case "backspace":
		if len(f.query) > 0 {
			f.query = f.query[:len(f.query)-1]
		}
		return KeyHandled
	case "ctrl+u":
		f.query = nil
		return KeyHandled
	case "up", "down", "ctrl+n", "ctrl+p", "pgup", "pgdown", "home", "end", "tab", "shift+tab":
		return KeyIgnored
	case "space":
		f.Insert(" ")
		return KeyHandled
	}
	if text != "" && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") {
		f.Insert(text)
		return KeyHandled
	}
	return KeyIgnored
}

// QueryRow is one query bar's whole content: what has been typed, whether the
// row is taking text, the placeholder to show when it is not, and an
// already-styled right-aligned cell.
//
// It exists because the app has more than one query bar and only one look for
// them. Filter owns the workspace sidebar's state; a surface whose query lives
// somewhere else — the plugin browser's per-collection state, say — renders
// through RenderQueryRow directly rather than imitating this row.
type QueryRow struct {
	Query   string
	Focused bool
	// Placeholder replaces the query while it is empty and the row is idle.
	Placeholder string
	// Right is a rendered cell pinned to the right edge: a count, an outcome,
	// or nothing. It is already styled, because only the caller knows what the
	// number it is reporting means.
	Right string
}

// RenderQueryRow draws the app's query bar: the `/` prompt and the text in
// styles.Muted while idle and styles.Title while taking text, a ▌ block caret
// on the focused row, and the right cell pinned to the far edge.
func RenderQueryRow(width int, row QueryRow) string {
	if width < 1 {
		return ""
	}
	prompt := "/ "
	body := row.Query
	if body == "" {
		body = row.Placeholder
	}
	left := prompt + body
	if row.Focused {
		left = prompt + row.Query + "▌"
	}
	rightW := ansi.StringWidth(row.Right)
	gap := width - ansi.StringWidth(left) - rightW
	if gap < 1 {
		left = ansi.Truncate(left, max(1, width-rightW-1), "…")
		gap = max(1, width-ansi.StringWidth(left)-rightW)
	}
	style := styles.Muted
	if row.Focused {
		style = styles.Title
	}
	out := style.Render(left) + strings.Repeat(" ", gap)
	if row.Right != "" {
		out += row.Right
	}
	return out
}

// RenderRow draws the one-line filter affordance both consumers show. It is
// shared so the project sidebar and the global browser cannot drift on what
// filtering looks like or on how counts are phrased.
func (f *Filter) RenderRow(width, matched, total int) string {
	counts := ""
	if f.Active() {
		counts = styles.Muted.Render(fmt.Sprintf("%d of %d", matched, total))
	}
	return RenderQueryRow(width, QueryRow{
		Query:       f.Query(),
		Focused:     f.Focused(),
		Placeholder: "filter…",
		Right:       counts,
	})
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

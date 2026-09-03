// Package queryfield is the app's one inline query bar: its state, its keys,
// and its look.
//
// Every `/` bar in sidecar used to be hand-rolled from append and backspace, so
// the caret could not move, a word could not be deleted, and a paste arrived
// character by character or not at all. This package answers that once by
// wrapping a bubbles textinput.Model — which brings cursor movement, word
// forward and back, word delete, home and end, and paste with no code of its
// own — behind the row RenderRow draws.
//
// It lives outside internal/workspacelist because the surfaces that need it
// have nothing to do with workspaces: the plugin browser, the file browser, the
// git history, the notes search, Sessions and the terminal all want the same
// field, and none of them should import a workspace list to get a text box.
// workspacelist.Filter is now a thin wrapper over the type here.
package queryfield

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
)

// KeyResult tells a consumer what a key did to the field, so the list beneath
// keeps its own navigation while the field owns the text.
type KeyResult uint8

const (
	// KeyIgnored: the field did not want this key. Navigation keys land here on
	// purpose — arrows, paging and ctrl+n/ctrl+p must stay live while a query
	// is being typed.
	KeyIgnored KeyResult = iota
	// KeyHandled: the query or the caret moved (or a first escape cleared it).
	KeyHandled
	// KeyAccept: enter — leave the focused item selected, return to the list.
	KeyAccept
	// KeyExit: escape on an empty query — field focus is released.
	KeyExit
)

// ClearGlyph is the row's clear control. It is drawn only when there is
// something to clear, and it is a hit target wherever it is drawn.
const ClearGlyph = "×"

// clearCellWidth is the control's whole cell: one column of separation from
// whatever is left of it, and the glyph. Two columns is a target a pointer can
// actually land on without stealing a column from the count beside it.
const clearCellWidth = 2

// unclaimedKeys are the keys the field never takes, whatever the text input
// would do with them. They are the list's: a user types, arrows onto a match
// and presses enter without ever leaving the query. `tab` is the app's focus
// ring, which no text field may swallow.
//
// This is why suggestions stay off. textinput binds `tab`, `up`/`ctrl+p` and
// `down`/`ctrl+n` to its suggestion cycle, and a field that accepted them would
// take exactly the keys the surface underneath needs.
var unclaimedKeys = map[string]bool{
	"up": true, "down": true,
	"pgup": true, "pgdown": true,
	"ctrl+n": true, "ctrl+p": true,
	"tab": true, "shift+tab": true,
}

// Field is one query bar: the text, the caret, and whether the row owns the
// keyboard. It is a plain value with a usable zero — query state is in-memory
// per consumer and is never written to config or project state, and a surface
// that holds one as a struct field must not have to remember a constructor.
type Field struct {
	input   textinput.Model
	ready   bool
	focused bool
}

// New builds a field. The zero value works too; this exists for a caller that
// would rather say so.
func New() Field {
	var f Field
	f.init()
	return f
}

// init installs the text input on first use. Everything sidecar changes about
// the default key map is here, so there is one place to read what this field
// does and does not take.
func (f *Field) init() {
	if f.ready {
		return
	}
	f.ready = true
	f.input = textinput.New()
	f.input.Prompt = ""
	f.input.ShowSuggestions = false
	// The row draws its own ▌ caret through RenderRow, so the input's own
	// cursor is never rendered and must not ask for a blink command.
	f.input.SetVirtualCursor(false)

	km := textinput.DefaultKeyMap()
	// ctrl+a is select-all in internal/textselect, on every surface that has
	// selectable text. A query bar that stole it for line-start would make the
	// one chord that means "select everything" mean two different things
	// depending on which row had focus.
	km.LineStart = key.NewBinding(key.WithKeys("home"))
	// ctrl+u clears the whole query here, as it does in every sidecar query bar
	// today; textinput's own binding deletes only what is before the caret.
	km.DeleteBeforeCursor = key.NewBinding()
	// ctrl+v would read the system clipboard through a second path. Bubble Tea
	// already delivers a bracketed paste as tea.PasteMsg, which is what
	// HandlePaste takes.
	km.Paste = key.NewBinding()
	// Suggestions are off, so their keys belong to the list beneath.
	km.AcceptSuggestion = key.NewBinding()
	km.NextSuggestion = key.NewBinding()
	km.PrevSuggestion = key.NewBinding()
	f.input.KeyMap = km
	f.input.Focus()
}

// editingKeys are the bindings the field keeps. A key that matches one of them
// is handled even when it changed nothing — backspace on an empty query is
// still the query's key, not the list's.
func (f *Field) editingKeys() []key.Binding {
	km := f.input.KeyMap
	return []key.Binding{
		km.CharacterForward, km.CharacterBackward,
		km.WordForward, km.WordBackward,
		km.DeleteWordBackward, km.DeleteWordForward,
		km.DeleteAfterCursor, km.DeleteCharacterBackward, km.DeleteCharacterForward,
		km.LineStart, km.LineEnd,
	}
}

// Focused reports whether the field owns the keyboard.
func (f *Field) Focused() bool { return f != nil && f.focused }

// Active reports that the field still affects the list. A query survives losing
// focus so a user can filter, press enter, and keep working inside the narrowed
// list.
func (f *Field) Active() bool {
	return f != nil && (f.focused || f.Query() != "")
}

// Query is the text.
func (f *Field) Query() string {
	if f == nil || !f.ready {
		return ""
	}
	return f.input.Value()
}

// SetQuery replaces the text and puts the caret at its end, which is where a
// restored or programmatically set query leaves it.
func (f *Field) SetQuery(query string) {
	if f == nil {
		return
	}
	f.init()
	f.input.SetValue(query)
	f.input.CursorEnd()
}

// Cursor is the caret's rune offset into the query.
func (f *Field) Cursor() int {
	if f == nil || !f.ready {
		return 0
	}
	return f.input.Position()
}

// Focus enters editing. `/` is the explicit entry precisely so printable
// commands keep working when the field is not focused.
func (f *Field) Focus() {
	if f == nil {
		return
	}
	f.init()
	f.focused = true
}

// Blur releases focus without discarding the query.
func (f *Field) Blur() {
	if f != nil {
		f.focused = false
	}
}

// Clear empties the query and leaves focus where it is.
func (f *Field) Clear() {
	if f == nil {
		return
	}
	f.init()
	f.input.SetValue("")
	f.input.CursorStart()
}

// Reset clears both query and focus — used when the underlying list is replaced
// wholesale (a project switch, a plugin reinit).
func (f *Field) Reset() {
	if f == nil {
		return
	}
	f.Clear()
	f.focused = false
}

// Insert puts text in at the caret. Typed keys and pasted text go through the
// same path, so a pasted branch name filters exactly like a typed one.
func (f *Field) Insert(text string) {
	if f == nil || text == "" {
		return
	}
	f.init()
	f.input, _ = f.input.Update(tea.PasteMsg{Content: text})
}

// HandlePaste puts a bracketed paste into the query. Newlines and tabs are
// collapsed by the input's own sanitizer, because a query bar is one line.
func (f *Field) HandlePaste(msg tea.PasteMsg) KeyResult {
	if f == nil || !f.focused || msg.Content == "" {
		return KeyIgnored
	}
	f.Insert(msg.Content)
	return KeyHandled
}

// HandleKey applies one key while the field has focus. It returns KeyIgnored
// for anything the list beneath should handle itself.
func (f *Field) HandleKey(msg tea.KeyPressMsg) KeyResult {
	if f == nil || !f.focused {
		return KeyIgnored
	}
	f.init()
	k := msg.String()
	switch {
	case unclaimedKeys[k]:
		return KeyIgnored
	case k == "esc":
		// First escape clears the query, second releases focus.
		if f.Query() != "" {
			f.Clear()
			return KeyHandled
		}
		f.focused = false
		return KeyExit
	case k == "enter":
		f.focused = false
		return KeyAccept
	case k == "ctrl+u":
		f.Clear()
		return KeyHandled
	}

	before, beforePos := f.input.Value(), f.input.Position()
	// The command is the input's cursor blink, which this row never draws.
	f.input, _ = f.input.Update(msg)
	if f.input.Value() != before || f.input.Position() != beforePos {
		return KeyHandled
	}
	if key.Matches(msg, f.editingKeys()...) {
		return KeyHandled
	}
	// A printable key that changed nothing is still the field's: a rune the
	// input refused is not a key the list should act on.
	if msg.Text != "" && !strings.HasPrefix(k, "ctrl+") && !strings.HasPrefix(k, "alt+") {
		return KeyHandled
	}
	return KeyIgnored
}

// Row is one query bar's whole content: what has been typed, where the caret
// is, whether the row is taking text, the placeholder to show when it is not,
// and an already-styled right-aligned cell.
//
// It exists because the app has more than one query bar and only one look for
// them. A surface whose query lives somewhere else — the plugin browser's
// per-collection state, say — renders through RenderRow directly rather than
// imitating it.
type Row struct {
	Query   string
	Cursor  int
	Focused bool
	// Placeholder replaces the query while it is empty and the row is idle.
	Placeholder string
	// Right is a rendered cell pinned to the right edge: a count, an outcome,
	// or nothing. It is already styled, because only the caller knows what the
	// number it is reporting means.
	Right string
	// Clearable draws the × control right of everything else. A caller sets it
	// when there is something to clear AND it will register the rect RenderRow
	// hands back: a control nothing listens to is worse than no control.
	Clearable bool
}

// RenderRow draws the app's query bar: the `/` prompt and the text in
// styles.Muted while idle and styles.Title while taking text, a ▌ block caret
// at the cursor on the focused row, the right cell pinned to the far edge, and
// the × clear control right of that.
//
// The second return is where the × landed, in the row's own coordinates, so the
// host can register it. It is the zero rect whenever the control is not drawn —
// there is nothing to clear, the caller did not ask for it, or the row was too
// narrow to hold it — and a host that registers the zero rect registers nothing.
func RenderRow(width int, row Row) (string, mouse.Rect) {
	if width < 1 {
		return "", mouse.Rect{}
	}
	prompt := "/ "
	body := row.Query
	if body == "" {
		body = row.Placeholder
	}
	left := prompt + body
	if row.Focused {
		left = prompt + withCaret(row.Query, row.Cursor)
	}

	right := row.Right
	clear := mouse.Rect{}
	if row.Clearable && row.Query != "" {
		if cell := clearCell(); ansi.StringWidth(right)+clearCellWidth < width {
			right += cell
			clear = mouse.Rect{X: width - clearCellWidth, W: clearCellWidth, H: 1}
		}
	}

	rightW := ansi.StringWidth(right)
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
	if right != "" {
		out += right
	}
	return out, clear
}

// clearCell is the × and the column of air that separates it from the count.
func clearCell() string { return " " + styles.Muted.Render(ClearGlyph) }

// withCaret puts the ▌ block caret at the cursor rather than always at the end,
// so a caret that moved — word back, home, a click into the middle of a word —
// is visible where it actually is.
func withCaret(query string, cursor int) string {
	runes := []rune(query)
	if cursor < 0 || cursor > len(runes) {
		cursor = len(runes)
	}
	return string(runes[:cursor]) + "▌" + string(runes[cursor:])
}

// Render draws this field's row, with the caller's right-hand cell and the ×
// wherever there is a query to clear. It is the call a surface makes; RenderRow
// is for a surface whose query state is not a Field.
func (f *Field) Render(width int, placeholder, right string) (string, mouse.Rect) {
	return RenderRow(width, Row{
		Query:       f.Query(),
		Cursor:      f.Cursor(),
		Focused:     f.Focused(),
		Placeholder: placeholder,
		Right:       right,
		Clearable:   true,
	})
}

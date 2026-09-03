package queryfield

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// M4d-a: the app has one query bar, and everything a text field is expected to
// do — word ops, home and end, a caret that moves, paste — is the field's, not
// each surface's.

func press(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "alt+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	case "alt+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func typeInto(t *testing.T, f *Field, text string) {
	t.Helper()
	for _, r := range text {
		if got := f.HandleKey(press(string(r))); got != KeyHandled {
			t.Fatalf("typing %q returned %v, want KeyHandled", r, got)
		}
	}
}

// The whole reason the field wraps a textinput: a word delete is one key, not
// six backspaces, and it leaves the caret where the word was.
func TestWordDeleteRemovesOneWord(t *testing.T) {
	for _, chord := range []string{"alt+backspace", "ctrl+w"} {
		t.Run(chord, func(t *testing.T) {
			var f Field
			f.Focus()
			typeInto(t, &f, "plugin browser")
			if got := f.HandleKey(press(chord)); got != KeyHandled {
				t.Fatalf("%s returned %v", chord, got)
			}
			if f.Query() != "plugin " {
				t.Fatalf("query = %q, want the last word deleted", f.Query())
			}
			if f.Cursor() != len("plugin ") {
				t.Fatalf("cursor = %d, want %d", f.Cursor(), len("plugin "))
			}
		})
	}
}

// A word delete in the middle of the text is what the visual proof shows: the
// caret stays mid-string and the tail is untouched.
func TestWordDeleteMidStringKeepsTheTail(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "schema notes")
	for range "notes" {
		f.HandleKey(press("left"))
	}
	if got := f.HandleKey(press("alt+backspace")); got != KeyHandled {
		t.Fatalf("alt+backspace returned %v", got)
	}
	if f.Query() != "notes" || f.Cursor() != 0 {
		t.Fatalf("query = %q cursor = %d, want %q at 0", f.Query(), f.Cursor(), "notes")
	}
}

// Cursor movement, home and end come from the text input, so the field never
// implements them.
func TestCaretMovesWithoutTouchingTheText(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "alpha beta")
	if got := f.HandleKey(press("home")); got != KeyHandled || f.Cursor() != 0 {
		t.Fatalf("home = %v cursor = %d", got, f.Cursor())
	}
	if got := f.HandleKey(press("end")); got != KeyHandled || f.Cursor() != len("alpha beta") {
		t.Fatalf("end = %v cursor = %d", got, f.Cursor())
	}
	if got := f.HandleKey(press("alt+left")); got != KeyHandled || f.Cursor() != len("alpha ") {
		t.Fatalf("word back = %v cursor = %d", got, f.Cursor())
	}
	if f.Query() != "alpha beta" {
		t.Fatalf("moving the caret changed the text: %q", f.Query())
	}
}

// A bracketed paste lands at the caret, whole, with its newlines flattened —
// a query bar is one line.
func TestPasteLandsAtTheCaret(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "ab")
	f.HandleKey(press("left"))
	if got := f.HandlePaste(tea.PasteMsg{Content: "XY"}); got != KeyHandled {
		t.Fatalf("paste returned %v", got)
	}
	if f.Query() != "aXYb" {
		t.Fatalf("query = %q, want the paste at the caret", f.Query())
	}
	f.Clear()
	f.HandlePaste(tea.PasteMsg{Content: "two\nlines"})
	if strings.Contains(f.Query(), "\n") {
		t.Fatalf("a newline survived the paste: %q", f.Query())
	}
	if f.Focused() {
		// Sanity: a paste never changes focus.
		return
	}
	t.Fatal("paste blurred the field")
}

// ctrl+a is select-all everywhere text can be selected. The field must not
// take it for line-start, and must not swallow it either: the surface
// underneath is the one that answers it.
func TestCtrlADoesNothingInTheField(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "abc")
	before := f.Cursor()
	if got := f.HandleKey(press("ctrl+a")); got != KeyIgnored {
		t.Fatalf("ctrl+a = %v, want KeyIgnored", got)
	}
	if f.Cursor() != before || f.Query() != "abc" {
		t.Fatalf("ctrl+a moved the caret or the text: %q at %d", f.Query(), f.Cursor())
	}
	// ctrl+e is still the field's, so this is a refusal of one chord rather
	// than of every control key.
	if got := f.HandleKey(press("home")); got != KeyHandled || f.Cursor() != 0 {
		t.Fatalf("home = %v cursor = %d", got, f.Cursor())
	}
	if got := f.HandleKey(press("ctrl+e")); got != KeyHandled || f.Cursor() != 3 {
		t.Fatalf("ctrl+e = %v cursor = %d", got, f.Cursor())
	}
}

// The list beneath keeps its navigation while the field has the keyboard.
func TestNavigationKeysAreLeftToTheList(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "abc")
	for _, k := range []string{"up", "down", "pgup", "pgdown", "ctrl+n", "ctrl+p", "tab"} {
		if got := f.HandleKey(press(k)); got != KeyIgnored {
			t.Fatalf("%s = %v, want KeyIgnored", k, got)
		}
		if f.Query() != "abc" {
			t.Fatalf("%s changed the query: %q", k, f.Query())
		}
	}
}

// Esc clears, then blurs. Enter accepts and keeps the query. ctrl+u clears.
func TestEscapeEnterAndCtrlU(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "abc")
	if got := f.HandleKey(press("esc")); got != KeyHandled || f.Query() != "" || !f.Focused() {
		t.Fatalf("first esc = %v query=%q focused=%v", got, f.Query(), f.Focused())
	}
	if got := f.HandleKey(press("esc")); got != KeyExit || f.Focused() {
		t.Fatalf("second esc = %v focused=%v", got, f.Focused())
	}
	f.Focus()
	typeInto(t, &f, "abc")
	if got := f.HandleKey(press("enter")); got != KeyAccept || f.Focused() || f.Query() != "abc" {
		t.Fatalf("enter = %v focused=%v query=%q", got, f.Focused(), f.Query())
	}
	f.Focus()
	if got := f.HandleKey(press("ctrl+u")); got != KeyHandled || f.Query() != "" {
		t.Fatalf("ctrl+u = %v query=%q", got, f.Query())
	}
	// A blurred field is not a text input: every key belongs to the surface.
	f.Blur()
	if got := f.HandleKey(press("a")); got != KeyIgnored || f.Query() != "" {
		t.Fatalf("a blurred field took a key: %v %q", got, f.Query())
	}
}

// The × is a control only where there is something to clear, and the rect it
// reports is the whole of what a host registers.
func TestClearCellIsDrawnAndRegisteredOnlyWhenThereIsAQuery(t *testing.T) {
	var f Field
	f.Focus()
	row, clear := f.Render(60, "filter…", "")
	if strings.Contains(row, ClearGlyph) || clear.W != 0 {
		t.Fatalf("an empty query drew a clear control: %q rect=%+v", row, clear)
	}
	typeInto(t, &f, "abc")
	row, clear = f.Render(60, "filter…", "")
	if !strings.Contains(row, ClearGlyph) {
		t.Fatalf("a non-empty query drew no clear control: %q", row)
	}
	if clear.W != clearCellWidth || clear.X != 60-clearCellWidth || clear.H != 1 {
		t.Fatalf("clear rect = %+v", clear)
	}
	// The glyph is inside the rect the host was handed.
	plain := []rune(ansi.Strip(row))
	if got := string(plain[clear.X : clear.X+clear.W]); !strings.Contains(got, ClearGlyph) {
		t.Fatalf("the rect %+v covers %q, not the ×", clear, got)
	}
	f.Clear()
	if _, clear = f.Render(60, "filter…", ""); clear.W != 0 {
		t.Fatal("clearing the query left the × registered")
	}
}

// A row too narrow for the control drops it rather than overprinting the count,
// and drops its hit rect with it.
func TestClearCellIsDroppedOnANarrowRow(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "abc")
	row, clear := f.Render(6, "filter…", "500 of 550")
	if clear.W != 0 || strings.Contains(ansi.Strip(row), ClearGlyph) {
		t.Fatalf("a narrow row kept the ×: %q rect=%+v", ansi.Strip(row), clear)
	}
}

// The caret is drawn where the caret is, not always at the end.
func TestCaretIsDrawnAtTheCursor(t *testing.T) {
	var f Field
	f.Focus()
	typeInto(t, &f, "beta")
	f.HandleKey(press("alt+backspace"))
	typeInto(t, &f, "alpha beta")
	for range "beta" {
		f.HandleKey(press("left"))
	}
	row, _ := f.Render(40, "filter…", "")
	if !strings.Contains(ansi.Strip(row), "alpha ▌beta") {
		t.Fatalf("the caret is not at the cursor: %q", ansi.Strip(row))
	}
}

// The idle row is the placeholder, and it never wears a caret.
func TestIdleRowShowsThePlaceholder(t *testing.T) {
	var f Field
	row, clear := f.Render(40, "filter…", "")
	plain := ansi.Strip(row)
	if !strings.Contains(plain, "/ filter…") || strings.Contains(plain, "▌") || clear.W != 0 {
		t.Fatalf("idle row = %q rect=%+v", plain, clear)
	}
}

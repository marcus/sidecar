package docview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/ui"
)

// newSearchModel is a raw, line-numbered document of exactly the given lines,
// which is the shape every search assertion below is written against.
func newSearchModel(t *testing.T, width, height int, lines ...string) *Model {
	t.Helper()
	m := newTestModel(t)
	m.loading = false
	m.rendered = false
	m.result.Content = strings.Join(lines, "\n")
	m.result.HighlightedLines = lines
	m.SetSize(width, height)
	return m
}

// typeSearch opens search and types a query one key at a time, the way a user
// does, so the incremental re-match on every keystroke is what is being tested.
func typeSearch(m *Model, query string) {
	m.StartSearch()
	for _, r := range query {
		m.HandleSearchKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func pressSearch(m *Model, key string) (bool, tea.Cmd) {
	switch key {
	case "enter":
		return m.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	case "esc":
		return m.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	case "backspace":
		return m.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	default:
		r := []rune(key)[0]
		return m.HandleSearchKey(tea.KeyPressMsg{Code: r, Text: key})
	}
}

func matchStrings(matches []Match) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, fmt.Sprintf("%d:%d-%d", m.Line, m.StartCol, m.EndCol))
	}
	return out
}

func TestSearchMatchesAreCaseInsensitiveAndOverlapping(t *testing.T) {
	m := newSearchModel(t, 40, 10, "Alpha alpha", "nothing here", "aaa")
	typeSearch(m, "aa")

	got := matchStrings(m.SearchMatches())
	want := []string{"2:0-2", "2:1-3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("matches for \"aa\" = %v, want %v", got, want)
	}

	m.CloseSearch()
	typeSearch(m, "ALPHA")
	got = matchStrings(m.SearchMatches())
	want = []string{"0:0-5", "0:6-11"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("matches for \"ALPHA\" = %v, want %v (case-insensitive)", got, want)
	}
}

func TestSearchPhaseTransitions(t *testing.T) {
	m := newSearchModel(t, 40, 10, "needle one", "needle two")

	if m.SearchActive() {
		t.Fatal("search reported active before it was opened")
	}
	typeSearch(m, "need")
	if !m.SearchActive() || m.SearchCommitted() {
		t.Fatalf("after typing: active=%v committed=%v, want a live uncommitted search", m.SearchActive(), m.SearchCommitted())
	}
	if m.SearchQuery() != "need" {
		t.Errorf("query = %q, want %q", m.SearchQuery(), "need")
	}

	// n is query text while typing, not navigation.
	pressSearch(m, "n")
	if m.SearchQuery() != "needn" {
		t.Errorf("query after typing n = %q, want it appended, not treated as navigate", m.SearchQuery())
	}
	pressSearch(m, "backspace")
	if m.SearchQuery() != "need" {
		t.Errorf("query after backspace = %q, want %q", m.SearchQuery(), "need")
	}

	pressSearch(m, "enter")
	if !m.SearchCommitted() {
		t.Fatal("enter did not commit the query")
	}
	if handled, _ := pressSearch(m, "n"); !handled {
		t.Error("a committed search declined a key it owns")
	}

	pressSearch(m, "esc")
	if m.SearchActive() || m.SearchQuery() != "" || len(m.SearchMatches()) != 0 {
		t.Errorf("after esc: active=%v query=%q matches=%d, want the search gone",
			m.SearchActive(), m.SearchQuery(), len(m.SearchMatches()))
	}
	if handled, _ := m.HandleSearchKey(tea.KeyPressMsg{Code: 'x'}); handled {
		t.Error("a closed search consumed a key")
	}
}

func TestSearchEnterOnEmptyQueryDoesNotCommit(t *testing.T) {
	m := newSearchModel(t, 40, 10, "alpha")
	m.StartSearch()
	pressSearch(m, "enter")
	if m.SearchCommitted() {
		t.Error("an empty query committed")
	}
}

func TestSearchNextAndPreviousWrapAround(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "filler"
	}
	lines[1], lines[20], lines[38] = "hit one", "hit two", "hit three"
	m := newSearchModel(t, 40, 10, lines...)

	typeSearch(m, "hit")
	pressSearch(m, "enter")
	if got := len(m.SearchMatches()); got != 3 {
		t.Fatalf("matches = %d, want 3", got)
	}
	if got := m.SearchMatchIndex(); got != 0 {
		t.Fatalf("cursor = %d, want the first match", got)
	}

	pressSearch(m, "n")
	if got := m.SearchMatchIndex(); got != 1 {
		t.Fatalf("cursor after n = %d, want 1", got)
	}
	if m.ScrollOffset() == 0 {
		t.Error("jumping to an off-screen match did not scroll the document")
	}
	pressSearch(m, "n")
	pressSearch(m, "n")
	if got := m.SearchMatchIndex(); got != 0 {
		t.Errorf("cursor after wrapping forward = %d, want 0", got)
	}
	pressSearch(m, "N")
	if got := m.SearchMatchIndex(); got != 2 {
		t.Errorf("cursor after wrapping backward = %d, want the last match", got)
	}
}

func TestSearchScrollKeysStillScrollWhileCommitted(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d hit", i)
	}
	m := newSearchModel(t, 40, 10, lines...)
	typeSearch(m, "hit")
	pressSearch(m, "enter")

	before := m.ScrollOffset()
	pressSearch(m, "j")
	if m.ScrollOffset() != before+1 {
		t.Errorf("scroll after j = %d, want %d", m.ScrollOffset(), before+1)
	}
	pressSearch(m, "k")
	if m.ScrollOffset() != before {
		t.Errorf("scroll after k = %d, want %d", m.ScrollOffset(), before)
	}
}

// searchHighlightColumns reports the screen columns a highlight prefix opens at
// on a rendered row, so a test can say where the paint landed rather than only
// that it happened.
func searchHighlightColumns(row, prefix string) []int {
	var cols []int
	for offset := 0; ; {
		idx := strings.Index(row[offset:], prefix)
		if idx < 0 || prefix == "" {
			return cols
		}
		at := offset + idx
		cols = append(cols, ansi.StringWidth(ansi.Strip(row[:at])))
		offset = at + len(prefix)
	}
}

func TestSearchHighlightsTabExpandedColumns(t *testing.T) {
	m := newSearchModel(t, 40, 4, "\tneedle")
	typeSearch(m, "needle")

	row := strings.Split(m.View(), "\n")[0]
	cols := searchHighlightColumns(row, searchMatchCurrentPrefix())
	if len(cols) != 1 {
		t.Fatalf("highlights on the row = %v, want exactly one", cols)
	}
	// The gutter is drawn in front of the text and the tab stop is measured
	// from the pane's left edge, so the word starts at the next stop past the
	// gutter — never at column 1, which is where a naive byte offset would put
	// it.
	if cols[0] != tabStopWidth {
		t.Errorf("highlight column = %d, want the tab stop at %d", cols[0], tabStopWidth)
	}
	if !strings.Contains(ansi.Strip(row), "needle") {
		t.Errorf("row = %q, want the matched text still drawn", ansi.Strip(row))
	}
}

func TestSearchHighlightsBothRowsOfAWrappedMatch(t *testing.T) {
	// A width narrow enough that the line wraps mid-word, so the match straddles
	// the boundary and must paint on the row it starts on and the row it ends on.
	m := newSearchModel(t, 12, 6, "placeholder")
	m.SetWrap(true)
	textWidth := 12 - m.display().gutterWidth
	if textWidth < 8 {
		t.Fatalf("text width = %d, too narrow to split a word across rows", textWidth)
	}
	// Put the word across the break: the row ends three letters into "needle".
	line := strings.Repeat("a", textWidth-3) + "needle" + "aaa"
	m.result.HighlightedLines = []string{line}
	m.result.Content = line
	m.invalidateRender()
	typeSearch(m, "needle")

	if got := len(m.SearchMatches()); got != 1 {
		t.Fatalf("matches = %d, want the wrapped match found once", got)
	}
	rows := strings.Split(m.View(), "\n")
	painted := 0
	for _, row := range rows {
		if len(searchHighlightColumns(row, searchMatchCurrentPrefix())) > 0 {
			painted++
		}
	}
	if painted != 2 {
		t.Errorf("rows carrying the highlight = %d, want 2 (the match straddles the wrap)", painted)
	}
}

func TestSearchKeepsTheCursorOnAMatchAcrossAWrapToggle(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "short"
	}
	lines[3] = strings.Repeat("x", 60) + " hit " + strings.Repeat("y", 60)
	lines[25] = "hit again"
	m := newSearchModel(t, 20, 8, lines...)

	typeSearch(m, "hit")
	pressSearch(m, "enter")
	pressSearch(m, "n")
	before := m.SearchMatchIndex()

	m.ToggleWrap()
	if got := m.SearchMatchIndex(); got != before {
		t.Errorf("cursor after toggling wrap = %d, want it held at %d", got, before)
	}
	if got := len(m.SearchMatches()); got != 2 {
		t.Errorf("matches after toggling wrap = %d, want both still found", got)
	}
	// The match must still be reachable on screen in the new layout.
	m.scrollToMatch()
	row := m.matchRow(m.SearchMatches()[m.SearchMatchIndex()])
	if row < m.ScrollOffset() || row >= m.ScrollOffset()+m.contentHeight() {
		t.Errorf("match row %d is outside the viewport [%d,%d)",
			row, m.ScrollOffset(), m.ScrollOffset()+m.contentHeight())
	}
}

func TestSearchHighlightPreservesExistingANSIStyling(t *testing.T) {
	styled := "\x1b[31mred needle red\x1b[0m"
	m := newSearchModel(t, 40, 4, styled)
	typeSearch(m, "needle")

	row := strings.Split(m.View(), "\n")[0]
	if !strings.Contains(row, "\x1b[31m") {
		t.Errorf("row = %q, want the line's own colour still present", row)
	}
	if len(searchHighlightColumns(row, searchMatchCurrentPrefix())) != 1 {
		t.Errorf("row = %q, want the match highlighted inside the styled text", row)
	}
	if got := ansi.Strip(row); !strings.HasPrefix(got, "1 red needle red") && !strings.Contains(got, "red needle red") {
		t.Errorf("visible text = %q, want it unchanged by the injection", got)
	}
}

func TestSearchHighlightRestoresTheRowsStylingAfterAMatch(t *testing.T) {
	// The highlight is closed with a full reset, so whatever colour the row had
	// open before the match has to be put back or the tail of a syntax
	// highlighted line goes plain.
	styled := "\x1b[31mred needle red\x1b[0m"
	m := newSearchModel(t, 40, 4, styled)
	typeSearch(m, "needle")

	row := strings.Split(m.View(), "\n")[0]
	after := row[strings.Index(row, searchMatchCurrentPrefix())+len(searchMatchCurrentPrefix()):]
	reset := strings.Index(after, "\x1b[0m")
	if reset < 0 {
		t.Fatalf("row = %q, want the highlight closed", row)
	}
	if !strings.HasPrefix(after[reset+len("\x1b[0m"):], "\x1b[31m") {
		t.Errorf("after the highlight = %q, want the row's own colour restored", after[reset:])
	}
}

func TestSearchHighlightsOverlappingMatches(t *testing.T) {
	m := newSearchModel(t, 40, 4, "aaa")
	typeSearch(m, "aa")
	if got := len(m.SearchMatches()); got != 2 {
		t.Fatalf("matches = %d, want 2 overlapping hits", got)
	}
	row := strings.Split(m.View(), "\n")[0]
	// Both matches paint: the current one from column 0, the second one picking
	// up where the first closed rather than being dropped.
	if got := len(searchHighlightColumns(row, searchMatchCurrentPrefix())); got != 1 {
		t.Errorf("current-match highlights = %d, want 1 (row %q)", got, row)
	}
	if got := len(searchHighlightColumns(row, searchMatchPrefix())); got != 1 {
		t.Errorf("second-match highlights = %d, want the overlapped match still painted (row %q)", got, row)
	}
}

func TestSearchCurrentMatchIsStyledDifferentlyFromTheRest(t *testing.T) {
	m := newSearchModel(t, 40, 4, "hit and hit")
	typeSearch(m, "hit")
	pressSearch(m, "enter")

	row := strings.Split(m.View(), "\n")[0]
	if got := len(searchHighlightColumns(row, searchMatchCurrentPrefix())); got != 1 {
		t.Errorf("current-match highlights = %d, want exactly the one under the cursor", got)
	}
	if got := len(searchHighlightColumns(row, searchMatchPrefix())); got != 1 {
		t.Errorf("other-match highlights = %d, want the second match painted plainly", got)
	}
}

func TestSelectionWinsOverASearchMatchOnTheSameRow(t *testing.T) {
	m := newSearchModel(t, 40, 6, "alpha needle beta", "second needle")
	m.SetOrigin(0, 0)
	m.SetSelection(textselect.Keys{Copy: "alt+c"}, false)
	typeSearch(m, "needle")

	gutter := m.display().gutterWidth
	m.HandleSelectionMouse(mouse.MouseAction{Type: mouse.ActionClick, X: gutter, Y: 0})
	m.HandleSelectionMouse(mouse.MouseAction{Type: mouse.ActionDrag, X: gutter + 17, Y: 0})
	if !m.HasSelection() {
		t.Fatal("the drag left no selection to test precedence against")
	}

	rows := strings.Split(m.View(), "\n")
	if len(searchHighlightColumns(rows[0], searchMatchCurrentPrefix())) != 0 {
		t.Errorf("row under the selection = %q, want the selection to win over the match", rows[0])
	}
	if !strings.Contains(rows[0], ui.GetSelectionBgANSI()) {
		t.Errorf("row = %q, want the selection highlight drawn", rows[0])
	}
	// A row the selection does not cover still shows its match.
	if len(searchHighlightColumns(rows[1], searchMatchPrefix())) == 0 {
		t.Errorf("row outside the selection = %q, want its match still highlighted", rows[1])
	}
}

func TestSearchBarIsDrawnByTheModelAndCostsARow(t *testing.T) {
	m := newSearchModel(t, 40, 5, "alpha", "hit", "gamma", "hit", "epsilon", "zeta")

	plain := func() []string {
		rows := strings.Split(m.View(), "\n")
		for i, row := range rows {
			rows[i] = strings.TrimRight(ansi.Strip(row), " ")
		}
		return rows
	}

	before := plain()
	if len(before) != 5 {
		t.Fatalf("rows without search = %d, want the full height", len(before))
	}

	typeSearch(m, "hit")
	rows := plain()
	if len(rows) != 5 {
		t.Fatalf("rows with search = %d, want the height unchanged", len(rows))
	}
	bar := rows[len(rows)-1]
	if !strings.Contains(bar, "/ hit") || !strings.Contains(bar, "(1/2)") {
		t.Errorf("search bar = %q, want the query and the match count", bar)
	}
	if strings.Contains(bar, "[n/N]") {
		t.Errorf("search bar = %q, want no navigation hint before the query is committed", bar)
	}
	pressSearch(m, "enter")
	if bar := plain()[4]; !strings.Contains(bar, "[n/N]") {
		t.Errorf("committed search bar = %q, want the navigation hint", bar)
	}

	m.CloseSearch()
	typeSearch(m, "zzz")
	if bar := plain()[4]; !strings.Contains(bar, "(0 matches)") {
		t.Errorf("search bar for a query with no hits = %q, want it to say so", bar)
	}
}

func TestSearchBarShrinksTheScrollableViewport(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m := newSearchModel(t, 40, 5, lines...)
	m.Scroll(1000)
	closed := m.ScrollOffset()

	m.StartSearch()
	m.Scroll(1000)
	if got := m.ScrollOffset(); got != closed+1 {
		t.Errorf("max scroll with the search bar open = %d, want one row more than %d", got, closed)
	}
	m.CloseSearch()
	if got := m.ScrollOffset(); got != closed {
		t.Errorf("max scroll after closing search = %d, want it clamped back to %d", got, closed)
	}
}

func TestSearchMatchesFollowALiveReload(t *testing.T) {
	m := newSearchModel(t, 40, 6, "hit", "nothing")
	typeSearch(m, "hit")
	if got := len(m.SearchMatches()); got != 1 {
		t.Fatalf("matches = %d, want 1", got)
	}

	m.result.HighlightedLines = []string{"hit", "hit", "hit"}
	m.result.Content = "hit\nhit\nhit"
	m.invalidateRender()

	if got := len(m.SearchMatches()); got != 3 {
		t.Errorf("matches after the document changed = %d, want 3", got)
	}
}

func TestSearchStyleIsReadThroughTheThemeEveryTime(t *testing.T) {
	// The prefixes must not be captured in a var: ApplyTheme reassigns the
	// style variables, and a frozen copy would paint the old theme's colours.
	before := searchMatchPrefix()
	original := styles.SearchMatch
	t.Cleanup(func() { styles.SearchMatch = original })
	styles.SearchMatch = styles.SearchMatchCurrent
	if after := searchMatchPrefix(); after == before {
		t.Error("the match style did not follow a change to styles.SearchMatch")
	}
}

// TestTopSourceLineFollowsScroll is the number a host opens an editor at: the
// source line the reader is looking at, not the visual row it landed on.
func TestTopSourceLineFollowsScroll(t *testing.T) {
	m := newSearchModel(t, 40, 4, "one", "two", "three", "four", "five", "six")
	if got := m.TopSourceLine(); got != 1 {
		t.Fatalf("unscrolled: got %d, want 1", got)
	}
	m.Scroll(2)
	if got := m.TopSourceLine(); got != 3 {
		t.Fatalf("after two rows: got %d, want 3", got)
	}
}

// InjectHighlights is exported for hosts whose renderer is not a docview.Model
// (the files plugin's preview pane), so it is worth one test that pins the
// exported shape independently of Model.
func TestInjectHighlightsIsUsableWithoutAModel(t *testing.T) {
	row := "\x1b[31mfoo\x1b[0m bar foo"
	out := InjectHighlights(row, []MatchRange{
		{Index: 0, Start: 0, End: 3},
		{Index: 1, Start: 8, End: 11},
	}, 1)

	if !strings.Contains(out, "\x1b[31m") {
		t.Error("the row's own styling should survive injection")
	}
	if strings.Count(out, "\x1b[0m") < 2 {
		t.Errorf("expected a reset per highlighted range, got %q", out)
	}
	if ansi.Strip(out) != ansi.Strip(row) {
		t.Errorf("visible text changed: %q vs %q", ansi.Strip(out), ansi.Strip(row))
	}
	if InjectHighlights(row, nil, 0) != row {
		t.Error("no ranges should return the row unchanged")
	}
}

// M4d-d: the in-pane search bar is the shared query field, so it edits like
// every other query bar in the app rather than only appending and backspacing.
func TestSearchWordDeleteRemovesOneWord(t *testing.T) {
	m := newSearchModel(t, 40, 6, "alpha beta", "alpha bet")
	typeSearch(m, "alpha beta")
	if got := len(m.SearchMatches()); got != 1 {
		t.Fatalf("matches for the typed query = %d, want 1", got)
	}
	m.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.SearchQuery() != "alpha " {
		t.Fatalf("query after alt+backspace = %q, want the last word gone", m.SearchQuery())
	}
	// The narrower query re-matched: a word delete is a query change, not just
	// a redraw.
	if got := len(m.SearchMatches()); got != 2 {
		t.Fatalf("matches after the word delete = %d, want 2", got)
	}
}

// A paste lands in the query whole and re-matches, rather than arriving
// character by character or not at all.
func TestSearchPasteInsertsAtTheCaret(t *testing.T) {
	m := newSearchModel(t, 40, 6, "needle", "haystack")
	m.StartSearch()
	if handled, _ := m.HandleSearchPaste(tea.PasteMsg{Content: "needle"}); !handled {
		t.Fatal("a typing search refused a paste")
	}
	if m.SearchQuery() != "needle" {
		t.Fatalf("query after paste = %q", m.SearchQuery())
	}
	if got := len(m.SearchMatches()); got != 1 {
		t.Fatalf("matches after paste = %d, want the pasted query to have matched", got)
	}
	// A committed search has no text input on screen, so the paste is not its.
	pressSearch(m, "enter")
	if handled, _ := m.HandleSearchPaste(tea.PasteMsg{Content: "x"}); handled {
		t.Fatal("a committed search took a paste")
	}
	if m.SearchQuery() != "needle" {
		t.Fatalf("query after the refused paste = %q", m.SearchQuery())
	}
}

// The caret moves, which is the whole point of the shared field: home puts it
// at the start and a rune typed there lands at the start.
func TestSearchCaretMoves(t *testing.T) {
	m := newSearchModel(t, 40, 6, "xneedle")
	typeSearch(m, "needle")
	m.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyHome})
	m.HandleSearchKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.SearchQuery() != "xneedle" {
		t.Fatalf("query = %q, want the rune typed at the caret", m.SearchQuery())
	}
	if got := len(m.SearchMatches()); got != 1 {
		t.Fatalf("matches = %d, want the re-typed query to match", got)
	}
}

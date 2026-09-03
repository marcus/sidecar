package pluginbrowser

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/plugin"
)

// M4d-a: the browser's query row is the app's shared query field, and the row
// and the list beneath it are one keyboard path.

// chord builds a key the browser_test keyPress helper's single-rune shape
// cannot: a modified named key.
func chord(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func typeQuery(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		press(t, m, string(r))
	}
}

// Word delete is one key here because the row is the shared field, not a
// hand-rolled append-and-backspace loop.
func TestWordDeleteInTheBrowsersQuery(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	press(t, m, "/")
	typeQuery(t, m, "schema notes")
	s := m.activeState()
	if s.queryText() != "schema notes" {
		t.Fatalf("query = %q", s.queryText())
	}
	run(t, m, first(m.HandleKey(chord(tea.KeyBackspace, tea.ModAlt))))
	if got := s.queryText(); got != "schema " {
		t.Fatalf("alt+backspace left %q, want one word deleted", got)
	}
	// And the caret is where the word was, so the row draws it mid-string.
	if s.field.Cursor() != len("schema ") {
		t.Fatalf("cursor = %d", s.field.Cursor())
	}
	if !strings.Contains(strip(m.View()), "/ schema ▌") {
		t.Fatalf("the caret is not on the row:\n%s", strip(m.View()))
	}
}

// A bracketed paste reaches the query whole.
func TestPasteReachesTheBrowsersQuery(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	press(t, m, "/")
	typeQuery(t, m, "ab")
	run(t, m, first(m.HandleKey(tea.KeyPressMsg{Code: tea.KeyLeft})))
	cmd, took := m.HandlePaste(tea.PasteMsg{Content: "XY"})
	if !took {
		t.Fatal("the query row refused a paste")
	}
	run(t, m, cmd)
	if got := m.activeState().queryText(); got != "aXYb" {
		t.Fatalf("query = %q, want the paste at the caret", got)
	}
}

// ctrl+a is select-all's, everywhere. The query row must not take it.
func TestCtrlADoesNothingInTheBrowsersQuery(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	press(t, m, "/")
	typeQuery(t, m, "abc")
	s := m.activeState()
	before := s.field.Cursor()
	run(t, m, first(m.HandleKey(chord('a', tea.ModCtrl))))
	if s.queryText() != "abc" || s.field.Cursor() != before {
		t.Fatalf("ctrl+a changed the row: %q at %d", s.queryText(), s.field.Cursor())
	}
}

// Down from the query row hands the keyboard to the list on the first row; `k`
// and `up` there hand it back with the text intact. That is the whole arrow
// contract, and it is what makes a query and its results one surface.
func TestArrowsHandOffBetweenTheQueryRowAndTheList(t *testing.T) {
	host := &fakeHost{page: testPage(5)}
	m := newTestModel(t, host)
	press(t, m, "/")
	typeQuery(t, m, "dex")
	press(t, m, "enter")
	press(t, m, "/")
	if !m.ConsumesTextInput() {
		t.Fatal("`/` did not focus the query row")
	}

	press(t, m, "down")
	if m.ConsumesTextInput() {
		t.Fatal("down left the keyboard in the query row")
	}
	s := m.activeState()
	if s.cursor != 0 {
		t.Fatalf("down landed on row %d, want the first row", s.cursor)
	}
	press(t, m, "j")
	if s.cursor != 1 {
		t.Fatalf("j from the first row moved to %d", s.cursor)
	}
	press(t, m, "k")
	if s.cursor != 0 || m.ConsumesTextInput() {
		t.Fatalf("k moved to %d editing=%v, want the first row", s.cursor, m.ConsumesTextInput())
	}
	press(t, m, "k")
	if !m.ConsumesTextInput() {
		t.Fatal("k on the first row did not return to the query row")
	}
	if got := s.queryText(); got != "dex" {
		t.Fatalf("the round trip lost the query: %q", got)
	}
	// up is the same hand-off as k, and the query row is where it stops:
	// there is nothing above it.
	press(t, m, "down")
	press(t, m, "up")
	if !m.ConsumesTextInput() {
		t.Fatal("up on the first row did not return to the query row")
	}
	press(t, m, "up")
	if !m.ConsumesTextInput() || s.cursor != 0 {
		t.Fatalf("up from the query row moved somewhere: editing=%v cursor=%d", m.ConsumesTextInput(), s.cursor)
	}
}

// Esc from the query row clears it, then blurs to the list.
func TestEscapeFromTheQueryRowClearsThenBlurs(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	press(t, m, "/")
	typeQuery(t, m, "dex")
	press(t, m, "esc")
	if !m.ConsumesTextInput() || m.activeState().queryText() != "" {
		t.Fatalf("the first esc did not clear: editing=%v query=%q", m.ConsumesTextInput(), m.activeState().queryText())
	}
	press(t, m, "esc")
	if m.ConsumesTextInput() {
		t.Fatal("the second esc did not blur")
	}
}

// The × is a control only where there is a query to drop, it runs the same
// clear the filter-clear command runs, and the command is offered under exactly
// the same condition.
func TestTheClearControlAndTheFilterClearCommand(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	p := &TabPlugin{id: "fixture", name: "fixture", model: m, focused: true}
	m.View()
	if regions := regionsWithID(m, regionClear); len(regions) != 0 {
		t.Fatalf("an empty query registered a clear control: %+v", regions)
	}

	press(t, m, "/")
	typeQuery(t, m, "dex")
	press(t, m, "enter")
	m.View()
	if !strings.Contains(strip(m.View()), "×") {
		t.Fatalf("a non-empty query drew no ×:\n%s", strip(m.View()))
	}
	press(t, m, "/")
	if !hasCommand(p.Commands(), cmdFilterClear) {
		t.Fatalf("filter-clear is not offered with a query on the row: %+v", p.Commands())
	}
	for _, command := range p.Commands() {
		if command.Context != p.FocusContext() {
			t.Fatalf("command %q is in context %q, not the query row's", command.ID, command.Context)
		}
	}
	press(t, m, "enter")

	clear := firstRegion(t, m, regionClear)
	clickRegion(t, m, clear)
	if got := m.activeState().queryText(); got != "" {
		t.Fatalf("the × left %q on the row", got)
	}
	// Clearing re-lists through the same debounce every other edit uses, and
	// this collection requires a query, so what comes back is the unqueried
	// page rather than a process nobody asked for.
	if !m.activeState().unqueried || len(m.activeState().items) != 0 {
		t.Fatalf("clearing the query never re-listed: unqueried=%v items=%d",
			m.activeState().unqueried, len(m.activeState().items))
	}
	m.View()
	if regions := regionsWithID(m, regionClear); len(regions) != 0 {
		t.Fatalf("the × survived the clear: %+v", regions)
	}
	press(t, m, "/")
	if hasCommand(p.Commands(), cmdFilterClear) {
		t.Fatal("filter-clear is offered with nothing to clear")
	}
}

func hasCommand(commands []plugin.Command, id string) bool {
	for _, command := range commands {
		if command.ID == id {
			return true
		}
	}
	return false
}

func first(cmd tea.Cmd, _ bool) tea.Cmd { return cmd }

// The detail follows the cursor: a sweep down ten rows spends one get, not ten,
// and what is on screen stays on screen the whole way down.
func TestTheDetailFollowsTheCursorAtMostOncePerSweep(t *testing.T) {
	host := &fakeHost{page: testPage(12), doc: longDocument()}
	m := loadedModel(t, host)
	press(t, m, "enter")
	if len(host.gets) != 1 || !m.detail.loaded {
		t.Fatalf("the first row never opened: gets=%d loaded=%v", len(host.gets), m.detail.loaded)
	}

	// The sweep, as the runtime sees it: every keypress schedules, and the
	// ticks land afterwards. Running each tick inside the loop would be a user
	// who waited 80 ms between keys, which is the case that was never in
	// question.
	var ticks []tea.Cmd
	for i := 0; i < 10; i++ {
		cmd, _ := m.HandleKey(keyPress("j"))
		ticks = append(ticks, cmd)
		if !strings.Contains(strip(m.View()), "Fixture row") {
			t.Fatalf("the detail blanked on row %d of the sweep:\n%s", i, strip(m.View()))
		}
	}
	if len(host.gets) != 1 {
		t.Fatalf("the sweep spent %d gets before a single tick landed", len(host.gets))
	}
	for _, cmd := range ticks {
		run(t, m, cmd)
	}
	if len(host.gets) != 2 {
		t.Fatalf("a ten-row sweep spent %d gets, want one for the row it landed on", len(host.gets)-1)
	}
	if got := host.gets[1].Params.ID; got != "rc:notes:11" {
		t.Fatalf("the sweep loaded %q, want the row it landed on", got)
	}
	if got := host.gets[1].PaneKey; got == "" {
		t.Fatal("the get carried no pane key, so nothing supersedes it")
	}
	if !strings.Contains(strip(m.View()), "Fixture row") {
		t.Fatalf("the detail is empty after the sweep:\n%s", strip(m.View()))
	}
}

// Enter on a row the box already shows spends no process: it moves the
// keyboard into the document, which is the second Enter in the user contract.
func TestEnterOnALoadedRowSpawnsNothing(t *testing.T) {
	host := &fakeHost{page: testPage(12), doc: longDocument()}
	m := loadedModel(t, host)
	press(t, m, "j")
	if len(host.gets) != 1 {
		t.Fatalf("the cursor move did not load the row: gets=%d", len(host.gets))
	}
	press(t, m, "enter")
	if len(host.gets) != 1 {
		t.Fatalf("enter on a followed row spent another get: %d", len(host.gets))
	}
	if m.focus != FocusDetail {
		t.Fatalf("enter left focus at %q, want the document", m.focus)
	}
}

// A schedule the cursor moved away from never loads, and neither does one whose
// plugin re-described underneath it.
func TestAStaleFollowNeverLoads(t *testing.T) {
	host := &fakeHost{page: testPage(12), doc: longDocument()}
	m := loadedModel(t, host)
	cmd, _ := m.HandleKey(keyPress("j"))
	// The cursor moves on before the tick lands.
	m.moveTo(5)
	run(t, m, cmd)
	if len(host.gets) != 0 {
		t.Fatalf("a superseded schedule loaded %d documents", len(host.gets))
	}

	cmd, _ = m.HandleKey(keyPress("j"))
	m.describeGeneration++
	run(t, m, cmd)
	if len(host.gets) != 0 {
		t.Fatalf("a schedule from before a re-describe loaded %d documents", len(host.gets))
	}
}

// M4d-a review fixes: a held navigation key must not type itself into the row
// it just focused, home and end belong to whichever window has the keyboard,
// and a host that scrolls the list through ScrollBy gets the follow with it.

// Ten rapid `k` presses scroll a list up and stop at the query row. The
// repeats still in flight when focus arrives there are the tail of a held key,
// not text: without the arrival guard the row ends up holding "kkkkkkk" and
// re-lists 250 ms later against a query nobody typed.
func TestAHeldKDoesNotTypeItselfIntoTheQueryRow(t *testing.T) {
	clock := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	host := &fakeHost{page: testPage(12), doc: longDocument(), clock: &clock}
	m := loadedModel(t, host)
	m.moveTo(3)
	if m.activeState().cursor != 3 {
		t.Fatalf("the sweep did not start on row three: cursor=%d", m.activeState().cursor)
	}

	// One burst of key repeats, 30 ms apart, as a held key arrives.
	for i := 0; i < 10; i++ {
		clock = clock.Add(30 * time.Millisecond)
		press(t, m, "k")
	}
	s := m.activeState()
	if !m.ConsumesTextInput() {
		t.Fatal("the burst never reached the query row")
	}
	if got := s.queryText(); got != "rows" {
		t.Fatalf("the held key typed itself into the row: query = %q", got)
	}
	if s.cursor != 0 {
		t.Fatalf("the burst left the cursor at %d, want the first row", s.cursor)
	}

	// The burst is over once the user's finger comes off the key: a `k` typed
	// after the repeat window is a letter like any other.
	clock = clock.Add(400 * time.Millisecond)
	press(t, m, "k")
	if got := s.queryText(); got != "rowsk" {
		t.Fatalf("a deliberate k after the burst was swallowed: query = %q", got)
	}
}

// The guard is about one held key, not about the field being new. Any other
// key ends it immediately, so a user who lands on the row and types goes
// straight into the text.
func TestADifferentLetterRightAfterTheHandOffIsTyped(t *testing.T) {
	clock := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	host := &fakeHost{page: testPage(12), doc: longDocument(), clock: &clock}
	m := loadedModel(t, host)
	press(t, m, "k") // row one to the query row
	if !m.ConsumesTextInput() {
		t.Fatal("k on the first row did not reach the query row")
	}
	clock = clock.Add(10 * time.Millisecond)
	press(t, m, "x")
	if got := m.activeState().queryText(); got != "rowsx" {
		t.Fatalf("a different letter was swallowed: query = %q", got)
	}
	// And the guard is spent: the next k is text too.
	clock = clock.Add(10 * time.Millisecond)
	press(t, m, "k")
	if got := m.activeState().queryText(); got != "rowsxk" {
		t.Fatalf("the guard outlived the key that armed it: query = %q", got)
	}
}

// `/` and a click hand the row the keyboard with no key repeat behind them, so
// they arm nothing.
func TestSlashArmsNoArrivalGuard(t *testing.T) {
	clock := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	host := &fakeHost{page: testPage(12), doc: longDocument(), clock: &clock}
	m := loadedModel(t, host)
	press(t, m, "/")
	press(t, m, "k")
	if got := m.activeState().queryText(); got != "rowsk" {
		t.Fatalf("`/` armed a guard: query = %q", got)
	}
}

// A host scrolling the list — a wheel over a surface that routes through
// ScrollBy — is a cursor move, so the detail follows it. The cmd is the whole
// follow: dropping it both loses this load and invalidates the one the keyboard
// had already scheduled.
func TestScrollByFollowsTheCursor(t *testing.T) {
	host := &fakeHost{page: testPage(12), doc: longDocument()}
	m := loadedModel(t, host)
	cmd, moved := m.ScrollBy(1)
	if !moved {
		t.Fatal("ScrollBy(1) moved nothing")
	}
	if m.activeState().cursor != 1 {
		t.Fatalf("ScrollBy(1) left the cursor at %d", m.activeState().cursor)
	}
	if cmd == nil {
		t.Fatal("ScrollBy scheduled no follow for the row it landed on")
	}
	run(t, m, cmd)
	if len(host.gets) != 1 {
		t.Fatalf("the quiet tick loaded %d documents, want the row under the cursor", len(host.gets))
	}
	if got := host.gets[0].Params.ID; got != "rc:notes:2" {
		t.Fatalf("ScrollBy loaded %q, want the row the cursor landed on", got)
	}
}

// Over the detail, ScrollBy is a viewport move and schedules nothing: there is
// no new row to follow.
func TestScrollByOverTheDetailSchedulesNothing(t *testing.T) {
	host := &fakeHost{page: testPage(12), doc: longDocument()}
	m := loadedModel(t, host)
	press(t, m, "enter")
	press(t, m, "enter")
	if m.focus != FocusDetail {
		t.Fatalf("focus is %q, want the document", m.focus)
	}
	cmd, moved := m.ScrollBy(3)
	if !moved || cmd != nil {
		t.Fatalf("ScrollBy over the detail: moved=%v cmd=%v", moved, cmd != nil)
	}
}

// home and end go where j and k go. With the detail focused they are the top
// and the bottom of the document being read; sending them to the list would
// move the cursor and replace the document under the reader.
func TestHomeAndEndBelongToTheFocusedWindow(t *testing.T) {
	host := &fakeHost{page: testPage(12), doc: longDocument()}
	m := loadedModel(t, host)
	press(t, m, "j")
	press(t, m, "j")
	run(t, m, first(m.HandleKey(keyPress("enter"))))
	press(t, m, "enter")
	if m.focus != FocusDetail {
		t.Fatalf("focus is %q, want the document", m.focus)
	}
	cursor := m.activeState().cursor
	shown := m.detail.shownID

	press(t, m, "end")
	if m.detail.scroll != m.maxDetailScroll() || m.maxDetailScroll() == 0 {
		t.Fatalf("end left the document at %d of %d", m.detail.scroll, m.maxDetailScroll())
	}
	if m.activeState().cursor != cursor || m.detail.shownID != shown {
		t.Fatalf("end moved the list: cursor %d→%d, document %q→%q",
			cursor, m.activeState().cursor, shown, m.detail.shownID)
	}
	press(t, m, "home")
	if m.detail.scroll != 0 {
		t.Fatalf("home left the document at %d", m.detail.scroll)
	}
	if m.activeState().cursor != cursor || m.detail.shownID != shown {
		t.Fatalf("home moved the list: cursor %d→%d, document %q→%q",
			cursor, m.activeState().cursor, shown, m.detail.shownID)
	}

	// With the list focused they are still the list's, and they take the
	// detail with them like every other cursor move.
	m.focus = FocusList
	run(t, m, first(m.HandleKey(keyPress("end"))))
	if got := m.activeState().cursor; got != 11 {
		t.Fatalf("end on the list landed on row %d, want the last", got)
	}
	run(t, m, first(m.HandleKey(keyPress("home"))))
	if got := m.activeState().cursor; got != 0 {
		t.Fatalf("home on the list landed on row %d, want the first", got)
	}
	if len(host.gets) < 2 {
		t.Fatalf("home and end on the list scheduled no follow: gets=%d", len(host.gets))
	}
}

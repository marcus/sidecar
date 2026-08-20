package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/plugin"
)

// sizingPlugin records every content box the shell hands it. The panel's whole
// contract with plugins is that box, so these tests read it rather than any
// panel-specific hook — a plugin must not need to know the centre exists.
type sizingPlugin struct {
	nativeTestPlugin
	id     string
	widths []int
}

func (p *sizingPlugin) ID() string           { return p.id }
func (p *sizingPlugin) Name() string         { return p.id }
func (p *sizingPlugin) FocusContext() string { return p.id }
func (p *sizingPlugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		p.widths = append(p.widths, size.Width)
	}
	return p, nil
}

func (p *sizingPlugin) lastWidth() int {
	if len(p.widths) == 0 {
		return -1
	}
	return p.widths[len(p.widths)-1]
}

func centreTestModel(t *testing.T, plugins ...*sizingPlugin) Model {
	t.Helper()
	reg := plugin.NewRegistry(nil)
	for _, p := range plugins {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)
	m := Model{
		registry:                reg,
		keymap:                  km,
		ui:                      &UIState{},
		ready:                   true,
		applicationFocused:      true,
		width:                   120,
		height:                  40,
		intro:                   IntroModel{Done: true},
		cfg:                     config.Default(),
		notifications:           notify.NewMemStore(),
		notificationCentreMouse: mouse.NewHandler(),
		toastMouse:              mouse.NewHandler(),
	}
	m.updateContext()
	return m
}

func postCentreNotification(t *testing.T, m *Model, source notify.SourceID, title string) notify.Notification {
	t.Helper()
	stored, err := m.notifications.Post(notify.Notification{Source: source, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	m.refreshNotifications()
	return stored
}

// The reservation is the feature: every plugin must be handed the narrowed
// width, and the narrowing must arrive as an ordinary resize.
func TestOpeningTheCentreNarrowsEveryPluginAndReEmitsSize(t *testing.T) {
	first := &sizingPlugin{id: "files"}
	second := &sizingPlugin{id: "git"}
	m := centreTestModel(t, first, second)

	m.toggleNotificationCentre()
	reserved := m.reservedRightWidth()
	if reserved <= 0 {
		t.Fatalf("reservedRightWidth = %d, want a reserved column", reserved)
	}
	want := m.width - reserved
	for _, p := range []*sizingPlugin{first, second} {
		if got := p.lastWidth(); got != want {
			t.Fatalf("%s width = %d, want %d", p.id, got, want)
		}
	}
	if got := m.contentWidth(); got != want {
		t.Fatalf("contentWidth = %d, want %d", got, want)
	}

	m.toggleNotificationCentre()
	for _, p := range []*sizingPlugin{first, second} {
		if got := p.lastWidth(); got != m.width {
			t.Fatalf("%s width after close = %d, want %d", p.id, got, m.width)
		}
	}
}

// A terminal resize while the panel is open must keep handing out the narrowed
// width rather than resetting to the full terminal.
func TestTerminalResizeKeepsTheReservation(t *testing.T) {
	p := &sizingPlugin{id: "files"}
	m := centreTestModel(t, p)
	m.toggleNotificationCentre()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = asAppModel(t, updated)
	if !m.notificationCentreOpen {
		t.Fatal("resize closed the centre")
	}
	want := 160 - m.reservedRightWidth()
	if got := p.lastWidth(); got != want {
		t.Fatalf("width after resize = %d, want %d", got, want)
	}
}

// Tab switches, modals, and clicks into content are navigation, not a close.
func TestCentreSurvivesTabSwitchesAndModals(t *testing.T) {
	first := &sizingPlugin{id: "files"}
	second := &sizingPlugin{id: "git"}
	m := centreTestModel(t, first, second)
	m.toggleNotificationCentre()
	reserved := m.reservedRightWidth()

	m.selectProjectTabByNumber(1)
	if !m.notificationCentreOpen || m.reservedRightWidth() != reserved {
		t.Fatal("switching tabs disturbed the centre")
	}
	if got := second.lastWidth(); got != m.width-reserved {
		t.Fatalf("plugin width after tab switch = %d, want %d", got, m.width-reserved)
	}

	// A modal takes the keyboard while it is up and gives it back afterwards,
	// without the panel ever closing.
	m.showHelp = true
	m.updateContext()
	if m.activeContext == notificationCentreContext {
		t.Fatal("an open modal left the centre holding the keyboard")
	}
	if !m.notificationCentreOpen {
		t.Fatal("opening a modal closed the centre")
	}
	m.showHelp = false
	m.updateContext()
	if m.activeContext != notificationCentreContext {
		t.Fatalf("context after modal = %q, want the centre to take the keyboard back", m.activeContext)
	}
	if !m.notificationCentreOpen {
		t.Fatal("closing a modal closed the centre")
	}
}

// The highest-risk path: a project switch rebuilds every plugin, so the
// reservation has to be restored before the next frame.
func TestCentreSurvivesProjectSwitchReinit(t *testing.T) {
	source := newOverviewGitRepo(t, "source")
	target := newOverviewGitRepo(t, "target")
	isolateAppState(t)

	cfg := config.Default()
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	p := &sizingPlugin{id: "files"}
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	m := New(reg, km, cfg, "", source, source, "files")
	m.width, m.height, m.ready = 120, 40, true
	m.toggleNotificationCentre()
	reserved := m.reservedRightWidth()
	if reserved <= 0 {
		t.Fatal("centre reserved nothing to begin with")
	}

	m.switchProjectWithInventory(target, nil)

	if !m.notificationCentreOpen {
		t.Fatal("a project switch closed the centre")
	}
	if got := m.reservedRightWidth(); got != reserved {
		t.Fatalf("reservation after switch = %d, want %d", got, reserved)
	}
	if got := p.lastWidth(); got != m.width-reserved {
		t.Fatalf("plugin width after Reinit = %d, want %d", got, m.width-reserved)
	}
}

// Clicking back into content returns focus; it does not close the panel.
func TestClickingContentBlursTheCentreWithoutClosingIt(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	m.toggleNotificationCentre()
	if !m.notificationCentreOwnsKeys() {
		t.Fatal("opening the centre did not give it the keyboard")
	}
	m.View() // registers the panel's hit regions

	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: 5}
	updated, _ := m.Update(click)
	m = asAppModel(t, updated)
	if !m.notificationCentreOpen {
		t.Fatal("a click in the content closed the centre")
	}
	if m.notificationCentreFocused {
		t.Fatal("a click in the content left focus on the centre")
	}
	if m.activeContext == notificationCentreContext {
		t.Fatalf("context after content click = %q", m.activeContext)
	}
}

// Esc with the panel focused is an explicit close, and the only kind there is.
func TestEscapeClosesTheFocusedCentre(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	m.toggleNotificationCentre()
	m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.notificationCentreOpen {
		t.Fatal("esc did not close the focused centre")
	}
	if m.reservedRightWidth() != 0 {
		t.Fatal("a closed centre still reserves a column")
	}
}

func TestCentreListKeysMoveDismissAndGroupDismiss(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	// Two sources so `D` has a group to clear and something to leave behind.
	postCentreNotification(t, &m, notify.SourceTasks, "task one")
	postCentreNotification(t, &m, notify.SourceTasks, "task two")
	postCentreNotification(t, &m, notify.SourceAgent, "agent one")
	m.toggleNotificationCentre()

	items := m.notificationCentreItems()
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.notificationCentreCursor != 1 {
		t.Fatalf("cursor after j = %d, want 1", m.notificationCentreCursor)
	}
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.notificationCentreCursor != 0 {
		t.Fatalf("cursor after k = %d, want 0", m.notificationCentreCursor)
	}

	// `d` dismisses the selected notification outright, as it does on a toast,
	// and leaves the rest of its source alone.
	m.notificationCentreCursor = 1
	second := items[1]
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'd', Text: "d"})
	remaining := m.notificationCentreItems()
	if len(remaining) != 2 {
		t.Fatalf("items after d = %d, want 2", len(remaining))
	}
	for _, n := range remaining {
		if n.ID == second.ID {
			t.Fatal("d left the selected notification in the list")
		}
	}

	// `D` clears the selected item's whole source and nothing else.
	m.notificationCentreCursor = 0
	source := notify.SourceOf(remaining[0].Source).ID
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'D', Text: "D", Mod: tea.ModShift})
	left := m.notificationCentreItems()
	for _, n := range left {
		if notify.SourceOf(n.Source).ID == source {
			t.Fatalf("D left %q behind in source %s", n.Title, source)
		}
	}
	if len(left) == 0 {
		t.Fatal("D cleared sources it was not pointed at")
	}
}

// enter is "view details": the selected notification comes back as a toast,
// without the store learning anything about it.
func TestCentreEnterReshowsTheSelectionAsAToast(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	posted := postCentreNotification(t, &m, notify.SourceTasks, "task one")
	m.toggleNotificationCentre()
	// Mark it read, as selecting it in the centre does, so nothing would be
	// toasting on its own.
	m.readNotification(posted.ID)
	m.notificationCentreCursor = 0

	before := len(m.notificationCentreItems())
	// `enter` is consumed by the panel; the only command it may return is the
	// re-shown block's reveal tick.
	handled, _ := m.notificationCentreKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter was not consumed by the panel")
	}
	if got := len(m.notificationCentreItems()); got != before {
		t.Fatalf("enter changed the list: %d -> %d", before, got)
	}
	if !m.notificationCentreOpen || !m.notificationCentreFocused {
		t.Fatal("enter closed or blurred the panel; it is a detail view, not a navigation")
	}
	shown, ok := m.visibleToast()
	if !ok || shown.ID != posted.ID {
		t.Fatalf("visibleToast after enter = %+v (ok=%v), want the selection", shown, ok)
	}
	if shown.CreatedAt.Equal(posted.CreatedAt) {
		t.Fatal("the re-shown copy did not get a fresh countdown")
	}
	// The record itself is untouched: a re-show is presentation only.
	stored, ok := m.findNotification(posted.ID)
	if !ok || !stored.CreatedAt.Equal(posted.CreatedAt) || stored.Dismissed() {
		t.Fatalf("enter rewrote the stored record: %+v", stored)
	}
	if got := len(notify.Toastable(m.notificationCache, time.Now())); got != 0 {
		t.Fatalf("the store thinks %d notifications should be toasting; a re-show must not re-post", got)
	}
}

// The panel is a content pane and wears the shell's shared gradient border,
// not a border rule of its own.
func TestCentreWearsTheSharedPaneBorder(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postCentreNotification(t, &m, notify.SourceTasks, "a due task")
	m.toggleNotificationCentre()

	panel := m.renderNotificationCentre(m.height - headerHeight - footerHeight)
	lines := strings.Split(panel, "\n")
	if len(lines) < 3 {
		t.Fatalf("panel rendered %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "╭") || !strings.Contains(lines[0], "╮") {
		t.Fatalf("panel has no top border: %q", lines[0])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "╰") || !strings.Contains(last, "╯") {
		t.Fatalf("panel has no bottom border: %q", last)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got != m.reservedRightWidth() {
			t.Fatalf("panel row width = %d, want %d (%q)", got, m.reservedRightWidth(), line)
		}
	}
}

// An entry is two lines — title and body — so a call to action is not lost to
// a truncated title. A notification with no body stays one line.
func TestCentreEntriesAreTwoLines(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	if _, err := m.notifications.Post(notify.Notification{
		Source: notify.SourceTD, Title: "review requested", Body: "td-4c1f9a needs a reviewer",
	}); err != nil {
		t.Fatal(err)
	}
	postCentreNotification(t, &m, notify.SourceTasks, "bare title")
	m.refreshNotifications()
	m.toggleNotificationCentre()

	rows := m.notificationCentreBody(notificationCentreDefaultWidth-4, time.Now())
	counts := map[int]int{}
	var bodyRow string
	for _, row := range rows {
		if row.item < 0 {
			continue
		}
		counts[row.item]++
		if strings.Contains(row.text, "needs a reviewer") {
			bodyRow = row.text
		}
	}
	if bodyRow == "" {
		t.Fatal("the body never got a row of its own")
	}
	if strings.Contains(bodyRow, "review requested") {
		t.Fatalf("title and body share a row: %q", bodyRow)
	}
	// Entry 0 is the selection on a focused panel and names a target, so it
	// carries the numbered targets row as well (Phase 5); entry 1 has neither
	// body nor target and stays one row.
	if counts[0] != 3 || counts[1] != 1 {
		t.Fatalf("row counts per entry = %v, want 3 and 1", counts)
	}
	// Blurred, the selection loses the targets row and the entry is the two
	// lines plan 1.5 asked for.
	m.blurNotificationCentre()
	counts = map[int]int{}
	for _, row := range m.notificationCentreBody(notificationCentreDefaultWidth-4, time.Now()) {
		if row.item >= 0 {
			counts[row.item]++
		}
	}
	if counts[0] != 2 || counts[1] != 1 {
		t.Fatalf("unfocused row counts per entry = %v, want 2 and 1", counts)
	}
}

// Navigation keys still work while the panel has focus: it is a panel, not a
// modal.
func TestCentreLeavesNavigationKeysAlone(t *testing.T) {
	first := &sizingPlugin{id: "files"}
	second := &sizingPlugin{id: "git"}
	m := centreTestModel(t, first, second)
	m.toggleNotificationCentre()
	m.handleKeyMsg(tea.KeyPressMsg{Code: '2', Text: "2"})
	if m.activePlugin != 1 {
		t.Fatalf("activePlugin = %d, want the tab number to still switch tabs", m.activePlugin)
	}
	if !m.notificationCentreOpen {
		t.Fatal("a tab switch closed the centre")
	}
}

// A terminal with nothing to spare keeps its width for content. The panel is
// still open, so widening the terminal brings it back with no user action.
func TestCentreYieldsOnATerminalWithNoRoom(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	m.toggleNotificationCentre()
	m.width = 62
	if got := m.reservedRightWidth(); got != 0 {
		t.Fatalf("reserved = %d on a 62-column terminal, want 0", got)
	}
	if got := m.contentWidth(); got != 62 {
		t.Fatalf("contentWidth = %d, want the whole terminal", got)
	}
	if !m.notificationCentreOpen {
		t.Fatal("a narrow terminal closed the centre")
	}
	m.width = 120
	if m.reservedRightWidth() <= 0 {
		t.Fatal("widening the terminal did not bring the panel back")
	}
}

func TestCentreWidthClamps(t *testing.T) {
	if got := clampNotificationCentreWidth(notificationCentreMaxWidth+40, 200); got != notificationCentreMaxWidth {
		t.Fatalf("clamp(huge) = %d, want %d", got, notificationCentreMaxWidth)
	}
	if got := clampNotificationCentreWidth(2, 200); got != notificationCentreMinWidth {
		t.Fatalf("clamp(tiny) = %d, want %d", got, notificationCentreMinWidth)
	}
	if got := clampNotificationCentreWidth(40, 60); got != 0 {
		t.Fatalf("clamp on a cramped terminal = %d, want 0", got)
	}
}

// The panel paints its own title, close affordance, section grammar, and the
// retention note — and it paints them inside the reserved column, so nothing it
// draws lands on the content.
func TestCentreRendersTheSectionGrammar(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postCentreNotification(t, &m, notify.SourceTasks, "a due task")
	m.toggleNotificationCentre()

	panel := m.renderNotificationCentre(m.height - headerHeight - footerHeight)
	if panel == "" {
		t.Fatal("panel rendered nothing")
	}
	for _, want := range []string{"Notifications", "TASKS", "a due task", notificationCentreFootnote} {
		if !strings.Contains(panel, want) {
			t.Fatalf("panel is missing %q", want)
		}
	}
	for _, line := range strings.Split(panel, "\n") {
		if got := lipgloss.Width(line); got != m.reservedRightWidth() {
			t.Fatalf("panel row width = %d, want %d (%q)", got, m.reservedRightWidth(), line)
		}
	}
}

func TestCentreAgeMetaColumn(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		age  time.Duration
		want string
	}{
		{10 * time.Second, "now"},
		{4 * time.Minute, "4m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, tc := range cases {
		if got := notificationAge(now.Add(-tc.age), now); got != tc.want {
			t.Fatalf("notificationAge(%s) = %q, want %q", tc.age, got, tc.want)
		}
	}
}

// A toast on screen must not shadow a plugin's own `d`. Precedence level 3
// covers plugin.KeyRouter implementations only — git status and the workspace
// list bind `d` at level 5, after the global switch — so the global toast
// dismissal is gated on the focused context not having claimed the key.
func TestToastDismissDoesNotStealAPluginsDKey(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "git"})
	posted := postCentreNotification(t, &m, notify.SourceAgent, "staged main.go")
	if len(m.ToastableNotifications(time.Now())) != 1 {
		t.Fatal("expected a toast to be up")
	}

	for _, ctx := range []string{"git-status", "workspace-list", "workspace-preview"} {
		m.activeContext = ctx
		m.handleKeyMsg(tea.KeyPressMsg{Code: 'd', Text: "d"})
		if n, ok := m.findNotification(posted.ID); !ok || n.Dismissed() {
			t.Fatalf("context %q: the toast swallowed the plugin's d key", ctx)
		}
	}

	// Where `d` really is free, it still dismisses the toast.
	syncToasts(t, &m)
	m.activeContext = "global"
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if n, ok := m.findNotification(posted.ID); !ok || !n.Dismissed() {
		t.Fatal("d did not dismiss the toast where the key was free")
	}
}

// Keyboard-only users must be able to leave the panel without closing it: a
// navigation key hands the keyboard back to the content, and `N` brings it back.
func TestNavigationKeysReleaseCentreFocusWithoutClosingIt(t *testing.T) {
	first := &sizingPlugin{id: "files"}
	second := &sizingPlugin{id: "git"}
	m := centreTestModel(t, first, second)
	postCentreNotification(t, &m, notify.SourceTasks, "task one")
	m.toggleNotificationCentre()
	if !m.notificationCentreOwnsKeys() {
		t.Fatal("opening should focus the panel")
	}

	m.handleKeyMsg(tea.KeyPressMsg{Code: '2', Text: "2"})
	if !m.notificationCentreOpen {
		t.Fatal("a tab switch closed the panel")
	}
	if m.notificationCentreOwnsKeys() {
		t.Fatal("the panel kept the keyboard after the user navigated away")
	}
	if m.activeContext == notificationCentreContext {
		t.Fatalf("activeContext = %q, want the content's context", m.activeContext)
	}

	// `N` from there is a request to go back to the panel, not to close it.
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'N', Text: "N", Mod: tea.ModShift})
	if !m.notificationCentreOpen || !m.notificationCentreOwnsKeys() {
		t.Fatal("N did not return focus to the open panel")
	}
	// And again closes, as the toggle always has.
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'N', Text: "N", Mod: tea.ModShift})
	if m.notificationCentreOpen {
		t.Fatal("N did not close the focused panel")
	}
}

// Nothing marked anything read before: ReadAt stayed nil forever, the header
// counter only climbed, and every unexpired notification toasted again at the
// next start.
func TestSelectingAndExpiringMarkNotificationsRead(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	first := postCentreNotification(t, &m, notify.SourceTasks, "task one")
	postCentreNotification(t, &m, notify.SourceTasks, "task two")

	m.toggleNotificationCentre()
	if n, _ := m.findNotification(m.notificationCentreItems()[0].ID); !n.Read() {
		t.Fatal("opening the centre on an item did not mark it read")
	}
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if n, _ := m.findNotification(m.notificationCentreItems()[1].ID); !n.Read() {
		t.Fatal("moving the cursor onto an item did not mark it read")
	}
	if m.UnreadNotifications() != 0 {
		t.Fatalf("unread = %d after selecting both, want 0", m.UnreadNotifications())
	}

	// A toast whose countdown runs out has had its moment: the sweep reads it.
	m.closeNotificationCentre()
	third, err := m.notifications.Post(notify.Notification{Source: notify.SourceAgent, Title: "agent done"})
	if err != nil {
		t.Fatal(err)
	}
	m.refreshNotifications()
	if m.UnreadNotifications() != 1 {
		t.Fatalf("a fresh notification should be unread")
	}
	// A tick with the toast on screen. The column is reconciled first, exactly
	// as the heartbeat does it: the read gate asks the reveal states what is
	// painted, so a sweep with no synced column reads nothing.
	m.syncToastReveal(third.CreatedAt.Add(time.Second))
	m.sweepNotifications(third.CreatedAt.Add(time.Second))
	m.sweepNotifications(third.ExpiresAt.Add(time.Second))
	if n, _ := m.findNotification(third.ID); !n.Read() {
		t.Fatal("an expired toast stayed unread, so it would toast again at the next start")
	}
	if len(m.ToastableNotifications(time.Now())) != 0 {
		t.Fatal("a read notification must not toast again")
	}
	if len(notify.Active(m.Notifications())) != 3 {
		t.Fatal("reading must not remove anything from the centre")
	}
	_ = first
}

// A notification posted while sidecar was closed arrives already past its
// countdown. Expiry alone would read it on the first heartbeat, so it would
// never announce itself — the whole point of the CLI's offline fallback.
func TestExpiredWhileClosedStaysUnread(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	stale, err := m.notifications.Post(notify.Notification{
		Source:    notify.SourceAgent,
		Title:     "build failed",
		CreatedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.refreshNotifications()

	m.sweepNotifications(time.Now())
	if n, _ := m.findNotification(stale.ID); n.Read() {
		t.Fatal("a notification whose toast was never painted was marked read")
	}
	if m.UnreadNotifications() != 1 {
		t.Fatalf("unread = %d, want 1: the user has still not seen it", m.UnreadNotifications())
	}
}

// The centre is reachable by keyboard on every tab. `N` yields to plugins that
// bind it (git's prev-match), so alt+n is the route that always works.
func TestAltNOpensTheCentreEvenWhereNIsBound(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	m.activeContext = "git-status-commits" // binds N to prev-match

	m.handleKeyMsg(tea.KeyPressMsg{Code: 'N', Text: "N", Mod: tea.ModShift})
	if m.notificationCentreOpen {
		t.Fatal("N must yield to a context that rebinds it")
	}
	m.handleKeyMsg(tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	if !m.notificationCentreOpen {
		t.Fatal("alt+n did not open the centre")
	}
}

// centreRegion finds a registered panel region by exact id.
func centreRegion(t *testing.T, m *Model, id string) mouse.Region {
	t.Helper()
	for _, r := range m.notificationCentreMouse.HitMap.Regions() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no region %q in %+v", id, m.notificationCentreMouse.HitMap.Regions())
	return mouse.Region{}
}

// A single click selects and marks read; a double-click is `enter` — today
// "view details", and whatever enter means after Phase 5, because both run
// activateSelectedNotification.
func TestCentreDoubleClickOnAnEntryDoesWhatEnterDoes(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	first := postCentreNotification(t, &m, notify.SourceTasks, "task one")
	second := postCentreNotification(t, &m, notify.SourceTasks, "task two")
	m.toggleNotificationCentre()
	// Both read, so nothing is toasting on its own and any toast the test sees
	// is the one the pointer asked for.
	m.readNotification(first.ID)
	m.readNotification(second.ID)
	m.View() // registers the panel's hit regions

	// The list is source-grouped and newest-first, so name the target by index
	// rather than by which of the two was posted first.
	target := m.notificationCentreItems()[1]
	region := centreRegion(t, &m, regionNotificationCentreItem+"1")
	// A few columns in from the panel edge: the resize rail's widened hit box
	// overlaps the panel's first column, and that is the rail's to keep.
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: region.Rect.X + 3, Y: region.Rect.Y}

	// First click: selection only, no toast.
	if handled, _ := m.notificationCentreMouseEvent(click); !handled {
		t.Fatal("a click on an entry was not handled by the panel")
	}
	if m.notificationCentreCursor != 1 {
		t.Fatalf("cursor after click = %d, want 1", m.notificationCentreCursor)
	}
	if _, ok := m.visibleToast(); ok {
		t.Fatal("a single click re-showed the entry; it must only select")
	}

	// Second click at the same cell inside the double-click window.
	before := len(m.notificationCentreItems())
	if handled, _ := m.notificationCentreMouseEvent(click); !handled {
		t.Fatal("a double-click on an entry was not handled by the panel")
	}
	shown, ok := m.visibleToast()
	if !ok || shown.ID != target.ID {
		t.Fatalf("visibleToast after double-click = %+v (ok=%v), want the clicked entry", shown, ok)
	}
	if got := len(m.notificationCentreItems()); got != before {
		t.Fatalf("double-click changed the list: %d -> %d", before, got)
	}
	if !m.notificationCentreOpen || !m.notificationCentreFocused {
		t.Fatal("double-click closed or blurred the panel")
	}
	stored, ok := m.findNotification(target.ID)
	if !ok || !stored.CreatedAt.Equal(target.CreatedAt) || stored.Dismissed() {
		t.Fatalf("double-click rewrote the stored record: %+v", stored)
	}
}

// Every group header carries a clear control at its right end, and clicking it
// is `D` for that group — whatever the cursor happens to be on.
func TestCentreGroupHeaderClearClearsItsOwnGroup(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postCentreNotification(t, &m, notify.SourceTasks, "task one")
	postCentreNotification(t, &m, notify.SourceTasks, "task two")
	agent := postCentreNotification(t, &m, notify.SourceAgent, "agent one")
	m.toggleNotificationCentre()
	m.View()

	panel := m.renderNotificationCentre(m.height - headerHeight - footerHeight)
	if !strings.Contains(panel, notificationGroupClear) {
		t.Fatal("no group clear control in the rendered panel")
	}

	// The cursor sits on the first entry; the control clears the group it is
	// drawn on, not the selection's.
	m.notificationCentreCursor = 0
	source := notify.SourceOf(agent.Source).ID
	region := centreRegion(t, &m, regionNotificationCentreGroup+string(source))
	if got := m.notificationCentreMouse.HitMap.Test(region.Rect.X, region.Rect.Y); got == nil || got.ID != region.ID {
		t.Fatalf("the group × is covered by another region: %v", got)
	}
	// It is the last interior cell of the header row, where the rule ends.
	inner := m.notificationCentrePanelWidth() - 4
	wantX := m.width - m.notificationCentrePanelWidth() + 2 + notificationGroupClearCol(inner)
	if region.Rect.X != wantX || region.Rect.W != 1 {
		t.Fatalf("group × region = %+v, want x=%d w=1", region.Rect, wantX)
	}

	handled, _ := m.notificationCentreMouseEvent(
		tea.MouseClickMsg{Button: tea.MouseLeft, X: region.Rect.X, Y: region.Rect.Y})
	if !handled {
		t.Fatal("a click on the group × was not handled")
	}
	left := m.notificationCentreItems()
	if len(left) != 2 {
		t.Fatalf("items after the group × = %d, want 2", len(left))
	}
	for _, n := range left {
		if notify.SourceOf(n.Source).ID == source {
			t.Fatalf("the group × left %q behind", n.Title)
		}
	}
}

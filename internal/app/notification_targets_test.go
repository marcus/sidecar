package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/uirequest"
)

// postTargetNotification posts a notification with a body and returns it.
func postTargetNotification(t *testing.T, m *Model, title, body string, targets ...notify.Target) notify.Notification {
	t.Helper()
	stored, err := m.notifications.Post(notify.Notification{
		Source: notify.SourceSystem, Title: title, Body: body, Targets: targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	m.refreshNotifications()
	return stored
}

// activateOne runs a command and returns the single message it produced,
// flattening the batch the activation route returns.
func drainMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	out := []tea.Msg{}
	msg := cmd()
	switch typed := msg.(type) {
	case tea.BatchMsg:
		for _, sub := range typed {
			out = append(out, drainMsgs(t, sub)...)
		}
	default:
		out = append(out, msg)
	}
	return out
}

func activateTargetMsgFrom(t *testing.T, cmd tea.Cmd) (ActivateTargetMsg, bool) {
	t.Helper()
	for _, msg := range drainMsgs(t, cmd) {
		if activate, ok := msg.(ActivateTargetMsg); ok {
			return activate, true
		}
	}
	return ActivateTargetMsg{}, false
}

// `enter` on the centre is the notification's first call to action.
func TestCentreEnterActivatesTheFirstTarget(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postTargetNotification(t, &m, "review requested", "see td-4c1f9a for the diff")
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	handled, cmd := m.notificationCentreKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter was not consumed by the panel")
	}
	activate, ok := activateTargetMsgFrom(t, cmd)
	if !ok {
		t.Fatal("enter did not ask for an activation")
	}
	if activate.Target.Kind != uirequest.TargetKindIssue || activate.Target.Value != "td-4c1f9a" {
		t.Fatalf("activated %+v", activate.Target)
	}
	if !m.notificationCentreOpen || !m.notificationCentreFocused {
		t.Fatal("activation closed or blurred the panel")
	}
}

// Digits jump to a numbered target, and a digit past the list stays the
// project tab it is everywhere else in the shell.
func TestCentreDigitsJumpToNumberedTargets(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postTargetNotification(t, &m, "two targets", "td-4c1f9a and https://example.com/x")
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	handled, cmd := m.notificationCentreKey(tea.KeyPressMsg{Code: '2', Text: "2"})
	if !handled {
		t.Fatal("2 was not consumed while a second target exists")
	}
	activate, ok := activateTargetMsgFrom(t, cmd)
	if !ok || activate.Target.Kind != uirequest.TargetKindURL {
		t.Fatalf("2 activated %+v (ok=%v)", activate.Target, ok)
	}

	handled, _ = m.notificationCentreKey(tea.KeyPressMsg{Code: '3', Text: "3"})
	if handled {
		t.Fatal("3 was consumed with only two targets; it must stay a tab digit")
	}
	if m.notificationCentreFocused {
		t.Fatal("an unclaimed digit should release focus to the content, as before")
	}
}

// Re-show moved off `enter` onto `v`.
func TestCentreVShowsDetails(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	posted := postTargetNotification(t, &m, "review requested", "see td-4c1f9a")
	m.toggleNotificationCentre()
	m.readNotification(posted.ID)
	m.notificationCentreCursor = 0

	handled, _ := m.notificationCentreKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if !handled {
		t.Fatal("v was not consumed by the panel")
	}
	shown, ok := m.visibleToast()
	if !ok || shown.ID != posted.ID {
		t.Fatalf("visibleToast after v = %+v (ok=%v), want the selection", shown, ok)
	}
}

// A file target activates through the shared service and lands in the file
// browser at its line — the papercut the terminal surfaces still have.
func TestCentreFileTargetLandsAtItsLine(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "model.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := centreTestModel(t, &sizingPlugin{id: "file-browser"})
	m.ui.WorkDir = root
	postTargetNotification(t, &m, "build failed", "at internal/model.go:42")
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	_, cmd := m.notificationCentreKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	activate, ok := activateTargetMsgFrom(t, cmd)
	if !ok || activate.Target.Kind != uirequest.TargetKindFile || activate.Target.Line != 42 {
		t.Fatalf("enter activated %+v (ok=%v)", activate.Target, ok)
	}
	// And the shell turns that into the canonical navigation, line included.
	var navigate NavigateToFileMsg
	found := false
	for _, produced := range drainMsgs(t, m.activateTarget(activate)) {
		if nav, ok := produced.(NavigateToFileMsg); ok {
			navigate, found = nav, true
		}
	}
	if !found {
		t.Fatal("activation produced no NavigateToFileMsg")
	}
	if navigate.Path != "internal/model.go" || navigate.Line != 42 {
		t.Fatalf("navigate = %+v", navigate)
	}
}

// A path that does not exist in this checkout is neither underlined nor
// numbered: the verified-underline invariant for same-project targets.
func TestCentreDoesNotNumberUnverifiedFiles(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	m.ui.WorkDir = t.TempDir()
	posted := postTargetNotification(t, &m, "build failed", "at nowhere/missing.go:42")
	m.refreshNotifications()
	if ctas := m.notificationCallsToAction(posted); len(ctas) != 0 {
		t.Fatalf("got %+v, want no calls to action for a path that is not here", ctas)
	}
}

// The selection's targets row names each digit, and the target is underlined
// where it is written.
func TestCentreRendersNumberedUnderlinedTargets(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postTargetNotification(t, &m, "review requested", "see td-4c1f9a")
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	var body, targets string
	for _, row := range m.notificationCentreBody(notificationCentreDefaultWidth-4, time.Now()) {
		plain := ansi.Strip(row.text)
		switch {
		case strings.Contains(plain, "see td-4c1f9a"):
			body = row.text
		case strings.Contains(plain, "1 td-4c1f9a"):
			targets = row.text
		}
	}
	if body == "" {
		t.Fatal("the body row never rendered")
	}
	if !strings.Contains(body, "\x1b[4m") {
		t.Fatalf("the target was not underlined in the body: %q", body)
	}
	if targets == "" {
		t.Fatal("the selected entry has no numbered targets row")
	}
}

// The memo answers from cache and forgets records that leave the store.
func TestNotificationCTAMemoIsPrunedWithTheCache(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	posted := postTargetNotification(t, &m, "review requested", "see td-4c1f9a")
	if got := m.notificationCallsToAction(posted); len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if _, ok := m.notificationCTAs[posted.ID]; !ok {
		t.Fatal("the target list was not memoized")
	}
	m.notifications = notify.NewMemStore()
	m.refreshNotifications()
	if _, ok := m.notificationCTAs[posted.ID]; ok {
		t.Fatal("the memo kept a record the store no longer has")
	}
}

// A target in another checkout activates with its project attached: the
// activation service parks it, switches project, and lands afterwards. It is
// deliberately not existence-verified here — the resolvers are per-checkout —
// so it renders activatable and fails through the service's error path.
func TestCentreActivatesACrossProjectTarget(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postTargetNotification(t, &m, "fixed upstream", "",
		notify.Target{Kind: notify.TargetIssue, Value: "td-99aabb", Project: "/Users/x/code/braid"})
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	handled, cmd := m.notificationCentreKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter was not consumed by the panel")
	}
	activate, ok := activateTargetMsgFrom(t, cmd)
	if !ok {
		t.Fatal("enter produced no activation")
	}
	if activate.Project != "/Users/x/code/braid" {
		t.Fatalf("activation lost its project qualifier: %+v", activate)
	}
	if activate.Target.Kind != uirequest.TargetKindIssue || activate.Target.Value != "td-99aabb" {
		t.Fatalf("unexpected target: %+v", activate.Target)
	}

	// And it reads with the project in front of it in the numbered row.
	list := m.selectedNotificationTargets()
	if len(list) != 1 || list[0].Display() != "braid/td-99aabb" {
		t.Fatalf("numbered list = %+v", list)
	}
}

// A session named in an agent notification is a call to action from the
// centre: the scan finds Sidecar's own session names, so no poster has to
// attach one for the common case.
func TestCentreActivatesAScannedSessionTarget(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postTargetNotification(t, &m, "agent finished", "sidecar-ws-alpha is waiting for you")
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	handled, cmd := m.notificationCentreKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter was not consumed by the panel")
	}
	activate, ok := activateTargetMsgFrom(t, cmd)
	if !ok {
		t.Fatal("enter did not ask for an activation")
	}
	if activate.Target.Kind != uirequest.TargetKindSession || activate.Target.Value != "sidecar-ws-alpha" {
		t.Fatalf("activated %+v", activate.Target)
	}
}

// A task target has no detection pattern, so it only ever arrives attached —
// and when it does, it is numbered and it jumps.
func TestCentreActivatesAnAttachedTaskTarget(t *testing.T) {
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	postTargetNotification(t, &m, "task due", "pay the invoice",
		notify.Target{Kind: notify.TargetTask, Value: "a1b2c3d4"})
	m.toggleNotificationCentre()
	m.notificationCentreCursor = 0

	handled, cmd := m.notificationCentreKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter was not consumed by the panel")
	}
	activate, ok := activateTargetMsgFrom(t, cmd)
	if !ok {
		t.Fatal("enter did not ask for an activation")
	}
	if activate.Target.Kind != uirequest.TargetKindTask || activate.Target.Value != "a1b2c3d4" {
		t.Fatalf("activated %+v", activate.Target)
	}
}

// The store is global, so the same record is looked at from more than one
// project in a session. The verification memo belongs to the checkout it was
// computed in: after a switch, a file that only exists in the previous project
// is neither numbered nor underlined here.
func TestCentreTargetMemoIsPerCheckout(t *testing.T) {
	rootA := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "model.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := centreTestModel(t, &sizingPlugin{id: "files"})
	m.ui.WorkDir = rootA
	posted := postTargetNotification(t, &m, "build failed", "at model.go:42")
	m.refreshNotifications()
	if got := m.notificationCallsToAction(posted); len(got) != 1 {
		t.Fatalf("in the project that has the file: %+v", got)
	}
	m.ui.WorkDir = t.TempDir()
	if got := m.notificationCallsToAction(posted); len(got) != 0 {
		t.Fatalf("after the switch, before any refresh: %+v", got)
	}
	m.refreshNotifications()
	if got := m.notificationCallsToAction(posted); len(got) != 0 {
		t.Fatalf("after the switch and a refresh: %+v", got)
	}
}

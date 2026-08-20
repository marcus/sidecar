package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
)

// notifyEnv is a CLI environment pinned to a private state dir, with tmux
// deliberately out of the picture so the tests never touch a live server.
func notifyEnv(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	var out, errOut bytes.Buffer
	return Env{Stdout: &out, Stderr: &errOut, StateDir: t.TempDir()}, &out, &errOut
}

func TestNotifyPostFallsBackToTheLog(t *testing.T) {
	env, out, errOut := notifyEnv(t)

	if code := runNotifyPost(env, []string{"--body", "detail", "--source", "session", "Tests are green"}); code != 0 {
		t.Fatalf("post = %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no running Sidecar instance") {
		t.Fatalf("expected the fallback to be reported, got %q", out.String())
	}

	all, err := notify.ReadAll(notify.Path(env.StateDir))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 notification in the log, got %d", len(all))
	}
	if all[0].Title != "Tests are green" || all[0].Body != "detail" || all[0].Source != notify.SourceSession {
		t.Fatalf("post did not record its flags: %+v", all[0])
	}
	if all[0].Origin.Zero() {
		t.Fatalf("a posted notification must carry its caller's origin")
	}
}

func TestNotifyPostValidates(t *testing.T) {
	env, _, errOut := notifyEnv(t)
	if code := runNotifyPost(env, []string{}); code != 2 {
		t.Fatalf("missing title should be a usage error, got %d", code)
	}
	if code := runNotifyPost(env, []string{"--source", "nope", "title"}); code != 2 {
		t.Fatalf("unknown source should be a usage error, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown source") {
		t.Fatalf("expected the valid sources to be named, got %q", errOut.String())
	}
	if code := runNotifyPost(env, []string{"--expiry", "soon", "title"}); code != 2 {
		t.Fatalf("unparseable expiry should be a usage error, got %d", code)
	}
}

func TestNotifyPostExpiryFlag(t *testing.T) {
	env, _, errOut := notifyEnv(t)
	if code := runNotifyPost(env, []string{"--expiry", "never", "waiting on you"}); code != 0 {
		t.Fatalf("post = %d, stderr %q", code, errOut.String())
	}
	all, _ := notify.ReadAll(notify.Path(env.StateDir))
	if len(all) != 1 || !all[0].Sticky || all[0].ExpiresAt != nil {
		t.Fatalf("--expiry never should be sticky: %+v", all)
	}
}

func TestNotifyListReadsTheLogWithNoInstance(t *testing.T) {
	env, out, errOut := notifyEnv(t)
	if code := runNotifyList(env, nil); code != 0 || !strings.Contains(out.String(), "No notifications") {
		t.Fatalf("empty list = %d, %q", code, out.String())
	}

	if code := runNotifyPost(env, []string{"one"}); code != 0 {
		t.Fatalf("post: %d %q", code, errOut.String())
	}
	out.Reset()
	if code := runNotifyList(env, []string{"--json"}); code != 0 {
		t.Fatalf("list --json = %d, stderr %q", code, errOut.String())
	}
	var res struct {
		Unread int                   `json:"unread"`
		Items  []notify.Notification `json:"items"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("parse list json: %v (%q)", err, out.String())
	}
	if res.Unread != 1 || len(res.Items) != 1 || res.Items[0].Title != "one" {
		t.Fatalf("unexpected list result: %+v", res)
	}
	if code := runNotifyList(env, []string{"--nope"}); code != 2 {
		t.Fatalf("unknown option should be a usage error, got %d", code)
	}
}

func TestNotifyDismissIsOriginChecked(t *testing.T) {
	env, out, errOut := notifyEnv(t)

	// Something posted by somebody else, written straight into the log.
	store, err := notify.Open(env.StateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	theirs, err := store.Post(notify.Notification{
		ID:     "ntf-theirs",
		Source: notify.SourceAgent,
		Title:  "another agent's",
		Origin: notify.Origin{TmuxSession: "sidecar-sh-someone-else"},
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	_ = store.Close()

	if code := runNotifyDismiss(env, []string{theirs.ID}); code != 4 {
		t.Fatalf("dismissing another caller's notification = %d, want 4", code)
	}
	if !strings.Contains(errOut.String(), "only dismiss its own") {
		t.Fatalf("expected a refusal that says why, got %q", errOut.String())
	}
	if code := runNotifyDismiss(env, []string{"ntf-missing"}); code != 3 {
		t.Fatalf("unknown id = %d, want 3", code)
	}

	// And one this caller posted.
	errOut.Reset()
	if code := runNotifyPost(env, []string{"mine"}); code != 0 {
		t.Fatalf("post: %d %q", code, errOut.String())
	}
	all, _ := notify.ReadAll(notify.Path(env.StateDir))
	mineID := ""
	for _, n := range all {
		if n.Title == "mine" {
			mineID = n.ID
		}
	}
	if mineID == "" {
		t.Fatalf("posted notification not found in %+v", all)
	}
	out.Reset()
	if code := runNotifyDismiss(env, []string{mineID}); code != 0 {
		t.Fatalf("dismissing my own = %d, stderr %q", code, errOut.String())
	}

	after, _ := notify.ReadAll(notify.Path(env.StateDir))
	for _, n := range after {
		if n.ID == mineID && !n.Dismissed() {
			t.Fatalf("dismiss did not stick: %+v", n)
		}
		if n.ID == theirs.ID && n.Dismissed() {
			t.Fatalf("refused dismissal must not have been applied")
		}
	}
	if notify.UnreadCount(after) != 1 {
		t.Fatalf("only the other caller's notification stays unread, got %d", notify.UnreadCount(after))
	}
}

func TestNotifyRootDispatches(t *testing.T) {
	env, out, errOut := notifyEnv(t)
	if code := runNotifyRoot(env, nil); code != 0 || !strings.Contains(out.String(), "sidecar notify") {
		t.Fatalf("bare notify should print help, got %d %q", code, out.String())
	}
	if code := runNotifyRoot(env, []string{"nonsense"}); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown notify command") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	out.Reset()
	if code := runNotifyRoot(env, []string{"list"}); code != 0 {
		t.Fatalf("notify list via root = %d", code)
	}
}

// The app re-applies the origin check on its own side, so the request has to
// name the caller. Naming the target's origin made the host compare the record
// against itself and pass every time.
func TestNotifyDismissRequestCarriesTheCallersOrigin(t *testing.T) {
	caller := notify.Origin{TmuxSession: "sidecar-sh-caller", WorkDir: "/tmp/caller"}
	req := notifyDismissRequest(caller, "ntf-123")

	if req.Origin.TmuxSession != caller.TmuxSession || req.Origin.WorkDir != caller.WorkDir {
		t.Fatalf("request origin = %+v, want the caller's", req.Origin)
	}
	if req.Target.Value != "ntf-123" {
		t.Fatalf("target value = %q, want the notification id", req.Target.Value)
	}
	// The record's own origin must be nowhere in the request: it is exactly
	// what the host would then be checking against itself.
	poster := notify.Origin{TmuxSession: "sidecar-sh-poster"}
	if req.Origin.TmuxSession == poster.TmuxSession {
		t.Fatal("the request must not carry the poster's origin")
	}
}

// The fallback message has to be true. Every failure used to read "no running
// Sidecar instance", which sent the user looking for a process that was on
// screen in front of them: the instance was running, it had simply not claimed
// the request. Now each outcome says what actually happened.
func TestNotifyPostReportsWhyItFellBack(t *testing.T) {
	env, out, errOut := notifyEnv(t)

	// A live announced instance that never answers: this process is one.
	if err := uirequest.Announce(env.StateDir, uirequest.Instance{
		PID:     os.Getpid(),
		Project: "somewhere-else",
		WorkDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("announce: %v", err)
	}

	if code := runNotifyPost(env, []string{"Tests are green"}); code != 0 {
		t.Fatalf("post = %d, stderr %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "no running Sidecar instance,") {
		t.Fatalf("an instance was running; the message denied it: %q", out.String())
	}
	if !strings.Contains(out.String(), "did not answer in time") {
		t.Fatalf("expected the unanswered outcome, got %q", out.String())
	}
	if all, err := notify.ReadAll(notify.Path(env.StateDir)); err != nil || len(all) != 1 {
		t.Fatalf("the notification must still be filed: %d, %v", len(all), err)
	}
}

// --target is the precise call to action an agent attaches when the prose does
// not spell the target out. It must survive the whole way to the log, in order,
// with its project qualifier intact.
func TestNotifyPostStoresTargets(t *testing.T) {
	env, _, errOut := notifyEnv(t)

	code := runNotifyPost(env, []string{
		"--target", "issue:td-4c1f9a",
		"--target=file:internal/app/model.go:42",
		"--target", "issue:td-99aabb@braid",
		"Review this",
	})
	if code != 0 {
		t.Fatalf("post = %d, stderr %q", code, errOut.String())
	}

	all, err := notify.ReadAll(notify.Path(env.StateDir))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(all))
	}
	want := []notify.Target{
		{Kind: notify.TargetIssue, Value: "td-4c1f9a"},
		{Kind: notify.TargetFile, Value: "internal/app/model.go", Line: 42},
		{Kind: notify.TargetIssue, Value: "td-99aabb", Project: "braid"},
	}
	got := all[0].Targets
	if len(got) != len(want) {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The bus route carries the same record: the request payload is the
	// notification itself, so a delivered post keeps its targets too.
	list := notify.CallsToAction(all[0], terminallink.Options{})
	if len(list) != 3 || list[2].Display() != "braid/td-99aabb" {
		t.Fatalf("stored targets did not reconcile into the numbered list: %+v", list)
	}
}

func TestNotifyPostRefusesAMalformedTarget(t *testing.T) {
	env, _, errOut := notifyEnv(t)
	if code := runNotifyPost(env, []string{"--target", "branch:main", "title"}); code != 2 {
		t.Fatalf("a malformed target should be a usage error, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown target kind") {
		t.Fatalf("expected the kinds to be named, got %q", errOut.String())
	}
	if code := runNotifyPost(env, []string{"--target", "title"}); code != 2 {
		t.Fatalf("--target with no spec should be a usage error, got %d", code)
	}
	all, _ := notify.ReadAll(notify.Path(env.StateDir))
	if len(all) != 0 {
		t.Fatalf("a refused post must store nothing, got %d records", len(all))
	}
}

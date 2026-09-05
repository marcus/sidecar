package agentcontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
)

// stageTerminal is a Terminal whose world a test can change between
// observations. Its screen is a small encoding — "kind:status[:stale]" — read
// by stageDetect, so lifecycle scenarios are written as the states they mean
// rather than as provider fixture text.
type stageTerminal struct {
	mu        sync.Mutex
	stage     string
	target    Target
	dead      bool
	copyMode  bool
	paneCount int
	panePID   int
	// currentCommand and processIdentity default to the fake provider; a test
	// that wants the real detector names a provider the catalog knows.
	currentCommand  string
	processIdentity string
	inspects        int
	onInspect       func(t *stageTerminal, n int)
	inspectErr      error
	// beforeWrite runs once, inside the mutating path and under the lock, just
	// after the service handed over its pinned snapshot and just before the
	// adapter re-proves it. That gap is the real race a replacement wins.
	beforeWrite func(t *stageTerminal)
	submitted   []string
	submitErr   error
	keys        []string
	captures    []ReadRequest
	captureErr  error
}

func newStage(stage string) *stageTerminal {
	return &stageTerminal{
		stage:           stage,
		target:          Target{Host: "local", Project: "p", Session: "s", Namespace: "n", PaneID: "%1", ServerPID: 7, ServerIncarnation: "server-1"},
		paneCount:       1,
		panePID:         42,
		currentCommand:  "fake",
		processIdentity: "fake",
	}
}

func (t *stageTerminal) set(stage string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stage = stage
}

func (t *stageTerminal) Inspect(context.Context, Target) (Snapshot, error) {
	t.mu.Lock()
	t.inspects++
	n := t.inspects
	hook := t.onInspect
	t.mu.Unlock()
	if hook != nil {
		hook(t, n)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inspectErr != nil {
		return Snapshot{}, t.inspectErr
	}
	target := t.target
	target.PanePID = t.panePID
	return Snapshot{
		Target:          target,
		Dead:            t.dead,
		CopyMode:        t.copyMode,
		PaneCount:       t.paneCount,
		CurrentCommand:  t.currentCommand,
		ProcessIdentity: t.processIdentity,
		Screen:          t.stage,
		CapturedAt:      time.Unix(100, int64(n)),
	}, nil
}

func (t *stageTerminal) Launch(context.Context, Snapshot, []string) error { return nil }

// replaced re-proves the pinned snapshot against the current occupant, which is
// the mutating half of the Terminal contract: every adapter is handed an
// already-pinned Snapshot precisely so it can refuse a pane that changed hands
// between the preflight observation and the write. LocalTerminal does this with
// a second tmux inspection; modelling it here is what lets the service's
// "nothing is sent to a replaced target" promise be tested at all.
func (t *stageTerminal) replaced(snap Snapshot, what string) error {
	if t.beforeWrite != nil {
		hook := t.beforeWrite
		t.beforeWrite = nil
		hook(t)
	}
	current := t.target
	current.PanePID = t.panePID
	if sameOccupant(snap.Target, current) {
		return nil
	}
	pinned := snap.Target
	return &Error{Code: ErrReplaced, Message: "managed pane was replaced before " + what, Target: &pinned}
}

func (t *stageTerminal) Submit(_ context.Context, snap Snapshot, text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.replaced(snap, "prompt"); err != nil {
		return err
	}
	if t.submitErr != nil {
		return t.submitErr
	}
	t.submitted = append(t.submitted, text)
	return nil
}

// SendKeys deliberately does NOT validate. It stands in for an adapter that
// trusts what it is handed — the shape a remote adapter is free to take — so
// the refusal of an unencodable key can only be the service's own.
func (t *stageTerminal) SendKeys(_ context.Context, snap Snapshot, names []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.replaced(snap, "keys"); err != nil {
		return err
	}
	t.keys = append(t.keys, names...)
	return nil
}

func (t *stageTerminal) Capture(_ context.Context, snap Snapshot, req ReadRequest) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.captures = append(t.captures, req)
	if t.captureErr != nil {
		return "", t.captureErr
	}
	return string(req.Source) + " of " + snap.Screen, nil
}

func (t *stageTerminal) wrote() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.submitted...)
}

func stageDetect(s Snapshot, _ *agentactivity.Tracker) AgentState {
	parts := strings.Split(s.Screen, ":")
	state := AgentState{Freshness: "current", CapturedAt: s.CapturedAt, Evidence: "stage." + s.Screen}
	if len(parts) > 0 && parts[0] != "" {
		state.Kind = parts[0]
	}
	state.Status = StatusUnknown
	if len(parts) > 1 {
		state.Status = Status(parts[1])
	}
	if len(parts) > 2 && parts[2] == "stale" {
		state.Freshness = "stale"
	}
	state.InteractiveReady = state.Kind != "" && (state.Status == StatusIdle || state.Status == StatusDone)
	return state
}

func stageService(terminal *stageTerminal) Service {
	return Service{Terminal: terminal, Poll: time.Millisecond, Observe: time.Millisecond, Verify: time.Millisecond, StallAfter: 100 * time.Millisecond, Detect: stageDetect}
}

func codeOf(t *testing.T, err error) ErrorCode {
	t.Helper()
	var typed *Error
	if !AsError(err, &typed) {
		t.Fatalf("err = %T %v, want a typed agentcontrol error", err, err)
	}
	return typed.Code
}

func TestPromptRefusesEveryUnpromptableTargetWithoutWritingBytes(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*stageTerminal)
		want    ErrorCode
	}{
		{"blocked", func(s *stageTerminal) { s.stage = "fake:blocked" }, ErrBlocked},
		{"no provider identified", func(s *stageTerminal) { s.stage = ":unknown" }, ErrNotReady},
		{"stale status", func(s *stageTerminal) { s.stage = "fake:idle:stale" }, ErrNotReady},
		{"unknown status", func(s *stageTerminal) { s.stage = "fake:unknown" }, ErrNotReady},
		{"dead pane", func(s *stageTerminal) { s.dead = true }, ErrPaneBusy},
		{"copy mode", func(s *stageTerminal) { s.copyMode = true }, ErrPaneBusy},
		{"ambiguous session", func(s *stageTerminal) { s.paneCount = 2 }, ErrPaneBusy},
		{"incomplete identity", func(s *stageTerminal) { s.panePID = 0 }, ErrPaneBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terminal := newStage("fake:idle")
			tc.prepare(terminal)
			_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go"})
			if got := codeOf(t, err); got != tc.want {
				t.Fatalf("Prompt() code = %s, want %s", got, tc.want)
			}
			if wrote := terminal.wrote(); len(wrote) != 0 {
				t.Fatalf("refusal wrote %q to the pane", wrote)
			}
			var typed *Error
			if !AsError(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != SubmissionNotSubmitted || typed.Receipt.Wait != PromptWaitNotStarted {
				t.Fatalf("refusal receipt = %+v, want not_submitted/not_started", typed)
			}
		})
	}
}

func TestPromptWriteFailureKeepsSubmissionUnknown(t *testing.T) {
	terminal := newStage("fake:idle")
	terminal.submitErr = errors.New("connection dropped during write")
	_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go"})
	if got := codeOf(t, err); got != ErrTransport {
		t.Fatalf("code = %s, want %s", got, ErrTransport)
	}
	var typed *Error
	if !AsError(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != SubmissionUnknown {
		t.Fatalf("write failure receipt = %+v, want unknown", typed)
	}
}

func TestPromptObservationFailureKeepsTheSubmittedTargetPin(t *testing.T) {
	terminal := newStage("fake:idle")
	want := terminal.target
	want.PanePID = terminal.panePID
	terminal.onInspect = func(s *stageTerminal, n int) {
		if n >= 2 {
			s.mu.Lock()
			s.inspectErr = &Error{Code: ErrTransport, Message: "observer disconnected"}
			s.mu.Unlock()
		}
	}
	_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go"})
	if got := codeOf(t, err); got != ErrTransport {
		t.Fatalf("code = %s, want %s", got, ErrTransport)
	}
	var typed *Error
	if !AsError(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != SubmissionSubmitted || typed.Receipt.Target != want {
		t.Fatalf("observation failure receipt = %+v, want submitted target %+v", typed, want)
	}
}

func TestPromptPostWriteReplacementReportsSubmittedUnderOriginalPin(t *testing.T) {
	terminal := newStage("fake:idle")
	want := terminal.target
	want.PanePID = terminal.panePID
	terminal.onInspect = func(s *stageTerminal, n int) {
		if n >= 2 {
			s.set("other:idle")
		}
	}
	_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go"})
	if got := codeOf(t, err); got != ErrReplaced {
		t.Fatalf("code = %s, want %s", got, ErrReplaced)
	}
	var typed *Error
	if !AsError(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != SubmissionSubmitted || typed.Receipt.Wait != PromptWaitNotRequested || typed.Receipt.Target != want {
		t.Fatalf("replacement receipt = %+v, want submitted original target %+v", typed, want)
	}
	if wrote := terminal.wrote(); len(wrote) != 1 || wrote[0] != "go" {
		t.Fatalf("submitted = %q", wrote)
	}
}

func TestPromptReportsStalledWhenTheLifecycleNeverMoves(t *testing.T) {
	terminal := newStage("fake:idle")
	_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "review the diff"})
	if got := codeOf(t, err); got != ErrPromptStalled {
		t.Fatalf("Prompt() code = %s, want %s", got, ErrPromptStalled)
	}
	// The bytes did go out — a stall is a report about the agent, not a claim
	// that nothing was written.
	if wrote := terminal.wrote(); len(wrote) != 1 || wrote[0] != "review the diff" {
		t.Fatalf("submitted = %q", wrote)
	}
	var typed *Error
	if !AsError(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != SubmissionSubmitted || typed.Receipt.Wait != PromptWaitNotRequested {
		t.Fatalf("stall receipt = %+v", typed)
	}
}

// TestPromptStallRuleHoldsUnderTheRealDetector runs the rule through the
// package's own detect and a checked-in provider fixture rather than the stage
// detector, because the stage detector answers from the screen string alone and
// cannot see the thing that broke this rule in practice: the real detector's
// verdict depends on the lifecycle tracker it is given, so reading "before" and
// "after" through two differently seeded trackers made the reseeding itself
// look like the agent reacting. A prompt into a pane that never changed
// returned success. Found by running the real binary against an isolated tmux
// server; the mechanism needs the real detector, so the test uses it.
func TestPromptStallRuleHoldsUnderTheRealDetector(t *testing.T) {
	idle, err := os.ReadFile(filepath.Join("..", "agentactivity", "testdata", "codex", "startup_idle.txt"))
	if err != nil {
		t.Fatal(err)
	}
	terminal := newStage(string(idle))
	terminal.currentCommand, terminal.processIdentity = "codex", "codex"
	svc := stageService(terminal)
	svc.Detect = nil // the real detector

	_, err = svc.Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "review the diff"})
	if got := codeOf(t, err); got != ErrPromptStalled {
		t.Fatalf("Prompt() code = %s, want %s; a screen that never changed reported success", got, ErrPromptStalled)
	}
	if wrote := terminal.wrote(); len(wrote) != 1 {
		t.Fatalf("submitted = %q", wrote)
	}
}

// TestPromptStallRuleIgnoresAStatusThatBecameUnknown pins the other half of the
// rule. unknown is Sidecar losing sight of the agent, not the agent reacting,
// and accepting it would turn "the prompt landed" into "something changed on
// screen" — which is exactly what the rule exists to distinguish.
func TestPromptStallRuleIgnoresAStatusThatBecameUnknown(t *testing.T) {
	terminal := newStage("fake:idle")
	terminal.onInspect = func(s *stageTerminal, n int) {
		if n >= 2 {
			s.set("fake:unknown")
		}
	}
	_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go"})
	if got := codeOf(t, err); got != ErrPromptStalled {
		t.Fatalf("Prompt() code = %s, want %s; losing track of the agent was read as it reacting", got, ErrPromptStalled)
	}
}

func TestPromptReturnsAtTheObservedLifecycleChangeWithoutWait(t *testing.T) {
	terminal := newStage("fake:idle")
	terminal.onInspect = func(s *stageTerminal, n int) {
		if n >= 2 {
			s.set("fake:working")
		}
	}
	got, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Status != StatusWorking {
		t.Fatalf("agent = %+v, want the observed working transition", got.Agent)
	}
}

func TestPromptIntoAWorkingAgentMakesNoTurnClaim(t *testing.T) {
	terminal := newStage("fake:working")
	got, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "and also this"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Status != StatusWorking {
		t.Fatalf("agent = %+v", got.Agent)
	}
	if terminal.inspects != 1 {
		t.Fatalf("inspected %d times; an already-working prompt has nothing honest to observe", terminal.inspects)
	}
}

func TestPromptWaitRequiresAnExplicitTimeout(t *testing.T) {
	terminal := newStage("fake:idle")
	_, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go", Wait: true})
	if got := codeOf(t, err); got != ErrNotReady {
		t.Fatalf("code = %s", got)
	}
	if wrote := terminal.wrote(); len(wrote) != 0 {
		t.Fatalf("a usage refusal wrote %q", wrote)
	}
	if _, err := stageService(terminal).Wait(context.Background(), WaitRequest{Target: terminal.target}); codeOf(t, err) != ErrNotReady {
		t.Fatalf("Wait() accepted an implicit timeout")
	}
}

func TestPromptWaitRunsSubmissionAndSettleUnderOnePin(t *testing.T) {
	terminal := newStage("fake:idle")
	terminal.onInspect = func(s *stageTerminal, n int) {
		switch {
		case n >= 4:
			s.set("fake:done")
		case n >= 2:
			s.set("fake:working")
		}
	}
	got, err := stageService(terminal).Prompt(context.Background(), PromptRequest{Target: terminal.target, Text: "go", Wait: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Status != StatusDone || got.Target.PaneID != "%1" {
		t.Fatalf("agent = %+v", got)
	}
	if got.Receipt.Submission != SubmissionSubmitted || got.Receipt.Wait != PromptWaitSettled || got.Receipt.Target != got.Target {
		t.Fatalf("receipt = %+v, target = %+v", got.Receipt, got.Target)
	}
}

// The documented caveat path: `--wait` from an already-working agent. There is
// no new turn to identify, so the prompt makes no turn claim and the wait is
// allowed to be satisfied by the completion of the turn that was already
// running. That is a deliberate weakening of the stall rule, and help text says
// so, which makes it worth pinning rather than leaving to the reader.
func TestPromptWaitFromAWorkingAgentIsSatisfiedByTheRunningTurn(t *testing.T) {
	terminal := newStage("fake:working")
	terminal.onInspect = func(s *stageTerminal, n int) {
		if n >= 3 {
			s.set("fake:done")
		}
	}
	got, err := stageService(terminal).Prompt(context.Background(),
		PromptRequest{Target: terminal.target, Text: "and also this", Wait: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Status != StatusDone {
		t.Fatalf("agent = %+v, want the running turn's completion to settle the wait", got.Agent)
	}
	// The text still went out; the caveat is about what may be concluded from
	// the settle, not about withholding the prompt.
	if wrote := terminal.wrote(); len(wrote) != 1 || wrote[0] != "and also this" {
		t.Fatalf("submitted = %q", wrote)
	}
	// And it never went through the stall rule: no prompt-landed observation is
	// possible when the agent was already working, so this cannot be reported
	// as stalled however long the existing turn takes to change status.
	terminal2 := newStage("fake:working")
	if _, err := stageService(terminal2).Prompt(context.Background(),
		PromptRequest{Target: terminal2.target, Text: "again", Wait: true, Timeout: 200 * time.Millisecond}); codeOf(t, err) != ErrTimeout {
		t.Fatalf("a never-finishing turn = %v, want a timeout rather than a stall", err)
	} else {
		var typed *Error
		if !AsError(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != SubmissionSubmitted || typed.Receipt.Wait != PromptWaitTimeout || typed.Receipt.Target.PaneID != "%1" {
			t.Fatalf("timeout receipt = %+v", typed)
		}
	}
}

func TestWaitAcceptsOnlyTheNamedStates(t *testing.T) {
	terminal := newStage("fake:working")
	terminal.onInspect = func(s *stageTerminal, n int) {
		if n >= 3 {
			s.set("fake:blocked")
		}
	}
	// The default set settles on blocked as well as idle and done: a wait that
	// ignored blocked would hang until its timeout on the exact case a caller
	// most needs to hear about.
	got, err := stageService(terminal).Wait(context.Background(), WaitRequest{Target: terminal.target, Timeout: 5 * time.Second})
	if err != nil || got.Agent.Status != StatusBlocked {
		t.Fatalf("Wait() = %+v, %v", got, err)
	}

	narrowed := newStage("fake:blocked")
	_, err = stageService(narrowed).Wait(context.Background(), WaitRequest{Target: narrowed.target, Until: []Status{StatusDone}, Timeout: 80 * time.Millisecond})
	if got := codeOf(t, err); got != ErrTimeout {
		t.Fatalf("narrowed wait code = %s, want %s", got, ErrTimeout)
	}
}

func TestWaitCannotBeSatisfiedByAReplacementOccupant(t *testing.T) {
	t.Run("a different provider takes the composer", func(t *testing.T) {
		terminal := newStage("fake:working")
		terminal.onInspect = func(s *stageTerminal, n int) {
			if n >= 3 {
				s.set("other:idle")
			}
		}
		_, err := stageService(terminal).Wait(context.Background(), WaitRequest{Target: terminal.target, Timeout: 5 * time.Second})
		if got := codeOf(t, err); got != ErrReplaced {
			t.Fatalf("code = %s, want %s", got, ErrReplaced)
		}
	})

	t.Run("the pane itself is replaced", func(t *testing.T) {
		terminal := newStage("fake:working")
		terminal.onInspect = func(s *stageTerminal, n int) {
			if n >= 3 {
				s.mu.Lock()
				s.panePID = 4242
				s.stage = "fake:done"
				s.mu.Unlock()
			}
		}
		_, err := stageService(terminal).Wait(context.Background(), WaitRequest{Target: terminal.target, Timeout: 5 * time.Second})
		if got := codeOf(t, err); got != ErrReplaced {
			t.Fatalf("code = %s, want %s", got, ErrReplaced)
		}
	})

	t.Run("the pane dies", func(t *testing.T) {
		terminal := newStage("fake:working")
		terminal.onInspect = func(s *stageTerminal, n int) {
			if n >= 3 {
				s.mu.Lock()
				s.dead = true
				s.stage = "fake:done"
				s.mu.Unlock()
			}
		}
		_, err := stageService(terminal).Wait(context.Background(), WaitRequest{Target: terminal.target, Timeout: 5 * time.Second})
		if got := codeOf(t, err); got != ErrReplaced {
			t.Fatalf("code = %s, want %s", got, ErrReplaced)
		}
	})
}

func TestWaitRefusesATargetWithNoIdentifiedProvider(t *testing.T) {
	terminal := newStage(":unknown")
	_, err := stageService(terminal).Wait(context.Background(), WaitRequest{Target: terminal.target, Timeout: time.Second})
	if got := codeOf(t, err); got != ErrNotReady {
		t.Fatalf("code = %s", got)
	}
}

func TestWaitReportsCallerCancellationAsTransportNotTimeout(t *testing.T) {
	terminal := newStage("fake:working")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := stageService(terminal).Wait(ctx, WaitRequest{Target: terminal.target, Timeout: 5 * time.Second})
	if got := codeOf(t, err); got != ErrTransport {
		t.Fatalf("code = %s, want %s", got, ErrTransport)
	}
}

// The adapter under this service records whatever it is handed without
// checking it, so the refusal below is the service's own. If the validation
// moved back down into an adapter, this fake would happily write "down" and
// "enter" and the test would fail on the keys assertion rather than passing
// because something, somewhere, happened to check.
func TestSendKeysValidatesTheWholeListBeforeWritingAny(t *testing.T) {
	terminal := newStage("fake:blocked")
	svc := stageService(terminal)
	_, err := svc.SendKeys(context.Background(), KeysRequest{Target: terminal.target, Keys: []string{"down", "enter", "cmd+q"}})
	if err == nil {
		t.Fatal("SendKeys accepted an unencodable key")
	}
	if got := codeOf(t, err); got != ErrNotReady {
		t.Fatalf("unencodable key = %s, want %s", got, ErrNotReady)
	}
	if len(terminal.keys) != 0 {
		t.Fatalf("rejected sequence still wrote %q", terminal.keys)
	}
	// Nothing was observed either: a name that cannot be encoded is answered
	// before the target costs a terminal round trip.
	if terminal.inspects != 0 {
		t.Fatalf("an unencodable key cost %d inspections", terminal.inspects)
	}

	// A blocked agent is exactly what send-keys is for, so it is not refused.
	got, err := svc.SendKeys(context.Background(), KeysRequest{Target: terminal.target, Keys: []string{"down", "enter"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Status != StatusBlocked || strings.Join(terminal.keys, ",") != "down,enter" {
		t.Fatalf("agent = %+v keys = %q", got.Agent, terminal.keys)
	}

	if _, err := svc.SendKeys(context.Background(), KeysRequest{Target: terminal.target}); err == nil {
		t.Fatal("SendKeys accepted an empty sequence")
	}
}

// "Refusals happen before any byte is written ... a replaced one gets
// agent_replaced ... nothing is sent in any of them" is the documented promise
// for prompt and send-keys alike. The pane can change hands after the preflight
// observation and before the write, so the refusal has to come from the write
// path re-proving its pin, and the thing worth testing is that the service
// surfaces it as agent_replaced with nothing delivered.
func TestPromptAndSendKeysWriteNothingToAReplacedTarget(t *testing.T) {
	t.Run("prompt", func(t *testing.T) {
		terminal := newStage("fake:idle")
		// Something else takes the pane after the preflight pinned it, so the
		// snapshot the service carries into Submit is already stale.
		terminal.beforeWrite = func(s *stageTerminal) { s.panePID = 4242 }
		_, err := stageService(terminal).Prompt(context.Background(),
			PromptRequest{Target: terminal.target, Text: "review the diff"})
		if got := codeOf(t, err); got != ErrReplaced {
			t.Fatalf("code = %s, want %s", got, ErrReplaced)
		}
		if len(terminal.submitted) != 0 {
			t.Fatalf("a replaced target still received %q", terminal.submitted)
		}
	})

	t.Run("send-keys", func(t *testing.T) {
		terminal := newStage("fake:blocked")
		terminal.beforeWrite = func(s *stageTerminal) { s.panePID = 4242 }
		_, err := stageService(terminal).SendKeys(context.Background(),
			KeysRequest{Target: terminal.target, Keys: []string{"down", "enter"}})
		if got := codeOf(t, err); got != ErrReplaced {
			t.Fatalf("code = %s, want %s", got, ErrReplaced)
		}
		if len(terminal.keys) != 0 {
			t.Fatalf("a replaced target still received %q", terminal.keys)
		}
	})
}

func TestSendKeysRefusesAPaneWithNoIdentifiedProvider(t *testing.T) {
	terminal := newStage(":unknown")
	_, err := stageService(terminal).SendKeys(context.Background(), KeysRequest{Target: terminal.target, Keys: []string{"enter"}})
	if got := codeOf(t, err); got != ErrNotReady {
		t.Fatalf("code = %s", got)
	}
	if len(terminal.keys) != 0 {
		t.Fatalf("wrote %q into an unidentified pane", terminal.keys)
	}
}

func TestReadPassesEverySourceThroughAndBoundsLines(t *testing.T) {
	terminal := newStage("fake:idle")
	svc := stageService(terminal)
	for _, source := range []ReadSource{SourceVisible, SourceRecent, SourceRecentUnwrapped, SourceDetection} {
		got, err := svc.Read(context.Background(), ReadRequest{Target: terminal.target, Source: source, Lines: 40, ANSI: true})
		if err != nil {
			t.Fatalf("Read(%s): %v", source, err)
		}
		if got.Source != source || !strings.HasPrefix(got.Text, string(source)+" of ") {
			t.Fatalf("Read(%s) = %+v", source, got)
		}
		if got.Kind != "fake" || got.Status != StatusIdle {
			t.Fatalf("Read(%s) lost the lifecycle context: %+v", source, got)
		}
	}
	last := terminal.captures[len(terminal.captures)-1]
	if last.Lines != 40 || !last.ANSI {
		t.Fatalf("capture request = %+v", last)
	}
	if _, err := svc.Read(context.Background(), ReadRequest{Target: terminal.target, Source: "screenshot"}); codeOf(t, err) != ErrNotReady {
		t.Fatal("Read accepted an unknown source")
	}
	// An omitted source is the visible screen, not an error. The default has
	// to reach Capture too: leaving it only on the result JSON is how a live
	// LocalTerminal used to refuse with `source "" is not a terminal capture`.
	if got, err := svc.Read(context.Background(), ReadRequest{Target: terminal.target}); err != nil || got.Source != SourceVisible {
		t.Fatalf("default source = %+v, %v", got, err)
	}
	if last := terminal.captures[len(terminal.captures)-1]; last.Source != SourceVisible {
		t.Fatalf("omitted source was captured as %q", last.Source)
	}
}

type fixedTranscript struct {
	messages []TranscriptMessage
	err      error
}

func (f fixedTranscript) SessionMessages(context.Context, Target, int) ([]TranscriptMessage, error) {
	return f.messages, f.err
}

func TestTranscriptReadIsUnavailableUntilAnExactBindingExists(t *testing.T) {
	terminal := newStage("fake:idle")
	_, err := stageService(terminal).Read(context.Background(), ReadRequest{Target: terminal.target, Source: SourceTranscript})
	if got := codeOf(t, err); got != ErrTranscriptUnavailable {
		t.Fatalf("code = %s, want %s", got, ErrTranscriptUnavailable)
	}
	if len(terminal.captures) != 0 {
		t.Fatal("an unavailable transcript fell back to scraping the terminal")
	}

	svc := stageService(terminal)
	svc.Transcript = fixedTranscript{messages: []TranscriptMessage{{Role: "user", Text: "review the diff"}, {Role: "assistant", Text: "two findings"}}}
	got, err := svc.Read(context.Background(), ReadRequest{Target: terminal.target, Source: SourceTranscript, Lines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[1].Text != "two findings" || got.Text != "" {
		t.Fatalf("transcript = %+v", got)
	}

	svc.Transcript = fixedTranscript{err: errors.New("bound session no longer exists")}
	if _, err := svc.Read(context.Background(), ReadRequest{Target: terminal.target, Source: SourceTranscript}); codeOf(t, err) != ErrTranscriptUnavailable {
		t.Fatal("a failing reader did not degrade to transcript_unavailable")
	}
}

func TestParseHelpersHoldTheFrozenVocabulary(t *testing.T) {
	for _, name := range []string{"idle", "WORKING", " blocked ", "done"} {
		if _, err := ParseStatus(name); err != nil {
			t.Fatalf("ParseStatus(%q): %v", name, err)
		}
	}
	if _, err := ParseStatus("settled"); err == nil {
		t.Fatal("ParseStatus accepted an invented status")
	}
	for _, source := range ReadSources() {
		if _, err := ParseReadSource(string(source)); err != nil {
			t.Fatalf("ParseReadSource(%q): %v", source, err)
		}
	}
	if _, err := ParseReadSource("screen"); err == nil {
		t.Fatal("ParseReadSource accepted an unknown source")
	}
}

func TestLastLinesKeepsTheTail(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd\n" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("a\nb", 0); got != "a\nb" {
		t.Fatalf("unbounded lastLines = %q", got)
	}
	if got := lastLines("a\nb", 9); got != "a\nb" {
		t.Fatalf("short lastLines = %q", got)
	}
}

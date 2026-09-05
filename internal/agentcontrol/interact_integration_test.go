package agentcontrol

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/tmuxenv"
)

// Every tmux call in this file runs against the package's private server: the
// package TestMain isolates TMUX_TMPDIR and scrubs TMUX, so a bare `tmux` here
// can never resolve to the developer's live default server. Sessions are killed
// individually on cleanup; nothing kills a server.

// fakeProviderScript is an agent-shaped process: it announces itself idle,
// treats each submitted line as a turn, and finishes that turn either blocked
// or done. It is deliberately a shell loop rather than a real provider — the
// contract under test is Sidecar's, and a paid provider cannot be part of an
// ordinary test run.
//
// The markers are assembled from $m at runtime rather than written as literals.
// A launch is typed into the shell, so the script's own source is echoed onto
// the pane before it runs — and with literal markers that echo ends in
// FAKE_DONE, which a last-marker-wins detector reads as a finished agent. The
// window between the echo and the first real marker is short, so it only opened
// under load, and it made every "the idle agent has not settled" precondition
// in this file a coin flip. Building the markers keeps the fixture's source out
// of the fixture's own evidence.
const fakeProviderScript = `m=FAKE; printf '%s_IDLE\n' "$m"; while IFS= read -r line; do printf '%s_WORKING:%s\n' "$m" "$line"; sleep 0.3; if [ "$line" = block ]; then printf '%s_BLOCKED\n' "$m"; else printf '%s_DONE\n' "$m"; fi; done`

// fakeProviderDetect reads the fake provider's markers. The most recent marker
// on screen wins, which is how a real screen-evidence detector behaves.
func fakeProviderDetect(s Snapshot, _ *agentactivity.Tracker) AgentState {
	state := AgentState{Freshness: "current", Evidence: "fake.screen", CapturedAt: s.CapturedAt}
	latest := -1
	for marker, candidate := range map[string]Status{"FAKE_IDLE": StatusIdle, "FAKE_WORKING": StatusWorking, "FAKE_DONE": StatusDone, "FAKE_BLOCKED": StatusBlocked} {
		if at := strings.LastIndex(s.Screen, marker); at > latest {
			latest, state.Status = at, candidate
		}
	}
	if latest < 0 {
		return AgentState{Status: StatusUnknown, Freshness: "current", Evidence: "provider-not-identified", CapturedAt: s.CapturedAt}
	}
	state.Kind = "fake"
	state.InteractiveReady = state.Status == StatusIdle || state.Status == StatusDone
	return state
}

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
}

// startFakeAgent brings up one isolated managed session running the fake
// provider and returns the service and its pinned target.
func startFakeAgent(t *testing.T, name string) (Service, *LocalTerminal, Target) {
	t.Helper()
	session := fmt.Sprintf("sidecar-agentcontrol-m2-%s-%d", name, time.Now().UnixNano())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", session).CombinedOutput(); err != nil {
		t.Fatalf("new isolated session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() })

	terminal := NewLocalTerminal()
	t.Cleanup(terminal.Close)
	svc := Service{Terminal: terminal, Poll: 20 * time.Millisecond, Observe: 20 * time.Millisecond, Verify: 200 * time.Millisecond, ShellStableFor: 100 * time.Millisecond, Detect: fakeProviderDetect}
	target := Target{Host: "local", Project: "fixture", Session: session, Name: name, Namespace: tmuxenv.Namespace()}
	ready, err := svc.WaitShellReady(context.Background(), target, 5*time.Second)
	if err != nil {
		t.Fatalf("%s shell never became ready: %v", name, err)
	}
	agent, err := svc.Start(context.Background(), StartRequest{Target: ready.Target, Kind: "fake", Argv: []string{"sh", "-c", fakeProviderScript}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("%s did not start: %v", name, err)
	}
	return svc, terminal, agent.Target
}

// TestTwoIsolatedAgentsPromptWaitReadAndKeysInvolveOnlyTheirOwnShell is the M2
// exit gate. Two managed agents run at once; the caller prompts each under its
// own pinned target; one finishes done and the other stops blocked; then the
// blocked one is read and answered with logical keys while the other is proved
// untouched.
func TestTwoIsolatedAgentsPromptWaitReadAndKeysInvolveOnlyTheirOwnShell(t *testing.T) {
	requireTmux(t)
	finishing, _, finishingTarget := startFakeAgent(t, "finishing")
	blocking, _, blockingTarget := startFakeAgent(t, "blocking")
	if finishingTarget.Session == blockingTarget.Session || finishingTarget.PaneID == "" || blockingTarget.PaneID == "" {
		t.Fatalf("targets are not distinct and pinned: %+v %+v", finishingTarget, blockingTarget)
	}

	type outcome struct {
		agent PromptResult
		err   error
	}
	results := make([]outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		agent, err := finishing.Prompt(context.Background(), PromptRequest{Target: finishingTarget, Text: "summarise the diff", Wait: true, Timeout: 20 * time.Second})
		results[0] = outcome{agent, err}
	}()
	go func() {
		defer wg.Done()
		agent, err := blocking.Prompt(context.Background(), PromptRequest{Target: blockingTarget, Text: "block", Wait: true, Timeout: 20 * time.Second})
		results[1] = outcome{agent, err}
	}()
	wg.Wait()

	for i, want := range []Status{StatusDone, StatusBlocked} {
		if results[i].err != nil {
			t.Fatalf("prompt %d failed: %v", i, results[i].err)
		}
		if results[i].agent.Agent.Status != want {
			t.Fatalf("prompt %d settled at %s, want %s", i, results[i].agent.Agent.Status, want)
		}
	}
	if results[0].agent.Target.Session != finishingTarget.Session || results[1].agent.Target.Session != blockingTarget.Session {
		t.Fatalf("results crossed shells: %+v %+v", results[0].agent.Target, results[1].agent.Target)
	}

	// Read before keys: the caller inspects the blocked screen and only then
	// decides what to send. The read is passive and names its own target.
	read, err := blocking.Read(context.Background(), ReadRequest{Target: blockingTarget, Source: SourceRecentUnwrapped, Lines: 60})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.Text, "FAKE_BLOCKED") || read.Status != StatusBlocked {
		t.Fatalf("read did not show the blocked agent: %+v", read)
	}
	if strings.Contains(read.Text, "summarise the diff") {
		t.Fatal("the blocked agent's read returned the other shell's work")
	}

	before, err := finishing.Read(context.Background(), ReadRequest{Target: finishingTarget, Source: SourceVisible})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := blocking.SendKeys(context.Background(), KeysRequest{Target: blockingTarget, Keys: []string{"y", "enter"}}); err != nil {
		t.Fatal(err)
	}
	answered, err := blocking.Wait(context.Background(), WaitRequest{Target: blockingTarget, Until: []Status{StatusDone}, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("the blocked agent never answered: %v", err)
	}
	if answered.Agent.Status != StatusDone || answered.Target.Session != blockingTarget.Session {
		t.Fatalf("answered = %+v", answered)
	}
	confirm, err := blocking.Read(context.Background(), ReadRequest{Target: blockingTarget, Source: SourceRecent, Lines: 60})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(confirm.Text, "FAKE_WORKING:y") {
		t.Fatalf("the keys did not reach the blocked agent:\n%s", confirm.Text)
	}

	after, err := finishing.Read(context.Background(), ReadRequest{Target: finishingTarget, Source: SourceVisible})
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text {
		t.Fatalf("input aimed at one shell changed another:\n--- before ---\n%s\n--- after ---\n%s", before.Text, after.Text)
	}
	if strings.Contains(after.Text, "FAKE_WORKING:y") {
		t.Fatal("keys leaked into the untargeted shell")
	}
}

// TestWaitCannotBeSatisfiedByARealReplacementOccupant replaces the pane's
// process under a running wait with something that prints the very marker the
// wait is looking for. The wait must refuse it.
func TestWaitCannotBeSatisfiedByARealReplacementOccupant(t *testing.T) {
	requireTmux(t)
	svc, terminal, target := startFakeAgent(t, "replaced")

	if err := terminal.Submit(context.Background(), Snapshot{Target: target}, "keep working"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.Wait(context.Background(), WaitRequest{Target: target, Until: []Status{StatusDone}, Timeout: 15 * time.Second})
		done <- err
	}()

	// Give the wait time to pin and subscribe, then hand the pane to a
	// different process that immediately shows the settled marker.
	time.Sleep(300 * time.Millisecond)
	if out, err := exec.Command("tmux", "respawn-pane", "-k", "-t", target.PaneID, "sh", "-c", "printf 'FAKE_DONE\\n'; sleep 30").CombinedOutput(); err != nil {
		t.Fatalf("respawn pane: %v: %s", err, out)
	}

	select {
	case err := <-done:
		var typed *Error
		if !AsError(err, &typed) || typed.Code != ErrReplaced {
			t.Fatalf("Wait() = %v, want %s; a replacement occupant satisfied the wait", err, ErrReplaced)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("wait never returned")
	}
}

// controlClients counts this process's live tmux control-mode children.
func controlClients(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "ppid=,command=").Output()
	if err != nil {
		t.Skipf("cannot enumerate child processes: %v", err)
	}
	parent := strconv.Itoa(os.Getpid())
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != parent {
			continue
		}
		if strings.Contains(strings.Join(fields[1:], " "), "-C attach-session") {
			count++
		}
	}
	return count
}

// TestObserverLeavesNoControlClientOrGoroutineBehind is the cleanup half of the
// M0 observer decision: an event-driven observer is only worth having if a
// cancelled, timed-out, or completed wait releases every client and goroutine
// it made.
// countingSignaler wraps the real terminal and counts the observer clients a
// wait opens and releases. The leak this test exists to catch is a Signal
// without its stop, and counting the pair says so directly rather than
// inferring it from a client census that anything else on the server also
// moves. It also proves the satisfied case reached the observer at all.
type countingSignaler struct {
	*LocalTerminal
	mu      sync.Mutex
	signals int
	stops   int
}

func (c *countingSignaler) Signal(ctx context.Context, snap Snapshot) (<-chan Signal, func(), error) {
	signals, stop, err := c.LocalTerminal.Signal(ctx, snap)
	if err != nil {
		return signals, stop, err
	}
	c.mu.Lock()
	c.signals++
	c.mu.Unlock()
	var once sync.Once
	return signals, func() {
		stop()
		once.Do(func() {
			c.mu.Lock()
			c.stops++
			c.mu.Unlock()
		})
	}, nil
}

func (c *countingSignaler) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.signals, c.stops
}

func TestObserverLeavesNoControlClientOrGoroutineBehind(t *testing.T) {
	requireTmux(t)
	svc, terminal, target := startFakeAgent(t, "leaks")
	observer := &countingSignaler{LocalTerminal: terminal}
	svc.Terminal = observer

	settle := func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if controlClients(t) == 0 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	settle()
	baselineClients := controlClients(t)
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	// A timed-out wait, a cancelled wait, and a satisfied wait each exercise a
	// different exit from the observer loop.
	//
	// The two waits below can only time out if the target is not already what
	// they are waiting for, so assert that directly instead of reading it out
	// of their failure: "the wait failed" is also what a broken fixture looks
	// like, and this precondition used to be a coin flip under load.
	if agent, err := svc.Get(context.Background(), target); err != nil || agent.Agent.Status != StatusIdle {
		t.Fatalf("precondition: target reads %+v (err %v), want a settled idle agent", agent.Agent, err)
	}
	if _, err := svc.Wait(context.Background(), WaitRequest{Target: target, Until: []Status{StatusDone}, Timeout: 400 * time.Millisecond}); err == nil {
		t.Fatal("precondition: the idle agent should not have settled as done")
	} else if got := codeOf(t, err); got != ErrTimeout {
		t.Fatalf("precondition: wanted the wait to time out, got %s: %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	if _, err := svc.Wait(ctx, WaitRequest{Target: target, Until: []Status{StatusDone}, Timeout: 15 * time.Second}); err == nil {
		t.Fatal("precondition: the cancelled wait should have failed")
	}

	// The satisfied wait has to build an observer and then release it. Waiting
	// for the state the agent is already in returns from watch's first accept,
	// before the Signaler is ever constructed, which proves nothing about
	// cleanup — so drive a real turn and wait for its completion.
	if _, err := svc.Prompt(context.Background(), PromptRequest{Target: target, Text: "finish"}); err != nil {
		t.Fatalf("starting a turn for the satisfied wait: %v", err)
	}
	if _, err := svc.Wait(context.Background(), WaitRequest{Target: target, Until: []Status{StatusDone}, Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("satisfied wait: %v", err)
	}

	// Every observer that was opened was released. This is the leak itself,
	// stated as the invariant rather than as a side effect.
	if signals, stops := observer.counts(); signals == 0 || signals != stops {
		t.Fatalf("observer clients: %d opened, %d released", signals, stops)
	}

	settle()
	if got := controlClients(t); got > baselineClients {
		t.Fatalf("control clients grew from %d to %d across three waits", baselineClients, got)
	}
	terminal.Close()
	settle()
	if got := controlClients(t); got != 0 {
		t.Fatalf("%d control clients survived Close", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baselineGoroutines+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutines grew from %d to %d across three waits", baselineGoroutines, runtime.NumGoroutine())
}

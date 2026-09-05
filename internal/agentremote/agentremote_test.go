package agentremote

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/hosts"
)

// recorder is the transport stub: it records argv and replays one canned
// answer, so the wire contract can be asserted with no ssh and no host.
type recorder struct {
	calls   [][]string
	hostIDs []string
	result  any
	err     error
}

func (r *recorder) runner() Runner {
	return func(_ context.Context, hostID string, args []string, out any) error {
		r.calls = append(r.calls, append([]string(nil), args...))
		r.hostIDs = append(r.hostIDs, hostID)
		if r.err != nil {
			return r.err
		}
		if out == nil || r.result == nil {
			return nil
		}
		encoded, err := json.Marshal(r.result)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, out)
	}
}

func client(r *recorder) Client {
	return Client{HostID: "mac-mini", Run: r.runner(), Project: "sidecar"}
}

func TestEveryVerbCrossesAsAJSONCLIInvocation(t *testing.T) {
	// The plan's whole remote design rests on each verb being an ordinary
	// target-taking `--json` CLI invocation. If one of these ever stopped
	// carrying --json the host would answer in human text and the decode would
	// fail with a confusing "not the expected result".
	c := Client{HostID: "h", Project: "sidecar"}
	cases := map[string][]string{
		"list":      c.ListArgs(),
		"get":       c.GetArgs("s", false),
		"start":     c.StartArgs("s", "codex", time.Minute, nil),
		"prompt":    c.PromptArgs("s", "hello", false, nil, 0),
		"wait":      c.WaitArgs("s", nil, time.Minute),
		"read":      c.ReadArgs("s", agentcontrol.SourceVisible, 0, false),
		"send-keys": c.SendKeysArgs("s", []string{"enter"}),
		"status":    c.SessionStatusArgs(),
		"restore":   c.SessionRestoreArgs(false, false, false, ""),
	}
	for name, args := range cases {
		if len(args) == 0 || args[0] != "agent" && args[0] != "session" {
			t.Fatalf("%s: argv does not begin with a command group: %v", name, args)
		}
		if !contains(args, "--json") {
			t.Fatalf("%s: argv carries no --json: %v", name, args)
		}
	}
}

func TestTheTargetIsTheLastArgumentSoAFlagCannotSwallowIt(t *testing.T) {
	c := Client{HostID: "h"}
	// agent start puts provider arguments after --, and every other verb takes
	// its target positionally. A target placed next to a flag that takes a
	// value would silently address the wrong shell.
	args := c.WaitArgs("reviewer", []agentcontrol.Status{agentcontrol.StatusDone}, time.Minute)
	if args[len(args)-1] != "reviewer" {
		t.Fatalf("target is not last: %v", args)
	}
	start := c.StartArgs("reviewer", "codex", 0, []string{"-m", "gpt-5.4"})
	want := []string{"agent", "start", "--json", "--kind", "codex", "reviewer", "--", "-m", "gpt-5.4"}
	if !reflect.DeepEqual(start, want) {
		t.Fatalf("start argv = %v, want %v", start, want)
	}
}

func TestAConversationValueIsNotRequestedUnlessTheCallerAsked(t *testing.T) {
	// The host redacts by default, so the only way a remote conversation
	// identifier reaches this machine is this flag. A test on the argv is the
	// only test that can prove it is absent, because a host that chose to send
	// it anyway would look identical from the result side.
	c := Client{HostID: "h"}
	if contains(c.GetArgs("s", false), "--include-session-ref") {
		t.Fatal("a default get asked the host for the conversation value")
	}
	if contains(c.ListArgs(), "--include-session-ref") {
		t.Fatal("a list asked the host for conversation values")
	}
	if !contains(c.GetArgs("s", true), "--include-session-ref") {
		t.Fatal("an explicit get did not ask for the value")
	}
}

func TestResultsAreStampedWithTheHostThatAnsweredThem(t *testing.T) {
	// The host describes itself as "local". Two machines running a project of
	// the same name generate the same tmux session names, and Target.Host is
	// what agentcontrol.sameOccupant compares, so leaving the host's own answer
	// in place would let two different panes compare equal.
	r := &recorder{result: agentcontrol.Agent{
		Target: agentcontrol.Target{Host: "local", Session: "sidecar-sh-sidecar-1"},
		Agent:  agentcontrol.AgentState{Kind: "codex", Status: agentcontrol.StatusIdle},
	}}
	got, err := client(r).Get(context.Background(), "sidecar-sh-sidecar-1", false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Target.Host != "mac-mini" {
		t.Fatalf("target host = %q, want the host that answered", got.Target.Host)
	}
}

func TestPromptReceiptCrossesTheRemoteVerbAndIsHostStamped(t *testing.T) {
	target := agentcontrol.Target{Host: "local", Project: "sidecar", Session: "reviewer", PaneID: "%7", PanePID: 42, ServerPID: 9}
	r := &recorder{result: agentcontrol.PromptResult{
		Target: target,
		Agent:  agentcontrol.AgentState{Kind: "codex", Status: agentcontrol.StatusDone},
		Receipt: agentcontrol.PromptReceipt{
			Submission: agentcontrol.SubmissionSubmitted,
			Wait:       agentcontrol.PromptWaitSettled,
			Target:     target,
		},
	}}
	got, err := client(r).Prompt(context.Background(), "reviewer", "go", true, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got.Target.Host != "mac-mini" || got.Receipt.Target.Host != "mac-mini" || got.Receipt.Submission != agentcontrol.SubmissionSubmitted || got.Receipt.Wait != agentcontrol.PromptWaitSettled {
		t.Fatalf("prompt result = %+v", got)
	}
}

func TestPromptTransportFailureKeepsSubmissionUnknown(t *testing.T) {
	r := &recorder{err: &hosts.RunError{Failure: hosts.FailTimeout, HostID: "mac-mini", Detail: "connection closed"}}
	_, err := client(r).Prompt(context.Background(), "reviewer", "go", true, nil, time.Minute)
	var typed *agentcontrol.Error
	if !errors.As(err, &typed) || typed.Receipt == nil || typed.Receipt.Submission != agentcontrol.SubmissionUnknown || typed.Receipt.Wait != agentcontrol.PromptWaitFailed || typed.Receipt.Target.Host != "mac-mini" || typed.Receipt.Target.Session != "reviewer" {
		t.Fatalf("prompt transport error = %+v", typed)
	}
}

func TestPromptHostTimeoutReceiptIsRelayedAndStamped(t *testing.T) {
	target := agentcontrol.Target{Host: "local", Project: "sidecar", Session: "reviewer", PaneID: "%7", PanePID: 42, ServerPID: 9}
	envelope := agentcontrol.ErrorEnvelope{Error: &agentcontrol.Error{
		Code:    agentcontrol.ErrTimeout,
		Message: "timed out waiting for the agent to settle",
		Target:  &target,
		Receipt: &agentcontrol.PromptReceipt{Submission: agentcontrol.SubmissionSubmitted, Wait: agentcontrol.PromptWaitTimeout, Target: target},
	}}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{err: &hosts.RunError{Failure: hosts.FailTimeout, HostID: "mac-mini", ExitCode: 1, Stderr: string(encoded)}}
	_, err = client(r).Prompt(context.Background(), "reviewer", "go", true, nil, time.Minute)
	var typed *agentcontrol.Error
	if !errors.As(err, &typed) || typed.Code != agentcontrol.ErrTimeout || typed.Receipt == nil || typed.Receipt.Submission != agentcontrol.SubmissionSubmitted || typed.Receipt.Wait != agentcontrol.PromptWaitTimeout || typed.Receipt.Target.Host != "mac-mini" || typed.Target == nil || typed.Target.Host != "mac-mini" {
		t.Fatalf("host timeout = %+v", typed)
	}
}

func TestAnEmptyRemoteAgentListIsAnAnswerRatherThanADecodeFailure(t *testing.T) {
	// hosts.decodeRemoteResult rejects a zero-valued decode because a login
	// banner that happens to be JSON is the usual cause. "No live managed
	// agents" is a legitimate zero-valued answer and must survive that rule.
	if !(listResult{}).ValidRemoteResult() {
		t.Fatal("an empty agent list would be rejected as a non-result")
	}
}

// --- error translation -----------------------------------------------------

func TestTheHostsOwnRefusalIsReturnedVerbatim(t *testing.T) {
	// This is the property the whole parity claim rests on: a remote refusal is
	// the host's refusal, with its code, its sentence and its target, not a
	// local approximation of it. `sidecar agent` writes the frozen envelope to
	// stderr and hosts.RunError carries stderr, so it can simply be read back.
	envelope := agentcontrol.ErrorEnvelope{Error: &agentcontrol.Error{
		Code:    agentcontrol.ErrBlocked,
		Message: "the agent is blocked and cannot be prompted",
		Target:  &agentcontrol.Target{Host: "local", Session: "sidecar-sh-sidecar-4"},
	}}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	runErr := &hosts.RunError{Failure: hosts.FailRefused, HostID: "mac-mini", ExitCode: 5, Stderr: string(encoded)}

	translated := TranslateError("mac-mini", runErr)
	var typed *agentcontrol.Error
	if !errors.As(translated, &typed) {
		t.Fatalf("translated error is not an agent error: %T", translated)
	}
	if typed.Code != agentcontrol.ErrBlocked {
		t.Fatalf("code = %q, want the host's own agent_blocked", typed.Code)
	}
	if typed.Message != "the agent is blocked and cannot be prompted" {
		t.Fatalf("message = %q, want the host's sentence", typed.Message)
	}
	if typed.Target == nil || typed.Target.Session != "sidecar-sh-sidecar-4" {
		t.Fatalf("target was lost: %+v", typed.Target)
	}
}

func TestAHostEnvelopeIsFoundBehindBannerNoiseOnStderr(t *testing.T) {
	// A login profile writing to stderr is at least as common as one writing to
	// stdout, and the CLI's envelope is written last.
	envelope := `{"error":{"code":"agent_pane_busy","message":"the pane is busy"}}`
	stderr := "Welcome to mac-mini\n{\"notjson\":\n" + envelope + "\n"
	runErr := &hosts.RunError{Failure: hosts.FailRefused, HostID: "mac-mini", ExitCode: 5, Stderr: stderr}
	var typed *agentcontrol.Error
	if !errors.As(TranslateError("mac-mini", runErr), &typed) {
		t.Fatal("no agent error was recovered")
	}
	if typed.Code != agentcontrol.ErrPaneBusy {
		t.Fatalf("code = %q, want agent_pane_busy from behind the banner", typed.Code)
	}
}

func TestAnUnknownRemoteCodeIsPassedThroughRatherThanApproximated(t *testing.T) {
	// A host running a newer Sidecar can refuse with a code this build has
	// never heard of. Replacing it with a local guess would throw away the only
	// accurate description of what happened.
	stderr := `{"error":{"code":"agent_something_new","message":"a refusal from the future"}}`
	runErr := &hosts.RunError{Failure: hosts.FailRejected, HostID: "h", ExitCode: 5, Stderr: stderr}
	var typed *agentcontrol.Error
	if !errors.As(TranslateError("h", runErr), &typed) {
		t.Fatal("no agent error was recovered")
	}
	if typed.Code != "agent_something_new" {
		t.Fatalf("code = %q, want the host's own unknown code", typed.Code)
	}
}

func TestTransportFailuresMapOntoTheAgentVocabulary(t *testing.T) {
	// With no envelope to read, the transport's classification decides. The
	// distinctions that matter are the two M5 added: a machine that could not
	// be reached is not a failed operation, and a host that does not know the
	// verb is a version skew whose fix is to update a binary.
	cases := []struct {
		name    string
		failure hosts.Failure
		exit    int
		want    agentcontrol.ErrorCode
	}{
		{"unreachable host", hosts.FailUnavailable, -1, agentcontrol.ErrHostUnavailable},
		{"no sidecar on the host", hosts.FailNoSidecar, 127, agentcontrol.ErrHostUnavailable},
		{"ssh failed", hosts.FailTransport, 255, agentcontrol.ErrHostUnavailable},
		{"host does not know the verb", hosts.FailUnsupported, 2, agentcontrol.ErrVersionSkew},
		{"host did not answer", hosts.FailTimeout, -1, agentcontrol.ErrTimeout},
		{"host does not own the target", hosts.FailNoTarget, 3, agentcontrol.ErrNotFound},
		{"host rejected a value", hosts.FailRejected, 5, agentcontrol.ErrTransport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runErr := &hosts.RunError{Failure: tc.failure, HostID: "mac-mini", ExitCode: tc.exit, Detail: "detail"}
			var typed *agentcontrol.Error
			if !errors.As(TranslateError("mac-mini", runErr), &typed) {
				t.Fatalf("not an agent error")
			}
			if typed.Code != tc.want {
				t.Fatalf("code = %q, want %q", typed.Code, tc.want)
			}
			if !strings.Contains(typed.Message, "mac-mini") {
				t.Fatalf("message does not name the host: %q", typed.Message)
			}
		})
	}
}

func TestAWaitsDeadlineIsTheCallersRatherThanTheTransportDefault(t *testing.T) {
	// hosts.RunSidecar replaces an absent deadline with its own 30s default.
	// Without the slack a `wait --timeout 2m` would be severed by the transport
	// at 30 seconds and reported as a transport timeout for a wait that was
	// proceeding correctly.
	var seen time.Duration
	c := Client{HostID: "h", Run: func(ctx context.Context, _ string, _ []string, _ any) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("a wait was dispatched with no deadline at all")
		}
		seen = time.Until(deadline)
		return nil
	}}
	if _, err := c.Wait(context.Background(), "reviewer", nil, 2*time.Minute); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if seen <= 2*time.Minute {
		t.Fatalf("invocation deadline %v does not outlast the 2m wait it carries", seen)
	}
	if seen > 2*time.Minute+WaitSlack+time.Second {
		t.Fatalf("invocation deadline %v is more slack than intended", seen)
	}
}

func TestAClientWithNoTransportRefusesRatherThanPanicking(t *testing.T) {
	if _, err := (Client{HostID: "h"}).Get(context.Background(), "s", false); err == nil {
		t.Fatal("a client with no runner returned success")
	}
}

func TestNothingHereCanWriteALocalRecord(t *testing.T) {
	// Session references stay on the host that owns the provider store. This is
	// structural rather than a rule somebody has to remember: the package has no
	// store, no manifest and no shellstate import, so there is nowhere for a
	// remote conversation identifier to land locally.
	banned := []string{
		"github.com/marcus/sidecar/internal/shellstate",
		"github.com/marcus/sidecar/internal/agentsession",
		"github.com/marcus/sidecar/internal/sessionrestore",
		"os",
	}
	for _, path := range packageImports(t) {
		for _, deny := range banned {
			if path == deny {
				t.Fatalf("agentremote imports %q; a remote session value would have somewhere local to land", path)
			}
		}
	}
}

func contains(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

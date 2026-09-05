// Package agentremote runs Sidecar's agent and session verbs on a registered
// remote host, as one-shot `sidecar <verb> --json` invocations over the ssh
// ControlMaster the remote-hosts plan already ships (hosts.RunSidecar).
//
// The adapter is at the *verb* level, not at agentcontrol.Terminal level, and
// that is the whole design. A Terminal-level remote adapter would tunnel
// Inspect/Capture/Submit across the link and re-run the occupant pin, the
// readiness contract and the refusal rules on the viewer — over a connection
// that can stall between the pin and the write. Running the verb on the host
// instead puts every rule on the machine that owns the pane, and leaves exactly
// one implementation of them. The viewer composes argv, waits, and decodes.
//
// The consequence worth stating: a remote refusal is the host's own refusal.
// `sidecar agent` writes the frozen agentcontrol error envelope to stderr, so
// this package decodes that envelope and returns the *agentcontrol.Error the
// host produced, with its code, sentence and target intact. Parity with the
// local path is therefore a property of the transport rather than of a
// translation table somebody has to keep in step.
//
// Session references stay on the host that owns the provider store. Nothing
// here writes anything: there is no store, no manifest and no shellstate
// import, so a remote conversation identifier cannot reach the viewer's
// shells.json even by mistake. The value crosses only when the caller asks for
// it explicitly, because the host redacts it by default and this package adds
// --include-session-ref only when told to.
package agentremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/hosts"
)

// Runner is the one-shot remote CLI seam. Production binds
// hosts.Registry.RunSidecar; tests bind a recorder.
//
// It is a function rather than an interface so a test can be three lines, and
// so this package needs no knowledge of how a host was registered or dialled.
type Runner func(ctx context.Context, hostID string, args []string, out any) error

// Client addresses one registered host.
type Client struct {
	HostID string
	Run    Runner
	// Project scopes every verb to one project on the host. It is the host's
	// own project reference, not a scoped key: the host resolves it against
	// its own registry, and a viewer-side key would be meaningless there.
	Project string
}

// WaitSlack is added to a caller's requested agent timeout to produce the
// deadline of the invocation carrying it.
//
// A wait is the one verb whose useful duration is chosen by the caller rather
// than bounded by the work, and hosts.RunSidecar replaces an absent deadline
// with its own 30s default. Without this, `agent wait --timeout 2m` would be
// killed by the transport at 30 seconds and report a transport timeout for a
// wait that was proceeding correctly. The slack covers the ssh round trip and
// the host's own process start, so the *host's* timeout is what expires first
// and the caller gets the agent-level `timeout` refusal it asked for rather
// than a severed connection.
const WaitSlack = 15 * time.Second

// ErrNoRunner is returned when a Client was built without a transport. It is a
// programming error rather than a host condition, so it is not a RunError.
var ErrNoRunner = errors.New("agentremote: no runner is configured")

func (c Client) run(ctx context.Context, args []string, out any) error {
	if c.Run == nil {
		return &agentcontrol.Error{Code: agentcontrol.ErrTransport, Message: ErrNoRunner.Error(), Err: ErrNoRunner}
	}
	if err := c.Run(ctx, c.HostID, args, out); err != nil {
		return TranslateError(c.HostID, err)
	}
	return nil
}

// scoped appends --project when the client is scoped to one.
func (c Client) scoped(args []string) []string {
	if strings.TrimSpace(c.Project) == "" {
		return args
	}
	return append(args, "--project", c.Project)
}

// ---------------------------------------------------------------------------
// Argument builders.
//
// These are pure and exported so the wire contract can be asserted without a
// transport, which is how the remote-hosts plan's own mutation verbs are
// tested (internal/overview/remote_actions.go). A test that reads the argv is
// the only test that can prove --include-session-ref is absent by default.
// ---------------------------------------------------------------------------

// ListArgs builds `sidecar agent list`.
func (c Client) ListArgs() []string {
	return c.scoped([]string{"agent", "list", "--json"})
}

// GetArgs builds `sidecar agent get`.
func (c Client) GetArgs(session string, includeSessionRef bool) []string {
	args := c.scoped([]string{"agent", "get", "--json"})
	if includeSessionRef {
		args = append(args, "--include-session-ref")
	}
	return appendTarget(args, session)
}

// StartArgs builds `sidecar agent start`. Provider arguments stay separate
// argv entries after `--`, exactly as the local path keeps them.
func (c Client) StartArgs(session, kind string, timeout time.Duration, providerArgs []string) []string {
	args := c.scoped([]string{"agent", "start", "--json", "--kind", kind})
	if timeout > 0 {
		args = append(args, "--timeout", timeout.String())
	}
	args = appendTarget(args, session)
	if len(providerArgs) > 0 {
		args = append(args, "--")
		args = append(args, providerArgs...)
	}
	return args
}

// PromptArgs builds `sidecar agent prompt`.
func (c Client) PromptArgs(session, text string, wait bool, until []agentcontrol.Status, timeout time.Duration) []string {
	args := c.scoped([]string{"agent", "prompt", "--json"})
	if wait {
		args = append(args, "--wait")
		if timeout > 0 {
			args = append(args, "--timeout", timeout.String())
		}
		args = appendUntil(args, until)
	}
	args = appendTarget(args, session)
	return append(args, text)
}

// WaitArgs builds `sidecar agent wait`.
func (c Client) WaitArgs(session string, until []agentcontrol.Status, timeout time.Duration) []string {
	args := c.scoped([]string{"agent", "wait", "--json"})
	if timeout > 0 {
		args = append(args, "--timeout", timeout.String())
	}
	args = appendUntil(args, until)
	return appendTarget(args, session)
}

// ReadArgs builds `sidecar agent read`.
func (c Client) ReadArgs(session string, source agentcontrol.ReadSource, lines int, ansi bool) []string {
	args := c.scoped([]string{"agent", "read", "--json"})
	if source != "" {
		args = append(args, "--source", string(source))
	}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	if ansi {
		args = append(args, "--ansi")
	}
	return appendTarget(args, session)
}

// SendKeysArgs builds `sidecar agent send-keys`.
func (c Client) SendKeysArgs(session string, keys []string) []string {
	args := c.scoped([]string{"agent", "send-keys", "--json"})
	args = appendTarget(args, session)
	return append(args, keys...)
}

// SessionStatusArgs builds `sidecar session status`.
func (c Client) SessionStatusArgs() []string { return []string{"session", "status", "--json"} }

// SessionRestoreArgs builds `sidecar session restore`.
//
// The viewer requests and observes; the host executes. --yes is forwarded only
// when the caller supplied it, so the host's own `ask` policy still refuses an
// unconfirmed remote resume rather than the viewer deciding on its behalf.
func (c Client) SessionRestoreArgs(dryRun, agents, yes bool, shell string) []string {
	args := []string{"session", "restore", "--json"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	if agents {
		args = append(args, "--agents")
	}
	if yes {
		args = append(args, "--yes")
	}
	if strings.TrimSpace(shell) != "" {
		args = append(args, "--shell", shell)
	}
	return args
}

// appendTarget puts the target last, after every flag.
//
// `agent start` takes provider arguments after `--`, and a target parsed as a
// flag value would silently address the wrong shell, so target placement is
// one rule here rather than a habit at six call sites.
func appendTarget(args []string, session string) []string {
	if strings.TrimSpace(session) == "" {
		return args
	}
	return append(args, session)
}

func appendUntil(args []string, until []agentcontrol.Status) []string {
	for _, status := range until {
		args = append(args, "--until", string(status))
	}
	return args
}

// ---------------------------------------------------------------------------
// Verbs.
// ---------------------------------------------------------------------------

type listResult struct {
	Agents []agentcontrol.Agent `json:"agents"`
}

// ValidRemoteResult lets an empty agent list decode as a real answer.
//
// hosts.decodeRemoteResult rejects a candidate that decodes to the zero value,
// because a login banner that happens to be JSON is the usual cause. "This host
// has no live managed agents" is a legitimate zero-valued answer, and without
// this it would be reported as a host that wrote something other than a result.
func (listResult) ValidRemoteResult() bool { return true }

// List reports the host's live managed agents.
func (c Client) List(ctx context.Context) ([]agentcontrol.Agent, error) {
	var result listResult
	if err := c.run(ctx, c.ListArgs(), &result); err != nil {
		return nil, err
	}
	return c.stamp(result.Agents), nil
}

// Get reports one agent.
func (c Client) Get(ctx context.Context, session string, includeSessionRef bool) (agentcontrol.Agent, error) {
	return c.one(ctx, c.GetArgs(session, includeSessionRef))
}

// Start launches a provider in an existing remote managed shell. It returns
// only when the host's own readiness contract is satisfied.
func (c Client) Start(ctx context.Context, session, kind string, timeout time.Duration, providerArgs []string) (agentcontrol.Agent, error) {
	ctx, cancel := c.deadline(ctx, timeout)
	defer cancel()
	return c.one(ctx, c.StartArgs(session, kind, timeout, providerArgs))
}

// Prompt submits prompt text, optionally waiting for the host to settle.
func (c Client) Prompt(ctx context.Context, session, text string, wait bool, until []agentcontrol.Status, timeout time.Duration) (agentcontrol.PromptResult, error) {
	if wait {
		var cancel context.CancelFunc
		ctx, cancel = c.deadline(ctx, timeout)
		defer cancel()
	}
	var result agentcontrol.PromptResult
	if err := c.run(ctx, c.PromptArgs(session, text, wait, until, timeout), &result); err != nil {
		// A newer host returns its exact receipt in the error envelope. With an
		// older host or a severed transport, Sidecar cannot know whether the
		// remote write landed, so say unknown rather than manufacturing retry
		// safety.
		var typed *agentcontrol.Error
		if agentcontrol.AsError(err, &typed) && typed.Receipt == nil {
			copy := *typed
			outcome := agentcontrol.PromptWaitNotStarted
			if wait {
				outcome = agentcontrol.PromptWaitFailed
			}
			copy.Receipt = &agentcontrol.PromptReceipt{
				Submission: agentcontrol.SubmissionUnknown,
				Wait:       outcome,
				Target:     agentcontrol.Target{Host: c.HostID, Project: c.Project, Session: session},
			}
			return agentcontrol.PromptResult{}, &copy
		}
		return agentcontrol.PromptResult{}, err
	}
	result.Target.Host = c.HostID
	result.Receipt.Target.Host = c.HostID
	// A successful old-host Agent response decodes into the compatible leading
	// fields but has no receipt. Success still proves the prompt call completed,
	// so fill the additive fields locally.
	if result.Receipt.Submission == "" {
		result.Receipt.Submission = agentcontrol.SubmissionSubmitted
		if wait {
			result.Receipt.Wait = agentcontrol.PromptWaitSettled
		} else {
			result.Receipt.Wait = agentcontrol.PromptWaitNotRequested
		}
		result.Receipt.Target = result.Target
	}
	return result, nil
}

// Wait blocks until the host reports a settled state or its own timeout
// expires.
func (c Client) Wait(ctx context.Context, session string, until []agentcontrol.Status, timeout time.Duration) (agentcontrol.Agent, error) {
	ctx, cancel := c.deadline(ctx, timeout)
	defer cancel()
	return c.one(ctx, c.WaitArgs(session, until, timeout))
}

// SendKeys writes validated logical keys to the remote agent's UI.
func (c Client) SendKeys(ctx context.Context, session string, keys []string) (agentcontrol.Agent, error) {
	return c.one(ctx, c.SendKeysArgs(session, keys))
}

// Read passively captures the remote pane or its bound transcript.
//
// Its result carries a Target too, so it is stamped for the same reason every
// other verb's is: a caller holding reads from two machines has nothing else to
// tell them apart by, and the host describes itself as "local". This was missed
// on the first pass — Read was the one verb that returned the host's own view
// unstamped — and the parity suite caught it.
func (c Client) Read(ctx context.Context, session string, source agentcontrol.ReadSource, lines int, ansi bool) (agentcontrol.ReadResult, error) {
	var result agentcontrol.ReadResult
	if err := c.run(ctx, c.ReadArgs(session, source, lines, ansi), &result); err != nil {
		return agentcontrol.ReadResult{}, err
	}
	result.Target.Host = c.HostID
	return result, nil
}

// SessionDocument is a host's restore plan or restore result, relayed whole.
//
// It is a map rather than a struct because the viewer has no decision to make
// about the contents — it is relaying, not interpreting, and re-modelling the
// host's schema here would create a second copy of it that drifts. It is a
// named type rather than a bare map so it can carry ValidRemoteResult.
type SessionDocument map[string]any

// ValidRemoteResult tells the transport's decoder which JSON object on stdout
// is the document.
//
// This is not defensive: without it the relay returns the wrong object, and the
// loopback suite caught it doing so. `session status --json` is *indented*, and
// hosts.decodeRemoteResult scans stdout for lines that begin an object and
// tries them last-first — correct for a result printed after banner noise, and
// exactly wrong for a pretty-printed document whose every nested object starts
// its own line. The last such line is the final element of "steps", which
// decodes perfectly well, so the viewer received one step of the plan wearing
// the whole document's place.
//
// resumePolicy is the discriminator because both document shapes carry it
// without omitempty (planDocument and resultDocument in internal/cli/session.go)
// and no individual step or outcome has the field, so a fragment can never
// satisfy it.
func (d SessionDocument) ValidRemoteResult() bool {
	if len(d) == 0 {
		return false
	}
	_, ok := d["resumePolicy"]
	return ok
}

// SessionStatus reads the host's ordered restore plan. It is read-only and
// runs on the host: a viewer never reconstructs another machine's state
// locally, it asks the machine that owns it.
func (c Client) SessionStatus(ctx context.Context) (SessionDocument, error) {
	return c.document(ctx, c.SessionStatusArgs())
}

// SessionRestore performs, or previews, a restore on the host.
func (c Client) SessionRestore(ctx context.Context, dryRun, agents, yes bool, shell string) (SessionDocument, error) {
	return c.document(ctx, c.SessionRestoreArgs(dryRun, agents, yes, shell))
}

func (c Client) document(ctx context.Context, args []string) (SessionDocument, error) {
	var doc SessionDocument
	if err := c.run(ctx, args, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (c Client) one(ctx context.Context, args []string) (agentcontrol.Agent, error) {
	var agent agentcontrol.Agent
	if err := c.run(ctx, args, &agent); err != nil {
		return agentcontrol.Agent{}, err
	}
	return c.stampOne(agent), nil
}

// deadline gives the invocation the caller's own timeout plus slack. See
// WaitSlack for why a bounded wait cannot inherit the transport default.
func (c Client) deadline(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout+WaitSlack {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout+WaitSlack)
}

// stamp writes this client's host id onto every returned target.
//
// The host answers with its own view, in which it is "local". A viewer holding
// results from two machines needs them distinguishable, and Target.Host is the
// field agentcontrol.sameOccupant already compares — so leaving the host's
// "local" in place would let two machines' identically named tmux sessions
// compare equal. M1 built the identity host-shaped for exactly this moment.
func (c Client) stamp(agents []agentcontrol.Agent) []agentcontrol.Agent {
	out := make([]agentcontrol.Agent, 0, len(agents))
	for _, agent := range agents {
		out = append(out, c.stampOne(agent))
	}
	return out
}

func (c Client) stampOne(agent agentcontrol.Agent) agentcontrol.Agent {
	agent.Target.Host = c.HostID
	return agent
}

// ---------------------------------------------------------------------------
// Error translation.
// ---------------------------------------------------------------------------

// TranslateError turns a transport failure into the agent error vocabulary.
//
// The first thing it tries is the host's own answer. `sidecar agent` writes an
// agentcontrol.ErrorEnvelope to stderr for every refusal, and hosts.RunError
// carries that stderr, so a remote `agent_blocked` can be returned as
// `agent_blocked` — same code, same sentence, same exit status as if the
// caller had run it locally. Only when there is no envelope to read does the
// transport's own classification decide, and then it says so in the host's
// vocabulary rather than pretending an unreachable machine is a busy pane.
func TranslateError(hostID string, err error) error {
	if err == nil {
		return nil
	}
	var runErr *hosts.RunError
	if !errors.As(err, &runErr) {
		var agentErr *agentcontrol.Error
		if errors.As(err, &agentErr) {
			return agentErr
		}
		return &agentcontrol.Error{Code: agentcontrol.ErrTransport, Message: err.Error(), Err: err}
	}
	if hosted := hostedError(runErr); hosted != nil {
		if hosted.Target != nil {
			hosted.Target.Host = hostID
		}
		if hosted.Receipt != nil {
			hosted.Receipt.Target.Host = hostID
		}
		hosted.Err = err
		return hosted
	}
	code, message := classify(hostID, runErr)
	return &agentcontrol.Error{Code: code, Message: message, Err: err}
}

// hostedError recovers the host's own error envelope from its stderr.
func hostedError(runErr *hosts.RunError) *agentcontrol.Error {
	for _, line := range envelopeCandidates(runErr.Stderr) {
		var envelope agentcontrol.ErrorEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		if envelope.Error == nil || envelope.Error.Code == "" {
			continue
		}
		// A host running an older Sidecar can emit a code this build has never
		// heard of. Passing it through unchanged is deliberate: inventing a
		// local approximation would lose the only accurate description of what
		// the host refused, and the message beside it is already the sentence a
		// user needs.
		return envelope.Error
	}
	return nil
}

// envelopeCandidates returns stderr lines that could be a JSON object, newest
// last. A login profile writing to stderr is at least as common as one writing
// to stdout, which is why this scans lines rather than parsing the whole text.
func envelopeCandidates(stderr string) []string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			out = append(out, trimmed)
		}
	}
	// Last first: the CLI writes its envelope last, after any banner noise.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// classify maps a transport failure with no host envelope onto the agent
// vocabulary, following the Phase C exit-code discipline.
func classify(hostID string, runErr *hosts.RunError) (agentcontrol.ErrorCode, string) {
	detail := strings.TrimSpace(runErr.Detail)
	if detail == "" {
		detail = runErr.Error()
	}
	with := func(text string) string {
		if fix := runErr.Fix(); fix != "" {
			return text + " — " + fix
		}
		return text
	}
	switch runErr.Failure {
	case hosts.FailTimeout:
		return agentcontrol.ErrTimeout, with(fmt.Sprintf("%s did not answer in time: %s", hostID, detail))
	case hosts.FailUnsupported:
		// Remote exit 2. An older host answers a verb it lacks with a usage
		// error, so capability negotiation falls out of the exit-code contract
		// rather than needing a handshake.
		return agentcontrol.ErrVersionSkew, with(fmt.Sprintf("%s did not accept this command: %s", hostID, detail))
	case hosts.FailUnavailable, hosts.FailNoSidecar, hosts.FailTransport, hosts.FailCanceled:
		return agentcontrol.ErrHostUnavailable, with(fmt.Sprintf("%s is not reachable: %s", hostID, detail))
	case hosts.FailNoTarget:
		return agentcontrol.ErrNotFound, with(fmt.Sprintf("%s does not own that target: %s", hostID, detail))
	default:
		// FailRejected (5), FailRefused (1), FailExit, FailNotResult. The host
		// ran and declined, but wrote no envelope this build could read.
		return agentcontrol.ErrTransport, with(fmt.Sprintf("%s refused: %s", hostID, detail))
	}
}

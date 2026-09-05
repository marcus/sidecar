package agentcontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
)

// PromptStallWindow is how long a prompt sent from a non-working state has to
// produce an observed lifecycle change before the caller is told the prompt
// stalled.
//
// It is not a timeout on the agent's work — those are always the caller's
// explicit --timeout — but an honesty rule about the submission itself: an
// agent composer that swallowed the Enter, or a UI that is wedged, otherwise
// looks exactly like an agent that is thinking. Five seconds is long enough
// that no real provider misses it and short enough that a caller learns the
// prompt went nowhere before it starts waiting minutes for an answer.
const PromptStallWindow = 5 * time.Second

// DefaultSettledStates is what a wait accepts when the caller names nothing.
// working is deliberately absent: a wait that "succeeds" the moment an agent
// starts working is a wait nobody wanted.
func DefaultSettledStates() []Status { return []Status{StatusIdle, StatusDone, StatusBlocked} }

// PromptableStates are the states a prompt may be sent from.
func PromptableStates() []Status { return []Status{StatusIdle, StatusDone, StatusWorking} }

// ParseStatus accepts a status name from a caller.
func ParseStatus(value string) (Status, error) {
	switch status := Status(strings.ToLower(strings.TrimSpace(value))); status {
	case StatusIdle, StatusWorking, StatusBlocked, StatusDone:
		return status, nil
	default:
		return "", fmt.Errorf("unknown status %q; use idle, working, blocked, or done", value)
	}
}

type PromptRequest struct {
	Target Target
	Text   string
	// Wait combines submission and settling under one pinned target so no
	// second command can race a replacement occupant into the gap.
	Wait bool
	// Until narrows or widens the accepted settled states. Empty means
	// DefaultSettledStates.
	Until []Status
	// Timeout is required when Wait is set. There is no implicit timeout.
	Timeout time.Duration
}

type WaitRequest struct {
	Target  Target
	Until   []Status
	Timeout time.Duration
}

type KeysRequest struct {
	Target Target
	Keys   []string
}

// Prompt submits text to a pinned managed agent, refusing before it writes any
// bytes if the target is not in a state that may receive input.
func (s Service) Prompt(ctx context.Context, req PromptRequest) (PromptResult, error) {
	s = s.defaults()
	notSubmitted := func(err error) (PromptResult, error) {
		return PromptResult{}, withPromptReceipt(err, req.Target, SubmissionNotSubmitted, PromptWaitNotStarted)
	}
	if s.Terminal == nil {
		return notSubmitted(&Error{Code: ErrTransport, Message: "terminal adapter is unavailable"})
	}
	if strings.TrimSpace(req.Text) == "" {
		return notSubmitted(&Error{Code: ErrNotReady, Message: "prompt text is required", Target: &req.Target})
	}
	if req.Wait && req.Timeout <= 0 {
		return notSubmitted(&Error{Code: ErrNotReady, Message: "--wait requires an explicit --timeout", Target: &req.Target})
	}

	// One tracker serves the preflight and every later observation.
	//
	// Restarting it after submission looked tidier — lifecycle history from
	// before the prompt should not let the previous turn's completion satisfy
	// this one — but it silently broke the stall rule: the reset tracker
	// reported a different status for the very same snapshot, so "the
	// lifecycle moved" was satisfied by the reseeding rather than by the agent
	// reacting, and a prompt into a wedged pane returned success. The stall
	// rule's own requirement — a change away from the status the prompt was
	// sent from — is what keeps a stale completion out, and it only means
	// anything if both readings come from the same tracker.
	var tracker agentactivity.Tracker
	snap, state, err := s.observeOnce(ctx, req.Target, &tracker)
	if err != nil {
		return notSubmitted(err)
	}
	if err := promptable(snap, state); err != nil {
		return notSubmitted(err)
	}
	pinnedKind, before := state.Kind, state.Status
	submissionTarget := snap.Target

	if err := s.Terminal.Submit(ctx, snap, req.Text); err != nil {
		status := SubmissionUnknown
		var typed *Error
		if AsError(err, &typed) && promptWriteRefusal(typed.Code) {
			// Terminal's mutating contract uses semantic refusals only for its
			// revalidation before the first write. Generic/transport errors can
			// land after any step and therefore remain unknown.
			status = SubmissionNotSubmitted
		}
		return PromptResult{}, WithPromptReceipt(transport(snap.Target, err), submissionTarget, status, PromptWaitNotStarted)
	}

	if before != StatusWorking {
		snap, state, err = s.awaitPromptLanded(ctx, snap, state, &tracker, pinnedKind, before)
		if err != nil {
			outcome := PromptWaitNotRequested
			if req.Wait {
				outcome = promptWaitOutcome(err)
			}
			return PromptResult{}, WithPromptReceipt(err, submissionTarget, SubmissionSubmitted, outcome)
		}
		if !req.Wait {
			return promptResult(snap.Target, submissionTarget, state, PromptWaitNotRequested), nil
		}
	} else if !req.Wait {
		// A prompt into an already-working agent cannot be attributed to a new
		// turn, so there is nothing honest to observe and nothing to wait for.
		return promptResult(snap.Target, submissionTarget, state, PromptWaitNotRequested), nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	snap, state, err = s.awaitSettled(waitCtx, snap, state, &tracker, pinnedKind, req.Until)
	if err != nil {
		return PromptResult{}, WithPromptReceipt(err, submissionTarget, SubmissionSubmitted, promptWaitOutcome(err))
	}
	return promptResult(snap.Target, submissionTarget, state, PromptWaitSettled), nil
}

func promptWriteRefusal(code ErrorCode) bool {
	switch code {
	case ErrReplaced, ErrPaneBusy, ErrBlocked, ErrNotReady:
		return true
	default:
		return false
	}
}

func promptResult(target, submissionTarget Target, state AgentState, wait PromptWaitOutcome) PromptResult {
	return PromptResult{Target: target, Agent: state, Receipt: PromptReceipt{
		Submission: SubmissionSubmitted, Wait: wait, Target: submissionTarget,
	}}
}

// WithPromptReceipt attaches the prompt's independently useful submission
// outcome to an existing operational error. target is the identity pinned for
// the write; an observation error may carry a different or absent target, but
// it cannot rewrite which occupant received the prompt.
func WithPromptReceipt(err error, target Target, submission SubmissionStatus, wait PromptWaitOutcome) error {
	var typed *Error
	if !AsError(err, &typed) {
		typed = &Error{Code: ErrTransport, Message: err.Error(), Err: err}
	}
	copy := *typed
	if copy.Target == nil {
		copy.Target = &target
	}
	copy.Receipt = &PromptReceipt{Submission: submission, Wait: wait, Target: target}
	return &copy
}

func withPromptReceipt(err error, target Target, submission SubmissionStatus, wait PromptWaitOutcome) error {
	return WithPromptReceipt(err, target, submission, wait)
}

func promptWaitOutcome(err error) PromptWaitOutcome {
	var typed *Error
	if AsError(err, &typed) {
		switch typed.Code {
		case ErrTimeout:
			return PromptWaitTimeout
		case ErrReplaced:
			return PromptWaitReplaced
		case ErrPromptStalled:
			return PromptWaitStalled
		}
	}
	if errors.Is(err, context.Canceled) {
		return PromptWaitCancelled
	}
	return PromptWaitFailed
}

// Wait observes an already-running agent until it reaches one of the accepted
// states. It never writes to the pane.
func (s Service) Wait(ctx context.Context, req WaitRequest) (Agent, error) {
	s = s.defaults()
	if s.Terminal == nil {
		return Agent{}, &Error{Code: ErrTransport, Message: "terminal adapter is unavailable"}
	}
	if req.Timeout <= 0 {
		return Agent{}, &Error{Code: ErrNotReady, Message: "agent wait requires an explicit --timeout", Target: &req.Target}
	}
	var tracker agentactivity.Tracker
	snap, state, err := s.observeOnce(ctx, req.Target, &tracker)
	if err != nil {
		return Agent{}, err
	}
	if err := liveAgentPane(snap); err != nil {
		return Agent{}, err
	}
	if state.Kind == "" {
		return Agent{}, &Error{Code: ErrNotReady, Message: "no agent provider is identified in the target pane", Target: &snap.Target}
	}

	waitCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	snap, state, err = s.awaitSettled(waitCtx, snap, state, &tracker, state.Kind, req.Until)
	if err != nil {
		return Agent{}, err
	}
	return Agent{Target: snap.Target, Agent: state}, nil
}

// SendKeys writes a validated logical-key sequence to a pinned managed agent.
// Every key is encoded before any of them is sent, so a caller answering a
// blocked agent's UI can never deliver half a sequence.
//
// The validation is the service's, not the adapter's. The local tmux adapter
// re-encodes for its own writes and the CLI validates early to answer a typo
// without spawning anything, but neither of those is where the invariant can
// live: a second Terminal implementation — the remote host adapter this
// interface exists for — would otherwise be free to write a partial sequence
// and nothing above it would notice. Validating here means every adapter
// inherits it by construction.
func (s Service) SendKeys(ctx context.Context, req KeysRequest) (Agent, error) {
	s = s.defaults()
	if s.Terminal == nil {
		return Agent{}, &Error{Code: ErrTransport, Message: "terminal adapter is unavailable"}
	}
	if len(req.Keys) == 0 {
		return Agent{}, &Error{Code: ErrNotReady, Message: "at least one key is required", Target: &req.Target}
	}
	// Before the target is even observed: an unencodable name is the caller's
	// mistake and costs no terminal round trip to find.
	if err := ValidateKeys(req.Keys); err != nil {
		return Agent{}, &Error{Code: ErrNotReady, Message: err.Error(), Target: &req.Target, Err: err}
	}
	var tracker agentactivity.Tracker
	snap, state, err := s.observeOnce(ctx, req.Target, &tracker)
	if err != nil {
		return Agent{}, err
	}
	if err := liveAgentPane(snap); err != nil {
		return Agent{}, err
	}
	if state.Kind == "" {
		return Agent{}, &Error{Code: ErrNotReady, Message: "no agent provider is identified in the target pane; raw terminal input belongs to tmux", Target: &snap.Target}
	}
	if err := s.Terminal.SendKeys(ctx, snap, req.Keys); err != nil {
		return Agent{}, transport(snap.Target, err)
	}
	return Agent{Target: snap.Target, Agent: state}, nil
}

// Read returns a passive view of the target. It never scrolls, resizes, or
// otherwise manipulates the agent's own screen.
func (s Service) Read(ctx context.Context, req ReadRequest) (ReadResult, error) {
	s = s.defaults()
	if s.Terminal == nil {
		return ReadResult{}, &Error{Code: ErrTransport, Message: "terminal adapter is unavailable"}
	}
	source := req.Source
	if source == "" {
		source = SourceVisible
	}
	if !source.valid() {
		return ReadResult{}, &Error{Code: ErrNotReady, Message: fmt.Sprintf("unknown read source %q", req.Source), Target: &req.Target}
	}
	req.Source = source
	var tracker agentactivity.Tracker
	snap, state, err := s.observeOnce(ctx, req.Target, &tracker)
	if err != nil {
		return ReadResult{}, err
	}
	result := ReadResult{Target: snap.Target, Source: source, Kind: state.Kind, Status: state.Status, CapturedAt: snap.CapturedAt}

	if source == SourceTranscript {
		if s.Transcript == nil {
			return ReadResult{}, &Error{Code: ErrTranscriptUnavailable, Message: "this managed shell has no exact provider-reported session binding; run a source that reads the terminal instead", Target: &snap.Target}
		}
		messages, err := s.Transcript.SessionMessages(ctx, snap.Target, req.Lines)
		if err != nil {
			var typed *Error
			if AsError(err, &typed) {
				return ReadResult{}, typed
			}
			return ReadResult{}, &Error{Code: ErrTranscriptUnavailable, Message: err.Error(), Target: &snap.Target, Err: err}
		}
		result.Messages = messages
		return result, nil
	}

	text, err := s.Terminal.Capture(ctx, snap, req)
	if err != nil {
		return ReadResult{}, transport(snap.Target, err)
	}
	result.Text = text
	result.Lines = len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
	if text == "" {
		result.Lines = 0
	}
	return result, nil
}

// observeOnce is the shared passive preflight: one Inspect of the resolved
// target plus the lifecycle verdict, with the same quiet tracker seeding Get
// uses so a provider whose composer has no explicit idle marker is not
// mistaken for an unidentified pane.
func (s Service) observeOnce(ctx context.Context, target Target, tracker *agentactivity.Tracker) (Snapshot, AgentState, error) {
	snap, err := s.Terminal.Inspect(ctx, target)
	if err != nil {
		return Snapshot{}, AgentState{}, transport(target, err)
	}
	tracker.ResetForProcessChange(snap.CapturedAt)
	return snap, s.Detect(snap, tracker), nil
}

// awaitPromptLanded is the PromptStallWindow rule. It ends as soon as the
// lifecycle moves off the state the prompt was sent from.
func (s Service) awaitPromptLanded(ctx context.Context, from Snapshot, fromState AgentState, tracker *agentactivity.Tracker, kind string, before Status) (Snapshot, AgentState, error) {
	stallCtx, cancel := context.WithTimeout(ctx, s.StallAfter)
	defer cancel()
	snap, state, err := s.watch(stallCtx, from, fromState, tracker, func(_ Snapshot, state AgentState) (watchOutcome, error) {
		if err := stillTheSameProvider(from.Target, kind, state); err != nil {
			return watchContinue, err
		}
		// unknown is losing track of the agent, not evidence that it reacted.
		if state.Status != before && state.Status != StatusUnknown {
			return watchSettled, nil
		}
		return watchContinue, nil
	})
	if err == nil {
		return snap, state, nil
	}
	var typed *Error
	// Only the stall window's own deadline is a stall. If the caller's context
	// ended first, that is their timeout or their cancellation, not a verdict
	// about whether the prompt landed.
	if AsError(err, &typed) && typed.Code == ErrTimeout && ctx.Err() == nil {
		pinned := from.Target
		return Snapshot{}, AgentState{}, &Error{
			Code:    ErrPromptStalled,
			Message: fmt.Sprintf("prompt was written but %s did not leave %s within %s; read the target before sending more input", kind, before, s.StallAfter),
			Target:  &pinned,
		}
	}
	return Snapshot{}, AgentState{}, err
}

// awaitSettled observes until the agent reaches one of the accepted states.
func (s Service) awaitSettled(ctx context.Context, from Snapshot, fromState AgentState, tracker *agentactivity.Tracker, kind string, until []Status) (Snapshot, AgentState, error) {
	accepted := until
	if len(accepted) == 0 {
		accepted = DefaultSettledStates()
	}
	return s.watch(ctx, from, fromState, tracker, func(_ Snapshot, state AgentState) (watchOutcome, error) {
		if err := stillTheSameProvider(from.Target, kind, state); err != nil {
			return watchContinue, err
		}
		for _, want := range accepted {
			if state.Status == want {
				return watchSettled, nil
			}
		}
		return watchContinue, nil
	})
}

// stillTheSameProvider is the half of occupant pinning the pane's identity
// fields cannot express. A pane keeps its ID and PID when the agent inside it
// exits and something else takes the composer, so the provider the caller
// addressed is checked on every observation too.
func stillTheSameProvider(pinned Target, kind string, state AgentState) error {
	if state.Kind == "" || state.Kind == kind {
		return nil
	}
	target := pinned
	return &Error{Code: ErrReplaced, Message: fmt.Sprintf("expected %s in the target pane, found %s", kind, state.Kind), Target: &target}
}

// liveAgentPane is the passive-operation floor: the pane must still be the one
// pane of the addressed session, alive, and out of tmux's own modes. It
// deliberately says nothing about the foreground process, because an agent
// running in the pane is exactly what these operations address.
func liveAgentPane(s Snapshot) error {
	t := s.Target
	refuse := func(message string) error { return &Error{Code: ErrPaneBusy, Message: message, Target: &t} }
	if s.PaneCount != 1 {
		return refuse("managed session must contain exactly one pane")
	}
	if s.Dead {
		return refuse("managed pane is dead")
	}
	if s.CopyMode {
		return refuse("managed pane is in copy or another tmux mode; leave it before sending input")
	}
	if s.PaneID == "" || s.PanePID <= 0 || s.ServerPID <= 0 {
		return refuse("managed pane identity is incomplete")
	}
	return nil
}

// promptable is the complete refusal set applied before a single prompt byte
// is written.
func promptable(snap Snapshot, state AgentState) error {
	if err := liveAgentPane(snap); err != nil {
		return err
	}
	target := snap.Target
	if state.Kind == "" {
		return &Error{Code: ErrNotReady, Message: "no agent provider is identified in the target pane", Target: &target}
	}
	if state.Status == StatusBlocked {
		return &Error{Code: ErrBlocked, Message: "agent is blocked; read the screen and answer it with agent send-keys", Target: &target}
	}
	if state.Freshness != "current" {
		return &Error{Code: ErrNotReady, Message: fmt.Sprintf("agent status is %s, not current; nothing is sent to a target Sidecar cannot vouch for", state.Freshness), Target: &target}
	}
	for _, ok := range PromptableStates() {
		if state.Status == ok {
			return nil
		}
	}
	return &Error{Code: ErrNotReady, Message: fmt.Sprintf("agent status is %s; prompt accepts idle, done, or working", state.Status), Target: &target}
}

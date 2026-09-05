// Package agentcontrol owns provider-aware control of Sidecar-managed shells.
// It is transport-neutral: CLI and UI callers use the same service, while the
// local implementation speaks tmux through Terminal.
package agentcontrol

import (
	"encoding/json"
	"fmt"
	"time"
)

type Status string

const (
	StatusUnknown Status = "unknown"
	StatusIdle    Status = "idle"
	StatusWorking Status = "working"
	StatusBlocked Status = "blocked"
	StatusDone    Status = "done"
)

// Target is the pinned, host-shaped identity of one managed pane.
type Target struct {
	Host      string `json:"host"`
	Project   string `json:"project"`
	Session   string `json:"session"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	PaneID    string `json:"paneId,omitempty"`
	PanePID   int    `json:"panePid,omitempty"`
	// ServerPID is the tmux server process, and it — not ServerIncarnation — is
	// what the occupant check compares.
	//
	// The incarnation string includes the socket's ctime, and tmux rewrites the
	// socket's permission bits whenever the set of attached clients changes
	// (server_update_socket). Attaching M2's own control-mode observer
	// therefore bumps the ctime, and an incarnation-based pin would report
	// every observed target as replaced the instant it began observing it. The
	// server pid answers the question the pin is actually asking — is this the
	// same server process? — and cannot drift because somebody attached.
	ServerPID int `json:"serverPid,omitempty"`
	// ServerIncarnation stays as observed evidence about the socket. It is
	// reported, not compared.
	ServerIncarnation string `json:"serverIncarnation,omitempty"`
}

type AgentState struct {
	Kind             string    `json:"kind,omitempty"`
	Status           Status    `json:"status"`
	Freshness        string    `json:"freshness"`
	Attention        bool      `json:"attention"`
	Evidence         string    `json:"evidence,omitempty"`
	ChangedAt        time.Time `json:"changedAt,omitzero"`
	CapturedAt       time.Time `json:"capturedAt"`
	InteractiveReady bool      `json:"interactiveReady"`
	// SessionRef reports whether this shell is bound to an exact provider
	// conversation. It is absent when there is no binding.
	SessionRef *SessionRef `json:"sessionRef,omitempty"`
}

// SessionRef is what an agent query says about a shell's exact conversation
// binding.
//
// Kind and Reported are capability and presence: they answer "can this shell be
// resumed, and did an official integration say so" without naming the
// conversation. Value is the conversation itself and is filled only for a
// caller's own shell or an explicit opt-in, because `agent list` output lands in
// logs, transcripts, and CI output, and a conversation identifier sprayed across
// those is not something a later decision to redact can take back.
type SessionRef struct {
	Kind     string `json:"kind,omitempty"`
	Reported bool   `json:"reported"`
	Value    string `json:"value,omitempty"`
}

// Agent is the stable result shared by list, get, and start.
type Agent struct {
	Target Target     `json:"target"`
	Agent  AgentState `json:"agent"`
}

// SubmissionStatus is the certainty Sidecar has about a prompt crossing the
// terminal boundary. Unknown is deliberate: a transport or terminal write can
// fail after applying input, and reporting either submitted or not_submitted in
// that case would invite an unsafe automatic retry.
type SubmissionStatus string

const (
	SubmissionNotSubmitted SubmissionStatus = "not_submitted"
	SubmissionSubmitted    SubmissionStatus = "submitted"
	SubmissionUnknown      SubmissionStatus = "unknown"
)

// PromptWaitOutcome describes the second half of `agent prompt --wait` without
// changing the command's established error code or exit status.
type PromptWaitOutcome string

const (
	PromptWaitNotRequested PromptWaitOutcome = "not_requested"
	PromptWaitNotStarted   PromptWaitOutcome = "not_started"
	PromptWaitSettled      PromptWaitOutcome = "settled"
	PromptWaitTimeout      PromptWaitOutcome = "timeout"
	PromptWaitCancelled    PromptWaitOutcome = "cancelled"
	PromptWaitReplaced     PromptWaitOutcome = "replaced"
	PromptWaitStalled      PromptWaitOutcome = "stalled"
	PromptWaitFailed       PromptWaitOutcome = "failed"
)

// PromptReceipt is the additive prompt contract carried on success and error.
// Target is the identity pinned before submission; callers can decide whether
// a retry is safe without reconstructing identity from prose.
type PromptReceipt struct {
	Submission SubmissionStatus  `json:"submission"`
	Wait       PromptWaitOutcome `json:"wait"`
	Target     Target            `json:"target"`
}

// PromptResult preserves Agent's top-level target/agent shape and adds the
// receipt, so existing JSON readers keep decoding the fields they know.
type PromptResult struct {
	Target  Target        `json:"target"`
	Agent   AgentState    `json:"agent"`
	Receipt PromptReceipt `json:"receipt"`
}

type ErrorCode string

const (
	ErrNotFound              ErrorCode = "agent_not_found"
	ErrPaneBusy              ErrorCode = "agent_pane_busy"
	ErrKindMismatch          ErrorCode = "agent_kind_mismatch"
	ErrNotReady              ErrorCode = "agent_not_ready"
	ErrStartFailed           ErrorCode = "agent_start_failed"
	ErrBlocked               ErrorCode = "agent_blocked"
	ErrPromptStalled         ErrorCode = "agent_prompt_stalled"
	ErrReplaced              ErrorCode = "agent_replaced"
	ErrTranscriptUnavailable ErrorCode = "transcript_unavailable"
	ErrTimeout               ErrorCode = "timeout"
	ErrTransport             ErrorCode = "transport_failed"
	ErrFeatureDisabled       ErrorCode = "feature_disabled"
	// ErrUsage is a command line the verb could not accept: a refused flag
	// combination, a missing argument. It exists so a --json caller reads the
	// refusal as the same envelope every other refusal arrives in, rather
	// than a prose line followed by the help text (td-a658ed).
	ErrUsage ErrorCode = "usage"
	// ErrHostUnavailable and ErrVersionSkew are M5 additions, and they exist
	// because a remote target has two failure modes the local vocabulary
	// cannot express without lying about them.
	//
	// A machine that is not reachable is not a transport that failed
	// mid-operation: nothing was attempted, retrying later is the fix, and
	// reporting transport_failed for it sends a reader looking for a fault
	// that is not there. And a host whose Sidecar does not know the verb
	// answers with a usage error — the exit-code contract's own capability
	// negotiation — where the fix is to update one of the two binaries and no
	// other code says so. Both keep the exit status the CLI already documents:
	// host_unavailable exits 1 with the other retryable failures, version_skew
	// exits 2, which agentExitCodes has always described as "usage error or
	// version skew".
	ErrHostUnavailable ErrorCode = "host_unavailable"
	ErrVersionSkew     ErrorCode = "version_skew"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Target  *Target   `json:"target,omitempty"`
	// Receipt is present for agent prompt failures. Other verbs keep their
	// existing envelope unchanged.
	Receipt *PromptReceipt `json:"receipt,omitempty"`
	Err     error          `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}
func (e *Error) Unwrap() error { return e.Err }

// ErrorEnvelope is the machine stderr contract.
type ErrorEnvelope struct {
	Error *Error `json:"error"`
}

func MarshalError(err error) []byte {
	var e *Error
	if !AsError(err, &e) {
		e = &Error{Code: ErrTransport, Message: err.Error(), Err: err}
	}
	b, marshalErr := json.Marshal(ErrorEnvelope{Error: e})
	if marshalErr != nil {
		return []byte(fmt.Sprintf(`{"error":{"code":"transport_failed","message":%q}}`, err.Error()))
	}
	return b
}

func AsError(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}

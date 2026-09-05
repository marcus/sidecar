package agentcontrol

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func contractTarget() Target {
	return Target{Host: "local", Project: "sidecar", Session: "sidecar-sh-sidecar-4", Name: "reviewer", Namespace: "/private/tmp/tmux/default", PaneID: "%7", PanePID: 4242, ServerPID: 30, ServerIncarnation: "present inode=10 ctime=20 pid=30"}
}

func assertJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("schema drift in %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestAgentAndErrorJSONContracts(t *testing.T) {
	target := contractTarget()
	agent := Agent{Target: target, Agent: AgentState{Kind: "codex", Status: StatusBlocked, Freshness: "current", Attention: true, Evidence: "codex.approval.command", ChangedAt: time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC), CapturedAt: time.Date(2026, 8, 30, 17, 0, 1, 0, time.UTC)}}
	assertJSONFixture(t, "testdata/agent.json", agent)
	assertJSONFixture(t, "testdata/error.json", ErrorEnvelope{Error: &Error{Code: ErrReplaced, Message: "managed pane was replaced", Target: &target}})
}

func TestStatusAndErrorCodeVocabularyIsFrozen(t *testing.T) {
	statuses := []Status{StatusUnknown, StatusIdle, StatusWorking, StatusBlocked, StatusDone}
	wantStatuses := []Status{"unknown", "idle", "working", "blocked", "done"}
	if !reflect.DeepEqual(statuses, wantStatuses) {
		t.Fatalf("statuses = %#v", statuses)
	}
	// host_unavailable and version_skew were added by M5 for remote targets.
	// The vocabulary is frozen against accidental drift, not against a
	// milestone that states what it is adding and why; see types.go.
	codes := []ErrorCode{ErrNotFound, ErrPaneBusy, ErrKindMismatch, ErrNotReady, ErrStartFailed, ErrBlocked, ErrPromptStalled, ErrReplaced, ErrTranscriptUnavailable, ErrTimeout, ErrTransport, ErrFeatureDisabled, ErrUsage, ErrHostUnavailable, ErrVersionSkew}
	wantCodes := []ErrorCode{"agent_not_found", "agent_pane_busy", "agent_kind_mismatch", "agent_not_ready", "agent_start_failed", "agent_blocked", "agent_prompt_stalled", "agent_replaced", "transcript_unavailable", "timeout", "transport_failed", "feature_disabled", "usage", "host_unavailable", "version_skew"}
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("error codes = %#v", codes)
	}
	submissions := []SubmissionStatus{SubmissionNotSubmitted, SubmissionSubmitted, SubmissionUnknown}
	wantSubmissions := []SubmissionStatus{"not_submitted", "submitted", "unknown"}
	if !reflect.DeepEqual(submissions, wantSubmissions) {
		t.Fatalf("submission statuses = %#v", submissions)
	}
	waits := []PromptWaitOutcome{PromptWaitNotRequested, PromptWaitNotStarted, PromptWaitSettled, PromptWaitTimeout, PromptWaitCancelled, PromptWaitReplaced, PromptWaitStalled, PromptWaitFailed}
	wantWaits := []PromptWaitOutcome{"not_requested", "not_started", "settled", "timeout", "cancelled", "replaced", "stalled", "failed"}
	if !reflect.DeepEqual(waits, wantWaits) {
		t.Fatalf("prompt wait outcomes = %#v", waits)
	}
}

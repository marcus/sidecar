package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/shellstate"
)

func agentFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "agentactivity", "testdata", "codex", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runAgentCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	handled, code := Run(append([]string{"--enable-feature=agent_control"}, args...), &out, &errOut)
	if !handled {
		t.Fatalf("Run(%v) was not handled", args)
	}
	return code, out.String(), errOut.String()
}

// TestAgentPromptSendsWaitsAndReportsUnderOnePinnedTarget covers the happy path
// of the two verbs that write: the prompt lands, the wait settles, and both
// answer with the same Agent shape list/get/start use.
func TestAgentPromptSendsWaitsAndReportsUnderOnePinnedTarget(t *testing.T) {
	screens := []string{agentFixture(t, "startup_idle.txt"), agentFixture(t, "working.txt"), agentFixture(t, "completed.txt")}
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screens: screens}
	useCLIAgentTerminal(t, terminal)

	code, out, errOut := runAgentCLI(t, "agent", "prompt", "sidecar-sh-demo-2", "Review the current diff.", "--wait", "--timeout", "10s", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("prompt = %d stdout=%q stderr=%q", code, out, errOut)
	}
	var result agentcontrol.PromptResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("prompt JSON %q: %v", out, err)
	}
	if result.Target.Session != "sidecar-sh-demo-2" || result.Target.PaneID != "%11" || result.Agent.Kind != "codex" {
		t.Fatalf("prompt agent = %+v", result)
	}
	settled := result.Agent.Status == agentcontrol.StatusDone || result.Agent.Status == agentcontrol.StatusIdle || result.Agent.Status == agentcontrol.StatusBlocked
	if !settled {
		t.Fatalf("prompt returned at %s, which is not a settled state", result.Agent.Status)
	}
	if result.Receipt.Submission != agentcontrol.SubmissionSubmitted || result.Receipt.Wait != agentcontrol.PromptWaitSettled || result.Receipt.Target != result.Target {
		t.Fatalf("prompt receipt = %+v", result.Receipt)
	}
	if terminal.inspects < 3 {
		t.Fatalf("inspected %d times; --wait returned without observing the turn", terminal.inspects)
	}
	if len(terminal.submitted) != 1 || terminal.submitted[0] != "Review the current diff." {
		t.Fatalf("submitted = %q", terminal.submitted)
	}
}

func TestAgentPromptUsesTheCurrentShellWhenNoTargetIsNamed(t *testing.T) {
	screens := []string{agentFixture(t, "startup_idle.txt"), agentFixture(t, "working.txt")}
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screens: screens}
	useCLIAgentTerminal(t, terminal)
	t.Setenv(shellstate.SessionEnv, "sidecar-sh-demo-1")

	code, out, errOut := runAgentCLI(t, "agent", "prompt", "look at the failing test", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("prompt = %d stdout=%q stderr=%q", code, out, errOut)
	}
	var result agentcontrol.PromptResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Target.Session != "sidecar-sh-demo-1" {
		t.Fatalf("target = %+v, want the current shell", result.Target)
	}
	if result.Receipt.Submission != agentcontrol.SubmissionSubmitted || result.Receipt.Wait != agentcontrol.PromptWaitNotRequested {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if len(terminal.submitted) != 1 || terminal.submitted[0] != "look at the failing test" {
		t.Fatalf("submitted = %q; the single positional is the prompt, not a target", terminal.submitted)
	}
}

func TestAgentPromptRefusesABlockedTargetWithoutWritingBytes(t *testing.T) {
	blocked := agentFixture(t, "blocked.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: blocked}
	useCLIAgentTerminal(t, terminal)

	code, out, errOut := runAgentCLI(t, "agent", "prompt", "sidecar-sh-demo-2", "do it anyway", "--json")
	if code != 5 || out != "" || !strings.Contains(errOut, `"code":"agent_blocked"`) {
		t.Fatalf("blocked prompt = %d stdout=%q stderr=%q", code, out, errOut)
	}
	if len(terminal.submitted) != 0 {
		t.Fatalf("a refusal wrote %q", terminal.submitted)
	}
	var envelope agentcontrol.ErrorEnvelope
	if err := json.Unmarshal([]byte(errOut), &envelope); err != nil || envelope.Error == nil || envelope.Error.Receipt == nil || envelope.Error.Receipt.Submission != agentcontrol.SubmissionNotSubmitted {
		t.Fatalf("blocked receipt = %+v, decode err %v", envelope, err)
	}
}

func TestAgentPromptPreServiceRefusalsReportNotSubmitted(t *testing.T) {
	assertReceipt := func(t *testing.T, code int, out, errOut string, wantCode agentcontrol.ErrorCode) {
		t.Helper()
		if out != "" {
			t.Fatalf("stdout = %q, want empty", out)
		}
		var envelope agentcontrol.ErrorEnvelope
		if err := json.Unmarshal([]byte(errOut), &envelope); err != nil {
			t.Fatalf("stderr %q: %v", errOut, err)
		}
		if envelope.Error == nil || envelope.Error.Code != wantCode || envelope.Error.Receipt == nil || envelope.Error.Receipt.Submission != agentcontrol.SubmissionNotSubmitted || envelope.Error.Receipt.Wait != agentcontrol.PromptWaitNotStarted {
			t.Fatalf("exit %d envelope = %+v, want %s not_submitted/not_started", code, envelope, wantCode)
		}
	}

	t.Run("feature disabled", func(t *testing.T) {
		targetProject(t)
		var out, errOut bytes.Buffer
		_, code := Run([]string{"agent", "prompt", "sidecar-sh-demo-2", "go", "--json"}, &out, &errOut)
		assertReceipt(t, code, out.String(), errOut.String(), agentcontrol.ErrFeatureDisabled)
	})

	t.Run("target not found", func(t *testing.T) {
		targetProject(t)
		code, out, errOut := runAgentCLI(t, "agent", "prompt", "missing", "go", "--json")
		assertReceipt(t, code, out, errOut, agentcontrol.ErrNotFound)
	})

	t.Run("ambiguous target", func(t *testing.T) {
		_, stateDir := setupIsolatedCLI(t)
		alpha, beta := t.TempDir(), t.TempDir()
		writeProjectMeta(t, stateDir, "alpha", alpha)
		writeProjectMeta(t, stateDir, "beta", beta)
		writeProjectShells(t, stateDir, "alpha", shellstate.Definition{TmuxName: "sidecar-sh-alpha-1", DisplayName: "reviewer", WorkDir: alpha})
		writeProjectShells(t, stateDir, "beta", shellstate.Definition{TmuxName: "sidecar-sh-beta-1", DisplayName: "reviewer", WorkDir: beta})
		t.Chdir(t.TempDir())
		code, out, errOut := runAgentCLI(t, "agent", "prompt", "reviewer", "go", "--json")
		assertReceipt(t, code, out, errOut, agentcontrol.ErrTransport)
	})
}

func TestRemoteAgentPromptPreTransportRefusalsReportNotSubmitted(t *testing.T) {
	run := func(t *testing.T, configBody string, args ...string) *agentcontrol.Error {
		t.Helper()
		stateHome, _ := setupIsolatedCLI(t)
		cfgPath := filepath.Join(stateHome, "config.json")
		if err := os.WriteFile(cfgPath, []byte(configBody), 0o644); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		full := append([]string{"-config", cfgPath, "agent", "prompt"}, args...)
		handled, _ := Run(full, &out, &errOut)
		if !handled || out.Len() != 0 {
			t.Fatalf("Run(%v) handled=%v stdout=%q stderr=%q", full, handled, out.String(), errOut.String())
		}
		var envelope agentcontrol.ErrorEnvelope
		if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
			t.Fatalf("stderr %q: %v", errOut.String(), err)
		}
		if envelope.Error == nil || envelope.Error.Receipt == nil || envelope.Error.Receipt.Submission != agentcontrol.SubmissionNotSubmitted || envelope.Error.Receipt.Wait != agentcontrol.PromptWaitNotStarted {
			t.Fatalf("envelope = %+v, want not_submitted/not_started", envelope)
		}
		return envelope.Error
	}

	const enabled = `{"features":{"flags":{"agent_control":true,"sidecar_remote_hosts":true}},"hosts":{"list":[{"id":"book","target":"book"}]}}`
	t.Run("missing explicit target", func(t *testing.T) {
		got := run(t, enabled, "go", "--host", "book", "--json")
		if got.Code != agentcontrol.ErrNotFound || got.Receipt.Target.Host != "book" || got.Receipt.Target.Session != "" {
			t.Fatalf("error = %+v", got)
		}
	})
	t.Run("unknown host", func(t *testing.T) {
		got := run(t, enabled, "reviewer", "go", "--host", "missing", "--json")
		if got.Code != agentcontrol.ErrHostUnavailable || got.Receipt.Target.Host != "missing" || got.Receipt.Target.Session != "reviewer" {
			t.Fatalf("error = %+v", got)
		}
	})
	t.Run("remote feature disabled", func(t *testing.T) {
		got := run(t, `{"features":{"flags":{"agent_control":true,"sidecar_remote_hosts":false}}}`, "reviewer", "go", "--host", "book", "--json")
		if got.Code != agentcontrol.ErrFeatureDisabled || got.Receipt.Target.Host != "book" || got.Receipt.Target.Session != "reviewer" {
			t.Fatalf("error = %+v", got)
		}
	})
}

func TestAgentPromptWaitTimeoutReportsSubmittedReceipt(t *testing.T) {
	working := agentFixture(t, "working.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: working}
	useCLIAgentTerminal(t, terminal)

	code, out, errOut := runAgentCLI(t, "agent", "prompt", "sidecar-sh-demo-2", "keep going", "--wait", "--timeout", "20ms", "--json")
	if code != 1 || out != "" {
		t.Fatalf("timeout = %d stdout=%q stderr=%q", code, out, errOut)
	}
	var envelope agentcontrol.ErrorEnvelope
	if err := json.Unmarshal([]byte(errOut), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != agentcontrol.ErrTimeout || envelope.Error.Receipt == nil || envelope.Error.Receipt.Submission != agentcontrol.SubmissionSubmitted || envelope.Error.Receipt.Wait != agentcontrol.PromptWaitTimeout || envelope.Error.Receipt.Target.PaneID != "%11" {
		t.Fatalf("timeout envelope = %+v", envelope)
	}
	if len(terminal.submitted) != 1 || terminal.submitted[0] != "keep going" {
		t.Fatalf("submitted = %q", terminal.submitted)
	}
}

func TestAgentPromptHumanOutputDistinguishesTimeoutFromRefusal(t *testing.T) {
	working := agentFixture(t, "working.txt")
	blocked := agentFixture(t, "blocked.txt")
	t.Run("submitted but not settled", func(t *testing.T) {
		targetProject(t)
		terminal := &cliAgentTerminal{launched: true, screen: working}
		useCLIAgentTerminal(t, terminal)
		code, out, errOut := runAgentCLI(t, "agent", "prompt", "sidecar-sh-demo-2", "keep going", "--wait", "--timeout", "20ms")
		if code != 1 || out != "" || !strings.Contains(errOut, "Prompt submitted") || !strings.Contains(errOut, "did not settle") {
			t.Fatalf("timeout = %d stdout=%q stderr=%q", code, out, errOut)
		}
	})
	t.Run("refused before submission", func(t *testing.T) {
		targetProject(t)
		terminal := &cliAgentTerminal{launched: true, screen: blocked}
		useCLIAgentTerminal(t, terminal)
		code, out, errOut := runAgentCLI(t, "agent", "prompt", "sidecar-sh-demo-2", "do it anyway")
		if code != 5 || out != "" || !strings.Contains(errOut, "was not submitted") {
			t.Fatalf("refusal = %d stdout=%q stderr=%q", code, out, errOut)
		}
	})
}

func TestAgentWaitAndPromptRefuseAnImplicitTimeout(t *testing.T) {
	idle := agentFixture(t, "startup_idle.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: idle}
	useCLIAgentTerminal(t, terminal)

	for _, args := range [][]string{
		{"agent", "wait", "sidecar-sh-demo-2", "--json"},
		{"agent", "prompt", "sidecar-sh-demo-2", "go", "--wait", "--json"},
	} {
		code, out, errOut := runAgentCLI(t, args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "timeout") {
			t.Fatalf("%v = %d stdout=%q stderr=%q; want a usage error", args, code, out, errOut)
		}
	}
	if len(terminal.submitted) != 0 {
		t.Fatalf("a usage error wrote %q", terminal.submitted)
	}

	// --timeout and --until without --wait are equally a usage error: silently
	// ignoring them would let a caller believe it waited.
	if code, _, _ := runAgentCLI(t, "agent", "prompt", "sidecar-sh-demo-2", "go", "--timeout", "1s"); code != 2 {
		t.Fatalf("--timeout without --wait = %d, want 2", code)
	}
}

// A lone positional is the prompt text — except when it names a managed
// target, in which case the caller left the text off and carrying on would type
// a shell's own name into the shell the caller is sitting in. Empty text is a
// usage error by the same reasoning: exit 2 is "you built the command line
// wrong", and a caller cannot act on the difference if it arrives as an
// operational refusal.
func TestAgentPromptRefusesAMissingOrEmptyPromptWithoutWriting(t *testing.T) {
	idle := agentFixture(t, "startup_idle.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: idle}
	useCLIAgentTerminal(t, terminal)
	t.Setenv(shellstate.SessionEnv, "sidecar-sh-demo-1")

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a lone target name is a missing prompt", []string{"agent", "prompt", "sidecar-sh-demo-2"}, "the prompt text is missing"},
		{"empty text", []string{"agent", "prompt", "sidecar-sh-demo-2", ""}, "must not be empty"},
		{"blank text", []string{"agent", "prompt", "   "}, "must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runAgentCLI(t, tc.args...)
			if code != 2 {
				t.Fatalf("%v = exit %d, want 2 (a usage error); stderr=%q", tc.args, code, errOut)
			}
			if out != "" {
				t.Fatalf("a usage error wrote to stdout: %q", out)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Fatalf("stderr = %q, want it to name %q", errOut, tc.want)
			}
		})
	}
	if len(terminal.submitted) != 0 {
		t.Fatalf("a usage error still wrote %q to a pane", terminal.submitted)
	}

	// The guard is about targets, not about words: an ordinary prompt that is
	// not a target's name still reaches the caller's own shell. This fixture's
	// screen never moves, so the command goes on to report a stall — what
	// matters here is that it was delivered rather than refused as usage.
	if code, _, errOut := runAgentCLI(t, "agent", "prompt", "look at the failing test"); code == 2 {
		t.Fatalf("an ordinary one-argument prompt was refused as usage: %q", errOut)
	}
	if len(terminal.submitted) != 1 || terminal.submitted[0] != "look at the failing test" {
		t.Fatalf("submitted = %q", terminal.submitted)
	}
}

func TestAgentWaitRejectsAnUnknownState(t *testing.T) {
	targetProject(t)
	code, _, errOut := runAgentCLI(t, "agent", "wait", "sidecar-sh-demo-2", "--until", "settled", "--timeout", "1s")
	if code != 2 || !strings.Contains(errOut, "unknown status") {
		t.Fatalf("code = %d stderr = %q", code, errOut)
	}
}

func TestAgentReadPassesTheSourceThroughAndPrintsTheText(t *testing.T) {
	idle := agentFixture(t, "startup_idle.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: idle}
	useCLIAgentTerminal(t, terminal)

	code, out, errOut := runAgentCLI(t, "agent", "read", "sidecar-sh-demo-2", "--source", "recent-unwrapped", "--lines", "120")
	if code != 0 || errOut != "" || !strings.Contains(out, "recent-unwrapped capture") {
		t.Fatalf("read = %d stdout=%q stderr=%q", code, out, errOut)
	}
	if len(terminal.captured) != 1 || terminal.captured[0].Source != agentcontrol.SourceRecentUnwrapped || terminal.captured[0].Lines != 120 {
		t.Fatalf("capture request = %+v", terminal.captured)
	}

	code, out, errOut = runAgentCLI(t, "agent", "read", "sidecar-sh-demo-2")
	if code != 0 || errOut != "" || !strings.Contains(out, "visible capture") {
		t.Fatalf("omitted --source = %d stdout=%q stderr=%q", code, out, errOut)
	}
	if len(terminal.captured) != 2 || terminal.captured[1].Source != agentcontrol.SourceVisible {
		t.Fatalf("omitted --source capture = %+v", terminal.captured)
	}

	code, out, errOut = runAgentCLI(t, "agent", "read", "sidecar-sh-demo-2", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("read json = %d stderr=%q", code, errOut)
	}
	var result agentcontrol.ReadResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Source != agentcontrol.SourceVisible || result.Kind != "codex" || result.Target.Session != "sidecar-sh-demo-2" {
		t.Fatalf("read result = %+v", result)
	}

	code, _, errOut = runAgentCLI(t, "agent", "read", "sidecar-sh-demo-2", "--source", "screenshot")
	if code != 2 || !strings.Contains(errOut, "unknown source") {
		t.Fatalf("unknown source = %d stderr=%q", code, errOut)
	}
}

func TestAgentReadTranscriptIsUnavailableWithoutAnExactBinding(t *testing.T) {
	idle := agentFixture(t, "startup_idle.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: idle}
	useCLIAgentTerminal(t, terminal)

	code, out, errOut := runAgentCLI(t, "agent", "read", "sidecar-sh-demo-2", "--source", "transcript", "--json")
	if code != 5 || out != "" || !strings.Contains(errOut, `"code":"transcript_unavailable"`) {
		t.Fatalf("transcript read = %d stdout=%q stderr=%q", code, out, errOut)
	}
	if len(terminal.captured) != 0 {
		t.Fatal("an unavailable transcript fell back to scraping the terminal")
	}
}

func TestAgentSendKeysValidatesBeforeItResolvesOrWrites(t *testing.T) {
	blocked := agentFixture(t, "blocked.txt")
	targetProject(t)
	terminal := &cliAgentTerminal{launched: true, screen: blocked}
	useCLIAgentTerminal(t, terminal)

	code, out, errOut := runAgentCLI(t, "agent", "send-keys", "sidecar-sh-demo-2", "down", "enter", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("send-keys = %d stdout=%q stderr=%q", code, out, errOut)
	}
	if strings.Join(terminal.keys, ",") != "down,enter" {
		t.Fatalf("keys = %q", terminal.keys)
	}

	terminal.keys = nil
	code, out, errOut = runAgentCLI(t, "agent", "send-keys", "sidecar-sh-demo-2", "down", "cmd+q")
	if code != 2 || out != "" || !strings.Contains(errOut, "cmd+q") {
		t.Fatalf("bad key = %d stdout=%q stderr=%q; want a usage error naming the key", code, out, errOut)
	}
	if len(terminal.keys) != 0 {
		t.Fatalf("one bad key still wrote %q", terminal.keys)
	}

	// One positional is a key for the current shell, not a target.
	t.Setenv(shellstate.SessionEnv, "sidecar-sh-demo-1")
	terminal.keys = nil
	code, _, errOut = runAgentCLI(t, "agent", "send-keys", "esc", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("current-shell send-keys = %d stderr=%q", code, errOut)
	}
	if strings.Join(terminal.keys, ",") != "esc" {
		t.Fatalf("keys = %q", terminal.keys)
	}
}

func TestAgentInteractVerbsAreDiscoverableWhileTheFeatureIsDisabled(t *testing.T) {
	setupIsolatedCLI(t)
	for _, verb := range []string{"prompt", "wait", "read", "send-keys"} {
		var out, errOut bytes.Buffer
		handled, code := Run([]string{"agent", verb, "--help"}, &out, &errOut)
		if !handled || code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "sidecar agent "+verb) {
			t.Fatalf("%s help = handled=%v code=%d stderr=%q", verb, handled, code, errOut.String())
		}
	}
	for _, args := range [][]string{
		{"agent", "prompt", "sidecar-sh-demo-2", "go", "--json"},
		{"agent", "wait", "sidecar-sh-demo-2", "--timeout", "1s", "--json"},
		{"agent", "read", "sidecar-sh-demo-2", "--json"},
		{"agent", "send-keys", "sidecar-sh-demo-2", "enter", "--json"},
	} {
		var out, errOut bytes.Buffer
		handled, code := Run(append([]string{"--disable-feature=agent_control"}, args...), &out, &errOut)
		if !handled || code != 5 || !strings.Contains(errOut.String(), `"code":"feature_disabled"`) {
			t.Fatalf("%v while disabled = handled=%v code=%d stderr=%q", args, handled, code, errOut.String())
		}
	}
}

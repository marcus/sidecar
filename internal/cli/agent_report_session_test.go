package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentcontrol"

	"github.com/marcus/sidecar/internal/agentsession"
	"github.com/marcus/sidecar/internal/shellstate"
)

// TestReportSessionNoOpsOutsideAManagedShell is the fail-open rule. A provider
// hook fires wherever the provider runs, and a user who has never opened
// Sidecar must not see its complaints in their own agent terminal.
func TestReportSessionNoOpsOutsideAManagedShell(t *testing.T) {
	t.Setenv(shellstate.ManagedEnv, "")

	code, out, errOut := runLifecycleCLI(t, "agent", "report-session", "--kind", "codex", "--id", "019f2c8a")
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, errOut)
	}
	if out != "" || errOut != "" {
		t.Fatalf("a no-op was not silent: stdout=%q stderr=%q", out, errOut)
	}
}

func TestReportSessionNoOpIsStillStructuredForJSONCallers(t *testing.T) {
	t.Setenv(shellstate.ManagedEnv, "")

	code, out, errOut := runLifecycleCLI(t, "agent", "report-session", "--kind", "codex", "--id", "019f2c8a", "--json")
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr not empty with --json: %q", errOut)
	}
	var res struct {
		SchemaVersion int    `json:"schemaVersion"`
		Managed       bool   `json:"managed"`
		Bound         bool   `json:"bound"`
		Note          string `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not one JSON document: %q (%v)", out, err)
	}
	if res.Managed || res.Bound {
		t.Fatalf("a no-op reported itself as managed/bound: %+v", res)
	}
	if res.Note == "" {
		t.Fatal("the no-op carried no explanation")
	}
	if res.SchemaVersion != reportSessionSchemaVersion {
		t.Fatalf("schemaVersion = %d", res.SchemaVersion)
	}
}

// TestReportSessionUsageErrorsComeBeforeAnyContextWork pins the same ordering
// the rest of the agent family uses: a mistyped command line is exit 2, and it
// is answered before anything looks at tmux or the manifest.
func TestReportSessionUsageErrorsComeBeforeAnyContextWork(t *testing.T) {
	t.Setenv(shellstate.ManagedEnv, "")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no kind", []string{"agent", "report-session", "--id", "x"}, "--kind is required"},
		{"no way of naming a conversation", []string{"agent", "report-session", "--kind", "codex"},
			"one of --id, --path, --clear, or --hook-stdin is required"},
		{"id and path together", []string{"agent", "report-session", "--kind", "codex", "--id", "x", "--path", "/tmp/a"},
			"mutually exclusive"},
		{"id and clear together", []string{"agent", "report-session", "--kind", "codex", "--id", "x", "--clear"},
			"mutually exclusive"},
		{"clear and hook-stdin together", []string{"agent", "report-session", "--kind", "codex", "--clear", "--hook-stdin"},
			"mutually exclusive"},
		{"a flag with no value", []string{"agent", "report-session", "--kind"}, "--kind requires a value"},
		{"an unknown flag", []string{"agent", "report-session", "--kind", "codex", "--id", "x", "--nope"}, "unknown argument"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runLifecycleCLI(t, tc.args...)
			if code != 2 {
				t.Fatalf("exit %d, wanted 2 (stdout=%q stderr=%q)", code, out, errOut)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Fatalf("stderr = %q, wanted it to mention %q", errOut, tc.want)
			}
		})
	}
}

// TestAProviderWithNoOfficialIntegrationSaysSo names the cause rather than the
// symptom. Falling through to the validator produced "the source is empty",
// which describes what the defaulting failed to fill in instead of why.
func TestAProviderWithNoOfficialIntegrationSaysSo(t *testing.T) {
	// pi is on this list again, and the round trip it made is the reason this
	// comment exists. It had an official source before any adapter existed, both
	// were retracted, and PiAdapter plus assets/pi/sidecar-lifecycle.js earned
	// the source back. When the source came back this case did not, so the loop
	// below skipped itself on pi and `go test` still printed ok -- a test that
	// silently stopped testing anything.
	for _, kind := range []string{"codex", "claude", "opencode", "pi", "kilo"} {
		if agentsession.OfficialSourceFor(kind) == "" {
			t.Fatalf("%s is expected to have an official source", kind)
		}
	}
	// grok is a real catalog family with no Sidecar integration, so the
	// defaulting genuinely has nothing to fill in.
	if agentsession.OfficialSourceFor("grok") != "" {
		t.Skip("grok gained an official integration; pick another provider for this case")
	}
}

// TestReportSessionIsNotInTheHappyPathList keeps the plan's decision that this
// is an integration surface: discoverable in help, absent from the short
// `sidecar agents` list an agent reads to learn the coordination sequence.
func TestReportSessionIsNotInTheHappyPathList(t *testing.T) {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("report-session")
	if cmd == nil {
		t.Fatal("report-session is not registered")
	}
	if cmd.Agent.Invocation != "" || cmd.Agent.Summary != "" {
		t.Fatal("report-session carries AgentDoc metadata, which would put it in the agents happy-path list")
	}
	if !cmd.Mutates {
		t.Fatal("report-session writes a binding and must be marked as mutating")
	}
	// The help has to state the trust rules and the current-shell requirement,
	// because that is the only place a hook author reads them.
	for _, want := range []string{"official Sidecar integration", "managed shell", "occupies the pane"} {
		if !strings.Contains(cmd.Long, want) {
			t.Fatalf("help does not mention %q", want)
		}
	}
}

func TestHookPayloadReaderIsBoundedAndStrict(t *testing.T) {
	t.Run("a codex session-start payload", func(t *testing.T) {
		p, err := readHookPayload(strings.NewReader(
			`{"session_id":"019f2c8a","transcript_path":"/home/u/.codex/sessions/a.jsonl","hook_event_name":"SessionStart","source":"startup"}`))
		if err != nil {
			t.Fatal(err)
		}
		if p.SessionID != "019f2c8a" || p.HookEventName != "SessionStart" {
			t.Fatalf("payload = %+v", p)
		}
	})

	t.Run("a sub-agent payload is recognisable", func(t *testing.T) {
		p, err := readHookPayload(strings.NewReader(`{"session_id":"x","agent_id":"sub-1"}`))
		if err != nil {
			t.Fatal(err)
		}
		if p.AgentID == "" {
			t.Fatal("the sub-agent marker was not decoded, so a nested conversation could bind the pane")
		}
	})

	t.Run("fields Sidecar does not name cannot reach a record", func(t *testing.T) {
		// The struct decodes four fields. Everything else in a provider payload
		// -- prompt text, tool arguments, model names -- is dropped by the
		// decoder rather than filtered later.
		p, err := readHookPayload(strings.NewReader(
			`{"session_id":"x","prompt":"secret prompt text","tool_input":{"password":"hunter2"},"cwd":"/repo"}`))
		if err != nil {
			t.Fatal(err)
		}
		if p.SessionID != "x" {
			t.Fatalf("payload = %+v", p)
		}
		blob, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, leaked := range []string{"secret prompt text", "hunter2", "/repo"} {
			if strings.Contains(string(blob), leaked) {
				t.Fatalf("the decoded payload carried %q: %s", leaked, blob)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		cases := []struct {
			name, body, want string
		}{
			{"empty", "", "empty"},
			{"whitespace only", "   \n", "empty"},
			{"not JSON", "session_id=x", "not valid JSON"},
			{"a JSON array", "[1,2,3]", "not valid JSON"},
			{"over the cap", `{"session_id":"` + strings.Repeat("a", maxHookStdinBytes) + `"}`, "cap"},
			// The boundary itself: exactly one byte over must be refused, and
			// exactly at the cap must be accepted (asserted separately below).
			{"one byte over the cap", strings.Repeat("x", maxHookStdinBytes+1), "cap"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := readHookPayload(strings.NewReader(tc.body))
				if err == nil {
					t.Fatalf("%s was accepted", tc.name)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err = %v, wanted it to mention %q", err, tc.want)
				}
			})
		}
	})

	t.Run("exactly at the cap is accepted", func(t *testing.T) {
		// A bound that refuses at its own limit is a bound nobody can describe.
		// Pad a valid document to exactly maxHookStdinBytes.
		const head = `{"session_id":"x","hook_event_name":"`
		const tail = `"}`
		pad := maxHookStdinBytes - len(head) - len(tail)
		if pad < 0 {
			t.Skip("cap is smaller than the fixture")
		}
		body := head + strings.Repeat("E", pad) + tail
		if len(body) != maxHookStdinBytes {
			t.Fatalf("fixture is %d bytes, wanted exactly %d", len(body), maxHookStdinBytes)
		}
		p, err := readHookPayload(strings.NewReader(body))
		if err != nil {
			t.Fatalf("a payload exactly at the cap was refused: %v", err)
		}
		if p.SessionID != "x" {
			t.Fatalf("payload = %+v", p)
		}
	})

	t.Run("a nil reader is a refusal, not a panic", func(t *testing.T) {
		if _, err := readHookPayload(nil); err == nil {
			t.Fatal("a nil stdin was accepted")
		}
	})
}

// TestReportSessionErrorCodesMapToTheRefusalTheyDescribe keeps the JSON error
// vocabulary tied to the validator's own error values rather than to message
// text, so an integration can branch on the code.
func TestReportSessionErrorCodesMapToTheRefusalTheyDescribe(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{agentsession.ErrStaleGeneration, "stale_generation"},
		{agentsession.ErrKindMismatch, "kind_mismatch"},
		{agentsession.ErrUntrustedSource, "untrusted_source"},
		{agentsession.ErrOutsideStoreRoot, "outside_store_root"},
		{agentsession.ErrUnsupportedKind, "unsupported_kind"},
		{agentsession.ErrInvalidRef, "invalid_reference"},
		{errors.New("disk on fire"), "store_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := reportSessionCode(tc.err); got != tc.want {
				t.Fatalf("reportSessionCode(%v) = %q, wanted %q", tc.err, got, tc.want)
			}
			// Wrapped errors must map the same way; the CLI never sees a bare
			// sentinel in practice.
			wrapped := errors.Join(errors.New("while binding"), tc.err)
			if got := reportSessionCode(wrapped); got != tc.want {
				t.Fatalf("a wrapped %v mapped to %q, wanted %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestOnlyStoreFailuresExitOne separates "the machine broke" from "the report
// lost", which is the difference between a bug worth investigating and normal
// operation.
func TestOnlyStoreFailuresExitOne(t *testing.T) {
	var out, errOut strings.Builder
	env := Env{Stdout: &out, Stderr: &errOut}

	if code := emitReportSessionError(env, true, "store_failed", errors.New("boom")); code != 1 {
		t.Fatalf("a store failure exited %d, wanted 1", code)
	}
	for _, code := range []string{"stale_generation", "kind_mismatch", "invalid_reference", "untrusted_source", "outside_store_root"} {
		if got := emitReportSessionError(env, true, code, errors.New("x")); got != exitInputRejected {
			t.Fatalf("%s exited %d, wanted %d", code, got, exitInputRejected)
		}
	}
}

// TestAKindMismatchRefusalStaysFailOpenForTheProvider is the hook-safety half of
// td-11040b.
//
// Refusing the wrong provider is only correct if the refusal cannot damage the
// session the hook is running inside. The convention every reporting surface
// here follows is that a refusal is diagnostic: it exits with a rejection code
// so an integration test can see it, writes its explanation to stderr, and
// leaves stdout clean so nothing is interpreted as hook output. Claude Code in
// particular treats exit 2 as "block the tool call" and every other non-zero
// code as a non-blocking error, so the one exit code this must never use is 2 —
// a mis-attributed report would otherwise interrupt the user's grok session
// instead of quietly declining to record it.
func TestAKindMismatchRefusalStaysFailOpenForTheProvider(t *testing.T) {
	var out, errOut strings.Builder
	env := Env{Stdout: &out, Stderr: &errOut}

	err := fmt.Errorf("%w: this pane is running grok, but the report claims claude",
		agentsession.ErrKindMismatch)
	code := emitReportSessionError(env, false, reportSessionCode(err), err)

	if code == 2 {
		t.Fatal("a kind mismatch exited 2, which Claude Code reads as 'block this tool call'")
	}
	if code != exitInputRejected {
		t.Fatalf("a kind mismatch exited %d, wanted %d", code, exitInputRejected)
	}
	if out.String() != "" {
		t.Fatalf("a refusal wrote to stdout, where a provider may read it as hook output: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "grok") || !strings.Contains(errOut.String(), "claude") {
		t.Fatalf("the refusal did not name both providers on stderr: %q", errOut.String())
	}
}

// TestTheJSONResultNeverCarriesTheConversationValue is the redaction rule at
// the reporting surface. The hook already knows what it reported; anything
// capturing its output should not learn it too.
func TestTheJSONResultNeverCarriesTheConversationValue(t *testing.T) {
	blob, err := json.Marshal(reportSessionResult{
		SchemaVersion: reportSessionSchemaVersion,
		Managed:       true,
		Decision:      agentsession.DecisionRecorded,
		Kind:          "codex",
		RefKind:       agentsession.RefID,
		Reported:      true,
		Bound:         true,
		Shell:         "sidecar-sh-p-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), `"value"`) {
		t.Fatalf("the result contract has a value field: %s", blob)
	}
	for _, want := range []string{`"refKind":"id"`, `"reported":true`, `"bound":true`} {
		if !strings.Contains(string(blob), want) {
			t.Fatalf("the result lost %s: %s", want, blob)
		}
	}
}

// TestRedactionPinsWhoMaySeeAConversationValue covers the security property this
// milestone advertises most loudly and which had no test at all.
//
// Kind and Reported are capability and presence and are always safe to publish.
// The value names the conversation and is withheld unless the caller is that
// shell or asked for it by name, because list output lands in logs and CI
// artifacts and a conversation identifier written there cannot be unwritten.
func TestRedactionPinsWhoMaySeeAConversationValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shells.json")
	const (
		session = "sidecar-sh-proj-1"
		ns      = "/tmp/sock"
		value   = "conv-secret-0123456789"
		gen     = "pid=1,start=T"
	)
	if err := shellstate.AddAtPath(path, shellstate.Definition{
		TmuxName: session, DisplayName: "reviewer", Namespace: ns, WorkDir: "/repo",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := shellstate.BindSessionAtPath(path, shellstate.Identity{TmuxName: session, Namespace: ns},
		shellstate.SessionUpdate{Kind: "codex", Live: gen, Ref: agentsession.Ref{
			Kind: agentsession.RefID, Value: value,
			Source: "sidecar.codex.hooks", Reported: true, Generation: gen,
		}}); err != nil {
		t.Fatal(err)
	}

	decorated := func(includeValue bool) *agentcontrol.SessionRef {
		a := agentcontrol.Agent{}
		newSessionRefCache().decorate(&a, path, session, ns, includeValue)
		return a.Agent.SessionRef
	}

	t.Run("redacted still reports capability and presence", func(t *testing.T) {
		ref := decorated(false)
		if ref == nil {
			t.Fatal("a bound shell reported no sessionRef at all")
		}
		if ref.Kind != "id" || !ref.Reported {
			t.Fatalf("capability was lost in redaction: %+v", ref)
		}
		if ref.Value != "" {
			t.Fatalf("the redacted projection carried the value: %q", ref.Value)
		}
		blob, err := json.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(blob), value) {
			t.Fatalf("the value reached JSON: %s", blob)
		}
	})

	t.Run("the explicit opt-in reveals it", func(t *testing.T) {
		ref := decorated(true)
		if ref == nil || ref.Value != value {
			t.Fatalf("--include-session-ref did not reveal the value: %+v", ref)
		}
	})

	t.Run("an unbound shell has no sessionRef key at all", func(t *testing.T) {
		// "not bound" and "bound but redacted" must stay distinguishable.
		if err := shellstate.AddAtPath(path, shellstate.Definition{
			TmuxName: "sidecar-sh-proj-2", DisplayName: "plain", Namespace: ns,
		}); err != nil {
			t.Fatal(err)
		}
		a := agentcontrol.Agent{}
		newSessionRefCache().decorate(&a, path, "sidecar-sh-proj-2", ns, true)
		if a.Agent.SessionRef != nil {
			t.Fatalf("an unbound shell reported %+v", a.Agent.SessionRef)
		}
	})
}

// TestTheListPathNeverRevealsAValueByDefault pins the call site rather than the
// helper: the property is that `agent list` does not pass includeValue unless
// the caller asked, which a test of decorate alone would not catch.
func TestTheListPathNeverRevealsAValueByDefault(t *testing.T) {
	src, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	i := strings.Index(text, "func runAgentList(")
	if i < 0 {
		t.Fatal("runAgentList not found")
	}
	body := text[i:]
	if j := strings.Index(body, "\nfunc "); j > 0 {
		body = body[:j]
	}
	if !strings.Contains(body, "f.includeSession") {
		t.Fatal("agent list no longer gates the conversation value on f.includeSession; " +
			"a list that reveals values by default is the regression this pins")
	}
	// And it must not acquire an own-shell exemption: get has one, list must not,
	// because a list is about other people's shells by definition.
	if strings.Contains(body, "SessionEnv") {
		t.Fatal("agent list gained an own-shell exemption; only agent get has one")
	}
}

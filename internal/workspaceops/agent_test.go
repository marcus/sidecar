package workspaceops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentcatalog"
)

type fakeTmuxRunner struct {
	calls             [][]string
	sessionExists     bool
	failSendSubstring string
}

func (r *fakeTmuxRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) > 0 && args[0] == "has-session" {
		if r.sessionExists {
			return nil, nil
		}
		return nil, errors.New("missing")
	}
	if len(args) > 0 && args[0] == "new-session" {
		r.sessionExists = true
	}
	if len(args) > 0 && args[0] == "kill-session" {
		r.sessionExists = false
	}
	if len(args) > 3 && args[0] == "send-keys" && r.failSendSubstring != "" && strings.Contains(args[3], r.failSendSubstring) {
		return []byte("send failed"), errors.New("send failed")
	}
	if len(args) > 0 && args[0] == "list-panes" {
		return []byte("%7\n"), nil
	}
	return nil, nil
}

func TestLaunchWorktreeSessionCleansUpFailedNewSessionBeforeRetry(t *testing.T) {
	for _, failCommand := range []string{"TD_SESSION_ID", "CUSTOM_FLAG", "td start"} {
		t.Run(failCommand, func(t *testing.T) {
			runner := &fakeTmuxRunner{failSendSubstring: failCommand}
			spec := AgentLaunchSpec{
				SessionName: "sidecar-ws-topic", WorkDir: "/tmp/topic", AgentCommand: "codex-custom", TaskID: "td-123",
				Env: map[string]string{"CUSTOM_FLAG": "from-file"}, StartAgent: true,
			}
			if _, err := LaunchWorktreeSessionWithRunner(context.Background(), spec, runner); err == nil {
				t.Fatalf("%s setup failure unexpectedly succeeded", failCommand)
			}
			if runner.sessionExists {
				t.Fatal("failed launch left the newly created session behind")
			}
			if got := strings.Join(flattenCalls(runner.calls), "\n"); !strings.Contains(got, "kill-session -t sidecar-ws-topic") {
				t.Fatalf("failed launch did not clean up session:\n%s", got)
			}

			runner.failSendSubstring = ""
			result, err := LaunchWorktreeSessionWithRunner(context.Background(), spec, runner)
			if err != nil {
				t.Fatalf("retry launch: %v", err)
			}
			if result.Reconnected {
				t.Fatal("retry falsely reconnected to the failed launch session")
			}
		})
	}
}

func flattenCalls(calls [][]string) []string {
	flat := make([]string, 0, len(calls))
	for _, call := range calls {
		flat = append(flat, strings.Join(call, " "))
	}
	return flat
}

func TestLaunchWorktreeSessionRunsEnvironmentTaskAndAgent(t *testing.T) {
	runner := &fakeTmuxRunner{}
	result, err := LaunchWorktreeSessionWithRunner(context.Background(), AgentLaunchSpec{
		SessionName: "sidecar-ws-topic", WorkDir: "/tmp/topic", AgentCommand: "codex-custom", TaskID: "td-123",
		Env: map[string]string{"CUSTOM_FLAG": "from-file"}, StartAgent: true,
	}, runner)
	if err != nil || result.PaneID != "%7" {
		t.Fatalf("launch result=%+v err=%v", result, err)
	}
	joined := ""
	for _, call := range runner.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	for _, want := range []string{"new-session -d -s sidecar-ws-topic -c /tmp/topic", "CUSTOM_FLAG", "td start", "codex-custom"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("launch calls missing %q:\n%s", want, joined)
		}
	}
}

func TestTypeInShellOmitsEnter(t *testing.T) {
	runner := &fakeTmuxRunner{}
	if err := TypeInShellWithRunner(context.Background(), "sidecar-sh-demo-1", "go test ./...", runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	got := strings.Join(runner.calls[0], " ")
	if got != "send-keys -t sidecar-sh-demo-1 go test ./..." {
		t.Fatalf("type keys = %q", got)
	}
	if strings.HasSuffix(got, "Enter") {
		t.Fatal("type must not press Enter")
	}

	runner = &fakeTmuxRunner{}
	if err := StartAgentInShellWithRunner(context.Background(), "sidecar-sh-demo-1", "python3 -m http.server", runner); err != nil {
		t.Fatal(err)
	}
	got = strings.Join(runner.calls[0], " ")
	if got != "send-keys -t sidecar-sh-demo-1 python3 -m http.server Enter" {
		t.Fatalf("start keys = %q", got)
	}
}

func TestResolveAgentCommandUsesConfiguredOverrideAndSkipFlag(t *testing.T) {
	got := ResolveAgentCommand(t.TempDir(), "codex", map[string]string{"codex": "codex-custom"}, true)
	if got != "codex-custom --dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("resolved command = %q", got)
	}
}

func TestResolveAgentLaunchArgvKeepsCatalogStructuredAndOverridesOpaque(t *testing.T) {
	argv, opaque, err := ResolveAgentLaunchArgv(t.TempDir(), "codex", nil, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "--dangerously-bypass-approvals-and-sandbox"}
	if opaque || !reflect.DeepEqual(argv, want) {
		t.Fatalf("catalog launch = %#v opaque=%v, want %#v false", argv, opaque, want)
	}

	argv, opaque, err = ResolveAgentLaunchArgv(t.TempDir(), "codex", map[string]string{"codex": "codex-custom --profile fast"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"sh", "-lc", "codex-custom --profile fast --dangerously-bypass-approvals-and-sandbox"}
	if !opaque || !reflect.DeepEqual(argv, want) {
		t.Fatalf("override launch = %#v opaque=%v, want %#v true", argv, opaque, want)
	}

	if _, _, err := ResolveAgentLaunchArgv(t.TempDir(), "not-real", nil, false, nil); err == nil {
		t.Fatal("unknown provider received a launch")
	}
}

// Provider arguments follow the family's command on a catalog launch and are
// appended one quoted shell word each to an opaque override, so a configured
// command still runs and a value with a space stays one argument.
func TestResolveAgentLaunchArgvAppendsProviderArguments(t *testing.T) {
	argv, opaque, err := ResolveAgentLaunchArgv(t.TempDir(), "codex", nil, false, []string{"--model", "space value"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "--model", "space value"}
	if opaque || !reflect.DeepEqual(argv, want) {
		t.Fatalf("catalog launch = %#v opaque=%v, want %#v", argv, opaque, want)
	}
	argv, opaque, err = ResolveAgentLaunchArgv(t.TempDir(), "codex", map[string]string{"codex": "codex-custom --profile fast"}, true, []string{"--model", "space value"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"sh", "-lc", "codex-custom --profile fast --dangerously-bypass-approvals-and-sandbox '--model' 'space value'"}
	if !opaque || !reflect.DeepEqual(argv, want) {
		t.Fatalf("override launch = %#v opaque=%v, want %#v", argv, opaque, want)
	}
	if _, _, err := ResolveAgentLaunchArgv(t.TempDir(), "codex", map[string]string{"codex": "codex-custom"}, false, []string{"bad\x00"}); err == nil {
		t.Fatal("a NUL in a provider argument reached the shell")
	}
	// Appended to a pipeline the arguments would reach tee, not the provider.
	_, _, err = ResolveAgentLaunchArgv(t.TempDir(), "codex", map[string]string{"codex": "codex-custom 2>&1 | tee /tmp/agent.log"}, false, []string{"--model", "fable"})
	if err == nil || !strings.Contains(err.Error(), "shell syntax") || !strings.Contains(err.Error(), "tee") {
		t.Fatalf("pipeline override = %v, want a refusal naming the command", err)
	}
}

func TestAgentSkipFlag(t *testing.T) {
	if got := AgentSkipFlag("codex"); got != "--dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("codex skip flag = %q", got)
	}
	if got := AgentSkipFlag("claude"); got != "--dangerously-skip-permissions" {
		t.Fatalf("claude skip flag = %q", got)
	}
	if got := AgentSkipFlag("opencode"); got != "--auto" {
		t.Fatalf("opencode skip flag = %q, want --auto", got)
	}
	if got := AgentSkipFlag("aider"); got != "--yes" {
		t.Fatalf("legacy aider skip flag = %q, want --yes", got)
	}
	if got := AgentSkipFlag("copilot"); got != "" {
		t.Fatalf("copilot skip flag = %q, want empty", got)
	}
	if got := AgentSkipFlag(""); got != "" {
		t.Fatalf("empty agent skip flag = %q, want empty", got)
	}
	if got := AgentSkipFlag("nonesuch"); got != "" {
		t.Fatalf("unknown agent skip flag = %q, want empty", got)
	}
}

func TestResolveAgentCommandOpenCode(t *testing.T) {
	t.Run("default without skip flag", func(t *testing.T) {
		if got := ResolveAgentCommand(t.TempDir(), "opencode", nil, false); got != "opencode" {
			t.Fatalf("resolved command = %q, want opencode", got)
		}
	})
	t.Run("default appends auto when skipping permissions", func(t *testing.T) {
		if got := ResolveAgentCommand(t.TempDir(), "opencode", nil, true); got != "opencode --auto" {
			t.Fatalf("resolved command = %q, want %q", got, "opencode --auto")
		}
	})
	t.Run("configured override with profile appends after existing flags", func(t *testing.T) {
		got := ResolveAgentCommand(t.TempDir(), "opencode", map[string]string{"opencode": "opencode --profile fast"}, true)
		if got != "opencode --profile fast --auto" {
			t.Fatalf("resolved command = %q", got)
		}
	})
	t.Run("run prefix is stripped before appending skip flag", func(t *testing.T) {
		got := ResolveAgentCommand(t.TempDir(), "opencode", map[string]string{"opencode": "opencode run --model anthropic/claude-sonnet-4"}, true)
		if got != "opencode --model anthropic/claude-sonnet-4 --auto" {
			t.Fatalf("resolved command = %q", got)
		}
	})
	t.Run("sidecar-agent-start override composes with skip flag", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".sidecar-agent-start"), []byte("opencode run\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := ResolveAgentCommand(dir, "opencode", map[string]string{"*": "ignored"}, true); got != "opencode --auto" {
			t.Fatalf("resolved command = %q", got)
		}
	})
}

// A family whose bare command is not its agent carries the subcommand that is,
// and every resolution has to keep it. ResolveAgentLaunchArgv builds argv from
// the catalog and gets LaunchArgs for free; the string form is a second path,
// and it is the one the workspace plugin and every remote create-shell use.
//
// Kiro is why this exists and is deliberately not named here: `kiro-cli` alone
// prints help and exits, and `kiro-cli --trust-all-tools` is rejected as an
// unexpected argument, so dropping `chat` turns a launch into a pane that
// closes. Reading the rule off the catalog means the next such family is
// covered on the day its file lands.
func TestStringLaunchKeepsTheSubcommandThatStartsTheAgent(t *testing.T) {
	found := 0
	for _, family := range agentcatalog.Families() {
		if len(family.LaunchArgs) == 0 {
			continue
		}
		found++
		want := family.Command + " " + strings.Join(family.LaunchArgs, " ")
		if got := ResolveAgentCommandFromConfig(family.ID, nil, false); got != want {
			t.Errorf("ResolveAgentCommandFromConfig(%q) = %q, want %q", family.ID, got, want)
		}
		wantSkip := want
		if family.SkipPermissionsArg != "" {
			wantSkip = want + " " + family.SkipPermissionsArg
		}
		if got := ResolveAgentCommandFromConfig(family.ID, nil, true); got != wantSkip {
			t.Errorf("ResolveAgentCommandFromConfig(%q, skip) = %q, want %q", family.ID, got, wantSkip)
		}
	}
	if found == 0 {
		t.Skip("no catalog family states launch_args")
	}
}

// A configured launch command replaces the catalog's, subcommand included: the
// user wrote the whole line and Sidecar must not splice a subcommand into it.
func TestAConfiguredCommandIsNotGivenTheCatalogSubcommand(t *testing.T) {
	for _, family := range agentcatalog.Families() {
		if len(family.LaunchArgs) == 0 {
			continue
		}
		configured := map[string]string{family.ID: "my-wrapper"}
		if got := ResolveAgentCommandFromConfig(family.ID, configured, false); got != "my-wrapper" {
			t.Errorf("ResolveAgentCommandFromConfig(%q, configured) = %q, want %q", family.ID, got, "my-wrapper")
		}
	}
}

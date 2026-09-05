package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspaceops"
)

func TestCreateShellValidation(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		code     int
		contains string
	}{
		{"unknown option", []string{"create", "shell", "--bogus"}, 2, "unknown option"},
		{"positional", []string{"create", "shell", "extra"}, 2, "takes no positional"},
		{"run and type", []string{"create", "shell", "--run", "true", "--type", "false"}, 2, "mutually exclusive"},
		{"split invalid", []string{"create", "shell", "--split", "diagonal"}, 2, "invalid split option"},
		{"skip perms without agent", []string{"create", "shell", "--skip-permissions"}, 2, "--skip-permissions requires --agent"},
		// Blank names no agent, so it must reach the same refusal. Judged
		// untrimmed, it would pass this guard and then record a shell with no
		// agent type and skipPerms set — durable state, replayed on every start.
		{"skip perms with a blank agent", []string{"create", "shell", "--agent", "  ", "--skip-permissions"}, 2, "--skip-permissions requires --agent"},
		{"unknown create kind", []string{"create", "wat"}, 2, "unknown create command"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := Run(tt.args, &out, &errOut)
			if !handled || code != tt.code {
				t.Fatalf("Run(%v) = handled %v, code %d; want true, %d (%s)", tt.args, handled, code, tt.code, errOut.String())
			}
			combined := out.String() + errOut.String()
			if !strings.Contains(combined, tt.contains) {
				t.Fatalf("output for %v missing %q; got %q", tt.args, tt.contains, combined)
			}
		})
	}
}

func TestCreateShellUnknownProjectDoesNotInitState(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	orphan := t.TempDir()
	t.Chdir(orphan)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--wait", "0"}, &out, &errOut)
	if !handled || code != 2 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "registered") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unknown project initialized state: %v", entries)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"create", "shell", "--project", "nosuch", "--wait", "0"}, &out, &errOut)
	// A --project nobody can resolve is a rejected value (5), not a command
	// that could not be parsed (2). Across a host boundary the second reads as
	// version skew and sends the user off to update a binary.
	if !handled || code != exitInputRejected {
		t.Fatalf("--project unknown = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "unknown project") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	entries, err = os.ReadDir(filepath.Join(stateDir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--project unknown initialized state: %v", entries)
	}
}

func TestCreateShellJSONNoAckNonFatal(t *testing.T) {
	stateHome, stateDir := setupIsolatedCLI(t)
	workDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	t.Chdir(workDir)
	writeProjectMeta(t, stateDir, "demo", workDir)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--name", "dev server", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}

	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if result.Acked {
		t.Fatal("expected acked=false with no instance")
	}
	if result.Placement != createPlacementWorkspace {
		t.Fatalf("placement = %q", result.Placement)
	}
	if result.Shell.DisplayName != "dev server" || result.Shell.WorkDir != workDir || result.Shell.Session == "" {
		t.Fatalf("shell = %+v", result.Shell)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", result.Shell.Session).Run()
	})

	listed, err := shellstate.ListAtPath(filepath.Join(stateDir, "projects", "demo", "shells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].DisplayName != "dev server" || listed[0].TmuxName != result.Shell.Session {
		t.Fatalf("manifest = %+v", listed)
	}

	reqs, err := os.ReadDir(filepath.Join(stateHome, "sidecar", "requests"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range reqs {
		if !strings.Contains(entry.Name(), string(uirequest.ActionCreate)) {
			continue
		}
		found = true
	}
	if !found {
		t.Fatalf("no create request in %v", reqs)
	}
}

func TestCreateShellExplicitCWDKeepsProjectOwnershipAndPersistsEffectiveDirectory(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	projectRoot := t.TempDir()
	destination := filepath.Join(t.TempDir(), "agent work")
	if err := os.MkdirAll(destination, 0755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(destination); err == nil {
		destination = resolved
	}
	writeProjectMeta(t, stateDir, "demo", projectRoot)
	t.Chdir(projectRoot)
	tmuxDir, err := os.MkdirTemp("/tmp", "sidecar-cwd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmuxDir) })
	t.Setenv("TMUX_TMPDIR", tmuxDir)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--project", "demo", "--cwd", destination, "--agent", "codex", "--run", "pwd", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("create = handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Project != "demo" || result.Shell.WorkDir != destination || result.Placement != createPlacementWorkspace {
		t.Fatalf("result = %+v, want demo owned shell in %q", result, destination)
	}
	t.Cleanup(func() {
		cmd := exec.Command("tmux", "kill-server")
		cmd.Env = append(scrubTmuxIdentity(os.Environ()), "TMUX_TMPDIR="+tmuxDir)
		_ = cmd.Run()
	})

	cmd := exec.Command("tmux", "display-message", "-p", "-t", result.Shell.Session, "#{pane_current_path}")
	cmd.Env = append(scrubTmuxIdentity(os.Environ()), "TMUX_TMPDIR="+tmuxDir)
	current, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(current)); got != destination {
		t.Fatalf("pane cwd = %q, want %q", got, destination)
	}
	listed, err := shellstate.ListAtPath(filepath.Join(stateDir, "projects", "demo", "shells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].WorkDir != destination || listed[0].AgentType != "codex" {
		t.Fatalf("manifest = %+v", listed)
	}
}

func TestCreateShellCWDValidationHappensBeforeMutation(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	projectRoot := t.TempDir()
	writeProjectMeta(t, stateDir, "demo", projectRoot)
	t.Chdir(projectRoot)
	file := filepath.Join(projectRoot, "file")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, value := range []string{filepath.Join(t.TempDir(), "missing"), file, "~somebody/work"} {
		var out, errOut bytes.Buffer
		handled, code := Run([]string{"create", "shell", "--project", "demo", "--cwd", value, "--json", "--wait", "0"}, &out, &errOut)
		if !handled || code != 5 || out.Len() != 0 || !strings.Contains(errOut.String(), `"code":"agent_not_ready"`) {
			t.Fatalf("--cwd %q = handled %v code %d stdout %q stderr %q", value, handled, code, out.String(), errOut.String())
		}
	}
	listed, err := shellstate.ListAtPath(filepath.Join(stateDir, "projects", "demo", "shells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("invalid cwd created records: %+v", listed)
	}

	// Resolving a configured-only project registers it on first use. Invalid
	// --cwd must be rejected before even that project-state mutation.
	_, isolatedState := setupIsolatedCLI(t)
	repo, cfgPath := configuredOnlyProject(t)
	var out, errOut bytes.Buffer
	handled, code := Run([]string{"-config", cfgPath, "create", "shell", "--project", repo, "--cwd", filepath.Join(t.TempDir(), "missing"), "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 5 {
		t.Fatalf("configured-only invalid cwd = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	entries, err := os.ReadDir(filepath.Join(isolatedState, "projects"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid cwd registered configured project: %v %v", entries, err)
	}
}

func TestResolveCreateShellCWDRelativeAndHomePaths(t *testing.T) {
	base := t.TempDir()
	relative := filepath.Join(base, "relative")
	spaced := filepath.Join(base, " spaced ")
	home := t.TempDir()
	homeDir := filepath.Join(home, "home-dir")
	for _, dir := range []string{relative, spaced, homeDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(base)
	t.Setenv("HOME", home)
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"relative", relative},
		{" spaced ", spaced},
		{"~/home-dir", homeDir},
		{"~", home},
	} {
		got, err := resolveCreateShellCWD(tc.value)
		if err != nil {
			t.Fatalf("resolve %q: %v", tc.value, err)
		}
		want, _ := filepath.EvalSymlinks(tc.want)
		if got != want {
			t.Fatalf("resolve %q = %q, want %q", tc.value, got, want)
		}
	}
}

func scrubTmuxIdentity(env []string) []string {
	out := env[:0]
	for _, item := range env {
		if strings.HasPrefix(item, "TMUX=") || strings.HasPrefix(item, "TMUX_PANE=") || strings.HasPrefix(item, "SIDECAR_") {
			continue
		}
		out = append(out, item)
	}
	return out
}

func TestCreateShellSplitRequiresCurrentShell(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	workDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	t.Chdir(workDir)
	writeProjectMeta(t, stateDir, "demo", workDir)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--split", "right", "--wait", "0"}, &out, &errOut)
	if !handled || code != 2 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "managed shell") && !strings.Contains(errOut.String(), "--shell") {
		t.Fatalf("stderr = %q, want a current-shell hint", errOut.String())
	}
	if strings.Contains(errOut.String(), "terminal-splits Phase A") {
		t.Fatalf("still the Phase A refusal: %q", errOut.String())
	}
}

func TestCreateShellExplicitCWDRefusesSplitBeforeRequest(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "active task")
	destination := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(destination); err == nil {
		destination = resolved
	}
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--split", "right", "--cwd", destination, "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), `"code":"usage"`) || !strings.Contains(errOut.String(), "managed workspace shell") {
		t.Fatalf("split = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	requestDir := filepath.Join(stateHome, "sidecar", "requests")
	if entries, err := os.ReadDir(requestDir); err == nil && len(entries) != 0 {
		t.Fatalf("refused split wrote requests: %v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestCreateShellSplitNoInstanceExit3(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "active task")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--split", "right", "--json", "--wait", "100ms"}, &out, &errOut)
	if !handled || code != 3 {
		t.Fatalf("Run() = handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	if !strings.Contains(errOut.String(), "sidecar-sh-sidecar-1") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestCreateShellSplitDeclinedExit4(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "active task")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")
	reason := "Two live terminals at a time; close one first"
	done := ackCreateRequests(t, stateHome, uirequest.Ack{
		Instance: "test-instance", Host: "localhost", PID: 1,
		Status: uirequest.StatusDeclined, Reason: reason, At: time.Now().UTC(),
	})

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--split=right", "--wait", ackWaitFlag}, &out, &errOut)
	<-done
	if !handled || code != 4 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), reason) {
		t.Fatalf("stderr = %q, want host reason", errOut.String())
	}
}

func TestCreateShellSplitOpenedJSON(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "active task")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")
	done := ackCreateRequests(t, stateHome, uirequest.Ack{
		Instance: "test-instance", Host: "localhost", PID: 1,
		Status: uirequest.StatusOpened, Surface: "shell:sidecar-tp-sidecar-sh-sidecar-1",
		At: time.Now().UTC(),
	})

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--split", "right", "--name", "dev server", "--json", "--wait", ackWaitFlag}, &out, &errOut)
	<-done
	if !handled || code != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if !result.Acked || result.Placement != "right" || result.Surface != "shell:sidecar-tp-sidecar-sh-sidecar-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Shell.Session != "sidecar-tp-sidecar-sh-sidecar-1" || result.Shell.DisplayName != "dev server" {
		t.Fatalf("shell = %+v", result.Shell)
	}
}

func TestCreateShellSplitWait0WritesOptions(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "active task")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--split=below", "--run", "echo hi", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if result.Acked || result.Placement != "below" || result.Shell.Session != "" {
		t.Fatalf("result = %+v", result)
	}

	req := readLatestCreateRequest(t, stateHome)
	if req.Options.Split != "below" {
		t.Fatalf("Options.Split = %q", req.Options.Split)
	}
	payload, err := uirequest.DecodeCreatePayload(req.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Kind != uirequest.CreateKindShell || payload.Run != "echo hi" || payload.Session != "" {
		t.Fatalf("payload = %+v", payload)
	}
}

// ackWatchBudget and ackWaitFlag are deliberately far larger than the work
// they bound. The tests they serve assert what happens when an ack *arrives*,
// so the only thing a tight budget can produce is a false failure on a busy
// machine. Neither costs wall-clock on the happy path.
const (
	ackWatchBudget = 60 * time.Second
	ackWaitFlag    = "30s"
)

// ackCreateRequests plays the running Sidecar instance: it watches the request
// directory and acks the first create request it sees.
//
// It polls to a wall-clock deadline rather than a fixed iteration count, and
// the tests that use it pass a --wait far longer than they need. Both halves
// matter. The helper used to poll 40 times at 25ms — about a second — against a
// CLI given --wait 1s, so the ack had to be found and written inside the same
// second the CLI was willing to wait. Under a loaded `go test ./...` those
// sleeps stretch, the budget ran out before the request was seen, and the CLI
// timed out into exit 3 ("no running Sidecar instance is showing this shell").
// That is td-9d3b09, and it was reproducible only in the full parallel suite.
//
// Nothing here is slower as a result: --wait is a ceiling, not a sleep, so the
// happy path still returns the moment the ack lands.
func ackCreateRequests(t *testing.T, stateHome string, ack uirequest.Ack) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		reqsDir := filepath.Join(stateHome, "sidecar", "requests")
		deadline := time.Now().Add(ackWatchBudget)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
			entries, err := os.ReadDir(reqsDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), ".tmp.") {
					continue
				}
				req, err := uirequest.ReadRequest(filepath.Join(reqsDir, e.Name()))
				if err != nil || req.Action != uirequest.ActionCreate {
					continue
				}
				_ = uirequest.WriteAck(filepath.Join(stateHome, "sidecar"), req.ID, req.Action, ack)
				return
			}
		}
	}()
	return done
}

func readLatestCreateRequest(t *testing.T, stateHome string) uirequest.Request {
	t.Helper()
	dir := filepath.Join(stateHome, "sidecar", "requests")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), ".tmp.") {
			continue
		}
		if !strings.Contains(e.Name(), string(uirequest.ActionCreate)) {
			continue
		}
		req, err := uirequest.ReadRequest(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	t.Fatalf("no create request in %v", entries)
	return uirequest.Request{}
}

func TestCreateListedForAgents(t *testing.T) {
	rendered := RenderAgents(RootCommand())
	if !strings.Contains(rendered, "sidecar create shell") {
		t.Fatalf("sidecar --agents does not list create shell:\n%s", rendered)
	}
	if !strings.Contains(rendered, "sidecar create worktree") {
		t.Fatalf("sidecar --agents does not list create worktree:\n%s", rendered)
	}
	if !strings.Contains(rendered, "sidecar shell list --json") {
		t.Fatalf("sidecar --agents does not list shell list:\n%s", rendered)
	}
	if !strings.Contains(rendered, "sidecar shell forget") {
		t.Fatalf("sidecar --agents does not list shell forget:\n%s", rendered)
	}
	if !strings.Contains(rendered, "sidecar shell restore") {
		t.Fatalf("sidecar --agents does not list shell restore:\n%s", rendered)
	}
}

func TestCreateWorktreeValidation(t *testing.T) {
	setupIsolatedCLI(t)
	for _, tt := range []struct {
		name     string
		args     []string
		code     int
		contains string
	}{
		{"missing name", []string{"create", "worktree"}, 2, "exactly one name"},
		{"no-launch with agent", []string{"create", "worktree", "--no-launch", "--agent", "claude", "x"}, 2, "--no-launch cannot be combined"},
		{"no-launch with run", []string{"create", "worktree", "--run", "true", "--no-launch", "x"}, 2, "--no-launch cannot be combined"},
		{"unknown option", []string{"create", "worktree", "--bogus", "x"}, 2, "unknown option"},
		{"split unsupported", []string{"create", "worktree", "--split", "right", "x"}, 2, "not supported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := Run(tt.args, &out, &errOut)
			if !handled || code != tt.code {
				t.Fatalf("Run(%v) = handled %v, code %d; want true, %d (%s)", tt.args, handled, code, tt.code, errOut.String())
			}
			if !strings.Contains(errOut.String()+out.String(), tt.contains) {
				t.Fatalf("output missing %q; got %q", tt.contains, errOut.String()+out.String())
			}
		})
	}
}

func TestCreateShellFromSiblingWorktree(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	repo := filepath.Join(root, "repo")
	topic := filepath.Join(root, "repo-topic")
	initGitRepo(t, repo)
	runGit(t, repo, "worktree", "add", "-b", "topic", topic)
	writeProjectMeta(t, stateDir, "demo", repo)
	writeRegisteredWorktree(t, stateDir, repo, topic)
	t.Chdir(topic)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--name", "from-topic", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("from sibling worktree: handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", result.Shell.Session).Run() })
	if result.Shell.Session == "" {
		t.Fatal("missing session")
	}
	// td-e3a93d: a shell created from inside a worktree must land in that
	// worktree, not the main checkout registeredProjectForCreate resolves the
	// project to.
	if result.Shell.WorkDir != topic {
		t.Fatalf("workDir = %q, want the worktree %q", result.Shell.WorkDir, topic)
	}
}

// TestCreateShellTabAgentFromWorktreeUsesWorktreeWorkDir is td-e3a93d Bug 2's
// exact repro: `create shell --tab --agent claude` from inside a worktree
// shell used to land the new workspace shell (and its manifest WorkDir) in
// the main checkout, because registeredProjectForCreate's project is always
// the main checkout's registeredProject.Path — never a worktree's. --tab
// forces the workspace path regardless of the caller's tmux identity, so this
// covers that path the same way TestCreateShellFromSiblingWorktree covers the
// beside-the-session default.
func TestCreateShellTabAgentFromWorktreeUsesWorktreeWorkDir(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	repo := filepath.Join(root, "repo")
	topic := filepath.Join(root, "repo-topic")
	initGitRepo(t, repo)
	runGit(t, repo, "worktree", "add", "-b", "topic", topic)
	writeProjectMeta(t, stateDir, "demo", repo)
	writeRegisteredWorktree(t, stateDir, repo, topic)
	t.Chdir(topic)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--tab", "--agent", "claude", "--name", "from-topic-tab", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("--tab --agent from worktree: handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", result.Shell.Session).Run() })
	if result.Shell.WorkDir != topic {
		t.Fatalf("workDir = %q, want the worktree %q", result.Shell.WorkDir, topic)
	}

	listed, err := shellstate.ListAtPath(filepath.Join(stateDir, "projects", "demo", "shells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].WorkDir != topic {
		t.Fatalf("manifest = %+v, want one record with workDir %q", listed, topic)
	}
}

func TestCreateShellFromSidecarWSIdentity(t *testing.T) {
	stateHome, stateDir := setupIsolatedCLI(t)
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	repo := filepath.Join(root, "repo")
	topic := filepath.Join(root, "repo-topic")
	initGitRepo(t, repo)
	runGit(t, repo, "worktree", "add", "-b", "topic", topic)
	writeProjectMeta(t, stateDir, "demo", repo)
	writeRegisteredWorktree(t, stateDir, repo, topic)
	t.Chdir(topic)

	socket := filepath.Join(t.TempDir(), "tmux.sock")
	binDir := t.TempDir()
	tmux := filepath.Join(binDir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\t%s\\t%s\\n' sidecar-ws-repo-topic " + shellQuote(socket) + " " + shellQuote(topic) + "\n"
	if err := os.WriteFile(tmux, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--name", "from-ws", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("from sidecar-ws identity: handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", result.Shell.Session).Run() })
}

func TestCreateShellLookupOriginKindState(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "stale")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")
	projectDir := filepath.Join(stateHome, "sidecar", "projects", "sidecar")
	if err := os.Remove(filepath.Join(projectDir, "shells.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectDir, "shells.json"), 0755); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--wait", "0"}, &out, &errOut)
	if !handled || code != 1 {
		t.Fatalf("KindState = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "read registered shell manifest") {
		t.Fatalf("stderr = %q, want original KindState message", errOut.String())
	}
}

func TestCreateWorktreeUnknownProjectDoesNotInitState(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "worktree", "--wait", "0", "topic"}, &out, &errOut)
	if !handled || code != 2 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unknown project initialized state: %v", entries)
	}
}

func TestCreateWorktreeNoLaunchHonorsHook(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(repo, ".worktree-setup.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/bash\necho hooked > HOOK_RAN\n"), 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
	t.Chdir(repo)
	writeProjectMeta(t, stateDir, "demo", repo)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"-config", cfgPath, "create", "worktree", "--no-launch", "--json", "--wait", "0", "cli-wt"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	var result createWorktreeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if result.Acked {
		t.Fatal("expected acked=false")
	}
	if result.Branch == "" || result.Path == "" || result.Shell.Session == "" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.HasPrefix(result.Shell.Session, "sidecar-ws-") {
		t.Fatalf("session = %q", result.Shell.Session)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "HOOK_RAN")); err != nil {
		t.Fatalf("hook did not run in %s: %v", result.Path, err)
	}
	if _, err := os.Stat(filepath.Join(result.Path, ".env")); err != nil {
		t.Fatalf("env file was not copied: %v", err)
	}
}

func TestCreateWorktreeRequiredHookDoesNotLaunch(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{
  "plugins": {"workspace": {"worktreeSetup": {"runHook": true, "hookPath": ".worktree-setup.sh", "hookRequired": true}}}
}`), 0644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	initGitRepo(t, repo)
	if err := os.WriteFile(filepath.Join(repo, ".worktree-setup.sh"), []byte("#!/bin/bash\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	writeProjectMeta(t, stateDir, "demo", repo)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"-config", cfgPath, "create", "worktree", "--json", "--wait", "0", "boom"}, &out, &errOut)
	if !handled || code != 1 {
		t.Fatalf("required hook: handled %v code %d stderr %q stdout %q", handled, code, errOut.String(), out.String())
	}
	var result createWorktreeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if workspaceops.SessionExists(result.Shell.Session) {
		t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", result.Shell.Session).Run() })
		t.Fatalf("required hook failure launched %s", result.Shell.Session)
	}
	head, err := exec.Command("git", "-C", result.Path, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	wtKey, err := projectdir.WorktreeKey(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, err := workspaceops.RepoKeyForPath(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := workspaceops.LoadPendingCreation(t.Context(), repo, []workspaceops.WorktreeRecord{{
		Key: wtKey, Path: result.Path, HEADOID: strings.TrimSpace(string(head)),
	}}, repoKey)
	if err != nil {
		t.Fatal(err)
	}
	if journal == nil {
		t.Fatal("CLI journal not visible to LoadPendingCreation with plugin repoKey")
	}
	if journal.RepoKey != repoKey {
		t.Fatalf("journal.RepoKey = %q want %q", journal.RepoKey, repoKey)
	}
}

func initGitRepo(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
}

func writeRegisteredWorktree(t *testing.T, stateDir, projectRoot, worktreePath string) {
	t.Helper()
	if _, err := projectdir.WorktreeDirWithBase(stateDir, projectRoot, worktreePath); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}

// nt-7c82c9: an agent inside a Sidecar shell that asks for a shell gets it
// beside its own session by default; a workspace-wide switch needs --tab.
func TestCreateShellDefaultsToBesideSession(t *testing.T) {
	stateHome, _ := setupShellCLI(t, "host shell")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", "/nonexistent/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%1")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--name", "dev", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(out.String(), `"placement":"auto"`) {
		t.Fatalf("stdout = %q, want the beside-the-session (auto) placement", out.String())
	}

	out, errOut = bytes.Buffer{}, bytes.Buffer{}
	handled, code = Run([]string{"create", "shell", "--tab", "--name", "dev", "--json", "--wait", "0"}, &out, &errOut)
	if !handled {
		t.Fatal("not handled")
	}
	if strings.Contains(out.String(), `"placement":"auto"`) {
		t.Fatalf("stdout = %q, --tab must not take the beside-the-session path", out.String())
	}
	if code == 0 && !strings.Contains(out.String(), `"placement":"workspace"`) {
		t.Fatalf("stdout = %q, want workspace placement for --tab", out.String())
	}
}

// TestCreateShellRecordsTheAgentType is the durable half of "this is a Claude
// shell".
//
// The TUI's Create Shell has always written AgentType as it creates, so
// HasAgent() is true from that moment and the shell keeps its Activity card
// while the agent boots. The CLI had no way to say it, so a remote create —
// which is this verb over ssh — produced a shell whose only evidence of an
// agent was live screen identification, and it went missing from the board
// whenever that missed a frame.
func TestCreateShellRecordsTheAgentType(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	workDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	t.Chdir(workDir)
	writeProjectMeta(t, stateDir, "demo", workDir)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--agent", "claude", "--json", "--wait", "0"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var result createShellResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", result.Shell.Session).Run()
	})

	listed, err := shellstate.ListAtPath(filepath.Join(stateDir, "projects", "demo", "shells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].AgentType != "claude" {
		t.Fatalf("manifest = %+v, want one record carrying agentType claude", listed)
	}
	// --agent alone starts nothing: recording the family and launching the
	// process are separate concerns, and the caller that passes --run (or
	// `shell send --run` afterwards) owns the second.
	if result.Shell.Session == "" {
		t.Fatal("no session was created")
	}
}

// TestCreateShellRefusesAnUnknownAgentType. An unchecked --agent would create a
// shell recorded as "claud", and every surface keyed on the agent type — the
// provider column, activity identification, session lookup — would then disagree
// with whatever ends up in the pane. Exit 5, not 2: it is a verdict on a value,
// and a caller on another machine reads 2 as version skew.
func TestCreateShellRefusesAnUnknownAgentType(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	workDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	t.Chdir(workDir)
	writeProjectMeta(t, stateDir, "demo", workDir)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"create", "shell", "--agent", "claud", "--wait", "0"}, &out, &errOut)
	if !handled || code != exitInputRejected {
		t.Fatalf("unknown agent = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "unknown agent type") || !strings.Contains(errOut.String(), "claude") {
		t.Fatalf("stderr = %q, want the refusal and the accepted list", errOut.String())
	}
	listed, err := shellstate.ListAtPath(filepath.Join(stateDir, "projects", "demo", "shells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("a refused agent type still created a shell: %+v", listed)
	}
}

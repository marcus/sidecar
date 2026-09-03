package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspaceops"
)

func TestRunDispatch(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		handled  bool
		code     int
		contains string
	}{
		{"legacy", []string{"--version"}, false, 0, ""},
		{"shell help", []string{"shell", "--help"}, true, 0, "sidecar shell <command>"},
		{"name help", []string{"shell", "name", "--help"}, true, 0, "Print the Sidecar display name"},
		{"rename help", []string{"shell", "rename", "--help"}, true, 0, "Sidecar-managed shell or worktree agent"},
		{"list help", []string{"shell", "list", "--help"}, true, 0, "List Sidecar-managed shell records"},
		{"forget help", []string{"shell", "forget", "--help"}, true, 0, "Forget a Sidecar-managed shell record"},
		{"restore help", []string{"shell", "restore", "--help"}, true, 0, "Restore a forgotten Sidecar-managed shell record"},
		{"content help", []string{"content", "--help"}, true, 0, "internal transport endpoint"},
		{"unknown", []string{"shell", "wat"}, true, 2, "unknown shell command"},
		{"forget missing name", []string{"shell", "forget"}, true, 2, "exactly one tmux session name"},
		{"restore extra args", []string{"shell", "restore", "one", "two"}, true, 2, "exactly one tmux session name"},
		{"name positional", []string{"shell", "name", "extra"}, true, 2, "no positional"},
		{"missing name", []string{"shell", "rename"}, true, 2, "exactly one quoted"},
		{"unquoted name", []string{"shell", "rename", "two", "words"}, true, 2, "exactly one quoted"},
		{"invalid name", []string{"shell", "rename", "bad\nname"}, true, 2, "control characters"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := Run(tt.args, &out, &errOut)
			if handled != tt.handled || code != tt.code {
				t.Fatalf("Run() = %v,%d", handled, code)
			}
			if tt.contains != "" && !strings.Contains(out.String()+errOut.String(), tt.contains) {
				t.Fatalf("output %q missing %q", out.String()+errOut.String(), tt.contains)
			}
		})
	}
}

func TestAgentGuidanceAliases(t *testing.T) {
	var canonical bytes.Buffer
	handled, code := Run([]string{"agents"}, &canonical, &bytes.Buffer{})
	if !handled || code != 0 {
		t.Fatalf("Run(agents) = handled %v code %d", handled, code)
	}
	if !strings.Contains(canonical.String(), "Sidecar commands for agents") {
		t.Fatalf("agents output is not guidance:\n%s", canonical.String())
	}

	for _, alias := range []string{"--agents", "-a"} {
		t.Run(alias, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := Run([]string{alias}, &out, &errOut)
			if !handled || code != 0 || errOut.Len() != 0 {
				t.Fatalf("Run(%s) = handled %v code %d stderr %q", alias, handled, code, errOut.String())
			}
			if out.String() != canonical.String() {
				t.Fatalf("%s output differs from sidecar agents", alias)
			}
		})
	}
}

func TestRunRenameJSONSteelThread(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "stale")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")
	var out, errOut bytes.Buffer
	handled, code := Run([]string{"shell", "rename", "--json", "  current work  "}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("Run() = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var result shellstate.RenameResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not one JSON object: %q: %v", out.String(), err)
	}
	if !result.Changed || result.OldName != "stale" || result.Name != "current work" {
		t.Fatalf("unexpected result: %+v", result)
	}
	stateDir := filepath.Join(stateHome, "sidecar")
	entries, err := os.ReadDir(filepath.Join(stateDir, "requests"))
	if err != nil {
		t.Fatalf("read repaint requests: %v", err)
	}
	var repaint uirequest.Request
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-rename-shell.json") {
			continue
		}
		repaint, err = uirequest.ReadRequest(filepath.Join(stateDir, "requests", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		found = true
	}
	if !found || repaint.Action != uirequest.ActionRenameShell || repaint.Origin.TmuxSession != "sidecar-sh-sidecar-1" || repaint.Target.Value != "current work" {
		t.Fatalf("repaint request = %+v", repaint)
	}
}

func TestRunNameSteelThread(t *testing.T) {
	stateHome, socket := setupShellCLI(t, "prior task context")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"shell", "name"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("Run(name) = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "prior task context" {
		t.Fatalf("human name = %q", got)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "name", "--json"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("Run(name --json) = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var result shellstate.LookupResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not one JSON object: %q: %v", out.String(), err)
	}
	if result.Shell != "sidecar-sh-sidecar-1" || result.Name != "prior task context" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunShellCommandsResolveManagedWorktreeAgent(t *testing.T) {
	stateHome, stateDir, projectRoot, worktreeRoot, session, socket := setupWorktreeCLI(t, "panes")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")

	var out, errOut bytes.Buffer
	if handled, code := Run([]string{"shell", "name", "--json"}, &out, &errOut); !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("name = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var lookup shellstate.LookupResult
	if err := json.Unmarshal(out.Bytes(), &lookup); err != nil {
		t.Fatalf("name output = %q: %v", out.String(), err)
	}
	if lookup.Shell != session || lookup.Name != "panes" {
		t.Fatalf("lookup = %+v", lookup)
	}

	out.Reset()
	errOut.Reset()
	if handled, code := Run([]string{"shell", "rename", "--json", "trim pane handles"}, &out, &errOut); !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("rename = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var renamed shellstate.RenameResult
	if err := json.Unmarshal(out.Bytes(), &renamed); err != nil {
		t.Fatalf("rename output = %q: %v", out.String(), err)
	}
	if !renamed.Changed || renamed.OldName != "panes" || renamed.Name != "trim pane handles" {
		t.Fatalf("rename = %+v", renamed)
	}
	if got, err := workspaceops.LookupWorktreeDisplayName(stateDir, projectRoot, worktreeRoot); err != nil || got != "trim pane handles" {
		t.Fatalf("persisted name = %q, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "requests"))
	if err != nil {
		t.Fatalf("read repaint requests: %v", err)
	}
	var repaint uirequest.Request
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-rename-worktree.json") {
			continue
		}
		repaint, err = uirequest.ReadRequest(filepath.Join(stateDir, "requests", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
	}
	if repaint.Action != uirequest.ActionRenameWorktree || repaint.Origin.WorkDir != worktreeRoot || repaint.Target.Value != "trim pane handles" {
		t.Fatalf("repaint request = %+v", repaint)
	}
}

func TestRunShellListForgetRestore(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	workDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	t.Chdir(workDir)
	writeProjectMeta(t, stateDir, "demo", workDir)
	def := shellstate.Definition{
		TmuxName: "sidecar-sh-demo-1", DisplayName: "prior task", Namespace: "/tmp/socket",
		AgentType: "codex", SkipPerms: true, WorkDir: workDir,
	}
	writeProjectShell(t, stateDir, "demo", def)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"shell", "list", "--json"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("list = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var listed shellListResult
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("list json: %v (%q)", err, out.String())
	}
	if len(listed.Shells) != 1 || listed.Shells[0].Shell != def.TmuxName || listed.Shells[0].Name != def.DisplayName || listed.Shells[0].Status != shellStatusLive {
		t.Fatalf("list = %+v", listed)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "forget", "--json", def.TmuxName}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("forget = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var forgotten shellRecordResult
	if err := json.Unmarshal(out.Bytes(), &forgotten); err != nil {
		t.Fatalf("forget json: %v (%q)", err, out.String())
	}
	if forgotten.Status != shellStatusForgotten || forgotten.Shell != def.TmuxName {
		t.Fatalf("forget = %+v", forgotten)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "list", "--json"}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("list after forget = %v %d %q", handled, code, errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Shells) != 1 || listed.Shells[0].Status != shellStatusForgotten || listed.Shells[0].Name != "prior task" || listed.Shells[0].AgentType != "codex" || !listed.Shells[0].SkipPerms {
		t.Fatalf("forgotten list = %+v", listed)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "forget", "--json", def.TmuxName}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("forget already = %v %d %q", handled, code, errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), &forgotten); err != nil {
		t.Fatal(err)
	}
	if forgotten.Status != shellStatusAlreadyForgotten {
		t.Fatalf("already forgotten = %+v", forgotten)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "forget", "sidecar-sh-missing"}, &out, &errOut)
	if !handled || code != 1 || !strings.Contains(errOut.String(), "no shell record named") {
		t.Fatalf("forget unknown = %v %d %q", handled, code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "restore", "--json", def.TmuxName}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("restore = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	var restored shellRecordResult
	if err := json.Unmarshal(out.Bytes(), &restored); err != nil {
		t.Fatalf("restore json: %v (%q)", err, out.String())
	}
	if restored.Status != shellStatusRestored || restored.Name != "prior task" {
		t.Fatalf("restore = %+v", restored)
	}
	path := filepath.Join(stateDir, "projects", "demo", "shells.json")
	live, err := shellstate.ListAtPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].DisplayName != "prior task" || live[0].AgentType != "codex" || !live[0].SkipPerms || live[0].WorkDir != workDir {
		t.Fatalf("live after restore = %+v", live)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "restore", "--json", def.TmuxName}, &out, &errOut)
	if !handled || code != 0 {
		t.Fatalf("restore live = %v %d %q", handled, code, errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Status != shellStatusAlreadyLive {
		t.Fatalf("already live = %+v", restored)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"shell", "restore", "sidecar-sh-missing"}, &out, &errOut)
	if !handled || code != 1 || !strings.Contains(errOut.String(), "no forgotten shell record named") {
		t.Fatalf("restore unknown = %v %d %q", handled, code, errOut.String())
	}
}

// The tmux name is the only argument `sidecar shell restore` takes, so the
// human listing has to show forgotten records too. Hiding them behind --json
// would leave a human able to forget a record and unable to name it again.
func TestRunShellListShowsForgottenRecordsWithoutJSON(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	workDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	t.Chdir(workDir)
	writeProjectMeta(t, stateDir, "demo", workDir)
	live := shellstate.Definition{
		TmuxName: "sidecar-sh-demo-1", DisplayName: "live task", Namespace: "/tmp/socket", WorkDir: workDir,
	}
	gone := shellstate.Definition{
		TmuxName: "sidecar-sh-demo-2", DisplayName: "prior task", Namespace: "/tmp/socket", WorkDir: workDir,
	}
	writeProjectShells(t, stateDir, "demo", live, gone)

	var out, errOut bytes.Buffer
	if handled, code := Run([]string{"shell", "forget", gone.TmuxName}, &out, &errOut); !handled || code != 0 {
		t.Fatalf("forget = %v %d %q", handled, code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	handled, code := Run([]string{"shell", "list"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("list = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{live.TmuxName, "live task", gone.TmuxName, "prior task", "sidecar shell restore"} {
		if !strings.Contains(text, want) {
			t.Fatalf("list output %q is missing %q", text, want)
		}
	}
}

func TestRunShellNameRejectsLookalikeWorktreeSession(t *testing.T) {
	stateHome, _, _, _, _, socket := setupWorktreeCLI(t, "panes")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", socket+",1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("FAKE_TMUX_SESSION", "sidecar-ws-lookalike")

	var out, errOut bytes.Buffer
	if handled, code := Run([]string{"shell", "name"}, &out, &errOut); !handled || code != 1 {
		t.Fatalf("name = handled %v code %d stderr %q", handled, code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "does not match its Sidecar worktree identity") {
		t.Fatalf("unexpected refusal: %q", errOut.String())
	}
}

// setupShellCLI installs a fake tmux that reports a fixed session/socket and a
// matching shells.json under an isolated XDG_STATE_HOME tree.
func setupShellCLI(t *testing.T, displayName string) (stateHome, socket string) {
	t.Helper()
	stateHome = t.TempDir()
	projectDir := filepath.Join(stateHome, "sidecar", "projects", "sidecar")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	socket = filepath.Join(t.TempDir(), "tmux.sock")
	manifest := `{"version":1,"shells":[{"tmuxName":"sidecar-sh-sidecar-1","displayName":` + quoteJSON(t, displayName) + `,"namespace":` + quoteJSON(t, socket) + `}]}`
	if err := os.WriteFile(filepath.Join(projectDir, "shells.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	tmux := filepath.Join(binDir, "tmux")
	script := "#!/bin/sh\nprintf 'sidecar-sh-sidecar-1\\t%s\\n' " + shellQuote(socket) + "\n"
	if err := os.WriteFile(tmux, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stateHome, socket
}

func setupWorktreeCLI(t *testing.T, displayName string) (stateHome, stateDir, projectRoot, worktreeRoot, session, socket string) {
	t.Helper()
	stateHome = t.TempDir()
	stateDir = filepath.Join(stateHome, "sidecar")
	projectRoot = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(projectRoot); err == nil {
		projectRoot = resolved
	}
	worktreeRoot = projectRoot
	cmd := exec.Command("git", "init", "-q", projectRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	if _, err := projectdir.WorktreeDirWithBase(stateDir, projectRoot, worktreeRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceops.RenameWorktreeDisplayName(t.Context(), stateDir, projectRoot, worktreeRoot, displayName); err != nil {
		t.Fatal(err)
	}
	session = workspaceops.WorktreeSessionName(worktreeRoot, "")
	socket = filepath.Join(t.TempDir(), "tmux.sock")
	binDir := t.TempDir()
	tmux := filepath.Join(binDir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\t%s\\t%s\\n' \"${FAKE_TMUX_SESSION:-" + session + "}\" " + shellQuote(socket) + " " + shellQuote(worktreeRoot) + "\n"
	if err := os.WriteFile(tmux, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stateHome, stateDir, projectRoot, worktreeRoot, session, socket
}

// setupIsolatedCLI points state at a temp tree and clears TMUX so open
// resolution cannot see a Sidecar shell.
func setupIsolatedCLI(t *testing.T) (stateHome, stateDir string) {
	t.Helper()
	stateHome = t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	// Both isolation axes, not one. XDG_STATE_HOME moves the state tree;
	// config.json and state.json are $HOME-based and only -config moves them
	// (AGENTS.md, td-8d18de). Without this the config path still resolved into
	// the developer's real ~/.config/sidecar, which is exactly what the
	// pre-handler CheckStateIsolation gate now refuses for a mutating verb.
	config.SetConfigPath(filepath.Join(stateHome, "config", "config.json"))
	t.Cleanup(func() { config.SetConfigPath(defaultTestConfigPath) })
	stateDir = filepath.Join(stateHome, "sidecar")
	if err := os.MkdirAll(filepath.Join(stateDir, "projects"), 0755); err != nil {
		t.Fatal(err)
	}
	return stateHome, stateDir
}

func writeProjectMeta(t *testing.T, stateDir, slug, workDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "projects", slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"path":`+quoteJSON(t, workDir)+`}`), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeProjectShell(t *testing.T, stateDir, slug string, shell shellstate.Definition) {
	t.Helper()
	writeProjectShells(t, stateDir, slug, shell)
}

// writeProjectShells replaces the project's manifest with exactly these
// definitions. It replaces rather than appends, so callers that need more than
// one shell must pass them in a single call.
func writeProjectShells(t *testing.T, stateDir, slug string, shells ...shellstate.Definition) {
	t.Helper()
	dir := filepath.Join(stateDir, "projects", slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := struct {
		Version int                     `json:"version"`
		Shells  []shellstate.Definition `json:"shells"`
	}{Version: 1, Shells: shells}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shells.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// `sidecar -config <path> notify post ...` must reach the subcommand. The
// global flag before the command used to make args[0] unmatchable, so dispatch
// fell through to TUI startup and the binary died with "Sidecar requires an
// interactive terminal".
func TestGlobalFlagsBeforeACommandStillDispatch(t *testing.T) {
	rest, cfg, ok := stripGlobalFlags([]string{"-config", "/tmp/x/config.json", "notify", "post", "hi"})
	if !ok || cfg != "/tmp/x/config.json" {
		t.Fatalf("strip = (%v, %q, %v)", rest, cfg, ok)
	}
	if len(rest) != 3 || rest[0] != "notify" {
		t.Fatalf("rest = %v, want the command and its arguments", rest)
	}

	rest, cfg, ok = stripGlobalFlags([]string{"--config=/tmp/y", "-debug", "-project", "/repo", "notify", "list"})
	if !ok || cfg != "/tmp/y" || len(rest) != 2 || rest[0] != "notify" {
		t.Fatalf("strip = (%v, %q, %v)", rest, cfg, ok)
	}

	// Flags the CLI does not own are left alone: `sidecar -version` and
	// `sidecar -project /x` still belong to ordinary TUI startup.
	if _, _, ok := stripGlobalFlags([]string{"-version"}); ok {
		t.Fatal("-version must fall through to flag parsing")
	}
	if rest, _, ok := stripGlobalFlags([]string{"-project", "/repo"}); !ok || len(rest) != 0 {
		t.Fatalf("a flag-only argv leaves no command: rest=%v ok=%v", rest, ok)
	}

	if !namesCommand("notify") || namesCommand("-project") {
		t.Fatal("namesCommand disagrees with the registry")
	}
}

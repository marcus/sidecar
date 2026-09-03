package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/workspaceops"
)

func TestWorktreeDeleteRequiresConfirmationAndIsDiscoverable(t *testing.T) {
	setupIsolatedCLI(t)
	for _, tt := range []struct {
		name     string
		args     []string
		code     int
		contains string
	}{
		{name: "missing target", args: []string{"worktree", "delete", "--plan"}, code: 2, contains: "requires exactly one"},
		{name: "confirmation required", args: []string{"worktree", "delete", "topic"}, code: 2, contains: "--yes is required"},
		{name: "plan cannot confirm", args: []string{"worktree", "delete", "topic", "--plan", "--yes"}, code: 2, contains: "cannot be combined"},
		{name: "unknown option", args: []string{"worktree", "delete", "topic", "--wat"}, code: 2, contains: "unknown option"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := Run(tt.args, &out, &errOut)
			if !handled || code != tt.code {
				t.Fatalf("Run(%v) = handled %v code %d, want true/%d; stderr %q", tt.args, handled, code, tt.code, errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), tt.contains) {
				t.Fatalf("Run(%v) output missing %q: stdout %q stderr %q", tt.args, tt.contains, out.String(), errOut.String())
			}
		})
	}

	help := RenderHelp(RootCommand().FindSubcommand("worktree").FindSubcommand("delete"))
	for _, want := range []string{"--dry-run", "--yes", "--delete-local-branch", "--expect-branch", "--expect-head-oid", "absolute path", "pending-creation journal"} {
		if !strings.Contains(help, want) {
			t.Errorf("worktree delete help missing %q:\n%s", want, help)
		}
	}
	agents := RenderAgents(RootCommand())
	if !strings.Contains(agents, "sidecar worktree delete TARGET --plan --json") {
		t.Fatalf("sidecar agents does not advertise worktree delete:\n%s", agents)
	}
}

func TestWorktreeDeletePlannedIdentityFlagsArePairedAndRequireAbsoluteTarget(t *testing.T) {
	setupIsolatedCLI(t)
	for _, tt := range []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "head only", args: []string{"topic", "--expect-head-oid", "abc", "--yes"}, contains: "must be provided together"},
		{name: "branch only", args: []string{"topic", "--expect-branch", "topic", "--yes"}, contains: "must be provided together"},
		{name: "relative target", args: []string{"topic", "--expect-branch", "topic", "--expect-head-oid", "abc", "--yes"}, contains: "absolute path returned by --plan"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			args := append([]string{"worktree", "delete"}, tt.args...)
			handled, code := Run(args, &out, &errOut)
			if !handled || code != 2 || !strings.Contains(errOut.String(), tt.contains) {
				t.Fatalf("Run(%v) = handled %v code %d stdout %q stderr %q", args, handled, code, out.String(), errOut.String())
			}
		})
	}
}

func TestWorktreeDeletePlanRefusesUnsafeInventoryStates(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	writeProjectMeta(t, stateDir, "demo", root)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"worktree", "delete", root, "--project", "demo", "--plan"}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "main worktree") {
		t.Fatalf("main worktree refusal = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}

	locked := filepath.Join(filepath.Dir(root), "locked")
	runGit(t, root, "worktree", "add", "-q", "-b", "locked", locked)
	runGit(t, root, "worktree", "lock", locked)
	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"worktree", "delete", "locked", "--project", "demo", "--plan", "--json"}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "locked") {
		t.Fatalf("locked worktree refusal = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	var refusal worktreeDeleteErrorDocument
	if err := json.Unmarshal(errOut.Bytes(), &refusal); err != nil || refusal.Error.Code != "refused" {
		t.Fatalf("structured refusal = %+v, %v (%q)", refusal, err, errOut.String())
	}

	localOnly := filepath.Join(filepath.Dir(root), "local-only")
	runGit(t, root, "worktree", "add", "-q", "-b", "local-only", localOnly)
	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{
		"worktree", "delete", "local-only", "--project", "demo", "--delete-remote-branch", "--plan",
	}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "does not exist") || !strings.Contains(errOut.String(), "omit --delete-remote-branch") {
		t.Fatalf("absent remote branch refusal = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	assertPathExists(t, localOnly)
}

func TestWorktreeDeleteCarriesEverySharedRefusalMarker(t *testing.T) {
	existing := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	mainPath := workspaceops.CanonicalWorktreePath(existing)
	for _, tt := range []struct {
		name  string
		state workspaceops.WorktreeState
		want  string
	}{
		{name: "main", state: workspaceops.WorktreeState{Path: existing, Branch: "main"}, want: "main worktree"},
		{name: "bare", state: workspaceops.WorktreeState{Path: existing + "-bare", Branch: "topic", Bare: true}, want: "bare worktree"},
		{name: "detached", state: workspaceops.WorktreeState{Path: existing + "-detached", Detached: true}, want: "checked-out branch"},
		{name: "locked", state: workspaceops.WorktreeState{Path: existing + "-locked", Branch: "topic", Locked: true}, want: "locked"},
		{name: "missing", state: workspaceops.WorktreeState{Path: missing, Branch: "topic"}, want: "path is missing"},
		{name: "prunable", state: workspaceops.WorktreeState{Path: existing + "-prunable", Branch: "topic", Prunable: true}, want: "record is prunable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// For every marker except missing, give the pure mapping an existing
			// directory so the earlier missing-path refusal cannot hide it.
			if tt.name != "missing" && tt.name != "main" {
				if err := os.MkdirAll(tt.state.Path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := worktreeDeleteRefusal(tt.state, mainPath); !strings.Contains(got, tt.want) {
				t.Fatalf("refusal = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorktreeDeletePlanAndExecuteRealLifecycle(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	worktree := filepath.Join(filepath.Dir(root), "repo-topic")
	runGit(t, root, "worktree", "add", "-q", "-b", "topic", worktree)
	worktree = canonicalTestPath(t, worktree)
	writeProjectMeta(t, stateDir, "demo", root)
	writeRegisteredWorktree(t, stateDir, root, worktree)
	if _, err := workspaceops.RenameWorktreeDisplayName(t.Context(), stateDir, root, worktree, "Plugin cleanup"); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(stateDir, "projects", "demo", "shells.json")
	writeProjectShells(t, stateDir, "demo",
		shellstate.Definition{TmuxName: "sidecar-sh-topic", DisplayName: "Topic shell", WorkDir: worktree},
		shellstate.Definition{TmuxName: "sidecar-sh-main", DisplayName: "Main shell", WorkDir: root},
	)
	tmuxLog := installAbsentTmux(t)

	if err := os.WriteFile(filepath.Join(worktree, "uncommitted.txt"), []byte("keep warning honest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	headOID := gitOutputForTest(t, worktree, "rev-parse", "HEAD")
	repoKey, err := workspaceops.RepoKeyForPath(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	worktreeKey, err := projectdir.WorktreeKey(worktree)
	if err != nil {
		t.Fatal(err)
	}
	creationPlan := &workspaceops.WorktreePlan{
		RepoKey: repoKey, OperationID: "failed-create", SourceWorktree: root, MainWorktree: root,
		SourceRef: "refs/heads/main", SourceOID: headOID, Branch: "topic", Path: worktree, DisplayName: "Plugin cleanup",
	}
	record := &workspaceops.WorktreeRecord{Key: worktreeKey, RepoKey: repoKey, Name: "Plugin cleanup", Path: worktree, Branch: "topic", HEADOID: headOID}
	if err := workspaceops.PersistPendingCreation(t.Context(), creationPlan, record); err != nil {
		t.Fatal(err)
	}
	journalPath, err := workspaceops.PendingCreationPath(t.Context(), creationPlan)
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"worktree", "delete", "Plugin cleanup", "--project", "demo", "--delete-local-branch", "--plan", "--json"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("plan = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	var planned worktreeDeleteDocument
	if err := json.Unmarshal(out.Bytes(), &planned); err != nil {
		t.Fatalf("plan JSON: %v (%q)", err, out.String())
	}
	if planned.Status != worktreeDeleteStatusPlanned || planned.Deleted || planned.Plan.Path != worktree || planned.Plan.HeadOID != headOID {
		t.Fatalf("plan identity = %+v", planned)
	}
	if !strings.HasPrefix(planned.Plan.Dirtiness, "dirty") || !planned.Plan.DeleteLocalBranch || planned.Plan.DeleteRemoteBranch || !planned.Plan.PendingCreation {
		t.Fatalf("plan choices = %+v", planned.Plan)
	}
	assertPathExists(t, worktree)
	assertPathExists(t, journalPath)
	if got := gitOutputForTest(t, root, "show-ref", "--verify", "refs/heads/topic"); got == "" {
		t.Fatal("planning deleted the topic branch")
	}
	if data, err := os.ReadFile(tmuxLog); err != nil || len(data) != 0 {
		t.Fatalf("planning touched tmux: %q, %v", data, err)
	}
	if defs, err := shellstate.ListAtPath(manifestPath); err != nil || len(defs) != 2 {
		t.Fatalf("planning changed shell records: %+v, %v", defs, err)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{"worktree", "delete", "Plugin cleanup", "--project", "demo", "--delete-local-branch", "--dry-run"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("human dry-run = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	for _, want := range []string{"HEAD OID: " + headOID, "Remote branch exists: false", "Delete local branch: true", "No changes made"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("human dry-run missing %q:\n%s", want, out.String())
		}
	}
	assertPathExists(t, worktree)
	assertPathExists(t, journalPath)

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{
		"worktree", "delete", planned.Plan.Path, "--project", "demo", "--delete-local-branch",
		"--expect-branch", planned.Plan.Branch, "--expect-head-oid", planned.Plan.HeadOID, "--yes", "--json",
	}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("delete = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	var deleted worktreeDeleteDocument
	if err := json.Unmarshal(out.Bytes(), &deleted); err != nil {
		t.Fatalf("delete JSON: %v (%q)", err, out.String())
	}
	if deleted.Status != worktreeDeleteStatusDeleted || !deleted.Deleted || len(deleted.Warnings) != 0 {
		t.Fatalf("delete result = %+v", deleted)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree directory survived: %v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("pending creation journal survived: %v", err)
	}
	if cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/topic"); func() bool {
		cmd.Dir = root
		return cmd.Run() == nil
	}() {
		t.Fatal("local branch survived --delete-local-branch")
	}
	defs, err := shellstate.ListAtPath(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].TmuxName != "sidecar-sh-main" {
		t.Fatalf("live shell records = %+v, want only main shell", defs)
	}
	tombs, err := shellstate.ListTombstonesAtPath(manifestPath)
	if err != nil || len(tombs) != 1 || tombs[0].TmuxName != "sidecar-sh-topic" {
		t.Fatalf("tombstones = %+v, %v", tombs, err)
	}
	logData, err := os.ReadFile(tmuxLog)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	for _, session := range []string{"sidecar-sh-topic", workspaceops.WorktreeSessionName(worktree, "")} {
		if !strings.Contains(logText, "kill-session -t "+session) {
			t.Errorf("tmux teardown did not name %q; log:\n%s", session, logText)
		}
	}
}

func TestWorktreeDeleteExpectedHEADRefusesBeforeMutation(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	worktree := filepath.Join(filepath.Dir(root), "repo-topic")
	runGit(t, root, "worktree", "add", "-q", "-b", "topic", worktree)
	worktree = canonicalTestPath(t, worktree)
	writeProjectMeta(t, stateDir, "demo", root)
	installAbsentTmux(t)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{
		"worktree", "delete", worktree, "--project", "demo", "--expect-branch", "topic", "--expect-head-oid", strings.Repeat("0", 40), "--yes", "--json",
	}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "HEAD moved") {
		t.Fatalf("stale HEAD = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	assertPathExists(t, worktree)
}

func TestWorktreeDeleteContinuesCleanupAfterPostRemovalShellWarning(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	worktree := filepath.Join(filepath.Dir(root), "repo-warning")
	runGit(t, root, "worktree", "add", "-q", "-b", "warning", worktree)
	worktree = canonicalTestPath(t, worktree)
	writeProjectMeta(t, stateDir, "demo", root)
	writeRegisteredWorktree(t, stateDir, root, worktree)
	writeProjectShells(t, stateDir, "demo", shellstate.Definition{
		TmuxName: "sidecar-sh-stubborn", DisplayName: "Stubborn", WorkDir: worktree,
	})
	installStubbornTmux(t, "sidecar-sh-stubborn")

	headOID := gitOutputForTest(t, worktree, "rev-parse", "HEAD")
	repoKey, err := workspaceops.RepoKeyForPath(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	worktreeKey, err := projectdir.WorktreeKey(worktree)
	if err != nil {
		t.Fatal(err)
	}
	creationPlan := &workspaceops.WorktreePlan{
		RepoKey: repoKey, OperationID: "warning-create", SourceWorktree: root, MainWorktree: root,
		SourceRef: "refs/heads/main", SourceOID: headOID, Branch: "warning", Path: worktree, DisplayName: "warning",
	}
	record := &workspaceops.WorktreeRecord{Key: worktreeKey, RepoKey: repoKey, Name: "warning", Path: worktree, Branch: "warning", HEADOID: headOID}
	if err := workspaceops.PersistPendingCreation(t.Context(), creationPlan, record); err != nil {
		t.Fatal(err)
	}
	journalPath, err := workspaceops.PendingCreationPath(t.Context(), creationPlan)
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	handled, code := Run([]string{
		"worktree", "delete", worktree, "--project", "demo", "--delete-local-branch", "--yes", "--json",
	}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("delete with teardown warning = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	var result worktreeDeleteDocument
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Deleted || result.Status != worktreeDeleteStatusDeleted || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "shell teardown") {
		t.Fatalf("result = %+v, want deleted with a shell teardown warning", result)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree survived: %v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("journal cleanup stopped after warning: %v", err)
	}
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/warning")
	cmd.Dir = root
	if cmd.Run() == nil {
		t.Fatal("branch cleanup stopped after warning")
	}
}

func TestWorktreeDeletePlannedIdentityRefusesBranchRenameAtSameHEAD(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	worktree := filepath.Join(filepath.Dir(root), "repo-topic")
	runGit(t, root, "worktree", "add", "-q", "-b", "topic", worktree)
	worktree = canonicalTestPath(t, worktree)
	writeProjectMeta(t, stateDir, "demo", root)
	installAbsentTmux(t)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"worktree", "delete", worktree, "--project", "demo", "--plan", "--json"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("plan = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	var planned worktreeDeleteDocument
	if err := json.Unmarshal(out.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}
	runGit(t, worktree, "branch", "-m", "replacement")
	if got := gitOutputForTest(t, worktree, "rev-parse", "HEAD"); got != planned.Plan.HeadOID {
		t.Fatalf("rename moved HEAD to %s, want %s", got, planned.Plan.HeadOID)
	}

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{
		"worktree", "delete", planned.Plan.Path, "--project", "demo",
		"--expect-branch", planned.Plan.Branch, "--expect-head-oid", planned.Plan.HeadOID, "--yes", "--json",
	}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "branch changed") || !strings.Contains(errOut.String(), "replacement") {
		t.Fatalf("renamed identity = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	assertPathExists(t, worktree)
}

func TestWorktreeDeletePlannedIdentityRefusesWorktreeMoveAtSameBranchAndHEAD(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	worktree := filepath.Join(filepath.Dir(root), "repo-topic")
	runGit(t, root, "worktree", "add", "-q", "-b", "topic", worktree)
	worktree = canonicalTestPath(t, worktree)
	writeProjectMeta(t, stateDir, "demo", root)
	installAbsentTmux(t)

	var out, errOut bytes.Buffer
	handled, code := Run([]string{"worktree", "delete", worktree, "--project", "demo", "--plan", "--json"}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("plan = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	var planned worktreeDeleteDocument
	if err := json.Unmarshal(out.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(filepath.Dir(root), "repo-moved")
	runGit(t, root, "worktree", "move", worktree, moved)
	moved = canonicalTestPath(t, moved)

	out.Reset()
	errOut.Reset()
	handled, code = Run([]string{
		"worktree", "delete", planned.Plan.Path, "--project", "demo",
		"--expect-branch", planned.Plan.Branch, "--expect-head-oid", planned.Plan.HeadOID, "--yes", "--json",
	}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "no worktree named") {
		t.Fatalf("moved identity = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	assertPathExists(t, moved)
}

func TestWorktreeDeleteRevalidatesIdentityAfterPlanningRace(t *testing.T) {
	_, stateDir := setupIsolatedCLI(t)
	root := filepath.Join(t.TempDir(), "repo")
	initGitRepoOnMain(t, root)
	root = canonicalTestPath(t, root)
	worktree := filepath.Join(filepath.Dir(root), "repo-topic")
	runGit(t, root, "worktree", "add", "-q", "-b", "topic", worktree)
	worktree = canonicalTestPath(t, worktree)
	writeProjectMeta(t, stateDir, "demo", root)
	headOID := gitOutputForTest(t, worktree, "rev-parse", "HEAD")
	tmuxLog := installAbsentTmux(t)
	installGitRenameOnLsRemote(t, worktree, "replacement")

	var out, errOut bytes.Buffer
	handled, code := Run([]string{
		"worktree", "delete", worktree, "--project", "demo",
		"--expect-branch", "topic", "--expect-head-oid", headOID, "--yes", "--json",
	}, &out, &errOut)
	if !handled || code != exitInputRejected || !strings.Contains(errOut.String(), "replacement") || !strings.Contains(errOut.String(), "topic") {
		t.Fatalf("raced identity = handled %v code %d stdout %q stderr %q", handled, code, out.String(), errOut.String())
	}
	assertPathExists(t, worktree)
	if got := gitOutputForTest(t, worktree, "symbolic-ref", "--short", "HEAD"); got != "replacement" {
		t.Fatalf("worktree branch = %q, want injected replacement", got)
	}
	if got := gitOutputForTest(t, worktree, "rev-parse", "HEAD"); got != headOID {
		t.Fatalf("worktree HEAD = %q, want %q", got, headOID)
	}
	if data, err := os.ReadFile(tmuxLog); err != nil || len(data) != 0 {
		t.Fatalf("identity refusal touched a session: %q, %v", data, err)
	}
}

func initGitRepoOnMain(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-qm", "initial")
}

func gitOutputForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
	return strings.TrimSpace(string(out))
}

func installAbsentTmux(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func installStubbornTmux(t *testing.T, liveSession string) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = has-session ] && [ \"$3\" = " + shellQuote(liveSession) + " ]; then exit 0; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installGitRenameOnLsRemote(t *testing.T, worktree, replacement string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "renamed")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = ls-remote ] && [ ! -e " + shellQuote(marker) + " ]; then\n" +
		"  touch " + shellQuote(marker) + "\n" +
		"  " + shellQuote(realGit) + " -C " + shellQuote(worktree) + " branch -m " + shellQuote(replacement) + " || exit $?\n" +
		"fi\n" +
		"exec " + shellQuote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(resolved)
}

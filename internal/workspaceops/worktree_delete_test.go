package workspaceops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every repository here is created fresh under t.TempDir(). Nothing in this
// file may reach a real checkout, worktree, branch, or remote.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.email=test@example.invalid",
		"-c", "user.name=Test",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// throwawayRepo is a repository with one commit on main, living only for this
// test.
func throwawayRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "README")
	git(t, root, "commit", "-q", "-m", "initial")
	return root
}

func worktreePaths(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	for _, line := range strings.Split(git(t, root, "worktree", "list", "--porcelain"), "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

func TestDeleteWorktreeRemovesEvenWithUncommittedChanges(t *testing.T) {
	root := throwawayRepo(t)
	path := filepath.Join(filepath.Dir(root), "feature")
	git(t, root, "worktree", "add", "-q", path, "-b", "feature")

	// A dirty worktree makes plain `git worktree remove` fail; only a caller
	// that states Force reaches the fallback that overrides it.
	if err := os.WriteFile(filepath.Join(path, "dirty"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DeleteWorktree(context.Background(), WorktreeRemoval{RepoPath: root, Path: path, Force: true}); err != nil {
		t.Fatalf("DeleteWorktree: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the working directory survived: %v", err)
	}
	if got := worktreePaths(t, root); len(got) != 1 {
		t.Fatalf("git still lists %v", got)
	}
}

func TestDeleteWorktreePrunesOneWhoseDirectoryIsGone(t *testing.T) {
	root := throwawayRepo(t)
	path := filepath.Join(filepath.Dir(root), "gone")
	git(t, root, "worktree", "add", "-q", path, "-b", "gone")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

	if err := DeleteWorktree(context.Background(), WorktreeRemoval{RepoPath: root, Path: path, Missing: true, Force: true}); err != nil {
		t.Fatalf("DeleteWorktree(isMissing): %v", err)
	}
	if got := worktreePaths(t, root); len(got) != 1 {
		t.Fatalf("the prunable record survived: %v", got)
	}
}

func TestDeleteLocalBranchRefusesTheDefaultBranchAndForcesTheRest(t *testing.T) {
	root := throwawayRepo(t)
	git(t, root, "branch", "unmerged")
	// Give the branch a commit main does not have, so the safe delete fails
	// and only the forced one can succeed.
	git(t, root, "checkout", "-q", "unmerged")
	if err := os.WriteFile(filepath.Join(root, "extra"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "extra")
	git(t, root, "commit", "-q", "-m", "extra")
	git(t, root, "checkout", "-q", "main")

	if err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "main", Force: true}); err == nil {
		t.Fatal("the default branch was deleted")
	} else if !strings.Contains(err.Error(), "refusing to delete main branch") {
		t.Fatalf("refusal read %q", err)
	}
	if !strings.Contains(git(t, root, "branch", "--format=%(refname:short)"), "main") {
		t.Fatal("main is gone")
	}

	if err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "unmerged", Force: true}); err != nil {
		t.Fatalf("DeleteLocalBranch: %v", err)
	}
	if strings.Contains(git(t, root, "branch", "--format=%(refname:short)"), "unmerged") {
		t.Fatal("the unmerged branch survived")
	}
}

func TestDeleteLocalBranchReportsAFailureItCannotForce(t *testing.T) {
	root := throwawayRepo(t)
	err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "never-existed", Force: true})
	if err == nil {
		t.Fatal("deleting a branch that does not exist reported success")
	}
	if !strings.Contains(err.Error(), "delete branch") {
		t.Fatalf("error read %q", err)
	}
}

// origin is a bare repository on disk. Nothing here talks to a network remote.
func withLocalOrigin(t *testing.T, root string) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	git(t, root, "remote", "add", "origin", origin)
	git(t, root, "push", "-q", "-u", "origin", "main")
	return origin
}

func TestRemoteBranchDeletionIsIdempotentAgainstAnAlreadyGoneBranch(t *testing.T) {
	root := throwawayRepo(t)
	withLocalOrigin(t, root)
	git(t, root, "branch", "topic")
	git(t, root, "push", "-q", "origin", "topic")

	ctx := context.Background()
	if !RemoteBranchExists(ctx, root, "topic") {
		t.Fatal("the pushed branch is not reported on origin")
	}
	if RemoteBranchExists(ctx, root, "absent") {
		t.Fatal("a branch nobody pushed is reported on origin")
	}

	if err := DeleteRemoteBranch(ctx, BranchDeletion{RepoPath: root, Branch: "topic"}); err != nil {
		t.Fatalf("DeleteRemoteBranch: %v", err)
	}
	if RemoteBranchExists(ctx, root, "topic") {
		t.Fatal("the remote branch survived")
	}

	// Deleting it again is not an error: the branch is already gone, which is
	// the outcome the caller asked for. Note the tolerated-output match is
	// deliberately broad (it also swallows a push refused with "unable to
	// delete"); that behaviour is unchanged from where this code used to live.
	if err := DeleteRemoteBranch(ctx, BranchDeletion{RepoPath: root, Branch: "topic"}); err != nil {
		t.Fatalf("second delete reported %v, want the already-gone branch tolerated", err)
	}
}

func TestDeleteRemoteBranchRefusesTheDefaultBranch(t *testing.T) {
	root := throwawayRepo(t)
	withLocalOrigin(t, root)
	if err := DeleteRemoteBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "main"}); err == nil {
		t.Fatal("origin's default branch was deleted")
	}
	if !RemoteBranchExists(context.Background(), root, "main") {
		t.Fatal("main is gone from origin")
	}
}

func TestDefaultBranchPrefersOriginsHeadAndFallsBackToConvention(t *testing.T) {
	ctx := context.Background()

	// No remote: the conventional name that exists wins.
	local := throwawayRepo(t)
	if got := DefaultBranch(ctx, local); got != "main" {
		t.Fatalf("DefaultBranch(no remote) = %q, want main", got)
	}
	if !IsDefaultBranch(ctx, local, "main") || IsDefaultBranch(ctx, local, "topic") || IsDefaultBranch(ctx, local, "") {
		t.Fatal("IsDefaultBranch disagrees with DefaultBranch")
	}

	// With origin's HEAD recorded, that answer wins over convention.
	withHead := throwawayRepo(t)
	git(t, withHead, "checkout", "-q", "-b", "trunk")
	withLocalOrigin(t, withHead)
	git(t, withHead, "push", "-q", "-u", "origin", "trunk")
	git(t, withHead, "remote", "set-head", "origin", "trunk")
	if got := DefaultBranch(ctx, withHead); got != "trunk" {
		t.Fatalf("DefaultBranch(origin HEAD=trunk) = %q, want trunk", got)
	}
}

func TestDefaultBranchObservedRecordsEverySpawn(t *testing.T) {
	root := throwawayRepo(t)
	spawns := 0
	// A repository with no origin HEAD needs the fallback probes, so more than
	// one process runs and each must be recorded (the startup trace counts
	// them).
	if got := DefaultBranchObserved(context.Background(), root, func() { spawns++ }); got != "main" {
		t.Fatalf("DefaultBranchObserved = %q", got)
	}
	if spawns < 2 {
		t.Fatalf("recorded %d spawns, want one per git process", spawns)
	}
}

func TestPruneWorktreesReportsGitFailures(t *testing.T) {
	notARepo := t.TempDir()
	if err := PruneWorktrees(context.Background(), notARepo); err == nil {
		t.Fatal("pruning outside a repository reported success")
	}
}

// td-c31d14. There used to be three worktree-delete implementations and they
// disagreed about a dirty worktree: the one two surfaces shared force-removed
// it, while the post-merge cleanup path refused. These tests pin the collapsed
// answer — one implementation, two semantics the caller has to name.

// worktreeAt adds a worktree and returns its path and HEAD.
func worktreeAt(t *testing.T, root, name string) (string, string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(root), name)
	git(t, root, "worktree", "add", "-q", path, "-b", name)
	return path, git(t, path, "rev-parse", "HEAD")
}

func TestRemovalWithoutForceRefusesADirtyWorktree(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "careful")
	valuable := filepath.Join(path, "valuable.txt")
	if err := os.WriteFile(valuable, []byte("irreplaceable untracked work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, Path: path, Branch: "careful", ExpectedOID: head,
	})
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("removal of a dirty worktree returned %v, want ErrWorktreeDirty", err)
	}
	if _, statErr := os.Stat(valuable); statErr != nil {
		t.Fatalf("the untracked work was destroyed anyway: %v", statErr)
	}
	if got := worktreePaths(t, root); len(got) != 2 {
		t.Fatalf("the worktree was removed anyway: %v", got)
	}
}

func TestRemovalWithoutForceRefusesAWorktreeWhoseHEADMoved(t *testing.T) {
	root := throwawayRepo(t)
	path, _ := worktreeAt(t, root, "moved")
	if err := os.WriteFile(filepath.Join(path, "later"), []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", "later")
	git(t, path, "commit", "-q", "-m", "work that arrived after the confirmation")

	err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, Path: path, Branch: "moved", ExpectedOID: "0000000000000000000000000000000000000000",
	})
	if err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("removal returned %v, want a refusal naming the changed HEAD", err)
	}
	if got := worktreePaths(t, root); len(got) != 2 {
		t.Fatalf("the worktree was removed anyway: %v", got)
	}
}

func TestForcedRemovalStillEnforcesSuppliedIdentity(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "topic")
	git(t, path, "branch", "-m", "replacement")

	killed := false
	restore := killWorktreeSessions
	killWorktreeSessions = func(context.Context, string) error {
		killed = true
		return nil
	}
	t.Cleanup(func() { killWorktreeSessions = restore })

	err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, Path: path, Branch: "topic", ExpectedOID: head, Force: true,
	})
	var identityErr *WorktreeIdentityError
	if !errors.As(err, &identityErr) || !strings.Contains(err.Error(), "replacement") || !strings.Contains(err.Error(), "topic") {
		t.Fatalf("forced removal returned %T %v, want the pinned identity refusal", err, err)
	}
	if killed {
		t.Fatal("identity refusal killed the worktree session")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("identity refusal removed the worktree: %v", statErr)
	}
	if got := git(t, path, "rev-parse", "HEAD"); got != head {
		t.Fatalf("identity refusal moved HEAD to %s, want %s", got, head)
	}
}

// A caller that has pinned nothing cannot quietly get the careful path's
// blessing: without Force it must say which branch and which commit it means.
func TestRemovalWithoutForceDemandsAPinnedIdentity(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "unpinned")

	for _, tc := range []struct {
		name string
		req  WorktreeRemoval
		want string
	}{
		{"no branch", WorktreeRemoval{RepoPath: root, Path: path, ExpectedOID: head}, "expected branch"},
		{"no OID", WorktreeRemoval{RepoPath: root, Path: path, Branch: "unpinned"}, "expected HEAD OID"},
	} {
		err := DeleteWorktree(context.Background(), tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: returned %v, want a refusal mentioning %q", tc.name, err, tc.want)
		}
	}
	if got := worktreePaths(t, root); len(got) != 2 {
		t.Fatalf("an unpinned removal deleted the worktree: %v", got)
	}
}

func TestRemovalWithoutForceRemovesACleanWorktree(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "tidy")

	if err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, Path: path, Branch: "tidy", ExpectedOID: head,
	}); err != nil {
		t.Fatalf("careful removal of a clean worktree: %v", err)
	}
	if got := worktreePaths(t, root); len(got) != 1 {
		t.Fatalf("git still lists %v", got)
	}
}

// A refusal must not cost the user their session: on the careful path the
// checks run before the kill, and only the kill-then-remove order is fixed.
func TestARefusedRemovalDoesNotKillTheSession(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "still-running")
	if err := os.WriteFile(filepath.Join(path, "dirty"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	killed := false
	restore := killWorktreeSessions
	killWorktreeSessions = func(context.Context, string) error { killed = true; return nil }
	t.Cleanup(func() { killWorktreeSessions = restore })

	if err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, Path: path, Branch: "still-running", ExpectedOID: head,
	}); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("removal returned %v, want ErrWorktreeDirty", err)
	}
	if killed {
		t.Fatal("a refused removal killed the session anyway")
	}
}

func TestCarefulRemovalStillKillsBeforeRemovingTheDirectory(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "ordered-careful")

	directoryPresentAtKill := false
	restore := killWorktreeSessions
	killWorktreeSessions = func(ctx context.Context, p string) error {
		_, err := os.Stat(p)
		directoryPresentAtKill = err == nil
		return nil
	}
	t.Cleanup(func() { killWorktreeSessions = restore })

	if err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, Path: path, Branch: "ordered-careful", ExpectedOID: head,
	}); err != nil {
		t.Fatalf("careful removal: %v", err)
	}
	if !directoryPresentAtKill {
		t.Fatal("the session was killed after the directory was removed")
	}
}

// The branch half of the same rule: a caller that pins a tip gets it checked,
// and -D is reached only by a caller that asked for it.
func TestLocalBranchDeletionHonoursThePinnedTip(t *testing.T) {
	root := throwawayRepo(t)
	git(t, root, "branch", "topic")
	stale := "0000000000000000000000000000000000000000"

	err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "topic", ExpectedOID: stale})
	if err == nil || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("deletion against a stale pin returned %v, want a refusal", err)
	}
	if !strings.Contains(git(t, root, "branch", "--format=%(refname:short)"), "topic") {
		t.Fatal("the branch was deleted against a stale pin")
	}

	tip := git(t, root, "rev-parse", "refs/heads/topic")
	if got := BranchOID(context.Background(), root, "topic"); got != tip {
		t.Fatalf("BranchOID = %q, want %q", got, tip)
	}
	if err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "topic", ExpectedOID: tip}); err != nil {
		t.Fatalf("deletion against the current tip: %v", err)
	}
}

func TestLocalBranchDeletionWithoutForceRefusesAnUnmergedBranch(t *testing.T) {
	root := throwawayRepo(t)
	git(t, root, "checkout", "-q", "-b", "unmerged")
	if err := os.WriteFile(filepath.Join(root, "extra"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "extra")
	git(t, root, "commit", "-q", "-m", "extra")
	git(t, root, "checkout", "-q", "main")

	if err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "unmerged"}); err == nil {
		t.Fatal("an unmerged branch was deleted without force")
	}
	if !strings.Contains(git(t, root, "branch", "--format=%(refname:short)"), "unmerged") {
		t.Fatal("the unmerged branch is gone")
	}
	if err := DeleteLocalBranch(context.Background(), BranchDeletion{RepoPath: root, Branch: "unmerged", Force: true}); err != nil {
		t.Fatalf("forced deletion: %v", err)
	}
}

func TestRemoteBranchDeletionWithALeaseLeavesAMovedBranchAlone(t *testing.T) {
	root := throwawayRepo(t)
	withLocalOrigin(t, root)
	git(t, root, "checkout", "-q", "-b", "leased")
	git(t, root, "push", "-q", "origin", "leased")
	git(t, root, "checkout", "-q", "main")

	stale := "0000000000000000000000000000000000000000"
	if err := DeleteRemoteBranch(context.Background(), BranchDeletion{
		RepoPath: root, Branch: "leased", Remote: "origin", ExpectedOID: stale,
	}); err == nil {
		t.Fatal("a stale lease deleted the remote branch")
	}
	if !RemoteBranchExists(context.Background(), root, "leased") {
		t.Fatal("the remote branch went despite the stale lease")
	}

	tip := git(t, root, "rev-parse", "refs/heads/leased")
	if err := DeleteRemoteBranch(context.Background(), BranchDeletion{
		RepoPath: root, Branch: "leased", Remote: "origin", ExpectedOID: tip,
	}); err != nil {
		t.Fatalf("deletion with a current lease: %v", err)
	}
	if RemoteBranchExists(context.Background(), root, "leased") {
		t.Fatal("the remote branch survived a valid lease")
	}
}

// Creation rollback runs the shared path too, so the session a host may have
// already launched into the new worktree is closed rather than orphaned.
func TestDeleteCreatedWorktreeRunsTheSharedRemoval(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "rolled-back")
	plan := &WorktreePlan{SourceWorktree: root, Path: path, Branch: "rolled-back"}
	record := &WorktreeRecord{Path: path, Branch: "rolled-back", HEADOID: head}

	session := WorktreeSessionName(path, "")
	startThrowawaySession(t, session, path)

	if err := DeleteCreatedWorktree(context.Background(), plan, record); err != nil {
		t.Fatalf("DeleteCreatedWorktree: %v", err)
	}
	if SessionExists(session) {
		t.Fatalf("session %q survived the rollback", session)
	}
	if got := worktreePaths(t, root); len(got) != 1 {
		t.Fatalf("git still lists %v", got)
	}
	if strings.Contains(git(t, root, "branch", "--format=%(refname:short)"), "rolled-back") {
		t.Fatal("the created branch survived the rollback")
	}
}

func TestDeleteCreatedWorktreeStillRefusesADirtyOne(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "rollback-dirty")
	if err := os.WriteFile(filepath.Join(path, "work"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &WorktreePlan{SourceWorktree: root, Path: path, Branch: "rollback-dirty"}
	record := &WorktreeRecord{Path: path, Branch: "rollback-dirty", HEADOID: head}

	if err := DeleteCreatedWorktree(context.Background(), plan, record); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("rollback of a dirty worktree returned %v, want ErrWorktreeDirty", err)
	}
	if got := worktreePaths(t, root); len(got) != 2 {
		t.Fatalf("the worktree was removed anyway: %v", got)
	}
}

// The point of td-c31d14 is not that this file is careful; it is that nothing
// else removes a worktree. A second implementation is how the tree ended up
// with three that disagreed, and it is also how a caller would reach "remove"
// without the session kill that lives in DeleteWorktree. So the rule is
// enforced by scanning the source rather than by memory.
func TestWorktreeRemovalHasExactlyOneImplementation(t *testing.T) {
	root := moduleRoot(t)
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			// A nested checkout — an agent worktree under .claude/worktrees, a
			// vendored example — carries its own go.mod and its own copy of this
			// file. It is not part of what `go test ./...` builds, so scanning it
			// reports the shared implementation as an offender against itself.
			if path != root {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if filepath.ToSlash(rel) == "internal/workspaceops/worktree_delete.go" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(source), `"worktree", "remove"`) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("worktree removal is implemented outside the shared path in %s; "+
			"route it through workspaceops.DeleteWorktree so the session kill and the "+
			"force decision stay in one place", strings.Join(offenders, ", "))
	}
}

// moduleRoot walks up from this package to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

// The shell teardown is part of the removal, so it obeys the removal's answer:
// a refusal must not close the shells either. Validation runs before both the
// forget and the kill for exactly this reason.
func TestARefusedRemovalDoesNotForgetTheShells(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "shells-intact")
	if err := os.WriteFile(filepath.Join(path, "dirty"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	forgot := false
	restore := forgetShellsInWorktree
	forgetShellsInWorktree = func(string, string) error { forgot = true; return nil }
	t.Cleanup(func() { forgetShellsInWorktree = restore })

	if err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, ProjectRoot: root, Path: path, Branch: "shells-intact", ExpectedOID: head,
	}); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("removal returned %v, want ErrWorktreeDirty", err)
	}
	if forgot {
		t.Fatal("a refused removal forgot the worktree's shells anyway")
	}
}

// A shell that will not close is reported, but it does not stop the worktree
// going: the user asked for the worktree to be removed, and stopping halfway
// would leave both the worktree and the stale manifest rows.
func TestAShellThatWillNotCloseStillLetsTheWorktreeGo(t *testing.T) {
	root := throwawayRepo(t)
	path, head := worktreeAt(t, root, "stubborn-shell")

	restore := forgetShellsInWorktree
	forgetShellsInWorktree = func(string, string) error { return errors.New("shell refused to close") }
	t.Cleanup(func() { forgetShellsInWorktree = restore })

	err := DeleteWorktree(context.Background(), WorktreeRemoval{
		RepoPath: root, ProjectRoot: root, Path: path, Branch: "stubborn-shell", ExpectedOID: head,
	})
	if err == nil || !strings.Contains(err.Error(), "shell refused to close") {
		t.Fatalf("removal returned %v, want the shell failure reported", err)
	}
	var warning *WorktreeRemovedWarning
	if !errors.As(err, &warning) || warning.Cause == nil || !strings.Contains(warning.Cause.Error(), "shell refused to close") {
		t.Fatalf("removal returned %T %v, want a typed post-removal warning", err, err)
	}
	if got := worktreePaths(t, root); len(got) != 1 {
		t.Fatalf("the worktree survived a shell failure: %v", got)
	}
}

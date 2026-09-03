package workspaceops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// This file is the presentation-neutral worktree deletion path. Every surface
// that can delete a worktree — the project workspace and the global Workspaces
// browser — executes through it, so the two cannot drift in what "delete" does
// to git, or in what it does to the worktree's tmux session.

// WorktreeSessionPrefix names the tmux sessions Sidecar runs a worktree's agent
// in.
const WorktreeSessionPrefix = "sidecar-ws-"

// SanitizeSessionName strips the characters tmux gives meaning to in a target.
// It is the project plugin's rule, kept here so the shared delete path can
// resolve a session that plugin created.
func SanitizeSessionName(name string) string {
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}

// WorktreeSessionNames lists every tmux session name Sidecar may have started a
// worktree's agent under, most-canonical first and never empty of meaning.
//
// There are two spellings in live use and this is not the place to unify them:
// WorktreeSessionName above lowercases and slugifies (it names the sessions the
// global surface and the CLI create), while the project plugin only replaces
// tmux's metacharacters. For an ordinary lowercase directory the two agree and
// this returns one name; for `My_Feature` they do not, and a delete that knew
// only one spelling would leave the other session running — the exact bug this
// path exists to prevent. Killing both is safe: both are Sidecar-owned
// `sidecar-ws-` names derived from this one directory.
func WorktreeSessionNames(path, name string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	candidates := []string{
		WorktreeSessionName(path, name),
		WorktreeSessionPrefix + SanitizeSessionName(filepath.Base(path)),
	}
	var out []string
	for _, candidate := range candidates {
		if candidate == "" || candidate == WorktreeSessionPrefix {
			continue
		}
		if !slices.Contains(out, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// KillWorktreeSession closes a worktree's tmux session, if one is running.
//
// A session tmux has already lost is success: the requested state is reached.
// A session that is still there after the kill is a hard failure, because the
// only reason to call this is that the directory underneath it is about to be
// removed — see DeleteWorktree.
func KillWorktreeSession(ctx context.Context, sessionName string) error {
	if sessionName == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", sessionName)
	output, err := cmd.CombinedOutput()
	if err == nil || !SessionExists(sessionName) {
		return nil
	}
	return fmt.Errorf("close worktree session %s: %s: %w", sessionName, strings.TrimSpace(string(output)), err)
}

// killWorktreeSessions is indirected so tests can exercise the delete ordering
// without a tmux server.
var killWorktreeSessions = func(ctx context.Context, path string) error {
	for _, session := range WorktreeSessionNames(path, "") {
		if err := KillWorktreeSession(ctx, session); err != nil {
			return err
		}
	}
	return nil
}

// ErrWorktreeDirty is returned when a removal that was not asked to force
// finds uncommitted or untracked work in the worktree.
var ErrWorktreeDirty = errors.New("worktree is dirty (uncommitted or untracked changes)")

// WorktreeRemovedWarning reports a teardown error discovered after Git has
// already removed the worktree. Existing callers continue to receive a
// non-nil error, preserving their conservative behaviour; callers that own
// follow-up cleanup can use errors.As to distinguish this completed removal
// from a failure that left the checkout in place.
//
// Today Cause is a failure closing one or more managed shells rooted in the
// worktree. DeleteWorktree deliberately attempts the Git removal anyway so a
// stubborn shell cannot strand both the checkout and its already-forgotten
// Sidecar records.
type WorktreeRemovedWarning struct {
	Cause error
}

func (e *WorktreeRemovedWarning) Error() string {
	if e == nil || e.Cause == nil {
		return "worktree was removed with a teardown warning"
	}
	return e.Cause.Error()
}

func (e *WorktreeRemovedWarning) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func removalResult(shellErr error) error {
	if shellErr == nil {
		return nil
	}
	return &WorktreeRemovedWarning{Cause: shellErr}
}

// WorktreeIdentityError is a removal refusal caused by a checkout no longer
// matching the branch and HEAD the caller pinned. It is typed so a transport
// can report an identity refusal distinctly from a Git or tmux failure while
// existing callers continue to receive the same non-nil error text.
type WorktreeIdentityError struct {
	Cause error
}

func (e *WorktreeIdentityError) Error() string {
	if e == nil || e.Cause == nil {
		return "worktree identity changed"
	}
	return e.Cause.Error()
}

func (e *WorktreeIdentityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WorktreeRemoval is one caller's stated intent to remove a worktree. It is a
// struct rather than a list of arguments because the dangerous choice — Force —
// has to be spelled out at the call site instead of hiding in a fallback.
type WorktreeRemoval struct {
	// RepoPath is a surviving checkout the git commands run from.
	RepoPath string
	// ProjectRoot is the owning project — the manifest whose shells rooted in
	// this worktree are forgotten and closed as part of the removal
	// (td-f017b9). It is not RepoPath: on the project surface the git commands
	// run from the current worktree while the manifest belongs to the project,
	// and on the global browser the two happen to coincide. Empty means the
	// caller has no manifest to reconcile.
	ProjectRoot string
	// Path is the worktree to remove.
	Path string
	// Branch is the branch the worktree is expected to have checked out.
	// Required when Force is false or ExpectedOID is supplied. A supplied
	// branch/OID pair is always enforced, including for a forced removal.
	Branch string
	// ExpectedOID is the HEAD the caller pinned when the user confirmed.
	// Required when Force is false. When supplied with Force, it remains an
	// identity fence: Force authorizes dirty content removal, not a different
	// checkout that appeared after confirmation.
	ExpectedOID string
	// Missing means the directory is already gone, so git's record of it is
	// pruned instead of removed.
	Missing bool
	// Force removes the worktree even when it holds uncommitted or untracked
	// work, and is the only way to reach `git worktree remove --force`.
	//
	// It belongs to interactive deletion and nothing else. A person who chose
	// "Delete" in the confirmation, which says in as many words that
	// uncommitted changes will be lost, has stated the intent; refusing them
	// afterwards leaves no way to finish the job from inside Sidecar, and a
	// worktree with one untracked build artefact in it is "dirty" by the same
	// check. Every non-interactive caller — post-merge cleanup, creation
	// rollback — leaves this false and pins Branch and ExpectedOID instead:
	// nobody confirmed anything there, so an unexpected change is a reason to
	// stop, not a thing to force past.
	Force bool
}

// DeleteWorktree closes the worktree's tmux session and then removes the
// worktree at Path. A worktree whose directory has already gone is pruned from
// git's metadata instead.
//
// This is the only worktree removal in the tree. Interactive deletion on both
// surfaces, post-merge cleanup, and creation rollback all reach it, so there is
// one place that decides what "delete a worktree" does to git and to the tmux
// session, and one place to read to find out.
//
// The session teardown lives here, ahead of the git work, because the ordering
// is the point: removing the directory first leaves whatever is running in the
// session — an agent, most of the time — alive in a working directory that no
// longer exists (td-a66836, td-3df472). Putting it on the shared path is what
// stops one caller having it and another not; no caller can opt out or spell
// the session name differently, because none of them supplies it.
//
// A removal that has not been forced validates identity and dirtiness first.
// A forced removal with ExpectedOID still validates its pinned identity: Force
// authorizes deleting dirty content, not deleting a different checkout. Every
// validation runs before shell/session teardown, so a refusal costs nobody
// their session; the kill still precedes the destructive command, which is the
// guarantee that matters.
//
// Interaction with internal/shellliveness: none, deliberately. That subsystem
// reaps *shells* — sidecar-sh-* sessions recorded in shells.json — and both of
// its bindings skip anything that is not a KindShell workspace. A worktree
// session is in neither the manifest nor a liveness tracker, so a kill here
// cannot be mistaken for a suspicious disappearance and cannot race the reaper.
func DeleteWorktree(ctx context.Context, req WorktreeRemoval) error {
	if strings.TrimSpace(req.Path) == "" {
		return fmt.Errorf("worktree removal has no path")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return fmt.Errorf("worktree removal has no surviving repository path")
	}

	if !req.Missing {
		switch {
		case !req.Force:
			if err := requireRemovableWorktree(ctx, req); err != nil {
				return err
			}
		case strings.TrimSpace(req.ExpectedOID) != "":
			if err := requireRemovalIdentity(ctx, req); err != nil {
				return err
			}
		}
	}

	// The shells rooted in the worktree go first, for the same reason the
	// worktree's own session does: removing the directory first strands
	// whatever is running in them. A shell that will not close does not abort
	// the removal — the user asked for the worktree to go, and stopping halfway
	// would leave both the worktree and the stale manifest rows — so the error
	// is held back and reported only if the git work itself succeeds.
	shellErr := forgetShellsInWorktree(req.ProjectRoot, req.Path)

	if err := killWorktreeSessions(ctx, req.Path); err != nil {
		return err
	}

	if req.Missing {
		if err := PruneWorktrees(ctx, req.RepoPath); err != nil {
			return err
		}
		return removalResult(shellErr)
	}

	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", req.Path)
	cmd.Dir = req.RepoPath
	output, err := cmd.CombinedOutput()
	if err == nil {
		return removalResult(shellErr)
	}
	if !req.Force {
		// No --force fallback: the identity and cleanliness checks above are
		// immediately adjacent to this command, and git refusing here means
		// something changed between them and now.
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(output)), err)
	}

	cmd = exec.CommandContext(ctx, "git", "worktree", "remove", "--force", req.Path)
	cmd.Dir = req.RepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return removalResult(shellErr)
}

// requireRemovableWorktree is the careful half of DeleteWorktree: the checks a
// removal nobody confirmed has to pass. It reads and never writes.
func requireRemovableWorktree(ctx context.Context, req WorktreeRemoval) error {
	if err := requireRemovalIdentity(ctx, req); err != nil {
		return err
	}
	dirty, err := WorktreeIsDirty(ctx, req.Path)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("%w: %q", ErrWorktreeDirty, req.Path)
	}
	return nil
}

func requireRemovalIdentity(ctx context.Context, req WorktreeRemoval) error {
	if strings.TrimSpace(req.Branch) == "" {
		return &WorktreeIdentityError{Cause: fmt.Errorf("removal identity has no expected branch")}
	}
	if strings.TrimSpace(req.ExpectedOID) == "" {
		return &WorktreeIdentityError{Cause: fmt.Errorf("removal identity has no expected HEAD OID")}
	}
	if state := WorktreeOperationState(ctx, req.Path); state != "clean" {
		return &WorktreeIdentityError{Cause: fmt.Errorf("worktree has a Git operation in progress: %s", state)}
	}
	if err := requireCheckoutIdentity(ctx, req.Path, req.Branch, req.ExpectedOID); err != nil {
		return &WorktreeIdentityError{Cause: err}
	}
	return nil
}

// WorktreeIsDirty reports whether a worktree holds work a removal would
// destroy: anything `git status` shows, tracked or untracked.
//
// This is the one definition of "dirty" in the delete path. The refusal above
// consults it before a removal nobody forced, and both delete confirmations
// consult it to decide whether to warn that uncommitted changes will be lost —
// so the sentence the user reads and the check the code makes cannot disagree.
//
// It costs one git process, which is why no caller runs it on a refresh cycle:
// the confirmations ask when the modal opens, once, for the single worktree the
// user named. An error is not "clean": callers must treat an unanswerable
// worktree as unknown rather than reassure the user about it.
func WorktreeIsDirty(ctx context.Context, path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, fmt.Errorf("worktree path is empty")
	}
	status, err := gitOutput(ctx, path, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}
	return strings.TrimSpace(status) != "", nil
}

// requireCheckoutIdentity refuses unless path is still the safe checkout of
// branch at oid: the same worktree git listed, on the same branch, at the same
// commit the caller pinned.
func requireCheckoutIdentity(ctx context.Context, path, branch, oid string) error {
	current, err := gitOutput(ctx, path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || current != branch {
		return fmt.Errorf("worktree %q checks out %q, expected %q", path, current, branch)
	}
	worktrees, err := ListWorktreeStates(ctx, path)
	if err != nil {
		return fmt.Errorf("refresh worktree inventory: %w", err)
	}
	found := false
	for _, wt := range worktrees {
		if wt.Path == CanonicalWorktreePath(path) && wt.Branch == branch &&
			!wt.Bare && !wt.Detached && !wt.Locked && !wt.Prunable {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("worktree %q is no longer the safe checkout of %q", path, branch)
	}
	if oid != "" {
		head, err := gitOutput(ctx, path, "rev-parse", "HEAD")
		if err != nil || head != oid {
			return fmt.Errorf("HEAD changed in %q since the operation was confirmed", path)
		}
	}
	return nil
}

// WorktreeState is what `git worktree list --porcelain` reports about one
// worktree.
type WorktreeState struct {
	Path     string
	Branch   string
	HEAD     string
	Bare     bool
	Detached bool
	Locked   bool
	Prunable bool
}

// ListWorktreeStates reads git's worktree inventory for the repository dir
// belongs to.
func ListWorktreeStates(ctx context.Context, dir string) ([]WorktreeState, error) {
	out, err := gitOutput(ctx, dir, "--no-optional-locks", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var result []WorktreeState
	var current *WorktreeState
	flush := func() {
		if current != nil {
			current.Path = CanonicalWorktreePath(current.Path)
			result = append(result, *current)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &WorktreeState{Path: strings.TrimPrefix(line, "worktree ")}
		case current == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "bare":
			current.Bare = true
		case line == "detached":
			current.Detached = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			current.Prunable = true
		}
	}
	flush()
	return result, nil
}

// CanonicalWorktreePath resolves a worktree path the way git reports it, so
// two spellings of the same directory compare equal.
func CanonicalWorktreePath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

// WorktreeOperationState names the Git operation a worktree is in the middle
// of, or "clean".
func WorktreeOperationState(ctx context.Context, path string) string {
	states := []struct{ name, key string }{
		{"merge", "MERGE_HEAD"}, {"rebase", "rebase-merge"}, {"rebase", "rebase-apply"}, {"cherry-pick", "CHERRY_PICK_HEAD"},
	}
	for _, state := range states {
		gitPath, err := gitOutput(ctx, path, "rev-parse", "--git-path", state.key)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(gitPath) {
			gitPath = filepath.Join(path, gitPath)
		}
		if _, err := os.Stat(gitPath); err == nil {
			return state.name
		}
	}
	return "clean"
}

// PruneWorktrees drops git's records of worktrees whose directories are gone.
func PruneWorktrees(ctx context.Context, workDir string) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	cmd.Dir = workDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// BranchDeletion is one caller's stated intent to delete a branch. As with
// WorktreeRemoval, the destructive choice is named by the caller: Force is
// `git branch -D`, which drops commits nothing else references.
type BranchDeletion struct {
	// RepoPath is a surviving checkout the git commands run from.
	RepoPath string
	// Branch is the branch to delete.
	Branch string
	// ExpectedOID is the tip the caller resolved for this branch. When set,
	// deletion is refused if the branch has moved since — the safety property
	// the post-merge cleanup path has always had, applied to every caller that
	// can name a tip.
	ExpectedOID string
	// Remote names the remote for DeleteRemoteBranch; empty means origin.
	Remote string
	// Force deletes a branch git considers unmerged. Interactive deletion sets
	// it because the everyday case — a branch merged by squash or rebase — is
	// one git cannot recognise as merged; cleanup sets it only after it has
	// validated the merge itself.
	Force bool
}

// DeleteLocalBranch deletes a local branch, refusing the repository's default
// branch outright and refusing a branch that has moved since the caller
// resolved it.
func DeleteLocalBranch(ctx context.Context, req BranchDeletion) error {
	if IsDefaultBranch(ctx, req.RepoPath, req.Branch) {
		return fmt.Errorf("refusing to delete main branch %q", req.Branch)
	}
	if err := requireBranchTip(ctx, req.RepoPath, req.Branch, req.ExpectedOID); err != nil {
		return err
	}
	flag := "-d"
	if req.Force {
		flag = "-D"
	}
	cmd := exec.CommandContext(ctx, "git", "branch", flag, req.Branch)
	cmd.Dir = req.RepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("delete branch: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// DeleteRemoteBranch deletes the remote's copy of a branch. A branch the remote
// has already dropped (GitHub's auto-delete, for instance) is not an error.
// With ExpectedOID set the push carries a lease, so a branch someone else has
// advanced is left alone.
func DeleteRemoteBranch(ctx context.Context, req BranchDeletion) error {
	if IsDefaultBranch(ctx, req.RepoPath, req.Branch) {
		return fmt.Errorf("refusing to delete remote main branch %q", req.Branch)
	}
	remote := strings.TrimSpace(req.Remote)
	if remote == "" {
		remote = "origin"
	}
	args := []string{"push"}
	if req.ExpectedOID != "" {
		args = append(args, "--force-with-lease=refs/heads/"+req.Branch+":"+req.ExpectedOID)
	}
	args = append(args, remote, "--delete", req.Branch)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = req.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := string(output)
		if strings.Contains(text, "remote ref does not exist") ||
			strings.Contains(text, "unable to delete") ||
			strings.Contains(text, "couldn't find remote ref") {
			return nil
		}
		return fmt.Errorf("delete remote branch: %s", strings.TrimSpace(text))
	}
	return nil
}

// BranchOID resolves a local branch's tip, so an interactive caller can pin
// the identity it is about to delete before it starts removing things. An
// unresolvable branch yields an empty OID, which means "nothing to pin" to
// DeleteLocalBranch rather than a failure.
func BranchOID(ctx context.Context, repoPath, branch string) string {
	if strings.TrimSpace(branch) == "" {
		return ""
	}
	oid, err := gitOutput(ctx, repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return oid
}

// requireBranchTip refuses when the branch no longer points at the OID the
// caller pinned. An empty expectation means the caller had no tip to pin.
func requireBranchTip(ctx context.Context, repoPath, branch, expectedOID string) error {
	if strings.TrimSpace(expectedOID) == "" {
		return nil
	}
	head, err := gitOutput(ctx, repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("resolve branch %q: %w", branch, err)
	}
	if head != expectedOID {
		return fmt.Errorf("branch %q moved to %s since the operation was confirmed, expected %s",
			branch, shortOID(head), shortOID(expectedOID))
	}
	return nil
}

// RemoteBranchExists reports whether origin carries the branch.
func RemoteBranchExists(ctx context.Context, workDir, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "origin", branch)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

var (
	defaultBranchCache   = make(map[string]string)
	defaultBranchCacheMu sync.RWMutex
)

// IsDefaultBranch reports whether branch is the repository's primary branch.
func IsDefaultBranch(ctx context.Context, workDir, branch string) bool {
	if branch == "" {
		return false
	}
	return branch == DefaultBranch(ctx, workDir)
}

// DefaultBranch detects a repository's default branch: origin's HEAD when it is
// known, then the conventional names, then "main". Answers are cached per
// working directory.
func DefaultBranch(ctx context.Context, workDir string) string {
	return DefaultBranchObserved(ctx, workDir, nil)
}

// DefaultBranchObserved is DefaultBranch with a hook called immediately before
// each git process it spawns. Hosts that count subprocesses (the startup trace)
// pass their recorder here so detection is not under-reported as one spawn.
func DefaultBranchObserved(ctx context.Context, workDir string, onSpawn func()) string {
	if ctx.Err() != nil {
		return ""
	}
	spawn := func() {
		if onSpawn != nil {
			onSpawn()
		}
	}
	defaultBranchCacheMu.RLock()
	if branch, ok := defaultBranchCache[workDir]; ok {
		defaultBranchCacheMu.RUnlock()
		return branch
	}
	defaultBranchCacheMu.RUnlock()

	spawn()
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = workDir
	if output, err := cmd.Output(); err == nil {
		ref := strings.TrimSpace(string(output))
		if branch, found := strings.CutPrefix(ref, "refs/remotes/origin/"); found {
			cacheDefaultBranch(workDir, branch)
			return branch
		}
	}
	if ctx.Err() != nil {
		return ""
	}

	for _, branch := range []string{"main", "master"} {
		if ctx.Err() != nil {
			return ""
		}
		spawn()
		cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branch)
		cmd.Dir = workDir
		if err := cmd.Run(); err == nil {
			cacheDefaultBranch(workDir, branch)
			return branch
		}
	}

	if ctx.Err() != nil {
		return ""
	}
	cacheDefaultBranch(workDir, "main")
	return "main"
}

func cacheDefaultBranch(workDir, branch string) {
	defaultBranchCacheMu.Lock()
	defaultBranchCache[workDir] = branch
	defaultBranchCacheMu.Unlock()
}

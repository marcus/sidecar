package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/workspaceops"
	"github.com/marcus/sidecar/internal/worktreedelete"
)

const (
	worktreeDeleteStatusPlanned = "planned"
	worktreeDeleteStatusDeleted = "deleted"
)

type worktreeDeletePlan struct {
	Project              string `json:"project"`
	Name                 string `json:"name"`
	Path                 string `json:"path"`
	Branch               string `json:"branch"`
	HeadOID              string `json:"headOid"`
	BranchOID            string `json:"branchOid"`
	Dirtiness            string `json:"dirtiness"`
	HasRemoteBranch      bool   `json:"hasRemoteBranch"`
	DeleteLocalBranch    bool   `json:"deleteLocalBranch"`
	DeleteRemoteBranch   bool   `json:"deleteRemoteBranch"`
	PendingCreation      bool   `json:"pendingCreation"`
	pendingCreationPlan  *workspaceops.WorktreePlan
	resolvedWorktreePath string
}

type worktreeDeleteDocument struct {
	Status   string             `json:"status"`
	Deleted  bool               `json:"deleted"`
	Plan     worktreeDeletePlan `json:"plan"`
	Warnings []string           `json:"warnings,omitempty"`
}

type worktreeDeleteErrorDocument struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type worktreeDeletePlanError struct {
	err      error
	code     string
	exitCode int
}

func (e *worktreeDeletePlanError) Error() string { return e.err.Error() }

func runWorktreeRoot(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("worktree")
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(cmd))
		return 0
	}
	sub := cmd.FindSubcommand(args[0])
	if sub != nil && sub.Run != nil {
		return sub.Run(env, args[1:])
	}
	cliErrf(env.Stderr, "unknown worktree command %q\n\n%s", args[0], RenderHelp(cmd))
	return 2
}

func runWorktreeDelete(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("worktree").FindSubcommand("delete")
	help := RenderHelp(cmd)
	usage := newUsageReporter(env, wantsJSON(args), help)

	jsonOutput := false
	projectFlag := ""
	expectHeadOID := ""
	expectBranch := ""
	planOnly := false
	yes := false
	deleteLocal := false
	deleteRemote := false
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--plan" || arg == "--dry-run":
			planOnly = true
		case arg == "--yes":
			yes = true
		case arg == "--delete-local-branch":
			deleteLocal = true
		case arg == "--delete-remote-branch":
			deleteRemote = true
		case arg == "--project" || strings.HasPrefix(arg, "--project="):
			value, next, ok := takeFlagArg(arg, args, i, "--project")
			if !ok || strings.TrimSpace(value) == "" {
				return usage("--project requires a project name")
			}
			projectFlag = value
			i = next
		case arg == "--expect-head-oid" || strings.HasPrefix(arg, "--expect-head-oid="):
			value, next, ok := takeFlagArg(arg, args, i, "--expect-head-oid")
			if !ok || strings.TrimSpace(value) == "" {
				return usage("--expect-head-oid requires a commit OID")
			}
			expectHeadOID = value
			i = next
		case arg == "--expect-branch" || strings.HasPrefix(arg, "--expect-branch="):
			value, next, ok := takeFlagArg(arg, args, i, "--expect-branch")
			if !ok || strings.TrimSpace(value) == "" {
				return usage("--expect-branch requires a branch name")
			}
			expectBranch = value
			i = next
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		default:
			if strings.HasPrefix(arg, "-") {
				return usage("unknown option %q", arg)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		return usage("worktree delete requires exactly one worktree name, branch, or path")
	}
	if planOnly && yes {
		return usage("--yes cannot be combined with --plan or --dry-run")
	}
	if planOnly && (expectHeadOID != "" || expectBranch != "") {
		return usage("--expect-branch and --expect-head-oid apply only when deleting; use the path, branch, and headOid returned by --plan")
	}
	if (expectHeadOID == "") != (expectBranch == "") {
		return usage("--expect-branch and --expect-head-oid must be provided together")
	}
	if expectHeadOID != "" && !filepath.IsAbs(positional[0]) {
		return usage("a planned deletion must use the absolute path returned by --plan as TARGET")
	}
	if !planOnly && !yes {
		return usage("--yes is required to delete a worktree; use --plan or --dry-run to inspect it first")
	}

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	project, err := resolveWorktreeDeleteProject(ctx, env, projectFlag)
	if err != nil {
		return emitWorktreeDeleteError(env, jsonOutput, "project", err.Error(), createDestExitCode(err))
	}
	plan, err := resolveWorktreeDeletePlan(ctx, env, project, positional[0], deleteLocal, deleteRemote)
	if err != nil {
		var planErr *worktreeDeletePlanError
		if errors.As(err, &planErr) {
			return emitWorktreeDeleteError(env, jsonOutput, planErr.code, planErr.Error(), planErr.exitCode)
		}
		return emitWorktreeDeleteError(env, jsonOutput, "refused", err.Error(), exitInputRejected)
	}

	if planOnly {
		doc := worktreeDeleteDocument{Status: worktreeDeleteStatusPlanned, Plan: plan}
		if jsonOutput {
			return writeJSON(env, doc)
		}
		return writeWorktreeDeletePlan(env, plan)
	}
	if expectHeadOID != "" && plan.HeadOID != expectHeadOID {
		return emitWorktreeDeleteError(env, jsonOutput, "identity_changed",
			fmt.Sprintf("worktree HEAD moved since the plan was confirmed: it is now %s, not the expected %s", plan.HeadOID, expectHeadOID),
			exitInputRejected)
	}
	if expectBranch != "" && plan.Branch != expectBranch {
		return emitWorktreeDeleteError(env, jsonOutput, "identity_changed",
			fmt.Sprintf("worktree branch changed since the plan was confirmed: it is now %q, not the expected %q", plan.Branch, expectBranch),
			exitInputRejected)
	}

	warnings := executeWorktreeDeletePlan(ctx, project, plan)
	if warnings.err != nil {
		var identityErr *workspaceops.WorktreeIdentityError
		if errors.As(warnings.err, &identityErr) {
			return emitWorktreeDeleteError(env, jsonOutput, "identity_changed", identityErr.Error(), exitInputRejected)
		}
		return emitWorktreeDeleteError(env, jsonOutput, "delete_failed", warnings.err.Error(), 1)
	}
	doc := worktreeDeleteDocument{Status: worktreeDeleteStatusDeleted, Deleted: true, Plan: plan, Warnings: warnings.items}
	if jsonOutput {
		return writeJSON(env, doc)
	}
	_, _ = fmt.Fprintf(env.Stdout, "Deleted worktree %q at %s.\n", plan.Name, plan.Path)
	for _, warning := range warnings.items {
		_, _ = fmt.Fprintf(env.Stdout, "Warning: %s\n", warning)
	}
	return 0
}

func resolveWorktreeDeleteProject(ctx context.Context, env Env, projectFlag string) (registeredProject, error) {
	if projectFlag != "" {
		projects, err := loadRegisteredProjects(env.StateDir)
		if err != nil {
			return registeredProject{}, err
		}
		return matchProject(env.StateDir, projects, projectFlag, resolveProjectOnly)
	}
	dest, err := resolveCreateDestination(ctx, env.StateDir, "", "", resolveProjectOnly)
	if err != nil {
		return registeredProject{}, err
	}
	return registeredProjectForCreate(env.StateDir, dest)
}

func resolveWorktreeDeletePlan(ctx context.Context, env Env, project registeredProject, target string, deleteLocal, deleteRemote bool) (worktreeDeletePlan, error) {
	if strings.TrimSpace(project.Path) == "" {
		return worktreeDeletePlan{}, fmt.Errorf("project %q has no repository path", project.Key)
	}
	states, err := workspaceops.ListWorktreeStates(ctx, project.Path)
	if err != nil {
		return worktreeDeletePlan{}, &worktreeDeletePlanError{
			err: fmt.Errorf("read worktree inventory for project %q: %w", project.Key, err), code: "inventory", exitCode: 1,
		}
	}
	state, name, err := matchWorktreeDeleteTarget(env.StateDir, project, states, target)
	if err != nil {
		return worktreeDeletePlan{}, err
	}
	mainPath := workspaceops.CanonicalWorktreePath(workspaceops.MainWorktreePath(ctx, project.Path))
	refusal := worktreeDeleteRefusal(state, mainPath)
	if refusal != "" {
		return worktreeDeletePlan{}, errorsLowerFirst(refusal)
	}

	isDefault := workspaceops.IsDefaultBranch(ctx, project.Path, state.Branch)
	hasRemote := false
	if !isDefault {
		hasRemote = workspaceops.RemoteBranchExists(ctx, project.Path, state.Branch)
	}
	if deleteLocal && isDefault {
		return worktreeDeletePlan{}, fmt.Errorf("refusing to delete the repository's default branch %q", state.Branch)
	}
	if deleteRemote && isDefault {
		return worktreeDeletePlan{}, fmt.Errorf("refusing to delete the remote default branch %q", state.Branch)
	}
	if deleteRemote && !hasRemote {
		return worktreeDeletePlan{}, fmt.Errorf("remote branch %q does not exist; omit --delete-remote-branch", state.Branch)
	}
	var confirmation worktreedelete.State
	confirmation.Open(worktreedelete.Target{Name: name, Branch: state.Branch, Path: state.Path}, isDefault)
	confirmation.HasRemote = hasRemote
	confirmation.DeleteLocal = deleteLocal
	confirmation.DeleteRemote = deleteRemote

	plan := worktreeDeletePlan{
		Project: project.Key, Name: name, Path: state.Path, Branch: state.Branch,
		HeadOID: state.HEAD, BranchOID: workspaceops.BranchOID(ctx, project.Path, state.Branch),
		Dirtiness:       dirtinessName(worktreedelete.ProbeDirtiness(ctx, state.Path, false)),
		HasRemoteBranch: hasRemote, DeleteLocalBranch: confirmation.DeleteLocal, DeleteRemoteBranch: confirmation.DeleteRemoteBranch(),
		resolvedWorktreePath: state.Path,
	}
	if _, ok := projectdir.LookupWorktreeWithBase(env.StateDir, project.Path, state.Path); ok {
		repoKey, keyErr := workspaceops.RepoKeyForPath(ctx, project.Path)
		worktreeKey, worktreeKeyErr := projectdir.WorktreeKey(state.Path)
		if keyErr == nil && worktreeKeyErr == nil {
			journal, journalErr := workspaceops.LoadPendingCreation(ctx, project.Path, []workspaceops.WorktreeRecord{{
				Key: worktreeKey, RepoKey: repoKey, Path: state.Path, Branch: state.Branch, HEADOID: state.HEAD,
			}}, repoKey)
			if journalErr != nil {
				return worktreeDeletePlan{}, &worktreeDeletePlanError{
					err: fmt.Errorf("inspect pending worktree creation: %w", journalErr), code: "state", exitCode: 1,
				}
			}
			if journal != nil {
				plan.PendingCreation = true
				plan.pendingCreationPlan = &journal.Plan
			}
		}
	}
	return plan, nil
}

func worktreeDeleteRefusal(state workspaceops.WorktreeState, mainPath string) string {
	return workspaceops.WorktreeActionRefusal(&workspaceops.WorktreeActionState{
		Path: state.Path, Branch: state.Branch,
		IsMain: workspaceops.CanonicalWorktreePath(state.Path) == mainPath,
		IsBare: state.Bare, IsDetached: state.Detached, IsLocked: state.Locked,
		IsMissing: !directoryExists(state.Path), IsPrunable: state.Prunable,
		TrustPath: true,
	}, workspaceops.WorktreeActionDelete)
}

func matchWorktreeDeleteTarget(stateDir string, project registeredProject, states []workspaceops.WorktreeState, target string) (workspaceops.WorktreeState, string, error) {
	want := strings.TrimSpace(target)
	if want == "" {
		return workspaceops.WorktreeState{}, "", fmt.Errorf("worktree target is empty")
	}
	wantPath := ""
	if filepath.IsAbs(want) || strings.ContainsRune(want, filepath.Separator) || strings.HasPrefix(want, ".") {
		if abs, err := filepath.Abs(want); err == nil {
			wantPath = workspaceops.CanonicalWorktreePath(abs)
		}
	}

	type candidate struct {
		state workspaceops.WorktreeState
		name  string
	}
	var exactPath, named []candidate
	for _, state := range states {
		name := filepath.Base(state.Path)
		if display, err := workspaceops.LookupWorktreeDisplayName(stateDir, project.Path, state.Path); err == nil {
			name = display
		}
		candidate := candidate{state: state, name: name}
		if wantPath != "" && workspaceops.CanonicalWorktreePath(state.Path) == wantPath {
			exactPath = append(exactPath, candidate)
			continue
		}
		if want == name || want == state.Branch || want == filepath.Base(state.Path) || want == workspaceops.WorktreeSessionName(state.Path, "") {
			named = append(named, candidate)
		}
	}
	hits := exactPath
	if len(hits) == 0 {
		hits = named
	}
	if len(hits) == 0 {
		return workspaceops.WorktreeState{}, "", fmt.Errorf("no worktree named %q belongs to project %q", target, project.Key)
	}
	if len(hits) > 1 {
		paths := make([]string, 0, len(hits))
		for _, hit := range hits {
			paths = append(paths, hit.state.Path)
		}
		return workspaceops.WorktreeState{}, "", fmt.Errorf("worktree target %q is ambiguous in project %q; use one of these paths: %s", target, project.Key, strings.Join(paths, ", "))
	}
	return hits[0].state, hits[0].name, nil
}

type worktreeDeleteWarnings struct {
	items []string
	err   error
}

func executeWorktreeDeletePlan(ctx context.Context, project registeredProject, plan worktreeDeletePlan) worktreeDeleteWarnings {
	var warnings []string
	if err := workspaceops.DeleteWorktree(ctx, workspaceops.WorktreeRemoval{
		RepoPath: project.Path, ProjectRoot: project.Path, Path: plan.resolvedWorktreePath,
		Branch: plan.Branch, ExpectedOID: plan.HeadOID, Force: true,
	}); err != nil {
		var removedWarning *workspaceops.WorktreeRemovedWarning
		if !errors.As(err, &removedWarning) {
			return worktreeDeleteWarnings{err: err}
		}
		warnings = append(warnings, "shell teardown: "+removedWarning.Error())
	}
	if plan.DeleteLocalBranch {
		if err := workspaceops.DeleteLocalBranch(ctx, workspaceops.BranchDeletion{
			RepoPath: project.Path, Branch: plan.Branch, ExpectedOID: plan.BranchOID, Force: true,
		}); err != nil {
			warnings = append(warnings, "local branch: "+err.Error())
		}
	}
	if plan.DeleteRemoteBranch {
		if err := workspaceops.DeleteRemoteBranch(ctx, workspaceops.BranchDeletion{
			RepoPath: project.Path, Branch: plan.Branch,
		}); err != nil {
			warnings = append(warnings, "remote branch: "+err.Error())
		}
	}
	if plan.pendingCreationPlan != nil {
		if err := workspaceops.RemovePendingCreation(plan.pendingCreationPlan); err != nil {
			warnings = append(warnings, "pending creation journal: "+err.Error())
		}
	}
	return worktreeDeleteWarnings{items: warnings}
}

func writeWorktreeDeletePlan(env Env, plan worktreeDeletePlan) int {
	_, _ = fmt.Fprintln(env.Stdout, "Worktree delete plan")
	_, _ = fmt.Fprintf(env.Stdout, "  Project: %s\n", plan.Project)
	_, _ = fmt.Fprintf(env.Stdout, "  Name: %s\n", plan.Name)
	_, _ = fmt.Fprintf(env.Stdout, "  Branch: %s\n", plan.Branch)
	_, _ = fmt.Fprintf(env.Stdout, "  Path: %s\n", plan.Path)
	_, _ = fmt.Fprintf(env.Stdout, "  HEAD OID: %s\n", plan.HeadOID)
	_, _ = fmt.Fprintf(env.Stdout, "  Uncommitted work: %s\n", plan.Dirtiness)
	_, _ = fmt.Fprintf(env.Stdout, "  Remote branch exists: %t\n", plan.HasRemoteBranch)
	_, _ = fmt.Fprintf(env.Stdout, "  Delete local branch: %t\n", plan.DeleteLocalBranch)
	_, _ = fmt.Fprintf(env.Stdout, "  Delete remote branch: %t\n", plan.DeleteRemoteBranch)
	if plan.PendingCreation {
		_, _ = fmt.Fprintln(env.Stdout, "  Pending creation journal: clear after successful deletion")
	}
	_, _ = fmt.Fprintln(env.Stdout, "No changes made. Re-run with --yes to delete this worktree.")
	return 0
}

func emitWorktreeDeleteError(env Env, jsonOutput bool, code, message string, exitCode int) int {
	if jsonOutput {
		var doc worktreeDeleteErrorDocument
		doc.Error.Code = code
		doc.Error.Message = message
		if err := json.NewEncoder(env.Stderr).Encode(doc); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return exitCode
	}
	cliErrln(env.Stderr, message)
	return exitCode
}

func dirtinessName(d worktreedelete.Dirtiness) string {
	switch d {
	case worktreedelete.DirtinessClean:
		return "clean"
	case worktreedelete.DirtinessDirty:
		return "dirty; uncommitted or untracked work will be lost"
	default:
		return "unknown; Sidecar could not verify whether work will be lost"
	}
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func errorsLowerFirst(message string) error {
	if message == "" {
		return fmt.Errorf("worktree delete is unavailable")
	}
	return fmt.Errorf("%s%s", strings.ToLower(message[:1]), message[1:])
}

package workspace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/tdroot"
)

// WorkDirDeletedMsg signals that the current working directory was deleted.
// This happens when sidecar is running inside a worktree that gets deleted.
type WorkDirDeletedMsg struct {
	MainWorktreePath string
}

// refreshWorktrees returns a command to refresh the worktree list.
func (p *Plugin) refreshWorktrees() tea.Cmd {
	workDir := p.ctx.WorkDir
	// Capture epoch for stale detection on project switch
	epoch := p.ctx.Epoch
	return func() tea.Msg {
		// Check if current WorkDir still exists (may have been a deleted worktree)
		if _, err := os.Stat(workDir); os.IsNotExist(err) {
			// WorkDir was deleted - find main worktree to switch to
			// We need to find the main worktree from a parent directory
			mainPath := findMainWorktreeFromDeleted(workDir)
			if mainPath != "" {
				return WorkDirDeletedMsg{MainWorktreePath: mainPath}
			}
		}

		worktrees, err := p.listWorktrees()
		return RefreshDoneMsg{Epoch: epoch, Worktrees: worktrees, Err: err}
	}
}

// findMainWorktreeFromDeleted finds the main worktree path when the current
// directory has been deleted. It searches parent directories for a git repo
// that owned the deleted worktree by checking .git/worktrees/*/gitdir files.
func findMainWorktreeFromDeleted(deletedPath string) string {
	// Try parent directory first - worktrees are typically siblings of main repo
	parentDir := filepath.Dir(deletedPath)
	if parentDir == deletedPath {
		return "" // reached root
	}

	// Look for directories in parent that are git repos
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return ""
	}

	// Normalize the deleted path for comparison
	normalizedDeleted := filepath.Clean(deletedPath)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidatePath := filepath.Join(parentDir, entry.Name())
		// Skip the deleted directory
		if candidatePath == deletedPath {
			continue
		}

		// Check if this repo's .git/worktrees contains a reference to the deleted path
		// This is more reliable than just checking if it's any git repo
		gitWorktreesDir := filepath.Join(candidatePath, ".git", "worktrees")
		wtEntries, err := os.ReadDir(gitWorktreesDir)
		if err != nil {
			continue // Not a git repo or no worktrees
		}

		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() {
				continue
			}
			gitdirPath := filepath.Join(gitWorktreesDir, wtEntry.Name(), "gitdir")
			content, err := os.ReadFile(gitdirPath)
			if err != nil {
				continue
			}
			// gitdir contains path like "/path/to/worktree/.git\n"
			wtPath := strings.TrimSuffix(strings.TrimSpace(string(content)), "/.git")
			if filepath.Clean(wtPath) == normalizedDeleted {
				// Found the repo that owned this worktree
				return app.GetMainWorktreePath(candidatePath)
			}
		}
	}

	return ""
}

// listWorktrees parses git worktree list --porcelain output.
func (p *Plugin) listWorktrees() ([]*Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = p.ctx.WorkDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	// Get the actual main worktree path (the original repo), not the current workdir
	// This ensures IsMain is set correctly regardless of which worktree we're in
	mainRepoPath := app.GetMainWorktreePath(p.ctx.WorkDir)
	if mainRepoPath == "" {
		mainRepoPath = p.ctx.WorkDir // Fallback if detection fails
	}

	worktrees, err := parseWorktreeList(string(output), mainRepoPath)
	if err != nil {
		return nil, err
	}

	// Detect missing worktrees and auto-prune fully-gone ones
	needsPrune := false
	filtered := make([]*Worktree, 0, len(worktrees))
	for _, wt := range worktrees {
		if wt.IsMain {
			filtered = append(filtered, wt)
			continue
		}

		if _, statErr := os.Stat(wt.Path); os.IsNotExist(statErr) {
			wt.IsMissing = true
		}

		if wt.IsMissing {
			// If branch is also gone, auto-prune and exclude from list
			if !branchExists(p.ctx.WorkDir, wt.Branch) {
				needsPrune = true
				continue // exclude from returned list
			}
			// Branch still exists but folder gone — keep with IsMissing=true
		}

		filtered = append(filtered, wt)
	}

	if needsPrune {
		_ = doWorktreePrune(p.ctx.WorkDir)
	}

	return filtered, nil
}

// parseWorktreeList parses porcelain format output.
func parseWorktreeList(output, mainWorkdir string) ([]*Worktree, error) {
	var worktrees []*Worktree
	var current *Worktree
	var mainWorktree *Worktree // Track main worktree to prepend later

	// Parent directory of main workdir - worktrees are created as siblings
	parentDir := filepath.Dir(mainWorkdir)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "worktree ") {
			if current != nil {
				if current.IsMain {
					mainWorktree = current
				} else {
					worktrees = append(worktrees, current)
				}
			}
			path := strings.TrimPrefix(line, "worktree ")
			// Mark main worktree (where git repo lives) with IsMain flag
			isMain := path == mainWorkdir
			// Derive name as relative path from parent dir, not just basename.
			// This handles nested worktree directories (e.g., repo-prefix/branch-name)
			// which are created when the branch name contains '/'.
			name := filepath.Base(path)
			if !isMain {
				if relPath, err := filepath.Rel(parentDir, path); err == nil && relPath != "" {
					name = relPath
				}
			}
			current = &Worktree{
				Name:      name,
				Path:      path,
				Status:    StatusPaused,
				CreatedAt: time.Now(), // Will be updated from file stat
				IsMain:    isMain,
			}
		} else if current != nil {
			if strings.HasPrefix(line, "HEAD ") {
				// HEAD commit hash - not storing currently
			} else if strings.HasPrefix(line, "branch ") {
				branch := strings.TrimPrefix(line, "branch refs/heads/")
				current.Branch = branch
			} else if line == "bare" {
				// Bare worktree
			} else if line == "detached" {
				current.Branch = "(detached)"
			} else if strings.HasPrefix(line, "prunable ") {
				current.IsMissing = true
			}
		}
	}

	if current != nil {
		if current.IsMain {
			mainWorktree = current
		} else {
			worktrees = append(worktrees, current)
		}
	}

	// Prepend main worktree to the list so it appears first
	if mainWorktree != nil {
		worktrees = append([]*Worktree{mainWorktree}, worktrees...)
	}

	return worktrees, scanner.Err()
}

// createWorktree returns a command to create a new worktree.
func (p *Plugin) createWorktree() tea.Cmd {
	name := p.createNameInput.Value()
	baseBranch := p.createBaseBranchInput.Value()
	taskID := p.createTaskID
	taskTitle := p.createTaskTitle
	agentType := p.createAgentType
	skipPerms := p.createSkipPermissions
	prompt := p.getSelectedPrompt()

	// Debug log to trace taskID flow
	if p.ctx != nil && p.ctx.Logger != nil {
		p.ctx.Logger.Debug("createWorktree: captured modal state", "name", name, "taskID", taskID, "taskTitle", taskTitle, "agentType", agentType, "skipPerms", skipPerms, "hasPrompt", prompt != nil)
	}

	if name == "" {
		return func() tea.Msg {
			return CreateDoneMsg{Err: fmt.Errorf("workspace name is required")}
		}
	}

	return func() tea.Msg {
		wt, err := p.doCreateWorktree(name, baseBranch, taskID, taskTitle, agentType)
		return CreateDoneMsg{Worktree: wt, AgentType: agentType, SkipPerms: skipPerms, Prompt: prompt, Err: err}
	}
}

// doCreateWorktree performs the actual worktree creation.
func (p *Plugin) doCreateWorktree(name, baseBranch, taskID, taskTitle string, agentType AgentType) (*Worktree, error) {
	// Default base branch to current branch if not specified
	if baseBranch == "" {
		baseBranch = "HEAD"
	}

	// Determine worktree directory name with optional repo prefix
	// When enabled, prefixes directory with repo name (e.g., "myrepo-feature-auth")
	// This helps conversation adapters discover related worktree conversations
	// by matching the directory path pattern after worktree deletion
	dirName := name
	if p.ctx.Config != nil && p.ctx.Config.Plugins.Workspace.DirPrefix {
		repoName := app.GetRepoName(p.ctx.WorkDir)
		if repoName != "" {
			dirName = repoName + "-" + name
		}
	}

	// Determine worktree path (sibling to main repo)
	parentDir := filepath.Dir(p.ctx.WorkDir)
	wtPath := filepath.Join(parentDir, dirName)

	// Create worktree with new branch (branch name stays simple, just the user-provided name)
	args := []string{"worktree", "add", "-b", name, wtPath, baseBranch}
	cmd := exec.Command("git", args...)
	cmd.Dir = p.ctx.WorkDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(output)), err)
	}

	// Create .td-root file pointing to main repo for td database sharing
	if err := p.setupTDRoot(wtPath); err != nil {
		// Log but don't fail - td integration is optional
		p.ctx.Logger.Warn("failed to setup .td-root", "path", wtPath, "error", err)
	}

	// If task is linked, create .sidecar-task file and start the task
	if taskID != "" {
		taskPath := filepath.Join(wtPath, sidecarTaskFile)
		if err := os.WriteFile(taskPath, []byte(taskID+"\n"), 0644); err != nil {
			p.ctx.Logger.Warn("failed to write .sidecar-task", "path", taskPath, "error", err)
		}

		// Auto-start the task in td (if td is available)
		startCmd := exec.Command("td", "start", taskID)
		startCmd.Dir = wtPath
		if err := startCmd.Run(); err != nil {
			p.ctx.Logger.Warn("failed to start td task", "task", taskID, "error", err)
		}
	}

	// Determine actual base branch name
	actualBase := baseBranch
	if baseBranch == "HEAD" {
		if b, err := getCurrentBranch(p.ctx.WorkDir); err == nil {
			actualBase = b
		}
	}

	wt := &Worktree{
		Name:            dirName,
		Path:            wtPath,
		Branch:          name,
		BaseBranch:      actualBase,
		TaskID:          taskID,
		TaskTitle:       taskTitle,
		ChosenAgentType: agentType,
		Status:          StatusPaused,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Persist agent type to .sidecar-agent file
	if err := saveAgentType(wtPath, agentType); err != nil {
		p.ctx.Logger.Warn("failed to save agent type", "path", wtPath, "error", err)
	}

	// Persist base branch to .sidecar-base file
	if err := saveBaseBranch(wtPath, actualBase); err != nil {
		p.ctx.Logger.Warn("failed to save base branch", "path", wtPath, "error", err)
	}

	// Run post-creation setup (env files, symlinks, setup script)
	if err := p.setupWorktree(wtPath, name); err != nil {
		p.ctx.Logger.Warn("workspace setup had errors", "path", wtPath, "error", err)
		// Don't fail creation for setup errors
	}

	return wt, nil
}

// doDeleteWorktree removes a worktree. When isMissing is true, uses prune
// instead of remove since the directory no longer exists on disk.
func doDeleteWorktree(workDir, path string, isMissing bool) error {
	if isMissing {
		return doWorktreePrune(workDir)
	}

	// First try without force
	cmd := exec.Command("git", "worktree", "remove", path)
	cmd.Dir = workDir
	if err := cmd.Run(); err == nil {
		return nil
	}

	// If that fails, try with force
	cmd = exec.Command("git", "worktree", "remove", "--force", path)
	cmd.Dir = workDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// pushSelected returns a command to push the selected worktree's branch.
func (p *Plugin) pushSelected() tea.Cmd {
	wt := p.selectedWorktree()
	if wt == nil {
		return nil
	}
	name := wt.Name
	path := wt.Path
	branch := wt.Branch

	return func() tea.Msg {
		err := doPush(path, branch, false, true)
		return PushDoneMsg{WorkspaceName: name, Err: err}
	}
}

// doPush pushes a branch to remote.
func doPush(workdir, branch string, force, setUpstream bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force-with-lease")
	}
	if setUpstream {
		args = append(args, "-u", "origin", branch)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// getCurrentBranch returns the current branch name.
func getCurrentBranch(workdir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// checkRemoteBranchExists checks if a remote branch exists for the given branch name.
func checkRemoteBranchExists(workdir, branch string) bool {
	cmd := exec.Command("git", "ls-remote", "--heads", "origin", branch)
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// branchExists checks if a local branch exists using git rev-parse.
func branchExists(workdir, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
	cmd.Dir = workdir
	return cmd.Run() == nil
}

// doWorktreePrune runs git worktree prune to clean up stale worktree references.
func doWorktreePrune(workDir string) error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = workDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// isMainBranch returns true if the given branch is the repository's primary branch
// (e.g., main, master). This is used as a universal guard to prevent accidental
// deletion of the main branch.
func isMainBranch(workdir, branch string) bool {
	return branch == detectDefaultBranch(workdir)
}

// deleteBranch deletes a local branch, trying safe delete first then force.
func deleteBranch(workdir, branch string) error {
	if isMainBranch(workdir, branch) {
		return fmt.Errorf("refusing to delete main branch %q", branch)
	}
	// Try safe delete first
	cmd := exec.Command("git", "branch", "-d", branch)
	cmd.Dir = workdir
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try force delete
	cmd = exec.Command("git", "branch", "-D", branch)
	cmd.Dir = workdir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("delete branch: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// deleteRemoteBranchCmd deletes the remote branch from origin.
func deleteRemoteBranchCmd(workdir, branch string) error {
	if isMainBranch(workdir, branch) {
		return fmt.Errorf("refusing to delete remote main branch %q", branch)
	}
	cmd := exec.Command("git", "push", "origin", "--delete", branch)
	cmd.Dir = workdir
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Check if branch was already deleted (GitHub auto-delete)
		if strings.Contains(outputStr, "remote ref does not exist") ||
			strings.Contains(outputStr, "unable to delete") ||
			strings.Contains(outputStr, "couldn't find remote ref") {
			return nil // Not an error - branch already gone
		}
		return fmt.Errorf("delete remote branch: %s", strings.TrimSpace(outputStr))
	}
	return nil
}

// checkRemoteBranch returns a command to check if a remote branch exists.
func (p *Plugin) checkRemoteBranch(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		exists := checkRemoteBranchExists(p.ctx.WorkDir, wt.Branch)
		return RemoteCheckDoneMsg{
			WorkspaceName: wt.Name,
			Branch:        wt.Branch,
			Exists:        exists,
		}
	}
}

// loadBranches returns a command to fetch all local branches.
func (p *Plugin) loadBranches() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "branch", "--format=%(refname:short)")
		cmd.Dir = p.ctx.WorkDir
		output, err := cmd.Output()
		if err != nil {
			return BranchListMsg{Err: fmt.Errorf("git branch: %w", err)}
		}

		var branches []string
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line != "" {
				branches = append(branches, line)
			}
		}
		return BranchListMsg{Branches: branches}
	}
}

// filterBranches filters branches based on a search query.
func filterBranches(query string, allBranches []string) []string {
	if query == "" {
		return allBranches
	}

	query = strings.ToLower(query)
	var matches []string
	for _, branch := range allBranches {
		if strings.Contains(strings.ToLower(branch), query) {
			matches = append(matches, branch)
		}
	}
	return matches
}

// setupTDRoot creates a .td-root file in the worktree pointing to the main repo.
// This allows td commands in the worktree to use the main repo's database.
func (p *Plugin) setupTDRoot(worktreePath string) error {
	mainPath := app.GetMainWorktreePath(p.ctx.WorkDir)
	if mainPath == "" {
		mainPath = p.ctx.WorkDir
	}
	return tdroot.CreateTDRoot(worktreePath, mainPath)
}

const sidecarTaskFile = ".sidecar-task"
const sidecarAgentFile = ".sidecar-agent"
const sidecarPRFile = ".sidecar-pr"
const sidecarBaseFile = ".sidecar-base"

// saveBaseBranch persists the base branch to the worktree.
func saveBaseBranch(worktreePath string, branch string) error {
	if branch == "" {
		basePath := filepath.Join(worktreePath, sidecarBaseFile)
		_ = os.Remove(basePath)
		return nil
	}
	basePath := filepath.Join(worktreePath, sidecarBaseFile)
	return os.WriteFile(basePath, []byte(branch+"\n"), 0644)
}

// loadBaseBranch reads the base branch from the worktree.
func loadBaseBranch(worktreePath string) string {
	basePath := filepath.Join(worktreePath, sidecarBaseFile)
	content, err := os.ReadFile(basePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// loadTaskLink reads the linked task ID from the .sidecar-task file.
func loadTaskLink(worktreePath string) string {
	taskPath := filepath.Join(worktreePath, sidecarTaskFile)
	content, err := os.ReadFile(taskPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// saveAgentType persists the chosen agent type to the worktree.
func saveAgentType(worktreePath string, agentType AgentType) error {
	if agentType == AgentNone || agentType == "" {
		// Remove file if None selected
		agentPath := filepath.Join(worktreePath, sidecarAgentFile)
		_ = os.Remove(agentPath)
		return nil
	}
	agentPath := filepath.Join(worktreePath, sidecarAgentFile)
	return os.WriteFile(agentPath, []byte(string(agentType)+"\n"), 0644)
}

// loadAgentType reads the chosen agent type from the worktree.
func loadAgentType(worktreePath string) AgentType {
	agentPath := filepath.Join(worktreePath, sidecarAgentFile)
	content, err := os.ReadFile(agentPath)
	if err != nil {
		return AgentNone
	}
	return AgentType(strings.TrimSpace(string(content)))
}

// savePRURL persists the PR URL to the worktree.
func savePRURL(worktreePath string, prURL string) error {
	if prURL == "" {
		// Remove file if empty
		prPath := filepath.Join(worktreePath, sidecarPRFile)
		_ = os.Remove(prPath)
		return nil
	}
	prPath := filepath.Join(worktreePath, sidecarPRFile)
	return os.WriteFile(prPath, []byte(prURL+"\n"), 0644)
}

// loadPRURL reads the PR URL from the worktree.
func loadPRURL(worktreePath string) string {
	prPath := filepath.Join(worktreePath, sidecarPRFile)
	content, err := os.ReadFile(prPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// linkTask returns a command to link a td task to a worktree.
func (p *Plugin) linkTask(wt *Worktree, taskID string) tea.Cmd {
	return func() tea.Msg {
		// Validate task exists by running td show
		cmd := exec.Command("td", "show", taskID)
		cmd.Dir = p.ctx.WorkDir
		if err := cmd.Run(); err != nil {
			return TaskLinkedMsg{
				WorkspaceName: wt.Name,
				Err:           fmt.Errorf("task not found: %s", taskID),
			}
		}

		// Write task link file
		taskPath := filepath.Join(wt.Path, sidecarTaskFile)
		if err := os.WriteFile(taskPath, []byte(taskID+"\n"), 0644); err != nil {
			return TaskLinkedMsg{
				WorkspaceName: wt.Name,
				Err:           fmt.Errorf("write .sidecar-task: %w", err),
			}
		}

		return TaskLinkedMsg{
			WorkspaceName: wt.Name,
			TaskID:        taskID,
		}
	}
}

// unlinkTask returns a command to unlink a td task from a worktree.
func (p *Plugin) unlinkTask(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		taskPath := filepath.Join(wt.Path, sidecarTaskFile)
		if err := os.Remove(taskPath); err != nil && !os.IsNotExist(err) {
			return TaskLinkedMsg{
				WorkspaceName: wt.Name,
				Err:           fmt.Errorf("remove .sidecar-task: %w", err),
			}
		}

		return TaskLinkedMsg{
			WorkspaceName: wt.Name,
			TaskID:        "", // Empty means unlinked
		}
	}
}

// loadOpenTasks fetches all non-closed tasks from td.
func (p *Plugin) loadOpenTasks() tea.Cmd {
	return func() tea.Msg {
		// Use --limit 500 to fetch more items (td defaults to 50)
		// Include all statuses except closed so users can link tasks in_review, etc.
		cmd := exec.Command("td", "list", "--json", "--status", "open,in_progress,in_review", "--limit", "500")
		cmd.Dir = p.ctx.WorkDir
		output, err := cmd.Output()
		if err != nil {
			return TaskSearchResultsMsg{Err: fmt.Errorf("td list: %w", err)}
		}

		tasks, err := parseTDJSON(output)
		return TaskSearchResultsMsg{Tasks: tasks, Err: err}
	}
}

// parseTDJSON parses JSON output from td list command.
func parseTDJSON(data []byte) ([]Task, error) {
	// Handle empty response
	if len(data) == 0 {
		return []Task{}, nil
	}

	// td outputs a JSON array of issues
	type tdIssue struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Description string `json:"description"`
		Type        string `json:"type"`
		ParentID    string `json:"parent_id"`
	}

	var issues []tdIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return nil, fmt.Errorf("parse td json: %w", err)
	}

	// Build map of epic IDs to titles for lookup
	epicTitles := make(map[string]string)
	for _, issue := range issues {
		if issue.Type == "epic" {
			epicTitles[issue.ID] = issue.Title
		}
	}

	tasks := make([]Task, len(issues))
	for i, issue := range issues {
		tasks[i] = Task{
			ID:          issue.ID,
			Title:       issue.Title,
			Status:      issue.Status,
			Description: issue.Description,
			EpicTitle:   epicTitles[issue.ParentID], // Populate epic title if task has parent
		}
	}
	return tasks, nil
}

// filterTasks filters tasks using fuzzy matching and returns results sorted by relevance.
// Scores against Title (3x weight), ID (2x), and EpicTitle (1x).
// When query is empty, returns all tasks unmodified.
func filterTasks(query string, allTasks []Task) []Task {
	if query == "" {
		return allTasks
	}

	type scoredTask struct {
		task  Task
		score int
	}

	var scored []scoredTask

	for _, task := range allTasks {
		titleScore, _ := palette.FuzzyMatch(query, task.Title)
		idScore, _ := palette.FuzzyMatch(query, task.ID)
		epicScore, _ := palette.FuzzyMatch(query, task.EpicTitle)

		total := titleScore*3 + idScore*2 + epicScore

		if total > 0 {
			scored = append(scored, scoredTask{task: task, score: total})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]Task, len(scored))
	for i, s := range scored {
		result[i] = s.task
	}

	return result
}

// ValidateBranchName validates a git branch name and returns validation state.
// Returns: (valid, errors, sanitized suggestion)
// Based on git-check-ref-format rules.
func ValidateBranchName(name string) (bool, []string, string) {
	var errors []string

	if name == "" {
		return false, []string{}, ""
	}

	// Invalid characters in git branch names
	invalidChars := []string{" ", "~", "^", ":", "?", "*", "[", "\\", "@{"}
	for _, char := range invalidChars {
		if strings.Contains(name, char) {
			errors = append(errors, fmt.Sprintf("contains '%s'", char))
		}
	}

	// Cannot start with dash, dot, or slash
	if strings.HasPrefix(name, "-") {
		errors = append(errors, "starts with '-'")
	}
	if strings.HasPrefix(name, ".") {
		errors = append(errors, "starts with '.'")
	}
	if strings.HasPrefix(name, "/") {
		errors = append(errors, "starts with '/'")
	}

	// Cannot end with .lock
	if strings.HasSuffix(name, ".lock") {
		errors = append(errors, "ends with '.lock'")
	}

	// Cannot contain consecutive dots
	if strings.Contains(name, "..") {
		errors = append(errors, "contains '..'")
	}

	// Cannot end with dot
	if strings.HasSuffix(name, ".") {
		errors = append(errors, "ends with '.'")
	}

	// Cannot end with slash
	if strings.HasSuffix(name, "/") {
		errors = append(errors, "ends with '/'")
	}

	// Cannot contain double slash
	if strings.Contains(name, "//") {
		errors = append(errors, "contains '//'")
	}

	// Cannot contain slash followed by dot (e.g., "feature/.hidden")
	if strings.Contains(name, "/.") {
		errors = append(errors, "contains '/.'")
	}

	// Cannot be exactly "@"
	if name == "@" {
		errors = append(errors, "cannot be '@'")
	}

	// Cannot contain control characters (ASCII < 32) or DEL (ASCII 127)
	for _, r := range name {
		if r < 32 || r == 127 {
			errors = append(errors, "contains control character")
			break
		}
	}

	// Generate sanitized suggestion
	sanitized := SanitizeBranchName(name)

	return len(errors) == 0, errors, sanitized
}

// SanitizeBranchName converts a string to a valid git branch name.
// The output should always pass ValidateBranchName.
func SanitizeBranchName(name string) string {
	// Replace spaces with dashes
	result := strings.ReplaceAll(name, " ", "-")

	// Remove invalid characters
	invalidChars := []string{"~", "^", ":", "?", "*", "[", "\\", "@{"}
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "")
	}

	// Remove control characters (ASCII < 32 and DEL 127)
	var cleaned strings.Builder
	for _, r := range result {
		if r >= 32 && r != 127 {
			cleaned.WriteRune(r)
		}
	}
	result = cleaned.String()

	// Remove leading dashes, dots, and slashes
	for len(result) > 0 && (result[0] == '-' || result[0] == '.' || result[0] == '/') {
		result = result[1:]
	}

	// Collapse consecutive dots to single dot
	for strings.Contains(result, "..") {
		result = strings.ReplaceAll(result, "..", ".")
	}

	// Collapse double slashes to single slash
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}

	// Remove /. sequences (slash followed by dot)
	result = strings.ReplaceAll(result, "/.", "/")

	// Remove trailing .lock
	result = strings.TrimSuffix(result, ".lock")

	// Remove trailing dots, slashes, and dashes
	for len(result) > 0 {
		last := result[len(result)-1]
		if last == '.' || last == '/' || last == '-' {
			result = result[:len(result)-1]
		} else {
			break
		}
	}

	// Final check: remove .lock suffix if exposed by previous cleanup steps
	// (e.g., "foo.lock-" -> "foo.lock" after dash trim -> "foo")
	for strings.HasSuffix(result, ".lock") {
		result = strings.TrimSuffix(result, ".lock")
	}

	// Handle special case of "@"
	if result == "@" {
		result = ""
	}

	// Convert to lowercase (common convention)
	result = strings.ToLower(result)

	return result
}

// loadTaskDetails fetches full task details from td.
func (p *Plugin) loadTaskDetails(taskID string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("td", "show", taskID, "--json")
		cmd.Dir = p.ctx.WorkDir
		output, err := cmd.Output()
		if err != nil {
			return TaskDetailsLoadedMsg{TaskID: taskID, Err: fmt.Errorf("td show: %w", err)}
		}

		var details struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Status      string `json:"status"`
			Priority    string `json:"priority"`
			Type        string `json:"type"`
			Description string `json:"description"`
			Acceptance  string `json:"acceptance"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
		}

		if err := json.Unmarshal(output, &details); err != nil {
			return TaskDetailsLoadedMsg{TaskID: taskID, Err: fmt.Errorf("parse task json: %w", err)}
		}

		return TaskDetailsLoadedMsg{
			TaskID: taskID,
			Details: &TaskDetails{
				ID:          details.ID,
				Title:       details.Title,
				Status:      details.Status,
				Priority:    details.Priority,
				Type:        details.Type,
				Description: details.Description,
				Acceptance:  details.Acceptance,
				CreatedAt:   details.CreatedAt,
				UpdatedAt:   details.UpdatedAt,
			},
		}
	}
}

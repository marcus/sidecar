package worktree

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshWorktrees returns a command to refresh the worktree list.
func (p *Plugin) refreshWorktrees() tea.Cmd {
	return func() tea.Msg {
		worktrees, err := p.listWorktrees()
		return RefreshDoneMsg{Worktrees: worktrees, Err: err}
	}
}

// listWorktrees parses git worktree list --porcelain output.
func (p *Plugin) listWorktrees() ([]*Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = p.ctx.WorkDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	return parseWorktreeList(string(output), p.ctx.WorkDir)
}

// parseWorktreeList parses porcelain format output.
func parseWorktreeList(output, mainWorkdir string) ([]*Worktree, error) {
	var worktrees []*Worktree
	var current *Worktree

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "worktree ") {
			if current != nil {
				worktrees = append(worktrees, current)
			}
			path := strings.TrimPrefix(line, "worktree ")
			name := filepath.Base(path)
			// Skip main worktree (where git repo lives)
			if path == mainWorkdir {
				current = nil
				continue
			}
			current = &Worktree{
				Name:      name,
				Path:      path,
				Status:    StatusPaused,
				CreatedAt: time.Now(), // Will be updated from file stat
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
			}
		}
	}

	if current != nil {
		worktrees = append(worktrees, current)
	}

	return worktrees, scanner.Err()
}

// createWorktree returns a command to create a new worktree.
func (p *Plugin) createWorktree() tea.Cmd {
	name := p.createNameInput.Value()
	baseBranch := p.createBaseBranchInput.Value()
	taskID := p.createTaskID
	agentType := p.createAgentType
	skipPerms := p.createSkipPermissions

	if name == "" {
		return func() tea.Msg {
			return CreateDoneMsg{Err: fmt.Errorf("worktree name is required")}
		}
	}

	return func() tea.Msg {
		wt, err := p.doCreateWorktree(name, baseBranch, taskID, agentType)
		return CreateDoneMsg{Worktree: wt, AgentType: agentType, SkipPerms: skipPerms, Err: err}
	}
}

// doCreateWorktree performs the actual worktree creation.
func (p *Plugin) doCreateWorktree(name, baseBranch, taskID string, agentType AgentType) (*Worktree, error) {
	// Default base branch to current branch if not specified
	if baseBranch == "" {
		baseBranch = "HEAD"
	}

	// Determine worktree path (sibling to main repo)
	parentDir := filepath.Dir(p.ctx.WorkDir)
	wtPath := filepath.Join(parentDir, name)

	// Create worktree with new branch
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
		Name:            name,
		Path:            wtPath,
		Branch:          name,
		BaseBranch:      actualBase,
		TaskID:          taskID,
		ChosenAgentType: agentType,
		Status:          StatusPaused,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Persist agent type to .sidecar-agent file
	if err := saveAgentType(wtPath, agentType); err != nil {
		p.ctx.Logger.Warn("failed to save agent type", "path", wtPath, "error", err)
	}

	// Run post-creation setup (env files, symlinks, setup script)
	if err := p.setupWorktree(wtPath, name); err != nil {
		p.ctx.Logger.Warn("worktree setup had errors", "path", wtPath, "error", err)
		// Don't fail creation for setup errors
	}

	return wt, nil
}

// doDeleteWorktree removes a worktree.
func doDeleteWorktree(path string) error {
	// First try without force
	cmd := exec.Command("git", "worktree", "remove", path)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// If that fails, try with force
	cmd = exec.Command("git", "worktree", "remove", "--force", path)
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
		return PushDoneMsg{WorktreeName: name, Err: err}
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
	tdRootPath := filepath.Join(worktreePath, ".td-root")
	return os.WriteFile(tdRootPath, []byte(p.ctx.WorkDir+"\n"), 0644)
}

const sidecarTaskFile = ".sidecar-task"
const sidecarAgentFile = ".sidecar-agent"

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
		os.Remove(agentPath) // Ignore error
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

// linkTask returns a command to link a td task to a worktree.
func (p *Plugin) linkTask(wt *Worktree, taskID string) tea.Cmd {
	return func() tea.Msg {
		// Validate task exists by running td show
		cmd := exec.Command("td", "show", taskID)
		cmd.Dir = p.ctx.WorkDir
		if err := cmd.Run(); err != nil {
			return TaskLinkedMsg{
				WorktreeName: wt.Name,
				Err:          fmt.Errorf("task not found: %s", taskID),
			}
		}

		// Write task link file
		taskPath := filepath.Join(wt.Path, sidecarTaskFile)
		if err := os.WriteFile(taskPath, []byte(taskID+"\n"), 0644); err != nil {
			return TaskLinkedMsg{
				WorktreeName: wt.Name,
				Err:          fmt.Errorf("write .sidecar-task: %w", err),
			}
		}

		return TaskLinkedMsg{
			WorktreeName: wt.Name,
			TaskID:       taskID,
		}
	}
}

// unlinkTask returns a command to unlink a td task from a worktree.
func (p *Plugin) unlinkTask(wt *Worktree) tea.Cmd {
	return func() tea.Msg {
		taskPath := filepath.Join(wt.Path, sidecarTaskFile)
		if err := os.Remove(taskPath); err != nil && !os.IsNotExist(err) {
			return TaskLinkedMsg{
				WorktreeName: wt.Name,
				Err:          fmt.Errorf("remove .sidecar-task: %w", err),
			}
		}

		return TaskLinkedMsg{
			WorktreeName: wt.Name,
			TaskID:       "", // Empty means unlinked
		}
	}
}

// loadOpenTasks fetches all open/in_progress tasks from td.
func (p *Plugin) loadOpenTasks() tea.Cmd {
	return func() tea.Msg {
		// Use --limit 500 to fetch more items (td defaults to 50)
		cmd := exec.Command("td", "list", "--json", "--status", "open,in_progress", "--limit", "500")
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

// filterTasks filters tasks based on a search query.
func filterTasks(query string, allTasks []Task) []Task {
	if query == "" {
		return allTasks
	}

	query = strings.ToLower(query)
	var matches []Task

	for _, task := range allTasks {
		// Match on title, ID, or parent epic title
		if strings.Contains(strings.ToLower(task.Title), query) ||
			strings.Contains(strings.ToLower(task.ID), query) ||
			strings.Contains(strings.ToLower(task.EpicTitle), query) {
			matches = append(matches, task)
		}
	}

	return matches
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

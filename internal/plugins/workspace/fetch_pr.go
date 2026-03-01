package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/app"
)

// fetchPRList runs gh pr list and returns open PRs.
func (p *Plugin) fetchPRList() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "list",
			"--json", "number,title,headRefName,url,isDraft,createdAt,author",
			"--limit", "30",
		)
		cmd.Dir = workDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg == "" {
				errMsg = err.Error()
			}
			return FetchPRListMsg{Err: fmt.Errorf("gh pr list: %s", errMsg)}
		}

		var prs []PRListItem
		if err := json.Unmarshal(output, &prs); err != nil {
			return FetchPRListMsg{Err: fmt.Errorf("parse pr list: %w", err)}
		}

		return FetchPRListMsg{PRs: prs}
	}
}

// fetchAndCreateWorktree fetches a PR branch and creates a worktree from it.
func (p *Plugin) fetchAndCreateWorktree(pr PRListItem) tea.Cmd {
	workDir := p.ctx.WorkDir
	projectRoot := p.ctx.ProjectRoot
	dirPrefix := p.ctx.Config != nil && p.ctx.Config.Plugins.Workspace.DirPrefix

	return func() tea.Msg {
		branch := pr.Branch

		// Fetch the remote branch
		fetchCmd := exec.Command("git", "fetch", "origin", branch)
		fetchCmd.Dir = workDir
		if output, err := fetchCmd.CombinedOutput(); err != nil {
			return FetchPRDoneMsg{Err: fmt.Errorf("git fetch: %s", strings.TrimSpace(string(output)))}
		}

		// Determine worktree path
		dirName := branch
		if dirPrefix {
			repoName := app.GetRepoName(workDir)
			if repoName != "" {
				dirName = repoName + "-" + branch
			}
		}
		// Use projectRoot (the main worktree) rather than workDir so that
		// starting from a subfolder doesn't place the worktree inside the repo. Fixes #174.
		mainRepoDir := projectRoot
		if mainRepoDir == "" {
			mainRepoDir = workDir
		}
		parentDir := filepath.Dir(mainRepoDir)
		wtPath := filepath.Join(parentDir, dirName)

		// Create worktree tracking the remote branch
		addCmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, "origin/"+branch)
		addCmd.Dir = workDir
		if output, err := addCmd.CombinedOutput(); err != nil {
			outStr := strings.TrimSpace(string(output))
			if strings.Contains(outStr, "already exists") {
				// Branch exists locally. Try creating worktree without -b.
				addCmd2 := exec.Command("git", "worktree", "add", wtPath, branch)
				addCmd2.Dir = workDir
				if output2, err2 := addCmd2.CombinedOutput(); err2 != nil {
					outStr2 := strings.TrimSpace(string(output2))
					// Worktree already checked out — find and focus it
					if strings.Contains(outStr2, "already") {
						existingPath := findWorktreePathForBranch(workDir, branch)
						if existingPath != "" {
							_ = savePRURL(projectRoot, existingPath, pr.URL)
							_ = saveBaseBranch(projectRoot, existingPath, detectDefaultBranch(workDir))
						}
						return FetchPRDoneMsg{AlreadyLocal: true, Branch: branch}
					}
					return FetchPRDoneMsg{Err: fmt.Errorf("git worktree add: %s", outStr2)}
				}
				// Worktree created from existing local branch
				_ = savePRURL(projectRoot, wtPath, pr.URL)
				baseBranch := detectDefaultBranch(workDir)
				_ = saveBaseBranch(projectRoot, wtPath, baseBranch)

				wt := &Worktree{
					Name:       dirName,
					Path:       wtPath,
					Branch:     branch,
					BaseBranch: baseBranch,
					PRURL:      pr.URL,
					Status:     StatusPaused,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
				return FetchPRDoneMsg{Worktree: wt, AlreadyLocal: true}
			}
			return FetchPRDoneMsg{Err: fmt.Errorf("git worktree add: %s", outStr)}
		}

		// Write PR URL to centralized worktree data directory (non-fatal)
		_ = savePRURL(projectRoot, wtPath, pr.URL)

		// Detect base branch for diff
		baseBranch := detectDefaultBranch(workDir)

		// Persist base branch to centralized worktree data directory (non-fatal)
		_ = saveBaseBranch(projectRoot, wtPath, baseBranch)

		wt := &Worktree{
			Name:       dirName,
			Path:       wtPath,
			Branch:     branch,
			BaseBranch: baseBranch,
			PRURL:      pr.URL,
			Status:     StatusPaused,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		return FetchPRDoneMsg{Worktree: wt}
	}
}

// findWorktreePathForBranch returns the worktree path for a branch, or "" if not found.
func findWorktreePathForBranch(workDir, branch string) string {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	var currentPath string
	for line := range strings.SplitSeq(string(output), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			currentPath = p
		} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			if b == branch {
				return currentPath
			}
		}
	}
	return ""
}

// filteredFetchPRItems returns PR items matching the current filter.
func (p *Plugin) filteredFetchPRItems() []PRListItem {
	if p.fetchPRFilter == "" {
		return p.fetchPRItems
	}
	query := strings.ToLower(p.fetchPRFilter)
	var matches []PRListItem
	for _, pr := range p.fetchPRItems {
		if strings.Contains(strings.ToLower(pr.Title), query) ||
			strings.Contains(strings.ToLower(pr.Branch), query) ||
			strings.Contains(strings.ToLower(pr.Author.Login), query) ||
			strings.Contains(fmt.Sprintf("#%d", pr.Number), query) {
			matches = append(matches, pr)
		}
	}
	return matches
}

// adjustFetchPRScroll keeps the cursor visible within the 10-item window.
func (p *Plugin) adjustFetchPRScroll() {
	const maxVisible = 10
	if p.fetchPRCursor < p.fetchPRScrollOffset {
		p.fetchPRScrollOffset = p.fetchPRCursor
	}
	if p.fetchPRCursor >= p.fetchPRScrollOffset+maxVisible {
		p.fetchPRScrollOffset = p.fetchPRCursor - maxVisible + 1
	}
	if p.fetchPRScrollOffset < 0 {
		p.fetchPRScrollOffset = 0
	}
}

// clearFetchPRState resets fetch PR modal state.
func (p *Plugin) clearFetchPRState() {
	p.fetchPRItems = nil
	p.fetchPRFilter = ""
	p.fetchPRCursor = 0
	p.fetchPRScrollOffset = 0
	p.fetchPRLoading = false
	p.fetchPRError = ""
	p.clearFetchPRModal()
}

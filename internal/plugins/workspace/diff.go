package workspace

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// loadSelectedDiff returns a command to load diff for the selected worktree.
// Also loads task details if Task tab is active.
func (p *Plugin) loadSelectedDiff() tea.Cmd {
	wt := p.selectedWorktree()
	if wt == nil {
		return nil
	}

	cmds := []tea.Cmd{p.loadDiff(wt.Path, wt.Name)}

	// Also load task details if Task tab is active
	if p.previewTab == PreviewTabTask && wt.TaskID != "" {
		cmds = append(cmds, p.loadTaskDetailsIfNeeded())
	}

	return tea.Batch(cmds...)
}

// loadDiff returns a command to load diff for a worktree.
func (p *Plugin) loadDiff(path, name string) tea.Cmd {
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	return func() tea.Msg {
		content, raw, err := getDiff(path)
		if err != nil {
			return DiffErrorMsg{WorkspaceName: name, Err: err}
		}
		return DiffLoadedMsg{Epoch: epoch, WorkspaceName: name, Content: content, Raw: raw}
	}
}

// getDiff returns the diff for a worktree.
func getDiff(workdir string) (content, raw string, err error) {
	// Get combined staged and unstaged diff
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		// No HEAD yet, try just staged/unstaged
		cmd = exec.Command("git", "diff")
		cmd.Dir = workdir
		output, _ = cmd.Output()
	}

	raw = string(output)

	// For now, content is same as raw
	// Later can add syntax highlighting or delta processing
	content = raw

	return content, raw, nil
}

// getDiffStatFromBase returns the --stat output compared to the base branch.
func getDiffStatFromBase(workdir, baseBranch string) (string, error) {
	if baseBranch == "" {
		baseBranch = detectDefaultBranch(workdir)
	}

	// Try to find merge-base first
	mbCmd := exec.Command("git", "merge-base", baseBranch, "HEAD")
	mbCmd.Dir = workdir
	mbOutput, err := mbCmd.Output()

	var args []string
	if err == nil {
		mbHash := strings.TrimSpace(string(mbOutput))
		if len(mbHash) >= 40 {
			args = []string{"diff", "--stat", mbHash[:40] + "..HEAD"}
		} else {
			args = []string{"diff", "--stat", baseBranch + "..HEAD"}
		}
	} else {
		args = []string{"diff", "--stat", baseBranch + "..HEAD"}
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// splitLines splits a string into lines, handling various line endings.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// loadCommitStatus returns a command to load commit status for a worktree.
func (p *Plugin) loadCommitStatus(wt *Worktree) tea.Cmd {
	if wt == nil {
		return nil
	}
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	name := wt.Name
	path := wt.Path
	baseBranch := wt.BaseBranch

	return func() tea.Msg {
		commits, err := getWorktreeCommits(path, baseBranch)
		if err != nil {
			return CommitStatusLoadedMsg{Epoch: epoch, WorkspaceName: name, Err: err}
		}
		return CommitStatusLoadedMsg{Epoch: epoch, WorkspaceName: name, Commits: commits}
	}
}

// getWorktreeCommits returns commits unique to this branch vs base branch with status.
func getWorktreeCommits(workdir, baseBranch string) ([]CommitStatusInfo, error) {
	// If baseBranch is empty, detect the default branch
	if baseBranch == "" {
		baseBranch = detectDefaultBranch(workdir)
	}

	// Try to get commits comparing against base branch
	output, err := tryGitLog(workdir, baseBranch)
	if err != nil {
		// Try origin/baseBranch
		output, err = tryGitLog(workdir, "origin/"+baseBranch)
	}
	if err != nil {
		// Last resort: detect default branch fresh (in case baseBranch was stale/wrong)
		detected := detectDefaultBranch(workdir)
		if detected != baseBranch {
			output, err = tryGitLog(workdir, detected)
			if err != nil {
				output, err = tryGitLog(workdir, "origin/"+detected)
			}
		}
	}
	if err != nil {
		// No commits or error - return empty list
		return []CommitStatusInfo{}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return []CommitStatusInfo{}, nil
	}

	// Get remote tracking branch and find unpushed commits in one batch call
	remoteBranch := getRemoteTrackingBranch(workdir)
	unpushed := getUnpushedCommits(workdir, remoteBranch)

	var commits []CommitStatusInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		hash := parts[0]
		subject := parts[1]

		// A commit is pushed if remote branch exists and commit is not in the unpushed set
		pushed := remoteBranch != "" && !unpushed[hash]

		commits = append(commits, CommitStatusInfo{
			Hash:    hash,
			Subject: subject,
			Pushed:  pushed,
		})
	}

	return commits, nil
}

// tryGitLog attempts to get commit log comparing HEAD to a base ref.
func tryGitLog(workdir, baseRef string) ([]byte, error) {
	cmd := exec.Command("git", "log", baseRef+"..HEAD", "--oneline", "--format=%h|%s")
	cmd.Dir = workdir
	return cmd.Output()
}

// detectDefaultBranch detects the default branch for a repository.
// Checks remote HEAD first, then falls back to common names.
var defaultBranchCache = make(map[string]string)

func detectDefaultBranch(workdir string) string {
	if branch, ok := defaultBranchCache[workdir]; ok {
		return branch
	}

	// Try to get the remote HEAD (most reliable)
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err == nil {
		// Output is like "refs/remotes/origin/main"
		ref := strings.TrimSpace(string(output))
		if branch, found := strings.CutPrefix(ref, "refs/remotes/origin/"); found {
			defaultBranchCache[workdir] = branch
			return branch
		}
	}

	// Fallback: check which common branch exists
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "rev-parse", "--verify", branch)
		cmd.Dir = workdir
		if err := cmd.Run(); err == nil {
			defaultBranchCache[workdir] = branch
			return branch
		}
	}

	// Last resort default
	defaultBranchCache[workdir] = "main"
	return "main"
}

// resolveBaseBranch returns the worktree's BaseBranch if set,
// otherwise detects the default branch from the worktree's repo.
func resolveBaseBranch(wt *Worktree) string {
	if wt.BaseBranch != "" {
		return wt.BaseBranch
	}
	return detectDefaultBranch(wt.Path)
}

// getRemoteTrackingBranch returns the remote tracking branch for HEAD.
func getRemoteTrackingBranch(workdir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getUnpushedCommits returns a set of short commit hashes that are in HEAD but not
// in the remote tracking branch. Uses a single git call instead of per-commit checks.
func getUnpushedCommits(workdir, remoteBranch string) map[string]bool {
	if remoteBranch == "" || workdir == "" {
		return nil
	}
	cmd := exec.Command("git", "log", remoteBranch+"..HEAD", "--format=%h")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	result := make(map[string]bool)
	for _, h := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if h != "" {
			result[h] = true
		}
	}
	return result
}

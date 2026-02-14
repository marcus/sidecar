package gitstatus

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// PushStatus represents the push state of the current branch.
type PushStatus struct {
	HasUpstream    bool     // Whether an upstream branch is configured
	UpstreamBranch string   // Name of upstream branch (e.g., "origin/main")
	Ahead          int      // Commits ahead of upstream
	Behind         int      // Commits behind upstream
	UnpushedHashes []string // Hashes of unpushed commits
	DetachedHead   bool     // Whether HEAD is detached
	CurrentBranch  string   // Current branch name (empty if detached)
}

// GetPushStatus retrieves the push status for the current branch.
// Returns a PushStatus struct with information about ahead/behind counts,
// unpushed commits, and upstream configuration.
// Always returns a valid PushStatus (may be minimal if operations fail).
func GetPushStatus(workDir string) *PushStatus {
	status := &PushStatus{}

	// Check if HEAD is detached
	branchCmd := exec.Command("git", "branch", "--show-current")
	branchCmd.Dir = workDir
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return status // Return empty status on error
	}
	status.CurrentBranch = strings.TrimSpace(string(branchOutput))
	status.DetachedHead = status.CurrentBranch == ""

	if status.DetachedHead {
		return status // No push status for detached HEAD
	}

	// Get upstream branch name
	upstreamCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "@{upstream}")
	upstreamCmd.Dir = workDir
	upstreamOutput, err := upstreamCmd.Output()
	if err != nil {
		// No upstream configured - this is not an error, just means
		// the branch has never been pushed or has no tracking branch
		status.HasUpstream = false
		return status
	}
	status.HasUpstream = true
	status.UpstreamBranch = strings.TrimSpace(string(upstreamOutput))

	// Get ahead/behind counts
	// Format: "X\tY" where X is behind, Y is ahead
	countCmd := exec.Command("git", "rev-list", "--count", "--left-right", "@{upstream}...HEAD")
	countCmd.Dir = workDir
	countOutput, err := countCmd.Output()
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(countOutput)), "\t")
		if len(parts) == 2 {
			status.Behind, _ = strconv.Atoi(parts[0])
			status.Ahead, _ = strconv.Atoi(parts[1])
		}
	}

	// Get unpushed commit hashes if we're ahead
	if status.Ahead > 0 {
		// Use upstream..HEAD to get commits that are in HEAD but not in upstream
		logCmd := exec.Command("git", "log", "@{upstream}..HEAD", "--format=%H")
		logCmd.Dir = workDir
		logOutput, err := logCmd.Output()
		if err == nil {
			hashes := strings.Split(strings.TrimSpace(string(logOutput)), "\n")
			for _, hash := range hashes {
				if hash != "" {
					status.UnpushedHashes = append(status.UnpushedHashes, hash)
				}
			}
		}
	}

	return status
}

// IsCommitPushed checks if a commit hash is pushed to the upstream.
// Returns true if the commit is in the upstream branch.
// Hash can be either full (40 chars) or short (7+ chars).
func (ps *PushStatus) IsCommitPushed(hash string) bool {
	if !ps.HasUpstream {
		return false // No upstream means nothing is pushed
	}
	// Check if hash matches any unpushed commit (UnpushedHashes are full hashes)
	for _, unpushed := range ps.UnpushedHashes {
		if strings.HasPrefix(unpushed, hash) {
			return false // This commit is in the unpushed list
		}
	}
	return true // Not in unpushed list means it's pushed
}

// ExecutePush performs a git push operation.
// Returns the output from git and any error encountered.
func ExecutePush(workDir string, force bool) (string, error) {
	args := []string{"push"}
	if force {
		args = append(args, "--force-with-lease")
	}

	// For new branches, set upstream automatically
	remote := GetRemoteName(workDir)
	if remote == "" {
		return "", &PushError{Output: "No remote configured", Err: errors.New("no remote configured")}
	}
	args = append(args, "-u", remote, "HEAD")

	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), &PushError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// PushError wraps a git push error with its output.
type PushError struct {
	Output string
	Err    error
}

func (e *PushError) Error() string {
	return strings.TrimSpace(e.Output)
}

// isPushRejectedError returns true if the push failed because the remote
// contains commits not present locally (non-fast-forward rejection).
func isPushRejectedError(err error) bool {
	var pe *PushError
	if !errors.As(err, &pe) {
		return false
	}
	lower := strings.ToLower(pe.Output)
	return strings.Contains(lower, "rejected") ||
		strings.Contains(lower, "fetch first") ||
		strings.Contains(lower, "the remote contains work that you do")
}

// GetRemoteName returns the primary remote name (usually "origin").
// Returns empty string if no remotes are configured.
func GetRemoteName(workDir string) string {
	cmd := exec.Command("git", "remote")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(remotes) == 0 || remotes[0] == "" {
		return ""
	}
	// Prefer "origin" if it exists
	for _, r := range remotes {
		if r == "origin" {
			return "origin"
		}
	}
	return remotes[0]
}

// HasRemote checks if any remote is configured for the repository.
func HasRemote(workDir string) bool {
	return GetRemoteName(workDir) != ""
}

// FormatAheadBehind returns a formatted string showing ahead/behind status.
// Examples: "↑2", "↓1", "↑2 ↓1", "" (when synced)
func (ps *PushStatus) FormatAheadBehind() string {
	if !ps.HasUpstream {
		if ps.DetachedHead {
			return "detached"
		}
		return "no upstream"
	}

	var parts []string
	if ps.Ahead > 0 {
		parts = append(parts, "↑"+strconv.Itoa(ps.Ahead))
	}
	if ps.Behind > 0 {
		parts = append(parts, "↓"+strconv.Itoa(ps.Behind))
	}
	if len(parts) == 0 {
		return "" // Synced
	}
	return strings.Join(parts, " ")
}

// NeedsForce checks if push would require force (when behind upstream).
func (ps *PushStatus) NeedsForce() bool {
	return ps.Behind > 0 && ps.Ahead > 0
}

// CanPush checks if there are commits that can be pushed.
func (ps *PushStatus) CanPush() bool {
	// Can push if we're ahead of upstream OR if we have no upstream (new branch)
	return ps.Ahead > 0 || (!ps.HasUpstream && !ps.DetachedHead)
}

// ExecutePushForce performs a force push with lease.
// Returns the output from git and any error encountered.
func ExecutePushForce(workDir string) (string, error) {
	remote := GetRemoteName(workDir)
	if remote == "" {
		return "", &PushError{Output: "No remote configured", Err: errors.New("no remote configured")}
	}

	cmd := exec.Command("git", "push", "--force-with-lease", remote, "HEAD")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), &PushError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// ExecutePushSetUpstream performs a push with upstream tracking.
// Returns the output from git and any error encountered.
func ExecutePushSetUpstream(workDir string) (string, error) {
	remote := GetRemoteName(workDir)
	if remote == "" {
		return "", &PushError{Output: "No remote configured", Err: errors.New("no remote configured")}
	}

	// Get current branch
	branchCmd := exec.Command("git", "branch", "--show-current")
	branchCmd.Dir = workDir
	branchOut, err := branchCmd.Output()
	if err != nil {
		return "", &PushError{Output: "Could not get current branch", Err: err}
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		return "", &PushError{Output: "Detached HEAD - cannot push", Err: errors.New("detached head")}
	}

	cmd := exec.Command("git", "push", "-u", remote, branch)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), &PushError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// ParsePushOutput extracts useful information from git push output.
// Returns a human-readable summary.
func ParsePushOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "Push completed"
	}

	// Look for common patterns
	if strings.Contains(output, "Everything up-to-date") {
		return "Already up-to-date"
	}

	// Look for the summary line like "abc123..def456  main -> main"
	re := regexp.MustCompile(`([a-f0-9]+)\.\.([a-f0-9]+)\s+\S+\s+->\s+\S+`)
	if matches := re.FindStringSubmatch(output); len(matches) > 0 {
		return "Pushed successfully"
	}

	// Look for new branch creation
	if strings.Contains(output, "new branch") {
		return "Created remote branch"
	}

	return "Push completed"
}

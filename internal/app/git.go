package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// normalizePath converts a path to absolute form and resolves symlinks.
// This ensures consistent path comparison across different path formats.
func normalizePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Try to resolve symlinks; if it fails (e.g., path doesn't exist), use absolute path
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return filepath.Clean(absPath), nil
	}
	return filepath.Clean(resolved), nil
}

// WorktreeInfo contains information about a git worktree.
type WorktreeInfo struct {
	Path   string // Absolute path to the worktree
	Branch string // Branch name (e.g., "feature-auth")
	IsMain bool   // True if this is the main worktree
}

// GetWorktrees returns all worktrees for the repository containing workDir.
// Returns nil if workDir is not in a git repository.
func GetWorktrees(workDir string) []WorktreeInfo {
	// First, verify this is a git repo
	cmd := exec.Command("git", "--no-optional-locks", "rev-parse", "--git-dir")
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		return nil
	}

	// Get list of worktrees
	cmd = exec.Command("git", "--no-optional-locks", "worktree", "list", "--porcelain")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	return parseWorktreeList(string(output))
}

// parseWorktreeList parses the porcelain output of `git worktree list`.
// Format is:
//
//	worktree /path/to/worktree
//	HEAD <sha>
//	branch refs/heads/branch-name
//	<blank line>
func parseWorktreeList(output string) []WorktreeInfo {
	var worktrees []WorktreeInfo
	var current WorktreeInfo
	isFirst := true

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				current.IsMain = isFirst
				worktrees = append(worktrees, current)
				isFirst = false
			}
			current = WorktreeInfo{}
			continue
		}

		if path, found := strings.CutPrefix(line, "worktree "); found {
			current.Path = filepath.Clean(path)
		} else if branchRef, found := strings.CutPrefix(line, "branch "); found {
			// Extract branch name from refs/heads/branch-name
			current.Branch = strings.TrimPrefix(branchRef, "refs/heads/")
		}
		// We ignore HEAD and other lines
	}

	// Handle last entry if no trailing newline
	if current.Path != "" {
		current.IsMain = isFirst
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// GetMainWorktreePath returns the path to the main worktree for the repository.
// Returns empty string if not in a git repo or no main worktree found.
func GetMainWorktreePath(workDir string) string {
	worktrees := GetWorktrees(workDir)
	for _, wt := range worktrees {
		if wt.IsMain {
			return wt.Path
		}
	}
	return ""
}

// GetAllRelatedPaths returns all paths that share the same git repository:
// the main worktree and all linked worktrees. Each path is absolute.
// Returns nil if workDir is not in a git repository.
func GetAllRelatedPaths(workDir string) []string {
	worktrees := GetWorktrees(workDir)
	if len(worktrees) == 0 {
		return nil
	}

	paths := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		paths = append(paths, wt.Path)
	}
	return paths
}

// WorktreeNameForPath returns the worktree name for a given absolute path.
// Returns empty string if the path is the main worktree or not found.
func WorktreeNameForPath(workDir, targetPath string) string {
	cleanTarget, err := normalizePath(targetPath)
	if err != nil {
		return ""
	}
	worktrees := GetWorktrees(workDir)
	for _, wt := range worktrees {
		normalizedWtPath, err := normalizePath(wt.Path)
		if err != nil {
			continue
		}
		if normalizedWtPath == cleanTarget && !wt.IsMain {
			if wt.Branch != "" {
				return wt.Branch
			}
			return filepath.Base(wt.Path)
		}
	}
	return ""
}

// GetRepoName returns the git repository name for the given directory.
// It tries to extract the name from the remote URL first, falling back
// to the directory name if no remote is configured.
// Returns empty string if the directory is not a git repository.
func GetRepoName(workDir string) string {
	// Check if this is a git repo
	cmd := exec.Command("git", "--no-optional-locks", "rev-parse", "--git-dir")
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		return ""
	}

	// Try to get remote URL
	cmd = exec.Command("git", "--no-optional-locks", "remote", "get-url", "origin")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err == nil {
		url := strings.TrimSpace(string(output))
		if name := parseRepoNameFromURL(url); name != "" {
			return name
		}
	}

	// Fallback to directory name
	absPath, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	return filepath.Base(absPath)
}

// parseRepoNameFromURL extracts the repository name from a git URL.
// Handles both SSH (git@github.com:user/repo.git) and HTTPS URLs.
func parseRepoNameFromURL(url string) string {
	// Remove .git suffix
	url = strings.TrimSuffix(url, ".git")

	// Handle SSH format: git@github.com:user/repo
	if idx := strings.LastIndex(url, ":"); idx != -1 && !strings.Contains(url, "://") {
		url = url[idx+1:]
	}

	// Handle HTTPS format: https://github.com/user/repo
	if idx := strings.LastIndex(url, "/"); idx != -1 {
		return url[idx+1:]
	}

	return url
}

// WorktreeExists checks if the given worktree path still exists and is valid.
// Returns false if the directory doesn't exist or is not a valid git worktree.
func WorktreeExists(worktreePath string) bool {
	// Check if directory exists
	info, err := os.Stat(worktreePath)
	if err != nil || !info.IsDir() {
		return false
	}

	// Verify it's still a valid git worktree by checking for .git file/directory
	gitPath := filepath.Join(worktreePath, ".git")
	_, err = os.Stat(gitPath)
	return err == nil
}

// CheckCurrentWorktree checks if the current working directory is still a valid worktree.
// Returns (exists, mainPath) where mainPath is the path to switch to if worktree was deleted.
func CheckCurrentWorktree(workDir string) (exists bool, mainPath string) {
	if WorktreeExists(workDir) {
		return true, ""
	}

	// Current worktree doesn't exist - find the main worktree that owned it
	// by checking .git/worktrees/*/gitdir files in sibling directories
	parentDir := filepath.Dir(workDir)
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return false, ""
	}

	// Normalize workDir for comparison
	normalizedWorkDir := filepath.Clean(workDir)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidatePath := filepath.Join(parentDir, entry.Name())
		if candidatePath == workDir {
			continue
		}

		// Check if this repo's .git/worktrees contains a reference to workDir
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
			if filepath.Clean(wtPath) == normalizedWorkDir {
				// Found the repo that owned this worktree
				main := GetMainWorktreePath(candidatePath)
				if main != "" {
					return false, main
				}
			}
		}
	}

	return false, ""
}

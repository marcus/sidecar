package gitstatus

import (
	"os"
	"os/exec"
	"strings"
)

// ExecuteFetch runs git fetch.
func ExecuteFetch(workDir string) (string, error) {
	cmd := exec.Command("git", "fetch")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &RemoteError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// ExecutePull runs git pull.
func ExecutePull(workDir string) (string, error) {
	cmd := exec.Command("git", "pull")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &RemoteError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// ExecutePullRebase runs git pull --rebase.
func ExecutePullRebase(workDir string) (string, error) {
	cmd := exec.Command("git", "pull", "--rebase")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &RemoteError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// ExecutePullFFOnly runs git pull --ff-only.
func ExecutePullFFOnly(workDir string) (string, error) {
	cmd := exec.Command("git", "pull", "--ff-only")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &RemoteError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// ExecutePullAutostash runs git pull --rebase --autostash.
func ExecutePullAutostash(workDir string) (string, error) {
	cmd := exec.Command("git", "pull", "--rebase", "--autostash")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &RemoteError{Output: string(output), Err: err}
	}
	return string(output), nil
}

// GetConflictedFiles returns a list of files with merge conflicts.
func GetConflictedFiles(workDir string) []string {
	cmd := gitReadOnly("diff", "--name-only", "--diff-filter=U")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var files []string
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files
}

// IsConflictError checks if a RemoteError indicates merge/rebase conflicts.
func IsConflictError(err error) bool {
	if re, ok := err.(*RemoteError); ok {
		out := strings.ToLower(re.Output)
		return strings.Contains(out, "conflict") ||
			strings.Contains(out, "merge conflict") ||
			strings.Contains(out, "automatic merge failed") ||
			strings.Contains(out, "could not apply")
	}
	return false
}

// AbortMerge runs git merge --abort.
func AbortMerge(workDir string) error {
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = workDir
	_, err := cmd.CombinedOutput()
	return err
}

// AbortRebase runs git rebase --abort.
func AbortRebase(workDir string) error {
	cmd := exec.Command("git", "rebase", "--abort")
	cmd.Dir = workDir
	_, err := cmd.CombinedOutput()
	return err
}

// IsRebaseInProgress checks if a rebase is currently in progress.
func IsRebaseInProgress(workDir string) bool {
	cmd := gitReadOnly("rev-parse", "--git-path", "rebase-merge")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	path := strings.TrimSpace(string(output))
	if _, err := os.Stat(path); err == nil {
		return true
	}
	cmd = gitReadOnly("rev-parse", "--git-path", "rebase-apply")
	cmd.Dir = workDir
	output, err = cmd.Output()
	if err != nil {
		return false
	}
	path = strings.TrimSpace(string(output))
	_, err = os.Stat(path)
	return err == nil
}

// RemoteError wraps a git remote operation error with its output.
type RemoteError struct {
	Output string
	Err    error
}

func (e *RemoteError) Error() string {
	return strings.TrimSpace(e.Output)
}

package gitstatus

import (
	"bufio"
	"bytes"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Branch represents a git branch.
type Branch struct {
	Name       string // Branch name
	IsCurrent  bool   // True if this is the current branch
	IsRemote   bool   // True if this is a remote tracking branch
	Upstream   string // Upstream branch name (if set)
	Ahead      int    // Commits ahead of upstream
	Behind     int    // Commits behind upstream
	LastCommit string // Short hash of last commit
}

// GetBranches retrieves the list of local branches.
func GetBranches(workDir string) ([]*Branch, error) {
	// Use git branch with format to get detailed info
	// Format: refname:short, HEAD, upstream:short, upstream:track
	cmd := gitReadOnly("branch",
		"--format=%(refname:short)|%(HEAD)|%(upstream:short)|%(upstream:track)")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var branches []*Branch
	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Pattern for tracking info: [ahead N], [behind N], [ahead N, behind N]
	trackRe := regexp.MustCompile(`\[(?:ahead (\d+))?(?:, )?(?:behind (\d+))?\]`)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}

		branch := &Branch{
			Name:      parts[0],
			IsCurrent: parts[1] == "*",
		}

		if len(parts) > 2 && parts[2] != "" {
			branch.Upstream = parts[2]
		}

		if len(parts) > 3 && parts[3] != "" {
			matches := trackRe.FindStringSubmatch(parts[3])
			if len(matches) > 0 {
				if matches[1] != "" {
					branch.Ahead, _ = strconv.Atoi(matches[1])
				}
				if matches[2] != "" {
					branch.Behind, _ = strconv.Atoi(matches[2])
				}
			}
		}

		branches = append(branches, branch)
	}

	return branches, nil
}

// CheckoutBranch switches to a branch.
func CheckoutBranch(workDir, branchName string) error {
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &BranchError{Output: string(output), Err: err}
	}
	return nil
}

// CreateBranch creates a new branch from HEAD.
func CreateBranch(workDir, branchName string) error {
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &BranchError{Output: string(output), Err: err}
	}
	return nil
}

// DeleteBranch deletes a branch.
func DeleteBranch(workDir, branchName string) error {
	cmd := exec.Command("git", "branch", "-d", branchName)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &BranchError{Output: string(output), Err: err}
	}
	return nil
}

// ForceDeleteBranch force-deletes a branch.
func ForceDeleteBranch(workDir, branchName string) error {
	cmd := exec.Command("git", "branch", "-D", branchName)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &BranchError{Output: string(output), Err: err}
	}
	return nil
}

// BranchError wraps a git branch error with its output.
type BranchError struct {
	Output string
	Err    error
}

func (e *BranchError) Error() string {
	return strings.TrimSpace(e.Output)
}

// FormatTrackingInfo formats the ahead/behind info for display.
func (b *Branch) FormatTrackingInfo() string {
	if b.Ahead == 0 && b.Behind == 0 {
		return ""
	}
	if b.Ahead > 0 && b.Behind > 0 {
		return "↑" + strconv.Itoa(b.Ahead) + "↓" + strconv.Itoa(b.Behind)
	}
	if b.Ahead > 0 {
		return "↑" + strconv.Itoa(b.Ahead)
	}
	return "↓" + strconv.Itoa(b.Behind)
}

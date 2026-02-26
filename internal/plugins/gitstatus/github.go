package gitstatus

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/msg"
)

// GitHubInfo holds parsed GitHub repository information.
type GitHubInfo struct {
	Owner string
	Repo  string
}

// GetRemoteURL returns the URL for the primary remote (origin).
func GetRemoteURL(workDir string) string {
	cmd := gitReadOnly("remote", "get-url", "origin")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// ParseGitHubInfo extracts owner/repo from a GitHub remote URL.
// Returns nil if not a GitHub URL.
func ParseGitHubInfo(remoteURL string) *GitHubInfo {
	if !strings.Contains(remoteURL, "github.com") {
		return nil
	}

	// Handle SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@github.com:") {
		path := strings.TrimPrefix(remoteURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return &GitHubInfo{Owner: parts[0], Repo: parts[1]}
		}
	}

	// Handle HTTPS: https://github.com/owner/repo.git
	if strings.Contains(remoteURL, "github.com/") {
		idx := strings.Index(remoteURL, "github.com/")
		path := remoteURL[idx+len("github.com/"):]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return &GitHubInfo{Owner: parts[0], Repo: parts[1]}
		}
	}

	return nil
}

// BuildCommitURL constructs a GitHub commit URL.
func BuildCommitURL(info *GitHubInfo, hash string) string {
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", info.Owner, info.Repo, hash)
}

// openInBrowser opens the URL in the default browser.
func openInBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		case "linux":
			cmd = exec.Command("xdg-open", url)
		default:
			return app.ToastMsg{Message: "Unsupported platform", Duration: 3 * time.Second, IsError: true}
		}
		if err := cmd.Start(); err != nil {
			return app.ToastMsg{Message: "Failed to open browser: " + err.Error(), Duration: 3 * time.Second, IsError: true}
		}
		return nil
	}
}

// openCommitInGitHub opens the current commit in GitHub.
func (p *Plugin) openCommitInGitHub() tea.Cmd {
	commit := p.getCurrentCommit()
	if commit == nil {
		return nil
	}

	remoteURL := GetRemoteURL(p.repoRoot)
	if remoteURL == "" {
		return msg.ShowToast("No remote configured", 2*time.Second)
	}

	ghInfo := ParseGitHubInfo(remoteURL)
	if ghInfo == nil {
		return msg.ShowToast("Not a GitHub repository", 2*time.Second)
	}

	url := BuildCommitURL(ghInfo, commit.Hash)
	return tea.Batch(
		openInBrowser(url),
		msg.ShowToast("Opening in GitHub...", 2*time.Second),
	)
}

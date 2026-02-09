package filebrowser

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fetchGitInfo retrieves git status and last commit for a file.
func (p *Plugin) fetchGitInfo(path string) tea.Cmd {
	return func() tea.Msg {
		if path == "" {
			return GitInfoMsg{}
		}

		// Use context with timeout to prevent hanging
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Check status
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--", path)
		statusCmd.Dir = p.ctx.WorkDir
		statusOut, err := statusCmd.Output()
		var status string
		if err != nil {
			status = "Error"
		} else {
			status = strings.TrimSpace(string(statusOut))
			if status == "" {
				status = "Clean"
			} else {
				// Extract status code (e.g. "M ", "??")
				if len(status) >= 2 {
					status = status[:2]
				}
			}
		}

		// Check last commit
		logCmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%h - %s (%cr)", "--", path)
		logCmd.Dir = p.ctx.WorkDir
		logOut, err := logCmd.Output()
		var lastCommit string
		if err != nil {
			lastCommit = "Error"
		} else {
			lastCommit = strings.TrimSpace(string(logOut))
			if lastCommit == "" {
				lastCommit = "Not committed"
			}
		}

		return GitInfoMsg{Status: status, LastCommit: lastCommit}
	}
}

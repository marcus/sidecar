package worktree

import (
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

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
			return nil
		}
		_ = cmd.Start()
		return nil
	}
}

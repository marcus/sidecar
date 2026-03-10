package gitstatus

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
)

// RepoDetectedMsg is sent after probing for a repository in the current workdir.
type RepoDetectedMsg struct {
	Epoch uint64
	Root  string
}

// GetEpoch implements plugin.EpochMessage.
func (m RepoDetectedMsg) GetEpoch() uint64 { return m.Epoch }

// RepoInitDoneMsg is sent after attempting to run git init.
// Root is set on successful repository creation. Err is set on failure.
type RepoInitDoneMsg struct {
	Epoch uint64
	Root  string
	Err   error
}

// GetEpoch implements plugin.EpochMessage.
func (m RepoInitDoneMsg) GetEpoch() uint64 { return m.Epoch }

// updateNoRepo handles key events when the current project has no git repository.
func (p *Plugin) updateNoRepo(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "i", "enter":
		if p.repoInitInProgress {
			return p, nil
		}
		p.repoInitInProgress = true
		return p, p.initRepo()
	case "r":
		return p, p.detectRepo()
	}
	return p, nil
}

// detectRepo checks whether the current working directory is now inside a git repo.
func (p *Plugin) detectRepo() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	workDir := p.ctx.WorkDir
	epoch := p.ctx.Epoch

	return func() tea.Msg {
		root, err := resolveGitRoot(workDir)
		if err != nil {
			return RepoDetectedMsg{Epoch: epoch}
		}
		return RepoDetectedMsg{Epoch: epoch, Root: root}
	}
}

// initRepo initializes a git repository at the current workdir.
func (p *Plugin) initRepo() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	workDir := p.ctx.WorkDir
	epoch := p.ctx.Epoch

	return func() tea.Msg {
		cmd := exec.Command("git", "init")
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return RepoInitDoneMsg{Epoch: epoch, Err: fmt.Errorf("git init failed: %s", msg)}
		}

		root, err := resolveGitRoot(workDir)
		if err != nil {
			return RepoInitDoneMsg{Epoch: epoch, Err: fmt.Errorf("git init succeeded but repository was not detected: %w", err)}
		}

		return RepoInitDoneMsg{Epoch: epoch, Root: root}
	}
}

// renderNoRepoView renders the git plugin view when no repository exists.
func (p *Plugin) renderNoRepoView() string {
	if p.mouseHandler != nil {
		p.mouseHandler.Clear()
	}

	var sb strings.Builder
	sb.WriteString(styles.Title.Render("Git"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Muted.Render("No git repository found in this project."))
	sb.WriteString("\n")
	if p.repoInitInProgress {
		sb.WriteString(styles.StatusInProgress.Render("Initializing repository..."))
	} else {
		sb.WriteString(styles.Muted.Render("Press "))
		sb.WriteString(styles.Title.Render("i"))
		sb.WriteString(styles.Muted.Render(" to initialize one."))
		sb.WriteString("\n")
		sb.WriteString(styles.Muted.Render("Press "))
		sb.WriteString(styles.Title.Render("r"))
		sb.WriteString(styles.Muted.Render(" to re-check."))
	}

	panelHeight := p.height
	if panelHeight < 4 {
		panelHeight = 4
	}
	return styles.RenderPanel(sb.String(), p.width, panelHeight, true)
}


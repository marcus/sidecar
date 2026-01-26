package gitstatus

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	commitMessageID = "commit-message"
	commitActionID  = "execute-commit"
)

// commitModalWidth returns the width for the commit modal content.
func (p *Plugin) commitModalWidth() int {
	w := p.width - 8 // 4-char margin each side
	if w > 80 {
		w = 80
	}
	if w < 40 {
		w = 40
	}
	return w
}

func (p *Plugin) ensureCommitModal() {
	modalW := p.commitModalWidth()
	if p.commitModal != nil && p.commitModalWidthCache == modalW {
		return
	}
	p.commitModalWidthCache = modalW
	p.commitModal = modal.New("",
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(commitActionID),
		modal.WithHints(false),
	).
		AddSection(p.commitHeaderSection()).
		AddSection(p.commitStagedSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Textarea(commitMessageID, &p.commitMessage, 4)).
		AddSection(modal.When(p.showCommitAmendToggle, modal.CheckboxDisplay("Amend last commit", &p.commitAmend, "ctrl+a"))).
		AddSection(p.commitStatusSection()).
		AddSection(modal.Buttons(
			modal.Btn(p.commitButtonLabel(), commitActionID),
			modal.Btn(" Cancel ", "cancel"),
		))
}

func (p *Plugin) showCommitAmendToggle() bool {
	if len(p.recentCommits) == 0 {
		return false
	}
	return p.tree.HasStagedFiles()
}

func (p *Plugin) commitButtonLabel() string {
	if p.commitAmend {
		return " Amend "
	}
	return " Commit "
}

func (p *Plugin) commitHeaderSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		additions, deletions := p.tree.StagedStats()
		fileCount := len(p.tree.Staged)

		titleText := " Commit "
		if p.commitAmend {
			titleText = " Amend "
		}
		title := styles.Title.Render(titleText)

		statsStr := ""
		if fileCount > 0 {
			statsStr = fmt.Sprintf("[%d: +%d -%d]", fileCount, additions, deletions)
		}
		statsRendered := styles.Muted.Render(statsStr)

		padding := contentWidth - lipgloss.Width(title) - lipgloss.Width(statsStr)
		if padding < 1 {
			padding = 1
		}

		line := title + strings.Repeat(" ", padding) + statsRendered
		sep := styles.Muted.Render(strings.Repeat("─", contentWidth))

		return modal.RenderedSection{Content: line + "\n" + sep}
	}, nil)
}

func (p *Plugin) commitStagedSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var sb strings.Builder

		fileCount := len(p.tree.Staged)
		if p.commitAmend && fileCount == 0 {
			sb.WriteString(styles.Muted.Render("Message-only amend (no staged changes)"))
			return modal.RenderedSection{Content: sb.String()}
		}

		if p.commitAmend {
			sb.WriteString(styles.StatusStaged.Render(fmt.Sprintf("Staged (%d) — will be added to amended commit", fileCount)))
		} else {
			sb.WriteString(styles.StatusStaged.Render(fmt.Sprintf("Staged (%d)", fileCount)))
		}
		sb.WriteString("\n")

		maxFiles := 8
		if p.height < 30 {
			maxFiles = 6
		}
		if p.height < 24 {
			maxFiles = 4
		}

		for i, entry := range p.tree.Staged {
			if i >= maxFiles {
				remaining := len(p.tree.Staged) - maxFiles
				sb.WriteString(styles.Muted.Render(fmt.Sprintf("  ... +%d more", remaining)))
				break
			}

			status := styles.StatusStaged.Render(string(entry.Status))

			path := entry.Path
			maxPathWidth := contentWidth - 18
			if maxPathWidth < 10 {
				maxPathWidth = 10
			}
			if len(path) > maxPathWidth {
				path = "..." + path[len(path)-maxPathWidth+3:]
			}

			stats := ""
			if entry.DiffStats.Additions > 0 || entry.DiffStats.Deletions > 0 {
				addStr := styles.DiffAdd.Render(fmt.Sprintf("+%d", entry.DiffStats.Additions))
				delStr := styles.DiffRemove.Render(fmt.Sprintf("-%d", entry.DiffStats.Deletions))
				stats = fmt.Sprintf(" %s %s", addStr, delStr)
			}

			sb.WriteString(fmt.Sprintf("  %s %s%s", status, path, stats))
			if i < maxFiles-1 && i < len(p.tree.Staged)-1 {
				sb.WriteString("\n")
			}
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

func (p *Plugin) commitStatusSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		lines := make([]string, 0, 2)
		if p.commitError != "" {
			lines = append(lines, styles.StatusDeleted.Render("✗ "+p.commitError))
		}
		if p.commitInProgress {
			progressText := "Committing..."
			if p.commitAmend {
				progressText = "Amending..."
			}
			lines = append(lines, styles.Muted.Render(progressText))
		}
		return modal.RenderedSection{Content: strings.Join(lines, "\n")}
	}, nil)
}

// renderCommitModal renders the commit modal overlaid on the status view.
func (p *Plugin) renderCommitModal() string {
	background := p.renderThreePaneView()
	p.ensureCommitModal()
	if p.commitModal == nil {
		return background
	}

	modalContent := p.commitModal.Render(p.width, p.height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, p.width, p.height)
}

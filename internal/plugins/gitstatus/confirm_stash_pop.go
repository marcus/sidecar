package gitstatus

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// renderConfirmStashPop renders the confirm stash pop modal overlay.
func (p *Plugin) renderConfirmStashPop() string {
	// Render the background (status view dimmed)
	background := p.renderThreePaneView()

	if p.stashPopItem == nil {
		return background
	}

	stash := p.stashPopItem

	// Build modal content
	var sb strings.Builder

	// Warning style
	warningStyle := lipgloss.NewStyle().Foreground(styles.Warning).Bold(true)

	// Warning icon and title
	title := warningStyle.Render(" Pop Stash ")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Stash info
	sb.WriteString(fmt.Sprintf("  %s\n", styles.Subtitle.Render(stash.Ref)))
	if stash.Message != "" {
		// Truncate long messages (rune-based for Unicode safety)
		msg := stash.Message
		if runes := []rune(msg); len(runes) > 50 {
			msg = string(runes[:47]) + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s\n\n", styles.Muted.Render(msg)))
	} else {
		sb.WriteString("\n")
	}

	// Warning message
	sb.WriteString(warningStyle.Render("  Warning: "))
	sb.WriteString("This may cause merge conflicts.\n")
	sb.WriteString(styles.Muted.Render("  The stash will be removed if successful."))
	sb.WriteString("\n\n")

	// Interactive buttons
	sb.WriteString(ui.RenderButtonPair(" Pop ", " Cancel ", p.stashPopButtonFocus, p.stashPopButtonHover))

	// Create modal box
	modalWidth := 55
	if modalWidth > p.width-10 {
		modalWidth = p.width - 10
	}

	modalContent := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Warning).
		Padding(1, 2).
		Width(modalWidth).
		Render(sb.String())

	// Overlay modal on dimmed background
	return ui.OverlayModal(background, modalContent, p.width, p.height)
}

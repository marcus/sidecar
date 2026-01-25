package gitstatus

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// buildDiscardModal creates or updates the discard confirmation modal.
func (p *Plugin) buildDiscardModal() {
	if p.discardFile == nil {
		p.discardModal = nil
		return
	}

	entry := p.discardFile

	// Determine status label
	statusLabel := "modified"
	if entry.Staged {
		statusLabel = "staged"
	} else if entry.Status == StatusUntracked {
		statusLabel = "untracked"
	}

	// Determine warning message
	var warningMsg string
	if entry.Status == StatusUntracked {
		warningMsg = styles.StatusDeleted.Render("This will permanently delete the file!")
	} else {
		warningMsg = styles.Muted.Render("This will revert to the last committed state.")
	}

	// Calculate modal width based on path length
	modalWidth := 50
	if len(entry.Path) > 35 {
		modalWidth = len(entry.Path) + 15
	}
	if modalWidth > p.width-10 {
		modalWidth = p.width - 10
	}

	p.discardModal = modal.New("Discard Changes",
		modal.WithVariant(modal.VariantDanger),
		modal.WithWidth(modalWidth),
	).
		AddSection(modal.Text(fmt.Sprintf("Discard %s changes to:", statusLabel))).
		AddSection(modal.Text(styles.Subtitle.Render(entry.Path))).
		AddSection(modal.Spacer()).
		AddSection(modal.Text(warningMsg)).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Discard ", "discard", modal.BtnDanger()),
			modal.Btn(" Cancel ", "cancel"),
		))
}

// renderConfirmDiscard renders the confirm discard modal overlay.
func (p *Plugin) renderConfirmDiscard() string {
	// Render the background (status view dimmed)
	background := p.renderThreePaneView()

	if p.discardFile == nil {
		return background
	}

	// Build or rebuild modal if needed
	if p.discardModal == nil {
		p.buildDiscardModal()
	}

	// Render modal with hit regions
	modalContent := p.discardModal.Render(p.width, p.height, p.mouseHandler)

	// Overlay modal on dimmed background
	return ui.OverlayModal(background, modalContent, p.width, p.height)
}

// handleDiscardMouse handles mouse events for the discard confirmation modal.
func (p *Plugin) handleDiscardMouse(msg tea.MouseMsg) (plugin.Plugin, tea.Cmd) {
	if p.discardModal == nil {
		return p, nil
	}

	action := p.discardModal.HandleMouse(msg, p.mouseHandler)

	switch action {
	case "discard":
		return p.confirmDiscard()
	case "cancel":
		return p.cancelDiscard()
	}

	return p, nil
}

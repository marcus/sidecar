package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacelist"
)

// The project sidebar's View surface. It deliberately mirrors the global
// browser's — same title, same shape, same wording, and the same action IDs out
// of workspacelist — so the two answer the same question the same way.
//
// The sections are composed here rather than shared wholesale because the two
// scopes genuinely offer different things: this one has Manual and no project
// facet, global has a "show idle worktrees" toggle and no Manual. Sharing the
// composition would mean a surface full of conditionals about which caller it
// was serving; sharing the vocabulary gets the parity that actually matters.

const (
	viewFlyoutSortListID = "view-sort"
	viewFlyoutDoneID     = "view-done"
)

func (p *Plugin) viewFlyoutActive() bool { return p.viewFlyout != nil }

func (p *Plugin) openViewFlyout() {
	p.viewFlyoutSortIdx = workspacelist.SortIndex(p.listSort, projectSortModes)
	p.viewFlyoutWidth = 0
	p.viewFlyout = nil
	p.ensureViewFlyout()
	if p.viewFlyout == nil {
		return
	}
	if p.viewFlyoutMouse == nil {
		p.viewFlyoutMouse = mouse.NewHandler()
	}
	// Render once so focus IDs exist before the next Update, or the first key
	// after `v` lands on a modal that has not laid itself out yet.
	w, h := max(p.width, 80), max(p.height, 24)
	_ = p.viewFlyout.Render(w, h, p.viewFlyoutMouse)
	p.viewFlyout.Reset()
	p.viewFlyout.SetFocus(viewFlyoutSortListID)
}

func (p *Plugin) closeViewFlyout() { p.viewFlyout = nil }

func (p *Plugin) ensureViewFlyout() {
	modalW := min(42, max(20, p.width-4))
	if p.viewFlyout != nil && p.viewFlyoutWidth == modalW {
		return
	}
	p.viewFlyoutWidth = modalW

	items := make([]modal.SelectItem, len(projectSortModes))
	for i, mode := range projectSortModes {
		items[i] = modal.SelectItem{ID: workspacelist.SortActionID(mode), Label: sortMenuLabel(mode), Data: mode}
	}

	p.viewFlyout = modal.New("View",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(modal.Custom(func(int, string, string) modal.RenderedSection {
			return modal.RenderedSection{Content: "Current sort: " + p.listSort.Label()}
		}, nil)).
		AddSection(modal.Spacer()).
		// The list shape rather than the segmented one: this flyout is a menu,
		// and "Manual — shells and worktrees" is a sentence no segment could
		// hold. The global Workspaces flyout forces the same shape, because
		// the two surfaces are one model.
		AddSection(modal.Select(viewFlyoutSortListID, items, &p.viewFlyoutSortIdx,
			modal.WithShape(modal.ShapeList), modal.WithMaxVisible(len(items)))).
		// The filter line appears only when a filter is doing something. A
		// permanent "Filter: none" is a row of chrome spent saying nothing,
		// and it dilutes the line that matters when a query is live. The
		// global browser's flyout says it the same way.
		AddSection(modal.When(func() bool { return p.filterActive() }, modal.Spacer())).
		AddSection(modal.When(func() bool { return p.filterActive() },
			modal.Custom(func(int, string, string) modal.RenderedSection {
				return modal.RenderedSection{Content: "Filter: " + p.listFilter.Query()}
			}, nil),
		)).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(modal.Btn(" Done ", viewFlyoutDoneID)))
}

// sortMenuLabel names each mode in the menu. Manual gets a word about what it
// means, because "Manual" alone does not say that it is the mode where shells
// sit inside their worktree.
func sortMenuLabel(mode workspacelist.Sort) string {
	if mode == workspacelist.SortManual {
		return "Manual — shells and worktrees"
	}
	return mode.Label()
}

func (p *Plugin) overlayViewFlyout(background string, width, height int) string {
	p.ensureViewFlyout()
	if p.viewFlyout == nil {
		return background
	}
	if p.viewFlyoutMouse == nil {
		p.viewFlyoutMouse = mouse.NewHandler()
	}
	return ui.OverlayModal(background, p.viewFlyout.Render(width, height, p.viewFlyoutMouse), width, height)
}

func (p *Plugin) handleViewFlyoutKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	p.ensureViewFlyout()
	if p.viewFlyout == nil {
		return true, nil
	}
	if key := msg.String(); key == "v" || key == "q" {
		// The key that opened it closes it, the way `I` closes doc info.
		p.closeViewFlyout()
		return true, nil
	}
	action, cmd := p.viewFlyout.HandleKey(msg)
	return true, tea.Batch(cmd, p.applyViewFlyoutAction(action))
}

func (p *Plugin) handleViewFlyoutMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureViewFlyout()
	if p.viewFlyout == nil || p.viewFlyoutMouse == nil {
		return nil
	}
	return p.applyViewFlyoutAction(p.viewFlyout.HandleMouse(msg, p.viewFlyoutMouse))
}

func (p *Plugin) applyViewFlyoutAction(action string) tea.Cmd {
	switch action {
	case "", viewFlyoutSortListID:
		return nil
	case "cancel", viewFlyoutDoneID:
		p.closeViewFlyout()
		return nil
	}
	if mode, ok := workspacelist.SortFromAction(action, projectSortModes); ok {
		return p.setListSort(mode)
	}
	return nil
}

// setListSort changes the order and keeps the cursor on the same workspace.
// Selection is by identity, so the row travels and the cursor rides it; only
// the viewport has to be brought back to wherever the row landed.
func (p *Plugin) setListSort(mode workspacelist.Sort) tea.Cmd {
	p.viewFlyoutSortIdx = workspacelist.SortIndex(mode, projectSortModes)
	p.closeViewFlyout()
	if p.listSort == mode {
		return nil
	}
	p.listSort = mode
	p.ensureVisible()
	p.saveListSort()
	return nil
}

// saveListSort persists the chosen order for this project. It is per-project
// rather than global because the answer is genuinely project-shaped: a
// repository you keep four agents in wants Activity, and one you keep two long
// worktrees in wants Manual.
func (p *Plugin) saveListSort() {
	if p.ctx == nil {
		return
	}
	hooks := p.shellStartupHooks.withDefaults()
	wtState := hooks.getWorkspaceState(p.ctx.ProjectRoot)
	wtState.ListSort = p.listSort.Label()
	_ = hooks.setWorkspaceState(p.ctx.ProjectRoot, wtState)
}

// restoreListSort reads the saved order back. It is deliberately separate from
// restoreSelectionState, which returns early when there is no saved selection:
// a project can have a remembered sort and no remembered selection, and the
// sort has to survive that.
func (p *Plugin) restoreListSort() {
	if p.ctx == nil {
		return
	}
	saved := p.shellStartupHooks.withDefaults().getWorkspaceState(p.ctx.ProjectRoot).ListSort
	if mode, ok := workspacelist.SortFromLabel(saved, projectSortModes); ok {
		p.listSort = mode
	}
}

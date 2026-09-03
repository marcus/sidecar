package overview

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacelist"
)

const (
	viewFlyoutSortListID  = "view-sort"
	viewFlyoutIdleID      = "show-idle"
	viewFlyoutRemotesID   = "show-remotes"
	viewFlyoutHostPrefix  = "show-host:"
	viewFlyoutDoneID      = "done"
	viewFlyoutHostIndent  = "  "
	viewFlyoutRemotesText = "show remotes"
)

// viewFlyoutHostID names one machine's checkbox. The ID carries the host so a
// click resolves to a machine rather than to a position, which matters because
// the section is rebuilt whenever the configured set changes.
func viewFlyoutHostID(host string) string { return viewFlyoutHostPrefix + host }

func viewFlyoutHostFromID(id string) (string, bool) {
	if !strings.HasPrefix(id, viewFlyoutHostPrefix) {
		return "", false
	}
	return strings.TrimPrefix(id, viewFlyoutHostPrefix), true
}

func workspacesEmptyText(showIdle bool) string {
	if showIdle {
		return "No shells or worktrees found in the configured projects"
	}
	return "no sessions"
}

func globalFirstRunEmpty(width int) (lines []string, actionLine int) {
	if width < 1 {
		width = 1
	}
	pill := styles.RenderPillWithStyle(globalFirstRunPillLabel(width), styles.ButtonHover, nil)
	lines = []string{
		styles.Title.Render(ansi.Truncate("No workspaces yet", width, "…")),
		"",
	}
	for _, text := range []string{
		"Press n to create a worktree, or ctrl+n for a shell.",
		"Click + in the header, or the button below.",
		"Pick an agent in that form to launch one.",
	} {
		wrapped := ansi.Wordwrap(text, width, "")
		for _, line := range strings.Split(strings.TrimRight(wrapped, "\n"), "\n") {
			if line != "" {
				lines = append(lines, styles.Muted.Render(line))
			}
		}
	}
	lines = append(lines, "")
	actionLine = len(lines)
	lines = append(lines, pill)
	return lines, actionLine
}

func globalFirstRunPillLabel(width int) string {
	for _, label := range []string{"n  Create Workspace", "n  Create", "Create"} {
		if ansi.StringWidth(label)+2 <= width {
			return label
		}
	}
	return "Create"
}

func (m *Model) ViewFlyoutOpen() bool { return m.viewFlyoutOpen }

func (m *Model) openViewFlyout() {
	m.closeRenameShell()
	m.viewFlyoutOpen = true
	m.viewFlyoutSortIdx = sortIndex(m.workspaces.Sort())
	m.viewFlyout = nil
	m.viewFlyoutWidth = 0
	m.ensureViewFlyout()
	if m.viewFlyout == nil {
		return
	}
	if m.viewFlyoutMouse == nil {
		m.viewFlyoutMouse = mouse.NewHandler()
	}
	// Render once so focus IDs exist before the next Update. Without this,
	// the first key after `s` is dropped (View has not run yet).
	w, h := m.width, m.height
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 24
	}
	_ = m.viewFlyout.Render(w, h, m.viewFlyoutMouse)
	m.viewFlyout.Reset()
	m.viewFlyout.SetFocus(viewFlyoutSortListID)
}

func (m *Model) closeViewFlyout() {
	m.viewFlyoutOpen = false
}

func (m *Model) overlayViewFlyout(background string, width, height int) string {
	m.ensureViewFlyout()
	if m.viewFlyout == nil {
		return background
	}
	if m.viewFlyoutMouse == nil {
		m.viewFlyoutMouse = mouse.NewHandler()
	}
	rendered := m.viewFlyout.Render(width, height, m.viewFlyoutMouse)
	return ui.OverlayModal(background, rendered, width, height)
}

func (m *Model) ensureViewFlyout() {
	modalW := 42
	if m.width > 0 && modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}
	hostsKey := strings.Join(m.configuredHostIDs(), "\x00")
	if m.viewFlyout != nil && m.viewFlyoutWidth == modalW && m.viewFlyoutHostsKey == hostsKey {
		m.syncViewFlyoutHosts()
		return
	}
	// A host registering or de-registering rebuilds the sections. Keeping the
	// focused control across that rebuild matters because the rebuild is not
	// something the user did: focus jumping back to the sort list mid-keystroke
	// would be a machine elsewhere moving the keyboard under their hands.
	focused := ""
	if m.viewFlyout != nil {
		focused = m.viewFlyout.FocusedID()
	}
	m.viewFlyoutWidth = modalW
	m.viewFlyoutHostsKey = hostsKey
	m.viewFlyoutSortIdx = sortIndex(m.workspaces.Sort())
	m.syncViewFlyoutHosts()
	defer func() {
		// A checkbox for a machine that has just been de-registered has no
		// control to return to, and a focus request the modal can never satisfy
		// stays pending — it would snap focus back if that host ever returned.
		if host, isHost := viewFlyoutHostFromID(focused); isHost && !m.hostIsConfigured(host) {
			return
		}
		if focused != "" && m.viewFlyout != nil {
			m.viewFlyout.SetFocus(focused)
		}
	}()

	items := make([]modal.SelectItem, len(workspacelist.SortModes))
	for i, mode := range workspacelist.SortModes {
		items[i] = modal.SelectItem{ID: sortActionID(mode), Label: mode.Label(), Data: mode}
	}

	m.viewFlyout = modal.New("View",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(modal.Custom(func(contentWidth int, _, _ string) modal.RenderedSection {
			return modal.RenderedSection{Content: "Current sort: " + m.workspaces.Sort().Label()}
		}, nil)).
		AddSection(modal.Spacer()).
		// The list shape rather than the segmented one, because this flyout is
		// a menu and because the project sidebar's twin flyout names a mode in
		// a sentence ("Manual — shells and worktrees") that no segment could
		// hold. The two surfaces are one model and must not diverge here.
		AddSection(modal.Select(viewFlyoutSortListID, items, &m.viewFlyoutSortIdx,
			modal.WithShape(modal.ShapeList), modal.WithMaxVisible(len(items)))).
		// Remotes sit above the filter line because they answer the same
		// question a step earlier: "why is that row not here?" is a question
		// about which machines are on before it is a question about the query.
		AddSection(modal.When(m.hasConfiguredHosts, modal.Spacer())).
		AddSection(modal.When(m.hasConfiguredHosts,
			modal.Checkbox(viewFlyoutRemotesID, workspacelist.HostGlyph+" "+viewFlyoutRemotesText, &m.viewFlyoutShowHosts))).
		AddSection(modal.When(m.hasConfiguredHosts, m.viewFlyoutHostsSection())).
		// The filter line appears only when a filter is doing something. A
		// permanent "Filter: none" is a row of chrome spent saying nothing,
		// and it dilutes the line that matters when a query is live.
		AddSection(modal.When(func() bool { return m.workspaces.Filter().Active() }, modal.Spacer())).
		AddSection(modal.When(func() bool { return m.workspaces.Filter().Active() },
			modal.Custom(func(contentWidth int, _, _ string) modal.RenderedSection {
				return modal.RenderedSection{Content: "Filter: " + m.workspaces.Filter().Query()}
			}, nil),
		)).
		AddSection(modal.Spacer()).
		AddSection(modal.Checkbox(viewFlyoutIdleID, "show idle worktrees", &m.showIdleWorktrees)).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(modal.Btn(" Done ", viewFlyoutDoneID)))
}

// hostIsConfigured reports whether a machine still has a checkbox to focus.
func (m *Model) hostIsConfigured(id string) bool {
	for _, configured := range m.configuredHostIDs() {
		if configured == id {
			return true
		}
	}
	return false
}

// hasConfiguredHosts reports whether the remotes controls have anything to
// show. With no hosts registered the whole section is absent — a checkbox
// group for a feature the user has not set up is noise.
func (m *Model) hasConfiguredHosts() bool { return len(m.configuredHostIDs()) > 0 }

// syncViewFlyoutHosts rebuilds the checkbox state from the model's hidden set.
//
// The flyout reads the model, never the other way round: nothing here is the
// source of truth for what is hidden, so a config reload or a restore while
// the flyout is open cannot leave a box disagreeing with the list behind it.
func (m *Model) syncViewFlyoutHosts() {
	ids := m.configuredHostIDs()
	m.viewFlyoutHostIDs = ids
	if cap(m.viewFlyoutHostShow) >= len(ids) {
		m.viewFlyoutHostShow = m.viewFlyoutHostShow[:len(ids)]
	} else {
		m.viewFlyoutHostShow = make([]bool, len(ids))
	}
	anyShown := false
	for i, id := range ids {
		m.viewFlyoutHostShow[i] = m.hostShown(id)
		if m.viewFlyoutHostShow[i] {
			anyShown = true
		}
	}
	// The master reads "any machine is contributing rows", which is what makes
	// it useful as a single switch: pressing it when it is on hides everything,
	// pressing it again brings everything back.
	m.viewFlyoutShowHosts = anyShown
}

// viewFlyoutHostsSection draws one indented checkbox per configured machine.
//
// It is a Custom section rather than a run of modal.Checkbox sections because
// the rows are indented under the master toggle, and a checkbox's label is
// painted with a button background — leading spaces inside the label would
// widen the button rather than indent it.
func (m *Model) viewFlyoutHostsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		lines := make([]string, 0, len(m.viewFlyoutHostIDs))
		focusables := make([]modal.FocusableInfo, 0, len(m.viewFlyoutHostIDs))
		for i, id := range m.viewFlyoutHostIDs {
			box := "[ ]"
			if i < len(m.viewFlyoutHostShow) && m.viewFlyoutHostShow[i] {
				box = "[x]"
			}
			actionID := viewFlyoutHostID(id)
			style := styles.Button
			switch actionID {
			case focusID:
				style = styles.ButtonFocused
			case hoverID:
				style = styles.ButtonHover
			}
			label := box + " " + id
			if health, ok := m.hostHealth[id]; ok && !health.State.Shows() {
				// A machine that is not contributing rows says so here, or its
				// empty checkbox reads as "hidden" when it is in fact unreachable.
				label += " · " + string(health.State)
			}
			rendered := style.Render(ansi.Truncate(label, max(1, contentWidth-len(viewFlyoutHostIndent)), "…"))
			lines = append(lines, viewFlyoutHostIndent+rendered)
			focusables = append(focusables, modal.FocusableInfo{
				ID:      actionID,
				OffsetX: len(viewFlyoutHostIndent),
				OffsetY: i,
				Width:   ansi.StringWidth(rendered),
				Height:  1,
			})
		}
		return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
	}, func(msg tea.Msg, focusID string) (string, tea.Cmd) {
		host, ok := viewFlyoutHostFromID(focusID)
		if !ok || host == "" {
			return "", nil
		}
		key, isKey := msg.(tea.KeyPressMsg)
		if !isKey {
			return "", nil
		}
		switch key.String() {
		case " ", "space", "enter":
			// The toggle itself is the caller's, so keyboard and mouse take
			// exactly one path into the hidden set.
			return focusID, nil
		}
		return "", nil
	})
}

// applyViewFlyoutHosts writes the checkbox state into the hidden set and
// rebuilds both projections. Returns a command when anything actually moved.
func (m *Model) applyViewFlyoutHosts() tea.Cmd {
	changed := false
	for i, id := range m.viewFlyoutHostIDs {
		if i >= len(m.viewFlyoutHostShow) {
			break
		}
		if m.setHostHidden(id, !m.viewFlyoutHostShow[i]) {
			changed = true
		}
	}
	m.syncViewFlyoutHosts()
	if !changed {
		return nil
	}
	_ = saveSessionsHiddenHosts(m.hiddenHostIDs())
	// The board and the list are both projections of the host set, so both are
	// rebuilt from the one sync rather than the list alone.
	m.syncBoard()
	return m.previewSync()
}

func (m *Model) handleViewFlyoutKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	m.ensureViewFlyout()
	if m.viewFlyout == nil {
		return true, nil
	}
	before := m.viewFlyoutSnapshot()
	action, cmd := m.viewFlyout.HandleKey(msg)
	return true, tea.Batch(cmd, m.applyViewFlyoutAction(action, before))
}

func (m *Model) handleViewFlyoutMouse(msg tea.MouseMsg) tea.Cmd {
	m.ensureViewFlyout()
	if m.viewFlyout == nil || m.viewFlyoutMouse == nil {
		return nil
	}
	before := m.viewFlyoutSnapshot()
	action := m.viewFlyout.HandleMouse(msg, m.viewFlyoutMouse)
	return m.applyViewFlyoutAction(action, before)
}

// viewFlyoutState is what the flyout's toggles read before an event, so an
// event that flipped a bound value itself (a keyboard space on a modal.Checkbox
// toggles in place) is told apart from one that only reported an action.
type viewFlyoutState struct {
	idle      bool
	showHosts bool
}

func (m *Model) viewFlyoutSnapshot() viewFlyoutState {
	return viewFlyoutState{idle: m.showIdleWorktrees, showHosts: m.viewFlyoutShowHosts}
}

func (m *Model) applyViewFlyoutAction(action string, before viewFlyoutState) tea.Cmd {
	if host, ok := viewFlyoutHostFromID(action); ok {
		m.toggleViewFlyoutHost(host)
		return tea.Batch(m.applyViewFlyoutHosts(), m.persistIdleIfChanged(before))
	}
	switch action {
	case "", viewFlyoutSortListID:
		return tea.Batch(m.applyRemotesMaster(before), m.persistIdleIfChanged(before))
	case "cancel", viewFlyoutDoneID:
		m.closeViewFlyout()
		return tea.Batch(m.applyRemotesMaster(before), m.persistIdleIfChanged(before))
	case viewFlyoutIdleID:
		if m.showIdleWorktrees == before.idle {
			m.showIdleWorktrees = !m.showIdleWorktrees
		}
		return m.persistIdleAndSync()
	case viewFlyoutRemotesID:
		// A click reports the checkbox without flipping it; a keyboard space
		// flipped it already. Either way the value below is what the user
		// asked for, and every machine follows it.
		if m.viewFlyoutShowHosts == before.showHosts {
			m.viewFlyoutShowHosts = !m.viewFlyoutShowHosts
		}
		for i := range m.viewFlyoutHostShow {
			m.viewFlyoutHostShow[i] = m.viewFlyoutShowHosts
		}
		return tea.Batch(m.applyViewFlyoutHosts(), m.persistIdleIfChanged(before))
	}
	if mode, ok := sortFromAction(action); ok {
		m.workspaces.SetSort(mode)
		m.viewFlyoutSortIdx = sortIndex(mode)
		_ = saveWorkspaceListSort(mode.Label())
		m.closeViewFlyout()
		return m.previewSync()
	}
	return tea.Batch(m.applyRemotesMaster(before), m.persistIdleIfChanged(before))
}

// applyRemotesMaster catches a keyboard space on the master checkbox, which
// flips the bound value in place and reports no action of its own.
func (m *Model) applyRemotesMaster(before viewFlyoutState) tea.Cmd {
	if m.viewFlyoutShowHosts == before.showHosts {
		return nil
	}
	for i := range m.viewFlyoutHostShow {
		m.viewFlyoutHostShow[i] = m.viewFlyoutShowHosts
	}
	return m.applyViewFlyoutHosts()
}

func (m *Model) toggleViewFlyoutHost(host string) {
	for i, id := range m.viewFlyoutHostIDs {
		if id == host && i < len(m.viewFlyoutHostShow) {
			m.viewFlyoutHostShow[i] = !m.viewFlyoutHostShow[i]
			return
		}
	}
}

func (m *Model) persistIdleIfChanged(before viewFlyoutState) tea.Cmd {
	if m.showIdleWorktrees == before.idle {
		return nil
	}
	return m.persistIdleAndSync()
}

func (m *Model) persistIdleAndSync() tea.Cmd {
	_ = saveShowIdleWorktrees(m.showIdleWorktrees)
	m.syncWorkspaces()
	return m.previewSync()
}

// The sort vocabulary is workspacelist's so the project sidebar's View surface
// and this one cannot name the same choice differently.
func sortIndex(mode workspacelist.Sort) int {
	return workspacelist.SortIndex(mode, workspacelist.SortModes)
}

func sortActionID(mode workspacelist.Sort) string { return workspacelist.SortActionID(mode) }

func sortFromAction(action string) (workspacelist.Sort, bool) {
	return workspacelist.SortFromAction(action, workspacelist.SortModes)
}

package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/configui"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/gitinit"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/livepanes"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/noteview"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/overview"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/theme"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/version"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// isMouseEscapeSequence returns true if the key message appears to be
// an unparsed mouse escape sequence (SGR format: [<...M or [<...m)
func isMouseEscapeSequence(msg tea.KeyPressMsg) bool {
	s := msg.String()
	// SGR mouse sequences contain [< and end with M or m
	if strings.Contains(s, "[<") && (strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m")) {
		return true
	}
	// Check for semicolon-separated coordinate patterns typical of mouse sequences
	if strings.Contains(s, ";") && strings.ContainsAny(s, "0123456789") {
		if strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m") {
			return true
		}
	}
	return false
}

// offsetMouseY returns a copy of the mouse message with its Y coordinate
// shifted by dy, preserving the concrete message type. In bubbletea v2 mouse
// messages are interfaces (no struct rebuild), so we reconstruct the matching
// concrete type from the offset Mouse value.
func offsetMouseY(msg tea.MouseMsg, dy int) tea.MouseMsg {
	mm := msg.Mouse()
	mm.Y += dy
	switch msg.(type) {
	case tea.MouseClickMsg:
		return tea.MouseClickMsg(mm)
	case tea.MouseReleaseMsg:
		return tea.MouseReleaseMsg(mm)
	case tea.MouseWheelMsg:
		return tea.MouseWheelMsg(mm)
	case tea.MouseMotionMsg:
		return tea.MouseMotionMsg(mm)
	}
	return msg
}

// handlePaste routes a bracketed-paste message into the active text-input modal
// (mirroring the per-modal key routing in handleKeyMsg), or forwards it to the
// active plugin when no app-level text-input modal is open. textinput.Update handles
// tea.PasteMsg natively in v2.
func (m *Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	switch m.activeModal() {
	case ModalPalette:
		var cmd tea.Cmd
		m.palette, cmd = m.palette.Update(msg)
		return m, cmd

	case ModalWorktreeSwitcher:
		var cmd tea.Cmd
		m.worktreeSwitcherInput, cmd = m.worktreeSwitcherInput.Update(msg)
		m.worktreeSwitcherFiltered = filterWorktreeRows(m.worktreeSwitcherAll, m.worktreeSwitcherInput.Value())
		m.clearWorktreeSwitcherModal()
		return m, cmd

	case ModalProjectSwitcher:
		// The project-add sub-flow has multiple focus-dependent inputs; leave it
		// to the plugin-forward fallback rather than guess the focused field.
		if !m.projectAddMode {
			return m, m.updateProjectSwitcherFilter(msg)
		}

	case ModalThemeSwitcher:
		var cmd tea.Cmd
		m.themeSwitcherInput, cmd = m.themeSwitcherInput.Update(msg)
		m.themeSwitcherFiltered = filterThemeEntries(buildUnifiedThemeList(), m.themeSwitcherInput.Value())
		m.clearThemeSwitcherModal()
		return m, cmd

	case ModalPaneSwitcher:
		// The modal covers the plugin: a paste belongs to its picker or to
		// nothing, never to the surface underneath.
		return m, m.paneSwitcherPaste(msg.Content)

	case ModalPaneReposition:
		return m, nil

	case ModalIssueInput:
		var cmd tea.Cmd
		m.issueInputInput, cmd = m.issueInputInput.Update(msg)
		m.issueInputModal = nil
		m.issueInputModalWidth = 0
		newValue := strings.TrimSpace(m.issueInputInput.Value())
		if newValue != m.issueSearchQuery && len(newValue) >= 2 {
			m.issueSearchQuery = newValue
			m.issueSearchLoading = true
			m.issueSearchCursor = -1
			return m, tea.Batch(cmd, issueSearchCmd(m.ui.WorkDir, newValue, m.issueSearchIncludeClosed))
		}
		if len(newValue) < 2 {
			m.issueSearchResults = nil
			m.issueSearchQuery = ""
			m.issueSearchCursor = -1
		}
		return m, cmd
	}
	// The global Sessions pane modal is owned by overview rather than the app's
	// ModalKind stack. Stop paste at that host boundary before any hidden global
	// filter, terminal, app-content editor, or project plugin can receive it.
	if m.globalWorkspacesVisible() && m.overview.WorkspacesPaneLayoutModalOpen() {
		return m, nil
	}

	if m.globalWorkspacesVisible() && m.overview.CreateOpen() && m.overview.CreatePaste(msg.Content) {
		return m, nil
	}

	if m.globalWorkspacesVisible() && m.overview.RenameShellOpen() && m.overview.RenameShellPaste(msg.Content) {
		return m, nil
	}

	// An in-file search bar in the global browser's document pane is a text
	// field too, and it owns the keyboard while it is up.
	if m.globalWorkspacesVisible() {
		if handled, cmd := m.overview.WorkspacesDocFindPaste(msg); handled {
			return m, cmd
		}
	}

	// A focused global filter is a text input and takes the paste, exactly as
	// it takes typed characters.
	if m.globalWorkspacesFilterFocused() && m.overview.WorkspacesPaste(msg.Content) {
		// A paste can change what the filter matches, and therefore what is
		// selected; the preview follows the selection.
		return m, m.overview.WorkspacesPreviewCmd()
	}

	// A live terminal search bar in the global browser is a text input and
	// takes the paste before the pane it is searching does.
	if m.globalWorkspacesVisible() {
		if handled, cmd := m.overview.WorkspacesTerminalSearchPaste(msg); handled {
			return m, cmd
		}
	}

	// A pane the global browser is typing into is a real terminal and takes the
	// paste, exactly as it takes typed characters.
	if m.globalWorkspacesVisible() {
		if handled, cmd := m.overview.WorkspacesTerminalPaste(msg.Content); handled {
			return m, cmd
		}
	}
	if cmd, handled := m.routeAppContentEditMsg(msg); handled {
		return m, cmd
	}
	// An in-file search bar in a document pane is a text field: it takes a
	// paste exactly as it takes typed characters, and it is asked before the
	// tail below drops every paste aimed at a non-primary leaf.
	if cmd, handled := m.routeAppContentDocSearchPaste(msg); handled {
		return m, cmd
	}

	// A global view that sidecar draws itself owns keyboard focus, so a paste
	// must not reach a hidden project plugin (an interactive tmux pane would
	// run it). The hosted Tasks tab is a real surface and gets its own pastes,
	// routed to the focused surface by forwardKeyToPlugin.
	if m.globalOverlayOwnsKeys() {
		return m, nil
	}

	// No app-level text-input modal active: hand the paste to the active plugin
	// only, exactly as keys are routed. Broadcasting it instead dropped the same
	// text into every background plugin's text input — a paste into a workspace
	// terminal also landed in the Tasks prompt.
	if h := m.currentContentDeck(); h != nil && h.deck.FocusedLeaf() != h.deck.Leaf(panelayout.Primary) {
		return m, nil
	}
	return m.forwardKeyToPlugin(msg)
}

// Update handles all messages and returns the updated model and commands.
func (m Model) Update(msg tea.Msg) (result tea.Model, command tea.Cmd) {
	if _, ok := msg.(attentionRefreshMsg); ok {
		m.attentionTracking = true
	}
	defer func() { result, command = attachAttentionPublish(result, command) }()
	var cmds []tea.Cmd
	if result, ok := msg.(termpreview.LinkResultMsg); ok && m.terminalLinks != nil {
		m.terminalLinks.Apply(result)
	}
	for _, h := range m.contentDecks {
		if h.live == nil {
			continue
		}
		// A watcher start must always be adopted, even after its deck left the
		// screen. Handle stops the now-unwanted watcher and clears the in-flight
		// flag; dropping it here leaks the watcher and wedges future starts.
		if _, started := msg.(livepanes.WatchStartedMsg); !h.laidOut && !started {
			continue
		}
		if cmd, handled := h.live.Handle(msg); handled && cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	updated, cmd := m.update(msg)
	cmds = append(cmds, cmd)
	switch model := updated.(type) {
	case Model:
		cmds = append(cmds, model.takeNotificationDeliveryCmds()...)
		model.prepareTerminalLinks()
		cmds = append(cmds, model.terminalLinkCmd())
		updated = model
	case *Model:
		cmds = append(cmds, model.takeNotificationDeliveryCmds()...)
		model.prepareTerminalLinks()
		cmds = append(cmds, model.terminalLinkCmd())
	}
	var decks map[string]*appContentDeck
	switch model := updated.(type) {
	case Model:
		decks = model.contentDecks
	case *Model:
		decks = model.contentDecks
	}
	for _, h := range decks {
		cmds = append(cmds, h.takeQueued())
	}
	return updated, tea.Batch(cmds...)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Rebound every Update so HostWorkspaces / RelayedLanding see this copy's
	// bind and scope, not the Model New captured. boundDestination is a value
	// field; Update is a value receiver.
	m.installPluginHostSeams()
	// Remote-host stream messages reach the global browser whatever is on
	// screen. See overview.IsHostMessage: each delivery is what schedules the
	// next read of the update channel, so dropping one on a focus check would
	// not delay a row — it would end the stream for the rest of the session.
	if m.overview != nil && overview.IsHostMessage(msg) {
		cmd := m.overview.Update(msg)
		// The Remote Hosts page reads health from the running registry rather
		// than probing, so a machine that has just connected — or just stopped
		// answering — has to reach an open Configuration surface by the same
		// stream that reaches the browser. Only the health is pushed: a full
		// host state on this cadence would re-run Configuration's own sync
		// several times a minute.
		if m.configOpen() {
			m.config.SetRemoteHosts(m.configRemoteHosts())
		}
		var cmds []tea.Cmd
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.showProjectSwitcher {
			m.refreshOpenProjectSwitcher()
		}
		if m.showWorktreeSwitcher {
			m.refreshOpenWorktreeSwitcher()
		}
		if m.boundDestination.HostID != "" && m.registry != nil {
			m.syncBoundHostIncarnation()
			// Every bound plugin hears that its host moved, not just the one
			// that lists shells. It is the only change signal that crosses the
			// boundary: livewatch is a filesystem watch and stays on the
			// machine that owns the files.
			for i, p := range m.registry.Plugins() {
				updated, extra := p.Update(plugin.HostInventoryMsg{})
				m.registry.Replace(i, updated)
				if extra != nil {
					cmds = append(cmds, extra)
				}
			}
		}
		return m, tea.Batch(cmds...)
	}
	var cmds []tea.Cmd
	// A terminal-default cell belongs to the terminal hosting Sidecar, not to
	// Sidecar's palette. Convert the host's color report once, then hand the
	// presentation context to both workspace projections through one shared
	// message. Project plugins receive it in the ordinary broadcast below; the
	// app-owned global browser is offered it explicitly.
	if background, ok := msg.(tea.BackgroundColorMsg); ok {
		msg = termpreview.HostBackgroundMsg{ANSI: styles.BgANSISeqFor(background.Color)}
		if m.overview != nil {
			if cmd := m.overview.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if result, ok := msg.(contentpanes.Result); ok {
		if cmd := (&m).applyAppContentResult(result); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	switch msg := msg.(type) {
	case appContentResolvedMsg:
		if h := m.contentDecks[msg.Key]; h != nil {
			delete(h.pending, appContentResolutionKey{Root: msg.Result.Request.Root, Candidate: msg.Result.Request.Candidate})
			if changed, accepted := h.resolution.Apply(msg.Result); changed && accepted {
				h.generation++
				h.links = nil
			}
		}
		return m, tea.Batch(cmds...)
	case appDeckSearchMsg:
		if h := m.contentDecks[msg.DeckKey]; h != nil {
			if cmd := h.applyAppContentSearchMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	case appDeckInfoMsg:
		if h := m.contentDecks[msg.DeckKey]; h != nil {
			h.applyAppContentInfoMsg(msg)
		}
		return m, tea.Batch(cmds...)
	case paneSwitcherPickerDataMsg:
		(&m).applyPaneSwitcherPickerData(msg)
		return m, tea.Batch(cmds...)
	case paneSwitcherFilesMsg:
		(&m).applyPaneSwitcherFiles(msg)
		return m, tea.Batch(cmds...)
	case docview.GitInfoMsg:
		if cmd := (&m).applyAppContentBroadcast(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case docview.LoadedMsg, issueview.LoadedMsg, noteview.LoadedMsg,
		workspacediff.SnapshotMsg, workspacediff.RangeMsg, workspacediff.CommitDetailMsg, workspacediff.CommitFileDiffMsg,
		resourceview.ResolvedMsg:
		if cmd := (&m).applyAppContentBroadcast(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.overview != nil && overview.IsAsyncMessage(msg) {
		cmd := m.overview.Update(msg)
		// A workspacediff result is not the global browser's alone. A project
		// plugin's Diff pane hosts the same view and issued its own load, so
		// returning here claimed the answer and left commit tabs on
		// "Loading diff…" forever. Offer it to the browser, then let it reach
		// the plugins like any other broadcast; every host drops what is not
		// addressed to it. Pane-switcher picker results share for the same
		// reason: whichever host has its modal open fired the loaders, and
		// both hosts' forms answer to their own types. A protocol plugin's page,
		// document, action outcome and debounce tick are shared for exactly the
		// same reason: the project workspace hosts the same browser, and a page
		// claimed here would leave a project pane refreshing forever.
		if !overview.IsSharedDiffMessage(msg) && !overview.IsSharedPickerMessage(msg) &&
			!overview.IsSharedPluginMessage(msg) {
			return m, cmd
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Configuration's own requests are answered before anything else looks at
	// them: they are addressed to the host, not to a plugin.
	if cmd, handled := (&m).configSurfaceMsg(msg); handled {
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.FocusMsg:
		m.applicationFocused = true
		return m, m.forwardApplicationFocus(msg)

	case tea.BlurMsg:
		m.applicationFocused = false
		return m, m.forwardApplicationFocus(msg)

	case tea.KeyPressMsg:
		// Input is what geometry arbitration uses to tell two focused instances
		// apart: the machine the user walked away from never blurs (td-ee222a).
		tty.NoteUserInput()
		return (&m).handleKeyMsg(msg)

	case tea.PasteMsg:
		// Pasting is user input like any other; without this a session driven
		// entirely by pastes would look unattended to arbitration (td-ee222a).
		tty.NoteUserInput()
		// v2: bracketed paste arrives as a dedicated message (not a KeyMsg).
		// Route it into the active text-input modal so paste-into-filter works
		// like v1; otherwise forward to plugins (notes editor handles it natively).
		return (&m).handlePaste(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			// Prime worktree cache before first render
			m.refreshWorktreeCache()
		}
		m.ready = true
		// The terminal has real dimensions, so a UI is genuinely being driven
		// and the first ready frame is imminent. This is where cold restore is
		// scheduled: the command still waits on the first-ready-frame latch
		// before touching anything, so the ordering guarantee is unchanged, but
		// it now exists only inside a running program rather than being returned
		// from Init where a synchronous command-runner would park on it forever.
		// Per model, not per process: a second Model in one process is entitled
		// to its own restore, and a package-global latch would let test order
		// decide which one got it.
		var restoreCmd tea.Cmd
		if !m.sessionRestoreStarted {
			m.sessionRestoreStarted = true
			restoreCmd = restoreSessionsCmd(m.cfg)
		}
		// Reset diagnostics modal on resize (will be rebuilt on next render)
		if m.showDiagnostics {
			m.diagnosticsModalWidth = 0
		}
		// Forward the content box to every surface. It is the terminal minus the
		// header, the footer, and any column the notification centre has
		// reserved — so a resize while the panel is open keeps handing out the
		// narrowed width rather than resetting to the full terminal.
		cmds := (&m).emitContentSize()
		// First real frame: name the terminal after the project.
		if cmd := (&m).syncTerminalTitle(false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if restoreCmd != nil {
			cmds = append(cmds, restoreCmd)
		}
		return m, tea.Batch(cmds...)

	case tea.MouseMsg:
		tty.NoteUserInput()
		// Route mouse events to active modal (priority order)
		switch m.activeModal() {
		case ModalPalette:
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		case ModalHelp:
			return m.handleHelpModalMouse(msg)
		case ModalUpdate:
			return m.handleUpdateModalMouse(msg)
		case ModalDiagnostics:
			return m.handleDiagnosticsModalMouse(msg)
		case ModalQuitConfirm:
			return m.handleQuitConfirmMouse(msg)
		case ModalProjectSwitcher:
			if m.projectAddMode {
				return m.handleProjectAddModalMouse(msg)
			}
			return m.handleProjectSwitcherMouse(msg)
		case ModalWorktreeSwitcher:
			return m.handleWorktreeSwitcherMouse(msg)
		case ModalThemeSwitcher:
			return m.handleThemeSwitcherMouse(msg)
		case ModalOpenIn:
			return m.handleOpenInMouse(msg)
		case ModalIssueInput:
			return m.handleIssueInputMouse(msg)
		case ModalIssuePreview:
			return m.handleIssuePreviewMouse(msg)
		case ModalPaneReposition:
			return m, (&m).handleAppPaneLayoutMouse(msg)
		case ModalPaneSwitcher:
			return m.handlePaneSwitcherMouse(msg)
		}

		// Only row 0 is painted header chrome. Left-clicks on that row stay
		// here so they are not rewritten into plugin-local Y<0 (some surfaces
		// treat that as the first row).
		mi := msg.Mouse()
		// The gear is the header's only control with a hover look, so tracking it
		// is one bool against the same bounds a click is tested with. Motion
		// anywhere else clears it, which is what keeps the highlight from being
		// left behind when the pointer leaves the header.
		if _, isMotion := msg.(tea.MouseMotionMsg); isMotion {
			hovered := false
			if mi.Y == 0 && !m.intro.Active {
				if start, end, ok := m.getGearBounds(); ok && mi.X >= start && mi.X < end {
					hovered = true
				}
			}
			m.headerGearHovered = hovered
		}
		_, isClickPress := msg.(tea.MouseClickMsg)
		if isClickPress && mi.Button == tea.MouseLeft && mi.Y < headerHeight {
			if mi.Y == 0 {
				// Brand logo opens the Overview (when the feature is enabled).
				if start, end, ok := m.getLogoBounds(); ok && !m.intro.Active && mi.X >= start && mi.X < end {
					return m, m.toggleOverview()
				}

				if start, end, ok := m.getProjectRestoreBounds(); ok && !m.intro.Active && mi.X >= start && mi.X < end {
					return m, m.exitOverview()
				}

				// The unread indicator is the centre's only pointer route in —
				// the centre has no navbar tab — and toggles it the same way
				// the shortcut does.
				if start, end, ok := m.getNotificationIndicatorBounds(); ok && !m.intro.Active && mi.X >= start && mi.X < end {
					return m, (&m).toggleNotificationCentre()
				}

				// The gear toggles Configuration: it is the control that opened
				// the surface, so it is also the one that puts it away, and
				// reopening returns to the section the user was last on.
				if start, end, ok := m.getGearBounds(); ok && !m.intro.Active && mi.X >= start && mi.X < end {
					return m, m.toggleConfiguration()
				}

				// The project selector is a stable far-right target in both scopes.
				if start, end, ok := m.getProjectSelectorBounds(); ok && !m.intro.Active && mi.X >= start && mi.X < end {
					m.showProjectSwitcher = true
					m.activeContext = "project-switcher"
					m.initProjectSwitcher()
					return m, nil
				}

				// Check if click is on a tab. The bounds carry the typed tab of the
				// scope that painted them, so a click activates that tab and only
				// that tab.
				tabBounds := m.getTabBounds()
				for _, bounds := range tabBounds {
					if mi.X >= bounds.Start && mi.X < bounds.End {
						return m, m.activateTab(bounds.Tab)
					}
				}
			}
			return m, nil
		}

		// The reserved right column belongs to the notification centre: its
		// close affordance, its list rows, and its resize rail. Anything it
		// does not claim falls through to the content below — and a press that
		// lands in the content returns focus there without closing the panel.
		if handled, cmd := (&m).notificationCentreMouseEvent(msg); handled {
			return m, cmd
		}
		// A toast floats over the content and is click-to-dismiss. It is tested
		// after the panel (which owns its own column) and before the content, so
		// the only clicks it takes are the ones that landed on the block itself.
		if (&m).toastMouseEvent(msg) {
			// The dismissal changed the column, so the retraction needs its
			// tick: without it the block sits frozen until the next heartbeat.
			return m, (&m).syncToastReveal(time.Now())
		}
		if isClickPress && mi.X < m.contentWidth() {
			(&m).blurNotificationCentre()
		}

		if m.configOpen() {
			cmd := m.config.Mouse(offsetMouseY(msg, -headerHeight))
			m.updateContext()
			return m, tea.Batch(cmd, m.notifyThemeChanged())
		}

		if m.inGlobalScope() {
			cmd := m.globalMouse(offsetMouseY(msg, -headerHeight))
			m.updateContext()
			return m, cmd
		}

		// Forward mouse events to active plugin with Y offset for the app header.
		if p := m.ActivePlugin(); p != nil {
			adjusted := offsetMouseY(msg, -headerHeight) // Offset for app header
			if handled, cmd := (&m).handleAppContentEditMouse(adjusted); handled {
				m.updateContext()
				return m, cmd
			}
			if cmd, handled := (&m).appContentMouse(adjusted); handled {
				m.updateContext()
				return m, cmd
			}
			newPlugin, cmd := p.Update(adjusted)
			plugins := m.registry.Plugins()
			if m.activePlugin < len(plugins) {
				plugins[m.activePlugin] = newPlugin
			}
			m.updateContext()
			return m, cmd
		}
		return m, nil

	case IntroTickMsg:
		if m.intro.Active {
			m.intro.Update(16 * time.Millisecond)
			// Keep ticking until logo done AND repo name fully faded in
			if !m.intro.Done || m.intro.RepoOpacity < 1.0 {
				return m, IntroTick()
			}
			// All animations complete - mark intro as inactive so header clicks work
			m.intro.Active = false
			return m, Refresh()
		}
		return m, nil

	case TickMsg:
		m.ui.UpdateClock()
		// Notification expiry rides the existing heartbeat rather than a timer
		// per toast: a countdown ticks one cell a second, which is exactly the
		// resolution this tick already has.
		// The same heartbeat reconciles the toast column — see
		// reconcileNotifications for the order and why it is that order.
		revealCmd := (&m).reconcileNotifications(time.Now())
		// The worktree inventory costs a `git worktree list` fork, so it is
		// refreshed off the update loop (never inline: this runs on the render
		// goroutine) and only every worktreeInventoryTicks. A branch switched
		// outside sidecar reaches the tab label a few seconds later instead of
		// within one, which is worth ~3500 fewer subprocess spawns an hour on a
		// session left open all day.
		m.worktreeInventoryCounter++
		var inventoryCmd tea.Cmd
		if m.worktreeInventoryCounter >= worktreeInventoryTicks {
			m.worktreeInventoryCounter = 0
			if m.ui.WorkDir != "" {
				inventoryCmd = refreshWorktreeInventoryCmd(m.ui.WorkDir)
			}
		}
		// Resync the tab title against the freshly refreshed worktree cache, so
		// a branch switched outside sidecar shows up within a second. Every
		// titleResyncTicks the title is re-asserted even when unchanged, to take
		// the tab label back from anything run through tea.ExecProcess.
		m.titleResyncCounter++
		forceTitle := m.titleResyncCounter >= titleResyncTicks
		if forceTitle {
			m.titleResyncCounter = 0
		}
		titleCmd := (&m).syncTerminalTitle(forceTitle)
		// Periodically check if current worktree still exists (every 10 seconds)
		m.worktreeCheckCounter++
		if m.worktreeCheckCounter >= 10 {
			m.worktreeCheckCounter = 0
			return m, tea.Batch(tickCmd(), checkWorktreeExists(m.ui.WorkDir), titleCmd, inventoryCmd, revealCmd)
		}
		return m, tea.Batch(tickCmd(), titleCmd, inventoryCmd, revealCmd)

	case worktreeInventoryRefreshedMsg:
		current, _ := normalizePath(m.ui.WorkDir)
		requested, _ := normalizePath(msg.WorkDir)
		if current == requested {
			m.setWorktreeInventory(msg.Inventory, m.ui.WorkDir)
		}
		return m, nil

	case ToastMsg:
		// Every legacy toast is a notification now: same call sites, same
		// message, but it lands in the store, floats as a bordered toast, and
		// stays in the centre afterwards.
		(&m).showToastWithSeverity(msg.Message, msg.Duration, msg.IsError)
		return m, (&m).syncToastReveal(time.Now())

	case FlashMsg:
		// The flash tier never touches the store: it is feedback, not a
		// record. A new flash replaces whatever is on screen.
		return m, (&m).showFlash(msg)

	case flashTickMsg:
		return m, (&m).advanceFlash(msg)

	case revealTickMsg:
		// One whole row per frame (design 1h). The loop stops the moment every
		// block has settled: motion must not hold a frame timer over a screen
		// that is not moving.
		return m, (&m).advanceToastReveal(msg)

	case notify.PostMsg:
		// The store is the app's, so posting is answered here and the result
		// broadcast: whoever draws toasts reacts to PostedMsg rather than
		// reaching into the store.
		cmd := (&m).postNotification(msg.Notification)
		return m, tea.Batch(cmd, (&m).syncToastReveal(time.Now()))

	case notify.PostedMsg:
		if msg.Created {
			if current, ok := m.findNotification(msg.Notification.ID); ok && !current.Dismissed() {
				cmds = append(cmds, m.deliverNotificationCmd(current, false))
			} else if ok {
				cmds = append(cmds, m.cancelNotificationCmd(current))
			}
		}

	case notify.DismissMsg:
		(&m).dismissNotification(msg.ID)
		return m, (&m).syncToastReveal(time.Now())

	case notify.DismissTransitionMsg:
		(&m).dismissTransition(msg.DedupeKey)
		return m, (&m).syncToastReveal(time.Now())

	case notify.ReadMsg:
		(&m).readNotification(msg.ID)
		return m, nil

	case RefreshMsg:
		m.ui.MarkRefresh()
		if m.inGlobalScope() {
			if host := m.focusedGlobalHost(); host != nil {
				return m, host.update(msg)
			}
			return m, (&m).startVisibleGlobalTab()
		}
		// Refresh active plugin
		if p := m.ActivePlugin(); p != nil {
			_, cmd := p.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case ErrorMsg:
		m.lastError = msg.Err
		m.ShowToast("Error: "+msg.Err.Error(), 5*time.Second)
		return m, nil

	case version.ChangelogMsg:
		m.handleUpdateChangelogMsg(msg)
		return m, nil

	case UpdateBatchReadyMsg:
		return m, m.handleUpdateBatchReady(msg)

	case UpdateTargetResultMsg:
		return m, m.handleUpdateTargetResult(msg)

	case UpdateElapsedTickMsg:
		// The clock belongs to the batch, not to the overlay showing it:
		// continue while a batch is in flight so hiding the modal does not
		// kill the timer and reopening never has to restart it.
		if m.updateInProgress {
			return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return UpdateElapsedTickMsg{}
			})
		}
		return m, nil

	case ActivateTargetMsg:
		return m, m.activateTarget(msg)

	case FocusPluginByIDMsg:
		// Switch to requested plugin
		m.leaveOverview(false)
		return m, m.FocusPluginByID(msg.PluginID)

	case SessionRestoredMsg:
		// One grouped summary for the whole restore, never one per shell.
		return m, m.handleSessionRestored(msg)

	case ResourceProvidersDescribedMsg:
		// Recording the outcome is metadata — instance, state, matcher count —
		// never a locator, a title, or provider output.
		logResourceProviderStatuses(msg)
		// A describe pass is the only moment the matcher snapshot can change,
		// so it is the only moment either surface needs republishing. Until
		// this runs both surfaces hold an empty matcher set, which is why a
		// resource key is ordinary text before a provider is ready.
		return m, m.publishResourceProviders()

	case firstRunProbeMsg:
		return m, (&m).handleFirstRunProbe(msg)

	case OpenConfigurationMsg:
		// One entry: an empty state, a launch command, and the gear all arrive
		// here, and escape returns to whatever was underneath when they did. A
		// named page is honored exactly; an unnamed one resumes where the user
		// last was. AddProject is the first-run deep link onto the form.
		return m, (&m).handleOpenConfiguration(msg)

	case OpenNotesPreferencesMsg:
		cmd := m.openConfiguration(configui.PagePanels)
		m.config.FocusNotesPreference()
		m.updateContext()
		return m, cmd

	case overview.OpenInGitMsg:
		return m, m.openInGitFromOverview(msg.Path)

	case overview.OpenIssueInTDMsg:
		// The global issue preview asked for the jump every issue surface
		// makes. FocusPluginByIDMsg leaves global on its own way through.
		return m, OpenIssueInTD(msg.IssueID)

	case openInGitSwitchMsg:
		// Nil inventory, same as navigateFromOverview: resolve ProjectRoot
		// from the target checkout, not the current project's worktree cache.
		pending := plugin.PendingWorkspaceSelection{
			Kind: plugin.WorkspaceSelectionWorktree,
			Key:  msg.Path,
			Path: msg.Path,
		}
		return m, m.switchProjectWithSelection(msg.Path, nil, &pending, false)

	case overview.NavigateMsg:
		if !m.globalCatalogNavigable() || !m.overview.IsCurrentNavigation(msg.Generation, msg.RequestID) {
			return m, nil
		}
		return m, m.overview.Validate(msg)

	case overview.RevealMsg:
		// An Activity card opens in the global Workspaces browser. The project
		// underneath is untouched: this is a move between two projections of
		// the same catalog, not a navigation out of the global space.
		if m.overview == nil || !m.inGlobalScope() {
			return m, nil
		}
		reveal := m.overview.RevealWorkspace(msg.Workspace)
		return m, tea.Batch(reveal, m.setGlobalTab(GlobalSessions))

	case overview.ValidationMsg:
		if !m.globalCatalogNavigable() || !m.overview.ConsumeValidation(msg.Generation, msg.RequestID) {
			return m, nil
		}
		if msg.Err != nil {
			return m, func() tea.Msg {
				return ToastMsg{Message: "Overview item is stale: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
			}
		}
		return m, m.navigateFromOverviewAction(msg.Workspace, msg.Action)

	case SwitchWorktreeMsg:
		// Switch to the requested worktree
		return m, m.switchWorktree(msg.WorktreePath)

	case WorktreeDeletedMsg:
		// Current worktree was deleted (detected by periodic check) - switch to main
		return m, tea.Batch(
			m.switchWorktree(msg.MainPath),
			ShowToast("Worktree deleted, switched to main", 3*time.Second),
		)

	case SwitchToMainWorktreeMsg:
		// Current worktree was deleted (detected by workspace plugin) - switch to main
		if msg.MainWorktreePath != "" && msg.MainWorktreePath != m.ui.WorkDir {
			return m, tea.Batch(
				m.switchProject(msg.MainWorktreePath),
				func() tea.Msg {
					return ToastMsg{
						Message:  "Worktree deleted, switched to main repo",
						Duration: 3 * time.Second,
					}
				},
			)
		}
		return m, nil

	case plugin.OpenFileMsg:
		// The editor runs through the user's login shell so their profile
		// applies; see editor_launch.go. Most editors support +lineNo syntax
		// for opening at a line.
		return m, m.launchEditor(msg)

	case EditorReturnedMsg:
		// After editor exits, trigger refresh. In v2 mouse mode is declared on
		// tea.View and the renderer re-asserts it on the next frame after
		// tea.ExecProcess returns, so no manual mouse re-enable is needed.
		if retry := editorFallbackCmd(msg); retry != nil {
			// The profile-loading shell never reached the editor. Fall back to
			// a direct exec rather than reporting a failure the user did not
			// cause and cannot see.
			return m, retry
		}
		var cmds []tea.Cmd
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg { return ErrorMsg{Err: msg.Err} })
		} else {
			cmds = append(cmds, func() tea.Msg { return RefreshMsg{} })
		}
		// The editor set its own terminal title; take it back now rather than
		// leaving the tab mislabelled until the next forced resync.
		cmds = append(cmds, (&m).syncTerminalTitle(true))
		return m, tea.Batch(cmds...)

	case palette.CommandSelectedMsg:
		// Execute the selected command from the palette
		m.showPalette = false
		m.updateContext()
		// Look up and execute the command
		if cmd, ok := m.keymap.GetCommand(msg.CommandID); ok && cmd.Handler != nil {
			return m, cmd.Handler()
		}
		// Plugins may carry the handler on the command itself rather than
		// registering a keymap command. Prefer an exact context match: one
		// command ID can mean different things in different plugin contexts.
		if handler := m.pluginCommandHandler(msg.CommandID, msg.Context); handler != nil {
			return m, handler()
		}
		// Sidecar's own globals are answered inside handleKeyMsg rather than by a
		// registered keymap handler, so the palette resolves them here. They must
		// not be registered with the keymap instead: findCommand falls back to the
		// global context whenever the focused context's binding has no handler, so
		// a registered global would fire for every context that rebinds its key.
		if cmd, ok := (&m).runHostCommand(msg.CommandID); ok {
			return m, cmd
		}
		if cmd := m.runGlobalWorkspacesCommand(msg.CommandID); cmd != nil {
			return m, cmd
		}
		// Fallback for contextual plugin commands: if entry has a key bound, forward to active plugin
		if msg.Key != "" {
			return m.forwardKeyToPlugin(appContentKeyPress(msg.Key))
		}
		return m, nil

	case version.ProductStatusMsg:
		m.setProductStatus(msg)
		m.clearDiagnosticsModal() // rebuild so the modal picks up new state
		// The single update modal reads live state through its sections, so a
		// late discovery result needs no rebuild — only an invalidation of the
		// cached geometry when the modal is open.
		if m.updateModalState != UpdateModalClosed && m.updateModal != nil {
			m.updateModal.Invalidate()
		}
		// Summarize rather than emitting one toast per product: the checks are
		// asynchronous, so per-product toasts would overwrite one another.
		if summary := m.updateToastSummary(); summary != "" && !m.updateInProgress && !m.needsRestart {
			m.ShowToast(summary, 15*time.Second)
		}
		return m, nil

	case IssuePreviewResultMsg:
		m.applyIssuePreviewData(msg.Data, msg.Error)
		return m, nil

	case issuePreviewWatchStartedMsg:
		return m, m.handleIssuePreviewWatchStarted(msg)

	case issuePreviewStoreChangedMsg:
		return m, m.handleIssuePreviewStoreChanged()

	case issueview.LoadedMsg:
		// The modal is one host of issueview. A workspace issue pane is
		// another. Claiming every LoadedMsg here left those panes stuck on
		// "Loading issue…" because the plugin never saw its own result.
		if cmd, claimed := m.claimIssuePreviewLoad(msg); claimed {
			return m, cmd
		}

	case IssueSearchResultMsg:
		// Discard stale results
		if msg.Query != m.issueSearchQuery || !m.showIssueInput {
			return m, nil
		}
		m.issueSearchLoading = false
		if msg.Error == nil {
			m.issueSearchResults = msg.Results
			// Auto-select the sole hit so it is highlighted and Enter is consistent.
			if len(m.issueSearchResults) == 1 {
				m.issueSearchCursor = 0
			}
		}
		m.issueSearchScrollOffset = 0
		m.issueInputModal = nil
		m.issueInputModalWidth = 0
		return m, nil

	case inlineedit.StartedMsg, inlineedit.ExitedMsg:
		// A pane editor's lifecycle messages are surface-tagged and reach both
		// workspace projections: the plugins get them from the broadcast below,
		// and the global browser is not a plugin, so it is offered them here.
		var appEditCmd tea.Cmd
		switch editMsg := msg.(type) {
		case inlineedit.StartedMsg:
			appEditCmd, _ = (&m).applyAppContentEditStarted(editMsg)
		case inlineedit.ExitedMsg:
			appEditCmd, _ = (&m).applyAppContentEditExited(editMsg)
		}
		if appEditCmd != nil {
			cmds = append(cmds, appEditCmd)
		}
		if m.overview != nil {
			if cmd := m.overview.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case uirequest.RequestMsg:
		if m.uiRequestWatcher != nil {
			cmds = append(cmds, listenForUIRequests(m.uiRequestWatcher.Messages()))
		}
		if msg.Request.Action == uirequest.ActionNotify {
			if cmd := (&m).handleNotifyRequest(msg.Request); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if msg.Request.Action == uirequest.ActionPluginChanged {
			if cmd := (&m).handlePluginChangedRequest(msg.Request); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if msg.Request.Action == uirequest.ActionConfigReload {
			if cmd := (&m).handleConfigReloadRequest(msg.Request); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if msg.Request.Action == uirequest.ActionSwitchProject {
			if cmd := (&m).handleSwitchProjectRequest(msg.Request); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if cmd, handled := (&m).handleAppContentUIRequest(msg.Request); handled {
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
		landing := m.uiRequestLanding(msg.Request)
		relayed := msg.Request.Origin.HostID != ""
		if m.overview != nil && (!relayed || landing != uiRequestLandingBoundWorkspace) {
			if cmd := m.overview.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.sessionsOwnsCreateSplit(msg.Request) || (relayed && landing != uiRequestLandingBoundWorkspace) {
			return m, tea.Batch(cmds...)
		}
	}

	// Unparsed terminal input (CSI u / modifyOtherKeys sequences) is keyboard
	// input in disguise: while a global view sidecar draws itself holds focus,
	// it must not reach a hidden interactive pane, same as a regular key press.
	if m.globalOverlayOwnsKeys() && tty.ExtractUnknownCSIBytes(msg) != nil {
		// Unless a global surface is itself driving a terminal, in which case the
		// sequence is that pane's input and is delivered as the key it encodes.
		if m.globalWorkspacesVisible() {
			if handled, cmd := m.overview.WorkspacesTerminalKeySequence(msg); handled {
				return m, cmd
			}
		}
		return m, nil
	}

	// An embedded terminal's own messages are scope-tagged, so the global
	// Workspaces browser is offered every one of them alongside the plugins:
	// whichever activation owns the scope acts on it and the rest ignore it.
	if tty.IsTerminalMessage(msg) {
		if cmd, handled := (&m).routeAppContentEditMsg(msg); handled && cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.overview != nil && tty.IsTerminalMessage(msg) {
		if cmd := m.overview.WorkspacesTerminalMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if ready, ok := msg.(gitinit.ReadyMsg); ok && ready.Root != "" {
		m.refreshWorktreeCache()
	}

	// Forward other messages to ALL plugins (not just active)
	// This ensures plugin-specific messages (like SessionsLoadedMsg) reach
	// their target plugin even when another plugin is focused
	plugins := m.registry.Plugins()
	for i, p := range plugins {
		newPlugin, cmd := p.Update(msg)
		plugins[i] = newPlugin
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// The global plugin hosts are not in the registry, so they are forwarded
	// here. This is what keeps a global plugin's file watch, ticks, and queues
	// running while any other tab — global or project — is visible.
	if cmd := m.updateGlobalHosts(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.updateContext()

	return m, tea.Batch(cmds...)
}

func (m *Model) forwardApplicationFocus(msg tea.Msg) tea.Cmd {
	// Geometry arbitration is process-wide and must see focus even if no plugin
	// with a control manager is loaded (td-ee222a).
	tty.SetAppFocused(m.applicationFocused)

	var cmds []tea.Cmd
	if m.overview != nil {
		if cmd := m.overview.SetApplicationFocused(m.applicationFocused); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for _, p := range m.registry.Plugins() {
		_, cmd := p.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := m.updateGlobalHosts(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// handleKeyMsg processes keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Close modals with escape (priority order via activeModal)
	if msg.Code == tea.KeyEsc {
		switch m.activeModal() {
		case ModalPalette:
			if m.palette.Query() != "" {
				var cmd tea.Cmd
				m.palette, cmd = m.palette.Update(msg)
				return m, cmd
			}
			m.showPalette = false
			m.updateContext()
			return m, nil
		case ModalHelp:
			m.showHelp = false
			m.clearHelpModal()
			return m, nil
		case ModalUpdate:
			// Esc closes the confirmation and result phases, and hides the
			// modal during an install (the batch keeps running; every entry
			// point reopens it in its current phase).
			m.closeUpdateModal()
			return m, nil
		case ModalDiagnostics:
			m.showDiagnostics = false
			return m, nil
		case ModalQuitConfirm:
			m.showQuitConfirm = false
			return m, nil
		case ModalProjectSwitcher:
			// If in add mode, Esc exits back to list
			if m.projectAddMode {
				m.resetProjectAdd()
				return m, nil
			}
			// Esc: clear filter if set, otherwise close
			if m.projectSwitcherInput.Value() != "" {
				m.projectSwitcherInput.SetValue("")
				m.projectSwitcherFiltered = m.projectSwitcherDestinations("")
				m.projectSwitcherCursor = 0
				m.projectSwitcherScroll = 0
				return m, nil
			}
			cmd := m.resetProjectSwitcher()
			m.updateContext()
			return m, cmd
		case ModalWorktreeSwitcher:
			// Esc: clear filter if set, otherwise close
			if m.worktreeSwitcherInput.Value() != "" {
				m.worktreeSwitcherInput.SetValue("")
				m.worktreeSwitcherFiltered = m.worktreeSwitcherAll
				m.worktreeSwitcherCursor = 0
				m.worktreeSwitcherScroll = 0
				return m, nil
			}
			m.resetWorktreeSwitcher()
			m.updateContext()
			return m, nil
		case ModalIssueInput:
			m.resetIssueInput()
			m.updateContext()
			return m, nil
		case ModalIssuePreview:
			if m.issuePreviewView != nil && m.issuePreviewView.Active() {
				m.issuePreviewView.SetActive(false)
				return m, nil
			}
			m.resetIssuePreview()
			m.resetIssueInput()
			m.updateContext()
			return m, nil
		case ModalPaneReposition:
			return m, m.handleAppPaneLayoutKey(msg)
		case ModalPaneSwitcher:
			// The form owns Esc: on the picker step it returns to the kind list
			// rather than closing, which is the same two-step flow both
			// Workspaces hosts have. Only the kind step's cancel closes.
			return m.handlePaneSwitcherKey(msg)
		case ModalThemeSwitcher:
			// Esc: clear filter if set, otherwise close (restore original)
			if m.themeSwitcherInput.Value() != "" {
				m.themeSwitcherInput.SetValue("")
				m.themeSwitcherFiltered = buildUnifiedThemeList()
				m.themeSwitcherSelectedIdx = 0
				return m, nil
			}
			cmd := m.previewThemeEntry(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
			return m, cmd
		case ModalNone:
			// The notification centre is the focused surface when it has the
			// keyboard, and esc is its explicit close — the only kind of close
			// there is. It answers before Configuration and before the global
			// space, because focus, not layering, decides who owns esc.
			if m.notificationCentreOwnsKeys() {
				return m, m.closeNotificationCentre()
			}
			// Configuration answers esc itself: clear the search, then return
			// from a focused child route, then close and restore the surface it
			// covered.
			if m.configOpen() {
				return m, m.configEscape()
			}
			// No modal: Esc leaves the global space and returns to the project
			// plugin underneath — unless the focused global surface wants esc
			// itself. The hosted Tasks tab is a real surface whose overlays,
			// pickers, and prompts close on esc through precedence level 2; this
			// branch runs before that level, so without the guard esc would yank
			// the user out of the global space and leave the overlay open.
			if m.inGlobalScope() && !m.globalSurfaceWantsEsc() {
				return m, m.exitOverview()
			}
		}
	}

	if m.showQuitConfirm {
		action, cmd := m.quitModal.HandleKey(msg)
		switch action {
		case "quit":
			// Save active plugin before quitting
			m.shutdown()
			return m, quitWithInstanceWithdrawal()
		case "cancel":
			m.showQuitConfirm = false
			return m, nil
		}
		return m, cmd
	}

	// Handle update modal keys
	if m.updateModalState != UpdateModalClosed {
		return m.handleUpdateModalKey(msg)
	}

	if _, controller := m.activePaneLayoutController(); controller != nil {
		return m, m.handleAppPaneLayoutKey(msg)
	}

	// The pane switcher owns the keyboard while it is up, like every other
	// app-level modal. hasModal() already keeps the rungs below off it; this is
	// where its own keys are answered.
	if m.paneSwitcherOpen {
		return m.handlePaneSwitcherKey(msg)
	}

	// The notification centre owns the keyboard while it is focused, so it
	// answers before every surface below — including Configuration, which it
	// sits beside rather than under. It claims only its own list keys: tab
	// numbers, the project selector, and quit keep working underneath, which is
	// what makes it a panel and not a modal.
	if !m.hasModal() && m.notificationCentreOwnsKeys() {
		if handled, cmd := m.notificationCentreKey(msg); handled {
			return m, cmd
		}
	}

	// With the panel open it is also a stop on the focus cycle: the focused
	// surface keeps cycling its own panes, and the press that would have
	// wrapped its ring lands here instead. This has to run before the surfaces
	// below see the key, and it declines every context that owns tab for
	// something of its own — see notificationCentreTabKey.
	if handled, cmd := m.notificationCentreTabKey(msg); handled {
		m.updateContext()
		return m, cmd
	}

	// Configuration covers the content area, so it answers before any of
	// sidecar's global switches: a tab number, `q`, or a printable key typed
	// into Search must not reach the plugin hidden underneath.
	if !m.hasModal() && m.configOpen() {
		return m.configKey(msg)
	}

	// The placeholder behind an empty global Sessions tab offers the same
	// contextual route the project sidebar does, and Enter is it.
	if !m.hasModal() && m.globalWorkspacesPlaceholderVisible() && m.configurationBlocked() && msg.String() == "enter" {
		return m, OpenConfiguration(configui.PageSetup)
	}

	// The global Workspaces browser answers for its own keys before sidecar's
	// global switch runs. It has to be here rather than beside the Agents board
	// below: while its filter has focus every printable key is query text, and
	// the tab/number/quit switches further down would otherwise take "q", "1",
	// and "`" out of the middle of a search.
	if !m.hasModal() && m.globalWorkspacesVisible() {
		if handled, cmd := m.overview.WorkspacesKey(msg); handled {
			m.updateContext()
			return m, cmd
		}
	}

	// Interactive/inline edit mode: forward ALL keys to plugin including ctrl+c
	// This ensures characters like `, ~, ?, !, @, q, 1-5 reach tmux instead of triggering app shortcuts
	// Ctrl+C is forwarded to tmux (to interrupt running processes) instead of showing quit dialog
	// User can exit interactive mode with Ctrl+\ first, then quit normally
	// An open modal takes keyboard focus away from the pane; the plugin keeps its
	// mode, so focus returns to it when the modal closes.
	// A global view covers the plugin pane and owns keyboard focus, so a plugin
	// left in interactive/text-input mode underneath it must not swallow keys.
	if !m.hasModal() && !m.globalOverlayOwnsKeys() {
		if cmd, handled := m.handleAppContentEditKey(msg); handled {
			m.updateContext()
			return m, cmd
		}
	}
	// A finder or project search belongs to the app-owned document leaf, not to
	// the primary plugin under it. It takes the same precedence as the two
	// Workspace hosts' pane searches: every key is query input except ctrl+c,
	// which remains Sidecar's interrupt.
	if !m.hasModal() && !m.globalOverlayOwnsKeys() && m.appContentSearchActive() {
		if msg.String() == "ctrl+c" {
			m.initQuitModal()
			m.showQuitConfirm = true
			return m, nil
		}
		cmd, _ := m.handleAppContentKey(msg)
		m.updateContext()
		return m, cmd
	}
	if !m.hasModal() && !m.globalOverlayOwnsKeys() &&
		(m.activeContext == "workspace-interactive" || m.activeContext == "file-browser-inline-edit" ||
			m.activeContext == "notes-inline-edit" || m.activeContext == "workspace-doc-edit") {
		// Forward ALL keys to plugin (exit keys and ctrl+c handled by plugin)
		return m.forwardKeyToPlugin(msg)
	}

	// Precedence level 2: the active plugin's text-input or blocking-overlay
	// context. Forward all keys to the plugin except ctrl+c.
	// Uses plugin runtime capability first, then app-level fallback contexts.
	// Skipped while a modal is open so the modal's own input gets the keys.
	if !m.hasModal() && !m.globalOverlayOwnsKeys() && (m.consumesTextInput() || m.pluginBlocksGlobalKeys()) {
		// ctrl+c shows quit confirmation
		if msg.String() == "ctrl+c" {
			if !m.hasModal() {
				m.initQuitModal()
				m.showQuitConfirm = true
			}
			return m, nil
		}
		// Forward everything else to plugin (esc, alt+enter handled by plugin)
		return m.forwardKeyToPlugin(msg)
	}

	// Once passive content exists, the app owns the combined inner+outer focus
	// ring and passive-leaf keys. Inputs and blocking overlays above retain Tab.
	if !m.hasModal() && !m.globalOverlayOwnsKeys() {
		if cmd, handled := m.handleAppContentKey(msg); handled {
			m.updateContext()
			return m, cmd
		}
	}

	// The pane switcher's entry, bound once here rather than once per plugin:
	// the deck it opens into is the app's and so is the routing (decision 6).
	//
	// This rung sits deliberately BELOW every surface that types. `ctrl+n` is
	// cursor-down in the global context and in each filter, finder, search and
	// editor context, and all of those have already claimed the key by the time
	// execution reaches here — an inline edit and workspace-interactive forward
	// wholesale two rungs up, and precedence level 2 forwards every key of a
	// plugin's text-input or blocking-overlay context (which is what
	// notes-search, notes-editor, file-browser-quick-open and
	// file-browser-project-search each report through ConsumesTextInput). A live
	// PTY is reached the same way, so no control character is stolen from one.
	// paneSwitcherAvailable re-checks textInputFocused rather than trusting the
	// order alone.
	//
	// The key comes from the keymap rather than from a constant, so this one
	// rung serves both shapes the entry has: `ctrl+n` in a plugin's browse
	// context, and the `n` a focused passive leaf's context already binds on the
	// two Workspaces surfaces. A context that never named open-pane never
	// matches, whatever key it was.
	if !m.hasModal() && !m.globalOverlayOwnsKeys() && m.paneSwitcherClaimsKey(msg.String()) {
		if cmd, opened := m.openPaneSwitcher(); opened {
			m.updateContext()
			return m, cmd
		}
	}

	// Precedence level 3: an active plugin contextual binding beats sidecar's
	// global bindings. Only plugins that implement plugin.KeyRouter take part,
	// and pluginClaimsKey refuses the host's reserved keys (keymap.HostReservedKeys),
	// so this cannot capture ctrl+c, the quit flow, or merged help.
	if m.pluginClaimsKey(msg.String()) {
		// A user keymap override outranks a plugin claim. Plan §1.4 offers the
		// override as the way to change a claimed mapping ("through Sidecar's
		// keymap override rather than forking the Tasks registry"), and level 3
		// runs before keymap.Handle, so without this the documented escape
		// hatch would be unreachable for exactly the keys that need it.
		//
		// Deliberately scoped to keys a plugin actually claims: consulting
		// overrides for every key here would move them ahead of sidecar's own
		// global switch too, which is a different change to a different level
		// of the ladder.
		if cmd, ok := m.keymap.UserOverride(msg); ok {
			return m, cmd
		}
		return m.forwardKeyToPlugin(msg)
	}

	// Precedence level 4: sidecar global bindings, starting with quit. ctrl+c
	// always takes precedence; 'q' quits from root plugin contexts.
	switch msg.String() {
	case "ctrl+c":
		if !m.hasModal() {
			m.initQuitModal()
			m.showQuitConfirm = true
			return m, nil
		}
	case "q":
		if !m.hasModal() && m.quitKeyExits() {
			m.initQuitModal()
			m.showQuitConfirm = true
			return m, nil
		}
		// Fall through to forward to plugin for navigation (back/escape)
	}

	// Handle palette input when open (Esc handled above)
	if m.showPalette {
		var cmd tea.Cmd
		m.palette, cmd = m.palette.Update(msg)
		return m, cmd
	}

	// Handle diagnostics modal keys
	if m.showDiagnostics {
		m.ensureDiagnosticsModal()
		if m.diagnosticsModal != nil {
			action, cmd := m.diagnosticsModal.HandleKey(msg)
			if cmd != nil {
				return m, cmd
			}
			switch action {
			case "close", "cancel":
				m.showDiagnostics = false
				return m, nil
			case "update":
				// Open the updater in whatever phase the flow is in — including
				// mid-batch, where refusing left the user blind to a running
				// install.
				if m.openUpdateModal() {
					m.updateContext()
					return m, nil
				}
			}
		}
		// Handle 'u' shortcut for update - open update modal
		if msg.String() == "u" && m.openUpdateModal() {
			m.updateContext()
			return m, nil
		}
		return m, nil
	}

	// Handle worktree switcher modal keys (Esc handled above)
	if m.showWorktreeSwitcher {
		worktrees := m.worktreeSwitcherFiltered

		switch msg.Code {
		case tea.KeyEnter:
			if m.worktreeSwitcherCursor >= 0 && m.worktreeSwitcherCursor < len(worktrees) {
				selected := worktrees[m.worktreeSwitcherCursor]
				m.resetWorktreeSwitcher()
				m.updateContext()
				return m, m.activateWorktreeSwitcherRow(selected)
			}
			return m, nil

		case tea.KeyUp:
			m.worktreeSwitcherCursor--
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil

		case tea.KeyDown:
			m.worktreeSwitcherCursor++
			if m.worktreeSwitcherCursor >= len(worktrees) {
				m.worktreeSwitcherCursor = len(worktrees) - 1
			}
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil
		}

		// Handle non-text shortcuts
		switch msg.String() {
		case "ctrl+n":
			m.worktreeSwitcherCursor++
			if m.worktreeSwitcherCursor >= len(worktrees) {
				m.worktreeSwitcherCursor = len(worktrees) - 1
			}
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil

		case "ctrl+p":
			m.worktreeSwitcherCursor--
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil

		case "W":
			// Close modal with same key
			m.resetWorktreeSwitcher()
			m.updateContext()
			return m, nil
		}

		// Filter out unparsed mouse escape sequences
		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.worktreeSwitcherInput, cmd = m.worktreeSwitcherInput.Update(msg)

		// Re-filter on input change
		m.worktreeSwitcherFiltered = filterWorktreeRows(m.worktreeSwitcherAll, m.worktreeSwitcherInput.Value())
		m.clearWorktreeSwitcherModal() // Clear modal cache on filter change
		// Reset cursor if it's beyond filtered list
		if m.worktreeSwitcherCursor >= len(m.worktreeSwitcherFiltered) {
			m.worktreeSwitcherCursor = len(m.worktreeSwitcherFiltered) - 1
		}
		if m.worktreeSwitcherCursor < 0 {
			m.worktreeSwitcherCursor = 0
		}
		m.worktreeSwitcherScroll = 0
		m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)

		return m, cmd
	}

	// Handle project switcher modal keys (Esc handled above)
	if m.showProjectSwitcher {
		// Handle project add sub-mode keys
		if m.projectAddMode {
			return m.handleProjectAddModalKeys(msg)
		}

		allProjects := m.cfg.Projects.List
		if len(allProjects) == 0 && !m.globalScopeAvailable() {
			// No projects configured - handle y for LLM prompt, ctrl+a for add, close on q/@
			switch msg.String() {
			case "enter":
				// The switcher has nothing to switch to, so enter is free for
				// the route that fixes that. ctrl+a's direct add stays exactly
				// as it was; this is the way into the rest of Setup.
				resetCmd := m.resetProjectSwitcher()
				m.updateContext()
				return m, tea.Batch(resetCmd, OpenConfiguration(configui.PageSetup))
			case "y":
				return m, m.copyProjectSetupPrompt()
			case "ctrl+a":
				m.initProjectAdd()
				return m, nil
			case "q", "@":
				cmd := m.resetProjectSwitcher()
				m.updateContext()
				return m, cmd
			}
			return m, nil
		}

		projects := m.projectSwitcherFiltered

		// The + button takes focus from the filter input via tab or right
		// arrow; enter or space then opens add-project.
		if m.projectSwitcherAddFocused {
			switch msg.String() {
			case "enter", " ", "space":
				m.projectSwitcherAddFocused = false
				m.initProjectAdd()
				return m, nil
			case "tab", "shift+tab", "left", "backtab":
				m.projectSwitcherAddFocused = false
				return m, nil
			case "up", "down", "ctrl+n", "ctrl+p":
				m.projectSwitcherAddFocused = false
				// fall through to the normal handling below
			default:
				// Typing returns to the filter input.
				m.projectSwitcherAddFocused = false
			}
		} else {
			switch msg.String() {
			case "tab":
				m.projectSwitcherAddFocused = true
				return m, nil
			case "right":
				// Only when the caret is already at the end, so right arrow
				// still moves through filter text.
				if m.projectSwitcherInput.Position() >= len(m.projectSwitcherInput.Value()) {
					m.projectSwitcherAddFocused = true
					return m, nil
				}
			}
		}

		switch msg.Code {
		case tea.KeyEnter:
			// Select project and switch to it
			if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(projects) {
				return m, m.activateProjectSwitcherDestination(projects[m.projectSwitcherCursor])
			}
			return m, nil

		case tea.KeyUp:
			m.projectSwitcherCursor--
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, m.previewProjectTheme()

		case tea.KeyDown:
			m.projectSwitcherCursor++
			if m.projectSwitcherCursor >= len(projects) {
				m.projectSwitcherCursor = len(projects) - 1
			}
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, m.previewProjectTheme()
		}

		// Handle non-text shortcuts
		switch msg.String() {
		case "ctrl+n":
			m.projectSwitcherCursor++
			if m.projectSwitcherCursor >= len(projects) {
				m.projectSwitcherCursor = len(projects) - 1
			}
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, m.previewProjectTheme()

		case "ctrl+p":
			m.projectSwitcherCursor--
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, m.previewProjectTheme()

		case "ctrl+a":
			m.initProjectAdd()
			return m, nil

		case "@":
			// Close modal
			cmd := m.resetProjectSwitcher()
			m.updateContext()
			return m, cmd
		}

		// Filter out unparsed mouse escape sequences
		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		return m, m.updateProjectSwitcherFilter(msg)
	}

	// Handle theme switcher modal keys (Esc handled above)
	if m.showThemeSwitcher {
		// ctrl+s or left/right toggles scope between global and project
		if m.currentProjectConfig() != nil {
			switch msg.String() {
			case "ctrl+s", "left", "right":
				if m.themeSwitcherScope == "global" {
					m.themeSwitcherScope = "project"
				} else {
					m.themeSwitcherScope = "global"
				}
				return m, nil
			}
		}

		themes := m.themeSwitcherFiltered

		switch msg.Code {
		case tea.KeyEnter:
			// Confirm selection and close (ignore separators)
			if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				entry := themes[m.themeSwitcherSelectedIdx]
				var tc config.ThemeConfig
				if entry.IsBuiltIn {
					tc = config.ThemeConfig{Name: entry.ThemeKey}
				} else {
					tc = config.ThemeConfig{Name: "default", Community: entry.ThemeKey}
				}
				return m, tea.Batch(m.previewThemeEntry(entry), m.confirmThemeSelection(tc, entry.Name))
			}
			return m, nil

		case tea.KeyUp:
			m.themeSwitcherSelectedIdx--
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			// Skip separators
			for m.themeSwitcherSelectedIdx > 0 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx--
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				return m, m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case tea.KeyDown:
			m.themeSwitcherSelectedIdx++
			if m.themeSwitcherSelectedIdx >= len(themes) {
				m.themeSwitcherSelectedIdx = len(themes) - 1
			}
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			// Skip separators
			for m.themeSwitcherSelectedIdx < len(themes)-1 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx++
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				return m, m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil
		}

		// Handle non-text shortcuts
		switch msg.String() {
		case "ctrl+n":
			m.themeSwitcherSelectedIdx++
			if m.themeSwitcherSelectedIdx >= len(themes) {
				m.themeSwitcherSelectedIdx = len(themes) - 1
			}
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			for m.themeSwitcherSelectedIdx < len(themes)-1 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx++
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				return m, m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case "ctrl+p":
			m.themeSwitcherSelectedIdx--
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			for m.themeSwitcherSelectedIdx > 0 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx--
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				return m, m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case "#":
			// Close modal and restore original
			cmd := m.previewThemeEntry(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
			return m, cmd
		}

		// Filter out unparsed mouse escape sequences
		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.themeSwitcherInput, cmd = m.themeSwitcherInput.Update(msg)

		// Re-filter on input change
		m.themeSwitcherFiltered = filterThemeEntries(buildUnifiedThemeList(), m.themeSwitcherInput.Value())
		m.clearThemeSwitcherModal() // Force modal rebuild
		if m.themeSwitcherSelectedIdx >= len(m.themeSwitcherFiltered) {
			m.themeSwitcherSelectedIdx = len(m.themeSwitcherFiltered) - 1
		}
		if m.themeSwitcherSelectedIdx < 0 {
			m.themeSwitcherSelectedIdx = 0
		}

		// Live preview current selection (skip separators)
		if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(m.themeSwitcherFiltered) && !m.themeSwitcherFiltered[m.themeSwitcherSelectedIdx].IsSeparator {
			cmd = tea.Batch(cmd, m.previewThemeEntry(m.themeSwitcherFiltered[m.themeSwitcherSelectedIdx]))
		}

		return m, cmd
	}

	// Handle Open In modal keys (Esc handled above)
	if m.showOpenIn {
		m.ensureOpenInModal()
		if m.openInModal != nil {
			action, cmd := m.openInModal.HandleKey(msg)
			switch action {
			case "cancel":
				m.resetOpenIn()
				m.updateContext()
				return m, nil
			case "select":
				return m, m.confirmOpenIn()
			}
			if cmd != nil {
				return m, cmd
			}
		}
		return m, nil
	}

	// Handle issue input modal keys
	if m.showIssueInput {
		// ctrl+x toggles closed issue visibility (before type switch)
		if msg.String() == "ctrl+x" {
			m.issueSearchIncludeClosed = !m.issueSearchIncludeClosed
			m.issueSearchScrollOffset = 0
			m.issueSearchCursor = -1
			m.issueInputModal = nil
			m.issueInputModalWidth = 0
			if len(strings.TrimSpace(m.issueInputInput.Value())) >= 2 {
				m.issueSearchLoading = true
				return m, issueSearchCmd(m.ui.WorkDir, strings.TrimSpace(m.issueInputInput.Value()), m.issueSearchIncludeClosed)
			}
			return m, nil
		}

		switch msg.Code {
		case tea.KeyEnter:
			return m.issueInputSubmit()
		case tea.KeyUp:
			if len(m.issueSearchResults) > 0 {
				m.issueSearchCursor--
				if m.issueSearchCursor < -1 {
					m.issueSearchCursor = -1
				}
				// Keep cursor visible in viewport
				if m.issueSearchCursor >= 0 && m.issueSearchCursor < m.issueSearchScrollOffset {
					m.issueSearchScrollOffset = m.issueSearchCursor
				}
				m.issueInputModal = nil
				m.issueInputModalWidth = 0
				return m, nil
			}
		case tea.KeyDown:
			if len(m.issueSearchResults) > 0 {
				m.issueSearchCursor++
				if m.issueSearchCursor >= len(m.issueSearchResults) {
					m.issueSearchCursor = len(m.issueSearchResults) - 1
				}
				// Keep cursor visible in viewport
				const maxVisible = 10
				if m.issueSearchCursor >= m.issueSearchScrollOffset+maxVisible {
					m.issueSearchScrollOffset = m.issueSearchCursor - maxVisible + 1
				}
				m.issueInputModal = nil
				m.issueInputModalWidth = 0
				return m, nil
			}
		case tea.KeyTab:
			if m.issueSearchCursor >= 0 && m.issueSearchCursor < len(m.issueSearchResults) {
				m.issueInputInput.SetValue(m.issueSearchResults[m.issueSearchCursor].ID)
				m.issueInputInput.CursorEnd()
				m.issueInputModal = nil
				m.issueInputModalWidth = 0
			}
			// Tab is consumed (fill-in or no-op) — don't forward to textinput
			return m, nil
		}

		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		// Forward key to text input, then clear modal cache so it rebuilds
		var cmd tea.Cmd
		m.issueInputInput, cmd = m.issueInputInput.Update(msg)
		m.issueInputModal = nil
		m.issueInputModalWidth = 0

		// Trigger search if input changed (min 2 chars)
		newValue := strings.TrimSpace(m.issueInputInput.Value())
		if newValue != m.issueSearchQuery && len(newValue) >= 2 {
			m.issueSearchQuery = newValue
			m.issueSearchLoading = true
			// Keep previous results visible while loading to avoid modal shrink/grow flicker.
			// Results are replaced when the new IssueSearchResultMsg arrives.
			m.issueSearchCursor = -1
			return m, tea.Batch(cmd, issueSearchCmd(m.ui.WorkDir, newValue, m.issueSearchIncludeClosed))
		}
		if len(newValue) < 2 {
			m.issueSearchResults = nil
			m.issueSearchQuery = ""
			m.issueSearchCursor = -1
		}
		return m, cmd
	}

	// Handle issue preview modal keys
	if m.showIssuePreview {
		m.ensureIssuePreviewModal()
		if m.issuePreviewModal == nil {
			return m, nil
		}
		view := m.ensureIssuePreviewView()
		key := msg.String()

		// Enter on the card (or before buttons take it) activates rather than
		// firing Open in TD. After that, arrows belong to the epic.
		if key == "enter" && view != nil && !view.Active() &&
			(m.issuePreviewModal.FocusedID() == "" || m.issuePreviewModal.FocusedID() == issueViewFocusID) {
			view.SetActive(true)
			view.SetFocused(true)
			m.issuePreviewModal.SetFocus(issueViewFocusID)
			return m, nil
		}

		if view != nil && view.Active() && m.issuePreviewModal.FocusedID() == issueViewFocusID {
			handled, cmd := view.HandleKey(msg)
			if handled {
				return m, cmd
			}
		}

		// Inactive (or unhandled): j/k/arrows scroll the card, not the modal
		// chrome — the card owns its own viewport.
		if view != nil && !view.Active() {
			switch key {
			case "j", "down":
				view.Scroll(1)
				return m, nil
			case "k", "up":
				view.Scroll(-1)
				return m, nil
			case "ctrl+d":
				view.Scroll(10)
				return m, nil
			case "ctrl+u":
				view.Scroll(-10)
				return m, nil
			case "g":
				view.Scroll(-10000)
				return m, nil
			case "G":
				view.Scroll(10000)
				return m, nil
			}
		}

		switch key {
		case "o", "O":
			// O is the same request the issue panes answer; the modal has
			// always answered o, so both reach td.
			if d := m.previewIssueData(); d != nil {
				issueID := d.ID
				m.resetIssuePreview()
				m.resetIssueInput()
				m.updateContext()
				return m, OpenIssueInTD(issueID)
			}
		case "b":
			m.backToIssueInput()
			return m, nil
		case "y":
			if d := m.previewIssueData(); d != nil {
				return m, issueview.CopyMarkdown(d)
			}
		case "Y", "shift+y":
			if d := m.previewIssueData(); d != nil {
				return m, issueview.CopyID(d)
			}
		}

		action, cmd := m.issuePreviewModal.HandleKey(msg)
		if key == "tab" || key == "shift+tab" {
			if view != nil && m.issuePreviewModal.FocusedID() != issueViewFocusID {
				view.SetActive(false)
				view.SetFocused(false)
			} else if view != nil {
				view.SetFocused(true)
			}
		}
		switch action {
		case "open-in-td":
			issueID := ""
			if d := m.previewIssueData(); d != nil {
				issueID = d.ID
			}
			m.resetIssuePreview()
			m.resetIssueInput()
			m.updateContext()
			return m, OpenIssueInTD(issueID)
		case "back":
			m.backToIssueInput()
			return m, nil
		case "cancel":
			m.resetIssuePreview()
			m.resetIssueInput()
			m.updateContext()
			return m, nil
		}
		return m, cmd
	}

	// If any modal is open, don't process plugin/toggle keys
	if m.hasModal() {
		return m, nil
	}

	if m.agentsBoardVisible() && m.overview != nil {
		switch msg.String() {
		case "left", "h", "right", "l", "up", "k", "down", "j", "enter", "r":
			return m, m.overview.Update(msg)
		case "K":
			// Same key that opened it closes it.
			return m, m.toggleOverview()
		}
	}

	// Tab switching. The header is one row of entries — the global ones the
	// left cluster paints (Sessions / Activity / Tasks) followed by the
	// project's plugin tabs — and these keys address that one row.
	//
	// Cycling wraps through all of it, in both scopes, so `]` from the last
	// plugin tab lands on Sessions and `[` brings it back. The number row is
	// positional for the project tabs (1-7) and named for the global entries
	// (8/9/0), which is what makes 8 mean Sessions everywhere rather than
	// "the eighth thing in whichever list you happen to be looking at".
	switch msg.String() {
	case "`", "]":
		// Next header entry (except in text input contexts).
		if m.consumesTextInput() {
			break
		}
		return m, m.cycleTabs(1)
	case "~", "[":
		// Previous header entry. `~` is kept as the long-standing alias for
		// `[`, exactly as `` ` `` is for `]`; retiring it would break muscle
		// memory for nothing.
		if m.consumesTextInput() {
			break
		}
		return m, m.cycleTabs(-1)
	case "1", "2", "3", "4", "5", "6", "7":
		// Positional project tabs.
		// Block in text input contexts (user is typing numbers)
		if m.consumesTextInput() {
			break
		}
		return m, m.selectProjectTabByNumber(int([]rune(msg.String())[0] - '1'))
	case "8", "9", "0":
		// The header's global entries, addressed by name. These three keys are
		// the global space's whatever the row contains, so a key whose entry is
		// disabled does nothing at all rather than falling through to a plugin
		// tab — silently, which is the same answer 1-7 give for a plugin index
		// that does not exist.
		if m.consumesTextInput() {
			break
		}
		tab, ok := m.globalTabForKey(msg.String())
		if !ok {
			return m, nil
		}
		return m, m.selectGlobalTab(tab)
	}

	// Toggles
	switch msg.String() {
	case "?":
		m.showPalette = !m.showPalette
		if m.showPalette {
			// Open palette with current context
			pluginCtx := "global"
			if p := m.focusedSurface(); p != nil {
				pluginCtx = p.ID()
			}
			m.palette.SetSize(m.width, m.height)
			// surfacePlugins includes the global Tasks host. A passive app-owned
			// leaf contributes its own command projection without changing the
			// underlying plugin's mandatory interface.
			surfaces := m.surfacePlugins()
			if commands := m.appContentCommands(); len(commands) > 0 {
				surfaces = append(surfaces, appContentCommandPlugin{Plugin: m.focusedSurface(), commands: commands})
			}
			// The pane switcher's entry is the app's, contributed for whichever
			// plugin browse context is on screen — same reason, same wrapper.
			if commands := m.paneSwitcherCommands(); len(commands) > 0 {
				surfaces = append(surfaces, appContentCommandPlugin{Plugin: m.focusedSurface(), commands: commands})
			}
			m.palette.Open(m.keymap, surfaces, m.activeContext, pluginCtx)
			m.activeContext = "palette"
		} else {
			m.updateContext()
		}
		return m, nil
	case "!":
		m.showDiagnostics = !m.showDiagnostics
		if m.showDiagnostics {
			m.activeContext = "diagnostics"
			// Force version check in background (bypasses cache)
			return m, tea.Batch(m.productCheckCmds(true)...)
		}
		m.clearDiagnosticsModal()
		m.updateContext()
		return m, nil
	case "@":
		// Toggle project switcher modal
		m.showProjectSwitcher = !m.showProjectSwitcher
		if m.showProjectSwitcher {
			m.activeContext = "project-switcher"
			m.initProjectSwitcher()
		} else {
			cmd := m.resetProjectSwitcher()
			m.updateContext()
			return m, cmd
		}
		return m, nil
	case "N", "alt+n":
		// The notification centre. Bare `n` is taken several times over
		// (new-worktree, new-note, next-match); `N` is free in the global
		// context, and the search/diff contexts that bind it are answered at
		// precedence level 3 before this switch runs.
		//
		// `alt+n` is the guaranteed route (`ctrl+n` is cursor-down everywhere). `N` is genuinely taken in some
		// plugins — git binds it to prev-match, which is worth more there than
		// this toggle — and the centre has no navbar tab, so without a key that
		// no context rebinds it would be reachable only by mouse on those tabs.
		if m.consumesTextInput() {
			break
		}
		if msg.String() == "N" && m.contextRebindsKey("N") {
			break
		}
		// Open but not focused — the user navigated away with a tab key — means
		// `N` is a request to go back to it, not to close something they are not
		// looking at. Pressing it again from there closes, as it always has.
		if m.notificationCentreVisible() && !m.notificationCentreFocused {
			m.focusNotificationCentre()
			m.readSelectedNotification()
			return m, nil
		}
		return m, m.toggleNotificationCentre()
	case "d":
		// `d` dismisses the toast on screen, exactly as the toast's own key row
		// says — but only where `d` is otherwise free. Precedence level 3 covers
		// plugins implementing plugin.KeyRouter, which is `tasks` alone; git
		// status and the workspace list bind `d` at level 5, *after* this switch,
		// so without the contextRebindsKey guard a toast on screen would swallow
		// their diff key for the length of its countdown.
		if m.hasModal() || m.consumesTextInput() || m.contextRebindsKey("d") {
			break
		}
		if m.dismissVisibleToast() {
			return m, m.syncToastReveal(time.Now())
		}
	case toastExpandKey:
		// The expand affordance on a collapsed stack (design 1b). `tab` is the
		// design's key and Phase 2 spent it on the focus cycle, so this is
		// `alt+e` — global, like `alt+n`, since a toast can be on screen on any
		// tab and has no focus context of its own to route through. It falls
		// through untouched when there is nothing collapsed to open.
		if m.hasModal() || m.consumesTextInput() || m.contextRebindsKey(toastExpandKey) {
			break
		}
		if m.toggleToastExpand() {
			return m, m.syncToastReveal(time.Now())
		}
	case "K":
		// Toggle cross-project Overview (Kanban). Blocked in text-input contexts
		// above. Workspace shell delete is D (with confirm); this stays global.
		if m.consumesTextInput() {
			break
		}
		return m, m.toggleOverview()
	case "W":
		// Toggle worktree switcher modal (capital W)
		if m.showWorktreeSwitcher {
			m.resetWorktreeSwitcher()
			m.updateContext()
			return m, nil
		}
		return m, m.openWorktreeSwitcher()
	case "#":
		// Toggle theme switcher modal
		m.showThemeSwitcher = !m.showThemeSwitcher
		if m.showThemeSwitcher {
			m.activeContext = "theme-switcher"
			m.initThemeSwitcher()
		} else {
			cmd := m.previewThemeEntry(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
			return m, cmd
		}
		return m, nil
	case ",":
		// The conventional settings key in a TUI, and free in sidecar's global
		// context. A context that binds it for itself — the Workspaces diff
		// leaf cycles target tabs with it — answers first, exactly as `i` does
		// below. Configuration reopens where the user last left it, and comma
		// closes it again the way the gear does. (An open surface answers comma
		// in configKey; this branch is the closed case.)
		if m.contextRebindsKey(",") {
			break
		}
		if !m.hasModal() && !m.consumesTextInput() {
			return m, m.toggleConfiguration()
		}
		return m, nil
	case "^":
		// Toggle Open In modal
		if !m.hasModal() {
			m.showOpenIn = true
			m.activeContext = "open-in"
			m.initOpenIn()
		}
		return m, nil
	case "i":
		// A context that binds "i" for itself answers before the issue modal,
		// or the binding help advertises could never fire. Workspaces no longer
		// takes the key — Enter / E / click start typing — so find-TD-task
		// stays reachable on those lists.
		if m.contextRebindsKey("i") {
			break
		}
		if m.openIssueInput() {
			return m, nil
		}
	case "r":
		// Forward 'r' to plugin in contexts where it's used for specific actions
		// or where the user is typing text input
		if !isGlobalRefreshContext(m.activeContext) {
			// Fall through to forward to plugin
			break
		}
		return m, Refresh()
	}

	// Try keymap for context-specific bindings
	if cmd := m.keymap.Handle(msg, m.activeContext); cmd != nil {
		return m, cmd
	}

	// A global view sidecar draws itself covers the plugin pane: unhandled keys
	// stop here instead of reaching a plugin the user cannot see.
	if m.globalOverlayOwnsKeys() {
		return m, nil
	}

	// Precedence level 5: unbound input is forwarded to the active plugin.
	return m.forwardKeyToPlugin(msg)
}

// updateContext sets activeContext based on current state.
// An open modal owns the context; only when none is open does the active
// plugin decide it. Closing a modal therefore restores the plugin's context
// (including "workspace-interactive") on the next call, with no per-modal
// bookkeeping.
func (m *Model) updateContext() {
	if ctx, ok := modalFocusContext(m.activeModal()); ok {
		m.activeContext = ctx
		return
	}
	// A modal takes the keyboard from the panel while it is up, and the panel
	// takes it back when the modal closes — without ever having closed itself.
	if m.notificationCentreOwnsKeys() {
		m.activeContext = notificationCentreContext
		return
	}
	if m.configOpen() {
		// "config" for navigation, "config-edit" while an editor has the
		// keyboard. The edit context is in isTextInputContext, so typing there
		// can never reach a global shortcut.
		m.activeContext = m.config.FocusContext()
		return
	}
	if m.inGlobalScope() {
		// The visible global tab owns the context. Tasks reports its own, so a
		// Tasks overlay keeps sidecar's globals off its keyboard.
		if host := m.globalPluginPlugin(); host != nil {
			if ctx, ok := m.appContentContext(); ok {
				m.activeContext = ctx
			} else {
				m.activeContext = host.FocusContext()
			}
			return
		}
		if m.globalWorkspacesVisible() && m.overview != nil {
			// Includes the filter, rename prompt, a focused document or
			// issue leaf, and typing. Those contexts are not the list's.
			m.activeContext = m.overview.WorkspaceFocusContext()
			return
		}
		if tab, ok := m.activeGlobalSurface(); ok && tab.context != "" {
			m.activeContext = tab.context
		} else {
			m.activeContext = "overview"
		}
		return
	}
	if p := m.ActivePlugin(); p != nil {
		if ctx, ok := m.appContentContext(); ok {
			m.activeContext = ctx
		} else {
			m.activeContext = p.FocusContext()
		}
	} else {
		m.activeContext = "global"
	}
}

// pluginCommandHandler finds a plugin command handler for a palette selection.
// A handler declared for the selected context wins over one declared elsewhere.
func (m *Model) pluginCommandHandler(commandID, context string) func() tea.Cmd {
	for _, cmd := range m.appContentCommands() {
		if cmd.ID == commandID && cmd.Context == context && cmd.Handler != nil {
			return cmd.Handler
		}
	}
	for _, cmd := range m.paneSwitcherCommands() {
		if cmd.ID == commandID && cmd.Context == context && cmd.Handler != nil {
			return cmd.Handler
		}
	}
	var fallback func() tea.Cmd
	for _, p := range m.surfacePlugins() {
		for _, cmd := range p.Commands() {
			if cmd.ID != commandID || cmd.Handler == nil {
				continue
			}
			if cmd.Context == context {
				return cmd.Handler
			}
			if fallback == nil {
				fallback = cmd.Handler
			}
		}
	}
	return fallback
}

// activeKeyRouter returns the active plugin's explicit key-routing capability,
// or nil when it has none.
//
// Nil is the answer for six of the seven plugins today, and that is the point:
// levels 2 (overlay) and 3 (contextual binding) of the precedence order are
// opt-in, so adding them changed nothing for git-status, file-browser,
// conversations, workspace, notes, or td-monitor. Their keys still reach the
// global switch first and fall through to the plugin exactly as before.
func (m *Model) activeKeyRouter() plugin.KeyRouter {
	if m.hasModal() {
		return nil
	}
	p := m.focusedSurface()
	if p == nil {
		return nil
	}
	router, ok := p.(plugin.KeyRouter)
	if !ok {
		return nil
	}
	return router
}

// pluginBlocksGlobalKeys reports that the active plugin has an overlay owning
// the keyboard (precedence level 2).
func (m *Model) pluginBlocksGlobalKeys() bool {
	if m.hasModal() {
		return false
	}
	// A plugin mode left open in the primary leaf is visually underneath a
	// focused app-owned content pane. Its modal/input claims stop at that focus
	// boundary; otherwise Files quick-open can steal ctrl+p/f from the document
	// the user is visibly typing into.
	if m.appContentPassiveFocused() {
		return false
	}
	// The global Sessions surface is not a plugin, so it cannot implement
	// plugin.GlobalKeyBlocker — but it hosts the same panes the project
	// workspace does, and an overlay inside one of them owns the keyboard on
	// both. Asking here is what keeps the two projections one answer.
	if m.globalWorkspacesVisible() && m.overview.WorkspacesBlocksGlobalKeys() {
		return true
	}
	p := m.focusedSurface()
	blocker, ok := p.(plugin.GlobalKeyBlocker)
	return ok && blocker.BlocksGlobalKeys()
}

// pluginClaimsKey reports that the active plugin has a live contextual binding
// for a key (precedence level 3).
//
// The host refuses keymap.HostReservedKeys here, whatever the router says. A
// plugin filtering them on its own side is welcome defence in depth, but that
// is the plugin's goodwill; this is the guarantee.
func (m *Model) pluginClaimsKey(key string) bool {
	if keymap.HostReservedKeys[key] {
		return false
	}
	router := m.activeKeyRouter()
	return router != nil && router.ClaimsKey(key)
}

// quitKeyExits reports whether `q` should open sidecar's quit flow. A plugin
// that routes its own keys answers for itself; everything else keeps the
// host's context list.
func (m *Model) quitKeyExits() bool {
	if router := m.activeKeyRouter(); router != nil {
		return router.QuitKeyExits()
	}
	return isRootContext(m.activeContext)
}

// forwardKeyToPlugin hands a key to the focused surface (precedence level 5,
// and the delivery mechanism for levels 2 and 3). That is the hosted global
// plugin while its tab is visible, and the active project plugin otherwise.
func (m *Model) forwardKeyToPlugin(msg tea.Msg) (tea.Model, tea.Cmd) {
	if host := m.focusedGlobalHost(); host != nil {
		cmd := host.update(msg)
		m.updateContext()
		return m, cmd
	}
	p := m.ActivePlugin()
	if p == nil {
		return m, nil
	}
	newPlugin, cmd := p.Update(msg)
	plugins := m.registry.Plugins()
	if m.activePlugin < len(plugins) {
		plugins[m.activePlugin] = newPlugin
	}
	m.updateContext()
	return m, cmd
}

// consumesTextInput returns true when the active context should treat printable
// keys as text input (block app-level navigation shortcuts).
func (m *Model) consumesTextInput() bool {
	return Model(*m).textInputFocused()
}

// textInputFocused is consumesTextInput for the value receivers — the footer,
// above all, which has to know whether the keys it is about to advertise are
// already spoken for.
func (m Model) textInputFocused() bool {
	if m.appContentSearchActive() {
		return true
	}
	// The primary plugin may retain an input mode while focus moves to an
	// app-owned sibling. It is no longer the keyboard owner until Primary is
	// focused again.
	if m.appContentPassiveFocused() {
		return isTextInputContext(m.activeContext)
	}
	// The global Sessions surface answers for itself: its list filter and a
	// focused collection pane's query line take typed text exactly as the
	// project workspace's do, and it is not a plugin, so it cannot say so
	// through plugin.TextInputConsumer.
	if m.globalWorkspacesVisible() && m.overview.WorkspacesConsumesTextInput() {
		return true
	}
	// A global view overlays the plugin pane and takes keyboard focus, so a
	// plugin sitting in a text-input mode underneath it does not consume keys.
	// focusedSurface answers nil for exactly that case.
	if p := m.focusedSurface(); p != nil {
		if c, ok := p.(plugin.TextInputConsumer); ok && c.ConsumesTextInput() {
			return true
		}
	}
	return isTextInputContext(m.activeContext)
}

// isRootContext returns true if the context is a root view where 'q' should quit.
// Root contexts are plugin top-level views (not sub-views like detail/diff/commit).
func isRootContext(ctx string) bool {
	switch ctx {
	case "global", "", "overview", "global-workspaces":
		return true
	// Plugin root contexts where 'q' is not used for navigation
	case "conversations", "conversations-sidebar", "conversations-main":
		return true
	case "git-status", "git-status-commits", "git-status-diff", "git-commit-preview", "git-no-repo":
		return true
	case "file-browser-tree", "file-browser-preview":
		return true
	case "workspace-list", "workspace-preview":
		return true
	case "td-monitor", "td-board", "td-not-installed":
		return true
	case "notes-list":
		return true
	default:
		return false
	}
}

// isTextInputContext returns true if the context is a text input mode
// where alphanumeric keys should be forwarded to the plugin for typing.
func isTextInputContext(ctx string) bool {
	switch ctx {
	case "td-search", "td-form", "td-board-editor", "td-confirm", "td-close-confirm",
		"theme-switcher",
		"config-edit",
		"global-workspaces-filter",
		"global-workspaces-rename",
		"global-workspaces-create",
		// The pane switcher's picker filter is a text input, and the modal has
		// every key while it is up. Without this the footer would go on
		// advertising the tab digits, `?` and `q` at a user who is typing them
		// into the filter — the same reason global-workspaces-create is here.
		"pane-switcher",
		"issue-input":
		return true
	default:
		return false
	}
}

// contextRebindsKey reports that the focused context claimed a key for itself,
// which is what makes a global fallback stand aside for it.
//
// The global context is never such a claim. Sidecar's own globals — `,` for
// Configuration, `i` for the issue input — are registered there so the palette
// and the help modal can name them, while the work itself lives in this key
// handler. Treating that registration as a rebind made the key shadow itself:
// with no plugin context focused, `,` matched its own global binding, stood
// aside for it, and then found no handler to run.
func (m *Model) contextRebindsKey(key string) bool {
	if m.activeContext == "" || m.activeContext == "global" {
		return false
	}
	_, bound := m.keymap.CommandForContextKey(m.activeContext, key)
	return bound
}

// isGlobalRefreshContext returns true if 'r' should trigger a global refresh.
// Returns false for contexts where 'r' should be forwarded to the plugin
// (text input modes or plugin-specific 'r' bindings).
func isGlobalRefreshContext(ctx string) bool {
	switch ctx {
	// Global context - 'r' refreshes
	case "global", "":
		return true

	// Git status contexts - 'r' refreshes (no text input, no 'r' binding)
	case "git-status", "git-history", "git-commit-detail", "git-diff":
		return true

	// Conversations list - 'r' refreshes (no text input, no 'r' binding)
	case "conversations", "conversation-detail", "message-detail":
		return true

	// File browser preview - 'r' refreshes (no text input)
	case "file-browser-preview":
		return true

	// Contexts where 'r' should be forwarded to plugin:
	// - td-monitor: 'r' is mark-review
	// - file-browser-tree: 'r' is rename
	// - file-browser-search: text input mode
	// - file-browser-content-search: text input mode
	// - file-browser-quick-open: text input mode
	// - file-browser-file-op: text input mode
	// - conversations-search: text input mode
	// - conversations-filter: text input mode
	// - git-commit: text input mode (commit message)
	// - td-modal: modal view
	// - palette: command palette
	// - diagnostics: diagnostics view
	default:
		return false
	}
}

// handleProjectSwitcherMouse handles mouse events for the project switcher modal.
func (m *Model) handleProjectSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureProjectSwitcherModal()
	if m.projectSwitcherModal == nil {
		return m, nil
	}
	if m.projectSwitcherMouseHandler == nil {
		m.projectSwitcherMouseHandler = mouse.NewHandler()
	}

	// Handle scroll wheel for project list navigation
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.projectSwitcherCursor--
		if m.projectSwitcherCursor < 0 {
			m.projectSwitcherCursor = 0
		}
		m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(
			m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
		m.clearProjectSwitcherModal()
		return m, m.previewProjectTheme()
	case tea.MouseWheelDown:
		projects := m.projectSwitcherFiltered
		m.projectSwitcherCursor++
		if m.projectSwitcherCursor >= len(projects) {
			m.projectSwitcherCursor = len(projects) - 1
		}
		if m.projectSwitcherCursor < 0 {
			m.projectSwitcherCursor = 0
		}
		m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(
			m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
		m.clearProjectSwitcherModal()
		return m, m.previewProjectTheme()
	}

	if handled, cmd := m.projectSwitcherBarEvent(msg); handled {
		return m, cmd
	}

	action := m.projectSwitcherModal.HandleMouse(msg, m.projectSwitcherMouseHandler)

	// Check if action is a project item click
	if strings.HasPrefix(action, projectSwitcherItemPrefix) {
		// Extract index from item ID
		var idx int
		if _, err := fmt.Sscanf(action, projectSwitcherItemPrefix+"%d", &idx); err == nil {
			projects := m.projectSwitcherFiltered
			if idx >= 0 && idx < len(projects) {
				return m, m.activateProjectSwitcherDestination(projects[idx])
			}
		}
		return m, nil
	}

	switch action {
	case projectSwitcherAddButtonID:
		m.projectSwitcherAddFocused = false
		m.initProjectAdd()
		return m, nil
	case "cancel":
		cmd := m.resetProjectSwitcher()
		m.updateContext()
		return m, cmd
	case "select":
		projects := m.projectSwitcherFiltered
		if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(projects) {
			return m, m.activateProjectSwitcherDestination(projects[m.projectSwitcherCursor])
		}
		return m, nil
	}

	return m, nil
}

// handleThemeSwitcherMouse handles mouse events for the theme switcher modal.
func (m *Model) handleThemeSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureThemeSwitcherModal()
	if m.themeSwitcherModal == nil {
		return m, nil
	}
	if m.themeSwitcherMouseHandler == nil {
		m.themeSwitcherMouseHandler = mouse.NewHandler()
	}

	// Handle scroll wheel for theme list navigation
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		themes := m.themeSwitcherFiltered
		if len(themes) > 0 {
			prevIdx := m.themeSwitcherSelectedIdx
			for i := m.themeSwitcherSelectedIdx - 1; i >= 0; i-- {
				if !themes[i].IsSeparator {
					m.themeSwitcherSelectedIdx = i
					break
				}
			}
			if m.themeSwitcherSelectedIdx != prevIdx {
				m.clearThemeSwitcherModal()
				return m, m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
		}
		return m, nil
	case tea.MouseWheelDown:
		themes := m.themeSwitcherFiltered
		if len(themes) > 0 {
			prevIdx := m.themeSwitcherSelectedIdx
			for i := m.themeSwitcherSelectedIdx + 1; i < len(themes); i++ {
				if !themes[i].IsSeparator {
					m.themeSwitcherSelectedIdx = i
					break
				}
			}
			if m.themeSwitcherSelectedIdx != prevIdx {
				m.clearThemeSwitcherModal()
				return m, m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
		}
		return m, nil
	}

	if handled, cmd := m.themeSwitcherBarEvent(msg); handled {
		return m, cmd
	}

	action := m.themeSwitcherModal.HandleMouse(msg, m.themeSwitcherMouseHandler)

	// Check if action is a theme item click
	if strings.HasPrefix(action, themeSwitcherItemPrefix) {
		var idx int
		if _, err := fmt.Sscanf(action, themeSwitcherItemPrefix+"%d", &idx); err == nil {
			themes := m.themeSwitcherFiltered
			if idx >= 0 && idx < len(themes) && !themes[idx].IsSeparator {
				m.themeSwitcherSelectedIdx = idx
				entry := themes[idx]
				themeCmd := m.previewThemeEntry(entry)
				var tc config.ThemeConfig
				if entry.IsBuiltIn {
					tc = config.ThemeConfig{Name: entry.ThemeKey}
				} else {
					tc = config.ThemeConfig{Name: "default", Community: entry.ThemeKey}
				}
				return m, tea.Batch(themeCmd, m.confirmThemeSelection(tc, entry.Name))
			}
		}
		return m, nil
	}

	switch action {
	case "cancel":
		cmd := m.previewThemeEntry(m.themeSwitcherOriginal)
		m.resetThemeSwitcher()
		m.updateContext()
		return m, cmd
	case "select":
		themes := m.themeSwitcherFiltered
		if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(themes) {
			entry := themes[m.themeSwitcherSelectedIdx]
			if !entry.IsSeparator {
				themeCmd := m.previewThemeEntry(entry)
				var tc config.ThemeConfig
				if entry.IsBuiltIn {
					tc = config.ThemeConfig{Name: entry.ThemeKey}
				} else {
					tc = config.ThemeConfig{Name: "default", Community: entry.ThemeKey}
				}
				return m, tea.Batch(themeCmd, m.confirmThemeSelection(tc, entry.Name))
			}
		}
	}
	return m, nil
}

// handleQuitConfirmMouse handles mouse events for the quit confirmation modal.
func (m *Model) handleHelpModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureHelpModal()
	if m.helpModal == nil {
		return m, nil
	}
	// Info-only modal - no mouse interaction needed beyond ensuring modal exists
	return m, nil
}

// handleUpdateModalKey and handleUpdateModalMouse live in update_modal.go;
// both route through the single modal's own action handling.

func (m *Model) handleQuitConfirmMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	action := m.quitModal.HandleMouse(msg, m.quitMouseHandler)
	switch action {
	case "quit":
		// Save active plugin before quitting
		m.shutdown()
		return m, quitWithInstanceWithdrawal()
	case "cancel":
		m.showQuitConfirm = false
		return m, nil
	}
	return m, nil
}

// handleProjectAddThemePickerKeys handles keys within the theme picker sub-modal.
func (m *Model) handleProjectAddThemePickerKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	maxVisible := 6
	switch msg.String() {
	case "esc":
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		return m, m.applyResolvedTheme(resolved)

	case "up", "k":
		if m.projectAddThemeCursor > 0 {
			m.projectAddThemeCursor--
			if m.projectAddThemeCursor < m.projectAddThemeScroll {
				m.projectAddThemeScroll = m.projectAddThemeCursor
			}
			return m, m.previewProjectAddTheme()
		}
		return m, nil

	case "down", "j":
		if m.projectAddThemeCursor < len(m.projectAddThemeFiltered)-1 {
			m.projectAddThemeCursor++
			if m.projectAddThemeCursor >= m.projectAddThemeScroll+maxVisible {
				m.projectAddThemeScroll = m.projectAddThemeCursor - maxVisible + 1
			}
			return m, m.previewProjectAddTheme()
		}
		return m, nil

	case "enter":
		if m.projectAddThemeCursor >= 0 && m.projectAddThemeCursor < len(m.projectAddThemeFiltered) {
			if m.projectAdd != nil {
				m.projectAdd.themeSelected = m.projectAddThemeFiltered[m.projectAddThemeCursor]
			}
		}
		m.projectAddModalWidth = 0 // Force modal rebuild to show new theme
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		return m, m.applyResolvedTheme(resolved)
	}

	// Filter out unparsed mouse escape sequences
	if isMouseEscapeSequence(msg) {
		return m, nil
	}

	// Forward to filter input
	var cmd tea.Cmd
	m.projectAddThemeInput, cmd = m.projectAddThemeInput.Update(msg)
	// Re-filter
	query := m.projectAddThemeInput.Value()
	all := append([]string{"(use global)"}, styles.ListThemes()...)
	if query == "" {
		m.projectAddThemeFiltered = all
	} else {
		var filtered []string
		q := strings.ToLower(query)
		for _, name := range all {
			if strings.Contains(strings.ToLower(name), q) {
				filtered = append(filtered, name)
			}
		}
		m.projectAddThemeFiltered = filtered
	}
	m.projectAddThemeCursor = 0
	m.projectAddThemeScroll = 0
	return m, cmd
}

// resolveIssueOpenID picks which issue ID to open from the issue input modal.
// Priority: selected result (cursor ≥ 0) → sole visible search result when
// cursor is unset → typed value (direct ID open / multi-result fallback).
func resolveIssueOpenID(cursor int, results []IssueSearchResult, typed string) string {
	if cursor >= 0 && cursor < len(results) {
		return results[cursor].ID
	}
	if cursor < 0 && len(results) == 1 {
		return results[0].ID
	}
	return strings.TrimSpace(typed)
}

// issueInputSubmit resolves the current issue input (selected result or typed ID)
// and either opens the full issue in TD monitor or shows a lightweight preview.
func (m *Model) issueInputSubmit() (tea.Model, tea.Cmd) {
	issueID := resolveIssueOpenID(m.issueSearchCursor, m.issueSearchResults, m.issueInputInput.Value())
	if issueID == "" {
		return m, nil
	}
	// Check if active plugin is TD monitor — go directly to rich modal
	if p := m.ActivePlugin(); p != nil && p.ID() == "td-monitor" {
		m.resetIssueInput()
		m.updateContext()
		return m, tea.Batch(
			func() tea.Msg { return OpenFullIssueMsg{IssueID: issueID} },
		)
	}
	// Hide input modal but preserve search state so "back" can restore it.
	m.showIssueInput = false
	// Show lightweight preview
	m.showIssuePreview = true
	m.activeContext = "issue-preview"
	m.issuePreviewLoading = true
	m.issuePreviewData = nil
	m.issuePreviewError = nil
	m.issuePreviewModal = nil
	m.issuePreviewModalWidth = 0
	m.issuePreviewModalHeight = 0
	m.issuePreviewMouseHandler = mouse.NewHandler()
	workDir := ""
	if m.ui != nil {
		workDir = m.ui.WorkDir
	}
	m.issuePreviewView = m.newIssuePreviewView()
	return m, tea.Batch(
		m.issuePreviewView.Load(issuePreviewModelID, workDir, issueID, 0),
		m.startIssuePreviewWatch(workDir, issueID),
	)
}

// handleIssueInputMouse handles mouse events for the issue input modal.
func (m *Model) handleIssueInputMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureIssueInputModal()
	if m.issueInputModal == nil {
		return m, nil
	}
	if m.issueInputMouseHandler == nil {
		m.issueInputMouseHandler = mouse.NewHandler()
	}
	// Pre-render to sync hit regions and focusIDs on the (potentially rebuilt) modal.
	// The issue input modal is nilled on every keystroke to fix a stale text-input
	// pointer, so the modal object seen here may lack focusIDs from a prior Render.
	m.issueInputModal.Render(m.width, m.height, m.issueInputMouseHandler)
	action := m.issueInputModal.HandleMouse(msg, m.issueInputMouseHandler)
	switch {
	case action == "cancel":
		m.resetIssueInput()
		m.updateContext()
	case action == "open":
		return m.issueInputSubmit()
	case strings.HasPrefix(action, issueSearchResultPrefix):
		// Click on a search result — select it and submit
		idxStr := strings.TrimPrefix(action, issueSearchResultPrefix)
		if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 && idx < len(m.issueSearchResults) {
			m.issueSearchCursor = idx
			return m.issueInputSubmit()
		}
	}
	return m, nil
}

// handleIssuePreviewMouse handles mouse events for the issue preview modal.
func (m *Model) handleIssuePreviewMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureIssuePreviewModal()
	if m.issuePreviewModal == nil {
		return m, nil
	}
	if m.issuePreviewMouseHandler == nil {
		m.issuePreviewMouseHandler = mouse.NewHandler()
	}
	// Pre-render to sync hit regions and focusIDs on the modal, which may have
	// been rebuilt (e.g. after data/error arrival cleared the cache).
	m.issuePreviewModal.Render(m.width, m.height, m.issuePreviewMouseHandler)

	view := m.ensureIssuePreviewView()
	if view != nil {
		switch ev := msg.(type) {
		case tea.MouseWheelMsg:
			// The card is the scroll owner (see issuePreviewWheelAtBoundary),
			// and it earns its repaints through the shared burst guard like
			// every other surface that hosts this viewer.
			if m.issuePreviewWheel == nil {
				m.issuePreviewWheel = &tty.WheelBurst{}
			}
			delta := -modalWheelLines
			if ev.Button == tea.MouseWheelDown {
				delta = modalWheelLines
			}
			if flushed, ok := m.issuePreviewWheel.Add(delta, m.issuePreviewClock()); ok {
				view.Scroll(flushed)
			}
			return m, nil
		}
	}

	action := m.issuePreviewModal.HandleMouse(msg, m.issuePreviewMouseHandler)
	if action == issueViewFocusID && view != nil {
		view.SetActive(true)
		view.SetFocused(true)
		if r := findMouseRegion(m.issuePreviewMouseHandler, issueViewFocusID); r != nil {
			mi := msg.Mouse()
			_, cmd := view.HandleClick(mi.X-r.Rect.X, mi.Y-r.Rect.Y)
			return m, cmd
		}
		return m, nil
	}
	switch action {
	case "cancel":
		m.resetIssuePreview()
		m.resetIssueInput()
		m.updateContext()
	case "back":
		m.backToIssueInput()
		return m, nil
	case "open-in-td":
		issueID := ""
		if d := m.previewIssueData(); d != nil {
			issueID = d.ID
		}
		m.resetIssuePreview()
		m.resetIssueInput()
		m.updateContext()
		return m, OpenIssueInTD(issueID)
	}
	return m, nil
}

func findMouseRegion(h *mouse.Handler, id string) *mouse.Region {
	if h == nil || h.HitMap == nil {
		return nil
	}
	for _, r := range h.HitMap.Regions() {
		if r.ID == id {
			reg := r
			return &reg
		}
	}
	return nil
}

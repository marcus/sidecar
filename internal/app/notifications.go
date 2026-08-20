package app

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/reveal"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/uirequest"
)

// The app shell owns the notification store. It is the single writer inside
// this process, and the only thing that answers a `notify` UI request, so
// every surface — toasts, the header indicator, the centre panel — reads one
// snapshot rather than each opening the log for itself.

// PostNotification returns a command that files a notification.
func PostNotification(n notify.Notification) tea.Cmd {
	return func() tea.Msg { return notify.PostMsg{Notification: n} }
}

// DismissNotification returns a command that dismisses one notification.
func DismissNotification(id string) tea.Cmd {
	return func() tea.Msg { return notify.DismissMsg{ID: id} }
}

// ReadNotification returns a command that marks one notification read.
func ReadNotification(id string) tea.Cmd {
	return func() tea.Msg { return notify.ReadMsg{ID: id} }
}

// openNotificationStore opens the JSONL store, falling back to an in-memory
// store when the state tree cannot be written. A state dir that refuses to
// open costs the user persistence, never the alert itself.
func openNotificationStore() notify.Store {
	store, err := notify.Open(config.StateDir())
	if err != nil {
		slog.Debug("notify: falling back to an in-memory store", "err", err)
		return notify.NewMemStore()
	}
	return store
}

// contentWidth is the width of the content region: the terminal minus any
// column the app shell has reserved on the right. It is the single place that
// answers "how wide is the content", so the toast host and the plugin host
// cannot disagree about where the content's right edge is. The notification
// centre panel (steel-thread step 6) reserves its width here; until it does,
// the content region is the whole terminal.
func (m Model) contentWidth() int {
	return max(0, m.width-m.reservedRightWidth())
}

// contentHeight is the height of the content region — the same arithmetic
// viewContent does, kept here so the toast column can ask how much room it has
// without waiting to be handed the geometry at render time. A block that will
// not fit must not be counted as painted, and the read gate runs on the
// heartbeat, not in View.
func (m Model) contentHeight() int {
	return max(0, m.height-headerHeight-footerHeight)
}

// reservedRightWidth is the width of the right-hand column reserved by the
// notification centre: the panel plus the one column its resize handle owns.
// It is 0 whenever the panel is closed, and 0 for a terminal with no room to
// give — a panel that would leave the content unusable yields rather than
// shrinking it below notificationCentreMinContent.
func (m Model) reservedRightWidth() int {
	panel := m.notificationCentrePanelWidth()
	if panel <= 0 {
		return 0
	}
	return panel + notificationCentreHandleWidth
}

// contentSize is the box every plugin lays out against: the content region's
// width (the terminal minus any reserved right column) and the height left
// between the header and the footer. Both the resize path and the render path
// read it, so a plugin can never be sized against a box it is not drawn in.
func (m Model) contentSize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{
		Width:  m.contentWidth(),
		Height: max(0, m.height-headerHeight-footerHeight),
	}
}

// emitContentSize re-announces the content box to every surface that lays out
// against it. Opening, closing, or dragging the notification centre changes
// that box exactly as a terminal resize does, so it is delivered exactly as a
// terminal resize is — there is no second, panel-specific notification and no
// plugin that has to know the panel exists.
//
// Every path that rebuilds or re-sizes the surfaces goes through here: the
// WindowSizeMsg handler, the project/worktree switch that calls Reinit, and
// the panel's own open/close/resize.
func (m *Model) emitContentSize() []tea.Cmd {
	size := m.contentSize()
	var cmds []tea.Cmd
	if m.registry == nil {
		return nil
	}
	plugins := m.registry.Plugins()
	for i, p := range plugins {
		newPlugin, cmd := p.Update(size)
		plugins[i] = newPlugin
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// The global Tasks host is not in the registry, and the Workspaces browser
	// sizes a live pane; both lay out against the same box.
	if cmd := m.globalTasks.update(size); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.overview != nil {
		if cmd := m.overview.WorkspacesResize(size.Width, size.Height); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// toggleNotificationCentre is the single app-level entry point for opening and
// closing the centre. The header indicator's click and its shortcut both come
// through here, so the panel has one way to be opened however it was asked for.
// Opening reserves the right-hand column and gives the panel the keyboard;
// closing hands the column and the keyboard back. Either way the content is
// re-sized before the next frame.
func (m *Model) toggleNotificationCentre() tea.Cmd {
	m.notificationCentreOpen = !m.notificationCentreOpen
	m.notificationCentreFocused = m.notificationCentreOpen
	if m.notificationCentreOpen {
		m.notificationCentreCursor = 0
		m.notificationCentreScroll = 0
		// Resolve the persisted preference once, on the way in, rather than
		// reading the state file on every frame. It is stored unclamped: a
		// terminal too narrow to honour it must not quietly rewrite it.
		if m.notificationCentreWidth <= 0 {
			if saved := state.GetNotificationCentreWidth(); saved > 0 {
				m.notificationCentreWidth = saved
			} else {
				m.notificationCentreWidth = notificationCentreDefaultWidth
			}
		}
		// Opening the centre on an item is seeing it.
		m.readSelectedNotification()
	}
	m.updateContext()
	return tea.Batch(m.emitContentSize()...)
}

// closeNotificationCentre is the explicit close — esc with the panel focused,
// the close affordance, or the indicator toggle. Nothing else may call it: the
// panel survives every navigation until the user asks for it to go.
func (m *Model) closeNotificationCentre() tea.Cmd {
	if !m.notificationCentreOpen {
		return nil
	}
	m.notificationCentreOpen = false
	m.notificationCentreFocused = false
	m.updateContext()
	return tea.Batch(m.emitContentSize()...)
}

// Notifications returns the current snapshot, newest first.
func (m *Model) Notifications() []notify.Notification {
	if m.notifications == nil {
		return nil
	}
	return m.notificationCache
}

// UnreadNotifications is what the header indicator counts.
func (m *Model) UnreadNotifications() int {
	return notify.UnreadCount(m.notificationCache)
}

// refreshNotifications re-reads the store into the render-side cache. Views
// never touch the store directly: they read a slice that only changes on an
// update, so a frame cannot be built mid-write.
func (m *Model) refreshNotifications() {
	if m.notifications == nil {
		m.notificationCache = nil
		m.pruneNotificationCTAs()
		return
	}
	all, err := m.notifications.List()
	if err != nil {
		slog.Debug("notify: list failed", "err", err)
		return
	}
	m.notificationCache = all
	m.pruneNotificationCTAs()
}

// postNotification stores a notification and returns the broadcast announcing
// it, so a toast host can start its countdown from the stored record.
func (m *Model) postNotification(n notify.Notification) tea.Cmd {
	if m.notifications == nil {
		return nil
	}
	stored, err := m.notifications.Post(n)
	if err != nil {
		slog.Debug("notify: post failed", "err", err)
		return nil
	}
	m.refreshNotifications()
	return func() tea.Msg { return notify.PostedMsg{Notification: stored} }
}

// dismissNotification dismisses by id, ignoring an id the store never saw.
func (m *Model) dismissNotification(id string) {
	if m.notifications == nil || id == "" {
		return
	}
	if err := m.notifications.Dismiss(id); err != nil {
		slog.Debug("notify: dismiss failed", "id", id, "err", err)
		return
	}
	m.refreshNotifications()
}

// readNotification marks one notification read.
func (m *Model) readNotification(id string) {
	if m.notifications == nil || id == "" {
		return
	}
	if err := m.notifications.MarkRead(id); err != nil {
		slog.Debug("notify: mark read failed", "id", id, "err", err)
		return
	}
	m.refreshNotifications()
}

// reconcileNotifications is the whole notification side of the 1s heartbeat,
// in the order the three steps have to run in:
//
//  1. Sync the column first, because the sweep's read gate asks the reveal
//     states what is on screen — a block that just took a slot freed by an
//     expiry is painted this frame, not next second.
//  2. Sweep: retire expired toasts, mark what was painted read, compact the
//     24h window — and re-read the log, which is where a record another
//     process appended becomes visible.
//  3. Sync again, because of that last clause. The CLI's fallback path (no
//     instance took the request) appends straight to the log, so a swept-in
//     record has no reveal state after step 1 and, without this, waited for
//     the *next* heartbeat to get one — a fallback post reached the screen up
//     to a second later than the same post delivered through the request bus.
//     Both arrival paths now reach the column on the same frame.
//
// The reveal ticks are sequence-tagged, so the second sync's tick supersedes
// the first's and two loops can never advance the same states. When the second
// sync has nothing to animate it returns nil without bumping the sequence, and
// the first tick — which is still current — is the one to keep.
func (m *Model) reconcileNotifications(now time.Time) tea.Cmd {
	revealCmd := m.syncToastReveal(now)
	m.sweepNotifications(now)
	if cmd := m.syncToastReveal(now); cmd != nil {
		revealCmd = cmd
	}
	return revealCmd
}

// sweepNotifications runs on the 1s heartbeat. It retires toasts whose
// countdown has run out (they stay in the centre — suppressed is not dropped)
// and compacts records past the 24h retention window.
func (m *Model) sweepNotifications(now time.Time) {
	if m.notifications == nil {
		return
	}
	// Record what is on screen right now. A toast whose countdown then runs out
	// has had its moment and is read — without this nothing ever marks anything
	// read, so the header would climb to `●40` in an ordinary session and every
	// unexpired notification would toast again at the next start.
	//
	// The gate on "was actually painted" is the whole point: expiry alone would
	// silently read things the user never saw — a notification an agent posted
	// while sidecar was closed arrives already past its countdown, and with one
	// toast slot a burst reads everything queued behind the newest. Sticky
	// notifications have no countdown and stay unread until the user answers
	// them, which is what sticky is for.
	if !m.hasModal() && !m.overlaysSuppressed() {
		if m.toastPainted == nil {
			m.toastPainted = make(map[string]bool)
		}
		for _, r := range m.toastColumnBlocks() {
			s := r.stack
			// Only what is actually legible counts as painted. A collapsed
			// stack shows its lead and a `×N`; the members hiding behind it
			// were never read, so they keep their unread state and their place
			// in the centre until the user expands the block or opens the
			// centre. Expanding the block is seeing them, and marks them.
			// No reveal state means the block is not on screen at all: the
			// column is too narrow for a bordered block, or the block did not
			// fit the content region's remaining height. syncToastReveal is
			// the single answer to "what is painted", and absence from it is
			// as much of an answer as a retracted state — defaulting to
			// "painted" here would read notifications nothing ever drew.
			if !r.state.Visible() {
				continue
			}
			m.toastPainted[s.Lead().ID] = true
			// Members are only legible once the block has finished arriving:
			// mid-reveal the lower rows are not on screen yet.
			if !m.toastExpanded || r.state.Phase() != reveal.Shown {
				continue
			}
			for i, member := range s.Members {
				if i > toastExpandedMembers {
					break
				}
				m.toastPainted[member.ID] = true
			}
		}
	}
	for _, n := range m.notificationCache {
		if n.Read() || n.Dismissed() || !m.toastPainted[n.ID] {
			continue
		}
		if notify.ToastExpired(n, now) {
			if err := m.notifications.MarkRead(n.ID); err != nil {
				slog.Debug("notify: mark read on toast expiry failed", "id", n.ID, "err", err)
			}
			delete(m.toastPainted, n.ID)
		}
	}

	if _, err := m.notifications.Sweep(now); err != nil {
		slog.Debug("notify: sweep failed", "err", err)
		return
	}
	// Always refresh, not only when something was pruned: Sweep is also where
	// the store re-reads the log, so this is where records another process
	// appended become visible.
	m.refreshNotifications()
}

// handleNotifyRequest answers a `notify` request from the file-RPC bus: the
// out-of-process posting API, landing in the same store as an in-process post.
//
// An instance only answers for its own project. Otherwise a second Sidecar on
// another repo would file another copy, and the CLI's no-ack fallback (which
// writes the log directly) would never run for the project that is not open.
func (m *Model) handleNotifyRequest(req uirequest.Request) tea.Cmd {
	if req.Action != uirequest.ActionNotify {
		return nil
	}
	if !m.ownsNotifyRequest(req) {
		m.ackNotify(req, uirequest.StatusDeclined, "this instance is not showing that project")
		return nil
	}

	if req.Target.Kind == uirequest.TargetKindNotification && req.Target.Value != "" {
		// Dismissal. The CLI has already applied the origin check against the
		// log; it is re-applied here because this process must never dismiss
		// something on a caller's word alone.
		id := req.Target.Value
		found, ok := m.findNotification(id)
		if !ok {
			m.ackNotify(req, uirequest.StatusDeclined, "no such notification")
			return nil
		}
		if !notify.MayDismiss(found, notifyOriginFrom(req.Origin)) {
			m.ackNotify(req, uirequest.StatusDeclined, "a caller may only dismiss notifications it posted")
			return nil
		}
		m.dismissNotification(id)
		m.ackNotify(req, uirequest.StatusOpened, "")
		return nil
	}

	var n notify.Notification
	if len(req.Payload) == 0 {
		m.ackNotify(req, uirequest.StatusError, "notify request carried no notification")
		return nil
	}
	if err := json.Unmarshal(req.Payload, &n); err != nil {
		m.ackNotify(req, uirequest.StatusError, "notify payload could not be read")
		return nil
	}
	if n.Origin.Zero() {
		n.Origin = notifyOriginFrom(req.Origin)
	}
	cmd := m.postNotification(n)
	m.ackNotify(req, uirequest.StatusOpened, "")
	return cmd
}

// ownsNotifyRequest reports whether this instance is the one the request is
// addressed to. A request with no project or working directory is unaddressed
// and every instance takes it.
//
// req.Origin is always the *caller's* origin — for a post it is also the
// poster's, for a dismiss it is deliberately not the target's — so routing and
// the dismissal check read the same field and mean the same thing by it.
func (m *Model) ownsNotifyRequest(req uirequest.Request) bool {
	origin := req.Origin
	if origin.WorkDir == "" && origin.ProjectKey == "" {
		return true
	}
	for _, mine := range []string{m.ui.WorkDir, m.ui.ProjectRoot} {
		if mine == "" {
			continue
		}
		// Containment, not equality. A caller's working directory is wherever
		// its shell happens to be, and an agent shell is nearly always in a
		// subdirectory of the project — `internal/app`, not the root. Requiring
		// the exact root disowned every post from inside the tree, so the CLI
		// fell back to writing the log and told the user no instance was
		// running while one was showing that very project.
		if origin.WorkDir != "" && pathWithin(origin.WorkDir, mine) {
			return true
		}
		if origin.ProjectKey != "" && filepath.Base(mine) == origin.ProjectKey {
			return true
		}
	}
	return false
}

func pathsEqual(a, b string) bool {
	an, _ := normalizePath(a)
	bn, _ := normalizePath(b)
	return an == bn
}

// pathWithin reports whether path is root itself or lives beneath it. The
// separator on the prefix keeps a sibling named sidecar-notification-center
// from matching sidecar.
func pathWithin(path, root string) bool {
	pn, _ := normalizePath(path)
	rn, _ := normalizePath(root)
	if pn == "" || rn == "" {
		return false
	}
	if pn == rn {
		return true
	}
	return strings.HasPrefix(pn, strings.TrimSuffix(rn, string(filepath.Separator))+string(filepath.Separator))
}

func (m *Model) findNotification(id string) (notify.Notification, bool) {
	for _, n := range m.notificationCache {
		if n.ID == id {
			return n, true
		}
	}
	return notify.Notification{}, false
}

func notifyOriginFrom(o uirequest.Origin) notify.Origin {
	return notify.Origin{
		TmuxSession: o.TmuxSession,
		ProjectKey:  o.ProjectKey,
		WorkDir:     o.WorkDir,
		PID:         o.PID,
	}
}

func (m *Model) ackNotify(req uirequest.Request, status uirequest.Status, reason string) {
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: uirequest.InstanceID("app"),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   status,
		Reason:   reason,
		Surface:  "notifications",
	})
}

// ToastableNotifications is the set a toast host should currently be drawing.
func (m *Model) ToastableNotifications(now time.Time) []notify.Notification {
	return notify.Toastable(m.notificationCache, now)
}

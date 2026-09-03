package issueview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/markdown"
	sharedscroll "github.com/marcus/sidecar/internal/scroll"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	tabStopWidth             = 8
	horizontalContentPadding = 1
)

// LoadedMsg is the result of a Model load. Its identity fields ensure a result
// can only be applied to the model, request, and plugin epoch that issued it.
type LoadedMsg struct {
	ModelID           int
	RequestGeneration uint64
	Epoch             uint64
	IssueID           string
	Data              *Data
	Error             error
	// FoundIn is nil for a local issue. Non-nil, the issue was resolved in
	// another project's store; hosts use it to retarget live watchers.
	FoundIn *Owner

	// Refresh marks a result produced by Model.Refresh rather than Model.Load:
	// an in-place re-read that must not disturb scroll, cursor or hover, and
	// that is discarded entirely when it found nothing new. See live.go.
	Refresh bool
	// NotModified completes an in-flight refresh without replacing content.
	NotModified bool
	// Revision is the last adopted source revision, for a later conditional read.
	Revision string
}

// NotModified is returned by an injected loader when a refresh found no change.
type NotModified struct {
	IssueID  string
	Epoch    uint64
	Revision string
}

// IssueLoader produces an issue load command. IfRevision is the last adopted
// source revision and may be empty. The command should return FetchedMsg,
// NotModified, or LoadedMsg.
type IssueLoader func(workDir, issueID string, epoch uint64, ifRevision string) tea.Cmd

// GetEpoch allows callers to apply the normal plugin epoch checks if desired.
func (m LoadedMsg) GetEpoch() uint64 { return m.Epoch }

type navKind int

const (
	navParent navKind = iota
	navChild
)

type navItem struct {
	kind     navKind
	id       string
	childIdx int
}

// HitKind names what a click or hover landed on.
type HitKind int

const (
	HitNone HitKind = iota
	HitParent
	HitChild
	HitBody
	// HitScrollbar reports a press that began a scrollbar gesture instead of
	// landing on the card's content. Hosts see it from HandleClick while
	// ScrollbarDragging is true, which is their cue to start the shared
	// handler's drag so motions come back to ScrollbarDrag.
	HitScrollbar
)

// Hit is one interactive rectangle in the last View, in view-local cells.
type Hit struct {
	Kind   HitKind
	Index  int
	ID     string
	X, Y   int
	W, H   int
	Cursor int
}

// Model is one td issue in one content box.
type Model struct {
	renderer *markdown.Renderer

	modelID           int
	requestGeneration uint64
	epoch             uint64
	issueID           string
	workDir           string
	// foundIn is the owning project's display name when the current issue was
	// resolved in another configured project's store; empty for local issues.
	foundIn string

	width  int
	height int
	scroll int

	pendingScroll    int
	hasPendingScroll bool

	loading bool
	data    *Data
	err     error

	// live sequences in-place re-reads driven by a change to the td store, and
	// holds the fingerprint that keeps an unchanged re-read off the screen. See
	// live.go.
	live livewatch.Refresher

	// Interaction. Active is the modal's "tab here and press enter, or click"
	// state: only then do arrows walk the epic. A workspace pane that already
	// owns the keyboard sets this when the pane is focused.
	active   bool
	focused  bool
	cursor   int // index into navItems(); -1 means the issue itself
	hover    int
	hints    []ActionHint
	hits     []Hit
	rows     []row
	buildFor int
	// buildStyle is the markdown style key the current rows were built under,
	// so a live theme change rebuilds them without a resize.
	buildStyle string

	// Text selection. originX/originY are where the host last drew the body;
	// renderGeneration counts the times the rows were invalidated, which is
	// what tells a selection its text has been replaced. See select.go.
	selection        textselect.Surface
	selectionKey     string
	originX, originY int
	renderGeneration uint64

	// Scrollbar pointer state. See scrollbar.go.
	scrollbarHover      bool
	scrollbarDragging   bool
	scrollbarGrabDelta  int
	scrollbarDragParams ui.ScrollbarParams

	// OpenHandler, when set, receives parent/subtask/sibling activations
	// instead of Load retargeting this model. Hosts that tab issues use this
	// so navigation cannot destroy the issue the user is reading.
	OpenHandler func(issueID string) tea.Cmd

	// OpenInTDHandler, when set, is what O does: it hands the selected issue
	// to the host so the host can put it in front of the user in td. The
	// capability lives here rather than in each host's key switch so every
	// surface that shows an issue card answers O the same way and advertises
	// it in the same ACTIONS row.
	OpenInTDHandler func(issueID string) tea.Cmd

	// FallbackRefs, when set, supplies this host's configured projects at
	// click time. On a local miss the card searches them (see crossproject.go)
	// instead of declaring the issue missing. It is called inside the fetch
	// command — resolving candidates stats files and may shell out to git —
	// never on the update goroutine. Hosts without cross-project search leave
	// it nil and behave exactly as before.
	FallbackRefs func() []ProjectRef

	loader   IssueLoader
	revision string
}

// SetLoader replaces the default td fetch. Nil restores it.
func (m *Model) SetLoader(loader IssueLoader) {
	if m == nil {
		return
	}
	m.loader = loader
}

// ActionHint is one key/label pair drawn in the card's ACTIONS row.
type ActionHint struct {
	Key, Label string
}

// New creates an empty issue viewer.
//
// The card owns its wrap contract: it insets each side by
// horizontalContentPadding itself, so its markdown must render without
// Glamour's document margin — the same pairing Notes uses. A nil renderer, or
// one built without markdown.CompactDocument, is replaced by the card's own
// compact renderer rather than mutated: an injected instance may be shared
// with viewers (docview) whose inset is Glamour's margin. This is what keeps
// every surface that shows a card — workspace leaf, overview preview, app
// content deck, preview modal — wrapping identically no matter what it
// injects; without it, body text wraps around width−7 while width−3 columns
// are free.
func New(renderer *markdown.Renderer) *Model {
	if renderer == nil || !renderer.CompactsDocument() {
		renderer, _ = markdown.NewRenderer(markdown.CompactDocument)
	}
	return &Model{renderer: renderer, cursor: -1, hover: -1, buildFor: -1}
}

// Load retargets the model at issueID and returns a command that fetches it.
// Only the issueview-owned LoadedMsg is broadcast. A pending restore scroll
// survives so SetResult can apply it to this generation.
func (m *Model) Load(modelID int, workDir, issueID string, epoch uint64) tea.Cmd {
	m.modelID = modelID
	m.requestGeneration++
	m.epoch = epoch
	m.issueID = issueID
	// Reloading the store an adopted card already owns (a restored tab's first
	// fetch, or in-card navigation) keeps the adoption; anything else is a
	// genuine retarget and starts clean.
	keepAdoption := m.foundIn != "" && workDir == m.workDir
	m.workDir = workDir
	if !keepAdoption {
		m.foundIn = ""
	}
	m.scroll = 0
	m.cursor = -1
	m.hover = -1
	m.loading = true
	m.data = nil
	m.err = nil
	m.revision = ""
	// A retarget invalidates the refresh gate: "unchanged since last time" is
	// meaningless once the subject changed, and a re-read owed for the previous
	// issue must not fire against this one.
	m.live.Reset()
	m.invalidateRender()

	generation := m.requestGeneration
	base := LoadedMsg{
		ModelID: modelID, RequestGeneration: generation, Epoch: epoch, IssueID: issueID,
	}
	if m.loader != nil {
		load := m.loader(workDir, issueID, epoch, "")
		return func() tea.Msg { return adoptIssueMsg(load, base) }
	}
	fallbacks := m.fallbackRefs()
	fetch := FetchWithFallbacks(workDir, issueID, fallbacks)
	return func() tea.Msg {
		msg, _ := fetch().(FetchedMsg)
		base.Data, base.Error, base.FoundIn = msg.Data, msg.Error, msg.FoundIn
		return base
	}
}

func adoptIssueMsg(load tea.Cmd, base LoadedMsg) LoadedMsg {
	if load == nil {
		base.Error = fmt.Errorf("unexpected issue load result")
		return base
	}
	switch msg := load().(type) {
	case FetchedMsg:
		base.Data = msg.Data
		base.Error = msg.Error
		base.FoundIn = msg.FoundIn
		return base
	case NotModified:
		base.Refresh = true
		base.NotModified = true
		base.Revision = msg.Revision
		if msg.IssueID != "" {
			base.IssueID = msg.IssueID
		}
		if msg.Epoch != 0 {
			base.Epoch = msg.Epoch
		}
		return base
	case LoadedMsg:
		msg.ModelID = base.ModelID
		msg.RequestGeneration = base.RequestGeneration
		if msg.Epoch == 0 {
			msg.Epoch = base.Epoch
		}
		if msg.IssueID == "" {
			msg.IssueID = base.IssueID
		}
		if base.Refresh {
			msg.Refresh = true
		}
		return msg
	default:
		base.Error = fmt.Errorf("unexpected issue load result")
		return base
	}
}

// fallbackRefs snapshots the host's project list once per load. A nil handler
// means no cross-project search; the closure keeps the update goroutine out of
// filesystem work either way.
func (m *Model) fallbackRefs() []ProjectRef {
	if m.FallbackRefs == nil {
		return nil
	}
	return m.FallbackRefs()
}

// SetResult applies msg if it belongs to the current load. It returns false
// without changing the model for stale model, request, epoch, or issue results.
//
// A result produced by [Model.Refresh] is applied in place instead: see
// applyRefresh. It also returns false when the refresh found nothing new, so a
// host can treat "false" uniformly as "nothing to repaint".
// ResultMatches reports whether msg belongs to this model's current load.
func (m *Model) ResultMatches(msg LoadedMsg) bool {
	if m == nil {
		return false
	}
	return msg.ModelID == m.modelID &&
		msg.RequestGeneration == m.requestGeneration &&
		msg.Epoch == m.epoch &&
		msg.IssueID == m.issueID
}

func (m *Model) SetResult(msg LoadedMsg) bool {
	if !m.ResultMatches(msg) {
		return false
	}
	if msg.Revision != "" {
		m.revision = msg.Revision
	}
	if msg.NotModified {
		if !msg.Refresh {
			return false
		}
		return m.applyRefresh(msg)
	}
	if msg.Refresh {
		return m.applyRefresh(msg)
	}
	// A fresh load defines what is on screen, so the refresh gate measures from
	// here rather than reporting the first watcher signal as a change.
	m.live.Reset()
	m.live.Adopt(fingerprintData(msg.Data))
	m.loading = false
	m.data = msg.Data
	m.err = msg.Error
	// A cross-project hit adopts its owning store: Refresh and any in-card
	// navigation address it directly from here on, and the badge names where
	// the card came from. A local result clears both.
	if msg.FoundIn != nil && msg.FoundIn.Root != "" {
		m.workDir = msg.FoundIn.Root
		m.foundIn = msg.FoundIn.Name
	} else {
		m.foundIn = ""
	}
	m.cursor = -1
	m.hover = -1
	m.invalidateRender()
	if m.hasPendingScroll {
		m.scroll = m.pendingScroll
		m.hasPendingScroll = false
	}
	m.clampScroll()
	return true
}

// Arm shows the loading placeholder for a restored tab without issuing a load.
func (m *Model) Arm(modelID int, issueID string, epoch uint64) {
	m.modelID = modelID
	m.epoch = epoch
	m.issueID = issueID
	m.loading = true
}

// NeedsLoad reports whether this model has never been asked to Load.
func (m *Model) NeedsLoad() bool { return m.requestGeneration == 0 }

// SetData installs already-fetched data. Tests and hosts that fetched
// themselves use this instead of going through Load.
func (m *Model) SetData(d *Data) {
	m.loading = false
	m.data = d
	m.err = nil
	if d != nil {
		m.issueID = d.ID
	}
	m.invalidateRender()
	m.clampScroll()
}

// Data returns the current issue, or nil before a successful fetch.
func (m *Model) Data() *Data { return m.data }

// Err returns the last fetch error, if any.
func (m *Model) Err() error { return m.err }

// SetSize sets the content box dimensions. A width change invalidates rendered
// markdown because wrapping depends on it.
func (m *Model) SetSize(width, height int) {
	width = max(width, 0)
	height = max(height, 0)
	if width != m.width {
		m.invalidateRender()
	}
	m.width = width
	m.height = height
	m.clampScroll()
}

// SetActive enables epic/subtask arrow navigation. Hosts that share those
// keys with something else (the preview modal) only set this after the user
// tabs to the card and presses enter, or clicks it.
func (m *Model) SetActive(active bool) {
	if m.active == active {
		if !active {
			m.hover = -1
		}
		return
	}
	m.active = active
	if !active {
		m.hover = -1
	}
	m.invalidateRender()
}

// Active reports whether arrow keys should navigate the epic.
func (m *Model) Active() bool { return m.active }

// SetFocused marks the card as the current tab stop. It is a visual cue
// distinct from Active: focused-but-inactive still scrolls on arrows.
func (m *Model) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.invalidateRender()
}

// Focused reports whether the card is the current tab stop.
func (m *Model) Focused() bool { return m.focused }

// SetActionHints replaces the ACTIONS row. An empty list hides the section
// unless the card can still show its own navigation hints.
// ActionHints returns the host-supplied ACTIONS row.
func (m *Model) ActionHints() []ActionHint { return append([]ActionHint(nil), m.hints...) }

func (m *Model) SetActionHints(hints []ActionHint) {
	if actionHintsEqual(m.hints, hints) {
		return
	}
	m.hints = append([]ActionHint(nil), hints...)
	m.invalidateRender()
}

func actionHintsEqual(a, b []ActionHint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// View returns exactly the configured number of rows, each no wider than the
// configured width in terminal cells.
func (m *Model) View() string {
	if m.height <= 0 {
		return ""
	}
	// Settle the selection before drawing it: a card that has never held one
	// still has to say so, because an untouched selection state reads as a
	// one-cell selection at the top-left corner.
	m.expireSelection()
	rows := m.visibleRows()
	bodyWidth := m.contentWidth()
	useBar := m.needsScrollbar()
	m.hits = m.hits[:0]

	out := make([]string, m.height)
	for i := range out {
		line := ""
		selected, hovered := false, false
		if i < len(rows) {
			r := rows[i]
			line = r.text
			if r.cursor >= 0 && m.cursor == r.cursor {
				selected = true
			}
			if r.cursor >= 0 && m.hover == r.cursor {
				hovered = true
			}
			if r.kind == rowParent || r.kind == rowChild {
				kind := HitParent
				if r.kind == rowChild {
					kind = HitChild
				}
				m.hits = append(m.hits, Hit{
					Kind:   kind,
					Index:  r.childIdx,
					ID:     m.hitID(r),
					X:      m.leftPadding(),
					Y:      i,
					W:      bodyWidth,
					H:      1,
					Cursor: r.cursor,
				})
			}
		}
		// The highlight is painted at slice time, onto the row about to be
		// drawn and never into the built rows the card caches: a selection
		// belongs to this frame only, and the row it covers is named by its
		// place in the unscrolled card.
		out[i] = m.selection.DecorateRow(paintRow(line, bodyWidth, selected, hovered, m.active), m.scroll+i)
	}
	if useBar {
		bar, _ := ui.RenderScrollbarWithState(m.ScrollbarParams(), m.scrollbarStyle())
		out = strings.Split(lipglossJoin(out, bar), "\n")
	}
	for i := range out {
		out[i] = strings.Repeat(" ", m.leftPadding()) +
			fitLine(out[i], m.innerWidth()) +
			strings.Repeat(" ", m.rightPadding())
	}
	return strings.Join(out, "\n")
}

func lipglossJoin(body []string, bar string) string {
	barLines := strings.Split(bar, "\n")
	for i := range body {
		b := body[i]
		s := " "
		if i < len(barLines) {
			s = barLines[i]
		}
		body[i] = b + s
	}
	return strings.Join(body, "\n")
}

func (m *Model) hitID(r row) string {
	if m.data == nil {
		return ""
	}
	if r.kind == rowParent {
		if m.data.Parent != nil {
			return m.data.Parent.ID
		}
		return m.data.ParentID
	}
	if r.kind == rowChild && r.childIdx >= 0 && r.childIdx < len(m.data.Children) {
		return m.data.Children[r.childIdx].ID
	}
	return ""
}

// Hits returns the interactive rectangles from the last View, in view-local
// cells. Hosts that want per-row regions register these; hosts that only
// have a pane box can call HandleClick with local coordinates instead.
func (m *Model) Hits() []Hit { return append([]Hit(nil), m.hits...) }

// HandleKey applies issue keys. When the card is not active, arrows and j/k
// only scroll. When it is active, arrows walk parent/subtasks/siblings and
// enter opens the selected row; j/k, paging, and g/G still scroll.
// A navigation key returns a Load command for the new issue.
func (m *Model) HandleKey(k tea.KeyMsg) (bool, tea.Cmd) {
	return m.handleKeyString(k.String())
}

func (m *Model) handleKeyString(key string) (bool, tea.Cmd) {
	switch key {
	case "j":
		m.Scroll(1)
		return true, nil
	case "k":
		m.Scroll(-1)
		return true, nil
	case "ctrl+d", "pgdown":
		m.Scroll(max(m.height/2, 1))
		return true, nil
	case "ctrl+u", "pgup":
		m.Scroll(-max(m.height/2, 1))
		return true, nil
	case "g", "home":
		m.scroll = 0
		return true, nil
	case "G", "end":
		m.scroll = m.maxScroll()
		return true, nil
	case "O":
		// Answered before the active check: opening the issue in td is about
		// the card, not about walking the epic, so a focused-but-inactive card
		// takes it too.
		if m.OpenInTDHandler != nil {
			return true, m.OpenInTDHandler(m.SelectedID())
		}
	}

	if !m.active {
		switch key {
		case "down":
			m.Scroll(1)
			return true, nil
		case "up":
			m.Scroll(-1)
			return true, nil
		default:
			return false, nil
		}
	}

	switch key {
	case "down":
		m.moveCursor(1)
		return true, nil
	case "up":
		return true, m.moveUp()
	case "left":
		return true, m.moveSibling(-1)
	case "right":
		return true, m.moveSibling(1)
	case "enter":
		return true, m.OpenSelection()
	default:
		return false, nil
	}
}

// HandleClick selects and opens a parent or child row at view-local (x, y).
// A click on empty chrome still only activates the card so the next arrow key
// can navigate; hosts do not need a separate double-click path for issue rows.
// A click on an interactive scrollbar instead begins a drag gesture and never
// activates anything: see scrollbar.go.
func (m *Model) HandleClick(x, y int) (HitKind, tea.Cmd) {
	// The bar is answered before any row hit can be: action buttons live in
	// the card's rows, and a press on the bar must never open one.
	if m.beginScrollbarGesture(x, y) {
		return HitScrollbar, nil
	}
	// A fresh press implies the previous gesture's button came up. If its end
	// was never reported back, settle here so the thumb cannot stay rendered
	// pressed under a host that wires clicks but not drags.
	m.settleStaleScrollbarGesture()
	// Resolve the row against the frame that was clicked before changing focus:
	// activation can add an ACTIONS row, which invalidates this frame's hits.
	var clicked *Hit
	if hit := m.hitAt(x, y); hit != nil {
		copy := *hit
		clicked = &copy
	}
	m.SetActive(true)
	m.SetFocused(true)
	if clicked != nil {
		m.cursor = clicked.Cursor
		m.ensureCursorVisible()
		return clicked.Kind, m.OpenSelection()
	}
	return HitBody, nil
}

// HandleHover updates the hover highlight from view-local coordinates. The
// scrollbar column answers first: hovering the bar highlights the bar and
// clears any row highlight, the same exclusivity a row hover has.
func (m *Model) HandleHover(x, y int) {
	// A hover can only be delivered while the shared mouse handler holds no
	// drag — motion during a real drag arrives as ActionDrag instead — so one
	// landing on a card that still holds a scrollbar gesture proves the
	// gesture's release was lost or never wired. Settle before it can render
	// a thumb stuck pressed.
	m.settleStaleScrollbarGesture()
	if m.scrollbarContains(x, y) {
		m.scrollbarHover = true
		m.hover = -1
		return
	}
	m.scrollbarHover = false
	if hit := m.hitAt(x, y); hit != nil && hit.Cursor >= 0 {
		m.hover = hit.Cursor
		return
	}
	m.hover = -1
}

func (m *Model) hitAt(x, y int) *Hit {
	for i := range m.hits {
		h := &m.hits[i]
		if y >= h.Y && y < h.Y+h.H && x >= h.X && x < h.X+h.W {
			return h
		}
	}
	return nil
}

// OpenSelection loads the selected parent or child. Nothing selected is a no-op.
func (m *Model) OpenSelection() tea.Cmd {
	items := m.navItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return nil
	}
	return m.navigateTo(items[m.cursor].id)
}

func (m *Model) moveCursor(delta int) {
	items := m.navItems()
	if len(items) == 0 {
		if delta > 0 {
			m.Scroll(1)
		} else {
			m.Scroll(-1)
		}
		return
	}
	if m.cursor < 0 {
		if delta > 0 {
			m.cursor = 0
		} else {
			m.cursor = len(items) - 1
		}
	} else {
		m.cursor += delta
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor >= len(items) {
			m.cursor = len(items) - 1
		}
	}
	m.ensureCursorVisible()
}

// moveUp walks the selection toward the parent. From the parent row, or from
// the issue itself when a parent exists and nothing is selected, it loads
// the epic — that is the "keyboard goes up to the epic" path.
func (m *Model) moveUp() tea.Cmd {
	items := m.navItems()
	if len(items) == 0 {
		m.Scroll(-1)
		return nil
	}
	if m.cursor < 0 {
		if items[0].kind == navParent {
			return m.navigateTo(items[0].id)
		}
		m.cursor = len(items) - 1
		m.ensureCursorVisible()
		return nil
	}
	if m.cursor == 0 && items[0].kind == navParent {
		return m.navigateTo(items[0].id)
	}
	m.moveCursor(-1)
	return nil
}

func (m *Model) moveSibling(delta int) tea.Cmd {
	if m.data == nil || len(m.data.Siblings) < 2 {
		return nil
	}
	idx := -1
	for i, s := range m.data.Siblings {
		if s.ID == m.issueID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	next := idx + delta
	n := len(m.data.Siblings)
	next = (next%n + n) % n
	if m.data.Siblings[next].ID == m.issueID {
		return nil
	}
	return m.navigateTo(m.data.Siblings[next].ID)
}

func (m *Model) navigateTo(id string) tea.Cmd {
	if id == "" || id == m.issueID {
		return nil
	}
	if m.OpenHandler != nil {
		return m.OpenHandler(id)
	}
	// Tests and hosts that injected data without Load still need a way
	// to observe the destination. Reload is skipped; the host can read
	// SelectedID and fetch itself. When workDir is known, Load it.
	if m.workDir == "" {
		m.issueID = id
		return nil
	}
	return m.Load(m.modelID, m.workDir, id, m.epoch)
}

// ModelID is the load identity last passed to Load. Hosts route async
// results by this, not by pane-leaf identity.
func (m *Model) ModelID() int { return m.modelID }

// SelectedID is the issue the cursor is on, or the current issue when
// nothing is selected.
func (m *Model) SelectedID() string {
	items := m.navItems()
	if m.cursor >= 0 && m.cursor < len(items) {
		return items[m.cursor].id
	}
	return m.issueID
}

func (m *Model) navItems() []navItem {
	if m.data == nil {
		return nil
	}
	var items []navItem
	if m.data.Parent != nil && m.data.Parent.ID != "" {
		items = append(items, navItem{kind: navParent, id: m.data.Parent.ID})
	} else if m.data.ParentID != "" {
		items = append(items, navItem{kind: navParent, id: m.data.ParentID})
	}
	for i, c := range m.data.Children {
		items = append(items, navItem{kind: navChild, id: c.ID, childIdx: i})
	}
	return items
}

func (m *Model) actionHints(width int) string {
	var parts []string
	if m.active {
		if m.data != nil && (m.data.Parent != nil || m.data.ParentID != "") {
			parts = append(parts, hint("↑", "epic"))
		}
		if m.data != nil && len(m.data.Siblings) > 1 {
			parts = append(parts, hint("←→", "issues"))
		}
		if m.data != nil && len(m.data.Children) > 0 {
			parts = append(parts, hint("↓", "tasks"), hint("↵", "open"))
		}
	} else if m.focused {
		parts = append(parts, hint("↵", "activate"))
	}
	// O sits with the card's own keys because it is one: it is offered exactly
	// when a host wired the capability, and on every surface that did.
	if m.OpenInTDHandler != nil && (m.active || m.focused) {
		parts = append(parts, hint("O", "td"))
	}
	for _, h := range m.hints {
		parts = append(parts, hint(h.Key, h.Label))
	}
	if len(parts) == 0 {
		return ""
	}
	line := strings.Join(parts, "  ")
	if ansi.StringWidth(line) > width {
		return ansi.Truncate(line, width, "")
	}
	return line
}

func hint(key, label string) string {
	return styles.KeyHint.Render(key) + " " + styles.Muted.Render(label)
}

func (m *Model) ensureCursorVisible() {
	rows := m.ensureRows()
	target := -1
	for i, r := range rows {
		if r.cursor == m.cursor && m.cursor >= 0 {
			target = i
			break
		}
	}
	if target < 0 {
		return
	}
	if target < m.scroll {
		m.scroll = target
	}
	if target >= m.scroll+m.height {
		m.scroll = target - m.height + 1
	}
	m.clampScroll()
}

func (m *Model) contentWidth() int {
	// Geometry, one decision per term: rows are built to
	// width − horizontalContentPadding×2 (the card's own 1+1 inset) − 1
	// reserved scrollbar column. The scrollbar column is ALWAYS reserved when
	// the box is wide enough — needsScrollbar ignores actual overflow on
	// purpose. The track renders as a blank spacer when everything fits, and
	// paying it unconditionally means the body never reflows the first time
	// content crosses the viewport. Rows are built against exactly this width
	// by a compact-document renderer (see New), so wrapped text reaches the
	// right chrome edge with nothing hidden underneath.
	w := m.innerWidth()
	if m.needsScrollbar() {
		w--
	}
	if w < 0 {
		return 0
	}
	return w
}

func (m *Model) innerWidth() int {
	return max(m.width-m.leftPadding()-m.rightPadding(), 0)
}

func (m *Model) leftPadding() int {
	if m.width <= 0 {
		return 0
	}
	return horizontalContentPadding
}

func (m *Model) rightPadding() int {
	if m.width <= horizontalContentPadding {
		return 0
	}
	return horizontalContentPadding
}

func (m *Model) needsScrollbar() bool {
	return m.width >= 8 && m.height > 0
}

func (m *Model) visibleRows() []row {
	all := m.ensureRows()
	if m.scroll > len(all) {
		m.scroll = max(len(all)-m.height, 0)
	}
	end := m.scroll + m.height
	if end > len(all) {
		end = len(all)
	}
	if m.scroll < 0 || m.scroll > end {
		return nil
	}
	return all[m.scroll:end]
}

func (m *Model) ensureRows() []row {
	if m.loading {
		return []row{
			{kind: rowText, text: styles.Muted.Render("Loading issue…"), cursor: -1},
			{kind: rowText, text: styles.Subtle.Render(m.issueID), cursor: -1},
		}
	}
	if m.err != nil {
		return []row{
			{kind: rowText, text: styles.ToastError.Render(" Issue unavailable "), cursor: -1},
			{kind: rowText, text: styles.Subtle.Render(m.issueID), cursor: -1},
			{kind: rowText, text: styles.Muted.Render(m.err.Error()), cursor: -1},
		}
	}
	if m.data == nil {
		return []row{{kind: rowText, text: styles.Muted.Render("No issue"), cursor: -1}}
	}
	if style := m.renderer.StyleKey(); m.buildFor != m.width || m.buildStyle != style || m.rows == nil {
		m.rows = m.buildRows()
		m.buildFor = m.width
		m.buildStyle = style
	}
	return m.rows
}

// Scroll moves the viewport by delta rows and clamps it to the issue.
func (m *Model) Scroll(delta int) {
	m.scroll += delta
	m.clampScroll()
}

// ScrollOffset is the current (or still-pending restore) viewport offset.
func (m *Model) ScrollOffset() int {
	if m.hasPendingScroll {
		return m.pendingScroll
	}
	return m.scroll
}

// ScrollAtBoundary reports whether delta would leave this issue viewport
// unchanged. It is safe to ask from Bubble Tea's pre-update input filter.
func (m *Model) ScrollAtBoundary(delta int) bool {
	if m == nil {
		return true
	}
	return (sharedscroll.Bounds{Position: m.ScrollOffset(), Maximum: m.maxScroll()}).AtBoundary(delta)
}

// SetPendingScroll remembers an offset for the current load generation. A
// stale result cannot consume it because SetResult rejects stale generations
// before applying or clearing this value.
func (m *Model) SetPendingScroll(offset int) {
	m.pendingScroll = max(offset, 0)
	m.hasPendingScroll = true
}

// IssueID returns the issue this model is targeting.
func (m *Model) IssueID() string { return m.issueID }

// WorkDir is the directory the card fetches from: the host-supplied root for a
// local issue, or the owning project's root after a cross-project hit. Hosts
// use it to point live watchers at the store that can actually change this
// card.
func (m *Model) WorkDir() string { return m.workDir }

// Owner returns the owning project's display name and store root when the
// current issue was resolved in another configured project; empty strings for
// a local issue.
func (m *Model) Owner() (name, root string) { return m.foundIn, m.workDir }

// RestoreOwner reinstates a persisted cross-project adoption before the first
// load, so a restored tab refetches from the owning store — and keeps its
// badge — without re-running the search. Only the restore path calls this.
func (m *Model) RestoreOwner(name, root string) {
	if name == "" || root == "" {
		return
	}
	m.foundIn = name
	m.workDir = root
}

// Title returns the issue's headline, or its ID before data arrives. A
// cross-project card is prefixed with its owning project so tab strips,
// overview headers, and modal titles carry the context, not just the card.
func (m *Model) Title() string {
	if m.data == nil {
		return m.issueID
	}
	title := Heading(m.data)
	if m.foundIn != "" {
		return "[" + m.foundIn + "] " + title
	}
	return title
}

// FoundIn is the owning project's display name, or empty for a local issue.
func (m *Model) FoundIn() string { return m.foundIn }

// Loading reports whether a fetch is outstanding.
func (m *Model) Loading() bool { return m.loading }

func (m *Model) invalidateRender() {
	m.buildFor = -1
	m.buildStyle = ""
	m.rows = nil
	m.hits = nil
	m.renderGeneration++
}

func (m *Model) maxScroll() int {
	return max(len(m.ensureRows())-m.height, 0)
}

func (m *Model) clampScroll() {
	m.scroll = min(max(m.scroll, 0), m.maxScroll())
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	// A terminal expands tabs based on their current visual column, while ANSI
	// width helpers count them as zero-width control characters. Normalize them
	// first so the subsequent clamp describes what the terminal will display.
	line = ui.ExpandTabs(line, tabStopWidth)
	line = ansi.Truncate(line, width, "")
	if padding := width - ansi.StringWidth(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

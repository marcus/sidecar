package resourceview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/terminallink"
)

// Surface is what the app injects provider state into. It is declared here
// rather than in the app so each surface can assert it at compile time:
//
//	var _ resourceview.Surface = (*Plugin)(nil)
//
// The app reaches surfaces through a type assertion, and an assertion that
// quietly fails is a surface that never receives a matcher — the pane would
// simply never appear, with nothing to debug. The assertion in each package is
// what turns that into a build error.
type Surface interface {
	// SetResourceMatchers publishes the live matcher snapshot. An empty slice
	// is meaningful: it is how output goes back to being ordinary text.
	SetResourceMatchers([]terminallink.ResourceMatcher)
	// SetResourceResolver injects how a reference becomes a document, and
	// returns the work that injection starts.
	//
	// Returning a command is the point, not a convenience. A tab restored
	// before any provider was ready is armed, and its card promises it resolves
	// when the provider reports ready. Readiness arrives here and nowhere else,
	// so a surface that only rebound its tabs would leave that promise unkept
	// and the user re-resolving every tab by hand after each relaunch. A
	// surface with nothing on screen waiting returns nil.
	SetResourceResolver(Resolver) tea.Cmd
}

// Host is everything a Resource leaf needs from the surface showing it, and
// deliberately nothing more. Each method is a place the project Workspace and
// the global Workspaces browser genuinely differ:
//
//   - the project has a pane tree with leaf IDs; the global has one preview
//     slot;
//   - the project persists references to disk; the global is memory-only by
//     design, matching its document/issue/diff lifetime;
//   - only the project has a live tmux pane to abandon and an interactive
//     input mode to leave.
//
// Everything that does NOT differ lives on Pane below, so neither surface can
// answer a resource key differently from the other by accident.
type Host interface {
	// FocusLeaf moves focus to the leaf holding this pane. It is called on
	// every activation, including one that only focuses an existing tab.
	FocusLeaf()

	// EnterFromTerminal is the ritual a click in terminal output owes before
	// a new leaf appears: clear the selection, leave interactive input, and
	// freeze the terminal viewport so the pre-click context survives the
	// tmux resize. A surface with no live terminal implements it as a no-op.
	EnterFromTerminal()

	// Persist records reference-only tab state. The global surface implements
	// it as a no-op; that difference is the intended one.
	Persist()

	// OpenURL puts a validated http(s) URL in front of the user through the
	// host's own confirmed path.
	OpenURL(url string) tea.Cmd
}

// Pane is the host-independent behavior of one Resource leaf: what a click
// does, what the documented keys do, and when state is persisted.
//
// Both surfaces own a Pane and implement Host. A binding that reimplements one
// of these methods instead of calling it is the drift this type exists to
// prevent.
type Pane struct {
	Tabs *Tabs
	host Host
	// saved is the persisted projection as it was at the last save, so a key
	// that changed nothing a reader would see does not cost a write. See
	// persist.
	saved paneSnapshot
}

// NewPane binds a tab set to a surface.
func NewPane(tabs *Tabs, host Host) *Pane {
	return &Pane{Tabs: tabs, host: host}
}

// ActivateFromTerminal is the click journey: the terminal ritual first, then
// focus, then open-or-focus the tab, then persist. The order matters — the
// viewport must be frozen before the new leaf resizes tmux — and getting it
// right once here is the point.
func (p *Pane) ActivateFromTerminal(ref Ref) tea.Cmd {
	if p == nil || p.Tabs == nil {
		return nil
	}
	if p.host != nil {
		p.host.EnterFromTerminal()
		p.host.FocusLeaf()
	}
	cmd := p.Tabs.Open(ref)
	p.persist()
	return cmd
}

// Activate opens or focuses a tab without the terminal ritual. This is the
// `sidecar open --provider` path: there was no click in terminal output, so
// there is no selection to clear and no viewport to freeze.
func (p *Pane) Activate(ref Ref) tea.Cmd {
	if p == nil || p.Tabs == nil {
		return nil
	}
	if p.host != nil {
		p.host.FocusLeaf()
	}
	cmd := p.Tabs.Open(ref)
	p.persist()
	return cmd
}

// SelectTab focuses a tab by index, resolving it if it was only armed.
func (p *Pane) SelectTab(i int) tea.Cmd {
	if p == nil || p.Tabs == nil {
		return nil
	}
	if p.host != nil {
		p.host.FocusLeaf()
	}
	cmd := p.Tabs.SetActive(i)
	p.persist()
	return cmd
}

// CycleTab moves through the strip, wrapping.
func (p *Pane) CycleTab(delta int) tea.Cmd {
	if p == nil || p.Tabs == nil {
		return nil
	}
	cmd := p.Tabs.Cycle(delta)
	p.persist()
	return cmd
}

// CloseActiveTab closes the focused tab and reports whether the leaf is now
// empty, which is the host's cue to collapse the split.
func (p *Pane) CloseActiveTab() (empty bool, cmd tea.Cmd) {
	if p == nil || p.Tabs == nil {
		return true, nil
	}
	empty = p.Tabs.CloseActive()
	p.persist()
	return empty, nil
}

// CloseTab closes the tab at index through the same last-tab empty report.
func (p *Pane) CloseTab(i int) (empty bool, cmd tea.Cmd) {
	if p == nil || p.Tabs == nil {
		return true, nil
	}
	empty = p.Tabs.Close(i)
	p.persist()
	return empty, nil
}

// Refresh re-resolves the active tab, bypassing cache freshness.
func (p *Pane) Refresh() tea.Cmd {
	if p == nil || p.Tabs == nil {
		return nil
	}
	return p.Tabs.RefreshActive()
}

// OpenSource opens the active document's validated source URL. A document
// without one, or a tab that has not resolved, does nothing.
func (p *Pane) OpenSource() tea.Cmd {
	if p == nil || p.Tabs == nil || p.host == nil {
		return nil
	}
	m := p.Tabs.Active()
	if m == nil {
		return nil
	}
	url := m.SourceURL()
	if url == "" {
		return nil
	}
	return p.host.OpenURL(url)
}

// Apply routes a resolve result and persists the outcome, since a canonical
// re-key changes what should be on disk.
func (p *Pane) Apply(msg ResolvedMsg) bool {
	if p == nil || p.Tabs == nil {
		return false
	}
	if !p.Tabs.Apply(msg) {
		return false
	}
	p.persist()
	return true
}

// ReArmPending is the counterpart to a host discarding results in bulk. Call
// it wherever results stop being routed — a workspace row switch, a project
// change — so a tab is never stranded on a loading card.
func (p *Pane) ReArmPending() int {
	if p == nil || p.Tabs == nil {
		return 0
	}
	return p.Tabs.ReArmPending()
}

// ScrollAtBoundary reports whether a scroll of delta would move the active tab.
func (p *Pane) ScrollAtBoundary(delta int) bool {
	if p == nil || p.Tabs == nil {
		return true
	}
	m := p.Tabs.Active()
	if m == nil {
		return true
	}
	return m.ScrollAtBoundary(delta)
}

// Scroll moves the active tab's viewport and reports whether it moved.
func (p *Pane) Scroll(delta int) bool {
	if p == nil || p.Tabs == nil {
		return false
	}
	m := p.Tabs.Active()
	if m == nil {
		return false
	}
	moved := m.ScrollBy(delta)
	if moved {
		p.persist()
	}
	return moved
}

// HandleKey answers the documented Resource-pane keys. It deliberately does
// NOT claim q or esc: those follow each surface's existing content-pane
// close/hide rule, which is the one key behavior that legitimately differs.
//
// handled=false means the host should keep looking.
func (p *Pane) HandleKey(key string) (handled bool, cmd tea.Cmd) {
	if p == nil || p.Tabs == nil || p.Tabs.Empty() {
		return false, nil
	}
	// A plugin-shaped tab answers first, with the browser's own keys. They are
	// the same keys the global tab binds, which is the whole point of one
	// browser: j/k, Enter, /, v, r, a and o mean what they mean everywhere.
	// { and } stay the leaf's, because the strip is the leaf's.
	if m := p.Tabs.Active(); m != nil && m.IsPlugin() && key != "{" && key != "}" {
		if cmd, ok := p.Tabs.HandlePluginKey(key); ok {
			p.persist()
			return true, cmd
		}
	}
	switch key {
	case "r":
		return true, p.Refresh()
	case "o":
		return true, p.OpenSource()
	case "}":
		return true, p.CycleTab(1)
	case "{":
		return true, p.CycleTab(-1)
	case "up", "k":
		p.Scroll(-1)
		return true, nil
	case "down", "j":
		p.Scroll(1)
		return true, nil
	case "pgup":
		p.Scroll(-p.pageStep())
		return true, nil
	case "pgdown":
		p.Scroll(p.pageStep())
		return true, nil
	}
	return false, nil
}

func (p *Pane) pageStep() int {
	if m := p.Tabs.Active(); m != nil && m.Height() > 1 {
		return m.Height() - 1
	}
	return 1
}

// persist saves the leaf's tabs — but only when what would be written has
// actually changed. Every consumed key asks, and on the project surface the
// host's answer is a state.json write, so an unconditional persist would spend
// one synchronous save per character typed into a query line. The comparison is
// over the reference-only projection that IS the saved record, so anything that
// moves the saved position (a committed query, a view, a sort, the cursor, the
// scroll, a tab opening or closing) still writes exactly once.
func (p *Pane) persist() {
	if p == nil || p.host == nil {
		return
	}
	if p.Tabs != nil {
		next := paneSnapshot{set: true, active: p.Tabs.ActiveIndex(), tabs: p.Tabs.References()}
		if next.equal(p.saved) {
			return
		}
		p.saved = next
	}
	p.host.Persist()
}

// paneSnapshot is the persisted projection of a leaf at one moment.
type paneSnapshot struct {
	set    bool
	active int
	tabs   []PersistedTab
}

func (s paneSnapshot) equal(other paneSnapshot) bool {
	// An unset snapshot equals nothing: the first save after a pane is built
	// always happens, whatever it happens to hold.
	if !s.set || !other.set {
		return false
	}
	if s.active != other.active || len(s.tabs) != len(other.tabs) {
		return false
	}
	for i := range s.tabs {
		if !s.tabs[i].Equal(other.tabs[i]) {
			return false
		}
	}
	return true
}

// Stable command IDs for a Resource leaf. They are declared here, not derived
// per host, because the footer resolves a hint's key through the keymap by ID:
// two hosts inventing their own IDs is two surfaces advertising different keys
// for one pane. They follow the vocabulary the other panes already use —
// prev-tab/next-tab rather than one combined "{/}" command, which is a rule
// TestBracesAlwaysMeanTabCycling enforces repo-wide.
const (
	CommandRefresh    = "refresh"
	CommandOpenSource = "open-source"
	CommandPrevTab    = "prev-tab"
	CommandNextTab    = "next-tab"
	CommandCloseTab   = "close-tab"
)

// Commands is the footer vocabulary for a focused Resource leaf. Both hosts
// register exactly this, so the two surfaces cannot advertise different keys
// for the same pane. Names are one word to keep the footer from wrapping.
func Commands() []Command {
	return []Command{
		{ID: CommandRefresh, Key: "r", Name: "Refresh"},
		{ID: CommandOpenSource, Key: "o", Name: "Open"},
		{ID: CommandNextTab, Key: "}", Name: "Next"},
		{ID: CommandPrevTab, Key: "{", Name: "Prev"},
		{ID: CommandCloseTab, Key: "x", Name: "Close"},
	}
}

// Command is one footer hint. ID is the keymap command the host registers
// under; Key is the default binding it documents.
type Command struct {
	ID   string
	Key  string
	Name string
}

package pluginbrowser

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginhost"
)

// Command IDs. They are what the footer's key chips are keyed by, so they are
// stable strings rather than derived from anything a plugin says.
const (
	cmdMove     = "plugin-move"
	cmdOpen     = "plugin-open"
	cmdQuery    = "plugin-query"
	cmdView     = "plugin-view"
	cmdRefresh  = "plugin-refresh"
	cmdActions  = "plugin-actions"
	cmdSource   = "plugin-source"
	cmdCoverage = "plugin-coverage"

	cmdModalMove   = "plugin-modal-move"
	cmdModalSelect = "plugin-modal-select"
	cmdModalClose  = "plugin-modal-close"
	cmdModalSubmit = "plugin-modal-submit"
)

// TabPlugin hosts one browser as a navbar surface. Its Calls are built once, at
// construction: each closure reads the live manager when it runs, so a browser
// built before the host had one observes it appearing without the render path
// rebuilding a seam every frame.
//
// It is the whole of what a protocol plugin needs to be a tab: the descriptor
// says it has one, the app builds a globalPluginHost around this, and every
// capability the shell asks for is answered here rather than by each plugin.
type TabPlugin struct {
	id    string
	name  string
	model *Model
	calls func(instance string) Calls

	ctx     *plugin.Context
	focused bool
	width   int
	height  int

	// listOuter is the last split the view drew, kept so the content-link
	// rectangle describes the frame that is actually on screen.
	listOuter   int
	detailOuter int
}

// NewTabPlugin builds the tab surface for one configured instance. It performs
// no I/O and starts nothing: everything it shows comes from a describe pass the
// host runs after the first ready frame.
func NewTabPlugin(instance, name string, calls func(instance string) Calls) *TabPlugin {
	p := &TabPlugin{id: instance, name: name, calls: calls, focused: true}
	var c Calls
	if calls != nil {
		c = calls(instance)
	}
	p.model = New(instance, name, c, nil)
	p.model.SetReservedKeys(surfaceReservedKeys())
	return p
}

// surfaceReservedKeys is what a plugin-suggested action letter is refused
// against: the host's non-negotiables and the keys sidecar's own root handler
// acts on while a plugin tab is focused. The browser's own keys are refused
// separately, inside the model.
func surfaceReservedKeys() map[string]bool {
	out := make(map[string]bool, len(keymap.HostReservedKeys)+len(keymap.GlobalKeys)+1)
	for k := range keymap.HostReservedKeys {
		out[k] = true
	}
	for k := range keymap.GlobalKeys {
		out[k] = true
	}
	// `n` is the pane switcher's everywhere it exists, and the browser must not
	// let a plugin take it in the one surface where it does.
	out["n"] = true
	return out
}

// Model exposes the browser, for a host that needs to read its state.
func (p *TabPlugin) Model() *Model { return p.model }

func (p *TabPlugin) ID() string { return p.id }

func (p *TabPlugin) Name() string { return p.model.Name() }

// Icon is the header glyph. A protocol plugin declares none, so the host takes
// the first letter of its name — which is what the header would have shown
// anyway, and never a glyph a plugin could pick to impersonate a built-in tab.
func (p *TabPlugin) Icon() string {
	for _, r := range p.model.Name() {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "P"
}

// Init records the host context and publishes the browser's key table. It does
// no I/O: a protocol plugin's first process runs after the first ready frame,
// from the host's describe pass.
func (p *TabPlugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx
	if ctx == nil || ctx.Keymap == nil {
		return nil
	}
	context := p.browseContext()
	for _, b := range []struct{ key, command string }{
		{"j", cmdMove}, {"k", cmdMove},
		{"enter", cmdOpen},
		{"/", cmdQuery},
		{"v", cmdView},
		{"r", cmdRefresh},
		{"a", cmdActions},
		{"c", cmdCoverage},
		{"o", cmdSource},
	} {
		ctx.Keymap.RegisterPluginBinding(b.key, b.command, context)
	}
	// An overlay is its own context: while it owns the keyboard the footer must
	// describe it, not the list underneath, or the bar advertises keys that are
	// already spoken for.
	modal := p.modalContext()
	for _, b := range []struct{ key, command string }{
		{"j", cmdModalMove}, {"k", cmdModalMove},
		{"enter", cmdModalSelect},
		{"ctrl+enter", cmdModalSubmit},
		{"esc", cmdModalClose},
	} {
		ctx.Keymap.RegisterPluginBinding(b.key, b.command, modal)
	}
	return nil
}

// Start returns no command. Everything the browser shows arrives on a
// DescribedMsg the host broadcasts once its describe pass has settled, so
// nothing here waits on a process.
func (p *TabPlugin) Start() tea.Cmd { return nil }

// Stop cancels nothing: the browser owns no process. The manager owns every
// child and the app cancels it at shutdown.
func (p *TabPlugin) Stop() {}

func (p *TabPlugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// A key only reaches a global host through forwardKeyToPlugin, which
		// addresses the visible tab, so there is no focus test here: a test
		// would be this surface second-guessing the ladder that already
		// decided.
		cmd, _ := p.model.HandleKey(msg)
		return p, cmd
	case tea.MouseMsg:
		return p, p.model.HandleMouse(msg)
	case plugin.PluginFocusedMsg:
		return p, p.model.Refresh()
	}
	return p, p.model.Update(msg)
}

func (p *TabPlugin) View(width, height int) string {
	p.width, p.height = width, height
	p.model.SetSize(width, height)
	p.listOuter, p.detailOuter = p.model.split()
	return p.model.View()
}

// ViewIsSelfConstrained reports that View returns exactly the box it was given.
func (p *TabPlugin) ViewIsSelfConstrained() bool { return true }

func (p *TabPlugin) IsFocused() bool { return p.focused }

func (p *TabPlugin) SetFocused(focused bool) {
	p.focused = focused
	p.model.SetFocused(focused)
}

// FocusContext is per instance, because a plugin-granted action letter belongs
// to one plugin and a shared context would put it on every plugin's footer. An
// open overlay reports its own, so the footer describes what has the keyboard.
func (p *TabPlugin) FocusContext() string {
	if p.model.OverlayOpen() {
		return p.modalContext()
	}
	return p.browseContext()
}

func (p *TabPlugin) browseContext() string { return "plugin-" + p.id }

func (p *TabPlugin) modalContext() string { return "plugin-" + p.id + "-modal" }

// Commands are the footer's, in the order the design language asks for:
// frequency of use, not alphabetical.
func (p *TabPlugin) Commands() []plugin.Command {
	if p.model.OverlayOpen() {
		return p.modalCommands()
	}
	context := p.browseContext()
	commands := []plugin.Command{
		{ID: cmdMove, Name: "Move", Description: "Move the cursor", Category: plugin.CategoryNavigation, Context: context, Priority: 1},
		{ID: cmdOpen, Name: "Open", Description: "Open the row", Category: plugin.CategoryNavigation, Context: context, Priority: 2},
	}
	if c, ok := p.model.ActiveCollection(); ok && c.Search != pluginhost.SearchNone {
		commands = append(commands, plugin.Command{
			ID: cmdQuery, Name: "Query", Description: "Edit the query",
			Category: plugin.CategorySearch, Context: context, Priority: 3,
		})
	}
	if c, ok := p.model.ActiveCollection(); ok && p.model.hasViewControl(c) {
		commands = append(commands, plugin.Command{
			ID: cmdView, Name: "View", Description: "Sort and views",
			Category: plugin.CategoryView, Context: context, Priority: 4,
		})
	}
	commands = append(commands, plugin.Command{
		ID: cmdRefresh, Name: "Refresh", Description: "Re-list the collection",
		Category: plugin.CategoryActions, Context: context, Priority: 5,
	})
	if len(p.model.applicableActions()) > 0 {
		commands = append(commands, plugin.Command{
			ID: cmdActions, Name: "Actions", Description: "Plugin actions",
			Category: plugin.CategoryActions, Context: context, Priority: 6,
		})
	}
	if p.model.hasCoverage() {
		commands = append(commands, plugin.Command{
			ID: cmdCoverage, Name: "Coverage", Description: "What this page's claim means",
			Category: plugin.CategoryView, Context: context, Priority: 7,
		})
	}
	if p.hasSource() {
		commands = append(commands, plugin.Command{
			ID: cmdSource, Name: "Source", Description: "Open the source URL",
			Category: plugin.CategoryActions, Context: context, Priority: 8,
		})
	}
	return commands
}

// modalCommands describe the overlay that has the keyboard. Submit appears only
// where Enter is a newline the user meant to type.
func (p *TabPlugin) modalCommands() []plugin.Command {
	context := p.modalContext()
	commands := []plugin.Command{
		{ID: cmdModalMove, Name: "Move", Description: "Move within the overlay", Category: plugin.CategoryNavigation, Context: context, Priority: 1},
		{ID: cmdModalSelect, Name: "Select", Description: "Choose the focused control", Category: plugin.CategoryActions, Context: context, Priority: 2},
	}
	if p.model.overlay.kind == overlayForm {
		commands = append(commands, plugin.Command{
			ID: cmdModalSubmit, Name: p.model.overlay.action.Title, Description: "Run the action",
			Category: plugin.CategoryActions, Context: context, Priority: 3,
		})
	}
	return append(commands, plugin.Command{
		ID: cmdModalClose, Name: "Close", Description: "Close the overlay",
		Category: plugin.CategoryNavigation, Context: context, Priority: 4,
	})
}

func (p *TabPlugin) hasSource() bool {
	if doc, ok := p.model.DetailDocument(); ok && doc.SourceURL != "" {
		return true
	}
	item, ok := p.model.currentItem()
	return ok && item.SourceURL != ""
}

// ConsumesTextInput forwards typed text to the query line instead of running
// the host's shortcuts.
func (p *TabPlugin) ConsumesTextInput() bool { return p.model.ConsumesTextInput() }

// BlocksGlobalKeys reports that a modal owns the keyboard.
func (p *TabPlugin) BlocksGlobalKeys() bool { return p.model.BlocksGlobalKeys() }

// ClaimsKey reports a live contextual binding, at precedence level 3.
func (p *TabPlugin) ClaimsKey(key string) bool { return p.model.ClaimsKey(key) }

// QuitKeyExits reports whether `q` reaches sidecar's quit flow.
func (p *TabPlugin) QuitKeyExits() bool { return p.model.QuitKeyExits() }

// WheelAtBoundary drops an inertia event that cannot move the surface. It is
// answered for the box under the pointer, because that is the box the notch
// would have scrolled.
func (p *TabPlugin) WheelAtBoundary(msg tea.MouseWheelMsg) bool {
	mi := msg.Mouse()
	switch msg.Button {
	case tea.MouseWheelUp:
		return p.model.ScrollAtBoundaryAt(mi.X, mi.Y, -1)
	case tea.MouseWheelDown:
		return p.model.ScrollAtBoundaryAt(mi.X, mi.Y, 1)
	default:
		return false
	}
}

// PaneFocusStops projects the browser's two windows into the app-owned ring.
func (p *TabPlugin) PaneFocusStops() []plugin.PaneFocusStop {
	ids := p.model.PaneFocusStops()
	out := make([]plugin.PaneFocusStop, 0, len(ids))
	for _, id := range ids {
		out = append(out, plugin.PaneFocusStop{ID: id})
	}
	return out
}

func (p *TabPlugin) PaneFocus() string { return p.model.PaneFocus() }

func (p *TabPlugin) SetPaneFocus(id string) tea.Cmd {
	p.model.SetPaneFocus(id)
	return nil
}

func (p *TabPlugin) SetPaneFocusActive(active bool) { p.model.SetPaneFocusActive(active) }

// ContentLinkSurfaces publishes the detail box as read-only rendered text, so
// an issue key or a path in a plugin's document is clickable exactly as it is
// everywhere else. Nothing in it is renderer-owned: a body may be the plugin's
// own bytes word-wrapped, and an OSC-8 sequence in those is authored content.
func (p *TabPlugin) ContentLinkSurfaces() []contentlink.Surface {
	if p.ctx == nil || p.width <= 0 || p.height <= 0 || p.detailOuter <= 0 {
		return nil
	}
	if p.model.OverlayOpen() {
		return nil
	}
	if _, ok := p.model.DetailDocument(); !ok {
		return nil
	}
	// The scrollbar's reserved column is chrome, not text: a locator cannot be
	// in it, and claiming it would put a link target under the bar.
	w := scrolledWidth(p.detailOuter - chromeOverhead)
	h := p.height - 2
	if w < 1 || h < 1 {
		return nil
	}
	return []contentlink.Surface{{
		ID:          string(FocusDetail),
		Rect:        mouse.Rect{X: p.listOuter + paneGap + 2, Y: 1, W: w, H: h},
		WorkDir:     p.ctx.WorkDir,
		ProjectRoot: p.ctx.ProjectRoot,
		Kinds: contentlink.NewKindSet(
			contentlink.KindFile,
			contentlink.KindIssue,
			contentlink.KindURL,
			contentlink.KindResource,
		),
		ReadOnly: true,
	}}
}

// FooterStatus is the standing condition the host right-aligns on the footer.
// A describe that failed is a condition that stays true until someone fixes it,
// which is exactly what this line is for; the outcome of the page on screen is
// the rest of the time's honest answer.
func (p *TabPlugin) FooterStatus() (string, bool) {
	m := p.model
	if !m.Described() {
		if m.Status().LastError != nil {
			return m.instance + " · " + strings.ToLower(errorHeadline(m.Status().LastError.Code)), true
		}
		return m.instance + " · describing", false
	}
	if flash, isErr := m.Flash(); flash != "" {
		return flash, isErr
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return m.instance + " · ready", false
	}
	s := m.state(c)
	if !s.loaded {
		return m.instance + " · " + c.ID, false
	}
	return m.instance + " · " + c.ID + " · " + string(s.outcome), s.outcome == pluginhost.OutcomeDegraded
}

// Diagnostics report what the host knows about this instance without running
// anything: the describe state, what it declared, and the last typed failure.
func (p *TabPlugin) Diagnostics() []plugin.Diagnostic {
	st := p.model.Status()
	detail := "not described yet"
	if st.LastError != nil {
		detail = string(st.LastError.Code) + ": " + st.LastError.Message
	} else if p.model.Described() {
		desc := p.model.Description()
		detail = pluralize(len(desc.Collections), "collection") + ", " +
			pluralize(len(desc.Actions), "action") + ", " +
			pluralize(len(desc.Matchers), "matcher")
	}
	state := string(st.State)
	if state == "" {
		state = string(pluginhost.StateUnchecked)
	}
	return []plugin.Diagnostic{{
		ID:     "plugin:" + p.id,
		Status: state,
		Detail: detail,
	}}
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TabDescriptors projects every configured protocol instance that declares a
// tab placement onto a descriptor with New attached.
//
// It is a pure function of configuration: it runs no plugin, resolves nothing
// on PATH, and reads nothing from disk, so it is safe before the first frame.
// The describe result that fills in the plugin's real name arrives later, on
// the browser's own DescribedMsg.
func TabDescriptors(cfg *config.Config, calls func(instance string) Calls) []plugin.Descriptor {
	descriptors := plugin.ProtocolDescriptors(cfg)
	out := make([]plugin.Descriptor, 0, len(descriptors))
	for _, d := range descriptors {
		if !d.HasPlacement(plugin.PlacementTab) {
			continue
		}
		instance, name := d.ID, d.Name
		d.New = func() plugin.Plugin { return NewTabPlugin(instance, name, calls) }
		out = append(out, d)
	}
	return out
}

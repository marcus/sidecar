package app

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/startuptrace"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	headerHeight = 1 // single painted header row
	footerHeight = 1
	minWidth     = 60
	minHeight    = 24

	projectSwitcherItemPrefix  = "project-switcher-item-"
	projectSwitcherAddButtonID = "project-switcher-add"
)

// shortenHomePath replaces the user's home directory with ~ for display.
func shortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

// Startup-trace markers: fired once each on the first rendered frame and the
// first frame after the app has real dimensions (i.e. actual usable UI).
var firstFrame, firstReadyFrame sync.Once

// projectSwitcherItemID returns the ID for a project item at the given index.
func projectSwitcherItemID(idx int) string {
	return fmt.Sprintf("%s%d", projectSwitcherItemPrefix, idx)
}

// View renders the entire application UI and declares the terminal features
// (alt-screen, mouse) that were previously NewProgram options in v1.
func (m Model) View() tea.View {
	terminalperf.Record(terminalperf.ApplicationViewRendered)
	if m.ready {
		firstReadyFrame.Do(func() {
			startuptrace.Mark("first ready frame")
			// Same branch, same moment: anything that must not run before the
			// user has a usable UI waits on this latch rather than on Bubble
			// Tea's command scheduling. See resourceproviders.go.
			firstReadyFrameLatch.close()
		})
	} else {
		firstFrame.Do(func() { startuptrace.Mark("first frame (loading)") })
	}
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = m.preferredMouseMode()
	v.Cursor = m.pluginCursor()
	// Deliberately no v.WindowTitle: sidecar emits the terminal title itself
	// (internal/app/title.go). Bubble Tea clears a title it owns every time the
	// renderer stops, which would blank the terminal for the whole duration of
	// an editor or an attached session.
	// Keep KeyboardEnhancements at its zero value. Bubble Tea v2 already
	// requests basic key disambiguation; release events and all-keys escape
	// encoding would alter ordinary text delivery and are intentionally opt-in.
	return v
}

func (m Model) preferredMouseMode() tea.MouseMode {
	// App-level overlays rely on hover motion, regardless of what the covered
	// plugin prefers.
	if !m.ready || m.hasModal() || m.configOpen() {
		return tea.MouseModeAllMotion
	}
	// A global terminal being typed into is the same case as a plugin's: cell
	// motion, so the pane's own application gets clean clicks.
	if m.globalWorkspacesVisible() && m.overview.PreviewOwnsKeyboard() {
		return tea.MouseModeCellMotion
	}
	if mode, ok := m.appContentDocumentEditMouseMode(); ok {
		return mode
	}
	if h := m.currentContentDeck(); h != nil && h.deck.FocusedLeaf() != h.deck.Leaf(panelayout.Primary) {
		return tea.MouseModeAllMotion
	}
	if provider, ok := m.focusedSurface().(plugin.MouseModeProvider); ok {
		switch mode := provider.PreferredMouseMode(); mode {
		case tea.MouseModeCellMotion, tea.MouseModeAllMotion:
			return mode
		}
	}
	return tea.MouseModeAllMotion
}

func (m Model) pluginCursor() *tea.Cursor {
	if !m.ready || !m.applicationFocused || m.hasModal() ||
		m.width < minWidth || m.height < minHeight {
		return nil
	}
	if m.configOpen() {
		// Configuration's inputs draw their own cursor, like every other
		// sidecar-owned surface; no native cursor is placed for them.
		return nil
	}
	if m.inGlobalScope() {
		if h := m.currentContentDeck(); h != nil {
			if h.deck.FocusedLeaf() != h.deck.Leaf(panelayout.Primary) {
				if cursor := m.appContentDocumentEditCursor(); cursor != nil {
					return m.placeContentCursor(cursor)
				}
				return nil
			}
			cursor := providerCursor(h.plugin)
			if cursor == nil {
				return nil
			}
			cursor.X += h.primaryInner.X
			cursor.Y += h.primaryInner.Y
			return m.placeContentCursor(cursor)
		}
		return m.placeContentCursor(m.globalCursor())
	}
	active := m.ActivePlugin()
	if active == nil || !active.IsFocused() {
		return nil
	}
	if h := m.currentContentDeck(); h != nil {
		if h.deck.FocusedLeaf() != h.deck.Leaf(panelayout.Primary) {
			if cursor := m.appContentDocumentEditCursor(); cursor != nil {
				return m.placeContentCursor(cursor)
			}
			return nil
		}
		cursor := providerCursor(active)
		if cursor == nil {
			return nil
		}
		cursor.X += h.primaryInner.X
		cursor.Y += h.primaryInner.Y
		return m.placeContentCursor(cursor)
	}
	provider, ok := active.(plugin.CursorProvider)
	if !ok {
		return nil
	}
	return m.placeContentCursor(provider.Cursor())
}

func providerCursor(p plugin.Plugin) *tea.Cursor {
	provider, ok := p.(plugin.CursorProvider)
	if !ok {
		return nil
	}
	return provider.Cursor()
}

// globalCursor is the only cursor the global space draws: the Workspaces
// browser's embedded terminal while the user is typing into it. Every other
// global surface is a reader and reports none.
func (m Model) globalCursor() *tea.Cursor {
	if !m.globalWorkspacesVisible() {
		return nil
	}
	return m.overview.WorkspacesCursor()
}

// placeContentCursor moves a surface-local cursor into screen coordinates and
// drops one the content area does not contain.
func (m Model) placeContentCursor(local *tea.Cursor) *tea.Cursor {
	if local == nil {
		return nil
	}
	cursor := *local
	cursor.Y += headerHeight
	contentHeight := m.height - headerHeight - footerHeight
	if cursor.X < 0 || cursor.X >= m.width || cursor.Y < headerHeight ||
		cursor.Y >= headerHeight+contentHeight {
		return nil
	}
	return &cursor
}

// viewContent builds the rendered screen as a string (header, content, footer,
// and any modal overlays). It is wrapped into a tea.View by View().
func (m Model) viewContent() string {
	if !m.ready {
		return "Loading..."
	}

	// Show warning if terminal is too small
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("Terminal too small (%dx%d)\nMinimum: %dx%d",
			m.width, m.height, minWidth, minHeight)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styles.StatusBlocked.Render(msg))
	}

	// Calculate content area
	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 0 {
		contentHeight = 0
	}

	// Build layout
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Main content. contentWidth is what a reserved right-hand column (the
	// notification centre) narrows; the toast is placed against this region's
	// right edge rather than the terminal's, so it never lands under the panel.
	contentWidth := m.contentWidth()
	// Exactly one pane is drawn focused, app-wide. The centre is a focus stop
	// that is not a pane, so the surface underneath cannot see that it lost the
	// keyboard; the shell says so once, at the shared border rule, for the
	// duration of this content render. Every surface inherits it — see
	// internal/styles/focus.go. Attention (toasts, pane flash) deliberately does
	// not set it, and the centre panel, toasts and modals are all drawn after the
	// signal is cleared, so their own chrome is untouched.
	styles.SetFocusHeldOutsidePanes(m.notificationCentreOwnsKeys())
	content := m.renderContent(contentWidth, contentHeight)
	styles.SetFocusHeldOutsidePanes(false)
	// The centre is drawn beside the content, never over it: the shell hands
	// the surfaces a narrower box and spends the difference here.
	if panel := m.renderNotificationCentre(contentHeight); panel != "" {
		content = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).
				Height(contentHeight).MaxHeight(contentHeight).Render(content),
			panel)
	}
	b.WriteString(content)

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	bg := b.String()
	// Toasts float over the content region, under any modal: a modal has the
	// user's attention already, and a block drawn over it would be unreadable.
	bg = m.renderToastOverlay(bg, 0, headerHeight, contentWidth, contentHeight)
	// The flash shares that corner, one row below any toast: same region, same
	// margins, so both tiers move together when the centre narrows the content.
	bg = m.renderFlashOverlay(bg, 0, headerHeight, contentWidth, contentHeight)

	// Overlay modals (priority order via activeModal)
	switch m.activeModal() {
	case ModalPalette:
		return m.renderPaletteOverlay(bg)
	case ModalHelp:
		return m.renderHelpModal(bg)
	case ModalUpdate:
		return m.renderUpdateModalOverlay(bg)
	case ModalDiagnostics:
		return m.renderDiagnosticsModal(bg)
	case ModalQuitConfirm:
		return m.renderQuitConfirmOverlay(bg)
	case ModalProjectSwitcher:
		return m.renderProjectSwitcherOverlay(bg)
	case ModalWorktreeSwitcher:
		return m.renderWorktreeSwitcherModal(bg)
	case ModalThemeSwitcher:
		return m.renderThemeSwitcherModal(bg)
	case ModalOpenIn:
		return m.renderOpenInModal(bg)
	case ModalIssueInput:
		return m.renderIssueInputOverlay(bg)
	case ModalIssuePreview:
		return m.renderIssuePreviewOverlay(bg)
	case ModalPaneReposition:
		return m.renderAppPaneLayoutOverlay(bg)
	case ModalPaneSwitcher:
		return (&m).renderPaneSwitcherOverlay(bg)
	}

	return bg
}

// renderPaletteOverlay renders the command palette modal.
func (m Model) renderPaletteOverlay(content string) string {
	modal := m.palette.View()
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderQuitConfirmOverlay renders the quit confirmation modal.
func (m Model) renderQuitConfirmOverlay(content string) string {
	// Lazy init modal if needed
	if m.quitModal == nil {
		// This shouldn't happen, but handle gracefully
		return content
	}
	rendered := m.quitModal.Render(m.width, m.height, m.quitMouseHandler)
	return ui.OverlayModal(content, rendered, m.width, m.height)
}

// renderProjectAddThemePickerOverlay renders the theme picker within add-project.
func (m Model) renderProjectAddThemePickerOverlay(content string) string {
	var b strings.Builder
	maxVisible := 6
	cursorStyle := lipgloss.NewStyle().Foreground(styles.Primary)
	selectedStyle := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)

	// Theme list
	b.WriteString(styles.ModalTitle.Render("Pick Theme"))
	b.WriteString("\n\n")
	b.WriteString(m.projectAddThemeInput.View())
	b.WriteString("\n\n")

	list := m.projectAddThemeFiltered
	visibleCount := len(list)
	if visibleCount > maxVisible {
		visibleCount = maxVisible
	}

	if m.projectAddThemeScroll > 0 {
		b.WriteString(styles.Muted.Render("  ↑ more"))
		b.WriteString("\n")
	}

	for i := m.projectAddThemeScroll; i < m.projectAddThemeScroll+visibleCount && i < len(list); i++ {
		cursor := "  "
		nameStyle := styles.Muted
		if i == m.projectAddThemeCursor {
			cursor = cursorStyle.Render("▸ ")
			nameStyle = selectedStyle
		}
		b.WriteString(cursor)
		b.WriteString(nameStyle.Render(list[i]))
		b.WriteString("\n")
	}

	if len(list) > m.projectAddThemeScroll+visibleCount {
		b.WriteString(styles.Muted.Render("  ↓ more"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.KeyHint.Render("enter"))
	b.WriteString(styles.Muted.Render(" select  "))
	b.WriteString(styles.KeyHint.Render("esc"))
	b.WriteString(styles.Muted.Render(" back"))

	modal := styles.ModalBox.Render(b.String())
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderHeader renders the single physical header row. Its two clusters come
// from headerGeometry, which is also the sole source for mouse hit regions.
func (m Model) renderHeader() string {
	layout := m.headerGeometry()
	header := layout.left
	if gap := layout.rightStart - lipgloss.Width(layout.left); gap > 0 {
		header += strings.Repeat(" ", gap)
	}
	header += layout.right
	header = ansi.Truncate(header, max(0, m.width), "")
	return m.headerBarStyle().Width(m.width).MaxWidth(m.width).Render(header)
}

func (m Model) headerBarStyle() lipgloss.Style {
	return styles.Header
}

type headerTab struct {
	ref        tabRef
	text       string
	start, end int
}

type headerLayout struct {
	left                    string
	right                   string
	rightStart              int
	logoEnd                 int
	globalTabs, projectTabs []headerTab
	selectorStart           int
	selectorEnd             int
	restoreStart            int
	restoreEnd              int
	gearStart               int
	gearEnd                 int
	indicatorStart          int
	indicatorEnd            int
}

// headerIndicatorMaxWidth is design 1d's budget: `·`, `●3`, `?12`, `●99+`,
// `◌4` — never more than five cells.
const headerIndicatorMaxWidth = 5

// renderHeaderIndicator paints the unread indicator that sits next to the
// gear (design 1d). Its colour carries the loudest unread source, and it is
// inverted while the notification centre is open, so the control that opened
// the panel also shows that the panel is what is open.
func (m Model) renderHeaderIndicator() string {
	unread := m.UnreadNotifications()
	hue, any := notify.LoudestHue(m.notificationCache)
	text := "·"
	if any && unread > 0 {
		count := "99+"
		if unread <= 99 {
			count = strconv.Itoa(unread)
		}
		text = "●" + count
	}
	style := lipgloss.NewStyle().Foreground(notify.ResolveHue(hue))
	if m.notificationCentreOpen {
		style = lipgloss.NewStyle().Foreground(styles.BgPrimary).Background(notify.ResolveHue(hue))
	}
	return style.Render(text)
}

// headerGear is the Configuration control. It is a plain Unicode glyph rather
// than a Nerd Font one: the header's Nerd Font affordances are pill caps, and
// an icon nobody can render is worse than a small one everybody can.
const headerGear = "⚙"

// renderHeaderGear paints the gear chip. Hovered, it takes the raised chrome
// surface the rest of the bar's chips use, so the pointer has something to
// land on before it clicks.
func renderHeaderGear(hovered bool) string {
	if hovered {
		return styles.ProjectRestore.Background(styles.SurfaceRaised).Render(headerGear)
	}
	return styles.ProjectRestore.Render(headerGear)
}

// headerClock renders the optional clock, or "" when it is disabled.
func (m Model) headerClock() string {
	if !m.showClock || m.ui == nil {
		return ""
	}
	return styles.BarText.Render(m.ui.Clock.Format("15:04"))
}

// headerGeometry lays out both stable anchor zones. Global navigation is
// always part of the left cluster. The right cluster ends with the project
// selector followed by the Configuration gear, and its right edge is always the
// terminal's right edge; project tabs consume only the space between them.
func (m Model) headerGeometry() headerLayout {
	width := max(0, m.width)
	brand := styles.BrandLogo.Render(" ◱ Sidecar")
	brandPrefix := brand + " " + styles.HeaderDivider.Render("│") + " "
	layout := headerLayout{logoEnd: min(width, lipgloss.Width(brand))}

	global := m.globalTabsVisible()
	for i, tab := range global {
		ref := globalTabRef(tab.id)
		text := styles.RenderTab(tab.name, i, len(global), m.inGlobalScope() && tab.id == m.globalTab, false)
		layout.globalTabs = append(layout.globalTabs, headerTab{ref: ref, text: text})
	}
	selectorLabel := "Select Project"
	if !m.inGlobalScope() {
		if name := m.activeDestinationName(); name != "" {
			selectorLabel = name
		}
		if m.boundDestination.HostID == "" {
			if wt := m.currentWorktreeInfo(); wt != nil && !wt.IsMain {
				branch := wt.Branch
				if branch == "" {
					branch = "worktree"
				}
				selectorLabel += " [" + branch + "]"
			}
		}
	}
	renderSelector := func(label string, budget int) string {
		render := func(text string) string {
			if m.inGlobalScope() {
				// Do not route this through RenderPillWithStyle: Nerd Font caps
				// require a background and would refill the global action.
				return styles.GlobalHeaderAction.Render(text)
			}
			return styles.RenderPillWithStyle(text, styles.ProjectSelector, styles.BgSecondary)
		}
		const suffix = " ▾"
		full := render(label + suffix)
		if lipgloss.Width(full) <= budget {
			return full
		}
		overhead := lipgloss.Width(render(suffix)) - lipgloss.Width(suffix)
		labelBudget := max(0, budget-overhead-lipgloss.Width(suffix))
		fitted := render(ansi.Truncate(label, labelBudget, "…") + suffix)
		if lipgloss.Width(fitted) <= budget {
			return fitted
		}
		// Unsupported widths may not even hold the style padding and arrow.
		// Preserve the right edge and never publish bounds beyond what paints.
		return ansi.TruncateLeft(fitted, max(0, lipgloss.Width(fitted)-budget), "")
	}

	// The gear and the clock live inside the same right-cluster budget as the
	// selector. The gear is small and is the only way into Configuration with a
	// mouse, so it survives every width; the clock is the first thing dropped
	// when the header runs out of room.
	gear := renderHeaderGear(m.headerGearHovered)
	gearWidth := lipgloss.Width(gear)
	clock := m.headerClock()
	clockWidth := lipgloss.Width(clock)
	// The notification indicator sits immediately left of the gear and is
	// budgeted with it: it outlives the clock (it is the only place an unread
	// count is visible at all) and is given up before the gear, which is the
	// sole mouse route into Configuration.
	indicator := m.renderHeaderIndicator()
	indicatorWidth := min(headerIndicatorMaxWidth, lipgloss.Width(indicator))
	// trailing is what the right cluster always ends with: indicator, space,
	// gear. Every budget below reserves it.
	trailing := indicatorWidth + 1 + gearWidth

	// A fully hidden or partially clipped tab must never retain a hit region.
	// Fit whole global tabs into the space left of the pinned selector, dropping
	// inactive tabs from the right at exceptionally narrow widths.
	leftWidth := func(tabs []headerTab) int {
		result := lipgloss.Width(brandPrefix)
		for i, tab := range tabs {
			result += lipgloss.Width(tab.text)
			if i > 0 {
				result++
			}
		}
		return result
	}
	minimumSelectorWidth := lipgloss.Width(renderSelector("", width)) + trailing + 1
	for len(layout.globalTabs) > 0 && leftWidth(layout.globalTabs)+minimumSelectorWidth > width {
		remove := -1
		for i := len(layout.globalTabs) - 1; i >= 0; i-- {
			if !m.inGlobalScope() || layout.globalTabs[i].ref.global != m.globalTab {
				remove = i
				break
			}
		}
		if remove < 0 {
			layout.globalTabs = nil
			break
		}
		layout.globalTabs = append(layout.globalTabs[:remove], layout.globalTabs[remove+1:]...)
	}
	left := brandPrefix
	for i := range layout.globalTabs {
		if i > 0 {
			left += " "
		}
		layout.globalTabs[i].start = lipgloss.Width(left)
		left += layout.globalTabs[i].text
		layout.globalTabs[i].end = lipgloss.Width(left)
	}

	// The right cluster's optional pieces go before the selector's label is
	// truncated: a header that has to abbreviate the project name has no room
	// for decoration. The clock goes first, then the indicator — the indicator
	// outlives it because it is the only place an unread count is visible.
	squeezed := func() bool {
		need := lipgloss.Width(left) + lipgloss.Width(renderSelector(selectorLabel, width)) + trailing + 1
		if clockWidth > 0 {
			need += clockWidth + 1
		}
		return need > width
	}
	if clockWidth > 0 && squeezed() {
		clock, clockWidth = "", 0
	}
	if indicatorWidth > 0 && squeezed() {
		indicator, indicatorWidth = "", 0
		trailing = gearWidth
	}

	// The left anchor is protected. Fit the selector into exactly the columns
	// that remain so a long repo or worktree name cannot cover the brand/tabs or
	// push its arrow beyond the right edge.
	selectorBudget := max(0, width-lipgloss.Width(left)-trailing-1)
	selector := renderSelector(selectorLabel, selectorBudget)
	selectorWidth := lipgloss.Width(selector)

	restore := ""
	if m.inGlobalScope() {
		if name := strings.TrimSpace(m.intro.RepoName); name != "" {
			candidate := styles.ProjectRestore.Render("↖ " + name)
			fullSelector := renderSelector(selectorLabel, width)
			if lipgloss.Width(left)+lipgloss.Width(candidate)+1+trailing+1+lipgloss.Width(fullSelector) <= width {
				restore = candidate
				selector = fullSelector
				selectorWidth = lipgloss.Width(selector)
			}
		}
	}
	restoreWidth := lipgloss.Width(restore)
	suffixWidth := selectorWidth + trailing + 1
	if restoreWidth > 0 {
		suffixWidth += restoreWidth + 1
	}
	if clockWidth > 0 {
		suffixWidth += clockWidth + 1
	}

	project := []headerTab(nil)
	if !m.inGlobalScope() && m.registry != nil {
		plugins := m.registry.Plugins()
		for i := range plugins {
			ref := projectTabRef(i)
			project = append(project, headerTab{
				ref:  ref,
				text: styles.RenderTab(m.tabLabel(ref), i, len(plugins), i == m.activePlugin, false),
			})
		}
	}

	clusterWidth := func(tabs []headerTab) int {
		result := suffixWidth
		if len(tabs) > 0 {
			result += 1
		}
		for i, tab := range tabs {
			result += lipgloss.Width(tab.text)
			if i > 0 {
				result++
			}
		}
		return result
	}
	// The clock is the first casualty of a narrow header: it is the only piece
	// of the right cluster nothing depends on.
	if clockWidth > 0 && lipgloss.Width(left)+clusterWidth(project) > width {
		suffixWidth -= clockWidth + 1
		clock = ""
	}
	// Next casualty after the clock: the unread indicator. It goes before any
	// project tab is dropped only in the sense that it is cheaper — the gear
	// never goes, because it is the only pointer route into Configuration.
	if indicatorWidth > 0 && lipgloss.Width(left)+clusterWidth(project) > width {
		suffixWidth -= indicatorWidth + 1
		indicator = ""
		indicatorWidth = 0
	}
	for len(project) > 0 && lipgloss.Width(left)+clusterWidth(project) > width {
		remove := -1
		for i := len(project) - 1; i >= 0; i-- {
			if project[i].ref.plugin != m.activePlugin {
				remove = i
				break
			}
		}
		if remove < 0 {
			project = nil
			break
		}
		project = append(project[:remove], project[remove+1:]...)
	}

	right := ""
	for i := range project {
		if i > 0 {
			right += " "
		}
		project[i].start = lipgloss.Width(right)
		right += project[i].text
		project[i].end = lipgloss.Width(right)
	}
	if len(project) > 0 {
		right += " "
	}
	restoreOffset := lipgloss.Width(right)
	if restore != "" {
		right += restore
		right += " "
	}
	if clock != "" {
		right += clock
		right += " "
	}
	selectorOffset := lipgloss.Width(right)
	right += selector
	right += " "
	indicatorOffset := lipgloss.Width(right)
	if indicator != "" {
		right += indicator
		right += " "
	}
	gearOffset := lipgloss.Width(right)
	right += gear
	rightStart := max(lipgloss.Width(left), width-lipgloss.Width(right))
	for i := range project {
		project[i].start += rightStart
		project[i].end += rightStart
	}

	layout.left = left
	layout.right = right
	layout.rightStart = rightStart
	layout.projectTabs = project
	if restore != "" {
		layout.restoreStart = rightStart + restoreOffset
		layout.restoreEnd = layout.restoreStart + restoreWidth
	}
	layout.selectorStart = rightStart + selectorOffset
	layout.selectorEnd = min(width, layout.selectorStart+selectorWidth)
	if indicator != "" {
		layout.indicatorStart = rightStart + indicatorOffset
		layout.indicatorEnd = min(width, layout.indicatorStart+indicatorWidth)
	}
	layout.gearStart = rightStart + gearOffset
	layout.gearEnd = min(width, layout.gearStart+gearWidth)
	return layout
}

// getGearBounds returns the painted geometry of the Configuration gear, which
// sits immediately right of the project selector, at the terminal's right edge.
// The gear, not the selector, is what the right cluster now ends with: settings
// belong at the far corner of the bar, and the selector keeps its stable
// position by staying the same distance from that corner at every width.
func (m Model) getGearBounds() (start, end int, ok bool) {
	layout := m.headerGeometry()
	if layout.gearEnd <= layout.gearStart {
		return 0, 0, false
	}
	return layout.gearStart, layout.gearEnd, true
}

// getNotificationIndicatorBounds returns the painted geometry of the unread
// indicator, or ok=false at widths where it was dropped. Clicking it toggles
// the notification centre, which is — with its shortcut — the only way in:
// the centre has no navbar tab.
func (m Model) getNotificationIndicatorBounds() (start, end int, ok bool) {
	layout := m.headerGeometry()
	if layout.indicatorEnd <= layout.indicatorStart {
		return 0, 0, false
	}
	return layout.indicatorStart, layout.indicatorEnd, true
}

// getTabBounds calculates the X position bounds for each tab in the header.
// Used for mouse click detection on tabs.
func (m Model) getTabBounds() []TabBounds {
	layout := m.headerGeometry()
	tabs := append(append([]headerTab(nil), layout.globalTabs...), layout.projectTabs...)
	bounds := make([]TabBounds, 0, len(tabs))
	for _, tab := range tabs {
		bounds = append(bounds, TabBounds{Start: tab.start, End: tab.end, Tab: tab.ref})
	}

	return bounds
}

// getLogoBounds returns the X bounds for the "Sidecar" brand in the header.
// Clicking it toggles the global space, so it is live whenever that space has
// a tab to show — the cross-project Overview tabs, the hosted Tasks tab, or
// both.
func (m Model) getLogoBounds() (start, end int, ok bool) {
	if !m.globalScopeAvailable() {
		return 0, 0, false
	}
	// Leading space is part of the painted brand (" Sidecar").
	end = m.headerGeometry().logoEnd
	if end <= 0 {
		return 0, 0, false
	}
	if m.width > 0 {
		end = min(end, m.width)
	}
	return 0, end, true
}

// getProjectSelectorBounds returns the exact painted selector geometry in both
// scopes. It ends one gear's width short of the terminal edge.
func (m Model) getProjectSelectorBounds() (start, end int, ok bool) {
	layout := m.headerGeometry()
	if layout.selectorEnd <= layout.selectorStart {
		return 0, 0, false
	}
	return layout.selectorStart, layout.selectorEnd, true
}

// getProjectRestoreBounds returns the optional global-scope control that
// restores the covered project without reinitializing or switching it.
func (m Model) getProjectRestoreBounds() (start, end int, ok bool) {
	layout := m.headerGeometry()
	if layout.restoreEnd <= layout.restoreStart {
		return 0, 0, false
	}
	return layout.restoreStart, layout.restoreEnd, true
}

// renderContent renders the main content area.
func (m *Model) renderContent(width, height int) string {
	if m.configOpen() {
		return m.config.View(width, height)
	}
	if m.inGlobalScope() {
		return m.renderGlobalContent(width, height)
	}
	if m.firstRunProbePending {
		return m.renderFirstRunProbe(width, height)
	}
	p := m.ActivePlugin()
	if p == nil {
		msg := "No plugins loaded"
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, styles.Muted.Render(msg))
	}
	if h := m.activeContentDeck(); h != nil {
		return m.renderContentDeck(h, width, height)
	}

	content := p.View(width, height)
	if width <= 0 || height <= 0 {
		return ""
	}
	if constrained, ok := p.(plugin.SelfConstrainedView); ok && constrained.ViewIsSelfConstrained() {
		return content
	}
	// Use MaxHeight to truncate content that exceeds allocated space.
	// Height() only pads short content; MaxHeight() also truncates tall content.
	// This prevents plugin content from pushing the header off-screen.
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

// renderGlobalContent renders the visible global tab.
func (m *Model) renderGlobalContent(width, height int) string {
	if host := m.globalPluginPlugin(); host != nil {
		if h := m.activeContentDeck(); h != nil {
			return m.renderContentDeck(h, width, height)
		}
		return host.View(width, height)
	}
	if m.globalTab == GlobalSessions {
		if m.overview != nil {
			return m.overview.WorkspacesView(width, height)
		}
		return m.renderGlobalWorkspacesPlaceholder(width, height)
	}
	if m.overview != nil {
		return m.overview.View(width, height)
	}
	return ""
}

// renderGlobalWorkspacesPlaceholder is the honest empty state for a build with
// no cross-project catalog behind the tab at all. With the Overview model
// present, the tab renders the shared workspace list instead.
func (m Model) renderGlobalWorkspacesPlaceholder(width, height int) string {
	lines := []string{
		styles.Title.Render("Workspaces"),
		"",
		styles.Muted.Render("Every configured project's shells and worktrees will be browsable here."),
		styles.Muted.Render("Nothing is being collected for this tab yet."),
		"",
		styles.Muted.Render("Use the project Workspaces tab for the current project."),
	}
	// With no project configured there is nothing this tab could ever collect,
	// so the placeholder offers the one route that changes that. With projects
	// configured it stays a plain placeholder: the user is not blocked, this
	// build simply has no cross-project catalog behind the tab.
	if m.configurationBlocked() {
		lines = append(lines,
			"",
			styles.Muted.Render("No projects are configured yet."),
			"",
			styles.RenderPillWithStyle("Enter  Open Sidecar Setup", styles.ButtonHover, nil),
		)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

// configurationBlocked reports the one prerequisite the app itself can answer
// for cheaply: whether any project is configured. It reads the configuration
// already in memory and touches nothing on the render path.
func (m Model) configurationBlocked() bool {
	return m.cfg == nil || len(m.cfg.Projects.List) == 0
}

// globalWorkspacesPlaceholderVisible reports that the placeholder — not the
// cross-project browser — is what the global Sessions tab is showing.
func (m Model) globalWorkspacesPlaceholderVisible() bool {
	return m.inGlobalScope() && m.globalTab == GlobalSessions && m.overview == nil
}

// renderFooter renders the bottom bar with key hints and status.
func (m Model) renderFooter() string {
	// The footer no longer carries toasts. Every transient message is a
	// notification now (internal/notify): it floats over the content region as
	// a bordered toast and stays in the centre afterwards, so nothing that just
	// happened depends on the user watching one line at the bottom of the
	// screen. What remains here is the *standing* plugin condition — "this
	// plugin cannot read its data" — which is true until someone fixes it and
	// has no other always-on surface.
	var status string
	if text, isError := m.pluginFooterStatus(); text != "" {
		style := styles.ToastSuccess
		if isError {
			style = styles.ToastError
		}
		status = style.Render(text)
	}

	// Refresh time and update status live in Settings → Diagnostics, not here.
	statusWidth := lipgloss.Width(status)
	minSpacing := 4
	availableForHints := m.width - statusWidth - minSpacing

	hintsStr := renderHintLineTruncated(m.footerHints(), availableForHints)

	hintsWidth := lipgloss.Width(hintsStr)
	spacing := m.width - hintsWidth - statusWidth
	if spacing < 0 {
		spacing = 0
	}

	footer := hintsStr + strings.Repeat(" ", spacing) + status

	return styles.Footer.Width(m.width).MaxWidth(m.width).Render(footer)
}

// pluginFooterStatus asks the active plugin for a condition that must stay
// visible in the host footer.
func (m Model) pluginFooterStatus() (string, bool) {
	p := m.focusedSurface()
	if p == nil {
		return "", false
	}
	provider, ok := p.(plugin.FooterStatusProvider)
	if !ok {
		return "", false
	}
	return provider.FooterStatus()
}

type footerHint struct {
	keys  string
	label string
}

// typingFooterHints answers for a pane the user is typing into, on either
// surface. Such a footer advertises only the ways out: every other key is on its
// way to the pane — the tab numbers, the help key and ctrl+c included — so the
// global hints are not appended to it.
func (m Model) typingFooterHints() ([]footerHint, bool) {
	if m.inGlobalScope() {
		if m.globalTab == GlobalSessions && m.overview != nil && m.overview.PreviewInteractive() {
			return []footerHint{
				{keys: m.overview.InteractiveExitKey(), label: "Stop typing"},
				{keys: "esc esc", label: "Stop typing"},
			}, true
		}
		return nil, false
	}
	if m.activeContext == "workspace-interactive" {
		if p := m.ActivePlugin(); p != nil {
			return m.pluginFooterHints(p, m.activeContext), true
		}
	}
	return nil, false
}

func (m Model) footerHints() []footerHint {
	if hints, typing := m.typingFooterHints(); typing {
		return hints
	}
	// Surface-specific hints first - they're more contextually relevant
	var hints []footerHint
	switch {
	case m.notificationCentreOwnsKeys():
		// The panel is the focused surface, so the host footer describes it —
		// derived from its Commands plus the registered bindings, exactly like
		// a plugin's. The panel renders no footer of its own.
		hints = m.commandFooterHints(m.notificationCentreCommands(), notificationCentreContext)
	case m.configOpen():
		// Derived from the registered config bindings like every other surface,
		// so a rebound key changes the footer with it.
		hints = m.commandFooterHints(m.configCommands(), m.activeContext)
	case len((&m).appContentCommands()) > 0:
		// A passive app-owned leaf is the focused surface even when its primary
		// host is app-global Tasks. Its Close/Tab/Focus controls must outrank the
		// covered host's commands just as its keys and help context do.
		hints = m.commandFooterHints((&m).appContentCommands(), m.activeContext)
	case m.globalPluginFocused():
		hints = m.pluginFooterHints(m.globalPluginPlugin(), m.activeContext)
	case m.inGlobalScope() && m.globalTab == GlobalSessions:
		// Typing is the host's "only ways out" footer — almost every key is
		// already on its way to the pane. Every other Workspaces context,
		// including a focused document or issue leaf, is Commands + keymap.
		if m.overview != nil {
			hints = m.commandFooterHints(m.overview.Commands(), m.overview.WorkspaceFocusContext())
		}
	case m.inGlobalScope() && m.overview != nil:
		hints = append(hints,
			footerHint{keys: "hjkl", label: "Move"},
			footerHint{keys: "enter", label: "Open"},
			footerHint{keys: "r", label: "Refresh"},
			footerHint{keys: "esc", label: "Close"},
		)
	case m.focusedSurface() != nil:
		if p := m.focusedSurface(); p != nil {
			hints = m.pluginFooterHints(p, m.activeContext)
		}
	}
	// The pane switcher's entry belongs to the app, not to the plugin whose
	// browse context it appears in, so it is merged here rather than in five
	// plugins' Commands(). It sits outside the switch on purpose: a focused
	// passive leaf and the global Tasks host are both branches above, and the
	// entry works in both. paneSwitcherCommands self-gates on availability and on
	// the context's binding, so an unconditional append contributes nothing
	// where the entry does not belong.
	hints = append(hints, m.commandFooterHints((&m).paneSwitcherCommands(), m.activeContext)...)
	// Then essential global hints
	hints = append(hints, m.globalFooterHints()...)
	return hints
}

func (m Model) activeDestinationName() string {
	if m.inGlobalScope() {
		return "Overview"
	}
	if m.boundDestination.HostID != "" {
		return BoundDestinationNavbarLabel(m.boundDestination)
	}
	return m.intro.RepoName
}

func (m Model) globalFooterHints() []footerHint {
	bindings := m.keymap.BindingsForContext("global")
	keysByCmd := bindingKeysByCommand(bindings)

	// Only essential global hints - plugin shortcuts are more relevant
	specs := []struct {
		id    string
		label string
	}{
		{id: "toggle-palette", label: "help"},
		{id: "quit", label: "quit"},
	}

	var hints []footerHint

	typing := m.textInputFocused()

	// Tab switching hints (consolidated for brevity). The number row addresses
	// the whole header from either scope, so the same two hints are correct in
	// both: 1-N for the project's plugin tabs, and the 8/9/0 keys for whichever
	// global entries actually exist. A disabled Tasks tab drops `0` from the
	// hint rather than renumbering anything.
	//
	// A focused text input has taken the digits: typing "2" into a file
	// finder's query is a query, not a tab switch. A footer that advertises a
	// binding the focused surface has claimed is not a hint, it is wrong.
	if !typing && !m.configOpen() {
		if count := m.numberedProjectTabs(); count > 1 {
			hints = append(hints, footerHint{keys: fmt.Sprintf("1-%d", count), label: "plugins"})
		}
		var globalKeys []string
		for _, tab := range m.globalTabsVisible() {
			if tab.key != "" {
				globalKeys = append(globalKeys, tab.key)
			}
		}
		if len(globalKeys) > 0 {
			hints = append(hints, footerHint{keys: strings.Join(globalKeys, "/"), label: "global"})
		}
	}

	// The digits are not the only global binding a text input takes. `q` types
	// a q into the query and `?` types a question mark: precedence level 2 in
	// update.go forwards the focused surface everything except ctrl+c. So each
	// remaining global hint is advertised on the first of its keys that still
	// reaches the host — which is why quit survives as ctrl+c while help, whose
	// only key is `?`, drops out entirely rather than promising a key that
	// types.
	for _, spec := range specs {
		// Configuration owns the keyboard the way a text input does — `q` types
		// nothing there, but it does not quit either — so quit advertises the
		// one key that still reaches the host. Help is unaffected: `?` opens
		// the palette from Configuration.
		reachable := typing || (m.configOpen() && spec.id == "quit")
		key, ok := firstReachableKey(keysByCmd[spec.id], reachable)
		if !ok {
			continue
		}
		hints = append(hints, footerHint{keys: key, label: spec.label})
	}
	return hints
}

// firstReachableKey picks the key a global hint should advertise: the first one
// bound, or — while a text input has the keyboard — the first one the input has
// not taken.
func firstReachableKey(keys []string, typing bool) (string, bool) {
	for _, key := range keys {
		if typing && !survivesTextInput(key) {
			continue
		}
		return key, true
	}
	return "", false
}

// survivesTextInput reports whether the host still acts on key while a focused
// surface is consuming text input. Update's level 2 hands the surface every key
// but one, so this is the one.
func survivesTextInput(key string) bool {
	return key == "ctrl+c"
}

func (m Model) pluginFooterHints(p plugin.Plugin, context string) []footerHint {
	if p == nil {
		return nil
	}
	return m.commandFooterHints(p.Commands(), context)
}

func (m Model) commandFooterHints(commands []plugin.Command, context string) []footerHint {
	if context == "" || context == "global" {
		return nil
	}

	keysByCmd := bindingKeysByCommand(m.keymap.BindingsForContext(context))

	// Collect commands with their priorities
	type cmdWithPriority struct {
		cmd      plugin.Command
		keys     []string
		priority int
	}

	var cmds []cmdWithPriority
	for _, cmd := range commands {
		if cmd.Context != context {
			continue
		}
		keys := keysByCmd[cmd.ID]
		if len(keys) == 0 {
			continue
		}
		priority := cmd.Priority
		if priority == 0 {
			priority = 99 // Default to low priority
		}
		cmds = append(cmds, cmdWithPriority{cmd, keys, priority})
	}

	// Sort by priority (lower = more important, shown first). Stable so commands
	// sharing a priority keep their Commands() declaration order.
	sort.SliceStable(cmds, func(i, j int) bool {
		return cmds[i].priority < cmds[j].priority
	})

	var hints []footerHint
	for _, c := range cmds {
		hints = append(hints, footerHint{
			keys:  formatBindingKeys(c.keys),
			label: c.cmd.Name,
		})
	}
	return hints
}

func bindingKeysByCommand(bindings []keymap.Binding) map[string][]string {
	keysByCmd := make(map[string][]string, len(bindings))
	for _, b := range bindings {
		keysByCmd[b.Command] = append(keysByCmd[b.Command], b.Key)
	}
	return keysByCmd
}

// renderHintLineTruncated renders hints but stops adding when maxWidth is exceeded.
func renderHintLineTruncated(hints []footerHint, maxWidth int) string {
	if len(hints) == 0 || maxWidth <= 0 {
		return ""
	}
	var result string
	separator := "  "
	for i, hint := range hints {
		if hint.keys == "" || hint.label == "" {
			continue
		}
		part := fmt.Sprintf("%s %s", styles.KeyHint.Render(hint.keys), hint.label)
		var candidate string
		if i == 0 {
			candidate = part
		} else {
			candidate = result + separator + part
		}
		if lipgloss.Width(candidate) > maxWidth {
			break // Stop adding hints if we exceed available width
		}
		result = candidate
	}
	return result
}

// ensureHelpModal builds/rebuilds the help modal.
func (m *Model) ensureHelpModal() {
	modalW := 60
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	// Only rebuild if modal doesn't exist or width changed
	if m.helpModal != nil && m.helpModalWidth == modalW {
		return
	}
	m.helpModalWidth = modalW

	m.helpModal = modal.New("Keyboard Shortcuts",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(m.helpGlobalSection()).
		AddSection(m.helpPluginSection()).
		AddSection(m.helpSelectionSection())
}

// clearHelpModal clears the help modal state.
func (m *Model) clearHelpModal() {
	m.helpModal = nil
	m.helpModalWidth = 0
	m.helpMouseHandler = nil
}

// helpGlobalSection renders the global bindings section.
func (m *Model) helpGlobalSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var b strings.Builder
		b.WriteString(styles.Title.Render("Global"))
		b.WriteString("\n")
		m.renderBindingSection(&b, "global", contentWidth)
		return modal.RenderedSection{Content: b.String()}
	}, nil)
}

// helpPluginSection renders the bindings of the surface that owns the screen:
// the visible global tab in global scope, the active plugin in project scope.
// Help must describe what the keys do here, not what they would do in the space
// the user is not in.
func (m *Model) helpPluginSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		title, ctx := m.helpSurface()
		if title == "" || ctx == "global" || ctx == "" {
			return modal.RenderedSection{}
		}
		bindings := m.keymap.BindingsForContext(ctx)
		if len(bindings) == 0 {
			return modal.RenderedSection{}
		}
		var b strings.Builder
		b.WriteString(styles.Title.Render(title))
		b.WriteString("\n")
		m.renderBindingSection(&b, ctx, contentWidth)
		return modal.RenderedSection{Content: b.String()}
	}, nil)
}

// helpSelectionSection is the one paragraph help owes text selection. The
// chords are already in the binding lists above; what no list can say is that
// holding shift or option hands the drag to the terminal emulator instead,
// which is the answer to "why can I not select that" on any pane Sidecar does
// not make selectable — and the reason the emulator's own quick-copy still
// works everywhere.
func (m *Model) helpSelectionSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var b strings.Builder
		b.WriteString(styles.Title.Render("Selecting text"))
		b.WriteString("\n")
		for _, line := range []string{
			"Drag over a pane's text to select it; " + m.selectionCopyKeyLabel() + " copies.",
			"Hold shift or option while dragging to hand the drag to your",
			"terminal instead, for its own selection and quick copy.",
		} {
			b.WriteString(styles.Muted.Render(ui.TruncateString(line, contentWidth)))
			b.WriteString("\n")
		}
		return modal.RenderedSection{Content: strings.TrimRight(b.String(), "\n")}
	}, nil)
}

// selectionCopyKeyLabel is the configured copy chord, so help names the key the
// user actually has rather than the default.
func (m *Model) selectionCopyKeyLabel() string {
	if key := TerminalConfig(m.cfg).SelectionKeys().Copy; key != "" {
		return key
	}
	return tty.SuperCopyKey
}

// helpSurface names the surface help should document and the keymap context it
// reads its bindings from.
func (m *Model) helpSurface() (title, context string) {
	if m.notificationCentreOwnsKeys() {
		return "Notifications", notificationCentreContext
	}
	if m.inGlobalScope() {
		if host := m.globalPluginPlugin(); host != nil {
			if _, ok := m.appContentContext(); ok {
				return host.Name() + " content", m.activeContext
			}
			return host.Name(), host.FocusContext()
		}
		tab, _ := m.activeGlobalSurface()
		if m.globalWorkspacesVisible() {
			return tab.name, m.overview.WorkspaceFocusContext()
		}
		return tab.name, tab.context
	}
	if p := m.ActivePlugin(); p != nil {
		if _, ok := m.appContentContext(); ok {
			return p.Name() + " content", m.activeContext
		}
		return p.Name(), p.FocusContext()
	}
	return "", ""
}

// renderHelpModal renders the help modal.
func (m *Model) renderHelpModal(content string) string {
	m.ensureHelpModal()
	if m.helpModal == nil {
		return content
	}

	if m.helpMouseHandler == nil {
		m.helpMouseHandler = mouse.NewHandler()
	}
	modalContent := m.helpModal.Render(m.width, m.height, m.helpMouseHandler)
	return ui.OverlayModal(content, modalContent, m.width, m.height)
}

const (
	bindingHelpIndent = 2
	bindingHelpGap    = 1
	bindingHelpKeyMin = 11
	bindingHelpCmdMin = 8
)

type bindingHelpRow struct {
	keys string
	name string
}

// renderBindingSection renders bindings for a context, aligned to contentWidth.
func (m Model) renderBindingSection(b *strings.Builder, context string, contentWidth int) {
	bindings := m.keymap.BindingsForContext(context)

	seen := make(map[string]bool)
	var rows []bindingHelpRow
	for _, binding := range bindings {
		if seen[binding.Command] {
			continue
		}
		seen[binding.Command] = true

		var keys []string
		for _, b2 := range bindings {
			if b2.Command != binding.Command {
				continue
			}
			// A global key the focused context binds for itself does something
			// else there. The command keeps its line, because the palette can
			// still run it, but help must not promise the key.
			if context == "global" && m.contextShadowsGlobalKey(b2.Key) {
				continue
			}
			keys = append(keys, b2.Key)
		}

		rows = append(rows, bindingHelpRow{
			keys: formatBindingKeys(keys),
			name: formatCommandName(binding.Command),
		})
	}
	writeBindingRows(b, rows, contentWidth)
}

// writeBindingRows paints key/command rows with an ANSI-aware key column so
// styled keys cannot steal padding and wrap into the next command.
func writeBindingRows(b *strings.Builder, rows []bindingHelpRow, contentWidth int) {
	if len(rows) == 0 {
		return
	}

	keyCol := bindingHelpKeyMin
	for _, row := range rows {
		if w := lipgloss.Width(row.keys); w > keyCol {
			keyCol = w
		}
	}

	unlimited := contentWidth <= 0
	if !unlimited {
		maxKey := contentWidth - bindingHelpIndent - bindingHelpGap - bindingHelpCmdMin
		if maxKey < 1 {
			maxKey = 1
		}
		if keyCol > maxKey {
			keyCol = maxKey
		}
	}

	cmdWidth := 0
	if !unlimited {
		cmdWidth = contentWidth - bindingHelpIndent - bindingHelpGap - keyCol
		if cmdWidth < 1 {
			cmdWidth = 1
		}
	}

	indent := strings.Repeat(" ", bindingHelpIndent)
	cont := strings.Repeat(" ", bindingHelpIndent+keyCol+bindingHelpGap)

	for _, row := range rows {
		keys := row.keys
		if !unlimited && lipgloss.Width(keys) > keyCol {
			keys = ansi.Truncate(keys, keyCol, "…")
		}
		keyCell := padVisual(styles.Muted.Render(keys), keyCol)

		nameLines := []string{row.name}
		if !unlimited {
			nameLines = wrapPlain(row.name, cmdWidth)
		}
		if len(nameLines) == 0 {
			nameLines = []string{""}
		}

		fmt.Fprintf(b, "%s%s%s%s\n", indent, keyCell, strings.Repeat(" ", bindingHelpGap), nameLines[0])
		for _, extra := range nameLines[1:] {
			fmt.Fprintf(b, "%s%s\n", cont, extra)
		}
	}
}

func padVisual(s string, width int) string {
	n := width - lipgloss.Width(s)
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}

func wrapPlain(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var cur string
	flush := func() {
		if cur == "" {
			return
		}
		lines = append(lines, cur)
		cur = ""
	}
	for _, word := range words {
		for lipgloss.Width(word) > width {
			flush()
			cut := 1
			for cut < len(word) && lipgloss.Width(word[:cut+1]) <= width {
				cut++
			}
			lines = append(lines, word[:cut])
			word = word[cut:]
		}
		if word == "" {
			continue
		}
		if cur == "" {
			cur = word
			continue
		}
		if lipgloss.Width(cur+" "+word) <= width {
			cur += " " + word
			continue
		}
		flush()
		cur = word
	}
	flush()
	return lines
}

// contextShadowsGlobalKey reports that the focused context binds this key
// itself, which is the same lookup handleKeyMsg makes before answering a global
// key.
func (m Model) contextShadowsGlobalKey(key string) bool {
	if m.activeContext == "" || m.activeContext == "global" {
		return false
	}
	_, bound := m.keymap.CommandForContextKey(m.activeContext, key)
	return bound
}

// formatBindingKeys formats multiple keys into a display string.
func formatBindingKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return keys[0]
	}
	// Show up to 2 keys
	if len(keys) > 2 {
		keys = keys[:2]
	}
	return strings.Join(keys, ", ")
}

// formatCommandName converts a command ID to a display name.
func formatCommandName(cmd string) string {
	// Convert kebab-case to readable format
	name := strings.ReplaceAll(cmd, "-", " ")
	return name
}

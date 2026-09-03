package modal

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/scroll"
)

// Modal represents a declarative modal dialog with automatic hit region management.
type Modal struct {
	title           string
	variant         Variant
	width           int
	sections        []Section
	showHints       bool
	hintText        string
	primaryAction   string
	closeOnBackdrop bool
	customFooter    string // Fixed footer rendered outside scroll viewport
	marginX         int    // Cells of surface kept clear either side of the box
	marginY         int    // Rows of surface kept clear above and below the box

	// State (managed internally)
	focusIdx       int      // Current focused element index in focusIDs
	pendingFocusID string   // Pending focus target before focusIDs is resolved
	hoverID        string   // Currently hovered element ID
	focusIDs       []string // Ordered list of focusable IDs (built during Render)
	scrollOffset   int      // Content scroll position in lines

	// Focus-scroll tracking (cached during buildLayout)
	focusPositions map[string]focusablePos // Absolute Y positions of focusable elements
	lastViewportH  int                     // Viewport height from last render

	// Render-derived scroll bounds, cached during buildLayout. layoutValid is
	// false until the first render and whenever the cached geometry can no
	// longer be trusted (see Invalidate).
	lastMaxScroll int
	layoutValid   bool

	// Interactive scrollbar state (see scrollbar.go). bars is rebuilt by every
	// buildLayout; press survives across renders so a viewport-bar drag keeps
	// its press-time mapping even when the layout shifts underneath it.
	bars     []placedBar // viewport bar (section -1) first, then section-declared
	barHover bool        // pointer over the framework's own viewport bar
	press    *barGesture // live viewport-bar gesture; nil when none
}

// focusablePos records the absolute position of a focusable element within the full content.
type focusablePos struct {
	y      int // Line offset from top of full content
	height int // Height in lines
}

// New creates a new Modal with the given title and options.
func New(title string, opts ...Option) *Modal {
	m := &Modal{
		title:           title,
		variant:         VariantDefault,
		width:           DefaultWidth,
		showHints:       true,
		closeOnBackdrop: true,
		marginX:         DefaultMarginX,
		marginY:         DefaultMarginY,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// AddSection adds a section to the modal. Returns the modal for chaining.
func (m *Modal) AddSection(s Section) *Modal {
	m.sections = append(m.sections, s)
	m.Invalidate()
	return m
}

// Invalidate marks the cached layout bounds as untrustworthy. Call it whenever
// the modal's content changes outside a render (for example when an async load
// replaces the text a Custom section draws). Until the next Render, boundary
// queries answer "unknown" rather than guessing.
func (m *Modal) Invalidate() {
	m.layoutValid = false
	m.lastMaxScroll = 0
}

// Render renders the modal and registers hit regions.
// Returns the styled modal content string.
func (m *Modal) Render(screenW, screenH int, handler *mouse.Handler) string {
	return m.buildLayout(screenW, screenH, handler)
}

// HandleKey processes keyboard input.
// Returns:
//   - action: the action ID if triggered ("cancel" for Esc, button/input ID for Enter, etc.)
//   - cmd: any tea.Cmd from bubbles models (cursor blink, etc.)
func (m *Modal) HandleKey(msg tea.KeyPressMsg) (action string, cmd tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		// Offer Esc to the focused section first so an open overlay can
		// consume it. Only a dismiss-overlay action suppresses cancel.
		action, cmd = m.routeToFocusedSection(msg)
		if action == actionDismissOverlay {
			return "", cmd
		}
		return "cancel", nil

	case "tab":
		m.routeToFocusedSection(overlayCommitMsg{})
		m.cycleFocus(1)
		return "", nil

	case "shift+tab":
		m.routeToFocusedSection(overlayCommitMsg{})
		m.cycleFocus(-1)
		return "", nil

	case "enter":
		// Enter on a focused element triggers that element's action
		focusID := m.currentFocusID()
		if focusID != "" {
			// Route to focused section first
			action, cmd = m.routeToFocusedSection(msg)
			return m.resolveEnterAction(focusID, action), cmd
		}
		// No focus list yet — the modal was configured but has not rendered
		// (or renders nothing focusable). An implicit submit still honours
		// the modal's declared primary action rather than swallowing Enter;
		// with no primary action there is nothing to submit.
		return m.primaryAction, nil

	default:
		// Route other keys to the focused section
		action, cmd = m.routeToFocusedSection(msg)
		return m.normalizeAction(action), cmd
	}
}

// HandleMouse processes mouse input.
// Returns the action ID if a clickable element was clicked, empty string otherwise.
func (m *Modal) HandleMouse(msg tea.MouseMsg, handler *mouse.Handler) string {
	action := handler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick, mouse.ActionDoubleClick, mouse.ActionTripleClick:
		if action.Region == nil {
			return ""
		}
		id := action.Region.ID

		// A scrollbar press starts (or restarts) a viewport-bar gesture; a
		// section-declared bar's press is absorbed here — its owner claims
		// those events through SectionBarAt before they ever arrive, and in
		// any case a press on the bar must never select the row beneath it.
		if id == RegionScrollbarThumb || id == RegionScrollbarTrack {
			if idx, ok := m.barIndexAt(action.Region); ok && m.bars[idx].section < 0 {
				m.handleViewportBarPress(action, handler)
			}
			return ""
		}

		// Backdrop click optionally dismisses the modal.
		if id == BackdropRegionID {
			if m.closeOnBackdrop {
				return "cancel"
			}
			return ""
		}

		// Body clicks absorb but don't trigger actions.
		if id == "modal-body" {
			return ""
		}

		// Click on a tab-order focusable — focus it and return its ID.
		for i, fid := range m.focusIDs {
			if fid == id {
				if id != m.currentFocusID() {
					m.notifyAll(overlayBlurMsg{})
				}
				m.focusIdx = i
				return id
			}
		}

		// A mouse-only row inside a selector belongs to that selector's Tab
		// stop. Focus it before the click is applied, so the arrows that
		// follow steer the control the pointer just used, and so a form that
		// rebuilds itself around the new choice remembers the right focus.
		m.focusSectionForClick(id)

		// Overlay rows are not in the tab order; a click commits without submit.
		action, _ := m.routeToFocusedSection(overlayClickMsg{id: id})
		if action == actionOverlayIdle {
			return ""
		}
		if action != "" {
			return action
		}

		// Mouse-only section hits (single-focus list rows) are in the hit map
		// and focusPositions, but not in focusIDs.
		if _, ok := m.focusPositions[id]; ok {
			return id
		}
		return ""

	case mouse.ActionDrag:
		m.handleViewportBarMotion(action.Y)
		return ""

	case mouse.ActionDragEnd:
		// The handler has already closed the drag; settle the gesture.
		m.endViewportBarGesture()
		return ""

	case mouse.ActionHover:
		// The handler cancels a drag on the first button-less motion (a lost
		// release). That hover is the gesture's only survivor here, so the bar
		// settles with it instead of holding a dead anchor.
		if m.press != nil && !handler.IsDragging() {
			m.endViewportBarGesture()
		}
		barHovered := m.isViewportBarRegion(action.Region)
		m.barHover = barHovered
		if action.Region != nil && !barHovered &&
			action.Region.ID != BackdropRegionID && action.Region.ID != "modal-body" {
			m.hoverID = action.Region.ID
		} else {
			m.hoverID = ""
		}
		return ""

	case mouse.ActionScrollUp:
		// The viewport bar's column scrolls the body, exactly as it did before
		// the bar owned regions; a section-declared bar absorbs instead.
		if action.Region != nil &&
			(action.Region.ID == "modal-body" || m.isViewportBarRegion(action.Region)) {
			m.ScrollBy(-wheelLines)
		}
		return ""

	case mouse.ActionScrollDown:
		if action.Region != nil &&
			(action.Region.ID == "modal-body" || m.isViewportBarRegion(action.Region)) {
			m.ScrollBy(wheelLines)
		}
		return ""
	}

	return ""
}

// wheelLines is how far one wheel notch moves the modal body.
const wheelLines = 3

// bounds returns the modal body's scroll bounds from the last render.
func (m *Modal) bounds() scroll.Bounds {
	return scroll.Bounds{Position: m.scrollOffset, Maximum: m.lastMaxScroll}
}

// ScrollBy adjusts the scroll offset by delta lines (positive = down, negative = up).
// Movement is clamped through the last render's bounds; before the first
// trustworthy render the offset is clamped again in buildLayout.
func (m *Modal) ScrollBy(delta int) {
	if m.layoutValid {
		m.scrollOffset, _ = m.bounds().Move(delta)
		return
	}
	m.scrollOffset = max(0, m.scrollOffset+delta)
}

// CanScroll reports whether the body viewport can still move delta lines in
// the given direction — what an inner scroller's owner needs to decide when
// to hand leftover scroll keys to the body.
func (m *Modal) CanScroll(delta int) bool {
	if !m.layoutValid {
		return false
	}
	_, moved := m.bounds().Move(delta)
	return moved
}

// ScrollToTop scrolls to the top of the content.
func (m *Modal) ScrollToTop() { m.scrollOffset = 0 }

// ScrollToBottom scrolls to the bottom of the content.
// The offset is clamped to the actual max in buildLayout.
func (m *Modal) ScrollToBottom() {
	if m.layoutValid {
		m.scrollOffset = m.lastMaxScroll
		return
	}
	m.scrollOffset = 999999
}

// WheelAtBoundary reports whether this wheel event is certainly a no-op for the
// modal, using only the geometry and scroll bounds produced by the most recent
// Render. It never rebuilds or mutates visible content.
//
// True means "certain no-op": the event can be dropped before Update and View.
// False means the surface can move, or that the answer is unknown - callers
// must forward the event. The query answers unknown before the first render and
// after Invalidate, and answers true for wheel over the backdrop or over
// non-scrollable modal chrome, because an open modal absorbs those events
// without changing state. When the pointer is over a section that owns its own
// scroll state (see ScrollOwnerSection), that section answers instead.
func (m *Modal) WheelAtBoundary(msg tea.MouseWheelMsg, h *mouse.Handler) bool {
	if h == nil || !m.layoutValid {
		return false
	}

	mm := msg.Mouse()
	var delta int
	switch mm.Button {
	case tea.MouseWheelUp:
		delta = -wheelLines
	case tea.MouseWheelDown:
		delta = wheelLines
	default:
		// Horizontal wheel is outside this vertical contract.
		return false
	}
	if mm.Mod.Contains(tea.ModShift) {
		// Shift+wheel is a horizontal gesture; stay out of it.
		return false
	}

	region := h.HitMap.Test(mm.X, mm.Y)
	if region == nil {
		// No hit region under the pointer: stale or unbuilt geometry.
		return false
	}

	// A section that owns its own scroll state answers for its own region.
	for _, s := range m.sections {
		owner, ok := asScrollOwner(s)
		if ok && owner.OwnsScrollRegion(region.ID) {
			return owner.ScrollAtBoundary(delta)
		}
	}

	if region.ID == "modal-body" || m.isViewportBarRegion(region) {
		return m.bounds().AtBoundary(delta)
	}

	// Backdrop and non-scrollable chrome (buttons, inputs, overlays): the modal
	// absorbs the wheel and nothing moves.
	return true
}

// SetFocus sets focus to a specific element by ID.
func (m *Modal) SetFocus(id string) {
	if id == "" {
		m.pendingFocusID = ""
		return
	}
	m.pendingFocusID = id
	for i, fid := range m.focusIDs {
		if fid == id {
			m.focusIdx = i
			m.pendingFocusID = ""
			m.scrollToFocused()
			return
		}
	}
}

// FocusedID returns the currently focused element ID.
func (m *Modal) FocusedID() string {
	return m.currentFocusID()
}

// HoveredID returns the currently hovered element ID.
func (m *Modal) HoveredID() string {
	return m.hoverID
}

// Reset resets the modal state (focus, hover, scroll).
func (m *Modal) Reset() {
	m.focusIdx = 0
	m.pendingFocusID = ""
	m.hoverID = ""
	m.scrollOffset = 0
	// A modal reused across opens must not inherit a dead gesture: the drag
	// that was live when it closed settles here rather than leaking into the
	// next open (the td-f63097 boundary, from the modal's side).
	m.press = nil
	m.barHover = false
	m.bars = nil
}

// currentFocusID returns the ID of the currently focused element.
func (m *Modal) currentFocusID() string {
	if len(m.focusIDs) == 0 {
		return m.pendingFocusID
	}
	if m.focusIdx < 0 || m.focusIdx >= len(m.focusIDs) {
		return m.focusIDs[0]
	}
	return m.focusIDs[m.focusIdx]
}

// cycleFocus moves focus by delta (1 for next, -1 for previous).
func (m *Modal) cycleFocus(delta int) {
	if len(m.focusIDs) == 0 {
		return
	}
	m.pendingFocusID = ""
	m.focusIdx = (m.focusIdx + delta + len(m.focusIDs)) % len(m.focusIDs)
	m.scrollToFocused()
}

// scrollToFocused adjusts scrollOffset so the focused element is visible in the viewport.
func (m *Modal) scrollToFocused() {
	id := m.currentFocusID()
	if id == "" || m.focusPositions == nil || m.lastViewportH <= 0 {
		return
	}
	pos, ok := m.focusPositions[id]
	if !ok {
		return
	}
	// If focused element is above the viewport, scroll up to it
	if pos.y < m.scrollOffset {
		m.scrollOffset = pos.y
	}
	// If focused element extends below the viewport, scroll down
	if pos.y+pos.height > m.scrollOffset+m.lastViewportH {
		m.scrollOffset = pos.y + pos.height - m.lastViewportH
	}
	if m.layoutValid {
		m.scrollOffset = clamp(m.scrollOffset, 0, m.lastMaxScroll)
	}
}

// routeToFocusedSection routes a message to the focused section.
func (m *Modal) routeToFocusedSection(msg tea.Msg) (string, tea.Cmd) {
	focusID := m.currentFocusID()
	if focusID == "" {
		return "", nil
	}

	// Find which section contains this focus ID and route to it
	for _, section := range m.sections {
		action, cmd := section.Update(msg, focusID)
		if action != "" || cmd != nil {
			return action, cmd
		}
	}
	return "", nil
}

// focusClaimer is implemented by a section whose mouse-only rows belong to a
// Tab stop of its own, so a click on a row can move focus to the control that
// owns it.
type focusClaimer interface {
	// FocusIDForClick names the Tab stop a click on id belongs to, or the
	// empty string when the section does not own id.
	FocusIDForClick(id string) string
}

// focusSectionForClick moves focus to the control owning a clicked mouse-only
// row, if any section claims it.
func (m *Modal) focusSectionForClick(id string) {
	if id == "" {
		return
	}
	for _, section := range m.sections {
		claimer, ok := asFocusClaimer(section)
		if !ok {
			continue
		}
		target := claimer.FocusIDForClick(id)
		if target == "" {
			continue
		}
		for i, fid := range m.focusIDs {
			if fid != target {
				continue
			}
			if target != m.currentFocusID() {
				m.notifyAll(overlayBlurMsg{})
			}
			m.focusIdx = i
			return
		}
		return
	}
}

// asFocusClaimer resolves a section to its focus claimer, seeing through When
// wrappers whose condition currently holds.
func asFocusClaimer(s Section) (focusClaimer, bool) {
	for {
		if w, ok := s.(*whenSection); ok {
			if !w.condition() {
				return nil, false
			}
			s = w.inner
			continue
		}
		claimer, ok := s.(focusClaimer)
		return claimer, ok
	}
}

func (m *Modal) notifyAll(msg tea.Msg) {
	focusID := m.currentFocusID()
	for _, section := range m.sections {
		section.Update(msg, focusID)
	}
}

func (m *Modal) resolveEnterAction(focusID, action string) string {
	switch action {
	case actionOverlayIdle, actionDismissOverlay:
		return ""
	case actionSubmitPrimary:
		if m.primaryAction != "" {
			return m.primaryAction
		}
		return focusID
	case "":
		if m.primaryAction != "" {
			return m.primaryAction
		}
		return focusID
	default:
		return action
	}
}

func (m *Modal) normalizeAction(action string) string {
	switch action {
	case actionOverlayIdle, actionDismissOverlay:
		return ""
	case actionSubmitPrimary:
		if m.primaryAction != "" {
			return m.primaryAction
		}
		return m.currentFocusID()
	default:
		return action
	}
}

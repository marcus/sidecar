package pluginbrowser

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacelist"
)

// Overlay IDs. They are constants because a click resolves to one of these
// rather than to a position, and the sections are rebuilt on every describe.
const (
	viewSortListID  = "view-sort"
	viewViewsListID = "view-views"
	viewDoneID      = "done"
	actionListID    = "action-list"
	actionRunID     = "run"
	actionCancelID  = "cancel"
	coverageDoneID  = "coverage-done"
	// coverageModalCols is the coverage modal's narrowest useful width and
	// coverageModalMaxCols its widest. It is wider than the browser's other
	// modals because it carries a four-column table, and it grows with the
	// frame up to the cap so a plugin's own reason is read rather than clipped.
	coverageModalCols    = 72
	coverageModalMaxCols = 100
	coverageRetryID      = "coverage-retry"
	filterChoicePfx      = "filter:"
	filterTextPfx        = "filtertext:"
	formInputPrefix      = "input:"
	overlayModalCols     = 46
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayView
	overlayActions
	overlayForm
	overlayConfirm
	overlayCoverage
)

// formInput is one control of an action's form. Exactly one of the four value
// fields is live, decided by the declared kind.
type formInput struct {
	decl pluginhost.ActionInput

	text   textinput.Model
	area   textarea.Model
	choice int
	flag   bool
}

// filterControl is one declared filter's control inside the View modal.
// Exactly one of the two value fields is live, decided by the declared kind.
type filterControl struct {
	decl pluginhost.Filter
	// choice indexes decl.Choices for a choice filter.
	choice int
	// text is the input of a text filter.
	text textinput.Model
}

// overlay is whatever modal the browser has open. There is at most one: the
// browser never stacks, because a stack is a second focus model.
type overlay struct {
	kind  overlayKind
	box   *modal.Modal
	mouse *mouse.Handler
	width int

	// View modal.
	sortIdx int
	viewIdx int
	// filters is one control per declared filter, in declared order.
	filters []filterControl

	// Action menu and form.
	actions   []pluginhost.Action
	actionIdx int
	action    pluginhost.Action
	inputs    []formInput
	// err is the validation line a required input that is empty leaves behind.
	err string
}

func (o overlay) open() bool { return o.kind != overlayNone }

func (m *Model) closeOverlay() {
	m.overlay = overlay{}
}

// setOverlay opens one. Every overlay goes through it so a selection in the
// detail box is dropped as the modal takes the keyboard: the highlight would
// otherwise sit under a card nothing on screen still acts on, and the gesture
// behind it would answer the next unrelated drag as an extension of a
// selection the user finished before the modal opened.
func (m *Model) setOverlay(o overlay) {
	m.ClearSelection()
	m.overlay = o
}

// overlayView composites whatever is open over the browser's own content.
func (m *Model) overlayView(background string) string {
	if !m.overlay.open() || m.overlay.box == nil {
		return background
	}
	if m.overlay.mouse == nil {
		m.overlay.mouse = mouse.NewHandler()
	}
	rendered := m.overlay.box.Render(m.width, m.height, m.overlay.mouse)
	return ui.OverlayModal(background, rendered, m.width, m.height)
}

// overlayKey routes a key into the open modal and applies whatever it reports.
func (m *Model) overlayKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.overlay.box == nil {
		m.closeOverlay()
		return nil, true
	}
	// Ctrl+Enter submits a form from inside a multiline control, where Enter is
	// a newline the user meant to type.
	if m.overlay.kind == overlayForm && msg.String() == "ctrl+enter" {
		return m.submitAction(), true
	}
	// Retry has a key as well as a button, because nothing this modal does may
	// be reachable only by pointer — and `r` is what refresh is everywhere else
	// on this surface.
	if m.overlay.kind == overlayCoverage && msg.String() == "r" {
		m.closeOverlay()
		return m.refreshActive(), true
	}
	action, cmd := m.overlay.box.HandleKey(msg)
	return tea.Batch(cmd, m.applyOverlayAction(action)), true
}

func (m *Model) applyOverlayAction(action string) tea.Cmd {
	switch m.overlay.kind {
	case overlayView:
		return m.applyViewAction(action)
	case overlayActions:
		return m.applyMenuAction(action)
	case overlayForm, overlayConfirm:
		return m.applyFormAction(action)
	case overlayCoverage:
		switch action {
		case "":
			return nil
		case coverageRetryID:
			m.closeOverlay()
			return m.refreshActive()
		default:
			m.closeOverlay()
			return nil
		}
	}
	return nil
}

// modalWidth is the modal's own width, held inside the box it floats over.
func (m *Model) modalWidth() int {
	w := overlayModalCols
	if m.width > 0 && w > m.width-4 {
		w = m.width - 4
	}
	if w < 20 {
		w = 20
	}
	return w
}

// primeOverlay renders once so focus IDs exist before the next key, then puts
// focus where the modal's first control is. Without it the first key after the
// modal opens is dropped, because View has not run yet.
func (m *Model) primeOverlay(focus string) {
	if m.overlay.box == nil {
		return
	}
	if m.overlay.mouse == nil {
		m.overlay.mouse = mouse.NewHandler()
	}
	w, h := m.width, m.height
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 24
	}
	_ = m.overlay.box.Render(w, h, m.overlay.mouse)
	m.overlay.box.Reset()
	if focus != "" {
		m.overlay.box.SetFocus(focus)
	}
}

// openViewModal builds the View modal: the current sort named, the declared
// sort keys with the live one selected, the declared views with the live one
// selected, and Done. It is the same construction internal/overview's View
// flyout uses, so the two surfaces cannot describe the same control differently.
func (m *Model) openViewModal() tea.Cmd {
	c, ok := m.ActiveCollection()
	if !ok || !m.hasViewControl(c) {
		return nil
	}
	s := m.state(c)
	m.setOverlay(overlay{kind: overlayView, width: m.modalWidth()})
	m.overlay.sortIdx = indexOfSort(c, s.sortKey)
	m.overlay.viewIdx = indexOfView(c, s.view)

	sortItems := make([]modal.SelectItem, 0, len(c.Sort))
	for _, key := range c.Sort {
		sortItems = append(sortItems, modal.SelectItem{ID: "sort:" + key.ID, Label: key.Label})
	}
	viewItems := make([]modal.SelectItem, 0, len(c.Views)+1)
	viewItems = append(viewItems, modal.SelectItem{ID: "view:", Label: "all"})
	for _, v := range c.Views {
		viewItems = append(viewItems, modal.SelectItem{ID: "view:" + v.ID, Label: v.Title})
	}

	box := modal.New("View · "+c.Title, modal.WithWidth(m.overlay.width), modal.WithHints(false)).
		AddSection(modal.Custom(func(int, string, string) modal.RenderedSection {
			return modal.RenderedSection{Content: "Current sort: " + sortLabel(c, s.sortKey)}
		}, nil))
	if len(sortItems) > 0 {
		box = box.
			AddSection(modal.Spacer()).
			AddSection(modal.Select(viewSortListID, sortItems, &m.overlay.sortIdx,
				modal.WithMaxVisible(min(len(sortItems), maxFilterChoicesVisible))))
	}
	if len(c.Views) > 0 {
		box = box.
			AddSection(modal.Spacer()).
			AddSection(modal.Custom(func(int, string, string) modal.RenderedSection {
				return modal.RenderedSection{Content: "View"}
			}, nil)).
			AddSection(modal.Select(viewViewsListID, viewItems, &m.overlay.viewIdx,
				modal.WithMaxVisible(min(len(viewItems), maxFilterChoicesVisible))))
	}
	// The filters block, after the sort list and before Done, one control per
	// declared filter in declared order — which is the order that matters,
	// because the first is the collection's scope.
	m.overlay.filters = make([]filterControl, 0, len(c.Filters))
	for _, decl := range c.Filters {
		control := filterControl{decl: decl}
		value := decl.Value(s.filters)
		switch decl.Kind {
		case pluginhost.FilterText:
			ti := textinput.New()
			ti.Prompt = ""
			ti.SetValue(value)
			control.text = ti
		default:
			control.choice = indexOfFilterChoice(decl, value)
		}
		m.overlay.filters = append(m.overlay.filters, control)
	}
	for i := range m.overlay.filters {
		control := &m.overlay.filters[i]
		label := control.decl.Label
		if i == 0 {
			// The scope is what the pill always carries, so the modal says
			// which control that is rather than leaving it to be inferred from
			// position.
			label += "  (scope)"
		}
		box = box.
			AddSection(modal.Spacer()).
			AddSection(modal.Custom(func(int, string, string) modal.RenderedSection {
				return modal.RenderedSection{Content: styles.Muted.Render(label)}
			}, nil))
		if control.decl.Kind == pluginhost.FilterText {
			box = box.AddSection(modal.Input(filterTextPfx+control.decl.ID, &control.text))
			continue
		}
		items := make([]modal.SelectItem, 0, len(control.decl.Choices))
		for _, choice := range control.decl.Choices {
			items = append(items, modal.SelectItem{
				ID:    filterChoicePfx + control.decl.ID + ":" + choice.ID,
				Label: choice.Title,
			})
		}
		box = box.AddSection(modal.Select(
			filterChoicePfx+control.decl.ID, items, &control.choice,
			modal.WithMaxVisible(min(len(items), maxFilterChoicesVisible)),
		))
	}
	box = box.AddSection(modal.Spacer()).AddSection(modal.Buttons(modal.Btn(" Done ", viewDoneID)))

	m.overlay.box = box
	focus := viewSortListID
	if len(sortItems) == 0 {
		focus = viewViewsListID
	}
	if len(sortItems) == 0 && len(c.Views) == 0 && len(m.overlay.filters) > 0 {
		focus = filterFocusID(m.overlay.filters[0])
	}
	m.primeOverlay(focus)
	return nil
}

// maxFilterChoicesVisible bounds how tall ONE selector in this modal draws —
// each filter's choices, and the sort and view lists beside them. A filter may
// declare 64 options; a modal that spent 64 rows on one control would push
// every other control off the box, and a Select scrolls.
const maxFilterChoicesVisible = 6

func filterFocusID(control filterControl) string {
	if control.decl.Kind == pluginhost.FilterText {
		return filterTextPfx + control.decl.ID
	}
	return filterChoicePfx + control.decl.ID
}

// splitFilterChoiceAction reads a radio's action back into the filter it
// belongs to and the choice that was picked. It resolves the filter against the
// live declaration rather than cutting at the first ":", because a filter id is
// storable text and may contain one: cutting would hand setFilter a name no
// collection declares, and the pick would close the modal having done nothing.
func splitFilterChoiceAction(c pluginhost.Collection, action string) (string, string, bool) {
	for _, f := range c.Filters {
		prefix := filterChoicePfx + f.ID + ":"
		if strings.HasPrefix(action, prefix) {
			return f.ID, strings.TrimPrefix(action, prefix), true
		}
	}
	return "", "", false
}

func indexOfFilterChoice(decl pluginhost.Filter, value string) int {
	for i, choice := range decl.Choices {
		if choice.ID == value {
			return i
		}
	}
	return 0
}

func (m *Model) applyViewAction(action string) tea.Cmd {
	c, ok := m.ActiveCollection()
	if !ok {
		m.closeOverlay()
		return nil
	}
	s := m.state(c)
	switch {
	case action == "":
		return nil
	case action == "cancel":
		// Esc discards uncommitted text; the radios have already been applied
		// as they were picked.
		m.closeOverlay()
		return nil
	case action == viewDoneID:
		return m.applyViewClose(c, s)
	case strings.HasPrefix(action, filterTextPfx):
		// Enter inside a text input is a commit, not a submission of the modal:
		// the user is still choosing.
		return m.applyViewClose(c, s)
	case strings.HasPrefix(action, "sort:"):
		key := strings.TrimPrefix(action, "sort:")
		if key == s.sortKey {
			m.closeOverlay()
			return nil
		}
		s.sortKey = key
		s.sortDir = defaultDir(c, key)
		m.closeOverlay()
		return m.list(c, s, false)
	case strings.HasPrefix(action, "view:"):
		id := strings.TrimPrefix(action, "view:")
		if id == s.view {
			m.closeOverlay()
			return nil
		}
		s.view = id
		m.closeOverlay()
		return m.list(c, s, false)
	case strings.HasPrefix(action, filterChoicePfx):
		id, choice, ok := splitFilterChoiceAction(c, action)
		if !ok {
			return nil
		}
		// Picking a radio also commits whatever was typed into the text
		// filters, because both are one statement of what this page should
		// cover and applying half of it would list something nobody asked for.
		changed := m.commitFilterText(c, s)
		changed = m.setFilter(c, s, id, choice) || changed
		m.closeOverlay()
		if !changed {
			return nil
		}
		return m.list(c, s, false)
	}
	return nil
}

// applyViewClose commits the text filters a Done (or an Enter in an input)
// leaves standing, and relists if anything moved. Esc is a cancel and discards
// them: an uncommitted edit is not a choice.
func (m *Model) applyViewClose(c pluginhost.Collection, s *collectionState) tea.Cmd {
	changed := m.commitFilterText(c, s)
	m.closeOverlay()
	if !changed {
		return nil
	}
	return m.list(c, s, false)
}

// commitFilterText folds every text control's value into the applied set and
// reports whether anything changed.
func (m *Model) commitFilterText(c pluginhost.Collection, s *collectionState) bool {
	changed := false
	for i := range m.overlay.filters {
		control := &m.overlay.filters[i]
		if control.decl.Kind != pluginhost.FilterText {
			continue
		}
		if m.setFilter(c, s, control.decl.ID, strings.TrimSpace(control.text.Value())) {
			changed = true
		}
	}
	return changed
}

// setFilter records one filter's value and reports whether it moved. A value
// equal to the filter's default is stored as an ABSENCE, because that is what
// the wire means by a missing key and two spellings of one state is how a pill
// and a request start disagreeing.
func (m *Model) setFilter(c pluginhost.Collection, s *collectionState, id, value string) bool {
	decl, ok := c.Filter(id)
	if !ok {
		return false
	}
	before := decl.Value(s.filters)
	if value == before {
		return false
	}
	if value == decl.Default {
		delete(s.filters, id)
		return true
	}
	if s.filters == nil {
		s.filters = make(map[string]string, len(c.Filters))
	}
	s.filters[id] = value
	return true
}

func defaultDir(c pluginhost.Collection, key string) pluginhost.SortDir {
	for _, k := range c.Sort {
		if k.ID == key && k.Default != "" {
			return k.Default
		}
	}
	return pluginhost.SortAsc
}

func indexOfSort(c pluginhost.Collection, id string) int {
	for i, k := range c.Sort {
		if k.ID == id {
			return i
		}
	}
	return 0
}

func indexOfView(c pluginhost.Collection, id string) int {
	if id == "" {
		return 0
	}
	for i, v := range c.Views {
		if v.ID == id {
			return i + 1
		}
	}
	return 0
}

// applicableActions is every declared action the current selection can reach:
// item actions when there is a row, collection actions always, and global ones.
//
// `on: "resource"` actions are absent on purpose. They apply to a
// matcher-resolved document, which is a different subject from a collection
// row, and offering one here would send the plugin a locator nobody selected.
func (m *Model) applicableActions() []pluginhost.Action {
	c, ok := m.ActiveCollection()
	if !ok {
		return nil
	}
	_, hasRow := m.currentItem()
	var out []pluginhost.Action
	for _, action := range m.desc.Actions {
		switch action.On {
		case pluginhost.ActionOnItem:
			if action.Collection == c.ID && hasRow {
				out = append(out, action)
			}
		case pluginhost.ActionOnCollection:
			if action.Collection == c.ID {
				out = append(out, action)
			}
		case pluginhost.ActionOnGlobal:
			out = append(out, action)
		}
	}
	return out
}

// openActionMenu offers what the current selection can reach. A menu with one
// entry still opens: the confirm step is the point, not the choosing.
//
// A selection that can reach none opens nothing and says nothing. The Actions
// hint is already absent from the footer, and a surface that announces it has
// nothing to offer is a design failure — the missing hint said it first.
func (m *Model) openActionMenu() tea.Cmd {
	actions := m.applicableActions()
	if len(actions) == 0 {
		return nil
	}
	m.setOverlay(overlay{kind: overlayActions, width: m.modalWidth(), actions: actions})
	items := make([]modal.ListItem, 0, len(actions))
	for _, action := range actions {
		label := action.Title
		if key, ok := m.keyFor(action.ID); ok {
			label += "   " + key
		}
		items = append(items, modal.ListItem{ID: "act:" + action.ID, Label: label})
	}
	m.overlay.box = modal.New("Actions", modal.WithWidth(m.overlay.width), modal.WithHints(false)).
		AddSection(modal.List(actionListID, items, &m.overlay.actionIdx, modal.WithMaxVisible(len(items)))).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(modal.Btn(" Cancel ", actionCancelID)))
	m.primeOverlay(actionListID)
	return nil
}

func (m *Model) keyFor(actionID string) (string, bool) {
	for key, id := range m.grantedKeys {
		if id == actionID {
			return key, true
		}
	}
	return "", false
}

func (m *Model) applyMenuAction(action string) tea.Cmd {
	switch {
	case action == "":
		return nil
	case action == "cancel" || action == actionCancelID:
		m.closeOverlay()
		return nil
	case strings.HasPrefix(action, "act:"):
		id := strings.TrimPrefix(action, "act:")
		m.closeOverlay()
		return m.startAction(id)
	}
	return nil
}

// startAction opens whatever step the action needs before it runs: a form when
// it declares inputs, a confirm when it mutates and declares none, and nothing
// at all when it is neither.
func (m *Model) startAction(id string) tea.Cmd {
	action, ok := m.desc.Action(id)
	if !ok {
		return nil
	}
	if !m.actionReachable(action) {
		m.flash, m.flashErr = "That action needs a row.", false
		return nil
	}
	if len(action.Inputs) > 0 {
		return m.openForm(action)
	}
	if action.Confirm {
		return m.openConfirm(action)
	}
	return m.runAction(action, nil)
}

func (m *Model) actionReachable(action pluginhost.Action) bool {
	c, ok := m.ActiveCollection()
	if !ok {
		return false
	}
	switch action.On {
	case pluginhost.ActionOnItem:
		if action.Collection != c.ID {
			return false
		}
		_, hasRow := m.currentItem()
		return hasRow
	case pluginhost.ActionOnCollection:
		return action.Collection == c.ID
	case pluginhost.ActionOnGlobal:
		return true
	default:
		return false
	}
}

// openForm builds one control per declared input. The form is the confirm step
// for an action that has one: a user who filled it in has already said yes.
func (m *Model) openForm(action pluginhost.Action) tea.Cmd {
	m.setOverlay(overlay{kind: overlayForm, width: m.modalWidth(), action: action})
	m.overlay.inputs = make([]formInput, 0, len(action.Inputs))
	for _, decl := range action.Inputs {
		in := formInput{decl: decl}
		switch decl.Kind {
		case pluginhost.InputMultiline:
			ta := textarea.New()
			ta.ShowLineNumbers = false
			ta.Prompt = ""
			ta.SetValue(decl.Default)
			in.area = ta
		case pluginhost.InputChoice:
			in.choice = indexOfChoice(decl, decl.Default)
		case pluginhost.InputConfirm:
			in.flag = strings.EqualFold(decl.Default, "true")
		default:
			ti := textinput.New()
			ti.Prompt = ""
			ti.SetValue(decl.Default)
			in.text = ti
		}
		m.overlay.inputs = append(m.overlay.inputs, in)
	}

	title := action.Title
	if item, ok := m.currentItem(); ok && action.On == pluginhost.ActionOnItem {
		title += " · " + m.itemLabel(item)
	}
	box := modal.New(title, modal.WithWidth(m.overlay.width), modal.WithHints(false))
	for i := range m.overlay.inputs {
		in := &m.overlay.inputs[i]
		id := formInputPrefix + in.decl.ID
		label := in.decl.Label
		if in.decl.Required {
			label += " *"
		}
		switch in.decl.Kind {
		case pluginhost.InputMultiline:
			box = box.AddSection(modal.TextareaWithLabel(id, label, &in.area, 5))
		case pluginhost.InputChoice:
			items := make([]modal.ListItem, 0, len(in.decl.Choices))
			for _, choice := range in.decl.Choices {
				items = append(items, modal.ListItem{ID: id + ":" + choice, Label: choice})
			}
			box = box.
				AddSection(modal.Custom(func(int, string, string) modal.RenderedSection {
					return modal.RenderedSection{Content: label}
				}, nil)).
				AddSection(modal.List(id, items, &in.choice, modal.WithMaxVisible(len(items))))
		case pluginhost.InputConfirm:
			box = box.AddSection(modal.Checkbox(id, label, &in.flag))
		default:
			box = box.AddSection(modal.InputWithLabel(id, label, &in.text))
		}
		box = box.AddSection(modal.Spacer())
	}
	box = box.AddSection(modal.Custom(func(int, string, string) modal.RenderedSection {
		return modal.RenderedSection{Content: m.overlay.err}
	}, nil))
	box = box.AddSection(modal.Buttons(
		modal.Btn(" "+action.Title+" ", actionRunID, modal.BtnPrimary()),
		modal.Btn(" Cancel ", actionCancelID),
	))
	m.overlay.box = box
	focus := ""
	if len(m.overlay.inputs) > 0 {
		focus = formInputPrefix + m.overlay.inputs[0].decl.ID
	}
	m.primeOverlay(focus)
	return nil
}

// openConfirm is the step a mutating action with no inputs gets, unless the
// plugin said confirm:false explicitly.
func (m *Model) openConfirm(action pluginhost.Action) tea.Cmd {
	m.setOverlay(overlay{kind: overlayConfirm, width: m.modalWidth(), action: action})
	subject := ""
	if item, ok := m.currentItem(); ok && action.On == pluginhost.ActionOnItem {
		subject = m.itemLabel(item)
	}
	text := action.Title + "?"
	if subject != "" {
		text = action.Title + " · " + subject + "?"
	}
	m.overlay.box = modal.New("Confirm", modal.WithWidth(m.overlay.width), modal.WithHints(false)).
		AddSection(modal.Text(text)).
		AddSection(modal.Text("This changes data in " + m.Name() + ".")).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" "+action.Title+" ", actionRunID, modal.BtnPrimary()),
			modal.Btn(" Cancel ", actionCancelID),
		))
	m.primeOverlay(actionRunID)
	return nil
}

func (m *Model) applyFormAction(action string) tea.Cmd {
	switch action {
	case "":
		return nil
	case actionCancelID:
		m.closeOverlay()
		return nil
	case actionRunID:
		return m.submitAction()
	}
	// A list inside a form reports its item's ID when Enter lands on it. That
	// is a selection, not a submission: the value is already bound.
	if strings.HasPrefix(action, formInputPrefix) {
		return nil
	}
	return nil
}

// submitAction validates the form and runs the action. A required input left
// empty stops here with a line in the modal rather than sending the plugin a
// value it declared it needs and did not get.
func (m *Model) submitAction() tea.Cmd {
	inputs := make(map[string]string, len(m.overlay.inputs))
	for i := range m.overlay.inputs {
		in := &m.overlay.inputs[i]
		value := ""
		switch in.decl.Kind {
		case pluginhost.InputMultiline:
			value = in.area.Value()
		case pluginhost.InputChoice:
			if in.choice >= 0 && in.choice < len(in.decl.Choices) {
				value = in.decl.Choices[in.choice]
			}
		case pluginhost.InputConfirm:
			if in.flag {
				value = "true"
			} else {
				value = "false"
			}
		default:
			value = in.text.Value()
		}
		if in.decl.Required && strings.TrimSpace(value) == "" && in.decl.Kind != pluginhost.InputConfirm {
			m.overlay.err = in.decl.Label + " is required."
			return nil
		}
		inputs[in.decl.ID] = value
	}
	action := m.overlay.action
	m.closeOverlay()
	return m.runAction(action, inputs)
}

// runAction sends one act. It is never cached and never deduplicated: two
// identical acts are two intentions.
func (m *Model) runAction(action pluginhost.Action, inputs map[string]string) tea.Cmd {
	if m.calls.Act == nil {
		return nil
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return nil
	}
	params := pluginhost.ActParams{Action: action.ID, Inputs: inputs}
	switch action.On {
	case pluginhost.ActionOnItem:
		item, hasRow := m.currentItem()
		if !hasRow {
			return nil
		}
		params.Collection = c.ID
		params.ID = item.ID
	case pluginhost.ActionOnCollection:
		params.Collection = c.ID
	}
	m.seq++
	m.flash, m.flashErr = "", false
	return m.calls.Act(ActCall{
		Instance:   m.instance,
		Browser:    m.id,
		Params:     params,
		Context:    m.context(),
		Generation: m.seq,
	})
}

// itemLabel is the row's own name, which is what a confirm step has to say back
// to the user so they can tell which row they are about to change.
func (m *Model) itemLabel(item pluginhost.Item) string {
	c, ok := m.ActiveCollection()
	if !ok {
		return item.ID
	}
	if col, ok := c.PrimaryColumn(); ok {
		if v := strings.TrimSpace(item.Cells[col.ID]); v != "" {
			return v
		}
	}
	return item.ID
}

func indexOfChoice(decl pluginhost.ActionInput, value string) int {
	for i, choice := range decl.Choices {
		if choice == value {
			return i
		}
	}
	return 0
}

// hasCoverage reports whether there is anything to explain beyond the word
// already on the row. A page that answered with no notices has nothing more to
// say, so the control is absent and its key is inert — restating the outcome in
// a modal would be the same failure as announcing that there is no action here.
func (m *Model) hasCoverage() bool {
	c, ok := m.ActiveCollection()
	if !ok {
		return false
	}
	s := m.state(c)
	if s == nil || !s.loaded {
		return false
	}
	// A `search: required` collection nobody has typed into was never asked
	// anything, so it has made no claim to explain (td-c2dc19).
	if s.unqueried {
		return false
	}
	if len(s.notices) > 0 || len(s.coverage) > 0 || s.omitted.Any() {
		return true
	}
	return s.outcome != pluginhost.OutcomeAnswered
}

// openCoverage explains the claim the page on screen is making: the outcome
// word, what that word means, and every notice in full rather than truncated to
// the row it was drawn on.
//
// It is what the outcome cell and a notice open under the pointer, and `c`
// opens by key, because nothing a click does may be reachable only by click.
// M4b gives it the plugin's own per-source coverage data; the shape it has here
// is what the host can already say honestly.
func (m *Model) openCoverage() tea.Cmd {
	c, ok := m.ActiveCollection()
	if !ok || !m.hasCoverage() {
		return nil
	}
	s := m.state(c)
	m.setOverlay(overlay{kind: overlayCoverage, width: m.coverageModalWidth()})
	width := m.overlay.width
	box := modal.New("Coverage · "+c.Title, modal.WithWidth(width), modal.WithHints(false)).
		AddSection(modal.Text(outcomeStyle(s.outcome).Render(string(s.outcome)))).
		AddSection(modal.Text(outcomeDefinition(s.outcome)))
	if len(s.notices) > 0 {
		box = box.AddSection(modal.Spacer())
		for _, notice := range s.notices {
			// Untruncated: the row it was drawn on had one line, this does not.
			box = box.AddSection(modal.Text(noticeGlyph(notice.Tone) + "  " + notice.Text))
		}
	}
	if s.omitted.Any() {
		// Two lines, one per count, because they are two different reasons a
		// row is missing and a reader acts on them differently.
		box = box.AddSection(modal.Spacer()).
			AddSection(modal.Text(fmt.Sprintf("%d below the relevance floor", s.omitted.Suppressed))).
			AddSection(modal.Text(fmt.Sprintf("%d over the budget", s.omitted.Dropped)))
	}
	if len(s.coverage) > 0 {
		rows := s.coverage
		truncated := s.coverageTruncated
		// The table is laid out against the CONTENT width the section is
		// handed, not the modal's outer width: the difference is the border
		// and padding, and measuring against the wrong one puts an ellipsis on
		// every row that has nothing to elide.
		box = box.AddSection(modal.Spacer()).
			AddSection(modal.Custom(func(contentWidth int, _, _ string) modal.RenderedSection {
				lines := coverageTable(rows, contentWidth)
				if truncated {
					lines = append(lines, styles.Subtle.Render("the plugin sent more sources than Sidecar keeps"))
				}
				return modal.RenderedSection{Content: strings.Join(lines, "\n")}
			}, nil))
	}
	box = box.AddSection(modal.Spacer()).AddSection(modal.Buttons(
		modal.Btn(" Retry ", coverageRetryID, modal.BtnPrimary()),
		modal.Btn(" Done ", coverageDoneID),
	))
	m.overlay.box = box
	// Deliberately no explicit focus: setting one scrolls the body to it, and
	// this modal opens on the outcome — the sentence the reader came for — with
	// the table below it. Focus still lands on Retry, because it is the first
	// focusable, and `r` works wherever the body happens to be scrolled to.
	m.primeOverlay("")
	return nil
}

// coverageModalWidth is wider than the browser's other modals, because this one
// carries a four-column table and a table squeezed to 46 columns is four
// ellipses. It is still held to the box: the modal never exceeds the frame it
// floats over, and the body scrolls when it is taller.
func (m *Model) coverageModalWidth() int {
	w := coverageModalCols
	if m.width-8 > w {
		w = min(m.width-8, coverageModalMaxCols)
	}
	if m.width > 0 && w > m.width-4 {
		w = m.width - 4
	}
	if w < 20 {
		w = 20
	}
	return w
}

// coverageTable renders the per-source ledger: Source, State as a tone pill,
// Elapsed, and the plugin's own reason. The state's colour is the host's, from
// CoverageState.Tone, so a plugin cannot paint its own failure green.
func coverageTable(rows []pluginhost.Coverage, width int) []string {
	sourceW, stateW, elapsedW := 6, 5, 7
	for _, row := range rows {
		sourceW = max(sourceW, ansi.StringWidth(row.Source))
		stateW = max(stateW, ansi.StringWidth(string(row.State)))
		elapsedW = max(elapsedW, ansi.StringWidth(elapsedLabel(row.ElapsedMs)))
	}
	sourceW = min(sourceW, 20)
	// What is left after the three fixed columns and their gaps belongs to the
	// reason, which is the one column whose content is unbounded prose.
	reasonW := width - sourceW - stateW - elapsedW - 3*columnGap
	out := make([]string, 0, len(rows)+2)
	head := padRight("SOURCE", sourceW) + gap() + padRight("STATE", stateW) + gap() +
		padLeft("ELAPSED", elapsedW)
	if reasonW >= 6 {
		head += gap() + padRight("REASON", reasonW)
	}
	out = append(out, styles.Muted.Render(head))
	for _, row := range rows {
		line := padRight(ansi.Truncate(row.Source, sourceW, "…"), sourceW) + gap() +
			fitStyled(toneStyle(row.State.Tone()).Render(string(row.State)), stateW) + gap() +
			padLeft(elapsedLabel(row.ElapsedMs), elapsedW)
		if reasonW >= 6 {
			line += gap() + styles.Muted.Render(ansi.Truncate(row.Reason, reasonW, "…"))
		}
		out = append(out, fitStyled(line, width))
	}
	return out
}

func gap() string { return strings.Repeat(" ", columnGap) }

func padRight(s string, width int) string {
	if pad := width - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func padLeft(s string, width int) string {
	if pad := width - ansi.StringWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// elapsedLabel is how long a source took. A plugin that did not say gets an
// em dash rather than "0ms", because zero milliseconds is a measurement and
// silence is not.
func elapsedLabel(ms int) string {
	switch {
	case ms <= 0:
		return "—"
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	default:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
}

// outcomeDefinition is Sidecar's sentence for each word in the outcome
// vocabulary. It is the host's, not the plugin's, for the same reason
// errorHeadline is: a plugin must not be able to reframe what its own claim
// means.
func outcomeDefinition(outcome pluginhost.PageOutcome) string {
	switch outcome {
	case pluginhost.OutcomeDegraded:
		return "Some source that should have answered could not, so this page is not a fact about the query."
	case pluginhost.OutcomeFailed:
		return "Every source this page needed failed, so it is not an answer at all — not even an empty one."
	case pluginhost.OutcomeAbstained:
		return "Nothing matched and every source was fine, so an empty page is a fact about the query."
	default:
		return "The plugin asked everything it should have, so this page is what there is."
	}
}

// OverlayOpen reports whether a modal is up, for a host deciding whether the
// surface owns the keyboard.
func (m *Model) OverlayOpen() bool { return m.overlay.open() }

// viewControlLabel is the label the pill carries, exported for tests and for a
// host that draws the control somewhere else.
func (m *Model) viewControlLabel() string {
	c, ok := m.ActiveCollection()
	if !ok {
		return workspacelist.SortGlyph
	}
	return m.viewPillLabel(c)
}

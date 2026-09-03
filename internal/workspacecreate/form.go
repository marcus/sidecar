// Package workspacecreate owns the shared Create Workspace form: presentation
// and in-memory state. It does not talk to git or tmux. Hosts bind it and
// submit through workspaceops.
package workspacecreate

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// State accessors are package vars so tests can stub them without a real state file.
var (
	loadLastCreateAgent  = state.GetLastCreateAgent
	saveLastCreateAgent  = state.SetLastCreateAgent
	loadAgentAutoApprove = state.GetAgentAutoApprove
	saveAgentAutoApprove = state.SetAgentAutoApprove
)

// lastKind is the row the list was last left on. The modal remembers it across
// opens because the row a user picked once is the row they usually want again.
var lastKind = KindShell

// Stable field IDs shared by both hosts.
const (
	FieldKind    = "create-kind"
	FieldProject = "create-project"
	FieldName    = "create-name"
	FieldBase    = "create-base"
	FieldAgent   = "create-agent"
	FieldSkip    = "create-skip-permissions"
	ActionCreate = "create-submit"
	ActionCancel = "create-cancel"
)

const worktreeNamePlaceholder = "feature name"

// ProjectItem is one row in the global project combo.
type ProjectItem struct {
	Key   string
	Label string
}

// OpenOpts configures a new form. PreferredAgent and DefaultAgent are the
// already-resolved fallbacks from the host (.sidecar-agent / defaultAgentType);
// this package does not load them from disk.
type OpenOpts struct {
	Kind      Kind
	FocusKind bool
	// UseLastKind starts the list on the row it was last left on, when that
	// row is one this host offers.
	UseLastKind bool
	// AllowTerminalSplit offers the Terminal split row, and nothing else: the
	// flag means "this host can run a SECOND live terminal beside its own".
	// Every other row places a passive pane in a tree, which both hosts have,
	// so no other row is gated on it — bundling them here is what once cost the
	// global browser its resource-provider rows.
	AllowTerminalSplit bool
	// ShowNotes offers the Note row. It follows whether the notes plugin is
	// registered, so a build without Notes never promises a note pane.
	ShowNotes bool
	// PaneKindsOnly drops the Shell and Worktree rows: they create workspace
	// rows, and a host that is not a workspace surface — an ordinary plugin's
	// pane deck — has nowhere to put one. The catalog is otherwise the same
	// table, so a pane kind added there arrives here with no further work.
	PaneKindsOnly bool
	// Providers are the configured terminal-resource provider instances; each
	// becomes its own kind row labelled with its instance ID.
	Providers []ProviderItem
	// TerminalSplitDisabled is why a terminal split cannot be created right
	// now — the on-screen live-terminal cap being the one reason today. When
	// it is set the row and its form render disabled with this one line, and
	// the create paths refuse: the modal states the rule instead of offering a
	// creation that would be refused after the fact.
	TerminalSplitDisabled string
	// TerminalName is the auto-name a terminal split takes when the name field
	// is left empty.
	TerminalName   string
	ShowProject    bool
	ProjectKey     string
	Name           string
	Projects       []ProjectItem
	Agents         []string
	Branches       []string
	NextShell      string
	PreferredAgent string
	DefaultAgent   string
}

// Form is the Create Workspace chooser: inputs, indexes, skip, error, and modal cache.
type Form struct {
	kind Kind
	// providerID is the instance behind the selected row when that row is a
	// resource provider; every resource row shares KindResource, so the Kind
	// alone cannot say which one was chosen.
	providerID string
	// kindIdx mirrors the selected row for the kind control, which owns a
	// plain index the way every other modal section owns its value. selectRow
	// is the only writer on this side; the control writes it directly and
	// then reports the change back through selectRow.
	kindIdx          int
	rows             []kindRow
	showNotes        bool
	step             FormStep
	placement        Placement
	picker           pickerState
	terminalName     string
	terminalDisabled string
	showProject      bool
	openedFocus      string
	// paneKindsOnly is kept past Open only to title the kind step. A host with
	// no Shell or Worktree row cannot create a workspace, and a modal that says
	// it can is the one thing on screen describing what the keystroke did.
	paneKindsOnly bool

	projects     []ProjectItem
	projectKey   string
	projectIndex int
	projectInput textinput.Model

	nameInput textinput.Model
	nextShell string

	branches  []string
	baseInput textinput.Model
	baseIndex int

	allowlist      []string
	preferredAgent string
	defaultAgent   string
	agentType      string
	agentIndex     int
	agentInput     textinput.Model

	skip       bool
	loadedSkip bool
	lastAgent  string

	err string

	modal        *modal.Modal
	modalWidth   int
	cachedKind   Kind
	cachedStep   FormStep
	cachedPicker int
	cachedBranch int
	pendingFocus string
}

// FormStep is which screen of the two-step flow the form shows. Shell and
// Worktree never leave StepKind; pane kinds that need a target continue to
// StepTarget, and Esc there returns here.
type FormStep int

const (
	StepKind FormStep = iota
	StepTarget
)

// Stable field IDs of the target picker step.
const (
	FieldPickerInput = "create-picker-input"
	pickerItemPrefix = "create-picker-item-"
	pickerMaxVisible = 8
)

// Suggestion is one row of a target picker's list. Value is the identifier the
// target resolves from (a workspace-relative path, a git spec, an issue or
// note id); Label is what the row displays; Badge is optional provenance; Meta
// is an optional right-aligned column — an age, today — that answers "which of
// these is the one I want" without competing with the label for the left edge.
type Suggestion struct {
	Value string
	Label string
	Badge string
	Meta  string
}

// Open builds form state and loads last-used agent and that agent's auto-approve.
// It does not persist last agent.
func Open(opts OpenOpts) *Form {
	f := &Form{
		kind:             opts.Kind,
		rows:             kindRowsForOpts(rowOpts{allowTerminalSplit: opts.AllowTerminalSplit, showNotes: opts.ShowNotes, paneKindsOnly: opts.PaneKindsOnly, providers: opts.Providers}),
		showNotes:        opts.ShowNotes,
		terminalName:     strings.TrimSpace(opts.TerminalName),
		terminalDisabled: strings.TrimSpace(opts.TerminalSplitDisabled),
		showProject:      opts.ShowProject,
		paneKindsOnly:    opts.PaneKindsOnly,
		projects:         append([]ProjectItem(nil), opts.Projects...),
		projectKey:       opts.ProjectKey,
		allowlist:        append([]string(nil), opts.Agents...),
		branches:         append([]string(nil), opts.Branches...),
		nextShell:        opts.NextShell,
		preferredAgent:   opts.PreferredAgent,
		defaultAgent:     opts.DefaultAgent,
	}
	f.picker.init()
	if opts.UseLastKind && kindLabel(f.rows, lastKind) != "" {
		f.kind = lastKind
	}
	offered := kindLabel(f.rows, f.kind) != ""
	if !offered {
		f.kind = f.rows[0].Kind
	}
	// The initial row resolves its provider before any change handler runs:
	// the fields below are still being built.
	f.providerID = f.rows[f.firstRowOfKind(f.kind)].ProviderID
	if offered {
		// lastKind is the row a user left the list on, shared by every host. A
		// row this host had to fall back to was nobody's choice, so recording it
		// would let a narrow catalog overwrite the wider hosts' memory — a
		// PaneKindsOnly open would silently move the Workspaces list off Shell.
		// The first arrow key or click writes it through selectRow anyway.
		lastKind = f.kind
	}
	f.nameInput = textinput.New()
	f.nameInput.Prompt = ""
	f.nameInput.CharLimit = 100
	if name := strings.TrimSpace(opts.Name); name != "" {
		f.nameInput.SetValue(name)
	}
	f.updateNamePlaceholder()

	f.projectInput = textinput.New()
	f.projectInput.Prompt = ""
	f.projectInput.CharLimit = 80

	f.baseInput = textinput.New()
	f.baseInput.Prompt = ""
	f.baseInput.CharLimit = 100

	f.agentInput = textinput.New()
	f.agentInput.Prompt = ""
	f.agentInput.CharLimit = 80

	f.resolveProjectIndex()
	f.prefillProjectInput()
	f.agentType = f.pickAgent()
	f.rematchAgentIndex()
	f.loadAutoApprove()
	f.lastAgent = f.agentType
	f.prefillAgentInput()

	if opts.FocusKind || !kindUsesName(f.kind) {
		f.openedFocus = FieldKind
	} else {
		f.openedFocus = FieldName
	}
	f.pendingFocus = f.openedFocus
	return f
}

// Build returns the cached modal, rebuilding when width or the step/kind/
// branch-list shape changes.
func (f *Form) Build(width int) *modal.Modal {
	if f == nil {
		return nil
	}
	if width < 1 {
		width = 52
	}
	prevFocus := f.pendingFocus
	if f.modal != nil {
		if id := f.modal.FocusedID(); id != "" {
			prevFocus = id
		}
	}
	if f.modal != nil && f.modalWidth == width && f.cachedKind == f.kind && f.cachedStep == f.step &&
		f.cachedBranch == len(f.branches) && f.cachedPicker == f.pickerSignature() {
		return f.modal
	}
	if prevFocus == "" {
		prevFocus = f.openedFocus
	}
	f.build(width, prevFocus)
	f.pendingFocus = ""
	return f.modal
}

// pickerSignature changes whenever the picker's visible answer could change:
// its query or how many suggestions each source holds.
func (f *Form) pickerSignature() int {
	h := len(f.picker.files)*1e6 + len(f.picker.refs)*1e4 + len(f.picker.issues)*100 + len(f.picker.notes)
	return h + len(f.picker.input.Value())
}

// RestoreFocus applies the pending focus after the modal has been rendered.
// No-op if focus is already managed by the modal.
func (f *Form) RestoreFocus() {
	if f == nil || f.modal == nil || f.pendingFocus == "" {
		return
	}
	f.modal.SetFocus(f.pendingFocus)
	f.pendingFocus = ""
}

// InitialFocusID is Name, or the kind toggle when Open was given FocusKind.
func (f *Form) InitialFocusID() string {
	if f == nil {
		return ""
	}
	return f.openedFocus
}

// SetBranches replaces the worktree base-branch list. Prefills current as the
// value unless the typed value is still a branch in the new list.
func (f *Form) SetBranches(branches []string, current string) {
	if f == nil {
		return
	}
	f.branches = append([]string(nil), branches...)
	typed := f.baseInput.Value()
	keep := false
	if typed != "" {
		for _, b := range f.branches {
			if b == typed {
				keep = true
				break
			}
		}
	}
	if !keep {
		f.baseInput.SetValue(current)
	}
	f.syncBaseIdx()
	f.invalidate()
}

// AgentItems is this kind's picker (None first for shells, last for worktrees).
func (f *Form) AgentItems() []modal.DropdownItem {
	if f == nil {
		return nil
	}
	ids := f.agentIDs()
	items := make([]modal.DropdownItem, len(ids))
	for i, id := range ids {
		label := agentcatalog.Label(id)
		items[i] = modal.DropdownItem{ID: "agent:" + id, Label: label, Value: label, Data: id}
	}
	return items
}

func (f *Form) Kind() Kind {
	if f == nil {
		return KindShell
	}
	return f.kind
}

// selectRow makes row the chosen one. Selection is a ROW, not just a Kind:
// resource rows all share KindResource, so the provider instance rides with
// the row — picking the second provider must title, validate, and resolve
// against that second instance.
func (f *Form) selectRow(idx int) {
	if f == nil || idx < 0 || idx >= len(f.rows) {
		return
	}
	row := f.rows[idx]
	f.kindIdx = idx
	if f.kind == row.Kind && f.providerID == row.ProviderID {
		return
	}
	f.kind = row.Kind
	f.providerID = row.ProviderID
	lastKind = row.Kind
	if f.step == StepTarget {
		// Kind switching is a step-1 gesture; a stale picker must not survive
		// into the next advance.
		f.step = StepKind
	}
	f.applyKindChange()
}

func (f *Form) SetKind(k Kind) {
	f.selectRow(f.firstRowOfKind(k))
}

// firstRowOfKind resolves a Kind to its row, falling back to row 0 when no
// row offers it. Resource kinds resolve to their FIRST configured instance;
// row-precise selection goes through selectRow via clicks and arrow keys.
func (f *Form) firstRowOfKind(k Kind) int {
	for i, row := range f.rows {
		if row.Kind == k {
			return i
		}
	}
	return 0
}

// Placement is the segmented row's current value; Auto until a placement button
// is clicked.
func (f *Form) Placement() Placement {
	if f == nil {
		return PlacementAuto
	}
	return f.placement
}

// SetPlacement records the placement a button click asked for.
func (f *Form) SetPlacement(placement Placement) {
	if f == nil {
		return
	}
	f.placement = placement
	f.invalidate()
}

// PlacementSplit is the `--split` value for the current placement.
func (f *Form) PlacementSplit() string {
	if f == nil {
		return "auto"
	}
	return PlacementSplit(f.placement)
}

// KindDisabledReason is why the selected row cannot be created right now, or
// empty when it can. It is the one gate every create path asks — keyboard,
// click, and placement button alike — so a disabled row cannot be created
// through a path that forgot to check.
func (f *Form) KindDisabledReason() string {
	if f == nil {
		return ""
	}
	return f.kindDisabledReason(f.kind)
}

func (f *Form) kindDisabledReason(kind Kind) string {
	if f == nil || kind != KindTerminalSplit {
		return ""
	}
	return f.terminalDisabled
}

// ApplyPlacementAction records a placement button click and reports that the
// form should now be submitted. One click is the whole gesture.
func (f *Form) ApplyPlacementAction(action string) bool {
	placement, ok := PlacementFromAction(action)
	if !ok || f == nil {
		return false
	}
	if f.KindDisabledReason() != "" {
		return false
	}
	f.placement = placement
	return true
}

// PlacementAction is what a placement button click means on the current step:
// nothing, a continue-with-this-placement onto the picker step, or a create
// right now. The split exists because pane kinds that need a target cannot
// create from the kind list — but they must not lose the clicked placement,
// which Enter on the picker step then honors.
type PlacementAction int

const (
	PlacementIgnored PlacementAction = iota
	PlacementAdvanced
	PlacementSubmitted
)

// ApplyPlacementActionStep records a placement button click and answers what
// it means for the form's current step and kind.
func (f *Form) ApplyPlacementActionStep(action string) PlacementAction {
	if !f.ApplyPlacementAction(action) {
		return PlacementIgnored
	}
	if f.step == StepKind && f.needsTarget() {
		f.AdvanceToTarget()
		return PlacementAdvanced
	}
	return PlacementSubmitted
}

// ShowPlacement reports that the placement row belongs on screen: only a pane
// this modal places has somewhere to put it. Every pane kind qualifies — File,
// Git diff, td issue, resource, Note, and the Terminal split that had it
// first. Shell and Worktree create workspace rows, not panes.
func (f *Form) ShowPlacement() bool {
	return f != nil && kindIsPane(f.kind)
}

// TerminalName is the name a created terminal split takes: what was typed, or
// the auto-name.
func (f *Form) TerminalName() string {
	if f == nil {
		return ""
	}
	if name := strings.TrimSpace(f.nameInput.Value()); name != "" {
		return name
	}
	return f.terminalName
}

func (f *Form) Name() string {
	if f == nil {
		return ""
	}
	return f.nameInput.Value()
}

func (f *Form) Agent() string {
	if f == nil {
		return ""
	}
	return f.agentType
}

func (f *Form) SkipPerms() bool {
	if f == nil {
		return false
	}
	return f.skip
}

func (f *Form) BaseBranch() string {
	if f == nil {
		return ""
	}
	return f.baseInput.Value()
}

func (f *Form) ProjectKey() string {
	if f == nil {
		return ""
	}
	return f.projectKey
}

func (f *Form) ProjectIndex() int {
	if f == nil {
		return 0
	}
	return f.projectIndex
}

func (f *Form) Error() string {
	if f == nil {
		return ""
	}
	return f.err
}

func (f *Form) SetError(msg string) {
	if f == nil {
		return
	}
	f.err = msg
	if f.modal != nil {
		f.modal.Invalidate()
	}
}

func (f *Form) ShowSkip() bool {
	if f == nil {
		return false
	}
	return workspaceops.AgentSkipFlag(f.selectedAgent()) != ""
}

// Validate reports why Create cannot proceed. Worktree name is required;
// shell name is optional. Empty return means the form is submittable.
func (f *Form) Validate() string {
	if f == nil {
		return ""
	}
	if reason := f.KindDisabledReason(); reason != "" {
		return reason
	}
	if f.kind != KindWorktree {
		return ""
	}
	name := strings.TrimSpace(f.nameInput.Value())
	if name == "" {
		return "Name is required"
	}
	if workspaceops.SlugifyWorktreeName(name) == "" {
		return "Name does not produce a valid git branch"
	}
	return ""
}

// PersistLastAgent writes the current agent after a successful modal create.
func (f *Form) PersistLastAgent() {
	if f == nil {
		return
	}
	f.syncAgentFromIdx()
	_ = saveLastCreateAgent(f.agentType)
}

// SyncAfterInput rematches combo indexes, reloads auto-approve when the agent
// changes, and persists skip immediately on toggle.
func (f *Form) SyncAfterInput() {
	if f == nil {
		return
	}
	f.syncProjectFromIdx()
	prev := f.lastAgent
	f.syncAgentFromIdx()
	if f.agentType != prev {
		f.loadAutoApprove()
		f.lastAgent = f.agentType
	} else if f.skip != f.loadedSkip {
		f.persistSkip()
	}
	// The picker's list answers its query live: every input tick reclamps the
	// cursor and flags the modal for rebuild.
	if f.step == StepTarget {
		f.syncPickerCursor()
	}
}

func (f *Form) Modal() *modal.Modal {
	if f == nil {
		return nil
	}
	return f.modal
}

func (f *Form) build(width int, prevFocus string) {
	if !kindUsesName(f.kind) && prevFocus == FieldName {
		// The Name field is gone for this row, and focus cannot stay on a field
		// that is not drawn — the kind list is where the gesture came from.
		prevFocus = FieldKind
	}
	if prevFocus != FieldProject {
		f.prefillProjectInput()
	}
	if prevFocus != FieldAgent {
		f.prefillAgentInput()
	}
	if prevFocus != FieldBase {
		f.syncBaseIdx()
	}

	f.kindIdx = f.selectedRowIndex()
	f.modalWidth = width
	f.cachedKind = f.kind
	f.cachedStep = f.step
	f.cachedBranch = len(f.branches)
	f.cachedPicker = f.pickerSignature()

	if f.step == StepTarget {
		f.buildPicker(width, prevFocus)
		return
	}

	sections := []modal.Section{
		kindControl(FieldKind, f.rows, &f.kindIdx, f.selectRow, f.kindDisabledReason),
		modal.Spacer(),
	}
	if f.showProject {
		projectItems := f.projectItems()
		sections = append(sections,
			modal.Text("Project"),
			modal.Combo(FieldProject, &f.projectInput, projectItems, &f.projectIndex,
				modal.WithComboFilter(comboExactOrAllFilter(projectItems))),
		)
	}
	if kindUsesName(f.kind) {
		sections = append(sections, modal.InputWithLabel(FieldName, "Name", &f.nameInput))
		sections = append(sections, f.slugHintSection())
	}
	if f.kind == KindWorktree {
		branchItems := f.branchItems()
		sections = append(sections,
			modal.Text("Base Branch"),
			modal.Combo(FieldBase, &f.baseInput, branchItems, &f.baseIndex,
				modal.WithComboFilter(comboExactOrAllFilter(branchItems))),
		)
	}
	if f.kind == KindTerminalSplit {
		disabled := f.KindDisabledReason()
		reason := f.errorSection()
		if disabled != "" {
			reason = modal.Text(styles.Muted.Render(disabled))
		}
		sections = append(sections,
			reason,
			modal.Spacer(),
			placementButtons(disabled != ""),
			modal.Spacer(),
			modal.Buttons(
				createButton(disabled != ""),
				modal.Btn(" Cancel ", ActionCancel),
			),
		)
		f.assemble(width, prevFocus, sections)
		return
	}
	if kindNeedsTarget(f.kind) {
		// Step 1 of a target-needing pane kind: the kind list plus the
		// placement row. Enter continues to the picker; a placement click
		// continues with that placement already recorded.
		hints := modal.WithHintText(kindStepHint(width, "Enter continues · Esc cancels"))
		m := modal.New(f.title(),
			modal.WithWidth(width),
			modal.WithPrimaryAction(ActionCreate),
			hints,
			modal.WithInitialFocus(prevFocus),
		)
		sections = append(sections,
			f.errorSection(),
			modal.Spacer(),
			placementButtons(false),
			modal.Spacer(),
			modal.Buttons(
				modal.Btn(" Cancel ", ActionCancel),
			),
		)
		for _, section := range sections {
			m.AddSection(section)
		}
		f.modal = m
		return
	}
	agentItems := f.AgentItems()
	sections = append(sections,
		modal.Text("Agent"),
		modal.Combo(FieldAgent, &f.agentInput, agentItems, &f.agentIndex,
			modal.WithComboFilter(comboExactOrAllFilter(agentItems))),
		modal.When(f.ShowSkip, modal.Checkbox(FieldSkip, "Auto-approve all actions", &f.skip)),
		f.skipHintSection(),
		f.errorSection(),
		modal.Spacer(),
		modal.Buttons(
			modal.Btn(" Create ", ActionCreate, modal.BtnPrimary()),
			modal.Btn(" Cancel ", ActionCancel),
		),
	)

	f.assemble(width, prevFocus, sections)
}

// kindStepHint is the kind step's hint line: the arrows first, because moving
// the list without leaving the Name field is the gesture nothing else on screen
// announces, then the tail this modal's state calls for. The hint is drawn
// outside the section column, so nothing clamps it — a line too long for the
// box wraps and grows the modal by a row. Tab is the part a user already
// expects of a modal, so it is what a narrow box drops.
func kindStepHint(width int, tail string) string {
	full := "↑↓ type · Tab to switch · " + tail
	if ansi.StringWidth(full) <= width-modal.ChromeWidth {
		return full
	}
	return "↑↓ type · " + tail
}

func (f *Form) assemble(width int, prevFocus string, sections []modal.Section) {
	// Enter resolves to Create, and Create is refused while the selected kind is
	// disabled — so the hint line must not promise a confirm that is a no-op.
	hints := modal.WithHintText(kindStepHint(width, "Enter to confirm · Esc to cancel"))
	if f.KindDisabledReason() != "" {
		hints = modal.WithHintText(kindStepHint(width, "Esc to cancel"))
	}
	m := modal.New(f.title(),
		modal.WithWidth(width),
		modal.WithPrimaryAction(ActionCreate),
		hints,
		modal.WithInitialFocus(prevFocus),
	)
	for _, section := range sections {
		m.AddSection(section)
	}
	f.modal = m
}

// title names the kind step for the host it opened in. On the two Workspaces
// surfaces the list starts with Shell and Worktree and the modal is what it has
// always been; in a plugin host those rows are gone, so the same heading would
// name the one act that host cannot perform.
func (f *Form) title() string {
	if f.paneKindsOnly {
		return "Open Pane"
	}
	return "Create Workspace"
}

// buildPicker assembles the target picker step: "New · <label>" with the
// filter/count/list sections from the picker, the placement row, and a Cancel
// that closes (Esc is the way back). Enter opens with the recorded placement,
// Auto unless a placement button was clicked earlier in this session; clicking
// a placement button creates right now with it.
func (f *Form) buildPicker(width int, prevFocus string) {
	// The chosen ROW's label: for resource rows that is the instance ID that
	// was picked, not whichever provider sorts first.
	m := modal.New("New · "+f.selectedLabel(),
		modal.WithWidth(width),
		modal.WithPrimaryAction(ActionCreate),
		modal.WithInitialFocus(prevFocus),
	)
	m.AddSection(modal.Text(styles.Muted.Render("Open " + strings.ToLower(kindLabel(f.rows, f.kind)) + " in a split")))
	m.AddSection(modal.Spacer())
	for _, section := range f.pickerSections() {
		m.AddSection(section)
	}
	m.AddSection(modal.Spacer())
	m.AddSection(placementButtons(false))
	m.AddSection(f.errorSection())
	f.modal = m
}

// placementButtons is the segmented Auto/Right/Below row. Each button is a
// create action, so one click both chooses the placement and creates.
func placementButtons(disabled bool) modal.Section {
	buttons := make([]modal.ButtonDef, 0, len(placementCatalog))
	for _, row := range placementCatalog {
		opts := []modal.BtnOption{}
		if row.Placement == PlacementAuto {
			opts = append(opts, modal.BtnPrimary())
		}
		if disabled {
			opts = append(opts, modal.BtnDisabled())
		}
		buttons = append(buttons, modal.Btn(" "+row.Label+" ", row.Action, opts...))
	}
	return modal.Buttons(buttons...)
}

// createButton is the modal's Create action, disabled when the selected row
// cannot be created. A disabled Create is unfocusable and inert, so the row's
// refusal is visible before the click rather than in a toast after it.
func createButton(disabled bool) modal.ButtonDef {
	if disabled {
		return modal.Btn(" Create ", ActionCreate, modal.BtnDisabled())
	}
	return modal.Btn(" Create ", ActionCreate, modal.BtnPrimary())
}

func (f *Form) applyKindChange() {
	f.rematchAgentIndex()
	f.updateNamePlaceholder()
	f.invalidate()
}

func (f *Form) invalidate() {
	if f.modal != nil {
		if id := f.modal.FocusedID(); id != "" {
			f.pendingFocus = id
		}
	}
	f.modal = nil
	f.modalWidth = 0
}

func (f *Form) updateNamePlaceholder() {
	if f.kind == KindTerminalSplit {
		ph := f.terminalName
		if ph == "" {
			ph = "Terminal"
		}
		f.nameInput.Placeholder = ph
		return
	}
	if f.kind == KindWorktree {
		f.nameInput.Placeholder = worktreeNamePlaceholder
		return
	}
	ph := strings.TrimSpace(f.nextShell)
	if ph == "" {
		ph = "Shell 1"
	}
	f.nameInput.Placeholder = ph
}

func (f *Form) agentIDs() []string {
	return agentcatalog.ResolvePicker(f.allowlist, f.kind == KindShell)
}

func (f *Form) selectedAgent() string {
	agents := f.agentIDs()
	if f.agentIndex >= 0 && f.agentIndex < len(agents) {
		return agents[f.agentIndex]
	}
	return f.agentType
}

func (f *Form) pickAgent() string {
	agents := f.agentIDs()
	if last := strings.TrimSpace(loadLastCreateAgent()); last != "" && containsString(agents, last) {
		return last
	}
	if pref := strings.TrimSpace(f.preferredAgent); pref != "" && containsString(agents, pref) {
		return pref
	}
	if def := strings.TrimSpace(f.defaultAgent); def != "" && containsString(agents, def) {
		return def
	}
	if f.kind == KindWorktree {
		for _, at := range agents {
			if at != "" {
				return at
			}
		}
	}
	return ""
}

func (f *Form) rematchAgentIndex() {
	agents := f.agentIDs()
	idx := indexOfString(agents, f.agentType)
	if idx < 0 {
		f.agentType = f.pickAgent()
		idx = indexOfString(agents, f.agentType)
	}
	if idx < 0 {
		idx = 0
		if len(agents) > 0 {
			f.agentType = agents[0]
		}
	}
	f.agentIndex = idx
}

func (f *Form) syncAgentFromIdx() {
	agents := f.agentIDs()
	if f.agentIndex >= 0 && f.agentIndex < len(agents) {
		f.agentType = agents[f.agentIndex]
		return
	}
	f.rematchAgentIndex()
}

func (f *Form) loadAutoApprove() {
	f.skip = loadAgentAutoApprove(f.agentType)
	f.loadedSkip = f.skip
}

func (f *Form) persistSkip() {
	if f.agentType == "" {
		return
	}
	_ = saveAgentAutoApprove(f.agentType, f.skip)
	f.loadedSkip = f.skip
}

func (f *Form) prefillAgentInput() {
	label := agentcatalog.Label(f.agentType)
	if f.agentInput.Value() != label {
		f.agentInput.SetValue(label)
	}
}

func (f *Form) projectItems() []modal.DropdownItem {
	items := make([]modal.DropdownItem, 0, len(f.projects))
	for _, p := range f.projects {
		items = append(items, modal.DropdownItem{
			ID:    "project:" + p.Key,
			Label: p.Label,
			Value: p.Label,
			Data:  p.Key,
		})
	}
	return items
}

func (f *Form) resolveProjectIndex() {
	if f.projectKey != "" {
		for i, p := range f.projects {
			if p.Key == f.projectKey {
				f.projectIndex = i
				return
			}
		}
	}
	if len(f.projects) > 0 {
		f.projectIndex = 0
		f.projectKey = f.projects[0].Key
	}
}

func (f *Form) syncProjectFromIdx() {
	if f.projectIndex >= 0 && f.projectIndex < len(f.projects) {
		f.projectKey = f.projects[f.projectIndex].Key
	}
}

func (f *Form) prefillProjectInput() {
	label := ""
	if f.projectIndex >= 0 && f.projectIndex < len(f.projects) {
		label = f.projects[f.projectIndex].Label
	}
	if f.projectInput.Value() != label {
		f.projectInput.SetValue(label)
	}
}

func (f *Form) branchItems() []modal.DropdownItem {
	items := make([]modal.DropdownItem, len(f.branches))
	for i, branch := range f.branches {
		items[i] = modal.DropdownItem{ID: branch, Label: branch, Value: branch}
	}
	return items
}

func (f *Form) syncBaseIdx() {
	val := f.baseInput.Value()
	for i, branch := range f.branches {
		if branch == val {
			f.baseIndex = i
			return
		}
	}
	if f.baseIndex >= len(f.branches) {
		f.baseIndex = 0
	}
}

func (f *Form) slugHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if f.kind != KindWorktree {
			return modal.RenderedSection{}
		}
		display := strings.TrimSpace(f.nameInput.Value())
		slug := workspaceops.SlugifyWorktreeName(display)
		if slug == "" || slug == display {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: styles.Muted.Render("git: " + slug)}
	}, nil)
}

func (f *Form) skipHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if !f.ShowSkip() {
			return modal.RenderedSection{}
		}
		flag := workspaceops.AgentSkipFlag(f.selectedAgent())
		return modal.RenderedSection{Content: styles.Muted.Render(fmt.Sprintf("      (Adds %s)", flag))}
	}, nil)
}

func (f *Form) errorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if f.err == "" {
			return modal.RenderedSection{}
		}
		errStyle := lipgloss.NewStyle().Foreground(styles.Error)
		// Wrap rather than let the modal cut the line off. A remote failure's
		// message is two halves — what the host said, then what to do about it
		// — and the half that gets clipped at this width is always the second
		// one, which is the only half the user can act on.
		if contentWidth > 0 {
			errStyle = errStyle.Width(contentWidth)
		}
		return modal.RenderedSection{Content: errStyle.Render("Error: " + f.err)}
	}, nil)
}

func containsString(list []string, id string) bool {
	return indexOfString(list, id) >= 0
}

func indexOfString(list []string, id string) int {
	for i, at := range list {
		if at == id {
			return i
		}
	}
	return -1
}

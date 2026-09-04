package configui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentintegration"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/styles"
)

// Configuration → Agents → Integrations.
//
// An integration is a small addition to a supported agent's own configuration
// agent, which reports that agent's own lifecycle events so Sidecar does not
// have to read them off its screen.
//
// Every fact and every action on this route comes from
// [agentintegration.Service] — the same application service behind
// `sidecar agent integration ...`. Nothing here decides what an install does,
// what makes a status current, or when a mutation is refused. That is not
// tidiness: a surface that computed any of it would be a second answer, and the
// two would agree only until one of them changed.
//
// Two consequences worth stating, because they shape the code:
//
// Every mutation is confirmed against the service's own dry-run plan, so the
// files named in the confirmation are the files the mutation will touch — the
// same list, from the same call, that `--dry-run` prints.
//
// Nothing here runs on a render path. Discovery reads directories, hashes
// installed files, and looks up provider executables on PATH; all of it happens
// in a tea.Cmd, and the route paints a checking state until the answer arrives.

const (
	// regionAgentIntegrations is the row on the Agents page that opens this
	// route.
	regionAgentIntegrations = "config-agent-integrations"

	regionIntegrationRow     = "config-integration-row-"
	regionIntegrationAction  = "config-integration-action-"
	regionIntegrationRecheck = "config-integration-recheck"
)

// ChildAgentIntegrations is the focused route listing agent integrations.
const ChildAgentIntegrations ChildID = "agent-integrations"

// Action shortcut letters.
//
// The action pills are deliberately not cursor stops. Four of them sit on every
// row, so a cursor that visited them would need five presses per provider to
// cross the table, and moving onto one would take the highlight off the row the
// pill acts on. Remote Hosts' row actions work the same way. That makes a
// shortcut the keyboard route to them, and the letters therefore have to be
// free.
//
// They are not all first letters, because i, d, and e are already spoken for by
// other pages and controlCommand maps a letter to a footer name globally: a
// pill labelled "Install" whose footer said "Init" is exactly the drift the
// shared key/label rule exists to prevent. Each label carries its own key.
const (
	keyIntegrationInstall   = "s"
	keyIntegrationUpdate    = "u"
	keyIntegrationRepair    = "p"
	keyIntegrationUninstall = "x"
)

func integrationActionKey(act agentintegration.Action) string {
	switch act {
	case agentintegration.ActionInstall:
		return keyIntegrationInstall
	case agentintegration.ActionUpdate:
		return keyIntegrationUpdate
	case agentintegration.ActionRepair:
		return keyIntegrationRepair
	case agentintegration.ActionUninstall:
		return keyIntegrationUninstall
	}
	return ""
}

func integrationActionLabel(act agentintegration.Action) string {
	key := strings.ToUpper(integrationActionKey(act))
	name := strings.ToUpper(string(act)[:1]) + string(act)[1:]
	if act == agentintegration.ActionUninstall {
		name = "Remove"
	}
	return key + "  " + name
}

// agentIntegrationsState is what the route knows between frames.
type agentIntegrationsState struct {
	// checking and checked are the tri-state that lets the route say
	// "Checking…" rather than "nothing is installed" before it has looked.
	checking bool
	checked  bool
	// generation drops the result of a probe the user has already superseded.
	generation uint64

	list   []agentintegration.Status
	cursor int

	// notice is the outcome of the last mutation, shown until the next one.
	notice string
	// busy is the action in flight, so the route can say so rather than look
	// frozen while a write happens.
	busy string
}

func (m *Model) agentIntegrations() *agentIntegrationsState {
	if m.agentIntegrationsState == nil {
		m.agentIntegrationsState = &agentIntegrationsState{}
	}
	return m.agentIntegrationsState
}

// SetIntegrationService overrides the application service this route uses.
//
// It exists for tests, which must describe a machine rather than have one:
// without it every test of this route would inspect, and its mutation tests
// would rewrite, the developer's real provider configuration.
func (m *Model) SetIntegrationService(svc *agentintegration.Service) { m.integrationService = svc }

func (m *Model) integrations() agentintegration.Service {
	if m.integrationService != nil {
		return *m.integrationService
	}
	return agentintegration.NewService()
}

// OpenAgentIntegrations pushes the route and starts discovery.
func (m *Model) OpenAgentIntegrations() {
	m.PushChild(ChildAgentIntegrations, "Integrations")
	m.queueIntegrationProbe()
}

// queueIntegrationProbe schedules discovery.
//
// It goes on the pending queue rather than being returned as a command because
// the callers are a route push and a control, neither of which has a tea.Cmd
// return path of its own that reaches the runtime. Nothing about it may happen
// synchronously: it stats directories, reads and hashes files, and looks up
// executables on PATH, and the Configuration surface paints on the frame that
// opens it.
func (m *Model) queueIntegrationProbe() {
	state := m.agentIntegrations()
	if state.checking {
		return
	}
	state.checking, state.checked = true, false
	state.generation++
	generation := state.generation
	svc := m.integrations()
	m.pending = append(m.pending, func() tea.Msg {
		return agentIntegrationsMsg{Generation: generation, List: svc.List()}
	})
}

// agentIntegrationsMsg carries a completed discovery back to the route.
//
// It has no error field on purpose. [agentintegration.Service.List] cannot
// fail: a path it cannot read becomes a FileState carrying the reason, and a
// provider it cannot inspect is reported as unsupported. Carrying an error that
// is never set would give the route a branch nothing can reach and a reader the
// impression that discovery has a failure mode it does not have.
type agentIntegrationsMsg struct {
	Generation uint64
	List       []agentintegration.Status
}

func (agentIntegrationsMsg) configMsg() {}

// agentIntegrationPlanMsg carries the dry-run plan a confirmation is built
// from. The confirmation names the files the service says it will touch, rather
// than a description this page composed and hoped was accurate.
type agentIntegrationPlanMsg struct {
	Provider string
	Action   agentintegration.Action
	Plan     agentintegration.Plan
	Err      string
}

func (agentIntegrationPlanMsg) configMsg() {}

// agentIntegrationMutationMsg carries a completed mutation.
type agentIntegrationMutationMsg struct {
	Provider string
	Action   agentintegration.Action
	Plan     agentintegration.Plan
	Err      string
}

func (agentIntegrationMutationMsg) configMsg() {}

func (m *Model) applyIntegrationList(msg agentIntegrationsMsg) {
	state := m.agentIntegrations()
	if msg.Generation != 0 && msg.Generation != state.generation {
		return
	}
	state.checking, state.checked = false, true
	state.list = msg.List
	if state.cursor >= len(state.list) {
		state.cursor = max(0, len(state.list)-1)
	}
}

// applyIntegrationPlan raises the confirmation for a planned mutation.
func (m *Model) applyIntegrationPlan(msg agentIntegrationPlanMsg) tea.Cmd {
	state := m.agentIntegrations()
	state.busy = ""
	if msg.Err != "" {
		// A refusal is the service declining, with a reason. It is shown where
		// the outcome of an action is shown, not raised as a dialog: the user
		// asked for something Sidecar will not do, and the answer is one line.
		state.notice = msg.Err
		return nil
	}
	if msg.Plan.Unchanged {
		state.notice = fmt.Sprintf("%s is already %s; nothing to do.", msg.Provider, describeStatusShort(msg.Plan.StatusBefore))
		return nil
	}
	m.confirm = integrationConfirm(msg.Provider, msg.Action, msg.Plan, m.integrations())
	m.rowCursor = 0
	return nil
}

func (m *Model) applyIntegrationMutation(msg agentIntegrationMutationMsg) tea.Cmd {
	state := m.agentIntegrations()
	state.busy = ""
	if msg.Err != "" {
		state.notice = msg.Err
	} else {
		state.notice = describeMutation(msg.Provider, msg.Action, msg.Plan)
	}
	// The list is re-read rather than patched. What a mutation intended and
	// what the filesystem now holds are different claims, and only the second
	// one is worth showing.
	m.queueIntegrationProbe()
	return nil
}

func describeMutation(provider string, act agentintegration.Action, p agentintegration.Plan) string {
	if p.Unchanged {
		return fmt.Sprintf("%s %s: nothing needed changing.", provider, act)
	}
	files := len(p.Ops)
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	return fmt.Sprintf("%s %s: %d %s changed; now %s.", provider, act, files, noun, p.StatusAfter)
}

// abbreviateHome replaces the user's home directory with ~, so a path stays
// legible in a pane that is 34 columns wide.
func abbreviateHome(path, home string) string {
	if home == "" || !strings.HasPrefix(path, home+"/") {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

func describeStatusShort(s agentlifecycle.IntegrationStatus) string {
	switch s {
	case agentlifecycle.StatusCurrent:
		return "up to date"
	case agentlifecycle.StatusNotInstalled:
		return "not installed"
	}
	return string(s)
}

// integrationConfirm builds the confirmation for one mutation.
//
// It names every path, in order, with what happens to it. "Explicit" in the
// plan means the user sees the files before they change, and a dialog that said
// "Install the OpenCode integration?" would not be that.
func integrationConfirm(provider string, act agentintegration.Action, plan agentintegration.Plan, svc agentintegration.Service) *confirmState {
	title := strings.ToUpper(string(act)[:1]) + string(act)[1:] + " integration"
	intro := []string{Body(fmt.Sprintf("%s the %s integration?", strings.ToUpper(string(act)[:1])+string(act)[1:], provider))}

	// Paths are wrapped rather than painted as fixed lines. The detail pane
	// truncates, and a confirmation whose whole point is naming the file must
	// not be the place where a long path loses its last component to an
	// ellipsis. Home is abbreviated for the same reason: shorter is more
	// legible, and `~` is unambiguous.
	body := []string{IndentedMuted(fmt.Sprintf("%s -> %s", plan.StatusBefore, plan.StatusAfter)), ""}
	wrap := []string{}
	for _, op := range plan.Ops {
		wrap = append(wrap, fmt.Sprintf("%s  %s", op.Kind, abbreviateHome(op.Path, svc.Env.Home)))
		wrap = append(wrap, "    "+op.Note)
	}
	wrap = append(wrap, "")
	switch act {
	case agentintegration.ActionUninstall:
		wrap = append(wrap, "Only files Sidecar installed are removed. The provider's own configuration and every other plugin are left exactly as they are.")
	default:
		wrap = append(wrap, "Writes are atomic, anything replaced is backed up beside it, and unrelated configuration is untouched. The integration reports lifecycle facts only: never prompt text, response text, tool arguments or results, or credentials.")
	}

	return &confirmState{
		title:    title,
		intro:    intro,
		body:     body,
		wrapBody: wrap,
		apply: func(m *Model) tea.Cmd {
			state := m.agentIntegrations()
			state.busy = string(act)
			state.notice = ""
			return func() tea.Msg {
				applied, err := svc.Apply(provider, act)
				msg := agentIntegrationMutationMsg{Provider: provider, Action: act, Plan: applied}
				if err != nil {
					msg.Err = err.Error()
				}
				return msg
			}
		},
	}
}

// planIntegration asks the service what an action would do, so the
// confirmation can name it.
func (m *Model) planIntegration(provider string, act agentintegration.Action) tea.Cmd {
	state := m.agentIntegrations()
	state.busy = string(act)
	state.notice = ""
	svc := m.integrations()
	return func() tea.Msg {
		plan, err := svc.Plan(provider, act)
		msg := agentIntegrationPlanMsg{Provider: provider, Action: act, Plan: plan}
		if err != nil {
			msg.Err = err.Error()
		}
		return msg
	}
}

// The route is a table, not an accordion.
//
// Every provider Sidecar can install for is one row of fixed height, and the
// action pills sit in a fixed column on every one of those rows: painted and
// live where the service offers the action, painted and inert where it does
// not. Moving the cursor therefore changes the highlight and nothing else.
//
// That is not a preference about looks. The row used to grow a detail
// paragraph and a pill row when it took focus, so every row below it moved by
// three or four lines as the cursor passed, and a pill could not be clicked
// without first focusing the row it belonged to, which is to say the pointer
// could not reach the actions at all, because arriving at them moved them.
//
// The facts that used to live on the focused row live in one detail box below
// the table, which follows the cursor the way every list-and-detail pair in
// Sidecar does. It is fixed height for the same reason the rows are.

const (
	// integrationColumnGap is the gutter between two table columns.
	integrationColumnGap = 2
	// integrationTierColumn caps the tier column. The longest tier today is
	// "screen-fallback"; a longer one is truncated rather than allowed to push
	// the action pills off the row.
	integrationTierColumn = 15
	// integrationFilesColumn caps the files column on a wide terminal. A path
	// column that grows without limit turns the table into two clusters with a
	// gulf between them.
	integrationFilesColumn = 34
	// integrationFilesMinimum is the narrowest a files column is worth drawing.
	integrationFilesMinimum = 14
	// integrationNameColumn caps the provider column.
	integrationNameColumn = 16
	// integrationDetailLabel is the width of the detail box's label column.
	integrationDetailLabel = 8
)

// integrationTable is the column plan for one frame: how wide each column is,
// which optional columns the pane can afford, and whether the action pills
// carry their names or only their keys.
//
// It is computed once per frame from the pane's width and the rows themselves,
// so every row lands on the same columns. A row that measured itself would
// produce a ragged table the moment one provider had a longer name than
// another.
type integrationTable struct {
	name   int
	status int
	// tier and files are 0 when the pane cannot afford the column.
	tier  int
	files int
	// compact means the pills carry their shortcut letter alone, with the
	// legend line under the table naming what each letter does. It is what a
	// narrow pane buys the action column with, because the action column is the
	// one thing on the row that is never dropped.
	compact bool
	// gap is the gutter between two columns. It closes to one column before the
	// name and status columns are made to give up any of their text.
	gap int
	// width is the painted width of a row, including the left indent, which is
	// also the width of the selection band and of the row's hit region.
	width int
}

// The floors under the two columns a very narrow pane has to squeeze. Six
// columns still tells providers apart; seven holds "current" and the first
// syllable of everything else.
const (
	integrationNameFloor   = 6
	integrationStatusFloor = 7
)

// layoutIntegrationTable decides the columns.
//
// The order it gives things up in is the order of how much each one is worth:
// the files column first, then the tier column, then the pill labels. The
// actions themselves are never given up, because a table whose actions vanish
// at 80 columns is the accordion again with extra steps.
func layoutIntegrationTable(rows []agentintegration.Status, inner int) integrationTable {
	t := integrationTable{name: ansi.StringWidth(integrationHeaderName), status: ansi.StringWidth(integrationHeaderStatus)}
	tier := ansi.StringWidth(integrationHeaderTier)
	for _, st := range rows {
		t.name = max(t.name, ansi.StringWidth(st.Provider))
		t.status = max(t.status, ansi.StringWidth(integrationStatusWord(st)))
		tier = max(tier, ansi.StringWidth(string(st.EffectiveTier)))
	}
	t.name = min(t.name, integrationNameColumn)
	tier = min(tier, integrationTierColumn)

	available := max(0, inner-RowIndent)
	t.gap = integrationColumnGap
	t.compact = t.name+t.gap+t.status+t.gap+integrationActionsWidth(false) > available
	actions := integrationActionsWidth(t.compact)
	used := t.name + t.gap + t.status + t.gap + actions

	// Give the pane back what it cannot afford, cheapest first: the gutters
	// close, then the name column shrinks, then the status column. The action
	// column is never touched, because a table whose actions vanish at 60
	// columns is the accordion again with extra steps.
	if used > available && t.gap > 1 {
		t.gap = 1
		used -= 2
	}
	if over := used - available; over > 0 {
		give := min(over, t.name-integrationNameFloor)
		if give > 0 {
			t.name -= give
			used -= give
		}
	}
	if over := used - available; over > 0 {
		give := min(over, t.status-integrationStatusFloor)
		if give > 0 {
			t.status -= give
			used -= give
		}
	}

	if used+t.gap+tier <= available {
		t.tier = tier
		used += t.gap + tier
	}
	if t.tier > 0 {
		if room := available - used - t.gap; room >= integrationFilesMinimum {
			t.files = min(room, integrationFilesColumn)
			used += t.gap + t.files
		}
	}
	t.width = RowIndent + used
	return t
}

// Column names are the CLI's own, so `sidecar agent integration list` and this
// page name the same facts the same way.
const (
	integrationHeaderName    = "PROVIDER"
	integrationHeaderStatus  = "STATUS"
	integrationHeaderTier    = "TIER"
	integrationHeaderFiles   = "FILES"
	integrationHeaderActions = "ACTIONS"
)

// integrationActionsWidth is the width of the action column: every action in
// the frozen vocabulary, whether or not any row offers it, because the column
// has to be the same width on every row.
func integrationActionsWidth(compact bool) int {
	width := 0
	for i, act := range agentintegration.Actions() {
		if i > 0 {
			width++
		}
		// Two columns for the pill's own padding, which styles.Button adds.
		width += ansi.StringWidth(integrationPillText(act, compact)) + 2
	}
	return width
}

// integrationPillText is what a pill says. The compact form is the shortcut
// letter alone; the legend under the table is what keeps it legible.
func integrationPillText(act agentintegration.Action, compact bool) string {
	if compact {
		return strings.ToUpper(integrationActionKey(act))
	}
	return integrationActionLabel(act)
}

// integrationStatusWord is the status column's vocabulary.
//
// It says what is true of the integration, not of the agent, with one
// exception that has to be named: an agent whose CLI is not on PATH cannot
// have an integration installed, and "not on PATH" is the reason. Calling that
// "not installed" would leave two rows reading the same while meaning
// different things.
func integrationStatusWord(st agentintegration.Status) string {
	switch st.Status {
	case agentlifecycle.StatusCurrent:
		return "current"
	case agentlifecycle.StatusOutdated:
		return "outdated"
	case agentlifecycle.StatusNeedsRepair:
		return "needs repair"
	case agentlifecycle.StatusNotInstalled:
		return "available"
	case agentlifecycle.StatusProviderMissing:
		return "not on PATH"
	case agentlifecycle.StatusUnsupported:
		return "unsupported"
	}
	return string(st.Status)
}

// integrationStatusStyle colours the status word by what it means, which is the
// only thing colour is spent on. A column of filled badges would say the same
// thing seventeen times as loudly.
func integrationStatusStyle(st agentintegration.Status) lipgloss.Style {
	style := lipgloss.NewStyle()
	switch st.Status {
	case agentlifecycle.StatusCurrent:
		return style.Foreground(styles.Success)
	case agentlifecycle.StatusOutdated:
		return style.Foreground(styles.Warning)
	case agentlifecycle.StatusNeedsRepair:
		return style.Foreground(styles.Error)
	case agentlifecycle.StatusNotInstalled:
		return style.Foreground(styles.Info)
	}
	return style.Foreground(styles.TextMuted)
}

// splitIntegrations separates the providers that can have an integration from
// the ones Sidecar has surveyed and ships nothing for. Both slices hold indices
// into the discovered list, so a row's identity never depends on which of the
// two it landed in.
func splitIntegrations(list []agentintegration.Status) (rows, unsupported []int) {
	for i := range list {
		if list[i].Status == agentlifecycle.StatusUnsupported {
			unsupported = append(unsupported, i)
			continue
		}
		rows = append(rows, i)
	}
	return rows, unsupported
}

// buildAgentIntegrations paints the route.
func (m *Model) buildAgentIntegrations(b *paneBuilder) {
	state := m.agentIntegrations()

	b.lead("A small addition to an agent's own configuration, so Sidecar learns what that agent is doing from its own lifecycle events instead of its screen.")
	b.blank()

	if state.checking && !state.checked {
		b.lead("Checking which agents are installed…")
		return
	}
	if !state.checked {
		// Reached without going through OpenAgentIntegrations — a restored
		// route, or a direct push. Discovery is deliberately not started from
		// here. This is a render path, and inspecting integrations stats
		// directories, hashes files, and looks up executables on PATH; queuing
		// it here also painted "Checking…" over work that only a keypress would
		// drain, so the route claimed to be looking while nothing was. It now
		// says what it actually knows, and names the key that finds out.
		b.rightControl(Body("Agents")+"  "+Muted("not checked yet"),
			regionIntegrationRecheck, "r", "R  Recheck", func(m *Model) tea.Cmd {
				m.queueIntegrationProbe()
				return m.drain(nil)
			})
		b.blank()
		b.lead("Sidecar has not looked yet. Press R to check which agents are installed.")
		return
	}

	rows, unsupported := splitIntegrations(state.list)
	b.rightControl(integrationHeaderLeft(b.inner, state.list),
		regionIntegrationRecheck, "r", "R  Recheck", func(m *Model) tea.Cmd {
			m.queueIntegrationProbe()
			return m.drain(nil)
		})
	b.blank()

	if len(state.list) == 0 {
		b.lead("Sidecar ships no agent integrations in this build.")
		return
	}

	m.syncIntegrationCursor()
	m.clampIntegrationCursor(rows)

	if len(rows) > 0 {
		table := layoutIntegrationTable(integrationRows(state.list, rows), b.inner)
		b.text(integrationColumnHeader(table))
		for _, index := range rows {
			m.buildIntegrationRow(b, table, index, state.list[index])
		}
		if table.compact {
			b.text(integrationKeyLegend())
		}
	}

	if len(unsupported) > 0 {
		b.blank()
		names := make([]string, 0, len(unsupported))
		for _, index := range unsupported {
			names = append(names, state.list[index].Provider)
		}
		// One line for all of them rather than a row each. An agent Sidecar
		// ships nothing for has no action, no files and no tier, so a table row
		// would be four empty columns saying what one sentence says once.
		b.note("Unsupported: " + strings.Join(names, ", ") + ". Sidecar has surveyed these agents and ships no integration for them yet.")
	}

	b.blank()
	switch {
	case state.busy != "":
		b.note("Working…")
		b.blank()
	case state.notice != "":
		b.note(state.notice)
		b.blank()
	}

	if len(rows) > 0 && state.cursor >= 0 && state.cursor < len(state.list) {
		m.buildIntegrationDetail(b, state.list[state.cursor])
		b.blank()
	}

	b.note("Every fact and action here is also `sidecar agent integration`, with --dry-run and --json.")
}

// integrationRows resolves indices back to the statuses they name.
func integrationRows(list []agentintegration.Status, indices []int) []agentintegration.Status {
	out := make([]agentintegration.Status, 0, len(indices))
	for _, index := range indices {
		out = append(out, list[index])
	}
	return out
}

// integrationHeaderLeft is the left half of the header line: the page's subject
// and, when the pane can hold it beside the Recheck control, the count.
//
// The count is dropped rather than truncated. rightControl pins its pill to the
// pane's right edge and the pane clips whatever ran past it, so a header that
// asked for more columns than it had lost the pill's last letters -- which is
// how "R  Recheck" became "R  Rech…" at 80 columns.
func integrationHeaderLeft(inner int, list []agentintegration.Status) string {
	const recheck = "  R  Recheck  "
	left := Body("Agents")
	summary := "  " + summariseIntegrations(list)
	if ansi.StringWidth("Agents")+ansi.StringWidth(summary)+ansi.StringWidth(recheck) <= inner {
		left += Muted(summary)
	}
	return left
}

// integrationColumnHeader names the columns, in the CLI's own words.
func integrationColumnHeader(t integrationTable) string {
	cells := []string{
		padDisplay(clampStart(integrationHeaderName, t.name), t.name),
		padDisplay(clampStart(integrationHeaderStatus, t.status), t.status),
	}
	if t.tier > 0 {
		cells = append(cells, padDisplay(integrationHeaderTier, t.tier))
	}
	if t.files > 0 {
		cells = append(cells, padDisplay(integrationHeaderFiles, t.files))
	}
	cells = append(cells, integrationHeaderActions)
	return strings.Repeat(" ", RowIndent) + Muted(clampStart(strings.Join(cells, strings.Repeat(" ", t.gap)), t.width-RowIndent))
}

// integrationKeyLegend names the shortcut letters the compact pills carry
// alone. It is drawn only in the compact form, because in the wide form each
// pill already says it.
func integrationKeyLegend() string {
	parts := make([]string, 0, 4)
	for _, act := range agentintegration.Actions() {
		parts = append(parts, integrationActionLabel(act))
	}
	return strings.Repeat(" ", RowIndent) + Muted(strings.Join(parts, "   "))
}

// buildIntegrationRow paints one provider on one line, always the same line.
//
// The row is the cursor stop. The pills on it are not: a pill that took the
// cursor would move focus off the row it acts on, and with four pills per row
// and seventeen rows the cursor would need eighty-five presses to cross the
// page. They answer to the pointer on every row, and to their shortcut letters
// on the row the cursor is on.
func (m *Model) buildIntegrationRow(b *paneBuilder, t integrationTable, index int, st agentintegration.Status) {
	id := fmt.Sprintf("%s%d", regionIntegrationRow, index)
	offered := map[agentintegration.Action]bool{}
	for _, act := range st.Offered {
		offered[act] = true
	}

	rowState := b.declare(id, "", true, func(m *Model) tea.Cmd {
		m.agentIntegrations().cursor = index
		return nil
	})
	// A pointer anywhere on the row, including over a pill, lights the row it
	// is on, so the highlight and the thing about to be clicked agree.
	if !rowState.Focused {
		for _, act := range agentintegration.Actions() {
			if b.hovering(integrationActionRegion(index, act)) {
				rowState.Hovered = true
			}
		}
	}

	cells := []string{
		integrationNameCell(st.Provider, t.name, rowState),
		integrationStatusStyle(st).Render(padDisplay(clampStart(integrationStatusWord(st), t.status), t.status)),
	}
	if t.tier > 0 {
		cells = append(cells, Muted(padDisplay(clampStart(string(st.EffectiveTier), t.tier), t.tier)))
	}
	if t.files > 0 {
		cells = append(cells, Muted(padDisplay(integrationFilesCell(st, m.integrations().Env.Home, t.files), t.files)))
	}

	// Pills are rendered before the row is assembled so their exact widths are
	// what the hit regions are computed from. A region measured from anything
	// but the pill that was painted is a region that drifts the first time a
	// label changes.
	pills := make([]string, 0, 4)
	for _, act := range agentintegration.Actions() {
		act := act
		if !offered[act] {
			// Painted, so the column keeps its shape and the reader learns
			// where Remove lives before they ever need it; inert, because the
			// service would refuse it. No control, no region, no shortcut.
			pills = append(pills, Button(integrationPillText(act, t.compact), false, State{Disabled: true}))
			continue
		}
		key := ""
		if rowState.Focused {
			// Only the focused row's pills claim the letters. runShortcut takes
			// the first control carrying a key, so letting every row register
			// them would make S mean "install whatever is at the top".
			key = integrationActionKey(act)
		}
		pillID := integrationActionRegion(index, act)
		pillState := b.declare(pillID, key, false, func(m *Model) tea.Cmd {
			// Clicking a pill on a row the keyboard is not on moves the
			// keyboard there too, so the notice, the detail box and the
			// highlight all describe the provider that was just acted on.
			m.agentIntegrations().cursor = index
			m.focusControlByID(fmt.Sprintf("%s%d", regionIntegrationRow, index))
			return m.planIntegration(st.Provider, act)
		})
		pills = append(pills, Button(integrationPillText(act, t.compact), act == agentintegration.ActionInstall, pillState))
	}
	cells = append(cells, strings.Join(pills, " "))

	line := strings.Repeat(" ", RowIndent) + strings.Join(cells, strings.Repeat(" ", t.gap))
	y := len(b.lines)
	b.lines = append(b.lines, HighlightRow(line, t.width, rowState))

	// The row's region first, then the pills over it: HitMap.Test scans in
	// reverse, so the smaller, more specific target has to be registered last.
	b.m.mouse.HitMap.AddRect(id, b.originX, 1+y, t.width, 1, nil)
	x := b.originX + t.width - integrationActionsWidth(t.compact)
	for i, act := range agentintegration.Actions() {
		width := ansi.StringWidth(pills[i])
		if offered[act] {
			b.m.mouse.HitMap.AddRect(integrationActionRegion(index, act), x, 1+y, width, 1, nil)
		}
		x += width + 1
	}
}

func integrationActionRegion(index int, act agentintegration.Action) string {
	return fmt.Sprintf("%s%d-%s", regionIntegrationAction, index, act)
}

// integrationNameCell is the provider's name, styled the way every other
// Configuration row styles its title.
func integrationNameCell(provider string, width int, state State) string {
	style := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	switch {
	case state.Focused:
		style = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case state.Hovered:
		style = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	}
	return style.Render(padDisplay(clampStart(provider, width), width))
}

// integrationFilesCell is the shortest true thing about where this
// integration lives: the first path, with the count of the rest beside it. The
// detail box below names them properly.
func integrationFilesCell(st agentintegration.Status, home string, width int) string {
	if len(st.TargetPaths) == 0 {
		return "none"
	}
	extra := ""
	if len(st.TargetPaths) > 1 {
		extra = fmt.Sprintf("  +%d", len(st.TargetPaths)-1)
	}
	// The end of a path is the part that identifies the file, so the start is
	// what gives way.
	return clampEnd(abbreviateHome(st.TargetPaths[0], home), max(1, width-ansi.StringWidth(extra))) + extra
}

// clampIntegrationCursor keeps the selection on a row that exists.
//
// The cursor is an index into the discovered list rather than into the table,
// so an unsupported provider -- which has no row -- can be named by it after a
// re-read. It is moved to the nearest row that is drawn rather than to the top,
// because a cursor that jumped home on every refresh would be unusable while an
// install was running.
func (m *Model) clampIntegrationCursor(rows []int) {
	state := m.agentIntegrations()
	if len(rows) == 0 {
		state.cursor = 0
		return
	}
	for _, index := range rows {
		if index == state.cursor {
			return
		}
	}
	nearest := rows[0]
	for _, index := range rows {
		if abs(index-state.cursor) < abs(nearest-state.cursor) {
			nearest = index
		}
	}
	state.cursor = nearest
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// buildIntegrationDetail paints the box under the table: what the row the
// cursor is on would not fit.
//
// It is fixed height, and every field is present on every provider, because a
// box that grew and shrank as the cursor moved would reflow the page in exactly
// the way the table stopped doing. A field with nothing to report says so in
// one word rather than disappearing.
//
// The known gaps are named by the command that lists them and nothing else. A
// count of them on the row was a number nobody could act on: "10 known gaps"
// beside claude reads as ten faults in Sidecar, when they are gaps in the
// provider's own hook contract, and the only useful next step was always to go
// and read them.
func (m *Model) buildIntegrationDetail(b *paneBuilder, st agentintegration.Status) {
	width := min(b.inner, MaxRowWidth+integrationTierColumn)
	b.text(integrationDetailRule(st, width))
	for _, field := range integrationDetailFields(st, m.integrations().Env.Home) {
		b.text(integrationDetailLine(field[0], field[1], width))
	}
}

// integrationDetailRule is the box's header: the provider in bold, a rule, and
// its status right-aligned in the status column's own colour, which is how
// every section header in Sidecar is drawn.
func integrationDetailRule(st agentintegration.Status, width int) string {
	label := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true).Render(st.Provider)
	status := integrationStatusStyle(st).Render(integrationStatusWord(st))
	fill := width - RowIndent - ansi.StringWidth(st.Provider) - ansi.StringWidth(integrationStatusWord(st)) - 2
	if fill < 1 {
		fill = 1
	}
	rule := lipgloss.NewStyle().Foreground(styles.BorderNormal).Render(strings.Repeat("─", fill))
	return strings.Repeat(" ", RowIndent) + label + " " + rule + " " + status
}

func integrationDetailLine(label, value string, width int) string {
	room := max(8, width-RowIndent-integrationDetailLabel)
	return strings.Repeat(" ", RowIndent) +
		Muted(padDisplay(label, integrationDetailLabel)) +
		Body(clampStart(value, room))
}

// integrationDetailFields is what the box says, in the order it says it.
func integrationDetailFields(st agentintegration.Status, home string) [][2]string {
	files := "none"
	if len(st.TargetPaths) > 0 {
		shown := make([]string, 0, len(st.TargetPaths))
		for _, path := range st.TargetPaths {
			shown = append(shown, abbreviateHome(path, home))
		}
		files = strings.Join(shown, ", ")
	}

	tier := string(st.EffectiveTier)
	if st.TierReason != "" {
		tier += " (" + string(st.TierReason) + ")"
	}

	agent := "not found on PATH"
	if st.ProviderPath != "" {
		agent = abbreviateHome(st.ProviderPath, home)
		if st.ProviderVersion != "" {
			proved := "outside the proved range"
			if st.ProviderInTestedRange {
				proved = "inside the proved range"
			}
			agent += ", " + st.ProviderVersion + ", " + proved
		}
	}

	report := "nothing reported on this machine yet"
	if r := st.LastReport; r != nil {
		what := string(r.State)
		if r.Kind != agentlifecycle.KindState {
			what = string(r.Kind)
			if r.Outcome != "" {
				what += " " + string(r.Outcome)
			}
		}
		report = fmt.Sprintf("%s on pane %s, %s ago", what, r.PaneID, r.Age)
	}

	note := st.Message
	if note == "" {
		note = "nothing to report"
	}

	return [][2]string{
		{"Files", files},
		{"Tier", tier},
		{"Agent", agent},
		{"Report", report},
		{"Note", strings.TrimSuffix(note, ".")},
		// Never the count. The command is the only actionable thing about them.
		{"Gaps", "sidecar agent integration status " + st.Provider},
	}
}

// summariseIntegrations counts installations against the agents that can
// actually have one.
//
// The denominator is deliberately not the row count. The list also carries
// evaluation records -- agents Sidecar has surveyed and deliberately not built
// an integration for, collapsed into one line under the table -- and counting
// those here would report "0 of 9 installed" on a machine where five of the
// nine can never be anything else, which reads as nine things being broken
// rather than four being available.
func summariseIntegrations(list []agentintegration.Status) string {
	installed, available := 0, 0
	for _, st := range list {
		if st.Status == agentlifecycle.StatusUnsupported {
			continue
		}
		available++
		switch st.Status {
		case agentlifecycle.StatusCurrent, agentlifecycle.StatusOutdated, agentlifecycle.StatusNeedsRepair:
			installed++
		}
	}
	return fmt.Sprintf("%d of %d installed", installed, available)
}

// syncIntegrationCursor keeps the route's own selection and the pane's row
// cursor naming the same provider.
func (m *Model) syncIntegrationCursor() {
	state := m.agentIntegrations()
	prefix := regionIntegrationRow
	if !strings.HasPrefix(m.focusedID, prefix) {
		return
	}
	var index int
	if _, err := fmt.Sscanf(strings.TrimPrefix(m.focusedID, prefix), "%d", &index); err == nil && index >= 0 && index < len(state.list) {
		state.cursor = index
	}
}

// buildAgentIntegrationsRow is the row on the Agents page that leads here.
func (m *Model) buildAgentIntegrationsRow(b *paneBuilder) {
	detail := "Let a supported agent report its own lifecycle, instead of Sidecar reading its screen."
	state := b.declare(regionAgentIntegrations, "", true, func(m *Model) tea.Cmd {
		m.OpenAgentIntegrations()
		return m.drain(nil)
	})
	arrow := Muted("→")
	block := PanelRow("Integrations", "", detail, arrow, b.inner, state)
	lines := strings.Split(block, "\n")
	y := len(b.lines)
	b.lines = append(b.lines, lines...)
	rowWidth := RowWidth(b.inner)
	b.m.mouse.HitMap.AddRect(regionAgentIntegrations, b.originX, 1+y, rowWidth, len(lines), nil)
}

package configui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/configchecks"
)

// Agents is which agent families Sidecar offers when work is created, what each
// one launches, and the state of the project's agent instructions.
//
// The list of families and the meaning of the stored allowlist both come from
// internal/agentcatalog — the same table the creation pickers resolve — so this
// page cannot offer a family creation would not, or claim one is off when
// creation would still show it.

const (
	regionAgentToggle  = "config-agent-toggle-"
	regionAgentCommand = "config-agent-command-"
	regionAgentDocs    = "config-agent-instructions"

	agentCommandWidth = 34
)

// agentsState holds one editor per family's launch command.
type agentsState struct {
	commands map[string]*textinput.Model
}

func (m *Model) agents() *agentsState {
	if m.agentsState == nil {
		m.agentsState = &agentsState{commands: map[string]*textinput.Model{}}
	}
	return m.agentsState
}

// commandInput is the field a family's launch command is edited in, created on
// first use and always primed from the running configuration.
func (m *Model) commandInput(id string) *textinput.Model {
	state := m.agents()
	input, ok := state.commands[id]
	if !ok {
		field := textinput.New()
		field.Prompt = ""
		field.CharLimit = 200
		if family, found := agentcatalog.Find(id); found {
			field.Placeholder = family.Command
		}
		input = &field
		state.commands[id] = input
	}
	return input
}

// agentEnabled reports whether creation would offer a family right now.
func (m *Model) agentEnabled(id string) bool {
	for _, family := range m.offeredAgentFamilies() {
		if family.ID == id {
			return true
		}
	}
	return false
}

// agentCommand is the command a family launches: the configured override, or
// the catalog default.
func (m *Model) agentCommand(id string) (command string, overridden bool) {
	if value, ok := m.Config().Plugins.Workspace.AgentStart[id]; ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed, true
		}
	}
	if family, ok := agentcatalog.Find(id); ok {
		return family.Command, false
	}
	return "", false
}

// toggleAgent writes the allowlist with a family added or removed. The list is
// always rebuilt in catalog order, so the stored setting stays readable and a
// user cannot end up with an order they never chose.
//
// Turning off the last family stores an empty allowlist, which is not "offer
// nothing" — it is the unset state, and creation offers every family. The page
// says so rather than pretending the user disabled everything.
func (m *Model) toggleAgent(id string) tea.Cmd {
	enabled := map[string]bool{}
	for _, family := range m.offeredAgentFamilies() {
		enabled[family.ID] = true
	}
	enabled[id] = !enabled[id]

	var next []string
	for _, family := range agentcatalog.Families() {
		if enabled[family.ID] {
			next = append(next, family.ID)
		}
	}
	notice := "Agent allowlist cleared — creation offers every agent"
	if len(next) > 0 {
		short := id
		if family, ok := agentcatalog.Find(id); ok {
			short = family.Short
		}
		notice = toggleNotice(short, enabled[id])
	}
	// A list naming every family is the same offer as no list at all; storing
	// the shorter form keeps the config honest about the user's intent.
	if len(next) == len(agentcatalog.Families()) {
		next = nil
	}
	return SaveCmd(notice, func() error {
		return config.SaveWorkspace(func(ws *config.WorkspacePluginConfig) { ws.Agents = next })
	})
}

// editAgentCommand opens the launch command for editing. Saving an empty value
// removes the override, which is what returns the family to its default.
func (m *Model) editAgentCommand(id string) {
	input := m.commandInput(id)
	command, overridden := m.agentCommand(id)
	if overridden {
		input.SetValue(command)
	} else {
		input.SetValue("")
	}
	region := regionAgentCommand + id
	m.openEditor(&editorState{
		id:    region,
		input: input,
		submit: func(m *Model) (tea.Cmd, bool) {
			value := strings.TrimSpace(m.commandInput(id).Value())
			notice := "Launch command saved"
			if value == "" {
				notice = "Launch command reset to default"
			}
			return SaveCmd(notice, func() error {
				return config.SaveWorkspace(func(ws *config.WorkspacePluginConfig) {
					if value == "" {
						delete(ws.AgentStart, id)
						if len(ws.AgentStart) == 0 {
							ws.AgentStart = nil
						}
						return
					}
					if ws.AgentStart == nil {
						ws.AgentStart = map[string]string{}
					}
					ws.AgentStart[id] = value
				})
			}), false
		},
		cancel: func(m *Model) {
			m.commandInput(id).SetValue("")
		},
	})
	m.focusControlByID(region)
}

func (m *Model) buildAgents(b *paneBuilder) {
	b.text(PaneTitle(PageTitle(PageAgents)), "")
	b.lead("Choose which agents Sidecar offers when you create work.")

	b.text(SectionHeader("Available agents"))
	for _, family := range agentcatalog.Families() {
		m.buildAgentRow(b, family)
	}

	if len(m.Config().Plugins.Workspace.Agents) == 0 {
		b.blank()
		b.note("No allowlist is set, so creation offers every agent whose command is installed. Turn one off to narrow the list.")
	} else {
		b.blank()
		b.note("An allowlist is set, so creation offers exactly these, installed or not.")
	}
	b.blank()
	b.lead("Enter on a launch command edits it; an empty value restores the default.")

	b.text(SectionHeader("Integrations"))
	m.buildAgentIntegrationsRow(b)

	b.text(SectionHeader("Instructions"))
	m.buildAgentInstructionsRow(b)
}

// buildAgentRow paints one family: its name, a toggle for whether creation
// offers it, and the command it launches. The toggle and the command are two
// controls on one line, each with its own hit region, so a click lands on the
// thing under the pointer.
func (m *Model) buildAgentRow(b *paneBuilder, family agentcatalog.Family) {
	enabled := m.agentEnabled(family.ID)
	command, overridden := m.agentCommand(family.ID)
	editing := m.editingID() == regionAgentCommand+family.ID

	toggleID := regionAgentToggle + family.ID
	toggleState := b.declare(toggleID, "", true, func(m *Model) tea.Cmd {
		return m.toggleAgent(family.ID)
	})
	toggle := Toggle(enabled, toggleState)

	commandID := regionAgentCommand + family.ID
	commandState := b.declare(commandID, "", true, func(m *Model) tea.Cmd {
		m.editAgentCommand(family.ID)
		return nil
	})
	// Two controls share this row's control column, so the command field gets
	// what is left after the toggle rather than the whole column: sizing it as
	// if it were alone is what pushed the row past the pane's right edge.
	fieldWidth := min(agentCommandWidth, max(minControlWidth, b.inner-ControlColumn-ansi.StringWidth(toggle)-2))
	var field string
	if editing {
		commandState.Focused = true
		field = Field(m.commandInput(family.ID), fieldWidth, commandState)
	} else {
		field = StaticField(command, fieldWidth, commandState)
	}

	line := FormRow(family.Short, toggle+"  "+field, toggleState)
	if !editing {
		// Quiet annotations, not part of the control. On a pane too narrow to
		// hold them, dropping them is honest; letting them run off the edge
		// would put an ellipsis where the row's own value should be.
		//
		// This page lists every family Sidecar can start, installed or not,
		// because "what exists" is the question a configuration page answers.
		// The creation picker asks a different one -- "what can I start right
		// now" -- and filters to what is on PATH, so the annotation is what
		// stops the two lists reading as a contradiction.
		var notes []string
		if !overridden {
			notes = append(notes, "default")
		}
		if agentcatalog.InstalledKnown() && !agentcatalog.Installed(family.ID) {
			notes = append(notes, "not installed")
		}
		if len(notes) > 0 {
			suffix := "  " + Muted(strings.Join(notes, " · "))
			if ansi.StringWidth(line)+ansi.StringWidth(suffix) <= b.inner {
				line += suffix
			}
		}
	}
	y := len(b.lines)
	b.lines = append(b.lines, line)

	x := b.originX + ControlColumn
	b.m.mouse.HitMap.AddRect(toggleID, x, 1+y, ansi.StringWidth(toggle), 1, nil)
	x += ansi.StringWidth(toggle) + 2
	b.m.mouse.HitMap.AddRect(commandID, x, 1+y, ansi.StringWidth(field), 1, nil)

	if editing {
		b.help("Empty resets to the default: " + family.Command)
	}
}

// buildAgentInstructionsRow shows the cached agent-instructions check and the
// way into its repair. The route is the same one Diagnostics opens, so the
// guidance, the confirmation, and the write have exactly one implementation.
func (m *Model) buildAgentInstructionsRow(b *paneBuilder) {
	if !m.ChecksReady() {
		b.note("Checking the project's agent instructions…")
		return
	}
	result := m.result(configchecks.CheckAgentInstructions)
	summary := result.Summary
	if summary == "" {
		summary = "Agent instructions"
	}
	b.text(StatusRow(result.OK, summary, "", "", b.inner, State{}))
	b.row(regionAgentDocs, "", func(m *Model) tea.Cmd {
		m.OpenAgentInstructions()
		return nil
	}, func(s State) string {
		return ButtonRow(Button("Enter  View guidance and repairs", !result.OK, s))
	})
}

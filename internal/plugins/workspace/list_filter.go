package workspace

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/workspacelist"
)

// The project Workspaces sidebar and the global Workspaces browser share one
// definition of filtering: the same `/` entry, the same multi-field matcher,
// the same counts and no-match row, and the same escape/enter behaviour. That
// shared definition lives in internal/workspacelist; this file is the project
// side's projection into it.
//
// Filtering is deliberately explicit. `/` enters it so the project list keeps
// its printable commands (n, D, p) as they are, and the filter reports a
// text-input context while it has focus so sidecar's own shortcuts cannot take
// characters or pastes out of a query.

// filterActive reports that a query is narrowing the list. Everything about
// the non-filtered journey — navigation, hit regions, scrollbar, commands —
// is reached through the same code paths whether or not this is true; an
// inactive filter simply makes every row visible.
func (p *Plugin) filterActive() bool { return p.listFilter.Active() }

func (p *Plugin) filterFocused() bool { return p.listFilter.Focused() }

// focusListFilter is the `/` entry point.
func (p *Plugin) focusListFilter() {
	p.listFilter.Focus()
}

// projectDisplayName is the textual project identity rows can match on. The
// project sidebar shows one project, but the field still participates so a
// query typed in one list behaves the same in the other.
func (p *Plugin) projectDisplayName() string {
	if p.ctx == nil || p.ctx.ProjectRoot == "" {
		return ""
	}
	return filepath.Base(p.ctx.ProjectRoot)
}

// worktreeFilterFields is the exact promised field set for a worktree row.
func (p *Plugin) worktreeFilterFields(wt *Worktree) []string {
	if wt == nil {
		return nil
	}
	provider := ""
	if wt.Agent != nil {
		provider = string(wt.Agent.Type)
	} else if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
		provider = string(wt.ChosenAgentType)
	}
	status := agentStatusPresentation(wt).Label
	if wt.IsMain {
		status += " main"
	}
	return []string{wt.Name, p.projectDisplayName(), wt.Branch, wt.TaskID, wt.TaskTitle, provider, status}
}

// shellFilterFields is the exact promised field set for a shell row.
func (p *Plugin) shellFilterFields(shell *ShellSession) []string {
	if shell == nil {
		return nil
	}
	provider := string(liveShellProvider(shell))
	if provider == "" && shell.IsOrphaned && shell.ChosenAgent != "" && shell.ChosenAgent != AgentNone {
		provider = string(shell.ChosenAgent)
	}
	status := "no session"
	switch {
	case shell.IsOrphaned:
		status = "offline"
	case shell.Agent != nil:
		status = "live"
	}
	return []string{shell.Name, p.projectDisplayName(), shell.TmuxName, provider, status, "shell"}
}

// visibleShellIndices lists the shell indices the sidebar draws, in order.
// With no query it is every shell, which is why the unfiltered journey is
// byte-identical through the same loop.
func (p *Plugin) visibleShellIndices() []int {
	indices := make([]int, 0, len(p.shells))
	query := p.listFilter.Query()
	for i, shell := range p.shells {
		if query == "" || workspacelist.MatchFields(query, p.shellFilterFields(shell)...) {
			indices = append(indices, i)
		}
	}
	return indices
}

// listedWorktree reports whether a worktree is offered as a row at all.
//
// The main worktree is not. It is the project's primary checkout rather than a
// workspace: it cannot be created, deleted, merged, or pushed from this list,
// and selecting it replaces the preview with a static explainer instead of a
// terminal. Rendered with the same marker, glyph, and two-line grammar as its
// neighbours, it read as one more workspace that happened to be inert — the one
// row in the list that answers nothing you can act on.
//
// The exception is a main checkout that is hosting shells. That happens when
// Sidecar is running from inside a worktree, so the main checkout's shells are
// nested under its row rather than in the top Shells section. Hiding the row
// there would take live sessions off the surface entirely, which is a worse
// outcome than an odd-looking parent. In the ordinary case — Sidecar running in
// the main checkout, its shells already in the Shells section — the row is
// simply gone.
func (p *Plugin) listedWorktree(wt *Worktree) bool {
	if wt == nil {
		return false
	}
	return !wt.IsMain || p.hostsNestedShells(wt)
}

// hostsNestedShells asks whether a worktree has shells living in it at all,
// ignoring the filter. Whether a row is ever offered is a property of the
// project; whether it is showing right now is a property of the query. Reading
// the filtered list here made the "N of M" denominator shrink as the user
// typed, so the total they were being measured against moved under them.
func (p *Plugin) hostsNestedShells(wt *Worktree) bool {
	if wt == nil || p.isCurrentWorkDir(wt.Path) {
		return false
	}
	return len(p.nestedByWorkDir[filepath.Clean(wt.Path)]) > 0
}

// visibleWorktreeIndices lists the worktree indices the sidebar draws.
func (p *Plugin) visibleWorktreeIndices() []int {
	indices := make([]int, 0, len(p.worktrees))
	query := p.listFilter.Query()
	for i, wt := range p.worktrees {
		if !p.listedWorktree(wt) {
			continue
		}
		if query == "" || workspacelist.MatchFields(query, p.worktreeFilterFields(wt)...) || len(p.visibleNestedShells(wt)) > 0 {
			indices = append(indices, i)
		}
	}
	return indices
}

func (p *Plugin) nestedShellTotal() int {
	n := 0
	for _, wt := range p.worktrees {
		if p.isCurrentWorkDir(wt.Path) {
			continue
		}
		n += len(p.nestedByWorkDir[filepath.Clean(wt.Path)])
	}
	return n
}

func (p *Plugin) visibleNestedCount() int {
	n := 0
	for _, i := range p.visibleWorktreeIndices() {
		n += len(p.visibleNestedShells(p.worktrees[i]))
	}
	return n
}

// filterCounts is the "N of M" the filter row reports. M counts the rows the
// list would show with no query, so a worktree the list never offers — the main
// checkout — is absent from both halves rather than inflating the total the
// user is measured against.
func (p *Plugin) filterCounts() (matched, total int) {
	listable := 0
	for _, wt := range p.worktrees {
		if p.listedWorktree(wt) {
			listable++
		}
	}
	return len(p.visibleShellIndices()) + len(p.visibleWorktreeIndices()) + p.visibleNestedCount(),
		len(p.shells) + listable + p.nestedShellTotal()
}

// selectionVisible reports that the current selection survives the query.
func (p *Plugin) selectionVisible() bool {
	if p.shellSelected {
		return containsIndex(p.visibleShellIndices(), p.selectedShellIdx)
	}
	if p.selectedNestedTmux != "" {
		for _, i := range p.visibleWorktreeIndices() {
			for _, shell := range p.visibleNestedShells(p.worktrees[i]) {
				if shell.TmuxName == p.selectedNestedTmux {
					return true
				}
			}
		}
		return false
	}
	return containsIndex(p.visibleWorktreeIndices(), p.selectedIdx)
}

func containsIndex(indices []int, want int) bool {
	for _, index := range indices {
		if index == want {
			return true
		}
	}
	return false
}

// clampSelectionToFilter keeps the cursor on a row the user can see. Selection
// is preserved whenever the selected identity still matches; only a selection
// the query removed moves, and then to the first visible row.
//
// Scroll is re-clamped either way: the offset is a position into the filtered
// projection, so a query typed while the list is scrolled has to bring the
// offset back inside the rows that survived it, even when the selection did.
func (p *Plugin) clampSelectionToFilter() tea.Cmd {
	if p.selectionVisible() {
		p.ensureVisible()
		return nil
	}
	items := p.visibleSidebarItems()
	if len(items) == 0 {
		return nil
	}
	p.selectSidebarItem(items[0])
	p.exitInteractiveMode()
	p.saveSelectionState()
	p.ensureVisible()
	return p.loadSelectedContent()
}

// handleFilterKey routes one key while the filter owns the keyboard. Arrow and
// ctrl+n/ctrl+p navigation stays live, so a user can type, arrow onto a match,
// and press enter without leaving the query.
func (p *Plugin) handleFilterKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if !p.filterFocused() {
		return false, nil
	}
	switch p.listFilter.HandleKey(msg) {
	case workspacelist.KeyIgnored:
		switch msg.String() {
		case "up", "ctrl+p":
			p.moveCursor(-1)
			return true, p.loadSelectedContent()
		case "down", "ctrl+n":
			p.moveCursor(1)
			return true, p.loadSelectedContent()
		}
		// Everything else is swallowed: the filter is a text input, and a stray
		// key must not fall through to a destructive project command.
		return true, nil
	case workspacelist.KeyAccept:
		// Enter leaves the focused item selected and returns to list navigation
		// with the query still narrowing the list.
		return true, nil
	default:
		return true, p.clampSelectionToFilter()
	}
}

// handleFilterPaste puts pasted text into a focused query at the caret. The
// field's own sanitizer flattens newlines, because a query bar is one line.
func (p *Plugin) handleFilterPaste(text string) (bool, tea.Cmd) {
	if !p.filterFocused() || text == "" {
		return false, nil
	}
	p.listFilter.HandlePaste(tea.PasteMsg{Content: text})
	return true, p.clampSelectionToFilter()
}

// resetListFilter drops query and focus. Filter state is in-memory and per
// consumer, so a plugin reinit starts clean rather than restoring a query the
// user cannot see the origin of.
func (p *Plugin) resetListFilter() { p.listFilter.Reset() }

// selectFirstVisible / selectLastVisible are the filtered forms of g and G.
func (p *Plugin) selectFirstVisible() {
	items := p.visibleSidebarItems()
	if len(items) == 0 {
		return
	}
	p.selectSidebarItem(items[0])
	// g is an absolute jump, so it owns the viewport even when the selection
	// was already first: Model.Top resets scroll and drops free-scroll together.
	p.scrollOffset = 0
	p.freeScroll = false
	p.exitInteractiveMode()
	p.saveSelectionState()
}

func (p *Plugin) selectLastVisible() {
	items := p.visibleSidebarItems()
	if len(items) == 0 {
		return
	}
	p.selectSidebarItem(items[len(items)-1])
	p.exitInteractiveMode()
	p.saveSelectionState()
	p.ensureVisible()
}

package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/workspacelist"
)

type sidebarNavKind int

const (
	navKindShell sidebarNavKind = iota
	navKindWorktree
	navKindNestedShell
)

type sidebarNavItem struct {
	kind        sidebarNavKind
	shellIdx    int
	worktreeIdx int
	shell       *ShellSession
}

// nestedShellHit is the hit-map payload for a sibling shell nested under a
// worktree. Top-section shells keep the existing negative-int encoding.
type nestedShellHit struct {
	TmuxName string
}

func (p *Plugin) visibleNestedShells(wt *Worktree) []*ShellSession {
	if wt == nil || p.isCurrentWorkDir(wt.Path) {
		return nil
	}
	shells := p.nestedByWorkDir[filepath.Clean(wt.Path)]
	query := p.listFilter.Query()
	if query == "" {
		return shells
	}
	out := make([]*ShellSession, 0, len(shells))
	for _, shell := range shells {
		if workspacelist.MatchFields(query, p.shellFilterFields(shell)...) {
			out = append(out, shell)
		}
	}
	return out
}

// sidebarNavSection is one headed run of navigable items. It is the single
// description of what the sidebar contains and in what order.
type sidebarNavSection struct {
	title  string
	action *workspacelist.SidebarAction
	items  []sidebarNavItem
}

// sidebarNavSections is the one place that decides what the sidebar shows and
// in what order.
//
// Keyboard navigation and rendering used to derive that order independently and
// happened to agree, which was survivable only while the order was hard-coded
// as "shells, then worktrees with their nests". It stops being survivable the
// moment the order becomes a user choice: j/k would walk one sequence while the
// eye read another. Both now consume this.
func (p *Plugin) sidebarNavSections() []sidebarNavSection {
	if p.listSort != workspacelist.SortManual {
		return p.sortedNavSections()
	}
	return p.manualNavSections()
}

// manualNavSections is the structural order: shells the user is working in,
// then worktrees, each followed by the shells living inside it. It is the shape
// of the project rather than a judgement about it, which is why it stays the
// default and why it is the only mode where a shell is drawn as a child.
func (p *Plugin) manualNavSections() []sidebarNavSection {
	shells := sidebarNavSection{title: "Shells"}
	for _, index := range p.visibleShellIndices() {
		shells.items = append(shells.items, sidebarNavItem{kind: navKindShell, shellIdx: index})
	}
	if len(shells.items) > 0 {
		shells.action = &workspacelist.SidebarAction{ID: regionShellsPlusButton, Label: "+", Hovered: p.hoverShellsPlusButton}
	}

	worktrees := sidebarNavSection{title: "Worktrees"}
	for _, index := range p.visibleWorktreeIndices() {
		worktrees.items = append(worktrees.items, sidebarNavItem{kind: navKindWorktree, worktreeIdx: index})
		for _, shell := range p.visibleNestedShells(p.worktrees[index]) {
			worktrees.items = append(worktrees.items, sidebarNavItem{kind: navKindNestedShell, worktreeIdx: index, shell: shell})
		}
	}
	// With no Shells section above it this heading lands one row under the panel
	// header, whose "New" creates the same thing the "+" would.
	if len(worktrees.items) > 0 && len(shells.items) > 0 {
		worktrees.action = &workspacelist.SidebarAction{ID: regionWorkspacesPlusButton, Label: "+", Hovered: p.hoverWorkspacesPlusButton}
	}

	sections := make([]sidebarNavSection, 0, 2)
	for _, section := range []sidebarNavSection{shells, worktrees} {
		if len(section.items) > 0 {
			sections = append(sections, section)
		}
	}
	return sections
}

func (p *Plugin) visibleSidebarItems() []sidebarNavItem {
	sections := p.sidebarNavSections()
	items := make([]sidebarNavItem, 0, len(p.shells)+len(p.worktrees)+p.nestedShellTotal())
	for _, section := range sections {
		items = append(items, section.items...)
	}
	return items
}

// rowID is the stable identity the shared list selects and hit-tests by.
func (p *Plugin) rowID(item sidebarNavItem) string {
	switch item.kind {
	case navKindShell:
		shell := p.shells[item.shellIdx]
		if shell.TmuxName == "" {
			return fmt.Sprintf("shell:%s:%d", shell.Name, item.shellIdx)
		}
		return "shell:" + shell.TmuxName
	case navKindNestedShell:
		if item.shell.TmuxName == "" {
			return fmt.Sprintf("nested:%s:%s", p.worktrees[item.worktreeIdx].IdentityKey(), item.shell.Name)
		}
		return "nested:" + item.shell.TmuxName
	default:
		return "worktree:" + p.worktrees[item.worktreeIdx].IdentityKey()
	}
}

// rowData is the caller-owned payload the hit map hands back on a click. The
// encodings are historical and unchanged: top shells are a negative index,
// worktrees a plain index, nested shells their tmux name.
func (p *Plugin) rowData(item sidebarNavItem) any {
	switch item.kind {
	case navKindShell:
		return -(item.shellIdx + 1)
	case navKindNestedShell:
		return nestedShellHit{TmuxName: item.shell.TmuxName}
	default:
		return item.worktreeIdx
	}
}

func (p *Plugin) renderNavItem(item sidebarNavItem, width int, selected bool) []string {
	switch item.kind {
	case navKindShell:
		return []string{p.renderShellEntryForSession(p.shells[item.shellIdx], selected, width)}
	case navKindNestedShell:
		// Indented under its worktree only while the order is structural. A
		// computed order has flattened the tree, so the row draws as a peer
		// carrying its worktree as context instead.
		if p.listSort != workspacelist.SortManual {
			return []string{p.renderPeerShellEntry(item.shell, p.worktrees[item.worktreeIdx], selected, width)}
		}
		return []string{p.renderNestedShellEntry(item.shell, selected, width)}
	default:
		return []string{p.renderWorktreeSidebarItem(p.worktrees[item.worktreeIdx], selected, width)}
	}
}

// selectedRowID is the identity of the current selection, or "" when nothing
// visible is selected.
func (p *Plugin) selectedRowID() string {
	for _, item := range p.visibleSidebarItems() {
		if p.sidebarItemSelected(item) {
			return p.rowID(item)
		}
	}
	return ""
}

func (p *Plugin) selectSidebarItem(item sidebarNavItem) {
	switch item.kind {
	case navKindShell:
		p.selectTopShellAt(item.shellIdx)
	case navKindWorktree:
		p.selectWorktreeAt(item.worktreeIdx)
	case navKindNestedShell:
		tmuxName := ""
		if item.shell != nil {
			tmuxName = item.shell.TmuxName
		}
		p.selectNestedShell(item.worktreeIdx, tmuxName)
	}
}

func (p *Plugin) sidebarItemSelected(item sidebarNavItem) bool {
	switch item.kind {
	case navKindShell:
		return p.shellSelected && p.selectedShellIdx == item.shellIdx
	case navKindWorktree:
		return !p.shellSelected && p.selectedNestedTmux == "" && p.selectedIdx == item.worktreeIdx
	case navKindNestedShell:
		return !p.shellSelected && item.shell != nil && p.selectedNestedTmux == item.shell.TmuxName
	default:
		return false
	}
}

// toastDuration is how long a toast stays up. It is longer than the attach
// flash because a toast is the only place a refused action explains itself: the
// window it appears in is the narrow one that caused the refusal, so a reader
// needs long enough to find it as well as to read it.
const toastDuration = 4 * time.Second

// fitToast picks the longest form of a toast message that survives the sidebar
// it is drawn in. The sidebar of the window narrow enough to refuse a split is
// itself narrow — seventeen columns at 60x24 — and a refusal truncated to
// "⚠ Document pan…" is a message that never reaches the user it is for.
func fitToast(msg string, width int) string {
	candidates := []string{msg}
	if head, _, ok := strings.Cut(msg, ";"); ok {
		candidates = append(candidates, strings.TrimSpace(head))
	}
	switch {
	case strings.Contains(msg, "wider"):
		candidates = append(candidates, "Needs a wider window", "Too narrow")
	case strings.Contains(msg, "taller"):
		candidates = append(candidates, "Needs a taller window", "Too short")
	}
	for _, candidate := range candidates {
		if ansi.StringWidth(candidate) <= width {
			return candidate
		}
	}
	return ansi.Truncate(candidates[len(candidates)-1], width, "…")
}

// renderSidebarContent projects project-owned shells, worktrees and optional
// lifecycle actions into the same presentation component used by global
// Workspaces. Nothing in workspacelist can create, attach, delete or load a
// preview; those remain callbacks reached through the typed regions below.
func (p *Plugin) renderSidebarContent(width, height int) string {
	terminalperf.Record(terminalperf.ProjectSidebarRendered)
	warnings := make([]string, 0, len(p.deleteWarnings)+1)
	warningStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	for _, warning := range p.deleteWarnings {
		warnings = append(warnings, warningStyle.Render("⚠ "+ansi.Truncate(warning, max(1, width-2), "…")))
	}
	if p.toastMessage != "" && !p.toastTime.IsZero() && time.Since(p.toastTime) < toastDuration {
		warnings = append(warnings, warningStyle.Bold(true).Render("⚠ "+fitToast(p.toastMessage, max(1, width-2))))
	}

	matched, total := p.filterCounts()
	navSections := p.sidebarNavSections()
	sections := make([]workspacelist.SidebarSection, 0, len(navSections))
	rowCount := 0
	for _, nav := range navSections {
		section := workspacelist.SidebarSection{Title: nav.title, Count: len(nav.items), Action: nav.action}
		for _, item := range nav.items {
			item := item
			section.Rows = append(section.Rows, workspacelist.SidebarRow{
				ID: p.rowID(item), Data: p.rowData(item),
				Render: func(rowWidth int, selected, _ bool) []string {
					return p.renderNavItem(item, rowWidth, selected)
				},
			})
		}
		rowCount += len(section.Rows)
		sections = append(sections, section)
	}

	selectedID := p.selectedRowID()

	empty := []string(nil)
	emptyActionID, emptyActionLine := "", 0
	if rowCount == 0 {
		if p.filterActive() {
			empty = []string{workspacelist.NoMatchRow(max(1, width-1), p.listFilter.Query())}
		} else if prompt, blocked := p.setupPromptFor(); blocked {
			// Nothing can be created here until a prerequisite is in place, so
			// the empty state routes to where that is repaired rather than
			// repeating advice that cannot work.
			empty, emptyActionLine = p.setupPromptLines(prompt, max(1, width-1))
			emptyActionID = regionOpenSetupButton
		} else {
			// Every project has a main checkout and the list does not offer it,
			// so counting raw worktrees here left a fresh clone with an empty
			// sidebar and no word about what to do next. The question is
			// whether there is anything to show, not whether Git found
			// something.
			empty, emptyActionLine = firstRunEmptyLines(max(1, width-1))
			emptyActionID = regionOpenCreateButton
		}
	}

	filterLine, filterClear := p.listFilter.RenderRow(width, matched, total)
	rendered := workspacelist.RenderSidebar(workspacelist.SidebarOptions{
		Width: width, Height: height, Title: "Workspaces", Focused: p.activePane == PaneSidebar,
		SelectedID: selectedID, ScrollOffset: p.scrollOffset,
		// One header grammar with the global list: the order the list is in,
		// then the button that adds to it. "New" became "+" because the section
		// headings already offer "+" for the same job — three words for one
		// action was the noisiest thing in this header.
		HeaderMeta:   &workspacelist.SidebarAction{ID: regionListSortButton, Label: workspacelist.SortPillLabel(p.listSort), Hovered: p.hoverSortButton},
		HeaderAction: &workspacelist.SidebarAction{ID: regionCreateWorktreeButton, Label: "+", Hovered: p.hoverNewButton},
		// The project list's bar is live like global Sessions': its thumb/track
		// regions ride along with every content region, and an offset a pointer
		// gesture chose is honored rather than re-derived from the selection.
		InteractiveScrollbar: true,
		FreeScroll:           p.freeScroll,
		ScrollbarHover:       p.sidebarBar.hover && !p.sidebarBar.gesture.Active(),
		ScrollbarDrag:        p.sidebarBar.gesture.Active(),
		PrefixLines:          warnings, FilterActive: p.filterActive(),
		FilterLine: filterLine, FilterClear: filterClear,
		Sections: sections, EmptyLines: empty,
		EmptyActionID: emptyActionID, EmptyActionLine: emptyActionLine,
	})
	p.scrollOffset, p.visibleCount = rendered.ScrollOffset, rendered.VisibleRows
	for _, region := range rendered.Regions {
		id, data := string(region.Kind), region.Data
		switch region.Kind {
		// The sort pill is a header control like any other: it carries the
		// caller's own region ID, not the shared component's kind. Falling
		// through to the kind left the pill drawn, hit-tested, and wired to a
		// handler that could never be reached — a button that looks pressable
		// and does nothing.
		case workspacelist.RegionHeaderAction, workspacelist.RegionSectionAction, workspacelist.RegionSort, workspacelist.RegionEmptyAction:
			id = region.ID
		case workspacelist.RegionRow:
			id = regionWorktreeItem
		case workspacelist.RegionFilter:
			id = regionListFilter
		case workspacelist.RegionFilterClear:
			id = regionListFilterClear
		}
		p.mouseHandler.HitMap.AddRect(id, sidebarContentX+region.X, sidebarContentY+region.Y, region.W, region.H, data)
	}
	// The bar snapshot travels with the same origin the regions were just
	// registered at, so a press maps back onto what was actually drawn.
	p.sidebarBar.bar, p.sidebarBar.originY = rendered.Scrollbar, sidebarContentY
	return rendered.View
}

// Panel content begins one row and two columns inside RenderPanel.
const (
	sidebarContentX = 2
	sidebarContentY = 1
)

// sidebarBarState is the project sidebar's scrollbar pointer state. The bar
// snapshot is what the last render reported; originY turns the snapshot's
// content-local geometry into the coordinates the mouse handler answers; the
// gesture keeps the press-time mapping so re-renders cannot shift it under the
// pointer; hover feeds the bar's emphasis back into the draw.
type sidebarBarState struct {
	bar     workspacelist.SidebarScrollbar
	originY int
	hover   bool
	gesture workspacelist.ScrollGesture
}

func (p *Plugin) sharedSidebarRowCount() int {
	return len(p.visibleSidebarItems())
}

func (p *Plugin) sharedSidebarSelectionIndex() int {
	items := p.visibleSidebarItems()
	for i, item := range items {
		if p.sidebarItemSelected(item) {
			return i
		}
	}
	return -1
}

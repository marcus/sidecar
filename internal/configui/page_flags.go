package configui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/mouse"
)

const regionFlag = "config-flag-"

// Feature Flags is the whole flag registry, one switch per flag, in the shape
// Panels & Integrations uses for surfaces. It is a page of its own rather than
// a section on Advanced because the registry grows independently of configui;
// the page scrolls its detail pane to keep the focused flag visible.
//
// The list is derived from internal/features rather than curated here. A
// hand-picked list is how tmux_interactive_input, tmux_inline_edit,
// files_auto_refresh, plugin_content_panes and terminal_resource_providers all
// ended up settable only by editing config.json.

func (m *Model) buildFlags(b *paneBuilder) {
	b.text(PaneTitle(PageTitle(PageFlags)), "")
	// Above the list, not below it. Appended after twelve rows this was the
	// first thing the pane's height clamp cut, so a user toggling a
	// startup-scoped flag on a normal-sized terminal saw the success toast and
	// never the sentence telling them it needs a restart.
	if m.restartNote != "" {
		b.blank()
		b.note(m.restartNote)
	}
	// One blank between the title and the first row, not two. PaneTitle already
	// emits its own trailing blank, leaving more of the scrolling window for
	// actual settings on an ordinary terminal.
	//
	// Flags another page owns sort last, so the switches that work here are not
	// interleaved with rows that only point elsewhere.
	items := flagPagePreviews()
	ownedAt := len(items)
	for i, item := range items {
		if item.owner != "" {
			ownedAt = i
			break
		}
	}

	// No blank line between rows: the scrolling window should spend its height
	// on settings. Each row carries its explanation directly beneath its name.
	for _, item := range items[:ownedAt] {
		m.previewRow(b, item)
	}

	if ownedAt < len(items) {
		// SectionHeader's own leading blank is dropped here, for the same
		// reason as above: the header text separates the groups on its own,
		// and the blank costs a row this page does not have.
		b.text(strings.TrimPrefix(SectionHeader("Set on other pages"), "\n"))
		for _, item := range items[ownedAt:] {
			m.previewRow(b, item)
		}
	}
}

// flagPagePreviews is the page's cursor order. Flags owned by another settings
// page remain at the end as one read-only group.
func flagPagePreviews() []preview {
	items := previews()
	ordered := make([]preview, 0, len(items))
	for _, item := range items {
		if item.owner == "" {
			ordered = append(ordered, item)
		}
	}
	for _, item := range items {
		if item.owner != "" {
			ordered = append(ordered, item)
		}
	}
	return ordered
}

// preview is one feature flag offered on Feature Flags.
type preview struct {
	// flag is the feature name in features.flags.
	flag string
	// label is what the user reads.
	label string
	// help is the input-aligned explanation under the control.
	help string
	// restart marks a flag whose consumer reads it once at startup, so the
	// change is real but not visible until Sidecar is restarted. It is set per
	// flag from what actually consumes it, never as blanket caution.
	restart bool
	// note is an honest scope line for a flag that applies live but not
	// retroactively.
	note string
	// owner names the page that already offers this flag as a first-class
	// setting. A flag with an owner is listed here read-only, with a jump to
	// the control that owns it, because a second toggle is how two surfaces
	// start disagreeing: Panels pairs conversations_plugin with the plugin's
	// own enabled key, and a raw flag switch here would set one and not the
	// other. This is the rule FocusNotesPreference already follows in the
	// other direction.
	owner PageID
	// ownerControl is the control id on that page to put the cursor on.
	ownerControl string
	// reads answers what this row should show, when the raw flag is not it.
	// Panels' Conversations switch is the flag AND the plugin's own enabled
	// key, and toggleConversations clears only the plugin key on the way off —
	// so the flag stays true and a row reading it alone renders ON next to a
	// Panels page rendering OFF. The row is labelled with the panel's name, so
	// it owes the panel's answer. Nil means the flag itself.
	reads func(*Model) bool
}

// state is what the row reports: the owning surface's answer where one exists,
// the flag otherwise.
func (p preview) state(m *Model) bool {
	if p.reads != nil {
		return p.reads(m)
	}
	return m.flagEnabled(p.flag)
}

// previewCopy is the hand-written presentation for a flag, keyed by flag name.
// A flag absent from this map still appears on the page — see previews — using
// the registry's own name and description, so registering a feature is all it
// takes to make it reachable. The entries here are the ones worth saying more
// about than the registry does.
//
// Restart accuracy, checked against each flag's consumers rather than applied
// as blanket caution:
//   - cross_project_overview is read once in app.New to decide whether the
//     cross-project surface is constructed at all → restart.
//   - terminal_resource_providers gates a describe pass that runs once after
//     the first ready frame (app.describeResourceProvidersCmd) → restart.
//   - plugin_protocol gates the same describe pass for plugins.external
//     entries, which is read once on the same latch → restart.
//   - workspace_doc_panes is checked live wherever a pane or a diff is opened
//     (workspace.Plugin, internal/overview) → immediate.
//   - tmux_full_attach is checked live, but a terminal resolves its chords when
//     it is created (app.TerminalConfig) → immediate, next terminal.
//   - workspace_terminal_panel is checked live every time the split panel is
//     shown or toggled → immediate.
//   - tmux_interactive_input, tmux_inline_edit, files_auto_refresh and
//     plugin_content_panes are all read at the point of use → immediate.
var previewCopy = map[string]preview{
	features.CrossProjectOverview.Name: {
		label:   "Cross-project Activity",
		help:    "Show every configured project's workspaces in Activity.",
		restart: true,
	},
	features.WorkspaceDocPanes.Name: {
		label: "Document panes",
		help:  "Open files, issues, and diffs in panes beside the workspace.",
	},
	features.TmuxFullAttach.Name: {
		label: "Full tmux attach",
		help:  "Hand the terminal to tmux's own client and native shortcuts.",
		note:  "Applies to terminals opened after the change, and unlocks the attach chord on Terminal.",
	},
	features.WorkspaceTerminalPanel.Name: {
		label: "Split workspace terminal",
		help:  "Show a live terminal beside the workspace list.",
	},
	features.TmuxInteractiveInput.Name: {
		label: "Type into terminals",
		help:  "Type into panes. Off makes every terminal read-only.",
	},
	features.TmuxInlineEdit.Name: {
		label: "Inline file editing",
		help:  "Edit a file in the preview pane, without an external editor.",
	},
	features.FilesAutoRefresh.Name: {
		label: "Auto-refresh files",
		help:  "Refresh the file tree when a watched directory changes on disk.",
	},
	features.PluginContentPanes.Name: {
		label: "Plugin content panes",
		help:  "Open a content pane beside Files, Git, Notes, and issue views.",
	},
	features.PaneMove.Name: {
		label: "Move panes",
		help:  "Reposition an open pane with the keyboard or its header control.",
	},
	features.TerminalResourceProviders.Name: {
		label:   "Terminal resource providers",
		help:    "Turn terminal text into openable links via external providers.",
		restart: true,
		note:    "Providers are described once at startup; turning this off stops every provider process.",
	},
	features.PluginProtocol.Name: {
		label:   "Plugin protocol",
		help:    "Host external plugins configured under plugins.external.",
		restart: true,
		note:    "Plugins are described once at startup; turning this off stops every plugin process.",
	},
	features.SidecarRemoteHosts.Name: {
		label: "Remote hosts",
		help:  "Watch other machines running Sidecar, over SSH.",
		// Not restart-scoped. hosts.FromConfig asks features.IsEnabled every
		// time the registry is reconciled, and every save reconciles it
		// (app.applyConfigSaved → overview.SyncHosts), so turning this on
		// connects to the registered machines without a restart. The row said
		// otherwise while the registry was hand-written and nothing reconciled
		// it on a save.
		note: "Register machines on Remote Hosts, or with `sidecar host add`.",
	},
	features.NotesPlugin.Name: {
		label:        "Notes panel",
		help:         "Keep quick notes in their own Sidecar panel.",
		owner:        PagePanels,
		ownerControl: regionPanel + panelIDNotes,
		reads:        panelReads(panelIDNotes, features.NotesPlugin.Name),
	},
	features.TasksPlugin.Name: {
		label:        "Tasks panel",
		help:         "Show the embedded Tasks tab for your task list.",
		owner:        PagePanels,
		ownerControl: regionPanel + panelIDTasks,
		reads:        panelReads(panelIDTasks, features.TasksPlugin.Name),
	},
	features.ConversationsPlugin.Name: {
		label:        conversationsFlagLabel,
		help:         "Browse past agent sessions from Claude, Codex, and others.",
		owner:        PagePanels,
		ownerControl: regionPanel + panelIDConversations,
		reads:        (*Model).conversationsOn,
	},
}

// panelReads makes a row report the panel's own answer rather than the flag
// behind it.
//
// notes_plugin and tasks_plugin are read-only aliases: the config key decides
// once it is written, and the flag answers only while it is absent. A row
// reading the flag alone therefore renders ON next to a Panels page rendering
// OFF, which is what it did. It reports the preference, exactly as Panels does,
// so the two pages agree even when a dependency (td, for Notes) is off.
//
// The flag is the fallback for a Model with no descriptor catalog, where the
// panel's own answer is not reachable.
func panelReads(panelID, flag string) func(*Model) bool {
	return func(m *Model) bool {
		d, ok := m.panelDescriptor(panelID)
		if !ok {
			return m.flagEnabled(flag)
		}
		return m.panelOn(d)
	}
}

// scrollFlagsPage keeps the focused flag inside the detail pane while retaining
// every registered control in keyboard order. The page is the one general
// settings list whose length is intentionally registry-driven; applying the
// window here avoids changing the geometry of unrelated configuration pages.
//
// Regions are built against the full list, then clipped and translated with the
// same offset as the painted lines. A hidden row therefore cannot leave a live
// mouse target behind, and the row that becomes visible is clickable in the
// cell where it is actually drawn.
func (m *Model) scrollFlagsPage(lines []string, height int) []string {
	if height <= 0 {
		m.flagsScroll = 0
		return nil
	}
	maxScroll := max(0, len(lines)-height)
	if !m.detailOwnsKeys() {
		m.flagsScroll = 0
	}
	m.flagsScroll = min(max(0, m.flagsScroll), maxScroll)

	if index := m.cursorControl(); index >= 0 && index < len(m.controls) {
		focusedID := m.controls[index].id
		for _, region := range m.mouse.HitMap.Regions() {
			if region.ID != focusedID {
				continue
			}
			top := region.Rect.Y - 1
			bottom := top + region.Rect.H
			if top < m.flagsScroll {
				m.flagsScroll = top
			} else if bottom > m.flagsScroll+height {
				m.flagsScroll = bottom - height
			}
			m.flagsScroll = min(max(0, m.flagsScroll), maxScroll)
			break
		}
	}

	start := m.flagsScroll
	end := min(len(lines), start+height)
	m.translateFlagRegions(start, end)
	return lines[start:end]
}

func (m *Model) translateFlagRegions(start, end int) {
	regions := m.mouse.HitMap.Regions()
	m.mouse.HitMap.Clear()
	for _, region := range regions {
		if !strings.HasPrefix(region.ID, regionFlag) {
			m.mouse.HitMap.Add(region.ID, region.Rect, region.Data)
			continue
		}
		top := region.Rect.Y - 1
		bottom := top + region.Rect.H
		visibleTop := max(top, start)
		visibleBottom := min(bottom, end)
		if visibleTop >= visibleBottom {
			continue
		}
		rect := mouse.Rect{
			X: region.Rect.X,
			Y: 1 + visibleTop - start,
			W: region.Rect.W,
			H: visibleBottom - visibleTop,
		}
		m.mouse.HitMap.Add(region.ID, rect, region.Data)
	}
}

// previews is every registered feature flag, in registry order, each carrying
// whatever hand-written copy it has. The list is derived rather than curated so
// a flag cannot be added to internal/features and stay invisible — which is how
// five of them (interactive input, inline edit, auto-refresh, content panes,
// resource providers) were only reachable by hand-editing config.json.
func previews() []preview {
	all := features.ListAll()
	items := make([]preview, 0, len(all))
	for _, feature := range all {
		items = append(items, previewFor(feature))
	}
	return items
}

// previewFor is the derivation for one feature: hand-written copy where it
// exists, the registry's own name and description where it does not. It is a
// separate function so the fallback can be tested with a feature that has no
// entry — every registered flag currently has one, which made a test that
// skipped curated flags iterate zero times and prove nothing.
func previewFor(feature features.Feature) preview {
	item := previewCopy[feature.Name]
	item.flag = feature.Name
	if item.label == "" {
		item.label = feature.Name
	}
	if item.help == "" {
		item.help = feature.Description
	}
	return item
}

// previewRow paints one flag the way Panels paints a surface: title and the
// same ON/OFF pill on the first line, explanation underneath. Only the pill
// toggles on click.
//
// A flag owned by another page is shown with its real state but does not toggle
// here; activating the row jumps to the control that owns it.
func (m *Model) previewRow(b *paneBuilder, item preview) {
	enabled := item.state(m)
	// Every row says what it does, whether or not the cursor is on it: half
	// these names do not explain themselves, and a flag a user cannot interpret
	// is one they will not touch.
	//
	// The help line is written to fit the row's width, so a row stays two lines
	// and twelve of them stay inside the pane. Anything conditional is added
	// only for the focused row, which is the one place a third line is
	// affordable.
	detailFor := func(s State) string {
		detail := item.help
		if !s.Focused {
			return detail
		}
		// Every clause with something to say gets said. A switch stopping at
		// the first match is what silently dropped the one sentence warning
		// that turning terminal_resource_providers off kills running provider
		// processes — it is the only flag that both restarts and has a note.
		if item.owner != "" {
			detail += " Set this on " + PageTitle(item.owner) + ", which pairs it with the panel's own settings."
		}
		if item.restart {
			detail += " Read once when Sidecar starts, so a change takes effect after a restart."
		}
		if item.note != "" {
			detail += " " + item.note
		}
		return detail
	}
	// A restart-scoped flag says so inline rather than spending a line on it,
	// so the warning is visible on every row and not only the focused one.
	badge := ""
	if item.restart {
		badge = Badge("restart", false)
	}
	if item.owner != "" {
		b.panelStatus(regionFlag+item.flag, item.label, badge, detailFor, enabled, func(m *Model) tea.Cmd {
			m.Navigate(item.owner)
			m.detailFocus = true
			m.focusControlByID(item.ownerControl)
			return nil
		})
		return
	}
	b.panelToggleFocusDetail(regionFlag+item.flag, item.label, badge, detailFor, enabled, func(m *Model) tea.Cmd {
		next := !m.flagEnabled(item.flag)
		// The restart requirement is stated at save time, next to the control
		// that needs it, and only for the flags that genuinely need it.
		if item.restart {
			m.noteRestart()
		}
		return saveFlagCmd(toggleNotice(item.label, next), item.flag, next)
	})
}

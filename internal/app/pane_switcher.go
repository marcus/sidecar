package app

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// paneSwitcherKeyName is the key a plugin's browse context binds to the
// switcher. The two Workspaces surfaces answer `n`
// (workspace.paneSwitcherKeyName, overview.paneSwitcherKeyName); a plugin
// cannot, because every plugin that has a create already spends `n` on it —
// new-note, next-match — and displacing those is exactly the drift the
// Workspaces half avoided. `ctrl+n` is the same intent one modifier out, and it
// is the key the Workspaces list already answers with its own second create.
//
// It is documentation and a test anchor, not the lookup: the host reads the key
// out of the keymap (paneSwitcherKeyFor), so a focused passive leaf keeps
// answering the `n` its context binds on all three surfaces, and a rebind
// works.
const paneSwitcherKeyName = "ctrl+n"

// paneSwitcherCommand is the keymap command a browse context binds the key to.
// The binding is the opt-in: a context that does not name this command never
// reaches the switcher, whatever the plugin behind it is.
const paneSwitcherCommand = "open-pane"

const paneSwitcherContext = "pane-switcher"

// paneSwitcherPickerDataMsg lands the switcher's suggestion sources in one
// round trip. Files travel separately because a large tree takes whole seconds
// to scan and the other three answers must not wait behind it.
//
// The app host carries its own message types rather than reusing the ones the
// Workspaces hosts broadcast: those are answered by whichever of them has a
// form open, and a third claimant would either steal them or be stolen from.
type paneSwitcherPickerDataMsg struct {
	Root   string
	Refs   []workspaceops.DiffRef
	Issues []workspaceops.IssueRef
	Notes  []workspaceops.NoteRef
}

type paneSwitcherFilesMsg struct {
	Root  string
	Paths []string
}

// paneSwitcherKeyFor answers which key opens the switcher in a context, and ""
// when the context never asked for it. The keymap is the whole opt-in, so the
// key is read from it rather than compared against a constant: a plugin's
// browse context names `ctrl+n`, while a focused passive leaf reports one of the
// workspace-doc|issue|note|diff|resource contexts and answers the `n` the two
// Workspaces surfaces answer in that same context. One model, three
// projections, one key each — and a user who rebinds open-pane moves the entry
// and its footer hint together rather than losing both.
//
// A context that binds the key to something else — workspace-doc-find spends
// `n` on next-match, every finder and editor spends ctrl+n on cursor-down —
// never names open-pane and so never reaches here.
func (m *Model) paneSwitcherKeyFor(context string) string {
	if context == "" || context == "global" || m.keymap == nil {
		return ""
	}
	for _, b := range m.keymap.BindingsForContext(context) {
		if b.Command == paneSwitcherCommand {
			return b.Key
		}
	}
	return ""
}

// paneSwitcherClaimsKey reports that a key is the switcher's entry where the
// user is standing. A focused passive leaf absorbs almost every key it does not
// own; this is what makes it hand this one back, since opening a pane beside
// the pane you are reading is the whole point of the entry.
func (m *Model) paneSwitcherClaimsKey(key string) bool {
	return key != "" && key == m.paneSwitcherKeyFor(m.activeContext)
}

// paneSwitcherAvailable reports that the switcher can open from where the user
// is standing. Two things have to be true, and both are structural rather than
// per-plugin: the focused surface holds an app content deck (which is where the
// result lands, and which carries the PluginContentPanes gate), and the focused
// context binds the entry.
//
// A key that opens a modal whose result has nowhere to go is worse than no key,
// which is why the deck answers first.
func (m *Model) paneSwitcherAvailable() bool {
	if m.hasModal() || m.configOpen() || m.textInputFocused() {
		return false
	}
	if !m.contentDeckEligible(m.focusedSurface()) {
		return false
	}
	return m.paneSwitcherKeyFor(m.activeContext) != ""
}

// openPaneSwitcher raises the switcher over the focused plugin. It reports
// false when the entry does not belong here, so the caller can let the key fall
// through to the rest of the ladder untouched.
func (m *Model) openPaneSwitcher() (tea.Cmd, bool) {
	if !m.paneSwitcherAvailable() {
		return nil, false
	}
	h := m.activeContentDeck()
	if h == nil {
		return nil, false
	}
	m.paneSwitcher = workspacecreate.Open(workspacecreate.OpenOpts{
		// Kind is only the fallback when nothing was remembered; the list opens
		// on the row it was last left on, focused, exactly as it does on the
		// Workspaces surfaces.
		Kind:        workspacecreate.KindFile,
		FocusKind:   true,
		UseLastKind: true,
		// A plugin deck is a passive contentpanes deck with no live-leaf host:
		// no termpanes binding, no tty routing, no live-leaf cap. The row
		// arrives here by flipping this flag once that adoption lands.
		PaneKindsOnly: true,
		// ShowProject stays off: the deck belongs to one project's plugin, and
		// there is no second project a pane could be opened against from here.
		AllowTerminalSplit: false,
		ShowProject:        false,
		ShowNotes:          m.notesPaneAvailable(),
		Providers:          m.configuredPaneProviders(),
	})
	m.paneSwitcherOpen = true
	if m.paneSwitcherMouse == nil {
		m.paneSwitcherMouse = mouse.NewHandler()
	}
	return tea.Batch(m.loadPaneSwitcherPickerData(h.workdir), m.loadPaneSwitcherFiles(h.workdir)), true
}

func (m *Model) closePaneSwitcher() {
	m.paneSwitcherOpen = false
	m.paneSwitcher = nil
}

// notesPaneAvailable mirrors assembly.NotesWanted from the one place that does
// not have to infer it: the registry says whether the Notes surface exists in
// this build and configuration, so the Note row is offered exactly when a note
// pane can be resolved.
func (m *Model) notesPaneAvailable() bool {
	if m.registry == nil {
		return false
	}
	for _, p := range m.registry.Plugins() {
		if p.ID() == "notes" {
			return true
		}
	}
	return false
}

// configuredPaneProviders is one kind row per enabled terminal-resource
// provider, matching what the deck's Resource leaf can actually resolve.
func (m *Model) configuredPaneProviders() []workspacecreate.ProviderItem {
	if m.cfg == nil {
		return nil
	}
	var items []workspacecreate.ProviderItem
	for _, provider := range m.cfg.TerminalResources.EnabledProviders() {
		items = append(items, workspacecreate.ProviderItem{ID: provider.ID})
	}
	return items
}

// loadPaneSwitcherPickerData fetches everything the target pickers offer except
// the file list, in a command — never on the update path.
func (m *Model) loadPaneSwitcherPickerData(root string) tea.Cmd {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	wantNotes := m.notesPaneAvailable()
	return func() tea.Msg {
		ctx := context.Background()
		msg := paneSwitcherPickerDataMsg{Root: root}
		if refs, err := workspaceops.RecentDiffRefs(ctx, root, 15); err == nil {
			msg.Refs = refs
		}
		if issues, err := workspaceops.RecentIssues(ctx, root, 30); err == nil {
			msg.Issues = issues
		}
		if wantNotes {
			if notes, err := workspaceops.RecentNotes(ctx, root, 20); err == nil {
				msg.Notes = notes
			}
		}
		return msg
	}
}

// loadPaneSwitcherFiles scans the deck's root for the File picker,
// gitignore-aware, off the update path.
func (m *Model) loadPaneSwitcherFiles(root string) tea.Cmd {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	return func() tea.Msg {
		paths, _ := filefind.ScanPaths(root, false)
		return paneSwitcherFilesMsg{Root: root, Paths: paths}
	}
}

func (m *Model) applyPaneSwitcherPickerData(msg paneSwitcherPickerDataMsg) {
	if m.paneSwitcher == nil || !m.paneSwitcherRootMatches(msg.Root) {
		return
	}
	// The folds are workspacecreate's, shared with both Workspaces surfaces —
	// Value carries what resolves, Label only what reads.
	m.paneSwitcher.SetDiffRefs(workspacecreate.FoldDiffRefs(msg.Refs))
	m.paneSwitcher.SetIssues(workspacecreate.FoldIssues(msg.Issues))
	m.paneSwitcher.SetNotes(workspacecreate.FoldNotes(msg.Notes))
}

// paneSwitcherRootMatches drops a load that outlived the root it was fired
// for. A worktree switch or a project switch replaces the deck under an open
// form, and suggestions from the old tree resolve to nothing in the new one.
func (m *Model) paneSwitcherRootMatches(root string) bool {
	h := m.currentContentDeck()
	return h != nil && h.workdir == root
}

// applyPaneSwitcherFiles orders the scan so the documents already open in this
// deck lead — the recents this surface genuinely knows about — and hands the
// rest to the picker as-is.
func (m *Model) applyPaneSwitcherFiles(msg paneSwitcherFilesMsg) {
	if m.paneSwitcher == nil || !m.paneSwitcherRootMatches(msg.Root) {
		return
	}
	recent := make([]string, 0, 8)
	seen := make(map[string]bool, len(msg.Paths)+8)
	if h := m.currentContentDeck(); h != nil {
		if leaf := h.deck.Leaf(panelayout.Document); leaf != 0 {
			items, _ := h.deck.Tabs(leaf)
			for _, item := range items {
				rel := strings.TrimSpace(item.Ref.Value)
				if rel == "" || seen[rel] {
					continue
				}
				seen[rel] = true
				recent = append(recent, rel)
			}
		}
	}
	candidates := make([]string, 0, len(recent)+len(msg.Paths))
	candidates = append(candidates, recent...)
	for _, path := range msg.Paths {
		if !seen[path] {
			seen[path] = true
			candidates = append(candidates, path)
		}
	}
	m.paneSwitcher.SetFileCandidates(candidates)
}

// ensurePaneSwitcherModal builds the form at the current terminal width. The
// form owns the cache — Build returns the same modal until width, step, kind or
// picker content changes — so nothing is stored here, which is what lets View's
// value receiver call it without the built modal being thrown away with the
// copy.
func (m *Model) ensurePaneSwitcherModal() {
	if !m.paneSwitcherOpen || m.paneSwitcher == nil {
		return
	}
	width := 52
	if m.width > 0 && width > m.width-4 {
		width = m.width - 4
	}
	if width < 24 {
		width = 24
	}
	m.paneSwitcher.Build(width)
}

func (m *Model) activePaneSwitcherModal() *modal.Modal {
	if m.paneSwitcher == nil {
		return nil
	}
	return m.paneSwitcher.Modal()
}

func (m *Model) renderPaneSwitcherOverlay(background string) string {
	m.ensurePaneSwitcherModal()
	md := m.activePaneSwitcherModal()
	if md == nil {
		return background
	}
	rendered := md.Render(m.width, m.height, m.paneSwitcherMouse)
	return ui.OverlayModal(background, rendered, m.width, m.height)
}

// handlePaneSwitcherKey gives the modal the keyboard while it is up. The form
// owns the two-step flow: Esc on the picker step returns to the kind list
// instead of closing, and Enter on a target-needing kind advances to it. What
// escapes is an action for applyPaneSwitcherAction.
func (m *Model) handlePaneSwitcherKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensurePaneSwitcherModal()
	if m.paneSwitcher == nil {
		m.closePaneSwitcher()
		m.updateContext()
		return m, nil
	}
	action, cmd := m.paneSwitcher.HandleKey(msg)
	m.paneSwitcher.SyncAfterInput()
	if action == "" {
		m.paneSwitcher.SetError("")
	}
	return m, tea.Batch(cmd, m.applyPaneSwitcherAction(action))
}

func (m *Model) handlePaneSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensurePaneSwitcherModal()
	md := m.activePaneSwitcherModal()
	if md == nil || m.paneSwitcher == nil {
		return m, nil
	}
	action := md.HandleMouse(msg, m.paneSwitcherMouse)
	action = m.paneSwitcher.TranslateMouseAction(action)
	m.paneSwitcher.SyncAfterInput()
	return m, m.applyPaneSwitcherAction(action)
}

// paneSwitcherPaste types a paste into the picker's filter, the modal's only
// text field — pasting the issue id the recents do not know is exactly what
// that field is for. On the kind step there is nothing to type into, and the
// paste is dropped rather than reaching the plugin behind the modal.
func (m *Model) paneSwitcherPaste(text string) tea.Cmd {
	if m.paneSwitcher == nil || m.paneSwitcher.Step() != workspacecreate.StepTarget {
		return nil
	}
	m.ensurePaneSwitcherModal()
	md := m.activePaneSwitcherModal()
	if md == nil {
		return nil
	}
	prev := md.FocusedID()
	md.SetFocus(workspacecreate.FieldPickerInput)
	for _, r := range text {
		_, _ = md.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if prev != "" && prev != workspacecreate.FieldPickerInput {
		md.SetFocus(prev)
	}
	m.paneSwitcher.SyncAfterInput()
	return nil
}

func (m *Model) applyPaneSwitcherAction(action string) tea.Cmd {
	if m.paneSwitcher == nil {
		return nil
	}
	if workspacecreate.IsPlacementAction(action) {
		// On the picker step one click opens with that placement; from the kind
		// list of a target-needing kind it continues there instead.
		if m.paneSwitcher.ApplyPlacementActionStep(action) == workspacecreate.PlacementSubmitted {
			return m.applyPaneSwitcherAction(workspacecreate.ActionCreate)
		}
		return nil
	}
	switch action {
	case "cancel", workspacecreate.ActionCancel:
		m.closePaneSwitcher()
		m.updateContext()
		return nil
	case workspacecreate.ActionCreate:
		return m.submitPaneSwitcher()
	}
	return nil
}

// submitPaneSwitcher resolves the picker's answer onto the same
// uirequest.Target `sidecar open` produces and opens it through the deck's own
// open path. Nothing here decides where the pane goes or what it holds: the
// target-to-ref mapping is the deck's, and the placement is the same
// auto|right|below the CLI's --split means.
func (m *Model) submitPaneSwitcher() tea.Cmd {
	h := m.activeContentDeck()
	if h == nil || m.paneSwitcher == nil {
		m.closePaneSwitcher()
		m.updateContext()
		return nil
	}
	target, err := m.paneSwitcher.TargetFor(h.workdir)
	if err != nil {
		// A resolution failure keeps the modal up with the reason: the answer
		// is fixable in place, and closing would discard the query behind it.
		m.paneSwitcher.SetError(err.Error())
		return nil
	}
	ref, refusal, ok := h.contentRefForTarget(target)
	if !ok {
		if refusal == "" {
			refusal = "this target cannot be opened beside " + h.pluginID
		}
		m.paneSwitcher.SetError(refusal)
		return nil
	}
	split := m.paneSwitcher.PlacementSplit()
	m.closePaneSwitcher()
	out := m.openAppContentOutcome(h, ref, split, nil)
	m.updateContext()
	if out.Status == contentpanes.StatusRefused {
		if out.Refusal == contentpanes.RefusalFit || out.Refusal == contentpanes.RefusalPlacement {
			return appmsg.ShowToast("Content pane needs a wider window; layout left unchanged", 3*time.Second)
		}
		return nil
	}
	return out.Command
}

// paneSwitcherCommands is the app's contribution to a plugin's footer and to
// the merged help. The deck and the key are the app's, not the plugin's, so the
// command cannot come from plugin.Commands() without five plugins each growing
// a copy of the same entry — the drift decision 6 exists to prevent.
//
// It is scoped to the exact context on screen, so it appears only where the
// binding does — including the focused passive leaf contexts, whose own
// appContentCommands names Close and the tab controls but has never named this
// one.
//
// While the modal is up it describes the modal instead: nothing else contributes
// to the footer of a context that belongs to the app rather than to a plugin.
func (m *Model) paneSwitcherCommands() []plugin.Command {
	if m.paneSwitcherOpen {
		return paneSwitcherModalCommands()
	}
	if !m.paneSwitcherAvailable() {
		return nil
	}
	return []plugin.Command{{
		ID:          paneSwitcherCommand,
		Name:        "Pane",
		Description: "Open a pane beside this",
		Context:     m.activeContext,
		Priority:    20,
		Handler: func() tea.Cmd {
			cmd, _ := m.openPaneSwitcher()
			return cmd
		},
	}}
}

// paneSwitcherModalCommands names the modal's own keys for the footer, the way
// both Workspaces hosts name their create modal's. They carry no handler: the
// form answers these keys directly at the top of the key ladder, and nothing
// can reach a command dispatcher while a modal owns the keyboard. They exist so
// the footer describes the surface the keys are actually going to.
func paneSwitcherModalCommands() []plugin.Command {
	command := func(id, name, description string, priority int) plugin.Command {
		return plugin.Command{ID: id, Name: name, Description: description, Context: paneSwitcherContext, Priority: priority}
	}
	return []plugin.Command{
		command("confirm", "Open", "Open the selected pane", 1),
		command("navigate-picker", "Move", "Move the selection", 2),
		command("next-field", "Field", "Next field", 3),
		command("cancel", "Back", "Back, then close", 4),
	}
}

// paneSwitcherWheelAtBoundary answers for the switcher overlay: its wheel is
// exactly modal.Modal.HandleMouse, so the body scrolls and everything else
// absorbs the event.
func (m *Model) paneSwitcherWheelAtBoundary(msg tea.MouseWheelMsg) bool {
	return modalWheelAtBoundary(m.activePaneSwitcherModal(), m.paneSwitcherMouse, msg)
}

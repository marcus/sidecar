package workspacecreate

import (
	"strconv"
	"strings"

	"github.com/marcus/sidecar/internal/modal"
)

// Kind is the workspace type the form will create.
type Kind int

const (
	KindShell Kind = iota
	KindWorktree
	// KindTerminalSplit creates a live terminal beside the workspace's own
	// terminal. It owns no branch and no agent, so it needs at most a name.
	KindTerminalSplit
	// The pane kinds below open one leaf of the pane tree. Each needs a
	// target, so the modal grows a second step for them; each has somewhere
	// to be placed, so the placement row shows for every one of them.
	KindFile
	KindDiff
	KindIssue
	// KindResource opens a configured terminal-resource provider's locator as
	// a resource pane. One row exists per configured instance; ProviderID on
	// the row says which, and the label is that instance ID.
	KindResource
	KindNote
)

// kindRow is one row of the create modal's kind list. The list is a table so a
// later pane kind is an entry here rather than new modal code.
type kindRow struct {
	Kind  Kind
	Label string
	// Description is the aligned second column of the vertical list.
	Description string
	// NeedsTarget marks the pane kinds whose step 2 is a target picker.
	NeedsTarget bool
	// ProviderID names the configured instance behind a KindResource row.
	ProviderID string
	// NeedsLiveTerminal marks the row that opens a SECOND live terminal beside
	// the host's own. It is the one capability the two hosts genuinely differ
	// on: the project workspace owns a live terminal peer, the global browser
	// has one producer bound to the selected row. Every other row places a
	// passive pane in a tree, which both hosts have — so nothing else belongs
	// here, and a passive row tagged with it goes missing on a surface that
	// could have drawn it.
	NeedsLiveTerminal bool
}

// kindCatalog is every row the modal knows, in list order. Provider rows are
// appended per host from config, after Issue where the mockup puts them.
var kindCatalog = []kindRow{
	{Kind: KindShell, Label: "Shell", Description: "new agent/shell session"},
	{Kind: KindWorktree, Label: "Worktree", Description: "shell in a new worktree"},
	{Kind: KindTerminalSplit, Label: "Terminal split", Description: "terminal beside current pane", NeedsLiveTerminal: true},
	{Kind: KindFile, Label: "File", Description: "open a file in a split", NeedsTarget: true},
	{Kind: KindDiff, Label: "Git diff", Description: "open a diff in a split", NeedsTarget: true},
	{Kind: KindIssue, Label: "td issue", Description: "open an issue in a split", NeedsTarget: true},
	{Kind: KindNote, Label: "Note", Description: "open a note in a split", NeedsTarget: true},
}

// providerDescription is the fixed description for resource rows.
const providerDescription = "open a resource in a split"

// ProviderItem is one configured terminal-resource provider a host offers.
type ProviderItem struct {
	ID string
}

// kindRowsFor is the catalog a host offers.
func kindRowsFor(allowTerminalSplit bool) []kindRow {
	return kindRowsForOpts(rowOpts{allowTerminalSplit: allowTerminalSplit})
}

// rowOpts is what shapes a host's catalog beyond the base table.
type rowOpts struct {
	allowTerminalSplit bool
	showNotes          bool
	// paneKindsOnly drops the rows that create a workspace rather than a pane.
	paneKindsOnly bool
	providers     []ProviderItem
}

func kindRowsForOpts(opts rowOpts) []kindRow {
	rows := make([]kindRow, 0, len(kindCatalog)+1+len(opts.providers))
	for _, row := range kindCatalog {
		if row.Kind == KindNote && !opts.showNotes {
			continue
		}
		if row.NeedsLiveTerminal && !opts.allowTerminalSplit {
			continue
		}
		// Shell and Worktree create workspace rows, which a host with no
		// workspace list has nowhere to put. Asked as kindIsPane rather than as
		// a Shell/Worktree exclusion so a future non-pane row is dropped too.
		if opts.paneKindsOnly && !kindIsPane(row.Kind) {
			continue
		}
		rows = append(rows, row)
	}
	// A provider row opens a passive Resource pane, which every host with a
	// pane tree can place — and both hosts have one. It is offered wherever the
	// provider is configured, exactly like the File and Git diff rows beside it.
	for _, p := range opts.providers {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		rows = append(rows, kindRow{
			Kind:        KindResource,
			Label:       id,
			Description: providerDescription,
			NeedsTarget: true,
			ProviderID:  id,
		})
	}
	return rows
}

func kindLabel(rows []kindRow, kind Kind) string {
	for _, row := range rows {
		if row.Kind == kind {
			return row.Label
		}
	}
	return ""
}

// kindIsPane reports whether kind opens a leaf of the pane tree, which is the
// set the placement row belongs to.
func kindIsPane(kind Kind) bool {
	switch kind {
	case KindTerminalSplit, KindFile, KindDiff, KindIssue, KindResource, KindNote:
		return true
	}
	return false
}

// kindUsesName reports whether kind takes a name the user types. Shell,
// Worktree and the Terminal split are things a user names — a worktree's name
// even becomes its branch. Every other pane kind is named by what it shows: a
// file by its path, an issue and a note by their titles. Offering those a Name
// field asked for a value no create path ever read, and implied a pane could be
// called something other than the thing inside it.
func kindUsesName(kind Kind) bool {
	switch kind {
	case KindShell, KindWorktree, KindTerminalSplit:
		return true
	}
	return false
}

// kindNeedsTarget reports whether kind's create flow continues onto a target
// picker step rather than submitting from the kind list.
func kindNeedsTarget(kind Kind) bool {
	switch kind {
	case KindFile, KindDiff, KindIssue, KindResource, KindNote:
		return true
	}
	return false
}

// kindItemID is one kind row's hit-region ID. Rows are addressed by INDEX
// rather than by Kind, because every configured resource provider is its own
// row under one Kind and a click has to land on the instance it was over.
func kindItemID(id string, idx int) string {
	return id + ":" + strconv.Itoa(idx)
}

// kindControl is the create modal's kind chooser: modal.Select over this
// host's catalog. The two shapes, the disabled rule and the click resolution
// all come from the shared control, so this function's whole job is turning
// rows into choices. Selection is row-precise — the index resolves the
// highlighted row, since resource providers share one Kind — and onSelect
// receives that index. disabledReason answers, per row, why that row cannot
// be created right now.
func kindControl(id string, rows []kindRow, selected *int, onSelect func(int), disabledReason func(Kind) string) modal.Section {
	items := make([]modal.SelectItem, len(rows))
	for i, row := range rows {
		items[i] = modal.SelectItem{
			ID:          kindItemID(id, i),
			Label:       row.Label,
			Description: row.Description,
		}
	}
	return modal.Select(id, items, selected,
		modal.WithDisabled(func(i int) string {
			if disabledReason == nil || i < 0 || i >= len(rows) {
				return ""
			}
			return disabledReason(rows[i].Kind)
		}),
		modal.WithOnSelect(func(i int) {
			if onSelect != nil {
				onSelect(i)
			}
		}),
		// A click on a row reports the control, which is what every host has
		// always seen from this modal; the row it landed on has already been
		// applied through onSelect.
		modal.WithSelectAction(id),
	)
}

// verticalArrowsSteerKindList reports that up/down, pressed while focusID
// holds focus, belong to the kind list rather than to the focused field. The
// kind step is meant to be steered with arrows alone — open it, arrow to the
// row, Enter — so the list keeps up/down everywhere except the fields that
// give them a meaning of their own: a combo's dropdown moves with them, while
// a plain input, a checkbox, and a button have nothing to do with a vertical
// arrow. A new field that grows a vertical gesture belongs in this list.
func verticalArrowsSteerKindList(focusID string) bool {
	switch focusID {
	case FieldProject, FieldBase, FieldAgent:
		return false
	}
	return true
}

// moveKindSelection moves the list by delta rows and stops at either end,
// rather than wrapping: the ends of a short list are easier to feel than to
// count, and a wrap past Note back onto Shell reads as a lost keypress. A row
// that cannot be created right now is stepped over, exactly as the control
// steps over it when the same gesture arrives through the modal.
func (f *Form) moveKindSelection(delta int) {
	if f == nil || len(f.rows) == 0 || delta == 0 {
		return
	}
	idx := clampIndex(f.selectedRowIndex(), len(f.rows))
	for {
		idx += delta
		if idx < 0 || idx >= len(f.rows) {
			return
		}
		if f.kindDisabledReason(f.rows[idx].Kind) == "" {
			f.selectRow(idx)
			return
		}
	}
}

func clampIndex(idx, length int) int {
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}

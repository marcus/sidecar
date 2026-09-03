package workspacecreate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// pickerState is the target picker's data: what the hosts loaded, the query
// being typed, and where the list cursor sits. Only one picker step is live at
// a time, so one state serves every kind.
type pickerState struct {
	input     textinput.Model
	files     []string
	refs      []Suggestion
	issues    []Suggestion
	notes     []Suggestion
	cursor    int
	scroll    int
	lastQuery string

	filesLoaded bool
}

// FilesScannedMsg carries a background file scan for the File picker. Hosts
// produce it from filefind.ScanPaths so both surfaces feed the picker the same
// way without this package learning to walk a filesystem.
type FilesScannedMsg struct {
	Root  string
	Paths []string
}

func (p *pickerState) init() {
	p.input = textinput.New()
	p.input.Prompt = "❯ "
	p.input.CharLimit = 200
	p.cursor = 0
	p.scroll = 0
	p.lastQuery = ""
}

// source returns the picker's suggestion pool for kind. The working-tree diff
// entry is prepended here so every consumer — count line, list, selection —
// sees one list.
func (p *pickerState) source(kind Kind) []Suggestion {
	switch kind {
	case KindFile:
		out := make([]Suggestion, 0, len(p.files))
		for _, path := range p.files {
			out = append(out, Suggestion{Value: path, Label: path})
		}
		return out
	case KindDiff:
		out := make([]Suggestion, 0, len(p.refs)+1)
		out = append(out, Suggestion{Value: "", Label: "Working tree", Badge: "default"})
		out = append(out, p.refs...)
		return out
	case KindIssue:
		return p.issues
	case KindNote:
		return p.notes
	default:
		return nil
	}
}

// filterSuggestions applies the query to suggestions. Empty queries keep
// order; fuzzy matches sort by score, ties keeping list order (recent-first).
func filterSuggestions(query string, all []Suggestion) []Suggestion {
	query = strings.TrimSpace(query)
	if query == "" {
		return all
	}
	type scored struct {
		item  Suggestion
		score int
	}
	var matches []scored
	for _, item := range all {
		best := 0
		if s, _ := palette.FuzzyMatch(query, item.Label); s > best {
			best = s
		}
		if item.Value != item.Label {
			if s, _ := palette.FuzzyMatch(query, item.Value); s > best {
				best = s
			}
		}
		if best > 0 {
			matches = append(matches, scored{item: item, score: best})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	out := make([]Suggestion, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.item)
	}
	return out
}

// items are the picker's filtered rows for the current step and kind.
// PickerSuggestions is the current picker's suggestion pool, for tests.
func (f *Form) PickerSuggestions() []Suggestion {
	if f == nil {
		return nil
	}
	return f.items()
}

func (f *Form) items() []Suggestion {
	if f == nil || f.step != StepTarget {
		return nil
	}
	return filterSuggestions(f.picker.input.Value(), f.picker.source(f.kind))
}

// selectedProviderID is the instance behind the selected row; empty for every
// non-resource kind.
func (f *Form) selectedProviderID() string {
	if f == nil {
		return ""
	}
	return f.providerID
}

// selectedRowIndex is the exact row the form has chosen: Kind plus provider
// instance, since resource rows share a Kind.
func (f *Form) selectedRowIndex() int {
	for i := range f.rows {
		if f.rows[i].Kind == f.kind && f.rows[i].ProviderID == f.providerID {
			return i
		}
	}
	return f.firstRowOfKind(f.kind)
}

// selectedLabel is the chosen row's label — the provider's instance ID for
// resource rows, so titles and pickers name the instance that was picked.
func (f *Form) selectedLabel() string {
	if f == nil || len(f.rows) == 0 {
		return ""
	}
	return f.rows[f.selectedRowIndex()].Label
}

// countLine is the state line between input and list, in the switcher idiom.
func (f *Form) countLine() string {
	if f == nil || f.step != StepTarget {
		return ""
	}
	all := f.picker.source(f.kind)
	filtered := f.items()
	query := strings.TrimSpace(f.picker.input.Value())
	switch f.kind {
	case KindFile:
		if len(all) == 0 && !f.picker.filesLoaded {
			return "indexing files…"
		}
		if query != "" {
			return fmt.Sprintf("%d match%s · recent first", len(filtered), plural(len(filtered), "es"))
		}
		return fmt.Sprintf("%d recent · path:line accepted", len(all))
	case KindDiff:
		// Counted over refs alone: the working-tree default is not a ref, and
		// whether it fuzzy-matches a query must not move the denominator.
		refs := max(0, len(all)-1)
		if query != "" {
			return fmt.Sprintf("%d of %d refs", len(filterSuggestions(query, f.picker.refs)), refs)
		}
		return fmt.Sprintf("working tree default · %d refs", refs)
	case KindIssue:
		if query != "" {
			return fmt.Sprintf("%d of %d issues", len(filtered), len(all))
		}
		return fmt.Sprintf("in progress + recent · %d shown", len(all))
	case KindNote:
		if len(all) == 0 {
			return "no notes found"
		}
		if query != "" {
			return fmt.Sprintf("%d of %d notes", len(filtered), len(all))
		}
		return fmt.Sprintf("%d note%s", len(all), plural(len(all), "s"))
	case KindResource:
		return "locator validated by the provider on open"
	default:
		return ""
	}
}

// plural is the suffix a count takes. The suffix is the caller's because it is
// not the same one everywhere: matches take "es", notes take "s".
func plural(n int, suffix string) string {
	if n == 1 {
		return ""
	}
	return suffix
}

// Step reports which screen the form shows.
func (f *Form) Step() FormStep {
	if f == nil {
		return StepKind
	}
	return f.step
}

// needsTarget reports whether the currently selected kind continues onto the
// picker step.
// NeedsTarget reports that the selected kind has a picker step.
func (f *Form) NeedsTarget() bool { return f.needsTarget() }

func (f *Form) needsTarget() bool {
	if f == nil {
		return false
	}
	for _, r := range f.rows {
		if r.Kind == f.kind {
			return r.NeedsTarget
		}
	}
	return false
}

// AdvanceToTarget moves to the picker step for kinds that need one. The caller
// owns why: Enter chose the kind, or a placement button chose the placement
// and the target is all that is left to ask.
func (f *Form) AdvanceToTarget() {
	if f == nil || f.step == StepTarget || !f.needsTarget() {
		return
	}
	f.step = StepTarget
	f.picker.init()
	f.pendingFocus = FieldPickerInput
	f.invalidate()
}

// BackToKind returns from the picker step to the kind list. Esc on the picker
// step means this, never close.
func (f *Form) BackToKind() {
	if f == nil || f.step != StepTarget {
		return
	}
	f.step = StepKind
	f.pendingFocus = FieldKind
	f.invalidate()
}

// PickerInput exposes the filter input so tests can drive typing exactly as a
// keyboard would and hosts can paste into the step.
func (f *Form) PickerInput() *textinput.Model {
	if f == nil {
		return nil
	}
	return &f.picker.input
}

// SetFileCandidates replaces the File picker's candidates, most recently
// relevant first.
func (f *Form) SetFileCandidates(paths []string) {
	if f == nil {
		return
	}
	f.picker.files = append([]string(nil), paths...)
	f.picker.filesLoaded = true
	f.syncPickerCursor()
	f.invalidate()
}

// SetDiffRefs replaces the diff picker's refs (recent commits and branches).
func (f *Form) SetDiffRefs(refs []Suggestion) {
	if f == nil {
		return
	}
	f.picker.refs = append([]Suggestion(nil), refs...)
	f.syncPickerCursor()
	f.invalidate()
}

// SetIssues replaces the issue picker's in-progress + recent issues.
func (f *Form) SetIssues(items []Suggestion) {
	if f == nil {
		return
	}
	f.picker.issues = append([]Suggestion(nil), items...)
	f.syncPickerCursor()
	f.invalidate()
}

// SetNotes replaces the note picker's recent notes.
func (f *Form) SetNotes(items []Suggestion) {
	if f == nil {
		return
	}
	f.picker.notes = append([]Suggestion(nil), items...)
	f.syncPickerCursor()
	f.invalidate()
}

// TargetFor resolves the picker's current answer onto the same uirequest.Target
// the CLI produces: the highlighted suggestion wins when there are matches,
// otherwise the raw typed value does (paste an id the recents do not know).
// workDir is the workspace root file paths resolve against.
func (f *Form) TargetFor(workDir string) (uirequest.Target, error) {
	if f == nil {
		return uirequest.Target{}, fmt.Errorf("no form")
	}
	if f.step != StepTarget {
		return uirequest.Target{}, fmt.Errorf("the %s row has no target to resolve", kindLabel(f.rows, f.kind))
	}
	return ResolvePickerTarget(workDir, f.kind, f.selectedProviderID(), f.pickerRaw())
}

func (f *Form) pickerRaw() string {
	raw := strings.TrimSpace(f.picker.input.Value())
	filtered := f.items()
	if f.kind != KindResource && len(filtered) > 0 && f.picker.cursor < len(filtered) {
		raw = filtered[f.picker.cursor].Value
	}
	return raw
}

// TargetForRemote resolves the picker answer without touching this machine's
// filesystem. File and diff values pass through as the host listed them;
// openPreviewTarget re-resolves through SourceContext.
func (f *Form) TargetForRemote() (uirequest.Target, error) {
	if f == nil {
		return uirequest.Target{}, fmt.Errorf("no form")
	}
	if f.step != StepTarget {
		return uirequest.Target{}, fmt.Errorf("the %s row has no target to resolve", kindLabel(f.rows, f.kind))
	}
	return ResolvePickerTargetRemote(f.kind, f.selectedProviderID(), f.pickerRaw())
}

// ResolvePickerTarget resolves one picker answer onto the CLI's target shape,
// per kind. This is the whole reason the picker stays an entry point: it ends
// exactly where `sidecar open` begins.
func ResolvePickerTarget(workDir string, kind Kind, providerID, raw string) (uirequest.Target, error) {
	raw = strings.TrimSpace(raw)
	switch kind {
	case KindFile:
		return uirequest.ResolveFileTarget(workDir, raw, 0)
	case KindDiff:
		return uirequest.ResolveDiffSpec(workDir, raw)
	case KindIssue:
		if !terminallink.IssueID(raw) {
			return uirequest.Target{}, fmt.Errorf("type an issue id like td-756c34")
		}
		return uirequest.Target{Kind: uirequest.TargetKindIssue, Value: raw}, nil
	case KindNote:
		if !contentlink.NoteID(raw) {
			return uirequest.Target{}, fmt.Errorf("type a note identity like nt-4jdj4e")
		}
		return uirequest.Target{Kind: uirequest.TargetKindNote, Value: raw}, nil
	case KindResource:
		return uirequest.ResolveResourceTarget(providerID, raw)
	default:
		return uirequest.Target{}, fmt.Errorf("kind %d takes no target", int(kind))
	}
}

// ResolvePickerTargetRemote is the host-catalog equivalent of ResolvePickerTarget:
// identity only, no local path or git resolution.
func ResolvePickerTargetRemote(kind Kind, providerID, raw string) (uirequest.Target, error) {
	raw = strings.TrimSpace(raw)
	switch kind {
	case KindFile:
		path, line := splitPickerFileLine(raw)
		if path == "" {
			return uirequest.Target{}, fmt.Errorf("file path cannot be empty")
		}
		return uirequest.Target{Kind: uirequest.TargetKindFile, Value: path, Line: line}, nil
	case KindDiff:
		if raw == "" {
			return uirequest.Target{Kind: uirequest.TargetKindDiff, Value: workspacediff.IdentityWorkingTree}, nil
		}
		return uirequest.Target{Kind: uirequest.TargetKindDiff, Value: raw}, nil
	case KindIssue:
		if !terminallink.IssueID(raw) {
			return uirequest.Target{}, fmt.Errorf("type an issue id like td-756c34")
		}
		return uirequest.Target{Kind: uirequest.TargetKindIssue, Value: raw}, nil
	case KindNote:
		if !contentlink.NoteID(raw) {
			return uirequest.Target{}, fmt.Errorf("type a note identity like nt-4jdj4e")
		}
		return uirequest.Target{Kind: uirequest.TargetKindNote, Value: raw}, nil
	case KindResource:
		return uirequest.ResolveResourceTarget(providerID, raw)
	default:
		return uirequest.Target{}, fmt.Errorf("kind %d takes no target", int(kind))
	}
}

func splitPickerFileLine(raw string) (string, int) {
	if colonIdx := strings.LastIndex(raw, ":"); colonIdx > 0 && colonIdx < len(raw)-1 {
		if n, err := strconv.Atoi(raw[colonIdx+1:]); err == nil && n > 0 {
			return raw[:colonIdx], n
		}
	}
	return raw, 0
}

// HandleKey routes a key through the modal and keeps the two-step flow local:
// Esc on the picker step goes back to the kind list instead of closing, and
// Enter on a target-needing kind advances instead of submitting. Up/down on
// the kind step steer the kind list from whichever field holds focus. What
// comes back is the action the host should treat, with those consumed.
func (f *Form) HandleKey(msg tea.KeyPressMsg) (string, tea.Cmd) {
	if f == nil || f.modal == nil {
		return "", nil
	}
	if delta, ok := f.kindArrowDelta(msg); ok {
		f.moveKindSelection(delta)
		return "", nil
	}
	action, cmd := f.modal.HandleKey(msg)
	if action == FieldKind {
		// The kind list reports an activation as its own ID, so a CLICK on a
		// row tells a host "the kind control was used" rather than naming a
		// row the host has no branch for. From the keyboard that same gesture
		// is Enter on the form, which is the modal's primary action.
		action = ActionCreate
	}
	if action == "cancel" && f.step == StepTarget {
		f.BackToKind()
		return "", cmd
	}
	if action == ActionCreate && f.step == StepKind && f.needsTarget() && f.KindDisabledReason() == "" {
		f.AdvanceToTarget()
		return "", cmd
	}
	return action, cmd
}

// kindArrowDelta answers whether this key is an up/down the kind list should
// take from the focused field, and which way it moves the selection. It asks
// only on the kind step: the picker step's own list already owns up/down, and
// there is no kind list on screen to steer.
func (f *Form) kindArrowDelta(msg tea.KeyPressMsg) (int, bool) {
	if f.step != StepKind {
		return 0, false
	}
	var delta int
	switch msg.String() {
	case "up":
		delta = -1
	case "down":
		delta = 1
	default:
		return 0, false
	}
	if !verticalArrowsSteerKindList(f.modal.FocusedID()) {
		return 0, false
	}
	return delta, true
}

// TranslateMouseAction maps picker-row clicks onto the submit action after
// pointing the cursor at the clicked row, so a host's existing ActionCreate
// branch performs the open. Everything else passes through untouched.
func (f *Form) TranslateMouseAction(action string) string {
	if f == nil || !strings.HasPrefix(action, pickerItemPrefix) {
		return action
	}
	var idx int
	if _, err := fmt.Sscanf(action, pickerItemPrefix+"%d", &idx); err != nil || idx < 0 {
		return ""
	}
	items := f.items()
	if idx >= len(items) {
		return ""
	}
	f.picker.cursor = idx
	f.syncScroll()
	return ActionCreate
}

// syncScroll keeps the cursor inside the visible window of the picker list.
func (f *Form) syncScroll() {
	count := len(f.items())
	visible := min(pickerMaxVisible, count)
	if f.picker.cursor >= count {
		f.picker.cursor = max(0, count-1)
	}
	if f.picker.cursor < f.picker.scroll {
		f.picker.scroll = f.picker.cursor
	}
	if visible > 0 && f.picker.cursor >= f.picker.scroll+visible {
		f.picker.scroll = f.picker.cursor - visible + 1
	}
	if f.picker.scroll < 0 {
		f.picker.scroll = 0
	}
}

// syncPickerCursor reclamps the picker after its inputs moved — new data or a
// new query — and flags the modal for rebuild, since both can change what the
// list should show.
func (f *Form) syncPickerCursor() {
	f.picker.lastQuery = f.picker.input.Value()
	f.syncScroll()
	// Data setters and query ticks both arrive here; the rebuild cost is a
	// section re-render, and missing one leaves a stale list on screen.
	f.invalidate()
}

// pickerSections renders filter input + count + suggestion list + hint line
// for the current picker kind, in the worktree-switcher idiom: a real input
// section, a muted count line, and a custom list whose rows carry their own
// hit regions.
func (f *Form) pickerSections() []modal.Section {
	input := modal.Input(FieldPickerInput, &f.picker.input, modal.WithSubmitOnEnter(false))
	count := modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		line := f.countLine()
		if line == "" {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: styles.Muted.Render(line)}
	}, nil)
	list := modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		items := f.items()
		if len(items) == 0 {
			text := "no matches"
			if f.kind == KindFile && !f.picker.filesLoaded {
				text = "indexing files…"
			}
			return modal.RenderedSection{Content: styles.Muted.Render(text)}
		}
		first := min(f.picker.scroll, max(0, len(items)-1))
		last := min(len(items), first+pickerMaxVisible)
		lines := make([]string, 0, last-first)
		focusables := make([]modal.FocusableInfo, 0, last-first)
		for i := first; i < last; i++ {
			item := items[i]
			row := "  "
			if i == f.picker.cursor {
				row = "❯ "
			}
			badge := ""
			if item.Badge != "" {
				badge = "  " + styles.Muted.Render("["+item.Badge+"]")
			}
			lines = append(lines, alignRow(row+item.Label, badge, item.Meta, contentWidth))
			focusables = append(focusables, modal.FocusableInfo{
				ID:      fmt.Sprintf("%s%d", pickerItemPrefix, i),
				OffsetX: 0,
				OffsetY: i - first,
				Width:   contentWidth,
				Height:  1,
			})
		}
		content := truncateLines(strings.Join(lines, "\n"), contentWidth)
		return modal.RenderedSection{Content: content, Focusables: focusables}
	}, func(msg tea.Msg, focusID string) (string, tea.Cmd) {
		key, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return "", nil
		}
		count := len(f.items())
		if count == 0 {
			return "", nil
		}
		switch key.String() {
		case "up", "k", "ctrl+p":
			if f.picker.cursor > 0 {
				f.picker.cursor--
				f.syncScroll()
				f.invalidate()
			}
			return "", nil
		case "down", "j", "ctrl+n":
			if f.picker.cursor < count-1 {
				f.picker.cursor++
				f.syncScroll()
				f.invalidate()
			}
			return "", nil
		}
		return "", nil
	})
	hints := modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		text := "Esc returns to the kind list"
		switch f.kind {
		case KindFile:
			text = "Esc returns to the kind list · path:line accepted"
		case KindIssue:
			text = "Esc returns to the kind list · pasting an id works too"
		case KindResource:
			text = "Esc returns to the kind list · e.g. PROJ-123"
		}
		return modal.RenderedSection{Content: styles.Muted.Render(text)}
	}, nil)
	return []modal.Section{input, count, list, hints}
}

// alignRow lays out one picker row: text on the left, its badge just after it,
// and meta pinned to the right edge so the column stays straight down the list
// rather than following the longest label.
//
// Both the badge and the meta are reserved before the text is fitted, and it is
// the text that gives way. The row used to be assembled left to right and cut
// at the content width, which meant a long title quietly ate its own "[in
// progress]" — dropping the badge from exactly the issues it was there to mark,
// since those are the ones with the most to say in their titles.
func alignRow(text, badge, meta string, contentWidth int) string {
	if contentWidth < 1 {
		return text + badge
	}
	reserved := ansi.StringWidth(badge)
	metaWidth := ansi.StringWidth(meta)
	if meta != "" {
		// At least one space between the badge and the meta column.
		reserved += metaWidth + 1
	}
	maxText := contentWidth - reserved
	if maxText < 1 {
		// Too narrow to hold the furniture: the text identifies the row, so it
		// is what survives.
		return ansi.Truncate(text, contentWidth, "…")
	}
	if ansi.StringWidth(text) > maxText {
		text = ansi.Truncate(text, maxText, "…")
	}
	row := text + badge
	if meta == "" {
		return row
	}
	gap := contentWidth - metaWidth - ansi.StringWidth(row)
	return row + strings.Repeat(" ", gap) + styles.Muted.Render(meta)
}

// truncateLines holds every line of a section's content to contentWidth, the
// same rule the modal library applies to its own sections.
func truncateLines(content string, contentWidth int) string {
	if content == "" || contentWidth < 1 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > contentWidth {
			lines[i] = ansi.Truncate(line, contentWidth, "…")
		}
	}
	return strings.Join(lines, "\n")
}

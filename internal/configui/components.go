package configui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
)

// The Configuration surface aligns every page against one grid: labels occupy
// a fixed left column and their controls start at one shared column, so help
// text, completions, and nested pickers can align to the control rather than to
// the pane edge.
const (
	// LabelColumn is the width of the fixed left label column. It is wide
	// enough for the longest label any page actually uses ("Split workspace
	// terminal", "Terminal preview capture"): a shared column that truncates a
	// real setting's name is a broken column, not a page's problem to work
	// around.
	LabelColumn = 26
	// RowIndent is the left inset shared by rows inside a section.
	RowIndent = 2
	// ControlColumn is the column every control starts at, measured from the
	// pane's content origin.
	ControlColumn = RowIndent + LabelColumn
	// MaxControlWidth is the widest any fixed-width control asks to be drawn.
	MaxControlWidth = 48
	// MaxRowWidth caps the width a row lays its content out in. A row that
	// right-aligns a control — the ON/OFF pill on every panel switch — would
	// otherwise pin it to the pane's right edge, so widening the window drags
	// the pill away from the label it belongs to until the two are reading as
	// separate columns. Capping at the control column plus the widest control
	// puts that right edge exactly where the widest form field already ends, so
	// pills and fields share one edge instead of the pills drifting past it.
	MaxRowWidth = ControlColumn + MaxControlWidth
)

// RowWidth is the width a row lays its content out in: everything the pane has
// to give, but never more than MaxRowWidth. Rows stay left-aligned in the pane
// and simply stop growing, which is what keeps a right-aligned control near its
// label on a wide terminal.
func RowWidth(inner int) int { return min(inner, MaxRowWidth) }

// ControlWidth is how wide a fixed-width control may actually be drawn: the
// width the page asked for, or everything the pane has left after the label
// column, whichever is smaller. A page that insists on its full width in a
// narrow terminal does not get a wide field — it gets a field with its right
// half cut off, which is the one thing the brief's narrow-terminal rule rules
// out.
func ControlWidth(inner, preferred int) int {
	available := inner - ControlColumn
	if available < minControlWidth {
		available = minControlWidth
	}
	return min(preferred, available)
}

// minControlWidth is the narrowest a control is allowed to become before the
// pane is simply too small to render the surface at all.
const minControlWidth = 10

// State is the interaction state of a control. Every control renders rest,
// focus, and hover distinctly so keyboard and mouse describe the same surface.
type State struct {
	Focused bool
	Hovered bool
	// Disabled marks a control the current environment cannot offer.
	Disabled bool
}

// Styles are built per call rather than cached in package vars: the theme is
// swapped at runtime (and previewed live), and styles.* is reassigned when it
// is.

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextMuted)
}

func bodyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextPrimary)
}

func chipStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextPrimary).
		Background(styles.SurfaceRaised).
		Padding(0, 1)
}

func accentChipStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Primary).
		Background(styles.BgTertiary).
		Padding(0, 1).
		Bold(true)
}

// PaneTitle is the page or pane title at the top of a pane, with the breathing
// room the design brief asks for supplied by the caller's blank line.
func PaneTitle(text string) string { return titleStyle().Render(text) }

// SectionHeader titles a group of controls. It carries its own leading blank
// line so sections separate by whitespace rather than by divider lines.
func SectionHeader(text string) string {
	return "\n" + titleStyle().Render(text)
}

// Body renders ordinary primary text.
func Body(text string) string { return bodyStyle().Render(text) }

// Muted renders quieter secondary text.
func Muted(text string) string { return mutedStyle().Render(text) }

// HelpLine renders muted help aligned to the control it belongs to, not to the
// pane edge.
func HelpLine(text string) string {
	return strings.Repeat(" ", ControlColumn) + mutedStyle().Render(text)
}

// FormRow renders a labelled control: the label in the fixed left column, the
// control starting at the shared column.
func FormRow(label, control string, state State) string {
	labelText := label
	if width := ansi.StringWidth(labelText); width > LabelColumn-1 {
		labelText = ansi.Truncate(labelText, LabelColumn-1, "…")
	}
	labelStyle := bodyStyle()
	switch {
	case state.Disabled:
		labelStyle = mutedStyle()
	case state.Focused:
		labelStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case state.Hovered:
		labelStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	}
	pad := LabelColumn - ansi.StringWidth(labelText)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", RowIndent) + labelStyle.Render(labelText) +
		strings.Repeat(" ", pad) + control
}

// Toggle renders an ON/OFF pill. The pill is the control; FormRow supplies the
// label column around it.
func Toggle(on bool, state State) string {
	label := "OFF"
	style := lipgloss.NewStyle().
		Foreground(styles.TextSecondary).
		Background(styles.SurfaceRaised).
		Padding(0, 1)
	if on {
		label = "ON"
		style = lipgloss.NewStyle().
			Foreground(styles.Success).
			Background(styles.BgTertiary).
			Padding(0, 1).
			Bold(true)
	}
	switch {
	case state.Disabled:
		style = style.Foreground(styles.TextMuted)
	case state.Focused:
		style = style.Background(styles.Primary).Foreground(styles.OnPrimaryColor)
	case state.Hovered:
		style = style.Background(styles.BgTertiary)
	}
	return style.Render(label)
}

// ToggleWidth renders an ON/OFF pill that fills a fixed width, so a group of
// toggles and selectors shares one right edge the way the mockups draw them.
func ToggleWidth(on bool, width int, state State) string {
	label := "OFF"
	style := lipgloss.NewStyle().
		Foreground(styles.TextSecondary).
		Background(styles.SurfaceRaised).
		Padding(0, 1)
	if on {
		label = "ON"
		style = lipgloss.NewStyle().
			Foreground(styles.Success).
			Background(styles.BgTertiary).
			Padding(0, 1).
			Bold(true)
	}
	switch {
	case state.Disabled:
		style = style.Foreground(styles.TextMuted)
	case state.Focused:
		style = style.Background(styles.Primary).Foreground(styles.OnPrimaryColor)
	case state.Hovered:
		style = style.Background(styles.BgTertiary)
	}
	return style.Width(width).Render(label)
}

// Selector renders a value with a disclosure arrow.
func Selector(value string, state State) string {
	return SelectorArrow(value, "▾", state)
}

// SelectorArrow renders a selector whose arrow says which way it opens: ▾ for a
// closed disclosure, ▴ for one already expanded below the field.
func SelectorArrow(value, arrow string, state State) string {
	style := chipStyle()
	switch {
	case state.Disabled:
		style = style.Foreground(styles.TextMuted)
	case state.Focused:
		style = accentChipStyle()
	case state.Hovered:
		style = style.Background(styles.BgTertiary)
	}
	return style.Render(value + "  " + arrow)
}

// SelectorWidth renders a selector that fills a fixed width, so several
// selectors in a group share one right edge the way the mockups draw them.
func SelectorWidth(value string, width int, state State) string {
	return SelectorWidthArrow(value, "▾", width, state)
}

// SelectorWidthArrow is SelectorWidth with the arrow the control's own state
// asks for: ▾ while its list is closed, ▴ while it is open over the page.
func SelectorWidthArrow(value, arrow string, width int, state State) string {
	style := chipStyle()
	switch {
	case state.Disabled:
		style = style.Foreground(styles.TextMuted)
	case state.Focused:
		style = accentChipStyle()
	case state.Hovered:
		style = style.Background(styles.BgTertiary)
	}
	text := value + "  " + arrow
	if ansi.StringWidth(text) > width-2 {
		text = ansi.Truncate(text, max(1, width-3), "…")
	}
	return style.Width(width).Render(text)
}

// Button renders an action pill. primary marks the action the page recommends.
func Button(label string, primary bool, state State) string {
	style := chipStyle()
	if primary {
		style = accentChipStyle()
	}
	switch {
	case state.Disabled:
		style = style.Foreground(styles.TextMuted)
	case state.Focused:
		style = lipgloss.NewStyle().
			Foreground(styles.OnPrimaryColor).
			Background(styles.Primary).
			Padding(0, 1).
			Bold(true)
	case state.Hovered:
		style = style.Background(styles.BgTertiary)
	}
	return style.Render(label)
}

// ButtonRow joins rendered buttons into one indented row.
func ButtonRow(buttons ...string) string {
	present := make([]string, 0, len(buttons))
	for _, button := range buttons {
		if button != "" {
			present = append(present, button)
		}
	}
	if len(present) == 0 {
		return ""
	}
	return strings.Repeat(" ", RowIndent) + strings.Join(present, "  ")
}

// Badge renders a row's action label. A badge on a problem row is the accent
// call to action; a badge on a healthy row is a quiet way in, so it must not
// shout at a user who has nothing to fix.
func Badge(text string, urgent bool) string {
	if text == "" {
		return ""
	}
	if urgent {
		return accentChipStyle().Render(text)
	}
	return chipStyle().Render(text)
}

// StatusRow renders a diagnostic row.
//
// A healthy row with nothing to do is one quiet line: marker, label, and its
// detail in the shared control column. A row that offers an action carries a
// badge at the right edge and moves its detail to a second line, so the badge
// and the detail never compete for the same run of space. Both forms are one
// control: the caller registers the whole block.
func StatusRow(ok bool, label, detail, badge string, width int, state State) string {
	marker := lipgloss.NewStyle().Foreground(styles.Success).Render("✓")
	if !ok {
		marker = lipgloss.NewStyle().Foreground(styles.Warning).Render("●")
	}
	labelStyle := bodyStyle()
	switch {
	case state.Focused:
		labelStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case state.Hovered:
		labelStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	}
	left := strings.Repeat(" ", RowIndent) + marker + " " + labelStyle.Render(label)
	if badge == "" {
		if detail != "" {
			pad := ControlColumn - ansi.StringWidth(left)
			if pad < 2 {
				pad = 2
			}
			left += strings.Repeat(" ", pad) + mutedStyle().Render(detail)
		}
		return HighlightRow(left, width, state)
	}
	rendered := Badge(badge, !ok)
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(rendered)
	if pad < 1 {
		pad = 1
	}
	row := left + strings.Repeat(" ", pad) + rendered
	if detail == "" {
		return HighlightRow(row, width, state)
	}
	return HighlightBlock(row+"\n"+strings.Repeat(" ", RowIndent+2)+mutedStyle().Render(detail), width, state)
}

// BetaBadge marks an integration Sidecar is still shipping as a preview. It
// stays on the row after the integration is enabled: turning something on does
// not make it finished.
func BetaBadge() string {
	return lipgloss.NewStyle().
		Foreground(styles.Warning).
		Background(styles.BgTertiary).
		Padding(0, 1).
		Bold(true).
		Render("BETA")
}

// PanelRow is one surface on Panels & Integrations: the whole two-line block is
// the control, with its ON/OFF state as a distinct pill at the right edge. The
// name and the pill share a line so the state is legible at a glance; the
// description sits underneath in the muted column so it never competes with the
// pill for space.
func PanelRow(title, badge, detail, control string, width int, state State) string {
	titleStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	switch {
	case state.Disabled:
		titleStyle = mutedStyle().Bold(true)
	case state.Focused:
		titleStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case state.Hovered:
		titleStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	}
	left := strings.Repeat(" ", RowIndent) + titleStyle.Render(title)
	if badge != "" {
		left += "  " + badge
	}
	// The row is measured in its own capped width, not the pane's: the pill,
	// the wrapped detail, and the selection band all stop at the same edge, so
	// a wide terminal grows the empty space to the right of the row rather than
	// stretching the row itself.
	width = RowWidth(width)
	first := padRight(left, control, width)
	block := first
	if detail != "" {
		block = first + "\n" + WrapAt(detail, width, RowIndent, Muted)
	}
	return HighlightBlock(block, width, state)
}

// Centered places already-styled content in the middle of the pane, for the
// small amount of Configuration that is a signature rather than a control.
func Centered(content string, width int) string {
	pad := (width - ansi.StringWidth(content)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + content
}

// RepairRow is Sidecar Setup's actionable row: a leading badge, the work stated
// as something to do, and a quieter line saying why. The whole two-line block is
// one control.
func RepairRow(badge, title, detail string, width int, state State) string {
	titleStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	switch {
	case state.Focused:
		titleStyle = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	case state.Hovered:
		titleStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
	}
	pill := Badge(badge, true)
	first := " " + pill + "  " + titleStyle.Render(title)
	if pad := width - ansi.StringWidth(first); pad > 0 {
		first += strings.Repeat(" ", pad)
	}
	if detail == "" {
		return HighlightRow(first, width, state)
	}
	return HighlightBlock(first+"\n"+strings.Repeat(" ", RowIndent+5)+mutedStyle().Render(detail), width, state)
}

// Warning states an observed problem in the repair route that fixes it.
func Warning(text string) string {
	return lipgloss.NewStyle().Foreground(styles.Warning).Bold(true).Render(text)
}

// CodeChip renders a command or a literal the user may copy or run.
func CodeChip(text string) string { return accentChipStyle().Render(text) }

// Indented renders body text at the shared row indent.
func Indented(text string) string {
	return strings.Repeat(" ", RowIndent) + bodyStyle().Render(text)
}

// IndentedRaw places already-styled content at the shared row indent.
func IndentedRaw(content string) string {
	return strings.Repeat(" ", RowIndent) + content
}

// IndentedMuted renders quieter text at the shared row indent.
func IndentedMuted(text string) string {
	return strings.Repeat(" ", RowIndent) + mutedStyle().Render(text)
}

// WrapAt lays a paragraph out inside a pane: every line is indented to the same
// column and no line runs past the pane's writable width. It returns one string
// with embedded newlines, which the pane builder splits into painted rows, so
// wrapped help still counts for hit regions and the height clamp.
func WrapAt(text string, inner, indent int, style func(string) string) string {
	width := inner - indent
	if width < 8 {
		width = 8
	}
	pad := strings.Repeat(" ", indent)
	words := strings.Fields(text)
	if len(words) == 0 {
		return pad
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		if ansi.StringWidth(current)+1+ansi.StringWidth(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	lines = append(lines, current)
	for i, line := range lines {
		lines[i] = pad + style(line)
	}
	return strings.Join(lines, "\n")
}

// ListRow renders a focusable row that fills the pane width, so the whole row
// is legibly the control.
func ListRow(text string, width int, state State) string {
	style := lipgloss.NewStyle().Foreground(styles.TextSecondary)
	prefix := "  "
	switch {
	case state.Focused:
		style = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
		prefix = lipgloss.NewStyle().Foreground(styles.Primary).Render("▸ ")
	case state.Hovered:
		style = lipgloss.NewStyle().Foreground(styles.TextPrimary)
	}
	return HighlightRow(prefix+style.Render(text), width, state)
}

// HighlightRow fills a list row so selection is the whole line, not just the
// label. Focused and hovered rows keep their existing text colour and sit on a
// darker surface, which is what makes the cursor readable on a dark theme.
func HighlightRow(content string, width int, state State) string {
	if width < 1 {
		return content
	}
	if !state.Focused && !state.Hovered {
		return padDisplay(content, width)
	}
	return styles.FillBackground(content, width, rowFill(state))
}

// HighlightBlock applies HighlightRow to every line of a multi-line control.
func HighlightBlock(content string, width int, state State) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = HighlightRow(line, width, state)
	}
	return strings.Join(lines, "\n")
}

func rowFill(state State) color.Color {
	if state.Focused {
		return styles.CardSelectedBg
	}
	return styles.BgSecondary
}

// clampStart keeps the beginning of a string and ellipsizes the end.
func clampStart(s string, width int) string {
	if width < 1 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// clampEnd keeps the end of a string and ellipsizes the start, which is what a
// path wants, so the filename stays visible.
//
// ansi.TruncateLeft's count is how many cells to *remove* from the left, not
// how many to keep, and the prefix it adds occupies one of the cells that
// survive. Passing the target width straight through therefore cut a 43-column
// path down to ten columns rather than to thirty-four, which is how the Projects
// page's path column and the Integrations table's files column both ended up
// showing an ellipsis and a suffix.
func clampEnd(s string, width int) string {
	if width < 1 {
		return ""
	}
	total := ansi.StringWidth(s)
	if total <= width {
		return s
	}
	return ansi.TruncateLeft(s, total-width+1, "…")
}

func padDisplay(s string, width int) string {
	if pad := width - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// padRight pushes a right-hand control to the pane's right edge.
func padRight(left, right string, width int) string {
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// BackBar is the top row of a focused child route: the route's own title on the
// left and the visible parent-return control on the right. Escape does the same
// thing the control does.
func BackBar(title, parent string, width int, state State) string {
	left := titleStyle().Render(title)
	control := BackControl(parent, state)
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(control)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + control
}

// BackControl renders the parent-return control on its own, for callers that
// place it outside a bar.
func BackControl(parent string, state State) string {
	label := "←  Back"
	if parent != "" {
		label = "←  Back to " + parent
	}
	return Button(label, false, state)
}

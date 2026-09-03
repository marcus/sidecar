package overview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/termpanes"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
)

// Terminal search is a property of the loaded absolute buffer, not of where
// tmux runs. The only host-specific operation is RequestAll's bounded history
// read, which goes through tty.Model.CaptureRange and therefore stays in-band
// for a remote pane.
type previewTerminalSearchMatch struct {
	Line     int
	StartCol int
	EndCol   int
}

// previewTerminalSearchState is the global browser's terminal `/` bar. Its
// query is the app's shared query field, exactly as the project workspace's is:
// the two surfaces are two projections of one model, so a bar that edited
// differently here would be a bug.
type previewTerminalSearchState struct {
	InputActive bool
	Target      tty.Target
	field       queryfield.Field
	Matches     []previewTerminalSearchMatch
	Current     int
	Generation  uint64
}

// Query is the text typed into the terminal search bar.
func (s *previewTerminalSearchState) Query() string { return s.field.Query() }

// SetQuery replaces the text and puts the caret at its end.
func (s *previewTerminalSearchState) SetQuery(query string) { s.field.SetQuery(query) }

type previewTerminalSearchLoadedMsg struct {
	Target     tty.Target
	Capture    tty.CaptureRange
	RequestGen uint64
	SearchGen  uint64
	Err        error
}

func previewInteractiveSearchKey(msg tea.KeyPressMsg) bool {
	return (msg.Code == 'f' || msg.Code == 'F') &&
		msg.Mod.Contains(tea.ModCtrl) && msg.Mod.Contains(tea.ModShift)
}

func (m *Model) handlePreviewTerminalSearchKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	search := &m.terminalSearch
	if search.InputActive {
		// InputActive is what means "this bar owns the keyboard", so the
		// field's focus is derived from it rather than tracked twice.
		search.field.Focus()
		switch msg.Code {
		case tea.KeyEscape:
			// Esc leaves the bar in one press — the one key the field is never
			// offered, because this bar is a mode over a live terminal rather
			// than a filter beside a list.
			search.InputActive = false
			search.field.Blur()
			return true, nil
		case tea.KeyEnter:
			search.InputActive = false
			search.field.Blur()
			m.recomputePreviewTerminalSearch()
			m.revealPreviewTerminalSearchMatch()
			return true, nil
		}
		// Everything else is the field's: text, the caret keys, word ops, home
		// and end. The bar still consumes what the field refuses, so a stray
		// key cannot reach the terminal underneath.
		before := search.Query()
		search.field.HandleKey(msg)
		if search.Query() != before {
			m.recomputePreviewTerminalSearch()
		}
		return true, nil
	}

	trigger := (!m.PreviewInteractive() && msg.String() == "/") ||
		(m.PreviewInteractive() && previewInteractiveSearchKey(msg))
	if trigger {
		return true, m.beginPreviewTerminalSearch()
	}
	if search.Query() != "" && len(search.Matches) > 0 {
		switch msg.String() {
		case "n":
			search.Current = (search.Current + 1) % len(search.Matches)
			m.revealPreviewTerminalSearchMatch()
			return true, nil
		case "N", "shift+n":
			search.Current = (search.Current - 1 + len(search.Matches)) % len(search.Matches)
			m.revealPreviewTerminalSearchMatch()
			return true, nil
		}
	}
	if search.Query() != "" && msg.Code == tea.KeyEscape {
		m.clearPreviewTerminalSearch()
		return true, nil
	}
	return false, nil
}

func (m *Model) beginPreviewTerminalSearch() tea.Cmd {
	terminal := m.previewTerminalState().terminal
	buffer := m.previewBuffer()
	if terminal == nil || !terminal.IsActive() || buffer == nil {
		return nil
	}
	target := m.previewTarget()
	search := &m.terminalSearch
	if search.Target != target {
		search.field.Reset()
		search.Matches = nil
		search.Current = 0
	}
	search.InputActive = true
	search.field.Focus()
	search.Target = target
	search.Generation++
	searchGen := search.Generation
	if info := terminal.History(); info.HasHistory {
		m.previewTerminalLeaf().History.Record(info.HistorySize)
	}
	base, _, absolute := buffer.AbsoluteRange()
	reach := m.previewTerminalLeaf().History
	request, ok := reach.RequestAll(base, absolute)
	m.previewTerminalLeaf().History = reach
	if !ok {
		m.recomputePreviewTerminalSearch()
		return nil
	}
	capturer, ok := terminal.(previewRangeCapturer)
	if !ok {
		reach.Cancel()
		m.previewTerminalLeaf().History = reach
		return nil
	}
	return func() tea.Msg {
		capture, err := capturer.CaptureRange(request.Start, request.End)
		return previewTerminalSearchLoadedMsg{
			Target: target, Capture: capture, RequestGen: request.Generation,
			SearchGen: searchGen, Err: err,
		}
	}
}

func (m *Model) applyPreviewTerminalSearchHistory(msg previewTerminalSearchLoadedMsg) tea.Cmd {
	if msg.Target != m.previewTarget() || msg.SearchGen != m.terminalSearch.Generation {
		return nil
	}
	reach := m.previewTerminalLeaf().History
	_, ok := reach.Accept(msg.RequestGen)
	if !ok || msg.Err != nil {
		m.previewTerminalLeaf().History = reach
		return nil
	}
	buffer := m.previewBuffer()
	if buffer == nil {
		return nil
	}
	oldBase, _, absolute := buffer.AbsoluteRange()
	if !absolute || !m.previewTerminalState().terminal.PrependHistory(msg.Capture.Output, msg.Capture.StartLine) {
		m.previewTerminalLeaf().History = reach
		return nil
	}
	newBase, _, _ := buffer.AbsoluteRange()
	added := oldBase - newBase
	reach.Settle(newBase, msg.Capture.HistorySize)
	m.previewTerminalLeaf().History = reach
	m.previewTerminalLeaf().Freeze.Rebase(added)
	m.recomputePreviewTerminalSearch()
	return nil
}

func (m *Model) clearPreviewTerminalSearch() {
	if m == nil {
		return
	}
	generation := m.terminalSearch.Generation + 1
	m.terminalSearch = previewTerminalSearchState{Generation: generation}
}

func (m *Model) recomputePreviewTerminalSearch() {
	search := &m.terminalSearch
	search.Matches = search.Matches[:0]
	search.Current = 0
	query := previewSearchGraphemes(search.Query())
	buffer := m.previewBuffer()
	if len(query) == 0 || buffer == nil || search.Target != m.previewTarget() {
		return
	}
	base, _, _ := buffer.AbsoluteRange()
	for row, raw := range buffer.Lines() {
		line := previewSearchGraphemes(ansi.Strip(ui.ExpandTabs(raw, tty.DefaultTabWidth)))
		for from := 0; from+len(query) <= len(line); {
			matched := true
			for i := range query {
				if !strings.EqualFold(line[from+i].Text, query[i].Text) {
					matched = false
					break
				}
			}
			if !matched {
				from++
				continue
			}
			last := from + len(query) - 1
			search.Matches = append(search.Matches, previewTerminalSearchMatch{
				Line: base + row, StartCol: line[from].StartCol,
				EndCol: max(line[last].EndCol-1, line[from].StartCol),
			})
			from += len(query)
		}
	}
}

type previewSearchGrapheme struct {
	Text             string
	StartCol, EndCol int
}

func previewSearchGraphemes(value string) []previewSearchGrapheme {
	var result []previewSearchGrapheme
	state, col := ansi.NormalState, 0
	for len(value) > 0 {
		seq, width, n, next := ansi.GraphemeWidth.DecodeSequenceInString(value, state, nil)
		if n <= 0 {
			break
		}
		if width > 0 {
			result = append(result, previewSearchGrapheme{Text: seq, StartCol: col, EndCol: col + width})
			col += width
		}
		state, value = next, value[n:]
	}
	return result
}

func (m *Model) revealPreviewTerminalSearchMatch() {
	search := &m.terminalSearch
	if len(search.Matches) == 0 || search.Current < 0 || search.Current >= len(search.Matches) {
		return
	}
	buffer := m.previewBuffer()
	if buffer == nil {
		return
	}
	base, _, _ := buffer.AbsoluteRange()
	localLine := search.Matches[search.Current].Line - base
	window := m.previewWindow()
	height := max(window.layout.DisplayHeight, 1)
	maxScroll := m.previewMaxOffset()
	start := min(max(localLine-height/2, 0), maxScroll)
	m.thawPreviewWindow()
	m.previewTerminalLeaf().Scroll = maxScroll - start
}

func (m *Model) appendPreviewTerminalSearchStatus(hints string) string {
	search := &m.terminalSearch
	if search.Target != m.previewTarget() || (search.Query() == "" && !search.InputActive) {
		return hints
	}
	// This bar is a segment of the pane's hint line rather than a full-width
	// row, so it keeps its own shape instead of queryfield.RenderRow — but the
	// caret is drawn where the caret actually is.
	var status string
	if search.InputActive {
		runes := []rune(search.Query())
		caret := min(max(search.field.Cursor(), 0), len(runes))
		status = "/" + string(runes[:caret]) + "▌" + string(runes[caret:])
	} else if len(search.Matches) == 0 {
		status = "no matches"
	} else {
		status = fmt.Sprintf("%d/%d matches · n/N", search.Current+1, len(search.Matches))
	}
	return hints + " " + status
}

func (m *Model) previewTerminalDecorator(leaf *termpanes.Leaf) func(string, int) string {
	return func(line string, absoluteLine int) string {
		line = leaf.LinkState.Decorate(line, absoluteLine)
		search := &m.terminalSearch
		if search.Query() == "" || search.Target != m.previewTarget() {
			return line
		}
		for _, match := range search.Matches {
			if match.Line == absoluteLine {
				line = ui.InjectCharacterRangeBackground(line, match.StartCol, match.EndCol)
			}
		}
		return line
	}
}

// WorkspacesTerminalSearchPaste puts a bracketed paste into the global
// browser's terminal search bar while it is taking text. A query bar is a text
// input, so a paste lands in it exactly as typed characters do, and it is asked
// before the paste is offered to the live pane.
func (m *Model) WorkspacesTerminalSearchPaste(msg tea.PasteMsg) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	search := &m.terminalSearch
	if !search.InputActive {
		return false, nil
	}
	search.field.Focus()
	before := search.Query()
	search.field.HandlePaste(msg)
	if search.Query() != before {
		m.recomputePreviewTerminalSearch()
	}
	return true, nil
}

package notes

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

func layoutTestPlugin(t *testing.T, contents ...string) *Plugin {
	t.Helper()
	p := New()
	p.store = &Store{}
	p.editorTextarea = newEditorTextarea()
	p.width, p.height = 120, 30
	p.listWidth = 30
	p.notePlaces = make(map[string]notePlace)
	p.previewMode = true
	p.markdownView = false
	p.notes = make([]Note, len(contents))
	for i, content := range contents {
		p.notes[i] = Note{ID: string(rune('a' + i)), Content: content}
	}
	if len(p.notes) > 0 {
		p.editorNote = &p.notes[0]
		p.editorTextarea.SetValue(p.notes[0].Content)
		p.previewLines = strings.Split(p.notes[0].Content, "\n")
		if len(p.previewLines) == 0 {
			p.previewLines = []string{""}
		}
	}
	p.updateTextareaDimensions()
	return p
}

func numberedContent(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line"
	}
	return strings.Join(lines, "\n")
}

func TestEditorLayoutGeometryParity(t *testing.T) {
	p := layoutTestPlugin(t, numberedContent(40))

	p.previewMode = true
	viewLay := p.editorLayout()
	viewH, viewW := p.previewViewport()

	p.previewMode = false
	editLay := p.editorLayout()
	p.updateTextareaDimensions()

	if viewLay != editLay {
		t.Fatalf("layout differs by mode: view=%+v edit=%+v", viewLay, editLay)
	}
	if viewLay.wrapColumn != viewW || viewLay.contentHeight != viewH {
		t.Fatalf("previewViewport (%d,%d) != layout (h=%d wrap=%d)", viewH, viewW, viewLay.contentHeight, viewLay.wrapColumn)
	}
	if p.editorTextarea.Width() != viewLay.wrapColumn {
		t.Fatalf("textarea width %d, want wrap column %d", p.editorTextarea.Width(), viewLay.wrapColumn)
	}
	if p.editorTextarea.Height() != viewLay.contentHeight {
		t.Fatalf("textarea height %d, want content height %d", p.editorTextarea.Height(), viewLay.contentHeight)
	}
	if viewLay.scrollbarCol != viewLay.innerWidth-viewLay.rightMargin-1 {
		t.Fatalf("scrollbar col %d does not leave right margin %+v", viewLay.scrollbarCol, viewLay)
	}
	if viewLay.leftMargin != editorSideCols || viewLay.rightMargin != editorSideCols {
		t.Fatalf("body margins = (%d,%d), want %d each", viewLay.leftMargin, viewLay.rightMargin, editorSideCols)
	}
	if viewLay.statusRow != 0 {
		t.Fatalf("status row %d, want 0", viewLay.statusRow)
	}
	if viewLay.contentRow != editorStatusRows+editorTopRows {
		t.Fatalf("content row %d, want %d (status + %d blank rows)", viewLay.contentRow, editorStatusRows+editorTopRows, editorTopRows)
	}
}

func firstBodyGlyph(pane string) (row, col int) {
	lines := strings.Split(pane, "\n")
	for i := 1; i < len(lines); i++ {
		for j, r := range lines[i] {
			if r != ' ' {
				return i, j
			}
		}
	}
	return -1, -1
}

func TestMarkdownAndEditShareBodyOrigin(t *testing.T) {
	p := layoutTestPlugin(t, "Hello notes body")
	p.markdownView = true
	l := p.editorLayout()

	p.previewMode = true
	p.invalidateViewSurface()
	mdRow, mdCol := firstBodyGlyph(ansi.Strip(p.renderEditorPane(p.height-2, l.innerWidth)))

	p.previewMode = false
	p.updateTextareaDimensions()
	edRow, edCol := firstBodyGlyph(ansi.Strip(p.renderEditorPane(p.height-2, l.innerWidth)))

	if mdRow < 0 || edRow < 0 {
		t.Fatalf("missing body glyph markdown=(%d,%d) edit=(%d,%d)", mdRow, mdCol, edRow, edCol)
	}
	if mdRow != edRow || mdCol != edCol {
		t.Fatalf("body origin markdown=(%d,%d) edit=(%d,%d)", mdRow, mdCol, edRow, edCol)
	}
	if mdCol != l.leftMargin {
		t.Fatalf("body col %d, want leftMargin %d", mdCol, l.leftMargin)
	}
	if mdRow != l.contentRow {
		t.Fatalf("body row %d, want contentRow %d", mdRow, l.contentRow)
	}
}

func TestMarkdownAndEditShareWrapWidth(t *testing.T) {
	p := layoutTestPlugin(t, strings.Repeat("wrapme ", 48))
	p.markdownView = true
	l := p.editorLayout()

	p.previewMode = true
	p.invalidateViewSurface()
	mdMax := maxBodyLineWidth(ansi.Strip(p.renderEditorPane(p.height-2, l.innerWidth)), l)

	p.previewMode = false
	p.updateTextareaDimensions()
	edMax := maxBodyLineWidth(ansi.Strip(p.renderEditorPane(p.height-2, l.innerWidth)), l)

	if mdMax != edMax {
		t.Fatalf("wrap width markdown=%d edit=%d", mdMax, edMax)
	}
	if mdMax < 1 || mdMax > l.wrapColumn {
		t.Fatalf("shared wrap width %d outside wrapColumn %d", mdMax, l.wrapColumn)
	}
}

func maxBodyLineWidth(pane string, l editorLayout) int {
	lines := strings.Split(pane, "\n")
	maxWidth := 0
	end := l.contentRow + l.contentHeight
	if end > len(lines) {
		end = len(lines)
	}
	for i := l.contentRow; i < end; i++ {
		line := lines[i]
		if l.leftMargin < len(line) {
			line = line[l.leftMargin:]
		}
		if ansi.StringWidth(line) > l.wrapColumn {
			line = ansi.Truncate(line, l.wrapColumn, "")
		}
		if w := ansi.StringWidth(strings.TrimRight(line, " ")); w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

func TestEditorPaneAppliesSharedBreathingRoom(t *testing.T) {
	p := layoutTestPlugin(t, "body")
	l := p.editorLayout()

	for _, preview := range []bool{true, false} {
		p.previewMode = preview
		got := ansi.Strip(p.renderEditorPane(p.height-2, l.innerWidth))
		lines := strings.Split(got, "\n")
		if len(lines) != p.height-paneChromeY {
			t.Fatalf("preview=%v rendered %d inner rows, want %d", preview, len(lines), p.height-paneChromeY)
		}
		for row := 1; row < l.contentRow; row++ {
			if strings.TrimSpace(lines[row]) != "" {
				t.Fatalf("preview=%v row %d above body is not blank: %q", preview, row, lines[row])
			}
		}
		wantPrefix := strings.Repeat(" ", l.leftMargin) + "body"
		if !strings.HasPrefix(lines[l.contentRow], wantPrefix) {
			t.Fatalf("preview=%v body lacks left inset at row %d: %q", preview, l.contentRow, lines[l.contentRow])
		}
		if ansi.StringWidth(lines[l.contentRow]) != l.innerWidth {
			t.Fatalf("preview=%v body row width %d, want %d: %q", preview, ansi.StringWidth(lines[l.contentRow]), l.innerWidth, lines[l.contentRow])
		}
		if strings.TrimSpace(lines[len(lines)-1]) != "" {
			t.Fatalf("preview=%v bottom row is not blank: %q", preview, lines[len(lines)-1])
		}
	}
}

func TestModeSwitchCarriesSourceLine(t *testing.T) {
	p := layoutTestPlugin(t, numberedContent(40))
	p.previewMode = true
	p.previewCursorLine = 12
	p.previewScrollOff = 8

	_ = p.enterEditAtPreviewPlace()
	if p.previewMode {
		t.Fatal("enter edit left preview mode on")
	}
	if got := p.editorTextarea.Line(); got != 12 {
		t.Fatalf("edit cursor line %d, want 12", got)
	}
	if got := p.editorTextarea.ScrollYOffset(); got < 6 || got > 10 {
		t.Fatalf("enter edit YOffset=%d, want near previewScrollOff=8", got)
	}

	p.editorTextarea.MoveToBegin()
	p.setTextareaCursorPosition(15, 0)
	p.trackTextareaScroll()
	p.captureEditPlace()
	p.previewMode = true

	if p.previewCursorLine != 15 {
		t.Fatalf("preview cursor %d, want 15 (line being edited)", p.previewCursorLine)
	}
	l := p.editorLayout()
	visibleEnd := p.previewScrollOff + l.contentHeight
	if p.previewCursorLine < p.previewScrollOff || p.previewCursorLine >= visibleEnd {
		t.Fatalf("preview scroll %d does not show edit line 15 (height %d)", p.previewScrollOff, l.contentHeight)
	}
}

func TestEnterEditDeepScrollKeepsCursorOnScreen(t *testing.T) {
	p := layoutTestPlugin(t, numberedContent(80))
	p.previewMode = true
	p.previewCursorLine = 40
	p.previewScrollOff = 35

	_ = p.enterEditAtPreviewPlace()
	if got := p.editorTextarea.Line(); got != 40 {
		t.Fatalf("edit cursor line %d, want 40", got)
	}
	l := p.editorLayout()
	off := p.editorTextarea.ScrollYOffset()
	if off == 0 {
		t.Fatal("enter edit left YOffset=0; preview row was 5, not the top of the note")
	}
	if got := p.editorTextarea.Line(); got < off || got >= off+l.contentHeight {
		t.Fatalf("cursor line 40 not visible after enter (YOffset=%d height=%d)", off, l.contentHeight)
	}
}

func TestPerNotePlaceMemory(t *testing.T) {
	p := layoutTestPlugin(t, numberedContent(40), numberedContent(10))
	p.cursor = 0
	p.previewMode = true
	p.loadNoteIntoEditor()
	p.previewCursorLine = 12
	p.previewScrollOff = 8
	p.ensurePreviewCursorVisible()

	p.cursor = 1
	p.loadNoteIntoEditor()
	if p.editorNote == nil || p.editorNote.ID != p.notes[1].ID {
		t.Fatalf("expected note B, got %+v", p.editorNote)
	}
	if p.previewScrollOff == 8 && p.previewCursorLine == 12 {
		t.Fatal("note B inherited note A's place")
	}

	p.cursor = 0
	p.loadNoteIntoEditor()
	if p.editorNote == nil || p.editorNote.ID != p.notes[0].ID {
		t.Fatalf("expected note A, got %+v", p.editorNote)
	}
	if p.previewCursorLine != 12 {
		t.Fatalf("note A cursor %d, want 12", p.previewCursorLine)
	}
	if p.previewScrollOff != 8 {
		t.Fatalf("note A scroll %d, want 8", p.previewScrollOff)
	}
	last := len(p.previewLines) - 1
	if p.previewCursorLine == last {
		t.Fatal("note A restored at end-of-note")
	}
}

func TestNewNoteOpensAtEnd(t *testing.T) {
	p := layoutTestPlugin(t, numberedContent(12))
	p.cursor = 0
	p.loadNoteIntoEditorAtEnd()
	if p.previewMode {
		t.Fatal("new note should open in edit mode")
	}
	want := p.editorTextarea.LineCount() - 1
	if got := p.editorTextarea.Line(); got != want {
		t.Fatalf("new-note cursor line %d, want last line %d", got, want)
	}
}

func TestSelectionDoesNotSwapRenderer(t *testing.T) {
	p := layoutTestPlugin(t, "alpha\nbeta\ngamma")
	p.previewMode = false
	p.activePane = PaneEditor
	p.updateTextareaDimensions()
	beforeW, beforeH := p.editorTextarea.Width(), p.editorTextarea.Height()

	p.selection.SelectRange(
		ui.SelectionPoint{Line: 0, Col: 0},
		ui.SelectionPoint{Line: 0, Col: 3},
		false,
	)
	if !p.selection.HasSelection() {
		t.Fatal("expected an active selection")
	}

	// Desync previewLines so a renderer swap would be visible in the output.
	p.previewLines = []string{"XXX", "YYY", "ZZZ"}
	out := p.renderEditorPane(p.height-2, 80)
	if p.previewMode {
		t.Fatal("selection flipped previewMode on")
	}
	if strings.Contains(out, "XXX") {
		t.Fatalf("selection swapped to preview renderer: %q", out)
	}
	// "alpha" is split by the selection overlay; an unselected line is intact.
	if !strings.Contains(out, "beta") {
		t.Fatalf("edit+selection did not draw textarea: %q", out)
	}
	if p.editorTextarea.Width() != beforeW || p.editorTextarea.Height() != beforeH {
		t.Fatalf("textarea geometry changed: %dx%d -> %dx%d",
			beforeW, beforeH, p.editorTextarea.Width(), p.editorTextarea.Height())
	}

	// Re-render in preview for contrast: that path is previewMode, not selection.
	p.selection.Clear()
	p.previewMode = true
	_ = p.renderEditorPane(p.height-2, 80)
	if !p.previewMode {
		t.Fatal("preview render cleared previewMode")
	}
}

func TestPreviewTruncationIsRuneSafe(t *testing.T) {
	// 世 is 3 UTF-8 bytes; a byte slice at wrapWidth-in-bytes would split it.
	line := "ab世界cd"
	got := truncatePreviewLine(line, 3)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated %q is not valid UTF-8: %q", line, got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncated %q contains replacement rune: %q", line, got)
	}
	if ansiWidth := len([]rune(got)); ansiWidth == 0 {
		t.Fatal("truncated line is empty")
	}

	p := layoutTestPlugin(t, line)
	p.previewMode = true
	p.markdownView = false
	p.previewWrapEnabled = false
	p.previewLines = []string{line}
	p.invalidateViewSurface()
	out := p.renderPreviewContent(4, 3)
	if !utf8.ValidString(out) {
		t.Fatalf("preview render is not valid UTF-8: %q", out)
	}
	if strings.Contains(out, "~") {
		t.Fatalf("preview still draws ~ filler: %q", out)
	}
}

func TestPreviewHasNoGutter(t *testing.T) {
	p := layoutTestPlugin(t, "hello")
	p.previewMode = true
	p.markdownView = false
	p.previewWrapEnabled = false
	p.previewLines = []string{"hello"}
	p.invalidateViewSurface()
	out := p.renderPreviewContent(3, 20)
	if strings.Contains(out, "~") {
		t.Fatalf("preview still draws ~ filler: %q", out)
	}
	// A line-number gutter would prefix the first content line with digits.
	first := strings.Split(out, "\n")[0]
	if strings.HasPrefix(strings.TrimLeft(first, " "), "1 ") {
		t.Fatalf("preview still draws a line-number gutter: %q", first)
	}
}

func TestEditorStatusHeaderKeepsSaveStateAtNarrowWidths(t *testing.T) {
	p := layoutTestPlugin(t, "body")
	p.editorNote.CreatedAt = time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	p.editorNote.UpdatedAt = p.editorNote.CreatedAt
	p.previewMode = true

	for _, width := range []int{12, 24, 40} {
		got := p.renderEditorStatusHeader(width)
		plain := ansi.Strip(got)
		if !strings.HasSuffix(plain, "•") {
			t.Fatalf("width %d lost saved symbol: %q", width, plain)
		}
		if gotWidth := ansi.StringWidth(got); gotWidth != width {
			t.Fatalf("width %d rendered %d cells: %q", width, gotWidth, plain)
		}
	}

	p.editorDirty = true
	got := ansi.Strip(p.renderEditorStatusHeader(1))
	if got != "*" {
		t.Fatalf("one-cell dirty header = %q, want unsaved star", got)
	}
	p.saveErr = errors.New("disk full")
	got = ansi.Strip(p.renderEditorStatusHeader(1))
	if got != "!" {
		t.Fatalf("one-cell failed header = %q, want failure mark", got)
	}
}

func TestListHeaderCountFilterAndSpacing(t *testing.T) {
	p := layoutTestPlugin(t)
	p.notes = make([]Note, 14)
	for i := range p.notes {
		p.notes[i] = Note{ID: fmt.Sprintf("n-%d", i), Title: fmt.Sprintf("note %d", i)}
	}
	p.viewFilter = FilterActive

	plain := strings.Split(ansi.Strip(p.renderListPane(10)), "\n")
	fields := strings.Fields(plain[0])
	if !strings.HasPrefix(plain[0], "Notes") || !strings.Contains(plain[0], "14") || !strings.Contains(plain[0], "Active") || !strings.Contains(plain[0], "+ New") {
		t.Fatalf("header is not title-left/count+state+new-right: %q fields=%v", plain[0], fields)
	}
	if strings.Contains(plain[0], "(14)") {
		t.Fatalf("count retained parentheses: %q", plain[0])
	}
	if plain[1] != "" {
		t.Fatalf("row below title is not blank: %q", plain[1])
	}

	p.searchField.SetQuery("note 1")
	p.updateFilteredNotes()
	header := ansi.Strip(p.listHeader(p.listWidth-paneChromeX, len(p.getDisplayNotes())).view)
	if !strings.Contains(header, fmt.Sprintf("%d/14", len(p.getDisplayNotes()))) {
		t.Fatalf("search count is ambiguous: %q", header)
	}
}

func TestFilterPillColorsFollowThemeRoles(t *testing.T) {
	p := layoutTestPlugin(t)
	want := map[NoteFilter]any{
		FilterActive:   styles.Success,
		FilterArchived: styles.Info,
		FilterDeleted:  styles.Error,
	}
	for filter, color := range want {
		p.viewFilter = filter
		if got := fmt.Sprint(p.noteFilterStyle().GetForeground()); got != fmt.Sprint(color) {
			t.Fatalf("%s pill color = %s, want %v", filter, got, color)
		}
	}
	textStyle := p.editorTextarea.Styles().Focused.Text
	if got, want := fmt.Sprint(textStyle.GetForeground()), fmt.Sprint(styles.Body.GetForeground()); got != want {
		t.Fatalf("built-in editor text color = %s, preview body = %s", got, want)
	}

	originalTheme := styles.GetCurrentThemeName()
	t.Cleanup(func() { styles.ApplyTheme(originalTheme) })
	styles.ApplyTheme("dracula")
	_, _ = p.Update(app.ThemeChangedMsg{})
	textStyle = p.editorTextarea.Styles().Focused.Text
	if got, want := fmt.Sprint(textStyle.GetForeground()), fmt.Sprint(styles.Body.GetForeground()); got != want {
		t.Fatalf("theme change left editor text at %s, preview body moved to %s", got, want)
	}
}

func TestFilterShortcutsToggleAndPillCycles(t *testing.T) {
	p := layoutTestPlugin(t)
	p.ctx = &plugin.Context{Epoch: 1, ProjectRoot: t.TempDir()}
	p.notes = []Note{} // exercise shortcuts even when the current filter is empty

	p.viewFilter = FilterArchived
	_, _ = p.handleKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if p.viewFilter != FilterActive {
		t.Fatalf("a from Archived selected %s, want Active", p.viewFilter)
	}
	p.viewFilter = FilterDeleted
	_, _ = p.handleKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if p.viewFilter != FilterActive {
		t.Fatalf("x from Deleted selected %s, want Active", p.viewFilter)
	}

	p.viewFilter = FilterActive
	p.registerMouseRegions()
	var region *mouse.Region
	for _, candidate := range p.mouseHandler.HitMap.Regions() {
		if candidate.ID == regionListFilter {
			copy := candidate
			region = &copy
			break
		}
	}
	if region == nil {
		t.Fatal("state pill has no mouse hit region")
	}
	header := p.listHeader(p.listWidth-paneChromeX, len(p.getDisplayNotes()))
	if region.Rect.X != 2+header.filterX || region.Rect.Y != 1 || region.Rect.W != header.filterWidth {
		t.Fatalf("pill hit region %+v does not match painted header %+v", region.Rect, header)
	}
	_, _ = p.handleMouseClick(mouse.MouseAction{Region: region})
	if p.viewFilter != FilterArchived {
		t.Fatalf("first pill click selected %s, want Archived", p.viewFilter)
	}
	_, _ = p.handleMouseClick(mouse.MouseAction{Region: region})
	if p.viewFilter != FilterDeleted {
		t.Fatalf("second pill click selected %s, want Deleted", p.viewFilter)
	}
	_, _ = p.handleMouseClick(mouse.MouseAction{Region: region})
	if p.viewFilter != FilterActive {
		t.Fatalf("third pill click selected %s, want Active", p.viewFilter)
	}
}

func TestNewNoteHeaderButtonCreatesAndStaysRightOfFilter(t *testing.T) {
	p := layoutTestPlugin(t)
	p.ctx = &plugin.Context{Epoch: 1, ProjectRoot: t.TempDir()}
	p.store = openTestStore(t)
	p.viewFilter = FilterActive
	p.registerMouseRegions()
	header := p.listHeader(p.listWidth-paneChromeX, len(p.getDisplayNotes()))
	if header.newWidth < 1 || header.newX <= header.filterX {
		t.Fatalf("new-note control is not right of the filter: %+v", header)
	}
	var region *mouse.Region
	for _, candidate := range p.mouseHandler.HitMap.Regions() {
		if candidate.ID == regionNewNote {
			copy := candidate
			region = &copy
			break
		}
	}
	if region == nil {
		t.Fatal("+ New Note has no mouse hit region")
	}
	if region.Rect.X != 2+header.newX || region.Rect.W != header.newWidth {
		t.Fatalf("new-note hit region %+v does not match painted header %+v", region.Rect, header)
	}
	_, cmd := p.handleMouseClick(mouse.MouseAction{Region: region})
	if cmd == nil {
		t.Fatal("new-note click scheduled no create")
	}
	if p.mutation == nil || p.mutation.kind != noteMutationCreate || p.activePane != PaneEditor || p.previewMode {
		t.Fatalf("new-note click did not enter optimistic create: mutation=%+v pane=%v preview=%v", p.mutation, p.activePane, p.previewMode)
	}

	p.viewFilter = FilterArchived
	archived := p.listHeader(p.listWidth-paneChromeX, 0)
	if archived.newWidth != 0 {
		t.Fatalf("archived header still painted new-note: %+v", archived)
	}
}

func TestFilterCommandsAdvertiseToggleBindings(t *testing.T) {
	p := layoutTestPlugin(t)
	p.activePane = PaneList
	for _, tc := range []struct {
		filter NoteFilter
		id     string
		name   string
	}{
		{FilterActive, "show-archived", "Archived"},
		{FilterArchived, "show-archived", "Active"},
		{FilterDeleted, "show-deleted", "Active"},
	} {
		p.viewFilter = tc.filter
		found := false
		for _, command := range p.Commands() {
			if command.ID == tc.id {
				found = true
				if command.Name != tc.name || command.Context != "notes-list" {
					t.Fatalf("filter=%s command=%s got name/context %q/%q", tc.filter, tc.id, command.Name, command.Context)
				}
			}
		}
		if !found {
			t.Fatalf("filter=%s did not advertise %s", tc.filter, tc.id)
		}
	}
}

func TestNoteRowUnicodeTitleIsCellSafe(t *testing.T) {
	p := layoutTestPlugin(t, "body")
	note := Note{Title: strings.Repeat("世界", 40)}

	for _, selected := range []bool{false, true} {
		got := p.renderNoteRow(note, selected, 24)
		if !utf8.ValidString(got) {
			t.Fatalf("selected=%v produced invalid UTF-8: %q", selected, got)
		}
		if width := ansi.StringWidth(got); width > 24 {
			t.Fatalf("selected=%v rendered %d cells, want <= 24: %q", selected, width, got)
		}
	}
}

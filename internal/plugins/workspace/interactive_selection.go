package workspace

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	app "github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/mouse"
)

func (p *Plugin) clearInteractiveSelection() {
	p.interactiveSelectionActive = false
	p.interactiveSelectionStart = -1
	p.interactiveSelectionEnd = -1
	p.interactiveSelectionAnchor = -1
	p.interactiveSelectionRect = mouse.Rect{}
}

func (p *Plugin) hasInteractiveSelection() bool {
	return p.interactiveSelectionStart >= 0 && p.interactiveSelectionEnd >= 0
}

func (p *Plugin) isInteractiveLineSelected(lineIdx int) bool {
	if !p.hasInteractiveSelection() {
		return false
	}
	start := p.interactiveSelectionStart
	end := p.interactiveSelectionEnd
	if start > end {
		start, end = end, start
	}
	return lineIdx >= start && lineIdx <= end
}

func (p *Plugin) interactiveLineIndexAtY(y int) (int, bool) {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return 0, false
	}
	if p.interactiveSelectionRect.W == 0 || p.interactiveSelectionRect.H == 0 {
		return 0, false
	}
	if p.interactiveState.VisibleEnd <= p.interactiveState.VisibleStart {
		return 0, false
	}

	contentRow := y - p.interactiveSelectionRect.Y - 1 // account for top border
	if contentRow < 0 {
		return 0, false
	}
	if p.interactiveState.ContentRowOffset <= 0 {
		return 0, false
	}
	outputRow := contentRow - p.interactiveState.ContentRowOffset
	if outputRow < 0 {
		return 0, false
	}
	lineIdx := p.interactiveState.VisibleStart + outputRow
	if lineIdx < p.interactiveState.VisibleStart || lineIdx >= p.interactiveState.VisibleEnd {
		return 0, false
	}
	return lineIdx, true
}

// prepareInteractiveDrag stores the click position and starts drag tracking
// without initializing selection. Selection only activates on actual drag motion.
func (p *Plugin) prepareInteractiveDrag(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}
	p.interactiveSelectionRect = action.Region.Rect
	lineIdx, ok := p.interactiveLineIndexAtY(action.Y)
	if !ok {
		p.clearInteractiveSelection()
		return nil
	}

	p.interactiveSelectionActive = false
	p.interactiveSelectionStart = -1
	p.interactiveSelectionEnd = -1
	p.interactiveSelectionAnchor = lineIdx
	p.autoScrollOutput = false

	p.mouseHandler.StartDrag(action.X, action.Y, regionPreviewPane, lineIdx)
	return nil
}

func (p *Plugin) handleInteractiveSelectionDrag(action mouse.MouseAction) tea.Cmd {
	lineIdx, ok := p.interactiveLineIndexAtY(action.Y)
	if !ok {
		return nil
	}

	// First drag motion: initialize selection from anchor
	if p.interactiveSelectionStart < 0 {
		p.interactiveSelectionStart = p.interactiveSelectionAnchor
		p.interactiveSelectionEnd = p.interactiveSelectionAnchor
	}

	p.interactiveSelectionActive = true

	if lineIdx < p.interactiveSelectionAnchor {
		p.interactiveSelectionStart = lineIdx
		p.interactiveSelectionEnd = p.interactiveSelectionAnchor
	} else {
		p.interactiveSelectionStart = p.interactiveSelectionAnchor
		p.interactiveSelectionEnd = lineIdx
	}
	return nil
}

func (p *Plugin) finishInteractiveSelection() tea.Cmd {
	if p.interactiveSelectionStart < 0 {
		// No drag occurred — click without motion, clear state
		p.clearInteractiveSelection()
		return nil
	}
	p.interactiveSelectionActive = false
	return nil
}

func (p *Plugin) interactiveOutputBuffer() *OutputBuffer {
	if p.shellSelected {
		shell := p.getSelectedShell()
		if shell != nil && shell.Agent != nil {
			return shell.Agent.OutputBuf
		}
		return nil
	}
	wt := p.selectedWorktree()
	if wt == nil || wt.Agent == nil {
		return nil
	}
	return wt.Agent.OutputBuf
}

func (p *Plugin) interactiveSelectionLines() []string {
	if !p.hasInteractiveSelection() {
		return nil
	}
	buf := p.interactiveOutputBuffer()
	if buf == nil {
		return nil
	}

	lineCount := buf.LineCount()
	if lineCount == 0 {
		return nil
	}

	start := p.interactiveSelectionStart
	end := p.interactiveSelectionEnd
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end >= lineCount {
		end = lineCount - 1
	}
	if end < start {
		return nil
	}
	return buf.LinesRange(start, end+1)
}

func (p *Plugin) interactiveVisibleLines() []string {
	if p.interactiveState == nil || !p.interactiveState.Active {
		return nil
	}
	buf := p.interactiveOutputBuffer()
	if buf == nil {
		return nil
	}
	start := p.interactiveState.VisibleStart
	end := p.interactiveState.VisibleEnd
	if end <= start {
		return nil
	}
	return buf.LinesRange(start, end)
}

func (p *Plugin) copyInteractiveSelectionCmd() tea.Cmd {
	return func() tea.Msg {
		lines := p.interactiveSelectionLines()
		if len(lines) == 0 {
			lines = p.interactiveVisibleLines()
		}
		if len(lines) == 0 {
			return app.ToastMsg{Message: "No output to copy", Duration: 2 * time.Second}
		}

		stripped := make([]string, 0, len(lines))
		for _, line := range lines {
			stripped = append(stripped, ansi.Strip(line))
		}
		text := strings.Join(stripped, "\n")
		if err := clipboard.WriteAll(text); err != nil {
			return app.ToastMsg{Message: "Copy failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}

		return app.ToastMsg{Message: fmt.Sprintf("Copied %d line(s)", len(stripped)), Duration: 2 * time.Second}
	}
}

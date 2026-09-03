package workspace

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/tty"
)

// The reach itself — one request per bound-hit, a pending scroll coalesced onto
// it, a superseded result ignored, a hard stop at tmux's oldest line — is the
// shared layer's (tty.HistoryReach). What is left here is the adapter: which
// buffer and which tmux target a surface is reading, and rebasing the absolute
// coordinates a prepend renumbered.

type terminalHistorySource struct {
	Key    string
	Target string
	Buffer *tty.OutputBuffer
	LeafID int
}

type terminalHistoryLoadedMsg struct {
	Source     terminalHistorySource
	Capture    tty.CaptureRange
	RequestGen uint64
	Err        error
}

func terminalHistoryKey(kind, target string) string {
	return kind + ":" + target
}

func (p *Plugin) recordTerminalHistory(kind, target string, historySize int) {
	if target == "" || historySize < 0 {
		return
	}
	if p.terminalHistory == nil {
		p.terminalHistory = make(map[string]tty.HistoryReach)
	}
	key := terminalHistoryKey(kind, target)
	state := p.terminalHistory[key]
	state.Record(historySize)
	p.terminalHistory[key] = state
	if p.terminalSearch.SourceKey == key && p.terminalSearch.Query() != "" {
		p.recomputeTerminalSearch()
	}
}

func (p *Plugin) terminalHistoryFor(termPanel bool) (terminalHistorySource, bool) {
	if termPanel {
		target := p.requireShellTermPane().PaneID
		if target == "" {
			target = p.requireShellTermPane().Session
		}
		if target == "" || p.requireShellTermPane().Buffer == nil {
			return terminalHistorySource{}, false
		}
		return terminalHistorySource{
			Key:    terminalHistoryKey("panel", p.requireShellTermPane().Session),
			Target: target,
			Buffer: p.requireShellTermPane().Buffer,
			LeafID: p.terminalLeafID(true),
		}, true
	}
	if p.selectingShell() {
		shell := p.getSelectedShell()
		if shell == nil || shell.Agent == nil || shell.Agent.OutputBuf == nil {
			return terminalHistorySource{}, false
		}
		target := shell.Agent.TmuxPane
		if target == "" {
			target = shell.TmuxName
		}
		return terminalHistorySource{
			Key:    terminalHistoryKey("shell", shell.TmuxName),
			Target: target,
			Buffer: shell.Agent.OutputBuf,
			LeafID: p.terminalLeafID(false),
		}, true
	}
	wt := p.selectedWorktree()
	if wt == nil || wt.Agent == nil || wt.Agent.OutputBuf == nil {
		return terminalHistorySource{}, false
	}
	target := wt.Agent.TmuxPane
	if target == "" {
		target = wt.Agent.TmuxSession
	}
	return terminalHistorySource{
		Key:    terminalHistoryKey("agent", wt.Agent.TmuxSession),
		Target: target,
		Buffer: wt.Agent.OutputBuf,
		LeafID: p.terminalLeafID(false),
	}, true
}

// loadOlderTerminalHistory fetches only the range immediately preceding the
// currently loaded buffer. scrollLines is replayed after the async prepend, and
// a reader who has run out of tmux history is told so rather than left pushing
// against a bound with no explanation.
func (p *Plugin) loadOlderTerminalHistory(termPanel bool, scrollLines int) tea.Cmd {
	source, ok := p.terminalHistoryFor(termPanel)
	if !ok {
		return nil
	}
	ownership := p.currentTerminalOwnership()
	if ownership == 0 {
		return nil
	}
	// A reader can reach the bound before any capture has recorded a reach for
	// this pane, and the request state is what remembers that they did.
	if p.terminalHistory == nil {
		p.terminalHistory = make(map[string]tty.HistoryReach)
	}
	state := p.terminalHistory[source.Key]
	base, _, absolute := source.Buffer.AbsoluteRange()
	request, outcome := state.Request(base, absolute, scrollLines)
	p.terminalHistory[source.Key] = state
	switch outcome {
	case tty.HistoryRequested:
	case tty.HistoryEnded:
		return p.noteTerminalHistoryEnd(source.Key)
	default:
		return nil
	}
	return func() tea.Msg {
		return p.withTerminalOwnership(ownership, func() tea.Msg {
			capture, err := workspaceCapturePaneRange(source.Target, request.Start, request.End)
			return terminalHistoryLoadedMsg{
				Source:     source,
				Capture:    capture,
				RequestGen: request.Generation,
				Err:        err,
			}
		})
	}
}

// noteTerminalHistoryEnd says out loud that tmux has no more history for this
// pane. It is the same sentence the global browser says, because it is a fact
// about the pane rather than about the surface reading it.
func (p *Plugin) noteTerminalHistoryEnd(key string) tea.Cmd {
	state := p.terminalHistory[key]
	told := state.NoteEnd()
	p.terminalHistory[key] = state
	if !told {
		return nil
	}
	// A scroll boundary is worth saying and not worth keeping (audit row 41).
	return appmsg.ShowFlash(tty.HistoryExhaustedNotice)
}

func (p *Plugin) applyTerminalHistory(msg terminalHistoryLoadedMsg) tea.Cmd {
	state := p.terminalHistory[msg.Source.Key]
	scrollLines, ok := state.Accept(msg.RequestGen)
	if !ok {
		return nil
	}
	if msg.Err != nil {
		p.terminalHistory[msg.Source.Key] = state
		if p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Debug("terminal history capture failed", "source", msg.Source.Key, "err", msg.Err)
		}
		return nil
	}
	termPanel := p.terminalPaneIsPanel(msg.Source.LeafID)
	current, ok := p.terminalHistoryFor(termPanel)
	if !ok || current.Key != msg.Source.Key || current.Buffer != msg.Source.Buffer {
		p.terminalHistory[msg.Source.Key] = state
		return nil
	}
	oldBase, _, ok := current.Buffer.AbsoluteRange()
	if !ok {
		p.terminalHistory[msg.Source.Key] = state
		return nil
	}
	prepended := false
	if model := p.terminalModelForHistorySource(current); model != nil {
		prepended = model.PrependHistory(msg.Capture.Output, msg.Capture.StartLine)
	} else {
		prepended = current.Buffer.PrependSnapshot(msg.Capture.Output, msg.Capture.StartLine)
	}
	if !prepended {
		p.terminalHistory[msg.Source.Key] = state
		return nil
	}
	newBase, _, _ := current.Buffer.AbsoluteRange()
	added := oldBase - newBase
	state.Settle(newBase, msg.Capture.HistorySize)
	remainder, more := state.Remainder(scrollLines, added)
	p.terminalHistory[msg.Source.Key] = state
	if p.terminalSearch.SourceKey == msg.Source.Key && p.terminalSearch.Query() != "" {
		p.recomputeTerminalSearch()
	}

	if termPanel {
		// A pinned window names an absolute row, which the prepend just renumbered.
		p.requireShellTermPane().Freeze.Rebase(added)
		p.requireShellTermPane().Scroll = min(p.requireShellTermPane().Scroll+scrollLines, p.terminalMaxScroll(true))
		if more {
			return p.loadOlderTerminalHistory(true, remainder)
		}
		return nil
	}
	// A window placed from the live bottom is not renumbered by a prepend, so
	// only the user's pending upward movement is replayed here.
	p.primaryTermPane().Freeze.Rebase(added)
	p.primaryTermPane().Scroll = min(p.primaryTermPane().Scroll+scrollLines, p.terminalMaxScroll(false))
	if more {
		return p.loadOlderTerminalHistory(false, remainder)
	}
	return nil
}

func (p *Plugin) terminalModelForHistorySource(source terminalHistorySource) *tty.Model {
	if p.terminalPaneIsPanel(source.LeafID) {
		if p.panelTerminalOwns() && p.requireShellTermPane().Terminal != nil && p.requireShellTermPane().Terminal.State != nil &&
			p.requireShellTermPane().Terminal.State.OutputBuf == source.Buffer {
			return p.requireShellTermPane().Terminal
		}
		return nil
	}
	if p.primaryTermPane().Terminal != nil && p.primaryTermPane().Terminal.IsActive() && p.primaryTermPane().Terminal.State != nil &&
		p.primaryTermPane().Terminal.State.OutputBuf == source.Buffer {
		return p.primaryTermPane().Terminal
	}
	return nil
}

func (p *Plugin) cancelTerminalHistoryIntent(termPanel bool) {
	source, ok := p.terminalHistoryFor(termPanel)
	if !ok {
		return
	}
	p.cancelTerminalHistoryIntentByKey(source.Key)
}

func (p *Plugin) cancelTerminalHistoryIntentByKey(key string) {
	// No reach has been recorded for any pane yet, so there is nothing in flight
	// to retire — and a cancellation is not a reason to start holding state.
	if key == "" || p.terminalHistory == nil {
		return
	}
	state := p.terminalHistory[key]
	state.Cancel()
	p.terminalHistory[key] = state
}

func (p *Plugin) cancelAllTerminalHistoryLoads() {
	for key, state := range p.terminalHistory {
		state.Cancel()
		p.terminalHistory[key] = state
	}
}

// terminalHistorySummary reports the buffer's absolute base, the total number
// of lines the scrollbar should represent, and whether an older-history load is
// in flight.
//
// The base always comes from the buffer itself. Selection anchors and search
// matches are recorded in the buffer's absolute coordinates, and rendering maps
// them back with this base, so reporting 0 for a buffer that actually starts at
// a nonzero absolute line would offset every highlight by the amount of loaded
// scrollback. Only the total and loading flag depend on finding matching
// history state for this buffer.
func (p *Plugin) terminalHistorySummary(termPanel bool, buffer *tty.OutputBuffer) (base, total int, loading bool) {
	base, total = tty.BufferBase(buffer)
	if !tty.BufferAbsolute(buffer) {
		// Relative buffer: local indices already are the coordinate space, and
		// tracked history is counted in absolute lines it cannot be measured in.
		return base, total, false
	}
	source, ok := p.terminalHistoryFor(termPanel)
	if !ok || source.Buffer != buffer {
		// No tracked history for this buffer — its own range is still the truth.
		return base, total, false
	}
	state := p.terminalHistory[source.Key]
	return base, max(total, state.HistorySize), state.Loading
}

func (m terminalHistoryLoadedMsg) String() string {
	if m.Err != nil {
		return fmt.Sprintf("terminal history %s: %v", m.Source.Key, m.Err)
	}
	return fmt.Sprintf("terminal history %s [%d,%d)", m.Source.Key, m.Capture.StartLine, m.Capture.EndLine)
}

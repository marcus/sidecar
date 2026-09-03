package textselect

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/mouse"
)

// Binder is the half of a selectable surface a host can settle without hosting
// the gesture: the chords it answers and whether finishing a drag copies
// without being asked. A host reads both from config once and hands them to
// everything it draws, including a whole plugin that owns a selectable box
// inside its own frame.
type Binder interface {
	SetSelection(keys Keys, copyOnSelect bool)
}

// Pane is the host-facing shape of a selectable pane: everything a surface
// hosting one has to be able to say to it, and nothing about what the pane
// draws.
//
// It exists because three surfaces host the same viewers — the app's content
// deck, the project workspace and the global Workspaces browser — and each of
// them routes a press, a drag, a lost release and a chord. Routing written
// against a concrete viewer is routing that has to be written again for the
// next one; routing written against this interface is one arm per host for
// every viewer that has a [Surface] behind it.
//
// A viewer implements it by holding one Surface and a [Source] over the rows it
// already lays out. What a gesture means is the engine's; where the pane was
// drawn and which of the host's events reach it are the host's.
type Pane interface {
	Binder

	// SetOrigin records where the host last drew this pane's content, in the
	// coordinate space its mouse events arrive in. A pane that has not been
	// drawn has no origin, so it hit-tests as empty rather than as the
	// top-left corner of the screen.
	SetOrigin(x, y int)

	// HasSelection reports whether anything in this pane is selected.
	HasSelection() bool

	// ClearSelection drops the selection and any gesture editing it.
	ClearSelection()

	// AbandonSelection ends a gesture whose release never arrived — the pointer
	// left the window, a modal opened, focus moved.
	AbandonSelection() Result

	// HandleSelectionMouse advances the gesture and reports what the host owes.
	HandleSelectionMouse(action mouse.MouseAction) Result

	// HandleSelectionKey answers the chords that act on the selection.
	HandleSelectionKey(msg tea.KeyMsg) Result

	// SelectionCopyCmd delivers the copy a result asked for, wrapped in
	// whatever notification type the host uses. A result that asked for
	// nothing produces no command, so a host can hand every result it gets
	// straight here.
	SelectionCopyCmd(result Result, wrap func(CopyNotice) tea.Msg) tea.Cmd
}

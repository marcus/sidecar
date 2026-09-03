package notes

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/ui"
)

type controlledMutationStore struct {
	noteStore
	createStarted  chan struct{}
	createRelease  chan struct{}
	createErr      error
	createOnce     sync.Once
	deleteStarted  chan struct{}
	deleteRelease  chan struct{}
	deleteErr      error
	deleteOnce     sync.Once
	archiveStarted chan struct{}
	archiveRelease chan struct{}
	archiveErr     error
	archiveOnce    sync.Once
	actionMu       sync.Mutex
	pinIDs         []string
	archiveIDs     []string
	restoreIDs     []string
	restoreErrs    []error
}

func newControlledMutationStore(store noteStore) *controlledMutationStore {
	return &controlledMutationStore{
		noteStore:     store,
		createStarted: make(chan struct{}),
		createRelease: make(chan struct{}),
		deleteStarted: make(chan struct{}),
		deleteRelease: make(chan struct{}),
	}
}

func (s *controlledMutationStore) Create(title, content string) (*Note, error) {
	s.createOnce.Do(func() { close(s.createStarted) })
	<-s.createRelease
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.noteStore.Create(title, content)
}

func (s *controlledMutationStore) Delete(id string) error {
	s.deleteOnce.Do(func() { close(s.deleteStarted) })
	<-s.deleteRelease
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.noteStore.Delete(id)
}

func (s *controlledMutationStore) TogglePin(id string) error {
	s.actionMu.Lock()
	s.pinIDs = append(s.pinIDs, id)
	s.actionMu.Unlock()
	return s.noteStore.TogglePin(id)
}

func (s *controlledMutationStore) ToggleArchive(id string) error {
	s.actionMu.Lock()
	s.archiveIDs = append(s.archiveIDs, id)
	s.actionMu.Unlock()
	if s.archiveRelease != nil {
		s.archiveOnce.Do(func() {
			if s.archiveStarted != nil {
				close(s.archiveStarted)
			}
		})
		<-s.archiveRelease
	}
	if s.archiveErr != nil {
		return s.archiveErr
	}
	return s.noteStore.ToggleArchive(id)
}

func (s *controlledMutationStore) Restore(id string) error {
	s.actionMu.Lock()
	s.restoreIDs = append(s.restoreIDs, id)
	var err error
	if len(s.restoreErrs) > 0 {
		err = s.restoreErrs[0]
		s.restoreErrs = s.restoreErrs[1:]
	}
	s.actionMu.Unlock()
	if err != nil {
		return err
	}
	return s.noteStore.Restore(id)
}

func (s *controlledMutationStore) durableActionIDs() (pins, archives, restores []string) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	return append([]string(nil), s.pinIDs...), append([]string(nil), s.archiveIDs...), append([]string(nil), s.restoreIDs...)
}

func (s *controlledMutationStore) failRestores(errs ...error) {
	s.actionMu.Lock()
	s.restoreErrs = append([]error(nil), errs...)
	s.actionMu.Unlock()
}

func TestPendingCreateGuardsPinAndArchiveUntilCanonicalIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "pin", key: tea.KeyPressMsg{Code: 'p', Text: "p"}},
		{name: "archive", key: tea.KeyPressMsg{Code: 'A', Text: "A"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, controlled := newMutationPlugin(t)
			_, create := p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
			tempID := p.editorNote.ID
			result := runCommandAsync(create)
			<-controlled.createStarted

			_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			_, blocked := p.Update(test.key)
			toast, ok := firstToast(blocked)
			if !ok || toast.IsError {
				t.Fatalf("pending %s guard toast = %+v, ok=%v", test.name, toast, ok)
			}
			pins, archives, _ := controlled.durableActionIDs()
			if len(pins) != 0 || len(archives) != 0 {
				t.Fatalf("pending %s reached store: pins=%v archives=%v", test.name, pins, archives)
			}
			for _, action := range p.undoStack {
				if action.NoteID == tempID {
					t.Fatalf("pending %s recorded temp undo: %+v", test.name, action)
				}
			}

			close(controlled.createRelease)
			created := (<-result).(NoteSavedMsg)
			_, _ = p.Update(created)
			_, durable := p.Update(test.key)
			if durable == nil {
				t.Fatalf("canonical %s scheduled no durable action", test.name)
			}
			_, _ = p.Update(durable())
			pins, archives, _ = controlled.durableActionIDs()
			ids := pins
			if test.name == "archive" {
				ids = archives
			}
			if len(ids) != 1 || ids[0] != created.Note.ID {
				t.Fatalf("canonical %s IDs = %v, want [%s]", test.name, ids, created.Note.ID)
			}
			for _, action := range p.undoStack {
				if action.NoteID == tempID {
					t.Fatalf("canonical %s retained temp undo: %+v", test.name, action)
				}
			}
		})
	}
}

func TestOptimisticCreateStaysResponsiveAndRekeysQueuedSave(t *testing.T) {
	for _, queueBeforeCreate := range []bool{false, true} {
		name := "success-before-autosave-tick"
		if queueBeforeCreate {
			name = "success-after-autosave-tick"
		}
		t.Run(name, func(t *testing.T) {
			p, controlled := newMutationPlugin(t)
			_, create := p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
			if create == nil || p.mutation == nil || p.editorNote == nil {
				t.Fatal("new note did not synchronously enter optimistic editing")
			}
			tempID := p.editorNote.ID
			if !p.pendingCreateID(tempID) || p.previewMode || p.activePane != PaneEditor {
				t.Fatalf("optimistic editor state = id %q preview=%v pane=%v", tempID, p.previewMode, p.activePane)
			}

			result := runCommandAsync(create)
			<-controlled.createStarted
			_, _ = p.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			if got := p.editorTextarea.Value(); got != "x" {
				t.Fatalf("UI key did not land while Create was blocked: %q", got)
			}
			p.selection = ui.SelectionState{
				Active: true,
				Start:  ui.SelectionPoint{Line: 0, Col: 0},
				End:    ui.SelectionPoint{Line: 0, Col: 1},
				Anchor: ui.SelectionPoint{Line: 0, Col: 0},
			}
			p.rememberCurrentPlace()
			place := p.notePlaces[tempID]
			p.pendingEditorSyncID = tempID
			p.inlineEditNoteID = tempID
			p.taskModalNote = &Note{ID: tempID}
			p.deleteModalNote = &Note{ID: tempID}
			p.infoModalNote = &Note{ID: tempID}
			p.writeSequence.Store(7)
			p.latestWriteByNote.Store(tempID, uint64(7))
			previewCursor, previewScroll := p.previewCursorLine, p.previewScrollOff
			if queueBeforeCreate {
				_, queued := p.Update(AutoSaveTickMsg{ID: p.autoSaveID})
				if queued != nil || !p.saveQueued || p.saveInFlight {
					t.Fatalf("temp autosave escaped to td: cmd=%v queued=%v inFlight=%v", queued != nil, p.saveQueued, p.saveInFlight)
				}
			}

			close(controlled.createRelease)
			created := (<-result).(NoteSavedMsg)
			if created.Err != nil || created.Note == nil {
				t.Fatalf("create result = %+v", created)
			}

			// A current list response can arrive after td committed but before the
			// create result is processed. It must show one local row, not neither
			// row or both local and canonical identities.
			_, _ = p.Update(NotesLoadedMsg{
				Notes:     []Note{*created.Note},
				Epoch:     p.ctx.Epoch,
				RequestID: p.loadRequestID,
				Filter:    FilterActive,
			})
			if len(p.notes) != 1 || p.notes[0].ID != tempID {
				t.Fatalf("pending create merge = %+v, want one temp row", p.notes)
			}

			_, followup := p.Update(created)
			if p.mutation != nil || p.editorNote == nil || p.editorNote.ID != created.Note.ID {
				t.Fatalf("canonical identity was not installed atomically: mutation=%+v editor=%+v", p.mutation, p.editorNote)
			}
			if p.editorTextarea.Value() != "x" || !p.editorDirty {
				t.Fatalf("canonical result lost typing: value=%q dirty=%v", p.editorTextarea.Value(), p.editorDirty)
			}
			if !p.selection.HasSelection() || p.selection.Start.Col != 0 || p.selection.End.Col != 1 {
				t.Fatalf("canonical result lost selection: %+v", p.selection)
			}
			if p.previewCursorLine != previewCursor || p.previewScrollOff != previewScroll {
				t.Fatalf("canonical result lost viewport: cursor=%d/%d scroll=%d/%d", p.previewCursorLine, previewCursor, p.previewScrollOff, previewScroll)
			}
			if got, ok := p.notePlaces[created.Note.ID]; !ok || got != place {
				t.Fatalf("canonical result did not rekey place: got=%+v ok=%v want=%+v", got, ok, place)
			}
			if _, ok := p.notePlaces[tempID]; ok {
				t.Fatal("temporary place survived canonical rekey")
			}
			if _, ok := p.editHistories[created.Note.ID]; !ok {
				t.Fatal("temporary edit history was not rekeyed")
			}
			if p.pendingEditorSyncID != created.Note.ID || p.inlineEditNoteID != created.Note.ID ||
				p.taskModalNote == nil || p.taskModalNote.ID != created.Note.ID ||
				p.deleteModalNote == nil || p.deleteModalNote.ID != created.Note.ID ||
				p.infoModalNote == nil || p.infoModalNote.ID != created.Note.ID {
				t.Fatal("canonical result left an identity-indexed owner on the temporary ID")
			}
			if sequence, ok := p.latestWriteByNote.Load(created.Note.ID); !ok || sequence.(uint64) != 8 {
				t.Fatalf("canonical result did not rekey write generation: %v %v", sequence, ok)
			}
			if _, ok := p.latestWriteByNote.Load(tempID); ok {
				t.Fatal("temporary write generation survived canonical rekey")
			}
			applyCommandResults(t, p, followup)
			persisted, err := controlled.Get(created.Note.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Content != "x" {
				t.Fatalf("queued canonical save content = %q, want x", persisted.Content)
			}
		})
	}
}

func TestOptimisticCreateFailureRestoresExactSnapshotAndFutureLoadsWin(t *testing.T) {
	p, controlled := newMutationPlugin(t)
	controlled.createErr = errors.New("create unavailable")
	beforeNotes := append([]Note(nil), p.notes...)
	p.cursor = 0
	p.scrollOff = 3
	p.activePane = PaneList
	p.previewMode = true
	p.previewCursorLine = 2
	p.previewScrollOff = 1
	p.selection = ui.SelectionState{Active: true, Start: ui.SelectionPoint{Line: 1, Col: 2}, End: ui.SelectionPoint{Line: 1, Col: 4}}
	beforeTextarea := p.editorTextarea
	beforeSelection := p.selection

	_, create := p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	result := runCommandAsync(create)
	<-controlled.createStarted
	_, _ = p.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	p.viewFilter = FilterArchived
	p.searchMode = true
	p.searchField.SetQuery("changed while pending")
	p.markdownView = false
	close(controlled.createRelease)
	failed := (<-result).(NoteSavedMsg)
	_, toast := p.Update(failed)
	if got, ok := firstToast(toast); !ok || !got.IsError {
		t.Fatalf("create rollback returned no error toast: %+v", got)
	}
	if p.mutation != nil || len(p.notes) != len(beforeNotes) || p.notes[0].ID != beforeNotes[0].ID {
		t.Fatalf("create rollback list = %+v, before %+v", p.notes, beforeNotes)
	}
	if p.cursor != 0 || p.scrollOff != 3 || p.activePane != PaneList || !p.previewMode {
		t.Fatalf("create rollback navigation = cursor %d scroll %d pane %v preview %v", p.cursor, p.scrollOff, p.activePane, p.previewMode)
	}
	if p.viewFilter != FilterActive || p.searchMode || p.searchQuery() != "" || !p.markdownView {
		t.Fatalf("create rollback mode/filter = filter %v search=%v/%q markdown=%v", p.viewFilter, p.searchMode, p.searchQuery(), p.markdownView)
	}
	if p.editorTextarea.Value() != beforeTextarea.Value() || p.selection != beforeSelection {
		t.Fatalf("create rollback editor mismatch: value=%q selection=%+v", p.editorTextarea.Value(), p.selection)
	}
	if status, isErr := p.FooterStatus(); !isErr || status == "" {
		t.Fatalf("create failure left no durable retry truth: %q %v", status, isErr)
	}

	// The rollback invalidates only older snapshots; it is not a permanent
	// overlay. A later explicit refresh can still replace the restored list.
	newNote, err := controlled.noteStore.Create("external", "external")
	if err != nil {
		t.Fatal(err)
	}
	load := p.loadNotes()
	_, _ = p.Update(load())
	if p.noteByID(newNote.ID) == nil {
		t.Fatal("post-rollback external load could not become current truth")
	}
}

func TestOptimisticDeleteImmediateSelectionTombstoneAndSuccess(t *testing.T) {
	t.Run("middle-selects-former-next", func(t *testing.T) {
		p, controlled, notes := newDeleteMutationPlugin(t, 3)
		p.cursor = 1
		loadEditorForTest(p, 1)
		p.deleteModalNote = &p.notes[1]
		p.showDeleteModal = true

		deleteCmd := p.confirmDeleteNote()
		if len(p.notes) != 2 || p.notes[0].ID != notes[0].ID || p.notes[1].ID != notes[2].ID {
			t.Fatalf("middle delete did not remove immediately: %+v", p.notes)
		}
		if p.cursor != 1 || p.editorNote == nil || p.editorNote.ID != notes[2].ID {
			t.Fatalf("middle delete selected cursor=%d editor=%+v, want former next", p.cursor, p.editorNote)
		}
		if p.editorTextarea.Value() != notes[2].Content {
			t.Fatalf("deleted editor buffer survived: %q", p.editorTextarea.Value())
		}

		result := runCommandAsync(deleteCmd)
		<-controlled.deleteStarted
		_, _ = p.Update(NotesLoadedMsg{
			Notes:     append([]Note(nil), notes...),
			Epoch:     p.ctx.Epoch,
			RequestID: p.loadRequestID,
			Filter:    FilterActive,
		})
		if p.noteByID(notes[1].ID) != nil {
			t.Fatal("pending tombstone was resurrected by a current list load")
		}
		close(controlled.deleteRelease)
		deleted := (<-result).(NoteDeletedMsg)
		_, followup := p.Update(deleted)
		if p.mutation != nil || len(p.undoStack) != 1 || p.undoStack[0].NoteID != notes[1].ID {
			t.Fatalf("delete success truth = mutation %+v undo %+v", p.mutation, p.undoStack)
		}
		applyCommandResults(t, p, followup)
	})

	t.Run("last-selects-previous", func(t *testing.T) {
		p, _, notes := newDeleteMutationPlugin(t, 2)
		p.cursor = 1
		loadEditorForTest(p, 1)
		p.deleteModalNote = &p.notes[1]
		p.showDeleteModal = true
		_ = p.confirmDeleteNote()
		if p.cursor != 0 || p.editorNote == nil || p.editorNote.ID != notes[0].ID {
			t.Fatalf("last delete selected cursor=%d editor=%+v, want previous", p.cursor, p.editorNote)
		}
	})
}

// Archiving the note that is open in the right pane must load the newly
// selected neighbor's content into that pane, exactly as delete does.
// Regression: the optimistic list filter compacts p.notes in place, which
// shifted the editorNote pointer onto the neighbor's slot; loadNoteIntoEditor
// then early-returned on the matching ID and the pane kept the archived body.
func TestOptimisticArchiveReloadsRightPaneContent(t *testing.T) {
	p, controlled, notes := newDeleteMutationPlugin(t, 3)
	controlled.archiveStarted = make(chan struct{})
	controlled.archiveRelease = make(chan struct{})
	p.cursor = 1
	loadEditorForTest(p, 1)
	p.activePane = PaneList

	archiveCmd := p.toggleArchive()
	if p.editorNote == nil || p.editorNote.ID != notes[2].ID {
		t.Fatalf("begin: editor=%v, want %s", p.editorNote, notes[2].ID)
	}
	if got := p.editorTextarea.Value(); got != notes[2].Content {
		t.Fatalf("begin: textarea=%q, want %q", got, notes[2].Content)
	}
	if len(p.previewLines) != 1 || p.previewLines[0] != notes[2].Content {
		t.Fatalf("begin: previewLines=%v, want [%q]", p.previewLines, notes[2].Content)
	}

	result := runCommandAsync(archiveCmd)
	<-controlled.archiveStarted
	close(controlled.archiveRelease)
	archived := (<-result).(NoteArchiveToggledMsg)
	_, followup := p.Update(archived)
	applyCommandResults(t, p, followup)

	if p.cursor != 1 || p.editorNote == nil || p.editorNote.ID != notes[2].ID {
		t.Fatalf("after cycle: cursor=%d editor=%+v, want 1/%s", p.cursor, p.editorNote, notes[2].ID)
	}
	if got := p.editorTextarea.Value(); got != notes[2].Content {
		t.Fatalf("after cycle: textarea=%q, want %q", got, notes[2].Content)
	}
	if len(p.previewLines) != 1 || p.previewLines[0] != notes[2].Content {
		t.Fatalf("after cycle: previewLines=%v, want [%q]", p.previewLines, notes[2].Content)
	}
}

func TestOptimisticArchiveImmediateSelectionAndRollback(t *testing.T) {
	t.Run("middle-selects-next", func(t *testing.T) {
		p, controlled, notes := newDeleteMutationPlugin(t, 3)
		controlled.archiveStarted = make(chan struct{})
		controlled.archiveRelease = make(chan struct{})
		p.cursor = 1
		loadEditorForTest(p, 1)
		p.activePane = PaneList

		archiveCmd := p.toggleArchive()
		if len(p.notes) != 2 || p.noteByID(notes[1].ID) != nil {
			t.Fatalf("archive did not remove immediately: %+v", p.notes)
		}
		if p.cursor != 1 || p.editorNote == nil || p.editorNote.ID != notes[2].ID {
			t.Fatalf("middle archive selected cursor=%d editor=%+v, want former next", p.cursor, p.editorNote)
		}
		if p.activePane != PaneList {
			t.Fatalf("archive moved focus to %v, want list", p.activePane)
		}

		result := runCommandAsync(archiveCmd)
		<-controlled.archiveStarted
		_, _ = p.Update(NotesLoadedMsg{
			Notes:     append([]Note(nil), notes...),
			Epoch:     p.ctx.Epoch,
			RequestID: p.loadRequestID,
			Filter:    FilterActive,
		})
		if p.noteByID(notes[1].ID) != nil {
			t.Fatal("pending archive tombstone was resurrected by a current list load")
		}
		close(controlled.archiveRelease)
		archived := (<-result).(NoteArchiveToggledMsg)
		_, followup := p.Update(archived)
		if p.mutation != nil || len(p.undoStack) != 1 || p.undoStack[0].NoteID != notes[1].ID {
			t.Fatalf("archive success truth = mutation %+v undo %+v", p.mutation, p.undoStack)
		}
		applyCommandResults(t, p, followup)
	})

	t.Run("last-selects-previous", func(t *testing.T) {
		p, _, notes := newDeleteMutationPlugin(t, 2)
		p.cursor = 1
		loadEditorForTest(p, 1)
		p.activePane = PaneEditor
		_ = p.toggleArchive()
		if p.cursor != 0 || p.editorNote == nil || p.editorNote.ID != notes[0].ID {
			t.Fatalf("last archive selected cursor=%d editor=%+v, want previous", p.cursor, p.editorNote)
		}
		if p.activePane != PaneEditor {
			t.Fatalf("archive moved focus to %v, want editor", p.activePane)
		}
	})

	t.Run("store-error-restores-and-toasts", func(t *testing.T) {
		p, controlled, notes := newDeleteMutationPlugin(t, 2)
		controlled.archiveStarted = make(chan struct{})
		controlled.archiveRelease = make(chan struct{})
		controlled.archiveErr = errors.New("archive unavailable")
		p.cursor = 0
		p.scrollOff = 2
		loadEditorForTest(p, 0)
		p.activePane = PaneList
		before := p.captureMutationSnapshot()

		archiveCmd := p.toggleArchive()
		result := runCommandAsync(archiveCmd)
		<-controlled.archiveStarted
		close(controlled.archiveRelease)
		failed := (<-result).(NoteArchiveToggledMsg)
		_, toast := p.Update(failed)
		if got, ok := firstToast(toast); !ok || !got.IsError {
			t.Fatalf("archive rollback returned no error toast: %+v", got)
		}
		if p.mutation != nil || len(p.notes) != 2 || p.notes[0].ID != notes[0].ID || p.cursor != 0 || p.scrollOff != 2 {
			t.Fatalf("archive rollback list/cursor = %+v cursor=%d scroll=%d", p.notes, p.cursor, p.scrollOff)
		}
		if p.activePane != before.activePane {
			t.Fatalf("archive rollback pane = %v, want %v", p.activePane, before.activePane)
		}
		if len(p.undoStack) != 0 {
			t.Fatalf("failed archive recorded undo: %+v", p.undoStack)
		}
	})
}

func TestOptimisticDeleteFailureRestoresExactModalEditorAndUndoState(t *testing.T) {
	p, controlled, notes := newDeleteMutationPlugin(t, 2)
	controlled.deleteErr = errors.New("delete unavailable")
	p.cursor = 0
	p.scrollOff = 4
	loadEditorForTest(p, 0)
	p.previewMode = false
	p.editorTextarea.SetValue("local editor")
	p.editorTextarea.MoveToEnd()
	p.previewCursorLine = 3
	p.previewScrollOff = 2
	p.notePlaces[notes[0].ID] = notePlace{editRow: 3, editCol: 5, editScrollOff: 2, hasEdit: true}
	p.editHistories[notes[0].ID] = &editHistory{undo: []editSnapshot{{content: "before", row: 1, col: 2}}, bytes: 6}
	p.selection = ui.SelectionState{Active: true, Start: ui.SelectionPoint{Line: 0, Col: 1}, End: ui.SelectionPoint{Line: 0, Col: 3}}
	p.undoStack = []UndoAction{{Type: UndoArchive, NoteID: notes[1].ID, Title: notes[1].Title}}
	p.deleteModalNote = &p.notes[0]
	p.showDeleteModal = true
	beforeTextarea := p.editorTextarea
	beforeSelection := p.selection
	beforePlace := p.notePlaces[notes[0].ID]

	deleteCmd := p.confirmDeleteNote()
	result := runCommandAsync(deleteCmd)
	<-controlled.deleteStarted
	if p.noteByID(notes[0].ID) != nil || p.showDeleteModal {
		t.Fatal("delete was not optimistic while store blocked")
	}
	close(controlled.deleteRelease)
	failed := (<-result).(NoteDeletedMsg)
	_, toast := p.Update(failed)
	if got, ok := firstToast(toast); !ok || !got.IsError {
		t.Fatalf("delete rollback returned no error toast: %+v", got)
	}
	if len(p.notes) != 2 || p.notes[0].ID != notes[0].ID || p.cursor != 0 || p.scrollOff != 4 {
		t.Fatalf("delete rollback list/cursor = %+v cursor=%d scroll=%d", p.notes, p.cursor, p.scrollOff)
	}
	if !p.showDeleteModal || p.deleteModalNote == nil || p.deleteModalNote.ID != notes[0].ID {
		t.Fatalf("delete rollback did not restore actionable confirmation: show=%v note=%+v", p.showDeleteModal, p.deleteModalNote)
	}
	if p.editorTextarea.Value() != beforeTextarea.Value() || p.editorTextarea.Line() != beforeTextarea.Line() || p.editorTextarea.Column() != beforeTextarea.Column() {
		t.Fatalf("delete rollback editor = %q (%d,%d), want %q (%d,%d)", p.editorTextarea.Value(), p.editorTextarea.Line(), p.editorTextarea.Column(), beforeTextarea.Value(), beforeTextarea.Line(), beforeTextarea.Column())
	}
	if p.selection != beforeSelection || p.notePlaces[notes[0].ID] != beforePlace {
		t.Fatalf("delete rollback selection/place = %+v %+v", p.selection, p.notePlaces[notes[0].ID])
	}
	if len(p.editHistories[notes[0].ID].undo) != 1 || len(p.undoStack) != 1 || p.undoStack[0].Type != UndoArchive {
		t.Fatalf("delete failure corrupted history/undo: history=%+v undo=%+v", p.editHistories[notes[0].ID], p.undoStack)
	}
}

func TestDeleteUndoRestoreFailureStaysVisibleAndRetryable(t *testing.T) {
	p, controlled, notes := newDeleteMutationPlugin(t, 2)
	close(controlled.deleteRelease)
	p.cursor = 0
	loadEditorForTest(p, 0)
	p.deleteModalNote = &p.notes[0]
	p.showDeleteModal = true
	deleted := p.confirmDeleteNote()().(NoteDeletedMsg)
	_, _ = p.Update(deleted)
	if len(p.undoStack) != 1 || p.undoStack[0].NoteID != notes[0].ID {
		t.Fatalf("successful delete undo = %+v", p.undoStack)
	}

	controlled.failRestores(errors.New("restore unavailable"), nil)
	_, undo := p.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	failed := undo().(NoteRestoredMsg)
	if failed.Action.Type != UndoDelete || failed.Action.NoteID != notes[0].ID {
		t.Fatalf("restore result lost popped action: %+v", failed.Action)
	}
	_, failureToast := p.Update(failed)
	if toast, ok := firstToast(failureToast); !ok || !toast.IsError {
		t.Fatalf("restore failure toast = %+v, ok=%v", toast, ok)
	}
	if len(p.undoStack) != 1 || p.undoStack[0] != failed.Action {
		t.Fatalf("restore failure did not retain exact undo: stack=%+v action=%+v", p.undoStack, failed.Action)
	}
	if status, isErr := p.FooterStatus(); !isErr || status != "notes: undo failed — u to retry" {
		t.Fatalf("restore failure footer = %q, error=%v", status, isErr)
	}

	_, retry := p.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	succeeded := retry().(NoteRestoredMsg)
	_, _ = p.Update(succeeded)
	if succeeded.Err != nil || len(p.undoStack) != 0 || p.undoErr != nil {
		t.Fatalf("restore retry result=%+v stack=%+v undoErr=%v", succeeded, p.undoStack, p.undoErr)
	}
	_, _, restores := controlled.durableActionIDs()
	if len(restores) != 2 || restores[0] != notes[0].ID || restores[1] != notes[0].ID {
		t.Fatalf("restore attempts = %v, want same note twice", restores)
	}
	restored, err := controlled.Get(notes[0].ID)
	if err != nil || restored == nil || restored.DeletedAt != nil {
		t.Fatalf("restored note = %+v, err=%v", restored, err)
	}
}

func newMutationPlugin(t *testing.T) (*Plugin, *controlledMutationStore) {
	t.Helper()
	base := openTestStore(t)
	existing, err := base.Create("existing", "existing body")
	if err != nil {
		t.Fatal(err)
	}
	controlled := newControlledMutationStore(base)
	p := New()
	p.ctx = &plugin.Context{Epoch: 1, ProjectRoot: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p.store = controlled
	p.notes = []Note{*existing}
	p.cursor = 0
	p.viewFilter = FilterActive
	p.width = 100
	p.height = 24
	p.listWidth = 30
	p.editorTextarea = newEditorTextarea()
	return p, controlled
}

func newDeleteMutationPlugin(t *testing.T, count int) (*Plugin, *controlledMutationStore, []Note) {
	t.Helper()
	base := openTestStore(t)
	notes := make([]Note, count)
	for i := range notes {
		note, err := base.Create(string(rune('A'+i)), "body "+string(rune('A'+i)))
		if err != nil {
			t.Fatal(err)
		}
		notes[i] = *note
	}
	controlled := newControlledMutationStore(base)
	p := New()
	p.ctx = &plugin.Context{Epoch: 1, ProjectRoot: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p.store = controlled
	p.notes = append([]Note(nil), notes...)
	p.viewFilter = FilterActive
	p.width = 100
	p.height = 24
	p.listWidth = 30
	p.editorTextarea = newEditorTextarea()
	return p, controlled, notes
}

func loadEditorForTest(p *Plugin, index int) {
	p.cursor = index
	p.editorNote = nil
	_ = p.loadNoteIntoEditor()
}

func runCommandAsync(cmd tea.Cmd) <-chan tea.Msg {
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	return result
}

func applyCommandResults(t *testing.T, p *Plugin, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch result := cmd().(type) {
	case tea.BatchMsg:
		for _, nested := range result {
			applyCommandResults(t, p, nested)
		}
	default:
		_, followup := p.Update(result)
		applyCommandResults(t, p, followup)
	}
}

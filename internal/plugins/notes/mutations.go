package notes

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
)

const optimisticNoteIDPrefix = "local-note-"

type noteMutationKind uint8

const (
	noteMutationCreate noteMutationKind = iota + 1
	noteMutationDelete
	noteMutationArchive
)

// noteMutation is the single owner of a structural change while td is
// persisting it. Keeping the optimistic identity/tombstone here lets every
// list snapshot pass through one reconciliation point.
type noteMutation struct {
	id        uint64
	kind      noteMutationKind
	tempID    string
	deleteID  string
	note      Note
	listIndex int
	startedAt time.Time
	before    noteMutationSnapshot
}

// noteMutationSnapshot is intentionally UI-shaped: a failed structural write
// restores the exact list/editor/caret state that was visible before it began.
// Async request identities are not restored; they only move forward so a load
// started against the optimistic state cannot overwrite the rollback.
type noteMutationSnapshot struct {
	notes         []Note
	filteredNotes []NoteMatch
	cursor        int
	scrollOff     int
	activePane    FocusPane
	viewFilter    NoteFilter
	pendingG      bool
	searchMode    bool
	searchQuery   string

	editorNoteID     string
	editorTextarea   textarea.Model
	editorValue      string
	editorRow        int
	editorCol        int
	editorScroll     int
	editorFocused    bool
	editorDirty      bool
	previewMode      bool
	markdownView     bool
	previewLines     []string
	previewCursor    int
	previewScrollOff int
	lastSavedContent string
	selection        ui.SelectionState
	pointer          tty.Pointer
	selAnchor        ui.SelectionPoint
	selExtend        bool

	notePlaces       map[string]notePlace
	editHistories    map[string]*editHistory
	undoStack        []UndoAction
	saveQueued       bool
	saveErr          error
	pendingAfterSave func() tea.Cmd

	showDeleteModal bool
	deleteModalID   string
}

func (p *Plugin) captureMutationSnapshot() noteMutationSnapshot {
	s := noteMutationSnapshot{
		notes:            append([]Note(nil), p.notes...),
		filteredNotes:    append([]NoteMatch(nil), p.filteredNotes...),
		cursor:           p.cursor,
		scrollOff:        p.scrollOff,
		activePane:       p.activePane,
		viewFilter:       p.viewFilter,
		pendingG:         p.pendingG,
		searchMode:       p.searchMode,
		searchQuery:      p.searchQuery(),
		editorTextarea:   p.editorTextarea,
		editorValue:      p.editorTextarea.Value(),
		editorRow:        p.editorTextarea.Line(),
		editorCol:        p.editorTextarea.Column(),
		editorScroll:     p.editorTextarea.ScrollYOffset(),
		editorFocused:    p.editorTextarea.Focused(),
		editorDirty:      p.editorDirty,
		previewMode:      p.previewMode,
		markdownView:     p.markdownView,
		previewLines:     append([]string(nil), p.previewLines...),
		previewCursor:    p.previewCursorLine,
		previewScrollOff: p.previewScrollOff,
		lastSavedContent: p.lastSavedContent,
		selection:        p.selection,
		pointer:          p.pointer,
		selAnchor:        p.selAnchor,
		selExtend:        p.selExtend,
		notePlaces:       cloneNotePlaces(p.notePlaces),
		editHistories:    cloneEditHistories(p.editHistories),
		undoStack:        append([]UndoAction(nil), p.undoStack...),
		saveQueued:       p.saveQueued,
		saveErr:          p.saveErr,
		pendingAfterSave: p.pendingAfterSave,
		showDeleteModal:  p.showDeleteModal,
	}
	if p.editorNote != nil {
		s.editorNoteID = p.editorNote.ID
	}
	if p.deleteModalNote != nil {
		s.deleteModalID = p.deleteModalNote.ID
	}
	return s
}

func (p *Plugin) restoreMutationSnapshot(s noteMutationSnapshot) {
	p.notes = append([]Note(nil), s.notes...)
	p.filteredNotes = append([]NoteMatch(nil), s.filteredNotes...)
	p.cursor = s.cursor
	p.scrollOff = s.scrollOff
	p.activePane = s.activePane
	p.viewFilter = s.viewFilter
	p.pendingG = s.pendingG
	p.searchMode = s.searchMode
	p.searchField.SetQuery(s.searchQuery)
	p.editorTextarea = s.editorTextarea
	p.editorTextarea.SetValue(s.editorValue)
	p.setTextareaCursorAndScroll(s.editorRow, s.editorCol, s.editorScroll)
	if s.editorFocused {
		p.editorTextarea.Focus()
	} else {
		p.editorTextarea.Blur()
	}
	p.editorDirty = s.editorDirty
	p.previewMode = s.previewMode
	p.markdownView = s.markdownView
	p.previewLines = append([]string(nil), s.previewLines...)
	p.previewCursorLine = s.previewCursor
	p.previewScrollOff = s.previewScrollOff
	p.lastSavedContent = s.lastSavedContent
	p.selection = s.selection
	p.pointer = s.pointer
	p.selAnchor = s.selAnchor
	p.selExtend = s.selExtend
	p.notePlaces = cloneNotePlaces(s.notePlaces)
	p.editHistories = cloneEditHistories(s.editHistories)
	p.undoStack = append([]UndoAction(nil), s.undoStack...)
	p.saveQueued = s.saveQueued
	p.saveErr = s.saveErr
	p.pendingAfterSave = s.pendingAfterSave
	p.editorNote = p.noteByID(s.editorNoteID)
	p.showDeleteModal = s.showDeleteModal
	p.deleteModalNote = p.noteByID(s.deleteModalID)
	p.clearDeleteModal()
	p.invalidateViewSurface()
}

func cloneNotePlaces(src map[string]notePlace) map[string]notePlace {
	dst := make(map[string]notePlace, len(src))
	for id, place := range src {
		dst[id] = place
	}
	return dst
}

func cloneEditHistories(src map[string]*editHistory) map[string]*editHistory {
	dst := make(map[string]*editHistory, len(src))
	for id, history := range src {
		if history == nil {
			dst[id] = nil
			continue
		}
		copyHistory := *history
		copyHistory.undo = append([]editSnapshot(nil), history.undo...)
		copyHistory.redo = append([]editSnapshot(nil), history.redo...)
		dst[id] = &copyHistory
	}
	return dst
}

func (p *Plugin) noteByID(id string) *Note {
	if id == "" {
		return nil
	}
	for i := range p.notes {
		if p.notes[i].ID == id {
			return &p.notes[i]
		}
	}
	return nil
}

func (p *Plugin) pendingCreateID(id string) bool {
	return p.mutation != nil && p.mutation.kind == noteMutationCreate && p.mutation.tempID == id
}

// guardPendingCreateDurableAction keeps the temporary identity inside the UI.
// Content saving is the one exception: saveEditorContent queues it until the
// Create result provides a canonical td identity.
func (p *Plugin) guardPendingCreateDurableAction(noteID string) (tea.Cmd, bool) {
	if !p.pendingCreateID(noteID) {
		return nil, false
	}
	return msg.ShowToast("Creating note — try again when it is saved", 2*time.Second), true
}

func (p *Plugin) beginOptimisticCreate(title, content string) tea.Cmd {
	if p.store == nil {
		return nil
	}
	if p.mutation != nil {
		return msg.ShowToast("A note change is still pending", 2*time.Second)
	}

	p.nextMutationID++
	mutationID := p.nextMutationID
	tempID := optimisticNoteID(mutationID)
	now := time.Now()
	mutation := &noteMutation{
		id:        mutationID,
		kind:      noteMutationCreate,
		tempID:    tempID,
		listIndex: 0,
		startedAt: now,
		before:    p.captureMutationSnapshot(),
		note: Note{
			ID:        tempID,
			Title:     title,
			Content:   content,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	p.mutation = mutation
	p.mutationErr = nil
	p.mutationAction = ""
	p.loadRequestID++ // pre-mutation snapshots cannot erase the local row

	p.notes = append(p.notes, Note{})
	copy(p.notes[1:], p.notes[:len(p.notes)-1])
	p.notes[0] = mutation.note
	if p.searchQuery() != "" {
		p.filteredNotes = FilterNotes(p.notes, p.searchQuery())
	}
	p.cursor = 0
	p.scrollOff = 0
	p.activePane = PaneEditor
	p.previewMode = false
	_ = p.loadNoteIntoEditorAtEnd()

	epoch := p.ctx.Epoch
	store := p.store
	return func() tea.Msg {
		note, err := store.Create(title, content)
		return NoteSavedMsg{
			Note:       note,
			Err:        err,
			Epoch:      epoch,
			MutationID: mutationID,
			TempID:     tempID,
		}
	}
}

func (p *Plugin) finishOptimisticCreate(result NoteSavedMsg) tea.Cmd {
	mutation := p.mutation
	if mutation == nil || mutation.kind != noteMutationCreate ||
		mutation.id != result.MutationID || mutation.tempID != result.TempID {
		return nil
	}
	if result.Err != nil || result.Note == nil || result.Note.ID == "" {
		err := result.Err
		if err == nil {
			err = fmt.Errorf("td returned no canonical note")
		}
		p.restoreMutationSnapshot(mutation.before)
		p.mutation = nil
		p.mutationErr = err
		p.mutationAction = "create"
		p.autoSaveID++
		p.loadRequestID++
		if p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Error("notes: create failed", "error", err)
		}
		return showMutationFailedToast(noteMutationCreate, err)
	}

	p.rekeyCreatedNote(mutation.tempID, *result.Note)
	p.mutation = nil
	p.mutationErr = nil
	p.mutationAction = ""
	p.loadRequestID++ // reject snapshots taken before canonical identity existed
	save := p.saveEditorContent()
	load := p.loadNotes()
	return tea.Batch(save, load)
}

func (p *Plugin) beginOptimisticDelete(note Note) tea.Cmd {
	if p.store == nil {
		p.closeDeleteModal()
		return nil
	}
	if blocked, ok := p.guardPendingCreateDurableAction(note.ID); ok {
		return blocked
	}
	if p.mutation != nil {
		return msg.ShowToast("A note change is still pending", 2*time.Second)
	}

	p.nextMutationID++
	mutationID := p.nextMutationID
	mutation := &noteMutation{
		id:        mutationID,
		kind:      noteMutationDelete,
		deleteID:  note.ID,
		note:      note,
		listIndex: p.cursor,
		startedAt: time.Now(),
		before:    p.captureMutationSnapshot(),
	}
	p.mutation = mutation
	p.mutationErr = nil
	p.mutationAction = ""
	p.loadRequestID++ // pre-delete snapshots cannot resurrect the tombstone
	p.closeDeleteModal()
	p.clearDeletedEditorState(note.ID)

	kept := p.notes[:0]
	for _, candidate := range p.notes {
		if candidate.ID != note.ID {
			kept = append(kept, candidate)
		}
	}
	p.notes = kept
	p.selectNeighborAfterRemoval(mutation.listIndex)

	epoch := p.ctx.Epoch
	store := p.store
	return func() tea.Msg {
		err := store.Delete(note.ID)
		return NoteDeletedMsg{ID: note.ID, Err: err, Epoch: epoch, MutationID: mutationID}
	}
}

func (p *Plugin) finishOptimisticDelete(result NoteDeletedMsg) tea.Cmd {
	mutation := p.mutation
	if mutation == nil || mutation.kind != noteMutationDelete ||
		mutation.id != result.MutationID || mutation.deleteID != result.ID {
		return nil
	}
	if result.Err != nil {
		p.restoreMutationSnapshot(mutation.before)
		p.mutation = nil
		p.mutationErr = result.Err
		p.mutationAction = "delete"
		p.loadRequestID++
		if p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Error("notes: delete failed", "error", result.Err)
		}
		return showMutationFailedToast(noteMutationDelete, result.Err)
	}

	p.mutation = nil
	p.mutationErr = nil
	p.mutationAction = ""
	p.pushUndo(UndoAction{Type: UndoDelete, NoteID: mutation.note.ID, Title: mutation.note.Title})
	p.loadRequestID++
	return p.loadNotes()
}

func (p *Plugin) beginOptimisticArchive(note Note) tea.Cmd {
	if p.store == nil {
		return nil
	}
	if blocked, ok := p.guardPendingCreateDurableAction(note.ID); ok {
		return blocked
	}
	if p.mutation != nil {
		return msg.ShowToast("A note change is still pending", 2*time.Second)
	}

	p.nextMutationID++
	mutationID := p.nextMutationID
	mutation := &noteMutation{
		id:        mutationID,
		kind:      noteMutationArchive,
		deleteID:  note.ID,
		note:      note,
		listIndex: p.cursor,
		startedAt: time.Now(),
		before:    p.captureMutationSnapshot(),
	}
	p.mutation = mutation
	p.mutationErr = nil
	p.mutationAction = ""
	p.loadRequestID++

	// Same code path as delete: drop the editor binding for the note leaving
	// the list BEFORE the in-place filter below. editorNote points into the
	// p.notes backing array, so compaction would otherwise shift it onto the
	// neighbor's slot and loadNoteIntoEditor would early-return on the matching
	// ID, leaving the archived note's body stale in the right pane. Safe to
	// clear unconditionally: toggleArchive saves a dirty buffer first.
	p.clearDeletedEditorState(note.ID)

	kept := p.notes[:0]
	for _, candidate := range p.notes {
		if candidate.ID != note.ID {
			kept = append(kept, candidate)
		}
	}
	p.notes = kept
	p.selectNeighborAfterRemoval(mutation.listIndex)

	epoch := p.ctx.Epoch
	store := p.store
	return func() tea.Msg {
		err := store.ToggleArchive(note.ID)
		return NoteArchiveToggledMsg{ID: note.ID, Err: err, Epoch: epoch, MutationID: mutationID}
	}
}

func (p *Plugin) finishOptimisticArchive(result NoteArchiveToggledMsg) tea.Cmd {
	mutation := p.mutation
	if mutation == nil || mutation.kind != noteMutationArchive ||
		mutation.id != result.MutationID || mutation.deleteID != result.ID {
		return nil
	}
	if result.Err != nil {
		p.restoreMutationSnapshot(mutation.before)
		p.mutation = nil
		p.mutationErr = result.Err
		p.mutationAction = "archive"
		p.loadRequestID++
		if p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Error("notes: archive failed", "error", result.Err)
		}
		return showMutationFailedToast(noteMutationArchive, result.Err)
	}

	p.mutation = nil
	p.mutationErr = nil
	p.mutationAction = ""
	if !mutation.note.Archived {
		p.pushUndo(UndoAction{Type: UndoArchive, NoteID: mutation.note.ID, Title: mutation.note.Title})
	}
	p.loadRequestID++
	return p.loadNotes()
}

func (p *Plugin) selectNeighborAfterRemoval(index int) {
	if p.searchQuery() != "" {
		p.filteredNotes = FilterNotes(p.notes, p.searchQuery())
	}
	display := p.getDisplayNotes()
	if len(display) == 0 {
		p.cursor = 0
		p.scrollOff = 0
		_ = p.abandonEditor()
		return
	}
	p.cursor = index
	if p.cursor >= len(display) {
		p.cursor = len(display) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.ensureCursorVisibleForList(p.height-2, len(display))
	_ = p.loadNoteIntoEditor()
}

func (p *Plugin) clearDeletedEditorState(id string) {
	delete(p.notePlaces, id)
	delete(p.editHistories, id)
	if p.editorNote == nil || p.editorNote.ID != id {
		return
	}
	p.editorNote = nil
	p.editorTextarea.SetValue("")
	p.editorDirty = false
	p.lastSavedContent = ""
	p.previewLines = nil
	p.previewCursorLine = 0
	p.previewScrollOff = 0
	p.pendingAfterSave = nil
	p.saveQueued = false
	p.selection.Clear()
	p.pointer = tty.Pointer{}
	p.selAnchor = ui.SelectionPoint{Line: -1, Col: -1}
	p.selExtend = false
	p.invalidateViewSurface()
}

func (p *Plugin) reconcileMutation(notes []Note) []Note {
	merged := append([]Note(nil), notes...)
	mutation := p.mutation
	if mutation == nil {
		return merged
	}
	if mutation.kind == noteMutationDelete || mutation.kind == noteMutationArchive {
		filtered := merged[:0]
		for _, note := range merged {
			if note.ID != mutation.deleteID {
				filtered = append(filtered, note)
			}
		}
		return filtered
	}
	if p.viewFilter != FilterActive {
		return merged
	}

	// td may have committed Create while its result is still queued behind a
	// list response. Suppress that exact fresh canonical row until the create
	// result can atomically replace the temporary identity.
	filtered := merged[:0]
	for _, note := range merged {
		if note.ID != mutation.tempID && samePendingCreate(note, mutation) {
			continue
		}
		filtered = append(filtered, note)
	}
	merged = filtered
	index := mutation.listIndex
	if index < 0 {
		index = 0
	}
	if index > len(merged) {
		index = len(merged)
	}
	merged = append(merged, Note{})
	copy(merged[index+1:], merged[index:])
	merged[index] = mutation.note
	return merged
}

func samePendingCreate(note Note, mutation *noteMutation) bool {
	if note.Title != mutation.note.Title || note.Content != mutation.note.Content {
		return false
	}
	for _, prior := range mutation.before.notes {
		if prior.ID == note.ID {
			return false
		}
	}
	// In-process and CLI stores normally report a creation timestamp. Test
	// doubles may omit it; a new identity plus exact initial payload remains
	// the best available correlation until the Create result is delivered.
	return note.CreatedAt.IsZero() || !note.CreatedAt.Before(mutation.startedAt)
}

func (p *Plugin) rekeyCreatedNote(tempID string, canonical Note) {
	selectedID := ""
	if selected := p.getSelectedNote(); selected != nil {
		selectedID = selected.ID
	}
	for i := range p.notes {
		if p.notes[i].ID == tempID {
			p.notes[i] = canonical
			break
		}
	}
	moveNoteMapKey(p.notePlaces, tempID, canonical.ID)
	moveHistoryKey(p.editHistories, tempID, canonical.ID)
	p.moveSyncMapKey(&p.latestWriteByNote, tempID, canonical.ID)
	p.moveSyncMapKey(&p.durableWriteByNote, tempID, canonical.ID)

	if p.editorNote != nil && p.editorNote.ID == tempID {
		p.editorNote = p.noteByID(canonical.ID)
	}
	if p.taskModalNote != nil && p.taskModalNote.ID == tempID {
		p.taskModalNote = p.noteByID(canonical.ID)
	}
	if p.deleteModalNote != nil && p.deleteModalNote.ID == tempID {
		p.deleteModalNote = p.noteByID(canonical.ID)
	}
	if p.infoModalNote != nil && p.infoModalNote.ID == tempID {
		p.infoModalNote = p.noteByID(canonical.ID)
	}
	if p.inlineEditNoteID == tempID {
		p.inlineEditNoteID = canonical.ID
	}
	if p.pendingInlineEditID == tempID {
		p.pendingInlineEditID = canonical.ID
	}
	if p.pendingEditorSyncID == tempID {
		p.pendingEditorSyncID = canonical.ID
	}
	if p.activeSaveID == tempID {
		p.activeSaveID = canonical.ID
	}
	if p.activeExport.ID == tempID {
		p.activeExport.ID = canonical.ID
	}
	for i := range p.exportQueue {
		if p.exportQueue[i].ID == tempID {
			p.exportQueue[i].ID = canonical.ID
		}
	}
	for i := range p.supersededExports {
		if p.supersededExports[i].ID == tempID {
			p.supersededExports[i].ID = canonical.ID
		}
	}

	if p.searchQuery() != "" {
		p.filteredNotes = FilterNotes(p.notes, p.searchQuery())
	}
	if selectedID == tempID {
		p.moveCursorToNote(canonical.ID)
	}
	// The canonical create contains the initial buffer, not any typing that
	// landed while it was slow. Keep textarea, selection and viewport intact.
	if p.editorNote != nil && p.editorNote.ID == canonical.ID {
		p.lastSavedContent = canonical.Content
	}
}

func moveNoteMapKey(values map[string]notePlace, oldID, newID string) {
	if value, ok := values[oldID]; ok {
		values[newID] = value
		delete(values, oldID)
	}
}

func moveHistoryKey(values map[string]*editHistory, oldID, newID string) {
	if value, ok := values[oldID]; ok {
		values[newID] = value
		delete(values, oldID)
	}
}

func (p *Plugin) moveSyncMapKey(values interface {
	LoadAndDelete(any) (any, bool)
	Store(any, any)
}, oldID, newID string) {
	if value, ok := values.LoadAndDelete(oldID); ok {
		values.Store(newID, value)
	}
}

func showMutationFailedToast(kind noteMutationKind, err error) tea.Cmd {
	action := "Create"
	switch kind {
	case noteMutationDelete:
		action = "Delete"
	case noteMutationArchive:
		action = "Archive"
	}
	text := action + " failed"
	if err != nil {
		text += ": " + err.Error()
	}
	return func() tea.Msg {
		return msg.ToastMsg{Message: text, Duration: 4 * time.Second, IsError: true}
	}
}

func optimisticNoteID(id uint64) string {
	return fmt.Sprintf("%s%d", optimisticNoteIDPrefix, id)
}

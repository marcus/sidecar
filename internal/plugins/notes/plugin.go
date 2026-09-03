package notes

import (
	"errors"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/tdsetup"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
)

type retainedExport struct {
	ID          string
	Path        string
	RequestID   uint64
	Epoch       uint64
	ProjectRoot string
	Store       noteStore
	StartedAt   int64
	Sequence    uint64
}

const (
	pluginID   = "notes"
	pluginName = "Notes"
	pluginIcon = "N"

	// Pane layout
	dividerWidth = 1
)

// FocusPane represents which pane is active.
type FocusPane int

const (
	PaneList FocusPane = iota
	PaneEditor
)

// NoteFilter represents the current note filter view.
type NoteFilter int

const (
	FilterActive NoteFilter = iota
	FilterArchived
	FilterDeleted
)

// String returns the display name for the filter.
func (f NoteFilter) String() string {
	switch f {
	case FilterArchived:
		return "Archived"
	case FilterDeleted:
		return "Deleted"
	default:
		return "Active"
	}
}

// Plugin implements the notes plugin.
type Plugin struct {
	ctx     *plugin.Context
	focused bool
	store   noteStore

	// View dimensions
	width  int
	height int

	// Pane state
	activePane       FocusPane
	listWidth        int  // width of list pane (calculated from ratio)
	paneFocusManaged bool // Set once an outer deck composes Notes pane focus.
	paneFocusActive  bool // Whether Notes inner active chrome should be drawn.

	// Filter state
	viewFilter NoteFilter // Active, Archived, or Deleted view

	// Note state
	notes       []Note
	cursor      int
	scrollOff   int
	loading     bool
	loadErr     error
	recoveryErr error

	// td setup is offered only after the first asynchronous store probe says
	// this project has no .todos environment. Nothing here is touched by Init
	// or View until that command has settled.
	setupNeeded       bool
	showSetupModal    bool
	setupModal        *modal.Modal
	setupModalWidth   int
	setupMouseHandler *mouse.Handler
	setupErr          error
	setupInitializing bool
	setupDismissed    bool

	// g key state for g g sequence
	pendingG bool

	// Search state (NV-style). The query is the app's shared query field, so
	// the bar edits like every other `/` bar: the caret moves, alt+backspace
	// deletes a word, home and end work, and a paste arrives whole.
	// searchQuery() reads it.
	searchMode    bool             // true when search input is focused
	searchField   queryfield.Field // current search query
	filteredNotes []NoteMatch      // filtered results

	// Editor state
	editorNote     *Note          // The note being edited (nil = no note open)
	editorTextarea textarea.Model // Bubbles textarea for edit mode
	editorDirty    bool           // Unsaved changes
	previewMode    bool           // true = read-only preview, false = editing

	// Preview mode state (read-only navigation)
	previewLines       []string // Source lines; the view surface is derived from these
	previewCursorLine  int      // Visual row in view mode; source line in edit mode
	previewScrollOff   int      // Visual row offset in view mode; source offset in edit
	previewWrapEnabled bool     // retained for state compat; view/edit always wrap

	// markdownView is the resting Notes body: glamour-rendered markdown.
	// false is the raw source view (`m`). Both wrap at editorLayout.wrapColumn.
	markdownView     bool
	md               *markdown.Renderer
	viewSurface      markdown.MappedRender
	viewSurfaceSrc   string
	viewSurfaceWidth int
	viewSurfaceMD    bool
	// viewSurfaceStyle is the markdown renderer's theme identity the surface
	// was built under; a live theme change rebuilds it without a resize.
	viewSurfaceStyle string

	// Per-note view/edit place for this session (not persisted).
	notePlaces map[string]notePlace

	// Mouse state
	mouseHandler *mouse.Handler
	hoverDivider bool
	hoverNewNote bool

	// Interactive scrollbar pointer state (td-550ce1)
	scrollPointer notesScrollState

	// In-note search (preview pane). List search is searchMode/searchQuery.
	noteSearchMode      bool
	noteSearchCommitted bool
	noteSearchField     queryfield.Field
	noteSearchMatches   []noteSearchMatch
	noteSearchCursor    int

	// selection stores exclusive [Start, End) source carets in edit mode
	// (logical line + rune offset). View-mode (archived/deleted) selection
	// still uses visual rows for copy-only drags.
	selection ui.SelectionState
	pointer   tty.Pointer       // shared character/word/line mouse gesture state
	selAnchor ui.SelectionPoint // keyboard/mouse extend origin
	selExtend bool              // alt+s: ordinary movement extends

	// Per-note content undo/redo for the built-in editor. The list's
	// delete/archive stack is separate (undoStack).
	editHistories    map[string]*editHistory
	lastSavedContent string

	// Task modal state
	showTaskModal         bool
	taskModal             *modal.Modal
	taskModalWidth        int
	taskModalNote         *Note
	taskModalTitleInput   textinput.Model
	taskModalTypeIdx      int
	taskModalPriorityIdx  int
	taskModalArchiveNote  bool
	taskModalMouseHandler *mouse.Handler

	// Delete modal state
	showDeleteModal         bool
	deleteModal             *modal.Modal
	deleteModalWidth        int
	deleteModalNote         *Note
	deleteModalMouseHandler *mouse.Handler

	// Info modal state
	showInfoModal         bool
	infoModal             *modal.Modal
	infoModalWidth        int
	infoModalNote         *Note
	infoModalMouseHandler *mouse.Handler

	// Structural create/delete state. All list snapshots reconcile through
	// this one optimistic owner while td persists the mutation.
	mutation       *noteMutation
	nextMutationID uint64
	mutationErr    error
	mutationAction string

	// External editor state (for reading back content after $EDITOR exits)
	pendingInlineEditID    string // Note ID being edited
	pendingInlineEditPath  string // Temp file path
	externalPrepareID      uint64 // Latest $EDITOR export preparation request.
	exportSaveRequestID    uint64 // Latest retained export persistence attempt.
	exportSaveInFlight     bool
	exportCheckpointFailed bool // Stop could not hand a retained export to a draft.
	activeExport           retainedExport
	exportQueue            []retainedExport
	supersededExports      []retainedExport

	// One-shot sync after out-of-band editor saves
	pendingEditorSyncID string

	// Inline tty editor state. Session lifecycle, mouse and cursor mapping,
	// and the exit confirmation live in internal/inlineedit; only
	// notes-specific state stays here.
	edit                inlineedit.Session
	inlineEditNoteID    string
	orphanEditSession   string    // Defensive re-init cleanup, executed asynchronously in Start
	lastDragForwardTime time.Time // Throttle: last time a drag event was forwarded to tmux
	inlineWheel         tty.WheelBurst

	// Inline auto-save state (for periodic saving during inline edit)
	inlineAutoSaveGen      int    // Generation for staleness check
	inlineLastSavedContent string // Last saved content for change detection

	// Auto-save state
	autoSaveID         int        // Incremented on each edit to identify debounce timer
	saveMu             sync.Mutex // Serializes background writes with lifecycle's last-chance flush.
	writeSequence      atomic.Uint64
	latestWriteByNote  sync.Map // note id -> uint64; read by async persistence commands.
	durableWriteByNote sync.Map // note id -> uint64; canonical acknowledgments only.
	saveActivation     uint64   // Invalidates completions across Stop/Init.
	saveRequestID      uint64   // Identifies the single built-in save in flight.
	saveInFlight       bool
	activeSaveID       string
	activeSaveContent  string
	lastWriteRequest   uint64 // Guarded by saveMu; lets Stop reuse a completed in-flight write.
	lastWriteErr       error  // Guarded by saveMu.
	lastWriteSkipped   bool   // Guarded by saveMu.
	saveQueued         bool
	saveErr            error
	pendingAfterSave   func() tea.Cmd // Latest buffer-replacing action waiting on durable content.
	loadRequestID      uint64         // Latest list snapshot allowed to replace the local cache.
	navigateRequestID  uint64         // Latest internal note navigation existence check.

	// Undo state
	undoStack []UndoAction // Stack of undoable actions (most recent last)
	undoErr   error
}

// notePlace is the session-only view/edit position for one note.
// previewScrollOff / previewCursorLine are stored as source lines so they
// survive rendered/raw toggles and width changes.
type notePlace struct {
	previewScrollOff  int
	previewCursorLine int
	sourceCol         int
	editRow           int
	editCol           int
	editScrollOff     int
	hasEdit           bool
}

// newEditorTextarea builds the built-in editor with the same chrome as preview:
// no line-number gutter and no '~' end-of-buffer filler.
func newEditorTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.Prompt = ""
	ta.EndOfBufferCharacter = ' '
	focusedStyle := textarea.StyleState{
		Base:             lipgloss.NewStyle(),
		CursorLine:       lipgloss.NewStyle(),
		CursorLineNumber: styles.Muted,
		EndOfBuffer:      styles.Muted,
		LineNumber:       styles.Muted,
		Placeholder:      styles.Muted,
		Prompt:           lipgloss.NewStyle(),
		Text:             styles.Body,
	}
	taStyles := ta.Styles()
	taStyles.Focused = focusedStyle
	taStyles.Blurred = focusedStyle
	ta.SetStyles(taStyles)
	ta.KeyMap.CapitalizeWordForward = key.NewBinding(key.WithDisabled())
	ta.Blur()
	return ta
}

func (p *Plugin) applyEditorTextTheme() {
	taStyles := p.editorTextarea.Styles()
	taStyles.Focused.Text = styles.Body
	taStyles.Blurred.Text = styles.Body
	p.editorTextarea.SetStyles(taStyles)
}

// UndoActionType represents the type of undoable action.
type UndoActionType string

const (
	UndoDelete  UndoActionType = "delete"
	UndoArchive UndoActionType = "archive"
)

// UndoAction represents an undoable action.
type UndoAction struct {
	Type   UndoActionType
	NoteID string
	Title  string // For toast message
}

// New creates a new Notes plugin.
func New() *Plugin {
	md, _ := markdown.NewRenderer(markdown.CompactDocument)
	p := &Plugin{
		mouseHandler:   mouse.NewHandler(),
		previewMode:    true,
		markdownView:   true,
		md:             md,
		notePlaces:     make(map[string]notePlace),
		editHistories:  make(map[string]*editHistory),
		saveActivation: 1,
	}
	p.edit.Model = tty.New(nil)
	p.edit.Host = p
	p.clearInlineEditorAttachKey()
	return p
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

func (p *Plugin) remoteBound() bool {
	return p.ctx != nil && p.ctx.HostID != ""
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	// Project switches normally call Stop first, but Init is defensive: kill
	// any surviving editor asynchronously from Start and invalidate its autosave
	// chain before replacing the store/context.
	if p.editorDirty || p.exportCheckpointFailed {
		return errors.New("unsaved note or editor export remains after the previous project stopped")
	}
	p.edit.Host = p
	p.saveActivation++
	p.saveInFlight = false
	p.saveQueued = false
	p.saveErr = nil
	p.pendingAfterSave = nil
	p.loadRequestID++
	p.navigateRequestID++
	p.externalPrepareID++
	p.exportSaveRequestID++
	p.exportSaveInFlight = false
	p.exportCheckpointFailed = false
	p.activeExport = retainedExport{}
	p.exportQueue = nil
	p.supersededExports = nil
	p.autoSaveID++
	leftoverExport := p.edit.Path
	leftoverPending := p.pendingInlineEditPath
	if p.edit.Name != "" {
		p.orphanEditSession = p.edit.Name
	}
	p.resetInlineEditState()
	removeNoteExport(leftoverExport)
	removeNoteExport(leftoverPending)
	p.ctx = ctx
	p.notes = nil
	p.cursor = 0
	p.scrollOff = 0
	p.loading = false
	p.loadErr = nil
	p.recoveryErr = nil
	p.setupNeeded = false
	p.showSetupModal = false
	p.setupModal = nil
	p.setupModalWidth = 0
	p.setupErr = nil
	p.setupInitializing = false
	p.setupDismissed = false
	if p.setupMouseHandler == nil {
		p.setupMouseHandler = mouse.NewHandler()
	}
	p.pendingG = false
	p.searchMode = false
	p.searchField.Reset()
	p.filteredNotes = nil
	p.clearNoteSearch()
	p.mutation = nil
	p.nextMutationID = 0
	p.mutationErr = nil
	p.mutationAction = ""

	// Pane state
	p.activePane = PaneList
	p.viewFilter = FilterActive
	if p.remoteBound() {
		p.listWidth = 0
	} else {
		notesState := state.GetNotesState(ctx.ProjectRoot)
		if notesState.ListWidth > 0 {
			p.listWidth = notesState.ListWidth
		} else {
			p.listWidth = 0
		}
	}

	// Mouse state
	if p.mouseHandler == nil {
		p.mouseHandler = mouse.NewHandler()
	}
	p.selection.Clear()
	p.selAnchor = ui.SelectionPoint{Line: -1, Col: -1}
	p.selExtend = false
	p.editHistories = make(map[string]*editHistory)
	p.lastSavedContent = ""
	p.undoErr = nil

	// Editor state
	p.editorNote = nil
	p.editorDirty = false
	p.previewMode = true
	p.previewLines = nil
	p.previewCursorLine = 0
	p.previewScrollOff = 0
	p.previewWrapEnabled = true
	p.markdownView = true
	p.invalidateViewSurface()
	p.notePlaces = make(map[string]notePlace)
	p.pendingInlineEditID = ""
	p.pendingInlineEditPath = ""
	p.pendingEditorSyncID = ""
	p.clearInlineEditorAttachKey()

	p.editorTextarea = newEditorTextarea()

	if p.remoteBound() {
		p.store = nil
		return nil
	}

	// Construct the lazy td CLI adapter. The first load runs asynchronously.
	store, err := NewStore(ctx.ProjectRoot, "")
	if err != nil {
		// Store initialization may fail if .todos directory doesn't exist
		// This is OK - plugin will show appropriate message
		p.ctx.Logger.Debug("notes: store init failed", "error", err)
		p.store = nil
		return nil
	}

	p.store = store
	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	var cmds []tea.Cmd
	if p.orphanEditSession != "" {
		session := tty.EditorSession{Name: p.orphanEditSession}
		p.orphanEditSession = ""
		cmds = append(cmds, session.KillCmd())
	}
	if p.remoteBound() {
		if len(cmds) == 0 {
			return nil
		}
		return tea.Batch(cmds...)
	}
	if p.store != nil {
		cmds = append(cmds, p.loadNotes())
	}
	return tea.Batch(cmds...)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Invalidate and clear editor/autosave state before closing the store. A
	// queued tick from this project must not observe a later project's store.
	if p.orphanEditSession != "" {
		tty.EditorSession{Name: p.orphanEditSession}.Kill()
		p.orphanEditSession = ""
	}
	leftoverExport := p.edit.Path
	leftoverExportID := p.inlineEditNoteID
	leftoverExportBase := p.inlineLastSavedContent
	leftoverPending := p.pendingInlineEditPath
	leftoverPendingID := p.pendingInlineEditID
	p.exitInlineEditMode()
	seenExports := make(map[string]bool)
	var failedExports []retainedExport
	ownedSequences := make(map[string]uint64)
	for _, export := range append(append([]retainedExport{p.activeExport}, p.exportQueue...), p.supersededExports...) {
		if export.Path != "" && ownedSequences[export.Path] < export.Sequence {
			ownedSequences[export.Path] = export.Sequence
		}
	}
	activeExportContent := ""
	if p.activeExport.Path != "" {
		if content, err := os.ReadFile(p.activeExport.Path); err == nil {
			activeExportContent = string(content)
		}
	}
	checkpoint := func(noteID, path, inFlightContent string) bool {
		if path == "" || seenExports[path] {
			return true
		}
		seenExports[path] = true
		if p.writeIsDurablySuperseded(noteID, ownedSequences[path]) {
			removeNoteExport(path)
			return true
		}
		ok := p.checkpointStoppedExport(noteID, path, inFlightContent)
		if !ok {
			failedExports = append(failedExports, retainedExport{ID: noteID, Path: path})
		}
		return ok
	}
	allExportsCheckpointed := checkpoint(leftoverExportID, leftoverExport, leftoverExportBase)
	allExportsCheckpointed = checkpoint(leftoverPendingID, leftoverPending, "") && allExportsCheckpointed
	for _, export := range p.supersededExports {
		allExportsCheckpointed = checkpoint(export.ID, export.Path, "") && allExportsCheckpointed
	}
	allExportsCheckpointed = checkpoint(p.activeExport.ID, p.activeExport.Path, "") && allExportsCheckpointed
	for _, export := range p.exportQueue {
		inFlightContent := ""
		if export.ID == p.activeExport.ID {
			inFlightContent = activeExportContent
		}
		allExportsCheckpointed = checkpoint(export.ID, export.Path, inFlightContent) && allExportsCheckpointed
	}
	if allExportsCheckpointed {
		p.pendingInlineEditID = ""
		p.pendingInlineEditPath = ""
		p.activeExport = retainedExport{}
		p.exportQueue = nil
		p.supersededExports = nil
	} else {
		p.activeExport = retainedExport{}
		p.exportQueue = failedExports
		p.supersededExports = nil
		if len(failedExports) > 0 {
			p.pendingInlineEditID = failedExports[0].ID
			p.pendingInlineEditPath = failedExports[0].Path
		}
	}
	p.exportCheckpointFailed = !allExportsCheckpointed
	// A pending create has no td identity yet. Its content save is deliberately
	// queued for the Create result; lifecycle teardown must not turn that queue
	// into SaveContent(local-note-N).
	checkpointPendingCreate := p.editorNote != nil && p.pendingCreateID(p.editorNote.ID)
	if p.needsEditorCheckpoint() && !checkpointPendingCreate {
		root := ""
		var logger *slog.Logger
		if p.ctx != nil {
			root = p.ctx.ProjectRoot
			logger = p.ctx.Logger
		}
		if root == "" {
			if concrete, ok := p.store.(*Store); ok {
				root = concrete.baseDir
			}
		}
		baseContent := p.lastSavedContent
		if baseContent == "" && p.editorNote.Content != "" {
			baseContent = p.editorNote.Content
		}
		draft := noteDraft{ID: p.editorNote.ID, Content: p.editorTextarea.Value(), BaseContent: baseContent}
		if p.saveInFlight && p.activeSaveID == draft.ID {
			draft.InFlightContent = p.activeSaveContent
		}
		path, err := writeNoteDraftState(root, draft)
		if err != nil {
			// Never block the lifecycle on td. The draft writer already tried its
			// secondary local checkpoint; retain the in-memory dirty state if both
			// filesystems reject it and report the durability failure.
			if logger != nil {
				logger.Error("notes: stop could not checkpoint dirty note", "error", err)
			}
			p.autoSaveID++
			return
		} else {
			store := p.store
			requestID := p.saveRequestID
			activeID, activeContent := p.activeSaveID, p.activeSaveContent
			p.editorDirty = false
			p.saveErr = nil
			p.store = nil
			stopSequence := p.nextWriteSequence(draft.ID)
			go p.flushStoppedDraft(store, path, draft, requestID, activeID, activeContent, stopSequence, logger)
		}
	}
	p.saveActivation++
	p.loadRequestID++
	p.externalPrepareID++
	p.exportSaveRequestID++
	p.exportSaveInFlight = false
	p.saveInFlight = false
	p.saveQueued = false
	p.pendingAfterSave = nil
	if p.store != nil {
		p.autoSaveID++
		_ = p.store.Close()
		p.store = nil
	}
}

// checkpointStoppedExport converts an editor temp file into the same atomic
// recovery draft used by the built-in editor before lifecycle teardown. No td
// or database work occurs here; the export is removed only after the durable
// checkpoint exists.
func (p *Plugin) checkpointStoppedExport(noteID, path, inFlightContent string) bool {
	if path == "" {
		return true
	}
	if noteID == "" || p.ctx == nil {
		return false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	base := ""
	for i := range p.notes {
		if p.notes[i].ID == noteID {
			base = p.notes[i].Content
			break
		}
	}
	if _, err := writeNoteDraftState(p.ctx.ProjectRoot, noteDraft{ID: noteID, Content: string(content), BaseContent: base, InFlightContent: inFlightContent}); err == nil {
		removeNoteExport(path)
		return true
	} else if p.ctx.Logger != nil {
		p.ctx.Logger.Error("notes: could not checkpoint editor export", "error", err)
	}
	return false
}

func (p *Plugin) flushStoppedDraft(store noteStore, path string, draft noteDraft, requestID uint64, activeID, activeContent string, sequence uint64, logger *slog.Logger) {
	if store == nil {
		return
	}
	p.saveMu.Lock()
	err := error(nil)
	if activeID != draft.ID || activeContent != draft.Content || p.lastWriteRequest != requestID || p.lastWriteErr != nil || p.lastWriteSkipped {
		_, err, _ = p.persistOrderedLocked(store, "", draft.ID, draft.Content, 0, sequence)
	}
	p.saveMu.Unlock()
	if err == nil {
		removeDraftIfCurrent(path, draft)
	} else if logger != nil {
		logger.Error("notes: background stop flush failed; draft retained", "error", err)
	}
	_ = store.Close()
}

func (p *Plugin) needsEditorCheckpoint() bool {
	if p.editorNote == nil {
		return false
	}
	desired := p.editorTextarea.Value()
	return desired != p.lastSavedContent ||
		(p.saveInFlight && p.activeSaveID == p.editorNote.ID && p.activeSaveContent != desired)
}

func (p *Plugin) nextWriteSequence(noteID string) uint64 {
	sequence := p.writeSequence.Add(1)
	p.latestWriteByNote.Store(noteID, sequence)
	return sequence
}

func (p *Plugin) writeIsLatest(noteID string, sequence uint64) bool {
	latest, ok := p.latestWriteByNote.Load(noteID)
	return !ok || latest.(uint64) == sequence
}

func (p *Plugin) acknowledgeWrite(noteID string, sequence uint64) {
	if noteID == "" || sequence == 0 {
		return
	}
	current, ok := p.durableWriteByNote.Load(noteID)
	if !ok || current.(uint64) < sequence {
		p.durableWriteByNote.Store(noteID, sequence)
	}
	p.cleanupSupersededExports(noteID, sequence)
}

func (p *Plugin) writeIsDurablySuperseded(noteID string, sequence uint64) bool {
	if sequence == 0 {
		return false
	}
	durable, ok := p.durableWriteByNote.Load(noteID)
	return ok && durable.(uint64) >= sequence
}

func (p *Plugin) persistOrdered(store noteStore, projectRoot, noteID, content string, startedAt int64, sequence uint64) (*Note, error, bool) {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	return p.persistOrderedLocked(store, projectRoot, noteID, content, startedAt, sequence)
}

func (p *Plugin) persistOrderedLocked(store noteStore, projectRoot, noteID, content string, startedAt int64, sequence uint64) (*Note, error, bool) {
	if !p.writeIsLatest(noteID, sequence) {
		return nil, nil, true
	}
	note, err := saveContentAndRetire(projectRoot, store, noteID, content, startedAt)
	return note, err, false
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if p.remoteBound() {
		if _, ok := msg.(app.ThemeChangedMsg); ok {
			p.applyEditorTextTheme()
		}
		return p, nil
	}
	if _, ok := msg.(app.ThemeChangedMsg); ok {
		p.applyEditorTextTheme()
		return p, nil
	}
	// Handle exit confirmation dialog first
	if p.edit.ShowExitConfirm {
		if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
			switch keyMsg.String() {
			case "j", "down":
				p.edit.MoveConfirmSelection(1)
				return p, nil
			case "k", "up":
				p.edit.MoveConfirmSelection(-1)
				return p, nil
			case "enter":
				return p.handleExitConfirmationChoice()
			case "esc", "q":
				// Cancel - return to editing
				p.edit.ShowExitConfirm = false
				p.edit.ClearPendingClick()
				return p, nil
			}
		}
		return p, nil
	}

	// Handle inline editor messages first when in inline edit mode
	if p.edit.Active && p.edit.Model != nil {
		// Check if editor became inactive or tmux session died
		// This proactively handles :wq exit before SessionDeadMsg arrives
		if !p.edit.Model.IsActive() || !p.isInlineEditSessionAlive() {
			noteID := p.inlineEditNoteID
			notePath := p.edit.Path
			p.exitInlineEditMode()
			return p, p.saveNoteAfterInlineExit(noteID, notePath)
		}

		if handled, cmd := p.handleTtyMessages(msg); handled {
			return p, cmd
		}
	}

	switch msg := msg.(type) {
	case app.NavigateToNoteMsg:
		return p, p.resolveNoteNavigation(msg)

	case NoteNavigationResolvedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.navigateRequestID || p.ctx == nil || msg.ProjectRoot != p.ctx.ProjectRoot {
			return p, nil
		}
		if msg.Err != nil || msg.Note == nil || msg.Note.DeletedAt != nil {
			return p, app.Blocked("Cannot open note " + msg.ID + ": it no longer exists in this project")
		}
		index := -1
		for i := range msg.Notes {
			if msg.Notes[i].ID == msg.ID && msg.Notes[i].DeletedAt == nil {
				index = i
				break
			}
		}
		if index < 0 {
			return p, app.Blocked("Cannot open note " + msg.ID + ": it no longer exists in this project")
		}
		return p, p.completeNoteNavigation(msg.ID, msg.Filter, msg.Notes, index)

	case InlineEditStartedMsg:
		if !p.ownsInlineEditMessage(msg.Activation, msg.Epoch) {
			return p, p.cleanupStaleInlineEditStart(msg)
		}
		return p, p.handleInlineEditStarted(msg)

	case InlineEditExitedMsg:
		if !p.ownsInlineEditMessage(msg.Activation, msg.Epoch) {
			return p, nil
		}
		return p, p.handleInlineEditExited(msg)

	case ExternalEditorPreparedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.externalPrepareID {
			p.removeExportIfUnowned(msg.Path)
			return p, nil
		}
		if msg.Err != nil || msg.Path == "" {
			if msg.Err == nil {
				msg.Err = errors.New("note export unavailable")
			}
			return p, showSaveFailedToast(msg.Err)
		}
		if p.pendingInlineEditPath != "" && p.pendingInlineEditPath != msg.Path {
			p.removeExportIfUnowned(p.pendingInlineEditPath)
		}
		p.pendingInlineEditID = msg.ID
		p.pendingInlineEditPath = msg.Path
		return p, func() tea.Msg {
			return plugin.OpenFileMsg{Editor: tty.ResolveEditor(), Path: msg.Path}
		}

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		// Update textarea dimensions
		p.updateTextareaDimensions()
		// Resize through the shared terminal lifecycle so control-backed targets
		// invalidate their old grid and reseed before regaining authority.
		if p.edit.Active && p.edit.Model != nil {
			width := p.calculateInlineEditorWidth()
			height := p.calculateInlineEditorHeight()
			if cmd := p.edit.Model.Resize(width, height); cmd != nil {
				return p, cmd
			}
		}

	case NotesLoadedMsg:
		// Check for stale message
		if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.loadRequestID || msg.Filter != p.viewFilter {
			return p, nil
		}
		p.loading = false
		p.recoveryErr = msg.RecoveryErr
		if msg.RecoveryErr != nil && p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Error("notes: unsaved draft recovery failed", "error", msg.RecoveryErr)
		}
		if errors.Is(msg.Err, tdsetup.ErrNotInitialized) {
			p.setupNeeded = true
			p.loadErr = nil
			p.setupErr = nil
			if !p.setupDismissed {
				p.showSetupModal = true
				p.clearSetupModal()
			}
		} else if msg.Err != nil {
			p.loadErr = msg.Err
			p.ctx.Logger.Error("notes: load failed", "error", msg.Err)
		} else {
			p.setupNeeded = false
			p.showSetupModal = false
			p.setupDismissed = false
			p.notes = p.reconcileMutation(msg.Notes)
			p.loadErr = nil

			// Optimistic creation already opened its temporary note in the editor;
			// every list snapshot reconciles through that mutation until td returns
			// the canonical identity. Ordinary reloads follow the current note.
			if p.editorNote != nil {
				// Follow the edited note if it moved position (due to updated_at sort)
				for i, n := range p.notes {
					if n.ID == p.editorNote.ID {
						p.cursor = i
						// Update editorNote reference to get latest content
						p.editorNote = &p.notes[i]
						// Out-of-band editor writes bypass textarea state, so force one sync.
						// A dirty built-in buffer owns the textarea; never overlay it.
						if p.pendingEditorSyncID == n.ID && !p.editorDirty {
							p.syncEditorFromNote(p.editorNote)
							p.pendingEditorSyncID = ""
						}
						break
					}
				}
				// Ensure cursor is visible in list
				p.ensureCursorVisibleForList(p.height-2, len(p.notes))
			} else if len(p.notes) > 0 {
				// Initial load: show the first note in the editor pane
				if p.cursor >= len(p.notes) {
					p.cursor = 0
				}
				return p, p.loadNoteIntoEditor()
			}
		}

	case NoteSavedMsg:
		if p.isStaleNoteSaveResult(msg.Epoch, msg.EditorActivation) {
			return p, nil
		}
		if msg.MutationID != 0 {
			return p, p.finishOptimisticCreate(msg)
		}
		if msg.Err != nil {
			p.ctx.Logger.Error("notes: save failed", "error", msg.Err)
		} else {
			return p, p.loadNotes()
		}

	case NoteDeletedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.MutationID != 0 {
			return p, p.finishOptimisticDelete(msg)
		}
		if msg.Err != nil {
			p.ctx.Logger.Error("notes: delete failed", "error", msg.Err)
		} else {
			return p, p.loadNotes()
		}

	case NotePinToggledMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		return p, p.loadNotes()

	case NoteArchiveToggledMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.MutationID != 0 {
			return p, p.finishOptimisticArchive(msg)
		}
		if msg.Err != nil {
			if p.ctx != nil && p.ctx.Logger != nil {
				p.ctx.Logger.Error("notes: archive failed", "error", msg.Err)
			}
			return p, nil
		}
		return p, p.loadNotes()

	case NoteRestoredMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err != nil {
			if msg.Action.Type != "" {
				p.pushUndo(msg.Action)
			}
			p.undoErr = msg.Err
			p.ctx.Logger.Error("notes: restore failed", "error", msg.Err)
			return p, showRestoreFailedToast(msg.Err)
		} else {
			p.undoErr = nil
			p.ctx.Logger.Debug("notes: restored", "id", msg.ID)
			return p, tea.Batch(
				showRestoredToast(msg.Title),
				p.loadNotes(),
			)
		}

	case NoteContentSavedMsg:
		if p.isStaleNoteSaveResult(msg.Epoch, msg.EditorActivation) {
			return p, nil
		}
		if msg.External {
			if msg.ExportRequestID != p.activeExport.RequestID || msg.ExportPath != p.activeExport.Path {
				return p, nil
			}
			p.exportSaveInFlight = false
			if msg.Err != nil {
				if p.hasQueuedExportForNote(p.activeExport.ID) {
					p.supersededExports = append(p.supersededExports, p.activeExport)
					p.activeExport = retainedExport{}
					p.saveErr = nil
					return p, p.startNextRetainedExport()
				}
				p.saveErr = msg.Err
				p.pendingInlineEditID = p.activeExport.ID
				p.pendingInlineEditPath = p.activeExport.Path
				if p.ctx != nil && p.ctx.Logger != nil {
					p.ctx.Logger.Error("notes: external editor save failed", "error", msg.Err)
				}
				return p, showSaveFailedToast(msg.Err)
			}
			finished := p.activeExport
			p.activeExport = retainedExport{}
			if msg.Skipped {
				if p.writeIsDurablySuperseded(finished.ID, finished.Sequence) {
					removeNoteExport(finished.Path)
				} else {
					p.supersededExports = append(p.supersededExports, finished)
				}
				if next := p.startNextRetainedExport(); next != nil {
					return p, next
				}
				return p, nil
			}
			p.saveErr = nil
			removeNoteExport(msg.ExportPath)
			p.acknowledgeWrite(msg.ID, msg.WriteSequence)
			if p.pendingInlineEditPath == msg.ExportPath {
				p.pendingInlineEditID = ""
				p.pendingInlineEditPath = ""
			}
			if next := p.startNextRetainedExport(); next != nil {
				return p, next
			}
			// Reload so the list follows $EDITOR. A dirty built-in buffer is
			// not overlaid (NotesLoaded skips pendingEditorSync when dirty).
			return p, tea.Batch(showSavedToast(), p.loadNotes())
		}
		if msg.Skipped {
			if msg.SaveActivation == p.saveActivation && msg.RequestID == p.saveRequestID {
				p.saveInFlight = false
			}
			return p, nil
		}
		if msg.EditorActivation != 0 {
			if msg.Err != nil {
				if p.ctx != nil && p.ctx.Logger != nil {
					p.ctx.Logger.Error("notes: content save failed", "error", msg.Err)
				}
				return p, nil
			}
			// Inline/tmux save owns the export, not the built-in textarea.
			// Clearing editorDirty here would drop typing that happened after :wq.
			return p, tea.Batch(showSavedToast(), p.loadNotes())
		}
		if msg.Err != nil && msg.SaveActivation == 0 {
			if p.ctx != nil && p.ctx.Logger != nil {
				p.ctx.Logger.Error("notes: content save failed", "error", msg.Err)
			}
			return p, showSaveFailedToast(msg.Err)
		}
		if msg.SaveActivation != p.saveActivation || msg.RequestID != p.saveRequestID || !p.saveInFlight {
			return p, nil
		}
		p.saveInFlight = false
		if msg.Err != nil {
			p.saveErr = msg.Err
			p.saveQueued = false
			p.revertCursorToEditorNote()
			if p.ctx != nil && p.ctx.Logger != nil {
				p.ctx.Logger.Error("notes: content save failed", "error", msg.Err)
			}
			return p, showSaveFailedToast(msg.Err)
		}
		p.saveErr = nil
		p.applySavedContent(msg)
		p.acknowledgeWrite(msg.ID, msg.WriteSequence)
		p.loadRequestID++ // any older list snapshot predates this canonical write
		if p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Debug("notes: content saved", "id", msg.ID)
		}
		if p.editorDirty && (p.saveQueued || p.pendingAfterSave != nil) {
			p.saveQueued = false
			return p, p.saveEditorContent()
		}
		p.saveQueued = false
		return p, tea.Batch(showSavedToast(), p.runPendingAfterSave())

	case TaskCreatedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err != nil {
			p.ctx.Logger.Error("notes: task creation failed", "error", msg.Err)
			return p, showTaskCreateFailedToast(msg.Err)
		}
		p.ctx.Logger.Debug("notes: task created", "taskID", msg.TaskID, "noteID", msg.NoteID)
		if msg.ArchiveErr != nil {
			p.ctx.Logger.Error("notes: archive after task create failed", "error", msg.ArchiveErr)
			return p, tea.Batch(
				showTaskCreatedArchiveFailedToast(msg.TaskID, msg.ArchiveErr),
				p.loadNotes(),
			)
		}
		return p, tea.Batch(showTaskCreatedToast(msg.TaskID), p.loadNotes())

	case AutoSaveTickMsg:
		// Debounce identity is the generation, not the focused pane. Tab/Esc
		// must not drop a pending save; the tick still owns this buffer.
		if msg.ID == p.autoSaveID && p.editorDirty {
			return p, p.saveEditorContent()
		}

	case InlineAutoSaveTickMsg:
		// Handle periodic auto-save during inline edit mode
		if p.edit.Active && msg.Generation == p.inlineAutoSaveGen {
			return p, p.performInlineAutoSave()
		}

	case InlineAutoSaveResultMsg:
		if plugin.IsStale(p.ctx, msg) ||
			!p.ownsInlineEditMessage(msg.Activation, msg.Epoch) ||
			msg.Generation != p.inlineAutoSaveGen {
			return p, nil
		}
		if msg.Err == nil && msg.Saved {
			p.inlineLastSavedContent = msg.Content
			p.acknowledgeWrite(p.inlineEditNoteID, msg.Sequence)
		}
		// Auto-save completed silently (no toast) - schedule next tick
		if p.edit.Active {
			return p, p.scheduleInlineAutoSave()
		}

	case app.RefreshMsg:
		// $EDITOR writes land while Sidecar is suspended; the refresh that
		// follows the resume is where we read them back.
		if p.pendingInlineEditID != "" && p.pendingInlineEditPath != "" {
			return p, p.readBackInlineEdit()
		}
		return p, p.loadNotes()

	case tdsetup.ResultMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err != nil {
			if msg.Origin == tdsetup.OriginNotes {
				p.setupInitializing = false
				p.setupErr = msg.Err
				p.showSetupModal = true
				p.clearSetupModal()
			}
			return p, nil
		}
		p.setupInitializing = false
		p.setupErr = nil
		p.setupNeeded = false
		p.showSetupModal = false
		p.setupDismissed = false
		p.clearSetupModal()
		return p, p.loadNotes()

	case app.ErrorMsg:
		// $EDITOR never launched, so only the error arrives and no refresh ever
		// comes to consume the pending read-back. Drop it with its temp file
		// rather than leaving both to outlive the attempt.
		if p.pendingInlineEditPath != "" {
			p.removeExportIfUnowned(p.pendingInlineEditPath)
		}
		p.pendingInlineEditID = ""
		p.pendingInlineEditPath = ""
		return p, nil

	case tea.KeyPressMsg:
		if p.showSetupModal {
			cmd, handled := p.handleSetupModalKey(msg)
			if handled {
				return p, cmd
			}
		}
		if p.setupNeeded {
			switch msg.String() {
			case "enter":
				p.showSetupModal = true
				p.setupDismissed = false
				p.clearSetupModal()
				return p, nil
			case "r":
				p.setupDismissed = false
				return p, p.loadNotes()
			}
			return p, nil
		}
		// Handle inline editor first if in inline edit mode
		if p.edit.Active {
			handled, cmd := p.handleInlineEditorKey(msg)
			if handled {
				return p, cmd
			}
		}
		// Handle info modal first if open
		if p.showInfoModal {
			p.ensureInfoModal()
			cmd, handled := p.handleInfoModalKey(msg)
			if handled {
				return p, cmd
			}
		}
		// Handle delete modal if open
		if p.showDeleteModal {
			p.ensureDeleteModal()
			cmd, handled := p.handleDeleteModalKey(msg)
			if handled {
				return p, cmd
			}
		}
		// Handle task modal if open
		if p.showTaskModal {
			p.ensureTaskModal()
			cmd, handled := p.handleTaskModalKey(msg)
			if handled {
				return p, cmd
			}
		}
		return p.handleKey(msg)

	case tea.MouseMsg:
		if p.showSetupModal {
			cmd, handled := p.handleSetupModalMouse(msg)
			if handled {
				return p, cmd
			}
		}
		// Inline-edit click-away is handled in handleMouse. Do not forward
		// presses to the tty model first — that hid list clicks (td-bb475e).
		// Handle info modal first if open
		if p.showInfoModal {
			p.ensureInfoModal()
			cmd, handled := p.handleInfoModalMouse(msg)
			if handled {
				return p, cmd
			}
		}
		// Handle delete modal if open
		if p.showDeleteModal {
			p.ensureDeleteModal()
			cmd, handled := p.handleDeleteModalMouse(msg)
			if handled {
				return p, cmd
			}
		}
		// Handle task modal if open
		if p.showTaskModal {
			p.ensureTaskModal()
			cmd, handled := p.handleTaskModalMouse(msg)
			if handled {
				return p, cmd
			}
		}
		return p.handleMouse(msg)

	case tea.PasteMsg:
		return p.handlePaste(msg)
	}

	// Pass through other messages to textarea (for cursor blink, etc.).
	// tea.PasteMsg is handled above; this path must not mutate content.
	if p.activePane == PaneEditor && !p.previewMode && p.editorNote != nil {
		var cmd tea.Cmd
		p.editorTextarea, cmd = p.editorTextarea.Update(msg)
		if cmd != nil {
			return p, cmd
		}
	}

	return p, nil
}

func (p *Plugin) resolveNoteNavigation(req app.NavigateToNoteMsg) tea.Cmd {
	if p.ctx == nil || p.store == nil || req.ID == "" || req.ProjectRoot == "" || req.ProjectRoot != p.ctx.ProjectRoot {
		return nil
	}
	p.navigateRequestID++
	requestID, epoch := p.navigateRequestID, p.ctx.Epoch
	store, projectRoot, id := p.store, p.ctx.ProjectRoot, req.ID
	return func() tea.Msg {
		note, err := store.Get(id)
		filter := FilterActive
		var notes []Note
		if err == nil && note != nil && note.DeletedAt == nil {
			if note.Archived {
				filter = FilterArchived
				notes, err = store.ListArchived()
			} else {
				notes, err = store.List(false)
			}
		}
		return NoteNavigationResolvedMsg{
			ID: id, ProjectRoot: projectRoot, Note: note, Notes: notes, Filter: filter,
			Err: err, Epoch: epoch, RequestID: requestID,
		}
	}
}

func (p *Plugin) completeNoteNavigation(id string, filter NoteFilter, notes []Note, index int) tea.Cmd {
	finish := func() tea.Cmd {
		p.viewFilter = filter
		p.searchMode = false
		p.searchField.Reset()
		p.filteredNotes = nil
		p.notes = p.reconcileMutation(notes)
		p.cursor = index
		for i := range p.notes {
			if p.notes[i].ID == id {
				p.cursor = i
				break
			}
		}
		p.activePane = PaneEditor
		p.previewMode = true
		if p.editorNote != nil && p.editorNote.ID == id {
			// The list reload replaced p.notes. Force the same stable ID through
			// the ordinary loader so title/content pointers cannot stay stale.
			p.editorNote = nil
		}
		load := p.loadNoteIntoEditor()
		return tea.Batch(load, app.FocusPlugin(pluginID))
	}
	if p.editorDirty {
		return p.saveBefore(finish)
	}
	return finish()
}

// handlePaste applies one tea.PasteMsg as a single operation for the focused
// surface. It never falls through to the textarea catch-all.
func (p *Plugin) handlePaste(msg tea.PasteMsg) (plugin.Plugin, tea.Cmd) {
	if p.showSetupModal || p.edit.ShowExitConfirm || p.showDeleteModal || p.showInfoModal {
		return p, nil
	}
	if p.showTaskModal {
		var cmd tea.Cmd
		p.taskModalTitleInput, cmd = p.taskModalTitleInput.Update(msg)
		return p, cmd
	}
	if p.edit.Active {
		return p, nil
	}
	if p.searchMode {
		p.pasteIntoSearch(msg.Content)
		return p, nil
	}
	// The in-note search bar is a text input too, and it owns the preview's
	// keyboard while it is taking text.
	if p.handleNoteSearchPaste(msg.Content) {
		return p, nil
	}
	if p.activePane == PaneEditor && p.editorNote != nil {
		if p.viewFilter != FilterActive {
			return p, readOnlyPasteToast(p.viewFilter)
		}
		return p.pasteIntoEditor(msg.Content)
	}
	if p.viewFilter != FilterActive {
		return p, readOnlyPasteToast(p.viewFilter)
	}
	return p, p.createNoteFromPaste(msg.Content)
}

func (p *Plugin) pasteIntoSearch(content string) {
	p.searchField.Focus()
	before := p.searchQuery()
	// The field's own sanitizer collapses newlines and tabs, because a query
	// bar is one line; the paste lands at the caret rather than at the end.
	p.searchField.HandlePaste(tea.PasteMsg{Content: content})
	if p.searchQuery() != before {
		p.updateFilteredNotes()
	}
}

func (p *Plugin) pasteIntoEditor(content string) (plugin.Plugin, tea.Cmd) {
	var cmds []tea.Cmd
	if p.previewMode {
		p.ensureViewSurface()
		a := p.viewSurface.At(p.previewCursorLine)
		p.prepareEdit(editOpPaste)
		p.insertAtSourceLine(a.SourceLine, content)
		newRow, newCol := pasteInsertPlace(a.SourceLine, content)
		if cmd := p.enterEditAt(newRow, newCol); cmd != nil {
			cmds = append(cmds, cmd)
		}
		p.clearEditSelection()
		cmds = append(cmds, p.afterContentChange())
		return p, tea.Batch(cmds...)
	}
	if p.hasEditSelection() {
		return p.replaceEditorSelection(content, editOpPaste)
	}
	p.prepareEdit(editOpPaste)
	p.editorTextarea.InsertString(content)
	if !p.editorTextarea.Focused() {
		cmds = append(cmds, p.editorTextarea.Focus())
	}
	p.clearEditSelection()
	p.trackTextareaScroll()
	cmds = append(cmds, p.afterContentChange())
	return p, tea.Batch(cmds...)
}

// insertAtSourceLine inserts text at the start of the given source line
// via SetValue. The caller must place the cursor at the insert point —
// SetValue leaves it at EOF.
func (p *Plugin) insertAtSourceLine(row int, content string) {
	lines := strings.Split(p.editorTextarea.Value(), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	lines[row] = content + lines[row]
	p.editorTextarea.SetValue(strings.Join(lines, "\n"))
	p.invalidateViewSurface()
}

// pasteInsertPlace is the (row, col) after prepending content at startRow.
func pasteInsertPlace(startRow int, content string) (row, col int) {
	if startRow < 0 {
		startRow = 0
	}
	parts := strings.Split(content, "\n")
	if len(parts) == 1 {
		return startRow, len([]rune(parts[0]))
	}
	return startRow + len(parts) - 1, len([]rune(parts[len(parts)-1]))
}

func (p *Plugin) createNoteFromPaste(content string) tea.Cmd {
	if strings.TrimSpace(content) == "" || p.store == nil {
		return nil
	}
	if p.editorDirty {
		return p.saveBefore(func() tea.Cmd { return p.createNoteFromPaste(content) })
	}
	return p.beginOptimisticCreate(firstNonBlankLine(content), content)
}

func firstNonBlankLine(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func readOnlyPasteToast(filter NoteFilter) tea.Cmd {
	label := "Notes"
	switch filter {
	case FilterArchived:
		label = "Archived notes"
	case FilterDeleted:
		label = "Deleted notes"
	}
	return func() tea.Msg {
		// A refusal the user provokes by typing: frequent, and worth saying
		// only while it is on screen.
		return msg.FlashMsg{Text: label + " are read-only"}
	}
}

func (p *Plugin) isStaleNoteSaveResult(epoch, editorActivation uint64) bool {
	if p.ctx == nil || epoch != p.ctx.Epoch {
		return true
	}
	return editorActivation != 0 && editorActivation != p.edit.Activation
}

// applySavedContent updates the cached note without paying for a full list
// reload. It clears dirty only when this exact buffer still owns the editor;
// typing that landed while td was slow remains dirty and is saved next.
func (p *Plugin) applySavedContent(msg NoteContentSavedMsg) {
	editedID := ""
	if p.editorNote != nil {
		editedID = p.editorNote.ID
	}
	for i := range p.notes {
		if p.notes[i].ID != msg.ID {
			continue
		}
		if msg.Note != nil {
			p.notes[i] = *msg.Note
		} else {
			p.notes[i].Content = msg.Content
		}
		break
	}
	sort.SliceStable(p.notes, func(i, j int) bool {
		if p.notes[i].Pinned != p.notes[j].Pinned {
			return p.notes[i].Pinned
		}
		return p.notes[i].UpdatedAt.After(p.notes[j].UpdatedAt)
	})
	if p.searchQuery() != "" {
		p.updateFilteredNotes()
	}
	for i := range p.notes {
		if p.notes[i].ID == editedID {
			p.editorNote = &p.notes[i]
			break
		}
	}
	if p.pendingAfterSave == nil {
		p.revertCursorToEditorNote()
	}
	if p.editorNote == nil || p.editorNote.ID != msg.ID {
		return
	}
	p.lastSavedContent = msg.Content
	p.editorDirty = p.editorTextarea.Value() != msg.Content
}

// FooterStatus keeps slow/failed saves visible after the transient toast.
func (p *Plugin) FooterStatus() (string, bool) {
	if p.mutationErr != nil {
		return "notes: " + p.mutationAction + " failed — retry the action", true
	}
	if p.saveErr != nil {
		return "notes: save failed — Ctrl-S to retry", true
	}
	if p.undoErr != nil {
		return "notes: undo failed — u to retry", true
	}
	if p.recoveryErr != nil {
		return "notes: unsaved draft recovery failed — r to retry", true
	}
	if p.mutation != nil {
		switch p.mutation.kind {
		case noteMutationDelete:
			return "notes: deleting…", false
		case noteMutationArchive:
			return "notes: archiving…", false
		default:
			return "notes: creating…", false
		}
	}
	// An in-flight save is routine and self-resolving: no status line.
	return "", false
}

// handleKey processes keyboard input.
func (p *Plugin) handleKey(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+s" && p.activeExport.Path != "" && !p.exportSaveInFlight {
		return p, p.retryActiveExport()
	}
	if key == "ctrl+s" && p.pendingInlineEditID != "" && p.pendingInlineEditPath != "" {
		return p, p.readBackInlineEdit()
	}
	if key == "ctrl+s" && p.editorDirty {
		p.autoSaveID++
		return p, p.saveEditorContent()
	}

	// Workspace terminal split is not a notes surface. Preference-gated off
	// elsewhere; here the chord is a no-op so it cannot leak into a split.
	if key == "ctrl+t" {
		return p, nil
	}

	// Handle search mode input (only when in list pane)
	if p.searchMode {
		return p.handleSearchKey(msg)
	}

	// Handle editor pane input
	if p.activePane == PaneEditor && p.editorNote != nil {
		return p.handleEditorKey(msg)
	}

	// Handle g g sequence for jump to top
	if p.pendingG {
		p.pendingG = false
		if key == "g" {
			p.cursor = 0
			p.scrollOff = 0
			return p, nil
		}
		// Not a g, fall through to normal handling
	}

	// Enter search mode with /
	if key == "/" {
		p.searchMode = true
		p.searchField.Reset()
		p.searchField.Focus()
		p.updateFilteredNotes()
		return p, nil
	}

	// Tab switches between panes (only if editor has a note open).
	// Landing on the body focuses the resting view so `m` and j/k work;
	// Enter/i/click are what enter edit.
	if key == "tab" && p.editorNote != nil {
		if p.activePane == PaneList {
			p.focusEditorPane()
			return p, nil
		}
		return p, p.focusListPane()
	}

	// Esc returns to Active view from Archived/Deleted views
	if key == "esc" && p.viewFilter != FilterActive {
		return p, p.switchViewFilter(FilterActive)
	}
	// The filter shortcuts are toggles: pressing the shortcut for the current
	// archived/deleted view returns to Active. Handle these before the empty-list
	// guard so an empty filter is never a keyboard trap.
	if key == "a" {
		return p, p.switchViewFilter(toggleNoteFilter(p.viewFilter, FilterArchived))
	}
	if key == "x" {
		return p, p.switchViewFilter(toggleNoteFilter(p.viewFilter, FilterDeleted))
	}

	// Get the notes list to navigate (filtered or all)
	notesList := p.getDisplayNotes()

	// Skip navigation operations when notes list is empty
	if len(notesList) == 0 {
		switch key {
		case "n", "c":
			// Create new note - allowed even with empty list
			return p, p.createNote()
		case "r":
			// Refresh - allowed even with empty list
			return p, p.loadNotes()
		case "alt+v", "super+v":
			// Paste creates a note from the session copy, exactly as a real
			// paste does with no list to stand in the way.
			return p, p.pasteRecentCmd()
		}
		return p, nil
	}

	switch key {
	case "j", "down":
		if p.cursor < len(notesList)-1 {
			p.cursor++
		}
		return p, p.loadNoteIntoEditor()
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, p.loadNoteIntoEditor()
	case "g":
		// Start g g sequence
		p.pendingG = true
	case "G":
		// Jump to bottom
		p.cursor = len(notesList) - 1
		return p, p.loadNoteIntoEditor()
	case "n", "c":
		// Create new note (only in Active view)
		if p.viewFilter == FilterActive {
			return p, p.createNote()
		}
		return p, nil
	case "X":
		// Delete note with confirmation (only in Active view)
		if p.viewFilter == FilterActive {
			return p, p.openDeleteModal()
		}
		return p, nil
	case "p":
		// Toggle pin (only in Active view)
		if p.viewFilter == FilterActive {
			return p, p.togglePin()
		}
		return p, nil
	case "A":
		// Archive note (only in Active view)
		if p.viewFilter == FilterActive {
			return p, p.toggleArchive()
		}
		return p, nil
	case "r":
		// Refresh
		return p, p.loadNotes()
	case "enter":
		// Enter is the configurable default gesture. Archived/deleted notes still
		// open only as a read-only preview.
		note := p.getSelectedNote()
		if note != nil {
			if cmd := p.loadNoteIntoEditor(); cmd != nil {
				return p, cmd
			}
			p.activePane = PaneEditor
			if p.viewFilter != FilterActive {
				p.previewMode = true
				return p, nil
			}
			return p, p.openDefaultEditor()
		}
		return p, nil
	case "i":
		// Built-in editor, regardless of the default preference.
		if p.viewFilter == FilterActive {
			if cmd := p.loadNoteIntoEditor(); cmd != nil {
				return p, cmd
			}
			p.activePane = PaneEditor
			return p, p.enterEditAtPreviewPlace()
		}
		return p, nil
	case "e":
		// Vim in the embedded right pane.
		if p.viewFilter == FilterActive {
			return p, p.editSelectedNote()
		}
		return p, nil
	case "E":
		// External $EDITOR.
		if p.viewFilter == FilterActive {
			return p, p.openInExternalEditor()
		}
		return p, nil
	case "T":
		// Convert note to task (only in Active view)
		if p.viewFilter == FilterActive {
			return p, p.openTaskModal()
		}
		return p, nil
	case "I":
		// Show info modal for selected note
		return p, p.openInfoModal()
	case "y":
		// Yank note content to clipboard
		return p, p.yankNoteContent()
	case "Y":
		// Yank note title to clipboard
		return p, p.yankNoteTitle()
	case "ctrl+y":
		return p, p.yankNoteID()
	case "alt+v", "super+v":
		return p, p.pasteRecentCmd()
	case "u":
		// Undo last delete/archive (only in Active view)
		if p.viewFilter == FilterActive {
			return p, p.undoLastAction()
		}
		return p, nil
	}
	return p, nil
}

// handleEditorKey processes keyboard input when editor pane is focused.
func (p *Plugin) handleEditorKey(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// In preview mode, only allow navigation and mode switches
	if p.previewMode {
		return p.handleEditorPreviewKey(msg)
	}

	switch key {
	case "tab", "esc":
		p.clearEditSelection()
		p.leaveEditToView()
		p.activePane = PaneList
		return p, p.saveEditorContent()

	case "ctrl+s":
		p.autoSaveID++
		return p, p.saveEditorContent()

	case "ctrl+z", "alt+z", "super+z":
		return p.undoEditorEdit()

	case "ctrl+y", "ctrl+shift+z", "alt+shift+z", "super+shift+z":
		return p.redoEditorEdit()

	case "alt+s":
		return p.toggleSelectAnchor()

	case "alt+a":
		return p.selectAllEditor()

	// Deliberately no "E" here. Before the notes rework this handler claimed
	// it for the external editor, which meant a capital E could never be typed
	// into a note. E reaches $EDITOR from the list and from preview; inside the
	// simple editor every printable key belongs to the textarea.

	case "alt+c":
		return p, p.copyEditorOrSelection()

	case "alt+x":
		return p.cutEditorSelection()

	case "alt+v", "super+v":
		return p, p.pasteRecentCmd()
	}

	// Command-key delivery depends on the terminal. Match modifier bits
	// directly so a populated Text field cannot hide ModSuper, and require an
	// exact chord so Ctrl/Alt combinations remain owned by the textarea.
	actionMods := msg.Mod &^ (tea.ModCapsLock | tea.ModNumLock)
	if msg.Code == 'a' && actionMods == tea.ModSuper {
		return p.selectAllEditor()
	}
	if (msg.Code == tea.KeyUp || msg.Code == tea.KeyDown) &&
		(actionMods == tea.ModSuper || actionMods == tea.ModSuper|tea.ModShift) {
		return p.moveEditorToNoteBoundary(msg.Code == tea.KeyDown, actionMods.Contains(tea.ModShift))
	}

	if isShiftMotion(msg) || (p.selExtend && isMotionKey(msg)) {
		return p.extendSelectionByMotion(msg)
	}

	if p.hasEditSelection() && isDeleteKey(msg) {
		return p.deleteEditorSelection(editOpReplace)
	}
	if p.hasEditSelection() && isInsertKey(msg) {
		text := msg.Text
		if key == "enter" || key == "ctrl+m" {
			text = "\n"
		}
		return p.replaceEditorSelection(text, editOpReplace)
	}

	if !p.hasEditSelection() && (key == "enter" || key == "ctrl+m") {
		if cmd, ok := p.continueListOnEnter(); ok {
			return p, cmd
		}
	}

	if p.hasEditSelection() && isMotionKey(msg) {
		p.clearEditSelection()
	}

	oldValue := p.editorTextarea.Value()
	kind := editOpNone
	if isInsertKey(msg) {
		kind = editOpTyping
	} else if isDeleteKey(msg) {
		kind = editOpDelete
	}
	if kind != editOpNone {
		p.prepareEdit(kind)
	}

	var cmd tea.Cmd
	p.editorTextarea, cmd = p.editorTextarea.Update(msg)
	p.trackTextareaScroll()

	if p.editorTextarea.Value() != oldValue {
		return p, tea.Batch(cmd, p.afterContentChange())
	}
	return p, cmd
}

// handleEditorPreviewKey handles keys in preview mode (read-only).
func (p *Plugin) handleEditorPreviewKey(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	if p.noteSearchMode {
		return p.handleNoteSearchKey(msg)
	}

	switch key {
	case "ctrl+s":
		if p.editorDirty {
			p.autoSaveID++
			return p, p.saveEditorContent()
		}
		return p, nil

	case "tab":
		p.clearNoteSearch()
		p.activePane = PaneList
		return p, nil

	case "esc":
		p.clearNoteSearch()
		p.activePane = PaneList
		return p, nil

	case "enter":
		if p.viewFilter == FilterActive {
			return p, p.openDefaultEditor()
		}
		return p, nil

	case "i":
		if p.viewFilter == FilterActive {
			return p, p.enterEditAtPreviewPlace()
		}
		return p, nil

	case "e":
		if p.viewFilter == FilterActive {
			return p, p.editSelectedNote()
		}
		return p, nil

	case "E":
		if p.viewFilter == FilterActive {
			return p, p.openInExternalEditor()
		}
		return p, nil

	case "alt+c":
		return p, p.copyEditorContent()

	case "ctrl+y":
		return p, p.yankNoteID()

	case "alt+v", "super+v":
		return p, p.pasteRecentCmd()

	case "j", "down", "ctrl+n":
		p.ensureViewSurface()
		if p.previewCursorLine < len(p.viewSurface.Lines)-1 {
			p.previewCursorLine++
		}
		p.ensurePreviewCursorVisible()

	case "k", "up", "ctrl+p":
		if p.previewCursorLine > 0 {
			p.previewCursorLine--
		}
		p.ensurePreviewCursorVisible()

	case "g":
		p.previewCursorLine = 0
		p.previewScrollOff = 0

	case "G":
		p.ensureViewSurface()
		if len(p.viewSurface.Lines) > 0 {
			p.previewCursorLine = len(p.viewSurface.Lines) - 1
		}
		p.ensurePreviewCursorVisible()

	case "m":
		p.toggleMarkdownView()

	case "/":
		if p.editorNote != nil {
			p.startNoteSearch()
		}
	}

	return p, nil
}

// ensurePreviewCursorVisible adjusts preview scroll offset to keep cursor visible.
// Uses last known height from the view dimensions.
func (p *Plugin) ensurePreviewCursorVisible() {
	height, width := p.previewViewport()
	p.ensurePreviewCursorVisibleWithHeight(height, width)
}

// setTextareaCursorPosition navigates the textarea cursor to the specified row and column.
// Uses CursorUp/CursorDown since textarea has no SetRow API. Soft-wrapped visual
// rows keep the same Line(), so the walk continues through those instead of stopping.
func (p *Plugin) setTextareaCursorPosition(row, col int) {
	lineCount := p.editorTextarea.LineCount()
	if lineCount == 0 {
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= lineCount {
		row = lineCount - 1
	}

	current := p.editorTextarea.Line()
	guard := 0
	maxGuard := lineCount * 8
	if maxGuard < 8 {
		maxGuard = 8
	}
	for current > row && guard < maxGuard {
		p.editorTextarea.CursorUp()
		next := p.editorTextarea.Line()
		if next > current {
			break
		}
		current = next
		guard++
	}
	guard = 0
	for current < row && guard < maxGuard {
		p.editorTextarea.CursorDown()
		next := p.editorTextarea.Line()
		if next < current {
			break
		}
		current = next
		guard++
	}

	p.editorTextarea.SetCursorColumn(col)
}

// textareaViewportSeedMsg is a no-op Update so bubbles can SetContent
// on the real viewport. textarea.View is a value receiver and does not
// persist that, and repositionView cannot scroll an empty viewport.
type textareaViewportSeedMsg struct{}

func (p *Plugin) seedTextareaViewport() {
	if p.editorTextarea.LineCount() == 0 {
		return
	}
	if !p.editorTextarea.Focused() {
		_ = p.editorTextarea.Focus()
	}
	p.editorTextarea, _ = p.editorTextarea.Update(textareaViewportSeedMsg{})
}

// setTextareaCursorAndScroll places the cursor on a source line and restores a
// visual-row viewport offset. Bubbles' ScrollYOffset is measured after soft
// wrapping, not in logical source lines.
func (p *Plugin) setTextareaCursorAndScroll(row, col, scrollOff int) {
	p.updateTextareaDimensions()
	p.seedTextareaViewport()
	lineCount := p.editorTextarea.LineCount()
	if lineCount == 0 {
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= lineCount {
		row = lineCount - 1
	}
	l := p.editorLayout()
	height := l.contentHeight
	if height < 1 {
		height = 1
	}
	if scrollOff < 0 {
		scrollOff = 0
	}

	// There is no public SetScrollYOffset on textarea. Walk a temporary cursor
	// to the bottom of the desired viewport so its normal repositioning lands
	// on scrollOff, then put the real cursor back inside that viewport.
	p.editorTextarea.MoveToBegin()
	forceVisual := scrollOff + height - 1
	for visual := 0; visual < forceVisual; visual++ {
		beforeLine := p.editorTextarea.Line()
		beforeCol := p.editorTextarea.Column()
		p.editorTextarea.CursorDown()
		if p.editorTextarea.Line() == beforeLine && p.editorTextarea.Column() == beforeCol {
			break
		}
	}
	p.setTextareaCursorPosition(row, col)
	// SetCursorColumn does not reposition the viewport. A no-op update does,
	// after the target cursor has been restored.
	p.editorTextarea, _ = p.editorTextarea.Update(textareaViewportSeedMsg{})
	p.previewCursorLine = row
	p.previewScrollOff = p.editorTextarea.ScrollYOffset()
}

// enterEditAtPreviewPlace drops into the textarea on the mapped source line
// and keeps that line on (or near) the same screen row.
func (p *Plugin) enterEditAtPreviewPlace() tea.Cmd {
	if p.viewFilter != FilterActive || p.editorNote == nil {
		return nil
	}
	if !p.previewMode {
		if !p.editorTextarea.Focused() {
			return p.editorTextarea.Focus()
		}
		return nil
	}
	p.ensureViewSurface()
	a := p.viewSurface.At(p.previewCursorLine)
	return p.enterEditAt(a.SourceLine, a.SourceCol)
}

// openDefaultEditor applies the configured default only to the default Enter
// and body-click journey. Explicit i/e/E commands never call this helper.
func (p *Plugin) openDefaultEditor() tea.Cmd {
	if p.viewFilter != FilterActive || p.editorNote == nil {
		return nil
	}
	if p.ctx != nil && p.ctx.Config != nil &&
		p.ctx.Config.Plugins.Notes.DefaultEditor == config.NotesEditorPane {
		return p.editSelectedNote()
	}
	return p.enterEditAtPreviewPlace()
}

// enterEditAt switches to edit mode at a source line/column. The current
// visual screen row is preserved across rendered/raw and textarea wrapping.
func (p *Plugin) enterEditAt(row, col int) tea.Cmd {
	p.clearNoteSearch()
	screenRow := p.previewCursorLine - p.previewScrollOff
	if screenRow < 0 {
		screenRow = 0
	}
	p.previewMode = false
	l := p.editorLayout()
	raw := markdown.MapWrappedSource(p.editorTextarea.Value(), l.wrapColumn)
	editVisual := raw.VisualRowForSource(row, col)
	editScroll := editVisual - screenRow
	if editScroll < 0 {
		editScroll = 0
	}
	p.setTextareaCursorAndScroll(row, col, editScroll)
	// Always return Focus so the blink cmd is not lost if seedTextareaViewport
	// already focused the textarea to populate its viewport.
	return p.editorTextarea.Focus()
}

// leaveEditToView returns the body to the resting rendered/raw view, mapping
// the textarea cursor back to a visual row on the same screen row.
func (p *Plugin) leaveEditToView() {
	if p.editorNote == nil {
		p.previewMode = true
		return
	}
	srcLine := p.editorTextarea.Line()
	srcCol := p.editorTextarea.Column()
	l := p.editorLayout()
	raw := markdown.MapWrappedSource(p.editorTextarea.Value(), l.wrapColumn)
	screenRow := raw.VisualRowForSource(srcLine, srcCol) - p.editorTextarea.ScrollYOffset()
	if screenRow < 0 {
		screenRow = 0
	}
	p.syncPreviewFromTextarea()
	p.previewMode = true
	p.ensureViewSurface()
	visual := p.viewSurface.VisualRowForSource(srcLine, srcCol)
	p.previewCursorLine = visual
	p.previewScrollOff = visual - screenRow
	p.ensurePreviewCursorVisible()
	p.editorTextarea.Blur()
	p.rememberCurrentPlace()
	p.clearEditSelection()
}

// captureEditPlace writes the textarea cursor/viewport back onto the preview
// place and the per-note session memory.
func (p *Plugin) captureEditPlace() {
	if p.editorNote == nil {
		return
	}
	p.syncPreviewFromTextarea()
	if !p.previewMode {
		p.previewCursorLine = p.editorTextarea.Line()
		p.trackTextareaScroll()
	}
	p.rememberCurrentPlace()
}

func (p *Plugin) rememberCurrentPlace() {
	if p.editorNote == nil {
		return
	}
	if p.notePlaces == nil {
		p.notePlaces = make(map[string]notePlace)
	}
	place := p.notePlaces[p.editorNote.ID]
	if p.previewMode {
		p.ensureViewSurface()
		cursor := p.viewSurface.At(p.previewCursorLine)
		scroll := p.viewSurface.At(p.previewScrollOff)
		place.previewCursorLine = cursor.SourceLine
		place.previewScrollOff = scroll.SourceLine
		place.sourceCol = cursor.SourceCol
	} else {
		place.editRow = p.editorTextarea.Line()
		place.editCol = p.editorTextarea.Column()
		place.editScrollOff = p.previewScrollOff
		place.hasEdit = true
		place.previewCursorLine = place.editRow
		place.previewScrollOff = place.editScrollOff
		place.sourceCol = place.editCol
	}
	p.notePlaces[p.editorNote.ID] = place
}

func (p *Plugin) restorePlace(noteID string) {
	place, ok := p.notePlaces[noteID]
	p.ensureViewSurface()
	if !ok {
		p.previewCursorLine = 0
		p.previewScrollOff = 0
		p.setTextareaCursorAndScroll(0, 0, 0)
		p.ensurePreviewCursorVisible()
		return
	}
	if place.hasEdit {
		p.setTextareaCursorAndScroll(place.editRow, place.editCol, place.editScrollOff)
	} else {
		p.setTextareaCursorAndScroll(place.previewCursorLine, place.sourceCol, place.previewScrollOff)
	}
	if p.previewMode {
		p.previewCursorLine = p.viewSurface.VisualRowForSource(place.previewCursorLine, place.sourceCol)
		p.previewScrollOff = p.viewSurface.VisualRowForSource(place.previewScrollOff, 0)
	} else {
		p.previewCursorLine = place.previewCursorLine
		p.previewScrollOff = place.previewScrollOff
	}
	p.ensurePreviewCursorVisible()
}

// syncPreviewFromTextarea updates previewLines from the current textarea content.
// Call this whenever switching from edit mode to preview/list mode.
func (p *Plugin) syncPreviewFromTextarea() {
	content := p.editorTextarea.Value()
	p.previewLines = strings.Split(content, "\n")
	if len(p.previewLines) == 0 {
		p.previewLines = []string{""}
	}
	p.invalidateViewSurface()
}

func (p *Plugin) invalidateViewSurface() {
	p.viewSurfaceSrc = ""
	p.viewSurfaceWidth = 0
	p.viewSurfaceStyle = ""
	p.viewSurface = markdown.MappedRender{}
}

func (p *Plugin) ensureViewSurface() {
	src := strings.Join(p.previewLines, "\n")
	if len(p.previewLines) == 0 {
		src = ""
	}
	width := 1
	if p.width > 0 && p.height > 0 {
		width = p.editorLayout().wrapColumn
	}
	if width < 1 {
		width = 1
	}
	// Only the glamour surface carries theme state; the raw wrap has none, so
	// it must not churn on a theme change.
	style := ""
	if p.markdownView && p.md != nil {
		style = p.md.StyleKey()
	}
	if p.viewSurfaceSrc == src && p.viewSurfaceWidth == width && p.viewSurfaceMD == p.markdownView &&
		p.viewSurfaceStyle == style && len(p.viewSurface.Lines) > 0 {
		return
	}
	// A theme change is the one rebuild that leaves the source and width alone,
	// so the cursor and scroll are re-anchored through the source mapping
	// rather than left pointing at visual rows from the previous render.
	reanchor := p.viewSurfaceSrc == src && p.viewSurfaceWidth == width &&
		p.viewSurfaceMD == p.markdownView && p.viewSurfaceStyle != style &&
		len(p.viewSurface.Lines) > 0
	var cursorAnchor, scrollAnchor markdown.Anchor
	screenRow := 0
	if reanchor {
		cursorAnchor = p.viewSurface.At(p.previewCursorLine)
		scrollAnchor = p.viewSurface.At(p.previewScrollOff)
		screenRow = p.previewCursorLine - p.previewScrollOff
	}
	if p.markdownView && p.md != nil {
		p.viewSurface = renderNotesMarkdown(p.md, src, width)
	} else {
		p.viewSurface = markdown.MapWrappedSource(src, width)
	}
	if len(p.viewSurface.Lines) == 0 {
		p.viewSurface = markdown.MappedRender{
			Lines:   []string{""},
			Anchors: []markdown.Anchor{{Precise: true}},
		}
	}
	p.viewSurfaceSrc = src
	p.viewSurfaceWidth = width
	p.viewSurfaceMD = p.markdownView
	p.viewSurfaceStyle = style
	if reanchor {
		p.previewCursorLine = p.viewSurface.VisualRowForSource(cursorAnchor.SourceLine, cursorAnchor.SourceCol)
		scroll := p.viewSurface.VisualRowForSource(scrollAnchor.SourceLine, scrollAnchor.SourceCol)
		if p.previewCursorLine-screenRow >= 0 {
			scroll = p.previewCursorLine - screenRow
		}
		p.previewScrollOff = max(scroll, 0)
	}
}

func (p *Plugin) toggleMarkdownView() {
	if !p.previewMode {
		return
	}
	p.ensureViewSurface()
	a := p.viewSurface.At(p.previewCursorLine)
	screenRow := p.previewCursorLine - p.previewScrollOff
	p.markdownView = !p.markdownView
	p.invalidateViewSurface()
	p.ensureViewSurface()
	if p.noteSearchMode && p.noteSearchQuery() != "" {
		p.updateNoteSearchMatches()
	}
	visual := p.viewSurface.VisualRowForSource(a.SourceLine, a.SourceCol)
	p.previewCursorLine = visual
	p.previewScrollOff = visual - screenRow
	p.ensurePreviewCursorVisible()
}

// syncEditorFromNote refreshes editor/preview buffers from the note content.
func (p *Plugin) syncEditorFromNote(note *Note) {
	if note == nil {
		return
	}
	p.editorTextarea.SetValue(note.Content)
	p.previewLines = strings.Split(note.Content, "\n")
	if len(p.previewLines) == 0 {
		p.previewLines = []string{""}
	}
	p.invalidateViewSurface()
	if p.previewCursorLine < 0 {
		p.previewCursorLine = 0
	}
	p.ensurePreviewCursorVisible()
	p.editorDirty = false
	p.lastSavedContent = note.Content
	p.clearEditSelection()
	if p.editHistories != nil && note.ID != "" {
		delete(p.editHistories, note.ID)
	}
}

// loadNoteIntoEditor loads the currently selected note into the editor pane
// and restores this session's remembered place (start of note if none).
// A dirty buffer is persisted first; on failure the buffer stays and a toast
// is returned so the edit cannot be silently dropped.
func (p *Plugin) loadNoteIntoEditor() tea.Cmd {
	note := p.getSelectedNote()
	if note == nil {
		return p.abandonEditor()
	}

	if p.editorNote != nil && p.editorNote.ID == note.ID {
		// Navigation is latest-wins while a save is pending. Moving back to
		// the note whose buffer is still visible cancels an older destination.
		if p.editorDirty {
			p.pendingAfterSave = nil
		}
		return nil
	}

	if p.editorDirty {
		targetID := note.ID
		return p.saveBefore(func() tea.Cmd {
			p.moveCursorToNote(targetID)
			return p.loadNoteIntoEditor()
		})
	}

	p.rememberCurrentPlace()
	p.clearNoteSearch()
	p.editorNote = note
	p.editorTextarea.SetValue(note.Content)
	p.previewLines = strings.Split(note.Content, "\n")
	if len(p.previewLines) == 0 {
		p.previewLines = []string{""}
	}
	p.invalidateViewSurface()
	p.editorDirty = false
	p.lastSavedContent = note.Content
	p.previewMode = true
	p.restorePlace(note.ID)
	p.editorTextarea.Blur()
	p.clearEditSelection()
	return nil
}

// loadNoteIntoEditorAtEnd loads the currently selected note into the editor pane
// with cursor positioned at the end of the content. Used for new notes.
func (p *Plugin) loadNoteIntoEditorAtEnd() tea.Cmd {
	note := p.getSelectedNote()
	if note == nil {
		return p.abandonEditor()
	}

	if p.editorDirty {
		targetID := note.ID
		return p.saveBefore(func() tea.Cmd {
			p.moveCursorToNote(targetID)
			return p.loadNoteIntoEditorAtEnd()
		})
	}

	p.rememberCurrentPlace()
	p.editorNote = note
	p.editorTextarea.SetValue(note.Content)
	p.previewLines = strings.Split(note.Content, "\n")
	if len(p.previewLines) == 0 {
		p.previewLines = []string{""}
	}
	p.invalidateViewSurface()
	p.editorDirty = false
	p.lastSavedContent = note.Content
	p.previewMode = false
	p.updateTextareaDimensions()
	p.editorTextarea.MoveToEnd()
	p.previewCursorLine = p.editorTextarea.Line()
	p.trackTextareaScroll()
	p.editorTextarea.Focus()
	p.clearEditSelection()
	return nil
}

func (p *Plugin) revertCursorToEditorNote() {
	if p.editorNote == nil {
		return
	}
	for i, n := range p.getDisplayNotes() {
		if n.ID == p.editorNote.ID {
			p.cursor = i
			return
		}
	}
}

func (p *Plugin) moveCursorToNote(id string) {
	for i, n := range p.getDisplayNotes() {
		if n.ID == id {
			p.cursor = i
			return
		}
	}
}

// abandonEditor persists a dirty buffer then clears the editor surface.
// On persist failure the dirty buffer is left in place.
func (p *Plugin) abandonEditor() tea.Cmd {
	if p.editorDirty {
		return p.saveBefore(func() tea.Cmd { return p.abandonEditor() })
	}
	if p.editorNote != nil {
		p.rememberCurrentPlace()
	}
	p.editorNote = nil
	p.previewLines = nil
	p.editorDirty = false
	return nil
}

// startAutoSaveTimer starts a 1-second debounce timer for auto-save.
func (p *Plugin) startAutoSaveTimer() tea.Cmd {
	p.autoSaveID++
	id := p.autoSaveID
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return AutoSaveTickMsg{ID: id}
	})
}

// saveEditorContent saves the editor content back to the note.
func (p *Plugin) saveEditorContent() tea.Cmd {
	if p.editorNote == nil || p.store == nil || (!p.editorDirty && !p.needsEditorCheckpoint()) {
		return p.runPendingAfterSave()
	}
	if p.pendingCreateID(p.editorNote.ID) {
		p.saveQueued = true
		return nil
	}
	if p.saveInFlight {
		p.saveQueued = true
		return nil
	}

	content := p.editorTextarea.Value()
	noteID := p.editorNote.ID
	generation := p.autoSaveID
	var epoch uint64
	if p.ctx != nil {
		epoch = p.ctx.Epoch
	}
	store := p.store
	projectRoot := ""
	if p.ctx != nil {
		projectRoot = p.ctx.ProjectRoot
	}
	p.saveRequestID++
	requestID := p.saveRequestID
	activation := p.saveActivation
	p.saveInFlight = true
	p.activeSaveID = noteID
	p.activeSaveContent = content
	p.saveErr = nil
	startedAt := time.Now().UnixNano()
	writeSequence := p.nextWriteSequence(noteID)

	return func() tea.Msg {
		p.saveMu.Lock()
		defer p.saveMu.Unlock()
		note, err, skipped := p.persistOrderedLocked(store, projectRoot, noteID, content, startedAt, writeSequence)
		p.lastWriteRequest = requestID
		p.lastWriteErr = err
		p.lastWriteSkipped = skipped
		return NoteContentSavedMsg{
			ID:             noteID,
			Err:            err,
			Epoch:          epoch,
			Generation:     generation,
			SaveActivation: activation,
			RequestID:      requestID,
			Content:        content,
			Note:           note,
			Skipped:        skipped,
			WriteSequence:  writeSequence,
		}
	}
}

// saveBefore remembers the latest buffer-replacing action and starts (or joins)
// an asynchronous save. Bubble Tea remains responsive while td is slow. The
// action runs only after the exact current buffer is durable.
func (p *Plugin) saveBefore(after func() tea.Cmd) tea.Cmd {
	p.pendingAfterSave = after
	if !p.editorDirty && !p.needsEditorCheckpoint() {
		return p.runPendingAfterSave()
	}
	return p.saveEditorContent()
}

func (p *Plugin) runPendingAfterSave() tea.Cmd {
	if p.editorDirty || p.needsEditorCheckpoint() || p.saveInFlight || p.pendingAfterSave == nil {
		return nil
	}
	after := p.pendingAfterSave
	p.pendingAfterSave = nil
	return after()
}

// openInExternalEditor opens the current note in $EDITOR. This is the one notes
// path that still leaves Sidecar; the in-pane editor is on e.
func (p *Plugin) openInExternalEditor() tea.Cmd {
	note := p.getSelectedNote()
	if note == nil || p.store == nil {
		return nil
	}
	if blocked, ok := p.guardPendingCreateDurableAction(note.ID); ok {
		return blocked
	}
	if p.editorDirty {
		noteID := note.ID
		return p.saveBefore(func() tea.Cmd {
			p.moveCursorToNote(noteID)
			return p.openInExternalEditor()
		})
	}
	noteID := note.ID
	store := p.store
	epoch := p.ctx.Epoch
	p.externalPrepareID++
	requestID := p.externalPrepareID
	return func() tea.Msg {
		notePath := store.NotePath(noteID)
		if notePath == "" {
			return ExternalEditorPreparedMsg{
				ID: noteID, Epoch: epoch, RequestID: requestID, Err: errors.New("note file unavailable"),
			}
		}
		return ExternalEditorPreparedMsg{ID: noteID, Path: notePath, Epoch: epoch, RequestID: requestID}
	}
}

// readBackInlineEdit reads the temp file content after $EDITOR exits and
// updates the note. Activation stays zero: no inline session owns this write,
// so isStaleNoteSaveResult judges it on epoch alone.
func (p *Plugin) readBackInlineEdit() tea.Cmd {
	noteID := p.pendingInlineEditID
	notePath := p.pendingInlineEditPath

	if noteID == "" || notePath == "" || p.store == nil || p.ctx == nil {
		return p.loadNotes()
	}
	if p.activeExport.Path == notePath {
		return p.retryActiveExport()
	}
	return p.saveRetainedExport(noteID, notePath, 0)
}

// saveRetainedExport persists an external/inline editor file without deleting
// the only copy first. A failed attempt keeps the path and Ctrl-S retries it;
// only the owning successful completion removes the export.
func (p *Plugin) saveRetainedExport(noteID, notePath string, _ uint64) tea.Cmd {
	if noteID == "" || notePath == "" || p.store == nil || p.ctx == nil {
		return nil
	}
	p.exportSaveRequestID++
	intent := retainedExport{
		ID: noteID, Path: notePath, RequestID: p.exportSaveRequestID,
		Epoch: p.ctx.Epoch, ProjectRoot: p.ctx.ProjectRoot, Store: p.store,
		StartedAt: time.Now().UnixNano(), Sequence: p.nextWriteSequence(noteID),
	}
	if p.pendingInlineEditPath == notePath {
		p.pendingInlineEditID = ""
		p.pendingInlineEditPath = ""
	}
	p.pendingEditorSyncID = noteID
	if p.activeExport.Path != "" {
		p.exportQueue = append(p.exportQueue, intent)
		if p.exportSaveInFlight {
			return nil
		}
		return p.retryActiveExport()
	}
	return p.startRetainedExport(intent)
}

func (p *Plugin) startRetainedExport(intent retainedExport) tea.Cmd {
	p.activeExport = intent
	p.exportSaveInFlight = true
	p.saveErr = nil
	return func() tea.Msg {
		content, err := os.ReadFile(intent.Path)
		if err != nil {
			return NoteContentSavedMsg{ID: intent.ID, Err: err, Epoch: intent.Epoch, External: true, ExportPath: intent.Path, ExportRequestID: intent.RequestID, WriteSequence: intent.Sequence}
		}
		note, err, skipped := p.persistOrdered(intent.Store, intent.ProjectRoot, intent.ID, string(content), intent.StartedAt, intent.Sequence)
		return NoteContentSavedMsg{ID: intent.ID, Err: err, Epoch: intent.Epoch, Content: string(content), Note: note, External: true, ExportPath: intent.Path, ExportRequestID: intent.RequestID, WriteSequence: intent.Sequence, Skipped: skipped}
	}
}

func (p *Plugin) retryActiveExport() tea.Cmd {
	if p.activeExport.Path == "" || p.exportSaveInFlight || p.ctx == nil || p.store == nil {
		return nil
	}
	// A retry is another attempt to persist the same content intent, not a new
	// edit. If a newer intent exists, do not promote this older file above it.
	// Keep it recoverable until the newer intent is durable, or remove it now
	// when a canonical acknowledgment has already superseded it.
	if !p.writeIsLatest(p.activeExport.ID, p.activeExport.Sequence) ||
		p.writeIsDurablySuperseded(p.activeExport.ID, p.activeExport.Sequence) {
		finished := p.activeExport
		p.activeExport = retainedExport{}
		p.saveErr = nil
		if p.pendingInlineEditPath == finished.Path {
			p.pendingInlineEditID = ""
			p.pendingInlineEditPath = ""
		}
		if p.writeIsDurablySuperseded(finished.ID, finished.Sequence) {
			removeNoteExport(finished.Path)
		} else {
			p.supersededExports = append(p.supersededExports, finished)
		}
		return p.startNextRetainedExport()
	}
	intent := p.activeExport
	p.exportSaveRequestID++
	intent.RequestID = p.exportSaveRequestID
	intent.Epoch = p.ctx.Epoch
	intent.ProjectRoot = p.ctx.ProjectRoot
	intent.Store = p.store
	intent.StartedAt = time.Now().UnixNano()
	return p.startRetainedExport(intent)
}

func (p *Plugin) startNextRetainedExport() tea.Cmd {
	if len(p.exportQueue) == 0 {
		return nil
	}
	next := p.exportQueue[0]
	p.exportQueue = p.exportQueue[1:]
	return p.startRetainedExport(next)
}

func (p *Plugin) hasQueuedExportForNote(noteID string) bool {
	for _, export := range p.exportQueue {
		if export.ID == noteID {
			return true
		}
	}
	return false
}

func (p *Plugin) cleanupSupersededExports(noteID string, throughSequence uint64) {
	kept := p.supersededExports[:0]
	for _, export := range p.supersededExports {
		if export.ID == noteID && export.Sequence <= throughSequence {
			removeNoteExport(export.Path)
			continue
		}
		kept = append(kept, export)
	}
	p.supersededExports = kept
}

func (p *Plugin) exportPathOwned(path string) bool {
	if path == "" {
		return false
	}
	if p.activeExport.Path == path {
		return true
	}
	for _, exports := range [][]retainedExport{p.exportQueue, p.supersededExports} {
		for _, export := range exports {
			if export.Path == path {
				return true
			}
		}
	}
	return false
}

func (p *Plugin) removeExportIfUnowned(path string) {
	if !p.exportPathOwned(path) {
		removeNoteExport(path)
	}
}

// searchQuery is the note list search bar's text.
func (p *Plugin) searchQuery() string { return p.searchField.Query() }

// handleSearchKey processes keyboard input in search mode.
//
// The list bar answers its own keys first — esc leaves, enter is NV's
// select-or-create, and ctrl+n/ctrl+p and the arrows walk the results — and
// everything else is the shared query field's, which is where cursor movement,
// word ops, home, end and paste come from.
func (p *Plugin) handleSearchKey(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	// searchMode is what means "this bar owns the keyboard", so the field's
	// focus is derived from it rather than tracked twice.
	p.searchField.Focus()
	key := msg.String()

	switch key {
	case "esc":
		// Exit search mode, clear query, show all notes
		p.searchMode = false
		p.searchField.Reset()
		p.filteredNotes = nil
		p.cursor = 0
		p.scrollOff = 0
		return p, nil

	case "enter":
		// NV behavior: if exact match exists, select it; otherwise create new note
		if p.searchQuery() != "" {
			exactMatch := FindExactTitleMatch(p.notes, p.searchQuery())
			if exactMatch != nil {
				// Select the exact match and open in editor
				for i, n := range p.notes {
					if n.ID == exactMatch.ID {
						p.cursor = i
						break
					}
				}
				p.searchMode = false
				p.searchField.Reset()
				p.filteredNotes = nil
				p.scrollOff = 0
				if cmd := p.loadNoteIntoEditor(); cmd != nil {
					return p, cmd
				}
				p.activePane = PaneEditor
				if p.viewFilter != FilterActive {
					p.previewMode = true
				} else {
					return p, p.enterEditAtPreviewPlace()
				}
				if p.ctx != nil && p.ctx.Logger != nil {
					p.ctx.Logger.Debug("notes: exact match selected", "id", exactMatch.ID)
				}
			} else if len(p.filteredNotes) > 0 {
				note := p.getSelectedNote()
				// cursor indexes the filtered list; clearing the filter below
				// makes it index p.notes instead. Re-anchor it on the note the
				// user actually picked or Enter opens whatever sits at the same
				// offset in the unfiltered list.
				if note != nil {
					for i, n := range p.notes {
						if n.ID == note.ID {
							p.cursor = i
							break
						}
					}
				}
				p.searchMode = false
				p.searchField.Reset()
				p.filteredNotes = nil
				p.scrollOff = 0
				if cmd := p.loadNoteIntoEditor(); cmd != nil {
					return p, cmd
				}
				p.activePane = PaneEditor
				if p.viewFilter != FilterActive {
					p.previewMode = true
				} else if note != nil {
					if p.ctx != nil && p.ctx.Logger != nil {
						p.ctx.Logger.Debug("notes: filtered match selected", "id", note.ID)
					}
					return p, p.enterEditAtPreviewPlace()
				}
				if note != nil && p.ctx != nil && p.ctx.Logger != nil {
					p.ctx.Logger.Debug("notes: filtered match selected", "id", note.ID)
				}
			} else {
				// No matches - create new note with query as title
				title := p.searchQuery()
				// Clear search state before creating
				p.searchMode = false
				p.searchField.Reset()
				p.filteredNotes = nil
				return p, p.createNoteWithTitle(title)
			}
		}
		// Exit search mode and clear query after selection
		p.searchMode = false
		p.searchField.Reset()
		p.filteredNotes = nil
		p.scrollOff = 0
		return p, nil

	case "ctrl+n", "down":
		// Navigate down in results
		notesList := p.getDisplayNotes()
		if p.cursor < len(notesList)-1 {
			p.cursor++
			p.ensureCursorVisibleForList(p.height-2, len(notesList))
		}
		return p, nil

	case "ctrl+p", "up":
		// Navigate up in results
		notesList := p.getDisplayNotes()
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisibleForList(p.height-2, len(notesList))
		}
		return p, nil

	default:
		// Everything else is the field's: text, the caret keys, word ops, home
		// and end. ctrl+n/ctrl+p and the arrows above still walk the result
		// list, which is what the field leaves unclaimed anyway.
		before := p.searchQuery()
		p.searchField.HandleKey(msg)
		if p.searchQuery() != before {
			p.updateFilteredNotes()
		}
		return p, nil
	}
}

// updateFilteredNotes updates the filtered notes list based on current query.
func (p *Plugin) updateFilteredNotes() {
	p.filteredNotes = FilterNotes(p.notes, p.searchQuery())
	// Reset cursor to 0 (or clamp if needed)
	p.cursor = 0
	p.scrollOff = 0

	// NV behavior: if exact match exists, select it automatically
	if p.searchQuery() != "" {
		for i, match := range p.filteredNotes {
			if ExactTitleMatch(p.searchQuery(), match.Note) {
				p.cursor = i
				break
			}
		}
	}
}

// getDisplayNotes returns the notes to display (filtered or all).
func (p *Plugin) getDisplayNotes() []Note {
	if p.searchQuery() != "" && len(p.filteredNotes) > 0 {
		notes := make([]Note, len(p.filteredNotes))
		for i, m := range p.filteredNotes {
			notes[i] = m.Note
		}
		return notes
	}
	return p.notes
}

// getSelectedNote returns the currently selected note from display list.
func (p *Plugin) getSelectedNote() *Note {
	notesList := p.getDisplayNotes()
	if len(notesList) == 0 || p.cursor < 0 || p.cursor >= len(notesList) {
		return nil
	}
	return &notesList[p.cursor]
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	if p.remoteBound() {
		content := styles.Title.Render(pluginName) + "\n\n" + styles.Muted.Render(plugin.FormatRemoteUnavailable(pluginName, p.ctx.HostID))
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	if p.showSetupModal {
		p.ensureSetupModal()
		content := p.renderSetupModal()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	// Info modal takes precedence
	if p.showInfoModal {
		p.ensureInfoModal()
		content := p.renderInfoModal()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	// Delete modal takes precedence
	if p.showDeleteModal {
		p.ensureDeleteModal()
		content := p.renderDeleteModal()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	// Task modal takes precedence
	if p.showTaskModal {
		p.ensureTaskModal()
		content := p.renderTaskModal()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	content := p.renderView()

	// Constrain output to allocated height
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

// selectedNote returns the currently selected note, or nil if none.
func (p *Plugin) selectedNote() *Note {
	return p.getSelectedNote()
}

// createNote returns a command that creates a new note.
func (p *Plugin) createNote() tea.Cmd {
	return p.createNoteWithTitle("")
}

// createNoteWithTitle returns a command that creates a new note with the given title.
// The title becomes the first line of the note content.
func (p *Plugin) createNoteWithTitle(title string) tea.Cmd {
	if p.editorDirty {
		return p.saveBefore(func() tea.Cmd { return p.createNoteWithTitle(title) })
	}
	if p.store == nil {
		return nil
	}
	// Use title as initial content (first line) so cursor can be positioned after it
	return p.beginOptimisticCreate(title, title)
}

// togglePin returns a command that toggles the pinned state of the selected note.
func (p *Plugin) togglePin() tea.Cmd {
	note := p.selectedNote()
	if note == nil || p.store == nil {
		return nil
	}
	if blocked, ok := p.guardPendingCreateDurableAction(note.ID); ok {
		return blocked
	}
	if p.editorDirty {
		noteID := note.ID
		return p.saveBefore(func() tea.Cmd {
			p.moveCursorToNote(noteID)
			return p.togglePin()
		})
	}
	noteID := note.ID
	epoch := p.ctx.Epoch
	store := p.store

	return func() tea.Msg {
		err := store.TogglePin(noteID)
		return NotePinToggledMsg{ID: noteID, Err: err, Epoch: epoch}
	}
}

// toggleArchive returns a command that toggles the archived state of the selected note.
func (p *Plugin) toggleArchive() tea.Cmd {
	note := p.selectedNote()
	if note == nil || p.store == nil {
		return nil
	}
	if blocked, ok := p.guardPendingCreateDurableAction(note.ID); ok {
		return blocked
	}
	if p.editorDirty {
		noteID := note.ID
		return p.saveBefore(func() tea.Cmd {
			p.moveCursorToNote(noteID)
			return p.toggleArchive()
		})
	}

	return p.beginOptimisticArchive(*note)
}

// yankNoteContent copies the note content to the system clipboard.
func (p *Plugin) yankNoteContent() tea.Cmd {
	note := p.selectedNote()
	if note == nil {
		return nil
	}

	content := note.Content
	if p.editorNote != nil && p.editorNote.ID == note.ID && p.editorDirty {
		content = p.editorTextarea.Value()
	}
	return clip.Copy(content, func(r clip.Result) tea.Msg {
		return msg.FlashMsg{Text: r.Message("Copied note content")}
	})
}

// yankNoteTitle copies the note title (first line) to the system clipboard.
func (p *Plugin) yankNoteTitle() tea.Cmd {
	note := p.selectedNote()
	if note == nil {
		return nil
	}

	title := note.Title
	if title == "" {
		// Use first line of content if no title
		lines := strings.SplitN(note.Content, "\n", 2)
		if len(lines) > 0 {
			title = strings.TrimSpace(lines[0])
		}
	}

	if title == "" {
		// Nothing to act on, nothing to say (audit row 43).
		return nil
	}

	return clip.Copy(title, func(r clip.Result) tea.Msg {
		return msg.FlashMsg{Text: r.Message("Copied: " + title)}
	})
}

// yankNoteID copies the stable td note identity. It is reachable only from
// notes-list and the read-only notes-preview context; edit contexts keep their
// own Ctrl-Y semantics.
func (p *Plugin) yankNoteID() tea.Cmd {
	note := p.selectedNote()
	if note == nil || note.ID == "" {
		return nil
	}
	return clip.Copy(note.ID, func(r clip.Result) tea.Msg {
		return msg.ToastMsg{Message: r.Message("Copied note ID: " + note.ID), Duration: 2 * time.Second}
	})
}

// copyEditorContent copies the current editor content to clipboard.
func (p *Plugin) copyEditorContent() tea.Cmd {
	content := p.editorTextarea.Value()
	if content == "" {
		// Nothing to act on, nothing to say (audit row 44).
		return nil
	}

	return clip.Copy(content, func(r clip.Result) tea.Msg {
		return msg.FlashMsg{Text: r.Message("Copied to clipboard")}
	})
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) {
	p.focused = f
	if p.edit.Model != nil {
		p.edit.Model.SetFocused(f)
	}
}

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	if p.remoteBound() {
		return nil
	}
	// Info modal commands
	if p.showInfoModal {
		return []plugin.Command{
			{ID: "close", Name: "Close", Description: "Close info modal", Category: plugin.CategoryActions, Context: "notes-info", Priority: 1},
		}
	}
	// Delete modal commands
	if p.showDeleteModal {
		return []plugin.Command{
			{ID: "delete-confirm", Name: "Delete", Description: "Confirm delete", Category: plugin.CategoryActions, Context: "notes-delete-modal", Priority: 1},
			{ID: "cancel", Name: "Cancel", Description: "Cancel delete", Category: plugin.CategoryActions, Context: "notes-delete-modal", Priority: 2},
		}
	}
	// Task modal commands
	if p.showTaskModal {
		return []plugin.Command{
			{ID: "create-task", Name: "Create", Description: "Create task from note", Category: plugin.CategoryActions, Context: "notes-task-modal", Priority: 1},
			{ID: "cancel", Name: "Cancel", Description: "Cancel task creation", Category: plugin.CategoryActions, Context: "notes-task-modal", Priority: 2},
		}
	}
	if p.noteSearchMode {
		cmds := []plugin.Command{
			{ID: "search-cancel", Name: "Cancel", Description: "Exit in-note search", Category: plugin.CategoryActions, Context: "notes-note-search", Priority: 1},
		}
		if p.noteSearchCommitted {
			cmds = append(cmds,
				plugin.Command{ID: "next-match", Name: "Next", Description: "Next match", Category: plugin.CategoryNavigation, Context: "notes-note-search", Priority: 2},
				plugin.Command{ID: "prev-match", Name: "Prev", Description: "Previous match", Category: plugin.CategoryNavigation, Context: "notes-note-search", Priority: 3},
			)
		} else {
			cmds = append(cmds, plugin.Command{ID: "search-confirm", Name: "Find", Description: "Commit search", Category: plugin.CategoryActions, Context: "notes-note-search", Priority: 2})
		}
		return cmds
	}
	if p.searchMode {
		cmds := []plugin.Command{
			{ID: "search-confirm", Name: "Select", Description: "Select note or create new", Category: plugin.CategoryActions, Context: "notes-search", Priority: 1},
			{ID: "search-cancel", Name: "Cancel", Description: "Exit search", Category: plugin.CategoryActions, Context: "notes-search", Priority: 2},
		}
		if p.editorDirty || p.saveErr != nil {
			cmds = append(cmds, plugin.Command{ID: "save", Name: "Retry", Description: "Retry saving note", Category: plugin.CategoryActions, Context: "notes-search", Priority: 1})
		}
		return cmds
	}
	if p.edit.Active {
		return nil
	}
	if p.activePane == PaneEditor && p.editorNote != nil {
		if p.previewMode {
			cmds := []plugin.Command{
				{ID: "search-in-note", Name: "Find", Description: "Search in this note", Category: plugin.CategorySearch, Context: "notes-preview", Priority: 1},
				{ID: "switch-pane", Name: "List", Description: "Switch to list pane", Category: plugin.CategoryNavigation, Context: "notes-preview", Priority: 2},
			}
			// Archived and deleted notes are read-only, and all three edit keys
			// return nil there. Advertising them promises what the key will not do.
			if p.viewFilter == FilterActive {
				cmds = append(cmds,
					plugin.Command{ID: "open-note", Name: "Open", Description: "Open with the default Notes editor", Category: plugin.CategoryActions, Context: "notes-preview", Priority: 1},
					plugin.Command{ID: "edit-note", Name: "Built-in", Description: "Edit in the built-in editor", Category: plugin.CategoryActions, Context: "notes-preview", Priority: 2},
					plugin.Command{ID: "vim-edit", Name: "Pane", Description: "Edit with $EDITOR in the right pane", Category: plugin.CategoryActions, Context: "notes-preview", Priority: 3},
					plugin.Command{ID: "external-editor", Name: "Ext", Description: "Open in external $EDITOR", Category: plugin.CategoryActions, Context: "notes-preview", Priority: 4},
				)
			}
			cmds = append(cmds, plugin.Command{ID: "yank-id", Name: "ID", Description: "Copy note ID", Category: plugin.CategoryActions, Context: "notes-preview", Priority: 6})
			if p.editorDirty || p.saveErr != nil {
				cmds = append(cmds, plugin.Command{ID: "save", Name: "Retry", Description: "Retry saving note", Category: plugin.CategoryActions, Context: "notes-preview", Priority: 1})
			}
			renderName := "Raw"
			if !p.markdownView {
				renderName = "Render"
			}
			cmds = append(cmds, plugin.Command{
				ID:          "toggle-markdown",
				Name:        renderName,
				Description: "Toggle rendered/raw markdown",
				Category:    plugin.CategoryView,
				Context:     "notes-preview",
				Priority:    5,
				Handler: func() tea.Cmd {
					p.toggleMarkdownView()
					return nil
				},
			})
			return cmds
		}
		cmds := []plugin.Command{
			{ID: "switch-pane", Name: "List", Description: "Switch to list pane", Category: plugin.CategoryNavigation, Context: "notes-editor", Priority: 1},
			{ID: "save", Name: "Save", Description: "Save note", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 2},
		}
		if p.editorDirty {
			cmds[1].Name = "Save*"
		}
		cmds = append(cmds,
			plugin.Command{ID: "copy-note", Name: "Copy", Description: "Copy selection or note", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 4},
			plugin.Command{ID: "paste-recent", Name: "Paste", Description: "Paste last session copy", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 5},
			plugin.Command{ID: "select-all", Name: "All", Description: "Select all (Alt-A; Cmd-A when delivered)", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 6},
			plugin.Command{ID: "select-toggle", Name: "Select", Description: "Set or clear the selection anchor", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 7},
			plugin.Command{ID: "note-start", Name: "Start", Description: "Move to note start (Cmd-Up when delivered)", Category: plugin.CategoryNavigation, Context: "notes-editor", Priority: 8},
			plugin.Command{ID: "note-end", Name: "End", Description: "Move to note end (Cmd-Down when delivered)", Category: plugin.CategoryNavigation, Context: "notes-editor", Priority: 9},
			plugin.Command{ID: "select-note-start", Name: "SelStart", Description: "Select to note start (Shift-Cmd-Up when delivered)", Category: plugin.CategoryNavigation, Context: "notes-editor", Priority: 10},
			plugin.Command{ID: "select-note-end", Name: "SelEnd", Description: "Select to note end (Shift-Cmd-Down when delivered)", Category: plugin.CategoryNavigation, Context: "notes-editor", Priority: 11},
		)
		if p.hasEditSelection() {
			cmds = append(cmds, plugin.Command{ID: "cut", Name: "Cut", Description: "Cut selection", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 3})
		}
		if p.historyForCurrent().canUndo() {
			cmds = append(cmds, plugin.Command{
				ID: "undo-edit", Name: "Undo", Description: "Undo last edit", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 3,
				Handler: func() tea.Cmd { _, cmd := p.undoEditorEdit(); return cmd },
			})
		}
		if p.historyForCurrent().canRedo() {
			cmds = append(cmds, plugin.Command{
				ID: "redo-edit", Name: "Redo", Description: "Redo last undone edit", Category: plugin.CategoryActions, Context: "notes-editor", Priority: 4,
				Handler: func() tea.Cmd { _, cmd := p.redoEditorEdit(); return cmd },
			})
		}
		return cmds
	}
	// Build commands based on current filter view
	cmds := []plugin.Command{
		{ID: "search", Name: "Search", Description: "Search notes", Category: plugin.CategorySearch, Context: "notes-list", Priority: 1},
	}
	if p.editorDirty || p.saveErr != nil {
		cmds = append(cmds, plugin.Command{ID: "save", Name: "Retry", Description: "Retry saving note", Category: plugin.CategoryActions, Context: "notes-list", Priority: 1})
	}

	// Keep both a/x filter bindings advertised in every list state. The shortcut
	// matching the current state is labeled Active because pressing it toggles
	// back; the other still switches directly to its state.
	archiveName := "Archived"
	deleteName := "Deleted"
	if p.viewFilter == FilterArchived {
		archiveName = "Active"
	}
	if p.viewFilter == FilterDeleted {
		deleteName = "Active"
	}
	cmds = append(cmds,
		plugin.Command{ID: "show-archived", Name: archiveName, Description: "Toggle archived notes", Category: plugin.CategoryNavigation, Context: "notes-list", Priority: 2},
		plugin.Command{ID: "show-deleted", Name: deleteName, Description: "Toggle deleted notes", Category: plugin.CategoryNavigation, Context: "notes-list", Priority: 3},
	)
	if p.viewFilter != FilterActive {
		// Add "Back" command when in Archived or Deleted view
		cmds = append(cmds,
			plugin.Command{ID: "back-to-active", Name: "Active", Description: "Return to active notes", Category: plugin.CategoryNavigation, Context: "notes-list", Priority: 0},
		)
	}

	if p.viewFilter == FilterActive {
		// Full editing commands only in Active view
		cmds = append(cmds,
			plugin.Command{ID: "new-note", Name: "New", Description: "Create new note", Category: plugin.CategoryActions, Context: "notes-list", Priority: 4},
			plugin.Command{ID: "open-note", Name: "Open", Description: "Open with the default Notes editor", Category: plugin.CategoryActions, Context: "notes-list", Priority: 5},
			plugin.Command{ID: "edit-note", Name: "Built-in", Description: "Edit in the built-in editor", Category: plugin.CategoryActions, Context: "notes-list", Priority: 6},
			plugin.Command{ID: "vim-edit", Name: "Pane", Description: "Edit with $EDITOR in the right pane", Category: plugin.CategoryActions, Context: "notes-list", Priority: 7},
			plugin.Command{ID: "external-editor", Name: "Ext", Description: "Open in external $EDITOR", Category: plugin.CategoryActions, Context: "notes-list", Priority: 8},
			plugin.Command{ID: "delete-note", Name: "Delete", Description: "Delete selected note", Category: plugin.CategoryActions, Context: "notes-list", Priority: 8},
			plugin.Command{ID: "toggle-pin", Name: "Pin", Description: "Toggle pin on note", Category: plugin.CategoryActions, Context: "notes-list", Priority: 9},
			plugin.Command{ID: "archive-note", Name: "Archive", Description: "Archive selected note", Category: plugin.CategoryActions, Context: "notes-list", Priority: 10},
			plugin.Command{ID: "to-task", Name: "Task", Description: "Convert to task", Category: plugin.CategoryActions, Context: "notes-list", Priority: 11},
			plugin.Command{ID: "show-info", Name: "Info", Description: "Show note info", Category: plugin.CategoryActions, Context: "notes-list", Priority: 12},
		)
		// Show Undo command when undo is available
		if p.hasUndo() {
			cmds = append(cmds,
				plugin.Command{ID: "undo", Name: "Undo", Description: "Undo last delete/archive", Category: plugin.CategoryActions, Context: "notes-list", Priority: 0},
			)
		}
	} else {
		// Read-only view - only preview available
		cmds = append(cmds,
			plugin.Command{ID: "preview-note", Name: "View", Description: "Preview selected note", Category: plugin.CategoryActions, Context: "notes-list", Priority: 4},
			plugin.Command{ID: "show-info", Name: "Info", Description: "Show note info", Category: plugin.CategoryActions, Context: "notes-list", Priority: 5},
		)
	}

	// Yank commands available in all views
	cmds = append(cmds,
		plugin.Command{ID: "yank-content", Name: "Yank", Description: "Copy note content", Category: plugin.CategoryActions, Context: "notes-list", Priority: 13},
		plugin.Command{ID: "yank-title", Name: "YankTitle", Description: "Copy note title", Category: plugin.CategoryActions, Context: "notes-list", Priority: 14},
		plugin.Command{ID: "yank-id", Name: "ID", Description: "Copy note ID", Category: plugin.CategoryActions, Context: "notes-list", Priority: 15},
		plugin.Command{ID: "refresh", Name: "Refresh", Description: "Reload notes", Category: plugin.CategoryActions, Context: "notes-list", Priority: 16},
	)

	return cmds
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.remoteBound() {
		return "notes"
	}
	if p.showSetupModal {
		return "notes-setup-modal"
	}
	if p.showInfoModal {
		return "notes-info"
	}
	if p.showDeleteModal {
		return "notes-delete-modal"
	}
	if p.showTaskModal {
		return "notes-task-modal"
	}
	if p.edit.Active {
		return "notes-inline-edit"
	}
	if p.noteSearchMode {
		return "notes-note-search"
	}
	if p.searchMode {
		return "notes-search"
	}
	if p.activePane == PaneEditor && p.editorNote != nil {
		if p.previewMode {
			return "notes-preview"
		}
		return "notes-editor"
	}
	return "notes-list"
}

// ConsumesTextInput reports whether notes currently has an active text-entry
// surface and should receive printable keys directly.
func (p *Plugin) ConsumesTextInput() bool {
	if p.searchMode || p.noteSearchMode || p.showTaskModal || p.edit.Active {
		return true
	}
	return p.activePane == PaneEditor && p.editorNote != nil && !p.previewMode
}

// BlocksGlobalKeys reports whether a plugin-owned modal has keyboard focus.
func (p *Plugin) BlocksGlobalKeys() bool {
	return p.showSetupModal || p.showInfoModal || p.showDeleteModal || p.showTaskModal
}

// loadNotes returns a command that loads notes from the store.
func (p *Plugin) loadNotes() tea.Cmd {
	if p.store == nil {
		return nil
	}
	// Only show loading screen on initial load; background refreshes
	// (auto-save, pin, archive, etc.) keep the current view visible.
	if p.notes == nil {
		p.loading = true
	}
	epoch := p.ctx.Epoch
	filter := p.viewFilter
	store := p.store
	projectRoot := p.ctx.ProjectRoot
	p.loadRequestID++
	requestID := p.loadRequestID

	return func() tea.Msg {
		if checker, ok := store.(interface{ SetupStatus() error }); ok {
			if err := checker.SetupStatus(); err != nil {
				return NotesLoadedMsg{
					Err:       err,
					Epoch:     epoch,
					RequestID: requestID,
					Filter:    filter,
				}
			}
		}
		recoveryErr := recoverNoteDrafts(projectRoot, store)
		var notes []Note
		var err error

		switch filter {
		case FilterArchived:
			notes, err = store.ListArchived()
		case FilterDeleted:
			notes, err = store.ListDeleted()
		default:
			notes, err = store.List(false)
		}

		return NotesLoadedMsg{
			Notes:       notes,
			Err:         err,
			RecoveryErr: recoveryErr,
			Epoch:       epoch,
			RequestID:   requestID,
			Filter:      filter,
		}
	}
}

// showSavedToast shows a toast notification for note save.
func showSavedToast() tea.Cmd {
	// Routine, high-frequency, and the editor already shows a clean buffer
	// (audit row 45).
	return msg.ShowFlash("Saved")
}

func showSaveFailedToast(err error) tea.Cmd {
	text := "Save failed"
	if err != nil {
		text = "Save failed: " + err.Error()
	}
	return func() tea.Msg {
		return msg.ToastMsg{
			Message:  text,
			Duration: 4 * time.Second,
			IsError:  true,
		}
	}
}

// switchViewFilter persists a dirty buffer, then swaps the list filter.
func (p *Plugin) switchViewFilter(filter NoteFilter) tea.Cmd {
	if p.editorDirty {
		return p.saveBefore(func() tea.Cmd { return p.switchViewFilter(filter) })
	}
	_ = p.abandonEditor()
	p.viewFilter = filter
	p.cursor = 0
	p.scrollOff = 0
	return p.loadNotes()
}

func toggleNoteFilter(current, target NoteFilter) NoteFilter {
	if current == target {
		return FilterActive
	}
	return target
}

func nextNoteFilter(current NoteFilter) NoteFilter {
	switch current {
	case FilterActive:
		return FilterArchived
	case FilterArchived:
		return FilterDeleted
	default:
		return FilterActive
	}
}

// showRestoredToast shows a toast notification for undo/restore.
func showRestoredToast(title string) tea.Cmd {
	displayTitle := truncateTitle(title, 30)
	text := "Restored"
	if displayTitle != "" {
		text = "Restored: " + displayTitle
	}
	return msg.ShowFlash(text)
}

// truncateTitle truncates a title to maxLen chars with ellipsis.
func truncateTitle(title string, maxLen int) string {
	if len(title) <= maxLen {
		return title
	}
	if maxLen <= 3 {
		return title[:maxLen]
	}
	return title[:maxLen-3] + "..."
}

// pushUndo adds an action to the undo stack.
func (p *Plugin) pushUndo(action UndoAction) {
	const maxUndoStack = 20
	p.undoStack = append(p.undoStack, action)
	if len(p.undoStack) > maxUndoStack {
		p.undoStack = p.undoStack[1:]
	}
}

// popUndo removes and returns the last action from the undo stack.
func (p *Plugin) popUndo() *UndoAction {
	if len(p.undoStack) == 0 {
		return nil
	}
	action := p.undoStack[len(p.undoStack)-1]
	p.undoStack = p.undoStack[:len(p.undoStack)-1]
	return &action
}

// hasUndo returns true if there are actions in the undo stack.
func (p *Plugin) hasUndo() bool {
	return len(p.undoStack) > 0
}

// undoLastAction undoes the last delete or archive action.
func (p *Plugin) undoLastAction() tea.Cmd {
	if p.store == nil || !p.hasUndo() {
		// Nothing to undo is nothing to report (audit row 47).
		return nil
	}
	latest := p.undoStack[len(p.undoStack)-1]
	if blocked, ok := p.guardPendingCreateDurableAction(latest.NoteID); ok {
		return blocked
	}
	action := p.popUndo()
	if action == nil {
		return nil
	}
	p.undoErr = nil

	noteID := action.NoteID
	title := action.Title
	actionType := action.Type
	epoch := p.ctx.Epoch
	store := p.store

	return func() tea.Msg {
		var err error
		switch actionType {
		case UndoDelete:
			err = store.Restore(noteID)
		case UndoArchive:
			err = store.Unarchive(noteID)
		}
		return NoteRestoredMsg{
			ID:     noteID,
			Title:  title,
			Err:    err,
			Epoch:  epoch,
			Action: *action,
		}
	}
}

func showRestoreFailedToast(err error) tea.Cmd {
	text := "Undo failed"
	if err != nil {
		text += ": " + err.Error()
	}
	return func() tea.Msg {
		return msg.ToastMsg{Message: text, Duration: 4 * time.Second, IsError: true}
	}
}

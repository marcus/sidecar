package workspacediff

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/livewatch"
	sharedscroll "github.com/marcus/sidecar/internal/scroll"
	"github.com/marcus/sidecar/internal/textselect"
)

// View is the reusable Diff pane model: one snapshot, one cursor, one commit
// detail. The project plugin wraps it; the global preview holds one for the
// selected worktree.
type View struct {
	Snapshot *Snapshot
	State    LoadState
	Error    string
	Scope    Scope

	Content string
	Raw     string
	Files   []File
	Commits []CommitInfo

	Cursor      int
	Scroll      int
	DiffScroll  int
	HorizScroll int
	Focus       Focus
	ViewMode    ViewMode

	CommitDetail      *CommitDetail
	CommitFileCursor  int
	CommitFileScroll  int
	CommitFileDiffRaw string

	// CommitDetailErr is why the commit under the cursor has no file list.
	// A load that fails must say so: a nil CommitDetail with nothing recorded
	// is indistinguishable from a load still in flight, and the pane sat on
	// "Loading commit files…" forever.
	CommitDetailErr string
	// CommitFileDiffLoaded reports that a commit-file patch load has landed,
	// empty or not. CommitFileDiffErr is why it failed. Together they separate
	// "still loading" from "loaded and there is nothing to show".
	CommitFileDiffLoaded bool
	CommitFileDiffErr    string

	Target      Target
	Epoch       uint64
	Binding     uint64
	WorkspaceID string
	WorkDir     string
	Revision    string

	// Loader issues git operations. Nil uses local git in this process.
	Loader Loader

	width     int
	height    int
	listWidth int

	// Text selection over the frame this pane last drew. originX/originY are
	// where the host drew it, frameRows/frameW/frameH are what it drew, and
	// selectionKey is what that frame was of, so a frame showing something
	// else drops the selection. See select.go.
	selection        textselect.Surface
	selectionKey     string
	frameRows        []string
	frameW, frameH   int
	originX, originY int

	// live sequences in-place re-runs of the diff driven by repository movement,
	// and holds the fingerprint that keeps an unchanged re-run off the screen.
	// See live.go.
	live livewatch.Refresher

	// Host paint/load hooks. workspacediff cannot import gitstatus; the
	// project plugin fills these so CycleViewMode, n/N, and paging work.
	LoadFullFile     func() tea.Cmd
	JumpChange       func(scroll int, prev bool) int
	PaintedLineCount func() int
	LeavingFullFile  func(scroll int) int
	ClearPaintedFile func()

	// HasFilePicker is set by a host that answers "f" with a file picker. The
	// global preview does not, and must not advertise a key nothing answers.
	HasFilePicker bool
}

// Bind records the host identity used to drop stale async results.
func (v *View) Bind(workdir, workspaceID string, epoch uint64) {
	v.BindGeneration(workdir, workspaceID, epoch, 0)
}

// BindGeneration adds a per-view request identity. Legacy hosts use zero;
// shared decks use their never-reused tab ID so raw broadcasts cannot cross a
// context rebind that deliberately reuses workspace and epoch.
func (v *View) BindGeneration(workdir, workspaceID string, epoch, binding uint64) {
	if workdir != "" {
		v.WorkDir = workdir
	}
	if workspaceID != "" {
		v.WorkspaceID = workspaceID
	}
	v.Epoch = epoch
	v.Binding = binding
	if v.Target.Identity() == "" {
		v.Target = WorkingTreeTarget()
	}
}

// SetSize records the allocated leaf box and reclamps scroll.
// It must not persist a clamped listWidth: hosts call this from View()
// every frame, and a shrink must not forget the user-dragged width.
func (v *View) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.ClampScroll()
}

// Width and Height are the last SetSize allocation.
func (v *View) Width() int  { return v.width }
func (v *View) Height() int { return v.height }

func (v *View) accepts(epoch, binding uint64, workspaceID, identity string) bool {
	if v.Binding != 0 && binding != v.Binding {
		return false
	}
	if workspaceID != "" && v.WorkspaceID != "" && workspaceID != v.WorkspaceID {
		return false
	}
	if epoch != 0 && v.Epoch != 0 && epoch != v.Epoch {
		return false
	}
	if identity != "" && v.Target.Identity() != "" && identity != v.Target.Identity() {
		return false
	}
	return true
}

// ApplySnapshot rebuilds the working-tree / commits lists from the snapshot
// and clamps the cursor. It does not load commit detail; callers that just
// applied a snapshot should also call LoadSelectedCommit.
func (v *View) ApplySnapshot() {
	v.Content, v.Raw = "", ""
	v.Files = nil
	v.Commits = nil
	if v.Snapshot == nil {
		return
	}
	switch v.Scope {
	case ScopeCommits:
		v.Commits = append([]CommitInfo(nil), v.Snapshot.Commits...)
	case ScopeAggregate:
		// Aggregate is rendered as two labelled raw sections.
	default:
		v.Content, v.Raw = v.Snapshot.WorkingTree, v.Snapshot.WorkingTree
		if len(v.Snapshot.Files) > 0 {
			v.Files = append([]File(nil), v.Snapshot.Files...)
		} else {
			v.Files = ParseFiles(v.Raw)
		}
		v.Commits = append([]CommitInfo(nil), v.Snapshot.Commits...)
	}
	v.ClampScroll()
}

// FileCount is the number of working-tree files in the current scope.
func (v *View) FileCount() int { return len(v.Files) }

// TotalItems is files + commits, the navigable left-pane length.
func (v *View) TotalItems() int { return v.FileCount() + len(v.Commits) }

// ClampCursor keeps the cursor inside the current item list.
func (v *View) ClampCursor() {
	total := v.TotalItems()
	if total == 0 {
		v.Cursor = 0
		v.Scroll = 0
		return
	}
	if v.Cursor >= total {
		v.Cursor = total - 1
	}
	if v.Cursor < 0 {
		v.Cursor = 0
	}
}

// SelectedCommit is the commit under the cursor, if any.
func (v *View) SelectedCommit() (CommitInfo, bool) {
	idx := v.Cursor - v.FileCount()
	if idx < 0 || idx >= len(v.Commits) {
		return CommitInfo{}, false
	}
	return v.Commits[idx], true
}

// LoadSelectedCommit loads the commit under the cursor. Snapshot/scope
// populate can leave the cursor on a commit without a move, so this does not
// require a cursor-change event. Skip if that commit is already loaded.
func (v *View) LoadSelectedCommit(workdir, workspaceID string) tea.Cmd {
	commit, ok := v.SelectedCommit()
	if !ok {
		return nil
	}
	if CommitDetailMatchesListHash(v.CommitDetail, commit.Hash) {
		return nil
	}
	v.resetCommitDetail()
	return v.loadCommit(workdir, workspaceID, commit.Hash)
}

// resetCommitDetail drops the loaded commit and everything derived from it,
// including the reasons a previous load failed. Every path that forgets a
// commit goes through here so a stale error cannot outlive its commit.
func (v *View) resetCommitDetail() {
	v.CommitDetail = nil
	v.CommitDetailErr = ""
	v.CommitFileCursor = 0
	v.CommitFileScroll = 0
	v.clearCommitFileDiff()
}

// LoadCommit fetches one commit's file list, tagged for stale-drop.
func (v *View) LoadCommit(hash string) tea.Cmd {
	return v.loadCommit(v.WorkDir, v.WorkspaceID, hash)
}

func (v *View) loadCommit(workdir, workspaceID, hash string) tea.Cmd {
	if workdir != "" {
		v.WorkDir = workdir
	}
	if workspaceID != "" {
		v.WorkspaceID = workspaceID
	}
	epoch, binding, id, ident := v.Epoch, v.Binding, v.WorkspaceID, v.Target.Identity()
	wd := v.WorkDir
	loader := v.git()
	return func() tea.Msg {
		result, err := loader.LoadCommitDetail(context.Background(), wd, hash, "")
		return CommitDetailMsg{
			Epoch: epoch, Binding: binding, WorkspaceID: id, Identity: ident,
			Hash: hash, Commit: result.Commit, Err: err, Revision: result.Revision, NotModified: result.NotModified,
		}
	}
}

// CommitDetailMsg is the result of LoadSelectedCommit.
type CommitDetailMsg struct {
	Epoch       uint64
	Binding     uint64
	WorkspaceID string
	Identity    string
	Hash        string
	Commit      *CommitDetail
	Err         error
	Revision    string
	NotModified bool
}

// ApplyCommitDetail installs a loaded commit if it is still the row under
// the cursor, or the root of a TargetCommit tab whose Identity matches.
func (v *View) ApplyCommitDetail(msg CommitDetailMsg) tea.Cmd {
	if !v.accepts(msg.Epoch, msg.Binding, msg.WorkspaceID, msg.Identity) {
		return nil
	}
	if v.Target.Kind == TargetCommit {
		return v.applyCommitRoot(msg)
	}
	if msg.Err != nil || msg.Commit == nil {
		// A failure is still an answer. Recording it against the row under the
		// cursor is what lets the preview say why there are no files instead of
		// claiming the load is still running.
		if commit, ok := v.SelectedCommit(); ok && (msg.Hash == "" || strings.HasPrefix(commit.Hash, msg.Hash) || strings.HasPrefix(msg.Hash, commit.Hash)) {
			v.CommitDetail = nil
			v.CommitDetailErr = commitLoadFailure(msg.Err)
		}
		return nil
	}
	commit, ok := v.SelectedCommit()
	if !ok || !CommitDetailMatchesListHash(msg.Commit, commit.Hash) {
		return nil
	}
	v.CommitDetailErr = ""
	preserve := v.CommitDetail != nil && CommitDetailMatchesListHash(v.CommitDetail, commit.Hash)
	v.CommitDetail = msg.Commit
	if !preserve {
		v.CommitFileCursor = 0
		v.CommitFileScroll = 0
		v.CommitFileDiffRaw = ""
	}
	v.ClampScroll()
	if v.Focus == FocusCommitFiles || v.Focus == FocusCommitDiff {
		return v.LoadSelectedCommitFile()
	}
	return nil
}

func (v *View) applyCommitRoot(msg CommitDetailMsg) tea.Cmd {
	if msg.Err != nil || msg.Commit == nil {
		v.CommitDetail = nil
		v.State = LoadStateError
		if msg.Err != nil {
			v.Error = msg.Err.Error()
		} else {
			v.Error = "commit not found"
		}
		return nil
	}
	preserve := v.CommitDetail != nil && CommitDetailMatchesListHash(v.CommitDetail, msg.Commit.Hash)
	v.CommitDetail = msg.Commit
	v.CommitDetailErr = ""
	v.Snapshot = nil
	v.Commits = nil
	v.Files = nil
	v.Content, v.Raw = "", ""
	v.Error = ""
	v.State = LoadStateReady
	v.Focus = FocusCommitFiles
	if !preserve {
		v.CommitFileCursor = 0
		v.CommitFileScroll = 0
		v.CommitFileDiffRaw = ""
	}
	v.ClampScroll()
	return v.LoadSelectedCommitFile()
}

// SnapshotMsg is a completed snapshot load for one worktree.
type SnapshotMsg struct {
	Epoch       uint64
	Binding     uint64
	WorkspaceID string
	Identity    string
	Snapshot    *Snapshot
	Err         error
	Command     string
	BaseRef     string

	// Refresh marks a snapshot produced by View.Refresh rather than an explicit
	// load: it preserves the selected file and scroll offset, and is discarded
	// when the repository state came back unchanged. See live.go.
	Refresh     bool
	Revision    string
	NotModified bool
}

// LoadSnapshotCmd loads a snapshot for workdir and tags it with workspaceID.
func LoadSnapshotCmd(workdir, baseRef, workspaceID string) tea.Cmd {
	return LoadSnapshotCmdAt(workdir, baseRef, workspaceID, 0, IdentityWorkingTree)
}

// LoadSnapshotCmdAt is LoadSnapshotCmd with epoch and target identity.
func LoadSnapshotCmdAt(workdir, baseRef, workspaceID string, epoch uint64, identity string) tea.Cmd {
	return LoadSnapshotCmdBound(workdir, baseRef, workspaceID, epoch, identity, 0)
}

// LoadSnapshotCmdBound is LoadSnapshotCmdAt with a per-view request identity.
func LoadSnapshotCmdBound(workdir, baseRef, workspaceID string, epoch uint64, identity string, binding uint64) tea.Cmd {
	return loadSnapshotCmdBound(nil, workdir, baseRef, workspaceID, epoch, identity, binding, false, "")
}

// LoadSnapshotCmd is this view's working-tree load, using Loader when set.
func (v *View) LoadSnapshotCmd(baseRef string, refresh bool) tea.Cmd {
	ifRevision := ""
	if refresh {
		ifRevision = v.Revision
	}
	return loadSnapshotCmdBound(v.git(), v.WorkDir, baseRef, v.WorkspaceID, v.Epoch, v.Target.Identity(), v.Binding, refresh, ifRevision)
}

func loadSnapshotCmdBound(loader Loader, workdir, baseRef, workspaceID string, epoch uint64, identity string, binding uint64, refresh bool, ifRevision string) tea.Cmd {
	if identity == "" {
		identity = IdentityWorkingTree
	}
	if loader == nil {
		loader = localLoader{}
	}
	return func() tea.Msg {
		result, err := loader.LoadSnapshot(context.Background(), workdir, baseRef, ifRevision)
		if err != nil {
			return SnapshotMsg{Epoch: epoch, Binding: binding, WorkspaceID: workspaceID, Identity: identity, Err: err,
				Command: "git diff HEAD / git log <base>..HEAD / git diff <merge-base>..HEAD", BaseRef: baseRef,
				Refresh: refresh, Revision: result.Revision}
		}
		return SnapshotMsg{
			Epoch: epoch, Binding: binding, WorkspaceID: workspaceID, Identity: identity,
			Snapshot: result.Snapshot, BaseRef: baseRef, Refresh: refresh,
			Revision: result.Revision, NotModified: result.NotModified,
		}
	}
}

// ApplySnapshotMsg installs a loaded snapshot or records the error, dropping stale msgs.
func (v *View) ApplySnapshotMsg(msg SnapshotMsg, workdir, workspaceID string) tea.Cmd {
	if v.Target.Kind != TargetWorkingTree {
		return nil
	}
	if !v.accepts(msg.Epoch, msg.Binding, msg.WorkspaceID, msg.Identity) {
		return nil
	}
	if msg.NotModified {
		if msg.Revision != "" {
			v.Revision = msg.Revision
		}
		if msg.Refresh {
			_, _ = v.applyRefresh(msg, workdir, workspaceID)
		}
		return nil
	}
	if msg.Refresh {
		cmd, _ := v.applyRefresh(msg, workdir, workspaceID)
		return cmd
	}
	if msg.Err != nil {
		v.Snapshot = nil
		v.State = LoadStateError
		v.Error = msg.Err.Error()
		v.Content, v.Raw = "", ""
		v.Files, v.Commits = nil, nil
		return nil
	}
	if msg.Revision != "" {
		v.Revision = msg.Revision
	}
	return v.ApplyLoadedSnapshot(msg.Snapshot, workdir, workspaceID)
}

// ApplyLoadedSnapshot installs a snapshot, applies the default working-tree
// scope, and loads the commit under the cursor if that is the current item.
func (v *View) ApplyLoadedSnapshot(snapshot *Snapshot, workdir, workspaceID string) tea.Cmd {
	v.Snapshot = snapshot
	v.Error = ""
	v.State = LoadStateClean
	if snapshot != nil {
		v.State = snapshot.State
	}
	// An explicit load defines what is on screen, so the refresh gate measures
	// from here rather than reporting the first watcher signal as a change.
	v.live.Reset()
	v.live.Adopt(fingerprintSnapshot(snapshot))
	v.ApplySnapshot()
	return tea.Batch(v.LoadSelectedCommit(workdir, workspaceID), v.LoadSelectedWorkingTreeFile())
}

// RangeMsg is the result of LoadRange for one A..B / A...B tab.
type RangeMsg struct {
	Epoch       uint64
	Binding     uint64
	WorkspaceID string
	Identity    string
	Raw         string
	Files       []File
	Err         error
	Revision    string
	NotModified bool
}

// LoadRange fetches git diff --binary A..B or A...B for this tab.
func (v *View) LoadRange() tea.Cmd {
	if v.Target.Kind != TargetRange || v.Target.A == "" || v.Target.B == "" {
		return nil
	}
	return loadRangeCmdBound(v.git(), v.WorkDir, v.Target, v.Epoch, v.WorkspaceID, v.Binding)
}

// LoadRangeCmd runs one git diff for a range target.
func LoadRangeCmd(workdir string, t Target, epoch uint64, workspaceID string) tea.Cmd {
	return loadRangeCmdBound(localLoader{}, workdir, t, epoch, workspaceID, 0)
}

func loadRangeCmdBound(loader Loader, workdir string, t Target, epoch uint64, workspaceID string, binding uint64) tea.Cmd {
	if t.Kind != TargetRange || t.A == "" || t.B == "" {
		return nil
	}
	if loader == nil {
		loader = localLoader{}
	}
	ident := t.Identity()
	return func() tea.Msg {
		result, err := loader.LoadRange(context.Background(), workdir, t, "")
		if err != nil {
			return RangeMsg{Epoch: epoch, Binding: binding, WorkspaceID: workspaceID, Identity: ident, Err: err}
		}
		return RangeMsg{Epoch: epoch, Binding: binding, WorkspaceID: workspaceID, Identity: ident, Raw: result.Raw, Files: result.Files, Revision: result.Revision, NotModified: result.NotModified}
	}
}

// LoadRangeDiff runs git diff --binary for a range target.
func LoadRangeDiff(ctx context.Context, workdir string, t Target) (string, error) {
	if t.Kind != TargetRange || t.A == "" || t.B == "" {
		return "", errors.New("not a range target")
	}
	dots := t.Dots
	if dots != "..." {
		dots = ".."
	}
	cmd := exec.CommandContext(ctx, "git", "diff", "--binary", t.A+dots+t.B)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", err
		}
	}
	return string(out), nil
}

// ApplyRangeMsg installs a range patch when Identity matches this r: tab.
func (v *View) ApplyRangeMsg(msg RangeMsg) tea.Cmd {
	if v.Target.Kind != TargetRange {
		return nil
	}
	if !v.accepts(msg.Epoch, msg.Binding, msg.WorkspaceID, msg.Identity) {
		return nil
	}
	if msg.NotModified {
		if msg.Revision != "" {
			v.Revision = msg.Revision
		}
		return nil
	}
	if msg.Revision != "" {
		v.Revision = msg.Revision
	}
	if msg.Err != nil {
		v.State = LoadStateError
		v.Error = msg.Err.Error()
		v.Files = nil
		v.Commits = nil
		v.Snapshot = nil
		v.CommitDetail = nil
		v.Content, v.Raw = "", ""
		return nil
	}
	v.Error = ""
	v.State = LoadStateReady
	v.Snapshot = nil
	v.Commits = nil
	v.CommitDetail = nil
	v.Raw = msg.Raw
	v.Content = msg.Raw
	if msg.Files != nil {
		v.Files = msg.Files
	} else {
		v.Files = ParseFiles(msg.Raw)
	}
	v.Focus = FocusFileList
	v.ClampScroll()
	return nil
}

// CycleScope walks working-tree → commits → aggregate. No-op on commit/range targets.
func (v *View) CycleScope() tea.Cmd {
	if v.Target.Kind != TargetWorkingTree {
		return nil
	}
	v.Scope = (v.Scope + 1) % 3
	v.Cursor, v.Scroll, v.DiffScroll, v.HorizScroll = 0, 0, 0, 0
	v.Focus = FocusFileList
	if v.Scope == ScopeAggregate {
		v.Focus = FocusDiff
	}
	v.CommitDetail = nil
	v.clearCommitFileDiff()
	v.dropPaintedFile()
	v.ApplySnapshot()
	cmd := v.LoadSelectedCommit(v.WorkDir, v.WorkspaceID)
	if v.ViewMode == ViewFullFile && v.LoadFullFile != nil {
		return tea.Batch(cmd, v.LoadFullFile())
	}
	return cmd
}

// CycleViewMode walks unified → side-by-side → full-file.
// Entering full-file returns the host LoadFullFile cmd.
func (v *View) CycleViewMode() tea.Cmd {
	switch v.ViewMode {
	case ViewUnified:
		v.ViewMode = ViewSideBySide
	case ViewSideBySide:
		v.ViewMode = ViewFullFile
		v.HorizScroll = 0
		v.ClampScroll()
		if v.LoadFullFile != nil {
			return v.LoadFullFile()
		}
		return nil
	default:
		if v.LeavingFullFile != nil && v.DiffScroll > 0 {
			v.DiffScroll = v.LeavingFullFile(v.DiffScroll)
		}
		if v.ClearPaintedFile != nil {
			v.ClearPaintedFile()
		}
		v.ViewMode = ViewUnified
	}
	v.HorizScroll = 0
	v.ClampScroll()
	return nil
}

// JumpFile moves to the next or previous file in this tab's list.
func (v *View) JumpFile(delta int) tea.Cmd {
	if v.Focus == FocusCommitDiff || v.Focus == FocusCommitFiles {
		if v.CommitDetail == nil {
			return nil
		}
		n := len(v.CommitDetail.Files)
		next := v.CommitFileCursor + delta
		if next < 0 || next >= n {
			return nil
		}
		v.CommitFileCursor = next
		v.DiffScroll, v.HorizScroll = 0, 0
		v.clearCommitFileDiff()
		if v.ClearPaintedFile != nil {
			v.ClearPaintedFile()
		}
		v.ClampScroll()
		load := v.LoadSelectedCommitFile()
		if v.ViewMode == ViewFullFile && v.LoadFullFile != nil {
			return tea.Batch(load, v.LoadFullFile())
		}
		return load
	}
	n := v.FileCount()
	if n <= 1 {
		return nil
	}
	next := v.Cursor + delta
	if next < 0 || next >= n {
		return nil
	}
	old := v.Cursor
	v.Cursor = next
	v.DiffScroll, v.HorizScroll = 0, 0
	v.ClampScroll()
	return v.OnCursorChanged(old)
}

// OnCursorChanged resets the right pane after a file-list move.
func (v *View) OnCursorChanged(oldCursor int) tea.Cmd {
	if v.Cursor == oldCursor {
		return nil
	}
	v.DiffScroll = 0
	v.HorizScroll = 0
	if v.ClearPaintedFile != nil {
		v.ClearPaintedFile()
	}
	v.ClampScroll()
	if v.Cursor < v.FileCount() {
		v.CommitDetail = nil
		load := v.LoadSelectedWorkingTreeFile()
		if v.ViewMode == ViewFullFile && v.LoadFullFile != nil {
			return tea.Batch(load, v.LoadFullFile())
		}
		return load
	}
	return v.LoadSelectedCommit(v.WorkDir, v.WorkspaceID)
}

func (v *View) selectedFileName() string {
	if v.Cursor >= 0 && v.Cursor < len(v.Files) {
		return v.Files[v.Cursor].Path
	}
	return ""
}

func (v *View) selectedFileRaw() string {
	if v.Cursor >= 0 && v.Cursor < len(v.Files) {
		return v.Files[v.Cursor].Raw
	}
	return ""
}

type fileRow struct {
	Path      string
	Additions int
	Deletions int
}

func (v *View) fileRows() []fileRow {
	rows := make([]fileRow, len(v.Files))
	for i, f := range v.Files {
		rows[i] = fileRow{Path: f.Path, Additions: f.Additions, Deletions: f.Deletions}
	}
	return rows
}

// SelectedFileName is the working-tree path under the cursor, if any.
func (v *View) SelectedFileName() string { return v.selectedFileName() }

// FileNames is the working-tree list for the host file picker.
func (v *View) FileNames() []string {
	names := make([]string, len(v.Files))
	for i, f := range v.Files {
		names[i] = f.Path
	}
	return names
}

// CommitFileDiffMsg is a completed commit-file patch load.
type CommitFileDiffMsg struct {
	Epoch       uint64
	Binding     uint64
	WorkspaceID string
	Identity    string
	CommitHash  string
	FilePath    string
	Raw         string
	Err         error
}

// ApplyCommitFileDiff installs a commit file patch if the cursor still matches.
func (v *View) ApplyCommitFileDiff(msg CommitFileDiffMsg) tea.Cmd {
	if !v.accepts(msg.Epoch, msg.Binding, msg.WorkspaceID, msg.Identity) {
		return nil
	}
	if v.CommitDetail == nil || v.CommitDetail.Hash != msg.CommitHash {
		return nil
	}
	if v.CommitFileCursor < 0 || v.CommitFileCursor >= len(v.CommitDetail.Files) {
		return nil
	}
	if v.CommitDetail.Files[v.CommitFileCursor].Path != msg.FilePath {
		return nil
	}
	// The load has landed either way. Marking it landed is what stops the pane
	// sitting on "Loading diff…" when git returned an error or an empty patch.
	v.CommitFileDiffLoaded = true
	if msg.Err != nil {
		v.CommitFileDiffErr = msg.Err.Error()
		v.CommitFileDiffRaw = ""
		return nil
	}
	v.CommitFileDiffErr = ""
	v.CommitFileDiffRaw = msg.Raw
	return nil
}

// commitLoadFailure is the one-line reason a commit could not be read.
func commitLoadFailure(err error) string {
	if err == nil {
		return "commit not found"
	}
	return err.Error()
}

// LoadSelectedCommitFile loads the patch for the commit file under the cursor.
func (v *View) LoadSelectedCommitFile() tea.Cmd {
	if v.CommitDetail == nil || v.CommitFileCursor < 0 || v.CommitFileCursor >= len(v.CommitDetail.Files) {
		return nil
	}
	file := v.CommitDetail.Files[v.CommitFileCursor]
	parentHash := ""
	if v.CommitDetail.IsMerge && len(v.CommitDetail.ParentHashes) > 0 {
		parentHash = v.CommitDetail.ParentHashes[0]
	}
	hash := v.CommitDetail.Hash
	workdir, epoch, binding, id, ident := v.WorkDir, v.Epoch, v.Binding, v.WorkspaceID, v.Target.Identity()
	loader := v.git()
	return func() tea.Msg {
		result, err := loader.LoadCommitFile(context.Background(), workdir, hash, file.Path, parentHash, "")
		raw := result.Raw
		return CommitFileDiffMsg{
			Epoch: epoch, Binding: binding, WorkspaceID: id, Identity: ident,
			CommitHash: hash, FilePath: file.Path, Raw: raw, Err: err,
		}
	}
}

// LoadCommitFileDiff loads one path's patch from a commit, diffing against
// parentHash for merges so combined diffs do not come back empty.
func LoadCommitFileDiff(ctx context.Context, workdir, hash, path, parentHash string) (string, error) {
	args := []string{"show", hash, "--", path}
	if parentHash != "" {
		args = []string{"diff", parentHash, hash, "--", path}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// WorkingTreeFileMsg is a cursor-driven working-tree file patch load.
type WorkingTreeFileMsg struct {
	Epoch       uint64
	Binding     uint64
	WorkspaceID string
	Identity    string
	Path        string
	Raw         string
	Err         error
}

// LoadSelectedWorkingTreeFile loads a working-tree file whose patch was omitted.
func (v *View) LoadSelectedWorkingTreeFile() tea.Cmd {
	if v.Target.Kind != TargetWorkingTree || v.Loader == nil {
		return nil
	}
	if v.Cursor < 0 || v.Cursor >= len(v.Files) {
		return nil
	}
	file := v.Files[v.Cursor]
	if file.Path == "" || file.Raw != "" {
		return nil
	}
	workdir, epoch, binding, id, ident := v.WorkDir, v.Epoch, v.Binding, v.WorkspaceID, v.Target.Identity()
	loader := v.git()
	path := file.Path
	return func() tea.Msg {
		result, err := loader.LoadWorkingTreeFile(context.Background(), workdir, path, "")
		return WorkingTreeFileMsg{
			Epoch: epoch, Binding: binding, WorkspaceID: id, Identity: ident,
			Path: path, Raw: result.Raw, Err: err,
		}
	}
}

// ApplyWorkingTreeFile installs a working-tree file patch if the cursor still matches.
func (v *View) ApplyWorkingTreeFile(msg WorkingTreeFileMsg) tea.Cmd {
	if !v.accepts(msg.Epoch, msg.Binding, msg.WorkspaceID, msg.Identity) {
		return nil
	}
	if msg.Err != nil || msg.Path == "" {
		return nil
	}
	for i := range v.Files {
		if v.Files[i].Path == msg.Path {
			v.Files[i].Raw = msg.Raw
			adds, dels := countDiffStats(msg.Raw)
			v.Files[i].Additions = adds
			v.Files[i].Deletions = dels
			break
		}
	}
	return nil
}

// ContentMaxScroll returns the exact vertical bound for the content currently
// rendered in the right pane (or collapsed view).
func (v *View) ContentMaxScroll(height int) int {
	content := v.Content
	visible := height
	switch {
	case v.Scope == ScopeAggregate:
		content = v.aggregateText()
	case len(v.Files) > 0 || len(v.Commits) > 0:
		if v.Cursor < 0 || v.Cursor >= len(v.Files) {
			return 0 // commit preview is not scrollable
		}
		content = v.Files[v.Cursor].Raw
		visible = max(1, height-2) // filename + spacer
	}
	return max(len(splitLines(content))-visible, 0)
}

// ScrollAtBoundary reports whether delta points farther past the rendered
// content boundary.
func (v *View) ScrollAtBoundary(delta, height int) bool {
	return (sharedscroll.Bounds{Position: v.DiffScroll, Maximum: v.ContentMaxScroll(height)}).AtBoundary(delta)
}

// ScrollContent moves the visible right-pane (or collapsed) content.
func (v *View) ScrollContent(delta, height int) {
	v.DiffScroll, _ = (sharedscroll.Bounds{
		Position: v.DiffScroll,
		Maximum:  v.ContentMaxScroll(height),
	}).Move(delta)
}

// ParseFiles splits a unified multi-file diff into named file entries.
func ParseFiles(raw string) []File {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	chunks := splitFileDiffs(raw)
	var files []File
	for _, chunk := range chunks {
		path := filePathFromDiff(chunk)
		if path == "" {
			continue
		}
		adds, dels := countDiffStats(chunk)
		files = append(files, File{Path: path, Raw: chunk, Additions: adds, Deletions: dels})
	}
	return files
}

func splitFileDiffs(diff string) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func filePathFromDiff(chunk string) string {
	for _, line := range strings.Split(chunk, "\n") {
		rest, ok := strings.CutPrefix(line, "diff --git ")
		if !ok {
			continue
		}
		a, b, found := strings.Cut(rest, " b/")
		if !found {
			return strings.TrimPrefix(rest, "a/")
		}
		_ = a
		return b
	}
	return ""
}

func countDiffStats(chunk string) (adds, dels int) {
	for _, line := range strings.Split(chunk, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			adds++
		case strings.HasPrefix(line, "-"):
			dels++
		}
	}
	return adds, dels
}

// LoadCommitDetail fetches %H/%h/parents/subject and the commit's numstat files.
//
// The parent list is not decoration: git show on a merge is a combined diff, and
// a combined diff for one path is empty for every path that was not a conflict
// resolution. Without the parents, LoadSelectedCommitFile has nothing to diff
// against and every file in a merge renders as an empty patch.
func LoadCommitDetail(ctx context.Context, workdir, hash string) (*CommitDetail, error) {
	cmd := exec.CommandContext(ctx, "git", "show", "--format=%H%n%h%n%P%n%s", "-s", hash)
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) < 4 {
		return nil, nil
	}
	parents := strings.Fields(lines[2])
	detail := &CommitDetail{
		Hash:         strings.TrimSpace(lines[0]),
		ShortHash:    strings.TrimSpace(lines[1]),
		Subject:      strings.TrimSpace(lines[3]),
		ParentHashes: parents,
		IsMerge:      len(parents) > 1,
	}
	stat := exec.CommandContext(ctx, "git", "show", "--numstat", "--format=", hash)
	stat.Dir = workdir
	statOut, _ := stat.Output()
	for _, line := range strings.Split(strings.TrimSpace(string(statOut)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		add, _ := strconv.Atoi(fields[0])
		del, _ := strconv.Atoi(fields[1])
		path := fields[len(fields)-1]
		status := "M"
		if fields[0] == "0" && fields[1] != "0" {
			status = "D"
		} else if fields[1] == "0" && fields[0] != "0" {
			status = "A"
		}
		detail.Files = append(detail.Files, CommitFile{Path: path, Status: status, Additions: add, Deletions: del})
	}
	return detail, nil
}

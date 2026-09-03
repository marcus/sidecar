package filebrowser

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/tty"
)

type TabOpenMode int

const (
	TabOpenReplace TabOpenMode = iota
	TabOpenNew
	TabOpenPreview
)

type FileTab struct {
	Path      string
	Scroll    int
	Loaded    bool
	Result    PreviewResult
	IsPreview bool // Ephemeral preview tab, replaced on next j/k navigation

	// Edit state (persisted when switching away from inline editor)
	EditSession   string    // Tmux session name (empty if not in edit mode)
	EditOrigMtime time.Time // Original file mtime when editing started
	EditEditor    string    // Editor command used (vim, nano, etc.)
}

type tabHit struct {
	Index  int
	X      int
	Width  int
	CloseX int
	CloseW int
}

// previewTabHit is the payload a tab-row click carries: Index selects, Close
// closes that same tab. The HitMap registers the close cells after the pill
// so they win the overlap.
type previewTabHit struct {
	Index int
	Close bool
}

func fileTabItem(tab FileTab) tabs.Item[FileTab] {
	return tabs.Item[FileTab]{
		Key:     filepath.Clean(tab.Path),
		Value:   tab,
		Preview: tab.IsPreview,
	}
}

func (p *Plugin) tabGroup() tabs.Group[FileTab] {
	g := tabs.Group[FileTab]{Active: p.activeTab}
	g.Items = make([]tabs.Item[FileTab], len(p.tabs))
	for i, tab := range p.tabs {
		g.Items[i] = fileTabItem(tab)
	}
	return g
}

func (p *Plugin) applyTabGroup(g tabs.Group[FileTab]) {
	p.tabs = make([]FileTab, len(g.Items))
	for i, item := range g.Items {
		tab := item.Value
		tab.IsPreview = item.Preview
		p.tabs[i] = tab
	}
	p.activeTab = g.Active
	p.normalizeActiveTab()
}

func (p *Plugin) findPreviewTab() int {
	for i, item := range p.tabGroup().Items {
		if item.Preview {
			return i
		}
	}
	return -1
}

func (p *Plugin) pinTab(idx int) {
	if idx >= 0 && idx < len(p.tabs) {
		p.tabs[idx].IsPreview = false
	}
}

func (p *Plugin) findTab(path string) int {
	return p.tabGroup().Find(filepath.Clean(path))
}

func (p *Plugin) normalizeActiveTab() {
	if len(p.tabs) == 0 {
		p.activeTab = 0
		return
	}
	if p.activeTab < 0 || p.activeTab >= len(p.tabs) {
		p.activeTab = 0
	}
}

func (p *Plugin) saveActiveTabState() {
	if len(p.tabs) == 0 || p.activeTab < 0 || p.activeTab >= len(p.tabs) {
		return
	}
	p.tabs[p.activeTab].Scroll = p.previewScroll
}

func (p *Plugin) updateActiveTabResult(result PreviewResult) {
	if len(p.tabs) == 0 || p.activeTab < 0 || p.activeTab >= len(p.tabs) {
		return
	}
	tab := &p.tabs[p.activeTab]
	if tab.Path != p.previewFile {
		return
	}
	tab.Result = result
	tab.Loaded = true
}

func (p *Plugin) openTab(path string, mode TabOpenMode) tea.Cmd {
	if path == "" {
		return nil
	}
	// A direct activation supersedes a quiet-period tree preview, including one
	// queued for a selection the user has already left.
	p.treePreviewGen++

	p.normalizeActiveTab()

	// Preview mode: reuse or create a single ephemeral preview tab
	if mode == TabOpenPreview {
		if idx := p.findPreviewTab(); idx >= 0 {
			if filepath.Clean(p.tabs[idx].Path) == filepath.Clean(path) {
				return p.switchTab(idx)
			}
			p.saveActiveTabState()
			p.tabs[idx] = FileTab{Path: path, IsPreview: true}
			p.activeTab = idx
			return p.applyActiveTab()
		}
		p.saveActiveTabState()
		g := p.tabGroup()
		g.AppendItem(tabs.Item[FileTab]{
			Key:     filepath.Clean(path),
			Value:   FileTab{Path: path, IsPreview: true},
			Preview: true,
		})
		p.applyTabGroup(g)
		return p.applyActiveTab()
	}

	// Only deduplicate for non-TabOpenNew; TabOpenNew always creates a new tab
	if mode != TabOpenNew {
		if idx := p.tabGroup().Find(filepath.Clean(path)); idx >= 0 {
			return p.switchTab(idx)
		}
	}

	p.saveActiveTabState()

	if mode == TabOpenReplace && len(p.tabs) > 0 {
		p.tabs[p.activeTab] = FileTab{Path: path}
	} else {
		g := p.tabGroup()
		g.Append(filepath.Clean(path), FileTab{Path: path})
		p.applyTabGroup(g)
	}

	return p.applyActiveTab()
}

func (p *Plugin) openTabAtLine(path string, lineNo int, mode TabOpenMode) tea.Cmd {
	cmd := p.openTab(path, mode)

	if lineNo > 0 {
		p.previewScroll = lineNo - 1
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}
		p.saveActiveTabState()
		if cmd == nil {
			p.clampPreviewScroll()
		}
	}

	return cmd
}

func (p *Plugin) switchTab(index int) tea.Cmd {
	if index < 0 || index >= len(p.tabs) {
		return nil
	}
	if index == p.activeTab {
		return nil
	}

	// Auto-close preview tab when switching to a pinned tab
	if previewIdx := p.findPreviewTab(); previewIdx >= 0 && previewIdx != index {
		p.killTabEditSession(previewIdx)
		g := p.tabGroup()
		g.CloseAt(previewIdx)
		if previewIdx < index {
			index--
		}
		if previewIdx == p.activeTab {
			g.Select(index)
		}
		if index < 0 || index >= len(g.Items) {
			p.applyTabGroup(g)
			return nil
		}
		p.applyTabGroup(g)
	}

	p.saveActiveTabState()
	g := p.tabGroup()
	g.Select(index)
	p.applyTabGroup(g)

	return p.applyActiveTab()
}

func (p *Plugin) cycleTab(delta int) tea.Cmd {
	g := p.tabGroup()
	prev := g.Active
	g.Cycle(delta)
	if g.Active == prev {
		return nil
	}
	return p.switchTab(g.Active)
}

func (p *Plugin) closeTab(index int) tea.Cmd {
	if index < 0 || index >= len(p.tabs) {
		return nil
	}

	// Kill any tmux session associated with this tab
	p.killTabEditSession(index)

	// If closing the active tab that's currently in edit mode, clean up plugin state
	if index == p.activeTab && p.edit.Active {
		p.clearPluginEditState()
	}

	if index == p.activeTab {
		p.saveActiveTabState()
	}

	g := p.tabGroup()
	result := g.CloseAt(index)
	p.applyTabGroup(g)

	if result.Empty {
		p.previewFile = ""
		p.previewScroll = 0
		p.resetPreviewContent()
		p.resetPreviewModes()
		p.updateWatchedFile()
		return nil
	}

	return p.applyActiveTab()
}

func (p *Plugin) applyActiveTab() tea.Cmd {
	if len(p.tabs) == 0 || p.activeTab < 0 || p.activeTab >= len(p.tabs) {
		return nil
	}

	tab := &p.tabs[p.activeTab]
	p.previewFile = tab.Path
	p.previewScroll = tab.Scroll
	p.resetPreviewModes()
	p.resetPreviewContent()
	p.updateWatchedFile()
	p.syncTreeSelection(tab.Path)

	// Check if this tab has a persisted edit session to restore
	if p.restoreEditStateFromTab() {
		// Re-attach to the still-running tmux session
		return p.reattachInlineEditSession()
	}

	if tab.Loaded {
		p.applyPreviewResult(tab.Result)
		p.clampPreviewScroll()
		return nil
	}

	return p.loadPreview(tab.Path)
}

func (p *Plugin) syncTreeSelection(path string) {
	if p.tree == nil || path == "" {
		return
	}

	// Fast path: cursor already points to this file (e.g. click-initiated)
	if node := p.tree.GetNode(p.treeCursor); node != nil && node.Path == path {
		return
	}

	// Try FlatList lookup (no disk I/O, O(n) over visible nodes)
	for i, node := range p.tree.FlatList {
		if node.Path == path {
			p.treeCursor = i
			p.ensureTreeCursorVisible()
			return
		}
	}

	// Fallback: descend the path itself to reach a node in unexpanded
	// directories, reading only the directories along the way.
	targetNode := p.findAndExpandPath(path)
	if targetNode == nil {
		return
	}

	p.expandParents(targetNode)
	p.tree.Flatten()
	p.syncWatcherDirs()

	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}
}

func (p *Plugin) applyPreviewResult(result PreviewResult) {
	// New bytes in the pane invalidate the revision describing the old ones.
	// A remote load re-records it right after this returns; nothing else does,
	// so a locally-cached tab result or an error page never leaves a revision
	// behind that a later read would ask the host about.
	p.forgetPreviewRevision()
	p.selection.Clear()
	p.previewLines = result.Lines
	p.previewHighlighted = result.HighlightedLines
	p.isBinary = result.IsBinary
	p.isTruncated = result.IsTruncated
	p.previewError = result.Error
	p.previewSize = result.TotalSize
	p.previewModTime = result.ModTime
	p.previewMode = result.Mode

	p.isImage = result.IsImage
	p.imageResult = nil
	if p.isImage {
		p.isBinary = false
	}

	p.markdownRendered = nil
	if p.markdownRenderMode && p.isMarkdownFile() {
		p.renderMarkdownContent()
	}
}

func (p *Plugin) clampPreviewScroll() {
	lines := p.getPreviewLines()
	visibleHeight := p.visibleContentHeight()
	maxScroll := len(lines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.previewScroll < 0 {
		p.previewScroll = 0
	} else if p.previewScroll > maxScroll {
		p.previewScroll = maxScroll
	}
	p.saveActiveTabState()
}

func (p *Plugin) resetPreviewModes() {
	p.selection.Clear()
	p.contentSearchMode = false
	p.contentSearchCommitted = false
	p.contentSearchField.Reset()
	p.contentSearchMatches = nil
	p.contentSearchCursor = 0
	p.lineJumpMode = false
	p.lineJumpBuffer = ""
	p.infoMode = false
	p.infoModal = nil
	p.infoModalWidth = 0
	p.blameMode = false
	p.blameState = nil
	p.blameModal = nil
	p.blameModalWidth = 0
	p.markdownRendered = nil
	p.imageResult = nil
}

func (p *Plugin) resetPreviewContent() {
	p.forgetPreviewRevision()
	p.previewLines = nil
	p.previewHighlighted = nil
	p.previewError = nil
	p.isBinary = false
	p.isTruncated = false
	p.previewSize = 0
	p.previewModTime = time.Time{}
	p.previewMode = 0
	p.isImage = false
}

func (p *Plugin) renderPreviewTabs(width int) string {
	p.tabHits = nil

	if len(p.tabs) == 0 || width < 4 {
		return ""
	}

	p.normalizeActiveTab()

	texts := p.tabLabels(width)
	labels := make([]tabs.Label, len(texts))
	for i, text := range texts {
		labels[i] = tabs.Label{Text: text, Preview: p.tabs[i].IsPreview}
	}

	strip := tabs.LayoutStrip(labels, p.activeTab, width, true, fitFilesLabel).HoverClose(p.hoverTabClose.Index0For())
	for _, hit := range strip.Tabs {
		p.tabHits = append(p.tabHits, tabHit{
			Index: hit.Index, X: hit.Col, Width: hit.Width,
			CloseX: hit.CloseCol, CloseW: hit.CloseW,
		})
	}
	return strip.Row
}

func (p *Plugin) registerPreviewTabHits(tabX, tabY int) {
	if p.mouseHandler == nil {
		return
	}
	for _, hit := range p.tabHits {
		p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX+hit.X, tabY, hit.Width, 1, previewTabHit{Index: hit.Index})
	}
	for _, hit := range p.tabHits {
		if hit.CloseW < 1 {
			continue
		}
		p.mouseHandler.HitMap.AddRect(regionPreviewTab, tabX+hit.CloseX, tabY, hit.CloseW, 1, previewTabHit{Index: hit.Index, Close: true})
	}
}

func previewTabPayload(data any) (index int, close, ok bool) {
	switch hit := data.(type) {
	case previewTabHit:
		return hit.Index, hit.Close, true
	case int:
		return hit, false, true
	}
	return 0, false, false
}

func (p *Plugin) clickPreviewTab(data any) tea.Cmd {
	index, close, ok := previewTabPayload(data)
	if !ok {
		return nil
	}
	if close {
		return p.closeTab(index)
	}
	return p.switchTab(index)
}

func fitFilesLabel(text string, _, _, maxWidth int, _ bool) string {
	return truncatePath(text, maxWidth)
}

func (p *Plugin) tabLabels(width int) []string {
	labels := make([]string, 0, len(p.tabs))
	counts := make(map[string]int, len(p.tabs))

	for _, tab := range p.tabs {
		base := filepath.Base(tab.Path)
		counts[base]++
	}

	maxLabelWidth := width / 3
	if maxLabelWidth < 8 {
		maxLabelWidth = 8
	} else if maxLabelWidth > 30 {
		maxLabelWidth = 30
	}

	for _, tab := range p.tabs {
		base := filepath.Base(tab.Path)
		label := base
		if counts[base] > 1 {
			parent := filepath.Base(filepath.Dir(tab.Path))
			label = filepath.Join(parent, base)
		}
		labels = append(labels, truncatePath(label, maxLabelWidth))
	}

	return labels
}

// saveEditStateToTab saves the current plugin-level edit state to the active tab.
// Call this when switching away from a tab that's in inline edit mode.
func (p *Plugin) saveEditStateToTab() {
	if len(p.tabs) == 0 || p.activeTab < 0 || p.activeTab >= len(p.tabs) {
		return
	}
	if !p.edit.Active || p.edit.Name == "" {
		return
	}
	tab := &p.tabs[p.activeTab]
	tab.EditSession = p.edit.Name
	tab.EditOrigMtime = p.inlineEditOrigMtime
	tab.EditEditor = p.edit.EditorCmd
}

// clearPluginEditState clears plugin-level edit state without killing the tmux session.
// Used when detaching from editor (session keeps running in background).
func (p *Plugin) clearPluginEditState() {
	p.edit.Active = false
	p.edit.Name = ""
	p.edit.Path = ""
	p.inlineEditOrigMtime = time.Time{}
	p.edit.EditorCmd = ""
	p.edit.Activation++
	p.edit.Dragging = false
	p.edit.Model.Close()
}

// restoreEditStateFromTab restores plugin-level edit state from the active tab.
// Returns true if the tab has a live edit session to restore.
func (p *Plugin) restoreEditStateFromTab() bool {
	if len(p.tabs) == 0 || p.activeTab < 0 || p.activeTab >= len(p.tabs) {
		return false
	}
	tab := &p.tabs[p.activeTab]
	if tab.EditSession == "" {
		return false
	}
	// Check if session is still alive
	if !(tty.EditorSession{Name: tab.EditSession}).IsAlive() {
		// Session died while away - clear tab edit state
		tab.EditSession = ""
		tab.EditOrigMtime = time.Time{}
		tab.EditEditor = ""
		return false
	}
	// Restore to plugin-level state
	p.edit.Active = true
	p.edit.Name = tab.EditSession
	p.edit.Path = tab.Path
	p.inlineEditOrigMtime = tab.EditOrigMtime
	p.edit.EditorCmd = tab.EditEditor
	return true
}

// killTabEditSession kills the tmux session for a tab if it has one.
func (p *Plugin) killTabEditSession(index int) {
	if index < 0 || index >= len(p.tabs) {
		return
	}
	tab := p.tabs[index]
	killFileTabEditSession(tab)
	tab.EditSession = ""
	tab.EditOrigMtime = time.Time{}
	tab.EditEditor = ""
	p.tabs[index] = tab
}

func killFileTabEditSession(tab FileTab) {
	if tab.EditSession != "" {
		tty.EditorSession{Name: tab.EditSession}.Kill()
	}
}

// closeTabsForPath kills edit sessions and removes tabs matching the deleted
// path. deletedPath may be workdir-relative or absolute (DeleteSuccessMsg).
// A file matches exactly; a directory also removes tabs underneath it.
func (p *Plugin) closeTabsForPath(deletedPath string) tea.Cmd {
	if deletedPath == "" || len(p.tabs) == 0 {
		return nil
	}

	p.saveActiveTabState()

	deleted := p.normalizeDeletedPath(deletedPath)
	g := p.tabGroup()
	result := g.CloseMatching(func(item tabs.Item[FileTab]) bool {
		return tabPathMatchesDeleted(item.Value.Path, deleted)
	})
	if len(result.Removed) == 0 {
		return nil
	}
	for _, item := range result.Removed {
		killFileTabEditSession(item.Value)
	}
	if result.ActiveRemoved && p.edit.Active {
		p.clearPluginEditState()
	}
	p.applyTabGroup(g)

	if result.Empty {
		p.previewFile = ""
		p.previewScroll = 0
		p.resetPreviewContent()
		p.resetPreviewModes()
		p.updateWatchedFile()
		return nil
	}

	// applyActiveTab resets search/blame/info/line-jump. Skip it when the
	// same loaded tab is still active so a sibling delete does not wipe
	// the preview the user is looking at.
	survivor := p.tabs[p.activeTab]
	if !result.ActiveRemoved && p.previewFile == survivor.Path && survivor.Loaded {
		return nil
	}
	return p.applyActiveTab()
}

// normalizeDeletedPath maps a deleted path onto FileTab.Path space
// (workdir-relative). Absolute DeleteSuccessMsg paths become relative when
// they live under WorkDir; already-relative paths are cleaned as-is so tests
// and any other relative caller keep working.
func (p *Plugin) normalizeDeletedPath(deletedPath string) string {
	deletedPath = filepath.Clean(deletedPath)
	if !filepath.IsAbs(deletedPath) {
		return deletedPath
	}
	if p.ctx == nil || p.ctx.WorkDir == "" {
		return deletedPath
	}
	workDir, err := filepath.Abs(p.ctx.WorkDir)
	if err != nil {
		return deletedPath
	}
	absDeleted, err := filepath.Abs(deletedPath)
	if err != nil {
		return deletedPath
	}
	rel, err := filepath.Rel(workDir, absDeleted)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return deletedPath
	}
	return rel
}

// tabPathMatchesDeleted reports whether a workdir-relative tab path is the
// deleted file or lives under the deleted directory. The trailing separator
// keeps "foo" from matching "foobar".
func tabPathMatchesDeleted(tabPath, deletedPath string) bool {
	tabPath = filepath.Clean(tabPath)
	deletedPath = filepath.Clean(deletedPath)
	if tabPath == deletedPath {
		return true
	}
	if deletedPath == "" || deletedPath == "." {
		return false
	}
	return strings.HasPrefix(tabPath, deletedPath+string(filepath.Separator))
}

// invalidateTabsInDirs drops the cached content of background tabs whose file
// lives directly in one of dirs (absolute paths), so switching back to such a
// tab re-reads it from disk instead of showing what was there before.
//
// The active tab is left alone: its content is what the user is looking at, and
// the watcher reloads it through the preview path.
func (p *Plugin) invalidateTabsInDirs(dirs []string) {
	if len(dirs) == 0 || len(p.tabs) == 0 || p.ctx == nil {
		return
	}

	changed := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		changed[filepath.Clean(abs)] = true
	}

	for i := range p.tabs {
		if i == p.activeTab || !p.tabs[i].Loaded {
			continue
		}
		abs, err := filepath.Abs(filepath.Join(p.ctx.WorkDir, p.tabs[i].Path))
		if err != nil {
			continue
		}
		if changed[filepath.Dir(abs)] {
			p.tabs[i].Loaded = false
			p.tabs[i].Result = PreviewResult{}
		}
	}
}

// cleanupAllEditSessions kills all tmux edit sessions for all tabs.
// Called on plugin exit to ensure no orphan tmux sessions remain.
func (p *Plugin) cleanupAllEditSessions() {
	killed := make(map[string]struct{})
	// Clean up current plugin-level edit state
	if p.edit.Active && p.edit.Name != "" {
		killed[p.edit.Name] = struct{}{}
		tty.EditorSession{Name: p.edit.Name, Editor: p.edit.EditorCmd}.Kill()
		p.clearPluginEditState()
	}
	// Clean up any backgrounded sessions in tabs
	for i := range p.tabs {
		if p.tabs[i].EditSession != "" {
			if _, alreadyKilled := killed[p.tabs[i].EditSession]; !alreadyKilled {
				tty.EditorSession{Name: p.tabs[i].EditSession}.Kill()
			}
			p.tabs[i].EditSession = ""
			p.tabs[i].EditOrigMtime = time.Time{}
			p.tabs[i].EditEditor = ""
		}
	}
}

// setTabCloseHover lights the × of the preview tab the pointer is inside.
// The Files pane draws one strip, so the tab index alone names it.
func (p *Plugin) setTabCloseHover(action mouse.MouseAction) {
	p.hoverTabClose = tabs.CloseHover{}
	if action.Region == nil || action.Region.ID != regionPreviewTab {
		return
	}
	index, close, ok := previewTabPayload(action.Region.Data)
	if !ok || !close {
		return
	}
	p.hoverTabClose = tabs.CloseHoverAt(0, index)
}

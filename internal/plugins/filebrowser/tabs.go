package filebrowser

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

type TabOpenMode int

const (
	TabOpenReplace TabOpenMode = iota
	TabOpenNew
)

type FileTab struct {
	Path   string
	Scroll int
	Loaded bool
	Result PreviewResult

	// Edit state (persisted when switching away from inline editor)
	EditSession   string    // Tmux session name (empty if not in edit mode)
	EditOrigMtime time.Time // Original file mtime when editing started
	EditEditor    string    // Editor command used (vim, nano, etc.)
}

type tabHit struct {
	Index int
	X     int
	Width int
}

func (p *Plugin) findTab(path string) int {
	normalizedPath := filepath.Clean(path)
	for i, tab := range p.tabs {
		if filepath.Clean(tab.Path) == normalizedPath {
			return i
		}
	}
	return -1
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

	p.normalizeActiveTab()

	if idx := p.findTab(path); idx >= 0 {
		return p.switchTab(idx)
	}

	p.saveActiveTabState()

	if mode == TabOpenReplace && len(p.tabs) > 0 {
		p.tabs[p.activeTab] = FileTab{Path: path}
	} else {
		p.tabs = append(p.tabs, FileTab{Path: path})
		p.activeTab = len(p.tabs) - 1
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

	p.saveActiveTabState()
	p.activeTab = index

	return p.applyActiveTab()
}

func (p *Plugin) cycleTab(delta int) tea.Cmd {
	if len(p.tabs) < 2 {
		return nil
	}

	idx := p.activeTab + delta
	if idx < 0 {
		idx = len(p.tabs) - 1
	} else if idx >= len(p.tabs) {
		idx = 0
	}

	return p.switchTab(idx)
}

func (p *Plugin) closeTab(index int) tea.Cmd {
	if index < 0 || index >= len(p.tabs) {
		return nil
	}

	// Kill any tmux session associated with this tab
	p.killTabEditSession(index)

	// If closing the active tab that's currently in edit mode, clean up plugin state
	if index == p.activeTab && p.inlineEditMode {
		p.clearPluginEditState()
	}

	if index == p.activeTab {
		p.saveActiveTabState()
	}

	p.tabs = append(p.tabs[:index], p.tabs[index+1:]...)

	if len(p.tabs) == 0 {
		p.activeTab = 0
		p.previewFile = ""
		p.previewScroll = 0
		p.resetPreviewContent()
		p.resetPreviewModes()
		p.updateWatchedFile()
		return nil
	}

	if index < p.activeTab {
		p.activeTab--
	} else if index == p.activeTab {
		if p.activeTab >= len(p.tabs) {
			p.activeTab = len(p.tabs) - 1
		}
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

	return LoadPreview(p.ctx.WorkDir, tab.Path, p.ctx.Epoch)
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

	// Fallback: walk full tree to find node in unexpanded directories
	var targetNode *FileNode
	p.walkTree(p.tree.Root, func(node *FileNode) {
		if node.Path == path {
			targetNode = node
		}
	})

	if targetNode == nil {
		return
	}

	p.expandParents(targetNode)
	p.tree.Flatten()

	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}
}

func (p *Plugin) applyPreviewResult(result PreviewResult) {
	p.clearTextSelection()
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
	p.clearTextSelection()
	p.contentSearchMode = false
	p.contentSearchCommitted = false
	p.contentSearchQuery = ""
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

	labels := p.tabLabels(width)
	rendered := make([]string, 0, len(p.tabs))
	widths := make([]int, 0, len(p.tabs))

	for i, label := range labels {
		isActive := i == p.activeTab
		item := styles.RenderTab(label, i, len(p.tabs), isActive)
		rendered = append(rendered, item)
		widths = append(widths, lipgloss.Width(item))
	}

	start, end, showLeft, showRight := p.visibleTabRange(widths, width)
	if start > end {
		return ""
	}

	var tokens []string
	x := 0

	if showLeft {
		left := styles.Muted.Render("<")
		tokens = append(tokens, left)
		x += 1
	}

	for i := start; i <= end; i++ {
		if len(tokens) > 0 {
			tokens = append(tokens, " ")
			x += 1
		}
		tokens = append(tokens, rendered[i])
		p.tabHits = append(p.tabHits, tabHit{Index: i, X: x, Width: widths[i]})
		x += widths[i]
	}

	if showRight {
		if len(tokens) > 0 {
			tokens = append(tokens, " ")
			x += 1
		}
		right := styles.Muted.Render(">")
		tokens = append(tokens, right)
	}

	return strings.Join(tokens, "")
}

func (p *Plugin) visibleTabRange(widths []int, maxWidth int) (int, int, bool, bool) {
	if len(widths) == 0 {
		return 0, -1, false, false
	}
	if p.activeTab < 0 || p.activeTab >= len(widths) {
		return 0, -1, false, false
	}

	start := p.activeTab
	end := p.activeTab
	used := widths[p.activeTab]

	for {
		expanded := false
		if end+1 < len(widths) && used+1+widths[end+1] <= maxWidth {
			end++
			used += 1 + widths[end]
			expanded = true
		}
		if start-1 >= 0 && used+1+widths[start-1] <= maxWidth {
			start--
			used += 1 + widths[start]
			expanded = true
		}
		if !expanded {
			break
		}
	}

	showLeft := start > 0
	showRight := end < len(widths)-1

	for {
		indicatorTokens := 0
		if showLeft {
			indicatorTokens++
		}
		if showRight {
			indicatorTokens++
		}

		tabCount := end - start + 1
		if tabCount < 1 {
			return 0, -1, false, false
		}

		totalTokens := tabCount + indicatorTokens
		sepCount := totalTokens - 1
		totalWidth := p.sumTabWidths(widths, start, end) + indicatorTokens + sepCount

		if totalWidth <= maxWidth || tabCount == 1 {
			break
		}

		if end-p.activeTab >= p.activeTab-start {
			end--
		} else {
			start++
		}

		showLeft = start > 0
		showRight = end < len(widths)-1
	}

	return start, end, showLeft, showRight
}

func (p *Plugin) sumTabWidths(widths []int, start, end int) int {
	total := 0
	for i := start; i <= end; i++ {
		total += widths[i]
	}
	return total
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
	if !p.inlineEditMode || p.inlineEditSession == "" {
		return
	}
	tab := &p.tabs[p.activeTab]
	tab.EditSession = p.inlineEditSession
	tab.EditOrigMtime = p.inlineEditOrigMtime
	tab.EditEditor = p.inlineEditEditor
}

// clearPluginEditState clears plugin-level edit state without killing the tmux session.
// Used when detaching from editor (session keeps running in background).
func (p *Plugin) clearPluginEditState() {
	p.inlineEditMode = false
	p.inlineEditSession = ""
	p.inlineEditFile = ""
	p.inlineEditOrigMtime = time.Time{}
	p.inlineEditEditor = ""
	p.inlineEditorDragging = false
	p.inlineEditor.Exit()
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
	if !isSessionAlive(tab.EditSession) {
		// Session died while away - clear tab edit state
		tab.EditSession = ""
		tab.EditOrigMtime = time.Time{}
		tab.EditEditor = ""
		return false
	}
	// Restore to plugin-level state
	p.inlineEditMode = true
	p.inlineEditSession = tab.EditSession
	p.inlineEditFile = tab.Path
	p.inlineEditOrigMtime = tab.EditOrigMtime
	p.inlineEditEditor = tab.EditEditor
	return true
}

// killTabEditSession kills the tmux session for a tab if it has one.
func (p *Plugin) killTabEditSession(index int) {
	if index < 0 || index >= len(p.tabs) {
		return
	}
	tab := &p.tabs[index]
	if tab.EditSession != "" {
		killSession(tab.EditSession)
		tab.EditSession = ""
		tab.EditOrigMtime = time.Time{}
		tab.EditEditor = ""
	}
}

// cleanupAllEditSessions kills all tmux edit sessions for all tabs.
// Called on plugin exit to ensure no orphan tmux sessions remain.
func (p *Plugin) cleanupAllEditSessions() {
	// Clean up current plugin-level edit state
	if p.inlineEditMode && p.inlineEditSession != "" {
		killSession(p.inlineEditSession)
		p.clearPluginEditState()
	}
	// Clean up any backgrounded sessions in tabs
	for i := range p.tabs {
		if p.tabs[i].EditSession != "" {
			killSession(p.tabs[i].EditSession)
			p.tabs[i].EditSession = ""
			p.tabs[i].EditOrigMtime = time.Time{}
			p.tabs[i].EditEditor = ""
		}
	}
}

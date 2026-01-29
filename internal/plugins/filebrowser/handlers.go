package filebrowser

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
)

func (p *Plugin) handleKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// Quick open can be triggered from any context (except when already open)
	if key == "ctrl+p" && !p.quickOpenMode && !p.projectSearchMode {
		return p.openQuickOpen()
	}

	// Project search can be triggered from any context (except when already open)
	if key == "f" && !p.projectSearchMode && !p.quickOpenMode {
		return p.openProjectSearch()
	}

	// Handle project search mode
	if p.projectSearchMode {
		return p.handleProjectSearchKey(msg)
	}

	// Handle quick open mode
	if p.quickOpenMode {
		return p.handleQuickOpenKey(msg)
	}

	// Handle info modal
	if p.infoMode {
		return p.handleInfoKey(msg)
	}

	// Handle blame mode
	if p.blameMode {
		return p.handleBlameKey(msg)
	}

	// Handle file operation mode (move/rename/create/delete)
	if p.fileOpMode != FileOpNone {
		return p.handleFileOpKey(msg)
	}

	// Handle line jump mode
	if p.lineJumpMode {
		return p.handleLineJumpKey(msg)
	}

	// Handle content search mode input (preview pane search)
	if p.contentSearchMode {
		return p.handleContentSearchKey(msg)
	}

	// Handle tree search mode input
	if p.searchMode {
		return p.handleSearchKey(msg)
	}

	// Handle keys based on active pane
	if p.activePane == PanePreview {
		return p.handlePreviewKey(key)
	}
	return p.handleTreeKey(key)
}

func (p *Plugin) handleTreeKey(key string) (plugin.Plugin, tea.Cmd) {
	switch key {
	case "j", "down":
		if p.treeCursor < p.tree.Len()-1 {
			p.treeCursor++
			p.ensureTreeCursorVisible()
			return p, p.loadPreviewForCursor()
		}

	case "k", "up":
		if p.treeCursor > 0 {
			p.treeCursor--
			p.ensureTreeCursorVisible()
			return p, p.loadPreviewForCursor()
		}

	case "l", "right":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			if node.IsDir {
				_ = p.tree.Expand(node)
			} else {
				// Load file preview, switch to preview pane, and pin the tab
				p.activePane = PanePreview
				cmd := p.openTab(node.Path, TabOpenReplace)
				p.pinTab(p.activeTab)
				return p, cmd
			}
		}

	case "enter":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			if node.IsDir {
				// Toggle expand/collapse
				_ = p.tree.Toggle(node)
			} else {
				// Load file preview, switch to preview pane, and pin the tab
				p.activePane = PanePreview
				cmd := p.openTab(node.Path, TabOpenReplace)
				p.pinTab(p.activeTab)
				return p, cmd
			}
		}

	case "h", "left":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			if node.IsDir && node.IsExpanded {
				p.tree.Collapse(node)
			} else if node.Parent != nil && node.Parent != p.tree.Root {
				if idx := p.tree.IndexOf(node.Parent); idx >= 0 {
					p.treeCursor = idx
					p.ensureTreeCursorVisible()
				}
			}
		}

	case "g":
		p.treeCursor = 0
		p.treeScrollOff = 0

	case "G":
		if p.tree.Len() > 0 {
			p.treeCursor = p.tree.Len() - 1
			p.ensureTreeCursorVisible()
		}

	case "ctrl+d":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor += visibleHeight / 2
		if p.treeCursor >= p.tree.Len() {
			p.treeCursor = p.tree.Len() - 1
		}
		p.ensureTreeCursorVisible()

	case "ctrl+u":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor -= visibleHeight / 2
		if p.treeCursor < 0 {
			p.treeCursor = 0
		}
		p.ensureTreeCursorVisible()

	case "ctrl+f", "pgdown":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor += visibleHeight
		if p.treeCursor >= p.tree.Len() {
			p.treeCursor = p.tree.Len() - 1
		}
		p.ensureTreeCursorVisible()

	case "ctrl+b", "pgup":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor -= visibleHeight
		if p.treeCursor < 0 {
			p.treeCursor = 0
		}
		p.ensureTreeCursorVisible()

	case "e", "o":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && !node.IsDir {
			cmd := p.openTab(node.Path, TabOpenReplace)
			p.pinTab(p.activeTab)
			// Use inline editing if supported, otherwise open in external editor
			if p.isInlineEditSupported(node.Path) {
				return p, tea.Batch(cmd, p.enterInlineEditMode(node.Path))
			}
			return p, tea.Batch(cmd, p.openFile(node.Path))
		}

	case "E":
		// Open in external editor (full terminal, bypasses inline edit)
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && !node.IsDir {
			cmd := p.openTab(node.Path, TabOpenReplace)
			p.pinTab(p.activeTab)
			return p, tea.Batch(cmd, p.openFile(node.Path))
		}

	case "t":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && !node.IsDir {
			p.activePane = PanePreview
			// If the active tab is a preview of this file, promote it instead of duplicating
			if p.activeTab >= 0 && p.activeTab < len(p.tabs) &&
				p.tabs[p.activeTab].IsPreview &&
				filepath.Clean(p.tabs[p.activeTab].Path) == filepath.Clean(node.Path) {
				p.pinTab(p.activeTab)
				return p, nil
			}
			return p, p.openTab(node.Path, TabOpenNew)
		}

	case "R":
		// Rename file/directory
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			p.fileOpMode = FileOpRename
			p.fileOpTarget = node
			p.fileOpTextInput = textinput.New()
			p.fileOpTextInput.SetValue(node.Name)
			p.fileOpTextInput.Focus()
			p.fileOpTextInput.CursorEnd()
			p.fileOpError = ""
			p.fileOpButtonFocus = 0
		}

	case "ctrl+r":
		// Reveal in file manager (Finder/Explorer/etc.)
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			return p, p.revealInFileManager(node.Path)
		}

	case "i":
		// Show file info
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			p.infoMode = true
			p.clearInfoModal()
			p.gitStatus = "Loading..."
			p.gitLastCommit = "Loading..."
			return p, p.fetchGitInfo(node.Path)
		}

	case "B":
		// Show git blame for file
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && !node.IsDir {
			return p.openBlameView(node.Path)
		}

	case "r":
		// Refresh file tree
		p.lastRefresh = time.Now()
		return p, p.refresh()

	case "m":
		// Move file/directory
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			p.fileOpMode = FileOpMove
			p.fileOpTarget = node
			p.fileOpTextInput = textinput.New()
			p.fileOpTextInput.SetValue(node.Path)
			p.fileOpTextInput.Focus()
			p.fileOpTextInput.CursorEnd()
			p.fileOpError = ""
			p.fileOpButtonFocus = 0
			p.fileOpShowSuggestions = false
		}

	case "a":
		// Create new file in current directory
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			p.fileOpMode = FileOpCreateFile
			p.fileOpTarget = node // Use as reference for directory
			p.fileOpTextInput = textinput.New()
			p.fileOpTextInput.Placeholder = "filename"
			p.fileOpTextInput.Focus()
			p.fileOpError = ""
			p.fileOpButtonFocus = 0
		}

	case "A":
		// Create new directory in current directory
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			p.fileOpMode = FileOpCreateDir
			p.fileOpTarget = node // Use as reference for directory
			p.fileOpTextInput = textinput.New()
			p.fileOpTextInput.Placeholder = "dirname"
			p.fileOpTextInput.Focus()
			p.fileOpError = ""
			p.fileOpButtonFocus = 0
		}

	case "d":
		// Delete file/directory (requires confirmation)
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			p.fileOpMode = FileOpDelete
			p.fileOpTarget = node
			p.fileOpConfirmDelete = true
			p.fileOpError = ""
			p.fileOpButtonFocus = 1 // Start with confirm button focused
		}

	case "y":
		// Yank (mark) file/directory for paste
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			p.clipboardPath = node.Path
			p.clipboardIsDir = node.IsDir
			return p, appmsg.ShowToast("Marked for copy: "+node.Path, 2*time.Second)
		}

	case "Y":
		// Copy relative path to system clipboard
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			if err := clipboard.WriteAll(node.Path); err != nil {
				return p, appmsg.ShowToast("Failed to copy path", 2*time.Second)
			}
			return p, appmsg.ShowToast("Copied: "+node.Path, 2*time.Second)
		}

	case "p":
		// Paste file/directory from clipboard
		if p.clipboardPath != "" {
			node := p.tree.GetNode(p.treeCursor)
			if node != nil {
				return p, p.doPaste(node)
			}
		}

	case "s":
		// Cycle sort mode
		newMode := p.tree.SortMode.Next()
		p.tree.SetSortMode(newMode)

	case ":":
		p.lineJumpMode = true
		p.lineJumpBuffer = ""

	case "/":
		p.searchMode = true
		p.searchQuery = ""
		p.searchMatches = nil
		p.searchCursor = 0

	case "n":
		// Next match
		if len(p.searchMatches) > 0 {
			p.searchCursor = (p.searchCursor + 1) % len(p.searchMatches)
			p.jumpToSearchMatch()
		}

	case "N":
		// Previous match
		if len(p.searchMatches) > 0 {
			p.searchCursor--
			if p.searchCursor < 0 {
				p.searchCursor = len(p.searchMatches) - 1
			}
			p.jumpToSearchMatch()
		}

	case "tab", "shift+tab":
		// Switch focus to preview pane (if tree visible and file selected)
		if p.treeVisible && p.previewFile != "" {
			p.activePane = PanePreview
		}

	case "\\":
		// Toggle tree pane visibility
		p.treeVisible = !p.treeVisible
		if !p.treeVisible {
			// When hiding tree, focus moves to preview pane
			p.activePane = PanePreview
		}

	case "I":
		// Toggle git-ignored file visibility
		p.showIgnored = !p.showIgnored
		p.tree.ShowIgnored = p.showIgnored
		p.tree.Flatten()
		// Ensure cursor stays valid
		if p.treeCursor >= p.tree.Len() {
			p.treeCursor = max(0, p.tree.Len()-1)
		}
		p.ensureTreeCursorVisible()
		// Persist state
		p.saveState()
		// Update preview if current file was hidden
		if p.treeCursor >= 0 && p.treeCursor < p.tree.Len() {
			node := p.tree.GetNode(p.treeCursor)
			if node != nil && node.Path != p.previewFile {
				return p, p.openTab(node.Path, TabOpenReplace)
			}
		}

	case "[":
		return p, p.cycleTab(-1)

	case "]":
		return p, p.cycleTab(1)

	case "x":
		return p, p.closeTab(p.activeTab)
	}

	return p, nil
}

func (p *Plugin) handlePreviewKey(key string) (plugin.Plugin, tea.Cmd) {
	lines := p.getPreviewLines()
	visibleHeight := p.visibleContentHeight()
	maxScroll := len(lines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch key {
	case "j", "down":
		if p.previewScroll < maxScroll {
			p.previewScroll++
		}

	case "k", "up":
		if p.previewScroll > 0 {
			p.previewScroll--
		}

	case "g":
		p.previewScroll = 0

	case "G":
		p.previewScroll = maxScroll

	case "ctrl+d":
		p.previewScroll += visibleHeight / 2
		if p.previewScroll > maxScroll {
			p.previewScroll = maxScroll
		}

	case "ctrl+u":
		p.previewScroll -= visibleHeight / 2
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}

	case "ctrl+f", "pgdown":
		p.previewScroll += visibleHeight
		if p.previewScroll > maxScroll {
			p.previewScroll = maxScroll
		}

	case "ctrl+b", "pgup":
		p.previewScroll -= visibleHeight
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}

	case "h", "left", "esc":
		// Return to tree pane
		p.activePane = PaneTree
		p.clearTextSelection()

	case "e":
		// Open previewed file in editor
		if p.previewFile != "" {
			// Use inline editing if supported, otherwise open in external editor
			if p.isInlineEditSupported(p.previewFile) {
				return p, p.enterInlineEditMode(p.previewFile)
			}
			return p, p.openFile(p.previewFile)
		}

	case "E":
		// Open in external editor (full terminal, bypasses inline edit)
		if p.previewFile != "" {
			return p, p.openFile(p.previewFile)
		}

	case ":":
		p.lineJumpMode = true
		p.lineJumpBuffer = ""

	case "/":
		// Enter content search mode if we have content to search
		if len(p.previewLines) > 0 && !p.isBinary {
			p.clearTextSelection() // Clear selection before entering search
			p.contentSearchMode = true
			p.contentSearchCommitted = false
			p.contentSearchQuery = ""
			p.contentSearchMatches = nil
			p.contentSearchCursor = 0
		}

	case "r":
		// Refresh file tree
		p.lastRefresh = time.Now()
		return p, p.refresh()

	case "R":
		// Rename the previewed file
		if p.previewFile != "" {
			node := p.tree.FindByPath(p.previewFile)
			if node != nil && node != p.tree.Root {
				p.fileOpMode = FileOpRename
				p.fileOpTarget = node
				p.fileOpTextInput = textinput.New()
				p.fileOpTextInput.SetValue(node.Name)
				p.fileOpTextInput.Focus()
				p.fileOpTextInput.CursorEnd()
				p.fileOpError = ""
				p.fileOpButtonFocus = 0
			}
		}

	case "ctrl+r":
		// Reveal in file manager (Finder/Explorer/etc.)
		if p.previewFile != "" {
			return p, p.revealInFileManager(p.previewFile)
		}

	case "i":
		// Show file info
		if p.previewFile != "" {
			p.infoMode = true
			p.clearInfoModal()
			p.gitStatus = "Loading..."
			p.gitLastCommit = "Loading..."
			return p, p.fetchGitInfo(p.previewFile)
		}

	case "y":
		// Copy selected text to clipboard, or entire file contents if no selection
		if p.hasTextSelection() {
			return p, p.copySelectedTextToClipboard()
		}
		return p, p.copyFileContentsToClipboard()

	case "Y":
		// Copy file path to clipboard
		if p.previewFile != "" {
			if err := clipboard.WriteAll(p.previewFile); err != nil {
				return p, appmsg.ShowToast("Failed to copy path", 2*time.Second)
			}
			return p, appmsg.ShowToast("Copied: "+p.previewFile, 2*time.Second)
		}

	case "m":
		// Toggle markdown rendering for .md files
		if p.isMarkdownFile() {
			p.toggleMarkdownRender()
		}

	case "w":
		// Toggle line wrapping
		p.previewWrapEnabled = !p.previewWrapEnabled
		_ = state.SetLineWrapEnabled(p.previewWrapEnabled)
		p.previewScroll = 0

	case "B":
		// Show git blame for current preview file
		if p.previewFile != "" {
			return p.openBlameView(p.previewFile)
		}

	case "[":
		return p, p.cycleTab(-1)

	case "]":
		return p, p.cycleTab(1)

	case "x":
		return p, p.closeTab(p.activeTab)

	case "tab", "shift+tab":
		// Switch focus to tree pane (if visible)
		if p.treeVisible {
			p.activePane = PaneTree
		}

	case "\\":
		// Toggle tree pane visibility
		p.treeVisible = !p.treeVisible
		if !p.treeVisible {
			p.activePane = PanePreview
		} else {
			p.activePane = PaneTree
		}
	}

	return p, nil
}

// handleFileOpKey handles key input during file operation mode (move/rename/create/delete).
func (p *Plugin) handleFileOpKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// Handle delete confirmation mode
	if p.fileOpConfirmDelete {
		switch key {
		case "y", "Y", "enter":
			// Proceed with delete (y/Y or Enter when confirm focused)
			if key == "enter" && p.fileOpButtonFocus == 2 {
				// Cancel button focused, treat as cancel
				p.fileOpMode = FileOpNone
				p.fileOpTarget = nil
				p.fileOpError = ""
				p.fileOpConfirmDelete = false
				return p, nil
			}
			p.fileOpConfirmDelete = false
			return p, p.doDelete()
		case "n", "N", "esc":
			// Cancel delete
			p.fileOpMode = FileOpNone
			p.fileOpTarget = nil
			p.fileOpError = ""
			p.fileOpConfirmDelete = false
			return p, nil
		case "tab":
			// Toggle between confirm (1) and cancel (2)
			if p.fileOpButtonFocus == 1 {
				p.fileOpButtonFocus = 2
			} else {
				p.fileOpButtonFocus = 1
			}
			return p, nil
		case "shift+tab":
			// Reverse toggle
			if p.fileOpButtonFocus == 2 {
				p.fileOpButtonFocus = 1
			} else {
				p.fileOpButtonFocus = 2
			}
			return p, nil
		}
		return p, nil
	}

	// Handle confirmation mode for directory creation (during move)
	if p.fileOpConfirmCreate {
		switch key {
		case "y", "Y", "enter":
			// Create directory and proceed with move (y/Y or Enter when confirm focused)
			if key == "enter" && p.fileOpButtonFocus == 2 {
				// Cancel button focused, return to edit mode
				p.fileOpConfirmCreate = false
				p.fileOpConfirmPath = ""
				return p, nil
			}
			if err := os.MkdirAll(p.fileOpConfirmPath, 0755); err != nil {
				p.fileOpError = fmt.Sprintf("failed to create directory: %v", err)
				p.fileOpConfirmCreate = false
				p.fileOpConfirmPath = ""
				return p, nil
			}
			p.fileOpConfirmCreate = false
			p.fileOpConfirmPath = ""
			return p.executeFileOp() // Retry the operation
		case "n", "N", "esc":
			// Cancel - return to edit mode
			p.fileOpConfirmCreate = false
			p.fileOpConfirmPath = ""
			return p, nil
		case "tab":
			// Toggle between confirm (1) and cancel (2)
			if p.fileOpButtonFocus == 1 {
				p.fileOpButtonFocus = 2
			} else {
				p.fileOpButtonFocus = 1
			}
			return p, nil
		case "shift+tab":
			// Reverse toggle
			if p.fileOpButtonFocus == 2 {
				p.fileOpButtonFocus = 1
			} else {
				p.fileOpButtonFocus = 2
			}
			return p, nil
		}
		return p, nil
	}

	switch key {
	case "esc":
		// Cancel file operation
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		p.fileOpShowSuggestions = false
		return p, nil

	case "up", "ctrl+p":
		// Navigate suggestions up (for move modal)
		if p.fileOpMode == FileOpMove && p.fileOpShowSuggestions && len(p.fileOpSuggestions) > 0 {
			p.fileOpSuggestionIdx--
			if p.fileOpSuggestionIdx < -1 {
				p.fileOpSuggestionIdx = len(p.fileOpSuggestions) - 1
			}
			return p, nil
		}
		return p, nil

	case "down", "ctrl+n":
		// Navigate suggestions down (for move modal)
		if p.fileOpMode == FileOpMove && p.fileOpShowSuggestions && len(p.fileOpSuggestions) > 0 {
			p.fileOpSuggestionIdx++
			if p.fileOpSuggestionIdx >= len(p.fileOpSuggestions) {
				p.fileOpSuggestionIdx = -1
			}
			return p, nil
		}
		return p, nil

	case "tab":
		// If suggestions are visible, use tab to complete
		if p.fileOpMode == FileOpMove && p.fileOpShowSuggestions {
			idx := p.fileOpSuggestionIdx
			if idx < 0 {
				idx = 0 // Auto-select first if none selected
			}
			if idx < len(p.fileOpSuggestions) {
				p.fileOpTextInput.SetValue(p.fileOpSuggestions[idx])
				p.fileOpShowSuggestions = false
				p.fileOpTextInput.CursorEnd()
				return p, nil
			}
		}

		// Cycle focus: input(0) -> confirm(1) -> cancel(2) -> input(0)
		p.fileOpButtonFocus = (p.fileOpButtonFocus + 1) % 3
		if p.fileOpButtonFocus == 0 {
			p.fileOpTextInput.Focus()
		} else {
			p.fileOpTextInput.Blur()
		}
		return p, nil

	case "shift+tab":
		// Reverse cycle
		p.fileOpButtonFocus = (p.fileOpButtonFocus - 1 + 3) % 3
		if p.fileOpButtonFocus == 0 {
			p.fileOpTextInput.Focus()
		} else {
			p.fileOpTextInput.Blur()
		}
		return p, nil

	case "enter":
		// If cancel button focused, cancel operation
		if p.fileOpButtonFocus == 2 {
			p.fileOpMode = FileOpNone
			p.fileOpTarget = nil
			p.fileOpError = ""
			p.fileOpShowSuggestions = false
			return p, nil
		}

		// If suggestions active and selected, use suggestion
		if p.fileOpMode == FileOpMove && p.fileOpShowSuggestions && p.fileOpSuggestionIdx >= 0 {
			if p.fileOpSuggestionIdx < len(p.fileOpSuggestions) {
				p.fileOpTextInput.SetValue(p.fileOpSuggestions[p.fileOpSuggestionIdx])
				p.fileOpShowSuggestions = false
				p.fileOpTextInput.CursorEnd()
				return p, nil
			}
		}

		// Otherwise execute file operation
		return p.executeFileOp()

	default:
		// Only delegate to textinput if input is focused
		if p.fileOpButtonFocus == 0 {
			var cmd tea.Cmd
			p.fileOpTextInput, cmd = p.fileOpTextInput.Update(msg)
			p.fileOpError = "" // Clear error on input change

			// Update suggestions for move modal on text change
			if p.fileOpMode == FileOpMove {
				query := p.fileOpTextInput.Value()
				if len(query) > 0 {
					p.fileOpSuggestions = p.getPathSuggestions(query)
					p.fileOpSuggestionIdx = -1
					p.fileOpShowSuggestions = len(p.fileOpSuggestions) > 0
				} else {
					p.fileOpShowSuggestions = false
				}
			}

			return p, cmd
		}
		return p, nil
	}
}

// handleInfoKey handles key input during info modal mode.
func (p *Plugin) handleInfoKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	p.ensureInfoModal()
	if p.infoModal == nil {
		p.infoMode = false
		return p, nil
	}

	switch msg.String() {
	case "q", "i":
		p.infoMode = false
		p.clearInfoModal()
		return p, nil
	}

	action, cmd := p.infoModal.HandleKey(msg)
	if action == "cancel" {
		p.infoMode = false
		p.clearInfoModal()
		return p, nil
	}
	return p, cmd
}

// handleContentSearchKey handles key input during content search mode.
// Implements vim-style two-phase search: type query, Enter to commit, then n/N navigate.
func (p *Plugin) handleContentSearchKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// Esc always exits search mode completely
	if key == "esc" {
		p.contentSearchMode = false
		p.contentSearchCommitted = false
		p.contentSearchQuery = ""
		p.contentSearchMatches = nil
		p.contentSearchCursor = 0
		return p, nil
	}

	// Phase 1: Typing query (not yet committed)
	if !p.contentSearchCommitted {
		switch key {
		case "enter":
			// Commit the search - now n/N will navigate matches
			if len(p.contentSearchQuery) > 0 {
				p.contentSearchCommitted = true
			}
		case "backspace":
			if len(p.contentSearchQuery) > 0 {
				runes := []rune(p.contentSearchQuery)
				p.contentSearchQuery = string(runes[:len(runes)-1])
				p.updateContentMatches()
			}
		default:
			// All printable characters go to query (including n, N, etc.)
			if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
				p.contentSearchQuery += key
				p.updateContentMatches()
			}
		}
		return p, nil
	}

	// Phase 2: Search committed - n/N navigate matches, j/k scroll preview
	switch key {
	case "n":
		if len(p.contentSearchMatches) > 0 {
			p.contentSearchCursor = (p.contentSearchCursor + 1) % len(p.contentSearchMatches)
			p.scrollToContentMatch()
		}
	case "N":
		if len(p.contentSearchMatches) > 0 {
			p.contentSearchCursor--
			if p.contentSearchCursor < 0 {
				p.contentSearchCursor = len(p.contentSearchMatches) - 1
			}
			p.scrollToContentMatch()
		}
	case "j", "down":
		// Scroll preview down one line
		visibleHeight := p.visibleContentHeight()
		maxScroll := len(p.getPreviewLines()) - visibleHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.previewScroll < maxScroll {
			p.previewScroll++
		}
	case "k", "up":
		// Scroll preview up one line
		if p.previewScroll > 0 {
			p.previewScroll--
		}
	case "enter":
		// Exit search, keep position at current match
		p.contentSearchMode = false
		p.contentSearchCommitted = false
	case "ctrl+d":
		// Half-page scroll down while keeping search active
		visibleHeight := p.visibleContentHeight()
		maxScroll := len(p.getPreviewLines()) - visibleHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		p.previewScroll += visibleHeight / 2
		if p.previewScroll > maxScroll {
			p.previewScroll = maxScroll
		}
	case "ctrl+u":
		// Half-page scroll up while keeping search active
		visibleHeight := p.visibleContentHeight()
		p.previewScroll -= visibleHeight / 2
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}
	}

	return p, nil
}

// handleQuickOpenKey handles key input during quick open mode.
func (p *Plugin) handleQuickOpenKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		p.quickOpenMode = false
		p.quickOpenQuery = ""
		p.quickOpenMatches = nil
		p.quickOpenCursor = 0

	case "enter":
		if len(p.quickOpenMatches) > 0 && p.quickOpenCursor < len(p.quickOpenMatches) {
			return p.selectQuickOpenMatch()
		}

	case "up", "ctrl+p":
		if p.quickOpenCursor > 0 {
			p.quickOpenCursor--
		}

	case "down", "ctrl+n":
		if p.quickOpenCursor < len(p.quickOpenMatches)-1 {
			p.quickOpenCursor++
		}

	case "backspace":
		if len(p.quickOpenQuery) > 0 {
			runes := []rune(p.quickOpenQuery)
			p.quickOpenQuery = string(runes[:len(runes)-1])
			p.updateQuickOpenMatches()
		}

	default:
		// Append printable characters
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			p.quickOpenQuery += key
			p.updateQuickOpenMatches()
		}
	}

	return p, nil
}

// handleProjectSearchKey handles key input during project search mode.
func (p *Plugin) handleProjectSearchKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()
	state := p.projectSearchState

	p.ensureProjectSearchModal()
	if p.projectSearchModal == nil {
		p.projectSearchMode = false
		p.projectSearchState = nil
		return p, nil
	}

	// Handle enter before modal to ensure it opens the result at state.Cursor
	// (modal's focus might be on an option button, but we want to open the selected result)
	if key == "enter" && state != nil && len(state.Results) > 0 {
		return p.openProjectSearchResult()
	}
	if key == "shift+enter" && state != nil && len(state.Results) > 0 {
		return p.openProjectSearchResultInNewTab()
	}

	action, cmd := p.projectSearchModal.HandleKey(msg)
	if action == "cancel" {
		p.projectSearchMode = false
		p.projectSearchState = nil
		p.clearProjectSearchModal()
		return p, nil
	}

	if action != "" && state != nil {
		switch action {
		case projectSearchOpenActionID:
			return p.openProjectSearchResult()
		case projectSearchToggleRegexID:
			return p.toggleProjectSearchOption(state, &state.UseRegex)
		case projectSearchToggleCaseID:
			return p.toggleProjectSearchOption(state, &state.CaseSensitive)
		case projectSearchToggleWordID:
			return p.toggleProjectSearchOption(state, &state.WholeWord)
		}

		if fileIdx, ok := parseProjectSearchFileID(action); ok {
			if flatIdx := p.findFlatIndexForFile(fileIdx); flatIdx >= 0 {
				state.Cursor = flatIdx
			}
			return p.openProjectSearchResult()
		}
		if fileIdx, matchIdx, ok := parseProjectSearchMatchID(action); ok {
			if flatIdx := p.findFlatIndexForMatch(fileIdx, matchIdx); flatIdx >= 0 {
				state.Cursor = flatIdx
			}
			return p.openProjectSearchResult()
		}
	}

	switch key {
	case "esc":
		// Close project search
		p.projectSearchMode = false
		p.projectSearchState = nil
		p.clearProjectSearchModal()

	// Note: enter and shift+enter are handled before modal.HandleKey above

	case "left":
		// Collapse file group containing cursor's match
		if state != nil {
			fileIdx, _, _ := state.FlatItem(state.Cursor)
			if fileIdx >= 0 && fileIdx < len(state.Results) {
				state.Results[fileIdx].Collapsed = true
				// After collapse, snap to nearest visible match
				state.Cursor = state.NearestMatchIndex(state.Cursor)
			}
		}

	case "right":
		// Expand file group containing cursor's match
		if state != nil {
			fileIdx, _, _ := state.FlatItem(state.Cursor)
			if fileIdx >= 0 && fileIdx < len(state.Results) {
				state.Results[fileIdx].Collapsed = false
				// After expand, snap to first match in that file
				state.Cursor = state.NearestMatchIndex(state.Cursor)
			}
		}

	case "down", "ctrl+n":
		if state != nil {
			// Skip file headers, only navigate between matches
			state.Cursor = state.NextMatchIndex()
		}

	case "up", "ctrl+p":
		if state != nil {
			// Skip file headers, only navigate between matches
			state.Cursor = state.PrevMatchIndex()
		}

	case "ctrl+g":
		// Go to first match (ctrl+g to avoid conflict with typing 'g')
		if state != nil {
			state.Cursor = state.FirstMatchIndex()
			state.ScrollOffset = 0
		}

	case "ctrl+e":
		// Open in editor at line (ctrl+e to avoid conflict with typing 'e')
		if state != nil && len(state.Results) > 0 {
			path, lineNo := state.GetSelectedFile()
			if path != "" {
				// Close project search
				p.projectSearchMode = false
				p.projectSearchState = nil
				p.clearProjectSearchModal()
				return p, p.openFileAtLine(path, lineNo)
			}
		}

	case "ctrl+d":
		// Page down, snap to nearest match
		if state != nil {
			state.Cursor += 10
			maxIdx := state.FlatLen() - 1
			if state.Cursor > maxIdx {
				state.Cursor = maxIdx
			}
			if state.Cursor < 0 {
				state.Cursor = 0
			}
			// Snap to nearest match (skip file headers)
			state.Cursor = state.NearestMatchIndex(state.Cursor)
		}

	case "ctrl+u":
		// Page up, snap to nearest match
		if state != nil {
			state.Cursor -= 10
			if state.Cursor < 0 {
				state.Cursor = 0
			}
			// Snap to nearest match (skip file headers)
			state.Cursor = state.NearestMatchIndex(state.Cursor)
		}

	case "alt+r":
		// Toggle regex mode
		return p.toggleProjectSearchOption(state, &state.UseRegex)

	case "alt+c":
		// Toggle case sensitivity
		return p.toggleProjectSearchOption(state, &state.CaseSensitive)

	case "alt+w":
		// Toggle whole word
		return p.toggleProjectSearchOption(state, &state.WholeWord)

	case "backspace":
		if state != nil && len(state.Query) > 0 {
			runes := []rune(state.Query)
			state.Query = string(runes[:len(runes)-1])
			if state.Query == "" {
				state.Results = nil
				state.Error = ""
				state.DebounceVersion++ // Cancel any pending search
			} else {
				state.IsSearching = true
				state.DebounceVersion++
				return p, scheduleProjectSearch(state.DebounceVersion, state.Query)
			}
		}

	default:
		// Append printable characters
		if state != nil && len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			state.Query += key
			state.IsSearching = true
			state.DebounceVersion++
			return p, scheduleProjectSearch(state.DebounceVersion, state.Query)
		}
	}

	return p, cmd
}

func (p *Plugin) toggleProjectSearchOption(state *ProjectSearchState, option *bool) (plugin.Plugin, tea.Cmd) {
	if state == nil || option == nil {
		return p, nil
	}

	*option = !*option
	if state.Query != "" {
		state.IsSearching = true
		state.DebounceVersion++ // Cancel any pending debounced search
		return p, RunProjectSearch(p.ctx.WorkDir, state, p.ctx.Epoch)
	}
	return p, nil
}

// handleSearchKey handles key input during search mode.
func (p *Plugin) handleSearchKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		p.searchMode = false
		p.searchQuery = ""

	case "enter":
		// Jump to selected match and exit search
		if len(p.searchMatches) > 0 && p.searchCursor < len(p.searchMatches) {
			match := p.searchMatches[p.searchCursor]
			p.jumpToSearchMatch()
			p.searchMode = false
			// If it's a file, load preview
			if !match.IsDir {
				return p, p.openTab(match.Path, TabOpenReplace)
			}
			return p, nil
		}
		p.searchMode = false

	case "backspace":
		if len(p.searchQuery) > 0 {
			runes := []rune(p.searchQuery)
			p.searchQuery = string(runes[:len(runes)-1])
			p.updateSearchMatches()
		}

	case "up", "ctrl+p":
		if p.searchCursor > 0 {
			p.searchCursor--
		}

	case "down", "ctrl+n":
		if p.searchCursor < len(p.searchMatches)-1 {
			p.searchCursor++
		}

	default:
		// Append printable characters to query
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			p.searchQuery += key
			p.updateSearchMatches()
		}
	}

	return p, nil
}

// visibleContentHeight returns the number of lines available for content.
func (p *Plugin) visibleContentHeight() int {
	// height - footer (1) - content search bar (0 or 1) - pane border (2) - header (2)
	// Note: tree search bar is inside the pane header, not counted separately
	searchBar := 0
	if p.contentSearchMode {
		searchBar = 1
	}
	h := p.height - 1 - searchBar - 2 - 2
	if h < 1 {
		return 1
	}
	return h
}

// ensureTreeCursorVisible adjusts scroll offset to keep cursor visible.
func (p *Plugin) ensureTreeCursorVisible() {
	visibleHeight := p.visibleContentHeight()

	if p.treeCursor < p.treeScrollOff {
		p.treeScrollOff = p.treeCursor
	} else if p.treeCursor >= p.treeScrollOff+visibleHeight {
		p.treeScrollOff = p.treeCursor - visibleHeight + 1
	}
}

// loadPreviewForCursor loads the preview for the file at the current tree cursor.
func (p *Plugin) loadPreviewForCursor() tea.Cmd {
	node := p.tree.GetNode(p.treeCursor)
	if node == nil || node.IsDir {
		return nil
	}
	return p.openTab(node.Path, TabOpenPreview)
}

// openBlameView opens the blame view for the specified file.
func (p *Plugin) openBlameView(path string) (plugin.Plugin, tea.Cmd) {
	p.blameMode = true
	p.blameState = &BlameState{
		FilePath:  path,
		IsLoading: true,
	}
	return p, RunGitBlame(p.ctx.WorkDir, path, p.ctx.Epoch)
}

// blameVisibleHeight returns the visible height for blame content.
func (p *Plugin) blameVisibleHeight() int {
	h := p.height - blameModalHeaderFooterLines
	if h < blameMinVisibleLines {
		return blameMinVisibleLines
	}
	return h
}

// handleBlameKey handles key input during blame view mode.
func (p *Plugin) handleBlameKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	p.ensureBlameModal()
	if p.blameModal == nil || p.blameState == nil {
		p.blameMode = false
		return p, nil
	}

	key := msg.String()

	// Handle modal keys (Esc, etc.)
	action, cmd := p.blameModal.HandleKey(msg)
	switch action {
	case "cancel", blameActionID:
		// Close blame view
		p.blameMode = false
		p.blameState = nil
		p.blameModal = nil
		p.blameModalWidth = 0
		return p, nil
	}

	// Handle custom navigation keys
	visibleHeight := p.blameVisibleHeight()

	switch key {
	case "q":
		// Close blame view
		p.blameMode = false
		p.blameState = nil
		p.blameModal = nil
		p.blameModalWidth = 0
		return p, nil

	case "j", "down":
		// Move cursor down
		if p.blameState.Cursor < len(p.blameState.Lines)-1 {
			p.blameState.Cursor++
		}

	case "k", "up":
		// Move cursor up
		if p.blameState.Cursor > 0 {
			p.blameState.Cursor--
		}

	case "g":
		// Go to top
		p.blameState.Cursor = 0
		p.blameState.ScrollOffset = 0

	case "G":
		// Go to bottom
		if len(p.blameState.Lines) > 0 {
			p.blameState.Cursor = len(p.blameState.Lines) - 1
		}

	case "ctrl+d":
		// Page down
		p.blameState.Cursor += visibleHeight / 2
		if p.blameState.Cursor >= len(p.blameState.Lines) {
			p.blameState.Cursor = len(p.blameState.Lines) - 1
		}
		if p.blameState.Cursor < 0 {
			p.blameState.Cursor = 0
		}

	case "ctrl+u":
		// Page up
		p.blameState.Cursor -= visibleHeight / 2
		if p.blameState.Cursor < 0 {
			p.blameState.Cursor = 0
		}

	case "y":
		// Copy commit hash to clipboard
		if len(p.blameState.Lines) > 0 && p.blameState.Cursor < len(p.blameState.Lines) {
			line := p.blameState.Lines[p.blameState.Cursor]
			if err := clipboard.WriteAll(line.CommitHash); err != nil {
				return p, appmsg.ShowToast(fmt.Sprintf("Copy failed: %v", err), 3*time.Second)
			}
			return p, appmsg.ShowToast("Copied: "+line.CommitHash, 2*time.Second)
		}

	case "enter":
		// Show commit details (toast for now)
		if len(p.blameState.Lines) > 0 && p.blameState.Cursor < len(p.blameState.Lines) {
			line := p.blameState.Lines[p.blameState.Cursor]
			info := fmt.Sprintf("%s by %s (%s)", line.CommitHash, line.Author, RelativeTime(line.AuthorTime))
			return p, appmsg.ShowToast(info, 3*time.Second)
		}
	}

	// Return modal cmd if any
	if cmd != nil {
		return p, cmd
	}
	return p, nil
}

// handleLineJumpKey handles key input during line jump mode (vim-style :<number>).
func (p *Plugin) handleLineJumpKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		p.lineJumpMode = false
		p.lineJumpBuffer = ""
		return p, nil

	case "enter":
		if p.lineJumpBuffer == "" {
			p.lineJumpMode = false
			return p, nil
		}

		lineNum, err := strconv.Atoi(p.lineJumpBuffer)
		if err != nil {
			p.lineJumpMode = false
			p.lineJumpBuffer = ""
			return p, appmsg.ShowToast("Invalid line number", 2*time.Second)
		}

		// Execute jump (1-based input -> 0-based index)
		target := lineNum - 1

		if p.activePane == PanePreview {
			// Jump in preview pane
			lines := p.getPreviewLines()
			if len(lines) > 0 {
				if target < 0 {
					target = 0
				}
				if target >= len(lines) {
					target = len(lines) - 1
				}
				p.previewScroll = target

				// Ensure visible
				visibleHeight := p.visibleContentHeight()
				maxScroll := len(lines) - visibleHeight
				if maxScroll < 0 {
					maxScroll = 0
				}
				if p.previewScroll > maxScroll {
					p.previewScroll = maxScroll
				}
			}
		} else {
			// Jump in tree pane
			if p.tree.Len() > 0 {
				if target < 0 {
					target = 0
				}
				if target >= p.tree.Len() {
					target = p.tree.Len() - 1
				}
				p.treeCursor = target
				p.ensureTreeCursorVisible()
			}
		}

		p.lineJumpMode = false
		p.lineJumpBuffer = ""
		return p, nil

	case "backspace":
		if len(p.lineJumpBuffer) > 0 {
			p.lineJumpBuffer = p.lineJumpBuffer[:len(p.lineJumpBuffer)-1]
		} else {
			// If buffer empty, backspace exits mode (like vim)
			p.lineJumpMode = false
		}
		return p, nil

	default:
		// Only accept numbers
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			p.lineJumpBuffer += key
		}
	}

	return p, nil
}

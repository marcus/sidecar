package filebrowser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/tty"
)

// InlineEditStartedMsg is sent when inline edit mode starts successfully.
type InlineEditStartedMsg struct {
	SessionName   string
	FilePath      string
	OriginalMtime time.Time // File mtime before editing (to detect changes)
	Editor        string    // Editor command used (vim, nano, emacs, etc.)
}

// InlineEditExitedMsg is sent when inline edit mode exits.
type InlineEditExitedMsg struct {
	FilePath string
}

// enterInlineEditMode starts inline editing for the specified file.
// Creates a tmux session running the user's editor and delegates to tty.Model.
func (p *Plugin) enterInlineEditMode(path string) tea.Cmd {
	// Check feature flag
	if !features.IsEnabled(features.TmuxInlineEdit.Name) {
		return p.openFile(path)
	}

	fullPath := filepath.Join(p.ctx.WorkDir, path)

	// Get user's editor preference
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vim"
	}

	// Generate a unique session name
	sessionName := fmt.Sprintf("sidecar-edit-%d", time.Now().UnixNano())

	// Get TERM for color support (inherit from parent or default to xterm-256color)
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}

	return func() tea.Msg {
		// Check if tmux is available
		if _, err := exec.LookPath("tmux"); err != nil {
			// Fall back to external editor
			return nil
		}

		// Capture original mtime to detect changes later
		var origMtime time.Time
		if info, err := os.Stat(fullPath); err == nil {
			origMtime = info.ModTime()
		}

		// Create a detached tmux session with the editor
		// Use -x and -y to set initial size (will be resized later)
		// Pass TERM environment for proper color/theme support
		cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName,
			"-x", "80", "-y", "24", "-e", "TERM="+term, editor, fullPath)
		if err := cmd.Run(); err != nil {
			return msg.ToastMsg{
				Message:  fmt.Sprintf("Failed to start editor: %v", err),
				Duration: 3 * time.Second,
				IsError:  true,
			}
		}

		return InlineEditStartedMsg{
			SessionName:   sessionName,
			FilePath:      path,
			OriginalMtime: origMtime,
			Editor:        editor,
		}
	}
}

// handleInlineEditStarted processes the InlineEditStartedMsg and activates the tty model.
func (p *Plugin) handleInlineEditStarted(msg InlineEditStartedMsg) tea.Cmd {
	p.inlineEditMode = true
	p.inlineEditSession = msg.SessionName
	p.inlineEditFile = msg.FilePath
	p.inlineEditOrigMtime = msg.OriginalMtime
	p.inlineEditEditor = msg.Editor

	// Configure the tty model callbacks
	p.inlineEditor.OnExit = func() tea.Cmd {
		return func() tea.Msg {
			return InlineEditExitedMsg{FilePath: p.inlineEditFile}
		}
	}
	p.inlineEditor.OnAttach = func() tea.Cmd {
		// Attach to full tmux session
		return p.attachToInlineEditSession()
	}

	// Enter interactive mode on the tty model
	width := p.calculateInlineEditorWidth()
	height := p.calculateInlineEditorHeight()
	p.inlineEditor.SetDimensions(width, height)

	return p.inlineEditor.Enter(msg.SessionName, "")
}

// reattachInlineEditSession re-attaches to an existing tmux session after tab switch.
// Called when returning to a tab that was previously in edit mode.
func (p *Plugin) reattachInlineEditSession() tea.Cmd {
	if p.inlineEditSession == "" {
		return nil
	}

	// Configure the tty model callbacks (same as handleInlineEditStarted)
	p.inlineEditor.OnExit = func() tea.Cmd {
		return func() tea.Msg {
			return InlineEditExitedMsg{FilePath: p.inlineEditFile}
		}
	}
	p.inlineEditor.OnAttach = func() tea.Cmd {
		return p.attachToInlineEditSession()
	}

	// Enter interactive mode with the existing session
	width := p.calculateInlineEditorWidth()
	height := p.calculateInlineEditorHeight()
	p.inlineEditor.SetDimensions(width, height)

	return p.inlineEditor.Enter(p.inlineEditSession, "")
}

// exitInlineEditMode cleans up inline edit state and kills the tmux session.
func (p *Plugin) exitInlineEditMode() {
	if p.inlineEditSession != "" {
		// Kill the tmux session
		_ = exec.Command("tmux", "kill-session", "-t", p.inlineEditSession).Run()
	}
	p.inlineEditMode = false
	p.inlineEditSession = ""
	p.inlineEditFile = ""
	p.inlineEditOrigMtime = time.Time{}
	p.inlineEditEditor = ""
	p.inlineEditorDragging = false
	p.inlineEditor.Exit()
}

// isFileModifiedSinceEdit checks if the file was modified since editing started.
// Returns false if we can't determine (safe to skip confirmation).
func (p *Plugin) isFileModifiedSinceEdit() bool {
	if p.inlineEditFile == "" || p.inlineEditOrigMtime.IsZero() {
		return false // Can't determine, assume not modified
	}
	fullPath := filepath.Join(p.ctx.WorkDir, p.inlineEditFile)
	info, err := os.Stat(fullPath)
	if err != nil {
		return false // File doesn't exist or error, assume not modified
	}
	return info.ModTime().After(p.inlineEditOrigMtime)
}

// isInlineEditSessionAlive checks if the tmux session for inline editing still exists.
// Returns false if the session has ended (vim quit).
func (p *Plugin) isInlineEditSessionAlive() bool {
	if p.inlineEditSession == "" {
		return false
	}
	// Check if the tmux session exists using has-session
	err := exec.Command("tmux", "has-session", "-t", p.inlineEditSession).Run()
	return err == nil
}

// attachToInlineEditSession attaches to the inline edit tmux session in full-screen mode.
func (p *Plugin) attachToInlineEditSession() tea.Cmd {
	if p.inlineEditSession == "" {
		return nil
	}

	sessionName := p.inlineEditSession
	p.exitInlineEditMode()

	return func() tea.Msg {
		// Suspend the TUI and attach to tmux
		return AttachToTmuxMsg{SessionName: sessionName}
	}
}

// AttachToTmuxMsg requests the app to suspend and attach to a tmux session.
type AttachToTmuxMsg struct {
	SessionName string
}

// calculateInlineEditorWidth returns the content width for the inline editor.
// Must stay in sync with renderNormalPanes() preview width calculation.
func (p *Plugin) calculateInlineEditorWidth() int {
	if !p.treeVisible {
		return p.width - 4 // borders + padding (panelOverhead)
	}
	p.calculatePaneWidths()
	return p.previewWidth - 4 // borders + padding
}

// calculateInlineEditorHeight returns the content height for the inline editor.
// Account for pane borders, header lines, and tab line.
func (p *Plugin) calculateInlineEditorHeight() int {
	paneHeight := p.height
	if paneHeight < 4 {
		paneHeight = 4
	}
	innerHeight := paneHeight - 2 // pane borders

	// Subtract header lines (matches renderInlineEditorContent)
	contentHeight := innerHeight - 2 // header + empty line
	if len(p.tabs) > 1 {
		contentHeight-- // tab line
	}

	if contentHeight < 5 {
		contentHeight = 5
	}
	return contentHeight
}

// isInlineEditSupported checks if inline editing can be used for the given file.
func (p *Plugin) isInlineEditSupported(path string) bool {
	// Check feature flag
	if !features.IsEnabled(features.TmuxInlineEdit.Name) {
		return false
	}

	// Check if tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		return false
	}

	// Don't support inline editing for binary files
	if p.isBinary {
		return false
	}

	return true
}

// renderInlineEditorContent renders the inline editor within the preview pane area.
// This is called from renderPreviewPane() when inline edit mode is active.
func (p *Plugin) renderInlineEditorContent(visibleHeight int) string {
	// If showing exit confirmation, render that instead
	if p.showExitConfirmation {
		return p.renderExitConfirmation(visibleHeight)
	}

	var sb strings.Builder

	// Tab line (to match normal preview rendering)
	if len(p.tabs) > 1 {
		tabLine := p.renderPreviewTabs(p.previewWidth - 4)
		sb.WriteString(tabLine)
		sb.WriteString("\n")
	}

	// Header with file being edited and exit hint
	fileName := filepath.Base(p.inlineEditFile)
	header := fmt.Sprintf("Editing: %s", fileName)
	sb.WriteString(styles.Title.Render(header))
	sb.WriteString("  ")
	sb.WriteString(styles.Muted.Render("(Ctrl+\\ or ESC ESC to exit)"))
	sb.WriteString("\n")

	// Calculate content height (account for tab line and header)
	contentHeight := visibleHeight
	if len(p.tabs) > 1 {
		contentHeight-- // tab line
	}
	contentHeight -= 2 // header + empty line

	// Render terminal content from tty model
	if p.inlineEditor != nil {
		content := p.inlineEditor.View()
		lines := strings.Split(content, "\n")

		// Limit to content height
		if len(lines) > contentHeight {
			lines = lines[:contentHeight]
		}

		sb.WriteString(strings.Join(lines, "\n"))
	}

	return sb.String()
}

// renderExitConfirmation renders the exit confirmation dialog overlay.
func (p *Plugin) renderExitConfirmation(visibleHeight int) string {
	options := []string{"Save & Exit", "Exit without saving", "Cancel"}

	var sb strings.Builder

	// Tab line (keep consistent with editor view)
	if len(p.tabs) > 1 {
		tabLine := p.renderPreviewTabs(p.previewWidth - 4)
		sb.WriteString(tabLine)
		sb.WriteString("\n")
	}

	sb.WriteString(styles.Title.Render("Exit editor?"))
	sb.WriteString("\n\n")

	for i, opt := range options {
		if i == p.exitConfirmSelection {
			sb.WriteString(styles.ListItemSelected.Render("> " + opt))
		} else {
			sb.WriteString("  " + opt)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styles.Muted.Render("[j/k to select, Enter to confirm, Esc to cancel]"))

	return sb.String()
}

// normalizeEditorName extracts the base editor name from a command string.
// Handles paths like /usr/bin/vim, aliases like nvim, and arguments.
func normalizeEditorName(editor string) string {
	// Get base name (handles /usr/bin/vim -> vim)
	base := filepath.Base(editor)

	// Remove common suffixes/variations
	base = strings.TrimSuffix(base, ".exe")

	// Handle common aliases
	switch base {
	case "nvim", "neovim":
		return "vim"
	case "vi":
		return "vim"
	case "hx":
		return "helix"
	case "kak":
		return "kakoune"
	case "emacsclient":
		return "emacs"
	}

	return base
}

// sendEditorSaveAndQuit sends the appropriate save-and-quit key sequence for the editor.
// Returns true if a known editor sequence was sent, false for unknown editors.
func sendEditorSaveAndQuit(target, editor string) bool {
	normalized := normalizeEditorName(editor)

	send := func(keys ...string) {
		for _, k := range keys {
			exec.Command("tmux", "send-keys", "-t", target, k).Run()
		}
	}

	switch normalized {
	case "vim":
		// vim/nvim/vi: Escape to normal mode, :wq to save and quit
		send("Escape", ":wq", "Enter")
		return true

	case "nano":
		// nano: Ctrl+O to write, Enter to confirm, Ctrl+X to exit
		send("C-o", "Enter", "C-x")
		return true

	case "emacs":
		// emacs: Ctrl+X Ctrl+S to save, Ctrl+X Ctrl+C to quit
		send("C-x", "C-s", "C-x", "C-c")
		return true

	case "helix":
		// helix: Escape to normal mode, :wq to save and quit (vim-like)
		send("Escape", ":wq", "Enter")
		return true

	case "micro":
		// micro: Ctrl+S to save, Ctrl+Q to quit
		send("C-s", "C-q")
		return true

	case "kakoune":
		// kakoune: Escape to normal mode, :write-quit
		send("Escape", ":write-quit", "Enter")
		return true

	case "joe":
		// joe: Ctrl+K X to save and exit
		send("C-k", "x")
		return true

	case "ne":
		// ne (nice editor): Escape, then save command, then exit
		send("Escape", "Escape", ":s", "Enter", ":q", "Enter")
		return true

	case "amp":
		// amp: similar to vim
		send("Escape", ":wq", "Enter")
		return true

	default:
		// Unknown editor - don't attempt to send commands
		return false
	}
}

// handleExitConfirmationChoice processes the user's selection in the exit confirmation dialog.
func (p *Plugin) handleExitConfirmationChoice() (*Plugin, tea.Cmd) {
	p.showExitConfirmation = false

	switch p.exitConfirmSelection {
	case 0: // Save & Exit
		target := p.inlineEditSession
		editor := p.inlineEditEditor

		// Try to send editor-specific save-and-quit commands
		// If unknown editor, we still proceed but skip the save attempt
		sendEditorSaveAndQuit(target, editor)

		// Give editor a moment to process, then kill session
		// (Session may already be dead from quit command, kill-session will fail silently)
		p.exitInlineEditMode()
		return p.processPendingClickAction()

	case 1: // Exit without saving
		// Kill session immediately, then process pending action
		p.exitInlineEditMode()
		return p.processPendingClickAction()

	case 2: // Cancel
		p.pendingClickRegion = ""
		p.pendingClickData = nil
		return p, nil
	}

	return p, nil
}

// processPendingClickAction handles the click that triggered exit confirmation.
func (p *Plugin) processPendingClickAction() (*Plugin, tea.Cmd) {
	region := p.pendingClickRegion
	data := p.pendingClickData

	// Clear pending state
	p.pendingClickRegion = ""
	p.pendingClickData = nil

	switch region {
	case "tree-item":
		// User clicked a tree item - select it
		if idx, ok := data.(int); ok {
			return p.selectTreeItem(idx)
		}
		// Fallback: if data is missing, load preview for current selection
		return p, p.loadCurrentTreeItemPreview()
	case "tree-pane":
		// User clicked tree pane background - focus tree and refresh preview
		p.activePane = PaneTree
		return p, p.loadCurrentTreeItemPreview()
	case "preview-tab":
		// User clicked a tab - switch to it using switchTab to trigger edit state restoration
		if idx, ok := data.(int); ok {
			return p, p.switchTab(idx)
		} else if len(p.tabs) > 1 {
			// Fallback: switch to a different tab than current
			newTab := 0
			if p.activeTab == 0 {
				newTab = 1
			}
			return p, p.switchTab(newTab)
		}
	}

	return p, nil
}

// loadCurrentTreeItemPreview returns a Cmd to load the preview for the currently selected tree item.
func (p *Plugin) loadCurrentTreeItemPreview() tea.Cmd {
	if p.tree == nil || p.treeCursor < 0 || p.treeCursor >= p.tree.Len() {
		return nil
	}
	node := p.tree.GetNode(p.treeCursor)
	if node == nil || node.IsDir {
		return nil
	}
	// Update previewFile so PreviewLoadedMsg is accepted
	p.previewFile = node.Path
	return LoadPreview(p.ctx.WorkDir, node.Path, p.ctx.Epoch)
}

// calculateInlineEditorMouseCoords converts screen coordinates to editor-relative coordinates.
// Returns (col, row, ok) where col and row are 1-indexed for SGR mouse protocol.
// Returns ok=false if the coordinates are outside the editor content area.
func (p *Plugin) calculateInlineEditorMouseCoords(x, y int) (col, row int, ok bool) {
	if p.width <= 0 || p.height <= 0 {
		return 0, 0, false
	}

	// Calculate preview pane X offset
	var previewX int
	if p.treeVisible {
		p.calculatePaneWidths()
		previewX = p.treeWidth + dividerWidth
	}

	// Content X offset: preview pane start + border(1) + padding(1)
	contentX := previewX + 2

	// Calculate Y offset based on input bars and pane structure
	contentY := 0

	// Account for input bars (content search, file op, line jump)
	if p.contentSearchMode || p.fileOpMode != FileOpNone || p.lineJumpMode {
		contentY++
		if p.fileOpMode != FileOpNone && p.fileOpError != "" {
			contentY++ // error line
		}
	}

	// Add pane border (top)
	contentY++

	// Add tab line if multiple tabs
	if len(p.tabs) > 1 {
		contentY++
	}

	// Add header line ("Editing: filename...")
	contentY++

	// Calculate relative coordinates
	relX := x - contentX
	relY := y - contentY

	if relX < 0 || relY < 0 {
		return 0, 0, false
	}

	// Validate bounds against editor dimensions
	editorWidth := p.calculateInlineEditorWidth()
	editorHeight := p.calculateInlineEditorHeight()

	if relX >= editorWidth || relY >= editorHeight {
		return 0, 0, false
	}

	// SGR mouse protocol uses 1-indexed coordinates
	return relX + 1, relY + 1, true
}

// forwardMousePressToInlineEditor sends a mouse press event to the inline editor.
// col and row are 1-indexed coordinates relative to the editor content area.
func (p *Plugin) forwardMousePressToInlineEditor(col, row int) tea.Cmd {
	if p.inlineEditor == nil || !p.inlineEditor.IsActive() {
		return nil
	}
	if p.inlineEditSession == "" {
		return nil
	}

	sessionName := p.inlineEditSession
	return func() tea.Msg {
		// Send SGR mouse press (button 0 = left button)
		if err := tty.SendSGRMouse(sessionName, 0, col, row, false); err != nil {
			if tty.IsSessionDeadError(err) {
				return tty.SessionDeadMsg{}
			}
		}
		return nil
	}
}

// forwardMouseDragToInlineEditor sends a mouse drag/motion event to the inline editor.
// col and row are 1-indexed coordinates relative to the editor content area.
func (p *Plugin) forwardMouseDragToInlineEditor(col, row int) tea.Cmd {
	if p.inlineEditor == nil || !p.inlineEditor.IsActive() {
		return nil
	}
	if p.inlineEditSession == "" {
		return nil
	}

	sessionName := p.inlineEditSession
	return func() tea.Msg {
		// Send SGR mouse motion with button held (button 32 = motion + left button)
		if err := tty.SendSGRMouse(sessionName, 32, col, row, false); err != nil {
			if tty.IsSessionDeadError(err) {
				return tty.SessionDeadMsg{}
			}
		}
		return nil
	}
}

// forwardMouseReleaseToInlineEditor sends a mouse release event to the inline editor.
// col and row are 1-indexed coordinates relative to the editor content area.
func (p *Plugin) forwardMouseReleaseToInlineEditor(col, row int) tea.Cmd {
	if p.inlineEditor == nil || !p.inlineEditor.IsActive() {
		return nil
	}
	if p.inlineEditSession == "" {
		return nil
	}

	sessionName := p.inlineEditSession
	return func() tea.Msg {
		// Send SGR mouse release (button 0 = left button, release=true)
		if err := tty.SendSGRMouse(sessionName, 0, col, row, true); err != nil {
			if tty.IsSessionDeadError(err) {
				return tty.SessionDeadMsg{}
			}
		}
		return nil
	}
}

// isSessionAlive checks if a tmux session exists.
func isSessionAlive(sessionName string) bool {
	if sessionName == "" {
		return false
	}
	err := exec.Command("tmux", "has-session", "-t", sessionName).Run()
	return err == nil
}

// killSession kills a tmux session by name.
func killSession(sessionName string) {
	if sessionName == "" {
		return
	}
	_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
}

// selectTreeItem selects the given tree item and loads its preview.
func (p *Plugin) selectTreeItem(idx int) (*Plugin, tea.Cmd) {
	if idx < 0 || idx >= p.tree.Len() {
		return p, nil
	}

	p.treeCursor = idx
	p.ensureTreeCursorVisible()
	p.activePane = PaneTree

	node := p.tree.GetNode(idx)
	if node == nil || node.IsDir {
		return p, nil
	}

	return p, LoadPreview(p.ctx.WorkDir, node.Path, p.ctx.Epoch)
}


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
		cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName,
			"-x", "80", "-y", "24", editor, fullPath)
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
	case "tree-pane":
		// User clicked tree pane background - focus tree
		p.activePane = PaneTree
		return p, nil
	case "preview-tab":
		// User clicked a tab - switch to it
		if idx, ok := data.(int); ok {
			p.activeTab = idx
			if idx < len(p.tabs) {
				return p, LoadPreview(p.ctx.WorkDir, p.tabs[idx].Path)
			}
		}
	}

	return p, nil
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

	return p, LoadPreview(p.ctx.WorkDir, node.Path)
}


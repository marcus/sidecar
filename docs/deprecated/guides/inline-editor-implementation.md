# Inline Editor Implementation Guide

## Overview

The inline editor feature (`tmux_inline_edit`) allows users to edit files directly within the Sidecar preview pane using their preferred terminal editor (vim, nvim, etc.) without leaving the TUI. The file tree remains visible during editing, providing context and enabling navigation.

**Core Principle**: This is NOT a terminal emulator. Tmux manages the PTY backend; Sidecar acts as an input/output relay, similar to the workspace plugin's interactive mode.

## Architecture

### Components

1. **Entry Layer** (`inline_edit.go`): Creates tmux sessions and manages editor lifecycle
2. **Rendering Layer** (`view.go`, `inline_edit.go`): Renders editor content within preview pane
3. **Input Layer** (`plugin.go`, `mouse.go`): Routes keys/clicks to editor or confirmation dialog
4. **TTY Model** (`internal/tty/tty.go`): Handles tmux capture, cursor overlay, and input forwarding

### Data Flow

```
User presses 'e' on file
    → enterInlineEditMode()
    → tmux new-session -d -s {sessionName} {editor} {path}
    → InlineEditStartedMsg
    → handleInlineEditStarted()
    → tty.Model.Enter()
    → Start polling tmux capture-pane
    → renderInlineEditorContent() in preview pane
    → User types → tty.Model forwards to tmux
    → User exits → SessionDeadMsg or exit keys
    → exitInlineEditMode()
    → Refresh preview
```

## Critical Implementation Details

### 1. Preview Pane Rendering (Not Full-Screen)

**Key Pattern**: The editor renders within `renderPreviewPane()`, not as a full-screen takeover.

```go
// view.go - renderPreviewPane()
func (p *Plugin) renderPreviewPane(visibleHeight int) string {
    // Handle inline edit mode - render editor within preview pane
    if p.inlineEditMode && p.inlineEditor != nil && p.inlineEditor.IsActive() {
        return p.renderInlineEditorContent(visibleHeight)
    }
    // ... normal preview rendering
}
```

**Why this matters**: The file tree remains visible, providing context and enabling click-away exit.

### 2. Dimension Calculations

The tty.Model needs exact dimensions matching the preview pane content area:

```go
// inline_edit.go
func (p *Plugin) calculateInlineEditorWidth() int {
    if !p.treeVisible {
        return p.width - 4 // borders + padding
    }
    p.calculatePaneWidths()
    return p.previewWidth - 4
}

func (p *Plugin) calculateInlineEditorHeight() int {
    paneHeight := p.height
    innerHeight := paneHeight - 2 // pane borders
    contentHeight := innerHeight - 2 // header lines
    if len(p.tabs) > 1 {
        contentHeight-- // tab line
    }
    return contentHeight
}
```

**Critical**: These must stay in sync with `renderInlineEditorContent()` layout calculations.

### 3. Confirmation Behavior

The implementation **always shows confirmation when the session is alive**, regardless of file modification status. This design choice was made because vim's modification status cannot be reliably detected externally.

**Key insight**: While mtime tracking was considered, it proved unreliable because:
1. Vim may not write to disk until `:w` is explicitly called
2. Auto-save plugins and swap files complicate detection
3. External changes to the file can't be distinguished from editor changes

The simple rule is: **session alive = show confirmation, session dead = exit immediately**.

### 3b. Session Alive Detection

When vim exits via `:wq`, the tmux session dies. However, the tty model's `SessionDeadMsg` may not arrive before the user clicks away. We check if the tmux session is still alive before showing confirmation:

```go
// isInlineEditSessionAlive() checks if the tmux session still exists
func (p *Plugin) isInlineEditSessionAlive() bool {
    if p.inlineEditSession == "" {
        return false
    }
    // Check if the tmux session exists using has-session
    err := exec.Command("tmux", "has-session", "-t", p.inlineEditSession).Run()
    return err == nil
}
```

This is checked:
1. At the start of Update() when in inline edit mode - if session is dead, exit immediately
2. In click-away handling - if session is dead, skip confirmation and clean up

**Why this matters**: Without this check, users would see "Exit editor?" confirmation after vim had already exited, which is confusing.

### 4. Exit Confirmation Dialog

When users click away from the editor **and the session is still alive**, a confirmation dialog prevents accidental data loss:

```go
// State fields in plugin.go
showExitConfirmation bool        // Dialog visible
pendingClickRegion   string      // Where user clicked
pendingClickData     interface{} // Click data (tree index, tab index)
exitConfirmSelection int         // 0=Save&Exit, 1=Exit without saving, 2=Cancel
```

**Options**:
- **Save & Exit**: Sends `Escape :wq Enter` to vim, waits for session death
- **Exit without saving**: Kills tmux session immediately
- **Cancel**: Returns to editing

### 5. Click-Away Detection

```go
// mouse.go - handleMouse()
if p.inlineEditMode && p.inlineEditor != nil && p.inlineEditor.IsActive() {
    action := p.mouseHandler.HandleMouse(msg)

    handleClickAway := func(regionID string, regionData interface{}) (*Plugin, tea.Cmd) {
        // Check if session is still alive (vim may have exited via :wq)
        if !p.isInlineEditSessionAlive() {
            // Session is dead - just clean up and process click
            p.exitInlineEditMode()
            p.pendingClickRegion = regionID
            p.pendingClickData = regionData
            return p.processPendingClickAction()
        }

        // Session alive - always show confirmation
        p.pendingClickRegion = regionID
        p.pendingClickData = regionData
        p.showExitConfirmation = true
        p.exitConfirmSelection = 0
        return p, nil
    }

    if action.Type == mouse.ActionClick {
        // Check registered regions
        if action.Region != nil {
            switch action.Region.ID {
            case regionTreePane, regionTreeItem, regionPreviewTab:
                return handleClickAway(action.Region.ID, action.Region.Data)
            }
        }

        // Fallback: position-based detection
        if p.treeVisible && action.X < p.treeWidth {
            return handleClickAway(regionTreePane, nil)
        }
    }

    // Forward to tty model
    return p, p.inlineEditor.Update(msg)
}
```

**Why position-based fallback**: Mouse regions are registered during render. Clicks between specific items may not hit a region, so we fall back to X-position checking.

### 6. Gradient Border Feedback

Visual indicator that edit mode is active:

```go
// view.go - renderNormalPanes()
if p.inlineEditMode && p.inlineEditor != nil && p.inlineEditor.IsActive() {
    rightPane = styles.RenderPanelWithGradient(previewContent, p.previewWidth,
        paneHeight, styles.GetInteractiveGradient())
} else {
    rightPane = styles.RenderPanel(previewContent, p.previewWidth,
        paneHeight, previewActive)
}
```

### 7. Mouse Support (SGR Protocol)

The inline editor supports full mouse interaction including text selection via the SGR (1006) mouse protocol. This enables:
- Click to position cursor
- Click and drag to select text
- Scroll wheel support

**Implementation**: Mouse events are forwarded to the tty model which translates them into SGR escape sequences for the editor:

```go
// Press, drag, and release events are all forwarded
case tea.MouseMsg:
    if p.inlineEditMode && p.inlineEditor != nil && p.inlineEditor.IsActive() {
        // Forward mouse events to editor (press, drag, release)
        return p, p.inlineEditor.Update(msg)
    }
```

The tty model handles SGR encoding: `\x1b[<button;x;y;M/m` where `M` = press/drag, `m` = release.

### 8. Multi-Editor Support

The `sendEditorSaveAndQuit()` function supports multiple terminal editors, detecting which editor is running and sending the appropriate save-and-quit sequence:

| Editor | Save & Quit Command |
|--------|---------------------|
| vim, nvim, vi | `Escape :wq Enter` |
| nano | `Ctrl+O Enter Ctrl+X` |
| emacs | `Ctrl+X Ctrl+S Ctrl+X Ctrl+C` |
| helix | `Escape :wq Enter` |
| micro | `Ctrl+S Ctrl+Q` |
| kakoune | `Escape :write-quit Enter` |
| joe | `Ctrl+K X` |
| ne | `Escape :SaveQuit Enter` |
| amp | `Ctrl+S Ctrl+Q` |

Editor detection is done by checking the running process in the tmux session.

## Exit Paths

| Method | Confirmation | Description |
|--------|--------------|-------------|
| `Ctrl+\` | No | Immediate exit (tty.Config.ExitKey) |
| Double-ESC | No | Exit with 150ms delay (vim ESC compatibility) |
| `:q`, `:wq` in vim | No | Normal editor exit, session death detected |
| Click tree/tab | Yes (if session alive) | Always shows confirmation when session is alive; exits immediately if session already dead |

## State Management

### Plugin State (plugin.go)

```go
// Inline editor state
inlineEditor         *tty.Model // Embeddable tty model
inlineEditMode       bool       // Currently editing
inlineEditSession    string     // Tmux session name
inlineEditFile       string     // File being edited

// Exit confirmation state
showExitConfirmation bool
pendingClickRegion   string
pendingClickData     interface{}
exitConfirmSelection int
```

### Update Priority (plugin.go)

```go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    // 1. Handle exit confirmation dialog FIRST
    if p.showExitConfirmation {
        // j/k navigation, Enter confirm, Esc cancel
    }

    // 2. Handle inline edit mode
    if p.inlineEditMode && p.inlineEditor.IsActive() {
        // Delegate to tty.Model
    }

    // 3. Normal plugin handling
}
```

## Common Pitfalls

### Don't Render Full-Screen

The old implementation took over the entire view:

```go
// WRONG - hides file tree
func (p *Plugin) renderView() string {
    if p.inlineEditMode {
        return p.renderInlineEditView() // Full-screen takeover
    }
}
```

The correct approach renders within the preview pane only.

### Don't Forget Position-Based Click Detection

Mouse regions may not cover every pixel. Always include fallback:

```go
// Without fallback, clicks between tree items are ignored
if p.treeVisible && action.X < p.treeWidth {
    // Handle as tree pane click
}
```

### Don't Skip Confirmation for Click-Away

Direct exits (Ctrl+\, :q) don't need confirmation - the user explicitly chose to exit. But click-away should always confirm to prevent accidental data loss.

### Don't Forget to Check IsActive() on Every Message

The tty model can become inactive asynchronously (when vim exits normally). Check at the start of inline edit mode handling:

```go
// plugin.go - Update()
if p.inlineEditMode && p.inlineEditor != nil {
    // Check if editor became inactive (vim exited normally)
    if !p.inlineEditor.IsActive() {
        editedFile := p.inlineEditFile
        p.exitInlineEditMode()
        return p, LoadPreview(p.ctx.WorkDir, editedFile)
    }
    // ... rest of inline edit handling
}
```

Without this check, the user gets a blank screen after vim exits normally.

### Don't Block on Subprocess in View()

The tty.Model handles async polling. Never query tmux synchronously during render:

```go
// WRONG
func (p *Plugin) renderInlineEditorContent() string {
    output, _ := exec.Command("tmux", "capture-pane", ...).Output() // Blocks!
}

// RIGHT - use cached content from tty.Model
func (p *Plugin) renderInlineEditorContent() string {
    content := p.inlineEditor.View() // Returns cached content
}
```

### Don't Forget to Handle Tab Row Clicks Separately

Tab row clicks need position-based detection BEFORE region-based detection. The tab row is at a specific Y position and must be checked first:

```go
// Check tab row clicks by Y position first
if len(p.tabs) > 1 && action.Y == tabRowY {
    // Handle as tab click using X position to determine which tab
}

// Then fall back to registered regions for other clicks
if action.Region != nil {
    // Handle region-based clicks
}
```

Without this ordering, tab clicks may be incorrectly handled as preview pane clicks.

### Don't Forget App-Level Key Routing

The app intercepts global shortcuts (like `q`, `1-5`, `` ` ``, `~`, `?`, `!`, `@`) before they reach plugins. For inline edit mode to receive ALL keys, the context must be handled specially in `internal/app/update.go`:

```go
// update.go - must include file-browser-inline-edit alongside workspace-interactive
if m.activeContext == "workspace-interactive" || m.activeContext == "file-browser-inline-edit" {
    // Forward ALL keys to plugin (exit keys and ctrl+c handled by plugin)
    if p := m.ActivePlugin(); p != nil {
        newPlugin, cmd := p.Update(msg)
        // ...
    }
}
```

Without this, typing `q` in vim triggers the quit confirmation instead of inserting the character. The plugin correctly returns `"file-browser-inline-edit"` from `FocusContext()`, but the app must recognize this context for full key forwarding.

## Keyboard Shortcuts

| Key | Command | Description |
|-----|---------|-------------|
| `e` | `edit` | Edit file inline (within preview pane) |
| `E` | `edit-external` | Edit in full terminal (suspends TUI, full vim experience) |

Both shortcuts work in tree and preview panes. The `E` shortcut is useful when inline editing isn't working well or when you need the full terminal editor experience.

These are registered in:
- `internal/plugins/filebrowser/plugin.go` - `Commands()` method (for footer hint and command palette)
- `internal/plugins/filebrowser/handlers.go` - Key handling in tree and preview handlers
- `internal/keymap/bindings.go` - Key bindings for `file-browser-tree` and `file-browser-preview` contexts

## Feature Flag

Gated behind `tmux_inline_edit`:

```go
if !features.IsEnabled(features.TmuxInlineEdit.Name) {
    return p.openFile(path) // Fall back to external editor
}
```

Enable in `~/.config/sidecar/config.json`:

```json
{
  "features": {
    "tmux_inline_edit": true
  }
}
```

## Files to Know

| File | Purpose |
|------|---------|
| `internal/plugins/filebrowser/inline_edit.go` | Editor lifecycle, confirmation dialog, dimension calculations |
| `internal/plugins/filebrowser/view.go` | Preview pane rendering, gradient border |
| `internal/plugins/filebrowser/mouse.go` | Click-away detection |
| `internal/plugins/filebrowser/plugin.go` | State management, Update routing |
| `internal/tty/tty.go` | TTY model for tmux interaction (shared with workspace) |
| `internal/app/update.go` | App-level key routing (must forward ALL keys for inline edit context) |

## Testing Checklist

1. **Basic functionality**:
   - [ ] Press `e` on a file to enter inline edit mode
   - [ ] File tree remains visible on the left
   - [ ] Gradient border appears on preview pane
   - [ ] Header shows "Editing: {filename}"
   - [ ] Press `E` (shift+e) opens file in full terminal editor (TUI suspends)

2. **Exit scenarios**:
   - [ ] `Ctrl+\` exits immediately
   - [ ] Double-ESC exits immediately
   - [ ] `:wq` in vim saves and exits
   - [ ] `:q!` in vim exits without saving
   - [ ] Click on file tree (session alive) shows confirmation dialog
   - [ ] Click on file tree (session dead) exits immediately without confirmation

3. **Confirmation behavior**:
   - [ ] Open file with `e`, click away while session is alive → always shows confirmation
   - [ ] Open file with `e`, exit with `:wq`, click away → exits immediately (session dead)

4. **Confirmation dialog**:
   - [ ] j/k navigates options
   - [ ] Enter confirms selected option
   - [ ] Esc cancels and returns to editing
   - [ ] "Save & Exit" saves file then processes click
   - [ ] "Exit without saving" discards and processes click

5. **Edge cases**:
   - [ ] Window resize updates editor dimensions
   - [ ] Multiple tabs: clicking different tab triggers confirmation (if session alive)
   - [ ] Binary files don't allow inline edit (falls back to external)

6. **Global shortcuts bypassed** (keys reach vim, not app):
   - [ ] `q` types 'q' in vim (doesn't trigger quit)
   - [ ] `1-5` types numbers in vim (doesn't switch plugins)
   - [ ] `` ` `` and `~` type characters in vim (doesn't switch plugins)
   - [ ] `?` types '?' in vim (doesn't open command palette)
   - [ ] `!` types '!' in vim (doesn't open diagnostics)
   - [ ] `@` types '@' in vim (doesn't open project switcher)

## References

- Original interactive shell guide: `docs/deprecated/guides/interactive-shell-implementation.md`
- TTY model implementation: `internal/tty/tty.go`
- Feature flag system: `internal/features/features.go`
- Related task: td-284383 (epic), td-b520d0d7 (original implementation)

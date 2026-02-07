# Interactive Shell Implementation Guide

## Overview

The interactive shell feature allows users to type directly into tmux sessions from within the Sidecar UI without suspending the TUI. Sidecar acts as a "transparent proxy" that forwards keypresses to tmux and displays captured output with a live cursor overlay.

**Core Principle**: This is NOT a terminal emulator. Tmux remains the PTY backend; Sidecar acts as an input/output relay.

## Architecture

### Package Structure

The interactive mode functionality is split between a shared `tty` package and plugin-specific code:

```
internal/tty/              # Shared tmux terminal abstraction
├── tty.go                 # Core Model and State types
├── keymap.go              # Bubble Tea → tmux key translation
├── messages.go            # Message types (CaptureResultMsg, PollTickMsg, etc.)
├── session.go             # tmux operations (send-keys, capture-pane, resize)
├── polling.go             # Polling interval constants and calculation
├── cursor.go              # Cursor rendering and position query
├── paste.go               # Paste handling (clipboard, bracketed paste)
├── terminal_mode.go       # Terminal mode detection (mouse, bracketed paste)
└── output_buffer.go       # Thread-safe output buffer with hash-based change detection

internal/plugins/workspace/
├── interactive.go         # Workspace-specific interactive mode logic
├── interactive_selection.go  # Text selection in interactive mode
├── view_preview.go        # Rendering with cursor overlay and scroll offset
├── mouse.go               # Scroll handling
└── types.go               # InteractiveState type

internal/plugins/filebrowser/
├── inline_edit.go         # Inline editor mode using tty.Model
└── handlers.go            # Message handling for inline edit
```

### Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| `tty.Model` | Embeddable component for interactive tmux sessions |
| `tty.State` | Tracks cursor, terminal modes, poll generation |
| `tty.OutputBuffer` | Thread-safe buffer with hash-based change detection |
| `tty.MapKeyToTmux()` | Translates Bubble Tea keys to tmux send-keys format |
| Workspace `interactive.go` | Plugin-specific key handling, selection, configuration |
| Workspace `view_preview.go` | Renders captured output with cursor and scroll offset |
| Filebrowser `inline_edit.go` | Uses `tty.Model` for vim/nano editing |

### Data Flow

```
User Keypress → handleInteractiveKeys()
              → tty.MapKeyToTmux()
              → tmux send-keys
              → schedulePoll(20ms debounce)
              → capture-pane + cursor query
              → CaptureResultMsg
              → OutputBuffer.Update()
              → pollInteractivePane() (adaptive 50-250ms)
              → renderWithCursor()
```

## Core Abstractions

### tty.Model

The `tty.Model` is an embeddable component that plugins can use for interactive tmux functionality:

```go
type Model struct {
    Config Config        // Exit key, copy/paste keys, scrollback lines
    State  *State        // Current interactive state
    Width  int           // Display width
    Height int           // Display height
    OnExit   func() tea.Cmd  // Callback when user exits
    OnAttach func() tea.Cmd  // Callback for full tmux attach
}

// Usage in filebrowser plugin:
p.inlineEditor = tty.New(&tty.Config{
    ExitKey: "ctrl+\\",
    ScrollbackLines: 600,
})
p.inlineEditor.OnExit = func() tea.Cmd { ... }
cmd := p.inlineEditor.Enter(sessionName, paneID)
```

### tty.State

Tracks all interactive session state:

```go
type State struct {
    Active        bool      // Interactive mode active?
    TargetPane    string    // tmux pane ID (e.g., "%12")
    TargetSession string    // tmux session name
    LastKeyTime   time.Time // For polling decay calculation

    // Cursor state (updated from CaptureResultMsg)
    CursorRow, CursorCol int
    CursorVisible        bool
    PaneHeight, PaneWidth int

    // Terminal mode detection
    BracketedPasteEnabled bool
    MouseReportingEnabled bool

    // Output buffer
    OutputBuf      *OutputBuffer
    PollGeneration int  // For invalidating stale polls
}
```

### tty.OutputBuffer

Thread-safe bounded buffer with efficient change detection:

```go
// Hash-based change detection skips processing when content unchanged
func (b *OutputBuffer) Update(content string) bool {
    rawHash := maphash.String(seed, content)
    if rawHash == b.lastRawHash { return false }  // Skip ALL processing

    // Only if changed: strip mouse sequences, split lines
    content = mouseEscapeRegex.ReplaceAllString(content, "")
    b.lines = strings.Split(content, "\n")
    return true
}

// Efficient range access for scrolling
func (b *OutputBuffer) LinesRange(start, end int) []string
```

## Scrolling Implementation

Scrolling operates entirely on the already-captured buffer. No tmux copy-mode is involved.

### Scroll State

The workspace plugin tracks scroll position with two fields:

```go
// plugin.go
type Plugin struct {
    previewOffset    int   // Lines from bottom (0 = at bottom/live)
    autoScrollOutput bool  // Auto-follow new output?
}
```

### Scroll Behavior

```go
// interactive.go
func (p *Plugin) forwardScrollToTmux(delta int) tea.Cmd {
    if delta < 0 {
        // Scroll UP: pause auto-scroll, show older content
        p.autoScrollOutput = false
        p.previewOffset++
    } else {
        // Scroll DOWN: show newer content
        if p.previewOffset > 0 {
            p.previewOffset--
            if p.previewOffset == 0 {
                p.autoScrollOutput = true // Resume at bottom
            }
        }
    }
    return nil
}
```

### Scroll Rendering

The `view_preview.go` calculates which lines to display:

```go
var start, end int
if p.autoScrollOutput {
    // Auto-scroll: show newest content (last visibleHeight lines)
    start = effectiveLineCount - visibleHeight
    end = effectiveLineCount
} else {
    // Manual scroll: previewOffset is lines from bottom
    start = effectiveLineCount - visibleHeight - p.previewOffset
    end = start + visibleHeight
}

// Get only the lines we need (avoids copying entire buffer)
lines := wt.Agent.OutputBuf.LinesRange(start, end)
```

### Scroll Characteristics

- **No subprocess calls**: Scrolling is pure state manipulation and rendering
- **Instant response**: No waiting for tmux commands
- **Bounded by capture**: Can only scroll within the captured scrollback (default 600 lines)
- **Auto-resume**: Scrolling to bottom (previewOffset=0) re-enables auto-scroll

### Current Scroll Limitations

1. **Single line per scroll event**: Each mouse wheel tick scrolls one line. Fast scrolling requires many events.
2. **No page up/down in interactive mode**: Page keys are forwarded to tmux, not used for preview scrolling.
3. **No scroll indicator**: No visual indicator showing position within scrollback.
4. **Fixed scrollback**: Cannot scroll beyond the 600-line capture window.

## Cursor Positioning

### Cursor Query

Cursor position is captured atomically with output:

```go
// cursor.go
func QueryCursorPositionSync(target string) (row, col, paneHeight, paneWidth int, visible, ok bool) {
    cmd := exec.Command("tmux", "display-message", "-t", target,
        "-p", "#{cursor_x},#{cursor_y},#{cursor_flag},#{pane_height},#{pane_width}")
    // Parse output...
}
```

### Cursor Rendering

```go
// cursor.go
func RenderWithCursor(content string, cursorRow, cursorCol int, visible bool) string {
    lines := strings.Split(content, "\n")
    line := lines[cursorRow]
    lineWidth := ansi.StringWidth(line)

    if cursorCol >= lineWidth {
        // Cursor past end: pad with spaces and render block
        padding := cursorCol - lineWidth
        lines[cursorRow] = line + strings.Repeat(" ", padding) + CursorStyle().Render("█")
    } else {
        // Cursor within line: ANSI-aware slicing
        before := ansi.Cut(line, 0, cursorCol)
        char := ansi.Cut(line, cursorCol, cursorCol+1)
        after := ansi.Cut(line, cursorCol+1, lineWidth)
        lines[cursorRow] = before + CursorStyle().Render(char) + after
    }
    return strings.Join(lines, "\n")
}
```

### Cursor Adjustment for Pane Height Mismatch

When display height differs from tmux pane height:

```go
// view_preview.go
relativeRow := cursorRow
if paneHeight > displayHeight {
    relativeRow = cursorRow - (paneHeight - displayHeight)
} else if paneHeight > 0 && paneHeight < displayHeight {
    relativeRow = cursorRow + (displayHeight - paneHeight)
}
```

## Polling and Performance

### Adaptive Polling Intervals

```go
// polling.go
const (
    PollingDecayFast   = 50 * time.Millisecond   // During active typing
    PollingDecayMedium = 200 * time.Millisecond  // After 2s inactivity
    PollingDecaySlow   = 250 * time.Millisecond  // After 10s inactivity
    KeystrokeDebounce  = 20 * time.Millisecond   // Delay after keystroke
)

func CalculatePollingInterval(lastActivityTime time.Time) time.Duration {
    inactivity := time.Since(lastActivityTime)
    if inactivity > 10*time.Second { return PollingDecaySlow }
    if inactivity > 2*time.Second { return PollingDecayMedium }
    return PollingDecayFast
}
```

### Three-State Visibility Polling (Workspace Plugin)

The workspace plugin uses additional polling states based on visibility:

| State | Active | Idle |
|-------|--------|------|
| Visible + focused | 200ms | 2s |
| Visible + unfocused | 500ms | 500ms |
| Not visible | 10-20s | 10-20s |

### Keystroke Debouncing

After each keystroke, polling is delayed by 20ms to batch rapid typing:

```go
// handleKey in tty.go
cmds = append(cmds, m.schedulePoll(KeystrokeDebounce))
```

This reduces subprocess spam by ~60% during fast typing.

### Poll Generation

Stale polls are invalidated using a generation counter:

```go
// tty.go
func (m *Model) schedulePoll(delay time.Duration) tea.Cmd {
    m.State.PollGeneration++
    gen := m.State.PollGeneration
    return tea.Tick(delay, func(t time.Time) tea.Msg {
        return PollTickMsg{Generation: gen}
    })
}

func (m *Model) handlePollTick(msg PollTickMsg) tea.Cmd {
    if msg.Generation != m.State.PollGeneration {
        return nil  // Skip stale poll
    }
    // ... proceed with capture
}
```

### Performance Characteristics

**Per keystroke (optimized)**:
1. `tmux send-keys` → subprocess (~10ms)
2. 20ms debounce delay
3. `tmux capture-pane` → subprocess (~5ms)
4. `tmux display-message` → cursor query (~5ms)
5. Hash check → O(n) string hash (~1ms for 600 lines)
6. Regex (only if changed) → O(n) pattern matching (~5ms)
7. Buffer split → O(n) string operations (~1ms)
8. Cursor overlay → O(1) ANSI-aware slicing (<1ms)

**Total**: ~42ms worst case, ~36ms typical

## Key Mapping

### Basic Keys

```go
// keymap.go
func MapKeyToTmux(msg tea.KeyMsg) (key string, useLiteral bool) {
    switch msg.Type {
    case tea.KeyEnter:    return "Enter", false
    case tea.KeyBackspace: return "BSpace", false
    case tea.KeyTab:      return "Tab", false
    case tea.KeyUp:       return "Up", false
    case tea.KeyCtrlC:    return "C-c", false
    case tea.KeyRunes:    return string(msg.Runes), true  // Literal mode
    }
}
```

### Modified Keys

Modified arrow keys use CSI sequences:

```go
case "shift+up":   return "\x1b[1;2A", true
case "ctrl+up":    return "\x1b[1;5A", true
case "alt+up":     return "\x1b[1;3A", true
case "shift+tab":  return "\x1b[Z", true
```

### Literal Mode

For printable characters, use `tmux send-keys -l` to prevent interpretation:

```go
// session.go
func SendLiteralToTmux(sessionName, text string) error {
    cmd := exec.Command("tmux", "send-keys", "-l", "-t", sessionName, text)
    return cmd.Run()
}
```

## Terminal Mode Detection

### Bracketed Paste

```go
// terminal_mode.go
func DetectBracketedPasteMode(output string) bool {
    enableIdx := strings.LastIndex(output, "\x1b[?2004h")   // Enable
    disableIdx := strings.LastIndex(output, "\x1b[?2004l") // Disable
    return enableIdx > disableIdx
}
```

### Mouse Reporting

```go
func DetectMouseReportingMode(output string) bool {
    // Check all mouse mode enable/disable sequences
    // Return true if latest enable > latest disable
}
```

## Copy/Paste

### Keyboard Shortcuts

- Copy: `alt+c` (configurable via `interactiveCopyKey`)
- Paste: `alt+v` (configurable via `interactivePasteKey`)

### Paste Implementation

```go
// paste.go
func PasteClipboardToTmuxCmd(sessionName string, bracketed bool) tea.Cmd {
    return func() tea.Msg {
        text, _ := clipboard.ReadAll()
        if bracketed {
            SendBracketedPasteToTmux(sessionName, text)
        } else {
            SendPasteToTmux(sessionName, text)
        }
        return PasteResultMsg{}
    }
}

// Bracketed paste wraps text in escape sequences
func SendBracketedPasteToTmux(sessionName, text string) error {
    SendLiteralToTmux(sessionName, "\x1b[200~")  // Start
    SendLiteralToTmux(sessionName, text)
    SendLiteralToTmux(sessionName, "\x1b[201~")  // End
}
```

## Inline Edit Mode (Filebrowser Plugin)

The filebrowser plugin uses `tty.Model` for inline editing:

```go
// inline_edit.go
func (p *Plugin) enterInlineEditMode(path string) tea.Cmd {
    editor := os.Getenv("EDITOR")  // vim, nano, etc.
    sessionName := fmt.Sprintf("sidecar-edit-%d", time.Now().UnixNano())

    // Create detached tmux session with editor
    exec.Command("tmux", "new-session", "-d", "-s", sessionName, editor, path).Run()

    return InlineEditStartedMsg{SessionName: sessionName, ...}
}

func (p *Plugin) handleInlineEditStarted(msg InlineEditStartedMsg) tea.Cmd {
    p.inlineEditor.OnExit = func() tea.Cmd { ... }
    return p.inlineEditor.Enter(msg.SessionName, "")
}
```

This provides vim/nano/emacs editing directly in the file preview pane.

## Horizontal Width Synchronization

### Background Resize

Tmux panes are resized in the background at all times (not just interactive mode):

```go
// session.go
func ResizeTmuxPane(paneID string, width, height int) {
    args := []string{"resize-window", "-t", paneID, "-x", width, "-y", height}
    if err := exec.Command("tmux", args...).Run(); err != nil {
        // Fallback to resize-pane for older tmux
        exec.Command("tmux", "resize-pane", "-t", paneID, "-x", width, "-y", height).Run()
    }
}
```

### Resize Triggers

- Window resize (`WindowSizeMsg`)
- Sidebar toggle/drag
- Selection change
- Agent/shell creation
- Interactive mode entry

### Dimension Calculation

```go
// workspace/interactive.go
func (p *Plugin) calculatePreviewDimensions() (width, height int) {
    if !p.sidebarVisible {
        width = p.width - panelOverhead
    } else {
        available := p.width - dividerWidth
        sidebarW := (available * p.sidebarWidth) / 100
        previewW := available - sidebarW
        width = previewW - panelOverhead
    }
    height = p.height - headerHeight - footerHeight
    return
}
```

## Common Pitfalls

### Don't Clear the OutputBuffer

```go
// BAD - breaks rendering
p.Agent.OutputBuf.Clear()
```

The hash-based change detection needs continuity. Clearing breaks the entire rendering pipeline.

### Don't Forget Poll Generation

When entering interactive mode, increment the generation counter:

```go
// In enterInteractiveMode():
if p.shellSelected {
    p.shellPollGeneration[sessionName]++
} else {
    p.pollGeneration[wt.Name]++
}
```

Without this, you get duplicate parallel poll chains causing 200% CPU usage.

### Don't Call Subprocesses from View()

Cursor queries and tmux operations must run asynchronously in poll handlers, not in the render path.

### Don't Mix Shell/Workspace Polling

- Shells use `scheduleShellPollByName()` and `shellPollGeneration`
- Workspaces use `scheduleAgentPoll()` and `pollGeneration`

## Feature Flags

### tmux_interactive_input (Workspace)

Enable in `~/.config/sidecar/config.json`:

```json
{
  "features": {
    "tmux_interactive_input": true
  }
}
```

### tmux_inline_edit (Filebrowser)

```json
{
  "features": {
    "tmux_inline_edit": true
  }
}
```

## Configuration

```json
{
  "plugins": {
    "workspace": {
      "interactiveExitKey": "ctrl+\\",
      "interactiveAttachKey": "ctrl+]",
      "interactiveCopyKey": "alt+c",
      "interactivePasteKey": "alt+v",
      "tmuxCaptureMaxBytes": 600
    }
  }
}
```

## Entry and Exit

**Workspace Plugin**:
- Enter: Press `i` when preview pane is focused with output tab visible
- Exit: `Ctrl+\` (instant) or double-Escape (150ms delay)
- Attach: `Ctrl+]` (exits interactive and attaches to full tmux session)

**Filebrowser Plugin**:
- Enter: Press `e` or `Enter` on a file (if inline edit enabled)
- Exit: `Ctrl+\` or double-Escape
- Attach: `Ctrl+]`

## Testing Interactive Mode

1. Start sidecar with `tmux_interactive_input` enabled
2. Create or select a workspace/shell
3. Focus preview pane, press `i`
4. Type commands: `ls`, `echo hello`, navigate with arrows
5. Verify:
   - Cursor appears on correct line
   - Output appears immediately after command completion
   - Scrolling works (mouse wheel, or run commands that produce output)
   - CPU usage stays reasonable during typing
   - Exit with `Ctrl+\` returns to list view

## Future Scroll Improvements

Potential improvements to scrolling:

1. **Faster scrolling**: Page up/down support or larger scroll increments
2. **Scroll indicator**: Show position within scrollback (e.g., "50/600")
3. **Scroll persistence**: Remember scroll position per session
4. **Keyboard scrolling**: Arrow keys for scrolling when not in interactive mode
5. **Scroll to search match**: Jump to lines matching a pattern

## References

- Original spec: `docs/spec-tmux-interactive-input.md`
- Related issues:
  - td-29f190: Buffer invalidation (reverted)
  - td-8a0978: Keystroke debouncing
  - td-15cc29: Hash optimization
  - td-380d89: Cursor adjustment
  - td-194689: Mouse escape regex strengthening
  - td-4218e8: Epic for all interactive mode fixes
  - td-97327e: Duplicate poll chain fix (200% CPU)

## Key Takeaways

1. **Two-layer architecture**: Shared `tty` package + plugin-specific code
2. **Cursor mapping is 0-indexed**: Keep pane-height padding and cursor-col padding
3. **Never clear OutputBuffer**: It breaks rendering
4. **Poll continuity is critical**: Interactive mode needs fast polling throughout
5. **Hash before regex**: Massive CPU savings when content unchanged
6. **Debouncing works**: 20ms delay reduces subprocess spam significantly
7. **Scrolling is buffer-based**: No tmux copy-mode, just offset into captured lines
8. **Width sync matters**: Resize panes in background at all times
9. **Atomic cursor capture**: Query cursor with output to avoid race conditions
10. **Separate shell/workspace polling**: Use correct scheduling function for each type
11. **Use correct generation maps**: Shells use `shellPollGeneration`, workspaces use `pollGeneration`
12. **Invalidate old poll chains**: Increment generation when entering interactive mode

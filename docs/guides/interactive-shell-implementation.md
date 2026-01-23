# Interactive Shell Implementation Guide

## Overview

The interactive shell feature (`tmux_interactive_input`) allows users to type directly into tmux sessions from within the Sidecar UI without suspending the TUI. This creates a "transparent proxy" where Sidecar forwards keypresses to tmux and displays the captured output with a live cursor overlay.

**Core Principle**: This is NOT a terminal emulator. Tmux remains the PTY backend; Sidecar acts as an input/output relay.

## Architecture

### Components

1. **Input Layer** (`interactive.go`): Captures Bubble Tea keypresses and translates them to tmux `send-keys` commands
2. **Output Layer** (`agent.go`, `shell.go`): Polls tmux via `capture-pane` and queries cursor position
3. **Rendering Layer** (`view_preview.go`): Overlays cursor on captured content
4. **State Management** (`types.go`): `InteractiveState` tracks mode, cursor position, timing

### Data Flow

```
User Keypress → handleInteractiveKeys()
              → MapKeyToTmux()
              → tmux send-keys
              → scheduleDebouncedPoll(20ms)
              → capture-pane + cursor query
              → AgentOutputMsg/ShellOutputMsg
              → update InteractiveState
              → pollInteractivePane() (adaptive 50-500ms)
              → renderWithCursor()
```

## Critical Implementation Details

### 1. Cursor Positioning

**Current Approach**

The cursor maps directly to tmux's 0-indexed `cursor_x/cursor_y`, with padding to keep line counts aligned to `pane_height`. No +1 hack is needed.

```go
// view_preview.go
displayHeight := len(displayLines)
relativeRow := cursorRow
if paneHeight > displayHeight {
    relativeRow = cursorRow - (paneHeight - displayHeight)
} else if paneHeight > 0 && paneHeight < displayHeight {
    relativeRow = cursorRow + (displayHeight - paneHeight)
}
```

**Trailing Space Rendering**

When the cursor is past the last visible character (e.g., after typing a space),
we pad the line to the cursor column so the cursor renders at the correct position:

```go
// interactive.go
padding := cursorCol - lineWidth
lines[cursorRow] = line + strings.Repeat(" ", padding) + cursorStyle.Render("█")
```

**Why this works**:
- `cursor_y` is already 0-indexed
- The display is padded to `pane_height` to preserve blank lines at the bottom
- The cursor column can be beyond the line's visible width; we pad to that column

### 2. Horizontal Width Calculation

**The Problem**: Getting the tmux pane width to exactly match Sidecar's preview area width is tricky.

**Solution**: Resize tmux panes in the background at all times (not just in interactive mode)
so that `capture-pane` output is already wrapped at the correct width. This eliminates the
mismatch between preview mode (client-side truncation) and interactive mode (pane-sized content).

**Implementation** (`interactive.go:calculatePreviewDimensions()`):

```go
if !p.sidebarVisible {
    width = p.width - panelOverhead
} else {
    available := p.width - dividerWidth
    sidebarW := (available * p.sidebarWidth) / 100
    previewW := available - sidebarW
    width = previewW - panelOverhead
}
```

**Background Resize Triggers** (`resizeSelectedPaneCmd()`):
- `WindowSizeMsg` — terminal resized (both interactive and non-interactive)
- Sidebar toggle (`\` key) — preview width changes
- Selection change (`loadSelectedContent()`) — different pane needs sizing
- Agent/shell creation (`AgentStartedMsg`/`ShellCreatedMsg`) — new pane at default size
- Interactive mode entry — immediate resize with verification

**Throttling**: Background resizes are throttled to max once per 500ms via `lastPreviewResizeAt`.

**Additional safety**:
- Capture `pane_width` alongside cursor position and clamp rendering to it
- If captured `pane_width/pane_height` mismatch the preview size, trigger a resize retry

### 3. Polling and Performance

**Three-State Visibility Polling** (td-97327e):

Output polling adapts based on visibility AND focus state:

| State | Active | Idle |
|-------|--------|------|
| Visible + focused | 200ms | 2s |
| Visible + unfocused | 500ms | 500ms |
| Not visible | 10-20s | 10-20s |

This ensures visible output updates reasonably even when the user clicks on another plugin.

**Interactive Mode Adaptive Polling**:

```go
pollingDecayFast   = 50ms   // During active typing
pollingDecayMedium = 200ms  // After 2s inactivity
pollingDecaySlow   = 500ms  // After 10s inactivity
```

**Keystroke Debouncing** (td-8a0978):
- Added 20ms delay after keystrokes before polling
- Batches rapid typing: "hello" at 10 chars/sec = ~2 polls instead of 5
- Reduces subprocess spam by 60%

**Critical Bug We Fixed**: After the debounced poll completed, the `AgentOutputMsg`/`ShellOutputMsg` handlers were scheduling the next poll using regular intervals (500ms-5s) instead of interactive mode intervals (50-500ms). This caused a 3-second delay before the prompt appeared after running commands like `ls`.

**Solution** (`update.go`):

```go
// In AgentOutputMsg handler:
if p.viewMode == ViewModeInteractive && !p.shellSelected {
    if wt := p.selectedWorktree(); wt != nil && wt.Name == msg.WorktreeName {
        cmds = append(cmds, p.pollInteractivePane())
        return p, tea.Batch(cmds...)
    }
}

// In ShellOutputMsg handler:
if p.viewMode == ViewModeInteractive && p.shellSelected {
    if selectedShell != nil && selectedShell.TmuxName == msg.TmuxName {
        cmds = append(cmds, p.pollInteractivePane())
        return p, tea.Batch(cmds...)
    }
}
```

This ensures interactive mode keeps using fast adaptive polling throughout the session.

### 4. Buffer Management

**Hash-Based Change Detection** (td-15cc29):

Originally, the mouse escape regex ran BEFORE the hash check:

```go
// ❌ Original (inefficient)
content = mouseEscapeRegex.ReplaceAllString(content, "")
hash := maphash.String(content)
if hash == lastHash { return false }
```

This processed 600 lines through regex even when content was unchanged.

**Optimization**:

```go
// ✅ Optimized (td-15cc29)
rawHash := maphash.String(content)
if rawHash == lastRawHash { return false } // Skip ALL processing
// Only if changed:
content = mouseEscapeRegex.ReplaceAllString(content, "")
cleanHash := maphash.String(content)
lastRawHash = rawHash
lastHash = cleanHash
```

**What We Tried and Reverted**:

**Buffer Clearing on Mode Transitions** (td-29f190 - REVERTED):
```go
// ❌ This broke everything
p.Agent.OutputBuf.Clear() // in enterInteractiveMode() and exitInteractiveMode()
```

**Result**: Screen flashing, blank output, no UI updates. The buffer clearing prevented any content from being displayed.

**Lesson**: The OutputBuffer hash-based change detection is critical for performance but also for state continuity. Clearing it breaks the entire rendering pipeline.

### 5. Cursor Capture

**Atomic Capture** (`agent.go:815-839`):

Cursor position must be captured atomically with output to avoid race conditions:

```go
func queryCursorPositionSync(target string) (row, col, paneHeight, paneWidth int, visible, ok bool) {
    cmd := exec.Command("tmux", "display-message", "-t", target,
        "-p", "#{cursor_x},#{cursor_y},#{cursor_flag},#{pane_height},#{pane_width}")
    // ...
}
```

This runs synchronously in the poll goroutine, NOT from `View()` which would block rendering.

**Cursor State Caching** (`interactive.go:766-773`):

```go
func (p *Plugin) getCursorPosition() (row, col, paneHeight int, visible bool, err error) {
    // Return cached values - never spawn subprocess from View()
    return p.interactiveState.CursorRow,
           p.interactiveState.CursorCol,
           p.interactiveState.PaneHeight,
           p.interactiveState.CursorVisible,
           nil
}
```

**Critical**: `getCursorPosition()` NEVER spawns subprocesses. It only returns cached state updated by the poll handler.

### 6. Key Mapping

**Basic Implementation** (`interactive.go`):

```go
func MapKeyToTmux(msg tea.KeyMsg) (key string, useLiteral bool) {
    switch msg.Type {
    case tea.KeyRunes:
        return string(msg.Runes), true // Use -l flag
    case tea.KeyEnter:
        return "Enter", false
    case tea.KeyCtrlC:
        return "C-c", false
    // ...
    }
}
```

**Literal Mode**: For printable characters, use `tmux send-keys -l "text"` to avoid interpretation.

**Notes**:
- Modified arrows are forwarded via CSI sequences (e.g., `\x1b[1;2A`, `\x1b[1;5A`)
- Mouse events are forwarded as SGR sequences when the app enables mouse reporting
- Some terminal apps use different escape sequences than tmux expects

## Common Pitfalls

### ❌ Don't Clear the OutputBuffer

Clearing `OutputBuf` breaks rendering. The hash-based change detection needs continuity.

### ❌ Don't Forget Interactive Polling Continuation

After output messages, always check if in interactive mode and use `pollInteractivePane()` instead of regular intervals.

### ❌ Don't Drop Pane Height Padding

Padding lines to `pane_height` prevents cursor drift when the buffer has fewer lines
than the actual tmux pane.

### ❌ Don't Call Subprocesses from View()

Cursor queries and tmux operations must run asynchronously in poll handlers, not in the render path.

### ❌ Don't Use `scheduleAgentPoll` for Shells

Shells require `scheduleShellPollByName()`, worktrees require `scheduleAgentPoll()`. Mixing them breaks the polling mechanism.

### ❌ Don't Use Wrong Generation Maps

When incrementing poll generations to cancel stale timers:
- Shells use `shellPollGeneration` (not `pollGeneration`)
- Worktrees use `pollGeneration`

Using the wrong map makes generation tracking ineffective. See td-97327e.

### ❌ Don't Forget to Invalidate Old Poll Chains

When entering interactive mode, increment the appropriate generation counter to invalidate existing poll timers. Without this, entering interactive mode creates a **second poll chain** running in parallel with the existing one, causing 200% CPU usage.

```go
// In enterInteractiveMode():
if p.shellSelected {
    p.shellPollGeneration[sessionName]++
} else {
    p.pollGeneration[wt.Name]++
}
```

## Performance Characteristics

**CPU Usage**:
- Before optimizations: 234% during typing
- After optimizations: <100% during typing

**Breakdown per keystroke** (optimized):
1. `tmux send-keys` → subprocess (10ms)
2. 20ms debounce delay
3. `tmux capture-pane` → subprocess (5ms)
4. `tmux display-message` → cursor query subprocess (5ms)
5. Hash check → O(n) string hash (1ms for 600 lines)
6. Regex (only if content changed) → O(n) pattern matching (~5ms for 600 lines)
7. Buffer split → O(n) string operations (1ms)
8. Cursor overlay rendering → O(1) ANSI-aware string slicing (< 1ms)

**Total**: ~42ms per keystroke worst case, ~36ms typical (when regex skipped)

**Polling frequency**: 200ms when active (visible+focused) = 5 polls/sec, reduced by debouncing during typing

## Feature Flag

The feature is gated behind `tmux_interactive_input`:

```go
if !features.IsEnabled(features.TmuxInteractiveInput.Name) {
    return nil
}
```

Enable in `~/.config/sidecar/config.json`:

```json
{
  "features": {
    "tmux_interactive_input": true
  }
}
```

## Entry and Exit

**Enter**: Press `i` when preview pane is focused with output tab visible

**Exit**:
- Primary: `Ctrl+\` (instant)
- Secondary: Double-Escape (with 150ms delay)
- Attach: `Ctrl+]` (exits interactive and attaches to full tmux session)

## Files to Know

| File | Purpose |
|------|---------|
| `internal/plugins/worktree/interactive.go` | Main interactive mode logic, polling, key handling |
| `internal/plugins/worktree/keymap_tmux.go` | Bubble Tea → tmux key translation |
| `internal/plugins/worktree/view_preview.go` | Cursor overlay rendering |
| `internal/plugins/worktree/agent.go` | Tmux capture and polling coordination |
| `internal/plugins/worktree/shell.go` | Shell-specific polling (similar to agent.go) |
| `internal/plugins/worktree/types.go` | OutputBuffer with hash-based change detection |
| `internal/plugins/worktree/update.go` | Message handlers for output and polling |

## Testing Interactive Mode

1. Start sidecar with tmux_interactive_input enabled
2. Create or select a worktree/shell
3. Focus preview pane, press `i`
4. Type commands: `ls`, `echo hello`, navigate with arrows
5. Verify:
   - Cursor appears on correct line (not 1 line above or below)
   - Output appears immediately after command completion (no 3-second delay)
   - CPU usage stays reasonable during typing
   - Text doesn't get truncated excessively on the right
   - Exit with `Ctrl+\` returns to list view

## Future Improvements

1. **Capture Gating**: Skip capture-pane when history/cursor hasn't changed
2. **Diagnostics**: Add optional logging to diff tmux output vs rendered buffer

## References

- Original spec: `docs/spec-tmux-interactive-input.md`
- Related issues:
  - td-29f190: Buffer invalidation (reverted)
  - td-8a0978: Keystroke debouncing
  - td-15cc29: Hash optimization
  - td-380d89: Cursor adjustment (reverted, then re-added)
  - td-194689: Mouse escape regex strengthening
  - td-4218e8: Epic for all interactive mode fixes
  - td-97327e: Duplicate poll chain fix (200% CPU)

## Key Takeaways

1. **Cursor mapping is 0-indexed** - keep pane-height padding and cursor-col padding
2. **Never clear OutputBuffer** - it breaks rendering
3. **Poll continuity is critical** - interactive mode needs fast polling throughout
4. **Hash before regex** - massive CPU savings when content unchanged
5. **Debouncing works** - 20ms delay reduces subprocess spam significantly
6. **Width sync matters** - resize panes in background at all times (not just interactive mode) and clamp rendering to `pane_width`
7. **Atomic cursor capture** - query cursor with output to avoid race conditions
8. **Separate shell/worktree polling** - use the right scheduling function for each type
9. **Use correct generation maps** - shells use `shellPollGeneration`, worktrees use `pollGeneration`
10. **Invalidate old poll chains on mode entry** - increment generation when entering interactive mode to prevent duplicate parallel poll chains (causes 200% CPU)
11. **Three-state visibility polling** - visible+focused (fast), visible+unfocused (medium), not visible (slow)

This feature is stable and works well with these learnings applied. Background pane resizing keeps tmux panes synced to the preview width at all times, eliminating width mismatches between preview and interactive modes.

# Worktree Git Parity: Context Switching Design

## Problem Statement

Users working in git worktrees have a diminished experience compared to the main repo:
- **Workspace plugin**: Can create/delete worktrees, view diff, basic merge flow, but no file-level staging
- **Git plugin**: Full capabilities (staging, history, commit, stash) but operates only on the app's current WorkDir

**The gap**: No way to use git plugin's rich features on a worktree without manually leaving sidecar.

---

## Chosen Direction

Based on discussion: **Context Switch Approach**

- Global worktree switcher (similar to project switcher but distinct)
- "Open in Git Tab" command from workspace plugin
- Clear header indicator when in a worktree
- Detect worktree deletion and gracefully switch back to main
- Remember last active plugin per worktree
- Main repo is special; worktrees are alternatives

---

## Architecture Insight

Sidecar already has the infrastructure:
- `switchProject()` + `registry.Reinit()` changes WorkDir and reinitializes all plugins
- `app.GetWorktrees()`, `WorktreeNameForPath()`, `GetMainWorktreePath()` already exist
- Inter-plugin messaging exists (`FocusPluginByIDMsg`, `NavigateToFileMsg`)
- Plugins are designed for re-initialization (reset state on `Init()`)

**The pattern exists** - it just needs to be used for worktrees.

---

## UX Design

### Worktree Switcher Modal

**Access**: Press `W` (global keybind) from anywhere

```
┌─ Switch Worktree ─────────────────────────────────────┐
│                                                        │
│  > main (current)                                      │
│    /Users/marcus/code/sidecar                          │
│                                                        │
│    feature-auth                                        │
│    /Users/marcus/code/sidecar-feature-auth             │
│                                                        │
│    bugfix-123                                          │
│    /Users/marcus/code/sidecar-bugfix-123               │
│                                                        │
│  ─────────────────────────────────────────────────────│
│  enter switch  ↑/↓ navigate  esc close                 │
└────────────────────────────────────────────────────────┘
```

**Key difference from project switcher**: This switches within the same repo (worktrees), not between repos.

### Header Indicator

When in a worktree (not main), header shows worktree context:

```
Before (main):
┌─ Sidecar / sidecar ─────────────────── Git │ Files │ ... ─┐

After (worktree):
┌─ Sidecar / sidecar → feature-auth ──── Git │ Files │ ... ─┐
```

The `→ feature-auth` indicator:
- Uses distinct styling (e.g., muted or accent color)
- Only appears when in a worktree, not on main
- Clickable to open worktree switcher (mouse support)

### "Open in Git Tab" from Workspace

When cursor is on a worktree in workspace plugin:
- Press `g` → switches context to that worktree AND focuses git tab
- Command name in footer: "Git" (short)
- Longform in help: "Open in Git Tab"

### Worktree Deletion Detection

If the current worktree is deleted externally:
1. Next operation (or periodic check) detects missing path
2. Auto-switch to main worktree
3. Show transient notification: "Worktree 'feature-auth' no longer exists. Switched to main."

### Quick Toggle (Future)

`Ctrl+6` or similar: Toggle between last two worktrees (like `Ctrl+6` in vim for alternate file).

---

## Reference Docs

- `docs/deprecated/guides/ui-feature-guide.md` - Modal creation, keyboard shortcuts, mouse support patterns
- `docs/deprecated/guides/declarative-modal-guide.md` - Full modal API reference

---

## Implementation Plan

### Phase 1: App-Level Worktree Context

**Files**: `internal/app/model.go`, `internal/app/commands.go`

1. Add worktree tracking fields to Model:
   ```go
   // In UIState or Model
   currentWorktreeName string  // empty = main
   previousWorktreePath string // for quick-toggle
   ```

2. Add message types:
   ```go
   type SwitchWorktreeMsg struct {
       Path string
   }
   ```

3. Leverage existing `app.GetWorktrees()`, `WorktreeNameForPath()` (already in `git.go`)

### Phase 2: Worktree Switcher Modal

**Files**: `internal/app/view.go`, `internal/app/update.go`

1. Add `ModalWorktreeSwitcher` modal type (similar to `ModalProjectSwitcher`)
2. Reuse project switcher patterns:
   - Cursor navigation
   - Scroll for long lists
   - `(current)` indicator
3. On selection: call `switchWorktree(path)` which calls `registry.Reinit(path)`

### Phase 3: Header Indicator

**File**: `internal/app/view.go` (in `renderHeader()`)

1. After repo name, conditionally render worktree indicator:
   ```go
   if m.currentWorktreeName != "" {
       repoSuffix += styles.WorktreeIndicator.Render(" → " + m.currentWorktreeName)
   }
   ```
2. Style: muted or accent color to distinguish from repo name

### Phase 4: Global Keybind

**File**: `internal/keymap/keymap.go`

1. Add `OpenWorktreeSwitcher` action
2. Default binding: `W` (uppercase, since `w` might conflict)
3. Handle in `internal/app/update.go`

### Phase 5: Workspace "Open in Git Tab"

**Files**: `internal/plugins/workspace/commands.go`, `internal/plugins/workspace/update.go`

1. Add `g` keybind when cursor is on a worktree item
2. Command returns:
   ```go
   return tea.Batch(
       func() tea.Msg { return app.SwitchWorktreeMsg{Path: wt.Path} },
       app.FocusPlugin("git"),
   )
   ```
3. Command name: "Git" (shown in footer)

### Phase 6: Worktree Deletion Detection

**File**: `internal/app/update.go`

1. On `SwitchWorktreeMsg` or periodic tick, check if path exists
2. If current worktree path doesn't exist:
   - Get main worktree path via `GetMainWorktreePath()`
   - Switch to main
   - Show notification (if notification system exists) or log

### Phase 7: State Persistence

**File**: `internal/state/state.go` (or new file)

1. Store per-worktree: `lastActivePlugin`
2. On worktree switch: save current state, restore target state
3. Key by worktree path (normalized)

---

## Files Summary

| File | Change |
|------|--------|
| `internal/app/model.go` | Add worktree state fields, `ModalWorktreeSwitcher` type |
| `internal/app/commands.go` | Add `SwitchWorktreeMsg` |
| `internal/app/update.go` | Handle worktree switch, deletion detection |
| `internal/app/view.go` | Worktree switcher modal, header indicator |
| `internal/keymap/keymap.go` | Add `OpenWorktreeSwitcher` action |
| `internal/plugins/workspace/commands.go` | Add "Git" command |
| `internal/plugins/workspace/update.go` | Handle `g` keybind |
| `internal/state/state.go` | Per-worktree plugin state |

---

## Edge Cases & Complexity

### Distinguishing from Project Switcher

Project switcher (`@`) switches between **different repos**.
Worktree switcher (`W`) switches within **same repo**.

To avoid confusion:
- Different keybind (`W` vs `@`)
- Different modal title ("Switch Worktree" vs "Switch Project")
- Worktree switcher only available when repo has >1 worktree (otherwise `W` does nothing or shows message)

### Worktree Disappearing

Scenarios:
1. User deletes worktree via `git worktree remove` outside sidecar
2. User deletes worktree folder manually
3. Workspace plugin deletes worktree during merge flow

Detection approaches:
- **Lazy**: Check on next user action (simple, might delay detection)
- **Active**: Check periodically via tick (more responsive, slight overhead)
- **Watch**: Use fsnotify on worktree parent (most responsive, more complex)

**Recommendation**: Start with lazy check on `SwitchWorktreeMsg` and any git operation. Add active tick if needed.

### State Restoration

What to persist per worktree:
- Last active plugin ID ✓ (Phase 7)
- Scroll positions (future)
- Expanded tree nodes (future)

Start simple: just active plugin. Expand based on user feedback.

---

## Verification

1. **Switch test**: Press `W`, select worktree, verify git plugin shows correct files
2. **Header test**: Verify header shows `→ worktree-name` when in worktree
3. **Workspace→Git test**: In workspace, cursor on worktree, press `g`, verify git tab opens with that worktree
4. **Back to main test**: Press `W`, select main, verify header indicator disappears
5. **Deletion test**: Delete worktree externally, trigger action in sidecar, verify graceful switch to main
6. **Plugin memory test**: Switch to worktree A (git tab), switch to B (files tab), switch back to A, verify git tab active

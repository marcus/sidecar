# Mouse Support for Sidecar - Implementation Plan

## Epic: td-8d8cdd24

## Overview

Add comprehensive mouse support to sidecar, starting with the git plugin. TD already has mouse support - reference its patterns and ensure it keeps working when embedded.

## Key Discovery

**Coordinate Offset Issue**: TD's `PanelBounds` are calculated from Y=0 in its content space, but sidecar has a 2-line header. Mouse events use screen coordinates, so sidecar must offset Y before forwarding MouseMsg to plugins.

## Stories (in order)

### 1. Enable mouse in sidecar (td-4030a21e)

**Files**:

- `cmd/sidecar/main.go:121` - Add `tea.WithMouseCellMotion()`
- `internal/app/update.go` - Handle `tea.MouseMsg` with Y offset

```go
// main.go
p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

// update.go
case tea.MouseMsg:
    // Offset Y by header height (2 lines) before forwarding
    adjusted := tea.MouseMsg{X: msg.X, Y: msg.Y - 2, ...}
    if p := m.ActivePlugin(); p != nil {
        newPlugin, cmd := p.Update(adjusted)
        // ...
    }
```

### 2. Create reusable mouse package (td-fe0e73bf)

**New file**: `internal/mouse/mouse.go`

Reference TD's implementation at `~/code/td/pkg/monitor/model.go:35-70`:

- `Rect` - rectangle with `Contains(x, y)`
- `Region` - named rect with associated data
- `HitMap` - `Clear()`, `Add()`, `Test()` methods
- `Handler` - combines HitMap with drag/double-click detection

### 3. Git plugin mouse support (td-fd02afd8)

**Files**:

- `internal/plugins/gitstatus/plugin.go` - add `mouseHandler` field
- `internal/plugins/gitstatus/mouse.go` - new file with mouse handlers
- `internal/plugins/gitstatus/sidebar_view.go` - register hit regions

**Features**:

| Feature | Implementation |
|---------|---------------|
| Click to select | HitMap regions per file/commit, update cursor |
| Scroll wheel | Detect which pane, scroll that pane's offset |
| Double-click | Open file, expand commit, toggle folder |
| Drag to resize | Track drag on pane divider, update `sidebarWidth` |

**Hit regions**:

- `sidebar`, `diff-pane`, `pane-divider`
- `file-{path}` per file entry
- `commit-{hash}` per recent commit

### 4. Verify TD plugin works (td-98d8b8e2)

TD has mouse support at `~/code/td/pkg/monitor/model.go`. After enabling mouse in sidecar:

- Test click/scroll/double-click in embedded TD
- Verify coordinate offset fix works (Y-2 for header)
- May need adjustments if TD's bounds calculation differs

### 5. Documentation (td-3ddea960)

**New file**: `docs/guides/ui-feature-guide.md`

- Architecture (HitMap, coordinate systems)
- Adding mouse to a plugin step-by-step
- Code examples for click, scroll, double-click, drag

## Critical Files

| File                                         | Change                           |
| -------------------------------------------- | -------------------------------- |
| `cmd/sidecar/main.go:121`                    | Add `tea.WithMouseCellMotion()`  |
| `internal/app/update.go`                     | Add `tea.MouseMsg` with Y offset |
| `internal/mouse/mouse.go`                    | New - core mouse infrastructure  |
| `internal/plugins/gitstatus/plugin.go`       | Add mouseHandler field           |
| `internal/plugins/gitstatus/mouse.go`        | New - mouse update handlers      |
| `internal/plugins/gitstatus/sidebar_view.go` | Register hit regions             |
| `docs/guides/ui-feature-guide.md`           | New - documentation              |

## Reference: TD's Mouse Implementation

Located at `~/code/td/pkg/monitor/`:

- `model.go:35-70` - HitTestPanel, HitTestRow
- `model.go:254-259` - PanelBounds, HoverPanel, click tracking
- `model.go:1687-1717` - updatePanelBounds()
- `model.go:1719-1785` - handleMouse(), handleMouseClick()

## Implementation Order

1. Enable mouse + handle MouseMsg with offset (unblocks testing)
2. Create mouse package (infrastructure)
3. Git plugin mouse support (main deliverable)
4. Verify TD plugin still works (integration test)
5. Write documentation

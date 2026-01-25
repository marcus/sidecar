> DEPRECATED: Superseded by `docs/guides/ui-feature-guide.md`.

# Mouse Support for Sidecar Plugins

This guide explains how to add mouse support to sidecar plugins using the `internal/mouse` package.

## Architecture

### Coordinate System

Sidecar has a 2-line header that's always visible. Mouse events use screen coordinates, so sidecar offsets Y by 2 before forwarding `tea.MouseMsg` to plugins. Plugins receive coordinates in their own content space starting at Y=0.

```
Screen Y=0  ┌─────────────────┐  ← App header (2 lines)
Screen Y=2  │                 │  ← Plugin content starts (Y=0 in plugin space)
            │  Plugin View    │
            │                 │
            └─────────────────┘
```

### Core Types

The `internal/mouse` package provides:

- **Rect** - Rectangle with `Contains(x, y)` hit testing
- **Region** - Named rect with associated data (e.g., file index)
- **HitMap** - Collection of regions with `Clear()`, `Add()`, `Test()`
- **Handler** - Combines HitMap with click/drag/double-click tracking

## Adding Mouse Support to a Plugin

### 1. Add Handler Field

```go
import "github.com/marcus/sidecar/internal/mouse"

type Plugin struct {
    // ... other fields
    mouseHandler *mouse.Handler
}

func New() *Plugin {
    return &Plugin{
        mouseHandler: mouse.NewHandler(),
    }
}
```

### 2. Handle MouseMsg in Update

```go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        return p.handleMouse(msg)
    // ... other cases
    }
    return p, nil
}
```

### 3. Register Hit Regions During Render

Hit regions must be updated each render to match the current layout:

```go
func (p *Plugin) View(width, height int) string {
    // Clear old regions at start of render
    p.mouseHandler.Clear()

    // Register regions as you render elements
    p.mouseHandler.HitMap.AddRect("sidebar", 0, 0, sidebarWidth, height, nil)
    p.mouseHandler.HitMap.AddRect("file-0", 2, 5, width-4, 1, 0) // data: file index

    // ... render content
}
```

### 4. Process Mouse Actions

```go
func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
    action := p.mouseHandler.HandleMouse(msg)

    switch action.Type {
    case mouse.ActionClick:
        if action.Region != nil {
            switch action.Region.ID {
            case "sidebar":
                p.focusSidebar()
            case "file-0":
                idx := action.Region.Data.(int)
                p.selectFile(idx)
            }
        }

    case mouse.ActionDoubleClick:
        if action.Region != nil && action.Region.ID == "file-0" {
            idx := action.Region.Data.(int)
            return p, p.openFile(idx)
        }

    case mouse.ActionScrollUp, mouse.ActionScrollDown:
        if action.Region != nil {
            p.scroll(action.Delta)
        }

    case mouse.ActionDrag:
        // Handle drag (e.g., pane resizing)
        dx, dy := action.DragDX, action.DragDY
    }

    return p, nil
}
```

## Common Patterns

## Table Checklist (Save Future Pain)

- Disable all table borders when embedding (`BorderTop/Bottom/Left/Right(false)`).
- Do not use `Height` + `Offset` for scrolling; slice rows to the visible window.
- Fix column widths via `StyleFunc` to prevent per-page column resizing.
- Use a shared layout helper for render + hit testing + visible height.
- Clamp scroll offset to `len(rows)-dataRowsVisible`.
- Ignore the scroll-indicator line in hit tests.

### Click to Select

```go
case mouse.ActionClick:
    if action.Region != nil && action.Region.ID == "item" {
        if idx, ok := action.Region.Data.(int); ok {
            p.cursor = idx
            p.ensureCursorVisible()
        }
    }
```

### Scroll Wheel

```go
case mouse.ActionScrollUp, mouse.ActionScrollDown:
    // Scroll content by delta (typically 3 lines)
    p.scrollOffset += action.Delta
    p.clampScroll()
    // Keep cursor visible (see below)
    p.keepCursorVisible()
```

### Keep Cursor Visible During Scroll

When scrolling with the mouse wheel, keep the cursor within the visible area to prevent UX issues:

```go
func (p *Plugin) keepCursorVisible() {
    visibleHeight := p.calculateVisibleHeight()

    // If cursor went above viewport, move it to first visible row
    if p.cursor < p.scrollOffset {
        p.cursor = p.scrollOffset
    }
    // If cursor went below viewport, move it to last visible row
    if p.cursor >= p.scrollOffset+visibleHeight {
        p.cursor = p.scrollOffset + visibleHeight - 1
        if p.cursor >= len(p.items) {
            p.cursor = len(p.items) - 1
        }
    }
}
```

**Why this matters**: If cursor goes off-screen:
1. No row appears highlighted (confusing)
2. Click selection may not work due to cursor/offset mismatch in rendering
3. User loses track of their position

### Double-Click Actions

```go
case mouse.ActionDoubleClick:
    if action.Region != nil {
        switch action.Region.ID {
        case "file":
            return p, p.openFile(action.Region.Data.(int))
        case "folder":
            p.toggleFolder(action.Region.Data.(int))
        }
    }
```

### Drag to Resize

> **See also:** `drag-pane-implementation-guide.md` for a complete step-by-step implementation guide with all the critical rules to avoid common bugs.

```go
case mouse.ActionClick:
    if action.Region != nil && action.Region.ID == "divider" {
        // Start drag with current width as initial value
        p.mouseHandler.StartDrag(action.X, action.Y, "divider", p.sidebarWidth)
    }

case mouse.ActionDrag:
    if p.mouseHandler.DragRegion() == "divider" {
        // Calculate new width from drag delta
        newWidth := p.mouseHandler.DragStartValue() + action.DragDX
        p.sidebarWidth = clamp(newWidth, minWidth, maxWidth)
    }
```

### Hover State

The mouse handler emits `ActionHover` on mouse motion (when not dragging). Use this for visual feedback like button highlighting:

```go
case mouse.ActionHover:
    if action.Region != nil {
        switch action.Region.ID {
        case "button":
            p.buttonHover = true
        default:
            p.buttonHover = false
        }
    } else {
        p.buttonHover = false
    }
```

Apply hover styling in render (focus takes precedence):
```go
style := styles.Button
if p.buttonFocus {
    style = styles.ButtonFocused
} else if p.buttonHover {
    style = styles.ButtonHover
}
```

## Hit Region Best Practices

1. **Clear regions each render** - The layout may change, so rebuild the hit map
2. **Register general regions FIRST, specific regions LAST** - Regions are tested in reverse order (last added = checked first). Add container/pane regions first as fallbacks, then add clickable items last so they take priority.
3. **Use meaningful IDs** - `"file"`, `"commit"`, `"divider"` not `"region1"`
4. **Store indices in Data** - Use `Data` field for item indices, not string parsing
5. **Account for borders** - Subtract padding/borders from click regions

### Region Priority (Critical)

This is a common source of bugs. The `HitMap.Test()` method checks regions in **reverse order** - the last region added is tested first.

```go
// WRONG: Pane regions added last will catch all clicks
p.mouseHandler.HitMap.AddRect("file-item", x, y, w, 1, idx)  // Added first
p.mouseHandler.HitMap.AddRect("tree-pane", 0, 0, w, h, nil)  // Added last - tested first!

// CORRECT: Add general regions first (fallback), specific regions last (priority)
p.mouseHandler.HitMap.AddRect("tree-pane", 0, 0, w, h, nil)  // Added first - tested last
p.mouseHandler.HitMap.AddRect("file-item", x, y, w, 1, idx)  // Added last - tested first!
```

If clicks on items aren't registering but pane focus works, check your region ordering.

## Example Implementations

### Git Plugin

- `internal/plugins/gitstatus/mouse.go` - Mouse event handlers
- `internal/plugins/gitstatus/sidebar_view.go` - Hit region registration

Key features:
- Click to select files/commits
- Scroll wheel navigation
- Double-click to open files or toggle folders
- Drag pane divider to resize

### File Browser Plugin

- `internal/plugins/filebrowser/mouse.go` - Region constants and all mouse handlers
- `internal/plugins/filebrowser/view.go` - Hit region registration in `renderView()`
- `internal/plugins/filebrowser/plugin.go` - Handler field and Update() routing

Key features:
- Click tree items to select, double-click to open/toggle
- Click panes to focus (tree or preview)
- Scroll wheel moves cursor (tree) or scrolls content (preview)
- Drag pane divider to resize
- Quick open modal with click/double-click support

## Embedded Plugins (TD Monitor)

When embedding a plugin that has its own mouse support (like TD), coordinate offsets are handled at the app level:

1. Sidecar subtracts 2 from `WindowSizeMsg.Height` (for app header) before forwarding to plugins
2. Sidecar subtracts 2 from `MouseMsg.Y` before forwarding to plugins
3. Plugins receive both messages in plugin-local coordinate space (Y=0 = top of plugin content)

```go
// In tdmonitor/plugin.go Update()
// The app already adjusts height for the header offset
if wsm, ok := msg.(tea.WindowSizeMsg); ok {
    p.width = wsm.Width
    p.height = wsm.Height
    newModel, cmd := p.model.Update(wsm)  // Forward directly, no adjustment needed
    // ...
}
```

This ensures TD's `PanelBounds` (calculated in `updatePanelBounds()`) align with the already-adjusted mouse coordinates.

## Testing Mouse Support

Since mouse events require terminal interaction, test by:

1. Running `sidecar` in a terminal with mouse support
2. Clicking on UI elements to verify selection
3. Using scroll wheel to navigate lists
4. Double-clicking to trigger actions
5. Dragging dividers to resize panes

## Troubleshooting

### Clicks not registering on items but pane focus works
**Cause:** Region ordering is wrong. Pane regions are added after item regions, so they match first.
**Fix:** Add pane/container regions FIRST, then add specific item regions LAST.

### Y coordinates are off by a few lines
**Cause:** Not accounting for input bars, headers, or borders.
**Fix:** Calculate Y offset: `itemY = inputBarHeight + borderTop + headerLines + itemIndex`

### Double-click not firing
**Cause:** Double-click detection requires clicks within 400ms on the same region.
**Fix:** Ensure the region ID and bounds are consistent between clicks. Check that `HandleMouse()` is being called for all mouse events.

### Scroll not working in one pane
**Cause:** Region not registered or X-coordinate fallback not implemented.
**Fix:** If `action.Region` is nil, fall back to checking `action.X` to determine which pane to scroll.

### Scroll not working when over items (but works on empty space)
**Cause:** Scroll handler only checks pane regions, but item regions have higher priority. When scrolling over an item, `HitMap.Test()` returns the item region (e.g., `regionFileItem`) instead of the pane region (e.g., `regionSidebar`), and the scroll handler ignores it.
**Fix:** Include item regions in scroll routing:
```go
case mouse.ActionScrollUp, mouse.ActionScrollDown:
    switch action.Region.ID {
    case regionSidebar, regionFileItem, regionCommitItem:  // Include items!
        return p.scrollSidebar(action.Delta)
    case regionMainPane, regionDetailItem:
        return p.scrollMainPane(action.Delta)
    }
```

### Drag not working
**Cause:** `StartDrag()` not called on initial click, or checking wrong region ID.
**Fix:** Call `StartDrag(x, y, regionID, initialValue)` in the click handler, then check `DragRegion()` in the drag handler.

### Clicks select the wrong row (off-by-one)
**Cause:** Lipgloss styles with `MarginTop()`, `MarginBottom()`, or `Padding()` add extra blank lines that aren't accounted for in hit testing. For example:
```go
sectionHeader := lipgloss.NewStyle().Bold(true).MarginTop(1)  // Adds 1 blank line ABOVE

// View renders 3 lines total:
content.WriteString("\n")                          // Line 0: explicit blank
content.WriteString(sectionHeader.Render("HEADER")) // Line 1: MarginTop blank, Line 2: "HEADER"

// But hit test only accounts for 2 lines:
// linePos 0: blank line
// linePos 1: header      <- WRONG! This is actually the MarginTop blank
// linePos 2: first item  <- WRONG! This is actually the header
```
This causes clicks to select the row BELOW the intended target.

**Fix:** Account for ALL lines including those from lipgloss styling:
```go
// linePos 0: explicit blank line
if relY == linePos { return -1 }
linePos++
// linePos 1: MarginTop blank line (from sectionHeader style)
if relY == linePos { return -1 }
linePos++
// linePos 2: header text
if relY == linePos { return -1 }
linePos++
// linePos 3+: content items
```

**Prevention:** When using styled components, check if they have margin/padding that adds vertical space. Test by clicking on the first few items after a styled header - if clicks select the wrong row, count the actual rendered lines vs what the hit test expects.

**See also:** TD Monitor's `hitTestCurrentWorkRow()` in `pkg/monitor/input.go` for an example of accounting for `sectionHeader`'s `MarginTop(1)`.

### Clicks select the wrong row after scrolling a table
**Cause:** `lipgloss/table` renders hidden border lines (even with `HiddenBorder`) and an overflow ellipsis row when `Height(...)` + `Offset(...)` are used. That ellipsis row is not real data, so cursor math and hit testing drift after scrolling.

**Fix:**
1. Disable all table borders (`BorderTop/Bottom/Left/Right(false)`) so header/data row counts are stable.
2. Avoid `Height(...)` + `Offset(...)` for scrolling. Slice your data to the visible window and pass only those rows to the table.
3. Fix column widths via `StyleFunc` so columns don’t resize when the visible window changes.
4. Centralize layout math (header rows, data rows visible, scroll indicator rows) and reuse it for render + hit testing.
5. Clamp scroll offset to `len(data) - dataRowsVisible` and ignore the scroll-indicator line below the table in hit tests.

If keyboard navigation keeps the cursor visible but mouse clicks drift, it usually means your render math and input math disagree.

### Modal hit regions are off by many rows (e.g., 5+ rows)
**Cause:** Multiple errors compounding:
1. Wrong border/padding offset (using +1 instead of +2)
2. Text wrapping in rendered content not accounted for

For modals with `Border(lipgloss.RoundedBorder())` and `Padding(1, 2)`:
- Border adds 1 row at top
- `Padding(1, 2)` adds 1 row vertical padding (first arg is vertical)
- Content starts at `modalY + 2`, NOT `modalY + 1`

**Fix:** Calculate Y positions dynamically, accounting for text wrapping:
```go
// Content starts after border(1) + padding(1) = 2
currentY := modalStartY + 2

// Track through content, handling wrapping
currentY += 2 // title + blank

// Long paths/text may wrap - calculate actual line count
contentWidth := modalW - 6 // border(2) + padding(4)
pathWidth := ansi.StringWidth(pathLine)
pathLineCount := (pathWidth + contentWidth - 1) / contentWidth
currentY += pathLineCount

// Continue tracking through remaining content...
checkboxY := currentY
```

**See also:** `modal-creator-guide.md` section "Hit Region Calculation for Modal Buttons" for detailed patterns.

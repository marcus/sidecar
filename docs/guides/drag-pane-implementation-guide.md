# Drag-to-Resize Pane Implementation Guide

This guide covers how to add drag-to-resize support for two-pane plugin layouts. Follow these steps exactly to avoid common bugs.

## Prerequisites

- Plugin already has a two-pane layout (sidebar + main content)
- State persistence functions exist (e.g., `state.GetPluginSidebarWidth()` / `state.SetPluginSidebarWidth()`)
- Familiarity with `internal/mouse` package (see `docs/guides/ui-feature-guide.md`)

## Implementation Checklist

### 1. Add Mouse Handler to Plugin Struct

```go
import "github.com/marcus/sidecar/internal/mouse"

type Plugin struct {
    // ... other fields
    mouseHandler *mouse.Handler
    sidebarWidth int  // Current sidebar width (persisted)
}

func New() *Plugin {
    return &Plugin{
        mouseHandler: mouse.NewHandler(),
    }
}
```

### 2. Define Hit Region Constants

```go
const (
    regionSidebar     = "sidebar"
    regionMainPane    = "main-pane"
    regionPaneDivider = "pane-divider"
    dividerWidth      = 1  // Visual divider width
)
```

### 3. Load Persisted Width in Init

```go
func (p *Plugin) Init(ctx *plugin.Context) error {
    // Load persisted sidebar width
    if savedWidth := state.GetPluginSidebarWidth(); savedWidth > 0 {
        p.sidebarWidth = savedWidth
    }
    // ... rest of init
}
```

### 4. Handle MouseMsg in Update

```go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        return p.handleMouse(msg)
    // ... other cases
    }
}
```

### 5. Create mouse.go with Handlers

```go
package myplugin

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/marcus/sidecar/internal/mouse"
    "github.com/marcus/sidecar/internal/state"
)

func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
    action := p.mouseHandler.HandleMouse(msg)

    switch action.Type {
    case mouse.ActionClick:
        return p.handleMouseClick(action)
    case mouse.ActionDrag:
        return p.handleMouseDrag(action)
    case mouse.ActionDragEnd:
        return p.handleMouseDragEnd()
    // ... other action types
    }
    return p, nil
}

func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    if action.Region == nil {
        return p, nil
    }

    switch action.Region.ID {
    case regionSidebar:
        p.activePane = PaneSidebar
    case regionMainPane:
        p.activePane = PaneMain
    case regionPaneDivider:
        // Start drag with current width as initial value
        p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
    }
    return p, nil
}

func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    if p.mouseHandler.DragRegion() != regionPaneDivider {
        return p, nil
    }

    // Calculate new width from drag delta
    startValue := p.mouseHandler.DragStartValue()
    newWidth := startValue + action.DragDX

    // Clamp to bounds
    available := p.width - 5 - dividerWidth
    minWidth := 25
    maxWidth := available - 40
    if newWidth < minWidth {
        newWidth = minWidth
    } else if newWidth > maxWidth {
        newWidth = maxWidth
    }

    p.sidebarWidth = newWidth
    return p, nil
}

func (p *Plugin) handleMouseDragEnd() (*Plugin, tea.Cmd) {
    _ = state.SetPluginSidebarWidth(p.sidebarWidth)
    return p, nil
}
```

### 6. Register Hit Regions in Render

This is where most bugs occur. Follow this pattern exactly:

```go
func (p *Plugin) renderTwoPane() string {
    // CRITICAL: Clear hit regions at start of each render
    p.mouseHandler.HitMap.Clear()

    // Calculate widths - only set default if not initialized
    available := p.width - 5 - dividerWidth
    sidebarWidth := p.sidebarWidth
    if sidebarWidth == 0 {
        sidebarWidth = available * 30 / 100  // Default 30%
    }
    // Clamp to bounds
    if sidebarWidth < 25 {
        sidebarWidth = 25
    }
    if sidebarWidth > available-40 {
        sidebarWidth = available - 40
    }
    mainWidth := available - sidebarWidth

    // Store back
    p.sidebarWidth = sidebarWidth

    // ... render panes and divider ...

    // CRITICAL: Register regions in priority order (last = highest priority)
    // 1. Sidebar region (lowest priority - fallback)
    p.mouseHandler.HitMap.AddRect(regionSidebar, 0, 0, sidebarWidth, p.height, nil)

    // 2. Main pane region (medium priority)
    mainX := sidebarWidth + dividerWidth
    p.mouseHandler.HitMap.AddRect(regionMainPane, mainX, 0, mainWidth, p.height, nil)

    // 3. Divider region (HIGHEST priority - registered LAST)
    dividerX := sidebarWidth
    dividerHitWidth := 3  // Wider than visual for easier clicking
    p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, 0, dividerHitWidth, p.height, nil)

    return content
}
```

### 7. Render Visible Divider

```go
func (p *Plugin) renderDivider(height int) string {
    dividerStyle := lipgloss.NewStyle().
        Foreground(styles.BorderNormal).
        MarginTop(1)  // Aligns with pane content (below top border)

    var sb strings.Builder
    for i := 0; i < height; i++ {
        sb.WriteString("│")
        if i < height-1 {
            sb.WriteString("\n")
        }
    }
    return dividerStyle.Render(sb.String())
}
```

## Critical Rules (Read These!)

### Rule 1: Never Reset Width in View()

**WRONG:**
```go
func (p *Plugin) View(width, height int) string {
    p.width = width
    p.height = height
    if p.twoPane {
        p.sidebarWidth = width * 30 / 100  // BUG: Overwrites drag changes!
    }
    // ...
}
```

**CORRECT:**
```go
func (p *Plugin) View(width, height int) string {
    p.width = width
    p.height = height
    // Width calculation happens in renderTwoPane(), not here
    // ...
}
```

Width must only be set when `sidebarWidth == 0`. Any other code path that unconditionally sets width will reset drag changes on every render.

### Rule 2: Hit Region X Coordinates

The divider X position = `sidebarWidth`, NOT `sidebarWidth + borderWidth`.

When lipgloss renders `Width(sidebarWidth)`, the pane occupies columns 0 to sidebarWidth-1. The divider starts at column sidebarWidth.

**WRONG:**
```go
dividerX := sidebarWidth + 2  // Off by 2!
p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, ...)
```

**CORRECT:**
```go
dividerX := sidebarWidth
p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, ...)
```

### Rule 3: Hit Region Priority (Registration Order)

`HitMap.Test()` checks regions in **reverse order** - last added = checked first.

The divider region MUST be registered LAST so it takes priority over overlapping pane regions.

**WRONG ORDER:**
```go
p.mouseHandler.HitMap.AddRect(regionSidebar, ...)
p.mouseHandler.HitMap.AddRect(regionPaneDivider, ...)  // Lower priority
p.mouseHandler.HitMap.AddRect(regionMainPane, ...)     // Wins - catches divider clicks!
```

**CORRECT ORDER:**
```go
p.mouseHandler.HitMap.AddRect(regionSidebar, ...)      // Lowest priority
p.mouseHandler.HitMap.AddRect(regionMainPane, ...)     // Medium priority
p.mouseHandler.HitMap.AddRect(regionPaneDivider, ...)  // HIGHEST priority (last)
```

### Rule 4: Divider Hit Width

Use `dividerHitWidth := 3` (wider than the visual 1-character divider) to make clicking easier.

### Rule 5: Height for Hit Regions

Use `p.height` for hit region height, not `paneHeight` or `paneHeight + 2`.

## Debugging

If drag isn't working, add temporary logging:

```go
func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    log.Printf("CLICK x=%d y=%d region=%v", action.X, action.Y, action.Region)
    // ...
}
```

Common issues:
- **Region is nil or wrong pane:** Check X coordinate calculation and registration order
- **Drag starts but width doesn't change:** Check that `handleMouseDrag` is being called
- **Width resets after drag:** Search for code that sets `sidebarWidth` unconditionally

## Reference Implementations

Working examples to compare against:

- `internal/plugins/filebrowser/mouse.go` + `view.go`
- `internal/plugins/gitstatus/mouse.go` + `sidebar_view.go`
- `internal/plugins/conversations/mouse.go` + `view.go`

## State Persistence

Add functions to `internal/state/state.go`:

```go
// In State struct
PluginSidebarWidth int `json:"pluginSidebarWidth,omitempty"`

// Getter
func GetPluginSidebarWidth() int {
    mu.RLock()
    defer mu.RUnlock()
    if current == nil {
        return 0
    }
    return current.PluginSidebarWidth
}

// Setter
func SetPluginSidebarWidth(width int) error {
    mu.Lock()
    if current == nil {
        current = &State{}
    }
    current.PluginSidebarWidth = width
    mu.Unlock()
    return Save()
}
```

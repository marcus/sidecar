# UI Feature Implementation Guide

This is the single entry point for Sidecar UI feature work: modals, keyboard shortcuts, and mouse support.
All new modals must use the internal modal library. The modal spec is authoritative: `docs/spec-modal-library.md`.

## Quick checklist
- Modals: use `internal/modal`, render with `ui.OverlayModal`, avoid manual hit region math.
- Keyboard: Commands + FocusContext + bindings match; names are short; priorities set.
- Mouse: rebuild hit regions on each render; add general regions first, specific last.
- Rendering: keep output within the View width and height to avoid header/footer overlap.
- Testing: verify keyboard, mouse, hover, scrolling, and footer hints.

## Modals (internal/modal)

### Requirements
- All new modals must be built with `internal/modal`.
- Follow `docs/spec-modal-library.md` for the full API and layout rules.
- Do not implement custom hit region math or manual button focus logic.

### Create a modal
```go
m := modal.New("Delete Worktree?",
    modal.WithWidth(58),
    modal.WithVariant(modal.VariantDanger),
    modal.WithPrimaryAction("delete"),
).
    AddSection(modal.Text("Name: " + wt.Name)).
    AddSection(modal.Spacer()).
    AddSection(modal.Buttons(
        modal.Btn(" Delete ", "delete", modal.BtnDanger()),
        modal.Btn(" Cancel ", "cancel"),
    ))
```

### Render in View
```go
func (p *Plugin) renderDeleteView(width, height int) string {
    background := p.renderListView(width, height)
    rendered := p.deleteModal.Render(width, height, p.mouseHandler)
    return ui.OverlayModal(background, rendered, width, height)
}
```

### Handle input in Update
```go
case tea.KeyMsg:
    action, cmd := p.deleteModal.HandleKey(msg)
    if action != "" {
        return p.handleModalAction(action)
    }
    return p, cmd

case tea.MouseMsg:
    action := p.deleteModal.HandleMouse(msg, p.mouseHandler)
    if action != "" {
        return p.handleModalAction(action)
    }
    return p, nil
```

### Modal notes
- `HandleKey` and `HandleMouse` already handle Tab, Shift+Tab, Enter, and Esc.
- Backdrop clicks return "cancel" by default; use `WithCloseOnBackdropClick(false)` to disable.
- Use built-in sections (Text, Input, Textarea, Buttons, Checkbox, List, When) before custom layouts.
- For bespoke layouts, use `modal.Custom` and return explicit focusable offsets.
- Do not render footers or hint lines in plugin View. The app renders the unified footer from Commands().

### Background overlay
- Prefer `ui.OverlayModal(background, modal, width, height)` for dimmed overlays.
- Do not pre-center modal content with `lipgloss.Place` when using OverlayModal.
- OverlayModal strips ANSI color and applies a consistent gray dim (242) for reliability.
- For a full blackout (rare, non-modal overlays), use `lipgloss.Place` with whitespace fill.

## Keyboard shortcuts

### Quick start: three things must match
1. Command ID in `Commands()` (example: "stage-file")
2. Binding command in `internal/keymap/bindings.go` (example: "stage-file")
3. Context string in both places (example: "git-status")

```go
// 1) Commands()
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        {ID: "stage-file", Name: "Stage", Context: "git-status", Priority: 1},
    }
}

// 2) FocusContext()
func (p *Plugin) FocusContext() string {
    return "git-status"
}

// 3) bindings.go
{Key: "s", Command: "stage-file", Context: "git-status"},
```

### Bindings vs handlers
- Most bindings exist to show footer hints and are handled in the plugin Update().
- Some commands are intercepted at the app level (quit, next plugin, palette, diagnostics, switch project, refresh).
- If the key is not handled by the app, it falls through to the plugin Update().
- Context precedence is plugin context first, then `global`.

### Footer hints and parity
- The footer shows hints for Commands() in the active context, sorted by Priority (1 is highest).
- Keep command names short (one word when possible) to avoid footer truncation.
- Plugins must not render their own footer or hint line in View.
- Match established patterns: Tab and Shift+Tab to switch panes, backslash to toggle sidebar, Esc to close modals, q to quit or go back depending on context.

### Root contexts (q behavior)
- In root contexts, "q" shows the quit confirmation.
- In non-root contexts, "q" navigates back or closes the view.
- Update the root list in `internal/app/update.go` when adding new contexts.

### Key format reference
```go
{Key: "j", Command: "cursor-down", Context: "global"}
{Key: "G", Command: "cursor-bottom", Context: "global"}
{Key: "ctrl+d", Command: "page-down", Context: "global"}
{Key: "alt+enter", Command: "execute-commit", Context: "git-commit"}
{Key: "enter", Command: "select", Context: "global"}
{Key: "esc", Command: "back", Context: "global"}
{Key: "`", Command: "next-plugin", Context: "global"}
{Key: "~", Command: "prev-plugin", Context: "global"}
{Key: "g g", Command: "cursor-top", Context: "global"} // sequences (space-separated, 500ms)
```

### Command palette
- Press `?` to open.
- Use j/k or up/down to move, enter to execute, esc to close.
- Press tab to toggle between current-context commands and all commands.

### Keyboard checklist
- Command added to Commands() with ID, Name, Context, Priority.
- FocusContext() returns the matching context.
- Binding added to `internal/keymap/bindings.go`.
- Key handled in Update() if the app does not intercept it.
- No conflicting keys in the same context.
- Footer hints are short and high priority actions use Priority 1 or 2.
- Verify q behavior with `isRootContext()`.

### Testing
- Run `sidecar --debug` to inspect key handling.
- Press `?` to verify the command palette shows your bindings.
- Check footer hints in each context and at narrow widths.

## Mouse support

### Coordinate system
- Sidecar has a 2-line header that is always visible.
- The app offsets Y by 2 before forwarding mouse events to plugins.
- Plugins operate in a local coordinate space where Y=0 is the top of the plugin content.

### Add mouse support to a plugin
1) Add a handler field:
```go
type Plugin struct {
    // ...
    mouseHandler *mouse.Handler
}

func New() *Plugin {
    return &Plugin{mouseHandler: mouse.NewHandler()}
}
```

2) Handle tea.MouseMsg in Update():
```go
case tea.MouseMsg:
    return p.handleMouse(msg)
```

3) Register hit regions during render:
```go
func (p *Plugin) View(width, height int) string {
    p.mouseHandler.Clear()
    p.mouseHandler.HitMap.AddRect("pane", 0, 0, width, height, nil)
    p.mouseHandler.HitMap.AddRect("item", 2, 5, width-4, 1, 0)
    return content
}
```

### Region ordering (critical)
- Regions are tested in reverse order.
- Add general regions first, specific regions last.
- Use meaningful IDs and store indices in Region.Data.

### Common patterns
- Click to select and focus.
- Scroll wheel to move cursor or scroll content.
- Keep the cursor visible when scrolling.
- Double-click for open actions.
- Drag regions for pane resizing.
- Hover for visual feedback (focus takes precedence over hover).

### Troubleshooting
- Clicks on items do not register: check region order (pane regions must be added first).
- Y offsets feel wrong: account for borders, padding, headers, or input bars.
- Scroll does not work over items: include item regions in scroll routing.
- Double-click does not fire: ensure consistent region ID and bounds between clicks.
- Drag does not work: call StartDrag on click and check DragRegion during drag.

### Testing
- Run sidecar in a terminal with mouse support.
- Verify click, scroll, double-click, drag, and hover behaviors.

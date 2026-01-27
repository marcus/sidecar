# UI Feature Implementation Guide

This is the single entry point for Sidecar UI feature work: modals, keyboard shortcuts, and mouse support.
All new modals must use the internal modal library. See `docs/guides/declarative-modal-guide.md` for the full API reference.

## Quick checklist
- Modals: use `internal/modal`, render with `ui.OverlayModal`, avoid manual hit region math.
- Pills/chips/tabs: use `styles.RenderPillWithStyle`; auto-fallback when `nerdFontsEnabled` is false.
- Keyboard: Commands + FocusContext + bindings match; names are short; priorities set.
- Mouse: rebuild hit regions on each render; add general regions first, specific last.
- Rendering: keep output within the View width and height to avoid header/footer overlap. The plugin's View height parameter already accounts for the header. Use `contentHeight := height - linesTakenByYourHeaders - footerLines` to ensure content fits.
- Testing: verify keyboard, mouse, hover, scrolling, and footer hints.

## Modals (internal/modal)

### Requirements
- All new modals must be built with `internal/modal`.
- See `docs/guides/declarative-modal-guide.md` for the full API and patterns.
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

### Modal initialization and caching (critical)

**Always call `ensureModal()` in BOTH the View and Update handlers.**

The modal must be initialized before any input handling. Create an `ensure` function that:
1. Returns early if required state is missing (e.g., `session == nil`)
2. Caches based on width to avoid rebuilding every frame
3. Creates the modal only when needed

```go
func (p *Plugin) ensureMyModal() {
    if p.targetItem == nil {
        return
    }
    modalW := 50
    if modalW > p.width-4 {
        modalW = p.width - 4
    }
    if modalW < 20 {
        modalW = 20  // Prevent negative/tiny widths
    }
    // Only rebuild if modal doesn't exist or width changed.
    // Caching prevents rebuilding the modal every frame, which is critical
    // for performance since View() is called on every render cycle.
    if p.myModal != nil && p.myModalWidthCache == modalW {
        return
    }
    p.myModalWidthCache = modalW
    p.myModal = modal.New("Title", modal.WithWidth(modalW), ...).
        AddSection(...)
}
```

**The key handler MUST call ensure before checking nil:**
```go
func (p *Plugin) handleMyModalKeys(msg tea.KeyMsg) tea.Cmd {
    p.ensureMyModal()  // <-- CRITICAL: Initialize before nil check
    if p.myModal == nil {
        return nil
    }
    action, cmd := p.myModal.HandleKey(msg)
    // ... handle actions
    return cmd
}
```

Without calling `ensureModal()` in the key handler, the first keypress after opening
the modal will be dropped because the modal hasn't been created yet (View runs
after Update in bubbletea).

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

## Pill-shaped elements (internal/styles)

Pill-shaped tabs, chips, and buttons use Nerd Font Powerline characters for rounded corners. This is controlled by the `nerdFontsEnabled` config flag.

### Feature flag
Set in `~/.config/sidecar/config.json`:
```json
{
  "ui": {
    "nerdFontsEnabled": true
  }
}
```

When enabled, `styles.PillTabsEnabled` is `true` and pill functions render rounded caps. When disabled, elements render as standard rectangular chips with padding.

### Render a pill with explicit colors
```go
// RenderPill(text, foreground, background, outerBackground)
label := styles.RenderPill("Output", styles.TextPrimary, styles.Primary, "")
```
- `outerBg` defaults to `styles.BgSecondary` if empty.

### Render a pill with a lipgloss.Style
```go
// Active tab
active := styles.RenderPillWithStyle("Output", styles.BarChipActive, "")

// Inactive tab
inactive := styles.RenderPillWithStyle("Diff", styles.BarChip, "")
```
This extracts foreground/background from the style automatically. Use this for chips and tab headers.

### Tab row example
```go
func (p *Plugin) renderTabs(width int) string {
    tabs := []string{"Output", "Diff", "Task"}
    var rendered []string
    for i, tab := range tabs {
        if i == p.activeTab {
            rendered = append(rendered, styles.RenderPillWithStyle(tab, styles.BarChipActive, ""))
        } else {
            rendered = append(rendered, styles.RenderPillWithStyle(tab, styles.BarChip, ""))
        }
    }
    return strings.Join(rendered, " ")
}
```

### Available styles for pills
| Style | Use case |
|-------|----------|
| `styles.BarChip` | Inactive tab/chip (muted text, tertiary bg) |
| `styles.BarChipActive` | Active tab/chip (primary text, primary bg, bold) |
| Custom `lipgloss.Style` | Buttons with danger/success variants |

### How it works
- Nerd Font characters: `\ue0b6` (left cap) and `\ue0b4` (right cap)
- Left cap: foreground = pill bg, background = outer bg
- Right cap: foreground = pill bg, background = outer bg
- Content: foreground = text color, background = pill bg

### Checklist
- Use `styles.RenderPillWithStyle` for tabs/chips with existing styles.
- Use `styles.RenderPill` when you need explicit color control.
- Ensure the outer background matches the surrounding area (or leave empty for default).
- Test with `nerdFontsEnabled: true` and `false` to verify fallback rendering.

## Keyboard shortcuts

For complete shortcut listings per plugin, see `docs/guides/keyboard-shortcuts-reference.md`.

### Key routing flow
```
User presses key → App handleKeyMsg() → Plugin Update()
                   (quit, palette, `, ~, !, @)   (if not handled)
```
Context precedence: plugin context checked first, then `global`.

### Quick start: three things must match
1. Command ID in `Commands()` (example: "stage-file")
2. Binding command in `internal/keymap/bindings.go` (example: "stage-file")
3. Context string in both places (example: "git-status")

```go
// 1) Commands()
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        {
            ID:          "stage-file",
            Name:        "Stage",
            Description: "Stage selected file for commit",  // optional
            Category:    plugin.CategoryGit,                // optional
            Context:     "git-status",
            Priority:    1,
        },
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
- Footer hints are sorted by Priority (1 is highest). Plugins can return different Command sets per context (e.g., "git-status" vs "git-status-commits").
- Keep command names short (one word when possible) to avoid footer truncation.
- Plugins must not render their own footer or hint line in View.
- Match established patterns: Tab and Shift+Tab to switch panes, backslash to toggle sidebar, Esc to close modals, q to quit or go back depending on context.

### Priority guidelines
- **1**: Primary actions (Stage, Commit, Open)
- **2**: Secondary actions (Diff, Search, Push)
- **3**: Tertiary actions (History, Refresh)
- **4+**: Palette only (Browse, external integrations)

### Root contexts (q behavior)
- In root contexts, "q" shows the quit confirmation.
- In non-root contexts, "q" navigates back or closes the view.
- Update the root list in `internal/app/update.go` when adding new contexts.

**Root contexts** (q = quit): `global`, `conversations`, `conversations-sidebar`, `git-status`, `git-status-commits`, `git-status-diff`, `file-browser-tree`, `workspace-list`, `td-monitor`

### Key format reference
```go
{Key: "j", Command: "cursor-down", Context: "global"}
{Key: "G", Command: "cursor-bottom", Context: "global"}
{Key: "ctrl+d", Command: "page-down", Context: "global"}
{Key: "ctrl+enter", Command: "execute-commit", Context: "git-commit"}
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

### Common mistakes
| Symptom | Fix |
|---------|-----|
| Shortcut doesn't work | Check Command ID matches in `Commands()` and `bindings.go`; verify `FocusContext()` returns matching context |
| Wrong/duplicate footer | Remove footer rendering from plugin's `View()` |
| Important hint truncated | Set lower Priority value (1=highest importance) |
| 'q' behavior wrong | Update `isRootContext()` in `internal/app/update.go` |

### Core files
| File | Purpose |
|------|---------|
| `internal/plugin/plugin.go` | `Command` struct, `Commands()`, `FocusContext()` interface |
| `internal/keymap/bindings.go` | Default key→command mappings |
| `internal/keymap/registry.go` | Runtime binding lookup, handler registration |
| `internal/app/update.go` | Key routing, `isRootContext()` |
| `internal/app/view.go` | Footer rendering |

### TD Monitor integration
TD Monitor uses dynamic shortcut export—TD is the single source of truth. See `docs/guides/keyboard-shortcuts-reference.md` for details.

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

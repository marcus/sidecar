# UI Feature Implementation Guide

This is the single entry point for Sidecar UI feature work: modals, keyboard shortcuts, and mouse support.
All new modals must use the internal modal library. See `docs/deprecated/guides/declarative-modal-guide.md` for the full API reference.

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
- See `docs/deprecated/guides/declarative-modal-guide.md` for the full API and patterns.
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

#### Async content invalidation

When a modal's content depends on data loaded asynchronously (network fetch, file read, etc.), the cache **must** be invalidated when that data arrives. The width-only cache check will not detect content changes.

```go
// BAD: Content loaded async but cache only checks width
case MyDataLoadedMsg:
    p.myData = msg.Data
    return p, nil  // Modal cache still has stale content!

// GOOD: Invalidate modal cache when content changes
case MyDataLoadedMsg:
    p.myData = msg.Data
    p.clearMyModal()  // Force rebuild with new content
    return p, nil
```

**Pattern**: Any `ensureModal()` that renders data from a field set by an async message handler must have a corresponding `clearModal()` call in that message handler.

**Common pitfall**: The modal renders correctly on second open (because close calls `clearModal()`), but the first display is stuck on placeholder content. This makes the bug intermittent and hard to spot during testing.

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

### Modal keyboard shortcuts and footer hints

Modals need their own focus context, commands, and bindings to get footer hints. Without this, the footer shows the parent view's commands while the modal is open.

**1. Return a dedicated context from `FocusContext()`:**
```go
func (p *Plugin) FocusContext() string {
    switch p.viewMode {
    case ViewModeError:
        return "git-error"
    case ViewModePushMenu:
        return "git-push-menu"
    default:
        return "git-status"
    }
}
```

**2. Add commands for the modal context in `Commands()`:**
```go
{ID: "dismiss", Name: "Dismiss", Context: "git-error", Priority: 1},
{ID: "yank-error", Name: "Yank", Context: "git-error", Priority: 2},
```

**3. Add bindings in `bindings.go`:**
```go
{Key: "y", Command: "yank-error", Context: "git-error"},
{Key: "esc", Command: "dismiss", Context: "git-error"},
```

**4. Intercept custom keys before `modal.HandleKey`:**

The modal library handles Tab, Enter, and Esc internally. To add shortcuts like yank, intercept them before delegating:
```go
func (p *Plugin) updateMyModal(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
    p.ensureMyModal()
    if p.myModal == nil {
        return p, nil
    }
    // Custom shortcuts first
    if msg.String() == "y" {
        return p, p.yankToClipboard()
    }
    // Then delegate to modal
    action, cmd := p.myModal.HandleKey(msg)
    if action == "dismiss" || action == "cancel" {
        return p.dismissModal()
    }
    return p, cmd
}
```

The footer automatically renders hints for the active context. No manual footer rendering needed.

### Modal notes
- `HandleKey` and `HandleMouse` already handle Tab, Shift+Tab, Enter, and Esc.
- Backdrop clicks return "cancel" by default; use `WithCloseOnBackdropClick(false)` to disable.
- Use built-in sections (Text, Input, Textarea, Buttons, Checkbox, List, When) before custom layouts.
- For bespoke layouts, use `modal.Custom` and return explicit focusable offsets.
- Do not render footers or hint lines in plugin View. The app renders the unified footer from Commands().
- `SetFocus(id)` auto-scrolls the viewport to the focused element. Plugins that manage their own focus state (e.g., workspace create modal with `createFocus` counter) should call `SetFocus()` after changing focus to trigger scroll-to-visible.

### Background colors in modals (critical)

Lipgloss `Background()` on a parent style does **not** cascade into child-rendered content. Each styled element's ANSI reset (`\x1b[0m`) clears all attributes including the parent's background, leaving terminal-default black for the remainder of that line.

**The problem**: A modal with `Background(styles.BgSecondary)` renders inner elements (input borders, dropdown items, hint text) that contain ANSI resets. After each reset, the background reverts to black instead of the modal's background color.

**Why naive fixes fail**:
- `lipgloss.Style.Width(w).Render(line)` — re-renders each line through lipgloss, which causes **wrapping artifacts** when lines contain complex ANSI sequences already at the target width.
- Appending background-padded spaces — only fixes the right margin, not mid-line black patches between styled elements.

**The solution** (`fillBackground` in `internal/modal/layout.go`): Replace ANSI resets within each viewport line with reset + background re-apply, then pad short lines with spaces. This avoids re-rendering through lipgloss (no wrapping) while maintaining uniform background.

```go
// Extract the raw ANSI background escape sequence
bgSeq := bgANSISeq() // renders a marker through lipgloss, takes the prefix

// For each viewport line:
// 1. Replace resets with reset + bg re-apply
line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bgSeq)
// 2. Pad short lines (no lipgloss Width — avoids wrapping)
if lipgloss.Width(line) < targetWidth {
    line += strings.Repeat(" ", targetWidth-lipgloss.Width(line))
}
```

**When to apply this pattern**: Any container with `Background()` that renders child content produced by separate `lipgloss.Style.Render()` calls. The ANSI reset problem affects modals, overlay panels, and any nested styled content.

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

For complete shortcut listings per plugin, see `docs/deprecated/guides/keyboard-shortcuts-reference.md`.

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

### Multiple contexts (view modes)

When your plugin has different modes (status view, diff view, modals), return different context strings from `FocusContext()`. Each context gets its own set of footer hints and key bindings.

```go
func (p *Plugin) FocusContext() string {
    switch p.viewMode {
    case ViewModeDiff:
        return "git-diff"
    case ViewModeCommit:
        return "git-commit"
    case ViewModeError:
        return "git-error"
    default:
        return "git-status"
    }
}
```

Define commands for each context separately in `Commands()`. The footer only shows commands matching the active context.

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

### Footer rendering flow
```
footerHints()
    ├── pluginFooterHints(activePlugin, context)
    │   └── Commands() filtered by FocusContext(), sorted by Priority
    └── globalFooterHints()
        └── App-level hints (plugins, help, quit)

renderHintLineTruncated(hints, availableWidth)
    └── Renders left-to-right until width exceeded
        (plugin hints first, then global)
```

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

### Text input contexts (critical for modals with inputs)

When a plugin view contains a text input (commit message, search box, modal with text field), implement `plugin.TextInputConsumer` and return `true` while text entry is active. This prevents app-level shortcuts from intercepting typed characters.

**To add text input handling in a plugin:**
1. Keep `FocusContext()` for footer hints and key context.
2. Implement `ConsumesTextInput() bool` and return `true` for active text-entry modes.

```go
// Plugin side
func (p *Plugin) FocusContext() string {
    if p.showMyModal {
        return "my-plugin-input" // Footer/context only
    }
    return "my-plugin"
}

func (p *Plugin) ConsumesTextInput() bool {
    return p.showMyModal
}
```

`isTextInputContext()` in `internal/app/update.go` is now fallback/app-owned. Use it for app-level contexts that are not driven by a plugin (or as compatibility fallback), not as the primary path for plugin input modes.

The app forwards all keys (except `ctrl+c`) to the active plugin while text input is consumed, bypassing global shortcuts like `i` (open issue), `r` (refresh), backtick (next plugin), etc.

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
| `internal/plugin/plugin.go` | `Command` struct, `Commands()`, `FocusContext()`, `TextInputConsumer` |
| `internal/keymap/bindings.go` | Default key→command mappings |
| `internal/keymap/registry.go` | Runtime binding lookup, handler registration |
| `internal/app/update.go` | Key routing, `isRootContext()` |
| `internal/app/view.go` | Footer rendering |

### TD Monitor integration
TD Monitor uses dynamic shortcut export—TD is the single source of truth. See `docs/deprecated/guides/keyboard-shortcuts-reference.md` for details.

### Testing
- Run `sidecar --debug` to inspect key handling.
- Press `?` to verify the command palette shows your bindings.
- Check footer hints in each context and at narrow widths.

## Scrollbar (internal/ui)

### RenderScrollbar Component

A 1-column vertical scrollbar track rendered alongside scrollable content. When all items fit the viewport, renders a spacer column (single spaces) to prevent layout jitter on resize.

### API

```go
ui.RenderScrollbar(ui.ScrollbarParams{
    TotalItems:   int, // Total logical items in the list
    ScrollOffset: int, // Index of first visible item
    VisibleItems: int, // Number of items that fit in the viewport
    TrackHeight:  int, // Height in terminal rows (usually == visible area height)
})
```

### Usage pattern

1. Reduce content width by 1 to reserve scrollbar space.
2. Render your content at the reduced width.
3. Call `ui.RenderScrollbar(...)` with current scroll state.
4. Join horizontally:

```go
contentWidth := width - 1
content := renderItems(contentWidth, height)
scrollbar := ui.RenderScrollbar(ui.ScrollbarParams{
    TotalItems:   len(items),
    ScrollOffset: p.scrollOffset,
    VisibleItems: visibleCount,
    TrackHeight:  height,
})
return lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
```

### Multi-line items

When each item renders as multiple terminal rows, set `TrackHeight` to the actual terminal row count, not the item count:

```go
TrackHeight: visibleCount * linesPerItem
```

### Characters and theming

| Element | Character | Theme key | Default |
|---------|-----------|-----------|---------|
| Track | `│` (U+2502) | `scrollbarTrack` | `TextSubtle` |
| Thumb | `┃` (U+2503) | `scrollbarThumb` | `TextMuted` |

Theme keys are optional overrides via `styles.ScrollbarTrackColor` and `styles.ScrollbarThumbColor`.

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

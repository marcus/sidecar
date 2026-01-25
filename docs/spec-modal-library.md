# Plan: First-Class Modal Library (`internal/modal/`)

## Goal

Create a declarative modal library that eliminates off-by-one hit region bugs, enforces full mouse/keyboard/hover conformity, and makes it foolproof to create uniform modals.

## Core Innovation: Render-Then-Measure Hit Regions

Instead of manually tracking `currentY` through content (the #1 source of bugs), the library:

1. Renders each section individually (width-constrained to `contentWidth`)
2. **Measures** the actual line count of each rendered section
3. Accumulates Y positions from measured heights
4. Registers hit regions at the measured positions

This guarantees hit regions match rendered output — wrapping, conditional content, bordered inputs, and conditional sections are all handled automatically.
Line measurement must trim trailing newlines and return 0 for empty content (use `lipgloss.Height` on the trimmed string).

## Package Structure

```
internal/modal/
    modal.go       -- Modal type, Render(), HandleKey(), HandleMouse()
    section.go     -- Section interface + built-in types (Text, Input, Buttons, etc.)
    layout.go      -- Layout engine: per-section render + measurement + hit registration
    options.go     -- Functional options (WithWidth, WithVariant, WithHints, WithPrimaryAction, WithCloseOnBackdropClick)
    modal_test.go  -- Tests for hit region accuracy, keyboard nav, hover
```

## Core API

### Creating a Modal

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

### Rendering (in View)

```go
func (p *Plugin) renderDeleteView(width, height int) string {
    background := p.renderListView(width, height)
    rendered := p.deleteModal.Render(width, height, p.mouseHandler)
    return ui.OverlayModal(background, rendered, width, height)
}
```

### Key/Mouse Handling (in Update)

```go
case tea.KeyMsg:
    action, cmd := p.deleteModal.HandleKey(msg)
    // action = "delete", "cancel", or "" (internal nav)
    // cmd = bubbles cursor blink, etc.

case tea.MouseMsg:
    action := p.deleteModal.HandleMouse(msg, p.mouseHandler)
    // action = "delete", "cancel", or "" (hover update)
```

`HandleKey` returns an optional `tea.Cmd` to support bubbles models (cursor blink, etc.).
Backdrop clicks return `"cancel"` by default; set `WithCloseOnBackdropClick(false)` to disable.

## Core Types

### Modal

```go
type Modal struct {
    title     string
    variant   Variant       // Default, Danger, Warning, Info
    width     int
    sections  []Section
    showHints bool          // "Tab to switch · Enter to confirm"
    primaryAction string    // Action ID for implicit submit
    closeOnBackdrop bool    // Click outside modal returns "cancel" when true

    // State (managed internally)
    focusIdx  int           // Current focused element index
    hoverID   string        // Currently hovered element ID
    focusIDs  []string      // Ordered list of focusable IDs (built during Render)
    scrollOffset int        // Content scroll position in lines
}
```

### Section Interface

```go
type Section interface {
    Render(contentWidth int, focusID, hoverID string) RenderedSection
    Update(msg tea.Msg, focusID string) (action string, cmd tea.Cmd)
}

type RenderedSection struct {
    Content    string
    Focusables []FocusableInfo
}

type FocusableInfo struct {
    ID      string
    OffsetX int // Relative to section top-left in content area
    OffsetY int // Relative to section top-left in content area
    Width   int
    Height  int
}
```

### Built-in Sections

| Section                             | Description                       | Lines                           |
| ----------------------------------- | --------------------------------- | ------------------------------- |
| `Text(s)`                           | Static text (supports multi-line) | measured                        |
| `Spacer()`                          | Blank line                        | 1                               |
| `Input(id, *textinput.Model, opts)` | Labeled text input                | measured (typically 2 or 4)     |
| `Textarea(id, *textarea.Model, h)`  | Multi-line editor                 | measured (typically h+2)        |
| `Buttons(btn...)`                   | Button row with focus/hover       | 1 (per-button hit regions)      |
| `Checkbox(id, label, *bool)`        | Toggle checkbox                   | 1                               |
| `List(id, items, *idx, opts)`       | Scrollable item list              | opts.MaxVisible                 |
| `When(fn, section)`                 | Conditional section               | 0 or section height             |
| `Custom(renderFn, focusables)`      | Escape hatch for complex content  | measured                        |

Input options:

```go
func WithSubmitOnEnter(bool) InputOption
func WithSubmitAction(actionID string) InputOption // Defaults to modal primaryAction
```

### Button Helper

```go
func Btn(label, id string, opts ...BtnOption) ButtonDef
func BtnDanger() BtnOption  // Use danger color scheme
func BtnPrimary() BtnOption // Use primary (focused) as default
```

## Layout Engine (The Key Piece)

```go
func (m *Modal) buildLayout(screenW, screenH int, handler *mouse.Handler) string {
    modalWidth := clamp(m.width, minModalWidth, screenW-4)
    contentWidth := modalWidth - 6 // border(2) + padding(4)

    // 1. Render sections individually, measure heights
    type renderedSection struct {
        content string
        height  int
        focusables []FocusableInfo
    }
    var rendered []renderedSection
    for _, s := range m.sections {
        res := s.Render(contentWidth, m.currentFocusID(), m.hoverID)
        height := measureHeight(res.Content)
        rendered = append(rendered, renderedSection{
            content: res.Content,
            height: height,
            focusables: res.Focusables,
        })
    }

    // 2. Join full content
    var parts []string
    for _, r := range rendered {
        parts = append(parts, r.content)
    }
    fullContent := strings.Join(parts, "\n")

    // 3. Compute scroll viewport
    contentHeight := measureHeight(fullContent)
    modalInnerHeight := desiredModalInnerHeight(screenH) // border/padding excluded
    headerLines := 0
    if m.title != "" {
        headerLines = 2 // title + blank line
    }
    footerLines := hintLines(m.showHints)
    maxViewportHeight := max(1, modalInnerHeight-headerLines-footerLines)
    viewportHeight := min(maxViewportHeight, max(1, contentHeight))
    m.scrollOffset = clamp(m.scrollOffset, 0, max(0, contentHeight-viewportHeight))
    viewport := sliceLines(fullContent, m.scrollOffset, viewportHeight)

    // 4. Apply modal style
    inner := joinNonEmpty([]string{
        renderTitleLine(m.title),
        viewport,
        renderHintLine(m.showHints),
    })
    styled := m.modalStyle().Width(modalWidth).Render(inner)
    modalH := lipgloss.Height(styled)
    modalX := (screenW - modalWidth) / 2
    modalY := (screenH - modalH) / 2

    // 5. Register hit regions from measured positions
    handler.HitMap.Clear()

    // Background absorber (added first = lowest priority)
    handler.HitMap.AddRect("modal-backdrop", 0, 0, screenW, screenH, nil)

    // Modal body absorber
    handler.HitMap.AddRect("modal-body", modalX, modalY, modalWidth, modalH, nil)

    // Focusable elements (added last = highest priority)
    contentX := modalX + 2 + 1  // border(1) + padding(2)
    contentY := modalY + 1 + 1 + headerLines  // border(1) + padding(1) + header
    currentY := 0
    for _, r := range rendered {
        for _, f := range r.focusables {
            x := contentX + f.OffsetX
            y := contentY + currentY + f.OffsetY - m.scrollOffset
            if intersectsViewport(y, f.Height, contentY, viewportHeight) {
                handler.HitMap.AddRect(f.ID, x, y, f.Width, f.Height, f.ID)
            }
        }
        currentY += r.height
    }

    return styled
}
```

`measureHeight` trims trailing newlines and returns 0 for empty strings before calling `lipgloss.Height`.
`desiredModalInnerHeight` clamps to the available screen height; if content fits, the modal shrinks to content height, otherwise it scrolls within the fixed viewport.
Width is clamped between `minModalWidth` and `screenW-4` to prevent negative offsets on narrow terminals.
`sliceLines` must truncate to `viewportHeight` and only pad with blank lines when scrolling is enabled (content taller than viewport).

**Why this eliminates off-by-one errors:**

- `currentY` advances by **measured** heights, not predicted heights
- Border+padding offsets are constants and include header lines explicitly
- Each section manages its own internal offsets (e.g., a bordered input knows its label is at offset 0, the input at offset 1)
- Conditional sections (`When`) render to 0 lines when inactive — measured correctly
- Text wrapping is captured by the line-count measurement

## Keyboard Navigation

Built-in, fully automatic:

- **Tab** / **Shift+Tab**: Cycle through `focusIDs` list
- **Enter**: Return the focused element's ID as action (inputs can opt into submit-on-enter)
- **Esc**: Return `"cancel"`
- Keys forwarded to focused section via `Section.Update`

`HandleKey` routes keystrokes to the focused section first (except for Tab/Shift+Tab/Esc), and returns the section action if one is emitted.
Single-line inputs may submit on Enter; if no input-specific action is set, use `primaryAction`. Textareas always treat Enter as a newline.

## Hover States

Built-in, fully automatic:

- `HandleMouse` updates `m.hoverID` on `ActionHover`
- Each section's `Render()` receives current `hoverID`
- `Buttons` section uses `ResolveButtonStyle(focusIdx, hoverIdx, btnIdx)` internally
- Focus always takes precedence over hover (existing pattern preserved)

## Scrolling (Content Only)

- Title, border, and hints stay fixed; only the content area scrolls.
- Mouse wheel scrolls **only** when the pointer is over the modal body.
- Focus changes only scroll if the focused element would be clipped (minimal scroll to reveal).
- Hit regions are clipped to the content viewport so offscreen elements cannot be clicked.

## Variant Styling

```go
const (
    VariantDefault Variant = iota  // Primary border color
    VariantDanger                   // Red border, danger button styles
    VariantWarning                  // Yellow/amber border
    VariantInfo                     // Blue border
)
```

Variants control: border color, primary button style (danger buttons use `ButtonDanger*` styles), and title styling.

## Implementation Steps

### Step 1: Core Framework

- `modal.go`: `Modal` struct, `New()`, `AddSection()`, `Render()`, `HandleKey()`, `HandleMouse()`
- `options.go`: `WithWidth`, `WithVariant`, `WithHints`, `WithPrimaryAction`, `WithCloseOnBackdropClick`
- `layout.go`: `buildLayout()` with measure-and-register + scrolling

### Step 2: Built-in Sections

- `section.go`: `Section` interface, `Text`, `Spacer`, `Buttons`, `When`, `Custom`
- Button rendering with focus/hover/danger styles

### Step 3: Input Sections

- `Input` section wrapping `textinput.Model`
- `Textarea` section wrapping `textarea.Model`
- `Checkbox` section with toggle state

### Step 4: List/Dropdown Section

- Scrollable item list with cursor
- Hover highlighting
- Max visible height constraint

### Step 5: Tests

- Hit region accuracy tests (render + verify region positions match)
- Per-button hit region tests (button rows)
- Keyboard nav cycling tests
- Submit-on-enter tests for inputs
- Hover state tests
- Conditional section height tests
- Text wrapping tests
- Scroll viewport + hit region clipping tests

### Step 6: Migration — Confirm Dialogs

- Migrate `ConfirmDialog` to use `modal.Modal` internally (or provide `modal.ConfirmDialogModal()` factory)
- Migrate git confirm_discard and confirm_stash_pop
- Migrate app-level quit confirm

### Step 7: Migration — Form Modals

- Migrate workspace create modal (the complex one)
- Migrate git commit modal
- Verify hit regions match existing behavior

## Files to Create

| File                           | Purpose                                                           |
| ------------------------------ | ----------------------------------------------------------------- |
| `internal/modal/modal.go`      | Core Modal type, Render, HandleKey, HandleMouse                   |
| `internal/modal/section.go`    | Section interface + Text, Spacer, Buttons, Checkbox, When, Custom |
| `internal/modal/input.go`      | Input and Textarea sections wrapping bubbles models               |
| `internal/modal/list.go`       | List/Dropdown section with scrolling                              |
| `internal/modal/layout.go`     | Layout engine (render-measure-register)                           |
| `internal/modal/options.go`    | Functional options and Variant enum                               |
| `internal/modal/modal_test.go` | Core tests                                                        |

## Files to Modify (Migration Phase)

| File                                              | Change                                      |
| ------------------------------------------------- | ------------------------------------------- |
| `internal/ui/confirm_dialog.go`                   | Add `ToModal() *modal.Modal` adapter method |
| `internal/plugins/gitstatus/confirm_discard.go`   | Replace manual render with modal.Modal      |
| `internal/plugins/gitstatus/confirm_stash_pop.go` | Replace manual render with modal.Modal      |
| `internal/app/view.go`                            | Quit confirm → modal.Modal                  |

## Verification

1. `go build ./...` — compiles clean
2. `go test ./internal/modal/...` — all tests pass
3. `go test ./...` — no regressions
4. Manual test: click buttons in migrated modals, verify hit regions are pixel-accurate
5. Manual test: Tab/Shift+Tab cycles correctly, Enter triggers correct action
6. Manual test: hover state shows on mouse-over, disappears on mouse-out

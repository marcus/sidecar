---
name: create-modal
description: Create declarative modals using the modal library API. Covers modal types (confirm, input, select, form), sections (Text, Buttons, Input, Textarea, Checkbox, Select, List, Combo, When, Custom), rendering with OverlayModal, and keyboard/mouse handling. Use when adding modals or dialogs to the application.
---

# Creating Declarative Modals

Use the `internal/modal` package. The library handles keyboard navigation, mouse hit regions, hover states, and scrolling automatically.

## Quick Start

```go
import "github.com/marcus/sidecar/internal/modal"

// 1. Create the modal
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

// 2. Render in View
func (p *Plugin) View(width, height int) string {
    background := p.renderListView(width, height)
    rendered := p.myModal.Render(width, height, p.mouseHandler)
    return ui.OverlayModal(background, rendered, width, height)
}

// 3. Handle input in Update
case tea.KeyMsg:
    action, cmd := p.myModal.HandleKey(msg)
    if action != "" {
        return p.handleAction(action) // "delete", "cancel", etc.
    }
    return p, cmd

case tea.MouseMsg:
    action := p.myModal.HandleMouse(msg, p.mouseHandler)
    if action != "" {
        return p.handleAction(action)
    }
    return p, nil
```

## Critical: Modal Initialization Pattern

The modal must exist before input handling. Create an `ensure` function called in **both** View and Update:

```go
func (p *Plugin) ensureMyModal() {
    if p.targetItem == nil {
        return // Required state missing
    }

    modalW := 50
    if modalW > p.width-4 {
        modalW = p.width - 4
    }
    if modalW < 20 {
        modalW = 20
    }

    // Only rebuild if needed
    if p.myModal != nil && p.myModalWidthCache == modalW {
        return
    }
    p.myModalWidthCache = modalW

    p.myModal = modal.New("Title", modal.WithWidth(modalW), ...).
        AddSection(...)
}
```

**Call `ensureModal()` before the nil check in key handlers:**

```go
func (p *Plugin) handleMyModalKeys(msg tea.KeyMsg) tea.Cmd {
    p.ensureMyModal()  // CRITICAL: Before nil check
    if p.myModal == nil {
        return nil
    }
    action, cmd := p.myModal.HandleKey(msg)
    // ...
}
```

Without this, the first keypress after opening drops because View runs after Update in bubbletea.

## Constructor and Options

```go
m := modal.New(title string, opts ...Option)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithWidth(int)` | Modal width in characters | 50 |
| `WithVariant(Variant)` | Visual style | `VariantDefault` |
| `WithPrimaryAction(string)` | Action ID for Enter on inputs | "" |
| `WithHints(bool)` | Show "Tab to switch..." hint | true |
| `WithCloseOnBackdropClick(bool)` | Backdrop click returns "cancel" | true |

**Variants:** `VariantDefault`, `VariantDanger` (red), `VariantWarning` (yellow), `VariantInfo` (blue)

## Built-in Sections

### Text and Spacer
```go
modal.Text("Static text with auto line wrapping")
modal.Spacer()  // Single blank line
```

### Buttons
```go
modal.Buttons(
    modal.Btn(" Save ", "save"),              // Standard button
    modal.Btn(" Delete ", "delete", modal.BtnDanger()),  // Red
    modal.Btn(" Submit ", "submit", modal.BtnPrimary()), // Primary
    modal.Btn(" Cancel ", "cancel"),
)
```
- Include padding in labels: `" Save "` not `"Save"`
- Button IDs are returned as actions
- Tab/Shift+Tab cycles focus

### Input
```go
var nameInput textinput.Model
modal.Input("name-input", &nameInput)
modal.InputWithLabel("name-input", "Name:", &nameInput)
modal.Input("name-input", &nameInput,
    modal.WithSubmitOnEnter(true),       // Default: true
    modal.WithSubmitAction("submit"),    // Override primary action
)
```

### Textarea
```go
var msgArea textarea.Model
modal.Textarea("message", &msgArea, 5)          // height in lines
modal.TextareaWithLabel("message", "Label:", &msgArea, 5)
```
- Enter inserts newlines (never submits)

### Combo (floating dropdown)
```go
items := []modal.DropdownItem{
    {ID: "main", Label: "main", Value: "main"},
    {ID: "dev", Label: "dev", Value: "dev"},
}
var selectedIdx int
modal.Combo("branch", &branchInput, items, &selectedIdx)
```
- Single-line input; filtered results float over later sections (modal height does not change)
- `selected` is an **items** index (same as List)
- Typing filters and selects the top match; up/down move the highlight
- Enter commits the highlight and, by default, returns the modal primary action
- Tab commits and moves focus; Esc closes the overlay without cancelling the modal
- Click an overlay row to commit without submitting

### Checkbox
```go
var includeFiles bool
modal.Checkbox("include-files", "Include untracked files", &includeFiles)
```
- Space toggles
- Enter does **not** toggle; it submits the modal primary action (if any)

### Select (one choice out of a set)
```go
items := []modal.SelectItem{
    {ID: "shell", Label: "Shell", Description: "new agent/shell session"},
    {ID: "worktree", Label: "Worktree", Description: "shell in a new worktree"},
}
var selectedIdx int
modal.Select("kind", items, &selectedIdx,
    modal.WithMaxVisible(6),
    modal.WithDisabled(func(i int) string { return reasons[i] }),
    modal.WithOnSelect(func(i int) { rebuildAround(i) }),
)
```
- The default control for a single choice: sort, filter, kind. `modal.List` is the low-level column of rows for lists that are not a single choice.
- Two shapes, chosen by count: a segmented `[ A | B | C ]` under five choices, a `❯`-cursor full-width list with an aligned description column at five or more (and the list whenever the segments would not fit the width). `WithShape(modal.ShapeList)` / `WithShape(modal.ShapeSegmented)` forces one.
- Arrows and h/j/k/l move by one and stop at the ends; home/end jump; Enter activates.
- `WithDisabled(func(i int) string)` keeps a choice visible and muted with its reason in place of its description, and makes it unreachable by key or click.
- `WithMaxVisible(n)` scrolls the rest, with `↑ more above` / `↓ more below`.
- A click resolves to a row inside the section — hosts add no glue — and focuses the control.
- `WithOnSelect(func(i int))` reports every change; `WithSelectAction(id)` makes activation return a fixed action instead of the row's ID, for a selector embedded in a form.
- See `docs/reference/design-language.md` ("Selectors").

### List
```go
items := []modal.ListItem{
    {ID: "item-1", Label: "First item", Data: someValue},
    {ID: "item-2", Label: "Second item"},
}
var selectedIdx int
modal.List("my-list", items, &selectedIdx, modal.WithMaxVisible(5))
```
- j/k or up/down moves selection; Enter returns selected item's ID

### When (Conditional)
```go
modal.When(func() bool { return showWarning },
    modal.Text("Warning: This action is irreversible!"),
)
```

### Custom
```go
modal.Custom(
    func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
        return modal.RenderedSection{
            Content: content,
            Focusables: []modal.FocusableInfo{
                {ID: "custom-btn", OffsetX: 0, OffsetY: 2, Width: 10, Height: 1},
            },
        }
    },
    func(msg tea.Msg, focusID string) (string, tea.Cmd) {
        return "", nil  // can be nil if no custom input handling
    },
)
```

## Handling Input

### Keyboard

```go
action, cmd := m.HandleKey(msg)
```

| Key | Behavior |
|-----|----------|
| Tab | Focus next element |
| Shift+Tab | Focus previous element |
| Enter | Return focused element's ID (or primaryAction for inputs). Checkbox Enter submits primary without toggling. Combo Enter commits then submits. |
| Esc | Offered to the focused section first (Combo closes its overlay). Otherwise "cancel". |
| Other | Forwarded to focused section |

### Mouse

```go
action := m.HandleMouse(msg, p.mouseHandler)
```

| Event | Behavior |
|-------|----------|
| Click backdrop | Return "cancel" (if enabled) |
| Click button/checkbox | Return element ID |
| Hover element | Update hover state |
| Scroll on modal | Scroll content |

## Modal Methods

```go
m.FocusedID() string   // Currently focused element ID
m.HoveredID() string   // Currently hovered element ID
m.SetFocus(id string)  // Focus specific element
m.Reset()              // Reset focus, hover, scroll to initial state
```

## Rendering Rules

Always use `ui.OverlayModal` for dimmed background:
```go
func (p *Plugin) View(width, height int) string {
    background := p.renderNormalView(width, height)
    rendered := p.myModal.Render(width, height, p.mouseHandler)
    return ui.OverlayModal(background, rendered, width, height)
}
```

**Do not:**
- Pre-center modal content with `lipgloss.Place` (OverlayModal handles centering)
- Render footers or hint lines in plugin View (app renders unified footer)

## State Management

- Focus state persists across renders
- Call `Reset()` when closing and reopening modals
- Width caching should include state-dependent changes

## Troubleshooting

| Issue | Solution |
|-------|----------|
| First keypress dropped | Call `ensureModal()` before nil check in Update |
| Modal too wide/narrow | Use width clamping: `modalW > p.width-4` |
| Hover not updating | Pass `mouseHandler` to both `Render` and `HandleMouse` |
| Input not receiving keys | Check `FocusedID()` |
| Modal rebuilds every frame | Cache by width |
| Modal shows with wrong focus | Call `m.Reset()` when showing modal |

See `references/complete-example.md` for a full plugin implementation with delete confirmation modal.

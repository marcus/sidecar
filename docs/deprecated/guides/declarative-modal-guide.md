# Declarative Modal Library Guide

This guide covers the `internal/modal` package for building modals in Sidecar. The library handles keyboard navigation, mouse hit regions, hover states, and scrolling automatically.

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

## Modal Initialization (Critical Pattern)

The modal must exist before input handling. Create an `ensure` function called in **both** View and Update:

```go
func (p *Plugin) ensureMyModal() {
    if p.targetItem == nil {
        return // Required state missing
    }

    // Calculate width with bounds
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

## Creating Modals

### Constructor and Options

```go
m := modal.New(title string, opts ...Option)
```

**Options:**

| Option | Description | Default |
|--------|-------------|---------|
| `WithWidth(int)` | Modal width in characters | 50 |
| `WithVariant(Variant)` | Visual style (see below) | `VariantDefault` |
| `WithPrimaryAction(string)` | Action ID for Enter on inputs | "" |
| `WithHints(bool)` | Show "Tab to switch..." hint line | true |
| `WithCloseOnBackdropClick(bool)` | Backdrop click returns "cancel" | true |

**Variants:**

```go
modal.VariantDefault  // Primary border color
modal.VariantDanger   // Red border, danger button styles
modal.VariantWarning  // Yellow/amber border
modal.VariantInfo     // Blue border
```

### Adding Sections

Chain `AddSection()` calls to build content:

```go
m := modal.New("Title").
    AddSection(modal.Text("Description")).
    AddSection(modal.Spacer()).
    AddSection(modal.Input("name", &nameInput)).
    AddSection(modal.Buttons(...))
```

## Built-in Sections

### Text

Static text with automatic line wrapping:

```go
modal.Text("Name: " + name)
modal.Text("This is a longer message that will\nwrap across multiple lines.")
```

### Spacer

Single blank line for visual separation:

```go
modal.Spacer()
```

### Buttons

Horizontal button row with automatic focus/hover styling:

```go
modal.Buttons(
    modal.Btn(" Save ", "save"),
    modal.Btn(" Cancel ", "cancel"),
)

// Danger button (red styling)
modal.Btn(" Delete ", "delete", modal.BtnDanger())

// Explicitly mark as primary (default for focused state)
modal.Btn(" Submit ", "submit", modal.BtnPrimary())
```

- Button IDs are returned as actions when clicked or Enter is pressed
- Tab/Shift+Tab cycles focus between buttons
- Include padding in labels (e.g., `" Save "` not `"Save"`)

### Input

Single-line text input wrapping `bubbles/textinput`:

```go
var nameInput textinput.Model

// Basic input
modal.Input("name-input", &nameInput)

// With label
modal.InputWithLabel("name-input", "Name:", &nameInput)

// Options
modal.Input("name-input", &nameInput,
    modal.WithSubmitOnEnter(true),       // Default: true
    modal.WithSubmitAction("submit"),    // Override primary action
)
```

- Enter submits (returns `submitAction` or modal's `primaryAction`)
- Input model is updated in place

### Textarea

Multi-line text editor wrapping `bubbles/textarea`:

```go
var msgArea textarea.Model

// Basic textarea (height in lines)
modal.Textarea("message", &msgArea, 5)

// With label
modal.TextareaWithLabel("message", "Commit message:", &msgArea, 5)
```

- Enter inserts newlines (never submits)
- Textarea model is updated in place

### Checkbox

Toggle checkbox with state pointer:

```go
var includeFiles bool
modal.Checkbox("include-files", "Include untracked files", &includeFiles)
```

- Enter or Space toggles the checkbox
- State is updated in place via pointer

### List

Scrollable item list with selection:

```go
items := []modal.ListItem{
    {ID: "item-1", Label: "First item", Data: someValue},
    {ID: "item-2", Label: "Second item"},
    {ID: "item-3", Label: "Third item"},
    // Data field is optional and can hold any associated value
}
var selectedIdx int

modal.List("my-list", items, &selectedIdx,
    modal.WithMaxVisible(5),  // Default: 5
)
```

- j/k or up/down moves selection
- home/end jumps to first/last item
- Enter returns selected item's ID as action
- Shows scroll indicators when content overflows

### When (Conditional)

Conditionally render a section:

```go
modal.When(func() bool { return showWarning },
    modal.Text("Warning: This action is irreversible!"),
)
```

- When condition is false, section takes 0 lines (not rendered)
- Hit regions are only registered when visible

### Custom

Escape hatch for complex layouts:

```go
modal.Custom(
    func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
        // Render custom content
        content := renderMyCustomContent(contentWidth)

        // Return focusables for hit regions (optional)
        return modal.RenderedSection{
            Content: content,
            Focusables: []modal.FocusableInfo{
                {ID: "custom-btn", OffsetX: 0, OffsetY: 2, Width: 10, Height: 1},
            },
        }
    },
    func(msg tea.Msg, focusID string) (string, tea.Cmd) {
        // Handle input when focused (optional, can be nil)
        return "", nil
    },
)
```

The `updateFn` parameter can be `nil` if no custom input handling is needed.

## Handling Input

### Keyboard

`HandleKey` returns an action string and optional tea.Cmd:

```go
action, cmd := m.HandleKey(msg)
```

**Built-in key handling:**

| Key | Behavior |
|-----|----------|
| Tab | Focus next element |
| Shift+Tab | Focus previous element |
| Enter | Return focused element's ID (or primaryAction for inputs) |
| Esc | Return "cancel" |
| Other | Forwarded to focused section |

**Action flow:**

1. If `action != ""`, handle it (close modal, execute command, etc.)
2. If `action == ""`, return the `cmd` for cursor blink, etc.

### Mouse

`HandleMouse` returns an action string:

```go
action := m.HandleMouse(msg, p.mouseHandler)
```

**Built-in mouse handling:**

| Event | Behavior |
|-------|----------|
| Click backdrop | Return "cancel" (if enabled) |
| Click button/checkbox | Return element ID |
| Click modal body | Absorb (no action) |
| Hover element | Update hover state |
| Scroll on modal | Scroll content |

## Modal Methods

```go
// State inspection
m.FocusedID() string  // Currently focused element ID
m.HoveredID() string  // Currently hovered element ID

// State manipulation
m.SetFocus(id string) // Focus specific element by ID
m.Reset()             // Reset focus, hover, scroll to initial state
```

## State Management

- **Focus state persists across renders** - Once an element is focused, it remains focused until explicitly changed or the modal is reset
- **Call `Reset()` when closing and reopening modals** - This ensures the modal starts with the correct initial focus state rather than stale focus from a previous interaction
- **Width caching should include state-dependent changes** - If your modal content changes based on state (not just width), include those state values in your cache check

## Rendering

Always use `ui.OverlayModal` for the dimmed background effect:

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

## Complete Example

```go
type Plugin struct {
    // ...
    deleteModal      *modal.Modal
    deleteModalWidth int
    targetWorktree   *Worktree
    mouseHandler     *mouse.Handler
}

func (p *Plugin) ensureDeleteModal() {
    if p.targetWorktree == nil {
        return
    }

    modalW := 58
    if modalW > p.width-4 {
        modalW = p.width - 4
    }
    if modalW < 30 {
        modalW = 30
    }

    if p.deleteModal != nil && p.deleteModalWidth == modalW {
        return
    }
    p.deleteModalWidth = modalW

    wt := p.targetWorktree
    p.deleteModal = modal.New("Delete Worktree?",
        modal.WithWidth(modalW),
        modal.WithVariant(modal.VariantDanger),
        modal.WithPrimaryAction("delete"),
    ).
        AddSection(modal.Text("Name: " + wt.Name)).
        AddSection(modal.Text("Path: " + wt.Path)).
        AddSection(modal.Spacer()).
        AddSection(modal.Buttons(
            modal.Btn(" Delete ", "delete", modal.BtnDanger()),
            modal.Btn(" Cancel ", "cancel"),
        ))
}

func (p *Plugin) handleDeleteModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    p.ensureDeleteModal()
    if p.deleteModal == nil {
        return p, nil
    }

    action, cmd := p.deleteModal.HandleKey(msg)
    switch action {
    case "delete":
        return p.executeDelete()
    case "cancel":
        p.showingDeleteModal = false
        p.deleteModal = nil
        return p, nil
    }
    return p, cmd
}

func (p *Plugin) handleDeleteModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    if p.deleteModal == nil {
        return p, nil
    }

    action := p.deleteModal.HandleMouse(msg, p.mouseHandler)
    switch action {
    case "delete":
        return p.executeDelete()
    case "cancel":
        p.showingDeleteModal = false
        p.deleteModal = nil
        return p, nil
    }
    return p, nil
}

func (p *Plugin) renderDeleteView(width, height int) string {
    p.ensureDeleteModal()
    background := p.renderListView(width, height)
    rendered := p.deleteModal.Render(width, height, p.mouseHandler)
    return ui.OverlayModal(background, rendered, width, height)
}
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| First keypress dropped | Call `ensureModal()` before nil check in Update |
| Modal too wide/narrow | Use width clamping: `modalW > p.width-4` |
| Hit regions misaligned | Library handles this automatically; check section order |
| Hover not updating | Pass `mouseHandler` to both `Render` and `HandleMouse` |
| Input not receiving keys | Ensure input section is focused (check `FocusedID()`) |
| Modal rebuilds every frame | Cache by width: `if p.myModal != nil && p.cachedWidth == modalW` |
| Modal shows with wrong focus | Call `m.Reset()` when showing modal |

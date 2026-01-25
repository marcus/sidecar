> DEPRECATED: Superseded by `docs/guides/ui-feature-guide.md`. Modal creation must follow `docs/spec-modal-library.md`.

# Modal Overlay Implementation Guide

This guide covers how to implement modals with dimmed backgrounds in Sidecar.

## Overview

Modals should dim the background to:
- Draw user focus to the modal content
- Provide visual separation between modal and underlying content
- Create a consistent, polished UX across the application

## Two Approaches

### 1. Solid Black Overlay (Hides Background)

Use `lipgloss.Place()` with whitespace options when you want to **completely hide** the background:

```go
func (m Model) renderMyModal(content string) string {
    modal := styles.ModalBox.Render(content)

    return lipgloss.Place(
        m.width, m.height,
        lipgloss.Center, lipgloss.Center,
        modal,
        lipgloss.WithWhitespaceChars(" "),
        lipgloss.WithWhitespaceForeground(lipgloss.Color("#000000")),
    )
}
```

**How it works:**
- `lipgloss.Place()` centers the modal and fills surrounding space with spaces
- The spaces use the terminal's default background color
- Background content is **hidden**, not dimmed

**Note:** `WithWhitespaceForeground()` sets the foreground color of space characters, which are invisible. This does NOT create visible dimming.

**Examples:** None currently - all modals use dimmed background overlay.

### 2. Dimmed Background Overlay (Shows Background)

Use `ui.OverlayModal()` when you want to show **dimmed background content** behind the modal. This works for both app-level and plugin-level modals:

```go
// App-level modal (internal/app/view.go):
func (m Model) renderMyModal(background string) string {
    modal := styles.ModalBox.Render(content)
    return ui.OverlayModal(background, modal, m.width, m.height)
}

// Plugin-level modal:
func (p *Plugin) renderMyModal() string {
    background := p.renderNormalView()
    modalContent := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(styles.Primary).
        Padding(1, 2).
        Width(modalWidth).
        Render(content)
    return ui.OverlayModal(background, modalContent, p.width, p.height)
}
```

**How `ui.OverlayModal()` works:**
1. Calculates modal position (centered horizontally and vertically)
2. Strips ANSI codes from background and applies dim gray styling (color 242)
3. Composites each row: `dimmed-left + modal + dimmed-right`
4. Shows dimmed background on all four sides of the modal

Note: Background colors are not preserved because ANSI SGR 2 (faint) doesn't reliably combine with existing color codes in most terminals. The gray overlay provides consistent dimming.

**Visual result:**
```
╔════════════════════════════════════════════════╗
║  [dimmed gray background text]                 ║
║  [gray left]  ┌─Modal─┐  [gray right]          ║
║  [gray left]  │ text  │  [gray right]          ║
║  [gray left]  └───────┘  [gray right]          ║
║  [dimmed gray background text]                 ║
╚════════════════════════════════════════════════╝
```

**Examples:** Command palette, Help modal, Diagnostics modal, Quit modal, Git commit modal, Push menu, Branch picker

## Implementation Checklist

When adding a modal:

1. **Decide on the visual effect:**
   - Hide background completely → Use `lipgloss.Place()` with whitespace options
   - Show dimmed background → Use `ui.OverlayModal()`

2. **For dimmed background modals** (preferred for most cases):
   - Import `github.com/marcus/sidecar/internal/ui`
   - Call `ui.OverlayModal(background, modalContent, width, height)`
   - Pass the full background content (the function handles dimming)
   - Pass raw modal content (don't pre-center with `lipgloss.Place()`)

3. **For solid overlay modals** (hides background):
   ```go
   lipgloss.WithWhitespaceChars(" "),
   lipgloss.WithWhitespaceForeground(lipgloss.Color("#000000")),
   ```

## Interactive Modal Buttons

All modals with user actions should use interactive buttons instead of key hints like `[Enter] Confirm [Esc] Cancel`.

### Button Rendering Pattern

```go
// In plugin struct, add:
fileOpButtonFocus int // 0=input, 1=confirm, 2=cancel

// In modal render function:
confirmStyle := styles.Button
cancelStyle := styles.Button
if p.fileOpButtonFocus == 1 {
    confirmStyle = styles.ButtonFocused
}
if p.fileOpButtonFocus == 2 {
    cancelStyle = styles.ButtonFocused
}

sb.WriteString("\n\n")
sb.WriteString(confirmStyle.Render(" Confirm "))
sb.WriteString("  ")
sb.WriteString(cancelStyle.Render(" Cancel "))
```

### Keyboard Navigation

- **Tab**: Cycle focus between input field and buttons (input → confirm → cancel → input)
- **Shift+Tab**: Reverse cycle
- **Enter**: Execute focused button (or confirm from input)
- **Esc**: Always cancels (global shortcut)

```go
case "tab":
    p.fileOpButtonFocus = (p.fileOpButtonFocus + 1) % 3
    if p.fileOpButtonFocus == 0 {
        p.fileOpTextInput.Focus()
    } else {
        p.fileOpTextInput.Blur()
    }
    return p, nil
```

### Mouse Support

Register hit regions for buttons during render:

```go
// In mouse.go, add region constants:
const (
    regionFileOpConfirm = "file-op-confirm"
    regionFileOpCancel  = "file-op-cancel"
)

// Register hit regions (calculate positions based on modal layout)
p.mouseHandler.HitMap.AddRect(regionFileOpConfirm, x, y, 10, 1, nil)
p.mouseHandler.HitMap.AddRect(regionFileOpCancel, x+15, y, 10, 1, nil)
```

Handle clicks in the mouse handler:

```go
case regionFileOpConfirm:
    return p.executeFileOp()
case regionFileOpCancel:
    return p.cancelFileOp()
```

### Hover State

Add hover state for visual feedback when mouse moves over buttons:

```go
// In plugin struct:
fileOpButtonHover int // 0=none, 1=confirm, 2=cancel

// Handle hover in mouse handler:
case mouse.ActionHover:
    return p.handleMouseHover(action)

func (p *Plugin) handleMouseHover(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    if p.fileOpMode == FileOpNone || action.Region == nil {
        p.fileOpButtonHover = 0
        return p, nil
    }
    switch action.Region.ID {
    case regionFileOpConfirm:
        p.fileOpButtonHover = 1
    case regionFileOpCancel:
        p.fileOpButtonHover = 2
    default:
        p.fileOpButtonHover = 0
    }
    return p, nil
}

// In modal render, focus takes precedence over hover:
confirmStyle := styles.Button
if p.fileOpButtonFocus == 1 {
    confirmStyle = styles.ButtonFocused
} else if p.fileOpButtonHover == 1 {
    confirmStyle = styles.ButtonHover
}
```

## Path Auto-Complete (for path inputs)

For modals accepting directory paths (like move), show fuzzy-matched suggestions.

### State Fields

```go
// In plugin struct:
dirCache              []string // Cached directory paths
fileOpSuggestions     []string // Current filtered suggestions
fileOpSuggestionIdx   int      // Selected suggestion (-1 = none)
fileOpShowSuggestions bool     // Show suggestions dropdown
```

### Directory Cache

Build a cache of directories (not files) for path suggestions:

```go
func (p *Plugin) buildDirCache() {
    // Walk filesystem collecting only directories
    // Skip .git, node_modules, etc.
    // Respect gitignore
}

func (p *Plugin) getPathSuggestions(query string) []string {
    if len(p.dirCache) == 0 {
        p.buildDirCache()
    }
    matches := FuzzyFilter(p.dirCache, query, maxResults)
    // Return matched paths
}
```

### Suggestion Rendering

```go
// After input field in modal render:
if p.fileOpShowSuggestions && len(p.fileOpSuggestions) > 0 {
    sb.WriteString("\n")
    for i, suggestion := range p.fileOpSuggestions {
        if i == p.fileOpSuggestionIdx {
            sb.WriteString(styles.ListItemSelected.Render("  → " + suggestion))
        } else {
            sb.WriteString(styles.Muted.Render("    " + suggestion))
        }
        if i < len(p.fileOpSuggestions)-1 {
            sb.WriteString("\n")
        }
    }
}
```

### Suggestion Navigation

- **Up/Down** or **Ctrl+P/N**: Navigate suggestions (-1 = no selection)
- **Tab**: Accept top/selected suggestion
- **Enter** (with selection): Accept suggestion and stay in input
- Update suggestions on each keystroke

## Style Constants

```go
// Dimming style used by ui.OverlayModal() (strips ANSI and applies gray)
var DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
```

**Why not preserve colors?** ANSI SGR 2 (faint) doesn't reliably combine with existing color codes in most terminals. Stripping colors and applying a consistent gray provides reliable dimming across all terminal emulators.

## Common Pitfalls

1. **`WithWhitespaceForeground()` doesn't create visible dimming** - It sets the foreground color of space characters, which are invisible. Use `ui.OverlayModal()` if you want visible dimmed background.

2. **Don't use `lipgloss.Place()` with `ui.OverlayModal()`** - they both handle centering, which causes layout issues.

3. **Pass the full background** - `ui.OverlayModal()` needs the complete background content to composite correctly. Don't pre-truncate or pre-dim.

4. **Height constraints** - Ensure modal content respects available height to prevent overflow.

5. **`RenderGradientBorder` only handles horizontal padding** - Unlike lipgloss's `Padding(1, 2)` which adds 1 line top/bottom padding, `RenderGradientBorder(..., padding)` only adds horizontal (left/right) padding. If you need vertical padding for consistent mouse hit regions, add blank lines to your content manually:
   ```go
   // Add vertical padding manually
   paddedContent := "\n" + content + "\n"
   return styles.RenderGradientBorder(paddedContent, width, height, gradient, 1)
   ```
   Without this, mouse Y calculations will be off by 1 row compared to lipgloss-styled modals.

6. **Border + padding offset is +2, not +1** - When using `modalStyle` with `Border(lipgloss.RoundedBorder())` and `Padding(1, 2)`, content starts at `modalY + 2`:
   - Border adds 1 row at top
   - `Padding(1, 2)` adds 1 row vertical padding at top (the `1` is vertical, `2` is horizontal)
   - Total: modalY + 2

7. **Text wrapping adds hidden lines** - Long content (like file paths) may wrap to multiple lines in the rendered output, but hardcoded line counts won't reflect this. Calculate dynamically:
   ```go
   // Use visual width for accurate line count
   pathLine := fmt.Sprintf("Path:   %s", path)
   pathWidth := ansi.StringWidth(pathLine)
   contentWidth := modalW - 6 // border(2) + padding(4)
   pathLineCount := (pathWidth + contentWidth - 1) / contentWidth
   ```

8. **Bordered inputs take 2 lines, not 1** - Text inputs with `Border(lipgloss.NormalBorder())` render as 2 lines (top border + content, bottom border). When calculating button Y positions, count bordered inputs as 2 lines each:
   ```go
   // Line 0: "Name:"
   // Lines 1-2: bordered input (2 lines)  ← not just line 1!
   // Line 3: blank
   // Line 4: buttons
   buttonRow := 4  // Not 3!
   ```

9. **Background region clicks leak through modals** - When `renderListView()` is called to generate background text for `ui.OverlayModal()`, it registers full-screen hit regions (sidebar, preview, divider, etc.) as a side effect. Modal-specific regions only cover interactive elements, so clicks on empty modal space match background regions. The fix: `mouse.go` has guards at the top of `handleMouseClick`, `handleMouseHover`, `handleMouseScroll`, `handleMouseDrag`, `handleMouseDragEnd`, and `handleMouseDoubleClick` that use `isModalViewMode()` + `isBackgroundRegion()` to absorb these events. New modals automatically benefit from these guards—no backdrop/body regions needed.

## Hit Region Calculation for Modal Buttons

Calculating mouse hit regions for modal buttons is error-prone. Common issues:

### Off-by-One (or More) Errors

Hit regions are calculated separately from rendering, so they can drift. Sources of errors:

1. **Newlines after components**: `sb.WriteString("\n")` after a multi-line component (like textarea) may add an extra blank line if the component's `View()` already includes a trailing newline.

2. **Border + padding offset**: With `Border(lipgloss.RoundedBorder())` and `Padding(1, 2)`:
   ```go
   // Content starts at modalY + 2:
   // - Border: 1 row at top
   // - Padding(1, 2): 1 row vertical padding (first arg is vertical)
   currentY := modalStartY + 2
   ```

3. **Text wrapping**: Long strings (paths, descriptions) may wrap to multiple lines:
   ```go
   // Calculate actual line count for wrappable content
   contentWidth := modalW - 6 // border(2) + padding(4)
   pathWidth := ansi.StringWidth(pathLine)
   pathLineCount := (pathWidth + contentWidth - 1) / contentWidth
   currentY += pathLineCount
   ```

4. **Content line counting**: Count actual rendered lines, not logical sections:
   ```go
   // Wrong: "header section" = 1
   // Right: title line + separator line = 2
   ```

5. **Cumulative errors**: Small errors compound. A 1-line border error + 1-line path wrap = 2-line total error, which can cause buttons to appear 5+ rows from their hit regions if other content wraps too.

### Debugging Strategy

When hit region is off:
1. **+1 row above visual** → Y is too small, add to calculation
2. **+1 row below visual** → Y is too large, subtract from calculation

Test with increments of 1 until aligned. Document the empirical offset with a comment explaining why.

### Example Calculation

```go
func (p *Plugin) registerButtonHitRegion() {
    modalHeight := lipgloss.Height(modal)
    startX := (p.width - modalWidth) / 2
    startY := (p.height - modalHeight) / 2

    // Content starts after border(1) + padding(1) = 2
    currentY := startY + 2

    // Track Y position through content structure
    currentY += 2 // title + blank line

    // Handle potentially wrapping content dynamically
    contentWidth := modalWidth - 6 // border(2) + padding(4)
    pathWidth := ansi.StringWidth(pathLine)
    pathLineCount := (pathWidth + contentWidth - 1) / contentWidth
    currentY += pathLineCount

    currentY += 4 // remaining fixed content lines

    // Buttons are here
    buttonY := currentY
    p.mouseHandler.HitMap.AddRect(regionButton, buttonX, buttonY, width, 1, nil)
}
```

### Keep Height Estimate in Sync

The modal height estimate and hit region calculation must use the same line counting logic. If you change one, update the other:

```go
// estimateModalHeight() and registerButtonHitRegion() must agree on:
// - Number of header lines
// - Number of content lines per item
// - Padding/spacing between sections
```

## File Locations

- App-level modals: `internal/app/view.go`
- Plugin modal helper: `internal/ui/overlay.go` (`OverlayModal()`)
- Modal styles: `internal/styles/styles.go` (`ModalBox`, `ModalTitle`, etc.)

## Interactive Modal Buttons

All modals with user actions should use interactive buttons instead of key hints:

### Button Rendering Pattern
```go
confirmStyle := styles.Button
cancelStyle := styles.Button
if p.buttonFocus == 1 {
    confirmStyle = styles.ButtonFocused
}
if p.buttonFocus == 2 {
    cancelStyle = styles.ButtonFocused
}

sb.WriteString(confirmStyle.Render(" Confirm "))
sb.WriteString("  ")
sb.WriteString(cancelStyle.Render(" Cancel "))
```

### Keyboard Navigation
- Tab: Cycle focus between input field and buttons
- Enter: Execute focused button (or confirm from input)
- Esc: Always cancels (global shortcut)

### Mouse Support
Register hit regions for buttons during render:
```go
p.mouseHandler.HitMap.AddRect(regionConfirm, x, y, width, 1, nil)
```

Handle clicks in Update():
```go
case regionConfirm:
    return p.executeAction()
```

### Path Auto-Complete (for path inputs)
For modals accepting directory paths (like move), show fuzzy-matched suggestions:
- Build directory cache (filter for IsDir during walk)
- Use FuzzyFilter() from fuzzy.go
- Show up to 5 directory suggestions below input
- Tab accepts top/selected suggestion
- Up/Down navigates suggestions
```

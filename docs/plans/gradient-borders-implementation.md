# Gradient Borders Implementation Plan

## Overview

Add support for gradient borders on container elements (sidebars, content panels) with approximately 30-degree angled gradients. This enhancement will make the UI more visually distinctive while maintaining the existing theme system's flexibility.

## Current State

### Theme System
- **Location**: `internal/styles/themes.go` and `internal/styles/styles.go`
- **Border colors**: `borderNormal`, `borderActive`, `borderMuted` (single hex colors)
- **Panel styles**: `PanelActive`, `PanelInactive` use `lipgloss.RoundedBorder()` with single `BorderForeground(color)`

### Existing Gradient Infrastructure
The codebase already has color interpolation utilities:
- `interpolateRainbow()` in `styles.go` - per-character rainbow gradients
- `lerpRGB()` in `tdmonitor/notinstalled.go` - linear RGB interpolation
- `hexToRGB()` / `toLipgloss()` - color conversion utilities
- `threewayGradient()` with smoothstep easing

### Limitation
Lipgloss `BorderForeground()` only accepts a single color. Gradient borders require custom rendering with per-character ANSI color codes.

---

## Implementation Plan

### Phase 1: Gradient Color Utilities

**File**: `internal/styles/gradient.go` (new)

#### 1.1 Core Types

```go
// GradientStop defines a color at a position (0.0 to 1.0)
type GradientStop struct {
    Position float64
    Color    string // hex color
}

// Gradient defines a multi-stop color gradient
type Gradient struct {
    Stops []GradientStop
    Angle float64 // degrees (0 = horizontal, 90 = vertical)
}

// RGB for interpolation (consolidate from scattered implementations)
type RGB struct {
    R, G, B float64
}
```

#### 1.2 Interpolation Functions

```go
// HexToRGB converts "#RRGGBB" to RGB
func HexToRGB(hex string) RGB

// RGBToHex converts RGB back to hex
func RGBToHex(c RGB) string

// LerpRGB linearly interpolates between two colors
func LerpRGB(c1, c2 RGB, t float64) RGB

// GradientColorAt returns interpolated color at position t (0.0-1.0)
func (g *Gradient) ColorAt(t float64) RGB

// PositionAt calculates gradient position for a coordinate given angle
// For 30-degree angle: position = (x + y*tan(30°)) / (width + height*tan(30°))
func (g *Gradient) PositionAt(x, y, width, height int) float64
```

#### 1.3 Angled Gradient Math

For a 30-degree gradient:
```
tan(30°) ≈ 0.577

position = (x + y * 0.577) / (width + height * 0.577)
```

This creates diagonal color bands tilted at 30 degrees from horizontal.

---

### Phase 2: Theme Gradient Definitions

**File**: `internal/styles/themes.go`

#### 2.1 Extend ColorPalette

```go
type ColorPalette struct {
    // ... existing fields ...

    // Gradient border colors (new)
    GradientBorderActive  []string // e.g., ["#7C3AED", "#3B82F6"]
    GradientBorderNormal  []string // e.g., ["#374151", "#1F2937"]
    GradientBorderAngle   float64  // default: 30.0
}
```

#### 2.2 Update Default Theme

```go
var DefaultTheme = Theme{
    Name: "default",
    Colors: ColorPalette{
        // ... existing colors ...

        // Gradient: purple → blue (matches primary/secondary)
        GradientBorderActive: []string{"#7C3AED", "#3B82F6"},
        // Gradient: subtle gray variation
        GradientBorderNormal: []string{"#374151", "#2D3748"},
        GradientBorderAngle:  30.0,
    },
}
```

#### 2.3 Update Dracula Theme

```go
var DraculaTheme = Theme{
    Name: "dracula",
    Colors: ColorPalette{
        // ... existing colors ...

        // Gradient: purple → cyan (Dracula signature colors)
        GradientBorderActive: []string{"#BD93F9", "#8BE9FD"},
        // Gradient: subtle Dracula grays
        GradientBorderNormal: []string{"#44475A", "#383A4A"},
        GradientBorderAngle:  30.0,
    },
}
```

#### 2.4 Config Override Support

Update `ApplyColorOverrides()` to support gradient arrays:

```yaml
# config.yaml example
ui:
  theme:
    name: "default"
    overrides:
      gradientBorderActive: ["#FF5500", "#FF00FF", "#00FFFF"]
      gradientBorderNormal: ["#333333", "#222222"]
      gradientBorderAngle: 45
```

---

### Phase 3: Gradient Border Renderer

**File**: `internal/styles/borders.go` (new)

#### 3.1 Border Character Constants

```go
// Rounded border characters (matching lipgloss.RoundedBorder)
const (
    cornerTL = "╭"
    cornerTR = "╮"
    cornerBL = "╰"
    cornerBR = "╯"
    horizontal = "─"
    vertical = "│"
)
```

#### 3.2 Gradient Border Rendering

```go
// RenderGradientBorder renders a box with gradient-colored borders
// Returns the complete bordered content as a string
func RenderGradientBorder(content string, width, height int, gradient Gradient, active bool) string

// RenderGradientBorderTop renders just the top border line
func RenderGradientBorderTop(width int, gradient Gradient, totalHeight int) string

// RenderGradientBorderBottom renders just the bottom border line
func RenderGradientBorderBottom(width int, gradient Gradient, yOffset, totalHeight int) string

// RenderGradientBorderSides wraps content lines with gradient side borders
func RenderGradientBorderSides(lines []string, width int, gradient Gradient, startY, totalHeight int) []string
```

#### 3.3 Per-Character Coloring

```go
// colorChar wraps a character with ANSI foreground color
func colorChar(char string, color RGB) string {
    return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m",
        int(color.R*255), int(color.G*255), int(color.B*255), char)
}
```

#### 3.4 Example Top Border Implementation

```go
func RenderGradientBorderTop(width int, g Gradient, totalHeight int) string {
    var sb strings.Builder

    // Top-left corner (position 0,0)
    pos := g.PositionAt(0, 0, width, totalHeight)
    sb.WriteString(colorChar(cornerTL, g.ColorAt(pos)))

    // Horizontal line
    for x := 1; x < width-1; x++ {
        pos := g.PositionAt(x, 0, width, totalHeight)
        sb.WriteString(colorChar(horizontal, g.ColorAt(pos)))
    }

    // Top-right corner
    pos = g.PositionAt(width-1, 0, width, totalHeight)
    sb.WriteString(colorChar(cornerTR, g.ColorAt(pos)))

    return sb.String()
}
```

---

### Phase 4: Style Integration

**File**: `internal/styles/styles.go`

#### 4.1 New Panel Rendering Functions

```go
// RenderPanel renders content in a panel with gradient borders
func RenderPanel(content string, width, height int, active bool) string {
    theme := GetCurrentTheme()

    var gradientColors []string
    if active {
        gradientColors = theme.Colors.GradientBorderActive
    } else {
        gradientColors = theme.Colors.GradientBorderNormal
    }

    // Fallback to solid color if no gradient defined
    if len(gradientColors) < 2 {
        style := PanelInactive
        if active {
            style = PanelActive
        }
        return style.Width(width).Height(height).Render(content)
    }

    gradient := BuildGradient(gradientColors, theme.Colors.GradientBorderAngle)
    return RenderGradientBorder(content, width, height, gradient, active)
}
```

#### 4.2 Backward Compatibility

Keep existing `PanelActive` and `PanelInactive` styles for:
- Simple use cases
- Themes without gradient definitions
- Performance-critical rendering (gradient is slightly more expensive)

---

### Phase 5: Plugin Updates

Update plugins to use gradient borders. Priority order:

#### 5.1 File Browser (`internal/plugins/filebrowser/view.go`)
- Tree view panel (left)
- Preview panel (right)

#### 5.2 Git Status (`internal/plugins/gitstatus/sidebar_view.go`)
- Files sidebar
- Diff panel

#### 5.3 Conversations (`internal/plugins/conversations/view.go`)
- Conversation list sidebar
- Main content panel

#### 5.4 Command Palette (`internal/palette/view.go`)
- Modal border

#### 5.5 Pattern

Replace:
```go
style := styles.PanelActive
if !p.focused {
    style = styles.PanelInactive
}
return style.Width(w).Height(h).Render(content)
```

With:
```go
return styles.RenderPanel(content, w, h, p.focused)
```

---

### Phase 6: Documentation Updates

**File**: `docs/guides/theme-creator-guide.md`

Add new section:

```markdown
### Gradient Border Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `gradientBorderActive` | Active panel gradient | `["#7C3AED", "#3B82F6"]` | `["#BD93F9", "#8BE9FD"]` |
| `gradientBorderNormal` | Normal panel gradient | `["#374151", "#2D3748"]` | `["#44475A", "#383A4A"]` |
| `gradientBorderAngle` | Gradient angle (degrees) | `30` | `30` |

Gradients support 2+ color stops. Colors flow from top-left to bottom-right at the specified angle.

Example multi-stop gradient:
```yaml
overrides:
  gradientBorderActive: ["#FF0000", "#00FF00", "#0000FF"]  # RGB rainbow
  gradientBorderAngle: 45
```
```

---

## File Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/styles/gradient.go` | Create | Gradient types, interpolation, angle math |
| `internal/styles/borders.go` | Create | Gradient border renderer |
| `internal/styles/themes.go` | Modify | Add gradient fields to ColorPalette |
| `internal/styles/styles.go` | Modify | Add RenderPanel(), integrate gradients |
| `internal/plugins/filebrowser/view.go` | Modify | Use gradient panels |
| `internal/plugins/gitstatus/sidebar_view.go` | Modify | Use gradient panels |
| `internal/plugins/conversations/view.go` | Modify | Use gradient panels |
| `internal/palette/view.go` | Modify | Use gradient borders |
| `docs/guides/theme-creator-guide.md` | Modify | Document gradient options |

---

## Testing Strategy

1. **Unit tests** for gradient math (angle calculations, interpolation)
2. **Visual verification** - manual testing of border appearance
3. **Theme switching** - verify gradients update on theme change
4. **Fallback behavior** - themes without gradient definitions use solid colors
5. **Performance** - ensure gradient rendering doesn't impact scroll/resize performance

---

## Rollout Considerations

1. **Feature flag** (optional): Add `ui.gradientBorders: true/false` config option for users who prefer solid borders
2. **Terminal compatibility**: Gradient requires 24-bit color support (most modern terminals)
3. **Graceful degradation**: Falls back to solid `borderActive`/`borderNormal` if gradient array is empty or single-color

---

## Visual Example

```
╭──────────────────────────────╮
│                              │  ← Gradient flows diagonally
│    Content Panel             │     from purple (top-left)
│                              │     to blue (bottom-right)
│                              │     at 30° angle
╰──────────────────────────────╯
```

With 30° angle, the gradient creates a subtle diagonal sweep that adds depth without being distracting.

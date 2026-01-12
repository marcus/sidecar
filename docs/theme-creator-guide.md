# Theme Creator Guide

This guide explains how to create custom themes for Sidecar.

## Quick Start

Themes are configured in your `~/.config/sidecar/config.yaml`:

```yaml
ui:
  theme:
    name: "default"  # Base theme: "default" or "dracula"
    overrides:       # Optional color overrides
      primary: "#FF5500"
      success: "#00FF00"
```

## Available Base Themes

- **default** - Dark theme with purple/blue accents
- **dracula** - Dracula-inspired dark theme with vibrant colors

## Color Palette Reference

All colors use hex format (`#RRGGBB`). Here's the complete palette:

### Brand Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `primary` | Primary brand color (active elements, highlights) | `#7C3AED` | `#BD93F9` |
| `secondary` | Secondary color (directories, info) | `#3B82F6` | `#8BE9FD` |
| `accent` | Accent color (code, special text) | `#F59E0B` | `#FFB86C` |

### Status Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `success` | Success/staged/added | `#10B981` | `#50FA7B` |
| `warning` | Warning/modified | `#F59E0B` | `#FFB86C` |
| `error` | Error/deleted/removed | `#EF4444` | `#FF5555` |
| `info` | Info/in-progress | `#3B82F6` | `#8BE9FD` |

### Text Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `textPrimary` | Primary text | `#F9FAFB` | `#F8F8F2` |
| `textSecondary` | Secondary text | `#9CA3AF` | `#BFBFBF` |
| `textMuted` | Muted text (hints, line numbers) | `#6B7280` | `#6272A4` |
| `textSubtle` | Very subtle text (ignored files) | `#4B5563` | `#44475A` |
| `textHighlight` | Highlighted text (subtitles) | `#E5E7EB` | `#F8F8F2` |

### Background Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `bgPrimary` | Primary background | `#111827` | `#282A36` |
| `bgSecondary` | Secondary background (header/footer) | `#1F2937` | `#343746` |
| `bgTertiary` | Tertiary background (selections) | `#374151` | `#44475A` |
| `bgOverlay` | Overlay background (modals) | `#00000080` | `#00000080` |

### Border Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `borderNormal` | Normal panel borders | `#374151` | `#44475A` |
| `borderActive` | Active panel borders | `#7C3AED` | `#BD93F9` |
| `borderMuted` | Muted borders | `#1F2937` | `#343746` |

### Diff Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `diffAddFg` | Added line foreground | `#10B981` | `#50FA7B` |
| `diffAddBg` | Added line background | `#0D2818` | `#1E3A29` |
| `diffRemoveFg` | Removed line foreground | `#EF4444` | `#FF5555` |
| `diffRemoveBg` | Removed line background | `#2D1A1A` | `#3D2A2A` |

### UI Element Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `buttonHover` | Button hover state | `#9D174D` | `#FF79C6` |
| `tabTextInactive` | Inactive tab text | `#1a1a1a` | `#282A36` |
| `link` | Hyperlink color | `#60A5FA` | `#8BE9FD` |
| `toastSuccessText` | Toast success foreground | `#000000` | `#282A36` |
| `toastErrorText` | Toast error foreground | `#FFFFFF` | `#F8F8F2` |

### Third-Party Theme Names
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `syntaxTheme` | Chroma syntax highlighting theme | `monokai` | `dracula` |
| `markdownTheme` | Glamour markdown theme (`dark`/`light`) | `dark` | `dark` |

## Creating a Custom Theme

### Method 1: Override Specific Colors

Start from a base theme and override specific colors:

```yaml
ui:
  theme:
    name: "default"
    overrides:
      primary: "#E91E63"      # Pink primary
      success: "#4CAF50"      # Material green
      error: "#F44336"        # Material red
      syntaxTheme: "github"   # Different syntax theme
```

### Method 2: Full Theme Override

Override all colors for complete control:

```yaml
ui:
  theme:
    name: "default"
    overrides:
      # Brand
      primary: "#6200EA"
      secondary: "#03DAC6"
      accent: "#FF9800"

      # Status
      success: "#4CAF50"
      warning: "#FF9800"
      error: "#F44336"
      info: "#2196F3"

      # Text
      textPrimary: "#FFFFFF"
      textSecondary: "#B0BEC5"
      textMuted: "#78909C"
      textSubtle: "#546E7A"
      textHighlight: "#ECEFF1"

      # Backgrounds
      bgPrimary: "#121212"
      bgSecondary: "#1E1E1E"
      bgTertiary: "#2D2D2D"
      bgOverlay: "#00000080"

      # Borders
      borderNormal: "#424242"
      borderActive: "#6200EA"
      borderMuted: "#1E1E1E"

      # Diff
      diffAddFg: "#4CAF50"
      diffAddBg: "#1B3D1B"
      diffRemoveFg: "#F44336"
      diffRemoveBg: "#3D1B1B"

      # UI elements
      buttonHover: "#7C4DFF"
      tabTextInactive: "#121212"
      link: "#82B1FF"
      toastSuccessText: "#000000"
      toastErrorText: "#FFFFFF"

      # Third-party
      syntaxTheme: "monokai"
      markdownTheme: "dark"
```

## Available Syntax Themes

The `syntaxTheme` value can be any Chroma theme. Popular options:

- `monokai` - Classic dark theme
- `dracula` - Dracula colors
- `github` - GitHub style
- `github-dark` - GitHub dark mode
- `nord` - Nord color scheme
- `onedark` - Atom One Dark
- `solarized-dark` - Solarized dark
- `solarized-light` - Solarized light (for light themes)
- `vs` - Visual Studio light
- `vim` - Vim colors

See [Chroma Style Gallery](https://xyproto.github.io/splash/docs/) for all options.

## Design Tips

1. **Contrast**: Ensure text colors have sufficient contrast against backgrounds
2. **Consistency**: Use related colors from the same palette (Tailwind, Material, etc.)
3. **Diff visibility**: Diff backgrounds should be subtle but visible
4. **Toast readability**: Toast text colors should contrast with success/error backgrounds

## Color Validation

Colors must be valid hex codes in `#RRGGBB` format. Invalid colors will be ignored.

```go
// Valid
"#FF5500"
"#ff5500"  // lowercase ok

// Invalid
"FF5500"   // missing #
"#F50"     // shorthand not supported
"red"      // named colors not supported
```

## Programmatic Theme Registration

For plugins or extensions, themes can be registered programmatically:

```go
import "github.com/marcus/sidecar/internal/styles"

myTheme := styles.Theme{
    Name:        "my-theme",
    DisplayName: "My Custom Theme",
    Colors: styles.ColorPalette{
        Primary:   "#FF5500",
        Secondary: "#00FF55",
        // ... all other colors
    },
}

styles.RegisterTheme(myTheme)
styles.ApplyTheme("my-theme")
```

## API Reference

```go
// Get list of available theme names
themes := styles.ListThemes() // ["default", "dracula", ...]

// Check if theme exists
exists := styles.IsValidTheme("my-theme") // true/false

// Validate hex color
valid := styles.IsValidHexColor("#FF5500") // true/false

// Get current theme
theme := styles.GetCurrentTheme()
name := styles.GetCurrentThemeName()

// Apply theme
styles.ApplyTheme("dracula")
styles.ApplyThemeWithOverrides("default", map[string]string{
    "primary": "#FF5500",
})
```

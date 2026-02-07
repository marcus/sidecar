# Theme Creator Guide

This guide explains how to create custom themes for Sidecar and configure UI appearance options.

## UI Configuration

The `ui` section of your config controls visual appearance:

```json
{
  "ui": {
    "showClock": true,
    "nerdFontsEnabled": false,
    "theme": { ... }
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `showClock` | bool | `true` | Show clock in header bar |
| `nerdFontsEnabled` | bool | `false` | Enable Nerd Font glyphs for enhanced UI |

### Nerd Fonts

When `nerdFontsEnabled` is `true`, Sidecar uses [Nerd Font](https://www.nerdfonts.com/) glyphs for:

- **Pill-shaped tabs** - Rounded tab edges in the header bar using Powerline characters
- **Pill-shaped buttons** - Rounded buttons in the workspace sidebar (New, +)
- **Future enhancements** - File type icons, status indicators, etc.

**Requirements:** You must have a Nerd Font installed and configured in your terminal (e.g., JetBrains Mono Nerd Font, FiraCode Nerd Font, Hack Nerd Font).

**Example:**

```json
{
  "ui": {
    "nerdFontsEnabled": true,
    "theme": {
      "name": "dracula"
    }
  }
}
```

Without a Nerd Font, these characters will render as boxes or question marks. Leave `nerdFontsEnabled` as `false` (the default) if you're unsure.

## Themes

Themes are configured in your `~/.config/sidecar/config.json`:

```json
{
  "ui": {
    "theme": {
      "name": "default",
      "overrides": {
        "primary": "#FF5500",
        "success": "#00FF00"
      }
    }
  }
}
```

## Available Base Themes

- **default** - Dark theme with purple/blue accents
- **dracula** - Dracula-inspired dark theme with vibrant colors
- **molokai** - Vibrant, high-contrast dark theme
- **nord** - Arctic, north-bluish color palette
- **solarized-dark** - Precision color scheme for reduced blue light
- **tokyo-night** - Clean dark theme celebrating Downtown Tokyo

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
| `textSelection` | Text on selected backgrounds | `#FFFFFF` | `#F8F8F2` |
| `textInverse` | Inverse text color | `#111827` | `#282A36` |

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
| `borderNormal` | Normal panel borders (fallback) | `#374151` | `#44475A` |
| `borderActive` | Active panel borders (fallback) | `#7C3AED` | `#BD93F9` |
| `borderMuted` | Muted borders | `#1F2937` | `#343746` |

### Gradient Border Colors
Panel borders support angled gradients (default 30°) that flow diagonally from top-left to bottom-right.

| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `gradientBorderActive` | Active panel gradient colors | `["#7C3AED", "#3B82F6"]` | `["#BD93F9", "#8BE9FD"]` |
| `gradientBorderNormal` | Normal panel gradient colors | `["#374151", "#2D3748"]` | `["#44475A", "#383A4A"]` |
| `gradientBorderAngle` | Gradient angle in degrees | `30` | `30` |

Gradients support 2 or more color stops. If gradient colors are not specified, the solid `borderActive`/`borderNormal` colors are used as fallback.

### Tab Theme Colors

Tabs in the header bar support multiple color schemes. Configure with `tabStyle` and `tabColors`:

| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `tabStyle` | Tab rendering style or preset name | `rainbow` | `gradient` |
| `tabColors` | Array of hex colors for gradient/per-tab | `["#DC3C3C", "#3CDC3C", "#3C3CDC", "#9C3CDC"]` | `["#BD93F9", "#FF79C6", "#8BE9FD"]` |

**Tab Styles:**
- `gradient` - Colors flow continuously across all tabs (per-character interpolation)
- `per-tab` - Each tab gets a distinct solid color from the array (cycles)
- `solid` - Uses theme primary/tertiary colors
- `minimal` - No background, active tab uses underline

**Built-in Presets** (use as `tabStyle` value):

| Preset | Style | Colors |
|--------|-------|--------|
| `rainbow` | gradient | Red → Green → Blue → Purple |
| `sunset` | gradient | Orange → Peach → Pink |
| `ocean` | gradient | Deep Blue → Cyan → Light Blue |
| `aurora` | gradient | Purple → Dark Purple → Teal |
| `neon` | gradient | Magenta → Cyan → Green |
| `fire` | gradient | Red-Orange → Orange → Gold |
| `forest` | gradient | Dark Green → Mid Green → Light Green |
| `candy` | gradient | Pink → Purple → Turquoise |
| `pastel` | per-tab | Pink, Green, Blue, Yellow |
| `jewel` | per-tab | Ruby, Sapphire, Amethyst, Topaz |
| `terminal` | per-tab | Red, Green, Cyan, Yellow |
| `mono` | solid | Theme primary color |
| `accent` | solid | Theme accent color |
| `underline` | minimal | No background, underlined active |
| `dim` | minimal | No background, dim inactive |

**Examples:**

```json
// Use a preset
{
  "ui": {
    "theme": {
      "overrides": {
        "tabStyle": "sunset"
      }
    }
  }
}

// Custom gradient
{
  "ui": {
    "theme": {
      "overrides": {
        "tabStyle": "gradient",
        "tabColors": ["#FF6B35", "#F7C59F", "#FF006E"]
      }
    }
  }
}

// Per-tab distinct colors
{
  "ui": {
    "theme": {
      "overrides": {
        "tabStyle": "per-tab",
        "tabColors": ["#FF5555", "#50FA7B", "#8BE9FD", "#F1FA8C"]
      }
    }
  }
}
```

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

### Danger Button Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `dangerLight` | Danger light variant | `#FCA5A5` | `#FF6E6E` |
| `dangerDark` | Danger dark variant | `#7F1D1D` | `#8B0000` |
| `dangerBright` | Danger bright variant | `#FF0000` | `#FF5555` |
| `dangerHover` | Danger hover state | `#DC2626` | `#FF4444` |

### Blame Age Colors
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `blameAge1` | Most recent commits | `#10B981` | `#50FA7B` |
| `blameAge2` | Recent commits | `#3B82F6` | `#8BE9FD` |
| `blameAge3` | Moderate age commits | `#8B5CF6` | `#BD93F9` |
| `blameAge4` | Older commits | `#F59E0B` | `#FFB86C` |
| `blameAge5` | Oldest commits | `#6B7280` | `#6272A4` |

### Third-Party Theme Names
| Key | Description | Default | Dracula |
|-----|-------------|---------|---------|
| `syntaxTheme` | Chroma syntax highlighting theme | `monokai` | `dracula` |
| `markdownTheme` | Glamour markdown theme (`dark`/`light`) | `dark` | `dark` |

## Creating a Custom Theme

### Method 1: Override Specific Colors

Start from a base theme and override specific colors:

```json
{
  "ui": {
    "theme": {
      "name": "default",
      "overrides": {
        "primary": "#E91E63",
        "success": "#4CAF50",
        "error": "#F44336",
        "syntaxTheme": "github"
      }
    }
  }
}
```

### Method 2: Full Theme Override

Override all colors for complete control:

```json
{
  "ui": {
    "theme": {
      "name": "default",
      "overrides": {
        "primary": "#6200EA",
        "secondary": "#03DAC6",
        "accent": "#FF9800",

        "success": "#4CAF50",
        "warning": "#FF9800",
        "error": "#F44336",
        "info": "#2196F3",

        "textPrimary": "#FFFFFF",
        "textSecondary": "#B0BEC5",
        "textMuted": "#78909C",
        "textSubtle": "#546E7A",
        "textHighlight": "#ECEFF1",
        "textSelection": "#FFFFFF",
        "textInverse": "#121212",

        "bgPrimary": "#121212",
        "bgSecondary": "#1E1E1E",
        "bgTertiary": "#2D2D2D",
        "bgOverlay": "#00000080",

        "borderNormal": "#424242",
        "borderActive": "#6200EA",
        "borderMuted": "#1E1E1E",

        "gradientBorderActive": ["#6200EA", "#03DAC6"],
        "gradientBorderNormal": ["#424242", "#303030"],
        "gradientBorderAngle": 30,

        "diffAddFg": "#4CAF50",
        "diffAddBg": "#1B3D1B",
        "diffRemoveFg": "#F44336",
        "diffRemoveBg": "#3D1B1B",

        "buttonHover": "#7C4DFF",
        "tabTextInactive": "#121212",
        "link": "#82B1FF",
        "toastSuccessText": "#000000",
        "toastErrorText": "#FFFFFF",

        "dangerLight": "#FCA5A5",
        "dangerDark": "#7F1D1D",
        "dangerBright": "#FF0000",
        "dangerHover": "#DC2626",

        "blameAge1": "#4CAF50",
        "blameAge2": "#2196F3",
        "blameAge3": "#9C27B0",
        "blameAge4": "#FF9800",
        "blameAge5": "#78909C",

        "syntaxTheme": "monokai",
        "markdownTheme": "dark"
      }
    }
  }
}
```

### Method 3: Custom Gradient Borders

Create eye-catching multi-color gradients:

```json
{
  "ui": {
    "theme": {
      "name": "default",
      "overrides": {
        "gradientBorderActive": [
          "#FF0000",
          "#FF7F00",
          "#FFFF00",
          "#00FF00",
          "#0000FF",
          "#8B00FF"
        ],
        "gradientBorderAngle": 45
      }
    }
  }
}
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
themes := styles.ListThemes() // ["default", "dracula", "molokai", "nord", ...]

// Get a theme by name
theme := styles.GetTheme("dracula") // Returns Theme struct

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

// Resolve effective theme for a project path (priority: project > global > default)
import "github.com/marcus/sidecar/internal/theme"

resolved := theme.ResolveTheme(cfg, "/path/to/project")
theme.ApplyResolved(resolved)
```

## Community Themes

Press `#` to open the theme switcher, then `Tab` to browse 453 community color schemes. The browser supports:

- **Search**: Type to filter by name
- **Live preview**: Themes apply instantly as you navigate
- **Color swatches**: Each entry shows a 4-color preview
- **Save**: Press `Enter` to save the selected theme to your config

Community themes are automatically converted from iTerm2 color schemes. The converter maps ANSI colors to Sidecar's semantic palette and derives tab gradients from the most saturated colors in each scheme.

Community themes are stored by scheme name in `~/.config/sidecar/config.json`. The full color palette is computed at runtime from the embedded scheme data:

```json
{
  "ui": {
    "theme": {
      "name": "default",
      "community": "Catppuccin Mocha"
    }
  }
}
```

You can layer custom overrides on top of a community theme:

```json
{
  "ui": {
    "theme": {
      "name": "default",
      "community": "Catppuccin Mocha",
      "overrides": { "primary": "#ff79c6" }
    }
  }
}
```

To revert, select any built-in theme from the `#` switcher.

## Updating Community Themes

The community theme browser (accessible via `#` → `Tab`) uses color schemes from the [iTerm2-Color-Schemes](https://github.com/mbadolato/iTerm2-Color-Schemes) repository (MIT licensed). These are embedded as `internal/community/schemes.json`.

To regenerate after pulling new schemes from upstream:

```bash
# Clone or update the source repo
git clone https://github.com/mbadolato/iTerm2-Color-Schemes ~/code/iTerm2-Color-Schemes

# Regenerate schemes.json (accepts optional path argument)
./scripts/generate-schemes.sh [path-to-repo]
```

## Per-Project Themes

Each project in your config can have its own theme. When you switch projects with `@`, the theme changes automatically.

### Config format

```json
{
  "projects": {
    "list": [
      { "name": "api", "path": "~/code/api", "theme": { "name": "dracula" } },
      { "name": "web", "path": "~/code/web", "theme": { "name": "default", "community": "Catppuccin Mocha" } },
      { "name": "tools", "path": "~/code/tools" }
    ]
  }
}
```

Projects without a `theme` field use the global theme from `ui.theme`.

### Setting a per-project theme

1. Press `#` to open the theme switcher
2. Press `ctrl+s` to toggle scope to "Set for this project"
3. Select a theme — it saves to the project's config entry only

The scope selector appears below the buttons when you're in a configured project. "Set globally" is the default and does not override projects that have their own theme.

### Adding a project with a theme

When adding a project via `ctrl+a` in the `@` switcher, the form includes a Theme field. Press `Enter` on it to open a mini theme picker where you can select a built-in or community theme. Projects default to "(use global)".

### Theme priority

Resolution order: project theme > global `ui.theme` > `"default"`.

The `@` project switcher live-previews each project's theme as you navigate.

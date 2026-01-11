# Theming System Implementation Plan

**Status:** Planning
**Epic:** Implement comprehensive theming support for sidecar
**Created:** 2026-01-11

## Executive Summary

This document outlines the plan to implement a comprehensive theming system for sidecar. Currently, colors and styles are defined in a semi-centralized manner with the main style definitions in `internal/styles/styles.go`, but the configuration system's theme support is not connected to actually override these values. Additionally, several plugins and components have hardcoded colors that bypass the central style system.

The goal is to:
1. Make all visual styles themeable through configuration
2. Consolidate all hardcoded colors into the central theme system
3. Ship two built-in themes: "default" (current dark theme) and "light"
4. Allow users to customize themes via config overrides

---

## Current State Analysis

### Centralized Styles (`internal/styles/styles.go`)

The main style hub defines ~30 color variables and ~50 style definitions:

**Color Categories:**
- Primary colors: `Primary`, `Secondary`, `Accent` (purple, blue, amber)
- Status colors: `Success`, `Warning`, `Error`, `Info`
- Text colors: `TextPrimary`, `TextSecondary`, `TextMuted`, `TextSubtle`
- Background colors: `BgPrimary`, `BgSecondary`, `BgTertiary`, `BgOverlay`
- Border colors: `BorderNormal`, `BorderActive`, `BorderMuted`

**Style Categories:**
- Panel styles: `PanelActive`, `PanelInactive`, `PanelHeader`, `PanelNoBorder`
- Text styles: `Title`, `Subtitle`, `Body`, `Muted`, `Subtle`, `Code`, `KeyHint`, `Logo`
- Status indicators: `StatusStaged`, `StatusModified`, `StatusUntracked`, `StatusDeleted`, etc.
- List items: `ListItemNormal`, `ListItemSelected`, `ListItemFocused`, `ListCursor`
- Diff styles: `DiffAdd`, `DiffRemove`, `DiffContext`, `DiffHeader`, `DiffAddBg`, `DiffRemoveBg`
- Modal/Button styles: `ModalBox`, `ModalTitle`, `Button`, `ButtonFocused`, `ButtonHover`
- Search styles: `SearchMatch`, `SearchMatchCurrent`, `FuzzyMatchChar`

### Config System (`internal/config/config.go`)

Theme configuration exists but is disconnected:

```go
type ThemeConfig struct {
    Name      string            `json:"name"`
    Overrides map[string]string `json:"overrides"`
}
```

The `LoadTheme()` function in styles.go returns a theme struct but doesn't apply it to the package-level style variables.

### Non-Uniform Style Definitions

The following locations define colors/styles outside the central system:

| Location | Issue | Colors Defined |
|----------|-------|----------------|
| `internal/plugins/tdmonitor/notinstalled.go:36-40` | Duplicates primary colors | `#7C3AED`, `#3B82F6`, `#F59E0B` |
| `internal/plugins/tdmonitor/notinstalled.go:212-235` | Inline hardcoded styles | Title, muted, text, link, code colors |
| `internal/plugins/gitstatus/diff_renderer.go:19-46` | Hardcoded diff backgrounds | `#0D3320`, `#3D1A1A` |
| `internal/plugins/gitstatus/syntax_highlight.go:35` | Hardcoded chroma theme | `"monokai"` |
| `internal/plugins/filebrowser/view.go:15-16` | Raw ANSI selection color | `#374151` via ANSI escape |
| `internal/plugins/filebrowser/preview.go:97-99` | Hardcoded chroma theme | `"monokai"` |
| `internal/markdown/renderer.go:109` | Hardcoded glamour theme | `"dark"` |
| `internal/ui/overlay.go:11-21` | Hardcoded dim color | `lipgloss.Color("242")` |
| `internal/app/intro.go:54-276` | Animation gradient colors | Multiple RGB values for animation |
| `internal/palette/view.go:12-59` | Local style definitions | Uses styles.* but creates new styles |
| `internal/styles/styles.go:192` | Hardcoded inactive tab text | `#1a1a1a` |
| `internal/styles/styles.go:322` | Hardcoded button hover | `#9D174D` |
| `internal/styles/styles.go:68` | Hardcoded subtitle color | `#E5E7EB` |

### Third-Party Theme Dependencies

| Library | Current Setting | Location |
|---------|-----------------|----------|
| Chroma (syntax highlighting) | `"monokai"` | gitstatus, filebrowser |
| Glamour (markdown) | `"dark"` | markdown/renderer.go |

---

## Proposed Architecture

### Theme Definition Structure

Expand `ColorPalette` to include all themeable colors:

```go
type ColorPalette struct {
    // Brand colors
    Primary   string `json:"primary"`
    Secondary string `json:"secondary"`
    Accent    string `json:"accent"`

    // Status colors
    Success string `json:"success"`
    Warning string `json:"warning"`
    Error   string `json:"error"`
    Info    string `json:"info"`

    // Text colors
    TextPrimary   string `json:"textPrimary"`
    TextSecondary string `json:"textSecondary"`
    TextMuted     string `json:"textMuted"`
    TextSubtle    string `json:"textSubtle"`

    // Background colors
    BgPrimary   string `json:"bgPrimary"`
    BgSecondary string `json:"bgSecondary"`
    BgTertiary  string `json:"bgTertiary"`
    BgOverlay   string `json:"bgOverlay"`

    // Border colors
    BorderNormal string `json:"borderNormal"`
    BorderActive string `json:"borderActive"`
    BorderMuted  string `json:"borderMuted"`

    // Diff colors
    DiffAddFg    string `json:"diffAddFg"`
    DiffAddBg    string `json:"diffAddBg"`
    DiffRemoveFg string `json:"diffRemoveFg"`
    DiffRemoveBg string `json:"diffRemoveBg"`

    // Third-party theme names
    SyntaxTheme   string `json:"syntaxTheme"`   // Chroma theme name
    MarkdownTheme string `json:"markdownTheme"` // Glamour theme name
}

type Theme struct {
    Name        string       `json:"name"`
    DisplayName string       `json:"displayName"`
    Colors      ColorPalette `json:"colors"`
}
```

### Theme Registry

Create a theme registry that:
1. Loads built-in themes (default, light)
2. Allows loading custom themes from config
3. Provides a `GetTheme(name string)` function
4. Applies theme to all style variables at startup

### Style Application Flow

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  config.json    │────▶│  Theme Registry  │────▶│  styles.Apply() │
│  theme.name     │     │  GetTheme()      │     │  Updates vars   │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                          │
                        ┌─────────────────────────────────┘
                        ▼
        ┌───────────────────────────────────────────────────────┐
        │                    Consumers                          │
        ├───────────────┬───────────────┬───────────────────────┤
        │ Plugins       │ UI Components │ Third-party libs      │
        │ (use styles.*) │ (use styles.*) │ (chroma, glamour)   │
        └───────────────┴───────────────┴───────────────────────┘
```

---

## Implementation Stories

### Story 1: Refactor styles package to support dynamic theming

**Description:** Convert the `styles` package from static variables to a theme-aware system that can be configured at runtime.

**Acceptance Criteria:**
- [ ] Create `Theme` and `ColorPalette` structs with JSON tags
- [ ] Create `themes.go` with built-in theme definitions
- [ ] Implement `ApplyTheme(theme Theme)` function that updates all style variables
- [ ] Implement `GetColor(name string) lipgloss.Color` for dynamic color access
- [ ] Ensure backwards compatibility - existing code using `styles.Primary` continues to work
- [ ] Add unit tests for theme application

**Files to modify:**
- `internal/styles/styles.go` - Refactor to use theme-based colors
- `internal/styles/themes.go` - New file with theme definitions
- `internal/styles/themes_test.go` - New file with tests

**Estimated complexity:** Medium

---

### Story 2: Connect config system to theme application

**Description:** Wire the configuration loader to apply the selected theme at startup.

**Acceptance Criteria:**
- [ ] Modify `config.ThemeConfig` to support full theme definition
- [ ] Load theme by name from built-in themes or config
- [ ] Apply theme overrides from config
- [ ] Call `styles.ApplyTheme()` during app initialization
- [ ] Log theme name being used at startup

**Files to modify:**
- `internal/config/config.go` - Expand ThemeConfig
- `internal/config/loader.go` - Load and apply theme
- `internal/app/model.go` - Apply theme on init
- `configs/default.json` - Update example config

**Estimated complexity:** Low-Medium

---

### Story 3: Consolidate hardcoded colors in TD Monitor plugin

**Description:** Replace hardcoded colors in the TD Monitor "not installed" view with central theme references.

**Acceptance Criteria:**
- [ ] Remove local `colorPurple`, `colorBlue`, `colorAmber` definitions
- [ ] Create helper function to get RGB from theme colors for animation
- [ ] Replace inline style definitions (lines 212-235) with theme-based styles
- [ ] Maintain animation smoothness with theme colors

**Files to modify:**
- `internal/plugins/tdmonitor/notinstalled.go`

**Estimated complexity:** Low

---

### Story 4: Consolidate hardcoded colors in Git Status plugin

**Description:** Replace hardcoded colors in diff rendering and syntax highlighting.

**Acceptance Criteria:**
- [ ] Move word-diff background colors (`#0D3320`, `#3D1A1A`) to theme palette
- [ ] Replace inline `NewStyle()` calls with theme-based styles
- [ ] Make chroma syntax theme configurable via theme
- [ ] Add `styles.WordDiffAddBg` and `styles.WordDiffRemoveBg`

**Files to modify:**
- `internal/plugins/gitstatus/diff_renderer.go`
- `internal/plugins/gitstatus/syntax_highlight.go`
- `internal/styles/styles.go` - Add word diff styles

**Estimated complexity:** Medium

---

### Story 5: Consolidate hardcoded colors in File Browser plugin

**Description:** Replace ANSI escape codes and hardcoded chroma theme.

**Acceptance Criteria:**
- [ ] Replace raw ANSI selection background with theme-based approach
- [ ] Create `styles.SelectionBackground` style
- [ ] Make chroma syntax theme configurable via theme palette
- [ ] Ensure selection highlighting works with both dark and light themes

**Files to modify:**
- `internal/plugins/filebrowser/view.go`
- `internal/plugins/filebrowser/preview.go`
- `internal/styles/styles.go` - Add selection style

**Estimated complexity:** Medium

---

### Story 6: Make markdown rendering theme-aware

**Description:** Configure glamour markdown renderer to use the theme's markdown style.

**Acceptance Criteria:**
- [ ] Pass theme's `MarkdownTheme` setting to glamour renderer
- [ ] Support at least "dark" and "light" glamour themes
- [ ] Re-initialize renderer when theme changes (if runtime switching added later)
- [ ] Cache rendered content per theme to avoid re-rendering

**Files to modify:**
- `internal/markdown/renderer.go`
- `internal/styles/styles.go` - Expose markdown theme getter

**Estimated complexity:** Low

---

### Story 7: Consolidate overlay and UI component colors

**Description:** Replace hardcoded colors in UI utilities.

**Acceptance Criteria:**
- [ ] Replace `lipgloss.Color("242")` dim color with theme value
- [ ] Add `styles.DimColor` and `styles.DimStyle` to theme
- [ ] Update confirm dialog to use only theme colors
- [ ] Audit buttons.go for hardcoded values

**Files to modify:**
- `internal/ui/overlay.go`
- `internal/ui/confirm_dialog.go`
- `internal/styles/styles.go` - Add dim styles

**Estimated complexity:** Low

---

### Story 8: Make intro animation theme-aware

**Description:** Update the intro animation to use theme colors for gradients.

**Acceptance Criteria:**
- [ ] Replace hardcoded start/end gradient colors with theme values
- [ ] Add theme palette entries for animation colors (optional, can derive)
- [ ] Maintain smooth RGB interpolation with theme colors
- [ ] Test animation appearance with both themes

**Files to modify:**
- `internal/app/intro.go`

**Estimated complexity:** Medium

---

### Story 9: Consolidate palette view styles

**Description:** Move palette-specific styles to central theme or make them theme-derived.

**Acceptance Criteria:**
- [ ] Review palette local styles for any not using `styles.*` references
- [ ] Either move unique styles to central location or derive from theme
- [ ] Ensure palette appearance is consistent with theme

**Files to modify:**
- `internal/palette/view.go`
- `internal/styles/styles.go` - Add palette styles if needed

**Estimated complexity:** Low

---

### Story 10: Fix remaining hardcoded colors in styles.go

**Description:** Address inline hex values that should be theme variables.

**Acceptance Criteria:**
- [ ] Replace `#1a1a1a` (TabTextInactive) with theme variable
- [ ] Replace `#9D174D` (ButtonHover) with theme variable
- [ ] Replace `#E5E7EB` (Subtitle) with theme variable
- [ ] Add these as named palette entries

**Files to modify:**
- `internal/styles/styles.go`

**Estimated complexity:** Low

---

### Story 11: Create "default" (dark) theme definition

**Description:** Formalize the current color scheme as the "default" dark theme.

**Acceptance Criteria:**
- [ ] Create complete `Theme` definition with all color values
- [ ] Use current hex values for full backwards compatibility
- [ ] Set `SyntaxTheme: "monokai"` and `MarkdownTheme: "dark"`
- [ ] Document all color choices with comments

**Theme Definition:**
```json
{
  "name": "default",
  "displayName": "Default Dark",
  "colors": {
    "primary": "#7C3AED",
    "secondary": "#3B82F6",
    "accent": "#F59E0B",
    "success": "#10B981",
    "warning": "#F59E0B",
    "error": "#EF4444",
    "info": "#3B82F6",
    "textPrimary": "#F9FAFB",
    "textSecondary": "#9CA3AF",
    "textMuted": "#6B7280",
    "textSubtle": "#4B5563",
    "bgPrimary": "#111827",
    "bgSecondary": "#1F2937",
    "bgTertiary": "#374151",
    "bgOverlay": "#00000080",
    "borderNormal": "#374151",
    "borderActive": "#7C3AED",
    "borderMuted": "#1F2937",
    "diffAddFg": "#10B981",
    "diffAddBg": "#0D2818",
    "diffRemoveFg": "#EF4444",
    "diffRemoveBg": "#2D1A1A",
    "syntaxTheme": "monokai",
    "markdownTheme": "dark"
  }
}
```

**Files to create/modify:**
- `internal/styles/themes.go`

**Estimated complexity:** Low

---

### Story 12: Create "dracula" theme definition

**Description:** Design and implement a Dracula-inspired dark theme with vibrant colors and slightly lighter backgrounds.

**Acceptance Criteria:**
- [ ] Create complete Dracula theme with characteristic purple/pink/cyan palette
- [ ] Use Dracula's signature background colors (#282A36, #44475A)
- [ ] Select appropriate chroma theme ("dracula")
- [ ] Select appropriate glamour theme ("dracula" or "dark")
- [ ] Test all UI surfaces with Dracula theme
- [ ] Ensure diff colors match Dracula palette

**Proposed Theme Definition:**
```json
{
  "name": "dracula",
  "displayName": "Dracula",
  "colors": {
    "primary": "#BD93F9",
    "secondary": "#8BE9FD",
    "accent": "#FFB86C",
    "success": "#50FA7B",
    "warning": "#FFB86C",
    "error": "#FF5555",
    "info": "#8BE9FD",
    "textPrimary": "#F8F8F2",
    "textSecondary": "#BFBFBF",
    "textMuted": "#6272A4",
    "textSubtle": "#44475A",
    "bgPrimary": "#282A36",
    "bgSecondary": "#343746",
    "bgTertiary": "#44475A",
    "bgOverlay": "#00000080",
    "borderNormal": "#44475A",
    "borderActive": "#BD93F9",
    "borderMuted": "#343746",
    "diffAddFg": "#50FA7B",
    "diffAddBg": "#1E3A29",
    "diffRemoveFg": "#FF5555",
    "diffRemoveBg": "#3D2A2A",
    "syntaxTheme": "dracula",
    "markdownTheme": "dark"
  }
}
```

**Dracula Color Reference:**
| Color | Hex | Usage |
|-------|-----|-------|
| Background | #282A36 | Main background |
| Current Line | #44475A | Selection, tertiary bg |
| Foreground | #F8F8F2 | Primary text |
| Comment | #6272A4 | Muted text |
| Cyan | #8BE9FD | Secondary, info |
| Green | #50FA7B | Success, diff add |
| Orange | #FFB86C | Accent, warning |
| Pink | #FF79C6 | Highlights |
| Purple | #BD93F9 | Primary |
| Red | #FF5555 | Error, diff remove |
| Yellow | #F1FA8C | Highlights |

**Files to create/modify:**
- `internal/styles/themes.go`

**Estimated complexity:** Medium (requires visual testing)

---

### Story 13: Add theme switching documentation and examples

**Description:** Document how to configure themes in the user guide.

**Acceptance Criteria:**
- [ ] Update GETTING_STARTED.md with theme configuration section
- [ ] Document all available themes and their appearance
- [ ] Show example config.json with theme settings
- [ ] Document override syntax for custom colors
- [ ] Add screenshots of both themes

**Files to create/modify:**
- `docs/GETTING_STARTED.md`
- `docs/guides/theming-guide.md` (new)
- `configs/default.json` - Add theme examples

**Estimated complexity:** Low

---

### Story 14: (Optional) Add runtime theme switching

**Description:** Allow users to switch themes without restarting sidecar.

**Acceptance Criteria:**
- [ ] Add `:theme <name>` command to palette
- [ ] Implement `styles.ApplyTheme()` to update all existing styles
- [ ] Broadcast theme change message to all plugins
- [ ] Plugins re-render with new theme
- [ ] Persist theme choice to config

**Files to modify:**
- `internal/styles/styles.go`
- `internal/palette/entries.go`
- `internal/app/model.go`
- Various plugin Update() functions

**Estimated complexity:** High (requires style reactivity)

---

## Implementation Order

### Phase 1: Foundation (Stories 1, 2, 11)
Establish the theme infrastructure and apply the default theme.

### Phase 2: Consolidation (Stories 3-10)
Migrate all hardcoded colors to the theme system. Can be done in parallel by different contributors.

### Phase 3: Light Theme (Story 12)
Create and test the light theme once all surfaces are themeable.

### Phase 4: Documentation (Story 13)
Document the theming system for users.

### Phase 5: Enhancement (Story 14)
Optional: Add runtime theme switching for improved UX.

---

## Testing Strategy

### Unit Tests
- Theme loading and application
- Color override merging
- RGB interpolation for animations

### Visual Testing
- Manual review of all UI surfaces with each theme
- Screenshot comparison before/after refactoring
- Diff view testing with various file types
- Modal and overlay appearance

### Accessibility Testing
- Contrast ratio verification (WCAG AA: 4.5:1 for text)
- Color blindness simulation testing (protanopia, deuteranopia)

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Visual regressions during migration | High | Take screenshots before/after each story |
| Third-party lib theme incompatibility | Medium | Test chroma/glamour themes early |
| Performance impact from dynamic styles | Low | Profile style application; cache where needed |
| Animation colors not interpolating well | Medium | Use HSL interpolation if RGB looks bad |

---

## Success Metrics

1. **Zero hardcoded colors** - All color definitions flow through theme system
2. **Full theme coverage** - Both themes work on all 50+ style definitions
3. **User satisfaction** - Users can customize appearance via config
4. **Maintainability** - Adding new themed components requires only style references

---

## Appendix: File Inventory

### Files to Modify
| File | Stories | Changes |
|------|---------|---------|
| `internal/styles/styles.go` | 1, 4, 5, 7, 9, 10 | Refactor to theme-based, add new styles |
| `internal/styles/themes.go` | 1, 11, 12 | New: theme definitions |
| `internal/config/config.go` | 2 | Expand ThemeConfig |
| `internal/config/loader.go` | 2 | Load and apply theme |
| `internal/app/model.go` | 2 | Initialize theme |
| `internal/app/intro.go` | 8 | Theme-aware animation |
| `internal/plugins/tdmonitor/notinstalled.go` | 3 | Remove hardcoded colors |
| `internal/plugins/gitstatus/diff_renderer.go` | 4 | Theme-based diff colors |
| `internal/plugins/gitstatus/syntax_highlight.go` | 4 | Configurable chroma theme |
| `internal/plugins/filebrowser/view.go` | 5 | Theme-based selection |
| `internal/plugins/filebrowser/preview.go` | 5 | Configurable chroma theme |
| `internal/markdown/renderer.go` | 6 | Theme-aware glamour |
| `internal/ui/overlay.go` | 7 | Theme-based dim color |
| `internal/ui/confirm_dialog.go` | 7 | Audit for hardcoded colors |
| `internal/palette/view.go` | 9 | Consolidate styles |
| `docs/GETTING_STARTED.md` | 13 | Theme documentation |
| `docs/guides/theming-guide.md` | 13 | New: theming guide |
| `configs/default.json` | 2, 13 | Theme examples |

### New Files
- `internal/styles/themes.go` - Theme definitions and registry
- `internal/styles/themes_test.go` - Theme tests
- `docs/guides/theming-guide.md` - User guide for theming

# Per-Project Themes Implementation Plan

## Overview

Three changes:

1. **Fix community theme storage** — store only the scheme name + user overrides (not the full converted palette)
2. **Per-project themes (Phase 1)** — each project gets an optional theme; switching projects switches themes
3. **Theme picker + live preview (Phase 2)** — select theme in add-project modal; preview project themes as you navigate the switcher

---

## Part 1: Fix Community Theme Storage

### Problem

Selecting a community theme writes ~35 color fields to `config.json`. This is fragile (if the conversion algorithm improves, existing users are stuck on the old conversion) and bloated.

### Solution

Store just `"community": "Catppuccin Mocha"` in the theme config. At runtime, convert the community scheme on-the-fly and layer any user overrides on top.

### Config change

```json
// Before (current):
{ "name": "default", "overrides": { "communityName": "Catppuccin Mocha", "primary": "#89b4fa", ... 30+ fields } }

// After:
{ "name": "default", "community": "Catppuccin Mocha", "overrides": { "primary": "#custom_if_any" } }
```

### Files

- `internal/config/config.go:91-94` — add `Community string` field to `ThemeConfig`
- `internal/config/loader.go` — migrate old `communityName` from overrides → `Community` field
- `internal/config/saver.go` — add `SaveCommunityTheme(name string, userOverrides map[string]interface{})` helper
- `internal/app/update.go` (community browser Enter handler) — use new save function
- `internal/app/model.go` (theme switcher init, `communityNameFromOverrides`) — read from `cfg.UI.Theme.Community` directly

---

## Part 2: Theme Resolution Module (New File)

Create `internal/styles/resolve.go` — a single entry point for determining and applying the effective theme.

```go
// ResolvedTheme represents a fully-determined theme configuration.
type ResolvedTheme struct {
    BaseName      string
    CommunityName string
    Overrides     map[string]interface{}
}

// ResolveTheme determines the effective theme for a project path.
// Priority: project.Theme > global UI.Theme > "default".
func ResolveTheme(cfg *config.Config, projectPath string) ResolvedTheme

// ApplyResolved applies a resolved theme to the styles system.
func ApplyResolved(r ResolvedTheme)
```

`ApplyResolved` logic:

1. If `CommunityName != ""` → `community.GetScheme()` → `Convert()` → merge user overrides on top → `ApplyThemeWithGenericOverrides(base, merged)`
2. Else if `Overrides` present → `ApplyThemeWithGenericOverrides(base, overrides)`
3. Else → `ApplyTheme(base)`

This replaces the direct `ApplyThemeWithGenericOverrides` call in `main.go:102`.

---

## Part 3: Per-Project Themes (Phase 1)

### Config change

```go
// internal/config/config.go
type ProjectConfig struct {
    Name  string       `json:"name"`
    Path  string       `json:"path"`
    Theme *ThemeConfig `json:"theme,omitempty"` // nil = use global
}
```

### switchProject integration

In `internal/app/model.go:482` (`switchProject`), after updating `m.ui.WorkDir`:

```go
resolved := styles.ResolveTheme(m.cfg, projectPath)
styles.ApplyResolved(resolved)
```

### Theme switcher (`#`) behavior — scope selector

Below the theme list (near the help/hint area), show a scope toggle:

- **"Set globally"** (default) — sets the global theme; does NOT override projects that have their own theme
- **"Set for this project"** — sets a per-project theme for the current project only

The scope selector is always visible. A muted hint next to "Set globally" reads: "projects with their own theme are unaffected".

Toggle between options with a key (e.g., `ctrl+s` for scope, or left/right arrows on the selector line).

When "Set for this project" is active and user confirms a theme:

- Save to `ProjectConfig.Theme` for the current project
- Show toast: "Theme: X (project: name)"

When "Set globally" is active:

- Save to `cfg.UI.Theme`
- Show toast: "Theme: X (global)"

New model fields:

- `themeSwitcherScope string` — "global" or "project" (default: "global")

New helpers in `model.go`:

- `currentProjectConfig() *config.ProjectConfig` — returns the project entry or nil
- `saveThemeForScope(tc ThemeConfig)` — saves to project or global based on `themeSwitcherScope`

### Saver changes

`internal/config/saver.go`:

- Add `SaveProjectTheme(projectPath string, theme ThemeConfig) error` — updates the project's theme in-place and saves config

### Startup

`cmd/sidecar/main.go:102`:

```go
// Replace:
styles.ApplyThemeWithGenericOverrides(cfg.UI.Theme.Name, cfg.UI.Theme.Overrides)
// With:
resolved := styles.ResolveTheme(cfg, workDir)
styles.ApplyResolved(resolved)
```

---

## Part 4: Theme Picker in Add-Project Modal (Phase 2)

### New model fields

```go
projectAddThemeMode    bool     // is theme picker sub-modal open?
projectAddThemeCursor  int      // cursor in theme list
projectAddThemeScroll  int
projectAddThemeInput   textinput.Model
projectAddThemeList    []string // "global" + built-in names
projectAddCommunity    string   // selected community name (empty = none)
projectAddThemeLabel   string   // display label for chosen theme
```

### UX flow

1. Add-project form shows: Name, Path, Theme (defaults to "Use global")
2. Pressing Enter/Space on the Theme field opens a mini theme picker (reuses community browser pattern)
3. User can pick a built-in theme or Tab into community themes
4. Selection returns to the add form with the theme label updated
5. On save, the chosen theme is stored in `ProjectConfig.Theme`

### Focus order

0=name, 1=path, 2=theme selector, 3=add button, 4=cancel button

### Files

- `internal/app/model.go` — new fields, `initProjectAddThemePicker()`, `resetProjectAddThemePicker()`
- `internal/app/update.go` — key handling for theme picker within add-project context
- `internal/app/view.go` — render theme field + mini picker modal

---

## Part 5: Live Preview in Project Switcher (Phase 2)

### Behavior

As the user moves the cursor in the `@` project switcher, the theme updates to show what that project looks like.

### Implementation

In `update.go` project switcher key handlers (up/down/mouse), after cursor moves:

```go
if idx >= 0 && idx < len(projects) {
    resolved := styles.ResolveTheme(m.cfg, projects[idx].Path)
    styles.ApplyResolved(resolved)
}
```

On close/cancel, restore the current project's theme:

```go
resolved := styles.ResolveTheme(m.cfg, m.ui.WorkDir)
styles.ApplyResolved(resolved)
```

This mirrors the existing live-preview pattern in the theme switcher.

### Files

- `internal/app/update.go` (project switcher section, ~line 385-488) — add preview calls on cursor change
- `internal/app/model.go` (`resetProjectSwitcher`) — restore theme on close

---

## Part 6: Doc Updates

### `docs/deprecated/guides/theme-creator-guide.md`

- Update the "Community Themes" section to document the new storage format (`community` field)
- Add a "Per-Project Themes" section explaining the config format
- Show example of project-specific theme in config.json

---

## Migration Strategy

1. On config load, if `overrides["communityName"]` exists:
   - Move value to `Theme.Community`
   - Remove `communityName` from overrides
   - Clear all community-derived keys from overrides (since they'll be computed fresh)
   - This happens transparently in `loader.go`
2. On next save (any theme change), the clean format is written
3. Old configs continue to work as-is until the user changes themes

---

## File Change Summary

| File                                    | Changes                                                                                                  |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `internal/config/config.go`             | Add `Community` to `ThemeConfig`, add `Theme *ThemeConfig` to `ProjectConfig`                            |
| `internal/config/loader.go`             | Migrate `communityName` from overrides, parse project themes                                             |
| `internal/config/saver.go`              | Add `SaveCommunityTheme()`, `SaveProjectTheme()`, update serialization                                   |
| `internal/styles/resolve.go` (NEW)      | `ResolvedTheme`, `ResolveTheme()`, `ApplyResolved()`                                                     |
| `internal/styles/resolve_test.go` (NEW) | Tests for resolution logic                                                                               |
| `internal/app/model.go`                 | Theme-aware `switchProject`, helpers, add-project theme picker state                                     |
| `internal/app/update.go`                | Theme switcher scope selector + save logic, project switcher live preview, add-project theme picker keys |
| `internal/app/view.go`                  | Add-project theme field, scope selector in `#` modal                                                     |
| `cmd/sidecar/main.go`                   | Use `ResolveTheme`+`ApplyResolved` instead of direct apply                                               |
| `docs/deprecated/guides/theme-creator-guide.md`    | Document community storage fix + per-project themes                                                      |

---

## Verification

1. `go build ./...` — compiles cleanly
2. `go test ./...` — all tests pass (including new resolve_test.go)
3. Manual: set a community theme via `#` → Tab → Enter → verify config.json has `"community"` field (not bloated overrides)
4. Manual: add a project with a different theme → switch projects with `@` → verify theme changes
5. Manual: open `@` switcher → navigate → verify live theme preview
6. Manual: open `#` → verify scope selector at bottom defaults to "Set globally" with hint text
7. Manual: toggle scope to "Set for this project" → select theme → verify it saves to project config only
8. Manual: ensure existing configs without per-project themes still work (global theme applies everywhere)

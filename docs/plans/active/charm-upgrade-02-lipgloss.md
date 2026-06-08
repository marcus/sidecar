# Lip Gloss v1 → v2 Upgrade Plan

> Status: **PLAN / not started** · Part of the Phase 1 atomic trio (see [overview](charm-upgrade-00-overview.md)).
> Do this **first** within Phase 1 — it is the foundation bubbletea/bubbles build on.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/lipgloss` | **`charm.land/lipgloss/v2`** |
| Version | `v1.1.1-0.20250404203927-76690c660834` | **`v2.0.3`** (re-verify) |

`go.mod` line becomes: `charm.land/lipgloss/v2 v2.0.3`. Requires Go 1.25 (already satisfied).

**Do NOT use `github.com/charmbracelet/lipgloss/v2`** — that path is frozen at `v2.0.0-beta.3` (July 2025). The GA line lives only at `charm.land/lipgloss/v2`.

## What changed upstream (the parts that matter for sidecar)

1. **`lipgloss.Color` is now a function, not a type.** v1: `type Color string`. v2: `func Color(s string) color.Color`. So `lipgloss.Color("#abc")` still works as a *call*, but `lipgloss.Color` used as a *type* (var/field/param/return/slice/assertion) no longer compiles.
2. **The whole `Renderer` model is gone** (`NewRenderer`, `DefaultRenderer`, `SetColorProfile`, `SetHasDarkBackground`). **Sidecar never used these** — confirmed zero matches. Nothing to remove.
3. **`AdaptiveColor` / `CompleteColor` / `TerminalColor` removed** from the root package. **Sidecar never used AdaptiveColor/CompleteColor** — confirmed zero matches. `TerminalColor` is referenced indirectly via `GetForeground()/GetBackground()` return types (see below).
4. **`color.Color` (stdlib `image/color`) is the universal color interface now.** Every method that took `TerminalColor` now takes `color.Color`. `GetForeground()/GetBackground()` return `color.Color`.
5. **`NoColor` still exists** (`type NoColor struct{}`). The one assertion `fg.(lipgloss.NoColor)` in `diff_renderer.go:158` still compiles.
6. **`Copy()` is deprecated but still works** (no-op returning self). Sidecar has **zero** `.Copy()` calls on styles (confirmed). Nothing to do.
7. **Layout/border APIs unchanged**: `JoinHorizontal`, `JoinVertical`, `Place`, `PlaceHorizontal`, `Width()`, `Height()`, `NormalBorder()`, `RoundedBorder()`, `Border`, `BorderForeground`, all `Style` builder methods (`Foreground`, `Background`, `Bold`, `Padding`, `Width`, `Render`, …) keep names and signatures.
8. **Downsampling moved to output time.** In a Bubble Tea v2 program (sidecar's case), the runtime handles color downsampling — nothing to do. (Only standalone tools need `lipgloss.Println`.)

## The work, grounded in the codebase

### Step 1 — Rewrite the import path (62 files)

```bash
# from repo root
grep -rl '"github.com/charmbracelet/lipgloss"' --include='*.go' . \
  | xargs sed -i '' 's#github.com/charmbracelet/lipgloss#charm.land/lipgloss/v2#g'
```

This also covers any `lipgloss/table|list|tree` subpackage imports (sidecar uses none — confirmed). Verify nothing now imports both old and new paths.

### Step 2 — Replace `lipgloss.Color` used AS A TYPE with `color.Color` (the big one)

Add `import "image/color"` to each file below and change the type references. **Keep `lipgloss.Color("…")` calls as-is** — only the *type* uses change.

| File:line | Current | Change to |
|-----------|---------|-----------|
| [internal/ui/confirm_dialog.go:15](../../../internal/ui/confirm_dialog.go) | `BorderColor lipgloss.Color` (struct field) | `BorderColor color.Color` |
| [internal/app/issue_preview_modal.go:19](../../../internal/app/issue_preview_modal.go) | `var issueTypeColors = map[string]lipgloss.Color{` | `map[string]color.Color{` |
| [internal/app/intro.go:58](../../../internal/app/intro.go) | `func (c RGB) toLipgloss() lipgloss.Color` | `func (c RGB) toLipgloss() color.Color` |
| [internal/plugins/workspace/view_kanban.go:58](../../../internal/plugins/workspace/view_kanban.go) | `map[WorktreeStatus]lipgloss.Color{` | `map[WorktreeStatus]color.Color{` |
| [internal/plugins/filebrowser/view_blame.go:232](../../../internal/plugins/filebrowser/view_blame.go) | `func getBlameAgeColor(...) lipgloss.Color` | `... color.Color` |
| [internal/plugins/gitstatus/minimap.go:236](../../../internal/plugins/gitstatus/minimap.go) | `func mmColor(...) lipgloss.Color` | `... color.Color` |
| [internal/styles/styles.go:429](../../../internal/styles/styles.go) | `func tabTextColor(...) lipgloss.Color` | `... color.Color` |
| [internal/styles/styles.go:431,434](../../../internal/styles/styles.go) | `candidates := []lipgloss.Color{...}` | `[]color.Color{...}` |
| [internal/styles/styles.go:458](../../../internal/styles/styles.go) | `func colorToRGB(c lipgloss.Color) RGB` | `func colorToRGB(c color.Color) RGB` — **see Step 4** |
| [internal/styles/styles.go:471](../../../internal/styles/styles.go) | `func RenderPill(text string, fg, bg, outerBg lipgloss.Color) string` | `... color.Color` |
| [internal/styles/styles.go:496](../../../internal/styles/styles.go) | `func RenderPillWithStyle(text string, style lipgloss.Style, outerBg lipgloss.Color) string` | `outerBg color.Color` |
| [internal/styles/fill_background.go:15](../../../internal/styles/fill_background.go) | `func FillBackground(content string, width int, bgColor lipgloss.Color) string` | `bgColor color.Color` |
| [internal/styles/fill_background.go:47](../../../internal/styles/fill_background.go) | `func BgANSISeqFor(bgColor lipgloss.Color) string` | `bgColor color.Color` |

> `internal/styles/styles.go` holds ~129 package-level color vars declared as `var Primary = lipgloss.Color("#…")`. Those use **type inference**, so they automatically become `color.Color` in v2 — no edit needed there. Only *explicit* `lipgloss.Color` type annotations (above) break. Confirm with: `grep -rnE 'lipgloss\.Color\b[^(]' --include='*.go' .` returning zero after this step (every remaining `lipgloss.Color` should be followed by `(`).

### Step 3 — Drop the `.(lipgloss.Color)` type assertions (4 sites)

You cannot assert to a function. In all four cases the source (`GetForeground()/GetBackground()`) and destination are now `color.Color`, so just drop the assertion.

```go
// view_kanban.go:59,61 — BEFORE
StatusActive:  styles.StatusCompleted.GetForeground().(lipgloss.Color),
StatusWaiting: styles.StatusModified.GetForeground().(lipgloss.Color),
// AFTER (map value type is now color.Color per Step 2)
StatusActive:  styles.StatusCompleted.GetForeground(),
StatusWaiting: styles.StatusModified.GetForeground(),
```

```go
// view_kanban.go:80 — BEFORE
headerStyle = lipgloss.NewStyle().Bold(true).
    Foreground(styles.Muted.GetForeground().(lipgloss.Color)).Width(colWidth)
// AFTER — Foreground() takes color.Color, GetForeground() returns color.Color
headerStyle = lipgloss.NewStyle().Bold(true).
    Foreground(styles.Muted.GetForeground()).Width(colWidth)
```

```go
// styles.go:502 (inside RenderPillWithStyle) — BEFORE
bg, _ := style.GetBackground().(lipgloss.Color)
// AFTER
bg := style.GetBackground()   // color.Color; feeds colorToRGB (Step 4)
```

Affected files: [view_kanban.go:59,61,80](../../../internal/plugins/workspace/view_kanban.go), [styles.go:502](../../../internal/styles/styles.go).

### Step 4 — Fix RGB extraction in the gradient/pill/tab code (semantic change, handle carefully)

In v1, `lipgloss.Color` was a `string`, so the helpers in `internal/styles/styles.go` parsed the hex string directly. In v2 the values are opaque `color.Color`, so RGB must be obtained via the stdlib `color.Color.RGBA()` method (which returns **16-bit alpha-premultiplied** components 0–65535).

Audit `colorToRGB`, `HexToRGB`, `RGBToHex`, `interpolateColors`, `RenderPill`, `renderGradientTab`, and `tabTextColor` in [internal/styles/styles.go](../../../internal/styles/styles.go) (and `internal/styles/borders.go` for gradient borders). Where code did `string(c)` and parsed `#rrggbb`, replace with:

```go
func colorToRGB(c color.Color) RGB {
    r, g, b, _ := c.RGBA()      // 16-bit, premultiplied
    return RGB{
        R: uint8(r >> 8),
        G: uint8(g >> 8),
        B: uint8(b >> 8),
    }
}
```

> Caveat: this resolves ANSI-indexed colors (e.g. `lipgloss.Color("196")`) to their RGB approximation via lipgloss's color tables, which is the desired behavior for gradient math. Visually QA the tab bar, pills, and gradient borders after this change — they are the most fragile part of the lipgloss migration. If any helper still needs the original hex *string*, keep a parallel string field on the theme palette (`internal/styles/themes.go` already stores hex strings in `ColorPalette`) rather than round-tripping through `color.Color`.

### Step 5 — Verify `NoColor` assertion still compiles

[internal/plugins/gitstatus/diff_renderer.go:158](../../../internal/plugins/gitstatus/diff_renderer.go): `_, isNoColor := fg.(lipgloss.NoColor)`. `NoColor` is still a struct type in v2, so this compiles unchanged. No action; just confirm.

### Step 6 — Downsampling / dark background

No action. Sidecar renders entirely through the Bubble Tea program, which downsamples in v2. Sidecar does not use `AdaptiveColor` or auto dark-background detection (themes are user-selected in `internal/styles/themes.go`), so there is no light/dark handshake to wire. If you later want auto theme selection, that is a *new feature*, not part of this upgrade — it would use `tea.RequestBackgroundColor` (see [bubbletea plan](charm-upgrade-03-bubbletea.md)).

## Ordered checklist

1. [ ] `go get charm.land/lipgloss/v2@v2.0.3` (re-verify version first)
2. [ ] Rewrite import path in all 62 files (Step 1 sed)
3. [ ] Add `import "image/color"` + change `lipgloss.Color` type uses → `color.Color` (Step 2 table)
4. [ ] Drop the 4 `.(lipgloss.Color)` assertions (Step 3)
5. [ ] Rework `colorToRGB` and friends to use `.RGBA()` (Step 4)
6. [ ] Confirm `grep -rnE 'lipgloss\.Color\b[^(]'` returns nothing (every survivor is a `Color(` call)
7. [ ] Confirm `grep -rn 'lipgloss.TerminalColor\|lipgloss.AdaptiveColor\|lipgloss.CompleteColor\|lipgloss.NewRenderer\|lipgloss.DefaultRenderer\|SetColorProfile' --include='*.go'` returns nothing
8. [ ] `go build ./...` — **will still be red** until bubbletea + bubbles are migrated (shared `color.Color` types in component calls). That is expected; proceed to [02-bubbletea](charm-upgrade-03-bubbletea.md).

## Common compile errors you will see

- `cannot use "21" (untyped string constant) as lipgloss.Color value` → you left a `var x lipgloss.Color = "…"`; make the var `color.Color` and call `lipgloss.Color("…")`.
- `lipgloss.Color (type) is not an expression` / `invalid type assertion: x.(lipgloss.Color)` → a leftover Step 3 assertion.
- `undefined: lipgloss.TerminalColor` → replace with `color.Color`.
- Colors render but gradients/pills look wrong → Step 4 RGB extraction; check the `>> 8` shift and ANSI-index resolution.

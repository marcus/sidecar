# Glamour v0.10 → v2 Upgrade Plan

> Status: **PLAN / not started** · **Phase 2** — separate follow-up PR, after [lipgloss v2](charm-upgrade-02-lipgloss.md) is merged.
> Smallest blast radius of all the upgrades: **one file**.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/glamour` | **`charm.land/glamour/v2`** |
| Version | `v0.10.0` | **`v2.0.0`** (re-verify) |

`go.mod`: `charm.land/glamour/v2 v2.0.0`. Do NOT use `github.com/charmbracelet/glamour/v2`.

## Dependency note

**Glamour v2 depends on Lip Gloss v2.** It does NOT need bubbletea or bubbles v2. So it can be its own PR, but only *after* lipgloss v2 is in main (Phase 1). If you'd rather, it can be folded into the Phase 1 atomic change — but isolating it keeps Phase 1 smaller.

## The only file: `internal/markdown/renderer.go`

Current usage ([internal/markdown/renderer.go:9,74,103-122](../../../internal/markdown/renderer.go)):
```go
import "github.com/charmbracelet/glamour"

renderer, err := glamour.NewTermRenderer(
    glamour.WithStylePath(styles.GetMarkdownTheme()),   // line 110
    glamour.WithWordWrap(width),                          // line 111
)
rendered, err := renderer.Render(content)                 // line 74
```

### Step 1 — Import path
```go
import "charm.land/glamour/v2"
```

### Step 2 — Verify the options used are still present
- `glamour.WithStylePath(...)` — **retained** in v2. No change.
- `glamour.WithWordWrap(...)` — **retained** in v2. No change.
- `glamour.NewTermRenderer(...)` and `.Render(...)` — core signatures **unchanged**.

What was **removed** in v2 (sidecar does NOT use these, so no impact — just don't add them):
- `glamour.WithAutoStyle()` — gone; default style is now `"dark"`.
- `glamour.WithColorProfile()` — gone; downsampling is handled by lipgloss v2 at output.
- The `Overlined` style field.

### Step 3 — Confirm `styles.GetMarkdownTheme()` still resolves

`WithStylePath` takes a path to a JSON style (or a built-in style name). Check what `styles.GetMarkdownTheme()` returns ([internal/styles/](../../../internal/styles/)):
- If it returns a **built-in name** (e.g. `"dark"`, `"dracula"`), verify the name still exists in v2 (v2 added `"tokyo-night"`; existing names persist).
- If it returns a **file path** to a custom JSON style, verify the JSON schema still loads (v2 dropped the `Overlined` field — if the custom JSON sets it, remove that key).

### Step 4 — Output path (no change)

The rendered markdown string is composed into the TUI (cached in the `Renderer` wrapper) and displayed through the Bubble Tea program, which downsamples color in v2. So you do **not** need `lipgloss.Println`. No change to how `rendered` is consumed.

## Ordered checklist

1. [ ] `go get charm.land/glamour/v2@v2.0.0` (re-verify version)
2. [ ] Change the import in `internal/markdown/renderer.go`
3. [ ] Confirm `WithStylePath` + `WithWordWrap` compile (they should, unchanged)
4. [ ] Verify `GetMarkdownTheme()` value loads under v2 (Step 3)
5. [ ] `go build ./...` && `go test ./...`
6. [ ] Manual: render a markdown-heavy conversation/issue preview and eyeball formatting, code blocks, and links (v2 adds OSC 8 clickable links — a nice bonus to verify)

## Gotchas

- Wrong path (`github.com/charmbracelet/glamour/v2`) → resolves to a stale beta. Use `charm.land/glamour/v2`.
- If a custom JSON style theme is in the repo and sets `overlined`, v2 will ignore/reject it — strip that key.
- Word-wrap behavior was rewritten on `lipgloss.Wrap` in v2 (better CJK/emoji handling). Long lines may wrap slightly differently — verify the preview panes still fit their allocated width (CLAUDE.md height/width rule).

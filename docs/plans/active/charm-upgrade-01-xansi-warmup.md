# x/ansi Upgrade Plan — Phase 0 Warm-up

> Status: **PLAN / not started** · **Phase 0** — standalone, ships *before* the v2 trio.
> The one charmbracelet bump that is safe to do while still on v1. A clean, isolated PR.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/x/ansi` | `github.com/charmbracelet/x/ansi` (**unchanged** — no vanity domain, no `/v2`) |
| Version | `v0.11.3` | **`v0.11.7`** (re-verify) |

## Why it's safe and independent

The 0.11.x line (`v0.11.4 → v0.11.7`) contains only **width-calculation fixes and East-Asian-ambiguous-width options** — no signature changes to the functions sidecar uses. v1 lipgloss and bubbletea both tolerate the newer ansi, so this does not require (or conflict with) the v2 trio. Doing it first de-risks the big change by getting one dependency current in isolation.

## Codebase usage (24 files)

Functions in use and their call counts:
- `ansi.StringWidth(s)` — ~40 calls (visual width)
- `ansi.Strip(s)` — ~20 calls (remove ANSI)
- `ansi.Truncate(s, width, tail)` — ~15 calls
- `ansi.TruncateLeft(s, offset, tail)` — 4 calls

Representative files: [internal/ui/overlay.go](../../../internal/ui/overlay.go), [internal/ui/truncate_cache.go](../../../internal/ui/truncate_cache.go), [internal/ui/selection.go](../../../internal/ui/selection.go), [internal/ui/selection_render.go](../../../internal/ui/selection_render.go), [internal/tty/cursor.go](../../../internal/tty/cursor.go), [internal/modal/list.go](../../../internal/modal/list.go), [internal/modal/section.go](../../../internal/modal/section.go), and many `internal/plugins/workspace/` view files ([view_list.go](../../../internal/plugins/workspace/view_list.go), [view_modals.go](../../../internal/plugins/workspace/view_modals.go), [view_preview.go](../../../internal/plugins/workspace/view_preview.go), [terminal_panel.go](../../../internal/plugins/workspace/terminal_panel.go), [prompt_picker_modal.go](../../../internal/plugins/workspace/prompt_picker_modal.go), [interactive.go](../../../internal/plugins/workspace/interactive.go), …), plus [filebrowser/view.go](../../../internal/plugins/filebrowser/view.go) and [notes/view.go](../../../internal/plugins/notes/view.go).

All of these signatures are **stable across 0.11.x** — no code changes expected, only the version bump.

## The work

```bash
go get github.com/charmbracelet/x/ansi@v0.11.7
go mod tidy
go build ./...
go test ./...
```

That's it — no source edits anticipated.

## Verification

- `go build ./... && go test ./...` clean.
- **Spot-check column alignment** in views that mix wide / emoji / CJK glyphs (the 0.11.x changes are width-calc fixes — in rare edge cases a glyph may now measure one cell differently). Look at: the workspace list/preview panes, the file browser tree, and any table-like alignment. If something is off by a cell, it's almost certainly an intended width correction, not a regression.

## Gotchas

- **Do NOT rewrite the import path.** `x/ansi` keeps `github.com/charmbracelet/x/ansi`. Only the UI libraries (bubbletea/lipgloss/bubbles/glamour) moved to `charm.land`. A blanket `charm.land` sed would wrongly catch this — keep the x/* paths as-is.
- This PR should land and be verified *before* starting the [Phase 1 trio](charm-upgrade-02-lipgloss.md), so any width-related visual shifts are isolated from the much larger v2 changes.

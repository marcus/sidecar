# Charmbracelet Library Upgrade — Overview & Coordination

> Status: **PLAN / not started**
> Tracking task: `td-e17cb9`
> Author research date: 2026-06-08 (verify versions again at execution time — see "Re-verify before starting")

This is the master document for upgrading sidecar's Charmbracelet dependencies. **The per-library files are numbered in execution order** — read this first, then work through them in sequence:

| # | Library | File | Current | Target | Phase | Risk |
|---|---------|------|---------|--------|-------|------|
| 01 | x/ansi | [charm-upgrade-01-xansi-warmup.md](charm-upgrade-01-xansi-warmup.md) | `v0.11.3` | `v0.11.7` | 0 (standalone) | Low |
| 02 | Lip Gloss | [charm-upgrade-02-lipgloss.md](charm-upgrade-02-lipgloss.md) | `v1.1.1-0.2025…` | `charm.land/lipgloss/v2 v2.0.3` | 1 (atomic) | **High** (color types) |
| 03 | Bubble Tea | [charm-upgrade-03-bubbletea.md](charm-upgrade-03-bubbletea.md) | `v1.3.10` | `charm.land/bubbletea/v2 v2.0.7` | 1 (atomic) | **High** (key/mouse/View) |
| 04 | Bubbles | [charm-upgrade-04-bubbles.md](charm-upgrade-04-bubbles.md) | `v0.21.1-0.2025…` | `charm.land/bubbles/v2 v2.1.0` | 1 (atomic) | Medium |
| 05 | Glamour | [charm-upgrade-05-glamour.md](charm-upgrade-05-glamour.md) | `v0.10.0` | `charm.land/glamour/v2 v2.0.0` | 2 (follow-up) | Low (1 file) |
| 06 | x/cellbuf + cleanup | [charm-upgrade-06-cellbuf.md](charm-upgrade-06-cellbuf.md) | `v0.0.14` | `v0.0.15` | 2 (post-trio) | Low |

> The number is execution order, not import-graph order. Files 02→03→04 are the **Lip Gloss → Bubble Tea → Bubbles** dependency chain and ship as one atomic change (Phase 1).

## TL;DR — the three things that make this hard

1. **The module path changes to a vanity domain.** v2 of bubbletea/lipgloss/bubbles/glamour are published under `charm.land/...`, NOT `github.com/charmbracelet/.../v2`. The `github.com/charmbracelet/.../v2` paths exist but are **frozen at 2025 betas** — do not use them. The VCS still lives on github.com; only the Go module identity moved. (The `x/*` utility libraries keep their `github.com/charmbracelet/x/...` path — only the UI libraries moved.)

2. **lipgloss v2 + bubbletea v2 + bubbles v2 are a single locked unit.** `bubbles/v2`'s `go.mod` hard-requires `bubbletea/v2 v2.0.7` and `lipgloss/v2 v2.0.3`. The shared types (`tea.KeyPressMsg`, `tea.View`, `image/color.Color`-based styling) mean there is **no compiling half-migrated state**. These three must land in one atomic change (files 02→03→04). Glamour can trail in a separate PR (it only needs lipgloss v2).

3. **`lipgloss.Color` stops being a type.** In v1 it is `type Color string`; in v2 it is a *function* returning `image/color.Color`. Sidecar uses `lipgloss.Color` as a type in ~18 places (struct fields, map keys, func params/returns, slices) and does 4 `.(lipgloss.Color)` type assertions. All of these break and must move to `color.Color`. This is the single largest source of mechanical churn.

## Scope (grounded in the current codebase)

Counted via `grep -rhoE '"github.com/charmbracelet/[^"]+"' --include="*.go"`:

| Import | Files | Notes |
|--------|-------|-------|
| `bubbletea` | 109 | 1 `tea.NewProgram` ([cmd/sidecar/main.go:218](../../../cmd/sidecar/main.go)) |
| `lipgloss` | 62 | Centralized in `internal/styles/` |
| `x/ansi` | 24 | `StringWidth`, `Strip`, `Truncate`, `TruncateLeft` |
| `bubbles/textinput` | 17 | |
| `bubbles/textarea` | 4 | |
| `x/cellbuf` | 2 | `cellbuf.Wrap` |
| `bubbles/key` | 2 | **No API change in v2** — path only |
| `glamour` | 1 | [internal/markdown/renderer.go](../../../internal/markdown/renderer.go) |

Bubbles components NOT used (confirmed): `viewport`, `spinner`, `list`, `table`, `paginator`, `help`, `progress`, `cursor`, `filepicker`. The "viewport" mentions in `internal/modal/` are sidecar's own scroll concept, not `bubbles/viewport`. The spinner in `internal/ui/braille_spinner.go` is custom, not `bubbles/spinner`. This significantly narrows the bubbles plan.

x/* packages that are **only indirect** today (not imported in source): `x/term`, `x/exp/*`, `x/mosaic`, `x/conpty`, `x/xpty`, `colorprofile`, `huh`. They float as transitive deps.

## Recommended sequencing

### Phase 0 — warm-up (file 01, independent, low risk, ships on its own)
Bump `x/ansi` → `v0.11.7` (width-calc fixes only; signatures unchanged) while still on v1. This is the only safe pre-bump. See [01-xansi-warmup](charm-upgrade-01-xansi-warmup.md).

### Phase 1 — the v2 trio (files 02→03→04, ONE atomic PR)
Do the code migration in dependency order inside the single change:
1. **Lip Gloss v2** ([02](charm-upgrade-02-lipgloss.md)) — the foundation. Fix `lipgloss.Color`-as-type → `color.Color`, drop `.(lipgloss.Color)` assertions, rewrite import paths.
2. **Bubble Tea v2** ([03](charm-upgrade-03-bubbletea.md)) — `View() string` → `View() tea.View` on the root model only, move `NewProgram` options to View fields, `tea.KeyMsg` → `tea.KeyPressMsg`, rework mouse + paste.
3. **Bubbles v2** ([04](charm-upgrade-04-bubbles.md)) — `textinput.Width` field → `SetWidth()`, `textarea.Style` → `StyleState` / `Styles.Focused`, `SetCursor` → `SetCursorColumn`, path-only change for `key`.

`colorprofile v0.4.3`, `x/term v0.2.2`, `x/ansi v0.11.7`, `x/exp/golden` resolve automatically via `go mod tidy`.

### Phase 2 — Glamour + cellbuf cleanup (files 05, 06, separate follow-up PRs)
- **Glamour v2** ([05](charm-upgrade-05-glamour.md)) — after lipgloss v2 is in main. Only `internal/markdown/renderer.go` changes.
- **x/cellbuf + transitive cleanup** ([06](charm-upgrade-06-cellbuf.md)) — `go mod tidy`, bump cellbuf, review the dep graph diff.

### Why not incremental within Phase 1
The module rename + hard `go.mod` requires + shared type changes mean you cannot, e.g., have bubbles on v2 while bubbletea is still v1. The compile will not succeed until all three call-sites are migrated. Plan for `go build ./...` to be **red for the entire duration** of Phase 1 and only go green at the end. Work file-by-file but expect no intermediate green build.

## Re-verify before starting (versions move)

The version numbers below were researched 2026-06-08; confirm the current latest before executing:

```bash
go list -m -versions charm.land/lipgloss/v2
go list -m -versions charm.land/bubbletea/v2
go list -m -versions charm.land/bubbles/v2
go list -m -versions charm.land/glamour/v2
go list -m -versions github.com/charmbracelet/x/ansi
```

Also re-read the official upgrade guides (they are the source of truth and may have been updated):
- bubbletea: https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md
- lipgloss: https://github.com/charmbracelet/lipgloss/blob/main/UPGRADE_GUIDE_V2.md
- bubbles: https://github.com/charmbracelet/bubbles/blob/main/UPGRADE_GUIDE_V2.md

## Prerequisites

- **Go 1.25+** required by all v2 modules (their `go.mod` declares `go 1.25.0`). Sidecar is already on `go 1.25.5` in [go.mod](../../../go.mod) and the dev machine has Go 1.26.4 — **prerequisite already met.**

## Target version cheat-sheet (for go.mod)

```
// v2 UI trio — must move together, vanity paths (Phase 1):
charm.land/lipgloss/v2          v2.0.3
charm.land/bubbletea/v2         v2.0.7
charm.land/bubbles/v2           v2.1.0

// follow-up PR (needs lipgloss v2) (Phase 2):
charm.land/glamour/v2           v2.0.0

// standalone warm-up, same path, no /v2 (Phase 0):
github.com/charmbracelet/x/ansi         v0.11.7

// float automatically with the v2 bump (direct deps of bt2/lg2):
github.com/charmbracelet/colorprofile   v0.4.3
github.com/charmbracelet/x/term         v0.2.2
github.com/charmbracelet/x/exp/golden   (pseudo, test only)

// bump explicitly ONLY if still directly imported after tidy (Phase 2):
github.com/charmbracelet/x/cellbuf      v0.0.15   // direct import in 2 files
```

> Note: lipgloss v2 switched its internal cell engine from `x/cellbuf` to `github.com/charmbracelet/ultraviolet`. cellbuf will likely **drop out of the transitive graph**, but sidecar imports it directly (2 files), so it stays as a direct dependency. Run `go mod why github.com/charmbracelet/x/cellbuf` after the bump.

## Risk register

| Risk | Where | Mitigation |
|------|-------|------------|
| Color rendering regressions (wrong/no color) | All views | Manual visual QA under light + dark terminals; the gradient/pill/tab code in `internal/styles/` extracts RGB and is most fragile |
| Mouse stops working | Whole app | Mouse is **opt-in** in v2 via `view.MouseMode`; forgetting it = silent dead mouse. Test click/scroll/drag everywhere |
| Paste broken in interactive shell | `internal/tty/`, workspace | Paste moved from `KeyMsg.Paste` to `tea.PasteMsg`; the tty/tmux key forwarding needs rework + manual paste test |
| `space` key matches stop firing | Anywhere matching `" "` | `msg.String()` returns `"space"` now; grep for `case " "` |
| Test suite breakage | ~30 test files | `tea.KeyMsg{...}` / `tea.MouseMsg{...}` struct literals no longer compile (now interfaces); rewrite constructors |
| Renderer/cursor overlay conflicts | `internal/tty/cursor.go`, tty View | v2 "Cursed Renderer" manages the real cursor via `view.Cursor`; manual cursor escapes fight it |
| SSH/profile color | N/A locally | Sidecar runs locally under a tea program, so bubbletea v2 handles downsampling — low risk here |

## Definition of done (per phase)

- `go build ./...` clean
- `go vet ./...` clean
- `go test ./...` green (after updating test constructors)
- Manual smoke test: launch sidecar, switch every plugin tab, exercise mouse click/scroll/drag, attach to a tmux workspace + paste, open a file in editor (`tea.ExecProcess`) and return, switch theme, resize the terminal. Verify the header never scrolls off (see CLAUDE.md plugin-height rule).
- Run under both a dark and a light terminal profile to confirm colors.

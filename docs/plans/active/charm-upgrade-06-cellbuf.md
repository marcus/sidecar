# x/cellbuf + Transitive Dependency Cleanup

> Status: **PLAN / not started** · **Phase 2** — runs *after* the [v2 trio](charm-upgrade-02-lipgloss.md) lands.
> Low risk. This is the dependency-graph tidy-up that follows the big change.

## x/cellbuf (2 files)

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/x/cellbuf` (**unchanged**) | same |
| Version | `v0.0.14` | **`v0.0.15`** (re-verify) |

`cellbuf.Wrap(text, width, prefix)` is used in:
- [internal/plugins/filebrowser/view.go:885](../../../internal/plugins/filebrowser/view.go)
- [internal/plugins/notes/view.go:355](../../../internal/plugins/notes/view.go)

### Why this is a Phase 2 step

lipgloss v2 **dropped cellbuf** — it switched its internal cell engine to `github.com/charmbracelet/ultraviolet`. cellbuf was only a *beta-era* transitive dependency of the v2 stack. So after the trio lands you must check whether cellbuf is still in the graph:

```bash
go mod why github.com/charmbracelet/x/cellbuf
```

- It **will** still be present because sidecar imports it **directly** (the 2 files above). Keep it as a direct dependency.
- Bump it to `v0.0.15` (module path unchanged):

```bash
go get github.com/charmbracelet/x/cellbuf@v0.0.15
```

- Verify `cellbuf.Wrap` is still `func(string, int, string) string` — it has been stable. No source changes anticipated.

> Optional cleanup (out of scope, but noted): both `cellbuf.Wrap` call-sites could move to `lipgloss.Wrap` (added in lipgloss v2) to drop the direct cellbuf dependency entirely. Only worth it if you want one fewer dependency; functionally `cellbuf.Wrap` keeps working.

## Transitive dependencies — let `go mod tidy` resolve them

These are NOT imported in sidecar source (confirmed); they appear only as `// indirect` in [go.mod](../../../go.mod). After the Phase 1 v2 bump, run `go mod tidy` and they resolve to whatever the v2 modules require:

| Library | Resolves via | Expected version |
|---------|-------------|------------------|
| `colorprofile` | direct dep of bubbletea v2 / lipgloss v2 | `v0.4.3` |
| `x/term` (charmbracelet) | direct dep of bubbletea v2 / lipgloss v2 | `v0.2.2` |
| `x/exp/golden` | test dep of the v2 modules | pseudo-version |
| `x/exp/slice`, `x/exp/strings` | transitive | float |
| `x/conpty`, `x/xpty` | PTY helpers (only if pulled) | float |
| `x/mosaic` | image-to-ANSI (independent) | float |
| `ultraviolet`, `clipperhouse/displaywidth` | **new** — lipgloss v2 internal engine + width | pulled by lipgloss v2 |

**Action:** none beyond `go mod tidy` after Phase 1, then eyeball the `go.mod`/`go.sum` diff:
- **New** entries expected: `ultraviolet`, `clipperhouse/displaywidth` (and friends).
- **Pruned** entries expected: v1-only deps; `x/cellbuf` may drop from *indirect* (but stays as a *direct* require — see above).
- Do **not** hand-pin transitive deps; only bump the directly-imported ones.

## Important: `golang.org/x/term` is NOT part of this upgrade

Sidecar uses `golang.org/x/term` (the **golang.org**, not charmbracelet, package) for terminal save/restore around `tea.ExecProcess`:
- [internal/app/update.go:337](../../../internal/app/update.go) — `term.GetState(int(os.Stdout.Fd()))` / `term.Restore(...)`
- [internal/plugins/workspace/interactive.go:770+](../../../internal/plugins/workspace/interactive.go)

This is `golang.org/x/term v0.39.0` and is unrelated to the charmbracelet upgrade — **leave it alone**. Don't confuse it with `github.com/charmbracelet/x/term`.

## Ordered checklist (after the Phase 1 trio is in)

1. [ ] `go mod tidy`
2. [ ] `go mod why github.com/charmbracelet/x/cellbuf` → confirm still direct
3. [ ] `go get github.com/charmbracelet/x/cellbuf@v0.0.15`
4. [ ] Review the `go.mod`/`go.sum` diff for new (`ultraviolet`, `displaywidth`) and pruned entries
5. [ ] `go build ./... && go test ./...`
6. [ ] Manual smoke test of the file browser and notes wrap behavior (the two `cellbuf.Wrap` sites)

## Gotchas

- `x/cellbuf`, `x/ansi`, `x/term`, `colorprofile` keep the `github.com/charmbracelet/...` path. Only the UI libraries moved to `charm.land`. Don't accidentally rewrite the x/* paths.
- Let `go mod tidy` do the transitive resolution; bump by hand only the directly-imported `x/cellbuf`.

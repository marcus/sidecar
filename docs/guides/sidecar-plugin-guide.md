# Sidecar Plugin Implementation Guide

A practical, code-oriented guide for building first-class Sidecar plugins. Use this as a checklist while you work.

## Architecture in 90 seconds
- **Bubble Tea model**: `internal/app/model.go` owns the active plugin index, dispatches key events, and renders plugin views.
- **Registry**: `internal/plugin/registry.go` stores plugins, handles lifecycle with panic protection, and keeps an `unavailable` map when `Init` fails (silent degradation).
- **Plugin contract**: `internal/plugin/plugin.go` defines the interface every plugin must satisfy and optional `DiagnosticProvider`.
- **Context**: `internal/plugin/context.go` is passed into `Init` and provides `WorkDir`, `ConfigDir`, `Adapters`, `EventBus`, and a structured `Logger`.
- **Keymap**: `internal/keymap` maps keys to command IDs. The footer/help UI reads bindings by *context* and uses `Plugin.Commands()` + `Plugin.FocusContext()` to decide which hints to show.

## Plugin interface expectations
- `ID() string`: Stable, kebab-case identifier (used for contexts and registry lookups).
- `Name() string`: Short human label for headers/help.
- `Icon() string`: Single-character glyph for tab strip.
- `Init(ctx *Context) error`: No side-effects beyond validation and lightweight setup. Return an error to mark the plugin as unavailable (app keeps running).
- `Start() tea.Cmd`: Kick off async work (initial loads, watchers). Must be non-blocking.
- `Update(msg tea.Msg) (Plugin, tea.Cmd)`: Pure state transition; return `p` and a command. Keep heavy work in commands, not inside `Update`.
- `View(width, height int) string`: Render based on provided size; cache `width/height` inside state if needed.
- `IsFocused()/SetFocused(bool)`: Used by the app when switching tabs; guard focus-only actions with this flag.
- `Commands() []plugin.Command`: Declarative list of actions per *context* so the footer/help can surface key hints.
- `FocusContext() string`: Current context name (see Keymap section). Must change when your view mode changes so key hints stay accurate.
- `Stop()`: Idempotent cleanup of goroutines, watchers, channels.
- Optional `Diagnostics() []plugin.Diagnostic`: Implement when you want `!` (diagnostics overlay) to show health.

## Lifecycle order (and what to do in each stage)
1. **Registration** (`cmd/sidecar/main.go`): `registry.Register(myplugin.New())`. Do not perform work here.
2. **Init**: Detect prerequisites (repos, adapters, env vars). Use `ctx.Logger` for warnings; return an error to degrade gracefully (shows in “Unavailable plugins”).
3. **Start**: Batch initial commands with `tea.Batch` (e.g., first data fetch + watcher start). Never block.
4. **Update**: Pattern-match on custom `Msg` types and `tea.KeyMsg`. Keep I/O inside commands you trigger, not directly in `Update`.
5. **View**: Render only; avoid side-effects. Honor `width/height` so the layout fits when split panes change.
6. **Focus/Blur**: The app calls `SetFocused` when switching tabs. Use it to pause expensive work or change visual focus states.
7. **Stop**: Close watchers, timers, and channels; guard with `sync.Once`/flags to be safe on double-stop.

## Keymap, contexts, and commands
- Define **contexts** that mirror your view modes (e.g., `git-status`, `git-diff`, `conversation-detail`). Return the active one from `FocusContext`.
- Expose **commands** with matching contexts via `Commands()`. Each command gets an ID + human label; these power footer hints and the help overlay.
- Add default **bindings** in `internal/keymap/bindings.go` so your commands have keys. Bindings are looked up by context.
- Keep command IDs stable; they are referenced by bindings and help text. Prefer verbs (`open-file`, `toggle-diff-mode`) over nouns.

## Event bus (cross-plugin signals)
- Subscribe: `ch := ctx.EventBus.Subscribe("topic")` inside `Start()` and watch in a command/goroutine that forwards typed messages into `Update`.
- Publish: `ctx.EventBus.Publish("topic", event.NewEvent(event.TypeRefreshNeeded, "topic", payload))`.
- Delivery is best-effort, buffered (size 16), and drops when full; design listeners to be resilient.

## Adapters
- `ctx.Adapters` holds integrations (e.g., `claude-code`). Check capability/Detect in `Init` before using.
- Watchers from adapters should feed messages back through `Update`; avoid doing UI work directly in watcher goroutines.

## Error handling & logging
- Prefer returning lightweight errors from `Init`; registry records them without crashing the app.
- Use `ctx.Logger` with structured fields for anything user-visible or for debugging (`logger.Warn("fetch failed", "err", err)`).
- Surface recoverable issues as status/toast messages via app messages, not panics.

## Rendering & UX tips
- Keep `View` deterministic; drive all dynamic data through state mutated in `Update`.
- Cache `width/height` in the plugin state so background commands can format correctly (see git diff rendering).
- Provide footer hints by keeping `Commands()` and `FocusContext()` aligned with your current view mode.
- Prefer small helper render functions per view mode to keep code readable.

## Watchers and goroutines
- Start watchers in `Start()` via a `tea.Cmd` that spawns the goroutine and returns a typed `Msg` on events.
- Store handles (e.g., `watcher *Watcher`) on the plugin struct and close them in `Stop()`; guard with `sync.Once`/flags.
- Debounce noisy sources (see git status watcher) before sending refresh messages.

## Adding a new plugin: checklist
1. Create `internal/plugins/<id>/` with `plugin.go` plus supporting files.
2. Implement the `plugin.Plugin` interface; consider `DiagnosticProvider`.
3. Add registration in `cmd/sidecar/main.go`.
4. Add default key bindings for your contexts in `internal/keymap/bindings.go`.
5. Ensure `Commands()` covers every binding you expose so hints/help work.
6. Wire any external needs (adapters, env detection) in `Init` and degrade gracefully.
7. Provide cleanup in `Stop` and keep `Start`/`Update` non-blocking.

## Testing & debug habits
- Keep business logic in testable helpers; wire Bubble Tea plumbing around it.
- Use small typed messages (`type RefreshMsg struct{}`) to keep `Update` readable.
- Enable `--debug` to get verbose logs from registry and plugins.


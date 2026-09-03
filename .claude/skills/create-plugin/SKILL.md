---
name: create-plugin
description: >
  Create new sidecar plugins implementing the plugin.Plugin interface, rendering
  views with Bubble Tea, handling keyboard input via keymap contexts, and
  integrating with the app shell (footer hints, event bus, adapters). Use when
  creating a new plugin, modifying plugin architecture, or debugging plugin
  rendering/lifecycle issues. See references/ for sidebar list and fixed footer
  layout details.
---

# Create Plugin

**This skill is the embedded class: a Go package compiled into Sidecar with its own Bubble Tea UI.** If what you want is a tool's data in Sidecar — a searchable tab, rows that open into documents, panes beside a terminal — you almost certainly want a *protocol* plugin instead: an executable in any language answering five JSON methods, which Sidecar renders itself and which ships as a config entry rather than a Sidecar release. Read [docs/guides/active/creating-plugins.md](../../../docs/guides/active/creating-plugins.md) and stop here. Come back only when the screen itself is the point — a board, a queue, a layout the collection-and-document vocabulary cannot express — because everything below costs a release, a keymap pass, and a permanent place in the repository.

## Architecture Overview

- **Bubble Tea model**: `internal/app/model.go` owns the active plugin index, dispatches key events, renders plugin views.
- **Registry**: `internal/plugin/registry.go` stores plugins, handles lifecycle with panic protection, keeps an `unavailable` map when `Init` fails (silent degradation).
- **Plugin contract**: `internal/plugin/plugin.go` defines the interface every plugin must satisfy.
- **Context**: `internal/plugin/context.go` provides `WorkDir`, `ConfigDir`, `Adapters`, `EventBus`, `Logger`, `Epoch`, and `Keymap`.
- **Keymap**: `internal/keymap` maps keys to command IDs. Footer/help reads bindings by context using `Plugin.Commands()` + `Plugin.FocusContext()`.

## Plugin Interface

Every plugin must implement all of these methods:

```go
ID() string              // Stable kebab-case identifier
Name() string            // Short human label for headers/help
Icon() string            // Single-character glyph for tab strip
Init(ctx *Context) error // Lightweight setup; return error to degrade gracefully
Start() tea.Cmd          // Kick off async work (non-blocking)
Update(msg tea.Msg) (Plugin, tea.Cmd) // Pure state transition
View(width, height int) string        // Render within provided dimensions
IsFocused() bool         // Check focus state
SetFocused(bool)         // App calls this on tab switch
Commands() []plugin.Command           // Footer hints per context
FocusContext() string    // Current context name for keymap
Stop()                   // Idempotent cleanup
```

Optional: implement `Diagnostics() []plugin.Diagnostic` for the diagnostics overlay.

## Lifecycle Order

1. **Registration** (`cmd/sidecar/main.go`): `registry.Register(myplugin.New())`. No work here.
2. **Init**: Detect prerequisites (repos, adapters, env vars). Use `ctx.Logger` for warnings. Return error to degrade gracefully.
3. **Start**: Batch initial commands with `tea.Batch`. Never block.
4. **Update**: Pattern-match on custom `Msg` types and `tea.KeyMsg`. Keep I/O in commands, not directly in Update.
5. **View**: Render only; no side-effects. Honor `width/height`.
6. **Focus/Blur**: `SetFocused` called on tab switch. Pause expensive work when unfocused.
7. **Stop**: Close watchers, timers, channels. Guard with `sync.Once`/flags.

## Epoch Pattern (Stale Message Detection)

When switching projects/worktrees, async operations may deliver stale data. Use the epoch pattern:

### Step 1: Add Epoch to message type
```go
type MyDataLoadedMsg struct {
    Epoch uint64
    Data  string
    Err   error
}
func (m MyDataLoadedMsg) GetEpoch() uint64 { return m.Epoch }
```

### Step 2: Capture epoch in command creators
```go
func (p *Plugin) loadData() tea.Cmd {
    epoch := p.ctx.Epoch // Capture synchronously before closure
    return func() tea.Msg {
        data, err := fetchData()
        return MyDataLoadedMsg{Epoch: epoch, Data: data, Err: err}
    }
}
```

### Step 3: Check staleness in Update
```go
case MyDataLoadedMsg:
    if plugin.IsStale(p.ctx, msg) {
        return p, nil // Discard stale message
    }
    p.data = msg.Data
```

Apply this to any async message that fetches data from filesystem/external sources or updates project-specific state.

## Keymap, Contexts, and Commands

- Define **contexts** mirroring your view modes (e.g., `git-status`, `git-diff`). Return the active one from `FocusContext()`.
- Expose **commands** with matching contexts via `Commands()`. These power footer hints and help overlay.
- Add default **bindings** in `internal/keymap/bindings.go`.
- Keep command IDs stable (verbs preferred: `open-file`, `toggle-diff-mode`).

### Command structure
```go
plugin.Command{
    ID:       "stage-file",
    Name:     "Stage",           // Keep 1-2 words max
    Category: plugin.CategoryGit,
    Priority: 10,                // Lower = higher priority; 0 treated as 99
    Context:  "git-status",
}
```

Categories: `CategoryNavigation`, `CategoryActions`, `CategoryView`, `CategorySearch`, `CategoryEdit`, `CategoryGit`, `CategorySystem`

### Context naming convention
- `plugin-name` for main view
- `plugin-name-detail` for detail/preview
- `plugin-name-modal` for modals
- `plugin-name-search` for search modes

### Dynamic binding registration
```go
func (p *Plugin) Init(ctx *plugin.Context) error {
    if ctx.Keymap != nil {
        ctx.Keymap.RegisterPluginBinding("g g", "go-to-top", "my-context")
    }
    return nil
}
```

## Event Bus (Cross-Plugin Communication)

- Subscribe: `ch := ctx.EventBus.Subscribe("topic")` in `Start()`, forward messages into `Update`.
- Publish: `ctx.EventBus.Publish("topic", event.NewEvent(event.TypeRefreshNeeded, "topic", payload))`.
- Best-effort, buffered (size 16), drops when full. Design listeners to be resilient.

## Inter-Plugin Messages

App-level messages (`internal/app/commands.go`):
- `FocusPluginByIDMsg{PluginID}` / `app.FocusPlugin(id)`

File browser messages (`internal/plugins/filebrowser/plugin.go`):
- `NavigateToFileMsg{Path}` - navigate to and preview a file

Pattern for cross-plugin navigation:
```go
func (p *Plugin) openInFileBrowser(path string) tea.Cmd {
    return tea.Batch(
        app.FocusPlugin("file-browser"),
        func() tea.Msg { return filebrowser.NavigateToFileMsg{Path: path} },
    )
}
```

## Plugin Focus Events

`PluginFocusedMsg` (from `internal/app`): sent when your plugin becomes active tab. Use to refresh data only needed when visible:
```go
case app.PluginFocusedMsg:
    if p.pendingRefresh {
        p.pendingRefresh = false
        return p, p.refresh()
    }
```

## External Editor Integration

```go
func (p *Plugin) openFile(path string, lineNo int) tea.Cmd {
    editor := p.ctx.Config.EditorCommand
    return func() tea.Msg {
        return plugin.OpenFileMsg{Editor: editor, Path: path, LineNo: lineNo}
    }
}
```

## Rendering Rules

**CRITICAL: Always constrain plugin output height.** The app header/footer are always visible. Plugins must not exceed allocated height.

```go
lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
```

**Do NOT render footers in plugin View().** The app renders footer using `Commands()` and keymap bindings.

Additional rendering rules:
- Keep `View` deterministic; drive dynamic data through state in `Update`.
- Cache `width/height` in plugin state.
- Expand `\t` to spaces before width checks.
- Use ANSI-aware helpers (`ansi.Truncate`, `lipgloss.Width`) for content with escape codes.
- Use small helper render functions per view mode.

See `references/sidebar-list-guide.md` for scrollable list implementation patterns.
See `references/fixed-footer-layout-guide.md` for footer and layout math details.

## Persisting User Preferences

Use `internal/state` to persist layout preferences across restarts:
1. Add field to `state.State` struct with getter/setter.
2. Load in `Init()`: `if saved := state.GetMyPaneWidth(); saved > 0 { p.paneWidth = saved }`
3. Save on user action: `_ = state.SetMyPaneWidth(p.paneWidth)`

## Adapters

- `ctx.Adapters` holds integrations. Check capability in `Init` before using.
- Watcher data from adapters should feed messages through `Update`.

## Error Handling

- Return lightweight errors from `Init`; registry records them without crashing.
- Use `ctx.Logger` with structured fields.
- Surface recoverable issues as status/toast messages, not panics.

## New Plugin Checklist

1. Create `internal/plugins/<id>/` with `plugin.go` plus supporting files.
2. Implement the `plugin.Plugin` interface; consider `DiagnosticProvider`.
3. Register in `cmd/sidecar/main.go`.
4. Add default key bindings in `internal/keymap/bindings.go`.
5. Ensure `Commands()` covers every binding so hints/help work.
6. Wire external needs (adapters, env detection) in `Init`; degrade gracefully.
7. Provide cleanup in `Stop`; keep `Start`/`Update` non-blocking.

## Testing

- Keep business logic in testable helpers; wire Bubble Tea plumbing around it.
- Use small typed messages (`type RefreshMsg struct{}`) to keep `Update` readable.
- Enable `--debug` for verbose logs from registry and plugins.

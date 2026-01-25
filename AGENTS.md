# Agents: Mandatory: use td usage --new-session to see open work and view tasks / epics.

# Agents: Mandatory if the human does not provide an existing task for the work you've been asked to start, create a task or tasks with td first and use td to track progress.

## Build & Versioning

```bash
# Build
go build ./...

# Run tests
go test ./...

# Install with version (use semantic versioning)
go install -ldflags "-X main.Version=v0.1.0" ./cmd/sidecar

# Tag a release
```

See docs/guides/releasing-new-version.md
Version is set via ldflags at build time. Without it, sidecar shows git revision info.

## Keyboard Shortcut Parity

See docs/guides/ui-feature-guide.md

See td-331dbf19 for diff paging implementation.

## Plugin View Rendering

**Critical: Always constrain plugin output height.** The app's header/footer are always visible - plugins must not exceed their allocated height or the header will scroll off-screen.

In `View(width, height int)`:

1. Store dimensions: `p.width, p.height = width, height`
2. Calculate internal layout respecting `height` (e.g., `contentHeight := height - headerLines - footerLines`)
3. Either use `lipgloss.Height(height).Render(content)` to enforce height, or manually limit rendered lines
4. Never rely on the app to truncate - it wraps with Height() but edge cases cause rendering bugs

This bug manifests as "top bar disappears" after state transitions (commits, refreshes, mode switches).

## Footer Hints

**Do NOT render footers in plugin View().** The app renders a unified footer bar using `plugin.Commands()` and keymap bindings. Plugins should:

1. Define commands with short names in `Commands()` method
2. Never render their own footer/hint line - this creates duplicate footers

Keep command names short (1 word preferred) to prevent footer wrapping:

- "Stage" not "Stage file"
- "Diff" not "Show diff"
- "History" not "Show history"

The footer auto-truncates hints that exceed available width.

## Inter-Plugin Communication

Plugins communicate via tea.Msg broadcast - all plugins receive all messages.

**App-level messages** (`internal/app/commands.go`):

- `FocusPluginByIDMsg{PluginID}` - switch focus to a plugin by ID
- `app.FocusPlugin(id)` - helper to create the above

**File browser messages** (`internal/plugins/filebrowser/plugin.go`):

- `NavigateToFileMsg{Path}` - navigate to and preview a file (relative path)

**Usage pattern** (e.g., git → file browser):

```go
func (p *Plugin) openInFileBrowser(path string) tea.Cmd {
    return tea.Batch(
        app.FocusPlugin("file-browser"),
        func() tea.Msg { return filebrowser.NavigateToFileMsg{Path: path} },
    )
}
```

Workspace tmux preview capture cap is configurable via `plugins.workspace.tmuxCaptureMaxBytes` in `~/.config/sidecar/config.json`.

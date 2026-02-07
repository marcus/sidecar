# Worktree Switching Guide

How sidecar handles git worktree context switching and how plugins participate.

## Overview

Sidecar supports seamless switching between git worktrees within the same repository. When switching:
1. All plugins are stopped, reinitialized with the new WorkDir, and restarted
2. Per-worktree state (active plugin, sidebar selections) is saved/restored
3. Project-specific themes are applied
4. If a worktree is deleted externally, sidecar gracefully falls back to main

## Core Mechanism

### Project Switching

Worktree switching uses the same mechanism as project switching via `Model.switchProject()` in `internal/app/model.go`:

```go
m.switchProject(worktreePath)
```

This triggers:
1. Save active plugin for old WorkDir
2. Update `m.ui.WorkDir` to new path
3. Apply resolved theme for new path
4. Call `registry.Reinit(newWorkDir)` - stops all plugins, updates context, reinits all
5. Send `WindowSizeMsg` to all plugins for layout recalculation
6. Restore saved active plugin for new WorkDir
7. Show toast notification

### Registry Reinitialization

`Registry.Reinit()` in `internal/plugin/registry.go`:

```go
func (r *Registry) Reinit(newWorkDir string) []tea.Cmd {
    // Stop all plugins (reverse order)
    for i := len(r.plugins) - 1; i >= 0; i-- {
        r.safeStop(r.plugins[i])
    }
    // Update context
    r.ctx.WorkDir = newWorkDir
    // Reinit all plugins
    for _, p := range r.plugins {
        r.safeInit(p)
    }
    // Collect and return start commands
    return startCmds
}
```

## Plugin Responsibilities

### Handle Reinitialization Cleanly

Your plugin will be stopped and reinitialized on worktree switch. Ensure:

1. **`Stop()`** releases all resources (watchers, goroutines, channels)
2. **`Init(ctx)`** resets state and reads from new `ctx.WorkDir`
3. **`Start()`** kicks off fresh async work for the new context

```go
func (p *Plugin) Stop() {
    p.stopOnce.Do(func() {
        if p.watcher != nil {
            p.watcher.Close()
        }
        close(p.done)
    })
}

func (p *Plugin) Init(ctx *plugin.Context) error {
    p.ctx = ctx
    p.items = nil           // Reset state
    p.stopOnce = sync.Once{} // Reset stop guard
    p.done = make(chan struct{})
    return nil
}
```

### Handle WindowSizeMsg After Switch

After reinitialization, the app sends `tea.WindowSizeMsg` to all plugins. Ensure your `Update` handles it:

```go
case tea.WindowSizeMsg:
    p.width = msg.Width
    p.height = msg.Height
    return p, nil
```

### Persist Per-Worktree State

Use `internal/state` to save/restore preferences keyed by WorkDir:

```go
// In Init or Start, restore state
saved := state.GetMyPluginState(p.ctx.WorkDir)
if saved.Selection != "" {
    p.selection = saved.Selection
}

// On user action, save state
state.SetMyPluginState(p.ctx.WorkDir, MyPluginState{
    Selection: p.selection,
})
```

Add state struct and accessors following the pattern in `internal/state/state.go`:

```go
type MyPluginState struct {
    Selection string `json:"selection,omitempty"`
}

func GetMyPluginState(workdir string) MyPluginState {
    mu.RLock()
    defer mu.RUnlock()
    if current == nil || current.MyPlugin == nil {
        return MyPluginState{}
    }
    return current.MyPlugin[workdir]
}
```

## Deleted Worktree Detection

When a worktree is deleted externally, plugins should detect this and request fallback to main.

### Message vs Command Flow

Understanding the distinction between messages and commands is important:

- **Messages** (`tea.Msg`): Data types passed through Bubble Tea's `Update()` method
- **Commands** (`tea.Cmd`): Functions that return messages, executed asynchronously

The worktree deletion flow uses both:
1. Plugin defines a local message type (e.g., `WorkDirDeletedMsg`)
2. Plugin detects deletion in a command, returns the message
3. Plugin handles the message in `Update()`, returns an app-level command
4. App handles the command's resulting message to perform the switch

### App-Level Commands (internal/app/commands.go)

```go
// SwitchWorktreeMsg requests switching to a different worktree.
type SwitchWorktreeMsg struct {
    WorktreePath string // Absolute path to the worktree
}

// SwitchWorktree returns a command that requests switching to a worktree by path.
func SwitchWorktree(path string) tea.Cmd {
    return func() tea.Msg {
        return SwitchWorktreeMsg{WorktreePath: path}
    }
}

// SwitchToMainWorktreeMsg requests switching to the main worktree.
// Sent when the current WorkDir (a worktree) has been deleted.
type SwitchToMainWorktreeMsg struct {
    MainWorktreePath string // Path to the main worktree to switch to
}

// SwitchToMainWorktree returns a command that requests switching to the main worktree.
func SwitchToMainWorktree(mainPath string) tea.Cmd {
    return func() tea.Msg {
        return SwitchToMainWorktreeMsg{MainWorktreePath: mainPath}
    }
}
```

The app handles `SwitchToMainWorktreeMsg` by switching to main and showing a toast.

### Real-World Example: Workspace Plugin

The workspace plugin demonstrates the complete deleted worktree pattern:

**1. Define plugin-local message (`internal/plugins/workspace/worktree.go`):**
```go
// WorkDirDeletedMsg signals that the current working directory was deleted.
// This happens when sidecar is running inside a worktree that gets deleted.
type WorkDirDeletedMsg struct {
    MainWorktreePath string
}
```

**2. Detect deletion in refresh command:**
```go
func (p *Plugin) refreshWorktrees() tea.Cmd {
    workDir := p.ctx.WorkDir
    return func() tea.Msg {
        // Check if current WorkDir still exists (may have been a deleted worktree)
        if _, err := os.Stat(workDir); os.IsNotExist(err) {
            // WorkDir was deleted - find main worktree to switch to
            mainPath := findMainWorktreeFromDeleted(workDir)
            if mainPath != "" {
                return WorkDirDeletedMsg{MainWorktreePath: mainPath}
            }
        }
        // Normal refresh logic...
        return RefreshDoneMsg{Worktrees: worktrees, Err: err}
    }
}
```

**3. Handle message in Update(), return app command:**
```go
case WorkDirDeletedMsg:
    // Current working directory (a worktree) was deleted - request switch to main repo
    p.refreshing = false
    if msg.MainWorktreePath != "" {
        return p, app.SwitchToMainWorktree(msg.MainWorktreePath)
    }
    return p, nil
```

### Inter-Plugin Communication

All plugins receive all messages via `tea.Msg` broadcast. This means:
- Your plugin will see messages from other plugins
- Use type switches to filter only messages you care about
- Plugin-specific messages should be defined in that plugin's package
- App-level messages (like `SwitchWorktreeMsg`) are defined in `internal/app/commands.go`

For more details on inter-plugin communication patterns, see `CLAUDE.md`.

## Git Helpers

`internal/app/git.go` provides worktree utilities:

| Function | Purpose |
|----------|---------|
| `GetWorktrees(workDir)` | List all worktrees for the repo |
| `GetMainWorktreePath(workDir)` | Get path to main worktree |
| `WorktreeNameForPath(workDir, path)` | Derive display name for a worktree |
| `GetAllRelatedPaths(workDir)` | Get all paths sharing the same repo |

## State Persistence

The following state is persisted per WorkDir:

| Key | Purpose |
|-----|---------|
| `ActivePlugin` | Which plugin tab was focused |
| `FileBrowser` | File browser selections and view state |
| `Workspace` | Workspace/shell selections |

State is saved to `~/.config/sidecar/state.json` and keyed by absolute WorkDir path.

**Note:** State accessors in `internal/state/state.go` take `workdir` as a parameter:
```go
func GetMyPluginState(workdir string) MyPluginState
func SetMyPluginState(workdir string, state MyPluginState)
```

This means state is automatically per-worktree when you pass `p.ctx.WorkDir`. When switching worktrees, the new WorkDir naturally retrieves different state without any additional logic.

## Best Practices

1. **Reset all state in `Init()`** - don't carry over stale data from previous worktree
2. **Use `sync.Once` for `Stop()`** - prevents double-close panics during rapid switching
3. **Validate WorkDir exists** before expensive operations
4. **Store WorkDir at command creation time** - closures may execute after switch
5. **Keep `Start()` non-blocking** - return commands that do async work

## Testing Worktree Switching

1. Create a worktree: `git worktree add ../my-feature feature-branch`
2. Switch to it via project switcher or workspace plugin
3. Verify your plugin reinitializes with correct data
4. Delete the worktree externally and trigger a refresh
5. Verify graceful fallback to main repo

---
name: worktree-switching
description: >
  Git worktree support in sidecar: worktree detection, switching between worktrees,
  worktree state management, and plugin reinitialization. Covers the full lifecycle
  of worktree context switching including registry reinit, per-worktree state
  persistence, deleted worktree detection and fallback. Use when working on git
  worktree features or worktree-related functionality.
---

# Worktree Switching

Sidecar supports seamless switching between git worktrees. When switching:
1. All plugins are stopped, reinitialized with the new WorkDir, and restarted
2. Per-worktree state (active plugin, sidebar selections) is saved/restored
3. Project-specific themes are applied
4. If a worktree is deleted externally, sidecar gracefully falls back to main

## Core Mechanism

### Project Switching

Worktree switching uses `Model.switchProject()` in `internal/app/model.go`:

```go
m.switchProject(worktreePath)
```

This triggers in order:
1. Save active plugin for old WorkDir
2. Update `m.ui.WorkDir` to new path
3. Apply resolved theme for new path
4. Call `registry.Reinit(newWorkDir)` -- stops all plugins, updates context, reinits all
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

## Plugin Responsibilities on Worktree Switch

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
    p.items = nil            // Reset state
    p.stopOnce = sync.Once{} // Reset stop guard
    p.done = make(chan struct{})
    return nil
}
```

### Handle WindowSizeMsg After Switch

After reinitialization, the app sends `tea.WindowSizeMsg`. Handle it in `Update`:

```go
case tea.WindowSizeMsg:
    p.width = msg.Width
    p.height = msg.Height
    return p, nil
```

### Persist Per-Worktree State

Use `internal/state` to save/restore preferences keyed by WorkDir:

```go
// Restore state in Init or Start
saved := state.GetMyPluginState(p.ctx.WorkDir)
if saved.Selection != "" {
    p.selection = saved.Selection
}

// Save state on user action
state.SetMyPluginState(p.ctx.WorkDir, MyPluginState{
    Selection: p.selection,
})
```

Add state struct and accessors following `internal/state/state.go`:

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

State is saved to `~/.config/sidecar/state.json` keyed by absolute WorkDir path. State is automatically per-worktree when you pass `p.ctx.WorkDir`.

## Deleted Worktree Detection

When a worktree is deleted externally, plugins should detect this and request fallback to main.

### App-Level Commands

Defined in `internal/app/commands.go`:

- `SwitchWorktreeMsg{WorktreePath}` -- requests switching to a specific worktree
- `SwitchWorktree(path) tea.Cmd` -- helper to create the above
- `SwitchToMainWorktreeMsg{MainWorktreePath}` -- requests fallback to main worktree
- `SwitchToMainWorktree(mainPath) tea.Cmd` -- helper to create the above

### Detection Pattern (from workspace plugin)

**1. Define plugin-local message** (`internal/plugins/workspace/worktree.go`):
```go
type WorkDirDeletedMsg struct {
    MainWorktreePath string
}
```

**2. Detect deletion in refresh command:**
```go
func (p *Plugin) refreshWorktrees() tea.Cmd {
    workDir := p.ctx.WorkDir
    return func() tea.Msg {
        if _, err := os.Stat(workDir); os.IsNotExist(err) {
            mainPath := findMainWorktreeFromDeleted(workDir)
            if mainPath != "" {
                return WorkDirDeletedMsg{MainWorktreePath: mainPath}
            }
        }
        return RefreshDoneMsg{Worktrees: worktrees, Err: err}
    }
}
```

**3. Handle message, return app command:**
```go
case WorkDirDeletedMsg:
    p.refreshing = false
    if msg.MainWorktreePath != "" {
        return p, app.SwitchToMainWorktree(msg.MainWorktreePath)
    }
    return p, nil
```

## Git Helpers

`internal/app/git.go` provides:

| Function | Purpose |
|----------|---------|
| `GetWorktrees(workDir)` | List all worktrees for the repo |
| `GetMainWorktreePath(workDir)` | Get path to main worktree |
| `WorktreeNameForPath(workDir, path)` | Derive display name for a worktree |
| `GetAllRelatedPaths(workDir)` | Get all paths sharing the same repo |

## Per-WorkDir State Keys

| Key | Purpose |
|-----|---------|
| `ActivePlugin` | Which plugin tab was focused |
| `FileBrowser` | File browser selections and view state |
| `Workspace` | Workspace/shell selections |

## Post-Create Hooks

Hooks run shell commands after a worktree is created. Users select which hooks to run via checkboxes in the create worktree modal.

### Config Format

Hooks are defined in `config.json` under `hooks.post-create`:

```json
{
  "hooks": {
    "post-create": [
      {"name": "Install deps", "command": "npm ci"},
      {"name": "Copy env", "command": "cp .env.example .env.local"}
    ]
  }
}
```

**Config locations** (same as prompts):
- Global: `~/.config/sidecar/config.json`
- Project: `.sidecar/config.json` (overrides global by name)

### Implementation

**Hook type** (`internal/plugins/workspace/hooks.go`):
```go
type Hook struct {
    Name    string `json:"name"`
    Command string `json:"command"`
    Source  string `json:"-"` // "global" or "project"
}
```

**Loading**: `LoadHooks(globalConfigDir, projectDir)` merges global + project hooks, project overrides by name, returns sorted by name. Follows the `LoadPrompts()` pattern exactly.

**Modal integration**: Hooks appear as inline checkboxes in the create modal between Prompt and Task sections. Each shows `[x]/[ ]`, name, scope `[G]/[P]`, and dimmed command preview.

**Focus index**: `createFocus` 3 = hooks (0=name, 1=base, 2=prompt, 3=hooks, 4=task, 5=agent, 6=skipPerms, 7=create, 8=cancel).

**Execution**: `runPostCreateHooks()` runs selected hooks sequentially in the new worktree directory after `setupWorktree()` completes. Environment variables:
- `MAIN_WORKTREE` -- path to main repo
- `WORKTREE_BRANCH` -- new branch name
- `WORKTREE_PATH` -- new worktree directory

**Result handling**: `HookResultMsg` carries results. Failures show a toast; successes log silently. Hook errors do not block worktree use or agent start.

### Key Files

| File | Purpose |
|------|---------|
| `internal/plugins/workspace/hooks.go` | Hook type, LoadHooks, runPostCreateHooks, HookResultMsg |
| `internal/plugins/workspace/create_modal.go` | createHookSection, focus routing |
| `internal/plugins/workspace/keys.go` | Hook toggle/navigation in create modal |
| `internal/plugins/workspace/mouse.go` | Click-to-toggle hook checkboxes |

## Best Practices

1. **Reset all state in `Init()`** -- do not carry over stale data from previous worktree
2. **Use `sync.Once` for `Stop()`** -- prevents double-close panics during rapid switching
3. **Validate WorkDir exists** before expensive operations
4. **Store WorkDir at command creation time** -- closures may execute after switch
5. **Keep `Start()` non-blocking** -- return commands that do async work

## Testing Worktree Switching

1. Create a worktree: `git worktree add ../my-feature feature-branch`
2. Switch to it via project switcher or workspace plugin
3. Verify your plugin reinitializes with correct data
4. Delete the worktree externally and trigger a refresh
5. Verify graceful fallback to main repo

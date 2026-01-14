# Sidecar Keyboard Shortcuts Guide

How to implement keyboard shortcuts for plugins.

## Quick Start: Adding a New Shortcut

**Three things must match for a shortcut to work:**

1. **Command ID** in `Commands()` → e.g., `"stage-file"`
2. **Binding command** in `bindings.go` → e.g., `Command: "stage-file"`
3. **Context** in both places → e.g., `"git-status"`

```go
// 1. In your plugin's Commands() method:
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        {ID: "stage-file", Name: "Stage", Context: "git-status"},
    }
}

// 2. In your plugin's FocusContext() method:
func (p *Plugin) FocusContext() string {
    return "git-status"  // Must match the Context above
}

// 3. In internal/keymap/bindings.go:
{Key: "s", Command: "stage-file", Context: "git-status"},
```

## Architecture: Bindings vs Handlers

The keymap system has two distinct purposes:

### 1. Footer Hints (Display Only)
Most bindings exist to show hints in the footer bar. These bindings:
- Map a key to a command ID
- Have a matching `Command` in the plugin's `Commands()` method
- **Do NOT have a registered handler** - the key falls through to the plugin's `Update()` method

### 2. App-Level Handlers (Intercepted Keys)
Some commands have registered handlers that intercept keys before they reach plugins:
- `quit` (ctrl+c) - Shows quit confirmation
- `next-plugin` / `prev-plugin` (` / ~) - Plugin cycling
- `toggle-palette` (?) - Command palette
- `toggle-diagnostics` (!) - Diagnostics overlay
- `refresh` (r) - Global refresh

### Key Routing Flow

```
User presses key
       │
       ▼
┌─────────────────────────────────────────────┐
│ App handleKeyMsg()                          │
│  1. Check quit confirm modal                │
│  2. Check text input contexts (git-commit)  │
│  3. Check 'q' in root contexts → quit       │
│  4. Check app-level shortcuts (`, ~, ?, !)  │
│  5. keymap.Handle() - registered handlers   │
└─────────────────────────────────────────────┘
       │ If not handled
       ▼
┌─────────────────────────────────────────────┐
│ Plugin Update()                             │
│  - Receives tea.KeyMsg                      │
│  - Routes to view-specific handler          │
│  - Handles the key directly (j/k/G/etc)     │
└─────────────────────────────────────────────┘
```

### Why This Design?

1. **Separation of concerns**: App handles global behavior, plugins handle domain logic
2. **Footer consistency**: bindings.go is the single source of truth for what keys do
3. **Flexibility**: Plugins can handle keys directly without registering handlers

## Adding Shortcuts to the Footer Bar

The footer bar automatically shows shortcuts for the current context. To add your shortcut:

### Step 1: Add to Commands()

```go
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        // Context MUST match what FocusContext() returns
        {ID: "stage-file", Name: "Stage", Context: "git-status"},
        {ID: "show-diff", Name: "Diff", Context: "git-status"},

        // Different context for different view
        {ID: "close-diff", Name: "Close", Context: "git-diff"},
    }
}
```

### Step 2: Add to bindings.go

```go
// The Key here shows in the footer as the shortcut
{Key: "s", Command: "stage-file", Context: "git-status"},
{Key: "d", Command: "show-diff", Context: "git-status"},
{Key: "esc", Command: "close-diff", Context: "git-diff"},
```

### Step 3: Handle in Update()

```go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "s":
            return p.stageFile()
        case "d":
            return p.showDiff()
        }
    }
    return p, nil
}
```

### Footer Priority System

The footer shows **plugin-specific hints first**, then global hints (help, quit). Within plugin hints, commands are sorted by `Priority` field (lower = shown first).

```go
type Command struct {
    ID          string
    Name        string
    Description string
    Category    Category
    Context     string
    Priority    int  // 1=highest priority, 0=default (treated as 99)
}
```

**Priority Guidelines:**
- **Priority 1**: Primary actions (Stage, Commit, View, Open)
- **Priority 2**: Common secondary actions (Diff, Search, Push)
- **Priority 3**: Tertiary actions (History, Refresh)
- **Priority 4+**: Rarely used actions (Browse, external integrations)

```go
// Example: git-status context priorities
{ID: "stage-file", Name: "Stage", Context: "git-status", Priority: 1},
{ID: "commit", Name: "Commit", Context: "git-status", Priority: 1},
{ID: "show-diff", Name: "Diff", Context: "git-status", Priority: 2},
{ID: "show-history", Name: "History", Context: "git-status", Priority: 3},
{ID: "open-in-file-browser", Name: "Browse", Context: "git-status", Priority: 4},
```

### Footer Width Considerations

The footer auto-truncates hints that exceed available width. To maximize visibility:
- Keep command names short: "Stage" not "Stage file"
- Set appropriate `Priority` values (important commands survive truncation)
- Test at different terminal widths

### Global Footer Hints

Global hints (shown after plugin hints) are defined in `internal/app/view.go` in `globalFooterHints()`:

```go
func (m Model) globalFooterHints() []footerHint {
    bindings := m.keymap.BindingsForContext("global")
    keysByCmd := bindingKeysByCommand(bindings)

    specs := []struct {
        id    string
        label string
    }{
        {id: "toggle-palette", label: "help"},
        {id: "quit", label: "quit"},
    }

    var hints []footerHint

    // Custom consolidated hints (not tied to single binding)
    hints = append(hints, footerHint{keys: "1-5", label: "plugins"})

    // Binding-based hints
    for _, spec := range specs {
        keys := keysByCmd[spec.id]
        if len(keys) == 0 {
            continue
        }
        hints = append(hints, footerHint{keys: keys[0], label: spec.label})
    }
    return hints
}
```

**Two ways to add global hints:**

1. **Binding-based**: Add to `specs` slice with the command ID from `bindings.go`. The key is looked up automatically.

2. **Custom/consolidated**: Directly append a `footerHint{keys: "...", label: "..."}`. Use this for:
   - Combining multiple keys (e.g., `"1-4"` for focus-plugin-1 through focus-plugin-4)
   - Showing key ranges or alternatives (e.g., `"j/k"` for navigation)

### Footer Rendering Flow

```
footerHints()
    │
    ├── pluginFooterHints(activePlugin, context)
    │   └── Returns hints from plugin.Commands() matching current context
    │       sorted by Priority (lower = first)
    │
    └── globalFooterHints()
        └── Returns app-level hints (plugins, help, quit)

renderHintLineTruncated(hints, availableWidth)
    └── Renders hints left-to-right until width exceeded
        (plugin hints shown first, then global)
```

## FocusContext Reference

Each plugin returns a context string that determines which bindings are active.

### Global (App-Level)
| Context | Description |
|---------|-------------|
| `global` | Default when no plugin-specific context |
| `""` | Empty string treated as global |

### Unified Sidebar Controls

All plugins with two-pane layouts (Git, Conversations, Files) share consistent sidebar shortcuts:

| Key | Action | Description |
|-----|--------|-------------|
| `Tab` | Switch focus | Move focus between sidebar and main pane |
| `\` | Toggle sidebar | Collapse/expand the sidebar pane |
| `h`/`left` | Focus left | Move focus to sidebar pane |
| `l`/`right` | Focus right | Move focus to main pane |

**Behavior notes:**
- `Tab` only switches focus between panes (doesn't toggle visibility)
- `\` collapses sidebar to give main pane full width, or restores it
- When sidebar is collapsed, focus automatically moves to main pane
- When sidebar is restored with `\`, focus moves to sidebar

### Conversations Plugin
| Context | View | Description |
|---------|------|-------------|
| `conversations` | Session list (single-pane) | Root view, 'q' quits |
| `conversations-sidebar` | Session list (two-pane) | Left pane focused, 'q' quits |
| `conversations-main` | Messages (two-pane) | Right pane focused |
| `conversations-search` | Search mode | Text input active |
| `conversations-filter` | Filter mode | Adapter filter active |
| `conversation-detail` | Turn list | Viewing turns for a session |
| `message-detail` | Single turn | Viewing one turn's content |
| `analytics` | Analytics view | Usage stats |

### Git Status Plugin
| Context | View | Description |
|---------|------|-------------|
| `git-status` | File list | Root view, 'q' quits |
| `git-status-commits` | Recent commits | Commit list in sidebar |
| `git-status-diff` | Inline diff | Diff pane focused |
| `git-commit-preview` | Commit preview | Commit detail in right pane |
| `git-diff` | Full diff | Full-screen diff view |
| `git-commit` | Commit editor | Text input active |
| `git-history` | History view | Commit history |
| `git-commit-detail` | Commit detail | Single commit view |

#### Git Status Commit List Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `enter` | view-commit | Open commit details |
| `d` | view-commit | Open commit details |
| `h` | show-history | Open history view |
| `y` | yank-commit | Copy commit as markdown |
| `Y` | yank-id | Copy commit hash |
| `/` | search-history | Search commit messages |
| `f` | filter-author | Filter by author |
| `p` | filter-path | Filter by path |
| `F` | clear-filter | Clear history filters |
| `n` | next-match | Next search match |
| `N` | prev-match | Previous search match |
| `v` | toggle-graph | Toggle commit graph (tree view) |
| `\` | toggle-sidebar | Collapse/expand sidebar |

### File Browser Plugin
| Context | View | Description |
|---------|------|-------------|
| `file-browser-tree` | Tree view | Root view, 'q' quits |
| `file-browser-preview` | Preview pane | File preview focused |
| `file-browser-search` | Search mode | Filename search |
| `file-browser-content-search` | Content search | File content search |
| `file-browser-quick-open` | Quick open | Fuzzy file finder |
| `file-browser-project-search` | Project search | Ripgrep search modal |
| `file-browser-file-op` | File operation | Create/rename/move/delete input |

#### File Browser Tree Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `/` | search | Filter files by name |
| `ctrl+p` | quick-open | Fuzzy file finder |
| `ctrl+s` | project-search | Project-wide search (ripgrep) |
| `a` | create-file | Create new file |
| `A` | create-dir | Create new directory |
| `d` | delete | Delete file/directory (with confirmation) |
| `y` | yank | Copy file/directory to clipboard |
| `p` | paste | Paste from clipboard |
| `s` | sort | Cycle sort mode (name/size/time/type) |
| `r` | rename | Rename file/directory |
| `m` | move | Move file/directory |
| `R` | reveal | Reveal in system file manager |
| `\` | toggle-sidebar | Collapse/expand tree pane |

### Worktrees Plugin
| Context | View | Description |
|---------|------|-------------|
| `worktree-list` | Worktree list | Root view, 'q' quits |
| `worktree-preview` | Preview pane | Preview pane focused |
| `worktree-create` | Create form | Create worktree input |
| `worktree-task-link` | Task linking | Task selection modal |
| `worktree-merge` | Merge workflow | Merge workflow modal |

#### Worktree List Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `n` | new-worktree | Create new worktree |
| `v` | toggle-view | Toggle list/kanban view |
| `r` | refresh | Refresh worktree list |
| `D` | delete-worktree | Delete selected worktree |
| `p` | push | Push branch to remote |
| `m` | merge-workflow | Start merge workflow |
| `t` | link-task | Link/unlink task |
| `s` | start-agent | Start agent |
| `enter` | attach | Attach to agent session |
| `S` | stop-agent | Stop agent |
| `y` | approve | Approve agent prompt |
| `N` | reject | Reject agent prompt |
| `Tab` | switch-pane | Switch focus between sidebar and preview |
| `\` | toggle-sidebar | Collapse/expand sidebar |
| `[` | prev-tab | Previous preview tab (Output/Diff/Task) |
| `]` | next-tab | Next preview tab (Output/Diff/Task) |

### TD Monitor Plugin

**Note:** TD shortcuts are dynamically exported from TD itself. The TD plugin consumes
`ExportBindings()` and `ExportCommands()` from TD's keymap package, making TD the single
source of truth. To add new TD shortcuts, modify TD's `pkg/monitor/keymap/bindings.go`.

| Context | View | Description |
|---------|------|-------------|
| `td-monitor` | Issue list | Root view, 'q' quits |
| `td-modal` | Issue detail modal | Issue details open |
| `td-stats` | Statistics modal | Stats dashboard |
| `td-search` | Search mode | Search input active |
| `td-confirm` | Confirm dialog | Confirmation prompt |
| `td-epic-tasks` | Epic task list | Task section focused in epic modal |
| `td-parent-epic` | Parent epic focused | Parent epic row focused |
| `td-handoffs` | Handoffs modal | Handoffs list open |
| `td-global` | Global TD context | TD global shortcuts |

## Root Contexts ('q' to Quit)

In root contexts, pressing 'q' shows the quit confirmation. In non-root contexts, 'q' navigates back.

**Root contexts** (q = quit):
- `global`, `""`
- `conversations`, `conversations-sidebar`
- `git-status`, `git-status-commits`, `git-status-diff`
- `file-browser-tree`
- `worktree-list`
- `td-monitor`

**Non-root contexts** (q = back):
- `conversation-detail`, `message-detail`, `analytics`
- `git-diff`, `git-commit`, `git-history`, etc.
- `file-browser-preview`, etc.
- `worktree-create`, `worktree-task-link`, `worktree-merge`, `worktree-preview`
- `td-modal`, `td-stats`, `td-search`, `td-confirm`, `td-epic-tasks`, `td-parent-epic`, `td-handoffs`

## Complete Example: Adding "edit" to File Browser

### Step 1: Add to Commands()

```go
// internal/plugins/filebrowser/plugin.go
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        {ID: "refresh", Name: "Refresh", Context: "file-browser-tree"},
        {ID: "expand", Name: "Open", Context: "file-browser-tree"},
        {ID: "edit", Name: "Edit", Context: "file-browser-tree"},  // NEW
    }
}
```

### Step 2: Add to bindings.go

```go
// internal/keymap/bindings.go
{Key: "e", Command: "edit", Context: "file-browser-tree"},
```

### Step 3: Handle in Update()

```go
// internal/plugins/filebrowser/plugin.go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "e" {
            return p, p.openInEditor()
        }
    }
    return p, nil
}
```

## Multiple Contexts (View Modes)

When your plugin has different modes, use different contexts:

```go
func (p *Plugin) FocusContext() string {
    switch p.viewMode {
    case ViewDiff:
        return "git-diff"      // Different bindings active
    case ViewCommit:
        return "git-commit"    // Different bindings active
    default:
        return "git-status"    // Default bindings
    }
}

func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        // Main view commands
        {ID: "stage-file", Name: "Stage", Context: "git-status"},
        {ID: "show-diff", Name: "Diff", Context: "git-status"},

        // Diff view commands
        {ID: "close-diff", Name: "Close", Context: "git-diff"},
        {ID: "scroll", Name: "Scroll", Context: "git-diff"},

        // Commit view commands
        {ID: "cancel", Name: "Cancel", Context: "git-commit"},
        {ID: "execute-commit", Name: "Commit", Context: "git-commit"},
    }
}
```

## Core Files

| File | Purpose |
|------|---------|
| `internal/plugin/plugin.go` | `Command` struct (ID, Name, Context, Priority), `Commands()`, `FocusContext()` interface |
| `internal/keymap/bindings.go` | Default key→command mappings |
| `internal/keymap/registry.go` | Runtime binding lookup, handler registration |
| `internal/app/update.go` | Key routing, `isRootContext()` |
| `internal/app/view.go` | Footer rendering, priority sorting |
| `internal/palette/palette.go` | Command palette model, context toggle |
| `internal/palette/view.go` | Palette rendering, virtual scrolling |
| `internal/palette/entries.go` | Entry building, context filtering, command grouping |

## Common Mistakes

| Symptom | Cause | Fix |
|---------|-------|-----|
| Shortcut doesn't work | Command ID mismatch | Ensure ID in `Commands()` matches `Command` in `bindings.go` |
| Shortcut doesn't work | Context mismatch | Ensure `FocusContext()` returns same context as binding |
| Double footer | Plugin renders own footer | Remove footer rendering from plugin's `View()` |
| Wrong hints shown | `FocusContext()` not updated | Return correct context for current view mode |
| Footer too long | Command names too verbose | Use 1-word names: "Stage" not "Stage file" |
| Important hint truncated | Priority too high (or 0) | Set lower Priority value (1=highest importance) |
| 'q' quits unexpectedly | Context is root | Add context to non-root list in `isRootContext()` |
| 'q' doesn't quit | Context not root | Add context to root list in `isRootContext()` |

## Checklist for New Shortcuts

- [ ] Command added to `Commands()` with ID, Name, Context, Priority
- [ ] `FocusContext()` returns matching context for current view
- [ ] Binding added to `bindings.go` with Key, Command, Context
- [ ] Key handled in `Update()` via `tea.KeyMsg`
- [ ] No duplicate/conflicting keys in same context
- [ ] Command name is short (1-2 words max)
- [ ] Priority set appropriately (1=primary, 2=secondary, 3+=tertiary)
- [ ] Plugin does NOT render its own footer
- [ ] 'q' behavior is correct (check `isRootContext()`)

## Key Format Reference

```go
// Letters (lowercase)
{Key: "j", Command: "cursor-down", Context: "global"}

// Shifted letters (uppercase)
{Key: "G", Command: "cursor-bottom", Context: "global"}

// Control combos
{Key: "ctrl+d", Command: "page-down", Context: "global"}
{Key: "ctrl+c", Command: "quit", Context: "global"}

// Alt combos
{Key: "alt+enter", Command: "execute-commit", Context: "git-commit"}

// Special keys
{Key: "enter", Command: "select", Context: "global"}
{Key: "esc", Command: "back", Context: "global"}
{Key: "`", Command: "next-plugin", Context: "global"}
{Key: "~", Command: "prev-plugin", Context: "global"}
{Key: "up", Command: "cursor-up", Context: "global"}
{Key: "down", Command: "cursor-down", Context: "global"}
{Key: "\\", Command: "toggle-sidebar", Context: "git-status"}

// Sequences (space-separated, 500ms timeout)
{Key: "g g", Command: "cursor-top", Context: "global"}
```

## Context Precedence

1. Plugin-specific context checked first (e.g., `git-status`)
2. Falls back to `global` context if no match

This means `c` in `git-status` context triggers `commit`, but `c` in `global` context triggers `focus-conversations`.

## Command Palette (?)

The command palette shows all available shortcuts. Press `?` to open.

### Navigation
- `j`/`k` or `↑`/`↓` - Move cursor
- `ctrl+d`/`ctrl+u` - Page down/up
- `enter` - Execute selected command
- `esc` - Close palette

### Context Toggle
Press `tab` to toggle between two modes:

1. **Current Context** (default): Shows only shortcuts for active context + global
   - No duplicates - clean, focused view
   - Example: In git-status, shows Stage/Commit/Diff but not file browser commands

2. **All Contexts**: Shows all shortcuts grouped by command
   - Commands in multiple contexts show "(N contexts)" indicator
   - Useful for discovering all available shortcuts

### Virtual Scrolling
The palette uses virtual scrolling - only visible entries are rendered. Scroll indicators appear when content extends beyond the viewport:
- `↑ N more above` - Content scrolled above
- `↓ N more below` - Content scrolled below

## Testing

1. Run `sidecar --debug` to see key handling logs
2. Press `?` to verify command palette shows your bindings
3. Press `tab` in palette to toggle context modes
4. Check footer shows your command names (plugin hints first)
5. Test that keys trigger correct actions
6. Test context switches (enter subview, verify new bindings active)
7. Test 'q' behavior in each context (quit vs back)

## TD Monitor: Dynamic Shortcut Integration

The TD Monitor plugin uses a **dynamic export pattern** instead of hardcoded bindings.
TD itself is the single source of truth for shortcuts.

### How It Works

1. **TD exports metadata** via `pkg/monitor/keymap/export.go`:
   - `ExportBindings()` - Returns all key→command mappings
   - `ExportCommands()` - Returns command metadata (name, description, priority)
   - `CurrentContextString()` - Returns current context for sidecar

2. **Sidecar consumes exports** in `internal/plugins/tdmonitor/plugin.go`:
   - `Init()` - Registers TD bindings with sidecar's keymap
   - `Commands()` - Returns TD's exported command metadata
   - `FocusContext()` - Delegates to TD's context tracking

3. **Context mapping** from TD to sidecar:
   | TD Context | Sidecar Context |
   |------------|-----------------|
   | main | td-monitor |
   | modal | td-modal |
   | stats | td-stats |
   | search | td-search |
   | confirm | td-confirm |
   | epic-tasks | td-epic-tasks |
   | parent-epic-focused | td-parent-epic |
   | handoffs | td-handoffs |
   | global | td-global |

### Adding a New TD Shortcut

**Step 1:** Add binding to TD's `pkg/monitor/keymap/bindings.go`:
```go
{Key: "n", Command: CmdNewIssue, Context: ContextMain, Description: "Create new issue"},
```

**Step 2:** Add command constant to TD's `pkg/monitor/keymap/registry.go`:
```go
CmdNewIssue Command = "new-issue"
```

**Step 3:** Add metadata to TD's `pkg/monitor/keymap/export.go`:
```go
CmdNewIssue: {"New", "Create new issue", 2},
```

**Step 4:** Handle the command in TD's `pkg/monitor/model.go`:
```go
case keymap.CmdNewIssue:
    return m.createNewIssue()
```

The shortcut automatically appears in sidecar's footer and command palette.

### Priority Levels

Priority determines footer visibility:
- **1-3**: Shown in footer (space permitting)
- **4+**: Palette only

```go
// High priority - always visible
CmdOpenDetails: {"Details", "Open issue details", 1},

// Medium priority - shown when space allows
CmdOpenStats: {"Stats", "Open statistics", 2},

// Low priority - palette only
CmdCursorDown: {"Down", "Move cursor down", 5},
```

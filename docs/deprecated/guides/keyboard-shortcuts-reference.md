# Keyboard Shortcuts Reference

Complete shortcut listings and context reference. For implementation patterns, see `docs/deprecated/guides/ui-feature-guide.md`.

## Global Shortcuts

| Key | Command | Description |
|-----|---------|-------------|
| `j` / `down` | cursor-down | Move cursor down |
| `k` / `up` | cursor-up | Move cursor up |
| `G` | cursor-bottom | Jump to bottom |
| `g g` | cursor-top | Jump to top |
| `ctrl+d` | page-down | Page down |
| `ctrl+u` | page-up | Page up |
| `enter` | select | Select item |
| `esc` | back | Go back / close |
| `` ` `` | next-plugin | Next plugin |
| `~` | prev-plugin | Previous plugin |
| `1-5` | focus-plugin-N | Focus plugin by number |
| `?` | toggle-palette | Command palette |
| `!` | toggle-diagnostics | Diagnostics overlay |
| `@` | switch-project | Project switcher |
| `r` | refresh | Refresh |
| `q` | quit | Quit (root contexts only) |
| `ctrl+c` | quit | Force quit |

## Sidebar Controls (All Two-Pane Plugins)

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch focus between panes |
| `\` | Toggle sidebar visibility |
| `h` / `left` | Focus left pane |
| `l` / `right` | Focus right pane |

## Git Status Plugin

### Contexts
| Context | View |
|---------|------|
| `git-status` | File list (root) |
| `git-status-commits` | Recent commits sidebar (root) |
| `git-status-diff` | Inline diff pane (root) |
| `git-commit-preview` | Commit detail in right pane |
| `git-diff` | Full-screen diff |
| `git-commit` | Commit editor |
| `git-push-menu` | Push strategy selection |
| `git-pull-menu` | Pull strategy selection |
| `git-pull-conflict` | Conflict resolution |
| `git-history` | Commit history |
| `git-commit-detail` | Single commit view |

### File List Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `s` | stage-file | Stage selected file |
| `u` | unstage-file | Unstage selected file |
| `S` | stage-all | Stage all modified |
| `U` | unstage-all | Unstage all |
| `c` | commit | Open commit editor |
| `A` | amend | Amend last commit |
| `d` / `enter` | show-diff | View file changes |
| `D` | discard-changes | Discard unstaged changes |
| `h` | show-history | Open commit history |
| `P` | push | Open push menu |
| `L` | pull | Open pull menu |
| `f` | fetch | Fetch from remote |
| `b` | branch | Branch operations |
| `z` | stash | Stash changes |
| `Z` | stash-pop | Pop stash |
| `o` | open-in-github | Open in GitHub |
| `O` | open-in-file-browser | Open in file browser |
| `y` | yank-file | Copy file info |
| `Y` | yank-path | Copy file path |

### Commit List Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `enter` / `d` | view-commit | Open commit details |
| `h` | show-history | Open history view |
| `y` | yank-commit | Copy commit as markdown |
| `Y` | yank-id | Copy commit hash |
| `/` | search-history | Search commit messages |
| `f` | filter-author | Filter by author |
| `p` | filter-path | Filter by path |
| `F` | clear-filter | Clear filters |
| `n` | next-match | Next search match |
| `N` | prev-match | Previous match |
| `o` | open-in-github | Open commit in GitHub |
| `v` | toggle-graph | Toggle commit graph |

### Pull Menu
| Key | Command |
|-----|---------|
| `p` | pull-merge |
| `r` | pull-rebase |
| `f` | pull-ff-only |
| `a` | pull-autostash |

## File Browser Plugin

### Contexts
| Context | View |
|---------|------|
| `file-browser-tree` | Tree view (root) |
| `file-browser-preview` | Preview pane |
| `file-browser-search` | Filename search |
| `file-browser-content-search` | Content search |
| `file-browser-quick-open` | Fuzzy file finder |
| `file-browser-project-search` | Ripgrep search modal |
| `file-browser-file-op` | File operation input |
| `file-browser-inline-edit` | Inline vim editor (all keys forwarded, global shortcuts bypassed) |

### Tree Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `/` | search | Filter files by name |
| `ctrl+p` | quick-open | Fuzzy file finder |
| `ctrl+s` | project-search | Project-wide search |
| `a` | create-file | Create new file |
| `A` | create-dir | Create new directory |
| `d` | delete | Delete (with confirmation) |
| `t` | new-tab | Open in new tab |
| `[` | prev-tab | Previous tab |
| `]` | next-tab | Next tab |
| `x` | close-tab | Close active tab |
| `y` | yank | Copy to clipboard |
| `p` | paste | Paste from clipboard |
| `s` | sort | Cycle sort mode |
| `m` | move | Move file/directory |
| `R` | rename | Rename |
| `ctrl+r` | reveal | Reveal in file manager |

## Conversations Plugin

### Contexts
| Context | View |
|---------|------|
| `conversations` | Session list single-pane (root) |
| `conversations-sidebar` | Session list two-pane (root) |
| `conversations-main` | Messages pane |
| `conversations-search` | Search mode |
| `conversations-filter` | Adapter filter |
| `conversation-detail` | Turn list |
| `message-detail` | Single turn content |
| `analytics` | Usage stats |

## Workspaces Plugin

### Contexts
| Context | View |
|---------|------|
| `workspace-list` | Workspace list (root) |
| `workspace-preview` | Preview pane |
| `workspace-create` | Create worktree input |
| `workspace-task-link` | Task selection modal |
| `workspace-merge` | Merge workflow modal |
| `workspace-interactive` | Embedded terminal |

### List Shortcuts
| Key | Command | Description |
|-----|---------|-------------|
| `n` | new-workspace | Create new workspace |
| `v` | toggle-view | Toggle list/kanban |
| `D` | delete-workspace | Delete workspace |
| `p` | push | Push branch |
| `m` | merge-workflow | Start merge workflow |
| `T` | link-task | Link/unlink task |
| `s` | start-agent | Start agent |
| `enter` | interactive | Enter interactive mode |
| `t` | attach | Attach to tmux |
| `S` | stop-agent | Stop agent |
| `y` | approve | Approve agent prompt |
| `N` | reject | Reject agent prompt |
| `[` | prev-tab | Previous preview tab |
| `]` | next-tab | Next preview tab |

### Interactive Mode
| Key | Command |
|-----|---------|
| `ctrl+\` | exit |
| `ctrl+]` | attach |
| `alt+c` | copy |
| `alt+v` | paste |

## TD Monitor Plugin

TD shortcuts are dynamically exported from TD itself via `ExportBindings()` and `ExportCommands()` in `pkg/monitor/keymap/`. TD is the single source of truth.

### Contexts
| Context | View |
|---------|------|
| `td-monitor` | Issue list (root) |
| `td-modal` | Issue detail modal |
| `td-stats` | Statistics modal |
| `td-search` | Search input |
| `td-confirm` | Confirmation prompt |
| `td-epic-tasks` | Epic task list |
| `td-parent-epic` | Parent epic focused |
| `td-handoffs` | Handoffs modal |

### Adding TD Shortcuts

1. Add binding to TD's `pkg/monitor/keymap/bindings.go`
2. Add command constant to TD's `pkg/monitor/keymap/registry.go`
3. Add metadata to TD's `pkg/monitor/keymap/export.go`
4. Handle in TD's `pkg/monitor/model.go`

Shortcuts automatically appear in sidecar's footer and palette.

## Project Switcher

| Key | Command |
|-----|---------|
| `@` | toggle |
| `↓` / `ctrl+n` | cursor-down |
| `↑` / `ctrl+p` | cursor-up |
| `Enter` | select |
| `Esc` | close |

## Command Palette

Press `?` to open. Press `tab` to toggle between current-context and all-contexts view.

| Key | Action |
|-----|--------|
| `j` / `k` / `↑` / `↓` | Navigate |
| `ctrl+d` / `ctrl+u` | Page down/up |
| `enter` | Execute |
| `esc` | Close |
| `tab` | Toggle context filter |

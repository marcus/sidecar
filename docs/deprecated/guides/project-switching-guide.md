# Project Switching Guide

Switch between git repositories without restarting sidecar.

## Project vs Worktree Switching

Sidecar supports two types of switching:

- **Project Switching** (`@`): Switch between configured projects from `config.json` (arbitrary repos)
- **Worktree Switching** (`W`): Switch between git worktrees within the current repository

Project switching requires manual configuration but supports any directory. Worktree switching auto-discovers worktrees from the current git repo.

## Quick Start

1. Add projects to `~/.config/sidecar/config.json`:

```json
{
  "projects": {
    "list": [
      {"name": "sidecar", "path": "~/code/sidecar"},
      {"name": "td", "path": "~/code/td"},
      {"name": "my-app", "path": "/Users/me/projects/my-app"}
    ]
  }
}
```

2. Press `@` to open the project switcher
3. Select a project with arrow keys (or `ctrl+n/ctrl+p`)
4. Press `Enter` to switch

## Configuration

### Config Location

`~/.config/sidecar/config.json`

### Project Config Structure

```json
{
  "projects": {
    "list": [
      {
        "name": "display-name",
        "path": "/absolute/path/to/repo"
      }
    ]
  }
}
```

### Path Expansion

Paths support `~` expansion:
- `~/code/myapp` expands to `/Users/you/code/myapp`

## Keyboard Shortcuts

### Opening the Switcher

| Key | Action |
|-----|--------|
| `@` | Open/close project switcher |
| `W` | Open worktree switcher (for git worktrees) |

### Navigation

| Key | Action |
|-----|--------|
| `↓` / `ctrl+n` | Move cursor down |
| `↑` / `ctrl+p` | Move cursor up |
| `Enter` | Switch to selected project |
| `Esc` | Close without switching |

Typing in the filter box always updates the filter. Use arrows or `ctrl+n/ctrl+p` to navigate.

### Mouse Support

- **Click** on a project to switch to it
- **Scroll** to navigate the list
- **Click outside** the modal to close it

## Session Isolation

Each sidecar instance maintains its own project state:

- Switching projects in one terminal doesn't affect others
- Each session tracks its own active plugin per project
- State is persisted per working directory

## What Happens on Switch

When you switch projects:

1. All plugins stop (file watchers, git commands, etc.)
2. Plugin context updates to new working directory
3. All plugins reinitialize with new path
4. Your previously active plugin for that project is restored
5. A toast notification confirms the switch

## State Persistence

Sidecar remembers:

- Which plugin was active for each project
- File browser cursor position and expanded directories
- Sidebar widths and view preferences

These are saved per project path in `~/.config/sidecar/state.json`.

## Worktree-Specific Behavior

When using worktree switching (`W`):

- **Deleted worktrees**: If a worktree is deleted externally, sidecar gracefully falls back to the main branch
- **Last-active restoration**: When switching to a project's main repo, sidecar restores the last-active worktree
- **Non-git repos**: Shows "No worktrees found" in non-git repos or repos with only the main branch

## Per-Project Themes

Projects can have individual themes:

```json
{
  "projects": {
    "list": [
      {"name": "work", "path": "~/work/main", "theme": "dark"},
      {"name": "personal", "path": "~/code/personal", "theme": "light"}
    ]
  }
}
```

## Troubleshooting

### "No projects configured" message

Add projects to your config file:

```json
{
  "projects": {
    "list": [
      {"name": "myproject", "path": "~/code/myproject"}
    ]
  }
}
```

### Project path doesn't exist

The switcher will show the project but switching may fail. Ensure all paths are valid:

```bash
# Verify your paths
ls ~/code/myproject
```

### Current project not highlighted

The current project is shown in green with "(current)" label. If not highlighted:
- Check that the path in config exactly matches the current working directory
- Paths are compared after `~` expansion

### "No worktrees found" message

This appears when pressing `W` in:
- A non-git repository
- A git repository with only the main branch (no additional worktrees)

To use worktree switching, create worktrees with `git worktree add`.

### Switch seems to hang

Complex projects with many files may take longer to initialize. The switch includes:
- Stopping file watchers
- Scanning the new directory tree
- Starting new watchers
- Loading git status

## Example Configs

### Minimal

```json
{
  "projects": {
    "list": [
      {"name": "work", "path": "~/work/main-project"}
    ]
  }
}
```

### Multiple Projects

```json
{
  "projects": {
    "list": [
      {"name": "sidecar", "path": "~/code/sidecar"},
      {"name": "td", "path": "~/code/td"},
      {"name": "frontend", "path": "~/work/frontend"},
      {"name": "backend", "path": "~/work/backend"},
      {"name": "docs", "path": "~/work/documentation"}
    ]
  }
}
```

### With Other Settings

```json
{
  "projects": {
    "mode": "single",
    "root": ".",
    "list": [
      {"name": "myapp", "path": "~/code/myapp"}
    ]
  },
  "plugins": {
    "git-status": {"enabled": true},
    "td-monitor": {"enabled": true}
  }
}
```

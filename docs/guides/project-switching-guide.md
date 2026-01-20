# Project Switching Guide

Switch between git repositories without restarting sidecar.

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
3. Select a project with `j/k` or arrow keys
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

### Navigation

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `Enter` | Switch to selected project |
| `Esc` / `q` | Close without switching |

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

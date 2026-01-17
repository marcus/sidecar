---
sidebar_position: 1
title: Getting Started
---

# Sidecar

A terminal dashboard for AI coding agents. Monitor git changes, browse conversations, track tasks, and manage worktrees—all without leaving your terminal.

![Sidecar Git Status](/img/screenshots/sidecar-git.png)

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/scripts/setup.sh | bash
```

**Requirements:** macOS, Linux, or WSL. Go 1.21+ (for building from source).

## Quick Start

Run from any project directory:

```bash
sidecar
```

Sidecar automatically detects your git repo and any active AI agent sessions.

### Suggested Setup

Split your terminal: agent on the left, sidecar on the right.

```
+-----------------------------+---------------------+
|                             |                     |
|   Claude Code / Cursor      |      Sidecar        |
|                             |                     |
|   $ claude                  |   [Git] [Files]     |
|   > fix the auth bug...     |   [TD]  [Worktrees] |
|                             |                     |
+-----------------------------+---------------------+
```

As the agent works, watch files change in Git Status, track tasks in TD Monitor, and browse code in File Browser.

## Plugins

Sidecar uses a plugin architecture. Each plugin provides a focused view into your development workflow.

### Git Status

Stage files, view diffs, browse commit history. A lightweight alternative to `git status` and `git diff`.

![Git Status with Diff](/img/screenshots/sidecar-git.png)

| Key | Action |
|-----|--------|
| `s` | Stage file |
| `u` | Unstage file |
| `d` | View diff (full-screen) |
| `v` | Toggle side-by-side diff |
| `c` | Commit staged changes |
| `h/l` | Switch sidebar/diff focus |

![Side-by-side Diff](/img/screenshots/sidecar-git-diff-side-by-side.png)

### Conversations

Browse AI agent session history with message content, token usage, and search.

![Conversations](/img/screenshots/sidecar-conversations.png)

Supported agents:
- Claude Code
- Cursor
- Gemini CLI
- OpenCode
- Codex
- Warp

| Key | Action |
|-----|--------|
| `/` | Search sessions |
| `enter` | Expand/collapse messages |
| `j/k` | Navigate sessions |

### TD Monitor

Integration with [TD](https://github.com/marcus/td), a task management system for AI agents working across context windows.

![TD Monitor](/img/screenshots/sidecar-td.png)

| Key | Action |
|-----|--------|
| `r` | Submit review |
| `enter` | View task details |

### File Browser

Navigate project files with a collapsible tree and syntax-highlighted preview.

![File Browser](/img/screenshots/sidecar-file-browser.png)

| Key | Action |
|-----|--------|
| `enter` | Open/close folder |
| `/` | Search files |
| `h/l` | Switch tree/preview focus |

### Worktrees

Manage git worktrees for parallel development. Create isolated branches, link tasks, and launch agents directly.

| Key | Action |
|-----|--------|
| `n` | Create worktree |
| `D` | Delete worktree |
| `a` | Launch agent |
| `t` | Link TD task |
| `m` | Start merge workflow |

## Navigation

Global shortcuts work across all plugins:

| Key | Action |
|-----|--------|
| `q`, `ctrl+c` | Quit |
| `tab` / `shift+tab` | Next/previous plugin |
| `1-5` | Focus plugin by number |
| `j/k`, `arrow keys` | Navigate items |
| `ctrl+d/u` | Page down/up |
| `g/G` | Jump to top/bottom |
| `?` | Toggle help |
| `r` | Refresh |

## Configuration

Config file: `~/.config/sidecar/config.json`

```json
{
  "plugins": {
    "git-status": { "enabled": true, "refreshInterval": "1s" },
    "td-monitor": { "enabled": true, "refreshInterval": "2s" },
    "conversations": { "enabled": true },
    "file-browser": { "enabled": true },
    "worktrees": { "enabled": true }
  },
  "ui": {
    "showFooter": true,
    "showClock": true
  }
}
```

## CLI Options

```bash
sidecar                      # Run in current directory
sidecar --project /path      # Specify project root
sidecar --debug              # Enable debug logging
sidecar --version            # Print version
```

## Updates

Sidecar checks for updates on startup. When available, a notification appears. Press `!` to open diagnostics and see the update command.

## Source

[GitHub Repository](https://github.com/marcus/sidecar)

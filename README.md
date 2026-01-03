# Sidecar

Terminal UI for monitoring AI coding agent sessions.

**Status: Alpha** - Functional but expect rough edges and breaking changes.

![Git Status](docs/screenshots/sidecar-git.png)

## Overview

Sidecar provides a unified terminal interface for viewing Claude Code conversations, git status, and task progress. Built for developers who want visibility into their AI coding sessions without leaving the terminal.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/scripts/setup.sh | bash
```

Or see [Getting Started](docs/GETTING_STARTED.md) for manual installation.

## Requirements

- Go 1.21+
- macOS, Linux, or WSL

## Quick Start

After installation, run from any project directory:

```bash
sidecar
```

## Suggested Use

Split your terminal horizontally: run your coding agent (Claude Code, Cursor, etc.) on the left and sidecar on the right.

```
┌─────────────────────────────┬─────────────────────┐
│                             │                     │
│   Claude Code / Cursor      │      Sidecar        │
│                             │                     │
│   $ claude                  │   [Git] [Files]     │
│   > fix the auth bug...     │   [Tasks] [Convos]  │
│                             │                     │
└─────────────────────────────┴─────────────────────┘
```

As the agent works, you can:

- Watch tasks move through the workflow in TD Monitor
- See files change in real-time in the Git plugin
- Browse and edit code yourself in the File Browser
- View and resume conversations across all supported agent adapters

This setup gives you visibility into what the agent is doing without interrupting your workflow. The goal is to allow working with agents without the need for an editor or IDE.

## Usage

```bash
# Run from any project directory
sidecar

# Specify project root
sidecar --project /path/to/project

# Enable debug logging
sidecar --debug

# Check version
sidecar --version
```

## Updates

Sidecar checks for updates on startup. When a new version is available, a toast notification appears. Press `!` to open the diagnostics modal and see the update command.

## Plugins

### Git Status

View staged, modified, and untracked files with a split-pane interface. The sidebar shows files and recent commits; the main pane shows syntax-highlighted diffs.

![Git Status with Diff](docs/screenshots/sidecar-git.png)

**Features:**

- Stage/unstage files with `s`/`u`
- View diffs inline or full-screen with `d`
- Toggle side-by-side diff view with `v`
- Browse commit history and view commit diffs
- Auto-refresh on file system changes

![Side-by-side Diff](docs/screenshots/sidecar-git-diff-side-by-side.png)

### Conversations

Browse Claude Code session history with message content, token usage, and search.

![Conversations](docs/screenshots/sidecar-conversations.png)

**Features:**

- View all sessions grouped by date
- Search sessions with `/`
- Expand messages to see full content
- Track token usage per session

### TD Monitor

Integration with [TD](https://github.com/marcus/td), a task management system designed for AI agents working across context windows. TD helps agents track work, log progress, and maintain context across sessions—essential for AI-assisted development where context windows reset between conversations.

![TD Monitor](docs/screenshots/sidecar-td.png)

**Features:**

- Current focused task display
- Scrollable task list with status indicators
- Activity log with session context
- Quick review submission with `r`

See the [TD repository](https://github.com/marcus/td) for installation and CLI usage.

### File Browser

Navigate project files with a tree view and syntax-highlighted preview.

![File Browser](docs/screenshots/sidecar-file-browser.png)

**Features:**

- Collapsible directory tree
- Code preview with syntax highlighting
- Auto-refresh on file changes

## Keyboard Shortcuts

| Key                 | Action                           |
| ------------------- | -------------------------------- |
| `q`, `ctrl+c`       | Quit                             |
| `tab` / `shift+tab` | Navigate plugins                 |
| `1-9`               | Focus plugin by number           |
| `j/k`, `↓/↑`        | Navigate items                   |
| `ctrl+d/u`          | Page down/up in scrollable views |
| `g/G`               | Jump to top/bottom               |
| `enter`             | Select                           |
| `esc`               | Back/close                       |
| `r`                 | Refresh                          |
| `?`                 | Toggle help                      |

### Git Status Shortcuts

| Key   | Action                    |
| ----- | ------------------------- |
| `s`   | Stage file                |
| `u`   | Unstage file              |
| `d`   | View diff (full-screen)   |
| `v`   | Toggle side-by-side diff  |
| `h/l` | Switch sidebar/diff focus |
| `c`   | Commit staged changes     |

## Configuration

Config file: `~/.config/sidecar/config.json`

```json
{
  "plugins": {
    "git-status": { "enabled": true, "refreshInterval": "1s" },
    "td-monitor": { "enabled": true, "refreshInterval": "2s" },
    "conversations": { "enabled": true },
    "file-browser": { "enabled": true }
  },
  "ui": {
    "showFooter": true,
    "showClock": true
  }
}
```

## Development

```bash
make build        # Build to ./bin/sidecar
make test         # Run tests
make test-v       # Verbose test output
make install-dev  # Install with git version info
make fmt          # Format code
```

## License

MIT

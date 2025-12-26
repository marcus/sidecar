# Sidecar

Terminal UI for viewing AI coding agent sessions. Monitor Claude Code conversations, git status, and task progress in a unified interface.

## Requirements

- Go 1.23+

## Installation

```bash
# Clone and install with version info
git clone https://github.com/sst/sidecar
cd sidecar
make install-dev

# Or basic install (no version info)
make install

# Or direct go install
go install ./cmd/sidecar
```

## Usage

```bash
# Run from any project directory
sidecar

# Specify project root
sidecar --project /path/to/project

# Use custom config
sidecar --config ~/.config/sidecar/config.json

# Enable debug logging
sidecar --debug

# Check version
sidecar --version
```

## Plugins

Sidecar includes three built-in plugins:

### Git Status
Shows changed files with staging actions.
- View staged, modified, and untracked files
- Stage/unstage files with `s`/`u`
- View diffs with `d`

### TD Monitor
Shows tasks from the TD task management system.
- View in-progress, ready, and reviewable issues
- Approve issues with `a`
- Switch lists with `tab`

### Conversations
Browse Claude Code session history.
- View recent sessions for the current project
- Read conversation messages and tool usage
- Token usage stats

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `q`, `ctrl+c` | Quit |
| `tab` | Next plugin |
| `shift+tab` | Previous plugin |
| `1-9` | Focus plugin by number |
| `?` | Toggle help |
| `!` | Toggle diagnostics |
| `ctrl+h` | Toggle footer |
| `r` | Refresh |
| `j/k` or `↓/↑` | Navigate |
| `enter` | Select |
| `esc` | Back/close |

## Configuration

Config file: `~/.config/sidecar/config.json`

```json
{
  "plugins": {
    "git-status": { "enabled": true, "refreshInterval": "1s" },
    "td-monitor": { "enabled": true, "refreshInterval": "2s" },
    "conversations": { "enabled": true }
  },
  "ui": {
    "showFooter": true,
    "showClock": true
  }
}
```

## Development

```bash
# Build binary to ./bin/
make build

# Run tests
make test

# Run tests with verbose output
make test-v

# Install with version from git
make install-dev

# Format code
make fmt

# Show current version
make version
```

## Releasing

```bash
# Create a version tag (validates semver format)
make tag VERSION=v0.1.0

# Push tag to origin
make release VERSION=v0.1.0
```

## Build Targets

| Target | Description |
|--------|-------------|
| `make build` | Build binary to `./bin/sidecar` |
| `make install` | Install to GOBIN |
| `make install-dev` | Install with git-derived version |
| `make test` | Run tests |
| `make clean` | Remove build artifacts |
| `make build-all` | Cross-platform builds |

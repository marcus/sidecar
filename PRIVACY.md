# Privacy

Sidecar is a local-first terminal application. This document describes exactly what data it accesses, what network requests it makes, and what it writes to disk.

## Local Data Access

### Git repository

Runs `git` CLI commands (status, diff, log, branch, worktree, stash) in the current project directory. Read-only except when you explicitly stage, commit, push, merge, or create worktrees.

### AI agent sessions (read-only)

Reads conversation history from local agent data directories to display in the Conversations plugin:

- **Claude Code** — `~/.claude/projects/` and `~/.config/claude/projects/` (JSONL session files), `~/.claude/stats-cache.json` (token usage stats)
- **Cursor** — `~/.cursor/chats/` (SQLite per-workspace)
- **Codex** — `~/.codex/sessions/` (JSONL)
- **Warp** — `~/Library/Application Support/dev.warp.Warp-Stable/warp.sqlite` (macOS), `~/.local/state/warp-terminal/warp.sqlite` (Linux)
- **Gemini CLI** — `~/.gemini/tmp/{hash}/chats/`
- **Amp** — `~/.local/share/amp/threads/` (or `$AMP_DATA_HOME`)
- **Kiro** — `~/.kiro/data.sqlite3` and platform-specific fallbacks
- **OpenCode** — `~/Library/Application Support/opencode/storage/` (macOS) and platform-specific fallbacks

These files are read-only. Sidecar never writes to agent data directories.

### Config and state (read/write)

Sidecar reads and writes its own files under `~/.config/sidecar/`:

| File | Purpose |
|------|---------|
| `config.json` | User configuration (projects, plugin settings, theme, keymaps) |
| `state.json` | Persistent UI state (diff modes, pane widths, active plugin per project) |
| `version_cache.json` | Cached sidecar version check result (3-hour TTL) |
| `td_version_cache.json` | Cached td version check result (3-hour TTL) |
| `debug.log` | Debug log output (only when `--debug` flag is used) |

### Project-level dotfiles (read/write)

In workspace directories, sidecar may create:

- `.sidecar/config.json` — per-project configuration (prompts, theme overrides)
- `.sidecar/shells.json` — shell display names and metadata
- `.sidecar-task`, `.sidecar-agent`, `.sidecar-pr`, `.sidecar-base` — workspace state files
- `.sidecar-start.sh` — temporary agent launcher script
- `.td-root` — links worktrees to a shared td database root

These are added to `.gitignore` automatically.

### TD tasks

If [td](https://github.com/marcus/td) is installed, sidecar runs `td` CLI commands and reads the `.todos/issues.db` SQLite database (shared across worktrees via `.td-root`).

### Tmux sessions

The Workspaces plugin creates and controls tmux sessions to run agents and shells. It sends commands via `tmux send-keys`, captures output via `tmux capture-pane`, and manages session lifecycle.

### Clipboard

Sidecar reads from and writes to the system clipboard (via the `atotto/clipboard` library) for copy/paste operations: yanking commit hashes, file paths, session details, and pasting text in interactive mode.

### Environment variables

Sidecar reads `EDITOR`/`VISUAL` (to open files in your editor), `SIDECAR_PPROF` (profiling server), and standard path variables (`HOME`, `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`) for locating agent data directories. It does not read or require API keys or tokens.

### Session export

The Conversations plugin can export a session to a markdown file in the current working directory, or copy it to the clipboard. This is user-initiated only.

## Network Requests

Sidecar makes outbound HTTP requests in two cases:

### Version checks (automatic, cached)

On startup, sidecar checks for updates by fetching the latest release tag from:

- `api.github.com/repos/marcus/sidecar/releases/latest`
- `api.github.com/repos/marcus/td/releases/latest`

These requests use a 5-second timeout, send no authentication, and are cached locally for 3 hours (`version_cache.json`, `td_version_cache.json`). After the first successful check, no network call occurs until the cache expires. Development builds (untagged or `devel` versions) skip these checks entirely.

### Changelog fetch (user-initiated)

When you open the changelog from the update modal, sidecar fetches `raw.githubusercontent.com/marcus/sidecar/main/CHANGELOG.md` with a 10-second timeout.

### Self-update (user-initiated)

When you confirm an update from the update modal, sidecar runs `brew upgrade sidecar` or `go install ...` depending on your install method. These commands make their own network requests.

### External CLI tools

The Workspaces plugin runs `gh` CLI commands (e.g., `gh pr list`, `gh pr create`) using your existing GitHub CLI authentication. These run only in response to explicit user actions.

Git push, pull, and fetch operations use the local `git` CLI with your configured remotes and credentials.

Sidecar also opens URLs in your system browser (`open`/`xdg-open`) when you choose to view a commit or PR on GitHub.

## What Sidecar Does NOT Do

- No telemetry, analytics, or usage tracking
- No crash reporting
- No data transmitted to any server other than the GitHub API calls listed above
- No account or login required
- No cookies, local storage, or browser fingerprinting

## Opting Out of Network Requests

Version checks are skipped automatically for development builds (untagged or `devel` versions). There is currently no config flag to disable version checks on release builds. If you need fully offline operation, run sidecar with network access blocked at the OS or firewall level.

## pprof Profiling Server

When the `SIDECAR_PPROF` environment variable is set, sidecar starts a Go pprof HTTP server on `localhost` (default port 6060). This is localhost-only and intended for development profiling. It is never started unless you explicitly set the variable.

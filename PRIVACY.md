# Privacy

Sidecar is a local-first terminal application. This document describes exactly what data it accesses and what network requests it makes.

## Local Data Access

Sidecar reads local files to populate its plugins. It never modifies files outside its own config and state directories.

**Git data** — Runs `git` commands (status, diff, log, worktree) in the current project directory.

**AI agent sessions** — Reads conversation history from local agent data directories to display in the Conversations plugin:
- Claude Code: `~/.claude/projects/` and `~/.config/claude/projects/`
- Cursor: `~/.cursor/chats/`
- Codex: `~/.codex/sessions/`
- Warp: `~/Library/Application Support/dev.warp.Warp-Stable/`
- Gemini CLI, Amp, Kiro, OpenCode: their respective local session directories

These files are read-only. Sidecar never writes to agent data directories.

**Config and state** — Reads and writes its own configuration (`~/.config/sidecar/config.json`) and persistent state (`~/.config/sidecar/state.json`). Debug logs go to `~/.config/sidecar/debug.log`.

**TD tasks** — If [td](https://github.com/marcus/td) is installed, sidecar runs `td` CLI commands to display tasks.

## Network Requests

Sidecar makes **two** outbound HTTP requests, both to the GitHub API:

1. **Version check** — On startup, fetches the latest release tag from `api.github.com/repos/marcus/sidecar/releases/latest` and `api.github.com/repos/marcus/td/releases/latest` to show an update notification. These requests have a 5-second timeout and no authentication.

2. **Changelog fetch** — When you open the changelog from the update modal, fetches `raw.githubusercontent.com/marcus/sidecar/main/CHANGELOG.md` (10-second timeout).

The Workspaces plugin runs `gh` CLI commands locally (e.g., `gh pr list`, `gh pr create`). These use your existing GitHub CLI authentication and only run in response to explicit user actions.

## What Sidecar Does NOT Do

- No telemetry, analytics, or usage tracking
- No crash reporting
- No data transmitted to any server other than the GitHub API calls listed above
- No account or login required
- No cookies, local storage, or browser fingerprinting
- Pprof profiling server only starts when you explicitly set `SIDECAR_PPROF`

## Opting Out of Network Requests

Version checks are skipped automatically for development builds (untagged or `devel` versions). There is currently no config flag to disable version checks on release builds. If you need fully offline operation, run sidecar with network access blocked at the OS or firewall level.

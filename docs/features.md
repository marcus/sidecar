# Comprehensive List of Sidecar Features

Sidecar puts your entire software engineering workflow into a single, unified terminal interface. This document provides an exhaustive, section-by-section feature list of Sidecar, the **TD** task context protocol, the **Tasks** embedded manager, and all integrated agent tools.

---

## Table of Contents
1. [Core TUI Shell & System Architecture](#1-core-tui-shell--system-architecture)
2. [Git Status & Diff Plugin](#2-git-status--diff-plugin)
3. [Conversations Plugin & Agent Adapters](#3-conversations-plugin--agent-adapters)
4. [TD Task System & TD Monitor Plugin](#4-td-task-system--td-monitor-plugin)
5. [File Browser & Preview Plugin](#5-file-browser--preview-plugin)
6. [Workspaces & Parallel Agent Plugin](#6-workspaces--parallel-agent-plugin)
7. [Embedded Tasks TUI Plugin](#7-embedded-tasks-tui-plugin)
8. [Notes & Scratchpad Plugin](#8-notes--scratchpad-plugin)
9. [Project & Worktree Switchers](#9-project--worktree-switchers)
10. [Themes & Design Palette System](#10-themes--design-palette-system)
11. [Virtual Terminal & Shell Integration (PTY)](#11-virtual-terminal--shell-integration-pty)
12. [Declarative Modal & UI System](#12-declarative-modal--ui-system)
13. [Keyboard Shortcuts & Navigation Parity](#13-keyboard-shortcuts--navigation-parity)
14. [Configuration, CLI & Feature Flags](#14-configuration-cli--feature-flags)

---

## 1. Core TUI Shell & System Architecture

- **Bubble Tea v2 Rendering Engine:** Built on Charm's Bubble Tea v2 for high-performance terminal UI rendering, handling rapid state updates and smooth layout resizing.
- **Header & Navigation Bar:** Displays current project name, active Git worktree, active tab plugin, system clock, and diagnostic indicators.
- **Unified Keymap & Footer Bar:** Auto-truncating footer key hints generated dynamically from active plugin bindings without duplicating plugin views.
- **Startup Latency Tracing:** Zero-I/O `Init()` phase before first frame paint (`internal/startuptrace`). Avoids filesystem walks, database opens, or subprocess spawns until async `tea.Cmd` execution.
- **Diagnostics Modal (`!`):** Inspect runtime system status, update availability, active configuration paths (`SIDECAR_DIAG_PATHS=1`), and error logs.
- **Automated Version Checking:** Non-blocking background check against GitHub releases for new versions with toast notifications and diagnostic upgrade commands.
- **Terminal Title Formatting:** Dynamic window/tab title interpolation (`terminalTitle`) supporting variables `{project}`, `{worktree}`, `{plugin}`, and `{dir}`.
- **State Tree Isolation:** Isolated application state management per project and per worktree (`SIDECAR_ISOLATED_STATE=1` for safe headless test execution).

---

## 2. Git Status & Diff Plugin

- **Dual-Pane Split View:** Left pane lists staged, modified, untracked, and conflicting files alongside recent commit history; right pane displays syntax-highlighted diff previews.
- **Interactive Staging & Unstaging:** One-key file staging (`s`) and unstaging (`u`) with instant visual status updates.
- **Full-Screen Diff View (`d`):** Toggle full-screen syntax-highlighted diff modal with page scrolling and search.
- **Side-by-Side Split Diff View (`v`):** Switch between unified inline diffs and side-by-side split diff comparisons.
- **Commit History Inspector:** Browse recent git commit logs, inspect historical commit diffs, and view author/timestamp metadata.
- **Commit Modal (`c`):** Interactive modal dialog to author commit messages directly from Sidecar without leaving the TUI.
- **Real-Time Disk Watcher:** Automatic file system notification listener that auto-refreshes git status and diff views when changes occur on disk.

---

## 3. Conversations Plugin & Agent Adapters

**Opt-in (default off).** Gated by feature flag `conversations_plugin`. When disabled, Sidecar does not register the tab, construct history adapters, or read agent session stores. Enable via `features.flags.conversations_plugin` or `--enable-feature=conversations_plugin`. With the flag on, `plugins.conversations.enabled: false` remains a hard off-switch.

- **Multi-Agent Session Browser:** Centralized history viewer for sessions logged across **10+ AI coding agent tools**:
  1. **Amp Code** (`amp`)
  2. **Antigravity** (`antigravity`)
  3. **Claude Code** (`claudecode`)
  4. **Codex** (`codex`)
  5. **Copilot CLI** (`copilot`)
  6. **Cursor CLI** (`cursor`)
  7. **Kiro** (`kiro`)
  8. **OpenCode / OMP** (`opencode`, `omp`)
  9. **Pi Agent** (`piagent`, `pi`)
  10. **Warp** (`warp`)
- **Full-Text Session Search (`/`):** Real-time text filtering across message transcripts, session titles, prompt inputs, and token outputs.
- **Date-Grouped Sidebar:** Sessions organized chronologically (Today, Yesterday, Past Week) with model icons and conversation lengths.
- **Expandable Message Inspector:** View full turn-by-turn prompt/response exchanges, tool call inputs, and execution logs.
- **Token Metrics & Cost Tracking:** Computes input, output, cached token counts, and estimated run costs across active agent conversations.
- **Tiered File Watcher & SQLite Cache:** Incremental log file parsing with high-performance SQLite caching for sub-millisecond retrieval of massive historical logs.

---

## 4. TD Task System & TD Monitor Plugin

### The TD Task Protocol
- **Agent Context Preservation:** Designed for AI agents working across context window boundaries, tracking work, logging progress, and maintaining context durable across resets.
- **CLI Commands & Handoffs:** `td start <id>`, `td log <progress>`, `td handoff <id>`, `td review <id>`, and `td approve <id>`.
- **Review Verification Modes:** Supports independent session approval, sub-agent reviewer verification (`--reviewed-by`), and self-review (`--self-review`).
- **SQLite & Journal Backend:** Context engine keeping task state consistent across CLI and Sidecar sessions.

### Sidecar TD Monitor Plugin
- **Focused Task Banner:** Top header display highlighting the currently active task ID, description, and status.
- **Status-Filtered Task List:** Filterable task browser displaying tasks by status (open, in-progress, pending review, completed).
- **Session Activity Feed:** Scrollable log of progress updates, handoff notes, and sub-agent task transitions.
- **One-Key Review Submission (`r`):** Rapidly trigger task status transitions and handoff reviews directly from the list.

---

## 5. File Browser & Preview Plugin

- **Directory Tree View:** Hierarchical file browser with expanding/collapsible directory nodes and file type icons.
- **Syntax-Highlighted Preview:** High-speed code preview pane supporting line numbers, custom palette themes, and soft wrapping.
- **PTY Inline Editor (`tmux_inline_edit`):**
  - Live in-place text editor backed by a virtual tmux PTY process.
  - Full keyboard text editing, cursor movement, character insertion, deletion, and line jumping.
  - Unsaved changes guard modal on exit.
  - Direct mouse click-to-position cursor forwarding.
- **Directory Watcher (`files_auto_refresh`):** Automatically detects file additions, deletions, and modifications in expanded folders.

---

## 6. Workspaces & Parallel Agent Plugin

- **Worktree & Branch Workspaces:**
  - Create (`n`) and delete (`D`) isolated workspace directory trees.
  - Sibling directory isolation and Git worktree detection.
  - Automatic `.gitignore` management for workspace metadata files.
- **Embedded Agent Shell Launcher (`a`):**
  - Launch coding agents (Claude, Cursor, Codex, Gemini, OpenCode, Pi, etc.) in dedicated embedded shell PTY panes.
  - **Shell Renaming (`sidecar shell rename`):** Automatically update tmux pane names to reflect active task goals.
  - Cross-instance manifest tracking and automated background shell recovery.
- **GitHub Pull Request & Merge Workflow (`m`):**
  - Create pull requests directly from workspace branches.
  - Fetch and checkout PR branches (`fetch_pr`).
  - **Configurable Merge Strategies:** Direct merge, squash merge, rebase, and PR workflow.
  - Conflict detection, resolution helpers, and stale commit detection.
  - One-key branch pushing (`p`) and external file manager/terminal opening (`o`).
- **Cross-Project Overview:**
  - Kanban board and list views of running agents across all active workspaces.
  - Real-time agent status indicators (Active, Waiting, Idle) with animated status icons.

---

## 7. Embedded Tasks TUI Plugin

- **Embedded Application Facade:** Wraps `github.com/marcus/tasks/pkg/tui` inside Sidecar (`plugins.tasks.enabled`; the `tasks_plugin` flag is a read-only alias while that key is absent).
- **Rich Shortcut Registry:** Features 355+ keybindings across 14 focus contexts for full task lifecycle management.
- **Isolated Session Namespace:** Keeps state in `$XDG_STATE_HOME/tasks/hosts/sidecar/tui.json` to prevent config drift.
- **Task Management Engine:** Task lists, kanban cards, priority levels, tag filtering, journal logs, undo history, and automated agent execution queues.

---

## 8. Notes & Scratchpad Plugin

- **Project Scratchpad:** Project-scoped note-taking engine for quick thoughts, scratch code, and agent instructions (`plugins.notes.enabled`; the `notes_plugin` flag is a read-only alias while that key is absent).
- **Inline Note Editor:** Multi-line text editor with mouse text selection and instant disk save. Opened by clicking a note or pressing `enter` (`i` from the preview); needs no tmux.
- **Editor in the Right Pane:** `e` runs `$EDITOR` inside the notes pane rather than taking over the screen.
- **External Editor:** `E` opens the note in `$EDITOR`, leaving Sidecar until you exit.
- **Search & Conversion Modals:**
  - Quick note search modal (`/`).
  - Convert notes directly into actionable TD tasks or worktree specs.
- **Disk Synchronization:** Auto-reloads external note modifications made outside of Sidecar.

---

## 9. Project & Worktree Switchers

- **Project Switcher Modal (`@`):**
  - Instant project context switcher listing all projects configured in `~/.config/sidecar/config.json`.
  - Re-initializes all 8 plugins under the target project context without restarting Sidecar.
  - Preserves per-project active plugin tab, cursor positions, and scroll offsets.
- **Worktree Switcher Modal (`W`):**
  - Detects all git worktrees within the current repository.
  - Remembers and restores worktree context when switching back and forth between projects.

---

## 10. Themes & Design Palette System

- **Built-in Themes:** Pre-configured themes (`default`, `dracula`) with gradient borders, status pills, and tab styling.
- **Community Theme Browser (`#`):**
  - Access to **453+ themes** derived from `iTerm2-Color-Schemes`.
  - Search filtering by theme name with instant live visual preview as you navigate.
  - Color swatches displaying primary, background, selection, and accent colors for every scheme.
- **Theme Overrides & Customization:** Per-project theme assignments and custom color overrides in `config.json`. Programmatic Lipgloss token distribution across all views and modals.

---

## 11. Virtual Terminal & Shell Integration (PTY)

- **Low-Latency PTY Engine:** High-performance terminal emulation engine using `internal/tty` and isolated tmux servers.
- **Interactive Input (`tmux_interactive_input`):** Direct keypress forwarding to interactive shell sessions and sub-processes.
- **Full tmux attach (`tmux_full_attach`, default off):** Suspend Sidecar and run `tmux attach-session`. Off so users stay in the embedded pane.
- **Workspace terminal panel (`workspace_terminal_panel`, default on):** Ctrl+T / Alt+T split terminal beside the workspace preview.
- **Adaptive Polling & Cursor Rendering:** Smooth virtual cursor positioning, line scrolling, and output buffer management.
- **Mouse Interaction:** Mouse scroll wheel, click-to-focus, and drag-and-drop pane boundary resizing (`internal/mouse`).
- **Headless Testing Toolchain (`scripts/tmux-drive.sh`):** Drives Sidecar in headless tmux sessions with isolated configuration/state paths (`SIDECAR_ISOLATED_STATE=1`) and captures text/PNG screenshots.

---

## 12. Declarative Modal & UI System

- **Declarative Modal Framework (`internal/modal`):**
  - **Confirm Modal:** Action confirmation dialogs with custom button text.
  - **Input Modal:** Text input dialogs with validation and placeholders.
  - **Select Modal:** Searchable options selection dialogs.
  - **Form Modal:** Multi-field form dialogs (inputs, textareas, checkboxes, lists).
- **Overlay Stack:** Stacked modal rendering with backdrop dimming, mouse click-away detection, and keyboard focus traps.
- **Drag-to-Resize Panes (`drag-pane`):** Drag split-pane dividers to re-size UI panels with state persistence.

---

## 13. Keyboard Shortcuts & Navigation Parity

| Key Range | Action |
| --- | --- |
| `q`, `ctrl+c` | Quit Sidecar |
| `@` | Open Project Switcher |
| `W` | Open Worktree Switcher |
| `#` | Open Theme Switcher |
| `i` | Open Issue Modal |
| `!` | Open Diagnostics Modal |
| `tab` / `shift+tab` | Navigate Plugin Tabs |
| `1` – `9` | Direct Focus Plugin by Index |
| `j` / `k`, `↓` / `↑` | Navigate Items |
| `ctrl+d` / `ctrl+u` | Page Down / Page Up |
| `g` / `G` | Jump to Top / Jump to Bottom |
| `enter` | Select / Execute |
| `esc` | Back / Close Modal |
| `r` | Refresh View |
| `?` | Toggle Global Help Menu |

*Additional plugin-specific keybindings available in Git Status (`s`, `u`, `d`, `v`, `c`), Workspaces (`n`, `D`, `a`, `t`, `m`, `p`, `o`), Conversations (`/`), and Notes.*

---

## 14. Configuration, CLI & Feature Flags

- **Configuration File Location:** `~/.config/sidecar/config.json`
- **CLI Options:**
  - `sidecar` — Launch in current directory.
  - `sidecar --project <path>` — Launch targeting specified project root.
  - `sidecar --debug` — Enable debug logging to file.
  - `sidecar --version` — Print version and git revision details.
- **Feature Flag System (`internal/features`):**
  - CLI overrides (`--feature <name>=<bool>`), configuration overrides (`config.json`), and default fallbacks.
  - Supported flags: `tmux_interactive_input`, `tmux_full_attach`, `tmux_inline_edit`, `files_auto_refresh`, `notes_plugin`, `tasks_plugin`, `workspace_doc_panes`, `workspace_terminal_panel`, `cross_project_overview`.
- **Diagnostic Environment Variables:**
  - `SIDECAR_STARTUP_TRACE=stderr` — Print startup phase timing and first ready frame timestamp.
  - `SIDECAR_DIAG_PATHS=1` — Print state, config, and tmux socket path resolutions on startup.
  - `SIDECAR_ISOLATED_STATE=1` — Enforce strict isolation mode, refusing to start if user configuration or state paths are resolved.

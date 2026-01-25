# Worktree Manager Plugin Technical Specification

> A sidecar plugin for orchestrating git worktrees with AI coding agents

**Version:** 0.1.0-draft  
**Status:** Design Phase  
**Authors:** Collaborative design session  
**Last Updated:** January 2026

---

## Table of Contents

1. [Overview](#1-overview)
2. [Goals and Non-Goals](#2-goals-and-non-goals)
3. [Architecture](#3-architecture)
4. [User Interface Design](#4-user-interface-design)
5. [Git Worktree Operations](#5-git-worktree-operations)
6. [tmux Integration](#6-tmux-integration)
7. [Agent Status Detection](#7-agent-status-detection)
8. [TD Task Manager Integration](#8-td-task-manager-integration)
9. [Data Persistence](#9-data-persistence)
10. [Configuration](#10-configuration)
11. [Safety Precautions](#11-safety-precautions)
12. [Implementation Phases](#12-implementation-phases)
13. [Reference Implementations](#13-reference-implementations)
14. [Appendix: Command Reference](#appendix-command-reference)

---

## 1. Overview

### 1.1 Problem Statement

Developers using AI coding agents (Claude Code, Codex, Aider, Gemini) want to run multiple agents in parallel on different features without:

- Branch conflicts from concurrent work
- Losing visibility into what each agent is doing
- Manual context-switching overhead
- Forgetting to commit/push completed work

### 1.2 Solution

A sidecar plugin that:

- Manages git worktrees for isolated parallel development
- Runs agents in tmux sessions for process isolation
- Provides a unified TUI to monitor all agents
- Integrates with `td` for task tracking and handoffs

### 1.3 Prior Art

| Tool                                                                      | Strengths                                   | Gaps                           |
| ------------------------------------------------------------------------- | ------------------------------------------- | ------------------------------ |
| [Claude Squad](https://github.com/smtg-ai/claude-squad)                   | Proven tmux/worktree pattern, BubbleTea TUI | No task management integration |
| [Conductor](https://conductor.ai)                                         | Beautiful dashboard, auto-management        | macOS GUI only, closed source  |
| [Treehouse Worktree](https://github.com/mark-hingston/treehouse-worktree) | MCP support, lock system                    | No TUI, focused on Cursor      |

This plugin bridges the gap by combining worktree orchestration with td's task management in a terminal-native interface that fits sidecar's existing plugin architecture.

---

## 2. Goals and Non-Goals

### 2.1 Goals

- **G1**: View all worktrees and their agent status at a glance
- **G2**: Create worktrees linked to td tasks with one command
- **G3**: See live agent output without leaving sidecar
- **G4**: Approve/reject agent prompts from the TUI
- **G5**: Push, merge, and cleanup completed work
- **G6**: Resume crashed agents on existing worktrees

### 2.2 Non-Goals

- **NG1**: Replace the agent's native UI (users can always attach)
- **NG2**: Implement a full IDE (no editing, just viewing)
- **NG3**: Support non-git version control systems
- **NG4**: Cross-machine synchronization

---

## 3. Architecture

### 3.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         sidecar                                 │
│  ┌──────────┬──────────┬──────────┬──────────┬──────────────┐   │
│  │   Git    │  Files   │    TD    │  Convos  │  Worktrees   │   │
│  │  Plugin  │  Plugin  │  Plugin  │  Plugin  │   Plugin     │   │
│  └──────────┴──────────┴──────────┴──────────┴──────┬───────┘   │
└─────────────────────────────────────────────────────┼───────────┘
                                                      │
              ┌───────────────────────────────────────┼──────────┐
              │            Worktree Manager           │          │
              │  ┌─────────────────────────────────────────────┐ │
              │  │              WorktreeManager                │ │
              │  │  - worktrees: []*Worktree                   │ │
              │  │  - agents: map[string]*Agent                │ │
              │  │  - tdClient: *TDClient                      │ │
              │  └──────────────┬─────────────┬───────────────┘ │
              │                 │             │                  │
              │    ┌────────────┴─┐  ┌────────┴────────┐        │
              │    │  Git Ops     │  │  tmux Manager   │        │
              │    │  - add       │  │  - sessions     │        │
              │    │  - remove    │  │  - capture      │        │
              │    │  - list      │  │  - send-keys    │        │
              │    └──────────────┘  └─────────────────┘        │
              └──────────────────────────────────────────────────┘
                        │                     │
                        ▼                     ▼
              ┌─────────────────┐   ┌─────────────────────┐
              │   Git Repo      │   │   tmux Server       │
              │  .git/worktrees │   │  sidecar-wt-*       │
              └─────────────────┘   └─────────────────────┘
```

### 3.2 Package Structure

```
internal/plugins/worktree/
├── plugin.go           # Plugin interface implementation + lifecycle
├── model.go            # State types + helpers
├── view.go             # UI rendering (list view, preview pane)
├── view_kanban.go      # Kanban view
├── messages.go         # tea.Msg type definitions
├── worktree.go         # Git worktree operations
├── agent.go            # Agent process management
├── tmux.go             # tmux session management
├── td.go               # TD integration
├── config.go           # Configuration handling
├── status.go           # Status detection logic
└── types.go            # Shared types and constants

internal/keymap/bindings.go  # Add worktree plugin bindings
```

**Note:** No separate `keymap.go` file - use sidecar's Registry-based keymap system in `internal/keymap/bindings.go`.

### 3.3 Plugin Structure

The plugin must implement sidecar's Plugin interface:

```go
// Plugin implements the plugin.Plugin interface
type Plugin struct {
    // Required by plugin.Plugin interface
    ctx       *plugin.Context
    focused   bool
    width     int
    height    int

    // Worktree-specific state
    worktrees []*Worktree
    agents    map[string]*Agent

    // Session tracking for safe cleanup
    managedSessions map[string]bool  // session names we created

    // View state
    viewMode       ViewMode
    activePane     FocusPane
    selectedIdx    int
    scrollOffset   int              // Sidebar list scroll offset
    visibleCount   int              // Number of visible list items (computed in View)
    previewOffset  int
    sidebarWidth   int              // Persisted sidebar width (drag-to-resize)

    // Mouse support
    mouseHandler *mouse.Handler     // Hit regions, drag tracking (see Section 4.11)

    // Async state
    refreshing     bool
    lastRefresh    time.Time
    watcher        *fsnotify.Watcher
    stopChan       chan struct{}
}

// Required interface methods
func (p *Plugin) ID() string           { return "worktree-manager" }
func (p *Plugin) Name() string         { return "Worktrees" }
func (p *Plugin) Icon() string         { return "W" }
func (p *Plugin) IsFocused() bool      { return p.focused }
func (p *Plugin) SetFocused(f bool)    { p.focused = f }
func (p *Plugin) Commands() []plugin.Command { ... }  // See Section 4.5
func (p *Plugin) FocusContext() string { ... }        // See Section 4.6
```

### 3.4 Core Types

```go
// Worktree represents a git worktree with optional agent
type Worktree struct {
    Name       string          // e.g., "auth-oauth-flow"
    Path       string          // absolute path
    Branch     string          // git branch name
    BaseBranch string          // branch worktree was created from
    TaskID     string          // linked td task (e.g., "td-a1b2")
    Agent      *Agent          // nil if no agent running
    Status     WorktreeStatus  // derived from agent state
    Stats      *GitStats       // +/- line counts
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

type WorktreeStatus int

const (
    StatusPaused   WorktreeStatus = iota // No agent, worktree exists
    StatusActive                         // Agent running, recent output
    StatusWaiting                        // Agent waiting for input
    StatusDone                           // Agent completed task
    StatusError                          // Agent crashed or errored
)

// Agent represents an AI coding agent process
type Agent struct {
    Type        AgentType       // claude, codex, aider, gemini
    TmuxSession string          // tmux session name
    TmuxPane    string          // pane identifier
    PID         int             // process ID (if available)
    StartedAt   time.Time
    LastOutput  time.Time       // last time output was detected
    OutputBuf   *OutputBuffer   // last N lines of output (see 3.5)
    Status      AgentStatus
    WaitingFor  string          // prompt text if waiting
}

type AgentType string

const (
    AgentClaude AgentType = "claude"
    AgentCodex  AgentType = "codex"
    AgentAider  AgentType = "aider"
    AgentGemini AgentType = "gemini"
    AgentCustom AgentType = "custom"
)

// GitStats holds file change statistics
type GitStats struct {
    Additions    int
    Deletions    int
    FilesChanged int
    Ahead        int  // commits ahead of base branch
    Behind       int  // commits behind base branch
}
```

### 3.5 Output Buffer

A simple bounded buffer for agent output (no external dependency):

```go
// OutputBuffer is a thread-safe bounded buffer for agent output
type OutputBuffer struct {
    mu    sync.Mutex
    lines []string
    cap   int
}

func NewOutputBuffer(capacity int) *OutputBuffer {
    return &OutputBuffer{
        lines: make([]string, 0, capacity),
        cap:   capacity,
    }
}

func (b *OutputBuffer) Write(content string) {
    b.mu.Lock()
    defer b.mu.Unlock()

    newLines := strings.Split(content, "\n")
    b.lines = append(b.lines, newLines...)

    // Trim to capacity
    if len(b.lines) > b.cap {
        b.lines = b.lines[len(b.lines)-b.cap:]
    }
}

func (b *OutputBuffer) Lines() []string {
    b.mu.Lock()
    defer b.mu.Unlock()
    result := make([]string, len(b.lines))
    copy(result, b.lines)
    return result
}

func (b *OutputBuffer) String() string {
    return strings.Join(b.Lines(), "\n")
}
```

### 3.6 Message Types

All async operations communicate via typed messages (required for Bubble Tea):

```go
// Refresh cycle
type RefreshMsg struct{}
type RefreshDoneMsg struct {
    Worktrees []*Worktree
    Err       error
}

// File watching
type WatchEventMsg struct {
    Path string
}
type WatcherStartedMsg struct{}
type WatcherErrorMsg struct {
    Err error
}

// Agent output polling
type AgentOutputMsg struct {
    WorktreeName string
    Output       string
    Status       WorktreeStatus
    WaitingFor   string
}
type AgentStoppedMsg struct {
    WorktreeName string
    Err          error
}

// tmux attach/detach
type TmuxAttachFinishedMsg struct {
    WorktreeName string
    Err          error
}

// Diff loading
type DiffLoadedMsg struct {
    WorktreeName string
    Content      string
    Raw          string
}

// TD task operations
type TaskSearchResultsMsg struct {
    Tasks []*Task
    Err   error
}
type TaskLinkedMsg struct {
    WorktreeName string
    TaskID       string
    Err          error
}
```

---

## 4. User Interface Design

### 4.1 List View (Default)

The primary view shows worktrees in a split-pane layout:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ [`]Git [2]Files [3]TD [4]Convos [5]Worktrees                                │
├────────────────────────────────────────┬────────────────────────────────────┤
│ Worktrees                    [List|Kan]│ Output                  Diff  Task │
├────────────────────────────────────────┼────────────────────────────────────┤
│ ● auth-oauth-flow           💬  3m    │ Claude I'll implement the OAuth    │
│   claude  td-a1b2    +47 -12          │ 2.0 callback handler for your      │
│                                        │ authentication flow.               │
│ ○ payment-refactor          🟢  18m   │                                    │
│   codex   td-c3d4    +156 -34         │ Reading internal/auth/handler.go   │
│                                        │ Reading internal/config/oauth.go   │
│ ○ hotfix-login-timeout      ✅  42m   │                                    │
│   claude  td-e5f6    +8 -2            │ Claude I see the existing auth     │
│                                        │ structure. I'll add the OAuth      │
│ ○ ui-redesign-nav           ⏸  2h     │ callback endpoint that:            │
│   —       td-g7h8    +0 -0            │  1. Validates the state parameter  │
│                                        │  2. Exchanges the auth code        │
│                                        │  3. Creates the user session       │
│                                        │                                    │
│                                        │ Creating oauth_callback.go         │
│                                        │ Modifying routes.go                │
│                                        │                                    │
│                                        │ ┌──────────────────────────────┐   │
│                                        │ │ Allow edit to oauth_callback │   │
│                                        │ │ .go? (new file, 47 lines)    │   │
│                                        │ │                              │   │
│                                        │ │   [y] yes  [n] no  [e] view  │   │
│                                        │ └──────────────────────────────┘   │
│                                        │                                    │
├────────────────────────────────────────┴────────────────────────────────────┤
│ (Footer rendered by app from Commands() - plugin must NOT render footer)    │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Important:** The footer is rendered by the sidecar app, not the plugin. The plugin defines available commands via `Commands()` (see Section 4.5) and the app renders appropriate hints based on context.

**Status Indicators:**

- `🟢` / `●` Active (green) - Agent running, recent output
- `💬` / `○` Waiting (yellow, pulsing) - Agent needs input
- `✅` Done (cyan) - Agent completed or printed completion message
- `⏸` Paused (gray) - No agent running
- `❌` Error (red) - Agent crashed

### 4.2 Kanban View

Toggle with `v` key for column-based organization:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ [1]Git [2]Files [3]TD [4]Convos [5]Worktrees                                │
├────────────────────────────────────────────────────────────────────────────┤
│ Worktrees                                                      [List|Kanban]│
├─────────────────┬─────────────────┬─────────────────┬─────────────────────┤
│ 🟢 Active (2)   │ 💬 Waiting (1)  │ ✅ Ready (2)    │ ⏸ Paused (1)       │
├─────────────────┼─────────────────┼─────────────────┼─────────────────────┤
│ ┌─────────────┐ │ ┌─────────────┐ │ ┌─────────────┐ │ ┌─────────────────┐ │
│ │payment-refac│ │ │auth-oauth   │ │ │hotfix-login │ │ │ui-redesign-nav  │ │
│ │   codex     │ │ │   claude    │ │ │   claude    │ │ │                 │ │
│ │  td-c3d4    │ │ │  td-a1b2    │ │ │  td-e5f6    │ │ │  td-g7h8        │ │
│ │ +156 -34    │ │ │ +47 -12     │ │ │ +8 -2       │ │ │ +0 -0           │ │
│ └─────────────┘ │ │             │ │ └─────────────┘ │ └─────────────────┘ │
│ ┌─────────────┐ │ │⚡ oauth_call│ │ ┌─────────────┐ │                     │
│ │api-rate-lim │ │ │  back.go    │ │ │readme-update│ │                     │
│ │   claude    │ │ └─────────────┘ │ │   claude    │ │                     │
│ │  td-k9m2    │ │                 │ │  td-p4q5    │ │                     │
│ │ +23 -5      │ │                 │ │ +24 -8      │ │                     │
│ └─────────────┘ │                 │ └─────────────┘ │                     │
│                 │                 │                 │                     │
├─────────────────┴─────────────────┴─────────────────┴─────────────────────┤
│ (Footer rendered by app from Commands() - plugin must NOT render footer)  │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Important:** Columns represent **observed state**, not user-controlled state. Users cannot drag items between columns—status changes based on agent behavior.

**Note:** The kanban view may need a minimum width check. If terminal width is too narrow, auto-collapse to list view.

### 4.3 New Worktree Modal

Triggered by `n` key:

```
┌────────────────────────────────────────────────────────────────┐
│  New Worktree                                                  │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  Branch name                                                   │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ feature/user-preferences                                 │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                │
│  Link to TD task (optional)                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ user pref                                                │  │
│  └──────────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ td-x7y8  Add user preferences page                      ◀│  │
│  │ td-z9a1  User preference sync                            │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                │
│  Base branch                                                   │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ main                                                     │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                │
│  Agent                                                         │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐                   │
│  │◉ Claude│ │○ Codex │ │○ Aider │ │○ None  │                   │
│  └────────┘ └────────┘ └────────┘ └────────┘                   │
│                                                                │
│  Options                                                       │
│  ☑ Copy .env from main worktree                                │
│  ☑ Run setup script (.worktree-setup.sh)                       │
│  ☐ Symlink node_modules                                        │
│                                                                │
├────────────────────────────────────────────────────────────────┤
│                              Cancel (Esc)    Create (Enter)    │
└────────────────────────────────────────────────────────────────┘
```

### 4.4 Keyboard Shortcuts

**Note:** These shortcuts must be registered in `internal/keymap/bindings.go` with appropriate contexts. See Section 4.7 for binding definitions.

#### Global (handled by sidecar app, not plugin)

| Key        | Action          |
| ---------- | --------------- |
| `q`        | Quit sidecar    |
| `` ` ``    | Next plugin     |
| `~`        | Previous plugin |
| `1-9`      | Jump to plugin  |
| `?`        | Toggle help     |

#### Worktree List (context: `worktree-list`)

| Key      | Action                                |
| -------- | ------------------------------------- |
| `j`, `↓` | Next worktree                         |
| `k`, `↑` | Previous worktree                     |
| `n`      | New worktree                          |
| `N`      | New worktree with custom prompt       |
| `Enter`  | Attach to tmux session                |
| `y`      | Approve (send "y" to agent)           |
| `Y`      | Approve all pending                   |
| `Esc`    | Cancel / back                         |
| `d`      | View diff (toggle pane)               |
| `D`      | Delete worktree                       |
| `p`      | Push branch to remote                 |
| `P`      | Create pull request                   |
| `m`      | Merge workflow                        |
| `t`      | Link/unlink td task                   |
| `s`      | Resume/start agent on worktree        |
| `R`      | Refresh all                           |
| `/`      | Filter/search                         |
| `v`      | Toggle list/kanban view               |
| `Tab`    | Switch pane focus (list ↔ preview)    |
| `\`      | Toggle sidebar (collapse/expand)      |
| `h`      | Focus left pane (list)                |
| `l`      | Focus right pane (preview)            |

**Note:** Sidecar uses `r` for global refresh in root context. The worktree plugin uses `s` for resume/start to avoid conflict.

**Unified Sidebar Controls:** Follows the same pattern as Git, Files, and Conversations plugins:
- `Tab` switches focus between panes
- `\` collapses/expands the sidebar

#### When Attached to tmux

| Key      | Action                       |
| -------- | ---------------------------- |
| `Ctrl+b d` | Detach (default tmux binding) |

Users can also configure custom tmux bindings for detach.

### 4.5 Commands() Implementation

The plugin defines available commands for the app's footer rendering:

```go
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        // List view commands (shown in footer)
        {ID: "worktree-new", Name: "New", Context: "worktree-list", Priority: 1},
        {ID: "worktree-approve", Name: "Approve", Context: "worktree-list", Priority: 1},
        {ID: "worktree-attach", Name: "Attach", Context: "worktree-list", Priority: 2},
        {ID: "worktree-diff", Name: "Diff", Context: "worktree-list", Priority: 2},
        {ID: "worktree-push", Name: "Push", Context: "worktree-list", Priority: 3},
        {ID: "worktree-merge", Name: "Merge", Context: "worktree-list", Priority: 3},
        {ID: "worktree-delete", Name: "Delete", Context: "worktree-list", Priority: 4},
        {ID: "worktree-start-agent", Name: "Start", Context: "worktree-list", Priority: 2},
        {ID: "worktree-toggle-view", Name: "View", Context: "worktree-list", Priority: 4},
        {ID: "worktree-toggle-sidebar", Name: "Sidebar", Context: "worktree-list", Priority: 5},
        {ID: "worktree-focus-left", Name: "Left", Context: "worktree-list", Priority: 5},
        {ID: "worktree-focus-right", Name: "Right", Context: "worktree-list", Priority: 5},

        // List view commands (palette only - low priority)
        {ID: "worktree-new-with-prompt", Name: "NewPrompt", Context: "worktree-list", Priority: 5},
        {ID: "worktree-approve-all", Name: "ApproveAll", Context: "worktree-list", Priority: 5},
        {ID: "worktree-create-pr", Name: "PR", Context: "worktree-list", Priority: 4},
        {ID: "worktree-link-task", Name: "Task", Context: "worktree-list", Priority: 4},
        {ID: "worktree-filter", Name: "Filter", Context: "worktree-list", Priority: 5},
        {ID: "worktree-refresh", Name: "Refresh", Context: "worktree-list", Priority: 5},
        {ID: "worktree-pane-focus", Name: "Focus", Context: "worktree-list", Priority: 5},

        // Output pane commands
        {ID: "worktree-scroll-up", Name: "Up", Context: "worktree-output", Priority: 1},
        {ID: "worktree-scroll-down", Name: "Down", Context: "worktree-output", Priority: 1},

        // Diff pane commands
        {ID: "worktree-close-diff", Name: "Close", Context: "worktree-diff", Priority: 1},

        // Kanban view commands
        {ID: "worktree-list-view", Name: "List", Context: "worktree-kanban", Priority: 1},
        {ID: "worktree-column-left", Name: "Left", Context: "worktree-kanban", Priority: 2},
        {ID: "worktree-column-right", Name: "Right", Context: "worktree-kanban", Priority: 2},

        // Modal commands
        {ID: "worktree-cancel", Name: "Cancel", Context: "worktree-new-modal", Priority: 1},
        {ID: "worktree-confirm-create", Name: "Create", Context: "worktree-new-modal", Priority: 2},
        {ID: "worktree-close-modal", Name: "Close", Context: "worktree-new-modal", Priority: 1},
    }
}
```

**Note:** Keep command names short (1 word preferred) to prevent footer wrapping.

### 4.6 FocusContext() Implementation

Returns the current context for keymap binding selection:

```go
func (p *Plugin) FocusContext() string {
    switch p.viewMode {
    case ViewModeList:
        switch p.activePane {
        case PaneOutput:
            return "worktree-output"
        case PaneDiff:
            return "worktree-diff"
        case PaneTask:
            return "worktree-task"
        default:
            return "worktree-list"
        }
    case ViewModeKanban:
        return "worktree-kanban"
    case ViewModeNewModal:
        return "worktree-new-modal"
    case ViewModeConfirmDelete:
        return "worktree-confirm-delete"
    default:
        return "worktree-list"
    }
}
```

### 4.7 Keymap Bindings

Add to `internal/keymap/bindings.go`:

```go
// Worktree plugin bindings (list view)
{Key: "n", Command: "worktree-new", Context: "worktree-list"},
{Key: "N", Command: "worktree-new-with-prompt", Context: "worktree-list"},
{Key: "y", Command: "worktree-approve", Context: "worktree-list"},
{Key: "Y", Command: "worktree-approve-all", Context: "worktree-list"},
{Key: "enter", Command: "worktree-attach", Context: "worktree-list"},
{Key: "d", Command: "worktree-diff", Context: "worktree-list"},
{Key: "D", Command: "worktree-delete", Context: "worktree-list"},
{Key: "p", Command: "worktree-push", Context: "worktree-list"},
{Key: "P", Command: "worktree-create-pr", Context: "worktree-list"},
{Key: "m", Command: "worktree-merge", Context: "worktree-list"},
{Key: "t", Command: "worktree-link-task", Context: "worktree-list"},
{Key: "s", Command: "worktree-start-agent", Context: "worktree-list"},
{Key: "R", Command: "worktree-refresh", Context: "worktree-list"},
{Key: "/", Command: "worktree-filter", Context: "worktree-list"},
{Key: "v", Command: "worktree-toggle-view", Context: "worktree-list"},

// Unified sidebar controls (consistent with other plugins)
{Key: "tab", Command: "worktree-pane-focus", Context: "worktree-list"},
{Key: "\\", Command: "worktree-toggle-sidebar", Context: "worktree-list"},
{Key: "h", Command: "worktree-focus-left", Context: "worktree-list"},
{Key: "l", Command: "worktree-focus-right", Context: "worktree-list"},

// Modal bindings
{Key: "escape", Command: "worktree-close-modal", Context: "worktree-new-modal"},
{Key: "enter", Command: "worktree-confirm-create", Context: "worktree-new-modal"},

// Kanban bindings
{Key: "h", Command: "worktree-column-left", Context: "worktree-kanban"},
{Key: "l", Command: "worktree-column-right", Context: "worktree-kanban"},
{Key: "v", Command: "worktree-list-view", Context: "worktree-kanban"},
```

### 4.7.1 Root Context Handling

Add worktree contexts to `internal/app/update.go` in `isRootContext()`:

```go
func isRootContext(ctx string) bool {
    switch ctx {
    case "global", "",
        "conversations", "conversations-sidebar",
        "git-status", "git-status-commits", "git-status-diff",
        "file-browser-tree",
        "td-monitor",
        // Worktree root contexts (q = quit)
        "worktree-list", "worktree-kanban":
        return true
    }
    return false
}
```

**Root contexts** (q = quit): `worktree-list`, `worktree-kanban`
**Non-root contexts** (q = back): `worktree-output`, `worktree-diff`, `worktree-task`, `worktree-new-modal`, `worktree-confirm-delete`

### 4.7.2 Text Input Context Handling

The `worktree-new-modal` context has text input fields (branch name, task search). Add to `isTextInputContext()` in `internal/app/update.go`:

```go
func isTextInputContext(ctx string) bool {
    switch ctx {
    case "git-commit",
        "conversations-search",
        "file-browser-search", "file-browser-content-search",
        "file-browser-quick-open", "file-browser-project-search",
        "file-browser-file-op",
        "td-search",
        // Worktree text input contexts
        "worktree-new-modal", "worktree-filter":
        return true
    }
    return false
}
```

In text input contexts, letter keys are passed through to the input field instead of triggering shortcuts.

### 4.8 View() Height Constraint

**Critical:** Plugins must constrain output height to prevent the header from scrolling off-screen.

```go
func (p *Plugin) View(width, height int) string {
    p.width, p.height = width, height

    var content string
    switch p.viewMode {
    case ViewModeList:
        content = p.renderListView()
    case ViewModeKanban:
        content = p.renderKanbanView()
    }

    // Handle modals as overlays
    if p.viewMode == ViewModeNewModal {
        modal := p.renderNewWorktreeModal()
        content = ui.OverlayModal(content, modal, width, height)
    }

    // CRITICAL: Constrain to allocated height (prevents header scroll-off)
    return lipgloss.NewStyle().
        Width(width).
        Height(height).
        MaxHeight(height).
        Render(content)
}
```

### 4.9 Split Pane Implementation

Use sidecar's existing pattern from git plugin:

```go
func (p *Plugin) renderListView() string {
    // Calculate pane widths
    totalWidth := p.width
    listWidth := min(60, totalWidth/3)
    previewWidth := totalWidth - listWidth - 1 // -1 for border

    // Render each pane
    listPane := p.renderWorktreeList(listWidth, p.height)
    previewPane := p.renderPreviewPane(previewWidth, p.height)

    // Join with border
    border := strings.Repeat("│\n", p.height)
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        listPane,
        border,
        previewPane,
    )
}
```

### 4.10 Diff Rendering

Reuse the existing diff rendering from the git plugin for consistency:

```go
import "github.com/yourorg/sidecar/internal/plugins/gitstatus/diff"

func (p *Plugin) renderDiffPane(width, height int) string {
    if p.currentDiff == nil {
        return "No diff available"
    }

    // Use shared diff renderer (same as git plugin)
    return diff.Render(p.currentDiff.Raw, width, height, p.diffOffset)
}
```

**Note:** The diff renderer should be extracted to a shared package (e.g., `internal/ui/diff`) if not already available. See td-331dbf19 for paging implementation reference.

### 4.11 Mouse Support

The worktree plugin supports mouse interactions for list selection, scrolling, pane focus, and drag-to-resize. Follow the patterns in `internal/mouse` and `docs/guides/ui-feature-guide.md`.

#### 4.11.1 Handler Setup

```go
import "github.com/marcus/sidecar/internal/mouse"

func New(ctx *plugin.Context) *Plugin {
    return &Plugin{
        ctx:          ctx,
        mouseHandler: mouse.NewHandler(),
        // ...
    }
}
```

#### 4.11.2 Hit Region Constants

```go
const (
    // Pane regions (registered first = lowest priority)
    regionSidebar     = "sidebar"
    regionPreviewPane = "preview-pane"
    regionPaneDivider = "pane-divider"

    // Item regions (registered last = highest priority)
    regionWorktreeItem = "worktree-item"

    dividerWidth    = 1  // Visual divider width
    dividerHitWidth = 3  // Wider hit target for easier clicking
)
```

#### 4.11.3 Handling MouseMsg in Update

```go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        return p.handleMouse(msg)
    // ... other cases
    }
    return p, nil
}

func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
    action := p.mouseHandler.HandleMouse(msg)

    switch action.Type {
    case mouse.ActionClick:
        return p.handleMouseClick(action)
    case mouse.ActionDoubleClick:
        return p.handleMouseDoubleClick(action)
    case mouse.ActionDrag:
        return p.handleMouseDrag(action)
    case mouse.ActionDragEnd:
        return p.handleMouseDragEnd()
    case mouse.ActionScrollUp, mouse.ActionScrollDown:
        return p.handleMouseScroll(action)
    }
    return p, nil
}
```

#### 4.11.4 Region Registration (Critical: Order Matters)

Regions are tested in **reverse order** - last added = checked first. Register pane regions first (fallback), then item regions last (priority):

```go
func (p *Plugin) View(width, height int) string {
    p.width, p.height = width, height

    // CRITICAL: Clear hit regions at start of each render
    p.mouseHandler.HitMap.Clear()

    // Calculate pane widths
    sidebarW := p.sidebarWidth
    if sidebarW == 0 {
        sidebarW = width * 30 / 100  // Default 30%
    }
    previewW := width - sidebarW - dividerWidth

    // ... render panes ...

    // Register regions in priority order (last = highest priority)

    // 1. Pane regions (lowest priority - fallback for scroll)
    p.mouseHandler.HitMap.AddRect(regionSidebar, 0, 0, sidebarW, height, nil)
    p.mouseHandler.HitMap.AddRect(regionPreviewPane, sidebarW+dividerWidth, 0, previewW, height, nil)

    // 2. Divider region (medium priority - for drag)
    p.mouseHandler.HitMap.AddRect(regionPaneDivider, sidebarW, 0, dividerHitWidth, height, nil)

    // 3. Item regions (highest priority - registered LAST)
    // These are registered during sidebar rendering (see 4.11.5)

    return content
}
```

#### 4.11.5 Registering Item Regions During List Render

```go
func (p *Plugin) renderWorktreeList(width, visibleHeight int) string {
    var sb strings.Builder
    currentY := 0  // Track Y position for hit regions

    // Header line
    sb.WriteString(headerStyle.Render("Worktrees"))
    currentY++

    // Render visible worktrees
    startIdx := p.scrollOffset
    endIdx := min(startIdx+visibleHeight-1, len(p.worktrees))

    for i := startIdx; i < endIdx; i++ {
        wt := p.worktrees[i]
        line := p.renderWorktreeLine(wt, width, i == p.selectedIdx)
        sb.WriteString(line)
        sb.WriteString("\n")

        // Register hit region with ABSOLUTE index (not visible index)
        p.mouseHandler.HitMap.AddRect(
            regionWorktreeItem,
            0,           // x
            currentY,    // y (tracks rendered position)
            width,       // width
            1,           // height (single line)
            i,           // data: absolute worktree index
        )
        currentY++
    }

    return sb.String()
}
```

#### 4.11.6 Common Mouse Patterns

**Click to select worktree:**

```go
func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    if action.Region == nil {
        return p, nil
    }

    switch action.Region.ID {
    case regionWorktreeItem:
        if idx, ok := action.Region.Data.(int); ok {
            p.selectedIdx = idx
            p.ensureCursorVisible()
        }

    case regionSidebar:
        p.activePane = PaneSidebar

    case regionPreviewPane:
        p.activePane = PanePreview

    case regionPaneDivider:
        // Start drag - store current width as initial value
        p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
    }

    return p, nil
}
```

**Double-click to attach:**

```go
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    if action.Region == nil || action.Region.ID != regionWorktreeItem {
        return p, nil
    }

    if idx, ok := action.Region.Data.(int); ok {
        p.selectedIdx = idx
        return p, p.attachToSession()
    }
    return p, nil
}
```

**Scroll wheel:**

```go
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    // Include item regions in scroll routing (items have higher priority than panes)
    switch action.Region.ID {
    case regionSidebar, regionWorktreeItem:
        p.scrollOffset += action.Delta
        p.clampScroll()

    case regionPreviewPane:
        p.previewOffset += action.Delta
        p.clampPreviewScroll()
    }
    return p, nil
}
```

**See Section 4.13 for drag-to-resize implementation.**

### 4.12 Sidebar List Implementation

The worktree list follows the patterns in `sidebar-list-guide.md` to ensure stable scrolling and correct rendering.

#### 4.12.1 Layout Accounting

Compute pane height once and pass it down. Never double-count headers or borders:

```go
func (p *Plugin) View(width, height int) string {
    p.width, p.height = width, height

    // Calculate available content height (minus panel borders)
    paneHeight := height - 2  // Panel top/bottom border

    // Sidebar header takes 1 line
    sidebarContentHeight := paneHeight - 1

    // Use same height for scroll clamping and rendering
    p.visibleCount = sidebarContentHeight
    p.clampScroll()

    return p.renderSplitPane(width, paneHeight)
}
```

#### 4.12.2 Scroll and Cursor Stability

```go
// Clamp scroll offset after any data change
func (p *Plugin) clampScroll() {
    maxScroll := len(p.worktrees) - p.visibleCount
    if maxScroll < 0 {
        maxScroll = 0
    }
    if p.scrollOffset > maxScroll {
        p.scrollOffset = maxScroll
    }
    if p.scrollOffset < 0 {
        p.scrollOffset = 0
    }
}

// Ensure selected item is visible
func (p *Plugin) ensureCursorVisible() {
    if p.selectedIdx < p.scrollOffset {
        p.scrollOffset = p.selectedIdx
    } else if p.selectedIdx >= p.scrollOffset+p.visibleCount {
        p.scrollOffset = p.selectedIdx - p.visibleCount + 1
    }
}
```

**Critical rules:**

- Never mutate scroll offsets inside `View()` - rendering must be pure
- Always clamp after data changes (refresh, filter, add, remove)
- Use absolute indices for hit regions, not visible indices
- Preserve selection by worktree name (stable ID), not index

#### 4.12.3 Async Refresh Safety

When refreshing the worktree list, preserve user position:

```go
func (p *Plugin) handleRefreshDone(msg RefreshDoneMsg) (*Plugin, tea.Cmd) {
    if msg.Err != nil {
        p.refreshing = false
        return p, nil
    }

    // Preserve selection by stable ID (worktree name)
    selectedName := ""
    if p.selectedIdx < len(p.worktrees) {
        selectedName = p.worktrees[p.selectedIdx].Name
    }

    p.worktrees = msg.Worktrees

    // Restore selection by name
    for i, wt := range p.worktrees {
        if wt.Name == selectedName {
            p.selectedIdx = i
            break
        }
    }

    // Clamp in case list shrunk
    if p.selectedIdx >= len(p.worktrees) {
        p.selectedIdx = len(p.worktrees) - 1
    }
    if p.selectedIdx < 0 {
        p.selectedIdx = 0
    }

    p.clampScroll()
    p.ensureCursorVisible()
    p.refreshing = false
    return p, nil
}
```

#### 4.12.4 Pitfalls Checklist

- [ ] Double-counted header lines or borders
- [ ] Scroll offsets adjusted inside render methods
- [ ] Refresh replaced list without preserving selection
- [ ] No clamp after list size changes
- [ ] Hit regions using visible index instead of absolute index
- [ ] Headers wrapped instead of truncated (steals height)

### 4.13 Drag-to-Resize Pane

Enable users to drag the pane divider to resize the sidebar. Follow the patterns in `drag-pane-implementation-guide.md`.

#### 4.13.1 State Persistence

Add sidebar width to plugin state:

```go
// In internal/state/state.go
type State struct {
    // ... existing fields
    WorktreeSidebarWidth int `json:"worktreeSidebarWidth,omitempty"`
}

func GetWorktreeSidebarWidth() int {
    mu.RLock()
    defer mu.RUnlock()
    if current == nil {
        return 0
    }
    return current.WorktreeSidebarWidth
}

func SetWorktreeSidebarWidth(width int) error {
    mu.Lock()
    if current == nil {
        current = &State{}
    }
    current.WorktreeSidebarWidth = width
    mu.Unlock()
    return Save()
}
```

#### 4.13.2 Load Persisted Width in Init

```go
func (p *Plugin) Init(ctx *plugin.Context) error {
    // Load persisted sidebar width
    if savedWidth := state.GetWorktreeSidebarWidth(); savedWidth > 0 {
        p.sidebarWidth = savedWidth
    }
    // ... rest of init
    return nil
}
```

#### 4.13.3 Handle Drag Events

```go
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
    if p.mouseHandler.DragRegion() != regionPaneDivider {
        return p, nil
    }

    // Calculate new width from drag delta
    startValue := p.mouseHandler.DragStartValue()
    newWidth := startValue + action.DragDX

    // Clamp to bounds
    available := p.width - 5 - dividerWidth
    minWidth := 25
    maxWidth := available - 40

    if newWidth < minWidth {
        newWidth = minWidth
    } else if newWidth > maxWidth {
        newWidth = maxWidth
    }

    p.sidebarWidth = newWidth
    return p, nil
}

func (p *Plugin) handleMouseDragEnd() (*Plugin, tea.Cmd) {
    // Persist width on drag end
    _ = state.SetWorktreeSidebarWidth(p.sidebarWidth)
    return p, nil
}
```

#### 4.13.4 Critical Rules

**Rule 1: Never reset width in View()**

```go
// WRONG - overwrites drag changes on every render!
func (p *Plugin) View(width, height int) string {
    p.sidebarWidth = width * 30 / 100  // BUG!
    // ...
}

// CORRECT - only set default if not initialized
func (p *Plugin) View(width, height int) string {
    if p.sidebarWidth == 0 {
        p.sidebarWidth = width * 30 / 100  // Default only once
    }
    // ... clamp to current bounds
}
```

**Rule 2: Correct divider X coordinate**

The divider X position = `sidebarWidth`, not `sidebarWidth + borderWidth`:

```go
// CORRECT: Divider starts at column sidebarWidth
dividerX := sidebarWidth
p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, 0, dividerHitWidth, height, nil)
```

**Rule 3: Region priority**

The divider region MUST be registered AFTER pane regions so it takes priority:

```go
// CORRECT ORDER:
p.mouseHandler.HitMap.AddRect(regionSidebar, ...)       // Lowest priority
p.mouseHandler.HitMap.AddRect(regionPreviewPane, ...)   // Medium priority
p.mouseHandler.HitMap.AddRect(regionPaneDivider, ...)   // HIGHEST (last)
```

**Rule 4: Use wider hit target**

```go
dividerHitWidth := 3  // Wider than visual 1-char divider for easier clicking
```

#### 4.13.5 Visual Divider

```go
func (p *Plugin) renderDivider(height int) string {
    dividerStyle := lipgloss.NewStyle().
        Foreground(styles.BorderNormal).
        MarginTop(1)  // Align with pane content

    var sb strings.Builder
    for i := 0; i < height; i++ {
        sb.WriteString("│")
        if i < height-1 {
            sb.WriteString("\n")
        }
    }
    return dividerStyle.Render(sb.String())
}
```

---

## 5. Git Worktree Operations

### 5.1 Worktree Location Strategy

By default, worktrees are created as siblings to the main repository:

```
~/code/
├── sidecar/                    # Main repository
│   ├── .git/
│   │   └── worktrees/          # Git's worktree metadata
│   │       ├── auth-oauth-flow/
│   │       └── payment-refactor/
│   └── [source files]
│
└── sidecar-worktrees/          # Actual worktree directories
    ├── auth-oauth-flow/
    │   ├── .git                # File pointing to main .git
    │   ├── .td-root            # Points to ~/code/sidecar
    │   └── [source files]
    │
    └── payment-refactor/
        ├── .git
        ├── .td-root
        └── [source files]
```

**Rationale:**

- Keeps worktrees separate from main repo (avoids accidental commits)
- Easy to find and navigate
- Consistent with Claude Squad's default pattern
- Configurable via `worktreeDir` setting

**Directory Writability:**

If the parent directory is not writable, the plugin should fail with a clear error message instructing the user to configure `worktreeDir` manually:

```go
func (m *WorktreeManager) validateWorktreeDir() error {
    parentDir := filepath.Dir(m.worktreeDir)

    // Check if parent exists and is writable
    if err := os.MkdirAll(m.worktreeDir, 0755); err != nil {
        return fmt.Errorf(
            "cannot create worktree directory %s: %w\n"+
            "Configure 'worktreeDir' in sidecar config to use a different location",
            m.worktreeDir, err,
        )
    }

    return nil
}
```

### 5.2 Creating a Worktree

```go
// CreateWorktree creates a new git worktree
func (m *WorktreeManager) CreateWorktree(opts CreateOptions) (*Worktree, error) {
    // Validate branch name
    if !isValidBranchName(opts.Branch) {
        return nil, fmt.Errorf("invalid branch name: %s", opts.Branch)
    }

    // Check if branch already exists
    branchExists, err := m.branchExists(opts.Branch)
    if err != nil {
        return nil, err
    }

    // Determine worktree path
    worktreePath := filepath.Join(m.worktreeDir, opts.Branch)

    // Build git worktree add command
    args := []string{"worktree", "add"}

    if branchExists {
        // Checkout existing branch
        args = append(args, worktreePath, opts.Branch)
    } else {
        // Create new branch from base
        args = append(args, "-b", opts.Branch, worktreePath)
        if opts.BaseBranch != "" {
            args = append(args, opts.BaseBranch)
        }
    }

    // Execute
    cmd := exec.Command("git", args...)
    cmd.Dir = m.repoRoot
    if output, err := cmd.CombinedOutput(); err != nil {
        return nil, fmt.Errorf("git worktree add failed: %s: %w", output, err)
    }

    // Post-creation setup
    if err := m.setupWorktree(worktreePath, opts); err != nil {
        // Cleanup on failure
        m.removeWorktreeDir(worktreePath)
        return nil, err
    }

    return &Worktree{
        Name:       opts.Branch,
        Path:       worktreePath,
        Branch:     opts.Branch,
        BaseBranch: opts.BaseBranch,
        TaskID:     opts.TaskID,
        Status:     StatusPaused,
        CreatedAt:  time.Now(),
    }, nil
}
```

**Git commands used:**

```bash
# Create new branch and worktree
git worktree add -b feature/auth ../sidecar-worktrees/feature-auth main

# Create worktree for existing branch
git worktree add ../sidecar-worktrees/feature-auth feature/auth

# List worktrees (porcelain format for parsing)
git worktree list --porcelain

# Example porcelain output:
# worktree /Users/dev/code/sidecar
# HEAD abc123def456
# branch refs/heads/main
#
# worktree /Users/dev/code/sidecar-worktrees/feature-auth
# HEAD def456abc123
# branch refs/heads/feature/auth
```

### 5.3 Removing a Worktree

```go
// RemoveWorktree removes a worktree and optionally its branch
func (m *WorktreeManager) RemoveWorktree(name string, opts RemoveOptions) error {
    wt, err := m.GetWorktree(name)
    if err != nil {
        return err
    }

    // Stop agent if running
    if wt.Agent != nil {
        if err := m.StopAgent(wt); err != nil {
            return fmt.Errorf("failed to stop agent: %w", err)
        }
    }

    // Check for uncommitted changes
    if !opts.Force {
        if dirty, err := m.isWorktreeDirty(wt.Path); err != nil {
            return err
        } else if dirty {
            return fmt.Errorf("worktree has uncommitted changes (use --force to override)")
        }
    }

    // Remove worktree
    args := []string{"worktree", "remove"}
    if opts.Force {
        args = append(args, "--force")
    }
    args = append(args, wt.Path)

    cmd := exec.Command("git", args...)
    cmd.Dir = m.repoRoot
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git worktree remove failed: %s: %w", output, err)
    }

    // Optionally delete branch
    if opts.DeleteBranch {
        args := []string{"branch", "-d", wt.Branch}
        if opts.Force {
            args[1] = "-D"
        }
        cmd := exec.Command("git", args...)
        cmd.Dir = m.repoRoot  // Must run in repo root
        cmd.Run() // Best effort
    }

    return nil
}
```

**Git commands used:**

```bash
# Remove worktree (fails if dirty)
git worktree remove ../sidecar-worktrees/feature-auth

# Force remove (even if dirty)
git worktree remove --force ../sidecar-worktrees/feature-auth

# Prune stale worktree entries (after manual directory deletion)
git worktree prune

# Delete branch after worktree removal
git branch -d feature/auth    # Safe delete (only if merged)
git branch -D feature/auth    # Force delete
```

### 5.4 Listing Worktrees

```go
// ListWorktrees returns all worktrees with their status
func (m *WorktreeManager) ListWorktrees() ([]*Worktree, error) {
    cmd := exec.Command("git", "worktree", "list", "--porcelain")
    cmd.Dir = m.repoRoot
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    worktrees := parseWorktreeList(output)

    // Enrich with agent status and td task info
    for _, wt := range worktrees {
        // Skip main worktree
        if wt.Path == m.repoRoot {
            continue
        }

        // Check for running agent
        if agent, ok := m.agents[wt.Name]; ok {
            wt.Agent = agent
            wt.Status = agent.DeriveStatus()
        }

        // Load td task link
        wt.TaskID = m.loadTaskLink(wt.Path)

        // Get git stats
        wt.Stats = m.getGitStats(wt.Path)
    }

    return worktrees, nil
}
```

### 5.5 Getting Diff Statistics

```go
// getGitStats returns line change statistics for a worktree
func (m *WorktreeManager) getGitStats(wt *Worktree) *GitStats {
    stats := &GitStats{}

    // Get diff stat against base branch
    cmd := exec.Command("git", "diff", "--shortstat", "HEAD")
    cmd.Dir = wt.Path
    output, err := cmd.Output()
    if err == nil {
        // Parse: " 3 files changed, 47 insertions(+), 12 deletions(-)"
        stats.parseShortstat(string(output))
    }

    // Get ahead/behind counts relative to base branch (not hardcoded main)
    baseBranch := wt.BaseBranch
    if baseBranch == "" {
        baseBranch = "main" // Fallback if not set
    }
    cmd = exec.Command("git", "rev-list", "--left-right", "--count",
        fmt.Sprintf("%s...HEAD", baseBranch))
    cmd.Dir = wt.Path
    output, err = cmd.Output()
    if err == nil {
        // Parse: "5\t3" (behind, ahead)
        fmt.Sscanf(string(output), "%d\t%d", &stats.Behind, &stats.Ahead)
    }

    return stats
}
```

**Git commands used:**

```bash
# Get diff statistics
git diff --shortstat HEAD
# Output: 3 files changed, 47 insertions(+), 12 deletions(-)

# Get ahead/behind count relative to main
git rev-list --left-right --count main...HEAD
# Output: 5    3  (5 behind, 3 ahead)

# Get list of changed files
git diff --name-only HEAD
```

### 5.6 Safety: Branch Already Checked Out

Git prevents checking out a branch in multiple worktrees. Handle this case:

```go
func (m *WorktreeManager) CreateWorktree(opts CreateOptions) (*Worktree, error) {
    // ... validation ...

    cmd := exec.Command("git", "worktree", "add", "-b", opts.Branch, worktreePath)
    output, err := cmd.CombinedOutput()

    if err != nil {
        if strings.Contains(string(output), "already checked out") {
            return nil, &BranchCheckedOutError{
                Branch:   opts.Branch,
                Location: extractLocation(string(output)),
            }
        }
        return nil, err
    }

    // ...
}
```

Error message example:

```
fatal: 'feature/auth' is already checked out at '/Users/dev/code/sidecar-worktrees/feature-auth'
```

---

## 6. tmux Integration

### 6.1 Session Naming Convention

tmux sessions created by the worktree manager follow this pattern:

```
sidecar-wt-{worktree-name}
```

Example: `sidecar-wt-auth-oauth-flow`

This allows:

- Easy identification of sidecar-managed sessions
- Cleanup on sidecar exit
- Avoiding conflicts with user sessions

### 6.2 Creating a tmux Session

```go
// StartAgent starts an AI agent in a new tmux session
func (m *WorktreeManager) StartAgent(wt *Worktree, agentType AgentType) error {
    sessionName := fmt.Sprintf("sidecar-wt-%s", sanitizeName(wt.Name))

    // Check if session already exists
    checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
    if checkCmd.Run() == nil {
        return fmt.Errorf("session %s already exists", sessionName)
    }

    // Get agent command
    agentCmd := m.getAgentCommand(agentType, wt)

    // Create new detached session
    args := []string{
        "new-session",
        "-d",                    // Detached
        "-s", sessionName,       // Session name
        "-c", wt.Path,           // Working directory
    }

    // Optionally set history limit
    // This is done after session creation to avoid tmux version issues

    cmd := exec.Command("tmux", args...)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to create tmux session: %w", err)
    }

    // Set history limit for scrollback capture
    exec.Command("tmux", "set-option", "-t", sessionName, "history-limit", "10000").Run()

    // Enable mouse mode (optional, for scrolling)
    exec.Command("tmux", "set-option", "-t", sessionName, "mouse", "on").Run()

    // Send the agent command
    sendCmd := exec.Command("tmux", "send-keys", "-t", sessionName, agentCmd, "Enter")
    if err := sendCmd.Run(); err != nil {
        return fmt.Errorf("failed to start agent: %w", err)
    }

    // Create agent record
    agent := &Agent{
        Type:        agentType,
        TmuxSession: sessionName,
        StartedAt:   time.Now(),
        OutputBuf:   NewOutputBuffer(500), // Last 500 lines
    }

    wt.Agent = agent
    p.agents[wt.Name] = agent

    // Track session for safe cleanup (only cleanup sessions we created)
    p.managedSessions[sessionName] = true

    return nil
}

// After StartAgent, return a command to begin polling:
func (p *Plugin) startAgentPolling(wt *Worktree) tea.Cmd {
    return p.scheduleAgentPoll(wt.Name, 500*time.Millisecond) // Initial poll faster
}
```

**tmux commands used:**

```bash
# Create detached session with working directory
tmux new-session -d -s "sidecar-wt-auth-feature" -c "/path/to/worktree"

# Set history limit for scrollback
tmux set-option -t "sidecar-wt-auth-feature" history-limit 10000

# Enable mouse mode
tmux set-option -t "sidecar-wt-auth-feature" mouse on

# Send command to start agent
tmux send-keys -t "sidecar-wt-auth-feature" "claude" Enter

# Check if session exists
tmux has-session -t "sidecar-wt-auth-feature"
```

### 6.3 Capturing Agent Output

**Critical:** In Bubble Tea, only the `Update` function can mutate the model. Background goroutines must return `tea.Msg`s. Direct mutation causes race conditions and UI rendering glitches.

This is the core mechanism for displaying live agent output in the TUI using the proper Bubble Tea command pattern:

```go
// scheduleAgentPoll returns a command that polls agent output after a delay
func (p *Plugin) scheduleAgentPoll(worktreeName string, delay time.Duration) tea.Cmd {
    return tea.Tick(delay, func(t time.Time) tea.Msg {
        return pollAgentMsg{WorktreeName: worktreeName}
    })
}

// pollAgentMsg triggers a poll for a specific worktree
type pollAgentMsg struct {
    WorktreeName string
}

// handlePollAgent captures output and returns an AgentOutputMsg
func (p *Plugin) handlePollAgent(worktreeName string) tea.Cmd {
    return func() tea.Msg {
        wt := p.findWorktree(worktreeName)
        if wt == nil || wt.Agent == nil {
            return AgentStoppedMsg{WorktreeName: worktreeName}
        }

        output, err := p.capturePane(wt.Agent.TmuxSession)
        if err != nil {
            // Session may have been killed
            if strings.Contains(err.Error(), "can't find") {
                return AgentStoppedMsg{WorktreeName: worktreeName}
            }
            // Schedule retry
            return pollAgentMsg{WorktreeName: worktreeName}
        }

        // Detect status from output (pure function, no state mutation)
        status := detectStatus(output)
        waitingFor := ""
        if status == StatusWaiting {
            waitingFor = extractPrompt(output)
        }

        return AgentOutputMsg{
            WorktreeName: worktreeName,
            Output:       output,
            Status:       status,
            WaitingFor:   waitingFor,
        }
    }
}

// In Update(), handle the messages:
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case pollAgentMsg:
        // Check if we should skip polling (user attached to session)
        if p.attachedSession == msg.WorktreeName {
            // Pause polling while user is attached
            return p, nil
        }
        return p, p.handlePollAgent(msg.WorktreeName)

    case AgentOutputMsg:
        // Update state here (safe - we're in Update)
        if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
            wt.Agent.OutputBuf.Write(msg.Output)
            wt.Agent.LastOutput = time.Now()
            wt.Agent.WaitingFor = msg.WaitingFor
            wt.Status = msg.Status
        }
        // Schedule next poll (1 second interval)
        return p, p.scheduleAgentPoll(msg.WorktreeName, 1*time.Second)

    case AgentStoppedMsg:
        if wt := p.findWorktree(msg.WorktreeName); wt != nil {
            wt.Agent = nil
            wt.Status = StatusPaused
        }
        return p, nil
    }
    // ...
}

// capturePane gets the last N lines from a tmux pane
func (p *Plugin) capturePane(sessionName string) (string, error) {
    // -p: Print to stdout (instead of buffer)
    // -S: Start line (-200 = last 200 lines)
    // -t: Target session
    cmd := exec.Command("tmux", "capture-pane", "-p", "-S", "-200", "-t", sessionName)
    output, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("capture-pane failed: %w", err)
    }
    return string(output), nil
}
```

**tmux capture-pane options:**

```bash
# Capture last 200 lines, print to stdout
tmux capture-pane -p -S -200 -t "sidecar-wt-auth-feature"

# Capture entire scrollback history
tmux capture-pane -p -S - -t "sidecar-wt-auth-feature"

# Options:
#   -p          Print output to stdout (instead of paste buffer)
#   -S <line>   Starting line number (negative = from end, - = beginning)
#   -E <line>   Ending line number (default: visible bottom)
#   -t <target> Target session:window.pane
#   -e          Include escape sequences (colors)
```

**Reference implementation:** See Claude Squad's [session/tmux.go](https://github.com/smtg-ai/claude-squad/blob/main/session/tmux.go) for production-tested capture logic.

### 6.4 Sending Input to Agents

```go
// SendKeys sends keystrokes to the agent's tmux session
func (m *WorktreeManager) SendKeys(wt *Worktree, keys string) error {
    if wt.Agent == nil {
        return fmt.Errorf("no agent running in worktree %s", wt.Name)
    }

    cmd := exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, keys)
    return cmd.Run()
}

// Approve sends "y" followed by Enter to approve a prompt
func (m *WorktreeManager) Approve(wt *Worktree) error {
    return m.SendKeys(wt, "y\n")
    // Or: return m.SendKeys(wt, "y", "Enter") with separate args
}

// Reject sends "n" followed by Enter
func (m *WorktreeManager) Reject(wt *Worktree) error {
    return m.SendKeys(wt, "n\n")
}

// SendText sends arbitrary text (e.g., custom prompts)
func (m *WorktreeManager) SendText(wt *Worktree, text string) error {
    // Use -l to send literal text (no key name lookup)
    cmd := exec.Command("tmux", "send-keys", "-l", "-t", wt.Agent.TmuxSession, text)
    if err := cmd.Run(); err != nil {
        return err
    }
    // Send Enter separately
    return exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "Enter").Run()
}
```

**tmux send-keys options:**

```bash
# Send "y" and press Enter
tmux send-keys -t "sidecar-wt-auth-feature" "y" Enter

# Send literal text (no key name interpretation)
tmux send-keys -l -t "sidecar-wt-auth-feature" "This is my prompt"

# Special keys: Enter, Escape, Space, Tab, Up, Down, Left, Right
# Ctrl+key: C-c, C-d, C-z
# Alt+key: M-a, M-x

# Send Ctrl+C to interrupt
tmux send-keys -t "sidecar-wt-auth-feature" C-c
```

### 6.5 Attaching to tmux Sessions

When the user presses Enter on a worktree, sidecar should suspend itself and attach to the tmux session:

```go
// AttachToSession suspends sidecar and attaches to the agent's tmux session
func (p *Plugin) AttachToSession(wt *Worktree) tea.Cmd {
    if wt.Agent == nil {
        return nil
    }

    // Track that we're attached (pauses polling - see Section 6.3)
    p.attachedSession = wt.Name

    // Use tea.ExecProcess to suspend Bubble Tea and run tmux attach
    c := exec.Command("tmux", "attach-session", "-t", wt.Agent.TmuxSession)

    return tea.ExecProcess(c, func(err error) tea.Msg {
        return TmuxAttachFinishedMsg{WorktreeName: wt.Name, Err: err}
    })
}

// In Update(), handle attach completion:
case TmuxAttachFinishedMsg:
    // Clear attached state
    p.attachedSession = ""
    // Resume polling and refresh to capture what happened while attached
    var cmds []tea.Cmd
    if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
        cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, 0))
    }
    cmds = append(cmds, p.refresh())
    return p, tea.Batch(cmds...)
```

**Polling Pause During Attach:**

When the user attaches to a tmux session, polling is paused for that agent. This reduces CPU/IO during interactive use. Polling resumes automatically when the user detaches (detected via `TmuxAttachFinishedMsg`).

**In BubbleTea, `tea.ExecProcess` handles:**

1. Suspending the TUI
2. Restoring terminal state
3. Running the external command
4. Restoring TUI when command exits

The user can detach from tmux using `Ctrl+b d` (default tmux binding).

**Reference:** See [BubbleTea ExecProcess documentation](https://pkg.go.dev/github.com/charmbracelet/bubbletea#ExecProcess)

### 6.6 Cleanup on Exit

```go
// Cleanup stops all agents and optionally removes tmux sessions
func (p *Plugin) Cleanup(removeSessions bool) error {
    for name, agent := range p.agents {
        if removeSessions {
            // Only kill sessions we created (tracked in managedSessions)
            if p.managedSessions[agent.TmuxSession] {
                exec.Command("tmux", "kill-session", "-t", agent.TmuxSession).Run()
                delete(p.managedSessions, agent.TmuxSession)
            }
        }

        delete(p.agents, name)
    }
    return nil
}

// CleanupOrphanedSessions removes sidecar-wt-* sessions that we created
// but no longer have corresponding worktrees
func (p *Plugin) CleanupOrphanedSessions() error {
    cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
    output, err := cmd.Output()
    if err != nil {
        return nil // No tmux server running
    }

    for _, session := range strings.Split(string(output), "\n") {
        session = strings.TrimSpace(session)
        if session == "" {
            continue
        }

        // Only cleanup sessions we explicitly created and tracked
        // This prevents accidentally killing user sessions named sidecar-wt-*
        if !p.managedSessions[session] {
            continue
        }

        // Check if corresponding worktree still exists
        worktreeName := strings.TrimPrefix(session, "sidecar-wt-")
        if p.findWorktree(worktreeName) == nil {
            exec.Command("tmux", "kill-session", "-t", session).Run()
            delete(p.managedSessions, session)
        }
    }
    return nil
}
```

**Session Tracking Safety:**

By tracking session IDs in `managedSessions`, we ensure:
1. Only sessions created by this sidecar instance are cleaned up
2. User sessions that happen to match `sidecar-wt-*` naming are never killed
3. Sessions persist across sidecar restarts for resume functionality

**tmux commands used:**

```bash
# List all sessions
tmux list-sessions -F "#{session_name}"

# Kill a specific session
tmux kill-session -t "sidecar-wt-auth-feature"

# Kill all sidecar sessions (bash)
tmux list-sessions -F "#{session_name}" | grep "^sidecar-wt-" | xargs -I{} tmux kill-session -t {}
```

---

## 7. Agent Status Detection

### 7.1 Status Detection Patterns

Detecting whether an agent is waiting for input requires parsing the captured output:

```go
// detectStatus analyzes output to determine agent status
func (m *WorktreeManager) detectStatus(output string) WorktreeStatus {
    lines := strings.Split(output, "\n")

    // Check last ~10 lines for patterns
    checkLines := lines
    if len(lines) > 10 {
        checkLines = lines[len(lines)-10:]
    }
    text := strings.Join(checkLines, "\n")

    // Waiting patterns (agent needs user input)
    waitingPatterns := []string{
        "[Y/n]",           // Claude Code permission prompt
        "[y/N]",           // Alternate capitalization
        "(y/n)",           // Aider style
        "? (Y/n)",         // Interactive prompt
        "Allow edit",      // Claude Code file edit
        "Allow bash",      // Claude Code bash command
        "waiting for",     // Generic waiting
        "Press enter",     // Continue prompt
        "Continue?",
    }

    for _, pattern := range waitingPatterns {
        if strings.Contains(strings.ToLower(text), strings.ToLower(pattern)) {
            return StatusWaiting
        }
    }

    // Done patterns (agent completed)
    donePatterns := []string{
        "Task completed",
        "All done",
        "Finished",
        "exited with code 0",
    }

    for _, pattern := range donePatterns {
        if strings.Contains(text, pattern) {
            return StatusDone
        }
    }

    // Error patterns
    errorPatterns := []string{
        "error:",
        "Error:",
        "failed",
        "exited with code 1",
        "panic:",
    }

    for _, pattern := range errorPatterns {
        if strings.Contains(text, pattern) {
            return StatusError
        }
    }

    // Default: active if recent output
    return StatusActive
}

// extractPrompt pulls out the specific prompt text for display
func (m *WorktreeManager) extractPrompt(output string) string {
    lines := strings.Split(output, "\n")

    // Find line containing prompt
    for i := len(lines) - 1; i >= 0 && i > len(lines)-10; i-- {
        line := lines[i]
        if strings.Contains(line, "[Y/n]") ||
           strings.Contains(line, "Allow edit") ||
           strings.Contains(line, "Allow bash") {
            return strings.TrimSpace(line)
        }
    }
    return ""
}
```

### 7.2 Activity Detection

Determine if an agent is actively working (vs. idle):

```go
// isAgentActive checks if the agent has produced output recently
func (a *Agent) isAgentActive() bool {
    // Consider active if output in last 30 seconds
    return time.Since(a.LastOutput) < 30*time.Second
}

// deriveStatus combines multiple signals
func (a *Agent) DeriveStatus() WorktreeStatus {
    if a.WaitingFor != "" {
        return StatusWaiting
    }
    if a.isAgentActive() {
        return StatusActive
    }
    // No recent output but session exists
    return StatusPaused
}
```

### 7.3 Claude Code Hooks (Alternative Approach)

For more reliable status detection with Claude Code, you can use [Claude Code hooks](https://docs.anthropic.com/en/docs/claude-code/hooks-guide):

```json
{
  "hooks": {
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo '{\"event\":\"notification\",\"message\":\"$CLAUDE_NOTIFICATION\"}' >> ~/.sidecar/agent-events.jsonl"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo '{\"event\":\"stop\",\"timestamp\":\"'$(date -Iseconds)'\"}' >> ~/.sidecar/agent-events.jsonl"
          }
        ]
      }
    ]
  }
}
```

The worktree manager can then watch this file for events:

```go
// watchAgentEvents monitors Claude Code hook output
func (m *WorktreeManager) watchAgentEvents(wt *Worktree) {
    eventFile := filepath.Join(os.Getenv("HOME"), ".sidecar", "agent-events.jsonl")

    // Use fsnotify or tail -f equivalent
    // Parse JSONL and update agent status
}
```

**Reference:** See [Claude Code hooks documentation](https://docs.anthropic.com/en/docs/claude-code/hooks) for details.

---

## 7A. Plugin Lifecycle Methods

### 7A.1 Start() Method

The `Start()` method initializes async operations when the plugin begins:

```go
func (p *Plugin) Start() tea.Cmd {
    return tea.Batch(
        p.refresh(),           // Load worktrees
        p.startWatcher(),      // Watch for git changes
        p.reconnectAgents(),   // Find existing tmux sessions
    )
}

func (p *Plugin) refresh() tea.Cmd {
    return func() tea.Msg {
        worktrees, err := p.listWorktrees()
        return RefreshDoneMsg{Worktrees: worktrees, Err: err}
    }
}

func (p *Plugin) startWatcher() tea.Cmd {
    return func() tea.Msg {
        // Watch .git/worktrees for changes
        watcher, err := fsnotify.NewWatcher()
        if err != nil {
            return WatcherErrorMsg{Err: err}
        }
        p.watcher = watcher

        go func() {
            for event := range watcher.Events {
                // Debounce and send refresh message via program.Send()
                _ = event
            }
        }()

        worktreesDir := filepath.Join(p.ctx.WorkDir, ".git", "worktrees")
        watcher.Add(worktreesDir)
        return WatcherStartedMsg{}
    }
}

func (p *Plugin) reconnectAgents() tea.Cmd {
    return func() tea.Msg {
        // Find existing sidecar-wt-* tmux sessions and reconnect
        cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
        output, _ := cmd.Output()

        var cmds []tea.Cmd
        for _, session := range strings.Split(string(output), "\n") {
            if strings.HasPrefix(session, "sidecar-wt-") {
                worktreeName := strings.TrimPrefix(session, "sidecar-wt-")
                // Start polling for reconnected sessions
                cmds = append(cmds, p.scheduleAgentPoll(worktreeName, 0))
            }
        }
        return reconnectedAgentsMsg{Cmds: cmds}
    }
}
```

### 7A.2 Stop() Method

Cleanup when the plugin stops:

```go
func (p *Plugin) Stop() {
    // Stop file watcher
    if p.watcher != nil {
        p.watcher.Close()
    }

    // Note: Don't kill tmux sessions on Stop() - they should persist
    // for agent resume functionality. Only cleanup on explicit user action.
}
```

### 7A.3 Update() Handler Structure

Complete Update() implementation showing message handling:

```go
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return p.handleKey(msg)
    case tea.MouseMsg:
        return p.handleMouse(msg)

    // Refresh cycle
    case RefreshMsg:
        p.refreshing = true
        return p, p.refresh()
    case RefreshDoneMsg:
        p.refreshing = false
        p.lastRefresh = time.Now()
        if msg.Err != nil {
            return p, p.showToast(msg.Err.Error())
        }
        p.worktrees = msg.Worktrees
        return p, nil

    // Agent output polling (see Section 6.3)
    case pollAgentMsg:
        if p.attachedSession == msg.WorktreeName {
            return p, nil // Pause while attached
        }
        return p, p.handlePollAgent(msg.WorktreeName)
    case AgentOutputMsg:
        if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
            wt.Agent.OutputBuf.Write(msg.Output)
            wt.Agent.LastOutput = time.Now()
            wt.Agent.WaitingFor = msg.WaitingFor
            wt.Status = msg.Status
        }
        return p, p.scheduleAgentPoll(msg.WorktreeName, 1*time.Second)
    case AgentStoppedMsg:
        if wt := p.findWorktree(msg.WorktreeName); wt != nil {
            wt.Agent = nil
            wt.Status = StatusPaused
        }
        return p, nil

    // Tmux attach (see Section 6.5)
    case TmuxAttachFinishedMsg:
        p.attachedSession = ""
        return p, p.refresh()

    // Diff loading
    case DiffLoadedMsg:
        if wt := p.findWorktree(msg.WorktreeName); wt != nil {
            p.currentDiff = &DiffContent{
                WorktreeName: msg.WorktreeName,
                Content:      msg.Content,
                Raw:          msg.Raw,
            }
        }
        return p, nil

    // App-level messages
    case app.PluginFocusedMsg:
        return p, p.refresh()
    case app.RefreshMsg:
        return p, p.refresh()
    }

    return p, nil
}
```

---

## 7B. Inter-Plugin Communication

### 7B.1 Navigating to Files

Open a file in the file browser plugin:

```go
func (p *Plugin) openInFileBrowser(worktreePath, filePath string) tea.Cmd {
    return tea.Batch(
        app.FocusPlugin("file-browser"),
        func() tea.Msg {
            // Use relative path from worktree
            return filebrowser.NavigateToFileMsg{Path: filePath}
        },
    )
}
```

### 7B.2 Showing Diffs in Git Plugin

Navigate to the git plugin to show a specific file's diff:

```go
func (p *Plugin) showInGitPlugin(path string) tea.Cmd {
    return tea.Batch(
        app.FocusPlugin("git-status"),
        func() tea.Msg {
            return gitstatus.ShowDiffMsg{Path: path}
        },
    )
}
```

### 7B.3 Event Bus Integration

Subscribe to events from other plugins:

```go
func (p *Plugin) Init(ctx *plugin.Context) error {
    p.ctx = ctx

    // Subscribe to git events for auto-refresh
    if ctx.EventBus != nil {
        gitEvents := ctx.EventBus.Subscribe("git:status-changed")
        go func() {
            for range gitEvents {
                // Trigger refresh when git status changes
                // This catches external commits, pushes, etc.
            }
        }()
    }

    return nil
}

// Publish worktree events for other plugins
func (p *Plugin) publishAgentStatus(wt *Worktree) {
    if p.ctx.EventBus == nil {
        return
    }
    p.ctx.EventBus.Publish("worktree:agent-status", map[string]interface{}{
        "worktree": wt.Name,
        "status":   wt.Status.String(),
    })
}
```

---

## 7C. DiagnosticProvider Implementation

Plugins can implement optional diagnostics for health checks:

```go
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
    var diags []plugin.Diagnostic

    // Check tmux availability
    if _, err := exec.LookPath("tmux"); err != nil {
        diags = append(diags, plugin.Diagnostic{
            Level:   plugin.DiagnosticError,
            Message: "tmux not found in PATH",
            Hint:    "Install tmux: brew install tmux",
        })
    }

    // Check for orphaned sessions
    orphaned := p.countOrphanedSessions()
    if orphaned > 0 {
        diags = append(diags, plugin.Diagnostic{
            Level:   plugin.DiagnosticWarning,
            Message: fmt.Sprintf("%d orphaned tmux sessions", orphaned),
            Hint:    "Press 'c' to cleanup",
        })
    }

    // Check worktree directory writability
    if err := p.validateWorktreeDir(); err != nil {
        diags = append(diags, plugin.Diagnostic{
            Level:   plugin.DiagnosticError,
            Message: "Worktree directory not writable",
            Hint:    err.Error(),
        })
    }

    return diags
}

func (p *Plugin) countOrphanedSessions() int {
    cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
    output, err := cmd.Output()
    if err != nil {
        return 0
    }

    count := 0
    for _, session := range strings.Split(string(output), "\n") {
        if strings.HasPrefix(session, "sidecar-wt-") {
            // Only count sessions we created
            if p.managedSessions[session] {
                worktreeName := strings.TrimPrefix(session, "sidecar-wt-")
                if p.findWorktree(worktreeName) == nil {
                    count++
                }
            }
        }
    }
    return count
}
```

---

## 8. TD Task Manager Integration

### 8.1 The Multi-Database Problem

When using worktrees, each worktree needs to access the **same** td database as the main repository. However, td uses a local `.todos/` directory by default.

**Problem:**

```
~/code/sidecar/.todos/issues.db          # Main repo's tasks
~/code/sidecar-worktrees/feature/.todos/ # Empty! Different database
```

**Solution: `.td-root` file**

When creating a worktree, write a `.td-root` file that points to the main repo:

```go
// setupTDRoot creates a .td-root file pointing to the main repo
func (p *Plugin) setupTDRoot(worktreePath string) error {
    tdRootPath := filepath.Join(worktreePath, ".td-root")
    return os.WriteFile(tdRootPath, []byte(p.repoRoot), 0644)
}
```

**.td-root file format:**
```
# Single line containing absolute path to main worktree
/Users/dev/code/sidecar
```

**Required td modification in `internal/db/db.go`:**

```go
const dbFile = ".todos/issues.db"

func Open(baseDir string) (*DB, error) {
    // Check for worktree redirection
    tdRootPath := filepath.Join(baseDir, ".td-root")
    if content, err := os.ReadFile(tdRootPath); err == nil {
        baseDir = strings.TrimSpace(string(content))
    }

    dbPath := filepath.Join(baseDir, dbFile)
    // ... rest of Open() implementation
}
```

**Important:** The `.td-root` redirection must apply to **all** td file paths, not just the database:
- Database: `.todos/issues.db`
- Config: `.todos/config/`
- Sessions: `.todos/sessions/<branch>/<agent-pid>.json`
- Analytics: `.todos/analytics/`

**Note:** TD already has branch-scoped sessions at `.todos/sessions/<branch>/`, providing automatic isolation per worktree branch.

**Backward Compatibility:** This change is fully backward-compatible with standalone td:
- If `.td-root` doesn't exist, `os.ReadFile` returns an error and the code continues with the original `baseDir`
- td used outside of worktrees (normal git repos) will work exactly as before
- No new flags, environment variables, or configuration required for standalone use

### 8.2 Linking Tasks to Worktrees

```go
// LinkTask associates a td task with a worktree
func (m *WorktreeManager) LinkTask(wt *Worktree, taskID string) error {
    // Validate task exists
    task, err := m.tdClient.GetTask(taskID)
    if err != nil {
        return fmt.Errorf("task not found: %s", taskID)
    }

    // Store link (in worktree metadata)
    linkPath := filepath.Join(wt.Path, ".sidecar-task")
    if err := os.WriteFile(linkPath, []byte(taskID), 0644); err != nil {
        return err
    }

    wt.TaskID = taskID
    return nil
}

// loadTaskLink reads the task link from a worktree
func (m *WorktreeManager) loadTaskLink(worktreePath string) string {
    linkPath := filepath.Join(worktreePath, ".sidecar-task")
    content, err := os.ReadFile(linkPath)
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(content))
}
```

### 8.3 Auto-Starting Tasks

When creating a worktree with a linked task, automatically start the task in td:

```go
// createWorktreeWithTask creates worktree and starts linked td task
func (m *WorktreeManager) createWorktreeWithTask(opts CreateOptions) (*Worktree, error) {
    // Create worktree
    wt, err := m.CreateWorktree(opts)
    if err != nil {
        return nil, err
    }

    // Start td task
    if opts.TaskID != "" {
        if err := m.tdStartTask(wt.Path, opts.TaskID); err != nil {
            // Log but don't fail
            log.Printf("warning: failed to start td task: %v", err)
        }
    }

    return wt, nil
}

// tdStartTask runs `td start <task-id>` in the worktree
func (m *WorktreeManager) tdStartTask(worktreePath, taskID string) error {
    cmd := exec.Command("td", "start", taskID)
    cmd.Dir = worktreePath
    return cmd.Run()
}
```

### 8.4 Providing Task Context to Agents

When starting an agent, inject task context. The recommended approach is to fetch the task context in Go and pass it directly, avoiding shell expansion issues.

```go
// getAgentCommand builds the command to start an agent with context
func (p *Plugin) getAgentCommand(agentType AgentType, wt *Worktree) (string, error) {
    // Get task context if linked
    var taskContext string
    if wt.TaskID != "" {
        ctx, err := p.getTaskContext(wt.TaskID)
        if err == nil {
            taskContext = ctx
        }
    }

    switch agentType {
    case AgentClaude:
        if taskContext != "" {
            // Claude accepts the prompt as an argument
            // Escape for shell safety
            return fmt.Sprintf("claude %q", taskContext), nil
        }
        return "claude", nil

    case AgentCodex:
        return "codex", nil

    case AgentAider:
        return "aider", nil

    case AgentGemini:
        return "gemini", nil

    default:
        return p.config.CustomAgentCommand, nil
    }
}

// getTaskContext fetches task details from td
func (p *Plugin) getTaskContext(taskID string) (string, error) {
    cmd := exec.Command("td", "show", taskID, "--format", "json")
    cmd.Dir = p.repoRoot
    output, err := cmd.Output()
    if err != nil {
        return "", err
    }

    var task struct {
        Title       string `json:"title"`
        Description string `json:"description"`
        Context     string `json:"context"`
    }
    if err := json.Unmarshal(output, &task); err != nil {
        return "", err
    }

    // Build a concise prompt
    return fmt.Sprintf("Task: %s\n\n%s", task.Title, task.Description), nil
}
```

**Agent Session Identity:**

For proper td integration, set `TD_SESSION_ID` when launching agents. This ensures logs and handoffs are correctly scoped:

```go
func (p *Plugin) startAgentInTmux(wt *Worktree, agentCmd string) error {
    sessionName := fmt.Sprintf("sidecar-wt-%s", sanitizeName(wt.Name))

    // Set TD_SESSION_ID environment variable
    envCmd := fmt.Sprintf("export TD_SESSION_ID=%s", sessionName)

    // Create session and set environment
    exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", wt.Path).Run()
    exec.Command("tmux", "send-keys", "-t", sessionName, envCmd, "Enter").Run()

    // Then start the agent
    return exec.Command("tmux", "send-keys", "-t", sessionName, agentCmd, "Enter").Run()
}
```

### 8.5 Task Search for UI

For the fuzzy task search in the new worktree modal:

```go
// GetOpenTasks returns all non-closed tasks for fuzzy filtering in UI
func (p *Plugin) GetOpenTasks() ([]*Task, error) {
    // Get all open tasks as JSON, then filter in Go for faster fuzzy matching
    cmd := exec.Command("td", "list", "--json", "--status", "open,in_progress")
    cmd.Dir = p.repoRoot
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    return parseTDJSON(output)
}

// SearchTasks filters tasks by query (client-side fuzzy matching)
func (p *Plugin) SearchTasks(query string, allTasks []*Task) []*Task {
    if query == "" {
        return allTasks
    }

    query = strings.ToLower(query)
    var matches []*Task
    for _, task := range allTasks {
        // Simple contains match; could use fuzzy matching library
        if strings.Contains(strings.ToLower(task.Title), query) ||
           strings.Contains(strings.ToLower(task.ID), query) {
            matches = append(matches, task)
        }
    }
    return matches
}

// Task represents a td task for the UI
type Task struct {
    ID          string `json:"id"`
    Title       string `json:"title"`
    Status      string `json:"status"`
    Description string `json:"description,omitempty"`
}

func parseTDJSON(data []byte) ([]*Task, error) {
    var tasks []*Task
    if err := json.Unmarshal(data, &tasks); err != nil {
        return nil, err
    }
    return tasks, nil
}
```

**Note:** Consider reusing td parsing logic from `internal/plugins/tdmonitor` if available, to avoid duplication.

---

## 9. Data Persistence

### 9.1 Worktree Metadata

Worktree state is stored in multiple locations:

1. **Git's worktree metadata:** `.git/worktrees/<name>/`
2. **sidecar metadata per worktree:** `<worktree>/.sidecar/`
3. **Project-level sidecar config:** `<repo>/.sidecar/worktrees.json`

```go
// WorktreeMetadata is stored in <worktree>/.sidecar/meta.json
type WorktreeMetadata struct {
    TaskID     string    `json:"taskId,omitempty"`
    AgentType  AgentType `json:"agentType,omitempty"`
    CreatedAt  time.Time `json:"createdAt"`
    CreatedBy  string    `json:"createdBy"` // "sidecar" or "manual"
    BaseBranch string    `json:"baseBranch"`
}

// saveMetadata writes metadata to the worktree
func (m *WorktreeManager) saveMetadata(wt *Worktree) error {
    metaDir := filepath.Join(wt.Path, ".sidecar")
    os.MkdirAll(metaDir, 0755)

    meta := WorktreeMetadata{
        TaskID:     wt.TaskID,
        AgentType:  wt.Agent.Type,
        CreatedAt:  wt.CreatedAt,
        CreatedBy:  "sidecar",
        BaseBranch: wt.BaseBranch,
    }

    data, _ := json.MarshalIndent(meta, "", "  ")
    return os.WriteFile(filepath.Join(metaDir, "meta.json"), data, 0644)
}
```

### 9.2 Runtime State

Runtime state (agents, sessions) is kept in memory and reconstructed on startup.

**Important:** Session reconnection happens via the `reconnectAgents()` function in `Start()` (see Section 7A.1), which properly uses the tea.Cmd pattern instead of spawning goroutines directly.

```go
// reconnectAgents is called from Start() and returns commands to start polling
// for any existing tmux sessions. See Section 7A.1 for full implementation.
func (p *Plugin) reconnectAgents() tea.Cmd {
    return func() tea.Msg {
        cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
        output, _ := cmd.Output()

        var pollingCmds []tea.Cmd
        for _, session := range strings.Split(string(output), "\n") {
            session = strings.TrimSpace(session)
            if !strings.HasPrefix(session, "sidecar-wt-") {
                continue
            }

            worktreeName := strings.TrimPrefix(session, "sidecar-wt-")

            // Load metadata if available
            if wt := p.findWorktree(worktreeName); wt != nil {
                agent := &Agent{
                    TmuxSession: session,
                    StartedAt:   time.Now(), // Unknown actual start
                    OutputBuf:   NewOutputBuffer(500),
                }

                meta := p.loadMetadata(wt.Path)
                if meta != nil {
                    agent.Type = meta.AgentType
                }

                wt.Agent = agent
                p.agents[wt.Name] = agent

                // Track as managed (for safe cleanup)
                p.managedSessions[session] = true

                // Schedule polling via tea.Cmd (NOT goroutine)
                pollingCmds = append(pollingCmds,
                    p.scheduleAgentPoll(worktreeName, 0))
            }
        }

        return reconnectedAgentsMsg{Cmds: pollingCmds}
    }
}

// Handle in Update():
case reconnectedAgentsMsg:
    return p, tea.Batch(msg.Cmds...)
```

**Note:** Never call `go pollAgentOutput()` directly. Always use the tea.Cmd pattern to ensure Bubble Tea's state management remains consistent.

---

## 10. Configuration

### 10.1 Plugin Configuration

Configuration is part of sidecar's main config file (`~/.config/sidecar/config.json`):

```json
{
  "plugins": {
    "worktree": {
      "enabled": true,
      "refreshInterval": "2s",
      "worktreeDir": "../{project}-worktrees",
      "defaultAgent": "claude",
      "agents": {
        "claude": {
          "command": "claude"
        },
        "codex": {
          "command": "codex"
        },
        "aider": {
          "command": "aider --model anthropic/claude-3-5-sonnet-20241022"
        },
        "gemini": {
          "command": "gemini"
        }
      },
      "setup": {
        "copyEnv": true,
        "runSetupScript": true,
        "setupScriptName": ".worktree-setup.sh",
        "symlinkDirs": []
      },
      "tmux": {
        "historyLimit": 10000,
        "mouseEnabled": true,
        "sessionPrefix": "sidecar-wt-"
      },
      "td": {
        "autoStart": true,
        "autoHandoff": false
      }
    }
  }
}
```

### 10.2 Project-Level Configuration

Projects can override settings via `.sidecar/config.json`:

```json
{
  "worktree": {
    "worktreeDir": "./worktrees",
    "setup": {
      "symlinkDirs": ["node_modules", ".venv"]
    }
  }
}
```

### 10.3 Setup Script

Projects can define `.worktree-setup.sh` to run after worktree creation:

```bash
#!/bin/bash
# .worktree-setup.sh - Runs in new worktree after creation

# Install dependencies
if [ -f "package.json" ]; then
    npm install
fi

if [ -f "requirements.txt" ]; then
    pip install -r requirements.txt
fi

# Copy environment files from main worktree
if [ -n "$MAIN_WORKTREE" ] && [ -f "$MAIN_WORKTREE/.env" ]; then
    cp "$MAIN_WORKTREE/.env" .env
fi

# Start dev server in background (optional)
# npm run dev &
```

The setup script receives these environment variables:

- `MAIN_WORKTREE`: Path to main repository
- `WORKTREE_BRANCH`: Name of the new branch
- `WORKTREE_PATH`: Path to the new worktree

---

## 11. Safety Precautions

### 11.1 Preventing Data Loss

1. **Uncommitted changes check:**

   ```go
   func (m *WorktreeManager) isWorktreeDirty(path string) (bool, error) {
       cmd := exec.Command("git", "status", "--porcelain")
       cmd.Dir = path
       output, err := cmd.Output()
       if err != nil {
           return false, err
       }
       return len(strings.TrimSpace(string(output))) > 0, nil
   }
   ```

2. **Confirmation dialogs for destructive actions:**
   - Deleting worktrees with uncommitted changes
   - Force-removing branches
   - Stopping agents with pending work

3. **Auto-commit on agent completion:**
   ```go
   func (m *WorktreeManager) autoCommitIfConfigured(wt *Worktree) error {
       if !m.config.AutoCommit {
           return nil
       }

       if dirty, _ := m.isWorktreeDirty(wt.Path); dirty {
           cmd := exec.Command("git", "add", "-A")
           cmd.Dir = wt.Path
           cmd.Run()

           msg := fmt.Sprintf("WIP: %s [sidecar auto-commit]", wt.Branch)
           cmd = exec.Command("git", "commit", "-m", msg)
           cmd.Dir = wt.Path
           return cmd.Run()
       }
       return nil
   }
   ```

### 11.2 Preventing Branch Conflicts

1. **Check for existing branches before creation:**

   ```go
   func (m *WorktreeManager) branchExists(name string) (bool, error) {
       cmd := exec.Command("git", "rev-parse", "--verify", name)
       cmd.Dir = m.repoRoot
       return cmd.Run() == nil, nil
   }
   ```

2. **Detect same-file modifications across worktrees:**
   ```go
   func (m *WorktreeManager) detectConflicts() []Conflict {
       var conflicts []Conflict

       // Get modified files in each worktree
       filesByWorktree := make(map[string][]string)
       for _, wt := range m.worktrees {
           files, _ := m.getModifiedFiles(wt.Path)
           filesByWorktree[wt.Name] = files
       }

       // Find overlaps
       for i, wt1 := range m.worktrees {
           for _, wt2 := range m.worktrees[i+1:] {
               overlap := intersection(filesByWorktree[wt1.Name], filesByWorktree[wt2.Name])
               if len(overlap) > 0 {
                   conflicts = append(conflicts, Conflict{
                       Worktrees: []string{wt1.Name, wt2.Name},
                       Files:     overlap,
                   })
               }
           }
       }

       return conflicts
   }
   ```

### 11.3 tmux Session Safety

1. **Unique session names:**

   ```go
   func sanitizeName(name string) string {
       // tmux session names can't contain periods or colons
       name = strings.ReplaceAll(name, ".", "-")
       name = strings.ReplaceAll(name, ":", "-")
       name = strings.ReplaceAll(name, "/", "-")
       return name
   }
   ```

2. **Session existence check:**

   ```go
   func sessionExists(name string) bool {
       cmd := exec.Command("tmux", "has-session", "-t", name)
       return cmd.Run() == nil
   }
   ```

3. **Graceful agent termination:**
   ```go
   func (m *WorktreeManager) StopAgent(wt *Worktree) error {
       if wt.Agent == nil {
           return nil
       }

       // Try graceful interrupt first
       exec.Command("tmux", "send-keys", "-t", wt.Agent.TmuxSession, "C-c").Run()

       // Wait briefly
       time.Sleep(2 * time.Second)

       // Check if still running
       if sessionExists(wt.Agent.TmuxSession) {
           // Force kill
           exec.Command("tmux", "kill-session", "-t", wt.Agent.TmuxSession).Run()
       }

       wt.Agent = nil
       delete(m.agents, wt.Name)
       return nil
   }
   ```

### 11.4 Worktree Cleanup

Handle orphaned worktrees (directory deleted manually):

```go
func (m *WorktreeManager) Prune() error {
    // Let git clean up its metadata
    cmd := exec.Command("git", "worktree", "prune")
    cmd.Dir = m.repoRoot
    return cmd.Run()
}

func (m *WorktreeManager) RepairWorktree(path string) error {
    cmd := exec.Command("git", "worktree", "repair", path)
    cmd.Dir = m.repoRoot
    return cmd.Run()
}
```

---

## 12. Implementation Phases

### Phase 1: Core Infrastructure (MVP)

**Goal:** Basic worktree management without agents

**Features:**

- [ ] Plugin structure and registration
- [ ] List view UI with worktree list
- [ ] Create worktree (branch name, base branch)
- [ ] Delete worktree
- [ ] View diff in preview pane
- [ ] Git stats (additions, deletions)
- [ ] Push to remote

### Phase 1.5: TUI Loop Validation

**Goal:** Ensure view rendering works correctly before adding complexity

**Features:**

- [ ] Test list view with mock data
- [ ] Test kanban ↔ list view toggle
- [ ] Test focus handling across panes
- [ ] Verify `View(width, height)` height constraints
- [ ] Add min-width check for kanban (auto-collapse to list if too narrow)

### Phase 2: Agent Integration

**Goal:** Run and monitor agents in worktrees

**Features:**

- [ ] Start agent in tmux session
- [ ] Capture and display agent output (using tea.Cmd/Msg pattern)
- [ ] Status detection (active/waiting/done)
- [ ] Approve/reject prompts from UI
- [ ] Attach to tmux session (with polling pause)
- [ ] Resume on existing worktrees
- [ ] Session tracking for safe cleanup

### Phase 3: TD Integration

**Goal:** Link worktrees to tasks

**Features:**

- [ ] `.td-root` file for worktrees
- [ ] Link task to worktree
- [ ] Fuzzy task search in create modal
- [ ] Auto-start td task
- [ ] Display task info in preview pane
- [ ] TD_SESSION_ID injection for agents

**Note:** Requires small modification to td's `internal/db/db.go` for `.td-root` support.

### Phase 4: Workflow Polish

**Goal:** Streamlined end-to-end experience

**Features:**

- [ ] Merge workflow (diff review → push → PR → merge → cleanup)
- [ ] Setup script support
- [ ] Environment file copying
- [ ] node_modules symlinking
- [ ] Conflict detection
- [ ] Activity timeline/log

### Phase 5: Advanced Features

**Goal:** Power user features

**Features:**

- [ ] Kanban view
- [ ] Batch operations
- [ ] Multiple agent types
- [ ] Claude Code hooks integration
- [ ] Keyboard customization
- [ ] Project-level config
- [ ] DiagnosticProvider implementation

---

## 13. Reference Implementations

### 13.1 Claude Squad

**Repository:** https://github.com/smtg-ai/claude-squad

**Key files to study:**

- `session/tmux.go` - tmux session management, capture-pane
- `session/git/worktree.go` - worktree creation and management
- `ui/` - BubbleTea UI implementation
- `config/` - Configuration handling

**Patterns to adopt:**

- Session naming convention
- Output capture polling
- Auto-yes mode implementation
- Diff view rendering

### 13.2 Treehouse Worktree

**Repository:** https://github.com/mark-hingston/treehouse-worktree

**Key features to study:**

- Lock system for agent coordination
- Setup script execution
- Cleanup with retention policies
- MCP server integration

### 13.3 Sidecar (Existing Plugins)

**Repository:** (this project)

**Key files to study:**

- `internal/plugins/git/` - Git status plugin (diff rendering)
- `internal/plugins/td/` - TD plugin (td integration patterns)
- `internal/tui/` - Shared TUI components

---

## Appendix: Command Reference

### Git Worktree Commands

```bash
# List worktrees
git worktree list
git worktree list --porcelain        # Machine-readable

# Add worktree
git worktree add <path>              # Auto-create branch from path name
git worktree add <path> <branch>     # Checkout existing branch
git worktree add -b <branch> <path>  # Create new branch
git worktree add -b <branch> <path> <base>  # New branch from base

# Remove worktree
git worktree remove <path>           # Fails if dirty
git worktree remove --force <path>   # Force remove

# Maintenance
git worktree prune                   # Clean up stale entries
git worktree prune --dry-run         # Preview what would be pruned
git worktree repair <path>           # Fix broken links

# Locking (prevent prune)
git worktree lock <path>
git worktree lock <path> --reason "Working on feature"
git worktree unlock <path>
```

### tmux Commands

```bash
# Sessions
tmux new-session -d -s <name>        # Create detached
tmux new-session -d -s <name> -c <dir>  # With working directory
tmux kill-session -t <name>          # Kill session
tmux has-session -t <name>           # Check if exists (exit code)
tmux list-sessions                   # List all sessions
tmux list-sessions -F "#{session_name}"  # Names only

# Capturing output
tmux capture-pane -t <session> -p    # Print to stdout
tmux capture-pane -t <session> -p -S -200  # Last 200 lines
tmux capture-pane -t <session> -p -S -     # Entire history
tmux capture-pane -t <session> -p -e       # Include escape sequences

# Sending input
tmux send-keys -t <session> "text" Enter   # Send text + Enter
tmux send-keys -t <session> -l "literal"   # Send without key lookup
tmux send-keys -t <session> C-c            # Send Ctrl+C
tmux send-keys -t <session> Escape         # Send Escape

# Attaching
tmux attach-session -t <name>        # Attach to session
tmux detach-client                   # Detach (from within)

# Options
tmux set-option -t <session> history-limit 10000
tmux set-option -t <session> mouse on
```

### td Commands

```bash
# Tasks
td list                              # List all tasks
td list --status open                # Filter by status
td list --json                       # JSON output
td show <task-id>                    # Task details
td create "Title" --type feature     # Create task
td start <task-id>                   # Start working on task
td handoff <task-id> --done "..." --remaining "..."

# Query
td query "status = open AND priority <= P1"
td query "title ~ 'auth'"

# Session
td usage                             # Current context
td session --new "name"              # New session
```

---

## Document History

| Version     | Date     | Changes               |
| ----------- | -------- | --------------------- |
| 0.1.0-draft | Jan 2026 | Initial specification |

---

_This document is a living specification. Update it as implementation reveals new requirements or constraints._

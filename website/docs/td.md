---
sidebar_position: 2
title: TD - Task Management for AI Agents
---

# TD

A task management CLI designed specifically for AI-assisted development. When an agent's context window ends, its memory ends. TD captures the work state—what's done, what's remaining, key decisions, and uncertainties—so the next session picks up exactly where the previous one left off.

![TD Monitor in Sidecar](/img/screenshots/sidecar-td.png)

## The Problem

AI coding agents have a fundamental limitation: **context windows reset**. When a session ends, the agent forgets everything. This leads to:

- **Hallucinated state**: Agents guess what's done vs. remaining
- **Lost decisions**: Why did we choose approach X over Y?
- **Repeated work**: Re-implementing things already completed
- **Broken handoffs**: No structured way to pass context between sessions

TD solves this by providing **external memory** for AI agents—a local database that persists across context windows.

## Quick Install

```bash
go install github.com/marcus/td@latest
```

**Requirements:** Go 1.21+

## Quick Start

Initialize TD in your project:

```bash
td init
```

This creates a `.todos/` directory with a SQLite database. Add to `.gitignore` automatically.

### For AI Agents (Claude Code, Cursor, etc.)

Add this to your `CLAUDE.md` or agent instructions:

```markdown
# Task Management

Run at the start of every conversation:
td usage --new-session

Before your context ends, ALWAYS run:
td handoff <issue-id> --done "..." --remaining "..."
```

## Core Concepts

### Sessions

Every terminal or AI agent gets a unique session ID. Sessions are scoped by **git branch + agent type**, so the same agent on the same branch maintains consistent identity.

```bash
td whoami                    # Show current session
td usage --new-session       # Start fresh session, see current state
```

TD auto-detects agent type: Claude Code, Cursor, Copilot, or manual terminal.

### Issues

Create structured work items with type and priority:

```bash
td create "Implement OAuth2 authentication flow" --type feature --priority P1
```

Issue types: `feature`, `bug`, `chore`, `docs`, `refactor`, `test`

Priorities: `P0` (critical), `P1` (high), `P2` (medium), `P3` (low)

### Lifecycle

Issues follow a state machine with enforced transitions:

```
open → in_progress → in_review → closed
         ↓              ↑
      blocked ──────────┘ (reject)
```

Key constraint: **The session that implements code cannot approve it.** This ensures a different context (human or another agent session) reviews the work.

## CLI Commands

### Working on Issues

```bash
td start <id>                # Begin work (moves to in_progress)
td log "message"             # Track progress
td log --decision "..."      # Log a decision with reasoning
td log --blocker "..."       # Log a blocker
td handoff <id> \            # Capture state for next session
  --done "OAuth flow working" \
  --remaining "Token refresh, error handling" \
  --decision "Using JWT for stateless auth" \
  --uncertain "Should tokens expire on password change?"
```

### Review Workflow

```bash
td review <id>               # Submit for review (moves to in_review)
td reviewable                # List issues you can review
td context <id>              # See full handoff state
td approve <id>              # Approve and close (different session required)
td reject <id> --reason "..."  # Reject back to in_progress
```

### Querying Issues

```bash
td list                      # All open issues
td list --status in_progress # Filter by status
td next                      # Highest priority open issue
td show <id>                 # Full issue details
td search "keyword"          # Full-text search
```

### TDQ Query Language

Powerful expressions for filtering:

```bash
td query "status = in_progress AND priority <= P1"
td query "type = bug AND labels ~ auth"
td query "assignee = @me AND created >= -7d"
td query "rework()"          # Issues rejected, needing fixes
td query "stale(14)"         # No updates in 14 days
```

Operators: `=`, `!=`, `~` (contains), `<`, `>`, `<=`, `>=`, `AND`, `OR`, `NOT`

### Dependencies

Model relationships between issues:

```bash
td dep add <issue> <depends-on>   # Issue depends on another
td dep rm <issue> <depends-on>    # Remove dependency
td dep <issue>                    # Show what this depends on
td blocked-by <issue>             # Show all transitively blocked
td critical-path                  # Optimal work sequence
```

### File Tracking

Link files to issues to track what changed:

```bash
td link <id> src/auth/*.go   # Link files (records SHA)
td files <id>                # Show status: [modified], [unchanged], [new]
```

### Boards

Organize issues with query-based boards:

```bash
td board create "Sprint 1" --query "labels ~ sprint-1"
td board list
td board show <board>
```

## Multi-Issue Work Sessions

When tackling multiple related issues:

```bash
td ws start "Auth implementation"    # Start work session
td ws tag td-a1b2 td-c3d4            # Associate issues (auto-starts them)
td ws log "Shared token storage"     # Log to all tagged issues
td ws handoff                        # Capture state for all, end session
```

## Structured Handoffs

The handoff is the most important command. It captures everything the next session needs:

```bash
td handoff td-a1b2 \
  --done "OAuth callback endpoint, token storage, login UI" \
  --remaining "Refresh token rotation, logout endpoint, error states" \
  --decision "Using httpOnly cookies instead of localStorage for tokens - more secure against XSS" \
  --uncertain "Should we support multiple active sessions per user?"
```

Each field serves a purpose:

| Field | Purpose |
|-------|---------|
| `--done` | What's actually complete and tested |
| `--remaining` | Specific tasks left (not vague "finish it") |
| `--decision` | Why you chose this approach (prevents re-litigation) |
| `--uncertain` | Questions for the next session or human review |

## Workflow Examples

### Single Issue (Typical)

```bash
# Session 1: Start work
td start td-a1b2
td log "Set up OAuth provider config"
td log --decision "Using Auth0 - better docs than Okta"
td handoff td-a1b2 --done "Provider setup" --remaining "Callback handling"

# Session 2: Continue
td usage --new-session        # See where we left off
td start td-a1b2              # Resume
td log "Implemented callback endpoint"
td review td-a1b2             # Submit for review

# Session 3 (different): Review
td reviewable                 # See pending reviews
td context td-a1b2            # Read full handoff
td approve td-a1b2            # Close it
```

### Bug Fix with Context

```bash
td create "Login fails silently on expired tokens" --type bug --priority P1
td start td-xyz123
td log "Reproduced: token refresh race condition"
td log --decision "Adding mutex around token refresh"
td link td-xyz123 src/auth/refresh.go
td handoff td-xyz123 \
  --done "Root cause identified, fix implemented" \
  --remaining "Add regression test" \
  --uncertain "Should we add retry logic for network failures?"
```

### Parallel Work with Worktrees

Combined with Sidecar's worktree management:

1. Create worktree in Sidecar (`n`)
2. Link TD task (`t`)
3. Agent works with TD tracking progress
4. Handoff before context ends
5. Review in separate session/worktree

## Live Monitor

Run the interactive TUI dashboard:

```bash
td monitor
```

Features:
- Real-time task visualization
- Board view with swimlanes
- Search and filtering
- Statistics modal
- Keyboard navigation

Or use Sidecar's TD Monitor plugin for integrated viewing.

## Configuration

TD is zero-config by default. Optional environment variables:

| Variable | Purpose |
|----------|---------|
| `TD_SESSION_ID` | Force specific session ID |
| `TD_ANALYTICS` | Set to `false` to disable usage analytics |

## Data Storage

All data lives in `.todos/db.sqlite`—a local SQLite database. No external services, no sync, no accounts.

```
.todos/
├── db.sqlite          # All issues, logs, handoffs
└── sessions/          # Session tracking per branch
```

## Integration with Sidecar

Sidecar's TD Monitor plugin provides a visual dashboard for TD:

- See all issues at a glance
- Submit reviews directly (`r`)
- View issue details (`enter`)
- Real-time refresh

![TD in Sidecar](/img/screenshots/sidecar-td2.png)

## Source

[GitHub Repository](https://github.com/marcus/td)

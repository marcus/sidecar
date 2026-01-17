---
sidebar_position: 6
title: Worktrees Plugin
---

# Worktrees Plugin

Manage multiple git worktrees with agent integration, real-time output streaming, and a Kanban board view—run parallel agents across branches.

## Overview

The Worktrees plugin provides a two-pane layout:

- **Left pane**: Worktree list (or Kanban columns)
- **Right pane**: Preview tabs (Output, Diff, Task)
- **Draggable divider**: Resize panes to your preference

Toggle views with `v` for list or Kanban board.

## View Modes

### List View

Vertical list of all worktrees with status indicators:

| Status | Meaning |
|--------|---------|
| Active | Agent running |
| Waiting | Agent needs approval |
| Done | Work completed |
| Paused | Agent stopped |
| Error | Something failed |

### Kanban View

Press `v` to switch to Kanban board with columns:
- **Active**: Agents currently working
- **Waiting**: Agents needing approval
- **Done**: Completed work
- **Paused**: Stopped agents

Each column shows worktree count.

## Worktree Navigation

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `g` | Jump to first |
| `G` | Jump to last |
| `h`, `←` | Previous column (Kanban) |
| `l`, `→` | Next column (Kanban) or focus preview |
| `v` | Toggle list/Kanban view |

## Preview Tabs

Three tabs in the preview pane:

| Key | Action |
|-----|--------|
| `[` | Previous tab |
| `]` | Next tab |

### Output Tab

Real-time agent output streaming:

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down (pauses auto-scroll) |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `g` | Jump to top |
| `G` | Jump to bottom (resumes auto-scroll) |

Auto-scroll follows new output by default. Manual scrolling pauses it; pressing `G` or `j` at the bottom resumes.

### Diff Tab

Git diff for the worktree branch:

| Key | Action |
|-----|--------|
| `v` | Toggle unified/side-by-side view |
| `h`, `←` | Scroll left (wide diffs) |
| `l`, `→` | Scroll right |
| `0` | Reset horizontal scroll |

Shows merge conflicts when present.

### Task Tab

Linked TD task details with markdown rendering:

| Key | Action |
|-----|--------|
| `m` | Toggle markdown rendering |
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |

## Agent Management

Start, stop, and control coding agents from any worktree.

### Starting Agents

| Key | Action |
|-----|--------|
| `s` | Start agent |

If no agent is running, opens a choice modal:
- Claude
- Codex
- Gemini
- Cursor
- OpenCode
- Aider

### Attaching to Agents

| Key | Action |
|-----|--------|
| `enter` | Attach to running agent |

Opens the agent's terminal session for direct interaction.

### Agent Controls

| Key | Action |
|-----|--------|
| `S` | Stop agent |
| `y` | Approve pending action |
| `Y` | Approve all pending prompts |
| `N` | Reject pending action |

The approval workflow handles agents in "Waiting" status that need permission to proceed.

## Worktree Operations

### Creating Worktrees

Press `n` to open the create modal:

| Field | Description |
|-------|-------------|
| Name | Worktree branch name |
| Base branch | Branch to create from |
| Prompt | Initial agent prompt |
| Task | Link to TD task (optional) |
| Agent | Which agent to start |
| Skip perms | Skip permission prompts |

Modal navigation:

| Key | Action |
|-----|--------|
| `tab` | Next field |
| `shift+tab` | Previous field |
| `j`, `↓` | Navigate dropdowns |
| `k`, `↑` | Navigate dropdowns |
| `enter` | Select or confirm |
| `esc` | Cancel |

### Deleting Worktrees

| Key | Action |
|-----|--------|
| `D` | Delete worktree |

Opens confirmation with options:
- Delete local branch
- Delete remote branch

| Key | Action |
|-----|--------|
| `j`, `↓` | Navigate options |
| `space` | Toggle checkbox |
| `enter` | Confirm |
| `D` | Quick delete (power user) |
| `esc` | Cancel |

### Push & Remote

| Key | Action |
|-----|--------|
| `p` | Push branch to remote |

## Task Linking

Link worktrees to TD tasks for context:

| Key | Action |
|-----|--------|
| `t` | Link/unlink TD task |

Opens task picker to select from open tasks.

## Merge Workflow

Press `m` to start a multi-step merge:

1. **Diff review**: See changes to be merged
2. **Method selection**: Choose merge strategy (merge, squash, rebase)
3. **PR creation**: Wait for PR to be created
4. **Cleanup options**: Delete branches after merge

| Key | Action |
|-----|--------|
| `j`, `↓` | Navigate options |
| `enter` | Proceed to next step |
| `space` | Toggle checkboxes |
| `tab` | Cycle focus |
| `s` | Skip step (for pushed branches) |
| `esc`, `q` | Cancel merge |

## Pane Navigation

| Key | Action |
|-----|--------|
| `tab` | Switch to next pane |
| `shift+tab` | Switch to previous pane |
| `l`, `→` | Focus preview pane |
| `h`, `←` | Focus sidebar |
| `enter` | Focus preview (from list) |
| `esc` | Return to sidebar |
| `\` | Toggle sidebar visibility |

## Mouse Support

- **Click worktree**: Select
- **Click tab**: Switch preview tab
- **Click button**: Execute action
- **Drag divider**: Resize panes
- **Scroll**: Navigate lists and content

## State Persistence

These preferences save across sessions:
- View mode (list/Kanban)
- Sidebar width
- Diff view mode (unified/side-by-side)
- Active tab

## Command Reference

All keyboard shortcuts by context:

### Sidebar Context (`worktree-sidebar`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `h`, `←` | Previous column (Kanban) |
| `l`, `→` | Next column / focus preview |
| `v` | Toggle view mode |
| `n` | Create worktree |
| `D` | Delete worktree |
| `p` | Push branch |
| `d` | Show diff |
| `m` | Merge workflow |
| `t` | Link task |
| `s` | Start agent |
| `S` | Stop agent |
| `y` | Approve action |
| `Y` | Approve all |
| `N` | Reject action |
| `enter` | Attach to agent |
| `[` | Previous tab |
| `]` | Next tab |
| `tab` | Focus preview |
| `\` | Toggle sidebar |

### Preview Context (`worktree-preview`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `v` | Toggle diff view (diff tab) |
| `h`, `←` | Scroll left / focus sidebar |
| `l`, `→` | Scroll right |
| `0` | Reset scroll |
| `m` | Toggle markdown (task tab) |
| `s` | Start agent |
| `S` | Stop agent |
| `y` | Approve action |
| `Y` | Approve all |
| `N` | Reject action |
| `[` | Previous tab |
| `]` | Next tab |
| `tab` | Focus sidebar |
| `esc` | Focus sidebar |
| `\` | Toggle sidebar |

### Create Modal (`worktree-create`)

| Key | Action |
|-----|--------|
| `tab` | Next field |
| `shift+tab` | Previous field |
| `j`, `↓` | Navigate dropdown |
| `k`, `↑` | Navigate dropdown |
| `enter` | Select / confirm |
| `esc` | Cancel |

### Merge Modal (`worktree-merge`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Navigate options |
| `k`, `↑` | Navigate options |
| `enter` | Proceed |
| `space` | Toggle checkbox |
| `tab` | Cycle focus |
| `s` | Skip step |
| `esc`, `q` | Cancel |

### Delete Modal (`worktree-delete`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Navigate options |
| `k`, `↑` | Navigate options |
| `space` | Toggle checkbox |
| `enter` | Confirm |
| `D` | Quick delete |
| `esc`, `q` | Cancel |

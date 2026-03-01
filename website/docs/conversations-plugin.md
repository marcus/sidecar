---
sidebar_position: 5
title: Conversations Plugin
---

# Conversations Plugin

Browse and search your AI coding sessions with turn-based organization, message expansion, and session analytics—see what your agents have been doing across multiple tools.

![Conversations Plugin](/img/sidecar-conversations.png)

## Supported Agents

The Conversations plugin automatically detects and displays sessions from:

| Agent | Icon | Description |
|-------|------|-------------|
| Amp Code | ⚡ | Amp's AI coding assistant |
| Claude Code | ◆ | Anthropic's CLI coding agent |
| Codex | ▶ | OpenAI's CLI coding agent |
| Cursor CLI | ▌ | Cursor's background agent |
| Gemini CLI | ★ | Google's CLI coding agent |
| GitHub Copilot CLI | ⋮⋮ | GitHub's terminal assistant |
| Kiro | κ | Amazon's AI coding assistant |
| OpenCode | ◇ | Open-source coding agent |
| Pi | 🐾 | Pi AI agent (OpenClaw) |
| Warp | » | Warp terminal AI |

Sessions from all detected agents appear in a unified list, with icons indicating the source.

## Overview

The Conversations plugin provides a two-pane layout:

- **Left pane**: Session list with search and filters
- **Right pane**: Message detail with expandable turns
- **Draggable divider**: Resize panes to your preference

Toggle the sidebar with `\` to maximize message space.

## Session List

Browse all sessions from your local history across all supported agents.

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `g` | Jump to first session |
| `G` | Jump to last session |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `enter` | View selected session |

### Search & Filter

| Key | Action |
|-----|--------|
| `/` | Search sessions by title or ID |
| `f` | Filter by project |
| `esc` | Clear search/filter |

Search matches session titles and conversation content.

### Session Actions

| Key | Action |
|-----|--------|
| `y` | Copy session as markdown |
| `o` | Open/resume session in CLI (agent-specific) |

## Message View

Two view modes for reading conversations:

| Key | Action |
|-----|--------|
| `l` or `r` | Toggle between flow and turn view |

### Conversation Flow

Messages display in order, similar to chat-style interfaces:
- User messages with prompts
- Assistant responses with tool results collapsed
- Expandable tool invocations

### Turn View

Groups messages into conversation "turns" (user prompt + assistant response):
- Collapsed by default
- Shows token counts and tool summary
- Expand to see full message content

## Message Navigation

| Key | Action |
|-----|--------|
| `j`, `↓` | Next turn/message |
| `k`, `↑` | Previous turn/message |
| `enter` or `d` | Expand/collapse turn or view detail |
| `y` | Copy turn content |
| `o` | Open in CLI |

### Detail View

Press `enter` on a turn to see full details in the right pane:

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `y` | Copy detail content |
| `h`, `←` | Return to turn list |
| `esc` | Close detail view |

## Pane Navigation

| Key | Action |
|-----|--------|
| `tab` | Switch to next pane |
| `shift+tab` | Switch to previous pane |
| `l`, `→` | Focus message pane |
| `h`, `←` | Focus sidebar |
| `\` | Toggle sidebar visibility |

## Session Analytics

View statistics about a session:
- Model usage breakdown (tokens by model)
- File impacts (which files were created/modified)
- Tool invocations (count by tool type)
- Total token consumption

## Pagination

Sessions load 50 messages at a time. Scroll to load older messages automatically with "load older" support for long conversations.

## Incremental Updates

The plugin watches for new messages and coalesces updates for performance. Your session list stays current as agents work.

## Render Caching

Markdown rendering is cached per-message to maintain smooth scrolling even with large conversations.

## Mouse Support

- **Click session**: Select and view
- **Click turn**: Expand/collapse
- **Click tool**: Toggle tool result visibility
- **Drag divider**: Resize panes
- **Scroll**: Navigate lists and content

## State Persistence

These preferences save across sessions:
- Sidebar width
- View mode (flow/turn)
- Expanded states

## Command Reference

All keyboard shortcuts by context:

### Sidebar Context (`conversations-sidebar`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `/` | Search sessions |
| `f` | Filter by project |
| `enter` | View session |
| `y` | Copy markdown |
| `o` | Open in CLI |
| `l`, `→` | Focus messages |
| `tab` | Focus messages |
| `\` | Toggle sidebar |

### Messages Context (`conversations-messages`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Next turn |
| `k`, `↑` | Previous turn |
| `l` or `r` | Toggle view mode |
| `enter`, `d` | Expand/view detail |
| `y` | Copy content |
| `o` | Open in CLI |
| `h`, `←` | Focus sidebar |
| `tab` | Focus sidebar |
| `esc` | Return to sidebar |
| `\` | Toggle sidebar |

### Detail Context (`conversations-detail`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `y` | Copy content |
| `h`, `←` | Close detail |
| `esc` | Close detail |

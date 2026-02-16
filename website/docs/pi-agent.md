---
sidebar_position: 8
title: Pi Agent Adapter
---

# Pi Agent Adapter

Sidecar automatically detects and displays sessions from [Pi Agent](https://github.com/anthropics/pi), Anthropic's standalone AI agent, alongside all other supported coding agents.

## How It Works

Pi Agent stores session data as JSONL files in `~/.pi/agent/sessions/`. Each project gets its own subdirectory based on its path (e.g., `/home/user/project` → `--home-user-project--/`).

Sidecar watches this directory for new and updated session files, parsing them in real-time. Sessions appear in the [Conversations plugin](/docs/conversations-plugin) with the 🐾 icon.

## Detection

No configuration is needed. If Pi Agent sessions exist at `~/.pi/agent/sessions/`, sidecar picks them up automatically. The adapter follows the same patterns as the Claude Code adapter:

- File system watcher for real-time updates
- JSONL parsing with the same message format
- Token counting and session analytics
- Resume support via the Conversations plugin

## Session Format

Pi Agent sessions use the same JSONL conversation format as other adapters. Each line represents a message (user prompt, assistant response, or tool invocation), enabling turn-based browsing and search within sidecar.

---
sidebar_position: 3
title: Git Plugin
---

# Git Plugin

A full-featured git interface for staging, diffing, committing, and managing branches—all without leaving your terminal. Watch your agent's changes in real-time with side-by-side diffs and inline previews.

![Git Status](/img/screenshots/sidecar-git.png)

## Overview

The Git plugin provides a three-pane layout:

- **Left pane**: File tree (staged, modified, untracked) + recent commits
- **Right pane**: Diff preview or commit details
- **Draggable divider**: Resize panes to your preference

Changes auto-refresh as your agent works, so you always see the current state.

## File Status

Files are organized into sections with status indicators:

| Symbol | Status | Meaning |
|--------|--------|---------|
| `M` | Modified | File changed |
| `A` | Added | New file (staged) |
| `D` | Deleted | File removed |
| `R` | Renamed | File renamed |
| `?` | Untracked | New file not tracked |
| `U` | Unmerged | Merge conflict |

Each file shows `+/-` line counts for quick impact assessment.

## Staging & Unstaging

| Key | Action |
|-----|--------|
| `s` | Stage selected file or folder |
| `u` | Unstage selected file |
| `S` | Stage all files |
| `D` | Discard changes (with confirmation) |

Stage entire folders by selecting the folder and pressing `s`. After staging, the cursor automatically moves to the next unstaged file.

## Diff Viewing

View changes inline or in full-screen mode with syntax highlighting.

| Key | Action |
|-----|--------|
| `d` | Open full-screen diff |
| `v` | Toggle unified / side-by-side view |
| `h/l` | Scroll horizontally (wide diffs) |
| `0` | Reset horizontal scroll |
| `esc`, `q` | Close diff |

![Side-by-side Diff](/img/screenshots/sidecar-git-diff-side-by-side.png)

### Diff Modes

- **Unified**: Traditional line-by-line format with `+` and `-` markers
- **Side-by-side**: Split view showing before/after columns

Your preferred mode persists across sessions.

### Diff Sources

- **Modified files**: Working directory changes (`git diff`)
- **Staged files**: Changes ready to commit (`git diff --cached`)
- **Untracked files**: Shows all lines as additions
- **Commits**: View what changed in any commit

## Commit Workflow

1. Stage files with `s` or `S`
2. Press `c` to open the commit modal
3. Enter your commit message (multi-line supported)
4. Press `ctrl+s` or Tab to button and Enter

The modal shows:
- Count of staged files
- Total lines added/removed
- List of files being committed
- Message textarea

If commit fails (hooks, etc.), your message is preserved for retry.

## Branch Management

| Key | Action |
|-----|--------|
| `b` | Open branch picker |

The branch picker shows:
- All local branches
- Current branch highlighted
- Upstream tracking info (`↑N ↓N` ahead/behind)

Select a branch and press Enter to switch.

## Push, Pull & Fetch

| Key | Action |
|-----|--------|
| `P` | Open push menu |
| `p` | Pull from remote |
| `f` | Fetch from remote |

### Push Menu

Three options when you press `P`:

1. **Push**: Regular `git push -u origin HEAD`
2. **Force push**: `git push --force-with-lease` (safer force push)
3. **Push with upstream**: Sets tracking if not configured

Quick shortcuts in the push menu:
- `p` - Quick push
- `f` - Quick force push
- `u` - Quick push with upstream

### Push Status

The commit sidebar shows:
- Commits ahead/behind upstream
- Per-commit pushed indicator
- Unpushed commit count

## Stash Operations

| Key | Action |
|-----|--------|
| `z` | Stash all changes |
| `Z` | Pop latest stash (with confirmation) |

Pop shows a confirmation modal with stash details before applying.

## Commit History

The sidebar shows recent commits with infinite scroll. Navigate to load more.

| Key | Action |
|-----|--------|
| `/` | Search commits |
| `n` | Next search match |
| `N` | Previous search match |
| `f` | Filter by author |
| `p` | Filter by file path |
| `F` | Clear all filters |
| `v` | Toggle commit graph |

### Commit Search

Search by commit subject or author name. Supports:
- Case-insensitive matching (default)
- Regex patterns (optional)
- Live-as-you-type results

### Commit Graph

Press `v` to toggle ASCII graph visualization showing branch history:

```
* abc123 Latest commit
* def456 Previous commit
|\
| * ghi789 Feature branch
|/
* jkl012 Merge base
```

### Commit Preview

Select a commit to see:
- Full commit message
- Files changed with `+/-` stats
- Navigate files and view their diffs

## Clipboard Operations

| Key | Action |
|-----|--------|
| `y` | Copy commit as markdown |
| `Y` | Copy commit hash only |

Markdown format includes subject, hash, author, date, stats, and file list.

## GitHub Integration

| Key | Action |
|-----|--------|
| `o` | Open commit in GitHub |

Auto-detects repository from remote URL (SSH or HTTPS).

## Navigation

### File Tree

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `enter` | Open file in editor / toggle folder |
| `O` | Open file in File Browser plugin |
| `l`, `→` | Focus diff pane |

### Diff Pane

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `h`, `←` | Focus sidebar / scroll left |

### General

| Key | Action |
|-----|--------|
| `tab` | Switch focus between panes |
| `\` | Toggle sidebar visibility |
| `r` | Refresh status |

## Mouse Support

- **Click file**: Select and show diff
- **Click commit**: Select and show preview
- **Click folder**: Expand/collapse
- **Drag divider**: Resize panes
- **Scroll**: Navigate lists and diffs

## Real-Time Updates

The plugin watches your `.git` directory and auto-refreshes when:
- Files change on disk
- Agent makes commits
- Branches switch
- Remote operations complete

Debounced to prevent excessive refreshes (500ms minimum).

## State Persistence

These preferences save across sessions:
- Diff view mode (unified/side-by-side)
- Sidebar width
- Commit graph enabled/disabled

## Command Reference

All keyboard shortcuts by context:

### Files Context (`git-status`)

| Key | Action |
|-----|--------|
| `s` | Stage |
| `u` | Unstage |
| `S` | Stage all |
| `d` | Full diff |
| `D` | Discard |
| `c` | Commit |
| `b` | Branch picker |
| `P` | Push menu |
| `p` | Pull |
| `f` | Fetch |
| `z` | Stash |
| `Z` | Pop stash |
| `r` | Refresh |
| `O` | Open in file browser |
| `enter` | Open in editor |

### Commits Context (`git-status-commits`)

| Key | Action |
|-----|--------|
| `/` | Search |
| `n` | Next match |
| `N` | Previous match |
| `f` | Filter by author |
| `p` | Filter by path |
| `F` | Clear filters |
| `v` | Toggle graph |
| `y` | Copy markdown |
| `Y` | Copy hash |
| `o` | Open in GitHub |

### Diff Context (`git-status-diff`, `git-diff`)

| Key | Action |
|-----|--------|
| `v` | Toggle view mode |
| `h`, `←` | Scroll left |
| `l`, `→` | Scroll right |
| `0` | Reset scroll |
| `O` | Open in file browser |
| `esc`, `q` | Close |

### Commit Modal (`git-commit`)

| Key | Action |
|-----|--------|
| `ctrl+s` | Execute commit |
| `tab` | Switch focus |
| `esc` | Cancel |

### Push Menu (`git-push-menu`)

| Key | Action |
|-----|--------|
| `p` | Quick push |
| `f` | Quick force push |
| `u` | Push with upstream |
| `enter` | Execute selected |
| `esc`, `q` | Close |

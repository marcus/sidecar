---
sidebar_position: 4
title: Files Plugin
---

# Files Plugin

Browse, preview, and manage your project files with syntax highlighting, markdown rendering, and fuzzy search—all in a two-pane terminal interface.

## Overview

The Files plugin provides a two-pane layout:

- **Left pane**: Collapsible file tree with directory expansion
- **Right pane**: File preview with syntax highlighting
- **Draggable divider**: Resize panes to your preference

Toggle the tree pane with `\` to maximize preview space.

## File Tree

Navigate your project structure with vim-style keys.

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `l`, `→` | Expand directory or open file |
| `h`, `←` | Collapse directory |
| `enter` | Toggle directory expansion |
| `g` | Jump to top |
| `G` | Jump to bottom |

### Quick Navigation

| Key | Action |
|-----|--------|
| `ctrl+p` | Quick open (fuzzy file search) |
| `ctrl+s` | Project-wide content search |
| `/` | Filter tree by filename |

Quick open caches up to 50,000 files and shows 50 results max. Project search finds matches across your entire codebase.

## File Preview

View files with syntax highlighting for code and rendered markdown.

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `g` | Jump to top |
| `G` | Jump to bottom |

### Content Search

Search within the current file:

| Key | Action |
|-----|--------|
| `?` | Open content search |
| `n` | Next match |
| `N` | Previous match |
| `esc` | Close search |

### Preview Features

- **Syntax highlighting**: Language detection from file extension
- **Markdown toggle**: Press `m` to switch between raw and rendered markdown
- **Image preview**: Terminal graphics protocol support (where available)
- **Large file handling**: Truncated preview with size warning for big files
- **Binary detection**: Shows file size instead of corrupted content

### Clipboard

| Key | Action |
|-----|--------|
| `y` | Copy file contents |
| `c` | Copy file path |

## File Operations

Create, rename, move, and delete files without leaving the terminal.

| Key | Action |
|-----|--------|
| `n` | Create new file |
| `N` | Create new directory |
| `r` | Rename file or directory |
| `m` | Move file or directory |
| `d` | Delete (with confirmation) |

### Copy & Paste

| Key | Action |
|-----|--------|
| `y` | Yank (mark for copy) |
| `p` | Paste yanked file |

Yank a file, navigate to the destination, and paste. Works across directories.

### File Info

Press `i` to see a modal with:
- Git status (tracked, modified, staged)
- File size
- Last modified date
- File permissions

## Navigation

### Pane Switching

| Key | Action |
|-----|--------|
| `tab` | Switch to next pane |
| `shift+tab` | Switch to previous pane |
| `\` | Toggle tree visibility |

### Tree to Preview

| Key | Action |
|-----|--------|
| `l`, `→` | Focus preview pane |
| `enter` | Load file into preview |

### Preview to Tree

| Key | Action |
|-----|--------|
| `h`, `←` | Focus tree pane |
| `esc` | Return to tree |
| `\` | Toggle tree (shows if hidden) |

## Mouse Support

- **Click file**: Select and preview
- **Click folder**: Expand/collapse
- **Drag divider**: Resize panes
- **Scroll**: Navigate tree or preview
- **Drag in preview**: Select text (multi-line supported)

## File Watching

The preview auto-reloads when the file changes on disk. Useful for watching agent modifications in real-time.

## State Persistence

These preferences save across sessions:
- Expanded directories
- Cursor position
- Scroll offsets
- Active pane
- Sidebar width

## Command Reference

All keyboard shortcuts by context:

### Tree Context (`file-browser-tree`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Move down |
| `k`, `↑` | Move up |
| `l`, `→` | Expand / open |
| `h`, `←` | Collapse |
| `enter` | Toggle expand |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `ctrl+p` | Quick open |
| `ctrl+s` | Project search |
| `/` | Filter files |
| `n` | Create file |
| `N` | Create directory |
| `r` | Rename |
| `m` | Move |
| `d` | Delete |
| `y` | Yank |
| `p` | Paste |
| `c` | Copy path |
| `i` | File info |
| `tab` | Focus preview |
| `\` | Toggle tree |

### Preview Context (`file-browser-preview`)

| Key | Action |
|-----|--------|
| `j`, `↓` | Scroll down |
| `k`, `↑` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `?` | Content search |
| `n` | Next match |
| `N` | Previous match |
| `m` | Toggle markdown |
| `y` | Copy contents |
| `c` | Copy path |
| `h`, `←` | Focus tree |
| `esc` | Return to tree |
| `\` | Toggle tree |

### Quick Open (`file-browser-quickopen`)

| Key | Action |
|-----|--------|
| type | Filter files |
| `j`, `↓` | Next result |
| `k`, `↑` | Previous result |
| `enter` | Open file |
| `esc` | Cancel |

### Project Search (`file-browser-project-search`)

| Key | Action |
|-----|--------|
| type | Search query |
| `j`, `↓` | Next result |
| `k`, `↑` | Previous result |
| `enter` | Open file at match |
| `esc` | Cancel |

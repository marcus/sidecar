# Changelog

All notable changes to sidecar are documented here.

## [v0.11.0] - 2026-01-07

### Bug Fixes
- Fixed WARN logs appearing in non-git directories (plugin unavailable now logs at DEBUG level)

## [v0.10.0] - 2026-01-07

### Features
- **Git History Search**: Search commits with `/` key, regex support, case-sensitive toggle
- **Author Filter**: Filter commits by author with `f` key
- **Path Filter**: Filter commits by file path with `p` key
- **Inline Commit Stats**: Display +/- stats next to selected commits

### Improvements
- Removed delta external tool fallback (built-in diff viewer only)
- Moved tree search bar inside pane for consistent UX
- Consolidated horizontal scroll bindings (h, left, <, H)

### Refactoring
- Removed single-pane mode from conversations plugin (~400 lines)
- Updated adapter-creator-guide with Icon() requirement

### Bug Fixes
- Fixed duplicate horizontal scroll bindings in git diff view
- Added unknown adapter fallback ("?") in badge rendering
- Added explicit .git exclusion in file search

## [v0.9.0] - 2026-01-07

### Features
- **File Info Modal**: View file info with git status via info modal
- **Copy Paths**: Copy files/paths from right panel of files plugin
- **Session Persistence**: Remember previously opened plugin/tab across restarts
- **File Memory**: File browser remembers open file across projects
- **Colorful Tabs**: Improved visual tab styling
- **Adapter Icons**: Populate AdapterIcon in session creation

### Improvements
- Better missing-td screen
- Improved git repo readability
- Removed emojis from info modal

### Refactoring
- Split filebrowser plugin.go into handlers.go and operations.go

### Bug Fixes
- Various fixes from code review

### Dependencies
- Updated embedded td to v0.5.0 (from v0.4.23)

## [v0.8.4] - 2026-01-06

### Dependencies
- Updated embedded td to v0.4.23 (from v0.4.22)

## [v0.8.3] - 2026-01-06

### Bug Fixes
- Fixed mouse wheel scrolling not working when cursor is over session or turn items in conversations plugin
- Added `scrollDetailPane()` for detail view mouse scrolling

## [v0.8.2] - 2026-01-06

### Dependencies
- Updated embedded td to v0.4.22

## [v0.8.1] - 2026-01-06

### Dependencies
- Updated embedded td from v0.4.18 to v0.4.21

### Documentation
- Updated release guide to document td sync requirement before releases

## [v0.8.0] - 2026-01-05

### Features
- **Cursor CLI Adapter**: Full support for Cursor Agent sessions with query extraction, system context filtering, meaningful session names, model info, and resume command support
- **In-App Updates**: Update sidecar and td directly from the app with interactive button in diagnostics modal
- **Markdown Rendering**: Toggle markdown preview in file browser with 'm' key

### UI Improvements
- Turn detail shown in right pane (two-pane layout)
- Improved conversations plugin layout

### Bug Fixes
- Fixed markdown cache invalidation on window resize
- Fixed detail pane height overflow with scroll indicators
- Optimized regex compilation in cursor adapter

## [v0.7.2] - 2026-01-05

### Features
- Force version check on diagnostics modal open (bypasses 3-hour cache)

## [v0.7.1] - 2026-01-05

### Bug Fixes
- Fixed `Y` key to copy correct adapter-specific resume command instead of always copying `claude --resume`

## [v0.7.0] - 2026-01-05

### Features
- **In-App Update Feature**: Update sidecar and td directly from within the app
  - Press `!` to open diagnostics modal
  - Press `u` or click **Update** button to install updates
  - Animated spinner shows installation progress
  - Restart prompt after successful update

## [v0.6.1] - 2026-01-05

### Changes
- Reduced version check cache TTL from 6 hours to 3 hours

## [v0.6.0] - 2026-01-05

### Features
- **Markdown Rendering in Conversations**: LLM responses render with proper markdown formatting
  - Code blocks with syntax highlighting
  - Headers, lists, emphasis
  - Automatic fallback to plain text for narrow terminals
  - Cached rendering for performance

# Changelog

All notable changes to sidecar are documented here.

## [v0.46.0] - 2026-01-25

### Improvements
- Watcher improvements for better file monitoring
- Better project switching with improved context handling
- Enhanced conversation switch guidance
- Cursor position improvements in modals

### Bug Fixes
- Project switcher context improvements

### Documentation
- Docs changes and additional tests

## [v0.45.0] - 2026-01-24

### Bug Fixes
- Add io.Closer return value to Watch() method for proper resource cleanup in adapter implementations

## [v0.44.0] - 2026-01-23

### Features
- **File Browser**: Fast file browser with improved performance
- **Projects**: Inline project creation from project switcher modal

### Improvements
- Eliminate interactive typing latency in worktree mode
- Unify modal priority with ModalKind type and activeModal() helper
- Remove side-scrolling
- Update keybindings
- Revert poll interval to 2s (fingerprint cache approach sufficient)

### Bug Fixes
- Fix button hit region calculation in project add modal
- Fix exit shell in some situations
- Remove dead shells properly

### Dependencies
- Updated embedded td to v0.21.0

## [v0.43.0] - 2026-01-23

### Features
- **Interactive Mode**: Character-level selection granularity with drag-to-select
- **Interactive Mode**: Selection background with preserved foreground colors
- **Git**: Add git amend commit shortcut (A) in git-status
- **Workspace**: Renamed worktrees to workspaces for clarity

### Improvements
- **Interactive Mode**: Incremental parsing with targeted session refresh reducing CPU usage
- **Interactive Mode**: Named shells upon creation for better session tracking
- **Modal**: Only close modals when clicking outside them (improved UX)
- **Input**: Align interactive copy/paste hints with configured keys
- Filter partial SGR mouse sequences to prevent stray ESC forwarding
- Enhanced keyboard shortcut handling and escape sequence processing

### Bug Fixes
- Fixed selections in interactive mode
- Fixed stray ESC forwarding in partial mouse sequence filter

### Dependencies
- Updated embedded td to latest version

## [v0.42.0] - 2026-01-23

### Improvements
- Enhanced keyboard shortcut handling and bindings
- Improved gitstatus plugin event handling

### Dependencies
- Updated embedded td to latest version

## [v0.41.0] - 2026-01-22

### Bug Fixes
- Fixed feature flags being reset during config saves
- Fixed interactive mode scroll to use previewOffset instead of tmux commands
- Fixed config save overwriting user settings
- Fixed git repo root detection from subdirectory in gitstatus plugin

### Improvements
- Enhanced tmux pane resizing for detached sessions
- Improved tmux pane width synchronization

## [v0.40.0] - 2026-01-22

### Performance
- **Interactive Mode**: Improved output capture and rendering performance
- **Interactive Mode**: Enhanced auto-scroll alignment with interactive content
- **Interactive Mode**: Fixed cursor spacing and synchronized pane sizing

### Dependencies
- Updated embedded td to latest version

## [v0.39.0] - 2026-01-22

### Performance
- **Interactive Mode**: Three-state visibility polling (visible+focused, visible+unfocused, not visible)
- **Interactive Mode**: Fixed duplicate poll chain bug causing 200% CPU usage
- **Interactive Mode**: Correct generation map usage for shell vs workspace polling

## [v0.38.0] - 2026-01-22

### Features
- **Interactive Mode**: Beta interactive shell mode behind feature flag (`features.interactiveMode`)

### Improvements
- **Workspace**: Modal keyboard navigation with tab/shift+tab cycling

### Dependencies
- Updated embedded td to v0.20.0

## [v0.37.0] - 2026-01-21

### Dependencies
- Updated embedded td to v0.19.0 (performance fix)

## [v0.36.0] - 2026-01-21

### Features
- **Themes**: Live theme switcher modal with persistence

## [v0.35.0] - 2026-01-21

### Improvements
- **File Browser**: Refactored scroll-to-line logic into reusable helper

## [v0.34.0] - 2026-01-20

### Features
- **Tabs**: Configurable tab themes with 16 built-in presets
- **Workspace**: File picker modal (`f`) in diff view
- **Workspace**: File headers and navigation in diff pane
- **Project Switcher**: Header click opens project switcher
- **Shells**: Rename shells with persistent display names
- **Shortcuts**: Improved modal layout

### Performance
- **Tasks**: Pre-fetch task details to eliminate lag in task tab
- **Adapters**: Cache metadata and reduce file I/O

### Bug Fixes
- **Merge**: Better error handling and resolution actions
- **Workspace**: Fix race conditions in caches and pre-fetch
- **Workspace**: Flash preview pane on invalid key interactions
- **Workspace**: Fix workspace deletion from non-repo cwd
- **Watchers**: Fix race conditions and buffer issues
- **Project Switcher**: Don't hijack filter input
- **Shells**: Fix display name persistence when saving defaults
- Guard flashPreviewTime against zero-value time

### Dependencies
- Updated embedded td to v0.18.0

## [v0.33.0] - 2026-01-20

### Features
- **Workspace**: Multiple shells per workspace - open and manage multiple terminal sessions
- **Workspace**: [+] buttons in Shells and Workspaces sub-headers for quick creation
- **Workspace**: Persist and restore workspace/shell selection across sessions
- **Project Switcher**: g/G navigation to jump to first/last project
- **File Browser**: Auto-refresh tree on plugin focus

### Bug Fixes
- **Workspace**: Fix orphaned tmux sessions on workspace delete/merge
- **Workspace**: Fix shell selection shift when earlier shell removed
- **Workspace**: Fix shell selection bugs and use name-based polling

## [v0.32.0] - 2026-01-20

### Features
- **Workspace**: Improved shell UX and navigation

### Bug Fixes
- **Workspace**: Auto-focus newly created workspace in list and preview
- **Workspace**: Handle waitForSession failure in ensureShellAndAttach

## [v0.31.0] - 2026-01-20

### Features
- **Workspace**: Project shell as first entry in workspace list

### Bug Fixes
- **Workspace**: Shell preview shows output immediately
- **Workspace**: Auto-attach to existing shell with improved primer text
- **Workspace**: Fixed shell preview, primer, and project switch issues
- **Workspace**: Replace fixed sleep with retry loop in ensureShellAndAttach
- **Project Switcher**: Better help modal
- **Website**: Fixed hamburger menu navigation links

## [v0.30.0] - 2026-01-20

### Features
- **Project Switcher**: Type-to-filter support - type to filter projects by name/path in real-time, shows match count, Esc clears filter or closes modal
- **Project Switcher**: j/k keyboard navigation now works correctly (previously went to text input)

### Bug Fixes
- Fixed project switcher Esc handler missing context update
- Fixed project switcher hover state not clearing on filter change

### Documentation
- Added project switcher developer guide (`docs/guides/project-switcher-dev-guide.md`)

## [v0.29.0] - 2026-01-19

### Features
- **Project Switcher**: Press `@` to switch between configured projects without restarting sidecar. Configure projects in `~/.config/sidecar/config.json` with `projects.list`. Supports keyboard navigation (j/k, Enter) and mouse interaction. State (active plugin, cursor positions) is remembered per project.
- **File Browser**: Toggle git-ignored file visibility with `I` key, state persists across sessions

### Dependencies
- Updated embedded td to v0.17.0

## [v0.28.0] - 2026-01-19

### Features
- **File Browser**: Vim-style `:<number>` line jump in file preview

### Bug Fixes
- **Workspace**: Reload commit status when cached list is empty

### Dependencies
- Updated embedded td to v0.16.0

## [v0.27.1] - 2026-01-19

### Bug Fixes
- **Conversations**: Use adapter-specific agent names instead of hardcoded "claude"

## [v0.27.0] - 2026-01-19

### Bug Fixes
- **Cursor Adapter**: Use blob hash as message ID to prevent cache collisions

## [v0.26.0] - 2026-01-19

### Features
- **Git Blame View**: Added blame view to file browser plugin
- **Thinking Status**: Added thinking status indicator to workspace with detection priority fix
- **Truncation Cache**: Added truncation cache to eliminate ANSI parser allocation churn

### Performance
- **Conversations Plugin**: Performance improvements with code review refinements

### Bug Fixes
- Fixed memory leak in workspace output panel horizontal scrolling
- Fixed unicode truncation and extracted blame constants

### Dependencies
- Updated embedded td to v0.15.1

## [v0.25.0] - 2026-01-17

### Features
- **Memory Profiling**: Added pprof instrumentation for diagnosing memory leaks (enable with `SIDECAR_PPROF=1`)
- **TD Theme Integration**: Embedded td now respects sidecar's theme colors for markdown rendering

### Dependencies
- Updated embedded td to v0.14.0 (includes theme support for markdown rendering)

## [v0.24.0] - 2026-01-17

### Dependencies
- Updated embedded td to v0.13.0

## [v0.23.0] - 2026-01-17

### Features
- **File Browser Improvements**: Support for vim-like line jumps (`:<number>`) in file browser

### Performance
- **Memory Optimizations**: Improved memory usage for long-running sessions

### Dependencies
- Updated embedded td to latest version

## [v0.22.0] - 2026-01-17

### Features
- **Yank keyboard shortcuts**: Added y/Y keys for copying content in conversations plugin
- **Send-to-workspace integration**: Launch agents directly from td monitor to workspaces

### Bug Fixes
- Fixed workspace session lookup for nested directories and sanitized names
- Fixed send-to-workspace with lazy loaded npm environments
- Fixed Unicode truncation and refactored modal initialization
- Fixed memory leak and CPU performance in workspace output pane
- Fixed off-by-one mouse hit regions in workspace modals
- Fixed commit status not showing for workspaces with unset BaseBranch
- Fixed O(n²) cache eviction in session metadata cache
- Fixed detectDefaultBranch() not being called due to caller defaults

### Changes
- Removed YAML config support (JSON only)
- Extracted resolveBaseBranch() helper to deduplicate default branch detection
- Replaced hardcoded 'main' defaults with detectDefaultBranch()

## [v0.21.0] - 2026-01-17

### Bug Fixes
- Fixed pullAfterMerge corrupting working tree when on base branch (uses pull --ff-only instead of update-ref)

## [v0.20.0] - 2026-01-17

### Features
- **Simplified workspace kanban**: Removed "Thinking" status, streamlined to Active/Waiting/Done/Paused
- Updated Waiting status icon from 💬 to ⧗ for better clarity

### Dependencies
- Updated embedded td to latest version

## [v0.19.0] - 2026-01-16

### Features
- **Workspace merge improvements**: Gracefully handle existing MRs, mouse support for merge modal
- **Workspace conversation integration**: Better workspace-conversation linking
- **Website**: TUI-themed homepage with interactive demo, agents section
- **Docusaurus documentation site**: Added Docusaurus 3.9 documentation site

### Bug Fixes
- Fixed race condition in cleanup completion
- Added branch deletion warnings
- Fixed workspace click offset
- Fixed workspace create modal mouse support

### Performance
- Workspace adaptive polling and optimized tmux capture

### Improvements
- Split large files for better maintainability

## [v0.18.0] - 2026-01-15

### Features
- **Workspace diff improvements**: Show commits in diff pane even when no uncommitted changes
- **Workspace conversation preservation**: Conversations now preserved after workspace deletion
- **Workspace-aware conversations**: Conversations plugin now understands workspace context
- **Mouse support**: Comprehensive mouse support added to workspace plugin
- **Workspace guide**: Added workspace explanation to welcome guide

### Bug Fixes
- Fixed SanitizeBranchName `.lock` suffix handling
- Improved workspace conversation detection

## [v0.17.0] - 2026-01-15

### Features
- **Workspace prompts: Create workspaces with custom prompts attached
- **Auto-generated default prompts**: New users get starter prompts automatically
- **PR indicator: Workspaces with open PRs now show visual indicator
- **Inline tmux guide**: Tmux setup instructions integrated into workspace view
- **Better waiting/paused visibility**: Clearer distinction between waiting and paused states in workspaces

### Bug Fixes
- Fixed 20+ Unicode byte-slicing bugs in UI string truncation across multiple components
- Fixed empty prompt picker UI display
- Added prompt creation guide for new users

### Dependencies
- Updated embedded td to v0.12.3 (from v0.12.2)

## [v0.16.3] - 2026-01-14

### Improvements
- Improved kanban board in workspaces plugin

### Bug Fixes
- Use launcher script for agent prompts to avoid shell escaping issues
- Change 'c' key in merge workflow to skip cleanup (keep workspace) instead of advancing to cleanup

## [v0.16.2] - 2026-01-14

### Bug Fixes
- Escape agent messages properly in workspaces plugin
- Pass task context to all agent types in workspaces plugin
- Better workspace initial environment handling
- Minor improvements to Claude Code adapter
- Consolidate env var commands in workspace sessions (cleaner output)

### Dependencies
- Updated embedded td to latest

## [v0.16.1] - 2026-01-14

### Bug Fixes
- Session list now only reserves space for duration/token columns when data exists (more room for titles)

## [v0.16.0] - 2026-01-14

### Features
- **Conversations UI Overhaul**: Premium experience for viewing Claude Code sessions
  - Colorful model badges (opus=purple, sonnet=green, haiku=blue)
  - Token flow indicators showing input→output usage
  - Tool-specific icons (Read, Edit, Write, Bash, Search, etc.)
  - Enhanced thinking block styling with expand/collapse
  - Session list shows adapter icons and token counts
  - Improved main pane header with model badge, stats, and cost estimate

### Bug Fixes
- Fixed XML tags appearing in session titles (now properly extracts user queries)
- Fixed session titles for messages starting with local command caveats
- Skip trivial commands (/clear, /compact) when finding session title
- Filter out metadata-only sessions (no messages) from session list
- Improved extraction of text content from tool inputs in message display

### Dependencies
- Updated embedded td to v0.12.2 (from v0.12.1)

## [v0.15.0] - 2026-01-14

### Features
- Remember workspace diff mode (staged/unstaged preference persists)
- Documented workspaces plugin in README

### Bug Fixes
- Fixed git diff view for commits
- Many QoL changes and bug fixes
- Ignore double-click on folders in git status (single-click handles expansion)
- Clear stale push hash on push error
- Add shift+tab support for workspace pane switching

### Dependencies
- Updated embedded td to v0.12.1 (from v0.12.0)

## [v0.14.7] - 2026-01-14

### Features
- Auto-add sidecar state files (.sidecar-agent, .sidecar-task, .td-root) to .gitignore on workspace creation

### Bug Fixes
- Fixed nil pointer in stageAllAndCommit when git tree fails to initialize
- Clear preview pane when workspace is deleted to prevent stale content
- Cancel merge workflow on error instead of proceeding with broken state
- Show "No workspace selected" message when workspace list is empty

## [v0.14.6] - 2026-01-14

### Bug Fixes
- Fixed panels not extending to footer row in Files, Conversations, and Git plugins (drag handle appeared longer than panels)

## [v0.14.5] - 2026-01-14

### Features
- Added confirmation dialog before deleting workspaces

### Bug Fixes
- Fixes from code review

## [v0.14.4] - 2026-01-14

### Bug Fixes
- Fixed layout rendering issues where plugin header would scroll off-screen
- Improved width calculations to properly account for borders and padding
- Added ANSI-aware truncation to handle escape codes correctly
- Added tab expansion for proper alignment in terminal output

### Dependencies
- Updated embedded td to latest version

## [v0.14.3] - 2026-01-14

### Bug Fixes
- Fixed horizontal scroll to preserve syntax highlighting in diffs and workspace views

### Dependencies
- Updated embedded td to v0.12.0 (from v0.11.0)

## [v0.14.2] - 2026-01-13

### Bug Fixes
- Fixed installation failure due to missing td types (updated td v0.10.0 → v0.11.0)

## [v0.14.1] - 2026-01-13

### Bug Fixes
- Fixed border rendering issues in conversations plugin
- Improved gradient border rendering in td integration

## [v0.14.0] - 2026-01-13

### Features
- **Theming System**: Thread-safe theming infrastructure with customizable colors
- **Unified Sidebars**: Consistent collapsible sidebar behavior across all plugins

### Improvements
- Cache improvements in conversation adapters
- Performance optimizations for conversations loading
- Render cache LRU comment fix

### Bug Fixes
- Fixed race condition and CPU optimization for session adapters
- Fixed losing mouse interactivity after editing a file
- Fixed quit bug in td
- Fixed IsValidHexColor comment to match regex behavior

### Dependencies
- Updated embedded td to v0.10.0 (from v0.9.0)

## [v0.13.2] - 2026-01-10

### Improvements
- Conversations plugin performance improvements
- Modal button hover/click behavior refined
- Modals now have more uniform styling

### Dependencies
- Updated embedded td to v0.9.0 (from v0.7.0)

## [v0.13.1] - 2026-01-10

### Bug Fixes
- Fixed off-by-one error in git sidebar commit click detection when working tree is clean

## [v0.13.0] - 2026-01-10

### Features
- **Git Graph View**: Visualize commit history as ASCII graph with `g` key toggle
- **Improved Git List View**: Better commit display with cleaner formatting

### Improvements
- Git sidebar UI refinements and polish
- Updated modal creator guide documentation

### Bug Fixes
- Fixed conversations plugin rendering issues
- Various conversations plugin stability fixes

## [v0.12.1] - 2026-01-08

### Bug Fixes
- Fixed intermittent crashes while an agent was running by mutex-protecting Claude Code adapter session cache

### Dependencies
- Updated embedded td to v0.7.0 (from v0.5.0)

## [v0.12.0] - 2026-01-07

### Features
- **Interactive Modal Buttons**: File browser modals now have clickable Confirm/Cancel buttons
- **Tab Navigation**: Tab key cycles focus between input field and modal buttons
- **Mouse Hover**: Buttons highlight on mouse hover (dark pink)
- **Path Auto-Complete**: Move modal shows fuzzy-matched directory suggestions

### Improvements
- Better visual feedback for modal button interactions (focus vs hover states)

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

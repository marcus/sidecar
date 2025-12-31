# Project-Wide Search Feature for File Browser Plugin

## Overview

Add VS Code-style project search to the file browser plugin using ripgrep for fast, gitignore-aware searching. Results display as collapsible file tree with match lines. Triggered via `ctrl+s`.

## Key Decisions

- **Backend**: ripgrep (`rg --json`) - fast, respects .gitignore, structured output
- **UI Pattern**: Integrated mode in file browser (not separate plugin)
- **Options**: Keyboard toggles (Alt+R=regex, Alt+C=case, Alt+W=word)
- **Results**: Collapsible file tree with match lines underneath
- **Mouse**: Use existing `internal/mouse` HitMap pattern (no bubblezone)
- **Forms**: Custom input handling (no huh library, follows existing patterns)

## Architecture Provisions

- **Future tabs**: State struct supports multiple search tabs
- **Future replace**: Data structures include match positions for replace preview

---

## Files to Create

### `internal/plugins/filebrowser/project_search.go`

Core data structures and ripgrep integration:

```go
type SearchOptions struct {
    UseRegex, CaseSensitive, WholeWord bool
}

type SearchMatch struct {
    LineNo, StartCol, EndCol int
    Line string
}

type SearchFileResult struct {
    Path       string
    Matches    []SearchMatch
    IsExpanded bool
    MatchCount int
}

type ProjectSearchState struct {
    Active       bool
    Query        string
    Options      SearchOptions
    Results      []*SearchFileResult
    TotalMatches int
    CursorIndex  int
    ScrollOffset int
    IsInputMode  bool
    IsSearching  bool
    Error        string
}
```

Ripgrep integration:

- `buildRipgrepCmd(query, opts)` - constructs rg command with flags
- `parseRipgrepOutput([]byte)` - parses JSON output to results
- `executeProjectSearch() tea.Cmd` - async search execution

### `internal/plugins/filebrowser/project_search_view.go`

View rendering:

- `renderProjectSearchView()` - main dual-pane layout
- `renderSearchInputBar()` - query input + toggle buttons
- `renderSearchResults(height)` - collapsible file/match tree
- `renderSearchFileHeader()` - file row with expand indicator
- `renderSearchMatchLine()` - indented match with line number

### `internal/plugins/filebrowser/project_search_test.go`

Tests for ripgrep output parsing and state transitions

---

## Files to Modify

### `internal/plugins/filebrowser/plugin.go`

1. **Add state field** (~line 132):

```go
// Project-wide search state
projectSearch ProjectSearchState
searchMouseHandler *mouse.Handler
```

1. **Add message types** (~line 67):

```go
ProjectSearchStartMsg struct{}
ProjectSearchResultMsg struct {
    Results      []*SearchFileResult
    TotalMatches int
    Error        error
}
```

1. **Handle ctrl+s** in `handleTreeKey()` and `handlePreviewKey()`:

```go
case "ctrl+s":
    p.projectSearch.Active = true
    p.projectSearch.IsInputMode = true
    p.projectSearch.Query = ""
    return p, nil
```

1. **Add focus context** in `FocusContext()`:

```go
if p.projectSearch.Active {
    return "file-browser-project-search"
}
```

1. **Route key handling** in `handleKey()`:

```go
if p.projectSearch.Active {
    return p.handleProjectSearchKey(msg)
}
```

1. **Handle search result messages** in `Update()`:

```go
case ProjectSearchResultMsg:
    p.projectSearch.IsSearching = false
    p.projectSearch.Results = msg.Results
    p.projectSearch.TotalMatches = msg.TotalMatches
    if msg.Error != nil {
        p.projectSearch.Error = msg.Error.Error()
    }
```

### `internal/plugins/filebrowser/view.go`

In `renderView()` (~line 34), add condition:

```go
if p.projectSearch.Active {
    return p.renderProjectSearchView()
}
```

### `internal/styles/styles.go`

Add styles:

```go
ToggleButton       // Inactive toggle (muted fg, tertiary bg)
ToggleButtonActive // Active toggle (primary fg, primary bg, bold)
SearchResultFile   // File path in results (secondary, bold)
SearchResultLineNo // Line number (muted, right-aligned)
SearchResultMatch  // Match highlight (warning bg)
```

---

## Key Implementation Details

### Ripgrep Command

```bash
rg --json --line-number --column --max-count 1000 --max-filesize 1M \
   [--case-sensitive | --ignore-case] \
   [--word-regexp] \
   [--fixed-strings] \
   "query" .
```

### Flattened Navigation

Results tree flattened for cursor:

- File headers = 1 item each
- Expanded file's matches = 1 item each
- CursorIndex indexes into flattened list

### Mouse Regions

```go
const (
    regionSearchInput  = "search-input"
    regionSearchFile   = "search-file"   // Data: fileIndex
    regionSearchMatch  = "search-match"  // Data: {fileIdx, matchIdx}
    regionToggleRegex  = "toggle-regex"
    regionToggleCase   = "toggle-case"
    regionToggleWord   = "toggle-word"
)
```

### Keyboard Shortcuts

**Input mode:**

- `Enter` - execute search
- `Esc` - exit (empty=close search, has results=exit input mode)
- `Alt+R` - toggle regex
- `Alt+C` - toggle case sensitive
- `Alt+W` - toggle whole word
- `Backspace` - delete char

**Results mode:**

- `j/k` or arrows - navigate results
- `Enter` on file - toggle expand/collapse
- `Enter` on match - focus preview pane
- `l/right` - focus preview pane
- `/` or `ctrl+s` - re-enter input mode
- `Esc` - exit project search
- `e/o` - open in editor at line

---

## Implementation Phases

### Phase 1: Core Infrastructure

- Create `project_search.go` with data structures
- Add `ProjectSearchState` to Plugin struct
- Add `ctrl+s` trigger
- Add focus context routing

### Phase 2: Ripgrep Integration

- Implement command builder with options
- Implement JSON output parser
- Add async execution with messages
- Handle errors (rg not found, no matches)

### Phase 3: View Rendering

- Create `project_search_view.go`
- Render search input bar with toggle buttons
- Render collapsible results tree
- Implement match highlighting in lines

### Phase 4: Navigation & Preview

- Flatten results for cursor navigation
- Implement expand/collapse on files
- Load preview with match highlighted
- Scroll preview to match line

### Phase 5: Mouse Support

- Add HitMap for toggle buttons
- Add HitMap for file/match rows
- Implement click handlers
- Implement double-click (expand/open editor)

### Phase 6: Polish

- Loading indicator during search
- Error display
- Commands() for footer hints
- Tests

---

## Critical Files Reference

| File                                     | Purpose                                 |
| ---------------------------------------- | --------------------------------------- |
| `internal/plugins/filebrowser/plugin.go` | Add state, keybindings, message routing |
| `internal/plugins/filebrowser/view.go`   | Conditional render for search mode      |
| `internal/mouse/mouse.go`                | HitMap/Handler patterns to follow       |
| `internal/styles/styles.go`              | Add toggle/result styles                |
| `internal/plugins/gitstatus/mouse.go`    | Reference for mouse region usage        |

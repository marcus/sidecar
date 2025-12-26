# Plan: Beautiful Git Diffs for Sidecar

## Goal

Enhance the git plugin to show IDE-quality diffs with:

- Line-by-line and side-by-side diff views (toggle between them)
- Commit history browser
- External tool integration (delta) with graceful fallback

## Implementation Phases

### Phase 1: External Tool Detection & Integration

**Files to create:**

- `internal/plugins/gitstatus/external_tools.go` (~100 lines)

**Tasks:**

1. Detect if `delta` is installed via `exec.LookPath("delta")`
2. Create `RenderDiff()` function to pipe raw diff through delta
3. Show one-time recommendation banner if not installed: `"Tip: Install delta for enhanced diffs: brew install git-delta"`
4. Add config option `ExternalTool: "auto" | "delta" | "builtin"`

**Why delta first:** Gives immediate value with minimal code. Users get beautiful diffs instantly if delta is installed.

---

### Phase 2: Structured Diff Parsing

**Files to create:**

- `internal/plugins/gitstatus/diff_parser.go` (~300 lines)

**Data structures:**

```go
type ParsedDiff struct {
    OldFile, NewFile string
    Hunks            []Hunk
}
type Hunk struct {
    OldStart, OldCount, NewStart, NewCount int
    Lines []DiffLine
}
type DiffLine struct {
    Type      LineType // Context, Add, Remove
    OldLineNo, NewLineNo int
    Content   string
    WordDiff  []WordSegment // For word-level highlighting
}
```

**Tasks:**

1. Parse unified diff format into `ParsedDiff`
2. Implement word-level diff using simple LCS algorithm
3. Handle edge cases (binary files, empty diffs)

**Dependencies:** Use `sourcegraph/go-diff` for robust parsing

---

### Phase 3: Enhanced Diff Renderer (Built-in Fallback)

**Files to create:**

- `internal/plugins/gitstatus/diff_renderer.go` (~400 lines)

**Files to modify:**

- `internal/styles/styles.go` - Add `DiffLineNumber`, `DiffWordAdd`, `DiffWordRemove`

**Tasks:**

1. `RenderLineDiff()` - Line-by-line with line numbers, word-level highlighting
2. `RenderSideBySide()` - Split view using `lipgloss.JoinHorizontal()`
3. Integrate syntax highlighting via `alecthomas/chroma` (optional stretch goal)

---

### Phase 4: Toggle Diff View Modes

**Files to modify:**

- `internal/plugins/gitstatus/plugin.go` - Add `diffViewMode` state
- `internal/plugins/gitstatus/view.go` - Dispatch to appropriate renderer

**New keybinding:** `v` to toggle line/side-by-side

**State additions to Plugin struct:**

```go
diffViewMode    DiffViewMode // LineDiff, SideDiff
horizontalOff   int          // For side-by-side horizontal scroll
parsedDiff      *ParsedDiff
externalTool    *ExternalTool
```

---

### Phase 5: Commit History Browser

**Files to create:**

- `internal/plugins/gitstatus/history.go` (~200 lines)
- `internal/plugins/gitstatus/history_view.go` (~250 lines)

**Data structures:**

```go
type Commit struct {
    Hash, ShortHash, Author, AuthorEmail string
    Date time.Time
    Subject, Body string
    Files []CommitFile
    Stats CommitStats
}
```

**Git commands:**

```bash
git log --format="%H%x00%h%x00%an%x00%ae%x00%at%x00%s" -n 50
git show --stat --format="%H%n%an%n%ae%n%at%n%s%n%b" <hash>
```

**New keybinding:** `h` to toggle history view from status

**Views:**

1. **Commit list** - Navigate commits with j/k, press enter for detail
2. **Commit detail** - Shows commit metadata + files changed, press d/enter to view file diff

---

### Phase 6: View Mode State Machine

**Files to modify:**

- `internal/plugins/gitstatus/plugin.go`

**State machine:**

```go
type ViewMode int
const (
    ViewModeStatus      ViewMode = iota // Current file list
    ViewModeHistory                      // Commit browser
    ViewModeCommitDetail                 // Single commit files
    ViewModeDiff                         // Enhanced diff view
)
```

**Navigation flow:**

```
Status --[h]--> History --[enter]--> CommitDetail --[d]--> Diff
   |                                                          |
   +------------------[d]---------------------------------->--+
```

---

## File Summary

| File                          | Action | Lines                           |
| ----------------------------- | ------ | ------------------------------- |
| `gitstatus/external_tools.go` | CREATE | ~100                            |
| `gitstatus/diff_parser.go`    | CREATE | ~300                            |
| `gitstatus/diff_renderer.go`  | CREATE | ~400                            |
| `gitstatus/history.go`        | CREATE | ~200                            |
| `gitstatus/history_view.go`   | CREATE | ~250                            |
| `gitstatus/plugin.go`         | MODIFY | Add view mode state, messages   |
| `gitstatus/view.go`           | MODIFY | Dispatch to new views           |
| `gitstatus/diff.go`           | MODIFY | Add commit diff functions       |
| `styles/styles.go`            | MODIFY | Add new diff styles             |
| `go.mod`                      | MODIFY | Add sourcegraph/go-diff, chroma |

---

## Keybindings Summary

| Key     | Context                        | Action                   |
| ------- | ------------------------------ | ------------------------ |
| `h`     | git-status                     | Toggle history view      |
| `v`     | git-diff                       | Toggle line/side-by-side |
| `d`     | git-history, git-commit-detail | Show diff                |
| `<`/`>` | git-diff (side-by-side)        | Horizontal scroll        |
| `esc`   | any modal                      | Back to previous view    |

---

## Dependencies to Add

```go
require (
    github.com/sourcegraph/go-diff v0.7.0
    github.com/alecthomas/chroma/v2 v2.12.0  // Optional for syntax hl
)
```

---

## Priority Order

1. **Phase 1** (External tools) - Immediate value, minimal code
2. **Phase 5+6** (History browser + state machine) - Core new feature
3. **Phases 2-4** (Built-in diff rendering) - Fallback when delta not installed

This order delivers value quickly while building toward full functionality.

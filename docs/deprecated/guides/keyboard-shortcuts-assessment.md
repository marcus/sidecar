# Keyboard Shortcuts Assessment and Improvement Plan

This document provides a comprehensive review of keyboard shortcuts across all sidecar plugins, identifying inconsistencies, alignment with vim patterns, mnemonic quality, and architectural improvements.

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Current Architecture Review](#current-architecture-review)
3. [Inconsistency Analysis](#inconsistency-analysis)
4. [Vim Pattern Alignment](#vim-pattern-alignment)
5. [Mnemonic Analysis](#mnemonic-analysis)
6. [Proposed Unified Shortcut Scheme](#proposed-unified-shortcut-scheme)
7. [Architectural Improvements](#architectural-improvements)
8. [Implementation Plan](#implementation-plan)
9. [Migration Strategy](#migration-strategy)

---

## Executive Summary

### Key Findings

1. **Inconsistent action mappings**: The same key performs different actions across plugins (e.g., `d` = delete in file-browser, diff in git, detail in conversations)
2. **Vim pattern violations**: Some vim conventions are followed (j/k, G, ctrl+d/u) but others are ignored or inconsistently applied
3. **Mnemonic conflicts**: Several shortcuts lack intuitive mnemonics or conflict with established patterns
4. **Modifier inconsistency**: Shift modifiers are used inconsistently (S=stage-all vs s=stage, but D=discard vs d=diff)
5. **Context fragmentation**: Too many narrow contexts create binding duplication and maintenance burden

### Recommended Priority Actions

1. Standardize primary action keys across all plugins
2. Adopt consistent vim-style navigation patterns
3. Create logical mnemonic groupings with shift/ctrl modifiers
4. Consolidate similar contexts to reduce binding duplication
5. Add a "shortcut discovery" mode for new users

---

## Current Architecture Review

### Strengths

- **Centralized binding registry** (`internal/keymap/bindings.go`) provides single source of truth
- **Context-based dispatch** allows plugin-specific overrides
- **Command palette** (`?`) provides discoverability
- **Plugin isolation** via `FocusContext()` prevents cross-plugin conflicts
- **User overrides** supported via config file
- **Key sequence support** (e.g., `g g`) enables vim-style compound commands

### Weaknesses

- **No binding conflict detection** - overlapping keys in same context not flagged
- **No shortcut category system** - actions aren't grouped by function type
- **Limited modifier usage** - ctrl/alt/shift underutilized for related actions
- **Missing "which-key" style help** - no inline hint when pressing leader keys
- **No layer/mode system** - can't define temporary command layers

---

## Inconsistency Analysis

### Critical Conflicts: Same Key, Different Actions

| Key | Git Status | File Browser | Workspace | Conversations | Issue |
|-----|------------|--------------|-----------|---------------|-------|
| `d` | show-diff | delete | show-diff | delete-session | **Major conflict**: destructive vs view action |
| `s` | stage-file | sort | start-agent | toggle-star | Different semantic domains |
| `p` | filter-path | paste | push | - | Clipboard vs action conflict |
| `m` | - | move | merge-workflow | - | File op vs git op |
| `f` | fetch | project-search | filter | filter | Inconsistent: fetch is outlier |
| `h` | show-history | - | - | - | Underused globally |
| `r` | refresh | refresh | refresh | rename-session | Mostly consistent except conversations |
| `n` | next-match | - | new-workspace | - | Different meanings |
| `v` | toggle-graph | - | toggle-view | toggle-view | Mostly consistent |
| `e` | - | edit | - | export-session | Different meanings |

### Shift Modifier Inconsistencies

| Base | Shifted | Git Status | File Browser | Workspace | Pattern Issue |
|------|---------|------------|--------------|-----------|---------------|
| `s`/`S` | stage/stage-all | Yes | sort/- | start/stop | No pattern |
| `u`/`U` | unstage/unstage-all | Yes | - | - | Git only |
| `d`/`D` | diff/discard | Yes (inconsistent) | delete/- | diff/delete | Confusing |
| `y`/`Y` | yank/yank-path | Yes | yank/copy-path | approve/approve-all | Inconsistent meanings |
| `n`/`N` | next/prev-match | Yes | next/prev-match | new/reject | Conflict: N means different things |
| `r`/`R` | refresh/- | - | -/rename | -/resume | Underused |

### Navigation Inconsistencies

| Action | Standard Key | Exceptions |
|--------|--------------|------------|
| Scroll down | `j` | conversations-main uses `j` for generic "scroll" |
| Scroll up | `k` | conversations-main uses `k` for generic "scroll" |
| Page down | `ctrl+d` | Consistent |
| Page up | `ctrl+u` | Consistent |
| Go to top | `g g` | conversations-main uses just `g` (vim violation) |
| Go to bottom | `G` | Consistent |
| Back/close | `esc` | Some contexts also use `q`, `h` |
| Focus left | `h` | Sometimes `left`, sometimes `tab` |
| Focus right | `l` | Sometimes `right`, sometimes `tab` |

---

## Vim Pattern Alignment

### Well-Aligned Patterns

| Vim Pattern | Current Implementation | Status |
|-------------|------------------------|--------|
| `j`/`k` for line movement | cursor-down/cursor-up | OK |
| `ctrl+d`/`ctrl+u` for page | page-down/page-up | OK |
| `G` for end of file | cursor-bottom | OK |
| `gg` for start of file | cursor-top (as `g g`) | OK |
| `y` for yank | yank (copy) operations | OK |
| `/` for search | search in most contexts | OK |
| `n`/`N` for next/prev match | next-match/prev-match | OK |
| `h`/`l` for left/right | focus-left/focus-right | Partial |

### Missing Vim Patterns (Should Implement)

| Vim Pattern | Action | Current State | Recommendation |
|-------------|--------|---------------|----------------|
| `dd` | Delete line/item | `d` (immediate) | Add `dd` for delete with confirmation |
| `yy` | Yank line | `y` (immediate) | Consider `yy` for yank-all |
| `o`/`O` | Open below/above | `o` = open-in-github | Conflict - keep current |
| `w`/`b` | Word forward/back | Not used | Add for tree navigation (next/prev sibling) |
| `{`/`}` | Paragraph jump | `[`/`]` for files | Use `{`/`}` for section jump in diffs |
| `zz`/`zt`/`zb` | Center/top/bottom | Not used | Add for diff/preview panes |
| `*`/`#` | Search word forward/back | Not used | Add for search current word |
| `:` | Command mode | `?` opens palette | Consider `:` as alias |
| `m`+letter | Set mark | Not used | Could mark files/commits for batch ops |
| `'`+letter | Jump to mark | Not used | Pair with above |

### Vim Patterns to Avoid

| Vim Pattern | Reason to Skip |
|-------------|----------------|
| `x` delete char | Conflicts with close-tab |
| `i`/`a` insert mode | Not applicable (no text editing in most views) |
| `v` visual mode | Would conflict with toggle-view |
| `c` change | Would conflict with commit |

---

## Mnemonic Analysis

### Strong Mnemonics (Keep)

| Key | Command | Mnemonic | Quality |
|-----|---------|----------|---------|
| `s` | **s**tage | Obvious | Excellent |
| `u` | **u**nstage | Obvious | Excellent |
| `c` | **c**ommit | Obvious | Excellent |
| `P` | **P**ush | Obvious (shifted = remote) | Good |
| `L` | pul**L** | Fair (shifted = remote) | Acceptable |
| `f` | **f**etch | Obvious | Good |
| `b` | **b**ranch | Obvious | Good |
| `z` | sta**s**h (vi**z**ualized as Z) | Weak but memorable | Acceptable |
| `h` | **h**istory | Obvious | Good |
| `d` | **d**iff | Obvious | Good |
| `r` | **r**efresh | Obvious | Excellent |
| `y` | **y**ank | Vim standard | Good |
| `a` | **a**dd (create file) | Obvious | Good |
| `t` | **t**ab | Obvious | Good |
| `/` | search | Vim standard | Excellent |
| `?` | help/palette | Vim standard | Excellent |
| `!` | diagnostics/shell | Unix convention | Good |
| `@` | project (at sign ≈ location) | Moderate | Acceptable |
| `\` | toggle sidebar | Visual (vertical bar) | Good |

### Weak/Conflicting Mnemonics (Fix)

| Key | Current Commands | Issue | Recommendation |
|-----|------------------|-------|----------------|
| `d` | diff/delete | **D**estroy vs **D**iff conflict | Use `x` for delete, `d` for diff |
| `D` | discard/delete | Uppercase meaning varies | Standardize: `D` = **D**iscard/delete (destructive) |
| `n` | new/next-match | **N**ew vs **N**ext | Use `+` or `ctrl+n` for new, keep `n` for next |
| `N` | reject/prev-match | **N**o vs previous | Standardize: `N` = prev-match only |
| `p` | paste/push/path-filter | Too many meanings | `p` = **p**aste, `P` = **P**ush, `ctrl+p` = **p**ath |
| `m` | move/merge/markdown | Too many meanings | `m` = **m**ove (file op), `M` = **M**erge |
| `e` | edit/export | Different domains | `e` = **e**dit, `E` = **E**xport |
| `o` | open-github/open-file | Ambiguous "open" | `o` = **o**pen (primary), `O` = **O**pen (alt target) |
| `A` | amend/show-analytics | Different domains | `A` = **A**mend (git), use `$` for analytics |
| `T` | link-task | Weak - why uppercase? | Keep lowercase `t` context-dependent |
| `K` | kill-shell | **K**ill is good | OK - destructive action is uppercase |
| `v` | view/graph toggle | Overloaded | OK - context-dependent is acceptable |

### Proposed Mnemonic System

#### Uppercase = Force/Destructive/All

| Pair | Lowercase | Uppercase |
|------|-----------|-----------|
| `s`/`S` | stage one | **S**tage all |
| `u`/`U` | unstage one | **U**nstage all |
| `d`/`D` | diff/detail | **D**iscard/**D**elete |
| `y`/`Y` | yank item | **Y**ank path/full |
| `r`/`R` | refresh | **R**ename |
| `a`/`A` | add file | **A**mend (git) |
| `p`/`P` | paste | **P**ush |
| `l`/`L` | focus-left | pul**L** |
| `e`/`E` | edit | **E**dit external |

#### Ctrl = Alternative/Quick Access

| Key | Command | Rationale |
|-----|---------|-----------|
| `ctrl+p` | quick-open | **P**ath jump (VS Code convention) |
| `ctrl+s` | project-search | **S**earch project (VS Code convention) |
| `ctrl+r` | reveal in finder | **R**eveal |
| `ctrl+g` | go to line | **G**o (vim style) |
| `ctrl+e` | open in editor | **E**ditor |
| `ctrl+n`/`ctrl+p` | cursor down/up | Emacs navigation fallback |
| `ctrl+d`/`ctrl+u` | page down/up | Vim scroll |
| `ctrl+c` | quit/cancel | Universal |

#### Alt = Toggles/Modifiers

| Key | Command | Rationale |
|-----|---------|-----------|
| `alt+r` | toggle-regex | **R**egex mode |
| `alt+c` | toggle-case | **C**ase sensitivity |
| `alt+w` | toggle-word | **W**ord match |

---

## Proposed Unified Shortcut Scheme

### Global Shortcuts (All Contexts)

| Key | Command | Mnemonic |
|-----|---------|----------|
| `j`/`k` | cursor-down/up | Vim standard |
| `down`/`up` | cursor-down/up | Arrow fallback |
| `ctrl+d`/`ctrl+u` | page-down/up | Vim standard |
| `g g` | cursor-top | Vim standard |
| `G` | cursor-bottom | Vim standard |
| `enter` | select/open | Universal |
| `esc` | back/cancel | Universal |
| `q` | quit (root only) | Vim standard |
| `ctrl+c` | force-quit | Universal |
| `` ` `` | next-plugin | Adjacent to 1-5 |
| `~` | prev-plugin | Shift of `` ` `` |
| `1-5` | focus-plugin-N | Direct access |
| `?` | command-palette | Vim help |
| `!` | diagnostics | Unix shell convention |
| `@` | switch-project | "At" a project |
| `#` | theme-switcher | Style/number sign |
| `r` | refresh | **R**efresh |

### Two-Pane Navigation (Git, Files, Workspace, Conversations)

| Key | Command | Mnemonic |
|-----|---------|----------|
| `tab`/`shift+tab` | switch-pane | Standard |
| `h`/`l` | focus-left/right | Vim horizontal |
| `left`/`right` | focus-left/right | Arrow fallback |
| `\` | toggle-sidebar | Vertical divider visual |

### List Operations (Standardized)

| Key | Command | Context | Mnemonic |
|-----|---------|---------|----------|
| `/` | search/filter | All lists | Vim search |
| `n`/`N` | next/prev-match | After search | Vim next |
| `y` | yank item info | All lists | Vim yank |
| `Y` | yank path/id | All lists | Full yank |
| `x` | close/delete | Tabs, items | E**x**it/remove |
| `d` | detail/diff | View more | **D**etail |
| `D` | discard/delete | Destructive | **D**estroy |

### Git Status Shortcuts (Proposed)

| Key | Command | Current | Change | Mnemonic |
|-----|---------|---------|--------|----------|
| `s` | stage-file | Same | - | **S**tage |
| `u` | unstage-file | Same | - | **U**nstage |
| `S` | stage-all | Same | - | **S**tage all |
| `U` | unstage-all | Same | - | **U**nstage all |
| `c` | commit | Same | - | **C**ommit |
| `A` | amend | Same | - | **A**mend |
| `d` | show-diff | Same | - | **D**iff |
| `D` | discard-changes | Same | - | **D**iscard |
| `h` | show-history | Same | - | **H**istory |
| `P` | push | Same | - | **P**ush |
| `L` | pull | Same | - | Pul**L** |
| `f` | fetch | Same | - | **F**etch |
| `b` | branch | Same | - | **B**ranch |
| `z` | stash | Same | - | Sta**z**h visual |
| `Z` | stash-pop | Same | - | Pop sta**z**h |
| `o` | open-in-github | Same | - | **O**pen |
| `O` | open-in-file-browser | Same | - | **O**pen alt |

### File Browser Shortcuts (Proposed)

| Key | Command | Current | Change | Mnemonic |
|-----|---------|---------|--------|----------|
| `/` | search-tree | Same | - | Vim search |
| `ctrl+p` | quick-open | Same | - | VS Code |
| `ctrl+s` | project-search | `f` | Change from `f` | VS Code |
| `a` | create-file | Same | - | **A**dd |
| `A` | create-dir | Same | - | **A**dd dir |
| `x` | delete | `d` | Change from `d` | E**x**terminate |
| `y` | yank | Same | - | **Y**ank |
| `Y` | copy-path | Same | - | **Y**ank full |
| `p` | paste | Same | - | **P**aste |
| `s` | sort | Same | - | **S**ort |
| `m` | move | Same | - | **M**ove |
| `R` | rename | Same | - | **R**ename |
| `e` | edit | Same | - | **E**dit |
| `E` | edit-external | Same | - | **E**dit (external) |
| `t` | new-tab | Same | - | **T**ab |
| `[`/`]` | prev/next-tab | Same | - | Bracket navigation |
| `ctrl+r` | reveal | Same | - | **R**eveal |
| `I` | info | Same | - | **I**nfo |
| `B` | blame | Same | - | **B**lame |
| `H` | toggle-ignored | Same | - | **H**idden/ignored |

### Workspace Shortcuts (Proposed)

| Key | Command | Current | Change | Mnemonic |
|-----|---------|---------|--------|----------|
| `n` | new-workspace | Same | - | **N**ew |
| `v` | toggle-view | Same | - | **V**iew |
| `D` | delete-workspace | Same | - | **D**elete |
| `d` | show-diff | Same | - | **D**iff |
| `p` | push | Same | - | **P**ush |
| `m` | merge-workflow | Same | - | **M**erge |
| `t` | attach | Same | - | **T**mux |
| `T` | link-task | Same | - | **T**ask |
| `s` | start-agent | Same | - | **S**tart |
| `S` | stop-agent | Same | - | **S**top |
| `y` | approve | Same | - | **Y**es |
| `N` | reject | - | Change from `N` | **N**o |
| `K` | kill-shell | Same | - | **K**ill |
| `O` | open-in-git | Same | - | **O**pen |
| `enter` | interactive | Same | - | Enter session |

**Note on `y`/`N` for approve/reject**: This deviates from yank semantics. Consider using `a`/**A**pprove and `ctrl+x`/reject or a dedicated confirm dialog instead.

### Conversations Shortcuts (Proposed)

| Key | Command | Current | Change | Mnemonic |
|-----|---------|---------|--------|----------|
| `a` | new-session | Same | - | **A**dd |
| `x` | delete-session | `d` | Change from `d` | E**x**terminate |
| `r` | rename-session | Same | - | **R**ename |
| `e` | export-session | Same | - | **E**xport |
| `c` | copy-session | Same | - | **C**opy |
| `f` | filter | Same | - | **F**ilter |
| `/` | search | Same | - | Vim search |
| `s` | toggle-star | Same | - | **S**tar |
| `$` | show-analytics | `A` | Change from `A` | Money/stats |
| `v` | toggle-view | Same | - | **V**iew |
| `y` | yank-details | Same | - | **Y**ank |
| `Y` | yank-resume | Same | - | **Y**ank command |
| `R` | resume-in-workspace | Same | - | **R**esume |
| `F` | content-search | New | Add | **F**ind in content |

---

## Architectural Improvements

### 1. Introduce Command Categories

Add semantic categories to bindings for better organization and conflict detection:

```go
type CommandCategory string

const (
    CatNavigation   CommandCategory = "navigation"   // j, k, h, l, g, G
    CatSelection    CommandCategory = "selection"    // enter, space
    CatEdit         CommandCategory = "edit"         // a, d, x, p, y
    CatGit          CommandCategory = "git"          // s, u, c, P, L, f
    CatView         CommandCategory = "view"         // v, \, tab
    CatSearch       CommandCategory = "search"       // /, n, N
    CatSystem       CommandCategory = "system"       // q, ?, !, @, r
)
```

### 2. Add Binding Conflict Detection

Implement compile-time or runtime validation:

```go
func (r *Registry) ValidateBindings() []ConflictError {
    // Detect same key in same context
    // Detect semantic conflicts (destructive on non-shifted key)
    // Warn on mnemonic violations
}
```

### 3. Implement "Which-Key" Style Help

When a leader key is pressed (like `g`), show available completions:

```
┌─────────────────────────┐
│ g ...                   │
│ ─────────────────────── │
│ g  → go to top          │
│ h  → open in GitHub     │
│ f  → open in files      │
│ b  → git blame          │
└─────────────────────────┘
```

This requires:
- Tracking pending key sequences
- Timeout-based disambiguation (already exists: 500ms)
- Popup component for showing options

### 4. Add Shortcut Layers/Modes

Allow plugins to define temporary command layers:

```go
type Layer struct {
    Name     string
    Bindings []Binding
    Timeout  time.Duration // 0 = sticky until esc
}

// Example: "g" prefix layer for git operations
gitLayer := Layer{
    Name: "git",
    Bindings: []Binding{
        {Key: "h", Command: "open-in-github"},
        {Key: "f", Command: "open-in-files"},
        {Key: "b", Command: "git-blame"},
    },
    Timeout: 500 * time.Millisecond,
}
```

### 5. Consolidate Similar Contexts

Reduce context fragmentation by merging related contexts:

| Current Contexts | Proposed Merged Context | Rationale |
|------------------|------------------------|-----------|
| `git-status`, `git-status-commits`, `git-status-diff` | `git-status` with sub-modes | Same plugin, shared shortcuts |
| `file-browser-tree`, `file-browser-preview` | `file-browser` with pane focus | Same plugin |
| `workspace-list`, `workspace-preview` | `workspace` with pane focus | Same plugin |
| `conversations-sidebar`, `conversations-main` | `conversations` with pane focus | Same plugin |

Benefits:
- Fewer bindings to maintain
- Consistent shortcuts within plugin
- Simpler mental model for users

### 6. Add User-Facing Shortcut Cheat Sheet

Auto-generate a printable/viewable cheat sheet from bindings:

```
$ sidecar shortcuts --format=markdown > shortcuts.md
$ sidecar shortcuts --format=pdf > shortcuts.pdf
```

Or in-app: `?` then `?` again = full cheat sheet view

### 7. Implement Shortcut Recording/Macros

Allow users to record and replay command sequences:

```
qq          → start recording to register q
[commands]
q           → stop recording
@q          → replay register q
@@          → replay last macro
```

This is advanced but aligns with vim philosophy.

---

## Implementation Plan

### Phase 1: Quick Wins (Low Risk)

1. **Fix `d` conflict**: Change file-browser delete to `x`
2. **Standardize search key**: Add `ctrl+s` alias to project-search
3. **Fix analytics key**: Change from `A` to `$` in conversations
4. **Add binding validation**: Implement conflict detection in tests
5. **Update documentation**: Reflect all current shortcuts accurately

### Phase 2: Mnemonic Improvements (Medium Risk)

1. **Standardize shift behavior**: Uppercase = force/all/destructive
2. **Add missing vim patterns**: `{`/`}` for section navigation, `zz` for centering
3. **Implement which-key hints**: Show available commands after leader key
4. **Consolidate contexts**: Merge related contexts per plugin

### Phase 3: Architecture Enhancements (Higher Risk)

1. **Command categories**: Add semantic grouping
2. **Layer/mode system**: Support temporary command layers
3. **Macro recording**: Implement vim-style macro system
4. **Cheat sheet generation**: Auto-generate documentation

---

## Migration Strategy

### Backward Compatibility

1. **Config migration**: Add `keymap.legacy_mode: true` option that preserves old bindings
2. **Deprecation warnings**: Show notice when deprecated shortcuts are used
3. **Gradual rollout**: Introduce changes over 2-3 releases

### User Communication

1. **Changelog entry**: Detailed shortcut changes in release notes
2. **In-app notice**: One-time popup explaining changes on upgrade
3. **Palette hints**: Show both old and new bindings during transition

### Rollback Plan

1. **Version pinning**: Users can stay on old version
2. **Override config**: All bindings customizable via user config
3. **Feature flag**: Enable/disable new scheme via config

---

## Appendix A: Complete Binding Comparison Table

| Key | Global | Git | Files | Workspace | Conversations | TD | Proposed Standard |
|-----|--------|-----|-------|-----------|---------------|-----|-------------------|
| `j` | cursor-down | cursor-down | cursor-down | cursor-down | scroll | cursor-down | cursor-down |
| `k` | cursor-up | cursor-up | cursor-up | cursor-up | scroll | cursor-up | cursor-up |
| `h` | - | history | - | focus-left | focus-left | - | focus-left/history |
| `l` | - | - | - | focus-right | focus-right | - | focus-right |
| `g g` | cursor-top | - | - | - | cursor-top | - | cursor-top |
| `G` | cursor-bottom | - | - | - | cursor-bottom | - | cursor-bottom |
| `d` | - | show-diff | delete | show-diff | delete-session | - | diff/detail |
| `D` | - | discard | - | delete | - | - | delete/discard |
| `s` | - | stage | sort | start-agent | toggle-star | - | context-specific |
| `S` | - | stage-all | - | stop-agent | - | - | force/all variant |
| `u` | - | unstage | - | - | - | - | unstage/undo |
| `U` | - | unstage-all | - | - | - | - | force/all variant |
| `y` | - | yank | yank | approve | yank | - | yank |
| `Y` | - | yank-path | copy-path | approve-all | yank-resume | - | yank-full |
| `n` | - | next-match | - | new | - | - | next-match |
| `N` | - | prev-match | prev-match | reject | - | - | prev-match |
| `p` | - | filter-path | paste | push | - | - | paste |
| `P` | - | push | - | - | - | - | push |
| `f` | - | fetch | project-search | - | filter | - | fetch/filter |
| `r` | refresh | refresh | refresh | refresh | rename | refresh | refresh |
| `R` | - | - | rename | resume | - | - | rename |
| `c` | - | commit | - | - | copy | - | commit/copy |
| `e` | - | - | edit | - | export | - | edit |
| `a` | - | - | create-file | - | new-session | - | add/create |
| `A` | - | amend | create-dir | - | analytics | - | amend/add-dir |
| `t` | - | - | new-tab | attach | - | - | tab/tmux |
| `T` | - | - | - | link-task | - | - | task |
| `x` | - | - | close-tab | - | - | - | close/delete |
| `v` | - | toggle-graph | - | toggle-view | toggle-view | - | toggle-view |
| `m` | - | - | move | merge | - | - | move/merge |
| `b` | - | branch | - | - | - | - | branch |
| `z` | - | stash | - | - | - | - | stash |
| `Z` | - | stash-pop | - | - | - | - | stash-pop |
| `o` | - | open-github | - | - | - | - | open |
| `O` | - | open-file-browser | - | open-git | - | - | open-alt |
| `/` | - | search-history | search | - | search | search | search |
| `?` | toggle-palette | - | - | - | - | - | help/palette |
| `!` | diagnostics | - | - | - | - | - | diagnostics |
| `@` | switch-project | - | - | - | - | - | project |
| `\` | - | toggle-sidebar | toggle-sidebar | toggle-sidebar | toggle-sidebar | - | toggle-sidebar |
| `[` | - | prev-file | prev-tab | prev-tab | - | - | prev |
| `]` | - | next-file | next-tab | next-tab | - | - | next |
| `tab` | - | switch-pane | switch-pane | switch-pane | switch-pane | - | switch-pane |
| `enter` | select | show-diff | open | interactive | view | select | select/open |
| `esc` | back | close | back | back | back | close | back/close |
| `q` | quit | close-diff | - | - | - | - | quit/close |

---

## Appendix B: Vim Reference for Comparison

### Standard Vim Navigation

| Key | Action | Sidecar Equivalent |
|-----|--------|-------------------|
| `h/j/k/l` | Left/Down/Up/Right | ✓ (partially) |
| `w/b` | Word forward/back | Not implemented |
| `0/$` | Line start/end | Not implemented |
| `gg/G` | File start/end | ✓ |
| `ctrl+d/u` | Page down/up | ✓ |
| `ctrl+f/b` | Page down/up (alt) | Not implemented |
| `zz/zt/zb` | Center/top/bottom | Not implemented |
| `H/M/L` | Screen top/mid/bottom | Not implemented |

### Standard Vim Editing

| Key | Action | Sidecar Equivalent |
|-----|--------|-------------------|
| `y` | Yank | ✓ (copy) |
| `d` | Delete | ✓ (delete in files) |
| `p` | Put/Paste | ✓ (paste) |
| `u` | Undo | ✓ (unstage in git) |
| `x` | Delete char | ✓ (close tab) |
| `r` | Replace | ✓ (refresh/rename) |
| `c` | Change | ✓ (commit) |

### Standard Vim Search

| Key | Action | Sidecar Equivalent |
|-----|--------|-------------------|
| `/` | Search forward | ✓ |
| `?` | Search backward | Used for palette |
| `n/N` | Next/prev match | ✓ |
| `*/#` | Search word | Not implemented |

---

## Appendix C: Comparison with Popular TUIs

### lazygit

| Action | lazygit | sidecar | Notes |
|--------|---------|---------|-------|
| Stage | `space` | `s` | Different |
| Commit | `c` | `c` | Same |
| Push | `P` | `P` | Same |
| Pull | `p` | `L` | Different |
| Stash | `s` | `z` | Different |
| Refresh | `R` | `r` | Same (case) |
| Quit | `q` | `q` | Same |

### ranger (file manager)

| Action | ranger | sidecar | Notes |
|--------|--------|---------|-------|
| Open | `l`/`enter` | `enter` | Similar |
| Back | `h` | `esc`/`h` | Similar |
| Delete | `dd` | `d` | Different |
| Yank | `yy` | `y` | Similar |
| Paste | `pp` | `p` | Similar |
| Rename | `cw` | `R` | Different |
| Search | `/` | `/` | Same |

### Recommendations from Comparison

1. Consider `space` for primary action (stage/select) - very accessible
2. Use `dd` pattern for destructive operations - muscle memory protection
3. Pull should perhaps be `p` (lowercase) with push as `P` (uppercase) - more intuitive
4. Consider double-key patterns (`yy`, `dd`, `pp`) for safer operations

---

*Document generated: 2026-01-28*
*Applies to: sidecar v0.51.0*

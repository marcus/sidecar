# Sidebar List Implementation Guide

This guide documents the conventions and pitfalls for building solid, scrollable sidebar lists in sidecar plugins. Use it as a checklist when adding new list-based UI.

## Goals

- Scrolling is stable: no jumping, no flicker, no disappearing rows.
- Lists fill the available space without extra blank rows.
- Cursor movement, paging, and mouse hit regions stay aligned with what is rendered.
- Refreshes and async loads do not reset user position or truncate history.

## Layout Accounting (Most Common Source of Bugs)

Sidebar list bugs almost always come from mismatched height math. The renderer and the scroll logic must use the same line budget.

Best practices:

- Compute pane height once at the view layer, then pass `visibleHeight` down into list rendering and sizing logic.
- When using bordered panels with padding, account for the borders and header lines exactly once.
- Treat `visibleHeight` as the post-header content height. Do not subtract header lines again later.
- Track every line the sidebar renders (headers, blank lines, separators, status lines, list rows).
- If a line is rendered, increment the local `currentY` so hit regions line up.
- Truncate headers to the sidebar width to prevent line wrapping from stealing rows.

Typical layout flow:

- `paneHeight := height - 2` (borders)
- `innerHeight := paneHeight - 2` (panel border + header lines)
- Files section reserves ~3 lines minimum, rest for commits

Example with concrete numbers (terminal height 40):
```
height = 40
paneHeight = 40 - 2 = 38        // outer borders
innerHeight = 38 - 2 = 36       // panel border + header
filesSection = 3                 // minimum for "Working tree clean" or staged files
separator = 1
commitsHeader = 1
commitCapacity = 36 - 3 - 1 - 1 = 31
```

A safe pattern is to centralize capacity calculation in a helper (used by both render and scroll logic) and have it consume `innerHeight` directly.

### Capacity Helper Example

The helper should mirror render logic exactly. Test with:
- Clean tree (only "Working tree clean" line)
- Full tree with sections
- Different height budgets (small terminal)

See `gitstatus.commitSectionCapacity()` for reference implementation.

## Scroll + Cursor Stability

- Keep scroll offsets (`scrollOff`, `commitScrollOff`, etc.) clamped after any data change.
- Only adjust scroll if the selected row is outside the visible range.
- Never mutate scroll offsets in `render` functions. Rendering must be pure.
- Use absolute indices for list hit regions to avoid drift when filters or pagination are active.
- Keep `selected` logic based on the same absolute index space used by cursor state.

Explicit scroll clamping pattern:
```go
if p.commitScrollOff > len(commits) - visibleCommits {
    p.commitScrollOff = len(commits) - visibleCommits
}
if p.commitScrollOff < 0 {
    p.commitScrollOff = 0
}
```

Paging keys should work across all lists:

- `j`/`down`, `k`/`up`
- `ctrl+d`, `ctrl+u`
- `g`, `G`

## Async Data + Refresh Safety

Refreshes and background loads often cause disappearing rows or cursor jumps. Avoid replacing the list unless you really intend to.

- When loading more history, append to existing data.
- When refreshing a list, merge by hash/ID to preserve older entries.
- Preserve selection by stable ID (hash), not index.
- If a refresh only affects metadata (like push status), update in place instead of replacing the list.
- After any merge/replace, clamp scroll offsets and cursor ranges immediately. Example: after `loadMoreCommits()`, call `clampCommitScroll()`.
- If the viewport is taller than the initial page size, auto-load additional pages until the list fills or history is exhausted.

## Empty Rows at the Bottom

Extra blank rows appear when the renderer thinks there are fewer visible rows than the scroll logic (or vice versa).

Avoid this by:

- Using one shared capacity calculation for both rendering and scroll clamping.
- Accounting for optional rows (status lines, “empty” placeholders).
- Not double-counting panel headers or borders.
- Clamping `scrollOff` to `len(items) - visibleCount` (minimum 0).
- Loading more data when the list is shorter than the visible window (and more data exists).
- Letting header text wrap into multiple lines (truncate instead).

## Mouse Hit Regions

- Register hit regions after the line is rendered and after updating `currentY`.
- Use absolute indices (not visible indices) so clicks map to the correct item even when scrolled.
- Ensure regions include the same width as the rendered list lines.

Hit region priority example (register in order of increasing priority, last wins):
```go
// Register in order of increasing priority (last wins)
p.HitMap.AddRect(regionSidebar, x, y, w, h, data)        // lowest
p.HitMap.AddRect(regionCommitLine, x, y, w, h, data)     // medium
p.HitMap.AddRect(regionCommitDivider, x, y, w, h, data)  // highest
```

## Footer Hints

- Do not render any footer or hint lines inside plugins.
- Use `Commands()` for key hints so the app renders the unified footer bar.

## Rendering Constraints

- Always constrain output height in `View(width, height int)` using `lipgloss.Height(height)` or a wrapper style.
- Never rely on the app to truncate overflow.
- Avoid mutating plugin state during rendering.

MaxHeight pattern for strict height enforcement:
```go
lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(content)
```

## Testing Guidance

Add targeted tests for list math and scrolling:

- Capacity calculation with file-heavy sections and minimum heights.
- Scroll clamping when the list is shorter than the visible window.
- Merge behavior when a refresh returns a shorter list than what is loaded.
- Selection preservation by hash/ID after refresh.

Keep tests focused on deterministic math or pure helpers. Avoid full render snapshots unless needed.

## Common Pitfalls Checklist

- [ ] Double-counted header lines or borders.
- [ ] Scroll offsets adjusted inside render methods.
- [ ] Refresh replaced list, losing older entries.
- [ ] No clamp after list size changes.
- [ ] Hit regions using visible index instead of absolute index.
- [ ] Placeholder lines (e.g., “Working tree clean”) not counted in height math.

Following these steps will keep sidebar lists stable under heavy scrolling, paging, and refresh.

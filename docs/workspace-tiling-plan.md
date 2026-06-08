# Workspace/Shell Tiling in the Workspaces Tab — Implementation Plan

## 1. Investigation Findings

### 1.1 Workspaces plugin & current `View` structure

The workspaces plugin lives at `internal/plugins/workspace/`. It is by far the largest plugin (many 1000+ line files) and is the only one with interactive tmux input today. Tiling belongs here.

- Plugin struct: `internal/plugins/workspace/plugin.go:121-384`. A single monolithic struct holding selection, sidebar width, preview tab, interactive state, terminal-panel state, diff-tab state, many modal states, etc.
- View entry point: `View(width, height int) string` at `internal/plugins/workspace/view_list.go:47-92`. It dispatches on `p.viewMode` to modal renderers or falls through to `renderListView`.
- Main split view: `renderListView(width, height)` at `view_list.go:95-213`. This is the critical function:
  - Computes pane dimensions: `available := width - dividerWidth`, `sidebarW := (available * p.sidebarWidth) / 100`, clamps, then `previewW := available - sidebarW` (`view_list.go:141-153`).
  - Derives `contentW = paneW - panelOverhead` where `panelOverhead = 4` (2 border + 2 padding). See constants at `view_list.go:40-44`.
  - Registers three hit regions (sidebar, preview, divider) on `p.mouseHandler.HitMap` (`view_list.go:164-186`).
  - Renders sidebar via `renderSidebarContent` and preview via `renderPreviewContent` (`view_preview.go:15-91`), wraps both in `styles.RenderPanel` panels, joins with a `ui.RenderDivider`.
- Preview content dispatcher: `renderPreviewContent` at `view_preview.go:16-91` branches on `p.shellSelected`, `wt.IsMain`, and `p.previewTab` (Output/Diff/Task). This is the function that renders one workspace's pane — the atomic unit that tiling will replicate.
- Output rendering with cursor overlay: `renderOutputContent` (`view_preview.go:222-407`), `renderShellOutput` (`view_preview.go:440-607`). Both read `Agent.OutputBuf.LinesRange(start, end)`, truncate each line to `displayWidth`, optionally inject character-level selection background, overlay cursor at a relative row/col. Display width and height explicitly clamp to `paneWidth`/`paneHeight` when the tmux pane is smaller than the display area (`view_preview.go:285-293, 378-404`).
- The height-constraint rule from `AGENTS.md:29-41` is enforced by the truncation layer and by `renderListView` using `styles.RenderPanel(..., paneHeight, ...)` with a fixed outer height equal to `height`.

Prior art for split/resizable layouts in this plugin:
- Sidebar+preview drag-to-resize: `view_list.go:165-185`, `mouse.go:1362-1378`, persisted via `state.GetWorkspaceSidebarWidth()` / `state.SetWorkspaceSidebarWidth()` (`state.go:286-306`).
- Output/terminal-panel split ("Ctrl+T"): `terminal_panel.go`. This is the closest living example of "two live tmux sessions side-by-side in the same pane". It supports a horizontal or vertical split between the agent session and a second tmux session (`TermPanelLayout` = bottom/right). It has its own divider drag region `regionTermPanelDivider` (plugin.go:105-106), its own focus bit `termPanelFocused`, its own poll generation `termPanelGeneration`, and its own dimension function `calculateAgentPaneDimensions`/`calculateTermPanelDimensions`. **This is the template for tiling**: tiling is the generalization of the terminal-panel mechanism from "1 agent + 1 aux" to "N arbitrary tiles".
- Diff-tab internal split (file list + diff viewer): `diffTabListWidth`, region `regionDiffTabDivider`. Not tmux; just UI panes.

### 1.2 TTY / tmux integration — how sizing and resize really work

Package `internal/tty/`:
- `session.go:70-96` `ResizeTmuxPane(paneID, width, height)`: runs `tmux resize-window -t <pane> -x W -y H`, falls back to `resize-pane` when that fails (older tmux or attached client). No SIGWINCH involvement — sidecar is not a terminal emulator, the child processes live inside tmux which owns the PTY. Resize is purely declarative.
- `session.go:101-103` `SetWindowSizeManual(sessionName)`: sets `window-size manual` on the tmux session so that tmux does not auto-shrink the window to the smallest attached client. **This is a precondition for driving sizes from sidecar.** Any new tile implementation must call this when creating a tile session.
- `session.go:106-125` `QueryPaneSize(target)`: `display-message '#{pane_width},#{pane_height}'` for verification.
- Duplicate, older copy of `resizeTmuxPane`/`queryPaneSize` lives inline in the workspace plugin at `interactive.go:687-736` — the plugin predates the `internal/tty` package and never migrated. That's fine for today; the tiling work should use the `tty` package directly.
- `session.go:143-158` `CapturePaneOutput`: `tmux capture-pane -p -e -t <target> -S -N` — the source of all rendered content for both agents and shells.

Resize triggers today (where tiling must hook in too):
- `tea.WindowSizeMsg` handler: `update.go:23-44` calls `resizeSelectedPaneCmd` (and `resizeTermPanelPaneCmd` if visible).
- Entering interactive mode: `interactive.go:450-470` calls `SetWindowSizeManual` then `resizeTmuxPane` with verify+retry.
- Changing selection: `plugin.go:1171-1174` (`loadSelectedContent` calls `resizeSelectedPaneCmd`).
- Sidebar width changes / toggle / drag end: `keys.go:898-916` (`+`/`-`), `keys.go:918-941` (`\`), `mouse.go:1373-1377` (drag end).
- Terminal panel show/hide/layout switch: `terminal_panel.go:93-136`, `mouse.go:1365-1368`.

Only the **currently visible/focused** pane is resized. Background tmux sessions stay at whatever size they last had — see `resizeSelectedPaneCmd` at `interactive.go:738-746` and `previewResizeTarget` at `interactive.go:787-807`. Tiling fundamentally changes this assumption: multiple tiles are visible simultaneously, so multiple panes must be resized when the layout changes.

Dimension calculation functions to note:
- `calculatePreviewDimensions()` at `interactive.go:554-606` — must stay in sync with `renderListView` width math. This is the point of consistency between "how big do I render" and "how big do I tell tmux to make the pane". **Tiling will need an equivalent per-tile function.**
- `calculateAgentPaneDimensions()` / `calculateTermPanelDimensions()` — layer that splits the preview region in two. The tiling version generalizes this to N regions.

Resize debouncing & stale-poll invalidation (critical for tiling):
- Debounce on interactive resize: `maybeResizeInteractivePane` at `interactive.go:659-685` gates to one resize per 500ms via `InteractiveState.LastResizeAt`. Without this, SIGWINCH-like cascades during a drag cause hundreds of `tmux resize-window` calls and visible flicker.
- Poll generation counters at `plugin.go:163-167`: `pollGeneration map[string]int` (per-workspace) and `shellPollGeneration map[string]int`. Every entry that owns a tmux session has a generation; polls carry the generation at creation time; stale polls are dropped (`agent.go:852-870`). **Tiling must give each tile its own generation bucket.**

Capture & render pipeline (for one pane today):
- Async capture: `AsyncCaptureResultMsg` (`types.go:563-569`) delivered by `scheduleAgentPoll`. Result is stored on `Agent.OutputBuf` which is shared for the whole workspace. Multiple tiles on the same workspace would currently share buffers — fine, but rendering each tile is independent.
- Cursor query: `getCursorPosition()` (via `cursor.go` in `internal/tty`) — only called when a tile is in interactive mode, and only for the single interactive tile.

### 1.3 Input routing & key handling

- Top-level routing: `Update()` at `update.go:19-44` receives `tea.KeyMsg` and delegates through `handleKeyPress` (`keys.go:17-51`), which dispatches on `p.viewMode`.
- The two key-handling paths tiling must modify:
  - `handleListKeys` (`keys.go:562-...`): sidebar navigation, preview scrolling, sidebar/preview focus (`h`/`l`), preview-tab cycling (`[`/`]`), `n` (new), `D` (delete), `t` (attach), `\` (sidebar toggle), `enter` (enter interactive), `+`/`-` (resize sidebar).
  - `handleInteractiveKeys` (`interactive.go:824-...`): intercepts the configured exit (`ctrl+\` by default), attach (`ctrl+]`), copy/paste (`alt+c`/`alt+v`), terminal-panel toggle (`ctrl+t`), double-Escape, and falls through to `MapKeyToTmux` for passthrough.
- Dynamic binding registration happens in `Plugin.Init()` at `plugin.go:487-522`. Contexts: `workspace-interactive`, `workspace-list`, `workspace-preview`, plus modal contexts. The app footer reads `Commands()` filtered by `FocusContext()` (`commands.go:280-319`). **The tile manager needs a new context, e.g. `workspace-tile`, and a prefix-key set of bindings registered there.**
- Today only one tile is ever "interactive" at a time (`p.interactiveState`) and keys are forwarded to its tmux session by `tty.MapKeyToTmux` + `tmux send-keys`. Tiling needs a routing decision: if the key matches the tile-manager prefix (or a direct-bound manager key), consume it; otherwise forward to the focused tile's tmux session.

### 1.4 Mouse support & drag-to-resize

- Generic hit region system: `internal/mouse/mouse.go:26-71` (`HitMap.Add`, `Test` checks in reverse order — last registered wins). Drag state is held on `Handler` (`mouse.go:73-96`). The skill doc `drag-pane/SKILL.md` summarizes the workspace's existing three-region setup (sidebar / preview / divider) and critical rules (clear every render, divider registered last, don't reset width in View).
- Workspace mouse dispatcher: `mouse.go:handleMouse` / `handleMouseClick` / `handleMouseDrag` / `handleMouseDragEnd` at `mouse.go:1362-1378` and earlier in the file for the two existing dividers (`regionPaneDivider`, `regionTermPanelDivider`, `regionDiffTabDivider`). Tiling can reuse the same `mouse.Handler` — add `regionTileDivider` hit regions with `Data` identifying *which* divider (split node ID) is being dragged. The `mouse.StartDrag` / `DragStartValue` flow already supports an `any` value, so we can pass split ratios.
- Mouse click on a tile forwards to the tile's tmux session today only when interactive mode is active and the target app enabled mouse reporting (`mouse.go:520-545`). With tiling, clicking any tile should focus it (manager-level), with the tmux-forward gated behind interactive mode as today.

### 1.5 State persistence

- Global per-plugin user prefs: `State` struct at `state.go:11-35` — flat JSON file `~/.config/sidecar/state.json`. Examples: `WorkspaceSidebarWidth`, `TermPanelSize`, `TermPanelLayout`, `TermPanelVisible`.
- Per-project workspace state: `map[string]WorkspaceState` keyed by `projectRoot`, stored in same file. Today `WorkspaceState` holds `WorkspaceName` / `ShellTmuxName` / `ShellDisplayNames` (`state.go:57-62`).
- Restore paths: `Plugin.Init` (`plugin.go:524-527, 530-531, 535-544`) pulls sidebar width, diff-tab width, term-panel size/layout/visibility on reinit. Per-project selection restore: `restoreSelectionState` at `plugin.go:651-688` runs once on first refresh.
- Shell sessions are separately persisted in `shells.json` under the project's data dir (`shell_manifest.go`, `Plugin.Init` at `plugin.go:467-483`). The same pattern — a project-scoped manifest file — is the right model for persisted tile layouts.

### 1.6 Existing constraints from `AGENTS.md`

- "Never exceed allocated height" — tiles must not blow past `height` in their aggregate render.
- "Never render footers" — footer is app-level; tile-manager hints should come via `Plugin.Commands()` with a new `workspace-tile` context, not rendered in `View`.
- Keep command names short (1-2 words): `Split`, `VSplit`, `Close`, `Next`, `Focus`, etc.

### 1.7 Summary of the current sizing/resize model

1. A single "preview pane" at a time displays the captured output of the currently selected workspace's tmux session.
2. The plugin computes that pane's target dimensions from its own `p.width`/`p.height` minus sidebar, divider, panel borders, tab header, and hint line (`calculatePreviewDimensions`).
3. The plugin calls `tmux resize-window -x W -y H` (falling back to `resize-pane`) to make the tmux session's window match. This is the *only* place the inner application learns of a new size — there is no SIGWINCH involved at the sidecar/tmux boundary because sidecar is not a PTY; tmux is, and tmux delivers SIGWINCH internally to its attached processes when its window resizes.
4. `window-size manual` is set on session creation/enter-interactive so tmux doesn't auto-size to attached clients.
5. Resize is debounced (500ms) in interactive mode and verify-retried once (`resizeTmuxTargetCmd`). Post-resize, a `paneResizedMsg` triggers a fresh capture so the display reflects the new wrap width.
6. The rendered output is clamped to the tmux pane dimensions in `renderOutputContent` — if sidecar thinks it has more rows to show than tmux thinks the pane has, it pads; if fewer, it truncates. Cursor position is translated accordingly.

Tiling preserves all of this per tile; the new machinery is layout math and routing.

## 2. Design

### 2.1 Tile tree model — binary tree of splits (recommended)

Use a **binary tree** of splits, not a flat list. Rationale:
- Users create splits by repeatedly halving *a specific tile* (tmux's exact model). Linear lists force the implementer to either reject "split the right half horizontally again" or re-derive a tree anyway.
- Each internal node owns a split direction (horizontal/vertical) and a ratio in `[0.05, 0.95]`. Each leaf node owns a reference to what it displays: `{kind: workspace|shell|empty, key: string}` where `key` is the workspace name or shell `TmuxName`.
- A tile ID (monotonic counter, or UUIDv4 string) identifies leaves for focus, close, and persistence.
- Resize a split = change the ratio of the corresponding internal node. Close a tile = remove the leaf; its sibling subtree replaces the parent. This is the textbook tmux algorithm.

Suggested types (to live in `layout.go` in the workspace package):

```go
type TileKind int
const (
    TileWorkspace TileKind = iota
    TileShell
    TileEmpty
)
type SplitDir int
const (
    SplitH SplitDir = iota // side-by-side
    SplitV                 // stacked
)
type Node struct {
    // Exactly one of Leaf or Split is non-nil
    Leaf  *Leaf
    Split *Split
}
type Leaf struct {
    ID   int       // stable across layout mutations
    Kind TileKind
    Key  string    // workspace name or shell TmuxName; empty for TileEmpty
}
type Split struct {
    Dir   SplitDir
    Ratio float64   // fraction going to A
    A, B  *Node
}
```

### 2.2 Per-tile PTY sizing

Each leaf's dimensions come from tree traversal: root gets `(previewW, previewH)` (today's `calculatePreviewDimensions` result). An internal split node allocates `floor(ratio*parent)` to child A, remainder minus 1-cell divider to child B. Minimum leaf width/height (e.g., 20×5) clamp the ratio on both ends.

After each render cycle — and after any layout mutation (split/close/resize-via-key/drag end/window resize) — iterate all visible leaves and call `tty.ResizeTmuxPane(leaf.TmuxTarget(), w, h)` once per leaf. Use the existing debounce idea (per-leaf `LastResizeAt`) to avoid resize storms during drags.

Do NOT try to resize hidden/zoomed-out tmux panes — there are none. Every leaf is a pointer to an existing tmux session (the workspace's `wt.Agent.TmuxSession` or the shell's `TmuxName`); tiling only adds *views* of sessions, it does not create new tmux sessions. Creating new tiles that show existing workspaces is cheap.

However, note a real issue: a single tmux session has a single window with a single effective size. If tile A and tile B both point at workspace W, they cannot independently size W's tmux window. Two policies are possible:

- **Policy 1 (simple, v1):** each workspace/shell may appear in at most one tile. Splitting creates a new empty tile that the user then fills by selecting a workspace from the sidebar. Attempting to select a workspace that's already in another tile focuses that tile instead.
- **Policy 2 (advanced):** allow duplicate views. The shared tmux session is sized to the *smallest* containing tile's dimensions; both tiles render from the same `capture-pane` output (smaller tile clips, larger tile pads). This is reasonable but adds complexity.

**Recommend Policy 1 for v1.** It matches the existing 1:1 workspace↔tmux-session invariant in the plugin.

### 2.3 Prefix-key vs. direct-key shortcut scheme

Tmux uses `Ctrl-b` prefix; that conflicts with nothing in sidecar today. However, sidecar already has:
- `ctrl+\` = exit interactive mode (configurable)
- `ctrl+]` = full attach
- `ctrl+t` = toggle terminal panel

A prefix-based scheme (`Ctrl-w` like vim windows) is the cleanest: it reserves exactly one chord and the second key can mimic vim-window / tmux exactly without collision. vim-style `Ctrl-w <letter>` is also the muscle memory most TUI users already have.

Two modes of operation:
- **Non-interactive (list view):** bindings are direct — tile navigation keys (`ctrl+w h/j/k/l`) are processed by `handleListKeys` before falling through to sidebar/preview nav.
- **Interactive (passthrough) mode:** bindings are prefix-gated. Pressing `Ctrl-w` enters a short-lived "tile command pending" state (150ms window like double-Escape); the next keypress is interpreted as a tile command and consumed; anything else or timeout forwards the `Ctrl-w` normally to tmux.

### 2.4 Proposed default bindings

| Binding | Action | Notes |
|---|---|---|
| `ctrl+w s` | Horizontal split (stack) | vim: `:split`; creates a new empty tile below the focused tile |
| `ctrl+w v` | Vertical split (side-by-side) | vim: `:vsplit`; creates a new empty tile to the right |
| `ctrl+w c` | Close focused tile | If last tile, fall back to single-tile mode |
| `ctrl+w h` / `ctrl+w left` | Focus tile to the left | |
| `ctrl+w l` / `ctrl+w right` | Focus tile to the right | |
| `ctrl+w j` / `ctrl+w down` | Focus tile below | |
| `ctrl+w k` / `ctrl+w up` | Focus tile above | |
| `ctrl+w w` | Focus next tile (cycle) | Tab-equivalent within tiles |
| `ctrl+w =` | Equalize ratios | All sibling splits to 0.5 |
| `ctrl+w H/J/K/L` (shift) | Resize: grow focused tile 3 cells in direction | Mirrors vim |
| `ctrl+w <` / `ctrl+w >` | Shrink/grow horizontally | Tmux-style alternate |
| `ctrl+w o` | "Only" — close all other tiles | vim `:only` |
| `ctrl+w n` | New empty tile by splitting (Ask: `h` or `v`) | Optional |

Keep all these registered under a new keymap context `workspace-tile` registered from `Plugin.Init`. Mirror the registrations in `commands.go` under the same context so the footer shows live hints.

### 2.5 Focus model

Add `p.focusedTileID int` and `p.rootTile *Node` to the plugin struct. `p.focusedTileID` is the ID of the leaf that receives key input (both in non-interactive list mode and interactive mode). Focus changes:
- Sidebar nav (`j`/`k`) moves the sidebar selection and, if a tile is currently filled by the "follow sidebar" policy, updates that tile; otherwise the sidebar is independent and `Enter` assigns the current selection to the focused tile.
- Direct tile focus via mouse click on a tile region.
- `ctrl+w h/j/k/l` uses geometric neighbor lookup: compute each leaf's rect during layout, then pick the nearest neighbor whose edge abuts the focused tile's edge in the requested direction. Break ties by y-then-x coordinate.

For the simpler v1, allow *at most one* interactive tile at a time (the focused one). Non-focused tiles continue their background polling (like the current "visible but unfocused" state), showing live output but not receiving keystrokes. This preserves the existing three-state polling model (`shell-integration/SKILL.md:149-156`) per leaf.

### 2.6 Mouse drag-to-resize hooks

During render of each `Split` node:
- After laying out children, register a `regionTileDivider` hit region 1-cell thick along the split line, extending the full length of the split. The region `Data` should be a `*Split` pointer (or the node's ID) so `handleMouseDrag` knows which split's ratio to adjust.
- Reuse `mouseHandler.StartDrag(x, y, regionTileDivider, initialRatioAsInt)` — multiply the ratio by 10000 to store as int since `DragStartValue` is `int`. On drag, compute new ratio from `DragDX`/`DragDY` relative to the split's length.
- On `handleMouseDragEnd`, invoke a function `resizeAllVisibleTilePanes()` that iterates the tree, recomputes rects, and issues resize commands for any leaf whose size changed.

Divider hit regions must be registered *after* tile body regions so they take priority (per `drag-pane/SKILL.md:243-263`).

## 3. Implementation Phases

Each phase should be a separately-shippable PR.

### Refactoring philosophy

**Do not pre-refactor the workspace plugin before starting tiling.** The plugin is large (monolithic `Plugin` struct, 1000+ line files), but a "clean it up first" pass without a forcing function will refactor toward the wrong abstractions. Tiling itself is the forcing function — "per-leaf state" is the abstraction the feature demands, and you cannot know what belongs in that struct without the feature pulling on it.

Instead, bake three targeted cleanups into Phase 0 below. Leave everything else (modal handlers, diff-tab, sidebar rendering, keymap scaffolding) alone — it works, tiling does not need it to change, and changing it widens the blast radius of this already-large feature.

**Stop-and-reassess rule:** if during Phase 1 you find yourself reaching into 4+ unrelated subsystems to wire up a single tile, that is a signal the `PaneSession` abstraction from Phase 0 is wrong. Fix the abstraction rather than pushing through; the rest of the phases compound on it.

### Phase 0 — Feature flag + tree scaffolding + targeted cleanups (no visible change)

Files: `internal/features/flags.go` (register `workspace_tiling`), `internal/plugins/workspace/layout.go` (new), `internal/plugins/workspace/pane_session.go` (new), `internal/plugins/workspace/interactive.go` (delete duplicate).

**Tree scaffolding:**
- Add feature flag `workspace_tiling`.
- Create the tree types (`Node`, `Leaf`, `Split`) and pure functions: `NewSingleTile(kind, key)`, `(*Node).Split(leafID, dir, newLeaf)`, `(*Node).Close(leafID)`, `(*Node).Find(leafID)`, `(*Node).Neighbor(fromID, dir)`, `(*Node).Layout(rect) map[int]Rect`.
- Unit tests for tree math (splitting, closing, neighbor lookup, ratio clamping).

**Targeted cleanups (do these now, not separately):**
1. **Delete the duplicate `resizeTmuxPane`/`queryPaneSize` in `interactive.go:687-736`.** Route all resize calls through `internal/tty/session.go` (`tty.ResizeTmuxPane`, `tty.QueryPaneSize`). This predates the `internal/tty` package and was never migrated. Low risk, ~30 min, and tiling needs one canonical resize path.
2. **Extract a `PaneSession` struct** for the per-tmux-session state that is currently scattered across `Agent`, `InteractiveState`, `pollGeneration` / `shellPollGeneration` maps, and the terminal-panel fields. Fields: `{TmuxTarget, LastResizeAt, PollGeneration, OutputBuf, Cursor}`. Initial consumers: the existing single preview pane and the terminal panel. Phase 1's `Leaf` will own one of these directly. **This is the highest-leverage prep work** — skipping it means the tree in Phase 1 has nothing clean to point at and Phase 5 (per-tile input routing) becomes painful.
3. **Do not touch anything else.** Modal handlers, diff-tab split, sidebar rendering, existing keymap contexts — all out of scope for Phase 0.

### Phase 1 — Refactor renderer to go through the tree with a single tile

Files: `view_list.go`, `view_preview.go`, new `view_tiles.go`.

- Extract the "render one preview pane for a selected workspace/shell" code from `renderPreviewContent` into a function `renderTile(leaf *Leaf, rect Rect) string`. Do not change its behavior — it still renders tabs, output, diff, task for the given workspace, or primer/output for the shell.
- In `renderListView`, replace the direct `renderPreviewContent(previewContentW, innerHeight)` call with `renderTileTree(p.rootTile, Rect{0,0,previewContentW,innerHeight})`. When `p.rootTile == nil` (flag off, or startup before layout init), fall back to the old path.
- Initialize `p.rootTile = NewSingleTile(...)` in `Plugin.Init` whenever the feature flag is on.
- Guarantee no visible change with a single tile (byte-for-byte identical output).

### Phase 2 — Horizontal split + focus navigation

Files: `layout.go`, `keys.go`, `update.go`, `view_tiles.go`, `plugin.go`.

- Add prefix-key state: `tilePrefixActive bool`, `tilePrefixAt time.Time`. In `handleListKeys`, intercept `ctrl+w`: set flag, start 1s timer, consume. Next key with flag set: dispatch to a new `handleTileCommand(key)`. Same handling path from `handleInteractiveKeys` (so tile commands work mid-session).
- Implement `ctrl+w v` (vertical split = side-by-side) — creates an empty tile adjacent to the focused tile.
- Implement `ctrl+w h/j/k/l` navigation using `Node.Neighbor` lookup.
- Empty-tile renderer: a placeholder that shows "Empty tile — press Enter to attach the current sidebar selection" with a subtle border.
- `Enter` in an empty focused tile: bind that tile's leaf to the currently-selected sidebar workspace/shell. Uniqueness check (Policy 1): if that workspace/shell is already in another tile, focus that tile instead of binding.

### Phase 3 — Horizontal split (stack) + tile close

Files: `layout.go`, `keys.go`, `view_tiles.go`, `mouse.go`.

- Implement `ctrl+w s` (stack) as the horizontal companion.
- Implement `ctrl+w c`: collapse the focused leaf up to its sibling; update focus to the nearest surviving leaf.
- Implement `ctrl+w o` (only): trim all non-focused leaves from the tree.
- Mouse click on a tile body (not divider) moves focus to that tile.

### Phase 4 — Resize (keyboard + drag) and resize propagation to tmux

Files: `layout.go`, `keys.go`, `mouse.go`, `interactive.go`, new `tile_resize.go`.

- Keyboard resize: `ctrl+w <`, `ctrl+w >`, `ctrl+w +`, `ctrl+w -`, `ctrl+w =`. Manipulate the closest-ancestor `Split` node's `Ratio` with the right orientation. Clamp.
- Mouse drag: register `regionTileDivider` hit regions in `renderTileTree`; `handleMouseDrag` for that region updates the ratio; `handleMouseDragEnd` calls `resizeAllVisibleTilePanes`.
- Replace the single-target `resizeSelectedPaneCmd` with `resizeAllVisibleTilePanes` (keep the old function for non-tiling mode or implement it as "one-leaf tree"). Must iterate all visible leaves and emit one `tty.ResizeTmuxPane` each, with per-leaf debounce stored on the `Leaf` struct.
- Window resize (`tea.WindowSizeMsg`) handler in `update.go:23-44`: call `resizeAllVisibleTilePanes` instead of `resizeSelectedPaneCmd`.

### Phase 5 — Per-tile input routing in interactive mode

Files: `interactive.go`, `update.go`, `keys.go`.

- Change `InteractiveState` to carry the focused leaf ID (or make interactive state a field on `Leaf` itself): `Leaf.Interactive bool` plus the existing `TargetSession`/`TargetPane`.
- `handleInteractiveKeys` forwards to `p.focusedLeaf().TmuxTarget()`. The prefix chord check (`ctrl+w` + timer) happens *before* tmux forwarding so tile commands win.
- `pollInteractivePane` loop is rekeyed to the focused leaf. Non-focused leaves continue their background `scheduleAgentPoll`/`pollShellSessionByName` pipelines unchanged.

### Phase 6 — Persistence

Files: `state.go`, `plugin.go` (`Init`, `saveSelectionState`), new serialization helpers in `layout.go`.

- Extend `WorkspaceState` with a `TileLayout *TileLayoutJSON` field. Serialize the tree compactly: `{"split":"v","ratio":0.6,"a":{...},"b":{...}}` or `{"leaf":{"kind":"workspace","key":"feature-foo"}}`.
- Save on every mutation that currently calls `saveSelectionState` (split/close/resize-drag-end/focus change). Restore in `restoreSelectionState`; validate each leaf's key still exists (workspace or shell manifest), drop stale leaves, collapse dangling splits.

### Phase 7 — Polish

- Visible focused-tile indicator: stronger border (reuse `styles.RenderPanel(..., focused=true)` or `styles.GetInteractiveGradient()` when interactive).
- Status pill in each tile's top-right showing workspace name + agent status icon (so users can tell tiles apart at a glance).
- Command footer hints via `Commands()` under `workspace-tile` context.
- Tile placeholder art when empty.
- Toast on tile-manager state transitions ("Tile closed", "Last tile — press Ctrl-W c again to close plugin? Nah, just ignore").
- Smooth draw: throttle re-layout during drag to 16ms via `tea.Tick` to avoid flicker; only call `ResizeTmuxPane` at drag-end, not during drag.

## 4. Risks / Unknowns

- **Resize latency & flicker during drag.** `tmux resize-window` is synchronous in the plugin today (direct `exec.Command(...).Run()` in `resizeTmuxPane`). Calling it 30+ times a second during a divider drag would stutter badly. Mitigation: during-drag render uses a locally-computed rect; `ResizeTmuxPane` fires only on drag end (as the terminal-panel divider already does at `mouse.go:1365-1368`). Expect a one-frame "snap" as the tmux content re-wraps.
- **`window-size manual` must be set on every tile's underlying session before resize will stick.** Today this is only set inside `enterInteractiveMode`. Tiling must set it on session attach *and* when first placing a workspace/shell into a tile, even outside interactive mode. Risk: forgetting this path causes resizes to "bounce back" if an external tmux client is attached.
- **Key conflicts with existing workspace bindings.** `ctrl+w` is currently unbound in the plugin. Worth checking the global keymap (`internal/keymap/bindings.go`) and the app-level `commands.go` to confirm nothing else steals it. `h/j/k/l` collide with existing non-interactive vim nav inside the preview pane — but that code already checks `p.activePane == PanePreview` before consuming, so a tile prefix gate avoids the collision.
- **`Ctrl-w` during interactive mode.** Some TUIs (emacs, less) use `C-w`; forwarding it to tmux is expected. The prefix timer means `C-w` followed by a non-tile key within 150ms will *not* reach tmux. That is user-surprising. Consider making the prefix configurable and default to `alt+w` or a two-chord sequence (`ctrl+b w`) in interactive mode to reduce collision.
- **SIGWINCH behavior under rapid resizes.** Each `tmux resize-window` makes tmux deliver SIGWINCH to its attached process; shells and agents that do readline re-rendering on SIGWINCH (claude-code, fish with reflow) can flicker. Tmux itself debounces nothing. The 500ms debounce in `maybeResizeInteractivePane` (`interactive.go:680`) addresses this today — must be applied per-leaf in tiling.
- **Interaction with inline-editor.** The filebrowser plugin's inline editor uses `tty.Model` against an ad-hoc `sidecar-edit-<ts>` tmux session (`shell-integration/SKILL.md:251-257`). It is a *separate plugin*, so tiling changes in the workspaces plugin don't directly affect it. But conceptually, the inline editor could later be a "tile kind" — keep `TileKind` open to that extension.
- **Interaction with drag-pane (sidebar divider).** Tiling's tile dividers are inside the preview region; the sidebar/preview divider is outside. No overlap. But the `mouse.Handler.HitMap` is a single map cleared per render, and order matters: register sidebar/preview/terminal-panel dividers first, then tile dividers (they are deeper in the view and should win clicks within their rects). Verify in `view_list.go` after the refactor.
- **Workspace destroyed while tiled.** If a workspace is deleted (D key) while bound to a tile, the tile's `Leaf.Key` becomes dangling. Handler: in `RefreshDoneMsg` processing (`update.go:69-...`), after worktrees list is updated, sweep the tree and convert dangling leaves to `TileEmpty`. Show a toast.
- **Terminal-panel + tiling overlap.** Today's terminal panel is a second tmux session attached to the selected preview's tile. With tiling, the terminal panel is redundant — tiling is strictly more general. For v1, disable the terminal-panel toggle (`ctrl+t`) when the layout has more than one tile, or keep it but scope it to the focused tile. Document the intent to deprecate `termPanel*` once tiling ships.
- **Async capture races.** `AsyncCaptureResultMsg` carries a `WorkspaceName` and is routed to the matching workspace's output buffer. That still works unchanged; tile rendering just reads the buffer. The risk is cursor-query races: `getCursorPosition` must target the *focused* leaf's tmux pane, not the "currently selected" workspace. Audit `pollInteractivePane` callers.
- **Stale-poll invalidation.** `pollGeneration` is keyed by workspace name today. With Policy 1 (one workspace per tile) the existing key still works. Under a future Policy 2, generation must key off `(workspaceName, tileID)`.
- **Mouse reporting in multiple tiles simultaneously.** If two tiles run agents that have enabled mouse reporting, a click must be SGR-translated to the correct tile's pane coordinates. The existing `handleInteractiveMouseForward` at `mouse.go:520-545` only targets the focused tile's session, which is correct. Non-focused tiles ignore mouse events other than focus-click.

## 5. Out of Scope for v1

- **Zoomed/floating tiles.** Tmux's `C-b z` pane-zoom toggle. Could be added later as a "hide all but focused leaf" overlay on the layout renderer.
- **Saved named layouts.** `ctrl+w ~` / preset layouts ("main+v-stack"). Just serialize whatever the user last had.
- **Cross-project layout sync.** Layouts are per-project (`WorkspaceState` is already keyed by `projectRoot`).
- **Multiple workspaces-plugin views on a single layout.** No "include this file-browser tab inside a workspace tile" — tiles only hold workspaces/shells/empty.
- **Duplicate views of the same workspace.** Policy 1 above; Policy 2 is a v2 candidate.
- **Rearranging tiles via drag.** v1 supports only resize-drag. Swap/move requires hit-testing drop targets and a different UX.
- **Per-tile scroll lock.** All tiles follow their own `autoScrollOutput`; OK to leave as a follow-up.
- **Tabs of tiled layouts.** Tmux has "windows" (groups of tiles); sidecar has tabs for different plugins. Doing "named layouts per project" is future work.
- **Animating split transitions.** No.

---

## Critical Files for Implementation

- `internal/plugins/workspace/view_list.go` — top-level layout renderer; source of the split-pane math that tiling must generalize.
- `internal/plugins/workspace/view_preview.go` — per-workspace rendering with cursor overlay; the body of each tile.
- `internal/plugins/workspace/interactive.go` — resize/target/debounce plumbing and `calculatePreviewDimensions`; all per-tile resize logic branches off here.
- `internal/plugins/workspace/keys.go` — key dispatch, including the new prefix gate and tile-command handler.
- `internal/plugins/workspace/terminal_panel.go` — closest existing precedent for "two live tmux sessions in one preview"; the generalization target.
- `internal/tty/session.go` — the `ResizeTmuxPane` / `SetWindowSizeManual` / `QueryPaneSize` primitives used by every tile.
- `internal/state/state.go` — where `WorkspaceState` must grow a `TileLayout` field for persistence.

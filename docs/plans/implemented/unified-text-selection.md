# Plan: Unified text selection across all surfaces

**Status:** Phases 1–4 implemented (Phase 1 2026-08-18; Phase 2, docview, 2026-08-19; Phases 3 and 4, workspacediff, issueview and resourceview, 2026-09-03 under td-2b8f79, which also made the plugin browser's detail box selectable — a pane that did not exist when this plan was written). **Phases 5 and 6 are outstanding**: the file browser and notes still run their own hand-wired glue, and gitstatus' own diff renderers are untouched. **Created:** 2026-08-18 **Research snapshot:** 2026-08-18, at 86dc806d — all `path:line` claims verified at that commit **Scope:** Mouse text selection (char/word/line), highlight rendering, and copy-to-clipboard for: filebrowser preview (rendered + raw), workspace/overview file panes (docview), td issue panes (issueview), diffs (git plugin + workspacediff), Jira panes (resourceview), and notes. One selection engine, one highlight renderer, one copy pipeline. **Related:** [terminal-resource-providers.md](terminal-resource-providers.md), [notes-plugin-overhaul.md](notes-plugin-overhaul.md) **Tracking:** td issues to be filed per phase (see §8)

---

## 0. The one-paragraph answer

Sidecar already contains every hard primitive this feature needs, built and tested: a shared selection model (`ui.SelectionState`, `internal/ui/selection.go`), an ANSI-safe post-render highlight injector (`ui.InjectCharacterRangeBackground`, `internal/ui/selection_render.go`), a headless gesture engine with word/line snapping and drag auto-scroll (`tty.Pointer` + `tty.Geometry`, `internal/tty/{gesture,pointer}.go`), and a shared copy pipeline with uniform chords and notices (`internal/tty/clipboard.go`, `surface_chords.go`). Four surfaces use them today (embedded terminals in workspace and overview, filebrowser preview, notes); the rest re-implement nothing — they simply have no selection at all. The plan is therefore **not** to build a selection system but to **extract the existing one into a neutral package (`internal/textselect`) with a small per-surface adapter interface, then bind each remaining surface with one adapter each**. This is the same architecture Textual ships (per-widget opt-in over one screen-level selection model) and the same rendering technique crush ships on our exact Charm v2 stack, so it is the proven state of the art, not an invention.

---

## 1. Current state (verified)

### What already exists and must be reused, not rebuilt

| Capability | Location | Notes |
| --- | --- | --- |
| Selection model (linear + rectangular, anchor/extend, per-line span query, text extraction) | `internal/ui/selection.go` | `SelectionState`, `GetLineSelectionCols`, `SelectedText`; 780 lines of tests |
| ANSI-safe highlight overlay | `internal/ui/selection_render.go:121` | `InjectCharacterRangeBackground` walks graphemes, re-asserts selection bg after any SGR the line sets, restores the line's own bg after |
| Visual-column math | `internal/ui/selection_render.go` | `ExpandTabs`, `VisualSubstring`, `VisualColAtRelativeX` — grapheme/wide-char aware |
| Gesture engine | `internal/tty/gesture.go` | `Pointer`: press/drag/extend/release/abandon, char/word/line units, drag auto-scroll past edges (generation-gated) |
| Coordinate mapping | `internal/tty/pointer.go` | `Geometry`, `CellAt`, `ClampedCellAt`, `WordSpanAt` (xterm word chars), `UnitSpanAt`, `SelectAllSpan` |
| Click-vs-drag disambiguation | `internal/tty/click.go` | `PointerIntentFor`, `ResolveClick` — a click activates, a drag selects |
| Copy pipeline + uniform notices | `internal/tty/clipboard.go:107` | `CopySelection` → `ansi.Strip` per line → `clipboard.WriteAll`; `CopyNotice` phrasing shared by all hosts |
| Uniform chords | `internal/tty/surface_chords.go:39` | `SurfaceChords{Copy, SelectAll, Scrollback}`, `ResolveSurfaceChord`; copy `alt+c` (config: `interactiveCopyKey`), select-all `ctrl+a` |
| Multi-click detection | `internal/mouse/mouse.go:115` | double/triple click, 400 ms window, same cell + region |
| Drag plumbing | `internal/mouse/mouse.go` | `MouseMotionMsg` → `ActionDrag`; app runs `MouseModeAllMotion` by default (`internal/app/view.go:79`) |

### Per-surface status

| Surface | Package | Renders via | Selection today |
| --- | --- | --- | --- |
| Embedded terminal (workspace + overview) | `internal/tty` + `internal/termpreview` | `DrawRows` (`rows.go:53`) | **Full** — the reference implementation |
| Filebrowser preview (raw, syntax, markdown) | `internal/plugins/filebrowser` | own `view.go` + chroma/glamour | **Full** (hand-wired: `mouse.go:379-904`, `view.go:970-1050`) |
| Notes | `internal/plugins/notes` | `markdown.RenderMapped` (source anchors) | **Full**, source-position based (`notes/selection.go`), copy-on-select |
| File pane in workspaces (global + project) | `internal/docview` | `layOutContent` + `fitLine` (`model.go:227,363`) | **None** |
| td issue panes | `internal/issueview` | `buildRows` + `paintRow` (`render.go:98,433`) | **None** (row-granular hits only) |
| Diff pane (workspace + overview) | `internal/workspacediff` | `renderRaw` (`render.go:692`) | **None** (`y` yanks target identity, not text) |
| Git plugin diffs | `internal/plugins/gitstatus` | `diff_renderer.go` (chroma-blended, side-by-side) | **None** |
| Jira / resource panes | `internal/resourceview` | `documentLines` (`render.go:172`) | **None** |

### The structural problem

Filebrowser and notes each hand-wired ~200 lines of gesture/render/copy glue against the shared primitives. Doing that six more times is how implementations drift. The missing piece is a **binding layer**: the equivalent of what `internal/livepanes` did for live refresh — one shared engine, one small `Binding` per surface.

### Two facts that make this cheap

1. **Every non-terminal surface's final output is already a `[]string` of ANSI-styled rows at known width, with an integer scroll offset.** docview `display().rows`, issueview `visibleRows()`, workspacediff's rendered diff body, resourceview `m.lines()`. A selection over *visual rows* with extraction via `ansi.Strip` — exactly what the terminal does — works on all of them with no source mapping.
2. **`paneframe.Render.Origin` (`paneframe.go:79`) already hands every content pane its own screen rectangle**, which is precisely the `Geometry.Content` rect the gesture engine needs. Workspace and overview inherit it identically, so pane-hosted surfaces get parity by construction.

---

## 2. What the research says (and what we take from it)

Full findings archived in this section so the plan is self-contained.

- **Textual** (the only mainstream TUI framework with shipped app-wide selection): screen-level selection keyed per widget, per-widget opt-in (`ALLOW_SELECT`), each widget answers `get_selection()` (owns its text extraction) and `get_span(y)` (per-line highlight span). We adopt this shape: central rules, per-surface opt-in adapter.
- **crush** (charmbracelet, on our exact bubbletea v2 / lipgloss v2 / ultraviolet stack): in-app selection with 400 ms click-count char/word/line units, highlight applied post-render, extraction from content cells not the styled screen, copy via `tea.SetClipboard` (OSC 52) **and** a native clipboard write, sequenced, with a toast. Its most-complained-about decision (issues #661/#695): **unconditional copy-on-select** — we make that a config option, not the default.
- **lazygit/k9s**: no text selection; they copy objects. k9s's atotto-only clipboard is its top clipboard complaint (no SSH, no headless) — the argument for adding OSC 52.
- **Terminal-native fallback**: every terminal lets shift (Linux/Windows) or option (iTerm2/macOS) bypass mouse tracking for native selection. Free; document it in help.
- **Rendering technique**: crush mutates an ultraviolet cell buffer; we do ANSI string surgery. Both are proven. Ours is already written, themed, and tested against arbitrary embedded SGR (`selection_render.go:137-209`), so we keep it — but it stays behind one function so a future swap to a cell-buffer implementation touches one file.
- **OSC 52 details that matter**: tmux needs `set-clipboard on` or passthrough; control mode (`tmux -CC`) drops it (tmux#4779); failure is silent, so always show the notice and always also write the native clipboard.

---

## 3. Design

### 3.1 One new package: `internal/textselect`

A neutral home for what `internal/tty` already proved out, so non-terminal surfaces don't import a terminal package to select text. Contents:

```go
// The adapter a surface implements. Everything else is shared.
type Source interface {
    // ContentRect is the screen-space rect of the selectable content
    // (inside chrome/gutter), in the same coordinate space as incoming
    // mouse events. For pane-hosted surfaces this is derived from
    // paneframe.Render.Origin; for plugins, from their own layout.
    ContentRect() mouse.Rect
    // Line returns the *plain* (ANSI-stripped is fine, but pre-wrap,
    // post-layout) text of visual row i, and LineCount the total rows.
    // Selection coordinates are visual rows — what the user sees.
    Line(i int) string
    LineCount() int
    // Scroll is the index of the first visible row.
    Scroll() int
    // TabWidth for visual-column expansion (0 = already expanded).
    TabWidth() int
}

type Surface struct { /* owns ui.SelectionState + gesture state */ }

func (s *Surface) HandleMouse(a mouse.MouseAction, src Source) Result
func (s *Surface) HandleKey(msg tea.KeyMsg, src Source) Result   // chords: copy, select-all
func (s *Surface) DecorateRow(row string, visualRow int) string  // highlight injection
func (s *Surface) SelectedText(src Source) []string
func (s *Surface) Clear()
```

Implementation is **moves, not writes**: the gesture state machine is `tty.Pointer` generalized (its `Geometry`/`CellAt`/`WordSpanAt` math is already buffer-agnostic — it takes lines and a rect, nothing terminal-specific); `DecorateRow` is `GetLineSelectionCols` + `InjectCharacterRangeBackground`; `SelectedText` is `SelectionState.SelectedText`. `internal/tty` then depends on `textselect` (or thin aliases keep its API stable) so the terminal remains the same engine, not a fork. `Result` carries what the host must act on: `Changed` (re-render), `Copy(text, notice)`, `ClickThrough` (gesture resolved to a click — host runs its normal click behavior), `AutoScroll(rows)`.

Shared behaviors, implemented once in `Surface`:

- Click activates, drag selects (`tty.PointerIntentFor` semantics) — surfaces with clickable rows (issueview, diff file lists) keep their click behavior untouched.
- Double-click = word (xterm word chars `_-./:` + alnum), triple-click = line, drag extends the unit; shift-click extends an existing selection.
- Drag past top/bottom edge auto-scrolls (the host applies `AutoScroll` to its offset).
- Any click without drag, scroll-away (where the surface can't keep absolute coordinates), Escape, or content refresh clears the selection.
- Copy chord (`alt+c` / configured key / `y` where the surface has no competing `y`), select-all (`ctrl+a`), identical `CopyNotice` phrasing everywhere.
- Selected text is what the user sees: ANSI-stripped visual rows, tabs expanded, joined with `\n`. Gutters/line numbers are outside `ContentRect` and never copied.

### 3.2 One clipboard upgrade, shared by everything

Today every copy path ends in `atotto/clipboard.WriteAll` (23 call sites). Add a single `internal/clip` (or extend `tty/clipboard.go` and move it) used by all of them:

1. `clipboard.WriteAll(text)` — native, keeps current behavior on local macOS.
2. `tea.SetClipboard(text)` — OSC 52, makes copy work over SSH and inside tmux (bubbletea v2 built-in; no new dependency).
3. Always return the existing `tty.CopyNotice`-style toast; OSC 52 failure is silent, so the notice reports what was attempted, and native failure falls back to OSC 52-only wording.

This is the crush belt-and-suspenders pattern and fixes a real gap (sidecar over SSH currently cannot copy at all). It ships in Phase 1 because every later phase's "did it work" test is "text landed on the clipboard."

**Landed in Phase 1:** every copy path in the tree writes both clipboards, through `clip.Copy` (text in hand) or `clip.CopyFrom` (text the command produces), and phrases its notice with `clip.Result.Message` so a native-only failure reads the same everywhere. The one exception is the embedded td monitor (`tdmonitor/plugin.go`): td's model takes a writer, not a command, so there is no way back into the program loop to emit OSC 52, and that yank stays native-only until td offers a command-shaped hook.

### 3.3 One selection at a time, app-wide

Like a native app: starting a selection anywhere clears the previous selection anywhere else. Implemented without a global registry: each `Surface` clear is cheap, and the owning plugin clears its surfaces when it loses focus or when one of its panes starts a new drag. (Textual's cross-widget multi-selection is deliberately **not** adopted — a drag stays within the pane it started in. Cross-pane drag is the one place we accept being less than Textual; the payoff-to-complexity ratio is poor in a pane-based UI, and native terminal selection via shift/option remains available for literal screen scraping.)

### 3.4 Copy-on-select is config, off by default

Notes currently copies on drag-end (`notes/mouse.go:411-420`). Standardize: new config key `selection.copyOnSelect` (default `false`); when true, drag-end copies with the usual notice. Notes migrates to the shared default — its current behavior remains available via the flag. (Research: unconditional copy-on-select is crush's most-complained-about behavior.)

**Landed in Phase 1:** one key and one control. Configuration → Terminal's existing "Copy on select" toggle now writes `selection.copyOnSelect`, the terminal's older `plugins.workspace.copyOnSelect` is folded into it at load and cleared on the next save, and `app.TerminalConfig` honours either. Surfaces read the setting from the config they were built with, so it applies from the next run — the same as the chords beside it.

### 3.5 Escape hatches

- Document shift-drag (Linux/Windows terminals) / option-drag (iTerm2, Terminal.app) for native terminal selection in help and the docs site — zero code.
- Optional (Phase 6, only if wanted): a toggle that runs `tea.DisableMouse` / re-enable, for terminals where the modifier convention is awkward.

### 3.6 What is explicitly out of scope

- Titles, tab strips, navbar, sidebars, footer, file lists, modal chrome — not selectable. The test is "would a native app select it": document bodies yes, chrome no.
- The embedded terminal's selection is already best-in-class here; it is refactored to sit on `textselect` (so improvements propagate) but its behavior does not change.
- Rectangular selection stays supported in the model (it already is) but no new UI for it outside the terminal.
- Rendered-markdown source mapping for docview/issueview/resourceview: selection is over the rendered rows (copy what you see). Only notes needs source positions (it edits), and it keeps its `selection.go` mapping layer *on top of* the shared gesture engine.

---

## 4. Per-surface bindings

Each phase = implement `Source`, route mouse/key events through `Surface`, call `DecorateRow` in the render path, wire the copy notice. Order chosen: shared-component surfaces first (each lands in two UIs at once), hardest renderer last.

### 4.1 docview — file panes in workspace and overview (Phase 2)

The highest-leverage single binding: it is the file pane in **both** the project workspace and the global Sessions browser.

- `Source.Line(i)` = `display().rows[i]` stripped; rect from the pane host's `Render.Origin`, minus the gutter width when line numbers are on (`docview/gutter.go`).
- `DecorateRow` goes into `View()` (`model.go:227`) between row slicing and `fitLine`.
- Raw mode: selection spans exclude the gutter; rendered (glamour) mode: plain visual rows, no gutter.
- Scroll during an active selection: docview rows are stable under scroll (absolute row indexing), so selection survives scrolling, like the terminal (`tty.ScrollKeepsSelection`, `pointer.go:117`).
- Filebrowser preview (§4.5) later migrates to the same binding shape; docview is the cleaner first target because it has no legacy selection code.

### 4.2 workspacediff — diff pane in workspace and overview (Phase 3)

- `Source.Line(i)` = the rendered diff body lines that `renderRaw` (`render.go:692`) produces (before `opts.Truncate`); rect = `ContentBox(leaf)` (`layout.go:28`) minus the file-list column — selection lives in the diff pane only, not the file list.
- Copied text keeps the `+`/`-`/`@@` prefixes — that is what the user sees and what they paste into review comments.
- Click-vs-drag matters here: clicks on `diffPaneHits` regions page/focus today; the `ClickThrough` result preserves that.
- The existing `y` (yank target identity) keeps its binding; selection copy uses the standard chord, and `y` copies the selection **when one is active**, identity otherwise — matching how filebrowser's `y` already behaves with a live selection.

### 4.3 issueview — td issue panes (Phase 4)

- Rows are heterogeneous (glamour description, lipgloss field rows), but `visibleRows()` is still `[]string`; `Source` wraps it. Hit regions stay row-granular for clicks (`model.go:330-345`); the gesture engine only needs the rect + rows.
- `paintRow`'s hover/selected row styling (`render.go:433`) composes with the highlight because `InjectCharacterRangeBackground` re-asserts over row backgrounds — the same situation the terminal's search-highlight already handles.
- Interaction rule: click still activates rows/links (`ClickThrough`); drag selects.
- The existing `y` yank-issue chords (`workspace/plugin.go:916`) keep priority when no selection is active, same rule as diffs.

### 4.4 resourceview — Jira and future providers (Phase 4, same shape as issueview)

- `Source` over `m.lines()` (`render.go:110`); rect from the pane's `Render.Origin`.
- Because every future terminal-resource provider renders through resourceview, **new providers are selectable with zero additional work** — the plan's "new features benefit seamlessly" requirement, satisfied structurally.

### 4.5 filebrowser + notes + terminal — converge on the kit (Phase 5)

Refactors, not features; behavior-preserving:

- Filebrowser: delete its hand-wired gesture glue (`mouse.go:379-904` selection paths, `view.go:970-1050` injection loop) in favor of `Surface` + `DecorateRow`. Its wrapped-segment overlap math moves into the binding's `Source` (it already knows the wrap layout).
- Notes: keep `notes/selection.go` (source-position model and `visualColsForSourceRange`) but drive it from the shared gesture engine so click-counting, auto-scroll, and chords stop being a parallel implementation; copy-on-select moves behind the config flag (§3.4).
- Terminal hosts: mechanical — `internal/tty` re-exports from `textselect`; workspace `mouse.go` / overview `preview.go` wiring shrinks but does not change behavior.

After this phase there is exactly **one** gesture engine, **one** highlight renderer, **one** copy pipeline, and two selection-coordinate models (visual rows everywhere; source positions only inside notes, layered on top).

### 4.6 gitstatus diffs (Phase 6)

Last because it is the hardest renderer (side-by-side, word-diff, blended syntax backgrounds, horizontal offset) and the least pane-shaped:

- Unified/inline views: same binding as workspacediff over the rendered lines, accounting for `horizontalOffset` (the engine's `ColOffset` in `tty.Geometry` already models exactly this).
- Side-by-side: two selection sub-rects (left/right columns); a drag stays within the column it started in and copies that column's text. This is what native side-by-side diff viewers do.
- The blended add/remove backgrounds (`diff_renderer.go:845`) are the stress test for `InjectCharacterRangeBackground`'s bg re-assertion; a dedicated render test goes in with this phase.

---

## 5. How new surfaces stay seamless

Mirror the `livepanes.Binding` precedent (AGENTS.md already mandates that pattern for live refresh): a new content-pane kind that renders text implements `textselect.Source` (typically ~20 lines: rect, rows, scroll) and forwards mouse/keys to a `Surface` in its pane host file. Add the requirement to the workspace-parity section of AGENTS.md so a pane kind without a selection binding is flagged in review the same way a pane kind without a live-refresh binding is today. Future improvements (e.g. swapping string surgery for an ultraviolet cell buffer, adding a selection-history, changing highlight styling) land in `internal/textselect`/`internal/ui` and propagate to every surface without per-surface edits.

---

## 6. Styling

Highlight color stays `GetSelectionBgANSI()` → theme `BgTertiary` (`selection_render.go:18`) so all surfaces already match and themes already control it. Add a dedicated optional theme key (`selectionBg`) defaulting to `BgTertiary` so theme authors can tune it without repurposing a semantic color; wire through `internal/styles`. No per-surface styling.

---

## 7. Risks and open questions

- **Wrapped-line column math** is the recurring source of off-by-ones (filebrowser's overlap code at `view.go:985-1012` exists for a reason). Mitigation: `Source` reports post-wrap visual rows, so the engine never sees wrapping; each binding is responsible for its own wrap layout, which it already owns for rendering.
- **Render caches**: docview caches layout by `layoutKey` (`model.go:327`) and issueview rebuilds rows — highlight must be injected **after** cache retrieval, never into cached strings. `DecorateRow` at slice-time guarantees this; the plan's review checklist item is "no `Inject*` call writes into a cached structure." (crush hit exactly this and needed freeze/thaw hooks.)
- **Click-vs-drag on interactive rows** (issueview, diff file hits): the terminal solved this (`ResolveClick`); regression risk is a click that no longer activates. Each binding phase includes a tmux-drive proof of click-through.
- **`ctrl+a` select-all** collides with readline-style "start of line" only in surfaces with text input; none of the target surfaces have input, and the terminal already claims `ctrl+a` for select-all, so this stays consistent.
- **OSC 52 under `tmux -CC`** (iTerm2 control mode) is dropped upstream (tmux#4779); native write covers local use, and the notice never claims more than it did.
- Open: should selection persist when a pane loses focus (dimmed highlight, native-app style) or clear? Proposal: persist until another selection starts, Escape, or content refresh; no dimmed variant in v1.
- Open: `y` copies selection-when-active on surfaces where `y` already yanks an object (§4.2/§4.3) — confirm this precedence feels right in use; it matches filebrowser today.

---

## 8. Phases

Each phase: implementation + unit tests + a `scripts/tmux-drive.sh` proof (drag across styled content, snapshot showing highlight, assert clipboard content in the isolated run), reviewed per the usual td flow, one td issue per phase.

| Phase | Deliverable | Size | Status |
| --- | --- | --- | --- |
| 1 | `internal/textselect` extracted from `internal/tty` (moves + adapter interface); dual-write clipboard (`tea.SetClipboard` + native) behind the shared notice; `selection.copyOnSelect` config key | M | done |
| 2 | docview binding — file panes selectable in workspace **and** overview | M | done |
| 3 | workspacediff binding — diff panes selectable in both surfaces | M | done, td-2b8f79 |
| 4 | issueview + resourceview bindings (same shape) — td and Jira panes | M | done, td-2b8f79 |
| 5 | Converge filebrowser, notes, and terminal hosts onto the kit; delete hand-wired glue; AGENTS.md parity rule + docs-site help (incl. shift/option native fallback) | M | outstanding |
| 6 | gitstatus diffs (incl. side-by-side two-rect model); optional mouse-tracking toggle if still wanted | L | outstanding |

**What phases 3 and 4 did differently from what is written above** (2026-09-03, td-2b8f79):

1. **The diff pane's coordinate space is the frame, not the diff body.** §4.2 assumed `renderRaw`'s lines and a rect that excluded the file list. Neither survived contact: the patch body is normally painted by the *host* through `RenderOpts.PaintFile` — gitstatus' line-diff and side-by-side renderers, with line-number gutters the raw patch does not carry — one window at a time, so the view cannot answer for a row that is not on screen. It therefore records the frame it just drew, numbers rows from the top of it, and drops the selection when the frame changes. Two consequences follow: a drag past an edge does not scroll (the rows it would reveal cannot be selected without dropping the selection that asked for them), and a selection covers whatever was drawn at those cells, the file list included — which is what a terminal's own selection does, and what makes a file's name as copyable as its hunks.
2. **`y` was left alone.** §4.2 and §4.3 said `y` should copy the selection when one is active and the identity otherwise. The selection chords are `alt+c`, the platform copy chord and `ctrl+a`, and they are answered before every pane's own key switch; giving `y` a second meaning would make one key mean two things depending on invisible state, on three surfaces, for no capability that is not already reachable. This is a deviation, not an omission.
3. **The issue card protects its own rows rather than relying on `ClickThrough` alone.** §4.3's rule holds for the body, but a parent row, a subtask row and the scrollbar each answer a press *on the press* in at least one host, so `issueview.SelectableAt` refuses those cells and everything else in the body is text. The focus click is unaffected on every surface, because focus follows the press before any gesture is armed.
4. **A fourth pane joined: the plugin browser's detail box.** It did not exist when this plan was written. It is the same adapter shape over `detailBlock`'s rows.
5. **`textselect.Pane` is new.** The plan described per-surface bindings; three hosts route four viewers, so the host half is written once against an interface rather than four times against concrete types.

Phases 2–4 are independent after Phase 1 and can interleave with other work; Phase 5 is the debt-payoff gate — the plan is not "done" until the old glue is deleted, because two live implementations is the failure mode this plan exists to prevent.

---

## 9. Acceptance (the north star, testable)

1. In every surface listed in Scope, press-drag highlights text, double-click selects a word, triple-click a line, `alt+c` (or configured key) copies, and pasting yields exactly the visible text without gutters, chrome, or ANSI garbage.
2. Copy works locally, over SSH, and inside tmux (native + OSC 52 dual-write).
3. A click that previously activated something still does; drag never activates.
4. One gesture engine, one highlight injector, one copy pipeline in the tree (`grep` for `InjectCharacterRangeBackground` call sites outside `textselect`/bindings returns only render-path `DecorateRow` implementations).
5. A new pane kind gains selection by implementing `Source` — demonstrated by resourceview covering any future provider with no additional code.

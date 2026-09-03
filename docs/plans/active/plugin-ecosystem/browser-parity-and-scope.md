# Browser parity, scope, and plugin authoring (M4a–M4c)

**Status:** proposed 2026-09-03; M4a is implemented (see its section), M4c is ready to start, M4b is gated on the four decisions under [Decisions to confirm](#decisions-to-confirm). Controlling document for milestones M4a, M4b and M4c of [README.md](README.md); M4d (freeze, migrate, flag flip, site docs) stays in the README. **Tracking:** td-f9f007 (epic); per-milestone issues are listed with each milestone.

**Reading order:** [README.md](README.md) for the settled decisions and the milestone map, this file for the work, [host.md](host.md#the-shared-browser) for how the browser is built today, and [mockups/README.md](mockups/README.md) for the screens M4b implements. The design rules this work adds to Sidecar as a whole live in [docs/reference/design-language.md](../../../reference/design-language.md) under *Pointer parity* and *The footer*, not here.

## Why

The shared browser (`internal/pluginbrowser`) shipped in M2b as a keyboard-only surface: a fixed 61% split, no hit regions, no scrollbar, a query line whose only focus signal is a caret, and a Tab key that never reaches it because the app's focus ring engages only when a secondary leaf is open. Every protocol plugin inherits those gaps, and every pointer improvement made elsewhere in Sidecar passes the browser by. Recall, the reference plugin, then made the second problem visible: its project scope is applied invisibly and is wrong for every documents source, so a corpus that answers eight results in the CLI answers none in the app and the empty page claims to be honest.

Both are fixed at the seams that already exist. The pointer work reuses `internal/mouse`, `internal/paneframe`, `ui.RenderScrollbarWithState`, `workspacelist.Filter` and `ui.ReserveHeaderControls`, so the browser tracks the rest of the app from then on. The scope work applies protocol revisions the M0 mockups already drew (`filters[]`, `omitted`, `failed`) and adds one (`coverage[]`), so what is reviewed in `mockups/recall-studio.scope-picker.txt` is what ships.

## What the audit found

Recorded here because the fixes depend on it; file references are to `main` at 1c9675ed and recall at dc7ba91.

- **Pointer.** `pluginbrowser.Model.HandleMouse` (`overlays.go:106`) returns nil unless an overlay is open. The list/detail gap is `strings.Repeat(" ", paneGap)` (`render.go:68`), not `ui.RenderHandle`. No `ui.Scrollbar` anywhere in the package. The View pill is drawn by hand (`render.go:184-190`) with a two-rung degradation ladder and no hit rect, while `workspacelist.sidebarHeader` returns placed controls precisely so its pill gets `RegionSort`.
- **Focus.** `app.handleAppContentKey` (`content_deck.go:1291`) returns before the Tab branch unless the deck holds a Document, Issue, Note, Diff or Resource leaf, so a global tab showing only its Primary leaf never cycles the browser's `PaneFocusStops`. The browser's `PaneFocusProvider` wiring is correct and unreachable.
- **Query line.** `render.go:236-254` draws `/`, a placeholder, and a `▏` caret; there is no style swap on focus. `workspacelist.Filter.RenderRow` (`filter.go:118-150`) is the app's existing focused-filter rendering: `Muted` to `Title` on focus, a `▌` caret, a right-aligned count, and a `RegionFilter` hit target.
- **Notices and outcome.** `"scoped to project …"` and `"1 of 13 sources did not answer …"` are recall-authored notice text; the host has no data behind them and no click target. `"This plugin offers no action here."` is host text (`overlays.go:317`), set as a flash when `a` finds no applicable action, and echoed into the app footer by `FooterStatus`.
- **Detail styling.** `detail.go` uses `styles.*` throughout (labels `Muted`, values `Body`, section rules `Muted`, tones through `toneStyle`); no hardcoded colours. The body cache is keyed on width and generation but not the renderer's style key (td-83a3fa).
- **Recall scope.** `sidecarProjectScope` (`recall/internal/cli/sidecarplugin.go:566`) maps `context.project.name` to `Scope{Project: name}` on every `list` and `get`. For a documents source, `Project` is the first path segment under the source root (`adapters/docs/index.go:482`), so `clara-home` matches nothing in the `clara-home` source and the adapter answers success-empty rather than `filter_unsupported`. Only the td adapter compares against a workspace name, which is why only td rows ever appeared. Proven: `recall query specialized --scope project=clara-home` gives 0, `--scope project=briefs` gives 4, unscoped gives 8. The plugin's per-source timeout is the same 2 s as the CLI and its whole-request budget is larger, so timeouts are not the cause. `params.view` is decoded and never read; `describe` declares no views and no filters.

## Milestones

### M4a. Pointer and focus parity in the shared browser — sidecar only

**Status:** implemented 2026-09-03 on `plugin-ux-m4a`, awaiting review.

No protocol change. Files: `internal/pluginbrowser`, `internal/app/content_deck.go`, `internal/state`, plus a shared query-row renderer extracted into `internal/workspacelist`. **Tracking:** td-62b81c. Folds in td-fcb648 (pane-mode reserved keys, state-aware `ClaimsKey`, query-bound feedback), td-c2dc19 item 1 (the flash that is never cleared) and td-83a3fa item 1 (body cache theme key), because each touches the same rows.

1. **One pointer model.** `Model` owns a `mouse.Handler` and a `mouse.HitMap`, cleared at the top of `View` and rebuilt in paint order: list box, detail box, each visible row, the query row, the View pill, the outcome pill, each notice, both scrollbars' thumb and track, and the divider last (`HitMap.Test` scans in reverse). `TabPlugin.Update` routes every `tea.MouseMsg` through it whether or not an overlay is open; an open overlay still wins.
2. **Click semantics, identical to the keys.** A click inside a pane focuses it through the same `SetPaneFocus` the app's focus ring calls. A click on a row selects it; a second click on the selected row, or a double click, is `Enter`. A click on the query row is `/`; a click on the View pill is `v`; a click on the outcome pill or a notice opens the coverage modal (M4a shows the notices in full; M4b gives it data). The wheel scrolls the box under the pointer, not the focused one, through `mouse.WheelScrollLines`. Nothing a click does is reachable only by click.
3. **Scrollbars.** The table and the detail viewport each draw `ui.RenderScrollbarWithState` when content overflows, with `ui.RegionScrollbarThumb`/`RegionScrollbarTrack` regions and the press-and-drag gesture `filebrowser` and `overview` already implement.
4. **Drag rail.** The gap draws `ui.RenderHandle` with `ui.HandleStateFrom(hover, drag)`, its hit box widened one cell each side as `paneframe.DividerHitBox` does. Dragging moves the list share within floors (the detail floor host.md already names); the share is persisted per plugin ID in `internal/state` beside `GetGitStatusSidebarWidth`, and restored on the next launch.
5. **Tab reaches the browser.** `handleAppContentKey` cycles the focused plugin's own `PaneFocusStops` when the deck holds only its Primary leaf, so any `PaneFocusProvider` gets Tab and Shift+Tab without binding them. The browser keeps `tab` out of `browserOwnedKeys`. Pane mode is unchanged: a browser inside a Resource leaf has one stop and the leaf's focus is the deck's.
6. **The query line is the app's filter row.** Render it with `workspacelist.Filter.RenderRow` (or the same style swap, caret and count if the type cannot be shared cleanly): `Muted` prompt and placeholder when idle; `Title` prompt, the text, a `▌` caret and the right-aligned `shown of total` when focused. Reaching `resource.MaxQueryChars` shows a one-line hint instead of silently refusing the key.
7. **The View pill is a header control.** Place it through `ui.ReserveHeaderControls` with the three-rung ladder `workspacelist.headerAttempts` uses, so it has a hit rect and sheds its word before its glyph.
8. **No "nothing to do" text.** `a` with no applicable action is inert and unclaimed (`ClaimsKey` becomes state-aware for `/`, `v` and `a`, per td-fcb648). The flash and footer string are deleted; the Actions hint's absence already says it.
9. **Detail panel.** Audit against `issueview` and `docview`: labels `Muted`, identifiers `Info`, links `Link`, timestamps `Muted`, status through `toneStyle`; key the body cache on `renderer.StyleKey()`; prove with a theme switch mid-session.

**Evidence:** `go test ./...` green. New tests in `internal/pluginbrowser` driving `tea.MouseMsg`: click selects, second click opens, click focuses the other pane, click on the query row begins editing, click on the pill opens the View modal, wheel over the detail scrolls the detail while the list is focused, drag on the rail changes the split and the value round-trips through state, scrollbar press jumps. A test in `internal/app` that Tab on a Primary-only deck cycles the browser's stops. An isolated `tmux-drive.sh` capture at 160x45 showing the rail, both scrollbars, the focused query row, and the same frame after a theme change; compared against `mockups/recall-studio.results-answered.txt` for the rows that mockup fixes.

Captures are under [proof/m4a/](proof/m4a/README.md), which also records how the run was isolated and which fixture modes stood in for data the fixture's normal page does not have.

**Deviations.**

1. **The query row goes through a new shared function, not the `Filter` type.** `workspacelist.Filter` owns the workspace sidebar's own query state, which the browser keeps per collection and persists with a tab; adopting the type would have meant two authorities on one query. The row's *rendering* moved into `workspacelist.RenderQueryRow(width, QueryRow{...})` — prompt, placeholder, style swap, `▌` caret, right-aligned cell — and `Filter.RenderRow` now calls it, so there is still exactly one function that decides what a query bar looks like. The browser passes its own placeholder and its own right-hand cell, because "8 of 22" and "500 of 550   answered" are different claims.
2. **The scrollbar column is reserved on every table and every document, not only when content overflows.** That is the shared renderer's own convention (`RenderScrollbar` returns a spacer column when everything fits) and it is what stops the table reflowing its columns the moment a page grows past its box.
3. **M4a builds the coverage card and picks its key, which the plan left to M4b.** Item 2 says a click on the outcome or a notice opens it, and item 2 also says nothing a click does may be reachable only by click — so the card needed a key here. It is `c`, from the free set, and it is claimed only where there is something to explain: a page that answered with no notices has nothing the card could add. What M4a shows is the outcome, Sidecar's own sentence for what that word means, and every notice untruncated. M4b gives it `coverage[]`, `omitted`, the Retry button, and the host.md record.
4. **Reserved keys are seeded in `pluginbrowser.New` rather than at the pane host's call site.** td-fcb648 asked for the call at the pane browser's construction; doing it in the constructor answers for every placement, including the next one, and `SetReservedKeys` still overrides.
5. **The query-limit hint is the query row's right-hand cell**, replacing the count while it stands and cleared by the next accepted edit. The footer was the other candidate and is the wrong one: the design language says a condition belonging to a surface lives on that surface's own rows.
6. **The detail audit found one gap and closed it, and left the rest alone.** Measured against `resourceview`, which is this card's nearest twin, `detail.go` was already right about labels (`Muted`), values (`Body`), section rules (`Muted`), timestamps (`Muted`) and status (`toneStyle`, byte-identical to resourceview's). What it did not do was show `doc.Identity` when the document also had a title, so the string a reader quotes back to the plugin's own CLI was invisible; it now draws resourceview's meta row. Item 9's "identifiers `Info`, links `Link`" was not adopted: resourceview draws the identity `Muted` and the `o  open <url>` line `Muted`, and matching it is the point — a third look for the same two things is the drift this milestone exists to stop.
7. **`internal/plugins/workspace`'s keyboard test changed with the contract.** `TestACollectionPaneClaimsTheBrowsersKeys` asserted that a focused collection pane claims `a` unconditionally, which is the behaviour td-fcb648 filed. It now asserts the rule instead: `/` and `v` are claimed because that collection declares them, and `a` is not because it declares no action.

### M4b. Scope, coverage, and honesty — protocol, host, recall

Applies four protocol revisions now rather than at freeze, because recall cannot be honest without them and the freeze is what M4d does once recall is. **Tracking:** td-9ca6a7 (sidecar: protocol and host), td-786e42 (recall repo: the plugin and the scope mapping; filed there, mirrored on td-35bcd1 which records the root cause).

**Protocol revisions**, each moving from the README's pending table into [protocol.md](protocol.md) as applied:

| Revision | Shape |
| --- | --- |
| `filters[]` on a collection's describe | `{id, label, kind: "choice"\|"text", choices?: [{id, title}], default?: string}`; bounded 8 per collection, 32 choices per filter. `list.params.filters{}` carries `{id: value}` and is persisted with the collection tab and with the global tab's remembered query. A filter the collection did not declare is dropped. |
| `page.omitted` | `{suppressed, dropped}` counts, rendered in the summary row as data ("8 shown · 1 below floor · 6 over budget"). |
| `page.outcome: "failed"` | Every asked source failed; the host renders an error card over an empty list, never "no matches". |
| `page.coverage[]` | Optional `{source, state: "answered"\|"timeout"\|"unhealthy"\|"skipped"\|"failed", reason?}`, bounded 64 rows and 200-char reasons. Read only by the coverage modal; notices stay the one-line summary. |

And one rule stated rather than added: `outcome` describes the row set of *this* page and nothing else (td-e476aa item 1). A collection whose rows are all present answers `answered` even when what the rows describe is unhealthy.

**Host.**

- The View modal grows the filters block the mockup draws: after the sort list, one control per declared filter (`choice` as a radio group, `text` as an input), then Done. Changing one relists with `params.filters`.
- The pill folds applied filters into its label: one applied filter shows its chosen title (`⇅ rank · This project`), more than one shows a count (`⇅ rank · 3 filters`), and the ladder sheds the suffix before the sort word before the glyph.
- The coverage modal opens from the outcome pill, from a notice, and from one host key chosen from the free set in `keymap/hostkeys.go` and recorded in host.md. It shows the outcome and its definition, every notice untruncated, `omitted` as two lines, and the `coverage[]` table when present, with a Retry button that is `r`.
- The `search: required` empty query stops reporting `abstained` in the footer (td-c2dc19 item 2); the browser carries an internal *unqueried* state and the footer says so.
- The fixture plugin declares filters and returns `omitted`, `failed` and `coverage[]`; the conformance suite and `testdata/protocol/` grow the canonical JSON.

**Recall** (in the recall repository):

- Declare on `results`: `scope` (choice: `project` "This project", `all` "Everything"; default `project` when project context is present, `all` otherwise), `source` (choice: any, then each configured source ID), `type` (choice from the profile's record types), `since` (text, RFC 3339 date). Read `params.filters` in `sidecarListResults`; stop applying `sidecarProjectScope` unconditionally; delete the "scoped to project" notice, which the pill now says.
- Fix the mapping. Project context restricts to what is *under* `project.root`: a documents source whose location is `root` or inside it answers with paths under `root`; a td source answers when its workspace root is `root`; an adapter that cannot evaluate the restriction reports `filter_unsupported` and degrades coverage instead of answering success-empty. `project.name` is never compared against a documents `Project` field again.
- Emit `omitted` from the suppressed and dropped ledger, `failed` from recall's own failed state, `coverage[]` from the `--explain` ledger, and `answered` for the `sources` collection.
- Either read `params.view` or stop declaring the field; declare nothing that is not read.

**Evidence:** conformance JSON for each revision; browser tests that a filter change relists with the new params and that the pill label follows the ladder; a persistence round trip carrying `filters`; the coverage modal rendered from a fixture page with 13 coverage rows, held to its box. Live: the Recall tab in `clara-home` with the query `specialized` shows 8 rows under *This project*, more under *Everything*, and the coverage modal names `clara-home-semantic (unhealthy)` with its reason; a stripped capture compared with `mockups/recall-studio.scope-picker.txt` and `recall-studio.results-degraded.txt`. td-35bcd1's proof case (`project.name = "nosuchproject"`) answers `degraded` with a `filter_unsupported` row, not `abstained`.

### M4c. Plugin authoring guide and protocol reference — docs only

**Tracking:** td-40eb97. Runs beside M4a; touches no Go.

- `docs/reference/plugin-protocol.md` becomes the single authority for `sidecar.plugin/v1` (marked draft until M4d freezes it). The contract text moves out of [protocol.md](protocol.md), which shrinks to the design rationale (*Why not a generic UI catalog*, *Deferred to evidence*, *Fixtures*), the pending-revisions table, and a link. One authority, as the frozen `terminal-resource-provider-protocol.md` already is for resource v1. The reference notes the `testdata/protocol/` path move recorded in td-4ccfb7.
- `docs/guides/active/creating-plugins.md`, for someone who has a CLI and wants it in Sidecar: choosing a class; the five methods with a complete minimal plugin in a scripting language checked in under `docs/guides/examples/hello-plugin/` and proven by `sidecar plugin check`; configuring with `sidecar plugin add` and what the trust act means; what the host renders for each declaration, pointing at the mockups; the keys and pointer routes the host owns so the author declares none; the outcome vocabulary and why an empty page must say which claim it makes; filters, views and sort; context kinds; freshness (`watch`, `everySeconds`, `sidecar plugin changed`); `sidecar open --plugin` and the layout spec; conformance against the fixture; and a short section on embedded plugins that hands off to `.agents/skills/create-plugin/SKILL.md`.
- `.agents/skills/create-plugin/SKILL.md` gains a first paragraph routing protocol-plugin authors to the guide; `docs/reference/cli.md` and the README's reading order link both documents.

**Evidence:** the example plugin passes `sidecar plugin check` from an isolated config in CI (a test under `internal/pluginhost` that runs it); every relative link in the two documents resolves; the guide's walkthrough executed once from a clean state by the reviewer.

## Sequencing

M4a and M4c are independent (disjoint files) and run as two parallel lanes in one worktree each. M4b's host half follows M4a because both edit the pill, the modal and the notice rows; its recall half starts as soon as the decisions below are confirmed and lands in the recall repository on its own schedule, with the fixture standing in for it in sidecar's tests. M4d follows all three.

## Decisions to confirm

Each has a default the work proceeds under; a different answer changes M4b only.

1. **Default scope on a project surface is *This project*, widened per tab through the View modal; global surfaces default to *Everything*.** The project context kind exists so a plugin can narrow to the project the surface shows, and the pill makes the narrowing visible and one keystroke wide. The alternative, defaulting to *Everything* everywhere, makes the context kind decorative for recall.
2. **`filters[]` is applied now, not at freeze.** The mockups already draw it, recall needs it, and applying it after freeze would be a v2 change for the same work.
3. **Coverage detail is data (`coverage[]`), not notice text.** Recall already holds the per-source ledger; four 200-char notices cannot carry thirteen sources.
4. **`outcome` describes only the row set.** Stated as a protocol rule; recall's `sources` collection changes from `degraded` to `answered` as a consequence.

## Changelog

- 2026-09-03: opened from nt-addf11 and two audits of the browser and of recall's plugin subcommand.

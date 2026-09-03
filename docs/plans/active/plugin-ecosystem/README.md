# Plugin ecosystem: protocol plugins, embedded plugins, one host

**Status:** M1 through M3 merged to `main` on 2026-09-03 (merge ce052436), behind the `plugin_protocol` flag; the recall reference plugin (td-26c2b4) is in review in its own repository; M4 is split: M4a (pointer and focus parity in the shared browser), M4b (scope filters, coverage, outcome honesty) and M4c (authoring guide and protocol reference) are implemented in [browser-parity-and-scope.md](browser-parity-and-scope.md), and M4d (freeze, migration, docs site, flag flip) follows them. Implemented: M1 (descriptor and generalized global host, td-01b62b), M2a (the protocol host half — `internal/pluginhost`, `plugins.external`, the fixture and conformance suite, and the `sidecar plugin` CLI — td-a6d276), M2b (the shared browser `internal/pluginbrowser` and the protocol global tab, td-0d3539) and M3 (collection and row tabs in the Resource leaf on both surfaces, the `resources` live-refresh binding, `sidecar open --plugin`, `sidecar plugin changed`, and the layout spec's `collection`/`query`, td-44fe20). M0's mockups are in [mockups/](mockups/); four of the protocol revisions they surfaced were applied in M4b and the rest are still pending. M4d is proposed. Decisions settled with the maintainer on 2026-09-02. **Tracking:** td-f9f007.

**Related:** [Terminal resource providers](../../implemented/terminal-resource-providers.md) built the executable protocol, the `Resource` leaf, and the trust posture this plan extends; its protocol stays frozen and keeps working. [Hosting Herdr plugins in Sidecar](../../deprecated/herdr-plugin-support.md) is superseded by this plan. [Pane switcher everywhere](../pane-switcher-everywhere.md) and [Cross-project td issue links](../cross-project-issue-links.md) are the two nearest live plans and neither conflicts.

**Reading order:** this file, then [docs/reference/plugin-protocol.md](../../../reference/plugin-protocol.md) (the contract an external plugin author implements) with [protocol.md](protocol.md) beside it (why that contract has the shape it has), then [host.md](host.md) (what changes inside Sidecar), then [browser-parity-and-scope.md](browser-parity-and-scope.md) (the M4a–M4c work in progress). [mockups/](mockups/) holds the M0 screen mockups, and [docs/guides/active/creating-plugins.md](../../../guides/active/creating-plugins.md) is the authoring guide the reference is written for.

## Decision first

Sidecar hosts two classes of plugin through one descriptor:

- **Protocol plugins** are explicitly configured external executables in any language. They answer five one-shot JSON methods (`describe`, `resolve`, `list`, `get`, `act`) and Sidecar renders everything: a navbar tab with a list-and-detail browser, collection and document tabs in the pane decks of both workspace projections, host-owned keys, live refresh, persistence, and content links. Recall, DEX, and ongoing are written against this contract before they have any UI of their own, and every existing terminal resource provider (Jira) is already a protocol plugin that happens to declare only matchers. A plugin the Sidecar release knows nothing about is a config entry.
- **Embedded plugins** are Go modules compiled into Sidecar with their own Bubble Tea UI. Tasks and the td monitor stay in this class with their UIs untouched. What changes is that they are described, enabled, placed, and hosted through the same descriptor as protocol plugins, so "global plugin" stops being a hardcoded special case for Tasks and a second global plugin is a descriptor, not an enum value.

A protocol plugin will never render as richly as Tasks, and this plan does not pretend otherwise: a vocabulary that could express the quadrant board and the agent queue would be a worse Bubble Tea. The vocabulary instead covers what a browser over a tool's data needs, and grows one typed object at a time when a real plugin proves the need.

## Why now

- Recall, DEX, and ongoing each have a complete CLI with `--json` and no TUI. Designing them against a Sidecar contract before writing a UI means their Sidecar presence costs one subcommand, and the protocol gets three real implementers instead of one.
- The resource protocol proved the shape (executable, JSON, host renders, frozen after a live implementation) but stopped at click-to-resolve. Lists, search, actions, and refresh are the same shape with more nouns.
- Tasks as a special-cased global host works but cannot be repeated. The second global plugin needs the generalization regardless of class.

## Settled decisions

1. **Two classes, one descriptor.** `plugin.Descriptor` is the only thing the assembly, settings page, global host list, palette, and `sidecar plugin list` read. Class decides who renders; scope decides lifecycle; placements decide where content shows. Its fields are listed in [host.md](host.md#the-descriptor).
2. **The protocol is `sidecar.terminal-resource/v1` grown, not replaced.** Same invocation model, environment allowlist, process-group rules, sanitization, limits, error codes, and matcher rules. Three new methods and a `sections` field. Old providers keep answering the old identifier and keep working unchanged.
3. **Domain-shaped vocabulary, not a generic widget tree.** Collections with columns, views, and sort keys; resources with fields, body, and sections; search as a `list` parameter; typed actions with up to eight inputs. A2UI's posture and action loop are borrowed; its component catalog is not, for the reasons in [protocol.md](protocol.md#why-not-a-generic-ui-catalog).
4. **One-shot invocation in v1.** Live search is debounce plus cancel; mutations are `act`; background updates are plugin-declared `watch` paths and poll intervals through `livepanes`, plus `sidecar plugin changed` on the file bus for a plugin that wants to poke Sidecar itself. Resident mode carries the same objects later and only with measured evidence that process startup, not tool latency, is the cost.
5. **Project context is declared, not granted twice.** A plugin lists the context kinds it reads in `describe`; the settings page and `sidecar plugin list` show them; configuring the plugin is the trust act. Only `project` and `selection` exist in v1.
6. **Panes reuse the Resource leaf.** A collection tab is a second tab shape in the existing leaf, not a new leaf kind, so `panelayout`, `paneframe`, both `pane_host.go` files, and the layout floors do not change and persistence gains fields rather than an array.
7. **Tasks keeps its embedded UI and joins the ecosystem through the descriptor.** A later, evidence-gated milestone ships a protocol-based Tasks provider beside it and measures the gap; retiring the embedded UI is a decision for that evidence, not this plan.
8. **Enablement is `plugins.<id>.enabled` for every plugin.** The `tasks_plugin` and `notes_plugin` flags and the `terminalResources.providers` section become read-only aliases for one minor release and are then removed.
9. **No discovery, no manifest, no sandbox.** Sidecar never scans directories or `PATH`, never auto-enables, never lets a repository declare a plugin, and says plainly that a process boundary is crash isolation. `sidecar plugin add` shows what will run and writes one config entry. A Herdr-manifest adapter could be written against these seams later; it is not part of this plan.
10. **Recall is the reference plugin.** It exercises search, ranked results with excerpts, a `get` that expands a locator, degraded and abstained outcomes, and global scope with optional project context. The protocol freezes after recall and the host have each been revised from what the other found, as `sidecar-jira` did for resource v1.
11. **Every owned capability has a non-interactive path from the first milestone.** Hosting plugins is something Sidecar owns, so the standing "presentation layer, no CLI parity" exception does not apply to `sidecar plugin`.
12. **Plugins are theme-aware without doing anything.** A protocol plugin sends tones, kinds, and text; the host maps them to the active theme, so a theme change re-renders every plugin the same way it re-renders a td issue card. An embedded plugin keeps the existing contract: theme injected at construction and again on `ThemeChangedMsg` without resetting its state. Mockups of either class follow Sidecar's design language reference so what is reviewed on the canvas is what ships.

## Scope boundary

**In scope:** the descriptor and generalized global host; unified enablement; protocol v1 host implementation with the shared browser; collection tabs in the Resource leaf on both surfaces with live refresh; `sidecar plugin` and `sidecar open --plugin`; a fixture plugin and conformance suite; the recall reference plugin's Sidecar subcommand (in recall's own repository); documentation on the site; migration of the resource config section.

**Out of scope:** resident transport; nested trees and boards; running protocol plugins on a remote host's side; porting Tasks or td to the protocol; making enablement live without restart; a Herdr-compatible manifest loader; DEX and ongoing subcommands beyond what M4 needs to confirm the contract generalizes (they are written in their own repositories against the frozen protocol).

## User contract

| Gesture | Required result |
| --- | --- |
| `sidecar plugin add recall --command recall sidecar-plugin` then restart | Recall appears as a global tab after Sessions and Activity, keyed `0` if it is the first plugin-provided global tab. A loading state shows until `describe` lands; a setup card shows if it fails. |
| Open the Recall tab, type `dex` | After a 250 ms pause the results collection lists ranked rows with excerpts. Typing again cancels the in-flight process group. A `degraded` outcome shows its notice line; an `abstained` outcome shows "no matches". |
| `Enter` on a row | The detail box shows the resource with its sections; a second `Enter` focuses it. `o` opens `sourceUrl` through the confirmed path when present. |
| `sidecar open --plugin recall --collection results --query dex --split right` from a terminal pane | A Resource leaf opens beside the terminal with one collection tab, on the viewer's screen, with the same declines `open` gives today. Relaunch restores the tab with its query. Implemented in M3. |
| `sidecar open --plugin ongoing --collection projects recall` | A document tab for that project; the plugin received `project` context if it declared it. Implemented in M3. |
| `Enter` on a row of a collection PANE | The row opens as a second tab in the same Resource leaf, re-keyed to the resource's identity; a second `Enter` focuses that tab and costs no process. Implemented in M3. |
| `/` in a collection PANE, then typing `dex1` | Every character reaches the query, on the project workspace and on the Sessions surface alike: the digit does not switch a tab, `` ` `` does not cycle the header, and the footer stops advertising the keys the pane has taken. An open View control or action form owns the keyboard the same way. Implemented in M3. |
| `sidecar layout apply --spec` with `{"kind":"resource","provider":"recall","collection":"results","query":"dex"}` | That collection opens as the Resource pane's tab; `layout get` reports the active tab's collection and query, so get → edit → apply is a round trip. Implemented in M3. |
| `a` on a row with item actions | An action menu; choosing one with inputs shows a small form; confirm calls `act`; the outcome message flashes and the named collections re-list. |
| A file under a declared `watch` path changes while a collection from that plugin is visible | The list refreshes within the livepanes latency window without the user pressing anything. Nothing refreshes when no tab from that plugin is on screen. Implemented in M3. |
| `sidecar plugin changed dex --collection people` from a shell hook | Same refresh, through the file bus. Implemented in M3. |
| Bound to a remote host, open a collection with `context: ["project"]` | The plugin receives `project.hostId` and either answers or refuses naming the host. Sidecar never substitutes a local path. M3 built the host verb (`content read --operation collection`); binding a remote-bound pane to it waits on `content describe` carrying collections, which is M4's. |
| Disable Tasks in settings | `plugins.tasks.enabled` is written; the `tasks_plugin` flag is left alone; after restart the global tab is gone and `0` names the next global plugin or nothing. Implemented in M1. |
| A plugin answers `sidecar.terminal-resource/v1` only | It keeps working exactly as today: matchers, resolve, the Resource card, `--provider`. |

## Delivery

Each milestone ends net-better than the tree before it, lands on main, and is gated by the `plugin_protocol` feature flag until M4 flips it on.

### M0. Mockups and contract review

- Mock up the browser in both placements using the TUI mockup tool the maintainer uses for screen design: a global tab (recall results plus detail), a narrow pane with a collection tab (ongoing projects with views and sort), a document tab with sections (a DEX person with fields and timeline), the action form, the degraded and setup states. Files land in [mockups/](mockups/) as `.tui.yaml` with rendered text snapshots beside them.
- Walk the contract (then this plan's draft, now [docs/reference/plugin-protocol.md](../../../reference/plugin-protocol.md)) as recall's author: write recall's `describe` and one `list` and `get` response by hand against the real CLI's `--json` output. Every field recall cannot fill or needs and cannot express is a protocol revision before any host code.
- Do the same on paper for DEX (`context`, `timeline`, `log` as an `act` with `multiline`) and ongoing (`list --view --sort`, `show`, `favorite`/`set` as actions with `choice` inputs) to confirm the vocabulary generalizes. Record what each needed in the protocol's changelog.
- **Evidence:** three mockup files reviewed on the canvas; a protocol revision commit that cites what recall, DEX, and ongoing each forced.

### M1. Descriptor and generalized global host (embedded class) — implemented, td-01b62b

- `plugin.Descriptor` in `internal/plugin`; one per plugin in `internal/plugins`; `assembly.Descriptors()` is the ordered catalog and `assembly.Plan` filters it. Tab order and IDs are unchanged and the existing ordering tests prove it.
- The `GlobalTab` enum and `globalTasksHost` are gone: the global tab row is a descriptor-driven ordered slice and each hosted plugin has a `globalPluginHost`. Sessions and Activity keep `8` and `9`; the first plugin-provided global tab keeps `0`; a second takes no number key and is reached by `[`/`]`, the palette command `focus-<id>`, or a click. The start/stop counters and every scope test pass unchanged.
- Unified enablement: `plugins.notes.enabled` and `plugins.tasks.enabled` with the two flags as read-only aliases; the settings page is one loop over descriptors; `sidecar plugin list [--json]`.
- **Evidence:** `go test ./...` green; an isolated `tmux-drive.sh` run at 160x45 whose stripped header capture is byte-identical before and after, with `0`/`8`/`9` opening Tasks, Sessions, and Activity in both builds, and with `plugins.tasks.enabled: false` removing the Tasks tab while `tasks_plugin` is still true; `TestSecondGlobalPluginGetsNoNumberKeyAndIsReachableByCycling` proving the fourth global entry takes no number key.

### M2a. Protocol host and CLI — implemented, td-a6d276

- `internal/pluginhost` (a rename and extension of `resourceprovider`): the new envelope, `list`/`get`/`act`, the describe snapshot with context kinds, collections and actions, cancellation of superseded `list` calls per pane, and the new limits. Old providers are dispatched with the old identifier by the same manager, the same cache, and the same concurrency budget.
- `plugins.external[]` with `Config.PluginInstances()` as the one ordered list both sections load into; `scope: "project"` refused with a message naming what to do instead.
- Protocol descriptors projected from configured instances (`plugin.ProtocolDescriptors`), so `sidecar plugin list` reports them.
- The fixture plugin speaks both identifiers from one binary and simulates every hostile case in [the reference's fixtures section](../../../reference/plugin-protocol.md#fixtures); the conformance suite runs against the real process, with canonical JSON under `internal/pluginhost/testdata/protocol/`.
- `sidecar plugin list --describe|check|call|add|remove|enable|disable`, all with `--json` and Agent docs; `terminal-links` unchanged as the frozen section's surface.
- Everything behind the `plugin_protocol` feature flag, default off.
- **Evidence:** `go test ./...` green; the fixture driven from an isolated config through `plugin list`, `check --list/--get`, `call describe|list|get|act`, and `add/disable/remove`; every hostile fixture case bounded, including an `act` that never returns ending in a killed process group at its configured timeout.

### M2b. The browser in a tab — implemented, td-0d3539

- `internal/pluginbrowser`: the shared list-and-detail browser with host-owned keys, a query line with a 250 ms debounce and cancellation, the View modal (sort keys and views, built as `internal/overview/view_flyout.go` builds its own), notices, paging on demand, a detail box rendering fields, body and sections with relative timelines, the action menu with typed input forms and a confirm step, and the capability interfaces implemented once. Host-independent in the same way `resourceview` is.
- A global tab for each enabled protocol plugin with a `tab` placement, hosted through M1's `globalPluginHost` with no new host type; the project context, including a remote surface's `hostId`, is republished from the same place the matchers are.
- **Evidence:** `go test ./...` green; the browser driven against the real fixture process through a real `pluginhost.Manager` in `internal/pluginbrowser/live_test.go`, including every hostile describe mode ending in a bounded card; an isolated `tmux-drive.sh` run at 160x45 covering the empty query, a live query, a document with sections, the View modal, the action menu, the input form, the outcome flash, the degraded notice, the abstained state, the setup card, and the narrow reflow.

### M3. Panes, persistence, refresh, and `open` — implemented, td-44fe20

- Collection and row tabs in the Resource leaf on both surfaces. `resource.Reference` grows `Shape()` with three alternatives — matched, collection, item — and `Valid()` accepts exactly one; `PaneResourceTabJSON` grows `collection`, `query`, `view`, `sort` and `cursorId`; decode refuses an ambiguous tab. The dispatch lives on `resourceview.Model`, so both projections inherit the shapes by holding the model they already hold and neither `pane_host.go` nor either `content.go` changes.
- The `resources` livepanes binding on both surfaces: `Targets` reads the plugin's declared `watch` paths from the cached describe snapshot, `Prepare` stats them once per describe generation and drops the entries whose tabs have left the screen, `Refresh` re-lists visible collections and re-fetches visible documents, and the declared poll interval is a ticker inside the binding. A refresh the project surface vetoes while a modal covers the panes stays owed and lands when the veto lifts. `sidecar plugin changed` reaches the same refresh through the file bus.
- Both surfaces report a collection pane's keyboard ownership to the host: while its query line is taking text or one of its overlays is open, the tab digits, `` ` ``, `~`, `[`, `]` and the other host globals stay with the pane. `workspace.Plugin` says so through `plugin.TextInputConsumer` and `plugin.KeyRouter`; the Sessions surface, which is not a plugin, says the same thing through `overview.WorkspacesConsumesTextInput` and `WorkspacesBlocksGlobalKeys`, which the app asks.
- `sidecar open --plugin ID [--collection C] [--query Q] [ROW]` with `--provider` as the locator form's alias; layout spec `collection`/`query`; `contentpanes.Source` gains `ListCollection` and `GetCollectionItem`, whose remote twin runs `sidecar content read --kind resource --operation collection|item` on the host that owns the pane.
- **Evidence:** `go test ./...` green; parity tests that the Resource viewer answers every tab shape on both surfaces and that `livepanes.Set.Kinds()` lists `resources` on both; twin keyboard-ownership tests on both surfaces (`/` then a digit reaches the query, an open overlay blocks host globals); a persistence round trip proving a collection tab restores with its query, and an isolated `tmux-drive.sh` run proving the same across a relaunch; a watched-file change re-listing within the latency window in an isolated run; `sidecar open --plugin` from a terminal pane landing beside it.

### M4a. Pointer and focus parity in the shared browser — implemented, td-62b81c

The browser gains the pointer model, scrollbars, drag rail, focused query row, header-control pill and Tab reachability the rest of Sidecar has, by composing `internal/mouse`, `internal/paneframe`, `ui.Scrollbar`, `workspacelist.Filter` and `ui.ReserveHeaderControls` rather than adding a second implementation of any of them. Details, evidence and the folded-in review chores are in [browser-parity-and-scope.md](browser-parity-and-scope.md#m4a-pointer-and-focus-parity-in-the-shared-browser--sidecar-only).

### M4b. Scope, coverage, and honesty — proposed, td-9ca6a7 (sidecar) and td-786e42 (recall repo)

Applies `filters[]`, `omitted`, `failed` and a new `coverage[]` to the protocol and the host, so the scope a list is narrowed to is visible in the pill and one keystroke wide, and a degraded page can show why. Recall becomes global first with a profile chooser and stops applying the project context on its own, which is what made every documents source answer empty. Decisions are settled in [browser-parity-and-scope.md](browser-parity-and-scope.md#decisions).

### M4c. Plugin authoring guide and protocol reference — implemented, td-40eb97

[docs/reference/plugin-protocol.md](../../../reference/plugin-protocol.md) is the single authority for the contract, [docs/guides/active/creating-plugins.md](../../../guides/active/creating-plugins.md) takes an author from a CLI to a plugin that passes `sidecar plugin check`, and `docs/guides/examples/hello-plugin/` is that plugin, run through the real host by a test in `internal/pluginhost` so it cannot rot. [protocol.md](protocol.md) keeps the rationale and nothing else. Deviations are recorded in [browser-parity-and-scope.md](browser-parity-and-scope.md#m4c-plugin-authoring-guide-and-protocol-reference--implemented-td-40eb97).

### M4d. Freeze, migrate, flag flip, site docs

- Recall's `sidecar-plugin` subcommand (td-26c2b4, approved) has been revised against the draft once; M4b revises both again; then freeze `sidecar.plugin/v1`.
- DEX and ongoing subcommands follow against the frozen contract, each in its own repository. Anything they cannot express is a v2 note, not a v1 change.
- `terminalResources.providers` migration; `terminal-links` aliases; flag flip; site docs (a "Plugins" page replacing "Terminal resources", linking the M4c reference and guide); `docs/reference/cli.md`.
- Move this plan set to `docs/plans/implemented/` and fix inbound links.
- **Evidence:** recall, DEX, and ongoing each listed by `sidecar plugin list --describe` on a machine with all three; the three mockups from M0 compared against real screenshots.

### M5. Evidence-driven only

Do not schedule these because the protocol exists:

- Resident transport, when measured process startup (not tool latency) is the cost on a real plugin.
- A protocol-based Tasks provider beside the embedded one, to measure the gap before any retirement decision.
- Nested trees and boards, when a plugin needs them.
- Host-side execution of protocol plugins for remote-bound surfaces.

## Risks

| Risk | Mitigation |
| --- | --- |
| The vocabulary is too small and plugins route around it with markdown bodies | M0 writes three real plugins' responses by hand before host code; each missing noun is a protocol change while changing is cheap |
| The vocabulary grows toward a widget tree | Every addition is a domain noun with host-owned behaviour on both surfaces, or it goes to the embedded class |
| Generalizing the global host regresses Tasks | M1 changes no behaviour and is proven by the existing start/stop and scope tests plus a before/after header capture |
| Live search spawns a process per keystroke | Debounce, cancel superseded calls, keep the previous page visible; measure on recall before considering resident mode |
| A second livepanes binding per surface drifts | One `Binding` literal per surface, `Kinds()` parity test, and the Resource viewer shared through `contentpanes` |
| Config migration loses a provider | Alias reads for one minor release; the saver never drops unknown sections; `sidecar plugin list` shows where each entry was read from |
| A plugin's `watch` path is outside the home directory or is a whole disk | Validated at describe time, bounded to 8 per plugin, and refused with a typed reason shown by `plugin list --describe` and `plugin check`. The refusal fails the whole describe, as an uncompilable matcher pattern already does: a plugin that would have the host watching `/etc` has a bug the user must see, not one to publish half of |

## Protocol revisions pending from the M0 recall mockup

Writing recall's screens against its real `--help` surfaced facts the draft cannot carry. Each is a proposed revision to [the protocol reference](../../../reference/plugin-protocol.md), applied after the maintainer confirms. M4b applied four of them — the `failed` outcome, `page.omitted`, `filters[]` with `list.params.filters`, and `page.coverage[]` — plus the rule that `outcome` describes only the row set; those rows have left this table and are in [the reference](../../../reference/plugin-protocol.md) instead. What is left below is still unimplemented and waits for M4d.

| Gap | Proposed revision |
| --- | --- |
| Excerpts carry a kind (matched span, record opening, unmarked) that the mockup fakes with `›`/`~` prefixes the host cannot explain | Add column `kind: "excerpt"` and an optional per-cell `{text, mark}` shape for that kind; the host draws the mark and owns the legend |
| `--as-of` changes what every row means and must survive refresh, but `list.params` has only `query`, `view`, `sort`, `filters`, `cursor` | Add `asOf` (RFC 3339) to `list.params` and to the persisted collection tab; the header shows it as a chip |
| `status.label` length is unbounded but the host must fit it in a reserved column | Add a 24-char bound under Limits |
| The narrow reflow rule names only the secondary column | State it fully: rank and primary on line one; status label, the remaining short columns, and the secondary text folded into line two |
| An empty detail box in a `Tab` placement | Host rule, for [host.md](host.md): show the plugin's next collection (recall's `sources`) rather than a blank card, so `abstained` is verifiable in place |
| `get` carries no filters, so a row found under a filter-chosen scope (recall: a raised-sensitivity profile) expands under the plugin's default scope and can be denied | Add `params.filters{}` to `get`, sent exactly as `list` sends them, so expansion runs under the scope the row was found in |

## Open questions

Not blocking M0 or M1; each has a default the plan proceeds under.

- Should `selection` context be offered in v1 at all, or deferred with resident mode? Default: declared in the protocol, implemented in M3 only for actions invoked from a text selection in a document pane.
- Whether `plugins.external` is the right key name versus a top-level `plugins.protocol` or `extensions`. Default: `plugins.external`, because the settings page already groups everything under plugins.
- Whether a `Project`-scoped protocol plugin (re-described on project switch) is worth supporting in v1, or every protocol plugin is global and reads `project` context per call. Default: global only in v1; `scope: "project"` is accepted by config validation and rejected with a clear message until there is a plugin that needs it.

## Changelog

- 2026-09-03: M4b implemented (td-9ca6a7). Four pending revisions applied and removed from the table above — `filters[]` with `list.params.filters`, `page.omitted`, the `failed` outcome, and `page.coverage[]` — along with the rule that `outcome` describes only the row set. The host draws the filters in its View modal, folds the collection's scope into the View pill, turns M4a's coverage card into a modal with a Source/State/Elapsed/Reason table and a Retry, and gives a `search: required` collection with no query its own *unqueried* state instead of `abstained`. The open question "should a global protocol tab remember its last query and view" is answered yes and implemented, because the applied filters had to be persisted somewhere and a query beside them was the same entry. Deviations are in [browser-parity-and-scope.md](browser-parity-and-scope.md#m4b-scope-coverage-and-honesty--protocol-host-recall).
- 2026-09-03: M4c implemented (td-40eb97). The contract now lives at [docs/reference/plugin-protocol.md](../../../reference/plugin-protocol.md) and `protocol.md` keeps only why it has that shape; [the authoring guide](../../../guides/active/creating-plugins.md) and its runnable example landed with it. Deviations are in [browser-parity-and-scope.md](browser-parity-and-scope.md#m4c-plugin-authoring-guide-and-protocol-reference--implemented-td-40eb97).
- 2026-09-03: M4 split into M4a–M4d after nt-addf11 and two audits (the browser's pointer and focus gaps; recall's project mapping answering empty for every documents source). [browser-parity-and-scope.md](browser-parity-and-scope.md) controls M4a–M4c; `coverage[]` and the row-set rule join the pending table. The pointer and footer rules the work adds to every surface are in `docs/reference/design-language.md`.
- 2026-09-02: opened. Decisions 1–11 settled in conversation with the maintainer; Herdr plugin-hosting plan superseded.
- 2026-09-02: decision 12 (theme awareness) and the pending-revisions table added from the M0 recall mockup.
- 2026-09-02: M1 implemented on branch `plugin-ecosystem` (td-01b62b). One deviation from the design: `tabRef.global` is the surface ID rather than an index into the global slice, because the persisted value is an ID already and carrying one identity instead of two removes a whole class of staleness.
- 2026-09-02: M2b implemented on branch `plugin-ecosystem` (td-0d3539). The view pill row became one View control on the pane title row opening a modal, following the M0 mockups; deviations are recorded in [host.md](host.md#deviations-from-the-design-recorded-in-m2b).
- 2026-09-03: M3's review found the collection pane's keyboard ownership was never reported to the host on the project surface, so a host global could be taken out of the middle of a pane's query. Both projections now report it, with twin tests; the same pass gave the `resources` binding its `Owed` debt, bounded the describe-generation watch cache, clamped a typed query to the bound its persisted record is checked against, and stopped a save per keystroke.
- 2026-09-02: M3 implemented on branch `plugin-ecosystem` (td-44fe20). The Resource leaf carries three tab shapes rather than the two the plan named: a row of a collection is its own shape, because `get` addresses it by collection and ID and there is no matcher anywhere in that journey to invent. Building the pane found two defects in what M2b had shipped — a plugin's answers were classified as the global browser's alone, and a browser had no identity on its answers — and both are fixed with tests; see [host.md](host.md#deviations-from-the-design-recorded-in-m3).
- 2026-09-02: M2 split into M2a (protocol host and CLI) and M2b (the browser and the tab); M2a implemented on branch `plugin-ecosystem` (td-a6d276). `internal/resourceprovider` was renamed to `internal/pluginhost` rather than wrapped, so there is literally one manager, one cache, and one process policy. Deviations from the design are recorded in [host.md](host.md#deviations-from-the-design-recorded-in-m2a). None of the pending protocol revisions above was implemented: the draft is implemented as written.

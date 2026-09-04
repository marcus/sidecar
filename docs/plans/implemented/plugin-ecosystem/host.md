# Host architecture: one descriptor, two classes, both surfaces

**Status:** implemented — see [README.md](README.md); this document is kept as the record of what was built and why.

This document is the Sidecar-side half: how a plugin is described, enabled, placed, rendered, refreshed, persisted, and reached from the CLI, and which existing seams each of those reuses.

## Baseline

What M1 built, and what the remaining milestones turn each of the rest into.

| Seam | Where | State |
| --- | --- | --- |
| Tab list is data: `assembly.Descriptors()` is the ordered catalog and `assembly.Plan(cfg)` filters it to the enabled project tabs | `internal/plugins/assembly/assembly.go` | Built. Protocol descriptors join the same catalog in M2 |
| The global tab row is a descriptor-driven ordered slice of surfaces, each hosted by a generic `globalPluginHost` | `internal/app/scope.go` | Built |
| Enablement is `plugins.<id>.enabled` for every plugin; `tasks_plugin` and `notes_plugin` are read-only aliases | `internal/config/config.go`, each plugin's `descriptor.go` | Built |
| `configui.Integration{ID, Name, Why, Descriptor}` is a projection of the descriptor, carrying only the install route | `internal/configui/integrations.go` | Built |
| External executables: `plugins.external[]` and `terminalResources.providers[]`, `pluginhost.Manager`, describe/resolve/list/get/act | `internal/config/pluginexternal.go`, `internal/config/terminalresources.go`, `internal/pluginhost` | Built in M2a |
| The shared browser and the protocol global tab | `internal/pluginbrowser`, `internal/app/pluginbrowser.go` | Built in M2b. The same model binds the pane, in pane mode |
| The `Resource` leaf, one kind for every external plugin's content, carrying three tab shapes | `internal/panelayout/panelayout.go:28`, `internal/resourceview/collection.go` | Built in M3 |
| The `Resource` leaf's `livepanes` binding, driven by plugin-declared `refresh` | `internal/plugins/workspace/live_panes.go`, `internal/overview/live_preview.go` | Built in M3. `internal/livepanes/livepanes.go:11-13` named its absence as the motivating defect |
| `sidecar open --plugin\|--provider`, `sidecar plugin changed`, layout spec `collection`/`query`, `terminal-links list/check` | `internal/cli/open.go`, `internal/cli/plugin.go`, `internal/uirequest/types.go`, `internal/cli/registry.go` | Built in M3. `--provider` and `terminal-links` kept as aliases |

## The descriptor

One Go type describes every plugin Sidecar can host. It is data, lives in `internal/plugin`, and is the only thing the assembly, the settings page, the global host list, the pane switcher, the palette, and `sidecar plugin list` read.

```go
// Descriptor is what Sidecar knows about a plugin before it runs.
type Descriptor struct {
    ID    string // stable; the config key, the CLI name, and the persisted tab ID
    Name  string // header label
    Icon  string
    Class Class  // Embedded | Protocol
    Scope Scope  // Project | Global
    // Placements the plugin can occupy. Tab is a navbar surface; Panes means its
    // content can open as leaves in the workspace and Sessions decks.
    Placements []Placement
    // Settings-page copy: the one-line detail under the name, the sentence the
    // install route leads with, and whether the surface carries a beta badge.
    Detail string
    Why    string
    Beta   bool
    // Enabled reads plugins.<id>.enabled and any legacy switch this plugin
    // migrated from, so a config written before this plan keeps working.
    // Nil means the plugin has no switch: Workspaces is exactly that.
    Enabled func(*config.Config) bool
    // Preference is what the user chose, ignoring dependencies on other
    // surfaces. Nil means the preference and the effective answer are the same
    // question. Notes is why it exists: it needs the td panel, and a Notes row
    // reading OFF because td is off would claim a choice nobody made.
    Preference func(*config.Config) bool
    // SetEnabled writes plugins.<id>.enabled. It never writes a legacy flag.
    SetEnabled func(*config.PluginsConfig, bool)
    // Embedded only: constructs the in-process plugin.
    New func() Plugin
    // Install and version UX (Homebrew tap, PATH probe). Zero means ships in-repo.
    Integration version.Descriptor
}
```

M2 adds `Instance *config.PluginInstanceConfig` for the protocol class: the configured entry a protocol descriptor is projected from.

**Class** decides who renders. `Embedded` plugins implement `plugin.Plugin` and own their frame (Tasks, td monitor, and every plugin in `internal/plugins`). `Protocol` plugins are executables; their descriptor is projected from a config entry after `describe`, and the host renders them through one shared browser.

**Scope** decides lifecycle. `Project` plugins live in the registry and are re-initialized on project switch. `Global` plugins are built once, survive project switches and scope toggles, and close once at shutdown, which is the behaviour `globalTasksHost` exists to guarantee (`internal/app/scope.go:571-580`). A protocol plugin's scope comes from its config entry (`"scope": "global"` default) because the host cannot infer it from data.

**Placements** decide where content shows. A `Tab` placement is a navbar entry. A `Panes` placement means its documents and collections can open in the pane decks of both workspace projections. Tasks is `Tab` only. Jira is `Panes` only. Recall is both: a global tab with a query box, and result panes beside a terminal.

There is no manifest file for embedded plugins: the descriptor is a Go literal in the plugin package. There is no manifest file for protocol plugins either: the config entry is the manifest, and `describe` fills in the rest at run time. A manifest format is what the superseded Herdr plan would have brought, and it is explicitly not adopted, because a config entry plus a `describe` answer is already every fact the host needs.

## Two classes, stated as the user contract

| Gesture | Embedded plugin (Tasks, td) | Protocol plugin (recall, DEX, ongoing, Jira) |
| --- | --- | --- |
| Appears in the navbar | When `plugins.<id>.enabled` and it declares `Tab` | When enabled, `describe` succeeded, and it declares `Tab` |
| Number key | Project tabs `1`–`7`; global tabs `8`, `9`, `0` in descriptor order, then none | Same rule, same pool |
| Keys inside it | Its own, routed through `KeyRouter` as today | Host-owned browser keys; plugin actions through the action menu, palette, or a granted letter |
| Content in a pane | Not in this plan (its TUI is one frame) | Collection tabs and document tabs in the `Resource` leaf, both surfaces |
| Theme | Injected at construction and on theme change, as today | Host renders in the host theme; nothing to inject |
| Refresh | Its own watcher or poll | `refresh.watch`, `refresh.everySeconds`, `sidecar plugin changed` |
| Project switch | `Project` scope: Reinit; `Global`: untouched | Same by scope; `project` context re-sent on the next call |
| Remote-bound surface | Refuses naming the host unless it reads through host verbs | Receives `project.hostId`; refuses or answers on its own terms |
| Unavailable | Its own not-installed or setup card | Setup card from `invalid_config` + `setupHint`, or the transport failure card |
| Settings page | One row: enable, install if missing, restart note | One row: enable, `describe` status, declared context, docs link |
| CLI | `sidecar plugin list` shows it with class `embedded` | `sidecar plugin list|check|call|add|remove|enable|disable|changed` |

A protocol plugin that Sidecar's release knows nothing about is the point of the second column: the user adds a config entry naming an executable, and every row above applies with no Sidecar change.

## Generalizing the global host

The global tab row is an ordered slice of `globalSurface` values built from descriptors at startup:

1. Sessions and Activity are app-owned surfaces, first in the order, and keep `8` and `9`.
2. Every enabled descriptor with `Scope: Global` and a `Tab` placement follows, in descriptor order. The first gets `0`; the rest are reachable through `[`/`]`, the palette command `focus-<id>`, and a click. There is no fourth number key: renumbering the three that exist would move Sessions under the user, which is the same reason the positional project keys stop at 7.
3. `tabRef.global` is the surface ID. `globalTabsVisible`, `ensureVisibleGlobalTab`, `headerEntries`, `globalMouse`, `activeGlobalSurface`, and `surfacePlugins` all read the slice by ID, so nothing can be shifted onto a different action by a tab disappearing.
4. `globalPluginHost` is one per global descriptor, with the start-once / forward-every-message / stop-once contract and the start and stop counters the tests assert.

Number keys belong to entries rather than to positions: `8`, `9`, and `0` never change meaning, and a key whose entry is not on the row does nothing at all rather than falling through to a project tab.

The persisted last-scope and last-tab values are descriptor IDs. A global tab that disappears — its plugin disabled — falls back to Sessions rather than to an index that now names something else. The two surface names state.json wrote before the header settled on the fleet vocabulary (`workspaces`, `agents`) still read back.

`internal/app` keeps its own list of global descriptors (`app.GlobalDescriptors()`) because the plugin packages import it, so it cannot import the assembly. Both lists call the same per-plugin `Descriptor()`, and an assembly test fails if they ever name different plugins.

## Unifying enablement

Every plugin has `plugins.<id>.enabled`. Migration is one-directional and silent:

- `tasks_plugin` and `notes_plugin` are deprecated aliases. Both config keys are pointers, so "absent" stays a third answer: while the key is absent the flag decides, and a save writes the key and leaves the flag untouched. The flags are removed from `allFeatures` one minor release after the settings page stopped writing them.
- `conversations_plugin` is deliberately not an alias. It is the preview opt-in, and the panel needs both it and `plugins.conversations.enabled`; turning the panel off clears only the plugin key so the opt-in is not silently revoked.
- `terminalResources.providers[]` entries load into the same list as `plugins.external[]` with `scope: global`, `placements: ["panes"]` (M4). Saving writes `plugins.external`. The old key is read for one minor release after that and then dropped.

The settings page (`page_panels.go`) is one loop over descriptors: the enable switch, the detail line, the beta badge, the missing-command note, and the install route from `configui.Integration` where the descriptor has one. Per-plugin settings that are not a switch — a refresh interval, a database path, an editor choice — are the one place the loop is not uniform. A descriptor with no `SetEnabled` has no row, because a control that cannot change anything is worse than no control.

The catalog reaches the page through `app.WithPluginDescriptors`, from the process that owns both: `internal/configui` cannot import the assembly, because the plugin packages import `internal/app`, which owns this surface. An assembly test renders the real page with the real catalog so configui's own fixture cannot drift.

`panelRestartNote` stays until enablement is live; making it live is deliberately out of scope.

## Protocol plugin configuration

```json
{
  "plugins": {
    "external": [
      {
        "id": "recall",
        "command": ["recall", "sidecar-plugin"],
        "passEnv": ["RECALL_PROFILE"],
        "enabled": true,
        "scope": "global",
        "placements": ["tab", "panes"],
        "timeout": "10s",
        "claimHosts": []
      },
      {
        "id": "jira-work",
        "command": ["sidecar-jira", "sidecar-provider", "--profile", "work"],
        "passEnv": ["JIRA_API_TOKEN"],
        "enabled": true,
        "placements": ["panes"],
        "claimHosts": ["example.atlassian.net"]
      }
    ]
  }
}
```

Same fields and bounds as the resource section plus `scope` and `placements`. The discovery policy is unchanged and restated because it is the standing decision a plugin-directory proposal has to argue against: Sidecar never scans a directory, never executes every `sidecar-*` binary on `PATH`, never auto-enables, and never lets a repository declare a plugin. `sidecar plugin add` writes one entry after showing exactly what will run; that is the whole install flow.

`Config.PluginInstances()` is the one ordered list the host reads. `plugins.external` entries lead, because order is precedence, and each `terminalResources.providers` entry follows projected onto the same type with the defaults its protocol implies: `scope: "global"`, `placements: ["panes"]`, no navbar tab. Every instance carries the section it was read from, and that section — never anything the executable says — decides which protocol identifier it is dispatched with. An ID configured in both sections is one plugin: `plugins.external` wins and the legacy entry is dropped, so a half-finished migration cannot start two child processes under one identity.

Validation:

- Bounds are the resource section's: 16 instances, a 64-character ID, a timeout clamped to [1s, 60s], 16 claimed hosts each a bare hostname, and `passEnv` names only — an entry containing `=` is refused loudly, with the value redacted out of the message, because a credential pasted into the config file needs removing rather than silently ignoring.
- `scope` defaults to `global`. `project` is refused with a message saying to remove the key and read project context per call, rather than coerced: a project-scoped plugin would be re-described on every switch and would see a different world each time, so running it as global would answer a question nobody asked. Any other value is refused naming the one that works.
- `placements` defaults to `["tab", "panes"]` for an entry a user wrote deliberately, and is exactly `["panes"]` for a projected `terminalResources` entry.
- The saver writes `plugins.external` inside the existing `plugins` block, which is re-marshalled whole on every save, so an emptied list disappears with it and unmanaged top-level sections are preserved as before.

Everything the protocol host does is behind the `plugin_protocol` feature flag, default off. It gates only `plugins.external`: `terminal_resource_providers` still governs the frozen section on its own, so turning the draft protocol off cannot take a working Jira provider down with it.

## The shared browser

One package, `internal/pluginbrowser`, renders a protocol plugin. It is host-independent in the same way `resourceview` is (`internal/resourceview/doc.go`): it knows `pluginhost` types and nothing about panes, tmux, or which surface is showing it. Every call to a plugin is a command the host supplies through `pluginbrowser.Calls`, so no process is ever started inside `Update` or `View`.

```text
╭───────────────────────────────────────────────╮ ╭──────────────────────────────╮
│ Fixture · Results                ⇅ Relevance  │ │ Fixture row rc:notes:1 exact │
│                                               │ │                              │
│ / dex                  3 results   answered   │ │ results · 2026-08-14         │
│                                               │ │                              │
│   #  TITLE            SOURCE   EXCERPT        │ │ Collection  results          │
│ ───  ───────────────  ───────  ────────  ──── │ │ Locator     rc:notes:1       │
│ ❯ 1  dex schema notes notes    …tiered…  exact│ │ ── Evidence ──────────────── │
│   2  dex context      shell    …the star…exact│ │ rendered markdown …          │
│                                               │ │ ── Timeline ──────────────── │
│ ───────────────────────────────────────────── │ │ 2w ago    Note added         │
│ ⚠  1 of 4 sources did not answer              │ │                              │
│ 3 shown · the rest load on scroll             │ │ o  open https://…            │
╰───────────────────────────────────────────────╯ ╰──────────────────────────────╯
```

The parts, in the order the rows fall:

- The pane title is the first row inside the box, never in the border, with the **View control right-aligned on it** — the sort pill Sidecar's own lists already use (`workspacelist.SortGlyph` in `styles.Button`), carrying the live sort key and, folded into the same label, the applied view. It sheds its word before it sheds its glyph, which is the app's own degradation ladder.
- One blank row under the title.
- The **query row**, shown only when the collection's `search` is not `none`. It owns its line, with the item count and the page's outcome right-aligned on it, and one blank row of padding beneath. A blank row and a rule say the same thing; the blank one is quieter.
- The **table**: column headings, a rule, then rows with a two-column cursor gutter. Row `status` gets a reserved, unlabelled right-hand column rather than a plugin-declared one. Below `narrowTable` the rows reflow, on the rule stated fully in M4e: **the columns declared before the primary — a rank, an index, the number a reader finds a row by — stay with the primary cell on line one, and everything after it folds into a dimmed line two**: the remaining short columns, then the status label, then the secondary cell, indented to sit under the primary. "Before the primary" rather than "the rank" is what makes it a host rule: the host does not know what a plugin calls its rank, and the plugin has already said which column names the row. This is `mockups/recall-studio.narrow-pane.txt`.
- A rule, the page's **notices**, and a summary line saying what is shown, what is left, and whether the host held anything back. An act's outcome message is right-aligned on that line and repeated in the host footer.
- An empty state that says what is true and offers the next action: "this collection needs a query" for `search: required`, "no matches" under `abstained`, and "no matches, and coverage was incomplete" under `degraded`. The three are never interchanged, because each is a different claim.
- **An empty detail box in a `Tab` placement shows the plugin's next collection** rather than a card of help text: the collection after the one on screen, listed without a query when its `search` is not `required`, drawn as its title, its outcome, and its first rows. A plugin whose second collection is its source ledger — recall's `sources` — therefore answers "why is this empty?" in the box beside the empty list, which is what makes `abstained` verifiable in place instead of one keystroke away. A plugin with only one collection keeps the help line, because there is nothing else to show. A `search: required` next collection is named in the box with the reason it is silent — it answers a query and there is none — rather than being listed, or reported as merely unread.

In a `Tab` placement the browser owns the whole content box and shows list and detail side by side, giving the list 61% and the detail the rest; below the detail floor the list takes the whole box. In a pane the same package renders one shape at a time — the collection's list, or one document — because a pane is usually too small for both, and because that is what makes the two of them sibling TABS of one leaf rather than two halves of one view.

`v` opens the **View modal**, built exactly as `internal/overview/view_flyout.go` builds its own: a `Current sort:` line, a spacer, the declared sort keys with the live one selected, the declared views with the live one selected, a spacer, and a `Done` button. It replaces the view pill row the earlier draft called for — pills spend a row on every collection whether or not anyone is choosing, and folding the choice into one control on the title row is what the list-row grammar in the design language has space for.

An action opens one of three steps, decided by its own declaration: a **form** when it declares inputs (one control per input, typed: text, multiline, choice, confirm), a **confirm** when it mutates and declares none, and nothing at all otherwise. The form is the confirm step for an action that has one — a user who filled it in has already said yes — and a required input left empty stops in the modal rather than sending the plugin a value it said it needs and did not get.

Keys are host-owned and identical for every plugin: `j`/`k` move, `Enter` opens (a document tab in the same leaf, or the detail box in a tab placement) and a second `Enter` on the same row focuses it, `/` edits the query, `v` opens the View modal, `r` refreshes, `a` opens the action menu, `o` opens `sourceUrl` through the host's confirmed path, `Esc` closes an overlay. `n` and `Tab` are deliberately not the browser's: the pane switcher and the focus ring belong to the app, and a surface that swallowed either would be answering a question the ladder already answered. A plugin-suggested action `key` is granted only if none of the browser's own keys, none of `keymap.HostReservedKeys`, none of `keymap.GlobalKeys`, and no other action already uses it; a grant is rebuilt on every describe and never persisted.

Search as you type is a 250 ms debounce (`pluginbrowser.QueryDebounce`) plus cancellation: every keystroke schedules a tick, only the newest one spends a process, and the manager supersedes the in-flight `list` for that pane key, which kills its process group. A page whose generation is not the live one is discarded rather than painted over the newer one.

`PaneFocusProvider`, `ContentLinkProvider`, `WheelBoundaryConsumer`, `FooterStatusProvider`, `DiagnosticProvider`, `TextInputConsumer` and `KeyRouter` are implemented once, by `pluginbrowser.TabPlugin`, so every protocol plugin gets the app focus ring, content links over its detail box, correct wheel behaviour, a footer status when `describe` fails, diagnostics, and the key ladder without doing anything. The browser never renders a footer of its own: it publishes `Commands()` in its own per-instance context, and an open overlay reports a second context so the bar describes what actually has the keyboard.

## The protocol tab

A configured instance that declares a `tab` placement becomes a global tab through M1's `globalPluginHost`, with no new host type:

1. `pluginbrowser.TabDescriptors(cfg, calls)` projects `plugin.ProtocolDescriptors(cfg)` down to the tab placements and attaches `New`. It is a pure function of configuration — no process, no `LookPath`, no disk — so it is safe on the first-frame path.
2. `app.globalProtocolDescriptors(cfg)` filters that to enabled, global-scope entries behind the `plugin_protocol` flag, and `newGlobalPluginHosts` receives it appended to `GlobalDescriptors()`. Sessions and Activity keep `8` and `9`; the first plugin-provided global tab keeps `0`, whether it is Tasks or a protocol plugin.
3. The header label comes from the plugin's own `describe` for the protocol class and from the descriptor for the embedded one, because a protocol descriptor names its plugin by the configured instance ID until there is something better to say.
4. `app.pluginBrowserCalls` is the seam: `Describe` reads the manager's cached status, and `List`, `Get` and `Act` each run inside a `tea.Cmd` over the same `pluginhost.Manager` the resource protocol uses — one cache, one dedupe table, one concurrency budget, one process policy.
5. A describe pass ends in `publishResourceProviders`, which hands every browser a `pluginbrowser.DescribedMsg` in the same breath as it republishes matchers and the resolver. That is also where the project context every protocol plugin is asked about is republished, because a global plugin outlives every project switch and the context it was constructed with is the wrong answer the moment the user moves. On a remote-bound surface it carries that host's ID and that host's paths; Sidecar never substitutes a local path.

## Panes: reuse the Resource leaf, add a tab shape

Adding a leaf kind touches 26 non-test files for the most recent kind (Note) and forces a persistence schema change, because every kind has its own `*TabJSON` array (`internal/state/state.go:169-251`). The Resource leaf already exists for exactly this purpose: "the single leaf kind every external terminal resource provider shares" (`panelayout.go:28-32`). So:

1. `PaneResourceTabJSON` carries `collection`, `query`, `view`, `sort` and `cursorId` beside `matcher` and `locator`, exactly one shape per record. Decode refuses a record that is more than one shape or none: which one a half-written record meant is not knowable, and guessing restores a tab pointing at something nobody opened.
2. `resource.Reference` grows `Shape()`, which names three alternatives and is the one place any of them is decided. `ShapeMatched` is `{instance, matcher, locator}` — the frozen protocol's only shape, unchanged. `ShapeCollection` is `{instance, collection}` plus the user-owned view position. `ShapeItem` is `{instance, collection, locator}`: one row, which `get` addresses by collection and ID. `Valid()` accepts exactly one. It stays the only plugin-shaped value that reaches persisted state.
3. `contentpanes` keeps one `Resource` viewer, and the viewer keeps one model: `resourceview.Model` renders a matched reference as today's resource card and delegates every question about a plugin reference — render, size, scroll, keys, refresh, persistence — to `internal/pluginbrowser` in pane mode. `contentpanes.Source` gains `ListCollection` and `GetCollectionItem` beside `ResolveResource`, with the remote twin routing through the host verb family the same way `ResolveResource` does: `sidecar content read --kind resource --operation collection|item` runs the plugin on the machine that owns the pane.
4. Both `content.go` files map the Resource kind to the same viewer as before; both `pane_host.go` files add nothing. Neither file changed, and `TestResourceLeafAcceptsEveryTabShape` plus one test per surface assert the viewer answers every shape on both.
5. The livepanes binding is one `Binding` literal per surface with `Kind: "resources"`. `Targets` reads the visible tabs' plugins' declared `watch` paths from the cached describe snapshot and never resolves anything on the update goroutine; `Prepare` expands and validates those paths once per describe generation, keyed by the plugin's own describe generation, and prunes every entry whose tab is no longer on screen, so the cache is bounded by what is visible rather than by process uptime. The generation itself moves only when a describe snapshot actually says something different — `Refresh()` runs on every tab focus and every resolve, and a generation that counted reads would mean one stat per declared path per focus. `Refresh` re-lists visible collections and re-fetches visible documents; the project surface vetoes a refresh while a modal covers the pane tree and records the debt in `Owed`, so the change lands as soon as the veto lifts. The Sessions surface declares no `Owed` because it has no veto to owe against. `everySeconds` polling is a ticker inside the same binding, armed only while a tab from that plugin is on screen, and a tick is discarded both by sequence and by project epoch. `livepanes.Set.Kinds()` lists `resources` on both surfaces and a test asserts it.
6. Keyboard ownership is reported to the host by both surfaces. A pane-mode browser answers three questions — is a query line taking text, does an overlay own the keyboard, would this key be acted on — through `resourceview.Tabs.ConsumesTextInput`, `BlocksGlobalKeys` and `ClaimsKey`. On the project surface `workspace.Plugin` implements `plugin.TextInputConsumer` and `plugin.KeyRouter` over those, so the host holds back the tab digits, `` ` ``, `~`, `[`, `]`, `K`, `W`, `#`, `@`, `!` and `^` while a query is being typed or an overlay is open. On the Sessions surface — which is not a plugin and cannot implement those interfaces — `overview.WorkspacesConsumesTextInput` and `WorkspacesBlocksGlobalKeys` answer the same two questions and the app asks them in `textInputFocused` and `pluginBlocksGlobalKeys`. `workspace.Plugin.QuitKeyExits` states the rule the host's own root-context list already applied to this plugin: `q` quits from the list and the preview, and is the pane's everywhere else. Twin tests on both surfaces assert the predicates so the projections cannot drift.

   Known limit: a tab whose plugin the manager has no status for at all — a configured instance that was later disabled, say — sits on the loading card rather than a setup card, because `Describe` answers `ok=false` for "nothing yet" and for "nothing ever" alike and the browser cannot tell them apart. The tab itself is preserved and persisted correctly; only the card is wrong. Fixing it means giving the browser a signal that a describe pass has ended, which belongs with M4's settings work.
7. Enter on a collection row opens a row tab in the same leaf, re-keyed to the resource's `identity` when the document lands, as resolve already does; a second Enter on the same row focuses that tab and spends no process. It travels as `resourceview.OpenRowMsg` rather than as a direct call: a model has no leaf, no placement and no deck, so whichever surface is showing the pane runs its own open journey with it, which is what makes the two surfaces agree by construction.

Floors, dividers, chrome, drag, and the compositor are untouched: `paneframe` does not know a tab shape exists.

## CLI

`sidecar plugin` is the owned surface. Hosting plugins is a capability Sidecar owns, so every operation has a non-interactive path from the first milestone.

| Verb | Does | Talks to | State |
| --- | --- | --- | --- |
| `sidecar plugin list [--describe] [--json]` | Every descriptor: class, scope, placements, enabled, and for an external one the config section and protocol identifier. `--describe` opts in to running `describe` | config, optionally subprocess | Built |
| `sidecar plugin check ID [--list COLLECTION [--query Q]] [--get COLLECTION ID] [--json]` | `describe` plus an explicit call, for authors | subprocess | Built |
| `sidecar plugin call ID METHOD [--params JSON] [--json]` | One raw method call with the host's envelope and validation, printing what the host would have kept | subprocess | Built |
| `sidecar plugin add ID --command ARGV... [--pass-env V]... [--scope] [--placement]... [--timeout] [--claim-host] [--disabled] [--yes]` | Appends a config entry after printing exactly what will run; `--yes` skips the confirm | config | Built |
| `sidecar plugin remove\|enable\|disable ID` | Config edits through the saver, never dropping unknown sections | config | Built |
| `sidecar plugin changed ID [--collection C]` | One `uirequest` on the file bus; a running Sidecar re-lists that plugin's visible tabs. Starts nothing and reads no configuration | uirequest bus | Built |
| `sidecar open --plugin ID [--collection C] [--query Q] [LOCATOR_OR_ROW] [--split\|--at]` | Opens a collection tab, a row's document tab, or a matched locator on the viewer's screen; `--provider` stays as an alias for the locator form | uirequest bus, same declines as today | Built |
| `sidecar layout apply --spec` | `{"kind":"resource","provider":"recall","collection":"results","query":"dex"}` beside the existing `targets` form; `layout get` reports the active tab's collection and query | uirequest bus | Built |
| `sidecar content read --kind resource --operation collection\|item` | The host half of a remote Resource pane: the host runs its own plugin and returns the page or document IT kept | subprocess on the host | Built |
| `sidecar terminal-links …` | The surface for `terminalResources.providers`, unchanged | config, subprocess | Built (kept) |

`recall query dex` from the request becomes `sidecar open --plugin recall --collection results --query dex`, and `ongoing show recall` becomes `sidecar open --plugin ongoing --collection projects recall`. Both are one line an agent can run from any pane with no keypress.

Exit codes are uniform across the family: `0` success, `1` a call or a write failed, `2` usage, `3` no plugin with that id is configured, `4` refused — the `plugin_protocol` flag is off, or the entry belongs to a section the verb does not own.

Two rules the verbs hold rather than merely document:

- **`list` starts nothing** without `--describe`, and **`add` starts nothing at all.** `add` validates the entry, then prints the argv one element per line — a joined line hides where an argument containing a space begins — plus the working directory, the protocol, and the variables passed by name, and then asks. Without a readable stdin it refuses and says to pass `--yes`.
- **Only what the host kept is printed**, never the plugin's raw stdout: everything on screen has been through the same sanitization and bounds a pane would apply, which is what makes `call` an authoring loop rather than a pretty-printer.

`remove`, `enable`, and `disable` refuse a `terminalResources` entry rather than editing it. That section belongs to the frozen protocol and `terminal-links` is its surface; two commands owning one section is how they start disagreeing.

The `Agent` doc on each verb and `sidecar --agents` cover them; `docs/reference/cli.md` carries the generated family.

## The manager

One `pluginhost.Manager` answers both dialects, with one cache, one dedupe table, and one global and per-instance concurrency budget. A plugin's `list` is a child process exactly as a provider's `resolve` is, so one budget has to cover both or the budget covers nothing.

What the plugin protocol added to it:

- `Description` grew `Context`, `Collections`, and `Actions`. A resource provider leaves all three empty, which is what "a plugin that declares no collections and no actions is exactly a resource provider" means inside the host.
- `List`, `Get`, and `Act`. `List` refuses a collection the newest successful `describe` did not declare, before spawning anything: the declaration is what says which columns exist, and a page sanitized against no declaration would carry cells the host has nowhere to paint.
- `Get` shares the resolve cache and dedupe under a `get`-prefixed key, so a second Enter on the same row costs no process. `Act` shares neither — two identical acts are two intentions, and collapsing them would drop a change the user asked for twice on purpose.
- A `ListRequest` carries a `PaneKey`. A second list for the same pane cancels the first, which kills its process group; `CancelPane` does the same when a pane closes. That is what makes search-as-you-type affordable before there is any resident transport.
- Context is filtered at the process boundary, in `CommandProvider`, against what the plugin declared in its last successful describe — so "an undeclared kind is never sent" is a property of the host rather than a promise each caller keeps, and a plugin that has never described successfully receives nothing.

## Startup

The startup posture is unchanged: no subprocess, no `LookPath`, no config read of either plugin section before the first ready frame, enforced by the same explicit latch the resource providers use (`internal/app/resourceproviders.go`). The describe pass builds from `Config.PluginInstances()` filtered by each section's own feature flag, inside the command, after the latch opens. Building a `globalPluginHost` constructs the plugin value and does no I/O; its model is built by the command `start` returns, after the first frame. `assembly.Plan`, `assembly.Descriptors`, and `plugin.ProtocolDescriptors` read only config and construct nothing. A global protocol tab will render a loading state until its describe snapshot lands.

## Deviations from the design, recorded in M2a

1. **`internal/resourceprovider` was renamed, not wrapped.** The plan allowed either. Renaming is what makes "one manager, one cache, one process policy" literally true rather than a convention two packages agree to keep.
2. **The descriptor carries `*config.PluginInstance`, not `*config.PluginInstanceConfig`.** The wider type also knows which config section the entry came from, which is what lets `plugin list` report it — the mitigation the plan's risk table asks for.
3. **Describe validation is all-or-nothing for the whole result**, not just for matchers. A plugin that declares a 13-column collection, an action naming a collection it never declared, or a watch path outside the home directory is refused entirely. Publishing the rest would hide a bug while changing what the scanner recognises and what the host watches on disk.
4. **An unrecognised `page.outcome` coerces to `degraded`.** The protocol names three values; a fourth from a later version is a claim this host cannot read, and of the two ways to be wrong, "coverage may be incomplete" is the one that does not invent a guarantee on the plugin's behalf.
5. **The identity block is accepted under either spelling.** `plugin` wins, `provider` is honoured — an author who copied a resource provider's response is describing the same thing under the older name.
6. **The home directory itself is not a valid `watch` path**, only somewhere under it. Watching a whole home directory is watching a whole disk with extra steps.
7. **A cell keyed by an undeclared column is dropped.** It has no width, no header, and no place in the row; keeping it would be keeping a string nothing can paint.
8. **M2 was split** into M2a (this) and M2b (the browser and the tab), because the host half is independently useful and independently provable.

None of the [protocol revisions the M0 mockup surfaced](README.md#protocol-revisions-from-the-m0-recall-mockup--all-settled) was implemented in M2a. The draft is implemented as written; the revisions landed in M4b and M4e-a.

## Deviations from the design, recorded in M2b

1. **The view pill row is a View modal.** The draft called for a pill row when `views` is non-empty and a separate sort picker on `s`. The mockups settled on one control instead: a sort pill on the title row that opens a modal carrying both, so a collection with four views does not spend a row saying so on every frame. `s` binds nothing; `v` opens the modal.
2. **`n` and `Tab` are not the browser's keys.** The design listed `n` among the browser's own. It is the app's, self-gated by `paneSwitcherCommands`, and claiming it would have put a second answer behind a key that already has one. `Tab` is the app-owned focus ring, which the browser joins through `PaneFocusProvider` rather than by binding the key.
3. **An empty detail box shows what the next gesture does, not the plugin's next collection.** The host rule in the pending-revisions table asks for the second collection there. It is not implemented, for the same reason nothing else in that table is: none of those revisions is confirmed, and M2a implemented the draft as written.
4. **`internal/pluginbrowser` has its own detail renderer** rather than reusing `resourceview.Model`. The card the mockups specify is a different layout from the resource card — title with a right-aligned status, subtitle, field grid, then sections under titled rules — and `resourceview` renders no sections. M3 met the two the other way round: a plugin-shaped `resourceview.Model` delegates its whole render to the browser, so a plugin document looks the same in a pane as it does in a tab and the resource card stays the frozen protocol's.
5. **The browser owns one truncation function.** `ui.TruncateString` measures runes rather than display cells behind escape sequences, so a styled table row handed to it came back cut mid-data while the frame stayed exactly the right width. Everything the browser paints goes through `fitStyled`, which is `ansi`-aware and pads as well as truncates, so "every line is exactly the box's width" is a property of one function instead of a promise each renderer keeps.
6. **The header label for a protocol tab comes from the plugin.** `globalPluginHost.label()` asks the hosted plugin first for the protocol class only. An embedded plugin keeps the descriptor's label, because that is a Go literal beside the plugin and the two cannot disagree.

## What does not change

- `paneframe`, `panelayout` kinds and floors, both `pane_host.go` files.
- The embedded plugins' own UIs, key tables, themes, and watchers.
- The trust posture: process boundary is crash isolation, not a sandbox; explicit config is the install step; no discovery.
- `contentlink`/`terminallink` scanning: matchers are the only thing a plugin contributes to it.
- Remote hosts: a protocol plugin runs on the viewer's machine and gets `project.hostId`; running plugins on the host side is a separate plan.

## Deviations from the design, recorded in M3

1. **The Resource leaf carries three tab shapes, not two.** The plan named a document tab and a collection tab. A row of a collection is a third: `get` addresses it by `{collection, id}`, and there is no matcher anywhere in that journey to invent one from. Folding it into the matched shape would have meant persisting a matcher no plugin declared and no scan produced.
2. **The dispatch lives on `resourceview.Model`, not in `contentpanes`.** The plan put it in `contentpanes` ("one `Resource` viewer; it dispatches on the reference shape"). Both workspace projections hold `*resourceview.Model` values directly — through `resourceview.Tabs`, the tab strips, the header renderers and the persistence projections — so a viewer that returned a different model type for a collection would have been dropped by both projections' type assertions. Putting the dispatch one layer down makes "both surfaces inherit the shapes" literally true: neither `pane_host.go`, neither `content.go`, and neither tab strip changed.
3. **Enter on a row is a message, not a call.** `resourceview.OpenRowMsg` reaches the surface, which runs its own open journey. A model has no leaf and no placement, and the surfaces' own journeys already focus an open tab rather than fetching it twice — which is exactly what the second Enter has to do.
4. **`contentlink.Ref` grew `Collection` and `Query`.** The plan did not say where the collection would ride between the request bus, the deck, and persistence. It rides on the ref, beside `Provider` and `Matcher`, because that is the value `Deck.Open` takes and the value `normalizeRef` keys a tab by. `View`, `Sort` and `CursorID` are view position and ride on `TabState`, beside the Diff leaf's `Scope` and `Mode`.
5. **A collection tab's identity is `{instance, collection}` and excludes the query.** Retyping a query re-lists the tab that is open rather than forking a second one, which is what a query line means everywhere else. A row tab's identity includes the row, so a collection and one of its rows are two tabs.
6. **A remote-bound pane is not yet bound to the host verbs.** `Source.ListCollection`, `Source.GetCollectionItem` and `sidecar content read --operation collection|item` are implemented and are the twin the host verb family needs. A remote-bound collection PANE does not use them yet, because a browser also needs the host's describe — collections, columns, actions, refresh — and `content describe` carries only matchers today. Extending it is M4's, alongside the rest of the remote story. Locally, collection tabs bind the app's own manager, exactly as the resource card binds its resolver.
7. **`pluginbrowser.QueryDebouncedMsg` is exported.** A pane-mode browser's debounce ticks travel the host's message bus like every other background answer, and a host cannot route a type it cannot name.
8. **A plugin's answers are SHARED background work, not the global browser's alone.** `overview.IsAsyncMessage` decides what reaches the Sessions surface while a modal owns focus, and the app returns as soon as that surface has been offered such a message — unless it is also shared. A page classified as async-only was claimed by the global browser and never reached the project workspace, which left a project collection pane on "refreshing…" with the page already fetched. `overview.IsSharedPluginMessage` is the diff and picker messages' precedent applied to plugin answers, and a test pins both halves.
9. **Every browser carries an identity.** A list answer named only the instance, the collection and a generation, and two browsers of one plugin — the global tab and a pane on the same collection — routinely sit on the same generation, so the tab's page landed in the pane and replaced its query's results with the tab's. `ListCall`/`GetCall`/`ActCall` and their answers now carry a per-browser id, checked on arrival, and the cancellation `PaneKey` includes it too so a pane re-querying cannot kill the tab's in-flight process group.
10. **A focus carries the view position the request named.** A collection tab's identity is `{instance, collection}`, so a second `open --plugin … --query …` focuses rather than creates. Without `resourceview.Model.FocusRef` that focus would drop the query, and the same command would mean two different things depending on whether the tab happened to be open. An `open` naming no query focuses the tab as it is rather than clearing what the user typed.
11. **The workspace plugin became a `plugin.KeyRouter`.** Reporting a collection pane's keyboard ownership needed `ConsumesTextInput` and `BlocksGlobalKeys`, which the plugin already implemented for its own text modes; claiming the browser's own keys at precedence level 3 needed the whole interface, and that interface also owns `QuitKeyExits`. It is implemented to give exactly the answer the host's `isRootContext` list already gave for this plugin's contexts — `q` quits from `workspace-list` and `workspace-preview` and nowhere else — so stating the rule in the plugin changed nothing about it, and a test pins that.
12. **The Sessions surface answers the same two questions without being a plugin.** `overview.WorkspacesConsumesTextInput` and `WorkspacesBlocksGlobalKeys` are the twin of the plugin capabilities, asked by the app in `textInputFocused` and `pluginBlocksGlobalKeys`. Without them the Sessions footer would go on advertising the tab digits at a user typing into a collection pane's query, which is the same class of bug the project surface had.

## Deviations from the design, recorded in M4b

The host rules M4b settled, recorded here because they are decisions about this surface rather than about the protocol.

1. **The coverage modal's key is `c`, and Retry inside it is `r`.** M4a picked `c` from the free set for the card it built; M4b keeps it for the modal the card became, and adds `r` while the modal owns the keyboard — which is what `r` means everywhere else on this surface. Both are claimed only where there is something to explain: a page that answered with no notices, no `coverage[]` and nothing omitted has nothing the modal could add, and a `search: required` collection nobody has typed into has made no claim at all. The modal also registers Retry and Done as buttons, so the pointer reaches everything the keys do.
2. **The View pill's rule, stated in full.** `⇅ <sort>` always; `· <view title>` when a view is applied; `· <scope title>` whenever the collection declares filters, where the scope is the FIRST declared filter and its title is shown whether or not it has been changed — a default is still a scope, and a page gathered under one nobody can see is a page whose emptiness means nothing; `· N filters` when any filter other than the scope is applied. The ladder sheds right to left: the count, then the scope title, then the view, then the sort word, keeping the glyph. A collection that declares no filters keeps exactly the label it had.
3. **A text filter's scope value is its own text, or its label when empty.** The plan named "its current choice title", which a text filter does not have. An empty text scope showing nothing would leave the pill reading `⇅ rank · `, so the label stands in until there is a value.
4. **A choice is applied as it is picked; text is applied on Done.** A radio has no other moment to commit at, and waiting for Done would make the modal feel inert. Text cannot commit per keystroke without a list per keystroke, so it commits on Done — and on a radio pick, because both are one statement about what this page should cover. Esc discards uncommitted text: an edit nobody confirmed is not a choice.
5. **The coverage modal is wider than the browser's other modals and grows with the frame**, from 72 columns up to 100. Four columns squeezed into the 46 the View modal uses is four ellipses, and a plugin's own reason clipped to nothing is the one thing this modal exists to show. It is still held to its box: the table scrolls with the modal body rather than the modal growing past the frame.
6. **The modal opens with no explicit focus.** Setting one scrolls the body to it, and this modal opens on the outcome — the sentence the reader came for. Focus still lands on Retry, because it is the first focusable, and `r` works wherever the body is scrolled to.
7. **A global tab now remembers its query, view, sort and filters**, in `state.PluginBrowserView` keyed by `{instance, collection}`. The README's open question defaulted to yes and this is where it landed: the applied filters had to be persisted somewhere for a global tab, which has no pane record, and a query beside them is the same entry. A pane-mode browser writes nothing there — its position rides on the tab record its surface saves, and two authorities on one position is how they start disagreeing.
8. **An applied filter set is a sorted slice on `resource.Reference` and a canonical string on `contentlink.Ref`.** A map on either would have made both types uncomparable, and `contentlink.Ref` is compared as a value all along the open journey. `Reference`, `uirequest.Target`, `state.PaneResourceTabJSON` and `resourceview.PersistedTab` each gained an `Equal` method instead, so every field still takes part in the comparison that used to be `==`.
9. **A restored filter the newest describe cannot express is dropped rather than kept.** The host would drop the key at call time anyway; dropping it on restore is what stops the View modal opening on a value nothing can select.

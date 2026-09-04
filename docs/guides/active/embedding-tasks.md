# Hosting the embedded Tasks tab

You are changing the **host**. The Tasks tab is not a sidecar plugin that happens
to show tasks — it is the whole Tasks TUI, a foreign application with 355 key
bindings across 14 focus contexts, embedded through
`github.com/marcus/tasks/pkg/tui` at a pinned module version. Sidecar owns
placement, lifecycle, key routing, the footer, and the palette. Tasks owns
everything else.

The mirror of this document, written for someone changing Tasks, is
`docs/guides/embedding-in-sidecar.md` in the tasks repo. The decision record is
[`docs/adr/0001-contextual-plugin-keys-take-precedence.md`](../../adr/0001-contextual-plugin-keys-take-precedence.md);
the cross-repo plan of record is `docs/plans/active/tasks-in-sidecar.md` in the
tasks repo.

## Your obligations, up front

1. **Never read or write `tasks.jsonl`.** Not a path in config, not a
   convenience read, not a "just for search". See [The boundary](#the-boundary).
2. **A new global key can silently take a key away from Tasks.** Check before you
   bind. See [Adding a global key](#adding-a-global-key-host-side).
3. **Only register a binding you will actually honour.** The footer and merged
   help are built from registered bindings; a registered key the host keeps is a
   lie on the most visible line in the app.
4. **`GOWORK=off` or it did not happen.** `go.work` masks an unpublished tasks
   dependency. See [Release timing](#release-and-version-timing).
5. **Drive the real app on an isolated tmux socket.** Every problem this
   integration actually had was invisible to unit tests.

## The shape of the integration

```text
Sidecar app
  ├─ tab/header/footer/help and project lifecycle
  └─ Tasks plugin (internal/plugins/tasks)
       └─ github.com/marcus/tasks/pkg/tui
            ├─ Tasks shortcut registry and interaction model
            ├─ Tasks application facade
            └─ Tasks store + journal ──▶ configured tasks data
```

The plugin is gated by `plugins.tasks.enabled` (off until it is asked for),
with the `tasks_plugin` feature flag as a read-only alias while that key is
absent — the rule lives in `internal/panelpref`. It is placed by
`internal/plugins/assembly/assembly.go` after Workspaces or after Notes per
`plugins.tasks.position`. Tab numbers are derived from that ordered plan —
nothing may assume Tasks is tab 6.

### The boundary

- **Sidecar never reads or writes `tasks.jsonl`.** Sidecar's config accepts no
  Tasks data path. `tasksui.NewEmbedded` runs normal Tasks configuration
  resolution and refuses an unconfigured store, reporting its own message which
  `internal/plugins/tasks/plugin.go` surfaces verbatim. Every mutation goes
  through the Tasks application, so its validation, canonical JSONL shape,
  journal, and undo still apply.
- **Tasks never imports sidecar.** `pkg/tui` carries Tasks-owned types and Bubble
  Tea v2 types only. We exchange `tea.Msg` values and strings.

What it buys: one shortcut registry, so the palette, merged help, and footer are
projections rather than a second table that drifts; one store writer; and a
Tasks release another Go host could adopt. `TestInitDoesNoIO` and
`TestModelIsBuiltOnlyByTheStartCommand` in
`internal/plugins/tasks/plugin_test.go` pin the no-I/O-before-the-first-frame
claim.

The cautionary tale is ours. `plugins.td-monitor.dbPath` is a sidecar config key
naming a path into td's storage — filed as `td-a3867c`, with `td-9fb430`
recording that the default now points at nothing since td moved off a single
SQLite DB, so the setting silently does nothing. td-monitor predates the rule.
Do not repeat it for Tasks.

### Lifecycle contract

- `Init` does no I/O; it clears state and bumps `generation`.
- `Start` builds the model in a `tea.Cmd`, returning an epoch- and
  generation-tagged `TasksReadyMsg`.
- Stale ready messages are **`Discard`ed, not `Close`d**. Every model built for
  one namespace shares one session file, so closing a model that was never
  presented would write its untouched default state over the live session.
- `Stop` calls `Close` — that model *was* presented, so its view state is what
  the next lifecycle should reload.
- `suppressQuit` wraps every returned command, including inside `tea.Batch` and
  `tea.Sequence`, so no `tea.Quit` can reach the runtime. Tasks also latches its
  own quit now; keep the guard anyway.
- The session namespace is `sidecar`
  (`$XDG_STATE_HOME/tasks/hosts/sidecar/tui.json`). It must never become the
  standalone `tasks-tui` session.

## The public API you depend on

Everything in `pkg/tui` is a compatibility surface: `NewEmbedded`,
`EmbeddedOptions` (`SessionNamespace`, `InitialView`, `InitialContexts`,
`SuppressFooter`, `SuppressKeyHints`, `SuppressViewKeyHints`, `SuppressQuit`,
`Theme`, `Environment`), `Model`'s methods (`Init`, `Update`, `View(w,h)`,
`Close`, `Discard`, `Invoke`, `CommandAvailable`, `FocusContext`,
`ConsumesTextInput`, `CurrentView`, `Contexts`, `QuitRequested`,
`ClearQuitRequest`, `LoadError`, `Warnings`), the three exports
(`ExportBindings`, `ExportCommands`, `ExportContexts`) and the types
`Binding` / `Command` / `ContextMetadata` / `FocusContext` / `View`.

Suppression settings we pass, and why (`buildModel`):

| Setting | Value | Reason |
| --- | --- | --- |
| `SuppressFooter` | `false` | Blunt switch. It also removes the prompt input (so `tab` focuses an invisible caret), the agent transcript, the store-read banner, and the filter lines |
| `SuppressKeyHints` | `true` | Removes exactly Tasks' key-hint row, because we render the unified one |
| `SuppressViewKeyHints` | **not yet set** (pending a published tag) | Removes the `1`..`6` prefixes from Tasks' view bar, because we took the number row. **Advertisement only — `1`-`6` still jump views inside Tasks** |
| `SuppressQuit` | `true` | Tasks latches a request rather than returning `tea.Quit` |
| `Theme.Colors` | overlay | We name ~22 slots; every slot we do not name keeps the user's own Tasks colour. `ReplaceColors` stays unset |

### Tasks-side changes that break us while compiling fine

A rename in `pkg/tui` compiles here as long as we do not use the old constant —
but routing changes underneath. This is why the routing table in
`internal/plugins/tasks/routing.go` is **derived at runtime** from
`ExportContexts` / `ExportBindings` rather than hardcoded, and why the derivation
is guarded:

| Tasks change | What breaks here |
| --- | --- |
| Rename a `FocusContext` | `rootContexts` no longer intersects it, so it is demoted to "overlay": globals stop firing and `q` stops reaching the quit flow there |
| Change a command ID | `hostOwnedCommands` stops filtering it (a host-owned command leaks into the palette), or `Model.Invoke` fails and the palette entry does nothing |
| Change a default binding | A key we would have advertised moves onto a global we keep, or vice versa; the footer and merged help go stale |
| Add a context | Unknown → treated as a blocking overlay (safe, but globals do not fire and `q` does not quit there) |
| Change `ConsumesTextInput` | Precedence level 2 flips: either we steal typed characters out of a Tasks editor, or a browsing context swallows every global |

**`TestRoutingTableIsDerivedFromTheTasksRegistry`**
(`internal/plugins/tasks/routing_test.go`) is the guard: it asserts every
exported context is recognised, that text-input classification matches Tasks'
own metadata, that no text-input context is root, and that the root set is
exactly `tasks-list`, `tasks-detail`, `tasks-response`, `tasks-response-detail`.
A Tasks-side rename fails that test rather than silently changing routing. Run it
immediately after every re-pin.

## Shortcuts and the key contract

### The precedence ladder

The first level that handles a key wins:

1. an open sidecar application modal;
2. the active plugin's text-input or blocking-overlay context
   (`ConsumesTextInput`, `GlobalKeyBlocker.BlocksGlobalKeys`);
3. an active plugin **contextual** binding (`KeyRouter.ClaimsKey`);
4. sidecar global bindings;
5. unbound input forwarded to the plugin.

Level 3 above level 4 is the substance of ADR-0001. Only the Tasks plugin
implements the full `KeyRouter`; other plugins may implement the narrower
`GlobalKeyBlocker` so their own overlays retain keyboard focus, but their
`pluginClaimsKey()` remains false. This is pinned by
`TestPluginsWithoutAKeyRouterAreUnaffected` and
`TestBracketsUnderAPluginOverlayWithoutKeyRouterReachThePlugin`. A user override
(`keymap.Registry.UserOverride`) is consulted **before** a plugin claim.

`internal/app/key_precedence_test.go` (`TestKeyPrecedence`) is the table-driven
proof of all five levels and every conflict-table row.

### Host-reserved keys

`ctrl+c`, `q`, and `?` are never offered to `ClaimsKey`, whatever a router says.
The single definition is `keymap.HostReservedKeys` in
`internal/keymap/hostkeys.go`; the Tasks plugin aliases the same variable as
defence in depth. This started as plugin-side courtesy and moved into the host
after review showed a router claiming `q` or `?` would swallow the quit flow and
the merged help entirely. `TestTheHostRefusesItsReservedKeysWhateverARouterClaims`
and `TestEveryReservedKeyIsAKeyTheHostHandles` pin it.

(Levels 2 forwards everything except `ctrl+c`, so `q` inside a Tasks modal
correctly cancels the modal.)

### The conflict table

| Key | Who wins inside the Tasks tab | Why |
| --- | --- | --- |
| `@` | **Tasks** (`open-context-palette`) | The only entry in `shadowableGlobals`. Our project switcher stays in `?`/palette |
| `1`-`6` | **Sidecar** tab switching | Revised after live use; see below |
| `[` / `]` | **Sidecar** tab cycling from Tasks **root** contexts; **Tasks** in text-input/overlay contexts | Global outside typing and overlays |
| `←` / `→` | **Tasks** (`prev-view` / `next-view`) | We do not bind them; how Tasks views are stepped now |
| `tab` | **Tasks** (`focus-prompt`) | No root sidecar action |
| `M` / `A` | **Tasks** (`toggle-model`, `open-agent-activity`) | Not sidecar globals |
| `K`, `W`, `#` | **Sidecar** | Accidental collisions, never in the conflict table |
| `ctrl+c`, `q`, `?` | **Sidecar**, always | Host-reserved |

**The number row.** It originally went to Tasks views; that shipped and lost.
Switching tabs by number is muscle memory across every other tab, and a key that
means one thing in six tabs and another in the seventh is a key you have to think
about. We changed it in `shadowableGlobals` (now `@` alone) rather than through a
user keymap override: the override is the right tool for one user's preference,
this is the default every user gets. Tasks' registry was not forked either —
Tasks still binds `1`-`6`, we simply decline the shadow. Tasks will also stop
advertising them in its own view bar once we set `SuppressViewKeyHints` — that
option exists in the tasks repo but is **not yet adopted here**, because it
landed after the newest published tag and our `go.mod` cannot pin what is not
pushed. Until then Tasks' view bar still reads `1 Agenda 2 Next …` inside the
tab, which is the one surface still naming keys we took.
`TestNumberKeysSwitchSidecarTabsFromTheTasksTab` and `TestArrowKeysReachTheTasksTab`
pin the exchange.

**Brackets.** `[`/`]` are global Sidecar tab navigation in every ordinary plugin
context. Local navigation moved to `{`/`}` for tab cycling inside a pane — File
Browser, document, issue and Diff target tabs alike — and to `,`/`.` for
stepping between files inside a diff. A literal bracket typed into a
Tasks prompt, filter, or form still reaches Tasks because level 2 forwards it
before the host's switch is reached. Pinned by
`TestBracketsCycleSidecarTabsAcrossPluginContexts`,
`TestBracketsTypedIntoATasksTextInputReachTheTab`,
and `TestBracketsUnderATasksOverlayReachTheTab`.

**Why shadowing is opt-in.** Claims were originally availability-aware for every
key, so in the same `tasks-list` context `#` opened the theme switcher with
nothing selected and ran Tasks' `delete-selected` with a task selected. A user
reaching for the theme switcher could delete a task. `K`, `W`, `#` were never in
the conflict table — they collided by accident. A claim on a global is now
**unconditional per context** (`claimIsUnconditional`), and availability-awareness
is kept only for keys we do not bind globally, where it is genuinely useful:
`r` claims nothing when there is no proposal to reject, so refresh still works.

### Advertise only what fires

`registerBindings` publishes a Tasks binding to the keymap only when
`registerableKey(context, key)` says the host will route it there:

- overlay/text-input context → everything but `ctrl+c` is real;
- root context → refuse `HostReservedKeys`, then require `mayShadowGlobal`.

That predicate is deliberately the same one `ClaimsKey` uses, so what is
advertised and what fires cannot disagree. Withholding a binding does not
withhold the command: `Commands()` still exports it and
`palette.BuildEntries` turns a command with no binding into a **keyless palette
entry** (only when it is actually invocable — a handler on the command or a
registered keymap command of that ID). `TestRefusedKeysAreNotRegisteredButStayReachable`
and `TestOverlayContextsKeepEveryBinding` pin both halves.

### Adding a global key (host side)

Adding to `keymap.GlobalKeys` or to the root key handler **takes that key away
from Tasks in every root context**, silently. Tasks binds a lot: 355 bindings.
Before you bind a new global:

```sh
# 1. What does Tasks bind on that key, in which contexts?
cd ~/code/tasks
cat > pkg/tui/zz_scratch_test.go <<'EOF'
package tui

import (
	"fmt"
	"testing"
)

func TestScratchBindings(t *testing.T) {
	for _, b := range ExportBindings() {
		if b.Key == "x" { // <- your candidate key
			fmt.Printf("%-28s %s\n", b.Context, b.CommandID)
		}
	}
}
EOF
go test ./pkg/tui/ -run TestScratchBindings -v
rm pkg/tui/zz_scratch_test.go

# 2. Prove routing still does what you think.
cd ~/code/sidecar
go test ./internal/app/ -run TestKeyPrecedence
go test ./internal/plugins/tasks/
```

`TestGlobalKeysAreTheOnesTheHostActuallyHandles` pins `GlobalKeys` against the
real handler, so the set cannot drift from the switch it describes — but it does
not tell you what you took from Tasks. Only step 1 does.

If Tasks should keep the key, add it to `shadowableGlobals` in
`internal/plugins/tasks/routing.go` and add a row to
`TestClaimsKeyFollowsTheConflictTable`. If sidecar should keep it, confirm the
Tasks command is still reachable as a keyless palette entry, and say so in the
commit — that is the trade being made.

Note `r` is deliberately absent from `GlobalKeys`: refresh yields to the plugin
in any context `isGlobalRefreshContext` does not name.

Adding a **second** `KeyRouter` plugin means deciding its conflict table first.

## Release and version timing

Sidecar pins `github.com/marcus/tasks` in `go.mod` and resolves it through
`go.work` (`use . ../tasks ../td`) for local development.

### The trap

`go.work` masks an unpublished dependency. Your local build resolves `../tasks`
from a working tree and passes; CI, a fresh clone, and every other developer
resolve the pinned tag and fail. **Always:**

```sh
GOWORK=off go build ./...
```

### Order of operations

1. Land and tag the Tasks change **first**, in the tasks repo
   (`RELEASE_VERSION=v1.X.0 make release`).
2. Confirm the tag is on the remote:
   `git -C ~/code/tasks ls-remote --tags origin | grep v1.X.0`
3. Re-pin and refresh `go.sum`:
   ```sh
   GOWORK=off go get github.com/marcus/tasks@v1.X.0
   GOWORK=off go mod download github.com/marcus/tasks
   GOWORK=off go build ./...
   ```
   `github.com/marcus/tasks` is public, so no `GOPRIVATE` is required; if that
   ever changes, prefix with `GOPRIVATE=github.com/marcus/tasks`.
4. Run `go test ./internal/plugins/tasks/ ./internal/app/` — that is where a
   Tasks-side rename shows up.
5. Land the sidecar change.

**Never re-pin to a tag that is not pushed.** A local-only tag resolves for you
and nobody else.

### Tag history

`v1.0.0` predates the integration. `v1.1.0` (initial `pkg/tui`), `v1.2.0`
(discard/load-error/quit-latch/theme-overlay fixes), `v1.3.0`
(`SuppressKeyHints`), and `v1.4.0` (published release superseding the three,
whose release workflows failed before publishing artifacts) were minted for this
work. Our `go.mod` currently pins `v1.3.0`; `SuppressViewKeyHints` landed after
`v1.4.0`. Every pin bump so far is one commit:
`2b499d8` → v1.1.0, `5655dc2` → v1.2.0, `c8b6e3f` → v1.3.0.

### Go directive coupling

Sidecar's `go` directive must be **>=** the tasks module's. Both are `go 1.26.0`.
`2b499d8` bumped ours to 1.26 for exactly this reason; a tasks-side bump forces a
matching one here in the same re-pin.

## Verifying a change end to end

### Gates

```sh
cd ~/code/sidecar
GOWORK=off go build ./...
go test ./...
go test ./internal/plugins/tasks/ ./internal/app/   # the routing/precedence proofs
go vet ./...

cd ~/code/tasks
go test ./... && go test -race ./... && go vet ./... && gofmt -l .
```

`pkg/tui/external_consumer_test.go` in the tasks repo (`TestOutOfModuleConsumer`)
proves the module boundary out-of-module with `GOWORK=off`;
`pkg/tui/external_host_contract_test.go` holds the host-facing contracts
(discard vs close, broken store vs empty store, each suppression switch
independently).

### Drive the real app

Unit tests are not sufficient and this integration proved it repeatedly.

**tmux discipline: isolated socket, always.** The machine's default tmux server
holds live Sidecar and agent sessions; never stop, restart, or kill it. Use
`scripts/tmux-drive.sh`, which isolates both axes — the outer host pane on
`-L sidecar-drive`, the sessions sidecar itself creates via `TMUX_TMPDIR`, and
the state tree via `XDG_STATE_HOME` + `-config` + `SIDECAR_ISOLATED_STATE=1`.
Isolating the tmux socket alone is **not** enough: see the td-8d18de story in
[`headless-testing.md`](headless-testing.md), where a proof run with a private
socket still rewrote the developer's live shell manifest.

```sh
go build -o /tmp/sidecar-proof ./cmd/sidecar
./scripts/tmux-drive.sh start 200 50
./scripts/tmux-drive.sh keys 6          # tab number is derived from assembly.Plan — check the header
./scripts/tmux-drive.sh snap tasks-tab
./scripts/tmux-drive.sh keys @          # context picker — Tasks should win
./scripts/tmux-drive.sh snap tasks-at
./scripts/tmux-drive.sh stop
```

### Use a fixture store

Anything that can mutate runs against an isolated `TASKS_DIR`, and you diff the
store before and after. Tasks resolves its own store from the environment, so
this is the only lever:

```sh
mkdir -p /tmp/tasks-proof-store
cp ~/code/tasks/testdata/fixtures/valid/small-gtd/store/tasks.jsonl /tmp/tasks-proof-store/
cp /tmp/tasks-proof-store/tasks.jsonl /tmp/before.jsonl
# ...drive with TASKS_DIR=/tmp/tasks-proof-store in the launch environment...
diff /tmp/before.jsonl /tmp/tasks-proof-store/tasks.jsonl
TASKS_DIR=/tmp/tasks-proof-store tasks check --all-files
```

A read-only smoke may use the configured real store; nothing destructive may.

### What only showed up in the real app

- **Keys stolen by the host.** `1`-`6` reached sidecar, not Tasks views — and
  reached Tasks views when we had decided they should not. Nothing on the Tasks
  side could see this; its registry was correct either way.
- **The footer advertising the wrong keys.** Footer and merged help are both
  built from registered bindings, so bindings the host wins were advertised by
  the plugin that lost them: `#` labelled delete-selected, `1`-`6` labelled
  views. That observation produced `registerableKey` and the keyless palette
  entry.
- **A duplicated footer row.** We render a unified key-hint bar; Tasks painted
  its own underneath. `SuppressFooter` was the only switch and it also removed
  the prompt input (making `tab` focus an invisible caret), the agent transcript,
  the store-read banner, and the filter lines. Tasks ADR-0016 exists because of
  that live run; our adoption is the one line `SuppressKeyHints: true`.

## Change checklist (host side)

- [ ] No sidecar code reads or writes Tasks data, and no config key names a Tasks
      path.
- [ ] New global key? Checked what Tasks binds on it; conflict-table row added if
      Tasks should keep it.
- [ ] Any binding you register, the router will actually honour
      (`registerableKey` ↔ `ClaimsKey` stay the same predicate).
- [ ] New Tasks context to treat as root? Added to `rootContexts` **and** to the
      expected set in `TestRoutingTableIsDerivedFromTheTasksRegistry`.
- [ ] Speculative/stale models `Discard`ed, presented models `Close`d.
- [ ] `GOWORK=off go build ./...` passes.
- [ ] `go test ./internal/plugins/tasks/ ./internal/app/` passes after any re-pin.
- [ ] Driven under `scripts/tmux-drive.sh` with a fixture `TASKS_DIR`; store
      diffed; screen captured.

## Known gaps and traps

- **A bare `[` can cycle tabs from a Tasks root context.** Split SGR mouse escape
  sequences sometimes leak a lone `[` as a rune. `internal/tty/tty.go` and
  `internal/plugins/workspace/mouse.go` carry mouse-proximity workarounds (drop a
  bare `[` within ~10 ms of a mouse event), but the app-level
  `isMouseEscapeSequence` in `internal/app/update.go` only matches sequences
  containing `[<`, or digits and `;` ending in `M`/`m` — it does not filter a
  lone `[`. Now that brackets cycle tabs, a leaked one switches tabs.
- **`Commands()` costs about 0.8 ms** with a live model (measured: 248 commands,
  ~840 µs per call) and is called per render from `internal/app/view.go`,
  `internal/app/update.go`, and `internal/palette/entries.go`. Each call runs
  Tasks' `ExportCommands()` plus a `CommandAvailable` check per current-context
  command. Cache it before adding another per-render caller.
- **`cancel-queued-agent-requests` has no default binding.** Tasks registers it
  with no key sequence, so it exists only as a palette entry. Nothing to route;
  do not treat its absence from the footer as a bug.
- **Root-ness is not derivable.** `ContextMetadata` carries only `Name` and
  `ConsumesTextInput`, with no "is an overlay" bit, so `rootContexts` is a
  hand-maintained allow-list that fails safe: an unknown context over-blocks
  (a global does not fire) rather than under-blocks (quit pops under an open
  overlay). If `pkg/tui` gains that bit, delete the list.
- **`q` is currently a no-op in the Tasks tab.** Tasks latches a quit request and
  `Update` clears it, because sidecar has no exported "request quit" command to
  forward it into. Translating it into our quit flow needs a host affordance that
  does not exist yet.
- **Four config-boundary issues filed during this work:** `td-a3867c` (sidecar
  config owns a td store path), `td-9fb430` (that default is dead config),
  `td-0b7210` (two plugin enablement mechanisms — `plugins.X.enabled` booleans
  for td-monitor/git-status/file-browser/conversations vs. `features.flags` for
  notes and tasks, with workspace having no switch at all), and `td-e3c390`
  (plugin config validation is inconsistent). The first is the same boundary
  violation the Tasks integration is drawn to avoid, and is why Tasks resolves
  its own store.

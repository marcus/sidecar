# Hosting Herdr plugins in Sidecar

Status: **deprecated**, 2026-09-02. Superseded by [Plugin ecosystem: protocol plugins, embedded plugins, one host](../implemented/plugin-ecosystem/README.md), which takes the opposite bet: a Sidecar-native contract grown from the frozen terminal resource protocol, with no manifest format and no PTY pane host. A Herdr-manifest adapter could be written against that plan's seams later; nothing below is scheduled. Kept for the Herdr source analysis and the noun-mapping table.

Related: [Herdr as Sidecar's remote host runtime](herdr-remote-hosts.md) — independent axis; that plan makes Sidecar a *client* of a remote Herdr, this one makes Sidecar a *host* for Herdr's plugin ecosystem locally. They share nothing but the upstream relationship and the habit of verifying claims against Herdr source.
Herdr source inspected: [`herdrdev/herdr`](https://github.com/herdrdev/herdr) at `c2637dc1` (local checkout `~/code/herdr`); authoring contract in `docs/next/website/src/content/docs/plugins.mdx` and `socket-api.mdx`.

## Decision first

Sidecar becomes a **compatible host for Herdr plugins**: a plugin written against Herdr's documented contract — a directory with a `herdr-plugin.toml` manifest whose entrypoints are argv subprocesses that call back over the `HERDR_*` environment contract — runs in Sidecar without modification, minus pixel graphics, which Sidecar refuses in the same way Herdr itself refuses them when the feature is off.

This is feasible because Herdr's plugin model is deliberately thin. Verified at `c2637dc1`, the entire host contract is:

1. A TOML manifest parsed by one function with stable string error codes (`src/app/api/plugins/manifest.rs`).
2. Argv subprocess spawns with cwd = plugin root and ~20 `HERDR_*` env vars, of which `HERDR_PLUGIN_CONTEXT_JSON` carries everything structured (`src/app/api/plugins/runtime.rs:39-83`).
3. A newline-delimited JSON socket (`HERDR_SOCKET_PATH`) and a CLI (`HERDR_BIN_PATH`) that are the *same* API — the CLI is a thin socket client. There is no SDK, no sandbox, no in-process runtime, no restricted plugin surface ("the entire Herdr CLI is the plugin API", plugins.mdx:22-29).
4. A PTY pane host with five placements (`overlay`, `popup`, `split`, `tab`, `zoomed`).

Nothing in a plugin binds to Herdr internals. A compatible host needs the manifest loader, the env contract, enough of the socket API, and the pane host. Sidecar builds those four things and reuses its existing tmux, keymap, modal, and notification machinery underneath.

**Ownership consequence.** Sidecar's standing design exception — no CLI parity because it owns nothing — does not apply here. Hosting plugins is a capability Sidecar *owns*: if Sidecar were uninstalled, the capability vanishes. Every operation in this plan therefore has a non-interactive path (`sidecar plugin …` and the compat shim) from Phase A, not as an afterthought.

## Scope boundary

**In scope**

- Manifest parsing and validation compatible with Herdr's, including its error codes and warnings.
- Plugin registry (link/unlink/list/enable/disable) with import from an existing Herdr installation.
- Startup hooks, event hooks, actions, plugin panes (all five placements), link handlers.
- A compat socket implementing the plugin-relevant subset of Herdr's JSON API, and a CLI shim so `$HERDR_BIN_PATH <herdr command>` works.
- An event stream (`events.subscribe` et al.) fed by Sidecar's agent-status and workspace lifecycle signals.
- Keyboard binding of plugin actions through Sidecar's existing keymap override mechanism, plus command-palette access.
- Structured refusal of pixel graphics.
- GitHub install flow with preview and confirmation (later phase).

**Explicitly out of scope**

- Pixel graphics support (`pane.graphics.*` rendering). Refused, not implemented — see [Graphics](#graphics-the-honest-refusal).
- A Sidecar-native plugin SDK or manifest format. If Sidecar ever wants first-party-shaped plugins, they should be Herdr-manifest plugins; inventing a second format would fork the ecosystem this plan joins.
- Running Sidecar's compiled-in plugins (git, files, workspace…) through this mechanism. They stay compiled in.
- Sandboxing. Herdr's trust model is explicit: a plugin runs with full user privileges, and installation shows the user what will run (plugins.mdx:35-52). Sidecar adopts the same model with the same preview-and-confirm honesty. Pretending to more isolation than exists would be worse than none.
- Serving plugins on remote hosts. A plugin on the Mac mini runs under the Herdr server there; the remote-hosts plan covers seeing its effects.

## The compatibility contract, surface by surface

Everything below is verified against `c2637dc1` with citations into `~/code/herdr`.

### Manifest and registry

`herdr-plugin.toml` fields: `id` (≤120 chars, charset-validated), `name`, `version` (any non-empty string), `min_herdr_version` (required, semver, gates install/link), `description`, `platforms` (linux/macos/windows; empty array is an error, omission a warning), and item tables `[[build]]`, `[[startup]]`, `[[actions]]` (id/title/contexts/command), `[[events]]` (on/command; unknown event names warn, not error), `[[panes]]` (id/title/placement/width/height/command; sizes legal only for `popup`), `[[link_handlers]]` (regex pattern → action). Commands are argv arrays, never shell; item-level `platforms` overrides top-level. All parsing/validation in `manifest.rs` with stable error codes (`plugin_requires_newer_herdr`, `invalid_plugin_pane_size`, `platform_unsupported`, …).

Sidecar ports this loader faithfully — same fields, same validation order, same error codes, same warnings — as a pure function in `internal/herdrplug/manifest`, with Herdr's manifest tests ported alongside as the conformance suite. The loader is the compatibility keel; everything else can be partial, this cannot.

**Registry decision: Sidecar keeps its own registry file, same record schema, with one-command import.** Herdr's registry is `~/.config/herdr/plugins.json` (flock + atomic writes, global per user, manifests cached in the records, `src/persist/plugin_registry.rs`). Sidecar writes `~/.config/sidecar/herdr-plugins.json` in the same shape rather than sharing Herdr's file, because two hosts read-modify-writing one flocked file with independent `enabled` semantics would fight, and because Sidecar must not corrupt another product's config to save one import step. `sidecar plugin link --from-herdr [<id>]` reads Herdr's registry read-only and links the same on-disk plugin directories — files are never copied by link, in either product.

Version gating: Sidecar's loader compares `min_herdr_version` against an **emulated Herdr version** — the newest Herdr release whose plugin surface this host implements, recorded as a constant and bumped deliberately. The compat `ping`/`herdr --version` report that version; the `capabilities` list in `ping` (Herdr's own extension point, `src/api/server.rs:347-355`) additionally carries `sidecar-host` so a plugin that cares can detect the real host. Env also gets `SIDECAR_HOST=1` alongside the standard `HERDR_*` set for the same reason.

### Execution and env

Every entrypoint is a one-shot subprocess: cwd = plugin root, stdout/stderr piped and capped at 64 KiB each, at most 32 in flight, completions recorded in an in-memory ring of 200 log records queryable via `plugin.log.list` (`runtime.rs:11-13, 284-311`). No stdin protocol; the callback channel is the socket/CLI. Sidecar replicates these numbers exactly — they are observable behavior plugins may depend on.

Env contract (always): `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH`, `HERDR_ENV=1`, `HERDR_PLUGIN_ID`, `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`, `HERDR_PLUGIN_CONTEXT_JSON`. Conditional: `HERDR_WORKSPACE_ID`/`HERDR_TAB_ID`/`HERDR_PANE_ID`, `HERDR_PLUGIN_ACTION_ID`, `HERDR_PLUGIN_EVENT`(+`_JSON`), `HERDR_PLUGIN_ENTRYPOINT_ID`, `HERDR_PLUGIN_CLICKED_URL`/`HERDR_PLUGIN_LINK_HANDLER_ID`. Per-plugin config/state dirs follow Herdr's split (`plugin_paths.rs:15-31`) but under Sidecar's trees: config `~/.config/sidecar/herdr-plugins/config/<escaped-id>/`, state `$XDG_STATE_HOME/sidecar/herdr-plugins/<escaped-id>/`.

`HERDR_PLUGIN_CONTEXT_JSON` is the structured invocation context (`src/api/schema/plugins.rs:363-395`): workspace/tab/pane ids and labels, cwd, worktree info, focused agent + status, selected text, invocation source, clicked URL. Sidecar synthesizes it from the noun mapping below plus `internal/agentstatus` (agent + status) and its selection state (selected text — Sidecar has real selections to offer here).

### The noun mapping — the central design risk

Herdr's model is workspace → tab → pane, all inside one compositor process. Sidecar's is project → shells (detached tmux sessions) and worktrees, with panes as split leaves. The compat API projects Sidecar into Herdr nouns:

| Herdr noun | Sidecar backing | Notes |
| --- | --- | --- |
| workspace | project workspace entry (a worktree or the project root, `workspaceinventory`) | `identity_cwd` = checkout path. `workspace.create` with `cwd` maps to worktree/project semantics only where Sidecar has them; otherwise `unsupported` |
| tab | shell (tmux session, `shells.json`) | `tab.create` = create shell; labels = shell display names |
| pane | tmux pane within a shell (terminal split leaves) | public IDs minted by the compat layer, stable per Sidecar instance, mapped to tmux pane IDs internally |
| agent | Sidecar's per-shell agent identity (`agentactivity` + `agentstatus`) | status vocabulary maps lane-for-lane; `done` = Sidecar's `LaneDone` |
| worktree | Sidecar worktree (`workspaceops`) | closest to 1:1 of all the nouns |
| popup | new: the popup host (below) | singleton, session-modal, matches Herdr semantics |

This projection is where compatibility can quietly rot, so it lives in one package (`internal/herdrplug/projection`), is exercised by the conformance suite, and every method the compat socket serves declares its fidelity: **full**, **partial** (documented deviation), or **unsupported** (structured error, never a silent no-op). The spike's job is to find out whether the real example plugins fit this projection or break it.

### The compat socket and CLI shim

**Socket.** One JSONL socket per running Sidecar instance, `0600`, under the instance's state path (Sidecar already announces instances at `$XDG_STATE_HOME/sidecar/instances/<pid>.json`; the socket path joins that record). Protocol: Herdr's — one JSON request line, one response line, except `events.subscribe` which upgrades the connection to a one-way event stream, and the blocking waits (`events.wait`, `agent.wait`, `pane.wait_for_output`) which hold the connection until match/timeout/disconnect (`src/api/server.rs:173, 686-748`). Implemented in `internal/herdrplug/api` as a plain `net.Listener` with one goroutine per connection — the same shape as Herdr's thread-per-connection model.

Initial method surface (fidelity class in Phase A):

- **Full:** `ping`, `api snapshot`/`schema` equivalents, `plugin.list/link/unlink/enable/disable`, `plugin.action.list/invoke`, `plugin.log.list`, `plugin.pane.open/focus/close`, `popup.close`, `notification.show` (maps directly onto `internal/notify`), `events.subscribe`, `events.wait`, `agent.list/get/wait`, `worktree.list`.
- **Partial:** `session.snapshot`, `workspace.list/get`, `tab.list/get`, `pane.list/get/read/send_text/send_keys/focus`, `agent.read/explain` (backed by tmux capture and Sidecar's detection, not Herdr's), `workspace.create`/`tab.create` (worktree/shell creation), `pane.wait_for_output`.
- **Unsupported (structured errors):** `pane.graphics.*` (`feature_disabled` — see below), `layout.*` beyond what shell splits express, `server.*`, `integration.*`, `agent.view.set/clear` (no agents-sidebar filter concept; error until there is something honest to bind it to), `pane.report_agent`/`report_agent_session`/`clear_agent_authority`/`release_agent` (agent authority — deliberately deferred, open question below).

**Shim.** Plugins call `$HERDR_BIN_PATH <herdr CLI grammar>`. Sidecar ships the shim as a busybox-style alternate entry: a `herdr-compat` executable (symlink or copy of `sidecar` dispatching on `argv[0]`, never named `herdr` and never placed on `PATH`, so a real Herdr install is never shadowed). It implements Herdr's CLI grammar for the supported method set by doing exactly what Herdr's CLI does — translating argv to socket requests — so the socket remains the single implementation and the shim cannot drift from it. `HERDR_BIN_PATH` points at the shim; `HERDR_SOCKET_PATH` at the instance socket; a plugin may use either, per the contract.

**CLI extensions (Sidecar-owned, additive).** Native spellings under `sidecar plugin …` mirror the management surface (`link`, `unlink`, `list [--json]`, `enable`, `disable`, `action list/invoke`, `log list`, `pane open/focus/close`, `config-dir`) — required by the ownership rule, and routed through the same socket so TUI-less operation degrades gracefully to direct registry edits exactly as Herdr's CLI does offline (`src/cli/plugin.rs:82, 307-317`). Two additions Herdr does not have:

- `sidecar plugin doctor [<id>]` — a compatibility report built from the command log plus a per-plugin tally of `unsupported`/`feature_disabled` responses the compat socket has returned. This turns "the plugin silently doesn't work" into "this plugin called `pane.graphics.set` 14 times and `layout.export` twice; those are unsupported here."
- `sidecar plugin events tail [--json]` — attach to the event hub as a JSONL stream from the terminal. This is simultaneously a debugging tool for plugin authors and the local proof of the `herdr api events` shape the remote-hosts plan requests upstream.

### Popups and pane placements

Herdr's popup (`src/app/popup.rs`, `src/popup_size.rs`): a **singleton, session-modal PTY** — always a spawned command's terminal, never rendered text. Centered, size in cells or `"NN%"` (default half the screen, min 6×4 outer, border eats a cell each side), no position parameter. All input — including Escape and the prefix key — is forwarded to the PTY; it closes when the command exits or on `popup.close`; opening while another modal surface is active returns `ui_busy`; agent detection is disabled inside it. It has no pane ID and does not participate in pane/layout/persistence/agent APIs.

Sidecar has every piece except the assembled whole: detached tmux sessions running arbitrary argv (the inline-editor pattern, `internal/tty/editor_session.go`), text-cell capture rendering (`internal/termpreview`, `internal/tty/screenmodel`), render-into-a-rect compositing (`internal/panemodal`, `internal/ui/overlay.go`), and app-modal priority with keymap capture (`internal/app/model.go:40-129`). The popup host is a new top-priority app modal kind that mounts a `termpanes`-style leaf in a centered box with Herdr's exact geometry resolution, forwards *all* keys to the PTY ahead of mode dispatch (mirroring Herdr's `input/mod.rs:82-84`), disables agent detection for the session, and closes on process exit or `popup.close`. The existing two-live-terminals cap (`internal/termpanes/session.go:17-20`) gets a popup exemption or a raise; the popup is short-lived by design.

The other placements map onto the pane tree Sidecar already has: `split` → a terminal split of the focused shell; `tab` → a new shell in the target workspace; `zoomed` → split then zoom; `overlay` → zoom-over-active-pane with focus/zoom restore on close (Herdr's own overlay is exactly that, `panes.rs:44-80`). Opened panes are recorded as plugin-owned so `plugin.pane.focus/close` operate only on them, and `plugin.pane.close` never touches anything else — the same ownership rule as everywhere in Sidecar.

### Keyboard shortcuts

The anticipated hard problem mostly dissolves on evidence: **Herdr plugins do not declare keybindings.** The manifest has no key field; users bind qualified action IDs in their own Herdr config (`[[keys.command]] type = "plugin_action"`, plugins.mdx:334-347), and plugin actions ship unbound. So Sidecar owes an *equivalent user-side binding mechanism*, not manifest-driven key registration — and it already has one.

- Every enabled plugin action registers as a keymap command with ID `herdr-action:<qualified.action.id>` via the existing `RegisterPluginBinding` seam (`internal/keymap/registry.go:75`), in the global context, **with no default binding** — matching Herdr.
- Users bind them exactly like any other command: `keymap.overrides` in `~/.config/sidecar/config.json`, key → `herdr-action:<id>`. Multi-key sequences ("g g"-style, already supported with the 500 ms timeout) give users prefix-like chords if they want Herdr's `prefix+x` feel.
- Binding validation refuses `HostReservedKeys` (`internal/keymap/hostkeys.go:20-24`) and warns on collisions with existing context bindings instead of silently losing by registration order — the one genuine gap in today's registry (no conflict diagnostics for dynamic registrations) and the real work item in this section. Diagnostics surface in the existing config-diagnostics path, mirroring Herdr's "kept X, disabled Y" approach (`src/config/keybinds.rs:1041-1046`).
- Unbound actions remain reachable: they appear in the command palette (the Palette modal) under their plugin's name, and via `sidecar plugin action invoke` / the shim. Keyboard is an accelerator, never the only path.
- Sidecar does **not** read Herdr's `config.toml` keybindings. Bindings are host-side by both products' design; importing another host's bindings would collide with Sidecar's own tables for no compatibility gain.

Invocation via a binding sets `invocation_source: "keybinding"` in the context JSON, as Herdr does (`mod.rs:225-254`).

### Hooks

Herdr has two plugin hook kinds, both argv subprocesses (plugins.mdx:238-266): `[[startup]]` — once per enabled plugin after the server is ready, one-shot, unsupervised, failures logged and non-fatal — and `[[events]] on = "<name>"` — spawned per matching emitted event with `HERDR_PLUGIN_EVENT` and the full `EventEnvelope` in `HERDR_PLUGIN_EVENT_JSON`. The hookable set is `PLUGIN_HOOK_EVENT_KINDS` (`src/api/schema/events.rs:286-309`): workspace/worktree/tab/pane lifecycle plus `pane.agent_detected` and `pane.agent_status_changed`, with high-volume kinds (`pane.output_changed`, `pane.updated`, `layout.updated`) deliberately excluded. `[[build]]` runs only during GitHub install, never at link or runtime, with all `HERDR_*` runtime env scrubbed (`src/cli/plugin.rs:1502-1520`).

Sidecar's precedent is the worktree setup hook (`internal/workspaceops/setup.go:48-64`) — but that one is blocking, fd-passed, and output-discarding because it runs at a synchronous decision point. Plugin hooks are the opposite profile: fire-and-forget, capped-output, ring-logged. They share the spawner in `internal/herdrplug/runtime` rather than the worktree hook's code. Startup hooks run once per enabled plugin after the compat socket is listening — which per the startup-latency rule is itself after the first frame, never before it. Event hooks hang off the event hub (next section) filtered to the hookable kinds.

The worktree setup hook stays exactly as it is. It is a Sidecar feature, not a Herdr surface, and its blocking-with-confirmation design is correct for its job. What this plan adds is the observation that `worktree.created` is a hookable Herdr event — so a plugin like `dev-layout-bootstrap` gets its layout-on-new-worktree behavior through the event hook path without Sidecar inventing anything.

### Events

Herdr's subscription vocabulary (`src/api/schema/events.rs:16-85`): parameterless lifecycle subscriptions (workspace/worktree/tab/pane created/closed/focused/renamed/moved/exited, `pane.agent_detected`, `layout.updated`) plus three parameterized ones — `pane.output_matched` (substring/regex over pane output), `pane.agent_status_changed` (with initial-snapshot delivery), and `pane.scroll_changed`. A subscription is a held connection: `events.subscribe` replies `subscription_started` and the connection becomes a one-way JSONL stream until either side closes.

Sidecar builds an **event hub** in `internal/herdrplug/api`: a sequence-numbered ring fed by adapters from Sidecar's internal signals, serving both held subscriptions and the one-shot waits. The feeding adapters are the honest inventory of what Sidecar can actually emit:

| Herdr event | Sidecar source | Phase |
| --- | --- | --- |
| `pane.agent_status_changed`, `pane.agent_detected` | `internal/notify.LaneTracker` transitions — explicitly designed for a second, headless consumer (`internal/notify/triggers.go:11-29`) | A |
| `worktree.created/opened/removed` | `internal/workspaceops` | A |
| `pane.created/closed/focused` (shell-level) | shell lifecycle in the workspace plugin / `shells.json` observer | A |
| `workspace.*`, `tab.*` lifecycle | the noun projection over project/shell events | B |
| `pane.output_matched` | tmux capture pipeline + matcher; costlier, bounded per-subscription | B |
| `pane.exited`, `pane.moved`, `layout.updated`, `pane.scroll_changed` | as the projection grows; never-firing until then | C or never |

Kinds Sidecar cannot emit simply never fire — within spec, since Herdr itself treats unknown/unfired hooks as normal. But `plugin.list` warnings and `sidecar plugin doctor` both call out subscriptions/hooks naming events this host never emits, so the silence is visible instead of mysterious.

The agent-interaction events are the ones that matter most — the flagship example plugin (`agent-telegram-notify`) is precisely "subscribe to `pane.agent_status_changed`, notify externally." Sidecar's lane vocabulary (`working/blocked/done/idle`) maps lane-for-lane onto Herdr's `AgentStatus`, including the `done` = finished-unseen semantics both products already share via td-48ecf2.

### Graphics: the honest refusal

Herdr's pixel graphics (`pane.graphics.set/clear/info/stream`, kitty-protocol compositing in `src/kitty_graphics.rs`) are **experimental and off by default in Herdr itself**: without `[experimental] kitty_graphics = true`, every graphics method returns the stable error code `feature_disabled` (`src/config/model.rs:985`, socket-api.mdx:181-182). There is no manifest field declaring graphics use — the requirement is purely behavioral — so install-time rejection from the manifest is impossible for anyone, including Herdr.

Sidecar's behavior, all three layers of it:

1. **API:** every `pane.graphics.*` method returns `feature_disabled`, and `pane.graphics.info` reports no support — byte-for-byte the degraded mode a well-written plugin must already handle. This is within-spec host behavior, not a compatibility break.
2. **Runtime detection:** Sidecar's embedded panes are text-cell capture over tmux (`internal/tty/screenmodel`); kitty graphics APC sequences cannot render there regardless. When the vt layer sees a kitty-graphics APC sequence in a plugin pane's stream, the pane gets a one-time notice ("this plugin draws pixel graphics, which Sidecar's embedded panes cannot display") instead of silent garbage.
3. **Doctor:** graphics refusals are tallied per plugin, so `sidecar plugin doctor` names the incompatibility explicitly.

Upstream request worth filing: an optional manifest capability declaration (e.g. `requires = ["kitty_graphics"]`) so *both* products could refuse or warn at link time instead of at first use. Until then, the three layers above are the whole story.

### Startup

Per the startup-latency rule, nothing in this plan touches the first-frame path. Concretely: registry read, manifest parsing, socket listen, and startup hooks all begin from a `tea.Cmd` after the first frame; plugins render as "loading" in any UI that lists them until the registry message arrives; `SIDECAR_STARTUP_TRACE=stderr` must show no new pre-frame phase. Startup hooks then run once per enabled plugin, concurrently, fire-and-forget, after the socket is accepting — matching Herdr's ordering (server ready → hooks, `src/server/headless.rs:5137`). Hooks are *not* re-run on project switch; the plugin registry is instance-global, not per-project, and a `Reinit` does not constitute a new session. Whether project switch should emit `workspace.focused`-shaped events is part of the noun projection, not the hook lifecycle.

## Architecture

```text
internal/herdrplug
├── manifest        ported loader + validation, Herdr's error codes, conformance tests
├── registry        herdr-plugins.json (Herdr record schema), flock + atomic writes, --from-herdr import
├── runtime         subprocess spawner: env contract, 64 KiB caps, 32 in-flight, 200-entry ring log
├── projection      the noun mapping (workspace/tab/pane/agent ⇄ project/shell/tmux pane/agentstatus)
├── api             compat socket: JSONL dispatch, event hub, blocking waits, method fidelity table
└── shim            herdr CLI grammar → socket client (argv[0] dispatch, never named `herdr`)

internal/app        popup host (new top-priority modal kind, full key forwarding)
internal/keymap     conflict diagnostics for dynamic registrations (the one keymap work item)
internal/cli        `sidecar plugin …` native tree, doctor, events tail
```

The projection and the api packages are the only places Herdr nouns and Sidecar nouns meet. Manifest, registry, runtime, and shim are host-independent by construction — a deliberate echo of Herdr's own layering, and what keeps the conformance suite meaningful.

Feature flag: `features.HerdrPlugins`, default off, per the standard flag registry.

## Phases

### Phase 0 — spike

Prototype against the three official example plugins (`ogulcancelik/herdr-plugin-examples`: `agent-telegram-notify`, `github-link-preview`, `dev-layout-bootstrap`), which between them exercise agent events → outbound notification, link handlers → action with clicked URL, and workspace/worktree bootstrap via CLI calls — the practical compatibility bar.

1. **Manifest conformance.** Port the loader; run Herdr's manifest tests against it. Exit: identical accept/reject/warn behavior on every test case.
2. **The flagship path.** Minimal socket (`ping`, `events.subscribe` with `pane.agent_status_changed`, `notification.show`) + env contract + event hooks. Exit: `agent-telegram-notify` runs unmodified — its hook fires on a real Sidecar agent status transition and it successfully calls out through the shim.
3. **The projection stress test.** Run `dev-layout-bootstrap` and record every method it calls, each mapped as full/partial/unsupported. This is the evidence for whether the noun mapping holds or needs redesign — the plan's biggest open risk, answered with a real plugin rather than argument.
4. **Link handlers probe.** Determine what `github-link-preview` needs from the host's link pipeline and whether Sidecar's content-link handling (`ContentLinkProvider`, OSC 8 in panes) can feed `HERDR_PLUGIN_CLICKED_URL`.

**Exit gate:** a fidelity matrix over every method and event the three examples touch, and a go/no-go on the projection design.

### Phase A — management, hooks, events, actions

Behind `features.HerdrPlugins`, default off.

- Manifest + registry + import; `sidecar plugin link/unlink/list/enable/disable/config-dir` and the same over the socket.
- Runtime spawner with the full env contract; startup hooks; event hooks over the Phase-A event set (agent status/detected, worktree lifecycle, shell-level pane lifecycle).
- `events.subscribe`/`events.wait`/`agent.wait`; `plugin.action.list/invoke` (CLI, socket, palette); `plugin.log.list`; `notification.show`.
- The shim, with the Phase-A method surface; `sidecar plugin doctor` and `events tail`.
- Every unsupported method returns its structured error from day one — the fidelity table ships complete even though the implementations don't.

**Exit gate:** `agent-telegram-notify` works in daily use; actions invoke from CLI, palette, and a user keybinding; `doctor` correctly reports a deliberately incompatible test plugin; startup trace unchanged.

### Phase B — panes, popups, keys

- The popup host with Herdr's geometry, input-capture, singleton, and `ui_busy` semantics; `popup.close`.
- `split`, `tab`, `zoomed`, `overlay` placements over the pane tree; plugin pane ownership records; `plugin.pane.open/focus/close` end to end.
- Keymap conflict diagnostics for dynamic registrations; `herdr-action:*` commands bindable via `keymap.overrides`; palette listing polish.
- `pane.read`/`send_text`/`send_keys` over the projection; `pane.output_matched` subscriptions (bounded).
- Link handlers wired to Sidecar's link pipeline, if Phase 0 item 4 said yes.

**Exit gate:** `dev-layout-bootstrap` (or an equivalent pane-opening plugin) opens each placement correctly; a popup plugin captures and releases input exactly like a Herdr popup; no key bound to a plugin action can shadow a host-reserved key.

### Phase C — install flow and the long tail

- `sidecar plugin install <owner>/<repo>[/subdir]` with Herdr's exact safety shape: shallow fetch, preview of every command that will run, confirmation, env-scrubbed build, manifest byte-identical re-check after build, managed checkout under Sidecar's config tree, rollback on failure (`src/cli/plugin.rs:154-261, 1286-1296`).
- `workspace.create`/`tab.create` write paths; remaining projection methods that earned their keep in the field.
- Decide the agent-authority question (below) with Phase A/B evidence in hand.

**Exit gate:** install → preview → build → run of a real marketplace plugin, and an uninstall that leaves nothing behind but the plugin's own state dir.

## Failure, degradation, security

- **A plugin can never take the host down.** Spawns are capped, output is capped, logs are ringed, panics in compat-layer goroutines are recovered — the same posture as `plugin.Registry`'s panic recovery for compiled-in plugins.
- **Trust is explicit, exactly like Herdr's.** No sandbox is claimed. Link and install show what will run; build commands run only in the install flow, confirmed, with runtime env scrubbed. Docs state plainly that a plugin runs with the user's full privileges.
- **The socket is `0600`, per instance, never network-exposed.** Same rule as Herdr's, same rule as the remote-hosts plan.
- **Ownership-safe operations only.** `plugin.pane.close` closes only recorded plugin panes; unlink leaves plugin files on disk (link never copied them); disable clears only what the plugin contributed.
- **No silent incompatibility.** Unsupported methods error with stable codes; never-firing events are named by `doctor` and `plugin.list` warnings; graphics refusal is three-layered and visible.
- **Rollback:** disabling `features.HerdrPlugins` leaves Sidecar's behavior and state byte-identical apart from the inert registry file.

## Open questions

- **Agent authority.** Herdr accepts `pane.report_agent` as authoritative state that suppresses its own screen-scraping detection (`full_lifecycle_hook_authority`, `src/detect/mod.rs:316-327`). Sidecar has its own detection stack and no authority override. Accepting reports would improve fidelity for agents with Herdr integrations installed, but it wires an external claim into `agentstatus`'s trust model. Deferred to Phase C with a real-world example in hand.
- **`agent.view.set`.** Herdr plugins can install declarative filters over its agents sidebar. Sidecar's closest surface is the Sessions browser. Unsupported until there is an honest binding; revisit if a compelling plugin needs it.
- **Popup rendering fidelity.** Herdr's popup PTY renders in-process; Sidecar's renders through tmux capture, which the embedded-terminal work has made good but not identical (native cursor, key pacing). Phase B must verify a full-screen TUI plugin (a picker, say) is actually pleasant in the popup host, not merely functional.
- **Per-instance vs per-project plugin runtime.** The registry is user-global (matching Herdr), the socket per instance. Whether event hooks should fire in *every* running instance or only a designated one (duplicate Telegram messages from two open projects would be a bug from the user's view) needs a decision in Phase A — likely a single-instance election via the existing instances directory.
- **Emulated version policy.** Which Herdr release to claim, and what to do when a plugin's `min_herdr_version` outruns the implemented surface — refuse (safe, Herdr-faithful) or link-with-warning (pragmatic). Start with refuse; revisit on evidence.

## Acceptance evidence

| Journey | Evidence |
| --- | --- |
| Conformance | Ported manifest tests pass; fidelity table published for every wire method |
| Flagship plugin | `agent-telegram-notify` unmodified: hook fires on a real status transition, outbound call succeeds |
| Actions | One action invocable via socket, shim, `sidecar plugin action invoke`, palette, and a user keybinding — all five produce the same log record with the right `invocation_source` |
| Popup | Opens centered at manifest size, captures all input including Escape, closes on exit and on `popup.close`, restores prior focus |
| Placements | Each of split/tab/zoomed/overlay lands in the pane tree; `plugin.pane.close` touches only plugin-owned panes |
| Events | `sidecar plugin events tail` shows agent transitions matching the UI's status column, with an initial snapshot on subscribe |
| Graphics | A graphics-calling plugin gets `feature_disabled`, a visible notice, and a `doctor` entry — never silent garbage |
| Keys | A plugin action bound over a host-reserved key is refused with a diagnostic; conflicts warn instead of silently losing |
| Startup | `SIDECAR_STARTUP_TRACE=stderr` unchanged with ten plugins linked |
| Rollback | Flag off → behavior and state byte-identical apart from the registry file |

## Changelog

- **2026-08-28** — Created, from source inspection of Herdr's plugin system at `c2637dc1` (manifest loader, runtime contract, socket API, popup/pane host, keybinding model) and of Sidecar's extension seams (keymap registrar, modal/panemodal compositing, LaneTracker, uirequest bus, termpanes).

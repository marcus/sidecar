# Agent lifecycle capability matrix

**Status:** Phase A evidence baseline recorded 2026-08-30; OpenCode cancellation traced and promoted to `full` in Phase B, 2026-08-30; installation, status, and repair added in Phase C, 2026-08-30; Codex and Claude Code traced against their current releases in Phase D, 2026-08-30; the untraced Pi entry retracted 2026-09-02 because Sidecar ships no source that could produce a report for it. **Plan:** [Deterministic agent lifecycle hooks](../plans/active/notification-agent-lifecycle-hooks.md). **Tracking:** `td-43a93f`.

How an integration is installed, inspected, and removed is [Agent lifecycle integrations](agent-lifecycle-integrations.md). This document records what each agent provider's own lifecycle events can actually tell Sidecar, how strong the evidence for that claim is, and what authority tier the evidence justifies. It is the prose companion to `internal/agentlifecycle/capabilities.json`, which is the machine-readable version the code reads and the tests police. That file is embedded into the binary and read at runtime through `agentlifecycle.Capabilities()`, so the registry the resolver trusts and the evidence these tests police are one file rather than two that could drift.

The matrix is evidence, not aspiration. A provider does not get full lifecycle authority because its documentation lists the right event names. It gets full lifecycle authority when sanitized real traces show the transitions arriving, in order, from a released version.

## How a tier is earned

Sidecar recognises four tiers, defined in `internal/agentlifecycle`:

| Tier | What it means |
| --- | --- |
| `full` | A fresh same-run report authors `working`, `blocked`, or `idle`. Screen evidence remains diagnostic and cannot reverse it. |
| `advisory` | A fresh event may confirm what the screen already sees, or speak when the screen has no opinion, but may never contradict it. |
| `session-identity` | The source identifies the provider session reliably but not its state. Screen and process detection stay the sole lifecycle authority. |
| `screen-fallback` | No valid integration evidence applies. Existing detector and tracker behavior is unchanged. |

To hold `full`, a source must have `evidence: real-trace` and must cover every transition in `FullLifecycleTransitions()`: `work_start`, `blocked_on_request`, `unblocked`, `turn_complete`, `cancelled`, `session_identity`, and `process_exit`.

`tool_use` and `subagent` are deliberately excluded from that requirement. Tool use is a refinement of `work_start` rather than a separate lane, and subagent aggregation has no proved cross-provider rule yet, so requiring either would withhold authority for reasons unrelated to the lanes Sidecar renders.

This is enforced rather than documented. `Capability.TierFor` polices every tier boundary, not only the top one: a `full` claim without real traces or complete coverage falls to `advisory`, an `advisory` claim with no evidence or no covered transition falls out to `screen-fallback`, and a `session-identity` claim that does not actually cover session identity falls out too. Only `screen-fallback`, which asserts nothing, needs no evidence. `TestCapabilityMatrixCannotClaimUnearnedAuthority` re-derives every entry's tier from its own recorded evidence and `TestTierForPolicesEveryTierBoundary` covers the lower boundaries directly. An entry cannot be edited to claim authority its evidence does not support, and it cannot escape scrutiny by claiming less either.

## Summary

| Provider | Version seen | Evidence | Tier now | Herdr's tier | Blocking gap |
| --- | --- | --- | --- | --- | --- |
| OpenCode | 1.18.23, 1.18.25 | real-trace | `full` | lifecycle authority | none blocking; see gaps below |
| Codex | 0.151.0 | real-trace | `session-identity` | session only | shipped hook is SessionStart only. The provider's own contract is traced and would support `full`. |
| Claude Code | 2.1.220 | real-trace | `session-identity` | session only | shipped hook is SessionStart only. The provider's ceiling is `advisory`: no cancellation event exists. |
| Pi | 0.84.3 | real-trace | `advisory` | lifecycle authority | blocking is structurally impossible; `advisory` is the ceiling and is reached |
| Kilo Code | 7.5.9 | real-trace | `advisory` | lifecycle authority | cancellation is indistinguishable and the shipped asset releases nothing on exit, so `advisory` is the ceiling and is reached |
| Kimi Code | 0.40.1 | real-trace | `advisory` | lifecycle authority | session identity is refused by Sidecar's own catalog, and process exit is unclaimed by choice; `full` needs both |
| OMP (oh-my-pi) | 18.1.8 | real-trace | `advisory` | lifecycle authority | cancellation is observable and deliberately not read, and process exit is unclaimABLE; `full` needs both |

Pi's row went out of `capabilities.json` when nothing could produce a report for it, came back at `session-identity` when `PiAdapter` and `assets/pi/sidecar-lifecycle.js` shipped, and is now at `advisory` on `real-trace` evidence because a live Pi 0.84.3 session has been traced. It is the only row here that has reached its own ceiling: `advisory` is as high as Pi can ever go, because `full` needs `blocked_on_request` and `unblocked` and Pi ships no permission system to produce either. See "Why the Pi entry was retracted, and what brought it back".

Two columns are doing different jobs here and it is worth being explicit about which. **Tier now** is what the *shipped Sidecar asset* earns, and for Codex and Claude that is `session-identity` because each ships one `SessionStart` entry and nothing more. **Blocking gap** describes the *provider's* ceiling — the best any Sidecar adapter could honestly claim if one were written today. Codex's ceiling is `full` and Claude's is `advisory`, and neither is reached yet because neither asset asks for it.

"Herdr's tier" is what the Herdr project ships at commit `4a3b04f59ba3b7d8a15cea187b23e1e80c343b0c`. It is included because Herdr has shipped all four integrations in production, so where it disagrees with the published contract that disagreement is itself evidence.

## Transition coverage

`YES` means an official event exists and, where marked traced, was observed. `PARTIAL` means it must be inferred. `NO` means no event exists.

| Transition | OpenCode | Codex | Claude Code | Pi | Kilo Code | Kimi Code | OMP |
| --- | --- | --- | --- | --- | --- | --- | --- |
| work start | YES (traced) | YES (traced) | YES (traced) | YES | YES (traced) | YES (traced) | YES (traced) |
| tool use | YES (traced) | YES (traced) | YES (traced) | YES | NO (on the plugin event stream) | YES (traced) | YES (traced), not consumed |
| blocked on request | YES (traced) | YES (traced) | YES (traced) | NO | YES (traced) | YES (traced) | YES (traced) |
| unblocked | YES (traced) | YES (traced) | PARTIAL (traced) | NO | YES (traced) | YES (traced) | YES (traced) |
| turn complete | YES (traced) | YES (traced) | PARTIAL (traced) | YES | YES (traced) | YES (traced) | YES (traced) |
| cancellation | YES (traced) | YES (traced) | NO (confirmed) | PARTIAL | PARTIAL | YES (traced) | YES (traced), not consumed |
| session identity | YES (traced) | YES (traced) | YES (traced) | YES | YES (traced) | YES, provider side (traced); refused by Sidecar | YES (traced) |
| subagent | PARTIAL | YES | YES | NO | PARTIAL | PARTIAL | NO |
| process exit | YES (traced) | YES (traced) | YES (traced) | YES | YES (not consumed) | YES (traced), not hooked | NO (payload is empty) |

Claude Code's `unblocked` and `turn complete` are marked PARTIAL *after* tracing rather than before: both events exist and both fire on the ordinary path, and both go missing on exactly the paths where they would matter most. See the Claude Code section.

## OpenCode

**Source:** <https://opencode.ai/docs/plugins/>, cross-read against `packages/plugin/src/index.ts`. **Traced against 1.18.23 (Phase A) and 1.18.25 (Phase B cancellation) on darwin/arm64, 2026-08-30.** Traces: `internal/agentlifecycle/testdata/traces/opencode/`.

The observed sequence for a turn that used a tool and hit a permission prompt:

```
session.created  ->  chat.message  ->  session.status {"type":"busy"}
                 ->  tool.execute.before (bash)
                 ->  permission.asked  ->  permission.replied
                 ->  session.status {"type":"idle"}  ->  session.idle  ->  dispose
```

A failed turn produced `session.error` followed by `session.status {"type":"idle"}` and `session.idle`, which is the behavior that matters most: an error resolves to idle rather than latching the pane on `working`.

### Why OpenCode is the steel thread

`session.status` is state-shaped rather than transition-shaped. Every emission re-asserts ground truth as `{"type":"busy"}` or `{"type":"idle"}`, so a dropped or reordered event is corrected by the next one instead of leaving a pane stuck. That single property is the difference between an integration that stays true across a long agent run and one that needs a watchdog. It was confirmed by trace, not inferred from documentation.

It is also the only one of the four that emits an explicit unblock (`permission.replied`, `question.replied`, `question.rejected`) rather than requiring the resolution to be inferred, and `session.idle` is a positive readiness signal rather than a mere "stopped generating".

Installation is a single file dropped into a plugin directory with no edits to any existing user configuration file, so there is nothing Sidecar can corrupt on install or fail to restore on uninstall.

### Gaps found at runtime

These were discovered by tracing and are not in the documentation:

1. **The blocked lane is not state-shaped.** `session.status` only ever carried `busy` or `idle`. Blocking is visible solely through the transition-shaped `permission.asked` and `permission.replied` pair, so the self-healing property does **not** extend to the blocked lane. A dropped `permission.asked` will not correct itself.
2. **`tool.execute.after` never fired**, even though `tool.execute.before` did. Any mapping that depends on tool completion is dead on this version.
3. **Both plugin directories load.** `~/.config/opencode/plugin/` and `~/.config/opencode/plugins/` are both read, and an asset present in both fires every event twice. The installer must own exactly one path, and repair must detect the other.

3a. **Every export of a plugin module must be a plugin factory.** A single non-function export disqualifies the whole module — it is imported and then never called, silently, with no error printed anywhere. Measured on 1.18.25 with four probe plugins: a lone exported function loads; a function plus an exported string does not; a function plus an exported object does not; a function carrying helpers as properties loads. This is the most dangerous gap on the list because it is invisible to every offline test: the plugin installs cleanly, its own unit tests pass, and it reports nothing whatsoever. Sidecar's asset therefore exports exactly one symbol and hangs its testable mapping off that function, enforced by `TestTheAssetExportsOnlyPluginFactories`.

3b. **A hook is a direct child of the pane's root process when tmux launches the agent as the pane command**, which is what a Sidecar-managed agent shell does. Any "walk up to the process below the pane root" rule then resolves to the hook itself, giving every report a different process generation and therefore a different run. Sidecar's ancestry walk special-cases this; see `providerGeneration`.
4. **Cancellation has no dedicated event, and shares its shape with failure.** A user interrupt is observable, but only as `session.error` carrying `error.name = "MessageAbortedError"`, immediately followed by `session.status {"type":"idle"}` and `session.idle`. A provider failure emits the *identical* sequence with a different error name. An adapter that reads only event types can therefore get the lane right and the terminal outcome wrong.

### Cancellation, traced (Phase B entry condition)

Phase A left cancellation untraced, which held OpenCode at `advisory`. It is now traced, on 1.18.25, and the observed sequence is:

```
session.status {"type":"busy"}  ->  ...work...
                               ->  session.error  (error.name = MessageAbortedError)
                               ->  session.status {"type":"idle"}  ->  session.idle
```

The contrast run — the same harness with no credentials for the selected model — produced byte-for-byte the same event *shape* with `error.name = ProviderAuthError`. The bounded error class name is therefore the only thing separating a cancelled turn from a failed one, and it is the discriminator the provider handler must read. Both traces are checked in: `cancelled-turn.tsv` and `provider-error-named.tsv`.

Two practical notes from capturing it. Interrupting a busy turn in the OpenCode TUI takes **two** Escape presses; the first only arms the confirmation, and the footer changes from `esc interrupt` to `esc again to interrupt`. A single Escape is a no-op, which is easy to mistake for "cancellation is not observable". And the abort resolves on the bus a few seconds after the TUI stops showing the busy indicator, so a test that tears the session down as soon as the screen settles will miss the events entirely.

The lane itself is safe even for an adapter that ignores the error name, because `session.status` is state-shaped and re-asserts `idle`. Only the `Outcome` on the end report depends on reading it.

With cancellation covered, OpenCode's evidence now satisfies every entry in `FullLifecycleTransitions()` and `TierFor` derives `full` for it. `TestOpenCodeEarnsFullOnlyFromTheCancellationEvidence` asserts the converse: strip `cancelled` from the covered set and the entry falls back to `advisory`.

## Codex

**Source:** the published config reference redirects and truncates before the hooks section, so the authoritative contract is the upstream `HookEventName` schema and the hook JSON schemas embedded in the shipped binary. **Traced against 0.151.0 on darwin/arm64, 2026-08-30.** Traces: `internal/agentlifecycle/testdata/traces/codex/`.

Codex had the best-shaped event set of the four on paper, and tracing confirms it. Twelve events close the loop, every one carries `session_id` and, from `UserPromptSubmit` onward, `turn_id`, and it is the only provider with a first-class `Interrupt` event — precisely the transition Claude Code cannot express.

The observed sequence for a turn that used a tool:

```
SessionStart  ->  UserPromptSubmit  ->  PreToolUse  ->  PostToolUse  ->  Stop
```

and for one that hit a permission prompt and was approved:

```
UserPromptSubmit  ->  PreToolUse  ->  PermissionRequest  ->  [blocked]  ->  PostToolUse  ->  Stop
```

**Every full-lifecycle transition is now traced**, so the ceiling for a Codex adapter is `full`. What ships is still `SessionStart` alone, so the tier is still `session-identity`.

### The finding that decides the blocked lane

Approval and denial do **not** converge on the same event. Denying a permission request emits `Interrupt` — not `PostToolUse`, and not `Stop`:

```
UserPromptSubmit  ->  PreToolUse  ->  PermissionRequest  ->  [blocked]  ->  Interrupt
```

`Interrupt` is therefore Codex's universal "this turn ended without completing" signal, covering both a mid-turn user interrupt and a refused request. That is what makes the blocked lane safe: it always resolves. An adapter that treated only `PostToolUse` as the unblock would unblock on every turn that never blocked and latch forever on the ones that did — which is exactly the failure that caps Claude Code below `full`.

### Gaps found at runtime

These came from tracing and appear in no documentation:

1. **`trusted_hash` is computed over the effective hook, not the declared one.** Codex clamps `SessionEnd` and `Interrupt` hook timeouts to 3s and prints a warning naming `hooks.json` on every start. A trust record pre-written for those events at Sidecar's canonical timeout of 10 does not match, and the user gets the interactive "Hooks need review" prompt on every launch. Proved by letting Codex write its own records and diffing them: the identity hashed with `timeout=3` matches byte-for-byte and `timeout=10` does not. **Any lifecycle adapter must declare `timeout` ≤ 3 on those two events.**
2. **`SessionEnd` does not fire under `codex exec` at all.** It was observed only in the interactive TUI. Process exit on a non-interactive run is detectable only through process liveness.
3. **Nothing re-asserts state.** Unlike OpenCode's `session.status`, every Codex event is transition-shaped. A dropped event does not self-correct, so a Codex adapter has no equivalent of the property that made OpenCode the steel thread and would need a bounded reconciliation against screen evidence for a long-running turn.
4. **`PermissionRequest` carries no `tool_use_id`** although `PreToolUse` does, so correlating a block with the specific tool call that caused it relies on turn ordering rather than an identifier.

Also confirmed, and good news for the existing installer: the reproduced `trusted_hash` algorithm is now live-verified across **eleven** distinct event names in a single run, rather than the one `session_start` vector Phase C pinned. Ten of twelve pre-written records were accepted silently; the two that were not are gap 1.

Herdr, having shipped a Codex integration through eight asset versions, deliberately removed its lifecycle hooks and now installs only `SessionStart`. Sidecar's traces do **not** reproduce a reason for that rollback: every transition such a hook set needs is observable on 0.151.0. It stays recorded as unexplained rather than refuted, because a contract that held in one capture session is not the same as one that holds across versions.

**What ships (asset version 1):** a single `SessionStart` entry in `~/.codex/hooks.json` invoking `sidecar agent report-session --kind codex --hook-stdin` (fixed argv, payload on stdin, no matcher key — Codex groups carry none), `features.hooks = true` in `config.toml`, and a `[hooks.state]` trust record. Codex only auto-runs a hook whose `trusted_hash` matches its normalized identity; an untrusted hook raises an interactive "Hooks need review" prompt (observed live on 0.151.0). Sidecar pre-writes the record with the algorithm reproduced from codex-rs (`hook_hash` + `version_for_toml`: sha256 over the key-sorted canonical JSON of `{"event_name","hooks":[normalized handler]}`) and verified byte-for-byte against a live 0.151.0 record; if the algorithm drifts on a future version, the failure mode is that visible one-time prompt. The trust key is positional (`<hooks.json path>:session_start:<group>:<hook>`), so edits that reorder `hooks.json` invalidate trust records for every hook that shifted. The tier is `session-identity` because that is all the shipped hook exercises — the wider event vocabulary stays unclaimed until Phase D traces it.

## Claude Code

**Source:** <https://code.claude.com/docs/en/hooks>. **Traced against 2.1.220 on darwin/arm64, 2026-08-30.** Traces: `internal/agentlifecycle/testdata/traces/claude/`. Hooks were injected with the additive `--settings` flag, so nothing in the user's `~/.claude` was written.

Claude Code has by far the richest event surface, including the best subagent model of the four, and the largest user base. On the ordinary path it is excellent:

```
SessionStart  ->  UserPromptSubmit  ->  PreToolUse  ->  PostToolUse  ->  Stop  ->  SessionEnd
```

Tracing corrected the previous docs-only reading in one direction and confirmed the disqualifying gap in the other. **Claude Code's ceiling is `advisory`.**

### What tracing corrected

`PermissionRequest` **does** fire, and so does a `Notification`. The earlier entry recorded Claude as offering session identity and nothing else; blocking is in fact first-class on the current release:

```
UserPromptSubmit  ->  PreToolUse  ->  PermissionRequest  ->  Notification  ->  [blocked]
```

### The two findings that cap it at advisory

1. **`PostToolUse` is silently skipped for any tool that went through the permission prompt.** The same tool with no prompt fires it normally. This is worse than a missing event: the event exists, works, and disappears on exactly the turns where it would carry information. An adapter that unblocks on `PostToolUse` unblocks only on turns that never blocked.
2. **No user-cancellation event exists at all.** Interrupting a live turn with Escape produced *no hook event whatsoever* — not `Stop`, and not any of the speculative event names Claude accepted into its settings without complaint. Denying a permission request, or Escape-cancelling the prompt, is equally silent. Because `Stop` does not fire on those paths either, a state machine driven only by these hooks latches on `working` or `blocked` indefinitely.

Finding 2 is a contract gap rather than a tracing gap, so no amount of further tracing can close it. It is the reason Claude Code cannot reach `full` however carefully an adapter is written, and it is why an `advisory` Claude adapter must let screen detection speak: advisory is not a consolation prize here, it is a precise description of an integration whose events are true when they arrive and absent exactly when the user needs the pane to stop saying "working".

`Stop` also means "stopped generating", not "ready for input", so completion cannot be taken from it without reconciliation against screen state. And hook configuration merges additively across five layers, so Sidecar can add entries but can never own the effective hook set.

Herdr shipped a full-lifecycle Claude hook set and then removed it, keeping only `SessionStart`, citing missed permission results and escape interrupts. **Both halves of that are now independently reproduced on the current release**, so the rollback is explained rather than merely cited — and Sidecar reaches the same conclusion from its own evidence rather than by deference.

**What ships (asset version 1):** a single `SessionStart` group in `~/.claude/settings.json` — matcher `"*"`, one entry invoking `sidecar agent report-session --kind claude --hook-stdin` (fixed argv, payload on stdin). The installer owns exactly that entry, identified by its command being an invocation of Sidecar's own report-session verb; every other setting and hook in the file is preserved token-for-token and in order, and uninstall removes only the entry and any container it alone occupied. The tier is `session-identity` because that is all the shipped entry exercises.

## Pi

**Source:** the released TypeScript definitions in the installed package (`dist/core/extensions/types.d.ts`), read rather than inferred from prose. **Not traced.**

Pi has the cleanest agent-flow abstraction of the four. `before_agent_start`/`agent_start`/`agent_end`/`agent_settled`, `turn_start`/`turn_end`, the `tool_execution_*` family, and `session_start`/`session_shutdown` are all typed events with a documented loading contract (`--extension`/`-e`, repeatable; `--no-extensions`; discovery from `~/.pi/agent/extensions/`). Session identity is solid: `ctx.sessionManager.getSessionId()`.

**Correction to the Phase A entry.** That entry cited `ui_prompt_start` as partial cover for blocking. **No such event exists anywhere in the 0.84.3 package.** The blocked lane is `NO`, not `PARTIAL`, and the correction matters more than a table cell: it is the difference between a gap that tracing might close and one that cannot be closed at all.

**Blocking is structurally impossible, not merely absent.** Pi deliberately ships no permission system, so there is nothing to be blocked on. An extension may open its own `ctx.ui` dialog, but that is invisible to every other extension, so no portable blocked signal exists for Sidecar to consume. **Pi's ceiling is therefore `advisory`**, and no amount of tracing will raise it.

Three further limits are worth recording because each is a trap for an adapter written from the event names alone. Turn completion must come from `agent_settled` rather than `agent_end`, because `agent_end` can be followed by an automatic retry or a compaction — it means "this attempt stopped", not "the turn is over". Process exit is `session_shutdown`, which fires for `quit`, `reload`, `new`, `resume` and `fork`, so three of its five reasons are not an exit at all and an adapter must read the reason. And cancellation has no event: it is inferable from a `StopReason` of `"aborted"` on the final assistant message, with `agent_settled` emitted from a `finally` so it arrives however the turn ended — which is exactly the shape OpenCode's cancellation had before it was traced, sharing its shape with `"error"`.

Two more limits belong with those three. **There are no subagent events**: an agent an extension spawns is a separate process, so nothing in Pi's event stream describes it and a parent's lane can never be derived from a child's. And **the extension API publishes no version or stability guarantee**, while the changelog records breaking changes that have already shipped, so an adapter written against 0.84.3 has no contractual reason to keep working on 0.85. The retracted entry recorded `minProviderVersion: 0.84.0`, which is the floor a future entry should carry forward rather than re-derive.

### Why the Pi entry was retracted, and what brought it back

`capabilities.json` used to carry Pi at `session-identity` with source `sidecar.pi.extension` and `assetVersion: "0"`. That zero was the tell. There is no `PiAdapter`, no `internal/agentintegration/assets/pi/`, and no code path anywhere in Sidecar that installs something Pi would load, so no report could ever arrive carrying that source. A tier granted to a source nothing can produce is not a weak claim, it is an empty one, and the matrix is only worth checking in because it never claims more than it has proved. The entry was recorded ahead of the port, and it came out; `internal/agentsession/trust.go` dropped the same source id with it, because an official source is what marks a reference resumable and the only hook that could have used one here is a hook Sidecar did not write.

Pi was not filed under "evaluated but not built" below either. That table means "surveyed, and deliberately not built", which was false: Pi is the first port in [Close the Herdr parity gap](../plans/active/herdr-parity-close-the-gap.md), Slice 1.

That port has now landed, and so have its traces. `PiAdapter` installs `sidecar-lifecycle.js` into `$PI_CODING_AGENT_DIR/extensions` (`~/.pi/agent/extensions` by default), the asset's provider half is Herdr's version 8 mapping kept verbatim in behavior, and the entry went to `session-identity` on `docs-only` evidence — the honest floor for a source with an installer and no traces — and then to `advisory` on `real-trace` evidence once a live Pi 0.84.3 was captured. `TestHerdrAuthorityGaps` prints `pi ... sidecar=advisory`, which is Herdr's target for this provider reached rather than approached.

What the entry claims is still deliberately narrow, and the list of what it does not claim did not shrink when the tier went up. `session_identity`, `work_start` and `turn_complete` are covered, and nothing else. `tool_use` is not, because the asset subscribes to no tool event — the traces show `tool_execution_start` and `tool_execution_end` firing with a tool name, so this is unclaimed by choice rather than by absence. `process_exit` is not, because the asset does not subscribe to `session_shutdown` at all: three of its five reasons are a session swap rather than an exit. The trace shows that event's reason IS readable and was `quit`, so a future asset that released the lane only on that reason could claim it. `cancelled` is not, because an interrupted turn produces byte-identical events and field names to a completed one. `blocked_on_request` and `unblocked` are not, and never will be.

### What the Pi traces measured that reading the types could not

Four captures are in `internal/agentlifecycle/testdata/traces/pi/`, taken 2026-09-02 from a live Pi TUI in a Sidecar-managed shell on a private tmux server. Six tests in `hooktrace_test.go` re-derive each claim below from the fixture that earned it.

- **The `agent_end` trap is mechanical, not theoretical.** `ctx.isIdle` is `false` on `agent_end` and `true` on `agent_settled` three milliseconds later. An asset that closed a turn on `agent_end` would report idle while Pi still considers the run active. The shipped asset's `isIdle !== true` guard is what this measures.
- **`turn_end` is not turn completion either.** A single agent run that calls a tool emits `turn_start`/`turn_end` twice, once around the tool call and once around the reply. A "turn" in Pi's vocabulary is a provider round trip. `agent_settled` emits once, at the end.
- **The blocked lane is unreachable, and the capture narrows what was already read from the types.** A `bash` tool ran unconditionally with no permission, approval or prompt event anywhere between `before_agent_start` and `agent_settled`. What the capture does not show is anything about Pi's untyped event bus: the tracer listened on both `sidecar:blocked` and `herdr:blocked` and neither fired, but no column in a trace distinguishes that from never having subscribed, so the claim rests on the emitter-side reading of Pi's shipped bundle and Herdr's tree rather than on this file.
- **A failed turn resolves rather than latching.** A provider 404 took the same `agent_start` → `agent_settled` path as a success, which is the failure mode the resolver has to survive.

### What the live proof showed about the lanes themselves

Two findings that belong to the arbitration machinery rather than to Pi, recorded here because the next four ports will meet both.

**Uninstalling an integration does not, by itself, hand the lane back to the screen.** `StoreSource.Evidence` derives `integrationStatus` from the *record* — source known, asset version matching — never from the installed files. So `sidecar agent integration status pi` reports `not-installed`/`screen-fallback` while `sidecar agent explain` still reports `integrationStatus=current`/`authority=lifecycle` from reports already in the store. Restarting the provider flips it: `authority=screen`, `fallbackReason=process_generation_mismatch`. That is arbitration working as designed — a stored claim checked against the live world — but a fallback proof has to end the run, not just remove the file.

**`sidecar agent start --kind pi` times out, and no tier can fix it.** The refusal is `pi.process-mismatch`: Pi installs as a `#!/usr/bin/env node` shim, tmux reports the pane's foreground command as `node`, and `DetectPi` refuses before any manifest rule runs. The screen lane by itself answers `idle` for the same capture (`sidecar agent explain --file` gives `fallback_reason=default_known_agent_idle_fallback`), so widening the process gate is the whole fix. `agentcontrol`'s detector calls `agentactivity.Detect` only and never consults the lifecycle store, so hooks authority is not on that path at all. Tracked as Slice 3 of the parity plan.

## Kilo Code

**Source:** the shipped `@kilocode/cli` 7.5.9 binary, read rather than inferred from prose, and four sanitized captures of that version in `internal/agentlifecycle/testdata/traces/kilo/`. **Traced.**

Kilo is an OpenCode fork, so its plugin surface is OpenCode's: a module export that is a function is called as a plugin factory, the object it returns is a bag of hooks, and `chat.message` plus a bus `event` hook are the two that carry lifecycle. Everything below was measured against 7.5.9, because Herdr's kilo integration is at version 4 and several of its assumptions no longer hold.

### The one upstream bug the port fixes

Kilo's `session.status` event carries an **object** whose `type` is the discriminator. The shipped schema is a union of `{type:"idle"}`, `{type:"busy"}`, `{type:"retry",...}` and `{type:"offline",...}`. Herdr's kilo asset accepts a status only when `typeof status === "string"`, so on this release it maps none of them and falls through to re-reporting the session on every status event.

Herdr's own **opencode** asset at version 10 reads `status?.type` and has done since before this port; the kilo variant never received the fix, in the same way the pi variant never received omp's Windows-path fix. Sidecar takes the fixed form, and the reason it matters is not tidiness: `session.status` is the only **state-shaped** signal Kilo has, so an asset that cannot read it gives up the property that lets a dropped or misordered event self-correct. `traces/kilo/error-turn.tsv` shows the concrete cost, below.

### Two branches that can never fire, and one that fires oddly

`tool.execute.before` and `tool.execute.after` are plugin **hooks** in Kilo, invoked through `Plugin.trigger`, not bus events. An `event` handler never sees them. `traces/kilo/tool-turn.tsv` is a turn in which a bash tool really ran, with the command and its output in the run log, and neither name appears anywhere between `chat.message` and `session.idle`. Herdr lists both in its event switch in its kilo and its opencode assets alike, so the branches are upstream's reading and are kept, and `tool_use` is not claimed on the strength of them.

`session.error` maps to **blocked** upstream, without reading the error name, and the port keeps that. It reads oddly and it is safe: `traces/kilo/error-turn.tsv` shows `session.status` idle arriving one millisecond after the error, so the lane an error opens is closed by the next state-shaped assertion rather than latching. That trace is also the argument for the status fix, because with upstream's string-only read nothing would close it until `session.idle`.

`retry` is deliberately absent from the status vocabulary, which is upstream's list kept verbatim. A `{type:"retry"}` status re-asserts the binding rather than a lane, which is harmless because a retry happens inside a turn a busy assertion already opened. Herdr's opencode asset does map retry to working, so this is a place where the kilo asset is behind its sibling and the port chose fidelity over improvement.

### Why `advisory` is the ceiling

`full` needs `cancelled` and `process_exit`, and this asset can produce neither. A user interrupt reaches the bus as `session.error` carrying `error.name = MessageAbortedError`, the same shape a provider failure takes with a different name, and upstream's asset does not read the name; and upstream has never had a `dispose` hook, so nothing releases the lane on teardown. Kilo does call `dispose`, and Sidecar's own OpenCode asset uses it, so both are differences between the two ports rather than limitations of the provider. Both move together with `covered` if a future asset version subscribes.

### Where Kilo reads plugins, and the trap it inherits

The global config directory is `$XDG_CONFIG_HOME/kilo`, falling back to `~/.config/kilo`, with `KILO_CONFIG_DIR` as the override. Herdr hardcodes `~/.config/kilo` and honours no override at all. Kilo also loads `~/.kilo/`, `~/.kilocode/`, and project-local `.kilo/` and `.kilocode/` walking up from the working directory; Sidecar installs into none of them.

Kilo globs `{plugin,plugins}/*.{ts,js}` in **every** config directory it discovers, so the OpenCode double-load trap applies here too: a copy in each directory fires every event twice. Measured with two probe plugins, one per directory, both of which loaded and ran. The installer owns `plugin/` and reports anything with its asset's name in `plugins/` as `needs-repair`.

One place Kilo is **laxer** than the OpenCode it forked from: a non-function export does not disqualify a plugin module. Kilo's loader walks the namespace and skips an export it cannot call, so a factory beside a string export still runs; OpenCode 1.18.25 drops the whole module in that case, silently. Sidecar's asset holds to the stricter convention anyway, because the cost is zero and relying on a fork staying laxer than its upstream is a bet nobody promised to keep.

### What the live proof showed

Sidecar's asset was installed by `sidecar agent integration install kilo` rather than by hand, and a Kilo TUI ran in a Sidecar-managed shell on a private tmux server with an isolated Sidecar config and state tree. The store recorded `report-session --kind kilo --id ...` and then `working`/`turn_start` and `idle`/`turn_complete`, all accepted, and `sidecar agent explain --shell` read `state=idle authority=lifecycle tier=advisory source=sidecar.kilo.plugin integrationStatus=current` with `screenState=unknown` throughout: the verdict was authored by hooks and by nothing else. The binding landed in `shells.json` as `reported: true`, which is what makes the reference auto-resumable. Uninstalling and ending the run flipped the lane to `authority=screen`, `fallbackReason=process_generation_mismatch`, exactly as the Pi proof recorded that it must.

What it did **not** establish is worth stating, because the summary invites the assumption. `screenEvidence` was `unsupported-agent` throughout, so the screen lane never answered and the hooks lane was the only lane. The binding was accepted, but `CheckReportedKind` refuses only a positively identified occupant of a *different* kind and passes an unidentified one, so exit 0 does not show that process identity named the pane `kilo`. Kilo installs as a `#!/usr/bin/env node` shim, which is the case Slice 3's process-tree scoring exists for; whether the widened resolver names it is a separate measurement this run did not make.

Two things the proof found that no offline test could:

- **`agent report-session --kind kilo` was refused on every turn.** `resolveReportedKind` resolved a `--kind` claim through `agentcatalog.Lookup`, which searches the launchable families and their aliases only. Kilo is detection-only today, so every binding failed with exit 5 while the state reports, which take no `--kind`, were accepted. Every test in the tree passed a launchable id, which is why nothing caught it. The lookup now falls back to the detection catalog, which is the right rule for a verb that answers a question about a pane rather than about a launch.
- **`KILO_CONFIG_DIR` is not enough to isolate a proof run.** Kilo still creates its XDG default config directory at startup whatever that variable says, and this run wrote one zero-byte migration marker into the maintainer's real `~/.config/kilo` before the mistake was caught. The file was removed and the directory restored. Move `XDG_CONFIG_HOME` as well.

## Kimi Code

**Source:** Kimi Code CLI's own published hooks reference and configuration reference, then traced against kimi-code 0.40.1. The event-to-lane mapping is ported from Herdr's kimi integration at `HERDR_INTEGRATION_VERSION=7`.

Kimi has the broadest hook surface Sidecar has integrated with: twenty events, configured as a `[[hooks]]` array of tables in `~/.kimi-code/config.toml` (or `$KIMI_CODE_HOME/config.toml`), each entry taking exactly `event`, `matcher`, `command` and `timeout` — any extra field makes the whole configuration fail to load. The payload arrives on stdin carrying `hook_event_name`, `session_id`, `session_title`, `client_type` and `cwd`, plus per-event fields. Exit codes fail open: 0 allows, 2 blocks on a blockable event, anything else allows.

**The port ships twelve of the twenty events**, which is upstream's table row for row. Sidecar's asset is not a script: Herdr's is a shell shim whose only jobs are gating on `HERDR_ENV`, shelling out to `python3` for `session_id`, and writing a JSON-RPC frame to a socket. Every one of those disappears when the transport is a CLI verb — `sidecar agent report` is itself the gate, and `report-session --hook-stdin` reads the payload with a bounded reader — so the twelve config entries invoke the CLI directly, exactly as the Claude and Codex adapters already do against the same upstream shape.

### The finding that makes the blocked lane claimable

**One event resolves a block, whichever way it goes.** `PermissionResult` fires on approval and on denial and carries the outcome in a `decision` field. That is the contrast the whole matrix is for: Claude Code caps below `full` because a denied permission emits *nothing* and a hook-driven pane latches on blocked forever; Codex escapes the same trap only because denial takes a *different* event (`Interrupt`) from approval, which an adapter has to know about in advance. Kimi needs neither workaround, so upstream's single unblocking row is sufficient and the lane is claimed.

There is a second blocked path with its own resolver pair, and it is why upstream's table has four `AskUserQuestion` rows rather than one. A `PreToolUse` on the `AskUserQuestion` tool means the model is asking the human something, so it blocks; `PostToolUse` and `PostToolUseFailure` on the same tool both unblock, because a question that ended without an answer is still a question that is over. Sidecar records that as `question` and `permission_resolved` rather than reusing the permission codes, because Kimi has a separate `PermissionRequest` and the vocabulary distinguishes them.

### One hook per event, by construction

Kimi runs **every** hook matching an event in parallel. Two rows matching one event would be two `sidecar agent report` processes racing for a store sequence, and which lane the pane ended in would depend on the scheduler — a defect invisible in every offline test and intermittent in the field. Upstream's table avoids it by construction: the only doubled event is `PreToolUse`, and its two matchers are exact complements (`^AskUserQuestion$` and `^(?!AskUserQuestion$).*$`). `TestKimiFiresExactlyOneHookPerEvent` pins the property so a row added later without a complementary matcher fails rather than shipping.

The negative lookahead is a provider-side regular expression, and Go's own `regexp` is RE2 and cannot even parse it — which is worth knowing, because it means no Sidecar test can evaluate the shipped matcher directly. What the live proof establishes is that Kimi *loads* it: `kimi doctor` reported the config Sidecar wrote as valid, and one Bash `PreToolUse` produced exactly one working report. What is still untraced is the other side of the complement, because no captured turn used the `AskUserQuestion` tool.

### What the traces measured, and the two gaps that cap it at advisory

Four captures are in `internal/agentlifecycle/testdata/traces/kimi/`, taken 2026-09-03 from a live Kimi TUI in a Sidecar-managed shell on a private tmux server, with the provider installed into a scratch prefix and its whole data directory redirected with `KIMI_CODE_HOME`. Five tests in `hooktrace_test.go` re-derive each claim from the fixture that earned it.

`work_start`, `tool_use`, `blocked_on_request`, `unblocked`, `turn_complete` and `cancelled` are covered. Cancellation is first-class: `Interrupt` fires carrying `reason=cancelled`, no `Stop` follows it, and upstream's `Interrupt`-to-idle row is therefore load-bearing rather than redundant. That is five of the seven transitions `FullLifecycleTransitions()` names, plus `tool_use`, which the covered list carries but that function does not name. The two it does name and this port does not reach are why the tier is `advisory` rather than `full`:

- **`session_identity` is refused by Sidecar, not by Kimi.** `SessionStart` carries a `session_id` and the shipped table has a row passing it to `sidecar agent report-session --hook-stdin`. That command canonicalises `--kind` through `agentcatalog.Lookup`, which searches the launchable families and their aliases only; `kimi` is a *detection-only* family, so the lookup fails. Measured live: exit 5, `unsupported_kind: "kimi" is not an agent kind Sidecar knows`. State reports are unaffected, because `agent report` checks `--provider` against the pane's resolved occupant instead. The row ships anyway — it is correct and inert rather than wrong — and it starts working the moment `kimi` becomes a family the catalog resolves, which is Slice 5 of the parity plan.
- **`process_exit` is unclaimed by choice.** `SessionEnd` fires and carries `reason=exit`, captured by typing `/quit`. Upstream's twelve rows do not include it and this port keeps the provider half verbatim, so the gap and `covered` move together if a future version subscribes.

Two further limits belong here. **The blocked lane is reachable only from an interactive session**: under `kimi -p` the same Bash call runs with no permission pair at all between `PreToolUse` and `PostToolUse`, because there is nobody to ask. And **sub-agent isolation is unknown**: `SubagentStart` maps to working and there is no `SubagentStop` row, so only the turn's own `Stop` returns the pane to idle, and no captured turn spawned a sub-agent. `PermissionRequest` does carry an `agent_id`, which suggests they are separable, but that is a field name in a fixture rather than a measurement.

### What the live proof showed about ownership

The installer owns one region of `config.toml`, delimited by two marker comments carrying the shared `sidecar-integration:` sentinel, and the proof ran it against a file that already held twenty hooks of the user's own. Install appended; uninstall left the file **byte-identical** to what it started from.

One hole in the ownership rule is worth recording because it is a real limit rather than a bug. Ownership reads the first word of a hook's command, so a copy of Sidecar's own command placed outside the managed block is detected and every mutation refuses. A hook that *wraps* that command in a script of the user's is not: a wrapper is not the `sidecar` binary. That fails in the documented direction — their entry is never adopted or deleted — but such a duplicate reports alongside Sidecar's own and is invisible to `integration status`. It was observed during the proof run, where a debug wrapper produced a second `idle` report for one `Stop`.

## OMP (oh-my-pi)

**Source:** OMP 18.1.8's own shipped TypeScript, read from the installed package rather than from documentation, then traced against that version. The event-to-lane mapping is ported from Herdr's omp integration at `HERDR_INTEGRATION_VERSION=9`.

OMP is a rebranded fork of Pi's codebase and its extension API is Pi's: a bare `.ts` or `.js` file in `<agent dir>/extensions`, a default export that is a factory taking the host object, a typed `on(event, handler)` registry and an untyped `events` bus beside it. Sidecar's asset is a `.js` file for the same reason Pi's is: the harness that keeps the shipped JavaScript and its Go mirror from drifting runs the asset under `node`, and `node` cannot import a `.ts` module. OMP itself runs on Bun and loads either.

The family resemblance is where the usefulness of this section ends. **Four differences decide the port, and each was measured rather than assumed.**

### OMP has no `agent_settled`, and Pi's guard is actively wrong here

Pi closes a turn on `agent_settled` and discards a settlement seen while `isIdle()` is not true. OMP's registry has no such event: a run ends on `agent_end` and the distinction Pi carries in a second event is carried in fields instead. Porting Pi's rule across would fail twice over, and the traces show both halves: `ctx.isIdle` is **false** on the `agent_end` of a turn that completed normally, and **true** on the `agent_end` of a cancelled one. So the guard would refuse every real settlement and accept every cancelled one.

What replaces it is upstream's three guards, kept verbatim. An `agent_end` arriving while the handler already believes the run inactive is ignored, because OMP emits duplicate and late end events while auto-retry is holding the pane. An `agent_end` carrying `willContinue === true` is ignored, because a continuation is already scheduled — the comparison is against an explicit `true` rather than key presence, and the traces show why: the key is on every payload and its value is *absent* on an ordinary end. And the idle that survives both guards is published only after a **250ms debounce**, so a run immediately followed by another does not flicker the pane through idle.

That debounce is the only clock in any Sidecar asset, and it is why this asset's pure mapping emits `schedule` and `cancel` actions instead of calling `setTimeout` itself. A timer that fires re-enters the mapping as an ordinary event, which is what lets one fixture drive the debounce through both the shipped JavaScript and the Go mirror and compare them action for action.

### The blocked lane is first-class, and a denial is the same event as an approval

This is the sharpest reversal from Pi, whose blocked lane is structurally unreachable because Pi ships no permission system at all. OMP has `tool_approval_requested` and `tool_approval_resolved`, typed events carrying a `toolName` and an `approvalMode`, and `tool_approval_resolved` fires on **approval and denial alike**, carrying the outcome in an `approved` boolean.

That is the contrast this matrix exists for. Claude Code caps below `full` because a denied permission emits nothing and a hook-driven pane latches on blocked forever; Codex escapes the same trap only because denial takes a *different* event from approval. OMP needs neither workaround, so upstream's single unblocking row clears the lane either way, and `unblocked` is claimed on evidence from both directions.

There is a second blocked path: the `ask` tool. `tool_execution_start` for `toolName === "ask"` means the model is asking the human something, so it blocks, and `tool_execution_end` on the same tool unblocks. Sidecar records that as `question` rather than `permission_request`, because the frozen reason vocabulary distinguishes them. **It is not traced**: no turn in the capture run called that tool, so the branch is driven by fixtures only.

One ordering fact decides why every other tool is ignored. `tool_execution_start` fires **before** `tool_approval_requested`, in the same millisecond. An asset that treated it as work would publish `working` one event before the pane actually blocked.

### OMP retries provider errors by itself, so a failure is held at `working` first

A failed run whose last assistant message carries `stopReason: "error"` is classified against a fixed pattern of retryable provider strings — overload, rate limit, 5xx, socket and timeout shapes. A match holds the pane at `working` for a **2500ms grace** and arms a timer; only a failure still outstanding when that timer fires is published as `blocked` with reason `provider_error`. The pattern is kept character for character, because it is a record of which errors OMP's own retry path recovers from and narrowing it would make Sidecar announce a block OMP is about to clear. Go's RE2 accepts it unchanged, so the Go mirror runs the same expression rather than an approximation of it. **This lane is not traced**: no captured turn hit a retryable error.

`auto_retry_start` and `auto_retry_end` exist and look like a cleaner source for the same signal. They are deliberately not used, because the provider half is upstream's and upstream does not use them. That is the obvious next version of this asset, recorded rather than guessed at.

### The gate is `ctx.hasUI`, and here that is correct

Sidecar's Pi asset gates on `ctx.mode` and says so at length, because an RPC Pi session reports `hasUI` true while being headless. OMP computes `hasUI` as `isInteractive || mode === "rpc-ui"` (`src/main.ts:1830`), so print, json and plain rpc are already false and the reason for preferring `mode` does not apply. Upstream's OMP asset uses `hasUI`; this port keeps it. The gate is re-checked on every handler that can adopt a session, so a headless invocation can never latch.

### Why `advisory` is the ceiling, and how the two gaps differ from Pi's

Five of the seven transitions `FullLifecycleTransitions()` names are covered: work start, blocked on request, unblocked, turn complete, and session identity. The two that are not are each interesting for a different reason.

- **`cancelled` is observable and is deliberately not read.** The last assistant message on a cancelled `agent_end` carries `stopReason=aborted` where a completed one carries `stopReason=stop`. Pi's cancelled and completed turns are byte-identical, so `cancelled` is *unknowable* there; here the discriminator exists and upstream's mapping simply does not consult it, reading `stopReason` only to classify a retryable error. The port keeps the provider half verbatim, so the transition stays unclaimed — a concrete next-version item rather than an unknown.
- **`process_exit` is unclaimABLE.** OMP's `SessionShutdownEvent` is `{type}` and nothing else, confirmed from the type and from the capture. Pi's carries a reason with five values, three of which are a session swap rather than an exit, so a future Pi asset could subscribe and release only on `quit`; nothing in OMP's payload distinguishes a quit from a swap, so no future version of this asset can claim it from this event alone. The asset subscribes only to cancel its pending timers.

Two smaller absences. `tool_use` is unclaimed by choice: both tool events fire for every tool and carry a `toolName`, and tool use is a refinement of `work_start` rather than a separate lane. There are no subagent events at all.

### Where OMP reads extensions, and the collision that has its own refusal

The user-level directory is `<agent dir>/extensions`, and the agent directory is where OMP differs from Pi in three ways that each produce a wrong install if missed. `PI_CONFIG_DIR` overrides the **name** of the config directory under `$HOME`, defaulting to `.omp` — it is not a path, and it is not a Pi variable at all. A named profile (`OMP_PROFILE`, or `PI_PROFILE` when `OMP_PROFILE` is unset) inserts `/profiles/<name>` **and makes OMP ignore `PI_CODING_AGENT_DIR` entirely**. And `PI_CODING_AGENT_DIR` is `path.resolve`d rather than tilde-expanded, so a relative or `~`-prefixed value binds to whatever directory OMP was launched from; Sidecar cannot know that directory and refuses with that reason rather than guessing at one. Herdr's `omp_extension_dir` tilde-expands the override and knows nothing about profiles.

`PI_CODING_AGENT_DIR` is the collision. It is Pi's variable and OMP reads it too, so with it set the two agents resolve to **one** extensions directory and every extension in it is loaded by both binaries. Sidecar would then be reporting one provider's lane from the other's pane, and `agent report` verifies `--provider` against the pane's occupant, so one of the two would be refused on every single event. Sidecar refuses to install into that state with a reason naming both sides, which is the refusal Herdr's `install_omp` makes, and its asset carries a distinct filename (`sidecar-omp-lifecycle.js`) so the two were never going to occupy one path. The residual is one-sided and is stated rather than hidden: installing OMP into a directory Pi already shares is refused, but setting the variable *after* installing OMP and then installing Pi is not, because that check would live in the Pi adapter. `sidecar agent integration status omp` reports the collision from either direction.

Herdr's `remove_legacy_pi_extension_from_omp_dir` is deliberately not copied. It deletes a file out of the OMP directory on the strength of a marker declaring `HERDR_INTEGRATION_ID=pi` — Herdr cleaning up a mistake of its own making. Sidecar has never installed a Pi asset into an OMP directory, and removing a file on the strength of a marker that is not Sidecar's own would break the ownership rule outright.

### What the live proof showed

Three captures are in `internal/agentlifecycle/testdata/traces/omp/`, taken 2026-09-04 from a live OMP TUI in a Sidecar-managed shell on a private tmux server, with `HOME`, every XDG directory, `-config` and `SIDECAR_ISOLATED_STATE=1` under a scratch tree; the CLI was installed into a scratch npm prefix and never onto the maintainer's `PATH`, and no `~/.omp` was created in their home. Eight tests in `hooktrace_test.go` re-derive each claim from the fixture that earned it.

Sidecar's asset was installed by `sidecar agent integration install omp` rather than by hand, so the run proves the installer. The store recorded, from source `sidecar.omp.extension` with store-assigned sequences 1 through 11 and no gaps: `idle(session_start)` → `working(turn_start)` → `idle(turn_complete)`, then a tool turn `working(turn_start)` → `blocked(permission_request)` → `working(permission_resolved)` → `idle(turn_complete)`, then the same block-and-unblock again on a **denial**. `agent explain --shell` read `state=working authority=lifecycle tier=advisory` while a turn ran and `state=idle` after it, with `screen=unknown` throughout: the verdict was authored by hooks and by nothing else. The session binding landed in `shells.json` by **path**, `reported: true`, which is what proves the approved store root for this provider is the directory the installer actually writes beside.

Uninstall removed only Sidecar's own file. A neighbouring extension in the same directory was left byte-identical and the directory itself was kept, because Sidecar did not empty it. Uninstalling alone did **not** flip the lane — stored reports keep authority while the run is alive, which is Slice 1's finding reproduced rather than assumed — and uninstalling *and* ending the run gave `authority=screen`, `fallbackReason=process_generation_mismatch`.

## Catalog agents evaluated but not built

These are recorded rather than omitted so that "evaluated, and deliberately not built" is distinguishable from "never looked at". All are `screen-fallback` with `evidence: none`: **none is trace-backed**, so each selects a candidate rather than earning a tier, and `TierFor` would refuse them anything else regardless.

| Agent | Seen | Hook surface | Ceiling on paper | Why it is not built |
| --- | --- | --- | --- | --- |
| grok | 1.0.13 | strongest in the catalog | `full` | Untraced. The only provider anywhere with a dedicated cancellation event: `StopCancelled` carrying `user_interrupt`, `permission_rejected` and `permission_cancelled`, alongside `PermissionDenied` and `Notification{permission_prompt, idle_prompt}`. |
| cursor | 2026.08.25 | full registry in the shipped bundle | `full` | Untraced, and there are user reports of events being omitted on particular versions, so tracing is mandatory rather than a formality. |
| copilot | not installed | GA hooks incl. `permissionRequest` | `full` | Not installed on any surveyed machine, so nothing is verified even against a shipped artifact — the weakest evidence here. Interrupt also appears to be session-granular rather than per turn. |
| amp | not installed | TypeScript plugin process | `advisory` | Not installed and not traced. No permission event and no reliable process-exit signal, so two lanes would stay with screen detection anyway. |
| antigravity | 1.1.22 | five events | `advisory` | Untraced. No session start/end, no blocking, no cancellation, and the hooks configuration path has already moved between releases. |

Two findings from this sweep are worth more than the table.

**No provider except OpenCode emits an approval-*resolved* event.** Codex, Claude Code, grok, cursor and antigravity all announce that permission is being requested and none announces the reply as its own event. Resolution has to be inferred from whatever happens next, and what happens next differs per provider — `PostToolUse` for Codex on approval, `Interrupt` on denial, and nothing at all for Claude Code on either. This is the single most consistent gap across the catalog, and it is the reason the `unblocked` transition is the one that separates the tiers in practice.

**grok reads `~/.claude/settings.json` by design.** Its shipped documentation carries a "Claude Code Compatibility" section for exactly this. Sidecar's installed Claude hook entry therefore also fires inside grok sessions, and because `--kind` is a flag rather than something checked against the pane's actual occupant, a grok session can be bound as `kind=claude` carrying grok's session identifier — after which a cold restore would offer to resume it with the wrong CLI. Tracked as `td-11040b`.

## What Phase B should do first

1. ~~Trace OpenCode cancellation.~~ **Done, 2026-08-30.** Cancellation is observable as `session.error` with `error.name = MessageAbortedError`; OpenCode is promoted to `full`. See "Cancellation, traced" above.
2. Confirm whether the blocked lane can be made self-correcting despite gap 1, or whether a bounded reconciliation against screen evidence is needed for that lane specifically.
3. Decide the single owned plugin path and make repair aware of the other, per gap 3.

## Requalifying a provider version

Authority belongs to a source at a version against a provider at a version, never to a provider name forever. This section is the procedure for the two events that can invalidate a recorded tier: **the provider ships a new version**, or **Sidecar changes a bundled asset**. Both end in the same place — evidence on disk that a test reads back — but they start differently.

### What actually goes stale

A recorded tier rests on three things, and each can rot on its own:

| What | Where it lives | How it goes stale |
| --- | --- | --- |
| The provider's event contract | `covered`, the traces, this document | The provider adds, removes, renames, or re-times an event. |
| The asset that consumes it | `assetVersion`, the bundled asset bytes | Sidecar changes what it installs. |
| The mechanism that makes the asset run | provider-specific: Codex's `trusted_hash`, OpenCode's plugin-loading rules | The provider changes a rule Sidecar reproduced rather than one it was given. |

The third is the dangerous one, because nothing in a Sidecar release notices it. Codex's trust hash is reproduced from provider source, not from a published contract; the Codex timeout-clamping finding above is exactly this category, and it was invisible until a live run showed a user-facing prompt no test would ever produce.

### When a provider ships a new version

1. **Check the range first.** `sidecar agent integration status PROVIDER` reports whether the installed provider version falls inside `testedProviderRange`. Outside it, the resolver already treats the source as unproved rather than trusting it — that is the safety net, not the answer.
2. **Re-trace the promotion-gate cases**, not just the happy path. The set is the one in `FullLifecycleTransitions()` plus the two that only appear under stress: a **denied or cancelled** permission request, and a **mid-turn user interrupt**. Every provider-specific finding recorded on this page came from one of those two, and none of them came from a turn that went well.
3. **Trace into an isolated configuration tree**, never the user's own. `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `XDG_CONFIG_HOME`, or an additive injection flag where the provider has one (`claude --settings`, `pi --extension`). Anything interactive runs on a private tmux socket. Capture event names and field *names* only.
4. **Update `capabilities.json` and this document together**, then check the sanitized traces in under `internal/agentlifecycle/testdata/traces/<provider>/`.
5. **Let the tests decide the tier.** Do not write a tier; write the evidence and let `Capability.TierFor` derive it. `TestCapabilityMatrixCannotClaimUnearnedAuthority` re-derives every entry from its own record, and a `real-trace` claim with no files on disk fails.

### When Sidecar changes an asset

1. Bump the asset version constant (`OpenCodeAssetVersion`, `CodexAssetVersion`, `ClaudeAssetVersion`, `PiAssetVersion`, `KiloAssetVersion`, `KimiAssetVersion`, `OmpAssetVersion`).
2. Append the superseded entry to that adapter's canonical history, so an installed copy of the old version reads as `outdated` rather than as damage.
3. Move `assetVersion` in `capabilities.json` to match.
4. Requalify against the traces — a new asset consuming the same events still needs to be shown to consume them correctly.
5. Update the golden checksum in `asset_golden_test.go`, which is the guard that makes steps 1–4 unskippable: changing the bytes without bumping the version fails the build.

The order matters. Updating the golden first turns the guard into a formality.

### What a requalification must be able to fail

A requalification that can only confirm is not one. These are the outcomes it must be able to reach, and each has a home:

- **A transition disappeared.** Remove it from `covered`; `TierFor` demotes the entry on its own. Add a gap saying what used to work.
- **A transition appeared.** Add it, and add a trace. A provider that starts emitting a cancellation event is the single most valuable thing this procedure can discover, and **only this procedure can discover it.** `TestClaudeCancellationEmitsNothingAtAll` reads a checked-in fixture, so it cannot notice a change in Claude's behavior; it fails only once a human has re-traced and edited the fixture, by which point that human already knows. It is a fixture-integrity guard — it stops the recorded absence from being edited away silently — not a tripwire on the provider. Nothing in CI will tell you this gap has closed. That is the reason this document specifies a *cadence* rather than relying on a test to raise its hand.
- **A reproduced mechanism drifted.** The failure is usually visible to the user rather than to a test, so record what the user would see. Codex's is a "Hooks need review" prompt; that is the right direction for a security control to fail, and the wrong thing to discover from a bug report.
- **Nothing changed.** Widen `testedProviderRange` and say which version was checked.

## Maintaining this document

When a provider version changes, or an integration is added, update `internal/agentlifecycle/capabilities.json` and this document together. The tests will reject a `real-trace` claim with no trace files, a `docs-only` claim with trace files present, an untraced entry claiming no known gaps, and any tier the entry's own coverage does not earn. Capture procedure and sanitization rules are in `internal/agentlifecycle/testdata/README.md`.

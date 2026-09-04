# Herdr parity: close the remaining gap

**Status:** In progress, opened 2026-09-02, scope settled 2026-09-03. This is the controlling plan for the work that remains after [Herdr detection parity](herdr-detection-parity.md) Phases 0 through 5 landed. That plan owns the screen lane and is finished except for its Phase 6, which this document replaces and expands. **Slices 1 and 3 are implemented** (see their result sections). Slices 2 and 4 are confirmed in full, and Slices 5 through 7 were added on 2026-09-03 for the user-facing half of the same goal: every agent Sidecar can recognise is one it can also start, and the surface that installs an integration reads as a table rather than an accordion.

## Decisions, 2026-09-03

Four questions were put to the maintainer and answered; they are recorded here so no slice reopens them.

1. **Every agent with launch config is launchable, but the Create picker shows only the ones that are installed.** A family appears in the creation pickers when its command resolves on `PATH`, or when the user has already named it in `plugins.workspace.agents`. The full catalog stays reachable through configuration. This keeps the picker short and honest without hiding anything.
2. **Slice 2 in full and all nine Slice 4 session-identity ports.** Open question 3 is answered: the ports are worth doing, because the user's stated goal is status and notifications as accurate as possible for whatever agent they happen to run, and exact session binding is what makes resume reliable. The maintenance surface is accepted; the sync report is what keeps each bump a diff review.
3. **Missing provider CLIs may be installed into a scratch prefix to capture traces.** Nothing lands on the maintainer's real `PATH` or in their dotfiles; the prefix is removed after the proof run. A provider that needs credentials the environment does not already carry ships at the tier the rules allow without traces, and says so in its capability entry.
4. **The branch merges into `main` when the work is complete, reviewed and proven.**

Two facts established while settling scope, recorded because they keep coming up. First, the "known gaps" the integration status prints for Claude are gaps in Claude Code's own hook contract, found by Sidecar's tracing of 2.1.220, and Herdr rolled back its full-lifecycle Claude hooks for the same two reasons; they are not Sidecar-specific and not inherited. The two Sidecar-scope items in that list are the `CLAUDE_CONFIG_DIR` blind spot and the grok/Claude shared-settings binding bug (`td-11040b`), which Slice 4 fixes while it is in that file. Second, Herdr has no per-agent launch or auto-approve table; its `agent_resume.rs` is the only launch-adjacent knowledge upstream holds, and it is the reference for the resume arguments Slice 5 records.

**Baseline:** Sidecar `main` at `9b8739f7`; Herdr vendored at `master` `d08e4468`, harness binary pinned at `preview-2026-08-31-b1ff4582e968`. Facts below were read from `internal/agentactivity/manifests/authority.upstream.json`, `aliases.upstream.json`, `upstream.lock.json`, `internal/agentlifecycle/capabilities.json` and the vendored asset tree at that commit, not from memory.

## One sentence

**Sidecar's screen lane is at parity with Herdr and stays there automatically; what remains is the hooks lane, where Herdr installs 17 provider integrations and Sidecar has 3, plus three agents Herdr identifies that Sidecar does not and four identity and signal mechanisms Sidecar never built.**

## Where parity stands today

### The screen lane is done and self-maintaining

Sidecar executes Herdr's manifest grammar at engine version 3, runs their 21 vendored manifests with 22 Sidecar overlay rules layered on top, and a weekly workflow opens a review whenever upstream moves. The review shows the manifest diff, the fixture verdict flips, and which overlay rules have stopped earning their place. A differential harness compares both engines against the real Herdr binary on every sync.

Current evidence: fixture census 61 fixtures, 24 declaring a state, 0 mismatches. Differential harness 61 compared, 37 agree, 0 disagree, 22 overlay divergences, 0 redundant. Merged mode 61 compared, 59 agree, 0 disagree. Two rows have no Herdr oracle, both Muse.

Nothing in this plan changes the screen lane. It is the permanent fallback and it is finished.

### Agent coverage: 20 of Herdr's 23, and the three absences are not the same kind

Herdr's `lookup_agent` table names 23 agents. Twenty-one of those have a screen manifest, and Sidecar vendors all 21. Sidecar registers 20 as families: ten launchable (`claude`, `codex`, `copilot`, `antigravity`, `cursor`, `opencode`, `pi`, `amp`, `grok`, `muse`) and ten detection-only (`cline`, `devin`, `droid`, `hermes`, `kilo`, `kimi`, `kiro`, `maki`, `qodercli`, `qwen`).

| Agent | Herdr has | Sidecar has | Why it is absent |
| --- | --- | --- | --- |
| `gemini` | alias, screen manifest | manifest vendored, no family | Deliberate. Antigravity replaced it and `agy` is already a full family. Registering it later is one alias line. |
| `mastracode` | alias, hooks integration (v2) | nothing | No screen manifest upstream, so there is nothing for the manifest lane to inherit. Reachable only through a hooks port. |
| `omp` | alias, hooks integration (v9) | nothing | Same. Herdr gives it lifecycle authority through hooks and ships no screen rules for it at all. |

So the agent-coverage gap is two agents, not three, and both of them are hooks-lane work rather than detection work. `gemini` is a decision already made.

### The hooks lane is where the real gap is: 3 of 17

Herdr ships 17 installable provider integrations, 33 asset files, all vendored under `internal/agentintegration/upstream/`. Sidecar has three adapters: `claude`, `codex`, `opencode`.

**Where Herdr holds lifecycle authority through hooks** and Sidecar's proved tier is below `full`. This is what `TestHerdrAuthorityGaps` prints today:

| Agent | Herdr integration | Sidecar tier | Sidecar adapter |
| --- | --- | --- | --- |
| `opencode` | v10, plugin | `full` | yes |
| `pi` | v8, extension | `advisory` (traced 0.84.3, 2026-09-02; the ceiling — `full` is structurally unreachable) | yes |
| `kimi` | v7, hooks | none | no |
| `kilo` | v4, plugin | none | no |
| `omp` | v9, hooks | none | no |
| `mastracode` | v2, hooks | none | no |

**Where Herdr installs an integration for session identity only** and Sidecar has none. State still comes from the screen for these in both projects, so the gap is exact session binding, not state:

`agy` (v3), `copilot` (v3), `cursor` (v1), `devin` (v2), `droid` (v3), `grok` (v1), `hermes` (v5), `qodercli` (v3), `qwen` (v1). Sidecar covers `claude` (v9) and `codex` (v8) here already.

**Where Herdr ships no integration at all**, so there is no gap: `amp`, `cline`, `gemini`, `kiro`, `maki`, `muse`.

### Other detection mechanisms Sidecar has not built

Four, and they are independent of the hooks work.

1. ~~**Process-tree scoring past generic runtimes.**~~ **Built in Slice 3, 2026-09-02.** Herdr keeps a generic-runtime list (`sh bash zsh fish tmux node bun cmd powershell pwsh`, plus a `python[3[.N]]` rule), unwraps a runtime using its argv, and scans the foreground process group preferring the group leader while scoring non-runtime matches higher. Sidecar matches argv[0] basenames only. **Measured consequence:** an agent installed as a plain `#!/usr/bin/env node` shim leaves the interpreter path in argv[0], tmux reports `node`, and neither identity input names the agent, so the pane is never claimed at all. Likely affected among the ten detection-only families: `qwen`, `cline`, `kilo`, `kimi`, `qodercli`. An agent that renames its own process, as Claude Code does, is unaffected. This is the single change that would make the most existing badges appear.

   The prediction held for the shim case and was wrong about Pi: Pi rewrites its own process title, so its argv[0] is `pi` and only tmux's `pane_current_command` says `node`. It was refused by the gate, not by the resolver. See Slice 3's result.

2. ~~**A launch-time agent hint.**~~ **Built in Slice 3, 2026-09-02, as a reader.** Herdr's `HERDR_AGENT=<agent>` on a wrapper command names the manifest to use when a sandbox hides the process. The detection-parity plan says Sidecar has an equivalent in `SIDECAR_AGENT`; it does not. `grep -rn SIDECAR_AGENT internal/` returns nothing. Managed shells already know their launch kind, so the hint matters for unmanaged and sandboxed panes — which is why Sidecar reads `SIDECAR_AGENT` and does not set it.

3. **`osc_progress` is permanently empty under tmux.** tmux consumes OSC 9;4 and exposes no payload, so seven upstream rules can never fire: `claude` 2, `grok` 4, `qwen` 1. The engine records this as region evidence rather than pretending. This is a tmux limitation, not a Sidecar defect, and closing it needs a terminal-model change well outside this plan. It is listed so nobody re-discovers it.

4. **Herdr's `agent read --source detection`** has a Sidecar equivalent in `explain --file --print-window`, but only offline. There is no live "print exactly the text detection saw for this pane" verb. Minor, and a tuning-loop convenience rather than a parity gap.

## Scope

**In scope:** porting Herdr's integration assets to Sidecar-native ones with Sidecar's own installers, per provider, on Sidecar's transport; registering `mastracode` and `omp` once they have a port; process-tree scoring and the launch hint; and the capability-matrix bookkeeping that keeps all of it honest.

**Out of scope:** the screen lane, the manifest engine, the sync tooling, the runtime fetch, `osc_progress`, and any change to how tiers are earned. A tier is proved by traces from a released provider version and is never copied from Herdr's table.

## Decisions carried forward from the detection-parity plan

These are settled and are not reopened here.

1. **Sidecar-native ports, not a compatibility shim.** Sidecar maintains its own copies of Herdr's assets, installed into the same provider locations by Sidecar's own installers, talking to Sidecar through `sidecar agent report`. Claiming to be Herdr through `HERDR_*` variables buys one agent today and a real identity collision whenever Sidecar and Herdr are nested.
2. **Every asset has two halves.** A provider half (which hook or plugin event maps to `working`, `blocked`, `idle`, or a session reference; the ordering guards; the per-provider quirks) which is the knowledge and is kept verbatim. A transport half (gate on `HERDR_ENV`, write JSON-RPC to `HERDR_SOCKET_PATH`) which is swapped one-for-one for Sidecar's: gate on `SIDECAR_MANAGED_SHELL` and `SIDECAR_BIN`, spawn `$SIDECAR_BIN agent report`. `internal/agentintegration/assets/opencode/sidecar-lifecycle.js` is the reference translation.
3. **A tier is earned by traces, never copied.** Herdr's authority table says which tier is *achievable*. It is a target and nothing more.
4. **Ownership is absolute.** An installer only ever removes changes Sidecar made, identified by the `sidecar-integration:` marker and by nothing else.

## Open questions, to answer before or during the first slice

1. ~~**Sidecar claims a `session-identity` tier for `pi` from a source with no installer.**~~ **Answered 2026-09-02: the entry was recorded ahead of the port, and it is retracted.** No asset exists. `DefaultAdapters()` returns OpenCode, Codex and Claude; `portedFrom` records those same three; `internal/agentintegration/assets/` holds only `opencode`; and `upstream/pi/herdr-agent-state.ts` is vendored read-only, embedded solely so `herdrsync` can diff it. Nothing Sidecar installs could produce a report carrying `sidecar.pi.extension`, so the tier was granted to a source that cannot speak. The `pi` entry is out of `capabilities.json` and the source id is out of `OfficialSources()`, which also stops a hand-written Pi hook from marking a reference resumable through a source Sidecar never wrote. `TestHerdrAuthorityGaps` now prints `pi ... sidecar=(none)` beside the four other hooks-authority agents. The evidence the entry carried is preserved in [the capability matrix](../../reference/agent-lifecycle-capability-matrix.md#pi), which is the brief Slice 1 works from.
2. **Which provider goes first.** Herdr's hooks-authority list is `pi`, `kimi`, `kilo`, `omp`, `mastracode`. The plan below proposes Pi, on the grounds that it is already installed on the maintainer's machine and already has a capability entry to either prove or retract. Confirm before starting.
3. **Whether the nine session-identity ports are worth doing at all.** For those agents Herdr reads state from the screen exactly as Sidecar does, so the port buys exact session binding and nothing else. That is real but small. It may be right to do two or three where a conversation adapter already exists and stop.
4. **How process-tree scoring interacts with Sidecar's stricter process gate.** Sidecar refuses to evaluate a manifest against a pane whose foreground process is not that agent, which is stricter than Herdr and deliberate. Widening identity without widening the gate leaves the badge missing anyway; widening both needs care that one agent's manifest can never read another's screen.

## Work sequence

Each slice is independently shippable and independently reviewable. Sizing is relative, not calendar.

### Slice 1 — Resolve the Pi claim, and port Pi (small, then medium)

~~Answer open question 1 first and act on it: if the `pi` tier is unearned, remove it and let `TestHerdrAuthorityGaps` show `pi sidecar=(none)` until a port earns it back.~~ **Done, 2026-09-02** (`td-d452b1`). The tier was unearned and is retracted, along with the `sidecar.pi.extension` official source; the gap list shows `pi sidecar=(none)`. Read the capability matrix's Pi section before writing the asset: it is the whole of what the retracted entry knew, including the two traps that are not visible in the event names (`agent_settled` rather than `agent_end` for turn completion, and `session_shutdown` firing for three reasons that are not an exit).

Then port Pi as the steel thread for every port that follows, because it is the provider where Herdr's advantage over the screen lane is largest and provable: Pi's upstream manifest has one rule, and it is a *working* rule, so an idle Pi pane only ever reaches the low-evidence fallback. That is why `agent start` on Pi times out waiting for a positive kind match, a failure recorded as pre-existing during the Phase 2 cutover.

- `internal/agentintegration/assets/pi/` — the Sidecar asset, provider half kept verbatim from `upstream/pi/herdr-agent-state.ts`, transport half swapped. A `portedFrom` row records that it was written against Herdr's pi integration version 8, so the sync report computes the diff on the next bump.
- A `PiAdapter`: `Provider`, `Source`, `Assets`, `Inspect` and `Plan` over the four actions, like every other adapter, since nothing in an adapter writes to disk. Herdr's `targets.rs` is the reference for where Pi reads its extensions.
- Hook-payload fixtures translated to Sidecar's report shape, from Herdr's shared `herdr-agent-state.test.ts`.
- A capability entry starting at the tier the lifecycle plan's rules allow before traces, promoted only on evidence from a released Pi version.

**Three facts this slice's bullets originally got wrong**, corrected 2026-09-02 from Herdr's source and from Pi 0.84.3's own package, and stated here so the port is not written against them.

1. `PI_CONFIG_DIR` is not a Pi variable. Pi 0.84.3 reads only `PI_CODING_AGENT_DIR`, tilde-expanded, defaulting to `~/.pi/agent`, and loads extensions from `<that>/extensions` and from a project-local `<cwd>/.pi/extensions`. `PI_CONFIG_DIR` is Herdr's *OMP* directory-name override.
2. `config_edit.rs` is not a Pi dependency. Herdr's `install_pi` is a three-line file drop: resolve the directory, refuse if the parent is missing, write the asset. No config is edited, no executable bit is set, no version is probed. Herdr's `uninstall_pi` deletes without checking its own marker; Sidecar's ownership rule is stricter and does not copy that.
3. The extension-directory collision refusal lives in `install_omp`, not `install_pi`. It exists because OMP is a rebranded fork of the same codebase, and `PI_CODING_AGENT_DIR` collapses both to one directory when it is set. Sidecar has no OMP adapter yet, so the collision is Slice 2's problem, not this one's.

**The blocked lane cannot be delivered, and the exit gate below is amended accordingly.** The only producer of `blocked` in Herdr's Pi asset is a listener on `pi.events.on("herdr:blocked")`, and nothing emits that channel: not Herdr, whose four occurrences of the string are two listeners and two test drivers; not Pi, whose shipped bundle does not contain the string `herdr` at all and whose thirty extension events include no permission, approval or prompt event; and not any extension installed on this machine. It is a cooperative extension-to-extension protocol on Pi's untyped event bus that nobody publishes to. This is the same conclusion the retracted capability entry had already reached from the released type definitions, now confirmed against the emitter side: Pi ships no permission system, so there is nothing to be blocked on, and the blocked lane is NO rather than PARTIAL. Port the ladder with its blocked branch intact, because it costs one comparison and the fixture from upstream's own test drives it directly, but do not claim the transition and do not wait for it in the proof.

**Consequences for the tier.** `full` is structurally unreachable for Pi: it requires `blocked_on_request` and `unblocked`, which no released Pi version can produce. The lifecycle plan caps a source with no traces at `advisory` in three separate places, and `TierFor` enforces that at runtime. So this slice's ceiling is `advisory`, reached only once traces from Pi 0.84.3 are captured and checked in; anything less traced than that is `session-identity` or nothing.

**Exit gate:** a live Pi pane reports `working` and `idle` through hooks, with the screen lane still the fallback; `agent start` on Pi no longer times out; the blocked branch exists, is driven by a fixture, and is recorded as unreachable with its evidence; the tier is whatever the traces prove and not more.

#### Result, 2026-09-02 (`td-f44647`)

**Shipped, and three of the four gate clauses are met.** The port landed in two halves: the offline half (asset, Go mirror, adapter, nine fixtures translated from upstream's own test, three node harnesses) and then a live proof against Pi 0.84.3 in a Sidecar-managed shell on a private tmux server, with `PI_CODING_AGENT_DIR`, `XDG_STATE_HOME` and `-config` all under a scratch tree. Sidecar's asset was installed by `sidecar agent integration install pi` rather than by hand, so the run proves the installer too. The user's real `~/.pi` was never written.

**The hooks lane drives a live pane.** One turn, read through `sidecar agent explain --shell`: `state=idle authority=lifecycle tier=advisory reportReason=turn_complete` → `state=working authority=lifecycle reportReason=turn_start` → `state=idle authority=lifecycle reportReason=turn_complete`, with `screenState=unknown` throughout. The verdict was authored by hooks and by nothing else. The store shows the reports it came from, sourced `sidecar.pi.extension`, with the pane bound to its transcript by path.

**The tier moved to `advisory` on `real-trace` evidence, `testedProviderRange` 0.84.3, covering `work_start`, `turn_complete` and `session_identity` — and nothing else.** Four sanitized traces are checked in under `internal/agentlifecycle/testdata/traces/pi/` with a provenance row, and six tests in `hooktrace_test.go` re-derive each claim from the fixture that earned it. `advisory` is the ceiling, as predicted. What the traces added beyond the tier:

- **The blocked lane is unreachable, now measured from the emitter side.** A `bash` tool ran unconditionally with no permission, approval or prompt event anywhere around it, and nothing published either `sidecar:blocked` or `herdr:blocked` across the whole run. The docs-only reading was right.
- **The `agent_end` trap is real and mechanical.** `ctx.isIdle` is `false` on `agent_end` and `true` on `agent_settled` three milliseconds later. An asset that closed a turn on `agent_end` would report idle mid-run.
- **`turn_end` is not turn completion either.** One agent run that calls a tool emits `turn_start`/`turn_end` twice. A "turn" in Pi's vocabulary is a provider round trip.
- **Cancellation is unknowable, not merely untraced.** An Escape-interrupted turn produces byte-identical events and field names to a completed one. The lane still resolves correctly, because `agent_settled` comes from a `finally` block; only the outcome is lost. `cancelled` stays unclaimed.
- **`session_shutdown`'s reason is readable and was `quit`.** So `process_exit` is unclaimed *by choice*, and a future asset that released the lane only on that reason could claim it.

**The screen lane is still the fallback, but the gate's mechanism was wrong.** Uninstalling the asset alone does not flip the lane. `StoreSource.Evidence` derives `integrationStatus` from the *record* — source known, asset version matching — and never from the installed files, so stored reports keep authority while `sidecar agent integration status pi` already reports `not-installed`/`screen-fallback`. Uninstalling *and* restarting Pi does flip it: `authority=screen`, `fallbackReason=process_generation_mismatch`, `freshness=none`. That is arbitration working exactly as designed — a stored claim is checked against the live world — but "stop the integration and watch the lane change" needs the run to end as well, and that is worth knowing before the same proof is written for the next four providers.

**`agent start` on Pi still times out, and this slice could never have fixed it.** Exit 1, `code=timeout`, before and after the tier promotion. The cause recorded above — Pi's manifest has one rule and it is a *working* rule, so an idle pane never matches — is not what happens. The refusal is one step earlier: `sidecar agent list` reports `evidence=pi.process-mismatch`. Pi installs as a `#!/usr/bin/env node` shim, tmux reports the pane's foreground command as `node`, and `DetectPi` refuses before any manifest rule is evaluated. The screen lane by itself answers `idle` for the very same capture — `sidecar agent explain --file <idle capture> --agent pi` gives `state=idle`, `fallback_reason=default_known_agent_idle_fallback` — so **widening the process gate is the whole fix, and it is Slice 3's first item.** No tier promotion reaches it: `agentcontrol`'s detector (`service.go:363`) calls `agentactivity.Detect` only and never consults the lifecycle store. This clause moves to Slice 3's exit gate, and Pi is the concrete case Slice 3 should be measured against.

**A regression landed between the proof and the review, and the re-proof is what closed it.** Applying review findings moved the asset's report counter to module scope seeded at `Date.now() * 1000`, copying upstream. Upstream writes to Herdr's socket, which bounds nothing; Sidecar's store bounds the field at `MaxSequence = 1 << 40` and enforces it unconditionally, so the seed sat about 1600x over the ceiling and every Pi report was rejected as `sequence N exceeds 1099511627776` — silently, because reports spawn with `stdio: "ignore"` and their exit codes are never read. The advisory tier briefly rested on a live proof of bytes that were no longer shipped.

The fix is to omit `--seq` entirely rather than to re-tune the seed. `sidecar agent report --help` already names that as what a per-event hook process should do, `AppendNext` assigns under the lock it already holds for the append, and the asset's queue is serialized so the order it assigns is the order the events happened. It also makes the reload hazard the seed was introduced for **structurally impossible** rather than statistically unlikely: a replacement instance has no counter to restart and no clock reading to overflow, because the only counter left is the store's high-water mark and it only goes up. Re-proved live on the shipped bytes (checksum-identical to the repo asset): store records `seq=1 idle session_start`, `seq=2 working turn_start`, `seq=3 idle turn_complete`, all accepted, with `authority=lifecycle` and `screenState=unknown` at each step. A `/new` inside the same Pi process then landed `seq=1 idle session_change` against a new `runId` on the same process generation, which is the forced session_start publish arriving correctly on a fresh run.

**The guard that would have caught it now exists.** `steelthread_test.go` said out loud that the subprocess boundary between a bundled asset and `sidecar agent report` was the one link nothing exercised, and that is exactly where this lived: a value 1600x over the store's bound passed the whole suite, because every existing test compares argv against argv and none asked whether the argv is one the shipped CLI accepts. `TestBundledAssetsSpawnArgvTheShippedCLIAccepts` in `internal/cli` takes the argv both assets' ordering harnesses record from real spawns and pushes each through the real flag parser, the real report construction, `agentlifecycle.Validate`, and an append to a real store. Reintroducing the seed makes it fail with the provider's own error text.

**One hazard found the hard way, and it belongs in every future proof run.** `cmd/sidecar/main.go` dispatches `cli.Run` before `main` unsets `TMUX`, deliberately and by comment. A **CLI**-driven proof started from inside a tmux pane therefore talks to the socket named in `$TMUX` — the machine's default server — no matter what `TMUX_TMPDIR` says, and the first `sidecar create shell` of this run created a session on the developer's live server before it was caught and removed. State isolation was never at risk (`internal/cli/cli.go:94` does check it on the CLI path). `scripts/tmux-drive.sh` is unaffected because it launches the TUI, which does reach the unset. Unset `TMUX` in any CLI-driven harness. Recorded in `internal/agentlifecycle/testdata/README.md`.

A second, smaller finding for the same audience: an asset spawns `$SIDECAR_BIN agent report` with no `-config`, and `ConfigPath()` has no environment override, so a bare report under `SIDECAR_ISOLATED_STATE=1` refuses. This proof used a one-line shim at `SIDECAR_BIN` that adds `-config` and passes the argv through unchanged. A real user needs none of it, but every future hooks-lane proof will hit it.

### Slice 2 — The remaining hooks-authority providers (medium)

`kimi`, `kilo`, `omp`, `mastracode`, in that order. Each follows Slice 1's shape. `omp` and `mastracode` also gain their first Sidecar identity here: an alias case and a catalog family, detection-only in the sense of Phase 4 except that their state comes from hooks rather than from a screen manifest, since upstream ships none for them.

**Exit gate:** every provider in this slice ships a Sidecar source, and every row `TestHerdrAuthorityGaps` still prints is a provider sitting at the ceiling its own traces allow, with that ceiling named in its capability entry. An empty list is not the measure and was never reachable: the gap rule closes a row only at `full`, and a port that keeps upstream's provider half cannot reach `full` for a provider that emits no distinguishable cancellation and no exit signal.

#### Result for `kilo`, 2026-09-03 (`td-16cbac`)

**Shipped and proved live, at `advisory`, which is this asset's ceiling.** The port follows Slice 1's shape exactly: asset with the provider half kept verbatim and the transport half swapped, a Go mirror held to it by a node replay harness over twelve fixtures, an adapter with Sidecar's ownership rules, four sanitized traces of kilo 7.5.9 re-derived by six tests in `hooktrace_test.go`, and a capability entry the tests derive rather than accept.

**Kilo is an OpenCode fork, so most of the shape was already built.** The plugin contract is OpenCode's, the double-load trap is OpenCode's, and the installer is OpenCode's with a different directory. What is not shared is the event vocabulary and one bug.

**The bug is the field's shape, and it is the same shape of bug the Pi port fixed.** Kilo's `session.status` carries an object whose `type` is the discriminator; Herdr's kilo asset at version 4 accepts a status only when it is a string, so on this release it maps none of them and re-reports the session instead. Herdr's own opencode asset at version 10 reads `status?.type` and has done since before this port, exactly as its omp variant already carried the Windows-path fix the pi variant lacked. Sidecar takes the fixed form, because `session.status` is the only state-shaped signal kilo has and an asset that cannot read it gives up the self-correction that makes a state-shaped signal worth having.

**Everything about kilo's contract was measured against 7.5.9 rather than read from Herdr, and four of the measurements changed the port.** The config directory is `$XDG_CONFIG_HOME/kilo` with `KILO_CONFIG_DIR` as the override, where Herdr hardcodes `~/.config/kilo` and honours nothing. Both `plugin/` and `plugins/` are globbed, so the double-load trap applies and the installer treats the second directory as damage. `tool.execute.before` and `tool.execute.after` are plugin hooks rather than bus events, so upstream's two tool branches are dead code against this release, which a turn that really ran a bash tool measures rather than argues. And a non-function export does not disqualify a kilo plugin module, where OpenCode 1.18.25 silently drops the whole thing; the asset holds to the stricter convention anyway, because relying on a fork staying laxer than its upstream is a bet nobody promised to keep.

**`advisory` is the ceiling, and it is reached.** `full` needs `cancelled`, which upstream's mapping cannot distinguish because a user interrupt and a provider failure are the same `session.error` with a different name that the asset never reads, and `process_exit`, which upstream's asset does not subscribe to at all. Both are differences between this port and Sidecar's own OpenCode asset rather than limitations of kilo, both are stated as such, and both move together with `covered` if a future asset version subscribes.

**The live proof did what a live proof is for: it found two things no offline test could.**

The first is a real defect this port would otherwise have shipped. `agent report-session --kind kilo` was refused on every turn with exit 5, because `resolveReportedKind` resolved a `--kind` claim through `agentcatalog.Lookup`, which searches the launchable families and their aliases only. Kilo is detection-only today, so every binding the asset sent failed while its state reports, which take no `--kind`, were accepted. Every test in the tree passed a launchable id, which is exactly why nothing caught it. The lookup now falls back to the detection catalog and `TestADetectionOnlyFamilyCanStillBindItsSession` drives the whole detection catalog rather than the one provider that exposed the bug. This is a gap the `kimi` port will meet identically.

The second is a mistake this run made, and it belongs in the next lane's briefing rather than in a footnote. **`KILO_CONFIG_DIR` does not isolate a proof run.** Kilo creates its XDG default config directory at startup whatever that variable says, and the first managed shell of this run wrote one zero-byte migration marker into the maintainer's real `~/.config/kilo` before it was caught. The file was removed and the directory restored to its previous contents; nothing under `~/.local/share/kilo`, `~/.local/state/kilo` or `~/.cache/kilo` was touched. A provider's own config-dir override is not a promise about where it writes, and `XDG_CONFIG_HOME` has to move too.

**Two harness facts for the next port.** Slice 1's `TMUX` trap is real and this run rediscovered it by invoking `tmux` once outside its own env file, creating a session on the maintainer's default server; it exited on its own and nothing of theirs was touched, but the rule is to put `unset TMUX` in the file every command sources, not to remember it. And the `-config` shim Slice 1 built is still needed and cannot be installed by being the binary Sidecar was invoked as: Sidecar publishes `SIDECAR_BIN` from `os.Executable` with symlinks resolved, so the pane has to export the shim over the published value. Moving `HOME` does not help either, because the isolation check is structural rather than a comparison against the real user's path.

**What the lane did.** `report-session --kind kilo --id ses_...`, then `working`/`turn_start`, then `idle`/`turn_complete`, all accepted, with `agent explain --shell` reading `state=idle authority=lifecycle tier=advisory source=sidecar.kilo.plugin integrationStatus=current` and `screenState=unknown` throughout. The binding landed in `shells.json` as `reported: true`. Uninstalling and ending the run flipped it to `authority=screen`, `fallbackReason=process_generation_mismatch`, which is Slice 1's finding reproduced rather than assumed.

**One thing the proof did not establish, stated because the gate's wording invites the assumption.** `agent explain` reported `screenEvidence=unsupported-agent` for the proof pane throughout, so the screen lane never answered and the hooks lane was the only lane. The session binding was accepted, but `CheckReportedKind` refuses only a positively identified occupant of a different kind and passes an unidentified one, so exit 0 does not show that process identity named the pane `kilo`. Kilo installs as a `#!/usr/bin/env node` shim, which is the case Slice 3 exists for; whether the widened resolver names it is a separate measurement this run did not make.

**`TestHerdrAuthorityGaps` still lists `kilo`,** because the rule closes a row only at `full` and this asset's ceiling is `advisory`. That is the same shape as Pi's row and is the honest reading: Herdr's table is a target, and for kilo the target is not reachable by a port that keeps upstream's provider half.

### Slice 3 — Process-tree scoring and the launch hint (medium)

Independent of Slices 1 and 2 and possibly worth doing first, because it makes badges appear for agents already registered.

- A generic-runtime predicate matching Herdr's list, kept distinct from Sidecar's existing `shell` bucket, which is a launch-readiness gate and not a scoring predicate.
- argv-based unwrapping of a runtime, and a foreground process-group scan preferring the group leader and scoring non-runtime matches higher.
- `SIDECAR_AGENT` as the process-identity hint, a hint only and never a lifecycle claim.
- Widen the process gate in step, per open question 4.

**Exit gate:** a Qwen or Cline pane installed as a plain `env node` shim shows a state badge. A test pins that one agent's manifest is never evaluated against another agent's pane. **And `sidecar agent start --kind pi` stops timing out**, inherited from Slice 1: Pi is the measured case, and its refusal is `pi.process-mismatch` from exactly this gate. Slice 1 established that the screen lane already answers `idle` for a real idle Pi capture once the gate lets it through, so nothing else is needed for that one.

#### Result, 2026-09-02 (`td-609a31`)

**Shipped, and every exit-gate clause is met.** Two commits: the port and the widened gate, then one fix the live proof found and the suite could not.

**Both halves are load-bearing, and not for the reason this plan predicted.** The proof ran on an isolated tmux server and state tree against real Pi 0.84.3 and a synthetic `#!/usr/bin/env node` shim named `qwen`, with a control binary built from `56a914ee` and pointed at the same live panes:

| pane | before (`56a914ee`) | after |
| --- | --- | --- |
| Pi 0.84.3 | `kind=pi status=unknown evidence=pi.process-mismatch` | `kind=pi status=idle evidence=pi.known-live-fallback interactiveReady=true` |
| node-shim `qwen` | `kind=(none) evidence=provider-not-identified` | `kind=qwen status=idle evidence=qwen.known-live-fallback` |

`sidecar agent start --kind pi`: before, `agent pi did not become ready: context deadline exceeded` at the full timeout; after, ready in about one second.

Read the first row carefully, because **Slice 1's account of Pi's mechanism is wrong and is corrected here**. Slice 1 recorded that Pi "installs as a `#!/usr/bin/env node` shim, tmux reports the pane's foreground command as `node`, and `DetectPi` refuses" — which reads as though argv[0] were the Node interpreter path and Pi were therefore unidentifiable. It is not. Pi rewrites its own process title, so its argv[0] is literally `pi` and `comm` is `pi`; only tmux's `pane_current_command` says `node`. Pi was **already identified** before this slice and was refused by the *gate*. The qwen shim is the case the *resolver* could not reach. So the two halves fix two different providers, and doing either alone would have left the other exactly where it was — which is the plan's own "identity and the gate widen together or not at all" rule arriving as a measurement rather than a prediction.

**The proof found a regression the suite could not.** Upstream's `procargs2_argv` returns `None` on an empty argv slot and the port copied that faithfully. A process that sets its own title on macOS writes into the same argv memory `kern.procargs2` reports and blanks the slots it stops using, while the kernel still reports the original argc — Pi reads back as `argc=2`, `argv[0]="pi"`, `argv[1]=""`. Voiding the vector there threw away the one element that names the program, so the widening made Pi *less* identifiable than the argv[0]-only parser it replaced: a regression introduced by this slice, on the exact provider this slice exists to reach. Every test passed throughout, because a synthetic argv has no blank slots and only a live title-rewriting process produces one. An empty slot now ends argv rather than voiding it; bounding by argc still holds, so the environment stays unreachable. `TestDarwinArgvSurvivesAProcessTitleRewrite` and `TestDarwinArgvStillStopsAtArgc` pin both directions.

**The gate closed a hole that was already open on `main`.** A resolved identity now settles the question in both directions, and the second direction was never stated as a goal: four of the launchable gates accept a bare `node` or `bun` with no other evidence, so on that same Pi pane `claudeProcess("node")` was true and `claude.toml` was evaluable against a Pi screen. `TestOneAgentsManifestIsNeverEvaluatedAgainstAnotherAgentsPane` drives every ordered pair of the twenty registered families off the live catalog, so a family added later is covered without anyone remembering to add it.

**Pi deliberately gains no bare-runtime allowance.** `upstream/pi.toml` has one rule, `working_literal` — the literal "Working..." anywhere in the read window — so allowing bare `node` would let Pi's manifest claim any Node pane that printed that word, and let every Pi fallback report a Node pane as idle. A shim pane reaches the engine on its own argv instead. The residual is stated rather than hidden: on a platform with no process-identity adapter nothing resolves, so a shim-installed pane is still refused there.

**`SIDECAR_AGENT` is read, not written, and it works on both adapters — but proving that took one wrong conclusion first, which is worth recording because the trap is reusable.** A live proof of the hint against a wrapper pane read back empty, and a follow-up measurement seemed to settle why: a `/bin/sleep` child spawned by the test itself, same uid, with the variable exported, returned 34 bytes of `kern.procargs2` with no environment at all, against 11172 bytes for the caller's own pid. The conclusion drawn — macOS never returns another process's environment, so the feature is Linux-only — was wrong, and an agent sent to implement it refused and disproved it instead.

macOS gates the environment section by *binary*, not by relationship: a restricted executable (SIP-protected platform binaries, the same protection that blocks `DYLD_*` inheritance) withholds it from everyone, same uid included, while an ordinary binary's is readable cross-process. `/bin/sleep` is SIP-protected, so every measurement in that first round was of the one case that cannot work. A byte-identical **copy** of `/bin/sleep` at an ordinary path returns 11356 bytes and yields the hint; a live tmux pane running an ordinary binary under `tmux -e SIDECAR_AGENT=codex` resolves to `codex` through `ResolveForegroundAgent`.

The real bound is narrower and is now written where the next person will hit it: `sandbox-exec`, `ssh` and the system shells cannot be hinted through on macOS; a wrapper the user installed can. Argv crosses the boundary in both cases, so identification still works on a pane the hint cannot reach. Upstream's `process_agent_hint` is bounded identically. The lasting lesson is in `process_identity_darwin.go`: **a live proof of this feature must use a binary you built**, because a stand-in from `/usr/bin` reads empty for a reason that has nothing to do with the code — which is exactly how a correct feature nearly got deleted as broken. Managed shells do not set it: `managedShellEnv()` has no access to the launch kind, so setting it would have meant threading a kind through two functions and a tmux-version fallback for a hint those panes do not need — they already know their family. The hint is for unmanaged and sandboxed panes, which is what the plan asked for.

**Reaching those panes at all took a second pass.** As first written the hint was unreachable in exactly the case it exists for: both hint-aware call sites gated on `NeedsProcessIdentity`, true only for `node`, `bun`, `agent` and `python*`, so a `docker` or `bwrap` pane never asked. Its only live effect was on runtime panes, where it *overrode* correct process evidence, because the port had faithfully kept upstream's precedence of reading the leader's hint before identifying the leader. Both are fixed: `ResolveForegroundAgent` now owns a three-rung cost ladder (nothing for a pane whose command already names an agent or a shell; one group lookup plus one leader environment read for an unrecognised command, with no process-table walk; the full walk only for a known shared runtime), and evidence beats the hint — a deliberate divergence from upstream, whose order is safe for them because their own wrapper writes `HERDR_AGENT` while Sidecar's is a bare variable it only ever reads.

**The hint stays out of the refusal path, structurally.** `ResolveForegroundProcess` is evidence-only and is what `lifecycleenv.OccupantKind` — and so `VerifyReportedKind`, which *refuses* hook reports — reads. `ResolveForegroundAgent` is the hint-aware one and is what the detection and UI callers read. Merging them would let `export SIDECAR_AGENT=codex` switch off a Claude pane's lifecycle lane, so the split is commented rather than left to be re-simplified later. `TestAnAgentHintCannotChangeTheOccupant` pins it.

**Not ported, deliberately:** the Windows `cmd`/`powershell` argument walkers. Sidecar has no Windows process-identity adapter, so they are unreachable and untestable; they are named in `process_tree.go`'s header so the gap is visible when one is written. `mastracode` resolves to a name the alias table cannot place and simply does not match, which is correct until Slice 2 registers the family.


### Slice 4 — Session-identity ports, as many as earn their place (small each)

Answer open question 3 first. Then port in order of live use, confirming for each that Sidecar's existing screen coverage plus the new session binding is worth the maintenance. Herdr's Claude asset is at v9 and Codex at v8; the sync report already shows the diff against the version Sidecar's adapters were written against, so re-porting those two is a diff review rather than new work.

**Exit gate:** every port has a fixture, a capability entry earned by traces, and a `ported-from` header the sync report can diff.

### Slice 5 — The launch catalog moves to TOML and grows to every recognised agent (medium)

Today `internal/agentcatalog` holds ten launchable families as a Go slice and ten detection-only families as a second slice. The knowledge in the first is small and flat: a command, an auto-approve flag, resume arguments, aliases, an adapter id. That is configuration, and it belongs in data a user or an agent can read and extend without a rebuild.

- One TOML file per family, embedded in the binary under `internal/agentcatalog/families/`, carrying every field `Family` has today. The Go slices go away; `Families()`, `DetectionFamilies()` and every finder read the parsed set, so no consumer changes.
- An optional user overlay directory under the Sidecar config dir. A file there with a known id overrides that family's fields; a file with a new id adds a family. Overlay parsing happens once at startup, off the render path, and a malformed file is reported and skipped rather than fatal.
- Every detection-only family that has a real CLI becomes launchable: `cline`, `devin`, `droid`, `hermes`, `kilo`, `kimi`, `kiro`, `maki`, `qodercli`, `qwen`, plus `omp` and `mastracode`, which gain their first identity here so Slice 2 can register adapters against them. Each entry's command, auto-approve flag and resume arguments are read from the provider's own documentation or `--help`, with Herdr's `agent_resume.rs` as the reference for resume, and the source is recorded in the file. A provider with no auto-approve mode says so rather than guessing a flag.
- The creation pickers filter by installation: a family is offered when its command resolves on `PATH` or when it is already named in `plugins.workspace.agents`. The lookup is cached per process and never runs on a render path.
- `docs/guides/active/adding-new-agent-clis.md` is rewritten so Step 1 is "write one TOML file".

**Exit gate:** the ten existing families launch with byte-identical argv before and after; `TestAgentPickersFollowCatalog` and the vocabulary parity tests pass unchanged; a new family added as a TOML file in the overlay directory appears in the Create Workspace picker of a running Sidecar once its command is on `PATH`, and the picker on this machine offers exactly the installed set.

### Slice 6 — The Integrations page is a table, not an accordion (small)

The Configuration → Agents → Integrations route currently expands the focused row to show its detail line and action pills, so moving the cursor reflows the whole list and the pills are unreachable by mouse. It becomes a fixed-shape table: one row per supported provider with name, status, tier and the action pills always painted in a fixed column, dimmed when the service does not offer them. Unsupported providers collapse to one summary line. The gap count leaves the row; the command that lists gaps, `sidecar agent integration status <agent>`, is shown once at the foot of the page. Every fact still comes from `agentintegration.Service` and nothing is computed on a render path.

**Exit gate:** moving the cursor changes only the highlight; every offered action is clickable on every row without moving the cursor first; the route renders the same for three, seven and seventeen providers.

### Slice 7 — A maintenance skill, so the next port is a procedure (small)

A skill under `.claude/skills/` that tells an agent how to pull Herdr, read the sync report, and port one hook or plugin integration following the Pi steel thread: which upstream files hold the provider half, how the transport half is swapped, where the adapter, fixtures, capability entry and `portedFrom` row go, the proof-run hazards Slice 1 recorded (`TMUX` on the CLI path, the `-config` shim, isolated state and a private tmux server), and how a tier is earned. The weekly sync report already diffs the vendored assets; the skill is what turns a diff into a port.

**Exit gate:** an agent given only the skill and a provider name produces a port whose shape matches the existing ones, judged by the same tests that gate every other adapter.

## Acceptance evidence

- `TestHerdrAuthorityGaps` prints an empty list.
- Every agent in Herdr's `lookup_agent` table has a Sidecar identity, except `gemini`, which is declared out and pinned as such by `TestEveryVendoredManifestIsRegisteredOrDeclaredUnregistered`.
- A live pane for each hooks-authority provider reports state through hooks, with the screen lane as fallback, provable by stopping the integration and watching the lane change in `sidecar agent explain`.
- A node-shim-installed agent shows a badge.
- No capability tier is claimed without a trace from a released provider version.

## Risks

- **Seventeen assets is a maintenance surface, and it is the reason to port selectively rather than exhaustively.** The sync report makes each bump a diff review, which is what keeps this affordable; a port nobody uses is still a diff somebody reads. Slice 4 exists to be cut.
- **Provider hook contracts change faster than manifests.** Herdr's integration versions bumped several times a month across the 0.8.x line. The mitigation is the same one the manifest lane uses: the version is in the lock, the diff is in the report, and the trace tests say which transition moved.
- **A wrong hook mapping is worse than no hooks**, because a hooks-lane verdict outranks the screen lane. Every port lands with fixtures translated from Herdr's own test payloads before it is trusted.
- **Widening process identity can misattribute a pane.** Sidecar's stricter gate is what prevents one agent's manifest reading another's screen; Slice 3 must widen identity and the gate together or not at all.

## Related plans

- [Herdr detection parity](herdr-detection-parity.md) owns the screen lane, the manifest engine, the sync tooling, the overlays, and the opt-in runtime fetch. Implemented through Phase 5. Its Phase 6 is superseded by this document.
- [Deterministic agent lifecycle hooks](notification-agent-lifecycle-hooks.md) owns the report contract, authority arbitration, the capability matrix and the evidence tiers. This plan adds providers to that matrix; it does not change its rules.
- [Herdr agent control and session restore](herdr-agent-control-and-session-restore.md) owns `sidecar agent start/prompt/wait/read`. Those verbs consume `agentactivity.Result` and inherit anything this plan improves. Slice 1's exit gate names one of its failures.

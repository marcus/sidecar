---
name: port-herdr-integration
description: >
  Port one Herdr provider integration into Sidecar: read the sync report to see
  which provider moved, pick the port shape, keep the provider half verbatim and
  swap the transport half, register the adapter everywhere it has to appear, earn
  a tier from traces rather than copying Herdr's table, and pass the guards that
  gate every other adapter. Use when adding or re-porting a provider integration
  under internal/agentintegration, when a weekly Herdr sync review shows an asset
  changed, or when a capability entry needs promoting.
user-invocable: false
---

# Porting one Herdr integration

Sidecar installs its own hook, plugin and extension assets into other agents' configuration so those agents report their lifecycle state and their session identity back to Sidecar. The knowledge in those assets comes from Herdr, which ships seventeen of them. A port takes one provider's knowledge, keeps it verbatim, replaces Herdr's transport with Sidecar's, and lands it behind the same guards every existing adapter passes.

This is a procedure. Work through it in order for one provider at a time. Every step names the file to open and the test that gates it.

## 0. How Sidecar mirrors Herdr

Four things hold the mirror in place. Read them before you write anything.

**The vendored tree.** `internal/agentintegration/upstream/<dir>/` holds byte-for-byte copies of Herdr's `src/integration/assets`. Nothing installs them and no runtime path reads them; they exist so re-porting a provider is a review of a diff rather than a fresh reading. `internal/agentintegration/upstream/herdr-agent-state.test.ts` at the root of that tree is upstream's own test, and its payloads are where your fixtures come from.

**The lock.** `internal/agentintegration/upstream.lock.json` pins every vendored file by SHA-256 and records each provider's `HERDR_INTEGRATION_VERSION`. It is keyed by Herdr's **agent id**, which is not always the directory name: `agy` is the id and `antigravity_cli` is the directory. `TestVendoredIntegrationAssetsMatchLock` fails if a vendored byte changes without a matching lock entry, so never hand-edit anything under `upstream/`.

**The provenance record.** `internal/agentintegration/portedfrom.go` holds one `PortedFrom` row per shipped asset: the Sidecar provider id, the Herdr agent id (`UpstreamID`, which must match the lock's key), the directory, the upstream version the port was written against, the commit, and prose evidence. It is Go data rather than a header comment because several Sidecar assets are Go values with no file to put a header in. `TestEveryAdapterRecordsWhatItWasPortedFrom` fails if an adapter ships without a row.

**The sync tool and the weekly review.** `internal/tools/herdrsync` re-vendors the tree, rewrites the lock, and renders `internal/agentactivity/manifests/report.md`. `.github/workflows/herdr-sync.yml` runs it every Monday, pushes `bot/herdr-sync`, and puts that report into the pull request body.

> **`go run ./internal/tools/herdrsync` performs a real sync.** It fetches from Herdr and overwrites the vendored manifest and integration trees in your working directory. Do not run it to "see what it does". Read `internal/agentactivity/manifests/report.md`, and drive the renderer from tests when you need to see output. Name the package explicitly if you ever must run it, and **never glob `./internal/tools/...`**, which expands to the sync tool and syncs.

## 1. Read the sync report to see which provider moved

Open `internal/agentactivity/manifests/report.md` and find `## Integration assets`. It has two halves.

The table has one row per vendored provider: `Agent | Asset directory | Version | Previous | Change | Sidecar port`. The `Change` column says `unchanged`, `**bumped**`, `**rolled back**`, `added` or `first sync`. The `Sidecar port` column is derived from `PortedFromRecords()` at render time and reads either `` `<provider>` from version N `` or `not ported`.

Then `### Upstream changes since each Sidecar port` gives, per ported provider, the diff between the upstream bytes at the commit the port was written from and the bytes just vendored. It diffs **bytes, not version numbers**, so a file upstream edited without bumping still shows. A file it could not read says `was **not compared**`; that is a comparison that did not happen and never evidence that anything changed.

Two traps:

- **The committed `report.md` is a snapshot, not a live view.** It is written only by a real sync run, so between syncs it is as stale as the last run. If it says `not ported` for a provider that has an adapter, believe `PortedFromRecords()` and not the file. `TestARenderedReportNeverCallsAPortedProviderUnported` in `internal/tools/herdrsync/herdrsync_test.go` renders the section against the embedded lock and the real records, so the *next* sync is guaranteed truthful; the committed file catches up when the workflow next runs. Do not regenerate it by hand.
- Everything below the table is bounded at `integrationDiffSectionBudget` (600 lines) so the report fits GitHub's pull request body limit. A diff elided there is named, not dropped.

The weekly review already surfaces this for you: `internal/agentintegration/upstream` is in the workflow's change-detection paths and in the paths it commits to the sync branch, so a ported provider's asset diff appears twice, as raw file changes on the branch and as rendered diffs in the pull request body.

## 2. Choose the port shape

There are three, and the choice is decided by where the provider's knowledge lives upstream, not by preference.

### Shape A: entry in the provider's config file

Use it when Herdr's knowledge is a table in its Rust rather than in a shipped script: `src/integration/mod.rs` holds `(event, matcher, action)` rows, and the asset upstream ships is a thin shell shim that only gates on `HERDR_ENV` and writes a socket frame. There is nothing in that shim worth porting, because `sidecar agent report` **is** the gate for a hook surface and `--hook-stdin` reads the payload with a bounded reader. So the port is the table, rendered into the provider's own configuration file.

Reference: `internal/agentintegration/kimi_install.go` with `kimi.go` and `kimi_test.go`. Twelve rows, a fenced marker region in `config.toml`, a TOML parser used as a read-only oracle at both ends of every edit, and a golden checksum taken over the **rendered block** because there is no asset file. `TestKimiFiresExactlyOneHookPerEvent` exists because Kimi runs every matching hook in parallel, so two rows matching one event would be two report processes racing.

The second reference is Mastra Code, merged and on `main`: `internal/agentintegration/mastracode_install.go` with `mastracode.go` and `mastracode_test.go`, and its fixtures under `internal/agentintegration/testdata/mastracode/`. Eleven rows into `~/.mastracode/hooks.json`, a millisecond timeout, `|| true` on the three blocking events, and one row whose event moved because the live proof found upstream binding on a payload that carries a constructor placeholder rather than a thread id.

For the most compact list there is of what a port touches, run `git log --stat` over the mastracode commits (`git log --stat --grep mastracode`) and read the file names rather than the diff.

### Shape B: a dropped file the provider loads

Use it when the knowledge is in a JS or TS asset upstream ships: a plugin, an extension, a Python package. The port is a Sidecar asset under `internal/agentintegration/assets/<provider>/` whose provider half is upstream's behaviour kept verbatim and whose transport half is swapped.

References, and each directory has the same five parts:

| Provider | Asset | Adapter | Reads plugins from | Relocated by |
| --- | --- | --- | --- | --- |
| OpenCode | `assets/opencode/sidecar-lifecycle.js` | `opencode_install.go` | `$XDG_CONFIG_HOME/opencode/plugin/` and `plugins/`, both loaded | `XDG_CONFIG_HOME` |
| Pi | `assets/pi/sidecar-lifecycle.js` | `pi_install.go` | `<agent dir>/extensions` plus project-local `.pi/extensions` | `PI_CODING_AGENT_DIR`, tilde-expanded |
| Kilo | `assets/kilo/sidecar-lifecycle.js` | `kilo_install.go` | `$XDG_CONFIG_HOME/kilo/plugin/` and `plugins/` | `KILO_CONFIG_DIR`, and see the isolation trap in step 8 |
| OMP | `assets/omp/sidecar-omp-lifecycle.js` | `omp_install.go` | the same directory Pi reads, which is why it has a collision refusal | `PI_CONFIG_DIR` names the directory, `OMP_PROFILE`/`PI_PROFILE` insert a segment and suppress the agent-dir override, `PI_CODING_AGENT_DIR` is `path.resolve`d and **not** tilde-expanded |
| Hermes | `assets/hermes/__init__.py` and `plugin.yaml` | `hermes_install.go` | `<hermes dir>/plugins/<plugin name>/`, and inert until the plugin is named in `plugins.enabled` in the user's `config.yaml` | `HERMES_HOME`, tilde-expanded |

The five parts of an asset directory:

1. `sidecar-lifecycle.js` (or the provider's own name) is the shipped bytes. Its first line carries the ownership marker.
2. A **Go mirror** beside it in the package root: `opencode.go`, `pi.go`, `kilo.go`, `omp.go`. It implements the same mapping in Go so Sidecar can reason about the asset without running node.
3. `ordering-harness.mjs` records the argv every real spawn produces. `internal/cli`'s argv corpus reads it.
4. `replay-harness.mjs` drives the shipped JavaScript over a fixture file.
5. `exports-harness.mjs` checks the module's shape against what the provider will load. `package.json` makes the directory a module. Pi adds `reinstantiate-harness.mjs` for its reload path.

The fixtures are `.tsv` files under `internal/agentintegration/testdata/<provider>/`, one file per branch, each with a `README.md` saying what the columns mean. One fixture drives **both** implementations and they are compared action for action, which is the whole point of keeping a Go mirror: a divergence is a test failure rather than a field bug. OMP is the reference for a mapping that needs a clock, because a pure mapping may not call `setTimeout`: `mapEvent` emits `schedule` and `cancel` actions, the runtime owns the timer, and a fired timer re-enters the mapping as an ordinary event, so the `.tsv` can drive the debounce deterministically through both sides.

**When the asset is not JavaScript. Hermes is the reference.** Four of the shipped assets are JS and their harnesses are `.mjs` because node is what runs them; `SIDECAR_REQUIRE_NODE=1` turns the skip into a failure in CI. Hermes is a Python package, and it is what a non-JS asset looks like when it is done: `assets/hermes/__init__.py` with `plugin.yaml` beside it, a Go mirror in `hermes.go` written exactly the way the JS mirrors are, and the harnesses as `.py` invoked through the interpreter the provider itself uses. `assets/hermes/replay-harness.py` drives the real `register()` over a fixture and reports the argv each spawn would have built, `assets/hermes/argv-harness.py` is the one that really spawns and feeds `internal/cli`'s corpus, and both skip when `python3` is absent unless `SIDECAR_REQUIRE_PYTHON=1` says the run is one where a skip would hide a break. Read `hermes_test.go` for how the two halves are compared element for element.

**When the asset is more than one file, or is a file plus a config edit. Hermes is the reference for that too.** The other shape B ports drop exactly one file; Hermes writes `__init__.py` and `plugin.yaml` into `<hermes dir>/plugins/sidecar-agent-state/` **and** adds one item to the `plugins.enabled` list in the user's `config.yaml`, which is shape B and shape A at once. Read `hermes_install.go` for the `Assets()` that returns three entries with two ownerships in it, `hermes_config.go` for the YAML line surgery, and `hermes_test.go` for what the suite has to prove.

Three things in that pair are the parts worth copying. The config edit is line surgery with a YAML parser used as a read-only oracle at both ends and never as a serializer, so the user's comments, anchors, quoting and key order survive byte for byte and a composed image that will not verify becomes a refusal with an empty op list rather than a partial write. Ownership of that line is its **value** rather than a marker comment, because `hermes config set`, `hermes plugins enable` and the setup wizard all round-trip the file through `yaml.dump` and drop every comment in it. And a half install reports nothing while looking present, so a state with the files but not the line is `needs-repair` rather than a lesser install. Only the shapes Hermes itself writes are edited: a flow sequence and a bare `plugins` list are both refused and named, because rewriting them would edit bytes outside Sidecar's own entry. Do not approximate any of it by rewriting the whole config file.

### Shape C: a session-identity entry through the shared generic

Use it when Herdr's integration exists only to name the conversation occupying the pane. State still comes from the screen lane in both projects, so the port buys exact session binding and nothing else, which is what makes exact resume reliable.

**Do not write a new adapter for this.** `internal/agentintegration/sessionhook.go` is the whole lifecycle (inspect, status, the four verbs, the ownership gate, the backup, the refusals) driven by a `sessionHookIntegration` descriptor. Eight providers are instances of it: Antigravity, Copilot, Cursor, grok, Devin, Droid, Qoder CLI and Qwen. What a provider contributes is data.

`sessionHookIntegration` fields, in the order you will fill them:

| Field | What it is |
| --- | --- |
| `provider` | the catalog agent kind, and the `--kind` the entry claims |
| `command` | the executable to find on `PATH` and probe for a version. Separate from `provider` because Antigravity's family is `antigravity` and its binary is `agy`, and Qoder's family is `qodercli` and its binary is `qoder` |
| `source` | the integration id reports carry, `sidecar.<provider>.hooks` |
| `assetVersion`, `assetSchema` | the bundled entry's version and its marker schema |
| `fileName` | the config file's base name, also the asset's name and the name every status message uses |
| `dir func(Env) string` | resolves the directory, honouring whatever override **the provider itself** honours |
| `spec hookEntrySpec` | the entry's shape and its address inside the file |
| `item func() json.RawMessage` | what gets appended: a matcher group for a grouped provider, the handler object for a flat one |
| `newFileMembers` | top-level members written only into a file Sidecar **creates**, never added to one the user already has |
| `setupHint` | the sentence shown when the config directory is absent. Setting it is also what makes a missing directory a refusal rather than something install creates |
| `shadowedBy`, `shadowNote` | a sibling file whose presence makes the entry inert |

`hookEntrySpec` in `internal/agentintegration/hookconfig.go` carries the coordinates: `namedBlocks`, `block`, `flat`, `commandKey`, `altCommandKeys`, `matcher`, `events`, and `canonical` (every asset version ever shipped, so an older installed entry reads as `outdated` rather than foreign).

Read one adapter per sub-shape before writing yours:

- **`copilot_install.go`**, the flat handler array. Three fields differ from every other provider and none was invented: `type: "command"`, the command under `bash` rather than `command`, and `timeoutSec` rather than `timeout`. `altCommandKeys` carries the Windows `powershell` spelling, read but never written.
- **`antigravity_install.go`**, the named block. `hooks.json` is keyed by hook *name*, blocks from every source merge, so the scan walks every top-level block. Sidecar owns `sidecar`. It also shows the `; printf '{}\n'` suffix a provider that demands JSON on stdout needs.
- **`cursor_install.go`**, the minimal entry. Cursor documents `{"command": ...}` with no `type`, and its own generator emits one with a `type`, so the scan accepts both. It is also where `newFileMembers` exists: Cursor's `"version": 1` goes into a file Sidecar creates and never into a user's.
- **`grok_install.go`**, the dedicated file. grok merges every `<grok home>/hooks/*.json`, so Sidecar writes `sidecar.json` rather than editing the user's. It still owns only the *entry* inside it: a hook a user added beside Sidecar's survives, and the file goes away only when Sidecar's entry was all it held.
- **`devin_install.go`**, the ordered event list. Upstream maps six events to the same session action because it trusts no single Devin event to carry the id, so `events` is a list; every other integration passes none and gets `SessionStart`.
- **`droid_install.go`**, the shadow file. `~/.factory/hooks.json` makes `settings.json`'s hooks key inert, so an entry Sidecar wrote would be correct, read as current, and never fire. Status inspects the shadow file and names it. It does not change the status and it does not edit that file.

`hookconfig.go` owns the scan that finds Sidecar's entries in a file somebody else owns. `hookPayload` in `internal/cli` reads both spellings of a session id, snake_case winning, so one `--hook-stdin` reader serves every provider; Devin is why, and grok's payload carries both.

**Whether to honour a provider's config-dir override is decided by evidence, not by the variable's name.** Follow the code path that reads the file:

| Provider | Herdr honours | Sidecar honours | Why |
| --- | --- | --- | --- |
| Claude | `CLAUDE_CONFIG_DIR` | yes | the shipped binary resolves its config home from it |
| grok | `GROK_HOME`, `GROK_CONFIG_DIR` | `GROK_HOME` only | the binary carries `GROK_HOME`; Herdr's own comment calls the other its test seam |
| Cursor | `CURSOR_CONFIG_DIR` | no | cursor-agent has a resolver that reads it and a hook loader that does not use it |
| Antigravity | `ANTIGRAVITY_CLI_CONFIG_DIR` | no | the variable appears nowhere in the shipped agy binary |
| Copilot | `COPILOT_HOME` | yes | no binary to check; a plausible provider override beats none, and the entry says it is unverified |
| Droid | nothing | nothing | `TestDroidLivesUnderTheFactoryDirectoryWithNoOverride` fails if one is invented later |
| Qoder | `QODER_CONFIG_DIR` | yes | on Herdr's word, and the capability entry records that it is Herdr's reading |
| Qwen | `QWEN_HOME` | yes | verified in qwen-code's own `Storage.getGlobalQwenDir` |

An override is read with `overrideDir`, which tilde-expands and trims whitespace. The exception is a provider that refuses a relative path itself: `ClaudeConfigHome` deliberately does neither, because Claude refuses a config home that is not absolute, and OMP's `PI_CODING_AGENT_DIR` is `path.resolve`d against a cwd Sidecar cannot know, so a relative or `~`-prefixed value is refused rather than guessed at.

## 3. Read the provider half, row by row, against Herdr

Read, read-only, in `/Users/marcus/code/herdr/src/integration/`: `targets.rs` (where the provider reads its hooks or plugins, and which variable relocates that), `registry.rs`, `actions.rs`, `config_edit.rs`, `mod.rs`, `tests.rs`. Then read the vendored asset under `upstream/<dir>/`, and `upstream.lock.json` for the version you are porting from.

Keep the provider half **verbatim in behaviour**: which event maps to `working`, `blocked`, `idle` or a session reference; the ordering guards; the per-provider quirks. Then check every claim against a released build of the provider where you can, because the ports that went wrong went wrong here:

- Kilo's `session.status` carries an object whose `type` is the discriminator, and Herdr's kilo asset at version 4 only accepts a string, so it maps none of them on that release. Sidecar takes the fixed form Herdr's own opencode asset already had.
- Pi's turn completion is `agent_settled`, not `agent_end`: `ctx.isIdle` is `false` on `agent_end` and `true` three milliseconds later.
- OMP has no `agent_settled` at all, and Pi's guard is wrong in both directions there. Upstream's three guards replace it.

Where Herdr does something Sidecar will not, say so and say why. Sidecar's rules are stricter in two standing places: ownership by marker only, and refuse rather than overwrite.

## 4. Swap the transport half

Herdr's transport gates on `HERDR_ENV` and writes a JSON-RPC frame to `HERDR_SOCKET_PATH`. Sidecar's is:

```js
const bin = process.env.SIDECAR_BIN
if (process.env.SIDECAR_MANAGED_SHELL !== "1" || !bin) return
```

then spawn `$SIDECAR_BIN agent report ...` for a state report, or `$SIDECAR_BIN agent report-session` for a session binding. `internal/agentintegration/assets/opencode/sidecar-lifecycle.js` and `assets/pi/` are the reference translations.

`report-session` takes exactly one of `--id ID`, `--path ABS_PATH`, `--clear` or `--hook-stdin`, and they are mutually exclusive. Which one you use follows from the shape: a **hook** the provider hands a JSON payload on stdin uses `--hook-stdin`, and the bounded reader takes both spellings of the id for you; a **plugin or extension** already holding the value in process passes `--id`, because piping a payload you constructed yourself back through a parser buys nothing. Omit `--source` and the report gets `OfficialSourceFor(kind)`, which is what makes it trusted and resumable, so name a source only if you deliberately want an untrusted one.

Five rules:

**Never pass `--seq`.** The store assigns sequences under the lock it already holds for the append. Upstream seeds a counter from `Date.now() * 1000` because a socket bounds nothing; Sidecar's store bounds the field at `MaxSequence = 1 << 40`, so that seed sits about 1600x over the ceiling and **every report is silently rejected**, because reports spawn with `stdio: "ignore"` and nobody reads their exit codes. Omitting the field also makes the reload hazard the seed existed for structurally impossible.

**Do not claim to be Herdr.** No `HERDR_*` variables. Sidecar and Herdr nest, and claiming the other's identity is a real collision.

**`|| true` on any hook event whose exit code the provider reads as a refusal.** Mastra Code reads exit code 2 on `PreToolUse`, `Stop` and `UserPromptSubmit` as the hook refusing the agent's own work, so those three commands end `|| true`. A hook surface must fail open: a Sidecar that is missing, refusing or slow may never change what the provider does. Antigravity gets the same guarantee a different way, because the `; printf '{}\n'` it needs anyway means the exit status the provider sees is printf's.

**Drop an upstream argument Sidecar's verbs do not have, rather than inventing a flag for it.** Herdr's assets pass `--session-start-source startup|new|resume` and a `--agent` beside the pane id. `report-session` has neither and wants neither: a session id names the same conversation whether it arrived at startup, on resume, or after a compact, and the kind is `--kind`. Keep the provider's own branching if it gates *whether* to report (Hermes reports `on_session_reset` and `pre_llm_call` as well as `on_session_start`, and only for interactive platforms); drop only the label it would have carried.

**Carry the attribution.** Herdr is Apache-2.0; `upstream/LICENSE` and `upstream/NOTICE` travel with the copies, and a Sidecar asset carries the attribution header the existing assets carry.

## 5. Ownership: the marker and nothing else

An installer only ever removes changes Sidecar made, identified by the `sidecar-integration:` marker (`markerToken` in `internal/agentintegration/install.go`) and by nothing else. The same token reads with `//` in JavaScript and `#` in TOML.

Consequences you must implement rather than assume:

- Refuse when the parent directory is missing, if the provider's directory comes from an override that could point anywhere. Set `setupHint`; that is what turns a missing directory into a refusal instead of something install creates.
- Back up a user's file before editing it, the way the Claude installer does.
- Never copy an upstream uninstall that deletes on a marker that is not Sidecar's own. Herdr's `uninstall_pi` deletes without checking its marker and its `remove_legacy_pi_extension_from_omp_dir` deletes a file carrying `HERDR_INTEGRATION_ID=pi`. Neither is copied.
- Uninstall gives the file back byte for byte where it can. `newFileMembers` go away only when they are all that is left and each still holds the value Sidecar wrote.
- One hole is a limit rather than a bug and is recorded, not worked around: ownership reads the first word of a command, so a hook that *wraps* Sidecar's command in a script of the user's is not recognised. Adopting it would be worse.

`sidecar agent integration status|install|uninstall|repair <provider> [--dry-run] [--json]` must work through the existing CLI with no CLI change beyond registration.

## 6. Register the port everywhere

A port is not done until every one of these carries it. Sibling lanes port other providers concurrently, so **append at the end** of every list; that makes the merge trivial.

| Registry | File | Guard |
| --- | --- | --- |
| The adapter | `internal/agentintegration/install.go`, `DefaultAdapters()` | everything below |
| The trusted source id | `internal/agentsession/trust.go`, `OfficialSources()` **and** `OfficialSourceFor()` | a reference is `Reported` and auto-resumable only from an official source |
| Provenance | `internal/agentintegration/portedfrom.go` | `TestEveryAdapterRecordsWhatItWasPortedFrom`, `TestEveryPortedFromNamesAVendoredUpstreamVersion`, `TestARenderedReportNeverCallsAPortedProviderUnported` |
| Asset golden | `internal/agentintegration/asset_golden_test.go`, `assetGoldens` | `TestEveryBundledAssetMatchesItsRecordedGolden` |
| Argv corpus | `internal/cli/agent_asset_argv_test.go` | `TestBundledAssetsSpawnArgvTheShippedCLIAccepts` |
| Capability entry | `internal/agentlifecycle/capabilities.json` | `TestCapabilityMatrixCannotClaimUnearnedAuthority` |
| Trace provenance row | `internal/agentlifecycle/testdata/README.md` | `hooktrace_test.go` |
| Capability matrix prose | `docs/reference/agent-lifecycle-capability-matrix.md` | reviewer |
| Plan result subsection | `docs/plans/implemented/herdr-parity-close-the-gap.md`, under Slice 2 or Slice 4 | reviewer |
| Changelog bullet | `CHANGELOG.md`, under `## [Unreleased]` | reviewer |

Shape C needs no argv-corpus entry of its own: `SessionHookArgvCorpus()` discovers the group by the promoted `sessionHookIntegrationData()` method, so a registered session-hook adapter is covered without being named. The same discovery drives `sessionHookAdapters` in `sessionhook_test.go`, so your provider joins nine shared tests the moment it is in `DefaultAdapters()`. Check that it did: if your provider does not appear as a subtest of `TestASessionHookInstallsAndUninstallsCleanly`, something about the embedding is wrong. That helper used to be a type switch naming four providers while the group had grown to eight, and four ports sat outside every assertion in the file until it was fixed.

Two more places a new adapter shows up and will need the assertion updated: `internal/configui/agent_integrations_test.go` carries the installed-provider list and count, and `internal/agentsession/trust.go`'s `Roots.For(kind)` needs the provider's approved session-store roots if Sidecar is to bind a conversation by path.

## 7. Earn the tier; never copy it

Herdr's authority table says what is *achievable*. It is a target and nothing more.

Four tiers, defined in `internal/agentlifecycle`:

| Tier | What it means |
| --- | --- |
| `full` | a fresh same-run report authors `working`, `blocked` or `idle`; screen evidence is diagnostic and cannot reverse it |
| `advisory` | a fresh event may confirm what the screen sees, or speak when the screen has no opinion, but never contradict it |
| `session-identity` | the source names the session reliably but not its state; screen and process detection stay the sole lifecycle authority |
| `screen-fallback` | no valid integration evidence applies |

`Capability.TierFor(status, providerInRange)` polices **every** boundary, not only the top one, and it demotes rather than audits:

- `full` needs `evidence: real-trace` **and** every transition in `FullLifecycleTransitions()`: `work_start`, `blocked_on_request`, `unblocked`, `turn_complete`, `cancelled`, `session_identity`, `process_exit`. Otherwise it falls to `advisory`.
- `advisory` with `evidence: none` or an empty `covered` falls all the way to `screen-fallback`, because advisory still authors state when the screen is undecided.
- `session-identity` that does not actually cover `session_identity` falls out too.
- Only `screen-fallback` needs no evidence, because it asserts nothing.

So **`docs-only` evidence may hold `session-identity`**: the tier needs `covered` to name `session_identity` and needs evidence to be something other than `none`, and a port whose whole claim is "this entry reports the conversation id" can state that from a published contract. What it may not do is author state. That is why the honest shipping position differs by shape:

- A **shape C** port with a documented session event ships at `session-identity` on `docs-only` and is promoted to `real-trace` by one capture. Antigravity, Copilot and Cursor are that.
- A shape C port whose session event has not been seen at all ships at **`screen-fallback` on `docs-only`**, with the first known gap saying in its own words what a capture would change and that nothing else would have to. Devin, Droid and Qoder are that, each because its installer needs an account.
- A **shape A or B** port authors state, so it stays at `screen-fallback` until traces exist, then goes to `advisory`. `full` is usually unreachable for a port that keeps upstream's provider half.

Write the entry to `internal/agentlifecycle/capabilities.json` with `schemaVersion`, `provider`, `source`, `assetVersion`, `tier`, `evidence`, `minProviderVersion`, `testedProviderRange`, `covered`, `knownGaps`, `orderingGuaranteed`, `sourceDoc` and `sourceVersionNote`. `knownGaps` is prose and is where the port is honest: say which transitions are unreachable, whether each is unclaimed *by choice* or *unclaimable*, and what a future asset version would have to do to move it. Compare `qwen` (a ceiling reached by construction) and `omp` (two gaps that differ in direction) for the register.

Then write the prose companion section in `docs/reference/agent-lifecycle-capability-matrix.md`: a **Source** line naming the release read and the Herdr version, then a heading per finding, then a "Why `<tier>` is the ceiling" heading. Add your row to the Summary table.

**`TestHerdrAuthorityGaps` closes a row only at `full`.** `sidecarTierRank` gives `advisory` the same rank as `screen-fallback`, so a provider whose ceiling is `advisory` stays listed with its tier, and that is a pass. Do not tune the rule to empty the list.

## 8. Capture traces, if you can

A tier above `session-identity`-on-docs is earned by sanitized traces from a **released** provider version, checked in under `internal/agentlifecycle/testdata/traces/<provider>/` with a provenance row in `internal/agentlifecycle/testdata/README.md`, and re-derived by tests in `hooktrace_test.go`. If the provider needs credentials the environment does not already export, stop, ship at the tier the rules allow, and say exactly why in the capability entry.

The six-column hook layout `readHookTrace` reads is `offset_ms`, `event`, `session`, `turn`, `tool`, `fields`. Sanitization is **by construction**: the tracer records event names, discriminators and payload field *names*, never prompt text, response text, tool arguments, results, paths, identifiers or environment values. Session ids are mapped to `session-N` placeholders inside the tracer process. A value is recorded only for a key whose vocabulary is closed by the provider's own source, and `TestNoHookTraceCarriesAValue` enforces `valueBearingTraceKeys` as an allowlist. Widening it is a deliberate act.

A trace whose value is an **absence** needs a `# capture-window: 18s` comment row when the interval is a watch; it needs none when the interval is delimited by two rows a reader can see.

There is no `-update` flag and there is not going to be one.

### The cheapest requalification

For a session-identity port the whole claim is one event carrying one field. If the provider fires that event **before authentication**, the capture costs one process start rather than a credentialed model turn. Qwen does: the proof pane was sitting on its provider picker with no auth type selected when the hook had already run. Check this first for any untraced provider.

### Proof-run hazards, every one of which was found the hard way

**tmux.** Read this before you type any tmux command.

- **Never run `tmux kill-server`, at all, on any socket.** A lane agent ran it and destroyed the maintainer's default server, and every live Sidecar and agent session on it, because `$TMUX` was inherited and the socket it named was the default one.
- **`unset TMUX` at the top of every script that touches tmux**, and in the env file every command sources. `cmd/sidecar/main.go` dispatches `cli.Run` **before** `main` unsets `TMUX`, deliberately and by comment, so a CLI-driven proof started inside a tmux pane talks to the socket named in `$TMUX` no matter what `TMUX_TMPDIR` says. `scripts/tmux-drive.sh` is unaffected because it launches the TUI.
- **Every tmux call carries an explicit `-S <absolute socket path>`.** Not `-L`, not an inherited default.
- **Clean up only with `tmux -S <socket> kill-session -t <name>`**, after listing the sessions on that socket to confirm what you created. Nothing else.
- Unix socket paths are capped near 104 bytes on macOS and the scratchpad path is too long. Use a short private directory such as `/private/tmp/sc-<lane>-$(id -u)` for the socket only; state and config still go under the scratchpad. Remove that directory afterwards.

**Sidecar state.** A private socket does nothing to isolate state. Set `XDG_STATE_HOME` under your scratch tree, `SIDECAR_ISOLATED_STATE=1` (which makes the binary refuse to start rather than touch the real tree), and `-config <scratch>/config.json`. `XDG_CONFIG_HOME` moves nothing for Sidecar: config and `state.json` are `$HOME`-based, so `-config` is the only lever. Run `./scripts/tmux-drive.sh paths` and confirm nothing resolves under `~/.local/state/sidecar` or `~/.config/sidecar`.

**Scrub every `SIDECAR_*` variable, not only `TMUX`.** `SIDECAR_TMUX_SERVER` leaking from an outer Sidecar shell made every hook report fail with `lifecycle_invalid_context`, silently, because a hook surface fails open.

**macOS `path_helper` reorders `PATH` inside a new tmux pane**, putting `/etc/paths.d` entries ahead of an inherited `PATH`, so a `SIDECAR_BIN` shim placed first loses to the Homebrew `sidecar`. Re-export `PATH` **in the pane** and check `command -v sidecar` before trusting a report.

**The `-config` shim.** An asset spawns `$SIDECAR_BIN agent report` with no `-config`, and `ConfigPath()` has no environment override, so a bare report under `SIDECAR_ISOLATED_STATE=1` refuses. Use a one-line shim at `SIDECAR_BIN` that adds `-config` and passes argv through. Sidecar publishes `SIDECAR_BIN` from `os.Executable` with symlinks resolved, so the pane has to export the shim **over** the published value; being invoked as the shim is not enough.

**Move every XDG directory as well as the provider's own override.** A provider's config-dir override is a statement about where it *reads*, not a promise about where it *writes*. Kilo created its XDG default directories regardless of `KILO_CONFIG_DIR`, and one marker file landed in the maintainer's real `~/.config/kilo`. Move `HOME` where you can; move `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` always.

**Export the provider's own directory override before the first `integration status` call, not after.** `detectProviderVersion` runs `<provider> --version`, and a provider that initialises its home on any invocation would write into the real tree from that first probe.

**Use a binary you built** when proving anything that reads another process's environment. macOS gates the environment section of `kern.procargs2` by *binary*: a SIP-protected platform binary such as `/bin/sleep` withholds it from everyone, same uid included, while an ordinary binary's is readable cross-process. A stand-in from `/usr/bin` reads empty for a reason that has nothing to do with the code, which is how a correct feature nearly got deleted as broken.

**Uninstalling alone does not flip the lane.** `StoreSource.Evidence` derives `integrationStatus` from the record, never from the installed files, so stored reports keep authority while `integration status` already says `not-installed`. The run must **end** as well; then you get `authority=screen`, `fallbackReason=process_generation_mismatch`.

**Never set `HERDR_ENV`.** With it set, Herdr's asset and Sidecar's would both claim the pane.

**Install through the CLI, not by hand.** `sidecar agent integration install <provider>` so the run proves the installer, its refusals and its mkdir.

**Check `$HOME` afterwards.** Create a marker file before the run and `find ~ -maxdepth 3 -newer <marker>` after it.

## 9. Run the guards

```bash
go build ./...
go vet ./...
go test ./internal/agentintegration ./internal/agentlifecycle ./internal/cli
go test -p 2 ./internal/tools/herdrsync    # never ./internal/tools/...
```

The guards that specifically gate a port:

| Test | What it catches |
| --- | --- |
| `TestEveryAdapterRecordsWhatItWasPortedFrom` | an adapter with no provenance row |
| `TestEveryPortedFromNamesAVendoredUpstreamVersion` | a row naming a version the lock does not carry |
| `TestEveryBundledAssetMatchesItsRecordedGolden` | the asset's bytes changed |
| `TestBundledAssetsSpawnArgvTheShippedCLIAccepts` | an argv the real flag parser, report construction, `Validate` and a real store append would reject. This is the seam that hid the `--seq` overflow |
| `TestEveryAdapterDeclaresWhatItOwnsAtEveryPathItTouches` | an adapter that touches a path it does not report on |
| `TestEveryAssetIsAtAPathTheAdapterAlsoReportsOn` | the mirror of the above |
| `TestAnAssetThatDeclaresNoOwnershipIsNotSidecarsFile` | an asset claiming a file it does not own |
| `TestTheMarkerRuleIsNeverRunAgainstAFileTheUserOwns` | the marker rule applied where it must not be |
| `TestAPreviewsAfterStateMatchesWhatTheOpActuallyDoes` | a `--dry-run` preview that describes a different outcome from the apply |
| `TestASymlinkedSessionIdentitySettingsFileIsRefusedUnwritten` | writing through a symlink instead of refusing |
| `TestCapabilityMatrixCannotClaimUnearnedAuthority` | a tier the entry's own evidence does not support |
| `TestHerdrAuthorityGaps` | prints the gap list; your provider leaves it or shows its earned tier |
| `TestVendoredIntegrationAssetsMatchLock` | a hand-edit under `upstream/` |

## 10. Review checklist

Go through this before handing the port off.

- [ ] **The provider half was read row by row against Herdr's own source**, and every place Sidecar diverges is named with its evidence. A divergence with no stated reason is a transcription error waiting to be found.
- [ ] **Every claim about the provider was checked against a released build** where a released build could be run, and the entry says which claims are Herdr's word rather than the provider's.
- [ ] **Goldens were not regenerated to make a test pass.** A golden moving means the shipped bytes moved: bump the asset version constant first, then `assetVersion` in `capabilities.json`, then the checksum. `bumpInstructions` in `asset_golden_test.go` states the order.
- [ ] `TestAPreviewsAfterStateMatchesWhatTheOpActuallyDoes` passes for your provider. A preview that lies is worse than no preview.
- [ ] The **symlink refusal** covers your adapter: `TestASymlinkedSessionIdentitySettingsFileIsRefusedUnwritten`.
- [ ] The **environment override** is honoured only where the provider's own code path reads it, is tilde-expanded through `overrideDir`, and is **not** expanded where the provider itself refuses a relative path (Claude, OMP).
- [ ] **`--seq` appears nowhere** in the asset or the entry.
- [ ] The **`|| true` guard** is on exactly the events whose exit code the provider reads as a refusal, and on no others.
- [ ] Ownership is by the `sidecar-integration:` marker and by nothing else, and uninstall gives a user's file back byte for byte.
- [ ] Every registry in step 6 has an appended entry, and the append is at the **end** of each list.
- [ ] A shape C port appears as a subtest of the nine `sessionhook_test.go` tests. If it does not, the embedding is wrong.
- [ ] The capability entry's tier is what the evidence earns, and `TestHerdrAuthorityGaps` is left alone.
- [ ] Any live proof used `-S <absolute socket>` on every tmux call, ran no `kill-server` of any kind, cleaned up with `kill-session -t <name>` only, and confirmed `$HOME` unchanged against a marker file.
- [ ] The plan, the capability matrix, `CHANGELOG.md` and the trace README all say what shipped.

## 11. What remains, as of Slice 7

**No Herdr integration exists at all**, so there is nothing to port: `amp`, `muse`, `cline`, `kiro`, `maki`, `gemini`. (`gemini` is additionally a declared-out family: Antigravity replaced it.)

**Ported and traced:** `opencode` (`full`), `pi`, `kilo`, `kimi`, `omp`, `mastracode` (all `advisory`, all at their ceiling), `grok`, `qwen` and `hermes` (`session-identity`, `real-trace`).

**Ported and untraced, each pending an account or credentials this environment does not carry:** `devin`, `droid`, `qodercli` (all at `screen-fallback` on `docs-only`; one `SessionStart` capture promotes each and nothing else changes), and `antigravity`, `cursor`, `copilot` (at `session-identity` on `docs-only`). cursor-agent stops at "Press any key to log in" once `HOME` moves and its hook loader has no override; agy opens a browser OAuth flow that times out; copilot is not installed anywhere Sidecar has surveyed.

**Not ported yet:** nothing. Every Herdr integration with a counterpart in Sidecar's catalog is ported. What is left is not porting work: it is a capture for each of the six untraced entries above, and subscription for the providers whose ceiling is higher than what their asset asks for.

Read `docs/plans/implemented/herdr-parity-close-the-gap.md` for the current state; its result subsections record what each port measured and are the reason most of this file exists.

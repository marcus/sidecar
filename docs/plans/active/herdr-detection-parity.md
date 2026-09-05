# Herdr detection parity: run their manifests, sync automatically, keep our edge

**Status:** Phases 0 through 5 implemented and merged (2026-09-01 and 2026-09-02). Phase 6 is superseded by [Herdr parity: close the remaining gap](../implemented/herdr-parity-close-the-gap.md), which is the controlling plan for the hooks lane and the remaining coverage. Phase 0: vendored tree with lock, regex compatibility and read window recorded in [the reference document](../../reference/herdr-detection-parity.md), alias parity fixed. Phase 1: engine, 36 ported conformance tests, `explain --file`, differential harness. Phase 2: all ten providers detect through the manifests; the Go rule tables, `Evaluate`, and the shadow flag are deleted.

Phase 3: the weekly sync workflow opens or updates one `bot/herdr-sync` review with `report.md` as its body, runs the Go suite itself (a pull request opened with `GITHUB_TOKEN` triggers no `pull_request` workflow, and Go CI's path filter covered neither the manifests nor the vendored assets), runs the differential harness against the pinned release binary, and fails when an agent has been newer upstream for two weeks with no review open. Herdr's 34 integration assets are vendored under `internal/agentintegration/upstream/` with their own lock and a `ported-from` record per Sidecar asset, so a bump is a diff review. Local overrides land at `~/.config/sidecar/agent-detection/<file>.toml` with Herdr's precedence. The fixture verdict-flip table and the overlay-redundancy flags are real, and the redundancy check has already retired one rule. **Exit gate met:** the workflow ran, and its first review was a partial rollback its own Go suite rejected, which is the gate working rather than failing. The second run vendored Herdr `master` at `d08e4468` with every manifest byte unchanged and an empty verdict-flip table, and merged as #325.

Phase 4: ten detection-only families registered as a parallel list, all twenty families' aliases resolving, `TestHerdrAuthorityGaps` listing kilo, kimi, mastracode, omp and pi, and a test pinning that every vendored manifest is registered or declared unregistered. Twenty of Herdr's twenty-one screen-manifest agents have a Sidecar identity; `gemini` is the declared exclusion.

Phase 5: `detection.remoteManifests` defaults to `off` and means it. With it on, the fetch runs in a `tea.Cmd` after the first frame at most once a day, under Herdr's own rules, with local overrides still on top and the Sidecar overlay merged onto the fetched file. `sidecar agent manifests` prints the vendored, remote and active version per agent, with `--refresh` and `--clear-cache` as the non-interactive way out.

**Known limitation, measured in Phase 4:** for an agent installed as a plain `#!/usr/bin/env node` shim, tmux reports `node` and the darwin argv scan returns argv[0] only, so neither identity input names the agent and the pane is never claimed. Herdr reaches those by scoring the process tree past its generic-runtime list; Sidecar matches argv[0] basenames. An agent that renames its own process, as Claude Code does, is unaffected. Widening `Identify` is not in this plan.

**Research baseline:** Sidecar `main` at the head of `claude/tui-lifecycle-herdr-parity-cu2d9t`; Herdr `e2b85c7` (`preview-2026-08-31`, ten commits past v0.8.2), the live catalog at `https://herdr.dev/agent-detection/index.toml`, and Herdr's Apache-2.0 `LICENSE`.

Related plans:

- [Deterministic agent lifecycle hooks](notification-agent-lifecycle-hooks.md) owns provider hooks, the report contract, authority arbitration, and the capability matrix. This plan does not touch the resolver or the tiers; it changes what the *screen* lane feeds into them, and it adds a Herdr-relative target for the *hooks* lane.
- [Herdr agent control and session restore](herdr-agent-control-and-session-restore.md) owns `sidecar agent start/prompt/wait/read`. Those verbs consume `agentactivity.Result`; they inherit anything this plan improves without change.
- [Workspace agent activity status (td-48ecf2)](../implemented/td-48ecf2-workspace-agent-activity-status.md) built `internal/agentactivity` by hand-porting four Herdr manifests. It said: "A Go data table is acceptable for v1; use embedded TOML only if re-harvested rule churn proves that data updates are materially easier than code review." This plan is the finding that the churn has proved it.
- [Adding new agent CLIs](../../guides/active/adding-new-agent-clis.md) is the seven-subsystem guide. Phase 4 changes Step 2 of that guide from "write a Go rule table" to "vendor a manifest and register an alias".

## One sentence

**Sidecar should execute Herdr's published detection manifests directly instead of hand-translating them, pull them on a schedule with a review gate that shows exactly which of our own fixtures changed verdict, layer Sidecar-only improvements on top as data in the same grammar, and measure the hooks lane against Herdr's per-agent authority table so falling behind is a failing check rather than a feeling.**

## Why now

Sidecar detects agent state in two lanes. The hooks lane is deterministic where a provider ships a complete lifecycle contract; today that is OpenCode only, with Codex, Claude, and Pi at session-identity. The screen lane, `internal/agentactivity`, covers everything else and is the permanent fallback. It was built by reading Herdr's TOML manifests and re-expressing each rule as a Go `Rule` literal. That worked for a one-time port and has been losing ground since:

- **Herdr's manifests are data and ours are code.** Herdr ships 21 screen manifests (122 rules at `e2b85c7`) and republishes them to `herdr.dev` so released binaries pick up rule fixes without a release. Sidecar has 10 provider files, each a hand-written approximation of a Herdr manifest pinned at a date in a comment (`amp 2026.07.09.1`, `copilot 2026.07.07.1`, `cursor 2026.08.03.1`, `grok 2026.07.16.2`, `opencode 2026.06.10.1`, `pi 2026.06.10.1`). Herdr's `claude.toml` is at `2026.08.29.1`, `codex.toml` at `2026.08.28.1`, `github-copilot.toml` at `2026.08.29.1`. Every one of those bumps is a fix we have to notice, read, translate, and test by hand.
- **Concrete drift exists today.** Claude Code 2.1.228 switched its busy title spinner from braille to half-circle glyphs (U+25D0–U+25D3). Herdr's `osc_title_working` matches both; Sidecar's `claudeTitleWorking` matches only braille, so on a current Claude the title rule falls through and idle detection has to carry the turn. Herdr also has rules Sidecar has no equivalent for: Claude's MCP elicitation dialog, `/btw` overlay, `Waiting for N background agents`, `N MCP tasks still running`; Codex's trust-directory prompt (a top-of-buffer region), prompt-marker-relative regions that stop stale scrollback from producing false blockers, and a working fallback that survives customised static titles.
- **Herdr covers 23 agents; Sidecar covers 10.** Missing on our side: Gemini CLI, Cline, Devin, Droid, Kimi, Kiro, Kilo, Hermes, Qoder, Qwen, Maki, and OMP. Adding one today costs a Go file, a process-name case, a dispatch case, a `Supports` case, a catalog family, theme colours in every curated theme, and fixtures. Most of that cost is not detection.
- **Their grammar is small and closed.** Four states, integer priority with first-match-wins on ties, fifteen named regions, three matcher kinds (`contains`, `regex`, `line_regex`), three gate kinds (`all`, `any`, `not`) with a depth cap of eight, three `visible_*` flags, `skip_state_update`, and a validator with hard limits (128 rules per manifest, 512 gates, 1024 matchers, 512 chars per matcher). Sidecar's `Rule` already models a subset of it; the missing pieces are regions and nested gates. Executing the grammar is less code than we already spend approximating it.

The trend outside both projects points the same way. Prime Agent ships a built-in Herdr state reporter; ClawTab publishes a dated per-provider hook compatibility matrix and says outright that "hook support changes faster than tmux itself". Agents and the tools around them are converging on Herdr's vocabulary. Riding that is cheaper than racing it.

## Decision first

1. **Execute Herdr manifests verbatim.** Add a manifest engine in Go that implements Herdr's manifest grammar at engine version 3 with identical semantics, validates with the same limits, and produces an `explain` record with the same fields Herdr's does. It replaces the per-provider `Rule` tables as the screen-lane classifier. The engine is state-free; the `Tracker` (debounce, `done`, `IdleInferred`, skip retention cap) and the `agentlifecycle` resolver are untouched.
2. **Vendor by default, fetch on opt-in.** Herdr's manifests are committed under a Sidecar path with a lock file recording upstream commit, catalog ETag, per-file digest, and manifest version. A sync tool refreshes them from both the Herdr repository and the live catalog. Runtime fetch from the catalog is an opt-in setting (Phase 5), off unless the user turns it on.
3. **Sidecar improvements are overlays in the same grammar.** Anything Sidecar knows that Herdr does not lives in `sidecar/<agent>.toml`, merged by rule id over the vendored manifest. An overlay can add rules, replace a rule, raise or lower a priority, or disable a rule. The vendored files are never edited, so a re-sync is a clean file replacement and the overlay's diff against upstream is the exact list of things we believe we do better.
4. **The fixture corpus is the parity gate.** Sidecar has real, sanitized screen fixtures with expected verdicts for ten providers; Herdr deliberately keeps none (its `AGENTS.md` says to tune rules against live panes, not fixture suites). Every sync runs the corpus against the old and new manifests and the review shows only the verdict flips. That corpus, plus a differential run against a real Herdr binary, is what "parity" means here.
5. **Measure the hooks lane against Herdr's authority table.** Herdr publishes which agents have lifecycle authority through hooks (Pi, OMP, Kimi, OpenCode, Kilo, MastraCode) and which are session-identity only (eleven more). Record that table beside `capabilities.json` as a *target*, and let a test list every provider where Sidecar's proved tier is below Herdr's. The existing plan's rule stands: a tier is earned by traces, never copied. The target only makes the gap visible.
6. **Process identity stays code, tracked by extraction.** Like Herdr, adding a brand-new agent still needs a binary change for process names and labels. The sync tool extracts Herdr's alias table from `src/detect/mod.rs` and its generic-runtime list, and a test asserts Sidecar's `identifyProcessName` recognises every alias for every family Sidecar claims.

What this plan does **not** do: it does not remove screen detection's role as the permanent fallback, does not change the hooks contract or the resolver, does not add a daemon or a listening socket, does not fetch from the network at runtime unless the user opts in, does not install Herdr's hook assets unmodified (they speak Herdr's socket API; Phase 6 ports them to speak `sidecar agent report`), and does not make a Sidecar shell claim to be Herdr through `HERDR_*` environment variables. It also does not adopt Herdr's positional pane ids, `done` semantics, or aggregate rollups; Sidecar's are already at or ahead of parity there.

## What Herdr actually has (research record)

Recorded so the later phases have a fixed reference. Paths are in the Herdr checkout at `e2b85c7`.

**Manifest grammar** (`src/detect/manifest.rs`, `scripts/agent_detection_manifest_check.py`):

| Element | Semantics |
| --- | --- |
| `id`, `version`, `min_engine_version`, `updated_at`, `aliases`, `rules` | Only these top-level keys; unknown keys reject the file. `version` is dotted numeric, compared segment-wise. Remote manifests must declare `min_engine_version`; a file requiring a newer engine is ignored. |
| `rules[].state` | `idle`, `working`, `blocked`, `unknown`. `skip_state_update` is only valid with `unknown` and no `visible_*` flag. |
| `rules[].priority` | Integer. Every rule is evaluated; the highest priority match wins; on a tie the earlier rule keeps it. No match on a known agent falls back to `idle` with reason `default_known_agent_idle_fallback`. |
| `rules[].region` | `whole_recent`, `whole_recent_without_current_prompt_marker`, `after_last_prompt_marker`, `before_current_prompt_marker`, `current_prompt_block_marker`, `after_current_prompt_block_marker`, `prompt_box_body`, `above_prompt_box`, `last_non_empty_above_prompt_box`, `after_last_horizontal_rule`, `osc_title`, `osc_progress`, `bottom_lines(N)`, `bottom_non_empty_lines(N)`, `top_non_empty_lines(N)` (engine 3). The prompt-marker regions are Codex-shaped (`›` composer line); the prompt-box regions are Claude-shaped (horizontal-rule box). |
| Matchers | `contains` is case-insensitive over the region; `regex` matches the region text; `line_regex` matches if any single line matches. All matchers in a gate must hold. |
| Gates | `all` (every nested gate), `any` (at least one), `not` (none). Nest to depth 8. |
| Limits | 128 rules, 512 gates, 32 matchers per gate, 1024 matchers, 512 chars per matcher. |

Usage across the 21 bundled manifests: 43 rules on `whole_recent`, 16 on `osc_title`, 4 on `osc_progress`, 2 on prompt-marker regions, 3 on prompt-box regions, 2 on `after_last_horizontal_rule`, 2 on `top_non_empty_lines`, the rest on `bottom_non_empty_lines(N)` for N in 1–30. Engine version: 14 manifests at 1, 5 at 2 (OSC regions), 2 at 3.

**Distribution** (`distribution/agent-detection/`, `src/detect/manifest_update.rs`): a `schema_version = 1` `index.toml` lists `{id, path}` per agent; clients fetch each file (256 KiB cap), validate, and cache under the state directory. Precedence is local override (`~/.config/herdr/agent-detection/<agent>.toml`) over the newer of cached-remote and bundled. Published and bundled copies may intentionally differ behind an exact version-and-digest exception (today: `grok` bundled at `2026.07.16.2` on engine 3 versus published `2026.07.16.1` on engine 2, so the bundled copy wins; `muse` bundled but unpublished until a stable release ships its process identity). Their validator enforces that these exceptions are explicit.

**Process identity** (`src/detect/mod.rs`): a single `lookup_agent` alias table (for example `"claude" | "claude-code"`, `"cursor" | "cursor-agent"`, `"opencode" | "opencode2" | "open-code"`, `"qodercli" | "qoderclicn" | "qoder" | "qodercn"`, `muse-bin-<digit>…`), a generic-runtime list (`sh bash zsh fish tmux node bun cmd powershell pwsh` plus `python[3[.N]]`), argv-based unwrapping of runtimes, and a foreground-process-group scan that prefers the group leader and scores non-runtime matches higher. `HERDR_AGENT=<agent>` on a wrapper command names the manifest to use when a sandbox hides the process.

**Hooks and authority** (`docs/preview/website/src/content/docs/agents.mdx`, `integrations.mdx`, `src/integration/registry.rs`): 17 installable integrations. Lifecycle authority through hooks: Pi, OMP, Kimi, OpenCode, Kilo, MastraCode. Session identity only, state from screen: Claude, Codex, Copilot, Devin, Droid, Qoder, Qwen, Cursor, Hermes, Antigravity, Grok. No integration: Amp, Kiro, Maki, Muse, Gemini, Cline. Integration assets carry `HERDR_INTEGRATION_VERSION` (at `e2b85c7`: Claude 9, Codex 8, Pi 8, Kimi 7, Hermes 5; the v0.8.2 docs still list Claude 6); the changelog shows those bump several times a month as providers change their hook payloads. Reports go over a Unix socket as `pane.report_agent` / `pane.report_agent_session` / `pane.release_agent` with a per-source `seq`; the pane learns its identity from `HERDR_ENV`, `HERDR_PANE_ID`, `HERDR_BIN_PATH`, `HERDR_SOCKET_PATH`.

**Diagnostics**: `herdr agent explain <target>` and `herdr agent explain --file screen.txt --agent codex --json` print the manifest source and version, matched rule, every evaluated rule with region evidence, visible flags, skip reason, and fallback reason. `herdr agent read <pane> --source detection --format text` prints exactly the text detection saw. Both are what make manifest tuning a five-minute loop in their `AGENTS.md`.

**Churn**: six commits touched `src/detect/manifests/` between 2026-08-20 and 2026-08-31, and the 0.8.x changelogs list a detection or hook fix in nearly every release (Claude half-circle spinner, Qwen locale-independent titles, Cursor "Run Everything" false blocker, Codex reasoning-summary headers, Kiro positive idle, Copilot `ask_user`, versioned Python wrappers). That is the rate a hand-port has to keep up with.

**What Sidecar already does better, and keeps**: a real fixture corpus with provenance; `Tracker` semantics (`done = idle && !seen`, `IdleInferred` so inferred idle never announces completion, a two-minute cap on overlay retention); the `agentlifecycle` capability matrix with per-source tested version ranges and evidence tiers (stricter than Herdr's per-agent table); host-scoped identity in the report contract; and `sidecar agent explain` already reporting the authoritative source and fallback reason. None of that sits in the manifest layer, so none of it is at risk from this plan.

## Terminal constraints the engine must respect

- **`osc_title` maps to tmux `#{pane_title}`.** Sidecar already captures it atomically with the screen. Rules on `osc_title` work unchanged.
- **`osc_progress` is empty under tmux.** tmux consumes OSC 9;4 and exposes no payload, so the region always resolves to `""`. Four upstream rules (Claude `osc_progress_idle` and three others) can never match in Sidecar; the engine records that as region evidence rather than pretending. This is a known, permanent gap, already noted in `grok.go`.
- **Screen text is the SGR-stripped capture.** Herdr evaluates plain text from its own terminal model; Sidecar evaluates `capture-pane -e` output after `ansi.Strip`. Both engines see what a human sees. Phase 0 confirms there is no width, wrapping, or trailing-whitespace difference that flips a verdict, using the differential harness.
- **`whole_recent` needs a defined window.** Herdr reads the recent bottom of the buffer, not the user's viewport. Sidecar's capture includes up to 600 lines of history. Phase 0 measures Herdr's detection read window on a live pane and the engine trims trailing blank rows then bounds the tail to that many lines. `top_non_empty_lines(N)` (Codex trust prompt) reads from the top of that same window, so the window has to be the same one Herdr uses or the trust prompt will scroll out of it at a different moment.
- **`HERDR_AGENT` has a Sidecar equivalent.** Managed shells already know their launch agent kind; a `SIDECAR_AGENT` hint on a wrapper command covers the sandbox case the same way, and it is a process-identity hint only, never a lifecycle claim.

## The journeys this plan must make real

### 1. A Herdr rule fix lands in Sidecar without anyone translating it

Herdr publishes `claude.toml 2026.09.03.1` adding a new permission-prompt shape. The Monday sync run opens a review with three things in it: the manifest diff, the lock-file bump, and a table of Sidecar fixtures whose verdict changed under the new file (expected: none, or a listed fixture that was wrong before). A maintainer reads the diff, sees no flips, merges. The next Sidecar release detects the new prompt. Nobody wrote a regex.

### 2. A Sidecar-only improvement survives the next sync

We notice Claude's `AskUserQuestion` renders with a ☐ glyph Herdr does not gate on and add a higher-priority rule in `sidecar/claude.toml` with a fixture proving it. Two weeks later upstream reworks `live_blocked_form`. The sync replaces the vendored file; the overlay is untouched; the corpus run shows the fixture still passes and shows whether upstream's rework made our overlay redundant (same verdict with the overlay disabled). If it is redundant, the review says so and we delete it. If Herdr later adopts the same idea, the overlay disappears on its own.

### 3. A wrong badge is explained in one command, offline

A pane shows `idle` while Grok is clearly working. `sidecar agent explain --current --json` reports the screen lane's manifest source (`upstream grok 2026.07.16.2 + sidecar overlay`), every evaluated rule with the region text it saw, the matched rule or the fallback reason, and whether a hook source was consulted. `sidecar agent explain --file screen.txt --agent grok` reproduces it from a saved capture, which is also exactly how a new fixture is minted.

### 4. Ten more agents show a real state badge

A user launches Qwen Code in a Sidecar shell. `identifyProcessName` knows `qwen`; the engine loads the vendored `qwen.toml`; the pane shows working and blocked states with no Sidecar-specific code beyond the alias. Launch/resume support, a curated colour, and a conversation adapter remain separate, optional work per the existing guide.

### 5. Falling behind on hooks is a failing check, not a surprise

`go test ./internal/agentlifecycle/` includes a report listing each provider where Herdr has lifecycle authority through hooks and Sidecar does not (today: Pi, Kimi, Kilo, MastraCode, OMP). The test does not fail the build; it fails only when the recorded target is stale against the vendored Herdr table, so the gap is always current and visible.

## Settled architecture

### Package layout

```
internal/agentactivity/
  manifest/                 engine: parse, validate, regions, gates, evaluate, explain
  manifests/
    upstream/<agent>.toml   vendored verbatim from Herdr (never edited)
    upstream/index.toml     vendored catalog index
    upstream/NOTICE         Apache-2.0 attribution for the vendored files
    upstream.lock.json      herdr commit, catalog ETag, per-file sha256 + version, sync time
    sidecar/<agent>.toml    Sidecar overlays in the same grammar
    aliases.upstream.json   extracted from Herdr src/detect/mod.rs by the sync tool
    authority.upstream.json extracted per-agent authority table (hooks vs screen)
  activity.go               Observation, Result, Tracker unchanged; Detect dispatches to the engine
  <provider>.go             shrinks to process gating + FallbackIdle wrapper, then is deleted per provider in Phase 2
scripts/sync-herdr.sh       thin wrapper over `go run ./internal/tools/herdrsync`
internal/tools/herdrsync/   fetch, verify, extract, write lock, render the review report
.github/workflows/herdr-sync.yml   weekly + manual dispatch
```

Manifests are embedded with `embed.FS`; the engine compiles them once at first use, not in `Init()`, per the startup-latency rule. A compile error in an overlay is a startup-visible diagnostic and that overlay is skipped; a compile error in a vendored file cannot happen because the lock test compiles every vendored file in CI.

### Engine contract

- Input is `agentactivity.Observation` plus the resolved agent id. Output is `Result` (unchanged fields) plus an `Explain` value with `ManifestSource`, `ManifestVersion`, `OverlayApplied`, `MatchedRule{ID, Priority, Region, State}`, `EvaluatedRules[]{ID, Priority, Region, State, Matched, RegionBytes, RegionPreview}`, `FallbackReason`, and `SkippedUpdateReason`. Field names follow Herdr's `explain` JSON so the differential harness can diff them structurally.
- Semantics are Herdr's, not Sidecar's current `Rule`: `contains` is case-insensitive; every rule is evaluated; highest priority wins; ties keep the earlier rule; `skip_state_update` yields `Result.SkipStateUpdate` with `State == StateUnknown`; `visible_*` flags come from the matched rule, not from the state alone. This is the single largest behavioural difference from today's `Evaluate`, which stops at the first match in file order and derives visibility from state. The characterization tests in `compat_test.go` pin today's behaviour; Phase 2 changes them deliberately, per fixture, with the reason recorded.
- Regexes compile with Go's `regexp` (RE2). Herdr uses Rust's `regex` crate; both are RE2-family with no lookaround or backreferences, and the syntax used in the 122 upstream rules (`\x{2800}`, `(?i)`, `(?m)`, `(?s)`, `\A`, `\b`, Unicode classes) is common to both. The lock test compiles every regex in every vendored manifest so an incompatible pattern fails CI on the sync PR, not in a user's pane. If one ever appears, the overlay mechanism can carry a rewritten equivalent for that rule id and the report names it.
- `min_engine_version` above Sidecar's declared engine version rejects the file at sync time, not at runtime; a rule using `top_non_empty_lines` requires engine 3 exactly as in Herdr's validator.
- Fallback: a positively identified live process with no matching rule returns `StateIdle` with `FallbackIdle: true` and evidence `default_known_agent_idle_fallback`, which is what `<provider>.known-live-fallback` means today.
- Process gating stays outside the engine: `Detect` still refuses to evaluate `claude.toml` against a pane whose foreground process is not Claude or a permitted runtime wrapper. That refusal is Sidecar's and is stricter than Herdr's; it stays.

### Overlay merge

An overlay file has the same shape as a manifest with two additions per rule: `disable = true` removes the upstream rule with that id, and a rule whose id matches an upstream id replaces it. Any other rule is appended. Overlay ids are prefixed `sidecar.` so an upstream rule can never collide with a Sidecar addition by accident. The merged result is validated with the same limits as a plain manifest. The review report renders, for each overlay rule, whether it changed any fixture verdict with the overlay on versus off; an overlay rule that changes nothing is flagged as a deletion candidate.

### Sync tool

`herdrsync` does one thing per invocation and writes only under `internal/agentactivity/manifests/`:

1. Fetch `distribution/agent-detection/*.toml` and `src/detect/manifests/*.toml` at a pinned Herdr ref (default: the newest release tag, so the vendored files match the release binary the differential harness downloads; override with `--ref` to track `main`), and the live catalog `index.toml` plus every listed file from `herdr.dev` with its ETag. Cap at 256 KiB per file like Herdr does.
2. Validate every file with the ported validator. Refuse the whole sync if any file fails.
3. Per agent, choose the newer of bundled and published, exactly as a Herdr client would, and record which won and why. Where the two differ and the published one is older (the `grok` case), keep the bundled one and say so.
4. Extract `lookup_agent` and `is_generic_runtime_or_shell` from `src/detect/mod.rs` into `aliases.upstream.json`, and the authority table from `agents.mdx` into `authority.upstream.json`. Extraction is a regex over stable Rust match-arm shapes; if the shape changes the tool fails loudly and the previous JSON stands.
5. Vendor `src/integration/assets/**` under `internal/agentintegration/upstream/<provider>/` with the same lock discipline as the manifests (per-file digest and the `HERDR_INTEGRATION_VERSION` header value). Report every version bump. For a provider Sidecar has ported (Phase 6), the report includes the upstream diff since the version the Sidecar asset's header says it was ported from, so re-porting is a review of a diff rather than a re-read of the file. For a provider not yet ported, the bump is a heads-up that the provider's hook payload changed.
6. Write `upstream.lock.json` and render `report.md`: version bumps, per-file diffs, alias table additions Sidecar lacks, authority gaps, integration version bumps, and the fixture-corpus verdict flips.

A `TestVendoredManifestsMatchLock` test hashes the vendored files against the lock so a hand edit to a vendored file fails CI; edits belong in overlays.

### Workflow

`.github/workflows/herdr-sync.yml` runs weekly and on dispatch, runs `herdrsync`, and if anything changed opens a pull request from `bot/herdr-sync` with `report.md` as the body. A second job in that workflow, not in ordinary CI, downloads the Herdr release binary for the runner's platform (`herdr-linux-x86_64` from the GitHub release matching the pinned tag) and runs the differential harness (below). Ordinary Go CI never needs Rust, a Herdr binary, or the network.

### Differential harness

The harness exists to answer one question the unit tests cannot: does Sidecar's Go engine produce the same verdict as Herdr's Rust engine on *real* screens, not just on the synthetic cases in Herdr's test file? The ported conformance tests cover the grammar. They do not cover how the two engines count "non-empty lines" on a screen with trailing whitespace, where each finds the Codex prompt marker or the Claude prompt box on a wrapped screen, or how a horizontal rule made of a slightly different glyph is classified. Those are exactly the details that produce a wrong badge in a pane, and the only oracle for them is Herdr itself.

Herdr makes that oracle cheap: `herdr agent explain --file screen.txt --agent codex --json` evaluates a saved screen through its engine with no pane, no tmux, and no agent running. So for every fixture in `internal/agentactivity/testdata/**` with a `screen:` block, the harness runs that command and Sidecar's `sidecar agent explain --file <screen> --agent <agent> --json` against the *same* vendored manifest (Herdr's local override directory is pointed at our vendored copy so both engines read one file), and diffs `state`, `matched_rule.id`, and `fallback_reason`. Disagreements are engine bugs by definition and fail the sync workflow.

The binary is the release build, not a source build. That keeps the workflow free of a Rust toolchain and means the oracle is the engine Herdr's users actually run. The cost is that a manifest on Herdr's `main` may declare a `min_engine_version` the release cannot evaluate; the sync tool already rejects those at vendoring time and the report names them, so the vendored tree is always evaluable by the pinned release.

### Local overrides

`~/.config/sidecar/agent-detection/<agent>.toml`, same precedence as Herdr: a valid local override wins over vendored+overlay for that agent; an invalid one is ignored with a warning in `explain`. This gives a user the same five-minute tuning loop Herdr's `AGENTS.md` describes, and it is how a fix gets proved before it becomes an overlay or an upstream contribution. No hot reload in v1; `explain` reports the loaded source so a stale process is obvious.

### Contributing upstream

When a Sidecar overlay fixes something Herdr also gets wrong, the preferred outcome is a Herdr pull request carrying the rule and a sanitized fixture, so the overlay can be deleted on the next sync. The report's "overlay changes nothing" flag is the signal that this has happened.

## Work sequence

### Phase 0 — Vendor and measure (no behaviour change)

- Build `herdrsync` fetch/validate/lock/report. Vendor the 21 manifests, `index.toml`, `NOTICE`, `aliases.upstream.json`, `authority.upstream.json`.
- Port Herdr's validator limits and rules as `manifest.Validate`; `TestAllVendoredManifestsParseAndValidate` and `TestVendoredManifestsMatchLock`.
- Compile every upstream regex under Go `regexp`; record any incompatibility.
- Measure Herdr's detection read window on a live pane (`herdr agent read --source detection`) and record the line count for the engine's `whole_recent` bound.
- Alias parity report: which upstream aliases `identifyProcessName` does not recognise for families Sidecar already claims. Fix the ones for existing families in this phase (they are one-line cases).
- **Exit gate:** vendored tree committed, lock test green, regex compatibility report attached, read window recorded.

### Phase 1 — Engine, explain, conformance, shadow mode

- Implement regions, gates, evaluation, fallback, and `Explain` per the contract above.
- Port Herdr's 45 inline manifest tests from `src/detect/manifest/tests.rs` as a Go table; they are the grammar's executable specification.
- Run the engine over every Sidecar fixture and record verdicts beside today's `Rule` verdicts. Every disagreement is triaged into: engine bug (fix), upstream rule better (accept, update fixture expectation with reason), Sidecar rule better (write an overlay with the fixture proving it).
- Add `--file PATH --agent KIND` to `sidecar agent explain`, and have the live explain carry the screen-lane `Explain` alongside the existing authority fields.
- Ship the differential harness in the sync workflow.
- **Shadow mode** behind `features.ManifestDetection` (default off): production polls run both classifiers and log disagreements with evidence ids to the lifecycle diagnostics; nothing user-visible changes. Run it on the maintainer's machines for at least a week across the providers actually in use.
- **Exit gate:** ported conformance suite green, differential harness green over the whole corpus, shadow disagreements triaged to zero or to an overlay with a fixture.

### Phase 2 — Cutover per provider

- One provider at a time, in the order of live use (Claude, Codex, OpenCode, Cursor, Grok, Pi, Copilot, Amp, Antigravity, Muse): flip `Detect` to the engine for that provider, delete its Go rule table, keep the process-gating wrapper, update the `compat_test.go` characterization fixtures deliberately with the reason for each change.
- Overlays written in Phase 1 are the only Sidecar-owned rules left. Each has a fixture.
- Remove the feature flag once all ten are cut over.
- **Exit gate:** `internal/agentactivity/<provider>.go` contains no `Rule` literals; corpus green; live matrix (`SIDECAR_LIVE_AGENT_MATRIX`) passes for the providers it passed before.

### Phase 3 — Automation and overrides

- Land the weekly workflow with pull-request creation and the report body.
- Local override directory and its `explain` reporting.
- Integration-version bump detection in the report for OpenCode, Codex, Claude.
- A staleness check in the workflow (not in Go CI): if the live catalog's version for any agent is newer than the lock for more than 14 days with no open sync PR, the run fails so it shows up.
- **Exit gate:** one real sync PR produced by the workflow, reviewed and merged, with a verdict-flip table in its body (possibly empty).

### Phase 4 — Coverage expansion

- Add a `DetectionOnly bool` to `agentcatalog.Family` (or a parallel `DetectionFamilies` list; decide during implementation, the vocabulary tests decide which is less invasive). A detection-only family needs an id, aliases, and a display name. It is not offered in creation pickers and has no resume, adapter, or curated colour.
- Register Cline, Devin, Droid, Kimi, Kiro, Kilo, Hermes, Qoder, Qwen, and Maki as detection-only, each with its vendored manifest. Gemini is not registered: Antigravity has replaced it and `agy` is already a full family. `gemini.toml` is still vendored because the sync mirrors the whole catalog, so registering it later is one alias line. OMP is hooks-only upstream, with no screen manifest, and is out of scope until a Sidecar integration exists.
- **Scope check, since this is the phase that adds the most agents.** The expensive part of a full family today is presentation, not detection: a curated colour in all 20 curated themes plus the website palette, an icon that must match a conversations adapter, and the picker and skip-permissions tables. Detection-only families skip all of that on purpose. `styles.AgentColor` already returns `TextMuted` for an unregistered provider and `AgentIcon` returns the bare name, so a detection-only badge renders correctly with no theme work. The tests that pin full families (`TestAgentIconMatchesConversationsAdapters`, `TestAgentPickersFollowCatalog`, the curated-theme normalisation test) iterate full families only; `TestTheProcessNameVocabularyMatchesTheAgentCatalog` covers both, which is the one that matters for detection. Net per agent: one alias case, one vendored manifest already present, one family entry. Promoting any of them to a full family later is the existing seven-step guide and is independent work.
- Update the "Adding new agent CLIs" guide: Step 2 becomes "sync or vendor the manifest, add the alias, add a fixture from `explain --file`". The seven-subsystem picture stays for full families.
- Authority target test: `TestHerdrAuthorityGaps` lists providers where `authority.upstream.json` says lifecycle-through-hooks and `capabilities.json` says below `full`. It fails only if `authority.upstream.json` and the lock disagree, so the list is always current.
- **Exit gate:** every agent in Herdr's `SCREEN_MANIFEST_AGENTS` has a Sidecar identity and a manifest, except the one this plan declares out. **Corrected 2026-09-01:** that is 20 of 21, not 21. Decision 4 excludes `gemini`, so registering all 21 was never achievable and the two sentences contradicted each other. Twenty-one manifests are vendored, twenty are registered (ten launchable, ten detection-only), and `gemini` is the declared exclusion. OMP and MastraCode never enter this count: they are in Herdr's alias and authority tables but ship no screen manifest, so they are not in `SCREEN_MANIFEST_AGENTS` at all. `TestEveryVendoredManifestIsRegisteredOrDeclaredUnregistered` pins the arithmetic, so a synced-in agent fails the build until someone either registers it or records why not. A fresh fixture per new agent is not required for the cutover but the guide says how to mint one.

### Phase 5 — Opt-in runtime catalog fetch

- `detection.remoteManifests: "off" | "herdr.dev" | "<url>"`, default `"off"`. When on, Sidecar fetches the catalog index and per-agent files with Herdr's own rules: 256 KiB cap per file, validate before use, ignore a file whose `min_engine_version` is newer than the engine, cache under the state directory, and take the newer of cached-remote and vendored per agent. Local overrides still win.
- The fetch runs in a `tea.Cmd` after the first frame, never in `Init()`, and never more than once per day. A fetch failure is a diagnostic in `explain`, not a user-visible error.
- `explain` reports the remote version, the vendored version, and which one is active, so a user with fetch on can always see whether they are ahead of the vendored tree. `sidecar agent manifests` (or a flag on `explain`) prints the same table for every agent.
- The Herdr reporter compatibility shim considered during review is **not** pursued. The only agent found shipping a native Herdr reporter is Prime Agent; every other Herdr integration is a hook asset Herdr installs, which Phase 6 ports on Sidecar's terms. Claiming to be Herdr through `HERDR_*` variables buys one agent today and a real identity collision whenever Sidecar and Herdr are nested. Revisit only if a second agent ships a native reporter and a user asks for it.

### Phase 6 — Sidecar-native ports of Herdr's integration assets

**Superseded.** This phase is now owned by [Herdr parity: close the remaining gap](../implemented/herdr-parity-close-the-gap.md), which is the controlling plan for it. That document records where parity actually stands after Phases 0 through 5, names the two agents Herdr identifies that Sidecar does not and why each is absent, lists the 17 upstream integrations against Sidecar's 3, and adds the identity and launch-hint work this plan never scoped. Read it rather than this section.

The reasoning that produced this phase is unchanged and is not repeated there in full: Herdr's manifests are data, so the screen lane became generic the moment the engine executed their grammar, but Herdr beats its own manifests by installing an integration per provider, and vendoring manifests does nothing for that. Decision 7 below records why the answer is Sidecar-native ports rather than a compatibility shim.

## Test matrix

- **Grammar:** the 45 ported Herdr tests; region resolution for every named region against synthetic screens; `contains` case-insensitivity; tie-break keeps earlier rule; `skip_state_update` validation; every limit rejects at limit+1.
- **Vendoring:** lock digest test; every vendored regex compiles; `min_engine_version` above ours rejects; published-vs-bundled choice recorded in the lock matches what the tool decided.
- **Corpus:** every fixture in `testdata/**` has an expected `state`, `evidence` (rule id), and visible flags; the engine matches all of them after Phase 2; the differential harness agrees with Herdr on `state`, `matched_rule.id`, `fallback_reason`.
- **Overlays:** merge by id, `disable`, append, prefix enforcement, overlay-off comparison per rule.
- **Identity:** every alias in `aliases.upstream.json` for a Sidecar family resolves in `identifyProcessName`; the generic-runtime list is a superset of Herdr's on Linux and Darwin.
- **Tracker unchanged:** `compat_test.go` `Tracker` fixtures pass byte-identical before and after Phase 2; only the `Detect` verdict fixtures change, each with a recorded reason.
- **Explain:** `--file` reproduces a live verdict from its saved screen; JSON field names match Herdr's for the shared fields.
- **Workflow:** a dry run against a pinned older Herdr ref produces a report whose verdict-flip table matches a checked-in expectation.

## Acceptance evidence

- A sync PR opened by the workflow, with the report body, merged with no manual manifest edits.
- `git grep 'Rule{' internal/agentactivity/*.go` returns nothing.
- `sidecar agent explain --file internal/agentactivity/testdata/claude/blocked.txt --agent claude --json` and the Herdr equivalent produce the same `state` and `matched_rule.id`.
- A Claude Code 2.1.228 or newer pane shows `working` from its half-circle title spinner with no Sidecar rule authored for it.
- A Qwen Code pane shows `blocked` on its confirmation prompt with no Sidecar regex authored for it.

## Risks and how the plan bounds them

- **Semantics drift between the two engines.** Bounded by the ported conformance tests and the differential harness over real fixtures, run on every sync. If Herdr adds a region or matcher kind, the validator rejects the file at sync time and the report says which rule needs engine work before it can be vendored.
- **Regex dialect.** Bounded by compiling every upstream pattern in CI; the overlay carries a rewritten rule if one ever fails.
- **Losing Sidecar behaviour in the cutover.** Bounded by the characterization fixtures: every verdict change is a deliberate, commented edit. The `Tracker` and resolver are outside the change.
- **Trusting upstream data.** Manifests are regex and literals, not code; the validator caps size and depth; Go's RE2 has no pathological backtracking. Vendored files are reviewed in a PR, never fetched at runtime by default.
- **Herdr changes course.** If manifests stop being published, the vendored tree keeps working and overlays become ordinary rules. Nothing in Sidecar's runtime depends on herdr.dev being up.
- **Attribution.** Apache-2.0 requires the licence and notice to travel with the vendored files; `NOTICE` under `manifests/upstream/` and a line in the repository's third-party notices cover it.

## Other projects surveyed

None publishes a machine-readable, versioned detection catalog the way Herdr does, which confirms the premise that Herdr is the right upstream. Worth knowing anyway:

- **ClawTab** (Tauri, tmux status bar): hooks-first for Claude Code, Codex, OpenCode with terminal parsing as fallback, three states, and a *dated* per-provider compatibility matrix with the explicit caveat that hook support changes faster than tmux. The dated matrix is a practice worth keeping in our capability matrix, which already records `testedProviderRange`.
- **agent-deck**: Claude via hook plus transcript reading, Codex via its `notify` hook, everything else by output parsing marked "untested". Nothing to borrow beyond confirmation that Codex `notify` is the community's session-identity path.
- **claude-code-kanban**, **Claude-Code-Agent-Monitor**: hooks-only, Claude-only, web dashboards. Their "awaiting reason" tooltips (`Needs input`, `Turn done`, `At prompt`, `Interrupted`) are a nice presentation of the same four-state model.
- **claude-squad**, **amux**, **dmux**, **repomon**, **thurbox**, **agterm**, **cmux**: worktree and session managers over tmux or a native terminal. They show attention state coarsely and do not publish rules.
- **vibe-kanban**: drives agents through their non-interactive JSON/stream modes rather than reading TUIs, so it sidesteps the problem instead of solving it. Not applicable to embedded interactive panes.
- **Prime Agent**: ships a built-in Herdr reporter that activates on `HERDR_ENV=1`. The first concrete sign that agents will report to whichever protocol is dominant. It prompted the compatibility shim considered and dropped under Phase 5.

## Decisions (2026-09-01)

Recorded from review so later phases do not reopen them.

1. **Vendored Herdr manifests are the screen-lane authority.** Sidecar's own rules become overlays. Accepted.
2. **Runtime fetch is opt-in.** Default off; Phase 5 as written.
3. **No Herdr reporter compatibility shim.** Dropped; reasoning recorded under Phase 5.
4. **Detection-only families for every Herdr screen-manifest agent except Gemini.** Cline, Devin, Droid, Kimi, Kiro, Kilo, Hermes, Qoder, Qwen, Maki. OMP stays out (hooks-only upstream). The scope note under Phase 4 records why this is cheap.
5. **Weekly sync with an automatic pull request.** Accepted.
6. **The differential harness uses Herdr's release binary**, and the sync pins vendored manifests to the matching release tag by default. The harness section records why a binary is needed at all. **Corrected 2026-09-02:** the release tag and the vendored ref are two pins with two jobs, and this decision originally conflated them. `--release-tag` names the release whose binary the differential harness downloads, so it must stay a real downloadable release and still resolves to the newest one, previews included. `--ref` selects the source tree to vendor and now defaults to Herdr's own default branch (`master`; there is no `main` in that repository, so the `--ref main` this document used to advertise never worked), because Sidecar ships faster than Herdr tags and unreleased upstream rule changes are worth taking on the assumption that they improve detection.

A first amendment on 2026-09-01 only widened the release default to include preview builds, reasoning that the manifests are byte-identical at the newest preview. That was not enough, and the first real workflow run proved it: `aliases.upstream.json` and `authority.upstream.json` are extracted from Herdr's *source tree* and move independently of the manifest files, so a pin that is behind can delete a documented agent while every vendored manifest byte stays the same. That is exactly how the first sync pull request dropped Muse from the authority table and failed its own Go suite.

The vendored pin is therefore also guarded: a sync never moves it backwards past the commit the lock records, unless an explicit `--ref` asks for it, in which case it says so in the report, in the lock's notes and on stdout. The guard covers a force-push, a rewritten branch and a hand-pinned release tag alike, and it would have caught the worse version of this independently, since the original stable-only default resolved to `v0.8.2`, sixty-one commits back, where `muse.toml` does not exist at all.
7. **The hooks lane is closed by porting Herdr's integration assets, as a later phase.** This decision took a second pass, because the first answer ("stay informational") hid a distinction worth stating plainly. Parity has two lanes and this plan's manifest work only makes one of them generic:
   - **Screen lane.** Herdr's manifests are data. Once the engine executes their grammar, every one of their 21 screen-manifest agents gets Herdr's verdicts on day one with no per-agent code beyond an alias. That is what Phases 1–4 deliver, and it already equals what Herdr does for Pi, Kimi, and Kilo when their integration is *not* installed.
   - **Hooks lane.** Where Herdr beats its own screen manifests, it does so by installing an integration: a TypeScript extension in Pi's extensions directory, a JavaScript plugin in OpenCode's config, a shell hook in Kimi's settings, and so on, 17 in all. Those are executable assets with a provider-specific installer each, and they talk to Herdr's Unix socket. Vendoring manifests does nothing for them. After Phase 4 the residual gap against Herdr is exactly Pi, Kimi, Kilo (deterministic in Herdr with the integration, screen-only here), plus OMP and MastraCode (no screen manifest upstream, so nothing to inherit).
   
   The direction chosen is **Sidecar-native ports, not a shim**: Sidecar maintains its own copies of Herdr's assets, installed into the same provider locations by Sidecar's own installers, talking to Sidecar through `sidecar agent report` exactly as the existing OpenCode plugin does. Ports are mechanical because every Herdr asset has the same two halves: a provider-specific half (which hook event maps to which state or session reference) that is the valuable part and is kept, and a transport half (gate on `HERDR_ENV`, write JSON to `HERDR_SOCKET_PATH`) that is swapped for Sidecar's. Phase 6 records the mechanics. Until it lands, `TestHerdrAuthorityGaps` keeps the residual list current. Any promotion to `full` still follows the lifecycle plan's evidence rules: traces from a released provider version, never a copied tier.

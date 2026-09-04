# Adding New Agent CLIs to Sidecar

This guide is the complete, step-by-step developer and agent reference for adding support for a new AI coding CLI (such as Meta's Muse, Grok, Claude Code, Codex, OpenCode, Pi, etc.) to Sidecar.

Sidecar provides unified workspace creation, live status tracking, terminal embedding, prompt coordination, session resume durability, and conversation history across all supported CLIs. To make a CLI a first-class citizen, several subsystems need to be wired.

> [!TIP]
> **Borrowing from Herdr**: When adding a new CLI, you can reference the source code for [Herdr](https://github.com/herdrdev/herdr). Herdr is often ahead on prototyping and testing new CLI integrations (process names, regex rules, hook schemas, and screen captures), and Sidecar frequently ports or adapts proven patterns from Herdr.

---

## Subsystem Architecture Overview

Adding a new agent CLI touches up to seven distinct subsystems. "Up to" is load-bearing: a **detection-only** family, one Sidecar recognises in a pane and never offers to start, needs Step 2 alone, and Step 2 is three moves. Read [Step 2.0](#20-decide-which-shape-you-are-adding) before working through Step 1, because it decides how much of this guide applies to you.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Catalog & Launch Registry (internal/agentcatalog)                        │
│    One TOML file: id, display name, command, auto-approve flag, resume      │
└──────────────┬──────────────────────────────────────────────┬───────────────┘
               │                                              │
               ▼                                              ▼
┌──────────────────────────────┐              ┌──────────────────────────────┐
│ 2. Live Activity & State     │              │ 3. Shell Creation & Settings │
│    (internal/agentactivity)  │              │    (workspacecreate, configui│
│    Vendored Herdr manifest,  │              │     workspace plugin)        │
│    process alias, fixture    │              │    Modals, pickers, defaults │
└──────────────┬───────────────┘              └──────────────┬───────────────┘
               │                                              │
               ▼                                              ▼
┌──────────────────────────────┐              ┌──────────────────────────────┐
│ 4. Lifecycle Hooks & Trust   │              │ 5. Conversation History      │
│    (agentlifecycle, session, │              │    (internal/adapter/<cli>)  │
│     agentintegration)        │              │    JSONL/DB transcript       │
│    session reporting, roots  │              │    parser, tiered watcher    │
└──────────────┬───────────────┘              └──────────────┬───────────────┘
               │                                              │
               ▼                                              ▼
┌──────────────────────────────┐              ┌──────────────────────────────┐
│ 6. Theme & Visual Language   │              │ 7. Verification & Matrix     │
│    (styles/overview,         │              │    (agentcontrol live tests, │
│     website/src/data/themes) │              │     vocabulary parity tests) │
│    Badge colors, palette     │              │    Prompt/wait/read control  │
└──────────────────────────────┘              └──────────────────────────────┘
```

---

## Step 1: Catalog & Launch/Resume Registry

`internal/agentcatalog` is the single shared source of truth for agent families Sidecar can launch, configure, and resume. **It is data, not code: one TOML file per family under `internal/agentcatalog/families/`, embedded in the binary.** Adding a family is writing one file. There is no Go slice to edit, and no other file in that package changes.

> [!NOTE]
> A **detection-only** family is the same file with no `command`. See [Step 2.0](#20-decide-which-shape-you-are-adding).

### 1.1 Write `internal/agentcatalog/families/<id>.toml`

The file name is the id. Every field is documented in [`internal/agentcatalog/families/README.md`](../../../internal/agentcatalog/families/README.md), which is the schema of record; the user-facing half is [`docs/reference/agent-catalog.md`](../../reference/agent-catalog.md).

```toml
# Muse Spark.
#
# Sources, read 2026-08-31: `muse --help` from the installed CLI; --yolo
# disables approval and sandbox (also --disable-approval, --trust-workspace).
# Resume shape confirmed against Herdr's agent_resume.rs.
id = "muse"
order = 100
name = "Muse Spark"
short = "Muse"
command = "muse"
skip_permissions_arg = "--yolo"
adapter_id = "muse"
resume_args = ["resume"]
resume_kinds = ["id"]
```

### 1.2 The three rules a file has to follow

**Record where every fact came from, in a comment at the top.** A command name, an auto-approve flag and a resume shape are claims about somebody else's software. The next person needs to know whether they came from the provider's `--help`, its documentation, or Herdr's `agent_resume.rs`, which is the reference for resume and the only launch-adjacent knowledge upstream holds.

**Never guess a flag.** A provider with no auto-approve mode gets no `skip_permissions_arg`, and the file says so and says why. Three bundled families have none, for three different reasons: Cline auto-approves by default and its flag takes a value, Devin's bypass is two argv entries the single-entry field cannot hold, and Mastra Code's is an in-app toggle. A guessed flag is a command line nothing has run.

**The session value is always the last argv entry**, and `resume_args` are what comes before it. A provider whose resume only works joined (`--resume=<value>`) cannot be expressed and gets no resume rather than an invented one; Copilot is the standing example.

### 1.3 What inherits this automatically

Writing the file wires all of:
- Creation pickers in `internal/workspacecreate/form.go` (Worktree & Shell modals) and the global Sessions create.
- Configuration settings in `internal/configui/page_agents.go` (allowlist toggles and launch command overrides).
- Workspace default agent dropdown in `internal/configui/page_workspaces.go`.
- CLI commands: `sidecar create shell --agent <kind>`, `sidecar create worktree --agent <kind>`, `sidecar agent start --kind <kind>`.
- Structured resume command generation via `agentcatalog.BuildResume` and `agentcatalog.DisplayCommand`.

### 1.4 Installation filtering, and what it means for a new family

The creation pickers offer a family when its command resolves on `PATH`, or when the user has already named it in `plugins.workspace.agents`. Configuration → Agents lists every launchable family regardless, annotated `not installed`.

So a family you add is invisible in the picker on a machine without the CLI, which is correct and is not a bug to work around. `agentcatalog.PrimeInstalled` does the `PATH` walk once per process from `cmd/sidecar/main.go`; nothing on a render path may call `exec.LookPath`.

### 1.5 The user overlay

Users extend the same catalog without a rebuild by dropping files into `<config dir>/agents/`, loaded by `agentcatalog.LoadOverlay` from `cmd/sidecar/main.go` and `internal/cli/cli.go`. A file naming a bundled family overrides only the fields it states; a new id adds a family. This is worth knowing when you are deciding whether a provider quirk belongs in the bundled catalog at all: if the answer is "only this user wants it", it is already supported.

---

## Step 2: Detection - sync or vendor the manifest, add the alias, mint a fixture

`internal/agentactivity` classifies live agent state (`working`, `blocked`, `idle`, `done`, `unknown`) from pane titles, tmux screen captures, and process information. There are no Go rule tables left. Every agent Sidecar detects executes Herdr's vendored detection manifest through `internal/agentactivity/manifest`, and anything Sidecar knows that upstream does not is a data overlay under `internal/agentactivity/manifests/sidecar/`. See `docs/plans/active/herdr-detection-parity.md` and `docs/reference/herdr-detection-parity.md`.

That makes this step three moves rather than a subsystem: **sync or vendor the manifest, add the alias, mint a fixture from `explain --file`.** It is also the only step a *detection-only* family needs at all.

### 2.0 Decide which shape you are adding

There are two kinds of agent family, and picking the wrong one is the difference between one commit and seven. Both are one TOML file; what differs is whether the file states a `command`.

**Detection-only** is an agent Sidecar recognises in a pane and never offers to start. Its file carries an id, a display name, a short label, and Herdr's process aliases, and no `command`, so no resume, no adapter id and no auto-approve flag either. It skips Steps 1, 3, 4, 5 and 6 entirely, and it costs no theme work, because `styles.AgentColor` answers `TextMuted` for a provider no theme registers and `styles.AgentLabel` falls back to the short label. This is the right shape when Herdr publishes a manifest for the agent and nobody has established what starts it.

Sidecar ships no detection-only family today. Ten were registered in Phase 4 of the parity plan and all ten gained a command in Slice 5, once somebody read each provider's documentation. The bucket stays because the next agent Herdr adds is detection-only from the moment its manifest is vendored until that reading is done, and a picker must never offer a program nobody can start.

**Full** is an agent Sidecar launches, resumes, reads transcripts for, and colours. Its file states a `command` and it works through all seven subsystems below. This is the right shape when a user will pick the agent from a creation modal.

Promoting a detection-only family to a full one is: add `command`, `skip_permissions_arg` and `resume_args` to the file it already has, then work the remaining steps. Nothing has to be undone, and nothing moves between lists: the bucket is derived from the presence of a command, not from a flag or a second file.

The id of a detection-only family is **Herdr's own agent label**, not a prettier product name, so the manifest file name, the key into `aliases.upstream.json`, and `sidecar agent explain --agent` all agree with no mapping. That is why Qoder is registered as `qodercli` even though the program it launches is `qoder`; where the two differ, `short` follows the command, because a chip reading `qodercli` names a manifest file rather than a program. A full family may use a Sidecar spelling, at the cost of an entry in `ManifestAgentID` and `HerdrAgentLabel` in `manifest_detect.go` (today: `copilot` → `github-copilot`, `antigravity` → `agy`).

Two catalog families have no vendored manifest at all: `omp` and `mastracode`, which Herdr ships hooks-only. They are launchable and `identifyProcessName` names them, so a hook report can be checked against the pane, but `Supports` is false for them and no row carries a chip that could only ever read unknown. They are declared in `familiesWithNoScreenManifest` in `internal/agentactivity/detection_only_test.go`.

### 2.1 Sync or vendor the manifest

1. **Check whether it is already vendored.** `ls internal/agentactivity/manifests/upstream/` lists 21 agents, and Herdr's catalog is likely to have the one you are adding. If it does, the rule half of detection is already done and you write no regexes.

2. **If it is not vendored, sync rather than write.** `scripts/sync-herdr.sh` refreshes the whole vendored tree from a Herdr ref, updates `upstream.lock.json`, and renders `report.md`. Never hand-edit a file under `upstream/`: `TestVendoredManifestsMatchLock` fails on any edit, which is what keeps a re-sync a clean file replacement.

3. **If upstream has no manifest for the agent at all**, this guide cannot help you skip the work: write the rules as a Sidecar overlay in the same grammar (`internal/agentactivity/manifests/sidecar/<agent>.toml`), prefix every rule id `sidecar.`, and open a pull request against Herdr carrying the same rules and a sanitized fixture so the overlay can be deleted on a later sync.

### 2.2 Add the alias

`identifyProcessName` in `internal/agentactivity/activity.go` is the one place a process name becomes a family id, and its vocabulary is Herdr's `lookup_agent` table as extracted into `internal/agentactivity/manifests/aliases.upstream.json`. Add one case carrying every spelling the table has for the agent:

```go
case oneOf(name, "qwen", "qwen code", "qwen-code"):
    return "qwen"
```

`normalizeProcessName` has already lower-cased, trimmed, taken the path basename, and stripped one launcher suffix (`.exe .cmd .bat .ps1 .js`), so the case carries bare names only. Two alias shapes in the table are not plain strings and are handled outside the switch: `versioned_binary_prefixes` (Herdr's `muse-bin-<digit>` rule, which today only Muse needs) and `normalized_suffixes`, which is the stripping above.

Then add the id to `Supports` in the same file: the `aliasGatedFamily` switch for a family with no hand-written process gate, or the hand-gated list for one that has written a `<provider>Process` predicate. `Supports` is what `Detect` gates on, so an agent missing from it answers `unsupported-agent` for every pane and shows no badge. `TestSupportedProviderSetIsFrozen` in `compat_test.go` is deliberately a frozen set: widening it is a decision, and the reason belongs in that test's comment.

An **alias-gated** family needs no process gate of its own. `processGate` in `manifest_detect.go` falls through to `identifyProcessName(command) == agent || ob.ProcessIdentity == agent`, which is the same refusal every provider makes, evaluate `qwen.toml` only against a pane running Qwen, without a file per agent to say so. It reads both identity inputs because `Identify` resolves `ProcessIdentity` first: a pane reporting `node` whose foreground argv[0] basename is `qwen` is claimed as Qwen, and a gate reading only the command name would then refuse every observation on it, leaving the row with a provider chip stuck at unknown. It is still strict: where neither input names the agent, such as a `#!/usr/bin/env node` shim whose argv[0] is the interpreter, the pane is refused rather than evaluated. That costs a missing badge and buys the guarantee that one agent's manifest never reads another's screen.

A family that needs a wider gate writes `<provider>Process(command string) bool` in `internal/agentactivity/<provider>.go` and registers it in `processGate`. Do that when the agent runs under a shared runtime and the gate has to admit the wrapper.

### 2.3 Mint a fixture from `explain --file`

```bash
tmux -L probe -f /dev/null new-session -d -s cap -c "$PWD" -x 120 -y 40
tmux -L probe send-keys -t cap 'qwen' Enter && sleep 12
tmux -L probe capture-pane -p -e -N -t cap > /tmp/qwen.txt
tmux -L probe kill-server
sidecar agent explain --file /tmp/qwen.txt --agent qwen
sidecar agent explain --file /tmp/qwen.txt --agent qwen --print-window   # what detection saw
sidecar agent explain --file /tmp/qwen.txt --agent qwen --json           # the record the harness diffs
```

`sidecar agent explain --file PATH --agent KIND [--title T] [--rows N] [--print-window] [--json]` runs the screen lane alone over a saved capture: no tmux, no lifecycle store, no running agent. `--agent` takes any id Sidecar vendors a manifest for, not only the ones it registers as families, so a manifest can be exercised before anything is registered at all. `--title` and `--rows` stand in for a header the capture does not carry; `--rows` is the detection read window and defaults to the fixture header, else 24.

Use a private tmux socket (`-L probe`) throughout. `tmux -L probe kill-server` is the teardown: killing the session alone leaves the probe server running, and every command above names the socket, so it never reaches the default server. Never run `tmux kill-server` without `-L probe`; the default server holds live sessions.

Save the capture under `internal/agentactivity/testdata/<agent>/` in the header format the other fixtures use (`pane_title:`, `pane_current_command:`, `pane_height:`, an optional `state:` the census checks, then a line reading exactly `screen:`). `internal/agentactivity/testdata/README.md` is the contract: a fixture is a real capture reduced to evidence rows with no conversation text, or it is synthetic and its `source:` header says so. `TestFixtureCensus` classifies every fixture and fails when one declares a state it does not reach.

A fresh fixture per new agent is not required to register it, because the manifest is already proved upstream, but it is what turns "the badge looked wrong once" into a test, and it is what gives the agent a row in the census and in `scripts/herdr-diff.sh`.

### 2.4 Only then consider an overlay

An overlay rule is a claim that upstream is wrong or silent about a screen you have captured, and every one of them costs something on the next sync. Write it in `internal/agentactivity/manifests/sidecar/<herdr-agent-id>.toml`, prefix new rule ids `sidecar.`, state which upstream priorities it sits between and why, and prove it with the fixture. `scripts/herdr-diff.sh` reports a rule that reaches the verdict Herdr reaches without it as **redundant** and fails, which is how an overlay rule that has stopped earning its place gets deleted.

### 2.5 Critical activity detection rules

- **Do not reintroduce a Go rule table.** If a screen is misread, the fix is an overlay rule with a fixture, or a pull request to Herdr.
- **Overlays that retain state need corroborating chrome.** A rule keyed on a bare word ("transcript", "resume session") freezes the badge for `SkipRetentionCap` whenever a turn merely discusses one. Two such rules were deleted in the Phase 2 cutover for exactly this reason.
- **Do not add a curated colour, an icon, a resume path, or a conversation adapter for a detection-only family.** Each of those has its own cost in twenty curated themes and the website palette, and none of them is needed for a state badge.
- **Verify with tests**: `go test ./internal/agentactivity/... ./internal/agentcatalog/...`, and specifically `TestTheProcessNameVocabularyMatchesTheAgentCatalog` (both halves of the catalog resolve), `TestUpstreamAliasesResolveForClaimedFamilies` (every upstream alias for every registered family resolves), and `TestEveryVendoredManifestIsRegisteredOrDeclaredUnregistered` (a synced-in agent is either registered or has a recorded reason it is not).

---

## Step 3: Workspace Plugin Compatibility

While new code queries `internal/agentcatalog`, the workspace plugin maintains several helper tables in `internal/plugins/workspace/types.go`:

1. **Add `AgentType` constant** in `internal/plugins/workspace/types.go`:
   ```go
   AgentMuse AgentType = "muse" // Muse Spark (Muse Code)
   ```
2. **Add to `buildSkipPermissionsFlags()`** in `internal/plugins/workspace/types.go`:
   ```go
   agents := []AgentType{
       AgentClaude, AgentCodex, AgentCopilot, AgentAider, AgentAntigravity,
       AgentCursor, AgentOpenCode, AgentPi, AgentAmp, AgentGrok, AgentMuse,
   }
   ```
3. **Optional flags**:
   - `SystemPromptAppendFlags`: If the CLI accepts a flag to append system prompt instructions (e.g. `AgentMuse: "--rules"`).
   - `PrintModeArgs`: If the CLI has non-interactive stdout execution mode (e.g. `AgentMuse: {"-p"}`).
4. **Fallback session status** in `internal/plugins/workspace/agent_session.go`: Add a case to `detectAgentSessionStatus` if file-based inspection is supported.

---

## Step 4: Agent Lifecycle Hooks & Session Binding

When an agent CLI supports telemetry/lifecycle hooks, Sidecar can track exact sessions, state transitions, and process exits.

### 4.1 Record Capability in `internal/agentlifecycle/capabilities.json`

Add an entry to `internal/agentlifecycle/capabilities.json`. Muse Spark 1.0.1 has **no published lifecycle hook** (extension surface is skills/MCP/MSP wire, not `hooks.json`); set `screen-fallback` until hooks are shipped:

```json
{
  "schemaVersion": 1,
  "provider": "muse",
  "source": "",
  "assetVersion": "",
  "tier": "screen-fallback",
  "evidence": "none",
  "minProviderVersion": "",
  "testedProviderRange": "",
  "covered": [],
  "knownGaps": [
    "No Sidecar integration is built, so nothing is claimed and screen detection remains the sole authority.",
    "Muse Spark 1.0.1 traced locally (darwin/arm64, echo provider) but hooks are not shipped: Muse Code's extension surface is skills, MCP servers, and the MSP wire schema, not lifecycle hooks. No published hook contract like Claude Code's hooks or Codex's hooks.json was found in the binary or docs at https://dev.meta.ai/docs/muse-code/.",
    "The session log (JSONL at ~/.local/share/muse/sessions/YYYY/MM/DD/<uuid>/session.jsonl) and SQLite index at ~/.local/share/muse/session-index.db are the durable stores; reasoning text is encrypted (encrypted_content) and tool call/result shapes are visible but not hook-authored.",
    "Screen detection is therefore the sole lane authority; the capability entry exists so the provider appears in the matrix and can be promoted when and if Muse ships lifecycle hooks."
  ],
  "orderingGuaranteed": false,
  "sourceDoc": "https://dev.meta.ai/docs/muse-code/",
  "sourceVersionNote": "muse 1.0.1 on darwin/arm64, inspected 2026-08-31. No hook contract found; session storage verified via local JSONL and SQLite traces, and live TUI captured via private tmux socket (braille title spinner U+2800-FF, ◈ Thinking + esc to interrupt chrome, ⟩ prompt idle)."
}
```

When Muse ships hooks, promote to `session-identity` / `advisory` / `full` with real traces as for `claude`/`codex`/`pi`.

### 4.2 Approved Store Roots in `internal/agentsession/trust.go`

No official source is registered for Muse Spark yet (screen-fallback). `OfficialSources()` / `OfficialSourceFor` stay empty; add them only when a `sidecar.muse.hooks` source is shipped.

Define approved storage roots in `Roots.For(kind)` in `internal/agentsession/trust.go` (supports `XDG_DATA_HOME` and `MUSE_HOME` overrides — Muse stores sessions under `~/.local/share/muse/sessions` by default, not `~/.muse`):
   ```go
   case "muse":
       base := r.env("XDG_DATA_HOME")
       if base == "" {
           if r.Home == "" {
               return nil
           }
           base = filepath.Join(r.Home, ".local", "share")
       }
       return []string{filepath.Join(base, "muse", "sessions")}
       // Also check MUSE_HOME when set: if r.env("MUSE_HOME") != "" { base = r.env("MUSE_HOME") }
   ```
The actual implementation checks `MUSE_HOME` first, then `XDG_DATA_HOME`, then `~/.local/share/muse/sessions` — keep `WithinRoots` symlink-aware as for `codex`/`opencode`.

### 4.3 Automated Integration Installer (Optional)

If Sidecar bundles an automatic hook or plugin installer for this CLI:
1. Implement `agentintegration.Adapter` in `internal/agentintegration/muse_install.go`.
2. Register the adapter in `agentintegration.DefaultAdapters()` in `internal/agentintegration/install.go`.
3. This exposes `sidecar agent integration install muse`, `status`, `update`, and `uninstall`, as well as the UI in **Configuration → Agents → Integrations**.

---

## Step 5: Conversation History Adapter

To allow Sidecar's **Conversations** plugin to display transcripts, search history, and view token analytics for this CLI:

### 5.1 Implement `adapter.Adapter` in `internal/adapter/<provider>/`

Create the adapter package in `internal/adapter/muse/`:
- `adapter.go`: Implements `adapter.Adapter`:
  - `ID() string`: `"muse"`
  - `Name() string`: `"Meta Muse"`
  - `Icon() string`: `"M"` or custom glyph
  - `Detect(projectRoot string) (bool, error)`: Checks if sessions exist for project
  - `Capabilities() CapabilitySet`
  - `Sessions(projectRoot string) ([]Session, error)`: Returns parsed session metadata
  - `Messages(sessionID string) ([]Message, error)`: Returns structured messages
  - `Usage(sessionID string) (*UsageStats, error)`: Token usage statistics
  - `Watch(projectRoot string) (<-chan Event, io.Closer, error)`: File watcher
- `types.go`: Native data structures for parsing session logs.
- `watcher.go`: Watcher setup (use `internal/adapter/tieredwatcher` for append-only JSONL files).

### 5.2 Register Adapter Factory

1. Create `internal/adapter/muse/register.go` to self-register via `adapter.RegisterFactory`:
   ```go
   package muse

   import "github.com/marcus/sidecar/internal/adapter"

   func init() {
       adapter.RegisterFactory(func() adapter.Adapter {
           return New()
       })
   }
   ```
2. Add blank import in `cmd/sidecar/main.go`:
   ```go
   _ "github.com/marcus/sidecar/internal/adapter/muse"
   ```

### 5.3 Conversations UI Integration

In `internal/plugins/conversations/view_content.go`:
- `renderAdapterIcon()`: Add color styling for the adapter badge.
- `adapterAbbrev()`: Add 2-letter abbreviation (e.g. `"MU"`).
- `adapterShortName()`: Return short name string.
- `adapterFilterOptions()`: Add dedicated filter shortcut key if desired.

---

## Step 6: Theme Colors & Visual Presentation

1. **Default Color & Icon Glyph**: In `internal/styles/overview.go`:
   - Add default hex color to `defaultAgentColors`:
     ```go
     "muse": "#A78BFA",
     ```
   - Add icon glyph to `defaultAgentIcons` (must match `Adapter.Icon()`):
     ```go
     "muse": "◈", // not ✦ (Grok) — verified via Muse adapter
     ```
2. **Per-theme palettes** (missing seam — guide previously omitted these):
   - `internal/styles/themes.go`: add `"muse": "#A78BFA"` to `DefaultTheme.Colors.AgentColors` (Sidecar Modern).
   - `internal/styles/curated_themes.go`: add `"muse"` to **every** theme's `AgentColors` map. Values must already pass `EnsureContrastOn(..., surface, 4.5)` or `TestCatppuccinMochaPassesNormalizationUnchanged` fails. For example catppuccin-mocha `#A78BFA` → `#ae94fa` on `#393947`; use the normalized value per theme (run `TestDebugMuse` helper to derive).
3. **Website & Theme Palette**: In `website/src/data/themes.json`, add `"muse": "#A78BFA"` (or the per-theme normalized value) under `AgentColors` for all 21 themes.

Why this matters: `NormalizePalette` rewrites any `AgentColors` entry that fails contrast against `SurfaceRaised`. A single `#A78BFA` for every curated theme would be rewritten for most dark themes (catppuccin, tokyonight, etc.) and break the “passes unchanged” test. Store the post-normalization value.

---

## Step 7: Verification & Testing Checklist

Always run the full verification suite when adding a new agent CLI:

### 1. Unit & Parity Tests
```bash
# Verify catalog and process name resolution
go test ./internal/agentcatalog/...
go test ./internal/agentactivity/...

# Verify icon consistency with conversations adapters
go test ./internal/styles/ -run TestAgentIconMatchesConversationsAdapters

# Verify workspace pickers match catalog
go test ./internal/plugins/workspace/ -run TestAgentPickersFollowCatalog

# Verify conversation adapter
go test ./internal/adapter/muse/... -v
```

### 2. Live Agent Matrix Test
Test the full programmatic coordination flow (start -> identify -> prompt -> wait -> read -> send-keys):
```bash
SIDECAR_LIVE_AGENT_MATRIX=muse go test -v ./internal/agentcontrol -run TestLiveProviderMatrix
```

### 3. Headless TUI Verification
Use `scripts/tmux-drive.sh` in an isolated environment:
```bash
./scripts/tmux-drive.sh paths
SIDECAR_BIN=$HOME/go/bin/sidecar ./scripts/tmux-drive.sh start 200 50
# Open workspace creation modal and verify the new agent appears in the list
./scripts/tmux-drive.sh keys n
./scripts/tmux-drive.sh snap modal-create-workspace
./scripts/tmux-drive.sh stop
```

---

## Summary Checklist for Adding a New CLI

### Detection-only family

- [ ] **Step 2.0**: Write `internal/agentcatalog/families/<id>.toml` with an id (Herdr's agent label), an order, a display name, a short label, Herdr's aliases, and **no `command`**. The display name and the short label are read: `agentcatalog.Label` is what a prose surface shows, and `styles.AgentLabel` lowercases the short label into the agent chip, which is why Qoder's chip reads `qoder` while its id stays `qodercli`.
- [ ] **Step 2.1**: Confirm the manifest is vendored under `internal/agentactivity/manifests/upstream/`, or run `scripts/sync-herdr.sh`.
- [ ] **Step 2.2**: Add the alias case to `identifyProcessName()` and the id to the `aliasGatedFamily` switch in `internal/agentactivity/activity.go`.
- [ ] **Step 2.3**: Optionally mint a fixture with `sidecar agent explain --file`.
- [ ] **Tests**: `go test ./internal/agentactivity/... ./internal/agentcatalog/... ./internal/styles/...`.

Steps 1, 3, 4, 5 and 6 do not apply, and no theme, icon, adapter, picker or resume work is owed.

### Full family

- [ ] **Step 1 (`internal/agentcatalog`)**: Write `families/<id>.toml` with the command, launch/resume args and skip-permissions flag (`--yolo` for Muse), and a comment recording the source of each fact.
- [ ] **Step 2 (`internal/agentactivity`)**: Vendor or sync the detection manifest, add the process alias to `identifyProcessName()` and the id to `Supports()`, write `<provider>Process` and register it in `processGate` if the agent runs under a shared runtime, and mint a fixture with `sidecar agent explain --file`.
- [ ] **Step 3 (`internal/plugins/workspace`)**: Add `AgentType` constant, append to `buildSkipPermissionsFlags()`, and set optional system prompt / print mode flags; add `AgentMuse` case to `detectAgentSessionStatus` if file-based status is supported.
- [ ] **Step 4 (`internal/agentlifecycle` & `agentsession`)**: Add capability to `capabilities.json` (use `screen-fallback`/`none` if no hooks as for Muse), register official source only when a hook is shipped, and configure approved store roots (`XDG_DATA_HOME/muse/sessions` for Muse).
- [ ] **Step 5 (`internal/adapter`)**: Implement conversation adapter (`adapter.go`, `types.go`, `watcher.go`, `register.go`), register constructor, and add blank import in `cmd/sidecar/main.go`.
- [ ] **Step 6 (`internal/styles` & themes)**: Add default hex color and icon (`◈` for Muse) to `defaultAgentColors`/`defaultAgentIcons` in `internal/styles/overview.go`, add to `internal/styles/themes.go` and **every** entry in `internal/styles/curated_themes.go` (pre-normalized), and update `website/src/data/themes.json` for all themes.
- [ ] **Step 7 (Tests)**: Pass vocabulary parity tests (`TestTheProcessNameVocabularyMatchesTheAgentCatalog`), icon tests (`TestAgentIconMatchesConversationsAdapters`), workspace picker tests (`TestAgentPickersFollowCatalog`), and live matrix tests (`SIDECAR_LIVE_AGENT_MATRIX=muse`).

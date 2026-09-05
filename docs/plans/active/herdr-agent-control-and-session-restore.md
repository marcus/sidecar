# Herdr gap closure: agent control and cold session restoration

**Status:** active; M0–M4 implemented on 2026-08-30. M5 partially implemented on 2026-08-31: the remote adapter, host-scoped target identity, host-local session references and the local/remote parity suite ship, proved over a loopback harness rather than a second machine. **M5's rollout half is deliberately incomplete and this plan stays active because of it** — the live provider matrix passed for codex, claude, opencode and cursor and failed for grok (`td-bf451a`) and pi (`td-c05b06`), so `agent_control` remains default-off rather than being flipped against a gate that did not pass. The plan's own move-to-implemented condition (local agent control, exact session binding and cold restore all shipping) is met, so this is a recommendation to move it once those two provider bugs are fixed and the matrix is re-run, not a claim that work remains in the design. **Research baseline:** Sidecar `main` at `13ddaaa6` (2026-08-29); Herdr v0.8.2 at commit `9eb52145`. Herdr is the feature benchmark this plan measures against — the bar is parity, and where Sidecar's runtime allows it, better. It is not a runtime Sidecar depends on: the Herdr remote-hosts plan is deprecated, and Sidecar is its own remote host runtime.

One sentence: **an agent working inside Sidecar should be able to start and coordinate another managed agent through provider-aware commands, and a machine restart should reconstruct Sidecar's durable workspace shape and optionally resume the exact agent conversations that were running, without replacing tmux or pretending tmux can live-handoff its PTYs.**

Related plans:

- [Pane repositioning](../implemented/pane-repositioning.md) (implemented) owns interactive and agent-driven pane movement. This plan composes with its `layout get` / `layout apply` / `layout move` surface and adds no second layout grammar.
- [Sidecar as its own remote host runtime](sidecar-remote-hosts.md) owns host registration, SSH transport, remote inventory, remote terminal control, and — as of its completed Phase C — remote mutation, shipped as one-shot `ssh <target> sidecar <verb> --json` invocations over the existing ControlMaster. Because this plan's commands are headless, target-taking CLI verbs, that seam carries them remotely without new protocol work; see [Interaction with remote hosts](#interaction-with-remote-hosts).
- [Hosting Herdr plugins in Sidecar](../deprecated/herdr-plugin-support.md) is deprecated, superseded by [the plugin ecosystem plan](../implemented/plugin-ecosystem/README.md), and orthogonal. A plugin may eventually call the same agent-control core, but plugin hosting is not part of this plan.
- [Deterministic agent lifecycle hooks](notification-agent-lifecycle-hooks.md) owns lifecycle reporting, authority arbitration, provider integration installation/status, and screen fallback. This plan owns the session-identity and resume semantics that use the same reporting envelope without claiming lifecycle authority.
- [Native Agent Orchestration in Sidecar](../deprecated/agent-orchestration-integration.md) remains deprecated. This plan deliberately exposes small coordination primitives and does not revive a Sidecar-owned plan/build/review engine, task policy, validator topology, or merge loop.

**Follow-on:** the restore work that remains after M3 and M4, including typed resume commands under the `ask` policy, candidates for shells whose conversation was never reported, worktree sessions, and a stuck-server diagnosis, is planned in [Session restore: resume the conversation, not only the shell](session-restore-resume-conversations.md).

## Decision first

Close the useful Herdr gaps in three bounded pieces:

1. **Provider-aware agent control.** Add a headless `sidecar agent` command group over Sidecar-managed shells: list/get, start, prompt, wait, read, and logical-key input. These commands resolve a durable managed-shell target, verify the live pane occupant, reuse Sidecar's existing activity semantics, and return structured outcomes. They do not require a running Sidecar TUI.
2. **Exact session identity and cold restore.** Extend `shells.json` with structured agent launch/resume metadata reported by official per-provider integrations. On the next Sidecar start after a confirmed tmux server replacement, recreate eligible managed shells and their persisted pane layouts. Resume an agent conversation only from a validated, exact session reference and only under the configured restore policy.
3. **One transport-neutral core.** Put target resolution, refusal rules, lifecycle waits, resume planning, and restore idempotency in library packages. The local adapter speaks tmux; the remote-host plan can add a host adapter without recreating the rules.

Do **not** replace tmux, add a Sidecar daemon, or build a native PTY runtime. Sidecar application updates already leave tmux and its children running. A tmux server replacement cannot preserve arbitrary live processes because tmux exposes no supported transfer of its PTY master file descriptors. Cold reconstruction and native agent conversation resumption are the honest fallback; an SCM_RIGHTS-style live handoff is not implementable from Sidecar without owning the PTYs.

## Research basis

The Herdr comparison is against the checked-out v0.8.2 tag, not a feature list or the current `main` branch:

- [`skills/herdr/SKILL.md`](https://github.com/herdrdev/herdr/blob/v0.8.2/skills/herdr/SKILL.md) defines the agent-facing workflow: discover IDs, split layout separately, start a named agent in an available pane, prompt/wait/read it through lifecycle state, and use raw pane commands only for raw terminal work.
- [`docs/preview/website/src/content/docs/agent-automation.mdx`](https://github.com/herdrdev/herdr/blob/v0.8.2/docs/preview/website/src/content/docs/agent-automation.mdx) is the detailed CLI contract, including target pinning, ready detection, blocked refusals, prompt-stall detection, passive reads, and ordinary-command primitives.
- [`docs/preview/website/src/content/docs/session-state.mdx`](https://github.com/herdrdev/herdr/blob/v0.8.2/docs/preview/website/src/content/docs/session-state.mdx) distinguishes detach, cold server restore, optional screen-history replay, native agent resume, and experimental live handoff.
- [`src/persist/snapshot.rs`](https://github.com/herdrdev/herdr/blob/v0.8.2/src/persist/snapshot.rs) persists workspace/tab/pane topology, cwd, agent name and kind, exact official agent session reference, and structured launch argv.
- [`src/agent_resume.rs`](https://github.com/herdrdev/herdr/blob/v0.8.2/src/agent_resume.rs) accepts session references only from allowlisted official integration sources, validates ID/path shape, builds argv as an argument vector, and deduplicates a native session across panes.
- [`src/server/handoff.rs`](https://github.com/herdrdev/herdr/blob/v0.8.2/src/server/handoff.rs) shows why handoff belongs to the PTY owner: the old process passes PTY file descriptors over a Unix socket, with an explicit 64-FD cap and reconnect semantics for interrupted requests.

The Sidecar baseline is the checked-out source, not whichever development binary wins `PATH`. The research snapshot predates the remote-hosts merges; `main` now registers the `host` command group and the Phase A–C remote-host capability behind `features.SidecarRemoteHosts`, along with the headless `shell rename --target`, `shell send --target`, and `create worktree --plan` verbs Phase C added. Where those change a matrix row below, the row says so.

## The journeys this plan must make real

### 1. Start a helper agent without taking the user's focus

An agent in the current Sidecar shell creates a sibling terminal through the existing layout surface, starts Codex or Claude in that managed shell, and receives a stable target only after Sidecar has verified that the expected provider owns the pane and is ready for input:

```bash
created=$(sidecar create shell --split right --name reviewer --json)
target=$(printf '%s\n' "$created" | jq -r '.shell.session')
sidecar agent start "$target" --kind codex --timeout 30s
```

`agent start` never creates or moves layout. That separation is one of Herdr's strongest interaction decisions and fits Sidecar's existing ownership boundaries: `create shell` owns managed-shell creation and pane placement; `agent start` owns provider identity and readiness.

### 2. Coordinate work through lifecycle state

The caller prompts the helper, waits until it settles, and reads the result without scraping an arbitrary focused pane:

```bash
sidecar agent prompt "$target" "Review the current diff and report only actionable findings." --wait --timeout 2m
sidecar agent read "$target" --source recent-unwrapped --lines 120
```

If the agent is blocked before the prompt, Sidecar refuses without writing bytes. If the wait returns blocked, the caller inspects the current screen and uses validated logical keys only after deciding what the UI needs. The target remains pinned to the same tmux session, pane incarnation, and provider; a replacement occupant cannot satisfy the old wait.

### 3. Recover from a machine or tmux server restart

Before the restart, Sidecar has already persisted shell identity, cwd, pane trees, provider kind, and an exact provider-reported native session reference. On the next Sidecar launch:

1. The tmux server incarnation check proves this is a cold replacement rather than an ordinary Sidecar restart.
2. Sidecar paints its first frame before restore work begins.
3. Eligible managed shells are recreated under their same tmux session names and cwd, making existing `PaneLayoutJSON.Session` selectors valid again.
4. Project and global Sessions pane trees decode through `panecodec` and reattach to those sessions.
5. Shell-only restoration happens automatically; known agent sessions are either left ready for one-click/CLI resume, or resumed automatically when the user has explicitly selected the `auto` policy.

No arbitrary `--run` command, dev server, test watcher, or unstructured custom agent start string is replayed after a cold restart. A missing worktree is a refusal, not a fallback to `$HOME` or the main checkout: resuming into the wrong repository is worse than leaving a clearly recoverable row.

### 4. Upgrade Sidecar without dropping work

Replacing the Sidecar executable does not touch the default tmux server and does not stop agents, shells, compiles, or dev servers. The currently running Sidecar process may continue until the user relaunches it; a new Sidecar instance reattaches to the same tmux sessions. If tmux itself must be replaced, Sidecar reports that a cold restart is required and offers the restore preview. It never restarts, kills, or replaces the default tmux server automatically.

## Capability gap matrix

`Covered` means the checked-out Sidecar `main` already has the user/agent outcome. `Partial` means useful machinery exists but the Herdr journey is not possible end to end. `Gap` is work owned by this plan. `Other plan` and `Non-goal` keep the scope honest.

| Capability | Herdr v0.8.2 | Sidecar `main` | Decision / owner |
| --- | --- | --- | --- |
| Discover agent-facing commands and structured output | `herdr --help`; most control commands return JSON | `sidecar agents`, command help/JSON metadata, `--json` on agent-facing commands | Covered |
| Caller context | Injected workspace/tab/pane IDs; `--current` | `SIDECAR_SHELL`, shell name, project and shell selectors | Covered for Sidecar's simpler model; do not copy Herdr's hierarchy |
| Create an ordinary managed terminal | Workspace/tab/pane create and split | `sidecar create shell`, including `--split`, `--tab`, `--run`, `--type` | Covered |
| Create a worktree and agent shell | `herdr worktree` plus pane/agent commands | `sidecar create worktree --agent`, using Sidecar's setup/journal pipeline | Covered, but readiness becomes this plan's shared start core |
| Read and replace pane topology | Workspace/tab/pane commands | `sidecar layout get` / `layout apply` | Covered |
| Move an existing pane | `pane move` | `sidecar layout move` shipped | Other plan: [pane repositioning](../implemented/pane-repositioning.md), implemented |
| Start a known agent in an existing idle terminal | `agent start`, provider allowlist, readiness wait | `shell send --target --run` starts one headlessly (and remotely) but proves no provider identity or readiness | Gap narrowed to the readiness contract; `agent start` supersedes the raw send for agents |
| Address an agent independently of UI focus | Unique live name or pane ID | Phase C's `shell rename --target` / `shell send --target` resolve durable managed targets headlessly (`internal/cli/shell_target.go`) | Partial; agent commands reuse that resolver rather than inventing a second alias namespace |
| Query one agent's provider, lifecycle, freshness, and evidence | `agent get` / `list` | `agentactivity`, `agentstatus`, and `workspaceinventory` compute this for TUI rows | Gap only at the application/CLI boundary |
| Guarded prompt submission | `agent prompt`; blocked refusal; bracketed-paste-aware input | Interactive TUI input exists; no provider-aware headless prompt | Gap |
| Wait for lifecycle state | Event-driven `agent wait`; target occupant pinned | TUI polling sees working/blocked/done/idle; no targeted wait API | Gap |
| Read live terminal output by semantic agent target | `agent read`, visible/recent/recent-unwrapped/detection | tmux captures and screen model exist; no agent-facing read command | Gap |
| Send validated logical keys to an agent UI | `agent send-keys` | TUI maps keys through terminal input; no headless semantic target | Gap |
| Run/read/wait on an arbitrary non-agent terminal | First-class pane run/read/wait-output | Agent can already use `tmux` after `sidecar shell list --json` | Deliberate non-goal: tmux owns raw terminal control; Sidecar documents the recipe instead of wrapping it |
| Global attention view | Sidebar rollups and done/unseen state | Global Agents lanes, blocked attention, done TTL, notifications | Covered with different semantics |
| Native conversation transcripts | Screen reads; alternate-screen paging for supported agents | Local adapter stack can return structured session messages | Sidecar advantage; expose only when an exact live-session binding exists |
| Persist project/global pane trees across Sidecar restart | `session.json` | Per-surface `PaneLayoutJSON`, `panecodec`, `state.json`, live-leaf session selectors | Covered |
| Preserve managed shell definitions across tmux death | Snapshot restores pane cwd/shape | `shells.json` v3; M4 made a server death mark records restorable instead of tombstoning them | Covered; the wipe-on-death path is the thing M4 fixed first |
| Reconstruct layout after a cold tmux restart | Automatic fresh shells in saved cwd | M4 recreates confirmed-live managed shells under their own names/cwd after the first frame; existing pane persistence reattaches by session name | Covered by M4 |
| Bind a live pane to an exact native agent session | Official integrations report ID/path into pane snapshot | Conversation adapters discover sessions; no authoritative live-pane binding | Gap |
| Resume exact agent conversations after cold restart | Default on for supported official integrations | M4 resumes exact reported references through `agentcontrol.StartResume` under policy; `ask` is the default and a reboot alone never resumes | Covered by M4, with a deliberately more conservative default than Herdr's |
| Persist terminal screen history across cold restart | Optional `session-history.json`, off by default | tmux scrollback dies with the server; Sidecar stores no output bodies | Non-goal for first delivery; transcripts are the better agent-history source and shell output may contain secrets |
| Live server binary handoff without process loss | Experimental Unix SCM_RIGHTS transfer by the PTY owner | Sidecar does not own tmux PTY FDs | Non-goal; impossible through supported tmux interfaces |
| Local application update without process loss | Live handoff may replace Herdr server | Sidecar binary replacement leaves the external tmux server and children untouched | Covered already; document and test the release path |
| Remote observation/control | Native remote Herdr flow | Shipped by the Sidecar remote-hosts plan (Phases A–C) behind a flag | Other plan; agent verbs ride its one-shot CLI seam when they exist |
| Host plugins | Herdr plugin manifest/runtime | On-hold Herdr-plugin support plan | Other plan |

## Product boundary: what Sidecar owns

Sidecar remains a presentation-layer tool over files, git, tmux, and harness CLIs. This plan adds a CLI only where Sidecar has accumulated rules and durable state of its own:

- Sidecar owns managed-shell identity, display name, cwd, provider preference, tombstones, and layout attachment.
- Sidecar owns its cross-provider lifecycle vocabulary and conservative blocked/done/freshness rules.
- Sidecar owns the refusal that says a prompt may not be sent to a blocked or replaced agent, and the contract that a start does not succeed until the expected provider is ready.
- Sidecar owns its exact binding between a managed shell and an officially reported provider session reference, plus the policy deciding whether that reference may be resumed after a cold restart.

Sidecar does not own arbitrary tmux pane input, command execution, output matching, or process supervision. An agent that wants to run a command in a raw terminal can use tmux. The remote-hosts Phase C added `sidecar shell send --target --run/--type`, and it stays on the right side of this line for a specific reason: it refuses any session that is not a live Sidecar-managed record for the resolved project, and refuses one recorded on a different tmux server — it is Sidecar's ownership rules wrapped around a send, not a raw pane wrapper. `shell wait-output` and output matching remain non-goals. The new agent commands earn their place because they enforce provider-aware rules that neither raw tmux nor `shell send` can.

Sidecar also does not own the workflow being coordinated. A caller may use `td`, `tasks`, a markdown plan, another harness, or no task engine at all. This plan does not choose planners, reviewers, models, timeouts for an entire workflow, merge policy, or retry topology.

## Agent command contract

### Commands

```text
sidecar agent list [--project PROJECT] [--json]
sidecar agent get [TARGET] [--json]
sidecar agent start [TARGET] --kind KIND [--timeout DURATION] [-- AGENT_ARG ...]
sidecar agent prompt [TARGET] TEXT [--wait] [--until STATUS]... [--timeout DURATION] [--json]
sidecar agent wait [TARGET] [--until STATUS]... [--timeout DURATION] [--json]
sidecar agent read [TARGET] [--source visible|recent|recent-unwrapped|detection|transcript] [--lines N] [--ansi] [--json]
sidecar agent send-keys [TARGET] KEY [KEY ...] [--json]
sidecar agent report-session --kind KIND (--id ID | --path ABS_PATH) [--source SOURCE] [--json]
```

`report-session` is public because provider hooks need a stable executable boundary, but it is described as an integration command rather than a general coordination command and is omitted from the short `sidecar agents` happy-path list. Its help names the trust rules and the current shell requirement.

### Target resolution

The target is a Sidecar-managed shell, not a tmux pane number and not a second agent-alias database:

1. Omitted target inside a managed shell resolves `SIDECAR_SHELL` plus the current tmux namespace.
2. An explicit tmux session name resolves exactly within its namespace/project.
3. A display name is accepted only when unique under the selected project/host.
4. Outside a managed shell, `--project` or a globally unique explicit target is required; no command falls back to the user's currently focused TUI row.
5. The target result carries `HostID`, project key, namespace, tmux session name, pane ID, and server/pane incarnation. Local v1 uses the local host; the identity is host-shaped now so remote support is additive.

The existing shell display name is the human coordination alias. `sidecar shell rename reviewer` already persists and advertises it. A separate ephemeral agent-name namespace would create two names for one Sidecar unit and is not justified.

### `agent start`

- Requires an existing, live, Sidecar-managed target whose foreground process is its interactive shell. A running command, editor, agent, copy mode, or ambiguous process is `agent_pane_busy`; `--force` is intentionally absent.
- Resolves the provider from the shared catalog and constructs a structured argv. Provider arguments after `--` stay separate argv entries; they are never concatenated into a shell command for persistence or resume.
- Sends the launch through the terminal adapter, pins the pane incarnation, and waits until the expected provider is positively identified and reaches `idle` or `done`. `blocked` returns `agent_not_ready` but keeps the target inspectable. A different provider is `agent_kind_mismatch`; process exit is `agent_start_failed`; timeout is bounded and explicit.
- Records provider kind and structured launch metadata only after the command is accepted. It does not claim a native session until an official integration report arrives.
- Returns the same `Agent` JSON shape `list`, `get`, `prompt`, and `wait` use.

`sidecar create shell --agent KIND` becomes the convenient composed path: create a managed shell through the current core, then call the same start service and return only after readiness. `sidecar create worktree --agent KIND` is changed from "command bytes were sent" success to the same readiness contract. Existing `--run` remains ordinary, unclassified command execution and does not imply restore eligibility.

### `agent get` and `agent list`

The machine contract includes:

```json
{
  "target": {
    "host": "local",
    "project": "sidecar",
    "session": "sidecar-sh-sidecar-4",
    "name": "reviewer"
  },
  "agent": {
    "kind": "codex",
    "status": "blocked",
    "freshness": "current",
    "attention": true,
    "evidence": "codex.approval.command",
    "changedAt": "...",
    "capturedAt": "...",
    "interactiveReady": false,
    "sessionRef": {"kind": "id", "reported": true}
  }
}
```

Session reference values are included only for the current shell's own query or an explicit `--include-session-ref` form; ordinary list output reports capability/presence without spraying conversation identifiers across logs. Human output remains compact and `--json` carries the full stable schema.

### `agent prompt` and `agent wait`

- `prompt` accepts `idle`, `done`, or `working`; it refuses `blocked`, unknown identity, stale status, dead target, or replaced occupant before sending input.
- Prompt input uses the same bracketed-paste-aware, ordered send path as the embedded terminal. Extract that behavior from the TUI path rather than writing a second encoder in the CLI.
- `--wait` combines submission and wait under one pinned target, avoiding a race between two commands. Default settled states are `idle`, `done`, and `blocked`; repeated `--until` narrows or widens the accepted set explicitly.
- A prompt begun from a non-working state must produce an observed lifecycle change within five seconds or returns `agent_prompt_stalled`. It does not pretend to identify an agent turn when the target was already working; completion of the existing turn may satisfy the wait, and help text says so.
- Waits are driven by a targeted terminal observer, not the TUI's 5/10/30-second inventory cadence. A polling fallback is allowed for the steel thread, but Phase 0 measures its spawn/CPU cost and the implementation moves to the existing control-mode event stream if polling cannot meet the latency/cost gate.
- Timeout and transport failures are JSON errors on stderr with exit 1; CLI usage is exit 2. There is no implicit timeout.

### `agent read` and `agent send-keys`

- `visible`, `recent`, `recent-unwrapped`, and `detection` are passive terminal snapshots. `recent-unwrapped` joins soft wraps for logs and agent answers. `--ansi` preserves styling only where the source has it.
- Reads never scroll or otherwise manipulate the agent's alternate-screen UI. When terminal history is insufficient, `--source transcript` uses the adapter stack only if the managed shell has an exact provider-reported session binding. It returns structured messages or a clear `transcript_unavailable`; it never guesses the newest same-cwd session.
- `send-keys` accepts a documented logical-key allowlist (`enter`, `esc`, arrows, page keys, `ctrl+<key>`, and other keys the shared terminal mapper can encode). It validates the complete list before writing any bytes. Prompt text goes through `agent prompt`, not a raw string-key escape hatch.
- If a wait or prompt returns blocked, the documented sequence is read first, decide, then send keys. Sidecar does not auto-answer approvals or questions.

## One application core

```text
sidecar agent CLI ─┐
create shell/worktree ─┼─> internal/agentcontrol.Service
future TUI actions ────┘           │
                                   ├─ target resolver (managed shells, host-scoped identity)
                                   ├─ provider registry (launch/resume argv, capabilities)
                                   ├─ activity/status resolver (existing agentactivity + agentstatus)
                                   ├─ session binding store (shellstate v3)
                                   └─ Terminal adapter
                                      ├─ local tmux
                                      └─ future remote adapter (hosts.RunSidecar one-shot verbs)
```

### Package responsibilities

- **`internal/agentcontrol`** owns typed commands/outcomes, target pinning, shell-readiness checks, prompt/wait refusal policy, lifecycle monitoring, and read/key operations. It imports no Bubble Tea package and no conversation plugin.
- **`internal/agentsession`** owns `SessionRef` validation (`id` or absolute `path`), official-source trust, provider resume planning, deduplication keys, and cold-restore decisions. It works on structured values and argv, not shell strings.
- **`internal/agentcatalog`** remains the single family catalog but grows capability-bearing provider entries or small provider adapters: canonical ID/aliases, launch argv builder, resume argv builder, supported session-ref kinds, skip-permissions argument, and the provider metadata consumed by the lifecycle integration manager. The current resume switch in `internal/plugins/conversations/view_content.go` moves here; the Conversations UI, restore coordinator, CLI, and [lifecycle-hook plan](notification-agent-lifecycle-hooks.md) become clients of the same catalog rather than defining parallel provider registries.
- **`internal/agentactivity` and `internal/agentstatus`** keep their existing evidence and presentation jobs. Control code consumes them; it does not add a second lifecycle classifier.
- **`internal/shellstate`** remains the only writer of managed-shell persistence. No command edits `shells.json` directly.
- **Terminal adapter.** The default implementation resolves the tmux session's sole managed pane, foreground process identity, capture sources, control-mode output events, ordered paste, and logical keys. Tests use a fake adapter. The remote-host plan supplies the second implementation through its shipped transport: the proxied control-mode channel for terminal I/O and one-shot CLI verbs for commands.

Do not turn the conversation-history `adapter.Adapter` into the live terminal adapter. Conversation stores and terminal control are independent seams. `agentsession` may query a matching history adapter after an exact binding exists, but a missing/disabled Conversations plugin must not disable start, prompt, wait, or restore metadata.

## Persistence and restore design

### Keep the existing stores; do not add a competing `session.json`

Sidecar already has the state Herdr puts in one file, separated by ownership:

- `shells.json` is durable managed-shell identity and recreation data.
- global/project `state.json` holds selection and per-surface `PaneLayoutJSON` trees.
- tmux holds the live processes, PTYs, and scrollback.

Adding a fourth aggregate `session.json` would create two authorities for shell identity and pane layout. Evolve the existing stores instead. `shells.json` moves from schema v2 to v3 with additive structured agent/restore fields; `PaneLayoutJSON` does not change for this plan because it already stores tree shape, ratios, focus, cwd-owned surface identity, and tmux session selectors.

Proposed v3 shape, illustrative rather than a locked Go struct:

```json
{
  "tmuxName": "sidecar-sh-sidecar-4",
  "displayName": "reviewer",
  "namespace": "/private/tmp/tmux-501/default",
  "createdAt": "...",
  "workDir": "/Users/marcus/code/sidecar",
  "agent": {
    "kind": "codex",
    "launchArgv": ["codex", "-m", "gpt-5.4"],
    "session": {
      "source": "sidecar:codex",
      "kind": "id",
      "value": "019f...",
      "reportedAt": "..."
    }
  },
  "restore": {
    "policy": "inherit",
    "eligible": true,
    "lastSeenIncarnation": "...",
    "lastSeenAliveAt": "..."
  }
}
```

Rules:

- Schema v3 keeps v2's newer-writer refusal. A v2 reader must never rewrite and drop v3 fields; compatibility tests exercise old/new binaries' allowed direction explicitly.
- `launchArgv` is recorded only for catalog-built provider launches. Existing arbitrary configured `agentStart` shell strings may still launch interactively, but they are not automatically replayable and are stored only as a display diagnostic if needed.
- Session IDs are capped, reject control characters, and are never interpolated into a shell string. Session paths must be absolute, normalized, and within a provider-approved store root before automatic resume.
- Only an installed official Sidecar integration source may set an auto-resumable reference. Same-cwd adapter discovery may propose a manual candidate but never marks it `reported` or auto-resumable.
- A new report replaces the prior reference only when it comes from the current pinned provider/process generation. Late hook output from an exited/replaced process is ignored.
- Session deduplication is global per host: one exact native session reference may resume into at most one managed shell. Duplicates restore as plain shells and report the conflicting target.
- Shell records are written on meaningful transitions—launch accepted, exact session reference changed, restore policy changed, confirmed live/dead incarnation transition—not on every output capture.

### Restore policy

Configuration exposes two independent choices:

```json
{
  "plugins": {
    "workspace": {
      "sessionRestore": {
        "recreateShells": true,
        "resumeAgents": "ask"
      }
    }
  }
}
```

- `recreateShells` defaults **true**. It recreates only interactive managed shells that were confirmed live in the prior tmux server incarnation, under their same name and existing cwd. It never replays arbitrary foreground commands.
- `resumeAgents` is `off | ask | auto` and defaults **ask**. `ask` paints the restored shell/layout and presents one grouped restore summary after the first frame; nothing paid or agent-mutating starts until the user confirms or runs the CLI. `auto` is explicit user authorization for exact, official session references only.
- A per-shell policy `inherit | shell | resume | never` exists in v3 so long-running servers, disposable helpers, and sensitive agent sessions can differ without changing the machine default. The TUI setting is added only after the headless policy is implemented and tested; the CLI can set it first.
- A missing cwd, deleted worktree, unavailable provider binary, invalid/stale reference, duplicate reference, or provider mismatch degrades to a visible offline/restored-shell result with an exact reason. No fallback directory and no fresh replacement conversation.

### Restore entry points

```text
sidecar session status [--json]
sidecar session restore [--dry-run] [--shell TARGET] [--agents] [--yes] [--json]
sidecar session policy [TARGET] [--shell|--resume|--never|--inherit] [--json]
```

- `status` and `restore --dry-run` are read-only and work without a running TUI. Their ordered plan names every shell as `reattach`, `recreate-shell`, `resume-agent`, `manual`, `skip`, or `refuse`, with the reason and whether external agent execution would occur.
- Plain `restore` follows configured policy. `--agents` requests eligible agent resumes; `--yes` is required when no TUI can confirm and the effective policy is `ask`.
- Automatic startup restoration calls the same planner/executor after first frame. It is not a hidden TUI implementation.
- The tmux session name is the idempotency key. Every step rechecks `has-session`, foreground occupant, server incarnation, and session binding under the shell manifest lock before mutation. A crash after session creation but before completion converges on retry rather than creating a second shell or agent.
- The restore executor never kills a conflicting live session. A name collision is a refusal shown to the user.

### Cold-restore ordering

1. Read state and shell manifests; validate schema versions; compute the current tmux server incarnation through `internal/tmuxserver`.
2. Paint the first frame. Startup tracing must show no tmux spawn, provider-store walk, or restore write before `first ready frame`.
3. Build a pure restore plan from the prior confirmed-live set, current tmux inventory, exact cwd existence, policy, provider capability, and dedupe set.
4. Recreate eligible shell sessions with `workspaceops` under the same names and environment identity. Never stop or restart the tmux server.
5. Let existing project/global pane decoding reattach by session name. Do not write a parallel tree or compositor.
6. Resume eligible exact agent sessions according to policy through `agentcontrol.StartResume`, then wait for provider identity/readiness with the same contract as `agent start`.
7. Persist one outcome per shell and surface a grouped summary. Failures are retryable and never prune shell records or pane layouts.

## Provider integrations and session identity

The steel thread supports Codex and Claude because Sidecar already has launch, activity, transcript, and resume knowledge for both. The interface must not hardcode those two:

1. Add `sidecar agent report-session` and the `agentsession` trust/validation core.
2. Install minimal provider hooks that call the command from inside the managed shell. The command derives the shell target from `SIDECAR_SHELL`; hooks do not receive a writable path to `shells.json`.
3. Record source/version so an outdated integration can be reported honestly and upgraded without changing the shell schema.
4. Expand to every catalog provider for which Sidecar can build and verify a native resume argv. Providers without native resume still gain start/get/prompt/wait/read and restore as plain shells.

Integration installation, status, versioning, safe configuration merge, and CLI/Configuration surfaces are controlled by [Deterministic agent lifecycle hooks](notification-agent-lifecycle-hooks.md). This plan contributes the session-reference validator and resume capability metadata to that shared application service. `sidecar agent integration status [--json]` reports provider, installed version, lifecycle authority, session-identity capability, and minimum version; session-only hooks report identity only and existing screen/process detection remains the status authority.

## Interaction with remote hosts

The local steel thread ships independently. [sidecar-remote-hosts.md](sidecar-remote-hosts.md) has since completed its mutation Phase C, and its shape settles how agent control travels: mutations are one-shot `ssh <target> sidecar <verb> --json` invocations over the existing ControlMaster (`hosts.RunSidecar`), not a request channel in `hostproto` — `hostserve` stays read-only by construction. That is exactly the seam this plan's commands were designed for. Every `sidecar agent` verb is headless, target-taking, and `--json` from birth, so remote agent control is transport plumbing plus host-scoped target identity, not new protocol work:

- `agent list/get/start/prompt/wait/read/send-keys` run on the host as ordinary CLI invocations through `hosts.RunSidecar`, following the exit-code discipline the Phase C verbs established (2 usage/version skew, 5 value rejected) and decoding results through its banner-tolerant result decoder. An older host answers a verb it lacks with a usage error the viewer already renders as version skew — capability negotiation falls out of the exit-code contract.
- Waits are the one shape that strains a one-shot invocation. `agent wait --timeout` runs as a bounded invocation whose deadline the caller owns, matching `hosts.RunSidecar`'s deadline discipline; a resident subscription channel is not built unless real usage shows the bounded form is insufficient.
- Target identity includes `HostID` from the beginning, so local and remote shells with the same tmux name cannot collide.
- Session references stay on the host that owns the provider store. The viewer receives presence/capability by default; exact IDs/paths cross SSH only for an explicit operation and are not persisted into the viewer's local `shells.json`.
- Cold restore executes on the host. A remote viewer may request/observe it, but it never reconstructs the host's state locally.

## Live handoff: explicit non-goal and operational posture

Herdr can attempt live server handoff because Herdr itself holds each PTY master FD and can pass those descriptors to a replacement process. Sidecar is a tmux client. It can capture bytes, send input, and recreate sessions, but it cannot obtain or transfer tmux's PTY ownership through a supported interface.

Therefore:

- **Sidecar updates:** supported without process loss today. Installation replaces an executable on disk; the old Sidecar process and the independent tmux server continue. Relaunch reattaches.
- **tmux package updates:** leave the old server running until a user-chosen maintenance point. Do not call `kill-server`, restart the default server, or claim the update is complete if the running server remains old; report the distinction.
- **tmux server restart/reboot:** cold reconstruction only. Agents with exact native session references may resume conversations; arbitrary compiles, servers, editors, and in-memory shell jobs are lost and are never described as preserved.
- **Future revisit trigger:** only a supported tmux upstream handoff/export API, or a deliberate Sidecar-owned PTY runtime, changes this conclusion. Neither is proposed here.

## Work sequence

### M0 — Contract spike and current-state fixtures

- Capture the current `sidecar agents`, create-shell/worktree JSON, shell manifest v2, project/global pane-layout round trips, and activity states as compatibility fixtures.
- On an isolated tmux server, prove the reliable shell-readiness signal on macOS and Linux: shell foreground process group, current command, pane ID/incarnation, copy mode, and a busy foreground process. A false ready verdict is a hard stop.
- Prototype a targeted lifecycle watcher after one prompt. Compare bounded polling against the existing control-mode event stream for transition latency, process spawns, idle CPU, and behavior under an output burst. Record the choice in this plan before M1 implementation.
- Prove bracketed-paste-safe prompt submission through the extracted terminal sender, including multiline text, Unicode, shell metacharacters, and a prompt whose contents resemble tmux format syntax.
- Define and freeze the `Agent`, target, status, and error JSON schemas. Error codes include `agent_not_found`, `agent_pane_busy`, `agent_kind_mismatch`, `agent_not_ready`, `agent_start_failed`, `agent_blocked`, `agent_prompt_stalled`, `agent_replaced`, `transcript_unavailable`, `timeout`, `transport_failed`, and the rollout-gate code `feature_disabled`.

**M0 observer decision (measured 2026-08-30):** `TestM0ObserverPollingVersusControlManagerMeasurement` ran both candidates against the same private tmux server and a 250-line output burst. At a 100 ms polling cadence, a 300 ms idle window spawned 6 tmux children and consumed 17,640 µs of child CPU; the burst was observed in 110 ms and required 2 more children. One existing `tty.ControlManager` client produced 0 idle snapshots, 0 additional spawns, and 0 µs of measured CPU during the same 300 ms idle window; it observed the burst in 42 ms with 0 additional spawns. The test also proves the control client drains the burst and closes through the subscription/manager lifecycle. Decision: use the existing pooled `ControlManager` for sustained targeted prompt/wait observation in M2; its event-driven latency and zero idle spawn cost are materially better, and a second read-only tmux client is not justified. M1's start-only wait may retain 100 ms bounded polling because it exists only between an explicit launch and its explicit timeout, has a simple pinned-occupant preflight, and leaves no resident idle poller; it is not acceptable for general lifecycle watching.

**M0 compatibility fixtures:** `TestM0AgentDiscoveryCompatibilityFixture` pins the agent-facing discovery lines, `TestM0CreateJSONCompatibilityFixtures` pins create-shell/worktree JSON, `TestM0VersionTwoManifestCompatibilityFixture` pins `shells.json` v2, `TestWorkspacePaneLayoutJSONRoundTrip`, `TestSessionsPaneLayoutsRoundTripAndSkipUnchanged`, and `TestDecodeEncodeRoundTripStable` pin project/global pane-layout persistence, and the provider fixture suites under `internal/agentactivity/testdata` plus their detector/tracker tests pin current activity states. The new `TestAgentAndErrorJSONContracts` and `TestStatusAndErrorCodeVocabularyIsFrozen` fixtures freeze the M0 target, `Agent`, status, and error envelope rather than deriving a new wire shape from the CLI renderer.

**Exit gate:** one fake provider driven through real isolated tmux reaches shell-ready → start → detected idle → prompt → working → done/blocked → read, with the pane target pinned throughout and zero access to the default tmux server or real Sidecar state tree.

**Implemented proof:** `TestIsolatedTmuxFakeProviderSteelThread` exercises that complete journey through `agentcontrol.Service` and `LocalTerminal` on the package's private tmux server, including exact multiline Unicode, shell-metacharacter, and tmux-format-like paste contents. The target's namespace, server incarnation, pane ID, and pane PID remain pinned through every transition. `TestIsolatedTmuxRefusesBusyForegroundAndCopyMode` proves the real tmux preflight rejects both a foreground `sleep` and copy mode. The Linux-only `TestForegroundShellReadyLinuxFixtureMatrix`, executed in a Linux container as well as cross-compiled from macOS, proves the sole-shell positive case and foreground-command, shared-helper, unknown-process, and current-command-mismatch refusals against a fixture `/proc` tree.

### M1 — Agent query and start steel thread

- Add `internal/agentcontrol` with injected terminal/clock/provider dependencies and a local tmux adapter.
- Extract target resolution from the existing shell CLI paths into one host-shaped resolver. Add `agent list`, `agent get`, and `agent start` with JSON/human output and gendoc metadata.
- Extend `agentcatalog` with typed launch capabilities; route workspace/TUI start, `create worktree --agent`, and new `create shell --agent` through the same service. Delete readiness-free command-send paths where they are no longer needed; keep ordinary `--run` separate.
- Reuse `agentactivity`/`agentstatus` for provider and lifecycle truth. Do not let launch preference claim the live provider before process evidence.
- Add only provider-neutral TUI affordances needed to show start/refusal outcomes; do not add an orchestration plugin.

**Exit gate:** from a Sidecar-managed shell, one command creates a sibling shell and starts a fake provider; `agent start` returns only at ready. Codex and Claude live opt-in proofs confirm the expected process and idle state, with no paid prompt submitted.

**Implemented proof:** project Workspace, global Sessions, `create shell --agent`, `create worktree --agent`, and `sidecar agent start` now converge on the same service and typed launch catalog. `TestLiveProviderStartGet`, run with `SIDECAR_LIVE_AGENT_PROVIDERS=codex,claude`, launched Codex CLI 0.151.0 and Claude Code 2.1.220 on the private tmux server, observed each at an interactive idle composer, and passively fetched the same pinned target without submitting a prompt. Codex's stable no-marker composer is reported as positively identified inferred idle, never as a manufactured completion.

### M2 — Prompt, wait, read, and logical keys

- Extract ordered bracketed-paste and logical-key encoding into the terminal adapter shared by TUI and CLI.
- Add `agent prompt`, combined prompt+wait, standalone `agent wait`, passive `agent read`, and validated `agent send-keys` with pinned-occupant semantics.
- Add the targeted observer chosen in M0, with cancellation/timeout cleanup and no leaked control clients or goroutines.
- Add exact-transcript reads behind an injected session-message reader, disabled until M3 supplies an exact binding.
- Update `sidecar agents`, CLI reference, AGENTS guidance, and a repository skill based on the Herdr skill's safe sequence: discover, create layout separately, start, prompt/wait, read before keys, preserve focus, never close a target the caller did not create. The tracked skill source remains canonical and any harness mirrors are generated/copied by the repository's existing skill workflow.

**Exit gate:** two isolated managed agents run concurrently; the caller prompts each, one returns done and one blocked, waits cannot be satisfied by replacement processes, and reading/sending keys to the blocked agent affects only the named shell. A `tmux-drive.sh` demo puts both panes in front of the user without stealing focus during creation.

**Implemented proof (2026-08-30):** `TestTwoIsolatedAgentsPromptWaitReadAndKeysInvolveOnlyTheirOwnShell` runs both agents concurrently on the package's private tmux server, settles one at `done` and one at `blocked`, reads the blocked one, answers it with logical keys, and proves the untargeted shell's screen is byte-identical before and after. `TestWaitCannotBeSatisfiedByARealReplacementOccupant` hands the pane to a `respawn-pane -k` process that prints the exact marker the wait is looking for and the wait returns `agent_replaced`. `TestObserverLeavesNoControlClientOrGoroutineBehind` exercises the timed-out, cancelled, and satisfied exits from the observer and proves no control client or goroutine survives.

The ordered input encoding lives in `internal/tty/agentinput.go`: `EncodeLogicalKey` builds a `tea.KeyPressMsg` and hands it to the existing `MapKeyToTmux`, so a headless key and a typed key are the same bytes by construction — pinned in both directions by `TestEncodeLogicalKeyMatchesTheEmbeddedTerminalEncoding`. `PromptSteps` is the ordered paste-then-Enter submission both the TUI launch path and `agent prompt` use.

The observer follows M0's decision with one split: the pooled `tty.ControlManager` is the *signal* that decides when to look, and `Inspect` remains the *truth* that decides what is so. Control events carry the screen, title, and current command but not pane identity, death, copy mode, or the server, so every verdict is confirmed by a full `Inspect` before it is returned and a slow verification heartbeat runs regardless of signals.

**Correction found in M2:** the M1 occupant pin compared `ServerIncarnation`, whose string embeds the socket's ctime. tmux rewrites its socket's permission bits whenever the set of attached clients changes, so attaching M2's own control-mode observer bumped the ctime and made every observed target report itself replaced the instant it began being observed. `Target` now carries `ServerPID` and `sameOccupant` compares that; the incarnation stays as reported evidence about the socket, and is no longer compared.

### M3 — Exact session reporting and unified resume registry

- Add `internal/agentsession`, v3 shell manifest fields, `agent report-session`, generation fencing, validation, redacted list behavior, and global deduplication.
- Move every resume command builder out of the Conversations plugin into the provider registry as structured argv. The optional Conversations UI consumes the registry and keeps its current user-confirmed behavior.
- Build and test Codex and Claude session-report mappings first through the shared lifecycle integration assets; then add providers with proven native resume commands one adapter at a time. `sidecar agent integration status` reports unsupported/missing/outdated honestly.
- Add an exact-bound transcript reader that resolves the bound session through the provider's existing history adapter without constructing the Conversations plugin.

**Exit gate:** a fake hook can report, rotate, clear, and attempt a stale late update; only the current process generation wins. Real Codex and Claude sessions bind to the exact current conversation and `agent read --source transcript` returns that conversation, not the newest other session in the same cwd.

**M3 implemented 2026-08-30 (`td-8ec2cc`); exit gate passed live on both halves.**

`internal/agentsession` owns the rules as values: reference validation (byte caps, C0/C1/DEL rejection, a character set containing no path separator and no shell metacharacter, absolute and normalized paths confined to per-provider approved store roots), official-source trust, generation fencing, dedup identity, and structured resume planning. It imports no tmux, CLI, or provider-config package, so every rule is testable without a terminal. `shells.json` is schema v3: `agent` and `restore` are additive pointer objects, so a v3 rewrite of a record that has never run an agent is byte-identical to what v2 wrote, and v2's newer-writer refusal is preserved — which matters more here than it did for tombstones, because a v2 binary rewriting a v3 file would drop a session reference and the symptom would not be an error but a restore that quietly declines to resume. `shellstate` remains the only writer, and the fence is evaluated *inside* the manifest lock: it is a read-decide-write, so two concurrent hook processes evaluating it outside the lock would both believe they won.

`sidecar agent report-session` reads the provider's hook payload as bounded JSON on **stdin**, so the installed hook command is fixed argv with no provider data interpolated into it — the shape the [lifecycle plan](notification-agent-lifecycle-hooks.md) asks for, and the reason no interpreter or script asset is needed for Codex or Claude. It decodes only four fields, so prompt and tool content cannot reach a record even by accident; it suppresses sub-agent events; and its result never carries the reference value. Redaction is asymmetric by design: `kind` and `reported` answer "can this be resumed and did an official integration vouch for it", while the value appears only for a shell's own query or `--include-session-ref`, because list output lands in logs and CI artifacts and a conversation identifier written there cannot be unwritten.

Every resume command builder moved out of `internal/plugins/conversations/view_content.go` into `agentcatalog` as structured argv with canonical aliases, and the plugin file no longer imports the workspace plugin at all. The agent-launch path now has exactly two argv-to-shell renderers, both in the catalog: `ShellCommand` always quotes and is execution-only, `DisplayCommand` quotes only entries containing a character a shell acts on. (`hosts/ssh.go` has its own renderer for a different transport, and `workspace/env.go`'s `shellQuote` is a single-value quoter for `export K=V` rather than an argv renderer; neither is in this path.) `agentcontrol.quoteArgv` delegates to the first. `internal/agenttranscript` implements the `TranscriptReader` seam M2 left injected, over the existing adapter stack and without constructing the Conversations plugin.

**A defect the proof found that no test would have.** The generation fence compares what the reporting hook derives about itself against what currently occupies the pane. `lifecycleenv.Context.ProcessGeneration` *falls back* to the pane's root process when it cannot walk from the hook to the pane, and `LiveProviderGeneration` returns the pane's root process when nothing is running there. Those are exactly the two conditions that hold for a hook left behind by a provider that has exited: it has been reparented away from the pane, and the pane is empty. Both sides fell back, compared equal, and the late report was accepted — in precisely the case the fence exists to reject. `ReportingProviderGeneration` is the same walk with no fallback: a reporter that cannot prove which provider it belongs to is refused. Two fallbacks agreeing is not evidence. `TestTheStrictGenerationRefusesWhereTheLenientOneFallsBack` pins the difference rather than the fix.

**Exit gate, first half (fake hook, live).** On a sandbox isolating HOME, all four XDG roots, `TMUX_TMPDIR`, `-config`, and `SIDECAR_ISOLATED_STATE=1`, a fake provider owning a real Sidecar-created pane called the real CLI as its own child: report bound `sess-alpha` at v3 with `reported=true`; an identical replay left the manifest byte-identical; a rotate moved it to `sess-beta`; an orphaned reporter armed inside the provider and fired six seconds after the provider was killed was refused with `stale_generation` and left the manifest byte-identical; a new provider generation took the pane over and rotated to `sess-gamma`; a clear from the live generation unbound it.

**Exit gate, second half (real providers, live).** Two managed shells in **one** working directory, per provider. Claude Code 2.1.220 bound `83a5396b…` and `d7c2d784…`; the bound shell's transcript is the **older** of the two files on disk and the newest in that cwd is the other one, so a newest-same-cwd guess would have returned the wrong conversation and the exact binding returned the right one. Codex CLI 0.151.0 bound `01a05614-0ca7…` and `01a05614-1cdf…` with the same ordering, and authenticated, so its bound transcript carries a real model reply rather than only the user's message. Redaction was checked on the same run: `agent get` default returned `{"kind":"id","reported":true}`, `--include-session-ref` added the value, and `agent list` carried no value for either shell.

**Codex needs three owned mutations, and the third one is a hash.** Codex records a per-hook `trusted_hash` in `config.toml`; without a valid one it refuses to run the hook and prompts instead. The algorithm is not documented, and an initial black-box attempt to recover it failed — sha256 over the command string, the state key, their concatenations, the hook object in several JSON encodings, and the whole `hooks.json` file all missed. It was then read out of the `codex-rs` source: the hashed value is a canonical serde encoding of the hook group, `{"event_name":"session_start","hooks":[{"async":false,"command":…,"timeout":…,"type":"command"}]}`, with serde's non-HTML-escaping string encoding. It is verified against two independent real pairs on this machine: the developer's own installed third-party hook, and — from the live proof — the hash **Codex itself wrote** after being asked to trust Sidecar's hook interactively. That second pair is the useful one, because it was produced by Codex for Sidecar's exact command rather than recovered from someone else's installation.

The behavior when the entry is absent was measured rather than assumed: Codex does not silently refuse, it shows an interactive "Hooks need review" prompt, and answering it makes Codex write the entry itself. So the trust entry is a convenience the installer can now provide correctly rather than a wall — and the fallback, if the encoding ever drifts, is a one-time prompt rather than a broken integration. That is the right shape for a security control to fail into.

**Codex and Claude ship at session-identity tier through the Phase C installer.** Both are new `agentintegration.Adapter` implementations, registered in `DefaultAdapters()`, and `agent integration list/status/install/update/repair/uninstall` and the Configuration route pick them up without provider branching. Their capability entries are corrected to what Sidecar actually ships — tier `session-identity`, covering `session_identity` only, asset version 1 — so a hook that reports which conversation is running can never be read as lifecycle authority; full lifecycle for these two is Phase D of the [lifecycle plan](notification-agent-lifecycle-hooks.md) and deliberately not here. Neither needs a script asset: because `report-session` reads the provider payload from bounded stdin, the installed hook command is fixed argv, so there is nothing to interpolate and no interpreter to depend on. Claude's entry carries a `matcher`, Codex's does not, matching each provider's schema. Verified live in the sandbox: `install --dry-run` produced an op list byte-identical to the real run, install left the sandbox `~/.codex` and `~/.claude` correct, uninstall backed up and removed only Sidecar's own content, and the developer's real provider trees were never written — their mtimes predate the work.

**Adapter-interface friction, since this was the interface's first test beyond OpenCode.** The ordered `Op` engine held without a new op kind, and the dry-run guarantee stayed structural. What did not fit is `Asset`: it is singular and its ownership model is a marker comment in the file's own bytes, which works for OpenCode because OpenCode's integration *is* one Sidecar-owned file dropped in a directory. Codex and Claude instead add an entry to a shared, user-owned config file that has no comment syntax, and Codex needs two files plus a hash. The adapters therefore overload `FileState.Owned` to mean "this file contains our entry" and return a synthetic `Asset` that nothing consumes, and ownership becomes a content rule — the entry whose command invokes `sidecar agent report-session` — rather than a marker. That rule is test-pinned against lookalikes, but it is a second ownership model living behind an interface that only describes the first. Phase D should either make `Asset` plural and let an adapter declare its own ownership predicate, or admit entry-in-file integrations as a distinct adapter shape. One further limitation is recorded rather than fixed: `config.toml` is edited by a line scanner that refuses anything it cannot parse, including multiline strings. (Corrected in M4 per td-71b508: an earlier version of this paragraph attributed that refusal to a TOML dependency being out of scope, which the very next paragraph contradicts — `github.com/pelletier/go-toml/v2` did land, as a read-only validation oracle. The scanner's limits are a deliberate consequence of preserving comments and ordering by writing lines rather than serializing a parsed document, not of a missing dependency.)

**The Codex config editor is a line writer with a parser as its oracle.** `config.toml` is a shared user file, so it is edited surgically to preserve comments and ordering rather than by serializing a parsed document. An independent review found that the line scanner *guessed* where its own contract said it refused: four legal TOML spellings — a quoted table name, a quoted key, an inline-array element line read as a header for want of bracket-depth tracking, and a spaced dotted header — were neither recognised nor refused, so the composer appended a duplicate table or key and produced a file Codex cannot parse, silently costing the user every Codex setting. A second finding: a state table's span ran to the next header or EOF and comments never closed it, and since Sidecar appends its table last, anything a user parked at the end of the file was destroyed on uninstall.

Both are fixed, and the shape of the fix is the durable part. `github.com/pelletier/go-toml/v2` is now a **validation oracle only** — read-only, never used to serialize, so format preservation remains the writer's job. The file is parsed before planning, and a file that is not valid TOML is refused rather than edited around. The composed output is parsed again and semantically diffed against the pre-image: every key the user owned must still be present and unchanged, Sidecar's own entry present or absent as intended, or the plan refuses and nothing enters the Op list. The scanner's own honesty is fixed too, so the oracle is a second line of defence rather than the only one, and a table's span now ends at its last real content so trailing comments belong to the file. `TestCodexOracleRejectsARewriteThatLosesAUserKey` is the proof the oracle is not decorative: it first asserts that every check the old scanner-only verifier made *passes* on a doctored image, and only then that the oracle rejects it and names the setting that would have been lost.

Verified live as well as in tests: on the sandbox, seeding a realistic user `config.toml`, installing, and starting real Codex 0.151.0 against the result gave exactly one `[features]` table and one `hooks` key, a clean start, and Codex displaying the user's own `model` setting — a duplicated table or key is precisely what would have stopped it starting. Parking a comment and a `[scratch]` table at EOF after install and then uninstalling left both intact.

**Two limits worth stating.** A `config.toml` containing a multi-line string is refused outright: the oracle can confirm such a file is valid, but the line scanner cannot safely locate headers when a `"""` block may contain text that looks like one, so Codex integration is unavailable for that user rather than merely careful. And the scanner now refuses any `key = value` line it cannot normalise, not only ones under `features` or `hooks`, which is stricter than necessary. Both fail in the safe direction — refusing to edit — and both are recorded rather than hidden.

**Two deviations from the illustrative v3 shape.** `restore.lastSeenIncarnation` is named `lastSeenServer` and holds the tmux server **pid**, following Phase B of the lifecycle plan: `tmuxserver.Incarnation.String()` embeds the socket's ctime, which tmux rewrites whenever the set of attached clients changes, so a field keyed on it would go stale the moment a user attached a client. The pid is stable for the server's lifetime and new after a restart. And `agent.kind` is stored alongside — not instead of — the existing v2 `agentType`: the older field stays authoritative for everything that already reads it, and the single writer keeps the two in step.

`restore` is defined and round-tripped but nothing writes it yet. Populating eligibility is the job of the component that consumes it, which is M4's planner; shipping the field now is what lets M4 be additive rather than another schema bump.

**Honest limits.** The copied OAuth credentials did not authenticate inside the sandbox HOME for Claude, so each Claude assistant turn is a 401 error string rather than a model reply. The gate does not depend on it — the two conversations are distinguished by their user messages, both transcripts are real files written by real Claude sessions, and the adapter read them through the real binding — but only the Codex half proves the path with a genuine model response. Dedup is implemented and unit-tested as a pure function over holders, and `report-session` reports a conflicting target, but nothing consumes the verdict yet because the component that would act on it is M4's restore planner. `--clear` is an addition to the command surface the plan sketched, needed because the exit gate requires a hook to be able to clear a binding.

### M4 — Cold shell/layout restoration

- Add the pure restore planner, `sidecar session status/restore/policy`, v3 per-shell policy, and executor over `workspaceops` plus `agentcontrol`.
- Run startup restoration asynchronously after first frame; show an `ask` summary through the existing notification/modal system and keep the CLI path authoritative.
- Recreate only previously confirmed-live managed shells under the same name and cwd. Let existing project/global pane persistence reattach them; add no new layout store.
- Resume exact sessions only after all refusals/dedupe checks and explicit policy. Store outcomes without pruning any shell or pane state.
- Cover crash/retry points after plan, after shell creation, after layout attachment, after resume send, and while waiting for provider readiness.

**Exit gate:** on a fully isolated tmux socket and state tree, create a project surface and global Sessions surface with mixed passive panes, two managed shells, and one bound fake agent; terminate only the isolated tmux server; relaunch Sidecar; confirm first-frame timing, exact tree/focus/ratios, same session names/cwds, shell-only automatic restoration, ask-policy non-execution, confirmed resume into the exact conversation, and idempotent second restore. The default tmux server is never inspected or mutated.

**M4 implemented 2026-08-30 (`td-e78e17`).**

**The reap path was the first thing fixed, because it is what the milestone is for.** On the day this was written a user's default tmux server crashed and Sidecar's known wipe-on-death bug destroyed the `sidecar` and `braid` shell records. The mechanism is not a defect in the writer. When a server dies every managed session disappears in the same instant, so a liveness pass asks "is this one shell gone?" once per shell, gets a true-looking answer every time, and tombstones the whole file. The existing guards — the empty-listing guard, the incarnation fence, the probe that returns Unknown on any error — are all real, and a plan that added a fourth would be guarding a question that has no true answer at that moment.

So the liveness path no longer asks for a deletion in that situation; it asks for a reclassification. `shellstate.ForgetOrPreserveAtPath` preserves the record and marks it a cold-restore candidate when no tmux server is running at all, or when the record's last confirmed server is not the one running now. A shell that vanished inside a server that is still up is still tombstoned, because that is a terminal someone closed, and it is still recoverable by `sidecar shell restore`. `shellliveness.ForgetFunc` gained the observed server identity so the decision is made with the evidence rather than around it.

Both destructive paths were fixed, and the second one mattered more than the first. The global browser reaps through `PlanReap`'s guards; the project workspace plugin reaps **per shell**, driven by one capture failing, with no listing to apply an empty-listing guard to, through an unconditional `RemoveShell(tmuxName)` that took no `CreatedAt` fence. That was the most exposed path in the system and it had the least protection. `ShellManifest.ReapShell` applies the same rule in that surface's own serializer and lock.

The proofs assert on the file rather than on a return value: `TestServerDeathPreservesEveryShellRecord` drives three shells through the write path with no server running and requires three records and zero tombstones afterwards, and `TestReapShellSurvivesAServerDeath` is its counterpart against the workspace plugin's separate serializer. `TestSingleShellExitInTheSameServerStillTombstones` and `TestReapShellStillTombstonesAClosedTerminal` prove reaping was not merely switched off, and `TestTombstoneBranchKeepsTheCreatedAtFence` proves the reused-name fence survived the change.

**Open question 3 is settled without a second ledger.** The existing `tmuxserver` incarnation tracker plus shell liveness transitions are sufficient. The global browser reads eligibility off the pane listing its refresh already takes, and the project plugin off `noteShellAlive`; neither adds a tmux invocation, because `#{pid}` — the server-scoped pid — was added to the `list-panes` format string the refresh was already running. Write amplification is avoided by writing only on a transition: a record already marked eligible under the same server is left completely alone, timestamp included, so the steady state is one write per shell per tmux-server lifetime rather than one per refresh. `TestObserveLiveWritesOnlyOnTransition` and `TestMarkRestoreEligibleWritesOnlyOnTransition` assert the file bytes are unchanged across repeated observation.

The identity written down is the server **pid** in `pid=N` form, never `Incarnation.String()`. This is the same reasoning M3 recorded for naming the field `lastSeenServer`, and it is now enforced by a single producer, `Incarnation.ServerID()`, which returns the empty string for an unknown or absent server rather than inventing `pid=0` — a caller comparing markers has to be able to tell "a different server" from "no observation", because those two lead to opposite decisions in the reap path.

Shell creation also stamps eligibility directly, in `CreateManagedShell`. Creation is the strongest liveness evidence available — the session was just made, in this server, by this process — it costs no extra write because the record is being written anyway, and it closes the window in which a shell created seconds before a crash would come back as "never confirmed live" and be left for the user to recreate by hand.

**`internal/sessionrestore` is a pure planner with an executor beside it.** `Build` takes the prior confirmed-live set, the current tmux inventory, exact cwd existence, policy, provider capability and the global dedupe set, and returns an ordered plan naming every shell `reattach`, `recreate-shell`, `resume-agent`, `manual`, `skip` or `refuse`, with a machine reason code, a human sentence, and whether performing it would run an agent. It does no I/O at all, which is what lets the whole refusal matrix be described in a table test rather than in a terminal.

Two orderings inside the planner are decisions rather than accidents. A live managed session is answered before policy is consulted, because "never restore this shell" is not a reason to disturb one that is already running (`TestPlanReattachDoesNotDependOnPolicy`). And deduplication is checked before provider capability, because when another shell has won a conversation, naming the winner is more useful than naming a capability that was never the binding constraint. Dedup is global per host across projects, not per manifest, since the same reference recorded in two projects would otherwise put one provider session in two panes (`TestPlanDedupSpansProjects`).

A plan carries the reference *kind* and whether it was reported, and never the reference value. A plan is printed, logged, and pasted into issues, and M3 established that a conversation identifier written into a log cannot be unwritten. `TestPlanNeverCarriesTheSessionValue` and `TestSessionStatusNeverPrintsTheSessionValue` pin it on both the library and the CLI.

**The executor rechecks at the moment of use, keyed on the tmux session name.** Every mutation re-asks what holds the name, whether the server is still the one the run adopted, and — before a resume — re-reads the binding from the manifest rather than trusting the plan's copy, since an integration can have rotated or cleared it in between. A foreign holder of a name is a refusal that leaves it running; there is no code path that kills a session to take its name. `TestExecuteConvergesAfterEachCrashPoint` walks the five crash points the plan names (after the plan, after shell creation, after layout attachment, after the resume send, during the readiness wait) as the world state each would leave, and requires convergence on one shell and at most one agent in every case.

`agentcontrol.StartResume` is a thin composition over `Start` rather than a second launch path, so a resume inherits the pinned occupant, kind mismatch, early-exit and blocked contracts instead of being the one path in Sidecar that could report success into an empty pane. It adds the refusal that belongs to resuming specifically — an unreported reference is rejected before any bytes are sent — as a second check on a rule `PlanResume` already enforces, at the boundary where the bytes would actually be written.

**Startup restoration runs strictly after the first frame, and that is enforced rather than intended.** Bubble Tea does not guarantee that a command returned from `Init` runs after the first render, so the restore waits on the same `firstReadyFrameLatch` the resource providers use before it reads a manifest, spawns tmux, or writes. Measured on the reboot harness with two restore candidates: `first ready frame` at 89ms, `session restore` beginning at 92ms and taking 131ms — entirely behind the paint, with no restore work in the trace before the mark. `TestRestoreDoesNoWorkBeforeTheFirstReadyFrame` pins the ordering every run, and it was checked against a deliberately broken build that hoists the work above the gate; it fails there, so it is not decorative. A configuration with restore fully off returns no command at all, so those users pay no goroutine, no manifest read and no tmux call.

Under the default `ask` policy the restore paints the shells and posts exactly one grouped summary naming what came back, what was refused, and which conversations are waiting with the command that resumes them. It never resumes on its own: a resume can spend money and change a repository, and a reboot is not consent. An ordinary restart where every shell is still running says nothing at all, because a notification for the common case is noise.

**The CLI is authoritative and the startup path is a client of it, not a parallel implementation.** `sidecar session status`, `restore` and `policy` ship with JSON/human parity and gendoc coverage; `status` and `restore --dry-run` are read-only and work with no TUI, which is what makes a restore reviewable before it happens. The local binding of the executor lives in `sessionrestore.LocalDeps` so the CLI restore and the automatic restore are one restore rather than two that agree today. The one asymmetry is `--yes`: under `ask` there is no TUI to confirm through, and rather than silently downgrading to shells-only or silently resuming, the command refuses with exit 5 and names the flag (`TestSessionRestoreRefusesAnUnconfirmedResume`, and its structured-JSON counterpart).

**Exit gate, live, on the purpose-built harness.** `scripts/session-restore-reboot.sh` isolates HOME, all four XDG roots, `TMUX_TMPDIR`, `-config` and `SIDECAR_ISOLATED_STATE=1`, and re-derives and re-checks the socket path before every kill, so there is no code path in it that can name the default server. `paths` confirmed every path resolved under the harness root. Two managed shells were created and recorded at v3 with `restore.eligible` and `lastSeenServer: pid=92640`. Only the harness server was terminated; the developer's real default server and its live sessions were confirmed untouched immediately afterwards. `session status` then reported `serverChanged: true` with `priorServers: [pid=92640]` and both shells as `recreate-shell` — and, the point of the milestone, **both records were still there to report**. A relaunched Sidecar restored both under their own names into their own working directories through the automatic post-first-frame path, in a new server (pid 99342). A second `session restore` reported `reattached 2` and created nothing.

**The review found the reap fix over-corrected, and the shape of the miss is the lesson.** Preserving instead of tombstoning was implemented as `currentServer == ""` meaning "no server is running" — but two of the three production paths passed `tmuxserver.Socket()`, which is `Present` with pid **0**, because a socket stat identifies a file rather than a server process. Every call on the global Sessions surface and the headless `host serve` loop therefore computed "the server died", and those surfaces never tombstoned anything: a shell the user deliberately closed was preserved, marked eligible, and recreated after the next tmux restart. That is the immortal-restore-candidate failure mode, and it contradicted the rule this plan and AGENTS.md both state.

The tests did not catch it because they called `shellliveness.ReapShell` directly with a pid-qualified incarnation built inside the test. They proved the plumbing carried a server id and never that any surface supplies one, and their pane fixtures carried no `ServerPID` for a correct implementation to pick up. A test that constructs the input the production caller gets wrong is a test that can only pass.

The fix makes the unsafe state unrepresentable rather than adding a guard. `shellstate.ServerState` has three answers — running with an id, positively gone, unknown — and `ServerRunning("")` degrades to unknown rather than being believed. Tombstoning requires positive evidence that the server is alive and the shell is gone from it; preserving requires positive evidence the server died or was replaced; anything else **defers**, keeping the record without marking it restorable. Deferring also settles the upgrading-user case: a pre-M4 record never confirmed alive in any server has no evidence tying it to the server running now, so it is kept unmarked rather than tombstoned on an assumption, and the next positive observation stamps it.

Two consequences worth recording. First, on the global Sessions surface the writer's preserve branch turns out to be **unreachable by construction**: that surface only probes a shell it has seen alive in the current server, and seeing it alive is what re-stamps its marker to that server, so a probe implies the marker names the running server. A server replacement is caught one level earlier, by the tracker's liveness reset, and the record survives because nothing touches it. The preserve branch earns its place on the project workspace plugin, which reaps per shell off a capture failing with no listing to reset against. Second, a restored shell's marker was never re-stamped, so closing it later looked like another server death; the executor now records liveness for every step that leaves a session running, which is first-hand evidence rather than an inference.

**A bug the live run found that no unit test had.** The executor's server-replacement guard compared against the server observed before any work. A cold restore normally begins with no tmux server at all — that is the situation it exists for — and creating the first shell is what starts one, so the `"" -> "pid=N"` transition read as a replacement and the run aborted after its first shell. The run's server is now *adopted* on first sight; a different pid, or the server disappearing again, is still an abort because the sessions already created went with it. `TestExecuteAdoptsTheServerAColdRestoreStarts` and `TestExecuteStopsWhenTheServerDisappearsMidRun` pin both directions. The same live sequence incidentally demonstrated retry convergence for real: the partial run's shell was reattached rather than duplicated on the next attempt.

**Honest limits.**

*Pane-tree reattachment is not covered by this milestone, and the earlier draft of this paragraph cited the wrong evidence for it.* It named `panecodec`, which has no session awareness at all — it imports no tmux and copies the session string verbatim with no existence check — so it cannot exercise cold-restart reattachment in either direction. The actual coverage is `internal/overview`'s `TestSessionsRestoreDegradationTable`, two shell cases with `EnsureSession` stubbed, and there is no project-surface equivalent. More substantively, the claim that "existing pane persistence reattaches them by session name" does not describe what happens to a restored managed shell at all: pane-layout shell leaves are `sidecar-tp-*` terminal splits, a **disjoint namespace** from M4's `sidecar-sh-*` managed shells. The primary leaf binds to the selected shell, and nothing re-binds after `SessionRestoredMsg`, which only posts a notification. So the gate's "exact tree/focus/ratios" clause is not proven, and the honest statement is stronger than "not re-proven": restoring a managed shell does not currently re-bind any pane to it, and closing that gap is follow-on work rather than something this milestone quietly achieved.

*The confirmed-resume half* is covered by the executor's tests and by `StartResume`'s contract rather than by a live provider resuming a real conversation after a reboot. The live provider matrix belongs to M5's rollout, and a paid resume is exactly the kind of proof that needs explicit operator opt-in. The reboot harness seeds a bound fake conversation so the refusal, the plan, and the redaction are exercised end to end against a real binding; what it does not do is spend money proving the provider then answers.

*`--shell TARGET` on `restore`* matches a tmux session name or display name directly rather than going through the full `shellTargetLookup` resolver, which is narrower than `agent`'s target resolution and worth widening if a second surface needs it.

*The server pid is not unique over all time.* An earlier version of this section claimed it "is new after a restart", which is the one sentence in it that is simply untrue — pids recur. The narrow window is a different tmux server, already running, that has coincidentally taken the dead server's pid. Both consumers fail in the same direction there: the reap path tombstones instead of preserving, and the planner reports `died_in_this_server` instead of `recreate-shell`. Both are refusals to act, never a duplicate shell, a stolen name, or a wrong-cwd resume, because every mutation is gated on `has-session`, name, and cwd rechecks that never consult the pid. That is why the pid needs no `start=` component here while M3's generation fence did: a false positive there accepted a stale report and rewrote a live binding, an actively wrong mutation, whereas the worst case here is a shell the user has to recreate by hand.

### M5 — Remote adapter and rollout

- The remote-host plan's mutation seam exists (Phase C: one-shot CLI verbs over `hosts.RunSidecar`). Add the remote terminal/session adapters over it; keep exact session values host-local by default.
- Run the local/remote parity suite over start/get/prompt/wait/read/key behavior and restore-plan reporting.
- Keep agent control behind a default-off feature flag through M2; enable by default after the live provider matrix passes. Keep `resumeAgents=ask` even after rollout; `auto` remains explicit.
- Add release notes and a demo recipe. Move this plan to implemented only after local agent control, exact session binding, and cold restore all ship; remote support may remain a linked follow-on.

**M5 remote adapter implemented 2026-08-31 (`td-a55114`). The rollout half did not pass its own gate and the flag stays off; see the matrix below.**

**The adapter is at the verb level, and that is a deviation from this plan's own diagram.** The "One application core" sketch puts the remote adapter under `Terminal`, beside local tmux. It is instead a sibling of the service: `internal/agentremote` runs each verb as a one-shot `sidecar <verb> --json` invocation through `hosts.RunSidecar`. The later and more specific [Interaction with remote hosts](#interaction-with-remote-hosts) section already describes it this way, and the two disagree, so the choice is recorded rather than left to be inferred. A `Terminal`-level adapter would tunnel `Inspect`/`Capture`/`Submit` across the link and re-run the occupant pin, the readiness contract and the refusal rules **on the viewer** — over a connection that can stall between the pin and the write, which is precisely the window those rules exist to close. Running the verb on the host puts every rule on the machine that owns the pane and leaves exactly one implementation of them.

**Parity is a property of the transport rather than of a translation table.** `sidecar agent` writes the frozen `agentcontrol.ErrorEnvelope` to stderr for every refusal, and `hosts.RunError` carries stderr, so a remote refusal is returned as the host's own `*agentcontrol.Error` with its code, sentence and target intact — a remote `agent_blocked` is `agent_blocked`, at the same exit status, without anything mapping it. A code from a newer host that this build has never heard of is passed through unchanged, because inventing a local approximation would discard the only accurate description of what the host refused. Only when there is no envelope to read does the transport's classification decide.

Two error codes were added, which extends a vocabulary M0 froze; the extension is deliberate and reasoned in `types.go`. A remote target has two failure modes the local vocabulary cannot express without lying about them: `host_unavailable` (exit 1) is a machine that was never reached, where nothing was attempted and retrying later is the fix, and reporting `transport_failed` for it sends a reader hunting a fault that is not there; `version_skew` (exit 2) is a host whose Sidecar does not know the verb, where the fix is to update one of two binaries and no other code says so. Exit 2 is what `agentExitCodes` has always documented as "usage error or version skew", and it is what the host itself exited with, so a relayed verb keeps the status it would have had locally. Capability negotiation therefore falls out of the exit-code contract with no handshake.

**Session references stay host-local by construction rather than by rule.** `agentremote` has no store, no manifest and no `shellstate`/`agentsession`/`sessionrestore` import, so a remote conversation identifier has nowhere local to land; the host redacts by default, and `--include-session-ref` is added to the argv only when the caller asks for it. `TestNothingHereCanWriteALocalRecord` is a source-level import guard, deliberately, for the same reason `TestTheThreeCallSitesGoThroughOneResolver` is: the property fails no behavioural test today because there is no writer to observe, and would only begin being wrong the moment somebody added one. (Its first version grepped the source text and failed on the package doc comment explaining that `shellstate` is what the package must not reach — a guard that fails on the sentence describing the property it protects is a guard nobody keeps, so it parses imports now.)

**A wait's deadline is the caller's, not the transport's.** `hosts.RunSidecar` replaces an absent deadline with its own 30s default, so a bounded invocation carrying `agent wait --timeout 2m` would have been severed at thirty seconds and reported as a transport timeout for a wait that was proceeding correctly. The invocation gets the caller's timeout plus slack, so the host's own timeout expires first and the caller receives the agent-level `timeout` refusal it asked for. No resident subscription channel was built, as the plan provides.

**A remote verb requires an explicit target.** The omitted-target rule resolves `SIDECAR_SHELL`, which names a shell on the viewer's machine. Two Sidecars running a project of the same name generate the same tmux session names by construction, so a silent fallback would not merely miss — it could address a real and entirely wrong pane on the other machine. `--host` without a target is refused.

**`hosts.OneShotClient` was needed because the health gate protects a case the CLI is not in.** `RunSidecar` refuses when nothing has dialled, so a TUI never waits out an ssh connect timeout inside whatever asked for it. A one-shot CLI process has no stream because nothing had the chance to start one, the user named the host explicitly on the command line, and waiting out a connect timeout is the correct behaviour rather than the hang. The one-shot client marks itself reachable and lets ssh decide.

**A defect the built binary found that no test in the suite would have.** The `--host` gate first asked `features.IsEnabled`, a process-wide singleton that `cmd/sidecar/main.go` initialises *after* `cli.Run` — so in any real invocation it was nil and fell through to the compiled-in default of false. Neither `features.flags.sidecar_remote_hosts` in the config file nor `-enable-feature` on the command line could turn it on, and every `--host` verb refused as though the feature were off. The gate now resolves `Env.FeatureOverrides` then the loaded config, the way `requireAgentControl` already did. The second half is the part worth recording: `hosts.FromConfig` consults the same singleton and returns nothing when it reads the flag as off, so fixing only the gate would have left an `-enable-feature` run seeing zero hosts and reporting "no host is registered as X" for a host that is registered. It was found by running the command in an isolated sandbox, and independently by the loopback suite; the tests written from the implementation had all constructed the enabled state directly, which is the shape of test that can only pass.

**The parity suite runs over a loopback harness, and the boundary is stated rather than implied.** There is no second machine and no sshd anywhere in this repository — the remote-hosts plan's own tests use a fake `ssh` on `PATH` and an injected `Invoker`, and the only real-ssh path is `internal/tty`'s `TestRemoteControlSpike`, skipped unless `SIDECAR_SPIKE_HOST` names a machine. So the harness builds the strongest honest substitute: a script named `ssh` ahead of the real one, which ignores every `-o` option and the target, takes the last argument — the remote word `internal/hosts` rendered, `$SHELL -l -c '<quoted command>'` — and runs it locally by substituting a concrete shell and letting `/bin/sh` re-parse the quoting.

What that genuinely exercises: `hosts.Transport`'s argv rendering and its allow-list shell quoting, unwound by a real shell rather than compared as a string; a real process spawn of a real `sidecar` binary built from the worktree, against a state tree and a tmux server that are not the viewer's; real exit codes through `hosts.classifyRun`; the banner-tolerant stdout decode and the stderr envelope recovery, because the fake `ssh` writes a login banner to **both** pipes before every invocation; and the CLI's own dispatch and exit codes. What it does **not** exercise, and what no claim here should be read as covering: sshd, authentication, ControlMaster multiplexing, network latency, partial writes, and a connection that drops mid-verb. A real second machine remains the outstanding proof, and the plan's "Remote proof, when available" line stays open.

The harness paid for itself immediately by finding two defects that the unit tests written from the implementation could not, both of which are the same shape — a test that constructs the state the production caller gets wrong is a test that can only pass. *The relay returned a fragment as the document.* `session status --json` is indented, and `hosts.decodeRemoteResult` scans stdout for lines that begin an object and tries them last-first — correct for a result printed after banner noise, and exactly wrong for a pretty-printed document whose every nested object begins its own line. The last such line is the final element of `steps`; it decoded perfectly well, so `session status --host` returned one step of the plan wearing the whole document's place. The relay now decodes into a named `SessionDocument` whose `ValidRemoteResult` requires a top-level `resumePolicy`, a key both document shapes carry without `omitempty` and no step or outcome has at all. *And the feature gate was unreachable from a shipped binary.* It asked `features.IsEnabled`, a process-wide singleton `cmd/sidecar` initialises **after** `cli.Run`, so in any real invocation it was nil and fell through to the compiled-in `false`: neither `features.flags.sidecar_remote_hosts` in the config nor `-enable-feature` on the command line could turn `--host` on. The gate now resolves `Env.FeatureOverrides` then the loaded config, the way `requireAgentControl` already did — and `hosts.FromConfig` consults the same singleton, so fixing only the gate would have left an `-enable-feature` run seeing zero hosts and reporting "no host is registered as X" for a host that is.

**Rollout: the live provider matrix did not pass, and `agent_control` stays default-off.** `TestLiveProviderMatrix` is the gate's non-mutating half — launch, identify, `get`, status, and every passive read source, with no prompt ever submitted, on the package's private tmux server. Run 2026-08-31 against every catalog provider installed on the development machine:

| Provider | Version | start | get / status | read | Verdict |
| --- | --- | --- | --- | --- | --- |
| codex | 0.151.0 | ready | idle, `codex.known-live-fallback` | all sources | pass |
| claude | 2.1.220 | ready | idle, `claude.screen.idle` | all sources | pass |
| opencode | 1.18.25 | ready | idle, `opencode.known-live-fallback` | all sources | pass |
| cursor | 2026.08.25 | ready | idle, `cursor.known-live-fallback` | all sources | pass |
| grok | 1.0.13 | ready | **unknown, indefinitely** | all sources | **fail** (`td-bf451a`) |
| pi | 0.84.3 | **never ready** | — | — | **fail** (`td-c05b06`) |

Both failures were diagnosed to a root cause rather than logged as flakes. *grok*: from about a second after start, every `agent get` returns `unknown` / `live-process-changed` and never recovers — probed for 25 seconds, with the pane title constant at `grok` and `pane_current_command` constant at `grok-1.0.13-mac` throughout, so it is neither a re-exec nor a title change. The cause is that `grok.overlay.retain` in `internal/agentactivity/grok.go` matches `(?im)(esc to close|resume session)` with `Skip: true`, and grok's **own startup splash** renders a keybinding menu line `Resume session … f3` inside its welcome box. The rule reads that as an open overlay and skips, retaining prior state; `Service.Get` is a one-shot with a fresh tracker, so there is no prior state to retain and it stays unknown. *pi*: `agent start --kind pi` times out every time although pi is running correctly. **This record's first diagnosis was wrong and is corrected here by the independent review**, because a wrong root cause in an evidence section costs the next engineer more than no root cause at all. The original claim was that pi execs as `node` and `agentactivity.Identify`'s shared-runtime branch has no pi rule, so pi is never identified. That branch is unreachable for pi: pi rewrites its own process title, so `argv[0]` is `pi` even though the exec path is node, `ResolveForegroundProcess` reports `ProcessIdentity: "pi"`, and `Identify` returns `"pi"` from its first check (`activity.go:81-83`) without ever reaching the `node` branch. Identification succeeds. The actual cause is one line further on: `DetectPi` (`internal/agentactivity/pi.go:10`) guards with `ob.Agent != "pi" || ob.CurrentCommand != "pi"`, and `CurrentCommand` is `node`, so pi's own detector refuses its own correctly-identified pane and the status never leaves `unknown`, so `Service.Start` never sees `idle` and runs to its timeout. `grokProcess` (`grok.go:159`) already accepts `node` and `bun`; pi's guard is the outlier, and relaxing it the same way is sufficient — the pane then falls through `piRules` to `pi.known-live-fallback` and reports ready. Neither was fixed here: grok's rule cannot be retuned safely without a captured fixture of its genuine resume overlay, and pi's fix is left with its corrected ticket rather than made in passing during a review. Both are filed with the evidence.

The plan's condition is that the flag is enabled by default "after the live provider matrix passes". Four of six pass; two fail with named, newly diagnosed causes. Flipping now would put `agent get`/`agent wait` in a permanently-unknown state in front of every grok user, and `agent start --kind pi` as a sixty-second timeout, on by default. So the flag stays off, and the honest sequence is: fix `td-bf451a` and `td-c05b06`, re-run `TestLiveProviderMatrix`, then flip. Flipping is a one-line change plus the two-surface gating discipline `pane_move` used; the gate, not the mechanics, is what is outstanding.

**Inherited kind-binding residuals closed alongside M5 (`td-6625e2`).** M4's fast follow stopped new records being written with a mis-attributed provider; it did nothing for the records the shipped bug had already poisoned, and readers preferred `Agent.Kind`, so an affected user's cold restore would still have offered `claude --resume` on a grok conversation. The remediation is a refusal on **both** doors rather than a reader that picks a field: the pure planner grows `ReasonKindDisagreement`, checked ahead of the reference, deduplication, the catalog and policy — all of which would otherwise answer confidently about a record that has no single answer — and such a record is kept out of `dedupWinners` so it cannot take a conversation key from a healthy shell and leave both unrestored; and `shellstate.SessionRefAtPath`, the one supported way out of `shells.json`, returns `ErrKindDisagreement` with a zero `Ref`, so the value never escapes even to a caller that ignores the error. Both were mutation-proven: deleting the planner check fails three named tests, and reverting the read to the old pick-a-field logic hands back `grok-session-1` labelled `claude`, which is the shipped bug verbatim. A healed-record control case rules out the refusal passing for an incidental reason.

Two deliberate omissions are worth recording because both are refusals to guess. There is **no migration** that rewrites poisoned records: nothing can tell which of the two fields is the mistake, and a migration that guessed would produce the same wrong resume with no refusal to warn anyone, so the plan names both providers and leaves the decision with the user. And startup adoption still records **no** kind: adoption sees a pre-existing tmux session with no evidence of which provider is inside it, and writing a guess into the field the gate treats as evidence is the opposite of the fix. `agent start` does now record the kind it started, through `shellstate.RecordAgentKindAtPath` — the only writer — which also drops a stale session binding when the kind changes, because that reference is the previous provider's conversation and keeping it is exactly how a restore offers one agent's conversation to another. The comment that claimed a grok shell refuses a claude report "on every platform" now says what is true: the refusal holds wherever a kind was recorded, and names the two cases that record none.

**A characteristic the matrix surfaced that is not a defect.** Readiness can precede first paint: `agent start` returns when the provider is positively identified, and for grok that is its terminal title while for opencode and cursor it is a live process, all of which are true before an alternate-screen UI has drawn anything. A read issued in that window returns an empty or partial screen, and the four sources sample slightly different instants of a screen being drawn, which is why they initially disagreed. The alternative explanation was ruled out rather than assumed: on a settled alternate-screen pane, `capture-pane -p`, `-S -40` and `-S -40 -J` return byte-identical content for both opencode and grok, so there is no capture defect. The documented workflow — start, prompt, wait, read — is unaffected because a wait always outlasts the first paint. The matrix lets providers settle before reading and says why.

## Verification and acceptance evidence

### Contract and unit coverage

- Provider table tests: every catalog family has one launch builder; native-resume providers have one structured resume builder and allowed session-ref kind; aliases and display labels cannot drift.
- Target tables: current shell, explicit tmux name, unique/ambiguous display name, wrong namespace, same name on two hosts, replaced pane, dead session, missing project.
- Start tables: initial shell, shell still initializing, editor/command/agent busy, expected provider ready, wrong provider, blocked during startup, exit, timeout, replacement during wait.
- Prompt/wait tables: each starting lifecycle state, blocked preflight, stalled transition, already-working caveat, multiple `--until` states, cancellation, timeout, replacement, stale capture.
- Input tables: bracketed paste on/off, multiline/Unicode/metacharacters, every logical key, reject-one-reject-all validation, no partial send.
- Read tables: source selection, ANSI stripping/preservation, unwrapping, line bounds, alt-screen passive limit, exact transcript, missing/disabled adapter, mismatched session binding.
- Manifest tables: v2→v3 migration, newer-version write refusal, unknown fields, official/unofficial reports, ID/path validation, stale generation, duplicate refs, tombstones retaining agent/restore fields.
- Restore-plan tables: same live server (reattach only), absent/replaced server, prior-live eligibility, intentional tombstone, missing cwd, name collision, unavailable binary, policy matrix, duplicate session, partial failure, retry at every step.

### Real consumer proof

- `go test` focused packages during each milestone; full `go test ./...` and the repository's lint/format gates on the integrated candidate.
- `./scripts/tmux-drive.sh paths` before every real-app proof, followed by start/keys/snap/stop; both tmux server and Sidecar state paths must be private.
- A purpose-built reboot harness may kill only its named isolated tmux server and preserves its temp state directory between Sidecar launches. It must prove the server socket/incarnation changed before claiming cold restore.
- Live provider matrix: Codex, Claude Code, Cursor Agent CLI, Grok, and at least one hook-authoritative provider where installed. Start/get/status/read are non-mutating after launch; prompt/resume proofs require explicit operator opt-in because they can create paid or externally mutating work.
- Startup trace with multiple restore candidates proves first frame is not delayed.
- Remote proof, when available, runs the same JSON contract as one-shot `sidecar agent` invocations over `hosts.RunSidecar` against a real second machine and confirms a blocked prompt can be inspected and deliberately answered.

### Safety invariants

- Never stop, restart, kill, or replace the default tmux server. Tests clean up only named isolated servers/sessions they created.
- Never infer an exact agent session from "newest file in cwd" for automatic resume.
- Never resume into a missing/fallback cwd.
- Never interpolate session IDs, paths, prompts, or provider args into a persisted/replayed shell command.
- Never send prompt bytes to a blocked, stale, ambiguous, or replaced target.
- Never let a restore failure delete a shell record, tombstone, pane layout, or conversation record.
- Never describe cold reconstruction as process preservation, or a Sidecar executable update as a tmux server handoff.

## Deferred work and revisit triggers

- **Persisted terminal screen bodies:** defer. Revisit only if transcript coverage and shell-only reconstruction leave a demonstrated recovery gap worth the secret-bearing storage and retention policy.
- **Sidecar-owned orchestration workflows:** defer indefinitely. A real demand should first prove that CLI primitives plus `td`/harness orchestration are insufficient; any future workflow engine is a separate product plan over this core.
- **Raw shell pane wrappers:** do not build while tmux remains the owner and its CLI is available. Revisit only if remote transport makes raw tmux unreachable to agents and a typed host protocol becomes the actual owning boundary.
- **Live tmux handoff:** no planned work. Revisit only on a supported tmux export/handoff interface or an explicit decision to own PTYs.
- **Automatic provider integration updates:** start with explicit status/install. Revisit background updates only after version drift causes observed restore failures and the security/update policy is settled.

## Open questions to settle in M0

1. **Targeted observer implementation.** Can the existing control-mode manager be extracted without inheriting TUI geometry ownership, or is a smaller read-only control client warranted? Decide from the latency/spawn measurements, not package aesthetics.
2. **Exact transcript output shape.** Reuse the adapter `Message` representation directly or define a smaller stable agent-control projection. Prefer the smaller projection unless an existing public JSON contract already exists by M3.
3. **Prior-live marker source.** Confirm whether the existing tmux-server incarnation tracker plus shell liveness transitions can record eligibility without an extra process spawn or write amplification. If not, add the smallest manifest transition needed; do not introduce a second runtime ledger.

   **Settled in M4 (2026-08-30): yes, they can, and no second ledger was needed.**

   *No extra spawn.* Both surfaces already collect the evidence. The global browser derives eligibility from the `tmux list-panes -a` its refresh cycle already runs, and the project plugin from `noteShellAlive`, which every capture already reaches. The one thing missing was the server's identity in a form safe to persist, and that was obtained by appending `#{pid}` to the pane-listing format string the refresh was already executing — the server pid now arrives on a listing Sidecar was taking anyway, at zero additional cost. The project plugin, which has no such listing, resolves and caches the pid once per socket identity, so its cost is one `tmux display-message` per tmux-server lifetime rather than per shell or per cycle.

   *No write amplification.* Eligibility is written only on a transition. A record already marked eligible under the same server id is left completely untouched — not even `lastSeenAliveAt` is refreshed — so the steady state is one write per shell per tmux-server lifetime. That is why `lastSeenAliveAt` means "when this shell was first confirmed alive in this server" rather than "when it was last polled", and the naming follows the meaning. The workspace plugin adds an in-memory set on top so the common path performs no syscall at all, not merely no write. Both properties are pinned by tests that compare the file's bytes across repeated observation rather than trusting the writer's own report.

   *No second ledger, and no new manifest transition.* The v3 `RestoreState` M3 shipped dormant was sufficient exactly as specified; M4 populated it and added no field. The marker identity is the server pid via a single producer, `tmuxserver.Incarnation.ServerID()`, which deliberately returns the empty string for an unknown or absent server instead of `pid=0`: the reap path's decision turns on being able to distinguish "a different server" from "no observation", and those two lead to opposite outcomes — preserve versus tombstone.

   *An empty string is not a third state, and treating it as one was the review's blocking finding.* `ServerID()` returning empty means "I could not identify the server", which is not the same fact as "no server is running", and the first implementation of the reap writer conflated them. The evidence now travels as a `shellstate.ServerState` with three explicit answers, so a caller holding a socket stat can no longer assert a death by omission. The pid itself is not unique over all time — pids recur — and that narrow collision window is recorded under Honest limits along with why it fails safely here but did not for M3's generation fence.

   One addition beyond what the question asked, because the question's framing assumed liveness observation was the only source: `CreateManagedShell` stamps the marker at creation. Creation is stronger evidence than any subsequent observation — the session was just made, in this server, by this process — it costs nothing because the record is being written in that same operation, and without it a shell created shortly before a crash would come back as "never confirmed live".

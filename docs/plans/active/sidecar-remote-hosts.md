# Sidecar as its own remote host runtime

Status: **active, Phase C complete — remote creation, mutation, and rename behind a flag**, 2026-08-30

Related: [Herdr as Sidecar's remote host runtime](../deprecated/herdr-remote-hosts.md) was the competing alternative for the same deliverable; it is deprecated — this plan won on its Phase 0 numbers, and [Relationship to the Herdr plan](#relationship-to-the-herdr-plan) records what was compared. [Remote host content-pane parity](../implemented/remote-host-content-pane-parity.md) is the follow-on for resolving and loading clicked files, td items, diffs, and provider resources on the machine that owns a remote Sessions row. [The viewer owns the screen](../implemented/remote-host-viewer-screen.md) is the follow-on for `sidecar open` / `layout` issued in a host pane, and for mixed `n` routing: the lease holder is the screen, the host still owns shells, worktrees, and tmux splits, and serve announces those requests without executing layout. [Hosting Herdr plugins in Sidecar](../deprecated/herdr-plugin-support.md) is deprecated, superseded by [the plugin ecosystem plan](../implemented/plugin-ecosystem/README.md), and orthogonal.
Evidence: all claims verified against the Sidecar codebase on `main` (citations inline); the Herdr comparisons reference the source inspection at `c2637dc1` recorded in the Herdr plan. Phase 0 measurements, transcripts, and findings are in [docs/evidence/sidecar-remote-hosts-phase0.md](../../evidence/sidecar-remote-hosts-phase0.md). Phase B's final-candidate tests and isolated two-machine proof are in [docs/evidence/sidecar-remote-hosts-phase-b.md](../../evidence/sidecar-remote-hosts-phase-b.md). Phase C's isolated two-machine proof is in [docs/evidence/sidecar-remote-hosts-phase-c.md](../../evidence/sidecar-remote-hosts-phase-c.md).

## Decision first

Sidecar becomes its own remote host agent. To see what is running on the Mac mini, the user installs Sidecar there — nothing else — and registers `remote:mac-mini` in the local Sidecar. The remote machine's shells, worktrees, agent states, and live panes appear in the Sessions browser, served by two channels over SSH:

1. **Pane content: proxied tmux control mode.** The local Sidecar attaches to the remote tmux server by spawning `ssh host tmux -C attach-session -f ignore-size -t <session>` instead of a local `tmux -C`. This is not a new protocol — it is tmux's own control protocol, which Sidecar's entire terminal stack already consumes, arriving over a different pipe.
2. **Sidecar-level truth: `sidecar host serve`.** A headless, ephemeral process spawned on the remote host over SSH stdio, running the existing inventory/liveness/agent-status stack and streaming snapshots and status transitions as versioned JSONL. Read-only through Phase C; it now has exactly one write, the reap — see [Remote shell improvements](remote-shells-improvements.md).

The "new streaming transport" is therefore mostly channel 2, and it is small: the pane-bytes problem — the hard, latency-sensitive, high-bandwidth part — is solved by reusing a protocol both ends already speak.

## Why the codebase is already most of the way there

Three findings make this plan an integration exercise rather than a build:

**The control-mode consumer is transport-agnostic behind one seam.** `newProcessControlChannelCommand(session, cmd *exec.Cmd)` (`internal/tty/control_transport.go:70-116`) wires any `exec.Cmd`'s pipes into the line parser; nothing below it knows the command is local. Production hardcodes the local spawn via `controlChannelFactory` (`control_transport.go:31`, `control_manager.go:146-150`). Swapping the factory's `exec.Cmd` for an ssh invocation carries the entire downstream stack unchanged: the single ordered-actor delivery (`control_manager.go:606-630`), seed transactions and race detection (`control_model.go:261-277, 529-540`), the byte-fed `screenmodel` with 30 fps publication (`control_model.go:194`), `%pause`/`%continue` reseeds, the capture path with its 12 ms coalesce, and the polling fallback that engages on control failure.

**The geometry problem was already solved for exactly this case.** `geometry_lease.go`'s own header describes the scenario: "Two sidecar instances on two machines can be attached to one tmux server. Each asserts its own pane geometry unconditionally, the last resize-window wins … a continuous ping-pong" (td-ee222a, `internal/tty/geometry_lease.go:16-21`). The lease store is a tmux session option — the server is the shared medium, so remote and local claimants coordinate through the same store with no extra machinery; tokens carry durations, not timestamps, precisely so they survive clock skew across machines (`geometry_lease.go:42-44`); the decision function is state-free "so a headless caller can adopt it unchanged" (`geometry_lease.go:160-191`). And `pane_fit.go` is already the non-owner's rendering path — clip with cursor-anchored offsets when the pane is bigger, letterbox when smaller, never stretch, with the "200x50, showing 120x40" indicator (`pane_fit.go:5-14, 169-177`). A read-only remote viewer is a lease non-owner by definition and reuses this wholesale.

**The awareness stack is UI-free by construction.** `workspaceinventory`, `shellliveness`, `shellstate`, `tmuxserver`, `agentactivity`, `agentstatus`, `activitystore`, `workspaceops`, and every `adapter` have zero Bubble Tea references (verified by grep); collectors take injectable runners, captures, and clocks (`workspaceinventory/inventory.go:226-239`); `workspaceops` already exposes shell and worktree creation as plain functions exercised headlessly by `sidecar create` today. A headless server is orchestration over existing libraries — the one piece of UI choreography to re-implement headlessly is the reap sequence in `internal/overview/shell_liveness.go` (~150 lines), and Phase A does not reap at all.

## Relationship to the Herdr plan

Both plans deliver the same product surface: a **Host** in the Sessions browser. They differ in the on-host agent, and each is honest about what that buys:

| Axis | Herdr on the host | Sidecar on the host |
| --- | --- | --- |
| On-host dependency | Herdr installed *and its server running*; no CLI verb to start it detached (upstream request filed) | Sidecar installed. No daemon at all — `host serve` is spawned per connection over SSH stdio and dies with it |
| Pane streaming | Herdr's frame protocol: absolutely-positioned cell blits, full frame on resize, no offset/range history | tmux control mode: `%output` deltas, absolute history addressing via `capture-pane -S/-E`, so lazy history, frozen selection, and search work remotely unchanged (`internal/tty/history.go`, `capture_range.go:49-81`) |
| Agent status | Herdr's detection manifests, computed by its server | Sidecar's own `agentactivity`/`agentstatus` stack — identical semantics to local, same lanes, same done-TTL, same provider detectors |
| Transcripts | Not available (`agent.read` is screen text; stores unreadable remotely) | The adapter stack runs on the host and can serve session lists/messages — a capability the Herdr path can never offer |
| Creation fidelity | `layout.apply` workaround for the argv gap; Phase C risk | `workspaceops.CreateManagedShell`/`LaunchWorktreeSession`, the same code paths as local, already headless |
| Protocol risk | Herdr's protocol integer moves fast (19→20→21); string-matched close reasons; silent-EOF crash semantics | Both ends are this repo; protocol versioned here, errors structured here |
| What it costs | Depends on a third-party product's pace and priorities | Sidecar must build the host protocol, a headless entry point, and maintain its own remote transport forever |
| Terminal runtime on the host | Herdr **is** the terminal runtime — it sees panes it owns | tmux — Sidecar sees any tmux session on the default server, including shells no Sidecar created |

Decision posture: **resolved — this plan won.** Its Phase 0 spike ran first, proxied control mode proved local-grade on a real link, and the Herdr plan was deprecated with that evidence cited, exactly as this paragraph originally provided for. The table above stays as the record of what was weighed. Herdr's remaining relevance is as the feature benchmark: where it names a capability Sidecar lacks, the bar is parity or better on Sidecar's own runtime — tracked in [the agent-control plan](herdr-agent-control-and-session-restore.md), not here. The shared transport pieces (the ssh ControlMaster recipe: `-S <dir>/ctl -o ControlMaster=auto -o ControlPersist=yes -T`, generated `-F` config with `ServerAliveInterval 15`/`CountMax 4`, `ssh -O exit` teardown) were adopted from the Herdr plan's research and shipped in `internal/hosts`.

## Scope boundary

**In scope**

- A host registry and SSH transport (shared shape with the Herdr plan).
- `sidecar host serve`: headless, stdio, versioned JSONL; read-only through Phase C, with the reap as its one write afterwards.
- Read-only remote observation: inventory, agent status with full local fidelity, live pane view via proxied control mode.
- Interactive remote panes (Phase B): in-band input, cross-host geometry lease rules.
- Remote creation (Phase C): shells and worktrees through the existing `workspaceops` pipeline.
- Remote conversations via the adapter stack (Phase C, gated on demand).

**Explicitly out of scope**

- Any change to local behavior. The transport injection must leave the local path byte-identical.
- A persistent daemon on the remote host. Serve processes are per-connection and ephemeral; if that ever changes, it changes in its own plan.
- Exposing anything to a network. SSH stdio is the only transport; serve never binds a socket.
- Remote git/diff/file browsing beyond what inventory already summarizes. Click-to-content parity in the existing Sessions pane deck is [Remote host content-pane parity](../implemented/remote-host-content-pane-parity.md). Agent `open` / `layout` from a host pane, and mixed `n` routing, are [The viewer owns the screen](../implemented/remote-host-viewer-screen.md). Entering a remote project from `@` / `W` is [Remote destinations in `@` and `W`](remote-project-switcher.md) (slices 0–2 bind Workspaces; Files/Git remoting is that plan’s slice 4).
- A Sidecar plugin system. Now planned in [the plugin ecosystem plan](../implemented/plugin-ecosystem/README.md); it is not prerequisite to this work.

## Architecture

```text
Sidecar (viewer)
├── internal/hosts                 NEW — registry, health, SSH transport (ControlMaster; shared with the Herdr plan)
├── internal/tty                   control channel factory becomes host-aware; alternate terminalInputSender
├── internal/hostproto             NEW — the serve protocol: types, version, encode/decode (shared by both ends)
├── internal/workspaceinventory    gains HostID; TmuxName stops leaking upward (owed regardless of this plan)
├── internal/overview              Sessions browser host grouping; remote rows fed by the serve stream
└── internal/paneframe/panelayout  unchanged — a remote pane is an ordinary leaf

Sidecar (host, same binary)
└── internal/hostserve             NEW — `sidecar host serve`: collector loop, liveness tracker,
                                   activity trackers, status resolution, JSONL stream over stdio
```

### Channel 1 — pane content

- **Attach:** the host-aware `controlChannelFactory` builds `ssh <target> tmux -C attach-session -f ignore-size -t <session>` through the ControlMaster transport. Everything downstream is untouched.
- **Input (Phase B):** a second `terminalInputSender` implementation (`internal/tty/terminal_surface.go:45-166` is the interface) that routes `send-keys`/paste through the control channel's own stdin (`Send`/`SendPair`, `control_transport.go:14-29`) instead of spawning subprocesses — one write on an open pipe rather than one ssh exec per keystroke, preserving the FIFO ordering the send queue exists for (td-8fcd2e, `send_queue.go:48-101`).
- **History, metadata, captures:** the in-band command path already exists — the capture path issues `display-message` + `capture-pane` pairs through the control channel today (`control_manager.go:1115-1129`). `CapturePaneRange`'s absolute `-S/-E` windows move in-band the same way, keeping `HistoryReach`, `WindowFreeze`, and search working remotely with their existing contracts.
- **Resize (Phase B):** `assertDimensions` currently restarts the control transport on every geometry change (`tty.go:1376-1436`). Locally that is cheap; over SSH it is a subprocess respawn through the master connection (~fast, but measured, not assumed — Phase 0 item 3). If the number is bad, the work item is teaching the transport to reseed without a process restart, which is a local-path improvement too.
- **Out-of-band one-shots** (`ResizeTmuxPane`, `QueryPaneSize`, lease option reads/writes) run as `ssh <target> tmux …` through the master connection, or move in-band where ordering matters.

### Channel 2 — the serve protocol

`ssh <target> sidecar host serve --stdio`, spawned by the viewer per connection. Newline-delimited JSON, both directions, defined once in `internal/hostproto`:

- **Hello:** `{proto, version, host, os, tmuxPresent, projects: N}` — protocol integer checked first; the version string is display-only. The running version currently lives in `main`-only ldflags (`cmd/sidecar/main.go:57-63`); a library accessor is a small prerequisite work item.
- **Snapshot:** the remote machine's own `config.json` `projects.list` (discovery mechanism unchanged — `internal/config/config.go:54-59`), each project's shells.json contents, worktrees, and resolved `agentstatus.Presentation` per workspace — the same `CollectProjectInventory` → `RefreshProjectStatus` pipeline the overview runs (`workspaceinventory/inventory.go:442-556`), driven by serve's own loop at the overview's adaptive cadence (5 s live / 10 s ready / 30 s idle).
- **Events:** inventory deltas and presentation transitions, pushed as they happen. This is the event stream the Herdr plan could only request upstream; here it is simply built. `notify.LaneTracker` — written so "a headless watcher tomorrow" reimplements only the adapter file (`internal/notify/triggers.go:11-29`) — is that adapter's second consumer.
- **Previews:** the status pass already captures ~80 lines per agent pane (`inventory.go:562-635`); serve ships that text (bounded by the existing `tmuxCaptureMaxBytes` discipline) so Sessions-browser preview cells work without opening a control channel. Full pane view on selection uses channel 1.
- **Requests (Phase C only):** create shell / worktree / start agent, mapping 1:1 onto `workspaceops` functions that the `sidecar create` CLI already exercises headlessly.

Serve was **read-only through Phase C** — it never wrote shells.json, never reaped, never took a geometry lease, and never resized (the one capture path that resizes, the lease-gated semantic preview at `workspace/agent.go:905-925`, is disabled under serve). Phase C's mutations arrived as one-shot `sidecar <verb> --json` invocations rather than as serve writes, so that held.

**It now has exactly one write, and the paragraph above is why it is the only one.** A remote shell the user had exited kept its record until somebody opened Sidecar on that machine, so serve reaps — through the existing guarded writers: flock + read-modify-write (`shellstate.go:294-336`), tombstones instead of deletions, incarnation fencing, the exact hardening added after the td-8d18de/shells-wipe incident. Serve still gets no bespoke state-writing code of its own: the decision half lives in `internal/shellliveness/reap.go` and the Sessions browser is the other caller. `TestServeIsReadOnly` continues to hold the tmux side of the guarantee, which is the part that was never in question. See [Remote shell improvements](remote-shells-improvements.md).

### Ephemeral serve dissolves the daemon problem

Herdr needs its server because the server *is* the terminal runtime. Sidecar's remote truth lives in the tmux server and the state tree, both of which outlive any Sidecar process — so serve can be spawned per connection and die on disconnect. There is no autostart question, no stale-daemon question, and no version-skew-restart question: the viewer spawns whatever binary is on the host, reads its protocol integer from the hello, and renders "sidecar too old on <host> (proto 3, need 4)" as an actionable row state. Multiple viewers each spawn their own serve; concurrent read-only serves against one state tree are the already-normal multi-instance case (flocked writes, atomic renames, fsnotify cross-instance visibility — `overview/live_shells.go:27-40`).

### Geometry across hosts (Phase B)

The lease already coordinates cross-machine through the tmux option store. Two rules need sharpening for a claimant whose PID is not on the tmux server's machine, both localized in `DecideGeometryLease`'s policy:

- **No defunct-PID reclaim of foreign tokens.** `OwnerDefunct` liveness checking is only valid on the owner's machine (`geometry_lease.go:143-147`); a remote viewer treats any token from another host as live and relies on idle/stale preemption only.
- **Input evidence is local.** The tty-matched `client_activity` harvest (`geometry_lease.go:340-371`) doesn't exist for a remote claimant; its "I typed recently" evidence comes from its own send-queue timestamps instead.

Everything else — duration-not-timestamp tokens, tick-based staleness, back-off on a fresh foreign token — was designed for this and transfers unchanged. A read-only viewer never claims; an interactive viewer claims on focus exactly as the local interactive mode does, and the human sitting at the remote machine wins it back by typing, per the idle-preemption rule that already exists.

### Where it binds in the UI

Identical to the Herdr plan, and stated once: remote panes are ordinary `paneframe` leaves; the remote pane kind gets one `livepanes.Binding` per surface fed by the frame stream rather than `livewatch`; hosts are overview-only until a remote checkout earns a project identity; nothing runs before the first frame (`SIDECAR_STARTUP_TRACE=stderr` must show no new pre-frame phase). `workspaceinventory.Workspace` gains `HostID` and drops the `TmuxName` leak — owed whichever remote plan ships, or neither.

## Work items surfaced by the research

- ~~**Library-visible version/protocol accessor** (currently `main`-only ldflags).~~ **Done in Phase 0** — `internal/buildinfo`.
- **Login-shell binary resolution is mandatory, not an optimisation.** A non-login ssh shell has no `/opt/homebrew/bin` on PATH, so a host with tmux plainly installed reports `tmux: executable file not found`. The remote command must be wrapped in `$SHELL -l -c CMD` — and specifically not `$SHELL -l -s`, which additionally runs the shell's interactive preexec hooks and writes OSC sequences onto the same stdout the protocol uses. Both forms were measured on a real host; `internal/hosts.RemoteShell` implements the safe one.
- **A "stream is not the protocol" row state.** Some host will have a login profile that prints to stdout regardless. The viewer must name that specific failure and its fix rather than surfacing a JSON syntax error.
- **Server death is not an incarnation change.** `tmux kill-server` does not unlink its socket (verified by inode), so `tmuxserver.Socket()` reports the same identity across a death; the incarnation only moves when a new server recreates the socket. "The remote server died" must be driven by the pane listing going empty — which is exactly the condition the reaper must refuse to act on (td-8d18de).
- **Linux process identity.** `process_identity_other.go` is a stub — argv0 disambiguation of shared-runtime panes (node/bun/agent) silently degrades to screen-chrome detection on Linux, and remote hosts will often be Linux. A `/proc`-based implementation (tpgid from `/proc/<pid>/stat`, argv0 from `cmdline`) is a Phase A item that also improves any Linux user's local fidelity today.
- **Host-aware control channel factory + remote `terminalInputSender`** — the two seams in `internal/tty`.
- ~~**Resize-without-transport-restart** if Phase 0 item 3 says the respawn is felt.~~ **Measured and dropped from Phase A.** A reseed over ssh costs 82–383 ms on a real link and 258–854 ms at 150 ms RTT — noticeable but not felt on a debounced, lease-gated, deliberate act. Revisit on Phase B experience, not on principle.
- ~~**Headless reap choreography** — not before Phase C, and only by porting the overview's guards (empty-listing skip, incarnation fence, tombstone writes), never fresh logic.~~ **Done** in [Remote shell improvements](remote-shells-improvements.md), and by moving the guards rather than porting them: `internal/shellliveness/reap.go` is the single decision function, with the overview and hostserve as its two bindings.

## Phases

### Phase 0 — spike ✅ complete (2026-08-29)

A second machine with Sidecar installed, a real link (LAN and a WAN/VPN hop), agents running in tmux sessions over there.

1. **Proxied control mode.** Swap the factory `exec.Cmd` for the ssh invocation against a remote session. Verify seed, byte continuity, `%pause` reseeds, and history windows behave identically to local. Measure: keystroke-to-frame latency (once input lands in Phase 0 item 4), output-burst throughput, idle cost (should be zero bytes), and seed cost on attach.
2. **Headless serve.** Drive `Collector` + `shellliveness.Tracker` + activity trackers + `agentstatus.Resolve` in a loop with no Bubble Tea, streaming snapshot + transitions as JSONL to a local consumer. Exit check: status shown locally matches the remote machine's own Sidecar TUI for working/blocked/done/idle across at least three agent providers, including the `done` decay.
3. **Resize cost.** Measure the control-transport restart over SSH per geometry change; decide whether reseed-without-restart is Phase A work or deferrable.
4. **In-band input.** Prototype the control-channel `terminalInputSender`; verify FIFO ordering under fast typing and paste; measure RTT per keystroke batch on the WAN hop.
5. **Failure axes.** ssh drop mid-stream (keepalive detection, fallback engagement, clean reattach); remote tmux server death (incarnation transition must mark rows dead, never wipe anything); sidecar missing/too old on the host (distinct actionable states); two viewers on one host; a viewer plus a human TUI on the host.

**Exit gate:** a recorded matrix of latency/bandwidth/CPU numbers and the answer to the only existential question: does a proxied-control-mode pane feel local-grade? Compare directly against the Herdr plan's Phase 0 numbers if both spikes run; this table is the bake-off input.

**Result: passed. Proceed to Phase A.** Run against `marcusbook` over LAN, Tailscale, and two shaped-latency columns, with both machines' default tmux servers and real state trees provably untouched. Headlines:

- **A proxied pane is local-grade.** Attach + first frame 94 ms on a LAN, 876 ms at 150 ms RTT. Seed 623 bytes. An idle pane costs **zero `%output` notifications**. A 256 KiB burst converges in 230 ms (LAN) / 913 ms (150 ms RTT) at 8–27 fps. No fallbacks in any run.
- **In-band input is worth building and the number says why.** Its overhead is a near-constant ~23 ms above link RTT (10.4 / 83.7 / 173.3 ms at 0 / 60 / 150 ms). Out-of-band — one `ssh tmux send-keys` per batch — costs 2.1×–6.2× more. FIFO held across 40 back-to-back batches on every link.
- **Serve matches the host's own TUI exactly.** Three providers, same lanes, same providers, same attention flags, same rows, at the same moment — because serve runs `agentactivity` and `agentstatus.Resolve` on the host over the host's own captures. Previews ship for free by decorating the capture the status pass already takes.
- **Failure axes held.** ssh drop → fallback in 0 ms and clean reattach. Remote tmux death → rows went `paused`, and `shells.json` was **byte-identical before and after**. Two viewers coexisted; a human's TUI on the host coexisted with a viewer; neither disturbed the other.
- **Not proven:** done-TTL decay over the wire, a Linux host (both machines are arm64 macOS, so the `process_identity` Linux stub was never exercised), a relayed WAN path, and a full day of use.

The three findings that change Phase A are recorded in the work items above and in the evidence document.

### Phase A — read-only remote hosts ✅ complete (2026-08-29)

Behind `features.SidecarRemoteHosts`, default off.

- Host registry with per-host config (ssh target, optional remote binary path — resolved via the login-shell probe recipe recorded in the Herdr plan — optional remote config path).
- Sessions browser host grouping; per-host projects/shells/worktrees with the full local status vocabulary; preview cells from serve captures.
- Live read-only pane view on selection via proxied control mode, rendered through `FitPane` at the pane's own size; never resizes, never claims a lease.
- Health as first-class row states: unreachable, no sidecar, protocol too old, no tmux server, stale data. Each names the fix.
- Linux process identity; version accessor; protocol v1.

**Exit gate:** the Herdr plan's, verbatim — on a real second machine, Sidecar answers "what is running over there and is anything blocked?" without opening a terminal, and a full day of use produces no stale or wrong status while a pane is on screen. Plus: the local path is provably untouched (no behavior or state diff with the flag off, and none with it on until a host is registered).

**Result: the gate is met except for the day of use.** Driven end to end against `marcusbook` from a fully isolated local Sidecar:

- **The question is answered without opening a terminal.** The Sessions browser showed `marcusbook · spike Claude pane` and `Opencode pane` under NEEDS ATTENTION, `Codex pane` under WORKING, and the worktree and plain shell under LIVE — the remote machine's real lanes, resolved by its own detectors.
- **Remote rows are ordinary rows.** They group, filter, sort and pin through the existing projections because `hosts.ProjectResults` converts a snapshot into `workspaceinventory` results carrying a `HostID`. Host grouping is the project label; no new renderer.
- **Health is a row, not a silence.** A deliberately unreachable second host showed `⚠ ghost` with ssh's own reason and the fix. Each state names one.
- **The live pane works.** Selecting the remote Claude row opened its actual screen through proxied control mode — the agent's question, rendered locally.
- **Read-only is enforced at three seams, not one.** Input is dropped, the pane is never resized and no lease is claimed, and the capture fallback is disabled rather than left pointing at local tmux — a local `capture-pane -t %4` for a remote `%4` does not fail, it paints an unrelated local pane. Interactive mode is refused outright rather than entered inertly.
- **Rollback is provable.** With the flag off: no rows, no registry, no ssh process. `SIDECAR_STARTUP_TRACE` shows the first ready frame at 63.7ms with an unreachable host configured, and no host phase before it.
- **Both machines were untouched:** default tmux servers and real state trees unchanged, run roots removed.

**Not met:** the full day of use. That is a soak, not a build step, and it is the remaining Phase A evidence.

The retained code was then independently reviewed. One CRITICAL defect, two HIGH, and several medium findings were fixed; the critical one is worth recording because no unit test would have found it and the real run did not surface it either:

**A `tty.Model` made remote stayed remote forever.** The preview reuses one terminal model across row selections, and `UseRemoteControl` had no counterpart — so after viewing a remote pane once, the next LOCAL row was opened by `ssh <host> tmux -C attach-session -t <local session name>`. Both machines run Sidecar and derive session names the same way, so that attach often *succeeds*: another machine's pane painted into a local workspace's preview, interactive mode offered and silently swallowing every keystroke, and the local pane never resized again. The fix is `UseLocalControl`, and the rule that the surface SETS the mode on every activation rather than changing it when it notices a difference. Pinned by a test that fails when the reset is removed.

Two more that mattered: preview content panes (diff, file finder, doc) resolved a remote workspace's path against the LOCAL filesystem — and on a machine with the same checkout that succeeds, showing this machine's diff under the remote row's name; and `tty.Target.Host` was dropped by the stored target, so "am I already showing this?" could never answer yes for a remote pane and every poll tore down its ssh connection and reseeded.

Three things the real run found that no unit test would have:

1. `hosts` in config.json parsed into nothing. The loader merges a `rawConfig` field by field, so a key present only on `Config` is silently ignored — a correct config producing no hosts and no error.
2. ssh's ControlMaster socket blew the ~104-byte unix path limit under macOS's `$TMPDIR` (`/var/folders/<2>/<28>/T/`), surfacing as an unreachable host with no hint of the cause. The control root is now under `/tmp`.
3. A registered host was invisible until its first connection resolved — and an ssh dial to a machine that is off runs to a full connect timeout. Initial health is now published at registration.

Host configuration is live-reloaded. Saving config refreshes feature resolution, reconciles the host registry, closes a selected terminal when its host is removed or retargeted, and rejects queued updates from the replaced host-client incarnation. This is the Phase B completion of td-998e58; restart-only host configuration is no longer a product limitation.

### Phase B — interactive remote panes ✅ complete (2026-08-29)

- In-band input uses the existing host-aware control pipe and a model-level FIFO, preserving exact ordering for fast keys, literal input, paste, mouse reports, lease changes, and backend replacement.
- Remote panes enter the Sessions browser's ordinary interactive mode with the same chrome, reserved chords, double-escape behavior, and immediate exit semantics as local panes.
- Cross-host leases use viewer-local input evidence, never foreign PID liveness. Interactive owners refresh at a settled size, blur/exit releases safely, and either machine restores its current viewport when its human input preempts the other.
- Complete history, search, match navigation, and frozen selection use the terminal model's host-aware in-band `CapturePaneRange`; no remote fallback can read an ambient local pane with the same ID.
- Remote resize remains a lease-gated explicit act through the existing debounced restart/reseed path. Generation and incarnation fences reject late activation, resize, host-update, and old-backend teardown work.
- Config saves refresh feature resolution and reconcile hosts without restart, including removal, same-ID retarget, selected-terminal teardown, and queued old-client rejection (td-998e58).

**Exit gate:** a blocked agent prompt on the remote host answered from the local Sidecar, with input ordering correct under fast typing, and a human at the remote machine able to take the pane back just by using it.

**Result: passed.** The final `7cff6d1e` candidate was driven between isolated Sidecars on `aerie` and `marcusbook`. Exact ordered input arrived on the remote pane. With deliberately different viewports, viewer input reclaimed and restored 103×45, then remote-human input reclaimed and restored 73×30 without either side leaving interactive mode. Viewer exit preserved the human's lease; human exit removed it. Both default tmux servers and real Sidecar state trees were untouched. Full repository tests, build, focused race tests, and independent review passed; see the Phase B evidence document.

### Phase C — creation, mutation, rename ✅ complete (2026-08-29)

**Serve did not gain a request channel, and that is the phase's main design decision.** Mutations are one-shot `ssh <target> sidecar <verb> --json` invocations through the ControlMaster that already carries the serve stream and tmux control mode. `hosts.Transport.SidecarCommand` already rendered that invocation; the CLI was already the proven headless caller with every guard; `create shell` and `create worktree` already emitted structured `--json` results.

What that buys is worth stating, because the plan originally specified the other shape:

- `hostproto` gains no request direction, and `hostserve` gains no write paths — so "serve is read-only by construction rather than by flag" remains a property of its call graph, checked by `TestServeIsReadOnly`, rather than a claim that quietly stopped being true. (Two later corrections, neither of them a request direction. The protocol integer moved to `Version = 2` for M5's server-to-viewer notification event: see the note below. And serve now has exactly one write, the reap — `TestServeIsReadOnly` still holds the tmux half, since serve issues no mutating tmux command, and the state write goes through `shellstate`'s conditional writer. The narrower claim is the true one.)
- The in-flight/replaced-transport ordering hazards are absent by construction. Phase B spent four consecutive review cycles on exactly that class inside one long-lived channel; a one-shot invocation with a deadline has no such state.
- Every mutation is equally reachable by an agent over plain ssh, which is the parity the project's design principles ask for. The verbs are the deliverable, not a byproduct.

The cost is one ssh exec per deliberate act — measured at 82–383 ms on a real link in Phase 0 — and three CLI verbs that had to be written because the capability was TUI-only.

- **Headless verbs, owed regardless of remote hosts.** `shell rename --target` renames a shell you are not sitting in; `shell send --target --run/--type` sends a command into an existing shell; `create worktree --plan` resolves a plan and emits it without mutating, so a confirmation can show branch, path, source OID, and whether a setup hook will run.
- **Four actions on a remote row**: create shell, create worktree (plan → confirm → execute), seed an agent, rename. Rows arrive through the ordinary next serve snapshot rather than being synthesized, so what the user sees is what the host reports.
- **`remoteActionRefusal` stopped being an unconditional no** and became a question about the verb, which is what its own doc comment anticipated. Delete, merge, and navigate still refuse: their implementations resolve paths against the local filesystem.

**Exit gate:** create → work → observe → answer → close entirely from the local Sidecar, with remote state trees left exactly as a local Sidecar would leave them.

**Result: passed, after a review cycle that found nine defects.** Driven between an isolated local Sidecar and `marcusbook`. Six of seven journeys passed outright; the seventh (a failed mutation being actionable) was partial until its fix landed.

`marcusbook` was byte-identical before and after — same default-server socket inode, same server pid, same 489 state files, same manifest checksum — and its installed sidecar still reported the pre-Phase-C build, because the proof pointed the registry's `remoteBinary` at a scratch path rather than installing anything. Remote isolation was proven three ways rather than asserted: the live ssh command line carried every lever, the host's own serve hello reported `isolatedState: true` with its resolved scratch `stateDir`, and the fail-closed backstop was demonstrated refusing when a lever was removed.

**The defect worth recording is the one about the safety surface.** `RunSidecar` decoded the *first* JSON value on stdout, and because Go tolerates missing fields, a host whose login profile emits structured log lines (`{"level":"info","msg":"loading nvm"}`) had that line accepted as the result — with a nil error and an all-zero value. The remote worktree confirmation then rendered blank, and pressing Create still ran the real `sidecar create worktree` on the host. The plan already named "a login profile that prints to stdout regardless" as a required row state; this is that condition one notch more specific, and no unit test found it because every banner fixture used non-JSON text. The decoder now prefers the last value, refuses a zero-value decode, and lets a result type declare which fields make an object its verb's answer — while still tolerating unknown fields, because forward compatibility is real.

Three more that mattered:

1. **Exit 2 meant both "usage error" and "your input was rejected."** Renaming a remote shell to a name already in use produced a perfectly good host message wrapped in "update Sidecar on whichever machine is older." Validation now has its own exit code (5); exit 2 keeps its version-skew meaning.
2. **`shell send` proved ownership against one tmux server and typed into another.** The record's namespace *is* the socket path, and the send path discarded it. The rename path passes it through and is pinned by a test; send is now refused on a mismatch. Every fixture in the suite shared one namespace, so deleting namespace handling entirely left the tests green.
3. **A host project that had never been opened on the host could not be mutated at all.** Serve advertises projects from the host's `config.projects.list`, but `--project` resolved only through already-existing state directories, so a first-run remote project returned `unknown project`. This was found by the live proof, not by review, and it is the first thing a new user of the feature would have hit.

The `pendingCreatedHost` finding is the Phase A `tty.Model` defect class verbatim — a field set on one of four activation paths, four lines below a comment stating the correct rule. It is now set on all four. That rule has earned its place as the first thing to check in this area.

**Not proven:** a first-run remote host end to end (it was blocked by the finding above and fixed after the run), multi-host and host-churn paths, so `hostReplyStale`/`remoteReplyDropped` went unexercised; concurrent mutations; a link dropped mid-mutation; any timeout path; a required or failing setup hook; and an agent actually doing work remotely — it reached its trust prompt unanswered.

**The fix cycle then needed a fix cycle, and that is the other thing worth recording.** Verifying the fixes found two regressions in local, flag-off behavior that the fixes' own green suite did not catch. One of them falsified the guarantee the first cycle had just established: `mutatesState` scanned every argument for `-h`/`--help`/`help` before resolving the subcommand, so a flag *value* disarmed the isolation gate — `shell send --run "help"`, an ordinary thing to send into a shell, skipped it and issued the tmux call. The gate now resolves the subcommand path first and reads the remainder the way the verb's own parser does. The other: the new exit 5 fell outside the set of codes meaning "the split placement declined", so `create shell --name <illegal>` printed its error, claimed it had created a workspace shell instead, printed the error again, and exited 5 having created nothing. That condition is now keyed on the decline codes, so the next code added is final by default rather than accidentally a fallback.

The lesson generalizes past this phase: a fix commit is a change like any other and earns its own verification pass. Both regressions were introduced by fixes to findings, both were in code the flag never gates, and neither was caught by the tests written alongside them.

**Gaps recorded rather than closed**, each with a task:

- `SIDECAR_ISOLATED_STATE` gates writes but not reads: an isolated run read a real repo's `.sidecar/shells.json` into its isolated tree (source unmodified, no damage). Closing it means read-boundary assertions with their own blast radius, since an isolated proof legitimately reads the repo — just not the real state tree.
- `config.Save` has no `AssertIsolatedPath` at all, so a proof run can write the real `config.json`. The dispatch-level `Command.Mutates` gate cannot cover it, because the TUI's config surface reaches the writer without passing through `cli.Run` — the fix belongs in the writer (td-cfa9a4).
- Worktree sessions record no tmux namespace, so `shell send` against a worktree target can still reach the wrong server. Same class as the shell-record hole this phase closed, narrower in practice, and it needs a new state field plus a migration (td-aad4f1).
- `/tmp/sc-hosts-*` ControlMaster directories outlive `tmux-drive.sh stop`.
- `sidecar shell rename` returns 5 for a rejected name with `--target` and 2 without. The exit tables describe both accurately; making them agree is a public exit-code change on a path unrelated to remote hosts, so it was left as a deliberate inconsistency rather than smuggled in here.

### Deferred from Phase C

Both were in the original Phase C scope and were cut deliberately, not forgotten:

- **Reap parity for remote rows.** This is the td-8d18de shells-wipe hazard class, executed against a machine the user is not sitting at. Remote shells are structurally never reaped today — `reapDeadShells` iterates local results only — so nothing regresses by waiting. Revisit when a real host actually accumulates dead rows, and only by porting the overview's three guards rather than writing fresh logic.
- **Remote conversations.** The plan's own position is that this should be scoped by real demand rather than built speculatively, and no demand has been recorded.

Also left: `remoteCreateShellArgs` does not pass `--tab` (it lands on workspace placement through an implicit dependency on the host's login environment, with a covering fallback), and create-with-agent is still two round trips (`create shell` then `shell send --run`) rather than one atomic `create shell --run`.

## Failure, degradation, security

- **Degrade the host, not the app** — a dead link marks one host offline; never blocks a frame; never touches local state (inherited verbatim from the Herdr plan).
- **Serve is read-only until Phase C, and provably so** — Phase A/B builds carry no state-writing code paths in `hostserve` at all, which is a stronger guarantee than a runtime flag. (True as written for Phases A through C; the reap added one write afterwards.)
- **The reaper never runs remotely until it has all three local guards.** The shells-wipe class of bug (td-8d18de) is the named hazard; empty-pane-listing skip, incarnation fencing, and tombstones are the named mitigations, already in the writers serve would eventually call.
- **SSH is the entire trust boundary.** Serve speaks stdio only; no sockets, no ports, no forwarding. The user's ssh config, keys, and agent are the security model, same as the Herdr plan.
- **Isolation discipline extends remotely.** Serve honors `SIDECAR_ISOLATED_STATE` and the `AssertIsolatedPath` guards, so proof runs against a remote host can never touch a real remote state tree — the same rule `tmux-drive.sh` enforces locally.
- **Bound everything:** serve output framed and capped; preview captures under the existing byte cap; control mailbox overflow already forces clean reseed (`terminal_surface.go:21, 301-312`); reconnect with backoff.

## Open questions

- **Does the serve loop's capture pass disturb a human using the remote machine?** It reuses the overview's semaphore-bounded, non-resizing observation, which coexists with a local TUI today — but "today" is same-machine; Phase 0 item 5 confirms nothing changes when the observer is remote-spawned.
- **One serve per viewer vs one serve shared.** Per-viewer is the simple default and matches the ephemeral design; if a host ever has many viewers, the collector work duplicates. Revisit only on evidence.
- **Namespace/socket scope.** Inventory correlates shell rows only on the default tmux socket namespace (`inventory.go:549`); whether remote hosts should surface non-default-socket sessions follows the local answer, whatever it becomes.
- **Should the hello carry host capabilities** (process-identity fidelity, adapter availability) so the viewer can render honest confidence per host? Likely yes and cheap; decide with the protocol v1 schema.
- ~~**Bake-off criteria vs the Herdr plan.**~~ Settled: this plan's spike ran, the numbers were decisive, and the Herdr plan is deprecated.

## Acceptance evidence

| Journey | Evidence |
| --- | --- |
| Startup | `SIDECAR_STARTUP_TRACE=stderr` shows no host work before the first ready frame, with an unreachable host configured |
| Host health | unreachable / no sidecar / protocol too old / no tmux / stale each render a distinct row state naming the fix |
| Status truth | Remote lanes match the remote machine's own Sidecar TUI, including `blocked` and `done` decay, across ≥3 providers |
| Live pane | A remote full-screen TUI, wide chars, colors, and alt-screen render correctly through the proxied control channel; idle costs zero bytes |
| History | Lazy history, search, and drag-selection during output work on a remote pane with the same contracts as local |
| Geometry | A read-only viewer never resizes anything; viewer and remote-human input preempt each other without re-entry and restore their own distinct viewports |
| Coexistence | A human's Sidecar TUI on the remote host sees no behavior change while a viewer observes; two viewers coexist |
| Reconnect | Link drop and tmux-server restart both reconverge (incarnation transition → dead rows → recovery), with nothing wiped |
| Isolation | Serve under `SIDECAR_ISOLATED_STATE` refuses to touch a real state tree, and a mutating CLI verb refuses before it creates a tmux session or a git worktree |
| Rollback | Flag off → local behavior and state byte-identical |
| Mutation | Create shell, create worktree through its confirmation, seed an agent, and rename both kinds, each landing on the host and returning as an ordinary row in the next snapshot |
| Refusal | Delete, merge, navigate and open-in-Git each refuse a remote row and name the machine |
| Mutation failure | A rejected mutation renders the host's own reason plus its fix, wrapped rather than truncated, and is recoverable from the running app's log |

Latency, bandwidth, and remote CPU are measured and recorded in the Phase 0 matrix. The Herdr spike never needed to run; the decision between the two plans was made from this plan's numbers alone, on the record.

## Changelog

- **2026-09-01** — The serve stream skips a login banner (td-055768). `hostproto.Decoder` refused the first non-JSON line, so a host whose profile printed one line to stdout was permanently `not-protocol`: no hello, no snapshot, no Sessions rows, no `@` destinations — and an error naming the exact cause it would not handle. Non-protocol output before the first message is now skipped up to `MaxPreludeLines` / `MaxPreludeBytes`, including JSON that is not a protocol message (a profile logging `{"level":"info"}` must not be mistaken for the stream starting, the same rule the run path already applies to a `--json` result). After the first message nothing is skipped, and an exhausted budget or an end of stream that only ever saw prelude fails quoting what the host wrote. An incompatible `proto` still reaches the caller, so a version mismatch stays a version mismatch rather than becoming a swallowed line. `scripts/loopback-ssh.sh` keeps writing its banner unconditionally, with no switch to disable it, and `internal/cli/serve_stream_loopback_test.go` drives the real serve stream over it.
- **2026-09-01** — [Remote destinations in `@` and `W`](remote-project-switcher.md) slices 0–2 bind a host project from `@`/`W` and list its shells in Workspaces. Files/Git remoting is that plan’s slice 4, not an unbounded hole.
- **2026-08-31** — Pointed at [The viewer owns the screen](../implemented/remote-host-viewer-screen.md) for agent `open` / `layout` on a host pane and mixed `n` routing. That plan observes host `uirequest` files on serve and announces them; it does not add a serve request channel or execute layout, which is the Phase C decision this file still holds.
- **2026-08-30** — `hostproto` moved to `Version = 2` for M5 of [notification sounds and native delivery](notification-sounds-and-native-delivery.md). The addition is one server-to-viewer message kind, `notify`, carrying a bounded typed event: a stable key, occurrence time, event class, title/body, and a remote origin. It carries no terminal output, prompt text, environment data, bundle identifier, TTY path, escape sequence, or command, and it introduces no request direction — the read-only call graph is unchanged. The bump is deliberate so mismatched binaries fail with the existing actionable version-mismatch row state rather than silently dropping a notification kind. Emission comes only from a live lane transition; snapshots and reconnect state never synthesize one, which is what keeps a stale remote prompt from crossing onto a local desktop at attach time. Local consumption is off by default behind `notifications.ssh.managedHosts`; the status stream is unaffected by that setting.
- **2026-08-30** — Pre-merge review pass (two fresh-context reviewers over the whole Phase C diff) closed six CLI/hosts findings and seven Sessions-browser findings before the branch reached `main`. The ones that changed contracts: `create worktree --expect-source-oid` now pins a confirmed remote plan to its commit, so a ref that moves between plan and Create is refused with exit 5 instead of silently building from the new head — the guard the local modal already had from executing its stored plan; a failing setup hook's own `command not found` is no longer misread as an uninstalled Sidecar; the result decoder's candidate window keeps the last JSON lines rather than the first, so a profile emitting 32+ structured-log lines cannot push a successful result out of the window; `notify config set` is now marked mutating and inside the isolation gate; and host-derived error text is stripped of terminal control bytes before display. In the browser: disabled hosts are refused up front with an accurate message instead of failing as "removed or retargeted", the rename modal gained an in-flight guard against double-Enter races, a stale reply from one host no longer wipes another host's pending selection, switching the create form to a remote project clears the local repo's branch list, and Merge is hidden on remote rows rather than offered and refused. Also updated the Herdr framing here and in the agent-control plan: the bake-off is resolved, the Herdr remote-hosts plan is deprecated, and Herdr remains the feature benchmark only.
- **2026-08-30** — Phase C fixes verified, and the verification found two regressions the fixes had introduced in local flag-off behavior: an isolation gate a flag value could disarm, and a rejected `--name` reported as a shell that had been created. Both repaired, with the exit-code tables and `docs/reference/cli.md` corrected to match. Recorded four gaps as tasks rather than widening the changeset: `config.Save`'s missing isolation assertion (td-cfa9a4), worktree sessions carrying no tmux namespace (td-aad4f1), reads not being gated by `SIDECAR_ISOLATED_STATE`, and the two rename forms disagreeing on a validation exit code.
- **2026-08-29** — Phase C completed, independently reviewed, and proven between two real machines. Mutations ship as one-shot `sidecar <verb> --json` invocations over the existing ssh master rather than as a serve request channel, so `hostproto` stays v1 and `hostserve` keeps its read-only call graph. Added three headless CLI verbs (`shell rename --target`, `shell send --target`, `create worktree --plan`), the `hosts.RunSidecar` seam, and remote create/worktree/seed-agent/rename in the Sessions browser. Review and the live run together found nine defects, including a decoder that let a host's structured log line blank the worktree confirmation for an operation that then really ran, and a configured-but-never-opened remote project that could not be mutated at all. Fixed `OpenSelectedInGit`, which had no remote guard and sent a remote path into a local `SwitchWorktree`. Reap parity and remote conversations deferred with reasons. Evidence in `docs/evidence/sidecar-remote-hosts-phase-c.md`.
- **2026-08-29** — Phase B completed and independently reviewed. Added ordered in-band remote input, Sessions interactive/search/history parity, host-aware lease and capture paths, bidirectional input-driven geometry takeover, nonblocking ordered teardown across control/backend replacement, and live host-config reconciliation including td-998e58. The final isolated two-machine gate passed with distinct 103×45 and 73×30 viewports; evidence is recorded in `docs/evidence/sidecar-remote-hosts-phase-b.md`.
- **2026-08-29** — Phase A built and driven end to end against a second real machine: host registry and config, a long-lived serve client with reconnect/backoff and a health vocabulary that names its fix, `HostID` on the inventory, remote rows in the Sessions browser and the Activity board, host grouping, health rows, a read-only live pane over proxied control mode, preview content from serve captures, and Linux `/proc` process identity. Behind `features.SidecarRemoteHosts`, default off. Remaining Phase A evidence: a full day of use.
- **2026-08-29** — Phase 0 run end to end against a second real machine. Verdict: proceed to Phase A. Resize-without-restart dropped from Phase A on measured evidence; login-shell resolution, a not-the-protocol row state, and the incarnation-vs-death distinction added as required Phase A items. Retained: `internal/hostproto`, `internal/hostserve`, `internal/hosts`, `internal/tty/control_remote.go`, `internal/buildinfo`, `sidecar host serve|probe`, and the spike harnesses under `scripts/`.
- **2026-08-28** — Created, from source research into the control-mode transport seam, the geometry lease's explicit two-machine design (td-ee222a), the UI-free awareness stack, shells.json v2 hardening, and the headless-readiness of `workspaceops`. Positioned as the competing alternative to the Herdr remote-hosts plan with a recorded bake-off posture.

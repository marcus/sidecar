---
sidebar_position: 3
title: CLI Reference
---

# CLI Reference

Complete reference for Sidecar's command-line interface, structured commands, flags, and exit codes for scripting and automation.

## Command Index

| Command | Purpose |
|---------|---------|
| [`sidecar agent`](#sidecar-agent) | Inspect, prompt, control, and coordinate AI coding agents |
| [`sidecar content`](#sidecar-content) | Internal host content transport (not `sidecar open --host`) |
| [`sidecar create`](#sidecar-create) | Create managed shells, terminal splits, and git worktrees |
| [`sidecar host`](#sidecar-host) | Register, configure, and probe remote hosts over SSH |
| [`sidecar layout`](#sidecar-layout) | Inspect, apply, and reposition multi-pane window layouts |
| [`sidecar notify`](#sidecar-notify) | Post, list, and dismiss notifications with action jumps |
| [`sidecar open`](#sidecar-open) | Open files, tasks, diffs, or resources in adjacent panes |
| [`sidecar plugin`](#sidecar-plugin) | Inspect and configure the plugins Sidecar hosts |
| [`sidecar session`](#sidecar-session) | Cold session restore planning, execution, and policy |
| [`sidecar setup`](#sidecar-setup) | Launch Sidecar on the setup and environment check screen |
| [`sidecar shell`](#sidecar-shell) | Manage shell records, display names, and send commands |
| [`sidecar terminal-links`](#sidecar-terminal-links) | Inspect terminal resource providers (the frozen protocol's alias of `sidecar plugin`) |

---

## `sidecar agent`

Inspect, start, and coordinate agents in Sidecar-managed shells.

```bash
Usage: sidecar agent <subcommand> [options]
```

### Subcommands

#### `sidecar agent list`
List all active agent sessions across workspaces.
- `--json`: Output stable structured JSON.

#### `sidecar agent get [TARGET]`
Get details for a specific agent by shell or session target.
- `--project NAME`: Target project (slug, basename, or path).
- `--include-session-ref`: Include the bound conversation ID.
- `--json`: Output structured JSON.

#### `sidecar agent start TARGET --kind KIND`
Start an agent provider in a managed shell.
- `--kind KIND`: Provider kind (`claude`, `codex`, `opencode`, `cursor`, etc.).
- `--json`: Output structured JSON.

#### `sidecar agent prompt [TARGET] TEXT`
Send prompt text to an agent.
- `--wait`: Block until the agent finishes processing.
- `--timeout DURATION`: Timeout for completion (e.g. `5m`, `10m`).
- `--json`: Output structured JSON.

#### `sidecar agent read TARGET`
Read the terminal output buffer from an agent shell.
- `--source SOURCE`: `recent-unwrapped` (default) or `scrollback`.
- `--json`: Output structured JSON.

#### `sidecar agent send-keys TARGET KEYS`
Send raw keys or escape sequences to an agent shell.

#### `sidecar agent integration <install|list|status|update|repair|uninstall> [PROVIDER]`
Manage provider lifecycle integration hooks.
- `--dry-run`: Preview file changes before modifying configuration.
- `--json`: Output structured JSON.

---

## `sidecar content`

Internal read-only transport a viewing Sidecar invokes on a registered host to resolve and load files, issues, notes, diffs, and resource documents. This is not a public file browser and not `sidecar open --host`. Agents in a Sidecar-managed pane on that host run `sidecar open` / `layout` onto the lease holder's screen; other processes use ordinary tools over SSH.

The host must advertise `ContentReadV1`. See [Remote Hosts](./remote-hosts#clicks-in-a-remote-terminal) for the user-visible clicks this powers, and [Agent open and layout from a host pane](./remote-hosts#agent-open-and-layout-from-a-host-pane) for the lease-holder rule.

```bash
Usage: sidecar content <describe|resolve|read> --json
```

### Subcommands

#### `sidecar content describe`
Return this host's validated, ordered resource-provider descriptors and a deterministic fingerprint.
- `--if-revision REV`: Return a small `notModified` object when the fingerprint is unchanged.
- `--json`: Write the machine contract (required).

#### `sidecar content resolve`
Resolve a file, issue, note, git spec, or resource locator against a durable workspace identity on this machine. The workspace id is re-resolved to its authoritative root on every request; the target is a hint, never authority.
- `--workspace ID`: Unscoped durable workspace id.
- `--kind KIND`: `file`, `issue`, `note`, `diff`, or `resource`.
- `--target VALUE`: Path, id, git spec, or resource locator.
- `--json`: Write the machine contract (required).

#### `sidecar content read`
Read a bounded document, issue card, note, git diff operation, or resource document.
- `--workspace ID`: Unscoped durable workspace id.
- `--kind KIND`: `file`, `issue`, `note`, `diff`, or `resource`.
- `--operation OP`: Kind-specific read (`document`, `card`, `note`, `resource`, `working-tree`, `commit`, …).
- `--target VALUE`: Path, id, git spec, or resource locator.
- `--if-revision REV`: Return a small `notModified` object when the content is unchanged.
- `--json`: Write the machine contract (required).

Full flags and exit codes: `sidecar content --help`.

---

## `sidecar create`

Create shells, terminal splits, and git worktrees in the running instance.

```bash
Usage: sidecar create <shell|worktree> [options]
```

### Subcommands

#### `sidecar create shell`
Create a new managed shell session.
- `--name NAME`: Display name for the shell.
- `--project NAME`: Target project (defaults to current directory).
- `--split DIR`: Open as a split pane (`left`, `right`, `up`, `down`).
- `--run CMD`: Execute command immediately on creation.
- `--type CMD`: Type command onto the prompt without pressing Enter.
- `--agent KIND`: Seed an agent in the new shell.
- `--auto`: Enable auto-approve for supported agent providers.

#### `sidecar create worktree`
Create a dedicated git worktree workspace.
- `--name NAME`: Branch / worktree name (required).
- `--base BRANCH`: Base branch to create from (default: main/default branch).
- `--plan`: Generate and output the worktree creation plan as JSON without modifying disk.
- `--expect-source-oid OID`: Verify the base branch commit OID matches expected value before creating.
- `--agent KIND`: Seed an agent in the new worktree.

---

## `sidecar host`

Manage and observe remote hosts over SSH.

```bash
Usage: sidecar host <subcommand> [options]
```

### Subcommands

#### `sidecar host add TARGET`
Register a new remote machine over SSH.
- `--id ID`: Unique identifier for the host (defaults to target).
- `--binary PATH`: Explicit path to `sidecar` binary on the remote host.
- `--remote-config PATH`: Path to config file on the remote host.
- `--env "KEY=VAL ..."`: Space-separated environment variables for the remote process.

#### `sidecar host list`
List all registered remote hosts and their enabled status.
- `--json`: Output structured JSON.

#### `sidecar host probe TARGET`
Probe an SSH target to inspect reachability, Sidecar version, protocol compatibility, and tmux status.
- `--json`: Output structured JSON.

#### `sidecar host set ID`
Update settings for an existing registered host.
- `--enabled` / `--disabled`: Toggle host connection state.
- `--target TARGET`: Update SSH destination target.

#### `sidecar host remove ID`
Unregister a remote host.

---

## `sidecar layout`

Inspect and manipulate the multi-pane grid layout. From a Sidecar-managed pane whose geometry lease is held by a connected viewer, these verbs read and mutate that viewer's screen. There is no `--host` flag. Off-screen, or a lease holder that cannot receive pane requests, is exit 4.

```bash
Usage: sidecar layout <get|apply|move> [options]
```

### Subcommands

#### `sidecar layout get`
Get the current layout structure, grid dimensions, and pane targets.
- `--sessions`: Inspect the global Sessions browser layout instead of the project workspace.
- `--json`: Output structured JSON.

#### `sidecar layout apply`
Compose panes additively or replace the layout atomically.
- `--pane DESCRIPTOR`: Add a single pane (repeatable).
- `--spec SPEC`: Complete layout specification string or `-` for standard input.
- `--sessions`: Target the global Sessions browser.
- `--json`: Output structured JSON.

#### `sidecar layout move FROM --to TO`
Reposition an existing pane in the layout.
- `FROM`: Grid cell coordinate (e.g. `2.1`).
- `--to CELL`: Target cell coordinate (e.g. `1.2`) or column number (e.g. `3`).
- `--focused --to DIRECTION`: Move the focused pane in a direction (`left`, `right`, `up`, `down`).
- `--sessions`: Target the global Sessions browser.
- `--json`: Output structured JSON.

---

## `sidecar notify`

Post and manage notifications and toasts.

```bash
Usage: sidecar notify <subcommand> [options]
```

### Subcommands

#### `sidecar notify post MESSAGE`
Post a notification toast.
- `--source SOURCE`: Source category identifier (e.g. `agent`, `tests`, `build`).
- `--target TARGET`: Actionable call to action (`file:path[:line]`, `issue:id`, `task:id`, `commit:hash`, `session:name`, `url:address`).
- `--urgency LEVEL`: `low`, `normal` (default), `high`, `critical`.

#### `sidecar notify list`
List active notifications in the Notification Centre.
- `--json`: Output structured JSON.

#### `sidecar notify dismiss`
Dismiss notifications.
- `--id ID`: Dismiss a specific notification.
- `--all`: Dismiss all active notifications.

#### `sidecar notify test`
Trigger a test notification.

---

## `sidecar open`

Open files, tasks, diffs, notes, or resources in adjacent panes. From a Sidecar-managed pane whose geometry lease is held by a connected viewer, the open lands on that viewer's screen. There is no `--host` flag; routing is the lease. A relayed open never queues. Remote Sessions clicks use the internal `sidecar content` transport.

```bash
Usage: sidecar open TARGET [options]
```

### Options

- `TARGET`: File path (`path/to/file.go[:line]`), TD issue ID (`td-abc123`), note ID, or resource locator.
- `--at CELL`: Place the pane at an exact grid cell (e.g. `1.2`, `2.1`).
- `--split DIR`: Open as a split in a direction (`left`, `right`, `up`, `down`).
- `--diff [REF]`: Open a git diff preview for a ref, commit, or range.
- `--plugin ID`: Open through a configured [plugin](plugins.md) instance. With `--collection` it opens that collection as a tab; without one it opens a matched locator.
- `--collection C`: With `--plugin`, the collection to open. A positional row ID opens that row's document instead of the list.
- `--query Q`: With `--collection`, the query the tab opens searched on.
- `--filter ID=VALUE`: With `--collection`, one of the collection's declared filters (repeatable).
- `--provider ID`: The older spelling of `--plugin`'s locator form, kept for the frozen resource protocol.

```bash
sidecar open --plugin recall --collection results --query dex --split right
sidecar open --plugin ongoing --collection projects recall
sidecar open --provider jira-work CASH-1245
```

---

## `sidecar plugin`

Inspect and configure the plugins Sidecar hosts. A plugin is either **embedded** (compiled into Sidecar, with its own UI) or **external**: an explicitly configured executable that answers JSON on stdout and that Sidecar renders itself. See [Plugins](plugins.md) for what a plugin is and what it looks like in the app.

An external plugin speaks one of two protocols, decided by the config section it is written in and never by anything the executable says. `plugins.external` entries speak `sidecar.plugin/v1`, which has `describe`, `resolve`, `list`, `get`, and `act`. `terminalResources.providers` entries speak the frozen `sidecar.terminal-resource/v1`, which has `describe` and `resolve`; [`sidecar terminal-links`](#sidecar-terminal-links) remains the surface for that section.

`plugins.external` is behind the `plugin_protocol` feature flag. Turn it on with `sidecar --enable-feature=plugin_protocol`, or set `features.flags.plugin_protocol` in config. Verbs that need it exit `4` when it is off.

```bash
Usage: sidecar plugin <command> [options]
```

Every subcommand takes `--json` for one structured result object on stdout.

### Subcommands

#### `sidecar plugin list`
List every plugin Sidecar knows about: the embedded ones in the order the header paints them, then every external plugin configured under `plugins.external` and `terminalResources.providers`. Each row reports class, scope, placements, and whether it is enabled; an external row also reports the config section it was read from.

Without `--describe` this reads configuration and runs nothing: no running Sidecar, no `PATH` lookup, no subprocess.

- `--describe`: Run `describe` on each active external plugin, with the app's own environment, working directory, and timeout.
- `--json`: Output structured JSON.

Exit codes: `0` success, `1` configuration read failure, `2` usage error.

#### `sidecar plugin check ID`
Answer "is this plugin configured, startable, and speaking the protocol", using the exact base environment, working directory, and timeouts the app uses. `describe` always runs. Only what the host kept is printed, never the plugin's raw stdout, so what you see is what a pane would draw.

`--list` and `--get` are separate, explicit flags because they can perform network access and print private data; neither is ever implied.

- `--list COLLECTION`: Also call `list` on this collection.
- `--query TEXT`: Query to send with `--list`. A collection whose search is required needs one.
- `--filter ID=VALUE`: Apply one declared filter with `--list` (repeatable). What is printed back is what the host actually sent, so a key that was dropped shows as dropped.
- `--get COLLECTION ID`: Also call `get` on this collection row (two values).
- `--json`: Output structured JSON.

Exit codes: `0` every requested call answered, `1` a call failed, `2` usage error, `3` no plugin with that ID is configured, `4` the governing feature flag is off.

```bash
sidecar plugin check recall
sidecar plugin check recall --list results --query dex --filter profile=docs
sidecar plugin check recall --get results rc:notes:1 --json
```

#### `sidecar plugin call ID METHOD`
Run one method — `describe`, `resolve`, `list`, `get`, or `act` — through the host's own envelope, validation, and sanitization, and print what the host would have kept. This is the authoring loop: write a response, call it, see exactly what survives.

`list` first runs `describe`, because the declared columns are what a page is sanitized against; a cell keyed by an undeclared column is dropped, and that is a finding worth seeing here rather than in a pane. No host context is sent: this process has no surface, so it has no project and no selection to offer.

- `--params JSON`: The method's params object.
- `--filter ID=VALUE`: Apply one declared filter to `list` (repeatable).
- `--json`: Output structured JSON.

Exit codes: `0` the plugin answered, `1` the call failed, `2` usage error, `3` no plugin with that ID is configured, `4` the governing feature flag is off.

```bash
sidecar plugin call recall describe --json
sidecar plugin call recall list --params '{"collection":"results","query":"dex"}' --json
sidecar plugin call dex act --params '{"action":"log-note","collection":"people","id":"p:ada","inputs":{"text":"hi"}}' --json
```

#### `sidecar plugin add ID --command ARGV...`
Append one entry to `plugins.external`. This is the whole install flow: Sidecar never scans a directory, never runs every `sidecar-*` binary on `PATH`, never auto-enables anything, and never lets a repository declare a plugin.

Everything after `--command` is the argv, executed directly with no shell, so put it last. Nothing is started: `add` prints exactly what will run — every argv element on its own line, the working directory, and the variables passed by name — and asks for confirmation.

A process boundary is crash isolation, not a sandbox. Configuring a plugin trusts that executable with your full OS privileges.

- `--command ARGV...`: The argv to run; everything after it is part of the command.
- `--pass-env NAME`: Pass this variable's current value through (repeatable, names only).
- `--scope SCOPE`: Lifecycle; `global` is the only value this version supports.
- `--placement WHERE`: `tab` or `panes` (repeatable; default both).
- `--timeout DURATION`: Per-call timeout, clamped to 1s–60s.
- `--claim-host HOST`: Hostname whose URLs this plugin may claim (repeatable).
- `--disabled`: Write the entry turned off.
- `-y, --yes`: Skip the confirmation.
- `--json`: Output structured JSON.

Exit codes: `0` the entry was written or the confirmation was declined, `1` the configuration could not be written, `2` usage error or the entry was refused by validation, `4` `plugin_protocol` is off.

```bash
sidecar plugin add recall --yes --command recall sidecar-plugin
sidecar plugin add dex --pass-env DEX_PROFILE --placement panes --yes --command dex sidecar-plugin
```

#### `sidecar plugin remove ID`
Delete one entry from `plugins.external`. Unknown config sections are preserved, and removing the last entry removes the key rather than leaving it empty. An entry in `terminalResources.providers` is not removed here: that section belongs to the frozen resource protocol, and the message says so.

- `--json`: Output structured JSON.

Exit codes: `0` written, `1` could not be written, `2` usage error, `3` no plugin with that ID is configured, `4` `plugin_protocol` is off, or the entry is in a section this verb does not own.

#### `sidecar plugin enable ID` / `sidecar plugin disable ID`
Set `enabled` on the `plugins.external` entry. `disable` keeps the entry, so turning it back on needs no argv. Enablement is read at startup, so a running Sidecar needs a restart.

- `--json`: Output structured JSON.

Exit codes are the same as `remove`.

#### `sidecar plugin changed ID`
Write one request onto the bus saying that a plugin's data changed. Every running instance re-lists the visible tabs of that plugin; a tab nobody is looking at costs nothing, so this is safe from a shell hook. It starts no plugin and reads no configuration.

- `--collection C`: Narrow the refresh to one collection. Omit it when the tool does not know what it touched.
- `--json`: Output structured JSON.

Exit codes: `0` the request was written, `1` it could not be written, `2` usage error.

```bash
sidecar plugin changed dex --collection people
```

---

## `sidecar session`

Manage cold session recovery and persistence policies.

```bash
Usage: sidecar session <status|restore|policy> [options]
```

### Subcommands

#### `sidecar session status`
Print the ordered cold restore plan for all managed shells.
- `--json`: Output structured JSON.

#### `sidecar session restore`
Execute the restore plan to recreate shells and optionally resume agents.
- `--dry-run`: Print plan without making changes.
- `--shell TARGET`: Restore only the specified shell.
- `--agents`: Resume eligible bound agent conversations.
- `--yes`: Confirm agent resumption non-interactively.
- `--json`: Output structured JSON.

#### `sidecar session policy TARGET <POLICY>`
Set restore policy for a shell.
- `--inherit`: Follow global default policy (ask before resuming agents).
- `--resume`: Always resume the agent automatically.
- `--shell`: Recreate the shell terminal only, never resume the agent.
- `--never`: Never restore this shell on restart.

---

## `sidecar shell`

Manage shell records and send commands to sessions.

```bash
Usage: sidecar shell <subcommand> [options]
```

### Subcommands

#### `sidecar shell list`
List live and forgotten shell records for the resolved project.
- `--json`: Output structured JSON.

#### `sidecar shell name`
Print the current shell's display name.
- `--json`: Output structured JSON.

#### `sidecar shell rename [NAME]`
Rename the current shell or a target session.
- `--target SESSION`: Rename a background tmux session.
- `--json`: Output structured JSON.

#### `sidecar shell send --target SESSION <--run CMD | --type CMD>`
Send a command to a background shell.
- `--run CMD`: Execute command in the shell (presses Enter).
- `--type CMD`: Type command without pressing Enter.
- `--json`: Output structured JSON.

#### `sidecar shell forget TARGET`
Move a shell record to a tombstone.

#### `sidecar shell restore TARGET`
Restore a tombstoned shell record.

#### `sidecar shell delete --target SESSION`
Close a tmux session and tombstone its record.

---

## `sidecar terminal-links`

Inspect the external executables that teach Sidecar to recognize resource keys in terminal output. This is the surface for the **frozen** `sidecar.terminal-resource/v1` protocol and the `terminalResources.providers` config section it is configured in — the alias of [`sidecar plugin`](#sidecar-plugin) for providers written before the plugin protocol. Those providers keep working unchanged; see [Terminal resource providers](plugins.md#terminal-resource-providers).

`sidecar plugin list` reports these instances too, naming the section each was read from. `sidecar plugin remove` will not touch one.

```bash
Usage: sidecar terminal-links <check|list> [options]
```

### Subcommands

#### `sidecar terminal-links list`
List the providers configured under `terminalResources`. By default this reads configuration and resolves each command on `PATH`; it starts no process. `passEnv` is reported by name and presence only, never by value.
- `--describe`: Also ask each enabled provider to describe itself (one child process per instance).
- `--config PATH`: Read a specific config file.
- `--json`: Output structured JSON.

Exit codes: `0` success, `1` configuration could not be read, `2` usage error.

#### `sidecar terminal-links check INSTANCE`
Check one configured provider instance: that it is enabled, that its command resolves, and that its `describe` answers the protocol. The child runs with the exact working directory, base environment, `passEnv` policy, and timeout Sidecar uses in the TUI, so this is the authoritative host-environment proof. The provider's stderr is drained and discarded, never printed.

`--resolve` is separate and explicit because it can perform network access and print private resource data. Without it, nothing is resolved.

- `--resolve LOCATOR`: Also resolve one locator.
- `--config PATH`: Read a specific config file.
- `--json`: Output structured JSON.

Exit codes: `0` the instance checked out, `1` the command, describe, or resolve failed, `2` usage error, `3` no provider instance with that ID is configured.

```bash
sidecar terminal-links list --describe --json
sidecar terminal-links check jira-work --resolve CASH-1245 --json
```

---

## `sidecar setup`

Start Sidecar with the Configuration page open on **Sidecar Setup** to inspect your environment, verify color support, and run automated health checks.

```bash
sidecar setup
sidecar setup --project /path/to/project
```

# Sidecar CLI Reference

Sidecar provides non-interactive commands for scripting and agent workflows.

## `sidecar agent`

Inspect, start, and coordinate agents in Sidecar-managed shells

Provider-aware control over shells Sidecar owns.

The safe sequence is: create the layout separately with sidecar create shell, start the provider with agent start, prompt and wait, read before you send keys, and never close a target you did not create.

With --host ID the verb runs on that registered host instead, as one invocation over the existing ssh connection, and the host's own answer is what you get back. A remote verb needs an explicit TARGET, because the omitted-target rule names the shell you are in and that shell is on this machine. Conversation identifiers stay on the host that owns them: remote output reports whether a shell is bound, not what it is bound to, unless you ask with --include-session-ref.

The report, end, release, and explain commands are a separate surface: they record and inspect the lifecycle events a provider's own integration reports, and they are not gated behind agent_control.

```
Usage: sidecar agent <command>
```

### `sidecar agent end`

Report that the current agent run ended

Records a terminal outcome and clears lifecycle authority. The outcome is not a fourth lane: a finished run's lane is idle, and the outcome is separate evidence the status projection may use for health. Process liveness still confirms the run really ended before any surface calls the pane orphaned or failed.
This is a hook surface and it fails open: outside a Sidecar-managed shell it exits 0 and prints nothing, and no failure here ever changes the provider's own behavior or output.

Identity is derived by Sidecar from the managed-shell environment, live tmux, and this process's ancestry. Host, tmux server, pane, and provider process cannot be selected through flags, so a hook can only ever report about the pane it is running in.

Nothing is stored beyond lanes, outcomes, bounded reason codes, sequences, timestamps, and opaque identity. Prompt text, response text, tool arguments and results, and credentials are never recorded.

```
Usage: sidecar agent end --outcome completed|cancelled|failed|unknown --source SOURCE --provider PROVIDER --seq N [--session-id ID] [--reason CODE] [--json]
```

**Options:**

- `--outcome OUTCOME`: completed, cancelled, failed, or unknown (required)
- `--source SOURCE`: Integration source identifier (required)
- `--source-version VERSION`: Installed integration asset version
- `--provider PROVIDER`: Catalog agent kind (required)
- `--seq N`: Strictly increasing sequence within this run. Omit it to have the store assign the next one, which is what a per-event hook process should do
- `--session-id ID`: Provider session identifier; only a salted digest is retained
- `--reason CODE`: Bounded reason code from the frozen allowlist
- `--detail TEXT`: Short sanitized diagnostic; never prompt, response, or tool content
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, or no-op outside a Sidecar-managed shell
- `1`: the report could not be stored
- `2`: usage error
- `5`: invalid context, stale sequence, or run mismatch

**Examples:**

```bash
sidecar agent end --outcome cancelled --source sidecar.opencode.plugin --provider opencode --seq 9 --reason cancelled
```

### `sidecar agent explain`

Explain which evidence authored a pane's lifecycle state

Reports the effective state, which evidence authored it, the source's exercisable tier, the last valid report, and — when lifecycle evidence did not win — exactly why not.

With --file it runs the screen lane alone over a saved capture: no tmux, no lifecycle store, no running agent. It does read the local override directory, so two people reproducing one fixture can reach different verdicts if one of them has an override for that agent; the `manifest` line of the output says which file answered. That is how a wrong badge is reproduced from a fixture, and how a new fixture is minted.

Detection manifests can be tuned locally: a file at ~/.config/sidecar/agent-detection/<file>.toml replaces the vendored Herdr manifest for that agent, where <file> is the vendored file's own base name (github-copilot.toml for Copilot, antigravity.toml for Antigravity). It replaces the Sidecar overlay too rather than layering over it, so a rule Sidecar rewrote upstream is not rewritten under an override. An override that cannot be parsed, that declares a different agent, or that needs a newer engine is ignored and the vendored manifest is used; either way explain prints a warning line saying what was found and why.

Every diagnostic fact the Configuration surface shows is available here, so a pane that is not being driven by its integration always has an actionable reason rather than silence.

This command is read-only. It never locks, compacts, repairs, or creates the lifecycle log.

```
Usage: sidecar agent explain [--current | --shell TARGET | --file PATH --agent KIND] [--json]
```

**Options:**

- `--current`: Explain the pane this command is running in (the default)
- `--shell TARGET`: Explain a managed shell by name
- `--file PATH`: Explain a saved capture offline, with no tmux and no lifecycle store (a local override for the agent is still read)
- `--agent KIND`: Which agent's manifest to evaluate --file against (required with --file)
- `--title TEXT`: Pane title for --file when the capture carries no header
- `--rows N`: Pane height for --file; the detection read window. Must be positive; defaults to the fixture header, else 24
- `--print-window`: With --file, print the detection read window instead of a verdict
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, or no-op outside a Sidecar-managed shell
- `1`: internal failure: the explanation could not be produced or written
- `2`: usage error
- `5`: invalid context, or a rejected value: an unreadable --file, an unknown --agent

**Examples:**

```bash
sidecar agent explain --current --json
sidecar agent explain --file internal/agentactivity/testdata/claude/blocked.txt --agent claude --json
```

### `sidecar agent get`

Get one managed agent

TARGET is a managed tmux session name or unique display name. Inside a managed shell it may be omitted.

An explicit TARGET is searched across every registered project. When that finds the same name in several projects, the caller's own project — the one SIDECAR_SHELL belongs to — breaks the tie; outside a managed shell the refusal lists the projects, and --project NAME (a slug, path, or a worktree Sidecar created, by path or basename) or --shell NAME picks one. This rule is shared by get, start, prompt, wait, read, and send-keys.

sessionRef reports whether the shell is bound to an exact provider conversation. Its value is shown for your own shell, or with --include-session-ref; otherwise only the kind and whether an official integration reported it are returned, so ordinary output does not carry conversation identifiers into logs.

```
Usage: sidecar agent get [TARGET] [--project NAME] [--include-session-ref] [--json]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help
- `--include-session-ref`: Include the bound conversation's value, not only its presence

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent get reviewer --json
```

### `sidecar agent integration`

Inspect and manage agent lifecycle integrations

An integration is a small addition to a supported agent's own user-level configuration -- a Sidecar-owned file, or one entry in a file the user owns -- which reports that agent's own lifecycle events so Sidecar does not have to infer them from its screen.

Installation is always explicit, always previewable, and always reversible. Sidecar shows the exact user-level paths it would change before changing them, writes atomically, keeps a recoverable backup of anything it replaces, and removes only what it installed.

The same application service answers Configuration → Agents → Integrations, so every fact and action there has an equivalent here.

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration <command>
```

#### `sidecar agent integration install`

Install a provider's Sidecar lifecycle integration

Writes the bundled integration into the provider's user-level configuration: a Sidecar-owned file for a provider that loads whole files from a directory, or one added entry for a provider whose hooks live in a configuration file the user owns. Nothing is installed into a repository, and an existing user configuration is edited in place rather than rewritten from a template.

Installing when the current version is already installed is a no-op. Installing over an older or a damaged installation is refused, naming update or repair instead: the verb should mean what the user believes the situation to be.

The exact ordered file operations are printed, each with the state of its path before and after and whether Sidecar owns it. --dry-run prints that same plan and changes nothing.

Sidecar only ever writes, replaces, or removes something it can prove is its own. Where the integration is a whole Sidecar-owned file, that proof is the integration marker its bytes carry. Where the integration is one entry inside a configuration file the user owns, it is that entry's own content, and every other byte of the file is preserved. Either way, something that merely has the name or shape Sidecar would have chosen is refused and left exactly as it is.

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration install PROVIDER [--dry-run] [--json]
```

**Options:**

- `--dry-run`: Print the exact ordered file operations and change nothing
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, including a no-op when nothing needed changing
- `1`: the change was attempted and failed part-way
- `2`: usage error
- `5`: refused: unknown or unsupported provider, wrong verb for the current state, or an unsafe path

**Examples:**

```bash
# see the exact files first
sidecar agent integration install opencode --dry-run
sidecar agent integration install opencode --json
```

#### `sidecar agent integration list`

List every agent integration Sidecar knows about

One line per provider: whether its CLI is installed, whether Sidecar's integration is installed and current, and the authority tier that integration can actually exercise.

A provider Sidecar has recorded evidence for but ships no asset for is listed as unsupported rather than omitted, so "not yet" is distinguishable from "never heard of it".

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration list [--json]
```

**Options:**

- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, including a no-op when nothing needed changing
- `1`: the change was attempted and failed part-way
- `2`: usage error
- `5`: refused: unknown or unsupported provider, wrong verb for the current state, or an unsafe path

**Examples:**

```bash
sidecar agent integration list --json
```

#### `sidecar agent integration repair`

Repair a damaged or duplicated installation

Restores the bundled asset over one that has been modified or truncated, and removes a duplicate copy Sidecar owns in a second directory the provider also loads.

It cannot repair a file Sidecar does not own, and says so rather than deleting it.

The exact ordered file operations are printed, each with the state of its path before and after and whether Sidecar owns it. --dry-run prints that same plan and changes nothing.

Sidecar only ever writes, replaces, or removes something it can prove is its own. Where the integration is a whole Sidecar-owned file, that proof is the integration marker its bytes carry. Where the integration is one entry inside a configuration file the user owns, it is that entry's own content, and every other byte of the file is preserved. Either way, something that merely has the name or shape Sidecar would have chosen is refused and left exactly as it is.

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration repair PROVIDER [--dry-run] [--json]
```

**Options:**

- `--dry-run`: Print the exact ordered file operations and change nothing
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, including a no-op when nothing needed changing
- `1`: the change was attempted and failed part-way
- `2`: usage error
- `5`: refused: unknown or unsupported provider, wrong verb for the current state, or an unsafe path

**Examples:**

```bash
sidecar agent integration repair opencode --json
```

#### `sidecar agent integration status`

Report one provider's integration state in full

Reports the installed and bundled asset versions, the provider CLI version and whether it falls inside the range Sidecar has proved, the authority tier and any demotion reason, every path inspected with what was found in it, the known gaps recorded for the source, and the actions that would be accepted right now.

Status is decided by inspecting the installed files: the bytes on disk are hashed against the bundled asset, so a modified, truncated, or hand-edited asset reports needs-repair rather than current.

With no PROVIDER, every provider is reported.

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration status [PROVIDER] [--json]
```

**Options:**

- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, including a no-op when nothing needed changing
- `1`: the change was attempted and failed part-way
- `2`: usage error
- `5`: refused: unknown or unsupported provider, wrong verb for the current state, or an unsafe path

**Examples:**

```bash
sidecar agent integration status opencode --json
# every provider
sidecar agent integration status
```

#### `sidecar agent integration uninstall`

Remove a Sidecar-owned integration and nothing else

Removes the asset Sidecar installed, any duplicate copy Sidecar owns, and the backup Sidecar kept. The provider's own configuration and every unrelated plugin are left untouched, and the plugin directory is removed only when removing Sidecar's files empties it.

Uninstalling when nothing is installed is a no-op, so a cleanup script can run unconditionally. It works with the provider CLI already gone.

The exact ordered file operations are printed, each with the state of its path before and after and whether Sidecar owns it. --dry-run prints that same plan and changes nothing.

Sidecar only ever writes, replaces, or removes something it can prove is its own. Where the integration is a whole Sidecar-owned file, that proof is the integration marker its bytes carry. Where the integration is one entry inside a configuration file the user owns, it is that entry's own content, and every other byte of the file is preserved. Either way, something that merely has the name or shape Sidecar would have chosen is refused and left exactly as it is.

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration uninstall PROVIDER [--dry-run] [--json]
```

**Options:**

- `--dry-run`: Print the exact ordered file operations and change nothing
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, including a no-op when nothing needed changing
- `1`: the change was attempted and failed part-way
- `2`: usage error
- `5`: refused: unknown or unsupported provider, wrong verb for the current state, or an unsafe path

**Examples:**

```bash
sidecar agent integration uninstall opencode --dry-run
```

#### `sidecar agent integration update`

Update an installed integration to the bundled version

Replaces an older installed asset with the version this Sidecar build ships, keeping a recoverable copy of what it replaced.

Refused when nothing is installed, and when the installation is damaged rather than merely old.

The exact ordered file operations are printed, each with the state of its path before and after and whether Sidecar owns it. --dry-run prints that same plan and changes nothing.

Sidecar only ever writes, replaces, or removes something it can prove is its own. Where the integration is a whole Sidecar-owned file, that proof is the integration marker its bytes carry. Where the integration is one entry inside a configuration file the user owns, it is that entry's own content, and every other byte of the file is preserved. Either way, something that merely has the name or shape Sidecar would have chosen is refused and left exactly as it is.

An installed integration reports lifecycle facts only: lanes, a terminal outcome, a bounded reason code, a sequence, and an opaque session digest. It never sends prompt text, response text, tool arguments or results, file paths, environment values, or credentials, and it cannot notify, play a sound, or choose delivery policy.

```
Usage: sidecar agent integration update PROVIDER [--dry-run] [--json]
```

**Options:**

- `--dry-run`: Print the exact ordered file operations and change nothing
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, including a no-op when nothing needed changing
- `1`: the change was attempted and failed part-way
- `2`: usage error
- `5`: refused: unknown or unsupported provider, wrong verb for the current state, or an unsafe path

**Examples:**

```bash
sidecar agent integration update opencode
```

### `sidecar agent list`

List live managed agents

```
Usage: sidecar agent list [--project NAME] [--include-session-ref] [--json]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help
- `--include-session-ref`: Include the bound conversation's value, not only its presence

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent list --json
```

### `sidecar agent manifests`

List every detection manifest, its version, and which source is active

Prints the table `explain` reports for one agent, for every agent Sidecar vendors a manifest for: which of the three sources is active, the version that source carries, the version vendored into this binary, the version in the runtime fetch cache, whether the Sidecar overlay was merged in, and any file that was found and refused.

Precedence is a local override in ~/.config/sidecar/agent-detection, then the newer of the runtime fetch cache and the vendored manifest, with the Sidecar overlay merged onto whichever upstream file won.

The runtime fetch is off unless `detection.remoteManifests` in ~/.config/sidecar/config.json is set to "herdr.dev" or to a catalog index URL. When it is on, Sidecar checks at most once a day, after the first frame, and a check that fails is reported here rather than shown to the user.

Off means off: with the setting off, nothing fetches and no cached manifest is loaded, so every agent runs the vendored file again. A cache left over from when it was on is still listed in the REMOTE column, marked as not in use, because "you have a fetched file and it is not the one running" is what this table exists to say. `--clear-cache` deletes it.

Without a flag this command is read-only: it never fetches, and it never writes the cache or its status file. `--refresh` and `--clear-cache` are the two forms that change something, and each of them prints the table afterwards.

```
Usage: sidecar agent manifests [--refresh | --clear-cache] [--json]
```

**Options:**

- `--refresh`: Check the catalog now, ignoring the once-a-day gate (requires detection.remoteManifests to be on)
- `--clear-cache`: Delete every cached manifest and the fetch status file, then print the table
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: the vendored manifest tree could not be read, or --refresh or --clear-cache failed
- `2`: usage error, including --refresh with detection.remoteManifests off

**Examples:**

```bash
sidecar agent manifests
sidecar agent manifests --json
sidecar agent manifests --refresh
sidecar agent manifests --clear-cache
```

### `sidecar agent prompt`

Send a prompt to a managed agent, optionally waiting for it to settle

With two positional arguments the first is the target and the second is the prompt.
With one, the prompt goes to the shell named by SIDECAR_SHELL — unless that one
argument names a managed target, which is read as a missing prompt rather than as
a prompt that happens to be a target's name. Empty text is a usage error too.

Nothing is written to a target that is blocked, unidentified, stale, dead, or
occupied by a replacement process. The text goes through the same ordered,
bracketed-paste-aware path the embedded terminal uses, and the submission key is
sent separately, so a headless prompt delivers exactly what typing it would.

A prompt sent from idle or done must produce an observed lifecycle change within 5s
or the command reports agent_prompt_stalled. A prompt sent to an agent that is
already working makes no claim about which turn is which: completion of the turn
already in flight may satisfy --wait.

```
Usage: sidecar agent prompt [TARGET] TEXT [--wait] [--until STATUS]... [--timeout DURATION] [--json]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help
- `--wait`: Submit and wait for the agent to settle under one pinned target
- `--until STATUS`: Repeatable settled state: idle, done, blocked, or working (default idle, done, blocked)
- `--timeout DURATION`: Required with --wait; there is no implicit timeout

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent prompt reviewer "Review the current diff and report only actionable findings." --wait --timeout 2m
# the shell you are running in
sidecar agent prompt "Summarise what changed." --json
```

### `sidecar agent read`

Read a managed agent's output without touching it

Every source is a passive snapshot. Reads never scroll, resize, or otherwise
manipulate the agent's own screen.

  visible           the current screen
  recent            the screen plus recent scrollback
  recent-unwrapped  recent, with soft-wrapped lines joined back together
  detection         the exact slice the lifecycle detector read
  transcript        the provider's own conversation, once an exact session
                    binding exists; otherwise transcript_unavailable. It is
                    never guessed from the newest session in the same directory.

```
Usage: sidecar agent read [TARGET] [--source SOURCE] [--lines N] [--ansi] [--json]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help
- `--source SOURCE`: visible, recent, recent-unwrapped, detection, or transcript (default visible)
- `--lines N`: Bound the result to the last N lines
- `--ansi`: Preserve styling where the source has it

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent read reviewer --source recent-unwrapped --lines 120
# the evidence behind the status
sidecar agent read reviewer --source detection --json
```

### `sidecar agent release`

Surrender lifecycle authority for the current agent run

Gives up authority without claiming an outcome, for an integration that is being uninstalled or disabled, or that has detected it can no longer observe the run truthfully. The pane returns to ordinary screen and process detection immediately rather than holding its last reported lane.
This is a hook surface and it fails open: outside a Sidecar-managed shell it exits 0 and prints nothing, and no failure here ever changes the provider's own behavior or output.

Identity is derived by Sidecar from the managed-shell environment, live tmux, and this process's ancestry. Host, tmux server, pane, and provider process cannot be selected through flags, so a hook can only ever report about the pane it is running in.

Nothing is stored beyond lanes, outcomes, bounded reason codes, sequences, timestamps, and opaque identity. Prompt text, response text, tool arguments and results, and credentials are never recorded.

```
Usage: sidecar agent release --source SOURCE --provider PROVIDER --seq N [--session-id ID] [--reason CODE] [--json]
```

**Options:**

- `--source SOURCE`: Integration source identifier (required)
- `--source-version VERSION`: Installed integration asset version
- `--provider PROVIDER`: Catalog agent kind (required)
- `--seq N`: Strictly increasing sequence within this run. Omit it to have the store assign the next one, which is what a per-event hook process should do
- `--session-id ID`: Provider session identifier; only a salted digest is retained
- `--reason CODE`: Bounded reason code from the frozen allowlist
- `--detail TEXT`: Short sanitized diagnostic; never prompt, response, or tool content
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, or no-op outside a Sidecar-managed shell
- `1`: the report could not be stored
- `2`: usage error
- `5`: invalid context, stale sequence, or run mismatch

**Examples:**

```bash
sidecar agent release --source sidecar.opencode.plugin --provider opencode --seq 10 --reason integration_removed
```

### `sidecar agent report`

Report a lifecycle lane for the current agent run

Records what a provider's own lifecycle event observed. A report is evidence, not a verdict: whether it authors the pane's state depends on the source's proved capability tier, the report's freshness, and whether every identity field still matches the live pane.
This is a hook surface and it fails open: outside a Sidecar-managed shell it exits 0 and prints nothing, and no failure here ever changes the provider's own behavior or output.

Identity is derived by Sidecar from the managed-shell environment, live tmux, and this process's ancestry. Host, tmux server, pane, and provider process cannot be selected through flags, so a hook can only ever report about the pane it is running in.

Nothing is stored beyond lanes, outcomes, bounded reason codes, sequences, timestamps, and opaque identity. Prompt text, response text, tool arguments and results, and credentials are never recorded.

```
Usage: sidecar agent report --state working|blocked|idle --source SOURCE --provider PROVIDER --seq N [--session-id ID] [--reason CODE] [--json]
```

**Options:**

- `--state LANE`: working, blocked, or idle (required)
- `--source SOURCE`: Integration source identifier (required)
- `--source-version VERSION`: Installed integration asset version
- `--provider PROVIDER`: Catalog agent kind (required)
- `--seq N`: Strictly increasing sequence within this run. Omit it to have the store assign the next one, which is what a per-event hook process should do
- `--session-id ID`: Provider session identifier; only a salted digest is retained
- `--reason CODE`: Bounded reason code from the frozen allowlist
- `--detail TEXT`: Short sanitized diagnostic; never prompt, response, or tool content
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success, or no-op outside a Sidecar-managed shell
- `1`: the report could not be stored
- `2`: usage error
- `5`: invalid context, stale sequence, or run mismatch

**Examples:**

```bash
sidecar agent report --state working --source sidecar.opencode.plugin --provider opencode --seq 1 --reason turn_start
```

### `sidecar agent report-session`

Bind the pane's exact native agent conversation

Records which of a provider's own conversations is running in this managed shell, so a cold restart can offer to resume that exact conversation and `agent read --source transcript` can return it.

This is an integration surface rather than a coordination command: it is meant to be called by a provider hook, and it fails open exactly like `agent report` — outside a Sidecar-managed shell it exits 0, prints nothing, and records nothing.

The shell it binds is derived from the managed-shell environment and verified against live tmux. A hook chooses only which conversation it is talking about; it can never select another shell, pane, host, or tmux server through a flag.

A report wins only if it comes from the provider process that currently occupies the pane. Late output from an exited or replaced provider is ignored rather than allowed to overwrite its successor's binding.

--kind is a claim, not evidence. Some providers read another provider's settings file on purpose, so an installed hook can fire under an agent it was not installed for. A kind that does not match the pane's own provider is refused rather than bound, because a wrong binding would offer to resume one agent's conversation with a different agent.

Only an official Sidecar integration source may set an auto-resumable reference. A path reference must be absolute, normalized, and inside one of that provider's approved conversation store roots. Session values are never interpolated into a shell command, and never appear in ordinary `agent list` output.

```
Usage: sidecar agent report-session --kind KIND (--id ID | --path ABS_PATH | --clear) [--source SOURCE] [--hook-stdin] [--json]
```

**Options:**

- `--kind KIND`: Catalog agent kind, e.g. codex or claude (required)
- `--id ID`: Provider session identifier
- `--path ABS_PATH`: Absolute path to the provider's own transcript file
- `--clear`: Remove the shell's session binding
- `--source SOURCE`: Reporting integration source; defaults to this provider's official source
- `--hook-stdin`: Read the provider's hook payload as bounded JSON on stdin
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: recorded, or a no-op outside a Sidecar-managed shell
- `1`: the binding could not be written
- `2`: usage error
- `5`: invalid reference, untrusted source, unusable hook payload, unverifiable shell context, a provider that does not occupy the pane, or a stale provider generation

**Examples:**

```bash
sidecar agent report-session --kind codex --id 019f2c8a-1d4e-7b02-9c11-6f3a0b7d2e55
sidecar agent report-session --kind claude --hook-stdin
sidecar agent report-session --kind codex --clear
```

### `sidecar agent send-keys`

Send validated logical keys to a managed agent's UI

With two or more positional arguments the first is the target and the rest are
keys. With exactly one, the key goes to the shell named by SIDECAR_SHELL.

Keys are named, not typed: enter, esc, tab, space, backspace, delete, insert,
the arrows, home, end, pageup, pagedown, f1-f12, ctrl+<letter>, ctrl+space,
alt+<key>, shift+tab, shift+enter, shift+<arrow>, and any single character.
The whole list is validated before any of it is written, so a typo sends
nothing at all.

This is for answering an agent's UI, not for typing at it: prompt text belongs
to sidecar agent prompt. When a wait returns blocked the sequence is read the
screen, decide, then send keys. Sidecar never answers an approval for you.

```
Usage: sidecar agent send-keys [TARGET] KEY [KEY ...] [--json]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent send-keys reviewer down enter
# dismiss a picker
sidecar agent send-keys reviewer esc
```

### `sidecar agent start`

Start a provider in an idle managed shell and wait for readiness

Refuses commands, editors, copy mode, agents, ambiguous panes, and replacement processes. Provider arguments remain structured until the final shell boundary.

```
Usage: sidecar agent start [TARGET] --kind KIND [--timeout DURATION] [-- AGENT_ARG ...]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help
- `--kind KIND`: Catalog provider kind (required)
- `--timeout DURATION`: Bound the readiness wait (default 30s)

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent start reviewer --kind codex --timeout 30s
```

### `sidecar agent wait`

Wait for a managed agent to reach a settled state

Observes the target without writing to it. The target stays pinned to the same
tmux session, pane, pane process, server, and provider for the whole wait: a
replacement occupant is reported as agent_replaced rather than satisfying it.

```
Usage: sidecar agent wait [TARGET] [--until STATUS]... --timeout DURATION [--json]
```

**Options:**

- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--shell NAME`: Resolve the project from a registered shell
- `--host ID`: Run the verb on a registered remote host (requires an explicit TARGET)
- `--json`: Write stable structured JSON
- `-h, --help`: Show this help
- `--until STATUS`: Repeatable settled state: idle, done, blocked, or working (default idle, done, blocked)
- `--timeout DURATION`: Required; there is no implicit timeout

**Exit codes:**

- `0`: success
- `1`: transport, timeout, or internal failure
- `2`: usage error or version skew
- `3`: target is not registered
- `5`: feature disabled or semantic/value refusal

**Examples:**

```bash
sidecar agent wait reviewer --timeout 5m --json
# blocked no longer settles the wait
sidecar agent wait reviewer --until done --timeout 5m
```

## `sidecar agents`

List what an agent can do from Sidecar

List the Sidecar commands worth reaching for, one line each.
Also spelled "sidecar --agents".

```
Usage: sidecar --agents
```

**Exit codes:**

- `0`: success

**Examples:**

```bash
sidecar --agents
```

## `sidecar content`

Read-only content and tree contract a viewing Sidecar invokes on a host

Resolve, list, and read files, issues, notes, diffs, and resources for a viewing Sidecar over the existing host request seam.

This is an internal transport endpoint, not a public open-on-host surface.
Every verb is non-interactive, read-only, and strictly enumerated.

```
Usage: sidecar content <command>
```

### `sidecar content catalog`

List file, diff, issue, and note picker candidates for a workspace

List the bounded picker catalogs a viewing Sidecar offers for File, Diff, Issue, and Note on this machine.

This is the read-only catalog a viewing Sidecar invokes on a host, not a general file browser.
The workspace id is re-resolved to its authoritative root on every request.
--kind restricts the list to one picker kind; omitting it returns every kind together.
Resource rows come from content describe, not this verb.

--json writes the machine contract.

```
Usage: sidecar content catalog --workspace ID [--kind file|issue|note|diff] [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--kind KIND`: Picker kind (file, issue, note, or diff); omit to list all
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: listed
- `1`: internal or load failure
- `2`: usage error or unknown kind
- `5`: value rejected: unknown workspace

**Examples:**

```bash
sidecar content catalog --workspace /home/me/api:shell:sidecar-sh-1 --json
sidecar content catalog --workspace /home/me/api:shell:sidecar-sh-1 --kind file --json
```

### `sidecar content describe`

Describe this host's terminal resource providers

Describe configured terminal resource providers on this machine and return validated ordered descriptors.

The fingerprint is a deterministic hash of that wire content, never a process-local snapshot generation.
--if-revision returns a small notModified object when the descriptors are unchanged.

--json writes the machine contract.

```
Usage: sidecar content describe [--if-revision REV] [--json]
```

**Options:**

- `--if-revision REV`: Skip the descriptors when they still have this fingerprint
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: described, or notModified
- `1`: internal or load failure
- `2`: usage error

**Examples:**

```bash
sidecar content describe --json
sidecar content describe --if-revision v1:abc --json
```

### `sidecar content read`

Read bounded file, issue, note, diff, or resource content

Read a file document, issue card, note, git diff operation, or resource document from a durable workspace identity on this machine.

This is the read-only content contract a viewing Sidecar invokes on a host, not a general file browser.
--if-revision returns a small notModified object when the content is unchanged, so a refresh is one round trip.
The encoded JSON is capped under 768KiB; a payload that would blow that cap is truncated or returned as a structured oversize object rather than invalid JSON.
Issue fallback candidates come from this host's configured projects.
Diff operations are enumerated: working-tree, working-tree-file, commit, range, commit-file, full-file.
Resource reads return the provider wire document; the viewer sanitizes again. --refresh bypasses the host manager cache.
The collection and item operations are the plugin protocol's: collection lists one collection of --provider, item expands one row of it. A Resource pane bound to this host asks through them, so it lists this machine's plugins rather than the viewer's.

--json writes the machine contract.

```
Usage: sidecar content read --workspace ID --kind file|issue|note|diff|resource --operation OP [--target VALUE] [--provider ID --matcher ID] [--collection ID --query Q --view ID --sort ID --cursor C] [--path PATH] [--parent HASH] [--offset N] [--limit N] [--if-revision REV] [--refresh] [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--kind KIND`: Content kind (file, issue, note, diff, or resource)
- `--operation OP`: Read operation (document, card, note, resource, collection, item, working-tree, working-tree-file, commit, range, commit-file, or full-file)
- `--target VALUE`: File path, issue/note id, git spec, or resource locator as resolved or as the viewer saw it
- `--provider ID`: Resource provider instance id
- `--matcher ID`: Resource matcher id
- `--collection ID`: Plugin collection id for the collection and item operations
- `--query Q`: Collection search query
- `--view ID`: Collection view id
- `--sort ID`: Collection sort key id
- `--cursor C`: Opaque page cursor from a previous collection read
- `--path PATH`: Diff file path for working-tree-file, commit-file, and full-file
- `--parent HASH`: Merge parent hash for commit-file and full-file
- `--offset N`: Full-file page offset in lines
- `--limit N`: Full-file page size in lines
- `--if-revision REV`: Skip the body when the content still has this revision
- `--refresh`: Bypass the host resource cache
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: read, or notModified
- `1`: internal or load failure
- `2`: usage error or unknown kind
- `5`: value rejected: unknown workspace, containment, or not found

**Examples:**

```bash
sidecar content read --workspace /home/me/api:shell:sidecar-sh-1 --kind file --operation document --target README.md --json
sidecar content read --workspace /home/me/api:shell:sidecar-sh-1 --kind file --operation document --target README.md --if-revision v1:abc --json
sidecar content read --workspace /home/me/api:shell:sidecar-sh-1 --kind resource --operation collection --provider recall --collection results --query dex --json
```

### `sidecar content resolve`

Resolve a file, issue, note, diff, or resource target to identity and metadata

Resolve a file, issue, note, git spec, or resource locator against a durable workspace identity on this machine.

This is the read-only content contract a viewing Sidecar invokes on a host, not a general file browser.
The workspace id is re-resolved to its authoritative root on every request; the target is a hint, never authority.
Relative file paths cannot escape that root. Explicit absolute and ~/ targets keep local Sidecar's rule: a regular readable file outside the project is allowed.
Issue and note targets are identity only: the id is normalized without consulting td.
Diff targets are git specs: wt, a commit, or A..B / A...B. The host rev-parses commit and range specs.
Resource targets need --provider and --matcher; resolve is identity only and does not invoke the provider.

--json writes the machine contract.

```
Usage: sidecar content resolve --workspace ID --kind file|issue|note|diff|resource --target VALUE [--provider ID --matcher ID] [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--kind KIND`: Content kind (file, issue, note, diff, or resource)
- `--target VALUE`: File path, issue/note id, git spec, or resource locator as the viewer saw it
- `--provider ID`: Resource provider instance id
- `--matcher ID`: Resource matcher id
- `--collection ID`: Plugin collection id for the collection and item operations
- `--query Q`: Collection search query
- `--view ID`: Collection view id
- `--sort ID`: Collection sort key id
- `--cursor C`: Opaque page cursor from a previous collection read
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: resolved
- `1`: internal or load failure
- `2`: usage error or unknown kind
- `5`: value rejected: unknown workspace, containment, or not found

**Examples:**

```bash
sidecar content resolve --workspace /home/me/api:shell:sidecar-sh-1 --kind file --target README.md --json
```

### `sidecar content tree`

List directories under a workspace root for a viewing Sidecar's file tree

List one or more directories inside a durable workspace identity on this machine.

This is the read-only tree contract a viewing Sidecar invokes on a host, not a public browse-my-disk surface.
--path is repeatable and relative to the workspace root; omitting it lists the root alone, and "." names the root explicitly alongside other paths.
A viewer opens its tree with one call for the root plus every directory it had expanded, so a deep tree costs one round trip rather than one per level.
Each listing reports name, directory and symlink flags, whether git ignores the entry, size, and modification time. Symlinks are not followed.
A directory that has gone missing or unreadable is reported on that directory alone; the other listings still return.
The encoded JSON is capped under 768KiB; oversized listings are truncated and say so rather than failing the call.

--json writes the machine contract.

```
Usage: sidecar content tree --workspace ID [--path REL]... [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--path REL`: Directory relative to the workspace root, or "." for the root; repeatable; omit for the root alone
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: listed
- `1`: internal or load failure
- `2`: usage error
- `5`: value rejected: unknown workspace, containment, or too many paths

**Examples:**

```bash
sidecar content tree --workspace /home/me/api:worktree:/home/me/api --json
sidecar content tree --workspace /home/me/api:worktree:/home/me/api --path internal --path internal/cli --json
```

## `sidecar create`

Create a Sidecar-managed shell or worktree

Create Sidecar-owned shells and worktrees so they appear in the workspace.

```
Usage: sidecar create <command>
```

### `sidecar create shell`

Create a Sidecar-managed workspace shell

Create a new Sidecar-managed shell in the resolved project's workspace.
The shell is recorded in shells.json so it appears in Sidecar whether or not
an instance is running. --run executes a command in the new shell; --type types
it without pressing Enter so the user can review it.

From inside a Sidecar shell the default placement is a live terminal beside
the current shell. --tab places the shell in the workspace instead, switching
to a completely new surface; --split auto|right|below picks the side of the
beside-the-session split explicitly (the workspace_terminal_panel feature,
on by default, must not be disabled). Beside-the-session modes need a running
instance and a current shell (SIDECAR_SHELL / --shell) and do not add a
workspace row: the result's session is a sidecar-tp-… terminal split, not a
managed shell, so it is invisible to `shell list`, refused by the agent verbs,
and closable only with `shell delete --target` (nothing else names it).
Handing a shell to a second agent (coordinate-agents) needs a workspace row, so
use --tab rather than the default placement.

--agent records which agent family the shell is for, in the same durable field
the TUI's Create Shell writes. That record is what keeps the shell on the
Activity board while the agent is booting and whenever live screen
identification misses a frame. With the agent_control feature enabled and no
--run or --type of your own, --agent also starts the provider and returns only
when it is ready; otherwise it records the family and starts nothing, and --run
(or `sidecar shell send --run` afterwards) owns the launch. Because only a
workspace row carries the field, --agent places the shell there: it is refused
with --split and overrides the beside-the-session default.

Provider arguments go after `--`, as `agent start` takes them: `--agent claude
-- --model fable` starts the family's command with those arguments appended and
still records the family. They need --agent and they need agent_control, since
they describe a launch this command performs. They belong to --agent's launch,
so with --run or --type they are refused: put them in your own command instead.
A configured launch override (.sidecar-agent-start, plugins.workspace.agentStart)
takes them appended, unless it contains shell syntax such as a pipe, in which
case the start is refused and names the command to put them in.

Usage refusals with --json are `{"error":{"code":"usage",...}}` on stderr,
like the agent verbs; without --json they are the reason and the help text.
The result carries `project`, the slug every other verb's --project accepts.

```
Usage: sidecar create shell [options]
```

**Options:**

- `--name NAME`: Display name (default: the next Shell N)
- `--agent TYPE`: Record the agent family (claude, codex, …), and start it when agent_control is on
- `-- ARGS`: Provider arguments appended to --agent's launch command
- `--skip-permissions`: Pass the selected provider's auto-approve flag
- `--run COMMAND`: Execute COMMAND in the new shell
- `--type COMMAND`: Type COMMAND without pressing Enter
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--split auto|right|below`: Place a live terminal beside the current shell
- `--tab`: Open as a workspace shell instead of beside this session
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: created (missing ack is non-fatal in workspace-shell mode)
- `1`: state or tmux failure
- `2`: usage error, or this directory is not in a registered project
- `3`: no running instance (split mode)
- `4`: instance declined (cap, too small, or feature off)
- `5`: a value was rejected: --name, --agent, an unknown --project / --shell, or provider arguments with agent_control off

**Examples:**

```bash
sidecar create shell --name reviewer --agent codex --json
sidecar create shell --name "dev server" --run "python3 -m http.server"
# an agent shell the board knows is one
sidecar create shell --agent claude --run claude
# the catalog command with provider arguments
sidecar create shell --tab --name orchestrator --agent claude -- --model fable
sidecar create shell --split right --run "python3 -m http.server 8765"
sidecar create shell --json --wait 0
# type a command for the user to review
sidecar create shell --type "go test ./..."
```

### `sidecar create worktree`

Create a Sidecar-managed git worktree

Create a git worktree with the same setup pipeline as the TUI create modal:
plan, add, pending-creation journal, identity, and configured hook/env-file rules.
--agent records the agent family on the worktree and, with agent_control on,
starts it in the worktree session (sidecar-ws-…) and returns when it is ready.
Provider arguments go after `--`, as `agent start` takes them: `--agent claude
-- --model fable` appends them to the family's command. --run COMMAND launches
the session with your own command instead; given with --agent it still records
the family and starts nothing else, the layering `create shell` has. --no-launch
skips the launch after the worktree and setup still complete.

The name comes before `--`. Because `--` also ends flag parsing, a name that
starts with a dash may stand alone after it (`create worktree -- -fix`), but
once provider arguments follow, or the lone value looks like a flag, the command
is refused rather than guessing which value was meant as the name.

--plan resolves the same plan and prints it without changing anything: no
worktree is added, no directory is created, no journal is written. It answers
the questions a confirmation has to ask — branch, path, source ref and OID,
remote policy, and whether a setup hook will run — while every validation
failure (an existing branch, an occupied path, an unsafe hook) still surfaces
as exit 5. --run and --no-launch describe a launch --plan never performs, so
they are refused with it; --agent and --skip-permissions are kept, since they
come back as plan fields.

--expect-source-oid OID pins a previously confirmed plan: if the base ref no
longer resolves to OID when this command runs, it is refused with exit 5 and a
message naming both commits. A caller that showed a --plan result in a
confirmation passes the plan's sourceOid back here, and gets the same
source-moved guard the TUI's confirmation gets from executing its stored plan.

The result carries `project`, the slug the agent verbs' --project accepts, and
those verbs also accept the worktree's path or basename as --project. Usage
refusals with --json are `{"error":{"code":"usage",...}}` on stderr, like
the agent verbs; without --json they are the reason and the help text.

```
Usage: sidecar create worktree [options] <name> [-- ARGS...]
```

**Options:**

- `--base REF`: Base ref (default HEAD)
- `--plan`: Resolve and print the plan without creating anything
- `--expect-source-oid OID`: Refuse (exit 5) if the base ref no longer resolves to this commit
- `--agent TYPE`: Record the agent family and, with agent_control on and no --run, start it in the worktree session
- `-- ARGS`: Provider arguments appended to --agent's launch command
- `--skip-permissions`: Pass the agent's auto-approve flag
- `--run COMMAND`: Execute COMMAND in the new worktree session
- `--no-launch`: Create the worktree without launching a session
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path; or a worktree it created, by path or basename)
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: created (missing ack is non-fatal), or plan resolved with --plan
- `1`: git, setup, or tmux failure
- `2`: usage error (an unknown flag, a refused flag combination)
- `5`: a value was rejected: the plan (branch exists, path occupied, unknown base ref, unsafe hook), an unknown --project / --shell, or the source moved past --expect-source-oid

**Examples:**

```bash
sidecar create worktree fix-auth --base main --agent claude
# the catalog command with provider arguments; the family is recorded
sidecar create worktree orchestrate --agent claude --json -- --model fable
sidecar create worktree scratch --no-launch --json
# what would be created, without creating it
sidecar create worktree fix-auth --base main --plan --json
```

## `sidecar help`

Show help for commands or emit JSON command metadata

Show help for Sidecar commands, or emit the full machine-readable command tree.

```
Usage: sidecar help [--json] [<command>]
```

**Options:**

- `--json`: Write the command tree as JSON to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `2`: unknown command

**Examples:**

```bash
sidecar help
sidecar help open
sidecar help --json
```

## `sidecar host`

Remote hosts: register them, and observe them over SSH

Register and observe other machines running Sidecar.

`list`, `add`, `remove` and `set` edit this Sidecar's host registry — the
same entries the Remote Hosts page in Configuration shows, written through
the same validation. `probe` asks one machine what it is answering with;
`serve` is the half that runs on the remote host.

```
Usage: sidecar host <list|add|remove|set|probe|serve> [options]
```

### `sidecar host add`

Register a remote host

Register another machine running Sidecar, to be observed over SSH.

The target is whatever `ssh <target>` already resolves on this machine —
its keys, its ProxyJump, its agent. Sidecar adds no second place to
describe how to reach a host, so anything that works in ssh works here and
nothing that does not can be fixed from this command.

--id names the host in the UI and scopes its workspace rows; it defaults to
the target. --binary is for a machine whose login shell does not find
sidecar on PATH. --config observes a host against a config other than its
user default. --env is extra environment for the remote process, which is
how a proof host is pinned to its own tmux server and state tree
(TMUX_TMPDIR, XDG_STATE_HOME, SIDECAR_ISOLATED_STATE).

--disabled registers a machine without connecting to it, which is what a
host that is off this week wants: the entry keeps its settings.

```
Usage: sidecar host add <ssh-target> [--id NAME] [--binary PATH] [--config PATH] [--env KEY=VALUE]... [--disabled] [--json]
```

**Options:**

- `--id NAME`: Local name for the host (defaults to the target)
- `--binary PATH`: Explicit sidecar path on the host
- `--config PATH`: -config path for the remote sidecar
- `--env KEY=VALUE`: Environment for the remote process (repeatable)
- `--disabled`: Register the host without connecting to it
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: registered
- `1`: the configuration could not be read or written
- `2`: usage error
- `5`: a value was rejected — an empty target, a name already registered, or a malformed --env

**Examples:**

```bash
sidecar host add marcusbook
# Name it, and point at a sidecar the login shell cannot find
sidecar host add marcusbook --id book --binary /opt/homebrew/bin/sidecar
# A proof host pinned to its own tmux server and state tree
sidecar host add proof-host --env TMUX_TMPDIR=/tmp/proof --env SIDECAR_ISOLATED_STATE=1
```

### `sidecar host list`

List the registered remote hosts

List the machines registered in this Sidecar's configuration, with the
target each resolves through ssh_config and whether it is switched off.

This reads config.json. It connects to nothing: use `sidecar host probe`
to ask whether a machine actually answers.

Registered hosts are only observed while the sidecar_remote_hosts feature
flag is on; the output says so when it is off.

```
Usage: sidecar host list [--json]
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: the configuration could not be read
- `2`: usage error

**Examples:**

```bash
sidecar host list
sidecar host list --json
```

### `sidecar host probe`

Connect to a remote host's serve stream and report what came back

Spawn `sidecar host serve --stdio` on an SSH target and consume its stream.

Prints a health verdict naming the fix when something is wrong: unreachable,
no sidecar on the host, protocol too old on either end, no tmux, or a stream
that is not the protocol at all (a login-shell banner on stdout is the usual
cause). With --raw it passes the JSONL through untouched, which is the form
to capture when recording evidence.

```
Usage: sidecar host probe <ssh-target> [--json] [--raw] [--cycles N] [--timeout D]
```

**Options:**

- `--json`: Write one structured result object to stdout
- `--raw`: Pass the host's JSONL through verbatim
- `--cycles N`: Stop after N snapshots (default 1)
- `--timeout D`: Give up after this long (default 30s)
- `--binary PATH`: Explicit sidecar path on the host
- `--remote-config PATH`: -config path for the remote sidecar
- `--env K=V`: Environment for the remote process (repeatable)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: host answered and is compatible
- `1`: host unreachable, incompatible, or not serving the protocol
- `2`: usage error

**Examples:**

```bash
sidecar host probe marcusbook
# Record a raw transcript
sidecar host probe marcusbook --raw --cycles 3
```

### `sidecar host remove`

Unregister a remote host

Drop a machine from this Sidecar's registry, by the name `sidecar host list`
shows.

Nothing on that machine is touched: the entry described how to watch it, not
what runs there. To stop connecting while keeping the settings, use
`sidecar host set <id> --disabled` instead.

```
Usage: sidecar host remove [--json] <id>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: unregistered
- `1`: the configuration could not be read or written
- `2`: usage error
- `3`: no host is registered under that id

**Examples:**

```bash
sidecar host remove marcusbook
sidecar host remove --json book
```

### `sidecar host serve`

Stream this machine's Sidecar state as JSONL (spawned over SSH by a remote viewer)

Run the headless host agent: collect this machine's projects, shells,
worktrees and agent status on the ordinary Overview cadence, and stream a
versioned JSONL snapshot plus status transitions to stdout.

This is not a daemon. It is spawned per connection over an SSH stdio pipe
and exits when that pipe closes.

It has two writes besides observation: a shell record whose tmux session is
confirmed gone is reaped — tombstoned through the flocked, conditional writer
the Sessions browser uses, so `sidecar shell restore` still brings it back —
and, when SIDECAR_VIEWER_INSTANCE is set, an ephemeral presence file under
stateDir/viewers/. Without the reap, a row for a shell the user had already
exited stayed on the viewer's screen until somebody opened Sidecar on this
machine. Serve does not write request acks, take a geometry lease, resize a
pane, or issue any mutating tmux command.

Nothing is bound to a network. SSH is the entire transport and the entire
trust boundary.

```
Usage: sidecar host serve --stdio [--cycles N] [--project NAME=PATH]
```

**Options:**

- `--stdio`: Serve on stdin/stdout (the only transport)
- `--cycles N`: Exit after N collection cycles (0 = run until the pipe closes)
- `--project NAME=PATH`: Observe this project instead of the configured list (repeatable)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: stream ended cleanly
- `1`: serve failed
- `2`: usage error

**Examples:**

```bash
# What a viewer runs over ssh
sidecar host serve --stdio
# One cycle, for inspection
sidecar host serve --stdio --cycles 1
```

### `sidecar host set`

Change a registered host's settings

Change one registered machine. Every field left unnamed is left alone.

--env replaces the whole environment list rather than appending to it, so
the entry after the command is exactly what the flags said; pass a single
empty --env "" to clear it. --binary and --config likewise clear when given
an empty value.

--disabled keeps the host registered but unconnected, which is what a
machine that is off this week wants; --enabled connects to it again.

```
Usage: sidecar host set <id> [--target T] [--id NEWID] [--binary PATH] [--config PATH] [--env KEY=VALUE]... [--enabled|--disabled] [--json]
```

**Options:**

- `--target T`: New ssh destination
- `--id NEWID`: Rename the host
- `--binary PATH`: Explicit sidecar path on the host
- `--config PATH`: -config path for the remote sidecar
- `--env KEY=VALUE`: Replace the remote environment (repeatable)
- `--enabled`: Connect to this host again
- `--disabled`: Keep the host registered but unconnected
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: saved
- `1`: the configuration could not be read or written
- `2`: usage error
- `3`: no host is registered under that id
- `5`: a value was rejected — an empty target, a name already registered, or a malformed --env

**Examples:**

```bash
sidecar host set book --disabled
sidecar host set book --target marcusbook.local --enabled
# Clear the pinned environment
sidecar host set proof --env ""
```

## `sidecar layout`

Read and compose the pane layout agents work beside

Read the current pane layout (`layout get`), open several panes at once in
one atomic call (`layout apply`), or reposition one pane that is already
open (`layout move`). All three act on the surface showing this Sidecar
shell — or, with --sessions, the global Sessions surface — and never queue:
a request whose destination is off screen declines with the reason.

From a Sidecar-managed pane whose geometry lease is held by a connected
viewer, these verbs read and mutate that viewer's screen, not a host TUI.
There is no --host flag. If the lease holder cannot receive pane requests,
the command declines (exit 4).

```
Usage: sidecar layout <command>
```

### `sidecar layout apply`

Open several panes in one all-or-nothing call

Compose panes onto the surface showing this Sidecar shell.

--spec is a FULL layout, given as columns of stacked panes; it replaces
what is on screen:

  {"columns":[
    {"panes":[{"kind":"primary"}]},
    {"panes":[{"kind":"file","targets":["path:line","path2",...]},
              {"kind":"issue","targets":["td-xxxxxx",...]}]},
    {"panes":[{"kind":"shell","run":"...","name":"..."}]}
  ]}

A spec needs exactly one "primary" pane and must CARRY every live leaf
already on screen exactly as `layout get` prints them: the primary as
{"kind":"primary"}, a split terminal as {"kind":"shell","session":
"<tmux-session>"}. A spec omitting a live terminal declines naming the
session — apply never destroys one. Passive panes not named are closed
freely (their content re-opens). Pass `-` to read the spec from stdin.

--pane opens panes ADDITIVELY without closing anything. Each value is one
descriptor as its JSON object verbatim:

  {"kind":"file","targets":["path:line",...],"at":"2.1"}
  {"kind":"issue","targets":["td-xxxxxx"]}
  {"kind":"note","targets":["nt-xxxxxx"]}
  {"kind":"diff","targets":["spec"]}   no targets = the working tree
  {"kind":"resource","provider":"<instance>","targets":["LOCATOR"]}
  {"kind":"shell","run":"...","type":"...","name":"..."}

The first target opens a pane; the rest join it as tabs of the same kind.
"at" is an optional grid cell col.row (1-based) and is a requirement, not a
preference: an unreachable cell declines rather than landing elsewhere.
File paths are workspace-relative; diffs re-resolve host-side; providers are
validated against the live matcher snapshot.

Either form is validated and fit-tested before anything changes: it all
happens, or nothing changes and the decline names the first violation.

The ack's items array lists EVERY requested pane with verdict opened,
retargeted, carried (a live leaf the spec kept rather than created), or
declined, plus its landed cell — so one round trip shows everything wrong
with a refused spec. Like get, apply never queues: off-screen, or a lease
holder that cannot receive pane requests, is exit 4.

```
Usage: sidecar layout apply (--spec '<json>' | --pane '<json>' [--pane '<json>' ...]) [--sessions [ROW]]
```

**Options:**

- `--spec JSON`: A full layout replacing the screen: columns of stacked panes (- reads stdin)
- `--pane JSON`: One pane descriptor to add (repeatable); see above for the object shape
- `--shell NAME`: Target a registered shell by display name or tmux name
- `--project NAME`: Target a project's Workspaces surface (slug, basename, or path)
- `--sessions [ROW]`: Target the global Sessions surface (optional row by ID or display name)
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: applied (or every pane retargeted an existing one)
- `1`: state failure
- `2`: usage or validation error
- `3`: no running instance
- `4`: declined host-side; the reason names the first violation (off-screen, unfit, or the lease holder cannot receive pane requests)
- `5`: an unknown --project or --shell

**Examples:**

```bash
# read before you write
sidecar layout get --json
# a full layout: primary left, file over issue right
sidecar layout apply --spec '{"columns":[{"panes":[{"kind":"primary"}]},{"panes":[{"kind":"file","targets":["README.md"]},{"kind":"issue","targets":["td-756c34"]}]}]}'
# apply a spec from stdin
sidecar layout apply --spec - <layout.json
# add two panes, auto-placed
sidecar layout apply --pane '{"kind":"file","targets":["internal/palette/list.go:112","internal/palette/state.go"]}' --pane '{"kind":"shell","run":"make dev","name":"dev server"}'
# explicit cell, structured result
sidecar layout apply --pane '{"kind":"file","targets":["README.md"],"at":"2.1"}' --json
```

### `sidecar layout get`

Read the current pane layout

Read the pane layout of the surface showing this Sidecar shell: the grid
projection, every pane's kind, targets and tmux session, geometry, and the
caps and floors an apply would be held to.

--sessions addresses the global Sessions surface of a running instance
(optional ROW is a durable inventory ID, then a display name). It is
mutually exclusive with --shell and --project.

A layout that escapes the grid vocabulary reports "grid": null plus the raw
tree; it is still valid. Human output is a small ASCII sketch plus a table;
--json passes the payload through unchanged, which is the contract.

Unlike open, a layout request never queues: when this shell is not on
screen the request declines instead (exit 4), because a stale answer is
worse than a refusal.

From a Sidecar-managed pane on a host you are viewing, the JSON is that
viewer's grid for the matching Sessions row.

```
Usage: sidecar layout get [--json] [--sessions [ROW]]
```

**Options:**

- `--shell NAME`: Target a registered shell by display name or tmux name
- `--project NAME`: Target a project's Workspaces surface (slug, basename, or path)
- `--sessions [ROW]`: Target the global Sessions surface (optional row by ID or display name)
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms)
- `--json`: Write the layout payload itself to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: answered
- `1`: state failure
- `2`: usage error
- `3`: no running instance
- `4`: declined: the origin shell is not on screen, or the lease holder cannot receive pane requests
- `5`: an unknown --project or --shell

**Examples:**

```bash
sidecar layout get
# the machine contract: read before you write
sidecar layout get --json
# the selected row on the global Sessions surface
sidecar layout get --sessions --json
```

### `sidecar layout move`

Reposition one open pane

Move a pane that is already open to another place in the grid. The pane is
pulled out and grafted back at the destination: its content, tabs, scroll
position and any live terminal travel with it, and so does the share of the
box you dragged it to.

Name the pane to move by its grid cell (`2.1`, column.row, 1-based) or with
--focused. Addresses are read against the layout AS IT STANDS — run
`layout get` and use the cells it prints; you never compensate for the
source column collapsing.

--to takes three forms:

  1.2      a cell in the current grid. An occupied cell is an insert:
           the pane lands there and the occupant moves down.
  3        a column number. The pane lands at the BOTTOM of that column,
           and a number one past the last column opens a new one.
  left     the direction rule the modal's h/j/k/l use. up and down step
  right    one row within the column; left and right append at the bottom
  up       of the column beside this one, and open a new outer column when
  down     there is none — including a new leftmost column, which no cell
           address can name.

It all happens or nothing does. Caps (a 4x4 grid), per-kind floors, and a
window too small for the result decline with that reason and leave the
layout untouched. A move with nothing to do — the pane is already there, or
a direction with no room beyond it — is a SUCCESS reported as "unchanged",
never as moved and never as a refusal.

With --sessions this changes the pane tree of the Sessions viewer on THIS
machine, including for a row whose workspace lives on another one: no
layout mutation is sent to the remote host. The acknowledgement names the
surface it changed. Like get and apply, move never queues: off-screen,
or a lease holder that cannot receive pane requests, is exit 4.

```
Usage: sidecar layout move (CELL | --focused) --to (CELL | COLUMN | left|right|up|down) [--sessions [ROW]]
```

**Options:**

- `--focused`: Move the surface's focused pane instead of naming a cell
- `--to DEST`: Destination: a cell (1.2), a column (3), or left|right|up|down
- `--shell NAME`: Target a registered shell by display name or tmux name
- `--project NAME`: Target a project's Workspaces surface (slug, basename, or path)
- `--sessions [ROW]`: Target the global Sessions surface (optional row by ID or display name)
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: moved, or already in the requested place
- `1`: state failure
- `2`: usage error
- `3`: no running instance
- `4`: declined host-side; the reason names the refusal (off-screen, unfit, or the lease holder cannot receive pane requests)
- `5`: an unknown --project or --shell

**Examples:**

```bash
# read the cells before you move one
sidecar layout get --json
# put the pane at 2.1 below the one at 1.1
sidecar layout move 2.1 --to 1.2
# the direction rule the modal's l uses
sidecar layout move --focused --to right
# append to column 3, opening it one past the end
sidecar layout move 2.1 --to 3
# open a new leftmost column, structured result
sidecar layout move 1.1 --to left --json
# the global Sessions viewer's own layout
sidecar layout move --focused --to up --sessions
```

## `sidecar notify`

Configure, test, post, dismiss, and list Sidecar notifications

Sidecar's notification surface: a toast in the running instance, an entry in the
notification centre, and a count in the header until the user reads it.

```
Usage: sidecar notify <command>
```

### `sidecar notify config`

Show or change notification delivery configuration

Print resolved notification settings and defaults without changing the file. Use config set for global modes, quiet hours, and custom sound paths; use source set for per-source rules.

```
Usage: sidecar notify config [--json]
```

**Options:**

- `--json`: Write notification configuration as JSON
- `-h, --help`: Show this help

#### `sidecar notify config set`

Set notification delivery, quiet hours, custom sounds, and SSH delivery

Set one or more global notification settings. Modes are off, background, or always. Quiet hours are off or a local wall-clock range such as 22:00-08:00. Custom sound paths may be absolute, start with ~, or be relative to config.json; an empty --*-path= restores the built-in cue. SSH delivery has two independent switches, both off by default: --ssh-managed-hosts lets this machine deliver notifications forwarded by a registered remote host, and --ssh-terminal picks the outer terminal to notify through when Sidecar itself runs inside an SSH session. The complete prospective configuration is validated before write, preserves unrelated rules, and applies to running Sidecar instances without restart.

```
Usage: sidecar notify config set [options]
```

**Options:**

- `--native MODE`: Set system notifications: off, background, or always
- `--sound MODE`: Set sounds: off, background, or always
- `--quiet-hours RANGE`: Set off or local HH:MM-HH:MM (equal times mean all day)
- `--attention-path PATH`: Set the attention cue file; empty restores built-in
- `--done-path PATH`: Set the done cue file; empty restores built-in
- `--failure-path PATH`: Set the failure cue file; empty restores built-in
- `--ssh-managed-hosts on|off`: Deliver notifications forwarded by registered remote hosts
- `--ssh-terminal TERMINAL`: Set off, auto, ghostty, iterm2, wezterm, or kitty
- `--json`: Write the resulting notification configuration as JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: saved
- `1`: configuration I/O failure
- `2`: usage or validation error

**Examples:**

```bash
sidecar notify config set --native background --sound background
sidecar notify config set --quiet-hours 22:00-08:00 --json
sidecar notify config set --attention-path ~/Sounds/attention.wav
sidecar notify config set --ssh-managed-hosts on --json
sidecar notify config set --ssh-terminal ghostty
```

### `sidecar notify dismiss`

Dismiss a notification you posted

Dismiss one notification. A caller may only dismiss notifications it posted:
identity is the Sidecar shell you are in, or failing that the working directory,
so the notification you posted a moment ago is dismissible and the user's own
and other agents' are not.

```
Usage: sidecar notify dismiss [--json] <id>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: dismissed
- `1`: state failure
- `2`: usage error
- `3`: no notification with that id
- `4`: that notification was posted by someone else

**Examples:**

```bash
sidecar notify dismiss ntf-06215f4b1a2c3-9f1e2d3c
```

### `sidecar notify list`

List notifications

List notifications, newest first. This reads Sidecar's notification log directly,
so it answers whether or not Sidecar is running and never changes anything.

By default dismissed notifications are left out; --all includes them.

```
Usage: sidecar notify list [--all] [--unread] [--json]
```

**Options:**

- `--all`: Include dismissed notifications
- `--unread`: Only notifications the user has not seen
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: the notification log could not be read
- `2`: usage error

**Examples:**

```bash
sidecar notify list
sidecar notify list --unread --json
```

### `sidecar notify post`

Post a notification the user sees in Sidecar

Post a notification. It appears as a toast in the running Sidecar instance for
this shell's project and stays in the notification centre until dismissed.

With no instance running the notification is written to Sidecar's notification
log and appears at the next start; nothing is lost.

--expiry sets how long the toast stays on screen — a duration such as 10s, or
"never" for one that waits for the user. Expiry never removes the notification
from the centre.

--target attaches a call to action: the notification centre numbers targets 1-N
and the user jumps to one with enter or a digit. Repeat it for several, in the
order they should be numbered. The form is kind:value[:line][@project], where
kind is issue, task, commit, file, session or url; :line applies to files only;
and @project names another checkout by configured project name or by path, in
which case Sidecar switches projects and then lands. Ids written in the title or
body are still found by scanning — --target is for precision and for targets the
text does not spell out. A session target attaches a Sidecar-owned tmux
session — the sidecar-sh-… and sidecar-ws-… names shells and worktree agents
run under, which are also the only ones found by scanning. A task target opens
the Tasks tab.

```
Usage: sidecar notify post [options] <title>
```

**Options:**

- `--body TEXT`: Detail line shown under the title
- `--target SPEC`: Call to action, kind:value[:line][@project]; repeatable
- `--source ID`: Source: agent, waiting, session, tasks, td, system (default agent)
- `--expiry DURATION`: Toast lifetime (e.g. 10s), or "never" (default: the source's)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: posted, or stored for the next start
- `1`: state failure
- `2`: usage or validation error

**Examples:**

```bash
sidecar notify post "Tests are green"
sidecar notify post "Need a decision" --source waiting --expiry never
sidecar notify post "Build failed" --body "go test ./internal/app" --json
sidecar notify post "Review needed" --target issue:td-4c1f9a --target file:internal/app/model.go:42
sidecar notify post "Fixed upstream" --target issue:td-99aabb@braid
```

### `sidecar notify source`

Configure per-source notification rules

Inspect resolved rules with notify config; use source set for deterministic non-interactive mutation.

```
Usage: sidecar notify source <command>
```

#### `sidecar notify source set`

Set one notification source rule

Change one or more fields for a registered notification source. The same validation and targeted save boundary as Configuration preserves unrelated root keys and unknown future source entries, and the running app reloads the result without restart.

```
Usage: sidecar notify source set <source> [options]
```

**Options:**

- `--toast on|off`: Enable or disable the in-app toast
- `--native on|off`: Enable or disable system notifications for this source
- `--sound CUE`: Set none, event, attention, done, or failure
- `--expiry DURATION`: Set a Go duration or sticky
- `--json`: Write the resulting resolved source rule as JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: saved
- `1`: configuration I/O failure
- `2`: usage or validation error

**Examples:**

```bash
sidecar notify source set waiting --toast on --native on --sound attention --expiry sticky
sidecar notify source set td --native on --json
```

### `sidecar notify status`

Probe native and sound provider availability

Probe providers without sending a notification or changing configuration.

```
Usage: sidecar notify status [--json]
```

**Options:**

- `--json`: Write provider capabilities as JSON
- `-h, --help`: Show this help

**Exit codes:**

- `0`: probe completed
- `1`: output failure
- `2`: usage error

**Examples:**

```bash
sidecar notify status --json
```

### `sidecar notify test`

Explicitly test enabled notification channels

Exercise enabled providers without creating a notification-centre record. Explicit tests bypass foreground and quiet-hours suppression but still honor disabled channels and unavailable providers.

```
Usage: sidecar notify test --channel native|sound|all [--event waiting|done|failure] [--source SOURCE] [--json]
```

**Options:**

- `--channel CHANNEL`: Test native, sound, or all (required)
- `--event EVENT`: Use waiting, done, or failure (default waiting)
- `--source SOURCE`: Test the selected registered source rule (default follows event)
- `--json`: Write per-channel attempted/provider/delivered/error results
- `-h, --help`: Show this help

**Exit codes:**

- `0`: requested channels delivered
- `1`: provider or output failure
- `2`: usage error
- `3`: a requested channel was disabled or unavailable

**Examples:**

```bash
sidecar notify test --channel all --event waiting --json
sidecar notify test --channel native --source td --json
```

## `sidecar open`

Show a file, a td issue, a note, a git diff, a plugin resource, or a plugin collection in a split pane

Show a file, a td issue, a td note, a git diff, an external resource, or a plugin collection to the user as a
split pane in a Sidecar workspace. From a Sidecar shell this targets that shell.
Otherwise it targets the unique running instance, or a specific --shell / --project.
--sessions addresses the global Sessions surface of a running instance.
Pass --sessions=ROW for a durable inventory ID or display name; a following
bare word is the open target, not the row. Mutually exclusive with --shell
and --project.
--diff with no spec is the working tree. --plugin names a configured plugin instance:
with --collection it opens that collection's tab (add --query to open it searched,
--filter id=value for one of the collection's own declared filters, or a
positional row id to open that row's document instead), and without --collection it
opens a matched locator through the plugin's matchers. --provider is the older spelling
of the locator form and still works. Either way the instance is required for a resource:
a bare locator is never guessed at.
--split only overrides the split axis; it never halves a live terminal after content is open.
--at places the pane at an explicit grid cell and is a requirement: a kind whose open
would retarget an existing pane, or any cell that cannot be honored exactly, declines
rather than land elsewhere (--split expresses a preference; --at, a demand).

From a Sidecar-managed pane whose geometry lease is held by a connected viewer,
the open lands on that viewer's screen — not on a host TUI that may not be running.
There is no --host flag: routing is the lease. A relayed open never queues: if that
row is not on the viewer's screen, or the lease holder cannot receive pane requests
(disconnected, too old, or presence expired), the command declines (exit 4).

```
Usage: sidecar open [options] [<target>]
```

**Targets:**

- `path`: A file inside the target workspace, optionally "path:line"
- `td-xxxxxx`: A td issue id
- `sidecar://note/nt-xxxx`: A td note, opened as a read-only pane
- `--diff`: Working-tree diff (wt); add a spec for a commit or range
- `spec`: A git commit or range (abc1234, A..B); --diff accepts HEAD and branch names
- `locator`: With --plugin (or --provider), a resource key such as CASH-1245
- `row id`: With --plugin and --collection, one row of that collection

**Options:**

- `--line N`: Line to reveal (alternative to "path:line")
- `--diff`: Open a Diff leaf (working tree if no spec)
- `--plugin ID`: Open through a configured plugin instance (collection tab with --collection, otherwise a matched locator)
- `--provider ID`: Alias for --plugin's locator form, kept for the frozen resource protocol
- `--collection C`: With --plugin, the collection to open as a tab
- `--query Q`: With --collection, the query the tab opens searched on
- `--filter ID=VALUE`: With --collection, one applied filter (repeatable)
- `--shell NAME`: Target a registered shell by display name or tmux name
- `--project NAME`: Target a project's Workspaces surface (slug, basename, or path)
- `--sessions [=ROW]`: Target the global Sessions surface (optional row as --sessions=ID)
- `--split auto|right|below`: Where to place a new pane (default auto)
- `--at COL[.ROW]`: Place at an explicit grid cell (1-based); a requirement, mutually exclusive with --split
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: opened or queued
- `1`: state failure
- `2`: usage or validation error
- `3`: no running instance, or several running with no target
- `4`: an instance declined (too small to split, row not on screen, or the lease holder cannot receive pane requests)
- `5`: an unknown --project or --shell

**Examples:**

```bash
# file, in a split beside the terminal
sidecar open internal/cli/cli.go
# file at a line
sidecar open internal/cli/cli.go:88
# td issue
sidecar open td-348d88
# td note pane
sidecar open sidecar://note/nt-4jdj4e
# working-tree Diff leaf
sidecar open --diff
# that commit, not the working tree
sidecar open --diff HEAD
# commit, unless a file of that name exists
sidecar open abc1234
# resource pane for that provider's locator
sidecar open --provider jira-work CASH-1245
# a collection tab beside the terminal, opened searched
sidecar open --plugin recall --collection results --query dex --split right
# the same tab, scoped by one of the collection's own filters
sidecar open --plugin recall --collection results --query dex --filter profile=docs
# one row's document tab
sidecar open --plugin ongoing --collection projects recall
# structured result for the agent
sidecar open --json --split below README.md
# explicit cell: second column, top row
sidecar open README.md --at 2.1
# from any terminal, that project's Workspaces surface
sidecar open --project sidecar README.md
# the selected row on the global Sessions surface
sidecar open --sessions README.md
```

## `sidecar plugin`

Inspect and configure the plugins Sidecar hosts

Inspect and configure the plugins Sidecar hosts. A plugin is either embedded
(compiled into Sidecar, with its own UI) or external: an explicitly configured
executable that answers JSON on stdout and that Sidecar renders itself.

An external plugin speaks one of two protocols, decided by the config section
it is written in and never by the executable. plugins.external entries speak
sidecar.plugin/v1-draft, which has describe, resolve, list, get, and act;
terminalResources.providers entries speak the frozen
sidecar.terminal-resource/v1, which has describe and resolve. The
`sidecar terminal-links` verbs remain the surface for that older section.

The draft protocol is behind the plugin_protocol feature flag. Turn it on
with `sidecar --enable-feature=plugin_protocol`, or set
features.flags.plugin_protocol.

Writing one: docs/guides/active/creating-plugins.md is the walkthrough, from
choosing a class to a plugin that passes `plugin check`, with a complete
example under docs/guides/examples/hello-plugin/. The contract itself is
docs/reference/plugin-protocol.md.

```
Usage: sidecar plugin <command> [options]
```

### `sidecar plugin list`

List the plugins Sidecar can host

List every plugin Sidecar knows about: the embedded ones in the order the
header paints them, then every external plugin configured under
plugins.external and terminalResources.providers.

Each row reports the plugin's class (who renders it), scope (project plugins
are rebuilt on a project switch, global ones are built once), the placements
its content can occupy, and whether it is enabled. An external row also
reports the config section it was read from.

Enablement is plugins.<id>.enabled. Two deprecated feature flags, tasks_plugin
and notes_plugin, still answer for their plugin while that key is absent. A
plugin that is enabled but whose feature flag is off is reported inactive,
naming the flag.

Without --describe this reads configuration and runs nothing: no running
Sidecar, no PATH lookup, no subprocess. --describe opts in to running each
active external plugin's describe method, with the same environment, working
directory, and timeout the app uses.

```
Usage: sidecar plugin list [--describe] [--json]
```

**Options:**

- `--describe`: Run describe on each active external plugin
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: configuration read failure
- `2`: usage error

**Examples:**

```bash
sidecar plugin list
sidecar plugin list --json
sidecar plugin list --describe --json
```

### `sidecar plugin check`

Describe one external plugin, and optionally call it

Answer "is this plugin configured, startable, and speaking the protocol",
using the exact base environment, working directory, and timeouts the app
uses. This is the authoring surface; it is not a replacement for the
plugin's own CLI.

describe always runs. --list and --get are separate, explicit flags because
they can perform network access and print private data; neither is ever
implied. --query applies only to --list, and a collection whose search is
required needs one. --filter applies only to --list too, is repeatable, and
takes id=value naming a filter the collection declared; what is printed back
is what the host actually sent, so a key that was dropped shows as dropped.

Only what the host kept is printed, never the plugin's raw stdout: every
string shown has been through the host's own sanitization and bounds, so what
you see is what a pane would draw.

```
Usage: sidecar plugin check [--list COLLECTION [--query Q]] [--get COLLECTION ID] [--json] <id>
```

**Options:**

- `--list COLLECTION`: Also call list on this collection
- `--query TEXT`: Query to send with --list
- `--filter ID=VALUE`: Apply one declared filter with --list (repeatable)
- `--get COLLECTION ID`: Also call get on this collection row (two values)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the plugin answered every requested call
- `1`: a call failed
- `2`: usage error
- `3`: no plugin with that id is configured
- `4`: the governing feature flag is off

**Examples:**

```bash
sidecar plugin check recall
sidecar plugin check recall --list results --query dex --json
sidecar plugin check recall --list results --query dex --filter profile=docs
sidecar plugin check recall --get results rc:notes:1 --json
```

### `sidecar plugin call`

Call one protocol method with the host's envelope

Run one method — describe, resolve, list, get, or act — through the host's own
envelope, validation, and sanitization, and print what the host would have
kept. It is the authoring loop for a plugin: write a response, call it, see
exactly what survives.

--params is the method's params object as JSON:
  resolve  {"matcher":"issue-key","locator":"CASH-1"}
  list     {"collection":"results","query":"dex","filters":{"profile":"docs"},"cursor":"","limit":100}
  get      {"collection":"results","id":"rc:notes:1"}
  act      {"action":"log-note","collection":"results","id":"rc:notes:1","inputs":{"text":"…"}}

list first runs describe, because the declared columns are what a page is
sanitized against — a cell keyed by an undeclared column is dropped, and that
is a finding worth seeing here rather than in a pane. The same is true of
filters: --filter id=value is shorthand for a key inside params.filters, and
a key the collection never declared, or a value equal to that filter's own
default, is dropped before the plugin is called.

No host context is sent: this process has no surface, so it has no project
and no selection to offer.

```
Usage: sidecar plugin call [--params JSON] [--json] <id> <method>
```

**Options:**

- `--params JSON`: The method's params object
- `--filter ID=VALUE`: Apply one declared filter to list (repeatable)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the plugin answered
- `1`: the call failed
- `2`: usage error
- `3`: no plugin with that id is configured
- `4`: the governing feature flag is off

**Examples:**

```bash
sidecar plugin call recall describe --json
sidecar plugin call recall list --params '{"collection":"results","query":"dex"}' --json
sidecar plugin call recall list --params '{"collection":"results","query":"dex"}' --filter profile=docs --json
sidecar plugin call dex act --params '{"action":"log-note","collection":"people","id":"p:ada","inputs":{"text":"hi"}}' --json
```

### `sidecar plugin add`

Configure an external plugin

Append one entry to plugins.external. This is the whole install flow: Sidecar
never scans a directory, never runs every sidecar-* binary on PATH, never
auto-enables anything, and never lets a repository declare a plugin.

Everything after --command is the argv, executed directly with no shell. Put
it last.

Nothing is started: add prints exactly what will run — every argv element on
its own line, the working directory, and the variables that will be passed by
name — and asks for confirmation. --yes skips the question, which is what a
script or an agent uses.

A process boundary is crash isolation, not a sandbox. Configuring a plugin
trusts that executable with your full OS privileges.

```
Usage: sidecar plugin add [--pass-env NAME]... [--scope global] [--placement tab|panes]... [--timeout DURATION] [--claim-host HOST]... [--disabled] [--yes] [--json] <id> --command ARGV...
```

**Options:**

- `--command ARGV...`: The argv to run; everything after it is part of the command
- `--pass-env NAME`: Pass this variable's current value through (repeatable, names only)
- `--scope SCOPE`: Lifecycle: global (the only value this version supports)
- `--placement WHERE`: tab or panes (repeatable; default both)
- `--timeout DURATION`: Per-call timeout, clamped to [1s, 60s]
- `--claim-host HOST`: Hostname whose URLs this plugin may claim (repeatable)
- `--disabled`: Write the entry turned off
- `-y, --yes`: Skip the confirmation
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the entry was written, or the confirmation was declined
- `1`: the configuration could not be written
- `2`: usage error, or the entry was refused by validation
- `4`: plugin_protocol is off

**Examples:**

```bash
sidecar plugin add recall --yes --command recall sidecar-plugin
sidecar plugin add dex --pass-env DEX_PROFILE --placement panes --yes --command dex sidecar-plugin
```

### `sidecar plugin remove`

Remove an external plugin's configuration

Delete one entry from plugins.external. Unknown config sections are preserved,
and removing the last entry removes the key rather than leaving it empty.

An entry in terminalResources.providers is not removed here: that section
belongs to the frozen resource protocol, and the message says so.

```
Usage: sidecar plugin remove [--json] <id>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the configuration was written
- `1`: the configuration could not be written
- `2`: usage error
- `3`: no plugin with that id is configured
- `4`: plugin_protocol is off, or the entry is in a section this verb does not own

**Examples:**

```bash
sidecar plugin remove recall
```

### `sidecar plugin enable`

Turn an external plugin on

Set enabled:true on the plugins.external entry.

Enablement is read at startup, so a running Sidecar needs a restart.

```
Usage: sidecar plugin enable [--json] <id>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the configuration was written
- `1`: the configuration could not be written
- `2`: usage error
- `3`: no plugin with that id is configured
- `4`: plugin_protocol is off, or the entry is in a section this verb does not own

**Examples:**

```bash
sidecar plugin enable recall
```

### `sidecar plugin disable`

Turn an external plugin off

Set enabled:false on the plugins.external entry. The entry is kept, so turning
it back on needs no argv.

Enablement is read at startup, so a running Sidecar needs a restart.

```
Usage: sidecar plugin disable [--json] <id>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the configuration was written
- `1`: the configuration could not be written
- `2`: usage error
- `3`: no plugin with that id is configured
- `4`: plugin_protocol is off, or the entry is in a section this verb does not own

**Examples:**

```bash
sidecar plugin disable recall
```

### `sidecar plugin changed`

Tell running Sidecar instances a plugin's data moved

Write one request onto the bus saying that a plugin's data changed. Every
running instance re-lists the visible tabs of that plugin; a tab nobody is
looking at costs nothing, so this is safe from a shell hook.

It is the poke for a change no declared watch path would catch. A plugin
whose store is one file should declare it under refresh.watch instead and
need no hook at all.

--collection narrows the refresh to one collection. Omit it when the tool
does not know what it touched.

This starts no plugin and reads no configuration: it neither knows nor cares
whether the id names a configured plugin, because only a running instance
has the tabs that would answer.

```
Usage: sidecar plugin changed [--collection C] [--json] <id>
```

**Options:**

- `--collection C`: Narrow the refresh to one collection
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the request was written
- `1`: the request could not be written
- `2`: usage error

**Examples:**

```bash
sidecar plugin changed dex --collection people
sidecar plugin changed recall --json
```

## `sidecar project`

Inspect, add, edit, and switch Sidecar projects

Manage Sidecar's configured projects and switch between project workspaces.

A Sidecar-managed shell is born in a project and cannot change projects for its
lifetime. `sidecar project switch` and `add --switch` change what the running
Sidecar TUI displays to the user; work in the new project occurs in a newly created
shell (`sidecar create shell --project`).

```
Usage: sidecar project <command>
```

### `sidecar project add`

Add a new project to Sidecar

Add a project to Sidecar's configuration.

The directory specified by --path must already exist and will not be created.
Adding a project does not initialize a Git repository or a td project.

With --switch, also switch the running Sidecar TUI to the new project immediately
after writing configuration. If no Sidecar instance is running, add still succeeds.
A landing shell is a separate `sidecar create shell --project` command.

```
Usage: sidecar project add <name> --path PATH [--theme NAME] [--open-in APP] [--switch] [--json]
```

**Options:**

- `--path PATH`: Directory path for the project (required)
- `--theme NAME`: Set a project-specific theme override
- `--open-in APP`: Set the default editor or IDE to open this project in
- `--switch`: Switch the running Sidecar TUI to the new project after adding
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: added (and switched, if requested and Sidecar is running)
- `1`: configuration I/O failure
- `2`: usage error (missing name or --path)
- `5`: a value was rejected (path does not exist, not a directory, or project name/path already exists)

**Examples:**

```bash
sidecar project add "vacuum-simulator" --path ~/code/vacuum-simulator
sidecar project add "vacuum-simulator" --path ~/code/vacuum-simulator --switch
sidecar project add "vacuum-simulator" --path ~/code/vacuum-simulator --theme "Catppuccin Mocha" --json
```

### `sidecar project current`

Print the calling shell's project and the visible project

Print the Sidecar project owning this shell and the project currently visible in
the running Sidecar TUI.

Human output names the shell's project first and mentions the visible one when
it differs. JSON reports both and whether they are aligned.

```
Usage: sidecar project current [--json]
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success (even when no TUI is running)
- `1`: not a managed shell and current directory is not a configured project
- `2`: usage error

**Examples:**

```bash
sidecar project current
sidecar project current --json
```

### `sidecar project list`

List configured Sidecar projects

List all Sidecar projects from configuration in list order. Marks the project
owning this shell and the project currently visible in the running Sidecar TUI.

This reads configuration directly and does not require a managed shell or a
running Sidecar instance.

```
Usage: sidecar project list [--json]
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: configuration read failure
- `2`: usage error

**Examples:**

```bash
sidecar project list
sidecar project list --json
```

### `sidecar project remove`

Remove a project from Sidecar's configuration

Remove a project from Sidecar's configuration.

--yes is required. Removing a project does not delete the project directory,
its Git repository, or its session history. If the project is currently visible
in a running Sidecar instance, removal is refused; switch to another project
with `sidecar project switch` first.

```
Usage: sidecar project remove <name> --yes [--json]
```

**Options:**

- `--yes`: Confirm removal (required for non-interactive safety)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: removed
- `1`: configuration I/O failure
- `2`: usage error (missing --yes)
- `3`: ambiguous project name
- `5`: a value was rejected (unknown project, or project is currently visible in Sidecar)

**Examples:**

```bash
sidecar project remove "vacuum-simulator" --yes
sidecar project remove "vacuum-simulator" --yes --json
```

### `sidecar project set`

Update configuration for an existing project

Change settings for an existing project in Sidecar's configuration. At least one
setting flag is required.

Path changes must point to an existing directory and re-validate uniqueness.
Editing a project notifies running Sidecar instances without switching the visible
project.

```
Usage: sidecar project set <name> [--name NEW] [--path PATH] [--theme NAME] [--open-in APP] [--clear-theme] [--json]
```

**Options:**

- `--name NEW`: Rename the project
- `--path PATH`: Change the project directory path
- `--theme NAME`: Set or change the project theme
- `--clear-theme`: Remove project theme override (use global theme)
- `--open-in APP`: Set the default editor or IDE to open this project in
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: updated
- `1`: configuration I/O failure
- `2`: usage error (no change flags specified or conflicting flags)
- `3`: ambiguous project name
- `5`: a value was rejected (unknown project, path does not exist, or name already taken)

**Examples:**

```bash
sidecar project set "vacuum-simulator" --name "Vacuum Sim"
sidecar project set "vacuum-simulator" --theme "Nord"
sidecar project set "vacuum-simulator" --clear-theme
```

### `sidecar project switch`

Switch the running Sidecar TUI to a project

Switch the running Sidecar TUI to another configured project.

This changes what the user is looking at in the Sidecar window; it does not move
or retarget the calling shell. Unlike `sidecar open`, switch is an intentional
view change that updates visible project context and restores the last active
worktree for that project.

```
Usage: sidecar project switch <name> [--wait DURATION] [--json]
```

**Options:**

- `--wait DURATION`: Time to wait for instance to acknowledge (default 1200ms; 0 = fire and forget)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: switched or already showing project
- `1`: state or communication failure
- `2`: usage error
- `3`: no running Sidecar instance, or multiple instances running
- `4`: running instance declined the switch
- `5`: unknown project

**Examples:**

```bash
sidecar project switch vacuum-simulator
sidecar project switch "vacuum-simulator" --json
```

## `sidecar repo`

Read-only repository contract a viewing Sidecar invokes on a host

Read one machine's git repository state — status, patches, history, commits, and refs — for a viewing Sidecar, over the existing host request seam.

This is not a git CLI and must not be adopted as one. Sidecar does not own git: an agent that wants to stage a file, commit, or push runs `git`.
These verbs exist because a viewing Sidecar needs one machine's repository state in one round trip, normalized to the model its panes already render.
Every verb is non-interactive, read-only, workspace-scoped, and strictly enumerated. There is no write path here, and adding one is a separate decision with its own confirmation and credential questions.

```
Usage: sidecar repo <command>
```

### `sidecar repo commit`

Read one commit's metadata and file list

Read one commit's subject, body, author, date, parent hashes, merge flag, and file list with per-file status and +/- counts.

This reads a commit. It does not create one: `sidecar repo` has no write path of any kind.
A merge's file list is its diff against the first parent, because git's combined diff for a clean merge lists nothing.
A workspace that is not a git repository answers with noRepository rather than failing, so a viewer can say so instead of offering to initialize one on the wrong machine.

--json writes the machine contract.

```
Usage: sidecar repo commit --workspace ID --commit HASH [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--commit HASH`: Commit object name
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: read, including a workspace that is not a git repository
- `1`: internal or load failure
- `2`: usage error
- `5`: value rejected: unknown workspace, containment, or an unknown commit

**Examples:**

```bash
sidecar repo commit --workspace /home/me/api:worktree:/home/me/api --commit 4f2b91c8ab --json
```

### `sidecar repo diff`

Read one raw unified patch for one path

Read one file's patch from a durable workspace identity on this machine, as raw unified diff text.

--mode is required and never inferred. A staged and an unstaged change to the same path are two different patches, and answering with the wrong one is a quiet, plausible lie about the host's working tree.
--mode commit needs --commit and returns that commit's patch for the path; a merge is diffed against its first parent, because git's combined diff for a clean merge is empty.
--mode untracked renders a file git does not track as the addition it is.
The patch is text and is not parsed here: the viewer runs the same parser it runs on a local patch, so a host upgrade is never a rendering change.
A patch over 512KiB is cut and says so rather than being returned short and silent.
A workspace that is not a git repository answers with noRepository rather than failing, so a viewer can say so instead of offering to initialize one on the wrong machine.

--json writes the machine contract.

```
Usage: sidecar repo diff --workspace ID --path REL --mode staged|unstaged|untracked|commit [--commit HASH] [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--path REL`: File path relative to the workspace root
- `--mode MODE`: Which patch: staged, unstaged, untracked, or commit
- `--commit HASH`: Commit object name, required for --mode commit
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: read, including a workspace that is not a git repository
- `1`: internal or load failure
- `2`: usage error
- `5`: value rejected: unknown workspace, containment, or an unknown commit

**Examples:**

```bash
sidecar repo diff --workspace /home/me/api:worktree:/home/me/api --path internal/cli/repo.go --mode unstaged --json
sidecar repo diff --workspace /home/me/api:worktree:/home/me/api --path README.md --mode commit --commit 4f2b91c --json
```

### `sidecar repo history`

Read one page of the commit log

Read commit rows — hash, short hash, subject, author, author date, parent hashes, and pushed state — newest first.

History is paged, not walked. --cursor is the hash of the previous page's last row, not an offset: an offset silently repeats or skips a commit when the host commits between two pages.
nextCursor is empty when the page reached the end of the log.
The host caps one page at 500 rows regardless of --limit, because the host is the machine that would pay for serializing an entire log.
--author and --path filter on the host. Subject search stays with the viewer, which runs it over the rows it already has.
Pushed state is asked of git for this page's commits alone, so a branch far ahead of its upstream cannot make the answer depend on a cap.
A workspace that is not a git repository answers with noRepository rather than failing, so a viewer can say so instead of offering to initialize one on the wrong machine.

--json writes the machine contract.

```
Usage: sidecar repo history --workspace ID [--limit N] [--cursor HASH] [--author TEXT] [--path REL] [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--limit N`: Rows in this page (default 100, host maximum 500)
- `--cursor HASH`: Continue after this commit: the previous page's last hash
- `--author TEXT`: Only commits whose author matches this text
- `--path REL`: Only commits touching this path, relative to the workspace root
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: read, including a workspace that is not a git repository
- `1`: internal or load failure
- `2`: usage error
- `5`: value rejected: unknown workspace, containment, or an unknown commit

**Examples:**

```bash
sidecar repo history --workspace /home/me/api:worktree:/home/me/api --limit 50 --json
sidecar repo history --workspace /home/me/api:worktree:/home/me/api --cursor 4f2b91c8ab --json
```

### `sidecar repo refs`

List local and remote branches and the stash

List local branches with their upstream and ahead/behind counts, remote-tracking branches, and the stash entries.

Listing only. A viewer bound to this host shows the branches and refuses to switch to one.
A workspace that is not a git repository answers with noRepository rather than failing, so a viewer can say so instead of offering to initialize one on the wrong machine.

--json writes the machine contract.

```
Usage: sidecar repo refs --workspace ID [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: listed, including a workspace that is not a git repository
- `1`: internal or load failure
- `2`: usage error
- `5`: value rejected: unknown workspace, containment, or an unknown commit

**Examples:**

```bash
sidecar repo refs --workspace /home/me/api:worktree:/home/me/api --json
```

### `sidecar repo status`

Read one repository's branch, upstream, state, and changed files

Read the current branch, upstream ref, ahead/behind counts, detached-HEAD flag, in-progress state, origin's URL, stash count, and the changed-file rows for a durable workspace identity on this machine.

One call, one instant: branch and file state come from a single `git status --porcelain=v2 --branch`, so a viewer never renders a branch from one moment beside files from another.
Each changed-file row carries the staged, unstaged, and untracked senses for that path, with the +/- counts of each sense, because a path staged and then edited again is one file with two patches.
In-progress state is one of merge, rebase, cherry-pick, revert, or bisect; empty is an ordinary working tree.
A workspace that is not a git repository answers with noRepository rather than failing, so a viewer can say so instead of offering to initialize one on the wrong machine.

--json writes the machine contract.

```
Usage: sidecar repo status --workspace ID [--json]
```

**Options:**

- `--workspace ID`: Unscoped durable workspace id (projectKey:shell:name or projectKey:worktree:path)
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: read, including a workspace that is not a git repository
- `1`: internal or load failure
- `2`: usage error
- `5`: value rejected: unknown workspace, containment, or an unknown commit

**Examples:**

```bash
sidecar repo status --workspace /home/me/api:worktree:/home/me/api --json
```

## `sidecar request`

Host-side UI request bus verbs a viewing Sidecar invokes

Acknowledge UI requests into this machine's request bus.

This is an internal transport endpoint, not a public open-on-host surface.

```
Usage: sidecar request <command>
```

### `sidecar request ack`

Write one acknowledgement into a host request's *.acks directory

Write an acknowledgement for a UI request file this machine already holds.

This is the mutation seam a viewing Sidecar uses to ack a relayed open or layout
request into the host *.acks directory. It is not a public targeting flag and not
a serve write: serve still does not write acks or apply requests.

--json writes the machine contract.

```
Usage: sidecar request ack --id ID --action open|layout --status STATUS [--reason TEXT] [--surface TEXT] [--pane N] [--layout JSON] [--items JSON] --json
```

**Options:**

- `--id ID`: Request id to acknowledge
- `--action ACTION`: Request action (open or layout)
- `--status STATUS`: Ack status (opened, declined, retargeted, queued, moved, unchanged)
- `--reason TEXT`: Decline or no-op reason
- `--surface TEXT`: Surface that handled the request
- `--pane N`: Pane id that received the open, when any
- `--layout JSON`: Layout get report to store on the ack
- `--items JSON`: Layout apply/move per-pane verdicts to store on the ack
- `--json`: Write the structured result object to stdout (required for the machine contract)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: acknowledged
- `1`: state failure
- `2`: usage error

**Examples:**

```bash
sidecar request ack --id req-1 --action open --status opened --json
```

## `sidecar session`

Inspect and perform cold restore of managed shells after a tmux restart

A tmux server restart destroys every managed shell's processes; tmux exposes no way to hand a live PTY to a replacement server, so Sidecar reconstructs rather than preserves. These commands read what is reconstructible, do the reconstruction, and decide per shell how far it should go.

Shell records are never deleted by a restore, a restore failure, or a tmux server death. A shell that cannot be restored stays as a visible row with a reason.

```
Usage: sidecar session <command>
```

### `sidecar session policy`

Read or set one shell's cold-restore policy

With no policy flag, reports the effective policy for the target shell.

  --inherit  follow the machine default (plugins.workspace.sessionRestore)
  --shell    recreate the terminal, but never resume its agent
  --resume   recreate the terminal and resume its exact conversation
  --never    leave this shell out of restore entirely

This is per shell, so a long-running server, a disposable helper, and a sensitive agent session can differ without changing the machine default. Omitting TARGET inside a managed shell targets that shell.

```
Usage: sidecar session policy [TARGET] [--shell|--resume|--never|--inherit] [--json]
```

**Options:**

- `--inherit`: Follow the machine default
- `--shell`: Recreate the shell, never resume the agent
- `--resume`: Recreate the shell and resume the exact conversation
- `--never`: Never restore this shell
- `--project NAME`: Target project (slug, basename, or path)
- `--json`: Write the stable structured document to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: Success
- `1`: Reading state or talking to tmux failed
- `2`: Usage error
- `5`: The request was refused: confirmation is required, the policy value is unknown, or the target is not a managed shell

**Examples:**

```bash
# Read this shell's policy
sidecar session policy
# Never resume this agent automatically
sidecar session policy reviewer --shell
# Always resume this one
sidecar session policy reviewer --resume
```

### `sidecar session restore`

Recreate managed shells, and optionally resume their exact conversations

Executes the plan `session status` prints.

Shells are recreated under their own tmux session names and existing working directories; no --run command, dev server, or test watcher is ever replayed. A missing working directory is a refusal, never a fallback to another directory, and a tmux session name held by something else is a refusal too — Sidecar never closes a live session to take its name.

Conversations are resumed only with --agents, only from an exact reference an official integration reported, and only when the policy allows it. Under the default ask policy a non-interactive resume additionally requires --yes.

The tmux session name is the idempotency key, so running this twice does not produce two shells or two agents, and a run interrupted at any point converges when it is run again. Nothing here ever deletes a shell record: a failure is reported and left retryable.

```
Usage: sidecar session restore [--dry-run] [--shell TARGET] [--agents] [--yes] [--host ID] [--json]
```

**Options:**

- `--dry-run`: Print the plan and exit without creating or starting anything
- `--shell TARGET`: Restore only this shell, by tmux session name or display name
- `--agents`: Also resume eligible exact agent conversations
- `--yes`: Confirm agent resumes non-interactively when the policy is ask
- `--host ID`: Restore on a registered remote host instead of this machine
- `--json`: Write the stable structured document to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: Success
- `1`: Reading state or talking to tmux failed
- `2`: Usage error
- `5`: The request was refused: confirmation is required, the policy value is unknown, or the target is not a managed shell

**Examples:**

```bash
# Recreate eligible shells, no agents
sidecar session restore
# See exactly what would happen first
sidecar session restore --agents --dry-run
# Recreate one shell and resume its conversation
sidecar session restore --shell reviewer --agents --yes
```

### `sidecar session status`

Report what a cold restore would do, without doing it

Reads Sidecar's managed shell records and the current tmux inventory and prints the ordered restore plan.

Every managed shell is named as reattach, recreate-shell, resume-agent, manual, skip, or refuse, with the reason and whether performing it would run an agent process. This command is read-only: it creates nothing, starts nothing, and does not require a running Sidecar.

```
Usage: sidecar session status [--host ID] [--json]
```

**Options:**

- `--host ID`: Read the plan on a registered remote host instead of this machine
- `--json`: Write the stable structured document to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: Success
- `1`: Reading state or talking to tmux failed
- `2`: Usage error
- `5`: The request was refused: confirmation is required, the policy value is unknown, or the target is not a managed shell

**Examples:**

```bash
# See whether the last tmux server was replaced and what is restorable
sidecar session status
# Read the plan as JSON
sidecar session status --json
```

## `sidecar setup`

Start Sidecar with Configuration open on Sidecar Setup

Start Sidecar normally, with Configuration open on the Sidecar Setup page.
Setup lists what is left to do — add a project, install tmux, connect agent
instructions — and opens a focused repair for each one.

This is a launch route, not a second settings interface: it renders nothing in
the terminal and changes nothing on its own. Sidecar's ordinary options still
apply (sidecar setup -project /path). Escape returns to the surface underneath,
and the header gear reopens Configuration at any time.

If startup fails before Sidecar can draw — a malformed config file, a terminal
that is not interactive — it exits nonzero with the specific next step and a
support path that uploads nothing.

```
Usage: sidecar setup [options]
```

**Options:**

- `-h, --help`: Show this help

**Exit codes:**

- `0`: Sidecar ran and exited normally
- `1`: startup failed before the first frame
- `2`: usage error

**Examples:**

```bash
sidecar setup
# that project's Setup
sidecar setup -project ~/code/myproject
```

## `sidecar shell`

Manage Sidecar shell records and the current shell's name

List, forget, restore, and delete this project's shells; read or rename a shell; and send a command into one.

```
Usage: sidecar shell <command>
```

### `sidecar shell delete`

Delete a shell: close its tmux session and forget its record

Delete a Sidecar-managed shell. This closes the tmux session and moves the
record to a tombstone, which is exactly what Delete does in the Sessions
browser — the same workspaceops call, so the two cannot drift.

`sidecar shell forget` is the half of this that only drops the record; use it
for a shell whose session is already gone, or one recorded on another tmux
server. Either way `sidecar shell restore` can put the record back.

--target is required and must name a session the resolved project owns: a
sidecar-sh-… record in its shells.json. tmux resolves a session name against
whatever answers to it, so an unregistered name is refused (exit 3) rather
than killed. A sidecar-ws-… worktree session resolves but is refused (exit 5):
removing a checkout carries branch-cleanup decisions this verb cannot express.

A sidecar-tp-… target is different: it is a beside-the-session terminal split
(`create shell`'s default placement, or --split), never a managed shell, so it
has no record to tombstone — this is the only CLI path that closes one, and
--shell/--project are refused alongside it since neither applies.

There is no current-shell form. Deleting the shell you are sitting in would
kill the session running the command, so the subject is always named.

```
Usage: sidecar shell delete --target SESSION [--project NAME] [--json]
```

**Options:**

- `--target SESSION`: The tmux session to delete (required)
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: deleted
- `1`: tmux, ambiguity, or state failure
- `2`: usage error, including a missing --target
- `3`: --target names no session this project owns (or, for a sidecar-tp-… split, no live session by that name), or one recorded on a different tmux server
- `5`: a value was rejected: --target names a worktree, or an unknown --project / --shell

**Examples:**

```bash
sidecar shell delete --target sidecar-sh-sidecar-2
sidecar shell delete --target sidecar-sh-sidecar-2 --project sidecar --json
```

### `sidecar shell forget`

Forget a shell record by tmux name

Forget a Sidecar-managed shell record in the current project. The definition
moves to a tombstone so `sidecar shell restore` can put it back; the tmux
session is not started or killed.

A name that is already forgotten is already in that state (exit 0). A name
that is in neither the live list nor the tombstones is not found (exit 1).

A forgotten record stays restorable for 14 days by default; set
shells.tombstoneRetention in config.json ("30d", "336h", or "forever") to
change the window.

```
Usage: sidecar shell forget [--json] <tmux-name>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: forgotten, or already forgotten
- `1`: not found, or state failure
- `2`: usage error
- `5`: an unknown --project or --shell

**Examples:**

```bash
sidecar shell forget sidecar-sh-sidecar-1
sidecar shell forget --json sidecar-sh-sidecar-1
```

### `sidecar shell list`

List this project's shell records

List Sidecar-managed shell records for the current project. Live records
are listed first, then forgotten ones, so either surface can restore a record
by tmux name.

This reads shells.json directly; it does not start or inspect tmux sessions.

```
Usage: sidecar shell list [--json]
```

**Options:**

- `--json`: Write one structured result object to stdout
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: state failure
- `2`: usage error
- `5`: an unknown --project or --shell

**Examples:**

```bash
sidecar shell list
sidecar shell list --json
```

### `sidecar shell name`

Print the current shell's display name

Print the Sidecar display name of the managed shell or worktree agent containing
this command. Reads registered Sidecar state (authoritative), not the agent SDK
or $SIDECAR_SHELL_NAME, so reopening another agent in place keeps its context.

Human output is the display name alone, one line, for easy scripting.
JSON includes the stable tmux session id and display name.

```
Usage: sidecar shell name [--json]
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: identity or state failure
- `2`: usage error

**Examples:**

```bash
sidecar shell name
sidecar shell name --json
```

### `sidecar shell rename`

Rename the current shell, or one named with --target

Rename the Sidecar-managed shell or worktree agent containing this command, or
with --target, one you are not sitting in. This changes Sidecar's display name;
it does not rename the tmux session, Git branch, or worktree directory.

The current display name is also published as $SIDECAR_SHELL_NAME. "Shell 3"
is the unset default; a previous task's name is equally stale — rename when
the name no longer describes the work in this shell.

--target takes a tmux session name: a sidecar-sh-… record from `sidecar shell
list`, or a sidecar-ws-… worktree agent. The session must belong to the resolved
project (--project, or the project this directory is in) — a name Sidecar does
not own is refused rather than renamed. --shell and --project only scope a
--target; without one, the current shell is the only subject.

```
Usage: sidecar shell rename [--target SESSION [--project NAME]] [--json] <display-name>
```

**Options:**

- `--target SESSION`: Rename this tmux session instead of the current shell
- `--shell NAME`: Resolve the project from a registered shell (with --target)
- `--project NAME`: Target project (slug, basename, or path; with --target)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: renamed, or already named that
- `1`: identity, ambiguity, or state failure
- `2`: usage error; without --target, also a rejected display name (the current-shell form's long-standing code)
- `3`: --target names no session this project owns
- `5`: with --target: a value was rejected — the display name (already used, or not legal), or an unknown --project / --shell

**Examples:**

```bash
sidecar shell rename "shell rename implementation"
sidecar shell rename --target sidecar-sh-sidecar-2 --json "release prep"
sidecar shell rename --project sidecar --target sidecar-ws-sidecar-fix-auth "fix auth"
```

### `sidecar shell restore`

Restore a forgotten shell record by tmux name

Restore a forgotten Sidecar-managed shell record in the current project.
Display name, agent type, skip-perms, and working directory come back with it.
The tmux session is not started.

A name that is still live is already in that state (exit 0). A name that is in
neither the live list nor the tombstones is not found (exit 1) — including a
record whose retention window (shells.tombstoneRetention, 14 days by default)
has passed.

```
Usage: sidecar shell restore [--json] <tmux-name>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path)
- `-h, --help`: Show this help

**Exit codes:**

- `0`: restored, or already live
- `1`: not found, or state failure
- `2`: usage error
- `5`: an unknown --project or --shell

**Examples:**

```bash
sidecar shell restore sidecar-sh-sidecar-1
sidecar shell restore --json sidecar-sh-sidecar-1
```

### `sidecar shell send`

Run or type a command in a shell you are not sitting in

Send a command to an existing Sidecar-managed session. --run presses Enter;
--type leaves the command on the prompt for the user to read and run. This is
the same distinction `sidecar create shell --run/--type` makes, for a session
that already exists.

--target is required and must name a session the resolved project owns: a
sidecar-sh-… record in its shells.json, or a sidecar-ws-… agent for one of its
registered worktrees. tmux resolves a session name against whatever answers to
it, so an unregistered name is refused (exit 3) rather than typed into.

The keys go to the tmux server this process resolves, and the session must be
running: a record for a session that is not up is a tmux failure (exit 1), not
a silent success.

```
Usage: sidecar shell send --target SESSION (--run COMMAND | --type COMMAND) [--project NAME] [--json]
```

**Options:**

- `--target SESSION`: The tmux session to send to (required)
- `--run COMMAND`: Execute COMMAND in the session
- `--type COMMAND`: Type COMMAND without pressing Enter
- `--shell NAME`: Resolve the project from a registered shell
- `--project NAME`: Target project (slug, basename, or path)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: sent
- `1`: tmux, ambiguity, or state failure
- `2`: usage error
- `3`: --target names no session this project owns, or one recorded on a different tmux server
- `5`: an unknown --project or --shell

**Examples:**

```bash
# start an agent in an existing shell
sidecar shell send --target sidecar-sh-sidecar-2 --run "claude"
sidecar shell send --target sidecar-ws-sidecar-fix-auth --run "go test ./..."
# leave it for the user to run
sidecar shell send --target sidecar-sh-sidecar-2 --type "git push" --json
```

## `sidecar terminal-links`

Inspect terminal resource providers

Inspect the external executables that teach Sidecar to recognize resource keys in
terminal output. This is a protocol and administration surface, not a replacement
for a provider's own CLI.

```
Usage: sidecar terminal-links <command>
```

### `sidecar terminal-links check`

Check one terminal resource provider instance

Check one configured provider instance: that it is enabled, that its command
resolves, and that its describe method answers the protocol. The child runs with
the exact working directory, base environment, passEnv policy, and timeout Sidecar
uses in the TUI, so this is the authoritative host-environment proof.

--resolve is separate and explicit because it can perform network access and print
private resource data. Without it, nothing is resolved.

The provider's stderr is drained and discarded, never printed: reproduce provider
failures by running the provider's own CLI deliberately.

```
Usage: sidecar terminal-links check [--resolve LOCATOR] [--json] [--config PATH] <instance>
```

**Options:**

- `--resolve LOCATOR`: Also resolve one locator (may hit the network and print private data)
- `--json`: Write one structured result object to stdout
- `--config PATH`: Read a specific config file
- `-h, --help`: Show this help

**Exit codes:**

- `0`: the instance checked out
- `1`: the command, describe, or resolve failed
- `2`: usage error
- `3`: no provider instance with that id is configured

**Examples:**

```bash
sidecar terminal-links check jira-work
sidecar terminal-links check jira-work --json
sidecar terminal-links check jira-work --resolve CASH-1245 --json
```

### `sidecar terminal-links list`

List configured terminal resource providers

List the terminal resource providers configured under "terminalResources".
By default this reads configuration and resolves each command on PATH; it starts
no process. --describe additionally asks each enabled provider to describe itself,
which is local and non-interactive but does spawn one child per instance.

passEnv is reported by name and presence only. A passed value is never printed.

Enabling a provider trusts that executable with your full OS privileges: a process
boundary is crash isolation, not a sandbox.

```
Usage: sidecar terminal-links list [--describe] [--json] [--config PATH]
```

**Options:**

- `--describe`: Also run each enabled provider's describe method
- `--json`: Write one structured result object to stdout
- `--config PATH`: Read a specific config file
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: configuration could not be read
- `2`: usage error

**Examples:**

```bash
sidecar terminal-links list
sidecar terminal-links list --json
sidecar terminal-links list --describe --json
```

## `sidecar worktree`

Manage Sidecar-visible git worktrees

Plan and perform worktree lifecycle operations through Sidecar's shared core.

```
Usage: sidecar worktree <command>
```

### `sidecar worktree delete`

Plan or delete a Sidecar-visible git worktree

Delete a git worktree through the same lifecycle the TUI's confirmation uses.
Sidecar first resolves the target from the owning project's live `git worktree`
inventory and applies the shared refusal rules: the main, bare, detached, locked,
missing, and prunable worktrees cannot be deleted. The target may be its Sidecar
display name, checked-out branch, path basename, absolute path, or sidecar-ws-…
session name. Use --project with the project slug returned by `create worktree`,
or run from the project or one of its worktrees.

--plan and --dry-run are aliases. They read the same confirmation facts without
changing git, tmux, Sidecar state, or a pending-creation journal: target identity,
dirtiness, remote-branch availability, cleanup choices, and the pinned HEAD OID.
A real deletion always requires --yes. For a plan-first deletion, use the returned
absolute path as TARGET and pass its branch and headOid back with --expect-branch
and --expect-head-oid. Both expectations are required together, so a branch rename
at the same commit is refused rather than mistaken for the confirmed checkout.

Deleting closes the Sidecar worktree session and any managed shells rooted in the
worktree before removing its directory, then forgets those shell records. A dirty
worktree is force-removed only after --yes, matching the warning and decision in
the TUI confirmation. --delete-local-branch and --delete-remote-branch are explicit
counterparts to its unchecked branch-cleanup boxes; the default is to keep both.
If this was a failed `create worktree`, its exact pending-creation journal is cleared
only after the worktree removal succeeds. Branch and journal cleanup failures are
reported as warnings after the primary deletion succeeds, as is a rooted shell that
could not be closed; those warnings do not skip the remaining requested cleanup.

```
Usage: sidecar worktree delete <name|branch|path> [--project NAME] [--plan|--dry-run | --yes] [options]
```

**Options:**

- `--project NAME`: Owning project (slug, basename, or path)
- `--plan`: Resolve and print the deletion plan without changing anything
- `--dry-run`: Alias for --plan
- `--yes`: Confirm worktree removal (required unless planning)
- `--delete-local-branch`: Also delete the checked-out local branch
- `--delete-remote-branch`: Also delete the branch from origin when it exists
- `--expect-branch BRANCH`: Refuse if the absolute target no longer checks out this planned branch
- `--expect-head-oid OID`: Refuse if HEAD differs from a previously returned plan
- `--json`: Write one structured plan or result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: plan resolved, or worktree deleted (cleanup warnings may be present)
- `1`: git, tmux, Sidecar state, or primary deletion failure
- `2`: usage error, including a deletion without --yes
- `5`: project, target, target state, branch choice, or expected branch/HEAD was refused

**Examples:**

```bash
# inspect the exact target and copy its path, branch, and headOid
sidecar worktree delete fix-auth --project sidecar --plan --json
# delete only if the confirmed checkout identity has not moved
sidecar worktree delete /path/to/repo-fix-auth --project sidecar --expect-branch fix-auth --expect-head-oid abc123 --yes --json
sidecar worktree delete /path/to/repo-fix-auth --delete-local-branch --yes
```


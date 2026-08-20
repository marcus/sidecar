# Sidecar CLI Reference

Sidecar provides non-interactive commands for scripting and agent workflows.

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

## `sidecar notify`

Post, dismiss, and list Sidecar notifications

Sidecar's notification surface: a toast in the running instance, an entry in the
notification centre, and a count in the header until the user reads it.

```
Usage: sidecar notify <command>
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

## `sidecar open`

Show a file, a td issue, a git diff, or a provider resource in a split pane

Show a file, a td issue, a git diff, or an external provider resource to the user as a
split pane in a Sidecar workspace. From a Sidecar shell this targets that shell.
Otherwise it targets the unique running instance, or a specific --shell / --project.
--diff with no spec is the working tree. --provider names a configured terminal resource
provider instance and is required for a resource: a bare locator is never guessed at.
--split only overrides the split axis; it never halves a live terminal after content is open.

```
Usage: sidecar open [options] [<target>]
```

**Targets:**

- `path`: A file inside the target workspace, optionally "path:line"
- `td-xxxxxx`: A td issue id
- `--diff`: Working-tree diff (wt); add a spec for a commit or range
- `spec`: A git commit or range (abc1234, A..B); --diff accepts HEAD and branch names
- `locator`: With --provider, a resource key such as CASH-1245

**Options:**

- `--line N`: Line to reveal (alternative to "path:line")
- `--diff`: Open a Diff leaf (working tree if no spec)
- `--provider ID`: Open a locator through a configured terminal resource provider
- `--shell NAME`: Target a registered shell by display name or tmux name
- `--project NAME`: Target a project's Workspaces surface (slug, basename, or path)
- `--split auto|right|below`: Where to place a new pane (default auto)
- `--wait DURATION`: Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)
- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: opened or queued
- `1`: state failure
- `2`: usage or validation error
- `3`: no running instance, or several running with no target
- `4`: an instance declined (e.g. the window is too small to split)

**Examples:**

```bash
# file, in a split beside the terminal
sidecar open internal/cli/cli.go
# file at a line
sidecar open internal/cli/cli.go:88
# td issue
sidecar open td-348d88
# working-tree Diff leaf
sidecar open --diff
# that commit, not the working tree
sidecar open --diff HEAD
# commit, unless a file of that name exists
sidecar open abc1234
# resource pane for that provider's locator
sidecar open --provider jira-work CASH-1245
# structured result for the agent
sidecar open --json --split below README.md
# from any terminal, that project's Workspaces surface
sidecar open --project sidecar README.md
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

Manage the current Sidecar shell context

Manage the current Sidecar-managed shell or worktree agent context.

```
Usage: sidecar shell <command>
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

Rename the current shell's display name

Rename only the Sidecar-managed shell or worktree agent containing this command.
This changes Sidecar's display name; it does not rename the tmux session, Git
branch, or worktree directory.

The current display name is also published as $SIDECAR_SHELL_NAME. "Shell 3"
is the unset default; a previous task's name is equally stale — rename when
the name no longer describes the work in this shell.

```
Usage: sidecar shell rename [--json] <display-name>
```

**Options:**

- `--json`: Write one structured result object to stdout
- `-h, --help`: Show this help

**Exit codes:**

- `0`: success
- `1`: identity or state failure
- `2`: usage or validation error

**Examples:**

```bash
sidecar shell rename "shell rename implementation"
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


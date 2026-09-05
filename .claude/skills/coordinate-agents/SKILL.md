---
name: coordinate-agents
description: Drive another Sidecar-managed agent from a shell — discover targets, create the layout, start a provider, prompt and wait, read before sending keys, and stay out of the user's way. Use when you need a second agent to review a diff, run a long task in parallel, or when a coordinated agent comes back blocked. Covers sidecar agent list/get/start/prompt/wait/read/send-keys and the refusal codes they return.
user-invocable: false
---

# Coordinating another agent

Sidecar can start and coordinate a second agent in a shell it owns. The commands are headless, target-taking, and `--json` from birth, so everything here works from an ordinary shell with no TUI attached.

Agent control is behind a default-off feature flag. Check first:

```bash
sidecar agent list --json
```

If that answers `{"error":{"code":"feature_disabled",...}}`, agent control is off. Ask the user to enable it (`agent_control` under `features.flags` in `~/.config/sidecar/config.json`) rather than turning it on yourself.

## The safe sequence

Do these in order. Each step exists because skipping it is how a coordinated agent ends up wedged, duplicated, or typing into the wrong pane.

### 1. Discover

```bash
sidecar shell list --json     # what shells exist
sidecar agent list --json     # which of them have a live agent, and its status
```

A target is a Sidecar-managed shell: its tmux session name, or its display name when that name is unique in the project. Inside a managed shell you may omit the target entirely and the command addresses `SIDECAR_SHELL`. Outside one, name the target.

`agent list` reports each live pane once, under the project that owns it, however many registered projects can see its checkout. An explicit target is searched across every project; if the same name exists in several, the project your own shell belongs to breaks the tie, so a sibling worktree resolves from a managed shell without flags. Outside a managed shell the refusal lists the projects and names the fix: `--project NAME` (a slug, a path, or a worktree Sidecar created, by path or basename) or `--shell NAME`. The `project` field of a `create shell` / `create worktree --json` result is the value `--project` accepts.

### 2. Create the layout separately

`agent start` never creates or moves a pane. Layout is `sidecar create shell`'s job, and keeping them apart is what makes it safe to start an agent without also rearranging the user's screen.

Use `--tab`, not the default placement and not `--split`. Without `--tab`, `create shell` opens a beside-the-session terminal split (`sidecar-tp-…`) — a live terminal, not a managed shell: it has no workspace row, `shell list` does not show it, and `agent start`/`agent prompt` refuse it as `agent_not_found` since there is nothing there to target. `--tab` is what actually adds the workspace row a coordinated agent needs.

```bash
created=$(sidecar create shell --tab --name reviewer --json)
target=$(printf '%s\n' "$created" | jq -r '.shell.session')
```

To start a catalog family with provider arguments in the same step, both `create shell` and `create worktree` take them after `--`, as `agent start` does, and still record the family: `sidecar create worktree orchestrate --agent claude --json -- --model fable`. Usage refusals under `--json` arrive as `{"error":{"code":"usage",...}}` on stderr, like every other refusal here.

From inside a worktree shell, `create shell --tab --agent KIND` inherits your own worktree's directory (the workspace row is placed there, not in the main checkout) — no `--worktree`/`--cwd` flag is needed.

When the managed shell should belong to one project but start elsewhere, pass both facts explicitly: `sidecar create shell --project sidecar --cwd ~/code/tui --name publisher --json`. `--cwd` never chooses project ownership and always creates a managed workspace row, even inside an existing Sidecar shell. It is refused with `--split` because a live terminal split has no durable shell record. Relative paths resolve from the caller's directory, `~` and `~/path` resolve from the caller's home, and the directory must exist before Sidecar creates tmux or durable state. The resolved path is the shell's live cwd, recorded `workDir`, provider launch cwd, and cold-restore cwd.

Creating a shell does not steal the user's focus. Do not rearrange panes the user set up, and never close a target you did not create.

### 3. Start the provider

```bash
sidecar agent start "$target" --kind codex --timeout 30s --json
```

This returns **only when the expected provider is positively identified and ready for input**. It is not "the bytes were sent". Refusals worth knowing:

| Code | Meaning |
| --- | --- |
| `agent_pane_busy` | the pane is running a command, an editor, another agent, or is in copy mode. There is no `--force`; wait or use a different shell. |
| `agent_kind_mismatch` | a different provider owns the pane. |
| `agent_start_failed` | the process exited before it was ready. |
| `agent_not_ready` | it came up blocked. The target is still inspectable — read it. |
| `timeout` | it did not become ready inside your `--timeout`. |

### 4. Prompt, and wait under one pinned target

```bash
sidecar agent prompt "$target" "Review the current diff and report only actionable findings." --wait --timeout 2m --json
```

`--wait` submits and waits as one operation, so no second command can race a replacement occupant into the gap. Things to know:

- **Read the receipt before retrying.** Prompt JSON adds `receipt.submission` (`submitted`, `not_submitted`, or `unknown`), `receipt.wait`, and the exact pinned `receipt.target`. The receipt is present on success and inside the error envelope on failure. A timeout after delivery remains exit 1 with error code `timeout`, plus `submission: "submitted"` and `wait: "timeout"`; do not send the prompt again. `unknown` means a write or transport may have landed and is equally unsafe to retry automatically.

- **Refusals happen before any byte is written.** Feature-disabled, unknown-host, missing-target, not-found, and ambiguous-target failures carry `submission: "not_submitted"` and `wait: "not_started"` too, without inventing a pane identity. A blocked target gets `agent_blocked`; an unidentified or stale one gets `agent_not_ready`; a replaced one gets `agent_replaced`; and a dead pane, a pane in copy mode, or a session that no longer holds exactly one pane gets `agent_pane_busy`. Nothing is sent in any of them.
- **There is no implicit timeout.** `--wait` requires `--timeout`, and so does `agent wait`.
- **Settled means `idle`, `done`, or `blocked`** by default. Narrow it with repeated `--until done`, or widen it with `--until working`.
- **A prompt that goes nowhere is reported, not hidden.** If the lifecycle does not move within 5 seconds of a prompt sent from idle or done, you get `agent_prompt_stalled`. The bytes were written; the agent did not react. Read the screen before you send anything else.
- **Prompting an already-working agent claims nothing.** Sidecar will not pretend to know which turn is which, and completion of the turn already in flight may satisfy your `--wait`. Wait for it to settle first if you care.

To wait without prompting — for an agent somebody else started, or after `send-keys`:

```bash
sidecar agent wait "$target" --until done --timeout 5m --json
```

The wait stays pinned to the same session, pane, pane process, tmux server, and provider. If any of those change you get `agent_replaced` rather than a wait that a different process quietly satisfied.

### 5. Read before you send keys

When a wait comes back `blocked`, the agent is asking a question. **Read the screen, decide, then answer.** Sidecar does not auto-answer approvals, and neither should you without knowing what was asked.

```bash
sidecar agent read "$target" --source recent-unwrapped --lines 120
```

| Source | Use it for |
| --- | --- |
| `visible` | the current screen (default) |
| `recent` | the screen plus recent scrollback |
| `recent-unwrapped` | the same, with soft wraps joined — the one you want for logs and long answers |
| `detection` | the exact slice the lifecycle detector read, when you want to argue with a status |
| `transcript` | the provider's own conversation. Returns `transcript_unavailable` until an exact session binding exists; it is never guessed from the newest session in the same directory. |

Reads are passive. They never scroll, resize, or otherwise touch the agent's UI.

### 6. Answer with logical keys

```bash
sidecar agent send-keys "$target" down enter
```

Keys are named, not typed: `enter`, `esc`, `tab`, `space`, `backspace`, `delete`, `insert`, the arrows, `home`, `end`, `pageup`, `pagedown`, `f1`–`f12`, `ctrl+<letter>`, `ctrl+space`, `alt+<key>`, `shift+tab`, `shift+enter`, `shift+<arrow>`, and any single character. The whole list is validated before any of it is written, so a typo sends nothing at all.

`send-keys` is for answering a UI. **Prompt text goes through `agent prompt`** — it is bracketed-paste aware and submits correctly; a string of characters through `send-keys` is not the same thing.

With two or more positional arguments the first is the target. With exactly one, the key goes to `SIDECAR_SHELL`.

## Reading the output

Every verb takes `--json`. Success writes one object on stdout; failure writes `{"error":{"code":...,"message":...,"target":{...}}}` on stderr. Prompt adds its receipt to the success object or error object without changing those codes. Exit codes:

| Exit | Meaning |
| --- | --- |
| 0 | success |
| 1 | transport failure or timeout |
| 2 | usage error — your command line, not the agent |
| 3 | the target is not a registered Sidecar shell |
| 5 | feature disabled, or a semantic refusal such as `agent_blocked` |

The `target` in every result carries the pin: host, project, tmux session, pane id, pane pid, and tmux server pid. Two shells with the same display name on different hosts cannot collide.

## Remote host panes

From a Sidecar-managed pane whose geometry lease is held by a connected viewer, `sidecar open` and `sidecar layout` are that viewer's screen — not a TUI that may not be running on the host. There is no `sidecar open --host`. Off-screen, or a disconnected or too-old lease holder, refuses (exit 4) rather than queue.

## What this is not

Sidecar owns provider identity, readiness, and its refusal rules. It does not own raw terminal control. If you want to run a command in a pane and read its output, that is `tmux`'s job — `sidecar shell list --json` gives you the session names. Do not reach for `agent send-keys` to drive a shell.

Sidecar also does not own the workflow. It gives you primitives; the plan, the task engine, the review policy, and the retry logic are yours.

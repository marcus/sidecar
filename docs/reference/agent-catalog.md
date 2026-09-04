# The agent catalog

Sidecar's agent catalog is the single description of the agent families it knows: what each one is called, what command starts it, which flag turns off its approval prompts, and how it resumes a session. Everything that offers you an agent reads it: the Create Workspace and Create Shell pickers, the global Sessions create, Configuration → Agents, `sidecar create shell --agent`, `sidecar create worktree --agent` and `sidecar agent start --kind`.

It is data, not code. Sidecar ships one TOML file per family, and you can override any of them or add your own without rebuilding anything.

## The overlay directory

Sidecar reads `*.toml` from an `agents` directory beside your config file:

| your config file | overlay directory |
| --- | --- |
| `~/.config/sidecar/config.json` (the default) | `~/.config/sidecar/agents/` |
| `sidecar -config /path/to/config.json` | `/path/to/agents/` |

The directory does not exist until you create it. It is read once, at startup, by both the app and the CLI, so a change takes effect the next time Sidecar starts.

**The file name is the family id.** `~/.config/sidecar/agents/claude.toml` overrides the bundled Claude family; `~/.config/sidecar/agents/housecat.toml` adds a new family called `housecat`. You can also state `id` explicitly in the file, and it wins over the file name.

**An override changes only the fields it states.** The file is decoded on top of what Sidecar ships, so this is a complete file:

```toml
# ~/.config/sidecar/agents/claude.toml
command = "claude-next"
```

Claude keeps its name, its aliases, its conversation adapter, its resume arguments and its position in the picker; only the command changes. An override cannot move a family in the picker or make one legacy, because `order` and `legacy` are catalog bookkeeping rather than facts about a provider.

**A new family needs a name, a short label and a command:**

```toml
# ~/.config/sidecar/agents/housecat.toml
name = "Housecat"
short = "Housecat"
command = "housecat"
skip_permissions_arg = "--pounce"
resume_args = ["--resume"]
resume_kinds = ["id"]
```

It appears at the end of the creation picker, launches as `housecat`, adds `--pounce` when you tick auto-approve, and resumes as `housecat --resume <session>`.

**A malformed file is skipped, not fatal.** Sidecar reports it (a warning in `debug.log` for the app, a line on stderr for the CLI) and carries on with every other family. A broken personal config file cannot stop Sidecar from starting.

## Schema

Every field is optional except `name` and `short` (and `id`, which defaults to the file name).

| key | type | meaning |
| --- | --- | --- |
| `id` | string | The stored identity: what is written to `plugins.workspace.agents`, `agentStart` and `defaultAgentType`. Defaults to the file name. |
| `order` | integer | Position in the creation picker. A family with no order sorts after every family that has one, then by id. |
| `name` | string | Full display name, shown in modals and menus. |
| `short` | string | Compact label for settings rows and the agent chip. |
| `command` | string | The executable to launch. **A family with no command is detection-only**: Sidecar can recognise it running in a pane and never offers to start it. |
| `launch_args` | array | Argv entries between the command and everything else, for a provider whose bare command is not the agent. Only `kiro` needs one (`kiro-cli chat`). |
| `skip_permissions_arg` | string | One argv entry, appended when you turn on auto-approve. Leave it out when the provider has no such flag. |
| `aliases` | array | Other identifiers naming this family: process spellings Sidecar may see in a pane, and a conversation adapter id that differs from the family id. |
| `adapter_id` | string | The conversation-history adapter's registered id, when it differs from `id`. |
| `resume_args` | array | Argv entries between the command and the session value. Leave it out when the provider has no resume. |
| `resume_kinds` | array | Which session references this family resumes from: `"id"`, `"path"`, or both. |

`legacy = true` marks the compatibility bucket: launchable for a setting you already have, offered by nothing. Aider is the only bundled case.

### The session value is always last

`resume_args` are what comes *before* the session value, and the value is its own argv entry. That expresses every shape Sidecar ships: a subcommand (`codex resume <id>`), a chain of them (`amp threads continue <id>`), and a flag pair (`opencode --continue -s <id>`).

A provider whose resume only works as `--resume=<value>`, joined by an equals sign, cannot be expressed and gets no resume rather than an invented one. GitHub Copilot is the bundled example.

### Auto-approve flags are one argv entry

`skip_permissions_arg` is a single word. A provider whose bypass is two words (`--permission-mode dangerous`) gets none, and the bundled file says so and says why. Sidecar does not guess a joined spelling it has not run.

## Which agents the picker offers

The Create Workspace, Create Shell and Sessions create pickers offer a family when:

- you have named it in `plugins.workspace.agents`, or
- you have named nothing there and its command is on your `PATH`.

Naming a family is the stronger signal and is honoured whether or not the command is installed: writing the setting is you telling Sidecar what you want offered.

Configuration → Agents lists **every** launchable family, installed or not, because "what exists" is the question a configuration page answers. A family whose command is not on `PATH` is annotated `not installed` there.

If nothing in the catalog resolves on `PATH`, which is an unusual environment more often than a machine with no agents, the picker falls back to offering everything. An empty picker is a dead end.

The `PATH` lookup runs once per process, in the background at startup. The CLI does not filter at all: `sidecar create shell --agent <kind>` launches any family in the catalog, because a non-interactive caller has said exactly what it wants.

## What the catalog does not decide

- **Detection.** Whether Sidecar can read an agent's state off its pane comes from `internal/agentactivity` and the detection manifests it vendors, not from this file. Adding a family here does not add a state badge.
- **Conversation history.** The Conversations plugin reads transcripts through an adapter under `internal/adapter/`. `adapter_id` names one; it does not create one.
- **Colour.** A family with no registered theme colour renders in the muted default and its chip shows its short label lowercased. That is deliberate, and it is why adding a family costs no theme work.

## See also

- `internal/agentcatalog/families/README.md`: the same schema from the maintainer's side, with the rules a bundled file has to follow.
- [Adding new agent CLIs](../guides/active/adding-new-agent-clis.md): the full seven-subsystem guide, of which this catalog is step one.

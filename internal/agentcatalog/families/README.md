# The agent family files

One TOML file per agent family, embedded in the binary. The file name is the family id. Adding a provider Sidecar can start is adding a file here; nothing else in this package has to change.

A user extends the same catalog without a rebuild by dropping files into `<config dir>/agents/`; see [docs/reference/agent-catalog.md](../../../docs/reference/agent-catalog.md).

## Schema

| key | type | meaning |
| --- | --- | --- |
| `id` | string | The stored identity: what is written to `plugins.workspace.agents`, `agentStart` and `defaultAgentType`. Must equal the file name. |
| `order` | integer | Position in the creation picker. Unique across bundled files. A family with no order sorts past every one that has one. |
| `legacy` | bool | The compatibility bucket: launchable for a persisted setting, offered by nothing. Aider is the only case. |
| `name` | string | Full display name. |
| `short` | string | Compact label. Lowercased, it is the token the agent chip renders, so it must lowercase to either the id or the command. |
| `command` | string | The executable Sidecar launches. **A family with no command is detection-only**: Sidecar can recognise it in a pane and never offers to start it. |
| `launch_args` | array | Argv entries between the command and everything else, for a provider whose bare command is not the agent. Only `kiro` needs one. |
| `skip_permissions_arg` | string | One argv entry, appended when the caller asks for the provider's auto-approve mode. Empty when the provider has none. |
| `aliases` | array | Other identifiers naming this family: Herdr's process spellings, and the conversation adapter ids that do not match the id. |
| `adapter_id` | string | The conversation-history adapter's registered id, when it differs from `id`. |
| `resume_args` | array | Argv entries between the command and the session value. Empty means no native resume. |
| `resume_kinds` | array | The session reference kinds this family resumes from: `id`, `path`, or both. |

Every field is optional except `id`, `name` and `short`.

## Rules a new file has to follow

**Record where every fact came from, in a comment at the top of the file.** A command name, an auto-approve flag and a resume shape are claims about somebody else's software, and the next person to touch the file needs to know whether they were read from the provider's `--help`, its documentation, or Herdr's `agent_resume.rs`.

**Never guess a flag.** A provider with no auto-approve mode gets no `skip_permissions_arg`, and the file says so and says why. Three bundled families have none for three different reasons: Cline auto-approves by default and its flag takes a value, Devin's bypass is two argv entries the single-entry field cannot hold, and Mastra Code's is an in-app toggle rather than a flag. Guessing produces a command line nothing has run.

**The session value is always the last argv entry.** `resume_args` are what comes before it. That expresses a subcommand (`codex resume`), a chain of them (`amp threads continue`), and a flag pair (`opencode --continue -s`). A provider whose resume only works as `--resume=<value>` cannot be expressed and gets no resume rather than an invented one; Copilot is the standing example.

**Ids are Herdr's own agent labels.** That is what makes the vendored manifest file, the key into `aliases.upstream.json` and `sidecar agent explain --agent` agree with no mapping. It is why Qoder's id is `qodercli` even though the program is `qoder`: `qodercli` is what upstream writes everywhere, and a prettier Sidecar-only spelling would buy a third entry in `agentactivity.ManifestAgentID` and `HerdrAgentLabel`. Where the id and the command differ, `short` follows the command, because a chip reading `qodercli` names a manifest file rather than a program.

**Aliases are Herdr's `lookup_agent` spellings, minus the id itself, plus the conversation adapter id where it differs.** They are data so the alias table and this catalog can be asserted against each other. `agentactivity.identifyProcessName` is what actually resolves a pane's process name, and `TestUpstreamAliasesResolveForClaimedFamilies` keeps the two honest.

## The three buckets

`Families()` is read as "everything Sidecar can launch" by the creation pickers, both configuration pages, `workspaceops`, and `TestAgentPickersFollowCatalog` in another package. So a family that cannot be launched must not be able to reach it, and that is what the buckets are for:

- **launchable**: has a command, is not legacy. `Families()`, in `order`.
- **detection-only**: no command. `DetectionFamilies()`, in id order. Empty as Sidecar ships today: every agent Herdr publishes a manifest for now has a command. The bucket stays because the next agent upstream adds is detection-only from the moment its manifest is vendored until somebody establishes its command.
- **legacy**: `legacy = true`. `LegacyFamilies()`, reachable only through `FindLaunch` at an execution boundary honouring an older persisted setting.

## What a family does not carry

No theme colour and no conversation adapter, deliberately. `styles.AgentColor` answers `TextMuted` for a provider no theme registers and `styles.AgentLabel` falls back to the short name, so a family with neither renders correctly. Both are real work and both are independent of being launchable.

Two families here have no vendored screen manifest at all: `omp` and `mastracode`, which Herdr ships hooks-only. They are launchable and `identifyProcessName` names them, so a hook report can be checked against the pane, but `agentactivity.Supports` is false for them and no row carries a provider chip whose state could only ever be unknown. They are declared in `familiesWithNoScreenManifest` in `internal/agentactivity/detection_only_test.go`.

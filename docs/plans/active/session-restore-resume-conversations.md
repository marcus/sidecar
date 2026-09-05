# Session restore: resume the conversation, not only the shell

**Status:** Planning, opened 2026-09-04. This is the controlling plan for the restore work that remains after [Herdr gap closure: agent control and cold session restoration](herdr-agent-control-and-session-restore.md) shipped M3 (exact session binding) and M4 (cold shell and layout restoration). Nothing here reopens those milestones; every slice below builds on the planner, executor, and manifest fields they left in place.

## Outcome

After a tmux server is lost, the user launches Sidecar and every restored shell already has its exact resume command typed at the prompt, waiting for Enter. That holds for shells whose conversation an official integration reported, and for shells whose conversation was never reported, because Sidecar can find the most likely conversation in the provider's own store and say how sure it is. Under the `auto` policy the reported ones are resumed without the keystroke. Worktree sessions come back with their shells. When a tmux server is stuck rather than dead, Sidecar names the cause and clears the part it owns. And an agent running a proof inside a Sidecar shell cannot reach the default server by accident.

## The incident this plan must make automatic

On 2026-09-04 a proof subagent, running inside a Sidecar shell, cleaned up its private tmux server with `TMUX_TMPDIR=$SOCK tmux kill-server`. The inherited `$TMUX` variable overrides `TMUX_TMPDIR` in tmux's socket resolution, so the command reached the default server. All 27 sessions on it were destroyed at 18:31:46 PDT: 22 managed shells across seven projects, 2 worktree workspace sessions (`sidecar-ws-*`), and 3 terminal splits (`sidecar-tp-*`). Everything below was recovered by hand and is the acceptance case for each slice.

| What happened | What Sidecar did | What had to be done by hand |
|---|---|---|
| The server did not exit. Six `tmux -C attach-session` control clients belonging to a Sidecar instance that had already exited kept it in tmux's exit-pending state, where every new client is refused with `server exited unexpectedly`. | Create Shell reported `prepare tmux server: exit status 1`; `sidecar session status` reported `read restore state: exit status 1`. Neither said the server was shutting down or what was holding it. | Read `lsof` on the server, terminate the six orphaned control clients, watch the server exit, start a new one. |
| A fresh server and a Sidecar launch. | M4 recreated all 22 managed shells under their own names and directories within three seconds and posted `Session restore: 22 shells restored`. This part worked. | Nothing. |
| No conversation was resumed. Every agent shell read `no conversation is bound to this shell`. | Correct by its own rules: the Claude integration had been installed at 18:03 that day, after every affected session had started, so no SessionStart hook had ever reported, and the other providers had no integration installed. | For each shell: read the provider's store, pick the conversation whose working directory matched and whose last write was the moment of the kill, and type the resume command into the shell without Enter. |
| The two worktree sessions and three splits were not restored. | Not in scope for M4. | The conversations that lived in the worktrees still have no shell. |
| The two shells whose worktrees had since been deleted were listed as `manual`. | Correct. | Nothing. |

What the manual recovery relied on, per provider, is the evidence Slice 2 has to read:

| Provider | Store | Identity | Time evidence | What resolved ambiguity |
|---|---|---|---|---|
| Claude Code | `~/.claude/projects/<cwd-slug>/<uuid>.jsonl` | filename | file mtime, all equal to the kill minute | first user message named the role the shell was named for; `permissionMode` and the last `model` field gave the flags |
| grok | `~/.grok/sessions/<url-encoded-cwd>/<uuid>/summary.json` | directory name | file mtime | `session_summary` text; two grok shells in one cwd were told apart by it, one grok shell in another cwd could not be and got the picker (`grok --resume` with no argument) |
| antigravity | `~/.gemini/antigravity-cli/cache/last_conversations.json` and `history.jsonl` | `conversationId` keyed by workspace | `history.jsonl` timestamps | one conversation per workspace, so none |
| opencode | `~/.local/share/opencode/opencode.db`, table `session` | `id` | `time_updated`, which is the last message and can be a day older than the kill | none; the newest same-directory session was a guess |

`internal/adapter` already has readers for these stores. The plan reuses them; it does not add a second set.

## Settled decisions

1. **Typing is the `ask` policy's answer.** Under `ask`, the executor types the resume command into the recreated shell and does not press Enter. The user reviews it in the shell itself, where the command will run, rather than in a modal. `sidecar create shell --type` already types without Enter; the executor reuses that primitive.
2. **A found conversation is a candidate, never a report.** Slice 2 proposes; it never sets `agentsession.Ref.Reported`. A candidate is typed under `ask` and is never started under `auto`. This is the rule M3 wrote down and it does not move.
3. **Sidecar owns its control clients, not the server.** A `tmux -C attach-session` client whose parent Sidecar process no longer exists is Sidecar's own leftover, and Sidecar may terminate it. Sidecar still never signals, restarts, or replaces the tmux server itself.
4. **Never replay, but typing is not replay.** A `--run` command is still never executed by restore. Whether it may be typed is open question 1.
5. **Binding starts at the next session start.** A provider integration installed after a conversation began cannot report it; that population, and every provider without an integration, is exactly what Slice 2 exists for. No work goes into making a hook report retroactively.

## Work sequence

Slices are ordered by how much of the incident each one removes. Slices 1 and 2 are the plan's core. The epic is `td-512a82`; the slices are `td-e8a19b`, `td-ede1c4`, `td-5d8a44`, `td-258ca0`, `td-16de78`, and `td-fe2a8c` in order.

### Slice 1 — Type the resume command (small)

- Add `ActionPrefillResume` to `internal/sessionrestore`. The planner emits it in place of `resume-agent` when the effective policy is `ask` and the shell has a reported reference; when the shell has a candidate from Slice 2 it emits it under both `ask` and `auto`.
- The typed text is `agentcatalog.DisplayCommand` over the same argv `ResumeArgv` builds for `resume-agent`, so what the user sees is what `auto` would have run. Provider launch flags Sidecar itself adds at start (the Claude system-prompt flag, grok's approval flag) come from the same catalog entry; the plan must not grow a second place that knows them.
- Idempotency: the tmux session name remains the key. Before typing, the executor checks that the pane's foreground process is the shell and the pane's input line is empty, and it records `restore.prefilledAt` on the shell record so a second run reports `already typed` rather than typing twice.
- `sidecar session restore` gains `--prefill` as the non-interactive spelling of this action, `session status` names it, and the `ask` summary the TUI shows after first frame lists which shells have a command waiting for Enter.

**Exit gate:** on the reboot harness (`scripts/session-restore-reboot.sh`), a bound fake agent under `ask` ends with its exact resume command visible at the prompt, nothing executed, and a second restore run leaves the pane byte-identical.

### Slice 2 — Candidates for unbound shells (medium)

- Add a candidate finder in `internal/agentsession` (or a sibling package that imports `internal/adapter` and nothing from tmux or the TUI). Input: the shell's working directory, agent kind, `createdAt`, and the moment the server was lost (Slice 3 records it). Output: zero or one candidate with the reference, a title, the last-write time, a confidence, and the reason in words.
- Ranking: sessions whose directory equals the shell's working directory, whose last write falls inside the shell's alive window, ordered by last write nearest the loss. A single match is `likely`. Several matches in one directory for one provider are `ambiguous` and no candidate is chosen unless another shell in the same directory has already claimed the other match; the step then says which conversations are in contention and the typed command becomes the provider's own picker where one exists (`grok --resume`, `opencode` with no session flag).
- The manifest gains `agent.candidate`, additive and distinct from `agent.session`, so a v3 reader that does not know the field drops nothing that matters. `sidecar agent get` shows it under the same redaction rule as the reference: value only for the shell's own query or with `--include-session-ref`.
- Provider order: Claude and grok first, since their stores carry per-session write times; antigravity next; opencode last, with its weaker time evidence stated in the reason.

**Exit gate:** on the reboot harness, an unbound shell whose provider store holds two same-directory sessions with different last-write times ends with the nearer one typed and the reason naming both; a shell with two indistinguishable matches ends with the picker typed and both ids in the reason; no candidate is ever marked `reported`, and `auto` never starts one.

### Slice 3 — Record the loss (small)

- When `shellstate.ForgetOrPreserveAtPath` reclassifies records as cold-restore candidates, also write `restore.serverLostAt` from the observation that proved the loss, and the last agent-activity evidence string for the shell. Slice 2 reads the first; the `ask` summary shows the second so a row reads "was running claude, idle since 13:44" rather than only a name.
- The write stays on the transition only, under the same one-write-per-lifetime rule `ObserveLiveAtPath` follows.

### Slice 4 — A stuck server is not a dead server (small)

- `internal/tmuxserver` distinguishes three answers where it now has two: no server, a server that answers, and a server whose socket accepts and then closes (`server exited unexpectedly`). The third is a server in tmux's exit-pending state.
- In that state Sidecar lists control-mode clients attached to that socket whose parent process is gone, and whose command line is Sidecar's own attach form. `sidecar session status` names them and says the server is shutting down behind them; `sidecar session restore` terminates only those clients, waits for the server to exit on its own, and starts a new one through the path Create Shell already uses. It never signals the server.
- Create Shell's `prepare tmux server: exit status 1` and `session status`'s `read restore state: exit status 1` are replaced by that sentence.

**Exit gate:** on the reboot harness, kill the isolated server while a fake control client with a dead parent is attached; Sidecar reports the stuck state with the client's pid, clears it, and the next `session status` shows a replaced server. A control client whose parent is alive is left alone and named as the reason the server cannot exit.

### Slice 5 — Worktree sessions and splits (medium)

- Recreate `sidecar-ws-*` sessions for worktrees whose directory still exists, using the same eligibility marker managed shells use, so a conversation that lived in a worktree has a shell to be typed into. A worktree whose directory is gone is a refusal with the path in the reason, matching the managed-shell rule.
- `sidecar-tp-*` splits are recreated from the saved pane layout, since the layout already names them.
- Both go through the Slice 2 candidate finder with the worktree path as the working directory.

### Slice 6 — Keep proofs off the default server (small)

- `AGENTS.md` states the rule: `$TMUX` overrides `TMUX_TMPDIR`, so a proof run inside a Sidecar shell that only sets `TMUX_TMPDIR` is talking to the default server; every tmux command in a proof either runs after `unset TMUX TMUX_PANE` or passes `-S` with the private socket path. This is done in this plan's first commit.
- `scripts/tmux-drive.sh` and `scripts/session-restore-reboot.sh` already unset `$TMUX`; a proof that hand-rolls its own tmux commands should source the same guard. Add `scripts/proof-tmux-env.sh`, one file that exports the private socket and unsets `$TMUX`, and point the headless-testing guide at it.
- Harness-side guard, optional and per harness: a Claude Code PreToolUse hook in this repository that refuses a Bash command containing `kill-server` unless it also contains `-S ` or `-L ` or `unset TMUX`. Recorded here so the maintainer can decide whether one more guard is worth its noise.

## Verification

The reboot harness is the proof surface for Slices 1 through 5; each exit gate above runs on it and none of them may inspect or mutate the default server. Slice 6 is verified by reading the changed files and by running one hand-rolled proof through the guard script.

## Open questions

1. May restore type a recorded `--run` command (for example `npm run dev`) the way it types a resume command? It is never executed, but a typed command is one keystroke from running.
2. Should the `ask` summary in the TUI offer one action that presses Enter in every shell that has a typed command, or is per-shell Enter the whole point?
3. Where does the candidate finder live once the Conversations plugin is removed ([plan](remove-conversations-plugin.md))? It must depend on `internal/adapter`, not on the plugin, whichever way that plan lands.
4. Provider "active session" registries (grok's `active_sessions.json` was empty at the loss) are not evidence this plan relies on. Revisit if one proves reliable.

## Changelog

- 2026-09-04: opened after the default tmux server was lost to a mis-scoped proof; recovery performed by hand and recorded above as the acceptance case.

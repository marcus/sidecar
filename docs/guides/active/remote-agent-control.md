# Driving an agent on another machine

Every `sidecar agent` verb, plus `session status` and `session restore`, takes `--host ID` and runs on a registered host. This guide is the demo recipe: what to set up, what to run, and what each answer means.

Both flags must be on:

```bash
sidecar -enable-feature sidecar_remote_hosts -enable-feature agent_control agent list --host mac-mini --json
```

or turn them on permanently in **Configuration → System → Feature Flags**.

## How it works, in one paragraph

There is no second protocol and no daemon. A remote verb is one `ssh <host> sidecar <verb> --json` invocation over the ControlMaster Sidecar already keeps open, so the round trip costs a request rather than a connection. The verb runs on the machine that owns the pane, which means the target resolution, the occupant pin, the readiness contract and every refusal rule are the host's — there is only one implementation of them, and it is the one closest to the terminal. What comes back is the host's own answer, including its errors.

## Register a host

```bash
sidecar host add --id mac-mini --target mac-mini.local
sidecar host list
sidecar host probe mac-mini
```

`--target` is whatever your ssh config resolves; Sidecar does not manage ssh configuration. `sidecar host probe` is the fastest way to tell a transport problem from a Sidecar problem before you start blaming a verb.

## The sequence

The safe sequence is the same one the local path uses, and it is the same for the same reason: create the layout separately, start the provider, prompt and wait, read before you send keys.

```bash
# 1. Discover. Presence and capability, never conversation identifiers.
sidecar agent list --host mac-mini --json

# 2. Create the shell. `create shell` owns placement; `agent start` owns
#    provider identity and readiness, and never creates or moves layout.
#
#    Note the asymmetry: --host is on the agent verbs, not on `create shell`.
#    Creation goes over plain ssh, which works because every Sidecar mutation
#    is by construction an ordinary documented CLI verb. See "What --host does
#    not cover" below.
ssh mac-mini sidecar create shell --name reviewer --json

# 3. Start the provider. Returns only when the host has positively
#    identified it and it is ready for input.
sidecar agent start reviewer --host mac-mini --kind codex --timeout 60s --json

# 4. Prompt and wait under one pinned target.
sidecar agent prompt reviewer "Review the current diff and report only actionable findings." \
  --host mac-mini --wait --timeout 5m --json

# 5. Read before you send keys.
sidecar agent read reviewer --host mac-mini --source recent-unwrapped --lines 120

# 6. Answer a blocked agent deliberately. Sidecar never answers an approval.
sidecar agent send-keys reviewer --host mac-mini down enter
```

## The three differences from a local verb

**A target is required.** Omitting the target locally means "the shell I am in", read from `SIDECAR_SHELL`. That names a shell on *this* machine, and two Sidecars running a project of the same name generate the same tmux session names by construction, so a silent fallback could address a real but entirely wrong pane on the other machine. `--host` without a target is refused.

**Conversation identifiers stay on the host.** A remote `agent list` or `agent get` reports whether a shell is bound to an exact conversation and whether an official integration vouched for it — not what it is bound to. The value crosses only for `--include-session-ref`, and it is never written into this machine's `shells.json`. The host does the redacting, so this holds even if the viewer asks carelessly.

**Restore runs on the host.** `sidecar session status --host mac-mini` reads that machine's plan; `sidecar session restore --host mac-mini --agents --yes` performs it there. The viewer requests and observes. It never rebuilds another machine's state locally, because everything a plan is built from — live tmux sessions, the server id, whether a working directory still exists, whether a provider binary is installed — is a fact about the host.

## Reading a failure

| Exit | Code | What it means | What to do |
| --- | --- | --- | --- |
| 1 | `host_unavailable` | The machine could not be reached. Nothing was attempted. | Check the network and `sidecar host probe`. |
| 1 | `timeout` | The host did not answer within the deadline. | Retry, or raise `--timeout`. |
| 2 | `version_skew` | The host's Sidecar does not know this verb. | Update Sidecar on whichever machine is older. |
| 3 | `agent_not_found` | That host does not own that target. | `sidecar agent list --host ID` to see what it does own. |
| 5 | `agent_blocked`, `agent_pane_busy`, … | The host's own refusal, verbatim. | Read the message; it is the same one you would get locally. |

Capability negotiation falls out of the exit-code contract rather than needing a handshake: a host that has never heard of a verb answers with a usage error, which is exit 2, which is version skew.

## What `--host` does not cover

`--host` is on the agent verbs and on `session status`/`session restore`. It is not on `create shell`, `create worktree`, `worktree delete`, `shell rename`, `shell send` or `shell delete`. Those are reachable on a host today either from the Sessions browser where wired, or over plain ssh as shown above — which works precisely because every mutation Sidecar can perform is an ordinary CLI verb rather than a private protocol. Remote worktree deletion is currently the plain-ssh case: run `sidecar worktree delete TARGET --project PROJECT --plan --json` on the host, then use the returned absolute `path` as the target and re-run with its `branch`, `--expect-branch`, its `headOid`, `--expect-head-oid`, and `--yes`.

The asymmetry is worth knowing rather than working around silently: the documented coordination sequence begins with a creation step that has no `--host` form, so a script that uses `--host` for everything else still shells out for that one line.

## Waits

`agent wait --timeout` and `agent prompt --wait --timeout` run as bounded invocations whose deadline the caller owns. There is no resident subscription channel and no implicit timeout. The invocation carrying a wait is given the wait's own timeout plus slack, so a five-minute wait is not severed at thirty seconds by the transport's default — the host's timeout is what expires first, and you get the agent-level `timeout` refusal rather than a dropped connection.

## Proving it without a second machine

The parity suite runs the whole remote path over a fake `ssh` that executes the rendered remote command locally. That exercises the real argv rendering, the real shell quoting, a real process, real exit codes, real JSON decoding through the banner-tolerant decoder and real error envelopes on stderr. It does not exercise sshd, authentication, network latency, or a dropped connection. For those, `scripts/remote-spike.sh` and the opt-in `SIDECAR_SPIKE_HOST` tests reach a real machine.

# Herdr detection sync report

Generated 2026-09-02T13:29:35Z by `go run ./internal/tools/herdrsync`.

| Field | Value |
| --- | --- |
| Herdr repository | https://github.com/herdrdev/herdr |
| Ref vendored | `preview-2026-08-31-b1ff4582e968` |
| Commit | `b1ff4582e9688f52ffb943cfa8bee4871ae122e4` |
| Pinned release for the differential harness | `preview-2026-08-31-b1ff4582e968` |
| Catalog | https://herdr.dev/agent-detection/index.toml |
| Catalog ETag | `W/"d78b183cb570f3f343ba80777bbf579d"` |
| Sidecar manifest engine version | 3 |
| Manifests vendored | 21 |

## Version changes

No manifest version changed since the previous lock.

## File changes

0 file(s) changed, 21 unchanged.

## Published versus bundled

Each row is the copy a Herdr client would load, and why.

| Agent | Vendored from | Bundled | Published | Reason |
| --- | --- | --- | --- | --- |
| `agy` | published | 2026.06.24.1 | 2026.06.24.1 | published and bundled are both 2026.06.24.1; a Herdr client prefers the remote copy |
| `amp` | published | 2026.07.09.1 | 2026.07.09.1 | published and bundled are both 2026.07.09.1; a Herdr client prefers the remote copy |
| `claude` | published | 2026.08.29.1 | 2026.08.29.1 | published and bundled are both 2026.08.29.1; a Herdr client prefers the remote copy |
| `cline` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `codex` | published | 2026.08.28.1 | 2026.08.28.1 | published and bundled are both 2026.08.28.1; a Herdr client prefers the remote copy |
| `copilot` | published | 2026.08.29.1 | 2026.08.29.1 | published and bundled are both 2026.08.29.1; a Herdr client prefers the remote copy |
| `cursor` | published | 2026.08.03.1 | 2026.08.03.1 | published and bundled are both 2026.08.03.1; a Herdr client prefers the remote copy |
| `devin` | published | 2026.06.15.1 | 2026.06.15.1 | published and bundled are both 2026.06.15.1; a Herdr client prefers the remote copy |
| `droid` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `gemini` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `grok` | bundled | 2026.07.16.2 | 2026.07.16.1 | bundled 2026.07.16.2 is newer than published 2026.07.16.1; a Herdr client ignores the older remote copy |
| `hermes` | published | 2026.07.24.1 | 2026.07.24.1 | published and bundled are both 2026.07.24.1; a Herdr client prefers the remote copy |
| `kilo` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `kimi` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `kiro` | published | 2026.08.01.1 | 2026.08.01.1 | published and bundled are both 2026.08.01.1; a Herdr client prefers the remote copy |
| `maki` | published | 2026.07.09.2 | 2026.07.09.2 | published and bundled are both 2026.07.09.2; a Herdr client prefers the remote copy |
| `muse` | bundled | 2026.08.26.1 | — | bundled only; the published catalog does not list this agent |
| `opencode` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `pi` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `qodercli` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `qwen` | published | 2026.08.14.1 | 2026.08.14.1 | published and bundled are both 2026.08.14.1; a Herdr client prefers the remote copy |

## Regex compatibility

4 pattern(s) that Rust's `regex` crate accepts cannot compile under Go's RE2. The vendored files keep them verbatim; an overlay carries the rewrite. See `docs/reference/herdr-detection-parity.md`.

- `upstream/antigravity.toml` rule `spinner_working` line_regex: `^\s*[\u2800-\u28FF]+\s+\p{Alphabetic}+\w*ing\b`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`
- `upstream/cursor.toml` rule `spinner_working` line_regex: `^\s*(⬡|⬢|[\u2800-\u28FF]+)\s+\p{Alphabetic}+\w*ing\b`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`
- `upstream/kiro.toml` rule `tool_spinner_working` line_regex: `^\s*(◔|◑|◕|●)\s+\p{Alphabetic}`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`
- `upstream/qodercli.toml` rule `spinner_working` line_regex: `^\s*[\u2800-\u28FF]\s+.*\p{Alphabetic}`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`

## Alias table

23 agents in Herdr's `lookup_agent`; generic runtimes: `bash`, `bun`, `cmd`, `fish`, `node`, `powershell`, `pwsh`, `sh`, `tmux`, `zsh` (plus python, or python<segment>[.<segment>...] where every dot-separated segment after the prefix is a non-empty run of ASCII digits (is_python_runtime)).

Every Herdr alias for a family Sidecar already claims appears literally in `internal/agentactivity/activity.go`.

## Authority gaps

Herdr's published authority is a *target*. Sidecar tiers are earned by traces and are never copied.

**Below target** marks an agent Herdr gives lifecycle authority to *through hooks* and Sidecar has not proved `full` for. That is the same rule `TestHerdrAuthorityGaps` prints, so this table and `go test ./internal/agentlifecycle/` name the same set.

| Agent | Herdr authority | Sidecar tier | Below target |
| --- | --- | --- | --- |
| `agy` | session_identity | screen-fallback |  |
| `amp` | none | screen-fallback |  |
| `claude` | session_identity | session-identity |  |
| `cline` | none | — |  |
| `codex` | session_identity | session-identity |  |
| `copilot` | session_identity | screen-fallback |  |
| `cursor` | session_identity | screen-fallback |  |
| `devin` | session_identity | — |  |
| `droid` | session_identity | — |  |
| `gemini` | none | — |  |
| `grok` | session_identity | screen-fallback |  |
| `hermes` | session_identity | — |  |
| `kilo` | hooks | — | yes |
| `kimi` | hooks | — | yes |
| `kiro` | none | — |  |
| `maki` | none | — |  |
| `mastracode` | hooks | — | yes |
| `omp` | hooks | — | yes |
| `opencode` | hooks | full |  |
| `pi` | hooks | session-identity | yes |
| `qodercli` | session_identity | — |  |
| `qwen` | session_identity | — |  |

## Integration assets

Vendored verbatim from `src/integration/assets` into `internal/agentintegration/upstream/`, pinned by `upstream.lock.json` there. They are reference material: Sidecar installs its own assets and these exist so a re-port is a diff.

| Agent | Asset directory | Version | Previous | Change | Sidecar port |
| --- | --- | --- | --- | --- | --- |
| `agy` | `antigravity_cli` | 3 | 3 | unchanged | not ported |
| `claude` | `claude` | 9 | 9 | unchanged | `claude` from version 9 |
| `codex` | `codex` | 8 | 8 | unchanged | `codex` from version 8 |
| `copilot` | `copilot` | 3 | 3 | unchanged | not ported |
| `cursor` | `cursor` | 1 | 1 | unchanged | not ported |
| `devin` | `devin` | 2 | 2 | unchanged | not ported |
| `droid` | `droid` | 3 | 3 | unchanged | not ported |
| `grok` | `grok` | 1 | 1 | unchanged | not ported |
| `hermes` | `hermes` | 5 | 5 | unchanged | not ported |
| `kilo` | `kilo` | 4 | 4 | unchanged | not ported |
| `kimi` | `kimi` | 7 | 7 | unchanged | not ported |
| `mastracode` | `mastracode` | 2 | 2 | unchanged | not ported |
| `omp` | `omp` | 9 | 9 | unchanged | not ported |
| `opencode` | `opencode` | 10 | 10 | unchanged | `opencode` from version 10 |
| `pi` | `pi` | 8 | 8 | unchanged | not ported |
| `qodercli` | `qodercli` | 3 | 3 | unchanged | not ported |
| `qwen` | `qwen` | 1 | 1 | unchanged | not ported |

### Upstream changes since each Sidecar port

`ported-from` is recorded in `internal/agentintegration/portedfrom.go`, not in an asset header: two of the three Sidecar assets are Go values with no header to carry it. A comparison is made on bytes rather than on the version number, so a file upstream edited without bumping still shows here.

#### `opencode` — ported from herdr `opencode` version 10

Compared against `4a3b04f5`; upstream is now at version 10.

No upstream change: all 4 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `codex` — ported from herdr `codex` version 8

Compared against `4a3b04f5`; upstream is now at version 8.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `claude` — ported from herdr `claude` version 9

Compared against `4a3b04f5`; upstream is now at version 9.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

## Fixture verdict flips

Every fixture in `internal/agentactivity/testdata` with a `screen:` block, classified against the manifests this sync replaced and against the ones it wrote. A verdict is the state, the matched rule id, and the fallback reason: the same triple `scripts/herdr-diff.sh` compares. The Sidecar overlays are applied to **both** sides, because a sync never touches them and applying them to one side would report every overlay rule as a flip. Sidecar's process gate is not applied: it reads the pane's process name and never the manifest, so its answer is the same on both sides and it cannot create or hide a flip.

**No fixture changed verdict.** 61 fixture(s) reach the same state, the same matched rule and the same fallback reason under the new manifests as under the old ones. That is the expected result and it is what the review gate is for.

## Overlay rules

Each rule is removed on its own from the manifests this sync wrote, and the corpus is reclassified. A rule that changes no verdict has stopped earning its place, which is the signal that upstream has adopted the same idea and the rule can go. Redundancy is judged on the state and the fallback reason alone, never on the rule id: a `sidecar.` id can never equal the upstream id that would win without it, so folding the id in would make the check unreachable.

A rule carrying an **upstream** id is not in that bucket and is never a deletion candidate. It replaces upstream's rule rather than adding one, so removing it leaves a rule that is dead (the `\p{Alphabetic}` rewrites RE2 cannot compile) or differently flagged (the copies that only add `visible_blocker`), which is a regression rather than a cleanup. Those rows are judged on the matched rule id and the visible flags as well, and say when no fixture covers them at all.

| Overlay | Rule | Kind | Effect |
| --- | --- | --- | --- |
| `antigravity` | `spinner_working` | replaces upstream | no fixture covers it; upstream's own rule stands without it |
| `antigravity` | `sidecar.trust_prompt_blocked` | addition | changes 1 fixture(s): `blocked.txt` |
| `antigravity` | `sidecar.permission_prompt_blocked` | addition | changes 1 fixture(s): `permission_prompt.txt` |
| `antigravity` | `sidecar.status_footer_working` | addition | changes 1 fixture(s): `working.txt` |
| `claude` | `sidecar.overlay_retain` | addition | changes 1 fixture(s): `overlay.txt` |
| `claude` | `sidecar.allow_prompt_blocker` | addition | changes 1 fixture(s): `allow_prompt.txt` |
| `claude` | `sidecar.background_agents_waiting` | addition | **changes nothing: deletion candidate**; without it working via `sidecar.background_agents_footer_working` |
| `claude` | `sidecar.background_agents_footer_working` | addition | changes 1 fixture(s): `background_agents_footer.txt` |
| `claude` | `legacy_no_prompt_blocker` | replaces upstream | changes 1 fixture(s): `legacy_permission_wait.txt` |
| `codex` | `sidecar.background_terminal_working` | addition | changes 1 fixture(s): `background_terminal.txt` |
| `codex` | `osc_title_idle` | disables upstream | changes 3 fixture(s): `completed.txt`, `interrupted.txt`, `startup_idle.txt` |
| `codex` | `sidecar.composer_idle` | addition | changes 3 fixture(s): `completed.txt`, `interrupted.txt`, `startup_idle.txt` |
| `codex` | `sidecar.working_chrome` | addition | changes 1 fixture(s): `tool_running_composer.txt` |
| `codex` | `sidecar.approval_blocker` | addition | changes 1 fixture(s): `approval_prompt.txt` |
| `codex` | `weak_blocker` | replaces upstream | changes 1 fixture(s): `weak_blocker.txt` |
| `cursor` | `spinner_working` | replaces upstream | changes 1 fixture(s): `working_spinner.txt` |
| `cursor` | `sidecar.decision_blocked` | addition | changes 1 fixture(s): `blocked_decision.txt` |
| `cursor` | `sidecar.finished_background_tasks_idle` | addition | changes 1 fixture(s): `false_positive_finished_background.txt` |
| `cursor` | `sidecar.background_suffix_working` | addition | changes 1 fixture(s): `working_background.txt` |
| `grok` | `sidecar.background_subagent_working` | addition | changes 1 fixture(s): `background_subagent.txt` |
| `grok` | `sidecar.overlay_retain` | addition | changes 1 fixture(s): `overlay.txt` |
| `grok` | `sidecar.working_footer` | addition | changes nothing, and the overlay declares it `harness-exempt`: no fixture holds the screen it is for, and its case is a Go test |
| `grok` | `sidecar.idle_footer` | addition | changes 1 fixture(s): `stale_working_scrollback.txt` |
| `kiro` | `tool_spinner_working` | replaces upstream | changes 1 fixture(s): `tool_spinner_working.txt` |
| `muse` | `sidecar.thinking_working` | addition | changes 1 fixture(s): `thinking.txt` |
| `qodercli` | `spinner_working` | replaces upstream | changes 1 fixture(s): `spinner_working.txt` |

1 overlay rule(s) changed no fixture verdict. Delete the rule, or record why it stays and add the fixture that proves it. Deleting one is a separate change with a fixture attached, so this report flags it rather than making it.


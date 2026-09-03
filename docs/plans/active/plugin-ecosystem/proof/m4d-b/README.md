# M4d-b proof captures

Stripped `tmux-drive.sh` captures at 160x45, from fully isolated runs: a private tmux socket, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` all under `/tmp/m4db-proof/`, with `./scripts/tmux-drive.sh paths` confirmed before each run and `stop` after it. Nothing resolved under `~/.local/state/sidecar` or `~/.config/sidecar`.

Two binaries were built for these captures, both with `GOWORK=off` so each compiles the same pinned dependencies:

- `sidecar-branch` — this worktree at the M4d-b commits.
- `sidecar-main` — `git archive main | tar -x` into a scratch tree, built there. `main` was `56b6a199`, which is this branch's own starting point, so it is the code exactly before `modal.Select` existed.

The plugin is `internal/pluginhost/testdata/fixtureprovider`, configured as one `plugins.external` instance with `plugin_protocol` on and run with the new `-wide-filters` flag: it declares the `source` filter with twenty choices instead of four, which is what a filter too long for its control looks like. Nothing else about the fixture changes.

| Capture | What it shows |
| --- | --- |
| `01-view-modal-selectors.stripped.txt` | The browser's View modal, every control now a `modal.Select`. The two sort keys draw as the segmented `[ Relevance | Recency ]`; `Scope  (scope)` and `Source` draw as `❯`-cursor lists — under five choices, but with labels too wide for a segmented row in a 44-column modal, which is the width floor under the count rule. `Source` shows six of its twenty rows with `↓ more below`, and `Since` and `Done` are still on the box. |
| `02-twenty-choice-selector-scrolled.stripped.txt` | The same modal after Tab, Tab and eight `j`. The `Source` selector has scrolled inside its own six-row window — `↑ more above`, `mail … git` with the cursor on `git`, `↓ more below` — and every other control is exactly where it was. A twenty-choice filter costs the modal six rows, not twenty. |
| `03-create-workspace-this-branch.stripped.txt` | The Create Workspace modal on the global Workspaces surface, from `sidecar-branch`: the seven-row kind list with `❯ Worktree` selected and the description column aligned, then Project, Name, Base Branch, Agent. |
| `04-create-workspace-main.stripped.txt` | The same screen, same key sequence, from `sidecar-main`. |

## The Create Workspace modal is unchanged

Captures 03 and 04 were taken by one script — `create-capture.sh`, kept here: fresh run root, `start 160 45`, `8`, `n`, `snap`, `stop` — run twice, once per binary. The **raw** captures — ANSI escapes included, before any stripping — are byte-identical:

```
$ cmp create-branch.txt create-main.txt && echo bytes-equal
bytes-equal
$ md5 -q create-branch.txt create-main.txt
7932e425f1f0ef33b59e83748a6f9fa5
7932e425f1f0ef33b59e83748a6f9fa5
```

That is the promotion's claim: the kind chooser now *is* `modal.Select`, and the modal draws what it drew before, colour for colour. The same claim is held by `internal/workspacecreate/testdata/kindcontrol/`, eight goldens recorded from the pre-change code covering both shapes, three catalog sizes, a disabled row, and the focused and unfocused frames.

## What deliberately did change

A disabled kind row can no longer be selected by arrow or click. Its reason still rides the row, and the create paths still refuse it, but a choice that cannot be created is not a choice to land on. Nothing in these captures shows it, because the live-terminal cap is not reached in an empty isolated run; `TestSelectDisabledRowIsNotSelectable` and `TestSwitcherDisabledRowKeepsReasonInline` cover it.

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

## td-c6904c: the modal grows, and the focused selector is bordered

Captures 05 to 07 are the follow-up fix. The maintainer's screenshot of the Recall View modal on main `33c8e100` showed the segmented sort control truncated to `[ Relevance | Source | Up… ]` in a modal that would not grow, and three list selectors each painting a gold row with nothing saying which one had the keyboard. The cause of the first was a measurement: `segmentedWidth` left out the two columns `styles.Button` pads each segment with, so a three-segment control measured twelve columns narrower than it drew and the width floor under the count rule never engaged.

Same isolation as above, a fresh run root at `/private/tmp/c6904c-proof/run` — private tmux socket, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` — with `./scripts/tmux-drive.sh paths` confirmed before the run and `stop` after it. Nothing resolved under `~/.local/state/sidecar` or `~/.config/sidecar`. Binary built from this worktree with `GOWORK=off`.

**The fixture gained `-wide-sort`** alongside `-wide-filters`, declaring the `results` collection with three sort keys — `Relevance`, `Source`, `Updated` — instead of two. Three is what Recall declares, and it is the shape that truncated: two keys fit 46 columns even with the padding counted, so nothing in the existing fixture could ask a modal to grow. The flag is off by default and touches nothing else.

| Capture | What it shows |
| --- | --- |
| `05-view-modal-grown-to-its-widest-control.stripped.txt` | The View modal at 160x45, opened on the sort control. The modal is no longer 46 columns: it grew to hold its widest control, which is `Scope`'s `[   Everything   |   This project   |   Notes only   ]`. All three sort segments are whole — `[   Relevance   |   Source   |   Updated   ]`, no `Up…` — and both controls now fit as segments where at 46 columns the Scope control had to fall back to a list. `Source`, at twenty choices, is a list inside a rounded border with `↓ more below` inside the box with the rows it belongs to. Exactly one control is lit: the sort control's `[ ]` frame is `Primary` `#c0982f`, while the Scope frame and the Source border are both `BorderNormal` `#3d444a`. |
| `06-view-modal-focused-selector-border.stripped.txt` | The same modal after two Tabs. The `Source` list's border is now `Primary` `#c0982f` and both segmented frames are `BorderNormal` — the keyboard moved and the border moved with it. Exactly one bordered-in-Primary selector, which is what the screenshot had no way to show. |
| `07-create-workspace-bordered-kind-list.stripped.txt` | The Create Workspace modal on the global Workspaces surface with the kind list focused. The seven-row list is inside the same rounded border, in `Primary`, while every input border below it is `BorderNormal`. Taken with the kind list unfocused first, the same modal draws the list border in `BorderNormal` and the focused `Name` input's border in `Primary` — one ladder, two controls. |

The colours above were read out of the raw captures before stripping, not inferred from the shapes.

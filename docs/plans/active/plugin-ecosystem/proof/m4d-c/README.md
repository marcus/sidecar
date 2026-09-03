# M4d-c proof captures

`tmux-drive.sh` captures at 160x45, from a fully isolated run: a private tmux socket, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` all under `/tmp/m4d-c-proof-run`, with `./scripts/tmux-drive.sh paths` confirmed before the run and `stop` after it. Nothing resolved under `~/.local/state/sidecar` or `~/.config/sidecar`.

The binary was built from this worktree and passed as `SIDECAR_BIN`. The plugin is `internal/pluginhost/testdata/fixtureprovider`, built from this worktree and configured as one `plugins.external` instance with `plugin_protocol` on. Its `sweep` query is the twelve-row page M4d-a added.

Every gesture below is a real pointer gesture, not a key: SGR mouse reports were written straight into the pane (`tmux-drive.sh keys -l $'\e[<0;COL;ROWM'` to press, `\e[<32;COL;ROWM` to drag with the button held, `\e[<0;COL;ROWm` to release), which is how the M4a rail drag was proven.

| Capture | What it shows |
| --- | --- |
| `01-browser-detail-selection.stripped.txt` | The plugin browser with `rc:notes:2` open in the detail box, after a press at the box's first text column on the body row, a drag down and right, and a release. Stripped, this is the frame; the highlight is in the file beside it. |
| `01-browser-detail-selection.highlight-rows.txt` | The four rows of that frame that carry `\e[48;2;38;79;120m` — the selection background — spanning the body line, the `── Evidence ──` rule, the blank row between them and the evidence line. Four rows from one drag: the selection crosses rules and blank rows as one continuous block, which is the shared engine's blank-row rule doing its job. |
| `02-issue-pane-selection.stripped.txt` | A td issue card (`td-62b81c`) open as an Issue leaf in the app's content deck, beside Files and the document that was clicked to open it. The same press-drag-release over the card's text. |
| `02-issue-pane-selection.highlight-rows.txt` | The three rows of that frame carrying the selection background: the title row, the meta row and the labels row. |
| `03-copy-notice.stripped.txt` | `alt+c` on that selection. The header flash reads `● Copied 3 line(s)` — the shared pipeline's own wording, from `textselect.Keys.Notice`, after `clip.Copy` wrote both clipboards. |

## How the issue leaf was opened

Through the app itself, not a flag: Files previewed `docs/plans/active/plugin-ecosystem/browser-parity-and-scope.md`, and a click on the `td-62b81c` token in its body opened the Issue leaf in the content deck. That is the ordinary content-link path, so the capture is of the pane a reader actually gets.

## What the captures do not show

- **The resource and diff panes.** Both need a live backend the isolated run does not have — a configured resource provider, and a worktree with changes — and both are the same adapter over their own rows. They are pinned by tests instead: `internal/resourceview/select_test.go` and `internal/workspacediff/select_test.go` each drag over two rows, copy through the `clip.Copy` sequence, double-click a word, and assert an untouched pane paints no highlight.
- **The abandon rules.** A modal opening, focus leaving and the document changing all drop the selection, and a still cannot show a thing that is no longer there. `TestOpeningAModalAbandonsTheDetailSelection`, `TestAModalAbandonsTheIssueSelection`, `TestANewDocumentDropsTheDetailSelection` and `TestScrollingTheDiffDropsTheSelection` are what hold them.

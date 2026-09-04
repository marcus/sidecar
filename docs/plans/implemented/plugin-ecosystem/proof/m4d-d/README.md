# M4d-d proof captures

Stripped `tmux-drive.sh` captures at 160x45, from a fully isolated run: a private tmux socket, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` all under `/private/tmp/sidecar-drive-501`, with `./scripts/tmux-drive.sh paths` confirmed before the run and `stop` after it. Nothing resolved under `~/.local/state/sidecar` or `~/.config/sidecar`. The binary was built from this worktree and passed as `SIDECAR_BIN`.

| Capture | What it shows |
| --- | --- |
| `01-tree-bar-typed.stripped.txt` | The Files tree bar after `/` and `internal browser`. It is the app's query bar: `/ internal browser▌`, the match state right-aligned as `(no matches)`, and the `×` right of that because there is a query to drop. |
| `02-tree-bar-after-alt-backspace.stripped.txt` | One `alt+backspace` later: `/ internal ▌`. One keypress deleted the word before the caret. Before M4d-d this row appended and backspaced and nothing else, so the same key did nothing at all. |
| `03-tree-bar-caret-at-the-start.stripped.txt` | `home` then `x`: `/ x▌internal`. The caret moves, and the caret is drawn where it actually is rather than always at the end. |
| `04-preview-bar-with-its-count.stripped.txt` | The Files preview content search over `AGENTS.md` for `sidecar shell`. Same row, this surface's own right cell: `(1/3)`, then the `×`. It is one screen row now rather than two, because it no longer carries `styles.ModalTitle`'s bottom margin — `contentSearchBarRows` is what keeps the tree hit regions on the rows they name. |
| `05-notes-bar-after-alt-backspace.stripped.txt` | The Notes list bar after `release notes` and one `alt+backspace`: `/ release ▌`, with the list re-filtered to `5/8` in the header beside it. |

## What the captures do not show

- **The `×` being clicked.** `tmux-drive.sh` sends keys, not pointer events. The clicks are pinned by tests instead: `TestSearchClearControls` in `internal/plugins/filebrowser` clicks both registered regions and asserts each query is dropped and each region gone when there is nothing to clear, and `TestSessionsSearchClearControl` does the same for the Sessions row.
- **The bars with no full-width row of their own.** The two terminal bars and the in-note prompt are segments of a status line; the git history and path-filter bars are modal overlays. They are pinned by their packages' tests rather than by a capture.
- **Paste.** A bracketed paste is not a keystroke `tmux-drive.sh` can send. Every surface has a test that a `tea.PasteMsg` lands in its bar at the caret and re-matches.

# M4d-a proof captures

Stripped `tmux-drive.sh` captures at 160x45, from a fully isolated run: a private tmux socket, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` all under `/tmp/m4d-a-proof/run`, with `./scripts/tmux-drive.sh paths` confirmed before the run and `stop` after it. Nothing resolved under `~/.local/state/sidecar` or `~/.config/sidecar`.

The binary was built from this worktree and passed as `SIDECAR_BIN`. The plugin is `internal/pluginhost/testdata/fixtureprovider`, built from this worktree and configured as one `plugins.external` instance with `plugin_protocol` on. Its `sweep` query is the page M4d-a added to the fixture: twelve answered rows on one page, with no cursor, so a cursor can be moved down it and what that cost can be counted.

| Capture | What it shows |
| --- | --- |
| `01-query-focused-mid-word.stripped.txt` | The query row after typing `sweep schema notes`, moving the caret left five characters, and pressing `alt+backspace`. The row reads `/ sweep ▌notes`: one keypress deleted the word before the caret, the caret is drawn where it actually is rather than at the end, and the tail survived. The `×` is on the right of `12 results   answered`, because there is a query to drop. Search-as-you-type has already re-listed against `sweep notes`. |
| `02-detail-follows-the-cursor.stripped.txt` | The cursor on row 4 of the `sweep` page with no Enter pressed on it: the box beside the list shows `rc:notes:4`, its fields, evidence and attributes. The detail followed the cursor after the quiet period. |
| `03-detail-after-three-more-rows.stripped.txt` | Three rows further down, `j` at a time. The cursor is on row 7 and the box shows `rc:notes:7` — the same box, replaced only when each load landed. Between the two frames the earlier document stayed on screen; the tests are what pin that (a ten-row sweep with a `View()` assertion on every step, over the fake host and over the real `pluginhost.Manager`), because a capture can only show where a sweep ended. |
| `04-k-on-row-one-returns-to-the-query.stripped.txt` | `k` pressed until the cursor reaches row 1, then once more. The caret is back in the query row with `sweep` intact, and the footer has swapped to the query row's own context: `enter  Search   esc, ctrl+u  Clear`. |
| `05-down-hands-the-keyboard-to-row-one.stripped.txt` | `down` from that focused query row. The caret is gone, the cursor is on row 1, the query text is unchanged, and the footer is the list's again — `j, k  Move   enter  Open   /  Query …`. |
| `06-cleared-query-has-no-clear-control.stripped.txt` | `ctrl+u` in the query row. The row is `/ ▌` with `no query` on the right and **no `×`**: the control is drawn, and registered, only where there is something to clear. `Clear` is gone from the footer for the same reason, and the collection is back to `waiting for a query` because clearing re-listed. |

## What the captures do not show

- **The `×` being clicked.** `tmux-drive.sh` sends keys, not pointer events. The click is pinned by tests instead: `TestTheClearControlAndTheFilterClearCommand` in `internal/pluginbrowser` clicks the registered region and asserts the query is dropped and the region gone, and `TestFilterClearRegionExistsOnlyWithAQuery` does the same for both Workspaces surfaces through `workspacelist.RegionFilterClear`.
- **The frame mid-sweep.** A capture is a still, and the claim being made is about what is on screen *between* two loads. `TestTheDetailFollowsTheCursorAtMostOncePerSweep` and `TestLiveCursorSweepCostsOneGet` render the frame after every one of the ten keypresses and fail if the document ever leaves it.

## One thing worth recording

In capture 06 the detail box still shows `rc:notes:1` after the query was cleared and the list emptied. That is the browser's existing rule — a document stays until something replaces it — and it is not new here; nothing in M4d-a blanks the box, deliberately. It is listed as a limit in the plan's M4d-a deviations rather than fixed, because "what happens to an open document when its list goes away" is a question for the milestone that has a second opinion about it.

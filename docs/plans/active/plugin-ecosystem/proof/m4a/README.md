# M4a proof captures

Stripped `tmux-drive.sh` captures at 160x45, from a fully isolated run: a private tmux socket, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` all under `/tmp/m4a-proof/run`, with `./scripts/tmux-drive.sh paths` confirmed before the run and `stop` after it. Nothing resolved under `~/.local/state/sidecar` or `~/.config/sidecar`.

The binary was built from this worktree and passed as `SIDECAR_BIN`. The plugin is `internal/pluginhost/testdata/fixtureprovider`, configured as one `plugins.external` instance with `plugin_protocol` on — the same fixture `internal/pluginbrowser/live_test.go` drives. Two of its own modes stand in for data the fixture's normal page does not have: the query `mode:over-limit-page:dex` asks for a 550-row page (the host clamps it to 500), and the instance was launched once with `-mode=over-limit-document` so a document taller than its box exists to scroll. Both are the fixture's declared hostile modes, reached without changing it.

| Capture | What it shows |
| --- | --- |
| `01-rail-scrollbars-query.stripped.txt` | The whole of M4a in one frame: the `┃` rail between the two boxes, the list's scrollbar and the detail's, and the query row focused — `Title` style, the `▌` block caret, and `500 of 550   answered` right-aligned. The View pill sits on the title row at its full `⇅ Relevance` rung. |
| `02-same-frame-after-theme-change.stripped.txt` | The same frame after switching to Tokyo Night Storm mid-session. Stripped, it is byte-identical to capture 01; raw, every colour moved (`38;2;191;148;47` to `38;2;122;164;247` on the first glyph), including the detail body. |
| `03-after-a-live-rail-drag.stripped.txt` | After an SGR mouse press on the rail at column 98 and a drag to column 78. The split moved, both boxes re-laid out, and the table re-fitted its columns to the narrower list. |
| `04-relaunch-restores-the-split.stripped.txt` | A second launch of the same binary against the same isolated state: the browser opens at the dragged 49% rather than the 61% default. `state.json` in the run's config dir holds `"pluginBrowserSplit": {"fixture": 49}`. |
| `05-coverage-card-from-the-notice-key.stripped.txt` | The degraded page's coverage card, opened with `c` — the outcome word, Sidecar's own sentence for what it means, and the notice in full rather than truncated to its row. The pointer opens the same card from the outcome cell or the notice. |

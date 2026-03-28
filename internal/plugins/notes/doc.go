// Package notes implements a two-pane notes plugin for sidecar.
//
// Notes are persisted in SQLite (co-located with td's issues.db) and support
// fuzzy search, pin/archive/soft-delete, undo, auto-save, and inline vim
// editing via a tmux PTY backend. The left pane is a filterable note list;
// the right pane shows a read-only preview or a full editor.
package notes

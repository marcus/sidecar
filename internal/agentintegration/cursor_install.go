package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strconv"
)

// The Cursor Agent adapter.
//
// Cursor keeps hooks in ~/.cursor/hooks.json: a `version` number and a `hooks`
// object whose events each hold a flat array of handlers. Sidecar registers one
// on `sessionStart`, which is the event Herdr's cursor integration subscribes
// to at version 1 and the only one that names a session before a turn runs.
//
// Cursor's event names are camelCase where Claude's are TitleCase, and the
// payload's field names are the reverse: snake_case, `session_id` and
// `hook_event_name`, exactly Claude's spelling. That crossing is not a mistake
// in either project and it is why the entry spec carries an event key
// separately from everything else.
//
// # Where it installs, and the override that would have been wrong to honour
//
// cursor-agent 2026.08.25 has a configuration-directory resolver that reads
// CURSOR_CONFIG_DIR, then $XDG_CONFIG_HOME/cursor, then ~/.cursor. Its hook
// loader does not use it: the user hooks path is built from `homedir()` and
// `.cursor` directly, in the same bundle. So honouring CURSOR_CONFIG_DIR here,
// as Herdr does, would put Sidecar's entry in a directory that resolver
// describes and the hook loader never opens. Sidecar targets $HOME/.cursor,
// which is where the loader looks, and a proof run redirects Cursor by moving
// HOME.
//
// This is the same rule that made Claude's CLAUDE_CONFIG_DIR worth honouring
// and Antigravity's ANTIGRAVITY_CLI_CONFIG_DIR worth ignoring: follow the code
// path that actually reads the file.
//
// # The entry shape is the minimal one
//
// Herdr writes `{"command": "..."}` and nothing else, per Cursor's published
// hooks documentation, and this port keeps that. Cursor's own writer emits
// `{"type": "command", "command": "..."}` when it generates a hooks.json, so
// both forms are accepted; the scan treats an absent `type` as a command
// handler for exactly this reason.
//
// # Cursor reads Claude's settings too
//
// The same bundle resolves ~/.claude/settings.json and the project-local
// Claude settings as hook sources. So Sidecar's Claude entry also fires inside
// cursor-agent sessions carrying `--kind claude`, and is refused there by the
// same gate that refuses it inside grok: report-session verifies the claimed
// kind against the pane's own occupant. The cursor entry beside it binds the
// session as cursor.

// Cursor integration identity.
const (
	CursorProvider = "cursor"
	// CursorCommand is the executable. `cursor` is the IDE launcher on a
	// machine with Cursor installed, and `cursor --version` is recorded in
	// source.go as a probe that never exits; `cursor-agent` is the CLI this
	// integration is for, and it is what the catalog family names too.
	CursorCommand = "cursor-agent"
	CursorSource  = "sidecar.cursor.hooks"

	// CursorAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the canonical
	// history so installed copies read as outdated rather than damaged.
	CursorAssetVersion = "1"

	// CursorAssetSchema is the plan/marker schema the asset declares.
	CursorAssetSchema = 1

	// CursorEvent is the event the entry is registered on. Cursor spells the
	// session start camelCase.
	CursorEvent = "sessionStart"

	// CursorSchemaVersion is the `version` number Cursor's own writer puts at
	// the top of a hooks.json it generates. Herdr adds it to any file lacking
	// one; Sidecar writes it only into a file it creates itself, because
	// 2026.08.25's hook loader never reads the key.
	CursorSchemaVersion = 1
)

// cursorCanonicalEntry is the exact handler version 1 ships.
//
// It carries a command and nothing else, which is the minimal shape Cursor
// documents and the one Herdr writes. A `type` key would be accepted too, and
// is not added: an entry that matches upstream byte for byte is one whose diff
// on the next Herdr bump is empty.
func cursorCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "command", val: mustJSONString(reportSessionCommand(CursorProvider))},
	})
}

func cursorEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		events: []string{CursorEvent},
		flat:   true,
		canonical: []versionedEntry{
			{version: CursorAssetVersion, entry: cursorCanonicalEntry()},
		},
	}
}

// CursorHooksHome resolves the directory cursor-agent reads its user hooks
// from. See the note above on why no environment override is honoured.
func CursorHooksHome(home string) string {
	return filepath.Join(home, ".cursor")
}

// CursorAdapter installs Sidecar's Cursor session-identity hook.
type CursorAdapter struct{ sessionHookAdapter }

// NewCursorAdapter returns the adapter this build ships.
func NewCursorAdapter() CursorAdapter {
	return CursorAdapter{sessionHookAdapter{integration: sessionHookIntegration{
		provider:     CursorProvider,
		command:      CursorCommand,
		source:       CursorSource,
		assetVersion: CursorAssetVersion,
		assetSchema:  CursorAssetSchema,
		fileName:     "hooks.json",
		dir:          func(env Env) string { return CursorHooksHome(env.Home) },
		spec:         cursorEntrySpec(),
		item:         cursorCanonicalEntry,
		// cursor-agent's own writer puts a `version` at the top of a hooks.json
		// it generates, so a file Sidecar creates looks like one Cursor
		// created. It is NOT added to a file the user already has: 2026.08.25's
		// hook loader never reads the key -- the word `version` appears in that
		// module only in sync-conflict messages and in `cursor_version` on the
		// payload -- so adding it would be editing bytes outside the entry for
		// no effect, and would leave uninstall unable to give the file back
		// byte for byte. Herdr does add it unconditionally.
		newFileMembers: []jsonMember{{key: "version", val: json.RawMessage(strconv.Itoa(CursorSchemaVersion))}},
	}}}
}

// CursorPaths returns the paths the Cursor adapter inspects and touches.
func CursorPaths(env Env) []string {
	return []string{NewCursorAdapter().integration.pathsFor(env).File}
}

var _ Adapter = CursorAdapter{}

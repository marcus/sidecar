package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
)

// The Droid (Factory CLI) adapter.
//
// Droid keeps hooks in ~/.factory/settings.json, in the nested group shape
// Claude Code uses, and Sidecar adds exactly one session-identity entry under
// SessionStart. That is upstream's whole table: Herdr's droid integration at
// version 3 has one row, DROID_HOOK_EVENTS = [("SessionStart", "session")],
// and its nine lifecycle rows were removed at that version rather than kept.
//
// Two facts about Droid that a reader will otherwise have to rediscover.
//
// It has no configuration-directory override. Herdr's droid_dir is
// home_dir()?.join(".factory") with nothing consulted, and Factory's own
// settings reference names ~/.factory/settings.json and documents no variable
// that moves it. So this is the one provider in this group whose configuration
// a proof run can only redirect by moving HOME, and that is stated rather than
// worked around with a variable nobody promised to honour.
//
// And ~/.factory/hooks.json SHADOWS the hooks key in settings.json. Factory's
// hooks reference is explicit: Droid reads hook declarations from hooks.json
// first, and falls back to the hooks key in the matching settings.json only
// when that file is absent. So an entry Sidecar wrote into settings.json while
// hooks.json existed would be correct, would read as current, and would never
// fire. Sidecar inspects that file and says so in the status; it does not edit
// it. Herdr does reach into hooks.json, but only to delete its own stale
// entries from an older layout, and Sidecar has never written one there.

// Droid integration identity.
const (
	DroidProvider = "droid"
	DroidSource   = "sidecar.droid.hooks"

	// DroidAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the canonical
	// history so installed copies read as outdated rather than damaged.
	DroidAssetVersion = "1"

	// DroidAssetSchema is the plan/marker schema the asset declares.
	DroidAssetSchema = 1

	// DroidShadowFile is the file whose presence makes settings.json's hooks
	// key inert. See the note above.
	DroidShadowFile = "hooks.json"
)

// droidCanonicalEntry is the exact handler version 1 ships.
//
// The timeout is in seconds: Factory's hooks reference documents `timeout` as
// seconds with a default of 60, which is the same unit Herdr's value of 10
// assumes and the same unit Claude and Codex use.
func droidCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(reportSessionCommand(DroidProvider))},
		{key: "timeout", val: json.RawMessage(strconv.Itoa(hookTimeoutSec))},
	})
}

// droidCanonicalGroup is the group the entry is installed in. It carries no
// matcher, because install_droid calls ensure_command_hook with matcher None.
func droidCanonicalGroup() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "hooks", val: marshalJSONArray([]json.RawMessage{droidCanonicalEntry()})},
	})
}

func droidEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		canonical: []versionedEntry{
			{version: DroidAssetVersion, entry: droidCanonicalEntry()},
		},
	}
}

// DroidConfigDir resolves the directory the Factory CLI keeps its settings in.
// There is no override to honour; see the note above.
func DroidConfigDir(home string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".factory")
}

// droidShadowNote is the one sentence a user needs when their hooks live in the
// file that wins.
func droidShadowNote(shadow string) string {
	return shadow + " exists, and droid reads hook declarations from it in preference to the hooks key in " +
		"settings.json, so an entry installed here would never fire; move your hooks into settings.json, or " +
		"add Sidecar's entry to " + shadow + " yourself"
}

// droidIntegration is the Droid integration as data.
func droidIntegration() sessionHookIntegration {
	return sessionHookIntegration{
		provider: DroidProvider,
		// The catalog records `droid` as both the id and the command; see
		// internal/agentcatalog/families/droid.toml.
		command:      DroidProvider,
		source:       DroidSource,
		assetVersion: DroidAssetVersion,
		assetSchema:  DroidAssetSchema,
		fileName:     "settings.json",
		dir:          func(env Env) string { return DroidConfigDir(env.Home) },
		spec:         droidEntrySpec(),
		item:         droidCanonicalGroup,
		setupHint: "droid's configuration directory ~/.factory does not exist, so the Factory CLI has not been set " +
			"up on this machine; run droid once and try again",
		shadowedBy: DroidShadowFile,
		shadowNote: droidShadowNote,
	}
}

// DroidAdapter installs Sidecar's Droid session-identity hook entry.
type DroidAdapter struct{ sessionHookAdapter }

// NewDroidAdapter returns the adapter this build ships.
func NewDroidAdapter() DroidAdapter {
	return DroidAdapter{sessionHookAdapter{integration: droidIntegration()}}
}

// DroidPaths returns the paths the Droid adapter inspects and touches.
func DroidPaths(env Env) []string {
	return []string{droidIntegration().pathsFor(env).File}
}

var _ Adapter = DroidAdapter{}

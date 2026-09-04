package agentintegration

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
	// canonical entry changes, and append the superseded entry to the spec's
	// canonical history so installed copies read as outdated rather than
	// damaged.
	DroidAssetVersion = "1"

	// DroidShadowFile is the file whose presence makes settings.json's hooks
	// key inert. See the note above.
	DroidShadowFile = "hooks.json"
)

// droidSpec is the Droid integration as data.
//
// No matcher, because install_droid calls ensure_command_hook with matcher
// None. The timeout is in seconds: Factory's hooks reference documents
// `timeout` as seconds with a default of 60, which is the same unit Herdr's
// value of 10 assumes and the same unit Claude and Codex use.
func droidSpec() sessionEntrySpec {
	return sessionEntrySpec{
		provider: DroidProvider,
		source:   DroidSource,
		// The catalog records `droid` as both the id and the command; see
		// internal/agentcatalog/families/droid.toml.
		command:     DroidProvider,
		version:     DroidAssetVersion,
		dirSegments: []string{".factory"},
		file:        "settings.json",
		matcher:     nil,
		events:      []string{"SessionStart"},
		timeout:     hookTimeoutSec,
		setupHint: "droid's configuration directory ~/.factory does not exist, so the Factory CLI has not been set " +
			"up on this machine; run droid once and try again",
		shadowedBy: DroidShadowFile,
		shadowNote: droidShadowNote,
	}
}

// droidShadowNote is the one sentence a user needs when their hooks live in the
// file that wins.
func droidShadowNote(shadow string) string {
	return shadow + " exists, and droid reads hook declarations from it in preference to the hooks key in " +
		"settings.json, so an entry installed here would never fire; move your hooks into settings.json, or " +
		"add Sidecar's entry to " + shadow + " yourself"
}

// DroidAdapter installs Sidecar's Droid session-identity hook entry.
type DroidAdapter struct{ sessionEntryAdapter }

// NewDroidAdapter returns the Droid adapter.
func NewDroidAdapter() DroidAdapter {
	return DroidAdapter{sessionEntryAdapter{spec: droidSpec()}}
}

// DroidPaths returns the paths the Droid adapter inspects and touches.
func DroidPaths(env Env) []string {
	return []string{droidSpec().pathsFor(env).Settings}
}

var _ Adapter = DroidAdapter{}

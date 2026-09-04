package agentintegration

// The Devin CLI adapter.
//
// Devin keeps hooks inside $XDG_CONFIG_HOME/devin/config.json (~/.config/devin
// when the variable is unset), in the nested group shape Claude Code uses:
// hooks -> event -> [{matcher?, hooks: [{type, command, timeout}]}]. Sidecar
// adds exactly one session-identity entry under each of six events and owns
// nothing else in the file.
//
// SIX events, and this is the one thing about the port that is not obvious.
// Herdr's devin integration at version 2 maps every one of SessionStart,
// UserPromptSubmit, PreToolUse, PostToolUse, PermissionRequest and Stop to the
// same `session` action, and its asset then does something no other Herdr asset
// does: when the payload carries no session id it shells out to
// `devin list --format json` and matches an entry by working directory --
// except on UserPromptSubmit, and except on SessionStart with source=startup,
// where the fallback is explicitly disallowed. Read together those two facts say
// upstream does not believe any single Devin event reliably carries the id. An
// integration that fired only on SessionStart would therefore bind the pane only
// when Devin volunteered the id at startup, and silently never otherwise, which
// is the failure mode a session-identity integration exists to prevent.
//
// So the event list is kept verbatim and the fallback is deliberately NOT
// copied. Sidecar's report-session reads the payload and nothing else: it never
// runs another provider's CLI, never parses its output, and never guesses which
// of several conversations a directory belongs to. A guess here would bind a
// pane to the wrong conversation, and a wrong binding is worse than none because
// a cold restore acts on it. When the payload is silent Sidecar records nothing
// and says so, and the next event that does carry the id binds the pane.
//
// The cost of six entries is six `sidecar agent report-session` processes per
// tool call rather than one per session. That is real, it is the price of
// matching upstream's coverage, and it is recorded in the capability entry.

// Devin integration identity.
const (
	DevinProvider = "devin"
	DevinSource   = "sidecar.devin.hooks"

	// DevinAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the spec's
	// canonical history so installed copies read as outdated rather than
	// damaged.
	DevinAssetVersion = "1"
)

// devinHookEvents are the events Sidecar's entry is installed under, in
// Herdr's own order (DEVIN_HOOK_EVENTS in src/integration/mod.rs at
// HERDR_INTEGRATION_VERSION=2, where every row's action is `session`).
var devinHookEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PermissionRequest",
	"Stop",
}

// devinSpec is the Devin integration as data.
//
// The group carries no matcher, which is what Herdr's install_devin writes:
// ensure_command_hook is called with matcher None for every one of the six
// events, unlike the qodercli and qwen installers beside it which pass "*".
// Writing a matcher Devin's own schema was not observed to want would be a
// difference from upstream introduced by guessing.
func devinSpec() sessionEntrySpec {
	return sessionEntrySpec{
		provider: DevinProvider,
		source:   DevinSource,
		// The catalog records `devin` as both the id and the command; see
		// internal/agentcatalog/families/devin.toml.
		command:           DevinProvider,
		version:           DevinAssetVersion,
		dirSegments:       []string{".config", "devin"},
		dirFromConfigHome: true,
		file:              "config.json",
		matcher:           nil,
		events:            devinHookEvents,
		timeout:           hookTimeoutSec,
		setupHint: "devin's configuration directory does not exist, so the Devin CLI has not been set up on this " +
			"machine; run devin once (or set XDG_CONFIG_HOME to where it keeps its configuration) and try again",
	}
}

// DevinAdapter installs Sidecar's Devin session-identity hook entries.
type DevinAdapter struct{ sessionEntryAdapter }

// NewDevinAdapter returns the Devin adapter.
func NewDevinAdapter() DevinAdapter {
	return DevinAdapter{sessionEntryAdapter{spec: devinSpec()}}
}

// DevinPaths returns the paths the Devin adapter inspects and touches.
func DevinPaths(env Env) []string {
	return []string{devinSpec().pathsFor(env).Settings}
}

var _ Adapter = DevinAdapter{}

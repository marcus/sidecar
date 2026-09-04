package agentintegration

// The Qwen Code adapter.
//
// Qwen keeps hooks in ~/.qwen/settings.json ($QWEN_HOME/settings.json when that
// variable is set) in Claude Code's nested group shape, and Sidecar adds exactly
// one session-identity entry under SessionStart. That is upstream's whole table:
// Herdr's qwen integration is at HERDR_INTEGRATION_VERSION=1, the first release,
// with QWEN_HOOK_EVENTS = [("SessionStart", "session")] and no lifecycle rows
// ever added and none removed.
//
// THE TIMEOUT IS IN MILLISECONDS, and it is the one number in this whole group
// that is not what it looks like. Herdr writes 10_000 for Qwen and 10 for every
// other provider it installs a hook for, and Qwen's own hooks reference is why:
// it documents `timeout` as milliseconds for a command hook (and seconds for an
// HTTP hook, in the same table). Ten seconds is the same slack every other
// Sidecar entry gets; writing 10 here would be a ten-millisecond budget, which
// on a loaded machine would kill the report process before it had opened the
// store, silently, because a hook surface fails open. That is why this spec
// spells the value out rather than reusing hookTimeoutSec.
//
// The upstream asset is named herdr-agent-session.sh rather than
// herdr-agent-state.sh, which is upstream saying the same thing this comment
// does: this integration is identity and never state.

// Qwen Code integration identity.
const (
	QwenProvider = "qwen"
	QwenSource   = "sidecar.qwen.hooks"

	// QwenAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the spec's
	// canonical history so installed copies read as outdated rather than
	// damaged.
	QwenAssetVersion = "1"

	// QwenHookTimeoutMillis is the entry's timeout, in the unit Qwen counts in.
	//
	// It is ten seconds, the same budget every other Sidecar hook entry gets,
	// and it is a separate constant from hookTimeoutSec precisely so the unit
	// change is visible at the point of use rather than hidden in a number.
	QwenHookTimeoutMillis = hookTimeoutSec * 1000
)

// qwenMatcher is the canonical group matcher: every session source reports
// identity. install_qwen passes Some("*"), as install_qodercli does.
var qwenMatcher = "*"

func qwenSpec() sessionEntrySpec {
	return sessionEntrySpec{
		provider: QwenProvider,
		source:   QwenSource,
		// The catalog records `qwen` as both the id and the command; see
		// internal/agentcatalog/families/qwen.toml.
		command:     QwenProvider,
		version:     QwenAssetVersion,
		dirSegments: []string{".qwen"},
		dirOverride: func(env Env) string { return env.QwenHome },
		file:        "settings.json",
		matcher:     &qwenMatcher,
		events:      []string{"SessionStart"},
		timeout:     QwenHookTimeoutMillis,
		setupHint: "qwen's configuration directory does not exist, so Qwen Code has not been set up on this " +
			"machine; run qwen once (or set QWEN_HOME) and try again",
	}
}

// QwenAdapter installs Sidecar's Qwen Code session-identity hook entry.
type QwenAdapter struct{ sessionEntryAdapter }

// NewQwenAdapter returns the Qwen Code adapter.
func NewQwenAdapter() QwenAdapter {
	return QwenAdapter{sessionEntryAdapter{spec: qwenSpec()}}
}

// QwenPaths returns the paths the Qwen adapter inspects and touches.
func QwenPaths(env Env) []string {
	return []string{qwenSpec().pathsFor(env).Settings}
}

var _ Adapter = QwenAdapter{}

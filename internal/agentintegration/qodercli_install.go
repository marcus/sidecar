package agentintegration

// The Qoder CLI adapter.
//
// Qoder keeps hooks in ~/.qoder/settings.json in the schema Claude Code uses --
// a top-level `hooks` object keyed by event name, each value a list of
// {matcher, hooks: [{type, command, timeout}]} groups -- and Sidecar adds
// exactly one session-identity entry under SessionStart. That is upstream's
// whole table: Herdr's qodercli integration at version 3 has one row,
// QODERCLI_HOOK_EVENTS = [("SessionStart", "session")], and its twelve
// lifecycle rows were removed at that version rather than kept.
//
// Two details differ from the Droid entry beside it, and both come from Qoder's
// own published hooks reference rather than from Herdr.
//
// The group carries a matcher of "*", because install_qodercli calls
// ensure_command_hook with Some("*") where install_droid and install_devin pass
// None. Qoder's schema documents `matcher` as a match condition on the group,
// so "*" is the every-source spelling, exactly as it is for Claude.
//
// The timeout is in seconds, which Qoder documents with a default of 600.
// Sidecar's ten is the same ten Herdr writes and the same unit Claude, Codex
// and Droid use. Qwen is the one provider in this group that counts in
// milliseconds; see qwen_install.go.
//
// The id and the command differ on purpose. `qodercli` is Herdr's label and
// therefore Sidecar's id everywhere, and `qoder` is what the npm package
// installs and what every official example types. See
// internal/agentcatalog/families/qodercli.toml, which records the same split
// and why.

// Qoder CLI integration identity.
const (
	QoderCLIProvider = "qodercli"
	QoderCLICommand  = "qoder"
	QoderCLISource   = "sidecar.qodercli.hooks"

	// QoderCLIAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the spec's
	// canonical history so installed copies read as outdated rather than
	// damaged.
	QoderCLIAssetVersion = "1"
)

// qoderCLIMatcher is the canonical group matcher: every session source reports
// identity, which is what "*" means in Qoder's schema.
var qoderCLIMatcher = "*"

func qoderCLISpec() sessionEntrySpec {
	return sessionEntrySpec{
		provider:    QoderCLIProvider,
		source:      QoderCLISource,
		command:     QoderCLICommand,
		version:     QoderCLIAssetVersion,
		dirSegments: []string{".qoder"},
		dirOverride: func(env Env) string { return env.QoderConfigDir },
		file:        "settings.json",
		matcher:     &qoderCLIMatcher,
		events:      []string{"SessionStart"},
		timeout:     hookTimeoutSec,
		setupHint: "qoder's configuration directory does not exist, so the Qoder CLI has not been set up on this " +
			"machine; run qoder once (or set QODER_CONFIG_DIR) and try again",
	}
}

// QoderCLIAdapter installs Sidecar's Qoder CLI session-identity hook entry.
type QoderCLIAdapter struct{ sessionEntryAdapter }

// NewQoderCLIAdapter returns the Qoder CLI adapter.
func NewQoderCLIAdapter() QoderCLIAdapter {
	return QoderCLIAdapter{sessionEntryAdapter{spec: qoderCLISpec()}}
}

// QoderCLIPaths returns the paths the Qoder CLI adapter inspects and touches.
func QoderCLIPaths(env Env) []string {
	return []string{qoderCLISpec().pathsFor(env).Settings}
}

var _ Adapter = QoderCLIAdapter{}

package agentintegration

import (
	_ "embed"
)

// The Hermes Agent integration.
//
// Same split as Kilo, Pi and OpenCode, for the same reason: the gate below is a
// Go mirror of the one inside assets/hermes/__init__.py, kept deliberately
// separate from the shipped Python so the mapping can be replayed in an ordinary
// test. A bug in "which hook binds which session" is then a failing assertion
// rather than something discovered when a pane offers to resume the wrong
// conversation. The two are held together by
// TestBundledHermesAssetBehavesLikeTheGate, which drives both over the same
// fixtures and requires identical ordered argv.
//
// The provider half is ported from Herdr's hermes integration at version 5 and
// is kept verbatim in behaviour apart from the three differences recorded in
// portedfrom.go and named in the asset.

// Hermes integration identity.
const (
	HermesProvider = "hermes"
	// HermesCommand is the executable. Hermes installs a shim on PATH that
	// execs the interpreter inside its own virtualenv, so this is the name to
	// look up rather than a path into the home directory.
	HermesCommand = "hermes"
	HermesSource  = "sidecar.hermes.plugin"

	// HermesAssetVersion is the bundled asset's version. Authority is granted
	// to a source at a version, so this changing is what makes an installed
	// copy "outdated" rather than merely different.
	//
	// Bump it whenever anything under assets/hermes/ changes, once a release
	// has shipped `agent integration install hermes`. Until then the bytes may
	// be revised in place, because there is no earlier copy of version 1
	// anywhere to be misread. See asset_golden_test.go, which states the whole
	// bump order.
	HermesAssetVersion = "1"

	// HermesAssetSchema is the marker schema the assets declare. It changes
	// only when the marker line's own format changes, which is why it is
	// separate from the asset version.
	HermesAssetSchema = 1

	// HermesPluginName is the directory Sidecar owns under <hermes home>/plugins,
	// and also the name that goes in `plugins.enabled`.
	//
	// The two are the same string because Hermes accepts either the
	// path-derived key or the manifest's own name in that list, and a plugin
	// whose directory and manifest disagree is one whose enable line matches
	// for a reason its reader cannot see. Herdr's directory is
	// `herdr-agent-state`; a machine with both installed has two directories
	// and two enable lines, and neither tool touches the other's.
	HermesPluginName = "sidecar-agent-state"

	// HermesInitName and HermesManifestName are the two files Hermes requires
	// of a directory plugin. A directory holding one without the other is
	// skipped by the loader with no error at all, which is why they are
	// installed and removed together.
	HermesInitName     = "__init__.py"
	HermesManifestName = "plugin.yaml"

	// HermesConfigName is the configuration file the enable line lives in. It
	// belongs to the user and Sidecar owns one line inside it.
	HermesConfigName = "config.yaml"
)

//go:embed assets/hermes/__init__.py
var hermesInitAsset string

//go:embed assets/hermes/plugin.yaml
var hermesManifestAsset string

// HermesInitAsset returns the bundled plugin source.
func HermesInitAsset() string { return hermesInitAsset }

// HermesManifestAsset returns the bundled plugin manifest.
func HermesManifestAsset() string { return hermesManifestAsset }

// HermesHook is one flattened Hermes plugin hook invocation.
//
// It carries only what the gate reads: which hook fired, the platform Hermes is
// driving, and the session id. No message content ever reaches this type, so no
// gate bug can leak any -- `user_message` and `conversation_history` arrive in
// pre_llm_call's kwargs on the Python side and are never read there either.
type HermesHook struct {
	// Name is the Hermes hook name, e.g. "on_session_start".
	Name string
	// Platform is the `platform` kwarg. Absent is the empty string, which is
	// what the tool hooks pass and is why they bind nothing.
	Platform string
	// SessionID is the `session_id` kwarg.
	SessionID string
}

// hermesInteractivePlatforms is upstream's platform gate, kept verbatim.
//
// Hermes runs the same agent behind a Telegram, Slack, Discord or WhatsApp
// gateway as it does behind a terminal, and a gateway session is not the pane's
// conversation. Binding one would offer to resume somebody's chat message as the
// pane's own work.
var hermesInteractivePlatforms = map[string]bool{
	"cli":     true,
	"tui":     true,
	"desktop": true,
	"acp":     true,
}

// HermesGate decides which hook invocations bind a session.
//
// The zero value is ready. `session` is the last id bound and exists only to
// suppress an exact repeat; see the note on _Reporter in the asset for why
// suppression belongs to the transport half.
type HermesGate struct {
	session string
}

// Bind returns the session id this hook should bind, or "" for none.
func (g *HermesGate) Bind(hook HermesHook) string {
	switch hook.Name {
	case "on_session_start", "on_session_reset":
		if !hermesInteractivePlatforms[hook.Platform] {
			return ""
		}
	case "pre_llm_call":
		// Upstream narrows this one to the terminal alone rather than to the
		// interactive set, and the narrowing is kept verbatim. It is the
		// recovery path for a session whose start this plugin missed, and
		// upstream evidently did not want a second chance at binding on the
		// surfaces that already have one.
		if hook.Platform != "cli" {
			return ""
		}
	default:
		// Every other hook Hermes has, including the ones that carry a session
		// id and no platform at all. Upstream subscribes to none of them and
		// neither does this.
		return ""
	}
	if hook.SessionID == "" || hook.SessionID == g.session {
		return ""
	}
	g.session = hook.SessionID
	return hook.SessionID
}

// Session returns the session id the gate has bound.
func (g *HermesGate) Session() string { return g.session }

// HermesReportArgs builds the exact CLI argv one binding becomes.
//
// It mirrors build_args in the bundled asset. Both exist because the asset must
// construct argv in Python at runtime, and the equivalence test compares the two
// lists element for element -- so this is the Go statement of the same contract,
// not a convenience wrapper.
//
// There is no --seq and no --source-version, and neither is an omission: a
// session binding is not a lifecycle report, so it never reaches the sequenced
// store and the verb accepts neither flag.
func HermesReportArgs(sessionID string) []string {
	return []string{
		"agent", "report-session",
		"--kind", HermesProvider,
		"--source", HermesSource,
		"--id", sessionID,
	}
}

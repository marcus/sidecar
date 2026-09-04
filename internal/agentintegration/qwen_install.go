package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
)

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
// store, silently, because a hook surface fails open. That is why this entry
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
	// canonical entry changes, and append the superseded entry to the canonical
	// history so installed copies read as outdated rather than damaged.
	QwenAssetVersion = "1"

	// QwenAssetSchema is the plan/marker schema the asset declares.
	QwenAssetSchema = 1

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

// qwenCanonicalEntry is the exact handler version 1 ships.
func qwenCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(reportSessionCommand(QwenProvider))},
		{key: "timeout", val: json.RawMessage(strconv.Itoa(QwenHookTimeoutMillis))},
	})
}

// qwenCanonicalGroup is the matcher group the entry is installed in.
func qwenCanonicalGroup() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "matcher", val: mustJSONString(qwenMatcher)},
		{key: "hooks", val: marshalJSONArray([]json.RawMessage{qwenCanonicalEntry()})},
	})
}

func qwenEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		matcher: &qwenMatcher,
		canonical: []versionedEntry{
			{version: QwenAssetVersion, entry: qwenCanonicalEntry()},
		},
	}
}

// QwenHome resolves Qwen Code's global directory: $QWEN_HOME when that names
// one, ~/.qwen otherwise. It is Qwen's own documented override -- Storage's
// getGlobalQwenDir resolves it in place of ~/.qwen -- so honouring it is what
// finds a relocated Qwen and what lets a proof run redirect the provider.
func QwenHomeDir(home, qwenHome string) string {
	if dir := overrideDir(home, qwenHome); dir != "" {
		return dir
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".qwen")
}

// qwenIntegration is the Qwen Code integration as data.
func qwenIntegration() sessionHookIntegration {
	return sessionHookIntegration{
		provider: QwenProvider,
		// The catalog records `qwen` as both the id and the command; see
		// internal/agentcatalog/families/qwen.toml.
		command:      QwenProvider,
		source:       QwenSource,
		assetVersion: QwenAssetVersion,
		assetSchema:  QwenAssetSchema,
		fileName:     "settings.json",
		dir:          func(env Env) string { return QwenHomeDir(env.Home, env.QwenHome) },
		spec:         qwenEntrySpec(),
		item:         qwenCanonicalGroup,
		setupHint: "qwen's configuration directory does not exist, so Qwen Code has not been set up on this " +
			"machine; run qwen once (or set QWEN_HOME) and try again",
	}
}

// QwenAdapter installs Sidecar's Qwen Code session-identity hook entry.
type QwenAdapter struct{ sessionHookAdapter }

// NewQwenAdapter returns the adapter this build ships.
func NewQwenAdapter() QwenAdapter {
	return QwenAdapter{sessionHookAdapter{integration: qwenIntegration()}}
}

// QwenPaths returns the paths the Qwen adapter inspects and touches.
func QwenPaths(env Env) []string {
	return []string{qwenIntegration().pathsFor(env).File}
}

var _ Adapter = QwenAdapter{}

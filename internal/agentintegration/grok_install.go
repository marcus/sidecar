package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// The grok adapter.
//
// grok merges hooks from every `<grok home>/hooks/*.json`, so Sidecar owns a
// file of its own there rather than editing one of the user's. The file holds
// grok's ordinary hook object: a `hooks` member, a `SessionStart` array, a
// matcher group, and one handler inside it. That is Claude's shape exactly,
// which is not a coincidence -- grok reads Claude's settings file too, and
// accepts Claude's spelling of everything.
//
// # Why the file is a dedicated one and the ownership rule is still by entry
//
// Herdr writes `~/.grok/hooks/herdr.json` and deletes it outright at uninstall,
// because every byte in it is Herdr's. Sidecar writes `sidecar.json` in the
// same directory and does NOT claim the file: ownership stays the entry rule
// from hookconfig.go. The difference costs nothing and buys the case where a
// user adds a hook of their own to a file named after Sidecar, which a
// file-owning uninstall would delete. When removing Sidecar's entry leaves the
// file empty, the file goes -- a file that held nothing but Sidecar's entry is
// one Sidecar created.
//
// # grok and Claude fire inside each other, and only one of them may bind
//
// grok's documented hook sources include `~/.claude/settings.json` and
// `~/.cursor/hooks.json`, for Claude Code and Cursor compatibility. So in a
// grok session all three of Sidecar's entries can fire: this one claiming
// `--kind grok`, and the Claude and Cursor entries claiming their own kinds.
// Exactly one of them binds. `report-session` canonicalises the claimed kind
// through the catalog and then verifies it against the pane's own occupant and
// the shell's recorded type, so the foreign claims are refused with
// `kind_mismatch` rather than binding a grok session id to a Claude resume.
// That gate is td-11040b, and
// TestBothEntriesFireInAGrokSessionAndOnlyGroksBinds in internal/cli drives
// both installed entries against a grok-typed shell to prove it.
//
// The reverse direction is safe by construction: Claude does not read
// `~/.grok/hooks/`, so this entry never fires anywhere but grok.
//
// # Where the session id comes from
//
// grok's payload is camelCase -- `hookEventName`, `sessionId`, `cwd`,
// `workspaceRoot`, `promptId` -- and it also exports `GROK_SESSION_ID` into
// every hook process. Herdr's asset prefers the environment variable and falls
// back to the payload. Sidecar reads the payload, because `--hook-stdin` is one
// bounded reader serving six providers and adding a per-provider environment
// read would be a second way for one provider to name a session. The two agree
// on every release traced so far; if they ever disagree, the payload is the one
// grok itself puts in front of the hook.

// grok integration identity.
const (
	GrokProvider = "grok"
	GrokCommand  = "grok"
	GrokSource   = "sidecar.grok.hooks"

	// GrokAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the canonical
	// history so installed copies read as outdated rather than damaged.
	GrokAssetVersion = "1"

	// GrokAssetSchema is the plan/marker schema the asset declares.
	GrokAssetSchema = 1

	// GrokHookFileName is the file Sidecar owns inside grok's hooks directory.
	// grok globs every *.json there and merges what it finds.
	GrokHookFileName = "sidecar.json"
)

// grokCanonicalEntry is the exact handler version 1 ships.
func grokCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(reportSessionCommand(GrokProvider))},
		{key: "timeout", val: json.RawMessage("10")},
	})
}

// grokCanonicalGroup is the matcher group the entry is installed in.
//
// It carries no `matcher` key, which is Herdr's shape and grok's own
// documented default: an omitted matcher matches everything. A matcher on
// SessionStart tests the start source (`startup`, `resume`, and so on), and
// binding the session is right for every one of them.
func grokCanonicalGroup() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "hooks", val: marshalJSONArray([]json.RawMessage{grokCanonicalEntry()})},
	})
}

func grokEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		canonical: []versionedEntry{
			{version: GrokAssetVersion, entry: grokCanonicalEntry()},
		},
	}
}

// GrokHooksDir resolves the directory grok reads its personal hook files from:
// `$GROK_HOME/hooks` when that is set, `~/.grok/hooks` otherwise.
//
// GROK_HOME is grok's own variable rather than Herdr's seam -- the shipped
// binary carries the string and the error "no user grok home (set $GROK_HOME or
// $HOME)" -- so honouring it puts the file where a relocated grok reads. Herdr
// also honours a `GROK_CONFIG_DIR`, and its own comment says the grok CLI does
// not read that one; Sidecar does not follow it, for the same reason it does
// not follow Antigravity's or Cursor's.
func GrokHooksDir(home, grokHome string) string {
	if dir := strings.TrimSpace(grokHome); dir != "" {
		return filepath.Join(expandTildePath(home, dir), "hooks")
	}
	return filepath.Join(home, ".grok", "hooks")
}

// GrokAdapter installs Sidecar's grok session-identity hook.
type GrokAdapter struct{ sessionHookAdapter }

// NewGrokAdapter returns the adapter this build ships.
func NewGrokAdapter() GrokAdapter {
	return GrokAdapter{sessionHookAdapter{integration: sessionHookIntegration{
		provider:     GrokProvider,
		command:      GrokCommand,
		source:       GrokSource,
		assetVersion: GrokAssetVersion,
		assetSchema:  GrokAssetSchema,
		fileName:     GrokHookFileName,
		dir:          func(env Env) string { return GrokHooksDir(env.Home, env.GrokHome) },
		spec:         grokEntrySpec(),
		item:         grokCanonicalGroup,
	}}}
}

// GrokPaths returns the paths the grok adapter inspects and touches.
func GrokPaths(env Env) []string {
	return []string{NewGrokAdapter().integration.pathsFor(env).File}
}

var _ Adapter = GrokAdapter{}

package agentintegration

import (
	"encoding/json"
	"path/filepath"
)

// The Antigravity CLI adapter.
//
// Antigravity has no session event at all. Its whole hook surface is five
// events -- PreToolUse, PostToolUse, PreInvocation, PostInvocation and Stop --
// and every one of them carries the same common fields, `conversationId` among
// them. So the earliest moment a pane's conversation can be named is the first
// PreInvocation of the run, which is what Herdr subscribes to and what this
// port keeps. PostInvocation and Stop carry the same id and would name it
// later; the tool events carry it too but only for a turn that calls a tool.
// PreInvocation fires before the model is called, on every turn, so it is both
// the earliest and the only one that fires unconditionally.
//
// Read from agy 1.1.22's own embedded hooks documentation rather than from
// Herdr, and the two agree on every point that matters here.
//
// # Three quirks the provider half keeps
//
// 1. The payload is camelCase, because Antigravity encodes it with protojson:
// `conversationId`, not `session_id`. `agent report-session --hook-stdin` reads
// both spellings for exactly this reason.
//
// 2. A hook must write a JSON object to stdout. Every documented contract says
// so and Herdr's asset emits `{}` on every exit path, including the ones that
// do nothing. `sidecar agent report-session` is silent on success, so the
// installed command appends `printf '{}\n'` after it. The suffix is a fixed
// literal with nothing interpolated, it runs under the `sh -c` Antigravity
// already uses, and it also makes the hook fail open: the exit status the
// provider sees is printf's, so a Sidecar that is missing, refusing or slow
// cannot fail an Antigravity turn.
//
// 3. hooks.json is keyed by hook NAME at the top level, not by event. Each
// named block holds its own events object, and blocks from different sources
// merge. Sidecar owns one block, `sidecar`, and reads every block, because an
// entry a user moved into a block of their own would otherwise keep reporting
// while uninstall could not see it.

// Antigravity integration identity.
const (
	AntigravityProvider = "antigravity"
	// AntigravityCommand is the executable. The family id is `antigravity` and
	// the binary is `agy`, which is also Herdr's label for this agent.
	AntigravityCommand = "agy"
	AntigravitySource  = "sidecar.antigravity.hooks"

	// AntigravityAssetVersion is the bundled entry's version. Bump it whenever
	// the canonical entry changes, and append the superseded entry to the
	// canonical history so installed copies read as outdated rather than
	// damaged.
	AntigravityAssetVersion = "1"

	// AntigravityAssetSchema is the plan/marker schema the asset declares.
	AntigravityAssetSchema = 1

	// AntigravityBlockName is the named hook block Sidecar owns in hooks.json.
	AntigravityBlockName = "sidecar"

	// AntigravityEvent is the event the entry is registered on. Antigravity has
	// no session event; PreInvocation is the earliest one that fires on every
	// turn and carries the conversation id.
	AntigravityEvent = "PreInvocation"
)

// antigravityHookCommand is the exact command the hook runs.
//
// The `printf` suffix is the provider half, not decoration: Antigravity reads a
// hook's stdout as JSON and Sidecar's report command prints nothing at all on
// success. See quirk 2 above.
func antigravityHookCommand() string {
	return reportSessionCommand(AntigravityProvider) + `; printf '{}\n'`
}

// antigravityCanonicalEntry is the exact handler version 1 ships.
//
// PreInvocation takes a flat handler list, so this is what goes into the event
// array directly. The matcher/hooks group wrapper is valid only for the two
// tool events, which this integration does not use.
func antigravityCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(antigravityHookCommand())},
		{key: "timeout", val: json.RawMessage("10")},
	})
}

func antigravityEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		namedBlocks: true,
		block:       AntigravityBlockName,
		events:      []string{AntigravityEvent},
		flat:        true,
		canonical: []versionedEntry{
			{version: AntigravityAssetVersion, entry: antigravityCanonicalEntry()},
		},
	}
}

// AntigravityConfigHome resolves the directory Antigravity CLI reads its global
// customizations from.
//
// It is `~/.gemini/config` and nothing else, deliberately. Herdr honours an
// `ANTIGRAVITY_CLI_CONFIG_DIR` override here; agy 1.1.22 does not read that
// variable -- it appears nowhere in the shipped binary -- so honouring it would
// put Sidecar's entry in a directory the provider never opens, which is worse
// than having no override at all. A proof run redirects Antigravity by moving
// HOME, which moves the provider and the installer together.
//
// `~/.gemini/antigravity-cli` is runtime data and is never read for hooks. The
// distinction is not academic: agy's own changelog records fixing a bug where
// its `/hooks` command wrote to that directory instead of the shared config
// one, which is the release the capability entry means when it says the path
// has already moved.
func AntigravityConfigHome(home string) string {
	return filepath.Join(home, ".gemini", "config")
}

// AntigravityAdapter installs Sidecar's Antigravity session-identity hook.
type AntigravityAdapter struct{ sessionHookAdapter }

// NewAntigravityAdapter returns the adapter this build ships.
func NewAntigravityAdapter() AntigravityAdapter {
	return AntigravityAdapter{sessionHookAdapter{integration: sessionHookIntegration{
		provider:     AntigravityProvider,
		command:      AntigravityCommand,
		source:       AntigravitySource,
		assetVersion: AntigravityAssetVersion,
		assetSchema:  AntigravityAssetSchema,
		fileName:     "hooks.json",
		dir:          func(env Env) string { return AntigravityConfigHome(env.Home) },
		spec:         antigravityEntrySpec(),
		item:         antigravityCanonicalEntry,
	}}}
}

// AntigravityPaths returns the paths the Antigravity adapter inspects and
// touches.
func AntigravityPaths(env Env) []string {
	return []string{NewAntigravityAdapter().integration.pathsFor(env).File}
}

var _ Adapter = AntigravityAdapter{}

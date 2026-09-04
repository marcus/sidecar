package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// The GitHub Copilot CLI adapter.
//
// Copilot keeps hooks inside ~/.copilot/settings.json, under `hooks`, as a flat
// array of handlers per event with no matcher group around them. Sidecar
// registers one, on SessionStart, which is the single event Herdr's copilot
// integration subscribes to at version 3.
//
// # This is the one port in the lane that could not be checked against a
// # released binary
//
// GitHub Copilot CLI is not installed on any machine Sidecar has surveyed, so
// every fact here comes from Herdr's installer and its vendored asset rather
// than from the provider. That is the weakest evidence in the registry and the
// capability entry says so: `docs-only`, and no trace. Two consequences follow
// and both are recorded rather than smoothed over.
//
// First, the entry's shape is Herdr's, faithfully: `type` is `command`, the
// command lives under `bash`, and the timeout is `timeoutSec` rather than
// `timeout`. Those are three departures from every other provider in this tree
// and none of them was invented here.
//
// Second, `bash` is written unconditionally rather than switched to
// `powershell` on Windows the way Herdr's `direct_command_field` does. Sidecar
// does not run on Windows -- it has no Windows process-identity adapter, and
// Slice 3 of the parity plan deliberately left upstream's Windows argument
// walkers unported for the same reason -- so the Windows spelling would be
// unreachable and untestable. The scan reads both spellings anyway, so a
// settings.json synced from a machine where something else wrote the
// `powershell` form is still recognised as Sidecar's and can still be removed.
//
// # Which event carries the session id
//
// Upstream's asset reads `session_id`, falling back to `sessionId`, from the
// payload, and refuses anything whose `hook_event_name` normalises to something
// other than `sessionstart`. `agent report-session --hook-stdin` reads both
// spellings, and Sidecar's entry is registered on SessionStart alone, so the
// event guard the shell asset needed is structural here rather than a check.
//
// Herdr's installer also removes its own registrations from nine other events
// -- UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure, Stop,
// agentStop, SessionEnd, notification and sessionStart -- which is the rollback
// of a full-lifecycle hook set it used to ship. Sidecar never shipped those, so
// there is nothing to roll back; the ownership rule finds and removes a Sidecar
// entry on any event regardless, which covers the same ground without a list
// that would go stale.

// Copilot integration identity.
const (
	CopilotProvider = "copilot"
	CopilotCommand  = "copilot"
	CopilotSource   = "sidecar.copilot.hooks"

	// CopilotAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to the canonical
	// history so installed copies read as outdated rather than damaged.
	CopilotAssetVersion = "1"

	// CopilotAssetSchema is the plan/marker schema the asset declares.
	CopilotAssetSchema = 1

	// CopilotCommandKey is the entry member Copilot reads the command from on
	// Unix. See the note above on why the Windows spelling is not written.
	CopilotCommandKey = "bash"
)

// copilotCanonicalEntry is the exact handler version 1 ships.
func copilotCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: CopilotCommandKey, val: mustJSONString(reportSessionCommand(CopilotProvider))},
		{key: "timeoutSec", val: json.RawMessage("10")},
	})
}

func copilotEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		event:      "SessionStart",
		flat:       true,
		commandKey: CopilotCommandKey,
		// The two spellings Sidecar does not write but must still recognise:
		// `command`, in case a future Copilot accepts the ordinary key, and
		// `powershell`, which is what a Windows Herdr wrote into a settings.json
		// that later reached this machine.
		altCommandKeys: []string{"command", "powershell"},
		canonical: []versionedEntry{
			{version: CopilotAssetVersion, entry: copilotCanonicalEntry()},
		},
	}
}

// CopilotConfigHome resolves the directory GitHub Copilot CLI keeps its
// settings in: $COPILOT_HOME when that names one, ~/.copilot otherwise.
//
// The variable is Herdr's, and unlike Antigravity's seam it is honoured here
// rather than ignored, because there is no shipped binary to check it against
// and a plausible provider override is the better guess than none: a user who
// has set it has a Copilot that reads there, and an installer that ignored it
// would write where nothing reads. If a released Copilot turns out not to read
// it, the fix is to drop this the way Antigravity's was dropped, and the
// capability entry records that this is unverified.
func CopilotConfigHome(home, copilotHome string) string {
	if dir := strings.TrimSpace(copilotHome); dir != "" {
		return expandTildePath(home, dir)
	}
	return filepath.Join(home, ".copilot")
}

// expandTildePath resolves a leading ~ against the given home, which is what
// Herdr's own directory resolution does for every provider override it reads.
// A path that does not start with a tilde is returned unchanged, including a
// relative one: guessing at what a relative override means is worse than
// passing it through to fail visibly.
func expandTildePath(home, path string) string {
	switch {
	case path == "~":
		return home
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(home, path[2:])
	}
	return path
}

// CopilotAdapter installs Sidecar's Copilot session-identity hook.
type CopilotAdapter struct{ sessionHookAdapter }

// NewCopilotAdapter returns the adapter this build ships.
func NewCopilotAdapter() CopilotAdapter {
	return CopilotAdapter{sessionHookAdapter{integration: sessionHookIntegration{
		provider:     CopilotProvider,
		command:      CopilotCommand,
		source:       CopilotSource,
		assetVersion: CopilotAssetVersion,
		assetSchema:  CopilotAssetSchema,
		fileName:     "settings.json",
		dir:          func(env Env) string { return CopilotConfigHome(env.Home, env.CopilotHome) },
		spec:         copilotEntrySpec(),
		item:         copilotCanonicalEntry,
	}}}
}

// CopilotPaths returns the paths the Copilot adapter inspects and touches.
func CopilotPaths(env Env) []string {
	return []string{NewCopilotAdapter().integration.pathsFor(env).File}
}

var _ Adapter = CopilotAdapter{}

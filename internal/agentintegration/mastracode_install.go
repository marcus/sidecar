package agentintegration

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Mastra Code adapter.
//
// Mastra Code reads its global hooks from one JSON file the user owns,
// ~/.mastracode/hooks.json, whose top level IS the event map: each key is a hook
// event name and each value an array of `{type, command, timeout, description,
// matcher}` entries. So Sidecar owns entries in a file rather than a file
// (Ownership OwnsEntry), and the whole installer is about touching exactly its
// own entries and nothing else.
//
// # Why ownership is by command rather than by marker
//
// Kimi's config.toml is fenced between two marker comments, because TOML has
// comments and an array of tables can be appended to at end of file. JSON has
// neither, so the shape that works there does not exist here. This adapter uses
// the rule hookconfig.go already established for Claude and Codex and that the
// mastracode table extends by one step: an entry is Sidecar's when its `command`
// invokes Sidecar's own mastracode source, and it is nobody else's business what
// event it sits under.
//
// That is a strictly better rule for this provider than a positional one would
// be, and the reason is upstream's own MASTRACODE_REMOVED_HOOK_EVENTS. Herdr
// keeps a hand-written list of two rows an earlier integration version shipped,
// and removes those exact command strings on every install and uninstall so a
// superseded row does not survive an upgrade. Sidecar needs no such list: a stale
// entry from any earlier asset version still names this source, so the same pass
// that installs the current eleven strips it. mastracode.go states the same fact
// from the other side.
//
// # What the editor may and may not do
//
// The file is parsed with the order- and token-preserving reader in
// hookconfig.go, so every value the user wrote keeps its own bytes and its place.
// Sidecar rewrites only:
//
//   - the arrays under the eleven event keys it installs into, and only by
//     removing its own entries and appending one of its own; and
//   - nothing else, ever.
//
// A file that does not parse is refused outright rather than rewritten, and an
// event key whose value is not an array is a refusal too: a rewrite of a file the
// scan could not read is a clobber by construction.

// MastracodeAdapter installs Sidecar's Mastra Code lifecycle hooks.
type MastracodeAdapter struct{}

func (MastracodeAdapter) Provider() string { return MastracodeProvider }
func (MastracodeAdapter) Source() string   { return MastracodeSource }

// Assets returns the single entry asset this integration installs.
//
// Content is the canonical hooks.json Sidecar would write into an empty tree,
// which for an OwnsEntry asset is a description of the entries rather than
// something ever written verbatim over a user's file.
func (MastracodeAdapter) Assets() []Asset {
	return []Asset{{
		Name:          MastracodeConfigName,
		Source:        MastracodeSource,
		SchemaVersion: MastracodeAssetSchema,
		Version:       MastracodeAssetVersion,
		Ownership:     OwnsEntry,
		Content:       string(renderJSONFile(mastracodeCanonicalTop())),
	}}
}

func (a MastracodeAdapter) asset() Asset { return a.Assets()[0] }

// mastracodeCanonicalEntry is the exact entry one row installs.
//
// The member order is the one Mastra Code's own documentation example writes and
// its `/hooks` command prints back: type, command, timeout, description. It is
// fixed rather than sorted because this value is compared byte for byte against
// what is on disk to decide current-versus-outdated.
//
// `timeout` is MILLISECONDS here. See mastracodeHookTimeoutMS.
func mastracodeCanonicalEntry(h MastracodeHook) json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(MastracodeHookCommand(h))},
		{key: "timeout", val: json.RawMessage(strconv.Itoa(mastracodeHookTimeoutMS))},
		{key: "description", val: mustJSONString(mastracodeDescription(h))},
	})
}

// mastracodeDescription is what Mastra Code's `/hooks` command prints beside the
// entry. A user reading their own hooks.json is entitled to know what the eleven
// entries a tool added to it mean without going and reading the tool's source.
func mastracodeDescription(h MastracodeHook) string {
	return "Sidecar: " + h.Why
}

// mastracodeCanonicalTop is the whole file Sidecar would create in an empty tree,
// as ordered members: one event key per row, in the table's order.
func mastracodeCanonicalTop() []jsonMember {
	top := make([]jsonMember, 0, len(mastracodeHooks))
	for _, h := range mastracodeHooks {
		top = append(top, jsonMember{
			key: h.Event,
			val: marshalJSONArray([]json.RawMessage{mastracodeCanonicalEntry(h)}),
		})
	}
	return top
}

// mastracodePaths are the exact user-level paths this adapter inspects.
type mastracodePaths struct {
	Dir    string
	Config string
	Backup string
}

// MastracodePaths returns the paths the Mastra Code adapter would inspect and
// touch.
func MastracodePaths(env Env) []string { return []string{mastracodePathsFor(env).Config} }

func mastracodePathsFor(env Env) mastracodePaths {
	dir := mastracodeHomeDir(env.Home)
	return mastracodePaths{
		Dir:    dir,
		Config: filepath.Join(dir, MastracodeConfigName),
		Backup: filepath.Join(dir, MastracodeConfigName+MastracodeBackupSuffix),
	}
}

// mastracodeHomeDir resolves Mastra Code's global configuration directory.
//
// There is no environment override to honour, and that is a measured absence
// rather than one this port declined to look for. Mastra Code 0.38.0 resolves the
// global hooks file as path.join(homeDir ?? os.homedir(), configDirName,
// "hooks.json"); `configDirName` is a constructor argument defaulting to
// ".mastracode", validated by validateConfigDirName to be a single directory name
// with no separators, and the TUI entry point passes neither it nor `homeDir`.
// Nothing in the CLI reads an environment variable for either. Herdr resolves the
// same directory the same way and honours nothing either.
//
// So $HOME is the only lever, which is what a proof run has to move, and what
// the capability entry records.
func mastracodeHomeDir(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, MastracodeDirName)
}

// mastracodeHomeMissing is the one sentence for "there is no home directory to
// resolve the configuration directory against".
//
// It is the only precondition on the path, and that is a correction this port
// made against a measurement rather than a rule it inherited. The Pi and Kimi
// adapters refuse when the provider's directory is absent, because those
// providers create it on startup and its absence means the provider has never
// run. Mastra Code's is not that kind of directory.
//
// What was measured: a real `mastracode -p` run in an empty home created
// ~/Library/Application Support/mastracode and nothing else. ~/.mastracode is
// where a user puts hooks.json, mcp.json and commands/, and the only thing in the
// 0.38.0 package that creates it unasked is the TUI's analytics writer, which
// mkdirs it to drop analytics.json and which the user can turn off. So it exists
// after a TUI run, does not exist after headless use, and does not exist before
// the first launch -- which is exactly when somebody installing an integration is
// likely to be standing. Refusing on its absence would have meant the integration
// could not be installed until the agent had been launched once, for a directory
// that carries no state Sidecar would be inventing.
//
// So Sidecar creates it, exactly as Herdr's install_mastracode does and exactly
// as Sidecar's own Claude adapter does for ~/.claude. The provider gate is the
// mastracode CLI being on PATH, which is the honest test of whether the agent is
// installed here.
func mastracodeHomeMissing() string {
	return "Sidecar could not determine a home directory, so it cannot say where mastra code reads its hooks from"
}

// mastracodeOwnedEntry is one Sidecar-owned entry found in hooks.json.
type mastracodeOwnedEntry struct {
	event string
	index int
	// canonicalFor is the row whose canonical entry this is byte-equivalent to,
	// or -1 when the entry invokes Sidecar's source but is not what this build
	// ships.
	canonicalFor int
}

// mastracodeScan is one reading of hooks.json.
type mastracodeScan struct {
	exists bool
	raw    []byte
	top    []jsonMember
	owned  []mastracodeOwnedEntry

	// parseErr names why the file cannot be safely interpreted or edited.
	// Empty means the scan is trustworthy.
	parseErr string
}

// converged reports whether the file already holds exactly the bundled
// integration: one owned entry per row, each under its own event, each
// byte-equivalent to the canonical entry for that row, and no other owned entry
// anywhere.
func (s mastracodeScan) converged() bool {
	if s.parseErr != "" || len(s.owned) != len(mastracodeHooks) {
		return false
	}
	seen := map[int]bool{}
	for _, o := range s.owned {
		if o.canonicalFor < 0 || seen[o.canonicalFor] {
			return false
		}
		if mastracodeHooks[o.canonicalFor].Event != o.event {
			return false
		}
		seen[o.canonicalFor] = true
	}
	return len(seen) == len(mastracodeHooks)
}

// scanMastracodeConfig reads hooks.json, honouring the file inspection that
// produced file.
func scanMastracodeConfig(file FileState) mastracodeScan {
	s := mastracodeScan{}
	if !file.Exists {
		return s
	}
	s.exists = true
	raw, ok := readEntryFileBytes(file)
	if !ok {
		s.parseErr = firstNonEmpty(file.UnsafeDetail, "the file exists but could not be read")
		return s
	}
	s.raw = raw

	top, err := parseJSONFile(raw)
	if err != nil {
		s.parseErr = "the file is not a JSON object: " + err.Error()
		return s
	}
	s.top = top

	canonical := make([]json.RawMessage, len(mastracodeHooks))
	for i, h := range mastracodeHooks {
		canonical[i] = mastracodeCanonicalEntry(h)
	}

	for _, ev := range top {
		// Only the keys Mastra Code itself recognises as events are read as hook
		// arrays. Its own loader ignores anything else at the top level
		// (validateConfig iterates VALID_EVENTS), so a key of the user's that is
		// not an event is not a malformed hook array, it is data Mastra Code
		// never looks at and Sidecar must not either.
		if !mastracodeEventNames[ev.key] {
			continue
		}
		entries, err := parseJSONArray(ev.val)
		if err != nil {
			s.parseErr = fmt.Sprintf("%s is not an array", ev.key)
			return s
		}
		for i, entryRaw := range entries {
			entry, err := parseJSONObject(entryRaw)
			if err != nil {
				s.parseErr = fmt.Sprintf("%s[%d] is not an object", ev.key, i)
				return s
			}
			typ, _ := memberString(entry, "type")
			command, ok := memberString(entry, "command")
			if typ != "command" || !ok || !invokesMastracodeReport(command) {
				continue
			}
			owned := mastracodeOwnedEntry{event: ev.key, index: i, canonicalFor: -1}
			for row := range canonical {
				if sameJSON(entryRaw, canonical[row]) {
					owned.canonicalFor = row
				}
			}
			s.owned = append(s.owned, owned)
		}
	}
	return s
}

// mastracodeEventNames is Mastra Code's own VALID_EVENTS, from
// @mastra/code-sdk/dist/hooks/config.js at 0.38.0.
//
// It is the full fourteen rather than the eleven this port installs into,
// because the question it answers is "is this top-level key one Mastra Code reads
// as hooks", and a stale Sidecar entry left under an event the port no longer
// uses has to still be found and removed.
var mastracodeEventNames = map[string]bool{
	"PreToolUse":        true,
	"PostToolUse":       true,
	"Stop":              true,
	"UserPromptSubmit":  true,
	"SessionStart":      true,
	"SessionEnd":        true,
	"Notification":      true,
	"AgentStart":        true,
	"AgentEnd":          true,
	"PermissionRequest": true,
	"PermissionResult":  true,
	"Interrupt":         true,
	"SubagentStart":     true,
	"SubagentEnd":       true,
}

// mastracodeState is everything one inspection learned. Both Inspect and Plan
// are built from it, so a plan can never rest on a different reading of the disk
// than the status the user was shown.
type mastracodeState struct {
	env    Env
	paths  mastracodePaths
	asset  Asset
	dir    FileState
	config FileState
	backup FileState
	scan   mastracodeScan

	providerPath string

	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a MastracodeAdapter) inspect(env Env) mastracodeState {
	p := mastracodePathsFor(env)
	s := mastracodeState{
		env:    env,
		paths:  p,
		asset:  a.asset(),
		dir:    inspectDir(env, p.Dir),
		config: inspectFile(env, p.Config, a.asset()),
		backup: FileState{Path: p.Backup, Exists: fileExists(p.Backup)},
	}
	s.scan = scanMastracodeConfig(s.config)
	if len(s.scan.owned) > 0 {
		s.installed = mastracodeInstalledVersion(s.scan)
		ownEntry(&s.config, s.installed)
	}
	if path, ok := env.lookPath(MastracodeProvider); ok {
		s.providerPath = path
	}
	s.assetStatus, s.message = mastracodeAssetStatus(s)

	s.status = s.assetStatus
	if s.providerPath == "" {
		// The provider CLI being absent is the more actionable of the two true
		// statements, and it is the one that decides authority: with no mastracode
		// there is nothing to run the hooks, so TierFor is right to return screen
		// fallback. The entries' own state is still in the message and in Files, so
		// an uninstall after removing the provider stays discoverable.
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the mastracode CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// mastracodeInstalledVersion reports the asset version the entries on disk
// belong to.
//
// There is exactly one version so far, so any entry equal to a canonical one is
// at MastracodeAssetVersion and anything else is unrecognised. When a second
// version ships this becomes a lookup over a canonical history, the way
// hookEntrySpec.canonical already is for Claude and Codex; until then a second
// table would be a table with one row and no way to test the other branch.
func mastracodeInstalledVersion(s mastracodeScan) string {
	for _, o := range s.owned {
		if o.canonicalFor >= 0 {
			return MastracodeAssetVersion
		}
	}
	return ""
}

// mastracodeAssetStatus decides the status from the inspected file alone.
//
// Nothing here trusts a claimed version, because there is nowhere in a JSON entry
// to claim one: the entries' bytes are compared with the entries this build
// renders, so a hand-edited, duplicated or partially applied set reads as
// needs-repair rather than as current.
func mastracodeAssetStatus(s mastracodeState) (agentlifecycle.IntegrationStatus, string) {
	switch {
	case s.dir.Exists && s.dir.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.dir.UnsafeDetail + " (" + s.paths.Dir + ")"
	case s.config.Exists && s.config.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.config.UnsafeDetail + " (" + s.paths.Config + ")"
	case s.scan.parseErr != "":
		return agentlifecycle.StatusNeedsRepair,
			MastracodeConfigName + " could not be interpreted (" + s.scan.parseErr +
				"), so the integration state is unknown; Sidecar will not modify the file"
	case len(s.scan.owned) == 0:
		if s.paths.Dir == "" {
			// The status has to carry this, not only the refusal. Without it a
			// machine with no resolvable home reads as a plain not-installed with an
			// empty message and nothing anywhere saying why the one action that
			// would fix it is not offered.
			return agentlifecycle.StatusNotInstalled, mastracodeHomeMissing()
		}
		return agentlifecycle.StatusNotInstalled, ""
	case s.scan.converged():
		return agentlifecycle.StatusCurrent, ""
	case mastracodeInstalledVersion(s.scan) == "":
		return agentlifecycle.StatusNeedsRepair,
			"entries invoking Sidecar's mastracode integration are installed in " + MastracodeConfigName +
				" but none of them matches what this build ships; repair converges on exactly the eleven it does"
	}
	return agentlifecycle.StatusNeedsRepair,
		"the entries Sidecar owns in " + MastracodeConfigName +
			" are not the eleven this build ships, so some events would be reported twice and others not at all; " +
			"repair converges on exactly the eleven"
}

// Inspect implements [Adapter].
func (a MastracodeAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a MastracodeAdapter) statusOf(s mastracodeState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(MastracodeSource)
	// The provider version is deliberately not probed, and it is left empty
	// rather than filled with what a probe would return. Mastra Code 0.38.0 ships
	// no version flag: `mastracode --version` is not a headless flag, so with no
	// TTY the CLI falls through to headless mode and prints its usage banner,
	// whose first line -- "Usage: mastracode --prompt <text> [options]" -- is what
	// detectProviderVersion would record and every surface would then render as
	// this provider's version. An empty field says "not known", which is true;
	// that banner would say something false. It costs nothing here: TierFor gates
	// on the tested range only at TierFull, and this source's ceiling is advisory.
	tier, reason := capability.TierFor(s.status, false)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              MastracodeProvider,
		Source:                MastracodeSource,
		Status:                s.status,
		BundledVersion:        MastracodeAssetVersion,
		InstalledVersion:      s.installed,
		ProviderInTestedRange: false,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.Config},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.config, s.backup}

	// Offered is computed by asking the planner, not by restating its rules in a
	// second place. A verb a surface offers is a verb that will not refuse when it
	// is pressed.
	for _, act := range Actions() {
		if _, err := a.plan(s, act); err == nil {
			st.Offered = append(st.Offered, act)
		}
	}
	return st
}

// Plan implements [Adapter].
func (a MastracodeAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a MastracodeAdapter) plan(s mastracodeState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      MastracodeProvider,
		Source:        MastracodeSource,
		Action:        act,
		StatusBefore:  s.status,
		StatusAfter:   s.status,
	}
	switch act {
	case ActionUninstall:
		return a.planUninstall(s, p)
	case ActionInstall, ActionUpdate, ActionRepair:
		return a.planConverge(s, p, act)
	}
	return Plan{}, refuse(RefuseUnknownProvider, "", "unknown action %q", act)
}

// planConverge builds the plan that ends with exactly the eleven entries this
// build ships, each under its own event, and every other byte of hooks.json as it
// was found.
func (a MastracodeAdapter) planConverge(s mastracodeState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the mastracode CLI was not found on PATH, so Sidecar will not modify %s for it; install mastra code first", s.paths.Config)
	}
	if s.paths.Dir == "" {
		return Plan{}, refuse(RefuseProviderMissing, "", "%s", mastracodeHomeMissing())
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.Config, MastracodeProvider, s.installed, s.message); err != nil {
		return Plan{}, err
	}
	if err := mastracodeRefuseUnsafe(s); err != nil {
		return Plan{}, err
	}

	if s.scan.converged() {
		p.Unchanged = true
		return p, nil
	}

	top, _, err := stripMastracodeEntries(s.scan)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	top, err = appendMastracodeEntries(top)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	content := renderJSONFile(top)

	p.Ops = entryFileOps(nil, s.env, s.dir, s.config, s.backup, content,
		"write Sidecar's eleven mastra code lifecycle hook entries, preserving every other hook",
		ownedEntry(MastracodeAssetVersion))
	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes exactly Sidecar's entries and the event arrays that held
// nothing else, and never anything more.
func (a MastracodeAdapter) planUninstall(s mastracodeState, p Plan) (Plan, error) {
	if !s.config.Exists || (len(s.scan.owned) == 0 && s.scan.parseErr == "") {
		p.Unchanged = true
		return p, nil
	}
	if err := mastracodeRefuseUnsafe(s); err != nil {
		return Plan{}, err
	}

	top, changed, err := stripMastracodeEntries(s.scan)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	if !changed {
		p.Unchanged = true
		return p, nil
	}
	p.Ops = removalOps(s.config, s.backup, top,
		"remove Sidecar's mastra code lifecycle hook entries, preserving every other hook")
	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

// mastracodeRefuseUnsafe is the safety gate every mutation of hooks.json passes.
func mastracodeRefuseUnsafe(s mastracodeState) error {
	if s.dir.Exists && s.dir.Unsafe != "" {
		return refuse(s.dir.Unsafe, s.paths.Dir, "%s: %s", s.paths.Dir, s.dir.UnsafeDetail)
	}
	if s.config.Exists && s.config.Unsafe != "" {
		return refuse(s.config.Unsafe, s.paths.Config, "%s: %s", s.paths.Config, s.config.UnsafeDetail)
	}
	if s.scan.parseErr != "" {
		return refuse(RefuseUnreadable, s.paths.Config,
			"%s could not be interpreted (%s); Sidecar will not rewrite a file it cannot read — fix or move it yourself and run this again",
			s.paths.Config, s.scan.parseErr)
	}
	if s.backup.Exists && s.backup.Unsafe != "" {
		return refuse(s.backup.Unsafe, s.paths.Backup, "%s: %s", s.paths.Backup, s.backup.UnsafeDetail)
	}
	return nil
}

// stripMastracodeEntries removes every owned entry, dropping any event array left
// empty by the removal. Untouched nodes keep their original bytes; an event array
// Sidecar merely added an entry to keeps every other entry, in order.
func stripMastracodeEntries(s mastracodeScan) ([]jsonMember, bool, error) {
	if len(s.owned) == 0 {
		return s.top, false, nil
	}
	drop := map[string]bool{}
	for _, o := range s.owned {
		drop[o.event+"/"+strconv.Itoa(o.index)] = true
	}
	var kept []jsonMember
	changed := false
	for _, ev := range s.top {
		if !mastracodeEventNames[ev.key] {
			kept = append(kept, ev)
			continue
		}
		entries, err := parseJSONArray(ev.val)
		if err != nil {
			return nil, false, err
		}
		var keptEntries []json.RawMessage
		eventChanged := false
		for i, entryRaw := range entries {
			if drop[ev.key+"/"+strconv.Itoa(i)] {
				eventChanged = true
				continue
			}
			keptEntries = append(keptEntries, entryRaw)
		}
		switch {
		case !eventChanged:
			kept = append(kept, ev)
		case len(keptEntries) == 0:
			// The removal emptied the event, so the key goes with it: an empty
			// array is one Sidecar's entry was the whole point of. Mastra Code's own
			// loader drops an empty array too (validateConfig keeps a key only when
			// hooks.length > 0), so leaving one behind would be leaving a key the
			// provider ignores and the user did not write.
			changed = true
		default:
			kept = append(kept, jsonMember{key: ev.key, val: marshalJSONArray(keptEntries)})
			changed = true
		}
	}
	return kept, changed, nil
}

// appendMastracodeEntries appends this build's entry for every row to its event's
// array, creating the arrays it needs and never reordering what exists.
func appendMastracodeEntries(top []jsonMember) ([]jsonMember, error) {
	out := append([]jsonMember(nil), top...)
	for _, h := range mastracodeHooks {
		entry := mastracodeCanonicalEntry(h)
		if i, ok := lastMember(out, h.Event); ok {
			entries, err := parseJSONArray(out[i].val)
			if err != nil {
				return nil, err
			}
			out[i].val = marshalJSONArray(append(entries, entry))
			continue
		}
		out = append(out, jsonMember{key: h.Event, val: marshalJSONArray([]json.RawMessage{entry})})
	}
	return out, nil
}

var _ Adapter = MastracodeAdapter{}

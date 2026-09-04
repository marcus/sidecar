package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The shared session-identity adapter.
//
// Four providers -- Antigravity, GitHub Copilot CLI, Cursor Agent and grok --
// each want exactly what Claude and Codex already have: one hook entry in a
// JSON configuration file, invoking `sidecar agent report-session`, so the pane
// is bound to the provider's own conversation. Lifecycle state keeps coming
// from the screen lane for all four, which is what makes them one shape rather
// than four ports: the knowledge each one carries is a file path, an event
// name, an entry shape and the field the session id arrives in, and nothing
// else.
//
// So the lifecycle logic lives here once. What differs per provider is a
// descriptor, and the descriptors are in antigravity_install.go,
// copilot_install.go, cursor_install.go and grok_install.go beside the evidence
// for each field. Claude and Codex are deliberately NOT rewritten onto this:
// Claude's adapter is the one this was generalized from and has its own suite,
// and Codex needs three owned mutations across two files in two formats, so
// folding it in would mean a descriptor with a hole in it exactly where the
// hard part is.
//
// Ownership is hookconfig.go's rule for every one of them: an entry whose
// command invokes report-session is Sidecar's to manage, an entry equal to the
// bundled one is current, and every other byte in the file is preserved
// token-for-token and in order.

// sessionHookIntegration is one provider's session-identity hook, as data.
type sessionHookIntegration struct {
	// provider is the catalog agent kind, which is also the --kind the entry
	// claims.
	provider string
	// command is the executable to look for on PATH and to ask for a version.
	// It is separate from provider because Antigravity's family id is
	// `antigravity` and its binary is `agy`.
	command string
	// source is the integration id reports carry.
	source string
	// assetVersion is the bundled entry's version, and assetSchema the
	// plan/marker schema it declares.
	assetVersion string
	assetSchema  int
	// fileName is the configuration file's base name, which is also the asset's
	// name and the name every status message uses.
	fileName string
	// dir resolves the directory holding fileName, honouring whatever override
	// the provider itself honours.
	dir func(Env) string
	// spec is the entry's shape and its address inside the file.
	spec hookEntrySpec
	// item is what gets appended to the canonical event's array: a matcher
	// group for a grouped provider, the handler object itself for a flat one.
	item func() json.RawMessage
	// ensure are top-level members the provider requires the file to carry and
	// Sidecar adds when it is absent -- Cursor's `"version": 1` is the only one
	// today. They are removed again at uninstall, but only when they are all
	// that is left and each still holds the exact value Sidecar wrote, so a
	// file Sidecar created is a file Sidecar takes away and a value the user
	// changed is a value the user keeps.
	ensure []jsonMember
}

// sessionHookAdapter is the Adapter every session-identity provider shares.
type sessionHookAdapter struct {
	integration sessionHookIntegration
}

func (a sessionHookAdapter) Provider() string { return a.integration.provider }
func (a sessionHookAdapter) Source() string   { return a.integration.source }

// Assets returns the one entry asset this integration installs.
//
// It is OwnsEntry for all four, including grok, whose file Sidecar is the only
// writer of. Declaring OwnsFile there would say "every byte here is Sidecar's",
// and the consequence of that claim is that uninstall deletes the file: a user
// who added a hook of their own beside Sidecar's, in a file named after
// Sidecar, would lose it. The entry rule costs nothing and cannot do that.
func (a sessionHookAdapter) Assets() []Asset {
	return []Asset{{
		Name:          a.integration.fileName,
		Source:        a.integration.source,
		SchemaVersion: a.integration.assetSchema,
		Version:       a.integration.assetVersion,
		Ownership:     OwnsEntry,
		Content:       string(a.integration.canonicalFile()),
	}}
}

func (a sessionHookAdapter) asset() Asset { return a.Assets()[0] }

// canonicalFile is the file Sidecar would create in an empty tree. It is shown
// so a surface can name exactly what an install adds, and it is never bytes
// written over a user's file.
func (i sessionHookIntegration) canonicalFile() []byte {
	top, err := appendCanonicalEntry(nil, i.item(), i.spec)
	if err != nil {
		// The members were constructed by this package from a literal, so this
		// is unreachable.
		return nil
	}
	return renderJSONFile(ensureMembers(top, i.ensure))
}

// ensureMembers adds each required member the top level does not already carry,
// preserving the order of what is there.
func ensureMembers(top []jsonMember, required []jsonMember) []jsonMember {
	for _, want := range required {
		if _, ok := lastMember(top, want.key); ok {
			continue
		}
		// Prepended rather than appended, because these members are a file
		// header -- Cursor writes `version` first, and so does Sidecar.
		top = append([]jsonMember{want}, top...)
	}
	return top
}

// onlyEnsuredMembersRemain reports whether every member left is one Sidecar
// itself added, still holding the value Sidecar wrote. It is the test for
// whether removing the entry leaves a file that was Sidecar's own creation.
func onlyEnsuredMembersRemain(top []jsonMember, required []jsonMember) bool {
	if len(required) == 0 {
		return false
	}
	for _, m := range top {
		matched := false
		for _, want := range required {
			if m.key == want.key && sameJSON(m.val, want.val) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

type sessionHookPaths struct {
	Dir    string
	File   string
	Backup string
}

func (i sessionHookIntegration) pathsFor(env Env) sessionHookPaths {
	dir := i.dir(env)
	file := filepath.Join(dir, i.fileName)
	return sessionHookPaths{Dir: dir, File: file, Backup: file + sessionHookBackupSuffix}
}

// sessionHookBackupSuffix names the recoverable copy kept beside a provider's
// configuration file before any rewrite of a pre-existing file. It is the same
// suffix the Claude adapter uses, because it is the same promise.
const sessionHookBackupSuffix = ".sidecar-backup"

// sessionHookState is everything one inspection learned. Both Inspect and Plan
// are built from it, so a plan is never based on a different reading of the
// disk than the status the user was shown.
type sessionHookState struct {
	env    Env
	paths  sessionHookPaths
	spec   hookEntrySpec
	dir    FileState
	file   FileState
	backup FileState
	scan   hookTreeScan

	providerPath    string
	providerVersion string

	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a sessionHookAdapter) inspect(env Env) sessionHookState {
	i := a.integration
	p := i.pathsFor(env)
	s := sessionHookState{
		env:    env,
		paths:  p,
		spec:   i.spec,
		dir:    inspectDir(env, p.Dir),
		file:   inspectFile(env, p.File, a.asset()),
		backup: FileState{Path: p.Backup, Exists: fileExists(p.Backup)},
	}
	if path, ok := env.lookPath(i.command); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(i.command)
	}
	_, s.scan = scanEntryFile(s.file, s.spec)
	if len(s.scan.owned) > 0 {
		ownEntry(&s.file, s.scan.owned[len(s.scan.owned)-1].version)
	}
	s.assetStatus, s.message, s.installed = entryAssetStatus(s.dir, s.file, s.scan, s.spec, i.fileName)

	s.status = s.assetStatus
	if s.providerPath == "" {
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the " + i.command + " CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// Inspect implements [Adapter].
func (a sessionHookAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a sessionHookAdapter) statusOf(s sessionHookState) Status {
	i := a.integration
	capability, _ := agentlifecycle.CapabilityForSource(i.source)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              i.provider,
		Source:                i.source,
		Status:                s.status,
		BundledVersion:        i.assetVersion,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.File},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.file, s.backup}
	for _, act := range Actions() {
		if _, err := a.plan(s, act); err == nil {
			st.Offered = append(st.Offered, act)
		}
	}
	return st
}

// Plan implements [Adapter].
func (a sessionHookAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a sessionHookAdapter) plan(s sessionHookState, act Action) (Plan, error) {
	i := a.integration
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      i.provider,
		Source:        i.source,
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

// planConverge builds the plan that ends with exactly one canonical Sidecar
// entry on the canonical event and everything else in the file untouched.
func (a sessionHookAdapter) planConverge(s sessionHookState, p Plan, act Action) (Plan, error) {
	i := a.integration
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the %s CLI was not found on PATH, so Sidecar will not modify %s for it; install %s first",
			i.command, s.paths.File, i.command)
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.File, i.provider, s.installed, s.message); err != nil {
		return Plan{}, err
	}
	if err := refuseUnsafeEntryFile(s.dir, s.file, s.scan); err != nil {
		return Plan{}, err
	}

	if s.scan.converged(s.spec) && !ensureMembersMissing(s.scan.top, i.ensure) {
		p.Unchanged = true
		return p, nil
	}

	top, _, err := stripOwnedHookEntries(s.scan, s.spec)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.File, "%s: %v", s.paths.File, err)
	}
	top, err = appendCanonicalEntry(top, i.item(), s.spec)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.File, "%s: %v", s.paths.File, err)
	}
	content := renderJSONFile(ensureMembers(top, i.ensure))

	p.Ops = entryFileOps(nil, s.env, s.dir, s.file, s.backup, content,
		"write the Sidecar session-identity hook entry, preserving every other setting", ownedEntry(i.assetVersion))
	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// ensureMembersMissing reports whether the file is missing a member the
// provider requires. A converged entry in a file whose header Sidecar owes it
// is not converged, which is what stops `install` reporting "unchanged" for a
// Cursor hooks.json that has lost its `version`.
func ensureMembersMissing(top []jsonMember, required []jsonMember) bool {
	for _, want := range required {
		if _, ok := lastMember(top, want.key); !ok {
			return true
		}
	}
	return false
}

// planUninstall removes exactly Sidecar's entry and the containers that held
// nothing else, and never anything more.
func (a sessionHookAdapter) planUninstall(s sessionHookState, p Plan) (Plan, error) {
	if !s.file.Exists || len(s.scan.owned) == 0 && s.scan.parseErr == "" {
		p.Unchanged = true
		return p, nil
	}
	if err := refuseUnsafeEntryFile(s.dir, s.file, s.scan); err != nil {
		return Plan{}, err
	}

	top, changed, err := stripOwnedHookEntries(s.scan, s.spec)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.File, "%s: %v", s.paths.File, err)
	}
	if !changed {
		p.Unchanged = true
		return p, nil
	}
	if onlyEnsuredMembersRemain(top, a.integration.ensure) {
		// What is left is only the header Sidecar wrote to satisfy the
		// provider, unchanged since. The file was Sidecar's own creation, so it
		// goes rather than being left as a stub nobody wrote on purpose.
		top = nil
	}
	p.Ops = removalOps(s.file, s.backup, top,
		"remove the Sidecar session-identity hook entry, preserving every other setting")
	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

var _ Adapter = sessionHookAdapter{}

// sessionHookIntegrationOf returns the descriptor behind an adapter built on
// the shared session-identity implementation.
func sessionHookIntegrationOf(a Adapter) (sessionHookIntegration, bool) {
	switch v := a.(type) {
	case AntigravityAdapter:
		return v.integration, true
	}
	return sessionHookIntegration{}, false
}

// SessionHookArgvCorpus is every argv the installed session-identity entries
// can spawn, one per registered provider.
//
// It is exported for TestBundledAssetsSpawnArgvTheShippedCLIAccepts in
// internal/cli, which pushes each one through the real flag parser and the real
// kind resolution. The argv is read out of each adapter's canonical asset --
// the bytes an install writes -- rather than rebuilt from the provider id,
// because a test about what the shipped entry spawns has to read the shipped
// entry. That is the same reason KimiHookArgvCorpus reads its table rather than
// a copy of it.
//
// The one piece of shell any of these commands carries is a trailing
// `; printf ...`, which Antigravity's stdout contract requires. Splitting on
// the first `;` is therefore the whole of the shell parsing needed here, and
// TestASessionHookCommandIsOneInvocationPlusAtMostOneSuffix pins that it stays
// that way.
func SessionHookArgvCorpus() [][]string {
	var out [][]string
	for _, a := range DefaultAdapters() {
		i, ok := sessionHookIntegrationOf(a)
		if !ok {
			continue
		}
		command, ok := i.installedCommand()
		if !ok {
			continue
		}
		fields := strings.Fields(strings.SplitN(command, ";", 2)[0])
		if len(fields) < 2 {
			continue
		}
		out = append(out, fields[1:])
	}
	return out
}

// installedCommand reads the command out of the canonical asset's own entry.
func (i sessionHookIntegration) installedCommand() (string, bool) {
	scan := scanHookTree(true, i.canonicalFile(), i.spec)
	if scan.parseErr != "" || len(scan.owned) != 1 {
		return "", false
	}
	entry, err := parseJSONObject(scan.owned[0].raw)
	if err != nil {
		return "", false
	}
	return entryCommand(entry, i.spec)
}

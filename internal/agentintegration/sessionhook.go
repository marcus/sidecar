package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The shared session-identity adapter.
//
// Eight providers -- Antigravity, GitHub Copilot CLI, Cursor Agent, grok, the
// Devin CLI, Droid, the Qoder CLI and Qwen Code -- each want exactly what
// Claude and Codex already have: one hook entry in a JSON configuration file,
// invoking `sidecar agent report-session`, so the pane is bound to the
// provider's own conversation. Lifecycle state keeps coming from the screen
// lane for all of them, which is what makes them one shape rather than eight
// ports: the knowledge each one carries is a file path, an event name or list
// of them, an entry shape and the field the session id arrives in, and nothing
// else.
//
// So the lifecycle logic lives here once. What differs per provider is a
// descriptor, and the descriptors are in antigravity_install.go,
// copilot_install.go, cursor_install.go, grok_install.go, devin_install.go,
// droid_install.go, qodercli_install.go and qwen_install.go beside the evidence
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
	// newFileMembers are top-level members Sidecar writes when it CREATES the
	// file, and never adds to one that already exists. Cursor's `"version": 1`
	// is the only one today: cursor-agent's own writer puts it at the top of a
	// hooks.json it generates, so a file Sidecar creates should look like one
	// Cursor created, but its hook loader never reads the key, so adding it to
	// a user's existing file would be changing their file for no reason and
	// would leave uninstall unable to give it back byte for byte.
	//
	// They are removed again at uninstall when they are all that is left and
	// each still holds the exact value Sidecar wrote, so a file Sidecar created
	// does not survive as a stub nobody wrote on purpose, and a value the user
	// changed is a value the user keeps.
	newFileMembers []jsonMember
	// setupHint is the sentence a user gets when the configuration directory is
	// not there, and setting it is also what makes a missing directory a
	// refusal rather than something the install creates.
	//
	// Claude's own installer creates ~/.claude when it is absent, which is safe
	// because the CLI was found on PATH and reads that exact path, and
	// Antigravity, Copilot, Cursor and grok are the same: their directory is
	// fixed or comes from a variable the provider itself reads. Devin, Droid,
	// Qoder and Qwen all resolve a directory that an override may point
	// anywhere, and three of the four create it themselves on first run, so a
	// Sidecar that created it would happily write a settings file into a typo
	// and report the integration as current forever. Empty means the older
	// behaviour: create the directory as part of the install.
	setupHint string
	// shadowedBy names a sibling file inside the same directory whose mere
	// presence makes the entry in fileName inert.
	//
	// Droid is the one provider that has one, and it is exactly the failure a
	// status surface exists to catch: Droid reads hook declarations from
	// ~/.factory/hooks.json first and falls back to settings.json's hooks key
	// only when that file is absent. An entry Sidecar wrote into settings.json
	// while hooks.json existed would be correct, would read as current, and
	// would never fire. Sidecar does not edit the shadow file -- it is the
	// user's, Sidecar has never written to it, and moving somebody's hooks
	// between two files is not an integration's business -- so the honest
	// response is to inspect it and say so.
	shadowedBy string
	// shadowNote renders the sentence a status carries while the shadow file
	// exists. It takes the path so the user can act on it.
	shadowNote func(shadow string) string
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

// overrideDir reads a provider's own configuration-directory override the way
// the provider reads it, and returns "" when none applies so the caller falls
// back to the documented default.
//
// Two readings are shared by every provider that has such a variable. A value
// that is only whitespace is a variable somebody exported without a value
// rather than a directory named " ", which is the reading
// agentsession.PiAgentDir already takes of PI_CODING_AGENT_DIR. And a leading
// "~" is expanded, because a Sidecar that did not expand would read and write a
// literal directory named "~" while the provider used somewhere else entirely.
//
// [ClaudeConfigHome] deliberately does neither of these to a tilde: Claude
// refuses a configuration home that is not absolute, so "~/x" names no
// configuration home at all there. That is a difference in what the providers
// do, not a difference in taste, which is why it stays in its own function.
func overrideDir(home, value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return expandTildePath(home, trimmed)
	}
	return ""
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
	return renderJSONFile(withNewFileMembers(top, i.newFileMembers))
}

// withNewFileMembers adds each new-file member the top level does not already
// carry, preserving the order of what is there.
func withNewFileMembers(top []jsonMember, required []jsonMember) []jsonMember {
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

// onlyNewFileMembersRemain reports whether every member left is one Sidecar
// itself writes when it creates the file, still holding the value Sidecar
// wrote. It is the test for whether removing the entry leaves a file that was
// Sidecar's own creation.
func onlyNewFileMembersRemain(top []jsonMember, required []jsonMember) bool {
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
	// Shadow is the file that would make File's entry inert, for a provider
	// that has one. Sidecar reads it and never writes it.
	Shadow string
}

func (i sessionHookIntegration) pathsFor(env Env) sessionHookPaths {
	dir := i.dir(env)
	if dir == "" {
		// Nothing resolved a directory, so there is no file to name. Joining
		// the base name onto an empty directory would produce a relative path,
		// which is a path in whatever directory Sidecar happened to be started
		// from.
		return sessionHookPaths{}
	}
	file := filepath.Join(dir, i.fileName)
	p := sessionHookPaths{Dir: dir, File: file, Backup: file + sessionHookBackupSuffix}
	if i.shadowedBy != "" {
		p.Shadow = filepath.Join(dir, i.shadowedBy)
	}
	return p
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

	// shadow is the state of the file that would make the installed entry
	// inert, for a provider that has one. It is inspected, never written.
	shadow FileState

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

	// The shadow file is a fact about whether an installation FIRES, not about
	// whether it is installed, so it changes the message and never the status.
	// A user whose hooks live in the shadow file has a perfectly healthy
	// installation that does nothing, and calling that needs-repair would offer
	// them a repair verb that cannot fix it.
	if i.shadowedBy != "" {
		s.shadow = FileState{Path: p.Shadow, Exists: fileExists(p.Shadow)}
		if s.shadow.Exists && i.shadowNote != nil {
			note := i.shadowNote(p.Shadow)
			s.message = note + orEmpty("; "+s.message, s.message != "")
		}
	}

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
	if i.shadowedBy != "" {
		// Reported among the files this adapter inspected, and deliberately not
		// among TargetPaths: TargetPaths are the files an install or uninstall
		// would touch, and Sidecar never writes this one.
		st.Files = append(st.Files, s.shadow)
	}
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
// entry under each of the spec's events and everything else in the file
// untouched. A provider carrying a setupHint refuses rather than creating its
// configuration directory; see the field for why the two halves differ.
func (a sessionHookAdapter) planConverge(s sessionHookState, p Plan, act Action) (Plan, error) {
	i := a.integration
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the %s CLI was not found on PATH, so Sidecar will not modify %s for it; install %s first",
			i.command, s.paths.File, i.command)
	}
	if i.setupHint != "" && !s.dir.Exists {
		return Plan{}, refuse(RefuseNotInstalled, s.paths.Dir, "%s", i.setupHint)
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.File, i.provider, s.installed, s.message); err != nil {
		return Plan{}, err
	}
	if err := refuseUnsafeEntryFile(s.dir, s.file, s.scan); err != nil {
		return Plan{}, err
	}

	if s.scan.converged(s.spec) {
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
	// The provider's own file header goes in only when Sidecar is creating the
	// file. Adding it to a file the user already has would be editing bytes
	// outside the entry, which is the one thing this installer promises not to
	// do.
	if !s.file.Exists {
		top = withNewFileMembers(top, i.newFileMembers)
	}
	content := renderJSONFile(top)

	p.Ops = entryFileOps(nil, s.env, s.dir, s.file, s.backup, content,
		"write the Sidecar session-identity hook entry, preserving every other setting", ownedEntry(i.assetVersion))
	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
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
	if onlyNewFileMembersRemain(top, a.integration.newFileMembers) {
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

// sessionHookIntegrationData exposes the descriptor to the discovery below. It
// is a method rather than a field read because every adapter in this group
// embeds sessionHookAdapter by value and is reached through the Adapter
// interface.
func (a sessionHookAdapter) sessionHookIntegrationData() sessionHookIntegration {
	return a.integration
}

// sessionHookIntegrationOf returns the descriptor behind an adapter built on
// the shared session-identity implementation.
//
// Discovery is by the promoted method rather than by a list of concrete types,
// so an adapter registered on this implementation is covered by everything
// below without being named anywhere. A type switch was what this used to be,
// and it is exactly the list that goes stale the first time somebody adds a
// ninth provider.
func sessionHookIntegrationOf(a Adapter) (sessionHookIntegration, bool) {
	type integrated interface {
		sessionHookIntegrationData() sessionHookIntegration
	}
	if v, ok := a.(integrated); ok {
		return v.sessionHookIntegrationData(), true
	}
	return sessionHookIntegration{}, false
}

// SessionHookProviders names every provider whose integration is one of these
// session-identity hook entries, in registration order.
func SessionHookProviders() []string {
	var out []string
	for _, a := range DefaultAdapters() {
		if _, ok := sessionHookIntegrationOf(a); ok {
			out = append(out, a.Provider())
		}
	}
	return out
}

// SessionHookArgvCorpus is every argv the installed session-identity entries
// can spawn, one row per event each registered provider installs under.
//
// Every event spawns the identical command -- the payload on stdin is what
// differs, and the CLI never sees the event name as an argument -- so the rows
// for one provider repeat. That repetition is the point for Devin: six entries
// mean six processes, and a change that made one of them spawn something the
// CLI refuses would fail here.
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
	for _, provider := range SessionHookProviders() {
		out = append(out, SessionHookArgvCorpusFor(provider)...)
	}
	return out
}

// SessionHookArgvCorpusFor is one registered provider's rows of that corpus,
// for a test that wants to name the provider a failure belongs to.
func SessionHookArgvCorpusFor(provider string) [][]string {
	for _, a := range DefaultAdapters() {
		i, ok := sessionHookIntegrationOf(a)
		if !ok || a.Provider() != provider {
			continue
		}
		command, ok := i.installedCommand()
		if !ok {
			return nil
		}
		fields := strings.Fields(strings.SplitN(command, ";", 2)[0])
		if len(fields) < 2 {
			return nil
		}
		var out [][]string
		for range i.spec.eventNames() {
			out = append(out, append([]string(nil), fields[1:]...))
		}
		return out
	}
	return nil
}

// installedCommand reads the command out of the canonical asset's own entry.
//
// The asset carries one entry per event, and all of them are the same bytes, so
// finding the expected number of owned entries and reading the first is both
// the check that the asset renders what the spec describes and the answer.
func (i sessionHookIntegration) installedCommand() (string, bool) {
	scan := scanHookTree(true, i.canonicalFile(), i.spec)
	if scan.parseErr != "" || len(scan.owned) != len(i.spec.eventNames()) {
		return "", false
	}
	entry, err := parseJSONObject(scan.owned[0].raw)
	if err != nil {
		return "", false
	}
	return entryCommand(entry, i.spec)
}

// sessionHookIntegrationFor finds one session-identity provider's descriptor
// among the shipped adapters, so a caller reading the descriptor cannot drift
// from what is registered.
func sessionHookIntegrationFor(provider string) (sessionHookIntegration, bool) {
	for _, a := range DefaultAdapters() {
		if i, ok := sessionHookIntegrationOf(a); ok && a.Provider() == provider {
			return i, true
		}
	}
	return sessionHookIntegration{}, false
}

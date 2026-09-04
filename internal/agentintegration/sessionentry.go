package agentintegration

// The session-identity entry adapter.
//
// Four of Herdr's integrations exist for one purpose only: bind the pane to the
// provider's own conversation. Their upstream shape is identical -- drop a
// shell script, then register it as a `{type, command, timeout}` hook entry
// under one or more events in a JSON settings file the user owns -- and Sidecar
// makes the same swap it already made for Claude and Codex: the script and its
// python3 dependency go away, and the config entry invokes
// `sidecar agent report-session --kind <id> --hook-stdin` directly. What is left
// per provider is data: where the settings file is, what relocates it, which
// events carry the session id, and what matcher and timeout the provider's own
// schema wants.
//
// So this file is the machinery and each provider file is the data. That is a
// deliberate departure from claude_install.go and codex_install.go, which each
// carry their own copy: those two were written one at a time against providers
// whose settings files differ in more than these fields (Codex edits a second
// TOML file for a feature flag and a trust record), and four more hand copies of
// the same 250 lines would be four places for the ownership rules to drift.
// Nothing here is Claude's or Codex's to change; they keep their own adapters.
//
// Every rule the Claude adapter states holds here unchanged, because they are
// the same functions: ownership is by the report-session command and nothing
// else, a pre-existing user file is backed up before it is rewritten, an
// unparseable file is refused rather than clobbered, and everything outside the
// path to Sidecar's entry is preserved token-for-token and in order.

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// sessionEntrySchema is the plan/marker schema these assets declare.
const sessionEntrySchema = 1

// sessionEntryBackupSuffix names the recoverable copy kept beside a settings
// file before any rewrite of a pre-existing one.
const sessionEntryBackupSuffix = ".sidecar-backup"

// sessionEntrySpec is one provider's session-identity integration, as data.
type sessionEntrySpec struct {
	// provider is the catalog agent kind, and the id `--kind` carries.
	provider string
	// source is the integration identifier reports carry.
	source string
	// command is the provider executable looked up on PATH and asked for its
	// version. It is separate from provider because the two differ: the Qoder
	// CLI's catalog id is `qodercli` and its binary is `qoder`.
	command string
	// version is the bundled entry's asset version.
	version string

	// dirSegments locate the provider's configuration directory under $HOME
	// when no override applies.
	dirSegments []string
	// dirOverride reads the provider's own config-dir environment override out
	// of Env, or returns "" when the provider documents none. Honouring it is
	// what lets a relocated provider be found, and what lets a proof run
	// redirect the provider away from the user's real configuration.
	dirOverride func(Env) string
	// dirFromConfigHome makes the directory a child of $XDG_CONFIG_HOME rather
	// than of $HOME. Devin is the one provider that resolves this way.
	dirFromConfigHome bool
	// file is the settings file's base name inside the directory.
	file string

	// matcher is the canonical group matcher: nil writes no matcher key at all,
	// non-nil writes exactly that value.
	matcher *string
	// events are the hook events the entry is installed under, in order.
	events []string
	// timeout is the value the entry's `timeout` field carries, in whatever
	// unit the provider's own schema uses.
	timeout int

	// setupHint is the sentence a user gets when the configuration directory is
	// not there. It names the provider's own way to create it.
	setupHint string
}

// canonicalEntry is the exact hook entry this spec's version ships.
func (s sessionEntrySpec) canonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(reportSessionCommand(s.provider))},
		{key: "timeout", val: json.RawMessage(strconv.Itoa(s.timeout))},
	})
}

// canonicalGroup is the group the entry is installed in.
func (s sessionEntrySpec) canonicalGroup() json.RawMessage {
	var members []jsonMember
	if s.matcher != nil {
		members = append(members, jsonMember{key: "matcher", val: mustJSONString(*s.matcher)})
	}
	members = append(members, jsonMember{key: "hooks", val: marshalJSONArray([]json.RawMessage{s.canonicalEntry()})})
	return marshalJSONObject(members)
}

func (s sessionEntrySpec) entrySpec() hookEntrySpec {
	return hookEntrySpec{
		matcher: s.matcher,
		events:  s.events,
		canonical: []versionedEntry{
			{version: s.version, entry: s.canonicalEntry()},
		},
	}
}

// content is the canonical file Sidecar would create in an empty tree. It is a
// description of what an install adds, shown by a surface, never bytes written
// over a user's file.
func (s sessionEntrySpec) content() []byte {
	var events []jsonMember
	for _, name := range s.eventNames() {
		events = append(events, jsonMember{key: name, val: marshalJSONArray([]json.RawMessage{s.canonicalGroup()})})
	}
	return renderJSONFile([]jsonMember{{key: "hooks", val: marshalJSONObject(events)}})
}

func (s sessionEntrySpec) eventNames() []string { return s.entrySpec().eventNames() }

// sessionEntryPaths are the paths one of these adapters inspects and touches.
type sessionEntryPaths struct {
	Dir      string
	Settings string
	Backup   string
}

func (s sessionEntrySpec) pathsFor(env Env) sessionEntryPaths {
	dir := s.dirFor(env)
	if dir == "" {
		return sessionEntryPaths{}
	}
	settings := filepath.Join(dir, s.file)
	return sessionEntryPaths{Dir: dir, Settings: settings, Backup: settings + sessionEntryBackupSuffix}
}

// dirFor resolves the provider's configuration directory the way the provider
// itself does: its own override when it has one and it is set, otherwise the
// documented default.
//
// The tilde expansion matches Herdr's expand_tilde_path and Sidecar's
// kimiHomeDir, and it matters for the same reason: a Sidecar that did not
// expand would read and write a literal directory named "~" while the provider
// used somewhere else entirely. A whitespace-only value is a variable somebody
// exported without a value, not a directory named " ".
func (s sessionEntrySpec) dirFor(env Env) string {
	if s.dirOverride != nil {
		if value := strings.TrimSpace(s.dirOverride(env)); value != "" {
			return expandHomePath(env.Home, value)
		}
	}
	if s.dirFromConfigHome {
		if base := strings.TrimSpace(env.ConfigHome); base != "" {
			return filepath.Join(expandHomePath(env.Home, base), s.dirSegments[len(s.dirSegments)-1])
		}
	}
	if env.Home == "" {
		return ""
	}
	return filepath.Join(append([]string{env.Home}, s.dirSegments...)...)
}

// expandHomePath expands a leading "~" against the home directory.
func expandHomePath(home, value string) string {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(value, "~"), "/"))
}

// sessionEntryAdapter is the [Adapter] every session-identity entry integration
// is an instance of.
type sessionEntryAdapter struct{ spec sessionEntrySpec }

func (a sessionEntryAdapter) Provider() string { return a.spec.provider }
func (a sessionEntryAdapter) Source() string   { return a.spec.source }

// Assets returns the one entry asset this integration installs.
//
// It is OwnsEntry: the settings file belongs to the user, Sidecar owns its own
// entries inside it, and Content describes exactly what an install adds.
func (a sessionEntryAdapter) Assets() []Asset {
	return []Asset{{
		Name:          a.spec.file,
		Source:        a.spec.source,
		SchemaVersion: sessionEntrySchema,
		Version:       a.spec.version,
		Ownership:     OwnsEntry,
		Content:       string(a.spec.content()),
	}}
}

func (a sessionEntryAdapter) settingsAsset() Asset { return a.Assets()[0] }

// sessionEntryState is everything one inspection learned. Both Inspect and Plan
// are built from it, so a plan is never based on a different reading of the
// disk than the status the user was shown.
type sessionEntryState struct {
	env      Env
	paths    sessionEntryPaths
	spec     hookEntrySpec
	dir      FileState
	settings FileState
	backup   FileState
	raw      []byte
	scan     hookTreeScan

	providerPath    string
	providerVersion string

	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a sessionEntryAdapter) inspect(env Env) sessionEntryState {
	p := a.spec.pathsFor(env)
	s := sessionEntryState{
		env:      env,
		paths:    p,
		spec:     a.spec.entrySpec(),
		dir:      inspectDir(env, p.Dir),
		settings: inspectFile(env, p.Settings, a.settingsAsset()),
		backup:   FileState{Path: p.Backup, Exists: fileExists(p.Backup)},
	}
	if path, ok := env.lookPath(a.spec.command); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(a.spec.command)
	}
	s.raw, s.scan = scanEntryFile(s.settings, s.spec)
	if len(s.scan.owned) > 0 {
		ownEntry(&s.settings, s.scan.owned[len(s.scan.owned)-1].version)
	}
	s.assetStatus, s.message, s.installed = entryAssetStatus(s.dir, s.settings, s.scan, s.spec, a.spec.file)

	s.status = s.assetStatus
	if s.providerPath == "" {
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the " + a.spec.command + " CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// Inspect implements [Adapter].
func (a sessionEntryAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a sessionEntryAdapter) statusOf(s sessionEntryState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(a.spec.source)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              a.spec.provider,
		Source:                a.spec.source,
		Status:                s.status,
		BundledVersion:        a.spec.version,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.Settings},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.settings, s.backup}
	for _, act := range Actions() {
		if _, err := a.plan(s, act); err == nil {
			st.Offered = append(st.Offered, act)
		}
	}
	return st
}

// Plan implements [Adapter].
func (a sessionEntryAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a sessionEntryAdapter) plan(s sessionEntryState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      a.spec.provider,
		Source:        a.spec.source,
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
// entry under each of the provider's events, and everything else untouched.
//
// The missing-directory refusal is the one rule this shape adds over Claude's.
// Claude's own installer creates ~/.claude when it is absent, which is safe
// because the CLI was found on PATH and reads that exact path. These four
// providers all resolve their directory through an override that may point
// anywhere, and three of the four create it themselves on first run, so a
// Sidecar that created it would happily write a settings file into a typo and
// report the integration as current forever.
func (a sessionEntryAdapter) planConverge(s sessionEntryState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the %s CLI was not found on PATH, so Sidecar will not modify %s for it; install %s first",
			a.spec.command, s.paths.Settings, a.spec.command)
	}
	if !s.dir.Exists {
		return Plan{}, refuse(RefuseNotInstalled, s.paths.Dir, "%s", a.spec.setupHint)
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.Settings, a.spec.provider, s.installed, s.message); err != nil {
		return Plan{}, err
	}
	if err := refuseUnsafeEntryFile(s.dir, s.settings, s.scan); err != nil {
		return Plan{}, err
	}

	if s.scan.converged(s.spec) {
		p.Unchanged = true
		return p, nil
	}

	top, _, err := stripOwnedHookEntries(s.scan)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Settings, "%s: %v", s.paths.Settings, err)
	}
	top, err = appendCanonicalGroups(top, a.spec.eventNames(), a.spec.canonicalGroup())
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Settings, "%s: %v", s.paths.Settings, err)
	}
	content := renderJSONFile(top)

	p.Ops = entryFileOps(nil, s.env, s.dir, s.settings, s.backup, content,
		"write the Sidecar session-identity hook entry, preserving every other setting", ownedEntry(a.spec.version))
	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes exactly Sidecar's entries and the containers that held
// nothing else, and never anything more.
func (a sessionEntryAdapter) planUninstall(s sessionEntryState, p Plan) (Plan, error) {
	if !s.settings.Exists || len(s.scan.owned) == 0 && s.scan.parseErr == "" {
		p.Unchanged = true
		return p, nil
	}
	if err := refuseUnsafeEntryFile(s.dir, s.settings, s.scan); err != nil {
		return Plan{}, err
	}

	top, changed, err := stripOwnedHookEntries(s.scan)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Settings, "%s: %v", s.paths.Settings, err)
	}
	if !changed {
		p.Unchanged = true
		return p, nil
	}
	p.Ops = removalOps(s.settings, s.backup, top,
		"remove the Sidecar session-identity hook entry, preserving every other setting")
	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

// SessionEntryArgvCorpus is every argv one session-identity integration can
// spawn, one per event it is installed under.
//
// It is exported for TestBundledAssetsSpawnArgvTheShippedCLIAccepts in
// internal/cli, which pushes each one through the real flag parser and the real
// kind resolution. For an asset that is a file of JavaScript that test has to
// run the file to learn what it spawns; for an asset that is a table of hook
// entries the table is the answer, and taking it from the same place the
// installer renders from is what makes the test about the shipped bytes.
//
// Every event spawns the identical command -- the payload on stdin is what
// differs, and the CLI never sees the event name as an argument -- so the
// corpus is one row per event rather than one distinct row. That repetition is
// the point for Devin: six entries mean six processes, and a change that made
// one of them spawn something the CLI refuses would fail here.
func SessionEntryArgvCorpus(provider string) [][]string {
	spec, ok := sessionEntrySpecFor(provider)
	if !ok {
		return nil
	}
	argv := []string{"agent", reportSessionVerb, "--kind", spec.provider, "--hook-stdin"}
	out := make([][]string, 0, len(spec.eventNames()))
	for range spec.eventNames() {
		out = append(out, append([]string(nil), argv...))
	}
	return out
}

// sessionEntrySpecFor finds one session-identity provider's spec among the
// shipped adapters, so the corpus above cannot drift from what is registered.
func sessionEntrySpecFor(provider string) (sessionEntrySpec, bool) {
	for _, adapter := range DefaultAdapters() {
		type specced interface{ entrySpecData() sessionEntrySpec }
		if s, ok := adapter.(specced); ok && adapter.Provider() == provider {
			return s.entrySpecData(), true
		}
	}
	return sessionEntrySpec{}, false
}

// entrySpecData exposes the spec to sessionEntrySpecFor. It is a method rather
// than a field read because DevinAdapter and its siblings embed the generic
// adapter by value and are reached through the Adapter interface.
func (a sessionEntryAdapter) entrySpecData() sessionEntrySpec { return a.spec }

// SessionEntryProviders names every provider whose integration is one of these
// session-identity hook entries, in registration order.
func SessionEntryProviders() []string {
	var out []string
	for _, adapter := range DefaultAdapters() {
		type specced interface{ entrySpecData() sessionEntrySpec }
		if _, ok := adapter.(specced); ok {
			out = append(out, adapter.Provider())
		}
	}
	return out
}

var _ Adapter = sessionEntryAdapter{}

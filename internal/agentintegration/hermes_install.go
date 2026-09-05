package agentintegration

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Hermes Agent adapter.
//
// Hermes is the first provider in the tree that needs BOTH shapes of ownership
// at once, and that is what makes it worth reading rather than skimming. A
// directory plugin is two files Sidecar writes whole -- __init__.py and the
// plugin.yaml Hermes refuses to load a plugin without -- dropped into
// <hermes home>/plugins/sidecar-agent-state/. And a plugin Hermes has found is
// still inert until its name appears in `plugins.enabled` in the user's
// config.yaml, so there is a third asset that is one line inside a file Sidecar
// must otherwise leave alone. Herdr's installer does the same three things.
//
// Three facts about where Hermes reads all of this, checked against hermes
// 0.17.0's own shipped source rather than read from prose:
//
//   - The home is $HERMES_HOME, falling back to ~/.hermes. That is
//     hermes_constants.get_hermes_home, which its own docstring calls "the
//     single source of truth -- all other copies should import this", and every
//     other copy does. Herdr honours the same variable.
//   - User plugins are <hermes home>/plugins/<name>/, scanned for a plugin.yaml
//     beside an __init__.py exporting register(ctx). A directory holding one
//     without the other is skipped silently, which is why the two files are one
//     asset in two parts here.
//   - Loading is opt-in. hermes_cli/plugins.py reads `plugins.enabled` and loads
//     a user plugin only when its path-derived key or its manifest name appears
//     there; a missing or malformed key means nothing is enabled. So the config
//     line is not an optimisation, it is half the integration.
//
// Hermes also loads bundled plugins from its own installation directory,
// project plugins from ./.hermes/plugins when HERMES_ENABLE_PROJECT_PLUGINS is
// set, and pip plugins through an entry point group. Sidecar installs into none
// of them: the first is Hermes's own tree, the second would follow a checkout
// into other people's clones, and the third would mean shipping a Python
// package.
//
// # Where this is stricter than Herdr
//
// Herdr's uninstall_hermes removes its plugin directory with remove_dir_all and
// strips its config line without checking that either is still its own.
// Sidecar's never removes anything it cannot prove it wrote: a file that has
// stopped carrying the marker is somebody else's now, a foreign file at the
// asset's own path is refused rather than adopted, and the plugin directory goes
// only when removing Sidecar's own files leaves it empty.

// HermesBackupSuffix names the recoverable copy kept beside a file before it is
// replaced or edited.
const HermesBackupSuffix = ".sidecar-backup"

// HermesAdapter installs Sidecar's Hermes session-identity plugin.
type HermesAdapter struct{}

func (HermesAdapter) Provider() string { return HermesProvider }
func (HermesAdapter) Source() string   { return HermesSource }

// Assets returns the three units this integration installs, in the order a
// surface should show them: the plugin, its manifest, and the line that turns
// the plugin on.
func (HermesAdapter) Assets() []Asset {
	return []Asset{
		{
			Name:          HermesInitName,
			Source:        HermesSource,
			SchemaVersion: HermesAssetSchema,
			Version:       HermesAssetVersion,
			Ownership:     OwnsFile,
			CommentPrefix: "#",
			Content:       hermesInitAsset,
		},
		{
			Name:          HermesManifestName,
			Source:        HermesSource,
			SchemaVersion: HermesAssetSchema,
			Version:       HermesAssetVersion,
			Ownership:     OwnsFile,
			CommentPrefix: "#",
			Content:       hermesManifestAsset,
		},
		{
			Name:          HermesConfigName,
			Source:        HermesSource,
			SchemaVersion: HermesAssetSchema,
			Version:       HermesAssetVersion,
			Ownership:     OwnsEntry,
			// The canonical file Sidecar would create in an empty tree, which
			// for an OwnsEntry asset is a description of the entry rather than
			// something ever written verbatim over a user's file.
			Content: "plugins:\n  enabled:\n" + hermesEnableLine(4, HermesAssetVersion) + "\n",
		},
	}
}

func (a HermesAdapter) initAsset() Asset     { return a.Assets()[0] }
func (a HermesAdapter) manifestAsset() Asset { return a.Assets()[1] }
func (a HermesAdapter) configAsset() Asset   { return a.Assets()[2] }

// hermesPaths are the exact user-level paths this adapter inspects.
type hermesPaths struct {
	Home           string
	PluginsDir     string
	PluginDir      string
	Init           string
	Manifest       string
	PyCache        string
	InitBackup     string
	ManifestBackup string
	Config         string
	ConfigBackup   string
}

// HermesHome resolves Hermes's home directory the way Hermes does: $HERMES_HOME
// when it is set, ~/.hermes otherwise.
//
// The trim is Sidecar's own and is deliberate, for the reason
// agentsession.PiAgentDir records: a variable somebody exported without a value
// is not a directory named " ".
func HermesHome(env Env) string {
	if value := strings.TrimSpace(env.HermesHome); value != "" {
		return expandTildePath(env.Home, value)
	}
	return filepath.Join(env.Home, ".hermes")
}

// HermesPaths returns the paths the Hermes adapter would inspect and touch.
//
// It is exported because "show the exact paths before mutating" is a rule, and a
// surface that wants to name them before asking for confirmation should not have
// to reconstruct them.
func HermesPaths(env Env) []string {
	p := hermesPathsFor(env)
	return []string{p.Init, p.Manifest, p.Config}
}

func hermesPathsFor(env Env) hermesPaths {
	home := HermesHome(env)
	plugins := filepath.Join(home, "plugins")
	dir := filepath.Join(plugins, HermesPluginName)
	config := filepath.Join(home, HermesConfigName)
	return hermesPaths{
		Home:           home,
		PluginsDir:     plugins,
		PluginDir:      dir,
		Init:           filepath.Join(dir, HermesInitName),
		Manifest:       filepath.Join(dir, HermesManifestName),
		PyCache:        filepath.Join(dir, "__pycache__"),
		InitBackup:     filepath.Join(dir, HermesInitName+HermesBackupSuffix),
		ManifestBackup: filepath.Join(dir, HermesManifestName+HermesBackupSuffix),
		Config:         config,
		ConfigBackup:   config + HermesBackupSuffix,
	}
}

// hermesNeverSetUp is the one sentence for "hermes's home is not there".
//
// It is a function because the same fact has to reach a user through two
// different surfaces -- the refusal a caller gets from Plan, and the message on
// a status that offers no install -- and a status that stayed silent while the
// refusal explained itself is how a missing action looks like a bug.
func hermesNeverSetUp(home string) string {
	return "hermes's home directory " + home + " does not exist, so Hermes Agent has not been set up on this machine; " +
		"run hermes once (or set HERMES_HOME) and try again"
}

// hermesState is everything one inspection learned. Both [HermesAdapter.Inspect]
// and [HermesAdapter.Plan] are built from it, so a plan can never be based on a
// different reading of the disk than the status the user was shown.
type hermesState struct {
	env   Env
	paths hermesPaths

	home       FileState
	pluginsDir FileState
	dir        FileState
	initFile   FileState
	manifest   FileState
	initBackup FileState
	manBackup  FileState
	config     FileState
	confBackup FileState

	// parsed is the config.yaml read for editing, and configErr is why it could
	// not be.
	parsed    hermesConfig
	configErr error

	providerPath    string
	providerVersion string

	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a HermesAdapter) inspect(env Env) hermesState {
	p := hermesPathsFor(env)
	s := hermesState{
		env:        env,
		paths:      p,
		home:       inspectDir(env, p.Home),
		pluginsDir: inspectDir(env, p.PluginsDir),
		dir:        inspectDir(env, p.PluginDir),
		initFile:   inspectFile(env, p.Init, a.initAsset()),
		manifest:   inspectFile(env, p.Manifest, a.manifestAsset()),
		initBackup: inspectFile(env, p.InitBackup, Asset{}),
		manBackup:  inspectFile(env, p.ManifestBackup, Asset{}),
		config:     inspectFile(env, p.Config, a.configAsset()),
		confBackup: inspectFile(env, p.ConfigBackup, Asset{}),
	}

	if s.config.Exists && s.config.Unsafe == "" {
		content, err := readFileString(p.Config)
		if err != nil {
			s.configErr = err
		} else if parsed, perr := readHermesConfig(content); perr != nil {
			s.configErr = perr
		} else {
			s.parsed = parsed
			if parsed.Installed() {
				ownEntry(&s.config, parsed.ownedVersion)
			}
		}
	} else if !s.config.Exists {
		// An absent config.yaml is ordinary: Hermes creates it lazily, and a
		// home where nothing has been configured has none. It parses as the
		// empty document, which is exactly what an install has to add a key to.
		s.parsed, _ = readHermesConfig("")
	}

	if path, ok := env.lookPath(HermesCommand); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(HermesProvider)
	}

	s.assetStatus, s.message = hermesAssetStatus(a, s)
	if s.initFile.Owned {
		s.installed = s.initFile.Version
	}

	s.status = s.assetStatus
	if s.providerPath == "" {
		// The provider CLI being absent is the more actionable of the two true
		// statements, and it is also the one that decides authority: with no
		// hermes there is nothing to load the plugin, so TierFor is right to
		// return screen fallback. The assets' own state is still reported in the
		// message and in Files, so an uninstall after removing the provider is
		// still discoverable.
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the hermes CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// hermesAssetStatus decides the status from the inspected files alone.
//
// Nothing here trusts a version a report claimed. The installed bytes are hashed
// and compared with the bundled assets' hashes, so a truncated, hand-edited or
// half-written plugin reads as needs-repair rather than as current.
func hermesAssetStatus(a HermesAdapter, s hermesState) (agentlifecycle.IntegrationStatus, string) {
	for _, unsafe := range []struct {
		state FileState
		path  string
	}{
		{s.dir, s.paths.PluginDir},
		{s.initFile, s.paths.Init},
		{s.manifest, s.paths.Manifest},
		{s.config, s.paths.Config},
	} {
		if unsafe.state.Exists && unsafe.state.Unsafe != "" {
			return agentlifecycle.StatusNeedsRepair, unsafe.state.UnsafeDetail + " (" + unsafe.path + ")"
		}
	}
	if s.configErr != nil {
		if errors.Is(s.configErr, errHermesConfigInvalid) {
			return agentlifecycle.StatusNeedsRepair, s.paths.Config + " is not valid YAML, so Sidecar will not edit it"
		}
		return agentlifecycle.StatusNeedsRepair, s.paths.Config + " could not be read"
	}
	if !s.parsed.Editable() {
		return agentlifecycle.StatusNeedsRepair, "the plugins key in " + s.paths.Config +
			" is a flow sequence or a scalar rather than the block mapping Hermes writes; Sidecar edits one line and will not rewrite it. " +
			"Run `hermes plugins enable " + HermesPluginName + "` yourself, or rewrite the key as a block list"
	}
	for _, foreign := range []struct {
		state FileState
		path  string
	}{{s.initFile, s.paths.Init}, {s.manifest, s.paths.Manifest}} {
		if foreign.state.Exists && !foreign.state.Owned {
			return agentlifecycle.StatusNeedsRepair,
				"a file that is not Sidecar's occupies " + foreign.path + "; Sidecar will not modify or remove it"
		}
	}

	present := 0
	for _, ok := range []bool{s.initFile.Owned, s.manifest.Owned, s.parsed.Installed()} {
		if ok {
			present++
		}
	}
	switch present {
	case 0:
		if !s.home.Exists {
			// The status has to carry this, not only the refusal. Without it a
			// machine where hermes is on PATH but has never been run reads as a
			// plain not-installed with an empty message and no install offered,
			// and nothing on the status surface says why the one action that
			// would fix it is missing. Offered is computed by asking the
			// planner, so the absence is real; this is the sentence that
			// explains it, and it is deliberately the same sentence
			// planConverge refuses with.
			return agentlifecycle.StatusNotInstalled, hermesNeverSetUp(s.paths.Home)
		}
		return agentlifecycle.StatusNotInstalled, ""
	case 1, 2:
		// Hermes needs all three: a plugin without its manifest is skipped
		// silently, and a plugin that is not in `plugins.enabled` is never
		// loaded at all. So a partial install is not a lesser install, it is one
		// that reports nothing while looking present.
		return agentlifecycle.StatusNeedsRepair, hermesPartial(s)
	}

	if s.initFile.Checksum == a.initAsset().Checksum() && s.manifest.Checksum == a.manifestAsset().Checksum() {
		return agentlifecycle.StatusCurrent, ""
	}
	if s.initFile.Version != HermesAssetVersion || s.manifest.Version != HermesAssetVersion {
		return agentlifecycle.StatusOutdated,
			"version " + s.initFile.Version + " is installed; this build ships version " + HermesAssetVersion
	}
	return agentlifecycle.StatusNeedsRepair,
		"the installed plugin claims version " + s.initFile.Version + " but its contents do not match the bundled asset"
}

// hermesPartial names which of the three parts is missing, because "half
// installed" is not an actionable sentence and "the manifest is missing" is.
func hermesPartial(s hermesState) string {
	var missing []string
	if !s.initFile.Owned {
		missing = append(missing, s.paths.Init)
	}
	if !s.manifest.Owned {
		missing = append(missing, s.paths.Manifest)
	}
	if !s.parsed.Installed() {
		missing = append(missing, HermesPluginName+" is not listed under plugins.enabled in "+s.paths.Config)
	}
	return "the integration is only partly installed, so Hermes loads nothing: " + strings.Join(missing, ", ") +
		" is missing; run sidecar agent integration repair " + HermesProvider
}

// Inspect implements [Adapter].
func (a HermesAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a HermesAdapter) statusOf(s hermesState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(HermesSource)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              HermesProvider,
		Source:                HermesSource,
		Status:                s.status,
		BundledVersion:        HermesAssetVersion,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.Init, s.paths.Manifest, s.paths.Config},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.initFile, s.manifest, s.config, s.initBackup, s.manBackup, s.confBackup}

	// Offered is computed by asking the planner, not by restating its rules in a
	// second place. A verb a surface offers is therefore a verb that will not
	// refuse when it is pressed.
	for _, act := range Actions() {
		if _, err := a.plan(s, act); err == nil {
			st.Offered = append(st.Offered, act)
		}
	}
	return st
}

// Plan implements [Adapter].
func (a HermesAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a HermesAdapter) plan(s hermesState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      HermesProvider,
		Source:        HermesSource,
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

// planConverge builds the plan that ends with both plugin files at the bundled
// version and exactly one enable line in the user's config.yaml.
//
// install, update and repair share it because the target state is identical;
// they differ only in which starting states they accept.
func (a HermesAdapter) planConverge(s hermesState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the hermes CLI was not found on PATH, so Sidecar will not create %s for it; install Hermes Agent first", s.paths.PluginDir)
	}
	// Herdr's install_hermes refuses when the home is not a directory, with
	// "install hermes agent first". The semantics are worth keeping and the
	// reason is not fussiness: Hermes creates its home on first run, so its
	// absence means Hermes has never run here, and creating a whole home tree
	// for an agent that may be about to be configured somewhere else is Sidecar
	// inventing a provider's private state.
	if !s.home.Exists {
		return Plan{}, refuse(RefuseProviderMissing, s.paths.Home, "%s", hermesNeverSetUp(s.paths.Home))
	}

	// Which starting states each verb accepts. The point of separate verbs is
	// that the user says what they believe the situation is, and Sidecar
	// disagrees out loud when it is something else.
	switch s.assetStatus {
	case agentlifecycle.StatusNotInstalled:
		if act != ActionInstall {
			return Plan{}, refuse(RefuseNotInstalled, s.paths.Init,
				"nothing is installed at %s; run sidecar agent integration install %s", s.paths.PluginDir, HermesProvider)
		}
	case agentlifecycle.StatusOutdated:
		if act == ActionInstall {
			return Plan{}, refuse(RefuseAlreadyInstalled, s.paths.Init,
				"version %s is already installed at %s; run sidecar agent integration update %s", s.installed, s.paths.PluginDir, HermesProvider)
		}
	case agentlifecycle.StatusNeedsRepair:
		if act != ActionRepair {
			return Plan{}, refuse(RefuseNeedsRepair, s.paths.Init,
				"the installation needs repair (%s); run sidecar agent integration repair %s", s.message, HermesProvider)
		}
	}

	// Safety. Every path this plan would write or remove is proved usable here,
	// before a single operation is emitted, so Apply never has to decide
	// anything.
	for _, check := range []struct {
		state FileState
		path  string
	}{
		{s.home, s.paths.Home},
		{s.pluginsDir, s.paths.PluginsDir},
		{s.dir, s.paths.PluginDir},
		{s.initFile, s.paths.Init},
		{s.manifest, s.paths.Manifest},
		{s.config, s.paths.Config},
		{s.initBackup, s.paths.InitBackup},
		{s.manBackup, s.paths.ManifestBackup},
		{s.confBackup, s.paths.ConfigBackup},
	} {
		if check.state.Exists && check.state.Unsafe != "" {
			return Plan{}, refuse(check.state.Unsafe, check.path, "%s: %s", check.path, check.state.UnsafeDetail)
		}
	}
	for _, foreign := range []struct {
		state FileState
		path  string
	}{{s.initFile, s.paths.Init}, {s.manifest, s.paths.Manifest}} {
		if foreign.state.Exists && !foreign.state.Owned {
			return Plan{}, refuse(RefuseForeignFile, foreign.path,
				"%s exists but does not carry Sidecar's integration marker, so Sidecar will not overwrite it; move or delete it yourself and run this again", foreign.path)
		}
	}
	if s.configErr != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config,
			"%s could not be read as YAML, so Sidecar will not edit it: %v", s.paths.Config, s.configErr)
	}
	if !s.parsed.Editable() {
		return Plan{}, refuse(RefuseForeignFile, s.paths.Config, "%s", s.message)
	}

	// The directories first, then the two files, then the line that turns them
	// on. The order is the safe one: a plugin Hermes has not been told to load
	// does nothing, so an interrupted run leaves an inert directory rather than
	// an enable line pointing at a plugin that is not there.
	for _, dir := range []struct {
		state FileState
		path  string
		note  string
	}{
		{s.pluginsDir, s.paths.PluginsDir, "create the user plugins directory Hermes scans"},
		{s.dir, s.paths.PluginDir, "create the directory holding Sidecar's plugin"},
	} {
		if dir.state.Exists {
			continue
		}
		mode := fs.FileMode(0o755)
		// Inherit the home directory's mode rather than imposing 0755. A user
		// who keeps ~/.hermes at 0700 should not find that installing an
		// integration created a world-readable directory inside it.
		if s.home.Mode != "" {
			if m := parseMode(s.home.Mode); m != 0 {
				mode = m
			}
		}
		p.Ops = append(p.Ops, Op{
			Kind: OpMkdir, Path: dir.path, Mode: renderMode(mode), mode: mode, Note: dir.note,
			Before: dir.state,
			After:  FileState{Path: dir.path, Exists: true, Kind: "dir", Mode: renderMode(mode)},
		})
	}

	for _, file := range []struct {
		asset  Asset
		state  FileState
		backup FileState
		path   string
		bpath  string
		note   string
	}{
		{a.initAsset(), s.initFile, s.initBackup, s.paths.Init, s.paths.InitBackup, "write version " + HermesAssetVersion + " of the Sidecar session plugin"},
		{a.manifestAsset(), s.manifest, s.manBackup, s.paths.Manifest, s.paths.ManifestBackup, "write the plugin manifest Hermes refuses to load a plugin without"},
	} {
		if file.state.Checksum == file.asset.Checksum() {
			continue
		}
		if file.state.Owned {
			p.Ops = append(p.Ops, Op{
				Kind: OpBackup, Path: file.bpath, From: file.path, Mode: "0644", mode: 0o644,
				Bytes: int(file.state.Size), Checksum: file.state.Checksum,
				Note:   "keep a recoverable copy of the file being replaced",
				Before: file.backup,
				After: FileState{
					Path: file.bpath, Exists: true, Kind: "file",
					Checksum: file.state.Checksum, Mode: "0644", Size: file.state.Size,
				},
			})
		}
		content := []byte(file.asset.Content)
		p.Ops = append(p.Ops, Op{
			Kind: OpWrite, Path: file.path, Mode: "0644", mode: 0o644,
			Bytes: len(content), Checksum: file.asset.Checksum(), content: content,
			Note:   file.note,
			Before: file.state,
			After: FileState{
				Path: file.path, Exists: true, Kind: "file", Owned: true, Ownership: OwnsFile,
				Version: HermesAssetVersion, Checksum: file.asset.Checksum(), Mode: "0644", Size: int64(len(content)),
			},
		})
	}

	if op, ok, err := a.configOp(s, true); err != nil {
		return Plan{}, err
	} else if ok {
		p.Ops = append(p.Ops, op...)
	}

	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes exactly what Sidecar put there and nothing else.
func (a HermesAdapter) planUninstall(s hermesState, p Plan) (Plan, error) {
	for _, check := range []struct {
		state FileState
		path  string
	}{
		{s.initFile, s.paths.Init},
		{s.manifest, s.paths.Manifest},
		{s.config, s.paths.Config},
	} {
		if check.state.Exists && check.state.Unsafe != "" {
			return Plan{}, refuse(check.state.Unsafe, check.path, "%s: %s", check.path, check.state.UnsafeDetail)
		}
	}
	for _, foreign := range []struct {
		state FileState
		path  string
	}{{s.initFile, s.paths.Init}, {s.manifest, s.paths.Manifest}} {
		if foreign.state.Exists && !foreign.state.Owned {
			return Plan{}, refuse(RefuseForeignFile, foreign.path,
				"%s does not carry Sidecar's integration marker, so Sidecar will not delete it; there is nothing here that Sidecar installed", foreign.path)
		}
	}
	if s.configErr != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config,
			"%s could not be read as YAML, so Sidecar will not edit it: %v", s.paths.Config, s.configErr)
	}

	// The enable line goes FIRST, which is the mirror of the install order and
	// is the reason it is worth stating: interrupting after it leaves a plugin
	// Hermes no longer loads, where interrupting the other way round would leave
	// an enable line naming a directory that is gone.
	if op, ok, err := a.configOp(s, false); err != nil {
		return Plan{}, err
	} else if ok {
		p.Ops = append(p.Ops, op...)
	}

	var removed []string
	for _, file := range []struct {
		state FileState
		path  string
		note  string
	}{
		{s.initFile, s.paths.Init, "remove the Sidecar session plugin"},
		{s.manifest, s.paths.Manifest, "remove the plugin manifest"},
		{s.initBackup, s.paths.InitBackup, "remove the backup Sidecar kept of a replaced plugin"},
		{s.manBackup, s.paths.ManifestBackup, "remove the backup Sidecar kept of a replaced manifest"},
	} {
		owned := file.state.Owned
		if strings.HasSuffix(file.path, HermesBackupSuffix) {
			// A backup carries no marker of its own -- it is a copy of whatever
			// was replaced -- so it is removed because it sits at a path only
			// Sidecar writes, inside a directory Sidecar owns.
			owned = file.state.Exists && s.initFile.Owned
		}
		if !owned {
			continue
		}
		p.Ops = append(p.Ops, Op{
			Kind: OpRemove, Path: file.path, Note: file.note,
			Before: file.state, After: FileState{Path: file.path},
		})
		removed = append(removed, file.path)
	}

	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}

	// CPython's own byproduct, and the one thing this uninstall removes that
	// Sidecar did not write.
	//
	// Importing the plugin leaves <plugin dir>/__pycache__/__init__.cpython-NN.pyc
	// behind, which is a compiled copy of Sidecar's own __init__.py under a name
	// derived from it. Without this the ownership rule is satisfied and the
	// result is still wrong: every file Sidecar wrote is gone, the directory is
	// not empty, and what survives an uninstall is a stale cache of the plugin
	// that was just removed. Herdr gets this for free by deleting its whole
	// directory; Sidecar removes only the compiled form of its own file, by
	// name, and only when it is removing that file in the same plan.
	//
	// Measured rather than anticipated: the live proof run left exactly this
	// directory behind, and nothing in the test suite could have found it,
	// because no test imports the asset from the directory it is installed into.
	if s.initFile.Owned {
		for _, cached := range hermesCompiledCopies(s.paths.PyCache) {
			p.Ops = append(p.Ops, Op{
				Kind: OpRemove, Path: cached,
				Note:   "remove Python's compiled copy of the plugin being removed",
				Before: FileState{Path: cached, Exists: true, Kind: "file"},
				After:  FileState{Path: cached},
			})
			removed = append(removed, cached)
		}
		if len(removed) > 0 && dirEmptyExcept(s.paths.PyCache, removed) {
			p.Ops = append(p.Ops, Op{
				Kind: OpRmdir, Path: s.paths.PyCache,
				Note:   "remove the bytecode cache directory, which holds nothing else",
				Before: FileState{Path: s.paths.PyCache, Exists: true, Kind: "dir"},
				After:  FileState{Path: s.paths.PyCache},
			})
			removed = append(removed, s.paths.PyCache)
		}
	}

	// The plugin directory goes only if removing Sidecar's own files empties it.
	if len(removed) > 0 && dirEmptyExcept(s.paths.PluginDir, removed) {
		p.Ops = append(p.Ops, Op{
			Kind: OpRmdir, Path: s.paths.PluginDir,
			Note:   "remove the plugin directory, which holds nothing else",
			Before: s.dir, After: FileState{Path: s.paths.PluginDir},
		})
	}

	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

// hermesCompiledCopies lists the bytecode CPython wrote for Sidecar's own
// plugin file, by the name CPython derives from it.
//
// Nothing else in that directory is touched. A `.pyc` for any other module is
// somebody else's, whoever put it there.
func hermesCompiledCopies(cacheDir string) []string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "__init__.cpython-") || !strings.HasSuffix(name, ".pyc") {
			continue
		}
		out = append(out, filepath.Join(cacheDir, name))
	}
	return out
}

// configOp composes the config.yaml edit, verifies it, and returns the ops that
// perform it.
//
// The composed image is checked against its pre-image before any operation is
// emitted, so a rewrite that would not verify produces a refusal with an empty
// op list rather than a partial change on disk.
func (a HermesAdapter) configOp(s hermesState, want bool) ([]Op, bool, error) {
	if s.parsed.Installed() == want {
		return nil, false, nil
	}
	before := ""
	if s.config.Exists {
		content, err := readFileString(s.paths.Config)
		if err != nil {
			return nil, false, refuse(RefuseUnreadable, s.paths.Config, "%s could not be read: %v", s.paths.Config, err)
		}
		before = content
	}

	var after string
	var err error
	if want {
		after, err = s.parsed.WithEntry(HermesAssetVersion)
	} else {
		after, err = s.parsed.WithoutEntry()
	}
	if err != nil {
		return nil, false, refuse(RefuseForeignFile, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	if err := hermesVerify(before, after, want); err != nil {
		return nil, false, refuse(RefuseUnreadable, s.paths.Config,
			"the edit Sidecar composed for %s did not verify, so nothing was written: %v", s.paths.Config, err)
	}

	var ops []Op
	if s.config.Exists {
		ops = append(ops, Op{
			Kind: OpBackup, Path: s.paths.ConfigBackup, From: s.paths.Config, Mode: "0644", mode: 0o644,
			Bytes: len(before), Checksum: checksum([]byte(before)),
			Note:   "keep a recoverable copy of the configuration before editing one line of it",
			Before: s.confBackup,
			After: FileState{
				Path: s.paths.ConfigBackup, Exists: true, Kind: "file",
				Checksum: checksum([]byte(before)), Mode: "0644", Size: int64(len(before)),
			},
		})
	}
	note := "add " + HermesPluginName + " to plugins.enabled, which is what makes Hermes load the plugin"
	if !want {
		note = "remove " + HermesPluginName + " from plugins.enabled and leave every other line where it is"
	}
	afterState := FileState{
		Path: s.paths.Config, Exists: true, Kind: "file",
		Checksum: checksum([]byte(after)), Mode: "0644", Size: int64(len(after)),
	}
	if want {
		afterState.Owned, afterState.Ownership, afterState.Version = true, OwnsEntry, HermesAssetVersion
	}
	return append(ops, Op{
		Kind: OpWrite, Path: s.paths.Config, Mode: "0644", mode: 0o644,
		Bytes: len(after), Checksum: checksum([]byte(after)), content: []byte(after),
		Note:   note,
		Before: s.config,
		After:  afterState,
	}), true, nil
}

var _ Adapter = HermesAdapter{}

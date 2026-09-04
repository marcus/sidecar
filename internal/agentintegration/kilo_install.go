package agentintegration

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Kilo adapter.
//
// Kilo's integration shape is OpenCode's, because Kilo is an OpenCode fork: one
// file dropped into a plugin directory, no configuration file to edit, no
// feature flag, no trust record, and no executable bit. Herdr's own installer is
// four lines -- resolve the directory, refuse if Kilo has never been installed,
// create `plugin/`, write the file. Everything else here is Sidecar's ownership
// contract, which is stricter than Herdr's in ways worth naming: Herdr's
// uninstall_kilo deletes its file without checking its own marker, and Sidecar's
// never removes anything it cannot prove it wrote.
//
// Three facts about where Kilo reads plugins, all measured against kilo 7.5.9
// rather than read from prose:
//
//   - The global config directory is `$XDG_CONFIG_HOME/kilo`, falling back to
//     `~/.config/kilo`. Running the CLI under an isolated XDG tree creates
//     exactly that path, so this is observed rather than assumed. Herdr hardcodes
//     `~/.config/kilo` and therefore misses a relocated config home.
//   - `KILO_CONFIG_DIR` relocates it. The shipped binary's own reference
//     documents it as "path to an additional config directory", and its
//     Global.Path derivation reads `KILO_CONFIG_DIR ?? config`, so a directory
//     named there is searched under either reading. Herdr honours no override at
//     all.
//   - Kilo globs `{plugin,plugins}/*.{ts,js}` in EVERY config directory it
//     discovers, so the OpenCode double-load trap applies here too: a copy in
//     each directory fires every event twice. Measured with two probe plugins,
//     one per directory, both of which loaded and ran. Sidecar owns `plugin/`
//     and treats anything with its asset's name in `plugins/` as damage.
//
// Kilo also loads `~/.kilo/`, `~/.kilocode/`, and project-local `.kilo/` and
// `.kilocode/` directories walking up from the working directory. Sidecar
// deliberately installs into none of them. A per-project copy would follow a
// checkout into other people's clones and would report from panes Sidecar never
// set up, and the two home-level legacy directories are a compatibility path for
// the Kilo Code VS Code extension rather than where a fresh install puts things.

// KiloAssetSchema is the marker schema version the bundled asset declares. It
// changes only when the marker line's own format changes, which is why it is
// separate from the asset version.
const KiloAssetSchema = 1

// KiloBackupSuffix is appended to the asset's name for the recoverable copy kept
// when an installed asset is replaced.
const KiloBackupSuffix = ".sidecar-backup"

// KiloOwnedDir is the single plugin directory Sidecar installs into, and
// KiloConflictDir is the other one Kilo also loads.
const (
	KiloOwnedDir    = "plugin"
	KiloConflictDir = "plugins"
)

// KiloAdapter installs Sidecar's Kilo lifecycle plugin.
type KiloAdapter struct{}

func (KiloAdapter) Provider() string { return KiloProvider }
func (KiloAdapter) Source() string   { return KiloSource }

// Assets returns the bundled plugin with the identity a marker check compares
// against.
func (KiloAdapter) Assets() []Asset {
	return []Asset{{
		Name:          KiloAssetName,
		Source:        KiloSource,
		SchemaVersion: KiloAssetSchema,
		Version:       KiloAssetVersion,
		Ownership:     OwnsFile,
		Content:       kiloAsset,
	}}
}

// asset is the single file this integration drops. Kilo loads whole plugin files
// from a directory it globs, so there is exactly one and it is Sidecar's.
func (a KiloAdapter) asset() Asset { return a.Assets()[0] }

// kiloPaths are the exact user-level paths this adapter inspects.
type kiloPaths struct {
	ConfigDir   string
	OwnedDir    string
	Owned       string
	ConflictDir string
	Conflict    string
	Backup      string
}

// KiloPaths returns the paths the Kilo adapter would inspect and touch.
//
// It is exported because "show the exact paths before mutating" is a rule, and a
// surface that wants to name them before asking for confirmation should not have
// to reconstruct them.
func KiloPaths(env Env) []string {
	p := kiloPathsFor(env)
	return []string{p.Owned, p.Conflict}
}

func kiloPathsFor(env Env) kiloPaths {
	dir := kiloConfigDir(env)
	owned := filepath.Join(dir, KiloOwnedDir)
	conflict := filepath.Join(dir, KiloConflictDir)
	return kiloPaths{
		ConfigDir:   dir,
		OwnedDir:    owned,
		Owned:       filepath.Join(owned, KiloAssetName),
		ConflictDir: conflict,
		Conflict:    filepath.Join(conflict, KiloAssetName),
		Backup:      filepath.Join(owned, KiloAssetName+KiloBackupSuffix),
	}
}

// kiloConfigDir resolves Kilo's global config directory the way Kilo does.
//
// KILO_CONFIG_DIR wins when it is set, then $XDG_CONFIG_HOME/kilo, then
// ~/.config/kilo. The trim is Sidecar's own and is deliberate, for the reason
// agentsession.PiAgentDir records: a variable somebody exported without a value
// is not a directory named " ".
func kiloConfigDir(env Env) string {
	if value := strings.TrimSpace(env.KiloConfigDir); value != "" {
		return value
	}
	config := env.ConfigHome
	if config == "" {
		config = filepath.Join(env.Home, ".config")
	}
	return filepath.Join(config, KiloProvider)
}

// kiloNeverSetUp is the one sentence for "kilo's config directory is not there".
//
// It is a function because the same fact has to reach a user through two
// different surfaces -- the refusal a caller gets from Plan, and the message on a
// status that offers no install -- and a status that stayed silent while the
// refusal explained itself is how a missing action looks like a bug.
func kiloNeverSetUp(configDir string) string {
	return "kilo's config directory " + configDir + " does not exist, so kilo has not been set up on this machine; " +
		"run kilo once (or set KILO_CONFIG_DIR) and try again"
}

// kiloState is everything one inspection learned. Both [KiloAdapter.Inspect] and
// [KiloAdapter.Plan] are built from it, so a plan can never be based on a
// different reading of the disk than the status the user was shown.
type kiloState struct {
	env    Env
	paths  kiloPaths
	asset  Asset
	config FileState
	dir    FileState
	owned  FileState
	// conflict is the copy in the directory Sidecar does not own.
	conflict FileState
	backup   FileState

	providerPath    string
	providerVersion string

	// assetStatus is what the files alone say. status is that overlaid with
	// provider availability, which is what the user is shown.
	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a KiloAdapter) inspect(env Env) kiloState {
	asset := a.asset()
	p := kiloPathsFor(env)
	s := kiloState{
		env:      env,
		paths:    p,
		asset:    asset,
		config:   inspectDir(env, p.ConfigDir),
		dir:      inspectDir(env, p.OwnedDir),
		owned:    inspectFile(env, p.Owned, asset),
		conflict: inspectFile(env, p.Conflict, asset),
		backup:   inspectFile(env, p.Backup, asset),
	}
	if path, ok := env.lookPath(KiloProvider); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(KiloProvider)
	}
	s.assetStatus, s.message = kiloAssetStatus(s)
	switch {
	case s.owned.Owned:
		s.installed = s.owned.Version
	case s.conflict.Owned:
		s.installed = s.conflict.Version
	}

	s.status = s.assetStatus
	if s.providerPath == "" {
		// The provider CLI being absent is the more actionable of the two true
		// statements, and it is also the one that decides authority: with no kilo
		// there is nothing to load the plugin, so TierFor is right to return
		// screen fallback. The asset's own state is still reported in the message
		// and in Files, so an uninstall after removing the provider is still
		// discoverable.
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the kilo CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// kiloAssetStatus decides the status from the inspected files alone.
//
// Nothing here trusts a version a report claimed. The installed bytes are hashed
// and compared with the bundled asset's hash, so a truncated, hand-edited, or
// half-written asset reads as needs-repair rather than as current.
func kiloAssetStatus(s kiloState) (agentlifecycle.IntegrationStatus, string) {
	switch {
	case s.dir.Exists && s.dir.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.dir.UnsafeDetail + " (" + s.paths.OwnedDir + ")"
	case s.owned.Exists && s.owned.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.owned.UnsafeDetail + " (" + s.paths.Owned + ")"
	case s.conflict.Exists && s.conflict.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.conflict.UnsafeDetail + " (" + s.paths.Conflict + ")"
	case s.owned.Exists && !s.owned.Owned:
		// Herdr's own Kilo plugin lives in this same directory on a machine that
		// has both installed, under a different name. It is not Sidecar's and
		// Sidecar never touches it; only a foreign file at Sidecar's own asset
		// path is a problem, and even then the answer is to say so rather than to
		// adopt it.
		return agentlifecycle.StatusNeedsRepair, "a file that is not Sidecar's occupies " + s.paths.Owned + "; Sidecar will not modify or remove it"
	case s.conflict.Exists:
		// Anything with the asset's name in the directory Sidecar does not own is
		// damage whether or not Sidecar wrote it: Kilo globs both directories, so
		// this file fires every event a second time. All four combinations are
		// named rather than collapsed, because which of the two files Sidecar owns
		// decides both the sentence and the verb that helps.
		switch {
		case s.owned.Owned && s.conflict.Owned:
			return agentlifecycle.StatusNeedsRepair, "the asset is installed in both " + KiloOwnedDir + "/ and " + KiloConflictDir + "/; Kilo loads both, so every event is reported twice"
		case s.conflict.Owned:
			return agentlifecycle.StatusNeedsRepair, "the asset is installed in " + KiloConflictDir + "/, which Sidecar does not own; move it to " + KiloOwnedDir + "/"
		case s.owned.Owned:
			return agentlifecycle.StatusNeedsRepair, "a file that is not Sidecar's occupies " + s.paths.Conflict +
				"; Kilo loads it alongside Sidecar's own asset in " + KiloOwnedDir + "/, so every event is reported twice. Sidecar will not modify or remove a file it does not own, so remove it yourself"
		}
		return agentlifecycle.StatusNotInstalled, "a file that is not Sidecar's occupies " + s.paths.Conflict + "; Sidecar will not modify or remove it"
	case !s.owned.Exists:
		if !s.config.Exists {
			// The status has to carry this, not only the refusal. Without it a
			// machine where kilo is on PATH but has never been run reads as a plain
			// not-installed with an empty message and no install offered, and
			// nothing on the status surface says why the one action that would fix
			// it is missing. Offered is computed by asking the planner, so the
			// absence is real; this is the sentence that explains it, and it is
			// deliberately the same sentence planConverge refuses with.
			return agentlifecycle.StatusNotInstalled, kiloNeverSetUp(s.paths.ConfigDir)
		}
		return agentlifecycle.StatusNotInstalled, ""
	case s.owned.Checksum == s.asset.Checksum():
		return agentlifecycle.StatusCurrent, ""
	case s.owned.Version != s.asset.Version:
		return agentlifecycle.StatusOutdated, "version " + s.owned.Version + " is installed; this build ships version " + s.asset.Version
	}
	return agentlifecycle.StatusNeedsRepair, "the installed asset claims version " + s.owned.Version + " but its contents do not match the bundled asset"
}

// Inspect implements [Adapter].
func (a KiloAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a KiloAdapter) statusOf(s kiloState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(KiloSource)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              KiloProvider,
		Source:                KiloSource,
		Status:                s.status,
		BundledVersion:        s.asset.Version,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.Owned, s.paths.Conflict},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.owned, s.conflict, s.backup}

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
func (a KiloAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a KiloAdapter) plan(s kiloState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      KiloProvider,
		Source:        KiloSource,
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

// planConverge builds the plan that ends with exactly one Sidecar-owned asset,
// at the bundled version, in the directory Sidecar owns.
//
// install, update, and repair share it because the target state is identical;
// they differ only in which starting states they accept.
func (a KiloAdapter) planConverge(s kiloState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the kilo CLI was not found on PATH, so Sidecar will not create %s for it; install kilo first", s.paths.OwnedDir)
	}
	// Herdr's install_kilo refuses when ~/.config/kilo is not a directory, with
	// "install kilo first". The semantics are worth keeping and the reason is not
	// fussiness: Kilo creates its config directory on first run, so its absence
	// means Kilo has never run here, and creating a whole config tree for an agent
	// that may be about to be configured somewhere else is Sidecar inventing a
	// provider's private state. Expressed as Sidecar's own refusal code rather
	// than as an io error.
	if !s.config.Exists {
		return Plan{}, refuse(RefuseProviderMissing, s.paths.ConfigDir, "%s", kiloNeverSetUp(s.paths.ConfigDir))
	}

	// Which starting states each verb accepts. The point of separate verbs is
	// that the user says what they believe the situation is, and Sidecar disagrees
	// out loud when it is something else.
	switch s.assetStatus {
	case agentlifecycle.StatusNotInstalled:
		if act != ActionInstall {
			return Plan{}, refuse(RefuseNotInstalled, s.paths.Owned,
				"nothing is installed at %s; run sidecar agent integration install %s", s.paths.Owned, KiloProvider)
		}
	case agentlifecycle.StatusOutdated:
		if act == ActionInstall {
			return Plan{}, refuse(RefuseAlreadyInstalled, s.paths.Owned,
				"version %s is already installed at %s; run sidecar agent integration update %s", s.installed, s.paths.Owned, KiloProvider)
		}
	case agentlifecycle.StatusNeedsRepair:
		if act != ActionRepair {
			return Plan{}, refuse(RefuseNeedsRepair, s.paths.Owned,
				"the installation needs repair (%s); run sidecar agent integration repair %s", s.message, KiloProvider)
		}
	}

	// Safety. Every path this plan would write or remove is proved usable here,
	// before a single operation is emitted, so Apply never has to decide anything.
	if s.dir.Exists && s.dir.Unsafe != "" {
		return Plan{}, refuse(s.dir.Unsafe, s.paths.OwnedDir, "%s: %s", s.paths.OwnedDir, s.dir.UnsafeDetail)
	}
	if s.config.Exists && s.config.Unsafe != "" {
		return Plan{}, refuse(s.config.Unsafe, s.paths.ConfigDir, "%s: %s", s.paths.ConfigDir, s.config.UnsafeDetail)
	}
	if s.owned.Exists && s.owned.Unsafe != "" {
		return Plan{}, refuse(s.owned.Unsafe, s.paths.Owned, "%s: %s", s.paths.Owned, s.owned.UnsafeDetail)
	}
	if s.owned.Exists && !s.owned.Owned {
		return Plan{}, refuse(RefuseForeignFile, s.paths.Owned,
			"%s exists but does not carry Sidecar's integration marker, so Sidecar will not overwrite it; move or delete it yourself and run this again", s.paths.Owned)
	}
	if s.conflict.Exists && !s.conflict.Owned {
		return Plan{}, refuse(RefuseForeignFile, s.paths.Conflict,
			"%s exists but does not carry Sidecar's integration marker; Kilo loads both %s/ and %s/, so it would report every event a second time. Sidecar will not remove a file it does not own, so move or delete it yourself",
			s.paths.Conflict, KiloOwnedDir, KiloConflictDir)
	}
	if s.backup.Exists && s.backup.Unsafe != "" {
		return Plan{}, refuse(s.backup.Unsafe, s.paths.Backup, "%s: %s", s.paths.Backup, s.backup.UnsafeDetail)
	}

	// The conflicting copy goes first. If the run is interrupted after this
	// operation the pane is momentarily uninstrumented, which is a state Sidecar
	// handles by falling back to screen detection; interrupting after a write
	// instead would leave the double-firing installation the user asked to be rid
	// of.
	if s.conflict.Exists && s.conflict.Owned {
		p.Ops = append(p.Ops, Op{
			Kind:   OpRemove,
			Path:   s.paths.Conflict,
			Note:   "remove the duplicate copy: Kilo loads both " + KiloOwnedDir + "/ and " + KiloConflictDir + "/, and a copy in each reports every event twice",
			Before: s.conflict,
			After:  FileState{Path: s.paths.Conflict},
		})
	}

	if !s.dir.Exists {
		mode := fs.FileMode(0o755)
		if s.config.Exists && s.config.Mode != "" {
			// Inherit the config directory's mode rather than imposing 0755. A user
			// who keeps ~/.config/kilo at 0700 should not find that installing an
			// integration created a world-readable directory inside it.
			if m := parseMode(s.config.Mode); m != 0 {
				mode = m
			}
		}
		p.Ops = append(p.Ops, Op{
			Kind:   OpMkdir,
			Path:   s.paths.OwnedDir,
			Mode:   renderMode(mode),
			mode:   mode,
			Note:   "create the plugin directory Kilo loads",
			Before: s.dir,
			After:  FileState{Path: s.paths.OwnedDir, Exists: true, Kind: "dir", Mode: renderMode(mode)},
		})
	}

	if s.owned.Owned && s.owned.Checksum != s.asset.Checksum() {
		p.Ops = append(p.Ops, Op{
			Kind:     OpBackup,
			Path:     s.paths.Backup,
			From:     s.paths.Owned,
			Mode:     "0644",
			mode:     0o644,
			Bytes:    int(s.owned.Size),
			Checksum: s.owned.Checksum,
			Note:     "keep a recoverable copy of the asset being replaced",
			Before:   s.backup,
			After: FileState{
				Path: s.paths.Backup, Exists: true, Kind: "file", Owned: true,
				Version: s.owned.Version, Checksum: s.owned.Checksum, Mode: "0644", Size: s.owned.Size,
			},
		})
	}

	if s.owned.Checksum != s.asset.Checksum() {
		content := []byte(s.asset.Content)
		p.Ops = append(p.Ops, Op{
			Kind:     OpWrite,
			Path:     s.paths.Owned,
			Mode:     "0644",
			mode:     0o644,
			Bytes:    len(content),
			Checksum: s.asset.Checksum(),
			content:  content,
			Note:     "write version " + s.asset.Version + " of the Sidecar lifecycle plugin",
			Before:   s.owned,
			After: FileState{
				Path: s.paths.Owned, Exists: true, Kind: "file", Owned: true,
				Version: s.asset.Version, Checksum: s.asset.Checksum(), Mode: "0644", Size: int64(len(content)),
			},
		})
	}

	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes exactly what Sidecar put there and nothing else.
//
// Herdr's uninstall_kilo deletes its file without checking that the file is still
// its own. Sidecar does not copy that: ownership is proved from the file's own
// bytes, and a file that has stopped carrying Sidecar's marker is somebody else's
// now.
func (a KiloAdapter) planUninstall(s kiloState, p Plan) (Plan, error) {
	if s.owned.Exists && s.owned.Unsafe != "" {
		return Plan{}, refuse(s.owned.Unsafe, s.paths.Owned, "%s: %s", s.paths.Owned, s.owned.UnsafeDetail)
	}
	if s.owned.Exists && !s.owned.Owned {
		return Plan{}, refuse(RefuseForeignFile, s.paths.Owned,
			"%s does not carry Sidecar's integration marker, so Sidecar will not delete it; there is nothing here that Sidecar installed", s.paths.Owned)
	}

	var removed []string
	if s.owned.Owned {
		p.Ops = append(p.Ops, Op{
			Kind: OpRemove, Path: s.paths.Owned,
			Note:   "remove the Sidecar lifecycle plugin",
			Before: s.owned, After: FileState{Path: s.paths.Owned},
		})
		removed = append(removed, s.paths.Owned)
	}
	if s.conflict.Owned {
		p.Ops = append(p.Ops, Op{
			Kind: OpRemove, Path: s.paths.Conflict,
			Note:   "remove the duplicate copy Sidecar owns in " + KiloConflictDir + "/",
			Before: s.conflict, After: FileState{Path: s.paths.Conflict},
		})
		removed = append(removed, s.paths.Conflict)
	}
	if s.backup.Owned {
		p.Ops = append(p.Ops, Op{
			Kind: OpRemove, Path: s.paths.Backup,
			Note:   "remove the backup Sidecar kept of a replaced asset",
			Before: s.backup, After: FileState{Path: s.paths.Backup},
		})
		removed = append(removed, s.paths.Backup)
	}

	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}

	// The plugin directory goes only if removing Sidecar's own files empties it.
	// On a machine that also has Herdr installed this directory holds Herdr's own
	// plugin, which is exactly the case this rule exists for.
	if dirEmptyExcept(s.paths.OwnedDir, removed) {
		p.Ops = append(p.Ops, Op{
			Kind: OpRmdir, Path: s.paths.OwnedDir,
			Note:   "remove the plugin directory, which holds nothing else",
			Before: s.dir, After: FileState{Path: s.paths.OwnedDir},
		})
	}

	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

var _ Adapter = KiloAdapter{}

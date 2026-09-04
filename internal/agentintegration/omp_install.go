package agentintegration

import (
	"io/fs"
	"path/filepath"

	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/agentsession"
)

// The OMP adapter.
//
// OMP's integration shape is Pi's: one file dropped into an extension directory,
// no configuration file to edit, no feature flag, no trust record, and no
// executable bit. What is not Pi's is where that directory is, and the one
// refusal this adapter has that no other adapter needs.
//
// WHERE OMP READS EXTENSIONS, read from OMP 18.1.8's own shipped TypeScript
// rather than from Herdr's installer, because the two disagree:
//
//   - The directory is <agent dir>/extensions, where the agent dir is
//     agentsession.OmpAgentDir's derivation: $HOME/${PI_CONFIG_DIR:-.omp}/agent,
//     with a named profile inserting /profiles/<name>, and PI_CODING_AGENT_DIR
//     overriding the whole thing when no profile is active
//     (@oh-my-pi/pi-utils/src/dirs.ts; the loader's own use of it is
//     getConfigDirs in src/discovery/builtin.ts, which reads
//     <getAgentDir()>/extensions).
//   - OMP also loads a project-local <cwd>/.omp/extensions. Sidecar deliberately
//     does not install there. A per-project copy would follow a checkout into
//     other people's clones and would report from panes Sidecar never set up,
//     and the user-level directory is the one a managed shell always sees.
//   - The loader takes any bare .ts or .js file in that directory
//     (isExtensionFile, src/extensibility/extensions/loader.ts:511-513), so the
//     extension is free and .js is chosen for the reason the asset's header
//     gives.
//
// THE COLLISION. PI_CODING_AGENT_DIR is Pi's variable and OMP reads it too,
// because OMP is a rebranded fork of Pi's codebase. With it set, `pi` and `omp`
// resolve to the same agent directory and therefore to the same extensions
// directory, and every extension in it is loaded by both binaries. Herdr refuses
// to install OMP in that state (install_omp in src/integration/targets.rs) and
// Sidecar refuses too, for a reason worth stating rather than inheriting: two
// Sidecar assets in one directory means both report on every pane, one of them
// naming a provider that is not the pane's occupant, and `agent report` verifies
// --provider against the occupant, so one of the two is refused on every single
// event. Misattributing a pane is the risk the parity plan names first, and a
// refusal with a reason is the honest answer to it.
//
// Two things this deliberately does NOT copy from Herdr. Its
// remove_legacy_pi_extension_from_omp_dir deletes a file named for its own PI
// asset out of the OMP directory when that file declares HERDR_INTEGRATION_ID=pi;
// that is Herdr cleaning up a mistake of its own making, Sidecar has never
// installed a Pi asset into an OMP directory, and deleting a file on the strength
// of a marker that is not Sidecar's own would break the ownership rule outright.
// And Herdr's uninstall_omp deletes its file without checking that the file is
// still its own, which Sidecar's ownership rule does not permit either.
//
// The residual, stated because it is real: the refusal is one-sided. Installing
// OMP into a directory Pi already shares is refused; setting PI_CODING_AGENT_DIR
// *after* installing OMP and then installing Pi is not, because that check would
// live in the Pi adapter. `sidecar agent integration status omp` still reports
// the collision from the shared directory in that state, so it is visible rather
// than silent, and closing it is one refusal in pi_install.go whenever someone
// wants it.

// OmpAssetSchema is the marker schema version the bundled asset declares. It
// changes only when the marker line's own format changes, which is why it is
// separate from the asset version.
const OmpAssetSchema = 1

// OmpBackupSuffix is appended to the asset's name for the recoverable copy kept
// when an installed asset is replaced.
const OmpBackupSuffix = ".sidecar-backup"

// OmpExtensionsDir is the directory name OMP scans inside its agent directory.
const OmpExtensionsDir = "extensions"

// OmpAdapter installs Sidecar's OMP lifecycle extension.
type OmpAdapter struct{}

func (OmpAdapter) Provider() string { return OmpProvider }
func (OmpAdapter) Source() string   { return OmpSource }

// Assets returns the bundled extension with the identity a marker check compares
// against.
func (OmpAdapter) Assets() []Asset {
	return []Asset{{
		Name:          OmpAssetName,
		Source:        OmpSource,
		SchemaVersion: OmpAssetSchema,
		Version:       OmpAssetVersion,
		Ownership:     OwnsFile,
		Content:       ompAsset,
	}}
}

// asset is the single file this integration drops.
func (a OmpAdapter) asset() Asset { return a.Assets()[0] }

// ompPaths are the exact user-level paths this adapter inspects.
type ompPaths struct {
	AgentDir string
	OwnedDir string
	Owned    string
	Backup   string
	// PiOwnedDir is Pi's extensions directory. It is carried so the collision
	// refusal can name both sides, and it is empty when Pi's own directory
	// cannot be derived.
	PiOwnedDir string
}

// OmpPaths returns the paths the OMP adapter would inspect and touch.
//
// It is exported because "show the exact paths before mutating" is a rule, and a
// surface that wants to name them before asking for confirmation should not have
// to reconstruct them.
func OmpPaths(env Env) []string {
	// Nothing, rather than one empty string, when the agent directory cannot be
	// resolved. An empty path in a list a surface is about to print reads as a
	// path Sidecar would touch, and there is none: the refusal in Plan says why
	// instead. See ompUnresolvableAgentDir.
	owned := ompPathsFor(env).Owned
	if owned == "" {
		return nil
	}
	return []string{owned}
}

func ompPathsFor(env Env) ompPaths {
	agent := ompAgentDir(env)
	if agent == "" {
		return ompPaths{PiOwnedDir: piExtensionsDir(env)}
	}
	owned := filepath.Join(agent, OmpExtensionsDir)
	return ompPaths{
		AgentDir:   agent,
		OwnedDir:   owned,
		Owned:      filepath.Join(owned, OmpAssetName),
		Backup:     filepath.Join(owned, OmpAssetName+OmpBackupSuffix),
		PiOwnedDir: piExtensionsDir(env),
	}
}

// piExtensionsDir is where Pi loads its extensions from, for the collision check
// and for nothing else. It goes through the Pi adapter's own derivation rather
// than repeating it, because the whole point of the check is that the two
// answers are compared.
func piExtensionsDir(env Env) string {
	agent := piAgentDir(env)
	if agent == "" {
		return ""
	}
	return filepath.Join(agent, PiExtensionsDir)
}

// ompAgentDir resolves OMP's agent directory the way OMP itself does.
//
// The derivation is agentsession's, and calling it rather than repeating it is
// the point: the same directory decides both where this installer writes and
// which store root a session binding from that extension may name, and when Pi
// derived those two separately they drifted in exactly the way that installs
// cleanly and then refuses every binding.
func ompAgentDir(env Env) string {
	profile := agentsession.OmpProfile(env.OmpProfile, env.OmpProfileSet, env.PiProfile)
	return agentsession.OmpAgentDir(env.Home, env.OmpConfigDir, env.PiAgentDir, profile)
}

// ompUnresolvableAgentDir is the one sentence for "PI_CODING_AGENT_DIR is set to
// something Sidecar cannot resolve".
//
// OMP calls path.resolve on that value, which binds a relative path — a leading
// "~" included, since a tilde is not special to path.resolve — to whatever
// directory OMP happened to be launched from. Sidecar cannot know that
// directory, and installing into a guess is worse than refusing.
const ompUnresolvableAgentDir = "PI_CODING_AGENT_DIR is set to a path that is not absolute, and OMP resolves it against " +
	"the directory it is launched from, which Sidecar cannot know; set PI_CODING_AGENT_DIR to an absolute path " +
	"(OMP does not expand a leading ~) and try again"

// ompNeverSetUp is the one sentence for "omp's agent directory is not there".
//
// It is a function because the same fact has to reach a user through two
// surfaces — the refusal a caller gets from Plan, and the message on a status
// that offers no install — and a status that stayed silent while the refusal
// explained itself is how a missing action looks like a bug.
func ompNeverSetUp(agentDir string) string {
	return "omp's agent directory " + agentDir + " does not exist, so omp has not been set up on this machine; " +
		"run omp once (or set PI_CONFIG_DIR / PI_CODING_AGENT_DIR) and try again"
}

// ompSharedWithPi is the collision refusal's sentence.
func ompSharedWithPi(dir string) string {
	return "pi and omp both read " + dir + ", because PI_CODING_AGENT_DIR is Pi's variable and omp reads it too; " +
		"every extension in that directory is loaded by both agents, so Sidecar would be reporting one provider's " +
		"lane from the other's pane. Give omp its own directory (PI_CONFIG_DIR, or an omp profile) before installing"
}

// ompState is everything one inspection learned. Both [OmpAdapter.Inspect] and
// [OmpAdapter.Plan] are built from it, so a plan can never be based on a
// different reading of the disk than the status the user was shown.
type ompState struct {
	env    Env
	paths  ompPaths
	asset  Asset
	agent  FileState
	dir    FileState
	owned  FileState
	backup FileState

	providerPath    string
	providerVersion string

	// unresolvable is set when PI_CODING_AGENT_DIR names a path OMP would resolve
	// against a cwd Sidecar cannot know. shared is set when omp's extensions
	// directory is also pi's.
	unresolvable bool
	shared       bool

	// assetStatus is what the files alone say. status is that overlaid with
	// provider availability, which is what the user is shown.
	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a OmpAdapter) inspect(env Env) ompState {
	asset := a.asset()
	p := ompPathsFor(env)
	s := ompState{
		env:          env,
		paths:        p,
		asset:        asset,
		unresolvable: p.AgentDir == "",
		shared:       p.OwnedDir != "" && p.OwnedDir == p.PiOwnedDir,
	}
	if !s.unresolvable {
		s.agent = inspectDir(env, p.AgentDir)
		s.dir = inspectDir(env, p.OwnedDir)
		s.owned = inspectFile(env, p.Owned, asset)
		s.backup = inspectFile(env, p.Backup, asset)
	}
	if path, ok := env.lookPath(OmpProvider); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(OmpProvider)
	}
	s.assetStatus, s.message = ompAssetStatus(s)
	if s.owned.Owned {
		s.installed = s.owned.Version
	}

	s.status = s.assetStatus
	if s.providerPath == "" {
		// The provider CLI being absent is the more actionable of the two true
		// statements, and it is also the one that decides authority: with no omp
		// there is nothing to load the extension, so TierFor is right to return
		// screen fallback. The asset's own state is still reported in the message
		// and in Files, so an uninstall after removing the provider is still
		// discoverable.
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the omp CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// ompAssetStatus decides the status from the inspected files alone.
//
// Nothing here trusts a version a report claimed. The installed bytes are hashed
// and compared with the bundled asset's hash, so a truncated, hand-edited, or
// half-written asset reads as needs-repair rather than as current.
func ompAssetStatus(s ompState) (agentlifecycle.IntegrationStatus, string) {
	switch {
	case s.unresolvable:
		return agentlifecycle.StatusNotInstalled, ompUnresolvableAgentDir
	case s.dir.Exists && s.dir.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.dir.UnsafeDetail + " (" + s.paths.OwnedDir + ")"
	case s.owned.Exists && s.owned.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.owned.UnsafeDetail + " (" + s.paths.Owned + ")"
	case s.owned.Exists && !s.owned.Owned:
		// Herdr's own OMP extension lives in this directory on a machine that has
		// both installed, under a different name, and so does Sidecar's Pi asset
		// when the two directories have been collapsed. Neither is Sidecar's OMP
		// asset and Sidecar never touches either; only a foreign file at Sidecar's
		// own asset path is a problem, and even then the answer is to say so
		// rather than to adopt it.
		return agentlifecycle.StatusNeedsRepair, "a file that is not Sidecar's occupies " + s.paths.Owned + "; Sidecar will not modify or remove it"
	case !s.owned.Exists:
		// The collision is reported on the status and not only in the refusal,
		// because otherwise a machine where omp is installed and shares Pi's
		// directory reads as a plain not-installed with no install offered and
		// nothing anywhere saying why.
		if s.shared {
			return agentlifecycle.StatusNotInstalled, ompSharedWithPi(s.paths.OwnedDir)
		}
		if !s.agent.Exists {
			return agentlifecycle.StatusNotInstalled, ompNeverSetUp(s.paths.AgentDir)
		}
		return agentlifecycle.StatusNotInstalled, ""
	case s.owned.Checksum == s.asset.Checksum():
		if s.shared {
			// Installed and colliding. The asset is current and works; what is
			// wrong is the directory, and saying so is the whole value of the
			// message field here.
			return agentlifecycle.StatusCurrent, ompSharedWithPi(s.paths.OwnedDir)
		}
		return agentlifecycle.StatusCurrent, ""
	case s.owned.Version != s.asset.Version:
		return agentlifecycle.StatusOutdated, "version " + s.owned.Version + " is installed; this build ships version " + s.asset.Version
	}
	return agentlifecycle.StatusNeedsRepair, "the installed asset claims version " + s.owned.Version + " but its contents do not match the bundled asset"
}

// Inspect implements [Adapter].
func (a OmpAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a OmpAdapter) statusOf(s ompState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(OmpSource)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              OmpProvider,
		Source:                OmpSource,
		Status:                s.status,
		BundledVersion:        s.asset.Version,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	if !s.unresolvable {
		st.TargetPaths = []string{s.paths.Owned}
		st.Files = []FileState{s.dir, s.owned, s.backup}
	}
	st.ProviderPath = s.providerPath

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
func (a OmpAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a OmpAdapter) plan(s ompState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      OmpProvider,
		Source:        OmpSource,
		Action:        act,
		StatusBefore:  s.status,
		StatusAfter:   s.status,
	}
	// Nothing can be planned against a directory that cannot be named, in either
	// direction: an uninstall has no path to remove either.
	if s.unresolvable {
		return Plan{}, refuse(RefuseProviderMissing, "", "%s", ompUnresolvableAgentDir)
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
// at the bundled version, in the directory OMP loads.
//
// install, update, and repair share it because the target state is identical;
// they differ only in which starting states they accept.
func (a OmpAdapter) planConverge(s ompState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the omp CLI was not found on PATH, so Sidecar will not create %s for it; install omp first", s.paths.OwnedDir)
	}
	// The collision refusal, before anything else that could write. It is checked
	// here rather than in inspect's status alone because a status is advice and
	// this is a decision: nothing is written into a directory two agents read.
	if s.shared {
		return Plan{}, refuse(RefuseUnsafePath, s.paths.OwnedDir, "%s", ompSharedWithPi(s.paths.OwnedDir))
	}
	// OMP's agent directory is created by OMP, so its absence means OMP has never
	// run here, and creating a whole ~/.omp/agent tree for an agent that may be
	// about to be configured somewhere else is Sidecar inventing a provider's
	// private state. The extensions directory inside it is Sidecar's to create;
	// its parent is not. Herdr's ensure_extension_dir draws the same line.
	if !s.agent.Exists {
		return Plan{}, refuse(RefuseProviderMissing, s.paths.AgentDir, "%s", ompNeverSetUp(s.paths.AgentDir))
	}

	// Which starting states each verb accepts. The point of separate verbs is
	// that the user says what they believe the situation is, and Sidecar
	// disagrees out loud when it is something else.
	switch s.assetStatus {
	case agentlifecycle.StatusNotInstalled:
		if act != ActionInstall {
			return Plan{}, refuse(RefuseNotInstalled, s.paths.Owned,
				"nothing is installed at %s; run sidecar agent integration install %s", s.paths.Owned, OmpProvider)
		}
	case agentlifecycle.StatusOutdated:
		if act == ActionInstall {
			return Plan{}, refuse(RefuseAlreadyInstalled, s.paths.Owned,
				"version %s is already installed at %s; run sidecar agent integration update %s", s.installed, s.paths.Owned, OmpProvider)
		}
	case agentlifecycle.StatusNeedsRepair:
		if act != ActionRepair {
			return Plan{}, refuse(RefuseNeedsRepair, s.paths.Owned,
				"the installation needs repair (%s); run sidecar agent integration repair %s", s.message, OmpProvider)
		}
	}

	// Safety. Every path this plan would write or remove is proved usable here,
	// before a single operation is emitted, so Apply never has to decide
	// anything.
	if s.dir.Exists && s.dir.Unsafe != "" {
		return Plan{}, refuse(s.dir.Unsafe, s.paths.OwnedDir, "%s: %s", s.paths.OwnedDir, s.dir.UnsafeDetail)
	}
	if s.agent.Exists && s.agent.Unsafe != "" {
		return Plan{}, refuse(s.agent.Unsafe, s.paths.AgentDir, "%s: %s", s.paths.AgentDir, s.agent.UnsafeDetail)
	}
	if s.owned.Exists && s.owned.Unsafe != "" {
		return Plan{}, refuse(s.owned.Unsafe, s.paths.Owned, "%s: %s", s.paths.Owned, s.owned.UnsafeDetail)
	}
	if s.owned.Exists && !s.owned.Owned {
		return Plan{}, refuse(RefuseForeignFile, s.paths.Owned,
			"%s exists but does not carry Sidecar's integration marker, so Sidecar will not overwrite it; move or delete it yourself and run this again", s.paths.Owned)
	}
	if s.backup.Exists && s.backup.Unsafe != "" {
		return Plan{}, refuse(s.backup.Unsafe, s.paths.Backup, "%s: %s", s.paths.Backup, s.backup.UnsafeDetail)
	}

	if !s.dir.Exists {
		mode := fs.FileMode(0o755)
		if s.agent.Exists && s.agent.Mode != "" {
			// Inherit the agent directory's mode rather than imposing 0755. A user
			// who keeps ~/.omp/agent at 0700 should not find that installing an
			// integration created a world-readable directory inside it.
			if m := parseMode(s.agent.Mode); m != 0 {
				mode = m
			}
		}
		p.Ops = append(p.Ops, Op{
			Kind:   OpMkdir,
			Path:   s.paths.OwnedDir,
			Mode:   renderMode(mode),
			mode:   mode,
			Note:   "create the extension directory OMP loads",
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
			Note:     "write version " + s.asset.Version + " of the Sidecar lifecycle extension",
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
// It deliberately does not check the collision: cleanup has to work in every
// state an install could have left, and a directory that became shared after the
// install is exactly a state a user would want to clean up out of.
//
// Herdr's uninstall_omp deletes its file without checking that the file is still
// its own. Sidecar does not copy that: ownership is proved from the file's own
// bytes, and a file that has stopped carrying Sidecar's marker is somebody
// else's now.
func (a OmpAdapter) planUninstall(s ompState, p Plan) (Plan, error) {
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
			Note:   "remove the Sidecar lifecycle extension",
			Before: s.owned, After: FileState{Path: s.paths.Owned},
		})
		removed = append(removed, s.paths.Owned)
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

	// The extension directory goes only if removing Sidecar's own files empties
	// it. On a machine that also has Herdr installed this directory holds Herdr's
	// own OMP extension, which is exactly the case this rule exists for.
	if dirEmptyExcept(s.paths.OwnedDir, removed) {
		p.Ops = append(p.Ops, Op{
			Kind: OpRmdir, Path: s.paths.OwnedDir,
			Note:   "remove the extension directory, which holds nothing else",
			Before: s.dir, After: FileState{Path: s.paths.OwnedDir},
		})
	}

	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

var _ Adapter = OmpAdapter{}

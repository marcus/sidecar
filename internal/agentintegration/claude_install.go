package agentintegration

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Claude Code adapter.
//
// Claude Code keeps hooks inside ~/.claude/settings.json — a shared, strictly
// JSON, user-owned file that Sidecar adds exactly one SessionStart entry to.
// The entry is session-identity only: it reports WHICH conversation occupies
// the pane, and screen/process detection remains the sole authority for
// lifecycle state. Claude's configuration merges additively across five
// layers, so Sidecar can never own the effective hook set — only its own entry
// in the user layer — and the capability registry records that as a known gap.
//
// Ownership follows the model in hookconfig.go: an entry that invokes
// `sidecar agent report-session` is Sidecar's to manage, an entry equal to the
// bundled one is current, and everything else in the file is preserved
// token-for-token, in order.

// Claude integration identity.
const (
	ClaudeProvider = "claude"
	ClaudeSource   = "sidecar.claude.hooks"

	// ClaudeAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to
	// claudeEntrySpec's canonical history so installed copies read as
	// outdated rather than damaged.
	ClaudeAssetVersion = "1"

	// ClaudeAssetSchema is the plan/marker schema the asset declares.
	ClaudeAssetSchema = 1

	// ClaudeBackupSuffix names the recoverable copy kept beside settings.json
	// before any rewrite of a pre-existing file.
	ClaudeBackupSuffix = ".sidecar-backup"
)

// claudeMatcher is the canonical SessionStart matcher: every source
// (startup, resume, clear, compact) reports identity.
var claudeMatcher = "*"

// claudeCanonicalEntry is the exact hook entry version 1 ships.
func claudeCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(reportSessionCommand(ClaudeProvider))},
		{key: "timeout", val: json.RawMessage("10")},
	})
}

// claudeCanonicalGroup is the group the entry is installed in.
func claudeCanonicalGroup() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "matcher", val: mustJSONString(claudeMatcher)},
		{key: "hooks", val: marshalJSONArray([]json.RawMessage{claudeCanonicalEntry()})},
	})
}

func claudeEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		matcher: &claudeMatcher,
		canonical: []versionedEntry{
			{version: ClaudeAssetVersion, entry: claudeCanonicalEntry()},
		},
	}
}

func mustJSONString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		// A Go string always marshals.
		panic(err)
	}
	return b
}

// ClaudeAdapter installs Sidecar's Claude Code session-identity hook.
type ClaudeAdapter struct{}

func (ClaudeAdapter) Provider() string { return ClaudeProvider }
func (ClaudeAdapter) Source() string   { return ClaudeSource }

// Assets returns the one entry asset this integration installs.
//
// It is OwnsEntry: ~/.claude/settings.json belongs to the user, Sidecar owns
// one hook entry inside it, and Content is the canonical file Sidecar would
// create in an empty tree -- a description of the entry, shown so a surface can
// name exactly what an install adds, never bytes written over a user's file.
func (ClaudeAdapter) Assets() []Asset {
	return []Asset{{
		Name:          "settings.json",
		Source:        ClaudeSource,
		SchemaVersion: ClaudeAssetSchema,
		Version:       ClaudeAssetVersion,
		Ownership:     OwnsEntry,
		Content:       string(renderJSONFile([]jsonMember{{key: "hooks", val: marshalJSONObject([]jsonMember{{key: "SessionStart", val: marshalJSONArray([]json.RawMessage{claudeCanonicalGroup()})}})}})),
	}}
}

func (a ClaudeAdapter) settingsAsset() Asset { return a.Assets()[0] }

type claudePaths struct {
	Dir      string
	Settings string
	Backup   string
}

// ClaudePaths returns the paths the Claude adapter inspects and touches.
func ClaudePaths(env Env) []string {
	p := claudePathsFor(env)
	return []string{p.Settings}
}

func claudePathsFor(env Env) claudePaths {
	dir := filepath.Join(env.Home, ".claude")
	settings := filepath.Join(dir, "settings.json")
	return claudePaths{Dir: dir, Settings: settings, Backup: settings + ClaudeBackupSuffix}
}

// claudeState is everything one inspection learned; both Inspect and Plan are
// built from it so a plan is never based on a different reading of the disk
// than the status the user was shown.
type claudeState struct {
	env      Env
	paths    claudePaths
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

func (a ClaudeAdapter) inspect(env Env) claudeState {
	p := claudePathsFor(env)
	s := claudeState{
		env:      env,
		paths:    p,
		spec:     claudeEntrySpec(),
		dir:      inspectDir(env, p.Dir),
		settings: inspectFile(env, p.Settings, a.settingsAsset()),
		backup:   FileState{Path: p.Backup, Exists: fileExists(p.Backup)},
	}
	if path, ok := env.lookPath(ClaudeProvider); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(ClaudeProvider)
	}
	s.raw, s.scan = scanEntryFile(s.settings, s.spec)
	if len(s.scan.owned) > 0 {
		ownEntry(&s.settings, s.scan.owned[len(s.scan.owned)-1].version)
	}
	s.assetStatus, s.message, s.installed = entryAssetStatus(s.dir, s.settings, s.scan, s.spec, "settings.json")

	s.status = s.assetStatus
	if s.providerPath == "" {
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the claude CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// scanEntryFile reads and scans a hook configuration file that has already
// been inspected, folding the file-safety verdict into the scan.
func scanEntryFile(file FileState, spec hookEntrySpec) ([]byte, hookTreeScan) {
	if !file.Exists {
		return nil, hookTreeScan{}
	}
	if file.Unsafe != "" {
		return nil, hookTreeScan{exists: true, parseErr: file.UnsafeDetail}
	}
	if file.Size > maxAssetBytes {
		return nil, hookTreeScan{exists: true, parseErr: "the file is larger than any configuration Sidecar will edit"}
	}
	b, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, hookTreeScan{exists: true, parseErr: "the file exists but could not be read"}
	}
	return b, scanHookTree(true, b, spec)
}

// entryAssetStatus decides the status an entry-in-file integration has, from
// the inspected file alone. Nothing here trusts a claimed version: the entry's
// bytes are compared with the bundled entry, so a hand-edited or duplicated
// entry reads as needs-repair rather than current.
func entryAssetStatus(dir, file FileState, scan hookTreeScan, spec hookEntrySpec, name string) (agentlifecycle.IntegrationStatus, string, string) {
	// wanted is how many owned entries a converged file holds: one per event
	// the spec installs under, which is one for every integration except Devin.
	wanted := len(spec.eventNames())
	switch {
	case dir.Exists && dir.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, dir.UnsafeDetail + " (" + dir.Path + ")", ""
	case file.Exists && file.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, file.UnsafeDetail + " (" + file.Path + ")", ""
	case scan.parseErr != "":
		return agentlifecycle.StatusNeedsRepair, name + " could not be interpreted (" + scan.parseErr + "), so the integration state is unknown; Sidecar will not modify the file", ""
	case len(scan.owned) == 0:
		return agentlifecycle.StatusNotInstalled, "", ""
	case len(scan.owned) > wanted:
		return agentlifecycle.StatusNeedsRepair, "more than one Sidecar report-session entry is installed in " + name + ", so every session would be reported twice; repair converges on exactly one", versionOf(scan)
	case len(scan.owned) < wanted:
		// Only a multi-event integration can reach this, and it is the shape a
		// half-finished hand edit leaves: some of the provider's events carry
		// Sidecar's entry and the rest do not, so a session binds on some turns
		// and silently not on others.
		return agentlifecycle.StatusNeedsRepair, "Sidecar's session-identity entry is installed under " +
			strconv.Itoa(len(scan.owned)) + " of the " + strconv.Itoa(wanted) + " hook events it belongs under in " + name +
			", so a session would be bound only some of the time; repair restores every one", versionOf(scan)
	case anyOwnedEntry(scan, func(o ownedHookEntry) bool { return o.version == "" }):
		return agentlifecycle.StatusNeedsRepair, "the installed entry in " + name + " invokes Sidecar's report-session command but no longer matches what Sidecar ships", ""
	case anyOwnedEntry(scan, func(o ownedHookEntry) bool { return !o.groupCanonical }):
		return agentlifecycle.StatusNeedsRepair, "the installed entry in " + name + " sits under a changed event or matcher, so it no longer fires the way Sidecar qualified it", scan.owned[0].version
	case !ownedEntriesCoverEachEvent(scan, spec):
		// The right NUMBER of entries in the wrong places. Counting alone cannot
		// see this: a hand edit that moves two entries onto an event that
		// already has one leaves six entries, every one byte-identical to the
		// bundled entry and every one under an event the spec names, so the two
		// branches above are both satisfied and the file reads as current while
		// two of Devin's events fire nothing at all.
		//
		// A single-event integration cannot reach this branch: it arrives here
		// with exactly one owned entry whose group is canonical, which for a
		// one-event spec is coverage by construction. So Claude and Codex are
		// unchanged by it.
		return agentlifecycle.StatusNeedsRepair, "Sidecar's session-identity entries in " + name +
			" are not one under each of the " + strconv.Itoa(wanted) + " hook events they belong under, so some events " +
			"would report twice and others not at all; repair restores one under each", scan.owned[0].version
	case anyOwnedEntry(scan, func(o ownedHookEntry) bool { return o.version != spec.current().version }):
		return agentlifecycle.StatusOutdated, "version " + scan.owned[0].version + " is installed; this build ships version " + spec.current().version, scan.owned[0].version
	}
	return agentlifecycle.StatusCurrent, "", scan.owned[0].version
}

// anyOwnedEntry reports whether any Sidecar-owned entry in the scan satisfies
// the predicate. For a single-event integration it is exactly a test on
// scan.owned[0]; for a multi-event one it is what stops a fault in the fifth
// entry from reading as a healthy install because the first entry is fine.
func anyOwnedEntry(scan hookTreeScan, pred func(ownedHookEntry) bool) bool {
	for _, o := range scan.owned {
		if pred(o) {
			return true
		}
	}
	return false
}

// ownedEntriesCoverEachEvent reports whether the scan holds exactly one
// Sidecar-owned entry under every event the spec installs under.
//
// It is the distribution question, which the count is not: len(scan.owned) says
// how many entries exist and this says whether they are in the right places.
func ownedEntriesCoverEachEvent(scan hookTreeScan, spec hookEntrySpec) bool {
	perEvent := map[string]int{}
	for _, o := range scan.owned {
		perEvent[o.event]++
	}
	for _, event := range spec.eventNames() {
		if perEvent[event] != 1 {
			return false
		}
	}
	return true
}

func versionOf(scan hookTreeScan) string {
	for _, o := range scan.owned {
		if o.version != "" {
			return o.version
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Inspect implements [Adapter].
func (a ClaudeAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a ClaudeAdapter) statusOf(s claudeState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(ClaudeSource)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              ClaudeProvider,
		Source:                ClaudeSource,
		Status:                s.status,
		BundledVersion:        ClaudeAssetVersion,
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
func (a ClaudeAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a ClaudeAdapter) plan(s claudeState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      ClaudeProvider,
		Source:        ClaudeSource,
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
// entry under hooks.SessionStart and everything else in the file untouched.
func (a ClaudeAdapter) planConverge(s claudeState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the claude CLI was not found on PATH, so Sidecar will not modify %s for it; install claude first", s.paths.Settings)
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.Settings, ClaudeProvider, s.installed, s.message); err != nil {
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
	top, err = appendCanonicalGroup(top, claudeCanonicalGroup())
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Settings, "%s: %v", s.paths.Settings, err)
	}
	content := renderJSONFile(top)

	p.Ops = entryFileOps(nil, s.env, s.dir, s.settings, s.backup, content,
		"write the Sidecar session-identity hook entry, preserving every other setting", ownedEntry(ClaudeAssetVersion))
	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes exactly Sidecar's entry and the containers that held
// nothing else, and never anything more.
func (a ClaudeAdapter) planUninstall(s claudeState, p Plan) (Plan, error) {
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

// gateConvergeVerb applies the same verb/starting-state discipline every
// adapter uses: the user says what they believe the situation is, and Sidecar
// disagrees out loud when it is something else.
func gateConvergeVerb(assetStatus agentlifecycle.IntegrationStatus, act Action, path, provider, installed, message string) error {
	switch assetStatus {
	case agentlifecycle.StatusNotInstalled:
		if act != ActionInstall {
			return refuse(RefuseNotInstalled, path,
				"nothing of Sidecar's is installed in %s; run sidecar agent integration install %s", path, provider)
		}
	case agentlifecycle.StatusOutdated:
		if act == ActionInstall {
			return refuse(RefuseAlreadyInstalled, path,
				"version %s is already installed in %s; run sidecar agent integration update %s", installed, path, provider)
		}
	case agentlifecycle.StatusNeedsRepair:
		if act != ActionRepair {
			return refuse(RefuseNeedsRepair, path,
				"the installation needs repair (%s); run sidecar agent integration repair %s", message, provider)
		}
	}
	return nil
}

// refuseUnsafeEntryFile is the safety gate every mutation of an entry-carrying
// file passes: the directory and file must be usable, and the file must have
// been interpretable — an unparseable file is never rewritten, because a
// rewrite of a file the scan could not read is a clobber by construction.
func refuseUnsafeEntryFile(dir, file FileState, scan hookTreeScan) error {
	if dir.Exists && dir.Unsafe != "" {
		return refuse(dir.Unsafe, dir.Path, "%s: %s", dir.Path, dir.UnsafeDetail)
	}
	if file.Exists && file.Unsafe != "" {
		return refuse(file.Unsafe, file.Path, "%s: %s", file.Path, file.UnsafeDetail)
	}
	if scan.parseErr != "" {
		return refuse(RefuseUnreadable, file.Path,
			"%s could not be interpreted (%s); Sidecar will not rewrite a file it cannot read — fix or move it yourself and run this again", file.Path, scan.parseErr)
	}
	return nil
}

// entryOutcome is what the file contains once an entry write lands.
//
// It is a parameter rather than an assumption because entryFileOps serves both
// directions: install and repair write Sidecar's entry in, and uninstall writes
// the same file back out without it. Hard-coding the after-state as owned made
// the dry-run preview tell a user that the file "contains Sidecar's entry" for
// the very operation that removes it — the op list was right and only the
// rendered claim was wrong, which is the more dangerous of the two, because a
// preview is the thing a cautious user reads instead of the code.
type entryOutcome struct {
	// Owned reports whether Sidecar's entry is present in the result.
	Owned bool
	// Version is the asset version the entry declares. It is meaningful only
	// when Owned is set.
	Version string
}

// ownedEntry is the after-state of a write that puts Sidecar's entry in.
func ownedEntry(version string) entryOutcome { return entryOutcome{Owned: true, Version: version} }

// userEntry is the after-state of a write that leaves the file to its user,
// which is what removing Sidecar's entry from a file the user owns produces.
func userEntry() entryOutcome { return entryOutcome{} }

// entryFileOps builds the ordered operations that land new content in an
// entry-carrying file: create the directory if needed, keep a recoverable
// copy of a pre-existing file, and write atomically. It returns nothing when
// the content already matches byte for byte, which is how idempotency stays
// visible.
func entryFileOps(ops []Op, env Env, dir, file, backup FileState, content []byte, note string, outcome entryOutcome) []Op {
	if !dir.Exists {
		mode := fs.FileMode(0o700)
		ops = append(ops, Op{
			Kind:   OpMkdir,
			Path:   dir.Path,
			Mode:   renderMode(mode),
			mode:   mode,
			Note:   "create the provider configuration directory",
			Before: dir,
			After:  FileState{Path: dir.Path, Exists: true, Kind: "dir", Mode: renderMode(mode)},
		})
	}
	if file.Exists && file.Checksum == checksum(content) {
		return ops
	}
	mode := fs.FileMode(0o644)
	if file.Exists {
		if m := parseMode(file.Mode); m != 0 {
			// A user who keeps their settings at 0600 should not find that an
			// integration install loosened them.
			mode = m
		}
		ops = append(ops, Op{
			Kind:     OpBackup,
			Path:     backup.Path,
			From:     file.Path,
			Mode:     renderMode(mode),
			mode:     mode,
			Bytes:    int(file.Size),
			Checksum: file.Checksum,
			Note:     "keep a recoverable copy of the file being rewritten",
			Before:   backup,
			After:    FileState{Path: backup.Path, Exists: true, Kind: "file", Checksum: file.Checksum, Mode: renderMode(mode), Size: file.Size},
		})
	}
	after := FileState{
		Path: file.Path, Exists: true, Kind: "file",
		Checksum: checksum(content), Mode: renderMode(mode), Size: int64(len(content)),
	}
	if outcome.Owned {
		after.Owned, after.Ownership, after.Version = true, OwnsEntry, outcome.Version
	}
	ops = append(ops, Op{
		Kind:     OpWrite,
		Path:     file.Path,
		Mode:     renderMode(mode),
		mode:     mode,
		Bytes:    len(content),
		Checksum: checksum(content),
		content:  content,
		Note:     note,
		Before:   file,
		After:    after,
	})
	return ops
}

// removalOps rewrites an entry-carrying file without Sidecar's entries, or
// removes it entirely when nothing at all remains — a file that held only
// Sidecar's entry is a file Sidecar created.
func removalOps(file, backup FileState, top []jsonMember, note string) []Op {
	mode := fs.FileMode(0o644)
	if m := parseMode(file.Mode); m != 0 {
		mode = m
	}
	ops := []Op{{
		Kind:     OpBackup,
		Path:     backup.Path,
		From:     file.Path,
		Mode:     renderMode(mode),
		mode:     mode,
		Bytes:    int(file.Size),
		Checksum: file.Checksum,
		Note:     "keep a recoverable copy of the file being rewritten",
		Before:   backup,
		After:    FileState{Path: backup.Path, Exists: true, Kind: "file", Checksum: file.Checksum, Mode: renderMode(mode), Size: file.Size},
	}}
	if len(top) == 0 {
		return append(ops, Op{
			Kind:   OpRemove,
			Path:   file.Path,
			Note:   "remove the file, which held nothing but Sidecar's entry",
			Before: file,
			After:  FileState{Path: file.Path},
		})
	}
	content := renderJSONFile(top)
	return append(ops, Op{
		Kind:     OpWrite,
		Path:     file.Path,
		Mode:     renderMode(mode),
		mode:     mode,
		Bytes:    len(content),
		Checksum: checksum(content),
		content:  content,
		Note:     note,
		Before:   file,
		After:    FileState{Path: file.Path, Exists: true, Kind: "file", Checksum: checksum(content), Mode: renderMode(mode), Size: int64(len(content))},
	})
}

var _ Adapter = ClaudeAdapter{}

package agentintegration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Codex adapter.
//
// Codex needs three owned mutations where Claude needs one, all for the same
// single SessionStart hook:
//
//  1. ~/.codex/hooks.json — the hook entry itself, in a group without a
//     matcher key (unlike Claude's).
//  2. ~/.codex/config.toml — `[features]` `hooks = true`, without which hooks
//     are disabled entirely.
//  3. ~/.codex/config.toml — a `[hooks.state."<key>"]` table whose
//     trusted_hash marks the hook as user-approved; without it Codex prompts
//     for or refuses the hook on every run.
//
// hooks.json follows the shared entry-ownership model in hookconfig.go.
// config.toml is a user-owned TOML file full of comments and unrelated
// configuration, so it is edited line-surgically — never re-serialized — and
// any spelling of the two regions Sidecar must touch that the editor does not
// understand is refused rather than guessed at.
//
// # Why there is a TOML parser here as well as a line scanner
//
// A line scanner is the only way to keep a user's comments, ordering and
// formatting, and it is a terrible way to decide what a file means. So the two
// jobs are split, and a real TOML parser is used as a read-only oracle at both
// ends of every edit — never to serialize anything, because a round trip
// through a parsed document is precisely what would destroy the formatting the
// line writer exists to preserve.
//
//  1. Before planning, the whole file is parsed. A file that is not valid TOML
//     is refused outright: Sidecar does not edit what it cannot fully
//     understand, and a line scanner's opinion of an invalid file is worthless.
//     This also removes a whole class of ambiguity from the scanner below — a
//     duplicate table, a key defined twice, a header split over two lines are
//     all already impossible by the time the scanner runs.
//  2. After composing, the result is parsed again and semantically diffed
//     against the pre-image. Every key/value path the user owned must still be
//     there with the same value, nothing outside Sidecar's region may appear,
//     and Sidecar's own entry must be present (converge) or gone (uninstall).
//     The diff runs at plan time, so a rewrite that would not verify produces a
//     refusal with nothing in the op list rather than a partial change on disk.
//
// The line scanner is still held to its own contract — it refuses spellings it
// cannot read rather than skipping them — so the oracle is a second line of
// defence rather than the only one.
//
// # The trust hash
//
// The trusted_hash algorithm was reproduced from the codex-rs source
// (codex-rs/hooks/src/engine/discovery.rs hook_hash and
// codex-rs/config/src/fingerprint.rs version_for_toml) and verified by
// reproducing a live codex-cli 0.151.0 trust record byte for byte: the hash is
// `sha256:` + hex(SHA-256(canonical JSON)) of the normalized hook identity
// {"event_name":"session_start","hooks":[{"async":false,"command":C,
// "timeout":T,"type":"command"}]} with object keys sorted recursively, absent
// optional fields omitted, and no whitespace. The state key is positional —
// "<abs hooks.json path>:session_start:<group>:<hook>" — which is why the key
// is computed from where the entry actually lands and why user edits that
// reorder hooks.json are a recorded known gap. The algorithm is a provider
// implementation detail, not a published contract; TestCodexTrustedHash pins
// the live-verified vector so a drift is a failing test rather than a silent
// re-prompt.

// Codex integration identity.
const (
	CodexProvider = "codex"
	CodexSource   = "sidecar.codex.hooks"

	// CodexAssetVersion is the bundled entry's version. Bump it whenever the
	// canonical entry changes, and append the superseded entry to
	// codexEntrySpec's canonical history so installed copies read as outdated
	// rather than damaged.
	CodexAssetVersion = "1"

	// CodexAssetSchema is the plan/marker schema the asset declares.
	CodexAssetSchema = 1

	// CodexBackupSuffix names the recoverable copy kept beside each file
	// before any rewrite of a pre-existing file.
	CodexBackupSuffix = ".sidecar-backup"
)

// codexCanonicalEntry is the exact hook entry version 1 ships.
func codexCanonicalEntry() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "type", val: json.RawMessage(`"command"`)},
		{key: "command", val: mustJSONString(reportSessionCommand(CodexProvider))},
		{key: "timeout", val: json.RawMessage("10")},
	})
}

// codexCanonicalGroup is the group the entry is installed in — no matcher key,
// which is how Codex (unlike Claude) spells "every session".
func codexCanonicalGroup() json.RawMessage {
	return marshalJSONObject([]jsonMember{
		{key: "hooks", val: marshalJSONArray([]json.RawMessage{codexCanonicalEntry()})},
	})
}

func codexEntrySpec() hookEntrySpec {
	return hookEntrySpec{
		matcher: nil,
		canonical: []versionedEntry{
			{version: CodexAssetVersion, entry: codexCanonicalEntry()},
		},
	}
}

// codexTrustedHash computes the trusted_hash Codex records for a command hook.
func codexTrustedHash(command string, timeoutSec int) string {
	identity := fmt.Sprintf(`{"event_name":"session_start","hooks":[{"async":false,"command":%s,"timeout":%d,"type":"command"}]}`,
		serdeJSONString(command), timeoutSec)
	sum := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// serdeJSONString encodes a string the way serde_json does — no HTML escaping —
// so the canonical identity hashes to the same bytes Codex computes.
func serdeJSONString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// A Go string always encodes.
		panic(err)
	}
	return string(bytes.TrimSpace(b.Bytes()))
}

// codexTrustHashes returns every trusted_hash Sidecar has ever written, newest
// last. A config.toml state table carrying one of these values is Sidecar's,
// which is what lets uninstall find its own stale records without ever
// touching another tool's — the hash of Sidecar's fixed command is as unique
// as the command itself.
func codexTrustHashes() []string {
	return []string{codexTrustedHash(reportSessionCommand(CodexProvider), hookTimeoutSec)}
}

// CodexAdapter installs Sidecar's Codex session-identity hook.
type CodexAdapter struct{}

func (CodexAdapter) Provider() string { return CodexProvider }
func (CodexAdapter) Source() string   { return CodexSource }

// Assets returns the two entry assets this integration installs.
//
// This is the adapter that made assets plural. Codex needs an entry in
// hooks.json *and* two regions of config.toml, and both files are the user's.
// While an integration could describe only one bundled thing, the second file
// was simply absent from everything an asset feeds: a surface asking "what does
// this integration install" was answered with hooks.json and told nothing about
// the feature flag and trust record that make hooks.json do anything at all.
func (CodexAdapter) Assets() []Asset {
	return []Asset{
		{
			Name:          "hooks.json",
			Source:        CodexSource,
			SchemaVersion: CodexAssetSchema,
			Version:       CodexAssetVersion,
			Ownership:     OwnsEntry,
			Content:       string(renderJSONFile([]jsonMember{{key: "hooks", val: marshalJSONObject([]jsonMember{{key: "SessionStart", val: marshalJSONArray([]json.RawMessage{codexCanonicalGroup()})}})}})),
		},
		{
			Name:          "config.toml",
			Source:        CodexSource,
			SchemaVersion: CodexAssetSchema,
			Version:       CodexAssetVersion,
			Ownership:     OwnsEntry,
			Content:       "[features]\nhooks = true\n\n" + codexStateBlock("<hooks.json path>:session_start:<group>:<hook>", codexTrustHashes()[len(codexTrustHashes())-1]),
		},
	}
}

func (a CodexAdapter) hooksAsset() Asset  { return a.Assets()[0] }
func (a CodexAdapter) configAsset() Asset { return a.Assets()[1] }

type codexPaths struct {
	Dir          string
	Hooks        string
	Config       string
	HooksBackup  string
	ConfigBackup string
}

// CodexPaths returns the paths the Codex adapter inspects and touches.
func CodexPaths(env Env) []string {
	p := codexPathsFor(env)
	return []string{p.Hooks, p.Config}
}

func codexPathsFor(env Env) codexPaths {
	dir := filepath.Join(env.Home, ".codex")
	return codexPaths{
		Dir:          dir,
		Hooks:        filepath.Join(dir, "hooks.json"),
		Config:       filepath.Join(dir, "config.toml"),
		HooksBackup:  filepath.Join(dir, "hooks.json"+CodexBackupSuffix),
		ConfigBackup: filepath.Join(dir, "config.toml"+CodexBackupSuffix),
	}
}

// codexState is everything one inspection learned.
type codexState struct {
	env          Env
	paths        codexPaths
	spec         hookEntrySpec
	dir          FileState
	hooks        FileState
	config       FileState
	hooksBackup  FileState
	configBackup FileState
	hooksScan    hookTreeScan
	configScan   codexConfigScan

	// wantKey/wantHash are the state key and trusted_hash for the entry where
	// it sits now — meaningful only when exactly one owned entry exists under
	// SessionStart.
	wantKey  string
	wantHash string
	// ownedTables are the config.toml trust tables Sidecar owns.
	ownedTables []tomlStateTable
	// trustConverged reports that config.toml already records exactly the
	// right feature flag and trust for the installed entry.
	trustConverged bool

	providerPath    string
	providerVersion string

	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a CodexAdapter) inspect(env Env) codexState {
	p := codexPathsFor(env)
	s := codexState{
		env:          env,
		paths:        p,
		spec:         codexEntrySpec(),
		dir:          inspectDir(env, p.Dir),
		hooks:        inspectFile(env, p.Hooks, a.hooksAsset()),
		config:       inspectFile(env, p.Config, a.configAsset()),
		hooksBackup:  FileState{Path: p.HooksBackup, Exists: fileExists(p.HooksBackup)},
		configBackup: FileState{Path: p.ConfigBackup, Exists: fileExists(p.ConfigBackup)},
	}
	if path, ok := env.lookPath(CodexProvider); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(CodexProvider)
	}
	_, s.hooksScan = scanEntryFile(s.hooks, s.spec)
	if len(s.hooksScan.owned) > 0 {
		ownEntry(&s.hooks, s.hooksScan.owned[len(s.hooksScan.owned)-1].version)
	}

	configRaw, configOK := readEntryFileBytes(s.config)
	if configOK {
		s.configScan = scanCodexConfig(s.config.Exists, configRaw)
	} else {
		s.configScan = codexConfigScan{exists: true, parseErr: "the file exists but could not be read"}
	}

	if s.hooksScan.converged(s.spec) {
		owned := s.hooksScan.owned[0]
		s.wantKey = codexStateKey(p.Hooks, owned.group, owned.hook)
		s.wantHash = codexTrustHashes()[len(codexTrustHashes())-1]
	}
	s.ownedTables = codexOwnedTables(s.configScan, s.wantKey)
	if len(s.ownedTables) > 0 {
		// The version of a trust record is the version of the entry whose
		// command it hashes, so a record found by hash names its version and one
		// found only by position does not. Leaving it empty in the second case is
		// the honest answer, and it is now renderable: a surface asks Ownership
		// what Owned means rather than assuming every owned file has a version.
		ownEntry(&s.config, codexTrustVersion(s.ownedTables))
	}
	s.trustConverged = s.wantKey != "" && s.configScan.parseErr == "" &&
		s.configScan.hooksEnabled() && len(s.ownedTables) == 1 &&
		s.ownedTables[0].key == s.wantKey && s.ownedTables[0].hash == s.wantHash

	s.assetStatus, s.message, s.installed = codexAssetStatus(s)

	s.status = s.assetStatus
	if s.providerPath == "" {
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the codex CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

func codexStateKey(hooksPath string, group, hook int) string {
	return hooksPath + ":session_start:" + strconv.Itoa(group) + ":" + strconv.Itoa(hook)
}

// codexOwnedTables selects the config.toml trust tables that are Sidecar's: a
// table whose hash is one Sidecar computes for its own command, or whose key
// names the position Sidecar's installed entry occupies right now. Everything
// else — herdr's records, hand-written trust — is never touched.
func codexOwnedTables(scan codexConfigScan, wantKey string) []tomlStateTable {
	ours := map[string]bool{}
	for _, h := range codexTrustHashes() {
		ours[h] = true
	}
	var out []tomlStateTable
	for _, t := range scan.state {
		if ours[t.hash] || (wantKey != "" && t.key == wantKey) {
			out = append(out, t)
		}
	}
	return out
}

// multilineStringOpener finds the first `"""` or `”'` that actually opens a
// multi-line string, returning its 1-based line number.
//
// The previous rule was a substring search over the whole file, which refused
// on any occurrence anywhere -- including inside a `#` comment and inside an
// ordinary single-line string. That is not a cautious over-approximation of a
// small risk: parseErr refuses install, refuses repair, AND refuses uninstall,
// so a user whose config.toml merely mentions `"""` in a comment could neither
// integrate Codex nor clean up an existing integration. The file has already
// been proved valid TOML by the oracle above, so the only thing left to decide
// is whether a delimiter is structural, and comment and single-line-string
// state is exactly what tells us.
//
// The refusal itself is kept: a genuine multi-line string really does defeat a
// line scanner. This narrows it to the case that warrants it.
func multilineStringOpener(content string) (int, bool) {
	for i, line := range strings.Split(content, "\n") {
		if multilineOpenerInLine(line) {
			return i + 1, true
		}
	}
	return 0, false
}

// multilineOpenerInLine walks one line tracking basic-string, literal-string,
// and comment state, and reports whether a triple delimiter survives outside
// all three.
//
// A triple delimiter that opens and closes on the same line is still a
// multi-line string syntactically, and TOML permits it; it is reported, because
// the scanner's problem is the delimiter, not the newline.
func multilineOpenerInLine(line string) bool {
	const (
		plain = iota
		basic
		literal
	)
	state := plain
	for i := 0; i < len(line); i++ {
		switch state {
		case plain:
			switch {
			case strings.HasPrefix(line[i:], `"""`), strings.HasPrefix(line[i:], "'''"):
				return true
			case line[i] == '#':
				// The rest of the line is a comment and has no structure.
				return false
			case line[i] == '"':
				state = basic
			case line[i] == '\'':
				state = literal
			}
		case basic:
			// Only a basic string honours backslash escapes, so only here can a
			// quote be escaped rather than closing.
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '"' {
				state = plain
			}
		case literal:
			if line[i] == '\'' {
				state = plain
			}
		}
	}
	return false
}

// codexTrustVersion names the asset version a set of owned trust tables
// records, or "" when none of them can be attributed to a version.
//
// codexTrustHashes is ordered oldest-first and its last element is the current
// version's hash, which is the same ordering codexEntrySpec's canonical history
// uses, so the two are indexed together.
func codexTrustVersion(tables []tomlStateTable) string {
	hashes := codexTrustHashes()
	versions := codexEntrySpec().canonical
	if len(hashes) != len(versions) {
		// The two lists are grown together by hand. If they ever disagree,
		// claiming a version would be guessing about a security record.
		return ""
	}
	best := ""
	for _, t := range tables {
		for i, h := range hashes {
			if t.hash == h {
				best = versions[i].version
			}
		}
	}
	return best
}

// readEntryFileBytes reads an inspected file's bytes, honoring the inspection:
// an absent file reads as empty and safe, an unsafe or oversized one as not
// readable.
func readEntryFileBytes(file FileState) ([]byte, bool) {
	if !file.Exists {
		return nil, true
	}
	if file.Unsafe != "" || file.Size > maxAssetBytes {
		return nil, false
	}
	b, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// codexAssetStatus decides the status from the inspected files alone.
func codexAssetStatus(s codexState) (agentlifecycle.IntegrationStatus, string, string) {
	st, msg, installed := entryAssetStatus(s.dir, s.hooks, s.hooksScan, s.spec, "hooks.json")
	switch st {
	case agentlifecycle.StatusNotInstalled:
		if s.config.Exists && s.config.Unsafe != "" {
			return agentlifecycle.StatusNeedsRepair, s.config.UnsafeDetail + " (" + s.paths.Config + ")", ""
		}
		if len(s.ownedTables) > 0 {
			return agentlifecycle.StatusNeedsRepair,
				"no hook is installed but Sidecar trust records remain in config.toml; repair reinstalls, uninstall removes them", ""
		}
		if s.configScan.parseErr != "" {
			// Nothing of Sidecar's is here, which is the primary fact; the
			// config.toml problem matters only once an install is attempted,
			// and the install will refuse with the specific reason.
			return st, "config.toml could not be interpreted (" + s.configScan.parseErr + ")", ""
		}
		return st, msg, installed
	case agentlifecycle.StatusCurrent:
		switch {
		case s.config.Exists && s.config.Unsafe != "":
			return agentlifecycle.StatusNeedsRepair, s.config.UnsafeDetail + " (" + s.paths.Config + ")", installed
		case s.configScan.parseErr != "":
			return agentlifecycle.StatusNeedsRepair,
				"config.toml could not be interpreted (" + s.configScan.parseErr + "), so the hook's feature flag and trust cannot be verified", installed
		case !s.configScan.hooksEnabled():
			return agentlifecycle.StatusNeedsRepair,
				"the hook is installed but config.toml does not enable features.hooks, so Codex ignores it entirely; repair enables the flag", installed
		case !s.trustConverged:
			return agentlifecycle.StatusNeedsRepair,
				"the hook is installed but its trust record in config.toml is missing or stale, so Codex will prompt for or refuse it; repair rewrites the record", installed
		}
		return st, msg, installed
	}
	return st, msg, installed
}

// Inspect implements [Adapter].
func (a CodexAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a CodexAdapter) statusOf(s codexState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(CodexSource)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              CodexProvider,
		Source:                CodexSource,
		Status:                s.status,
		BundledVersion:        CodexAssetVersion,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.Hooks, s.paths.Config},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.hooks, s.config, s.hooksBackup, s.configBackup}
	for _, act := range Actions() {
		if _, err := a.plan(s, act); err == nil {
			st.Offered = append(st.Offered, act)
		}
	}
	return st
}

// Plan implements [Adapter].
func (a CodexAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a CodexAdapter) plan(s codexState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      CodexProvider,
		Source:        CodexSource,
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

// planConverge builds the plan that ends with exactly one canonical entry in
// hooks.json, the feature flag on, and one matching trust record — and
// everything Sidecar does not own byte-identical.
func (a CodexAdapter) planConverge(s codexState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the codex CLI was not found on PATH, so Sidecar will not modify %s for it; install codex first", s.paths.Dir)
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.Hooks, CodexProvider, s.installed, s.message); err != nil {
		return Plan{}, err
	}
	if err := refuseUnsafeEntryFile(s.dir, s.hooks, s.hooksScan); err != nil {
		return Plan{}, err
	}
	if s.config.Exists && s.config.Unsafe != "" {
		return Plan{}, refuse(s.config.Unsafe, s.paths.Config, "%s: %s", s.paths.Config, s.config.UnsafeDetail)
	}
	if s.configScan.parseErr != "" {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config,
			"%s could not be interpreted (%s); Sidecar will not edit a file it cannot read — fix or move it yourself and run this again", s.paths.Config, s.configScan.parseErr)
	}

	if s.hooksScan.converged(s.spec) && s.trustConverged {
		p.Unchanged = true
		return p, nil
	}

	// hooks.json first: an interruption between the two writes then leaves an
	// installed hook Codex declines to trust — visible and safe — rather than
	// a trust record for content that does not exist.
	wantKey, wantHash := s.wantKey, s.wantHash
	if !s.hooksScan.converged(s.spec) {
		top, _, err := stripOwnedHookEntries(s.hooksScan, s.spec)
		if err != nil {
			return Plan{}, refuse(RefuseUnreadable, s.paths.Hooks, "%s: %v", s.paths.Hooks, err)
		}
		group, err := sessionStartGroupCount(top)
		if err != nil {
			return Plan{}, refuse(RefuseUnreadable, s.paths.Hooks, "%s: %v", s.paths.Hooks, err)
		}
		top, err = appendCanonicalEntry(top, codexCanonicalGroup(), s.spec)
		if err != nil {
			return Plan{}, refuse(RefuseUnreadable, s.paths.Hooks, "%s: %v", s.paths.Hooks, err)
		}
		wantKey = codexStateKey(s.paths.Hooks, group, 0)
		wantHash = codexTrustHashes()[len(codexTrustHashes())-1]
		p.Ops = entryFileOps(p.Ops, s.env, s.dir, s.hooks, s.hooksBackup, renderJSONFile(top),
			"write the Sidecar session-identity hook entry, preserving every other hook", ownedEntry(CodexAssetVersion))
	}

	content, err := codexConfigConverge(s.configScan, wantKey, wantHash)
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	if content != nil {
		note := "enable features.hooks and record the hook's trusted_hash, preserving every other line"
		if s.configScan.hooksEnabled() {
			note = "record the hook's trusted_hash, preserving every other line"
		}
		// The directory op, if any, was already planned with hooks.json.
		dirPlanned := s.dir
		if len(p.Ops) > 0 {
			dirPlanned = FileState{Path: s.paths.Dir, Exists: true}
		}
		p.Ops = entryFileOps(p.Ops, s.env, dirPlanned, s.config, s.configBackup, content,
			note, ownedEntry(CodexAssetVersion))
	}

	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes Sidecar's hook entry and its trust records, and
// deliberately leaves features.hooks alone: other hooks in the same file may
// depend on it, and disabling a feature the user's other tools rely on is not
// an uninstall's business.
func (a CodexAdapter) planUninstall(s codexState, p Plan) (Plan, error) {
	// Nothing of Sidecar's is visible and both files read cleanly enough to
	// say so: there is nothing to do. An unreadable file that visibly holds
	// something of Sidecar's — or that cannot be ruled out while its sibling
	// does — refuses below instead.
	hooksClean := !s.hooks.Exists || (len(s.hooksScan.owned) == 0 && s.hooksScan.parseErr == "")
	if hooksClean && len(s.ownedTables) == 0 {
		p.Unchanged = true
		return p, nil
	}
	if err := refuseUnsafeEntryFile(s.dir, s.hooks, s.hooksScan); err != nil {
		return Plan{}, err
	}
	if s.config.Exists && s.config.Unsafe != "" {
		return Plan{}, refuse(s.config.Unsafe, s.paths.Config, "%s: %s", s.paths.Config, s.config.UnsafeDetail)
	}
	if s.configScan.parseErr != "" {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config,
			"%s could not be interpreted (%s); Sidecar will not edit a file it cannot read, and removing the hook while its trust records cannot be checked would leave them behind", s.paths.Config, s.configScan.parseErr)
	}

	if len(s.hooksScan.owned) > 0 {
		top, changed, err := stripOwnedHookEntries(s.hooksScan, s.spec)
		if err != nil {
			return Plan{}, refuse(RefuseUnreadable, s.paths.Hooks, "%s: %v", s.paths.Hooks, err)
		}
		if changed {
			p.Ops = append(p.Ops, removalOps(s.hooks, s.hooksBackup, top,
				"remove the Sidecar session-identity hook entry, preserving every other hook")...)
		}
	}
	if len(s.ownedTables) > 0 {
		content, err := codexConfigWithoutOwnedTables(s.configScan, s.wantKey)
		if err != nil {
			return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
		}
		// userEntry, not ownedEntry: this op takes Sidecar's trust records out,
		// so what it leaves behind is the user's own config.toml.
		p.Ops = entryFileOps(p.Ops, s.env, s.dir, s.config, s.configBackup, content,
			"remove Sidecar's hook trust records, preserving every other line including features.hooks", userEntry())
	}

	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusNotInstalled
	if s.providerPath == "" {
		p.StatusAfter = agentlifecycle.StatusProviderMissing
	}
	return p, nil
}

// sessionStartGroupCount reports how many groups hooks.SessionStart holds.
func sessionStartGroupCount(top []jsonMember) (int, error) {
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		return 0, nil
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		return 0, err
	}
	evIdx, ok := lastMember(events, "SessionStart")
	if !ok {
		return 0, nil
	}
	groups, err := parseJSONArray(events[evIdx].val)
	if err != nil {
		return 0, err
	}
	return len(groups), nil
}

// --- the TOML oracle: parsing, never serializing ---

// codexTOMLDoc parses TOML into a plain map.
//
// The parser is used for reading only. Nothing it produces is ever written
// back: config.toml is a user-owned file whose comments, key order and
// formatting must survive, and a round trip through a parsed document is
// exactly what would destroy them. Composition stays the line writer's job;
// this is only ever asked what a file means.
func codexTOMLDoc(b []byte) (map[string]any, error) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(b)) == 0 {
		return doc, nil
	}
	if err := toml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// tomlErrBrief renders a parser error as one short line fit for a refusal.
func tomlErrBrief(err error) string {
	s := strings.TrimSpace(err.Error())
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

// tomlEmptyTable marks a table that exists but holds nothing. A header like
// `[hooks.state]` with no keys under it is a fact about the file that a flat
// map of leaf values would otherwise lose, and losing it would let a rewrite
// delete a user's empty table unnoticed.
type tomlEmptyTable struct{}

// isBareTOMLKey reports whether a key can be written unquoted.
func isBareTOMLKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if !isBareKeyByte(k[i]) {
			return false
		}
	}
	return true
}

func isBareKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// tomlKeySegment renders one key segment the way TOML itself would: bare when
// it can be, quoted otherwise. That is what makes a dotted path unambiguous —
// a key literally named `a.b` renders as `"a.b"` and can never be confused
// with the nested pair `a`.`b`.
func tomlKeySegment(k string) string {
	if isBareTOMLKey(k) {
		return k
	}
	return strconv.Quote(k)
}

// tomlPathOf joins key segments into one canonical dotted path.
func tomlPathOf(segs ...string) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = tomlKeySegment(s)
	}
	return strings.Join(parts, ".")
}

// tomlFlattenDoc reduces a parsed document to one entry per leaf value, keyed
// by its canonical dotted path. Comparing two of these is what "the user's
// settings are untouched" means precisely enough to test.
func tomlFlattenDoc(doc map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range doc {
		tomlFlatten(v, tomlKeySegment(k), out)
	}
	return out
}

func tomlFlatten(v any, path string, out map[string]any) {
	if m, ok := v.(map[string]any); ok {
		if len(m) == 0 {
			out[path] = tomlEmptyTable{}
			return
		}
		for k, child := range m {
			tomlFlatten(child, path+"."+tomlKeySegment(k), out)
		}
		return
	}
	out[path] = v
}

func sortedPaths(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// codexOwnsPath answers whether a leaf path is inside the region Sidecar is
// allowed to change: the features.hooks flag, and everything under each trust
// table it owns. The three container tables those live in are exempt only when
// they are empty, because creating or removing a trust table necessarily
// creates or empties its parents.
func codexOwnsPath(ownedKeys []string) func(string, any) bool {
	flag := tomlPathOf("features", "hooks")
	containers := map[string]bool{
		tomlPathOf("features"):       true,
		tomlPathOf("hooks"):          true,
		tomlPathOf("hooks", "state"): true,
	}
	prefixes := make([]string, 0, len(ownedKeys))
	for _, k := range ownedKeys {
		if k == "" {
			continue
		}
		prefixes = append(prefixes, tomlPathOf("hooks", "state", k))
	}
	return func(path string, val any) bool {
		if path == flag {
			return true
		}
		for _, p := range prefixes {
			if path == p || strings.HasPrefix(path, p+".") {
				return true
			}
		}
		if _, empty := val.(tomlEmptyTable); empty && containers[path] {
			return true
		}
		return false
	}
}

// codexTrustPathPresent reports whether anything under one trust table's key is
// still present in a flattened document.
func codexTrustPathPresent(flat map[string]any, key string) bool {
	p := tomlPathOf("hooks", "state", key)
	for _, have := range sortedPaths(flat) {
		if have == p || strings.HasPrefix(have, p+".") {
			return true
		}
	}
	return false
}

// codexOracleDiff proves that a composed image differs from the pre-image only
// inside the region Sidecar owns: nothing the user wrote was dropped, changed,
// or joined by something new.
func codexOracleDiff(pre, post []byte, ownedKeys []string) error {
	preDoc, err := codexTOMLDoc(pre)
	if err != nil {
		return fmt.Errorf("the file it started from is not valid TOML: %s", tomlErrBrief(err))
	}
	postDoc, err := codexTOMLDoc(post)
	if err != nil {
		return fmt.Errorf("the composed file is not valid TOML: %s", tomlErrBrief(err))
	}
	preFlat, postFlat := tomlFlattenDoc(preDoc), tomlFlattenDoc(postDoc)
	owns := codexOwnsPath(ownedKeys)
	for _, p := range sortedPaths(preFlat) {
		if owns(p, preFlat[p]) {
			continue
		}
		got, ok := postFlat[p]
		if !ok {
			return fmt.Errorf("it would drop %s, which Sidecar does not own", p)
		}
		if !reflect.DeepEqual(preFlat[p], got) {
			return fmt.Errorf("it would change the value of %s, which Sidecar does not own", p)
		}
	}
	for _, p := range sortedPaths(postFlat) {
		if owns(p, postFlat[p]) {
			continue
		}
		if _, ok := preFlat[p]; !ok {
			return fmt.Errorf("it would add %s, which is outside the region Sidecar owns", p)
		}
	}
	return nil
}

// codexOracleConverged checks the install/update/repair intent on top of the
// diff: the feature flag on, exactly Sidecar's trust record recorded, and no
// stale record of Sidecar's own left behind.
func codexOracleConverged(pre, post []byte, ownedKeys []string, wantKey, wantHash string) error {
	if err := codexOracleDiff(pre, post, ownedKeys); err != nil {
		return err
	}
	doc, err := codexTOMLDoc(post)
	if err != nil {
		return fmt.Errorf("the composed file is not valid TOML: %s", tomlErrBrief(err))
	}
	flat := tomlFlattenDoc(doc)
	if v, ok := flat[tomlPathOf("features", "hooks")]; !ok || v != true {
		return fmt.Errorf("features.hooks would not be true")
	}
	if v, ok := flat[tomlPathOf("hooks", "state", wantKey, "trusted_hash")]; !ok || v != any(wantHash) {
		return fmt.Errorf("the Sidecar trusted_hash would not be recorded at %s", tomlPathOf("hooks", "state", wantKey))
	}
	for _, k := range ownedKeys {
		if k == wantKey {
			continue
		}
		if codexTrustPathPresent(flat, k) {
			return fmt.Errorf("a stale Sidecar trust record %s would survive", tomlPathOf("hooks", "state", k))
		}
	}
	return nil
}

// codexOracleRemoved checks the uninstall intent on top of the diff: every
// trust record of Sidecar's gone, and features.hooks exactly as it was found —
// other hooks may depend on it, and turning it off is not an uninstall's
// business.
func codexOracleRemoved(pre, post []byte, ownedKeys []string) error {
	if err := codexOracleDiff(pre, post, ownedKeys); err != nil {
		return err
	}
	preDoc, err := codexTOMLDoc(pre)
	if err != nil {
		return fmt.Errorf("the file it started from is not valid TOML: %s", tomlErrBrief(err))
	}
	postDoc, err := codexTOMLDoc(post)
	if err != nil {
		return fmt.Errorf("the composed file is not valid TOML: %s", tomlErrBrief(err))
	}
	preFlat, postFlat := tomlFlattenDoc(preDoc), tomlFlattenDoc(postDoc)
	flag := tomlPathOf("features", "hooks")
	if !reflect.DeepEqual(preFlat[flag], postFlat[flag]) {
		return fmt.Errorf("it would change features.hooks, which uninstall must leave alone")
	}
	for _, k := range ownedKeys {
		if codexTrustPathPresent(postFlat, k) {
			return fmt.Errorf("the Sidecar trust record %s would survive", tomlPathOf("hooks", "state", k))
		}
	}
	return nil
}

// --- the line-surgical config.toml editor ---

// tomlStateTable is one [hooks.state."<key>"] table: its unquoted key, its
// trusted_hash value, and the exact line span it occupies.
//
// The span rule: start is the header line, end is the LAST line that belongs to
// the table — its header, or its last key line, or the last line of a value
// that ran over several lines. Blank lines and comments that trail the table
// belong to the FILE, not to the table, and are never part of the span. That
// asymmetry is deliberate. A comment between two keys inside the table is that
// table's; a comment parked after the last key is the user's note about what
// comes next, or about nothing at all, and Sidecar always appends its own table
// at the end of the file — so a span that ran to the next header or to EOF
// would delete whatever the user parked at the end of their config on
// uninstall. Interior trivia is dropped with the table; trailing trivia is kept.
type tomlStateTable struct {
	key   string
	hash  string
	start int
	end   int
}

// codexConfigScan is one reading of config.toml, precise about exactly the two
// regions Sidecar edits and deliberately ignorant of everything else.
type codexConfigScan struct {
	exists bool
	// raw is the file as read, kept so a composed image can be semantically
	// diffed against what it started from.
	raw   []byte
	lines []string
	// features is the [features] header line, -1 when absent.
	features int
	// hooksFlag is the parsed features.hooks value, nil when the key is absent.
	hooksFlag     *bool
	hooksFlagLine int
	state         []tomlStateTable
	// parseErr names why the file cannot be safely interpreted or edited in
	// the regions Sidecar cares about. Refusing on it is the whole safety
	// story of a line-level editor: a spelling this scanner does not
	// understand is a spelling it must not edit around.
	parseErr string
}

func (s codexConfigScan) hooksEnabled() bool {
	return s.hooksFlag != nil && *s.hooksFlag
}

func scanCodexConfig(exists bool, b []byte) codexConfigScan {
	s := codexConfigScan{exists: exists, features: -1, hooksFlagLine: -1}
	if !exists {
		return s
	}
	s.raw = append([]byte(nil), b...)

	// The oracle runs first. Sidecar will not edit a file it cannot fully
	// understand, and everything below is a line scanner whose reading of an
	// invalid file would be a guess.
	if _, err := codexTOMLDoc(b); err != nil {
		s.parseErr = "the file is not valid TOML: " + tomlErrBrief(err)
		return s
	}

	content := string(b)
	if line, found := multilineStringOpener(content); found {
		// Once a multi-line string opens, every following line's structure is
		// unknowable to a line scanner: the body can contain text that looks
		// exactly like a table header. Refuse rather than guess.
		s.parseErr = "the file contains a multi-line string (line " + strconv.Itoa(line) + "), which this editor does not support"
		return s
	}
	s.lines = strings.Split(content, "\n")

	var tableSegs []string
	open := -1
	// lastContent is the last line seen that belongs to the current table; see
	// the span rule on tomlStateTable.
	lastContent := -1
	// depth is the inline bracket/brace nesting carried across lines, so an
	// element of a multi-line array is never mistaken for a table header.
	depth := 0

	closeOpen := func() {
		if open >= 0 {
			end := lastContent
			if end < s.state[open].start {
				end = s.state[open].start
			}
			s.state[open].end = end
			open = -1
		}
	}

	for i, rawLine := range s.lines {
		if depth > 0 {
			// A continuation of a value that opened on an earlier line. It is
			// never a header and never a key, whatever it looks like.
			d, ok := tomlValueDepth(rawLine, depth)
			if !ok {
				s.parseErr = fmt.Sprintf("line %d continues a value this editor cannot follow", i+1)
				return s
			}
			depth = d
			lastContent = i
			continue
		}

		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") {
			closeOpen()
			segs, array, ok := tomlHeaderPath(line)
			if !ok {
				s.parseErr = fmt.Sprintf("line %d is a table header this editor cannot parse", i+1)
				return s
			}
			if array {
				if segs[0] == "features" || segs[0] == "hooks" {
					s.parseErr = "features or hooks are configured as arrays of tables, which this editor does not edit"
					return s
				}
				// The segments are recorded so the keys below read as being
				// inside a table rather than at the document root; an array of
				// tables named features or hooks was already refused above, so
				// nothing Sidecar edits can be reached from here.
				tableSegs = segs
				lastContent = i
				continue
			}
			tableSegs = segs
			switch {
			case len(segs) == 1 && segs[0] == "features":
				s.features = i
			case len(segs) >= 2 && segs[0] == "features" && segs[1] == "hooks":
				// features.hooks is a table here, not the boolean flag. Writing
				// the flag would be writing a duplicate key.
				s.parseErr = "the hooks feature flag is configured in a form this editor does not edit"
				return s
			case len(segs) == 3 && segs[0] == "hooks" && segs[1] == "state":
				s.state = append(s.state, tomlStateTable{key: segs[2], start: i, end: i})
				open = len(s.state) - 1
			case len(segs) > 3 && segs[0] == "hooks" && segs[1] == "state":
				s.parseErr = fmt.Sprintf("line %d nests a table below a hook trust record, which this editor does not edit", i+1)
				return s
			}
			lastContent = i
			continue
		}

		segs, rest, ok := tomlKeySegments(line)
		if ok {
			rest = strings.TrimLeft(rest, " \t")
			ok = strings.HasPrefix(rest, "=")
		}
		if !ok {
			// A line the scanner cannot normalise. It refuses rather than
			// skipping: an unread line in the regions Sidecar edits is exactly
			// how a duplicate [features] table gets appended to a file that
			// already had one.
			s.parseErr = fmt.Sprintf("line %d is written in a form this editor cannot read", i+1)
			return s
		}
		val := strings.TrimSpace(rest[1:])
		d, dok := tomlValueDepth(val, 0)
		if !dok {
			s.parseErr = fmt.Sprintf("line %d has a value this editor cannot follow", i+1)
			return s
		}
		depth = d
		lastContent = i

		inFeatures := len(tableSegs) == 1 && tableSegs[0] == "features"
		switch {
		case inFeatures && len(segs) == 1 && segs[0] == "hooks":
			switch tomlBareValue(val) {
			case "true":
				v := true
				s.hooksFlag, s.hooksFlagLine = &v, i
			case "false":
				v := false
				s.hooksFlag, s.hooksFlagLine = &v, i
			default:
				s.parseErr = "features.hooks has a value this editor does not understand"
				return s
			}
		case inFeatures && segs[0] == "hooks":
			s.parseErr = "the hooks feature flag is configured in a form this editor does not edit"
			return s
		case len(tableSegs) == 0 && segs[0] == "features":
			s.parseErr = "the hooks feature flag is configured in a form this editor does not edit"
			return s
		case len(tableSegs) == 0 && segs[0] == "hooks":
			s.parseErr = "hook trust state is configured in a form this editor does not edit"
			return s
		case len(tableSegs) == 1 && tableSegs[0] == "hooks" && segs[0] == "state":
			s.parseErr = "hook trust state is configured in a form this editor does not edit"
			return s
		case len(tableSegs) == 2 && tableSegs[0] == "hooks" && tableSegs[1] == "state":
			s.parseErr = "hook trust state is configured in a form this editor does not edit"
			return s
		case open >= 0 && len(segs) == 1 && segs[0] == "trusted_hash":
			s.state[open].hash = tomlQuotedValue(val)
		}
	}
	if depth > 0 {
		s.parseErr = "the file ends inside a value this editor cannot follow"
		return s
	}
	closeOpen()
	return s
}

// tomlHeaderPath parses a `[name]` or `[[name]]` line into its normalised key
// segments, tolerating a trailing comment. Quoted segments are unquoted and
// whitespace around the dots is dropped, so `[ hooks . state . "k" ]` and
// `[hooks.state."k"]` read as the same three segments and `["features"]` reads
// as `features`.
func tomlHeaderPath(line string) (segs []string, array bool, ok bool) {
	rest := line
	switch {
	case strings.HasPrefix(rest, "[["):
		array, rest = true, rest[2:]
	case strings.HasPrefix(rest, "["):
		rest = rest[1:]
	default:
		return nil, false, false
	}
	segs, rest, ok = tomlKeySegments(rest)
	if !ok {
		return nil, false, false
	}
	closer := "]"
	if array {
		closer = "]]"
	}
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, closer) {
		return nil, false, false
	}
	rest = strings.TrimSpace(rest[len(closer):])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return nil, false, false
	}
	return segs, array, true
}

// tomlKeySegments reads a dotted TOML key off the front of s and returns its
// normalised segments plus whatever follows. A quoted segment is unquoted; a
// quoted segment carrying an escape is refused, because guessing at what it
// spells is exactly the class of guess this editor does not make.
func tomlKeySegments(s string) (segs []string, rest string, ok bool) {
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return nil, "", false
		}
		var seg string
		switch s[0] {
		case '"', '\'':
			quote := s[0]
			end := strings.IndexByte(s[1:], quote)
			if end < 0 {
				return nil, "", false
			}
			seg = s[1 : 1+end]
			if strings.ContainsRune(seg, '\\') {
				return nil, "", false
			}
			s = s[2+end:]
		default:
			i := 0
			for i < len(s) && isBareKeyByte(s[i]) {
				i++
			}
			if i == 0 {
				return nil, "", false
			}
			seg, s = s[:i], s[i:]
		}
		segs = append(segs, seg)
		s = strings.TrimLeft(s, " \t")
		if strings.HasPrefix(s, ".") {
			s = s[1:]
			continue
		}
		return segs, s, true
	}
}

// tomlValueDepth walks a value fragment and returns the bracket and brace
// nesting depth after it, ignoring anything inside a string or a comment. It is
// what stops `  [20, 30]` — one element of a multi-line array — from being read
// as a table header. It reports false for a fragment it cannot walk.
func tomlValueDepth(s string, depth int) (int, bool) {
	for i := 0; i < len(s); {
		switch s[i] {
		case '#':
			return depth, true
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return 0, false
			}
			i += end + 2
		case '"':
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					break
				}
				j++
			}
			if j >= len(s) {
				return 0, false
			}
			i = j + 1
		case '[', '{':
			depth++
			i++
		case ']', '}':
			depth--
			if depth < 0 {
				return 0, false
			}
			i++
		default:
			i++
		}
	}
	return depth, true
}

// tomlBareValue strips a trailing comment from an unquoted value.
func tomlBareValue(val string) string {
	if i := strings.Index(val, "#"); i >= 0 {
		val = val[:i]
	}
	return strings.TrimSpace(val)
}

// tomlQuotedValue extracts a basic-string value, tolerating a trailing
// comment. A value with escapes or an unexpected shape reads as "", which is
// owned by nobody and therefore never touched.
func tomlQuotedValue(val string) string {
	if len(val) < 2 || val[0] != '"' {
		return ""
	}
	closing := strings.Index(val[1:], `"`)
	if closing < 0 {
		return ""
	}
	inner := val[1 : 1+closing]
	rest := strings.TrimSpace(val[2+closing:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return ""
	}
	if strings.ContainsRune(inner, '\\') {
		return ""
	}
	return inner
}

// codexTableKeys lists the keys of the given trust tables, plus extras, with no
// repeats.
func codexTableKeys(tables []tomlStateTable, extra ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	for _, t := range tables {
		add(t.key)
	}
	for _, k := range extra {
		add(k)
	}
	return out
}

// codexConfigConverge composes the config.toml content that enables the
// feature flag and records exactly one trust table for wantKey/wantHash,
// dropping Sidecar's stale trust tables and changing nothing else. It returns
// nil when the file already says all of that.
func codexConfigConverge(scan codexConfigScan, wantKey, wantHash string) ([]byte, error) {
	if scan.parseErr != "" {
		return nil, fmt.Errorf("%s", scan.parseErr)
	}
	if wantKey == "" || wantHash == "" {
		return nil, fmt.Errorf("the hook's trust key could not be determined")
	}
	converged := scan.hooksEnabled()
	owned := codexOwnedTables(scan, wantKey)
	if converged && len(owned) == 1 && owned[0].key == wantKey && owned[0].hash == wantHash {
		return nil, nil
	}
	ownedKeys := codexTableKeys(owned, wantKey)

	if !scan.exists || len(scan.lines) == 0 {
		content := "[features]\nhooks = true\n\n" + codexStateBlock(wantKey, wantHash)
		return codexVerifyConverged(scan.raw, []byte(content), ownedKeys, wantKey, wantHash)
	}

	drop := lineDropSet(scan, owned)
	var kept []string
	for i, line := range scan.lines {
		if drop[i] {
			continue
		}
		if i == scan.hooksFlagLine && !scan.hooksEnabled() {
			kept = append(kept, "hooks = true")
			continue
		}
		if i == scan.features && scan.hooksFlag == nil {
			kept = append(kept, line, "hooks = true")
			continue
		}
		kept = append(kept, line)
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if scan.features == -1 && scan.hooksFlag == nil {
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		kept = append(kept, "[features]", "hooks = true")
	}
	if len(kept) > 0 {
		kept = append(kept, "")
	}
	kept = append(kept, strings.TrimSuffix(codexStateBlock(wantKey, wantHash), "\n"))
	return codexVerifyConverged(scan.raw, []byte(strings.Join(kept, "\n")+"\n"), ownedKeys, wantKey, wantHash)
}

// codexConfigWithoutOwnedTables composes the uninstall content: Sidecar's
// trust tables gone, every other line — including features.hooks — untouched.
func codexConfigWithoutOwnedTables(scan codexConfigScan, wantKey string) ([]byte, error) {
	if scan.parseErr != "" {
		return nil, fmt.Errorf("%s", scan.parseErr)
	}
	owned := codexOwnedTables(scan, wantKey)
	if len(owned) == 0 {
		return nil, nil
	}
	drop := lineDropSet(scan, owned)
	var kept []string
	for i, line := range scan.lines {
		if drop[i] {
			continue
		}
		kept = append(kept, line)
	}
	content := strings.Join(kept, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return codexVerifyRemoved(scan.raw, []byte(content), codexTableKeys(owned), wantKey)
}

// lineDropSet marks the line spans of the given tables, plus one immediately
// preceding blank line each, for removal. The span is inclusive of its end line
// and ends before any trailing blank or comment lines — see tomlStateTable.
func lineDropSet(scan codexConfigScan, tables []tomlStateTable) map[int]bool {
	drop := map[int]bool{}
	for _, t := range tables {
		for i := t.start; i <= t.end && i < len(scan.lines); i++ {
			drop[i] = true
		}
		if t.start > 0 && strings.TrimSpace(scan.lines[t.start-1]) == "" {
			drop[t.start-1] = true
		}
	}
	return drop
}

func codexStateBlock(key, hash string) string {
	return "[hooks.state." + `"` + key + `"` + "]\ntrusted_hash = \"" + hash + "\"\n"
}

// codexVerifyConverged proves a composed install/update/repair image before it
// is allowed into a plan: the line scanner reads back exactly what was
// intended, and the TOML oracle confirms that nothing outside Sidecar's region
// moved. A line editor earns trust by checking its own work against a parser
// that does not share its blind spots, not by being clever.
func codexVerifyConverged(pre, content []byte, ownedKeys []string, wantKey, wantHash string) ([]byte, error) {
	scan := scanCodexConfig(true, content)
	if scan.parseErr != "" {
		return nil, fmt.Errorf("the composed file did not verify (%s); refusing to write it", scan.parseErr)
	}
	if !scan.hooksEnabled() {
		return nil, fmt.Errorf("the composed file did not enable features.hooks; refusing to write it")
	}
	owned := codexOwnedTables(scan, wantKey)
	if len(owned) != 1 || owned[0].key != wantKey || owned[0].hash != wantHash {
		return nil, fmt.Errorf("the composed file did not record exactly one trust table; refusing to write it")
	}
	if err := codexOracleConverged(pre, content, ownedKeys, wantKey, wantHash); err != nil {
		return nil, fmt.Errorf("the composed file did not verify against the original (%v); refusing to write it", err)
	}
	return content, nil
}

// codexVerifyRemoved is the same gate for an uninstall image.
func codexVerifyRemoved(pre, content []byte, ownedKeys []string, wantKey string) ([]byte, error) {
	scan := scanCodexConfig(true, content)
	if scan.parseErr != "" {
		return nil, fmt.Errorf("the composed file did not verify (%s); refusing to write it", scan.parseErr)
	}
	if len(codexOwnedTables(scan, wantKey)) != 0 {
		return nil, fmt.Errorf("the composed file did not verify; refusing to write it")
	}
	if err := codexOracleRemoved(pre, content, ownedKeys); err != nil {
		return nil, fmt.Errorf("the composed file did not verify against the original (%v); refusing to write it", err)
	}
	return content, nil
}

var _ Adapter = CodexAdapter{}

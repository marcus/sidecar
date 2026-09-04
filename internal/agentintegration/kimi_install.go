package agentintegration

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Kimi adapter.
//
// Kimi Code CLI reads its hooks from one TOML file the user owns,
// ~/.kimi-code/config.toml, which also holds their model, provider credentials
// and everything else. So Sidecar owns an entry in a file rather than a file
// (Ownership OwnsEntry), and the whole installer is about touching exactly one
// region of it and nothing else.
//
// # Why a fenced block rather than per-key surgery
//
// Codex's config.toml editor is line-surgical because the two things Sidecar
// must write there -- `features.hooks = true` and one `[hooks.state."key"]`
// table -- live at fixed paths the user may already have opinions about. Kimi's
// twelve entries are elements of a root-level `[[hooks]]` array of tables, and
// they are entirely Sidecar's: there is no key to merge with, only entries to
// add. A contiguous region delimited by two marker comments is therefore both
// simpler and stricter, because the region is identified by the marker rather
// than by matching a shape:
//
//   - Nothing outside the two marker lines is ever read for meaning, moved, or
//     rewritten. A TOML array of tables can be appended to at end of file with
//     no effect on any table above it, which is why the block goes there.
//   - Uninstall removes exactly the lines between the markers, which is exactly
//     what "only ever remove what carries the sidecar-integration: marker"
//     means for a file the user owns.
//   - A hook of the user's *inside* the block, or a hook invoking Sidecar's
//     kimi source *outside* it, both read as needs-repair and refuse the
//     mutation rather than being silently deleted or silently duplicated.
//     Herdr's own uninstall deletes its block without either check; Sidecar's
//     ownership rule is stricter and does not copy that.
//
// The line editor is held to the same contract Codex's is: a TOML parser is used
// as a read-only oracle at both ends of every edit, never to serialize anything.
// A file that is not valid TOML is refused outright, and a composed image is
// semantically diffed against its pre-image before a single operation is
// emitted, so a rewrite that would not verify produces a refusal with an empty
// op list rather than a partial change on disk.

// The two marker lines that delimit Sidecar's region of config.toml.
//
// Both carry markerToken, which is the ownership sentinel every Sidecar-owned
// region in every provider tree carries; only the comment character differs,
// because TOML has no `//`. They are distinguished by a `block=` field rather
// than by two different prefixes, so there is one marker syntax and one parser.
const (
	kimiMarkerPrefix = "# " + markerToken
	kimiBlockBegin   = "begin"
	kimiBlockEnd     = "end"
)

// KimiAdapter installs Sidecar's Kimi lifecycle hooks.
type KimiAdapter struct{}

func (KimiAdapter) Provider() string { return KimiProvider }
func (KimiAdapter) Source() string   { return KimiSource }

// Assets returns the single entry asset this integration installs.
//
// Content is the canonical block Sidecar would write into an empty tree, which
// for an OwnsEntry asset is a description of the entry rather than something
// ever written verbatim over a user's file.
func (KimiAdapter) Assets() []Asset {
	return []Asset{{
		Name:          KimiConfigName,
		Source:        KimiSource,
		SchemaVersion: KimiAssetSchema,
		Version:       KimiAssetVersion,
		Ownership:     OwnsEntry,
		Content:       kimiBlock(),
	}}
}

func (a KimiAdapter) asset() Asset { return a.Assets()[0] }

// kimiMarkerLine renders one of the two marker comments.
func kimiMarkerLine(which, version string) string {
	return fmt.Sprintf("%s id=%s schema=%d version=%s block=%s",
		kimiMarkerPrefix, KimiSource, KimiAssetSchema, version, which)
}

// parseKimiMarker reads a marker comment, reporting the identity it declares.
//
// It is deliberately strict about the identity: a marker for another source, or
// one at a schema this build does not understand, is not Sidecar's block here
// and is left exactly where it is.
func parseKimiMarker(line string) (version, which string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), kimiMarkerPrefix)
	if !found {
		return "", "", false
	}
	var id string
	schema := -1
	for _, field := range strings.Fields(rest) {
		key, value, split := strings.Cut(field, "=")
		if !split {
			continue
		}
		switch key {
		case "id":
			id = value
		case "schema":
			n, err := strconv.Atoi(value)
			if err != nil {
				return "", "", false
			}
			schema = n
		case "version":
			version = value
		case "block":
			which = value
		}
	}
	if id != KimiSource || schema != KimiAssetSchema {
		return "", "", false
	}
	if which != kimiBlockBegin && which != kimiBlockEnd {
		return "", "", false
	}
	return version, which, true
}

// kimiBlock renders the exact region this build installs, ending in a newline.
//
// Every hook carries its `Why` as a comment. A user reading their own
// config.toml is entitled to know what the twelve entries a tool added to it
// mean without going and reading the tool's source.
func kimiBlock() string {
	var b strings.Builder
	b.WriteString(kimiMarkerLine(kimiBlockBegin, KimiAssetVersion))
	b.WriteString("\n")
	b.WriteString("# Managed by Sidecar. `sidecar agent integration update kimi` replaces this\n")
	b.WriteString("# block and `sidecar agent integration uninstall kimi` removes it. Everything\n")
	b.WriteString("# outside the two marker lines is yours: Sidecar never moves or rewrites it.\n")
	b.WriteString("# Put your own hooks outside the block. One added inside it makes the\n")
	b.WriteString("# integration read as needs-repair rather than being quietly deleted.\n")
	for _, h := range kimiHooks {
		b.WriteString("\n# ")
		b.WriteString(h.Why)
		b.WriteString("\n[[hooks]]\nevent = ")
		b.WriteString(tomlBasicString(h.Event))
		b.WriteString("\n")
		if h.Matcher != "" {
			b.WriteString("matcher = ")
			b.WriteString(tomlBasicString(h.Matcher))
			b.WriteString("\n")
		}
		b.WriteString("command = ")
		b.WriteString(tomlBasicString(KimiHookCommand(h)))
		b.WriteString("\ntimeout = ")
		b.WriteString(strconv.Itoa(hookTimeoutSec))
		b.WriteString("\n")
	}
	b.WriteString(kimiMarkerLine(kimiBlockEnd, KimiAssetVersion))
	b.WriteString("\n")
	return b.String()
}

// tomlBasicString encodes a value as a TOML basic string.
//
// It is the same encoding Herdr's toml_basic_string writes, and it is spelled
// out rather than taken from a marshaller for the reason the whole editor
// exists: nothing here ever serializes a parsed document, so there is no
// document to ask.
func tomlBasicString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r <= 0x1f || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// kimiPaths are the exact user-level paths this adapter inspects.
type kimiPaths struct {
	Dir    string
	Config string
	Backup string
}

// KimiPaths returns the paths the Kimi adapter would inspect and touch.
func KimiPaths(env Env) []string { return []string{kimiPathsFor(env).Config} }

func kimiPathsFor(env Env) kimiPaths {
	dir := kimiHomeDir(env.Home, env.KimiCodeHome)
	return kimiPaths{
		Dir:    dir,
		Config: filepath.Join(dir, KimiConfigName),
		Backup: filepath.Join(dir, KimiConfigName+KimiBackupSuffix),
	}
}

// kimiHomeDir resolves Kimi Code CLI's data directory the way Kimi itself
// documents it: $KIMI_CODE_HOME when set, and ~/.kimi-code otherwise.
//
// The tilde expansion matches Herdr's expand_tilde_path, and it matters for the
// same reason Pi's does: a Sidecar that did not expand would read and write a
// literal directory named "~" while the provider used somewhere else entirely.
// A whitespace-only value is a variable somebody exported without a value, not
// a directory named " ".
func kimiHomeDir(home, override string) string {
	value := strings.TrimSpace(override)
	if value == "" {
		if home == "" {
			return ""
		}
		return filepath.Join(home, ".kimi-code")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		if home == "" {
			return ""
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(value, "~"), "/"))
	}
	return value
}

// kimiNeverSetUp is the one sentence for "kimi's data directory is not there".
//
// It is a function because the same fact has to reach a user through two
// surfaces: the refusal a caller gets from Plan, and the message on a status
// that offers no install. A status that stayed silent while the refusal
// explained itself is how a missing action looks like a bug.
func kimiNeverSetUp(dir string) string {
	return "kimi's data directory " + dir + " does not exist, so kimi code has not been set up on this machine; " +
		"run kimi once (or set KIMI_CODE_HOME) and try again"
}

// kimiConfigScan is one reading of config.toml: where Sidecar's block is, what
// it holds, and whether anything of Sidecar's has escaped it.
type kimiConfigScan struct {
	exists bool
	raw    []byte
	lines  []string

	// begin and end are the 0-based line numbers of the two marker comments,
	// or -1 when there is no block.
	begin int
	end   int
	// version is the asset version the begin marker declares.
	version string
	// text is the block's exact bytes, including both markers and a trailing
	// newline, so it can be compared with kimiBlock() directly.
	text string

	// ours counts the hook entries in the whole file that invoke Sidecar's kimi
	// source; inside counts the ones inside the block; foreign counts entries
	// inside the block that are not Sidecar's.
	ours    int
	inside  int
	foreign int

	// parseErr names why the file cannot be safely interpreted or edited.
	// Empty means the scan is trustworthy.
	parseErr string
}

func (s kimiConfigScan) hasBlock() bool { return s.begin >= 0 && s.end >= 0 }

// stray reports hook entries invoking Sidecar's kimi source that are not inside
// Sidecar's block. They are Sidecar's by command and not Sidecar's by placement,
// which is exactly the case that must refuse rather than be tidied up: every one
// of them reports independently, so leaving one behind doubles every event, and
// deleting it would be editing a region Sidecar does not own.
func (s kimiConfigScan) stray() int { return s.ours - s.inside }

// scanKimiConfig reads config.toml, honouring the file inspection that produced
// file.
func scanKimiConfig(file FileState) kimiConfigScan {
	s := kimiConfigScan{begin: -1, end: -1}
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

	// The oracle runs first. Sidecar will not edit a file it cannot fully
	// understand, and everything below is a line scanner whose reading of an
	// invalid file would be a guess.
	if _, err := codexTOMLDoc(raw); err != nil {
		s.parseErr = "the file is not valid TOML: " + tomlErrBrief(err)
		return s
	}
	content := string(raw)
	if line, found := multilineStringOpener(content); found {
		// Once a multi-line string opens, a line scanner cannot tell structure
		// from string body, and the body can contain text that looks exactly
		// like a marker comment. Refuse rather than guess.
		s.parseErr = "the file contains a multi-line string (line " + strconv.Itoa(line) +
			"), which this editor does not support"
		return s
	}
	s.lines = strings.Split(content, "\n")

	for i, line := range s.lines {
		version, which, ok := parseKimiMarker(line)
		if !ok {
			continue
		}
		switch which {
		case kimiBlockBegin:
			if s.begin >= 0 {
				s.parseErr = "config.toml carries more than one Sidecar block begin marker, so which lines are Sidecar's is unknowable"
				return s
			}
			if s.end >= 0 {
				s.parseErr = "config.toml carries a Sidecar block end marker before its begin marker"
				return s
			}
			s.begin, s.version = i, version
		case kimiBlockEnd:
			if s.begin < 0 {
				s.parseErr = "config.toml carries a Sidecar block end marker before its begin marker"
				return s
			}
			if s.end >= 0 {
				s.parseErr = "config.toml carries more than one Sidecar block end marker"
				return s
			}
			s.end = i
		}
	}
	if s.begin >= 0 && s.end < 0 {
		s.parseErr = "config.toml carries a Sidecar block begin marker with no end marker, so where Sidecar's region stops is unknowable"
		return s
	}

	s.ours = kimiOwnedHookCount(raw)
	if s.hasBlock() {
		s.text = strings.Join(s.lines[s.begin:s.end+1], "\n") + "\n"
		total, ours, err := kimiHookCounts([]byte(s.text))
		if err != nil {
			// The block's own lines do not parse as TOML on their own, which
			// means the region is damaged in a way only a human can resolve.
			s.parseErr = "Sidecar's own block is not valid TOML: " + tomlErrBrief(err)
			return s
		}
		s.inside, s.foreign = ours, total-ours
	}
	return s
}

// kimiOwnedHookCount counts hook entries in a whole config that invoke
// Sidecar's kimi source.
func kimiOwnedHookCount(raw []byte) int {
	_, ours, err := kimiHookCounts(raw)
	if err != nil {
		return 0
	}
	return ours
}

// kimiHookCounts parses a TOML fragment and reports how many `[[hooks]]` entries
// it holds and how many of those are Sidecar's.
func kimiHookCounts(raw []byte) (total, ours int, err error) {
	doc, err := codexTOMLDoc(raw)
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range kimiHookEntries(doc) {
		total++
		if command, _ := entry["command"].(string); invokesKimiReport(command) {
			ours++
		}
	}
	return total, ours, nil
}

// kimiHookEntries returns the `[[hooks]]` tables of a parsed document. Anything
// at that key that is not an array of tables yields nothing, which is the safe
// reading: Sidecar then believes it owns none of it.
func kimiHookEntries(doc map[string]any) []map[string]any {
	items, ok := doc["hooks"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// kimiState is everything one inspection learned. Both [KimiAdapter.Inspect] and
// [KimiAdapter.Plan] are built from it, so a plan can never rest on a different
// reading of the disk than the status the user was shown.
type kimiState struct {
	env    Env
	paths  kimiPaths
	asset  Asset
	dir    FileState
	config FileState
	backup FileState
	scan   kimiConfigScan

	providerPath    string
	providerVersion string

	assetStatus agentlifecycle.IntegrationStatus
	status      agentlifecycle.IntegrationStatus
	message     string
	installed   string
}

func (a KimiAdapter) inspect(env Env) kimiState {
	p := kimiPathsFor(env)
	s := kimiState{
		env:    env,
		paths:  p,
		asset:  a.asset(),
		dir:    inspectDir(env, p.Dir),
		config: inspectFile(env, p.Config, a.asset()),
		backup: FileState{Path: p.Backup, Exists: fileExists(p.Backup)},
	}
	s.scan = scanKimiConfig(s.config)
	if s.scan.hasBlock() {
		ownEntry(&s.config, s.scan.version)
		s.installed = s.scan.version
	}
	if path, ok := env.lookPath(KimiProvider); ok {
		s.providerPath = path
		s.providerVersion = env.providerVersion(KimiProvider)
	}
	s.assetStatus, s.message = kimiAssetStatus(s)

	s.status = s.assetStatus
	if s.providerPath == "" {
		// The provider CLI being absent is the more actionable of the two true
		// statements, and it is the one that decides authority: with no kimi
		// there is nothing to run the hooks, so TierFor is right to return
		// screen fallback. The block's own state is still in the message and in
		// Files, so an uninstall after removing the provider stays discoverable.
		s.status = agentlifecycle.StatusProviderMissing
		s.message = "the kimi CLI was not found on PATH" + orEmpty("; "+s.message, s.message != "")
	}
	return s
}

// kimiAssetStatus decides the status from the inspected file alone.
//
// Nothing here trusts the version the marker claims. The block's bytes are
// compared with the block this build renders, so a hand-edited, truncated or
// partially applied block reads as needs-repair rather than as current.
func kimiAssetStatus(s kimiState) (agentlifecycle.IntegrationStatus, string) {
	switch {
	case s.dir.Exists && s.dir.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.dir.UnsafeDetail + " (" + s.paths.Dir + ")"
	case s.config.Exists && s.config.Unsafe != "":
		return agentlifecycle.StatusNeedsRepair, s.config.UnsafeDetail + " (" + s.paths.Config + ")"
	case s.scan.parseErr != "":
		return agentlifecycle.StatusNeedsRepair,
			KimiConfigName + " could not be interpreted (" + s.scan.parseErr +
				"), so the integration state is unknown; Sidecar will not modify the file"
	case s.scan.foreign > 0:
		return agentlifecycle.StatusNeedsRepair,
			strconv.Itoa(s.scan.foreign) + " hook entries that are not Sidecar's sit inside Sidecar's managed block in " +
				KimiConfigName + "; move them outside the marker comments yourself and run this again"
	case s.scan.stray() > 0:
		return agentlifecycle.StatusNeedsRepair,
			strconv.Itoa(s.scan.stray()) + " hook entries invoking Sidecar's kimi integration sit outside Sidecar's managed block in " +
				KimiConfigName + ", so every event would be reported more than once; remove them yourself and run this again"
	case !s.scan.hasBlock():
		if !s.dir.Exists {
			// The status has to carry this, not only the refusal. Without it a
			// machine where kimi is on PATH but has never been run reads as a
			// plain not-installed with an empty message and nothing anywhere
			// saying why the one action that would fix it is not offered.
			return agentlifecycle.StatusNotInstalled, kimiNeverSetUp(s.paths.Dir)
		}
		// The same rule one branch up, for the other reason install can be
		// absent from Offered on a file that reads as plain not-installed: the
		// block Sidecar would append does not survive the oracle. A `[hooks]`
		// table where Kimi's schema wants an array of tables is the case that
		// found this, and appending `[[hooks]]` under it is not valid TOML at all,
		// so planConverge refuses with an exact reason while the status stayed
		// silent and simply showed no install action.
		if why := kimiConvergeBlocked(s.scan); why != "" {
			return agentlifecycle.StatusNotInstalled,
				"Sidecar cannot add its hooks to " + KimiConfigName + " as that file is written (" + why +
					"); fix or move it yourself and run this again"
		}
		return agentlifecycle.StatusNotInstalled, ""
	case s.scan.text == kimiBlock():
		return agentlifecycle.StatusCurrent, ""
	case s.scan.version != KimiAssetVersion:
		return agentlifecycle.StatusOutdated,
			"version " + s.scan.version + " is installed; this build ships version " + KimiAssetVersion
	}
	return agentlifecycle.StatusNeedsRepair,
		"the installed block claims version " + s.scan.version + " but its contents do not match the block this build ships"
}

// kimiConvergeBlocked reports why appending Sidecar's block to this file would
// be refused, or empty when it would be accepted.
//
// It asks the same two functions planConverge asks, in the same order, so the
// status and the refusal can never disagree about whether install is possible.
// A file that does not exist yet composes to the block alone and always
// verifies, so there is nothing to say about one.
func kimiConvergeBlocked(scan kimiConfigScan) string {
	if !scan.exists || scan.parseErr != "" {
		return ""
	}
	content, err := kimiCompose(scan, kimiBlock())
	if err != nil {
		return err.Error()
	}
	if err := kimiOracleConverged(scan.raw, content); err != nil {
		return err.Error()
	}
	return ""
}

// Inspect implements [Adapter].
func (a KimiAdapter) Inspect(env Env) Status {
	return a.statusOf(a.inspect(env))
}

func (a KimiAdapter) statusOf(s kimiState) Status {
	capability, _ := agentlifecycle.CapabilityForSource(KimiSource)
	inRange := versionInRange(s.providerVersion, capability.TestedProviderRange)
	tier, reason := capability.TierFor(s.status, inRange)

	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion:         agentlifecycle.SchemaVersion,
		Provider:              KimiProvider,
		Source:                KimiSource,
		Status:                s.status,
		BundledVersion:        KimiAssetVersion,
		InstalledVersion:      s.installed,
		ProviderVersion:       s.providerVersion,
		ProviderInTestedRange: inRange,
		EffectiveTier:         tier,
		TierReason:            reason,
		TargetPaths:           []string{s.paths.Config},
		KnownGaps:             capability.KnownGaps,
		Message:               s.message,
	}}
	st.ProviderPath = s.providerPath
	st.Files = []FileState{s.dir, s.config, s.backup}

	// Offered is computed by asking the planner, not by restating its rules in a
	// second place. A verb a surface offers is a verb that will not refuse when
	// it is pressed.
	for _, act := range Actions() {
		if _, err := a.plan(s, act); err == nil {
			st.Offered = append(st.Offered, act)
		}
	}
	return st
}

// Plan implements [Adapter].
func (a KimiAdapter) Plan(env Env, act Action) (Plan, error) {
	return a.plan(a.inspect(env), act)
}

func (a KimiAdapter) plan(s kimiState, act Action) (Plan, error) {
	p := Plan{
		SchemaVersion: InstallSchemaVersion,
		Provider:      KimiProvider,
		Source:        KimiSource,
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

// planConverge builds the plan that ends with exactly one Sidecar block, at the
// bundled version, and every other byte of config.toml as it was found.
func (a KimiAdapter) planConverge(s kimiState, p Plan, act Action) (Plan, error) {
	if s.providerPath == "" {
		return Plan{}, refuse(RefuseProviderMissing, "",
			"the kimi CLI was not found on PATH, so Sidecar will not modify %s for it; install kimi code first", s.paths.Config)
	}
	// Herdr's install_kimi refuses when the data directory is absent, and the
	// semantics are worth keeping: ~/.kimi-code is created by Kimi itself, so
	// its absence means Kimi has never run here, and creating a provider's
	// private state tree for an agent that may be about to be configured
	// somewhere else is Sidecar inventing configuration. Expressed as Sidecar's
	// own refusal code rather than as an io error.
	if !s.dir.Exists {
		return Plan{}, refuse(RefuseProviderMissing, s.paths.Dir, "%s", kimiNeverSetUp(s.paths.Dir))
	}
	if err := gateConvergeVerb(s.assetStatus, act, s.paths.Config, KimiProvider, s.installed, s.message); err != nil {
		return Plan{}, err
	}
	if err := kimiRefuseUnsafe(s); err != nil {
		return Plan{}, err
	}

	content, err := kimiCompose(s.scan, kimiBlock())
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	if err := kimiOracleConverged(s.scan.raw, content); err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config,
			"the rewrite Sidecar composed for %s does not verify against the file it started from: %v", s.paths.Config, err)
	}

	p.Ops = entryFileOps(nil, s.env, s.dir, s.config, s.backup, content,
		"write Sidecar's twelve kimi lifecycle hooks, preserving every other line",
		ownedEntry(KimiAssetVersion))
	if len(p.Ops) == 0 {
		p.Unchanged = true
		return p, nil
	}
	p.StatusAfter = agentlifecycle.StatusCurrent
	return p, nil
}

// planUninstall removes exactly the lines between Sidecar's two markers.
//
// It refuses in both of the ways an ownership boundary can be crossed, and
// neither refusal is fussiness. A hook of the user's inside the block would be
// deleted by removing the block. A hook invoking Sidecar's source outside the
// block would survive it, so the command would report success while the pane
// kept being reported on by a copy Sidecar may not touch — an uninstall that
// did not uninstall. Both name the exact fix instead.
func (a KimiAdapter) planUninstall(s kimiState, p Plan) (Plan, error) {
	// Nothing of Sidecar's is here and the file reads cleanly enough to say so:
	// there is nothing to do. A stray copy outside the block is deliberately not
	// "nothing of Sidecar's" -- it is the one case where an uninstall that did
	// nothing and reported success would leave the pane still being reported on.
	if !s.config.Exists || (!s.scan.hasBlock() && s.scan.parseErr == "" && s.scan.stray() == 0) {
		p.Unchanged = true
		return p, nil
	}
	if err := kimiRefuseUnsafe(s); err != nil {
		return Plan{}, err
	}
	if s.scan.foreign > 0 {
		return Plan{}, refuse(RefuseForeignFile, s.paths.Config,
			"%d hook entries that are not Sidecar's sit inside Sidecar's managed block in %s, and removing the block would delete them; move them outside the marker comments yourself and run this again",
			s.scan.foreign, s.paths.Config)
	}

	content, err := kimiCompose(s.scan, "")
	if err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config, "%s: %v", s.paths.Config, err)
	}
	if err := kimiOracleRemoved(s.scan.raw, content); err != nil {
		return Plan{}, refuse(RefuseUnreadable, s.paths.Config,
			"the rewrite Sidecar composed for %s does not verify against the file it started from: %v", s.paths.Config, err)
	}

	// userEntry, not ownedEntry: this op takes Sidecar's block out, so what it
	// leaves behind is the user's own config.toml.
	if len(strings.TrimSpace(string(content))) == 0 {
		p.Ops = kimiRemoveFileOps(s)
	} else {
		p.Ops = entryFileOps(nil, s.env, s.dir, s.config, s.backup, content,
			"remove Sidecar's kimi lifecycle hooks, preserving every other line", userEntry())
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

// kimiRemoveFileOps backs up and removes a config.toml that held nothing but
// Sidecar's block, which is a file Sidecar created.
func kimiRemoveFileOps(s kimiState) []Op {
	mode := fs.FileMode(0o644)
	if m := parseMode(s.config.Mode); m != 0 {
		mode = m
	}
	return []Op{
		{
			Kind:     OpBackup,
			Path:     s.paths.Backup,
			From:     s.paths.Config,
			Mode:     renderMode(mode),
			mode:     mode,
			Bytes:    int(s.config.Size),
			Checksum: s.config.Checksum,
			Note:     "keep a recoverable copy of the file being removed",
			Before:   s.backup,
			After: FileState{
				Path: s.paths.Backup, Exists: true, Kind: "file",
				Checksum: s.config.Checksum, Mode: renderMode(mode), Size: s.config.Size,
			},
		},
		{
			Kind:   OpRemove,
			Path:   s.paths.Config,
			Note:   "remove the file, which held nothing but Sidecar's block",
			Before: s.config,
			After:  FileState{Path: s.paths.Config},
		},
	}
}

// kimiRefuseUnsafe is the safety gate every mutation of config.toml passes.
func kimiRefuseUnsafe(s kimiState) error {
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
	if s.scan.stray() > 0 {
		return refuse(RefuseForeignFile, s.paths.Config,
			"%d hook entries invoking Sidecar's kimi integration sit outside Sidecar's managed block in %s, so every event would be reported more than once; Sidecar will not edit outside its own block — remove them yourself and run this again",
			s.scan.stray(), s.paths.Config)
	}
	return nil
}

// kimiCompose returns config.toml with Sidecar's block replaced by block, which
// is empty for an uninstall.
//
// The block always goes at end of file. A TOML array of tables appended there
// cannot change the meaning of anything above it, which is what makes appending
// safe on a file whose structure Sidecar has deliberately not modelled.
//
// Trailing blank lines are collapsed to the single separator Sidecar writes.
// That is the one whitespace liberty the editor takes, and it is what keeps
// install/uninstall/install idempotent instead of growing a blank line per
// cycle; the oracle below proves no value the user wrote changes.
func kimiCompose(s kimiConfigScan, block string) ([]byte, error) {
	lines := s.lines
	if s.hasBlock() {
		if s.begin > s.end || s.end >= len(lines) {
			return nil, fmt.Errorf("the recorded block span %d..%d is not inside the file", s.begin, s.end)
		}
		kept := append([]string(nil), lines[:s.begin]...)
		lines = append(kept, lines[s.end+1:]...)
	}
	body := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	// A file of nothing but whitespace has no user content to separate from.
	if strings.TrimSpace(body) == "" {
		body = ""
	}
	switch {
	case block == "" && body == "":
		return nil, nil
	case block == "":
		return []byte(body + "\n"), nil
	case body == "":
		return []byte(block), nil
	}
	return []byte(body + "\n\n" + block), nil
}

// --- the TOML oracle: parsing, never serializing ---
//
// Composition is the line editor's job above. These functions only ever ask a
// real parser what two byte slices mean, and refuse the edit when the answer is
// not the one the verb promised.

// kimiOracleDiff proves that a composed image differs from the pre-image only
// inside the region Sidecar owns: every key the user wrote is still there with
// the same value, nothing new appears outside Sidecar's own hook entries, and
// the user's own hook entries keep their order.
func kimiOracleDiff(pre, post []byte) error {
	preDoc, err := codexTOMLDoc(pre)
	if err != nil {
		return fmt.Errorf("the file it started from is not valid TOML: %s", tomlErrBrief(err))
	}
	postDoc, err := codexTOMLDoc(post)
	if err != nil {
		return fmt.Errorf("the composed file is not valid TOML: %s", tomlErrBrief(err))
	}

	// The hooks array is compared separately and by identity, because it is the
	// one key both sides legitimately differ on. Everything else is compared
	// leaf by leaf, with hooks removed so the array is never flattened into an
	// opaque value that would compare unequal for the right reason and the
	// wrong one alike.
	preHooks, postHooks := kimiForeignHooks(preDoc), kimiForeignHooks(postDoc)
	if !reflect.DeepEqual(preHooks, postHooks) {
		return fmt.Errorf("it would add, drop, reorder or change a hook entry that is not Sidecar's")
	}
	delete(preDoc, "hooks")
	delete(postDoc, "hooks")

	preFlat, postFlat := tomlFlattenDoc(preDoc), tomlFlattenDoc(postDoc)
	for _, p := range sortedPaths(preFlat) {
		got, ok := postFlat[p]
		if !ok {
			return fmt.Errorf("it would drop %s, which Sidecar does not own", p)
		}
		if !reflect.DeepEqual(preFlat[p], got) {
			return fmt.Errorf("it would change the value of %s, which Sidecar does not own", p)
		}
	}
	for _, p := range sortedPaths(postFlat) {
		if _, ok := preFlat[p]; !ok {
			return fmt.Errorf("it would add %s, which is outside the region Sidecar owns", p)
		}
	}
	return nil
}

// kimiForeignHooks is every hook entry in a parsed document that is not
// Sidecar's, in file order.
func kimiForeignHooks(doc map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, entry := range kimiHookEntries(doc) {
		if command, _ := entry["command"].(string); invokesKimiReport(command) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// kimiOracleConverged checks the install/update/repair intent on top of the
// diff: exactly the twelve hooks this build ships are present, each with the
// event, matcher, command and timeout it was rendered with.
func kimiOracleConverged(pre, post []byte) error {
	if err := kimiOracleDiff(pre, post); err != nil {
		return err
	}
	doc, err := codexTOMLDoc(post)
	if err != nil {
		return fmt.Errorf("the composed file is not valid TOML: %s", tomlErrBrief(err))
	}
	var ours []map[string]any
	for _, entry := range kimiHookEntries(doc) {
		if command, _ := entry["command"].(string); invokesKimiReport(command) {
			ours = append(ours, entry)
		}
	}
	if len(ours) != len(kimiHooks) {
		return fmt.Errorf("it would leave %d Sidecar hook entries, not %d", len(ours), len(kimiHooks))
	}
	for i, want := range kimiHooks {
		got := ours[i]
		if event, _ := got["event"].(string); event != want.Event {
			return fmt.Errorf("hook %d would carry event %q, not %q", i, event, want.Event)
		}
		matcher, hasMatcher := got["matcher"].(string)
		if want.Matcher == "" && hasMatcher {
			return fmt.Errorf("hook %d would carry a matcher where the port has none", i)
		}
		if want.Matcher != "" && matcher != want.Matcher {
			return fmt.Errorf("hook %d would carry matcher %q, not %q", i, matcher, want.Matcher)
		}
		if command, _ := got["command"].(string); command != KimiHookCommand(want) {
			return fmt.Errorf("hook %d would carry command %q, not the one this build renders", i, command)
		}
		if timeout, _ := got["timeout"].(int64); timeout != int64(hookTimeoutSec) {
			return fmt.Errorf("hook %d would carry timeout %d, not %d", i, timeout, hookTimeoutSec)
		}
	}
	return nil
}

// kimiOracleRemoved checks the uninstall intent on top of the diff: not one
// hook entry of Sidecar's survives.
func kimiOracleRemoved(pre, post []byte) error {
	if err := kimiOracleDiff(pre, post); err != nil {
		return err
	}
	if n := kimiOwnedHookCount(post); n != 0 {
		return fmt.Errorf("%d Sidecar hook entries would survive the uninstall", n)
	}
	return nil
}

var _ Adapter = KimiAdapter{}

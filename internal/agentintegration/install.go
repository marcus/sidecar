package agentintegration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/agentlifecycle/lifecyclestore"
	"github.com/marcus/sidecar/internal/config"
)

// The integration installer.
//
// One application service sits behind `sidecar agent integration ...` and the
// Configuration → Agents → Integrations route, so a fact shown in the TUI and a
// fact printed by the CLI are the same computed value rather than two
// implementations that agree today.
//
// The shape is deliberate in three ways.
//
// An adapter decides *what* an action should do; the engine below decides *how*
// it is done. Every mutation is expressed as an ordered list of [Op] values and
// then executed by one shared [Apply]. That is what makes `--dry-run` honest:
// the preview is not a description of the mutation, it is the mutation with the
// execution step skipped. A dry run and a real run of the same action produce
// byte-identical op lists because one function produces both.
//
// Safety is evaluated during planning, never during execution. By the time
// [Apply] runs, every path has already been proved to be a regular file or
// absent, owned by this user, not a symlink, and either Sidecar's own asset or
// nothing at all. An adapter that cannot say that refuses with a [Refusal]
// naming the exact path, and no operation is attempted.
//
// Ownership is a property of file content, not of a filename. Sidecar will
// install, replace, and remove a file carrying its own integration marker, and
// will refuse to touch anything else — including a file with exactly the name
// it would have chosen. "Never auto-adopt a similarly named existing script" is
// enforced here rather than trusted to the caller.

// InstallSchemaVersion is the wire version of the plan and status records this
// installer emits. It is separate from [agentlifecycle.SchemaVersion] because
// the report contract and the installer contract change for different reasons.
const InstallSchemaVersion = 1

// maxAssetBytes bounds what will be read from an installed asset while
// inspecting it. Anything larger is not a Sidecar asset, and reading it in full
// to discover that would be the bug.
const maxAssetBytes = 1 << 20

// Action is one mutating integration operation.
type Action string

const (
	ActionInstall   Action = "install"
	ActionUpdate    Action = "update"
	ActionRepair    Action = "repair"
	ActionUninstall Action = "uninstall"
)

// Actions is the frozen, ordered action vocabulary.
func Actions() []Action {
	return []Action{ActionInstall, ActionUpdate, ActionRepair, ActionUninstall}
}

// IsAction reports whether s names a known action.
func IsAction(s string) bool {
	for _, a := range Actions() {
		if string(a) == s {
			return true
		}
	}
	return false
}

// RefusalCode is the frozen vocabulary of reasons a mutation is refused.
//
// These are codes rather than prose so an agent can branch on them, and so the
// CLI and the Configuration surface can each say it in their own words without
// either inventing a new category.
type RefusalCode string

const (
	// RefuseUnknownProvider means no provider by that name is recorded at all.
	RefuseUnknownProvider RefusalCode = "unknown_provider"
	// RefuseUnsupported means Sidecar ships no integration asset for it.
	RefuseUnsupported RefusalCode = "unsupported"
	// RefuseProviderMissing means the provider CLI was not found on PATH.
	RefuseProviderMissing RefusalCode = "provider_missing"
	// RefuseNotInstalled means the action needs an installed asset.
	RefuseNotInstalled RefusalCode = "not_installed"
	// RefuseAlreadyInstalled means install found an existing installation that
	// is not merely current, and a different verb is the honest one.
	RefuseAlreadyInstalled RefusalCode = "already_installed"
	// RefuseNeedsRepair means the installation is damaged in a way this verb
	// does not address.
	RefuseNeedsRepair RefusalCode = "needs_repair"
	// RefuseForeignFile means a file Sidecar does not own occupies a path it
	// would otherwise write or remove. Sidecar never adopts or deletes it.
	RefuseForeignFile RefusalCode = "foreign_file"
	// RefuseUnsafePath means a path is a symlink or a non-regular file where a
	// regular file was required.
	RefuseUnsafePath RefusalCode = "unsafe_path"
	// RefuseUnsafeOwner means a path is owned by a different user.
	RefuseUnsafeOwner RefusalCode = "unsafe_owner"
	// RefuseUnsafeMode means a directory is group- or world-writable, so
	// installing an executable asset into it would broaden who can change it.
	RefuseUnsafeMode RefusalCode = "unsafe_mode"
	// RefuseUnreadable means a path exists but could not be inspected.
	RefuseUnreadable RefusalCode = "unreadable"
)

// RefusalCodes is the frozen, ordered refusal vocabulary.
func RefusalCodes() []RefusalCode {
	return []RefusalCode{
		RefuseUnknownProvider,
		RefuseUnsupported,
		RefuseProviderMissing,
		RefuseNotInstalled,
		RefuseAlreadyInstalled,
		RefuseNeedsRepair,
		RefuseForeignFile,
		RefuseUnsafePath,
		RefuseUnsafeOwner,
		RefuseUnsafeMode,
		RefuseUnreadable,
	}
}

// Refusal is a planning-time refusal. It always names what was refused and,
// where a path is involved, which one.
type Refusal struct {
	Code    RefusalCode `json:"code"`
	Message string      `json:"message"`
	Path    string      `json:"path,omitempty"`
}

func (r *Refusal) Error() string { return r.Message }

func refuse(code RefusalCode, path, format string, a ...any) *Refusal {
	return &Refusal{Code: code, Path: path, Message: fmt.Sprintf(format, a...)}
}

// AsRefusal extracts a [Refusal] from err, if there is one.
func AsRefusal(err error) (*Refusal, bool) {
	var r *Refusal
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

// OpKind is one primitive file operation.
type OpKind string

const (
	// OpMkdir creates a directory that does not exist.
	OpMkdir OpKind = "mkdir"
	// OpBackup copies a Sidecar-owned file aside before it is replaced.
	OpBackup OpKind = "backup"
	// OpWrite writes the bundled asset atomically.
	OpWrite OpKind = "write"
	// OpRemove removes a Sidecar-owned file.
	OpRemove OpKind = "remove"
	// OpRmdir removes a directory that is empty once Sidecar's own files are
	// gone. It is never planned for a directory that still holds anything.
	OpRmdir OpKind = "rmdir"
)

// Op is one ordered file operation in a [Plan].
//
// Before and After are the inspected state of Path on either side of the
// operation, which is what the plan's "before/after ownership status" means: a
// reader can see that a write lands on nothing, or replaces a file Sidecar
// owns, without having to take the verb's word for it.
type Op struct {
	Kind OpKind `json:"kind"`
	Path string `json:"path"`
	// From is the source path for a backup.
	From string `json:"from,omitempty"`
	// Mode is the mode a created path is given, rendered as "0755".
	Mode string `json:"mode,omitempty"`
	// Bytes and Checksum describe the content a write produces.
	Bytes    int    `json:"bytes,omitempty"`
	Checksum string `json:"checksum,omitempty"`
	// Note is a one-line human explanation of why this operation is here.
	Note   string    `json:"note"`
	Before FileState `json:"before"`
	After  FileState `json:"after"`

	// content is what OpWrite writes. It is deliberately unexported: the
	// serialized plan carries the asset's size and checksum, never a screenful
	// of JavaScript, and a plan is always built and applied in one process.
	content []byte
	// mode is the parsed form of Mode.
	mode fs.FileMode
}

// FileState is the inspected state of one path.
type FileState struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	// Kind is file, dir, symlink, or other. It is reported for an existing path
	// even when that path is unusable, because "it is a symlink" is the answer
	// to why an install refused.
	Kind string `json:"kind,omitempty"`
	// Owned reports that Sidecar has something of its own in this file. A file
	// that merely has the right name is never owned.
	//
	// What "something of its own" means is not a second meaning smuggled behind
	// one boolean: it is whatever [Asset.Ownership] declares for the asset that
	// installs here, and [FileState.Ownership] repeats it so a reader of this
	// value never has to guess. For [OwnsFile] the whole file is Sidecar's and
	// ownership is the marker its bytes carry. For [OwnsEntry] the file is the
	// user's, Sidecar owns one entry inside it, and ownership is that entry's
	// content.
	//
	// The distinction is not cosmetic. It is the difference between "remove
	// this file" and "remove this entry and leave the file", which is why the
	// two are named rather than inferred.
	Owned bool `json:"owned"`
	// Ownership is what Owned means here. It is meaningful only when the path
	// is one an asset installs to; it is zero for a path no asset claims, such
	// as a directory or a backup.
	Ownership Ownership `json:"ownership,omitempty"`
	// Version is the asset version, parsed from the marker where there is one.
	Version string `json:"version,omitempty"`
	// Checksum is the sha256 of the file's bytes.
	Checksum string `json:"checksum,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Size     int64  `json:"size,omitempty"`
	// Unsafe names why this path may not be written or removed, when it may
	// not. An empty value means the path is safe to operate on.
	Unsafe RefusalCode `json:"unsafe,omitempty"`
	// UnsafeDetail is the sanitized sentence behind Unsafe.
	UnsafeDetail string `json:"unsafeDetail,omitempty"`
}

// Plan is the complete, ordered description of one mutation.
//
// Ops, StatusBefore, and StatusAfter are the parts a dry run and a real run are
// required to agree on byte for byte. DryRun, Applied, and Unchanged describe
// this particular invocation and are deliberately outside that comparison.
type Plan struct {
	SchemaVersion int    `json:"schemaVersion"`
	Provider      string `json:"provider"`
	Source        string `json:"source"`
	Action        Action `json:"action"`

	StatusBefore agentlifecycle.IntegrationStatus `json:"statusBefore"`
	StatusAfter  agentlifecycle.IntegrationStatus `json:"statusAfter"`

	Ops []Op `json:"ops"`

	DryRun  bool `json:"dryRun"`
	Applied bool `json:"applied"`
	// Unchanged reports that the action was a no-op because the world already
	// matched what it would have produced. It is how idempotency becomes
	// visible rather than merely true.
	Unchanged bool `json:"unchanged"`
}

// Ownership is the shape of one bundled asset: what installing it does to the
// file at its path, and therefore how Sidecar recognizes its own work there
// again afterwards.
//
// There are exactly two shapes because there are exactly two things a provider
// can ask for. Either it loads whole files from a directory it scans, in which
// case Sidecar's integration *is* a file; or it reads one configuration file
// the user owns, in which case Sidecar's integration is an entry inside a file
// it must otherwise leave alone. Every rule that differs between adapters --
// how ownership is decided, whether a checksum means anything, what uninstall
// removes, what a surface should call it -- follows from this one distinction,
// so it is declared once here rather than rediscovered per adapter.
type Ownership string

const (
	// OwnsFile: the whole file at the asset's path is Sidecar's. It is
	// recognized by the integration marker its own bytes carry, it is written
	// byte-for-byte from the bundled content, its checksum is meaningful, and
	// uninstall removes the file.
	OwnsFile Ownership = "file"
	// OwnsEntry: the file at the asset's path belongs to the user and Sidecar
	// owns one entry inside it. It is recognized by that entry's content -- a
	// user-owned config format has no comment syntax to carry a marker -- the
	// file's checksum says nothing about ownership, and uninstall removes the
	// entry and leaves every other byte alone.
	OwnsEntry Ownership = "entry"
)

// Asset is one bundled unit an integration installs.
//
// An integration has one asset per file it touches. A file-drop integration
// therefore has exactly one; an integration that must edit two configuration
// files has two, and each declares its own [Ownership]. Plurality is not
// speculative generality: Codex genuinely edits hooks.json and config.toml,
// and describing that as one asset meant describing one of the two files and
// staying quiet about the other.
type Asset struct {
	// Name is the filename the asset is installed as.
	Name string `json:"name"`
	// Source is the integration identifier reports carry.
	Source string `json:"source"`
	// SchemaVersion is the marker schema the asset declares. It is meaningful
	// only for [OwnsFile].
	SchemaVersion int `json:"schemaVersion"`
	// Version is the asset version. Authority is granted to a source at a
	// version, so this changing is what makes an installed copy outdated.
	Version string `json:"version"`
	// Ownership is what installing this asset does to the file at its path.
	Ownership Ownership `json:"ownership"`
	// Content is the exact bytes installed for [OwnsFile]. For [OwnsEntry] it
	// is the canonical file Sidecar would create in an empty tree, which is a
	// description of the entry rather than something ever written verbatim over
	// a user's file.
	Content string `json:"-"`
}

// Checksum is the sha256 of the asset's bytes, hex encoded.
func (a Asset) Checksum() string { return checksum([]byte(a.Content)) }

func checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Env is the machine an adapter inspects and mutates.
//
// Every field is injectable so the whole installer can be exercised against a
// temporary tree with no provider installed, which is the only way an
// adversarial-fixture suite is honest: a test that needs the real
// ~/.config/opencode is a test nobody can run twice.
type Env struct {
	// Home is $HOME.
	Home string
	// ConfigHome is $XDG_CONFIG_HOME when set, and empty otherwise.
	ConfigHome string
	// PiAgentDir is $PI_CODING_AGENT_DIR when set, and empty otherwise.
	//
	// It is a field rather than a general environment accessor for the same
	// reason ConfigHome is: an adapter that read os.Getenv directly would make
	// every test in this package depend on the developer's own environment, and
	// the installer suite's whole premise is that it runs inside t.TempDir and
	// can be run twice. Empty means "no override", which is also what a test
	// that never sets it gets.
	PiAgentDir string
	// KiloConfigDir is $KILO_CONFIG_DIR when set, and empty otherwise.
	//
	// A field for the same reason PiAgentDir is one: an adapter reading
	// os.Getenv directly would make every test in this package depend on the
	// developer's own environment. Empty means "no override", which is also what
	// a test that never sets it gets.
	KiloConfigDir string
	// KimiCodeHome is $KIMI_CODE_HOME when set, and empty otherwise. It is Kimi
	// Code CLI's own documented override for its whole data directory, so
	// honouring it is what lets a relocated Kimi be found — and what lets a
	// proof run redirect the provider away from the user's real ~/.kimi-code.
	KimiCodeHome string
	// ClaudeConfigDir is $CLAUDE_CONFIG_DIR when set, and empty otherwise. It
	// is Claude Code's own override for its whole configuration home: the
	// binary resolves that home as the variable's value, falling back to
	// $HOME/.claude, so an installer that reads only $HOME writes into a
	// directory a relocated Claude never reads.
	ClaudeConfigDir string
	// LookPath finds a provider executable. Defaults to exec.LookPath.
	LookPath func(file string) (string, error)
	// ProviderVersion reports an installed provider's version string.
	ProviderVersion func(provider string) string
	// UID is the user this process runs as, compared against file ownership.
	UID int
	// StateDir is Sidecar's host-local state directory, where the lifecycle log
	// lives. Empty means the last report is simply not reported: an integration
	// that has never been used has none anyway, and a status command must not
	// fail because a log is missing.
	StateDir string
}

// OSEnv returns the real machine.
func OSEnv() Env {
	return Env{
		Home:            os.Getenv("HOME"),
		ConfigHome:      os.Getenv("XDG_CONFIG_HOME"),
		PiAgentDir:      os.Getenv("PI_CODING_AGENT_DIR"),
		KiloConfigDir:   os.Getenv("KILO_CONFIG_DIR"),
		KimiCodeHome:    os.Getenv("KIMI_CODE_HOME"),
		ClaudeConfigDir: os.Getenv("CLAUDE_CONFIG_DIR"),
		LookPath:        exec.LookPath,
		ProviderVersion: detectProviderVersion,
		UID:             os.Getuid(),
		StateDir:        config.StateDir(),
	}
}

func (e Env) lookPath(file string) (string, bool) {
	fn := e.LookPath
	if fn == nil {
		fn = exec.LookPath
	}
	path, err := fn(file)
	if err != nil {
		return "", false
	}
	return path, true
}

func (e Env) providerVersion(provider string) string {
	if e.ProviderVersion == nil {
		return detectProviderVersion(provider)
	}
	return e.ProviderVersion(provider)
}

// Status is one provider integration's inspected state.
//
// It embeds the frozen [agentlifecycle.IntegrationReport] rather than restating
// it, so the wire contract Phase A froze is the wire contract both surfaces
// render. The added fields are what only the installer can see: the individual
// paths, what is actually in them, and which verbs are offered right now.
type Status struct {
	agentlifecycle.IntegrationReport

	// ProviderPath is where the provider CLI was found, when it was.
	ProviderPath string `json:"providerPath,omitempty"`
	// Files is every path this adapter inspected, in the order it considers
	// them. A path that does not exist is still reported, because "nothing is
	// there" is the answer to where an install would put something.
	Files []FileState `json:"files,omitempty"`
	// Offered lists the actions that would not be refused right now.
	Offered []Action `json:"offered,omitempty"`
	// LastReport is the newest record this source has written on this machine.
	//
	// It is the difference between "the integration is installed" and "the
	// integration is working", which are not the same claim and are exactly what
	// someone opens this surface to tell apart.
	LastReport *ReportSummary `json:"lastReport,omitempty"`
}

// ReportSummary is what a surface shows about a source's newest report.
//
// It is a summary rather than the record because a record carries identity
// fields — a salted session fingerprint, a run id — that answer no question a
// human is asking here, and the rule for this data is to carry the minimum that
// serves the purpose.
type ReportSummary struct {
	Kind     agentlifecycle.Kind       `json:"kind"`
	State    agentactivity.State       `json:"state,omitempty"`
	Outcome  agentlifecycle.Outcome    `json:"outcome,omitempty"`
	Reason   agentlifecycle.ReasonCode `json:"reason,omitempty"`
	Sequence uint64                    `json:"sequence"`
	// ObservedAt and Age are both reported: the timestamp for a caller
	// computing against it, the rendered age for a human reading it.
	ObservedAt time.Time `json:"observedAt"`
	Age        string    `json:"age"`
	PaneID     string    `json:"paneId,omitempty"`
	Version    string    `json:"sourceVersion,omitempty"`
}

// lastReportFor returns the newest record a source wrote, or nil.
//
// The read is [lifecyclestore.ReadAll], which never locks, repairs, or creates
// anything: a status command must not contend with the hook processes appending
// to the log, and must not be able to damage it. Every failure yields nil,
// because "there is no last report" and "the log could not be read" both mean
// the same thing to a surface that is only reporting it.
func lastReportFor(stateDir, source string) *ReportSummary {
	if stateDir == "" || source == "" {
		return nil
	}
	records, err := lifecyclestore.ReadAll(filepath.Join(stateDir, lifecyclestore.FileName))
	if err != nil {
		return nil
	}
	var newest *agentlifecycle.Report
	for i := range records {
		if records[i].Source != source {
			continue
		}
		if newest == nil || records[i].ObservedAt.After(newest.ObservedAt) {
			newest = &records[i]
		}
	}
	if newest == nil {
		return nil
	}
	return &ReportSummary{
		Kind:       newest.Kind,
		State:      newest.State,
		Outcome:    newest.Outcome,
		Reason:     newest.Reason,
		Sequence:   newest.Sequence,
		ObservedAt: newest.ObservedAt,
		Age:        time.Since(newest.ObservedAt).Round(time.Second).String(),
		PaneID:     newest.Identity.PaneID,
		Version:    newest.SourceVersion,
	}
}

// Adapter is one provider's integration.
//
// The interface is intentionally small and read-heavy: three of its methods
// answer questions and the fourth returns a description of a mutation rather
// than performing one. Nothing in an adapter writes to disk.
type Adapter interface {
	// Provider is the catalog agent kind.
	Provider() string
	// Source is the integration identifier reports carry.
	Source() string
	// Assets are the bundled units this integration installs, one per file it
	// touches, each declaring its own [Ownership]. The order is the order a
	// surface should show them in.
	Assets() []Asset
	// Inspect reads the current state. It never mutates anything and never
	// fails: an unreadable path becomes a FileState carrying the reason.
	Inspect(Env) Status
	// Plan describes what one action would do against the current state, or
	// refuses with a [Refusal].
	Plan(Env, Action) (Plan, error)
}

// DefaultAdapters returns the adapters this build ships.
func DefaultAdapters() []Adapter {
	return []Adapter{OpenCodeAdapter{}, CodexAdapter{}, ClaudeAdapter{}, PiAdapter{}, KiloAdapter{}, KimiAdapter{}, NewAntigravityAdapter()}
}

// Service is the application service behind the CLI and the Configuration
// route.
type Service struct {
	Env      Env
	Adapters []Adapter
}

// NewService returns the service operating on the real machine.
func NewService() Service {
	return Service{Env: OSEnv(), Adapters: DefaultAdapters()}
}

func (s Service) adapters() []Adapter {
	if s.Adapters == nil {
		return DefaultAdapters()
	}
	return s.Adapters
}

func (s Service) adapter(provider string) (Adapter, bool) {
	for _, a := range s.adapters() {
		if a.Provider() == provider {
			return a, true
		}
	}
	return nil, false
}

// List returns every provider Sidecar knows about, sorted by name.
//
// The list is drawn from the capability registry rather than from the adapters,
// so a provider Sidecar has recorded evidence for but ships no asset for still
// appears — reported as unsupported, with its known gaps. Omitting it would
// make "Sidecar does not integrate with Codex yet" indistinguishable from
// "Sidecar has never heard of Codex", and the second is a bug report while the
// first is a roadmap.
func (s Service) List() []Status {
	seen := map[string]bool{}
	var out []Status
	for _, capability := range agentlifecycle.Capabilities() {
		if seen[capability.Provider] {
			continue
		}
		seen[capability.Provider] = true
		out = append(out, s.statusFor(capability.Provider, capability))
	}
	for _, a := range s.adapters() {
		if seen[a.Provider()] {
			continue
		}
		seen[a.Provider()] = true
		out = append(out, s.withLastReport(a.Inspect(s.Env)))
	}
	// Agents you can do something about come first, alphabetically, then the
	// evaluation records -- providers Sidecar has surveyed and deliberately not
	// built an integration for, listed so that "evaluated, not built" is
	// distinguishable from "never looked at".
	//
	// Ordering is part of the answer rather than a cosmetic preference. There
	// are twice as many evaluation records as integrations, and plain
	// alphabetical order buried every actionable row beneath them: at 60x24 the
	// list opened on five agents the user cannot install, and the ones they
	// could were off the page.
	sort.Slice(out, func(i, j int) bool {
		li := out[i].Status == agentlifecycle.StatusUnsupported
		lj := out[j].Status == agentlifecycle.StatusUnsupported
		if li != lj {
			return lj
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

// Status returns one provider's state.
func (s Service) Status(provider string) (Status, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return Status{}, refuse(RefuseUnknownProvider, "", "a provider is required")
	}
	if a, ok := s.adapter(provider); ok {
		return s.withLastReport(a.Inspect(s.Env)), nil
	}
	capability, known := capabilityForProvider(provider)
	if !known {
		return Status{}, refuse(RefuseUnknownProvider, "", "no integration is recorded for provider %q; run sidecar agent integration list", provider)
	}
	return s.statusFor(provider, capability), nil
}

func (s Service) statusFor(provider string, capability agentlifecycle.Capability) Status {
	if a, ok := s.adapter(provider); ok {
		return s.withLastReport(a.Inspect(s.Env))
	}
	return s.withLastReport(unsupportedStatus(s.Env, capability))
}

// withLastReport attaches the newest record this source has written.
func (s Service) withLastReport(st Status) Status {
	st.LastReport = lastReportFor(s.Env.StateDir, st.Source)
	return st
}

// Plan describes one action without performing it.
func (s Service) Plan(provider string, act Action) (Plan, error) {
	a, err := s.mutableAdapter(provider)
	if err != nil {
		return Plan{}, err
	}
	p, err := a.Plan(s.Env, act)
	if err != nil {
		return Plan{}, err
	}
	p.DryRun = true
	return p, nil
}

// Apply performs one action.
//
// It plans through exactly the same call [Plan] uses and then executes the
// result, which is why a preview cannot describe something different from what
// happens. The returned plan is the one that ran.
func (s Service) Apply(provider string, act Action) (Plan, error) {
	a, err := s.mutableAdapter(provider)
	if err != nil {
		return Plan{}, err
	}
	p, err := a.Plan(s.Env, act)
	if err != nil {
		return Plan{}, err
	}
	if err := Apply(p); err != nil {
		return p, err
	}
	p.Applied = true
	// The status afterwards is re-inspected rather than predicted. A plan says
	// what it intends; only the filesystem says what happened.
	p.StatusAfter = a.Inspect(s.Env).Status
	return p, nil
}

func (s Service) mutableAdapter(provider string) (Adapter, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, refuse(RefuseUnknownProvider, "", "a provider is required")
	}
	if a, ok := s.adapter(provider); ok {
		return a, nil
	}
	if _, known := capabilityForProvider(provider); known {
		return nil, refuse(RefuseUnsupported, "", "Sidecar ships no integration asset for %q; its recorded capability is evidence for a future adapter, not something that can be installed", provider)
	}
	return nil, refuse(RefuseUnknownProvider, "", "no integration is recorded for provider %q; run sidecar agent integration list", provider)
}

func capabilityForProvider(provider string) (agentlifecycle.Capability, bool) {
	for _, c := range agentlifecycle.Capabilities() {
		if c.Provider == provider {
			return c, true
		}
	}
	return agentlifecycle.Capability{}, false
}

// unsupportedStatus describes a provider Sidecar has recorded evidence for but
// ships no asset for.
func unsupportedStatus(env Env, capability agentlifecycle.Capability) Status {
	st := Status{IntegrationReport: agentlifecycle.IntegrationReport{
		SchemaVersion: agentlifecycle.SchemaVersion,
		Provider:      capability.Provider,
		Source:        capability.Source,
		Status:        agentlifecycle.StatusUnsupported,
		KnownGaps:     capability.KnownGaps,
		Message:       "No integration ships for this agent yet; its recorded capability is evidence for a future adapter",
	}}
	if path, ok := env.lookPath(capability.Provider); ok {
		st.ProviderPath = path
		st.ProviderVersion = env.providerVersion(capability.Provider)
	}
	st.EffectiveTier, st.TierReason = capability.TierFor(agentlifecycle.StatusUnsupported, false)
	return st
}

// Apply executes a plan's operations in order.
//
// Every safety question was answered while the plan was built, so this function
// deliberately makes no policy decisions: it creates, copies, writes, and
// removes exactly what it was given. A failure part-way through leaves the
// completed operations in place, which is why a replacement is ordered
// backup-then-write and why the write itself is a rename over the existing file
// rather than a truncate: there is no moment at which the asset is half a file.
//
// The remaining time-of-check-to-time-of-use window is named rather than closed,
// and it is narrower than "safety is decided at plan time" makes it sound. A
// plan is built and applied inside one process, microseconds apart, with no user
// interaction in between; the only paths involved are files under the user's own
// configuration directory; and every destructive operation was gated on
// ownership proved from the file's own bytes at plan time. Re-proving that here
// would not make the window disappear, only move it — a file that stopped being
// Sidecar's between the two checks is a file something else is rewriting at this
// exact moment, and no ordering of checks inside this loop can win that race.
// What is worth having instead is that a lost race fails safe, which is why
// [OpRemove] tolerates an already-absent path and [OpRmdir] tolerates a
// directory that is no longer empty.
func Apply(p Plan) error {
	for _, op := range p.Ops {
		if err := applyOp(op); err != nil {
			return fmt.Errorf("%s %s: %w", op.Kind, op.Path, err)
		}
	}
	return nil
}

func applyOp(op Op) error {
	switch op.Kind {
	case OpMkdir:
		return os.MkdirAll(op.Path, op.mode)
	case OpBackup:
		b, err := os.ReadFile(op.From)
		if err != nil {
			return err
		}
		return writeAtomic(op.Path, b, op.mode)
	case OpWrite:
		return writeAtomic(op.Path, op.content, op.mode)
	case OpRemove:
		if err := os.Remove(op.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case OpRmdir:
		// Remove, not RemoveAll. This is only planned for a directory observed
		// to hold nothing but Sidecar's own files, so it can never take
		// somebody else's plugin with it.
		//
		// It is also the one operation that is conditional by construction, and
		// a lost race on it is not a failure. "The directory is empty" is
		// evaluated while the plan is built; if anything landed in it between
		// then and here, the correct outcome is to leave the directory alone —
		// not to fail an uninstall that has already correctly removed the asset,
		// the duplicate, and the backup, and report exit 1 for a tidy-up that
		// was never the point. POSIX names that ENOTEMPTY; some systems surface
		// the same condition as EEXIST, so both are treated as the no-op they
		// are.
		if err := os.Remove(op.Path); err != nil &&
			!os.IsNotExist(err) &&
			!errors.Is(err, syscall.ENOTEMPTY) &&
			!errors.Is(err, syscall.EEXIST) {
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown operation %q", op.Kind)
}

// writeAtomic writes b to path through a temp file in the same directory and a
// rename, so a reader never sees a partial asset and an interrupted write never
// destroys the file it was replacing.
func writeAtomic(path string, b []byte, mode fs.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// inspectFile reads one path's state without following symlinks.
//
// Lstat rather than Stat is load-bearing. A symlink at the asset's path would
// make an ordinary Stat report a perfectly healthy regular file while a write
// landed wherever the link pointed — outside the directory Sidecar owns, and
// possibly outside the user's home entirely.
func inspectFile(env Env, path string, want Asset) FileState {
	st := FileState{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st
		}
		st.Exists = true
		st.Unsafe, st.UnsafeDetail = RefuseUnreadable, "the path exists but could not be inspected"
		return st
	}
	st.Exists = true
	st.Mode = renderMode(info.Mode().Perm())
	st.Size = info.Size()
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		st.Kind = "symlink"
		st.Unsafe, st.UnsafeDetail = RefuseUnsafePath, "the path is a symlink, so writing it would land outside the directory Sidecar owns"
		return st
	case info.IsDir():
		st.Kind = "dir"
		st.Unsafe, st.UnsafeDetail = RefuseUnsafePath, "the path is a directory where a file was expected"
		return st
	case !info.Mode().IsRegular():
		st.Kind = "other"
		st.Unsafe, st.UnsafeDetail = RefuseUnsafePath, "the path is not a regular file"
		return st
	}
	st.Kind = "file"
	if !ownedByUser(env, info) {
		st.Unsafe, st.UnsafeDetail = RefuseUnsafeOwner, "the file belongs to another user"
		return st
	}
	if info.Size() > maxAssetBytes {
		// Not unsafe, just not ours. It stays a foreign file, which the
		// planners already refuse to touch.
		st.UnsafeDetail = "the file is far larger than any Sidecar asset, so it is not one"
		return st
	}
	b, err := os.ReadFile(path)
	if err != nil {
		st.Unsafe, st.UnsafeDetail = RefuseUnreadable, "the file exists but could not be read"
		return st
	}
	st.Checksum = checksum(b)
	st.Ownership = want.Ownership
	// The marker rule belongs to OwnsFile and only to it. Running it over a
	// user's settings.json or config.toml could never succeed -- neither format
	// carries a `//` comment Sidecar would have written -- so doing it anyway
	// was not merely wasted work: it left every entry adapter with a FileState
	// whose Owned was false for a file it demonstrably had an entry in, and the
	// adapters then corrected it afterwards. The correction is now the rule.
	//
	// Stated as "only OwnsFile", not as "not OwnsEntry", and the difference is
	// the safety direction. An asset that declares no ownership at all has the
	// zero value here, and testing for OwnsEntry let that zero value fall through
	// into the marker rule — so a malformed or half-written asset declaration
	// would be one matching comment away from Sidecar concluding it owns a file
	// it does not, and every planner downstream treats an owned file as one it
	// may rewrite or delete. The default has to be "not Sidecar's".
	if want.Ownership != OwnsFile {
		return st
	}
	if id, schema, version, ok := parseMarker(string(b)); ok && id == want.Source && schema == want.SchemaVersion {
		st.Owned, st.Version = true, version
	}
	return st
}

// ownEntry records that an entry adapter found its own entry in a user-owned
// file. It is the OwnsEntry counterpart to the marker check in inspectFile:
// same conclusion, reached by the rule that shape of integration actually has.
func ownEntry(st *FileState, version string) {
	st.Owned, st.Version, st.Ownership = true, version, OwnsEntry
}

// inspectDir reads a directory's state for the purpose of writing into it.
func inspectDir(env Env, path string) FileState {
	st := FileState{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st
		}
		st.Exists = true
		st.Unsafe, st.UnsafeDetail = RefuseUnreadable, "the directory exists but could not be inspected"
		return st
	}
	st.Exists = true
	st.Mode = renderMode(info.Mode().Perm())
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		// A symlinked config directory is ordinary — dotfile repositories do it
		// constantly — so the link itself is not the problem. What it resolves
		// to is, and that is checked below against the same rules a real
		// directory faces.
		st.Kind = "symlink"
		resolved, err := os.Stat(path)
		if err != nil {
			st.Unsafe, st.UnsafeDetail = RefuseUnsafePath, "the directory is a symlink that does not resolve"
			return st
		}
		if !resolved.IsDir() {
			st.Unsafe, st.UnsafeDetail = RefuseUnsafePath, "the directory is a symlink to something that is not a directory"
			return st
		}
		info = resolved
		st.Mode = renderMode(info.Mode().Perm())
	case !info.IsDir():
		st.Kind = "file"
		st.Unsafe, st.UnsafeDetail = RefuseUnsafePath, "a file occupies the path where the plugin directory belongs"
		return st
	default:
		st.Kind = "dir"
	}
	if !ownedByUser(env, info) {
		st.Unsafe, st.UnsafeDetail = RefuseUnsafeOwner, "the directory belongs to another user"
		return st
	}
	// A group- or world-writable plugin directory means anyone in that group
	// can replace the file the provider loads and executes. Installing into it
	// would be handing that away, so it is refused rather than silently
	// tightened: narrowing a directory the user widened deliberately is not
	// this command's business either.
	if info.Mode().Perm()&0o022 != 0 {
		st.Unsafe, st.UnsafeDetail = RefuseUnsafeMode, "the directory is group- or world-writable, so an installed asset could be replaced by another account"
		return st
	}
	return st
}

func ownedByUser(env Env, info fs.FileInfo) bool {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// An unknown stat shape cannot be shown to belong to someone else, and
		// refusing here would break the installer on any platform whose
		// FileInfo does not carry a uid — a worse failure than the one it
		// prevents.
		return true
	}
	return int(sys.Uid) == env.UID
}

func renderMode(m fs.FileMode) string { return "0" + strconv.FormatUint(uint64(m), 8) }

func parseMode(s string) fs.FileMode {
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "0"), 8, 32)
	if err != nil {
		return 0
	}
	return fs.FileMode(n)
}

// markerToken is the ownership sentinel itself, without a comment syntax around
// it. Every Sidecar-owned region in a provider's tree carries it, whatever that
// file's comment character is: `//` for the JavaScript and TypeScript assets
// below, `#` for the managed block in Kimi's TOML configuration. Naming it once
// is what keeps "only ever remove what carries the sidecar-integration: marker"
// a single rule rather than a string repeated per adapter.
const markerToken = "sidecar-integration:"

// markerPrefix introduces the one line that makes a file Sidecar's.
//
// The marker exists because ownership cannot be a filename. Without it, an
// uninstall would delete whatever happened to be called sidecar-lifecycle.js
// and an install would silently adopt someone else's script of the same name —
// both of which the plan forbids in as many words.
const markerPrefix = "// " + markerToken

// markerScanLines bounds how far into a file the marker is looked for. It is
// near the top of every bundled asset, and scanning a whole file for it would
// mean a large unrelated file was read in full before being rejected.
const markerScanLines = 128

// Marker renders the line an asset carries.
func Marker(a Asset) string {
	return fmt.Sprintf("%s id=%s schema=%d version=%s", markerPrefix, a.Source, a.SchemaVersion, a.Version)
}

// parseMarker extracts the integration identity a file declares.
func parseMarker(content string) (id string, schema int, version string, ok bool) {
	for i, line := range strings.SplitN(content, "\n", markerScanLines+1) {
		if i >= markerScanLines {
			break
		}
		rest, found := strings.CutPrefix(strings.TrimSpace(line), markerPrefix)
		if !found {
			continue
		}
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
					return "", 0, "", false
				}
				schema = n
			case "version":
				version = value
			}
		}
		return id, schema, version, id != "" && version != ""
	}
	return "", 0, "", false
}

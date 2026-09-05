package agentsession

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// OfficialSources are the integration sources Sidecar itself ships.
//
// Only a reference reported by one of these is ever marked Reported, and only a
// Reported reference is eligible for automatic resume. A same-cwd adapter
// discovery may propose a candidate for a human to confirm, but it can never
// enter this set, because "the newest conversation in this directory" is a
// guess and resuming the wrong conversation is worse than resuming none.
//
// "Ships" is the whole test, and it is checked against the adapters rather than
// against intent. This list once carried "sidecar.pi.extension" alongside a
// capability entry for Pi, and neither was backed by anything: there was no
// PiAdapter, no asset under internal/agentintegration/assets/pi, and no code
// path that installed an extension Pi would load. Nothing Sidecar wrote could
// ever produce a report carrying that source, so the only caller it could have
// had was a hook somebody wrote by hand, and trusting one of those is exactly
// the "resume a conversation nobody proved was the right one" the paragraph
// above refuses. Both were retracted, and Pi is back here now that the port in
// docs/plans/active/herdr-parity-close-the-gap.md, Slice 1, has shipped
// PiAdapter and internal/agentintegration/assets/pi/sidecar-lifecycle.js, which
// is what does the reporting.
//
// Being here is a statement about provenance and nothing else: it says Sidecar
// wrote the thing that sent the report, so the conversation reference may be
// resumed without a human confirming it. It is not a tier. Pi's capability entry
// is session-identity on docs-only evidence, and that is what decides how much
// its *state* reports are trusted; the two are deliberately separate registers.
func OfficialSources() []string {
	return []string{
		"sidecar.codex.hooks",
		"sidecar.claude.hooks",
		"sidecar.opencode.plugin",
		"sidecar.pi.extension",
		"sidecar.kilo.plugin",
		"sidecar.kimi.hooks",
		"sidecar.omp.extension",
		"sidecar.antigravity.hooks",
		"sidecar.copilot.hooks",
		"sidecar.cursor.hooks",
		"sidecar.grok.hooks",
		"sidecar.devin.hooks",
		"sidecar.droid.hooks",
		"sidecar.qodercli.hooks",
		"sidecar.qwen.hooks",
		"sidecar.mastracode.hooks",
		"sidecar.hermes.plugin",
	}
}

// OfficialSourceFor is the official integration source for a catalog family.
//
// A hook that omits --source gets its provider's own official source rather than
// an empty one, so the ordinary installed path produces a trusted, resumable
// reference and only a caller that deliberately names a different source gets an
// untrusted one.
//
// A provider with no shipped integration returns the empty string, and the
// report-session command turns that into a refusal naming the cause rather than
// letting the validator complain that the source is blank. A hand-written hook
// for such a provider can still report by naming --source explicitly, and what
// it gets is an untrusted reference, which is the honest amount of authority for
// an integration Sidecar did not write.
func OfficialSourceFor(kind string) string {
	switch strings.TrimSpace(kind) {
	case "codex":
		return "sidecar.codex.hooks"
	case "claude":
		return "sidecar.claude.hooks"
	case "opencode":
		return "sidecar.opencode.plugin"
	case "pi":
		return "sidecar.pi.extension"
	case "kilo":
		return "sidecar.kilo.plugin"
	case "kimi":
		return "sidecar.kimi.hooks"
	case "omp":
		return "sidecar.omp.extension"
	case "antigravity":
		return "sidecar.antigravity.hooks"
	case "copilot":
		return "sidecar.copilot.hooks"
	case "cursor":
		return "sidecar.cursor.hooks"
	case "grok":
		return "sidecar.grok.hooks"
	case "devin":
		return "sidecar.devin.hooks"
	case "droid":
		return "sidecar.droid.hooks"
	case "qodercli":
		return "sidecar.qodercli.hooks"
	case "qwen":
		return "sidecar.qwen.hooks"
	case "mastracode":
		return "sidecar.mastracode.hooks"
	case "hermes":
		return "sidecar.hermes.plugin"
	default:
		return ""
	}
}

// Official reports whether source is an official Sidecar integration.
func Official(source string) bool {
	for _, known := range OfficialSources() {
		if known == source {
			return true
		}
	}
	return false
}

// PiAgentDir resolves Pi's agent directory the way Pi itself does, from the home
// directory and the raw PI_CODING_AGENT_DIR value. An empty result means the
// answer is unknowable, which happens only when a tilde has to be expanded and
// the home directory is unknown.
//
// Pi's getAgentDir trims nothing itself, but Sidecar does, and deliberately: a
// PI_CODING_AGENT_DIR that is whitespace is a variable somebody exported without
// a value, not a directory named " ". What matters far more than which reading is
// right is that there is only one of them. This function exists because there
// were two -- the installer's, in agentintegration, and the store root's, here --
// and they had already disagreed on exactly that case, which is the failure this
// derivation is worth naming: the installer would write the extension into
// ~/.pi/agent/extensions while the approved root became "  /sessions", so the
// extension installed cleanly and every session binding it sent was refused for
// being outside the store.
//
// The tilde expansion is Pi's own (dist/config.js:420-426, verified against pi
// 0.84.3), not a convenience: getAgentDir expands a leading "~" before using the
// value, so a Sidecar that did not would read and write a literal directory
// named "~" while Pi used somewhere else entirely.
//
// It lives in this package rather than in agentintegration because this is the
// package that already owns where every provider keeps its files, and because it
// is a leaf: agentintegration may import it, and the reverse edge -- a trust
// primitive importing an installer that pulls in the UI packages -- is one worth
// refusing.
func PiAgentDir(home, override string) string {
	value := strings.TrimSpace(override)
	if value == "" {
		if home == "" {
			return ""
		}
		return filepath.Join(home, ".pi", "agent")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		if home == "" {
			return ""
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(value, "~"), "/"))
	}
	return value
}

// OMP's default config directory name, relative to home. It is what
// CONFIG_DIR_NAME is in OMP's own @oh-my-pi/pi-utils/src/dirs.ts.
const ompDefaultConfigDirName = ".omp"

// ompProfileName is OMP's own profile-name grammar
// (PROFILE_NAME_RE in @oh-my-pi/pi-utils/src/dirs.ts, verified against 18.1.8).
var ompProfileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._\-]{0,63}$`)

// ompWindowsReservedName is the other half of OMP's profile validation: Windows
// reserves these basenames and any BASENAME.<anything> form, so OMP refuses them
// rather than letting a directory creation fail later with a confusing errno.
var ompWindowsReservedName = regexp.MustCompile(`^(?i:CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(\..*)?$`)

// OmpProfile resolves OMP's active profile the way OMP resolves it.
//
// OMP_PROFILE is canonical and PI_PROFILE is a legacy fallback consulted only
// when OMP_PROFILE is not set at all — which is why this takes a separate
// `ompSet` rather than inferring it from an empty string. An explicitly empty
// OMP_PROFILE selects the default profile and suppresses PI_PROFILE, and that
// distinction decides which directory the extension has to be installed into.
//
// An invalid name resolves to no profile here. OMP throws on one, and its CLI
// turns that into "Invalid OMP profile" and refuses to start; its own module-load
// path takes the same lenient reading this does. Either way there is no session
// to report from, so the conservative answer is the default directory.
func OmpProfile(ompProfile string, ompSet bool, piProfile string) string {
	raw := piProfile
	if ompSet {
		raw = ompProfile
	}
	name := strings.TrimSpace(raw)
	if name == "" || name == "default" {
		return ""
	}
	if name == "." || name == ".." || strings.HasSuffix(name, ".") {
		return ""
	}
	if !ompProfileName.MatchString(name) || ompWindowsReservedName.MatchString(name) {
		return ""
	}
	return name
}

// OmpAgentDir resolves OMP's agent directory the way OMP itself does.
//
// It exists for the same reason PiAgentDir does, and it is called from the same
// two places: the installer, which writes the extension into
// <this>/extensions, and the store root below, which decides which session paths
// a report from that extension may name. Deriving it twice is how those two
// drifted for Pi, so there is one derivation and both call it.
//
// The derivation, read from OMP 18.1.8's own @oh-my-pi/pi-utils/src/dirs.ts
// rather than from Herdr's installer, because the two disagree in one place:
//
//	configRoot = $HOME/${PI_CONFIG_DIR:-.omp}            (getBaseConfigRoot)
//	           + /profiles/<profile>  when one is active  (getProfileConfigRoot)
//	agentDir   = configRoot/agent
//	           unless no profile is active and PI_CODING_AGENT_DIR is set, in
//	           which case it is that value, path.resolve'd (DirResolver)
//
// Two facts in there are easy to get wrong and both were checked against the
// source. A NAMED PROFILE IGNORES PI_CODING_AGENT_DIR entirely
// (`const agentDirOverride = profile ? undefined : options.agentDirOverride`),
// so an installer that honoured the variable under a profile would write where
// OMP never looks. And PI_CODING_AGENT_DIR IS NOT TILDE-EXPANDED: OMP calls
// path.resolve on it, where Pi calls its own tilde expansion and Herdr's
// omp_extension_dir expands a tilde too. path.resolve makes a relative value —
// "~/..." included, since a leading tilde is not special to it — resolve against
// whatever directory OMP was launched from, which Sidecar cannot know. So a
// non-absolute override returns the empty string here, meaning "unknowable", and
// the adapter refuses with that reason rather than guessing a directory.
func OmpAgentDir(home, configDirName, agentDirOverride, profile string) string {
	if override := strings.TrimSpace(agentDirOverride); override != "" && profile == "" {
		if !filepath.IsAbs(override) {
			return ""
		}
		return filepath.Clean(override)
	}
	if home == "" {
		return ""
	}
	name := strings.TrimSpace(configDirName)
	if name == "" {
		name = ompDefaultConfigDirName
	}
	root := filepath.Join(home, name)
	if profile != "" {
		root = filepath.Join(root, "profiles", profile)
	}
	return filepath.Join(root, "agent")
}

// Roots describes where a provider is allowed to keep its conversations.
//
// A path reference outside every root is refused rather than stored. The point
// is not that a provider would lie; it is that a hook is untrusted local input,
// and "an absolute path the agent chose" is otherwise a instruction to read an
// arbitrary file later, at restore time, with Sidecar's privileges.
type Roots struct {
	// Home is the user's home directory. Injected so tests do not depend on
	// the developer's own tree.
	Home string
	// Env reads an environment variable. Nil means os.Getenv.
	Env func(string) string
}

func (r Roots) env(name string) string {
	if r.Env == nil {
		return os.Getenv(name)
	}
	return r.Env(name)
}

// OSRoots reads the ambient environment.
func OSRoots() Roots {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return Roots{Home: home}
}

// For returns the approved store roots for a catalog family id.
//
// An unknown provider returns no roots, which makes every path reference for it
// a refusal. That is the intended direction: a provider earns path references by
// having its store location recorded here, not by reporting one.
func (r Roots) For(kind string) []string {
	switch strings.TrimSpace(kind) {
	case "codex":
		base := r.env("CODEX_HOME")
		if base == "" {
			if r.Home == "" {
				return nil
			}
			base = filepath.Join(r.Home, ".codex")
		}
		return []string{
			filepath.Join(base, "sessions"),
			filepath.Join(base, "archived_sessions"),
		}
	case "claude":
		base := r.env("CLAUDE_CONFIG_DIR")
		if base == "" {
			if r.Home == "" {
				return nil
			}
			base = filepath.Join(r.Home, ".claude")
		}
		return []string{filepath.Join(base, "projects")}
	case "opencode":
		base := r.env("XDG_DATA_HOME")
		if base == "" {
			if r.Home == "" {
				return nil
			}
			base = filepath.Join(r.Home, ".local", "share")
		}
		return []string{filepath.Join(base, "opencode")}
	case "pi":
		// Pi keeps its conversations at getAgentDir()/sessions
		// (dist/config.js:455-457). The agent directory itself is derived by
		// PiAgentDir below, which is also what the installer calls, because the
		// two used to derive it separately and had already drifted: the
		// installer trimmed the environment value and this did not, so a
		// whitespace-only PI_CODING_AGENT_DIR installed the extension into
		// ~/.pi/agent/extensions while the approved root became "  /sessions"
		// and every binding that extension sent was refused.
		//
		// This root is what makes Pi's session binding more than decoration: the
		// installed extension reports the session FILE, because a path names the
		// exact transcript a restore would resume where an id alone does not, and
		// a path reference outside every approved root is refused rather than
		// stored. Without an entry here the binding would be refused on every
		// report and the session-identity tier would be a claim about nothing.
		base := PiAgentDir(r.Home, r.env("PI_CODING_AGENT_DIR"))
		if base == "" {
			return nil
		}
		return []string{filepath.Join(base, "sessions")}
	case "omp":
		// OMP keeps its conversations at getSessionsDir(), which is
		// <agentDir>/sessions with one twist: on linux and darwin, when
		// XDG_DATA_HOME is set and $XDG_DATA_HOME/omp exists and no agent-dir
		// override is in play, the agent/ prefix is flattened away and sessions
		// live at $XDG_DATA_HOME/omp/sessions instead (DirResolver, verified
		// against OMP 18.1.8). Both are listed, because this is an allowlist and
		// the existence check that decides between them is OMP's to make at its
		// own startup rather than Sidecar's to make at report time.
		//
		// The profile is resolved under the lenient reading of OMP_PROFILE — an
		// empty value is treated as unset — because a func(string) string cannot
		// tell an empty variable from an absent one, and OMP's own rule turns on
		// exactly that. The installer reads it precisely, through os.LookupEnv.
		// Where the two readings can differ, the DEFAULT-profile directory is
		// listed as well, so this side is never the one that refuses a binding
		// the installer's own extension sent. It is an allowlist, so the cost of
		// the extra entry is one directory OMP itself may use.
		var roots []string
		add := func(profile string) {
			base := OmpAgentDir(r.Home, r.env("PI_CONFIG_DIR"), r.env("PI_CODING_AGENT_DIR"), profile)
			if base == "" {
				return
			}
			roots = append(roots, filepath.Join(base, "sessions"))
			data := r.env("XDG_DATA_HOME")
			if data == "" || (profile == "" && strings.TrimSpace(r.env("PI_CODING_AGENT_DIR")) != "") {
				return
			}
			app := filepath.Join(data, "omp")
			if profile != "" {
				app = filepath.Join(app, "profiles", profile)
			}
			roots = append(roots, filepath.Join(app, "sessions"))
		}
		omp := r.env("OMP_PROFILE")
		profile := OmpProfile(omp, omp != "", r.env("PI_PROFILE"))
		add(profile)
		// The two readings can only differ when the profile came from PI_PROFILE,
		// because that is the case where an OMP_PROFILE this side cannot see may be
		// set to an empty value and be selecting the default profile instead. A
		// non-empty OMP_PROFILE is the same answer under either reading, so no
		// second directory is listed for it.
		if profile != "" && omp == "" {
			add("")
		}
		return roots
	case "muse":
		base := r.env("XDG_DATA_HOME")
		if base == "" {
			if r.Home == "" {
				return nil
			}
			base = filepath.Join(r.Home, ".local", "share")
		}
		return []string{filepath.Join(base, "muse", "sessions")}
	default:
		return nil
	}
}

// WithinRoots reports whether an already-validated absolute path lies inside one
// of the provider's approved roots.
//
// Containment is compared on cleaned paths with a separator boundary, so
// "/home/u/.codexsessions" does not pass as being inside "/home/u/.codex".
func (r Roots) WithinRoots(kind, path string) error {
	roots := r.For(kind)
	if len(roots) == 0 {
		return fmt.Errorf("%w: no approved store root is recorded for provider %q", ErrOutsideStoreRoot, kind)
	}
	// Resolve symlinks on BOTH sides before comparing. A lexical prefix test is
	// not containment: a symlink planted inside an approved root passes it while
	// pointing anywhere on the filesystem, and the target is what actually gets
	// opened. Resolving the root too means a store that is itself a symlink --
	// a dotfiles setup, a relocated home -- still matches its own contents.
	//
	// A path that does not exist yet cannot be resolved, and that is not a
	// refusal: EvalSymlinks fails on a missing leaf, so fall back to the
	// lexical form for the part that does not exist. What must never happen is
	// accepting a path whose RESOLVED form escapes, which is why resolution is
	// preferred wherever it succeeds.
	clean := resolvePath(path)
	for _, root := range roots {
		root = resolvePath(root)
		if clean == root {
			continue // the root itself is a directory, not a transcript
		}
		if strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not under any of: %s", ErrOutsideStoreRoot, path, strings.Join(roots, ", "))
}

// Report is one integration's claim about the conversation in its pane.
//
// It carries only what a provider is allowed to choose: which conversation it is
// talking about. Who it is talking about — host, server, pane, process
// generation — is derived by the caller from the environment and live tmux and
// is never reachable from a flag.
type Report struct {
	// Kind is the catalog family id of the provider.
	Kind string
	// RefKind and Value name the conversation.
	RefKind RefKind
	Value   string
	// Source is the reporting integration.
	Source string
	// Generation is the provider process generation the report came from.
	Generation string
}

// Validate turns an untrusted report into a Ref, or refuses.
//
// Everything a later stage relies on is decided here: bounds, character rules,
// path shape, store-root containment, provider support for the kind, and
// whether the source is official enough to mark the result auto-resumable.
func Validate(rep Report, roots Roots, now func() time.Time) (Ref, error) {
	if err := ValidateSource(rep.Source); err != nil {
		return Ref{}, err
	}
	if strings.TrimSpace(rep.Kind) == "" {
		return Ref{}, fmt.Errorf("%w: the provider kind is empty", ErrInvalidRef)
	}
	if err := ValidateValue(rep.RefKind, rep.Value); err != nil {
		return Ref{}, err
	}
	if rep.RefKind == RefPath {
		if err := roots.WithinRoots(rep.Kind, rep.Value); err != nil {
			return Ref{}, err
		}
	}
	at := time.Time{}
	if now != nil {
		at = now().UTC()
	}
	return Ref{
		Kind:       rep.RefKind,
		Value:      rep.Value,
		Source:     rep.Source,
		Reported:   Official(rep.Source),
		Generation: rep.Generation,
		ReportedAt: at,
	}, nil
}

// resolvePath returns path with symlinks resolved as far as the filesystem
// allows, and cleaned otherwise.
//
// EvalSymlinks refuses a path whose leaf does not exist, which is ordinary here:
// a provider may report a transcript Sidecar has not seen yet. So the deepest
// existing ancestor is resolved and the remainder appended. That still closes
// the escape this exists to stop, because the escape has to go THROUGH a
// component that exists.
func resolvePath(path string) string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	// Walk up to the deepest ancestor that resolves, then re-append the rest.
	rest := ""
	dir := clean
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return clean
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
	}
}

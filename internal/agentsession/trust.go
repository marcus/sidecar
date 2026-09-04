package agentsession

import (
	"fmt"
	"os"
	"path/filepath"
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
		"sidecar.devin.hooks",
		"sidecar.droid.hooks",
		"sidecar.qodercli.hooks",
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
	case "devin":
		return "sidecar.devin.hooks"
	case "droid":
		return "sidecar.droid.hooks"
	case "qodercli":
		return "sidecar.qodercli.hooks"
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

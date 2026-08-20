package terminallink

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Kind classifies one detected span.
type Kind string

const (
	KindURL   Kind = "url"
	KindFile  Kind = "file"
	KindIssue Kind = "issue"
	KindDiff  Kind = "diff"
	// KindResource is the one generic kind every external provider shares.
	// Which provider and matcher produced it lives in Extra, so adding a
	// provider never adds a kind.
	KindResource Kind = "resource"
	// KindSession is a tmux session a surface can attach to. Only sessions
	// Sidecar itself named are detected — see sessionPattern.
	KindSession Kind = "session"
)

// Activatable reports whether a kind is one a host can act on, and so one
// that may be underlined. Both terminal surfaces asked this question with
// their own copy of the same switch; a new kind must not be able to reach one
// surface's decoration and miss the other's hit testing.
func Activatable(k Kind) bool {
	switch k {
	case KindURL, KindFile, KindIssue, KindDiff, KindResource, KindSession:
		return true
	default:
		return false
	}
}

// MaxNewDiffResolves is the host budget for new git existence checks per
// (surface, buffer revision). Further unique spec tokens stay plain text
// until the next revision.
const MaxNewDiffResolves = 16

// Extra holds kind-specific fields. Line is 1-based and zero when the token
// has no :line suffix. Raw is the original file or git-spec token when Value
// is rewritten by a Resolver. Provider and Matcher are set only for
// KindResource and, with the span's Value as the locator, form the resource
// reference a host activates.
type Extra struct {
	Line     int
	Raw      string
	Provider string
	Matcher  string
}

// Span is one non-overlapping match in visual columns of the stripped line.
// EndCol is inclusive.
type Span struct {
	Kind     Kind
	StartCol int
	EndCol   int
	Value    string
	Extra    Extra
}

// Resolver reports whether a file token exists. value is what the host should
// store (typically a path relative to the selected root). ok=false drops the
// span. A nil Resolver skips existence-gated file spans (bare paths).
type Resolver func(raw string) (value string, extra Extra, ok bool)

// DiffResolver reports whether a git-spec token exists in the selected
// checkout. Extra.Raw is the matched token; Extra.Line is unused. ok=false
// drops the span. A nil DiffResolver emits no git-spec spans.
type DiffResolver func(raw string) (value string, extra Extra, ok bool)

var (
	urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)
	// path:line, including absolute and ~/ prefixes.
	pathLinePattern = regexp.MustCompile(
		`(?:^|[\s(\[])((?:~/|\.{0,2}/|/)?[A-Za-z0-9_][A-Za-z0-9_./-]*\.[A-Za-z0-9_+-]+):([1-9][0-9]*)`,
	)
	// Bare path with a suffix. Existence is the host Resolver's job.
	bareFilePattern = regexp.MustCompile(
		`(?:^|[\s(\x5b` + "`" + `])((?:~/|\.{0,2}/|/)?[^\s()\x5b\x5d` + "`" + `<>:"']+\.[A-Za-z0-9_+-]+[.,;!?)}\x5d` + "`" + `]*)`,
	)
	// Current td id shape. Title matching is out of scope until a split binds
	// this kind to a td pane — not to the issue-preview modal.
	issuePattern = regexp.MustCompile(`\btd-[0-9a-fA-F]{4,}\b`)
	// The same shape anchored, for callers holding a stored id rather than a
	// line of output.
	issueIDPattern = regexp.MustCompile(`^td-[0-9a-fA-F]{4,}$`)
	// Sidecar-owned tmux session names, and only those. A tmux session name
	// is otherwise free text — "main", "work", "notes" — and a pattern that
	// matched free text would underline half of ordinary prose. The two
	// prefixes here are the only sessions anything can attach to (a shell,
	// `sidecar-sh-<project>-<n>`, and a worktree agent, `sidecar-ws-<slug>`);
	// `sidecar-tp-`, `sidecar-edit-` and friends are internal panes that no
	// surface attaches, so detecting them would promise a jump that cannot
	// happen. Whole-token matching is enforced separately, so a path or a log
	// filename containing a session name is not a session.
	sessionPattern = regexp.MustCompile(`\bsidecar-(?:sh|ws)-[A-Za-z0-9][A-Za-z0-9_-]{0,63}`)
	// The same shape anchored, for a caller holding a stored session name.
	sessionNamePattern = regexp.MustCompile(`^sidecar-(?:sh|ws)-[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	// Lowercase hex only. Mixed-case and HEAD/branch names are CLI-only.
	gitRevPattern        = regexp.MustCompile(`[0-9a-f]{7,64}`)
	gitDottedPattern     = regexp.MustCompile(`[0-9a-f]{7,64}(?:\.\.\.|\.\.)[0-9a-f]{7,64}`)
	gitCommitWordPattern = regexp.MustCompile(`\bcommit[ \t]+[0-9a-f]{7,64}`)
)

// IssueID reports whether value is a td id of the shape this package detects.
// A host that restores an id from disk asks here rather than trusting the file:
// the click path can only ever produce this shape, and the id becomes an argv
// element of the fetch.
func IssueID(value string) bool {
	return issueIDPattern.MatchString(value)
}

// SessionName reports whether value is a Sidecar-owned tmux session name of
// the shape this package detects. A host restoring a name from disk — a stored
// notification target, say — asks here rather than trusting the file: the name
// becomes an argv element of a tmux command.
func SessionName(value string) bool {
	return sessionNamePattern.MatchString(value)
}

// Options collects everything a scan may consult. The zero value scans only
// the kinds that need no host callback and no configuration.
type Options struct {
	// Resolve existence-gates file tokens. Nil skips bare-file spans.
	Resolve Resolver
	// ResolveDiff existence-gates git specs. Nil emits no git-spec spans.
	ResolveDiff DiffResolver
	// Matchers are the live external matchers in precedence order. Empty
	// means no provider is ready, which must read as ordinary text.
	Matchers []ResourceMatcher
}

// Scan finds URL, file, issue, and git-spec spans in a terminal line. It is
// ScanWith without external matchers, which is what callers that have no
// provider snapshot want.
func Scan(line string, resolve Resolver, resolveDiff DiffResolver) []Span {
	return ScanWith(line, Options{Resolve: resolve, ResolveDiff: resolveDiff})
}

// ScanWith finds every span kind in a terminal line.
//
// line may still contain ANSI; it is stripped before matching. Overlaps are
// resolved first-kind-wins in this order: url, path:line, resolved bare
// files, issue, git spec, external resource. File tokens need a suffix. Bare
// files (and, when a Resolver is supplied, path:line) are existence-gated.
// Git specs are existence-gated by resolveDiff and scanned dotted →
// commit-word → rev.
//
// External matchers run last so built-in precedence cannot be bid away by a
// provider's priority, and matching stays pure: no process starts and no I/O
// happens here.
func ScanWith(line string, opts Options) []Span {
	plain := ansi.Strip(line)
	var spans []Span
	spans = append(spans, scanURLs(plain)...)
	spans = append(spans, scanPathLines(plain, spans, opts.Resolve)...)
	if opts.Resolve != nil {
		spans = append(spans, scanBareFiles(plain, spans, opts.Resolve)...)
	}
	// Sessions before issues and git specs: a worktree session named after an
	// issue (sidecar-ws-td-331dbf19) or ending in hex must be the whole token
	// it is, not the id hiding inside it.
	spans = append(spans, scanSessions(plain, spans)...)
	spans = append(spans, scanIssues(plain, spans)...)
	if opts.ResolveDiff != nil {
		spans = append(spans, scanGitSpecs(plain, spans, opts.ResolveDiff)...)
	}
	spans = append(spans, scanResources(plain, spans, opts.Matchers)...)
	return spans
}

func scanURLs(plain string) []Span {
	var spans []Span
	for _, loc := range urlPattern.FindAllStringIndex(plain, -1) {
		value, ok := SafeHTTPURL(plain[loc[0]:loc[1]])
		if !ok {
			continue
		}
		endByte := loc[0] + len(value)
		spans = append(spans, Span{
			Kind:     KindURL,
			StartCol: colAt(plain, loc[0]),
			EndCol:   colAt(plain, endByte) - 1,
			Value:    value,
		})
	}
	return spans
}

func scanPathLines(plain string, existing []Span, resolve Resolver) []Span {
	var spans []Span
	for _, loc := range pathLinePattern.FindAllStringSubmatchIndex(plain, -1) {
		if len(loc) < 6 || loc[2] < 0 || loc[4] < 0 {
			continue
		}
		start, end := loc[2], loc[3]
		path := plain[start:end]
		if containsControl(path) || overlaps(plain, existing, spans, start, end) {
			continue
		}
		lineNo, err := strconv.Atoi(plain[loc[4]:loc[5]])
		if err != nil {
			continue
		}
		value := path
		extra := Extra{Line: lineNo}
		if resolve != nil {
			resolved, resolvedExtra, ok := resolve(path)
			if !ok {
				continue
			}
			value = resolved
			extra = resolvedExtra
			extra.Line = lineNo
			if extra.Raw == "" {
				extra.Raw = path
			}
		}
		spans = append(spans, Span{
			Kind:     KindFile,
			StartCol: colAt(plain, start),
			EndCol:   colAt(plain, end) - 1,
			Value:    value,
			Extra:    extra,
		})
	}
	return spans
}

func scanBareFiles(plain string, existing []Span, resolve Resolver) []Span {
	var spans []Span
	for _, loc := range bareFilePattern.FindAllStringSubmatchIndex(plain, -1) {
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}
		start, end := loc[2], loc[3]
		value := strings.TrimRight(plain[start:end], ".,;!?)]}`")
		end = start + len(value)
		// The regexp stops at the suffix so it can retain punctuation for
		// trimming. Require the next byte to be a token boundary; otherwise
		// README.md5 and similar prefixes would become surprising links.
		matchEnd := loc[3]
		if matchEnd < len(plain) && !isBareFileRightBoundary(plain[matchEnd]) {
			continue
		}
		if value == "" || containsControl(value) || overlaps(plain, existing, spans, start, end) {
			continue
		}
		resolved, extra, ok := resolve(value)
		if !ok {
			continue
		}
		if extra.Raw == "" {
			extra.Raw = value
		}
		spans = append(spans, Span{
			Kind:     KindFile,
			StartCol: colAt(plain, start),
			EndCol:   colAt(plain, end) - 1,
			Value:    resolved,
			Extra:    extra,
		})
	}
	return spans
}

// scanSessions finds Sidecar-owned tmux session names. Whole-token only: the
// same rule issue ids follow, so `/tmp/sidecar-sh-repo-1.log` and
// `sidecar-sh-repo-1x` are text, not sessions.
func scanSessions(plain string, existing []Span) []Span {
	var spans []Span
	for _, loc := range sessionPattern.FindAllStringIndex(plain, -1) {
		start, end := loc[0], loc[1]
		if overlaps(plain, existing, spans, start, end) || !issueTokenWhole(plain, start, end) {
			continue
		}
		spans = append(spans, Span{
			Kind:     KindSession,
			StartCol: colAt(plain, start),
			EndCol:   colAt(plain, end) - 1,
			Value:    plain[start:end],
		})
	}
	return spans
}

func scanIssues(plain string, existing []Span) []Span {
	var spans []Span
	for _, loc := range issuePattern.FindAllStringIndex(plain, -1) {
		start, end := loc[0], loc[1]
		if overlaps(plain, existing, spans, start, end) || !issueTokenWhole(plain, start, end) {
			continue
		}
		spans = append(spans, Span{
			Kind:     KindIssue,
			StartCol: colAt(plain, start),
			EndCol:   colAt(plain, end) - 1,
			Value:    plain[start:end],
		})
	}
	return spans
}

func scanGitSpecs(plain string, existing []Span, resolve DiffResolver) []Span {
	var spans []Span
	for _, re := range []*regexp.Regexp{gitDottedPattern, gitCommitWordPattern, gitRevPattern} {
		spans = append(spans, scanGitPattern(plain, existing, spans, re, resolve)...)
	}
	return spans
}

func scanGitPattern(plain string, existing, pending []Span, re *regexp.Regexp, resolve DiffResolver) []Span {
	var spans []Span
	for _, loc := range re.FindAllStringIndex(plain, -1) {
		start, end := loc[0], loc[1]
		if overlaps(plain, existing, pending, start, end) || overlapsBytes(plain, spans, start, end) || !issueTokenWhole(plain, start, end) {
			continue
		}
		raw := plain[start:end]
		if containsControl(raw) {
			continue
		}
		value, extra, ok := resolve(raw)
		if !ok {
			continue
		}
		if extra.Raw == "" {
			extra.Raw = raw
		}
		if value == "" {
			value = raw
		}
		spans = append(spans, Span{
			Kind:     KindDiff,
			StartCol: colAt(plain, start),
			EndCol:   colAt(plain, end) - 1,
			Value:    value,
			Extra:    extra,
		})
	}
	return spans
}

// issueTokenWhole reports whether the matched id is the whole token rather than
// a stem inside a longer one. `\b` is satisfied by the `.` in td-a1b2c3.md, so
// a file that failed to resolve — deleted, or outside the root, so no file span
// covers it — would otherwise underline its first nine characters as an issue
// and fetch something that was never an id.
//
// A trailing sentence period is not a continuation: only a `.` that is followed
// by more of the token is.
func issueTokenWhole(plain string, start, end int) bool {
	if start > 0 && isIssueTokenByte(plain[start-1]) {
		return false
	}
	if end >= len(plain) {
		return true
	}
	next := plain[end]
	if !isIssueTokenByte(next) {
		return true
	}
	if next != '.' {
		return false
	}
	return end+1 >= len(plain) || !isAlphanumeric(plain[end+1])
}

// isIssueTokenByte names the bytes that continue a path or filename token. The
// alphanumerics cannot appear adjacent to a match — `\b` already excludes them —
// so what this really names is the punctuation a token is built from.
func isIssueTokenByte(b byte) bool {
	return b == '.' || b == '/' || b == '-' || b == '_' || b == '~' || isAlphanumeric(b)
}

func isAlphanumeric(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isBareFileRightBoundary(next byte) bool {
	return next == ' ' || next == '\t' || next == '\r' || next == '\n' ||
		next == ')' || next == ']' || next == '}' || next == '`'
}

func overlapsBytes(plain string, existing []Span, start, end int) bool {
	startCol := colAt(plain, start)
	endCol := colAt(plain, end) - 1
	for _, span := range existing {
		if startCol <= span.EndCol && endCol >= span.StartCol {
			return true
		}
	}
	return false
}

func overlaps(plain string, existing, pending []Span, start, end int) bool {
	return overlapsBytes(plain, existing, start, end) || overlapsBytes(plain, pending, start, end)
}

func colAt(plain string, byteIndex int) int {
	if byteIndex < 0 {
		return 0
	}
	if byteIndex > len(plain) {
		byteIndex = len(plain)
	}
	return ansi.StringWidth(plain[:byteIndex])
}

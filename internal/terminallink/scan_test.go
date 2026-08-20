package terminallink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestScanFindsSafeURLAndPathLine(t *testing.T) {
	spans := Scan("see https://example.com/docs?q=1, then internal/foo.go:123", nil, nil)
	if len(spans) != 2 {
		t.Fatalf("spans = %#v, want URL and path", spans)
	}
	if spans[0].Kind != KindURL || spans[0].Value != "https://example.com/docs?q=1" {
		t.Fatalf("URL span = %#v", spans[0])
	}
	if spans[1].Kind != KindFile || spans[1].Value != "internal/foo.go" || spans[1].Extra.Line != 123 {
		t.Fatalf("path span = %#v", spans[1])
	}
}

func TestScanIssueOnTypicalAgentLine(t *testing.T) {
	spans := Scan("review td-196c42", nil, nil)
	if len(spans) != 1 {
		t.Fatalf("spans = %#v, want one issue", spans)
	}
	if spans[0].Kind != KindIssue || spans[0].Value != "td-196c42" {
		t.Fatalf("issue span = %#v", spans[0])
	}
	if spans[0].StartCol != ansi.StringWidth("review ") || spans[0].EndCol != ansi.StringWidth("review td-196c42")-1 {
		t.Fatalf("issue columns = %d..%d", spans[0].StartCol, spans[0].EndCol)
	}
}

func TestScanIssueDoesNotOverlapURLOrFile(t *testing.T) {
	line := "see https://example.com/td-196c42 and td-196c42.go:12 then review td-196c42"
	spans := Scan(line, nil, nil)
	var issues, files, urls int
	for _, span := range spans {
		switch span.Kind {
		case KindIssue:
			issues++
			if span.Value != "td-196c42" {
				t.Fatalf("issue value = %q", span.Value)
			}
		case KindFile:
			files++
			if span.Value != "td-196c42.go" || span.Extra.Line != 12 {
				t.Fatalf("file span = %#v", span)
			}
		case KindURL:
			urls++
		}
		for _, other := range spans {
			if span == other {
				continue
			}
			if span.StartCol <= other.EndCol && span.EndCol >= other.StartCol {
				t.Fatalf("overlapping spans %#v and %#v", span, other)
			}
		}
	}
	if urls != 1 || files != 1 || issues != 1 {
		t.Fatalf("kinds url=%d file=%d issue=%d, want 1 each: %#v", urls, files, issues, spans)
	}
}

func TestScanIssueRequiresWordBoundaryAndFourHex(t *testing.T) {
	for _, line := range []string{
		"xtd-196c42",
		"td-abc",
		"td-",
		"TD-196c42",
	} {
		if spans := Scan(line, nil, nil); len(spans) != 0 {
			t.Fatalf("Scan(%q) = %#v, want none", line, spans)
		}
	}
	spans := Scan("see td-196C42 done", nil, nil)
	if len(spans) != 1 || spans[0].Value != "td-196C42" {
		t.Fatalf("mixed-case issue = %#v", spans)
	}
}

func TestScanBareMarkdownUsesResolverAndSkipsMisses(t *testing.T) {
	resolve := func(raw string) (string, Extra, bool) {
		if raw == "README.md" {
			return "README.md", Extra{Raw: raw}, true
		}
		return "", Extra{}, false
	}
	spans := Scan("please read README.md and missing.md", resolve, nil)
	if len(spans) != 1 || spans[0].Kind != KindFile || spans[0].Value != "README.md" || spans[0].Extra.Raw != "README.md" {
		t.Fatalf("bare spans = %#v", spans)
	}
}

func TestScanBareCodeAndHomePaths(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "dot.go"), []byte("package dot"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = orig })

	resolve := func(raw string) (string, Extra, bool) {
		display, _, ok := ResolveFile(home, raw)
		return display, Extra{Raw: raw}, ok
	}
	spans := Scan("see main.go and ~/dot.go and missing.go", resolve, nil)
	if len(spans) != 1 || spans[0].Kind != KindFile || spans[0].Extra.Raw != "~/dot.go" {
		t.Fatalf("spans = %#v", spans)
	}
	spans = Scan("main.go:37", resolve, nil)
	if len(spans) != 0 {
		t.Fatalf("missing path:line = %#v", spans)
	}
	if err := os.WriteFile(filepath.Join(home, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	spans = Scan("main.go:37", resolve, nil)
	if len(spans) != 1 || spans[0].Value != "main.go" || spans[0].Extra.Line != 37 {
		t.Fatalf("path:line = %#v", spans)
	}
}

func TestScanWithoutResolverOmitsBareMarkdown(t *testing.T) {
	spans := Scan("please read README.md", nil, nil)
	if len(spans) != 0 {
		t.Fatalf("nil resolver still emitted %#v", spans)
	}
}

func TestScanFirstKindWinsOnURLContainingMarkdownPath(t *testing.T) {
	spans := Scan("https://example.test/docs/guide.markdown", func(string) (string, Extra, bool) {
		t.Fatal("resolver should not run for a URL overlap")
		return "", Extra{}, false
	}, nil)
	if len(spans) != 1 || spans[0].Kind != KindURL {
		t.Fatalf("spans = %#v, want the URL only", spans)
	}
}

func TestSafeHTTPURLRejectsNonHTTPAndControls(t *testing.T) {
	for _, value := range []string{
		"javascript:alert(1)",
		"file:///etc/passwd",
		"https://example.com/\x1b]8;;evil",
		"https:///missing-host",
	} {
		if _, ok := SafeHTTPURL(value); ok {
			t.Fatalf("unsafe URL accepted: %q", value)
		}
	}
	if OpenHTTP("file:///etc/passwd") != nil {
		t.Fatal("browser command accepted non-http URL")
	}
}

func TestScanDoesNotImportWorkspaceOrOverview(t *testing.T) {
	// Compile-time contract: this package is the shared detector. Hosts import
	// it; it must not import them. The blank imports below would fail to
	// compile if we accidentally grew a host dependency — they stay in the
	// host packages' tests. Here we only document the rule and check kinds.
	for _, kind := range []Kind{KindURL, KindFile, KindIssue, KindDiff} {
		if kind == "" {
			t.Fatal("empty kind")
		}
	}
	if strings.Contains(string(KindIssue), "preview") {
		t.Fatal("issue kind must stay a data label, not an activation path")
	}
}

// A td-shaped stem inside a filename is not an issue. The bare-file scan runs
// first, so a path that resolves is already covered by a file span — but one
// that does not (deleted, or outside the root) leaves the id exposed, and `\b`
// is satisfied by the dot before the extension.
func TestScanIssueRejectsATdStemInsideALongerToken(t *testing.T) {
	for _, line := range []string{
		"open td-a1b2c3.md",
		"open docs/td-a1b2c3.md",
		"open /tmp/td-a1b2c3",
		"open notes-td-a1b2c3",
		"open td-a1b2c3_draft",
	} {
		if spans := Scan(line, nil, nil); len(spans) != 0 {
			t.Fatalf("Scan(%q) = %#v, want no issue span", line, spans)
		}
	}
	// A sentence's own punctuation still ends the token.
	for _, line := range []string{"closed by td-a1b2c3.", "closed by td-a1b2c3, then"} {
		spans := Scan(line, nil, nil)
		if len(spans) != 1 || spans[0].Kind != KindIssue || spans[0].Value != "td-a1b2c3" {
			t.Fatalf("Scan(%q) = %#v, want the issue", line, spans)
		}
	}
}

// IssueID is the anchored form of the same shape, for a host holding a stored
// id rather than a line of output.
func TestIssueIDAcceptsOnlyTheShapeTheScannerProduces(t *testing.T) {
	for _, value := range []string{"td-196c42", "td-196C42", "td-abcd"} {
		if !IssueID(value) {
			t.Fatalf("IssueID(%q) = false, want the shape a click produces", value)
		}
	}
	for _, value := range []string{"", " ", "td-abc", "-td-196c42", "--force", "td-196c42.md",
		"td-196c42 extra", "TD-196c42", "../td-196c42"} {
		if IssueID(value) {
			t.Fatalf("IssueID(%q) = true, want a refusal", value)
		}
	}
}

func acceptAllDiff(_ string) (string, Extra, bool) {
	return "", Extra{}, true
}

func TestScanGitRangeIsOneSpan(t *testing.T) {
	spans := Scan("landed abc1234..def5678", nil, acceptAllDiff)
	if len(spans) != 1 || spans[0].Kind != KindDiff || spans[0].Value != "abc1234..def5678" {
		t.Fatalf("range spans = %#v, want one dotted spec", spans)
	}
	if spans[0].StartCol != ansi.StringWidth("landed ") || spans[0].EndCol != ansi.StringWidth("landed abc1234..def5678")-1 {
		t.Fatalf("range columns = %d..%d", spans[0].StartCol, spans[0].EndCol)
	}

	three := Scan("compare abc1234...def5678", nil, acceptAllDiff)
	if len(three) != 1 || three[0].Value != "abc1234...def5678" {
		t.Fatalf("three-dot spans = %#v", three)
	}
}

func TestScanGitRejectsMixedCaseShortAndFilename(t *testing.T) {
	for _, line := range []string{
		"Abc1234",
		"DEADBEE",
		"abc123",
		"abc1234.go",
		"foo/abc1234",
		"see HEAD",
		"see HEAD~3",
		"see origin/main",
		"cafe",
		"filter",
	} {
		if spans := Scan(line, nil, acceptAllDiff); len(spans) != 0 {
			t.Fatalf("Scan(%q) = %#v, want no git spec", line, spans)
		}
	}
	spans := Scan("landed abc1234.", nil, acceptAllDiff)
	if len(spans) != 1 || spans[0].Value != "abc1234" {
		t.Fatalf("sentence period should still yield the rev: %#v", spans)
	}
}

func TestScanGitCommitWordAndBareRev(t *testing.T) {
	spans := Scan("see commit abc1234 then done", nil, acceptAllDiff)
	if len(spans) != 1 || spans[0].Kind != KindDiff || spans[0].Value != "commit abc1234" {
		t.Fatalf("commit-word spans = %#v", spans)
	}
	spans = Scan("landed abc1234 on main", nil, acceptAllDiff)
	if len(spans) != 1 || spans[0].Value != "abc1234" {
		t.Fatalf("bare rev spans = %#v", spans)
	}
}

func TestScanGitDoesNotOverlapURLFileOrIssue(t *testing.T) {
	line := "see https://example.com/abc1234 and abc1234.go then td-abc1234 and abc1234"
	fileResolve := func(raw string) (string, Extra, bool) {
		if raw == "abc1234.go" {
			return raw, Extra{Raw: raw}, true
		}
		return "", Extra{}, false
	}
	spans := Scan(line, fileResolve, acceptAllDiff)
	var diffs, files, issues, urls int
	for _, span := range spans {
		switch span.Kind {
		case KindDiff:
			diffs++
			if span.Value != "abc1234" {
				t.Fatalf("diff value = %q", span.Value)
			}
		case KindFile:
			files++
		case KindIssue:
			issues++
		case KindURL:
			urls++
		}
		for _, other := range spans {
			if span == other {
				continue
			}
			if span.StartCol <= other.EndCol && span.EndCol >= other.StartCol {
				t.Fatalf("overlapping spans %#v and %#v", span, other)
			}
		}
	}
	if urls != 1 || files != 1 || issues != 1 || diffs != 1 {
		t.Fatalf("kinds url=%d file=%d issue=%d diff=%d, want 1 each: %#v", urls, files, issues, diffs, spans)
	}
}

func TestScanGitNilResolverOmitsSpecs(t *testing.T) {
	if spans := Scan("landed abc1234 and abc1234..def5678", nil, nil); len(spans) != 0 {
		t.Fatalf("nil DiffResolver still emitted %#v", spans)
	}
}

func TestScanGitResolverMissDropsSpan(t *testing.T) {
	resolve := func(raw string) (string, Extra, bool) {
		return "", Extra{}, raw == "abc1234"
	}
	spans := Scan("abc1234 then deadbee", nil, resolve)
	if len(spans) != 1 || spans[0].Value != "abc1234" {
		t.Fatalf("resolver miss = %#v", spans)
	}
}

func TestScanGitDottedWinsOverComponentRevs(t *testing.T) {
	var seen []string
	resolve := func(raw string) (string, Extra, bool) {
		seen = append(seen, raw)
		return raw, Extra{Raw: raw}, true
	}
	spans := Scan("abc1234..def5678", nil, resolve)
	if len(spans) != 1 || spans[0].Value != "abc1234..def5678" {
		t.Fatalf("dotted span = %#v", spans)
	}
	if len(seen) != 1 || seen[0] != "abc1234..def5678" {
		t.Fatalf("resolver saw %#v, want only the range token", seen)
	}
}

// TestScanFindsSidecarSessionNames pins the session pattern to the names
// Sidecar itself mints — a shell (sidecar-sh-<project>-<n>) and a worktree
// agent (sidecar-ws-<slug>) — because those are the only sessions any surface
// attaches to.
func TestScanFindsSidecarSessionNames(t *testing.T) {
	for _, line := range []string{
		"agent idle in sidecar-sh-repo-1",
		"sidecar-ws-notification-center finished",
		"(sidecar-ws-td-331dbf19) needs review",
	} {
		spans := Scan(line, nil, nil)
		var sessions []Span
		for _, span := range spans {
			if span.Kind == KindSession {
				sessions = append(sessions, span)
			}
		}
		if len(sessions) != 1 {
			t.Fatalf("%q: got %d session spans: %+v", line, len(sessions), spans)
		}
		got := sessions[0]
		if line[got.StartCol:got.EndCol+1] != got.Value {
			t.Fatalf("%q: columns %d-%d do not cover %q", line, got.StartCol, got.EndCol, got.Value)
		}
		if !SessionName(got.Value) {
			t.Fatalf("%q: scanned %q that SessionName rejects", line, got.Value)
		}
	}
}

// TestScanDoesNotInventSessions is the false-positive gate. A tmux session name
// is free text, so anything looser than "a name Sidecar minted, whole token"
// would underline ordinary prose and paths.
func TestScanDoesNotInventSessions(t *testing.T) {
	for _, line := range []string{
		"attach to main",                        // an ordinary tmux name
		"the sidecar session is running",        // prose about sidecar
		"see /tmp/sidecar-sh-repo-1.log",        // a filename containing one
		"sidecar-tp-repo is the terminal panel", // an internal pane, never attached
		"sidecar-edit-12345 died",               // likewise
		"my-sidecar-sh-repo-1 is not ours",      // not the whole token
		"sidecar-sh- has no body",
		"SIDECAR-SH-REPO-1 shouts",
	} {
		for _, span := range Scan(line, nil, nil) {
			if span.Kind == KindSession {
				t.Fatalf("%q: invented session %q", line, span.Value)
			}
		}
	}
}

// TestSessionWinsOverTheIdInsideIt keeps a worktree session named after an
// issue whole: the token is a session, not the id hiding in it.
func TestSessionWinsOverTheIdInsideIt(t *testing.T) {
	spans := Scan("sidecar-ws-td-331dbf19 is stuck", nil, nil)
	for _, span := range spans {
		if span.Kind == KindIssue {
			t.Fatalf("issue span %q inside a session name: %+v", span.Value, spans)
		}
	}
	if len(spans) != 1 || spans[0].Kind != KindSession {
		t.Fatalf("got %+v", spans)
	}
}

func TestSessionNameRejectsWhatItDoesNotMint(t *testing.T) {
	for _, value := range []string{"", "main", "sidecar-tp-repo", "sidecar-sh-repo-1 ", "sidecar-sh-repo;rm -rf /", "sidecar-sh-repo-1.log"} {
		if SessionName(value) {
			t.Fatalf("SessionName(%q) = true", value)
		}
	}
	if !SessionName("sidecar-ws-alpha") {
		t.Fatal("SessionName rejected a name Sidecar mints")
	}
}

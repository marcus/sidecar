package notify

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/terminallink"
)

func TestParseTargetSpecAcceptsTheDocumentedGrammar(t *testing.T) {
	cases := []struct {
		spec string
		want Target
	}{
		{"issue:td-4c1f9a", Target{Kind: TargetIssue, Value: "td-4c1f9a"}},
		{"file:internal/app/model.go", Target{Kind: TargetFile, Value: "internal/app/model.go"}},
		{"file:internal/app/model.go:42", Target{Kind: TargetFile, Value: "internal/app/model.go", Line: 42}},
		{"url:https://example.com/a:b", Target{Kind: TargetURL, Value: "https://example.com/a:b"}},
		{"commit:abc1234", Target{Kind: TargetCommit, Value: "abc1234"}},
		{"session:sidecar-sh-repo-1", Target{Kind: TargetSession, Value: "sidecar-sh-repo-1"}},
		{"task:t-17", Target{Kind: TargetTask, Value: "t-17"}},
		{"FILE:x.go:7", Target{Kind: TargetFile, Value: "x.go", Line: 7}},
	}
	for _, tc := range cases {
		got, err := ParseTargetSpec(tc.spec)
		if err != nil {
			t.Fatalf("ParseTargetSpec(%q): %v", tc.spec, err)
		}
		if got != tc.want {
			t.Fatalf("ParseTargetSpec(%q) = %+v, want %+v", tc.spec, got, tc.want)
		}
	}
}

// A commit sha or a session name may end in digits; only a file has a line.
func TestParseTargetSpecReadsALineOnlyForFiles(t *testing.T) {
	got, err := ParseTargetSpec("commit:HEAD~1:42")
	if err != nil {
		t.Fatalf("ParseTargetSpec: %v", err)
	}
	if got.Line != 0 || got.Value != "HEAD~1:42" {
		t.Fatalf("a commit target must keep its trailing digits: %+v", got)
	}
	session, err := ParseTargetSpec("session:sidecar-sh-repo-42")
	if err != nil {
		t.Fatalf("ParseTargetSpec: %v", err)
	}
	if session.Line != 0 || session.Value != "sidecar-sh-repo-42" {
		t.Fatalf("a session target must keep its trailing digits: %+v", session)
	}
}

// Only Sidecar-owned session names attach, so only they are accepted: a name
// nothing can attach to would be a numbered digit that does nothing.
func TestParseTargetSpecRefusesAForeignSession(t *testing.T) {
	for _, spec := range []string{"session:main", "session:my-work", "session:sidecar-tp-repo"} {
		if got, err := ParseTargetSpec(spec); err == nil {
			t.Fatalf("ParseTargetSpec(%q) = %+v, want a refusal", spec, got)
		}
	}
}

func TestParseTargetSpecReadsAProjectQualifier(t *testing.T) {
	got, err := ParseTargetSpec("issue:td-99aabb@braid")
	if err != nil {
		t.Fatalf("ParseTargetSpec: %v", err)
	}
	if got.Project != "braid" || got.Value != "td-99aabb" {
		t.Fatalf("cross-project spec parsed as %+v", got)
	}

	got, err = ParseTargetSpec("file:cmd/main.go:12@/Users/x/code/braid")
	if err != nil {
		t.Fatalf("ParseTargetSpec path qualifier: %v", err)
	}
	if got.Project != "/Users/x/code/braid" || got.Value != "cmd/main.go" || got.Line != 12 {
		t.Fatalf("path-qualified spec parsed as %+v", got)
	}

	// An @ inside a URL's authority is part of the URL, not a project.
	got, err = ParseTargetSpec("url:https://user@example.com/path")
	if err != nil {
		t.Fatalf("ParseTargetSpec url with userinfo: %v", err)
	}
	if got.Project != "" || got.Value != "https://user@example.com/path" {
		t.Fatalf("a URL's userinfo was mistaken for a project: %+v", got)
	}
}

func TestParseTargetSpecRefusesMalformedSpecs(t *testing.T) {
	for _, spec := range []string{
		"",
		"td-4c1f9a",               // no kind
		"branch:main",             // unknown kind
		"file:",                   // no value
		"url:javascript:alert(1)", // not a safe http(s) URL
		"url:ftp://example.com",
		"issue:not-an-id",
		"@braid",
	} {
		if got, err := ParseTargetSpec(spec); err == nil {
			t.Fatalf("ParseTargetSpec(%q) = %+v, want a refusal", spec, got)
		}
	}
}

func TestParseTargetSpecsKeepsOrderAndDropsDuplicates(t *testing.T) {
	got, err := ParseTargetSpecs([]string{"issue:td-4c1f9a", "file:a.go:3", "issue:td-4c1f9a"})
	if err != nil {
		t.Fatalf("ParseTargetSpecs: %v", err)
	}
	if len(got) != 2 || got[0].Value != "td-4c1f9a" || got[1].Value != "a.go" {
		t.Fatalf("ParseTargetSpecs = %+v", got)
	}
	if _, err := ParseTargetSpecs([]string{"issue:td-4c1f9a", "nonsense"}); err == nil {
		t.Fatalf("one bad spec must fail the whole list")
	}
}

// A target in another checkout is numbered and activatable but never located
// in the text, and it reads with its project's name in front of it (design 1e).
func TestCrossProjectTargetRendersWithItsProjectPrefix(t *testing.T) {
	n := Notification{
		Source:  SourceAgent,
		Title:   "Review td-99aabb now",
		Targets: []Target{{Kind: TargetIssue, Value: "td-99aabb", Project: "/Users/x/code/braid"}},
	}
	list := CallsToAction(n, terminallink.Options{})
	if len(list) == 0 {
		t.Fatalf("expected the stored target to be numbered")
	}
	if got := list[0].Display(); got != "braid/td-99aabb" {
		t.Fatalf("Display() = %q, want braid/td-99aabb", got)
	}
	if list[0].Field != CTAFieldNone {
		t.Fatalf("a foreign target must not adopt a local span as its location: %+v", list[0])
	}
	// The same id written in this project's text is a separate, local target.
	if len(list) != 2 || list[1].Project != "" || list[1].Field != CTAFieldTitle {
		t.Fatalf("expected the scanned local id to remain its own call to action: %+v", list)
	}
}

// A URL never gives up its tail to a project qualifier: `@` is legal in a
// path, in userinfo and in a query, and truncating one silently would post a
// call to action that opens the wrong page.
func TestParseTargetSpecKeepsAtInURLs(t *testing.T) {
	for _, raw := range []string{
		"url:https://example.com/pkg@v2",
		"url:https://user@example.com/path",
		"url:https://example.com/search?q=a@b",
	} {
		target, err := ParseTargetSpec(raw)
		if err != nil {
			t.Fatalf("ParseTargetSpec(%q) = %v", raw, err)
		}
		want := strings.TrimPrefix(raw, "url:")
		if target.Value != want || target.Project != "" {
			t.Fatalf("ParseTargetSpec(%q) = %+v, want value %q and no project", raw, target, want)
		}
	}
}

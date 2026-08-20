package targetactivation

import (
	"errors"
	"testing"

	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
)

func TestResolveFileTargets(t *testing.T) {
	t.Parallel()
	plan, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindFile, Value: "internal/app/model.go", Line: 42})
	if err != nil {
		t.Fatalf("resolve file: %v", err)
	}
	if plan.Kind != PlanOpenFile || plan.PluginID != FileBrowserPluginID {
		t.Fatalf("unexpected plan %+v", plan)
	}
	if plan.Path != "internal/app/model.go" || plan.Line != 42 {
		t.Fatalf("unexpected path/line %+v", plan)
	}

	// The token survives resolution as the text wrote it: a terminal surface
	// resolves it against the root it scanned, where an absolute path is
	// ordinary. Only the project-relative execution cleans and constrains it.
	absolute, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindFile, Value: "/tmp/scratch.md"})
	if err != nil {
		t.Fatalf("resolve absolute file: %v", err)
	}
	if absolute.Path != "/tmp/scratch.md" {
		t.Fatalf("absolute path rewritten: %q", absolute.Path)
	}
}

func TestRelativeProjectPath(t *testing.T) {
	t.Parallel()
	clean, err := RelativeProjectPath("./docs/../docs/plan.md")
	if err != nil {
		t.Fatalf("clean relative path: %v", err)
	}
	if clean != "docs/plan.md" {
		t.Fatalf("path not normalized: %q", clean)
	}
	for name, value := range map[string]string{
		"absolute": "/etc/passwd",
		"home":     "~/.ssh/id_rsa",
		"escaping": "../../secrets.txt",
	} {
		if _, err := RelativeProjectPath(value); err == nil {
			t.Fatalf("%s: expected refusal for %q", name, value)
		}
	}
}

func TestResolveFileRefusals(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"empty":   "  ",
		"control": "notes\x07.md",
	} {
		if _, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindFile, Value: value}); err == nil {
			t.Fatalf("%s: expected refusal for %q", name, value)
		}
	}
	if _, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindFile, Value: "a.go", Line: -3}); err == nil {
		t.Fatal("expected refusal for negative line")
	}
}

func TestResolveURLSafety(t *testing.T) {
	t.Parallel()
	plan, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindURL, Value: "https://example.com/x."})
	if err != nil {
		t.Fatalf("resolve url: %v", err)
	}
	if plan.Kind != PlanOpenURL || plan.URL != "https://example.com/x" {
		t.Fatalf("unexpected plan %+v", plan)
	}
	for _, unsafe := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com", "https://", "not a url"} {
		if _, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindURL, Value: unsafe}); err == nil {
			t.Fatalf("expected refusal for %q", unsafe)
		}
	}
}

func TestResolvePaneKinds(t *testing.T) {
	t.Parallel()
	issue, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-123456"})
	if err != nil || issue.Kind != PlanOpenIssue || issue.Issue != "td-123456" || issue.PluginID != WorkspacePluginID {
		t.Fatalf("issue plan %+v err %v", issue, err)
	}
	diff, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "HEAD~1"})
	if err != nil || diff.Kind != PlanOpenDiff || diff.Spec != "HEAD~1" {
		t.Fatalf("diff plan %+v err %v", diff, err)
	}
	res, err := Resolve(uirequest.Target{
		Kind: uirequest.TargetKindResource, Value: "CASH-1245",
		Provider: "jira", Matcher: "issue-key",
	})
	if err != nil || res.Kind != PlanOpenResource || res.Provider != "jira" || res.Matcher != "issue-key" || res.Locator != "CASH-1245" {
		t.Fatalf("resource plan %+v err %v", res, err)
	}
	if _, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindResource, Value: "CASH-1245"}); err == nil {
		t.Fatal("expected refusal for a resource target with no provider")
	}
	session, err := Resolve(uirequest.Target{Kind: uirequest.TargetKindSession, Value: "sidecar-main"})
	if err != nil || session.Kind != PlanAttachSession || session.Session != "sidecar-main" {
		t.Fatalf("session plan %+v err %v", session, err)
	}
	for _, kind := range []uirequest.TargetKind{uirequest.TargetKindWorktree, uirequest.TargetKindShell, uirequest.TargetKindNotification} {
		if _, err := Resolve(uirequest.Target{Kind: kind, Value: "x"}); !errors.Is(err, ErrUnsupportedKind) {
			t.Fatalf("%s: want ErrUnsupportedKind, got %v", kind, err)
		}
	}
	if _, err := Resolve(uirequest.Target{}); err == nil {
		t.Fatal("expected refusal for kindless target")
	}
}

// TestSpanKindsCoverPlanKinds is the shared half of the surface parity pair:
// every activatable scanned span produces a plan, and every plan it can produce
// is listed in PlanKindsFromSpans. The two surfaces then each assert they
// dispatch that list, so a new kind cannot reach one surface and miss the other.
func TestSpanKindsCoverPlanKinds(t *testing.T) {
	t.Parallel()
	spans := map[terminallink.Kind]terminallink.Span{
		terminallink.KindURL:     {Kind: terminallink.KindURL, Value: "https://example.com"},
		terminallink.KindFile:    {Kind: terminallink.KindFile, Value: "internal/app/model.go", Extra: terminallink.Extra{Line: 12}},
		terminallink.KindIssue:   {Kind: terminallink.KindIssue, Value: "td-331dbf19"},
		terminallink.KindDiff:    {Kind: terminallink.KindDiff, Value: "abc1234", Extra: terminallink.Extra{Raw: "HEAD~1"}},
		terminallink.KindSession: {Kind: terminallink.KindSession, Value: "sidecar-sh-repo-1"},
		terminallink.KindResource: {
			Kind: terminallink.KindResource, Value: "CASH-1245",
			Extra: terminallink.Extra{Provider: "jira", Matcher: "issue-key"},
		},
	}
	listed := make(map[PlanKind]bool, len(PlanKindsFromSpans()))
	for _, kind := range PlanKindsFromSpans() {
		listed[kind] = true
	}
	produced := make(map[PlanKind]bool, len(spans))
	for kind, span := range spans {
		if !terminallink.Activatable(kind) {
			t.Fatalf("%s is not activatable; fixture is stale", kind)
		}
		plan, err := PlanForSpan(span)
		if err != nil {
			t.Fatalf("%s: PlanForSpan: %v", kind, err)
		}
		if !listed[plan.Kind] {
			t.Fatalf("%s produces %s, which PlanKindsFromSpans does not list", kind, plan.Kind)
		}
		produced[plan.Kind] = true
	}
	for kind := range listed {
		if !produced[kind] {
			t.Fatalf("PlanKindsFromSpans lists %s, which no span produces", kind)
		}
	}
}

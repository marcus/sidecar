package notify

import (
	"testing"

	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
)

func TestCallsToActionScansTitleThenBody(t *testing.T) {
	t.Parallel()
	n := Notification{
		Source: SourceSystem,
		Title:  "Review td-331dbf19 now",
		Body:   "See internal/app/model.go:42 and https://example.com/x",
	}
	list := CallsToAction(n, terminallink.Options{})
	if len(list) != 3 {
		t.Fatalf("got %d calls to action: %+v", len(list), list)
	}
	if list[0].Target.Kind != uirequest.TargetKindIssue || list[0].Target.Value != "td-331dbf19" {
		t.Fatalf("first = %+v", list[0])
	}
	if list[0].Field != CTAFieldTitle || list[0].Number != 1 {
		t.Fatalf("first location = %+v", list[0])
	}
	if got := CTATitle(n)[list[0].StartCol : list[0].EndCol+1]; got != "td-331dbf19" {
		t.Fatalf("title columns cover %q", got)
	}
	if list[1].Target.Kind != uirequest.TargetKindFile || list[1].Target.Line != 42 {
		t.Fatalf("second = %+v", list[1])
	}
	if list[1].Field != CTAFieldBody || list[1].Number != 2 || list[1].Label != "internal/app/model.go:42" {
		t.Fatalf("second location = %+v", list[1])
	}
	if got := CTABody(n)[list[1].StartCol : list[1].EndCol+1]; got != "internal/app/model.go" {
		t.Fatalf("body columns cover %q", got)
	}
	if list[2].Target.Kind != uirequest.TargetKindURL || list[2].Number != 3 {
		t.Fatalf("third = %+v", list[2])
	}
}

func TestCallsToActionPutsStoredTargetsFirstAndLocatesThem(t *testing.T) {
	t.Parallel()
	n := Notification{
		Source:  SourceSystem,
		Title:   "Blocked on td-aaaa1111",
		Body:    "Fix docs/plan.md:3 first",
		Targets: []Target{{Kind: TargetFile, Value: "docs/plan.md", Line: 3}},
	}
	list := CallsToAction(n, terminallink.Options{})
	if len(list) != 2 {
		t.Fatalf("got %d: %+v", len(list), list)
	}
	// The stored file target is number 1 even though the issue is written first.
	if list[0].Target.Kind != uirequest.TargetKindFile || list[0].Number != 1 {
		t.Fatalf("first = %+v", list[0])
	}
	// And it adopted the span's location, so it can be underlined in place
	// rather than counted twice.
	if list[0].Field != CTAFieldBody || list[0].StartCol < 0 {
		t.Fatalf("stored target was not located: %+v", list[0])
	}
	if got := CTABody(n)[list[0].StartCol : list[0].EndCol+1]; got != "docs/plan.md" {
		t.Fatalf("columns cover %q", got)
	}
	if list[1].Target.Kind != uirequest.TargetKindIssue || list[1].Number != 2 {
		t.Fatalf("second = %+v", list[1])
	}
}

func TestCallsToActionDropsDuplicatesAndUnactivatableKinds(t *testing.T) {
	t.Parallel()
	n := Notification{
		Source: SourceSystem,
		Title:  "td-aaaa1111 and td-aaaa1111 again",
		Targets: []Target{
			{Kind: TargetIssue, Value: "td-aaaa1111"},
			{Kind: TargetTask, Value: "task-7"}, // no activation until Phase 5c
		},
	}
	list := CallsToAction(n, terminallink.Options{})
	if len(list) != 1 {
		t.Fatalf("got %d: %+v", len(list), list)
	}
	if list[0].Target.Value != "td-aaaa1111" || list[0].Field != CTAFieldTitle {
		t.Fatalf("only = %+v", list[0])
	}
}

func TestCallsToActionStripsOSC8BeforeScanning(t *testing.T) {
	t.Parallel()
	n := Notification{
		Source: SourceSystem,
		Title:  "\x1b]8;;https://evil.example\x1b\\td-aaaa1111\x1b]8;;\x1b\\",
	}
	title := CTATitle(n)
	if title != "td-aaaa1111" {
		t.Fatalf("title = %q", title)
	}
	list := CallsToAction(n, terminallink.Options{})
	if len(list) != 1 || list[0].Target.Kind != uirequest.TargetKindIssue {
		t.Fatalf("got %+v", list)
	}
	if list[0].StartCol != 0 || list[0].EndCol != len(title)-1 {
		t.Fatalf("columns %d..%d are not the stripped title's", list[0].StartCol, list[0].EndCol)
	}
}

func TestCallToActionDisplayNamesAForeignProject(t *testing.T) {
	t.Parallel()
	n := Notification{
		Source:  SourceSystem,
		Title:   "elsewhere",
		Targets: []Target{{Kind: TargetIssue, Value: "td-aaaa1111", Project: "/Users/x/code/braid"}},
	}
	list := CallsToAction(n, terminallink.Options{})
	if len(list) != 1 {
		t.Fatalf("got %+v", list)
	}
	if got := list[0].Display(); got != "braid/td-aaaa1111" {
		t.Fatalf("display = %q", got)
	}
	if list[0].Field != CTAFieldNone || list[0].StartCol != -1 {
		t.Fatalf("a foreign target has nothing to underline here: %+v", list[0])
	}
}

func TestCallToActionAt(t *testing.T) {
	t.Parallel()
	list := CallsToAction(Notification{Source: SourceSystem, Title: "td-aaaa1111"}, terminallink.Options{})
	if _, ok := CallToActionAt(list, 0); ok {
		t.Fatal("0 is not a target number")
	}
	if _, ok := CallToActionAt(list, 2); ok {
		t.Fatal("2 is past the end")
	}
	got, ok := CallToActionAt(list, 1)
	if !ok || got.Target.Value != "td-aaaa1111" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

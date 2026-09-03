package uirequest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/terminallink"
)

func TestTargetFromSpan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		span terminallink.Span
		want Target
		ok   bool
	}{
		{
			name: "file prefers raw and carries line",
			span: terminallink.Span{Kind: terminallink.KindFile, Value: "docs/plan.md", Extra: terminallink.Extra{Raw: "./docs/plan.md", Line: 12}},
			want: Target{Kind: TargetKindFile, Value: "./docs/plan.md", Line: 12},
			ok:   true,
		},
		{
			name: "file without raw falls back to value",
			span: terminallink.Span{Kind: terminallink.KindFile, Value: "main.go"},
			want: Target{Kind: TargetKindFile, Value: "main.go"},
			ok:   true,
		},
		{
			name: "url",
			span: terminallink.Span{Kind: terminallink.KindURL, Value: "https://example.com"},
			want: Target{Kind: TargetKindURL, Value: "https://example.com"},
			ok:   true,
		},
		{
			name: "issue",
			span: terminallink.Span{Kind: terminallink.KindIssue, Value: "td-331dbf19"},
			want: Target{Kind: TargetKindIssue, Value: "td-331dbf19"},
			ok:   true,
		},
		{
			name: "diff prefers raw",
			span: terminallink.Span{Kind: terminallink.KindDiff, Value: "abc1234", Extra: terminallink.Extra{Raw: "HEAD~1"}},
			want: Target{Kind: TargetKindDiff, Value: "HEAD~1"},
			ok:   true,
		},
		{
			name: "note from internal span",
			span: terminallink.Span{Kind: terminallink.KindInternal, Value: "nt-4jdj4e", Extra: terminallink.Extra{Namespace: "note"}},
			want: Target{Kind: TargetKindNote, Value: "nt-4jdj4e"},
			ok:   true,
		},
		{
			name: "resource carries provider and matcher",
			span: terminallink.Span{
				Kind:  terminallink.KindResource,
				Value: "CASH-1245",
				Extra: terminallink.Extra{Provider: "jira", Matcher: "issue-key"},
			},
			want: Target{Kind: TargetKindResource, Value: "CASH-1245", Provider: "jira", Matcher: "issue-key"},
			ok:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := TargetFromSpan(tc.span)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && !got.Equal(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A collection target carries its applied filters, bounded on the way in
// because they are persisted with the tab and sent to a child process.
func TestResolveCollectionTargetCarriesFilters(t *testing.T) {
	got, err := ResolveCollectionTarget("recall", "results", "dex", "", map[string]string{
		"profile": "docs", "since": "2026-08-01",
	})
	if err != nil {
		t.Fatalf("ResolveCollectionTarget: %v", err)
	}
	if len(got.Filters) != 2 || got.Filters["profile"] != "docs" {
		t.Fatalf("filters = %v", got.Filters)
	}

	refusals := []struct {
		name    string
		row     string
		filters map[string]string
	}{
		{"a row and a filter", "rc:notes:1", map[string]string{"profile": "docs"}},
		{"an empty id", "", map[string]string{" ": "docs"}},
		{"a control character", "", map[string]string{"profile": "do\x00cs"}},
		{"an over-long id", "", map[string]string{strings.Repeat("p", 33): "docs"}},
		{"an over-long value", "", map[string]string{"profile": strings.Repeat("v", 65)}},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveCollectionTarget("recall", "results", "", tc.row, tc.filters); err == nil {
				t.Fatal("accepted a filter set it cannot persist or send")
			}
		})
	}

	many := map[string]string{}
	for i := 0; i < 9; i++ {
		many[fmt.Sprintf("f%d", i)] = "x"
	}
	if _, err := ResolveCollectionTarget("recall", "results", "", "", many); err == nil {
		t.Fatal("accepted more filters than a collection may declare")
	}
}

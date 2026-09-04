package cli

import (
	"errors"
	"testing"

	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/agentsession"
)

// TestAMistypedKindIsUnsupportedRatherThanAMismatch keeps the refusal honest
// about which thing was wrong.
//
// The kind gate compares --kind to the provider occupying the pane. Before the
// catalog lookup, a value that named no provider at all reached that comparison
// and came back as kind_mismatch, blaming the pane for a claim that could never
// have matched anything. The lookup also folds in the alias vocabulary: the
// conversation adapters call Claude "claude-code" and Cursor "cursor-cli", and
// those name the same providers, not different ones.
func TestAMistypedKindIsUnsupportedRatherThanAMismatch(t *testing.T) {
	t.Run("canonical ids pass through unchanged", func(t *testing.T) {
		for _, id := range []string{"claude", "codex", "opencode", "pi", "grok", "cursor", "antigravity", "amp", "copilot"} {
			got, err := resolveReportedKind(id)
			if err != nil {
				t.Fatalf("resolveReportedKind(%q) = %v", id, err)
			}
			if got != id {
				t.Fatalf("resolveReportedKind(%q) = %q, want it unchanged", id, got)
			}
		}
	})

	t.Run("aliases resolve to their family", func(t *testing.T) {
		cases := map[string]string{
			"claude-code": "claude",
			"cursor-cli":  "cursor",
			"pi-agent":    "pi",
			"agy":         "antigravity",
			// Whitespace a hook picked up from its own configuration must not
			// decide the provider either.
			"  codex ": "codex",
		}
		for claim, want := range cases {
			got, err := resolveReportedKind(claim)
			if err != nil {
				t.Fatalf("resolveReportedKind(%q) = %v", claim, err)
			}
			if got != want {
				t.Fatalf("resolveReportedKind(%q) = %q, want %q", claim, got, want)
			}
		}
	})

	t.Run("an unknown kind names its own problem", func(t *testing.T) {
		for _, claim := range []string{"", "   ", "claude-cli", "clyde", "Claude Code"} {
			got, err := resolveReportedKind(claim)
			if err == nil {
				t.Fatalf("resolveReportedKind(%q) = %q, wanted a refusal", claim, got)
			}
			if !errors.Is(err, agentsession.ErrUnsupportedKind) {
				t.Fatalf("resolveReportedKind(%q) error = %v, want ErrUnsupportedKind", claim, err)
			}
			// The sentinel is what the JSON code is derived from, so pin the
			// code the caller actually sees, not only the sentinel.
			if code := reportSessionCode(err); code != "unsupported_kind" {
				t.Fatalf("resolveReportedKind(%q) reported code %q, want unsupported_kind", claim, code)
			}
		}
	})
}

// TestADetectionOnlyFamilyCanStillBindItsSession is the live proof's finding,
// pinned.
//
// resolveReportedKind resolved a --kind claim through agentcatalog.Lookup, which
// searches the launchable families and their aliases only. That was right while
// every shipped integration belonged to a launchable family, and the Kilo port
// broke the assumption: kilo is a family Sidecar recognises in a pane and cannot
// yet start, its Sidecar-installed plugin reports from a pane the user launched
// themselves, and every binding it sent was refused with exit 5 while its state
// reports, which take no --kind, were accepted.
//
// No offline test caught it because every test in the tree passed a launchable
// id, which is exactly why this one drives the whole detection catalog rather
// than the single provider that exposed the bug. A family that becomes
// launchable later still passes, through the Lookup branch.
//
// The named ids come first, and they are what keeps this a tripwire in a world
// where DetectionFamilies is empty. Which side of the catalog a family sits on
// is not this test's subject: a shipped integration's own --kind claim must
// resolve, whether it resolves through Lookup or through FindDetection. Deriving
// the whole list from DetectionFamilies alone would make the catalog gaining
// launch configuration for every family turn this into either a vacuous loop or
// a failure about nothing, and neither is a statement about binding a session.
func TestADetectionOnlyFamilyCanStillBindItsSession(t *testing.T) {
	// kilo is named because its plugin is the shipped integration that found the
	// bug, and because it must keep resolving on the day kilo becomes launchable.
	ids := []string{"kilo"}
	for _, family := range agentcatalog.DetectionFamilies() {
		if family.ID != "kilo" {
			ids = append(ids, family.ID)
		}
	}
	for _, id := range ids {
		got, err := resolveReportedKind(id)
		if err != nil {
			t.Fatalf("resolveReportedKind(%q) = %v; a family Sidecar recognises in a pane cannot bind "+
				"the conversation in it", id, err)
		}
		if got != id {
			t.Fatalf("resolveReportedKind(%q) = %q, want it unchanged", id, got)
		}
	}
}

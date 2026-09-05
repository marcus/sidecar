package styles

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentcatalog"
)

// uncuratedFamilies are the catalog families no theme registers a colour for.
//
// Phase 4 of docs/plans/active/herdr-detection-parity.md registered ten of them
// as detection-only, and its whole scope argument is that they cost no
// presentation work: no curated colour in twenty themes, no website palette
// entry, no icon that has to match a conversation adapter. Slice 5 of
// docs/plans/implemented/herdr-parity-close-the-gap.md gave all ten a launch command
// and added two more, and the scope argument did not change: what makes them
// free is that this package degrades gracefully for a provider it has never
// heard of, not that they cannot be started.
//
// They are named here rather than read from agentcatalog.DetectionFamilies(),
// which is now empty. Reading that would have left every assertion below
// covering nothing while still passing, which is the one failure mode a test
// like this must not have.
var uncuratedFamilies = []string{
	"cline", "devin", "droid", "hermes", "kilo", "kimi",
	"kiro", "maki", "mastracode", "omp", "qodercli", "qwen",
}

// The presentation contract for a family nothing here has heard of, pinned
// rather than assumed.
//
// AgentColor answers TextMuted, in every theme, which is a real chip colour and
// not a crash or an empty style. AgentLabel answers the family's own name, with
// a glyph only where one already exists. Both are the graceful degradation the
// plan claims, and both
// stop being true the moment somebody adds a partial entry -- a colour in one
// theme and not the rest -- which is exactly what this catches.
func TestDetectionOnlyAgentsRenderWithNoThemeWork(t *testing.T) {
	original := GetCurrentThemeName()
	t.Cleanup(func() { ApplyTheme(original) })

	for _, id := range uncuratedFamilies {
		if _, ok := agentcatalog.Lookup(id); !ok {
			t.Fatalf("%q is not a catalog family; this list has drifted", id)
		}
	}

	for _, theme := range ListThemes() {
		ApplyTheme(theme)
		for _, id := range uncuratedFamilies {
			if got := AgentColor(id); got != TextMuted {
				t.Errorf("theme %s: AgentColor(%q) = %v, want TextMuted.\n"+
					"This family has no curated colour by design. A colour here means one theme "+
					"gained an entry the other %d did not, which is the drift the curated-palette tests exist to stop.",
					theme, id, got, len(ListThemes())-1)
			}
			if AgentLabel(id) == "" {
				t.Errorf("theme %s: AgentLabel(%q) is empty; a detected pane would render no agent chip at all", theme, id)
			}
		}
	}
}

// The one uncurated family that already has an icon, recorded so it is not
// mistaken for the rule. Kiro has a conversation-history adapter
// (internal/adapter/kiro) that predates all of this and shipped a glyph with it,
// so AgentIcon answers for it while the other eleven render with no glyph.
// Nothing was added for it here and nothing needs to be: both answers render.
func TestOnlyKiroAmongDetectionOnlyFamiliesAlreadyHasAnIcon(t *testing.T) {
	for _, id := range uncuratedFamilies {
		icon := AgentIcon(id)
		if id == "kiro" {
			if icon != "κ" {
				t.Errorf("AgentIcon(kiro) = %q, want κ from the Kiro conversations adapter", icon)
			}
			if got := AgentLabel("kiro"); got != "κ kiro" {
				t.Errorf("AgentLabel(kiro) = %q, want %q", got, "κ kiro")
			}
			continue
		}
		if icon != "" {
			t.Errorf("AgentIcon(%q) = %q; an uncurated family with an icon and no conversations adapter "+
				"breaks the rule TestAgentIconMatchesConversationsAdapters keeps", id, icon)
		}
	}
}

// A detected pane renders the family's own name, not the id its manifest file
// happens to be called.
//
// This is the half the first pass missed. Ten families were registered with a
// Name and a Short and nothing read either: agentcatalog.Label knew only the
// launchable list and fell through to the raw id, and AgentLabel rendered the
// raw id too. Qoder is the family that shows it, because it is the only one
// whose id is not a name -- `qodercli` is Herdr's manifest label, kept as the id
// on purpose so the vendored file, the alias table and `agent explain --agent`
// all agree with no mapping. A chip is a width-constrained lowercase token, so
// it takes Short; a settings row or any other prose surface takes Label.
func TestDetectionOnlyPanesRenderTheirDisplayNameNotTheirManifestID(t *testing.T) {
	if got, want := AgentLabel("qodercli"), "qoder"; got != want {
		t.Errorf("AgentLabel(qodercli) = %q, want %q; the chip is naming a manifest file rather than a program", got, want)
	}
	if got, want := agentcatalog.Label("qodercli"), "Qoder CLI"; got != want {
		t.Errorf("agentcatalog.Label(qodercli) = %q, want %q", got, want)
	}
	for _, id := range uncuratedFamilies {
		family, ok := agentcatalog.Lookup(id)
		if !ok {
			t.Fatalf("%q is not a catalog family", id)
		}
		if got, want := AgentLabel(family.ID), strings.ToLower(family.Short); !strings.HasSuffix(got, want) {
			t.Errorf("AgentLabel(%q) = %q, want it to end in the family's short name %q", family.ID, got, want)
		}
		if got := agentcatalog.Label(family.ID); got != family.Name {
			t.Errorf("agentcatalog.Label(%q) = %q, want %q; the display name registered for this family is read by nothing",
				family.ID, got, family.Name)
		}
	}
}

// Every launchable family renders its short name, lowercased, as its chip
// token, with its icon in front where one exists.
//
// The rule used to be stated as "the id", which was true only because Short
// lowercased was the id for all ten launchable families at the time. Qoder
// broke that the moment it became launchable, and correctly: its id is
// `qodercli` because that is Herdr's manifest label, while the program a user
// installs and types is `qoder`, so the chip takes the command. The rule is
// still pinned, because a family that changes its Short renames a chip on every
// workspace row and every Sessions card.
func TestLaunchableFamiliesKeepTheirChipToken(t *testing.T) {
	for _, family := range agentcatalog.Families() {
		want := strings.ToLower(family.Short)
		if icon := AgentIcon(family.ID); icon != "" {
			want = icon + " " + want
		}
		if got := AgentLabel(family.ID); got != want {
			t.Errorf("AgentLabel(%q) = %q, want %q", family.ID, got, want)
		}
	}
	// The one family where the short name is not the id, spelled out so the
	// exception is a decision rather than whatever the data happens to say.
	if got, want := AgentLabel("qodercli"), "qoder"; got != want {
		t.Errorf("AgentLabel(qodercli) = %q, want %q", got, want)
	}
	if got, want := AgentLabel("claude"), "◆ claude"; got != want {
		t.Errorf("AgentLabel(claude) = %q, want %q", got, want)
	}
}

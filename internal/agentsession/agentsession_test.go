package agentsession

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRoots(home string) Roots {
	return Roots{Home: home, Env: func(string) string { return "" }}
}

func TestValidateIDBoundsAndShape(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"a uuid is the ordinary case", "019f2c8a-1d4e-7b02-9c11-6f3a0b7d2e55", nil},
		{"an underscored id", "ses_d41fda", nil},
		{"a colon is allowed for namespaced ids", "thread:abc123", nil},
		{"empty", "", ErrInvalidRef},
		{"over the byte cap", strings.Repeat("a", MaxIDBytes+1), ErrInvalidRef},
		{"exactly at the cap", strings.Repeat("a", MaxIDBytes), nil},
		{"a NUL byte", "abc\x00def", ErrInvalidRef},
		{"a newline", "abc\ndef", ErrInvalidRef},
		{"a carriage return", "abc\rdef", ErrInvalidRef},
		{"an escape byte", "abc\x1bdef", ErrInvalidRef},
		{"DEL", "abc\x7fdef", ErrInvalidRef},
		{"a C1 control rune", "abc\u0085def", ErrInvalidRef},
		{"a forward slash cannot appear in an id", "abc/def", ErrInvalidRef},
		{"a backslash", `abc\def`, ErrInvalidRef},
		{"a space", "abc def", ErrInvalidRef},
		{"a shell metacharacter", "abc;rm -rf /", ErrInvalidRef},
		{"a quote", "abc'def", ErrInvalidRef},
		{"a dollar sign", "abc$def", ErrInvalidRef},
		{"a leading dot", ".abc", ErrInvalidRef},
		{"a leading dash would be read as a flag", "-abc", ErrInvalidRef},
		{"a leading double dash", "--resume", ErrInvalidRef},
		{"a dash inside is fine", "abc-def", nil},
		{"invalid UTF-8", "abc\xffdef", ErrInvalidRef},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateID(tc.value)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("ValidateID(%q) = %v, wanted it accepted", tc.value, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateID(%q) = %v, wanted %v", tc.value, err, tc.want)
			}
		})
	}
}

func TestValidatePathShape(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"an absolute normalized path", "/Users/x/.codex/sessions/2026/08/30/rollout-a.jsonl", nil},
		{"empty", "", ErrInvalidRef},
		{"relative", "sessions/a.jsonl", ErrInvalidRef},
		{"a dot-dot component is not normalized", "/Users/x/../x/a.jsonl", ErrInvalidRef},
		{"a trailing slash is not normalized", "/Users/x/sessions/", ErrInvalidRef},
		{"a doubled separator is not normalized", "/Users//x/a.jsonl", ErrInvalidRef},
		{"a newline", "/Users/x/a\n.jsonl", ErrInvalidRef},
		{"a NUL", "/Users/x/a\x00.jsonl", ErrInvalidRef},
		{"over the byte cap", "/" + strings.Repeat("a", MaxPathBytes), ErrInvalidRef},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePath(tc.value)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("ValidatePath(%q) = %v, wanted it accepted", tc.value, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidatePath(%q) = %v, wanted %v", tc.value, err, tc.want)
			}
		})
	}
}

// TestAPathOutsideTheProvidersStoreIsRefused is the rule that keeps a hook from
// naming an arbitrary file for Sidecar to read later. A path reference is only
// ever a transcript, and a transcript only ever lives where that provider keeps
// them.
func TestAPathOutsideTheProvidersStoreIsRefused(t *testing.T) {
	home := "/home/u"
	roots := testRoots(home)

	inside := filepath.Join(home, ".codex", "sessions", "2026", "08", "30", "rollout-a.jsonl")
	if err := roots.WithinRoots("codex", inside); err != nil {
		t.Fatalf("a path inside the codex store was refused: %v", err)
	}
	archived := filepath.Join(home, ".codex", "archived_sessions", "rollout-a.jsonl")
	if err := roots.WithinRoots("codex", archived); err != nil {
		t.Fatalf("a path inside the codex archive was refused: %v", err)
	}
	claude := filepath.Join(home, ".claude", "projects", "-home-u-code", "a.jsonl")
	if err := roots.WithinRoots("claude", claude); err != nil {
		t.Fatalf("a path inside the claude store was refused: %v", err)
	}

	refusals := []struct {
		name, kind, path string
	}{
		{"an unrelated absolute file", "codex", "/etc/passwd"},
		{"the user's ssh key", "codex", filepath.Join(home, ".ssh", "id_ed25519")},
		{"another provider's store", "codex", claude},
		{"a sibling directory sharing a prefix", "codex", filepath.Join(home, ".codexsessions", "a.jsonl")},
		{"the root directory itself, which is not a transcript", "codex", filepath.Join(home, ".codex", "sessions")},
		{"a provider with no recorded store root", "grok", filepath.Join(home, ".grok", "a.jsonl")},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			err := roots.WithinRoots(tc.kind, tc.path)
			if !errors.Is(err, ErrOutsideStoreRoot) {
				t.Fatalf("WithinRoots(%q, %q) = %v, wanted ErrOutsideStoreRoot", tc.kind, tc.path, err)
			}
		})
	}
}

// TestPisStoreRootFollowsItsAgentDirectory covers the provider whose root moves,
// which none of the cases above do.
//
// Pi's conversations live at getAgentDir()/sessions, and getAgentDir is
// PI_CODING_AGENT_DIR with a leading tilde expanded, so the approved root is
// wherever the user pointed Pi. Every branch of that is here because the branch
// that was not tested is the one that broke: PiAgentDir now trims the
// environment value, and before the two derivations were merged this side did
// not, so a whitespace-only PI_CODING_AGENT_DIR made the root "  /sessions"
// while the installer wrote the extension into ~/.pi/agent/extensions.
func TestPisStoreRootFollowsItsAgentDirectory(t *testing.T) {
	home := "/home/u"
	for _, tc := range []struct {
		name     string
		override string
		want     string
	}{
		{"unset", "", "/home/u/.pi/agent/sessions"},
		{"an absolute override", "/opt/pi/agent", "/opt/pi/agent/sessions"},
		{"a tilde override", "~/elsewhere/agent", "/home/u/elsewhere/agent/sessions"},
		{"the bare tilde", "~", "/home/u/sessions"},
		{"whitespace, which is an unset variable somebody exported", "  ", "/home/u/.pi/agent/sessions"},
		{"an override with surrounding whitespace", " ~/elsewhere/agent ", "/home/u/elsewhere/agent/sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots := Roots{Home: home, Env: func(name string) string {
				if name == "PI_CODING_AGENT_DIR" {
					return tc.override
				}
				return ""
			}}
			got := roots.For("pi")
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("PI_CODING_AGENT_DIR=%q gave roots %v, want [%s]", tc.override, got, tc.want)
			}
			// A transcript under that root is accepted and one beside it is not,
			// because a root nobody can be inside is the same as no root at all.
			if err := roots.WithinRoots("pi", filepath.Join(tc.want, "a.jsonl")); err != nil {
				t.Fatalf("a transcript inside pi's own store was refused: %v", err)
			}
			if err := roots.WithinRoots("pi", "/etc/passwd"); !errors.Is(err, ErrOutsideStoreRoot) {
				t.Fatalf("an unrelated file was accepted for pi: %v", err)
			}
		})
	}

	// A tilde that cannot be expanded is unknowable rather than guessable, and no
	// root at all is what makes every path reference for it a refusal.
	noHome := Roots{Home: "", Env: func(string) string { return "~/agent" }}
	if got := noHome.For("pi"); got != nil {
		t.Fatalf("a tilde override with no home resolved to %v; it cannot be known", got)
	}
}

// TestOnlyAnOfficialSourceMarksAReferenceResumable is the trust rule. An
// unofficial reporter may still record what it saw; what it may not do is make
// that record something Sidecar will act on unattended.
func TestOnlyAnOfficialSourceMarksAReferenceResumable(t *testing.T) {
	now := func() time.Time { return time.Unix(1700000000, 0).UTC() }
	roots := testRoots("/home/u")

	official, err := Validate(Report{
		Kind: "codex", RefKind: RefID, Value: "019f2c8a", Source: "sidecar.codex.hooks", Generation: "pid=1,start=t",
	}, roots, now)
	if err != nil {
		t.Fatalf("the official source was refused: %v", err)
	}
	if !official.Reported {
		t.Fatal("an official integration's reference was not marked reported")
	}

	guessed, err := Validate(Report{
		Kind: "codex", RefKind: RefID, Value: "019f2c8a", Source: "cwd.discovery", Generation: "pid=1,start=t",
	}, roots, now)
	if err != nil {
		t.Fatalf("an unofficial source should still validate, got %v", err)
	}
	if guessed.Reported {
		t.Fatal("an unofficial source marked its reference reported, which would make a guess auto-resumable")
	}
	if _, err := PlanResume("codex", guessed); !errors.Is(err, ErrUntrustedSource) {
		t.Fatalf("PlanResume from an unofficial reference = %v, wanted ErrUntrustedSource", err)
	}
	if _, err := PlanResume("codex", official); err != nil {
		t.Fatalf("PlanResume from an official reference failed: %v", err)
	}
}

// TestTheGenerationFenceDecidesEveryTransition is the M3 exit gate expressed as
// values: a hook can report, rotate, and clear, and a late report from a
// generation that no longer occupies the pane changes nothing.
func TestTheGenerationFenceDecidesEveryTransition(t *testing.T) {
	live := "pid=100,start=A"
	stale := "pid=99,start=Z"

	first := Ref{Kind: RefID, Value: "sess-1", Source: "sidecar.codex.hooks", Reported: true, Generation: live}
	second := Ref{Kind: RefID, Value: "sess-2", Source: "sidecar.codex.hooks", Reported: true, Generation: live}

	t.Run("a first report is recorded", func(t *testing.T) {
		got, err := Fence(nil, first, live)
		if err != nil || got != DecisionRecorded {
			t.Fatalf("Fence(nil, first) = %v, %v; wanted recorded", got, err)
		}
	})

	t.Run("the same report again is idempotent, not a rewrite", func(t *testing.T) {
		got, err := Fence(&first, first, live)
		if err != nil || got != DecisionUnchanged {
			t.Fatalf("replaying a report = %v, %v; wanted unchanged", got, err)
		}
	})

	t.Run("a new conversation from the live generation rotates", func(t *testing.T) {
		got, err := Fence(&first, second, live)
		if err != nil || got != DecisionRotated {
			t.Fatalf("Fence(first, second) = %v, %v; wanted rotated", got, err)
		}
	})

	t.Run("a clear from the live generation clears", func(t *testing.T) {
		got, err := FenceClear(&first, live, live)
		if err != nil || got != DecisionCleared {
			t.Fatalf("FenceClear = %v, %v; wanted cleared", got, err)
		}
	})

	t.Run("clearing nothing is unchanged rather than an error", func(t *testing.T) {
		got, err := FenceClear(nil, live, live)
		if err != nil || got != DecisionUnchanged {
			t.Fatalf("FenceClear(nil) = %v, %v; wanted unchanged", got, err)
		}
	})

	t.Run("a late report from an exited generation is ignored", func(t *testing.T) {
		late := Ref{Kind: RefID, Value: "sess-old", Source: "sidecar.codex.hooks", Reported: true, Generation: stale}
		got, err := Fence(&second, late, live)
		if got != DecisionIgnored || !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("a late report = %v, %v; wanted ignored + ErrStaleGeneration", got, err)
		}
	})

	t.Run("a late clear cannot unbind its successor", func(t *testing.T) {
		got, err := FenceClear(&second, stale, live)
		if got != DecisionIgnored || !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("a late clear = %v, %v; wanted ignored + ErrStaleGeneration", got, err)
		}
	})

	t.Run("a report carrying no generation cannot win", func(t *testing.T) {
		anon := Ref{Kind: RefID, Value: "x", Source: "sidecar.codex.hooks", Reported: true}
		got, err := Fence(nil, anon, live)
		if got != DecisionIgnored || !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("an ungenerated report = %v, %v; wanted ignored", got, err)
		}
	})

	t.Run("an unresolvable live generation refuses rather than defaulting to accept", func(t *testing.T) {
		got, err := Fence(nil, first, "")
		if got != DecisionIgnored || !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("Fence with no live generation = %v, %v; wanted ignored", got, err)
		}
	})

	// The fence must not be satisfiable by comparing the incoming report with
	// the record it is trying to replace. If it were, a late report would match
	// the record its own generation wrote and win.
	t.Run("a late report matching only the stored generation still loses", func(t *testing.T) {
		stored := Ref{Kind: RefID, Value: "sess-old", Source: "sidecar.codex.hooks", Reported: true, Generation: stale}
		late := Ref{Kind: RefID, Value: "sess-newer-from-dead-process", Source: "sidecar.codex.hooks", Reported: true, Generation: stale}
		got, err := Fence(&stored, late, live)
		if got != DecisionIgnored || !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("a late report agreeing with the record = %v, %v; wanted ignored", got, err)
		}
	})

	// The other direction: a genuinely new provider in the same pane takes the
	// binding over from an older generation's record. "Late" and "new" must not
	// be confused, and the difference is which side the live generation is on.
	t.Run("a new generation takes over from an older record", func(t *testing.T) {
		stored := Ref{Kind: RefID, Value: "sess-old", Source: "sidecar.codex.hooks", Reported: true, Generation: stale}
		fresh := Ref{Kind: RefID, Value: "sess-new", Source: "sidecar.codex.hooks", Reported: true, Generation: live}
		got, err := Fence(&stored, fresh, live)
		if err != nil || got != DecisionRotated {
			t.Fatalf("a new provider generation = %v, %v; wanted rotated", got, err)
		}
	})
}

// TestOneConversationResumesIntoAtMostOneShell is the global per-host dedup rule.
func TestOneConversationResumesIntoAtMostOneShell(t *testing.T) {
	early := time.Unix(1700000000, 0).UTC()
	late := early.Add(time.Hour)

	ref := func(value string, at time.Time) Ref {
		return Ref{Kind: RefID, Value: value, Source: "sidecar.codex.hooks", Reported: true, ReportedAt: at}
	}
	holders := []Holder{
		{Project: "p", Session: "sidecar-sh-p-1", Kind: "codex", Ref: ref("shared", early)},
		{Project: "p", Session: "sidecar-sh-p-2", Kind: "codex", Ref: ref("shared", late)},
		{Project: "p", Session: "sidecar-sh-p-3", Kind: "codex", Ref: ref("alone", early)},
		// The same identifier under a different provider is a different
		// conversation, not a conflict.
		{Project: "p", Session: "sidecar-sh-p-4", Kind: "claude", Ref: ref("shared", late)},
	}

	conflicts := Conflicts(holders)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicting conversations, wanted exactly 1: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.Winner.Session != "sidecar-sh-p-2" {
		t.Fatalf("the most recently reported claim should win, got %q", c.Winner.Session)
	}
	if len(c.Conflicts) != 1 || c.Conflicts[0].Session != "sidecar-sh-p-1" {
		t.Fatalf("wanted the older claim reported as the conflict, got %+v", c.Conflicts)
	}

	// Every conversation still gets exactly one winner, including the ones with
	// no conflict at all.
	all := Dedup(holders)
	if len(all) != 3 {
		t.Fatalf("got %d conversations, wanted 3 distinct ones", len(all))
	}
	for _, r := range all {
		if r.Winner.Session == "" {
			t.Fatalf("a conversation had no winner: %+v", r)
		}
	}
}

func TestDedupIsStableWhenTwoClaimsAreEquallyRecent(t *testing.T) {
	at := time.Unix(1700000000, 0).UTC()
	ref := Ref{Kind: RefID, Value: "shared", Source: "sidecar.codex.hooks", Reported: true, ReportedAt: at}
	forward := []Holder{
		{Session: "b", Kind: "codex", Ref: ref},
		{Session: "a", Kind: "codex", Ref: ref},
	}
	reverse := []Holder{
		{Session: "a", Kind: "codex", Ref: ref},
		{Session: "b", Kind: "codex", Ref: ref},
	}
	if Dedup(forward)[0].Winner.Session != Dedup(reverse)[0].Winner.Session {
		t.Fatal("the dedup winner depended on manifest order")
	}
	if got := Dedup(forward)[0].Winner.Session; got != "a" {
		t.Fatalf("wanted the tie broken on session name, got %q", got)
	}
}

// TestResumeIsAlwaysStructuredArgv is the rule that a session identifier never
// becomes part of a command string anywhere in the resume path.
func TestResumeIsAlwaysStructuredArgv(t *testing.T) {
	hostile := Ref{
		Kind:     RefID,
		Value:    "abc-def",
		Source:   "sidecar.codex.hooks",
		Reported: true,
	}
	plan, err := PlanResume("codex", hostile)
	if err != nil {
		t.Fatalf("PlanResume: %v", err)
	}
	want := []string{"codex", "resume", "abc-def"}
	if len(plan.Argv) != len(want) {
		t.Fatalf("argv = %q, wanted %q", plan.Argv, want)
	}
	for i := range want {
		if plan.Argv[i] != want[i] {
			t.Fatalf("argv = %q, wanted %q", plan.Argv, want)
		}
	}
	if plan.Argv[len(plan.Argv)-1] != hostile.Value {
		t.Fatal("the session value must be exactly one trailing argv entry")
	}
}

func TestResumeRefusesWhatItCannotBuild(t *testing.T) {
	reported := func(kind RefKind, value string) Ref {
		return Ref{Kind: kind, Value: value, Source: "sidecar.codex.hooks", Reported: true}
	}
	if _, err := PlanResume("copilot", reported(RefID, "x")); !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("a provider with no native resume = %v, wanted ErrUnsupportedKind", err)
	}
	if _, err := PlanResume("codex", reported(RefPath, "/tmp/a.jsonl")); !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("resuming codex from a path = %v, wanted ErrUnsupportedKind", err)
	}
	if _, err := PlanResume("nosuchprovider", reported(RefID, "x")); err == nil {
		t.Fatal("an unknown provider was accepted")
	}
	if _, err := PlanResume("codex", Ref{}); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("an empty reference = %v, wanted ErrInvalidRef", err)
	}
}

func TestPolicyVocabulary(t *testing.T) {
	// An older record with no policy field means inherit, not an error: that is
	// what a v2 manifest upgraded in place looks like.
	if p, err := ParsePolicy(""); err != nil || p != PolicyInherit {
		t.Fatalf("ParsePolicy(\"\") = %v, %v; wanted inherit", p, err)
	}
	for _, name := range []string{"inherit", "shell", "resume", "never"} {
		if _, err := ParsePolicy(name); err != nil {
			t.Fatalf("ParsePolicy(%q) failed: %v", name, err)
		}
	}
	if _, err := ParsePolicy("sometimes"); err == nil {
		t.Fatal("an unknown policy was accepted")
	}
}

func TestOfficialSourcesAreTheOnesAdaptersShip(t *testing.T) {
	// The ones that ship. This list is exactly agentintegration.DefaultAdapters,
	// and it is spelled out rather than derived so that adding a source here
	// without adding the adapter that produces it fails.
	for _, kind := range []string{"codex", "claude", "opencode", "pi", "kilo", "kimi", "antigravity", "copilot", "cursor"} {
		source := OfficialSourceFor(kind)
		if source == "" {
			t.Fatalf("no official source is recorded for %q", kind)
		}
		if !Official(source) {
			t.Fatalf("OfficialSourceFor(%q) returned %q, which Official() does not trust", kind, source)
		}
	}
	// amp and muse are catalog agents Sidecar recognises and ships no
	// integration for, and they are the standing example of what this list
	// refuses. pi is the cautionary one: it carried an official source and a
	// capability entry ahead of any adapter, so nothing Sidecar installed could
	// produce a report bearing it, and the only reference it could ever have
	// marked resumable came from a hook Sidecar did not write. Both were
	// retracted, and pi earned its source back only once PiAdapter and
	// assets/pi/sidecar-lifecycle.js shipped -- which is exactly the rule the
	// loop above enforces. cursor and grok were in this refusal list until the
	// session-identity ports gave each one an adapter; they moved up rather
	// than being deleted, because the rule is about what ships and not about
	// which providers were once absent.
	for _, kind := range []string{"amp", "muse"} {
		if OfficialSourceFor(kind) != "" {
			t.Fatalf("%q has no shipped integration but reported an official source", kind)
		}
	}
	if Official("") || Official("sidecar.codex.hooks.evil") {
		t.Fatal("Official() trusted a source it should not")
	}
}

// TestASymlinkInsideAnApprovedRootCannotEscape is the containment rule the
// package comment claims. A lexical prefix test is not containment: a symlink
// planted inside an approved root passes it while pointing anywhere, and the
// target is what actually gets opened.
func TestASymlinkInsideAnApprovedRootCannotEscape(t *testing.T) {
	home := t.TempDir()
	roots := testRoots(home)

	store := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.jsonl")
	if err := os.WriteFile(secret, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A real transcript inside the store still passes.
	real := filepath.Join(store, "rollout-real.jsonl")
	if err := os.WriteFile(real, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := roots.WithinRoots("codex", real); err != nil {
		t.Fatalf("a real transcript inside the store was refused: %v", err)
	}

	// A symlink INSIDE the approved root pointing outside it must not.
	escape := filepath.Join(store, "escape.jsonl")
	if err := os.Symlink(secret, escape); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if err := roots.WithinRoots("codex", escape); !errors.Is(err, ErrOutsideStoreRoot) {
		t.Fatalf("a symlink inside the root escaped containment: %v", err)
	}

	// A symlinked DIRECTORY inside the root is the same escape one level up.
	linkDir := filepath.Join(store, "linked")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if err := roots.WithinRoots("codex", filepath.Join(linkDir, "secret.jsonl")); !errors.Is(err, ErrOutsideStoreRoot) {
		t.Fatal("a path through a symlinked directory escaped containment")
	}
}

// TestAStoreRootThatIsItselfASymlinkStillMatches keeps the fix from being too
// strict: a relocated home or a dotfiles setup makes the root itself a link,
// and its own contents must still be inside it.
func TestAStoreRootThatIsItselfASymlinkStillMatches(t *testing.T) {
	base := t.TempDir()
	realHome := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(realHome, ".codex", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkHome := filepath.Join(base, "home")
	if err := os.Symlink(realHome, linkHome); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	transcript := filepath.Join(realHome, ".codex", "sessions", "a.jsonl")
	if err := os.WriteFile(transcript, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := testRoots(linkHome)
	if err := roots.WithinRoots("codex", transcript); err != nil {
		t.Fatalf("a transcript in a symlinked store root was refused: %v", err)
	}
}

// TestAResumeIsNeverBuiltFromAFlagLikeValue covers the builder boundary
// independently of the validator, because a Ref can be constructed directly.
func TestAResumeIsNeverBuiltFromAFlagLikeValue(t *testing.T) {
	hostile := Ref{
		Kind: RefID, Value: "--dangerously-bypass-approvals-and-sandbox",
		Source: "sidecar.codex.hooks", Reported: true,
	}
	if _, err := PlanResume("codex", hostile); err == nil {
		t.Fatal("a resume was built from a value the provider would read as a flag")
	}
}

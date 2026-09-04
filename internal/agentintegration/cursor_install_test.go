package agentintegration

import (
	"path/filepath"
	"strings"
	"testing"
)

func cursorFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionHookPaths) {
	t.Helper()
	return sessionHookFixture(t, NewCursorAdapter().sessionHookAdapter, opts...)
}

// TestCursorInstallsWhereItsHookLoaderLooks is the measurement that changed
// this port. cursor-agent 2026.08.25 has a configuration-directory resolver
// reading CURSOR_CONFIG_DIR, then $XDG_CONFIG_HOME/cursor, then ~/.cursor --
// and its hook loader does not use it, building the user hooks path from
// homedir() and ".cursor" directly in the same bundle. Herdr honours the
// variable; doing the same here would install into a directory that resolver
// describes and the loader never opens, which is a silently dead integration
// rather than a relocated one.
func TestCursorInstallsWhereItsHookLoaderLooks(t *testing.T) {
	elsewhere := t.TempDir()
	_, env, paths := cursorFixture(t, func(e *Env) { e.ConfigHome = elsewhere })

	if want := filepath.Join(env.Home, ".cursor", "hooks.json"); paths.File != want {
		t.Fatalf("the adapter targets %s, want %s", paths.File, want)
	}
	if strings.HasPrefix(paths.File, elsewhere) {
		t.Fatal("XDG_CONFIG_HOME moved Cursor's hooks file, which its hook loader does not read")
	}
}

// TestCursorRegistersOnSessionStartWithTheMinimalEntry pins both halves of the
// shape. The event is camelCase, which is Cursor's spelling and not Claude's,
// and the entry is a lone `command` member, which is the minimal form Cursor
// documents and the one Herdr writes. Matching upstream exactly is what makes
// the next Herdr bump a diff of nothing.
func TestCursorRegistersOnSessionStartWithTheMinimalEntry(t *testing.T) {
	a := NewCursorAdapter().sessionHookAdapter
	top, err := parseJSONFile([]byte(a.asset().Content))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lastMember(top, "version"); !ok {
		t.Fatalf("a file Sidecar creates has no version header, which is what Cursor's own writer puts there:\n%s", a.asset().Content)
	}
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		t.Fatalf("the asset has no hooks member:\n%s", a.asset().Content)
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].key != CursorEvent {
		t.Fatalf("the asset registers %d events, want exactly %s:\n%s", len(events), CursorEvent, a.asset().Content)
	}
	handlers, err := parseJSONArray(events[0].val)
	if err != nil {
		t.Fatalf("%s does not hold a flat handler array: %v", CursorEvent, err)
	}
	if len(handlers) != 1 {
		t.Fatalf("%s holds %d handlers, want one", CursorEvent, len(handlers))
	}
	handler, err := parseJSONObject(handlers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(handler) != 1 || handler[0].key != "command" {
		t.Fatalf("the handler carries %d members, want exactly `command`:\n%s", len(handler), a.asset().Content)
	}
}

// TestCursorAcceptsTheGeneratorsEntryShapeToo is why the scan treats an absent
// `type` as a command handler rather than requiring one. Cursor's own writer
// emits `{"type": "command", "command": "..."}` when it generates a hooks.json,
// so both forms exist in the wild and a Sidecar entry in either is Sidecar's.
func TestCursorAcceptsTheGeneratorsEntryShapeToo(t *testing.T) {
	spec := NewCursorAdapter().integration.spec
	for name, file := range map[string]string{
		"minimal, as Sidecar writes it": `{"version":1,"hooks":{"` + CursorEvent + `":[{"command":"sidecar agent report-session --kind cursor --hook-stdin"}]}}`,
		"typed, as Cursor generates it": `{"version":1,"hooks":{"` + CursorEvent + `":[{"type":"command","command":"sidecar agent report-session --kind cursor --hook-stdin"}]}}`,
	} {
		scan := scanHookTree(true, []byte(file), spec)
		if scan.parseErr != "" {
			t.Fatalf("%s: %s", name, scan.parseErr)
		}
		if len(scan.owned) != 1 {
			t.Fatalf("%s: scanned as %d Sidecar entries, want one", name, len(scan.owned))
		}
	}
}

// TestCursorAddsItsVersionHeaderOnlyToAFileItCreates is a deliberate departure
// from Herdr, which adds the header to any hooks.json lacking one.
// cursor-agent 2026.08.25's hook loader never reads the key, so adding it to a
// user's file is editing bytes outside Sidecar's entry to no effect -- and it
// is what would stop uninstall giving the file back exactly as it was.
func TestCursorAddsItsVersionHeaderOnlyToAFileItCreates(t *testing.T) {
	t.Run("a file Sidecar creates gets the header", func(t *testing.T) {
		svc, _, paths := cursorFixture(t)
		applyTo(t, svc, CursorProvider, ActionInstall)
		if got := readFileForTest(t, paths.File); !strings.Contains(got, `"version"`) {
			t.Fatalf("a created file has no version header:\n%s", got)
		}
		// And it goes away with the entry, because the whole file was
		// Sidecar's.
		applyTo(t, svc, CursorProvider, ActionUninstall)
		if _, err := readFileMaybe(paths.File); err == nil {
			t.Fatal("uninstall left a stub file nobody wrote on purpose")
		}
	})

	t.Run("a file the user has keeps its own shape", func(t *testing.T) {
		svc, _, paths := cursorFixture(t)
		before := `{
  "hooks": {
    "beforeSubmitPrompt": [
      {
        "command": "theirs.sh"
      }
    ]
  }
}
`
		writeFileForTest(t, paths.File, before)
		applyTo(t, svc, CursorProvider, ActionInstall)

		after := readFileForTest(t, paths.File)
		if strings.Contains(after, `"version"`) {
			t.Fatalf("install added a version header to the user's own file:\n%s", after)
		}
		if !strings.Contains(after, "theirs.sh") {
			t.Fatalf("install dropped the user's hook:\n%s", after)
		}

		applyTo(t, svc, CursorProvider, ActionUninstall)
		if got := readFileForTest(t, paths.File); got != before {
			t.Fatalf("uninstall did not give the file back byte for byte\n got: %s\nwant: %s", got, before)
		}
	})
}

// TestCursorCannotTellItsOwnHeaderFromTheUsers pins the one case the rule above
// does not cover, so it is a decision on the record rather than a surprise.
//
// A user whose hooks.json is exactly `{"version": 1}` and nothing else -- the
// shape cursor-agent's own writer produces before any hook is added -- gets that
// file removed by uninstall rather than handed back. The reason is that the two
// files are the same file: installing into `{"version": 1}` and creating one
// from nothing both end at the identical bytes, in the identical order, so no
// reading of the file can say which happened. Uninstall has to pick one, and it
// picks removal, because leaving a header behind with nothing under it is the
// more confusing of the two outcomes and the backup written beside it holds the
// original either way.
//
// The narrower promise still holds and is tested above: a file carrying
// anything of the user's, including a hook of their own, comes back byte for
// byte.
func TestCursorCannotTellItsOwnHeaderFromTheUsers(t *testing.T) {
	svc, _, paths := cursorFixture(t)
	const before = "{\n  \"version\": 1\n}\n"
	writeFileForTest(t, paths.File, before)

	applyTo(t, svc, CursorProvider, ActionInstall)
	// The header was already there, so install did not add one, and the result
	// is byte-identical to the file Sidecar creates in an empty tree. That
	// equality is the whole reason the case is undecidable.
	if got, want := readFileForTest(t, paths.File), string(NewCursorAdapter().integration.canonicalFile()); got != want {
		t.Fatalf("installing into a bare version header did not converge on the canonical file\n got: %s\nwant: %s", got, want)
	}

	applyTo(t, svc, CursorProvider, ActionUninstall)
	if _, err := readFileMaybe(paths.File); err == nil {
		t.Fatal("uninstall kept the file; the recorded decision is that it goes")
	}
	// What the backup holds is the installed file, not the user's original:
	// uninstall backs up what it is about to remove, over the copy install made
	// of what it was about to rewrite. So the header is recoverable by reading
	// the backup and deleting the hooks member, and not by restoring the backup.
	// Pinned because it is the second half of the same undecidability, and a
	// reader who assumes the backup is the pre-install file would be wrong.
	if got := readFileForTest(t, paths.Backup); got != string(NewCursorAdapter().integration.canonicalFile()) {
		t.Fatalf("the backup does not hold the file uninstall removed\n got: %s", got)
	}
}

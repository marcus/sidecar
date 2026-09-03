package app

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// A file target is the one kind the app deck's open path did not carry, even
// though its Document leaf has always been able to hold one. Both halves are
// checked here: the ref an open resolves to, and the pane kind an explicit
// `--at` cell plans against.
func TestAppDeckOpensAFileTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 50)
	h := m.currentContentDeck()
	if h == nil {
		t.Fatal("content deck was not created")
	}

	target, err := uirequest.ResolveFileTarget(root, "README.md", 12)
	if err != nil {
		t.Fatal(err)
	}
	ref, refusal, ok := h.contentRefForTarget(target)
	if !ok || refusal != "" {
		t.Fatalf("file target refused: ok=%v refusal=%q", ok, refusal)
	}
	if ref.Kind != contentlink.KindFile || ref.Value != "README.md" || ref.Line != 12 {
		t.Fatalf("ref = %+v, want a workspace-relative file ref at line 12", ref)
	}

	if kind, ok := appContentKindForTarget(uirequest.TargetKindFile); !ok || kind != panelayout.Document {
		t.Fatalf("appContentKindForTarget(file) = %v, %v; want Document, true", kind, ok)
	}

	out := m.openAppContentOutcome(h, ref, "right", nil)
	if !out.Accepted() {
		t.Fatalf("file open refused: %+v", out)
	}
	if h.deck.Leaf(panelayout.Document) == 0 {
		t.Fatal("no Document leaf after opening a file target")
	}
}

// A file that cannot be read from the deck's root is declined with a reason
// rather than opening an empty pane.
func TestAppDeckRefusesAnUnreadableFileTarget(t *testing.T) {
	root := t.TempDir()
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 50)
	h := m.currentContentDeck()

	_, refusal, ok := h.contentRefForTarget(uirequest.Target{Kind: uirequest.TargetKindFile, Value: "nope.md"})
	if ok || refusal == "" {
		t.Fatalf("missing file accepted: ok=%v refusal=%q", ok, refusal)
	}
}

// The entry exists only where its result can land: behind the deck's feature
// flag, and on a plugin that holds a deck at all.
func TestPaneSwitcherEntryIsGatedOnTheDeck(t *testing.T) {
	t.Run("feature off", func(t *testing.T) {
		root := t.TempDir()
		p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
		m := appDeckTestModel(t, root, p)
		m.activeContext = "file-browser-tree"
		if !m.paneSwitcherAvailable() {
			t.Fatal("premise: the entry is unavailable even with the flag on")
		}
		off := config.Default()
		off.Features.Flags = map[string]bool{features.PluginContentPanes.Name: false}
		features.Init(off)
		if m.paneSwitcherAvailable() {
			t.Fatal("the switcher is reachable with PluginContentPanes off")
		}
		if _, opened := m.openPaneSwitcher(); opened {
			t.Fatal("the switcher opened with PluginContentPanes off")
		}
	})

	t.Run("plugin without a deck", func(t *testing.T) {
		root := t.TempDir()
		// The workspace plugin owns its own pane tree and is excluded from the
		// app deck, so the app's entry must not appear over it.
		p := &deckHostTestPlugin{id: workspacePluginID, focus: "preview", frame: "x"}
		m := appDeckTestModel(t, root, p)
		m.activeContext = "workspace-list"
		if m.paneSwitcherAvailable() {
			t.Fatal("the switcher is reachable from the workspace plugin")
		}
	})

	t.Run("context that never asked for the key", func(t *testing.T) {
		root := t.TempDir()
		p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
		m := appDeckTestModel(t, root, p)
		// file-browser-quick-open spends ctrl+n on cursor-down; the entry must
		// not appear there even though the plugin is deck-eligible.
		m.activeContext = "file-browser-quick-open"
		if m.paneSwitcherAvailable() {
			t.Fatal("the switcher claimed ctrl+n in a finder context")
		}
		if len(m.paneSwitcherCommands()) != 0 {
			t.Fatal("a finder context was offered the switcher command")
		}
		m.renderContent(200, 50)
		m.handleKeyMsg(ctrlN())
		if m.paneSwitcherOpen {
			t.Fatal("ctrl+n opened the switcher out of a finder's cursor")
		}
	})
}

// The key ladder's half of the entry: ctrl+n reaches the switcher from a
// plugin's browse context, and a focused content leaf — which absorbs every key
// it does not own — hands back the `n` its own context binds, the same key the
// two Workspaces surfaces answer in that same context.
func TestPaneSwitcherOpensFromTheKeyLadder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 50)
	m.activeContext = "file-browser-tree"

	m.handleKeyMsg(ctrlN())
	if !m.paneSwitcherOpen {
		t.Fatal("ctrl+n did not open the switcher from a browse context")
	}
	if m.activeContext != paneSwitcherContext {
		t.Fatalf("context = %q, want %q while the modal owns the keyboard", m.activeContext, paneSwitcherContext)
	}
	m.closePaneSwitcher()

	// Now with a passive leaf focused: the deck is on screen and has the focus.
	h := m.currentContentDeck()
	out := m.openAppContentOutcome(h, contentlink.Ref{Kind: contentlink.KindFile, Value: "README.md"}, "right", nil)
	if !out.Accepted() {
		t.Fatalf("could not open a document leaf: %+v", out)
	}
	h.deck.FocusLeaf(h.deck.Leaf(panelayout.Document))
	m.renderContent(200, 50)
	m.updateContext()
	if m.activeContext != "workspace-doc" {
		t.Fatalf("context = %q, want the focused document leaf's", m.activeContext)
	}
	m.handleKeyMsg(ctrlN())
	if m.paneSwitcherOpen {
		t.Fatal("a leaf answered ctrl+n; its context binds `n`, like the same leaf on both Workspaces surfaces")
	}
	m.handleKeyMsg(keyRune('n'))
	if !m.paneSwitcherOpen {
		t.Fatal("a focused content leaf swallowed the `n` its context binds to open-pane")
	}
}

// All five deck-eligible plugins, driven through the real ladder. The keymap
// parity test proves the bindings exist; this proves a key pressed in each of
// those contexts actually arrives, which is a different claim — a context can
// bind the entry and still never see the key, because the rungs above this one
// forward wholesale for a plugin that types or holds an overlay.
//
// One context per plugin is enough here: the rung reads the key out of the
// keymap for whatever context is active, so what varies between two contexts of
// the same plugin is the binding, and that is the parity test's job.
func TestPaneSwitcherOpensFromEveryDeckEligiblePlugin(t *testing.T) {
	for _, tc := range []struct{ plugin, context string }{
		{"file-browser", "file-browser-tree"},
		{"git-status", "git-status"},
		{"notes", "notes-list"},
		{"tasks", "tasks-list"},
		{"td-monitor", "td-monitor"},
	} {
		t.Run(tc.plugin, func(t *testing.T) {
			p := &deckHostTestPlugin{id: tc.plugin, focus: "preview", frame: "x"}
			m := appDeckTestModel(t, t.TempDir(), p)
			m.renderContent(200, 50)
			m.activeContext = tc.context
			m.handleKeyMsg(ctrlN())
			if !m.paneSwitcherOpen {
				t.Fatalf("%s: ctrl+n in %s did not reach the switcher", tc.plugin, tc.context)
			}
		})
	}
}

// While the modal is up it has the whole keyboard: the plugin underneath must
// not see the keys being typed into the picker's filter, and the footer must
// stop advertising the global keys the modal has taken.
func TestPaneSwitcherOwnsTheKeyboardWhileOpen(t *testing.T) {
	root := t.TempDir()
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	other := &deckHostTestPlugin{id: "notes", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p, other)
	m.renderContent(200, 50)
	m.activeContext = "file-browser-tree"
	m.handleKeyMsg(ctrlN())
	if !m.paneSwitcherOpen {
		t.Fatal("premise: the switcher did not open")
	}

	before := len(p.seen)
	// `2` is a tab switch and `q` quits, everywhere but here.
	m.handleKeyMsg(keyRune('2'))
	m.handleKeyMsg(keyRune('q'))
	if len(p.seen) != before {
		t.Fatalf("the plugin under the modal received %d keys", len(p.seen)-before)
	}
	if !m.paneSwitcherOpen {
		t.Fatal("a printable key closed the switcher")
	}
	if m.activePlugin != 0 {
		t.Fatal("a digit typed into the picker switched plugin tabs")
	}

	labels := map[string]bool{}
	for _, hint := range Model(*m).footerHints() {
		switch hint.keys {
		case "1-2", "8/9/0", "?", "q":
			t.Fatalf("footer advertises %q while the switcher is typing into its filter", hint.keys)
		}
		labels[hint.label] = true
	}
	// What it advertises instead is the modal's own keys, as both Workspaces
	// hosts' create modals do.
	for _, want := range []string{"Open", "Back"} {
		if !labels[want] {
			t.Fatalf("footer hints = %v, missing the switcher's own %q", labels, want)
		}
	}
}

func ctrlN() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl} }

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// Every kind the plugin host offers resolves to the target shape `sidecar open`
// produces, and routes through the deck's own open path onto the leaf that kind
// belongs in. This is what keeps the modal an entry point rather than a second
// implementation.
func TestPaneSwitcherAnswersResolveAndOpenLikeTheCLI(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	notes := &deckHostTestPlugin{id: "notes", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, files, notes)
	m.cfg.TerminalResources.Providers = []config.TerminalResourceProviderConfig{{ID: "jira", Enabled: true}}
	m.activeContext = "file-browser-tree"
	m.renderContent(200, 50)
	h := m.currentContentDeck()
	if h == nil {
		t.Fatal("content deck was not created")
	}
	h.SetResourceMatchers([]contentlink.ResourceMatcher{{Provider: "jira", ID: "key", Re: regexp.MustCompile(`RES-[0-9]+`)}})

	cases := []struct {
		name     string
		kind     workspacecreate.Kind
		raw      string
		want     uirequest.Target
		wantLeaf panelayout.Kind
	}{
		{name: "file", kind: workspacecreate.KindFile, raw: "README.md",
			want:     uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md"},
			wantLeaf: panelayout.Document},
		{name: "issue", kind: workspacecreate.KindIssue, raw: "td-abc123",
			want:     uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-abc123"},
			wantLeaf: panelayout.Issue},
		{name: "note", kind: workspacecreate.KindNote, raw: "nt-4jdj4e",
			want:     uirequest.Target{Kind: uirequest.TargetKindNote, Value: "nt-4jdj4e"},
			wantLeaf: panelayout.Note},
		{name: "diff", kind: workspacecreate.KindDiff, raw: "",
			want:     uirequest.Target{Kind: uirequest.TargetKindDiff, Value: workspacediff.IdentityWorkingTree},
			wantLeaf: panelayout.Diff},
		{name: "resource", kind: workspacecreate.KindResource, raw: "RES-1",
			want:     uirequest.Target{Kind: uirequest.TargetKindResource, Value: "RES-1", Provider: "jira"},
			wantLeaf: panelayout.Resource},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, opened := m.openPaneSwitcher(); !opened {
				t.Fatal("the switcher did not open")
			}
			form := m.paneSwitcher
			form.SetKind(tc.kind)
			if form.Kind() != tc.kind {
				t.Fatalf("kind = %v, want %v — the row is not offered by this host", form.Kind(), tc.kind)
			}
			m.ensurePaneSwitcherModal()
			// Enter on a target-needing row continues to its picker; that is the
			// form's own rule, and the host must be driving it.
			m.handlePaneSwitcherKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if form.Step() != workspacecreate.StepTarget {
				t.Fatalf("step = %v, want the target picker", form.Step())
			}
			form.PickerInput().SetValue(tc.raw)
			form.SyncAfterInput()

			got, err := form.TargetFor(root)
			if err != nil {
				t.Fatalf("TargetFor(%q) = %v", tc.raw, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("target = %+v, want %+v", got, tc.want)
			}

			m.applyPaneSwitcherAction(workspacecreate.ActionCreate)
			if m.paneSwitcherOpen {
				t.Fatalf("the switcher stayed open after opening a pane (error: %q)", form.Error())
			}
			if h.deck.Leaf(tc.wantLeaf) == 0 {
				t.Fatalf("no %v leaf after opening a %s target", tc.wantLeaf, tc.name)
			}
		})
	}
}

// The file scan feeds the picker, and a scan that outlived the root it was
// fired for is dropped rather than offering paths that resolve to nothing.
func TestPaneSwitcherFileScanIsRootScoped(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 50)
	m.activeContext = "file-browser-tree"
	if _, opened := m.openPaneSwitcher(); !opened {
		t.Fatal("the switcher did not open")
	}

	m.applyPaneSwitcherFiles(paneSwitcherFilesMsg{Root: filepath.Join(root, "elsewhere"), Paths: []string{"stale.md"}})
	m.applyPaneSwitcherFiles(paneSwitcherFilesMsg{Root: root, Paths: []string{"README.md"}})

	m.paneSwitcher.SetKind(workspacecreate.KindFile)
	m.ensurePaneSwitcherModal()
	m.handlePaneSwitcherKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.paneSwitcher.PickerInput().SetValue("README")
	m.paneSwitcher.SyncAfterInput()

	got, err := m.paneSwitcher.TargetFor(root)
	if err != nil {
		t.Fatalf("TargetFor = %v — the scan never reached the picker", err)
	}
	if got.Value != "README.md" {
		t.Fatalf("target = %+v, want the scanned candidate", got)
	}
}

// The command that puts the entry in the footer and the palette is the app's,
// declared for whichever plugin browse context is on screen.
func TestPaneSwitcherContributesItsFooterCommand(t *testing.T) {
	root := t.TempDir()
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p)
	m.activeContext = "file-browser-tree"

	commands := m.paneSwitcherCommands()
	if len(commands) != 1 {
		t.Fatalf("paneSwitcherCommands() = %d entries, want 1", len(commands))
	}
	if commands[0].ID != paneSwitcherCommand || commands[0].Context != "file-browser-tree" {
		t.Fatalf("command = %+v, want %s in the focused context", commands[0], paneSwitcherCommand)
	}
	hints := Model(*m).commandFooterHints(commands, m.activeContext)
	if len(hints) != 1 || hints[0].label != commands[0].Name {
		t.Fatalf("footer hints = %+v, want the switcher entry", hints)
	}
	if hints[0].keys != paneSwitcherKeyName {
		t.Fatalf("footer hint key = %q, want %q", hints[0].keys, paneSwitcherKeyName)
	}
	if handler := m.pluginCommandHandler(paneSwitcherCommand, "file-browser-tree"); handler == nil {
		t.Fatal("the palette cannot run the switcher command")
	}
}

// A focused passive leaf reaches the switcher too, and its footer has to say so.
// The leaf's own commands (Close, the tab controls) come from appContentCommands,
// which has never named this one, and they are merged in a different branch of
// the footer switch — so the entry is only visible if the app contributes it
// outside that switch, under the key the leaf's context actually binds.
func TestPaneSwitcherFooterEntryOnAFocusedLeaf(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &deckHostTestPlugin{id: "file-browser", focus: "preview", frame: "x"}
	m := appDeckTestModel(t, root, p)
	m.renderContent(200, 50)
	h := m.currentContentDeck()
	out := m.openAppContentOutcome(h, contentlink.Ref{Kind: contentlink.KindFile, Value: "README.md"}, "right", nil)
	if !out.Accepted() {
		t.Fatalf("could not open a document leaf: %+v", out)
	}
	h.deck.FocusLeaf(h.deck.Leaf(panelayout.Document))
	m.renderContent(200, 50)
	m.updateContext()

	commands := m.paneSwitcherCommands()
	if len(commands) != 1 || commands[0].Context != "workspace-doc" {
		t.Fatalf("paneSwitcherCommands() = %+v, want the entry in the leaf's context", commands)
	}
	found := false
	for _, hint := range Model(*m).footerHints() {
		if hint.label == commands[0].Name {
			found = true
			if hint.keys != "n" {
				t.Fatalf("leaf footer advertises %q, want the `n` the two Workspaces surfaces answer", hint.keys)
			}
		}
	}
	if !found {
		t.Fatal("the footer of a focused leaf never names the switcher")
	}
}

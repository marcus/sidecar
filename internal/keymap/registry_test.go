package keymap

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestRegistry_SingleKey(t *testing.T) {
	r := NewRegistry()

	called := false
	r.RegisterCommand(Command{
		ID:   "test-cmd",
		Name: "Test",
		Handler: func() tea.Cmd {
			called = true
			return nil
		},
	})
	r.RegisterBinding(Binding{Key: "t", Command: "test-cmd", Context: "global"})

	key := tea.KeyPressMsg{Code: 't', Text: "t"}
	r.Handle(key, "global")

	if !called {
		t.Error("command handler not called")
	}
}

func TestRegistry_KeySequence(t *testing.T) {
	r := NewRegistry()

	called := false
	r.RegisterCommand(Command{
		ID:   "go-top",
		Name: "Go to top",
		Handler: func() tea.Cmd {
			called = true
			return nil
		},
	})
	r.RegisterBinding(Binding{Key: "g g", Command: "go-top", Context: "global"})

	// First 'g' should start sequence
	key1 := tea.KeyPressMsg{Code: 'g', Text: "g"}
	r.Handle(key1, "global")

	if called {
		t.Error("should not call handler after first key")
	}
	if !r.HasPending() {
		t.Error("should have pending key")
	}

	// Second 'g' should complete sequence
	key2 := tea.KeyPressMsg{Code: 'g', Text: "g"}
	r.Handle(key2, "global")

	if !called {
		t.Error("command handler not called for sequence")
	}
}

func TestRegistry_KeySequenceTimeout(t *testing.T) {
	r := NewRegistry()

	called := false
	r.RegisterCommand(Command{
		ID:   "go-top",
		Name: "Go to top",
		Handler: func() tea.Cmd {
			called = true
			return nil
		},
	})
	r.RegisterBinding(Binding{Key: "g g", Command: "go-top", Context: "global"})

	// First 'g'
	key1 := tea.KeyPressMsg{Code: 'g', Text: "g"}
	r.Handle(key1, "global")

	// Wait for timeout
	time.Sleep(sequenceTimeout + 10*time.Millisecond)

	// Second 'g' should not complete sequence due to timeout
	key2 := tea.KeyPressMsg{Code: 'g', Text: "g"}
	r.Handle(key2, "global")

	if called {
		t.Error("sequence should have timed out")
	}
}

func TestRegistry_ContextPrecedence(t *testing.T) {
	r := NewRegistry()

	globalCalled := false
	contextCalled := false

	r.RegisterCommand(Command{
		ID:   "global-action",
		Name: "Global Action",
		Handler: func() tea.Cmd {
			globalCalled = true
			return nil
		},
	})
	r.RegisterCommand(Command{
		ID:   "context-action",
		Name: "Context Action",
		Handler: func() tea.Cmd {
			contextCalled = true
			return nil
		},
	})

	r.RegisterBinding(Binding{Key: "s", Command: "global-action", Context: "global"})
	r.RegisterBinding(Binding{Key: "s", Command: "context-action", Context: "git-status"})

	// With git-status context, should use context binding
	key := tea.KeyPressMsg{Code: 's', Text: "s"}
	r.Handle(key, "git-status")

	if globalCalled {
		t.Error("global handler should not be called")
	}
	if !contextCalled {
		t.Error("context handler should be called")
	}
}

func TestRegistry_UserOverride(t *testing.T) {
	r := NewRegistry()

	defaultCalled := false
	overrideCalled := false

	r.RegisterCommand(Command{
		ID:   "default-action",
		Name: "Default",
		Handler: func() tea.Cmd {
			defaultCalled = true
			return nil
		},
	})
	r.RegisterCommand(Command{
		ID:   "override-action",
		Name: "Override",
		Handler: func() tea.Cmd {
			overrideCalled = true
			return nil
		},
	})

	r.RegisterBinding(Binding{Key: "x", Command: "default-action", Context: "global"})
	r.SetUserOverride("x", "override-action")

	key := tea.KeyPressMsg{Code: 'x', Text: "x"}
	r.Handle(key, "global")

	if defaultCalled {
		t.Error("default handler should not be called")
	}
	if !overrideCalled {
		t.Error("override handler should be called")
	}
}

func TestRegistry_SpecialKeys(t *testing.T) {
	cases := []struct {
		key    tea.KeyPressMsg
		expect string
	}{
		{tea.KeyPressMsg{Code: tea.KeyTab}, "tab"},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{tea.KeyPressMsg{Code: tea.KeyEsc}, "esc"},
		{tea.KeyPressMsg{Code: tea.KeyUp}, "up"},
		{tea.KeyPressMsg{Code: tea.KeyDown}, "down"},
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
	}

	for _, tc := range cases {
		got := keyToString(tc.key)
		if got != tc.expect {
			t.Errorf("keyToString(%v) = %q, want %q", tc.key, got, tc.expect)
		}
	}
}

func TestRegistry_GetCommand(t *testing.T) {
	r := NewRegistry()

	// Register a command
	r.RegisterCommand(Command{
		ID:      "test-cmd",
		Name:    "Test Command",
		Handler: func() tea.Cmd { return nil },
		Context: "global",
	})

	// Test finding existing command
	cmd, ok := r.GetCommand("test-cmd")
	if !ok {
		t.Error("GetCommand should find registered command")
	}
	if cmd.ID != "test-cmd" {
		t.Errorf("GetCommand returned wrong ID: got %q, want %q", cmd.ID, "test-cmd")
	}
	if cmd.Name != "Test Command" {
		t.Errorf("GetCommand returned wrong Name: got %q, want %q", cmd.Name, "Test Command")
	}

	// Test missing command
	_, ok = r.GetCommand("nonexistent")
	if ok {
		t.Error("GetCommand should return false for missing command")
	}
}

func TestDefaultBindings_GlobalShortcuts(t *testing.T) {
	// App-level globals hard-coded in update.go must also appear here so the
	// command palette (BuildEntries) can discover them.
	want := map[string]string{
		"i":      "open-issue",
		"W":      "switch-worktree",
		"#":      "switch-theme",
		"r":      "refresh",
		"ctrl+c": "quit",
		"q":      "quit",
		"@":      "switch-project",
		"^":      "open-in",
		"K":      "toggle-overview",
		"?":      "toggle-palette",
		"!":      "toggle-diagnostics",
		"[":      "prev-plugin",
		"]":      "next-plugin",
	}

	found := make(map[string]string)
	for _, b := range DefaultBindings() {
		if b.Context != "global" {
			continue
		}
		if _, ok := want[b.Key]; ok {
			found[b.Key] = b.Command
		}
	}

	for key, cmd := range want {
		got, ok := found[key]
		if !ok {
			t.Errorf("missing global binding for key %q (want command %q)", key, cmd)
			continue
		}
		if got != cmd {
			t.Errorf("global key %q: command = %q, want %q", key, got, cmd)
		}
	}
}

func TestDefaultBindings_DoNotBindCtrlKKillShell(t *testing.T) {
	for _, b := range DefaultBindings() {
		if b.Key == "ctrl+k" && b.Command == "kill-shell" {
			t.Fatalf("ctrl+k is still bound to kill-shell: %+v", b)
		}
	}
}

func TestDefaultBindings_DoNotAdvertiseGlobalWorkspacesPreview(t *testing.T) {
	for _, b := range DefaultBindings() {
		if b.Context == "global-workspaces-preview" {
			t.Fatalf("default bindings still advertise watched-preview: %+v", b)
		}
	}
}

func TestDefaultBindings_KeepApprovalKeysOutOfWorkspaces(t *testing.T) {
	for _, b := range DefaultBindings() {
		if b.Context != "workspace-list" && b.Context != "workspace-preview" {
			continue
		}
		switch b.Command {
		case "approve", "approve-all", "reject":
			t.Fatalf("approval binding leaked into %s: %+v", b.Context, b)
		}
	}
}

func TestDefaultBindings_NotesDefaultAndExplicitEditorsStayReachable(t *testing.T) {
	// Enter is preference-driven; i/e/E remain deterministic paths to all
	// three editors regardless of that preference.
	want := map[string]string{
		"notes-list:enter":    "open-note",
		"notes-list:i":        "edit-note",
		"notes-list:e":        "vim-edit",
		"notes-list:E":        "external-editor",
		"notes-preview:enter": "open-note",
		"notes-preview:i":     "edit-note",
		"notes-preview:e":     "vim-edit",
		"notes-preview:E":     "external-editor",
		"notes-preview:m":     "toggle-markdown",
	}
	got := map[string]string{}
	for _, b := range DefaultBindings() {
		if !strings.HasPrefix(b.Context, "notes-") {
			continue
		}
		switch b.Command {
		case "open-note", "edit-note", "vim-edit", "external-editor", "toggle-markdown":
			got[b.Context+":"+b.Key] = b.Command
		}
	}
	for k, cmd := range want {
		if got[k] != cmd {
			t.Errorf("%s = %q, want %q", k, got[k], cmd)
		}
	}
	for k, cmd := range got {
		if want[k] == "" {
			t.Errorf("unexpected notes editor binding %s = %q", k, cmd)
		}
	}
}

// TestDefaultBindings_NotesEditorPaneKeepsPrintableKeys pins the reason E is
// absent from notes-editor: the old binding meant a capital E could never be
// typed into a note.
func TestDefaultBindings_NotesEditorPaneKeepsPrintableKeys(t *testing.T) {
	for _, b := range DefaultBindings() {
		if b.Context != "notes-editor" {
			continue
		}
		if len(b.Key) == 1 {
			t.Errorf("notes-editor binds bare printable key %q to %s", b.Key, b.Command)
		}
	}
}

func TestDefaultBindings_NotesEditorSelectionUsesCombinedModifiers(t *testing.T) {
	// Bindings must match the actual KeyPressMsg.String() of combined
	// modifiers, not a hand-written fixture that could drift.
	want := map[string]string{
		tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}.String():                  "select-up",
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}.String():                "select-down",
		tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}.String():                "select-left",
		tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}.String():               "select-right",
		tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModShift}.String():                "select-line-start",
		tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModShift}.String():                 "select-line-end",
		tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt}.String():                          "select-toggle",
		tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt}.String():                          "select-all",
		tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper}.String():                        "select-all",
		tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModSuper}.String():                  "note-start",
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModSuper}.String():                "note-end",
		tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModSuper | tea.ModShift}.String():   "select-note-start",
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModSuper | tea.ModShift}.String(): "select-note-end",
		tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt}.String():                          "cut",
		tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}.String():                         "undo-edit",
		tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}.String():                         "redo-edit",
		tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl | tea.ModShift}.String():          "redo-edit",
	}
	got := map[string]string{}
	for _, b := range DefaultBindings() {
		if b.Context != "notes-editor" {
			continue
		}
		if _, ok := want[b.Key]; ok {
			got[b.Key] = b.Command
		}
	}
	for key, cmd := range want {
		if key == "" {
			t.Fatal("combined-modifier KeyPressMsg.String() was empty")
		}
		if got[key] != cmd {
			t.Errorf("%q = %q, want %q", key, got[key], cmd)
		}
	}
}

func TestDefaultBindings_NotesCtrlYIsContextSpecific(t *testing.T) {
	want := map[string]string{
		"notes-list":        "yank-id",
		"notes-preview":     "yank-id",
		"notes-editor":      "redo-edit",
		"notes-inline-edit": "",
	}
	for context, command := range want {
		got := ""
		for _, binding := range DefaultBindings() {
			if binding.Context == context && binding.Key == "ctrl+y" {
				got = binding.Command
				break
			}
		}
		if got != command {
			t.Errorf("%s ctrl+y = %q, want %q", context, got, command)
		}
	}
}

// TestConfigurationActionsAllHaveKeys keeps every page action Configuration
// advertises reachable from the help modal and the palette, not only from the
// pill that prints it. The Integrations table's four verbs joined this list
// when the route became a table; the rest were already here.
func TestConfigurationActionsAllHaveKeys(t *testing.T) {
	bound := map[string]bool{}
	for _, b := range DefaultBindings() {
		if b.Context == "config" {
			bound[b.Command] = true
		}
	}
	for _, command := range []string{
		"recheck", "copy-guidance", "open-file",
		"add-project", "init-repo", "remove-project",
		"edit-host", "enable-remote-hosts",
		"use-global-theme", "test-notifications",
		"install-integration", "update-integration", "repair-integration", "uninstall-integration",
	} {
		if !bound[command] {
			t.Fatalf("%s has no key in the config context", command)
		}
	}
}

package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
)

func TestUIRequests_ProviderResourceUsesLiveMatcherWithoutTerminalRitual(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	config.SetTestStateDir(filepath.Join(stateHome, "sidecar"))
	t.Cleanup(config.ResetTestStateDir)
	stubTd(t)
	root := t.TempDir()
	p := interactiveUIRequestTestPlugin(t, root)
	p.SetResourceMatchers([]terminallink.ResourceMatcher{{
		Provider: "jira-work", ID: "issue-key", Re: regexp.MustCompile(`CASH-[0-9]+`),
	}})
	p.SetResourceResolver((&resourceStub{}).resolve)

	cmd := p.handleUIRequest(uirequest.Request{
		ID: "provider-open", Action: uirequest.ActionOpen, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin: uirequest.Origin{TmuxSession: "test-shell"},
		Target: uirequest.Target{Kind: uirequest.TargetKindResource, Provider: "jira-work", Value: "CASH-1245"},
	})
	if cmd == nil {
		t.Fatal("resource request did not open")
	}
	res, _ := p.activeResourcePane()
	if res == nil || res.tabs.Find(resourceview.TabKey(resourceview.Ref{Instance: "jira-work", Matcher: "issue-key", Locator: "CASH-1245"})) < 0 {
		t.Fatal("resource request did not use the claiming live matcher")
	}
	if p.viewMode == ViewModeInteractive || (p.interactiveState != nil && p.interactiveState.Active) {
		t.Fatal("resource leaf took focus without releasing terminal input")
	}
}

func TestUIRequests_ProviderResourceDeclinesUnclaimedLocator(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	config.SetTestStateDir(filepath.Join(stateHome, "sidecar"))
	t.Cleanup(config.ResetTestStateDir)
	stubTd(t)
	root := t.TempDir()
	p := interactiveUIRequestTestPlugin(t, root)
	p.SetResourceMatchers([]terminallink.ResourceMatcher{{
		Provider: "jira-work", ID: "issue-key", Re: regexp.MustCompile(`CASH-[0-9]+`),
	}})
	req := uirequest.Request{
		ID: "provider-decline", Action: uirequest.ActionOpen, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin: uirequest.Origin{TmuxSession: "test-shell"},
		Target: uirequest.Target{Kind: uirequest.TargetKindResource, Provider: "jira-work", Value: "NOPE-1"},
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatal("unclaimed locator emitted an open command")
	}
	acks, err := uirequest.ReadAcks(filepath.Join(stateHome, "sidecar"), req.ID, req.Action)
	if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusDeclined || acks[0].Reason == "" {
		t.Fatalf("decline ack = %+v err=%v", acks, err)
	}
}

// This is the live `sidecar open` journey: the request arrives while the shell
// owns interactive input, mutates the pane tree without exiting that mode, and
// must hand the active terminal component the tree's new viewport immediately.
// The assertion is driven through the model's deferred-resize path and accepted
// by that same activation without executing the resulting tmux command.
func TestUIRequests_InteractiveOpenAssertsPostSplitTerminalGeometryOnce(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	stubTd(t)

	tests := []struct {
		name   string
		target uirequest.Target
		close  func(*Plugin) tea.Cmd
	}{
		{name: "Doc", target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md", Line: 1}, close: func(p *Plugin) tea.Cmd { return p.closeDocPane() }},
		{name: "Issue", target: uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-1a2b3c"}, close: func(p *Plugin) tea.Cmd { return p.hideIssuePane() }},
		{name: "Diff", target: uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "wt"}, close: func(p *Plugin) tea.Cmd { return p.hideDiffPane() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeDocPaneFixture(t, root, "README.md", "# resize\n")
			p := interactiveUIRequestTestPlugin(t, root)
			before, ok := p.terminalLeafBox()
			if !ok {
				t.Fatal("no terminal leaf before split")
			}
			openArmedAt := armDeferredResize(p.primaryTermPane().Terminal)

			cmd := p.handleUIRequest(uirequest.Request{
				ID: "interactive-" + tt.name, Action: uirequest.ActionOpen,
				CreatedAt: time.Now().UTC(), TTLMs: 5000,
				Origin: uirequest.Origin{TmuxSession: "test-shell"}, Target: tt.target,
			})
			assertResizeStayedDeferred(t, p.primaryTermPane().Terminal, openArmedAt)
			after, ok := p.terminalLeafBox()
			if !ok || after.W >= before.W {
				t.Fatalf("post-open terminal leaf = %+v ok=%v, want narrower than %+v", after, ok, before)
			}
			assertInteractiveTerminalGeometry(t, p, after)
			assertDeferredGeometryAssertion(t, p, lastBatchCommand(t, cmd), after)
			if p.viewMode != ViewModeInteractive || p.interactiveState == nil || !p.interactiveState.Active {
				t.Fatal("sidecar open exited the live interactive terminal")
			}
			openW, openH := p.primaryTermPane().Terminal.Width, p.primaryTermPane().Terminal.Height
			_ = p.View(p.width, p.height)
			if len(p.paneSizeCmds) != 0 || p.primaryTermPane().Terminal.Width != openW || p.primaryTermPane().Terminal.Height != openH {
				t.Fatalf("View scheduled or applied terminal geometry: queued=%d size=%dx%d", len(p.paneSizeCmds), p.primaryTermPane().Terminal.Width, p.primaryTermPane().Terminal.Height)
			}

			closeArmedAt := armDeferredResize(p.primaryTermPane().Terminal)
			closeCmd := tt.close(p)
			assertResizeStayedDeferred(t, p.primaryTermPane().Terminal, closeArmedAt)
			grown, ok := p.terminalLeafBox()
			if !ok || grown != before {
				t.Fatalf("post-close terminal leaf = %+v ok=%v, want original %+v", grown, ok, before)
			}
			assertInteractiveTerminalGeometry(t, p, grown)
			assertDeferredGeometryAssertion(t, p, closeCmd, grown)
		})
	}
}

func TestUIRequests_CreateShellSplitIgnoresSessionsOrigin(t *testing.T) {
	enableWorkspaceFeature(t, features.WorkspaceTerminalPanel.Name)
	stubTd(t)
	p := docPaneTestPlugin(t, t.TempDir(), true)
	p.sidebarVisible = false
	p.View(p.width, p.height)

	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindShell, DisplayName: "dev server", Focus: &focus,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-split-sessions", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "test-shell", Sessions: true},
		Options: uirequest.Options{Split: "right"},
		Payload: payload,
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatalf("project plugin stole a Sessions split: %T", cmd)
	}
	if p.shellLeaf() != nil {
		t.Fatal("project plugin opened a shell leaf for a Sessions-origin split")
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 0 {
		t.Fatalf("expected no project ack, got %+v", acks)
	}
}

func TestUIRequests_CreateShellSplitPlacement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		placement string
		want      SplitAxis
	}{
		{name: "auto", placement: "auto", want: SplitCols},
		{name: "right", placement: "right", want: SplitCols},
		{name: "below", placement: "below", want: SplitRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enableWorkspaceFeature(t, features.WorkspaceTerminalPanel.Name)
			stubTd(t)
			p := docPaneTestPlugin(t, t.TempDir(), true)
			p.sidebarVisible = false
			p.View(p.width, p.height)

			focus := true
			payload, err := json.Marshal(uirequest.CreatePayload{
				Kind: uirequest.CreateKindShell, DisplayName: "dev server", Focus: &focus,
			})
			if err != nil {
				t.Fatal(err)
			}
			req := uirequest.Request{
				ID: "req-split-" + tc.name, Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
				Origin:  uirequest.Origin{TmuxSession: "test-shell"},
				Options: uirequest.Options{Split: tc.placement},
				Payload: payload,
			}
			if cmd := p.handleUIRequest(req); cmd == nil {
				t.Fatal("expected createTerminalSplit cmd")
			}
			leaf := p.shellLeaf()
			if leaf == nil {
				t.Fatalf("no shell leaf after a %s create", tc.placement)
			}
			split := FindPane(p.paneRoot, p.shellSplitID())
			if split == nil || split.Split == nil || split.Split.Axis != tc.want {
				t.Fatalf("axis = %+v, want %v", split, tc.want)
			}
			if got := p.shellLeafTitle(); got != "dev server" {
				t.Fatalf("leaf title = %q", got)
			}
			acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
			if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusOpened {
				t.Fatalf("acks = %+v err=%v", acks, err)
			}
			if acks[0].Surface != "shell:"+p.requireShellTermPane().Session {
				t.Fatalf("surface = %q session = %q", acks[0].Surface, p.requireShellTermPane().Session)
			}
		})
	}
}

func TestUIRequests_CreateShellSplitSeedsReusedSession(t *testing.T) {
	enableWorkspaceFeature(t, features.WorkspaceTerminalPanel.Name)
	stubTd(t)
	p := docPaneTestPlugin(t, t.TempDir(), true)
	p.sidebarVisible = false
	p.View(p.width, p.height)

	if p.createTerminalSplit("dev server", "right") == nil {
		t.Fatal("first split did not open")
	}
	if p.shellLeaf() == nil || p.requireShellTermPane().Session == "" || p.requireShellTermPane().Buffer == nil {
		t.Fatal("first split did not claim a session")
	}
	p.rememberShellSplit()
	p.hideShellTermPane()
	p.setShellLeafFocused(false)
	p.shellLeafSurface = ""
	p.syncShellLeaf()
	if p.shellLeaf() != nil {
		t.Fatal("hide left a shell leaf")
	}

	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindShell, DisplayName: "dev server", Focus: &focus, Run: "echo HELLO_FROM_SPLIT",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-split-reuse-seed", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "test-shell"},
		Options: uirequest.Options{Split: "right"},
		Payload: payload,
	}
	if cmd := p.handleUIRequest(req); cmd == nil {
		t.Fatal("reused split returned no cmd")
	}
	if p.shellLeaf() == nil {
		t.Fatal("reused split did not restore the leaf")
	}
	if p.pendingTermPanelSeed != nil {
		t.Fatal("reuse left --run queued instead of applying it")
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusOpened {
		t.Fatalf("acks = %+v err=%v", acks, err)
	}
}

func TestUIRequests_CreateShellSplitCapDeclines(t *testing.T) {
	enableWorkspaceFeature(t, features.WorkspaceTerminalPanel.Name)
	p := shellLeafTestPlugin(t, SplitCols)
	before := clonePaneTree(p.paneRoot)
	beforeLeaf := p.shellLeaf()

	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindShell, DisplayName: "second", Focus: &focus,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-split-cap", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "test-shell"},
		Options: uirequest.Options{Split: "right"},
		Payload: payload,
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatal("cap should not return a create cmd")
	}
	if leaf := p.shellLeaf(); leaf != beforeLeaf {
		t.Fatalf("tree changed: %+v", leaf)
	}
	if !reflect.DeepEqual(p.paneRoot, before) {
		t.Fatal("pane tree changed on cap refusal")
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusDeclined {
		t.Fatalf("acks = %+v err=%v", acks, err)
	}
	if acks[0].Reason != shellCapMessage {
		t.Fatalf("reason = %q", acks[0].Reason)
	}
}

func TestUIRequests_CreateShellSplitFlagOffDeclines(t *testing.T) {
	stubTd(t)
	features.SetOverride(features.WorkspaceTerminalPanel.Name, false)
	t.Cleanup(func() { features.Init(config.Default()) })
	p := docPaneTestPlugin(t, t.TempDir(), true)
	p.View(p.width, p.height)
	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindShell, DisplayName: "dev server", Focus: &focus,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-split-flag", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "test-shell"},
		Options: uirequest.Options{Split: "auto"},
		Payload: payload,
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatal("flag off should not split")
	}
	if p.shellLeaf() != nil {
		t.Fatal("flag off opened a shell leaf")
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusDeclined {
		t.Fatalf("acks = %+v err=%v", acks, err)
	}
	if !strings.Contains(acks[0].Reason, features.WorkspaceTerminalPanel.Name) {
		t.Fatalf("reason = %q, want the flag name", acks[0].Reason)
	}
}

func TestUIRequests_CreateShellSplitForeignOriginIgnored(t *testing.T) {
	enableWorkspaceFeature(t, features.WorkspaceTerminalPanel.Name)
	stubTd(t)
	p := docPaneTestPlugin(t, t.TempDir(), true)
	p.View(p.width, p.height)
	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindShell, DisplayName: "dev server", Focus: &focus,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-split-foreign", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "other-session"},
		Options: uirequest.Options{Split: "right"},
		Payload: payload,
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatal("foreign origin should be ignored")
	}
	if p.shellLeaf() != nil {
		t.Fatal("foreign origin opened a shell leaf")
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 0 {
		t.Fatalf("expected no ack, got %+v", acks)
	}
}

func TestUIRequests_CreateShellSelectsAndAcks(t *testing.T) {
	p := &Plugin{
		shells: []*ShellSession{{Name: "Shell 1", TmuxName: "sidecar-sh-sidecar-1"}},
	}
	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindShell, Session: "sidecar-sh-sidecar-2", DisplayName: "dev server", Focus: &focus,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-create-shell", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{WorkDir: "/tmp/proj"},
		Payload: payload,
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatalf("expected no sync cmd on a bare plugin, got %T", cmd)
	}
	if len(p.shells) != 2 || p.shells[1].TmuxName != "sidecar-sh-sidecar-2" || p.shells[1].Name != "dev server" {
		t.Fatalf("shells = %+v", p.shells)
	}
	selected := p.getSelectedShell()
	if selected == nil || selected.TmuxName != "sidecar-sh-sidecar-2" {
		t.Fatalf("selected = %#v", selected)
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusOpened || acks[0].Surface != "shell:sidecar-sh-sidecar-2" {
		t.Fatalf("acks = %+v err=%v", acks, err)
	}
}

func TestUIRequests_CreateWorktreeDoesNotDuplicateInventoriedRow(t *testing.T) {
	path := t.TempDir()
	p := &Plugin{
		worktrees: []*Worktree{{Name: "cli-wt", Path: path, Key: "hash-key", Branch: "old"}},
	}
	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindWorktree, Session: "sidecar-ws-cli-wt", DisplayName: "renamed",
		Focus: &focus, Path: path, Branch: "cli-wt",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-create-wt-dup", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{WorkDir: path},
		Payload: payload,
	}
	_ = p.handleUIRequest(req)
	if len(p.worktrees) != 1 {
		t.Fatalf("worktrees grew to %d: %+v", len(p.worktrees), p.worktrees)
	}
	if p.worktrees[0].Key != "hash-key" {
		t.Fatalf("replaced inventoried key: %+v", p.worktrees[0])
	}
	if p.worktrees[0].Name != "renamed" || p.worktrees[0].Branch != "cli-wt" {
		t.Fatalf("did not update row: %+v", p.worktrees[0])
	}
	if p.selectedWorktree() == nil || p.selectedWorktree().Key != "hash-key" {
		t.Fatalf("selected = %#v", p.selectedWorktree())
	}
}

func TestUIRequests_CreateWorktreeSelectsAndAcks(t *testing.T) {
	mainPath := t.TempDir()
	publicSitePath := t.TempDir()
	createdPath := t.TempDir()
	mainKey, err := projectdir.WorktreeKey(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	publicSiteKey, err := projectdir.WorktreeKey(publicSitePath)
	if err != nil {
		t.Fatal(err)
	}
	createdKey, err := projectdir.WorktreeKey(createdPath)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "main", Path: mainPath, Key: mainKey},
			{Name: "public-site", Path: publicSitePath, Key: publicSiteKey},
		},
	}
	focus := true
	payload, err := json.Marshal(uirequest.CreatePayload{
		Kind: uirequest.CreateKindWorktree, Session: "sidecar-ws-cli-wt", DisplayName: "cli-wt",
		Focus: &focus, Path: createdPath, Branch: "cli-wt",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := uirequest.Request{
		ID: "req-create-wt", Action: uirequest.ActionCreate, CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{WorkDir: mainPath},
		Payload: payload,
	}
	_ = p.handleUIRequest(req)
	if p.selectedWorktree() == nil || p.selectedWorktree().Name != "cli-wt" || p.selectedWorktree().Key != createdKey {
		t.Fatalf("selected worktree = %#v", p.selectedWorktree())
	}

	// The inventory order need not match the provisional append order. Before
	// the request row carried its durable key, this refresh kept index 2 and
	// silently selected public-site instead of the newly created checkout.
	p.ctx = &plugin.Context{Epoch: 1, WorkDir: mainPath, ProjectRoot: mainPath, Config: config.Default()}
	p.operationCtx = context.Background()
	p.repoSnapshot = &RepoSnapshot{Key: "repo", CanonicalRoot: mainPath}
	p.refreshOperationID = "refresh-created"
	_, _ = p.update(RefreshDoneMsg{
		OperationScope: OperationScope{Epoch: 1, OperationID: "refresh-created", RepoKey: "repo"},
		Snapshot:       p.repoSnapshot,
		Worktrees: []*Worktree{
			{Name: "main", Path: mainPath, Key: mainKey},
			{Name: "cli-wt", Path: createdPath, Key: createdKey},
			{Name: "public-site", Path: publicSitePath, Key: publicSiteKey},
		},
	})
	if selected := p.selectedWorktree(); selected == nil || selected.Path != createdPath {
		t.Fatalf("selected after refresh = %#v, want created worktree %q", selected, createdPath)
	}
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil || len(acks) != 1 || acks[0].Status != uirequest.StatusOpened {
		t.Fatalf("acks = %+v err=%v", acks, err)
	}
}

func TestUIRequests_WorktreeRenameRepaintsLiveList(t *testing.T) {
	path := t.TempDir()
	p := &Plugin{worktrees: []*Worktree{{Name: "panes", Path: path}}}
	p.handleUIRequest(uirequest.Request{
		Action: uirequest.ActionRenameWorktree,
		Origin: uirequest.Origin{WorkDir: path},
		Target: uirequest.Target{Kind: uirequest.TargetKindWorktree, Value: "pane handle polish"},
	})
	if got := p.worktrees[0].Name; got != "pane handle polish" {
		t.Fatalf("live worktree name = %q", got)
	}
}

func TestUIRequests_ShellRenameRepaintsLiveList(t *testing.T) {
	p := &Plugin{
		shells: []*ShellSession{{Name: "Shell 1", TmuxName: "sidecar-sh-sidecar-1"}},
		nestedByWorkDir: map[string][]*ShellSession{
			"/tmp/other": {{Name: "Nested 1", TmuxName: "sidecar-sh-sidecar-2"}},
		},
	}
	p.handleUIRequest(uirequest.Request{
		Action: uirequest.ActionRenameShell,
		Origin: uirequest.Origin{TmuxSession: "sidecar-sh-sidecar-1"},
		Target: uirequest.Target{Kind: uirequest.TargetKindShell, Value: "active task"},
	})
	if got := p.shells[0].Name; got != "active task" {
		t.Fatalf("live shell name = %q", got)
	}

	p.handleUIRequest(uirequest.Request{
		Action: uirequest.ActionRenameShell,
		Origin: uirequest.Origin{TmuxSession: "sidecar-sh-sidecar-2"},
		Target: uirequest.Target{Kind: uirequest.TargetKindShell, Value: "nested task"},
	})
	if got := p.nestedByWorkDir["/tmp/other"][0].Name; got != "nested task" {
		t.Fatalf("nested shell name = %q", got)
	}
}

func TestUIRequests_RefusedInteractiveSplitEmitsNoGeometryAssertion(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	stubTd(t)
	tests := []struct {
		name   string
		target uirequest.Target
	}{
		{name: "Doc", target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md", Line: 1}},
		{name: "Issue", target: uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-1a2b3c"}},
		{name: "Diff", target: uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "wt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeDocPaneFixture(t, root, "README.md", "# too narrow\n")
			p := interactiveUIRequestTestPlugin(t, root)
			p.width = 40
			beforeTree := clonePaneTree(p.paneRoot)
			beforeGeometry := terminalModelGeometry(p.primaryTermPane().Terminal)
			beforeInteractive := *p.interactiveState
			beforeFocus, beforeNext, beforePane, beforeMode := p.paneFocus, p.paneNextID, p.activePane, p.viewMode

			cmd := p.handleUIRequest(uirequest.Request{
				ID: "interactive-refused-" + tt.name, Action: uirequest.ActionOpen,
				CreatedAt: time.Now().UTC(), TTLMs: 5000,
				Origin: uirequest.Origin{TmuxSession: "test-shell"}, Target: tt.target,
			})
			if cmd != nil {
				t.Fatal("refused split emitted work, including a possible terminal resize")
			}
			if !reflect.DeepEqual(p.paneRoot, beforeTree) {
				t.Fatalf("refused split changed pane tree\n before: %#v\n  after: %#v", beforeTree, p.paneRoot)
			}
			if got := terminalModelGeometry(p.primaryTermPane().Terminal); got != beforeGeometry {
				t.Fatalf("refused split changed terminal geometry/state: before=%+v after=%+v", beforeGeometry, got)
			}
			if p.interactiveState == nil || *p.interactiveState != beforeInteractive {
				t.Fatalf("refused split changed interactive state: before=%+v after=%+v", beforeInteractive, p.interactiveState)
			}
			if p.paneFocus != beforeFocus || p.paneNextID != beforeNext || p.activePane != beforePane || p.viewMode != beforeMode {
				t.Fatalf("refused split changed focus/allocation: focus %d->%d next %d->%d pane %v->%v mode %v->%v",
					beforeFocus, p.paneFocus, beforeNext, p.paneNextID, beforePane, p.activePane, beforeMode, p.viewMode)
			}
		})
	}
}

func interactiveUIRequestTestPlugin(t *testing.T, root string) *Plugin {
	t.Helper()
	p := docPaneTestPlugin(t, root, true)
	p.ctx.ProjectRoot = root
	p.shells[0].Agent.TmuxSession = "test-shell"
	box, ok := p.terminalLeafBox()
	if !ok {
		t.Fatal("test terminal leaf is not placed")
	}
	p.primaryTermPane().Terminal = p.newWorkspaceTerminal(workspaceTerminalPrimary)
	p.primaryTermPane().Terminal.Width = p.terminalContentWidth(box.W)
	p.primaryTermPane().Terminal.Height = box.H - terminalHeaderRows
	// Activate the model without Open: Open would start a real control client,
	// while this contract must never query or resize the developer's tmux server.
	p.primaryTermPane().Terminal.State = &tty.State{
		Active: true, TargetSession: "test-shell", TargetPane: "%901",
		OutputBuf: tty.NewOutputBuffer(20),
	}
	p.primaryTermPane().Target = workspaceTerminalTarget{
		Session: "test-shell", Pane: "%901", Width: p.primaryTermPane().Terminal.Width,
		Height: p.primaryTermPane().Terminal.Height, Source: "shell", SourceID: "test-shell",
	}
	p.viewMode = ViewModeInteractive
	p.interactiveState = &InteractiveState{
		Active: true, TargetPane: "%901", TargetSession: "test-shell", PaneOnEntry: PanePreview,
	}
	return p
}

func assertInteractiveTerminalGeometry(t *testing.T, p *Plugin, leaf Box) {
	t.Helper()
	wantW, wantH := p.terminalContentWidth(leaf.W), leaf.H-terminalHeaderRows
	if p.primaryTermPane().Terminal.Width != wantW || p.primaryTermPane().Terminal.Height != wantH {
		t.Fatalf("live terminal geometry = %dx%d, want leaf viewport %dx%d", p.primaryTermPane().Terminal.Width, p.primaryTermPane().Terminal.Height, wantW, wantH)
	}
}

func armDeferredResize(model *tty.Model) time.Time {
	armedAt := time.Now().Add(-tty.DefaultResizeDebounce + 100*time.Millisecond)
	model.State.LastResizeAt = armedAt
	return armedAt
}

func assertResizeStayedDeferred(t *testing.T, model *tty.Model, armedAt time.Time) {
	t.Helper()
	if !model.State.LastResizeAt.Equal(armedAt) {
		t.Fatalf("resize asserted synchronously instead of deferring: %v -> %v", armedAt, model.State.LastResizeAt)
	}
}

func lastBatchCommand(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("successful open emitted no command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("open command emitted %T, want load plus geometry batch", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("open command scheduled %d children, want exactly one load followed by one geometry assertion", len(batch))
	}
	return batch[len(batch)-1]
}

func assertDeferredGeometryAssertion(t *testing.T, p *Plugin, cmd tea.Cmd, leaf Box) {
	t.Helper()
	messages := terminalMessages(cmd)
	if len(messages) != 1 {
		t.Fatalf("geometry command emitted %d terminal messages, want exactly one", len(messages))
	}
	msg := messages[0]
	if got := reflect.TypeOf(msg).String(); got != "tty.deferredResizeMsg" {
		t.Fatalf("geometry command emitted %s, want tty.deferredResizeMsg", got)
	}
	before := p.primaryTermPane().Terminal.State.LastResizeAt
	assertCmd := p.primaryTermPane().Terminal.Update(msg)
	if assertCmd == nil {
		t.Fatal("the same active terminal rejected its deferred resize assertion")
	}
	if !p.primaryTermPane().Terminal.State.LastResizeAt.After(before) {
		t.Fatalf("accepted assertion did not advance LastResizeAt: %v -> %v", before, p.primaryTermPane().Terminal.State.LastResizeAt)
	}
	assertInteractiveTerminalGeometry(t, p, leaf)
	// Do not execute assertCmd: it is the real tmux query/resize closure. A
	// non-nil result plus the advanced timestamp proves the scoped debt was
	// accepted, while keeping the developer's default tmux server untouched.
}

func terminalMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, terminalMessages(child)...)
		}
		return messages
	}
	if tty.IsTerminalMessage(msg) {
		return []tea.Msg{msg}
	}
	return nil
}

type terminalGeometry struct {
	Width, Height int
	Active        bool
	Target        string
}

func terminalModelGeometry(model *tty.Model) terminalGeometry {
	return terminalGeometry{Width: model.Width, Height: model.Height, Active: model.IsActive(), Target: model.GetTarget()}
}

func TestUIRequests_PendingViewLifecycle(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	p := &Plugin{
		shells: []*ShellSession{
			{TmuxName: "sidecar-sh-sidecar-1", Name: "Shell 1"},
			{TmuxName: "sidecar-sh-sidecar-2", Name: "Shell 2"},
		},
		selectedShellIdx: 0,
		shellSelected:    true,
	}

	// Request for unselected shell: should queue and ack StatusQueued
	req := uirequest.Request{
		ID:        "req-1",
		Action:    uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(),
		TTLMs:     5000,
		Origin: uirequest.Origin{
			TmuxSession: "sidecar-sh-sidecar-2",
		},
		Target: uirequest.Target{
			Kind:  uirequest.TargetKindFile,
			Value: "README.md",
			Line:  10,
		},
	}

	cmd := p.handleUIRequest(req)
	if cmd != nil {
		t.Errorf("handleUIRequest on unselected shell returned non-nil cmd: %v", cmd)
	}

	badge, hasBadge := p.pendingViewBadge("sidecar-sh-sidecar-2")
	if !hasBadge || badge == "" {
		t.Errorf("expected pending view badge on shell 2, got %q, %v", badge, hasBadge)
	}

	// Read acks
	t.Logf("config.StateDir() = %s, stateHome = %s", config.StateDir(), filepath.Join(stateHome, "sidecar"))
	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil {
		t.Fatalf("ReadAcks error: %v", err)
	}
	if len(acks) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(acks))
	}
	if acks[0].Status != uirequest.StatusQueued {
		t.Errorf("expected status %s, got %s", uirequest.StatusQueued, acks[0].Status)
	}

	// Foreign shell: ignore silently
	foreignReq := uirequest.Request{
		ID:     "req-foreign",
		Action: uirequest.ActionOpen,
		Origin: uirequest.Origin{
			TmuxSession: "other-session",
		},
		Target: uirequest.Target{
			Kind:  uirequest.TargetKindFile,
			Value: "foo.go",
		},
	}
	foreignCmd := p.handleUIRequest(foreignReq)
	if foreignCmd != nil {
		t.Errorf("expected nil cmd for foreign shell, got %v", foreignCmd)
	}
	foreignAcks, _ := uirequest.ReadAcks(filepath.Join(stateHome, "sidecar"), foreignReq.ID, foreignReq.Action)
	if len(foreignAcks) > 0 {
		t.Errorf("expected 0 acks for foreign shell, got %d", len(foreignAcks))
	}
}

// An open that puts nothing on screen must be acknowledged as declined. The
// agent's exit code is the only thing telling it whether the user can see the
// file, so a pane that never opened may never be reported as opened.
func TestUIRequests_SelectedShellDeclinesWhenNothingOpens(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	workDir := t.TempDir()
	p := &Plugin{
		ctx: &plugin.Context{WorkDir: workDir},
		shells: []*ShellSession{
			{TmuxName: "sidecar-sh-sidecar-1", Name: "Shell 1", WorkDir: workDir},
		},
		selectedShellIdx: 0,
		shellSelected:    true,
	}

	req := uirequest.Request{
		ID:        "req-decline",
		Action:    uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(),
		TTLMs:     5000,
		Origin:    uirequest.Origin{TmuxSession: "sidecar-sh-sidecar-1"},
		Target:    uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md"},
	}

	// No pane tree: the open cannot land anywhere.
	p.handleUIRequest(req)

	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil {
		t.Fatalf("ReadAcks error: %v", err)
	}
	if len(acks) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(acks))
	}
	if acks[0].Status != uirequest.StatusDeclined {
		t.Errorf("expected status %s, got %s", uirequest.StatusDeclined, acks[0].Status)
	}
}

// Both pane hosts live in one process, so their acks must not share a file
// name — otherwise one host's answer silently overwrites the other's.
func TestUIRequests_ProjectTargetOpensSelectedSurface(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	workDir := t.TempDir()
	p := &Plugin{
		ctx: &plugin.Context{WorkDir: workDir, ProjectRoot: workDir},
		shells: []*ShellSession{
			{TmuxName: "sidecar-sh-sidecar-1", Name: "Shell 1", WorkDir: workDir},
		},
		selectedShellIdx: 0,
		shellSelected:    true,
	}

	req := uirequest.Request{
		ID:        "req-project",
		Action:    uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(),
		TTLMs:     5000,
		Origin:    uirequest.Origin{ProjectKey: "sidecar", WorkDir: workDir},
		Target:    uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md"},
	}
	p.handleUIRequest(req)

	acks, err := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if err != nil {
		t.Fatalf("ReadAcks error: %v", err)
	}
	if len(acks) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(acks))
	}
	if acks[0].Status != uirequest.StatusDeclined {
		t.Errorf("expected status %s, got %s", uirequest.StatusDeclined, acks[0].Status)
	}
}

func TestUIRequests_ProjectTargetOtherKeyIgnored(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	workDir := t.TempDir()
	p := &Plugin{
		ctx: &plugin.Context{WorkDir: workDir, ProjectRoot: workDir},
		shells: []*ShellSession{
			{TmuxName: "sidecar-sh-sidecar-1", Name: "Shell 1", WorkDir: workDir},
		},
		selectedShellIdx: 0,
		shellSelected:    true,
	}

	req := uirequest.Request{
		ID:     "req-other-project",
		Action: uirequest.ActionOpen,
		Origin: uirequest.Origin{ProjectKey: "other", WorkDir: "/not/this/project"},
		Target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md"},
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Errorf("expected nil cmd for other project, got %v", cmd)
	}
	acks, _ := uirequest.ReadAcks(config.StateDir(), req.ID, req.Action)
	if len(acks) > 0 {
		t.Errorf("expected 0 acks for other project, got %d", len(acks))
	}
}

func TestUIRequests_InstanceIDIsPerHost(t *testing.T) {
	if hostInstanceID() == uirequest.InstanceID("overview") {
		t.Errorf("workspace and overview hosts share instance id %q", hostInstanceID())
	}
}

func TestUIRequests_ExpiredPendingView(t *testing.T) {
	p := &Plugin{
		pendingViews: map[string]*pendingView{
			"sh-1": {
				Target:    uirequest.Target{Kind: uirequest.TargetKindFile, Value: "a.txt"},
				CreatedAt: time.Now().Add(-10 * time.Minute),
				TTLMs:     1000,
			},
		},
	}

	if _, has := p.pendingViewBadge("sh-1"); has {
		t.Errorf("expected expired pending view to have no badge")
	}

	cmd := p.consumePendingView("sh-1")
	if cmd != nil {
		t.Errorf("expected nil cmd from expired pending view, got %v", cmd)
	}
}

func TestUIRequests_PendingDiffLastWriteWins(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	p.shells = append(p.shells, &ShellSession{
		TmuxName: "sidecar-sh-sidecar-2", Name: "Shell 2",
		Agent: &Agent{TmuxPane: "%902", OutputBuf: tty.NewOutputBuffer(20)},
	})
	p.selectedShellIdx = 0

	first := uirequest.Request{
		ID: "req-diff-1", Action: uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin: uirequest.Origin{TmuxSession: "sidecar-sh-sidecar-2"},
		Target: uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "wt"},
	}
	second := first
	second.ID = "req-diff-2"
	second.Target.Value = "c:abc1234"

	if cmd := p.handleUIRequest(first); cmd != nil {
		t.Fatal("first queued open returned a cmd")
	}
	if cmd := p.handleUIRequest(second); cmd != nil {
		t.Fatal("second queued open returned a cmd")
	}
	pv := p.pendingViews["sidecar-sh-sidecar-2"]
	if pv == nil || pv.Target.Value != "c:abc1234" {
		t.Fatalf("pending = %+v, want last-write-wins c:abc1234", pv)
	}
	if len(p.pendingViews) != 1 {
		t.Fatalf("pending slots = %d, want one", len(p.pendingViews))
	}

	p.selectedShellIdx = 1
	if cmd := p.consumePendingView("sidecar-sh-sidecar-2"); cmd == nil {
		t.Fatal("consume opened nothing")
	}
	diff, _ := p.activeDiffPane()
	if diff == nil {
		t.Fatal("queued Diff did not open")
	}
	if keys := diffTabKeys(diff); !reflect.DeepEqual(keys, []string{"c:abc1234"}) {
		t.Fatalf("opened tabs = %v, want only the last queued target", keys)
	}
}

func TestUIRequests_OpenRequestEqualsHashClick(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	root := t.TempDir()
	clicked := docPaneTestPlugin(t, root, true)
	if _, ok := clicked.activateDiffLink("abc1234"); !ok {
		t.Fatal("hash click failed")
	}

	opened := docPaneTestPlugin(t, root, true)
	req := uirequest.Request{
		ID: "req-parity", Action: uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin: uirequest.Origin{TmuxSession: "test-shell"},
		Target: uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "c:abc1234"},
	}
	if cmd := opened.handleUIRequest(req); cmd == nil {
		t.Fatal("open request opened nothing")
	}

	if got, want := paneTreeShape(opened.paneRoot), paneTreeShape(clicked.paneRoot); got != want {
		t.Fatalf("request tree %s, click tree %s", got, want)
	}
	clickDiff, _ := clicked.activeDiffPane()
	openDiff, _ := opened.activeDiffPane()
	if clickDiff == nil || openDiff == nil {
		t.Fatal("missing Diff leaf")
	}
	if !reflect.DeepEqual(diffTabKeys(openDiff), diffTabKeys(clickDiff)) {
		t.Fatalf("request tabs = %v, click tabs = %v", diffTabKeys(openDiff), diffTabKeys(clickDiff))
	}
}

// The third open targets the emptiest (primary) column now, so --split below
// stacks the diff under the terminal — the axis override is honored against
// the grid rule's chosen node, and the right column keeps its stack.
func TestUIRequests_SplitBelowAfterFileAndIssueStacksUnderTheTerminal(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	stubTd(t)
	root := t.TempDir()
	writeDocPaneFixture(t, root, "clicked.md", "# clicked\n\nfile body\n")
	p := docPaneTestPlugin(t, root, true)
	p.shells[0].Agent.OutputBuf.Update("wrote clicked.md:1\nfollow-up is td-1a2b3c\n")
	deliverLoads(t, p, clickTerminalLink(t, p, "clicked.md"))
	if cmd := clickTerminalLink(t, p, "td-1a2b3c"); cmd == nil {
		t.Fatal("issue click opened nothing")
	}

	req := uirequest.Request{
		ID: "req-split", Action: uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "test-shell"},
		Target:  uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "wt"},
		Options: uirequest.Options{Split: "below"},
	}
	if cmd := p.handleUIRequest(req); cmd == nil {
		t.Fatal("--split below opened nothing")
	}
	boxes, content := paneLeafBoxes(t, p)
	if boxes[PaneDiff].X != boxes[PaneTerminal].X ||
		boxes[PaneDiff].Y != boxes[PaneTerminal].Y+boxes[PaneTerminal].H+1 {
		t.Fatalf("diff box %#v did not land below the terminal %#v", boxes[PaneDiff], boxes[PaneTerminal])
	}
	if boxes[PaneDoc].X != boxes[PaneIssue].X || boxes[PaneDoc].W != boxes[PaneIssue].W {
		t.Fatalf("--split below disturbed the right column: doc %#v issue %#v", boxes[PaneDoc], boxes[PaneIssue])
	}
	if boxes[PaneTerminal].H+boxes[PaneDiff].H+1 != content.H {
		t.Fatalf("left column spans %d+%d+1, want the full height %d",
			boxes[PaneTerminal].H, boxes[PaneDiff].H, content.H)
	}
	if p.paneRoot.Split == nil || p.paneRoot.Split.A.Kind != PaneTerminal || p.paneRoot.Split.A.Split == nil {
		t.Fatalf("--split below retargeted onto the terminal: %#v", p.paneRoot)
	}
}

func TestUIRequests_SplitIgnoredOnRetarget(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	root := t.TempDir()
	p := docPaneTestPlugin(t, root, true)
	if cmd := p.showDiffCmd(); cmd == nil {
		t.Fatal("first Diff open failed")
	}
	shape := paneTreeShape(p.paneRoot)
	req := uirequest.Request{
		ID: "req-retarget", Action: uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin:  uirequest.Origin{TmuxSession: "test-shell"},
		Target:  uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "c:abc1234"},
		Options: uirequest.Options{Split: "below"},
	}
	if cmd := p.handleUIRequest(req); cmd == nil {
		t.Fatal("retarget open failed")
	}
	if got := paneTreeShape(p.paneRoot); got != shape {
		t.Fatalf("--split on retarget grew the tree: %s -> %s", shape, got)
	}
	diff, _ := p.activeDiffPane()
	if diff == nil || diff.tabs.Find("c:abc1234") < 0 {
		t.Fatalf("retarget did not open the new tab: %v", diffTabKeys(diff))
	}
}

func paneTreeShape(n *PaneNode) string {
	if n == nil {
		return "nil"
	}
	if n.Split != nil {
		axis := "cols"
		if n.Split.Axis == SplitRows {
			axis = "rows"
		}
		return "(" + paneTreeShape(n.Split.A) + " " + axis + " " + paneTreeShape(n.Split.B) + ")"
	}
	switch n.Kind {
	case PaneTerminal:
		return "T"
	case PaneDoc:
		return "D"
	case PaneIssue:
		return "I"
	case PaneDiff:
		return "F"
	default:
		return "?"
	}
}

func TestUIRequests_ForeignLeaseDoesNotApplyOpen(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")

	original := tty.SessionOwner
	t.Cleanup(func() { tty.SessionOwner = original })
	tty.SessionOwner = func(string) string { return "laptop-99" }

	workDir := t.TempDir()
	p := &Plugin{
		ctx: &plugin.Context{WorkDir: workDir},
		shells: []*ShellSession{
			{TmuxName: "sidecar-sh-sidecar-1", Name: "Shell 1", WorkDir: workDir},
		},
		selectedShellIdx: 0,
		shellSelected:    true,
	}
	req := uirequest.Request{
		ID: "req-foreign-lease", Action: uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin: uirequest.Origin{TmuxSession: "sidecar-sh-sidecar-1"},
		Target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md"},
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatalf("host TUI applied an open it does not own: %v", cmd)
	}
	acks, err := uirequest.ReadAcks(filepath.Join(stateHome, "sidecar"), req.ID, req.Action)
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 0 {
		t.Fatalf("foreign-lease open acked: %+v", acks)
	}
}

func TestUIRequests_SessionsOriginIsIgnored(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("SIDECAR_ISOLATED_STATE", "1")
	workDir := t.TempDir()
	p := &Plugin{
		ctx:              &plugin.Context{WorkDir: workDir, ProjectRoot: workDir},
		selectedShellIdx: 0,
		shellSelected:    true,
		shells:           []*ShellSession{{TmuxName: "sidecar-sh-sidecar-1", Name: "Shell 1", WorkDir: workDir}},
	}
	req := uirequest.Request{
		ID: "req-sessions-open", Action: uirequest.ActionOpen,
		CreatedAt: time.Now().UTC(), TTLMs: 5000,
		Origin: uirequest.Origin{Sessions: true, ProjectKey: "sidecar", WorkDir: workDir},
		Target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "README.md"},
	}
	if cmd := p.handleUIRequest(req); cmd != nil {
		t.Fatalf("workspace applied a Sessions open: %v", cmd)
	}
}

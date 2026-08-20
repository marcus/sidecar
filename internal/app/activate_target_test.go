package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/uirequest"
)

func activationModel(workDir string) *Model {
	ui := NewUIState()
	ui.WorkDir = workDir
	ui.ProjectRoot = workDir
	return &Model{ui: ui}
}

// collect flattens a command (including a tea.Batch) into its messages.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, collect(sub)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestActivateFileTargetFocusesFileBrowser(t *testing.T) {
	m := activationModel(t.TempDir())
	msgs := collect(m.activateTarget(ActivateTargetMsg{
		Target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "internal/app/model.go", Line: 7},
	}))
	var focused bool
	var navigated NavigateToFileMsg
	for _, got := range msgs {
		switch typed := got.(type) {
		case FocusPluginByIDMsg:
			if typed.PluginID != "file-browser" {
				t.Fatalf("focused %q", typed.PluginID)
			}
			focused = true
		case NavigateToFileMsg:
			navigated = typed
		}
	}
	if !focused {
		t.Fatal("expected the file browser to be focused")
	}
	if navigated.Path != "internal/app/model.go" || navigated.Line != 7 {
		t.Fatalf("unexpected navigation %+v", navigated)
	}
}

func TestActivateMalformedTargetIsRefusedOutLoud(t *testing.T) {
	m := activationModel(t.TempDir())
	msgs := collect(m.activateTarget(ActivateTargetMsg{
		Target: uirequest.Target{Kind: uirequest.TargetKindFile, Value: "/etc/passwd"},
	}))
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	if _, ok := msgs[0].(notify.PostMsg); !ok {
		t.Fatalf("expected a notification, got %T", msgs[0])
	}
}

func TestTargetProjectIsCurrentAcceptsPathAndBaseName(t *testing.T) {
	dir := t.TempDir()
	m := activationModel(dir)
	for _, project := range []string{"", dir, baseName(dir)} {
		if !m.targetProjectIsCurrent(project) {
			t.Fatalf("expected %q to be the current project", project)
		}
	}
	if m.targetProjectIsCurrent("nowhere") {
		t.Fatal("expected an unrelated project to be rejected")
	}
}

// TestPathQualifierForMainRepoIsNotCurrentInAWorktree: a relative file target
// only means something in the checkout it was scanned against. A jump naming
// the main repo by path, made while the user sits in a linked worktree, must be
// a real switch — not "already here", which would open the worktree's copy of a
// file on a different branch.
func TestPathQualifierForMainRepoIsNotCurrentInAWorktree(t *testing.T) {
	main := t.TempDir()
	worktree := t.TempDir()
	m := activationModel(worktree)
	m.ui.ProjectRoot = main

	if m.targetProjectIsCurrent(main) {
		t.Fatal("a path naming the main repo is not the worktree the user is in")
	}
	if !m.targetProjectIsCurrent(worktree) {
		t.Fatal("the checkout the user is in is current")
	}
	// A qualifier given as a *name* still names the project, where either
	// checkout is a legitimate landing.
	if !m.targetProjectIsCurrent(baseName(main)) {
		t.Fatal("a project name should still match the project root")
	}
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// TestActivatePaneTargetsSendPublicMessages proves the seam: issue, diff,
// resource and session activation leave the shell as public messages any plugin
// or surface can send and receive, not as calls into a plugin's private methods.
func TestActivatePaneTargetsSendPublicMessages(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name   string
		target uirequest.Target
		check  func(t *testing.T, msgs []tea.Msg)
	}{
		{
			name:   "issue",
			target: uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-331dbf19"},
			check: func(t *testing.T, msgs []tea.Msg) {
				for _, got := range msgs {
					if typed, ok := got.(OpenIssuePaneMsg); ok {
						if typed.Issue != "td-331dbf19" {
							t.Fatalf("issue = %q", typed.Issue)
						}
						return
					}
				}
				t.Fatal("no OpenIssuePaneMsg")
			},
		},
		{
			name:   "diff",
			target: uirequest.Target{Kind: uirequest.TargetKindDiff, Value: "HEAD~1"},
			check: func(t *testing.T, msgs []tea.Msg) {
				for _, got := range msgs {
					if typed, ok := got.(OpenDiffPaneMsg); ok {
						if typed.Spec != "HEAD~1" {
							t.Fatalf("spec = %q", typed.Spec)
						}
						return
					}
				}
				t.Fatal("no OpenDiffPaneMsg")
			},
		},
		{
			name: "resource",
			target: uirequest.Target{
				Kind: uirequest.TargetKindResource, Value: "CASH-1245",
				Provider: "jira", Matcher: "issue-key",
			},
			check: func(t *testing.T, msgs []tea.Msg) {
				for _, got := range msgs {
					if typed, ok := got.(OpenResourcePaneMsg); ok {
						if typed.Provider != "jira" || typed.Matcher != "issue-key" || typed.Locator != "CASH-1245" {
							t.Fatalf("resource = %+v", typed)
						}
						return
					}
				}
				t.Fatal("no OpenResourcePaneMsg")
			},
		},
		{
			name:   "session",
			target: uirequest.Target{Kind: uirequest.TargetKindSession, Value: "sidecar-main"},
			check: func(t *testing.T, msgs []tea.Msg) {
				for _, got := range msgs {
					if typed, ok := got.(AttachSessionMsg); ok {
						if typed.Session != "sidecar-main" {
							t.Fatalf("session = %q", typed.Session)
						}
						return
					}
				}
				t.Fatal("no AttachSessionMsg")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := activationModel(dir)
			msgs := collect(m.activateTarget(ActivateTargetMsg{Target: tc.target}))
			focused := false
			for _, got := range msgs {
				if typed, ok := got.(FocusPluginByIDMsg); ok && typed.PluginID == "workspace-manager" {
					focused = true
				}
			}
			if !focused {
				t.Fatal("expected the workspace plugin to be focused")
			}
			tc.check(t, msgs)
		})
	}
}

// A task target focuses the Tasks tab. Landing on the task itself is not
// available — the embedded Tasks model exposes no select-by-id entry — so the
// tab is what this route promises and what it must actually do.
func TestActivateTaskTargetFocusesTasks(t *testing.T) {
	m := activationModel(t.TempDir())
	msgs := collect(m.activateTarget(ActivateTargetMsg{
		Target: uirequest.Target{Kind: uirequest.TargetKindTask, Value: "a1b2c3d4"},
	}))
	for _, got := range msgs {
		if typed, ok := got.(FocusPluginByIDMsg); ok && typed.PluginID == "tasks" {
			return
		}
	}
	t.Fatalf("task target did not focus the Tasks tab: %+v", msgs)
}

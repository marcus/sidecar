package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestGetRepoNameBounds_EmptyRepoName(t *testing.T) {
	m := Model{
		intro: IntroModel{
			RepoName: "",
		},
	}

	start, end, ok := m.getRepoNameBounds()
	if ok {
		t.Errorf("getRepoNameBounds() with empty repo name should return ok=false, got ok=true, start=%d, end=%d", start, end)
	}
}

func TestGetRepoNameBounds_NormalRepoName(t *testing.T) {
	m := Model{
		intro: IntroModel{
			RepoName: "sidecar",
		},
	}

	start, end, ok := m.getRepoNameBounds()
	if !ok {
		t.Fatal("getRepoNameBounds() with normal repo name should return ok=true")
	}

	// Start should be after " Sidecar" and " / "
	// End should be after the repo name
	if start <= 0 {
		t.Errorf("start should be > 0, got %d", start)
	}
	if end <= start {
		t.Errorf("end should be > start, got start=%d, end=%d", start, end)
	}

	// The width should roughly match the repo name length
	width := end - start
	if width < len("sidecar") {
		t.Errorf("bounds width (%d) should be >= repo name length (%d)", width, len("sidecar"))
	}
}

func TestGetRepoNameBounds_LongRepoName(t *testing.T) {
	longName := "this-is-a-very-long-repository-name-that-might-cause-issues"
	m := Model{
		intro: IntroModel{
			RepoName: longName,
		},
	}

	start, end, ok := m.getRepoNameBounds()
	if !ok {
		t.Fatal("getRepoNameBounds() with long repo name should return ok=true")
	}

	if start <= 0 {
		t.Errorf("start should be > 0, got %d", start)
	}
	if end <= start {
		t.Errorf("end should be > start, got start=%d, end=%d", start, end)
	}

	// The width should roughly match the long repo name
	width := end - start
	if width < len(longName) {
		t.Errorf("bounds width (%d) should be >= repo name length (%d)", width, len(longName))
	}
}

func TestRepoNameClick_OpensProjectSwitcher(t *testing.T) {
	cfg := &config.Config{
		Projects: config.ProjectsConfig{
			List: []config.ProjectConfig{},
		},
	}
	m := Model{
		intro: IntroModel{
			RepoName: "testrepo",
			Active:   false, // Animation complete
			Done:     true,
		},
		cfg:    cfg,
		ui:     &UIState{},
		width:  120,
		height: 40,
		ready:  true,
	}

	// Get the bounds for the repo name
	start, end, ok := m.getRepoNameBounds()
	if !ok {
		t.Fatal("getRepoNameBounds() should return ok=true")
	}

	// Click in the middle of the repo name area
	clickX := (start + end) / 2
	msg := tea.MouseMsg{
		X:      clickX,
		Y:      0, // Header is at Y=0
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}

	// Process the mouse message
	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	if !updated.showProjectSwitcher {
		t.Errorf("clicking repo name at X=%d (bounds %d-%d) should open project switcher", clickX, start, end)
	}
	if updated.activeContext != "project-switcher" {
		t.Errorf("activeContext should be 'project-switcher', got %q", updated.activeContext)
	}
}

func TestRepoNameClick_BlockedDuringIntro(t *testing.T) {
	cfg := &config.Config{
		Projects: config.ProjectsConfig{
			List: []config.ProjectConfig{},
		},
	}
	m := Model{
		intro: IntroModel{
			RepoName: "testrepo",
			Active:   true, // Animation still running
			Done:     false,
		},
		cfg:      cfg,
		ui:       &UIState{},
		registry: plugin.NewRegistry(nil),
		width:    120,
		height:   40,
		ready:    true,
	}

	start, end, ok := m.getRepoNameBounds()
	if !ok {
		t.Fatal("getRepoNameBounds() should return ok=true")
	}

	clickX := (start + end) / 2
	msg := tea.MouseMsg{
		X:      clickX,
		Y:      0,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}

	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	if updated.showProjectSwitcher {
		t.Error("clicking repo name during intro animation should NOT open project switcher")
	}
}

func TestRepoNameClick_OutsideBounds(t *testing.T) {
	cfg := &config.Config{
		Projects: config.ProjectsConfig{
			List: []config.ProjectConfig{},
		},
	}
	m := Model{
		intro: IntroModel{
			RepoName: "testrepo",
			Active:   false,
			Done:     true,
		},
		cfg:      cfg,
		ui:       &UIState{},
		registry: plugin.NewRegistry(nil),
		width:    120,
		height:   40,
		ready:    true,
	}

	start, _, ok := m.getRepoNameBounds()
	if !ok {
		t.Fatal("getRepoNameBounds() should return ok=true")
	}

	// Click before the repo name area (in the "Sidecar" text)
	msg := tea.MouseMsg{
		X:      start - 5,
		Y:      0,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}

	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	if updated.showProjectSwitcher {
		t.Error("clicking outside repo name bounds should NOT open project switcher")
	}
}

func TestIntroActive_SetFalseAfterCompletion(t *testing.T) {
	m := Model{
		intro: IntroModel{
			RepoName:    "testrepo",
			Active:      true,
			Done:        true,
			RepoOpacity: 1.0, // Fully faded in
		},
	}

	// Process IntroTickMsg when animation is complete
	msg := IntroTickMsg{}
	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	if updated.intro.Active {
		t.Error("intro.Active should be false after animation completes")
	}
}

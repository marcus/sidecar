package config

import (
	"path/filepath"
	"testing"
	"time"
)

// AddProject is the one boundary every surface registers a project through, so
// it is where the registration date is stamped. The date means registration and
// nothing else: no existing entry is backfilled, and a caller that already has
// a trustworthy value keeps it.
func TestAddProjectStampsARegistrationDateWithoutBackfillingExistingOnes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	existing := ProjectConfig{Name: "old", Path: t.TempDir()}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Projects.List = []ProjectConfig{existing}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Add(-time.Second)
	added, err := AddProject(ProjectConfig{Name: "fresh", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if added.AddedAt == nil {
		t.Fatal("a newly registered project must carry its registration date")
	}
	if added.AddedAt.Before(before) {
		t.Fatalf("registration date %v predates the call", added.AddedAt)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var old, fresh *ProjectConfig
	for i := range reloaded.Projects.List {
		switch reloaded.Projects.List[i].Name {
		case "old":
			old = &reloaded.Projects.List[i]
		case "fresh":
			fresh = &reloaded.Projects.List[i]
		}
	}
	if old == nil || fresh == nil {
		t.Fatalf("projects after add: %#v", reloaded.Projects.List)
	}
	if old.AddedAt != nil {
		t.Fatal("an already-registered project must stay honestly unknown, not be backfilled")
	}
	if fresh.AddedAt == nil || !fresh.AddedAt.Equal(*added.AddedAt) {
		t.Fatal("the registration date did not survive the round trip through the file")
	}

	// A caller with its own trustworthy value keeps it.
	supplied := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	imported, err := AddProject(ProjectConfig{Name: "imported", Path: t.TempDir(), AddedAt: &supplied})
	if err != nil {
		t.Fatal(err)
	}
	if !imported.AddedAt.Equal(supplied) {
		t.Fatalf("supplied registration date rewritten to %v", imported.AddedAt)
	}
}

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJJWorkspaceList(t *testing.T) {
	got := parseJJWorkspaceList("default\nfeature-a\n\nfeature-a\n")
	want := []string{"default", "feature-a"}

	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("ref[%d] = %q, want %q", i, got[i].Name, want[i])
		}
	}
}

func TestJJRepoPath(t *testing.T) {
	parent := t.TempDir()
	mainRoot := filepath.Join(parent, "repo")
	linkedRoot := filepath.Join(parent, "repo-feature")

	mustMkdir(t, filepath.Join(mainRoot, ".jj", "repo"))
	mustMkdir(t, filepath.Join(linkedRoot, ".jj"))
	if err := os.WriteFile(filepath.Join(linkedRoot, ".jj", "repo"), []byte(filepath.Join(mainRoot, ".jj", "repo")), 0644); err != nil {
		t.Fatal(err)
	}

	mainRepo, err := jjRepoPath(mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	linkedRepo, err := jjRepoPath(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(mainRoot, ".jj", "repo")
	if mainRepo != want {
		t.Fatalf("main repo path = %q, want %q", mainRepo, want)
	}
	if linkedRepo != want {
		t.Fatalf("linked repo path = %q, want %q", linkedRepo, want)
	}
}

func TestFindJJWorkspacePaths(t *testing.T) {
	parent := t.TempDir()
	mainRoot := filepath.Join(parent, "repo")
	featureRoot := filepath.Join(parent, "repo-feature")
	otherRoot := filepath.Join(parent, "other")
	repoPath := filepath.Join(mainRoot, ".jj", "repo")

	createFakeJJWorkspace(t, mainRoot, repoPath, "default", true)
	createFakeJJWorkspace(t, featureRoot, repoPath, "repo-feature", false)
	createFakeJJWorkspace(t, otherRoot, filepath.Join(otherRoot, ".jj", "repo"), "other", true)

	refs := []jjWorkspaceRef{{Name: "default"}, {Name: "repo-feature"}}
	got := findJJWorkspacePaths(mainRoot, repoPath, refs)

	if got["default"] != mainRoot {
		t.Fatalf("default path = %q, want %q", got["default"], mainRoot)
	}
	if got["repo-feature"] != featureRoot {
		t.Fatalf("repo-feature path = %q, want %q", got["repo-feature"], featureRoot)
	}
	if _, ok := got["other"]; ok {
		t.Fatalf("unexpected unrelated workspace path: %q", got["other"])
	}
}

func createFakeJJWorkspace(t *testing.T, root, repoPath, name string, colocated bool) {
	t.Helper()
	mustMkdir(t, filepath.Join(root, ".jj", "working_copy"))
	if colocated {
		mustMkdir(t, filepath.Join(root, ".jj", "repo"))
	} else if err := os.WriteFile(filepath.Join(root, ".jj", "repo"), []byte(repoPath), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".jj", "working_copy", "checkout"), []byte("\x00"+name+"\x00"), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

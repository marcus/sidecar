package tdroot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTDRoot_NoFile(t *testing.T) {
	// Create temp directory without .td-root
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	result := ResolveTDRoot(tmpDir)
	if result != tmpDir {
		t.Errorf("expected %q, got %q", tmpDir, result)
	}
}

func TestResolveTDRoot_ValidFile(t *testing.T) {
	// Create temp directory with .td-root pointing to another path
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	targetRoot := "/path/to/main/repo"
	tdRootPath := filepath.Join(tmpDir, TDRootFile)
	if err := os.WriteFile(tdRootPath, []byte(targetRoot+"\n"), 0644); err != nil {
		t.Fatalf("failed to write .td-root: %v", err)
	}

	result := ResolveTDRoot(tmpDir)
	if result != targetRoot {
		t.Errorf("expected %q, got %q", targetRoot, result)
	}
}

func TestResolveTDRoot_EmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write empty .td-root file
	tdRootPath := filepath.Join(tmpDir, TDRootFile)
	if err := os.WriteFile(tdRootPath, []byte("  \n"), 0644); err != nil {
		t.Fatalf("failed to write .td-root: %v", err)
	}

	result := ResolveTDRoot(tmpDir)
	if result != tmpDir {
		t.Errorf("expected %q (fallback to workDir), got %q", tmpDir, result)
	}
}

func TestResolveTDRoot_WhitespaceHandling(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	targetRoot := "/path/to/main/repo"
	tdRootPath := filepath.Join(tmpDir, TDRootFile)
	// Write with extra whitespace and newlines
	if err := os.WriteFile(tdRootPath, []byte("  "+targetRoot+"  \n\n"), 0644); err != nil {
		t.Fatalf("failed to write .td-root: %v", err)
	}

	result := ResolveTDRoot(tmpDir)
	if result != targetRoot {
		t.Errorf("expected %q, got %q", targetRoot, result)
	}
}

func TestResolveDBPath_NoTDRoot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	expected := filepath.Join(tmpDir, TodosDir, DBFile)
	result := ResolveDBPath(tmpDir)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestResolveDBPath_WithTDRoot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	targetRoot := "/path/to/main/repo"
	tdRootPath := filepath.Join(tmpDir, TDRootFile)
	if err := os.WriteFile(tdRootPath, []byte(targetRoot+"\n"), 0644); err != nil {
		t.Fatalf("failed to write .td-root: %v", err)
	}

	expected := filepath.Join(targetRoot, TodosDir, DBFile)
	result := ResolveDBPath(tmpDir)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestCreateTDRoot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	targetRoot := "/path/to/main/repo"
	if err := CreateTDRoot(tmpDir, targetRoot); err != nil {
		t.Fatalf("CreateTDRoot failed: %v", err)
	}

	// Verify file was created with correct content
	tdRootPath := filepath.Join(tmpDir, TDRootFile)
	data, err := os.ReadFile(tdRootPath)
	if err != nil {
		t.Fatalf("failed to read .td-root: %v", err)
	}

	expected := targetRoot + "\n"
	if string(data) != expected {
		t.Errorf("expected content %q, got %q", expected, string(data))
	}
}

func TestCreateTDRoot_Overwrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create initial file
	if err := CreateTDRoot(tmpDir, "/old/path"); err != nil {
		t.Fatalf("first CreateTDRoot failed: %v", err)
	}

	// Overwrite with new path
	newTarget := "/new/path/to/repo"
	if err := CreateTDRoot(tmpDir, newTarget); err != nil {
		t.Fatalf("second CreateTDRoot failed: %v", err)
	}

	// Verify new content
	result := ResolveTDRoot(tmpDir)
	if result != newTarget {
		t.Errorf("expected %q, got %q", newTarget, result)
	}
}

func TestCheckTodosConflict_NoTodosPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-conflict-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// No .todos at all — no conflict
	if err := CheckTodosConflict(tmpDir); err != nil {
		t.Errorf("expected nil error when .todos doesn't exist, got: %v", err)
	}
}

func TestCheckTodosConflict_TodosIsDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-conflict-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// .todos is a directory — no conflict
	if err := os.MkdirAll(filepath.Join(tmpDir, TodosDir), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := CheckTodosConflict(tmpDir); err != nil {
		t.Errorf("expected nil error when .todos is a directory, got: %v", err)
	}
}

func TestCheckTodosConflict_TodosIsFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-conflict-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// .todos is a regular file — conflict!
	todosPath := filepath.Join(tmpDir, TodosDir)
	if err := os.WriteFile(todosPath, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err = CheckTodosConflict(tmpDir)
	if err == nil {
		t.Fatal("expected error when .todos is a file, got nil")
	}
	if err != ErrTodosIsFile {
		t.Errorf("expected ErrTodosIsFile, got: %v", err)
	}
}

func TestCheckTodosConflict_TodosIsSymlink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tdroot-conflict-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// .todos is a symlink to a file — conflict (Lstat sees the symlink, not target)
	targetFile := filepath.Join(tmpDir, "target")
	if err := os.WriteFile(targetFile, []byte("data"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	todosPath := filepath.Join(tmpDir, TodosDir)
	if err := os.Symlink(targetFile, todosPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err = CheckTodosConflict(tmpDir)
	if err == nil {
		t.Fatal("expected error when .todos is a symlink to a file, got nil")
	}
}

// --- helpers for worktree tests ---

// initGitRepo creates a temp dir with a git repo containing one empty commit.
// Returns the repo path. Cleanup is handled by t.Cleanup.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tdroot-wt-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	runGit(t, dir, "init")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
	return dir
}

// runGit runs a git command in the given dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// assertSamePath compares two paths after resolving symlinks (handles macOS /private/tmp).
func assertSamePath(t *testing.T, want, got string) {
	t.Helper()
	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", want, err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	if wantResolved != gotResolved {
		t.Errorf("paths differ:\n  want: %s\n  got:  %s", wantResolved, gotResolved)
	}
}

// --- worktree tests ---

func TestResolveTDRoot_ExternalWorktreeFindsMainTodos(t *testing.T) {
	mainRepo := initGitRepo(t)

	// Create .todos dir in main repo
	if err := os.MkdirAll(filepath.Join(mainRepo, TodosDir), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	// Create linked worktree
	wtPath := filepath.Join(filepath.Dir(mainRepo), "wt-find-todos")
	runGit(t, mainRepo, "worktree", "add", wtPath, "-b", "test-branch")
	t.Cleanup(func() { _ = os.RemoveAll(wtPath) })

	result := ResolveTDRoot(wtPath)
	assertSamePath(t, mainRepo, result)
}

func TestResolveTDRoot_ExternalWorktreeFollowsMainTdRoot(t *testing.T) {
	mainRepo := initGitRepo(t)

	// Create a shared root dir and write .td-root in main repo pointing to it
	sharedRoot, err := os.MkdirTemp("", "tdroot-shared-*")
	if err != nil {
		t.Fatalf("MkdirTemp shared: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sharedRoot) })

	if err := CreateTDRoot(mainRepo, sharedRoot); err != nil {
		t.Fatalf("CreateTDRoot: %v", err)
	}

	// Create linked worktree
	wtPath := filepath.Join(filepath.Dir(mainRepo), "wt-follow-tdroot")
	runGit(t, mainRepo, "worktree", "add", wtPath, "-b", "test-branch")
	t.Cleanup(func() { _ = os.RemoveAll(wtPath) })

	result := ResolveTDRoot(wtPath)
	assertSamePath(t, sharedRoot, result)
}

func TestResolveDBPath_ExternalWorktree(t *testing.T) {
	mainRepo := initGitRepo(t)

	// Create .todos dir in main repo
	if err := os.MkdirAll(filepath.Join(mainRepo, TodosDir), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	// Create linked worktree
	wtPath := filepath.Join(filepath.Dir(mainRepo), "wt-dbpath")
	runGit(t, mainRepo, "worktree", "add", wtPath, "-b", "test-branch")
	t.Cleanup(func() { _ = os.RemoveAll(wtPath) })

	result := ResolveDBPath(wtPath)

	// Resolve mainRepo through symlinks for comparison (macOS /tmp -> /private/tmp)
	mainRepoResolved, err := filepath.EvalSymlinks(mainRepo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	expected := filepath.Join(mainRepoResolved, TodosDir, DBFile)

	// The DB file doesn't exist, so resolve just the repo root portion of the result
	gotRepoRoot := filepath.Dir(filepath.Dir(result))
	gotRepoRootResolved, err := filepath.EvalSymlinks(gotRepoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", gotRepoRoot, err)
	}
	got := filepath.Join(gotRepoRootResolved, TodosDir, DBFile)
	if expected != got {
		t.Errorf("paths differ:\n  want: %s\n  got:  %s", expected, got)
	}
}

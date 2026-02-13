package trifectaindex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndValidate_MissingFile(t *testing.T) {
	_, err := LoadAndValidate("/tmp/does-not-exist.json", "/tmp")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	var loadErr *LoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected LoadError, got %T", err)
	}
	if loadErr.Code != LoadErrorIndexMissing {
		t.Fatalf("expected %s, got %s", LoadErrorIndexMissing, loadErr.Code)
	}
}

func TestLoadAndValidate_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wo_worktrees.json")
	if err := os.WriteFile(path, []byte("{bad-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAndValidate(path, dir)
	if err == nil {
		t.Fatalf("expected invalid json error")
	}
	var loadErr *LoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected LoadError, got %T", err)
	}
	if loadErr.Code != LoadErrorInvalidJSON {
		t.Fatalf("expected %s, got %s", LoadErrorInvalidJSON, loadErr.Code)
	}
}

func TestLoadAndValidate_ValidIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wo_worktrees.json")
	content := `{
  "version": 1,
  "schema": "trifecta.sidecar.wo_index.v1",
  "generated_at": "2026-02-12T10:00:00Z",
  "repo_root": "` + dir + `",
  "git_head_sha_repo_root": "abc",
  "work_orders": [
    {
      "id": "WO-1",
      "title": "t",
      "status": "running",
      "priority": "P1",
      "owner": "",
      "epic_id": "",
      "worktree_path": "../.worktrees/WO-1",
      "worktree_exists": true,
      "branch": "feat/wo-WO-1",
      "wo_yaml_path": "_ctx/jobs/running/WO-1.yaml",
      "created_at": ""
    }
  ],
  "errors": []
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadAndValidate(path, dir)
	if err != nil {
		t.Fatalf("expected valid index, got error: %v", err)
	}
	if len(idx.WorkOrders) != 1 {
		t.Fatalf("expected 1 work order, got %d", len(idx.WorkOrders))
	}
}

func TestLoadAndValidate_AcceptsLegacySpaceSeparatedTimestamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wo_worktrees.json")
	content := `{
  "version": 1,
  "schema": "trifecta.sidecar.wo_index.v1",
  "generated_at": "2026-02-10 14:28:30+00:00",
  "repo_root": "` + dir + `",
  "git_head_sha_repo_root": "abc",
  "work_orders": [
    {
      "id": "WO-1",
      "title": "t",
      "status": "running",
      "priority": "P1",
      "owner": "",
      "epic_id": "",
      "worktree_path": "../.worktrees/WO-1",
      "worktree_exists": true,
      "branch": "feat/wo-WO-1",
      "wo_yaml_path": "_ctx/jobs/running/WO-1.yaml",
      "created_at": "2026-02-10 14:28:30+00:00"
    }
  ],
  "errors": []
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAndValidate(path, dir); err != nil {
		t.Fatalf("expected legacy timestamp format to be accepted, got error: %v", err)
	}
}

func TestLoadAndValidate_RepoMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wo_worktrees.json")
	content := `{
  "version": 1,
  "schema": "trifecta.sidecar.wo_index.v1",
  "generated_at": "2026-02-12T10:00:00Z",
  "repo_root": "/tmp/another-repo",
  "git_head_sha_repo_root": "abc",
  "work_orders": [],
  "errors": []
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAndValidate(path, dir)
	if err == nil {
		t.Fatalf("expected repo mismatch error")
	}
	var loadErr *LoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected LoadError, got %T", err)
	}
	if loadErr.Code != LoadErrorRepoMismatch {
		t.Fatalf("expected %s, got %s", LoadErrorRepoMismatch, loadErr.Code)
	}
}

func TestLoadAndValidate_RepoRootCanonicalSymlinkMatch(t *testing.T) {
	dir := t.TempDir()
	realRepo := filepath.Join(dir, "real-repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatal(err)
	}

	linkRepo := filepath.Join(dir, "repo-link")
	if err := os.Symlink(realRepo, linkRepo); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	indexPath := filepath.Join(realRepo, "wo_worktrees.json")
	content := `{
  "version": 1,
  "schema": "trifecta.sidecar.wo_index.v1",
  "generated_at": "2026-02-12T10:00:00Z",
  "repo_root": "` + realRepo + `",
  "git_head_sha_repo_root": "abc",
  "work_orders": [],
  "errors": []
}`
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAndValidate(indexPath, linkRepo)
	if err != nil {
		t.Fatalf("expected canonical symlink match, got error: %v", err)
	}
}

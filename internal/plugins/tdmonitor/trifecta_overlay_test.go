package tdmonitor

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/trifectaindex"
)

func TestWOOverlayIndexMissing(t *testing.T) {
	p := newOverlayPlugin(t.TempDir())
	p.refreshWOOverlay()
	if p.woStateCode == "" {
		t.Fatalf("expected missing index state code")
	}
	if got := p.renderWOOverlay(120); !strings.Contains(got, "INDEX_MISSING") {
		t.Fatalf("expected INDEX_MISSING in overlay, got %q", got)
	}
}

func TestWOOverlayInvalidJSON(t *testing.T) {
	root := t.TempDir()
	writeIndexFile(t, root, "{bad-json")

	p := newOverlayPlugin(root)
	p.refreshWOOverlay()
	if p.woStateCode != "INDEX_INVALID_JSON" {
		t.Fatalf("expected INDEX_INVALID_JSON, got %s", p.woStateCode)
	}
}

func TestWOOverlaySchemaUnsupported(t *testing.T) {
	root := t.TempDir()
	writeIndexFile(t, root, `{"version":1,"schema":"wrong","generated_at":"2026-02-12T10:00:00Z","repo_root":"`+root+`","git_head_sha_repo_root":"abc","work_orders":[],"errors":[]}`)

	p := newOverlayPlugin(root)
	p.refreshWOOverlay()
	if p.woStateCode != "INDEX_SCHEMA_UNSUPPORTED" {
		t.Fatalf("expected INDEX_SCHEMA_UNSUPPORTED, got %s", p.woStateCode)
	}
}

func TestWOOverlayRepoMismatch(t *testing.T) {
	root := t.TempDir()
	writeIndexFile(t, root, `{"version":1,"schema":"trifecta.sidecar.wo_index.v1","generated_at":"2026-02-12T10:00:00Z","repo_root":"/tmp/another-repo","git_head_sha_repo_root":"abc","work_orders":[],"errors":[]}`)

	p := newOverlayPlugin(root)
	p.refreshWOOverlay()
	if p.woStateCode != "INDEX_REPO_MISMATCH" {
		t.Fatalf("expected INDEX_REPO_MISMATCH, got %s", p.woStateCode)
	}
}

func TestWOOverlaySingleAndMultipleRunning(t *testing.T) {
	root := t.TempDir()
	writeIndexFile(t, root, `{
  "version": 1,
  "schema": "trifecta.sidecar.wo_index.v1",
  "generated_at": "2026-02-12T10:00:00Z",
  "repo_root": "`+root+`",
  "git_head_sha_repo_root": "abc",
  "work_orders": [
    {"id":"WO-001","title":"A","status":"running","priority":"P1","owner":"u1","epic_id":"","worktree_path":"../.worktrees/WO-001","worktree_exists":true,"branch":"","wo_yaml_path":"_ctx/jobs/running/WO-001.yaml","created_at":"2026-02-12T09:00:00Z"},
    {"id":"WO-002","title":"B","status":"running","priority":"P2","owner":"u2","epic_id":"","worktree_path":"../.worktrees/WO-002","worktree_exists":true,"branch":"","wo_yaml_path":"_ctx/jobs/running/WO-002.yaml","created_at":"2026-02-12T08:00:00Z"}
  ],
  "errors": []
}`)

	p := newOverlayPlugin(root)
	p.refreshWOOverlay()
	if p.woActive == nil {
		t.Fatalf("expected active WO")
	}
	got := p.renderWOOverlay(120)
	if !strings.Contains(got, "WO [WO-001]") {
		t.Fatalf("expected WO-001 active in overlay, got %q", got)
	}
	if !strings.Contains(got, "(+1)") {
		t.Fatalf("expected +1 in overlay, got %q", got)
	}
}

func TestWOOverlayPollMtimeBehavior(t *testing.T) {
	root := t.TempDir()
	indexPath := writeIndexFile(t, root, `{"version":1,"schema":"trifecta.sidecar.wo_index.v1","generated_at":"2026-02-12T10:00:00Z","repo_root":"`+root+`","git_head_sha_repo_root":"abc","work_orders":[{"id":"WO-001","title":"A","status":"running","priority":"P1","owner":"","epic_id":"","worktree_path":"../.worktrees/WO-001","worktree_exists":true,"branch":"","wo_yaml_path":"_ctx/jobs/running/WO-001.yaml","created_at":"2026-02-12T09:00:00Z"}],"errors":[]}`)

	p := newOverlayPlugin(root)
	p.refreshWOOverlay()
	firstMtime := p.woIndexMTime
	firstID := p.woActive.ID

	// same mtime => cached result reused
	p.refreshWOOverlay()
	if !p.woIndexMTime.Equal(firstMtime) {
		t.Fatalf("expected mtime unchanged")
	}
	if p.woActive.ID != firstID {
		t.Fatalf("expected same active WO when mtime unchanged")
	}

	// update file and mtime => reload must happen
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(indexPath, []byte(`{"version":1,"schema":"trifecta.sidecar.wo_index.v1","generated_at":"2026-02-12T10:00:10Z","repo_root":"`+root+`","git_head_sha_repo_root":"abc","work_orders":[{"id":"WO-009","title":"Z","status":"running","priority":"P0","owner":"","epic_id":"","worktree_path":"../.worktrees/WO-009","worktree_exists":true,"branch":"","wo_yaml_path":"_ctx/jobs/running/WO-009.yaml","created_at":"2026-02-12T10:00:10Z"}],"errors":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p.refreshWOOverlay()
	if p.woActive == nil || p.woActive.ID != "WO-009" {
		t.Fatalf("expected active WO to reload as WO-009, got %+v", p.woActive)
	}
}

func TestInjectWOInCurrentWorkReplacesNoCurrentWork(t *testing.T) {
	p := &Plugin{
		woActive: &trifectaindex.WorkOrder{
			ID:           "WO-0010",
			Status:       "running",
			Priority:     "P2",
			Owner:        "felipe_gonzalez",
			WorktreePath: "../.worktrees/WO-0010",
		},
		woExtra: 0,
	}

	input := "CURRENT WORK\nNo current work\n\nBOARD: All Issues"
	got := p.injectWOInCurrentWork(input)

	if strings.Contains(got, "No current work") {
		t.Fatalf("expected 'No current work' to be replaced, got %q", got)
	}
	if !strings.Contains(got, "WO [WO-0010]") {
		t.Fatalf("expected WO line inside CURRENT WORK, got %q", got)
	}
}

func newOverlayPlugin(root string) *Plugin {
	return &Plugin{
		ctx: &plugin.Context{
			WorkDir: root,
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		},
	}
}

func writeIndexFile(t *testing.T, root, content string) string {
	t.Helper()
	indexDir := filepath.Join(root, "_ctx", "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(indexDir, "wo_worktrees.json")
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

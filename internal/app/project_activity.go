package app

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/state"
)

// Project activity is what the switcher's "Last activity" column reports. It is
// a record of events Sidecar observed — today, binding a project — and never a
// measurement of the directory: a git commit time or a file mtime would be a
// different fact wearing this one's label, and a project Sidecar has never
// opened honestly reads Unknown.
//
// Recording happens in a command rather than inline because it writes state.json,
// and nothing that touches the filesystem belongs on the path to a frame.

// recordProjectActivityCmd stamps a configured project as active now. The path
// may be any location inside the project; it is resolved back to the configured
// entry so a worktree and its main checkout record against one project rather
// than two.
func (m *Model) recordProjectActivityCmd(path string) tea.Cmd {
	key := m.configuredProjectPath(path)
	if key == "" {
		return nil
	}
	when := time.Now().UTC()
	return func() tea.Msg {
		_ = state.RecordProjectActivity(key, when)
		return nil
	}
}

// configuredProjectPath resolves a location to the configured project path that
// owns it, or "" when no configured project does. An exact match wins; failing
// that the longest configured path the location sits under wins, so a worktree
// under a project root is attributed to that project.
func (m *Model) configuredProjectPath(path string) string {
	if path == "" || m.cfg == nil {
		return ""
	}
	target := filepath.Clean(config.ExpandPath(path))
	best := ""
	for _, project := range m.cfg.Projects.List {
		candidate := filepath.Clean(config.ExpandPath(project.Path))
		if candidate == target {
			return candidate
		}
		if isWithin(target, candidate) && len(candidate) > len(best) {
			best = candidate
		}
	}
	return best
}

// isWithin reports whether target sits under root.
func isWithin(target, root string) bool {
	if root == "" || root == "." {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// projectActivityTimes reads the recorded activity for every configured
// project. The current project is stamped as active now: it is being used, and
// waiting for the next bind to say so would show the row the user is standing
// in as older than the one they left.
func (m *Model) projectActivityTimes() map[string]time.Time {
	recorded := state.GetProjectActivity()
	if recorded == nil {
		recorded = map[string]time.Time{}
	}
	if !m.inGlobalScope() {
		for _, path := range []string{m.ui.WorkDir, m.ui.ProjectRoot} {
			if key := m.configuredProjectPath(path); key != "" {
				recorded[key] = time.Now().UTC()
				break
			}
		}
	}
	return recorded
}

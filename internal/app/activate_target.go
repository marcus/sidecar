package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/targetactivation"
	"github.com/marcus/sidecar/internal/terminallink"
)

// activateTarget executes a resolved activation. The decision — which plugin,
// which message, is this well-formed — belongs to targetactivation, which is
// state-free; the shell is here only because it is the one component that can
// focus plugins and (later) switch projects.
//
// A target naming another project switches projects first and lands afterwards,
// through the pending-target slot (see pending_target.go). A project that no
// longer resolves is declined out loud, never dropped silently.
func (m *Model) activateTarget(req ActivateTargetMsg) tea.Cmd {
	if !m.targetProjectIsCurrent(req.Project) {
		return m.activateTargetInOtherProject(req)
	}
	plan, err := targetactivation.Resolve(req.Target)
	if err != nil {
		return msg.Blocked(err.Error())
	}
	// The plugin a plan names can be absent — a project whose registry was just
	// rebuilt without it, or a build where it is compiled out. Say so rather
	// than focusing nothing and looking broken.
	if plan.PluginID != "" && m.registry != nil && m.registry.Get(plan.PluginID) == nil {
		return msg.Blocked("Cannot open that here: " + plan.PluginID + " is not available in this project")
	}
	switch plan.Kind {
	case targetactivation.PlanOpenFile:
		// The canonical file message is project-relative, so the containment
		// rule is applied here rather than in Resolve: a terminal surface
		// resolving the same plan against its own root accepts paths this
		// route must refuse.
		path, err := targetactivation.RelativeProjectPath(plan.Path)
		if err != nil {
			return msg.Blocked(err.Error())
		}
		return tea.Batch(
			FocusPlugin(plan.PluginID),
			func() tea.Msg { return NavigateToFileMsg{Path: path, Line: plan.Line} },
		)
	case targetactivation.PlanOpenURL:
		return terminallink.OpenHTTP(plan.URL)
	case targetactivation.PlanOpenIssue:
		return tea.Batch(FocusPlugin(plan.PluginID), OpenIssuePane(plan.Issue))
	case targetactivation.PlanOpenDiff:
		return tea.Batch(FocusPlugin(plan.PluginID), OpenDiffPane(plan.Spec))
	case targetactivation.PlanOpenResource:
		return tea.Batch(FocusPlugin(plan.PluginID), OpenResourcePane(plan.Provider, plan.Matcher, plan.Locator))
	case targetactivation.PlanAttachSession:
		return tea.Batch(FocusPlugin(plan.PluginID), AttachSession(plan.Session))
	case targetactivation.PlanOpenTask:
		// Focusing the tab is what this route can promise. The embedded Tasks
		// UI exposes no select-by-id entry point, so landing on the task
		// itself is not available yet; when it is, it becomes a second command
		// here and nothing else changes.
		return FocusPlugin(plan.PluginID)
	default:
		return nil
	}
}

// activateTargetInOtherProject parks the jump, switches project, and lets the
// pending-target slot re-emit it against the rebuilt registry.
func (m *Model) activateTargetInOtherProject(req ActivateTargetMsg) tea.Cmd {
	// Validate before switching. A malformed target should refuse where the
	// user is, not after tearing down every plugin in the project they were in.
	if _, err := targetactivation.Resolve(req.Target); err != nil {
		return msg.Blocked(err.Error())
	}
	destination, exact, ok := m.resolveProjectPath(req.Project)
	if !ok {
		return msg.Blocked("Cannot jump to " + strings.TrimSpace(req.Project) + ": that project is no longer available")
	}
	landing := req
	landing.Project = ""
	if destination == m.ui.WorkDir {
		// Named another way round but already here; switchProject would no-op
		// and strand the slot.
		return m.activateTarget(landing)
	}
	m.setPendingActivation(pendingActivation{target: &landing})
	// A qualifier given as a path names an exact checkout, and a relative target
	// only resolves there — so the remembered worktree must not override it. A
	// qualifier given as a project name names no worktree, so the remembered one
	// still wins, exactly as a shell selection does.
	switchCmd := m.switchProjectWithSelection(destination, nil, nil, !exact)
	if switchCmd == nil {
		// The switch declined, so nothing will ever apply the slot.
		m.clearPendingActivation()
		return m.activateTarget(landing)
	}
	return switchCmd
}

// resolveProjectPath turns a target's project qualifier — a path or a name —
// into a project path this instance can switch to. State-free resolution stops
// at the vocabulary; which projects exist is the shell's knowledge.
//
// exact reports that the qualifier named a checkout rather than a project, in
// which case the destination is a precise one the last-worktree memory must not
// override.
func (m *Model) resolveProjectPath(project string) (path string, exact bool, ok bool) {
	project = strings.TrimSpace(project)
	if project == "" {
		return m.ui.WorkDir, true, true
	}
	normalizedProject, projectErr := normalizePath(project)
	isPath := filepath.IsAbs(project)
	if m.cfg != nil {
		for _, candidate := range m.cfg.Projects.List {
			if candidate.Path == "" {
				continue
			}
			if candidate.Path == project {
				return candidate.Path, true, true
			}
			if projectErr == nil && isPath {
				if normalizedCandidate, err := normalizePath(candidate.Path); err == nil && normalizedCandidate == normalizedProject {
					return candidate.Path, true, true
				}
			}
			if strings.EqualFold(candidate.Name, project) || filepath.Base(candidate.Path) == project {
				return candidate.Path, false, true
			}
		}
	}
	// An unconfigured but real checkout is still somewhere the user can be sent;
	// a name that resolves to nothing on disk is not.
	if projectErr == nil && isPath {
		if info, err := os.Stat(normalizedProject); err == nil && info.IsDir() {
			return project, true, true
		}
	}
	return "", false, false
}

// targetProjectIsCurrent reports whether a target's project qualifier names the
// project this instance is already showing. Empty means "wherever the user is",
// which is every same-project activation.
func (m *Model) targetProjectIsCurrent(project string) bool {
	project = strings.TrimSpace(project)
	if project == "" {
		return true
	}
	// A qualifier given as a path names an exact checkout, so only the checkout
	// the user is actually in counts as "already here". Matching it against the
	// project root would call a jump to the main repo "current" while the user
	// sits in a linked worktree, and the target's relative path would then
	// resolve against the wrong tree — a different branch's copy of the file.
	// A qualifier given as a name names the project, where either is current.
	candidates := []string{m.ui.WorkDir, m.ui.ProjectRoot}
	if filepath.IsAbs(project) {
		candidates = candidates[:1]
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if project == candidate || project == filepath.Base(candidate) {
			return true
		}
		if normalizedCandidate, err := normalizePath(candidate); err == nil {
			if normalizedProject, err := normalizePath(project); err == nil && normalizedProject == normalizedCandidate {
				return true
			}
		}
	}
	return false
}

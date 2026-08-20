// Package targetactivation is the state-free half of Sidecar's one jump
// service: it answers "what should happen when someone activates this target"
// without touching a model, a plugin, or the filesystem. The app shell owns
// execution (focusing plugins, switching projects); this package owns the
// decision, so a headless caller — a CLI action, a test, a future API — can
// adopt the same answer unchanged.
//
// # Safety rules for every consumer
//
// The scanner (internal/terminallink) deliberately does not sanitize; the
// surfaces do. Both halves of that discipline live here so a new consumer
// cannot miss them:
//
//  1. URL activation refuses anything terminallink.SafeHTTPURL rejects. Resolve
//     enforces it; no caller should open a URL it did not get back in a Plan.
//  2. Any surface that renders untrusted text through terminallink.Decorate
//     must call terminallink.StripOSC8 on the line first. Otherwise a hostile
//     OSC 8 hyperlink already embedded in the text survives decoration and the
//     rendered link no longer means what the scanned span says it means.
//     Activation cannot defend against this — it never sees the raw line — so
//     the rule belongs at every render site that feeds this service.
//
// A file target's Path is the token as the text wrote it — it may be absolute,
// because a terminal surface resolves it against its own root. The
// project-relative constraint belongs to the one execution that needs it, the
// canonical app.NavigateToFileMsg, and lives in RelativeProjectPath.
package targetactivation

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
)

// FileBrowserPluginID is the plugin that owns file targets.
const FileBrowserPluginID = "file-browser"

// WorkspacePluginID owns the content panes — issue, diff, resource — and the
// tmux sessions a session target attaches to.
const WorkspacePluginID = "workspace-manager"

// TasksPluginID owns task targets.
const TasksPluginID = "tasks"

// PlanKind names the executable shape of a resolved activation.
type PlanKind string

const (
	// PlanOpenFile focuses PluginID and asks it to reveal Path (Line optional).
	PlanOpenFile PlanKind = "open-file"
	// PlanOpenURL opens an already-validated http(s) URL in the browser.
	PlanOpenURL PlanKind = "open-url"
	// PlanOpenIssue opens Issue in an issue pane.
	PlanOpenIssue PlanKind = "open-issue"
	// PlanOpenDiff opens the git spec in Spec in a diff pane. The spec is
	// re-resolved by the host against its own checkout: this package does not
	// run git.
	PlanOpenDiff PlanKind = "open-diff"
	// PlanOpenResource opens an external provider's locator in a resource
	// pane. Matcher may be empty — a host with a live matcher snapshot decides
	// which matcher claims the locator.
	PlanOpenResource PlanKind = "open-resource"
	// PlanAttachSession attaches the tmux session named Session.
	PlanAttachSession PlanKind = "attach-session"
	// PlanOpenTask focuses the Tasks tab on Task. Landing on the specific
	// task inside the embedded model is best effort — the host does what its
	// embedded UI allows — but focusing the tab always happens, so the jump is
	// never a no-op.
	PlanOpenTask PlanKind = "open-task"
)

// Plan is what the shell executes. It is data, not commands: the shell turns
// it into tea.Cmds, and a headless caller can act on the same fields.
type Plan struct {
	Kind     PlanKind
	PluginID string
	Path     string
	Line     int
	URL      string
	Issue    string
	Spec     string
	Provider string
	Matcher  string
	Locator  string
	Session  string
	Task     string
}

// PlanKindsFromSpans lists every plan kind a scanned terminal-link span can
// produce. It is the parity contract between the surfaces: each of them must
// dispatch all of these, and TestSpanKindsCoverPlanKinds keeps the list honest
// against terminallink.Activatable.
func PlanKindsFromSpans() []PlanKind {
	// PlanOpenTask is deliberately absent: a task id is bare 8-hex, which no
	// scanner can tell from a short sha or from prose, so a task target only
	// ever arrives from a poster that named one (`--target task:...`) and
	// never from a scanned span.
	return []PlanKind{PlanOpenURL, PlanOpenFile, PlanOpenIssue, PlanOpenDiff, PlanOpenResource, PlanAttachSession}
}

// PlanForSpan is the whole span→activation path in one call: the shared
// span→uirequest.Target mapping followed by Resolve. Surfaces use it so a
// scanned link means the same thing wherever it was scanned.
func PlanForSpan(span terminallink.Span) (Plan, error) {
	target, ok := uirequest.TargetFromSpan(span)
	if !ok {
		return Plan{}, fmt.Errorf("%w: span kind %s", ErrUnsupportedKind, span.Kind)
	}
	return Resolve(target)
}

// ErrUnsupportedKind reports a target kind this service does not route yet.
// Callers migrating a surface one kind at a time can keep their existing branch
// for these rather than treating them as malformed.
var ErrUnsupportedKind = errors.New("target kind is not activatable yet")

// Resolve decides how a target is activated. It touches nothing: no model, no
// filesystem, no network. Errors are user-facing refusals ("why nothing
// happened"), never diagnostics.
func Resolve(target uirequest.Target) (Plan, error) {
	value := strings.TrimSpace(target.Value)
	switch target.Kind {
	case uirequest.TargetKindFile:
		return resolveFile(value, target.Line)
	case uirequest.TargetKindURL:
		safe, ok := terminallink.SafeHTTPURL(value)
		if !ok {
			return Plan{}, fmt.Errorf("refusing to open %q: only http and https links are opened", value)
		}
		return Plan{Kind: PlanOpenURL, URL: safe}, nil
	case uirequest.TargetKindIssue:
		if err := plainValue(value, "issue"); err != nil {
			return Plan{}, err
		}
		return Plan{Kind: PlanOpenIssue, PluginID: WorkspacePluginID, Issue: value}, nil
	case uirequest.TargetKindDiff:
		if err := plainValue(value, "diff"); err != nil {
			return Plan{}, err
		}
		return Plan{Kind: PlanOpenDiff, PluginID: WorkspacePluginID, Spec: value}, nil
	case uirequest.TargetKindResource:
		provider := strings.TrimSpace(target.Provider)
		if provider == "" {
			return Plan{}, errors.New("resource target has no provider instance")
		}
		if err := plainValue(value, "resource"); err != nil {
			return Plan{}, err
		}
		if strings.ContainsFunc(provider, isControl) || strings.ContainsFunc(target.Matcher, isControl) {
			return Plan{}, errors.New("resource provider contains control characters")
		}
		return Plan{
			Kind: PlanOpenResource, PluginID: WorkspacePluginID,
			Provider: provider, Matcher: target.Matcher, Locator: value,
		}, nil
	case uirequest.TargetKindSession:
		if err := plainValue(value, "session"); err != nil {
			return Plan{}, err
		}
		return Plan{Kind: PlanAttachSession, PluginID: WorkspacePluginID, Session: value}, nil
	case uirequest.TargetKindTask:
		if err := plainValue(value, "task"); err != nil {
			return Plan{}, err
		}
		return Plan{Kind: PlanOpenTask, PluginID: TasksPluginID, Task: value}, nil
	case "":
		return Plan{}, errors.New("target has no kind")
	default:
		return Plan{}, fmt.Errorf("%w: %s", ErrUnsupportedKind, target.Kind)
	}
}

func resolveFile(value string, line int) (Plan, error) {
	if value == "" {
		return Plan{}, errors.New("file target has no path")
	}
	if strings.ContainsFunc(value, isControl) {
		return Plan{}, errors.New("file path contains control characters")
	}
	if line < 0 {
		return Plan{}, fmt.Errorf("file target has a negative line (%d)", line)
	}
	// Path stays the token as written. A terminal surface resolves it against
	// the root it scanned, where an absolute path is ordinary; only the
	// project-relative execution needs RelativeProjectPath.
	return Plan{Kind: PlanOpenFile, PluginID: FileBrowserPluginID, Path: value, Line: line}, nil
}

// RelativeProjectPath cleans a file plan's Path for the canonical file message
// (app.NavigateToFileMsg), which is defined relative to the project's workdir.
// An absolute path, a "~" path, or one that climbs out with ".." is refused
// rather than resolved. Hosts that resolve against their own root — the
// terminal surfaces — do not call this.
func RelativeProjectPath(value string) (string, error) {
	slashed := strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(slashed, "/") || strings.HasPrefix(slashed, "~") {
		return "", fmt.Errorf("refusing to open %q: file targets are relative to the project", value)
	}
	clean := path.Clean(slashed)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("refusing to open %q: file targets cannot leave the project", value)
	}
	return clean, nil
}

func plainValue(value, kind string) error {
	if value == "" {
		return fmt.Errorf("%s target has no value", kind)
	}
	if strings.ContainsFunc(value, isControl) {
		return fmt.Errorf("%s target contains control characters", kind)
	}
	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

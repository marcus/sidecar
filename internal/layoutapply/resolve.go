package layoutapply

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// ResolveTargets turns a descriptor's target strings into resolved uirequest
// targets through ResolveTarget — the exact classification the CLI's `open`
// argument goes through, here on the host where the workspace root is known.
func ResolveTargets(kind panelayout.Kind, spec uirequest.LayoutPane, root string, matchers []terminallink.ResourceMatcher) ([]uirequest.Target, string) {
	// A resource pane naming a collection is a plugin tab: the collection is
	// what it opens, an optional single target is the row inside it, and no
	// matcher is consulted because a plugin row is addressed by name.
	if kind == panelayout.Resource && strings.TrimSpace(spec.Collection) != "" {
		row := ""
		if len(spec.Targets) == 1 {
			row = spec.Targets[0]
		}
		tgt, err := uirequest.ResolveCollectionTarget(spec.Provider, spec.Collection, spec.Query, row, spec.Filters)
		if err != nil {
			return nil, err.Error()
		}
		return []uirequest.Target{tgt}, ""
	}
	if len(spec.Targets) == 0 {
		if kind == panelayout.Diff {
			return []uirequest.Target{{Kind: uirequest.TargetKindDiff, Value: workspacediff.IdentityWorkingTree}}, ""
		}
		return nil, "a " + kind.Name() + " pane needs at least one target"
	}
	want, ok := WireKind(kind)
	if !ok {
		return nil, "unsupported pane kind " + kind.Name()
	}
	targets := make([]uirequest.Target, 0, len(spec.Targets))
	for _, raw := range spec.Targets {
		var (
			tgt uirequest.Target
			err error
		)
		if kind == panelayout.Resource {
			tgt, err = uirequest.ResolveResourceTarget(spec.Provider, raw)
		} else {
			tgt, err = uirequest.ResolveTarget(root, raw, 0, uirequest.ResolveOptions{Diff: kind == panelayout.Diff})
		}
		if err != nil {
			return nil, fmt.Sprintf("target %q: %v", raw, err)
		}
		if tgt.Kind != want {
			return nil, fmt.Sprintf("target %q resolves to a %s pane, want %s", raw, wireNameForTarget(tgt.Kind), kind.Name())
		}
		targets = append(targets, tgt)
	}
	if kind == panelayout.Resource {
		ref, refusal := resourceview.ReferenceForLocator(matchers, spec.Provider, targets[0].Value)
		if refusal != "" {
			return nil, refusal
		}
		targets[0].Matcher = ref.Matcher
	}
	return targets, ""
}

func WireKind(kind panelayout.Kind) (uirequest.TargetKind, bool) {
	switch kind {
	case panelayout.Document:
		return uirequest.TargetKindFile, true
	case panelayout.Issue:
		return uirequest.TargetKindIssue, true
	case panelayout.Diff:
		return uirequest.TargetKindDiff, true
	case panelayout.Note:
		return uirequest.TargetKindNote, true
	case panelayout.Resource:
		return uirequest.TargetKindResource, true
	default:
		return "", false
	}
}

func wireNameForTarget(kind uirequest.TargetKind) string {
	if mapped, ok := panelayout.KindByName(string(kind)); ok {
		return mapped.Name()
	}
	return string(kind)
}

// ResolveRemoteTargets is ResolveTargets for a surface bound to another
// machine: it classifies each target string against the host's content Source
// rather than this machine's filesystem.
//
// It is shared by the Sessions preview and the bound project workspace on
// purpose. The two surfaces are two projections of one model, and the first
// time this rule lived in both packages the copies drifted within a single
// commit — a note URI normalized on one surface and passed through raw on the
// other. There is one remote resolver, and this is it.
//
// A failed resolve is a refusal, not a silent skip: apply is all-or-nothing.
func ResolveRemoteTargets(kind panelayout.Kind, spec uirequest.LayoutPane, src contentpanes.Source, sourceCtx contentpanes.SourceContext, matchers []terminallink.ResourceMatcher) ([]uirequest.Target, string) {
	// Resource locators are matched against this viewer's configured matchers,
	// exactly as the local path does; the host answers the read afterwards.
	if kind == panelayout.Resource {
		return ResolveTargets(kind, spec, "", matchers)
	}
	if len(spec.Targets) == 0 {
		if kind == panelayout.Diff {
			spec.Targets = []string{workspacediff.IdentityWorkingTree}
		} else {
			return nil, "a " + kind.Name() + " pane needs at least one target"
		}
	}
	want, ok := WireKind(kind)
	if !ok {
		return nil, "unsupported pane kind " + kind.Name()
	}
	pendingKind, ok := RemotePendingKind(kind)
	if !ok {
		return nil, "unsupported pane kind " + kind.Name()
	}
	targets := make([]uirequest.Target, 0, len(spec.Targets))
	for _, raw := range spec.Targets {
		raw = strings.TrimSpace(raw)
		line := 0
		if kind == panelayout.Document {
			raw, line = SplitFileLine(raw)
		}
		if kind == panelayout.Note {
			raw = noteTargetID(raw)
		}
		if kind == panelayout.Diff && raw == "" {
			raw = workspacediff.IdentityWorkingTree
		}
		ref, err := contentpanes.ResolveDocument(src, sourceCtx, contentlink.Pending{Kind: pendingKind, Raw: raw})
		if err != nil {
			return nil, fmt.Sprintf("target %q: %v", raw, err)
		}
		if ref.Value == "" {
			host := sourceCtx.HostID
			if host == "" {
				host = "that host"
			}
			return nil, fmt.Sprintf("target %q: not found on %s", raw, host)
		}
		tgt := TargetFromRef(ref, line)
		if tgt.Kind != want {
			return nil, fmt.Sprintf("target %q resolves to a %s pane, want %s", raw, wireNameForTarget(tgt.Kind), kind.Name())
		}
		targets = append(targets, tgt)
	}
	return targets, ""
}

// RemotePendingKind maps a pane kind onto the contentlink kind a remote
// Source resolves it as. Resource is absent: it never reaches the Source.
func RemotePendingKind(kind panelayout.Kind) (contentlink.Kind, bool) {
	switch kind {
	case panelayout.Document:
		return contentlink.KindFile, true
	case panelayout.Issue:
		return contentlink.KindIssue, true
	case panelayout.Note:
		return contentlink.KindInternal, true
	case panelayout.Diff:
		return contentlink.KindDiff, true
	default:
		return "", false
	}
}

// TargetFromRef turns a resolved content reference back into the wire target a
// layout item carries. An unrecognised ref yields the zero Target, which the
// caller reports as a kind mismatch rather than opening anything.
func TargetFromRef(ref contentlink.Ref, line int) uirequest.Target {
	switch ref.Kind {
	case contentlink.KindFile:
		return uirequest.Target{Kind: uirequest.TargetKindFile, Value: ref.Value, Line: line}
	case contentlink.KindIssue:
		return uirequest.Target{Kind: uirequest.TargetKindIssue, Value: ref.Value}
	case contentlink.KindInternal:
		if ref.Namespace == "note" {
			return uirequest.Target{Kind: uirequest.TargetKindNote, Value: ref.Value}
		}
	case contentlink.KindDiff:
		return uirequest.Target{Kind: uirequest.TargetKindDiff, Value: ref.Value}
	case contentlink.KindResource:
		return uirequest.Target{Kind: uirequest.TargetKindResource, Value: ref.Value, Provider: ref.Provider, Matcher: ref.Matcher}
	}
	return uirequest.Target{}
}

// SplitFileLine peels a trailing :N off a document target. The local path gets
// the same treatment inside uirequest.ResolveTarget; a remote target never
// reaches that function, so the split has to happen before the Source sees it.
func SplitFileLine(raw string) (string, int) {
	if colonIdx := strings.LastIndex(raw, ":"); colonIdx > 0 && colonIdx < len(raw)-1 {
		if n, err := strconv.Atoi(raw[colonIdx+1:]); err == nil && n > 0 {
			return raw[:colonIdx], n
		}
	}
	return raw, 0
}

// noteTargetID accepts a note either as a bare id or as the sidecar://note/<id>
// URI a link click carries, so both spell the same pane.
func noteTargetID(raw string) string {
	if parsed, err := contentlink.ParseInternalURI(raw); err == nil && parsed.Ref.Namespace == "note" && parsed.Ref.Value != "" {
		return parsed.Ref.Value
	}
	return raw
}

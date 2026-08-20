package notify

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/marcus/sidecar/internal/terminallink"
)

// Target specs
//
// `sidecar notify post --target kind:value[:line][@project]` is how an agent
// attaches a precise call to action instead of hoping the scanner finds one in
// the prose. The grammar is parsed here, not in the CLI, because it is a rule
// about the model: any surface that lets a poster name a target — a future API
// or MCP tool — must accept exactly the same strings.
//
// The parse is deliberately strict. A malformed target is a mistake an agent
// can see and fix; a silently dropped one is a notification that quietly does
// less than it says.

// TargetKinds lists the kinds a spec may name, in the order they are documented.
func TargetKinds() []TargetKind {
	return []TargetKind{TargetIssue, TargetTask, TargetCommit, TargetFile, TargetSession, TargetURL}
}

// TargetKindNames is TargetKinds as strings, for help text and error messages.
func TargetKindNames() []string {
	kinds := TargetKinds()
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	return out
}

// ValidTargetKind reports whether kind is one a spec may name.
func ValidTargetKind(kind TargetKind) bool {
	for _, k := range TargetKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// ParseTargetSpec parses one `kind:value[:line][@project]` spec.
//
// The two ambiguities in that grammar are resolved by kind:
//
//   - `:line` is read only for file targets. A commit sha, a session name and
//     a URL can all end in digits, and none of them has a line.
//   - `@project` is read only when the text after the last `@` looks like a
//     project qualifier: an absolute path, or a name with no `/` and no `:`.
//     That keeps `url:https://user@host/path` intact while `url:https://x@here`
//     is still qualifiable. (Project qualifiers are resolved by the running
//     instance — a configured project name or a checkout path.)
func ParseTargetSpec(spec string) (Target, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return Target{}, fmt.Errorf("empty target")
	}
	head, rest, ok := strings.Cut(raw, ":")
	if !ok {
		return Target{}, fmt.Errorf("target %q has no kind (want kind:value, one of: %s)", spec, strings.Join(TargetKindNames(), ", "))
	}
	kind := TargetKind(strings.ToLower(strings.TrimSpace(head)))
	if !ValidTargetKind(kind) {
		return Target{}, fmt.Errorf("unknown target kind %q (one of: %s)", head, strings.Join(TargetKindNames(), ", "))
	}

	value, project := splitTargetProject(rest, kind)
	value = strings.TrimSpace(value)
	line := 0
	if kind == TargetFile {
		value, line = splitTargetLine(value)
	}
	if value == "" {
		return Target{}, fmt.Errorf("target %q names no value", spec)
	}

	switch kind {
	case TargetURL:
		if _, ok := terminallink.SafeHTTPURL(value); !ok {
			return Target{}, fmt.Errorf("target %q is not a safe http(s) URL", spec)
		}
	case TargetSession:
		// Only Sidecar-owned session names attach: the lookup behind a session
		// target is over this instance's shells and worktree agents, and every
		// one of those runs under a name of this shape. Refusing here is the
		// difference between a typo an agent can see and a digit that silently
		// does nothing.
		if !terminallink.SessionName(value) {
			return Target{}, fmt.Errorf("target %q is not a Sidecar tmux session (want sidecar-sh-… or sidecar-ws-…)", spec)
		}
	case TargetIssue:
		if !terminallink.IssueID(value) {
			return Target{}, fmt.Errorf("target %q is not a td issue id (want td-xxxxxx)", spec)
		}
	}
	if strings.ContainsAny(value, "\n\r") {
		return Target{}, fmt.Errorf("target %q contains a line break", spec)
	}

	return Target{Kind: kind, Value: value, Line: line, Project: strings.TrimSpace(project)}, nil
}

// ParseTargetSpecs parses a repeatable --target list, keeping poster order and
// dropping exact duplicates (the same kind, value, line and project twice is a
// typo, not two calls to action).
func ParseTargetSpecs(specs []string) ([]Target, error) {
	var out []Target
	seen := map[Target]bool{}
	for _, spec := range specs {
		target, err := ParseTargetSpec(spec)
		if err != nil {
			return nil, err
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	return out, nil
}

func splitTargetProject(rest string, kind TargetKind) (value, project string) {
	idx := strings.LastIndex(rest, "@")
	if idx < 0 {
		return rest, ""
	}
	candidate := strings.TrimSpace(rest[idx+1:])
	if candidate == "" {
		return rest, ""
	}
	if !looksLikeProjectQualifier(candidate) {
		return rest, ""
	}
	if idx == 0 {
		// "@project" with nothing before it: no value, let the caller refuse.
		return "", candidate
	}
	_ = kind
	return rest[:idx], candidate
}

func looksLikeProjectQualifier(candidate string) bool {
	if strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "~") {
		return true
	}
	return !strings.ContainsAny(candidate, "/:")
}

func splitTargetLine(value string) (string, int) {
	idx := strings.LastIndex(value, ":")
	if idx <= 0 || idx == len(value)-1 {
		return value, 0
	}
	line, err := strconv.Atoi(value[idx+1:])
	if err != nil || line <= 0 {
		return value, 0
	}
	return value[:idx], line
}

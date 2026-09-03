package uirequest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// ResolveOptions controls how ResolveTarget classifies a CLI argument.
type ResolveOptions struct {
	// Diff is sidecar open --diff. A missing positional becomes the working
	// tree (wt). A positional is a git spec, including HEAD and branch names.
	Diff bool
	// Provider is sidecar open --provider <instance>. It short-circuits every
	// other classification: with it, the positional is a provider locator and
	// nothing else, so a ticket key that happens to name a file cannot be
	// silently reinterpreted.
	Provider string
}

// ResolveResourceTarget validates a --provider request without contacting
// anything. Matching the locator to a matcher is the running app's job: it
// holds the live snapshot, and a short-lived CLI process must never start a
// provider to answer `sidecar open`.
func ResolveResourceTarget(provider, raw string) (Target, error) {
	provider = strings.TrimSpace(provider)
	raw = strings.TrimSpace(raw)
	if provider == "" {
		return Target{}, fmt.Errorf("a provider instance is required")
	}
	if raw == "" {
		return Target{}, fmt.Errorf("a resource locator is required")
	}
	if utf8.RuneCountInString(provider) > resource.MaxInstanceIDChars {
		return Target{}, fmt.Errorf("provider instance is longer than %d characters", resource.MaxInstanceIDChars)
	}
	if utf8.RuneCountInString(raw) > resource.MaxLocatorChars {
		return Target{}, fmt.Errorf("locator is longer than %d characters", resource.MaxLocatorChars)
	}
	if strings.ContainsFunc(raw, isControl) || strings.ContainsFunc(provider, isControl) {
		return Target{}, fmt.Errorf("provider instance and locator cannot contain control characters")
	}
	return Target{Kind: TargetKindResource, Value: raw, Provider: provider}, nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// ResolveTarget parses and validates a target string against the shell's workspace root.
//
// Classification, in order:
//  1. td- issue id (even with --diff)
//  2. --diff with no positional → working tree (Value "wt")
//  3. --diff with a positional → ParseSpec + ResolveSpec
//  4. Existing regular file inside workDir (a real file named abc1234 wins)
//  5. ParseSpec + ResolveSpec → TargetKindDiff
//  6. Usage error (a missing file is not fatal until hash resolution also fails)
func ResolveTarget(workDir, raw string, explicitLine int, opts ResolveOptions) (Target, error) {
	raw = strings.TrimSpace(raw)
	// --provider is explicit and wins before anything else looks at the
	// filesystem, so CASH-1245 cannot become a file or a git spec.
	if opts.Provider != "" {
		return ResolveResourceTarget(opts.Provider, raw)
	}
	if raw == "" {
		if opts.Diff {
			return Target{Kind: TargetKindDiff, Value: workspacediff.IdentityWorkingTree}, nil
		}
		return Target{}, fmt.Errorf("target cannot be empty")
	}

	if terminallink.IssueID(raw) {
		return Target{
			Kind:  TargetKindIssue,
			Value: raw,
			Line:  0,
		}, nil
	}

	if parsed, err := contentlink.ParseInternalURI(raw); err == nil && parsed.Ref.Namespace == "note" {
		if !contentlink.NoteID(parsed.Ref.Value) {
			return Target{}, fmt.Errorf("invalid note identity %q", parsed.Ref.Value)
		}
		return Target{Kind: TargetKindNote, Value: parsed.Ref.Value}, nil
	}

	workDir, err := canonicalizeWorkDir(workDir)
	if err != nil {
		return Target{}, err
	}

	if opts.Diff {
		return resolveDiffTarget(workDir, raw)
	}

	file, fileErr := resolveFileTarget(workDir, raw, explicitLine)
	if fileErr == nil {
		return file, nil
	}
	if !errors.Is(fileErr, os.ErrNotExist) {
		return Target{}, fileErr
	}

	if diff, diffErr := resolveDiffTarget(workDir, raw); diffErr == nil {
		return diff, nil
	}
	return Target{}, fileErr
}

func canonicalizeWorkDir(workDir string) (string, error) {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
	}
	workDirClean := filepath.Clean(workDir)
	if resolvedRoot, err := filepath.EvalSymlinks(workDirClean); err == nil {
		workDirClean = filepath.Clean(resolvedRoot)
	}
	return workDirClean, nil
}

// DiffTarget turns a request Value into a workspacediff.Target. Hosts re-resolve
// in workDir (no shell) so a crafted request cannot skip rev-parse. An
// unresolvable spec is still returned parsed — the leaf load reports the error.
func DiffTarget(workDir, value string) workspacediff.Target {
	spec, ok := workspacediff.ParseSpec(value)
	if !ok {
		return workspacediff.WorkingTreeTarget()
	}
	if workDir == "" {
		return spec
	}
	resolved, err := workspacediff.ResolveSpec(context.Background(), workDir, spec)
	if err != nil {
		return spec
	}
	return resolved
}

func resolveDiffTarget(workDir, raw string) (Target, error) {
	spec, ok := workspacediff.ParseSpec(raw)
	if !ok {
		return Target{}, fmt.Errorf("not a git spec: %q", raw)
	}
	resolved, err := workspacediff.ResolveSpec(context.Background(), workDir, spec)
	if err != nil {
		return Target{}, fmt.Errorf("unknown git object %q", raw)
	}
	ident := resolved.Identity()
	if ident == "" {
		return Target{}, fmt.Errorf("unknown git object %q", raw)
	}
	return Target{Kind: TargetKindDiff, Value: ident}, nil
}

func resolveFileTarget(workDir, raw string, explicitLine int) (Target, error) {
	targetPath := raw
	line := explicitLine
	if colonIdx := strings.LastIndex(raw, ":"); colonIdx > 0 && colonIdx < len(raw)-1 {
		suffix := raw[colonIdx+1:]
		if lineNum, err := strconv.Atoi(suffix); err == nil && lineNum > 0 {
			targetPath = raw[:colonIdx]
			if explicitLine <= 0 {
				line = lineNum
			}
		}
	}

	if targetPath == "" {
		return Target{}, fmt.Errorf("file path cannot be empty")
	}

	var absPath string
	if filepath.IsAbs(targetPath) {
		absPath = filepath.Clean(targetPath)
	} else if strings.HasPrefix(targetPath, "~/") || targetPath == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Target{}, fmt.Errorf("resolve home dir: %w", err)
		}
		absPath = filepath.Join(home, strings.TrimPrefix(targetPath, "~"))
	} else {
		absPath = filepath.Join(workDir, targetPath)
	}

	resolvedTarget, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Target{}, fmt.Errorf("file %q does not exist: %w", targetPath, os.ErrNotExist)
		}
		return Target{}, fmt.Errorf("resolve target path %q: %w", targetPath, err)
	}
	resolvedTarget = filepath.Clean(resolvedTarget)

	info, err := os.Stat(resolvedTarget)
	if err != nil {
		if os.IsNotExist(err) {
			return Target{}, fmt.Errorf("file %q does not exist: %w", targetPath, os.ErrNotExist)
		}
		return Target{}, fmt.Errorf("stat target %q: %w", targetPath, err)
	}
	if info.IsDir() {
		return Target{}, fmt.Errorf("target %q is a directory, not a file", targetPath)
	}
	if !info.Mode().IsRegular() {
		return Target{}, fmt.Errorf("target %q is not a regular file", targetPath)
	}

	rel, err := filepath.Rel(workDir, resolvedTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Target{}, fmt.Errorf("target %q resolves outside workspace root %s", targetPath, workDir)
	}

	return Target{
		Kind:  TargetKindFile,
		Value: filepath.ToSlash(rel),
		Line:  line,
	}, nil
}

// ResolveFileTarget is resolveFileTarget's exported form: file-only
// classification for callers that have already chosen the kind (the pane
// switcher's File picker). It is the same resolution `sidecar open` runs, so
// both produce one target shape.
func ResolveFileTarget(workDir, raw string, explicitLine int) (Target, error) {
	workDir, err := canonicalizeWorkDir(workDir)
	if err != nil {
		return Target{}, err
	}
	return resolveFileTarget(workDir, raw, explicitLine)
}

// ResolveDiffSpec is resolveDiffTarget's exported form: diff-only
// classification for callers that have already chosen the kind. An empty raw
// resolves to the working tree, matching `sidecar open --diff`.
func ResolveDiffSpec(workDir, raw string) (Target, error) {
	if strings.TrimSpace(raw) == "" {
		return Target{Kind: TargetKindDiff, Value: workspacediff.IdentityWorkingTree}, nil
	}
	workDir, err := canonicalizeWorkDir(workDir)
	if err != nil {
		return Target{}, err
	}
	return resolveDiffTarget(workDir, raw)
}

// TargetFromSpan maps a scanned terminal-link span onto the cross-surface
// target vocabulary. It is the one span→Target translation: the surfaces used
// to each keep their own, and the two drifted. It resolves nothing — a span
// arrives already resolved by the scanner's Resolve hooks — so it is safe
// anywhere, including a render pass.
//
// Raw wins over Value for file and diff spans because Raw is the token as the
// text wrote it; the surfaces re-resolve it against their own root on
// activation, exactly as they did before.
//
// A resource span carries both halves of its identity — Provider and Matcher —
// because a live matcher already claimed the locator to produce the span; the
// host does not have to guess which matcher owns it a second time.
//
// It reports false only for spans this vocabulary does not carry, so a caller
// keeps its existing branch rather than mistaking "not mapped" for "malformed".
func TargetFromSpan(span terminallink.Span) (Target, bool) {
	rawOrValue := func() string {
		if span.Extra.Raw != "" {
			return span.Extra.Raw
		}
		return span.Value
	}
	switch span.Kind {
	case terminallink.KindFile:
		return Target{Kind: TargetKindFile, Value: rawOrValue(), Line: span.Extra.Line}, true
	case terminallink.KindURL:
		return Target{Kind: TargetKindURL, Value: span.Value}, true
	case terminallink.KindIssue:
		return Target{Kind: TargetKindIssue, Value: span.Value}, true
	case terminallink.KindInternal:
		if span.Extra.Namespace == "note" && contentlink.NoteID(span.Value) {
			return Target{Kind: TargetKindNote, Value: span.Value}, true
		}
		return Target{}, false
	case terminallink.KindDiff:
		return Target{Kind: TargetKindDiff, Value: rawOrValue()}, true
	case terminallink.KindSession:
		return Target{Kind: TargetKindSession, Value: span.Value}, true
	case terminallink.KindResource:
		return Target{
			Kind:     TargetKindResource,
			Value:    span.Value,
			Provider: span.Extra.Provider,
			Matcher:  span.Extra.Matcher,
		}, true
	default:
		return Target{}, false
	}
}

// ResolveCollectionTarget validates a `--plugin` request without contacting
// anything, for the same reason ResolveResourceTarget does: a short-lived CLI
// process must never start a plugin to answer `sidecar open`.
//
// The two shapes are distinguished by whether a row was named. A collection
// with no row opens the collection tab and carries its opening query; a
// collection with a row opens that row's document tab, and a query there would
// be describing a search nobody is running.
// Equal reports whether two targets name the same thing. It is spelled out
// because a resource target carries an applied filter map, which makes the
// struct uncomparable with ==; every field still takes part.
func (t Target) Equal(other Target) bool {
	if t.Kind != other.Kind || t.Value != other.Value || t.Line != other.Line ||
		t.Provider != other.Provider || t.Matcher != other.Matcher ||
		t.Collection != other.Collection || t.Query != other.Query ||
		len(t.Filters) != len(other.Filters) {
		return false
	}
	for id, value := range t.Filters {
		// Comma-ok, not a lookup: an absent key reads as "" and would compare
		// equal to a filter deliberately cleared to the empty string.
		if v, ok := other.Filters[id]; !ok || v != value {
			return false
		}
	}
	return true
}

func ResolveCollectionTarget(plugin, collection, query, row string, filters map[string]string) (Target, error) {
	plugin = strings.TrimSpace(plugin)
	collection = strings.TrimSpace(collection)
	row = strings.TrimSpace(row)
	if plugin == "" {
		return Target{}, fmt.Errorf("a plugin instance is required")
	}
	if collection == "" {
		return Target{}, fmt.Errorf("a collection is required")
	}
	if utf8.RuneCountInString(plugin) > resource.MaxInstanceIDChars {
		return Target{}, fmt.Errorf("plugin instance is longer than %d characters", resource.MaxInstanceIDChars)
	}
	if utf8.RuneCountInString(collection) > resource.MaxCollectionIDChars {
		return Target{}, fmt.Errorf("collection is longer than %d characters", resource.MaxCollectionIDChars)
	}
	if utf8.RuneCountInString(query) > resource.MaxQueryChars {
		return Target{}, fmt.Errorf("query is longer than %d characters", resource.MaxQueryChars)
	}
	if utf8.RuneCountInString(row) > resource.MaxLocatorChars {
		return Target{}, fmt.Errorf("row id is longer than %d characters", resource.MaxLocatorChars)
	}
	if strings.ContainsFunc(plugin, isControl) || strings.ContainsFunc(collection, isControl) ||
		strings.ContainsFunc(query, isControl) || strings.ContainsFunc(row, isControl) {
		return Target{}, fmt.Errorf("plugin instance, collection, query and row id cannot contain control characters")
	}
	if row != "" && query != "" {
		return Target{}, fmt.Errorf("a row id and a query name different things to open; pass one")
	}
	if len(filters) > resource.MaxFilters {
		return Target{}, fmt.Errorf("a collection carries at most %d filters", resource.MaxFilters)
	}
	var applied map[string]string
	for id, value := range filters {
		id = strings.TrimSpace(id)
		if id == "" {
			return Target{}, fmt.Errorf("a filter needs an id")
		}
		if utf8.RuneCountInString(id) > resource.MaxFilterIDChars {
			return Target{}, fmt.Errorf("filter id %q is longer than %d characters", id, resource.MaxFilterIDChars)
		}
		if utf8.RuneCountInString(value) > resource.MaxFilterValueChars {
			return Target{}, fmt.Errorf("filter %q value is longer than %d characters", id, resource.MaxFilterValueChars)
		}
		if strings.ContainsFunc(id, isControl) || strings.ContainsFunc(value, isControl) {
			return Target{}, fmt.Errorf("filter ids and values cannot contain control characters")
		}
		if applied == nil {
			applied = make(map[string]string, len(filters))
		}
		applied[id] = value
	}
	if row != "" && len(applied) > 0 {
		return Target{}, fmt.Errorf("a row id and a filter name different things to open; pass one")
	}
	return Target{
		Kind: TargetKindResource, Value: row,
		Provider: plugin, Collection: collection, Query: query, Filters: applied,
	}, nil
}

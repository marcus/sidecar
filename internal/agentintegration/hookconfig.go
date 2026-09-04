package agentintegration

// Shared machinery for integrations that live as an *entry inside a provider's
// own configuration file* rather than as a standalone Sidecar-owned file.
//
// OpenCode's ownership model — a marker line in a file whose every byte is
// Sidecar's — does not survive contact with Codex and Claude Code: both keep
// their hooks in a shared, user-owned JSON document that Sidecar may add one
// entry to but can never own outright. Ownership therefore has to be a
// property of the entry, and the only part of an entry that identifies it
// unambiguously is its command: Sidecar's hooks exist solely to invoke
// `sidecar agent report-session`, a verb that has no other caller. An entry
// whose command is that invocation is Sidecar's to manage — leaving a second
// copy behind would double every report, the exact damage the OpenCode
// adapter's conflict-directory rule exists to prevent — and an entry whose
// command is anything else is the user's, byte for byte, forever.
//
// The distinction between "Sidecar's" and "current" mirrors the marker/checksum
// split: invoking report-session is the marker (this entry is ours to manage),
// and canonical equality with the bundled entry is the checksum (this entry is
// exactly what this build ships). A tampered entry still invokes the verb, so
// it reads as Sidecar's and needs-repair rather than silently becoming foreign
// and firing alongside a freshly installed copy.
//
// Everything the adapters do not touch is preserved token-for-token and in
// order. The parser keeps every unrecognized value as raw bytes and every
// object's members in file order, so a rewrite changes only the nodes on the
// path to Sidecar's entry; whitespace is normalized to the two-space indent
// both providers themselves write.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// reportSessionVerb is the Sidecar subcommand every session-identity hook
// invokes. It doubles as the ownership sentinel: a hook entry that invokes it
// is Sidecar's to manage.
const reportSessionVerb = "report-session"

// hookTimeoutSec bounds how long a provider waits for the report command. The
// command fails open in well under a second; ten seconds is generous slack for
// a loaded machine without letting a wedged invocation stall the provider.
const hookTimeoutSec = 10

// reportSessionCommand is the exact command a provider hook runs.
//
// It is fixed argv with nothing interpolated: the provider's payload travels
// on stdin, where `--hook-stdin` reads it as bounded JSON, so no prompt, path,
// or environment content ever appears inside a shell command. The command
// deliberately relies on PATH rather than embedding an absolute binary
// location — the hook only matters inside a Sidecar-managed shell, where
// Sidecar is on PATH by construction, and an embedded path would silently
// break every hook the next time the binary moved.
func reportSessionCommand(kind string) string {
	return "sidecar agent " + reportSessionVerb + " --kind " + kind + " --hook-stdin"
}

// invokesReportSession reports whether a hook command is an invocation of
// Sidecar's report-session verb — the ownership test for config entries.
//
// The rule is deliberately narrow: the first word must be the sidecar binary
// (by base name) and the next two must be the exact subcommand. A command that
// merely *mentions* the verb — `echo sidecar agent report-session`, a wrapper
// script, `sidecar-helper agent report-session` — is not adopted, overwritten,
// or deleted. The failure direction is chosen on purpose: an exotic spelling
// of a genuine invocation is left alone (safe, at worst a duplicate report if
// one is then installed) rather than a user's similar-looking entry being
// claimed (unsafe, their configuration destroyed).
func invokesReportSession(command string) bool {
	fields := strings.Fields(command)
	if len(fields) < 3 {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(fields[0]), ".exe")
	return base == "sidecar" && fields[1] == "agent" && fields[2] == reportSessionVerb
}

// --- order- and token-preserving JSON ---

// jsonMember is one key/value pair of a JSON object, value kept as raw bytes.
type jsonMember struct {
	key string
	val json.RawMessage
}

// parseJSONFile parses a whole configuration file as a single JSON object.
// A missing or whitespace-only file parses as an empty object: there is no
// user content in it to protect, and both providers treat it the same way.
func parseJSONFile(b []byte) ([]jsonMember, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	var raw json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the top-level object")
	}
	return parseJSONObject(raw)
}

// parseJSONObject splits one JSON object into ordered members whose values are
// kept verbatim.
func parseJSONObject(raw json.RawMessage) ([]jsonMember, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	var members []jsonMember
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		members = append(members, jsonMember{key: key, val: val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return members, nil
}

// parseJSONArray splits one JSON array into elements kept verbatim.
func parseJSONArray(raw json.RawMessage) ([]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, fmt.Errorf("not a JSON array")
	}
	var items []json.RawMessage
	for dec.More() {
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		items = append(items, val)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return items, nil
}

func marshalJSONObject(members []jsonMember) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(bytes.TrimSpace(m.val))
	}
	b.WriteByte('}')
	return b.Bytes()
}

func marshalJSONArray(items []json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(bytes.TrimSpace(item))
	}
	b.WriteByte(']')
	return b.Bytes()
}

// renderJSONFile serializes the top-level members with the two-space indent
// both providers write themselves, and a trailing newline.
func renderJSONFile(top []jsonMember) []byte {
	var out bytes.Buffer
	if err := json.Indent(&out, marshalJSONObject(top), "", "  "); err != nil {
		// The members were parsed or constructed by this package, so this is
		// unreachable; returning the compact form keeps the file valid anyway.
		return append(marshalJSONObject(top), '\n')
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// lastMember finds the last member with the given key — last because that is
// the occurrence JSON consumers honor when a document carries duplicates.
func lastMember(members []jsonMember, key string) (int, bool) {
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].key == key {
			return i, true
		}
	}
	return -1, false
}

// memberString reads a member's value as a string, reporting false for a
// missing member or any other value shape.
func memberString(members []jsonMember, key string) (string, bool) {
	i, ok := lastMember(members, key)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(members[i].val, &s); err != nil {
		return "", false
	}
	return s, true
}

// canonicalJSON renders a value with sorted keys and no whitespace, so two
// spellings of the same value compare equal. An unparseable value canonicalizes
// to "", which equals nothing.
func canonicalJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// sameJSON reports semantic equality of two JSON values.
func sameJSON(a, b json.RawMessage) bool {
	ca := canonicalJSON(a)
	return ca != "" && ca == canonicalJSON(b)
}

// --- the hooks tree both providers share ---

// hookEntrySpec describes what one provider's canonical Sidecar entry looks
// like, which is everything the shared scan needs to tell "ours and current"
// from "ours and damaged" from "not ours at all", and where in that provider's
// configuration file it lives.
//
// The zero value describes the shape this scan was first written for and that
// Codex still uses: a top-level `hooks` object, an entry under `SessionStart`,
// wrapped in a matcher group, with the command in `command`. Every field below
// is a departure some later provider actually ships, and each names that
// provider, because a shape nobody ships is a shape nobody tests. The six
// combinations in the tree today are Claude and grok (grouped, `hooks`,
// SessionStart), Codex (the same without a matcher key), Copilot (flat, but
// still `hooks`/SessionStart, command in `bash`), Cursor (flat, `sessionStart`)
// and Antigravity (flat, `PreInvocation`, under a named block rather than
// `hooks`).
type hookEntrySpec struct {
	// namedBlocks reports that the file's top-level members are each a named
	// hook block holding its own events object, rather than one shared `hooks`
	// member. Antigravity CLI alone is this shape, and it is why the scan
	// carries a block coordinate at all: without walking every block, an entry
	// a user moved into a block of their own would be invisible to uninstall
	// and would keep reporting beside a freshly installed copy.
	namedBlocks bool
	// block is the member the canonical entry belongs in. Empty means `hooks`,
	// which is every provider but Antigravity, whose block is Sidecar's own
	// named one.
	block string
	// event is the canonical event key. Empty means `SessionStart`. Cursor
	// spells the same moment `sessionStart`, and Antigravity, which has no
	// session event at all, carries its conversation id on `PreInvocation`.
	event string
	// flat reports that an event's array holds handler entries directly,
	// without a matcher group wrapping them. Copilot, Cursor and Antigravity
	// are flat; Claude, Codex and grok are grouped.
	flat bool
	// commandKey is the entry member carrying the command. Empty means
	// `command`. GitHub Copilot CLI reads `bash` on Unix and `powershell` on
	// Windows instead, and writes the timeout as `timeoutSec`.
	commandKey string
	// altCommandKeys are further members the same provider would read a
	// command from, consulted only when commandKey is absent. They exist so a
	// Sidecar entry written in a spelling this build does not produce is still
	// found: Copilot's Windows `powershell` field is the case, and an entry the
	// scan cannot see is one that keeps reporting while status says nothing is
	// installed.
	altCommandKeys []string
	// matcher is the canonical group matcher, for the grouped shape only: nil
	// means the group carries no matcher key at all (Codex, grok), non-nil is
	// the exact value (Claude's "*").
	matcher *string
	// canonical maps every asset version Sidecar has ever shipped to the exact
	// entry object it shipped, newest last. An installed entry equal to an
	// older version is "outdated" rather than foreign or damaged.
	canonical []versionedEntry
}

// blockKey is the top-level member the canonical entry lives under.
func (s hookEntrySpec) blockKey() string {
	if s.block != "" {
		return s.block
	}
	return "hooks"
}

// eventKey is the event the canonical entry is registered on.
func (s hookEntrySpec) eventKey() string {
	if s.event != "" {
		return s.event
	}
	return "SessionStart"
}

// cmdKey is the entry member the provider reads the command from.
func (s hookEntrySpec) cmdKey() string {
	if s.commandKey != "" {
		return s.commandKey
	}
	return "command"
}

type versionedEntry struct {
	version string
	entry   json.RawMessage
}

func (s hookEntrySpec) current() versionedEntry {
	return s.canonical[len(s.canonical)-1]
}

// ownedHookEntry is one Sidecar-owned entry found in the hooks tree.
type ownedHookEntry struct {
	// block is the top-level member the entry was found under: the shared
	// `hooks` member for most providers, a named hook block for Antigravity.
	block string
	event string
	// group is the index of the matcher group holding the entry, or
	// flatEntryGroup when the event's array holds handlers directly.
	group int
	hook  int
	raw   json.RawMessage
	// version is the canonical asset version the entry matches, "" when the
	// entry has been modified.
	version string
	// groupCanonical reports whether the entry sits in the canonical block, on
	// the canonical event, in a group whose matcher is the canonical one — the
	// conditions under which the hook actually fires the way Sidecar qualified
	// it.
	groupCanonical bool
}

// flatEntryGroup is the group coordinate of an entry in a flat event array,
// where there is no group to index. It is negative so it can never collide
// with a real group index in the drop set.
const flatEntryGroup = -1

// hookTreeScan is one reading of a provider's hook configuration file.
type hookTreeScan struct {
	exists bool
	top    []jsonMember
	owned  []ownedHookEntry
	// parseErr names why the file cannot be safely interpreted or edited.
	// Empty means the scan is trustworthy.
	parseErr string
}

// converged reports whether the tree already holds exactly the bundled
// integration: one owned entry, byte-equivalent to the current canonical one,
// in a canonical group under SessionStart.
func (s hookTreeScan) converged(spec hookEntrySpec) bool {
	return s.parseErr == "" && len(s.owned) == 1 &&
		s.owned[0].version == spec.current().version && s.owned[0].groupCanonical
}

// scanHookTree reads the hooks tree out of a configuration file.
//
// Strictness is scoped: inside the `hooks` subtree every node is required to
// have the provider's documented shape, because an uninterpretable node could
// be hiding a Sidecar entry the scan would otherwise miss. Outside that
// subtree anything goes and is preserved verbatim.
func scanHookTree(exists bool, b []byte, spec hookEntrySpec) hookTreeScan {
	s := hookTreeScan{exists: exists}
	if !exists {
		return s
	}
	top, err := parseJSONFile(b)
	if err != nil {
		s.parseErr = "the file is not a JSON object: " + err.Error()
		return s
	}
	s.top = top

	if spec.namedBlocks {
		// Every top-level member is a named hook block, so every one of them is
		// inside the strict region and all of them are walked. Scanning only
		// Sidecar's own block would leave an entry a user moved elsewhere
		// firing beside a freshly installed copy, with uninstall unable to see
		// it.
		for _, blk := range top {
			events, err := parseJSONObject(blk.val)
			if err != nil {
				s.parseErr = fmt.Sprintf("%s is not a hook block object", blk.key)
				return s
			}
			if !s.scanBlockEvents(blk.key, events, spec) {
				return s
			}
		}
		return s
	}

	hooksIdx, ok := lastMember(top, spec.blockKey())
	if !ok {
		return s
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		s.parseErr = `the "` + spec.blockKey() + `" value is not an object`
		return s
	}
	s.scanBlockEvents(spec.blockKey(), events, spec)
	return s
}

// scanBlockEvents walks one events object, recording every Sidecar-owned entry
// under it. It reports false once it has set parseErr, so the caller stops.
func (s *hookTreeScan) scanBlockEvents(block string, events []jsonMember, spec hookEntrySpec) bool {
	for _, ev := range events {
		items, err := parseJSONArray(ev.val)
		if err != nil {
			if spec.namedBlocks {
				// A named block carries documented non-event members -- an
				// `enabled` boolean is the one Antigravity ships -- so a member
				// that is not an array is not an event and not damage.
				continue
			}
			s.parseErr = fmt.Sprintf("%s.%s is not an array", block, ev.key)
			return false
		}
		if spec.flat {
			if !s.scanEntries(block, ev.key, flatEntryGroup, items, spec, true) {
				return false
			}
			continue
		}
		for g, groupRaw := range items {
			group, err := parseJSONObject(groupRaw)
			if err != nil {
				s.parseErr = fmt.Sprintf("%s.%s[%d] is not an object", block, ev.key, g)
				return false
			}
			entriesIdx, ok := lastMember(group, "hooks")
			if !ok {
				continue
			}
			entries, err := parseJSONArray(group[entriesIdx].val)
			if err != nil {
				s.parseErr = fmt.Sprintf("%s.%s[%d].hooks is not an array", block, ev.key, g)
				return false
			}
			if !s.scanEntries(block, ev.key, g, entries, spec, groupMatcherCanonical(group, spec.matcher)) {
				return false
			}
		}
	}
	return true
}

// scanEntries records the Sidecar-owned handlers in one array of entries.
func (s *hookTreeScan) scanEntries(block, event string, group int, entries []json.RawMessage, spec hookEntrySpec, groupOK bool) bool {
	for h, entryRaw := range entries {
		entry, err := parseJSONObject(entryRaw)
		if err != nil {
			s.parseErr = fmt.Sprintf("%s.%s holds a handler that is not an object", block, event)
			return false
		}
		if !entryIsCommandHandler(entry) {
			continue
		}
		command, ok := entryCommand(entry, spec)
		if !ok || !invokesReportSession(command) {
			continue
		}
		owned := ownedHookEntry{block: block, event: event, group: group, hook: h, raw: entryRaw}
		for _, v := range spec.canonical {
			if sameJSON(entryRaw, v.entry) {
				owned.version = v.version
			}
		}
		owned.groupCanonical = block == spec.blockKey() && event == spec.eventKey() && groupOK
		s.owned = append(s.owned, owned)
	}
	return true
}

// entryIsCommandHandler reports whether a handler runs a shell command.
//
// An absent `type` counts, because it is the shape Cursor documents and the one
// Sidecar writes there: `{"command": "..."}` with nothing else. Requiring the
// key would have made Sidecar's own Cursor entry unrecognisable to the scan
// that has to find it again at uninstall.
func entryIsCommandHandler(entry []jsonMember) bool {
	typ, present := memberString(entry, "type")
	return !present || typ == "command"
}

// entryCommand reads a handler's command, from the key the provider is written
// to and then from every other key the same provider would accept.
//
// The fallbacks are deliberate and their direction matters. Copilot reads
// `bash` on Unix and `powershell` on Windows, and Sidecar writes only the Unix
// spelling, because it does not run on Windows. An entry in the other
// spelling -- a synced dotfile tree, a shared home directory, a Herdr install
// on the same account -- would otherwise be invisible to the scan, and an
// invisible Sidecar entry is one that keeps reporting while `integration
// status` says nothing is installed and uninstall has nothing to remove.
// Recognising every spelling costs nothing: ownership still turns on the
// command being an invocation of report-session, which no user's hook is.
func entryCommand(entry []jsonMember, spec hookEntrySpec) (string, bool) {
	for _, key := range append([]string{spec.cmdKey()}, spec.altCommandKeys...) {
		if command, ok := memberString(entry, key); ok {
			return command, true
		}
	}
	return "", false
}

// groupMatcherCanonical checks a group's matcher against the canonical one:
// absent when the provider takes none, exactly the canonical value otherwise.
func groupMatcherCanonical(group []jsonMember, want *string) bool {
	got, present := memberString(group, "matcher")
	if want == nil {
		if _, hasKey := lastMember(group, "matcher"); hasKey {
			return false
		}
		return true
	}
	return present && got == *want
}

// stripOwnedHookEntries removes every owned entry, dropping any group, event
// array, or hooks object left empty by the removal. Untouched nodes keep their
// original bytes; a group Sidecar merely added an entry to keeps every other
// entry and is not removed.
func stripOwnedHookEntries(s hookTreeScan, spec hookEntrySpec) ([]jsonMember, bool, error) {
	if len(s.owned) == 0 {
		return s.top, false, nil
	}
	drop := map[string]bool{}
	for _, o := range s.owned {
		drop[fmt.Sprintf("%s/%s/%d/%d", o.block, o.event, o.group, o.hook)] = true
	}
	top := append([]jsonMember(nil), s.top...)

	if spec.namedBlocks {
		var keptBlocks []jsonMember
		for _, blk := range top {
			events, err := parseJSONObject(blk.val)
			if err != nil {
				return nil, false, err
			}
			keptEvents, changed, err := stripBlockEvents(blk.key, events, drop, spec)
			if err != nil {
				return nil, false, err
			}
			switch {
			case !changed:
				keptBlocks = append(keptBlocks, blk)
			case len(keptEvents) == 0:
				// A block the removal emptied is one Sidecar's entry was the
				// whole point of, so it goes with the entry. A block that still
				// holds something of the user's -- another event, or the
				// `enabled` flag -- is kept with everything but the entry.
			default:
				keptBlocks = append(keptBlocks, jsonMember{key: blk.key, val: marshalJSONObject(keptEvents)})
			}
		}
		return keptBlocks, true, nil
	}

	hooksIdx, ok := lastMember(top, spec.blockKey())
	if !ok {
		return nil, false, fmt.Errorf("owned entries recorded but no %s member", spec.blockKey())
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		return nil, false, err
	}
	keptEvents, _, err := stripBlockEvents(spec.blockKey(), events, drop, spec)
	if err != nil {
		return nil, false, err
	}
	if len(keptEvents) == 0 {
		top = append(top[:hooksIdx], top[hooksIdx+1:]...)
	} else {
		top[hooksIdx].val = marshalJSONObject(keptEvents)
	}
	return top, true, nil
}

// stripBlockEvents removes the dropped entries from one events object,
// discarding any group or event array the removal emptied. Untouched nodes keep
// their original bytes.
func stripBlockEvents(block string, events []jsonMember, drop map[string]bool, spec hookEntrySpec) ([]jsonMember, bool, error) {
	var keptEvents []jsonMember
	blockChanged := false
	for _, ev := range events {
		items, err := parseJSONArray(ev.val)
		if err != nil {
			if spec.namedBlocks {
				keptEvents = append(keptEvents, ev)
				continue
			}
			return nil, false, err
		}
		var kept []json.RawMessage
		eventChanged := false

		if spec.flat {
			for h, entryRaw := range items {
				if drop[fmt.Sprintf("%s/%s/%d/%d", block, ev.key, flatEntryGroup, h)] {
					eventChanged = true
					continue
				}
				kept = append(kept, entryRaw)
			}
		} else {
			for g, groupRaw := range items {
				group, err := parseJSONObject(groupRaw)
				if err != nil {
					return nil, false, err
				}
				entriesIdx, ok := lastMember(group, "hooks")
				if !ok {
					kept = append(kept, groupRaw)
					continue
				}
				entries, err := parseJSONArray(group[entriesIdx].val)
				if err != nil {
					return nil, false, err
				}
				var keptEntries []json.RawMessage
				groupChanged := false
				for h, entryRaw := range entries {
					if drop[fmt.Sprintf("%s/%s/%d/%d", block, ev.key, g, h)] {
						groupChanged = true
						continue
					}
					keptEntries = append(keptEntries, entryRaw)
				}
				switch {
				case !groupChanged:
					kept = append(kept, groupRaw)
				case len(keptEntries) == 0:
					// The removal emptied the group, so the group goes with it:
					// an empty group is one Sidecar's entry was the whole point
					// of.
					eventChanged = true
				default:
					group[entriesIdx].val = marshalJSONArray(keptEntries)
					kept = append(kept, marshalJSONObject(group))
					eventChanged = true
				}
			}
		}

		switch {
		case !eventChanged:
			keptEvents = append(keptEvents, ev)
		case len(kept) == 0:
			// Drop the emptied event key entirely.
			blockChanged = true
		default:
			keptEvents = append(keptEvents, jsonMember{key: ev.key, val: marshalJSONArray(kept)})
			blockChanged = true
		}
	}
	return keptEvents, blockChanged, nil
}

// appendCanonicalEntry appends the bundled entry -- a matcher group for a
// grouped provider, the handler itself for a flat one -- to the canonical
// block's canonical event, creating the containers it needs and never
// reordering what exists.
func appendCanonicalEntry(top []jsonMember, item json.RawMessage, spec hookEntrySpec) ([]jsonMember, error) {
	top = append([]jsonMember(nil), top...)
	blockIdx, ok := lastMember(top, spec.blockKey())
	if !ok {
		events := marshalJSONObject([]jsonMember{{key: spec.eventKey(), val: marshalJSONArray([]json.RawMessage{item})}})
		return append(top, jsonMember{key: spec.blockKey(), val: events}), nil
	}
	events, err := parseJSONObject(top[blockIdx].val)
	if err != nil {
		return nil, err
	}
	if evIdx, ok := lastMember(events, spec.eventKey()); ok {
		items, err := parseJSONArray(events[evIdx].val)
		if err != nil {
			return nil, err
		}
		events[evIdx].val = marshalJSONArray(append(items, item))
	} else {
		events = append(events, jsonMember{key: spec.eventKey(), val: marshalJSONArray([]json.RawMessage{item})})
	}
	top[blockIdx].val = marshalJSONObject(events)
	return top, nil
}

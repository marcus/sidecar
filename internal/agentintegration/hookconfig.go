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
// from "ours and damaged" from "not ours at all".
type hookEntrySpec struct {
	// matcher is the canonical group matcher: nil means the group carries no
	// matcher key at all (Codex), non-nil is the exact value (Claude's "*").
	matcher *string
	// events are the hook events the canonical entry is installed under, in the
	// order the installer writes them. Empty means the single event
	// SessionStart, which is what every entry integration wrote before Devin.
	//
	// Devin is why this is a list. Its provider half does not trust any one
	// event to carry the session id -- upstream registers the same session
	// action on six of them and falls back to listing sessions when the payload
	// is silent -- so an integration that fired only on SessionStart would bind
	// the pane only when Devin happened to volunteer the id at startup.
	events []string
	// canonical maps every asset version Sidecar has ever shipped to the exact
	// entry object it shipped, newest last. An installed entry equal to an
	// older version is "outdated" rather than foreign or damaged.
	canonical []versionedEntry
}

// defaultHookEvent is the event an entry integration installs under when its
// spec names none. It is SessionStart for every provider whose session identity
// is knowable the moment a conversation opens.
const defaultHookEvent = "SessionStart"

// eventNames is the ordered event set the spec installs under.
func (s hookEntrySpec) eventNames() []string {
	if len(s.events) == 0 {
		return []string{defaultHookEvent}
	}
	return s.events
}

// hasEvent reports whether an event name is one this spec installs under.
func (s hookEntrySpec) hasEvent(name string) bool {
	for _, want := range s.eventNames() {
		if want == name {
			return true
		}
	}
	return false
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
	event string
	group int
	hook  int
	raw   json.RawMessage
	// version is the canonical asset version the entry matches, "" when the
	// entry has been modified.
	version string
	// groupCanonical reports whether the entry sits under one of the spec's own
	// events in a group whose matcher is the canonical one — the conditions
	// under which the hook actually fires the way Sidecar qualified it.
	groupCanonical bool
}

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
// integration: one owned entry per event the spec names, each byte-equivalent
// to the current canonical entry and each in a canonical group.
func (s hookTreeScan) converged(spec hookEntrySpec) bool {
	if s.parseErr != "" {
		return false
	}
	events := spec.eventNames()
	if len(s.owned) != len(events) {
		return false
	}
	count := map[string]int{}
	for _, o := range s.owned {
		if o.version != spec.current().version || !o.groupCanonical {
			return false
		}
		count[o.event]++
	}
	for _, event := range events {
		if count[event] != 1 {
			return false
		}
	}
	return true
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
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		return s
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		s.parseErr = `the "hooks" value is not an object`
		return s
	}
	for _, ev := range events {
		groups, err := parseJSONArray(ev.val)
		if err != nil {
			s.parseErr = fmt.Sprintf("hooks.%s is not an array", ev.key)
			return s
		}
		for g, groupRaw := range groups {
			group, err := parseJSONObject(groupRaw)
			if err != nil {
				s.parseErr = fmt.Sprintf("hooks.%s[%d] is not an object", ev.key, g)
				return s
			}
			entriesIdx, ok := lastMember(group, "hooks")
			if !ok {
				continue
			}
			entries, err := parseJSONArray(group[entriesIdx].val)
			if err != nil {
				s.parseErr = fmt.Sprintf("hooks.%s[%d].hooks is not an array", ev.key, g)
				return s
			}
			for h, entryRaw := range entries {
				entry, err := parseJSONObject(entryRaw)
				if err != nil {
					s.parseErr = fmt.Sprintf("hooks.%s[%d].hooks[%d] is not an object", ev.key, g, h)
					return s
				}
				typ, _ := memberString(entry, "type")
				command, ok := memberString(entry, "command")
				if typ != "command" || !ok || !invokesReportSession(command) {
					continue
				}
				owned := ownedHookEntry{event: ev.key, group: g, hook: h, raw: entryRaw}
				for _, v := range spec.canonical {
					if sameJSON(entryRaw, v.entry) {
						owned.version = v.version
					}
				}
				owned.groupCanonical = spec.hasEvent(ev.key) && groupMatcherCanonical(group, spec.matcher)
				s.owned = append(s.owned, owned)
			}
		}
	}
	return s
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
func stripOwnedHookEntries(s hookTreeScan) ([]jsonMember, bool, error) {
	if len(s.owned) == 0 {
		return s.top, false, nil
	}
	drop := map[string]bool{}
	for _, o := range s.owned {
		drop[fmt.Sprintf("%s/%d/%d", o.event, o.group, o.hook)] = true
	}
	top := append([]jsonMember(nil), s.top...)
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		return nil, false, fmt.Errorf("owned entries recorded but no hooks member")
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		return nil, false, err
	}
	var keptEvents []jsonMember
	for _, ev := range events {
		groups, err := parseJSONArray(ev.val)
		if err != nil {
			return nil, false, err
		}
		var keptGroups []json.RawMessage
		eventChanged := false
		for g, groupRaw := range groups {
			group, err := parseJSONObject(groupRaw)
			if err != nil {
				return nil, false, err
			}
			entriesIdx, ok := lastMember(group, "hooks")
			if !ok {
				keptGroups = append(keptGroups, groupRaw)
				continue
			}
			entries, err := parseJSONArray(group[entriesIdx].val)
			if err != nil {
				return nil, false, err
			}
			var keptEntries []json.RawMessage
			groupChanged := false
			for h, entryRaw := range entries {
				if drop[fmt.Sprintf("%s/%d/%d", ev.key, g, h)] {
					groupChanged = true
					continue
				}
				keptEntries = append(keptEntries, entryRaw)
			}
			switch {
			case !groupChanged:
				keptGroups = append(keptGroups, groupRaw)
			case len(keptEntries) == 0:
				// The removal emptied the group, so the group goes with it: an
				// empty group is one Sidecar's entry was the whole point of.
				eventChanged = true
			default:
				group[entriesIdx].val = marshalJSONArray(keptEntries)
				keptGroups = append(keptGroups, marshalJSONObject(group))
				eventChanged = true
			}
		}
		switch {
		case !eventChanged:
			keptEvents = append(keptEvents, ev)
		case len(keptGroups) == 0:
			// Drop the emptied event key entirely.
		default:
			keptEvents = append(keptEvents, jsonMember{key: ev.key, val: marshalJSONArray(keptGroups)})
		}
	}
	if len(keptEvents) == 0 {
		top = append(top[:hooksIdx], top[hooksIdx+1:]...)
	} else {
		top[hooksIdx].val = marshalJSONObject(keptEvents)
	}
	return top, true, nil
}

// appendCanonicalGroup appends the bundled group to hooks.SessionStart,
// creating the containers it needs and never reordering what exists.
func appendCanonicalGroup(top []jsonMember, group json.RawMessage) ([]jsonMember, error) {
	return appendCanonicalGroups(top, []string{defaultHookEvent}, group)
}

// appendCanonicalGroups appends the bundled group under each named event, in
// order, creating the containers it needs and never reordering what exists.
func appendCanonicalGroups(top []jsonMember, eventNames []string, group json.RawMessage) ([]jsonMember, error) {
	top = append([]jsonMember(nil), top...)
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		var events []jsonMember
		for _, name := range eventNames {
			events = append(events, jsonMember{key: name, val: marshalJSONArray([]json.RawMessage{group})})
		}
		return append(top, jsonMember{key: "hooks", val: marshalJSONObject(events)}), nil
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		return nil, err
	}
	for _, name := range eventNames {
		if evIdx, ok := lastMember(events, name); ok {
			groups, err := parseJSONArray(events[evIdx].val)
			if err != nil {
				return nil, err
			}
			events[evIdx].val = marshalJSONArray(append(groups, group))
			continue
		}
		events = append(events, jsonMember{key: name, val: marshalJSONArray([]json.RawMessage{group})})
	}
	top[hooksIdx].val = marshalJSONObject(events)
	return top, nil
}

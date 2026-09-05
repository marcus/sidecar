package agentintegration

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// The config.yaml line editor.
//
// Hermes loads a plugin only when its name appears in `plugins.enabled` in
// <hermes home>/config.yaml, so dropping the two plugin files is half an
// install. That file is the user's -- it holds their model, their provider
// choices, their gateway credentials and six hundred lines of settings -- so
// Sidecar owns one line inside it (Ownership OwnsEntry) and the whole of this
// file is about touching exactly that line and nothing else.
//
// The contract is Codex's and Kimi's, restated for YAML:
//
//   - The edit is line surgery. Nothing outside the one list item is read for
//     meaning, reordered, reindented, or rewritten, so the user's comments,
//     anchors, quoting style and key order all survive an install and an
//     uninstall byte for byte.
//   - A YAML parser is used as a read-only oracle at both ends of every edit and
//     never to serialize anything. A file that is not valid YAML is refused
//     outright, and the composed image is compared semantically against its
//     pre-image before a single operation is emitted, so a rewrite that would not
//     verify produces a refusal with an empty op list rather than a partial
//     change on disk. Serializing through the parser instead would rewrite the
//     whole file, which is what Hermes's own `config set` does and what makes
//     that command lose every comment in the file.
//   - Only the shapes Hermes itself writes are edited. A flow sequence
//     (`enabled: [a, b]`) is refused rather than rewritten as a block, because
//     rewriting it would edit bytes outside Sidecar's own entry.
//
// # Ownership is the item's value, not a marker comment, and that is measured
//
// Every other file-owning asset Sidecar ships proves ownership from a marker
// comment its own bytes carry. That rule cannot hold here: `hermes config set`,
// `hermes plugins enable` and the setup wizard all round-trip config.yaml
// through yaml.dump, which drops every comment in the file. So a marker on
// Sidecar's line would survive exactly until the next time the user ran an
// unrelated Hermes command, and after that an uninstall could not find its own
// entry.
//
// The line is therefore owned by its value. `sidecar-agent-state` names the
// plugin directory Sidecar installs and nothing else: Hermes matches the enable
// list against a plugin's directory key or its manifest name, both of which are
// Sidecar's own asset. This is the same rule the Antigravity, Copilot and Cursor
// adapters use for their entries -- ownership by what the entry says, which is
// what [OwnsEntry] means -- rather than a weaker version of the file rule.
//
// A marker comment IS still written on the line, and it is decoration with a
// purpose: a user reading their own config.yaml is entitled to know which tool
// added a line to it. Nothing depends on the comment surviving, and the tests
// drive both a file that has it and a file Hermes has since stripped.

// hermesMarkerPrefix introduces the trailing comment on Sidecar's enable line.
//
// It carries markerToken, the ownership sentinel every Sidecar-owned region in
// every provider tree carries; only the comment character differs, because YAML
// has no `//`.
const hermesMarkerPrefix = "# " + markerToken

// hermesEnableLine renders the line Sidecar writes, at a given indent.
func hermesEnableLine(indent int, version string) string {
	return fmt.Sprintf("%s- %s %s id=%s schema=%d version=%s",
		strings.Repeat(" ", indent), HermesPluginName,
		hermesMarkerPrefix, HermesSource, HermesAssetSchema, version)
}

// hermesConfigShape names what the `plugins` key in a config.yaml looks like.
type hermesConfigShape string

const (
	// hermesNoPluginsKey: the file has no top-level `plugins` key at all, which
	// is what a Hermes that has never enabled a plugin looks like.
	hermesNoPluginsKey hermesConfigShape = "no-plugins-key"
	// hermesNoEnabledKey: `plugins:` exists as a mapping with no `enabled` in it.
	hermesNoEnabledKey hermesConfigShape = "no-enabled-key"
	// hermesEmptyEnabled: `enabled:` is present and holds nothing -- either
	// `enabled: []` or a key with no items under it.
	hermesEmptyEnabled hermesConfigShape = "empty-enabled"
	// hermesBlockList: `enabled:` holds a block sequence, which is the shape
	// Hermes writes and the only one with items to insert beside.
	hermesBlockList hermesConfigShape = "block-list"
	// hermesUneditableShape: anything else -- a flow sequence, a scalar, a
	// `plugins` that is not a mapping. Refused rather than rewritten.
	hermesUneditableShape hermesConfigShape = "uneditable"
)

// hermesConfig is one config.yaml read for editing.
type hermesConfig struct {
	lines []string
	// trailingNewline records whether the file ended in one, so a rewrite gives
	// it back exactly as it was.
	trailingNewline bool
	shape           hermesConfigShape
	// enabledIndex is the line holding `enabled:`, or -1.
	enabledIndex int
	// enabledIndent is the column `enabled:` starts at.
	enabledIndent int
	// itemIndent is the column existing list items start at, or the column a
	// first item would be written at.
	itemIndent int
	// listEnd is one past the last line belonging to the enabled sequence.
	listEnd int
	// ownedIndex is the line carrying Sidecar's item, or -1.
	ownedIndex int
	// ownedVersion is the version its marker comment declares, empty when the
	// comment is gone. An item with no comment is still Sidecar's; see the
	// header.
	ownedVersion string
	// pluginsIndex is the line holding `plugins:`, or -1.
	pluginsIndex int
	// pluginsEnd is one past the last line belonging to the plugins block.
	pluginsEnd int
}

// errHermesConfigInvalid is returned for a config.yaml that does not parse.
var errHermesConfigInvalid = errors.New("config.yaml is not valid YAML")

// errHermesConfigShape is returned for a `plugins` key in a shape this editor
// will not touch.
var errHermesConfigShape = errors.New("the plugins key is in a shape Sidecar will not rewrite")

// readHermesConfig parses one config.yaml for editing.
//
// An empty file is valid and is the common case: Hermes creates config.yaml
// lazily, and a home where nothing has been configured has an empty one or none
// at all.
func readHermesConfig(content string) (hermesConfig, error) {
	var doc any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return hermesConfig{}, fmt.Errorf("%w: %v", errHermesConfigInvalid, err)
	}

	cfg := hermesConfig{
		trailingNewline: strings.HasSuffix(content, "\n"),
		enabledIndex:    -1,
		ownedIndex:      -1,
		pluginsIndex:    -1,
		itemIndent:      4,
	}
	if content != "" {
		cfg.lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	}

	cfg.pluginsIndex = hermesTopLevelKey(cfg.lines, "plugins")
	if cfg.pluginsIndex < 0 {
		cfg.shape = hermesNoPluginsKey
		cfg.pluginsEnd = len(cfg.lines)
		cfg.listEnd = len(cfg.lines)
		return cfg, nil
	}
	// A `plugins:` line carrying a value is a flow sequence or a scalar; either
	// way the block form below is not what is there.
	if rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cfg.lines[cfg.pluginsIndex]), "plugins:")); rest != "" && !strings.HasPrefix(rest, "#") {
		cfg.shape = hermesUneditableShape
		return cfg, nil
	}
	cfg.pluginsEnd = hermesBlockEnd(cfg.lines, cfg.pluginsIndex+1, 0)

	cfg.enabledIndex = hermesNestedKey(cfg.lines, cfg.pluginsIndex+1, cfg.pluginsEnd, "enabled")
	if cfg.enabledIndex < 0 {
		// `plugins:` with something else under it, or with nothing at all. A
		// bare `plugins:` followed by a list is a shape Hermes never writes and
		// Sidecar does not invent a reading for.
		for i := cfg.pluginsIndex + 1; i < cfg.pluginsEnd; i++ {
			if hermesListItem(cfg.lines[i]) != "" {
				cfg.shape = hermesUneditableShape
				return cfg, nil
			}
		}
		cfg.shape = hermesNoEnabledKey
		cfg.listEnd = cfg.pluginsIndex + 1
		return cfg, nil
	}
	cfg.enabledIndent = hermesIndent(cfg.lines[cfg.enabledIndex])
	cfg.itemIndent = cfg.enabledIndent + 2

	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cfg.lines[cfg.enabledIndex]), "enabled:"))
	switch {
	case rest == "[]":
		cfg.shape = hermesEmptyEnabled
		cfg.listEnd = cfg.enabledIndex + 1
		return cfg, nil
	case rest != "" && !strings.HasPrefix(rest, "#"):
		// A flow sequence with items, or a scalar. Rewriting it as a block list
		// would edit bytes outside Sidecar's entry, so it is refused instead.
		cfg.shape = hermesUneditableShape
		return cfg, nil
	}

	// Consume the sequence. PyYAML writes an indentless sequence -- items at the
	// same column as their key -- and a hand-written file usually indents them,
	// so both are accepted and whichever is there is matched when inserting.
	cfg.listEnd = cfg.enabledIndex + 1
	found := false
	for i := cfg.enabledIndex + 1; i < cfg.pluginsEnd; i++ {
		line := cfg.lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			// A blank or a comment inside the run belongs to it only if the run
			// continues past it; trailing ones stay with whatever follows.
			continue
		}
		value := hermesListItem(line)
		if value == "" || hermesIndent(line) < cfg.enabledIndent {
			break
		}
		if !found {
			cfg.itemIndent = hermesIndent(line)
			found = true
		}
		cfg.listEnd = i + 1
		if hermesItemValue(value) == HermesPluginName {
			cfg.ownedIndex = i
			cfg.ownedVersion = hermesItemVersion(line)
		}
	}
	if !found {
		cfg.shape = hermesEmptyEnabled
		cfg.listEnd = cfg.enabledIndex + 1
		return cfg, nil
	}
	cfg.shape = hermesBlockList
	return cfg, nil
}

// Installed reports whether Sidecar's enable line is present.
func (c hermesConfig) Installed() bool { return c.ownedIndex >= 0 }

// Editable reports whether an install or uninstall may touch this file.
func (c hermesConfig) Editable() bool { return c.shape != hermesUneditableShape }

// WithEntry returns the file with Sidecar's enable line present.
//
// The item goes at the END of an existing list, which is the repository's own
// convention for a shared list and also the least surprising place: a user
// scanning their enabled plugins sees their own choices in the order they made
// them, with the one a tool added last.
func (c hermesConfig) WithEntry(version string) (string, error) {
	if !c.Editable() {
		return "", errHermesConfigShape
	}
	if c.Installed() {
		return c.render(c.lines), nil
	}
	line := hermesEnableLine(c.itemIndent, version)
	var out []string
	switch c.shape {
	case hermesNoPluginsKey:
		out = append(out, c.lines...)
		// A file that does not end in a newline would otherwise have the new key
		// welded onto its last line.
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		out = append(out, "plugins:", "  enabled:", hermesEnableLine(4, version))
	case hermesNoEnabledKey:
		out = append(out, c.lines[:c.pluginsIndex+1]...)
		out = append(out, strings.Repeat(" ", 2)+"enabled:", hermesEnableLine(4, version))
		out = append(out, c.lines[c.pluginsIndex+1:]...)
	case hermesEmptyEnabled:
		out = append(out, c.lines[:c.enabledIndex]...)
		out = append(out, strings.Repeat(" ", c.enabledIndent)+"enabled:", line)
		out = append(out, c.lines[c.enabledIndex+1:]...)
	case hermesBlockList:
		out = append(out, c.lines[:c.listEnd]...)
		out = append(out, line)
		out = append(out, c.lines[c.listEnd:]...)
	}
	return c.render(out), nil
}

// WithoutEntry returns the file with Sidecar's enable line removed and every
// other byte where it was.
//
// The `enabled:` key and the `plugins:` mapping are left behind even when
// Sidecar's line was all they held, and that is a deliberate choice between two
// imperfect answers. A user who ran `hermes plugins enable sidecar-agent-state`
// by hand ends at bytes identical to the ones an install produces, so "did
// Sidecar create this key" is not decidable from the file. Leaving an inert
// two-line stub a user can delete is the recoverable error; deleting keys the
// user wrote is not.
//
// One line outside Sidecar's own is touched, and only in one case: when removing
// the item empties the sequence, the `enabled:` line is rewritten as
// `enabled: []`. A key with nothing under it is null rather than an empty list,
// and `enabled: []` is exactly what Hermes's own `plugins disable` writes when
// the last plugin goes, so this is the shape that both round-trips a file that
// started as `enabled: []` byte for byte and leaves a file Hermes recognises.
func (c hermesConfig) WithoutEntry() (string, error) {
	if !c.Editable() {
		return "", errHermesConfigShape
	}
	if !c.Installed() {
		return c.render(c.lines), nil
	}
	out := append([]string(nil), c.lines[:c.ownedIndex]...)
	out = append(out, c.lines[c.ownedIndex+1:]...)
	if c.remainingItems() == 0 && c.enabledIndex >= 0 {
		out[c.enabledIndex] = strings.Repeat(" ", c.enabledIndent) + "enabled: []"
	}
	return c.render(out), nil
}

// remainingItems counts the sequence entries that are not Sidecar's.
func (c hermesConfig) remainingItems() int {
	if c.shape != hermesBlockList {
		return 0
	}
	n := 0
	for i := c.enabledIndex + 1; i < c.listEnd; i++ {
		if i == c.ownedIndex {
			continue
		}
		if hermesListItem(c.lines[i]) != "" {
			n++
		}
	}
	return n
}

func (c hermesConfig) render(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := strings.Join(lines, "\n")
	if c.trailingNewline || c.shape == hermesNoPluginsKey {
		out += "\n"
	}
	return out
}

// hermesVerify is the read-only oracle at the far end of an edit.
//
// It decodes both images and requires that the ONLY difference between them is
// `plugins.enabled` holding, or not holding, Sidecar's own item. Anything else
// -- a key that moved, a value that changed type, a list that lost an entry --
// fails, and the caller turns that into a refusal rather than a partial write.
func hermesVerify(before, after string, want bool) error {
	var pre, post map[string]any
	if err := yaml.Unmarshal([]byte(before), &pre); err != nil {
		return fmt.Errorf("%w: %v", errHermesConfigInvalid, err)
	}
	if err := yaml.Unmarshal([]byte(after), &post); err != nil {
		return fmt.Errorf("composing the edit produced invalid YAML: %v", err)
	}
	expected := hermesExpected(pre, want)
	// An `enabled:` key holding nothing decodes as nil, and one written
	// `enabled: []` decodes as an empty slice. Removing the last item turns the
	// first into the second's shape on one side of the comparison and not the
	// other, and the difference is not one this edit is responsible for, so both
	// sides are normalised at that one key. Nowhere else is normalised, because
	// nowhere else does this editor write a line.
	hermesNormaliseEmptyList(post)
	hermesNormaliseEmptyList(expected)
	if !reflect.DeepEqual(post, expected) {
		return fmt.Errorf("the composed config.yaml does not match the one intended: got %#v, want %#v", post, expected)
	}
	return nil
}

// hermesNormaliseEmptyList collapses an empty `plugins.enabled` to nil.
func hermesNormaliseEmptyList(doc map[string]any) {
	plugins, ok := doc["plugins"].(map[string]any)
	if !ok {
		return
	}
	if list, isList := plugins["enabled"].([]any); isList && len(list) == 0 {
		plugins["enabled"] = nil
	}
}

// hermesExpected builds the document the edit is supposed to produce.
func hermesExpected(pre map[string]any, want bool) map[string]any {
	out := map[string]any{}
	for k, v := range pre {
		out[k] = v
	}
	plugins := map[string]any{}
	if existing, ok := out["plugins"].(map[string]any); ok {
		for k, v := range existing {
			plugins[k] = v
		}
	}
	var list []any
	if existing, ok := plugins["enabled"].([]any); ok {
		for _, item := range existing {
			if s, isString := item.(string); isString && s == HermesPluginName {
				continue
			}
			list = append(list, item)
		}
	}
	if want {
		list = append(list, HermesPluginName)
	}
	if list == nil {
		// An `enabled: []` in the source decodes to an empty non-nil slice and
		// an absent key decodes to nothing; both end here when the only item
		// was Sidecar's, and the composed file keeps whichever the source had.
		if _, had := plugins["enabled"]; had {
			list = []any{}
		}
	}
	if list != nil {
		plugins["enabled"] = list
	}
	if len(plugins) > 0 || pre["plugins"] != nil {
		out["plugins"] = plugins
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// readFileString reads a configuration file Sidecar is going to edit.
//
// It is bounded by the same maxAssetBytes every other read in this package is.
// A config.yaml larger than that is not one Sidecar will rewrite: reading it in
// full to discover so would be the bug, and Hermes's own configuration is tens
// of kilobytes.
func readFileString(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxAssetBytes {
		return "", fmt.Errorf("%s is larger than any configuration Sidecar will edit", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// hermesIndent is the column a line's first non-space character sits at, or -1
// for a line that is entirely whitespace.
func hermesIndent(line string) int {
	for i, r := range line {
		if r != ' ' {
			return i
		}
	}
	return -1
}

// hermesTopLevelKey finds a mapping key at column zero.
func hermesTopLevelKey(lines []string, key string) int {
	for i, line := range lines {
		if hermesIndent(line) != 0 {
			continue
		}
		if strings.HasPrefix(line, key+":") {
			return i
		}
	}
	return -1
}

// hermesBlockEnd returns one past the last line belonging to a block whose key
// sits at the given indent.
func hermesBlockEnd(lines []string, from, indent int) int {
	for i := from; i < len(lines); i++ {
		at := hermesIndent(lines[i])
		if at < 0 || strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			continue
		}
		if at <= indent && hermesListItem(lines[i]) == "" {
			return i
		}
	}
	return len(lines)
}

// hermesNestedKey finds a mapping key inside a range.
func hermesNestedKey(lines []string, from, to int, key string) int {
	for i := from; i < to && i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if hermesIndent(lines[i]) > 0 && strings.HasPrefix(trimmed, key+":") {
			return i
		}
	}
	return -1
}

// hermesListItem returns the text after a `- ` on a block sequence item, or "".
func hermesListItem(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "-" {
		return ""
	}
	rest, ok := strings.CutPrefix(trimmed, "- ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(rest)
}

// hermesItemValue strips a trailing comment from a list item's text.
func hermesItemValue(item string) string {
	if idx := strings.Index(item, " #"); idx >= 0 {
		item = item[:idx]
	}
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(item), `"'`))
}

// hermesItemVersion reads the version out of the marker comment on a line, or
// returns "" when the comment is not there.
//
// A missing comment is not a missing owner; see the header. It costs the status
// surface the ability to call an installed line outdated, which is why the two
// plugin FILES carry the version too and are what an update actually replaces.
func hermesItemVersion(line string) string {
	idx := strings.Index(line, hermesMarkerPrefix)
	if idx < 0 {
		return ""
	}
	id, schema, version, ok := parseMarkerAt(line[idx:], hermesMarkerPrefix)
	if !ok || id != HermesSource || schema != HermesAssetSchema {
		return ""
	}
	return version
}

package pluginhost

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/marcus/sidecar/internal/resource"
)

// ValidateDescribe checks a plugin-protocol describe response whole: the
// identity block and matchers by the frozen resource rules, then the
// collections and actions this protocol adds.
//
// It is all-or-nothing for the same reason ValidateDescription is. A plugin
// that declares one uncompilable pattern, one unrenderable collection, or one
// watch path outside the user's home has a bug the user needs to see, and
// publishing the rest of its declarations would hide that while changing what
// the scanner recognises and what the host is watching on disk. The refusal is
// a *TransportError with ReasonInvalidDescribe, which `sidecar plugin list
// --describe` prints verbatim.
//
// home is the user's home directory, used to bound watch paths. An empty home
// means no watch path can be validated, so any declared one is refused.
func ValidateDescribe(instance string, resp *Response, home string) (Description, error) {
	if resp == nil {
		return Description{}, describeFail(instance, "describe returned no response")
	}
	desc, err := ValidateDescription(instance, resp.identity(), resp.Matchers)
	if err != nil {
		return Description{}, err
	}

	desc.Context = validateContextKinds(resp.Context)

	collections, err := validateCollections(instance, resp.Collections, home)
	if err != nil {
		return Description{}, err
	}
	desc.Collections = collections

	actions, err := validateActions(instance, resp.Actions, collections)
	if err != nil {
		return Description{}, err
	}
	desc.Actions = actions

	return desc, nil
}

func describeFail(instance, format string, args ...any) error {
	return &TransportError{
		Instance: instance,
		Method:   MethodDescribe,
		Reason:   ReasonInvalidDescribe,
		Detail:   fmt.Sprintf(format, args...),
	}
}

// validateContextKinds keeps the kinds this host knows and drops the rest. An
// unknown kind is dropped rather than refused because it is the one
// forward-compatible declaration in describe: a plugin written against a later
// protocol may name a kind this host has never heard of, and since the host
// only ever sends kinds it knows, an unknown one costs nothing.
func validateContextKinds(declared []string) []ContextKind {
	if len(declared) == 0 {
		return nil
	}
	var out []ContextKind
	seen := make(map[ContextKind]bool, len(declared))
	for _, raw := range declared {
		kind := CoerceContextKind(raw)
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out
}

func validateCollections(instance string, wire []WireCollection, home string) ([]Collection, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > MaxCollections {
		return nil, describeFail(instance, "declared %d collections, the limit is %d", len(wire), MaxCollections)
	}

	seen := make(map[string]bool, len(wire))
	out := make([]Collection, 0, len(wire))
	for i, w := range wire {
		id, err := storableID(instance, "collection", i, w.ID, MaxCollectionIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "collection id %q is declared more than once", id)
		}
		seen[id] = true

		c := Collection{
			ID:      id,
			Title:   resource.SanitizeLine(w.Title, MaxCollectionTitle),
			Search:  CoerceSearchMode(w.Search),
			Detail:  w.Detail == nil || *w.Detail,
			Context: validateContextKinds(w.Context),
		}
		if c.Title == "" {
			c.Title = id
		}

		columns, err := validateColumns(instance, id, w.Columns)
		if err != nil {
			return nil, err
		}
		c.Columns = columns

		views, err := validateViews(instance, id, w.Views)
		if err != nil {
			return nil, err
		}
		c.Views = views

		sortKeys, err := validateSortKeys(instance, id, w.Sort)
		if err != nil {
			return nil, err
		}
		c.Sort = sortKeys

		filters, err := validateFilters(instance, id, w.Filters)
		if err != nil {
			return nil, err
		}
		c.Filters = filters

		refresh, err := validateRefresh(instance, id, w.Refresh, home)
		if err != nil {
			return nil, err
		}
		c.Refresh = refresh

		out = append(out, c)
	}

	// The watch bound is per plugin, not per collection: eight paths is what
	// the host will watch on this plugin's behalf however they are spread.
	watched := make(map[string]bool)
	for _, c := range out {
		for _, path := range c.Refresh.Watch {
			watched[path] = true
		}
	}
	if len(watched) > MaxWatchPaths {
		return nil, describeFail(instance, "declared %d distinct watch paths, the limit is %d", len(watched), MaxWatchPaths)
	}

	return out, nil
}

func validateColumns(instance, collection string, wire []WireColumn) ([]Column, error) {
	if len(wire) == 0 {
		return nil, describeFail(instance, "collection %q declares no columns", collection)
	}
	if len(wire) > MaxColumns {
		return nil, describeFail(instance, "collection %q declares %d columns, the limit is %d", collection, len(wire), MaxColumns)
	}

	seen := make(map[string]bool, len(wire))
	out := make([]Column, 0, len(wire))
	primary, secondary := -1, -1
	for i, w := range wire {
		id, err := storableID(instance, "collection "+collection+" column", i, w.ID, MaxColumnIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "collection %q declares column id %q more than once", collection, id)
		}
		seen[id] = true

		col := Column{
			ID:    id,
			Label: resource.SanitizeLine(w.Label, MaxColumnLabelChars),
			Align: CoerceAlign(w.Align),
			Kind:  CoerceColumnKind(w.Kind),
		}
		if col.Label == "" {
			col.Label = id
		}
		if w.Width > 0 {
			col.Width = min(w.Width, MaxColumnWidth)
		}
		// A second primary or secondary is dropped rather than refused: the
		// host needs exactly one of each to lay a narrow row out, and the first
		// declared is the one the plugin listed first.
		if w.Primary && primary < 0 {
			primary = i
		}
		if w.Secondary && secondary < 0 && primary != i {
			secondary = i
		}
		out = append(out, col)
	}
	if primary < 0 {
		primary = 0
	}
	out[primary].Primary = true
	if secondary >= 0 && secondary != primary {
		out[secondary].Secondary = true
	}
	return out, nil
}

func validateViews(instance, collection string, wire []WireView) ([]View, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > MaxViews {
		return nil, describeFail(instance, "collection %q declares %d views, the limit is %d", collection, len(wire), MaxViews)
	}
	seen := make(map[string]bool, len(wire))
	out := make([]View, 0, len(wire))
	for i, w := range wire {
		id, err := storableID(instance, "collection "+collection+" view", i, w.ID, MaxCollectionIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "collection %q declares view id %q more than once", collection, id)
		}
		seen[id] = true
		title := resource.SanitizeLine(w.Title, MaxCollectionTitle)
		if title == "" {
			title = id
		}
		out = append(out, View{ID: id, Title: title})
	}
	return out, nil
}

func validateSortKeys(instance, collection string, wire []WireSortKey) ([]SortKey, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > MaxSortKeys {
		return nil, describeFail(instance, "collection %q declares %d sort keys, the limit is %d", collection, len(wire), MaxSortKeys)
	}
	seen := make(map[string]bool, len(wire))
	out := make([]SortKey, 0, len(wire))
	defaulted := false
	for i, w := range wire {
		id, err := storableID(instance, "collection "+collection+" sort key", i, w.ID, MaxCollectionIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "collection %q declares sort key %q more than once", collection, id)
		}
		seen[id] = true
		label := resource.SanitizeLine(w.Label, MaxColumnLabelChars)
		if label == "" {
			label = id
		}
		key := SortKey{ID: id, Label: label, Default: CoerceSortDir(w.Default)}
		// One default sort, not several: the second is dropped, because a
		// picker cannot open on two keys at once.
		if key.Default != "" {
			if defaulted {
				key.Default = ""
			} else {
				defaulted = true
			}
		}
		out = append(out, key)
	}
	return out, nil
}

// validateFilters checks a collection's declared choosers.
//
// It refuses rather than repairs, like the rest of describe. A filter is a
// control the host draws and a value the host persists and sends back: an
// unknown kind would be drawn as the wrong control, a choice filter with no
// choices is a menu with nothing in it, and a default naming an option that
// does not exist would open the control on a value the plugin never declared
// and then send it back on every list. Each of those is a bug in the plugin
// that the author has to see, not one to publish half of.
func validateFilters(instance, collection string, wire []WireFilter) ([]Filter, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > MaxFilters {
		return nil, describeFail(instance, "collection %q declares %d filters, the limit is %d", collection, len(wire), MaxFilters)
	}
	seen := make(map[string]bool, len(wire))
	out := make([]Filter, 0, len(wire))
	for i, w := range wire {
		id, err := storableID(instance, "collection "+collection+" filter", i, w.ID, MaxFilterIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "collection %q declares filter id %q more than once", collection, id)
		}
		seen[id] = true

		kind := CoerceFilterKind(w.Kind)
		if kind == "" {
			return nil, describeFail(instance, "collection %q filter %q declares kind %q, which is not one of choice or text", collection, id, w.Kind)
		}
		f := Filter{ID: id, Kind: kind, Label: resource.SanitizeLine(w.Label, MaxFilterTitleChars)}
		if f.Label == "" {
			f.Label = id
		}

		switch kind {
		case FilterChoice:
			if len(w.Choices) == 0 {
				return nil, describeFail(instance, "collection %q filter %q is a choice with no choices", collection, id)
			}
			if len(w.Choices) > MaxFilterChoices {
				return nil, describeFail(instance, "collection %q filter %q declares %d choices, the limit is %d", collection, id, len(w.Choices), MaxFilterChoices)
			}
			choiceSeen := make(map[string]bool, len(w.Choices))
			for j, wc := range w.Choices {
				choiceID, err := storableID(instance, "collection "+collection+" filter "+id+" choice", j, wc.ID, MaxFilterIDChars)
				if err != nil {
					return nil, err
				}
				if choiceSeen[choiceID] {
					return nil, describeFail(instance, "collection %q filter %q declares choice id %q more than once", collection, id, choiceID)
				}
				choiceSeen[choiceID] = true
				title := resource.SanitizeLine(wc.Title, MaxFilterTitleChars)
				if title == "" {
					title = choiceID
				}
				f.Choices = append(f.Choices, FilterOption{ID: choiceID, Title: title})
			}
			f.Default = strings.TrimSpace(w.Default)
			if f.Default == "" {
				// No stated default is the first declared option: a radio group
				// has to open on something, and the plugin's own order says
				// which.
				f.Default = f.Choices[0].ID
			} else if !choiceSeen[f.Default] {
				return nil, describeFail(instance, "collection %q filter %q defaults to %q, which is not one of its choices", collection, id, f.Default)
			}
		case FilterText:
			if len(w.Choices) > 0 {
				return nil, describeFail(instance, "collection %q filter %q is text and declares choices", collection, id)
			}
			f.Default = resource.SanitizeLine(w.Default, MaxFilterValueChars)
			if f.Default != strings.TrimSpace(w.Default) {
				return nil, describeFail(instance, "collection %q filter %q declares a default the host cannot store verbatim", collection, id)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// validateRefresh clamps the poll interval and validates the watch paths.
//
// A watch path is a directory Sidecar's own file watcher will hold open for as
// long as a tab from this plugin is on screen, so the bound is not cosmetic: an
// unexpanded "~", a relative path, or "/" would either watch nothing or watch a
// whole disk. Paths must expand to an absolute location under the user's home
// directory, and a plugin that names one outside it is refused by name.
func validateRefresh(instance, collection string, wire *WireRefresh, home string) (Refresh, error) {
	if wire == nil {
		return Refresh{}, nil
	}
	out := Refresh{EverySeconds: ClampRefreshSeconds(wire.EverySeconds)}
	if len(wire.Watch) == 0 {
		return out, nil
	}
	if len(wire.Watch) > MaxWatchPaths {
		return Refresh{}, describeFail(instance, "collection %q declares %d watch paths, the limit is %d", collection, len(wire.Watch), MaxWatchPaths)
	}
	for _, raw := range wire.Watch {
		path, err := validateWatchPath(instance, collection, raw, home)
		if err != nil {
			return Refresh{}, err
		}
		out.Watch = append(out.Watch, path)
	}
	return out, nil
}

func validateWatchPath(instance, collection, raw, home string) (string, error) {
	path := resource.SanitizeLine(raw, resource.MaxURLChars)
	if path == "" {
		return "", describeFail(instance, "collection %q declares an empty watch path", collection)
	}
	if home == "" {
		return "", describeFail(instance, "collection %q declares a watch path but this host has no home directory to bound it to", collection)
	}
	switch {
	case path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", describeFail(instance, "collection %q watch path %q is not absolute", collection, raw)
	}
	path = filepath.Clean(path)
	// The home directory itself is refused along with everything above it:
	// watching a whole home directory is watching a whole disk with extra
	// steps, and no plugin needs it.
	cleanHome := filepath.Clean(home)
	if path == cleanHome || !strings.HasPrefix(path, cleanHome+string(filepath.Separator)) {
		return "", describeFail(instance, "collection %q watch path %q is not inside the home directory", collection, raw)
	}
	return path, nil
}

func validateActions(instance string, wire []WireAction, collections []Collection) ([]Action, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > MaxActions {
		return nil, describeFail(instance, "declared %d actions, the limit is %d", len(wire), MaxActions)
	}
	known := make(map[string]bool, len(collections))
	for _, c := range collections {
		known[c.ID] = true
	}

	seen := make(map[string]bool, len(wire))
	out := make([]Action, 0, len(wire))
	for i, w := range wire {
		id, err := storableID(instance, "action", i, w.ID, MaxActionIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "action id %q is declared more than once", id)
		}
		seen[id] = true

		target := CoerceActionTarget(w.On)
		if target == "" {
			return nil, describeFail(instance, "action %q declares target %q, which is not one of item, collection, resource, or global", id, w.On)
		}

		action := Action{ID: id, On: target, Mutates: w.Mutates}
		action.Title = resource.SanitizeLine(w.Title, MaxActionTitleChars)
		if action.Title == "" {
			action.Title = id
		}

		switch target {
		case ActionOnItem, ActionOnCollection:
			collection := strings.TrimSpace(w.Collection)
			if collection == "" {
				return nil, describeFail(instance, "action %q targets %s but names no collection", id, target)
			}
			if !known[collection] {
				return nil, describeFail(instance, "action %q names collection %q, which is not declared", id, collection)
			}
			action.Collection = collection
		default:
			// Absent for resource and global. A stray value is cleared rather
			// than refused: it changes nothing about what the action does.
			action.Collection = ""
		}

		inputs, err := validateActionInputs(instance, id, w.Inputs)
		if err != nil {
			return nil, err
		}
		action.Inputs = inputs

		if w.Confirm != nil {
			action.Confirm = *w.Confirm
		} else {
			// A mutating action with no inputs has nothing else standing
			// between the keystroke and the change, so it confirms. One with
			// inputs does not: its form is the confirm step.
			action.Confirm = w.Mutates && len(inputs) == 0
		}

		action.Key = requestedKey(w.Key)
		out = append(out, action)
	}
	return out, nil
}

func validateActionInputs(instance, action string, wire []WireActionInput) ([]ActionInput, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > MaxActionInputs {
		return nil, describeFail(instance, "action %q declares %d inputs, the limit is %d", action, len(wire), MaxActionInputs)
	}
	seen := make(map[string]bool, len(wire))
	out := make([]ActionInput, 0, len(wire))
	for i, w := range wire {
		id, err := storableID(instance, "action "+action+" input", i, w.ID, MaxActionIDChars)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, describeFail(instance, "action %q declares input id %q more than once", action, id)
		}
		seen[id] = true

		kind := CoerceInputKind(w.Kind)
		if kind == "" {
			// Refused rather than degraded to text: a field the host draws as
			// the wrong type collects the wrong value and the plugin acts on it.
			return nil, describeFail(instance, "action %q input %q declares kind %q, which is not one of text, multiline, choice, or confirm", action, id, w.Kind)
		}
		input := ActionInput{
			ID:       id,
			Label:    resource.SanitizeLine(w.Label, MaxInputLabelChars),
			Kind:     kind,
			Required: w.Required,
			Default:  resource.SanitizeLine(w.Default, MaxInputDefaultChars),
		}
		if input.Label == "" {
			input.Label = id
		}
		if kind == InputChoice {
			if len(w.Choices) == 0 {
				return nil, describeFail(instance, "action %q input %q is a choice with no choices", action, id)
			}
			if len(w.Choices) > MaxActionChoices {
				return nil, describeFail(instance, "action %q input %q declares %d choices, the limit is %d", action, id, len(w.Choices), MaxActionChoices)
			}
			for _, choice := range w.Choices {
				clean := resource.SanitizeLine(choice, MaxInputDefaultChars)
				if clean == "" {
					continue
				}
				input.Choices = append(input.Choices, clean)
			}
			if len(input.Choices) == 0 {
				return nil, describeFail(instance, "action %q input %q has no usable choices", action, id)
			}
		}
		out = append(out, input)
	}
	return out, nil
}

// requestedKey keeps a single lowercase ASCII letter and drops everything else.
// The key is a request the host may refuse later against its reserved set and
// the surface's own bindings; this only rejects what could never be granted.
func requestedKey(v string) string {
	v = strings.TrimSpace(v)
	if len(v) != 1 || v[0] < 'a' || v[0] > 'z' {
		return ""
	}
	return v
}

// storableID applies the rule matcher IDs already follow: an ID is persisted in
// pane state, so accepting a sanitized rewrite would orphan saved tabs the next
// time the plugin sends the original. A value the host cannot store verbatim is
// refused rather than repaired.
func storableID(instance, what string, index int, raw string, maxChars int) (string, error) {
	id := resource.SanitizeLine(raw, maxChars)
	if id == "" {
		return "", describeFail(instance, "%s %d has no id", what, index)
	}
	if id != strings.TrimSpace(raw) {
		return "", describeFail(instance, "%s id %q cannot be stored verbatim", what, id)
	}
	if utf8.RuneCountInString(id) > maxChars {
		return "", describeFail(instance, "%s id %q is longer than %d characters", what, id, maxChars)
	}
	return id, nil
}

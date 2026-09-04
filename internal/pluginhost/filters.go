package pluginhost

import (
	"sort"
	"strings"

	"github.com/marcus/sidecar/internal/resource"
)

// Filters are a collection's own choosers: the scope a page was gathered under,
// and whatever else narrows it. They are declared in describe, drawn by the
// host, sent back in list.params.filters, and persisted with the tab — so what a
// page covers is visible, changeable, and survives a relaunch.
//
// The FIRST declared filter is the collection's scope. Its current value is
// always folded into the View pill, because a page gathered under a scope
// nobody can see is a page whose emptiness means nothing.

// FilterKind is the shape of one filter's control.
type FilterKind string

const (
	// FilterChoice is a fixed set of options drawn as a radio group.
	FilterChoice FilterKind = "choice"
	// FilterText is free text drawn as an input.
	FilterText FilterKind = "text"
)

// CoerceFilterKind maps a declared kind onto a known one, or "" for an unknown
// one — refused rather than degraded, exactly as an action input's kind is: a
// control the host draws as the wrong type collects the wrong value and the
// plugin filters on it.
func CoerceFilterKind(v string) FilterKind {
	switch FilterKind(strings.TrimSpace(v)) {
	case FilterChoice:
		return FilterChoice
	case FilterText:
		return FilterText
	default:
		return ""
	}
}

// FilterOption is one option of a choice filter.
type FilterOption struct {
	ID    string
	Title string
}

// Filter is one declared chooser on a collection.
type Filter struct {
	ID    string
	Label string
	Kind  FilterKind
	// Choices is the option list of a choice filter, and empty for a text one.
	Choices []FilterOption
	// Default is the choice ID a choice filter opens on, or the initial text of
	// a text filter. A value equal to it is never sent: a missing key means the
	// default.
	Default string
}

// OptionTitle returns the display title of one of a choice filter's options.
func (f Filter) OptionTitle(id string) (string, bool) {
	for _, choice := range f.Choices {
		if choice.ID == id {
			return choice.Title, true
		}
	}
	return "", false
}

// Value resolves what this filter is currently set to, given the applied map.
// A missing key is the default, which is the whole of the wire rule.
func (f Filter) Value(applied map[string]string) string {
	if v, ok := applied[f.ID]; ok {
		return v
	}
	return f.Default
}

// DisplayValue is what the host shows for this filter's current setting: a
// choice's title, or a text filter's own value. A text filter with no value
// shows its label, because "since" says more than an empty string does.
func (f Filter) DisplayValue(applied map[string]string) string {
	value := f.Value(applied)
	if f.Kind == FilterChoice {
		if title, ok := f.OptionTitle(value); ok {
			return title
		}
		return value
	}
	if strings.TrimSpace(value) == "" {
		return f.Label
	}
	return value
}

// ScopeFilter is the collection's scope: the first declared filter, which is
// what the pill always carries.
func (c Collection) ScopeFilter() (Filter, bool) {
	if len(c.Filters) == 0 {
		return Filter{}, false
	}
	return c.Filters[0], true
}

// Filter returns one declared filter by ID.
func (c Collection) Filter(id string) (Filter, bool) {
	for _, f := range c.Filters {
		if f.ID == id {
			return f, true
		}
	}
	return Filter{}, false
}

// NormalizeFilters is the one place the wire rule for params.filters is
// enforced, so the browser, the CLI and the process boundary cannot disagree
// about what reaches a plugin:
//
//   - a key the collection did not declare is DROPPED, because the plugin has
//     no control behind it and would be filtering on a name it never published;
//   - a choice value that is not one of that filter's declared options is
//     dropped, for the same reason;
//   - a value equal to the filter's default is dropped, because a missing key
//     means the default and sending it twice is two spellings of one state;
//   - everything else is sanitized to the declared bounds.
//
// It returns nil rather than an empty map when nothing survives, so the field
// is omitted from the request entirely.
func NormalizeFilters(c Collection, applied map[string]string) map[string]string {
	if len(applied) == 0 || len(c.Filters) == 0 {
		return nil
	}
	out := make(map[string]string, len(applied))
	for _, f := range c.Filters {
		raw, ok := applied[f.ID]
		if !ok {
			continue
		}
		value := resource.SanitizeLine(raw, MaxFilterValueChars)
		if f.Kind == FilterChoice {
			if _, known := f.OptionTitle(value); !known {
				continue
			}
		}
		if value == f.Default {
			continue
		}
		out[f.ID] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FilterKeys returns the applied filter IDs in a stable order, which is what a
// CLI report and a persisted record need so two identical states are two
// identical strings.
func FilterKeys(applied map[string]string) []string {
	if len(applied) == 0 {
		return nil
	}
	keys := make([]string, 0, len(applied))
	for k := range applied {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FilterCacheKey renders an applied set as one canonical string, so two
// identical scopes are one cache entry and two different ones are two. It is
// the get cache's half of the rule that a row expanded under a filter-chosen
// scope is a different document from the same row expanded under another.
//
// The separators are NUL, which resource.SanitizeLine strips from every value
// that reaches here, so no filter value can spell a different set.
func FilterCacheKey(applied map[string]string) string {
	if len(applied) == 0 {
		return ""
	}
	var b strings.Builder
	for _, key := range FilterKeys(applied) {
		b.WriteString(key)
		b.WriteByte(0)
		b.WriteString(applied[key])
		b.WriteByte(0)
	}
	return b.String()
}

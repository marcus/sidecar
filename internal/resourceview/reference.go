package resourceview

import (
	"fmt"

	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/terminallink"
)

// ReferenceForLocator chooses the first live matcher from provider whose whole
// match is locator. UI requests intentionally carry no matcher ID: the running
// app is the authority for the provider snapshot and its declared precedence.
func ReferenceForLocator(matchers []terminallink.ResourceMatcher, provider, locator string) (Ref, string) {
	hasProvider := false
	for _, matcher := range matchers {
		if matcher.Provider != provider || matcher.ID == "" || matcher.Re == nil {
			continue
		}
		hasProvider = true
		match := matcher.Re.FindStringIndex(locator)
		if len(match) == 2 && match[0] == 0 && match[1] == len(locator) {
			ref := Ref{Instance: provider, Matcher: matcher.ID, Locator: locator}
			if ref.Valid() {
				return ref, ""
			}
		}
	}
	if !hasProvider {
		return Ref{}, fmt.Sprintf("provider %s has no live matchers", provider)
	}
	return Ref{}, fmt.Sprintf("provider %s has no live matcher that recognizes %s", provider, locator)
}

// ReferenceForRequest builds the reference a request names, choosing the shape
// from the fields it carries rather than from which surface is asking.
//
// It exists so both workspace projections resolve `sidecar open` and a layout
// spec identically. A collection is a plugin tab and consults no matcher: there
// is no span a matcher could have claimed, and a row is addressed by its
// collection and ID. Everything else is today's matched locator, and a locator
// no live matcher recognizes is refused out loud rather than opened blind.
func ReferenceForRequest(matchers []terminallink.ResourceMatcher, provider, matcher, collection, query, value string, filters map[string]string) (Ref, string) {
	if collection != "" {
		ref := Ref{
			Instance: provider, Collection: collection, Query: query, Locator: value,
			Filters: resource.FilterValues(filters),
		}
		if !ref.Valid() {
			return Ref{}, fmt.Sprintf("plugin %s cannot open collection %q as asked", provider, collection)
		}
		return ref, ""
	}
	if matcher != "" {
		ref := Ref{Instance: provider, Matcher: matcher, Locator: value}
		if ref.Valid() {
			return ref, ""
		}
	}
	return ReferenceForLocator(matchers, provider, value)
}

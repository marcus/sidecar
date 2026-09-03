package pluginhost

import (
	"strings"
	"testing"
)

// describeWithFilters builds a minimal, otherwise-valid describe response whose
// only interesting part is one collection's filters, so a refusal in these
// tests can only have come from the filters.
func describeWithFilters(filters []WireFilter) *Response {
	return &Response{
		Protocol: Protocol,
		Plugin:   &Info{Kind: "fixture", Name: "Fixture"},
		Collections: []WireCollection{{
			ID:      "results",
			Title:   "Results",
			Columns: []WireColumn{{ID: "title", Label: "Title", Primary: true}},
			Filters: filters,
		}},
	}
}

func TestFilterDeclarationsAreValidatedWhole(t *testing.T) {
	long := strings.Repeat("f", MaxFilterIDChars+1)
	tooMany := make([]WireFilter, 0, MaxFilters+1)
	for i := 0; i < MaxFilters+1; i++ {
		tooMany = append(tooMany, WireFilter{ID: string(rune('a'+i)) + "f", Kind: "text"})
	}
	tooManyChoices := make([]WireFilterChoice, 0, MaxFilterChoices+1)
	for i := 0; i < MaxFilterChoices+1; i++ {
		tooManyChoices = append(tooManyChoices, WireFilterChoice{ID: "c" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)})
	}

	refusals := []struct {
		name    string
		filters []WireFilter
		want    string
	}{
		{"over the filter bound", tooMany, "filters, the limit is"},
		{"no id", []WireFilter{{Kind: "text"}}, "has no id"},
		{"unstorable id", []WireFilter{{ID: "sc\tope", Kind: "text"}}, "cannot be stored verbatim"},
		{"id too long", []WireFilter{{ID: long, Kind: "text"}}, "cannot be stored verbatim"},
		{"duplicate id", []WireFilter{{ID: "scope", Kind: "text"}, {ID: "scope", Kind: "text"}}, "more than once"},
		{"unknown kind", []WireFilter{{ID: "scope", Kind: "slider"}}, "not one of choice or text"},
		{"missing kind", []WireFilter{{ID: "scope"}}, "not one of choice or text"},
		{"choice with no choices", []WireFilter{{ID: "scope", Kind: "choice"}}, "choice with no choices"},
		{
			"over the choice bound",
			[]WireFilter{{ID: "scope", Kind: "choice", Choices: tooManyChoices}},
			"choices, the limit is",
		},
		{
			"duplicate choice id",
			[]WireFilter{{ID: "scope", Kind: "choice", Choices: []WireFilterChoice{{ID: "a"}, {ID: "a"}}}},
			"choice id \"a\" more than once",
		},
		{
			"default that is not a choice",
			[]WireFilter{{ID: "scope", Kind: "choice", Choices: []WireFilterChoice{{ID: "a"}}, Default: "b"}},
			"not one of its choices",
		},
		{
			"text filter with choices",
			[]WireFilter{{ID: "since", Kind: "text", Choices: []WireFilterChoice{{ID: "a"}}}},
			"is text and declares choices",
		},
		{
			"text default the host cannot store",
			[]WireFilter{{ID: "since", Kind: "text", Default: strings.Repeat("d", MaxFilterValueChars+1)}},
			"cannot store verbatim",
		},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			desc, err := ValidateDescribe("fixture", describeWithFilters(tc.filters), t.TempDir())
			if err == nil {
				t.Fatalf("describe was accepted; filters are validated all-or-nothing like the rest of it: %+v", desc)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("reason = %q, want it to mention %q", err.Error(), tc.want)
			}
			if len(desc.Collections) != 0 {
				t.Fatalf("a refused describe published %d collections", len(desc.Collections))
			}
		})
	}
}

func TestFilterDeclarationsAtTheBoundsAreAccepted(t *testing.T) {
	choices := make([]WireFilterChoice, 0, MaxFilterChoices)
	for i := 0; i < MaxFilterChoices; i++ {
		choices = append(choices, WireFilterChoice{ID: "c" + strings.Repeat("x", i%20) + string(rune('a'+i%26))})
	}
	filters := make([]WireFilter, 0, MaxFilters)
	filters = append(filters, WireFilter{ID: "scope", Label: "Scope", Kind: "choice", Choices: choices, Default: choices[3].ID})
	for i := 1; i < MaxFilters; i++ {
		filters = append(filters, WireFilter{ID: "t" + string(rune('a'+i)), Kind: "text"})
	}
	desc, err := ValidateDescribe("fixture", describeWithFilters(filters), t.TempDir())
	if err != nil {
		t.Fatalf("exactly at the bounds must be accepted: %v", err)
	}
	c, _ := desc.Collection("results")
	if len(c.Filters) != MaxFilters {
		t.Fatalf("filters = %d, want %d", len(c.Filters), MaxFilters)
	}
	scope, ok := c.ScopeFilter()
	if !ok || scope.ID != "scope" {
		t.Fatalf("scope = %+v; the first declared filter is the collection's scope", scope)
	}
	if scope.Default != choices[3].ID {
		t.Fatalf("default = %q, want the declared one", scope.Default)
	}
}

// A choice filter that states no default opens on its first declared option: a
// radio group has to open on something, and the plugin's own order says which.
func TestChoiceFilterWithNoDefaultTakesItsFirstChoice(t *testing.T) {
	desc, err := ValidateDescribe("fixture", describeWithFilters([]WireFilter{
		{ID: "source", Kind: "choice", Choices: []WireFilterChoice{{ID: "any", Title: "Any"}, {ID: "notes"}}},
	}), t.TempDir())
	if err != nil {
		t.Fatalf("ValidateDescribe: %v", err)
	}
	c, _ := desc.Collection("results")
	if c.Filters[0].Default != "any" {
		t.Fatalf("default = %q, want the first declared choice", c.Filters[0].Default)
	}
	// An option with no title falls back to its id, exactly as a column label
	// and a view title do.
	if title, _ := c.Filters[0].OptionTitle("notes"); title != "notes" {
		t.Fatalf("title = %q, want the id as the fallback", title)
	}
}

// The wire rule for params.filters, enforced in one place so the browser, the
// CLI and the process boundary cannot disagree.
func TestNormalizeFiltersKeepsOnlyDeclaredNonDefaultValues(t *testing.T) {
	c := Collection{
		ID: "results",
		Filters: []Filter{
			{ID: "scope", Kind: FilterChoice, Default: "everything", Choices: []FilterOption{
				{ID: "everything", Title: "Everything"}, {ID: "project", Title: "This project"},
			}},
			{ID: "source", Kind: FilterChoice, Default: "any", Choices: []FilterOption{
				{ID: "any", Title: "Any"}, {ID: "notes", Title: "notes"},
			}},
			{ID: "since", Kind: FilterText},
		},
	}
	got := NormalizeFilters(c, map[string]string{
		"scope":    "project",    // declared, not the default: kept
		"source":   "any",        // declared, but IS the default: dropped
		"since":    "2026-08-01", // declared text: kept
		"smuggled": "nowhere",    // never declared: dropped
		"scope2":   "everything", // never declared: dropped
	})
	want := map[string]string{"scope": "project", "since": "2026-08-01"}
	if len(got) != len(want) {
		t.Fatalf("filters = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("filters[%q] = %q, want %q", k, got[k], v)
		}
	}

	// A choice value the filter never declared has no control behind it, so it
	// is dropped exactly as an undeclared key is.
	if got := NormalizeFilters(c, map[string]string{"source": "invented"}); got != nil {
		t.Fatalf("filters = %v, want nothing sent", got)
	}
	// Everything at its default is no filters at all, and the field is omitted
	// rather than sent as an empty object.
	if got := NormalizeFilters(c, map[string]string{"scope": "everything"}); got != nil {
		t.Fatalf("filters = %v, want nil", got)
	}
	if got := NormalizeFilters(Collection{ID: "x"}, map[string]string{"a": "b"}); got != nil {
		t.Fatalf("a collection with no filters must send none, got %v", got)
	}
}

func TestFilterDisplayValue(t *testing.T) {
	scope := Filter{ID: "scope", Label: "Scope", Kind: FilterChoice, Default: "everything", Choices: []FilterOption{
		{ID: "everything", Title: "Everything"}, {ID: "project", Title: "This project"},
	}}
	if got := scope.DisplayValue(nil); got != "Everything" {
		t.Fatalf("display = %q, want the default choice's title", got)
	}
	if got := scope.DisplayValue(map[string]string{"scope": "project"}); got != "This project" {
		t.Fatalf("display = %q", got)
	}
	since := Filter{ID: "since", Label: "Since", Kind: FilterText}
	if got := since.DisplayValue(nil); got != "Since" {
		t.Fatalf("display = %q; an empty text filter shows its label, not an empty string", got)
	}
	if got := since.DisplayValue(map[string]string{"since": "2026-08-01"}); got != "2026-08-01" {
		t.Fatalf("display = %q", got)
	}
}

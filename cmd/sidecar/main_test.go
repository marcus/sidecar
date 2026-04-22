package main

import "testing"

func TestNormalizeVCSPreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to auto", in: "", want: "auto"},
		{name: "whitespace defaults to auto", in: "   ", want: "auto"},
		{name: "auto remains auto", in: "auto", want: "auto"},
		{name: "jj normalized lowercase", in: "JJ", want: "jj"},
		{name: "git normalized lowercase", in: "Git", want: "git"},
		{name: "unknown defaults to auto", in: "svn", want: "auto"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeVCSPreference(tc.in); got != tc.want {
				t.Fatalf("normalizeVCSPreference(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

package gitstatus

import "testing"

func TestNormalizeCommitMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \n\t  ", ""},
		{"trims whitespace", "  fix bug  ", "Fix bug"},
		{"capitalizes first letter", "fix bug", "Fix bug"},
		{"already capitalized", "Fix bug", "Fix bug"},
		{"non-letter start unchanged", "123 fix", "123 fix"},
		{"removes trailing period", "Fix bug.", "Fix bug"},
		{"removes multiple trailing periods", "Fix bug...", "Fix bug"},
		{"keeps mid-sentence periods", "Fix a.b.c bug", "Fix a.b.c bug"},
		{"truncates long subject", string(make([]byte, 80)), string(make([]byte, 71)) + "…"},
		{"72 chars exact no truncate", string(make([]byte, 72)), string(make([]byte, 72))},
		{"subject and body with blank line", "fix bug\n\nbody text", "Fix bug\n\nbody text"},
		{"subject and body missing blank line", "fix bug\nbody text", "Fix bug\n\nbody text"},
		{"subject with extra blank lines before body", "fix bug\n\n\nbody", "Fix bug\n\nbody"},
		{"subject only with trailing newlines", "fix bug\n\n", "Fix bug"},
		{"unicode first char", "über cool", "Über cool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCommitMessage(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeCommitMessage(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

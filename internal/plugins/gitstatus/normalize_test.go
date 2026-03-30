package gitstatus

import (
	"testing"

	"github.com/marcus/sidecar/internal/config"
)

func TestNormalizeCommitMessage(t *testing.T) {
	defaultCfg := config.CommitConfig{SubjectMaxLen: 72}

	tests := []struct {
		name    string
		raw     string
		cfg     config.CommitConfig
		wantMsg string
		wantWrn string
		wantErr string
	}{
		{
			name:    "simple message unchanged",
			raw:     "fix: resolve crash on startup",
			cfg:     defaultCfg,
			wantMsg: "fix: resolve crash on startup",
		},
		{
			name:    "trims leading and trailing whitespace",
			raw:     "\n\n  fix: trim test  \n\n",
			cfg:     defaultCfg,
			wantMsg: "  fix: trim test",
		},
		{
			name:    "strips trailing whitespace per line",
			raw:     "subject line   \n\nbody line   ",
			cfg:     defaultCfg,
			wantMsg: "subject line\n\nbody line",
		},
		{
			name:    "collapses consecutive blank lines",
			raw:     "subject\n\n\n\nbody paragraph",
			cfg:     defaultCfg,
			wantMsg: "subject\n\nbody paragraph",
		},
		{
			name:    "normalizes CRLF to LF",
			raw:     "subject\r\n\r\nbody",
			cfg:     defaultCfg,
			wantMsg: "subject\n\nbody",
		},
		{
			name:    "normalizes CR to LF",
			raw:     "subject\r\rbody",
			cfg:     defaultCfg,
			wantMsg: "subject\n\nbody",
		},
		{
			name:    "empty message returns error",
			raw:     "   \n\n  ",
			cfg:     defaultCfg,
			wantErr: "Commit message cannot be empty",
		},
		{
			name:    "subject exceeds max length warns",
			raw:     "feat: this is a very long commit message subject line that exceeds the seventy-two character limit by quite a bit",
			cfg:     defaultCfg,
			wantMsg: "feat: this is a very long commit message subject line that exceeds the seventy-two character limit by quite a bit",
			wantWrn: "Subject line exceeds recommended length",
		},
		{
			name:    "subject within max length no warning",
			raw:     "feat: short subject",
			cfg:     config.CommitConfig{SubjectMaxLen: 72},
			wantMsg: "feat: short subject",
		},
		{
			name:    "conventional commits enforced with valid prefix",
			raw:     "feat: add feature",
			cfg:     config.CommitConfig{SubjectMaxLen: 72, ConventionalCommits: true},
			wantMsg: "feat: add feature",
		},
		{
			name:    "conventional commits enforced with scope",
			raw:     "fix(auth): resolve token bug",
			cfg:     config.CommitConfig{SubjectMaxLen: 72, ConventionalCommits: true},
			wantMsg: "fix(auth): resolve token bug",
		},
		{
			name:    "conventional commits enforced missing prefix",
			raw:     "add feature without prefix",
			cfg:     config.CommitConfig{SubjectMaxLen: 72, ConventionalCommits: true},
			wantMsg: "add feature without prefix",
			wantErr: "Missing conventional commit prefix (e.g., feat:, fix:)",
		},
		{
			name:    "custom allowed prefixes",
			raw:     "hotfix: emergency patch",
			cfg:     config.CommitConfig{SubjectMaxLen: 72, ConventionalCommits: true, AllowedPrefixes: []string{"hotfix", "release"}},
			wantMsg: "hotfix: emergency patch",
		},
		{
			name:    "custom prefixes rejects standard prefix",
			raw:     "feat: new thing",
			cfg:     config.CommitConfig{SubjectMaxLen: 72, ConventionalCommits: true, AllowedPrefixes: []string{"hotfix"}},
			wantMsg: "feat: new thing",
			wantErr: "Missing conventional commit prefix (e.g., feat:, fix:)",
		},
		{
			name:    "multiline with body preserved",
			raw:     "feat: add normalizer\n\nThis normalizes commit messages\nto ensure consistent formatting.",
			cfg:     defaultCfg,
			wantMsg: "feat: add normalizer\n\nThis normalizes commit messages\nto ensure consistent formatting.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeCommitMessage(tt.raw, tt.cfg)
			if result.Message != tt.wantMsg {
				t.Errorf("Message:\n got:  %q\nwant: %q", result.Message, tt.wantMsg)
			}
			if result.Warning != tt.wantWrn {
				t.Errorf("Warning: got %q, want %q", result.Warning, tt.wantWrn)
			}
			if result.Error != tt.wantErr {
				t.Errorf("Error: got %q, want %q", result.Error, tt.wantErr)
			}
		})
	}
}

func TestSubjectLength(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"single line", 11},
		{"subject\n\nbody", 7},
		{"", 0},
		{"subject\nbody", 7},
	}
	for _, tt := range tests {
		if got := SubjectLength(tt.msg); got != tt.want {
			t.Errorf("SubjectLength(%q) = %d, want %d", tt.msg, got, tt.want)
		}
	}
}

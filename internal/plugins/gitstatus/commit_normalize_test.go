package gitstatus

import (
	"testing"

	"github.com/marcus/sidecar/internal/config"
)

func TestNormalizeCommitMessage(t *testing.T) {
	defaultCfg := config.CommitConfig{
		SubjectMaxLen:          72,
		EnforceBlankSecondLine: true,
		AutoCapitalize:         true,
		StripTrailingPeriod:    true,
	}

	tests := []struct {
		name string
		msg  string
		cfg  config.CommitConfig
		want string
	}{
		{
			name: "empty message",
			msg:  "",
			cfg:  defaultCfg,
			want: "",
		},
		{
			name: "capitalize first letter",
			msg:  "fix bug in parser",
			cfg:  defaultCfg,
			want: "Fix bug in parser",
		},
		{
			name: "already capitalized",
			msg:  "Fix bug in parser",
			cfg:  defaultCfg,
			want: "Fix bug in parser",
		},
		{
			name: "strip trailing period",
			msg:  "Fix bug in parser.",
			cfg:  defaultCfg,
			want: "Fix bug in parser",
		},
		{
			name: "strip multiple trailing periods",
			msg:  "Fix bug...",
			cfg:  defaultCfg,
			want: "Fix bug",
		},
		{
			name: "enforce blank second line",
			msg:  "Fix bug\nThis is the body",
			cfg:  defaultCfg,
			want: "Fix bug\n\nThis is the body",
		},
		{
			name: "blank second line already present",
			msg:  "Fix bug\n\nThis is the body",
			cfg:  defaultCfg,
			want: "Fix bug\n\nThis is the body",
		},
		{
			name: "truncate long subject at word boundary",
			msg:  "This is a very long commit message subject that exceeds the maximum allowed length for subjects",
			cfg: config.CommitConfig{
				SubjectMaxLen:          50,
				EnforceBlankSecondLine: true,
				AutoCapitalize:         true,
				StripTrailingPeriod:    true,
			},
			want: "This is a very long commit message subject that",
		},
		{
			name: "all rules combined",
			msg:  "fix the parser bug.\ndetailed explanation here",
			cfg:  defaultCfg,
			want: "Fix the parser bug\n\ndetailed explanation here",
		},
		{
			name: "auto capitalize disabled",
			msg:  "fix bug",
			cfg: config.CommitConfig{
				SubjectMaxLen:          72,
				EnforceBlankSecondLine: true,
				AutoCapitalize:         false,
				StripTrailingPeriod:    true,
			},
			want: "fix bug",
		},
		{
			name: "strip period disabled",
			msg:  "Fix bug.",
			cfg: config.CommitConfig{
				SubjectMaxLen:          72,
				EnforceBlankSecondLine: true,
				AutoCapitalize:         true,
				StripTrailingPeriod:    false,
			},
			want: "Fix bug.",
		},
		{
			name: "blank line enforcement disabled",
			msg:  "Fix bug\nBody text",
			cfg: config.CommitConfig{
				SubjectMaxLen:          72,
				EnforceBlankSecondLine: false,
				AutoCapitalize:         true,
				StripTrailingPeriod:    true,
			},
			want: "Fix bug\nBody text",
		},
		{
			name: "subject only no body",
			msg:  "fix bug.",
			cfg:  defaultCfg,
			want: "Fix bug",
		},
		{
			name: "multiline body preserved",
			msg:  "fix bug.\n\nLine 1\nLine 2\nLine 3",
			cfg:  defaultCfg,
			want: "Fix bug\n\nLine 1\nLine 2\nLine 3",
		},
		{
			name: "unicode first character",
			msg:  "über cool feature",
			cfg:  defaultCfg,
			want: "Über cool feature",
		},
		{
			name: "zero max length disables truncation",
			msg:  "This is a very long subject line that would normally be truncated but max len is zero",
			cfg: config.CommitConfig{
				SubjectMaxLen:          0,
				EnforceBlankSecondLine: true,
				AutoCapitalize:         true,
				StripTrailingPeriod:    true,
			},
			want: "This is a very long subject line that would normally be truncated but max len is zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCommitMessage(tt.msg, tt.cfg)
			if got != tt.want {
				t.Errorf("NormalizeCommitMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

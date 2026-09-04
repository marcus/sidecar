package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadTerminalResources(t *testing.T) {
	path := writeConfig(t, `{
	  "terminalResources": {
	    "providers": [
	      {
	        "id": "jira-work",
	        "command": ["sidecar-jira", "sidecar-provider", "--profile", "work"],
	        "passEnv": ["JIRA_API_TOKEN"],
	        "enabled": true,
	        "timeout": "10s"
	      },
	      {
	        "id": "buildkite",
	        "command": ["/usr/local/bin/sidecar-buildkite"],
	        "enabled": false
	      }
	    ]
	  }
	}`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	providers := cfg.TerminalResources.Providers
	if len(providers) != 2 {
		t.Fatalf("providers = %+v", providers)
	}
	if providers[0].ID != "jira-work" || providers[0].Timeout != 10*time.Second {
		t.Fatalf("first = %+v", providers[0])
	}
	if !slices.Equal(providers[0].Command, []string{"sidecar-jira", "sidecar-provider", "--profile", "work"}) {
		t.Fatalf("command = %v", providers[0].Command)
	}
	if !slices.Equal(providers[0].PassEnv, []string{"JIRA_API_TOKEN"}) {
		t.Fatalf("passEnv = %v", providers[0].PassEnv)
	}
	if providers[1].Enabled {
		t.Fatal("explicit enabled:false was not honored")
	}
	// Array order is matcher precedence, so it must be preserved verbatim.
	if providers[1].ID != "buildkite" {
		t.Fatalf("order changed: %+v", providers)
	}

	if ids := cfg.TerminalResources.DisabledProviderIDs(); !slices.Equal(ids, []string{"buildkite"}) {
		t.Fatalf("disabled = %v", ids)
	}
	if enabled := cfg.TerminalResources.EnabledProviders(); len(enabled) != 1 || enabled[0].ID != "jira-work" {
		t.Fatalf("enabled = %+v", enabled)
	}
}

func TestLoadTerminalResourcesDefaults(t *testing.T) {
	// An omitted `enabled` means on: a configured instance the user typed out
	// should work without a second opt-in.
	path := writeConfig(t, `{"terminalResources":{"providers":[{"id":"p","command":["p"]}]}}`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	p := cfg.TerminalResources.Providers[0]
	if !p.Enabled {
		t.Fatal("an omitted enabled flag should default to true")
	}
	if p.Timeout != DefaultTerminalResourceTimeout {
		t.Fatalf("timeout = %s", p.Timeout)
	}
	if p.ClaimHosts != nil {
		t.Fatalf("claimHosts = %v, want none", p.ClaimHosts)
	}
}

// Matching is case-insensitive and the stored form is lowercase, so loading
// normalizes entries once instead of every scan.
func TestLoadNormalizesClaimHosts(t *testing.T) {
	path := writeConfig(t, `{"terminalResources":{"providers":[{
	  "id":"github","command":["sidecar-github","sidecar-provider"],
	  "claimHosts":["GitHub.com","gist.github.com"]
	}]}}`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	got := cfg.TerminalResources.Providers[0].ClaimHosts
	if !slices.Equal(got, []string{"github.com", "gist.github.com"}) {
		t.Fatalf("claimHosts = %v", got)
	}
}

// A claimHosts entry that can never match a hostname is refused rather than
// silently ignored: the user needs to know why GitHub URLs still open the
// browser.
func TestValidateRejectsMalformedClaimHosts(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"scheme", "https://github.com"},
		{"port", "github.com:443"},
		{"path", "github.com/owner"},
		{"userinfo", "user@github.com"},
		{"wildcard", "*.github.com"},
		{"percent escape", "%65%67ithub.com"},
		{"trailing dot path", "github.com./"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := TerminalResourcesConfig{Providers: []TerminalResourceProviderConfig{
				{ID: "a", Command: []string{"x"}, ClaimHosts: []string{tc.entry}},
			}}
			err := validateTerminalResources(&cfg)
			if err == nil {
				t.Fatalf("entry %q should be rejected", tc.entry)
			}
			if !strings.Contains(err.Error(), "not a bare hostname") {
				t.Fatalf("error = %q", err)
			}
		})
	}
}

func TestValidateBoundsClaimHosts(t *testing.T) {
	tooMany := make([]string, MaxTerminalResourceClaimHosts+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("host%d.example.com", i)
	}
	cfg := TerminalResourcesConfig{Providers: []TerminalResourceProviderConfig{
		{ID: "a", Command: []string{"x"}, ClaimHosts: tooMany},
	}}
	err := validateTerminalResources(&cfg)
	if err == nil || !strings.Contains(err.Error(), "the limit is") {
		t.Fatalf("err = %v, want the limit message", err)
	}
}

// The loader's unknown-field convention is deliberate and file-wide: unknown
// keys inside terminalResources load inertly on an older Sidecar instead of
// failing validation loudly.
func TestLoadIgnoresUnknownTerminalResourceFields(t *testing.T) {
	path := writeConfig(t, `{"terminalResources":{"providers":[
	  {"id":"a","command":["x"],"futureField":true,"claimHosts":["example.com"]}
	]}}`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	p := cfg.TerminalResources.Providers[0]
	if !slices.Equal(p.ClaimHosts, []string{"example.com"}) {
		t.Fatalf("claimHosts = %v", p.ClaimHosts)
	}
}

func TestLoadTerminalResourcesAbsentSection(t *testing.T) {
	path := writeConfig(t, `{"ui":{"showClock":true}}`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(cfg.TerminalResources.Providers) != 0 {
		t.Fatalf("providers = %+v", cfg.TerminalResources.Providers)
	}
}

func TestValidateTerminalResources(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TerminalResourcesConfig
		wantErr string
	}{
		{
			name:    "duplicate id",
			cfg:     providers(prov("a", "x"), prov("a", "y")),
			wantErr: "more than once",
		},
		{
			name:    "empty id",
			cfg:     providers(prov("   ", "x")),
			wantErr: "has no id",
		},
		{
			name:    "empty argv",
			cfg:     TerminalResourcesConfig{Providers: []TerminalResourceProviderConfig{{ID: "a"}}},
			wantErr: "has no command",
		},
		{
			name:    "blank argv0",
			cfg:     providers(prov("a", "  ")),
			wantErr: "has no command",
		},
		{
			name:    "inline secret",
			cfg:     TerminalResourcesConfig{Providers: []TerminalResourceProviderConfig{{ID: "a", Command: []string{"x"}, PassEnv: []string{"TOKEN=hunter2"}}}},
			wantErr: "inline value",
		},
		{
			name:    "too many providers",
			cfg:     manyProviders(MaxTerminalResourceProviders + 1),
			wantErr: "the limit is",
		},
		{
			name: "oversize id",
			cfg:  providers(prov(strings.Repeat("i", MaxTerminalResourceProviderIDChars+1), "x")),

			wantErr: "longer than",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{TerminalResources: tc.cfg}
			err := validateTerminalResources(&cfg.TerminalResources)
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// An inline secret must not be echoed back in the error, or a log line about a
// rejected config becomes the leak.
func TestValidateTerminalResourcesDoesNotEchoInlineSecrets(t *testing.T) {
	cfg := TerminalResourcesConfig{Providers: []TerminalResourceProviderConfig{
		{ID: "a", Command: []string{"x"}, PassEnv: []string{"JIRA_API_TOKEN=hunter2"}},
	}}
	err := validateTerminalResources(&cfg)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the error echoed the secret: %q", err)
	}
	if !strings.Contains(err.Error(), "JIRA_API_TOKEN") {
		t.Fatalf("the error should still name the variable: %q", err)
	}
}

func TestValidateTerminalResourcesClampsTimeouts(t *testing.T) {
	cfg := TerminalResourcesConfig{Providers: []TerminalResourceProviderConfig{
		{ID: "a", Command: []string{"x"}, Timeout: 0},
		{ID: "b", Command: []string{"x"}, Timeout: time.Millisecond},
		{ID: "c", Command: []string{"x"}, Timeout: 10 * time.Minute},
		{ID: "d", Command: []string{"x"}, Timeout: 20 * time.Second},
		{ID: "e", Command: []string{"x"}, Timeout: -5 * time.Second},
	}}
	if err := validateTerminalResources(&cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	want := []time.Duration{
		DefaultTerminalResourceTimeout,
		MinTerminalResourceTimeout,
		MaxTerminalResourceTimeout,
		20 * time.Second,
		DefaultTerminalResourceTimeout,
	}
	for i, w := range want {
		if cfg.Providers[i].Timeout != w {
			t.Fatalf("provider %d timeout = %s, want %s", i, cfg.Providers[i].Timeout, w)
		}
	}
}

func TestLoadRejectsInvalidTerminalResources(t *testing.T) {
	path := writeConfig(t, `{"terminalResources":{"providers":[{"id":"a","command":["x"]},{"id":"a","command":["y"]}]}}`)
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("a duplicate provider id should fail validation on load")
	}
}

// The round-trip the Configuration UI depends on: loading a config, changing
// something unrelated, and saving must leave terminalResources intact.
func TestSavePreservesTerminalResources(t *testing.T) {
	path := writeConfig(t, `{
	  "ui": {"showClock": false},
	  "terminalResources": {
	    "providers": [
	      {"id": "jira-work", "command": ["sidecar-jira", "sidecar-provider"], "passEnv": ["JIRA_API_TOKEN"], "enabled": true, "timeout": "12s"}
	    ]
	  },
	  "prompts": {"unmanagedKeyThatMustSurvive": true}
	}`)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	if err := SaveUI(func(ui *UIConfig) { ui.ShowClock = true }); err != nil {
		t.Fatalf("SaveUI: %v", err)
	}

	raw := readRawConfig(t, path)
	if _, ok := raw["terminalResources"]; !ok {
		t.Fatal("SaveUI erased the terminalResources section")
	}
	if _, ok := raw["prompts"]; !ok {
		t.Fatal("SaveUI erased an unmanaged key")
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.UI.ShowClock {
		t.Fatal("the UI change was not saved")
	}
	p := cfg.TerminalResources.Providers
	if len(p) != 1 || p[0].ID != "jira-work" || p[0].Timeout != 12*time.Second {
		t.Fatalf("providers after round-trip = %+v", p)
	}
	if !slices.Equal(p[0].Command, []string{"sidecar-jira", "sidecar-provider"}) {
		t.Fatalf("command after round-trip = %v", p[0].Command)
	}
	if !slices.Equal(p[0].PassEnv, []string{"JIRA_API_TOKEN"}) {
		t.Fatalf("passEnv after round-trip = %v", p[0].PassEnv)
	}
	if !p[0].Enabled {
		t.Fatal("enabled was lost")
	}
}

// Two consecutive saves must be stable: the second must not gain, lose, or
// reorder anything.
func TestSaveTerminalResourcesIsIdempotent(t *testing.T) {
	path := writeConfig(t, `{"terminalResources":{"providers":[
	  {"id":"a","command":["a"],"enabled":true,"timeout":"5s","claimHosts":["GitHub.com"]},
	  {"id":"b","command":["b","--x"],"enabled":false}
	]}}`)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(cfg2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("save is not idempotent:\n%s\n---\n%s", first, second)
	}
	// Order is matcher precedence and must survive verbatim.
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.TerminalResources.Providers[0].ID != "a" || reloaded.TerminalResources.Providers[1].ID != "b" {
		t.Fatalf("order changed: %+v", reloaded.TerminalResources.Providers)
	}
	// claimHosts must round-trip in normalized form, on disabled instances too.
	reloadedA := reloaded.TerminalResources.Providers[0]
	if !slices.Equal(reloadedA.ClaimHosts, []string{"github.com"}) {
		t.Fatalf("claimHosts after round-trip = %v", reloadedA.ClaimHosts)
	}
}

// terminalResources is a read-only alias, so clearing the providers in memory
// changes nothing on disk. The section belongs to the user's file until the
// release that rewrites its entries into plugins.external; Save neither writes
// it nor deletes it, and a caller that wants it gone edits the file.
func TestSaveKeepsTerminalResourcesEmptiedInMemory(t *testing.T) {
	path := writeConfig(t, `{"terminalResources":{"providers":[{"id":"a","command":["a"],"enabled":true}]}}`)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.TerminalResources.Providers = nil
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok := readRawConfig(t, path)["terminalResources"]; !ok {
		t.Fatal("Save deleted a section it does not own")
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.TerminalResources.Providers) != 1 {
		t.Fatalf("providers after the save = %+v", reloaded.TerminalResources.Providers)
	}
}

// A config with no providers must not start writing an empty section into
// every user's file.
func TestSaveDoesNotAddAnEmptyTerminalResourcesSection(t *testing.T) {
	path := writeConfig(t, `{"ui":{"showClock":false}}`)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok := readRawConfig(t, path)["terminalResources"]; ok {
		t.Fatal("Save added an empty terminalResources section")
	}
}

func readRawConfig(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	return raw
}

func prov(id, argv0 string) TerminalResourceProviderConfig {
	return TerminalResourceProviderConfig{ID: id, Command: []string{argv0}, Enabled: true}
}

func providers(ps ...TerminalResourceProviderConfig) TerminalResourcesConfig {
	return TerminalResourcesConfig{Providers: ps}
}

func manyProviders(n int) TerminalResourcesConfig {
	cfg := TerminalResourcesConfig{}
	for i := 0; i < n; i++ {
		cfg.Providers = append(cfg.Providers, prov(string(rune('a'+i%26))+strings.Repeat("x", i), "cmd"))
	}
	return cfg
}

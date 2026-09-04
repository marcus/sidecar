package config

import (
	"fmt"
	"strings"
	"time"
)

// External plugin bounds. They are the terminal resource provider bounds under
// a second name rather than new numbers: one host runs both kinds of child
// process, so one set of limits governs them. Restating them here keeps
// internal/config a leaf package, and TestPluginBoundsMatchTheProtocol in
// internal/pluginhost fails if they ever drift from internal/resource.
const (
	// MaxExternalPlugins bounds how many instances may be configured under
	// plugins.external.
	MaxExternalPlugins = MaxTerminalResourceProviders
	// MaxExternalPluginIDChars bounds an instance ID, which is a persisted key.
	MaxExternalPluginIDChars = MaxTerminalResourceProviderIDChars
)

// The values plugins.external[].scope takes.
const (
	// PluginScopeGlobal is the only scope v1 supports: the plugin is built
	// once, survives project switches, and reads project context per call.
	PluginScopeGlobal = "global"
	// PluginScopeProject is accepted by the parser and refused by validation
	// with a clear message. It is named here so the refusal can be specific
	// rather than "unknown scope".
	PluginScopeProject = "project"
)

// The values plugins.external[].placements takes. They are the string forms of
// plugin.Placement; internal/plugin cannot be imported from a leaf package.
const (
	// PluginPlacementTab is a navbar entry.
	PluginPlacementTab = "tab"
	// PluginPlacementPanes means the plugin's content can open as leaves in
	// the pane decks of both workspace projections.
	PluginPlacementPanes = "panes"
)

// The config sections an external plugin instance can be read from.
const (
	// PluginSourceExternal is plugins.external[], the section this plan added.
	PluginSourceExternal = "plugins.external"
	// PluginSourceTerminalResources is terminalResources.providers[], the
	// frozen resource protocol's section. Entries there are dispatched with the
	// old protocol identifier and keep working unchanged.
	PluginSourceTerminalResources = "terminalResources.providers"
)

// reservedPluginIDs are the IDs Sidecar's own surfaces already answer to,
// mapped to the name a message should call them by: every embedded plugin
// descriptor, plus the two app-owned global tabs.
//
// An external plugin may not take one. The ID is the config key, the CLI name
// and the persisted tab ID, so a second surface answering to "tasks" is not a
// duplicate row but an ambiguity: the header paints two entries, the lookup by
// ID answers with the first, and the plugin's own tab becomes reachable only by
// click.
//
// It is restated here rather than read from the descriptor catalog because
// internal/config is a leaf package — internal/plugin imports it, not the other
// way round. TestReservedPluginIDsCoverTheCatalog in internal/plugins/assembly
// fails if a descriptor or a global surface is ever added without being named
// here.
var reservedPluginIDs = map[string]string{
	"td-monitor":        "td",
	"git-status":        "Git",
	"file-browser":      "Files",
	"conversations":     "Conversations",
	"workspace-manager": "Workspaces",
	"notes":             "Notes",
	"tasks":             "Tasks",
	"sessions":          "Sessions",
	"activity":          "Activity",
}

// ReservedPluginID reports whether id names a built-in Sidecar surface, and
// what that surface is called. It is exported so `sidecar plugin add` can
// refuse a collision before it writes one, with the same message validation
// would have produced on the next load.
func ReservedPluginID(id string) (string, bool) {
	name, ok := reservedPluginIDs[strings.TrimSpace(id)]
	return name, ok
}

// PluginInstanceConfig is one configured external plugin instance.
//
// It is the terminal resource provider entry plus the two facts a protocol
// plugin needs and a resource provider never had: what its lifecycle is, and
// where its content may be shown. Everything else — the argv, the environment
// allowlist, the timeout, the claimed hosts — means exactly what it means for a
// resource provider, because the same host runs both.
//
// The discovery policy is unchanged and worth restating, because it is the
// standing decision a plugin-directory proposal has to argue against: Sidecar
// never scans a directory, never executes every sidecar-* binary on PATH, never
// auto-enables anything, and never lets a repository declare a plugin. A
// process boundary is crash isolation, not a sandbox: configuring a plugin
// trusts that executable with the user's full OS privileges.
type PluginInstanceConfig struct {
	// ID is unique and stable. It is the persisted plugin key, the CLI name,
	// and the authoritative identity of the instance: a plugin cannot rename
	// itself.
	ID string `json:"id"`
	// Command is an argv array executed without a shell. The first element may
	// be an absolute path or resolve through PATH.
	Command []string `json:"command"`
	// PassEnv names variables whose current values are inherited on top of the
	// documented base environment. Names only — inline secret values are not
	// supported, and a passed value is never logged or rendered.
	PassEnv []string `json:"passEnv,omitempty"`
	// Enabled defaults to true; a configured instance is on unless it says
	// otherwise.
	Enabled bool `json:"enabled"`
	// Scope is the plugin's lifecycle. Only PluginScopeGlobal is supported in
	// v1; see validatePluginInstances for why "project" is refused rather than
	// silently coerced.
	Scope string `json:"scope,omitempty"`
	// Placements are the surfaces this plugin's content can occupy, in
	// PluginPlacementTab / PluginPlacementPanes. An absent list means both.
	Placements []string `json:"placements,omitempty"`
	// Timeout is the per-call timeout for list, get, act and resolve, clamped
	// to [1s, 60s]. describe has its own fixed, shorter budget.
	Timeout time.Duration `json:"timeout,omitempty"`
	// ClaimHosts lists hostnames whose built-in URL spans this instance may
	// reclassify into resource spans. It is Sidecar instance configuration,
	// never a protocol field: the plugin does not know it exists. See
	// TerminalResourceProviderConfig.ClaimHosts for the whole-match rule.
	ClaimHosts []string `json:"claimHosts,omitempty"`
}

// HasPlacement reports whether the instance declares p.
func (p PluginInstanceConfig) HasPlacement(placement string) bool {
	for _, candidate := range p.Placements {
		if candidate == placement {
			return true
		}
	}
	return false
}

// PluginInstance is one configured instance together with the section it was
// read from. The section is not decoration: it decides which protocol
// identifier the host dispatches with, and `sidecar plugin list` shows it so a
// user with entries in both places can see which file section answered.
type PluginInstance struct {
	PluginInstanceConfig
	// Source is PluginSourceExternal or PluginSourceTerminalResources.
	Source string
}

// IsLegacyResourceProvider reports whether this instance came from the frozen
// resource protocol's config section, and therefore must be dispatched with the
// old protocol identifier.
func (p PluginInstance) IsLegacyResourceProvider() bool {
	return p.Source == PluginSourceTerminalResources
}

// PluginInstances returns every configured external plugin instance in one
// ordered list: plugins.external entries first, then the terminalResources
// providers that are not already named there.
//
// Order is precedence — for matchers, and for the order tabs and rows appear
// in — so the newer section leads. A terminalResources entry is projected onto
// the same type with the defaults its protocol implies: global scope, panes
// only, no navbar tab. It is dispatched with the old protocol identifier,
// which IsLegacyResourceProvider answers.
//
// An ID configured in both sections is one plugin, not two: plugins.external
// wins and the legacy entry is dropped. That is the migration-safe reading —
// during the release where both sections are honoured, a user who has copied an
// entry forward gets the new one rather than two child processes with one
// identity — and `sidecar plugin list` reports which section answered.
func (c *Config) PluginInstances() []PluginInstance {
	if c == nil {
		return nil
	}
	out := make([]PluginInstance, 0, len(c.Plugins.External)+len(c.TerminalResources.Providers))
	seen := make(map[string]bool, len(c.Plugins.External))
	for _, p := range c.Plugins.External {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, PluginInstance{PluginInstanceConfig: clonePluginInstance(p), Source: PluginSourceExternal})
	}
	for _, p := range c.TerminalResources.Providers {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, PluginInstance{
			PluginInstanceConfig: PluginInstanceConfig{
				ID:         p.ID,
				Command:    append([]string(nil), p.Command...),
				PassEnv:    append([]string(nil), p.PassEnv...),
				Enabled:    p.Enabled,
				Scope:      PluginScopeGlobal,
				Placements: []string{PluginPlacementPanes},
				Timeout:    p.Timeout,
				ClaimHosts: append([]string(nil), p.ClaimHosts...),
			},
			Source: PluginSourceTerminalResources,
		})
	}
	return out
}

// PluginInstance returns one configured instance by ID.
func (c *Config) PluginInstance(id string) (PluginInstance, bool) {
	for _, p := range c.PluginInstances() {
		if p.ID == id {
			return p, true
		}
	}
	return PluginInstance{}, false
}

func clonePluginInstance(p PluginInstanceConfig) PluginInstanceConfig {
	p.Command = append([]string(nil), p.Command...)
	p.PassEnv = append([]string(nil), p.PassEnv...)
	p.Placements = append([]string(nil), p.Placements...)
	p.ClaimHosts = append([]string(nil), p.ClaimHosts...)
	return p
}

// validatePluginInstances normalizes and checks plugins.external. Like the rest
// of Validate it repairs what it safely can — a clamped timeout, a trimmed ID,
// a defaulted scope — and errors on anything it cannot resolve without guessing
// at intent.
func validatePluginInstances(entries []PluginInstanceConfig) ([]PluginInstanceConfig, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	if len(entries) > MaxExternalPlugins {
		return nil, fmt.Errorf("plugins.external: %d plugins configured, the limit is %d",
			len(entries), MaxExternalPlugins)
	}

	seen := make(map[string]bool, len(entries))
	for i := range entries {
		p := &entries[i]

		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			return nil, fmt.Errorf("plugins.external: plugin %d has no id", i)
		}
		if len([]rune(p.ID)) > MaxExternalPluginIDChars {
			return nil, fmt.Errorf("plugins.external: plugin id %q is longer than %d characters",
				p.ID, MaxExternalPluginIDChars)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("plugins.external: plugin id %q is configured more than once", p.ID)
		}
		seen[p.ID] = true
		if name, ok := ReservedPluginID(p.ID); ok {
			return nil, fmt.Errorf("plugins.external: plugin id %q is already the id of Sidecar's built-in %s surface; "+
				"the id is the config key, the CLI name and the persisted tab id, so choose another one", p.ID, name)
		}

		argv := append(make([]string, 0, len(p.Command)), p.Command...)
		if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
			return nil, fmt.Errorf("plugins.external: plugin %q has no command", p.ID)
		}
		argv[0] = strings.TrimSpace(argv[0])
		p.Command = argv

		pass, err := validatePassEnv("plugins.external", p.ID, p.PassEnv)
		if err != nil {
			return nil, err
		}
		p.PassEnv = pass

		scope, err := validatePluginScope(p.ID, p.Scope)
		if err != nil {
			return nil, err
		}
		p.Scope = scope

		placements, err := validatePluginPlacements(p.ID, p.Placements)
		if err != nil {
			return nil, err
		}
		p.Placements = placements

		p.Timeout = clampTerminalResourceTimeout(p.Timeout)

		claimHosts, err := validatePluginClaimHosts(p.ID, p.ClaimHosts)
		if err != nil {
			return nil, err
		}
		p.ClaimHosts = claimHosts
	}
	return entries, nil
}

// validatePluginScope defaults an absent scope and refuses the one v1 does not
// implement.
//
// "project" is refused rather than coerced to "global" because the two are not
// the same plugin: a project-scoped plugin would be re-described on every
// project switch and would see a different world each time. Silently running it
// as global would answer a question the user did not ask. It becomes supported
// when a plugin needs it, and until then the message says what to do instead.
func validatePluginScope(id, scope string) (string, error) {
	switch strings.TrimSpace(scope) {
	case "":
		return PluginScopeGlobal, nil
	case PluginScopeGlobal:
		return PluginScopeGlobal, nil
	case PluginScopeProject:
		return "", fmt.Errorf("plugins.external: plugin %q has scope %q, which this version does not support; "+
			"remove the key (every plugin is global) and read project context in each call instead",
			id, PluginScopeProject)
	default:
		return "", fmt.Errorf("plugins.external: plugin %q has scope %q; the only supported value is %q",
			id, scope, PluginScopeGlobal)
	}
}

// validatePluginPlacements defaults an absent list to both surfaces and refuses
// an unknown one. An explicitly configured plugin is one the user asked for, so
// the default shows it wherever it can be shown; a legacy resource provider
// gets panes only, which PluginInstances applies.
func validatePluginPlacements(id string, placements []string) ([]string, error) {
	if len(placements) == 0 {
		return []string{PluginPlacementTab, PluginPlacementPanes}, nil
	}
	out := make([]string, 0, len(placements))
	seen := make(map[string]bool, len(placements))
	for _, placement := range placements {
		placement = strings.TrimSpace(placement)
		switch placement {
		case PluginPlacementTab, PluginPlacementPanes:
		default:
			return nil, fmt.Errorf("plugins.external: plugin %q placement %q is not one of %q or %q",
				id, placement, PluginPlacementTab, PluginPlacementPanes)
		}
		if seen[placement] {
			continue
		}
		seen[placement] = true
		out = append(out, placement)
	}
	return out, nil
}

// validatePassEnv is the one passEnv rule both config sections use. An inline
// value is refused loudly rather than dropped: the user needs to know the
// credential they pasted is not being passed, and that it should come out of
// the file.
func validatePassEnv(section, id string, entries []string) ([]string, error) {
	pass := make([]string, 0, len(entries))
	for _, name := range entries {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.Contains(name, "=") {
			return nil, fmt.Errorf("%s: %q passEnv entry %q looks like an inline value; passEnv takes variable names only",
				section, id, strings.SplitN(name, "=", 2)[0]+"=…")
		}
		pass = append(pass, name)
	}
	if len(pass) == 0 {
		return nil, nil
	}
	return pass, nil
}

// validatePluginClaimHosts is validateClaimHosts under the plugins.external
// section name, so the message names the section the user has to edit.
func validatePluginClaimHosts(id string, entries []string) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entries) > MaxTerminalResourceClaimHosts {
		return nil, fmt.Errorf("plugins.external: plugin %q claims %d hosts, the limit is %d",
			id, len(entries), MaxTerminalResourceClaimHosts)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		host, ok := NormalizeClaimHost(entry)
		if !ok {
			return nil, fmt.Errorf("plugins.external: plugin %q claimHosts entry %q is not a bare hostname (no scheme, port, path, or wildcard)",
				id, entry)
		}
		out = append(out, host)
	}
	return out, nil
}

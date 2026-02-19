package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/marcus/sidecar/internal/projectdir"
)

// TicketMode defines how the task field behaves with a prompt.
type TicketMode string

const (
	TicketRequired TicketMode = "required" // Task must be selected
	TicketOptional TicketMode = "optional" // Task is optional (may have fallback)
	TicketNone     TicketMode = "none"     // Task field is hidden
)

// Prompt represents a configurable prompt template.
type Prompt struct {
	Name       string     `json:"name"`
	TicketMode TicketMode `json:"ticketMode"`
	Body       string     `json:"body"`
	Source     string     `json:"-"` // "global" or "project" (set at load time)
}

// configWithPrompts is the config structure for loading prompts.
type configWithPrompts struct {
	Prompts []Prompt `json:"prompts"`
}

// LoadPrompts loads and merges prompts from global and project config directories.
// Project prompts override global prompts with the same name.
// If no config exists, creates global config with default prompts.
// Returns sorted list by name.
func LoadPrompts(globalConfigDir, projectDir string) []Prompt {
	// Load from global config
	globalPrompts := loadPromptsFromDir(globalConfigDir, "global")

	// Load from project config (centralized project directory)
	projectConfigDir, err := projectdir.Resolve(projectDir)
	if err != nil {
		projectConfigDir = "" // fallback: skip project prompts
	}
	projectPrompts := loadPromptsFromDir(projectConfigDir, "project")

	// If no prompts found, try to create defaults
	if len(globalPrompts) == 0 && len(projectPrompts) == 0 {
		if EnsureDefaultPrompts(globalConfigDir) {
			globalPrompts = loadPromptsFromDir(globalConfigDir, "global")
		}
	}

	// Merge: project overrides global by name
	merged := make(map[string]Prompt)
	for _, p := range globalPrompts {
		merged[p.Name] = p
	}
	for _, p := range projectPrompts {
		merged[p.Name] = p
	}

	// Convert to sorted slice
	result := make([]Prompt, 0, len(merged))
	for _, p := range merged {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// loadPromptsFromDir loads prompts from a config.json file in the given directory.
func loadPromptsFromDir(dir, source string) []Prompt {
	path := filepath.Join(dir, "config.json")
	prompts, err := loadPromptsFromFile(path, source)
	if err == nil && len(prompts) > 0 {
		return prompts
	}
	return nil
}

// loadPromptsFromFile loads prompts from a JSON config file.
func loadPromptsFromFile(path, source string) ([]Prompt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg configWithPrompts
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Set source on all prompts
	for i := range cfg.Prompts {
		cfg.Prompts[i].Source = source
		// Default ticketMode to optional if not specified
		if cfg.Prompts[i].TicketMode == "" {
			cfg.Prompts[i].TicketMode = TicketOptional
		}
	}

	return cfg.Prompts, nil
}

// fallbackPattern matches {{ticket || 'fallback text'}}
var fallbackPattern = regexp.MustCompile(`\{\{ticket\s*\|\|\s*'([^']*)'\}\}`)

// ExtractFallback extracts the fallback value from a prompt body.
// Returns the first fallback found, or empty string if none.
func ExtractFallback(body string) string {
	matches := fallbackPattern.FindStringSubmatch(body)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// HasTicketPlaceholder returns true if the body contains {{ticket}} or {{ticket || '...'}}
func HasTicketPlaceholder(body string) bool {
	return strings.Contains(body, "{{ticket")
}

// DefaultPrompts returns the built-in default prompts.
// These serve as examples users can modify.
func DefaultPrompts() []Prompt {
	return []Prompt{
		{
			Name:       "Begin Work on Ticket",
			TicketMode: TicketRequired,
			Body:       "Start work on {{ticket}}. Use td to track progress.",
			Source:     "default",
		},
		{
			Name:       "Code Review Ticket",
			TicketMode: TicketRequired,
			Body:       "Do a detailed code review of {{ticket}}. Focus on correctness and tests.",
			Source:     "default",
		},
		{
			Name:       "Plan to Epic (No Impl)",
			TicketMode: TicketNone,
			Body:       "Plan this task into an epic with sub-tasks using td. Do not implement yet.",
			Source:     "default",
		},
		{
			Name:       "Plan to Epic + Implement",
			TicketMode: TicketNone,
			Body:       "Plan this task into an epic with sub-tasks using td, then implement them.",
			Source:     "default",
		},
		{
			Name:       "TD Review Session",
			TicketMode: TicketNone,
			Body: `Start a td review session. Review open tasks, fix obvious bugs immediately, create tasks for larger issues.

Use: td usage --new-session`,
			Source: "default",
		},
	}
}

// EnsureDefaultPrompts creates the global config with default prompts if no config exists.
// Returns true if defaults were created.
func EnsureDefaultPrompts(globalConfigDir string) bool {
	// Check if config.json exists
	path := filepath.Join(globalConfigDir, "config.json")
	if _, err := os.Stat(path); err == nil {
		return false // Config exists, don't overwrite
	}

	// Create directory if needed
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		return false
	}

	// Write default config as JSON
	cfg := configWithPrompts{Prompts: DefaultPrompts()}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return false
	}

	return true
}

// WriteDefaultPromptsToConfig merges default prompts into the global config.
// If config.json exists, it preserves other fields and adds/replaces the prompts key.
// If config.json does not exist, it creates one with default prompts.
// Returns true on success.
func WriteDefaultPromptsToConfig(globalConfigDir string) bool {
	path := filepath.Join(globalConfigDir, "config.json")

	// Create directory if needed
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		return false
	}

	// Read existing config as raw JSON to preserve unknown fields
	var raw map[string]json.RawMessage
	if data, err := os.ReadFile(path); err == nil {
		if jsonErr := json.Unmarshal(data, &raw); jsonErr != nil {
			raw = make(map[string]json.RawMessage)
		}
	} else {
		raw = make(map[string]json.RawMessage)
	}

	// Marshal default prompts and set in config
	defaults := DefaultPrompts()
	promptsData, err := json.Marshal(defaults)
	if err != nil {
		return false
	}
	raw["prompts"] = promptsData

	// Write merged config
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return false
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return false
	}

	return true
}

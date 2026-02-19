package workspace

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/projectdir"
)

func TestPromptPickerDKeyEmptyPrompts(t *testing.T) {
	// When no prompts are configured, 'd' should emit PromptInstallDefaultsMsg
	pp := NewPromptPicker(nil, 80, 24)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	_, cmd := pp.Update(msg)

	if cmd == nil {
		t.Fatal("Expected cmd from 'd' key with empty prompts, got nil")
	}

	result := cmd()
	if _, ok := result.(PromptInstallDefaultsMsg); !ok {
		t.Errorf("Expected PromptInstallDefaultsMsg, got %T", result)
	}
}

func TestPromptPickerDKeyWithPrompts(t *testing.T) {
	// When prompts exist and filter is focused, 'd' should go to filter input (not install defaults)
	prompts := []Prompt{{Name: "test", Body: "test body", TicketMode: TicketNone}}
	pp := NewPromptPicker(prompts, 80, 24)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	_, cmd := pp.Update(msg)

	// Should not emit PromptInstallDefaultsMsg
	if cmd != nil {
		result := cmd()
		if _, ok := result.(PromptInstallDefaultsMsg); ok {
			t.Error("Should not emit PromptInstallDefaultsMsg when prompts exist")
		}
	}

	// Filter should now contain 'd'
	if pp.filterInput.Value() != "d" {
		t.Errorf("Expected filter to contain 'd', got %q", pp.filterInput.Value())
	}
}

func TestPromptPickerEscEmptyPrompts(t *testing.T) {
	// Esc should still cancel even with empty prompts
	pp := NewPromptPicker(nil, 80, 24)

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	_, cmd := pp.Update(msg)

	if cmd == nil {
		t.Fatal("Expected cmd from Esc, got nil")
	}

	result := cmd()
	if _, ok := result.(PromptCancelledMsg); !ok {
		t.Errorf("Expected PromptCancelledMsg, got %T", result)
	}
}

func TestPromptPickerEnterEmptyPrompts(t *testing.T) {
	// Enter should select "none" even with empty prompts
	pp := NewPromptPicker(nil, 80, 24)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := pp.Update(msg)

	if cmd == nil {
		t.Fatal("Expected cmd from Enter, got nil")
	}

	result := cmd()
	if selected, ok := result.(PromptSelectedMsg); !ok {
		t.Errorf("Expected PromptSelectedMsg, got %T", result)
	} else if selected.Prompt != nil {
		t.Error("Expected nil Prompt (none selected)")
	}
}

func TestInstallDefaultsRoundTrip(t *testing.T) {
	// Exercises the same code path as the PromptInstallDefaultsMsg handler:
	// WriteDefaultPromptsToConfig → LoadPrompts → NewPromptPicker
	configDir := t.TempDir()
	workDir := t.TempDir()

	// Simulate: no prompts exist, user presses 'd'
	if !WriteDefaultPromptsToConfig(configDir) {
		t.Fatal("WriteDefaultPromptsToConfig failed")
	}

	prompts := LoadPrompts(configDir, workDir)
	if len(prompts) != 5 {
		t.Fatalf("Expected 5 prompts after install, got %d", len(prompts))
	}

	pp := NewPromptPicker(prompts, 80, 24)

	// 'd' should now go to filter (prompts exist), not install defaults
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	_, cmd := pp.Update(msg)
	if cmd != nil {
		result := cmd()
		if _, ok := result.(PromptInstallDefaultsMsg); ok {
			t.Error("Should not emit PromptInstallDefaultsMsg after defaults installed")
		}
	}
}

func TestInstallDefaultsPreservesProjectPrompts(t *testing.T) {
	// Verify that after installing defaults, project prompts still override
	configDir := t.TempDir()
	projectRoot := t.TempDir()

	// Set up config path so projectdir resolves to our temp dir
	configBase := t.TempDir()
	config.SetTestConfigPath(filepath.Join(configBase, "config.json"))
	t.Cleanup(config.ResetTestConfigPath)

	// Resolve project dir and create project config with an override
	projDir, err := projectdir.Resolve(projectRoot)
	if err != nil {
		t.Fatalf("Failed to resolve project dir: %v", err)
	}
	projectConfig := `{"prompts": [{"name": "Begin Work on Ticket", "body": "Custom project override"}]}`
	_ = os.WriteFile(filepath.Join(projDir, "config.json"), []byte(projectConfig), 0644)

	// Install defaults to global
	WriteDefaultPromptsToConfig(configDir)

	// Load merged prompts
	prompts := LoadPrompts(configDir, projectRoot)

	// Find "Begin Work on Ticket" - should have project body
	for _, p := range prompts {
		if p.Name == "Begin Work on Ticket" {
			if p.Body != "Custom project override" {
				t.Errorf("Expected project override body, got %q", p.Body)
			}
			if p.Source != "project" {
				t.Errorf("Expected source 'project', got %q", p.Source)
			}
			return
		}
	}
	t.Error("Missing 'Begin Work on Ticket' prompt in merged results")
}

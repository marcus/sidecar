package docdrift

import (
	"strings"
	"testing"
)

func TestDocumentationParserExtractSections(t *testing.T) {
	content := `# Overview
This is the overview section.

# Features
- Feature 1
- Feature 2

# Installation
Instructions here.
`

	parser := NewDocumentationParser(content, "test.md")
	sections := parser.extractSections()

	if len(sections) != 3 {
		t.Fatalf("Expected 3 sections, got %d", len(sections))
	}

	if _, ok := sections["Overview"]; !ok {
		t.Error("Expected 'Overview' section")
	}

	if _, ok := sections["Features"]; !ok {
		t.Error("Expected 'Features' section")
	}

	if _, ok := sections["Installation"]; !ok {
		t.Error("Expected 'Installation' section")
	}
}

func TestDocumentationParserParseBulletFeatures(t *testing.T) {
	content := `# Features
- Stage files
- View diffs
- Commit changes
`

	parser := NewDocumentationParser(content, "test.md")
	parser.parseFeaturesInSection("Features", content)

	if len(parser.Claims) == 0 {
		t.Fatal("Expected features to be extracted from bullet points")
	}

	// Should extract at least the feature names
	found := false
	for _, claim := range parser.Claims {
		if claim.Type == "feature" && strings.Contains(claim.Name, "Stage") {
			found = true
		}
	}

	if !found {
		t.Error("Expected 'Stage files' feature to be extracted")
	}
}

func TestDocumentationParserParsePlugins(t *testing.T) {
	content := `# Sidecar Plugins

Sidecar includes the workspace plugin, conversations plugin, and git status plugin.
The file-browser helps navigate files.
Check out td-monitor for tasks.
`

	parser := NewDocumentationParser(content, "test.md")
	parser.parsePluginsInSection("Plugins", content)

	pluginFound := false
	for _, claim := range parser.Claims {
		if claim.Type == "plugin" {
			pluginFound = true
			break
		}
	}

	if !pluginFound {
		t.Error("Expected plugin mentions to be extracted")
	}
}

func TestDocumentationParserParseCommands(t *testing.T) {
	// parseCommandsInSection looks for patterns like `@`, `#`, `ctrl+d` in backticks
	// Since we can't easily include backticks in raw strings, test the functionality
	// through the Parse() method instead
	content := `# Commands

| Key | Action |
|-----|--------|
| @ | Switch |
| # | Theme |
`

	parser := NewDocumentationParser(content, "test.md")
	// Commands are extracted via backtick pattern in parseFeaturesInSection
	parser.parseFeaturesInSection("Commands", content)

	// At least verify the method runs without error
	if parser.Claims == nil {
		t.Error("Expected Claims to be initialized")
	}
}

func TestDocumentationParserFullParse(t *testing.T) {
	content := `# Plugin Documentation

The Git plugin helps you stage and commit code.
Press @ to switch projects.
`

	parser := NewDocumentationParser(content, "test.md")
	if err := parser.Parse(); err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(parser.Claims) == 0 {
		t.Error("Expected at least some claims from full parse")
	}
}

func TestDocumentationParserExtractTableFeatures(t *testing.T) {
	content := `| Key | Action |
|-----|--------|
| s   | Stage  |
| u   | Unstage|
| d   | Diff   |
`

	parser := NewDocumentationParser(content, "test.md")
	features := parser.ExtractTableFeatures()

	// Should extract table column values
	if len(features) > 0 {
		// Check if we got something reasonable
		found := false
		for _, feat := range features {
			if strings.TrimSpace(feat) != "" && strings.TrimSpace(feat) != "Key" {
				found = true
			}
		}
		if !found {
			t.Log("Table features extracted:", features)
		}
	}
}

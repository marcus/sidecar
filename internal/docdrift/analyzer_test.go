package docdrift

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodeAnalyzerAnalyzePackage(t *testing.T) {
	// Create a temporary directory with test Go files
	tmpDir := t.TempDir()

	// Create a simple Go package for testing
	packageDir := filepath.Join(tmpDir, "testpkg")
	if err := os.Mkdir(packageDir, 0755); err != nil {
		t.Fatalf("Failed to create test package directory: %v", err)
	}

	// Write a test Go file
	testFile := filepath.Join(packageDir, "test.go")
	content := `package testpkg

type PublicType struct {
    Field string
}

type privateType struct{}

func PublicFunc() {}

func privateFunc() {}

const PublicConst = "value"
const privateConst = "value"

var PublicVar = "value"
var privateVar = "value"
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Analyze the package
	analyzer := NewCodeAnalyzer(tmpDir)
	if err := analyzer.AnalyzePackage(packageDir); err != nil {
		t.Fatalf("AnalyzePackage failed: %v", err)
	}

	// Verify results
	if len(analyzer.Features) == 0 {
		t.Fatal("Expected features to be extracted")
	}

	// Check that public items are extracted
	featureNames := make(map[string]bool)
	for _, feat := range analyzer.Features {
		featureNames[feat.Name] = true
	}

	expectedFeatures := []string{"PublicType", "PublicFunc", "PublicConst", "PublicVar"}
	for _, expected := range expectedFeatures {
		if !featureNames[expected] {
			t.Errorf("Expected feature %s not found", expected)
		}
	}

	// Check that private items are not extracted
	unexpectedFeatures := []string{"privateType", "privateFunc", "privateConst", "privateVar"}
	for _, unexpected := range unexpectedFeatures {
		if featureNames[unexpected] {
			t.Errorf("Unexpected feature %s was extracted", unexpected)
		}
	}
}

func TestExtractPluginNames(t *testing.T) {
	tmpDir := t.TempDir()

	// Create plugin directories
	pluginsDir := filepath.Join(tmpDir, "internal", "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatalf("Failed to create plugins directory: %v", err)
	}

	pluginNames := []string{"gitstatus", "conversations", "filebrowser"}
	for _, name := range pluginNames {
		if err := os.Mkdir(filepath.Join(pluginsDir, name), 0755); err != nil {
			t.Fatalf("Failed to create plugin directory: %v", err)
		}
	}

	// Extract plugin names
	analyzer := NewCodeAnalyzer(tmpDir)
	plugins, err := analyzer.ExtractPluginNames()
	if err != nil {
		t.Fatalf("ExtractPluginNames failed: %v", err)
	}

	if len(plugins) != len(pluginNames) {
		t.Errorf("Expected %d plugins, got %d", len(pluginNames), len(plugins))
	}

	pluginSet := make(map[string]bool)
	for _, plugin := range plugins {
		pluginSet[plugin] = true
	}

	for _, expected := range pluginNames {
		if !pluginSet[expected] {
			t.Errorf("Expected plugin %s not found", expected)
		}
	}
}

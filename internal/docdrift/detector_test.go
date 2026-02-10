package docdrift

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectorDetect(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal structure
	err := os.MkdirAll(filepath.Join(tmpDir, "internal", "plugins", "test-plugin"), 0755)
	if err != nil {
		t.Fatalf("Failed to create test structure: %v", err)
	}

	// Create a simple Go file
	codeDir := filepath.Join(tmpDir, "internal", "test")
	err = os.MkdirAll(codeDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create code directory: %v", err)
	}

	codeFile := filepath.Join(codeDir, "test.go")
	content := `package test

type PublicType struct {}
func PublicFunc() {}
`
	err = os.WriteFile(codeFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write code file: %v", err)
	}

	// Create documentation
	docFile := filepath.Join(tmpDir, "README.md")
	docContent := `# Test Project

This documents the PublicType.
`
	err = os.WriteFile(docFile, []byte(docContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write doc file: %v", err)
	}

	// Run detector
	config := Config{
		ProjectRoot:  tmpDir,
		OutputFormat: FormatText,
	}

	detector := NewDetector(config)
	err = detector.Detect()
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Verify report was generated
	if detector.Report == nil {
		t.Fatal("Expected report to be generated")
	}

	// Should have some gaps (PublicFunc not documented)
	if len(detector.Report.Gaps) == 0 {
		t.Error("Expected gaps to be detected")
	}
}

func TestDetectorGetDefaultPackages(t *testing.T) {
	detector := NewDetector(Config{})
	packages := detector.getDefaultPackages()

	if len(packages) == 0 {
		t.Fatal("Expected default packages to be returned")
	}

	// Check that internal packages are included
	found := false
	for _, pkg := range packages {
		if pkg == "internal/plugin" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected 'internal/plugin' in default packages")
	}
}

func TestDetectorGetDefaultDocPaths(t *testing.T) {
	detector := NewDetector(Config{})
	paths := detector.getDefaultDocPaths()

	if len(paths) == 0 {
		t.Fatal("Expected default doc paths to be returned")
	}

	// Check that README is included
	found := false
	for _, path := range paths {
		if path == "README.md" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected 'README.md' in default doc paths")
	}
}

func TestDetectorProcessDocDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a docs directory with markdown files
	docsDir := filepath.Join(tmpDir, "docs")
	err := os.Mkdir(docsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Create a markdown file
	mdFile := filepath.Join(docsDir, "test.md")
	mdContent := `# Test Documentation

- Feature1
- Feature2
`
	err = os.WriteFile(mdFile, []byte(mdContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write markdown file: %v", err)
	}

	// Process directory
	detector := NewDetector(Config{ProjectRoot: tmpDir})
	var claims []DocumentationClaim
	err = detector.processDocDirectory(docsDir, &claims)
	if err != nil {
		t.Fatalf("processDocDirectory failed: %v", err)
	}

	if len(claims) == 0 {
		t.Error("Expected claims to be extracted from markdown files")
	}
}

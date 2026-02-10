package docdrift

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config configures the drift detector.
type Config struct {
	ProjectRoot     string
	DocPaths        []string // Paths to documentation files
	CodePackages    []string // Go packages to analyze
	IncludePrivate  bool
	OutputFormat    ReportFormat
}

// Detector orchestrates the drift detection process.
type Detector struct {
	Config Config
	Report *Report
}

// NewDetector creates a new detector.
func NewDetector(config Config) *Detector {
	return &Detector{
		Config: config,
	}
}

// Detect runs the full drift detection process.
func (d *Detector) Detect() error {
	// Analyze code features
	codeFeatures, err := d.extractCodeFeatures()
	if err != nil {
		return fmt.Errorf("failed to extract code features: %w", err)
	}

	// Parse documentation
	docClaims, err := d.extractDocumentation()
	if err != nil {
		return fmt.Errorf("failed to extract documentation: %w", err)
	}

	// Compare and generate report
	comparator := NewComparator(codeFeatures, docClaims)
	d.Report = comparator.Compare()

	return nil
}

// extractCodeFeatures analyzes all code packages.
func (d *Detector) extractCodeFeatures() ([]CodeFeature, error) {
	var allFeatures []CodeFeature

	// If no packages specified, use default internal packages
	packages := d.Config.CodePackages
	if len(packages) == 0 {
		packages = d.getDefaultPackages()
	}

	for _, pkg := range packages {
		pkgPath := filepath.Join(d.Config.ProjectRoot, pkg)
		if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
			continue
		}

		analyzer := NewCodeAnalyzer(d.Config.ProjectRoot)
		if err := analyzer.AnalyzePackage(pkgPath); err != nil {
			// Log but don't fail on individual packages
			continue
		}

		allFeatures = append(allFeatures, analyzer.Features...)
	}

	// Extract plugin names
	analyzer := NewCodeAnalyzer(d.Config.ProjectRoot)
	plugins, err := analyzer.ExtractPluginNames()
	if err == nil {
		for _, plugin := range plugins {
			allFeatures = append(allFeatures, CodeFeature{
				Name:       plugin,
				Type:       "plugin",
				Package:    "plugins",
				SourceFile: "plugin.go",
				IsExported: true,
			})
		}
	}

	return allFeatures, nil
}

// extractDocumentation parses all documentation files.
func (d *Detector) extractDocumentation() ([]DocumentationClaim, error) {
	var allClaims []DocumentationClaim

	docPaths := d.Config.DocPaths
	if len(docPaths) == 0 {
		docPaths = d.getDefaultDocPaths()
	}

	for _, docPath := range docPaths {
		fullPath := filepath.Join(d.Config.ProjectRoot, docPath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}

		// If it's a directory, scan for markdown files
		if info, err := os.Stat(fullPath); err == nil && info.IsDir() {
			if err := d.processDocDirectory(fullPath, &allClaims); err != nil {
				return nil, err
			}
		} else {
			// Single file
			if strings.HasSuffix(fullPath, ".md") {
				content, err := os.ReadFile(fullPath)
				if err != nil {
					return nil, err
				}

				parser := NewDocumentationParser(string(content), docPath)
				if err := parser.Parse(); err != nil {
					return nil, err
				}
				allClaims = append(allClaims, parser.Claims...)
			}
		}
	}

	return allClaims, nil
}

// processDocDirectory recursively processes markdown files in a directory.
func (d *Detector) processDocDirectory(dirPath string, claims *[]DocumentationClaim) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dirPath, entry.Name())

		if entry.IsDir() {
			// Recurse into subdirectories
			if !strings.HasPrefix(entry.Name(), ".") {
				if err := d.processDocDirectory(fullPath, claims); err != nil {
					return err
				}
			}
			continue
		}

		// Process markdown files
		if strings.HasSuffix(entry.Name(), ".md") {
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}

			relPath, _ := filepath.Rel(d.Config.ProjectRoot, fullPath)
			parser := NewDocumentationParser(string(content), relPath)
			if err := parser.Parse(); err != nil {
				continue
			}
			*claims = append(*claims, parser.Claims...)
		}
	}

	return nil
}

// getDefaultPackages returns default packages to analyze.
func (d *Detector) getDefaultPackages() []string {
	return []string{
		"internal/app",
		"internal/plugin",
		"internal/plugins",
		"cmd/sidecar",
	}
}

// getDefaultDocPaths returns default documentation paths.
func (d *Detector) getDefaultDocPaths() []string {
	return []string{
		"README.md",
		"docs",
	}
}

// GetFormattedReport returns the formatted report.
func (d *Detector) GetFormattedReport() string {
	reporter := NewReporter(d.Report, d.Config.OutputFormat)
	return reporter.Generate()
}

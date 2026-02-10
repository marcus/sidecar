package docdrift

import (
	"regexp"
	"strings"
)

// DocumentationClaim represents a claim made in documentation.
type DocumentationClaim struct {
	Name     string   // Feature name
	Type     string   // "feature", "plugin", "command", etc.
	Section  string   // Markdown section where it's mentioned
	Document string   // Source document path
	Keywords []string // Associated keywords
}

// DocumentationParser extracts claims from markdown documentation.
type DocumentationParser struct {
	Content string
	Path    string
	Claims  []DocumentationClaim
}

// NewDocumentationParser creates a new documentation parser.
func NewDocumentationParser(content string, path string) *DocumentationParser {
	return &DocumentationParser{
		Content: content,
		Path:    path,
		Claims:  []DocumentationClaim{},
	}
}

// Parse extracts claims from markdown content.
func (dp *DocumentationParser) Parse() error {
	sections := dp.extractSections()
	for sectionName, sectionContent := range sections {
		dp.parseFeaturesInSection(sectionName, sectionContent)
		dp.parsePluginsInSection(sectionName, sectionContent)
		dp.parseCommandsInSection(sectionName, sectionContent)
	}
	// Deduplicate claims after parsing all sections
	dp.deduplicateClaims()
	return nil
}

// extractSections splits markdown into sections by heading.
func (dp *DocumentationParser) extractSections() map[string]string {
	sections := make(map[string]string)
	lines := strings.Split(dp.Content, "\n")

	var currentSection string
	var currentContent strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			if currentSection != "" {
				sections[currentSection] = currentContent.String()
			}
			currentSection = strings.TrimPrefix(strings.TrimSpace(line), "#")
			currentSection = strings.TrimSpace(currentSection)
			currentContent.Reset()
		} else {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	if currentSection != "" {
		sections[currentSection] = currentContent.String()
	}

	return sections
}

// parseFeaturesInSection extracts feature mentions from a section.
func (dp *DocumentationParser) parseFeaturesInSection(section, content string) {
	// Look for bullet points and feature descriptions
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Match bullet points with feature descriptions
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			featureName := strings.TrimPrefix(line, "-")
			featureName = strings.TrimPrefix(featureName, "*")
			featureName = strings.TrimSpace(featureName)

			// Extract just the feature name (before any description)
			if colon := strings.Index(featureName, ":"); colon > 0 {
				featureName = featureName[:colon]
			}

			if featureName != "" {
				dp.Claims = append(dp.Claims, DocumentationClaim{
					Name:     featureName,
					Type:     "feature",
					Section:  section,
					Document: dp.Path,
				})
			}
		}

		// Look for backtick-wrapped names (code references)
		backtickPattern := regexp.MustCompile("`([a-zA-Z_][a-zA-Z0-9_]*)`")
		matches := backtickPattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) > 1 {
				dp.Claims = append(dp.Claims, DocumentationClaim{
					Name:     match[1],
					Type:     "reference",
					Section:  section,
					Document: dp.Path,
				})
			}
		}
	}
}

// parsePluginsInSection extracts plugin mentions from a section.
func (dp *DocumentationParser) parsePluginsInSection(section, content string) {
	// Look for plugin names in headers and links with word boundaries
	pluginPattern := regexp.MustCompile(`\b(?i)(plugin|workspace|git|conversations|file-?browser|td-?monitor|notes)\b`)
	matches := pluginPattern.FindAllString(content, -1)

	for _, match := range matches {
		normalizedName := strings.ToLower(match)
		normalizedName = strings.ReplaceAll(normalizedName, " ", "-")

		dp.Claims = append(dp.Claims, DocumentationClaim{
			Name:     normalizedName,
			Type:     "plugin",
			Section:  section,
			Document: dp.Path,
		})
	}
}

// parseCommandsInSection extracts keyboard command mentions.
func (dp *DocumentationParser) parseCommandsInSection(section, content string) {
	// Look for keyboard commands in backticks - more specific patterns
	commandPattern := regexp.MustCompile("`((?:ctrl|cmd|alt|shift|@|#)(?:\\+\\w+)*|[a-zA-Z0-9@#])`")
	matches := commandPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			cmd := match[1]
			if cmd != "" {
				dp.Claims = append(dp.Claims, DocumentationClaim{
					Name:     cmd,
					Type:     "command",
					Section:  section,
					Document: dp.Path,
				})
			}
		}
	}
}

// deduplicateClaims removes duplicate claims based on name, type, and document.
func (dp *DocumentationParser) deduplicateClaims() {
	seen := make(map[string]bool)
	var unique []DocumentationClaim

	for _, claim := range dp.Claims {
		// Create a unique key combining name, type, and document
		key := claim.Name + "|" + claim.Type + "|" + claim.Document
		if !seen[key] {
			seen[key] = true
			unique = append(unique, claim)
		}
	}

	dp.Claims = unique
}

// ExtractTableFeatures extracts feature names from markdown tables.
func (dp *DocumentationParser) ExtractTableFeatures() []string {
	var features []string
	lines := strings.Split(dp.Content, "\n")

	inTable := false
	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Detect table start
		if strings.HasPrefix(line, "|") {
			inTable = true
			continue
		}

		// Detect table separator
		if inTable && strings.Count(line, "|") > 0 && strings.Contains(line, "-") {
			// Next line might be table data
			if i+1 < len(lines) {
				continue
			}
		}

		// End table on blank line
		if inTable && line == "" {
			inTable = false
		}

		// Parse table rows
		if inTable && strings.HasPrefix(line, "|") {
			parts := strings.Split(line, "|")
			// First column typically contains the feature name
			if len(parts) > 1 {
				featureName := strings.TrimSpace(parts[1])
				if featureName != "" {
					features = append(features, featureName)
				}
			}
		}
	}

	return features
}

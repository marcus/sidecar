package run

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RunCommand represents a detected or configured runnable command.
type RunCommand struct {
	Name    string // Display name (e.g., "build", "test")
	Command string // Shell command to execute
	Source  string // Source label (e.g., "Makefile", "npm", "docker", "config")
	Group   string // Grouping key for display
}

// detectCommands scans the working directory for runnable commands.
func detectCommands(workDir string) []RunCommand {
	var commands []RunCommand

	commands = append(commands, detectMakefile(workDir)...)
	commands = append(commands, detectPackageJSON(workDir)...)
	commands = append(commands, detectDockerCompose(workDir)...)
	commands = append(commands, detectPyproject(workDir)...)

	return commands
}

// makefileTargetPattern matches non-hidden Makefile targets (lines like "target:" without leading tab/dot).
var makefileTargetPattern = regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9_.-]*)\s*:`)

// detectMakefile parses a Makefile for targets.
func detectMakefile(workDir string) []RunCommand {
	path := filepath.Join(workDir, "Makefile")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var commands []RunCommand
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		matches := makefileTargetPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		target := matches[1]
		// Skip common internal/hidden targets
		if strings.HasPrefix(target, ".") || target == "PHONY" {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		commands = append(commands, RunCommand{
			Name:    target,
			Command: "make " + target,
			Source:  "Makefile",
			Group:   "make",
		})
	}
	return commands
}

// detectPackageManager returns the package manager runner command and label
// by checking for lockfiles in priority order.
func detectPackageManager(workDir string) (runner string, label string) {
	// Check lockfiles in order of specificity
	lockfiles := []struct {
		file   string
		runner string
		label  string
	}{
		{"bun.lockb", "bun run", "bun"},
		{"bun.lock", "bun run", "bun"},
		{"pnpm-lock.yaml", "pnpm run", "pnpm"},
		{"yarn.lock", "yarn", "yarn"}, // yarn doesn't need "run" for scripts
		{"package-lock.json", "npm run", "npm"},
	}
	for _, lf := range lockfiles {
		if _, err := os.Stat(filepath.Join(workDir, lf.file)); err == nil {
			return lf.runner, lf.label
		}
	}
	// Default to npm if no lockfile found
	return "npm run", "npm"
}

// detectPackageJSON parses package.json for scripts.
func detectPackageJSON(workDir string) []RunCommand {
	path := filepath.Join(workDir, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	runner, label := detectPackageManager(workDir)

	var commands []RunCommand
	// Sort script names for stable ordering
	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		commands = append(commands, RunCommand{
			Name:    name,
			Command: runner + " " + name,
			Source:  label,
			Group:   label,
		})
	}
	return commands
}

// detectDockerCompose parses docker-compose.yml for services.
func detectDockerCompose(workDir string) []RunCommand {
	// Try both filenames
	var data []byte
	var err error
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		data, err = os.ReadFile(filepath.Join(workDir, name))
		if err == nil {
			break
		}
	}
	if data == nil {
		return nil
	}

	// Simple YAML parsing: look for top-level "services:" and then indented service names
	var commands []RunCommand
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inServices := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "services:" {
			inServices = true
			continue
		}

		if inServices {
			// A service is a line with exactly 2 spaces of indent followed by "name:"
			if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && strings.HasSuffix(trimmed, ":") {
				serviceName := strings.TrimSuffix(trimmed, ":")
				commands = append(commands, RunCommand{
					Name:    serviceName,
					Command: "docker compose up " + serviceName,
					Source:  "docker",
					Group:   "docker",
				})
			} else if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
				// Hit another top-level key, stop parsing services
				inServices = false
			}
		}
	}
	return commands
}

// detectPythonPackageManager returns the runner command and label
// by checking for Python lockfiles/markers.
func detectPythonPackageManager(workDir string) (runner string, label string) {
	// Check lockfiles in order of specificity
	lockfiles := []struct {
		file   string
		runner string
		label  string
	}{
		{"uv.lock", "uv run", "uv"},
		{"poetry.lock", "poetry run", "poetry"},
	}
	for _, lf := range lockfiles {
		if _, err := os.Stat(filepath.Join(workDir, lf.file)); err == nil {
			return lf.runner, lf.label
		}
	}
	// Default to uv if no lockfile found
	return "uv run", "uv"
}

// detectPyproject parses pyproject.toml for script entries.
// Checks both [project.scripts] (PEP 621) and [tool.poetry.scripts] (Poetry).
func detectPyproject(workDir string) []RunCommand {
	path := filepath.Join(workDir, "pyproject.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	runner, label := detectPythonPackageManager(workDir)

	// Script section headers to look for
	scriptSections := []string{
		"[project.scripts]",
		"[tool.poetry.scripts]",
	}

	seen := make(map[string]bool)
	var commands []RunCommand
	content := string(data)

	for _, section := range scriptSections {
		parsed := parseTomlSection(content, section)
		for _, name := range parsed {
			if seen[name] {
				continue
			}
			seen[name] = true
			commands = append(commands, RunCommand{
				Name:    name,
				Command: runner + " " + name,
				Source:  label,
				Group:   label,
			})
		}
	}
	return commands
}

// parseTomlSection extracts key names from a TOML section.
// Returns sorted key names found under the given section header.
func parseTomlSection(content string, sectionHeader string) []string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inSection := false
	var names []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == sectionHeader {
			inSection = true
			continue
		}

		if inSection {
			// New section starts
			if strings.HasPrefix(trimmed, "[") {
				break
			}
			// Parse "name = ..." lines
			if strings.Contains(trimmed, "=") && !strings.HasPrefix(trimmed, "#") {
				parts := strings.SplitN(trimmed, "=", 2)
				name := strings.TrimSpace(parts[0])
				if name != "" {
					names = append(names, name)
				}
			}
		}
	}

	sort.Strings(names)
	return names
}

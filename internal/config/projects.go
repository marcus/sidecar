package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Project mutations live here so every surface that adds, renames, removes, or
// reorders a project asks the same questions and writes through the same
// Load→mutate→Save boundary. The validation half is deliberately state-free: a
// non-interactive caller can adopt it unchanged.

// ValidateProject reports why a name/path pair may not be added, or "" when it
// is fine. skipIndex excludes one existing entry from the uniqueness checks, so
// editing a project does not collide with itself; pass -1 when adding.
//
// The returned string is the message a user reads, so it says what is wrong in
// plain language rather than naming a field.
func ValidateProject(existing []ProjectConfig, name, path string, skipIndex int) string {
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)

	if name == "" {
		return "Name is required"
	}
	if path == "" {
		return "Path is required"
	}

	expanded := ExpandPath(path)
	info, err := os.Stat(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			return "Path does not exist"
		}
		return "Cannot access path"
	}
	if !info.IsDir() {
		return "Path is not a directory"
	}

	for i, project := range existing {
		if i == skipIndex {
			continue
		}
		if strings.EqualFold(project.Name, name) {
			return "Project name already exists"
		}
		if project.Path == expanded {
			return "Project path already configured"
		}
	}
	return ""
}

// ValidateProjectName is the rename check: the path is already known good, so
// only the name is questioned.
func ValidateProjectName(existing []ProjectConfig, name string, skipIndex int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Name is required"
	}
	for i, project := range existing {
		if i == skipIndex {
			continue
		}
		if strings.EqualFold(project.Name, name) {
			return "Project name already exists"
		}
	}
	return ""
}

// AddProject appends a project to the configuration on disk and returns the
// saved entry. It reloads first, so a project added here never clobbers a
// change made to the file since Sidecar started.
func AddProject(project ProjectConfig) (ProjectConfig, error) {
	project.Name = strings.TrimSpace(project.Name)
	project.Path = ExpandPath(strings.TrimSpace(project.Path))

	cfg, err := Load()
	if err != nil {
		return ProjectConfig{}, err
	}
	if message := ValidateProject(cfg.Projects.List, project.Name, project.Path, -1); message != "" {
		return ProjectConfig{}, fmt.Errorf("%s", message)
	}
	// Registration is the one date Sidecar can state truthfully, and this is the
	// single boundary every surface adds a project through, so stamping it here
	// covers the TUI and `sidecar project add` without either knowing about it.
	// A caller that supplied its own value keeps it.
	if project.AddedAt == nil {
		now := time.Now().UTC()
		project.AddedAt = &now
	}
	cfg.Projects.List = append(cfg.Projects.List, project)
	if err := Save(cfg); err != nil {
		return ProjectConfig{}, err
	}
	return project, nil
}

// UpdateProject applies a change to the project at a path. mutate receives the
// live entry from a freshly loaded configuration.
//
// Removing or renaming a project deliberately leaves state.json's workdir-keyed
// entries alone: a stale key is harmless, and rewriting per-surface UI state as
// a side effect of a settings change is not.
func UpdateProject(projectPath string, mutate func(*ProjectConfig)) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	target := ExpandPath(projectPath)
	for i := range cfg.Projects.List {
		if cfg.Projects.List[i].Path != target {
			continue
		}
		mutate(&cfg.Projects.List[i])
		return Save(cfg)
	}
	return fmt.Errorf("project not found: %s", projectPath)
}

// RemoveProject drops a project from the configured list.
func RemoveProject(projectPath string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	target := ExpandPath(projectPath)
	for i := range cfg.Projects.List {
		if cfg.Projects.List[i].Path != target {
			continue
		}
		cfg.Projects.List = append(cfg.Projects.List[:i], cfg.Projects.List[i+1:]...)
		return Save(cfg)
	}
	return fmt.Errorf("project not found: %s", projectPath)
}

// MoveProject changes a project's position in the list by delta. The list order
// is what the switcher and the Projects page show, so it is a real setting.
func MoveProject(projectPath string, delta int) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	target := ExpandPath(projectPath)
	index := -1
	for i := range cfg.Projects.List {
		if cfg.Projects.List[i].Path == target {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("project not found: %s", projectPath)
	}
	moved := index + delta
	if moved < 0 || moved >= len(cfg.Projects.List) || delta == 0 {
		return nil
	}
	list := cfg.Projects.List
	list[index], list[moved] = list[moved], list[index]
	cfg.Projects.List = list
	return Save(cfg)
}

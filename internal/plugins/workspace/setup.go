package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Default setup configuration
const (
	setupScriptName = ".worktree-setup.sh"
)

// Sidecar files that should be in .gitignore
var sidecarGitignoreEntries = []string{
	".sidecar/",
	".sidecar-agent",
	".sidecar-task",
	".sidecar-pr",
	".sidecar-start.sh",
	".sidecar-base",
	".td-root",
}

var (
	// Default env files to copy
	defaultEnvFiles = []string{".env", ".env.local", ".env.development", ".env.development.local"}
)

// SetupConfig holds worktree setup configuration.
type SetupConfig struct {
	CopyEnv        bool     // Whether to copy env files (default: true)
	EnvFiles       []string // List of env files to copy
	SymlinkDirs    []string // Directories to symlink (default: empty, opt-in)
	RunSetupScript bool     // Whether to run .worktree-setup.sh (default: true)
}

// DefaultSetupConfig returns the default setup configuration.
func DefaultSetupConfig() *SetupConfig {
	return &SetupConfig{
		CopyEnv:        true,
		EnvFiles:       defaultEnvFiles,
		SymlinkDirs:    nil, // Opt-in, not enabled by default
		RunSetupScript: true,
	}
}

// setupWorktree performs post-creation setup for a new worktree.
// This includes copying env files, creating symlinks, and running setup scripts.
func (p *Plugin) setupWorktree(worktreePath, branchName string) error {
	config := DefaultSetupConfig()

	// 0. Ensure sidecar files are in main repo's .gitignore
	if err := p.ensureSidecarGitignore(); err != nil {
		p.ctx.Logger.Warn("failed to update .gitignore", "error", err)
		// Don't fail creation for gitignore errors
	}

	// 1. Copy environment files
	if config.CopyEnv {
		if err := p.copyEnvFiles(worktreePath, config.EnvFiles); err != nil {
			p.ctx.Logger.Warn("failed to copy env files", "path", worktreePath, "error", err)
			// Don't fail creation for env file errors
		}
	}

	// 2. Create symlinks for large directories (if configured)
	if len(config.SymlinkDirs) > 0 {
		if err := p.symlinkDirs(worktreePath, config.SymlinkDirs); err != nil {
			p.ctx.Logger.Warn("failed to create symlinks", "path", worktreePath, "error", err)
			// Don't fail creation for symlink errors
		}
	}

	// 3. Run setup script (if exists)
	if config.RunSetupScript {
		if err := p.runSetupScript(worktreePath, branchName); err != nil {
			p.ctx.Logger.Warn("setup script failed", "path", worktreePath, "error", err)
			// Don't fail creation for setup script errors
		}
	}

	return nil
}

// copyEnvFiles copies environment files from the main worktree to the new worktree.
func (p *Plugin) copyEnvFiles(worktreePath string, envFiles []string) error {
	for _, envFile := range envFiles {
		src := filepath.Join(p.ctx.WorkDir, envFile)

		// Check if source file exists
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue // Skip if file doesn't exist
		}

		dst := filepath.Join(worktreePath, envFile)

		// Copy the file
		if err := copyFile(src, dst); err != nil {
			p.ctx.Logger.Warn("failed to copy env file",
				"file", envFile, "src", src, "dst", dst, "error", err)
			continue // Continue with other files even if one fails
		}

		p.ctx.Logger.Debug("copied env file", "file", envFile, "dst", dst)
	}

	return nil
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	// Get source file info for permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	// Create destination file with same permissions
	destFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	// Copy contents
	_, err = io.Copy(destFile, sourceFile)
	return err
}

// symlinkDirs creates symlinks from the main worktree to the new worktree
// for large directories like node_modules to save disk space.
func (p *Plugin) symlinkDirs(worktreePath string, dirs []string) error {
	for _, dir := range dirs {
		src := filepath.Join(p.ctx.WorkDir, dir)

		// Check if source directory exists
		srcInfo, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue // Skip if directory doesn't exist in main worktree
		}
		if err != nil {
			p.ctx.Logger.Warn("failed to stat source dir",
				"dir", dir, "error", err)
			continue
		}

		// Only symlink directories
		if !srcInfo.IsDir() {
			continue
		}

		dst := filepath.Join(worktreePath, dir)

		// Remove existing directory in worktree if present
		// (git checkout might create empty dirs)
		if _, err := os.Lstat(dst); err == nil {
			if err := os.RemoveAll(dst); err != nil {
				p.ctx.Logger.Warn("failed to remove existing dir for symlink",
					"dir", dir, "dst", dst, "error", err)
				continue
			}
		}

		// Create symlink
		if err := os.Symlink(src, dst); err != nil {
			p.ctx.Logger.Warn("failed to create symlink",
				"dir", dir, "src", src, "dst", dst, "error", err)
			continue
		}

		p.ctx.Logger.Debug("created symlink", "dir", dir, "src", src, "dst", dst)
	}

	return nil
}

// runSetupScript executes the .worktree-setup.sh script if it exists in the main repo.
// The script runs with the new worktree as the working directory and receives
// environment variables for the main worktree path, branch name, and worktree path.
func (p *Plugin) runSetupScript(worktreePath, branchName string) error {
	scriptPath := filepath.Join(p.ctx.WorkDir, setupScriptName)

	// Check if setup script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return nil // No script, that's fine
	}

	// Run the script with the worktree as working directory
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = worktreePath

	// Build isolated environment with overrides applied
	isolatedEnv := ApplyEnvOverrides(os.Environ(), BuildEnvOverrides(p.ctx.WorkDir))

	// Add worktree-specific variables
	cmd.Env = append(isolatedEnv,
		"MAIN_WORKTREE="+p.ctx.WorkDir,
		"WORKTREE_BRANCH="+branchName,
		"WORKTREE_PATH="+worktreePath,
	)

	// Capture output for logging
	output, err := cmd.CombinedOutput()
	if err != nil {
		p.ctx.Logger.Warn("setup script failed",
			"script", scriptPath,
			"output", string(output),
			"error", err)
		return err
	}

	p.ctx.Logger.Debug("setup script completed",
		"script", scriptPath,
		"output", string(output))

	return nil
}

// ensureSidecarGitignore ensures sidecar worktree files are in .gitignore.
// This prevents .sidecar-agent, .sidecar-task, and .td-root from being
// accidentally committed when using the worktree commit workflow.
func (p *Plugin) ensureSidecarGitignore() error {
	gitignorePath := filepath.Join(p.ctx.WorkDir, ".gitignore")

	// Read existing .gitignore (or empty if doesn't exist)
	var existingContent string
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existingContent = string(data)
	}

	// Check which entries are missing
	var missingEntries []string
	for _, entry := range sidecarGitignoreEntries {
		// Check if entry exists (as whole line)
		found := false
		for _, line := range strings.Split(existingContent, "\n") {
			if strings.TrimSpace(line) == entry {
				found = true
				break
			}
		}
		if !found {
			missingEntries = append(missingEntries, entry)
		}
	}

	// Nothing to add
	if len(missingEntries) == 0 {
		return nil
	}

	// Build content to append
	var toAppend strings.Builder
	// Add newline if file doesn't end with one
	if existingContent != "" && !strings.HasSuffix(existingContent, "\n") {
		toAppend.WriteString("\n")
	}
	toAppend.WriteString("\n# Sidecar worktree state files\n")
	for _, entry := range missingEntries {
		toAppend.WriteString(entry + "\n")
	}

	// Append to .gitignore
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(toAppend.String()); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}

	p.ctx.Logger.Info("added sidecar entries to .gitignore", "entries", missingEntries)
	return nil
}

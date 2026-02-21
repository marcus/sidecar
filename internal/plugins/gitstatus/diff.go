package gitstatus

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GetDiff returns the diff for a file.
func GetDiff(workDir, path string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		// Try to get exit status - git diff returns 1 if there are changes
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return string(output), nil
			}
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// GetFullDiff returns the diff for all changes.
func GetFullDiff(workDir string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return string(output), nil
			}
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// GetFileDiffStats returns the +/- counts for a single file.
func GetFileDiffStats(workDir, path string, staged bool) (int, int, error) {
	args := []string{"diff", "--numstat"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	// Parse: <additions>\t<deletions>\t<path>
	line := strings.TrimSpace(string(output))
	if line == "" {
		return 0, 0, nil
	}

	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return 0, 0, nil
	}

	var additions, deletions int
	if parts[0] != "-" {
		_, _ = stringToInt(parts[0], &additions)
	}
	if parts[1] != "-" {
		_, _ = stringToInt(parts[1], &deletions)
	}

	return additions, deletions, nil
}

// stringToInt is a helper to parse int from string.
func stringToInt(s string, result *int) (bool, error) {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false, nil
		}
		*result = *result*10 + int(c-'0')
	}
	return true, nil
}

// GetNewFileDiff creates a diff-like view for an untracked file.
// Shows file content as all additions (new file).
func GetNewFileDiff(workDir, path string) (string, error) {
	fullPath := filepath.Join(workDir, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	// Detect binary files (contains null bytes or non-printable chars)
	if isBinaryContent(content) {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))
		sb.WriteString("new file mode 100644\n")
		sb.WriteString(fmt.Sprintf("Binary file %s\n", path))
		return sb.String(), nil
	}

	lines := strings.Split(string(content), "\n")
	lineCount := len(lines)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))
	sb.WriteString("new file mode 100644\n")
	sb.WriteString("--- /dev/null\n")
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", path))
	sb.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", lineCount))

	for _, line := range lines {
		sb.WriteString("+" + line + "\n")
	}

	return strings.TrimSuffix(sb.String(), "\n"), nil
}

// isBinaryContent checks if content appears to be binary.
// Returns true if content contains null bytes or a high ratio of non-printable chars.
func isBinaryContent(content []byte) bool {
	// Check first 8KB (git uses similar heuristic)
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}

	for i := 0; i < checkLen; i++ {
		// Null byte is strong indicator of binary
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// GetFolderDiff creates a concatenated diff for all files in an untracked folder.
// Each file gets a header with its path, followed by its content as additions.
func GetFolderDiff(workDir string, files []*FileEntry) (string, error) {
	var sb strings.Builder

	for i, file := range files {
		if i > 0 {
			sb.WriteString("\n")
		}

		fileDiff, err := GetNewFileDiff(workDir, file.Path)
		if err != nil {
			// Write error placeholder for this file
			sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", file.Path, file.Path))
			sb.WriteString(fmt.Sprintf("Error reading file: %s\n", err))
			continue
		}

		sb.WriteString(fileDiff)
	}

	return sb.String(), nil
}

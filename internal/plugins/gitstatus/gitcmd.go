package gitstatus

import "os/exec"

// gitReadOnly creates an exec.Cmd for a read-only git operation that won't
// acquire optional locks. This prevents .git/index.lock conflicts when the
// file watcher triggers a background refresh while a write operation (stage,
// commit, checkout, etc.) is in progress.
//
// Uses the --no-optional-locks flag (git 2.15+), which is the standard
// approach for background monitoring tools (VS Code, JetBrains, etc.).
func gitReadOnly(args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"--no-optional-locks"}, args...)...)
}

# Fix #122: Self-Update Broken

## Issue
https://github.com/marcus/sidecar/issues/122

Users report the built-in updater (! → u) doesn't work. Affects Homebrew users primarily, possibly go install users too.

## Root Cause

Three bugs in `internal/app/model.go` working together:

### Bug 1: Missing `brew update` before `brew upgrade`
In `runInstallPhase()` (~line 489), the code runs `brew upgrade sidecar` without first running `brew update`. Homebrew taps are cached locally — without refreshing, brew doesn't know a new version exists in the tap and reports "already installed" with exit code 0.

### Bug 2: Silent false success
`brew upgrade` returns exit code 0 when "already installed." The code only checks `err`, so it marks `sidecarUpdated = true` even when nothing happened.

### Bug 3: Verify doesn't check actual version
`runVerifyPhase()` (~line 542) only checks that `sidecar --version` runs without error. It never compares the output against `installResult.NewSidecarVersion`. Verification passes after a no-op update.

## Required Changes

### 1. `runInstallPhase()` — Homebrew case

Add `brew update` before `brew upgrade`:

```go
case version.InstallMethodHomebrew:
    // Refresh tap first so brew knows about the new version
    updateCmd := exec.Command("brew", "update", "--auto-update")
    _ = updateCmd.Run() // Best-effort; upgrade may still work if tap is fresh
    
    cmd := exec.Command("brew", "upgrade", "sidecar")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return UpdateErrorMsg{Step: "sidecar", Err: fmt.Errorf("%v: %s", err, output)}
    }
    // Check for "already installed" false positive
    if strings.Contains(string(output), "already installed") {
        return UpdateErrorMsg{Step: "sidecar", Err: fmt.Errorf("brew upgrade reported already installed — tap may be out of date")}
    }
```

### 2. `runVerifyPhase()` — Version comparison

After update, verify the actual installed version matches expected:

```go
if installResult.SidecarUpdated {
    sidecarPath, err := exec.LookPath("sidecar")
    if err != nil {
        return UpdateErrorMsg{Step: "verify", Err: fmt.Errorf("sidecar not found in PATH after install")}
    }
    cmd := exec.Command(sidecarPath, "--version")
    output, err := cmd.Output()
    if err != nil {
        return UpdateErrorMsg{Step: "verify", Err: fmt.Errorf("sidecar binary not executable: %v", err)}
    }
    installedVersion := strings.TrimSpace(string(output))
    if installResult.NewSidecarVersion != "" && !strings.Contains(installedVersion, strings.TrimPrefix(installResult.NewSidecarVersion, "v")) {
        return UpdateErrorMsg{Step: "verify", Err: fmt.Errorf("version mismatch after update: expected %s, got %s — the update may not have taken effect", installResult.NewSidecarVersion, installedVersion)}
    }
}
```

## Testing

1. Install sidecar via `brew install marcus/tap/sidecar`
2. Manually edit the local tap formula to an older version (or don't run `brew update`)
3. Trigger update via TUI (! → u)
4. Verify it now runs `brew update` first, then successfully upgrades
5. Verify the version check catches a failed update

## Notes
- The go install path appears to work correctly — @nick4eva's issue may be PATH-related or a restart issue. Worth asking for more info.
- Consider adding a user-facing message when the update fails verification: "Update didn't take effect. Try running `brew update && brew upgrade sidecar` manually."

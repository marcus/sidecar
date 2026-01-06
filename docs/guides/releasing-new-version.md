# Releasing a New Version

Guide for creating new sidecar releases.

## Prerequisites

- Clean working tree (`git status` shows no changes)
- All tests passing (`go test ./...`)
- GitHub CLI authenticated (`gh auth status`)
- **No `replace` directives in go.mod** (`grep replace go.mod` should be empty)

## Release Process

### 1. Determine Version

Follow semantic versioning:
- **Major** (v2.0.0): Breaking changes
- **Minor** (v0.2.0): New features, backward compatible
- **Patch** (v0.1.1): Bug fixes only

Check current version:
```bash
git tag -l | sort -V | tail -1
```

### 2. Update td Dependency

**Critical**: Sidecar embeds td as a Go module. The `td` version shown in diagnostics comes from the standalone binary, but the actual functionality uses the embedded version from go.mod. Always update to latest td before releasing:

```bash
go get github.com/marcus/td@latest
go mod tidy
```

### 3. Verify go.mod

**Critical**: Ensure no `replace` directives exist (they break `go install`):
```bash
grep replace go.mod && echo "ERROR: Remove replace directives before releasing!" && exit 1
```

### 4. Create Tag

```bash
git tag vX.Y.Z -m "Brief description of release"
```

### 5. Push Tag

```bash
git push origin vX.Y.Z
```

### 6. Create GitHub Release

```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes "$(cat <<'EOF'
## What's New

### Feature Name
- Description of feature

### Bug Fixes
- Fix description

EOF
)"
```

Or create interactively:
```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes ""
# Then edit on GitHub
```

### 7. Verify

```bash
# Check release exists
gh release view vX.Y.Z

# Test update notification
go build -ldflags "-X main.Version=v0.0.1" -o /tmp/sidecar-test ./cmd/sidecar
/tmp/sidecar-test
# Should show toast: "Update vX.Y.Z available!"
```

## Version in Binaries

Version is embedded at build time via ldflags:

```bash
# Build with specific version
go build -ldflags "-X main.Version=v0.2.0" ./cmd/sidecar

# Install with version
go install -ldflags "-X main.Version=v0.2.0" ./cmd/sidecar
```

Without ldflags, version falls back to:
1. Go module version (if installed via `go install`)
2. Git revision (`devel+abc123`)
3. `devel`

## Update Mechanism

Users see update notifications because:
1. On startup, sidecar checks `https://api.github.com/repos/marcus/sidecar/releases/latest`
2. Compares `tag_name` against current version
3. Shows toast if newer version exists
4. Results cached for 6 hours

Dev versions (`devel`, `devel+hash`) skip the check.

## Checklist

- [ ] Tests pass
- [ ] Working tree clean
- [ ] **td dependency updated to latest** (`go get github.com/marcus/td@latest`)
- [ ] **No `replace` directives in go.mod**
- [ ] Version number follows semver
- [ ] Tag created and pushed
- [ ] GitHub release created with notes
- [ ] Update notification verified

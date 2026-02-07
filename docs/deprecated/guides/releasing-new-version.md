# Releasing a New Version

Guide for creating new sidecar releases.

## Prerequisites

- Go installed with version matching go.mod version
- Clean working tree (`git status` shows no changes)
- All tests passing (`go test ./...`)
- GitHub CLI authenticated (`gh auth status`)
- **No `replace` directives in go.mod** (`grep replace go.mod` should be empty)
- **Beware of go.work**: A parent `go.work` file can silently use local dependencies instead of published versions. Always use `GOWORK=off` when updating dependencies and testing builds.
- **GoReleaser configured** (`.goreleaser.yml` in repo root)
- **`HOMEBREW_TAP_TOKEN` secret** exists in GitHub repo settings (used by GoReleaser to update the Homebrew tap)

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
GOWORK=off go get github.com/marcus/td@latest
GOWORK=off go mod tidy
```

### 3. Verify go.mod

**Critical**: Ensure no `replace` directives exist (they break `go install`):
```bash
grep replace go.mod && echo "ERROR: Remove replace directives before releasing!" && exit 1
```

### 4. Verify Build Without go.work

**Critical**: Test that the build works without go.work to catch missing published dependencies:
```bash
GOWORK=off go build ./...
```

If this fails with "undefined" errors, the required dependency version hasn't been published yet. Publish the dependency first, then update go.mod.

### 5. Update CHANGELOG.md

Add an entry for the new version at the top of `CHANGELOG.md`:

```markdown
## [vX.Y.Z] - YYYY-MM-DD

### Features
- New feature description

### Bug Fixes
- Fix description

### Dependencies
- Dependency update description
```

Commit the changelog update:
```bash
git add CHANGELOG.md
git commit -m "docs: Update changelog for vX.Y.Z"
```

### 6. Create Tag

```bash
git tag vX.Y.Z -m "Brief description of release"
```

### 7. Push Tag

```bash
git push origin main && git push origin vX.Y.Z
```

### 8. GitHub Release (Automated)

Push the tag (step 7). GitHub Actions will run GoReleaser automatically, which:
- Creates the GitHub Release with changelog
- Builds and attaches binaries for darwin/linux (amd64/arm64)
- Generates checksums
- Updates the Homebrew tap (`marcus/tap/sidecar`)

### 9. Verify

```bash
# Check that GitHub Actions workflow succeeded
gh run list --workflow=release.yml --limit=1

# Check release exists with binaries attached
gh release view vX.Y.Z

# Test Homebrew install
brew install marcus/tap/sidecar
sidecar --version

# Test that users can install from source (critical!)
GOWORK=off go install github.com/marcus/sidecar/cmd/sidecar@vX.Y.Z

# Verify binary shows correct version
sidecar --version
# Should output: sidecar version vX.Y.Z

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
4. Results cached for 3 hours (configured in `internal/version/cache.go`)

**Note:** Pre-release versions are normalized for comparison. For example, `v0.48.0-rc1` compares as `v0.48.0` (pre-release suffixes like `-rc1`, `-beta` are stripped).

Dev versions (`devel`, `devel+hash`) skip the check.

## Recovery: Fixing a Bad Release

If a release was published with bugs:
1. Publish a new patch release (v0.48.1) with fixes
2. For critical bugs, create a new release immediately
3. Delete unpublished GitHub release without affecting git tags:
   ```bash
   gh release delete vX.Y.Z
   ```
4. Keep the git tag to preserve history

If the GitHub Actions GoReleaser workflow fails, you can re-run it manually (requires goreleaser installed locally):
```bash
goreleaser release --clean
```

## Install Methods

Users can install sidecar in several ways:

1. **Setup script**: `curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/setup.sh | bash`
2. **Homebrew**: `brew install marcus/tap/sidecar`
3. **Download binary**: grab the appropriate binary from the [GitHub Releases](https://github.com/marcus/sidecar/releases) page
4. **From source**: `go install github.com/marcus/sidecar/cmd/sidecar@latest`

## Checklist

- [ ] Tests pass
- [ ] Working tree clean
- [ ] **td dependency updated to latest** (`GOWORK=off go get github.com/marcus/td@latest`)
- [ ] **No `replace` directives in go.mod**
- [ ] **Build works without go.work** (`GOWORK=off go build ./...`)
- [ ] **CHANGELOG.md updated** with new version entry
- [ ] Version number follows semver
- [ ] Tag created and pushed
- [ ] **GitHub Actions workflow completed** successfully
- [ ] **Binaries attached** to the GitHub release
- [ ] **Homebrew tap updated** (`brew install marcus/tap/sidecar` installs new version)
- [ ] **Installation verified** (`GOWORK=off go install ...@vX.Y.Z`)
- [ ] Update notification verified

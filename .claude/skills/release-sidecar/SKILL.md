---
name: release-sidecar
description: Release new versions of sidecar. Covers version tagging with semver, td dependency updates, go.mod validation, CHANGELOG updates, GoReleaser automation, Homebrew tap updates, and verification steps. Use when preparing or executing a release.
disable-model-invocation: true
---

# Releasing a New Version

## Prerequisites

- Go installed matching go.mod version
- Clean working tree (`git status` shows no changes)
- All tests passing (`go test ./...`)
- GitHub CLI authenticated (`gh auth status`)
- No `replace` directives in go.mod
- GoReleaser configured (`.goreleaser.yml` in repo root)
- `HOMEBREW_TAP_TOKEN` secret exists in GitHub repo settings

**Beware of go.work**: A parent `go.work` file can silently use local dependencies instead of published versions. Always use `GOWORK=off` when updating dependencies and testing builds.

## Release Process

### 1. Determine Version

Follow semantic versioning:
- **Major** (v2.0.0): Breaking changes
- **Minor** (v0.2.0): New features, backward compatible
- **Patch** (v0.1.1): Bug fixes only

```bash
git tag -l | sort -V | tail -1
```

### 2. Update td Dependency

Sidecar embeds td as a Go module. Always update to latest before releasing:

```bash
GOWORK=off go get github.com/marcus/td@latest
GOWORK=off go mod tidy
```

### 3. Verify go.mod

Ensure no `replace` directives (they break `go install`):
```bash
grep replace go.mod && echo "ERROR: Remove replace directives before releasing!" && exit 1
```

### 4. Verify Build Without go.work

```bash
GOWORK=off go build ./...
```

If this fails with "undefined" errors, the required dependency version hasn't been published yet.

### 5. Update CHANGELOG.md

```markdown
## [vX.Y.Z] - YYYY-MM-DD

### Features
- New feature description

### Bug Fixes
- Fix description

### Dependencies
- Dependency update description
```

```bash
git add CHANGELOG.md
git commit -m "docs: Update changelog for vX.Y.Z"
```

### 6. Create and Push Tag

```bash
git tag vX.Y.Z -m "Brief description of release"
git push origin main && git push origin vX.Y.Z
```

### 7. GitHub Release (Automated)

Pushing the tag triggers GitHub Actions GoReleaser, which:
- Creates the GitHub Release with changelog
- Builds and attaches binaries for darwin/linux (amd64/arm64)
- Generates checksums

**⚠️ GoReleaser does NOT update the Homebrew tap automatically.**
The tap uses source-build formulas (to avoid macOS Gatekeeper warnings on pre-built binaries), which GoReleaser doesn't support. You MUST manually update the tap.

### 7b. Update Homebrew Tap (REQUIRED — Manual Step)

The Homebrew tap at `marcus/homebrew-tap` must be updated for every release.
If you skip this, users will see "update available" but `brew upgrade` won't find it (see [#122](https://github.com/marcus/sidecar/issues/122)).

```bash
# Get the source tarball SHA256
curl -sL "https://github.com/marcus/sidecar/archive/refs/tags/vX.Y.Z.tar.gz" | shasum -a 256

# Edit the formula (local tap is at /opt/homebrew/Library/Taps/marcus/homebrew-tap/)
# Update: url (tag version) and sha256
cd /opt/homebrew/Library/Taps/marcus/homebrew-tap
# Edit Formula/sidecar.rb — update version in URL and sha256

# Commit and push
git add Formula/sidecar.rb
git commit -m "sidecar: bump to vX.Y.Z"
git push
```

**Also bump td and nightshift formulas if releasing those.**

### 8. Verify

```bash
# Check workflow succeeded
gh run list --workflow=release.yml --limit=1

# Check release exists with binaries
gh release view vX.Y.Z

# Test Homebrew install
brew install marcus/tap/sidecar
sidecar --version

# Test go install (critical!)
GOWORK=off go install github.com/marcus/sidecar/cmd/sidecar@vX.Y.Z
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
go build -ldflags "-X main.Version=v0.2.0" ./cmd/sidecar
go install -ldflags "-X main.Version=v0.2.0" ./cmd/sidecar
```

Without ldflags, version falls back to:
1. Go module version (if installed via `go install`)
2. Git revision (`devel+abc123`)
3. `devel`

## Update Mechanism

On startup, sidecar checks `https://api.github.com/repos/marcus/sidecar/releases/latest`, compares `tag_name` against current version, and shows a toast if newer. Results cached for 3 hours. Pre-release suffixes (`-rc1`, `-beta`) are stripped for comparison. Dev versions skip the check.

## Recovery: Fixing a Bad Release

1. Publish a new patch release with fixes
2. For critical bugs, release immediately
3. Delete unpublished GitHub release: `gh release delete vX.Y.Z`
4. Keep git tags to preserve history
5. If GoReleaser workflow fails, re-run locally: `goreleaser release --clean`

## Install Methods

1. **Setup script**: `curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/setup.sh | bash`
2. **Homebrew**: `brew install marcus/tap/sidecar`
3. **Download binary**: from GitHub Releases page
4. **From source**: `go install github.com/marcus/sidecar/cmd/sidecar@latest`

## Checklist

- [ ] Tests pass
- [ ] Working tree clean
- [ ] td dependency updated (`GOWORK=off go get github.com/marcus/td@latest`)
- [ ] No `replace` directives in go.mod
- [ ] Build works without go.work (`GOWORK=off go build ./...`)
- [ ] CHANGELOG.md updated
- [ ] Version follows semver
- [ ] Tag created and pushed
- [ ] GitHub Actions workflow completed
- [ ] Binaries attached to release
- [ ] Homebrew tap updated (MANUAL — see step 7b, GoReleaser does NOT do this)
- [ ] Installation verified (`GOWORK=off go install ...@vX.Y.Z`)
- [ ] Update notification verified

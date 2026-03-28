.PHONY: build install install-dev test test-v clean check-clean tag goreleaser-snapshot fmt fmt-check fmt-check-all lint lint-all

# Default target
all: build

LINT_BASE ?= main

# Build metadata injected into every binary via ldflags.
VERSION      ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
IS_DIRTY     := $(shell git diff --quiet 2>/dev/null && git diff --cached --quiet 2>/dev/null && echo "false" || echo "true")
BUILD_DATE   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_PROFILE ?= debug

LDFLAGS := -X main.Version=$(VERSION) \
           -X main.Commit=$(COMMIT) \
           -X main.Dirty=$(IS_DIRTY) \
           -X main.BuildDate=$(BUILD_DATE) \
           -X main.BuildProfile=$(BUILD_PROFILE)

# Build the binary
build:
	go build -ldflags "$(LDFLAGS)" -o bin/sidecar ./cmd/sidecar

# Install to GOBIN
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/sidecar

# Install with version info from git (explicit dev profile)
install-dev:
	@echo "Installing sidecar version=$(VERSION) commit=$(COMMIT) dirty=$(IS_DIRTY)"
	go install -ldflags "$(LDFLAGS)" ./cmd/sidecar

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Check for clean working tree
check-clean:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working tree is not clean"; \
		git status --short; \
		exit 1; \
	fi

# Create a new version tag
# Usage: make tag VERSION=v0.1.0
tag: check-clean
ifndef VERSION
	$(error VERSION is required. Usage: make tag VERSION=v0.1.0)
endif
	@if ! echo "$(VERSION)" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "Error: VERSION must match vX.Y.Z format (got: $(VERSION))"; \
		exit 1; \
	fi
	@echo "Creating tag $(VERSION)"
	git tag -a $(VERSION) -m "Release $(VERSION)"
	@echo "Tag $(VERSION) created. Run 'git push origin $(VERSION)' to trigger the release."

# Show version that would be used
version:
	@git describe --tags --always --dirty 2>/dev/null || echo "dev"

# Format code
fmt:
	go fmt ./...

# Check formatting for changed Go files only (merge-base with LINT_BASE)
fmt-check:
	@files="$$(git diff --name-only --diff-filter=ACMRTUXB $(LINT_BASE)...HEAD -- '*.go')"; \
	if [ -z "$$files" ]; then \
		echo "No changed Go files to check."; \
		exit 0; \
	fi; \
	unformatted="$$(echo "$$files" | xargs gofmt -l)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted changed Go files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

# Check formatting across all Go files
fmt-check-all:
	@unformatted="$$(find . -name '*.go' -not -path './vendor/*' -not -path './website/*' -print0 | xargs -0 gofmt -l)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted Go files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

# Run linter
lint:
	golangci-lint run --new-from-merge-base=$(LINT_BASE) ./...

# Run linter across the full codebase (includes legacy debt)
lint-all:
	golangci-lint run ./...

# Build for multiple platforms (local testing only — GoReleaser handles release builds)
build-all:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/sidecar-darwin-amd64 ./cmd/sidecar
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/sidecar-darwin-arm64 ./cmd/sidecar
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/sidecar-linux-amd64 ./cmd/sidecar
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/sidecar-linux-arm64 ./cmd/sidecar

# Test GoReleaser locally (creates snapshot build without publishing)
goreleaser-snapshot:
	goreleaser release --snapshot --clean

# Install pre-commit hooks
install-hooks:
	@chmod +x scripts/pre-commit.sh
	@ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
	@echo "✅ pre-commit hook installed"

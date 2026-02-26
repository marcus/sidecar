#!/bin/sh
set -eu

# Sidecar install script — downloads a pre-built binary from GitHub Releases.
# No Go required.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/install.sh | sh

REPO="marcus/sidecar"
BINARY="sidecar"

# Colors (disabled if not a terminal)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BOLD=''
    NC=''
fi

info() {
    printf "${BOLD}%s${NC}\n" "$1"
}

success() {
    printf "${GREEN}%s${NC}\n" "$1"
}

warn() {
    printf "${YELLOW}%s${NC}\n" "$1"
}

error() {
    printf "${RED}error: %s${NC}\n" "$1" >&2
}

die() {
    error "$1"
    exit 1
}

# --- Detect OS ---------------------------------------------------------------

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux" ;;
        *)      die "Unsupported OS: $(uname -s). This installer supports macOS and Linux." ;;
    esac
}

# --- Detect architecture -----------------------------------------------------

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              die "Unsupported architecture: $(uname -m). This installer supports amd64 and arm64." ;;
    esac
}

# --- Fetch latest release tag from GitHub API --------------------------------

get_latest_version() {
    local url="https://api.github.com/repos/${REPO}/releases/latest"
    local tag

    if command -v curl >/dev/null 2>&1; then
        tag=$(curl -fsSL "$url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        tag=$(wget -qO- "$url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    else
        die "Neither curl nor wget found. Please install one and try again."
    fi

    if [ -z "$tag" ]; then
        die "Could not determine the latest release. Check https://github.com/${REPO}/releases"
    fi

    echo "$tag"
}

# --- Download and extract ----------------------------------------------------

download() {
    local url="$1"
    local dest="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" "$url"
    fi
}

# --- Choose install directory ------------------------------------------------

choose_install_dir() {
    if [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
        echo "/usr/local/bin"
    elif [ "$(id -u)" = "0" ]; then
        echo "/usr/local/bin"
    else
        # No write access to /usr/local/bin and not root — try sudo, else fallback
        if command -v sudo >/dev/null 2>&1; then
            echo "/usr/local/bin"
        else
            mkdir -p "$HOME/.local/bin"
            echo "$HOME/.local/bin"
        fi
    fi
}

# --- Main --------------------------------------------------------------------

main() {
    info "Sidecar Installer"
    echo ""

    OS=$(detect_os)
    ARCH=$(detect_arch)
    info "Detected: ${OS}/${ARCH}"

    printf "Fetching latest release... "
    VERSION=$(get_latest_version)
    success "$VERSION"

    # Build download URL: sidecar_0.74.1_darwin_arm64.tar.gz
    VER_NUM="${VERSION#v}"
    ARCHIVE="${BINARY}_${VER_NUM}_${OS}_${ARCH}.tar.gz"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

    INSTALL_DIR=$(choose_install_dir)
    NEEDS_SUDO=false
    if [ ! -w "$INSTALL_DIR" ] && [ "$(id -u)" != "0" ]; then
        NEEDS_SUDO=true
    fi

    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    printf "Downloading %s... " "$ARCHIVE"
    if ! download "$URL" "$TMPDIR/$ARCHIVE"; then
        echo ""
        die "Download failed. Check that ${URL} exists."
    fi
    success "done"

    printf "Extracting... "
    if ! tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR" 2>/dev/null; then
        die "Failed to extract archive."
    fi

    if [ ! -f "$TMPDIR/$BINARY" ]; then
        die "Binary '${BINARY}' not found in archive."
    fi
    success "done"

    printf "Installing to %s... " "$INSTALL_DIR"
    if [ "$NEEDS_SUDO" = true ]; then
        sudo install -m 755 "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
    else
        install -m 755 "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
    fi
    success "done"

    # Verify
    echo ""
    if command -v "$BINARY" >/dev/null 2>&1; then
        INSTALLED_VERSION=$("$BINARY" --version 2>/dev/null || echo "unknown")
        success "sidecar ${INSTALLED_VERSION} installed successfully!"
    elif [ -x "$INSTALL_DIR/$BINARY" ]; then
        INSTALLED_VERSION=$("$INSTALL_DIR/$BINARY" --version 2>/dev/null || echo "unknown")
        success "sidecar ${INSTALLED_VERSION} installed to ${INSTALL_DIR}"
        echo ""
        warn "Note: ${INSTALL_DIR} is not in your PATH."
        echo "Add it with:"
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    else
        die "Installation failed — binary not found after install."
    fi

    echo ""
    echo "Run 'sidecar' in any project directory to get started."
}

main

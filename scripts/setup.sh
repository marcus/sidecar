#!/usr/bin/env bash
set -euo pipefail

# Sidecar Setup Script
# Installs sidecar and optionally td for AI-assisted development workflows

VERSION="1.0.0"

# Colors (used in plain mode)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

# Flags
YES_FLAG=false
FORCE_FLAG=false
SIDECAR_ONLY=false
HELP_FLAG=false
USE_GUM=false

# Versions (populated during detection)
GO_VERSION=""
TD_VERSION=""
SIDECAR_VERSION=""
LATEST_TD=""
LATEST_SIDECAR=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -y|--yes)
            YES_FLAG=true
            shift
            ;;
        -f|--force)
            FORCE_FLAG=true
            shift
            ;;
        --sidecar-only)
            SIDECAR_ONLY=true
            shift
            ;;
        -h|--help)
            HELP_FLAG=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

show_help() {
    cat << EOF
Sidecar Setup Script v${VERSION}

Usage: setup.sh [OPTIONS]

Options:
  -y, --yes           Skip all prompts (for CI/headless installs)
  -f, --force         Reinstall even if versions are up-to-date
  --sidecar-only      Install only sidecar, skip td
  -h, --help          Show this help message

Examples:
  # Interactive install
  curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/scripts/setup.sh | bash

  # Headless install (both tools)
  curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/scripts/setup.sh | bash -s -- --yes

  # Headless install (sidecar only)
  curl -fsSL https://raw.githubusercontent.com/marcus/sidecar/main/scripts/setup.sh | bash -s -- --yes --sidecar-only
EOF
}

if $HELP_FLAG; then
    show_help
    exit 0
fi

# Platform detection
detect_platform() {
    case "$(uname -s)" in
        Darwin) echo "macos" ;;
        Linux)
            if grep -qi microsoft /proc/version 2>/dev/null; then
                echo "wsl"
            else
                echo "linux"
            fi
            ;;
        *) echo "unsupported" ;;
    esac
}

PLATFORM=$(detect_platform)

if [[ "$PLATFORM" == "unsupported" ]]; then
    echo -e "${RED}Unsupported platform. This script supports macOS, Linux, and WSL.${NC}"
    exit 1
fi

if [[ "$PLATFORM" == "wsl" ]]; then
    echo -e "${YELLOW}WSL detected. WSL support is experimental.${NC}"
fi

# Gum helpers with plain fallback
try_install_gum() {
    if command -v gum &> /dev/null; then
        USE_GUM=true
        return 0
    fi

    # Try to install gum (terminal UI toolkit for nicer prompts/spinners)
    local install_cmd=""
    local install_name="gum"

    if command -v brew &> /dev/null; then
        install_cmd="brew install gum"
    elif command -v nix-env &> /dev/null; then
        install_cmd="nix-env -iA nixpkgs.gum"
    elif command -v apt &> /dev/null; then
        # gum requires adding the charm repo on Debian/Ubuntu
        install_cmd=""  # Skip - too complex for auto-install
    elif command -v dnf &> /dev/null; then
        install_cmd="dnf install -y gum"
    elif command -v pacman &> /dev/null; then
        install_cmd="pacman -S --noconfirm gum"
    fi

    if [[ -n "$install_cmd" ]]; then
        echo ""
        echo "Installing gum (terminal UI toolkit for better prompts)..."
        echo "  This may take a minute..."
        echo ""
        if $install_cmd 2>&1 | head -20; then
            if command -v gum &> /dev/null; then
                USE_GUM=true
                echo "Done."
                return 0
            fi
        fi
    fi

    # Fall back to plain mode
    USE_GUM=false
    return 0
}

# UI helpers that work in both gum and plain mode
style_header() {
    if $USE_GUM; then
        gum style --foreground 212 --bold "$1"
    else
        echo -e "${BOLD}${BLUE}$1${NC}"
    fi
}

style_success() {
    if $USE_GUM; then
        gum style --foreground 2 "$1"
    else
        echo -e "${GREEN}$1${NC}"
    fi
}

style_warning() {
    if $USE_GUM; then
        gum style --foreground 3 "$1"
    else
        echo -e "${YELLOW}$1${NC}"
    fi
}

style_error() {
    if $USE_GUM; then
        gum style --foreground 1 "$1"
    else
        echo -e "${RED}$1${NC}"
    fi
}

confirm() {
    local prompt="$1"
    local default="${2:-y}"

    if $YES_FLAG; then
        return 0
    fi

    if $USE_GUM; then
        gum confirm "$prompt"
        return $?
    else
        local yn
        if [[ "$default" == "y" ]]; then
            read -p "$prompt [Y/n] " yn
            yn=${yn:-y}
        else
            read -p "$prompt [y/N] " yn
            yn=${yn:-n}
        fi
        [[ "$yn" =~ ^[Yy] ]]
    fi
}

choose() {
    local prompt="$1"
    shift
    local options=("$@")

    if $YES_FLAG; then
        echo "${options[0]}"
        return 0
    fi

    if $USE_GUM; then
        gum choose --header "$prompt" "${options[@]}"
    else
        echo "$prompt"
        local i=1
        for opt in "${options[@]}"; do
            echo "  $i) $opt"
            ((i++))
        done
        local choice
        read -p "Select [1-${#options[@]}]: " choice
        choice=${choice:-1}
        echo "${options[$((choice-1))]}"
    fi
}

spin() {
    local title="$1"
    shift
    local ret

    if $USE_GUM; then
        gum spin --spinner dot --title "$title" -- "$@"
        ret=$?
    else
        echo "$title"
        "$@"
        ret=$?
    fi

    if [[ $ret -ne 0 ]]; then
        style_error "Command failed (exit code $ret)"
    fi
    return $ret
}

# Version helpers
get_go_version() {
    if command -v go &> /dev/null; then
        go version | grep -oE 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | head -1 | sed 's/go//'
    else
        echo ""
    fi
}

get_td_version() {
    local td_bin
    # Check PATH first, then check go bin directory directly
    if command -v td &> /dev/null; then
        td_bin="td"
    elif command -v go &> /dev/null; then
        local go_bin
        go_bin=$(get_go_bin)
        if [[ -x "$go_bin/td" ]]; then
            td_bin="$go_bin/td"
        fi
    fi

    if [[ -n "$td_bin" ]]; then
        local v
        v=$("$td_bin" version --short 2>/dev/null || true)
        if [[ -z "$v" ]]; then
            v=$("$td_bin" version 2>/dev/null | head -1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' || true)
        fi
        echo "$v"
    else
        echo ""
    fi
}

get_sidecar_version() {
    local sidecar_bin
    # Check PATH first, then check go bin directory directly
    if command -v sidecar &> /dev/null; then
        sidecar_bin="sidecar"
    elif command -v go &> /dev/null; then
        local go_bin
        go_bin=$(get_go_bin)
        if [[ -x "$go_bin/sidecar" ]]; then
            sidecar_bin="$go_bin/sidecar"
        fi
    fi

    if [[ -n "$sidecar_bin" ]]; then
        "$sidecar_bin" --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo ""
    else
        echo ""
    fi
}

get_latest_release() {
    local repo="$1"
    local url="https://api.github.com/repos/${repo}/releases/latest"
    curl -fsSL "$url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

version_gte() {
    local v1="$1"
    local v2="$2"
    # Remove 'v' prefix
    v1="${v1#v}"
    v2="${v2#v}"

    printf '%s\n%s\n' "$v2" "$v1" | sort -V | head -n1 | grep -q "^${v2}$"
}

# Check if PATH includes go/bin
check_go_path() {
    local go_bin
    go_bin=$(get_go_bin)
    echo "$PATH" | tr ':' '\n' | grep -qF "$go_bin"
}

get_shell_rc() {
    local shell_name
    shell_name=$(basename "$SHELL")
    case "$shell_name" in
        zsh) echo "$HOME/.zshrc" ;;
        bash)
            if [[ -f "$HOME/.bashrc" ]]; then
                echo "$HOME/.bashrc"
            else
                echo "$HOME/.bash_profile"
            fi
            ;;
        *) echo "$HOME/.profile" ;;
    esac
}

get_go_bin() {
    local gobin
    gobin=$(go env GOBIN 2>/dev/null)
    if [[ -n "$gobin" ]]; then
        echo "$gobin"
    else
        local gopath
        gopath=$(go env GOPATH 2>/dev/null)
        echo "${gopath:-$HOME/go}/bin"
    fi
}

# Main installation flow
main() {
    # Try to get gum for better UI
    try_install_gum

    echo ""
    style_header "Sidecar Setup"
    echo ""

    # Detect current state
    GO_VERSION=$(get_go_version)
    TD_VERSION=$(get_td_version)
    SIDECAR_VERSION=$(get_sidecar_version)

    # Fetch latest versions
    echo "Checking latest versions..."
    LATEST_SIDECAR=$(get_latest_release "marcus/sidecar" || echo "")
    if ! $SIDECAR_ONLY; then
        LATEST_TD=$(get_latest_release "marcus/td" || echo "")
    fi

    # Show status table
    echo ""
    style_header "Current Status"
    echo "──────────────────────────────────────"

    # Go status
    if [[ -n "$GO_VERSION" ]]; then
        if version_gte "$GO_VERSION" "1.21"; then
            style_success "  Go:      ✓ $GO_VERSION"
        else
            style_warning "  Go:      ! $GO_VERSION (need 1.21+)"
        fi
    else
        style_error "  Go:      ✗ not installed"
    fi

    # td status
    if ! $SIDECAR_ONLY; then
        if [[ -n "$TD_VERSION" ]]; then
            if [[ -n "$LATEST_TD" && "$TD_VERSION" != "$LATEST_TD" ]]; then
                style_warning "  td:      ✓ $TD_VERSION -> $LATEST_TD available"
            else
                style_success "  td:      ✓ $TD_VERSION"
            fi
        else
            echo "  td:      - not installed"
        fi
    fi

    # sidecar status
    if [[ -n "$SIDECAR_VERSION" ]]; then
        if [[ -n "$LATEST_SIDECAR" && "$SIDECAR_VERSION" != "$LATEST_SIDECAR" ]]; then
            style_warning "  sidecar: ✓ $SIDECAR_VERSION -> $LATEST_SIDECAR available"
        else
            style_success "  sidecar: ✓ $SIDECAR_VERSION"
        fi
    else
        echo "  sidecar: - not installed"
    fi

    echo "──────────────────────────────────────"
    echo ""

    # Tool selection (unless --sidecar-only)
    local install_td=false
    local install_sidecar=true

    if ! $SIDECAR_ONLY && ! $YES_FLAG; then
        local choice
        choice=$(choose "What would you like to install?" \
            "[Recommended] Both td and sidecar" \
            "sidecar only" \
            "td only")

        case "$choice" in
            *"Both"*) install_td=true; install_sidecar=true ;;
            *"sidecar only"*) install_td=false; install_sidecar=true ;;
            *"td only"*) install_td=true; install_sidecar=false ;;
        esac
    elif $SIDECAR_ONLY; then
        install_td=false
        install_sidecar=true
    else
        # --yes mode: install both by default
        install_td=true
        install_sidecar=true
    fi

    # Check Go
    if [[ -z "$GO_VERSION" ]]; then
        echo ""
        style_warning "Go is required but not installed."
        echo ""

        # Detect available package manager
        local go_install_cmd=""
        if command -v brew &> /dev/null; then
            go_install_cmd="brew install go"
        elif command -v nix-env &> /dev/null; then
            go_install_cmd="nix-env -iA nixpkgs.go"
        elif command -v apt &> /dev/null; then
            go_install_cmd="sudo apt update && sudo apt install -y golang"
        elif command -v dnf &> /dev/null; then
            go_install_cmd="sudo dnf install -y golang"
        elif command -v pacman &> /dev/null; then
            go_install_cmd="sudo pacman -S --noconfirm go"
        fi

        if [[ -n "$go_install_cmd" ]]; then
            echo "Will run:"
            echo "  $go_install_cmd"
            echo ""
            if confirm "Run this command?"; then
                spin "Installing Go..." bash -c "$go_install_cmd"
                GO_VERSION=$(get_go_version)
            else
                echo ""
                echo "Please install Go manually and run this script again."
                exit 1
            fi
        else
            echo "Please install Go 1.21+ and run this script again."
            echo "  macOS:         brew install go"
            echo "  Nix:           nix-env -iA nixpkgs.go"
            echo "  Ubuntu/Debian: sudo apt install golang"
            echo "  Fedora/RHEL:   sudo dnf install golang"
            echo "  Arch:          sudo pacman -S go"
            echo "  Other:         https://go.dev/dl/"
            exit 1
        fi
    elif ! version_gte "$GO_VERSION" "1.21"; then
        style_error "Go $GO_VERSION is too old. Please upgrade to Go 1.21 or newer."
        exit 1
    fi

    # Check PATH
    local go_bin
    go_bin=$(get_go_bin)

    if ! check_go_path; then
        echo ""
        style_warning "$go_bin is not in your PATH"
        echo ""

        local shell_rc
        shell_rc=$(get_shell_rc)

        echo "Will add to $shell_rc:"
        echo "  export PATH=\"$go_bin:\$PATH\""
        echo ""

        if confirm "Add to PATH?"; then
            echo "" >> "$shell_rc"
            echo "export PATH=\"$go_bin:\$PATH\"" >> "$shell_rc"
            export PATH="$go_bin:$PATH"
            style_success "Added to $shell_rc"
            echo ""
            echo "Note: Run 'source $shell_rc' to apply in current shell."
        else
            echo ""
            echo "Please add $go_bin to your PATH manually."
            echo "  export PATH=\"$go_bin:\$PATH\""
        fi
    fi

    # Install td
    if $install_td; then
        echo ""
        if [[ -z "$TD_VERSION" ]] || $FORCE_FLAG || [[ "$TD_VERSION" != "$LATEST_TD" ]]; then
            local td_version="${LATEST_TD:-latest}"

            echo "Will run:"
            echo "  go install github.com/marcus/td@${td_version}"
            echo ""

            if confirm "Install td?"; then
                echo "Installing td (this may take a minute)..."
                go install "github.com/marcus/td@${td_version}"
                TD_VERSION=$(get_td_version)
                style_success "td installed: $TD_VERSION"
            fi
        else
            style_success "td is up to date ($TD_VERSION)"
        fi
    fi

    # Install sidecar
    if $install_sidecar; then
        echo ""
        if [[ -z "$SIDECAR_VERSION" ]] || $FORCE_FLAG || [[ "$SIDECAR_VERSION" != "$LATEST_SIDECAR" ]]; then
            local sc_version="${LATEST_SIDECAR:-latest}"

            echo "Will run:"
            echo "  go install github.com/marcus/sidecar/cmd/sidecar@${sc_version}"
            echo ""

            if confirm "Install sidecar?"; then
                echo "Installing sidecar (this may take a minute)..."
                go install -ldflags "-X main.Version=${sc_version}" "github.com/marcus/sidecar/cmd/sidecar@${sc_version}"
                SIDECAR_VERSION=$(get_sidecar_version)
                style_success "sidecar installed: $SIDECAR_VERSION"
            fi
        else
            style_success "sidecar is up to date ($SIDECAR_VERSION)"
        fi
    fi

    # Final verification
    echo ""
    echo "──────────────────────────────────────"
    style_header "Installation Complete"
    echo ""

    local all_good=true

    if $install_sidecar; then
        if command -v sidecar &> /dev/null; then
            style_success "  ✓ sidecar $(get_sidecar_version)"
        else
            style_error "  ✗ sidecar not found"
            all_good=false
        fi
    fi

    if $install_td; then
        if command -v td &> /dev/null; then
            style_success "  ✓ td $(get_td_version)"
        else
            style_error "  ✗ td not found"
            all_good=false
        fi
    fi

    echo ""

    if $all_good; then
        echo ""
        style_success "✓ Setup complete!"
        echo ""
        style_header "Getting Started"
        echo ""
        if $install_td; then
            echo "  1. cd into your project directory"
            echo "  2. Run 'td init' to initialize task tracking"
            echo "  3. Run 'sidecar' to launch the UI"
        else
            echo "  1. cd into your project directory"
            echo "  2. Run 'sidecar' to launch the UI"
        fi
        echo ""
    else
        echo "Some installations may have failed. Check the output above."
        echo "You may need to run 'source $(get_shell_rc)' to update your PATH."
    fi
}

# Run main
main

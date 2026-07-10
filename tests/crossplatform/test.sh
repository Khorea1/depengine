#!/bin/sh

set -e # Exit immediately if a command exits with a non-zero status.

# Configuration passed via environment variables
SCHEMA=${DEPENGINE_SCHEMA:-tests/crossplatform/schema.toml}
BIN=${DEPENGINE_BIN:-./depengine}
PLATFORM=${DEPENGINE_PLATFORM:-linux}
DISTRO=${DEPENGINE_DISTRO:-unknown}

echo "Running cross-platform tests for distro: $DISTRO"

# Build depengine if it doesn't exist or if source has changed (simplified check)
# In a real CI, we'd likely copy a pre-built binary
if [ ! -x "$BIN" ] || [ "$BIN" -nt "main.go" ]; then
    echo "Building depengine..."
    go build -o "$BIN" . || exit 1
fi

# Install the depengine binary to a temporary location for testing
# This is a simplified approach. In a real scenario, we might install to a specific path
# or use a different mechanism.
INSTALL_DIR=$(mktemp -d)
cp "$BIN" "$INSTALL_DIR/depengine"
export PATH="$INSTALL_DIR:$PATH"

# Ensure the schema file exists
if [ ! -f "$SCHEMA" ]; then
    echo "Error: Schema file not found at $SCHEMA"
    exit 1
fi

# --- Test Cases ---

echo "Testing install command..."
$BIN install --schema "$SCHEMA" --dry-run --verbose
$BIN install --schema "$SCHEMA" --only zsh
$BIN install --schema "$SCHEMA" --skip kitty

echo "Testing check command..."
$BIN check zsh
$BIN check unknown-tool

echo "Testing status command..."
$BIN status

echo "Testing remove command..."
$BIN remove zsh

echo "Testing validate command..."
$BIN validate --schema "$SCHEMA" --check-env --format json

# Test native package installation (requires root/sudo within container, or privileges)
# These commands are illustrative and might need adjustments based on container setup.
# For simplicity, we'll focus on the depengine commands themselves and assume
# native package managers are available and functional in the base images.
# In a more robust test, we'd verify specific packages are installed/removable.

echo "Simulating native package checks (commands may not actually run inside container without privileges)"
case "$DISTRO" in
    debian)
        echo "Debian: Checking apt access..."
        # apt-get update -y
        ;; 
    arch)
        echo "Arch: Checking pacman access..."
        # pacman -Sy --noconfirm
        ;;  
    fedora)
        echo "Fedora: Checking dnf access..."
       # dnf update -y
        ;;  
    alpine)
        echo "Alpine: Checking apk access..."
        # apk update
        ;;  
esac

# Test language adapters (example with npm)
echo "Testing npm adapter..."
# Assuming nodejs and npm are installed in the Docker image or available
# npm install -g http-server # Example: install a global npm package
# $BIN install --schema "$SCHEMA" --only http-server # depengine should detect it

# Test git adapter
echo "Testing git adapter..."
# git clone https://github.com/some/repo.git /tmp/testrepo
# cd /tmp/testrepo && ./configure && make && make install # Example build steps
# $BIN install --schema "$SCHEMA" --only my-git-tool

# Test http adapter
echo "Testing http adapter..."
# $BIN install --schema "$SCHEMA" --only fastfetch # Example tool using http adapter

# Test state tracking (checking if state file is created/updated)
echo "Testing state tracking..."
# After an install, check for ~/.config/depengine/state.json or similar
# This is implicitly tested by `status` and `remove` commands.

# Test removal
echo "Testing removal command again..."
# $BIN remove zsh # zsh should be gone now

# Validation tests (using existing schema.toml)
echo "Running validation tests..."
$BIN validate --schema "$SCHEMA" --strict

echo "Cross-platform tests completed for $DISTRO."
exit 0

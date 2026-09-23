#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo "usage: $0 <schema> <tool>" >&2
    exit 2
fi

SCHEMA=$1
TOOL=$2
SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
REPO=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd)
BIN=${DEPENGINE_BIN:-"$REPO/depengine"}

cd "$REPO"

if [ ! -x "$BIN" ]; then
    echo "Building depengine..."
    go build -o "$BIN" .
fi

if [ ! -f "$SCHEMA" ]; then
    echo "schema not found: $SCHEMA" >&2
    exit 2
fi

WORKDIR=$(mktemp -d)
INSTALLED_BY_TEST=0
cleanup() {
    if [ "$INSTALLED_BY_TEST" -eq 1 ]; then
        "$BIN" remove "$TOOL" >/dev/null 2>&1 || true
    fi
    rm -rf "$WORKDIR"
}
trap cleanup 0
trap 'exit 1' 1 2 15

export XDG_CONFIG_HOME="$WORKDIR/config"
export XDG_STATE_HOME="$WORKDIR/state"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_STATE_HOME"

echo "Validating native smoke fixture..."
"$BIN" validate --schema "$SCHEMA" --no-manifest

echo "Checking clean precondition..."
if "$BIN" check --schema "$SCHEMA" --no-manifest --live "$TOOL"; then
    echo "refusing to remove pre-existing package for test tool: $TOOL" >&2
    exit 1
fi

echo "Installing $TOOL..."
"$BIN" install --schema "$SCHEMA" --only "$TOOL"
INSTALLED_BY_TEST=1
"$BIN" check --schema "$SCHEMA" --no-manifest --live "$TOOL"
"$BIN" status

echo "Checking idempotent reinstall..."
"$BIN" install --schema "$SCHEMA" --only "$TOOL"
"$BIN" check --schema "$SCHEMA" --no-manifest --live "$TOOL"

echo "Removing $TOOL..."
"$BIN" remove "$TOOL"
INSTALLED_BY_TEST=0

if "$BIN" check --schema "$SCHEMA" --no-manifest --live "$TOOL"; then
    echo "package still detected after removal: $TOOL" >&2
    exit 1
fi

echo "Native lifecycle smoke test passed for $TOOL."

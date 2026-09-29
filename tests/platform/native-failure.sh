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
    go build -o "$BIN" .
fi

WORKDIR=$(mktemp -d)
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup 0
trap 'exit 1' 1 2 15

export XDG_CONFIG_HOME="$WORKDIR/config"
export XDG_STATE_HOME="$WORKDIR/state"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_STATE_HOME"

"$BIN" validate --schema "$SCHEMA" --no-manifest

if "$BIN" install --schema "$SCHEMA" --only "$TOOL"; then
    echo "unexpectedly installed nonexistent native package: $TOOL" >&2
    exit 1
fi

if "$BIN" check --schema "$SCHEMA" --no-manifest --live "$TOOL"; then
    echo "failed native install was reported as present: $TOOL" >&2
    exit 1
fi

if "$BIN" remove "$TOOL"; then
    echo "failed native install left removable tracked state: $TOOL" >&2
    exit 1
fi

echo "Native failure-state test passed for $TOOL."

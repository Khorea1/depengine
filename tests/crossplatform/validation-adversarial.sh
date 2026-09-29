#!/bin/sh
set -eu

BIN=${DEPENGINE_BIN:-./depengine}
FIXTURES=internal/validate/testdata

fail() {
    echo "ERROR: $*" >&2
    exit 1
}

run_validate() {
    output=$1
    shift
    set +e
    "$BIN" validate --no-manifest --format json "$@" >"$output" 2>&1
    status=$?
    set -e
    return "$status"
}

expect_error() {
    fixture=$1
    code=$2
    output=$(mktemp)
    if run_validate "$output" --schema "$fixture"; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture unexpectedly validated"
    else
        status=$?
    fi
    if [ "$status" -ne 2 ]; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture exited $status, want 2"
    fi
    if ! grep -F "\"code\": \"$code\"" "$output" >/dev/null; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture omitted diagnostic $code"
    fi
    rm -f "$output"
}

expect_strict_warning() {
    fixture=$1
    code=$2
    output=$(mktemp)
    if run_validate "$output" --schema "$fixture" --strict; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture unexpectedly passed strict validation"
    else
        status=$?
    fi
    if [ "$status" -ne 1 ]; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture strict validation exited $status, want 1"
    fi
    if ! grep -F "\"code\": \"$code\"" "$output" >/dev/null; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture omitted strict warning $code"
    fi
    rm -f "$output"
}

expect_valid() {
    fixture=$1
    output=$(mktemp)
    if run_validate "$output" --schema "$fixture"; then
        status=0
    else
        status=$?
    fi
    if [ "$status" -ne 0 ]; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture failed validation with status $status"
    fi
    if ! grep -F '"errors": []' "$output" >/dev/null; then
        cat "$output" >&2
        rm -f "$output"
        fail "$fixture did not report an empty error list"
    fi
    rm -f "$output"
}

echo "Testing cross-platform adversarial validation fixtures..."
expect_valid "$FIXTURES/valid_edge_cases.toml"
expect_error "$FIXTURES/invalid_cycle.toml" E_CYCLE
expect_error "$FIXTURES/invalid_dangling_ref.toml" E_DANGLING_REF
expect_error "$FIXTURES/invalid_malformed_url.toml" E_MALFORMED_URL
expect_error "$FIXTURES/invalid_dupe_tool.toml" E_DUPE_TOOL
expect_error tests/crossplatform/fixtures/adversarial-unsafe-package.toml E_UNSAFE_PACKAGE_NAME
expect_strict_warning "$FIXTURES/invalid_unknown_placeholder.toml" W_UNKNOWN_PLACEHOLDER

echo "Adversarial validation fixtures passed."

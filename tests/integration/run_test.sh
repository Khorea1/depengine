#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
RUNNER="$DIR/run.sh"

bash -n "$RUNNER"

# Exercise the runner's real bookkeeping functions without triggering its
# Docker/build side effects. Keep this extraction narrow so implementation and
# regression test cannot drift into separate copies of the arithmetic.
eval "$(grep -E '^(PASS|FAIL|FAILED_SCENARIOS)=|^(pass|fail)\(\)' "$RUNNER")"

pass "first success" >/dev/null
pass "second success" >/dev/null
fail "expected failure" >/dev/null

[[ "$PASS" -eq 2 ]]
[[ "$FAIL" -eq 1 ]]
[[ "$FAILED_SCENARIOS" == *"expected failure"* ]]

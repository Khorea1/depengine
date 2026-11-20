#!/usr/bin/env bash
# The three security/concurrency checks used locally and by CI. Installed
# tool versions are pinned by CI, not by this script.
set -euo pipefail
cd "$(dirname "$0")/.."

mode="${1:-all}"
if [ "$#" -gt 0 ]; then shift; fi
case "$mode" in
  race)
    exec go test -race "$@" ./...
    ;;
  staticcheck)
    if ! command -v staticcheck >/dev/null 2>&1; then
      echo 'staticcheck is missing; install honnef.co/go/tools/cmd/staticcheck@v0.8.2' >&2
      exit 127
    fi
    exec staticcheck ./...
    ;;
  govulncheck)
    if ! command -v govulncheck >/dev/null 2>&1; then
      echo 'govulncheck is missing; install golang.org/x/vuln/cmd/govulncheck@v1.8.0' >&2
      exit 127
    fi
    exec govulncheck ./...
    ;;
  all)
    if [ "$#" -ne 0 ]; then
      echo 'usage: scripts/check-quality.sh [all|race|staticcheck|govulncheck] [go test flags for race]' >&2
      exit 2
    fi
    "$0" race
    "$0" staticcheck
    "$0" govulncheck
    ;;
  *)
    echo 'usage: scripts/check-quality.sh [all|race|staticcheck|govulncheck] [go test flags for race]' >&2
    exit 2
    ;;
esac

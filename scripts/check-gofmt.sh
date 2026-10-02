#!/bin/sh
set -eu

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	bad=$(git ls-files -z '*.go' | xargs -0 gofmt -l)
else
	# pre-commit validates a checkout-index snapshot without .git metadata.
	bad=$(find . -type f -name '*.go' -print0 | xargs -0 gofmt -l)
fi

if [ -n "$bad" ]; then
	printf 'gofmt required:\n%s\n' "$bad" >&2
	exit 1
fi

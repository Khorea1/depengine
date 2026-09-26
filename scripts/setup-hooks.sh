#!/bin/sh
# Enable versioned hooks for this worktree only.
set -eu

root=$(git rev-parse --show-toplevel)
cd "$root"
if [ ! -x githooks/pre-commit ] || [ ! -x githooks/pre-push ]; then
	echo "setup-hooks: executable hooks are missing from $root/githooks" >&2
	exit 1
fi

# Worktree-specific config prevents one checkout from changing another's hooks.
git config extensions.worktreeConfig true
git config --worktree core.hooksPath githooks
echo "Local Git gates enabled for $root"

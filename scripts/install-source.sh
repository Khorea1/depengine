#!/bin/sh
# Install depengine from a Git checkout without requiring an existing depengine.
# The old executable is left untouched until the new build is ready.
set -eu

usage() {
    cat <<'HELP'
Usage: sh install-source.sh [--source GIT_URL] [--ref GIT_REF] [--bin-dir ABSOLUTE_PATH]

Build depengine from a Git source checkout and atomically replace the user
executable. Requires git and a Go toolchain compatible with the repository.
Defaults: https://github.com/Khorea1/depengine.git and $HOME/.local/bin.
--ref can select a branch, tag, or commit; omit it for the default branch.
HELP
}

fail() { printf 'depengine bootstrap: %s\n' "$*" >&2; exit 1; }
usage_error() { printf 'depengine bootstrap: %s\n' "$*" >&2; usage >&2; exit 2; }

source_url=https://github.com/Khorea1/depengine.git
ref=
bin_dir=${HOME:+"$HOME/.local/bin"}
while [ "$#" -gt 0 ]; do
    case $1 in
        --source|--ref|--bin-dir)
            option=$1
            [ "$#" -ge 2 ] && [ -n "$2" ] || usage_error "missing value for $option"
            case $option in
                --source) source_url=$2 ;;
                --ref) ref=$2 ;;
                --bin-dir) bin_dir=$2 ;;
            esac
            shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) usage_error "unrecognized argument: $1" ;;
    esac
done
case $bin_dir in
    /*) ;;
    *) usage_error '--bin-dir must be absolute (or set HOME)' ;;
esac
for tool in git go mktemp; do
    command -v "$tool" >/dev/null 2>&1 || fail "required command not found: $tool"
done

umask 077
work=$(mktemp -d "${TMPDIR:-/tmp}/depengine-source.XXXXXXXX") || fail 'cannot create temporary directory'
staged=
cleanup() {
    [ -z "$staged" ] || rm -f -- "$staged"
    rm -rf -- "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# Build away from the destination. An interrupted clone, fetch, or build
# must not remove or truncate the existing executable.
git clone --quiet --depth 1 -- "$source_url" "$work/source" || fail 'clone failed'
if [ -n "$ref" ]; then
    case $ref in -*) usage_error '--ref must not begin with a dash' ;; esac
    git -C "$work/source" fetch --quiet --depth 1 origin "$ref" || fail "cannot fetch ref: $ref"
    git -C "$work/source" checkout --quiet --detach FETCH_HEAD || fail "cannot check out ref: $ref"
fi
(
    cd "$work/source"
    go build -buildvcs=true -o "$work/depengine" .
) || fail 'Go build failed; the previous executable was preserved'
"$work/depengine" --version >/dev/null || fail 'new executable failed its version check'

mkdir -p -- "$bin_dir" || fail "cannot create destination: $bin_dir"
[ ! -d "$bin_dir/depengine" ] || fail 'destination depengine is a directory'
staged=$(mktemp "$bin_dir/.depengine.XXXXXXXX") || fail 'cannot create destination staging file'
cp -- "$work/depengine" "$staged" || fail 'cannot stage new executable'
chmod 755 "$staged" || fail 'cannot set executable permissions'
# Same-directory rename replaces an existing running executable on Unix.
# Do not delete the old executable first.
mv -f -- "$staged" "$bin_dir/depengine" || fail 'cannot activate new executable'
staged=
printf 'Installed %s\n' "$bin_dir/depengine"

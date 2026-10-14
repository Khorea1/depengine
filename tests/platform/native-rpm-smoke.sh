#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo "usage: $0 <dnf|zypper> <installed-package>" >&2
    exit 2
fi
MANAGER=$1
PKG=$2
case "$MANAGER" in dnf|zypper) ;; *) echo "unsupported RPM manager: $MANAGER" >&2; exit 2 ;; esac
BIN=${DEPENGINE_BIN:-depengine}
WORKDIR=$(mktemp -d)
export XDG_CONFIG_HOME="$WORKDIR/config"
export XDG_STATE_HOME="$WORKDIR/state"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_STATE_HOME"
trap 'rm -rf "$WORKDIR"' 0
trap 'exit 1' 1 2 15

rpm --version
rpm --eval 'dbpath=%{_dbpath} backend=%{_db_backend}'
RPM_ARCH=$(rpm --eval '%{_arch}')
QUERY_FORMAT='%{NAME}\t%{ARCH}\n'
same_output() {
  [ "$(cat "$1")" = "$(cat "$2")" ] && [ "$(wc -c <"$1")" -eq "$(wc -c <"$2")" ]
}
LC_ALL=C rpm -q --qf="$QUERY_FORMAT" -- "$PKG" >"$WORKDIR/installed.stdout" 2>"$WORKDIR/installed.stderr"
INSTALLED_EXIT=$?
printf '%s\t%s\n' "$PKG" "$RPM_ARCH" >"$WORKDIR/expected-installed.stdout"
if [ "$INSTALLED_EXIT" -ne 0 ] || ! same_output "$WORKDIR/expected-installed.stdout" "$WORKDIR/installed.stdout" || [ -s "$WORKDIR/installed.stderr" ]; then
    echo "installed RPM query channels were unexpected (exit $INSTALLED_EXIT)" >&2
    cat "$WORKDIR/installed.stdout" "$WORKDIR/installed.stderr" >&2
    exit 1
fi

ABSENT_PKG=depengine-rpm-absent-smoke-9f21
set +e
LC_ALL=C rpm -q --qf="$QUERY_FORMAT" -- "$ABSENT_PKG" >"$WORKDIR/absent.stdout" 2>"$WORKDIR/absent.stderr"
ABSENT_EXIT=$?
LC_ALL=C rpm -q --qf="$QUERY_FORMAT" -- filesystem >"$WORKDIR/absent-sentinel.stdout" 2>"$WORKDIR/absent-sentinel.stderr"
ABSENT_SENTINEL_EXIT=$?
set -e
printf 'package %s is not installed\n' "$ABSENT_PKG" >"$WORKDIR/expected-absent.stdout"
printf 'filesystem\t%s\n' "$RPM_ARCH" >"$WORKDIR/expected-absent-sentinel.stdout"
if [ "$ABSENT_EXIT" -ne 1 ] || ! same_output "$WORKDIR/expected-absent.stdout" "$WORKDIR/absent.stdout" || [ -s "$WORKDIR/absent.stderr" ] || [ "$ABSENT_SENTINEL_EXIT" -ne 0 ] || ! same_output "$WORKDIR/expected-absent-sentinel.stdout" "$WORKDIR/absent-sentinel.stdout" || [ -s "$WORKDIR/absent-sentinel.stderr" ]; then
    echo "absent target/sentinel output channels were unexpected (target exit $ABSENT_EXIT, sentinel exit $ABSENT_SENTINEL_EXIT)" >&2
    cat "$WORKDIR/absent.stdout" "$WORKDIR/absent.stderr" "$WORKDIR/absent-sentinel.stdout" "$WORKDIR/absent-sentinel.stderr" >&2
    exit 1
fi

# With an unavailable database RPM emits the same absence-looking target and
# sentinel output. The sentinel's absent row must prevent that from proving absence.
MISSING_DB="$WORKDIR/unavailable-rpmdb"
mkdir -p "$MISSING_DB"
set +e
LC_ALL=C rpm --dbpath "$MISSING_DB" -q --qf="$QUERY_FORMAT" -- "$ABSENT_PKG" >"$WORKDIR/unavailable-target.stdout" 2>"$WORKDIR/unavailable-target.stderr"
UNAVAILABLE_TARGET_EXIT=$?
LC_ALL=C rpm --dbpath "$MISSING_DB" -q --qf="$QUERY_FORMAT" -- filesystem >"$WORKDIR/unavailable-sentinel.stdout" 2>"$WORKDIR/unavailable-sentinel.stderr"
UNAVAILABLE_SENTINEL_EXIT=$?
set -e
printf 'package %s is not installed\n' "$ABSENT_PKG" >"$WORKDIR/expected-unavailable-target.stdout"
printf 'package filesystem is not installed\n' >"$WORKDIR/expected-unavailable-sentinel.stdout"
if [ "$UNAVAILABLE_TARGET_EXIT" -ne 1 ] || [ -s "$WORKDIR/unavailable-target.stderr" ] || ! same_output "$WORKDIR/expected-unavailable-target.stdout" "$WORKDIR/unavailable-target.stdout" || [ "$UNAVAILABLE_SENTINEL_EXIT" -ne 1 ] || [ -s "$WORKDIR/unavailable-sentinel.stderr" ] || ! same_output "$WORKDIR/expected-unavailable-sentinel.stdout" "$WORKDIR/unavailable-sentinel.stdout"; then
    echo "unavailable RPM database channels were unexpected (target exit $UNAVAILABLE_TARGET_EXIT, sentinel exit $UNAVAILABLE_SENTINEL_EXIT)" >&2
    exit 1
fi

# Invalid query formatting returns a diagnostic on stderr on the probed RPMs.
set +e
LC_ALL=C rpm -q --qf='%{NOT_A_TAG}\n' -- "$PKG" >"$WORKDIR/query-error.stdout" 2>"$WORKDIR/query-error.stderr"
QUERY_ERROR_EXIT=$?
LC_ALL=C rpm -q --qf="$QUERY_FORMAT" -- filesystem >"$WORKDIR/query-sentinel.stdout" 2>"$WORKDIR/query-sentinel.stderr"
QUERY_SENTINEL_EXIT=$?
set -e
case "$(cat "$WORKDIR/query-error.stderr")" in *"incorrect format"*|*"unknown tag"*) ;; *) echo "malformed RPM query diagnostic was not on stderr" >&2; cat "$WORKDIR/query-error.stderr" >&2; exit 1 ;; esac
printf 'filesystem\t%s\n' "$RPM_ARCH" >"$WORKDIR/expected-query-sentinel.stdout"
if [ "$QUERY_ERROR_EXIT" -ne 0 ] || [ -s "$WORKDIR/query-error.stdout" ] || [ "$QUERY_SENTINEL_EXIT" -ne 0 ] || [ -s "$WORKDIR/query-sentinel.stderr" ] || ! same_output "$WORKDIR/expected-query-sentinel.stdout" "$WORKDIR/query-sentinel.stdout"; then
    echo "malformed-query/healthy-sentinel channels were unexpected" >&2
    exit 1
fi
printf 'installed/absent channels exact; unavailable DB target/sentinel both exit 1; malformed query diagnostic is stderr (exit %s) with healthy sentinel\n' "$QUERY_ERROR_EXIT"

cat >"$WORKDIR/schema.toml" <<EOF
schema_version = 1
[defaults]
manager = "$MANAGER"
method_order = ["native"]
[tools.rpm-smoke]
method_only = ["native"]
[tools.rpm-smoke.native]
pkg = "$PKG"
EOF
"$BIN" validate --schema "$WORKDIR/schema.toml" --no-manifest
"$BIN" check --schema "$WORKDIR/schema.toml" --no-manifest --live rpm-smoke

cat >"$WORKDIR/absent.toml" <<EOF
schema_version = 1
[defaults]
manager = "$MANAGER"
method_order = ["native"]
[tools.rpm-absent]
method_only = ["native"]
[tools.rpm-absent.native]
pkg = "$ABSENT_PKG"
EOF
set +e
ABSENT_CHECK=$("$BIN" check --schema "$WORKDIR/absent.toml" --no-manifest --live --format=json rpm-absent)
ABSENT_CHECK_EXIT=$?
set -e
case "$ABSENT_CHECK" in *'"status":"absent"'*'"state":"absent"'*) ;; *) echo "depengine check did not report PresenceAbsent: $ABSENT_CHECK" >&2; exit 1 ;; esac
if [ "$ABSENT_CHECK_EXIT" -ne 1 ]; then
    echo "depengine check exit = $ABSENT_CHECK_EXIT, want 1 for verified absence" >&2
    exit 1
fi

# Record the known installed package, then damage only this disposable
# container's RPM database. Subsequent observation must be unknown/broken,
# and remove must preserve both the package and its tracking record.
"$BIN" install --schema "$WORKDIR/schema.toml" --only rpm-smoke
DBPATH=$(rpm --eval '%{_dbpath}')
BACKEND=$(rpm --eval '%{_db_backend}')
case "$BACKEND" in
    sqlite) printf 'invalid rpm database probe\n' >"$DBPATH/rpmdb.sqlite" ;;
    ndb) printf 'invalid rpm database probe\n' >"$DBPATH/Packages.db"; printf 'invalid rpm database probe\n' >"$DBPATH/Index.db" ;;
    *) echo "unprobed RPM database backend: $BACKEND" >&2; exit 1 ;;
esac
STATUS=$("$BIN" status --format=json)
case "$STATUS" in
    *'"status": "unknown"'*|*'"status": "broken"'*) ;;
    *) echo "damaged RPM database did not produce unknown/broken status: $STATUS" >&2; exit 1 ;;
esac
if "$BIN" remove rpm-smoke; then
    echo "remove succeeded with a damaged RPM database" >&2
    exit 1
fi
STATUS_AFTER=$("$BIN" status --format=json)
case "$STATUS_AFTER" in
    *'"name": "rpm-smoke"'*'"status": "unknown"'*|*'"name": "rpm-smoke"'*'"status": "broken"'*) ;;
    *) echo "unknown RPM state/tracking was not preserved: $STATUS_AFTER" >&2; exit 1 ;;
esac

echo "RPM probe passed: manager=$MANAGER package=$PKG backend=$BACKEND"

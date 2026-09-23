#!/usr/bin/env bash
set -euo pipefail

: "${ANDROID_HOME:?ANDROID_HOME must be set by the runner}"
ndk="${ANDROID_HOME}/ndk/27.3.13750724"
cc="${ndk}/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android21-clang"
if [[ ! -x "${cc}" ]]; then
  echo "Android NDK compiler not found: ${cc}" >&2
  exit 1
fi

build_dir="$(mktemp -d)"
trap 'rm -rf "${build_dir}"' EXIT

echo "Building depengine for Android/amd64..."
CGO_ENABLED=1 GOOS=android GOARCH=amd64 CC="${cc}" \
  go build -trimpath -o "${build_dir}/depengine" .

adb wait-for-device
adb push "${build_dir}/depengine" /data/local/tmp/depengine >/dev/null
adb push tests/platform/fixtures/android-runtime.toml /data/local/tmp/android-runtime.toml >/dev/null
adb shell chmod 0755 /data/local/tmp/depengine

echo "Checking CLI execution on Android..."
version_output="$(adb shell /data/local/tmp/depengine --version | tr -d '\r')"
printf '%s\n' "${version_output}"
printf '%s\n' "${version_output}" | grep -q '^depengine '

echo "Checking depengine runtime detection on Android..."
adb shell rm -rf /data/local/tmp/depengine-smoke-home
adb shell mkdir -p \
  /data/local/tmp/depengine-smoke-home/config \
  /data/local/tmp/depengine-smoke-home/state \
  /data/local/tmp/depengine-smoke-home/tmp

runtime_output="$(
  adb shell "HOME=/data/local/tmp/depengine-smoke-home \
XDG_CONFIG_HOME=/data/local/tmp/depengine-smoke-home/config \
XDG_STATE_HOME=/data/local/tmp/depengine-smoke-home/state \
TMPDIR=/data/local/tmp/depengine-smoke-home/tmp \
CI=1 LANG=C \
/data/local/tmp/depengine install --dry-run --schema /data/local/tmp/android-runtime.toml" 2>&1 | tr -d '\r'
)"
printf '%s\n' "${runtime_output}"
printf '%s\n' "${runtime_output}" | grep -Eq 'target[[:space:]]+android \(android\)'

echo "Android runtime smoke test passed."

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


termux_version="0.118.3"
termux_package="com.termux"
termux_prefix="/data/data/com.termux/files/usr"
termux_home="/data/data/com.termux/files/home"
termux_apk_name="termux-app_v${termux_version}+github-debug_x86_64.apk"
termux_apk="${build_dir}/${termux_apk_name}"
termux_apk_sha256="3550e61f4d9eb49b712fd1bd9519dc37085a4d8eb597c57a340f0a64859b7144"
termux_release_base="https://github.com/termux/termux-app/releases/download/v${termux_version}"

echo "Installing pinned Termux GitHub-debug build..."
curl --fail --location --retry 3 --retry-all-errors \
  -o "${termux_apk}" \
  "${termux_release_base}/termux-app_v${termux_version}%2Bgithub-debug_x86_64.apk"
printf '%s  %s\n' "${termux_apk_sha256}" "${termux_apk}" | sha256sum --check --strict
adb install -r "${termux_apk}" >/dev/null
adb shell am start -W -n "${termux_package}/.app.TermuxActivity" >/dev/null

echo "Waiting for Termux bootstrap..."
termux_ready=0
for _ in $(seq 1 90); do
  if adb shell "run-as ${termux_package} /system/bin/sh -c 'test -x ${termux_prefix}/bin/sh'" >/dev/null 2>&1; then
    termux_ready=1
    break
  fi
  sleep 1
done
if [[ "${termux_ready}" -ne 1 ]]; then
  echo "Termux bootstrap did not become ready" >&2
  adb logcat -d -t 300 | grep -i termux || true
  exit 1
fi

# The pinned APK can bootstrap with a community mirror selected at build time.
# Keep CI independent of that mutable mirror choice by selecting Termux's
# official primary repository before exercising the native package lifecycle.
echo "Pinning Termux package source..."
adb shell "run-as ${termux_package} /system/bin/sh -c 'printf \"%s\\n\" \"deb https://packages.termux.dev/apt/termux-main stable main\" > ${termux_prefix}/etc/apt/sources.list'"

echo "Staging depengine lifecycle harness inside Termux..."
adb shell "run-as ${termux_package} /system/bin/mkdir -p ${termux_home}/tests/platform/fixtures"

copy_into_termux() {
  local src=$1
  local dest=$2
  local mode=$3
  local stage="/data/local/tmp/depengine-termux-stage"
  adb push "${src}" "${stage}" >/dev/null
  adb shell "cat ${stage} | run-as ${termux_package} /system/bin/sh -c 'cat > ${dest} && chmod ${mode} ${dest}'"
}

copy_into_termux "${build_dir}/depengine" "${termux_home}/depengine" 0755
copy_into_termux tests/platform/native-lifecycle.sh "${termux_home}/tests/platform/native-lifecycle.sh" 0755
copy_into_termux tests/platform/fixtures/termux-native.toml "${termux_home}/tests/platform/fixtures/termux-native.toml" 0644

echo "Running native pkg lifecycle inside Termux..."
adb shell "run-as ${termux_package} /system/bin/sh -c 'export PREFIX=${termux_prefix}; export HOME=${termux_home}; export TMPDIR=${termux_prefix}/tmp; export TERMUX_VERSION=${termux_version}; export PATH=${termux_prefix}/bin:/system/bin; export DEPENGINE_BIN=${termux_home}/depengine; export CI=1; export LANG=C; export LC_ALL=C; cd ${termux_home}; exec ${termux_prefix}/bin/sh ${termux_home}/tests/platform/native-lifecycle.sh ${termux_home}/tests/platform/fixtures/termux-native.toml depengine-termux-smoke'"

echo "Termux native lifecycle smoke test passed."
